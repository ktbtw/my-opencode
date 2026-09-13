package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/model"
	"relay-server/internal/projectmemory"
	"relay-server/internal/store"
)

type projectMemoryPatchRequest struct {
	Version    int64    `json:"version,omitempty"`
	Statement  *string  `json:"statement,omitempty"`
	Kind       *string  `json:"kind,omitempty"`
	Confidence *float64 `json:"confidence,omitempty"`
	Locked     *bool    `json:"locked,omitempty"`
	Status     *string  `json:"status,omitempty"`
}

type projectMemoryJobRequest struct {
	Trigger     string `json:"trigger,omitempty"`
	CursorStart string `json:"cursor_start,omitempty"`
	CursorEnd   string `json:"cursor_end,omitempty"`
}

type projectMemoryDeleteRequest struct {
	Version int64 `json:"version,omitempty"`
}

type projectIdentityCorrectionRequest struct {
	Action string `json:"action"`
}

type projectMemorySettingsResponse struct {
	Global struct {
		Enabled    bool   `json:"enabled"`
		EnabledSet bool   `json:"enabled_set"`
		Model      string `json:"model,omitempty"`
		Variant    string `json:"variant,omitempty"`
	} `json:"global"`
	Project struct {
		Enabled *bool  `json:"enabled,omitempty"`
		Model   string `json:"model,omitempty"`
		Variant string `json:"variant,omitempty"`
	} `json:"project"`
	Effective struct {
		Enabled       bool   `json:"enabled"`
		Model         string `json:"model,omitempty"`
		Variant       string `json:"variant,omitempty"`
		EnabledSource string `json:"enabled_source"`
		ModelSource   string `json:"model_source"`
	} `json:"effective"`
}

type deviceProjectMemorySettingsResponse struct {
	Model   string `json:"model,omitempty"`
	Variant string `json:"variant,omitempty"`
}

func (a *API) deviceBelongsToOperator(r *http.Request, operator model.Operator, machineID string) bool {
	if _, found := a.broker.GetMachine(operator.ID, machineID); found {
		return true
	}
	scopes, err := a.store.ListProjectScopes(r.Context(), operator.ID, machineID)
	return err == nil && len(scopes) > 0
}

