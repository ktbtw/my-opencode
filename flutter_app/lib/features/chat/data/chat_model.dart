import 'dart:typed_data';

enum TaskStatus {
  dispatched,
  started,
  running,
  cancelling,
  waitingApproval,
  completed,
  failed,
  cancelled,
  unknown,
}

TaskStatus taskStatusFromString(String? s) => switch (s) {
  'dispatched' => TaskStatus.dispatched,
  'started' => TaskStatus.started,
  'running' => TaskStatus.running,
  'cancelling' => TaskStatus.cancelling,
  'waiting_approval' => TaskStatus.waitingApproval,
  'completed' => TaskStatus.completed,
  'failed' => TaskStatus.failed,
  'cancelled' => TaskStatus.cancelled,
  _ => TaskStatus.unknown,
};

List<ChatMessage> chronologicalChatMessages(List<ChatMessage> messages) {
  final indexed = [
    for (var index = 0; index < messages.length; index++)
      (index: index, message: messages[index]),
  ];
  int compareIndexed(
    ({int index, ChatMessage message}) left,
    ({int index, ChatMessage message}) right,
  ) {
    final leftTaskId = left.message.taskId?.trim() ?? '';
    final rightTaskId = right.message.taskId?.trim() ?? '';
    final leftTaskCreatedAt = left.message.taskCreatedAt;
    final rightTaskCreatedAt = right.message.taskCreatedAt;

    // Event timestamps describe work inside a task. They must not move a
    // later round of an older task past a newer task in the conversation.
    if (leftTaskCreatedAt != null &&
        rightTaskCreatedAt != null &&
        leftTaskId.isNotEmpty &&
        rightTaskId.isNotEmpty) {
      final taskTimeCompare = leftTaskCreatedAt.compareTo(rightTaskCreatedAt);
      if (taskTimeCompare != 0) return taskTimeCompare;
      final taskIdCompare = leftTaskId.compareTo(rightTaskId);
      if (taskIdCompare != 0) return taskIdCompare;
      final leftIndex = left.message.taskMessageIndex;
      final rightIndex = right.message.taskMessageIndex;
      if (leftIndex != null && rightIndex != null) {
        final indexCompare = leftIndex.compareTo(rightIndex);
        if (indexCompare != 0) return indexCompare;
      }
    } else if (leftTaskId.isNotEmpty && leftTaskId == rightTaskId) {
      final leftIndex = left.message.taskMessageIndex;
      final rightIndex = right.message.taskMessageIndex;
      if (leftIndex != null && rightIndex != null) {
        final indexCompare = leftIndex.compareTo(rightIndex);
        if (indexCompare != 0) return indexCompare;
      }
    }

    final timeCompare = left.message.createdAt.compareTo(
      right.message.createdAt,
    );
    return timeCompare != 0 ? timeCompare : left.index.compareTo(right.index);
  }

  for (var index = 1; index < indexed.length; index++) {
    if (compareIndexed(indexed[index - 1], indexed[index]) > 0) {
      indexed.sort(compareIndexed);
      return indexed.map((entry) => entry.message).toList(growable: false);
    }
  }
  return messages;
}

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

class QuestionOptionInfo {
  final String label;
  final String description;

  const QuestionOptionInfo({required this.label, required this.description});

  factory QuestionOptionInfo.fromJson(Map<String, dynamic> j) {
    return QuestionOptionInfo(
      label: j['label'] as String? ?? '',
      description: j['description'] as String? ?? '',
    );
  }
}

class QuestionItemInfo {
  final String question;
  final String header;
  final List<QuestionOptionInfo> options;
  final bool multiple;
  final bool custom;

  const QuestionItemInfo({
    required this.question,
    required this.header,
    required this.options,
    required this.multiple,
    required this.custom,
  });

  factory QuestionItemInfo.fromJson(Map<String, dynamic> j) {
    return QuestionItemInfo(
      question: j['question'] as String? ?? '',
      header: j['header'] as String? ?? '',
      options: (j['options'] as List<dynamic>? ?? [])
          .whereType<Map<String, dynamic>>()
          .map(QuestionOptionInfo.fromJson)
          .toList(),
      multiple: j['multiple'] as bool? ?? false,
      custom: j['custom'] as bool? ?? true,
    );
  }
}

class QuestionRequestInfo {
  final String requestId;
  final String? sessionId;
  final List<QuestionItemInfo> questions;

  const QuestionRequestInfo({
    required this.requestId,
    this.sessionId,
    required this.questions,
  });

  factory QuestionRequestInfo.fromJson(Map<String, dynamic> j) {
    return QuestionRequestInfo(
      requestId: j['request_id'] as String? ?? '',
      sessionId: j['session_id'] as String?,
      questions: (j['questions'] as List<dynamic>? ?? [])
          .whereType<Map<String, dynamic>>()
          .map(QuestionItemInfo.fromJson)
          .toList(),
    );
  }
}

class PlanItemInfo {
  final String id;
  final String text;
  final String status;
  final String priority;

  const PlanItemInfo({
    required this.id,
    required this.text,
    required this.status,
    required this.priority,
  });

  factory PlanItemInfo.fromJson(Map<String, dynamic> j) {
    return PlanItemInfo(
      id: j['id'] as String? ?? '',
      text: j['text'] as String? ?? '',
      status: j['status'] as String? ?? 'pending',
      priority: j['priority'] as String? ?? '',
    );
  }
}

class PlanInfo {
  final String id;
  final String title;
  final String mode;
  final String status;
  final String? sessionId;
  final List<PlanItemInfo> items;

  const PlanInfo({
    required this.id,
    required this.title,
    required this.mode,
    required this.status,
    this.sessionId,
    this.items = const [],
  });

  bool get isEmpty => items.isEmpty;

  factory PlanInfo.fromJson(Map<String, dynamic> j) {
    return PlanInfo(
      id: j['id'] as String? ?? '',
      title: j['title'] as String? ?? '执行计划',
      mode: j['mode'] as String? ?? '',
      status: j['status'] as String? ?? 'pending',
      sessionId: j['session_id'] as String?,
      items: (j['items'] as List<dynamic>? ?? [])
          .whereType<Map<String, dynamic>>()
          .map(PlanItemInfo.fromJson)
          .toList(),
    );
  }
}

