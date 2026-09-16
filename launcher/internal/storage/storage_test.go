package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile 创建带指定大小的文件，用于构造可预期的占用数据。
func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
}

func categoryByKey(t *testing.T, usage Usage, key string) Category {
	t.Helper()
	for _, item := range usage.Categories {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("未找到类别 %s", key)
	return Category{}
}

func TestCollectReportsPerCategoryBytes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "caches", "npm", "a.bin"), 100)
	writeFile(t, filepath.Join(root, "caches", "pip", "b.bin"), 50)
	writeFile(t, filepath.Join(root, "versions", "1.15.96", "opencode"), 200)
	writeFile(t, filepath.Join(root, "versions", "1.15.97", "opencode"), 300)
	writeFile(t, filepath.Join(root, "logs", "launcher.log"), 25)
	writeFile(t, filepath.Join(root, "runtimes", "node", "node"), 400)

	usage, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect 失败: %v", err)
	}

	if got := categoryByKey(t, usage, "caches").Bytes; got != 150 {
		t.Errorf("caches 期望 150，实际 %d", got)
	}
	if got := categoryByKey(t, usage, "versions").Bytes; got != 500 {
		t.Errorf("versions 期望 500，实际 %d", got)
	}
	if got := categoryByKey(t, usage, "logs").Bytes; got != 25 {
		t.Errorf("logs 期望 25，实际 %d", got)
	}
	if got := categoryByKey(t, usage, "runtimes").Bytes; got != 400 {
		t.Errorf("runtimes 期望 400，实际 %d", got)
	}
	if usage.TotalBytes != 1075 {
		t.Errorf("总量期望 1075，实际 %d", usage.TotalBytes)
	}
}

func TestCollectMarksClearableCategories(t *testing.T) {
	root := t.TempDir()
	usage, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect 失败: %v", err)
	}
	clearable := map[string]bool{}
	for _, item := range usage.Categories {
		clearable[item.Key] = item.Clearable
	}
	for _, key := range []string{"caches", "versions", "downloads", "logs"} {
		if !clearable[key] {
			t.Errorf("类别 %s 应可清理", key)
		}
	}
	// 运行时与用户数据绝不能出现在可清理集合里。
	for _, key := range []string{"runtimes", "mcps", "agents", "bin"} {
		if clearable[key] {
			t.Errorf("类别 %s 不应可清理", key)
		}
	}
}

func TestCollectIgnoresSymlinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "versions", "1.15.97", "opencode"), 300)
	if err := os.Symlink(filepath.Join(root, "versions", "1.15.97"), filepath.Join(root, "current")); err != nil {
		t.Skipf("当前平台不支持符号链接: %v", err)
	}

	usage, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect 失败: %v", err)
	}
	// 软链指向的 300 字节只应被 versions 计一次，不应出现在「其他」里重复累加。
	if usage.TotalBytes != 300 {
		t.Errorf("总量期望 300（软链不重复计数），实际 %d", usage.TotalBytes)
	}
}

func TestClearRemovesCacheContentsButKeepsDirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "caches", "npm", "a.bin"), 100)
	writeFile(t, filepath.Join(root, "logs", "launcher.log"), 40)
	writeFile(t, filepath.Join(root, "runtimes", "node", "node"), 400)

	result, err := Clear(root, []string{"caches", "logs"})
	if err != nil {
		t.Fatalf("Clear 失败: %v", err)
	}
	if result.FreedBytes != 140 {
		t.Errorf("释放量期望 140，实际 %d", result.FreedBytes)
	}
	// 目录本身保留，便于后续写入。
	if info, err := os.Stat(filepath.Join(root, "caches")); err != nil || !info.IsDir() {
		t.Errorf("caches 目录应保留: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "caches")); len(entries) != 0 {
		t.Errorf("caches 应已清空，仍有 %d 项", len(entries))
	}
	// 不可清理的运行时必须原样保留。
	if got := DirSize(filepath.Join(root, "runtimes")); got != 400 {
		t.Errorf("runtimes 不应被清理，期望 400，实际 %d", got)
	}
}

