class SessionInfo {
  SessionInfo({
    required this.sessionId,
    required this.agentId,
    required this.projectId,
    required this.status,
    required this.summary,
    required this.updatedAt,
  });

  final String sessionId;
  final String agentId;
  final String projectId;
  final String status;
  final String summary;
  final DateTime? updatedAt;

  factory SessionInfo.fromJson(Map<String, dynamic> json) {
    return SessionInfo(
      sessionId: json['session_id'] as String? ?? '',
      agentId: json['agent_id'] as String? ?? '',
      projectId: json['project_id'] as String? ?? '',
      status: json['status'] as String? ?? '',
      summary: json['summary'] as String? ?? '',
      updatedAt: _parse(json['updated_at'] as String?),
    );
  }

  static DateTime? _parse(String? value) {
    if (value == null || value.isEmpty) return null;
    return DateTime.tryParse(value)?.toLocal();
  }
}

class TaskPart {
  TaskPart({
    required this.type,
    required this.text,
    required this.mime,
    required this.filename,
    required this.url,
  });

  final String type;
  final String text;
  final String mime;
  final String filename;
  final String url;

  Map<String, dynamic> toJson() {
    return {
      'type': type,
      if (text.isNotEmpty) 'text': text,
      if (mime.isNotEmpty) 'mime': mime,
      if (filename.isNotEmpty) 'filename': filename,
      if (url.isNotEmpty) 'url': url,
    };
  }

  factory TaskPart.fromJson(Map<String, dynamic> json) {
    return TaskPart(
      type: json['type'] as String? ?? '',
      text: json['text'] as String? ?? '',
      mime: json['mime'] as String? ?? '',
      filename: json['filename'] as String? ?? '',
      url: json['url'] as String? ?? '',
    );
  }
}

class ChatTurn {
  ChatTurn({
    required this.taskId,
    required this.sessionId,
    required this.status,
    required this.result,
    required this.error,
    required this.parts,
    required this.createdAt,
    required this.updatedAt,
    required this.permissionId,
    required this.permission,
    required this.patterns,
  });

  final String taskId;
  final String sessionId;
  final String status;
  final String result;
  final String error;
  final List<TaskPart> parts;
  final DateTime? createdAt;
  final DateTime? updatedAt;
  final String permissionId;
  final String permission;
  final List<String> patterns;

  String get prompt {
    for (final part in parts) {
      if (part.type == 'text' && part.text.trim().isNotEmpty) {
        return part.text.trim();
      }
    }
    return '';
  }

  factory ChatTurn.fromJson(Map<String, dynamic> json) {
    final approval = json['approval'] as Map<String, dynamic>?;
    return ChatTurn(
      taskId: json['task_id'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
      status: json['status'] as String? ?? '',
      result: json['result'] as String? ?? '',
      error: json['error'] as String? ?? '',
      createdAt: SessionInfo._parse(json['created_at'] as String?),
      updatedAt: SessionInfo._parse(json['updated_at'] as String?),
      parts: (json['parts'] as List<dynamic>? ?? const [])
          .whereType<Map<String, dynamic>>()
          .map(TaskPart.fromJson)
          .toList(),
      permissionId: approval?['permission_id'] as String? ?? '',
      permission: approval?['permission'] as String? ?? '',
      patterns: (approval?['patterns'] as List<dynamic>? ?? const [])
          .whereType<String>()
          .toList(),
    );
  }
}