func (a *API) GetDeviceProjectMemorySettings(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := strings.TrimSpace(chi.URLParam(r, "machineID"))
	if machineID == "" || !a.deviceBelongsToOperator(r, operator, machineID) {
		write(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	settings, err := a.store.GetDeviceSettings(operator.ID, projectmemory.DeviceSettingsAgentID(machineID))
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, deviceProjectMemorySettingsResponse{
		Model:   strings.TrimSpace(settings[projectmemory.ModelSettingKey]),
		Variant: strings.TrimSpace(settings[projectmemory.VariantSettingKey]),
	})
}

func (a *API) UpdateDeviceProjectMemorySettings(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := strings.TrimSpace(chi.URLParam(r, "machineID"))
	if machineID == "" || !a.deviceBelongsToOperator(r, operator, machineID) {
		write(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	agentID := projectmemory.DeviceSettingsAgentID(machineID)
	for _, key := range []string{projectmemory.ModelSettingKey, projectmemory.VariantSettingKey} {
		raw, exists := body[key]
		if !exists {
			continue
		}
		value := ""
		if string(raw) != "null" {
			if err := json.Unmarshal(raw, &value); err != nil {
				write(w, http.StatusBadRequest, map[string]string{"error": "invalid project memory setting"})
				return
			}
			value = strings.TrimSpace(value)
		}
		if err := a.store.SetDeviceSetting(operator.ID, agentID, key, value); err != nil {
			write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	settings, err := a.store.GetDeviceSettings(operator.ID, agentID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, deviceProjectMemorySettingsResponse{
		Model:   strings.TrimSpace(settings[projectmemory.ModelSettingKey]),
		Variant: strings.TrimSpace(settings[projectmemory.VariantSettingKey]),
	})
}

func projectMemorySettingsResponseFor(settings projectmemory.Settings) projectMemorySettingsResponse {
	var response projectMemorySettingsResponse
	response.Global.Enabled = settings.GlobalEnabled
	response.Global.EnabledSet = settings.GlobalEnabledSet
	response.Global.Model = settings.GlobalModel
	response.Global.Variant = settings.GlobalVariant
	response.Project.Enabled = settings.ProjectEnabled
	response.Project.Model = settings.ProjectModel
	response.Project.Variant = settings.ProjectVariant
	response.Effective.Enabled = settings.Enabled
	response.Effective.Model = settings.Model
	response.Effective.Variant = settings.Variant
	response.Effective.EnabledSource = settings.EnabledSource
	response.Effective.ModelSource = settings.ModelSource
	return response
}

func (a *API) GetProjectMemorySettings(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	scope, ok := a.projectScopeFromRequest(r, operator)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "project scope not found"})
		return
	}
	settings := projectmemory.Resolve(r.Context(), a.store, operator.ID, scope.MachineID, scope.ID)
	write(w, http.StatusOK, projectMemorySettingsResponseFor(settings))
}

func (a *API) UpdateProjectMemorySettings(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	scope, ok := a.projectScopeFromRequest(r, operator)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "project scope not found"})
		return
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	agentID := projectmemory.ProjectSettingsAgentID(scope.ID)
	if raw, exists := body[projectmemory.EnabledSettingKey]; exists {
		value := ""
		if string(raw) != "null" {
			var enabled bool
			if err := json.Unmarshal(raw, &enabled); err != nil {
				write(w, http.StatusBadRequest, map[string]string{"error": "invalid project memory enabled value"})
				return
			}
			value = strconv.FormatBool(enabled)
		}
		if err := a.store.SetDeviceSetting(operator.ID, agentID, projectmemory.EnabledSettingKey, value); err != nil {
			write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	for _, key := range []string{projectmemory.ModelSettingKey, projectmemory.VariantSettingKey} {
		raw, exists := body[key]
		if !exists {
			continue
		}
		value := ""
		if string(raw) != "null" {
			if err := json.Unmarshal(raw, &value); err != nil {
				write(w, http.StatusBadRequest, map[string]string{"error": "invalid project memory model value"})
				return
			}
			value = strings.TrimSpace(value)
		}
		if err := a.store.SetDeviceSetting(operator.ID, agentID, key, value); err != nil {
			write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	settings := projectmemory.Resolve(r.Context(), a.store, operator.ID, scope.MachineID, scope.ID)
	write(w, http.StatusOK, projectMemorySettingsResponseFor(settings))
}

func (a *API) projectScopeFromRequest(r *http.Request, operator model.Operator) (*model.ProjectScope, bool) {
	machineID := strings.TrimSpace(chi.URLParam(r, "machineID"))
	scopeID := strings.TrimSpace(chi.URLParam(r, "scopeID"))
	if machineID == "" || scopeID == "" {
		return nil, false
	}
	scope, err := a.store.GetProjectScope(r.Context(), operator.ID, machineID, scopeID)
	return scope, err == nil && scope != nil && scope.OperatorID == operator.ID && scope.MachineID == machineID
}

func (a *API) ListProjectScopes(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := strings.TrimSpace(chi.URLParam(r, "machineID"))
	scopes, err := a.store.ListProjectScopes(r.Context(), operator.ID, machineID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "list project scopes failed"})
		return
	}
	write(w, http.StatusOK, scopes)
}

func (a *API) CorrectProjectIdentity(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	scope, ok := a.projectScopeFromRequest(r, operator)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "project scope not found"})
		return
	}
	var req projectIdentityCorrectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	if req.Action != "keep_memory_here" && req.Action != "treat_as_new" {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid identity action"})
		return
	}

	machine, found := a.broker.GetMachine(operator.ID, scope.MachineID)
	if !found || !machine.LauncherOnline {
		write(w, http.StatusConflict, map[string]string{"error": "project launcher is offline"})
		return
	}
	agentID := ""
	for _, agent := range machine.Agents {
		for _, project := range agent.Projects {
			if project.ScopeID == scope.ID {
				agentID = agent.ID
				break
			}
		}
		if agentID != "" {
			break
		}
	}
	if agentID == "" {
		write(w, http.StatusConflict, map[string]string{"error": "project agent is offline"})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, scope.MachineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "project launcher is offline"})
		return
	}
	keepScopeID := scope.ID
	if req.Action == "keep_memory_here" && scope.LineageScopeID != "" {
		keepScopeID = scope.LineageScopeID
	}
	requestID := fmt.Sprintf("req_project_identity_%d", time.Now().UnixNano())
	result, err := a.requestDeviceLauncher(r.Context(), operator.ID, launcherID, requestID,
		"device.launcher.project_identity_correct", model.ProjectIdentityCorrectionInput{
			AgentID: agentID, Action: req.Action, KeepScopeID: keepScopeID,
		})
	if err != nil || result.Identity == nil || strings.TrimSpace(result.Identity.ProjectScopeID) == "" {
		message := "project identity correction failed"
		if err != nil {
			message = err.Error()
		}
		write(w, http.StatusConflict, map[string]string{"error": message})
		return
	}
	identity := result.Identity
	if identity.ProjectScopeID != scope.ID {
		now := time.Now().UTC()
		corrected, lookupErr := a.store.GetProjectScope(r.Context(), operator.ID, scope.MachineID, identity.ProjectScopeID)
		if lookupErr != nil {
			write(w, http.StatusInternalServerError, map[string]string{"error": "project scope lookup failed"})
			return
		}
		if corrected == nil {
			corrected = &model.ProjectScope{
				ID: identity.ProjectScopeID, OperatorID: operator.ID, MachineID: scope.MachineID,
				DisplayName: scope.DisplayName, CurrentRoot: identity.Root,
				InstanceNonce: identity.InstanceNonce, LineageScopeID: identity.LineageProjectScopeID,
				BindingEpoch: 1, Status: model.ProjectScopeActive, Revision: 1,
				CreatedAt: now, UpdatedAt: now,
			}
		}
		if err := a.store.UpsertProjectScope(r.Context(), *corrected); err != nil {
			write(w, http.StatusInternalServerError, map[string]string{"error": "project scope update failed"})
			return
		}
	}
	_ = a.store.RecordProjectMemoryAudit(r.Context(), model.ProjectMemoryAudit{
		ScopeID: scope.ID, OperatorID: operator.ID, Action: "identity_" + req.Action,
		Metadata: map[string]any{"result_scope_id": identity.ProjectScopeID, "agent_id": agentID},
	})
	write(w, http.StatusOK, map[string]any{"scope": identity.ProjectScopeID, "identity": identity})
}

