package model

import "time"

type DeviceView struct {
	Status           string                 `json:"status"`
	CurrentVersion   string                 `json:"current_version,omitempty"`
	TargetVersion    string                 `json:"target_version,omitempty"`
	PreviousVersion  string                 `json:"previous_version,omitempty"`
	LastError        string                 `json:"last_error,omitempty"`
	AgentCount       int                    `json:"agent_count"`
	UpgradeLocked    bool                   `json:"upgrade_locked,omitempty"`
	UpgradeStage     string                 `json:"upgrade_stage,omitempty"`
	UpgradeProgress  int                    `json:"upgrade_progress,omitempty"`
	UpgradeMessage   string                 `json:"upgrade_message,omitempty"`
	UpgradeStartedAt time.Time              `json:"upgrade_started_at,omitempty"`
	UpgradeUpdatedAt time.Time              `json:"upgrade_updated_at,omitempty"`
	UpgradeLogs      []UpgradeLogEntry      `json:"upgrade_logs,omitempty"`
	LauncherVersion  string                 `json:"launcher_version,omitempty"`
	Autostart        *LauncherAutostartInfo `json:"autostart,omitempty"`
	Agents           []Agent                `json:"agents,omitempty"`
}

type UpgradeLogEntry struct {
	Time    time.Time `json:"time"`
	Stage   string    `json:"stage,omitempty"`
	Message string    `json:"message"`
}

type CreateAgentInput struct {
	Name       string `json:"name"`
	ProjectDir string `json:"project_dir"`
}

type RenameAgentInput struct {
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
}

type AgentConfig struct {
	AgentID                    string                `json:"agent_id"`
	Name                       string                `json:"name"`
	ProjectDir                 string                `json:"project_dir"`
	Port                       int                   `json:"port"`
	BinaryName                 string                `json:"binary_name"`
	Args                       []string              `json:"args"`
	Env                        map[string]string     `json:"env,omitempty"`
	UserEnv                    map[string]string     `json:"user_env,omitempty"`
	MCPMode                    string                `json:"mcp_mode,omitempty"`
	MCPServers                 []string              `json:"mcp_servers,omitempty"`
	MCPServerConfigs           []DeviceMCPServerInfo `json:"mcp_server_configs,omitempty"`
	DisableVerifyMCP           bool                  `json:"disable_verify_mcp,omitempty"`
	SemanticAgentID            string                `json:"semantic_agent_id,omitempty"`
	SemanticAgentProfile       *SemanticAgentProfile `json:"semantic_agent_profile,omitempty"`
	ExtraSkillIDs              []string              `json:"extra_skill_ids,omitempty"`
	ExtraSkillDefinitions      []SemanticAgentSkill  `json:"extra_skill_definitions,omitempty"`
	CompactionThresholdPercent int                   `json:"compaction_threshold_percent,omitempty"`
}

type UpgradeInput struct {
	TargetVersion string `json:"target_version"`
}

type RollbackInput struct{}

type LauncherAutostartInput struct {
	DryRun bool `json:"dry_run,omitempty"`
}

type LauncherAutostartInfo struct {
	Enabled bool   `json:"enabled"`
	Method  string `json:"method"`
	Path    string `json:"path,omitempty"`
	Command string `json:"command,omitempty"`
	Error   string `json:"error,omitempty"`
}

type LauncherSelfUpdateInput struct {
	TargetVersion string `json:"target_version,omitempty"`
	BaseURL       string `json:"base_url,omitempty"`
	Apply         bool   `json:"apply,omitempty"`
	Restart       bool   `json:"restart,omitempty"`
	DryRun        bool   `json:"dry_run,omitempty"`
}

type LauncherSelfUpdateAsset struct {
	Platform  string `json:"platform"`
	Filename  string `json:"filename"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256,omitempty"`
	Available bool   `json:"available"`
}

type LauncherSelfUpdateResult struct {
	CurrentVersion  string                   `json:"current_version"`
	LatestVersion   string                   `json:"latest_version"`
	TargetVersion   string                   `json:"target_version,omitempty"`
	Channel         string                   `json:"channel,omitempty"`
	Platform        string                   `json:"platform"`
	UpdateAvailable bool                     `json:"update_available"`
	Asset           *LauncherSelfUpdateAsset `json:"asset,omitempty"`
	Downloaded      bool                     `json:"downloaded"`
	Applied         bool                     `json:"applied"`
	DryRun          bool                     `json:"dry_run,omitempty"`
	StagedPath      string                   `json:"staged_path,omitempty"`
	UpdaterPath     string                   `json:"updater_path,omitempty"`
	Message         string                   `json:"message,omitempty"`
}

