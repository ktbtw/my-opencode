package relay

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"launcher/internal/config"
	"launcher/internal/model"
	launcherVersion "launcher/internal/version"
)

type envelope struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	SentAt    string `json:"sent_at,omitempty"`
	Payload   any    `json:"payload,omitempty"`
}

type handlerFunc func(envelope, config.Config, Service) envelope

const (
	defaultHeartbeatInterval = 15 * time.Second
	relayWebSocketReadLimit  = 64 << 20
)

func Start(ctx context.Context, cfg config.Config, svc Service) {
	if cfg.Relay.URL == "" || cfg.Relay.OperatorKey == "" || cfg.Relay.MachineID == "" {
		return
	}
	go func() {
		for {
			if err := connect(ctx, cfg, svc); err != nil {
				log.Printf("[launcher.relay] %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()
}

func connect(ctx context.Context, cfg config.Config, svc Service) error {
	conn, _, err := websocket.Dial(ctx, cfg.Relay.URL, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(relayWebSocketReadLimit)
	var writeMu sync.Mutex
	writeEnvelope := func(ctx context.Context, msg envelope) error {
		buf, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.Write(ctx, websocket.MessageText, buf)
	}
	msg := envelope{
		Type:      "device.hello",
		RequestID: "launcher_hello",
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: map[string]any{
			"kind":         "launcher",
			"device_id":    "launcher:" + cfg.Relay.MachineID,
			"machine_id":   cfg.Relay.MachineID,
			"hostname":     cfg.Relay.Hostname,
			"operator_key": cfg.Relay.OperatorKey,
			"version":      "launcher-" + launcherVersion.Value,
			"capabilities": []string{"device_compaction_config_v1"},
			"projects":     []map[string]any{},
		},
	}
	if err := writeEnvelope(ctx, msg); err != nil {
		return err
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	heartbeatStarted := false
	handlers := buildHandlers()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		if env.Type == "device.welcome" {
			if !heartbeatStarted {
				body, err := json.Marshal(env.Payload)
				if err == nil {
					var welcome model.WelcomePayload
					if err := json.Unmarshal(body, &welcome); err == nil {
						heartbeatStarted = true
						go runHeartbeat(heartbeatCtx, welcome.AgentID, welcome.HeartbeatIntervalSec, writeEnvelope)
					}
				}
			}
			continue
		}
		if env.Type == "device.launcher.agent_preflight.start" {
			handleAgentSemanticPreflight(ctx, env, cfg, svc, writeEnvelope)
			continue
		}
		if env.Type == "device.launcher.tunnel.open" {
			go handleTunnelOpen(heartbeatCtx, env, cfg)
			continue
		}
		handler, ok := handlers[env.Type]
		if !ok {
			continue
		}
		result := handler(env, cfg, svc)
		if err := writeEnvelope(ctx, result); err != nil {
			return err
		}
	}
}

func runHeartbeat(
	ctx context.Context,
	agentID string,
	intervalSec int,
	write func(context.Context, envelope) error,
) {
	interval := defaultHeartbeatInterval
	if intervalSec > 0 {
		interval = time.Duration(intervalSec) * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := write(sendCtx, envelope{
				Type:      "device.heartbeat",
				RequestID: "launcher_heartbeat",
				SentAt:    time.Now().UTC().Format(time.RFC3339),
				Payload: model.HeartbeatPayload{
					AgentID: agentID,
				},
			})
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func buildHandlers() map[string]handlerFunc {
	return map[string]handlerFunc{
		"device.launcher.get":                      handleLauncher,
		"device.launcher.upgrade":                  handleLauncher,
		"device.launcher.rollback":                 handleLauncher,
		"device.launcher.create_agent":             handleLauncher,
		"device.launcher.rename_agent":             handleLauncher,
		"device.launcher.restart_agent":            handleLauncher,
		"device.launcher.project_identity_correct": handleLauncher,
		"device.launcher.set_agent_enabled":        handleLauncher,
		"device.launcher.remove_agent":             handleLauncher,
		"device.launcher.mcp_status":               handleLauncher,
		"device.launcher.semantic_agent_get":       handleLauncher,
		"device.launcher.semantic_agent_save":      handleLauncher,
		"device.launcher.skills_get":               handleLauncher,
		"device.launcher.skills_save":              handleLauncher,
		"device.launcher.skills_import":            handleLauncher,
		"device.launcher.skills_global_get":        handleLauncher,
		"device.launcher.skills_global_save":       handleLauncher,
		"device.launcher.skills_global_import":     handleLauncher,
		"device.launcher.autostart_status":         handleLauncher,
		"device.launcher.autostart_enable":         handleLauncher,
		"device.launcher.autostart_disable":        handleLauncher,
		"device.launcher.self_update":              handleLauncher,
		"device.launcher.directory_permission":     handleLauncher,
		"device.launcher.diagnostics":              handleLauncher,
		"device.launcher.ssh_status":               handleLauncher,
		"device.launcher.ssh_setup":                handleLauncher,
		"device.launcher.ssh_authorized_key":       handleLauncher,
		"device.ai_config.get":                     handleAIConfig,
		"device.ai_config.list_models":             handleAIConfig,
		"device.ai_config.save":                    handleAIConfig,
		"device.ai_config.save_text":               handleAIConfig,
		"device.ai_config.clear_provider":          handleAIConfig,
		"device.mcp_config.get":                    handleMCPConfig,
		"device.mcp_config.agent_get":              handleMCPConfig,
		"device.mcp_config.agent_save":             handleMCPConfig,
		"device.mcp_config.save":                   handleMCPConfig,
		"device.mcp_config.remove":                 handleMCPConfig,
		"device.env_config.get":                    handleEnvConfig,
		"device.env_config.save":                   handleEnvConfig,
		"device.compaction_config.get":             handleCompactionConfig,
		"device.compaction_config.save":            handleCompactionConfig,
		"device.directories.get":                   handleDirectories,
		"device.directory_files.upload_create":     handleDirectoryFiles,
		"device.directory_files.upload_chunk":      handleDirectoryFiles,
		"device.directory_files.upload_complete":   handleDirectoryFiles,
		"device.directory_files.create_file":       handleDirectoryFiles,
		"device.directory_files.mkdir":             handleDirectoryFiles,
		"device.directory_files.delete":            handleDirectoryFiles,
		"device.project_files.list":                handleProjectFiles,
		"device.project_files.download":            handleProjectFiles,
		"device.project_files.download_create":     handleProjectFiles,
		"device.project_files.download_chunk":      handleProjectFiles,
		"device.project_files.upload":              handleProjectFiles,
		"device.project_files.upload_create":       handleProjectFiles,
		"device.project_files.upload_chunk":        handleProjectFiles,
		"device.project_files.upload_complete":     handleProjectFiles,
		"device.project_files.create_file":         handleProjectFiles,
		"device.project_files.mkdir":               handleProjectFiles,
		"device.project_files.delete":              handleProjectFiles,
		"device.project_files.rename":              handleProjectFiles,
	}
}

func handleAgentSemanticPreflight(
	ctx context.Context,
	env envelope,
	cfg config.Config,
	svc Service,
	write func(context.Context, envelope) error,
) {
	var payload model.RuntimePreflightStartPayload
	blob, _ := json.Marshal(env.Payload)
	_ = json.Unmarshal(blob, &payload)
	if strings.TrimSpace(payload.JobID) == "" {
		payload.JobID = env.RequestID
	}
	sendStatus := func(status model.RuntimePreflightStatusPayload) error {
		status.JobID = firstNonEmpty(status.JobID, payload.JobID)
		status.MachineID = firstNonEmpty(status.MachineID, cfg.Relay.MachineID)
		status.LauncherAgentID = firstNonEmpty(status.LauncherAgentID, payload.LauncherAgentID)
		status.SemanticAgentID = firstNonEmpty(status.SemanticAgentID, payload.SemanticAgentID)
		sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return write(sendCtx, envelope{
			Type:      "device.launcher.agent_preflight.status",
			RequestID: status.JobID,
			SentAt:    time.Now().UTC().Format(time.RFC3339),
			Payload:   status,
		})
	}
	_ = sendStatus(model.RuntimePreflightStatusPayload{
		Status:          "running",
		CurrentStep:     "runtime",
		ProgressPercent: 2,
		Events: []model.RuntimeInstallEvent{{
			ItemType:        "job",
			Phase:           "accepted",
			Status:          "info",
			Message:         "launcher 已接收语义 Agent 预检任务",
			ProgressPercent: 2,
		}},
	})
	go func() {
		if err := svc.StartAgentSemanticPreflight(payload, sendStatus); err != nil {
			_ = sendStatus(model.RuntimePreflightStatusPayload{
				Status:          "failed",
				CurrentStep:     "runtime",
				ProgressPercent: 100,
				Error:           err.Error(),
				Events: []model.RuntimeInstallEvent{{
					ItemType:        "job",
					Phase:           "failed",
					Status:          "error",
					Message:         err.Error(),
					ProgressPercent: 100,
				}},
			})
		}
	}()
}

func handleLauncher(env envelope, cfg config.Config, svc Service) envelope {
	base := envelope{
		Type:      "device.launcher.result",
		RequestID: env.RequestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
	}
	statePayload := func(view model.DeviceView, action string, success bool, errText string) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    success,
				"error":      errText,
				"state":      view,
			},
		}
	}
	mcpPayload := func(status *model.MCPStatusInfo, success bool, errText string) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     "mcp_status",
				"success":    success,
				"error":      errText,
				"mcp_status": status,
			},
		}
	}
	semanticPayload := func(action string, selection *model.DeviceAgentSemanticSelectionInfo, success bool, errText string) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id":         cfg.Relay.MachineID,
				"action":             action,
				"success":            success,
				"error":              errText,
				"semantic_selection": selection,
				"state":              svc.State(),
			},
		}
	}
	skillPayload := func(action string, selection *model.DeviceAgentSkillSelectionInfo, success bool, errText string) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id":      cfg.Relay.MachineID,
				"action":          action,
				"success":         success,
				"error":           errText,
				"skill_selection": selection,
				"state":           svc.State(),
			},
		}
	}
	globalSkillPayload := func(action string, configInfo *model.DeviceSkillConfigInfo, success bool, errText string) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id":   cfg.Relay.MachineID,
				"action":       action,
				"success":      success,
				"error":        errText,
				"skill_config": configInfo,
				"state":        svc.State(),
			},
		}
	}
	autostartPayload := func(info model.LauncherAutostartInfo, action string, success bool, errText string) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    success,
				"error":      errText,
				"autostart":  info,
				"state":      svc.State(),
			},
		}
	}
	selfUpdatePayload := func(result model.LauncherSelfUpdateResult, success bool, errText string) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id":  cfg.Relay.MachineID,
				"action":      "self_update",
				"success":     success,
				"error":       errText,
				"self_update": result,
				"state":       svc.State(),
			},
		}
	}
	permissionPayload := func(result model.DirectoryPermissionResult, success bool, errText string) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     "directory_permission",
				"success":    success,
				"error":      errText,
				"permission": result,
				"state":      svc.State(),
			},
		}
	}
	diagnosticsPayload := func(result *model.LauncherDiagnostics, success bool, errText string) envelope {
		return envelope{
			Type: base.Type, RequestID: base.RequestID, SentAt: base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID, "action": "diagnostics",
				"success": success, "error": errText, "diagnostics": result,
			},
		}
	}
	sshPayload := func(status model.SSHStatus) envelope {
		return envelope{
			Type: base.Type, RequestID: base.RequestID, SentAt: base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID, "action": "ssh_status",
				"success": true, "ssh": status,
			},
		}
	}
	setupPayload := func(result model.SSHSetupResult) envelope {
		return envelope{
			Type: base.Type, RequestID: base.RequestID, SentAt: base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID, "action": "ssh_setup",
				"success": result.Error == "" || result.RequiresUserAction, "error": result.Error, "ssh": result.Status,
				"setup": result,
			},
		}
	}
	authorizedKeyPayload := func(result model.SSHManagedAuthorizedKeyResult) envelope {
		return envelope{
			Type: base.Type, RequestID: base.RequestID, SentAt: base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID, "action": "ssh_authorized_key",
				"success": result.Error == "", "error": result.Error, "authorized_key": result,
			},
		}
	}
	switch env.Type {
	case "device.launcher.get":
		return statePayload(svc.State(), "get", true, "")
	case "device.launcher.upgrade":
		var payload model.UpgradeInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		if err := svc.StartUpgrade(payload); err != nil {
			return statePayload(svc.State(), "upgrade", false, err.Error())
		}
		return statePayload(svc.State(), "upgrade", true, "")
	case "device.launcher.rollback":
		if err := svc.Rollback(); err != nil {
			return statePayload(svc.State(), "rollback", false, err.Error())
		}
		return statePayload(svc.State(), "rollback", true, "")
	case "device.launcher.create_agent":
		var payload model.CreateAgentInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		if _, err := svc.CreateAgent(payload); err != nil {
			return statePayload(svc.State(), "create_agent", false, err.Error())
		}
		return statePayload(svc.State(), "create_agent", true, "")
	case "device.launcher.rename_agent":
		var payload model.RenameAgentInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		if _, err := svc.RenameAgent(payload); err != nil {
			return statePayload(svc.State(), "rename_agent", false, err.Error())
		}
		return statePayload(svc.State(), "rename_agent", true, "")
	case "device.launcher.restart_agent":
		var payload struct {
			AgentID string `json:"agent_id"`
		}
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		if _, err := svc.RestartAgent(payload.AgentID); err != nil {
			return statePayload(svc.State(), "restart_agent", false, err.Error())
		}
		return statePayload(svc.State(), "restart_agent", true, "")
	case "device.launcher.project_identity_correct":
		var payload model.ProjectIdentityCorrectionInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		identity, err := svc.CorrectProjectIdentity(payload)
		errText := ""
		if err != nil {
			errText = err.Error()
		}
		return envelope{
			Type: base.Type, RequestID: base.RequestID, SentAt: base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID, "action": "project_identity_correct",
				"success": err == nil, "error": errText,
				"identity": identity, "state": svc.State(),
			},
		}
	case "device.launcher.set_agent_enabled":
		var payload model.SetAgentEnabledInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		if _, err := svc.SetAgentEnabled(payload); err != nil {
			return statePayload(svc.State(), "set_agent_enabled", false, err.Error())
		}
		return statePayload(svc.State(), "set_agent_enabled", true, "")
	case "device.launcher.remove_agent":
		var payload struct {
			AgentID string `json:"agent_id"`
		}
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		if err := svc.RemoveAgent(payload.AgentID); err != nil {
			return statePayload(svc.State(), "remove_agent", false, err.Error())
		}
		return statePayload(svc.State(), "remove_agent", true, "")
	case "device.launcher.mcp_status":
		var payload struct {
			AgentID string `json:"agent_id"`
		}
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		status, err := svc.MCPStatus(strings.TrimSpace(payload.AgentID))
		if err != nil {
			return mcpPayload(nil, false, err.Error())
		}
		return mcpPayload(status, true, "")
	case "device.launcher.semantic_agent_get":
		var payload model.DeviceAgentSemanticSelectionInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		selection, err := svc.AgentSemanticSelection(payload.AgentID)
		if err != nil {
			return semanticPayload("semantic_agent_get", nil, false, err.Error())
		}
		return semanticPayload("semantic_agent_get", &selection, true, "")
	case "device.launcher.semantic_agent_save":
		var payload model.DeviceAgentSemanticSelectionInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		selection, err := svc.SaveAgentSemanticSelection(payload)
		if err != nil {
			return semanticPayload("semantic_agent_save", nil, false, err.Error())
		}
		return semanticPayload("semantic_agent_save", &selection, true, "")
	case "device.launcher.skills_get", "device.launcher.skills_save":
		var payload model.DeviceAgentSkillSelectionInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		var selection model.DeviceAgentSkillSelectionInfo
		var err error
		if env.Type == "device.launcher.skills_get" {
			selection, err = svc.AgentSkillSelection(payload)
		} else {
			selection, err = svc.SaveAgentSkillSelection(payload)
		}
		errText := ""
		if err != nil {
			errText = err.Error()
		}
		return skillPayload(env.Type, &selection, err == nil, errText)
	case "device.launcher.skills_import":
		var payload model.DeviceAgentSkillImportInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		selection, err := svc.ImportAgentSkill(payload)
		errText := ""
		if err != nil {
			errText = err.Error()
		}
		return skillPayload("skills_import", &selection, err == nil, errText)
	case "device.launcher.skills_global_get":
		configInfo, err := svc.GlobalSkillConfig()
		return globalSkillPayload("global_get", configInfo, err == nil, relayErrorText(err))
	case "device.launcher.skills_global_save":
		var input model.DeviceSkillConfigInfo
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &input)
		configInfo, err := svc.SaveGlobalSkillConfig(input)
		return globalSkillPayload("global_save", configInfo, err == nil, relayErrorText(err))
	case "device.launcher.skills_global_import":
		var input model.DeviceSkillConfigInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &input)
		configInfo, err := svc.ImportGlobalSkill(input)
		return globalSkillPayload("global_import", configInfo, err == nil, relayErrorText(err))
	case "device.launcher.autostart_status":
		return autostartPayload(svc.AutostartStatus(), "autostart_status", true, "")
	case "device.launcher.autostart_enable":
		var payload model.LauncherAutostartInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		info, err := svc.EnableAutostart(payload)
		if err != nil {
			return autostartPayload(info, "autostart_enable", false, err.Error())
		}
		return autostartPayload(info, "autostart_enable", true, "")
	case "device.launcher.autostart_disable":
		info, err := svc.DisableAutostart()
		if err != nil {
			return autostartPayload(info, "autostart_disable", false, err.Error())
		}
		return autostartPayload(info, "autostart_disable", true, "")
	case "device.launcher.self_update":
		var payload model.LauncherSelfUpdateInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		result, err := svc.SelfUpdate(payload)
		if err != nil {
			return selfUpdatePayload(result, false, err.Error())
		}
		return selfUpdatePayload(result, true, "")
	case "device.launcher.directory_permission":
		var payload model.DirectoryPermissionInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		result, err := svc.DirectoryPermission(payload)
		if err != nil {
			return permissionPayload(result, false, err.Error())
		}
		return permissionPayload(result, true, "")
	case "device.launcher.diagnostics":
		var payload model.LauncherDiagnosticsInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		result, err := svc.Diagnostics(payload)
		if err != nil {
			return diagnosticsPayload(nil, false, err.Error())
		}
		return diagnosticsPayload(&result, true, "")
	case "device.launcher.ssh_status":
		provider, ok := svc.(interface{ SSHStatus() model.SSHStatus })
		if !ok {
			status := model.SSHStatus{Error: "launcher 不支持 SSH 状态探测"}
			result := sshPayload(status)
			payload := result.Payload.(map[string]any)
			payload["success"] = false
			payload["error"] = status.Error
			return result
		}
		return sshPayload(provider.SSHStatus())
	case "device.launcher.ssh_setup":
		provider, ok := svc.(interface{ SSHSetup() model.SSHSetupResult })
		if !ok {
			return setupPayload(model.SSHSetupResult{Error: "launcher 不支持 SSH 初始化"})
		}
		return setupPayload(provider.SSHSetup())
	case "device.launcher.ssh_authorized_key":
		provider, ok := svc.(interface {
			SSHInstallManagedAuthorizedKey(model.SSHManagedAuthorizedKeyInput) model.SSHManagedAuthorizedKeyResult
		})
		if !ok {
			return authorizedKeyPayload(model.SSHManagedAuthorizedKeyResult{Error: "launcher 不支持 SSH 公钥自动登记"})
		}
		var payload model.SSHManagedAuthorizedKeyInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &payload)
		return authorizedKeyPayload(provider.SSHInstallManagedAuthorizedKey(payload))
	default:
		return statePayload(svc.State(), env.Type, false, "unsupported launcher command")
	}
}

