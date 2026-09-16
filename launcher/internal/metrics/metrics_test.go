package metrics

import "testing"

// TestCollectReturnsHostFacts 验证采集在当前平台能拿到基础事实。
// 不断言具体数值（随机器变化），只断言"确实采到了合理数据"。
func TestCollectReturnsHostFacts(t *testing.T) {
	collector := &Collector{}
	snapshot := collector.Collect()

	if snapshot.CollectedAt.IsZero() {
		t.Fatal("collected_at 不应为空")
	}
	if snapshot.Platform == "" {
		t.Fatal("platform 不应为空")
	}
	if snapshot.Arch == "" {
		t.Fatal("architecture 不应为空")
	}
	if snapshot.CPUCores <= 0 {
		t.Fatalf("cpu_cores 应为正数，实际 %d", snapshot.CPUCores)
	}
	if snapshot.MemoryTotal == 0 {
		t.Fatal("内存总量应采集成功")
	}
	if snapshot.MemoryUsed > snapshot.MemoryTotal {
		t.Fatalf("已用内存(%d)不应超过总量(%d)", snapshot.MemoryUsed, snapshot.MemoryTotal)
	}
	if snapshot.MemoryPercent < 0 || snapshot.MemoryPercent > 100 {
		t.Fatalf("内存占用率越界: %v", snapshot.MemoryPercent)
	}
	if snapshot.UptimeSeconds == 0 {
		t.Fatal("运行时长应采集成功")
	}
	if len(snapshot.Disks) == 0 {
		t.Fatal("应至少采集到一个磁盘分区")
	}
	for _, disk := range snapshot.Disks {
		if disk.TotalBytes == 0 {
			t.Fatalf("分区 %s 总量为 0", disk.Mount)
		}
		if disk.UsedBytes > disk.TotalBytes {
			t.Fatalf("分区 %s 已用(%d)超过总量(%d)", disk.Mount, disk.UsedBytes, disk.TotalBytes)
		}
		if disk.UsedPercent < 0 || disk.UsedPercent > 100 {
			t.Fatalf("分区 %s 占用率越界: %v", disk.Mount, disk.UsedPercent)
		}
	}
}

// TestCPUPercentNeedsBaseline 验证 CPU 占用率需要两次采样。
// 首次没有基线，不应给出可能误导的数值。
func TestCPUPercentNeedsBaseline(t *testing.T) {
	collector := &Collector{}
	first := collector.Collect()
	if first.CPUPercent != 0 {
		t.Fatalf("首次采样不应给出 CPU 占用率，实际 %v", first.CPUPercent)
	}

	// 制造一点 CPU 活动，让累计时间前进。
	busyWork()

	second := collector.Collect()
	if !collector.seen {
		t.Fatal("采集后应记录基线")
	}
	if second.CPUPercent < 0 || second.CPUPercent > 100 {
		t.Fatalf("第二次 CPU 占用率越界: %v", second.CPUPercent)
	}
}

// TestDisksSortedByUsageDesc 验证分区按占用率降序，且数量受限。
func TestDisksSortedByUsageDesc(t *testing.T) {
	collector := &Collector{}
	snapshot := collector.Collect()
	if len(snapshot.Disks) > maxDisks {
		t.Fatalf("分区数量 %d 超过上限 %d", len(snapshot.Disks), maxDisks)
	}
	for i := 1; i < len(snapshot.Disks); i++ {
		if snapshot.Disks[i-1].UsedPercent < snapshot.Disks[i].UsedPercent {
			t.Fatalf("分区未按占用率降序: %v", snapshot.Disks)
		}
	}
	mounts := make(map[string]struct{}, len(snapshot.Disks))
	for _, disk := range snapshot.Disks {
		if _, ok := mounts[disk.Mount]; ok {
			t.Fatalf("挂载点重复: %s", disk.Mount)
		}
		mounts[disk.Mount] = struct{}{}
	}
}

// busyWork 制造少量可测量的 CPU 活动，避免编译器优化掉整个循环。
var busyWorkSink int

func busyWork() {
	total := 0
	for i := 0; i < 2_000_000; i++ {
		total += i % 7
	}
	busyWorkSink = total
}

// TestIsSystemMountFiltersPlatformInternals 验证 macOS 等平台的系统内部卷被排除。
// 这些卷不是用户关心的存储，混进来会挤掉真正的数据卷。
func TestIsSystemMountFiltersPlatformInternals(t *testing.T) {
	systemMounts := []string{
		"/System/Volumes/VM",
		"/System/Volumes/Preboot",
		"/System/Volumes/Update",
		"/System/Library/AssetsV2/foo",
		"/dev",
		"/proc",
		"/sys",
		"/run",
		"/snap/core20",
		"/boot/efi",
	}
	for _, mount := range systemMounts {
		if !isSystemMount(mount) {
			t.Fatalf("系统内部卷应被过滤: %s", mount)
		}
	}

	userMounts := []string{
		"/",
		"/Users",
		"/home",
		"/data",
		"E:",
		"/Volumes/MyDrive",
		"/mnt/storage",
	}
	for _, mount := range userMounts {
		if isSystemMount(mount) {
			t.Fatalf("用户数据卷不应被过滤: %s", mount)
		}
	}
}

// TestCollectDisksExcludesSystemVolumes 验证真实采集结果不包含系统内部卷。
func TestCollectDisksExcludesSystemVolumes(t *testing.T) {
	disks := collectDisks()
	for _, item := range disks {
		if isSystemMount(item.Mount) {
			t.Fatalf("采集结果不应包含系统内部卷: %s", item.Mount)
		}
	}
}
