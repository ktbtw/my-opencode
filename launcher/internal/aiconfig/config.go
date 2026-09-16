package aiconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"launcher/internal/model"
	"launcher/internal/opencodeconfig"
	"launcher/internal/paths"
)

type file struct {
	Provider map[string]provider `json:"provider,omitempty"`
	Model    string              `json:"model,omitempty"`
}

type provider struct {
	Options    map[string]string `json:"options,omitempty"`
	Models     map[string]any    `json:"models,omitempty"`
	API        string            `json:"api,omitempty"`
	BaseURL    string            `json:"base_url,omitempty"`
	ConsoleURL string            `json:"x-operit-console-url,omitempty"`
}

type rule struct {
	Pattern    *regexp.Regexp
	Modalities *model.DeviceAIModalities
}

type RuntimeProviderMetadata struct {
	Connected []string                      `json:"connected"`
	All       []RuntimeProviderMetadataItem `json:"all"`
}

type RuntimeProviderMetadataItem struct {
	ID     string                          `json:"id"`
	Models map[string]RuntimeModelMetadata `json:"models"`
}

type RuntimeModelMetadata struct {
	ID           string                    `json:"id"`
	ProviderID   string                    `json:"providerID"`
	Name         string                    `json:"name"`
	Limit        RuntimeModelLimit         `json:"limit"`
	Modalities   *model.DeviceAIModalities `json:"modalities"`
	Capabilities RuntimeModelCapabilities  `json:"capabilities"`
	Variants     map[string]any            `json:"variants"`
}

type RuntimeModelLimit struct {
	Context int64 `json:"context"`
}

type RuntimeModelCapabilities struct {
	Reasoning bool            `json:"reasoning"`
	Input     map[string]bool `json:"input"`
	Output    map[string]bool `json:"output"`
}

const (
	thinkingMetadataKey   = "x-operit-thinking"
	consoleURLMetadataKey = "x-operit-console-url"
	// 下列键记录窗口值的来源，保证「供应商返回 > 手填 > 预设推断」在多次刷新后仍成立。
	upstreamContextKey = "x-operit-upstream-context"
	upstreamOutputKey  = "x-operit-upstream-output"
	manualContextKey   = "x-operit-manual-context"
	manualOutputKey    = "x-operit-manual-output"
)

var exact = map[string]*model.DeviceAIModalities{
	"gpt-image-1":                 {Input: []string{"text"}, Output: []string{"image"}},
	"dall-e-2":                    {Input: []string{"text"}, Output: []string{"image"}},
	"dall-e-3":                    {Input: []string{"text"}, Output: []string{"image"}},
	"grok-imagine-1.0":            {Input: []string{"text"}, Output: []string{"image"}},
	"grok-imagine-1.0-fast":       {Input: []string{"text"}, Output: []string{"image"}},
	"grok-imagine-1.0-edit":       {Input: []string{"text", "image"}, Output: []string{"image"}},
	"grok-imagine-1.0-video":      {Input: []string{"text"}, Output: []string{"video"}},
	"grok-imagine-1.0-video-edit": {Input: []string{"text", "image"}, Output: []string{"video"}},
}

var providerRules = map[string][]rule{
	"google": {
		{Pattern: regexp.MustCompile(`(?i)^imagen([-/].*|\d.*|$)`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"image"}}},
		{Pattern: regexp.MustCompile(`(?i)^veo([-/].*|\d.*|$)`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"video"}}},
		{Pattern: regexp.MustCompile(`(?i)^nano-banana.*pro$`), Modalities: &model.DeviceAIModalities{Input: []string{"text", "image"}, Output: []string{"image"}}},
		{Pattern: regexp.MustCompile(`(?i)^nano-banana`), Modalities: &model.DeviceAIModalities{Input: []string{"text", "image"}, Output: []string{"text", "image"}}},
		{Pattern: regexp.MustCompile(`(?i)^gemini`), Modalities: &model.DeviceAIModalities{Input: []string{"text", "image", "pdf"}, Output: []string{"text"}}},
	},
	"x-ai": {
		{Pattern: regexp.MustCompile(`(?i)^grok-imagine.*video.*edit`), Modalities: &model.DeviceAIModalities{Input: []string{"text", "image"}, Output: []string{"video"}}},
		{Pattern: regexp.MustCompile(`(?i)^grok-imagine.*video`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"video"}}},
		{Pattern: regexp.MustCompile(`(?i)^grok-imagine.*edit`), Modalities: &model.DeviceAIModalities{Input: []string{"text", "image"}, Output: []string{"image"}}},
		{Pattern: regexp.MustCompile(`(?i)^grok-imagine`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"image"}}},
	},
	"openai": {
		{Pattern: regexp.MustCompile(`(?i)^gpt-image`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"image"}}},
		{Pattern: regexp.MustCompile(`(?i)^dall-e`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"image"}}},
		{Pattern: regexp.MustCompile(`(?i)^whisper`), Modalities: &model.DeviceAIModalities{Input: []string{"audio"}, Output: []string{"text"}}},
	},
	"openai-compatible": {
		{Pattern: regexp.MustCompile(`(?i)^gpt-image`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"image"}}},
		{Pattern: regexp.MustCompile(`(?i)^dall-e`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"image"}}},
		{Pattern: regexp.MustCompile(`(?i)^whisper`), Modalities: &model.DeviceAIModalities{Input: []string{"audio"}, Output: []string{"text"}}},
	},
	"claude-test": {
		{Pattern: regexp.MustCompile(`(?i)^claude`), Modalities: &model.DeviceAIModalities{Input: []string{"text", "image"}, Output: []string{"text"}}},
	},
	"bfl": {
		{Pattern: regexp.MustCompile(`(?i)^flux`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"image"}}},
	},
	"recraft": {
		{Pattern: regexp.MustCompile(`(?i)^recraft`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"image"}}},
	},
}