type DirectoryPermissionInput struct {
	AgentID string `json:"agent_id,omitempty"`
	Path    string `json:"path,omitempty"`
	Request bool   `json:"request,omitempty"`
}

type DirectoryPermissionResult struct {
	Platform           string `json:"platform"`
	Path               string `json:"path,omitempty"`
	Accessible         bool   `json:"accessible"`
	RequiresUserAction bool   `json:"requires_user_action,omitempty"`
	Action             string `json:"action,omitempty"`
	Message            string `json:"message,omitempty"`
	Error              string `json:"error,omitempty"`
}

type DeviceAIModalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

type DeviceAIThinkingInfo struct {
	Supported           bool           `json:"supported"`
	Source              string         `json:"source,omitempty"`
	Control             string         `json:"control,omitempty"`
	Protocol            string         `json:"protocol,omitempty"`
	SupportedParameters []string       `json:"supported_parameters,omitempty"`
	OverrideEnabled     bool           `json:"override_enabled,omitempty"`
	Variants            map[string]any `json:"variants,omitempty"`
	OverrideVariants    map[string]any `json:"override_variants,omitempty"`
}

type DeviceAIModelInfo struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	OwnedBy    string              `json:"owned_by"`
	Modalities *DeviceAIModalities `json:"modalities,omitempty"`

	// Context / Output 是最终生效值：供应商返回 > 手填 > 预设推断。
	Context int64 `json:"context_limit,omitempty"`
	Output  int64 `json:"output_limit,omitempty"`

	// Upstream* 记录供应商接口返回的原始值，用于保持优先级稳定。
	UpstreamContext int64 `json:"upstream_context_limit,omitempty"`
	UpstreamOutput  int64 `json:"upstream_output_limit,omitempty"`

	// Manual* 记录用户在界面上手填的值。0 表示不手填。
	ManualContext int64 `json:"manual_context_limit,omitempty"`
	ManualOutput  int64 `json:"manual_output_limit,omitempty"`

	Variants map[string]any        `json:"variants,omitempty"`
	Thinking *DeviceAIThinkingInfo `json:"thinking,omitempty"`
}

type DeviceAIProviderInfo struct {
	ID           string              `json:"id"`
	BaseURL      string              `json:"base_url,omitempty"`
	ConsoleURL   string              `json:"console_url,omitempty"`
	APIKeyMasked string              `json:"api_key_masked,omitempty"`
	APIMode      string              `json:"api_mode,omitempty"`
	Models       []DeviceAIModelInfo `json:"models,omitempty"`
}

type DeviceAIConfigInfo struct {
	Exists       bool                   `json:"exists"`
	ConfigPath   string                 `json:"config_path,omitempty"`
	Provider     string                 `json:"provider,omitempty"`
	BaseURL      string                 `json:"base_url,omitempty"`
	ConsoleURL   string                 `json:"console_url,omitempty"`
	APIKeyMasked string                 `json:"api_key_masked,omitempty"`
	APIMode      string                 `json:"api_mode,omitempty"`
	Model        string                 `json:"model,omitempty"`
	Force        bool                   `json:"force,omitempty"`
	RawJSON      string                 `json:"raw_json,omitempty"`
	PreviewJSON  string                 `json:"preview_json,omitempty"`
	ChangedKeys  []string               `json:"changed_keys,omitempty"`
	Warning      string                 `json:"warning,omitempty"`
	Models       []DeviceAIModelInfo    `json:"models,omitempty"`
	Providers    []DeviceAIProviderInfo `json:"providers,omitempty"`
}

