import 'dart:typed_data';

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
  final String? userText;
  final ApprovalInfo? approval;
  final DateTime? createdAt;

  const TaskModel({
    required this.taskId,
    required this.agentId,
    required this.projectId,
    this.sessionId,
    required this.status,
    this.result,
    this.userText,
    this.approval,
    this.createdAt,
  });

  factory TaskModel.fromJson(Map<String, dynamic> j) {
    ApprovalInfo? approval;
    final approvalJson = j['approval'] as Map<String, dynamic>?;
    if (approvalJson != null) {
      approval = ApprovalInfo.fromJson(approvalJson);
    }
    // 从 parts 提取用户发的文本
    String? userText;
    final parts = j['parts'] as List<dynamic>?;
    if (parts != null) {
      final textParts = parts
          .whereType<Map<String, dynamic>>()
          .where((p) => p['type'] == 'text')
          .map((p) => p['text'] as String? ?? '')
          .where((t) => t.isNotEmpty)
          .toList();
      if (textParts.isNotEmpty) userText = textParts.join('\n');
    }
    return TaskModel(
      taskId: j['task_id'] as String? ?? '',
      agentId: j['agent_id'] as String? ?? '',
      projectId: j['project_id'] as String? ?? '',
      sessionId: j['session_id'] as String?,
      status: taskStatusFromString(j['status'] as String?),
      result: j['result'] as String?,
      userText: userText,
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
  String thinkingContent;
  final DateTime createdAt;
  final String? taskId;
  final List<AttachedFile> attachedFiles;
  // 统计信息
  int? tokenCount;
  Duration? firstTokenTime; // 从发送到收到首个 delta 的时间

  ChatMessage({
    required this.id,
    required this.role,
    required this.state,
    required this.content,
    this.thinkingContent = '',
    required this.createdAt,
    this.taskId,
    this.attachedFiles = const [],
    this.tokenCount,
    this.firstTokenTime,
  });
}

// 可用模型信息
class ModelInfo {
  final String providerID;
  final String modelID;
  final String name;

  const ModelInfo({
    required this.providerID,
    required this.modelID,
    required this.name,
  });

  // 格式化为 metadata 传输字段 "providerID/modelID"
  String get metaKey => '$providerID/$modelID';

  @override
  String toString() => name.isNotEmpty ? name : metaKey;
}

// 已选附件文件
class AttachedFile {
  final String filename;
  final String mimeType;
  final String base64Data; // 不含 data URI 前缀的纯 base64
  final int sizeBytes;

  AttachedFile({
    required this.filename,
    required this.mimeType,
    required this.base64Data,
    required this.sizeBytes,
  });

  // 懒加载解码后的图片字节，避免每次 build 重新解码导致图片闪烁
  Uint8List? _cachedBytes;
  Uint8List? get imageBytes {
    if (!isImage) return null;
    _cachedBytes ??= Uri.parse('data:$mimeType;base64,$base64Data').data?.contentAsBytes();
    return _cachedBytes;
  }

  // 构建 file part 用于 createTask
  Map<String, dynamic> toFilePart() => {
        'type': 'file',
        'mime': mimeType,
        'filename': filename,
        'url': 'data:$mimeType;base64,$base64Data',
      };

  bool get isImage => mimeType.startsWith('image/');
  bool get isPdf => mimeType == 'application/pdf';
}
