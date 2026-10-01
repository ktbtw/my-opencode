package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"launcher/internal/model"
)

// RuntimeStatus 查询单个受管运行时组件在当前设备上的安装情况。
func (s *service) RuntimeStatus(runtimeID string) (model.DeviceRuntimeStatus, error) {
	id := strings.TrimSpace(runtimeID)
	status := model.DeviceRuntimeStatus{RuntimeID: id}
	if id == "" {
		return status, errors.New("缺少 runtime_id")
	}
	manifest, ok := s.bestRuntimeManifest(id, "")
	if !ok {
		return status, nil
	}
	status.Installed = true
	status.Version = manifest.Version
	status.Path = manifest.InstallDir
	status.Executable = runtimeExecutablePath(manifest)
	return status, nil
}

// InstallRuntimes 只安装 payload.Runtimes 列出的运行时，不做语义 Agent 切换。
// 进度通过 reporter 上报，与语义 Agent 预检共用同一套状态结构。
func (s *service) InstallRuntimes(
	payload model.RuntimePreflightStartPayload,
	reporter func(model.RuntimePreflightStatusPayload) error,
) error {
	if reporter == nil {
		return errors.New("运行时安装缺少状态上报器")
	}
	payload.RuntimeOnly = true
	payload.LauncherAgentID = strings.TrimSpace(payload.LauncherAgentID)
	payload.SemanticAgentID = ""
	payload.SemanticAgent = nil
	if payload.JobID == "" {
		return errors.New("运行时安装缺少 job_id")
	}
	// 单独安装运行时是设备级操作，不涉及任何项目 Agent：这里下发的
	// LauncherAgentID 是 launcher 设备标识（launcher:m_xxx），不是
	// agents.json 里的项目 Agent，因此不能按项目 Agent 校验，
	// 否则每次安装都会直接报「agent not found」。
	if payload.LauncherAgentID == "" {
		return errors.New("运行时安装缺少 launcher 标识")
	}
	if len(payload.Runtimes) == 0 {
		return errors.New("运行时安装缺少运行时清单")
	}
	if !s.semanticPreflightMu.TryLock() {
		s.semanticPreflightMu.Lock()
	}
	defer s.semanticPreflightMu.Unlock()
	runner := runtimePreflightRunner{
		service:     s,
		payload:     payload,
		reporter:    reporter,
		items:       buildPreflightItems(payload),
		runtimeOnly: true,
	}
	runner.startReporter()
	defer runner.stopReporter()
	return runner.run()
}

// runtimeExecutablePath 在已安装的运行时目录里找出可执行文件，
// 供上层展示和调试定位使用。
func runtimeExecutablePath(manifest installedRuntimeManifest) string {
	binDirs := runtimeManifestBinDirs(manifest)
	names := manifest.ExecutableNames
	if len(binDirs) == 0 || len(names) == 0 {
		return ""
	}
	for _, dir := range binDirs {
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			candidate := filepath.Join(dir, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
			if info, err := os.Stat(candidate + ".exe"); err == nil && !info.IsDir() {
				return candidate + ".exe"
			}
		}
	}
	return ""
}