func handleAIConfig(env envelope, cfg config.Config, svc Service) envelope {
	base := envelope{
		Type:      "device.ai_config.result",
		RequestID: env.RequestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
	}
	result := func(action string, success bool, cfgInfo *model.DeviceAIConfigInfo, errText string) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    success,
				"error":      errText,
				"config":     cfgInfo,
			},
		}
	}
	switch env.Type {
	case "device.ai_config.get":
		cfgInfo, err := svc.AIConfig()
		if err != nil {
			return result("get", false, nil, err.Error())
		}
		return result("get", true, cfgInfo, "")
	case "device.ai_config.list_models":
		var input model.DeviceAIConfigInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &input)
		cfgInfo, err := svc.ListAIModels(input)
		if err != nil {
			return result("list_models", false, nil, err.Error())
		}
		return result("list_models", true, cfgInfo, "")
	case "device.ai_config.save":
		input, err := decodeSaveAIConfigInput(env.Payload)
		if err != nil {
			return result("save", false, nil, err.Error())
		}
		cfgInfo, err := svc.SaveAIConfig(input)
		if err != nil {
			return result("save", false, nil, err.Error())
		}
		return result("save", true, cfgInfo, "")
	case "device.ai_config.save_text":
		var input struct {
			Text string `json:"text"`
		}
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &input)
		cfgInfo, err := svc.SaveAIConfigText(input.Text)
		if err != nil {
			return result("save_text", false, nil, err.Error())
		}
		return result("save_text", true, cfgInfo, "")
	case "device.ai_config.clear_provider":
		var input struct {
			Provider string `json:"provider"`
		}
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &input)
		cfgInfo, err := svc.ClearAIProvider(input.Provider)
		if err != nil {
			return result("clear_provider", false, nil, err.Error())
		}
		return result("clear_provider", true, cfgInfo, "")
	default:
		return result(env.Type, false, nil, "unsupported ai config command")
	}
}

