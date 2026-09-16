// Package metrics 采集设备运行状态（内存、磁盘、CPU、负载、运行时长），
// 供心跳上报给服务端，让手机端能观察设备运行情况。
//
// 采集遵循"尽力而为"：任何单项失败只影响对应字段，不阻断心跳本身。
package metrics

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

// DiskUsage 单个磁盘分区的占用情况。
type DiskUsage struct {
	Mount       string  `json:"mount"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

// Snapshot 是一次完整的指标采集结果。
// 字段均可选：采集失败时留零值，由 omitempty 决定是否上报。
type Snapshot struct {
	CollectedAt   time.Time   `json:"collected_at"`
	Platform      string      `json:"platform,omitempty"`
	Arch          string      `json:"architecture,omitempty"`
	UptimeSeconds uint64      `json:"uptime_seconds,omitempty"`
	MemoryTotal   uint64      `json:"memory_total_bytes,omitempty"`
	MemoryUsed    uint64      `json:"memory_used_bytes,omitempty"`
	MemoryPercent float64     `json:"memory_used_percent,omitempty"`
	CPUPercent    float64     `json:"cpu_used_percent,omitempty"`
	CPUCores      int         `json:"cpu_cores,omitempty"`
	Load1         float64     `json:"load_1,omitempty"`
	Load5         float64     `json:"load_5,omitempty"`
	Load15        float64     `json:"load_15,omitempty"`
	LoadAvailable bool        `json:"load_available,omitempty"`
	Disks         []DiskUsage `json:"disks,omitempty"`
}

// maxDisks 限制上报的分区数量，避免磁盘多的机器撑大心跳包。
// 按占用率降序保留前 N 个，占用率低的次要分区对用户意义有限。
const maxDisks = 6

// systemMountPrefixes 是各平台的系统内部挂载前缀，这些不是用户关心的存储卷。
// macOS 上尤其多：/dev、/System/Volumes/* 等会挤占展示位，
// 让真正的数据卷被埋没。
var systemMountPrefixes = []string{
	"/System/Volumes/",
	"/private/var/vm",
	"/dev",
	"/proc",
	"/sys",
	"/run",
	"/snap",
	"/boot/efi",
}

// isSystemMount 判断挂载点是否属于系统内部卷。
func isSystemMount(mount string) bool {
	for _, prefix := range systemMountPrefixes {
		if mount == prefix || strings.HasPrefix(mount, prefix) {
			return true
		}
	}
	// macOS 的 /System/Library/... 目录下也会挂载资源卷。
	if strings.HasPrefix(mount, "/System/Library/") {
		return true
	}
	// 测试/沙箱产生的临时挂载点，与用户存储无关。
	if strings.HasPrefix(mount, "/private/tmp/") ||
		strings.HasPrefix(mount, "/private/var/folders/") {
		return true
	}
	return false
}

// collectDisks 返回占用率最高的若干分区。
//
// macOS 上同一物理卷会以多个路径出现（如 `/` 与 `/System/Volumes/Data`、
// `/Volumes/X` 与合成挂载点）。此处按设备标识去重，并优先保留最短的
// 挂载路径——那通常是对用户最有意义的名称。
func collectDisks() []DiskUsage {
	partitions, err := disk.Partitions(false)
	if err != nil {
		return nil
	}
	byDevice := make(map[string]DiskUsage, len(partitions))
	anonymous := make([]DiskUsage, 0, 4)
	for _, partition := range partitions {
		mount := partition.Mountpoint
		if mount == "" || isSystemMount(mount) {
			continue
		}
		usage, err := disk.Usage(mount)
		if err != nil || usage.Total == 0 {
			continue
		}
		entry := DiskUsage{
			Mount:       mount,
			TotalBytes:  usage.Total,
			UsedBytes:   usage.Used,
			FreeBytes:   usage.Free,
			UsedPercent: round2(usage.UsedPercent),
		}
		device := strings.TrimSpace(partition.Device)
		if device == "" {
			anonymous = append(anonymous, entry)
			continue
		}
		existing, ok := byDevice[device]
		if !ok || len(mount) < len(existing.Mount) {
			byDevice[device] = entry
		}
	}

	usages := make([]DiskUsage, 0, len(byDevice)+len(anonymous))
	for _, entry := range byDevice {
		usages = append(usages, entry)
	}
	usages = append(usages, anonymous...)

	// 占用率降序，占用高的分区更值得关注。
	sort.SliceStable(usages, func(i, j int) bool {
		return usages[i].UsedPercent > usages[j].UsedPercent
	})

	// 兜底去重：同一卷可能以不同设备名暴露为多个挂载点，
	// 此时容量数据完全相同。保留路径最短的那个（通常是主挂载名）。
	usages = dedupeByCapacity(usages)

	if len(usages) > maxDisks {
		usages = usages[:maxDisks]
	}
	return usages
}

// dedupeByCapacity 按容量签名去除重复卷，保留挂载路径最短者。
func dedupeByCapacity(usages []DiskUsage) []DiskUsage {
	seen := make(map[string]int, len(usages))
	result := make([]DiskUsage, 0, len(usages))
	for _, entry := range usages {
		key := fmt.Sprintf("%d/%d", entry.TotalBytes, entry.UsedBytes)
		index, ok := seen[key]
		if !ok {
			seen[key] = len(result)
			result = append(result, entry)
			continue
		}
		if len(entry.Mount) < len(result[index].Mount) {
			result[index] = entry
		}
	}
	return result
}

// Collector 保存 CPU 采样所需的上一轮累计时间，用于计算增量占用率。
// 非并发安全，调用方需保证串行调用 Collect。
type Collector struct {
	lastBusy float64
	lastAll  float64
	seen     bool
}

// Collect 采集一次指标快照。
func (c *Collector) Collect() Snapshot {
	snapshot := Snapshot{
		CollectedAt: time.Now().UTC(),
		Platform:    runtime.GOOS,
		Arch:        runtime.GOARCH,
		CPUCores:    runtime.NumCPU(),
	}

	if uptime, err := host.Uptime(); err == nil {
		snapshot.UptimeSeconds = uptime
	}

	if vm, err := mem.VirtualMemory(); err == nil && vm.Total > 0 {
		snapshot.MemoryTotal = vm.Total
		snapshot.MemoryUsed = vm.Used
		snapshot.MemoryPercent = round2(vm.UsedPercent)
	}

	if times, err := cpu.Times(false); err == nil && len(times) > 0 {
		t := times[0]
		busy := t.User + t.System + t.Nice + t.Iowait + t.Irq + t.Softirq + t.Steal
		all := busy + t.Idle
		// 首次采样没有基线，只能记录累计值，占用率留空等下一轮。
		if c.seen && all > c.lastAll {
			deltaAll := all - c.lastAll
			deltaBusy := busy - c.lastBusy
			if deltaAll > 0 {
				snapshot.CPUPercent = round2(clampPercent(deltaBusy / deltaAll * 100))
			}
		}
		c.lastBusy = busy
		c.lastAll = all
		c.seen = true
	}

	// Windows 没有 load average 语义，gopsutil 会返回 0，此处显式标记不可用。
	if avg, err := load.Avg(); err == nil && (avg.Load1 > 0 || avg.Load5 > 0 || avg.Load15 > 0) {
		snapshot.Load1 = round2(avg.Load1)
		snapshot.Load5 = round2(avg.Load5)
		snapshot.Load15 = round2(avg.Load15)
		snapshot.LoadAvailable = true
	}

	snapshot.Disks = collectDisks()
	return snapshot
}

func round2(value float64) float64 {
	return float64(int64(value*100+0.5)) / 100
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
