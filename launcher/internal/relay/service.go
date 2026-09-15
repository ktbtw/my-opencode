package relay

import "launcher/internal/model"

type Service interface {
	State() model.DeviceView
	Directories(model.DirectoryRequest) (model.DirectoryListResult, error)
	CreateDirectoryUpload(model.DirectoryFileRequest) (model.ProjectFile, error)
	WriteDirectoryUploadChunk(model.DirectoryFileRequest) (model.ProjectFile, error)
	CompleteDirectoryUpload(model.DirectoryFileRequest) (model.ProjectFile, error)
	DirectoryUploadStatus(model.DirectoryFileRequest) (model.UploadStatus, error)
	CreateDirectoryFile(model.DirectoryFileRequest) (model.ProjectFile, error)
	CreateDirectoryFolder(model.DirectoryFileRequest) (model.ProjectFile, error)
	DeleteDirectoryFile(model.DirectoryFileRequest) (model.ProjectFile, error)
	ProjectFiles(model.ProjectFilesRequest) (model.DirectoryListResult, error)
	DownloadProjectFile(model.ProjectFilesRequest) (model.ProjectFile, error)
	CreateProjectDownload(model.ProjectFilesRequest) (model.ProjectFile, error)
	ReadProjectDownloadChunk(model.ProjectFilesRequest) (model.ProjectFile, error)
	UploadProjectFile(model.ProjectFilesRequest) (model.ProjectFile, error)
	CreateProjectUpload(model.ProjectFilesRequest) (model.ProjectFile, error)
	WriteProjectUploadChunk(model.ProjectFilesRequest) (model.ProjectFile, error)
	CompleteProjectUpload(model.ProjectFilesRequest) (model.ProjectFile, error)
	ProjectUploadStatus(model.ProjectFilesRequest) (model.UploadStatus, error)
	CreateProjectFile(model.ProjectFilesRequest) (model.ProjectFile, error)
	CreateProjectFolder(model.ProjectFilesRequest) (model.ProjectFile, error)
	DeleteProjectFile(model.ProjectFilesRequest) (model.ProjectFile, error)
	RenameProjectFile(model.ProjectFilesRequest) (model.ProjectFile, error)
	CreateAgent(model.CreateAgentInput) (model.Agent, error)
	RenameAgent(model.RenameAgentInput) (model.Agent, error)
	RestartAgent(string) (model.Agent, error)
	CorrectProjectIdentity(model.ProjectIdentityCorrectionInput) (model.ProjectIdentityCorrectionResult, error)
	SetAgentEnabled(model.SetAgentEnabledInput) (model.Agent, error)
	RemoveAgent(string) error
	AIConfig() (*model.DeviceAIConfigInfo, error)
	ListAIModels(model.DeviceAIConfigInput) (*model.DeviceAIConfigInfo, error)
	SaveAIConfig(model.DeviceAIConfigInfo) (*model.DeviceAIConfigInfo, error)
	SaveAIConfigText(string) (*model.DeviceAIConfigInfo, error)
	ClearAIProvider(string) (*model.DeviceAIConfigInfo, error)
	MCPConfig() (*model.DeviceMCPConfigInfo, error)
	MCPStatus(string) (*model.MCPStatusInfo, error)
	AgentMCPSelection(string) (model.DeviceAgentMCPSelectionInfo, error)
	SaveAgentMCPSelection(model.DeviceAgentMCPSelectionInput) (model.DeviceAgentMCPSelectionInfo, error)
	AgentSkillSelection(model.DeviceAgentSkillSelectionInput) (model.DeviceAgentSkillSelectionInfo, error)
	SaveAgentSkillSelection(model.DeviceAgentSkillSelectionInput) (model.DeviceAgentSkillSelectionInfo, error)
	ImportAgentSkill(model.DeviceAgentSkillImportInput) (model.DeviceAgentSkillSelectionInfo, error)
	GlobalSkillConfig() (*model.DeviceSkillConfigInfo, error)
	SaveGlobalSkillConfig(model.DeviceSkillConfigInfo) (*model.DeviceSkillConfigInfo, error)
	ImportGlobalSkill(model.DeviceSkillConfigInput) (*model.DeviceSkillConfigInfo, error)
	AgentSemanticSelection(string) (model.DeviceAgentSemanticSelectionInfo, error)
	SaveAgentSemanticSelection(model.DeviceAgentSemanticSelectionInput) (model.DeviceAgentSemanticSelectionInfo, error)
	StartAgentSemanticPreflight(model.RuntimePreflightStartPayload, func(model.RuntimePreflightStatusPayload) error) error
	SaveMCPConfig(model.DeviceMCPConfigInfo) (*model.DeviceMCPConfigInfo, error)
	RemoveMCPConfig(string) (*model.DeviceMCPConfigInfo, error)
	EnvConfig() (*model.DeviceEnvConfigInfo, error)
	SaveEnvConfig(model.DeviceEnvConfigInput) (*model.DeviceEnvConfigInfo, error)
	StartUpgrade(model.UpgradeInput) error
	Upgrade(model.UpgradeInput) error
	Rollback() error
	AutostartStatus() model.LauncherAutostartInfo
	EnableAutostart(model.LauncherAutostartInput) (model.LauncherAutostartInfo, error)
	DisableAutostart() (model.LauncherAutostartInfo, error)
	SelfUpdate(model.LauncherSelfUpdateInput) (model.LauncherSelfUpdateResult, error)
	DirectoryPermission(model.DirectoryPermissionInput) (model.DirectoryPermissionResult, error)
	Diagnostics(model.LauncherDiagnosticsInput) (model.LauncherDiagnostics, error)
}
