class SessionSummary {
  SessionSummary({
    required this.sessionId,
    required this.agentId,
    required this.machineId,
    required this.projectId,
    required this.status,
    required this.lastTaskId,
    required this.summary,
    required this.createdAt,
    required this.updatedAt,
  });

  final String sessionId;
  final String agentId;
  final String machineId;
  final String projectId;
  final String status;
  final String lastTaskId;
  final String summary;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  factory SessionSummary.fromJson(Map<String, dynamic> json) {
    return SessionSummary(
      sessionId: json['session_id'] as String? ?? '',
      agentId: json['agent_id'] as String? ?? '',
      machineId: json['machine_id'] as String? ?? '',
      projectId: json['project_id'] as String? ?? '',
      status: json['status'] as String? ?? '',
      lastTaskId: json['last_task_id'] as String? ?? '',
      summary: json['summary'] as String? ?? '',
      createdAt: _parse(json['created_at'] as String?),
      updatedAt: _parse(json['updated_at'] as String?),
    );
  }

  static DateTime? _parse(String? value) {
    if (value == null || value.isEmpty) {
      return null;
    }
    return DateTime.tryParse(value)?.toLocal();
  }
}