func handleMCPConfig(env envelope, cfg config.Config, svc Service) envelope {
	base := envelope{
		Type:      "device.mcp_config.result",
		RequestID: env.RequestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
	}
	result := func(action string, success bool, cfgInfo *model.DeviceMCPConfigInfo, selection *model.DeviceAgentMCPSelectionInfo, errText string) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    success,
				"error":      errText,
				"config":     cfgInfo,
				"selection":  selection,
			},
		}
	}
	switch env.Type {
	case "device.mcp_config.get":
		cfgInfo, err := svc.MCPConfig()
		if err != nil {
			return result("get", false, nil, nil, err.Error())
		}
		return result("get", true, cfgInfo, nil, "")
	case "device.mcp_config.agent_get":
		var input model.DeviceAgentMCPSelectionInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &input)
		selection, err := svc.AgentMCPSelection(input.AgentID)
		if err != nil {
			return result("agent_get", false, nil, nil, err.Error())
		}
		return result("agent_get", true, nil, &selection, "")
	case "device.mcp_config.agent_save":
		var input model.DeviceAgentMCPSelectionInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &input)
		selection, err := svc.SaveAgentMCPSelection(input)
		if err != nil {
			return result("agent_save", false, nil, nil, err.Error())
		}
		return result("agent_save", true, nil, &selection, "")
	case "device.mcp_config.save":
		var input model.DeviceMCPConfigInfo
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &input)
		cfgInfo, err := svc.SaveMCPConfig(input)
		if err != nil {
			return result("save", false, nil, nil, err.Error())
		}
		return result("save", true, cfgInfo, nil, "")
	case "device.mcp_config.remove":
		var input struct {
			Name string `json:"name"`
		}
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &input)
		cfgInfo, err := svc.RemoveMCPConfig(input.Name)
		if err != nil {
			return result("remove", false, nil, nil, err.Error())
		}
		return result("remove", true, cfgInfo, nil, "")
	default:
		return result(env.Type, false, nil, nil, "unsupported mcp config command")
	}
}

func relayErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func decodeSaveAIConfigInput(payload any) (model.DeviceAIConfigInfo, error) {
	var input struct {
		Provider   string                    `json:"provider"`
		BaseURL    string                    `json:"base_url"`
		ConsoleURL string                    `json:"console_url"`
		APIKey     string                    `json:"api_key"`
		APIMode    string                    `json:"api_mode"`
		Model      string                    `json:"model"`
		Force      bool                      `json:"force"`
		Models     []model.DeviceAIModelInfo `json:"models"`
		Config     struct {
			Provider   string                    `json:"provider"`
			BaseURL    string                    `json:"base_url"`
			ConsoleURL string                    `json:"console_url"`
			APIKey     string                    `json:"api_key"`
			APIMode    string                    `json:"api_mode"`
			Model      string                    `json:"model"`
			Models     []model.DeviceAIModelInfo `json:"models"`
		} `json:"config"`
	}
	blob, err := json.Marshal(payload)
	if err != nil {
		return model.DeviceAIConfigInfo{}, err
	}
	if err := json.Unmarshal(blob, &input); err != nil {
		return model.DeviceAIConfigInfo{}, err
	}
	models := input.Config.Models
	if len(models) == 0 {
		models = input.Models
	}
	return model.DeviceAIConfigInfo{
		Provider:     firstNonEmpty(input.Config.Provider, input.Provider),
		BaseURL:      firstNonEmpty(input.Config.BaseURL, input.BaseURL),
		ConsoleURL:   firstNonEmpty(input.Config.ConsoleURL, input.ConsoleURL),
		APIKeyMasked: firstNonEmpty(input.Config.APIKey, input.APIKey),
		APIMode:      firstNonEmpty(input.Config.APIMode, input.APIMode),
		Model:        firstNonEmpty(input.Config.Model, input.Model),
		Force:        input.Force,
		Models:       models,
	}, nil
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

func handleEnvConfig(env envelope, cfg config.Config, svc Service) envelope {
	base := envelope{
		Type:      "device.env_config.result",
		RequestID: env.RequestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
	}
	result := func(action string, success bool, info *model.DeviceEnvConfigInfo, errText string) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    success,
				"error":      errText,
				"config":     info,
			},
		}
	}
	switch env.Type {
	case "device.env_config.get":
		info, err := svc.EnvConfig()
		if err != nil {
			return result("get", false, nil, err.Error())
		}
		return result("get", true, info, "")
	case "device.env_config.save":
		var input model.DeviceEnvConfigInput
		blob, _ := json.Marshal(env.Payload)
		_ = json.Unmarshal(blob, &input)
		info, err := svc.SaveEnvConfig(input)
		if err != nil {
			return result("save", false, nil, err.Error())
		}
		return result("save", true, info, "")
	default:
		return result(env.Type, false, nil, "unsupported env config command")
	}
}

