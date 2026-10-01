package app

import (
	"os"
	"path/filepath"
	"testing"

	"launcher/internal/config"
)

func ffmpegExecutableNameForTest() string {
	if currentRuntimePlatform() == "windows" {
		return "ffmpeg.exe"
	}
	return "ffmpeg"
}

// RuntimeStatus 应报告已安装的运行时（含可执行文件路径），
// 未安装时返回 installed=false 而不是报错。
func TestRuntimeStatusReportsInstalledRuntime(t *testing.T) {
	runtimeDir := t.TempDir()
	installDir := filepath.Join(runtimeDir, "runtimes", "ffmpeg", "6.0")
	binDir := filepath.Join(installDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	executableName := ffmpegExecutableNameForTest()
	executable := filepath.Join(binDir, executableName)
	if err := os.WriteFile(executable, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeRuntimeManifest(runtimeDir, installedRuntimeManifest{
		RuntimeID:       "ffmpeg",
		VersionID:       "ffmpeg-6.0",
		Version:         "6.0",
		Platform:        currentRuntimePlatform(),
		Arch:            currentRuntimeArch(),
		InstallDir:      installDir,
		BinPaths:        []string{"bin"},
		ExecutableNames: []string{"ffmpeg", "ffmpeg.exe"},
	}); err != nil {
		t.Fatal(err)
	}

	svc := &service{cfg: config.Config{RuntimeDir: runtimeDir}}
	status, err := svc.RuntimeStatus("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Installed {
		t.Fatal("已安装的运行时应报告 installed=true")
	}
	if status.Version != "6.0" {
		t.Fatalf("版本应为 6.0，实际 %q", status.Version)
	}
	if status.Executable != executable {
		t.Fatalf("可执行文件路径应为 %q，实际 %q", executable, status.Executable)
	}

	missing, err := svc.RuntimeStatus("not-installed")
	if err != nil {
		t.Fatal(err)
	}
	if missing.Installed {
		t.Fatal("未安装的运行时不应报告 installed=true")
	}

	if _, err := svc.RuntimeStatus("   "); err == nil {
		t.Fatal("空 runtime_id 应报错")
	}
}
