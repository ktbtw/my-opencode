class AgentModel {
  final String agentId;
  final String name;
  final String projectId;
  final String projectRoot;
  final String projectScopeId;
  final String semanticAgentId;
  final String semanticAgentName;
  final String version;
  final String status;
  final bool enabled;
  final String? runningTaskId;
  final DateTime? lastSeen;

  const AgentModel({
    required this.agentId,
    this.name = '',
    required this.projectId,
    required this.projectRoot,
    this.projectScopeId = '',
    this.semanticAgentId = '',
    this.semanticAgentName = '',
    required this.version,
    required this.status,
    required this.enabled,
    this.runningTaskId,
    this.lastSeen,
  });

  factory AgentModel.fromJson(Map<String, dynamic> j) {
    // projects 数组取第一个
    final projects = j['projects'] as List<dynamic>? ?? [];
    final firstProject = projects.isNotEmpty
        ? projects[0] as Map<String, dynamic>
        : <String, dynamic>{};

    return AgentModel(
      agentId: j['agent_id'] as String? ?? '',
      name: j['name'] as String? ?? '',
      projectId:
          j['project_id'] as String? ??
          firstProject['project_id'] as String? ??
          '',
      projectRoot:
          j['project_root'] as String? ??
          j['project_dir'] as String? ??
          firstProject['root'] as String? ??
          '',
      projectScopeId: firstProject['project_scope_id'] as String? ?? '',
      semanticAgentId: j['semantic_agent_id'] as String? ?? '',
      semanticAgentName: j['semantic_agent_name'] as String? ?? '',
      version: j['version'] as String? ?? '',
      status: j['status'] as String? ?? 'online',
      enabled:
          j['enabled'] as bool? ??
          ((j['status'] as String?)?.toLowerCase() != 'disabled'),
      runningTaskId:
          j['current_task_id'] as String? ?? j['running_task_id'] as String?,
      lastSeen: j['seen_at'] != null
          ? DateTime.tryParse(j['seen_at'] as String)
          : j['last_seen'] != null
          ? DateTime.tryParse(j['last_seen'] as String)
          : null,
    );
  }

  String get normalizedStatus => status.trim().toLowerCase();
  String get displayName {
    final value = name.trim();
    if (value.isNotEmpty) return value;
    final normalizedRoot = projectRoot.trim().replaceAll('\\', '/');
    final segments = normalizedRoot
        .split('/')
        .where((segment) => segment.isNotEmpty)
        .toList(growable: false);
    if (segments.isNotEmpty) return segments.last;
    final project = projectId.trim();
    if (project.isNotEmpty) return project;
    final id = agentId.trim();
    return id.isEmpty ? 'Agent' : id;
  }

  bool get isDisabled => !enabled || normalizedStatus == 'disabled';
  bool get isTransitioning =>
      normalizedStatus == 'starting' ||
      normalizedStatus == 'restarting' ||
      normalizedStatus == 'upgrading';
  bool get isFailed => normalizedStatus == 'failed';
  bool get isStopped =>
      normalizedStatus == 'offline' ||
      normalizedStatus == 'stopped' ||
      normalizedStatus == 'failed';
  bool get isOnline =>
      !isDisabled && !isStopped && normalizedStatus != 'unknown';
  bool get isBusy =>
      !isDisabled &&
      isOnline &&
      (isTransitioning || (runningTaskId != null && runningTaskId!.isNotEmpty));
}

class DeviceModel {
  final String machineId;
  final String hostname;
  final String displayName;
  final int sortOrder;
  final bool online;
  final List<AgentModel> agents;
  final DateTime? lastSeen;

  const DeviceModel({
    required this.machineId,
    required this.hostname,
    this.displayName = '',
    this.sortOrder = 0,
    required this.online,
    required this.agents,
    this.lastSeen,
  });

  factory DeviceModel.fromJson(Map<String, dynamic> j) {
    final agentList = (j['agents'] as List<dynamic>? ?? [])
        .map((a) => AgentModel.fromJson(a as Map<String, dynamic>))
        .toList();

    // 后端返回 status: "online" 字符串，兼容布尔
    final statusRaw = j['status'];
    final bool online = statusRaw is bool ? statusRaw : statusRaw == 'online';

    return DeviceModel(
      machineId: j['machine_id'] as String? ?? '',
      hostname: j['hostname'] as String? ?? j['machine_id'] as String? ?? '',
      displayName: j['display_name'] as String? ?? '',
      sortOrder: (j['sort_order'] as num?)?.toInt() ?? 0,
      online: online,
      agents: agentList,
      lastSeen: j['seen_at'] != null
          ? DateTime.tryParse(j['seen_at'] as String)
          : j['last_seen'] != null
          ? DateTime.tryParse(j['last_seen'] as String)
          : null,
    );
  }

  int get runningAgentCount => agents.where((agent) => agent.isOnline).length;
  int get totalAgentCount => agents.length;
  int get activeTaskCount => agents.where((agent) => agent.isBusy).length;
  String get effectiveName {
    final custom = displayName.trim();
    if (custom.isNotEmpty) return custom;
    final host = hostname.trim();
    return host.isNotEmpty ? host : '未命名设备';
  }
}

class ModelTestTarget {
  final String agentId;
  final String projectId;

  const ModelTestTarget({required this.agentId, required this.projectId});
}

ModelTestTarget? resolveModelTestTarget({
  String preferredAgentId = '',
  String preferredProjectId = '',
  List<AgentModel> agents = const [],
}) {
  final agentId = preferredAgentId.trim();
  final projectId = preferredProjectId.trim();
  if (agentId.isNotEmpty && projectId.isNotEmpty) {
    return ModelTestTarget(agentId: agentId, projectId: projectId);
  }

  bool usable(AgentModel agent) {
    return agent.isOnline &&
        agent.agentId.trim().isNotEmpty &&
        agent.projectId.trim().isNotEmpty;
  }

  if (agentId.isNotEmpty) {
    for (final agent in agents) {
      if (agent.agentId.trim() == agentId && usable(agent)) {
        return ModelTestTarget(
          agentId: agent.agentId.trim(),
          projectId: agent.projectId.trim(),
        );
      }
    }
  }
  for (final agent in agents) {
    if (usable(agent)) {
      return ModelTestTarget(
        agentId: agent.agentId.trim(),
        projectId: agent.projectId.trim(),
      );
    }
  }
  return null;
}
