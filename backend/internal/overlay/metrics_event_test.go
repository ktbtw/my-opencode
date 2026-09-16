package overlay

import (
	"testing"

	"relay-server/internal/model"
)

// TestPublishDeviceMetricsEvent 验证指标事件能被订阅者收到，
// 且载荷结构与客户端解析约定一致。
func TestPublishDeviceMetricsEvent(t *testing.T) {
	hub := NewHub()
	events, unsubscribe := hub.Subscribe(7)
	defer unsubscribe()

	hub.Publish(7, "device.metrics", model.MachineMetricsUpdate{
		MachineID: "m_1",
		Metrics: &model.DeviceMetrics{
			Platform:      "windows",
			Arch:          "amd64",
			MemoryPercent: 44,
			CPUPercent:    12.5,
			Disks: []model.DiskMetric{
				{Mount: "C:", TotalBytes: 100, UsedBytes: 75, UsedPercent: 75},
			},
		},
	})

	select {
	case event := <-events:
		if event.Type != "device.metrics" {
			t.Fatalf("事件类型不一致: %s", event.Type)
		}
		update, ok := event.Data.(model.MachineMetricsUpdate)
		if !ok {
			t.Fatalf("载荷类型不一致: %T", event.Data)
		}
		if update.MachineID != "m_1" {
			t.Fatalf("machine_id 不一致: %s", update.MachineID)
		}
		if update.Metrics == nil {
			t.Fatal("指标不应为空")
		}
		if update.Metrics.Platform != "windows" {
			t.Fatalf("平台不一致: %s", update.Metrics.Platform)
		}
		if len(update.Metrics.Disks) != 1 || update.Metrics.Disks[0].Mount != "C:" {
			t.Fatalf("磁盘数据不一致: %+v", update.Metrics.Disks)
		}
	default:
		t.Fatal("未收到指标事件")
	}
}

// TestMetricsEventScopedToOperator 验证指标事件不会跨用户泄漏。
func TestMetricsEventScopedToOperator(t *testing.T) {
	hub := NewHub()
	otherEvents, unsubscribeOther := hub.Subscribe(99)
	defer unsubscribeOther()
	targetEvents, unsubscribeTarget := hub.Subscribe(7)
	defer unsubscribeTarget()

	hub.Publish(7, "device.metrics", model.MachineMetricsUpdate{MachineID: "m_1"})

	select {
	case <-targetEvents:
	default:
		t.Fatal("目标用户应收到事件")
	}
	select {
	case <-otherEvents:
		t.Fatal("其他用户不应收到事件")
	default:
	}
}
