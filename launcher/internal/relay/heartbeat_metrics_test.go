package relay

import (
	"testing"
	"time"

	"launcher/internal/metrics"
)

// TestDeviceMetricsFromSnapshotCarriesFields 验证采集结果被完整转换到上报模型。
func TestDeviceMetricsFromSnapshotCarriesFields(t *testing.T) {
	collectedAt := time.Now().UTC().Truncate(time.Second)
	snapshot := metrics.Snapshot{
		CollectedAt:   collectedAt,
		Platform:      "darwin",
		Arch:          "arm64",
		UptimeSeconds: 3600,
		MemoryTotal:   16 << 30,
		MemoryUsed:    8 << 30,
		MemoryPercent: 50,
		CPUPercent:    23.5,
		CPUCores:      8,
		Load1:         1.5,
		Load5:         1.2,
		Load15:        1.0,
		LoadAvailable: true,
		Disks: []metrics.DiskUsage{
			{Mount: "/", TotalBytes: 500 << 30, UsedBytes: 200 << 30, FreeBytes: 300 << 30, UsedPercent: 40},
		},
	}

	result := deviceMetricsFromSnapshot(snapshot)
	if result == nil {
		t.Fatal("有内存数据时不应返回 nil")
	}
	if !result.CollectedAt.Equal(collectedAt) {
		t.Fatalf("collected_at 不一致: %v", result.CollectedAt)
	}
	if result.Platform != "darwin" || result.Arch != "arm64" {
		t.Fatalf("平台信息不一致: %s/%s", result.Platform, result.Arch)
	}
	if result.MemoryTotal != 16<<30 || result.MemoryUsed != 8<<30 {
		t.Fatalf("内存数据不一致: %d/%d", result.MemoryTotal, result.MemoryUsed)
	}
	if result.MemoryPercent != 50 {
		t.Fatalf("内存占用率不一致: %v", result.MemoryPercent)
	}
	if result.CPUPercent != 23.5 {
		t.Fatalf("CPU 占用率不一致: %v", result.CPUPercent)
	}
	if !result.LoadAvailable {
		t.Fatal("负载可用标记应保留")
	}
	if len(result.Disks) != 1 {
		t.Fatalf("磁盘数量不一致: %d", len(result.Disks))
	}
	if result.Disks[0].Mount != "/" || result.Disks[0].UsedPercent != 40 {
		t.Fatalf("磁盘数据不一致: %+v", result.Disks[0])
	}
}

// TestDeviceMetricsFromSnapshotSkipsWhenEmpty 验证完全采不到数据时不携带指标字段。
func TestDeviceMetricsFromSnapshotSkipsWhenEmpty(t *testing.T) {
	if result := deviceMetricsFromSnapshot(metrics.Snapshot{}); result != nil {
		t.Fatalf("无数据时应返回 nil，实际 %+v", result)
	}
}

// TestDeviceMetricsFromSnapshotKeepsDisksWithoutMemory 验证仅有磁盘数据时也上报。
func TestDeviceMetricsFromSnapshotKeepsDisksWithoutMemory(t *testing.T) {
	result := deviceMetricsFromSnapshot(metrics.Snapshot{
		CollectedAt: time.Now().UTC(),
		Disks: []metrics.DiskUsage{
			{Mount: "C:", TotalBytes: 100, UsedBytes: 50, FreeBytes: 50, UsedPercent: 50},
		},
	})
	if result == nil {
		t.Fatal("有磁盘数据时不应返回 nil")
	}
	if len(result.Disks) != 1 {
		t.Fatalf("磁盘数量不一致: %d", len(result.Disks))
	}
}