class MCPToolInfo {
  final String id;
  final String description;

  const MCPToolInfo({required this.id, required this.description});

  factory MCPToolInfo.fromJson(Map<String, dynamic> j) {
    return MCPToolInfo(
      id: j['id'] as String? ?? '',
      description: j['description'] as String? ?? '',
    );
  }
}

class MCPServerStatusInfo {
  final String name;
  final String status;
  final String error;
  final bool enabled;
  final List<MCPToolInfo> tools;

  const MCPServerStatusInfo({
    required this.name,
    required this.status,
    required this.error,
    this.enabled = true,
    required this.tools,
  });

  bool get visibleInChat => enabled && status.toLowerCase() != 'disabled';

  factory MCPServerStatusInfo.fromJson(Map<String, dynamic> j) {
    return MCPServerStatusInfo(
      name: j['name'] as String? ?? '',
      status: j['status'] as String? ?? 'unknown',
      error: j['error'] as String? ?? '',
      enabled: j['enabled'] as bool? ?? true,
      tools: (j['tools'] as List<dynamic>? ?? const [])
          .whereType<Map<String, dynamic>>()
          .map(MCPToolInfo.fromJson)
          .toList(),
    );
  }
}

class MCPStatusInfo {
  final String agentId;
  final int port;
  final List<MCPServerStatusInfo> servers;

  const MCPStatusInfo({
    required this.agentId,
    required this.port,
    required this.servers,
  });

  factory MCPStatusInfo.fromJson(Map<String, dynamic> j) {
    return MCPStatusInfo(
      agentId: j['agent_id'] as String? ?? '',
      port: j['port'] as int? ?? 0,
      servers: (j['servers'] as List<dynamic>? ?? const [])
          .whereType<Map<String, dynamic>>()
          .map(MCPServerStatusInfo.fromJson)
          .toList(),
    );
  }
}

class ModelLatencyLogEntry {
  final DateTime at;
  final String stage;
  final String message;
  final Map<String, dynamic> data;

  const ModelLatencyLogEntry({
    required this.at,
    required this.stage,
    required this.message,
    this.data = const {},
  });
}

class ModelLatencyTestResult {
  final String taskId;
  final String sessionId;
  final String model;
  final String? variant;
  final DateTime startedAt;
  final DateTime? completedAt;
  final bool success;
  final String error;
  final Duration? createTaskTime;
  final Duration? firstEventTime;
  final Duration? firstReasoningTime;
  final Duration? firstTextTime;
  final Duration? totalTime;
  final int reasoningLength;
  final int textLength;
  final int? tokenCount;
  final List<ModelLatencyLogEntry> logs;

  const ModelLatencyTestResult({
    required this.taskId,
    required this.sessionId,
    required this.model,
    this.variant,
    required this.startedAt,
    this.completedAt,
    required this.success,
    this.error = '',
    this.createTaskTime,
    this.firstEventTime,
    this.firstReasoningTime,
    this.firstTextTime,
    this.totalTime,
    this.reasoningLength = 0,
    this.textLength = 0,
    this.tokenCount,
    this.logs = const [],
  });
}

String buildPlanHistorySignature(PlanInfo plan) {
  final itemsSignature = plan.items
      .map((item) => '${item.id}|${item.text}|${item.status}|${item.priority}')
      .join('||');
  return '${plan.id}|${plan.title}|${plan.mode}|${plan.status}|${plan.sessionId ?? ''}|$itemsSignature';
}

String _normalizePlanHistoryValue(String value) {
  return value.trim().replaceAll(RegExp(r'\s+'), ' ');
}

String buildPlanHistoryBatchKey(PlanInfo plan) {
  if (plan.isEmpty) return '';
  final id = _normalizePlanHistoryValue(plan.id);
  if (id.isEmpty) return '';
  final sessionId = _normalizePlanHistoryValue(plan.sessionId ?? '');
  return 'id|$sessionId|$id';
}

String buildPlanHistoryRevisionKey(PlanInfo plan) {
  final itemsSignature = plan.items
      .asMap()
      .entries
      .map((entry) {
        final item = entry.value;
        return [
          _normalizePlanHistoryValue(item.id),
          _normalizePlanHistoryValue(item.text),
          _normalizePlanHistoryValue(item.status),
          _normalizePlanHistoryValue(item.priority),
        ].join('|');
      })
      .join('||');
  return [
    buildPlanHistoryBatchKey(plan),
    _normalizePlanHistoryValue(plan.status),
    itemsSignature,
  ].join('|');
}

String buildPlanHistoryContentKey(PlanInfo plan) {
  return buildPlanHistoryRevisionKey(plan);
}

class PlanHistorySnapshot {
  final DateTime capturedAt;
  final PlanInfo plan;

  const PlanHistorySnapshot({required this.capturedAt, required this.plan});

  String get batchKey => buildPlanHistoryBatchKey(plan);
  String get dedupeKey => buildPlanHistoryRevisionKey(plan);
}

List<PlanHistorySnapshot> mergePlanHistorySnapshotsByBatch(
  List<PlanHistorySnapshot> current,
  List<PlanHistorySnapshot> incoming,
) {
  if (current.isEmpty && incoming.isEmpty) return const [];
  final combined = [...current, ...incoming]
    ..sort((a, b) {
      final compare = a.capturedAt.compareTo(b.capturedAt);
      if (compare != 0) return compare;
      return a.dedupeKey.compareTo(b.dedupeKey);
    });
  final latestByBatch = <String, PlanHistorySnapshot>{};
  for (final snapshot in combined) {
    if (snapshot.plan.isEmpty) continue;
    final batchKey = snapshot.batchKey;
    if (batchKey.isEmpty) continue;
    final existing = latestByBatch[batchKey];
    if (existing == null ||
        !snapshot.capturedAt.isBefore(existing.capturedAt)) {
      latestByBatch[batchKey] = snapshot;
    }
  }
  final merged = latestByBatch.values.toList()
    ..sort((a, b) {
      final compare = a.capturedAt.compareTo(b.capturedAt);
      if (compare != 0) return compare;
      return a.batchKey.compareTo(b.batchKey);
    });
  return merged;
}

