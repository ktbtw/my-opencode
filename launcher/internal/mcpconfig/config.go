package mcpconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"launcher/internal/model"
	"launcher/internal/opencodeconfig"
)

func Load() (*model.DeviceMCPConfigInfo, error) {
	path, raw, parsed, err := opencodeconfig.ReadMap()
	if err != nil {
		return nil, err
	}
	return parseInfo(path, raw, parsed), nil
}

func Save(input model.DeviceMCPConfigInfo) (*model.DeviceMCPConfigInfo, error) {
	path, _, parsed, err := opencodeconfig.ReadMap()
	if err != nil {
		return nil, err
	}
	current := asMap(parsed["mcp"])
	// 关键修复:以文件中已有的 mcp 节点为基底,再用 input 携带的 server 覆盖/新增。
	// 旧逻辑用 input.Servers 整体替换 mcp 字段,任何 input 未携带的 server(如用户手动
	// 配置的 verify)会被物理删除。此处改为并集,只更新/新增 input 里的 server,
	// 不动 input 未提及的已有 server,避免误删。
	next := map[string]any{}
	for name, value := range current {
		next[name] = value
	}
	incoming := map[string]bool{}
	for _, item := range input.Servers {
		server, err := buildServer(item, asMap(current[item.Name]))
		if err != nil {
			return nil, err
		}
		next[item.Name] = server
		incoming[item.Name] = true
	}
	// 审计日志:打印操作前后的 server 名单(仅名字,不含 token/env 等敏感内容),
	// 便于排查 MCP 配置(如 verify)意外消失的问题。
	preserved := make([]string, 0)
	for name := range current {
		if !incoming[name] {
			preserved = append(preserved, name)
		}
	}
	sort.Strings(preserved)
	log.Printf("[launcher][mcp] Save before=%v incoming=%v preserved(未在本次提交中、已保留)=%v",
		sortedKeys(current), sortedKeys(asMapBoolKeys(incoming)), preserved)
	if len(next) == 0 {
		delete(parsed, "mcp")
	} else {
		parsed["mcp"] = next
	}
	if _, raw, err := opencodeconfig.WriteMap(parsed); err != nil {
		return nil, err
	} else {
		log.Printf("[launcher][mcp] Save after=%v", sortedKeys(asMap(parsed["mcp"])))
		return parseInfo(path, string(raw), parsed), nil
	}
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func asMapBoolKeys(m map[string]bool) map[string]any {
	out := map[string]any{}
	for k := range m {
		out[k] = true
	}
	return out
}

func Remove(name string) (*model.DeviceMCPConfigInfo, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("请先选择要删除的 MCP")
	}
	path, _, parsed, err := opencodeconfig.ReadMap()
	if err != nil {
		return nil, err
	}
	current := asMap(parsed["mcp"])
	log.Printf("[launcher][mcp] Remove name=%s before=%v", name, sortedKeys(current))
	delete(current, name)
	if len(current) == 0 {
		delete(parsed, "mcp")
	} else {
		parsed["mcp"] = current
	}
	if _, raw, err := opencodeconfig.WriteMap(parsed); err != nil {
		return nil, err
	} else {
		log.Printf("[launcher][mcp] Remove name=%s after=%v", name, sortedKeys(asMap(parsed["mcp"])))
		return parseInfo(path, string(raw), parsed), nil
	}
}

func parseInfo(path string, raw string, parsed map[string]any) *model.DeviceMCPConfigInfo {
	names := make([]string, 0)
	for name := range asMap(parsed["mcp"]) {
		names = append(names, name)
	}
	sort.Strings(names)
	servers := make([]model.DeviceMCPServerInfo, 0, len(names))
	invalid := make([]string, 0)
	for _, name := range names {
		server, ok := parseServer(name, asMap(parsed["mcp"])[name])
		if !ok {
			invalid = append(invalid, name)
			continue
		}
		servers = append(servers, server)
	}
	warning := ""
	if len(invalid) > 0 {
		warning = "以下 MCP 配置缺少必要字段，已忽略：" + strings.Join(invalid, "、")
	}
	return &model.DeviceMCPConfigInfo{
		Exists:      true,
		ConfigPath:  path,
		RawJSON:     raw,
		PreviewJSON: scrub(raw),
		ChangedKeys: []string{"mcp"},
		Warning:     warning,
		Servers:     servers,
	}
}

