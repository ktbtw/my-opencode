package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/captcha"
	"relay-server/internal/mail"
	"relay-server/internal/model"
	"relay-server/internal/notify"
	"relay-server/internal/overlay"
	"relay-server/internal/projectmemory"
	"relay-server/internal/push"
	"relay-server/internal/store"
)

const userSettingsAgentID = "__user__"

const (
	globalPromptSettingKey               = "global_prompt"
	projectPromptSettingKey              = "project_prompt"
	deviceAllowAllDirsKey                = "allow_all_directories"
	deviceDeletedAgentKey                = "deleted_agent"
	orchestrationEnabledSettingKey       = "subagent_orchestration_enabled"
	orchestrationMaxConcurrentSettingKey = "subagent_orchestration_max_concurrent"
	orchestrationRoleModelsSettingKey    = "subagent_orchestration_role_models"
)

const launcherCompactionConfigCapability = "device_compaction_config_v1"

// launcherStorageCapability 标记 launcher 支持磁盘占用查询与缓存清理。
// 老版本 launcher 不会响应这两条指令，后端据此提前拒绝，避免请求悬空等超时。
const launcherStorageCapability = "device_storage_v1"

func (a *API) enrichOrchestrationTaskMetadata(operatorID int64, metadata map[string]string) map[string]string {
	settings, err := a.store.GetDeviceSettings(operatorID, userSettingsAgentID)
	if err != nil || settings == nil {
		return metadata
	}
	for _, key := range []string{
		orchestrationEnabledSettingKey,
		orchestrationMaxConcurrentSettingKey,
		orchestrationRoleModelsSettingKey,
	} {
		if value := strings.TrimSpace(settings[key]); value != "" {
			if metadata == nil {
				metadata = map[string]string{}
			}
			metadata[key] = value
		}
	}
	return metadata
}

type API struct {
	store                 *store.Memory
	broker                *broker.Broker
	auth                  *auth.Manager
	mailer                *mail.Mailer
	notifyService         *notify.Service
	artifactFetcher       func(context.Context, *model.Task, model.Artifact, io.Writer) error
	pushService           *push.Service
	aiConfigRequest       func(context.Context, int64, string, string, string, any) (model.DeviceAIConfigResultPayload, error)
	goalOptimizeRequest   func(context.Context, int64, string, string, string, any) (model.GoalOptimizeResultPayload, error)
	mcpConfigRequest      func(context.Context, int64, string, string, string, any) (model.DeviceMCPConfigResultPayload, error)
	envConfigRequest      func(context.Context, int64, string, string, string, any) (model.DeviceEnvConfigResultPayload, error)
	launcherRequest       func(context.Context, int64, string, string, string, any) (model.DeviceLauncherResultPayload, error)
	directoryRequest      func(context.Context, int64, string, string, string, any) (model.DeviceDirectoriesResultPayload, error)
	sessionHistoryRequest func(context.Context, int64, string, string, model.SessionHistoryRequestPayload) (model.SessionHistoryResultPayload, error)
	projectMemoryEnabled  bool
	overlayHub            *overlay.Hub
}

func (a *API) SetOverlayHub(hub *overlay.Hub) {
	a.overlayHub = hub
}

func (a *API) SetSessionHistoryRequest(fn func(context.Context, int64, string, string, model.SessionHistoryRequestPayload) (model.SessionHistoryResultPayload, error)) {
	a.sessionHistoryRequest = fn
}

const (
	launcherModelsInitialTimeout = 12 * time.Second
	launcherModelsRefreshTimeout = 2 * time.Second
)

var activeTaskStatuses = []model.TaskStatus{
	model.TaskDispatched,
	"started",
	model.TaskRunning,
	model.TaskCancelling,
	model.TaskWaitingApproval,
}

const supportLogDir = "/opt/chat-codex/support-logs"

func latencyLog(component, stage string, fields map[string]any) {
	payload := map[string]any{
		"component": component,
		"stage":     stage,
	}
	for key, value := range fields {
		payload[key] = value
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[latency][chat] component=%s stage=%s", component, stage)
		return
	}
	log.Printf("[latency][chat] %s", string(data))
}

type trackingWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *trackingWriter) Write(p []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(p)
}

func (w *trackingWriter) WriteHeader(statusCode int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(statusCode)
}

func New(
	s *store.Memory,
	b *broker.Broker,
	a *auth.Manager,
	m *mail.Mailer,
	artifactFetcher func(context.Context, *model.Task, model.Artifact, io.Writer) error,
	notifyService *notify.Service,
	pushService *push.Service,
	aiConfigRequest func(context.Context, int64, string, string, string, any) (model.DeviceAIConfigResultPayload, error),
	goalOptimizeRequest func(context.Context, int64, string, string, string, any) (model.GoalOptimizeResultPayload, error),
	mcpConfigRequest func(context.Context, int64, string, string, string, any) (model.DeviceMCPConfigResultPayload, error),
	envConfigRequest func(context.Context, int64, string, string, string, any) (model.DeviceEnvConfigResultPayload, error),
	launcherRequest func(context.Context, int64, string, string, string, any) (model.DeviceLauncherResultPayload, error),
	directoryRequest func(context.Context, int64, string, string, string, any) (model.DeviceDirectoriesResultPayload, error),
	projectMemoryFeature ...bool,
) *API {
	projectMemoryEnabled := true
	if len(projectMemoryFeature) > 0 {
		projectMemoryEnabled = projectMemoryFeature[0]
	}
	return &API{
		store:                s,
		broker:               b,
		auth:                 a,
		mailer:               m,
		notifyService:        notifyService,
		artifactFetcher:      artifactFetcher,
		pushService:          pushService,
		aiConfigRequest:      aiConfigRequest,
		goalOptimizeRequest:  goalOptimizeRequest,
		mcpConfigRequest:     mcpConfigRequest,
		envConfigRequest:     envConfigRequest,
		launcherRequest:      launcherRequest,
		directoryRequest:     directoryRequest,
		projectMemoryEnabled: projectMemoryEnabled,
	}
}

type registerPushDeviceReq struct {
	Platform       string `json:"platform"`
	Vendor         string `json:"vendor"`
	RegistrationID string `json:"registration_id"`
	DeviceID       string `json:"device_id"`
	DeviceBrand    string `json:"device_brand"`
	DeviceModel    string `json:"device_model"`
	AppVersion     string `json:"app_version"`
}

type pushTestReq struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type emailTestReq struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type reportSupportLogsReq struct {
	Category   string `json:"category"`
	AppVersion string `json:"app_version"`
	BaseURL    string `json:"base_url"`
	Note       string `json:"note"`
	Content    string `json:"content"`
}

type createTaskReq struct {
	AgentID   string            `json:"agent_id"`
	ProjectID string            `json:"project_id"`
	Parts     []model.Part      `json:"parts"`
	SessionID string            `json:"session_id"`
	Metadata  map[string]string `json:"metadata"`
}

type createChatQueueItemReq struct {
	AgentID   string            `json:"agent_id"`
	ProjectID string            `json:"project_id"`
	Parts     []model.Part      `json:"parts"`
	Metadata  map[string]string `json:"metadata"`
}

type updateChatQueueItemReq struct {
	ExpectedVersion int64  `json:"expected_version"`
	Model           string `json:"model"`
	Variant         string `json:"variant"`
}

type deleteChatQueueItemReq struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type reorderChatQueueReq struct {
	ItemIDs          []string         `json:"item_ids"`
	ExpectedVersions map[string]int64 `json:"expected_versions"`
}

type insertChatQueueItemReq struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type optimizeGoalReq struct {
	AgentID       string `json:"agent_id"`
	ProjectID     string `json:"project_id"`
	Goal          string `json:"goal"`
	Model         string `json:"model"`
	Variant       string `json:"variant"`
	MaxIterations int    `json:"max_iterations"`
}

type approveTaskReq struct {
	PermissionID string `json:"permission_id"`
	Reply        string `json:"reply"`
	Message      string `json:"message"`
}

type answerTaskQuestionReq struct {
	RequestID string     `json:"request_id"`
	Answers   [][]string `json:"answers"`
	Rejected  bool       `json:"rejected"`
}

type listDeviceAIModelsReq struct {
	Provider   string                `json:"provider"`
	BaseURL    string                `json:"base_url"`
	ConsoleURL string                `json:"console_url"`
	APIKey     string                `json:"api_key"`
	APIMode    string                `json:"api_mode"`
	Model      string                `json:"model"`
	Models     []model.DeviceAIModel `json:"models"`
}

type saveDeviceAIConfigReq struct {
	Provider   string                `json:"provider"`
	BaseURL    string                `json:"base_url"`
	ConsoleURL string                `json:"console_url"`
	APIKey     string                `json:"api_key"`
	APIMode    string                `json:"api_mode"`
	Model      string                `json:"model"`
	Models     []model.DeviceAIModel `json:"models"`
	Force      bool                  `json:"force"`
}

type saveDeviceAIConfigTextReq struct {
	Text string `json:"text"`
}

type clearDeviceAIProviderReq struct {
	Provider string `json:"provider"`
}

type saveDeviceMCPConfigReq struct {
	Servers []model.DeviceMCPServer `json:"servers"`
}

type removeDeviceMCPConfigReq struct {
	Name string `json:"name"`
}

type saveDeviceAgentMCPSelectionReq struct {
	Mode    string   `json:"mode"`
	Servers []string `json:"servers"`
}

type saveDeviceAgentSemanticSelectionReq struct {
	SemanticAgentID     string `json:"semantic_agent_id"`
	ApplyRecommendedMCP bool   `json:"apply_recommended_mcp"`
	DisableVerifyMCP    bool   `json:"disable_verify_mcp"`
}

type saveDeviceEnvConfigReq struct {
	GlobalEnvironment map[string]string `json:"global_environment"`
	AgentID           string            `json:"agent_id"`
	AgentEnvironment  map[string]string `json:"agent_environment"`
	ExpectedRevision  string            `json:"expected_revision"`
}

type mcpCatalogItem struct {
	ID            string                `json:"id"`
	Title         string                `json:"title"`
	Category      string                `json:"category"`
	Description   string                `json:"description"`
	Source        string                `json:"source"`
	SourceURL     string                `json:"source_url,omitempty"`
	CredentialURL string                `json:"credential_url,omitempty"`
	Install       model.MCPInstallInfo  `json:"install,omitempty"`
	LaunchReady   bool                  `json:"launch_ready"`
	LaunchReason  string                `json:"launch_block_reason,omitempty"`
	Server        model.DeviceMCPServer `json:"server"`
}

type mcpCatalogResponse struct {
	Items []mcpCatalogItem `json:"items"`
}

type launcherUpgradeReq struct {
	TargetVersion string `json:"target_version"`
}

type launcherAutostartReq struct {
	DryRun bool `json:"dry_run"`
}

type launcherSelfUpdateReq struct {
	TargetVersion string `json:"target_version"`
	BaseURL       string `json:"base_url"`
	Apply         bool   `json:"apply"`
	Restart       bool   `json:"restart"`
	DryRun        bool   `json:"dry_run"`
}

type createDeviceAgentReq struct {
	Name       string `json:"name"`
	ProjectDir string `json:"project_dir"`
}

type restartDeviceAgentReq struct {
	AgentID string `json:"agent_id"`
}

type renameDeviceAgentReq struct {
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
}

type setDeviceAgentEnabledReq struct {
	AgentID string `json:"agent_id"`
	Enabled bool   `json:"enabled"`
}

type removeDeviceAgentReq struct {
	AgentID string `json:"agent_id"`
}

type updateDeviceProfileReq struct {
	DisplayName string `json:"display_name"`
}

type reorderDevicesReq struct {
	MachineIDs []string `json:"machine_ids"`
}

type reorderAgentsReq struct {
	AgentIDs []string `json:"agent_ids"`
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *API) Health(w http.ResponseWriter, _ *http.Request) {
	write(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) Ready(w http.ResponseWriter, _ *http.Request) {
	if a.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := a.store.Health(ctx); err != nil {
			write(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded"})
			return
		}
	}
	write(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) ListAgents(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	machines := mergeSessionAgentsIntoMachines(
		a.broker.ListMachines(operator.ID),
		a.listSessions(model.SessionFilter{OperatorID: operator.ID, Limit: 200}),
		cutoff,
	)
	machines = a.filterDeletedAgents(operator.ID, machines)
	machines = applyActiveTasks(machines, a.activeTasksByAgent(operator.ID))
	var agents []model.Agent
	for _, m := range machines {
		agents = append(agents, m.Agents...)
	}
	write(w, http.StatusOK, agents)
}

func (a *API) Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	operator, err := a.store.AuthenticateOperator(req.Username, req.Password)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
		return
	}
	if operator == nil {
		write(w, http.StatusUnauthorized, map[string]string{"error": "username or password invalid"})
		return
	}
	token, err := a.auth.Issue(*operator)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "issue token failed"})
		return
	}
	write(w, http.StatusOK, map[string]any{
		"access_token": token,
		"operator":     operator,
	})
}

func (a *API) Me(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	// 会员等级以数据库为准，避免旧 token 携带过期等级。
	if tier, err := a.store.GetOperatorMembershipTier(operator.ID); err == nil {
		operator.MembershipTier = tier
	} else {
		operator.MembershipTier = model.NormalizeMembershipTier(operator.MembershipTier)
	}
	write(w, http.StatusOK, operator)
}

// GetUploadPolicy 下发当前用户的上传策略（分块大小、并发数、目标速率）。
// 客户端据此决定分块与并发，服务端仍会在每个 chunk 请求上做二次校验。
func (a *API) GetUploadPolicy(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	write(w, http.StatusOK, map[string]any{
		"policy": a.uploadPolicyForOperator(operator),
	})
}

// uploadPolicyForOperator 解析会员等级并返回上传策略。
// 等级优先取数据库当前值；读取失败时回退到 token 中的等级，再回退到 free。
func (a *API) uploadPolicyForOperator(operator model.Operator) model.UploadPolicy {
	tier := model.NormalizeMembershipTier(operator.MembershipTier)
	if resolved, err := a.store.GetOperatorMembershipTier(operator.ID); err == nil {
		tier = resolved
	}
	return model.UploadPolicyForTier(tier)
}

func (a *API) RegisterPushDevice(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if a.pushService == nil {
		write(w, http.StatusServiceUnavailable, map[string]string{"error": "push service unavailable"})
		return
	}
	var req registerPushDeviceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	device, err := a.pushService.RegisterDevice(model.PushDevice{
		OperatorID:     operator.ID,
		Platform:       strings.TrimSpace(req.Platform),
		Vendor:         strings.TrimSpace(req.Vendor),
		RegistrationID: strings.TrimSpace(req.RegistrationID),
		DeviceID:       strings.TrimSpace(req.DeviceID),
		DeviceBrand:    strings.TrimSpace(req.DeviceBrand),
		DeviceModel:    strings.TrimSpace(req.DeviceModel),
		AppVersion:     strings.TrimSpace(req.AppVersion),
	})
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, device)
}

func (a *API) PushTest(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if a.pushService == nil {
		write(w, http.StatusServiceUnavailable, map[string]string{"error": "push service unavailable"})
		return
	}
	var req pushTestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "码控"
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		body = "这是一条测试推送"
	}
	if err := a.pushService.SendTestToOperator(r.Context(), operator.ID, title, body); err != nil {
		write(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]string{"message": "push test sent"})
}

func (a *API) EmailTest(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if a.notifyService == nil {
		write(w, http.StatusServiceUnavailable, map[string]string{"error": "notify service unavailable"})
		return
	}
	var req emailTestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "[ChatCodex] 测试邮件"
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		body = "这是一封测试邮件通知。"
	}
	if err := a.notifyService.SendTestEmail(r.Context(), operator.ID, title, body); err != nil {
		write(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]string{"message": "email test sent"})
}

func newerTask(current, candidate *model.Task) *model.Task {
	if candidate == nil {
		return current
	}
	if current == nil {
		return candidate
	}
	switch {
	case candidate.UpdatedAt.After(current.UpdatedAt):
		return candidate
	case candidate.UpdatedAt.Before(current.UpdatedAt):
		return current
	case candidate.CreatedAt.After(current.CreatedAt):
		return candidate
	default:
		return current
	}
}

func (a *API) activeTasksByAgent(operatorID int64) map[string]*model.Task {
	result := map[string]*model.Task{}
	for _, status := range activeTaskStatuses {
		tasks, err := a.store.ListTasks(model.TaskFilter{
			OperatorID: operatorID,
			Status:     status,
			Limit:      200,
		})
		if err != nil {
			continue
		}
		for _, task := range tasks {
			result[task.AgentID] = newerTask(result[task.AgentID], task)
		}
	}
	return result
}

func applyActiveTasks(machines []model.Machine, activeByAgent map[string]*model.Task) []model.Machine {
	for mi := range machines {
		for ai := range machines[mi].Agents {
			task := activeByAgent[machines[mi].Agents[ai].ID]
			if task != nil {
				machines[mi].Agents[ai].CurrentTask = task.ID
				continue
			}
			machines[mi].Agents[ai].CurrentTask = ""
		}
	}
	return machines
}

func (a *API) ListDevices(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	result, err := a.listDevicesForOperator(r.Context(), operator.ID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "device preferences unavailable"})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) listDevicesForOperator(ctx context.Context, operatorID int64) ([]model.Machine, error) {
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	result := mergeSessionAgentsIntoMachines(
		a.broker.ListMachines(operatorID),
		a.listSessions(model.SessionFilter{OperatorID: operatorID, Limit: 200}),
		cutoff,
	)
	result = a.mergeLauncherManagedAgentsForList(ctx, operatorID, result)
	result = a.filterDeletedAgents(operatorID, result)
	result = applyActiveTasks(result, a.activeTasksByAgent(operatorID))
	result, err := a.applyDevicePreferences(operatorID, result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (a *API) mergeLauncherManagedAgentsForList(ctx context.Context, operatorID int64, machines []model.Machine) []model.Machine {
	if a.launcherRequest == nil || len(machines) == 0 {
		return machines
	}
	var wg sync.WaitGroup
	limit := make(chan struct{}, 6)
	for i := range machines {
		if !machines[i].LauncherOnline {
			continue
		}
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-ctx.Done():
				return
			}
			machines[index] = a.mergeLauncherManagedAgents(ctx, operatorID, machines[index])
		}(i)
	}
	wg.Wait()
	return machines
}

