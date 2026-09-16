package app

import (
	"encoding/json"
	"testing"
	"time"

	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/overlay"
	"relay-server/internal/store"
)

// TestDeviceMetricsFlowFromHeartbeatToAPI 验证指标从设备心跳一路流到客户端：
// 设备心跳携带指标 → broker 保存 → REST 返回 → SSE 广播。
func TestDeviceMetricsFlowFromHeartbeatToAPI(t *testing.T) {
	operator := model.Operator{
		ID:          1,
		OperatorUID: "operator-metrics",
		Username:    "metrics-tester",
		OperatorKey: "operator-key-metrics",
	}
	mem := store.NewMemory(relayTestArchive{
		operators: map[string]model.Operator{operator.OperatorKey: operator},
	})
	application := &App{
		store:            mem,
		broker:           broker.New(),
		auth:             auth.NewManager(time.Hour),
		taskCleanupTick:  time.Hour,
		taskTimeout:      time.Hour,
		taskCancelGrace:  time.Hour,
		artifactSubs:     map[string]chan artifactStreamEvent{},
		aiConfigSubs:     map[string]chan model.DeviceAIConfigResultPayload{},
		goalOptimizeSubs: map[string]chan model.GoalOptimizeResultPayload{},
		mcpConfigSubs:    map[string]chan model.DeviceMCPConfigResultPayload{},
		launcherSubs:     map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs:  map[string]chan model.DeviceDirectoriesResultPayload{},
	}
	// 订阅指标事件，验证广播。
	// 生产环境的接线在 app.New() 中完成，这里显式复现同样的注册。
	application.overlayHub = overlay.NewHub()
	application.broker.SetObserver(
		func(operatorID int64, agent model.Agent) {
			application.overlayHub.Publish(operatorID, "agent.upsert", agent)
		},
		func(operatorID int64, agent model.Agent) {
			application.overlayHub.Publish(operatorID, "agent.remove", map[string]string{
				"agent_id": agent.ID, "machine_id": agent.MachineID,
			})
		},
	)
	application.broker.SetMetricsObserver(func(operatorID int64, update model.MachineMetricsUpdate) {
		application.overlayHub.Publish(operatorID, "device.metrics", update)
	})
	events, unsubscribe := application.overlayHub.Subscribe(operator.ID)
	defer unsubscribe()

	device := application.broker.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_metrics",
		MachineID: "m_metrics",
		Kind:      "launcher",
		Hostname:  "metrics-host",
	}, operator.ID)

	metrics := &model.DeviceMetrics{
		CollectedAt:   time.Now().UTC(),
		Platform:      "windows",
		Arch:          "amd64",
		MemoryTotal:   16 << 30,
		MemoryUsed:    7 << 30,
		MemoryPercent: 43.75,
		CPUPercent:    11.5,
		CPUCores:      8,
		UptimeSeconds: 3600,
		Disks: []model.DiskMetric{
			{Mount: "C:", TotalBytes: 500 << 30, UsedBytes: 375 << 30, UsedPercent: 75},
		},
	}

	if err := application.handle(device, mustEnvelopeBytes(t, model.Envelope{
		Type:      "device.heartbeat",
		RequestID: "heartbeat-metrics",
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload:   model.HeartbeatPayload{Metrics: metrics},
	})); err != nil {
		t.Fatalf("handle heartbeat: %v", err)
	}

	// 1. broker 层应保存指标。
	machine, ok := application.broker.GetMachine(operator.ID, "m_metrics")
	if !ok {
		t.Fatal("设备应存在")
	}
	if machine.Metrics == nil {
		t.Fatal("指标应保存到 broker")
	}
	if machine.Metrics.Platform != "windows" {
		t.Fatalf("平台不一致: %s", machine.Metrics.Platform)
	}
	if machine.Metrics.ReceivedAt.IsZero() {
		t.Fatal("服务端应填写 received_at")
	}

	// 2. SSE 应广播 device.metrics 事件。
	// 同一次心跳也会广播 agent.upsert（供 agent 状态消费者使用），
	// 因此这里循环到指标事件为止。
	deadline := time.After(2 * time.Second)
	var metricsEvent *overlay.Event
	for metricsEvent == nil {
		select {
		case event := <-events:
			if event.Type == "device.metrics" {
				metricsEvent = &event
			}
		case <-deadline:
			t.Fatal("未收到 device.metrics 广播")
		}
	}
	update, ok := metricsEvent.Data.(model.MachineMetricsUpdate)
	if !ok {
		t.Fatalf("载荷类型不一致: %T", metricsEvent.Data)
	}
	if update.MachineID != "m_metrics" {
		t.Fatalf("machine_id 不一致: %s", update.MachineID)
	}
	if update.Metrics == nil || update.Metrics.CPUPercent != 11.5 {
		t.Fatalf("指标内容不一致: %+v", update.Metrics)
	}

	// 3. 设备列表数据源应包含指标。
	// 这里直接验证 listDevicesForOperator 的数据来源（broker），
	// 而不经过 HTTP 层：HTTP 的 ListDevices 会额外向设备请求 agent 列表，
	// 那需要真实 WebSocket 连接，不属于本用例的验证范围。
	machines := application.broker.ListMachines(operator.ID)
	var found *model.Machine
	for i := range machines {
		if machines[i].MachineID == "m_metrics" {
			found = &machines[i]
			break
		}
	}
	if found == nil {
		t.Fatal("设备列表应包含目标设备")
	}
	if found.Metrics == nil {
		t.Fatal("设备列表应包含指标")
	}
	if found.Metrics.MemoryPercent != 43.75 {
		t.Fatalf("内存占用率不一致: %v", found.Metrics.MemoryPercent)
	}
	if len(found.Metrics.Disks) != 1 || found.Metrics.Disks[0].Mount != "C:" {
		t.Fatalf("磁盘数据不一致: %+v", found.Metrics.Disks)
	}
	// 指标应能被序列化进 REST 响应（字段名与客户端约定一致）。
	encoded, err := json.Marshal(found)
	if err != nil {
		t.Fatalf("marshal machine: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("unmarshal machine: %v", err)
	}
	metricsRaw, ok := raw["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("REST 载荷应包含 metrics 字段: %s", encoded)
	}
	for _, key := range []string{
		"platform",
		"memory_used_percent",
		"cpu_used_percent",
		"received_at",
		"disks",
	} {
		if _, ok := metricsRaw[key]; !ok {
			t.Fatalf("REST 载荷缺少字段 %q: %s", key, encoded)
		}
	}
}

