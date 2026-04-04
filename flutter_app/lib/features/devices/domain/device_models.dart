class AgentProject {
  AgentProject({
    required this.projectId,
    required this.root,
  });

  final String projectId;
  final String root;

  factory AgentProject.fromJson(Map<String, dynamic> json) {
    return AgentProject(
      projectId: json['project_id'] as String? ?? '',
      root: json['root'] as String? ?? '',
    );
  }
}

class AgentInfo {
  AgentInfo({
    required this.agentId,
    required this.machineId,
    required this.hostname,
    required this.version,
    required this.projects,
    required this.currentTaskId,
  });

  final String agentId;
  final String machineId;
  final String hostname;
  final String version;
  final List<AgentProject> projects;
  final String currentTaskId;

  factory AgentInfo.fromJson(Map<String, dynamic> json) {
    return AgentInfo(
      agentId: json['agent_id'] as String? ?? '',
      machineId: json['machine_id'] as String? ?? '',
      hostname: json['hostname'] as String? ?? '',
      version: json['version'] as String? ?? '',
      currentTaskId: json['current_task_id'] as String? ?? '',
      projects: (json['projects'] as List<dynamic>? ?? const [])
          .whereType<Map<String, dynamic>>()
          .map(AgentProject.fromJson)
          .toList(),
    );
  }
}

class DeviceInfo {
  DeviceInfo({
    required this.machineId,
    required this.hostname,
    required this.status,
    required this.seenAt,
    required this.agents,
  });

  final String machineId;
  final String hostname;
  final String status;
  final DateTime? seenAt;
  final List<AgentInfo> agents;

  factory DeviceInfo.fromJson(Map<String, dynamic> json) {
    return DeviceInfo(
      machineId: json['machine_id'] as String? ?? '',
      hostname: json['hostname'] as String? ?? '',
      status: json['status'] as String? ?? '',
      seenAt: _parse(json['seen_at'] as String?),
      agents: (json['agents'] as List<dynamic>? ?? const [])
          .whereType<Map<String, dynamic>>()
          .map(AgentInfo.fromJson)
          .toList(),
    );
  }

  static DateTime? _parse(String? value) {
    if (value == null || value.isEmpty) return null;
    return DateTime.tryParse(value)?.toLocal();
  }
}