func (a *API) GetDevice(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	activeByAgent := a.activeTasksByAgent(operator.ID)
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	device, ok := a.broker.GetMachine(operator.ID, machineID)
	if ok {
		device = mergeSessionAgentsIntoMachines(
			[]model.Machine{device},
			a.listSessions(model.SessionFilter{
				OperatorID: operator.ID,
				MachineID:  machineID,
				Limit:      50,
			}),
			cutoff,
		)[0]
		device = a.mergeLauncherManagedAgents(r.Context(), operator.ID, device)
		device = a.filterDeletedAgents(operator.ID, []model.Machine{device})[0]
		device = applyActiveTasks([]model.Machine{device}, activeByAgent)[0]
		deviceList, err := a.applyDevicePreferences(operator.ID, []model.Machine{device})
		if err != nil {
			write(w, http.StatusInternalServerError, map[string]string{"error": "device preferences unavailable"})
			return
		}
		device = deviceList[0]
		write(w, http.StatusOK, device)
		return
	}
	// 设备离线时，从历史 session 记录重建离线设备视图
	sessions := a.listSessions(model.SessionFilter{
		OperatorID: operator.ID,
		MachineID:  machineID,
		Limit:      50,
	})
	if len(sessions) == 0 {
		write(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	// 按 agent_id 收集唯一 agent，取最近 session 时间
	type agentEntry struct {
		agentID   string
		projectID string
		lastSeen  time.Time
	}
	hostname := machineID
	agentMap := map[string]*agentEntry{}
	for _, s := range sessions {
		if s.MachineID != "" {
			hostname = s.MachineID
		}
		e, exists := agentMap[s.AgentID]
		if !exists {
			agentMap[s.AgentID] = &agentEntry{agentID: s.AgentID, projectID: s.ProjectID, lastSeen: s.UpdatedAt}
		} else if s.UpdatedAt.After(e.lastSeen) {
			e.lastSeen = s.UpdatedAt
			e.projectID = s.ProjectID
		}
	}
	agents := make([]model.Agent, 0, len(agentMap))
	var latestSeen time.Time
	for _, e := range agentMap {
		if a.isDeletedAgent(operator.ID, e.agentID) {
			continue
		}
		agents = append(agents, model.Agent{
			ID:        e.agentID,
			MachineID: machineID,
			Hostname:  hostname,
			Enabled:   true,
			Status:    "offline",
			Projects:  []model.HelloProject{{ProjectID: e.projectID}},
			SeenAt:    e.lastSeen,
		})
		if e.lastSeen.After(latestSeen) {
			latestSeen = e.lastSeen
		}
	}
	if len(agents) == 0 {
		write(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	offline := model.Machine{
		MachineID: machineID,
		Hostname:  hostname,
		Status:    "offline",
		SeenAt:    latestSeen,
		Agents:    agents,
	}
	offline = a.mergeLauncherManagedAgents(r.Context(), operator.ID, offline)
	offline = applyActiveTasks([]model.Machine{offline}, activeByAgent)[0]
	offlineList, err := a.applyDevicePreferences(operator.ID, []model.Machine{offline})
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "device preferences unavailable"})
		return
	}
	offline = offlineList[0]
	write(w, http.StatusOK, offline)
}

func (a *API) UpdateDeviceProfile(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := strings.TrimSpace(chi.URLParam(r, "machineID"))
	if machineID == "" || !a.deviceVisibleToOperator(operator.ID, machineID) {
		write(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	var request updateDeviceProfileReq
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	if utf8.RuneCountInString(request.DisplayName) > 40 {
		write(w, http.StatusBadRequest, map[string]string{"error": "device name must be 40 characters or fewer"})
		return
	}
	preference, err := a.store.UpdateDeviceDisplayName(operator.ID, machineID, request.DisplayName)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "failed to update device name"})
		return
	}
	write(w, http.StatusOK, preference)
}

func (a *API) ReorderDevices(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var request reorderDevicesReq
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	request.MachineIDs = uniqueTrimmedValues(request.MachineIDs)
	if len(request.MachineIDs) == 0 {
		write(w, http.StatusBadRequest, map[string]string{"error": "machine_ids is required"})
		return
	}
	for _, machineID := range request.MachineIDs {
		if !a.deviceVisibleToOperator(operator.ID, machineID) {
			write(w, http.StatusNotFound, map[string]string{"error": "device not found"})
			return
		}
	}
	preferences, err := a.store.ReorderDevicePreferences(operator.ID, request.MachineIDs)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "failed to reorder devices"})
		return
	}
	write(w, http.StatusOK, map[string]any{"items": preferences})
}

func (a *API) ReorderDeviceAgents(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := strings.TrimSpace(chi.URLParam(r, "machineID"))
	if machineID == "" || !a.deviceVisibleToOperator(operator.ID, machineID) {
		write(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	var request reorderAgentsReq
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	request.AgentIDs = uniqueTrimmedValues(request.AgentIDs)
	if len(request.AgentIDs) == 0 {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent_ids is required"})
		return
	}
	preferences, err := a.store.ReorderAgentPreferences(operator.ID, machineID, request.AgentIDs)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "failed to reorder agents"})
		return
	}
	write(w, http.StatusOK, map[string]any{"items": preferences})
}

func uniqueTrimmedValues(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func (a *API) deviceVisibleToOperator(operatorID int64, machineID string) bool {
	if _, ok := a.broker.GetMachine(operatorID, machineID); ok {
		return true
	}
	return len(a.listSessions(model.SessionFilter{
		OperatorID: operatorID,
		MachineID:  machineID,
		Limit:      1,
	})) > 0
}

func (a *API) applyDevicePreferences(operatorID int64, machines []model.Machine) ([]model.Machine, error) {
	if len(machines) == 0 {
		return machines, nil
	}
	result := append([]model.Machine(nil), machines...)
	newOrder := append([]model.Machine(nil), result...)
	sort.SliceStable(newOrder, func(i, j int) bool {
		if newOrder[i].SeenAt.Equal(newOrder[j].SeenAt) {
			return newOrder[i].MachineID < newOrder[j].MachineID
		}
		if newOrder[i].SeenAt.IsZero() {
			return false
		}
		if newOrder[j].SeenAt.IsZero() {
			return true
		}
		return newOrder[i].SeenAt.Before(newOrder[j].SeenAt)
	})
	machineIDs := make([]string, 0, len(newOrder))
	for _, machine := range newOrder {
		machineIDs = append(machineIDs, machine.MachineID)
	}
	preferences, err := a.store.EnsureDevicePreferences(operatorID, machineIDs)
	if err != nil {
		return nil, err
	}
	byMachine := make(map[string]model.DevicePreference, len(preferences))
	for _, preference := range preferences {
		byMachine[preference.MachineID] = preference
	}
	agentPreferences, err := a.store.ListAgentPreferences(operatorID, machineIDs)
	if err != nil {
		return nil, err
	}
	agentOrder := make(map[string]int64, len(agentPreferences))
	for _, preference := range agentPreferences {
		agentOrder[preference.MachineID+"\x00"+preference.AgentID] = preference.SortOrder
	}
	for i := range result {
		if preference, exists := byMachine[result[i].MachineID]; exists {
			result[i].DisplayName = preference.DisplayName
			result[i].SortOrder = preference.SortOrder
		}
		sortAgentsStable(result[i].MachineID, result[i].Agents, agentOrder)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].SortOrder == result[j].SortOrder {
			return result[i].MachineID < result[j].MachineID
		}
		return result[i].SortOrder < result[j].SortOrder
	})
	return result, nil
}

func sortAgentsStable(machineID string, agents []model.Agent, agentOrder map[string]int64) {
	sort.SliceStable(agents, func(i, j int) bool {
		orderI, hasI := agentOrder[machineID+"\x00"+agents[i].ID]
		orderJ, hasJ := agentOrder[machineID+"\x00"+agents[j].ID]
		if hasI != hasJ {
			return hasI
		}
		if hasI && orderI != orderJ {
			return orderI < orderJ
		}
		if agents[i].ID != agents[j].ID {
			return agents[i].ID < agents[j].ID
		}
		return firstAgentProjectID(agents[i]) < firstAgentProjectID(agents[j])
	})
}

func firstAgentProjectID(agent model.Agent) string {
	if len(agent.Projects) == 0 {
		return ""
	}
	return agent.Projects[0].ProjectID
}

func (a *API) mergeLauncherManagedAgents(ctx context.Context, operatorID int64, machine model.Machine) model.Machine {
	launcherID, err := a.deviceLauncherAgent(operatorID, machine.MachineID)
	if err != nil {
		return machine
	}
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	requestID := fmt.Sprintf("req_launcher_agents_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(requestCtx, operatorID, launcherID, requestID, "device.launcher.get", map[string]string{"machine_id": machine.MachineID})
	if err != nil || result.State == nil || len(result.State.Agents) == 0 {
		return machine
	}
	agentIndex := map[string]int{}
	for i := range machine.Agents {
		if machine.Agents[i].ID == "" {
			continue
		}
		agentIndex[machine.Agents[i].ID] = i
		if !strings.EqualFold(machine.Agents[i].Status, "disabled") {
			machine.Agents[i].Enabled = true
		}
	}
	for _, managed := range result.State.Agents {
		agentID := strings.TrimSpace(managed.ID)
		if agentID == "" {
			continue
		}
		projectID := ""
		projectRoot := ""
		if managed.ProjectDir != "" {
			projectRoot = managed.ProjectDir
		}
		if len(managed.Projects) > 0 {
			projectID = managed.Projects[0].ProjectID
			if projectRoot == "" {
				projectRoot = managed.Projects[0].Root
			}
		}
		if projectID == "" {
			projectID = managed.Name
		}
		if projectID == "" && projectRoot != "" {
			projectID = filepath.Base(projectRoot)
		}
		if projectID == "" {
			projectID = agentID
		}
		if idx, ok := agentIndex[agentID]; ok {
			if name := strings.TrimSpace(managed.Name); name != "" {
				machine.Agents[idx].Name = name
			}
			machine.Agents[idx].Enabled = managed.Enabled
			machine.Agents[idx].SemanticAgentID = managed.SemanticAgentID
			machine.Agents[idx].SemanticAgentName = managed.SemanticAgentName
			if !managed.Enabled {
				machine.Agents[idx].Status = "disabled"
				machine.Agents[idx].CurrentTask = ""
			} else {
				status := strings.TrimSpace(managed.Status)
				if status != "" {
					machine.Agents[idx].Status = status
				}
				if !managed.SeenAt.IsZero() {
					machine.Agents[idx].SeenAt = managed.SeenAt
				}
				if managed.Version != "" {
					machine.Agents[idx].Version = managed.Version
				}
			}
			if projectRoot != "" && len(machine.Agents[idx].Projects) > 0 && machine.Agents[idx].Projects[0].Root == "" {
				machine.Agents[idx].Projects[0].Root = projectRoot
			}
			continue
		}
		status := managed.Status
		if status == "" {
			status = "offline"
		}
		if !managed.Enabled {
			status = "disabled"
		}
		seenAt := managed.SeenAt
		if seenAt.IsZero() {
			seenAt = machine.SeenAt
		}
		machine.Agents = append(machine.Agents, model.Agent{
			ID:                agentID,
			OperatorID:        operatorID,
			MachineID:         machine.MachineID,
			Hostname:          machine.Hostname,
			Name:              managed.Name,
			ProjectDir:        projectRoot,
			SemanticAgentID:   managed.SemanticAgentID,
			SemanticAgentName: managed.SemanticAgentName,
			Version:           managed.Version,
			Enabled:           managed.Enabled,
			Status:            status,
			Projects:          []model.HelloProject{{ProjectID: projectID, Root: projectRoot}},
			SeenAt:            seenAt,
		})
	}
	return machine
}

func (a *API) listSessions(filter model.SessionFilter) []*model.Session {
	sessions, err := a.store.ListSessions(filter)
	if err != nil {
		return nil
	}
	return sessions
}

func mergeSessionAgentsIntoMachines(machines []model.Machine, sessions []*model.Session, cutoff time.Time) []model.Machine {
	result := append([]model.Machine(nil), machines...)
	if len(sessions) == 0 {
		return result
	}

	machineIndex := make(map[string]int, len(result))
	machineAgentSet := make(map[string]map[string]bool, len(result))
	onlineAgentSet := map[string]bool{}
	for i := range result {
		machineIndex[result[i].MachineID] = i
		agentSet := map[string]bool{}
		for _, ag := range result[i].Agents {
			agentSet[ag.ID] = true
			if ag.ID != "" {
				onlineAgentSet[ag.ID] = true
			}
		}
		machineAgentSet[result[i].MachineID] = agentSet
	}

	latestByAgent := map[string]*model.Session{}
	for _, session := range sessions {
		if session == nil || session.AgentID == "" || session.MachineID == "" {
			continue
		}
		if session.UpdatedAt.Before(cutoff) || onlineAgentSet[session.AgentID] {
			continue
		}
		prev, exists := latestByAgent[session.AgentID]
		if !exists || session.UpdatedAt.After(prev.UpdatedAt) {
			latestByAgent[session.AgentID] = session
		}
	}

	for _, session := range latestByAgent {
		idx, exists := machineIndex[session.MachineID]
		if !exists {
			result = append(result, model.Machine{
				MachineID: session.MachineID,
				Hostname:  session.MachineID,
				Status:    "offline",
				SeenAt:    session.UpdatedAt,
				Agents:    []model.Agent{},
			})
			idx = len(result) - 1
			machineIndex[session.MachineID] = idx
			machineAgentSet[session.MachineID] = map[string]bool{}
		}

		machine := &result[idx]
		if machine.Hostname == "" {
			machine.Hostname = session.MachineID
		}
		if session.UpdatedAt.After(machine.SeenAt) {
			machine.SeenAt = session.UpdatedAt
		}
		if machineAgentSet[session.MachineID][session.AgentID] {
			continue
		}

		machine.Agents = append(machine.Agents, model.Agent{
			ID:        session.AgentID,
			MachineID: session.MachineID,
			Hostname:  machine.Hostname,
			Enabled:   true,
			Status:    "offline",
			Projects:  []model.HelloProject{{ProjectID: session.ProjectID}},
			SeenAt:    session.UpdatedAt,
		})
		machineAgentSet[session.MachineID][session.AgentID] = true
	}

	return result
}

func (a *API) deletedAgentSet(operatorID int64) map[string]bool {
	ids, err := a.store.ListDeviceSettingAgentIDs(operatorID, deviceDeletedAgentKey)
	if err != nil || len(ids) == 0 {
		return map[string]bool{}
	}
	result := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			result[id] = true
		}
	}
	return result
}

func (a *API) isDeletedAgent(operatorID int64, agentID string) bool {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return false
	}
	settings, err := a.store.GetDeviceSettings(operatorID, agentID)
	if err != nil || settings == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(settings[deviceDeletedAgentKey]), "true")
}

func (a *API) filterDeletedAgents(operatorID int64, machines []model.Machine) []model.Machine {
	deleted := a.deletedAgentSet(operatorID)
	if len(deleted) == 0 {
		return machines
	}
	filtered := make([]model.Machine, 0, len(machines))
	for _, machine := range machines {
		agents := make([]model.Agent, 0, len(machine.Agents))
		for _, agent := range machine.Agents {
			if deleted[agent.ID] {
				continue
			}
			agents = append(agents, agent)
		}
		machine.Agents = agents
		if len(machine.Agents) == 0 && machine.Status == "offline" && !machine.LauncherOnline {
			continue
		}
		filtered = append(filtered, machine)
	}
	return filtered
}

func (a *API) ListTasks(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	filter := model.TaskFilter{
		OperatorID: operator.ID,
		AgentID:    r.URL.Query().Get("agent_id"),
		MachineID:  r.URL.Query().Get("machine_id"),
		ProjectID:  r.URL.Query().Get("project_id"),
		SessionID:  r.URL.Query().Get("session_id"),
		Status:     model.TaskStatus(r.URL.Query().Get("status")),
		Limit:      50,
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var limit int
		if _, err := fmt.Sscanf(raw, "%d", &limit); err != nil || limit <= 0 || limit > 200 {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		filter.Limit = limit
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("before_created_at")); raw != "" {
		before, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid before_created_at"})
			return
		}
		filter.BeforeCreatedAt = before
		filter.BeforeTaskID = strings.TrimSpace(r.URL.Query().Get("before_task_id"))
		filter.CreatedDesc = true
	}
	if r.URL.Query().Get("sort") == "created_desc" {
		filter.CreatedDesc = true
	}

	tasks, err := a.store.ListTasks(filter)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "list tasks failed"})
		return
	}
	write(w, http.StatusOK, tasks)
}

// ListTaskDelta returns tasks changed after an opaque session cursor.
// The cursor is derived from persisted updated_at/task_id fields.
func (a *API) ListTaskDelta(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}
	filter := model.TaskFilter{OperatorID: operator.ID, SessionID: sessionID, Limit: 200}
	afterRevision := strings.TrimSpace(r.URL.Query().Get("after_revision"))
	if afterRevision != "" {
		filter.CreatedAsc = true
		parts := strings.SplitN(afterRevision, "|", 2)
		if len(parts) != 2 {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid after_revision"})
			return
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil || strings.TrimSpace(parts[1]) == "" {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid after_revision"})
			return
		}
		filter.AfterUpdatedAt = updatedAt
		filter.AfterUpdatedTaskID = strings.TrimSpace(parts[1])
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		var limit int
		if _, err := fmt.Sscanf(raw, "%d", &limit); err != nil || limit <= 0 || limit > 200 {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		filter.Limit = limit
	}
	tasks, err := a.store.ListTasks(filter)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "list task delta failed"})
		return
	}
	revision := ""
	for _, task := range tasks {
		if task == nil || task.UpdatedAt.IsZero() {
			continue
		}
		candidate := task.UpdatedAt.UTC().Format(time.RFC3339Nano) + "|" + task.ID
		if candidate > revision {
			revision = candidate
		}
	}
	write(w, http.StatusOK, map[string]any{"revision": revision, "tasks": tasks, "has_more": len(tasks) >= filter.Limit})
}

