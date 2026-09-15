package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"launcher/internal/config"
	"launcher/internal/model"
)

func TestBuildHandlersIncludesKnownEnvelopeTypes(t *testing.T) {
	handlers := buildHandlers()
	for _, key := range []string{
		"device.launcher.get",
		"device.launcher.upgrade",
		"device.launcher.rollback",
		"device.launcher.create_agent",
		"device.launcher.rename_agent",
		"device.launcher.restart_agent",
		"device.launcher.project_identity_correct",
		"device.launcher.remove_agent",
		"device.launcher.autostart_status",
		"device.launcher.autostart_enable",
		"device.launcher.autostart_disable",
		"device.launcher.self_update",
		"device.ai_config.get",
		"device.ai_config.list_models",
		"device.ai_config.save",
		"device.ai_config.save_text",
		"device.ai_config.clear_provider",
		"device.mcp_config.get",
		"device.mcp_config.agent_get",
		"device.mcp_config.agent_save",
		"device.launcher.skills_import",
		"device.mcp_config.save",
		"device.mcp_config.remove",
		"device.env_config.get",
		"device.env_config.save",
		"device.directories.get",
		"device.directory_files.upload_create",
		"device.directory_files.upload_chunk",
		"device.directory_files.upload_complete",
		"device.directory_files.create_file",
		"device.directory_files.mkdir",
		"device.directory_files.delete",
		"device.launcher.directory_permission",
		"device.launcher.diagnostics",
		"device.launcher.ssh_authorized_key",
		"device.project_files.list",
		"device.project_files.download",
		"device.project_files.upload",
		"device.project_files.upload_create",
		"device.project_files.upload_chunk",
		"device.project_files.upload_complete",
		"device.project_files.create_file",
		"device.project_files.mkdir",
		"device.project_files.delete",
		"device.project_files.rename",
	} {
		if _, ok := handlers[key]; !ok {
			t.Fatalf("expected handler for %s", key)
		}
	}
}