type DeviceAIConfigInput struct {
	Provider   string `json:"provider,omitempty"`
	BaseURL    string `json:"base_url,omitempty"`
	ConsoleURL string `json:"console_url,omitempty"`
	APIKey     string `json:"api_key,omitempty"`
	APIMode    string `json:"api_mode,omitempty"`
	Model      string `json:"model,omitempty"`
	Text       string `json:"text,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
	// Config 携带界面上当前已配置的模型，刷新时用于保留用户手填的窗口值。
	Config struct {
		Models []DeviceAIModelInfo `json:"models,omitempty"`
	} `json:"config,omitempty"`
}

type DeviceMCPServerInfo struct {
	Name                    string            `json:"name"`
	Type                    string            `json:"type"`
	Enabled                 bool              `json:"enabled"`
	LaunchReady             bool              `json:"launch_ready,omitempty"`
	LaunchBlockReason       string            `json:"launch_block_reason,omitempty"`
	Timeout                 int               `json:"timeout,omitempty"`
	ConnectTimeout          int               `json:"connect_timeout,omitempty"`
	DiscoveryTimeout        int               `json:"discovery_timeout,omitempty"`
	ToolTimeout             int               `json:"tool_timeout,omitempty"`
	AsyncTools              []string          `json:"async_tools,omitempty"`
	URL                     string            `json:"url,omitempty"`
	Headers                 map[string]string `json:"headers,omitempty"`
	Command                 []string          `json:"command,omitempty"`
	Environment             map[string]string `json:"environment,omitempty"`
	OAuthMode               string            `json:"oauth_mode,omitempty"`
	OAuthClientID           string            `json:"oauth_client_id,omitempty"`
	OAuthClientSecret       string            `json:"oauth_client_secret,omitempty"`
	OAuthClientSecretMasked string            `json:"oauth_client_secret_masked,omitempty"`
	OAuthScope              string            `json:"oauth_scope,omitempty"`
}

type DeviceMCPConfigInfo struct {
	Exists      bool                  `json:"exists"`
	ConfigPath  string                `json:"config_path,omitempty"`
	RawJSON     string                `json:"raw_json,omitempty"`
	PreviewJSON string                `json:"preview_json,omitempty"`
	ChangedKeys []string              `json:"changed_keys,omitempty"`
	Warning     string                `json:"warning,omitempty"`
	Servers     []DeviceMCPServerInfo `json:"servers,omitempty"`
}

type DeviceAgentMCPSelectionInfo struct {
	AgentID          string                `json:"agent_id"`
	Mode             string                `json:"mode"`
	SelectedServers  []string              `json:"selected_servers"`
	AvailableServers []DeviceMCPServerInfo `json:"available_servers,omitempty"`
}

type DeviceAgentMCPSelectionInput struct {
	AgentID string   `json:"agent_id"`
	Mode    string   `json:"mode,omitempty"`
	Servers []string `json:"servers,omitempty"`
}

type DeviceAgentSkillSelectionInfo struct {
	AgentID         string               `json:"agent_id"`
	DefaultSkills   []string             `json:"default_skills,omitempty"`
	ExtraSkills     []string             `json:"extra_skills,omitempty"`
	EffectiveSkills []string             `json:"effective_skills,omitempty"`
	AvailableSkills []SemanticAgentSkill `json:"available_skills,omitempty"`
}

type DeviceAgentSkillSelectionInput struct {
	AgentID       string               `json:"agent_id"`
	ExtraSkillIDs []string             `json:"extra_skill_ids,omitempty"`
	Skills        []SemanticAgentSkill `json:"skills,omitempty"`
}

type DeviceAgentSkillImportInput struct {
	AgentID   string             `json:"agent_id"`
	Skill     SemanticAgentSkill `json:"skill"`
	Overwrite bool               `json:"overwrite,omitempty"`
}

type DeviceSkillConfigEntry struct {
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	Content      string             `json:"content,omitempty"`
	PackageFiles []SkillPackageFile `json:"package_files,omitempty"`
	Enabled      bool               `json:"enabled"`
}

type DeviceSkillConfigInfo struct {
	MachineID string                   `json:"machine_id,omitempty"`
	Enabled   bool                     `json:"enabled"`
	Skills    []DeviceSkillConfigEntry `json:"skills,omitempty"`
}

type DeviceSkillConfigInput struct {
	Skill     SemanticAgentSkill `json:"skill"`
	Overwrite bool               `json:"overwrite,omitempty"`
}

type MCPInstallInfo struct {
	PackageManager     string            `json:"package_manager,omitempty"`
	SourceURL          string            `json:"source_url,omitempty"`
	Assets             []MCPInstallAsset `json:"assets,omitempty"`
	Files              []MCPInstallFile  `json:"files,omitempty"`
	InstallCommands    []string          `json:"install_commands,omitempty"`
	BuildCommands      []string          `json:"build_commands,omitempty"`
	RunCommandTemplate []string          `json:"run_command_template,omitempty"`
	ConfigPathTemplate string            `json:"config_path_template,omitempty"`
	ExecutableNames    []string          `json:"executable_names,omitempty"`
	Environment        map[string]string `json:"environment,omitempty"`
	InstallNotes       string            `json:"install_notes,omitempty"`
}

type MCPInstallAsset struct {
	Name        string            `json:"name,omitempty"`
	URL         string            `json:"url,omitempty"`
	Filename    string            `json:"filename,omitempty"`
	SHA256      string            `json:"sha256,omitempty"`
	Extract     bool              `json:"extract,omitempty"`
	PackageKind string            `json:"package_kind,omitempty"`
	TargetDir   string            `json:"target_dir,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

type MCPInstallFile struct {
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
	Mode    int    `json:"mode,omitempty"`
}

type SemanticAgentMCPDependency struct {
	ID                string              `json:"id"`
	Name              string              `json:"name"`
	Title             string              `json:"title,omitempty"`
	Type              string              `json:"type,omitempty"`
	SourceURL         string              `json:"source_url,omitempty"`
	Config            DeviceMCPServerInfo `json:"config"`
	Install           MCPInstallInfo      `json:"install,omitempty"`
	LaunchReady       bool                `json:"launch_ready"`
	LaunchBlockReason string              `json:"launch_block_reason,omitempty"`
}

type SkillPackageFile struct {
	Path       string `json:"path"`
	Content    string `json:"content,omitempty"`
	Executable bool   `json:"executable,omitempty"`
}

type SemanticAgentSkill struct {
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	Content      string             `json:"content,omitempty"`
	PackageFiles []SkillPackageFile `json:"package_files,omitempty"`
}

type SemanticAgentProfile struct {
	ID                    string                       `json:"id"`
	Name                  string                       `json:"name"`
	Description           string                       `json:"description,omitempty"`
	Icon                  string                       `json:"icon,omitempty"`
	Color                 string                       `json:"color,omitempty"`
	OpencodeName          string                       `json:"opencode_name,omitempty"`
	Prompt                string                       `json:"prompt,omitempty"`
	SkillIDs              []string                     `json:"skill_ids,omitempty"`
	MCPIDs                []string                     `json:"mcp_ids,omitempty"`
	Skills                []string                     `json:"skills,omitempty"`
	SkillDefinitions      []SemanticAgentSkill         `json:"skill_definitions,omitempty"`
	RecommendedMCPServers []string                     `json:"recommended_mcp_servers,omitempty"`
	RecommendedMCPConfigs []DeviceMCPServerInfo        `json:"recommended_mcp_configs,omitempty"`
	MCPDependencies       []SemanticAgentMCPDependency `json:"mcp_dependencies,omitempty"`
	ToolPermissions       map[string]any               `json:"tool_permissions,omitempty"`
	RuntimeRequirements   []SemanticAgentRuntime       `json:"runtime_requirements,omitempty"`
	Model                 string                       `json:"model,omitempty"`
	Enabled               bool                         `json:"enabled"`
	SortOrder             int                          `json:"sort_order,omitempty"`
	BuiltIn               bool                         `json:"built_in,omitempty"`
}

type DeviceAgentSemanticSelectionInfo struct {
	AgentID          string                 `json:"agent_id"`
	SemanticAgentID  string                 `json:"semantic_agent_id"`
	VerifyMCPEnabled bool                   `json:"verify_mcp_enabled"`
	Profile          *SemanticAgentProfile  `json:"profile,omitempty"`
	AvailableAgents  []SemanticAgentProfile `json:"available_agents,omitempty"`
}

type DeviceAgentSemanticSelectionInput struct {
	AgentID             string                `json:"agent_id"`
	SemanticAgentID     string                `json:"semantic_agent_id"`
	ApplyRecommendedMCP bool                  `json:"apply_recommended_mcp,omitempty"`
	DisableVerifyMCP    bool                  `json:"disable_verify_mcp,omitempty"`
	SemanticAgent       *SemanticAgentProfile `json:"semantic_agent,omitempty"`
	ExtraSkillIDs       []string              `json:"extra_skill_ids,omitempty"`
	ExtraSkills         []SemanticAgentSkill  `json:"extra_skills,omitempty"`
}

type DeviceAgentEnvInfo struct {
	AgentID     string            `json:"agent_id"`
	Name        string            `json:"name,omitempty"`
	ProjectDir  string            `json:"project_dir,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

type DeviceEnvConfigInfo struct {
	GlobalEnvironment map[string]string    `json:"global_environment,omitempty"`
	Agents            []DeviceAgentEnvInfo `json:"agents,omitempty"`
	Revision          string               `json:"revision,omitempty"`
}

type MCPToolInfo struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
}

type MCPServerStatusInfo struct {
	Name   string        `json:"name"`
	Status string        `json:"status"`
	Error  string        `json:"error,omitempty"`
	Tools  []MCPToolInfo `json:"tools,omitempty"`
}

type MCPStatusInfo struct {
	AgentID string                `json:"agent_id,omitempty"`
	Port    int                   `json:"port,omitempty"`
	Servers []MCPServerStatusInfo `json:"servers,omitempty"`
}

type RuntimeToolStatusInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

type RuntimeEnvironmentInfo struct {
	RuntimeDir  string                  `json:"runtime_dir,omitempty"`
	Preparing   bool                    `json:"preparing,omitempty"`
	UpdatedAt   time.Time               `json:"updated_at,omitempty"`
	Environment map[string]string       `json:"environment,omitempty"`
	Tools       []RuntimeToolStatusInfo `json:"tools,omitempty"`
	Logs        []string                `json:"logs,omitempty"`
	LastError   string                  `json:"last_error,omitempty"`
}

type LauncherDiagnosticsInput struct {
	AgentID     string `json:"agent_id,omitempty"`
	LogLines    int    `json:"log_lines,omitempty"`
	IncludeLogs bool   `json:"include_logs,omitempty"`
}

type LauncherDiagnostics struct {
	CollectedAt     time.Time              `json:"collected_at"`
	Platform        string                 `json:"platform"`
	Architecture    string                 `json:"architecture"`
	LauncherVersion string                 `json:"launcher_version,omitempty"`
	RuntimeDir      string                 `json:"runtime_dir,omitempty"`
	State           DeviceView             `json:"state"`
	Agents          []Agent                `json:"agents,omitempty"`
	Runtime         RuntimeEnvironmentInfo `json:"runtime"`
	MCPStatus       *MCPStatusInfo         `json:"mcp_status,omitempty"`
	Logs            map[string][]string    `json:"logs,omitempty"`
	Warnings        []string               `json:"warnings,omitempty"`
}

type SSHStatus struct {
	Platform           string `json:"platform"`
	Installed          bool   `json:"installed"`
	Running            bool   `json:"running"`
	Listening          bool   `json:"listening"`
	ListenAddress      string `json:"listen_address,omitempty"`
	HostKeyFingerprint string `json:"host_key_fingerprint,omitempty"`
	Message            string `json:"message,omitempty"`
	Error              string `json:"error,omitempty"`
}

type SSHSetupResult struct {
	Status             SSHStatus `json:"status"`
	RequiresUserAction bool      `json:"requires_user_action,omitempty"`
	Action             string    `json:"action,omitempty"`
	Output             string    `json:"output,omitempty"`
	Error              string    `json:"error,omitempty"`
}

type SSHAuthorizedKeyInput struct {
	PublicKey string `json:"public_key"`
}

type SSHAuthorizedKeyResult struct {
	Installed     bool   `json:"installed"`
	AlreadyExists bool   `json:"already_exists,omitempty"`
	Fingerprint   string `json:"fingerprint,omitempty"`
	Message       string `json:"message,omitempty"`
	Error         string `json:"error,omitempty"`
}

type SSHManagedAuthorizedKeyInput struct {
	PublicKey string    `json:"public_key"`
	ExpiresAt time.Time `json:"expires_at"`
}

type SSHManagedAuthorizedKeyResult struct {
	Installed     bool      `json:"installed"`
	AlreadyExists bool      `json:"already_exists,omitempty"`
	Fingerprint   string    `json:"fingerprint,omitempty"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
	Message       string    `json:"message,omitempty"`
	Error         string    `json:"error,omitempty"`
}

type TunnelOpenPayload struct {
	MachineID string `json:"machine_id"`
	TunnelID  string `json:"tunnel_id"`
	Token     string `json:"token"`
	TunnelURL string `json:"tunnel_url"`
}

type RuntimeCatalogItem struct {
	ID                       string            `json:"id"`
	Name                     string            `json:"name"`
	Description              string            `json:"description,omitempty"`
	RuntimeKind              string            `json:"runtime_kind,omitempty"`
	DefaultVersionConstraint string            `json:"default_version_constraint,omitempty"`
	ExecutableNames          []string          `json:"executable_names,omitempty"`
	EnvTemplate              map[string]string `json:"env_template,omitempty"`
	InstallStrategy          string            `json:"install_strategy,omitempty"`
	Enabled                  bool              `json:"enabled"`
	Tags                     []string          `json:"tags,omitempty"`
	SortOrder                int               `json:"sort_order,omitempty"`
	BuiltIn                  bool              `json:"built_in,omitempty"`
	Versions                 []RuntimeVersion  `json:"versions,omitempty"`
	Artifacts                []RuntimeArtifact `json:"artifacts,omitempty"`
	Mirrors                  []RuntimeMirror   `json:"mirrors,omitempty"`
}

type RuntimeVersion struct {
	ID              string            `json:"id"`
	RuntimeID       string            `json:"runtime_id"`
	Version         string            `json:"version"`
	Channel         string            `json:"channel,omitempty"`
	Status          string            `json:"status,omitempty"`
	VersionOrder    int               `json:"version_order,omitempty"`
	DefaultSelected bool              `json:"default_selected"`
	MinOSVersion    map[string]string `json:"min_os_version,omitempty"`
	InstallManifest map[string]any    `json:"install_manifest,omitempty"`
	Enabled         bool              `json:"enabled"`
	SortOrder       int               `json:"sort_order,omitempty"`
}

type RuntimeArtifact struct {
	ID           string            `json:"id"`
	RuntimeID    string            `json:"runtime_id"`
	VersionID    string            `json:"version_id"`
	Platform     string            `json:"platform"`
	Arch         string            `json:"arch"`
	PackageKind  string            `json:"package_kind,omitempty"`
	Filename     string            `json:"filename"`
	SizeBytes    int64             `json:"size_bytes,omitempty"`
	SHA256       string            `json:"sha256,omitempty"`
	StorageKey   string            `json:"storage_key,omitempty"`
	DownloadPath string            `json:"download_path,omitempty"`
	ExtractRoot  string            `json:"extract_root,omitempty"`
	BinPaths     []string          `json:"bin_paths,omitempty"`
	EnvPatch     map[string]string `json:"env_patch,omitempty"`
	Priority     int               `json:"priority,omitempty"`
	Enabled      bool              `json:"enabled"`
}

type RuntimeMirror struct {
	ID                  string            `json:"id"`
	RuntimeID           string            `json:"runtime_id,omitempty"`
	VersionID           string            `json:"version_id,omitempty"`
	Name                string            `json:"name"`
	Description         string            `json:"description,omitempty"`
	BaseURL             string            `json:"base_url,omitempty"`
	URLTemplate         string            `json:"url_template,omitempty"`
	ChecksumURLTemplate string            `json:"checksum_url_template,omitempty"`
	Platform            string            `json:"platform,omitempty"`
	Arch                string            `json:"arch,omitempty"`
	Priority            int               `json:"priority,omitempty"`
	TimeoutSeconds      int               `json:"timeout_seconds,omitempty"`
	Enabled             bool              `json:"enabled"`
	Headers             map[string]string `json:"headers,omitempty"`
	Tags                []string          `json:"tags,omitempty"`
}

type SemanticAgentRuntime struct {
	RuntimeID         string `json:"runtime_id"`
	VersionConstraint string `json:"version_constraint,omitempty"`
	Required          bool   `json:"required"`
	Purpose           string `json:"purpose,omitempty"`
	SortOrder         int    `json:"sort_order,omitempty"`
}

type RuntimeInstallJobItem struct {
	JobID           string    `json:"job_id"`
	ItemType        string    `json:"item_type"`
	ItemID          string    `json:"item_id"`
	Name            string    `json:"name,omitempty"`
	Required        bool      `json:"required"`
	Status          string    `json:"status"`
	ProgressPercent int       `json:"progress_percent,omitempty"`
	Error           string    `json:"error,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

type RuntimeInstallEvent struct {
	ID              int64          `json:"id,omitempty"`
	JobID           string         `json:"job_id"`
	Sequence        int64          `json:"sequence"`
	ItemType        string         `json:"item_type,omitempty"`
	ItemID          string         `json:"item_id,omitempty"`
	Phase           string         `json:"phase,omitempty"`
	Status          string         `json:"status,omitempty"`
	Message         string         `json:"message,omitempty"`
	ReceivedBytes   int64          `json:"received_bytes,omitempty"`
	TotalBytes      int64          `json:"total_bytes,omitempty"`
	ProgressPercent int            `json:"progress_percent,omitempty"`
	Details         map[string]any `json:"details,omitempty"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
}

type RuntimeUserAction struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind,omitempty"`
	Title        string    `json:"title,omitempty"`
	Message      string    `json:"message,omitempty"`
	Instructions []string  `json:"instructions,omitempty"`
	RequestedAt  time.Time `json:"requested_at,omitempty"`
}

