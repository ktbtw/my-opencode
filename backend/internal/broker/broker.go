package broker

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"

	"relay-server/internal/model"
)

var ErrOffline = errors.New("device offline")

type Device struct {
	ID          string
	OperatorID  int64
	MachineID   string
	Hostname    string
	Version     string
	Projects    []model.HelloProject
	SeenAt      time.Time
	CurrentTask string
	ModelsJSON  json.RawMessage // 缓存的模型列表原始 JSON
	conn        *websocket.Conn
	mu          sync.Mutex
}

type Broker struct {
	mu      sync.RWMutex
	devices map[string]*Device
}

func New() *Broker {
	return &Broker{
		devices: map[string]*Device{},
	}
}

func (b *Broker) Add(conn *websocket.Conn, hello model.HelloPayload, operatorID int64) *Device {
	b.mu.Lock()
	defer b.mu.Unlock()
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
		ID:         id,
		OperatorID: operatorID,
		MachineID:  machineID,
		Hostname:   hello.Hostname,
		Version:    hello.Version,
		Projects:   hello.Projects,
		SeenAt:     time.Now().UTC(),
		conn:       conn,
	}
	b.devices[id] = dev
	return dev
}

func (b *Broker) Touch(id, currentTask string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if dev, ok := b.devices[id]; ok {
		dev.SeenAt = time.Now().UTC()
		dev.CurrentTask = currentTask
	}
}

func (b *Broker) Remove(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.devices, id)
}

func (b *Broker) SetModels(id string, data json.RawMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if dev, ok := b.devices[id]; ok {
		dev.ModelsJSON = data
	}
}

func (b *Broker) GetModels(id string) (json.RawMessage, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	dev, ok := b.devices[id]
	if !ok || dev.ModelsJSON == nil {
		return nil, false
	}
	return dev.ModelsJSON, true
}

func (b *Broker) Has(id string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.devices[id]
	return ok
}

func (b *Broker) Get(id string) (model.Agent, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	dev, ok := b.devices[id]
	if !ok {
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
		machine.Agents = append(machine.Agents, snapshot(dev))
	}

	out := make([]model.Machine, 0, len(index))
	for _, machine := range index {
		out = append(out, *machine)
	}
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
	dev, ok := b.devices[deviceID]
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

func snapshot(dev *Device) model.Agent {
	projects := make([]model.HelloProject, len(dev.Projects))
	copy(projects, dev.Projects)
	return model.Agent{
		ID:          dev.ID,
		OperatorID:  dev.OperatorID,
		MachineID:   dev.MachineID,
		Hostname:    dev.Hostname,
		Version:     dev.Version,
		Projects:    projects,
		SeenAt:      dev.SeenAt,
		CurrentTask: dev.CurrentTask,
	}
}