class ToolCallInfo {
  final String id;
  final String callId;
  final String tool;
  final String status;
  final Map<String, dynamic> input;
  final String title;
  final String output;
  final bool outputTruncated;
  final String error;
  final Map<String, dynamic> metadata;
  final DateTime? startedAt;
  final DateTime? endedAt;
  final List<AiOutputFile> attachments;

  const ToolCallInfo({
    required this.id,
    required this.callId,
    required this.tool,
    required this.status,
    this.input = const {},
    this.title = '',
    this.output = '',
    this.outputTruncated = false,
    this.error = '',
    this.metadata = const {},
    this.startedAt,
    this.endedAt,
    this.attachments = const [],
  });

  bool get isRunning => status == 'pending' || status == 'running';
  bool get isCompleted => status == 'completed';
  bool get isError => status == 'error';
  bool get isTerminal => isCompleted || isError;
  bool get isPlanOnly => tool == 'todowrite';
  String get stableKey => callId.isNotEmpty ? callId : id;
  int get statusProgress => switch (status) {
    'error' || 'completed' => 2,
    'running' => 1,
    _ => 0,
  };

  bool sameIdentity(ToolCallInfo other) {
    final call = callId.trim();
    final otherCall = other.callId.trim();
    final selfId = id.trim();
    final otherId = other.id.trim();
    if (selfId.isNotEmpty && selfId == otherId) return true;
    if (selfId.isNotEmpty && selfId == otherCall) return true;
    if (otherId.isNotEmpty && otherId == call) return true;
    if (call.isNotEmpty && call == otherCall) {
      return selfId.isEmpty || otherId.isEmpty || selfId == otherId;
    }
    return false;
  }

  ToolCallInfo coalescedWith(ToolCallInfo incoming) {
    if (incoming.statusProgress < statusProgress) return this;
    return incoming;
  }

  ToolCallInfo copyWith({String? status, DateTime? endedAt}) {
    return ToolCallInfo(
      id: id,
      callId: callId,
      tool: tool,
      status: status ?? this.status,
      input: input,
      title: title,
      output: output,
      outputTruncated: outputTruncated,
      error: error,
      metadata: metadata,
      startedAt: startedAt,
      endedAt: endedAt ?? this.endedAt,
      attachments: attachments,
    );
  }

  factory ToolCallInfo.fromJson(Map<String, dynamic> j) {
    final metadata = j['metadata'];
    final endedAt = _dateFromMillis(j['ended_at']);
    final rawStatus = (j['status'] as String?)?.trim() ?? '';
    final error = j['error'] as String? ?? '';
    return ToolCallInfo(
      id: j['id'] as String? ?? '',
      callId: j['call_id'] as String? ?? '',
      tool: j['tool'] as String? ?? '',
      status: rawStatus.isNotEmpty
          ? rawStatus
          : error.trim().isNotEmpty
          ? 'error'
          : endedAt != null
          ? 'completed'
          : 'running',
      input: _asStringMap(j['input']),
      title: j['title'] as String? ?? '',
      output: j['output'] as String? ?? '',
      outputTruncated: j['output_truncated'] == true,
      error: error,
      metadata: _asStringMap(metadata),
      startedAt: _dateFromMillis(j['started_at']),
      endedAt: endedAt,
      attachments: (j['attachments'] as List<dynamic>? ?? const [])
          .whereType<Map<String, dynamic>>()
          .map(AiOutputFile.fromJson)
          .toList(),
    );
  }

  static Map<String, dynamic> _asStringMap(Object? value) {
    if (value is Map<String, dynamic>) return value;
    if (value is Map) return Map<String, dynamic>.from(value);
    return const {};
  }

  static DateTime? _dateFromMillis(Object? value) {
    final millis = value is num
        ? value.toInt()
        : value is String
        ? int.tryParse(value)
        : null;
    if (millis == null || millis <= 0) return null;
    return DateTime.fromMillisecondsSinceEpoch(millis);
  }
}

bool isSubagentToolCall(ToolCallInfo tool) {
  if (tool.tool == 'subagent') return true;
  return tool.tool == 'task' &&
      (tool.input['subagent_type']?.toString().trim().isNotEmpty ?? false);
}

/// User-facing names for orchestration tools. Keep protocol identifiers out of
/// the conversation surface while retaining them in the event/model layer.
String displayToolName(String tool) {
  return switch (tool.trim()) {
    'task_status' => '查看进度',
    'orchestrate' => '安排协作',
    'task' => '协作处理',
    'job' => '后台处理',
    '' => '工具',
    final value => value,
  };
}

String displayToolTitle(ToolCallInfo tool) {
  if (tool.tool == 'task_status') {
    if (tool.isError) return '进度获取失败';
    if (tool.isRunning) return '正在查看进度';
    return '进度已更新';
  }
  return tool.title.trim();
}

String displaySubagentRole(String role) {
  return switch (role.trim()) {
    'repo-explorer' => '仓库探索',
    'implementation-planner' => '方案整理',
    'test-runner' => '测试执行',
    'code-reviewer' => '代码审查',
    'dependency-researcher' => '依赖调研',
    'apk-scout' => '应用分析',
    'manifest-mapper' => '结构分析',
    'native-tracer' => '原生调用追踪',
    'protocol-analyst' => '协议分析',
    'verification-runner' => '验证执行',
    'explore' => '探索分析',
    'general' => '综合处理',
    'researcher' => '技术研究',
    'planner' => '任务规划',
    'build-runner' => '构建执行',
    '' => '子代理',
    _ => '专项处理',
  };
}