var keywordRules = []rule{
	{Pattern: regexp.MustCompile(`(?i)(^|[-_/])kimi-k3([-_.]|$)`), Modalities: &model.DeviceAIModalities{Input: []string{"text", "image", "video"}, Output: []string{"text"}}},
	{Pattern: regexp.MustCompile(`(?i)(^|[-_/])kimi-k2[._-]?5([-_.]|$)`), Modalities: &model.DeviceAIModalities{Input: []string{"text", "image"}, Output: []string{"text"}}},
	{Pattern: regexp.MustCompile(`(?i)(^|[-_/])kimi-k2[._-]?[67]([-_.]|$)`), Modalities: &model.DeviceAIModalities{Input: []string{"text", "image", "video"}, Output: []string{"text"}}},
	{Pattern: regexp.MustCompile(`(?i)(^|[-_/])(gpt-image|dall-e|imagen|flux|recraft|sdxl|stable-diffusion)([-_/]|$)`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"image"}}},
	{Pattern: regexp.MustCompile(`(?i)(^|[-_/])(veo|video-gen|text-to-video)([-_/]|$)`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"video"}}},
	{Pattern: regexp.MustCompile(`(?i)(^|[-_/])(vision|vl|ocr)([-_/]|$)`), Modalities: &model.DeviceAIModalities{Input: []string{"text", "image"}, Output: []string{"text"}}},
	{Pattern: regexp.MustCompile(`(?i)(^|[-_/])(whisper|transcribe|stt|asr)([-_/]|$)`), Modalities: &model.DeviceAIModalities{Input: []string{"audio"}, Output: []string{"text"}}},
	{Pattern: regexp.MustCompile(`(?i)(^|[-_/])(embedding|embed|e5|bge|rerank)([-_/]|$)`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"text"}}},
	{Pattern: regexp.MustCompile(`(?i)(^|[-_/])(tts)([-_/]|$)`), Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"audio"}}},
}

var gptAPIModePattern = regexp.MustCompile(`(?i)(^|/)(chatgpt|gpt)([._-]|\d|$)`)
var grok46Pattern = regexp.MustCompile(`(?i)^grok[-_.]?4[-_.]?[56](?:$|[-_.])`)
var grok43Or420Pattern = regexp.MustCompile(`(?i)^grok[-_.]?4[-_.]?(?:3|20)(?:$|[-_.])`)

func Path() (string, error) {
	return opencodeconfig.Path()
}

func Dir() (string, error) {
	return paths.OpenCodeConfigDir()
}

func Load() (*model.DeviceAIConfigInfo, error) {
	configPath, raw, err := readText()
	if err != nil {
		return nil, err
	}
	return parseInfo(configPath, raw)
}

func EnrichWithRuntimeProviderMetadata(info *model.DeviceAIConfigInfo, metadata RuntimeProviderMetadata) *model.DeviceAIConfigInfo {
	if info == nil {
		return nil
	}
	connected := make(map[string]struct{}, len(metadata.Connected))
	for _, id := range metadata.Connected {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		connected[id] = struct{}{}
	}
	if len(connected) == 0 || len(metadata.All) == 0 {
		return info
	}
	runtimeProviders := make(map[string]map[string]RuntimeModelMetadata, len(metadata.All))
	for _, provider := range metadata.All {
		providerID := strings.TrimSpace(provider.ID)
		if providerID == "" {
			continue
		}
		if _, ok := connected[providerID]; !ok {
			continue
		}
		if len(provider.Models) == 0 {
			continue
		}
		models := make(map[string]RuntimeModelMetadata, len(provider.Models))
		for modelID, item := range provider.Models {
			modelID = strings.TrimSpace(modelID)
			if modelID == "" {
				continue
			}
			if strings.TrimSpace(item.ProviderID) == "" {
				item.ProviderID = providerID
			}
			if strings.TrimSpace(item.ID) == "" {
				item.ID = modelID
			}
			models[modelID] = item
		}
		if len(models) > 0 {
			runtimeProviders[providerID] = models
		}
	}
	if len(runtimeProviders) == 0 {
		return info
	}
	for i := range info.Providers {
		providerID := strings.TrimSpace(info.Providers[i].ID)
		if providerID == "" {
			continue
		}
		models, ok := runtimeProviders[providerID]
		if !ok {
			continue
		}
		info.Providers[i].Models = enrichDeviceAIModels(info.Providers[i].Models, providerID, models)
		if providerID == strings.TrimSpace(info.Provider) {
			info.Models = info.Providers[i].Models
		}
	}
	if strings.TrimSpace(info.Provider) != "" && len(info.Models) == 0 {
		for _, provider := range info.Providers {
			if strings.TrimSpace(provider.ID) == strings.TrimSpace(info.Provider) {
				info.Models = provider.Models
				break
			}
		}
	}
	return info
}

func enrichDeviceAIModels(items []model.DeviceAIModelInfo, providerID string, runtime map[string]RuntimeModelMetadata) []model.DeviceAIModelInfo {
	if len(runtime) == 0 {
		return items
	}
	out := make([]model.DeviceAIModelInfo, len(items), len(items)+len(runtime))
	copy(out, items)
	seen := make(map[string]struct{}, len(items))
	for i := range out {
		modelID := strings.TrimSpace(out[i].ID)
		if modelID == "" {
			continue
		}
		seen[modelID] = struct{}{}
		meta, ok := runtime[modelID]
		if !ok {
			continue
		}
		enrichDeviceAIModel(&out[i], providerID, modelID, meta)
	}
	runtimeIDs := make([]string, 0, len(runtime))
	for modelID := range runtime {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}
		if _, ok := seen[modelID]; ok {
			continue
		}
		runtimeIDs = append(runtimeIDs, modelID)
	}
	sort.Strings(runtimeIDs)
	for _, modelID := range runtimeIDs {
		item := model.DeviceAIModelInfo{ID: modelID}
		enrichDeviceAIModel(&item, providerID, modelID, runtime[modelID])
		out = append(out, item)
	}
	return out
}