func parseServer(name string, value any) (model.DeviceMCPServerInfo, bool) {
	info := asMap(value)
	if info == nil {
		return model.DeviceMCPServerInfo{}, false
	}
	typ := strings.TrimSpace(asString(info["type"]))
	if typ != "remote" && typ != "local" {
		return model.DeviceMCPServerInfo{}, false
	}
	server := model.DeviceMCPServerInfo{
		Name:             name,
		Type:             typ,
		Enabled:          asBoolDefault(info["enabled"], true),
		Timeout:          asInt(info["timeout"]),
		ConnectTimeout:   asInt(info["connect_timeout"]),
		DiscoveryTimeout: asInt(info["discovery_timeout"]),
		ToolTimeout:      asInt(info["tool_timeout"]),
		AsyncTools:       stringList(info["async_tools"]),
		URL:              strings.TrimSpace(asString(info["url"])),
		Headers:          stringMap(info["headers"]),
		Command:          stringList(info["command"]),
		Environment:      stringMap(info["environment"]),
		OAuthMode:        "auto",
	}
	if oauth, ok := info["oauth"].(bool); ok && !oauth {
		server.OAuthMode = "disabled"
	}
	if oauth := asMap(info["oauth"]); oauth != nil {
		server.OAuthMode = "custom"
		server.OAuthClientID = strings.TrimSpace(asString(oauth["clientId"]))
		server.OAuthClientSecretMasked = mask(asString(oauth["clientSecret"]))
		server.OAuthScope = strings.TrimSpace(asString(oauth["scope"]))
	}
	if server.Type == "remote" && server.URL == "" {
		return model.DeviceMCPServerInfo{}, false
	}
	if server.Type == "local" && len(server.Command) == 0 {
		return model.DeviceMCPServerInfo{}, false
	}
	return server, true
}

func buildServer(input model.DeviceMCPServerInfo, existing map[string]any) (map[string]any, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, errors.New("MCP 名称不能为空")
	}
	switch input.Type {
	case "remote":
	case "local":
	default:
		return nil, fmt.Errorf("不支持的 MCP 类型：%s", input.Type)
	}
	server := map[string]any{
		"type": input.Type,
	}
	if !input.Enabled {
		server["enabled"] = false
	}
	if input.Timeout > 0 {
		server["timeout"] = input.Timeout
	}
	if input.ConnectTimeout > 0 {
		server["connect_timeout"] = input.ConnectTimeout
	}
	if input.DiscoveryTimeout > 0 {
		server["discovery_timeout"] = input.DiscoveryTimeout
	}
	if input.ToolTimeout > 0 {
		server["tool_timeout"] = input.ToolTimeout
	}
	if tools := cleanedList(input.AsyncTools); len(tools) > 0 {
		server["async_tools"] = tools
	}
	if input.Type == "remote" {
		url := strings.TrimSpace(input.URL)
		if url == "" {
			return nil, errors.New("远程 MCP 必须填写 URL")
		}
		server["url"] = url
		headers := cleanedMap(input.Headers)
		if len(headers) > 0 {
			server["headers"] = headers
		}
		switch strings.TrimSpace(input.OAuthMode) {
		case "", "auto":
		case "disabled":
			server["oauth"] = false
		case "custom":
			oauth := map[string]any{}
			if value := strings.TrimSpace(input.OAuthClientID); value != "" {
				oauth["clientId"] = value
			}
			secret := strings.TrimSpace(input.OAuthClientSecret)
			if secret == "" {
				secret = existingOAuthSecret(existing)
			}
			if secret != "" {
				oauth["clientSecret"] = secret
			}
			if value := strings.TrimSpace(input.OAuthScope); value != "" {
				oauth["scope"] = value
			}
			server["oauth"] = oauth
		default:
			return nil, fmt.Errorf("不支持的 OAuth 模式：%s", input.OAuthMode)
		}
		return server, nil
	}
	command := cleanedList(input.Command)
	if len(command) == 0 {
		return nil, errors.New("本地 MCP 必须填写启动命令")
	}
	if containsUnresolvedPlaceholder(command) {
		return nil, fmt.Errorf("本地 MCP %s 启动命令仍包含 /ABSOLUTE/PATH 占位路径，请先安装 MCP 并改成真实路径", name)
	}
	server["command"] = command
	env := SanitizeRuntimeEnvironment(
		mergeCredentialEnvironment(cleanedMap(input.Environment), existing),
	)
	if containsUnresolvedPlaceholder(mapValues(env)) {
		return nil, fmt.Errorf("本地 MCP %s 环境变量仍包含 /ABSOLUTE/PATH 占位路径，请先安装 MCP 并改成真实路径", name)
	}
	if len(env) > 0 {
		server["environment"] = env
	}
	return server, nil
}

func mergeCredentialEnvironment(incoming map[string]string, existing map[string]any) map[string]string {
	current := stringMap(existing["environment"])
	for key, value := range current {
		if !isCredentialEnvironmentKey(key) {
			continue
		}
		next, ok := incoming[key]
		if ok && !isCredentialPlaceholder(next) {
			continue
		}
		if incoming == nil {
			incoming = map[string]string{}
		}
		incoming[key] = value
	}
	return incoming
}