type RuntimePreflightStartPayload struct {
	JobID               string                `json:"job_id"`
	MachineID           string                `json:"machine_id"`
	LauncherAgentID     string                `json:"launcher_agent_id"`
	SemanticAgentID     string                `json:"semantic_agent_id"`
	ApplyRecommendedMCP bool                  `json:"apply_recommended_mcp,omitempty"`
	DisableVerifyMCP    bool                  `json:"disable_verify_mcp,omitempty"`
	AutoRepair          bool                  `json:"auto_repair,omitempty"`
	RestartAfterApply   bool                  `json:"restart_after_apply,omitempty"`
	SemanticAgent       *SemanticAgentProfile `json:"semantic_agent,omitempty"`
	Runtimes            []RuntimeCatalogItem  `json:"runtimes,omitempty"`
}

type RuntimePreflightStatusPayload struct {
	JobID              string                            `json:"job_id"`
	MachineID          string                            `json:"machine_id,omitempty"`
	LauncherAgentID    string                            `json:"launcher_agent_id,omitempty"`
	SemanticAgentID    string                            `json:"semantic_agent_id,omitempty"`
	Status             string                            `json:"status"`
	CurrentStep        string                            `json:"current_step,omitempty"`
	ProgressPercent    int                               `json:"progress_percent,omitempty"`
	RequiresUserAction bool                              `json:"requires_user_action,omitempty"`
	UserAction         *RuntimeUserAction                `json:"user_action,omitempty"`
	Items              []RuntimeInstallJobItem           `json:"items,omitempty"`
	Events             []RuntimeInstallEvent             `json:"events,omitempty"`
	Error              string                            `json:"error,omitempty"`
	SemanticSelection  *DeviceAgentSemanticSelectionInfo `json:"semantic_selection,omitempty"`
}