func enrichDeviceAIModel(item *model.DeviceAIModelInfo, providerID string, modelID string, meta RuntimeModelMetadata) {
	manualOverride := item.Thinking != nil && item.Thinking.OverrideEnabled
	// Keep explicit model configuration authoritative. Runtime metadata only
	// fills modalities for models that do not have a saved value yet.
	if item.Modalities == nil {
		if modalities := runtimeModelModalities(meta); modalities != nil {
			item.Modalities = modalities
		}
	}
	// 手填的窗口值必须优先，runtime 上报只用于补空值：否则会把用户设置覆盖成
	// 上报值，再以 context_limit 回传给客户端，被当成「供应商返回值」压住手填值。
	if item.ManualContext <= 0 && meta.Limit.Context > 0 {
		item.Context = meta.Limit.Context
	}
	if item.Context <= 0 {
		item.Context = inferContextLimit(providerID, modelID, item.Name)
	}
	if len(meta.Variants) > 0 && !manualOverride {
		item.Variants = cloneMap(meta.Variants)
	}
	if item.Thinking == nil && (meta.Capabilities.Reasoning || len(meta.Variants) > 0) {
		item.Thinking = thinkingFromVariants("runtime", meta.Variants)
	}
	if item.Thinking != nil && !item.Thinking.OverrideEnabled {
		item.Thinking.Source = "runtime"
		item.Thinking.Supported = meta.Capabilities.Reasoning || len(meta.Variants) > 0
		item.Thinking.Variants = cloneMap(meta.Variants)
	}
	if strings.TrimSpace(item.OwnedBy) == "" {
		item.OwnedBy = providerID
	}
	if strings.TrimSpace(item.Name) == "" {
		item.Name = firstNonEmpty(meta.Name, modelID)
	}
}

func runtimeModelModalities(meta RuntimeModelMetadata) *model.DeviceAIModalities {
	if normalized := normalizeModalities(meta.Modalities); normalized != nil {
		return normalized
	}
	input := boolKeys(meta.Capabilities.Input)
	output := boolKeys(meta.Capabilities.Output)
	return normalizeModalities(&model.DeviceAIModalities{Input: input, Output: output})
}

