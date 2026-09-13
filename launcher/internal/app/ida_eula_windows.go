//go:build windows

package app

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const idaWindowsRegistryPath = `Software\Hex-Rays\IDA`

func prepareIDAEULAState(_ string, eulaKey string) (string, error) {
	eulaKey = strings.TrimSpace(eulaKey)
	if eulaKey == "" {
		return "", fmt.Errorf("IDA EULA 注册表项名称为空")
	}
	key, _, err := registry.CreateKey(
		registry.CURRENT_USER,
		idaWindowsRegistryPath,
		registry.SET_VALUE|registry.QUERY_VALUE,
	)
	if err != nil {
		return "", fmt.Errorf("打开 IDA 当前用户注册表失败: %w", err)
	}
	defer key.Close()
	if err := key.SetDWordValue(eulaKey, 1); err != nil {
		return "", fmt.Errorf("写入 IDA EULA 注册表项 %q 失败: %w", eulaKey, err)
	}
	value, _, err := key.GetIntegerValue(eulaKey)
	if err != nil {
		return "", fmt.Errorf("读取 IDA EULA 注册表项 %q 失败: %w", eulaKey, err)
	}
	if value != 1 {
		return "", fmt.Errorf("IDA EULA 注册表项 %q 验证失败: value=%d", eulaKey, value)
	}
	return fmt.Sprintf("IDA EULA registry state initialized: HKCU\\%s\\%s=1", idaWindowsRegistryPath, eulaKey), nil
}
