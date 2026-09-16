package broker

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"relay-server/internal/model"
)

// TestMetricsMemoryStaysBounded 验证长时间高频心跳不会导致内存增长。
// 指标是"替换"语义而非"追加"，因此内存占用应与心跳次数无关。
func TestMetricsMemoryStaysBounded(t *testing.T) {
	b := New()
	const deviceCount = 50
	devices := make([]*Device, 0, deviceCount)
	for i := 0; i < deviceCount; i++ {
		devices = append(devices, b.Add(nil, model.HelloPayload{
			AgentID:   fmt.Sprintf("launcher:m_%d", i),
			MachineID: fmt.Sprintf("m_%d", i),
			Kind:      "launcher",
		}, 1))
	}

	buildMetrics := func(round int) *model.DeviceMetrics {
		return &model.DeviceMetrics{
			CollectedAt:   time.Now().UTC(),
			Platform:      "windows",
			Arch:          "amd64",
			UptimeSeconds: uint64(3600 + round),
			MemoryTotal:   16 << 30,
			MemoryUsed:    uint64(7<<30 + round),
			MemoryPercent: 43.75,
			CPUPercent:    11.5,
			CPUCores:      8,
			Disks: []model.DiskMetric{
				{Mount: "C:", TotalBytes: 500 << 30, UsedBytes: 375 << 30, UsedPercent: 75},
				{Mount: "D:", TotalBytes: 1000 << 30, UsedBytes: 250 << 30, UsedPercent: 25},
			},
		}
	}

	// 预热，让 map 与常驻对象先分配好。
	for round := 0; round < 20; round++ {
		for _, device := range devices {
			b.TouchDevice(device, "", buildMetrics(round))
		}
	}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	// 模拟 200 轮心跳：50 设备 × 200 轮 = 10000 次指标更新。
	const rounds = 200
	for round := 0; round < rounds; round++ {
		for _, device := range devices {
			b.TouchDevice(device, "", buildMetrics(round))
		}
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	perDevice := float64(after.HeapAlloc) / float64(deviceCount)
	t.Logf("%d 设备 × %d 轮心跳 = %d 次指标更新", deviceCount, rounds, deviceCount*rounds)
	t.Logf("堆内存变化: %d 字节", growth)
	t.Logf("每设备常驻: %.0f 字节", perDevice)

	// 指标是替换语义，堆增长不应随更新次数线性累积。
	// 10000 次更新若每次泄漏 400 字节就是 4MB，这里给出明确上限。
	const maxGrowthBytes = 1 << 20 // 1MB
	if growth > maxGrowthBytes {
		t.Fatalf("内存增长过大: %d 字节（可能未替换而是累积）", growth)
	}
}

// TestDeviceMetricsAreReplacedNotAppended 验证指标确实被替换。
func TestDeviceMetricsAreReplacedNotAppended(t *testing.T) {
	b := New()
	device := b.Add(nil, model.HelloPayload{
		AgentID:   "launcher:m_1",
		MachineID: "m_1",
		Kind:      "launcher",
	}, 1)

	for round := 0; round < 500; round++ {
		b.TouchDevice(device, "", &model.DeviceMetrics{
			Platform:      "windows",
			UptimeSeconds: uint64(round),
			Disks: []model.DiskMetric{
				{Mount: "C:", TotalBytes: 100, UsedBytes: uint64(round)},
			},
		})
	}

	machine, ok := b.GetMachine(1, "m_1")
	if !ok {
		t.Fatal("设备应存在")
	}
	if machine.Metrics == nil {
		t.Fatal("指标应存在")
	}
	// 应保留最后一轮的值，而不是累积 500 条。
	if machine.Metrics.UptimeSeconds != 499 {
		t.Fatalf("应保留最后一轮指标，实际 uptime=%d", machine.Metrics.UptimeSeconds)
	}
	if len(machine.Metrics.Disks) != 1 {
		t.Fatalf("磁盘数量不应累积，实际 %d", len(machine.Metrics.Disks))
	}
}