// TestHeartbeatWithoutMetricsKeepsLastKnown 验证老版本设备不发指标时保留上次数据。
func TestHeartbeatWithoutMetricsKeepsLastKnown(t *testing.T) {
	operator := model.Operator{
		ID:          1,
		OperatorUID: "operator-legacy",
		Username:    "legacy-tester",
		OperatorKey: "operator-key-legacy",
	}
	mem := store.NewMemory(relayTestArchive{
		operators: map[string]model.Operator{operator.OperatorKey: operator},
	})
	application := &App{
		store:            mem,
		broker:           broker.New(),
		auth:             auth.NewManager(time.Hour),
		taskCleanupTick:  time.Hour,
		taskTimeout:      time.Hour,
		taskCancelGrace:  time.Hour,
		artifactSubs:     map[string]chan artifactStreamEvent{},
		aiConfigSubs:     map[string]chan model.DeviceAIConfigResultPayload{},
		goalOptimizeSubs: map[string]chan model.GoalOptimizeResultPayload{},
		mcpConfigSubs:    map[string]chan model.DeviceMCPConfigResultPayload{},
		launcherSubs:     map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs:  map[string]chan model.DeviceDirectoriesResultPayload{},
	}
	device := application.broker.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_legacy",
		MachineID: "m_legacy",
		Kind:      "launcher",
	}, operator.ID)

	// 先上报一次指标。
	if err := application.handle(device, mustEnvelopeBytes(t, model.Envelope{
		Type:    "device.heartbeat",
		Payload: model.HeartbeatPayload{Metrics: &model.DeviceMetrics{Platform: "darwin", MemoryPercent: 55}},
	})); err != nil {
		t.Fatalf("handle heartbeat: %v", err)
	}

	// 再用不带指标的旧格式心跳。
	if err := application.handle(device, mustEnvelopeBytes(t, model.Envelope{
		Type:    "device.heartbeat",
		Payload: model.HeartbeatPayload{AgentID: "launcher:m_legacy"},
	})); err != nil {
		t.Fatalf("handle legacy heartbeat: %v", err)
	}

	machine, ok := application.broker.GetMachine(operator.ID, "m_legacy")
	if !ok {
		t.Fatal("设备应存在")
	}
	if machine.Metrics == nil {
		t.Fatal("旧格式心跳不应清空已有指标")
	}
	if machine.Metrics.Platform != "darwin" {
		t.Fatalf("已有指标应保留，实际 %s", machine.Metrics.Platform)
	}
}