func isCredentialEnvironmentKey(key string) bool {
	key = strings.ToUpper(strings.TrimSpace(key))
	return strings.Contains(key, "TOKEN") ||
		strings.Contains(key, "SECRET") ||
		strings.Contains(key, "PASSWORD") ||
		strings.Contains(key, "API_KEY") ||
		strings.HasSuffix(key, "_KEY")
}

func isCredentialPlaceholder(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" ||
		strings.Contains(value, "replace_me") ||
		strings.Contains(value, "<redacted>") ||
		strings.Contains(value, "****") ||
		isShortCredentialPlaceholder(value)
}

func isShortCredentialPlaceholder(value string) bool {
	for _, prefix := range []string{"vat_", "vpt_"} {
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(value, prefix)
		return remainder != "" && strings.Trim(remainder, "x") == ""
	}
	return false
}

// SanitizeRuntimeEnvironment keeps placeholder credentials from masking
// real values inherited from the Agent process environment.
func SanitizeRuntimeEnvironment(input map[string]string) map[string]string {
	out := cleanedMap(input)
	for key, value := range out {
		if isCredentialEnvironmentKey(key) && isCredentialPlaceholder(value) {
			delete(out, key)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EnvironmentWithAgentCredentialOverrides keeps server-specific settings while
// allowing a per-Agent credential to replace the same credential in the shared
// MCP configuration.
func EnvironmentWithAgentCredentialOverrides(configured map[string]string, agent map[string]string) (map[string]string, bool) {
	out := cleanedMap(configured)
	changed := false
	for key := range out {
		if !isCredentialEnvironmentKey(key) {
			continue
		}
		value, ok := agent[key]
		value = strings.TrimSpace(value)
		if !ok || isCredentialPlaceholder(value) {
			continue
		}
		out[key] = value
		changed = true
	}
	if !changed {
		return nil, false
	}
	return out, true
}

func containsUnresolvedPlaceholder(items []string) bool {
	for _, item := range items {
		upper := strings.ToUpper(strings.TrimSpace(item))
		if strings.Contains(upper, "/ABSOLUTE/PATH") ||
			strings.Contains(upper, "\\ABSOLUTE\\PATH") ||
			strings.Contains(upper, "ABSOLUTE/PATH") ||
			strings.Contains(upper, "ABSOLUTE\\PATH") {
			return true
		}
	}
	return false
}

func mapValues(input map[string]string) []string {
	out := make([]string, 0, len(input))
	for _, value := range input {
		out = append(out, value)
	}
	return out
}

func existingOAuthSecret(existing map[string]any) string {
	oauth := asMap(existing["oauth"])
	if oauth == nil {
		return ""
	}
	return strings.TrimSpace(asString(oauth["clientSecret"]))
}

func cleanedList(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func cleanedMap(items map[string]string) map[string]string {
	if len(items) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range items {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func asMap(value any) map[string]any {
	info, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return info
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

func asInt(value any) int {
	switch item := value.(type) {
	case int:
		return item
	case int64:
		return int(item)
	case float64:
		return int(item)
	default:
		return 0
	}
}

func asBoolDefault(value any, fallback bool) bool {
	item, ok := value.(bool)
	if !ok {
		return fallback
	}
	return item
}

func stringList(value any) []string {
	var items []string
	switch value := value.(type) {
	case []string:
		items = value
	case []any:
		items = make([]string, 0, len(value))
		for _, item := range value {
			items = append(items, asString(item))
		}
	default:
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(item)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

func stringMap(value any) map[string]string {
	var info map[string]string
	switch value := value.(type) {
	case map[string]string:
		info = value
	case map[string]any:
		info = make(map[string]string, len(value))
		for key, item := range value {
			info[key] = asString(item)
		}
	default:
		return nil
	}
	out := map[string]string{}
	for key, item := range info {
		text := strings.TrimSpace(item)
		if strings.TrimSpace(key) == "" || text == "" {
			continue
		}
		out[key] = text
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mask(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "****" + value[len(value)-4:]
}

func scrub(raw string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return raw
	}
	maskSecrets(parsed, "")
	data, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return raw
	}
	return fmt.Sprintf("%s\n", data)
}

func maskSecrets(value any, key string) {
	switch item := value.(type) {
	case map[string]any:
		for name, next := range item {
			lower := strings.ToLower(strings.TrimSpace(name))
			if secret, ok := next.(string); ok && shouldMask(lower) {
				item[name] = mask(secret)
				continue
			}
			maskSecrets(next, lower)
		}
	case []any:
		for _, next := range item {
			maskSecrets(next, key)
		}
	}
}

func shouldMask(key string) bool {
	if strings.Contains(key, "secret") {
		return true
	}
	if strings.Contains(key, "token") {
		return true
	}
	if strings.Contains(key, "apikey") {
		return true
	}
	return strings.Contains(key, "authorization")
}
