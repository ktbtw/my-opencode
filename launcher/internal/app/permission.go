package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"launcher/internal/backgroundcmd"
	"launcher/internal/macapp"
	"launcher/internal/model"
)

func (s *service) DirectoryPermission(input model.DirectoryPermissionInput) (model.DirectoryPermissionResult, error) {
	path, err := s.permissionTargetPath(input)
	if err != nil {
		return model.DirectoryPermissionResult{Platform: runtime.GOOS, Error: err.Error()}, err
	}
	result := checkDirectoryPermission(path)
	if input.Request && !result.Accessible {
		if openErr := s.openDirectoryPermissionSettings(input, path); openErr != nil {
			result.Error = firstNonEmpty(result.Error, openErr.Error())
			result.Message = result.Message + "；打开系统授权入口失败：" + openErr.Error()
		} else {
			result.Action = "opened_settings"
		}
	}
	return result, nil
}

func (s *service) permissionTargetPath(input model.DirectoryPermissionInput) (string, error) {
	if path := strings.TrimSpace(input.Path); path != "" {
		return filepath.Clean(path), nil
	}
	agentID := strings.TrimSpace(input.AgentID)
	if agentID == "" {
		return "", errors.New("缺少 agent_id 或 path")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, agent := range s.agents {
		if agent.AgentID == agentID {
			return filepath.Clean(agent.ProjectDir), nil
		}
	}
	return "", errors.New("未找到对应 agent")
}

func checkDirectoryPermission(path string) model.DirectoryPermissionResult {
	type permissionCheck struct {
		result model.DirectoryPermissionResult
	}
	done := make(chan permissionCheck, 1)
	go func() {
		done <- permissionCheck{result: checkDirectoryPermissionDirect(path)}
	}()
	select {
	case item := <-done:
		return item.result
	case <-time.After(3 * time.Second):
		return model.DirectoryPermissionResult{
			Platform:           runtime.GOOS,
			Path:               path,
			Accessible:         false,
			RequiresUserAction: true,
			Error:              "目录权限检查超时",
			Message:            permissionTimeoutMessage(path),
		}
	}
}

func checkDirectoryPermissionDirect(path string) model.DirectoryPermissionResult {
	result := model.DirectoryPermissionResult{
		Platform: runtime.GOOS,
		Path:     path,
	}
	info, err := os.Stat(path)
	if err != nil {
		result.Accessible = false
		result.RequiresUserAction = isPermissionError(err)
		result.Error = friendlyPathError(path, err).Error()
		result.Message = permissionFixMessage(path, err)
		return result
	}
	if !info.IsDir() {
		result.Accessible = false
		result.Error = "路径不是目录"
		result.Message = "请选择一个项目目录后重试。"
		return result
	}
	dir, err := os.Open(path)
	if err != nil {
		result.Accessible = false
		result.RequiresUserAction = isPermissionError(err)
		result.Error = friendlyPathError(path, err).Error()
		result.Message = permissionFixMessage(path, err)
		return result
	}
	defer dir.Close()
	entries, err := dir.ReadDir(1)
	if err != nil && !errors.Is(err, io.EOF) {
		result.Accessible = false
		result.RequiresUserAction = isPermissionError(err)
		result.Error = friendlyPathError(path, err).Error()
		result.Message = permissionFixMessage(path, err)
		return result
	}
	result.Accessible = true
	result.Message = fmt.Sprintf("目录权限正常，已完成快速访问检查，样本条目 %d 个。", len(entries))
	return result
}

func friendlyPathError(path string, err error) error {
	if err == nil {
		return nil
	}
	if isPermissionError(err) {
		return errors.New(permissionDeniedMessage(path))
	}
	if os.IsNotExist(err) {
		return errors.New("项目目录不存在")
	}
	return err
}

func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return os.IsPermission(err) || strings.Contains(text, "operation not permitted") || strings.Contains(text, "access is denied")
}

func permissionDeniedMessage(path string) string {
	if runtime.GOOS == "darwin" {
		return "macOS 阻止访问目录：" + path
	}
	if runtime.GOOS == "windows" {
		return "Windows 阻止访问目录：" + path
	}
	return "系统阻止访问目录：" + path
}

func permissionFixMessage(path string, err error) string {
	if !isPermissionError(err) {
		return friendlyPathError(path, err).Error()
	}
	switch runtime.GOOS {
	case "darwin":
		return "请在系统设置的隐私与安全中给 /Applications/码控.app 开启完整磁盘访问。如果列表里没有码控，请点击加号选择 Finder 中高亮的码控.app；授权后重启对应 agent。"
	case "windows":
		return "请检查该目录的安全权限或 Windows 安全中心的受控文件夹访问，授权后重启对应 agent。"
	default:
		return "请检查该目录的系统权限，授权后重启对应 agent。"
	}
}

func permissionTimeoutMessage(path string) string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS 未及时返回目录权限检查结果，通常是完整磁盘访问未授予 /Applications/码控.app，或授权仍绑定到旧版本。请在权限列表中重新添加码控.app，授权后重启对应 agent。"
	case "windows":
		return "Windows 未及时返回目录权限检查结果，请检查目录安全权限或受控文件夹访问，授权后重启对应 agent。"
	default:
		return "系统未及时返回目录权限检查结果，请检查该目录权限，授权后重启对应 agent。"
	}
}

func (s *service) openDirectoryPermissionSettings(input model.DirectoryPermissionInput, path string) error {
	switch runtime.GOOS {
	case "darwin":
		settingsErr := exec.Command("open", "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles").Start()
		_ = macapp.RevealPermissionTarget()
		return settingsErr
	case "windows":
		cmd := exec.Command("cmd", "/c", "start", "", "windowsdefender://RansomwareProtection")
		backgroundcmd.Configure(cmd)
		if err := cmd.Start(); err == nil {
			return nil
		}
		return exec.Command("explorer.exe", filepath.Clean(path)).Start()
	default:
		return errors.New("当前系统暂不支持自动打开权限设置")
	}
}