func handleCompactionConfig(env envelope, cfg config.Config, svc Service) envelope {
	base := envelope{
		Type:      "device.launcher.result",
		RequestID: env.RequestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
	}
	result := func(action string, success bool, info *model.DeviceAgentCompactionConfig, errText string) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id":        cfg.Relay.MachineID,
				"action":            action,
				"success":           success,
				"error":             errText,
				"compaction_config": info,
			},
		}
	}
	var payload model.DeviceAgentCompactionConfigInput
	blob, _ := json.Marshal(env.Payload)
	_ = json.Unmarshal(blob, &payload)
	provider, ok := svc.(interface {
		AgentCompactionConfig(string) (model.DeviceAgentCompactionConfig, error)
		SaveAgentCompactionConfig(model.DeviceAgentCompactionConfigInput) (model.DeviceAgentCompactionConfig, error)
	})
	if !ok {
		return result("unsupported", false, nil, "launcher 不支持上下文压缩配置")
	}
	switch env.Type {
	case "device.compaction_config.get":
		info, err := provider.AgentCompactionConfig(payload.AgentID)
		if err != nil {
			return result("get", false, nil, err.Error())
		}
		return result("get", true, &info, "")
	case "device.compaction_config.save":
		info, err := provider.SaveAgentCompactionConfig(payload)
		if err != nil {
			return result("save", false, nil, err.Error())
		}
		return result("save", true, &info, "")
	default:
		return result(env.Type, false, nil, "unsupported compaction config command")
	}
}

