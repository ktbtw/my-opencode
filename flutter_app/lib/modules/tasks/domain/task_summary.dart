class TaskSummary {
  TaskSummary({
    required this.taskId,
    required this.agentId,
    required this.machineId,
    required this.projectId,
    required this.projectRoot,
    required this.sessionId,
    required this.status,
    required this.result,
    required this.error,
    required this.parts,
    required this.createdAt,
    required this.updatedAt,
  });

  final String taskId;
  final String agentId;
  final String machineId;
  final String projectId;
  final String projectRoot;
  final String sessionId;
  final String status;
  final String result;
  final String error;
  final List<TaskPartSummary> parts;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  String get primaryPrompt {
    for (final part in parts) {
      if (part.type == 'text' && part.text.trim().isNotEmpty) {
        return part.text.trim();
      }
    }
    return '无文本输入';
  }

  factory TaskSummary.fromJson(Map<String, dynamic> json) {
    final rawParts = (json['parts'] as List<dynamic>? ?? const [])
        .whereType<Map<String, dynamic>>()
        .map(TaskPartSummary.fromJson)
        .toList();

    return TaskSummary(
      taskId: json['task_id'] as String? ?? '',
      agentId: json['agent_id'] as String? ?? '',
      machineId: json['machine_id'] as String? ?? '',
      projectId: json['project_id'] as String? ?? '',
      projectRoot: json['project_root'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
      status: json['status'] as String? ?? 'unknown',
      result: json['result'] as String? ?? '',
      error: json['error'] as String? ?? '',
      parts: rawParts,
      createdAt: _parseDateTime(json['created_at'] as String?),
      updatedAt: _parseDateTime(json['updated_at'] as String?),
    );
  }

  static DateTime? _parseDateTime(String? raw) {
    if (raw == null || raw.isEmpty) {
      return null;
    }
    return DateTime.tryParse(raw)?.toLocal();
  }
}

class TaskPartSummary {
  TaskPartSummary({
    required this.type,
    required this.text,
    required this.filename,
    required this.mime,
  });

  final String type;
  final String text;
  final String filename;
  final String mime;

  factory TaskPartSummary.fromJson(Map<String, dynamic> json) {
    return TaskPartSummary(
      type: json['type'] as String? ?? '',
      text: json['text'] as String? ?? '',
      filename: json['filename'] as String? ?? '',
      mime: json['mime'] as String? ?? '',
    );
  }
}