func (a *API) GetProjectMemoryOverview(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	scope, ok := a.projectScopeFromRequest(r, operator)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "project scope not found"})
		return
	}
	brief, err := a.store.GetProjectMemoryBrief(r.Context(), operator.ID, scope.ID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "get project brief failed"})
		return
	}
	memories, err := a.store.ListProjectMemories(r.Context(), model.ProjectMemoryFilter{
		OperatorID: operator.ID, MachineID: scope.MachineID, ScopeID: scope.ID, Limit: 20,
	})
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "get project memory failed"})
		return
	}
	write(w, http.StatusOK, map[string]any{"scope": scope, "brief": brief, "recent": memories})
}

func (a *API) ListProjectMemories(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	scope, ok := a.projectScopeFromRequest(r, operator)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "project scope not found"})
		return
	}
	rawKind := strings.TrimSpace(r.URL.Query().Get("kind"))
	filter := model.ProjectMemoryFilter{
		OperatorID:   operator.ID,
		MachineID:    scope.MachineID,
		ScopeID:      scope.ID,
		Status:       model.ProjectMemoryStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
		Verification: model.ProjectMemoryVerificationStatus(strings.TrimSpace(r.URL.Query().Get("verification"))),
		Query:        strings.TrimSpace(r.URL.Query().Get("query")),
		RelativePath: strings.TrimSpace(r.URL.Query().Get("path")),
		ArtifactHash: strings.TrimSpace(r.URL.Query().Get("artifact_hash")),
		BeforeID:     strings.TrimSpace(r.URL.Query().Get("before_id")),
		Limit:        100,
	}
	if rawKind == "fact" {
		filter.Kinds = []model.ProjectMemoryKind{model.ProjectMemoryVerifiedFact, model.ProjectMemoryInferredFact}
	} else if rawKind != "" {
		filter.Kind = model.ProjectMemoryKind(rawKind)
		if !validProjectMemoryAPIKind(filter.Kind) {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid memory kind"})
			return
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("before_updated_at")); raw != "" {
		if filter.BeforeID == "" {
			write(w, http.StatusBadRequest, map[string]string{"error": "before_id is required with before_updated_at"})
			return
		}
		value, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid before_updated_at"})
			return
		}
		filter.BeforeUpdatedAt = value
	}
	if filter.Status != "" {
		if !validProjectMemoryListStatus(filter.Status) {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid memory status"})
			return
		}
		filter.IncludeDeleted = filter.Status == model.ProjectMemoryDeleted
	}
	if filter.Verification != "" && !validProjectMemoryVerification(filter.Verification) {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid verification status"})
		return
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("locked")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid locked filter"})
			return
		}
		filter.Locked = &value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 || value > 200 {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		filter.Limit = value
	}
	memories, err := a.store.ListProjectMemories(r.Context(), filter)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "list project memories failed"})
		return
	}
	write(w, http.StatusOK, memories)
}