func (a *API) ListSessions(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	filter := model.SessionFilter{
		OperatorID: operator.ID,
		AgentID:    r.URL.Query().Get("agent_id"),
		MachineID:  r.URL.Query().Get("machine_id"),
		ProjectID:  r.URL.Query().Get("project_id"),
		Status:     r.URL.Query().Get("status"),
		Limit:      50,
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var limit int
		if _, err := fmt.Sscanf(raw, "%d", &limit); err != nil || limit <= 0 || limit > 200 {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		filter.Limit = limit
	}

	sessions, err := a.store.ListSessions(filter)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "list sessions failed"})
		return
	}
	write(w, http.StatusOK, sessions)
}

func (a *API) CreateTask(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	stepStartedAt := startedAt
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req createTaskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	latencyLog("backend_api", "create_task_decoded", map[string]any{
		"elapsed_ms":  time.Since(startedAt).Milliseconds(),
		"step_ms":     time.Since(stepStartedAt).Milliseconds(),
		"operator_id": operator.ID,
		"agent_id":    req.AgentID,
		"project_id":  req.ProjectID,
		"session_id":  req.SessionID,
	})
	stepStartedAt = time.Now()
	parts, err := normalize(req)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.EqualFold(strings.TrimSpace(req.Metadata["task_command"]), "compact") && strings.TrimSpace(req.SessionID) == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "压缩上下文需要已有会话"})
		return
	}
	latencyLog("backend_api", "create_task_normalized", map[string]any{
		"elapsed_ms":  time.Since(startedAt).Milliseconds(),
		"step_ms":     time.Since(stepStartedAt).Milliseconds(),
		"operator_id": operator.ID,
		"agent_id":    req.AgentID,
		"project_id":  req.ProjectID,
		"part_count":  len(parts),
	})
	stepStartedAt = time.Now()
	if req.AgentID == "" || req.ProjectID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "missing required fields"})
		return
	}
	agent, ok := a.broker.Get(req.AgentID, operator.ID)
	if !ok {
		write(w, http.StatusConflict, map[string]string{"error": "agent offline"})
		return
	}
	latencyLog("backend_api", "create_task_agent_found", map[string]any{
		"elapsed_ms":  time.Since(startedAt).Milliseconds(),
		"step_ms":     time.Since(stepStartedAt).Milliseconds(),
		"operator_id": operator.ID,
		"agent_id":    req.AgentID,
		"machine_id":  agent.MachineID,
		"project_id":  req.ProjectID,
	})
	stepStartedAt = time.Now()
	projectRoot := ""
	for _, item := range agent.Projects {
		if item.ProjectID == req.ProjectID {
			projectRoot = item.Root
			break
		}
	}
	if projectRoot == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent does not own project"})
		return
	}

	latencyLog("backend_api", "create_task_project_verified", map[string]any{
		"elapsed_ms":  time.Since(startedAt).Milliseconds(),
		"step_ms":     time.Since(stepStartedAt).Milliseconds(),
		"operator_id": operator.ID,
		"agent_id":    req.AgentID,
		"machine_id":  agent.MachineID,
		"project_id":  req.ProjectID,
	})
	stepStartedAt = time.Now()

	originalParts := append([]model.Part(nil), parts...)
	baseSystemPrompt, err := a.taskSystemPrompt(operator.ID, req.AgentID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	latencyLog("backend_api", "create_task_system_prompt_loaded", map[string]any{
		"elapsed_ms":           time.Since(startedAt).Milliseconds(),
		"step_ms":              time.Since(stepStartedAt).Milliseconds(),
		"operator_id":          operator.ID,
		"agent_id":             req.AgentID,
		"machine_id":           agent.MachineID,
		"project_id":           req.ProjectID,
		"system_prompt_length": len(baseSystemPrompt),
	})
	stepStartedAt = time.Now()

	taskMetadata := normalizeGoalTaskMetadata(req.Metadata)
	taskMetadata = enrichTaskMetadata(taskMetadata, agent)
	taskMetadata = a.enrichOrchestrationTaskMetadata(operator.ID, taskMetadata)
	memorySnapshot := projectmemory.Snapshot{System: baseSystemPrompt}
	memoryEnabled := a.projectMemoryEnabled
	if scope, scopeErr := a.store.GetProjectScopeForAgent(r.Context(), operator.ID, agent.MachineID, req.AgentID); scopeErr == nil && scope != nil {
		memoryEnabled = memoryEnabled && projectmemory.Resolve(r.Context(), a.store, operator.ID, agent.MachineID, scope.ID).Enabled
	}
	if memoryEnabled {
		memorySnapshot = projectmemory.Build(r.Context(), a.store, baseSystemPrompt, operator.ID,
			agent.MachineID, req.AgentID, projectmemory.QueryFromParts(originalParts))
	}
	if memorySnapshot.ScopeID != "" {
		if taskMetadata == nil {
			taskMetadata = map[string]string{}
		}
		taskMetadata["project_memory_scope_id"] = memorySnapshot.ScopeID
		taskMetadata["project_memory_revision"] = strconv.FormatInt(memorySnapshot.Revision, 10)
	}
	task := a.store.CreateTask(req.AgentID, agent.MachineID, req.ProjectID, projectRoot, req.SessionID, originalParts, taskMetadata, operator.ID)
	if memoryEnabled {
		projectmemory.RecordUsage(r.Context(), a.store, task.ID, memorySnapshot)
	}
	a.store.SetStatus(task.ID, model.TaskDispatched)
	a.store.AddEvent(task.ID, model.Event{
		TaskID:  task.ID,
		Type:    "dispatched",
		Content: summarize(originalParts),
		SentAt:  time.Now().UTC(),
	})
	latencyLog("backend_api", "create_task_stored", map[string]any{
		"elapsed_ms":  time.Since(startedAt).Milliseconds(),
		"step_ms":     time.Since(stepStartedAt).Milliseconds(),
		"operator_id": operator.ID,
		"task_id":     task.ID,
		"agent_id":    req.AgentID,
		"machine_id":  agent.MachineID,
		"project_id":  req.ProjectID,
		"session_id":  req.SessionID,
	})
	stepStartedAt = time.Now()

	env := model.Envelope{
		Type:      "task.run",
		RequestID: fmt.Sprintf("req_%s", task.ID),
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.RunPayload{
			TaskID:    task.ID,
			AgentID:   req.AgentID,
			MachineID: agent.MachineID,
			ProjectID: req.ProjectID,
			SessionID: req.SessionID,
			System:    memorySnapshot.System,
			Parts:     originalParts,
			Metadata:  taskMetadata,
		},
	}

	if err := a.broker.DispatchForOperator(context.Background(), operator.ID, req.AgentID, env); err != nil {
		latencyLog("backend_api", "create_task_dispatch_failed", map[string]any{
			"elapsed_ms":  time.Since(startedAt).Milliseconds(),
			"step_ms":     time.Since(stepStartedAt).Milliseconds(),
			"operator_id": operator.ID,
			"task_id":     task.ID,
			"agent_id":    req.AgentID,
			"machine_id":  agent.MachineID,
			"project_id":  req.ProjectID,
			"error":       err.Error(),
		})
		a.store.Fail(task.ID, req.SessionID, err.Error())
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	latencyLog("backend_api", "create_task_dispatched", map[string]any{
		"elapsed_ms":  time.Since(startedAt).Milliseconds(),
		"step_ms":     time.Since(stepStartedAt).Milliseconds(),
		"operator_id": operator.ID,
		"task_id":     task.ID,
		"agent_id":    req.AgentID,
		"machine_id":  agent.MachineID,
		"project_id":  req.ProjectID,
		"session_id":  req.SessionID,
	})
	stepStartedAt = time.Now()

	if refreshed, ok := a.store.GetTask(task.ID); ok {
		task = refreshed
	}
	write(w, http.StatusAccepted, task)
	latencyLog("backend_api", "create_task_response_written", map[string]any{
		"elapsed_ms":  time.Since(startedAt).Milliseconds(),
		"step_ms":     time.Since(stepStartedAt).Milliseconds(),
		"operator_id": operator.ID,
		"task_id":     task.ID,
		"agent_id":    req.AgentID,
		"machine_id":  agent.MachineID,
		"project_id":  req.ProjectID,
		"session_id":  task.SessionID,
	})
}

func (a *API) OptimizeGoal(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req optimizeGoalReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	goal := strings.TrimSpace(req.Goal)
	if req.AgentID == "" || req.ProjectID == "" || goal == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "missing required fields"})
		return
	}
	agent, ok := a.broker.Get(req.AgentID, operator.ID)
	if !ok {
		write(w, http.StatusConflict, map[string]string{"error": "agent offline"})
		return
	}
	projectRoot := ""
	for _, item := range agent.Projects {
		if item.ProjectID == req.ProjectID {
			projectRoot = item.Root
			break
		}
	}
	if projectRoot == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent does not own project"})
		return
	}
	providerID, modelID := splitModelRef(req.Model)
	if providerID == "" || modelID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "请选择用于优化的模型"})
		return
	}
	maxIterations := 30
	if req.MaxIterations > 0 {
		maxIterations = req.MaxIterations
	}
	requestID := fmt.Sprintf("req_goal_optimize_%d", time.Now().UnixNano())
	result, err := a.requestGoalOptimize(r.Context(), operator.ID, req.AgentID, requestID, "goal.optimize", model.GoalOptimizePayload{
		AgentID:       req.AgentID,
		MachineID:     agent.MachineID,
		ProjectID:     req.ProjectID,
		Goal:          goal,
		ProviderID:    providerID,
		ModelID:       modelID,
		Variant:       strings.TrimSpace(req.Variant),
		MaxIterations: maxIterations,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	result.OptimizedGoal = strings.TrimSpace(result.OptimizedGoal)
	if result.OptimizedGoal == "" {
		write(w, http.StatusConflict, map[string]string{"error": "模型未返回优化后的目标"})
		return
	}
	write(w, http.StatusOK, map[string]any{
		"goal":           result.OptimizedGoal,
		"original_goal":  goal,
		"model":          providerID + "/" + modelID,
		"max_iterations": maxIterations,
	})
}

func (a *API) taskSystemPrompt(operatorID int64, agentID string) (string, error) {
	return TaskSystemPrompt(a.store, operatorID, agentID)
}

func TaskSystemPrompt(taskStore *store.Memory, operatorID int64, agentID string) (string, error) {
	globalPrompt, err := settingValue(taskStore, operatorID, userSettingsAgentID, globalPromptSettingKey)
	if err != nil {
		return "", err
	}
	projectPrompt, err := settingValue(taskStore, operatorID, agentID, projectPromptSettingKey)
	if err != nil {
		return "", err
	}
	return buildTaskSystemPrompt(globalPrompt, projectPrompt), nil
}

func (a *API) getSettingValue(operatorID int64, agentID, key string) (string, error) {
	return settingValue(a.store, operatorID, agentID, key)
}

func settingValue(taskStore *store.Memory, operatorID int64, agentID, key string) (string, error) {
	settings, err := taskStore.GetDeviceSettings(operatorID, agentID)
	if err != nil {
		return "", err
	}
	if settings == nil {
		return "", nil
	}
	return strings.TrimSpace(settings[key]), nil
}

func buildTaskSystemPrompt(globalPrompt, projectPrompt string) string {
	var builder strings.Builder
	if globalPrompt != "" {
		builder.WriteString("【全局提示词】\n")
		builder.WriteString(globalPrompt)
	}
	if projectPrompt != "" {
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString("【项目级提示词】\n")
		builder.WriteString(projectPrompt)
	}
	return builder.String()
}

func (a *API) GetTask(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	taskID := chi.URLParam(r, "taskID")
	task, found := a.store.GetTask(taskID)
	if !found || task.OperatorID != operator.ID {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	write(w, http.StatusOK, task)
}

func (a *API) DownloadTaskArtifact(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if a.artifactFetcher == nil {
		write(w, http.StatusNotImplemented, map[string]string{"error": "artifact download unavailable"})
		return
	}
	taskID := chi.URLParam(r, "taskID")
	artifactID := chi.URLParam(r, "artifactID")
	task, found := a.store.GetTask(taskID)
	if !found || task.OperatorID != operator.ID {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	var artifact *model.Artifact
	for i := range task.Artifacts {
		if task.Artifacts[i].ID == artifactID {
			artifact = &task.Artifacts[i]
			break
		}
	}
	if artifact == nil {
		write(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	filename := strings.TrimSpace(artifact.Filename)
	if filename == "" {
		filename = artifact.ID
	}
	contentType := strings.TrimSpace(artifact.MIME)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")
	if artifact.SizeBytes > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", artifact.SizeBytes))
	}
	tw := &trackingWriter{ResponseWriter: w}
	if err := a.artifactFetcher(r.Context(), task, *artifact, tw); err != nil {
		if !tw.wrote {
			write(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		return
	}
}

func (a *API) TaskEvents(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	taskID := chi.URLParam(r, "taskID")
	task, found := a.store.GetTask(taskID)
	if !found || task.OperatorID != operator.ID {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	lastEventID := int64(0)
	if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
		if _, err := fmt.Sscanf(raw, "%d", &lastEventID); err != nil || lastEventID < 0 {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid Last-Event-ID"})
			return
		}
	}
	var eventCursor *store.EventCursor
	if raw := strings.TrimSpace(r.Header.Get("X-Task-Event-Cursor")); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid task event cursor"})
			return
		}
		var cursor store.EventCursor
		if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.SentAt.IsZero() || cursor.ID < 0 {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid task event cursor"})
			return
		}
		eventCursor = &cursor
	}
	snapshotMode := r.URL.Query().Get("snapshot") == "1"
	lastCursor := eventCursor

	flusher, canFlush := w.(http.Flusher)

	sendEvent := func(evt model.Event) {
		if evt.Sequence > 0 {
			if evt.Sequence <= lastEventID {
				return
			}
		} else if lastCursor != nil && !evt.SentAt.IsZero() {
			// Sequence is the SSE Last-Event-ID. The (sent_at, id) cursor
			// mixes live delta IDs with MySQL tool row IDs and must not
			// filter sequenced events or tool completions are skipped.
			if evt.SentAt.Before(lastCursor.SentAt) ||
				(evt.SentAt.Equal(lastCursor.SentAt) && eventCursorID(evt) <= lastCursor.ID) {
				return
			}
		}
		body, _ := json.Marshal(evt)
		fmt.Fprintf(w, "event: %s\n", evt.Type)
		if evt.Sequence > 0 {
			fmt.Fprintf(w, "id: %d\n", evt.Sequence)
		}
		fmt.Fprintf(w, "data: %s\n\n", body)
		if canFlush {
			flusher.Flush()
		}
		if evt.Sequence > lastEventID {
			lastEventID = evt.Sequence
		}
		if !evt.SentAt.IsZero() {
			lastCursor = &store.EventCursor{SentAt: evt.SentAt, ID: eventCursorID(evt)}
		}
	}

	// 订阅新事件（先订阅，再发历史，避免遗漏）
	ch, cancel := a.store.Subscribe(taskID)
	defer cancel()

	isTerminal := func(t string) bool {
		return t == "completed" || t == "failed" || t == "cancelled"
	}
	historyTerminal := false
	sendHistory := func(events []model.Event) {
		for _, evt := range events {
			sendEvent(evt)
			if isTerminal(evt.Type) {
				historyTerminal = true
			}
		}
	}

	// Catch-up must include live memory deltas (not archived) and MySQL
	// tool/status events, merged in time order. Archive-only replay would
	// drop in-progress body text after a background disconnect.
	if snapshotMode {
		cursor := eventCursor
		for {
			page, err := a.store.ListEventPage(taskID, cursor, 200, taskDisplayEventTypes, "")
			if err != nil {
				return
			}
			sendHistory(page.Items)
			if historyTerminal || !page.HasMore || page.NextCursor == nil {
				break
			}
			cursor = page.NextCursor
		}
	} else {
		sendHistory(a.store.CatchUpEvents(taskID, eventCursor, lastEventID, 0))
	}

	// 检查任务是否已经结束。历史窗口可能被截断或内存终态事件已被清掉，
	// 此时必须补发一条终态，不能直接关流，否则客户端会一直停在生成中。
	if latest, ok := a.store.GetTask(taskID); ok {
		task = latest
	}
	if historyTerminal {
		return
	}
	if evt, ok := taskStatusTerminalEvent(task, lastEventID); ok {
		sendEvent(evt)
		return
	}

	// 等待新事件，直到终结或客户端断开
	ctx := r.Context()
	// 心跳，防止代理超时断开
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			if canFlush {
				flusher.Flush()
			}
		case evt, ok := <-ch:
			if !ok {
				return
			}
			sendEvent(evt)
			if isTerminal(evt.Type) {
				return
			}
		}
	}
}

// TaskEventPage serves only events needed to rebuild the chat presentation.
// It is intentionally separate from the live SSE stream: historical tasks are
// read in bounded keyset pages and never materialized into Memory.
func (a *API) TaskEventPage(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	taskID := chi.URLParam(r, "taskID")
	task, found := a.store.GetTask(taskID)
	if !found || task.OperatorID != operator.ID {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 200 {
		limit = 200
	}
	var cursor *store.EventCursor
	if raw := strings.TrimSpace(r.URL.Query().Get("cursor")); raw != "" {
		decoded, err := decodeEventCursor(raw)
		if err != nil {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid cursor"})
			return
		}
		cursor = decoded
	}
	page, err := a.store.ListEventPage(taskID, cursor, limit, taskDisplayEventTypes, "")
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "load task event page failed"})
		return
	}
	nextCursor := ""
	if page.NextCursor != nil {
		nextCursor = encodeEventCursor(*page.NextCursor)
	}
	write(w, http.StatusOK, map[string]any{
		"task_id":     taskID,
		"task_status": task.Status,
		"events":      page.Items,
		"next_cursor": nextCursor,
		"has_more":    page.HasMore,
	})
}