type Directory struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size,omitempty"`
}

type DirectoryListResult struct {
	CurrentPath string      `json:"current_path"`
	ParentPath  string      `json:"parent_path,omitempty"`
	Entries     []Directory `json:"entries"`
}

type DirectoryRequest struct {
	Path     string `json:"path,omitempty"`
	AllowAll bool   `json:"allow_all,omitempty"`
}

type DirectoryFileRequest struct {
	Path        string `json:"path,omitempty"`
	IsDir       bool   `json:"is_dir,omitempty"`
	AllowAll    bool   `json:"allow_all,omitempty"`
	Content     string `json:"content,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	UploadID    string `json:"upload_id,omitempty"`
	Size        int64  `json:"size,omitempty"`
	TotalChunks int    `json:"total_chunks,omitempty"`
	ChunkIndex  int    `json:"chunk_index,omitempty"`
	Offset      int64  `json:"offset,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	// Resume 为 true 时保留同一 upload_id 已写入的分块，仅补齐缺失分块。
	Resume bool `json:"resume,omitempty"`
}

// UploadStatus 描述一次分块上传的当前进度，供客户端断点续传使用。
type UploadStatus struct {
	Path           string `json:"path"`
	UploadID       string `json:"upload_id"`
	Size           int64  `json:"size"`
	TotalChunks    int    `json:"total_chunks"`
	ReceivedChunks []int  `json:"received_chunks"`
	ReceivedBytes  int64  `json:"received_bytes"`
	Completed      bool   `json:"completed"`
	Resumable      bool   `json:"resumable"`
}

