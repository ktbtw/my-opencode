export namespace main {
	
	export class BootstrapProgress {
	    phase?: string;
	    message?: string;
	    percent?: number;
	    started_at?: string;
	    updated_at?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new BootstrapProgress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.message = source["message"];
	        this.percent = source["percent"];
	        this.started_at = source["started_at"];
	        this.updated_at = source["updated_at"];
	        this.error = source["error"];
	    }
	}
	export class BootstrapResult {
	    ready: boolean;
	    message?: string;
	    state?: model.DeviceView;
	    logs?: string[];
	    bootstrap?: BootstrapProgress;
	
	    static createFrom(source: any = {}) {
	        return new BootstrapResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ready = source["ready"];
	        this.message = source["message"];
	        this.state = this.convertValues(source["state"], model.DeviceView);
	        this.logs = source["logs"];
	        this.bootstrap = this.convertValues(source["bootstrap"], BootstrapProgress);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GUISettings {
	    terminal_compat: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GUISettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.terminal_compat = source["terminal_compat"];
	    }
	}
	export class LoginInput {
	    server_url: string;
	    username: string;
	    password: string;
	
	    static createFrom(source: any = {}) {
	        return new LoginInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server_url = source["server_url"];
	        this.username = source["username"];
	        this.password = source["password"];
	    }
	}
	export class LoginResult {
	    success: boolean;
	    message?: string;
	    operator_name?: string;
	    operator_key?: string;
	    operator_key_mask?: string;
	
	    static createFrom(source: any = {}) {
	        return new LoginResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.operator_name = source["operator_name"];
	        this.operator_key = source["operator_key"];
	        this.operator_key_mask = source["operator_key_mask"];
	    }
	}
	export class SSHTunnelResult {
	    tunnel_id?: string;
	    command: string;
	    unix_command: string;
	    windows_command: string;
	    expires_at?: string;
	    target?: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHTunnelResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tunnel_id = source["tunnel_id"];
	        this.command = source["command"];
	        this.unix_command = source["unix_command"];
	        this.windows_command = source["windows_command"];
	        this.expires_at = source["expires_at"];
	        this.target = source["target"];
	    }
	}
	export class SavedLogin {
	    server_url: string;
	    username: string;
	    password: string;
	    auto_login: boolean;
	    has_saved: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SavedLogin(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server_url = source["server_url"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.auto_login = source["auto_login"];
	        this.has_saved = source["has_saved"];
	    }
	}

}

export namespace model {
	