func (a *API) TaskAgentHistory(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	taskID := chi.URLParam(r, "taskID")
	task, found := a.store.GetTask(taskID)
	if !found || task.OperatorID != operator.ID {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 200 {
		limit = 200
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	includeChildren := r.URL.Query().Get("include_children") != "0"
	writeHistory := func(source, errorCode, nextCursor string, events []model.Event, hasMore, complete bool) {
		if events == nil {
			events = []model.Event{}
		}
		log.Printf("[history] task_id=%s source=%s error_code=%s events=%d has_more=%v", taskID, source, errorCode, len(events), hasMore)
		write(w, http.StatusOK, map[string]any{
			"task_id":     taskID,
			"task_status": task.Status,
			"session_id":  task.SessionID,
			"source":      source,
			"error_code":  errorCode,
			"events":      events,
			"next_cursor": nextCursor,
			"has_more":    hasMore,
			"complete":    complete,
		})
	}

	if a.sessionHistoryRequest != nil && strings.TrimSpace(task.SessionID) != "" && strings.TrimSpace(task.AgentID) != "" {
		if agent, ok := a.broker.Get(task.AgentID, operator.ID); ok && !agentHasCapability(agent, "session_history_v1") {
			// Old online agents would otherwise accept the envelope and never reply.
		} else {
			requestID := fmt.Sprintf("req_hist_%s_%d", task.ID, time.Now().UnixNano())
			result, err := a.sessionHistoryRequest(r.Context(), operator.ID, task.AgentID, requestID, model.SessionHistoryRequestPayload{
				TaskID:          task.ID,
				SessionID:       task.SessionID,
				AgentID:         task.AgentID,
				MachineID:       task.MachineID,
				ProjectID:       task.ProjectID,
				Cursor:          cursor,
				Limit:           limit,
				IncludeChildren: includeChildren,
			})
			if err == nil && result.ErrorCode != "history_not_found" && result.ErrorCode != "agent_offline" && (len(result.Events) > 0 || result.HasMore) {
				source := strings.TrimSpace(result.Source)
				if source == "" {
					source = "agent_local"
				}
				events := appendTerminalHistoryEvent(task, result.Events, result.HasMore)
				writeHistory(source, result.ErrorCode, result.NextCursor, events, result.HasMore, result.Complete || !result.HasMore)
				return
			}
			if err != nil && !errors.Is(err, broker.ErrOffline) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				log.Printf("[history] agent history failed task_id=%s err=%v", taskID, err)
			}
		}
	}

	var eventCursor *store.EventCursor
	if cursor != "" {
		if decoded, err := decodeEventCursor(cursor); err == nil {
			eventCursor = decoded
		}
	}
	page, err := a.store.ListEventPage(taskID, eventCursor, limit, taskDisplayEventTypes, "")
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "load task history failed"})
		return
	}
	nextCursor := ""
	if page.NextCursor != nil {
		nextCursor = encodeEventCursor(*page.NextCursor)
	}
	source := "mysql_legacy"
	errorCode := ""
	if len(page.Items) == 0 && !page.HasMore {
		source = "task_index_only"
		if strings.TrimSpace(task.SessionID) != "" {
			errorCode = "agent_offline"
		}
	}
	writeHistory(source, errorCode, nextCursor, page.Items, page.HasMore, !page.HasMore)
}

func historyHasTerminal(events []model.Event) bool {
	for _, event := range events {
		switch event.Type {
		case "completed", "failed", "cancelled":
			return true
		}
	}
	return false
}

func appendTerminalHistoryEvent(task *model.Task, events []model.Event, hasMore bool) []model.Event {
	if task == nil || hasMore || historyHasTerminal(events) {
		return events
	}
	lastSequence := int64(len(events))
	if len(events) > 0 && events[len(events)-1].Sequence > 0 {
		lastSequence = events[len(events)-1].Sequence
	}
	evt, ok := taskStatusTerminalEvent(task, lastSequence)
	if !ok {
		return events
	}
	if !task.UpdatedAt.IsZero() {
		evt.SentAt = task.UpdatedAt.UTC()
	}
	return append(events, evt)
}

func taskStatusTerminalEvent(task *model.Task, lastSequence int64) (model.Event, bool) {
	if task == nil {
		return model.Event{}, false
	}
	eventType := ""
	switch task.Status {
	case model.TaskCompleted:
		eventType = "completed"
	case model.TaskFailed:
		eventType = "failed"
	case model.TaskCancelled:
		eventType = "cancelled"
	default:
		return model.Event{}, false
	}
	sequence := lastSequence + 1
	if sequence < 1 {
		sequence = 1
	}
	return model.Event{
		TaskID:    task.ID,
		SessionID: task.SessionID,
		Type:      eventType,
		Content:   task.Result,
		Error:     task.Error,
		Sequence:  sequence,
		SentAt:    time.Now().UTC(),
	}, true
}

func agentHasCapability(agent model.Agent, capability string) bool {
	for _, item := range agent.Capabilities {
		if item == capability {
			return true
		}
	}
	return false
}

func eventCursorID(event model.Event) int64 {
	if event.ID > 0 {
		return event.ID
	}
	return event.Sequence
}

var taskDisplayEventTypes = []string{
	"started", "delta", "input_applied", "retrying", "progress",
	"compaction_started", "compaction_completed", "tool_updated",
	"subagent_started", "subagent_result", "goal_created", "goal_heartbeat", "goal_continued",
	"goal_checkpoint", "goal_completed", "goal_paused", "goal_failed",
	"waiting_approval", "completed", "failed", "cancelling", "cancelled",
	"plan_updated",
}

func (a *API) CancelTask(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	taskID := chi.URLParam(r, "taskID")
	// 先检查归属
	if t, found := a.store.GetTask(taskID); !found || t.OperatorID != operator.ID {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	task, ok := a.store.MarkCancelling(taskID, "", "正在停止任务")
	if !ok {
		if task, found := a.store.GetTask(taskID); found {
			write(w, http.StatusOK, task)
			return
		}
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	env := model.Envelope{
		Type:      "task.cancel",
		RequestID: fmt.Sprintf("req_cancel_%s", task.ID),
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.CancelPayload{
			TaskID: task.ID,
		},
	}
	_ = a.broker.DispatchForOperator(context.Background(), operator.ID, task.AgentID, env)

	a.store.AddEvent(task.ID, model.Event{
		TaskID:  task.ID,
		Type:    "cancelling",
		SentAt:  time.Now().UTC(),
		Content: "正在停止任务",
		Error:   "正在停止任务",
	})

	write(w, http.StatusOK, task)
}

func (a *API) ApproveTask(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	taskID := chi.URLParam(r, "taskID")
	task, found := a.store.GetTask(taskID)
	if !found || task.OperatorID != operator.ID {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if task.Status != model.TaskWaitingApproval || task.Approval == nil {
		write(w, http.StatusConflict, map[string]string{"error": "task is not waiting approval"})
		return
	}

	var req approveTaskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Reply != "once" && req.Reply != "always" && req.Reply != "reject" {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid reply"})
		return
	}
	if req.PermissionID == "" {
		req.PermissionID = task.Approval.PermissionID
	}
	if req.PermissionID != task.Approval.PermissionID {
		write(w, http.StatusBadRequest, map[string]string{"error": "permission_id mismatch"})
		return
	}

	env := model.Envelope{
		Type:      "task.approval_response",
		RequestID: fmt.Sprintf("req_approval_%s", task.ID),
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.ApprovalResponsePayload{
			TaskID:       task.ID,
			PermissionID: req.PermissionID,
			Reply:        req.Reply,
			Message:      req.Message,
		},
	}

	if err := a.broker.DispatchForOperator(context.Background(), operator.ID, task.AgentID, env); err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	a.store.AddEvent(task.ID, model.Event{
		TaskID:       task.ID,
		Type:         "approval_requested",
		SessionID:    task.SessionID,
		PermissionID: req.PermissionID,
		Reply:        req.Reply,
		Content:      "approval response dispatched",
		SentAt:       time.Now().UTC(),
	})

	task, _ = a.store.GetTask(task.ID)
	write(w, http.StatusAccepted, task)
}

func (a *API) AnswerTaskQuestion(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	taskID := chi.URLParam(r, "taskID")
	task, found := a.store.GetTask(taskID)
	if !found || task.OperatorID != operator.ID {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if task.Question == nil {
		write(w, http.StatusConflict, map[string]string{"error": "task has no pending question"})
		return
	}

	var req answerTaskQuestionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.RequestID == "" {
		req.RequestID = task.Question.RequestID
	}
	if req.RequestID != task.Question.RequestID {
		write(w, http.StatusBadRequest, map[string]string{"error": "request_id mismatch"})
		return
	}
	if !req.Rejected && len(req.Answers) != len(task.Question.Questions) {
		write(w, http.StatusBadRequest, map[string]string{"error": "answers length mismatch"})
		return
	}
	if !req.Rejected && len(req.Answers) == 0 {
		write(w, http.StatusBadRequest, map[string]string{"error": "answers required"})
		return
	}

	env := model.Envelope{
		Type:      "task.question_response",
		RequestID: fmt.Sprintf("req_question_%s", task.ID),
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.QuestionResponsePayload{
			TaskID:    task.ID,
			RequestID: req.RequestID,
			Answers:   req.Answers,
			Rejected:  req.Rejected,
		},
	}

	if err := a.broker.DispatchForOperator(context.Background(), operator.ID, task.AgentID, env); err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	a.store.AddEvent(task.ID, model.Event{
		TaskID:    task.ID,
		Type:      "question_response_requested",
		SessionID: task.SessionID,
		Question:  task.Question,
		Content:   "question response dispatched",
		SentAt:    time.Now().UTC(),
	})

	task, _ = a.store.GetTask(task.ID)
	write(w, http.StatusAccepted, task)
}

func (a *API) GetCaptcha(w http.ResponseWriter, r *http.Request) {
	id, b64, err := captcha.Generate()
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "生成验证码失败"})
		return
	}
	write(w, http.StatusOK, map[string]string{"captcha_id": id, "captcha_image": b64})
}

func (a *API) SendEmailCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email     string `json:"email"`
		CaptchaID string `json:"captcha_id"`
		Captcha   string `json:"captcha"`
		Purpose   string `json:"purpose"` // "register" 或 "reset_password"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "参数错误"})
		return
	}
	if req.Email == "" || req.CaptchaID == "" || req.Captcha == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "邮箱和验证码不能为空"})
		return
	}
	if !captcha.Verify(req.CaptchaID, req.Captcha) {
		write(w, http.StatusBadRequest, map[string]string{"error": "图形验证码错误或已过期"})
		return
	}
	exists, err := a.store.EmailExists(req.Email)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "服务器内部错误"})
		return
	}
	// 注册时邮箱不能已存在，修改密码时邮箱必须存在
	if req.Purpose == "reset_password" {
		if !exists {
			write(w, http.StatusBadRequest, map[string]string{"error": "该邮箱未注册"})
			return
		}
	} else {
		if exists {
			write(w, http.StatusConflict, map[string]string{"error": "该邮箱已被注册"})
			return
		}
	}
	if err := a.mailer.SendCode(req.Email); err != nil {
		write(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]string{"message": "验证码已发送"})
}

