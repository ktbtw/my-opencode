package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"

	"relay-server/internal/model"
)

var ErrOffline = errors.New("device offline")

type Device struct {
	ID           string
	OperatorID   int64
	MachineID    string
	Hostname     string
	Version      string
	Kind         string
	Projects     []model.HelloProject
	Capabilities []string
	SeenAt       time.Time
	CurrentTask  string
	// Metrics 是设备最近一次心跳上报的运行状态，nil 表示该设备尚未上报。
	Metrics *model.DeviceMetrics
	conn    *websocket.Conn
	mu      sync.Mutex
}

type ModelCache struct {
	OperatorID int64
	Payload    map[string]any
	UpdatedAt  time.Time
}

type Broker struct {
	mu          sync.RWMutex
	devices     map[string]*Device
	modelCaches map[string]ModelCache
	observerMu  sync.RWMutex
	onUpsert    func(int64, model.Agent)
	onRemove    func(int64, model.Agent)
	// onMetrics 在设备上报新指标时触发，用于把指标推送给已打开设备页的客户端。
	onMetrics func(int64, model.MachineMetricsUpdate)
}

func New() *Broker {
	return &Broker{
		devices:     map[string]*Device{},
		modelCaches: map[string]ModelCache{},
	}
}

func (b *Broker) Add(conn *websocket.Conn, hello model.HelloPayload, operatorID int64) *Device {
	b.mu.Lock()
	id := hello.AgentID
	if id == "" {
		id = hello.DeviceID
	}
	machineID := hello.MachineID
	if machineID == "" {
		if hello.Hostname != "" {
			machineID = hello.Hostname
		} else {
			machineID = id
		}
	}
	dev := &Device{
		ID:           id,
		OperatorID:   operatorID,
		MachineID:    machineID,
		Hostname:     hello.Hostname,
		Version:      hello.Version,
		Kind:         hello.Kind,
		Projects:     hello.Projects,
		Capabilities: append([]string(nil), hello.Capabilities...),
		SeenAt:       time.Now().UTC(),
		conn:         conn,
	}
	key := deviceKey(operatorID, id)
	previous := b.devices[key]
	b.devices[key] = dev
	b.mu.Unlock()
	if previous != nil && previous != dev && previous.conn != nil {
		_ = previous.conn.Close(websocket.StatusNormalClosure, "replaced by newer connection")
	}
	b.notifyUpsert(operatorID, snapshot(dev))
	return dev
}

func (b *Broker) Touch(id, currentTask string) {
	b.mu.Lock()
	changed := make([]model.Agent, 0, 1)
	for _, dev := range b.devices {
		if dev.ID != id {
			continue
		}
		previousTask := dev.CurrentTask
		dev.SeenAt = time.Now().UTC()
		dev.CurrentTask = currentTask
		if previousTask != currentTask {
			changed = append(changed, snapshot(dev))
		}
	}
	b.mu.Unlock()
	for _, agent := range changed {
		b.notifyUpsert(agent.OperatorID, agent)
	}
}

// TouchDevice 更新设备的在线时间与当前任务，并可附带最新指标快照。
// metrics 为 nil 时保留已有的指标数据，避免老版本设备覆盖掉历史值。
// 返回值表示该设备是否仍属于当前 broker。
func (b *Broker) TouchDevice(dev *Device, currentTask string, metrics *model.DeviceMetrics) bool {
	if dev == nil {
		return false
	}
	b.mu.Lock()
	current, ok := b.devices[deviceKey(dev.OperatorID, dev.ID)]
	if !ok || current != dev {
		b.mu.Unlock()
		return false
	}
	previousTask := current.CurrentTask
	current.SeenAt = time.Now().UTC()
	current.CurrentTask = currentTask
	metricsChanged := false
	var metricsUpdate model.MachineMetricsUpdate
	if metrics != nil {
		metrics.ReceivedAt = time.Now().UTC()
		current.Metrics = metrics
		metricsChanged = true
		metricsUpdate = model.MachineMetricsUpdate{
			MachineID: current.MachineID,
			Metrics:   metrics,
		}
	}
	agent := snapshot(current)
	b.mu.Unlock()
	// 仅在任务状态真正变化时广播 agent 事件。
	// 心跳本身（含指标）不应触发 agent.upsert：agent 状态没有变化，
	// 每 15 秒重复推送同一份 agent 快照只是浪费带宽。
	if previousTask != currentTask {
		b.notifyUpsert(agent.OperatorID, agent)
	}
	// 指标走独立事件。每次心跳的指标都有变化（运行时长递增、
	// 内存与 CPU 波动），因此不做等值比较，直接推送。
	// 单设备每 15 秒约 400 字节，带宽开销可忽略。
	if metricsChanged && metricsUpdate.MachineID != "" {
		b.notifyMetrics(agent.OperatorID, metricsUpdate)
	}
	return true
}

