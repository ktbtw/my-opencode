// Package storage 统计 launcher 运行目录的磁盘占用，并支持安全清理可再生成的缓存。
//
// 目录体积只能按需统计：运行目录常见上万个文件、总量可达数 GB，
// 若放进每 15 秒的心跳里会造成持续的磁盘 IO。
package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Category 是一类磁盘占用的统计结果。
type Category struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Detail 是给界面看的补充说明，例如“保留当前版本 1.15.97”。
	Detail string `json:"detail,omitempty"`
	Bytes  uint64 `json:"bytes"`
	// Clearable 表示该类别是否允许被清理。
	// 运行时组件、Agent 数据与程序文件一律不可清理。
	Clearable bool `json:"clearable"`
}

// Usage 是一次完整的占用统计。
type Usage struct {
	Root       string     `json:"root"`
	TotalBytes uint64     `json:"total_bytes"`
	Categories []Category `json:"categories"`
}

// ClearResult 是清理结果。
type ClearResult struct {
	FreedBytes uint64     `json:"freed_bytes"`
	Categories []Category `json:"categories"`
	// Skipped 记录被跳过的条目及原因，便于排查“为什么没释放”。
	Skipped []string `json:"skipped,omitempty"`
}

// categorySpec 描述一个受统计的目录。
type categorySpec struct {
	Key       string
	Label     string
	Dir       string
	Clearable bool
}

// categories 的顺序即界面展示顺序。
// 目录名取自 launcher 运行目录的真实布局（见 launcher/internal/paths）。
var categories = []categorySpec{
	{Key: "caches", Label: "依赖缓存", Dir: "caches", Clearable: true},
	{Key: "versions", Label: "历史版本", Dir: "versions", Clearable: true},
	{Key: "downloads", Label: "下载安装包", Dir: "downloads", Clearable: true},
	{Key: "logs", Label: "运行日志", Dir: "logs", Clearable: true},
	{Key: "runtimes", Label: "运行时组件", Dir: "runtimes"},
	{Key: "mcps", Label: "MCP 组件", Dir: "mcps"},
	{Key: "agents", Label: "Agent 数据", Dir: "agents"},
	{Key: "bin", Label: "程序文件", Dir: "bin"},
}

// 未纳入统计的其余子目录统一归入「其他」，保证总量与实际占用一致。
const miscKey = "misc"

// Collect 统计运行目录下各类别的占用。
//
// 统计失败（例如目录不存在）不会中断整体流程：该类别按 0 处理。
// 符号链接不计入体积，避免 current 之类的软链造成重复计算。
func Collect(root string) (Usage, error) {
	if strings.TrimSpace(root) == "" {
		return Usage{}, errors.New("运行目录为空")
	}
	info, err := os.Stat(root)
	if err != nil {
		return Usage{}, fmt.Errorf("运行目录不可用: %w", err)
	}
	if !info.IsDir() {
		return Usage{}, errors.New("运行目录不是目录")
	}

	usage := Usage{Root: root, Categories: make([]Category, 0, len(categories)+1)}
	known := make(map[string]bool, len(categories))

	for _, spec := range categories {
		known[spec.Dir] = true
		item := Category{
			Key:       spec.Key,
			Label:     spec.Label,
			Clearable: spec.Clearable,
		}
		if spec.Key == "versions" {
			item.Detail = versionsDetail(root)
		}
		item.Bytes = DirSize(filepath.Join(root, spec.Dir))
		usage.Categories = append(usage.Categories, item)
		usage.TotalBytes += item.Bytes
	}

	// 其余同级目录归入「其他」。
	entries, err := os.ReadDir(root)
	if err == nil {
		var misc uint64
		for _, entry := range entries {
			if !entry.IsDir() || known[entry.Name()] {
				continue
			}
			misc += DirSize(filepath.Join(root, entry.Name()))
		}
		if misc > 0 {
			usage.Categories = append(usage.Categories, Category{
				Key:   miscKey,
				Label: "其他",
				Bytes: misc,
			})
			usage.TotalBytes += misc
		}
	}

	return usage, nil
}

// DirSize 递归统计目录下所有普通文件的大小。
// 符号链接与不可读目录会被跳过，不计入结果。
func DirSize(dir string) uint64 {
	var total uint64
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// 单个条目不可读时跳过，不影响其余统计。
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		// 不跟随符号链接，避免重复计算与跨目录逃逸。
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > 0 {
			total += uint64(info.Size())
		}
		return nil
	})
	return total
}

