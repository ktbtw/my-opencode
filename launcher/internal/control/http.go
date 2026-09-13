package control

import (
	"encoding/json"
	"net/http"
	"strings"

	"launcher/internal/model"
)

type Service interface {
	State() model.DeviceView
	AIConfig() (*model.DeviceAIConfigInfo, error)
	ListAIModels(model.DeviceAIConfigInput) (*model.DeviceAIConfigInfo, error)
	SaveAIConfig(model.DeviceAIConfigInfo) (*model.DeviceAIConfigInfo, error)
	SaveAIConfigText(string) (*model.DeviceAIConfigInfo, error)
	ClearAIProvider(string) (*model.DeviceAIConfigInfo, error)
	MCPConfig() (*model.DeviceMCPConfigInfo, error)
	MCPStatus(string) (*model.MCPStatusInfo, error)
	SaveMCPConfig(model.DeviceMCPConfigInfo) (*model.DeviceMCPConfigInfo, error)
	RemoveMCPConfig(string) (*model.DeviceMCPConfigInfo, error)
	AgentSemanticSelection(string) (model.DeviceAgentSemanticSelectionInfo, error)
	SaveAgentSemanticSelection(model.DeviceAgentSemanticSelectionInput) (model.DeviceAgentSemanticSelectionInfo, error)
	AgentSkillSelection(model.DeviceAgentSkillSelectionInput) (model.DeviceAgentSkillSelectionInfo, error)
	SaveAgentSkillSelection(model.DeviceAgentSkillSelectionInput) (model.DeviceAgentSkillSelectionInfo, error)
	ImportAgentSkill(model.DeviceAgentSkillImportInput) (model.DeviceAgentSkillSelectionInfo, error)
	GlobalSkillConfig() (*model.DeviceSkillConfigInfo, error)
	SaveGlobalSkillConfig(model.DeviceSkillConfigInfo) (*model.DeviceSkillConfigInfo, error)
	ImportGlobalSkill(model.DeviceSkillConfigInput) (*model.DeviceSkillConfigInfo, error)
	RuntimeEnvironment() (model.RuntimeEnvironmentInfo, error)
	PrepareRuntimeEnvironment() (model.RuntimeEnvironmentInfo, error)
	Directories(model.DirectoryRequest) (model.DirectoryListResult, error)
	Agents() []model.Agent
	CreateAgent(model.CreateAgentInput) (model.Agent, error)
	RestartAgent(string) (model.Agent, error)
	SetAgentEnabled(model.SetAgentEnabledInput) (model.Agent, error)
	RemoveAgent(string) error
	Versions() ([]string, error)
	StartUpgrade(model.UpgradeInput) error
	Upgrade(model.UpgradeInput) error
	Rollback() error
	AutostartStatus() model.LauncherAutostartInfo
	RefreshAutostartStatus() model.LauncherAutostartInfo
	EnableAutostart(model.LauncherAutostartInput) (model.LauncherAutostartInfo, error)
	DisableAutostart() (model.LauncherAutostartInfo, error)
	SelfUpdate(model.LauncherSelfUpdateInput) (model.LauncherSelfUpdateResult, error)
	DirectoryPermission(model.DirectoryPermissionInput) (model.DirectoryPermissionResult, error)
	Diagnostics(model.LauncherDiagnosticsInput) (model.LauncherDiagnostics, error)
	SSHStatus() model.SSHStatus
	SSHSetup() model.SSHSetupResult
	SSHInstallAuthorizedKey(string) model.SSHAuthorizedKeyResult
}