func TestClearRejectsProtectedCategories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "runtimes", "node", "node"), 400)
	writeFile(t, filepath.Join(root, "agents", "agent.json"), 10)

	result, err := Clear(root, []string{"runtimes", "agents"})
	if err != nil {
		t.Fatalf("Clear 失败: %v", err)
	}
	if result.FreedBytes != 0 {
		t.Errorf("受保护类别不应释放空间，实际 %d", result.FreedBytes)
	}
	if got := DirSize(filepath.Join(root, "runtimes")); got != 400 {
		t.Errorf("runtimes 必须保留，实际 %d", got)
	}
	if got := DirSize(filepath.Join(root, "agents")); got != 10 {
		t.Errorf("agents 必须保留，实际 %d", got)
	}
	if len(result.Skipped) == 0 {
		t.Error("应记录被跳过的受保护类别")
	}
}

func TestClearVersionsKeepsCurrentAndLocal(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "versions", "1.15.97", "opencode"), 300)
	writeFile(t, filepath.Join(root, "versions", "1.15.96", "opencode"), 200)
	writeFile(t, filepath.Join(root, "versions", "1.15.67", "opencode"), 150)
	writeFile(t, filepath.Join(root, "versions", "local", "opencode"), 250)
	if err := os.WriteFile(filepath.Join(root, "current-version"), []byte("1.15.97\n"), 0o644); err != nil {
		t.Fatalf("写入 current-version 失败: %v", err)
	}

	result, err := Clear(root, []string{"versions"})
	if err != nil {
		t.Fatalf("Clear 失败: %v", err)
	}
	if result.FreedBytes != 350 {
		t.Errorf("释放量期望 350（1.15.96 + 1.15.67），实际 %d", result.FreedBytes)
	}
	for _, keep := range []string{"1.15.97", "local"} {
		if _, err := os.Stat(filepath.Join(root, "versions", keep)); err != nil {
			t.Errorf("版本 %s 应保留: %v", keep, err)
		}
	}
	for _, gone := range []string{"1.15.96", "1.15.67"} {
		if _, err := os.Stat(filepath.Join(root, "versions", gone)); !os.IsNotExist(err) {
			t.Errorf("版本 %s 应被删除，err=%v", gone, err)
		}
	}
}

func TestClearVersionsKeepsSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "versions", "0.0.0-main-202604171457", "opencode"), 120)
	writeFile(t, filepath.Join(root, "versions", "1.15.96", "opencode"), 200)
	if err := os.Symlink(filepath.Join(root, "versions", "0.0.0-main-202604171457"), filepath.Join(root, "current")); err != nil {
		t.Skipf("当前平台不支持符号链接: %v", err)
	}
	// current-version 指向另一个版本时，软链目标同样必须保留。
	if err := os.WriteFile(filepath.Join(root, "current-version"), []byte("1.15.97"), 0o644); err != nil {
		t.Fatalf("写入 current-version 失败: %v", err)
	}

	if _, err := Clear(root, []string{"versions"}); err != nil {
		t.Fatalf("Clear 失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "0.0.0-main-202604171457")); err != nil {
		t.Errorf("软链目标应保留: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "1.15.96")); !os.IsNotExist(err) {
		t.Errorf("旧版本应被删除，err=%v", err)
	}
}

func TestClearAllWhenKeysEmpty(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "caches", "npm", "a.bin"), 100)
	writeFile(t, filepath.Join(root, "downloads", "pkg.zip"), 60)
	writeFile(t, filepath.Join(root, "bin", "launcher"), 500)

	result, err := Clear(root, nil)
	if err != nil {
		t.Fatalf("Clear 失败: %v", err)
	}
	if result.FreedBytes != 160 {
		t.Errorf("释放量期望 160，实际 %d", result.FreedBytes)
	}
	if got := DirSize(filepath.Join(root, "bin")); got != 500 {
		t.Errorf("bin 必须保留，实际 %d", got)
	}
}
