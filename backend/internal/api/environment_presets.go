package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/model"
)

func defaultEnvironmentPresets() []model.EnvironmentPreset {
	return []model.EnvironmentPreset{
		{
			ID:          "http-proxy-local",
			Name:        "HTTP 代理",
			Description: "让 Agent 的 HTTP 请求走本机代理，常用于模型接口、npm 或 pip 网络不稳定时。",
			Category:    "代理",
			Source:      "内置预设",
			Enabled:     true,
			Tags:        []string{"proxy", "network"},
			Variables: map[string]string{
				"HTTP_PROXY":  "http://127.0.0.1:7897",
				"HTTPS_PROXY": "http://127.0.0.1:7897",
			},
			SortOrder: 10,
			BuiltIn:   true,
		},
		{
			ID:          "socks-proxy-local",
			Name:        "SOCKS 代理",
			Description: "让支持 ALL_PROXY 的工具走 SOCKS5 代理。",
			Category:    "代理",
			Source:      "内置预设",
			Enabled:     true,
			Tags:        []string{"proxy", "network"},
			Variables: map[string]string{
				"ALL_PROXY": "socks5://127.0.0.1:7897",
			},
			SortOrder: 20,
			BuiltIn:   true,
		},
		{
			ID:          "npm-registry-cn",
			Name:        "npm 国内镜像",
			Description: "加速 Node 依赖下载，适合前端项目构建。",
			Category:    "依赖镜像",
			Source:      "内置预设",
			Enabled:     true,
			Tags:        []string{"node", "npm", "mirror"},
			Variables: map[string]string{
				"NPM_CONFIG_REGISTRY": "https://registry.npmmirror.com",
			},
			SortOrder: 30,
			BuiltIn:   true,
		},
		{
			ID:          "uv-python-mirror-cn",
			Name:        "uv Python 镜像",
			Description: "加速 uv 下载 Python 运行时和 Python 包。",
			Category:    "依赖镜像",
			Source:      "内置预设",
			Enabled:     true,
			Tags:        []string{"python", "uv", "mirror"},
			Variables: map[string]string{
				"UV_DEFAULT_INDEX":         "https://pypi.tuna.tsinghua.edu.cn/simple",
				"UV_PYTHON_INSTALL_MIRROR": "https://registry.npmmirror.com/-/binary/python-build-standalone",
			},
			SortOrder: 40,
			BuiltIn:   true,
		},
		{
			ID:          "pip-index-cn",
			Name:        "pip 国内镜像",
			Description: "让 pip 安装 Python 包时使用清华镜像。",
			Category:    "依赖镜像",
			Source:      "内置预设",
			Enabled:     true,
			Tags:        []string{"python", "pip", "mirror"},
			Variables: map[string]string{
				"PIP_INDEX_URL": "https://pypi.tuna.tsinghua.edu.cn/simple",
			},
			SortOrder: 50,
			BuiltIn:   true,
		},
		{
			ID:          "openai-api-key",
			Name:        "OpenAI Key",
			Description: "为兼容 OpenAI SDK 的工具提供默认 API Key。",
			Category:    "模型密钥",
			Source:      "内置预设",
			Enabled:     true,
			Tags:        []string{"openai", "api-key"},
			Variables: map[string]string{
				"OPENAI_API_KEY": "sk-xxx",
			},
			SortOrder: 60,
			BuiltIn:   true,
		},
		{
			ID:          "anthropic-api-key",
			Name:        "Anthropic Key",
			Description: "为 Claude/Anthropic SDK 提供默认 API Key。",
			Category:    "模型密钥",
			Source:      "内置预设",
			Enabled:     true,
			Tags:        []string{"anthropic", "claude", "api-key"},
			Variables: map[string]string{
				"ANTHROPIC_API_KEY": "sk-ant-xxx",
			},
			SortOrder: 70,
			BuiltIn:   true,
		},
	}
}

func (a *API) ListEnvironmentPresets(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentOperator(r); !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if err := a.ensureDefaultEnvironmentPresets(); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items, err := a.store.ListEnvironmentPresets(false)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) ListAdminEnvironmentPresets(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if err := a.ensureDefaultEnvironmentPresets(); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items, err := a.store.ListEnvironmentPresets(true)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) UpsertEnvironmentPreset(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req model.EnvironmentPreset
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if id := strings.TrimSpace(chi.URLParam(r, "id")); id != "" {
		req.ID = id
	}
	item, err := a.upsertEnvironmentPreset(req)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, item)
}

func (a *API) SetEnvironmentPresetEnabled(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "环境预设 ID 不能为空"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	item, err := a.environmentPresetByID(id, true)
	if err != nil {
		write(w, statusForStoreError(err), map[string]string{"error": err.Error()})
		return
	}
	item.Enabled = req.Enabled
	stored, err := a.upsertEnvironmentPreset(item)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, stored)
}

func (a *API) DeleteEnvironmentPreset(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "环境预设 ID 不能为空"})
		return
	}
	if err := a.store.DeleteEnvironmentPreset(id); err != nil {
		write(w, statusForStoreError(err), map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"success": true})
}

func (a *API) ensureDefaultEnvironmentPresets() error {
	initialized, err := a.defaultSeedInitialized(defaultSeedEnvironmentPresetsKey)
	if err != nil {
		return err
	}
	if initialized {
		return nil
	}
	items, err := a.store.ListEnvironmentPresets(true)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		for _, item := range defaultEnvironmentPresets() {
			if _, err := a.upsertEnvironmentPreset(item); err != nil {
				return err
			}
		}
	}
	return a.markDefaultSeedInitialized(defaultSeedEnvironmentPresetsKey)
}

func (a *API) environmentPresetByID(id string, includeDisabled bool) (model.EnvironmentPreset, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.EnvironmentPreset{}, errors.New("环境预设 ID 不能为空")
	}
	if err := a.ensureDefaultEnvironmentPresets(); err != nil {
		return model.EnvironmentPreset{}, err
	}
	items, err := a.store.ListEnvironmentPresets(includeDisabled)
	if err != nil {
		return model.EnvironmentPreset{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return model.EnvironmentPreset{}, errors.New("环境预设不存在")
}

func (a *API) upsertEnvironmentPreset(item model.EnvironmentPreset) (*model.EnvironmentPreset, error) {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		return nil, errors.New("环境预设 ID 不能为空")
	}
	item.Name = strings.TrimSpace(item.Name)
	if item.Name == "" {
		item.Name = item.ID
	}
	item.Category = strings.TrimSpace(item.Category)
	if item.Category == "" {
		item.Category = "通用"
	}
	item.Source = strings.TrimSpace(item.Source)
	item.Description = strings.TrimSpace(item.Description)
	item.Tags = cleanStringSlice(item.Tags)
	item.Variables = cleanEnvironmentVariables(item.Variables)
	if len(item.Variables) == 0 {
		return nil, errors.New("环境预设至少需要一个变量")
	}
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return a.store.UpsertEnvironmentPreset(item)
}

func cleanEnvironmentVariables(input map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	return out
}