func TestLauncherManagedAuthorizedKeyEnvelopeIsHandled(t *testing.T) {
	expiresAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	svc := &relaySSHAuthorizedKeyService{}
	result := handleLauncher(envelope{
		Type: "device.launcher.ssh_authorized_key", RequestID: "req-ssh-key",
		Payload: map[string]any{"public_key": "ssh-ed25519 TEST", "expires_at": expiresAt},
	}, config.Config{Relay: config.RelayConfig{MachineID: "m_test"}}, svc)
	if svc.input.PublicKey != "ssh-ed25519 TEST" || !svc.input.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("managed key payload was decoded incorrectly: %+v", svc.input)
	}
	data, err := json.Marshal(result.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		MachineID     string                              `json:"machine_id"`
		Action        string                              `json:"action"`
		Success       bool                                `json:"success"`
		AuthorizedKey model.SSHManagedAuthorizedKeyResult `json:"authorized_key"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if result.Type != "device.launcher.result" || result.RequestID != "req-ssh-key" || payload.MachineID != "m_test" || payload.Action != "ssh_authorized_key" || !payload.Success || !payload.AuthorizedKey.Installed {
		t.Fatalf("unexpected managed key response: envelope=%+v payload=%+v", result, payload)
	}
}

func TestMCPConfigAgentSelectionEnvelopeIsHandled(t *testing.T) {
	handlers := buildHandlers()
	handler, ok := handlers["device.mcp_config.agent_get"]
	if !ok {
		t.Fatal("expected handler for device.mcp_config.agent_get")
	}

	result := handler(envelope{
		Type:      "device.mcp_config.agent_get",
		RequestID: "req-agent-mcp-get",
		Payload: model.DeviceAgentMCPSelectionInput{
			AgentID: "agent_reverse",
		},
	}, config.Config{
		Relay: config.RelayConfig{MachineID: "m_test"},
	}, relayMCPSelectionService{})

	if result.Type != "device.mcp_config.result" {
		t.Fatalf("expected mcp config result, got %s", result.Type)
	}
	if result.RequestID != "req-agent-mcp-get" {
		t.Fatalf("expected request id to be preserved, got %s", result.RequestID)
	}
	payload := decodeMCPConfigResultPayload(t, result.Payload)
	if !payload.Success {
		t.Fatalf("expected success payload, got error=%q", payload.Error)
	}
	if payload.MachineID != "m_test" {
		t.Fatalf("expected machine id m_test, got %s", payload.MachineID)
	}
	if payload.Action != "agent_get" {
		t.Fatalf("expected action agent_get, got %s", payload.Action)
	}
	if payload.Selection == nil {
		t.Fatal("expected agent mcp selection")
	}
	if payload.Selection.AgentID != "agent_reverse" {
		t.Fatalf("expected agent_reverse selection, got %s", payload.Selection.AgentID)
	}
	if payload.Selection.Mode != "custom" {
		t.Fatalf("expected custom mode, got %s", payload.Selection.Mode)
	}
	if len(payload.Selection.SelectedServers) != 1 || payload.Selection.SelectedServers[0] != "idalib-mcp" {
		t.Fatalf("unexpected selected servers: %+v", payload.Selection.SelectedServers)
	}
}

func TestMCPConfigAgentSaveEnvelopeIsHandled(t *testing.T) {
	handlers := buildHandlers()
	handler, ok := handlers["device.mcp_config.agent_save"]
	if !ok {
		t.Fatal("expected handler for device.mcp_config.agent_save")
	}

	result := handler(envelope{
		Type:      "device.mcp_config.agent_save",
		RequestID: "req-agent-mcp-save",
		Payload: model.DeviceAgentMCPSelectionInput{
			AgentID: "agent_reverse",
			Mode:    "custom",
			Servers: []string{"idalib-mcp", "frida-mcp"},
		},
	}, config.Config{
		Relay: config.RelayConfig{MachineID: "m_test"},
	}, relayMCPSelectionService{})

	if result.Type != "device.mcp_config.result" {
		t.Fatalf("expected mcp config result, got %s", result.Type)
	}
	payload := decodeMCPConfigResultPayload(t, result.Payload)
	if !payload.Success {
		t.Fatalf("expected success payload, got error=%q", payload.Error)
	}
	if payload.Action != "agent_save" {
		t.Fatalf("expected action agent_save, got %s", payload.Action)
	}
	if payload.Selection == nil {
		t.Fatal("expected saved agent mcp selection")
	}
	if payload.Selection.Mode != "custom" {
		t.Fatalf("expected custom mode, got %s", payload.Selection.Mode)
	}
	if len(payload.Selection.SelectedServers) != 2 || payload.Selection.SelectedServers[1] != "frida-mcp" {
		t.Fatalf("unexpected selected servers: %+v", payload.Selection.SelectedServers)
	}
}

func TestAgentSemanticPreflightStartReportsStatus(t *testing.T) {
	statusCh := make(chan model.RuntimePreflightStatusPayload, 2)
	svc := relayPreflightService{
		onStart: func(payload model.RuntimePreflightStartPayload, report func(model.RuntimePreflightStatusPayload) error) error {
			return report(model.RuntimePreflightStatusPayload{
				JobID:           payload.JobID,
				LauncherAgentID: payload.LauncherAgentID,
				SemanticAgentID: payload.SemanticAgentID,
				Status:          "completed",
				CurrentStep:     "completed",
				ProgressPercent: 100,
			})
		},
	}
	handleAgentSemanticPreflight(context.Background(), envelope{
		Type:      "device.launcher.agent_preflight.start",
		RequestID: "job_preflight_1",
		Payload: model.RuntimePreflightStartPayload{
			JobID:           "job_preflight_1",
			LauncherAgentID: "agent_reverse",
			SemanticAgentID: "reverse-expert",
		},
	}, config.Config{
		Relay: config.RelayConfig{MachineID: "m_test"},
	}, svc, func(ctx context.Context, env envelope) error {
		data, err := json.Marshal(env.Payload)
		if err != nil {
			return err
		}
		var status model.RuntimePreflightStatusPayload
		if err := json.Unmarshal(data, &status); err != nil {
			return err
		}
		statusCh <- status
		return nil
	})
	first := <-statusCh
	if first.Status != "running" || first.JobID != "job_preflight_1" {
		t.Fatalf("expected accepted running status, got %+v", first)
	}
	select {
	case final := <-statusCh:
		if final.Status != "completed" || final.ProgressPercent != 100 {
			t.Fatalf("expected completed status, got %+v", final)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected async preflight status")
	}
}

func TestDecodeSaveAIConfigInputUsesNestedConfig(t *testing.T) {
	input, err := decodeSaveAIConfigInput(map[string]any{
		"provider": "outer-provider",
		"base_url": "https://outer.example.com/v1",
		"api_key":  "outer-key",
		"config": map[string]any{
			"provider": "inner-provider",
			"base_url": "https://inner.example.com/v1",
			"api_key":  "inner-key",
			"api_mode": "responses",
			"model":    "gpt-4.1",
			"models": []map[string]any{
				{"id": "gpt-4.1", "name": "GPT-4.1"},
			},
		},
	})
	if err != nil {
		t.Fatalf("decode save payload: %v", err)
	}
	if input.Provider != "inner-provider" {
		t.Fatalf("expected nested provider, got %s", input.Provider)
	}
	if input.BaseURL != "https://inner.example.com/v1" {
		t.Fatalf("expected nested baseURL, got %s", input.BaseURL)
	}
	if input.APIKeyMasked != "inner-key" {
		t.Fatalf("expected nested api key, got %s", input.APIKeyMasked)
	}
	if input.APIMode != "responses" {
		t.Fatalf("expected nested api mode, got %s", input.APIMode)
	}
	if input.Model != "gpt-4.1" {
		t.Fatalf("expected nested model, got %s", input.Model)
	}
	if len(input.Models) != 1 || input.Models[0].ID != "gpt-4.1" {
		t.Fatalf("expected nested models to be used, got %+v", input.Models)
	}
}

func TestDecodeSaveAIConfigInputFallsBackToFlatFields(t *testing.T) {
	input, err := decodeSaveAIConfigInput(map[string]any{
		"provider": "flat-provider",
		"base_url": "https://flat.example.com/v1",
		"api_key":  "flat-key",
		"api_mode": "responses",
		"model":    "gpt-4.1-mini",
		"models": []map[string]any{
			{"id": "gpt-4.1-mini", "name": "GPT-4.1 Mini"},
		},
	})
	if err != nil {
		t.Fatalf("decode flat save payload: %v", err)
	}
	if input.Provider != "flat-provider" {
		t.Fatalf("expected flat provider, got %s", input.Provider)
	}
	if input.BaseURL != "https://flat.example.com/v1" {
		t.Fatalf("expected flat baseURL, got %s", input.BaseURL)
	}
	if input.APIKeyMasked != "flat-key" {
		t.Fatalf("expected flat api key, got %s", input.APIKeyMasked)
	}
	if input.APIMode != "responses" {
		t.Fatalf("expected flat api mode, got %s", input.APIMode)
	}
	if input.Model != "gpt-4.1-mini" {
		t.Fatalf("expected flat model, got %s", input.Model)
	}
	if len(input.Models) != 1 || input.Models[0].ID != "gpt-4.1-mini" {
		t.Fatalf("expected flat models to be used, got %+v", input.Models)
	}
}

func TestConnectSendsHeartbeatAfterWelcome(t *testing.T) {
	received := make(chan envelope, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.CloseNow()

		ctx := r.Context()
		if _, data, err := conn.Read(ctx); err != nil {
			t.Errorf("read hello: %v", err)
			return
		} else {
			var hello envelope
			if err := json.Unmarshal(data, &hello); err != nil {
				t.Errorf("decode hello: %v", err)
				return
			}
			if hello.Type != "device.hello" {
				t.Errorf("expected hello envelope, got %s", hello.Type)
				return
			}
		}

		if err := conn.Write(ctx, websocket.MessageText, mustJSON(t, envelope{
			Type:      "device.welcome",
			RequestID: "welcome",
			SentAt:    time.Now().UTC().Format(time.RFC3339),
			Payload: model.WelcomePayload{
				AgentID:              "launcher:m_test",
				HeartbeatIntervalSec: 1,
			},
		})); err != nil {
			t.Errorf("write welcome: %v", err)
			return
		}

		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("read heartbeat: %v", err)
			return
		}
		var heartbeat envelope
		if err := json.Unmarshal(data, &heartbeat); err != nil {
			t.Errorf("decode heartbeat: %v", err)
			return
		}
		received <- heartbeat
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- connect(ctx, config.Config{
			Relay: config.RelayConfig{
				URL:         strings.Replace(server.URL, "http://", "ws://", 1),
				OperatorKey: "opk_test",
				MachineID:   "m_test",
				Hostname:    "tester",
			},
		}, relayTestService{})
	}()

	select {
	case msg := <-received:
		if msg.Type != "device.heartbeat" {
			t.Fatalf("expected heartbeat envelope, got %s", msg.Type)
		}
		body, err := json.Marshal(msg.Payload)
		if err != nil {
			t.Fatalf("marshal heartbeat payload: %v", err)
		}
		var payload model.HeartbeatPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode heartbeat payload: %v", err)
		}
		if payload.AgentID != "launcher:m_test" {
			t.Fatalf("expected heartbeat agent id launcher:m_test, got %s", payload.AgentID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for heartbeat")
	}

	cancel()
	if err := <-errCh; !expectedConnectEnd(err) {
		t.Fatalf("connect returned unexpected error: %v", err)
	}
}

func TestConnectHandlesLargeProjectFileUploadEnvelope(t *testing.T) {
	const contentSize = 40 << 10
	received := make(chan envelope, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(relayWebSocketReadLimit)

		ctx := r.Context()
		if _, _, err := conn.Read(ctx); err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		if err := conn.Write(ctx, websocket.MessageText, mustJSON(t, envelope{
			Type:      "device.welcome",
			RequestID: "welcome",
			SentAt:    time.Now().UTC().Format(time.RFC3339),
			Payload: model.WelcomePayload{
				AgentID:              "launcher:m_test",
				HeartbeatIntervalSec: 60,
			},
		})); err != nil {
			t.Errorf("write welcome: %v", err)
			return
		}
		if err := conn.Write(ctx, websocket.MessageText, mustJSON(t, envelope{
			Type:      "device.project_files.upload",
			RequestID: "upload-large",
			SentAt:    time.Now().UTC().Format(time.RFC3339),
			Payload: model.ProjectFilesRequest{
				AgentID:  "agent",
				Path:     "large.txt",
				Content:  strings.Repeat("a", contentSize),
				Encoding: "base64",
			},
		})); err != nil {
			t.Errorf("write upload request: %v", err)
			return
		}

		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("read upload response: %v", err)
			return
		}
		var msg envelope
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Errorf("decode upload response: %v", err)
			return
		}
		received <- msg
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- connect(ctx, config.Config{
			Relay: config.RelayConfig{
				URL:         strings.Replace(server.URL, "http://", "ws://", 1),
				OperatorKey: "opk_test",
				MachineID:   "m_test",
				Hostname:    "tester",
			},
		}, relayLargeUploadService{})
	}()

	select {
	case msg := <-received:
		if msg.Type != "device.directories.result" {
			t.Fatalf("expected directories result, got %s", msg.Type)
		}
		if msg.RequestID != "upload-large" {
			t.Fatalf("expected request id upload-large, got %s", msg.RequestID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for large upload response")
	}

	cancel()
	if err := <-errCh; !expectedConnectEnd(err) {
		t.Fatalf("connect returned unexpected error: %v", err)
	}
}

func expectedConnectEnd(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
		return true
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return data
}

type mcpConfigResultPayloadForTest struct {
	MachineID string                             `json:"machine_id,omitempty"`
	Action    string                             `json:"action"`
	Config    *model.DeviceMCPConfigInfo         `json:"config,omitempty"`
	Selection *model.DeviceAgentMCPSelectionInfo `json:"selection,omitempty"`
	Success   bool                               `json:"success"`
	Error     string                             `json:"error,omitempty"`
}

func decodeMCPConfigResultPayload(t *testing.T, payload any) mcpConfigResultPayloadForTest {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal mcp result payload: %v", err)
	}
	var result mcpConfigResultPayloadForTest
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode mcp result payload: %v", err)
	}
	return result
}

type relayTestService struct{}

type relaySSHAuthorizedKeyService struct {
	relayTestService
	input model.SSHManagedAuthorizedKeyInput
}

func (s *relaySSHAuthorizedKeyService) SSHInstallManagedAuthorizedKey(input model.SSHManagedAuthorizedKeyInput) model.SSHManagedAuthorizedKeyResult {
	s.input = input
	return model.SSHManagedAuthorizedKeyResult{Installed: true, Fingerprint: "SHA256:test", ExpiresAt: input.ExpiresAt}
}

func (relayTestService) State() model.DeviceView { return model.DeviceView{} }

func (relayTestService) Directories(model.DirectoryRequest) (model.DirectoryListResult, error) {
	return model.DirectoryListResult{}, nil
}

func (relayTestService) CreateDirectoryUpload(model.DirectoryFileRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) WriteDirectoryUploadChunk(model.DirectoryFileRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) CompleteDirectoryUpload(model.DirectoryFileRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) DirectoryUploadStatus(model.DirectoryFileRequest) (model.UploadStatus, error) {
	return model.UploadStatus{}, nil
}

func (relayTestService) CreateDirectoryFile(model.DirectoryFileRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) CreateDirectoryFolder(model.DirectoryFileRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) DeleteDirectoryFile(model.DirectoryFileRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) DirectoryPermission(input model.DirectoryPermissionInput) (model.DirectoryPermissionResult, error) {
	return model.DirectoryPermissionResult{
		Platform:   "test",
		Path:       input.Path,
		Accessible: true,
	}, nil
}

func (relayTestService) Diagnostics(model.LauncherDiagnosticsInput) (model.LauncherDiagnostics, error) {
	return model.LauncherDiagnostics{Platform: "test"}, nil
}

func (relayTestService) ProjectFiles(model.ProjectFilesRequest) (model.DirectoryListResult, error) {
	return model.DirectoryListResult{}, nil
}

func (relayTestService) DownloadProjectFile(model.ProjectFilesRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) CreateProjectDownload(model.ProjectFilesRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) ReadProjectDownloadChunk(model.ProjectFilesRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) UploadProjectFile(model.ProjectFilesRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) CreateProjectUpload(model.ProjectFilesRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) WriteProjectUploadChunk(model.ProjectFilesRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) CompleteProjectUpload(model.ProjectFilesRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) ProjectUploadStatus(model.ProjectFilesRequest) (model.UploadStatus, error) {
	return model.UploadStatus{}, nil
}

func (relayTestService) CreateProjectFile(model.ProjectFilesRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) CreateProjectFolder(model.ProjectFilesRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) DeleteProjectFile(model.ProjectFilesRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) RenameProjectFile(model.ProjectFilesRequest) (model.ProjectFile, error) {
	return model.ProjectFile{}, nil
}

func (relayTestService) CreateAgent(model.CreateAgentInput) (model.Agent, error) {
	return model.Agent{}, nil
}

func (relayTestService) RenameAgent(model.RenameAgentInput) (model.Agent, error) {
	return model.Agent{}, nil
}

func (relayTestService) RestartAgent(string) (model.Agent, error) {
	return model.Agent{}, nil
}
func (relayTestService) CorrectProjectIdentity(input model.ProjectIdentityCorrectionInput) (model.ProjectIdentityCorrectionResult, error) {
	return model.ProjectIdentityCorrectionResult{
		ProjectScopeID: input.KeepScopeID, InstanceNonce: "nonce", Root: "/project", MarkerWritable: true,
	}, nil
}

func (relayTestService) SetAgentEnabled(model.SetAgentEnabledInput) (model.Agent, error) {
	return model.Agent{}, nil
}

func (relayTestService) RemoveAgent(string) error { return nil }

func (relayTestService) AIConfig() (*model.DeviceAIConfigInfo, error) { return nil, nil }

func (relayTestService) ListAIModels(model.DeviceAIConfigInput) (*model.DeviceAIConfigInfo, error) {
	return nil, nil
}

func (relayTestService) SaveAIConfig(model.DeviceAIConfigInfo) (*model.DeviceAIConfigInfo, error) {
	return nil, nil
}

func (relayTestService) SaveAIConfigText(string) (*model.DeviceAIConfigInfo, error) {
	return nil, nil
}

func (relayTestService) ClearAIProvider(string) (*model.DeviceAIConfigInfo, error) {
	return nil, nil
}

func (relayTestService) MCPConfig() (*model.DeviceMCPConfigInfo, error) { return nil, nil }

func (relayTestService) MCPStatus(string) (*model.MCPStatusInfo, error) { return nil, nil }

func (relayTestService) AgentMCPSelection(string) (model.DeviceAgentMCPSelectionInfo, error) {
	return model.DeviceAgentMCPSelectionInfo{}, nil
}

func (relayTestService) SaveAgentMCPSelection(model.DeviceAgentMCPSelectionInput) (model.DeviceAgentMCPSelectionInfo, error) {
	return model.DeviceAgentMCPSelectionInfo{}, nil
}

func (relayTestService) AgentSkillSelection(model.DeviceAgentSkillSelectionInput) (model.DeviceAgentSkillSelectionInfo, error) {
	return model.DeviceAgentSkillSelectionInfo{}, nil
}

func (relayTestService) SaveAgentSkillSelection(model.DeviceAgentSkillSelectionInput) (model.DeviceAgentSkillSelectionInfo, error) {
	return model.DeviceAgentSkillSelectionInfo{}, nil
}

func (relayTestService) ImportAgentSkill(model.DeviceAgentSkillImportInput) (model.DeviceAgentSkillSelectionInfo, error) {
	return model.DeviceAgentSkillSelectionInfo{}, nil
}
func (relayTestService) GlobalSkillConfig() (*model.DeviceSkillConfigInfo, error) {
	return nil, nil
}
func (relayTestService) SaveGlobalSkillConfig(model.DeviceSkillConfigInfo) (*model.DeviceSkillConfigInfo, error) {
	return nil, nil
}
func (relayTestService) ImportGlobalSkill(model.DeviceSkillConfigInput) (*model.DeviceSkillConfigInfo, error) {
	return nil, nil
}

func (relayTestService) AgentSemanticSelection(string) (model.DeviceAgentSemanticSelectionInfo, error) {
	return model.DeviceAgentSemanticSelectionInfo{}, nil
}

func (relayTestService) SaveAgentSemanticSelection(model.DeviceAgentSemanticSelectionInput) (model.DeviceAgentSemanticSelectionInfo, error) {
	return model.DeviceAgentSemanticSelectionInfo{}, nil
}

func (relayTestService) StartAgentSemanticPreflight(model.RuntimePreflightStartPayload, func(model.RuntimePreflightStatusPayload) error) error {
	return nil
}

func (relayTestService) SaveMCPConfig(model.DeviceMCPConfigInfo) (*model.DeviceMCPConfigInfo, error) {
	return nil, nil
}

func (relayTestService) RemoveMCPConfig(string) (*model.DeviceMCPConfigInfo, error) {
	return nil, nil
}

func (relayTestService) EnvConfig() (*model.DeviceEnvConfigInfo, error) {
	return nil, nil
}

func (relayTestService) SaveEnvConfig(model.DeviceEnvConfigInput) (*model.DeviceEnvConfigInfo, error) {
	return nil, nil
}

func (relayTestService) StartUpgrade(model.UpgradeInput) error { return nil }

func (relayTestService) Upgrade(model.UpgradeInput) error { return nil }

func (relayTestService) Rollback() error { return nil }

func (relayTestService) AutostartStatus() model.LauncherAutostartInfo {
	return model.LauncherAutostartInfo{}
}

func (relayTestService) EnableAutostart(model.LauncherAutostartInput) (model.LauncherAutostartInfo, error) {
	return model.LauncherAutostartInfo{}, nil
}

func (relayTestService) DisableAutostart() (model.LauncherAutostartInfo, error) {
	return model.LauncherAutostartInfo{}, nil
}

func (relayTestService) SelfUpdate(model.LauncherSelfUpdateInput) (model.LauncherSelfUpdateResult, error) {
	return model.LauncherSelfUpdateResult{}, nil
}

type relayMCPSelectionService struct {
	relayTestService
}

func (relayMCPSelectionService) AgentMCPSelection(agentID string) (model.DeviceAgentMCPSelectionInfo, error) {
	return model.DeviceAgentMCPSelectionInfo{
		AgentID:         agentID,
		Mode:            "custom",
		SelectedServers: []string{"idalib-mcp"},
	}, nil
}

func (relayMCPSelectionService) SaveAgentMCPSelection(input model.DeviceAgentMCPSelectionInput) (model.DeviceAgentMCPSelectionInfo, error) {
	return model.DeviceAgentMCPSelectionInfo{
		AgentID:         input.AgentID,
		Mode:            input.Mode,
		SelectedServers: input.Servers,
	}, nil
}

type relayLargeUploadService struct {
	relayTestService
}

type relayPreflightService struct {
	relayTestService
	onStart func(model.RuntimePreflightStartPayload, func(model.RuntimePreflightStatusPayload) error) error
}

func (s relayPreflightService) StartAgentSemanticPreflight(payload model.RuntimePreflightStartPayload, report func(model.RuntimePreflightStatusPayload) error) error {
	if s.onStart == nil {
		return nil
	}
	return s.onStart(payload, report)
}

func (relayLargeUploadService) UploadProjectFile(req model.ProjectFilesRequest) (model.ProjectFile, error) {
	if len(req.Content) < 40<<10 {
		return model.ProjectFile{}, fmt.Errorf("content too small: %d", len(req.Content))
	}
	return model.ProjectFile{
		Path:     req.Path,
		Name:     "large.txt",
		Kind:     "文件",
		IsDir:    false,
		Size:     int64(len(req.Content)),
		Encoding: req.Encoding,
	}, nil
}
