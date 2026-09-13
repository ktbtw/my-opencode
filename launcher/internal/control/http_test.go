package control

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"launcher/internal/model"
)

type testService struct {
	state              model.DeviceView
	input              model.UpgradeInput
	runtimeInfo        model.RuntimeEnvironmentInfo
	semantic           model.DeviceAgentSemanticSelectionInfo
	autostartRefreshes int
}

func (s *testService) State() model.DeviceView { return s.state }
func (s *testService) SSHStatus() model.SSHStatus {
	return model.SSHStatus{Platform: "test", Installed: true, Running: true, Listening: true, ListenAddress: "127.0.0.1:22"}
}
func (s *testService) SSHSetup() model.SSHSetupResult {
	return model.SSHSetupResult{Status: s.SSHStatus(), Action: "already_ready"}
}
func (s *testService) SSHInstallAuthorizedKey(publicKey string) model.SSHAuthorizedKeyResult {
	return model.SSHAuthorizedKeyResult{Installed: publicKey != "", Fingerprint: "SHA256:test"}
}
func (s *testService) AIConfig() (*model.DeviceAIConfigInfo, error) {
	return nil, nil
}
func (s *testService) ListAIModels(model.DeviceAIConfigInput) (*model.DeviceAIConfigInfo, error) {
	return nil, nil
}
func (s *testService) SaveAIConfig(model.DeviceAIConfigInfo) (*model.DeviceAIConfigInfo, error) {
	return nil, nil
}
func (s *testService) SaveAIConfigText(string) (*model.DeviceAIConfigInfo, error) {
	return nil, nil
}
func (s *testService) ClearAIProvider(string) (*model.DeviceAIConfigInfo, error) {
	return nil, nil
}
func (s *testService) MCPConfig() (*model.DeviceMCPConfigInfo, error) { return nil, nil }
func (s *testService) MCPStatus(string) (*model.MCPStatusInfo, error) { return nil, nil }
func (s *testService) AgentSemanticSelection(agentID string) (model.DeviceAgentSemanticSelectionInfo, error) {
	s.semantic.AgentID = agentID
	return s.semantic, nil
}
func (s *testService) SaveAgentSemanticSelection(input model.DeviceAgentSemanticSelectionInput) (model.DeviceAgentSemanticSelectionInfo, error) {
	s.semantic.AgentID = input.AgentID
	s.semantic.SemanticAgentID = input.SemanticAgentID
	return s.semantic, nil
}
func (s *testService) AgentSkillSelection(model.DeviceAgentSkillSelectionInput) (model.DeviceAgentSkillSelectionInfo, error) {
	return model.DeviceAgentSkillSelectionInfo{}, nil
}
func (s *testService) SaveAgentSkillSelection(model.DeviceAgentSkillSelectionInput) (model.DeviceAgentSkillSelectionInfo, error) {
	return model.DeviceAgentSkillSelectionInfo{}, nil
}
func (s *testService) ImportAgentSkill(model.DeviceAgentSkillImportInput) (model.DeviceAgentSkillSelectionInfo, error) {
	return model.DeviceAgentSkillSelectionInfo{}, nil
}
func (s *testService) GlobalSkillConfig() (*model.DeviceSkillConfigInfo, error) {
	return nil, nil
}
func (s *testService) SaveGlobalSkillConfig(model.DeviceSkillConfigInfo) (*model.DeviceSkillConfigInfo, error) {
	return nil, nil
}
func (s *testService) ImportGlobalSkill(model.DeviceSkillConfigInput) (*model.DeviceSkillConfigInfo, error) {
	return nil, nil
}
func (s *testService) SaveMCPConfig(model.DeviceMCPConfigInfo) (*model.DeviceMCPConfigInfo, error) {
	return nil, nil
}
func (s *testService) RemoveMCPConfig(string) (*model.DeviceMCPConfigInfo, error) {
	return nil, nil
}
func (s *testService) RuntimeEnvironment() (model.RuntimeEnvironmentInfo, error) {
	return s.runtimeInfo, nil
}
func (s *testService) PrepareRuntimeEnvironment() (model.RuntimeEnvironmentInfo, error) {
	s.runtimeInfo.Preparing = false
	return s.runtimeInfo, nil
}
func (s *testService) Directories(model.DirectoryRequest) (model.DirectoryListResult, error) {
	return model.DirectoryListResult{}, nil
}
func (s *testService) DirectoryPermission(input model.DirectoryPermissionInput) (model.DirectoryPermissionResult, error) {
	return model.DirectoryPermissionResult{
		Platform:   "test",
		Path:       input.Path,
		Accessible: true,
	}, nil
}
func (s *testService) Diagnostics(model.LauncherDiagnosticsInput) (model.LauncherDiagnostics, error) {
	return model.LauncherDiagnostics{Platform: "test"}, nil
}
func (s *testService) Agents() []model.Agent { return nil }
func (s *testService) CreateAgent(model.CreateAgentInput) (model.Agent, error) {
	return model.Agent{}, nil
}
func (s *testService) RestartAgent(string) (model.Agent, error) { return model.Agent{}, nil }
func (s *testService) SetAgentEnabled(model.SetAgentEnabledInput) (model.Agent, error) {
	return model.Agent{}, nil
}
func (s *testService) RemoveAgent(string) error { return nil }
func (s *testService) Versions() ([]string, error) {
	return []string{s.state.CurrentVersion}, nil
}
func (s *testService) StartUpgrade(input model.UpgradeInput) error {
	s.input = input
	s.state.Status = "upgrading"
	s.state.TargetVersion = input.TargetVersion
	s.state.UpgradeLocked = true
	s.state.UpgradeStage = "checking_version"
	s.state.UpgradeProgress = 3
	s.state.UpgradeMessage = "升级任务已启动"
	return nil
}
func (s *testService) Upgrade(model.UpgradeInput) error { return nil }
func (s *testService) Rollback() error                  { return nil }
func (s *testService) AutostartStatus() model.LauncherAutostartInfo {
	return model.LauncherAutostartInfo{Method: "cached"}
}
func (s *testService) RefreshAutostartStatus() model.LauncherAutostartInfo {
	s.autostartRefreshes++
	return model.LauncherAutostartInfo{Method: "refreshed"}
}
func (s *testService) EnableAutostart(model.LauncherAutostartInput) (model.LauncherAutostartInfo, error) {
	return model.LauncherAutostartInfo{}, nil
}
func (s *testService) DisableAutostart() (model.LauncherAutostartInfo, error) {
	return model.LauncherAutostartInfo{}, nil
}
func (s *testService) SelfUpdate(model.LauncherSelfUpdateInput) (model.LauncherSelfUpdateResult, error) {
	return model.LauncherSelfUpdateResult{}, nil
}