func handleDirectories(env envelope, cfg config.Config, svc Service) envelope {
	var payload model.DirectoryRequest
	blob, _ := json.Marshal(env.Payload)
	_ = json.Unmarshal(blob, &payload)
	result, err := svc.Directories(payload)
	if err != nil {
		return envelope{
			Type:      "device.directories.result",
			RequestID: env.RequestID,
			SentAt:    time.Now().UTC().Format(time.RFC3339),
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     "get",
				"success":    false,
				"error":      err.Error(),
			},
		}
	}
	return envelope{
		Type:      "device.directories.result",
		RequestID: env.RequestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: map[string]any{
			"machine_id":   cfg.Relay.MachineID,
			"action":       "get",
			"success":      true,
			"current_path": result.CurrentPath,
			"parent_path":  result.ParentPath,
			"entries":      result.Entries,
		},
	}
}

func handleDirectoryFiles(env envelope, cfg config.Config, svc Service) envelope {
	var payload model.DirectoryFileRequest
	blob, _ := json.Marshal(env.Payload)
	_ = json.Unmarshal(blob, &payload)
	action := strings.TrimPrefix(env.Type, "device.directory_files.")
	base := envelope{
		Type:      "device.directories.result",
		RequestID: env.RequestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
	}
	fail := func(err error) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    false,
				"error":      err.Error(),
			},
		}
	}
	var (
		file model.ProjectFile
		err  error
	)
	switch action {
	case "upload_create":
		file, err = svc.CreateDirectoryUpload(payload)
	case "upload_chunk":
		file, err = svc.WriteDirectoryUploadChunk(payload)
	case "upload_complete":
		file, err = svc.CompleteDirectoryUpload(payload)
	case "create_file":
		file, err = svc.CreateDirectoryFile(payload)
	case "mkdir":
		file, err = svc.CreateDirectoryFolder(payload)
	case "delete":
		file, err = svc.DeleteDirectoryFile(payload)
	default:
		err = errors.New("不支持的设备目录文件操作")
	}
	if err != nil {
		return fail(err)
	}
	return envelope{
		Type:      base.Type,
		RequestID: base.RequestID,
		SentAt:    base.SentAt,
		Payload: map[string]any{
			"machine_id": cfg.Relay.MachineID,
			"action":     action,
			"success":    true,
			"file":       file,
		},
	}
}