type ProjectFile struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"`
	IsDir    bool   `json:"is_dir"`
	Size     int64  `json:"size,omitempty"`
	Content  string `json:"content,omitempty"`
	Encoding string `json:"encoding,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

type ProjectFilesRequest struct {
	AgentID     string `json:"agent_id"`
	Path        string `json:"path,omitempty"`
	Name        string `json:"name,omitempty"`
	IsDir       bool   `json:"is_dir,omitempty"`
	Content     string `json:"content,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	UploadID    string `json:"upload_id,omitempty"`
	Size        int64  `json:"size,omitempty"`
	TotalChunks int    `json:"total_chunks,omitempty"`
	ChunkIndex  int    `json:"chunk_index,omitempty"`
	Offset      int64  `json:"offset,omitempty"`
	Length      int64  `json:"length,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	// Resume 为 true 时保留同一 upload_id 已写入的分块，仅补齐缺失分块。
	Resume bool `json:"resume,omitempty"`
}

type WelcomePayload struct {
	AgentID              string `json:"agent_id"`
	HeartbeatIntervalSec int    `json:"heartbeat_interval_sec"`
}

type HeartbeatPayload struct {
	AgentID       string `json:"agent_id,omitempty"`
	RunningTaskID string `json:"running_task_id,omitempty"`
	// Metrics 是设备运行状态快照（内存/磁盘/CPU 等）。
	// 老版本 launcher 不上报该字段，服务端按"无指标"处理。
	Metrics *DeviceMetrics `json:"metrics,omitempty"`
}

// DeviceMetrics 是设备运行状态的一次采集快照，由 launcher 随心跳上报。
type DeviceMetrics struct {
	CollectedAt   time.Time    `json:"collected_at"`
	Platform      string       `json:"platform,omitempty"`
	Arch          string       `json:"architecture,omitempty"`
	UptimeSeconds uint64       `json:"uptime_seconds,omitempty"`
	MemoryTotal   uint64       `json:"memory_total_bytes,omitempty"`
	MemoryUsed    uint64       `json:"memory_used_bytes,omitempty"`
	MemoryPercent float64      `json:"memory_used_percent,omitempty"`
	CPUPercent    float64      `json:"cpu_used_percent,omitempty"`
	CPUCores      int          `json:"cpu_cores,omitempty"`
	Load1         float64      `json:"load_1,omitempty"`
	Load5         float64      `json:"load_5,omitempty"`
	Load15        float64      `json:"load_15,omitempty"`
	LoadAvailable bool         `json:"load_available,omitempty"`
	Disks         []DiskMetric `json:"disks,omitempty"`
}

// DiskMetric 是单个磁盘分区的占用情况。
type DiskMetric struct {
	Mount       string  `json:"mount"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type ProjectIdentityCorrectionInput struct {
	AgentID     string `json:"agent_id"`
	Action      string `json:"action"`
	KeepScopeID string `json:"keep_scope_id,omitempty"`
}