func (b *Broker) ClearTask(agentID, taskID string) {
	b.mu.Lock()
	changed := make([]model.Agent, 0, 1)
	for _, dev := range b.devices {
		if dev.ID == agentID && dev.CurrentTask == taskID {
			dev.CurrentTask = ""
			changed = append(changed, snapshot(dev))
		}
	}
	b.mu.Unlock()
	for _, agent := range changed {
		b.notifyUpsert(agent.OperatorID, agent)
	}
}

func (b *Broker) ClearTaskForOperator(operatorID int64, agentID, taskID string) {
	b.mu.Lock()
	var changed *model.Agent
	if dev, ok := b.devices[deviceKey(operatorID, agentID)]; ok && dev.CurrentTask == taskID {
		dev.CurrentTask = ""
		agent := snapshot(dev)
		changed = &agent
	}
	b.mu.Unlock()
	if changed != nil {
		b.notifyUpsert(operatorID, *changed)
	}
}

func (b *Broker) Remove(dev *Device) bool {
	if dev == nil {
		return false
	}
	b.mu.Lock()
	current, ok := b.devices[deviceKey(dev.OperatorID, dev.ID)]
	if !ok || current != dev {
		b.mu.Unlock()
		return false
	}
	agent := snapshot(current)
	if current.MachineID != "" {
		delete(b.modelCaches, machineKey(dev.OperatorID, dev.MachineID))
	}
	delete(b.devices, deviceKey(dev.OperatorID, dev.ID))
	b.mu.Unlock()
	b.notifyRemove(dev.OperatorID, agent)
	return true
}

func (b *Broker) SetObserver(onUpsert, onRemove func(int64, model.Agent)) {
	b.observerMu.Lock()
	b.onUpsert = onUpsert
	b.onRemove = onRemove
	b.observerMu.Unlock()
}

// SetMetricsObserver 注册设备指标变更回调。
func (b *Broker) SetMetricsObserver(onMetrics func(int64, model.MachineMetricsUpdate)) {
	b.observerMu.Lock()
	b.onMetrics = onMetrics
	b.observerMu.Unlock()
}

func (b *Broker) notifyMetrics(operatorID int64, update model.MachineMetricsUpdate) {
	b.observerMu.RLock()
	observer := b.onMetrics
	b.observerMu.RUnlock()
	if observer != nil {
		observer(operatorID, update)
	}
}

func (b *Broker) notifyUpsert(operatorID int64, agent model.Agent) {
	b.observerMu.RLock()
	observer := b.onUpsert
	b.observerMu.RUnlock()
	if observer != nil {
		observer(operatorID, agent)
	}
}

func (b *Broker) notifyRemove(operatorID int64, agent model.Agent) {
	b.observerMu.RLock()
	observer := b.onRemove
	b.observerMu.RUnlock()
	if observer != nil {
		observer(operatorID, agent)
	}
}

func (b *Broker) SetModelCache(operatorID int64, machineID string, payload map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.modelCaches[machineKey(operatorID, machineID)] = ModelCache{OperatorID: operatorID, Payload: payload, UpdatedAt: time.Now().UTC()}
}

func (b *Broker) ClearModelCache(operatorID int64, machineID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.modelCaches, machineKey(operatorID, machineID))
}

func (b *Broker) GetModelCache(operatorID int64, machineID string) (ModelCache, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cache, ok := b.modelCaches[machineKey(operatorID, machineID)]
	return cache, ok
}

func (b *Broker) Has(id string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, dev := range b.devices {
		if dev.ID == id {
			return true
		}
	}
	return false
}

func (b *Broker) Get(id string, operatorID ...int64) (model.Agent, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(operatorID) > 0 && operatorID[0] > 0 {
		dev, ok := b.devices[deviceKey(operatorID[0], id)]
		if !ok {
			return model.Agent{}, false
		}
		return snapshot(dev), true
	}
	var dev *Device
	for _, item := range b.devices {
		if item.ID == id {
			dev = item
			break
		}
	}
	if dev == nil {
		return model.Agent{}, false
	}
	return snapshot(dev), true
}