func handleProjectFiles(env envelope, cfg config.Config, svc Service) envelope {
	var payload model.ProjectFilesRequest
	blob, _ := json.Marshal(env.Payload)
	_ = json.Unmarshal(blob, &payload)
	action := strings.TrimPrefix(env.Type, "device.project_files.")
	base := envelope{
		Type:      "device.directories.result",
		RequestID: env.RequestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
	}
	fail := func(err error) envelope {
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    false,
				"error":      err.Error(),
			},
		}
	}
	switch action {
	case "list":
		result, err := svc.ProjectFiles(payload)
		if err != nil {
			return fail(err)
		}
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id":   cfg.Relay.MachineID,
				"action":       action,
				"success":      true,
				"current_path": result.CurrentPath,
				"parent_path":  result.ParentPath,
				"entries":      result.Entries,
			},
		}
	case "download":
		file, err := svc.DownloadProjectFile(payload)
		if err != nil {
			return fail(err)
		}
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    true,
				"file":       file,
			},
		}
	case "download_create":
		file, err := svc.CreateProjectDownload(payload)
		if err != nil {
			return fail(err)
		}
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    true,
				"file":       file,
			},
		}
	case "download_chunk":
		file, err := svc.ReadProjectDownloadChunk(payload)
		if err != nil {
			return fail(err)
		}
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    true,
				"file":       file,
			},
		}
	case "upload":
		file, err := svc.UploadProjectFile(payload)
		if err != nil {
			return fail(err)
		}
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    true,
				"file":       file,
			},
		}
	case "upload_create":
		file, err := svc.CreateProjectUpload(payload)
		if err != nil {
			return fail(err)
		}
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    true,
				"file":       file,
			},
		}
	case "upload_chunk":
		file, err := svc.WriteProjectUploadChunk(payload)
		if err != nil {
			return fail(err)
		}
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    true,
				"file":       file,
			},
		}
	case "upload_complete":
		file, err := svc.CompleteProjectUpload(payload)
		if err != nil {
			return fail(err)
		}
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    true,
				"file":       file,
			},
		}
	case "create_file":
		file, err := svc.CreateProjectFile(payload)
		if err != nil {
			return fail(err)
		}
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    true,
				"file":       file,
			},
		}
	case "mkdir":
		file, err := svc.CreateProjectFolder(payload)
		if err != nil {
			return fail(err)
		}
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    true,
				"file":       file,
			},
		}
	case "delete":
		file, err := svc.DeleteProjectFile(payload)
		if err != nil {
			return fail(err)
		}
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    true,
				"file":       file,
			},
		}
	case "rename":
		file, err := svc.RenameProjectFile(payload)
		if err != nil {
			return fail(err)
		}
		return envelope{
			Type:      base.Type,
			RequestID: base.RequestID,
			SentAt:    base.SentAt,
			Payload: map[string]any{
				"machine_id": cfg.Relay.MachineID,
				"action":     action,
				"success":    true,
				"file":       file,
			},
		}
	default:
		return fail(errors.New("不支持的项目文件操作"))
	}
}