func (a *API) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		Email     string `json:"email"`
		EmailCode string `json:"email_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "参数错误"})
		return
	}
	if req.Username == "" || req.Password == "" || req.Email == "" || req.EmailCode == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "所有字段不能为空"})
		return
	}
	if len(req.Username) < 3 || len(req.Username) > 32 {
		write(w, http.StatusBadRequest, map[string]string{"error": "用户名长度 3-32 位"})
		return
	}
	if len(req.Password) < 6 {
		write(w, http.StatusBadRequest, map[string]string{"error": "密码至少 6 位"})
		return
	}
	if !a.mailer.VerifyCode(req.Email, req.EmailCode) {
		write(w, http.StatusBadRequest, map[string]string{"error": "邮箱验证码错误或已过期"})
		return
	}
	exists, err := a.store.UsernameExists(req.Username)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "服务器内部错误"})
		return
	}
	if exists {
		write(w, http.StatusConflict, map[string]string{"error": "用户名已存在"})
		return
	}
	emailExists, err := a.store.EmailExists(req.Email)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "服务器内部错误"})
		return
	}
	if emailExists {
		write(w, http.StatusConflict, map[string]string{"error": "该邮箱已被注册"})
		return
	}
	operator, err := a.store.CreateOperator(req.Username, req.Password, req.Email, req.Username)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "注册失败: " + err.Error()})
		return
	}
	token, err := a.auth.Issue(*operator)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "生成令牌失败"})
		return
	}
	write(w, http.StatusOK, map[string]any{
		"access_token": token,
		"operator":     operator,
	})
}

func (a *API) GetDeviceSettings(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	agentID := chi.URLParam(r, "agentID")
	settings, err := a.store.GetDeviceSettings(operator.ID, agentID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if settings == nil {
		settings = map[string]string{}
	}
	delete(settings, deviceDeletedAgentKey)
	write(w, http.StatusOK, settings)
}

func (a *API) GetDeviceAIConfig(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_ai_config_get_%d", time.Now().UnixNano())
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	result, err := a.requestDeviceAIConfig(r.Context(), operator.ID, launcherID, requestID, "device.ai_config.get", model.DeviceAIConfigGetPayload{
		AgentID: machineID,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	enrichDeviceAIConfigResult(&result)
	a.updateLauncherModelCache(operator.ID, machineID, result)
	write(w, http.StatusOK, result)
}

func (a *API) ListDeviceAIModels(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req listDeviceAIModelsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	requestID := fmt.Sprintf("req_ai_models_%d", time.Now().UnixNano())
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	result, err := a.requestDeviceAIConfig(r.Context(), operator.ID, launcherID, requestID, "device.ai_config.list_models", model.DeviceAIConfigListModelsPayload{
		AgentID:    machineID,
		Provider:   strings.TrimSpace(req.Provider),
		BaseURL:    strings.TrimSpace(req.BaseURL),
		ConsoleURL: strings.TrimSpace(req.ConsoleURL),
		APIKey:     strings.TrimSpace(req.APIKey),
		APIMode:    normalizeDeviceAIAPIMode(req.APIMode),
		Config: model.DeviceAIConfig{
			Provider:   strings.TrimSpace(req.Provider),
			BaseURL:    strings.TrimSpace(req.BaseURL),
			ConsoleURL: strings.TrimSpace(req.ConsoleURL),
			APIKey:     strings.TrimSpace(req.APIKey),
			APIMode:    normalizeDeviceAIAPIMode(req.APIMode),
			Model:      strings.TrimSpace(req.Model),
			Models:     normalizeDeviceAIModels(strings.TrimSpace(req.Provider), req.Models),
		},
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	enrichDeviceAIConfigResult(&result)
	a.updateLauncherModelCache(operator.ID, machineID, result)
	write(w, http.StatusOK, result)
}

func (a *API) SaveDeviceAIConfig(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req saveDeviceAIConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	requestID := fmt.Sprintf("req_ai_save_%d", time.Now().UnixNano())
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	result, err := a.requestDeviceAIConfig(r.Context(), operator.ID, launcherID, requestID, "device.ai_config.save", model.DeviceAIConfigSavePayload{
		AgentID:    machineID,
		Provider:   strings.TrimSpace(req.Provider),
		BaseURL:    strings.TrimSpace(req.BaseURL),
		ConsoleURL: strings.TrimSpace(req.ConsoleURL),
		APIKey:     strings.TrimSpace(req.APIKey),
		APIMode:    normalizeDeviceAIAPIMode(req.APIMode),
		Force:      req.Force,
		Config: model.DeviceAIConfig{
			Provider:   strings.TrimSpace(req.Provider),
			BaseURL:    strings.TrimSpace(req.BaseURL),
			ConsoleURL: strings.TrimSpace(req.ConsoleURL),
			APIKey:     strings.TrimSpace(req.APIKey),
			APIMode:    normalizeDeviceAIAPIMode(req.APIMode),
			Model:      strings.TrimSpace(req.Model),
			Models:     normalizeDeviceAIModels(strings.TrimSpace(req.Provider), req.Models),
		},
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	enrichDeviceAIConfigResult(&result)
	a.updateLauncherModelCache(operator.ID, machineID, result)
	write(w, http.StatusOK, result)
}

func (a *API) SaveDeviceAIConfigText(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req saveDeviceAIConfigTextReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	requestID := fmt.Sprintf("req_ai_save_text_%d", time.Now().UnixNano())
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	result, err := a.requestDeviceAIConfig(r.Context(), operator.ID, launcherID, requestID, "device.ai_config.save_text", model.DeviceAIConfigTextSavePayload{
		AgentID: machineID,
		Text:    req.Text,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	enrichDeviceAIConfigResult(&result)
	a.updateLauncherModelCache(operator.ID, machineID, result)
	write(w, http.StatusOK, result)
}

func (a *API) ClearDeviceAIProvider(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req clearDeviceAIProviderReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	requestID := fmt.Sprintf("req_ai_clear_provider_%d", time.Now().UnixNano())
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	result, err := a.requestDeviceAIConfig(r.Context(), operator.ID, launcherID, requestID, "device.ai_config.clear_provider", model.DeviceAIConfigProviderCleanupPayload{
		AgentID:  machineID,
		Provider: strings.TrimSpace(req.Provider),
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	enrichDeviceAIConfigResult(&result)
	a.updateLauncherModelCache(operator.ID, machineID, result)
	write(w, http.StatusOK, result)
}

func (a *API) GetDeviceMCPConfig(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_mcp_get_%d", time.Now().UnixNano())
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	result, err := a.requestDeviceMCPConfig(r.Context(), operator.ID, launcherID, requestID, "device.mcp_config.get", model.DeviceMCPConfigGetPayload{
		AgentID: machineID,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) SaveDeviceMCPConfig(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req saveDeviceMCPConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	requestID := fmt.Sprintf("req_mcp_save_%d", time.Now().UnixNano())
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	serverNames := make([]string, 0, len(req.Servers))
	for _, srv := range req.Servers {
		serverNames = append(serverNames, srv.Name)
	}
	latencyLog("backend_api", "device_mcp_config_save", map[string]any{
		"operator_id":  operator.ID,
		"machine_id":   machineID,
		"server_names": serverNames,
		"server_count": len(serverNames),
	})
	result, err := a.requestDeviceMCPConfig(r.Context(), operator.ID, launcherID, requestID, "device.mcp_config.save", model.DeviceMCPConfigSavePayload{
		AgentID: machineID,
		Servers: req.Servers,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) RemoveDeviceMCPConfig(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req removeDeviceMCPConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	requestID := fmt.Sprintf("req_mcp_remove_%d", time.Now().UnixNano())
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	latencyLog("backend_api", "device_mcp_config_remove", map[string]any{
		"operator_id": operator.ID,
		"machine_id":  machineID,
		"name":        strings.TrimSpace(req.Name),
	})
	result, err := a.requestDeviceMCPConfig(r.Context(), operator.ID, launcherID, requestID, "device.mcp_config.remove", model.DeviceMCPConfigRemovePayload{
		AgentID: machineID,
		Name:    strings.TrimSpace(req.Name),
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) GetDeviceAgentMCPSelection(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "缺少 agent_id"})
		return
	}
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_mcp_agent_get_%d", time.Now().UnixNano())
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	result, err := a.requestDeviceMCPConfig(r.Context(), operator.ID, launcherID, requestID, "device.mcp_config.agent_get", model.DeviceAgentMCPSelectionPayload{
		AgentID: agentID,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) SaveDeviceAgentMCPSelection(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "缺少 agent_id"})
		return
	}
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req saveDeviceAgentMCPSelectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	requestID := fmt.Sprintf("req_mcp_agent_save_%d", time.Now().UnixNano())
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	result, err := a.requestDeviceMCPConfig(r.Context(), operator.ID, launcherID, requestID, "device.mcp_config.agent_save", model.DeviceAgentMCPSelectionPayload{
		AgentID: agentID,
		Mode:    strings.TrimSpace(req.Mode),
		Servers: req.Servers,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) GetDeviceAgentSkillSelection(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "缺少 agent_id"})
		return
	}
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_skill_agent_get_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.skills_get", model.DeviceAgentSkillSelectionPayload{
		AgentID: agentID,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) SaveDeviceAgentSkillSelection(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "缺少 agent_id"})
		return
	}
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req model.DeviceAgentSkillSelectionPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	selected := make([]string, 0, len(req.ExtraSkillIDs))
	seen := map[string]bool{}
	for _, rawID := range req.ExtraSkillIDs {
		id := strings.TrimSpace(rawID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		selected = append(selected, id)
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_skill_agent_save_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.skills_save", model.DeviceAgentSkillSelectionPayload{
		AgentID:       agentID,
		ExtraSkillIDs: selected,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) ImportDeviceAgentSkill(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "缺少 agent_id"})
		return
	}
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req model.DeviceAgentSkillImportPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 12<<20)).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.AgentID = agentID
	if strings.TrimSpace(req.Skill.Name) == "" || strings.TrimSpace(req.Skill.Content) == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "Skill 名称和 SKILL.md 内容不能为空"})
		return
	}
	if len(req.Skill.Content) > 2<<20 {
		write(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "SKILL.md 超过 2 MB"})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_skill_import_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.skills_import", req)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) GetDeviceSkillConfig(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	result, err := a.requestDeviceLauncher(
		r.Context(), operator.ID, launcherID,
		fmt.Sprintf("req_skill_global_get_%d", time.Now().UnixNano()),
		"device.launcher.skills_global_get",
		map[string]string{"machine_id": machineID},
	)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if result.SkillConfig == nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 未返回设备 Skill 配置"})
		return
	}
	write(w, http.StatusOK, result.SkillConfig)
}

func (a *API) SaveDeviceSkillConfig(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req model.DeviceSkillConfigInfo
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.MachineID = machineID
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	result, err := a.requestDeviceLauncher(
		r.Context(), operator.ID, launcherID,
		fmt.Sprintf("req_skill_global_save_%d", time.Now().UnixNano()),
		"device.launcher.skills_global_save", req,
	)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if result.SkillConfig == nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 未返回设备 Skill 配置"})
		return
	}
	write(w, http.StatusOK, result.SkillConfig)
}

func (a *API) ImportDeviceSkill(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req model.DeviceSkillImportPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 12<<20)).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	result, err := a.requestDeviceLauncher(
		r.Context(), operator.ID, launcherID,
		fmt.Sprintf("req_skill_global_import_%d", time.Now().UnixNano()),
		"device.launcher.skills_global_import", req,
	)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if result.SkillConfig == nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 未返回设备 Skill 配置"})
		return
	}
	write(w, http.StatusOK, result.SkillConfig)
}

func (a *API) enabledSkillCatalog() ([]model.Skill, error) {
	if err := a.ensureDefaultSemanticAgentAssets(); err != nil {
		return nil, err
	}
	items, err := a.store.ListSkills(false)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].Description = normalizeAssetDescription(items[index].Description, items[index].Content)
		items[index].Content = strings.TrimSpace(items[index].Content)
	}
	return items, nil
}

func semanticSkillDefinitions(items []model.Skill) []model.SemanticAgentSkill {
	defs := make([]model.SemanticAgentSkill, 0, len(items))
	for _, item := range items {
		defs = append(defs, model.SemanticAgentSkill{
			Name:         item.ID,
			Description:  item.Description,
			Content:      item.Content,
			PackageFiles: cloneSkillPackageFiles(item.PackageFiles),
		})
	}
	return defs
}

func (a *API) GetDeviceAgentSemanticSelection(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "缺少 agent_id"})
		return
	}
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_semantic_agent_get_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.semantic_agent_get", model.DeviceAgentSemanticSelectionPayload{
		AgentID: agentID,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if result.SemanticSelection != nil {
		if profiles, err := a.semanticAgentProfiles(false); err == nil {
			enrichSemanticSelection(result.SemanticSelection, profiles)
		}
	}
	write(w, http.StatusOK, result)
}

func (a *API) SaveDeviceAgentSemanticSelection(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "缺少 agent_id"})
		return
	}
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req saveDeviceAgentSemanticSelectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	profile, err := a.semanticAgentByID(req.SemanticAgentID, false)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_semantic_agent_save_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.semantic_agent_save", model.DeviceAgentSemanticSelectionPayload{
		AgentID:             agentID,
		SemanticAgentID:     profile.ID,
		ApplyRecommendedMCP: req.ApplyRecommendedMCP,
		DisableVerifyMCP:    req.DisableVerifyMCP,
		SemanticAgent:       &profile,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if result.SemanticSelection != nil {
		if profiles, err := a.semanticAgentProfiles(false); err == nil {
			enrichSemanticSelection(result.SemanticSelection, profiles)
		}
	}
	write(w, http.StatusOK, result)
}

func (a *API) GetDeviceEnvConfig(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_env_get_%d", time.Now().UnixNano())
	result, err := a.requestDeviceEnvConfig(r.Context(), operator.ID, launcherID, requestID, "device.env_config.get", model.DeviceEnvConfigGetPayload{MachineID: machineID})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) SaveDeviceEnvConfig(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req saveDeviceEnvConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_env_save_%d", time.Now().UnixNano())
	result, err := a.requestDeviceEnvConfig(r.Context(), operator.ID, launcherID, requestID, "device.env_config.save", model.DeviceEnvConfigSavePayload{
		MachineID:         machineID,
		GlobalEnvironment: req.GlobalEnvironment,
		AgentID:           strings.TrimSpace(req.AgentID),
		AgentEnvironment:  req.AgentEnvironment,
		ExpectedRevision:  strings.TrimSpace(req.ExpectedRevision),
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) GetDeviceAgentCompactionConfig(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "缺少 agent_id"})
		return
	}
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	launcherID, err := a.deviceLauncherAgentWithCapability(operator.ID, machineID, launcherCompactionConfigCapability)
	if err != nil {
		message := "launcher 不在线或无权限"
		if strings.Contains(err.Error(), "不支持") {
			message = err.Error()
		}
		write(w, http.StatusConflict, map[string]string{"error": message})
		return
	}
	requestID := fmt.Sprintf("req_compaction_config_get_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.compaction_config.get", map[string]any{
		"machine_id": machineID,
		"agent_id":   agentID,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) SaveDeviceAgentCompactionConfig(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "缺少 agent_id"})
		return
	}
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req struct {
		ThresholdPercent int `json:"threshold_percent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	launcherID, err := a.deviceLauncherAgentWithCapability(operator.ID, machineID, launcherCompactionConfigCapability)
	if err != nil {
		message := "launcher 不在线或无权限"
		if strings.Contains(err.Error(), "不支持") {
			message = err.Error()
		}
		write(w, http.StatusConflict, map[string]string{"error": message})
		return
	}
	requestID := fmt.Sprintf("req_compaction_config_save_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.compaction_config.save", map[string]any{
		"machine_id":        machineID,
		"agent_id":          agentID,
		"threshold_percent": req.ThresholdPercent,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) ListMCPCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentOperator(r); !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	items, err := a.mcpLibraryCatalogItems()
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, mcpCatalogResponse{Items: items})
}

func (a *API) mcpLibraryCatalogItems() ([]mcpCatalogItem, error) {
	catalog, err := a.store.ListMCPCatalog(false)
	if err != nil {
		return nil, err
	}
	items := make([]mcpCatalogItem, 0, len(catalog))
	for _, item := range catalog {
		if isLegacyReverseMCPCatalogItem(item) {
			continue
		}
		items = append(items, mcpCatalogItemFromCatalog(item))
	}
	return items, nil
}

func mcpCatalogItemFromCatalog(item model.MCPCatalogItem) mcpCatalogItem {
	server := item.Config
	server.Name = firstNonEmpty(server.Name, item.Name)
	server.Type = firstNonEmpty(server.Type, item.Type)
	server.Enabled = false
	return mcpCatalogItem{
		ID:            "catalog:" + item.ID,
		Title:         firstNonEmpty(item.Title, item.Name),
		Category:      firstNonEmpty(firstCatalogTag(item.Tags), item.Category, "MCP库"),
		Description:   item.Description,
		Source:        firstNonEmpty(item.Source, "MCP库"),
		SourceURL:     item.SourceURL,
		CredentialURL: item.CredentialURL,
		Install:       item.Install,
		LaunchReady:   item.LaunchReady,
		LaunchReason:  item.LaunchBlockReason,
		Server:        server,
	}
}

func firstCatalogTag(tags []string) string {
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			return tag
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (a *API) requestDeviceAIConfig(
	ctx context.Context,
	operatorID int64,
	agentID string,
	requestID string,
	envType string,
	payload any,
) (model.DeviceAIConfigResultPayload, error) {
	if a.aiConfigRequest == nil {
		return model.DeviceAIConfigResultPayload{}, errors.New("设备AI配置能力未启用")
	}
	return a.aiConfigRequest(ctx, operatorID, agentID, requestID, envType, payload)
}

func (a *API) requestGoalOptimize(
	ctx context.Context,
	operatorID int64,
	agentID string,
	requestID string,
	envType string,
	payload any,
) (model.GoalOptimizeResultPayload, error) {
	if a.goalOptimizeRequest == nil {
		return model.GoalOptimizeResultPayload{}, errors.New("目标提示词优化能力未启用")
	}
	return a.goalOptimizeRequest(ctx, operatorID, agentID, requestID, envType, payload)
}

func (a *API) requestDeviceMCPConfig(
	ctx context.Context,
	operatorID int64,
	agentID string,
	requestID string,
	envType string,
	payload any,
) (model.DeviceMCPConfigResultPayload, error) {
	if a.mcpConfigRequest == nil {
		return model.DeviceMCPConfigResultPayload{}, errors.New("设备MCP配置能力未启用")
	}
	return a.mcpConfigRequest(ctx, operatorID, agentID, requestID, envType, payload)
}

func (a *API) requestDeviceEnvConfig(
	ctx context.Context,
	operatorID int64,
	agentID string,
	requestID string,
	envType string,
	payload any,
) (model.DeviceEnvConfigResultPayload, error) {
	if a.envConfigRequest == nil {
		return model.DeviceEnvConfigResultPayload{}, errors.New("设备环境变量配置能力未启用")
	}
	return a.envConfigRequest(ctx, operatorID, agentID, requestID, envType, payload)
}

func (a *API) requestDeviceLauncher(
	ctx context.Context,
	operatorID int64,
	agentID string,
	requestID string,
	envType string,
	payload any,
) (model.DeviceLauncherResultPayload, error) {
	if a.launcherRequest == nil {
		return model.DeviceLauncherResultPayload{}, errors.New("设备 launcher 能力未启用")
	}
	return a.launcherRequest(ctx, operatorID, agentID, requestID, envType, payload)
}

func (a *API) requestDeviceDirectories(
	ctx context.Context,
	operatorID int64,
	deviceID string,
	requestID string,
	envType string,
	payload any,
) (model.DeviceDirectoriesResultPayload, error) {
	if a.directoryRequest == nil {
		return model.DeviceDirectoriesResultPayload{}, errors.New("设备目录能力未启用")
	}
	return a.directoryRequest(ctx, operatorID, deviceID, requestID, envType, payload)
}

func (a *API) GetDeviceLauncherState(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_launcher_get_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.get", map[string]string{"machine_id": machineID})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	allowAll, err := a.deviceAllowAllDirectories(operator.ID, machineID)
	if err == nil {
		if result.State == nil {
			result.State = &model.DeviceLauncherState{}
		}
		result.State.AllowAllDirectories = allowAll
	}
	write(w, http.StatusOK, result)
}

func (a *API) GetDeviceLauncherDiagnostics(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	logLines := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("log_lines")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil {
			logLines = parsed
		}
	}
	payload := map[string]any{
		"agent_id":     strings.TrimSpace(r.URL.Query().Get("agent_id")),
		"include_logs": r.URL.Query().Get("include_logs") != "0",
		"log_lines":    logLines,
	}
	requestCtx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	result, err := a.requestDeviceLauncher(requestCtx, operator.ID, launcherID,
		fmt.Sprintf("launcher_diagnostics_%d", time.Now().UnixNano()),
		"device.launcher.diagnostics", payload)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"diagnostics": result.Diagnostics})
}