type ProjectIdentityCorrectionResult struct {
	ProjectScopeID        string `json:"project_scope_id"`
	InstanceNonce         string `json:"instance_nonce"`
	LineageProjectScopeID string `json:"lineage_project_scope_id,omitempty"`
	Root                  string `json:"root"`
	MarkerWritable        bool   `json:"marker_writable"`
}

type Agent struct {
	AgentID           string    `json:"agent_id"`
	Name              string    `json:"name"`
	ProjectDir        string    `json:"project_dir"`
	SemanticAgentID   string    `json:"semantic_agent_id,omitempty"`
	SemanticAgentName string    `json:"semantic_agent_name,omitempty"`
	Enabled           bool      `json:"enabled"`
	Status            string    `json:"status"`
	PID               int       `json:"pid,omitempty"`
	Port              int       `json:"port,omitempty"`
	Version           string    `json:"version,omitempty"`
	Restarts          int       `json:"restarts,omitempty"`
	LastError         string    `json:"last_error,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type SetAgentEnabledInput struct {
	AgentID string `json:"agent_id"`
	Enabled bool   `json:"enabled"`
}

type DeviceEnvConfigInput struct {
	GlobalEnvironment map[string]string `json:"global_environment,omitempty"`
	AgentID           string            `json:"agent_id,omitempty"`
	AgentEnvironment  map[string]string `json:"agent_environment,omitempty"`
	ExpectedRevision  string            `json:"expected_revision,omitempty"`
}

type DeviceAgentCompactionConfig struct {
	AgentID                   string `json:"agent_id"`
	ThresholdPercent          int    `json:"threshold_percent"`
	DefaultThresholdPercent   int    `json:"default_threshold_percent"`
	EffectiveThresholdPercent int    `json:"effective_threshold_percent"`
}

type DeviceAgentCompactionConfigInput struct {
	AgentID          string `json:"agent_id"`
	ThresholdPercent int    `json:"threshold_percent"`
}