func (b *Broker) List() []model.Agent {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]model.Agent, 0, len(b.devices))
	for _, dev := range b.devices {
		out = append(out, snapshot(dev))
	}
	return out
}

func (b *Broker) ListMachines(operatorID int64) []model.Machine {
	b.mu.RLock()
	defer b.mu.RUnlock()

	index := map[string]*model.Machine{}
	for _, dev := range b.devices {
		if operatorID > 0 && dev.OperatorID != operatorID {
			continue
		}
		machine, ok := index[dev.MachineID]
		if !ok {
			machine = &model.Machine{
				MachineID: dev.MachineID,
				Hostname:  dev.Hostname,
				Status:    "online",
				SeenAt:    dev.SeenAt,
				Agents:    []model.Agent{},
			}
			index[dev.MachineID] = machine
		}
		if dev.SeenAt.After(machine.SeenAt) {
			machine.SeenAt = dev.SeenAt
		}
		if dev.Kind == "launcher" {
			machine.LauncherOnline = true
			// 指标由 launcher 上报，同一台机器以 launcher 设备的数据为准。
			if dev.Metrics != nil {
				machine.Metrics = dev.Metrics
			}
			continue
		}
		machine.Agents = append(machine.Agents, snapshot(dev))
	}

	out := make([]model.Machine, 0, len(index))
	for _, machine := range index {
		sort.SliceStable(machine.Agents, func(i, j int) bool {
			return machine.Agents[i].ID < machine.Agents[j].ID
		})
		out = append(out, *machine)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].MachineID < out[j].MachineID
	})
	return out
}

func (b *Broker) GetMachine(operatorID int64, machineID string) (model.Machine, bool) {
	for _, machine := range b.ListMachines(operatorID) {
		if machine.MachineID == machineID {
			return machine, true
		}
	}
	return model.Machine{}, false
}

func (b *Broker) Dispatch(ctx context.Context, deviceID string, env model.Envelope) error {
	b.mu.RLock()
	key := ""
	for candidate, item := range b.devices {
		if item.ID == deviceID {
			key = candidate
			break
		}
	}
	b.mu.RUnlock()
	if key == "" {
		return ErrOffline
	}
	return b.dispatch(ctx, key, env)
}

func (b *Broker) DispatchForOperator(ctx context.Context, operatorID int64, deviceID string, env model.Envelope) error {
	return b.dispatch(ctx, deviceKey(operatorID, deviceID), env)
}

func (b *Broker) dispatch(ctx context.Context, key string, env model.Envelope) error {
	b.mu.RLock()
	dev, ok := b.devices[key]
	b.mu.RUnlock()
	if !ok {
		return ErrOffline
	}

	dev.mu.Lock()
	defer dev.mu.Unlock()
	buf, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return dev.conn.Write(ctx, websocket.MessageText, buf)
}

func deviceKey(operatorID int64, id string) string {
	return fmt.Sprintf("%d:%s", operatorID, id)
}

func machineKey(operatorID int64, machineID string) string {
	return fmt.Sprintf("%d:%s", operatorID, machineID)
}

func snapshot(dev *Device) model.Agent {
	projects := make([]model.HelloProject, len(dev.Projects))
	copy(projects, dev.Projects)
	return model.Agent{
		ID:           dev.ID,
		OperatorID:   dev.OperatorID,
		MachineID:    dev.MachineID,
		Hostname:     dev.Hostname,
		Version:      dev.Version,
		Enabled:      true,
		Status:       "online",
		Kind:         dev.Kind,
		Projects:     projects,
		Capabilities: append([]string(nil), dev.Capabilities...),
		SeenAt:       dev.SeenAt,
		CurrentTask:  dev.CurrentTask,
	}
}

func (b *Broker) GetLauncher(machineID string, operatorID ...int64) (model.Agent, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, dev := range b.devices {
		if dev.Kind != "launcher" || dev.MachineID != machineID {
			continue
		}
		if len(operatorID) > 0 && operatorID[0] > 0 && dev.OperatorID != operatorID[0] {
			continue
		}
		return snapshot(dev), true
	}
	return model.Agent{}, false
}