func (a *API) SetupDeviceLauncherSSH(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_launcher_ssh_setup_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID,
		"device.launcher.ssh_setup", model.DeviceLauncherSSHSetupPayload{MachineID: machineID})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) GetDeviceAgentMCPStatus(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent_id 不能为空"})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_launcher_mcp_status_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.mcp_status", map[string]string{
		"machine_id": machineID,
		"agent_id":   agentID,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) UpgradeDeviceLauncher(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req launcherUpgradeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	requestID := fmt.Sprintf("req_launcher_upgrade_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.upgrade", model.DeviceLauncherUpgradePayload{
		MachineID:     machineID,
		TargetVersion: strings.TrimSpace(req.TargetVersion),
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) RollbackDeviceLauncher(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_launcher_rollback_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.rollback", map[string]string{"machine_id": machineID})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) GetDeviceLauncherAutostart(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_launcher_autostart_status_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.autostart_status", map[string]string{"machine_id": machineID})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) EnableDeviceLauncherAutostart(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req launcherAutostartReq
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	requestID := fmt.Sprintf("req_launcher_autostart_enable_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.autostart_enable", model.DeviceLauncherAutostartPayload{
		MachineID: machineID,
		DryRun:    req.DryRun,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) DisableDeviceLauncherAutostart(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_launcher_autostart_disable_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.autostart_disable", map[string]string{"machine_id": machineID})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) SelfUpdateDeviceLauncher(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req launcherSelfUpdateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	requestID := fmt.Sprintf("req_launcher_self_update_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.self_update", model.DeviceLauncherSelfUpdatePayload{
		MachineID:     machineID,
		TargetVersion: strings.TrimSpace(req.TargetVersion),
		BaseURL:       strings.TrimSpace(req.BaseURL),
		Apply:         req.Apply,
		Restart:       req.Restart,
		DryRun:        req.DryRun,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

// GetDeviceLauncherStorage 按需查询设备 launcher 运行目录的磁盘占用。
// 目录遍历开销较大，因此单独走接口而不是随心跳上报。
func (a *API) GetDeviceLauncherStorage(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgentWithCapability(operator.ID, machineID, launcherStorageCapability)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": launcherCapabilityMessage(err, "设备")})
		return
	}
	// 运行目录可达数 GB、数万个文件，遍历耗时可能超过默认的 30 秒。
	requestCtx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := a.requestDeviceLauncher(requestCtx, operator.ID, launcherID,
		fmt.Sprintf("req_launcher_storage_%d", time.Now().UnixNano()),
		"device.launcher.storage", map[string]string{"machine_id": machineID})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

// ClearDeviceLauncherStorage 清理设备上可再生成的缓存目录。
// 运行时组件、Agent 数据与程序文件不在可清理范围内。
func (a *API) ClearDeviceLauncherStorage(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgentWithCapability(operator.ID, machineID, launcherStorageCapability)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": launcherCapabilityMessage(err, "设备")})
		return
	}
	var req launcherStorageClearReq
	if r.Body != nil {
		// 允许空 body：表示清理全部可清理类别。
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	requestCtx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := a.requestDeviceLauncher(requestCtx, operator.ID, launcherID,
		fmt.Sprintf("req_launcher_storage_clear_%d", time.Now().UnixNano()),
		"device.launcher.storage_clear", map[string]any{
			"machine_id": machineID,
			"keys":       normalizeStorageKeys(req.Keys),
		})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

// launcherCapabilityMessage 把能力校验失败转换成面向用户的提示。
// launcher 版本过低时直接说明原因，避免用户误以为设备离线。
func launcherCapabilityMessage(err error, subject string) string {
	if err != nil && strings.Contains(err.Error(), "不支持") {
		return "设备端 launcher 版本过低，请先在设备上更新 launcher"
	}
	return subject + "不在线或无权限"
}

// launcherStorageClearReq 是清理请求体；keys 为空表示清理全部可清理类别。
type launcherStorageClearReq struct {
	Keys []string `json:"keys"`
}

// normalizeStorageKeys 去掉空白项并去重，避免把空字符串当成类别下发。
func normalizeStorageKeys(keys []string) []string {
	if len(keys) == 0 {
		return []string{}
	}
	seen := make(map[string]bool, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

func (a *API) RequestDeviceLauncherDirectoryPermission(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req model.DeviceLauncherDirectoryPermissionPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.MachineID = machineID
	requestID := fmt.Sprintf("req_launcher_directory_permission_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.directory_permission", req)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) GetDeviceDirectories(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	allowAll, err := a.deviceAllowAllDirectories(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	requestID := fmt.Sprintf("req_directories_%d", time.Now().UnixNano())
	result, err := a.requestDeviceDirectories(r.Context(), operator.ID, launcherID, requestID, "device.directories.get", model.DeviceDirectoriesGetPayload{
		MachineID: machineID,
		Path:      strings.TrimSpace(r.URL.Query().Get("path")),
		AllowAll:  allowAll,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) CreateDeviceDirectoryFileUpload(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceDirectoryFile(w, r, "device.directory_files.upload_create", "req_directory_upload_create", true)
}

func (a *API) UploadDeviceDirectoryFileChunk(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceDirectoryFile(w, r, "device.directory_files.upload_chunk", "req_directory_upload_chunk", true)
}

func (a *API) CompleteDeviceDirectoryFileUpload(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceDirectoryFile(w, r, "device.directory_files.upload_complete", "req_directory_upload_complete", true)
}

// DeviceDirectoryFileUploadStatus 查询设备目录分块上传进度，供断点续传使用。
func (a *API) DeviceDirectoryFileUploadStatus(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceDirectoryFile(w, r, "device.directory_files.upload_status", "req_directory_upload_status", true)
}

func (a *API) CreateDeviceDirectoryEmptyFile(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceDirectoryFile(w, r, "device.directory_files.create_file", "req_directory_create_file", false)
}

func (a *API) CreateDeviceDirectoryFolder(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceDirectoryFile(w, r, "device.directory_files.mkdir", "req_directory_mkdir", false)
}

func (a *API) DeleteDeviceDirectoryFile(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceDirectoryFile(w, r, "device.directory_files.delete", "req_directory_delete", false)
}

func (a *API) forwardDeviceDirectoryFile(
	w http.ResponseWriter,
	r *http.Request,
	envType string,
	requestPrefix string,
	requireUploadID bool,
) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	var req struct {
		Path        string `json:"path"`
		IsDir       bool   `json:"is_dir"`
		Content     string `json:"content"`
		Encoding    string `json:"encoding"`
		UploadID    string `json:"upload_id"`
		Size        int64  `json:"size"`
		TotalChunks int    `json:"total_chunks"`
		ChunkIndex  int    `json:"chunk_index"`
		Offset      int64  `json:"offset"`
		SHA256      string `json:"sha256"`
		Resume      bool   `json:"resume"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.Path = strings.TrimSpace(req.Path)
	if req.Path == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "文件路径不能为空"})
		return
	}
	if requireUploadID && strings.TrimSpace(req.UploadID) == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "upload_id 不能为空"})
		return
	}
	if strings.TrimSpace(req.Encoding) == "" {
		req.Encoding = "base64"
	}
	// 分块上传按会员等级限制单块大小，普通用户不允许超过 512KB。
	if isChunkStep(envType) {
		if err := a.validateUploadChunkSize(operator, req.Content); err != nil {
			write(w, http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
			return
		}
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	allowAll, err := a.deviceAllowAllDirectories(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	requestID := fmt.Sprintf("%s_%d", requestPrefix, time.Now().UnixNano())
	result, err := a.requestDeviceDirectories(
		r.Context(),
		operator.ID,
		launcherID,
		requestID,
		envType,
		model.DeviceDirectoryFilesPayload{
			MachineID:   machineID,
			Path:        req.Path,
			IsDir:       req.IsDir,
			AllowAll:    allowAll,
			Content:     req.Content,
			Encoding:    req.Encoding,
			UploadID:    req.UploadID,
			Size:        req.Size,
			TotalChunks: req.TotalChunks,
			ChunkIndex:  req.ChunkIndex,
			Offset:      req.Offset,
			SHA256:      req.SHA256,
			Resume:      req.Resume,
		},
	)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if !result.Success {
		message := strings.TrimSpace(result.Error)
		if message == "" {
			message = "设备目录操作失败"
		}
		write(w, http.StatusConflict, map[string]string{"error": message})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) ListDeviceAgentFiles(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent_id 不能为空"})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_project_files_list_%d", time.Now().UnixNano())
	result, err := a.requestDeviceDirectories(r.Context(), operator.ID, launcherID, requestID, "device.project_files.list", model.DeviceProjectFilesPayload{
		MachineID: machineID,
		AgentID:   agentID,
		Path:      strings.TrimSpace(r.URL.Query().Get("path")),
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) UploadDeviceAgentFile(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent_id 不能为空"})
		return
	}
	var req struct {
		Path     string `json:"path"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "文件路径不能为空"})
		return
	}
	if strings.TrimSpace(req.Encoding) == "" {
		req.Encoding = "base64"
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_project_files_upload_%d", time.Now().UnixNano())
	result, err := a.requestDeviceDirectories(r.Context(), operator.ID, launcherID, requestID, "device.project_files.upload", model.DeviceProjectFilesPayload{
		MachineID: machineID,
		AgentID:   agentID,
		Path:      req.Path,
		Content:   req.Content,
		Encoding:  req.Encoding,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) CreateDeviceAgentFileUpload(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceAgentFileTransferStep(w, r, "device.project_files.upload_create", "req_project_files_upload_create", true)
}

func (a *API) UploadDeviceAgentFileChunk(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceAgentFileTransferStep(w, r, "device.project_files.upload_chunk", "req_project_files_upload_chunk", true)
}

func (a *API) CompleteDeviceAgentFileUpload(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceAgentFileTransferStep(w, r, "device.project_files.upload_complete", "req_project_files_upload_complete", true)
}

// DeviceAgentFileUploadStatus 查询项目文件分块上传进度，供断点续传使用。
func (a *API) DeviceAgentFileUploadStatus(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceAgentFileTransferStep(w, r, "device.project_files.upload_status", "req_project_files_upload_status", true)
}

func (a *API) CreateDeviceAgentFileDownload(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceAgentFileTransferStep(w, r, "device.project_files.download_create", "req_project_files_download_create", false)
}

func (a *API) DownloadDeviceAgentFileChunk(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceAgentFileTransferStep(w, r, "device.project_files.download_chunk", "req_project_files_download_chunk", false)
}

func (a *API) CreateDeviceAgentEmptyFile(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceAgentFileMutation(w, r, "device.project_files.create_file", "req_project_files_create_file", false, false)
}

func (a *API) CreateDeviceAgentFolder(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceAgentFileMutation(w, r, "device.project_files.mkdir", "req_project_files_mkdir", false, false)
}

func (a *API) DeleteDeviceAgentFile(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceAgentFileMutation(w, r, "device.project_files.delete", "req_project_files_delete", true, false)
}

func (a *API) RenameDeviceAgentFile(w http.ResponseWriter, r *http.Request) {
	a.forwardDeviceAgentFileMutation(w, r, "device.project_files.rename", "req_project_files_rename", false, true)
}

func (a *API) forwardDeviceAgentFileMutation(w http.ResponseWriter, r *http.Request, envType string, requestPrefix string, includeIsDir bool, requireName bool) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent_id 不能为空"})
		return
	}
	var req struct {
		Path  string `json:"path"`
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "文件路径不能为空"})
		return
	}
	if requireName && strings.TrimSpace(req.Name) == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "名称不能为空"})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("%s_%d", requestPrefix, time.Now().UnixNano())
	payload := model.DeviceProjectFilesPayload{
		MachineID: machineID,
		AgentID:   agentID,
		Path:      req.Path,
		Name:      req.Name,
	}
	if includeIsDir {
		payload.IsDir = req.IsDir
	}
	result, err := a.requestDeviceDirectories(r.Context(), operator.ID, launcherID, requestID, envType, payload)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func isChunkStep(envType string) bool {
	return envType == "device.project_files.upload_chunk" ||
		envType == "device.directory_files.upload_chunk"
}

// validateUploadChunkSize 校验 base64 内容对应的原始分块是否超过该等级的允许上限。
// 校验在解码之前完成，避免为超限请求分配内存。
func (a *API) validateUploadChunkSize(operator model.Operator, content string) error {
	policy := a.uploadPolicyForOperator(operator)
	maxContentLen := model.MaxBase64ContentLen(policy.MaxChunkSize)
	if int64(len(content)) > maxContentLen {
		if policy.IsMember {
			return fmt.Errorf("分块超过会员上限 %d MB", policy.MaxChunkSize/1024/1024)
		}
		return fmt.Errorf("普通用户单个分块不能超过 %d KB，升级会员可使用更大分块", policy.MaxChunkSize/1024)
	}
	return nil
}

func (a *API) forwardDeviceAgentFileTransferStep(w http.ResponseWriter, r *http.Request, envType string, requestPrefix string, requireUploadID bool) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent_id 不能为空"})
		return
	}
	var req struct {
		Path        string `json:"path"`
		Content     string `json:"content"`
		Encoding    string `json:"encoding"`
		UploadID    string `json:"upload_id"`
		Size        int64  `json:"size"`
		TotalChunks int    `json:"total_chunks"`
		ChunkIndex  int    `json:"chunk_index"`
		Offset      int64  `json:"offset"`
		Length      int64  `json:"length"`
		SHA256      string `json:"sha256"`
		Resume      bool   `json:"resume"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "文件路径不能为空"})
		return
	}
	if requireUploadID && strings.TrimSpace(req.UploadID) == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "upload_id 不能为空"})
		return
	}
	if strings.TrimSpace(req.Encoding) == "" {
		req.Encoding = "base64"
	}
	// 分块上传按会员等级限制单块大小，普通用户不允许超过 512KB。
	if isChunkStep(envType) {
		if err := a.validateUploadChunkSize(operator, req.Content); err != nil {
			write(w, http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
			return
		}
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("%s_%d", requestPrefix, time.Now().UnixNano())
	result, err := a.requestDeviceDirectories(r.Context(), operator.ID, launcherID, requestID, envType, model.DeviceProjectFilesPayload{
		MachineID:   machineID,
		AgentID:     agentID,
		Path:        req.Path,
		Content:     req.Content,
		Encoding:    req.Encoding,
		UploadID:    req.UploadID,
		Size:        req.Size,
		TotalChunks: req.TotalChunks,
		ChunkIndex:  req.ChunkIndex,
		Offset:      req.Offset,
		Length:      req.Length,
		SHA256:      req.SHA256,
		Resume:      req.Resume,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) DownloadDeviceAgentFile(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent_id 不能为空"})
		return
	}
	filePath := strings.TrimSpace(r.URL.Query().Get("path"))
	if filePath == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "文件路径不能为空"})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	requestID := fmt.Sprintf("req_project_files_download_%d", time.Now().UnixNano())
	result, err := a.requestDeviceDirectories(r.Context(), operator.ID, launcherID, requestID, "device.project_files.download", model.DeviceProjectFilesPayload{
		MachineID: machineID,
		AgentID:   agentID,
		Path:      filePath,
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if result.File == nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 未返回文件"})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) GetDeviceDirectoryAccess(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	allowAll, err := a.deviceAllowAllDirectories(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"allow_all": allowAll})
}

func (a *API) UpdateDeviceDirectoryAccess(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	var req struct {
		AllowAll  bool   `json:"allow_all"`
		EmailCode string `json:"email_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.AllowAll {
		email, err := a.store.GetOperatorEmail(operator.ID)
		if err != nil {
			write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if strings.TrimSpace(email) == "" {
			write(w, http.StatusBadRequest, map[string]string{"error": "当前账号未绑定邮箱"})
			return
		}
		if !a.mailer.VerifyCode(strings.TrimSpace(email), strings.TrimSpace(req.EmailCode)) {
			write(w, http.StatusBadRequest, map[string]string{"error": "邮箱验证码错误或已过期"})
			return
		}
	}
	if err := a.store.SetDeviceSetting(operator.ID, machineID, deviceAllowAllDirsKey, strconv.FormatBool(req.AllowAll)); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"allow_all": req.AllowAll})
}

func (a *API) SendDeviceDirectoryAccessEmailCode(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		MachineID string `json:"machine_id"`
		CaptchaID string `json:"captcha_id"`
		Captcha   string `json:"captcha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "参数错误"})
		return
	}
	if _, ok := a.broker.GetMachine(operator.ID, strings.TrimSpace(req.MachineID)); !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if req.CaptchaID == "" || req.Captcha == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "图形验证码不能为空"})
		return
	}
	if !captcha.Verify(req.CaptchaID, req.Captcha) {
		write(w, http.StatusBadRequest, map[string]string{"error": "图形验证码错误或已过期"})
		return
	}
	email, err := a.store.GetOperatorEmail(operator.ID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(email) == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "当前账号未绑定邮箱"})
		return
	}
	if err := a.mailer.SendCode(strings.TrimSpace(email)); err != nil {
		write(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]string{"message": "验证码已发送"})
}

func (a *API) deviceAllowAllDirectories(operatorID int64, machineID string) (bool, error) {
	settings, err := a.store.GetDeviceSettings(operatorID, machineID)
	if err != nil {
		return false, err
	}
	if settings == nil {
		return false, nil
	}
	return strings.EqualFold(strings.TrimSpace(settings[deviceAllowAllDirsKey]), "true"), nil
}

func (a *API) CreateDeviceAgent(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	var req createDeviceAgentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	requestID := fmt.Sprintf("req_launcher_agent_create_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.create_agent", req)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) RestartDeviceAgent(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	var req restartDeviceAgentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	requestID := fmt.Sprintf("req_launcher_agent_restart_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.restart_agent", req)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) RenameDeviceAgent(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	var req renameDeviceAgentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.Name = strings.TrimSpace(req.Name)
	if req.AgentID == "" || req.Name == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent_id 和名称不能为空"})
		return
	}
	requestID := fmt.Sprintf("req_launcher_agent_rename_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.rename_agent", req)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) SetDeviceAgentEnabled(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	var req setDeviceAgentEnabledReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.AgentID = strings.TrimSpace(req.AgentID)
	if req.AgentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent_id 不能为空"})
		return
	}
	requestID := fmt.Sprintf("req_launcher_agent_enabled_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.set_agent_enabled", req)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, result)
}

func (a *API) RemoveDeviceAgent(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	var req removeDeviceAgentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	requestID := fmt.Sprintf("req_launcher_agent_remove_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID, "device.launcher.remove_agent", req)
	if err != nil && !strings.EqualFold(strings.TrimSpace(err.Error()), "agent not found") {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if err := a.store.DeleteDeviceSettings(operator.ID, req.AgentID); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := a.store.SetDeviceSetting(operator.ID, req.AgentID, deviceDeletedAgentKey, "true"); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		result = model.DeviceLauncherResultPayload{
			MachineID: machineID,
			Action:    "remove_agent",
			Success:   true,
		}
	}
	result.Success = true
	result.Error = ""
	write(w, http.StatusOK, result)
}

func (a *API) deviceLauncherAgent(operatorID int64, machineID string) (string, error) {
	launcher, ok := a.broker.GetLauncher(machineID, operatorID)
	if !ok {
		return "", errors.New("device not available")
	}
	return launcher.ID, nil
}

func (a *API) deviceLauncherAgentWithCapability(operatorID int64, machineID, capability string) (string, error) {
	launcher, ok := a.broker.GetLauncher(machineID, operatorID)
	if !ok {
		return "", errors.New("device not available")
	}
	for _, item := range launcher.Capabilities {
		if strings.TrimSpace(item) == capability {
			return launcher.ID, nil
		}
	}
	return "", fmt.Errorf("设备 launcher 不支持 %s", capability)
}

func enrichDeviceAIConfigResult(result *model.DeviceAIConfigResultPayload) {
	if result == nil || result.Config == nil {
		return
	}
	enrichDeviceAIConfigPreview(result.Config)
}

func enrichDeviceAIConfigPreview(cfg *model.DeviceAIConfigPreview) {
	if cfg == nil {
		return
	}
	cfg.Models = normalizeDeviceAIModels(cfg.Provider, cfg.Models)
	for i := range cfg.Providers {
		providerID := strings.TrimSpace(cfg.Providers[i].ID)
		if providerID == "" {
			providerID = strings.TrimSpace(cfg.Provider)
		}
		cfg.Providers[i].Models = normalizeDeviceAIModels(providerID, cfg.Providers[i].Models)
	}
}

func normalizeDeviceAIModels(provider string, items []model.DeviceAIModel) []model.DeviceAIModel {
	if len(items) == 0 {
		return nil
	}
	out := make([]model.DeviceAIModel, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		item.ID = id
		item.Name = strings.TrimSpace(item.Name)
		if item.Name == "" {
			item.Name = id
		}
		item.Owned = strings.TrimSpace(item.Owned)
		if item.Owned == "" {
			item.Owned = strings.TrimSpace(provider)
		}
		// 供应商返回 > 手填 > 预设推断。后端只做兜底，优先值以 launcher 回传为准。
		item.Context = firstPositiveLimit(item.Context, item.ManualContext, inferGrokContextLimit(item.Owned, item.ID, item.Name))
		item.Output = firstPositiveLimit(item.Output, item.ManualOutput)
		if item.Thinking != nil {
			item.Thinking.Source = strings.TrimSpace(item.Thinking.Source)
			item.Thinking.Control = strings.TrimSpace(item.Thinking.Control)
			item.Thinking.Protocol = strings.TrimSpace(item.Thinking.Protocol)
			item.Thinking.SupportedParameters = compactStrings(item.Thinking.SupportedParameters)
		}
		out = append(out, item)
	}
	return out
}

func normalizeDeviceAIAPIMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "chat":
		return "chat"
	default:
		return "responses"
	}
}

func effectiveDeviceAIAPIMode(value string, modelIDs ...string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "responses":
		return "responses"
	case "chat":
		return "chat"
	}
	for _, id := range modelIDs {
		if isDeviceAIGPTModel(id) {
			return "responses"
		}
	}
	if len(modelIDs) == 0 {
		return "responses"
	}
	return "chat"
}

func isDeviceAIGPTModel(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	if strings.Contains(id, "/") {
		id = id[strings.LastIndex(id, "/")+1:]
	}
	return strings.HasPrefix(id, "gpt") || strings.HasPrefix(id, "chatgpt")
}

var (
	grok46Pattern      = regexp.MustCompile(`^grok[-_.]?4[-_.]?[56](?:$|[-_.])`)
	grok43Or420Pattern = regexp.MustCompile(`^grok[-_.]?4[-_.]?(?:3|20)(?:$|[-_.])`)
)

func inferGrokContextLimit(provider, modelID, modelName string) int64 {
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
	alias := strings.ToLower(strings.TrimSpace(value))
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
	if idx := strings.LastIndex(value, "/"); idx >= 0 && idx+1 < len(value) {
		return value[idx+1:]
	}
	return ""
}

func deviceAIModelIDs(models []model.DeviceAIModel) []string {
	out := make([]string, 0, len(models))
	for _, item := range models {
		if strings.TrimSpace(item.ID) != "" {
			out = append(out, item.ID)
		}
	}
	return out
}

func launcherModels(cfg *model.DeviceAIConfigPreview) map[string]any {
	connected := []string{}
	all := []map[string]any{}
	defaultModel := map[string]any{}
	if cfg == nil {
		return map[string]any{"connected": connected, "all": all, "default": defaultModel}
	}
	providers := cfg.Providers
	if len(providers) == 0 && strings.TrimSpace(cfg.Provider) != "" {
		providers = []model.DeviceAIProvider{{
			ID:           cfg.Provider,
			BaseURL:      cfg.BaseURL,
			ConsoleURL:   cfg.ConsoleURL,
			APIKeyMasked: cfg.APIKeyMasked,
			APIMode:      cfg.APIMode,
			Models:       cfg.Models,
		}}
	}
	for _, provider := range providers {
		providerID := strings.TrimSpace(provider.ID)
		if providerID == "" {
			continue
		}
		connected = append(connected, providerID)
		models := map[string]any{}
		for _, item := range provider.Models {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				continue
			}
			name := strings.TrimSpace(item.Name)
			if name == "" {
				name = id
			}
			entry := map[string]any{
				"name":        name,
				"status":      "active",
				"tool_call":   true,
				"temperature": true,
			}
			contextLimit := item.Context
			if contextLimit <= 0 {
				owned := strings.TrimSpace(item.Owned)
				if owned == "" {
					owned = providerID
				}
				contextLimit = inferGrokContextLimit(owned, id, name)
			}
			if contextLimit > 0 || item.Output > 0 {
				limit := map[string]any{}
				if contextLimit > 0 {
					limit["context"] = contextLimit
				}
				if item.Output > 0 {
					limit["output"] = item.Output
				}
				entry["limit"] = limit
				if contextLimit > 0 {
					entry["context_limit"] = contextLimit
				}
				if item.Output > 0 {
					entry["output_limit"] = item.Output
				}
			}
			if modalities := normalizeModalities(item.Modalities); modalities != nil {
				entry["modalities"] = map[string]any{
					"input":  modalities.Input,
					"output": modalities.Output,
				}
				entry["image"] = containsString(modalities.Output, "image")
			}
			if len(item.Variants) > 0 {
				entry["variants"] = item.Variants
			}
			if item.Thinking != nil {
				entry["thinking"] = item.Thinking
			}
			models[id] = entry
		}
		all = append(all, map[string]any{
			"id":          providerID,
			"name":        providerID,
			"base_url":    strings.TrimSpace(provider.BaseURL),
			"console_url": deriveProviderConsoleURL(provider.ConsoleURL, provider.BaseURL),
			"api_mode":    effectiveDeviceAIAPIMode(provider.APIMode, deviceAIModelIDs(provider.Models)...),
			"models":      models,
		})
	}
	if modelID := strings.TrimSpace(cfg.Model); modelID != "" {
		defaultModel = map[string]any{"model": modelID}
	}
	return map[string]any{"connected": connected, "all": all, "default": defaultModel}
}

// deriveProviderConsoleURL keeps an explicitly configured dashboard URL and
// otherwise exposes the API origin as the provider entry point. The API path
// is intentionally removed because /v1 and similar paths are not consoles.
func deriveProviderConsoleURL(consoleURL, baseURL string) string {
	if value := strings.TrimSpace(consoleURL); value != "" {
		return value
	}
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
}

func normalizeModalities(value *model.DeviceAIModalities) *model.DeviceAIModalities {
	if value == nil {
		return nil
	}
	input := compactStrings(value.Input)
	output := compactStrings(value.Output)
	if len(input) == 0 && len(output) == 0 {
		return nil
	}
	return &model.DeviceAIModalities{Input: input, Output: output}
}

func firstPositiveLimit(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func compactStrings(items []string) []string {
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

func containsString(items []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), target) {
			return true
		}
	}
	return false
}

func (a *API) updateLauncherModelCache(operatorID int64, machineID string, result model.DeviceAIConfigResultPayload) {
	if result.Success && result.Config != nil {
		a.broker.SetModelCache(operatorID, machineID, launcherModels(result.Config))
		return
	}
	a.broker.ClearModelCache(operatorID, machineID)
}

func (a *API) fetchLauncherModels(ctx context.Context, operatorID int64, machineID string, timeout time.Duration) (map[string]any, error) {
	launcherID, err := a.deviceLauncherAgent(operatorID, machineID)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	requestID := fmt.Sprintf("req_ai_get_%d", time.Now().UnixNano())
	result, err := a.requestDeviceAIConfig(reqCtx, operatorID, launcherID, requestID, "device.ai_config.get", model.DeviceAIConfigGetPayload{
		AgentID: machineID,
	})
	if err != nil {
		return nil, err
	}
	if !result.Success || result.Config == nil {
		if result.Error != "" {
			return nil, errors.New(result.Error)
		}
		return nil, errors.New("launcher 未返回模型配置")
	}
	enrichDeviceAIConfigResult(&result)
	payload := launcherModels(result.Config)
	a.broker.SetModelCache(operatorID, machineID, payload)
	return payload, nil
}

func (a *API) refreshLauncherModels(operatorID int64, machineID string) {
	ctx, cancel := context.WithTimeout(context.Background(), launcherModelsRefreshTimeout)
	defer cancel()
	_, _ = a.fetchLauncherModels(ctx, operatorID, machineID, launcherModelsRefreshTimeout)
}

func (a *API) UpdateDeviceSettings(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	agentID := chi.URLParam(r, "agentID")
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	for k, v := range body {
		if err := a.store.SetDeviceSetting(operator.ID, agentID, k, v); err != nil {
			write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	write(w, http.StatusOK, map[string]string{"message": "ok"})
}

func (a *API) ListModels(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if machineID := strings.TrimSpace(r.URL.Query().Get("machine_id")); machineID != "" {
		if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
			write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
			return
		}
		payload, err := a.fetchLauncherModels(r.Context(), operator.ID, machineID, launcherModelsInitialTimeout)
		if err != nil {
			if cache, ok := a.broker.GetModelCache(operator.ID, machineID); ok && cache.Payload != nil {
				write(w, http.StatusOK, cache.Payload)
				return
			}
			write(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		write(w, http.StatusOK, payload)
		return
	}
	machines := a.broker.ListMachines(operator.ID)
	for _, m := range machines {
		if cache, ok := a.broker.GetModelCache(operator.ID, m.MachineID); ok && cache.Payload != nil {
			go a.refreshLauncherModels(operator.ID, m.MachineID)
			write(w, http.StatusOK, cache.Payload)
			return
		}
		payload, err := a.fetchLauncherModels(r.Context(), operator.ID, m.MachineID, launcherModelsInitialTimeout)
		if err != nil {
			continue
		}
		write(w, http.StatusOK, payload)
		return
	}
	write(w, http.StatusServiceUnavailable, map[string]string{"error": "no launcher with models available"})
}

// APK/CLI/Launcher 产物路径
var (
	apkDir              = "/opt/chat-codex/apk"
	cliDir              = "/opt/chat-codex/apk/cli"
	cliVersionFile      = "/opt/chat-codex/apk/cli/version.json"
	launcherDir         = "/opt/chat-codex/apk/launcher"
	launcherVersionFile = "/opt/chat-codex/apk/launcher/version.json"
	runtimeDir          = "/opt/chat-codex/apk/runtime"
)

type downloadAsset struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Platform  string `json:"platform"`
	Kind      string `json:"kind"`
	Filename  string `json:"filename"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256,omitempty"`
	Available bool   `json:"available"`
}

type cliVersionAsset struct {
	Platform  string `json:"platform"`
	Filename  string `json:"filename"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256,omitempty"`
	Available bool   `json:"available"`
}

func appDownloadAssets(r *http.Request) []downloadAsset {
	versionCode := strings.TrimSpace(r.URL.Query().Get("v"))
	versionSuffix := ""
	if versionCode != "" {
		versionSuffix = "?v=" + versionCode
	}
	return []downloadAsset{
		{
			ID:       "android-apk",
			Title:    "Android APK",
			Platform: "android",
			Kind:     "mobile",
			Filename: "chat-codex.apk",
			URL:      "/api/app/download" + versionSuffix,
		},
		{
			ID:       "windows-x64",
			Title:    "Windows x64 安装程序",
			Platform: "windows-x64",
			Kind:     "desktop",
			Filename: "chat-codex-windows-x64-setup.exe",
			URL:      "/api/app/download?artifact=chat-codex-windows-x64-setup.exe",
		},
		{
			ID:       "darwin-arm64",
			Title:    "macOS Apple Silicon 安装镜像",
			Platform: "darwin-arm64",
			Kind:     "desktop",
			Filename: "chat-codex-darwin-arm64.dmg",
			URL:      "/api/app/download?artifact=chat-codex-darwin-arm64.dmg",
		},
	}
}

func launcherDownloadAssets(r *http.Request) []downloadAsset {
	return []downloadAsset{
		{
			ID:       "chat-codex-launcher-windows-x64",
			Title:    "Windows x64 GUI Launcher",
			Platform: "windows-x64",
			Kind:     "launcher",
			Filename: "chat-codex-launcher-windows-x64.zip",
			URL:      "/api/launcher/download?artifact=chat-codex-launcher-windows-x64.zip",
		},
		{
			ID:       "chat-codex-launcher-darwin-arm64",
			Title:    "macOS Apple Silicon GUI Launcher",
			Platform: "darwin-arm64",
			Kind:     "launcher",
			Filename: "chat-codex-launcher-darwin-arm64.zip",
			URL:      "/api/launcher/download?artifact=chat-codex-launcher-darwin-arm64.zip",
		},
	}
}

func resolveAppDownloadFile(artifact string) (filePath string, contentType string, downloadName string, ok bool) {
	switch artifact {
	case "", "chat-codex.apk":
		return filepath.Join(apkDir, "app-release.apk"), "application/vnd.android.package-archive", "chat-codex.apk", true
	case "chat-codex-windows-x64-setup.exe":
		return filepath.Join(apkDir, artifact), "application/vnd.microsoft.portable-executable", artifact, true
	case "chat-codex-darwin-arm64.dmg":
		return filepath.Join(apkDir, artifact), "application/x-apple-diskimage", artifact, true
	default:
		if strings.HasPrefix(artifact, "chat-codex-") && strings.HasSuffix(artifact, ".apk") {
			return filepath.Join(apkDir, "app-release.apk"), "application/vnd.android.package-archive", artifact, true
		}
		return resolveCLIDownloadFile(artifact)
	}
}

func currentAppAPKDownloadName() string {
	data, err := os.ReadFile(filepath.Join(apkDir, "version.json"))
	if err != nil {
		return "chat-codex.apk"
	}
	var metadata struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &metadata) != nil {
		return "chat-codex.apk"
	}
	version := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, strings.TrimSpace(metadata.Version))
	if version == "" {
		return "chat-codex.apk"
	}
	return "chat-codex-" + version + ".apk"
}

func appVersionAssets(r *http.Request) []cliVersionAsset {
	downloads := appDownloadAssets(r)
	assets := make([]cliVersionAsset, 0, len(downloads))
	for _, item := range downloads {
		filePath, _, _, ok := resolveAppDownloadFile(item.Filename)
		available := false
		if ok {
			_, err := os.Stat(filePath)
			available = err == nil
		}
		assets = append(assets, cliVersionAsset{
			Platform:  item.Platform,
			Filename:  item.Filename,
			URL:       absoluteURL(r, item.URL),
			Available: available,
		})
	}
	return assets
}

func appVersionAssetsFromBody(r *http.Request, raw any) []cliVersionAsset {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return appVersionAssets(r)
	}
	assets := make([]cliVersionAsset, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		asset := cliVersionAsset{
			Platform: strings.TrimSpace(fmt.Sprint(obj["platform"])),
			Filename: strings.TrimSpace(fmt.Sprint(obj["filename"])),
			URL:      absoluteURL(r, fmt.Sprint(obj["url"])),
			SHA256:   strings.TrimSpace(fmt.Sprint(obj["sha256"])),
		}
		filePath, _, _, found := resolveAppDownloadFile(asset.Filename)
		if found {
			_, err := os.Stat(filePath)
			asset.Available = err == nil
		}
		if asset.Platform == "" || asset.Filename == "" || asset.URL == "" || !found {
			continue
		}
		assets = append(assets, asset)
	}
	if len(assets) == 0 {
		return appVersionAssets(r)
	}
	return assets
}