func boolKeys(items map[string]bool) []string {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for key, enabled := range items {
		if !enabled {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func parseInfo(configPath string, raw string) (*model.DeviceAIConfigInfo, error) {
	var parsed file
	if err := decodeConfig([]byte(raw), &parsed); err != nil {
		return nil, err
	}
	providerID := selectedProvider(parsed.Model, parsed.Provider)
	baseURL := ""
	consoleURL := ""
	masked := ""
	apiMode := "responses"
	modelID := stripModelProvider(parsed.Model)
	models := []model.DeviceAIModelInfo{}
	providerIDs := make([]string, 0, len(parsed.Provider))
	for id := range parsed.Provider {
		providerIDs = append(providerIDs, id)
	}
	sort.Strings(providerIDs)
	providers := make([]model.DeviceAIProviderInfo, 0, len(providerIDs))
	for _, id := range providerIDs {
		item := parsed.Provider[id]
		providerModels := parseModels(item.Models, id)
		providerBaseURL := providerBaseURL(item)
		itemAPIMode := effectiveAPIMode(item.Options["apiMode"], modelIDs(providerModels)...)
		providers = append(providers, model.DeviceAIProviderInfo{
			ID:           id,
			BaseURL:      providerBaseURL,
			ConsoleURL:   strings.TrimSpace(item.ConsoleURL),
			APIKeyMasked: mask(item.Options["apiKey"]),
			APIMode:      itemAPIMode,
			Models:       providerModels,
		})
		if id == providerID {
			baseURL = providerBaseURL
			consoleURL = item.ConsoleURL
			masked = mask(item.Options["apiKey"])
			apiMode = effectiveAPIMode(item.Options["apiMode"], modelID)
			models = providerModels
		}
	}
	return &model.DeviceAIConfigInfo{
		Exists:       true,
		ConfigPath:   configPath,
		Provider:     providerID,
		BaseURL:      baseURL,
		ConsoleURL:   strings.TrimSpace(consoleURL),
		APIKeyMasked: masked,
		APIMode:      apiMode,
		Model:        modelID,
		Models:       models,
		RawJSON:      raw,
		PreviewJSON:  scrub(raw),
		Providers:    providers,
		ChangedKeys:  []string{"provider", "model"},
	}, nil
}

func parseModels(items map[string]any, provider string) []model.DeviceAIModelInfo {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for id := range items {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	out := make([]model.DeviceAIModelInfo, 0, len(keys))
	for _, id := range keys {
		item := items[id]
		name := id
		owned := provider
		var modalities *model.DeviceAIModalities
		var variants map[string]any
		var thinking *model.DeviceAIThinkingInfo
		var upstreamContext, upstreamOutput, manualContext, manualOutput int64
		if info, ok := item.(map[string]any); ok {
			if value, ok := info["name"].(string); ok && strings.TrimSpace(value) != "" {
				name = value
			}
			if value, ok := info["owned_by"].(string); ok && strings.TrimSpace(value) != "" {
				owned = value
			}
			modalities = parseModalities(info["modalities"])
			upstreamContext = parseUpstreamContextLimit(info)
			upstreamOutput = parseUpstreamOutputLimit(info)
			manualContext = parseManualContextLimit(info)
			manualOutput = parseManualOutputLimit(info)
			// 兼容手写或旧版本配置：没有来源标记时，把 limit 里的值当作手填值，
			// 这样显式窗口不会被预设推断覆盖。
			if upstreamContext <= 0 && manualContext <= 0 {
				manualContext = parseContextLimit(info)
			}
			if upstreamOutput <= 0 && manualOutput <= 0 {
				manualOutput = parseOutputLimit(info)
			}
			variants = parseVariants(info["variants"])
			thinking = parseThinkingInfo(info, variants)
		}
		inferred := inferContextLimit(provider, id, name)
		contextLimit, outputLimit := effectiveLimits(upstreamContext, manualContext, inferred, upstreamOutput, manualOutput)
		out = append(out, model.DeviceAIModelInfo{
			ID:              id,
			Name:            name,
			OwnedBy:         owned,
			Modalities:      matchModalities(provider, id, name, modalities),
			Context:         contextLimit,
			Output:          outputLimit,
			UpstreamContext: upstreamContext,
			UpstreamOutput:  upstreamOutput,
			ManualContext:   manualContext,
			ManualOutput:    manualOutput,
			Variants:        variants,
			Thinking:        thinking,
		})
	}
	return out
}

func Save(input model.DeviceAIConfigInfo) (*model.DeviceAIConfigInfo, error) {
	configPath, _, parsed, err := readRaw()
	if err != nil {
		return nil, err
	}
	updated := applyConfigInput(parsed, input.Provider, input.BaseURL, input.ConsoleURL, input.APIKeyMasked, input.APIMode, input.Model, input.Models, input.Force, false)
	raw, err := marshalConfig(updated)
	if err != nil {
		return nil, err
	}
	if err := writeRaw(configPath, raw); err != nil {
		return nil, err
	}
	return parseInfo(configPath, string(raw))
}

func SaveText(text string) (*model.DeviceAIConfigInfo, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, errors.New("请先填写配置内容")
	}
	var parsed map[string]any
	if err := decodeConfig([]byte(trimmed), &parsed); err != nil {
		return nil, err
	}
	if _, _, err := opencodeconfig.WriteMap(parsed); err != nil {
		return nil, err
	}
	return Load()
}

func ClearProvider(id string) (*model.DeviceAIConfigInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("请先选择要删除的供应商")
	}
	_, _, parsed, err := readRaw()
	if err != nil {
		return nil, err
	}
	providers := asMap(parsed["provider"])
	delete(providers, id)
	if len(providers) == 0 {
		delete(parsed, "provider")
	} else {
		parsed["provider"] = providers
	}
	if strings.HasPrefix(strings.TrimSpace(asString(parsed["model"])), id+"/") {
		delete(parsed, "model")
	}
	path, raw, err := opencodeconfig.WriteMap(parsed)
	if err != nil {
		return nil, err
	}
	return parseInfo(path, string(raw))
}

func ListModels(input model.DeviceAIConfigInput) ([]model.DeviceAIModelInfo, error) {
	input = hydrateInput(input)
	baseURL := strings.TrimSpace(input.BaseURL)
	apiKey := strings.TrimSpace(input.APIKey)
	provider := strings.TrimSpace(input.Provider)
	if baseURL == "" {
		return nil, errors.New("请先填写接口地址")
	}
	if apiKey == "" {
		return nil, errors.New("请先填写 API Key")
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("获取模型失败")
	}
	var body struct {
		Data []struct {
			ID                  string                    `json:"id"`
			OwnedBy             string                    `json:"owned_by"`
			Modalities          *model.DeviceAIModalities `json:"modalities"`
			Context             int64                     `json:"context_length"`
			ContextMax          int64                     `json:"context_window"`
			Output              int64                     `json:"max_output_tokens"`
			Variants            map[string]any            `json:"variants"`
			SupportedParameters []string                  `json:"supported_parameters"`
			Capabilities        any                       `json:"capabilities"`
			Reasoning           any                       `json:"reasoning"`
			Limit               struct {
				Context int64 `json:"context"`
				Output  int64 `json:"output"`
			} `json:"limit"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	manuals := manualLimitLookup(input)
	out := make([]model.DeviceAIModelInfo, 0, len(body.Data))
	for _, item := range body.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		variants := parseVariants(item.Variants)
		thinking := providerThinkingInfo(baseURL, provider, item.SupportedParameters, item.Capabilities, item.Reasoning, variants)
		if len(variants) == 0 && thinking != nil {
			variants = cloneMap(thinking.Variants)
		}
		ownedBy := firstNonEmpty(item.OwnedBy, provider)
		upstreamContext := firstPositiveInt64(item.Context, item.ContextMax, item.Limit.Context)
		upstreamOutput := firstPositiveInt64(item.Output, item.Limit.Output)
		limits := manuals[id]
		manualContext, manualOutput := limits[0], limits[1]
		contextLimit, outputLimit := effectiveLimits(upstreamContext, manualContext, inferContextLimit(provider, id, id), upstreamOutput, manualOutput)
		out = append(out, model.DeviceAIModelInfo{
			ID:              id,
			Name:            id,
			OwnedBy:         ownedBy,
			Modalities:      matchModalities(provider, id, id, item.Modalities),
			Context:         contextLimit,
			Output:          outputLimit,
			UpstreamContext: upstreamContext,
			UpstreamOutput:  upstreamOutput,
			ManualContext:   manualContext,
			ManualOutput:    manualOutput,
			Variants:        variants,
			Thinking:        thinking,
		})
	}
	return out, nil
}

// manualLimitLookup 取用户此前手填的窗口值，刷新时不会被供应商返回值冲掉。
func manualLimitLookup(input model.DeviceAIConfigInput) map[string][2]int64 {
	result := map[string][2]int64{}
	for _, item := range input.Config.Models {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		result[id] = [2]int64{item.ManualContext, item.ManualOutput}
	}
	return result
}

func LoadWithModels(input model.DeviceAIConfigInput) (*model.DeviceAIConfigInfo, error) {
	input = hydrateInput(input)
	models, err := ListModels(input)
	if err != nil {
		return nil, err
	}
	configPath, _, parsed, err := readRaw()
	if err != nil {
		return nil, err
	}
	updated := applyConfigInput(parsed, input.Provider, input.BaseURL, input.ConsoleURL, input.APIKey, input.APIMode, input.Model, models, len(models) > 0, true)
	raw, err := marshalConfig(updated)
	if err != nil {
		return nil, err
	}
	return parseInfo(configPath, string(raw))
}

func hydrateInput(input model.DeviceAIConfigInput) model.DeviceAIConfigInput {
	providerID := strings.TrimSpace(firstNonEmpty(input.Provider, input.ProviderID))
	if providerID == "" {
		return input
	}
	_, _, parsed, err := read()
	if err != nil {
		return input
	}
	providerCfg, ok := parsed.Provider[providerID]
	if !ok {
		return input
	}
	if strings.TrimSpace(input.BaseURL) == "" {
		input.BaseURL = providerBaseURL(providerCfg)
	}
	if strings.TrimSpace(input.APIKey) == "" {
		input.APIKey = strings.TrimSpace(providerCfg.Options["apiKey"])
	}
	if strings.TrimSpace(input.APIMode) == "" {
		input.APIMode = providerCfg.Options["apiMode"]
	}
	if strings.TrimSpace(input.ConsoleURL) == "" {
		input.ConsoleURL = strings.TrimSpace(providerCfg.ConsoleURL)
	}
	if strings.TrimSpace(input.Model) == "" && strings.HasPrefix(strings.TrimSpace(parsed.Model), providerID+"/") {
		input.Model = stripModelProvider(parsed.Model)
	}
	input.Provider = providerID
	return input
}

func providerBaseURL(item provider) string {
	return firstNonEmpty(
		item.Options["baseURL"],
		item.Options["baseUrl"],
		item.Options["base_url"],
		item.API,
		item.BaseURL,
	)
}

func readText() (string, string, error) {
	return opencodeconfig.ReadText()
}

func readRaw() (string, string, map[string]any, error) {
	return opencodeconfig.ReadMap()
}

func read() (string, string, file, error) {
	path, raw, err := readText()
	if err != nil {
		return "", "", file{}, err
	}
	var parsed file
	if err := decodeConfig([]byte(raw), &parsed); err != nil {
		return "", "", file{}, err
	}
	return path, raw, parsed, nil
}

func writeRaw(path string, data []byte) error {
	return opencodeconfig.WriteRaw(path, data)
}

func marshalConfig(parsed map[string]any) ([]byte, error) {
	data, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func decodeConfig(data []byte, out any) error {
	return opencodeconfig.Decode(data, out)
}

func sanitizeJSONC(data []byte) ([]byte, error) {
	return opencodeconfig.SanitizeJSONC(data)
}

func stripJSONComments(data []byte) ([]byte, error) {
	var out strings.Builder
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(data) {
			next := data[i+1]
			if next == '/' {
				i += 2
				for ; i < len(data); i++ {
					if data[i] == '\n' {
						out.WriteByte('\n')
						break
					}
				}
				continue
			}
			if next == '*' {
				i += 2
				closed := false
				for ; i < len(data); i++ {
					if data[i] == '\n' {
						out.WriteByte('\n')
					}
					if data[i] == '*' && i+1 < len(data) && data[i+1] == '/' {
						i++
						closed = true
						break
					}
				}
				if !closed {
					return nil, errors.New("配置文件注释未正常闭合")
				}
				continue
			}
		}
		out.WriteByte(ch)
	}
	return []byte(out.String()), nil
}

func stripTrailingCommas(data []byte) []byte {
	var out strings.Builder
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == ',' {
			j := i + 1
			for ; j < len(data); j++ {
				if !isJSONWhitespace(data[j]) {
					break
				}
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue
			}
		}
		out.WriteByte(ch)
	}
	return []byte(out.String())
}

func isJSONWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t'
}

func firstProvider(items map[string]provider) string {
	for id := range items {
		if strings.TrimSpace(id) != "" {
			return id
		}
	}
	return "openai-compatible"
}

func selectedProvider(modelRef string, items map[string]provider) string {
	providerID := modelProvider(modelRef)
	if providerID != "" {
		if _, ok := items[providerID]; ok {
			return providerID
		}
	}
	return firstProvider(items)
}

func modelProvider(modelRef string) string {
	modelRef = strings.TrimSpace(modelRef)
	if modelRef == "" {
		return ""
	}
	idx := strings.Index(modelRef, "/")
	if idx <= 0 {
		return ""
	}
	return strings.TrimSpace(modelRef[:idx])
}

func stripModelProvider(modelRef string) string {
	modelRef = strings.TrimSpace(modelRef)
	if modelRef == "" {
		return ""
	}
	idx := strings.Index(modelRef, "/")
	if idx < 0 {
		return modelRef
	}
	return strings.TrimSpace(modelRef[idx+1:])
}

func joinModelRef(providerID, modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ""
	}
	if strings.Contains(modelID, "/") {
		return modelID
	}
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return modelID
	}
	return providerID + "/" + modelID
}

func applyConfigInput(parsed map[string]any, providerID, baseURL, consoleURL, apiKey, apiMode, modelID string, models []model.DeviceAIModelInfo, force, preserveOverrides bool) map[string]any {
	if parsed == nil {
		parsed = map[string]any{}
	}
	if _, ok := parsed["$schema"]; !ok {
		parsed["$schema"] = "https://opencode.ai/config.json"
	}
	providerID = firstNonEmpty(providerID, modelProvider(asString(parsed["model"])), firstMapKey(asMap(parsed["provider"])), "openai-compatible")
	delete(parsed, "enabled_providers")
	providers := ensureMapField(parsed, "provider")
	providerCfg := ensureMapValue(providers, providerID)
	if strings.TrimSpace(asString(providerCfg["name"])) == "" {
		providerCfg["name"] = providerID
	}
	if strings.TrimSpace(asString(providerCfg["npm"])) == "" {
		providerCfg["npm"] = "@ai-sdk/openai-compatible"
	}
	if strings.TrimSpace(baseURL) != "" {
		if strings.TrimSpace(asString(providerCfg["api"])) == "" {
			providerCfg["api"] = strings.TrimSpace(baseURL)
		}
		options := ensureMapField(providerCfg, "options")
		options["baseURL"] = strings.TrimSpace(baseURL)
	}
	if value := strings.TrimSpace(consoleURL); value != "" {
		providerCfg[consoleURLMetadataKey] = value
	} else {
		delete(providerCfg, consoleURLMetadataKey)
	}
	if strings.TrimSpace(apiKey) != "" && !strings.Contains(apiKey, "****") {
		options := ensureMapField(providerCfg, "options")
		options["apiKey"] = strings.TrimSpace(apiKey)
	}
	options := ensureMapField(providerCfg, "options")
	options["apiMode"] = effectiveAPIMode(apiMode, append([]string{modelID}, modelIDs(models)...)...)
	if force || len(models) > 0 {
		existingModels := asMap(providerCfg["models"])
		if len(models) == 0 {
			delete(providerCfg, "models")
		}
		updatedModels := make(map[string]any, len(models))
		for _, item := range models {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				continue
			}
			entry := cloneMap(asMap(existingModels[id]))
			if entry == nil {
				entry = map[string]any{}
			}
			if preserveOverrides {
				existingThinking := parseThinkingInfo(entry, parseVariants(entry["variants"]))
				if existingThinking != nil && existingThinking.OverrideEnabled {
					item.Thinking = existingThinking
					item.Variants = cloneMap(existingThinking.Variants)
				}
			}
			entry["name"] = firstNonEmpty(item.Name, id)
			if item.Modalities != nil {
				entry["modalities"] = map[string]any{
					"input":  append([]string(nil), item.Modalities.Input...),
					"output": append([]string(nil), item.Modalities.Output...),
				}
			}
			upstreamContext := item.UpstreamContext
			upstreamOutput := item.UpstreamOutput
			manualContext := item.ManualContext
			manualOutput := item.ManualOutput
			// 兼容只传最终值的调用方：没有来源标记时，把最终值当作手填值。
			if upstreamContext <= 0 && upstreamOutput <= 0 && manualContext <= 0 && manualOutput <= 0 {
				manualContext = item.Context
				manualOutput = item.Output
			}
			inferred := inferContextLimit(providerID, id, item.Name)
			contextLimit, outputLimit := effectiveLimits(upstreamContext, manualContext, inferred, upstreamOutput, manualOutput)
			writeLimitMarkers(entry, upstreamContext, upstreamOutput, manualContext, manualOutput)
			if limit := mergeLimit(entry["limit"], contextLimit, outputLimit); limit != nil {
				entry["limit"] = limit
			} else {
				delete(entry, "limit")
			}
			if contextLimit > 0 {
				entry["context_limit"] = contextLimit
			} else {
				delete(entry, "context_limit")
			}
			if outputLimit > 0 {
				entry["output_limit"] = outputLimit
			} else {
				delete(entry, "output_limit")
			}
			if len(item.Variants) > 0 {
				entry["variants"] = cloneMap(item.Variants)
			}
			if item.Thinking != nil {
				entry["reasoning"] = item.Thinking.Supported
				variants := item.Thinking.Variants
				if item.Thinking.OverrideEnabled {
					variants = item.Thinking.OverrideVariants
					if len(variants) == 0 {
						variants = item.Thinking.Variants
					}
				}
				if len(variants) > 0 {
					entry["variants"] = cloneMap(variants)
					entry["variants_mode"] = "replace"
				} else if item.Thinking.OverrideEnabled && !item.Thinking.Supported {
					entry["variants"] = map[string]any{"disabled": map[string]any{"disabled": true}}
					entry["variants_mode"] = "replace"
				} else {
					delete(entry, "variants")
					delete(entry, "variants_mode")
				}
				entry[thinkingMetadataKey] = thinkingMetadata(item.Thinking)
			}
			updatedModels[id] = entry
		}
		if len(updatedModels) > 0 {
			providerCfg["models"] = updatedModels
		}
	}
	if fullModel := joinModelRef(providerID, modelID); fullModel != "" {
		parsed["model"] = fullModel
	}
	return parsed
}

func normalizeAPIMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "chat":
		return "chat"
	default:
		return "responses"
	}
}

func effectiveAPIMode(value string, modelIDs ...string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "responses":
		return "responses"
	case "chat":
		return "chat"
	}
	for _, id := range modelIDs {
		if gptAPIModePattern.MatchString(strings.TrimSpace(id)) {
			return "responses"
		}
	}
	if len(modelIDs) == 0 {
		return "responses"
	}
	return "chat"
}

func modelIDs(models []model.DeviceAIModelInfo) []string {
	out := make([]string, 0, len(models))
	for _, item := range models {
		if strings.TrimSpace(item.ID) != "" {
			out = append(out, item.ID)
		}
	}
	return out
}

func ensureMapField(parent map[string]any, key string) map[string]any {
	value := asMap(parent[key])
	if value == nil {
		value = map[string]any{}
		parent[key] = value
	}
	return value
}

func ensureMapValue(parent map[string]any, key string) map[string]any {
	value := asMap(parent[key])
	if value == nil {
		value = map[string]any{}
		parent[key] = value
	}
	return value
}

func asMap(value any) map[string]any {
	result, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return result
}

func mergeLimitContext(value any, contextLimit int64) map[string]any {
	out := cloneMap(asMap(value))
	if out == nil {
		out = map[string]any{}
	}
	out["context"] = contextLimit
	return out
}

func parseContextLimit(info map[string]any) int64 {
	if len(info) == 0 {
		return 0
	}
	if limit := asMap(info["limit"]); limit != nil {
		if value := positiveInt64(limit["context"]); value > 0 {
			return value
		}
	}
	return firstPositiveInt64(
		positiveInt64(info["context_limit"]),
		positiveInt64(info["context_length"]),
		positiveInt64(info["context_window"]),
	)
}

func parseOutputLimit(info map[string]any) int64 {
	if len(info) == 0 {
		return 0
	}
	if limit := asMap(info["limit"]); limit != nil {
		if value := positiveInt64(limit["output"]); value > 0 {
			return value
		}
	}
	return firstPositiveInt64(
		positiveInt64(info["output_limit"]),
		positiveInt64(info["max_output_tokens"]),
	)
}

func parseManualContextLimit(info map[string]any) int64 {
	if len(info) == 0 {
		return 0
	}
	return positiveInt64(info[manualContextKey])
}

func parseManualOutputLimit(info map[string]any) int64 {
	if len(info) == 0 {
		return 0
	}
	return positiveInt64(info[manualOutputKey])
}

func parseUpstreamContextLimit(info map[string]any) int64 {
	if len(info) == 0 {
		return 0
	}
	return positiveInt64(info[upstreamContextKey])
}

func parseUpstreamOutputLimit(info map[string]any) int64 {
	if len(info) == 0 {
		return 0
	}
	return positiveInt64(info[upstreamOutputKey])
}

// writeLimitMarkers 只记录来源值，未提供的来源会被清除，避免旧值一直压住新值。
func writeLimitMarkers(entry map[string]any, upstreamContext, upstreamOutput, manualContext, manualOutput int64) {
	writeMarker(entry, upstreamContextKey, upstreamContext)
	writeMarker(entry, upstreamOutputKey, upstreamOutput)
	writeMarker(entry, manualContextKey, manualContext)
	writeMarker(entry, manualOutputKey, manualOutput)
}

func writeMarker(entry map[string]any, key string, value int64) {
	if value > 0 {
		entry[key] = value
		return
	}
	delete(entry, key)
}

// mergeLimit 写入最终生效值；两个值都为空时清掉 limit，交由 runtime 处理。
func mergeLimit(value any, contextLimit, outputLimit int64) map[string]any {
	if contextLimit <= 0 && outputLimit <= 0 {
		return nil
	}
	out := cloneMap(asMap(value))
	if out == nil {
		out = map[string]any{}
	}
	if contextLimit > 0 {
		out["context"] = contextLimit
	} else {
		delete(out, "context")
	}
	if outputLimit > 0 {
		out["output"] = outputLimit
	} else {
		delete(out, "output")
	}
	return out
}

// effectiveLimits 按 供应商返回 > 手填 > 预设推断 求出最终生效的窗口值。
// 输出上限没有预设推断，只取 供应商返回 > 手填。
func effectiveLimits(upstreamContext, manualContext, inferredContext, upstreamOutput, manualOutput int64) (int64, int64) {
	return firstPositiveInt64(upstreamContext, manualContext, inferredContext),
		firstPositiveInt64(upstreamOutput, manualOutput)
}

func inferContextLimit(provider, modelID, modelName string) int64 {
	aliases := []string{modelID, modelName}
	if last := lastPathSegment(modelID); last != "" {
		aliases = append(aliases, last)
	}
	if last := lastPathSegment(modelName); last != "" {
		aliases = append(aliases, last)
	}
	for _, alias := range aliases {
		if value := grokContextLimit(alias); value > 0 {
			return value
		}
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(provider)), "grok") {
		for _, alias := range aliases {
			if strings.EqualFold(strings.TrimSpace(alias), "kun") {
				return 500000
			}
		}
	}
	return 0
}

func grokContextLimit(value string) int64 {
	alias := normalizeModelAlias(value)
	switch {
	case grok46Pattern.MatchString(alias):
		return 500000
	case grok43Or420Pattern.MatchString(alias):
		return 1000000
	case strings.HasPrefix(alias, "grok"):
		return 256000
	default:
		return 0
	}
}

func lastPathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx := strings.LastIndexAny(value, "/"); idx >= 0 && idx+1 < len(value) {
		return value[idx+1:]
	}
	return ""
}

func normalizeModelAlias(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func parseVariants(value any) map[string]any {
	items := asMap(value)
	variants := make(map[string]any, len(items))
	for key, value := range items {
		options := cloneMap(asMap(value))
		if options != nil && boolOrDefault(options["disabled"], false) {
			continue
		}
		if options != nil {
			delete(options, "disabled")
			variants[key] = options
			continue
		}
		variants[key] = value
	}
	if len(variants) == 0 {
		return nil
	}
	return variants
}

func parseThinkingInfo(info map[string]any, variants map[string]any) *model.DeviceAIThinkingInfo {
	metadata := asMap(info[thinkingMetadataKey])
	reasoning, hasReasoning := info["reasoning"].(bool)
	if len(metadata) == 0 {
		if !hasReasoning && len(variants) == 0 {
			return nil
		}
		thinking := thinkingFromVariants("inferred", variants)
		thinking.Supported = reasoning || len(variants) > 0
		if len(variants) > 0 {
			thinking.Source = "manual"
			thinking.OverrideEnabled = true
			thinking.OverrideVariants = cloneMap(variants)
		}
		return thinking
	}
	thinking := thinkingFromVariants(firstNonEmpty(asString(metadata["source"]), "inferred"), variants)
	thinking.Supported = boolOrDefault(metadata["supported"], reasoning || len(variants) > 0)
	thinking.Control = firstNonEmpty(asString(metadata["control"]), thinking.Control)
	thinking.Protocol = firstNonEmpty(asString(metadata["protocol"]), thinking.Protocol)
	thinking.SupportedParameters = stringValues(metadata["supported_parameters"])
	thinking.OverrideEnabled = boolOrDefault(metadata["override_enabled"], false)
	if thinking.OverrideEnabled {
		thinking.Source = "manual"
		thinking.OverrideVariants = cloneMap(variants)
	}
	return thinking
}

func providerThinkingInfo(baseURL, provider string, supportedParameters []string, capabilities, reasoning any, variants map[string]any) *model.DeviceAIThinkingInfo {
	parameters := uniqueStrings(supportedParameters)
	has := func(target string) bool {
		for _, item := range parameters {
			if strings.EqualFold(item, target) {
				return true
			}
		}
		return false
	}
	supported := len(variants) > 0 || reasoningCapability(capabilities) || reasoningCapability(reasoning)
	for _, key := range []string{"reasoning", "reasoning_effort", "include_reasoning", "thinking", "thinking_config"} {
		if has(key) {
			supported = true
			break
		}
	}
	if !supported {
		return nil
	}
	protocol := "custom"
	generated := cloneMap(variants)
	control := "toggle"
	if len(generated) > 0 {
		thinking := thinkingFromVariants("provider", generated)
		thinking.SupportedParameters = parameters
		return thinking
	}
	if has("reasoning") && (strings.Contains(strings.ToLower(baseURL), "openrouter") || strings.Contains(strings.ToLower(provider), "openrouter")) {
		protocol = "openrouter"
		control = "effort"
		generated = map[string]any{
			"low":  map[string]any{"reasoning": map[string]any{"effort": "low"}},
			"high": map[string]any{"reasoning": map[string]any{"effort": "high"}},
		}
	} else if has("reasoning_effort") {
		protocol = "openai-compatible"
		control = "effort"
		generated = map[string]any{
			"low":    map[string]any{"reasoningEffort": "low"},
			"medium": map[string]any{"reasoningEffort": "medium"},
			"high":   map[string]any{"reasoningEffort": "high"},
		}
	} else if has("reasoning") {
		protocol = "reasoning"
		control = "effort"
		generated = map[string]any{
			"low":  map[string]any{"reasoning": map[string]any{"effort": "low"}},
			"high": map[string]any{"reasoning": map[string]any{"effort": "high"}},
		}
	} else if has("thinking_config") {
		protocol = "google"
	} else if has("thinking") {
		protocol = "anthropic"
	}
	return &model.DeviceAIThinkingInfo{
		Supported:           true,
		Source:              "provider",
		Control:             control,
		Protocol:            protocol,
		SupportedParameters: parameters,
		Variants:            generated,
	}
}

func thinkingFromVariants(source string, variants map[string]any) *model.DeviceAIThinkingInfo {
	control := "toggle"
	protocol := "custom"
	for _, value := range variants {
		options := asMap(value)
		if options == nil {
			continue
		}
		if _, ok := options["reasoning"]; ok {
			protocol = "openrouter"
			control = "effort"
		}
		if _, ok := options["reasoningEffort"]; ok {
			protocol = "openai-compatible"
			control = "effort"
		}
		if _, ok := options["reasoning_effort"]; ok {
			protocol = "openai-compatible"
			control = "effort"
		}
		if _, ok := options["thinkingConfig"]; ok {
			protocol = "google"
			control = "effort"
		}
		if thinking := asMap(options["thinking"]); thinking != nil {
			protocol = "anthropic"
			control = "effort"
			if positiveInt64(thinking["budgetTokens"]) > 0 {
				control = "budget"
			}
		}
	}
	return &model.DeviceAIThinkingInfo{
		Supported: len(variants) > 0,
		Source:    source,
		Control:   control,
		Protocol:  protocol,
		Variants:  cloneMap(variants),
	}
}

func thinkingMetadata(thinking *model.DeviceAIThinkingInfo) map[string]any {
	metadata := map[string]any{
		"supported":        thinking.Supported,
		"source":           firstNonEmpty(thinking.Source, "manual"),
		"control":          firstNonEmpty(thinking.Control, "effort"),
		"protocol":         firstNonEmpty(thinking.Protocol, "custom"),
		"override_enabled": thinking.OverrideEnabled,
	}
	if len(thinking.SupportedParameters) > 0 {
		metadata["supported_parameters"] = uniqueStrings(thinking.SupportedParameters)
	}
	return metadata
}

func reasoningCapability(value any) bool {
	switch current := value.(type) {
	case bool:
		return current
	case map[string]any:
		for _, key := range []string{"reasoning", "thinking", "reasoning_effort"} {
			if boolOrDefault(current[key], false) {
				return true
			}
		}
		return false
	case []any:
		for _, item := range current {
			if text, ok := item.(string); ok && strings.Contains(strings.ToLower(text), "reason") {
				return true
			}
		}
	}
	return false
}

func boolOrDefault(value any, fallback bool) bool {
	if parsed, ok := value.(bool); ok {
		return parsed
	}
	return fallback
}

func stringValues(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if strings, ok := value.([]string); ok {
			return uniqueStrings(strings)
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return uniqueStrings(out)
}

func uniqueStrings(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func positiveInt64(value any) int64 {
	switch v := value.(type) {
	case int:
		if v > 0 {
			return int64(v)
		}
	case int64:
		if v > 0 {
			return v
		}
	case float64:
		if v > 0 {
			return int64(v)
		}
	case json.Number:
		if n, err := v.Int64(); err == nil && n > 0 {
			return n
		}
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

func firstMapKey(items map[string]any) string {
	if len(items) == 0 {
		return ""
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.TrimSpace(key) != "" {
			return key
		}
	}
	return ""
}

func cloneMap(items map[string]any) map[string]any {
	if items == nil {
		return nil
	}
	out := make(map[string]any, len(items))
	for key, value := range items {
		out[key] = value
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
	providerMap, _ := parsed["provider"].(map[string]any)
	for _, value := range providerMap {
		providerInfo, ok := value.(map[string]any)
		if !ok {
			continue
		}
		options, ok := providerInfo["options"].(map[string]any)
		if !ok {
			continue
		}
		if apiKey, ok := options["apiKey"].(string); ok {
			options["apiKey"] = mask(apiKey)
		}
	}
	data, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return raw
	}
	return fmt.Sprintf("%s\n", data)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func parseModalities(value any) *model.DeviceAIModalities {
	info := asMap(value)
	if info == nil {
		return nil
	}
	return normalizeModalities(&model.DeviceAIModalities{
		Input:  stringList(info["input"]),
		Output: stringList(info["output"]),
	})
}

func normalizeModalities(value *model.DeviceAIModalities) *model.DeviceAIModalities {
	if value == nil {
		return nil
	}
	input := cleanStrings(value.Input)
	output := cleanStrings(value.Output)
	if len(input) == 0 && len(output) == 0 {
		return nil
	}
	return &model.DeviceAIModalities{Input: input, Output: output}
}

func matchModalities(provider, id, name string, current *model.DeviceAIModalities) *model.DeviceAIModalities {
	if value := normalizeModalities(current); value != nil {
		return value
	}
	id = strings.TrimSpace(strings.ToLower(id))
	name = strings.TrimSpace(strings.ToLower(name))
	provider = strings.TrimSpace(strings.ToLower(provider))
	if value := exact[id]; value != nil {
		return normalizeModalities(value)
	}
	for _, item := range providerRules[provider] {
		if item.Pattern.MatchString(id) || item.Pattern.MatchString(name) {
			return normalizeModalities(item.Modalities)
		}
	}
	for _, item := range keywordRules {
		if item.Pattern.MatchString(id) || item.Pattern.MatchString(name) {
			return normalizeModalities(item.Modalities)
		}
	}
	return nil
}

func stringList(value any) []string {
	switch items := value.(type) {
	case []string:
		return cleanStrings(items)
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			text := strings.TrimSpace(asString(item))
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func cleanStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		text := strings.TrimSpace(item)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