	export class Agent {
	    agent_id: string;
	    name: string;
	    project_dir: string;
	    semantic_agent_id?: string;
	    semantic_agent_name?: string;
	    enabled: boolean;
	    status: string;
	    pid?: number;
	    port?: number;
	    version?: string;
	    restarts?: number;
	    last_error?: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Agent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent_id = source["agent_id"];
	        this.name = source["name"];
	        this.project_dir = source["project_dir"];
	        this.semantic_agent_id = source["semantic_agent_id"];
	        this.semantic_agent_name = source["semantic_agent_name"];
	        this.enabled = source["enabled"];
	        this.status = source["status"];
	        this.pid = source["pid"];
	        this.port = source["port"];
	        this.version = source["version"];
	        this.restarts = source["restarts"];
	        this.last_error = source["last_error"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateAgentInput {
	    name: string;
	    project_dir: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateAgentInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.project_dir = source["project_dir"];
	    }
	}
	export class LauncherAutostartInfo {
	    enabled: boolean;
	    method: string;
	    path?: string;
	    command?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new LauncherAutostartInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.method = source["method"];
	        this.path = source["path"];
	        this.command = source["command"];
	        this.error = source["error"];
	    }
	}
	export class UpgradeLogEntry {
	    // Go type: time
	    time: any;
	    stage?: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new UpgradeLogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = this.convertValues(source["time"], null);
	        this.stage = source["stage"];
	        this.message = source["message"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DeviceView {
	    status: string;
	    current_version?: string;
	    target_version?: string;
	    previous_version?: string;
	    last_error?: string;
	    agent_count: number;
	    upgrade_locked?: boolean;
	    upgrade_stage?: string;
	    upgrade_progress?: number;
	    upgrade_message?: string;
	    // Go type: time
	    upgrade_started_at?: any;
	    // Go type: time
	    upgrade_updated_at?: any;
	    upgrade_logs?: UpgradeLogEntry[];
	    launcher_version?: string;
	    autostart?: LauncherAutostartInfo;
	    agents?: Agent[];
	
	    static createFrom(source: any = {}) {
	        return new DeviceView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.current_version = source["current_version"];
	        this.target_version = source["target_version"];
	        this.previous_version = source["previous_version"];
	        this.last_error = source["last_error"];
	        this.agent_count = source["agent_count"];
	        this.upgrade_locked = source["upgrade_locked"];
	        this.upgrade_stage = source["upgrade_stage"];
	        this.upgrade_progress = source["upgrade_progress"];
	        this.upgrade_message = source["upgrade_message"];
	        this.upgrade_started_at = this.convertValues(source["upgrade_started_at"], null);
	        this.upgrade_updated_at = this.convertValues(source["upgrade_updated_at"], null);
	        this.upgrade_logs = this.convertValues(source["upgrade_logs"], UpgradeLogEntry);
	        this.launcher_version = source["launcher_version"];
	        this.autostart = this.convertValues(source["autostart"], LauncherAutostartInfo);
	        this.agents = this.convertValues(source["agents"], Agent);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Directory {
	    path: string;
	    name: string;
	    kind: string;
	    is_dir: boolean;
	    size?: number;
	
	    static createFrom(source: any = {}) {
	        return new Directory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.is_dir = source["is_dir"];
	        this.size = source["size"];
	    }
	}
	export class DirectoryListResult {
	    current_path: string;
	    parent_path?: string;
	    entries: Directory[];
	
	    static createFrom(source: any = {}) {
	        return new DirectoryListResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current_path = source["current_path"];
	        this.parent_path = source["parent_path"];
	        this.entries = this.convertValues(source["entries"], Directory);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DirectoryPermissionInput {
	    agent_id?: string;
	    path?: string;
	    request?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DirectoryPermissionInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent_id = source["agent_id"];
	        this.path = source["path"];
	        this.request = source["request"];
	    }
	}
	export class DirectoryPermissionResult {
	    platform: string;
	    path?: string;
	    accessible: boolean;
	    requires_user_action?: boolean;
	    action?: string;
	    message?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new DirectoryPermissionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platform = source["platform"];
	        this.path = source["path"];
	        this.accessible = source["accessible"];
	        this.requires_user_action = source["requires_user_action"];
	        this.action = source["action"];
	        this.message = source["message"];
	        this.error = source["error"];
	    }
	}
	export class DirectoryRequest {
	    path?: string;
	    allow_all?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DirectoryRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.allow_all = source["allow_all"];
	    }
	}
	
	export class LauncherSelfUpdateAsset {
	    platform: string;
	    filename: string;
	    url: string;
	    sha256?: string;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LauncherSelfUpdateAsset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platform = source["platform"];
	        this.filename = source["filename"];
	        this.url = source["url"];
	        this.sha256 = source["sha256"];
	        this.available = source["available"];
	    }
	}
	export class LauncherSelfUpdateResult {
	    current_version: string;
	    latest_version: string;
	    target_version?: string;
	    channel?: string;
	    platform: string;
	    update_available: boolean;
	    asset?: LauncherSelfUpdateAsset;
	    downloaded: boolean;
	    applied: boolean;
	    dry_run?: boolean;
	    staged_path?: string;
	    updater_path?: string;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new LauncherSelfUpdateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current_version = source["current_version"];
	        this.latest_version = source["latest_version"];
	        this.target_version = source["target_version"];
	        this.channel = source["channel"];
	        this.platform = source["platform"];
	        this.update_available = source["update_available"];
	        this.asset = this.convertValues(source["asset"], LauncherSelfUpdateAsset);
	        this.downloaded = source["downloaded"];
	        this.applied = source["applied"];
	        this.dry_run = source["dry_run"];
	        this.staged_path = source["staged_path"];
	        this.updater_path = source["updater_path"];
	        this.message = source["message"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MCPToolInfo {
	    id: string;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new MCPToolInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.description = source["description"];
	    }
	}
	export class MCPServerStatusInfo {
	    name: string;
	    status: string;
	    error?: string;
	    tools?: MCPToolInfo[];
	
	    static createFrom(source: any = {}) {
	        return new MCPServerStatusInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.tools = this.convertValues(source["tools"], MCPToolInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MCPStatusInfo {
	    agent_id?: string;
	    port?: number;
	    servers?: MCPServerStatusInfo[];
	
	    static createFrom(source: any = {}) {
	        return new MCPStatusInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent_id = source["agent_id"];
	        this.port = source["port"];
	        this.servers = this.convertValues(source["servers"], MCPServerStatusInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class RuntimeToolStatusInfo {
	    name: string;
	    path?: string;
	    version?: string;
	    source?: string;
	    status: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeToolStatusInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.version = source["version"];
	        this.source = source["source"];
	        this.status = source["status"];
	        this.error = source["error"];
	    }
	}
	export class RuntimeEnvironmentInfo {
	    runtime_dir?: string;
	    preparing?: boolean;
	    // Go type: time
	    updated_at?: any;
	    environment?: Record<string, string>;
	    tools?: RuntimeToolStatusInfo[];
	    logs?: string[];
	    last_error?: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeEnvironmentInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runtime_dir = source["runtime_dir"];
	        this.preparing = source["preparing"];
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.environment = source["environment"];
	        this.tools = this.convertValues(source["tools"], RuntimeToolStatusInfo);
	        this.logs = source["logs"];
	        this.last_error = source["last_error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SSHAuthorizedKeyResult {
	    installed: boolean;
	    already_exists?: boolean;
	    fingerprint?: string;
	    message?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHAuthorizedKeyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.already_exists = source["already_exists"];
	        this.fingerprint = source["fingerprint"];
	        this.message = source["message"];
	        this.error = source["error"];
	    }
	}
	export class SSHStatus {
	    platform: string;
	    installed: boolean;
	    running: boolean;
	    listening: boolean;
	    listen_address?: string;
	    host_key_fingerprint?: string;
	    message?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platform = source["platform"];
	        this.installed = source["installed"];
	        this.running = source["running"];
	        this.listening = source["listening"];
	        this.listen_address = source["listen_address"];
	        this.host_key_fingerprint = source["host_key_fingerprint"];
	        this.message = source["message"];
	        this.error = source["error"];
	    }
	}
	export class SSHSetupResult {
	    status: SSHStatus;
	    requires_user_action?: boolean;
	    action?: string;
	    output?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHSetupResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = this.convertValues(source["status"], SSHStatus);
	        this.requires_user_action = source["requires_user_action"];
	        this.action = source["action"];
	        this.output = source["output"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SetAgentEnabledInput {
	    agent_id: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SetAgentEnabledInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent_id = source["agent_id"];
	        this.enabled = source["enabled"];
	    }
	}

}