func resolveCLIDownloadFile(artifact string) (filePath string, contentType string, downloadName string, ok bool) {
	switch artifact {
	case "opencode-windows-x64.zip":
		return filepath.Join(cliDir, artifact), "application/zip", artifact, true
	case "opencode-darwin-arm64.zip":
		return filepath.Join(cliDir, artifact), "application/zip", artifact, true
	case "opencode-linux-x64.tar.gz":
		return filepath.Join(cliDir, artifact), "application/gzip", artifact, true
	default:
		return "", "", "", false
	}
}

func resolveLauncherDownloadFile(artifact string) (filePath string, contentType string, downloadName string, ok bool) {
	switch artifact {
	case "launcher-windows-x64.zip":
		return filepath.Join(launcherDir, artifact), "application/zip", artifact, true
	case "launcher-darwin-arm64.zip":
		return filepath.Join(launcherDir, artifact), "application/zip", artifact, true
	case "chat-codex-launcher-windows-x64.zip":
		return filepath.Join(launcherDir, artifact), "application/zip", artifact, true
	case "chat-codex-launcher-darwin-arm64.zip":
		return filepath.Join(launcherDir, artifact), "application/zip", artifact, true
	case "chat-codex-tunnel-darwin-arm64", "chat-codex-tunnel-darwin-x64",
		"chat-codex-tunnel-linux-x64", "chat-codex-tunnel-linux-arm64":
		return filepath.Join(launcherDir, artifact), "application/octet-stream", artifact, true
	case "chat-codex-tunnel-windows-x64.exe", "chat-codex-tunnel-windows-arm64.exe":
		return filepath.Join(launcherDir, artifact), "application/vnd.microsoft.portable-executable", artifact, true
	default:
		return "", "", "", false
	}
}

func resolveRuntimeUVDownloadFile(artifact string) (filePath string, contentType string, downloadName string, ok bool) {
	switch artifact {
	case "uv-x86_64-pc-windows-msvc.zip", "uv-aarch64-pc-windows-msvc.zip":
		return filepath.Join(runtimeDir, "uv", artifact), "application/zip", artifact, true
	case "uv-aarch64-apple-darwin.tar.gz", "uv-x86_64-apple-darwin.tar.gz", "uv-x86_64-unknown-linux-gnu.tar.gz", "uv-aarch64-unknown-linux-gnu.tar.gz":
		return filepath.Join(runtimeDir, "uv", artifact), "application/gzip", artifact, true
	default:
		return "", "", "", false
	}
}

func resolveRuntimeNodeDownloadFile(artifact string) (filePath string, contentType string, downloadName string, ok bool) {
	switch artifact {
	case "node-v22.16.0-win-x64.zip", "node-v22.16.0-win-arm64.zip":
		return filepath.Join(runtimeDir, "node", artifact), "application/zip", artifact, true
	case "node-v22.16.0-darwin-arm64.tar.gz", "node-v22.16.0-darwin-x64.tar.gz", "node-v22.16.0-linux-x64.tar.gz", "node-v22.16.0-linux-arm64.tar.gz":
		return filepath.Join(runtimeDir, "node", artifact), "application/gzip", artifact, true
	default:
		return "", "", "", false
	}
}

func resolveRuntimeJavaDownloadFile(artifact string) (filePath string, contentType string, downloadName string, ok bool) {
	switch artifact {
	case "jdk-21-windows-x64.zip", "jdk-21-windows-aarch64.zip",
		"OpenJDK21U-jdk_x64_windows_hotspot_21.0.11_10.zip",
		"OpenJDK21U-jdk_aarch64_windows_hotspot_21.0.11_10.zip":
		return filepath.Join(runtimeDir, "java", artifact), "application/zip", artifact, true
	case "jdk-21-darwin-aarch64.tar.gz", "jdk-21-darwin-x64.tar.gz", "jdk-21-linux-x64.tar.gz", "jdk-21-linux-aarch64.tar.gz",
		"OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.11_10.tar.gz",
		"OpenJDK21U-jdk_x64_mac_hotspot_21.0.11_10.tar.gz",
		"OpenJDK21U-jdk_x64_linux_hotspot_21.0.11_10.tar.gz",
		"OpenJDK21U-jdk_aarch64_linux_hotspot_21.0.11_10.tar.gz":
		return filepath.Join(runtimeDir, "java", artifact), "application/gzip", artifact, true
	default:
		return "", "", "", false
	}
}

func resolveRuntimeGoDownloadFile(artifact string) (filePath string, contentType string, downloadName string, ok bool) {
	switch artifact {
	case "go1.25.4.windows-amd64.zip":
		return filepath.Join(runtimeDir, "go", artifact), "application/zip", artifact, true
	case "go1.25.4.darwin-arm64.tar.gz", "go1.25.4.darwin-amd64.tar.gz", "go1.25.4.linux-amd64.tar.gz":
		return filepath.Join(runtimeDir, "go", artifact), "application/gzip", artifact, true
	default:
		return "", "", "", false
	}
}

func resolveRuntimeGhidraDownloadFile(artifact string) (filePath string, contentType string, downloadName string, ok bool) {
	switch artifact {
	case "ghidra_12.1.2_PUBLIC_20260605.zip":
		return filepath.Join(runtimeDir, "ghidra", artifact), "application/zip", artifact, true
	default:
		return "", "", "", false
	}
}

func resolveRuntimeIDADownloadFile(artifact string) (filePath string, contentType string, downloadName string, ok bool) {
	switch artifact {
	case "bgspa-ida92-win.zip", "bgspa-ida92-x64mac.zip", "bgspa-ida92-armmac.zip":
		return filepath.Join(runtimeDir, "ida", artifact), "application/zip", artifact, true
	default:
		return "", "", "", false
	}
}

func resolveRuntimeToolDownloadFile(artifact string) (filePath string, contentType string, downloadName string, ok bool) {
	switch artifact {
	case "jadx-1.5.5.zip":
		return filepath.Join(runtimeDir, "tool", artifact), "application/zip", artifact, true
	case "apktool_3.0.2.jar":
		return filepath.Join(runtimeDir, "tool", artifact), "application/java-archive", artifact, true
	default:
		return "", "", "", false
	}
}

