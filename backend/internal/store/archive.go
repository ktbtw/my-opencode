package store

import (
	"time"

	"relay-server/internal/model"
)

type TaskArchive interface {
	UpsertTask(task *model.Task) error
	AppendEvent(event model.Event) error
	ListEvents(taskID string) ([]model.Event, error)
	LastEventAt(taskID string) (time.Time, bool, error)
	GetTask(taskID string) (*model.Task, error)
	ListTasks(filter model.TaskFilter) ([]*model.Task, error)
	UpsertSession(session *model.Session) error
	ListSessions(filter model.SessionFilter) ([]*model.Session, error)
	AuthenticateOperator(username, password string) (*model.Operator, error)
	GetOperatorByKey(operatorKey string) (*model.Operator, error)
	GetOperatorEmail(operatorID int64) (string, error)
	UsernameExists(username string) (bool, error)
	EmailExists(email string) (bool, error)
	CreateOperator(username, password, email, name string) (*model.Operator, error)
	ChangePassword(operatorID int64, newPassword string) error
	DeleteOperator(operatorID int64) error
	SetDeviceSetting(operatorID int64, agentID, key, value string) error
	GetDeviceSettings(operatorID int64, agentID string) (map[string]string, error)
	DeleteDeviceSettings(operatorID int64, agentID string) error
	ListDeviceSettingAgentIDs(operatorID int64, key string) ([]string, error)
	UpsertPushDevice(device model.PushDevice) (*model.PushDevice, error)
	ListPushDevices(operatorID int64) ([]*model.PushDevice, error)
	ListSkills(includeDisabled bool) ([]model.Skill, error)
	UpsertSkill(item model.Skill) (*model.Skill, error)
	DeleteSkill(id string) error
	ListMCPCatalog(includeDisabled bool) ([]model.MCPCatalogItem, error)
	UpsertMCPCatalogItem(item model.MCPCatalogItem) (*model.MCPCatalogItem, error)
	DeleteMCPCatalogItem(id string) error
	ListEnvironmentPresets(includeDisabled bool) ([]model.EnvironmentPreset, error)
	UpsertEnvironmentPreset(item model.EnvironmentPreset) (*model.EnvironmentPreset, error)
	DeleteEnvironmentPreset(id string) error
	ListRuntimeCatalog(includeDisabled bool) ([]model.RuntimeCatalogItem, error)
	UpsertRuntimeCatalogItem(item model.RuntimeCatalogItem) (*model.RuntimeCatalogItem, error)
	DeleteRuntimeCatalogItem(id string) error
	ListRuntimeVersions(includeDisabled bool) ([]model.RuntimeVersion, error)
	UpsertRuntimeVersion(item model.RuntimeVersion) (*model.RuntimeVersion, error)
	DeleteRuntimeVersion(id string) error
	ListRuntimeArtifacts(includeDisabled bool) ([]model.RuntimeArtifact, error)
	UpsertRuntimeArtifact(item model.RuntimeArtifact) (*model.RuntimeArtifact, error)
	DeleteRuntimeArtifact(id string) error
	ListRuntimeMirrors(includeDisabled bool) ([]model.RuntimeMirror, error)
	UpsertRuntimeMirror(item model.RuntimeMirror) (*model.RuntimeMirror, error)
	DeleteRuntimeMirror(id string) error
	ListToolCatalog(includeDisabled bool) ([]model.ToolCatalogItem, error)
	UpsertToolCatalogItem(item model.ToolCatalogItem) (*model.ToolCatalogItem, error)
	DeleteToolCatalogItem(id string) error
	ListSemanticAgents(includeDisabled bool) ([]model.SemanticAgentProfile, error)
	UpsertSemanticAgent(item model.SemanticAgentProfile) (*model.SemanticAgentProfile, error)
	DeleteSemanticAgent(id string) error
	Close() error
}

// ProjectMemoryEventReader lets background summarization read a bounded event
// window without populating the general-purpose task event cache.
type ProjectMemoryEventReader interface {
	ListProjectMemoryEvents(taskID string, limit int) ([]model.Event, error)
}