// clearableKeys 校验并归一化待清理类别，忽略未知或不可清理的类别。
func clearableKeys(keys []string) ([]categorySpec, []string) {
	wanted := make(map[string]bool, len(keys))
	for _, key := range keys {
		wanted[strings.TrimSpace(key)] = true
	}
	selected := make([]categorySpec, 0, len(keys))
	var skipped []string
	for _, spec := range categories {
		if !spec.Clearable || !wanted[spec.Key] {
			continue
		}
		selected = append(selected, spec)
	}
	for key := range wanted {
		if key == "" {
			continue
		}
		known := false
		clearable := false
		for _, spec := range categories {
			if spec.Key == key {
				known = true
				clearable = spec.Clearable
				break
			}
		}
		if !known {
			skipped = append(skipped, fmt.Sprintf("未知类别 %s", key))
		} else if !clearable {
			skipped = append(skipped, fmt.Sprintf("类别 %s 不允许清理", key))
		}
	}
	return selected, skipped
}

// Clear 清理指定的可清理类别，返回实际释放的字节数。
//
// keys 为空时清理全部可清理类别。
// versions 目录只删除非当前版本，当前版本由 current-version 指定，必须保留。
func Clear(root string, keys []string) (ClearResult, error) {
	if strings.TrimSpace(root) == "" {
		return ClearResult{}, errors.New("运行目录为空")
	}
	if _, err := os.Stat(root); err != nil {
		return ClearResult{}, fmt.Errorf("运行目录不可用: %w", err)
	}

	selected, skipped := clearableKeys(keys)
	if len(keys) == 0 {
		selected = nil
		for _, spec := range categories {
			if spec.Clearable {
				selected = append(selected, spec)
			}
		}
	}
	if len(selected) == 0 && len(skipped) == 0 {
		return ClearResult{}, errors.New("没有可清理的类别")
	}

	result := ClearResult{
		Skipped:    skipped,
		Categories: make([]Category, 0, len(selected)),
	}

	for _, spec := range selected {
		dir := filepath.Join(root, spec.Dir)
		before := DirSize(dir)
		var freed uint64
		var errText string

		if spec.Key == "versions" {
			freed, errText = clearVersions(root, dir)
		} else {
			freed, errText = clearDirContents(dir)
		}

		if errText != "" {
			result.Skipped = append(result.Skipped, fmt.Sprintf("%s: %s", spec.Label, errText))
		}
		// 释放量以实测为准：清理后再统计一次，避免估算偏差。
		after := DirSize(dir)
		delta := uint64(0)
		if before > after {
			delta = before - after
		}
		if delta == 0 {
			delta = freed
		}
		result.FreedBytes += delta
		result.Categories = append(result.Categories, Category{
			Key:       spec.Key,
			Label:     spec.Label,
			Bytes:     after,
			Clearable: true,
		})
	}

	return result, nil
}

// clearDirContents 清空目录内容但保留目录本身，返回释放量。
func clearDirContents(dir string) (uint64, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, ""
		}
		return 0, err.Error()
	}
	var freed uint64
	var firstErr string
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		size := pathSize(path)
		if err := os.RemoveAll(path); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		freed += size
	}
	return freed, firstErr
}

// clearVersions 删除 versions 目录下除当前版本与本地构建外的所有版本。
// 本地构建目录 local 保留，因为它无法重新下载。
func clearVersions(root, dir string) (uint64, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, ""
		}
		return 0, err.Error()
	}
	keep := map[string]bool{"local": true}
	if current, err := os.ReadFile(filepath.Join(root, "current-version")); err == nil {
		if name := strings.TrimSpace(string(current)); name != "" {
			keep[name] = true
		}
	}
	// current 是软链，其目标也要保留。
	if target, err := filepath.EvalSymlinks(filepath.Join(root, "current")); err == nil && target != "" {
		keep[filepath.Base(target)] = true
	}

	var freed uint64
	var firstErr string
	for _, entry := range entries {
		name := entry.Name()
		if keep[name] {
			continue
		}
		path := filepath.Join(dir, name)
		size := pathSize(path)
		if err := os.RemoveAll(path); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		freed += size
	}
	return freed, firstErr
}

// pathSize 统计单个路径的体积：目录递归求和，普通文件取自身大小。
func pathSize(path string) uint64 {
	info, err := os.Lstat(path)
	if err != nil {
		return 0
	}
	if info.IsDir() {
		return DirSize(path)
	}
	if info.Mode().IsRegular() && info.Size() > 0 {
		return uint64(info.Size())
	}
	return 0
}

// versionsDetail 描述版本目录的保留策略，供界面提示。
func versionsDetail(root string) string {
	if current, err := os.ReadFile(filepath.Join(root, "current-version")); err == nil {
		if name := strings.TrimSpace(string(current)); name != "" {
			return "保留当前版本 " + name
		}
	}
	return "保留当前版本"
}