/// The upstream tool is named `task`, but it is a delegated child-agent call
/// whenever subagent_type is present. Normalize it before it reaches the UI.
ToolCallInfo normalizeSubagentToolCall(ToolCallInfo tool) {
  if (!isSubagentToolCall(tool)) return tool;
  final role = (tool.input['role']?.toString().trim().isNotEmpty ?? false)
      ? tool.input['role'].toString().trim()
      : tool.input['subagent_type']?.toString().trim() ?? '';
  final title = tool.title.trim().isNotEmpty
      ? tool.title.trim()
      : (tool.input['description']?.toString().trim().isNotEmpty ?? false)
      ? tool.input['description'].toString().trim()
      : tool.input['prompt']?.toString().trim() ?? '';
  final metadata = {
    ...tool.metadata,
    // A direct task tool call is the durable start record. Relay result
    // events use the separate `result` phase so both lifecycle moments remain
    // visible in the parent conversation.
    'phase': tool.metadata['phase']?.toString().trim().isNotEmpty == true
        ? tool.metadata['phase']
        : 'start',
  };
  return ToolCallInfo(
    id: tool.id,
    callId: tool.callId,
    tool: 'subagent',
    status: tool.status,
    input: {...tool.input, 'role': role},
    title: title,
    output: tool.output,
    outputTruncated: tool.outputTruncated,
    error: tool.error,
    metadata: metadata,
    startedAt: tool.startedAt,
    endedAt: tool.endedAt,
    attachments: tool.attachments,
  );
}

/// Normalizes relay subagent events into the same ordered block stream used by
/// ordinary tool calls. The result is only marked completed when its input is
/// actually applied to the parent Agent task.
ToolCallInfo subagentToolCallFromMetadata({
  required String taskId,
  required Map<String, dynamic> metadata,
  String status = 'running',
  String output = '',
  String error = '',
}) {
  final nodeId = metadata['node_id']?.toString().trim() ?? '';
  final role = metadata['subagent_type']?.toString().trim() ?? '';
  final title = (metadata['title']?.toString().trim().isNotEmpty ?? false)
      ? metadata['title'].toString().trim()
      : (metadata['prompt']?.toString().trim() ?? '');
  final rawState = status.trim().toLowerCase();
  final normalizedStatus =
      const {
        'failed',
        'timed_out',
        'cancelled',
        'blocked',
        'error',
      }.contains(rawState)
      ? 'error'
      : rawState == 'completed'
      ? 'completed'
      : 'running';
  final startedAt = _subagentEventDate(metadata['started_at']);
  final endedAt = _subagentEventDate(metadata['completed_at']);
  final resolvedError = error.trim().isNotEmpty
      ? error.trim()
      : metadata['error']?.toString().trim() ?? '';

  final phase = (metadata['phase']?.toString().trim().isNotEmpty ?? false)
      ? metadata['phase'].toString().trim()
      : 'start';
  // Keep the relay's live state distinct from the terminal result. Collapsing
  // every non-start phase into `result` makes a just-started child look done.
  final phaseKey = switch (phase) {
    'running' => 'running',
    'result' => 'result',
    _ => 'start',
  };
  return ToolCallInfo(
    id: 'subagent_${taskId}_${nodeId}_$phaseKey',
    callId: 'subagent_${taskId}_${nodeId}_$phaseKey',
    tool: 'subagent',
    status: normalizedStatus,
    title: title,
    input: {
      'role': role,
      'resolved_agent': metadata['resolved_agent']?.toString().trim() ?? '',
      'node_id': nodeId,
    },
    output: output,
    error: resolvedError,
    metadata: {
      ...metadata,
      'task_id': taskId,
      'node_id': nodeId,
      'phase': phaseKey,
    },
    startedAt: startedAt,
    endedAt: endedAt,
  );
}

/// Retains the original model tool-call identity while enriching it with relay
/// state (node id, lifecycle and the display title).
ToolCallInfo mergeSubagentToolCall(ToolCallInfo existing, ToolCallInfo latest) {
  final previous = normalizeSubagentToolCall(existing);
  return ToolCallInfo(
    id: previous.id,
    callId: previous.callId,
    tool: 'subagent',
    status: latest.status,
    input: {...previous.input, ...latest.input},
    title: latest.title.trim().isNotEmpty ? latest.title : previous.title,
    output: latest.output.isNotEmpty ? latest.output : previous.output,
    outputTruncated: latest.outputTruncated || previous.outputTruncated,
    error: latest.error.isNotEmpty ? latest.error : previous.error,
    metadata: {...previous.metadata, ...latest.metadata},
    startedAt: previous.startedAt ?? latest.startedAt,
    endedAt: latest.endedAt ?? previous.endedAt,
    attachments: latest.attachments.isNotEmpty
        ? latest.attachments
        : previous.attachments,
  );
}

DateTime? _subagentEventDate(Object? value) {
  final millis = value is num
      ? value.toInt()
      : value is String
      ? int.tryParse(value.trim())
      : null;
  if (millis == null || millis <= 0) return null;
  return DateTime.fromMillisecondsSinceEpoch(millis);
}

enum ChatMessageBlockType { text, tool }

class ChatMessageBlock {
  final String id;
  final ChatMessageBlockType type;
  final String text;
  final ToolCallInfo? tool;

  const ChatMessageBlock({
    required this.id,
    required this.type,
    this.text = '',
    this.tool,
  });

  const ChatMessageBlock.text({required this.id, required this.text})
    : type = ChatMessageBlockType.text,
      tool = null;

  const ChatMessageBlock.tool({required this.id, required this.tool})
    : type = ChatMessageBlockType.tool,
      text = '';

  bool get isText => type == ChatMessageBlockType.text;
  bool get isTool => type == ChatMessageBlockType.tool && tool != null;