// TaskEventWindowReader reads bounded event windows without loading a full
// task history into the process.
type TaskEventWindowReader interface {
	ListEventsAfter(taskID string, afterSequence int64, limit int) ([]model.Event, error)
}

// TaskEventSequenceReader seeds the next local stream sequence for an active
// task restored after a process restart without counting all historical rows.
type TaskEventSequenceReader interface {
	LatestEventSequence(taskID string) (int64, error)
}

// EventCursor is an ordered, stable database cursor. The pair is required
// because sent_at is not unique under bursty event production.
type EventCursor struct {
	SentAt time.Time `json:"sent_at"`
	ID     int64     `json:"id"`
}

type EventPage struct {
	Items      []model.Event `json:"items"`
	NextCursor *EventCursor  `json:"next_cursor,omitempty"`
	HasMore    bool          `json:"has_more"`
}

// TaskEventPageReader is implemented by durable archives. Filters are kept
// here so status/log consumers do not need to materialize a whole history.
type TaskEventPageReader interface {
	ListEventPage(taskID string, after *EventCursor, limit int, eventTypes []string, nodeID string) (EventPage, error)
}

type TaskSubagentStateReader interface {
	ListLatestSubagentEvents(taskID string, limit int) ([]model.Event, error)
}

type TaskEventAppenderWithID interface {
	AppendEventWithID(*model.Event) error
}

type TaskTerminalEventReader interface {
	LatestTerminalEvent(taskID string) (model.Event, bool, error)
}

type RuntimePreflightArchive interface {
	UpsertRuntimeInstallJob(job model.RuntimeInstallJob) (*model.RuntimeInstallJob, error)
	GetRuntimeInstallJob(operatorID int64, jobID string) (*model.RuntimeInstallJob, error)
	ListRuntimeInstallJobs(operatorID int64, machineID string, limit int) ([]model.RuntimeInstallJob, error)
	ReplaceRuntimeInstallJobItems(jobID string, items []model.RuntimeInstallJobItem) error
	AppendRuntimeInstallEvent(event model.RuntimeInstallEvent) (*model.RuntimeInstallEvent, error)
	ListRuntimeInstallEvents(jobID string, afterSequence int64) ([]model.RuntimeInstallEvent, error)
	ClaimRuntimeUserActionNotification(operatorID int64, jobID, actionID, channel string) (bool, error)
}

// DevicePreferenceArchive is optional so lightweight test archives do not need
// to implement account-scoped device presentation settings.
type DevicePreferenceArchive interface {
	EnsureDevicePreferences(operatorID int64, machineIDs []string) ([]model.DevicePreference, error)
	UpdateDeviceDisplayName(operatorID int64, machineID, displayName string) (*model.DevicePreference, error)
	ReorderDevicePreferences(operatorID int64, machineIDs []string) ([]model.DevicePreference, error)
}

type AgentPreferenceArchive interface {
	ListAgentPreferences(operatorID int64, machineIDs []string) ([]model.AgentPreference, error)
	ReorderAgentPreferences(operatorID int64, machineID string, agentIDs []string) ([]model.AgentPreference, error)
}

type noopArchive struct{}

