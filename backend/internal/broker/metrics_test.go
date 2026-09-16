package broker

import (
	"testing"
	"time"

	"relay-server/internal/model"
)

// TestTouchDeviceStoresMetrics 验证心跳携带的指标被保存并可在设备列表中读到。
func TestTouchDeviceStoresMetrics(t *testing.T) {
	b := New()
	device := b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_1",
		MachineID: "m_1",
		Hostname:  "pc",
		Kind:      "launcher",
	}, 6)

	metrics := &model.DeviceMetrics{
		CollectedAt:   time.Now().UTC(),
		Platform:      "windows",
		Arch:          "amd64",
		MemoryTotal:   16 << 30,
		MemoryUsed:    8 << 30,
		MemoryPercent: 50,
		CPUPercent:    12.5,
		Disks: []model.DiskMetric{
			{Mount: "C:", TotalBytes: 500 << 30, UsedBytes: 250 << 30, FreeBytes: 250 << 30, UsedPercent: 50},
		},
	}

	if !b.TouchDevice(device, "", metrics) {
		t.Fatal("expected touch to succeed")
	}

	machine, ok := b.GetMachine(6, "m_1")
	if !ok {
		t.Fatal("expected machine to exist")
	}
	if machine.Metrics == nil {
		t.Fatal("指标应被保存到设备上")
	}
	if machine.Metrics.Platform != "windows" {
		t.Fatalf("平台不一致: %s", machine.Metrics.Platform)
	}
	if machine.Metrics.MemoryPercent != 50 {
		t.Fatalf("内存占用率不一致: %v", machine.Metrics.MemoryPercent)
	}
	if len(machine.Metrics.Disks) != 1 || machine.Metrics.Disks[0].Mount != "C:" {
		t.Fatalf("磁盘数据不一致: %+v", machine.Metrics.Disks)
	}
	// 服务端应填写接收时间，供客户端判断新鲜度。
	if machine.Metrics.ReceivedAt.IsZero() {
		t.Fatal("服务端应填写 received_at")
	}
}

// TestHeartbeatWithoutMetricsKeepsPrevious 验证老版本设备不发指标时不会清空已有数据。
func TestHeartbeatWithoutMetricsKeepsPrevious(t *testing.T) {
	b := New()
	device := b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_1",
		MachineID: "m_1",
		Kind:      "launcher",
	}, 6)

	b.TouchDevice(device, "", &model.DeviceMetrics{Platform: "darwin", MemoryPercent: 42})
	b.TouchDevice(device, "", nil)

	machine, ok := b.GetMachine(6, "m_1")
	if !ok {
		t.Fatal("expected machine to exist")
	}
	if machine.Metrics == nil {
		t.Fatal("未上报指标时不应清空已有指标")
	}
	if machine.Metrics.Platform != "darwin" {
		t.Fatalf("已有指标应被保留，实际 %s", machine.Metrics.Platform)
	}
}

// TestMetricsOnlyFromLauncher 验证同机器上普通 agent 的设备不会覆盖 launcher 指标。
func TestMetricsOnlyFromLauncher(t *testing.T) {
	b := New()
	launcher := b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_1",
		MachineID: "m_1",
		Kind:      "launcher",
	}, 6)
	agentDevice := b.Add(nil, model.HelloPayload{
		AgentID:   "agent_1",
		MachineID: "m_1",
		Kind:      "agent",
	}, 6)

	b.TouchDevice(launcher, "", &model.DeviceMetrics{Platform: "launcher-os"})
	b.TouchDevice(agentDevice, "", &model.DeviceMetrics{Platform: "agent-os"})

	machine, ok := b.GetMachine(6, "m_1")
	if !ok {
		t.Fatal("expected machine to exist")
	}
	if machine.Metrics == nil {
		t.Fatal("指标应存在")
	}
	if machine.Metrics.Platform != "launcher-os" {
		t.Fatalf("指标应以 launcher 上报为准，实际 %s", machine.Metrics.Platform)
	}
}

// TestMachineWithoutMetricsIsNil 验证设备未上报指标时该字段为空，客户端据此显示占位。
func TestMachineWithoutMetricsIsNil(t *testing.T) {
	b := New()
	b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_2",
		MachineID: "m_2",
		Kind:      "launcher",
	}, 6)

	machine, ok := b.GetMachine(6, "m_2")
	if !ok {
		t.Fatal("expected machine to exist")
	}
	if machine.Metrics != nil {
		t.Fatalf("未上报时指标应为 nil，实际 %+v", machine.Metrics)
	}
}