  ChatMessageBlock copyWith({String? text, ToolCallInfo? tool}) {
    return ChatMessageBlock(
      id: id,
      type: type,
      text: text ?? this.text,
      tool: tool ?? this.tool,
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

enum ChatQueueItemStatus { queued, dispatching, dispatched, inserting, unknown }

ChatQueueItemStatus chatQueueItemStatusFromString(String? value) =>
    switch (value) {
      'queued' => ChatQueueItemStatus.queued,
      'dispatching' => ChatQueueItemStatus.dispatching,
      'dispatched' => ChatQueueItemStatus.dispatched,
      'inserting' => ChatQueueItemStatus.inserting,
      _ => ChatQueueItemStatus.unknown,
    };

class ChatQueueItem {
  final String id;
  final String sessionId;
  final String agentId;
  final String machineId;
  final String projectId;
  final List<Map<String, dynamic>> parts;
  final Map<String, String> metadata;
  final int position;
  final ChatQueueItemStatus status;
  final int version;
  final String taskId;
  final int injectionVersion;
  final String error;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  const ChatQueueItem({
    required this.id,
    required this.sessionId,
    required this.agentId,
    this.machineId = '',
    required this.projectId,
    this.parts = const [],
    this.metadata = const {},
    this.position = 0,
    this.status = ChatQueueItemStatus.unknown,
    this.version = 0,
    this.taskId = '',
    this.injectionVersion = 0,
    this.error = '',
    this.createdAt,
    this.updatedAt,
  });

  String get text => parts
      .where((part) => part['type'] == 'text')
      .map((part) => part['text']?.toString() ?? '')
      .where((value) => value.isNotEmpty)
      .join('\n');

  int get attachmentCount =>
      parts.where((part) => part['type'] == 'file').length;
  String get modelRef => metadata['model'] ?? '';
  String get variant => metadata['variant'] ?? '';
  bool get isEditable => status == ChatQueueItemStatus.queued;

  factory ChatQueueItem.fromJson(Map<String, dynamic> json) {
    final rawMetadata = json['metadata'];
    final metadata = <String, String>{};
    if (rawMetadata is Map) {
      for (final entry in rawMetadata.entries) {
        metadata[entry.key.toString()] = entry.value.toString();
      }
    }
    return ChatQueueItem(
      id: json['queue_item_id'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
      agentId: json['agent_id'] as String? ?? '',
      machineId: json['machine_id'] as String? ?? '',
      projectId: json['project_id'] as String? ?? '',
      parts: (json['parts'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map((part) => Map<String, dynamic>.from(part))
          .toList(growable: false),
      metadata: Map<String, String>.unmodifiable(metadata),
      position: (json['position'] as num?)?.toInt() ?? 0,
      status: chatQueueItemStatusFromString(json['status'] as String?),
      version: (json['version'] as num?)?.toInt() ?? 0,
      taskId: json['task_id'] as String? ?? '',
      injectionVersion: (json['injection_version'] as num?)?.toInt() ?? 0,
      error: json['error'] as String? ?? '',
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? ''),
      updatedAt: DateTime.tryParse(json['updated_at'] as String? ?? ''),
    );
  }
}

class ChatQueueSnapshot {
  final String sessionId;
  final int version;
  final List<ChatQueueItem> items;

  const ChatQueueSnapshot({
    this.sessionId = '',
    this.version = 0,
    this.items = const [],
  });

  List<ChatQueueItem> get queuedItems => items
      .where(
        (item) =>
            item.status == ChatQueueItemStatus.queued ||
            item.status == ChatQueueItemStatus.inserting,
      )
      .toList(growable: false);

  factory ChatQueueSnapshot.fromJson(Map<String, dynamic> json) {
    return ChatQueueSnapshot(
      sessionId: json['session_id'] as String? ?? '',
      version: (json['version'] as num?)?.toInt() ?? 0,
      items: (json['items'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map(
            (item) => ChatQueueItem.fromJson(Map<String, dynamic>.from(item)),
          )
          .toList(growable: false),
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
  final String? error;
  final String? userText;
  final List<AiArtifact> artifacts;
  final ApprovalInfo? approval;
  final QuestionRequestInfo? question;
  final PlanInfo? plan;
  final DateTime? createdAt;

  const TaskModel({
    required this.taskId,
    required this.agentId,
    required this.projectId,
    this.sessionId,
    required this.status,
    this.result,
    this.error,
    this.userText,
    this.artifacts = const [],
    this.approval,
    this.question,
    this.plan,
    this.createdAt,
  });

  factory TaskModel.fromJson(Map<String, dynamic> j) {
    ApprovalInfo? approval;
    QuestionRequestInfo? question;
    PlanInfo? plan;
    final approvalJson = j['approval'] as Map<String, dynamic>?;
    if (approvalJson != null) {
      approval = ApprovalInfo.fromJson(approvalJson);
    }
    final questionJson = j['question'] as Map<String, dynamic>?;
    if (questionJson != null) {
      question = QuestionRequestInfo.fromJson(questionJson);
    }
    final planJson = j['plan'] as Map<String, dynamic>?;
    if (planJson != null) {
      plan = PlanInfo.fromJson(planJson);
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
    final artifacts = (j['artifacts'] as List<dynamic>? ?? [])
        .whereType<Map<String, dynamic>>()
        .map(AiArtifact.fromJson)
        .toList();
    return TaskModel(
      taskId: j['task_id'] as String? ?? '',
      agentId: j['agent_id'] as String? ?? '',
      projectId: j['project_id'] as String? ?? '',
      sessionId: j['session_id'] as String?,
      status: taskStatusFromString(j['status'] as String?),
      result: j['result'] as String?,
      error: j['error'] as String?,
      userText: userText,
      artifacts: artifacts,
      approval: approval,
      question: question,
      plan: plan,
      createdAt: j['created_at'] != null
          ? DateTime.tryParse(j['created_at'] as String)
          : null,
    );
  }
}

class TaskEventRoundSnapshot {
  final String text;
  final String reasoning;
  final String statusHint;
  final List<GoalProgressEntry> goalProgress;
  final List<PlanHistorySnapshot> planHistory;
  final List<ToolCallInfo> toolCalls;
  final List<ChatMessageBlock> blocks;
  final String errorDetail;
  final DateTime? startedAt;
  final int? tokenCount;

  const TaskEventRoundSnapshot({
    this.text = '',
    this.reasoning = '',
    this.statusHint = '',
    this.goalProgress = const [],
    this.planHistory = const [],
    this.toolCalls = const [],
    this.blocks = const [],
    this.errorDetail = '',
    this.startedAt,
    this.tokenCount,
  });

  bool get hasGoalProgress => goalProgress.isNotEmpty;

  bool get hasOutput =>
      text.isNotEmpty ||
      reasoning.isNotEmpty ||
      statusHint.isNotEmpty ||
      goalProgress.isNotEmpty ||
      planHistory.isNotEmpty ||
      toolCalls.isNotEmpty ||
      blocks.isNotEmpty ||
      errorDetail.isNotEmpty;
}

class TaskInputAppliedInfo {
  final String queueItemId;
  final String content;
  final int injectionVersion;
  final DateTime? appliedAt;
  final int afterRoundIndex;
  final String contextSource;
  final String contextType;
  final Map<String, dynamic> contextMetadata;

  const TaskInputAppliedInfo({
    required this.queueItemId,
    required this.content,
    this.injectionVersion = 0,
    this.appliedAt,
    this.afterRoundIndex = 0,
    this.contextSource = 'user_queue',
    this.contextType = 'queueInsert',
    this.contextMetadata = const <String, dynamic>{},
  });
}

class ContextUsageInfo {
  final String stage;
  final String providerID;
  final String modelID;
  final String agent;
  final int? estimatedInputTokens;
  final int? estimatedUserInputTokens;
  final int? contextLimit;
  final int? contextTokens;
  final int? compactionCountTokens;
  final double? contextUsagePercent;
  final int? compactionThresholdTokens;
  final double? compactionThresholdPercent;
  final int? inputTokens;
  final int? outputTokens;
  final int? reasoningTokens;
  final int? cacheReadTokens;
  final int? cacheWriteTokens;
  final int? totalTokens;
  final String finishReason;
  final DateTime? updatedAt;

  const ContextUsageInfo({
    this.stage = '',
    this.providerID = '',
    this.modelID = '',
    this.agent = '',
    this.estimatedInputTokens,
    this.estimatedUserInputTokens,
    this.contextLimit,
    this.contextTokens,
    this.compactionCountTokens,
    this.contextUsagePercent,
    this.compactionThresholdTokens,
    this.compactionThresholdPercent,
    this.inputTokens,
    this.outputTokens,
    this.reasoningTokens,
    this.cacheReadTokens,
    this.cacheWriteTokens,
    this.totalTokens,
    this.finishReason = '',
    this.updatedAt,
  });

  int get knownContextTokens {
    if (compactionCountTokens != null && compactionCountTokens! > 0) {
      return compactionCountTokens!;
    }
    if (contextTokens != null && contextTokens! > 0) return contextTokens!;
    return 0;
  }

  ContextUsageInfo merge(ContextUsageInfo next) {
    return ContextUsageInfo(
      stage: next.stage.isNotEmpty ? next.stage : stage,
      providerID: next.providerID.isNotEmpty ? next.providerID : providerID,
      modelID: next.modelID.isNotEmpty ? next.modelID : modelID,
      agent: next.agent.isNotEmpty ? next.agent : agent,
      estimatedInputTokens: null,
      estimatedUserInputTokens: null,
      contextLimit: next.contextLimit ?? contextLimit,
      contextTokens: next.contextTokens ?? contextTokens,
      compactionCountTokens:
          next.compactionCountTokens ?? compactionCountTokens,
      contextUsagePercent: next.contextUsagePercent ?? contextUsagePercent,
      compactionThresholdTokens:
          next.compactionThresholdTokens ?? compactionThresholdTokens,
      compactionThresholdPercent:
          next.compactionThresholdPercent ?? compactionThresholdPercent,
      inputTokens: next.inputTokens ?? inputTokens,
      outputTokens: next.outputTokens ?? outputTokens,
      reasoningTokens: next.reasoningTokens ?? reasoningTokens,
      cacheReadTokens: next.cacheReadTokens ?? cacheReadTokens,
      cacheWriteTokens: next.cacheWriteTokens ?? cacheWriteTokens,
      totalTokens: next.totalTokens ?? totalTokens,
      finishReason: next.finishReason.isNotEmpty
          ? next.finishReason
          : finishReason,
      updatedAt: next.updatedAt ?? updatedAt,
    );
  }

  static ContextUsageInfo? fromProgressEvent(Map<String, dynamic> event) {
    if (event['type'] != 'progress') return null;
    final metadata = _asMap(event['metadata']);
    if (metadata['source'] != 'llm_usage') return null;
    return fromUsageMetadata(
      metadata,
      updatedAt: event['sent_at'] != null
          ? DateTime.tryParse(event['sent_at'] as String)
          : null,
    );
  }

  static ContextUsageInfo? fromUsageMetadata(
    Map<String, dynamic> metadata, {
    DateTime? updatedAt,
  }) {
    if (metadata.isEmpty) return null;
    return ContextUsageInfo(
      stage: metadata['stage'] as String? ?? '',
      providerID: metadata['provider_id'] as String? ?? '',
      modelID: metadata['model_id'] as String? ?? '',
      agent: metadata['agent'] as String? ?? '',
      estimatedInputTokens: null,
      estimatedUserInputTokens: null,
      contextLimit: _intValue(metadata['context_limit']),
      contextTokens: _intValue(metadata['context_tokens']),
      compactionCountTokens: _intValue(metadata['compaction_count_tokens']),
      contextUsagePercent: _doubleValue(metadata['context_usage_percent']),
      compactionThresholdTokens: _intValue(
        metadata['compaction_threshold_tokens'],
      ),
      compactionThresholdPercent: _doubleValue(
        metadata['compaction_threshold_percent'],
      ),
      inputTokens: _intValue(metadata['input_tokens']),
      outputTokens: _intValue(metadata['output_tokens']),
      reasoningTokens: _intValue(metadata['reasoning_tokens']),
      cacheReadTokens: _intValue(metadata['cache_read_tokens']),
      cacheWriteTokens: _intValue(metadata['cache_write_tokens']),
      totalTokens: _intValue(metadata['total_tokens']),
      finishReason: metadata['finish_reason'] as String? ?? '',
      updatedAt: updatedAt,
    );
  }

  static Map<String, dynamic> _asMap(Object? value) {
    if (value is Map<String, dynamic>) return value;
    if (value is Map) return Map<String, dynamic>.from(value);
    return const {};
  }

  static int? _intValue(Object? value) {
    if (value is int) return value;
    if (value is num) return value.round();
    if (value is String) return int.tryParse(value);
    return null;
  }

  static double? _doubleValue(Object? value) {
    if (value is double) return value;
    if (value is num) return value.toDouble();
    if (value is String) return double.tryParse(value);
    return null;
  }
}

class TaskEventSnapshot {
  final String text;
  final String reasoning;
  final String statusHint;
  final List<GoalProgressEntry> goalProgress;
  final List<PlanHistorySnapshot> planHistory;
  final List<ToolCallInfo> toolCalls;
  final List<ChatMessageBlock> blocks;
  final String errorDetail;
  final List<TaskEventRoundSnapshot> rounds;
  final ContextUsageInfo? contextUsage;
  final List<TaskInputAppliedInfo> appliedInputs;
  final FinalDeliveryPhase finalDeliveryPhase;

  const TaskEventSnapshot({
    this.text = '',
    this.reasoning = '',
    this.statusHint = '',
    this.goalProgress = const [],
    this.planHistory = const [],
    this.toolCalls = const [],
    this.blocks = const [],
    this.errorDetail = '',
    this.rounds = const [],
    this.contextUsage,
    this.appliedInputs = const [],
    this.finalDeliveryPhase = FinalDeliveryPhase.idle,
  });
}

// 消息气泡（前端渲染模型）
enum MessageRole { user, agent }

enum MessageState { sending, streaming, done, failed }

enum FinalDeliveryPhase { idle, validating }

FinalDeliveryPhase finalDeliveryPhaseFromProgressEvent(
  Map<String, dynamic> event,
) {
  if (event['type'] != 'progress') return FinalDeliveryPhase.idle;
  final rawMetadata = event['metadata'];
  final metadata = rawMetadata is Map
      ? Map<String, dynamic>.from(rawMetadata)
      : const <String, dynamic>{};
  if (metadata['source'] == 'completion_guard' &&
      metadata['reason'] == 'missing_artifact') {
    return FinalDeliveryPhase.validating;
  }
  return FinalDeliveryPhase.idle;
}

class GoalProgressEntry {
  final String type;
  final String detail;
  final String reason;
  final int iteration;
  final int max;
  final DateTime? sentAt;

  const GoalProgressEntry({
    required this.type,
    this.detail = '',
    this.reason = '',
    this.iteration = 0,
    this.max = 0,
    this.sentAt,
  });

  bool get isTerminal =>
      type == 'goal_completed' ||
      type == 'goal_paused' ||
      type == 'goal_failed';

  bool get hasDetail => detail.trim().isNotEmpty;

  String get title {
    switch (type) {
      case 'goal_created':
        return '目标已启动';
      case 'goal_continued':
        return '继续执行目标';
      case 'goal_checkpoint':
        return '保存目标进度';
      case 'goal_completed':
        return '目标已完成';
      case 'goal_paused':
        return '目标已暂停';
      case 'goal_failed':
        return '目标执行失败';
      default:
        return '目标状态更新';
    }
  }

  String get summary {
    final suffix = max > 0 ? ' $iteration/$max' : '';
    if (reason.trim().isNotEmpty) return '$title$suffix：${reason.trim()}';
    if (detail.trim().isNotEmpty) return '$title$suffix：${detail.trim()}';
    return '$title$suffix';
  }

  static GoalProgressEntry fromEvent(Map<String, dynamic> event) {
    final metadata = _asMap(event['metadata']);
    final nested = _asMap(metadata['metadata']);
    final type = event['type'] as String? ?? '';
    final detail = _firstString([
      nested['summary'],
      nested['checkpoint_summary'],
      nested['checkpoint'],
      metadata['summary'],
      metadata['checkpoint'],
    ]);
    return GoalProgressEntry(
      type: type,
      detail: detail,
      reason: _firstString([event['reason'], metadata['reason']]),
      iteration:
          (event['iteration'] as num?)?.toInt() ??
          (metadata['iteration'] as num?)?.toInt() ??
          0,
      max:
          (event['max'] as num?)?.toInt() ??
          (metadata['max'] as num?)?.toInt() ??
          0,
      sentAt: event['sent_at'] != null
          ? DateTime.tryParse(event['sent_at'] as String)
          : null,
    );
  }

  static Map<String, dynamic> _asMap(Object? value) {
    if (value is Map<String, dynamic>) return value;
    if (value is Map) return Map<String, dynamic>.from(value);
    return const {};
  }

  static String _firstString(List<Object?> values) {
    for (final value in values) {
      if (value is String && value.trim().isNotEmpty) return value.trim();
    }
    return '';
  }
}

class AiArtifact {
  final String id;
  final String filename;
  final String mimeType;
  final int sizeBytes;
  final String relativePath;

  const AiArtifact({
    required this.id,
    required this.filename,
    required this.mimeType,
    required this.sizeBytes,
    required this.relativePath,
  });

  factory AiArtifact.fromJson(Map<String, dynamic> j) {
    return AiArtifact(
      id: j['id'] as String? ?? '',
      filename: j['filename'] as String? ?? '未命名文件',
      mimeType: j['mime'] as String? ?? 'application/octet-stream',
      sizeBytes: (j['size_bytes'] as num?)?.toInt() ?? 0,
      relativePath: j['relative_path'] as String? ?? '',
    );
  }

  bool get isImage => mimeType.startsWith('image/');
}

class AiOutputFile {
  final String id;
  final String filename;
  final String mimeType;
  final String url;

  AiOutputFile({
    required this.id,
    required this.filename,
    required this.mimeType,
    required this.url,
  });

  factory AiOutputFile.fromJson(Map<String, dynamic> j) {
    return AiOutputFile(
      id: j['id'] as String? ?? '',
      filename: j['filename'] as String? ?? '未命名文件',
      mimeType: j['mime'] as String? ?? 'application/octet-stream',
      url: j['url'] as String? ?? '',
    );
  }

  Uint8List? _cachedBytes;

  bool get isImage => mimeType.startsWith('image/');
  bool get isDataUrl => url.startsWith('data:');

  Uint8List? get imageBytes {
    if (!isImage || !isDataUrl) return null;
    _cachedBytes ??= Uri.tryParse(url)?.data?.contentAsBytes();
    return _cachedBytes;
  }
}

class ChatMessage {
  final String id;
  final MessageRole role;
  MessageState state;
  String content;
  String thinkingContent;
  String statusHint;
  String errorDetail;
  FinalDeliveryPhase finalDeliveryPhase;
  ImageGenerationInfo? imageGeneration;
  List<GoalProgressEntry> goalProgress;
  final DateTime createdAt;
  final String? taskId;
  final DateTime? taskCreatedAt;
  final int? taskMessageIndex;
  final List<AttachedFile> attachedFiles;

  /// True when this user-role message was injected into an active task.
  /// It is rendered as a task-context node instead of a new conversation bubble.
  final bool isInsertedContext;
  final String insertedContextSource;
  final String insertedContextType;
  final Map<String, dynamic> insertedContextMetadata;
  List<ToolCallInfo> toolCalls;
  List<ChatMessageBlock> blocks;
  List<AiArtifact> artifacts;
  List<AiOutputFile> files;
  PlanInfo? plan;
  List<PlanHistorySnapshot> planHistory;
  bool hasPlanBinding;
  // 统计信息
  int? tokenCount;
  Duration? firstTokenTime; // 从发送到收到首个 delta 的时间

  ChatMessage({
    required this.id,
    required this.role,
    required this.state,
    required this.content,
    this.thinkingContent = '',
    this.statusHint = '',
    this.errorDetail = '',
    this.finalDeliveryPhase = FinalDeliveryPhase.idle,
    this.imageGeneration,
    this.goalProgress = const [],
    required this.createdAt,
    this.taskId,
    this.taskCreatedAt,
    this.taskMessageIndex,
    this.attachedFiles = const [],
    this.isInsertedContext = false,
    this.insertedContextSource = 'user_queue',
    this.insertedContextType = 'queueInsert',
    this.insertedContextMetadata = const <String, dynamic>{},
    this.toolCalls = const [],
    this.blocks = const [],
    this.artifacts = const [],
    this.files = const [],
    this.plan,
    this.planHistory = const [],
    this.hasPlanBinding = false,
    this.tokenCount,
    this.firstTokenTime,
  });

  bool get isFinalizingDelivery =>
      finalDeliveryPhase == FinalDeliveryPhase.validating;

  /// Creates an independent mutable shell for state updates. Chat messages
  /// are updated incrementally while the previous ChatState is still used by
  /// Riverpod listeners, so sharing the mutable object makes old/new surface
  /// comparisons observe the already-mutated value.
  ChatMessage copyForUpdate() {
    return ChatMessage(
      id: id,
      role: role,
      state: state,
      content: content,
      thinkingContent: thinkingContent,
      statusHint: statusHint,
      errorDetail: errorDetail,
      finalDeliveryPhase: finalDeliveryPhase,
      imageGeneration: imageGeneration,
      goalProgress: List<GoalProgressEntry>.from(goalProgress),
      createdAt: createdAt,
      taskId: taskId,
      taskCreatedAt: taskCreatedAt,
      taskMessageIndex: taskMessageIndex,
      attachedFiles: List<AttachedFile>.from(attachedFiles),
      isInsertedContext: isInsertedContext,
      insertedContextSource: insertedContextSource,
      insertedContextType: insertedContextType,
      insertedContextMetadata: Map<String, dynamic>.from(
        insertedContextMetadata,
      ),
      toolCalls: List<ToolCallInfo>.from(toolCalls),
      blocks: List<ChatMessageBlock>.from(blocks),
      artifacts: List<AiArtifact>.from(artifacts),
      files: List<AiOutputFile>.from(files),
      plan: plan,
      planHistory: List<PlanHistorySnapshot>.from(planHistory),
      hasPlanBinding: hasPlanBinding,
      tokenCount: tokenCount,
      firstTokenTime: firstTokenTime,
    );
  }
}

class ImageGenerationInfo {
  final String providerID;
  final String modelID;
  final String modelName;
  final DateTime startedAt;

  const ImageGenerationInfo({
    required this.providerID,
    required this.modelID,
    required this.modelName,
    required this.startedAt,
  });

  factory ImageGenerationInfo.fromModel(ModelInfo model, DateTime startedAt) {
    return ImageGenerationInfo(
      providerID: model.providerID,
      modelID: model.modelID,
      modelName: model.name.isNotEmpty ? model.name : model.metaKey,
      startedAt: startedAt,
    );
  }
}

// 可用模型信息
class ModelInfo {
  final String providerID;
  final String providerBaseUrl;
  final String providerConsoleUrl;
  final String modelID;
  final String name;
  final List<String> variants; // 可选的思考强度级别，如 ['low','medium','high']
  final bool image;
  final List<String> inputModalities;
  final List<String> outputModalities;
  final int? contextLimit;

  const ModelInfo({
    required this.providerID,
    this.providerBaseUrl = '',
    this.providerConsoleUrl = '',
    required this.modelID,
    required this.name,
    this.variants = const [],
    this.image = false,
    this.inputModalities = const [],
    this.outputModalities = const [],
    this.contextLimit,
  });

  bool get hasVariants => variants.isNotEmpty;
  bool get supportsImageOutput =>
      image ||
      outputModalities.any((item) => item.toLowerCase() == 'image') ||
      _looksLikeImageGenerationModel;

  bool get _looksLikeImageGenerationModel {
    final text = '$providerID $modelID $name'.toLowerCase();
    return text.contains('gpt-image') ||
        text.contains('dall-e') ||
        text.contains('imagen') ||
        text.contains('flux') ||
        text.contains('recraft') ||
        text.contains('grok-imagine') ||
        text.contains('stable-diffusion') ||
        text.contains('sdxl');
  }

  // 格式化为 metadata 传输字段 "providerID/modelID"
  String get metaKey => '$providerID/$modelID';

  @override
  String toString() => name.isNotEmpty ? name : metaKey;
}

// 已选附件文件
class AttachedFile {
  final String id;
  final String filename;
  final String mimeType;
  final String base64Data; // 不含 data URI 前缀的纯 base64
  final int sizeBytes;
  final String? inlineToken;

  AttachedFile({
    required this.id,
    required this.filename,
    required this.mimeType,
    required this.base64Data,
    required this.sizeBytes,
    this.inlineToken,
  });

  // 懒加载解码后的图片字节，避免每次 build 重新解码导致图片闪烁
  Uint8List? _cachedBytes;
  Uint8List? get imageBytes {
    if (!isImage) return null;
    _cachedBytes ??= Uri.parse(
      'data:$mimeType;base64,$base64Data',
    ).data?.contentAsBytes();
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
