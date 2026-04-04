enum TaskStatus {
  dispatched,
  started,
  running,
  waitingApproval,
  completed,
  failed,
  unknown,
}

TaskStatus taskStatusFromString(String? s) => switch (s) {
      'dispatched' => TaskStatus.dispatched,
      'started' => TaskStatus.started,
      'running' => TaskStatus.running,
      'waiting_approval' => TaskStatus.waitingApproval,
      'completed' => TaskStatus.completed,
      'failed' => TaskStatus.failed,
      _ => TaskStatus.unknown,
    };

class ApprovalInfo {
  final String permissionId;
  final String permission;
  final List<String> patterns;
  final Map<String, dynamic> metadata;

  const ApprovalInfo({
    required this.permissionId,
    required this.permission,
    required this.patterns,
    required this.metadata,
  });

  factory ApprovalInfo.fromJson(Map<String, dynamic> j) {
    return ApprovalInfo(
      permissionId: j['permission_id'] as String? ?? '',
      permission: j['permission'] as String? ?? '',
      patterns: (j['patterns'] as List<dynamic>? ?? [])
          .map((e) => e.toString())
          .toList(),
      metadata: j['metadata'] as Map<String, dynamic>? ?? {},
    );
  }
}

class SessionModel {
  final String sessionId;
  final String agentId;
  final String projectId;
  final String? summary;
  final String? lastTaskId;
  final DateTime? updatedAt;

  const SessionModel({
    required this.sessionId,
    required this.agentId,
    required this.projectId,
    this.summary,
    this.lastTaskId,
    this.updatedAt,
  });

  factory SessionModel.fromJson(Map<String, dynamic> j) {
    return SessionModel(
      sessionId: j['session_id'] as String? ?? '',
      agentId: j['agent_id'] as String? ?? '',
      projectId: j['project_id'] as String? ?? '',
      summary: j['summary'] as String?,
      lastTaskId: j['last_task_id'] as String?,
      updatedAt: j['updated_at'] != null
          ? DateTime.tryParse(j['updated_at'] as String)
          : null,
    );
  }
}

class TaskModel {
  final String taskId;
  final String agentId;
  final String projectId;
  final String? sessionId;
  final TaskStatus status;
  final String? result;
  final ApprovalInfo? approval;
  final DateTime? createdAt;

  const TaskModel({
    required this.taskId,
    required this.agentId,
    required this.projectId,
    this.sessionId,
    required this.status,
    this.result,
    this.approval,
    this.createdAt,
  });

  factory TaskModel.fromJson(Map<String, dynamic> j) {
    ApprovalInfo? approval;
    final approvalJson = j['approval'] as Map<String, dynamic>?;
    if (approvalJson != null) {
      approval = ApprovalInfo.fromJson(approvalJson);
    }
    return TaskModel(
      taskId: j['task_id'] as String? ?? '',
      agentId: j['agent_id'] as String? ?? '',
      projectId: j['project_id'] as String? ?? '',
      sessionId: j['session_id'] as String?,
      status: taskStatusFromString(j['status'] as String?),
      result: j['result'] as String?,
      approval: approval,
      createdAt: j['created_at'] != null
          ? DateTime.tryParse(j['created_at'] as String)
          : null,
    );
  }
}

// 消息气泡（前端渲染模型）
enum MessageRole { user, agent }
enum MessageState { sending, streaming, done, failed }

class ChatMessage {
  final String id;
  final MessageRole role;
  MessageState state;
  String content;
  final DateTime createdAt;
  final String? taskId;

  ChatMessage({
    required this.id,
    required this.role,
    required this.state,
    required this.content,
    required this.createdAt,
    this.taskId,
  });
}
