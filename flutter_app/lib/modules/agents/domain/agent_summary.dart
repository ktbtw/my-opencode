class AgentProject {
  const AgentProject({
    required this.projectId,
    required this.root,
  });

  factory AgentProject.fromJson(Map<String, dynamic> json) {
    return AgentProject(
      projectId: json['project_id'] as String? ?? '',
      root: json['root'] as String? ?? '',
    );
  }

  final String projectId;
  final String root;
}

class AgentSummary {
  const AgentSummary({
    required this.agentId,
    required this.machineId,
    required this.hostname,
    required this.version,
    required this.projects,
    required this.currentTaskId,
  });

  factory AgentSummary.fromJson(Map<String, dynamic> json) {
    final projectsJson = json['projects'] as List<dynamic>? ?? const [];
    return AgentSummary(
      agentId: json['agent_id'] as String? ?? '',
      machineId: json['machine_id'] as String? ?? '',
      hostname: json['hostname'] as String? ?? '',
      version: json['version'] as String? ?? '',
      projects: projectsJson
          .map((item) => AgentProject.fromJson(item as Map<String, dynamic>))
          .toList(),
      currentTaskId: json['current_task_id'] as String? ?? '',
    );
  }

  final String agentId;
  final String machineId;
  final String hostname;
  final String version;
  final List<AgentProject> projects;
  final String currentTaskId;
}