func TestAutostartStatusUsesCacheUnlessRefreshRequested(t *testing.T) {
	svc := &testService{}
	handler := NewHandler(svc)

	cachedReq := httptest.NewRequest(http.MethodGet, "/autostart/status", nil)
	cachedRec := httptest.NewRecorder()
	handler.ServeHTTP(cachedRec, cachedReq)
	if cachedRec.Code != http.StatusOK || !bytes.Contains(cachedRec.Body.Bytes(), []byte(`"method":"cached"`)) {
		t.Fatalf("expected cached autostart response, code=%d body=%s", cachedRec.Code, cachedRec.Body.String())
	}
	if svc.autostartRefreshes != 0 {
		t.Fatalf("polling should not refresh platform status, got %d refreshes", svc.autostartRefreshes)
	}

	refreshReq := httptest.NewRequest(http.MethodGet, "/autostart/status?refresh=1", nil)
	refreshRec := httptest.NewRecorder()
	handler.ServeHTTP(refreshRec, refreshReq)
	if refreshRec.Code != http.StatusOK || !bytes.Contains(refreshRec.Body.Bytes(), []byte(`"method":"refreshed"`)) {
		t.Fatalf("expected refreshed autostart response, code=%d body=%s", refreshRec.Code, refreshRec.Body.String())
	}
	if svc.autostartRefreshes != 1 {
		t.Fatalf("expected one explicit refresh, got %d", svc.autostartRefreshes)
	}
}