func NewHandler(svc Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, svc.State())
	})
	mux.HandleFunc("/ssh/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, svc.SSHStatus())
	})
	mux.HandleFunc("/ssh/setup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		result := svc.SSHSetup()
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("/ssh/authorized-key", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var input model.SSHAuthorizedKeyInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		result := svc.SSHInstallAuthorizedKey(input.PublicKey)
		status := http.StatusOK
		if result.Error != "" {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, result)
	})
	mux.HandleFunc("/ai-config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cfg, err := svc.AIConfig()
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, cfg)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/ai-config/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req model.DeviceAIConfigInput
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		cfg, err := svc.ListAIModels(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	})
	mux.HandleFunc("/ai-config/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req model.DeviceAIConfigInfo
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		cfg, err := svc.SaveAIConfig(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	})
	mux.HandleFunc("/ai-config/save-text", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		cfg, err := svc.SaveAIConfigText(req.Text)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	})
	mux.HandleFunc("/ai-config/clear-provider", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Provider string `json:"provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		cfg, err := svc.ClearAIProvider(req.Provider)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	})
	mux.HandleFunc("/mcp-config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cfg, err := svc.MCPConfig()
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, cfg)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/mcp-config/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req model.DeviceMCPConfigInfo
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		cfg, err := svc.SaveMCPConfig(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	})
	mux.HandleFunc("/mcp-config/remove", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		cfg, err := svc.RemoveMCPConfig(req.Name)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	})
	mux.HandleFunc("/skills-config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			cfg, err := svc.GlobalSkillConfig()
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, cfg)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req model.DeviceSkillConfigInfo
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		cfg, err := svc.SaveGlobalSkillConfig(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	})
	mux.HandleFunc("/skills-config/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req model.DeviceSkillConfigInput
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		cfg, err := svc.ImportGlobalSkill(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	})
	mux.HandleFunc("/mcp-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		status, err := svc.MCPStatus(strings.TrimSpace(r.URL.Query().Get("agent_id")))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("/runtime-env", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		info, err := svc.RuntimeEnvironment()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("/runtime-env/prepare", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		info, err := svc.PrepareRuntimeEnvironment()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("/directories", func(w http.ResponseWriter, r *http.Request) {
		result, err := svc.Directories(model.DirectoryRequest{
			Path:     r.URL.Query().Get("path"),
			AllowAll: strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("allow_all")), "true"),
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("/versions", func(w http.ResponseWriter, _ *http.Request) {
		versions, err := svc.Versions()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
	})
	mux.HandleFunc("/upgrade", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req model.UpgradeInput
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if err := svc.StartUpgrade(req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "state": svc.State()})
	})
	mux.HandleFunc("/rollback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := svc.Rollback(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})
	mux.HandleFunc("/autostart/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		refresh := r.URL.Query().Get("refresh")
		if refresh == "1" || strings.EqualFold(refresh, "true") {
			writeJSON(w, http.StatusOK, svc.RefreshAutostartStatus())
			return
		}
		writeJSON(w, http.StatusOK, svc.AutostartStatus())
	})
	mux.HandleFunc("/autostart/enable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req model.LauncherAutostartInput
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}
		info, err := svc.EnableAutostart(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("/autostart/disable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		info, err := svc.DisableAutostart()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("/self-update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req model.LauncherSelfUpdateInput
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		result, err := svc.SelfUpdate(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("/directory-permission", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req model.DirectoryPermissionInput
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		result, err := svc.DirectoryPermission(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, result)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req model.LauncherDiagnosticsInput
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
				return
			}
		}
		result, err := svc.Diagnostics(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("/agents", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]any{"agents": svc.Agents()})
			return
		}
		if r.Method == http.MethodPost {
			var req model.CreateAgentInput
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
				return
			}
			agent, err := svc.CreateAgent(req)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, agent)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/agents/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/agents/")
		if strings.HasSuffix(path, "/skills") {
			agentID := strings.Trim(strings.TrimSuffix(path, "/skills"), "/")
			input := model.DeviceAgentSkillSelectionInput{AgentID: agentID}
			if r.Method == http.MethodPost {
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
					return
				}
				input.AgentID = agentID
			}
			if r.Method != http.MethodGet && r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			var (
				selection model.DeviceAgentSkillSelectionInfo
				err       error
			)
			if r.Method == http.MethodGet {
				selection, err = svc.AgentSkillSelection(input)
			} else {
				selection, err = svc.SaveAgentSkillSelection(input)
			}
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, selection)
			return
		}
		if strings.HasSuffix(path, "/skills/import") {
			agentID := strings.Trim(strings.TrimSuffix(path, "/skills/import"), "/")
			var input model.DeviceAgentSkillImportInput
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
				return
			}
			input.AgentID = agentID
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			selection, err := svc.ImportAgentSkill(input)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, selection)
			return
		}
		if strings.HasSuffix(path, "/semantic-agent") {
			agentID := strings.TrimSuffix(path, "/semantic-agent")
			agentID = strings.Trim(agentID, "/")
			if r.Method == http.MethodGet {
				selection, err := svc.AgentSemanticSelection(agentID)
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
				writeJSON(w, http.StatusOK, selection)
				return
			}
			if r.Method == http.MethodPost {
				var req model.DeviceAgentSemanticSelectionInput
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
					return
				}
				req.AgentID = agentID
				selection, err := svc.SaveAgentSemanticSelection(req)
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
				writeJSON(w, http.StatusOK, selection)
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if strings.HasSuffix(path, "/restart") && r.Method == http.MethodPost {
			agentID := strings.TrimSuffix(path, "/restart")
			agentID = strings.TrimSuffix(agentID, "/")
			agent, err := svc.RestartAgent(agentID)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, agent)
			return
		}
		if strings.HasSuffix(path, "/enabled") && r.Method == http.MethodPost {
			agentID := strings.TrimSuffix(path, "/enabled")
			agentID = strings.TrimSuffix(agentID, "/")
			var req struct {
				Enabled bool `json:"enabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
				return
			}
			agent, err := svc.SetAgentEnabled(model.SetAgentEnabledInput{
				AgentID: strings.Trim(agentID, "/"),
				Enabled: req.Enabled,
			})
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, agent)
			return
		}
		if r.Method == http.MethodDelete {
			if err := svc.RemoveAgent(strings.Trim(path, "/")); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": true})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