func (noopArchive) UpsertTask(*model.Task) error                      { return nil }
func (noopArchive) AppendEvent(model.Event) error                     { return nil }
func (noopArchive) ListEvents(string) ([]model.Event, error)          { return nil, nil }
func (noopArchive) LastEventAt(string) (time.Time, bool, error)       { return time.Time{}, false, nil }
func (noopArchive) GetTask(string) (*model.Task, error)               { return nil, nil }
func (noopArchive) ListTasks(model.TaskFilter) ([]*model.Task, error) { return nil, nil }
func (noopArchive) UpsertSession(*model.Session) error                { return nil }
func (noopArchive) ListSessions(model.SessionFilter) ([]*model.Session, error) {
	return nil, nil
}
func (noopArchive) AuthenticateOperator(string, string) (*model.Operator, error) {
	return nil, nil
}
func (noopArchive) GetOperatorByKey(string) (*model.Operator, error) { return nil, nil }
func (noopArchive) GetOperatorEmail(int64) (string, error)           { return "", nil }
func (noopArchive) UsernameExists(string) (bool, error)              { return false, nil }
func (noopArchive) EmailExists(string) (bool, error)                 { return false, nil }
func (noopArchive) CreateOperator(string, string, string, string) (*model.Operator, error) {
	return nil, nil
}
func (noopArchive) ChangePassword(int64, string) error                           { return nil }
func (noopArchive) DeleteOperator(int64) error                                   { return nil }
func (noopArchive) SetDeviceSetting(int64, string, string, string) error         { return nil }
func (noopArchive) GetDeviceSettings(int64, string) (map[string]string, error)   { return nil, nil }
func (noopArchive) DeleteDeviceSettings(int64, string) error                     { return nil }
func (noopArchive) ListDeviceSettingAgentIDs(int64, string) ([]string, error)    { return nil, nil }
func (noopArchive) UpsertPushDevice(model.PushDevice) (*model.PushDevice, error) { return nil, nil }
func (noopArchive) ListPushDevices(int64) ([]*model.PushDevice, error)           { return nil, nil }
func (noopArchive) ListSkills(bool) ([]model.Skill, error) {
	return nil, nil
}
func (noopArchive) UpsertSkill(model.Skill) (*model.Skill, error) {
	return nil, nil
}
func (noopArchive) DeleteSkill(string) error { return nil }
func (noopArchive) ListMCPCatalog(bool) ([]model.MCPCatalogItem, error) {
	return nil, nil
}
func (noopArchive) UpsertMCPCatalogItem(model.MCPCatalogItem) (*model.MCPCatalogItem, error) {
	return nil, nil
}
func (noopArchive) DeleteMCPCatalogItem(string) error { return nil }
func (noopArchive) ListEnvironmentPresets(bool) ([]model.EnvironmentPreset, error) {
	return nil, nil
}
func (noopArchive) UpsertEnvironmentPreset(model.EnvironmentPreset) (*model.EnvironmentPreset, error) {
	return nil, nil
}
func (noopArchive) DeleteEnvironmentPreset(string) error { return nil }
func (noopArchive) ListRuntimeCatalog(bool) ([]model.RuntimeCatalogItem, error) {
	return nil, nil
}
func (noopArchive) UpsertRuntimeCatalogItem(model.RuntimeCatalogItem) (*model.RuntimeCatalogItem, error) {
	return nil, nil
}
func (noopArchive) DeleteRuntimeCatalogItem(string) error { return nil }
func (noopArchive) ListRuntimeVersions(bool) ([]model.RuntimeVersion, error) {
	return nil, nil
}
func (noopArchive) UpsertRuntimeVersion(model.RuntimeVersion) (*model.RuntimeVersion, error) {
	return nil, nil
}
func (noopArchive) DeleteRuntimeVersion(string) error { return nil }
func (noopArchive) ListRuntimeArtifacts(bool) ([]model.RuntimeArtifact, error) {
	return nil, nil
}
func (noopArchive) UpsertRuntimeArtifact(model.RuntimeArtifact) (*model.RuntimeArtifact, error) {
	return nil, nil
}
func (noopArchive) DeleteRuntimeArtifact(string) error { return nil }
func (noopArchive) ListRuntimeMirrors(bool) ([]model.RuntimeMirror, error) {
	return nil, nil
}
func (noopArchive) UpsertRuntimeMirror(model.RuntimeMirror) (*model.RuntimeMirror, error) {
	return nil, nil
}
func (noopArchive) DeleteRuntimeMirror(string) error { return nil }
func (noopArchive) ListToolCatalog(bool) ([]model.ToolCatalogItem, error) {
	return nil, nil
}
func (noopArchive) UpsertToolCatalogItem(model.ToolCatalogItem) (*model.ToolCatalogItem, error) {
	return nil, nil
}
func (noopArchive) DeleteToolCatalogItem(string) error { return nil }
func (noopArchive) ListSemanticAgents(bool) ([]model.SemanticAgentProfile, error) {
	return nil, nil
}
func (noopArchive) UpsertSemanticAgent(model.SemanticAgentProfile) (*model.SemanticAgentProfile, error) {
	return nil, nil
}
func (noopArchive) DeleteSemanticAgent(string) error { return nil }
func (noopArchive) Close() error                     { return nil }