func TestSSHEndpoints(t *testing.T) {
	svc := &testService{}
	handler := NewHandler(svc)

	statusReq := httptest.NewRequest(http.MethodGet, "/ssh/status", nil)
	statusRec := httptest.NewRecorder()
	handler.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK || !bytes.Contains(statusRec.Body.Bytes(), []byte(`"listening":true`)) {
		t.Fatalf("unexpected SSH status response: code=%d body=%s", statusRec.Code, statusRec.Body.String())
	}

	setupReq := httptest.NewRequest(http.MethodPost, "/ssh/setup", nil)
	setupRec := httptest.NewRecorder()
	handler.ServeHTTP(setupRec, setupReq)
	if setupRec.Code != http.StatusOK || !bytes.Contains(setupRec.Body.Bytes(), []byte(`"action":"already_ready"`)) {
		t.Fatalf("unexpected SSH setup response: code=%d body=%s", setupRec.Code, setupRec.Body.String())
	}

	keyReq := httptest.NewRequest(http.MethodPost, "/ssh/authorized-key", bytes.NewBufferString(`{"public_key":"ssh-ed25519 TEST"}`))
	keyRec := httptest.NewRecorder()
	handler.ServeHTTP(keyRec, keyReq)
	if keyRec.Code != http.StatusOK || !bytes.Contains(keyRec.Body.Bytes(), []byte(`"fingerprint":"SHA256:test"`)) {
		t.Fatalf("unexpected SSH key response: code=%d body=%s", keyRec.Code, keyRec.Body.String())
	}
}

func TestUpgradeReturnsProgressState(t *testing.T) {
	svc := &testService{state: model.DeviceView{Status: "running", CurrentVersion: "1.0.0"}}
	body := bytes.NewBufferString(`{"target_version":"1.1.0"}`)
	req := httptest.NewRequest(http.MethodPost, "/upgrade", body)
	rec := httptest.NewRecorder()

	NewHandler(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Success bool             `json:"success"`
		State   model.DeviceView `json:"state"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Success {
		t.Fatalf("expected success")
	}
	if payload.State.Status != "upgrading" || !payload.State.UpgradeLocked {
		t.Fatalf("expected upgrading state, got %#v", payload.State)
	}
	if payload.State.UpgradeStage == "" || payload.State.UpgradeProgress == 0 {
		t.Fatalf("expected upgrade progress, got %#v", payload.State)
	}
	if svc.input.TargetVersion != "1.1.0" {
		t.Fatalf("expected StartUpgrade target, got %q", svc.input.TargetVersion)
	}
}

func TestRuntimeEnvironmentEndpoints(t *testing.T) {
	svc := &testService{
		runtimeInfo: model.RuntimeEnvironmentInfo{
			RuntimeDir: "/tmp/runtime",
			Tools: []model.RuntimeToolStatusInfo{
				{Name: "uv", Status: "available", Source: "managed"},
			},
		},
	}
	handler := NewHandler(svc)

	getReq := httptest.NewRequest(http.MethodGet, "/runtime-env", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected runtime-env 200, got %d body=%s", getRec.Code, getRec.Body.String())
	}
	var getPayload model.RuntimeEnvironmentInfo
	if err := json.Unmarshal(getRec.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("decode runtime-env response: %v", err)
	}
	if getPayload.RuntimeDir != "/tmp/runtime" || len(getPayload.Tools) != 1 {
		t.Fatalf("unexpected runtime info: %#v", getPayload)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/runtime-env/prepare", nil)
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("expected prepare 200, got %d body=%s", postRec.Code, postRec.Body.String())
	}
}

func TestAgentSemanticSelectionEndpoint(t *testing.T) {
	svc := &testService{
		semantic: model.DeviceAgentSemanticSelectionInfo{
			SemanticAgentID: "coding-assistant",
		},
	}
	handler := NewHandler(svc)

	getReq := httptest.NewRequest(http.MethodGet, "/agents/agent_test/semantic-agent", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d body=%s", getRec.Code, getRec.Body.String())
	}
	var getPayload model.DeviceAgentSemanticSelectionInfo
	if err := json.Unmarshal(getRec.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getPayload.AgentID != "agent_test" || getPayload.SemanticAgentID != "coding-assistant" {
		t.Fatalf("unexpected get payload: %#v", getPayload)
	}

	postReq := httptest.NewRequest(
		http.MethodPost,
		"/agents/agent_test/semantic-agent",
		bytes.NewBufferString(`{"semantic_agent_id":"reverse-expert","apply_recommended_mcp":true}`),
	)
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("expected post 200, got %d body=%s", postRec.Code, postRec.Body.String())
	}
	var postPayload model.DeviceAgentSemanticSelectionInfo
	if err := json.Unmarshal(postRec.Body.Bytes(), &postPayload); err != nil {
		t.Fatalf("decode post response: %v", err)
	}
	if postPayload.AgentID != "agent_test" || postPayload.SemanticAgentID != "reverse-expert" {
		t.Fatalf("unexpected post payload: %#v", postPayload)
	}
}
