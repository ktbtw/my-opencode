package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"launcher/internal/model"
)

const mcpHomePlaceholder = "${MCP_HOME}"

func resolveMCPServerForRuntime(runtimeDir string, server model.DeviceMCPServerInfo) (model.DeviceMCPServerInfo, error) {
	server.Name = strings.TrimSpace(server.Name)
	if server.Name == "" {
		return server, fmt.Errorf("MCP 名称不能为空")
	}
	return resolveMCPServerForRuntimeID(runtimeDir, server.Name, server)
}

func resolveMCPServerForRuntimeID(runtimeDir string, mcpID string, server model.DeviceMCPServerInfo) (model.DeviceMCPServerInfo, error) {
	server.Name = strings.TrimSpace(server.Name)
	if server.Name == "" {
		return server, fmt.Errorf("MCP 名称不能为空")
	}
	mcpID = strings.TrimSpace(mcpID)
	if mcpID == "" {
		mcpID = server.Name
	}
	if server.LaunchBlockReason != "" && !server.LaunchReady {
		return server, fmt.Errorf("MCP %s 不可启动：%s", server.Name, server.LaunchBlockReason)
	}
	resolved := server
	resolved.Command = make([]string, 0, len(server.Command))
	for _, item := range server.Command {
		value, err := resolveMCPPathTemplate(runtimeDir, mcpID, item)
		if err != nil {
			return server, fmt.Errorf("MCP %s 启动命令未就绪：%w", server.Name, err)
		}
		resolved.Command = append(resolved.Command, value)
	}
	if strings.TrimSpace(server.URL) != "" {
		value, err := resolveMCPPathTemplate(runtimeDir, mcpID, server.URL)
		if err != nil {
			return server, fmt.Errorf("MCP %s URL 未就绪：%w", server.Name, err)
		}
		resolved.URL = value
	}
	resolved.Environment = map[string]string{}
	for key, value := range server.Environment {
		next, err := resolveMCPPathTemplate(runtimeDir, mcpID, value)
		if err != nil {
			return server, fmt.Errorf("MCP %s 环境变量 %s 未就绪：%w", server.Name, key, err)
		}
		resolved.Environment[key] = next
	}
	if len(resolved.Environment) == 0 {
		resolved.Environment = nil
	}
	resolved.Command = normalizeMCPRunCommand(resolved.Command)
	return resolved, nil
}

func normalizeMCPRunCommand(command []string) []string {
	if len(command) == 0 || !isJADXMCPRunCommand(command) {
		return command
	}
	out := make([]string, 0, len(command))
	skipNext := false
	for _, arg := range command {
		if skipNext {
			skipNext = false
			continue
		}
		switch strings.TrimSpace(arg) {
		case "--http":
			continue
		case "--host", "--port":
			skipNext = true
			continue
		}
		out = append(out, arg)
	}
	return out
}

func isJADXMCPRunCommand(command []string) bool {
	for _, arg := range command {
		base := filepath.Base(strings.TrimSpace(arg))
		if base == "jadx_mcp_server.py" || strings.Contains(arg, "jadx_mcp_server.py") {
			return true
		}
	}
	return false
}

func resolveMCPPathTemplate(runtimeDir string, mcpID string, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if containsUnresolvedMCPPlaceholder(value) {
		return "", fmt.Errorf("仍包含示例占位路径 %q", value)
	}
	root := mcpInstallRoot(runtimeDir, mcpID)
	value = strings.ReplaceAll(value, mcpHomePlaceholder, root)
	for {
		start := strings.Index(value, "${MCP_HOME:")
		if start < 0 {
			break
		}
		end := strings.Index(value[start:], "}")
		if end < 0 {
			return "", fmt.Errorf("MCP_HOME 模板未闭合")
		}
		end += start
		id := strings.TrimSpace(value[start+len("${MCP_HOME:") : end])
		if id == "" {
			return "", fmt.Errorf("MCP_HOME 模板缺少 MCP ID")
		}
		value = value[:start] + mcpInstallRoot(runtimeDir, id) + value[end+1:]
	}
	if strings.Contains(value, "${MCP_HOME") {
		return "", fmt.Errorf("MCP_HOME 模板格式不正确")
	}
	return value, nil
}

func mcpInstallRoot(runtimeDir string, mcpID string) string {
	return filepath.Join(runtimeDir, "mcps", sanitizeMCPInstallID(mcpID))
}

func sanitizeMCPInstallID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, char := range value {
		valid := (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
		if valid {
			builder.WriteRune(char)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return "mcp"
	}
	return out
}

func containsUnresolvedMCPPlaceholder(value string) bool {
	upper := strings.ToUpper(strings.TrimSpace(value))
	return strings.Contains(upper, "/ABSOLUTE/PATH") ||
		strings.Contains(upper, "\\ABSOLUTE\\PATH") ||
		strings.Contains(upper, "ABSOLUTE/PATH") ||
		strings.Contains(upper, "ABSOLUTE\\PATH")
}
