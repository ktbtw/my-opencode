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
    return AgentModel(
      agentId: j['agent_id'] as String? ?? '',
      projectId: j['project_id'] as String? ?? '',
      projectRoot: j['project_root'] as String? ?? '',
      version: j['version'] as String? ?? '',
      runningTaskId: j['running_task_id'] as String?,
      lastSeen: j['last_seen'] != null
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

    return DeviceModel(
      machineId: j['machine_id'] as String? ?? '',
      hostname: j['hostname'] as String? ?? j['machine_id'] as String? ?? '',
      online: j['online'] as bool? ?? false,
      agents: agentList,
      lastSeen: j['last_seen'] != null
          ? DateTime.tryParse(j['last_seen'] as String)
          : null,
    );
  }
}