func (a *API) GetProjectMemory(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	scope, ok := a.projectScopeFromRequest(r, operator)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "project scope not found"})
		return
	}
	memory, err := a.store.GetProjectMemory(r.Context(), operator.ID, scope.ID, chi.URLParam(r, "memoryID"))
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "get project memory failed"})
		return
	}
	if memory == nil {
		write(w, http.StatusNotFound, map[string]string{"error": "project memory not found"})
		return
	}
	_ = a.store.RecordProjectMemoryAudit(r.Context(), model.ProjectMemoryAudit{
		ScopeID: scope.ID, MemoryID: memory.ID, OperatorID: operator.ID, Action: "view",
	})
	history, err := a.store.ListProjectMemoryVersions(r.Context(), operator.ID, scope.ID, memory.LogicalID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "get project memory history failed"})
		return
	}
	write(w, http.StatusOK, map[string]any{"memory": memory, "history": history})
}

func (a *API) UpdateProjectMemory(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	scope, ok := a.projectScopeFromRequest(r, operator)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "project scope not found"})
		return
	}
	memoryID := chi.URLParam(r, "memoryID")
	previous, err := a.store.GetProjectMemory(r.Context(), operator.ID, scope.ID, memoryID)
	if err != nil || previous == nil {
		write(w, http.StatusNotFound, map[string]string{"error": "project memory not found"})
		return
	}
	var req projectMemoryPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Version > 0 && req.Version != previous.Version {
		write(w, http.StatusConflict, map[string]string{"error": "project memory revision conflict"})
		return
	}
	next := *previous
	if req.Statement != nil {
		value := strings.TrimSpace(*req.Statement)
		if value == "" || len([]rune(value)) > 2000 {
			write(w, http.StatusBadRequest, map[string]string{"error": "statement must be 1-2000 characters"})
			return
		}
		next.Statement = value
	}
	if req.Kind != nil {
		next.Kind = model.ProjectMemoryKind(strings.TrimSpace(*req.Kind))
		if !validProjectMemoryAPIKind(next.Kind) {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid memory kind"})
			return
		}
	}
	if req.Confidence != nil {
		if *req.Confidence < 0 || *req.Confidence > 1 {
			write(w, http.StatusBadRequest, map[string]string{"error": "confidence must be between 0 and 1"})
			return
		}
		next.Confidence = *req.Confidence
	}
	if req.Locked != nil {
		next.Locked = *req.Locked
	}
	if req.Status != nil {
		next.Status = model.ProjectMemoryStatus(strings.TrimSpace(*req.Status))
		if !validProjectMemoryAPIStatus(next.Status) {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid memory status"})
			return
		}
	}
	if err := a.store.EditProjectMemoryVersion(r.Context(), operator.ID, scope.ID, memoryID, next); err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	_ = a.store.RecordProjectMemoryAudit(r.Context(), model.ProjectMemoryAudit{
		ScopeID: scope.ID, MemoryID: memoryID, OperatorID: operator.ID, Action: "edit",
	})
	write(w, http.StatusOK, map[string]any{"updated": true, "scope_revision": scope.Revision + 1})
}

