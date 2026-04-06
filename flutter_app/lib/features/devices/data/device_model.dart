class AgentModel {
  final String agentId;
  final String projectId;
  final String projectRoot;
  final String version;
  final String? runningTaskId;
  final DateTime? lastSeen;

  const AgentModel({
    required this.agentId,
    required this.projectId,
    required this.projectRoot,
    required this.version,
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
      projectId: j['project_id'] as String? ??
          firstProject['project_id'] as String? ?? '',
      projectRoot: j['project_root'] as String? ??
          firstProject['root'] as String? ?? '',
      version: j['version'] as String? ?? '',
      runningTaskId: j['current_task_id'] as String? ?? j['running_task_id'] as String?,
      lastSeen: j['seen_at'] != null
          ? DateTime.tryParse(j['seen_at'] as String)
          : j['last_seen'] != null
              ? DateTime.tryParse(j['last_seen'] as String)
              : null,
    );
  }

  bool get isBusy =>
      runningTaskId != null && runningTaskId!.isNotEmpty;
}

class DeviceModel {
  final String machineId;
  final String hostname;
  final bool online;
  final List<AgentModel> agents;
  final DateTime? lastSeen;

  const DeviceModel({
    required this.machineId,
    required this.hostname,
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
    final bool online = statusRaw is bool
        ? statusRaw
        : statusRaw == 'online';

    return DeviceModel(
      machineId: j['machine_id'] as String? ?? '',
      hostname: j['hostname'] as String? ?? j['machine_id'] as String? ?? '',
      online: online,
      agents: agentList,
      lastSeen: j['seen_at'] != null
          ? DateTime.tryParse(j['seen_at'] as String)
          : j['last_seen'] != null
              ? DateTime.tryParse(j['last_seen'] as String)
              : null,
    );
  }
}