func resolveRuntimeMCPDownloadFile(artifact string) (filePath string, contentType string, downloadName string, ok bool) {
	artifact = strings.TrimSpace(artifact)
	if artifact == "" || filepath.Base(artifact) != artifact {
		return "", "", "", false
	}
	contentType = ""
	switch {
	case strings.HasSuffix(artifact, ".tar.gz"):
		contentType = "application/gzip"
	case strings.HasSuffix(artifact, ".whl"):
		contentType = "application/zip"
	default:
		return "", "", "", false
	}
	return filepath.Join(runtimeDir, "mcp", artifact), contentType, artifact, true
}

func requestBaseURL(r *http.Request) string {
	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	return proto + "://" + host
}

func absoluteURL(r *http.Request, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	base := requestBaseURL(r)
	if base == "" {
		return raw
	}
	if strings.HasPrefix(raw, "/") {
		return base + raw
	}
	return base + "/" + raw
}

func cliAssets(r *http.Request) []cliVersionAsset {
	assets := []cliVersionAsset{
		{
			Platform: "windows-x64",
			Filename: "opencode-windows-x64.zip",
			URL:      "/api/app/download?artifact=opencode-windows-x64.zip",
		},
		{
			Platform: "darwin-arm64",
			Filename: "opencode-darwin-arm64.zip",
			URL:      "/api/app/download?artifact=opencode-darwin-arm64.zip",
		},
		{
			Platform: "linux-x64",
			Filename: "opencode-linux-x64.tar.gz",
			URL:      "/api/app/download?artifact=opencode-linux-x64.tar.gz",
		},
	}
	for i := range assets {
		filePath, _, _, ok := resolveCLIDownloadFile(assets[i].Filename)
		if ok {
			if _, err := os.Stat(filePath); err == nil {
				assets[i].Available = true
			}
		}
		assets[i].URL = absoluteURL(r, assets[i].URL)
	}
	return assets
}

func cliAssetsFromBody(r *http.Request, raw any) []cliVersionAsset {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return cliAssets(r)
	}
	assets := make([]cliVersionAsset, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		asset := cliVersionAsset{
			Platform: strings.TrimSpace(fmt.Sprint(obj["platform"])),
			Filename: strings.TrimSpace(fmt.Sprint(obj["filename"])),
			URL:      absoluteURL(r, fmt.Sprint(obj["url"])),
			SHA256:   strings.TrimSpace(fmt.Sprint(obj["sha256"])),
		}
		if value, ok := obj["available"].(bool); ok {
			asset.Available = value
		} else {
			filePath, _, _, found := resolveCLIDownloadFile(asset.Filename)
			if found {
				if _, err := os.Stat(filePath); err == nil {
					asset.Available = true
				}
			}
		}
		if asset.Platform == "" || asset.Filename == "" || asset.URL == "" {
			continue
		}
		assets = append(assets, asset)
	}
	if len(assets) == 0 {
		return cliAssets(r)
	}
	return assets
}

func launcherAssetsFromBody(r *http.Request, raw any) []cliVersionAsset {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		downloads := launcherDownloadAssets(r)
		assets := make([]cliVersionAsset, 0, len(downloads))
		for _, item := range downloads {
			assets = append(assets, cliVersionAsset{
				Platform:  item.Platform,
				Filename:  item.Filename,
				URL:       item.URL,
				Available: item.Available,
			})
		}
		return assets
	}
	assets := make([]cliVersionAsset, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		asset := cliVersionAsset{
			Platform: strings.TrimSpace(fmt.Sprint(obj["platform"])),
			Filename: strings.TrimSpace(fmt.Sprint(obj["filename"])),
			URL:      absoluteURL(r, fmt.Sprint(obj["url"])),
			SHA256:   strings.TrimSpace(fmt.Sprint(obj["sha256"])),
		}
		if asset.Filename == "launcher-linux-x64.tar.gz" {
			continue
		}
		if value, ok := obj["available"].(bool); ok {
			asset.Available = value
		} else {
			filePath, _, _, found := resolveLauncherDownloadFile(asset.Filename)
			if found {
				if _, err := os.Stat(filePath); err == nil {
					asset.Available = true
				}
			}
		}
		if asset.Platform == "" || asset.Filename == "" || asset.URL == "" {
			continue
		}
		assets = append(assets, asset)
	}
	if len(assets) == 0 {
		return launcherAssetsFromBody(r, nil)
	}
	return assets
}

func (a *API) ChangePassword(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		Email       string `json:"email"`
		EmailCode   string `json:"email_code"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "参数错误"})
		return
	}
	if len(req.NewPassword) < 6 {
		write(w, http.StatusBadRequest, map[string]string{"error": "新密码至少 6 位"})
		return
	}
	if !a.mailer.VerifyCode(req.Email, req.EmailCode) {
		write(w, http.StatusBadRequest, map[string]string{"error": "邮箱验证码错误或已过期"})
		return
	}
	if err := a.store.ChangePassword(operator.ID, req.NewPassword); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "修改失败"})
		return
	}
	write(w, http.StatusOK, map[string]string{"message": "密码已修改"})
}

func (a *API) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "参数错误"})
		return
	}
	// 需要密码确认
	op, err := a.store.AuthenticateOperator(operator.Username, req.Password)
	if err != nil || op == nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "密码错误"})
		return
	}
	if err := a.store.DeleteOperator(operator.ID); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "注销失败"})
		return
	}
	write(w, http.StatusOK, map[string]string{"message": "账号已注销"})
}

func (a *API) AppVersion(w http.ResponseWriter, r *http.Request) {
	versionFile := filepath.Join(apkDir, "version.json")
	body := map[string]any{
		"version":      "1.0.0",
		"version_code": 1,
		"download_url": "",
		"changelog":    "",
		"releases":     []any{},
	}
	if data, err := os.ReadFile(versionFile); err == nil {
		_ = json.Unmarshal(data, &body)
	}
	body["downloads"] = appVersionAssetsFromBody(r, body["downloads"])
	if raw := strings.TrimSpace(fmt.Sprint(body["download_url"])); raw != "" {
		body["download_url"] = absoluteURL(r, raw)
	}
	write(w, http.StatusOK, body)
}

func (a *API) AppReleases(w http.ResponseWriter, r *http.Request) {
	versionFile := filepath.Join(apkDir, "version.json")
	var body struct {
		Releases []any `json:"releases"`
	}
	if data, err := os.ReadFile(versionFile); err == nil {
		_ = json.Unmarshal(data, &body)
	}
	if body.Releases == nil {
		body.Releases = []any{}
	}
	write(w, http.StatusOK, map[string]any{"releases": body.Releases})
}

func (a *API) CliVersion(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{
		"version":   "local",
		"channel":   "latest",
		"changelog": "",
	}
	if data, err := os.ReadFile(cliVersionFile); err == nil {
		_ = json.Unmarshal(data, &body)
	}
	body["downloads"] = cliAssetsFromBody(r, body["downloads"])
	write(w, http.StatusOK, body)
}

func (a *API) AppDownloads(w http.ResponseWriter, r *http.Request) {
	versionFile := filepath.Join(apkDir, "version.json")
	body := map[string]any{
		"version":      "1.0.0",
		"version_code": 1,
		"download_url": "",
		"changelog":    "",
	}
	if data, err := os.ReadFile(versionFile); err == nil {
		_ = json.Unmarshal(data, &body)
	}

	versionAssets := appVersionAssetsFromBody(r, body["downloads"])
	assets := appDownloadAssets(r)
	assetByPlatform := make(map[string]cliVersionAsset, len(versionAssets))
	for _, item := range versionAssets {
		assetByPlatform[item.Platform] = item
	}
	for i := range assets {
		if versionAsset, ok := assetByPlatform[assets[i].Platform]; ok {
			assets[i].URL = versionAsset.URL
			assets[i].SHA256 = versionAsset.SHA256
			assets[i].Available = versionAsset.Available
		} else {
			assets[i].URL = absoluteURL(r, assets[i].URL)
		}
	}
	body["downloads"] = assets
	write(w, http.StatusOK, body)
}

func (a *API) AppDownload(w http.ResponseWriter, r *http.Request) {
	artifact := strings.TrimSpace(r.URL.Query().Get("artifact"))
	filePath, contentType, downloadName, ok := resolveAppDownloadFile(artifact)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		write(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	if artifact == "" || artifact == "chat-codex.apk" {
		downloadName = currentAppAPKDownloadName()
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", downloadName))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	http.ServeFile(w, r, filePath)
}

func (a *API) LauncherVersion(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{
		"version":   "local",
		"channel":   "latest",
		"changelog": "",
	}
	if data, err := os.ReadFile(launcherVersionFile); err == nil {
		_ = json.Unmarshal(data, &body)
	}
	body["downloads"] = launcherAssetsFromBody(r, body["downloads"])
	write(w, http.StatusOK, body)
}

func (a *API) LauncherDownloads(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{
		"version":   "local",
		"channel":   "latest",
		"changelog": "",
	}
	if data, err := os.ReadFile(launcherVersionFile); err == nil {
		_ = json.Unmarshal(data, &body)
	}
	assets := launcherDownloadAssets(r)
	for i := range assets {
		filePath, _, _, ok := resolveLauncherDownloadFile(assets[i].Filename)
		if !ok {
			continue
		}
		if _, err := os.Stat(filePath); err == nil {
			assets[i].Available = true
		}
	}
	body["downloads"] = assets
	write(w, http.StatusOK, body)
}

func (a *API) LauncherDownload(w http.ResponseWriter, r *http.Request) {
	artifact := strings.TrimSpace(r.URL.Query().Get("artifact"))
	filePath, contentType, downloadName, ok := resolveLauncherDownloadFile(artifact)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		write(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", downloadName))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	http.ServeFile(w, r, filePath)
}

func (a *API) RuntimeUVDownload(w http.ResponseWriter, r *http.Request) {
	artifact := strings.TrimSpace(r.URL.Query().Get("artifact"))
	filePath, contentType, downloadName, ok := resolveRuntimeUVDownloadFile(artifact)
	serveRuntimeDownload(w, r, filePath, contentType, downloadName, ok)
}

func (a *API) RuntimeNodeDownload(w http.ResponseWriter, r *http.Request) {
	artifact := strings.TrimSpace(r.URL.Query().Get("artifact"))
	filePath, contentType, downloadName, ok := resolveRuntimeNodeDownloadFile(artifact)
	serveRuntimeDownload(w, r, filePath, contentType, downloadName, ok)
}

func (a *API) RuntimeJavaDownload(w http.ResponseWriter, r *http.Request) {
	artifact := strings.TrimSpace(r.URL.Query().Get("artifact"))
	filePath, contentType, downloadName, ok := resolveRuntimeJavaDownloadFile(artifact)
	serveRuntimeDownload(w, r, filePath, contentType, downloadName, ok)
}

func (a *API) RuntimeGoDownload(w http.ResponseWriter, r *http.Request) {
	artifact := strings.TrimSpace(r.URL.Query().Get("artifact"))
	filePath, contentType, downloadName, ok := resolveRuntimeGoDownloadFile(artifact)
	serveRuntimeDownload(w, r, filePath, contentType, downloadName, ok)
}

func (a *API) RuntimeGhidraDownload(w http.ResponseWriter, r *http.Request) {
	artifact := strings.TrimSpace(r.URL.Query().Get("artifact"))
	filePath, contentType, downloadName, ok := resolveRuntimeGhidraDownloadFile(artifact)
	serveRuntimeDownload(w, r, filePath, contentType, downloadName, ok)
}

func (a *API) RuntimeIDADownload(w http.ResponseWriter, r *http.Request) {
	artifact := strings.TrimSpace(r.URL.Query().Get("artifact"))
	filePath, contentType, downloadName, ok := resolveRuntimeIDADownloadFile(artifact)
	serveRuntimeDownload(w, r, filePath, contentType, downloadName, ok)
}

func (a *API) RuntimeToolDownload(w http.ResponseWriter, r *http.Request) {
	artifact := strings.TrimSpace(r.URL.Query().Get("artifact"))
	filePath, contentType, downloadName, ok := resolveRuntimeToolDownloadFile(artifact)
	serveRuntimeDownload(w, r, filePath, contentType, downloadName, ok)
}

func (a *API) RuntimeMCPDownload(w http.ResponseWriter, r *http.Request) {
	artifact := strings.TrimSpace(chi.URLParam(r, "artifact"))
	if artifact == "" {
		artifact = strings.TrimSpace(r.URL.Query().Get("artifact"))
	}
	filePath, contentType, downloadName, ok := resolveRuntimeMCPDownloadFile(artifact)
	serveRuntimeDownload(w, r, filePath, contentType, downloadName, ok)
}

func serveRuntimeDownload(w http.ResponseWriter, r *http.Request, filePath string, contentType string, downloadName string, ok bool) {
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		write(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", downloadName))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, filePath)
}

func write(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func (a *API) currentOperator(r *http.Request) (model.Operator, bool) {
	token := auth.ExtractBearer(r.Header.Get("Authorization"))
	if token == "" {
		return model.Operator{}, false
	}
	return a.auth.Verify(token)
}

func (a *API) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}
	if !isAdminOperator(operator) {
		write(w, http.StatusForbidden, map[string]string{"error": "admin only"})
		return false
	}
	return true
}

func isAdminOperator(operator model.Operator) bool {
	return strings.EqualFold(strings.TrimSpace(operator.Username), "admin")
}

func statusForStoreError(err error) int {
	if errors.Is(err, sql.ErrNoRows) || strings.Contains(strings.ToLower(err.Error()), "not found") {
		return http.StatusNotFound
	}
	return http.StatusConflict
}

func splitModelRef(raw string) (string, string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ""
	}
	idx := strings.Index(value, "/")
	if idx <= 0 || idx >= len(value)-1 {
		return "", ""
	}
	return strings.TrimSpace(value[:idx]), strings.TrimSpace(value[idx+1:])
}

func normalize(req createTaskReq) ([]model.Part, error) {
	if len(req.Parts) == 0 {
		if strings.EqualFold(strings.TrimSpace(req.Metadata["task_command"]), "compact") {
			return nil, nil
		}
		return nil, errors.New("missing parts")
	}

	out := make([]model.Part, 0, len(req.Parts))

	for _, part := range req.Parts {
		switch part.Type {
		case "text":
			if part.Text == "" {
				return nil, errors.New("text part cannot be empty")
			}
			out = append(out, part)
		case "file":
			if part.URL == "" {
				return nil, errors.New("file part missing url")
			}
			out = append(out, part)
		case "agent":
			if part.Name == "" {
				return nil, errors.New("agent part missing name")
			}
			out = append(out, part)
		case "subtask":
			if part.Prompt == "" || part.Description == "" || part.Agent == "" {
				return nil, errors.New("subtask part missing required fields")
			}
			out = append(out, part)
		default:
			return nil, fmt.Errorf("unsupported part type: %s", part.Type)
		}
	}
	return out, nil
}

func normalizeGoalTaskMetadata(input map[string]string) map[string]string {
	metadata := map[string]string{}
	for key, value := range input {
		metadata[key] = value
	}
	objective := strings.TrimSpace(metadata["goal"])
	if objective == "" {
		objective = strings.TrimSpace(metadata["goal_objective"])
	}
	if objective == "" {
		if len(metadata) == 0 {
			return nil
		}
		return metadata
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	metadata["goal_objective"] = objective
	if strings.TrimSpace(metadata["goal_id"]) == "" {
		metadata["goal_id"] = fmt.Sprintf("goal_%d", time.Now().UnixNano())
	}
	if strings.TrimSpace(metadata["goal_max_iterations"]) == "" {
		metadata["goal_max_iterations"] = "30"
	}
	metadata["goal_status"] = "pending"
	metadata["goal_iteration"] = "0"
	metadata["goal_created_at"] = now
	metadata["goal_updated_at"] = now
	metadata["goal_last_event"] = "created"
	return metadata
}

func enrichTaskMetadata(input map[string]string, agent model.Agent) map[string]string {
	metadata := map[string]string{}
	for key, value := range input {
		metadata[key] = value
	}
	if strings.TrimSpace(metadata["agent_name"]) == "" {
		if name := strings.TrimSpace(agent.Name); name != "" {
			metadata["agent_name"] = name
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func summarize(parts []model.Part) string {
	files := 0
	for _, part := range parts {
		if part.Type == "file" {
			files++
		}
	}
	if files == 0 {
		return "multi-part input"
	}
	if files == 1 && len(parts) == 1 {
		return "file input"
	}
	return fmt.Sprintf("multi-part input (%d files)", files)
}

func (a *API) ReportSupportLogs(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req reportSupportLogsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "content required"})
		return
	}
	if len(content) > 200000 {
		write(w, http.StatusBadRequest, map[string]string{"error": "content too large"})
		return
	}
	email, err := a.store.GetOperatorEmail(operator.ID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "load operator email failed"})
		return
	}
	if strings.TrimSpace(email) == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "operator email not found"})
		return
	}
	filePath, err := saveSupportLog(operator.Username, email, req)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("save support log failed: %v", err)})
		return
	}
	write(w, http.StatusOK, map[string]string{"message": "ok", "file_path": filePath})
}

func saveSupportLog(username, email string, req reportSupportLogsReq) (string, error) {
	now := time.Now()
	dayDir := filepath.Join(supportLogDir, now.Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return "", err
	}
	safeUsername := sanitizeFilename(username)
	if safeUsername == "" {
		safeUsername = "unknown"
	}
	batchID := extractBatchID(req.Content)
	parts := []string{now.Format("150405"), safeUsername}
	if batchID != "" {
		parts = append(parts, sanitizeFilename(batchID))
	}
	filename := strings.Join(parts, "_") + ".json"
	fullPath := filepath.Join(dayDir, filename)
	body := map[string]any{
		"received_at": now.Format(time.RFC3339),
		"username":    username,
		"email":       email,
		"category":    req.Category,
		"app_version": req.AppVersion,
		"base_url":    req.BaseURL,
		"note":        req.Note,
		"content":     req.Content,
		"batch_id":    batchID,
	}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		return "", err
	}
	return fullPath, nil
}

func extractBatchID(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "current_batch_id=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "current_batch_id="))
		}
	}
	return ""
}

func sanitizeFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-', r == '_', r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	return strings.Trim(builder.String(), "._")
}

func htmlEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
	)
	return replacer.Replace(value)
}

func slicesClone[T any](items []T) []T {
	return append([]T(nil), items...)
}