func (a *API) DeleteProjectMemory(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	scope, ok := a.projectScopeFromRequest(r, operator)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "project scope not found"})
		return
	}
	memoryID := chi.URLParam(r, "memoryID")
	var req projectMemoryDeleteRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
	}
	if err := a.store.MarkProjectMemoryDeleted(r.Context(), operator.ID, scope.ID, memoryID, req.Version); err != nil {
		if errors.Is(err, store.ErrProjectMemoryRevisionConflict) {
			write(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		write(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	_ = a.store.RecordProjectMemoryAudit(r.Context(), model.ProjectMemoryAudit{
		ScopeID: scope.ID, MemoryID: memoryID, OperatorID: operator.ID, Action: "delete",
	})
	write(w, http.StatusOK, map[string]any{"deleted": true})
}

func (a *API) ResolveProjectMemory(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	scope, ok := a.projectScopeFromRequest(r, operator)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "project scope not found"})
		return
	}
	memoryID := chi.URLParam(r, "memoryID")
	previous, err := a.store.GetProjectMemory(r.Context(), operator.ID, scope.ID, memoryID)
	if err != nil || previous == nil {
		write(w, http.StatusNotFound, map[string]string{"error": "project memory not found"})
		return
	}
	var req struct {
		Version int64  `json:"version,omitempty"`
		Status  string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Version > 0 && req.Version != previous.Version {
		write(w, http.StatusConflict, map[string]string{"error": "project memory revision conflict"})
		return
	}
	req.Status = strings.TrimSpace(req.Status)
	if req.Status != string(model.ProjectMemoryActive) && req.Status != string(model.ProjectMemoryDisputed) && req.Status != string(model.ProjectMemoryStale) && req.Status != string(model.ProjectMemoryArchived) {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid memory status"})
		return
	}
	next := *previous
	next.Status = model.ProjectMemoryStatus(req.Status)
	if err := a.store.EditProjectMemoryVersion(r.Context(), operator.ID, scope.ID, memoryID, next); err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	action := "resolve"
	if previous.Status == model.ProjectMemoryDeleted && req.Status == string(model.ProjectMemoryActive) {
		action = "restore"
	}
	_ = a.store.RecordProjectMemoryAudit(r.Context(), model.ProjectMemoryAudit{
		ScopeID: scope.ID, MemoryID: memoryID, OperatorID: operator.ID, Action: action,
		Metadata: map[string]string{"status": req.Status},
	})
	write(w, http.StatusOK, map[string]any{"resolved": true})
}

func validProjectMemoryAPIKind(kind model.ProjectMemoryKind) bool {
	switch kind {
	case model.ProjectMemoryLockedRule, model.ProjectMemoryVerifiedFact, model.ProjectMemoryInferredFact,
		model.ProjectMemoryProcedure, model.ProjectMemoryDecision, model.ProjectMemoryIssue, model.ProjectMemoryEpisode:
		return true
	default:
		return false
	}
}

func validProjectMemoryAPIStatus(status model.ProjectMemoryStatus) bool {
	switch status {
	case model.ProjectMemoryActive, model.ProjectMemoryDisputed, model.ProjectMemoryStale,
		model.ProjectMemoryArchived, model.ProjectMemoryDetached, model.ProjectMemoryMissing:
		return true
	default:
		return false
	}
}

func validProjectMemoryListStatus(status model.ProjectMemoryStatus) bool {
	return validProjectMemoryAPIStatus(status) || status == model.ProjectMemorySuperseded || status == model.ProjectMemoryDeleted
}

func validProjectMemoryVerification(status model.ProjectMemoryVerificationStatus) bool {
	switch status {
	case model.ProjectMemoryVerified, model.ProjectMemoryUnverified, model.ProjectMemoryFailed, model.ProjectMemoryStaleCheck:
		return true
	default:
		return false
	}
}

func (a *API) CreateProjectMemoryJob(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	scope, ok := a.projectScopeFromRequest(r, operator)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "project scope not found"})
		return
	}
	if !a.projectMemoryEnabled || !projectmemory.Resolve(r.Context(), a.store, operator.ID, scope.MachineID, scope.ID).Enabled {
		write(w, http.StatusConflict, map[string]string{"error": "project memory is disabled"})
		return
	}
	var req projectMemoryJobRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
	}
	trigger := model.ProjectMemoryTrigger(strings.TrimSpace(req.Trigger))
	if trigger == "" {
		trigger = model.ProjectMemoryTriggerManual
	}
	if trigger != model.ProjectMemoryTriggerManual {
		write(w, http.StatusBadRequest, map[string]string{"error": "only manual jobs can be created by this endpoint"})
		return
	}
	jobs, err := a.store.ListProjectMemoryJobs(r.Context(), operator.ID, scope.ID, 20)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "list project memory jobs failed"})
		return
	}
	resumeCursor := ""
	for _, job := range jobs {
		if resumeCursor == "" && job.ManualVisible && job.Status == model.ProjectMemoryJobFailed {
			resumeCursor = job.CheckpointCursor
			if resumeCursor == "" {
				resumeCursor = job.CursorCommitted
			}
		}
	}
	if strings.TrimSpace(req.CursorStart) == "" {
		req.CursorStart = resumeCursor
	}
	job := model.ProjectMemoryJob{
		ID: fmt.Sprintf("pmjob_%d", time.Now().UnixNano()), OperatorID: operator.ID,
		MachineID: scope.MachineID, ScopeID: scope.ID, Trigger: trigger,
		Status: model.ProjectMemoryJobQueued, CursorStart: strings.TrimSpace(req.CursorStart),
		CursorEnd: strings.TrimSpace(req.CursorEnd), InputRevision: scope.Revision,
		ManualVisible: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	scheduled, created, err := a.store.ScheduleProjectMemoryJob(r.Context(), job)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "create project memory job failed"})
		return
	}
	_ = a.store.RecordProjectMemoryAudit(r.Context(), model.ProjectMemoryAudit{
		ScopeID: scope.ID, JobID: scheduled.ID, OperatorID: operator.ID, Action: "organize",
	})
	eventType := "manual_attached"
	message := "已关联正在执行的整理任务"
	if created {
		eventType = "queued"
		message = "项目记忆整理任务已进入队列"
	}
	_ = a.store.AppendProjectMemoryJobEvent(r.Context(), model.ProjectMemoryJobEvent{
		JobID: scheduled.ID, Type: eventType, Message: message, CreatedAt: time.Now().UTC(),
	})
	write(w, http.StatusAccepted, scheduled)
}

func (a *API) GetProjectMemoryJob(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	scope, ok := a.projectScopeFromRequest(r, operator)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "project scope not found"})
		return
	}
	job, err := a.store.GetProjectMemoryJob(r.Context(), operator.ID, chi.URLParam(r, "jobID"))
	if err != nil || job == nil || job.ScopeID != scope.ID {
		write(w, http.StatusNotFound, map[string]string{"error": "project memory job not found"})
		return
	}
	write(w, http.StatusOK, job)
}

func (a *API) ProjectMemoryJobEvents(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	scope, ok := a.projectScopeFromRequest(r, operator)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "project scope not found"})
		return
	}
	jobID := chi.URLParam(r, "jobID")
	job, err := a.store.GetProjectMemoryJob(r.Context(), operator.ID, jobID)
	if err != nil || job == nil || job.ScopeID != scope.ID || !job.ManualVisible {
		write(w, http.StatusNotFound, map[string]string{"error": "project memory job not found"})
		return
	}
	after := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		after, _ = strconv.ParseInt(raw, 10, 64)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, canFlush := w.(http.Flusher)
	send := func(events []model.ProjectMemoryJobEvent) {
		for _, event := range events {
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			after = event.Sequence
		}
		if canFlush {
			flusher.Flush()
		}
	}
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	for {
		events, err := a.store.ListProjectMemoryJobEvents(r.Context(), operator.ID, jobID, after, true)
		if err != nil {
			return
		}
		send(events)
		current, err := a.store.GetProjectMemoryJob(r.Context(), operator.ID, jobID)
		if err != nil || current == nil || current.ScopeID != scope.ID {
			return
		}
		if current.Status == model.ProjectMemoryJobCompleted || current.Status == model.ProjectMemoryJobFailed || current.Status == model.ProjectMemoryJobCancelled {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
