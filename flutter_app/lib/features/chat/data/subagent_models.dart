class SubagentNode {
  final String nodeId;
  final String planId;
  final String sessionId;
  final String childSessionId;
  final String role;
  final String resolvedAgent;
  final String title;
  final String state;
  final int attempt;
  final List<String> dependsOn;
  final List<String> blockedBy;
  final int? priority;
  final Map<String, dynamic> model;
  final List<String> pendingInstructions;
  final bool background;
  final int startedAt;
  final int completedAt;
  final String output;
  final List<Map<String, dynamic>> artifacts;
  final String error;
  final String wakeReason;
  final Map<String, dynamic> lastControl;

  const SubagentNode({
    required this.nodeId,
    this.planId = '',
    this.sessionId = '',
    this.childSessionId = '',
    this.role = '',
    this.resolvedAgent = '',
    this.title = '',
    this.state = 'unknown',
    this.attempt = 0,
    this.dependsOn = const [],
    this.blockedBy = const [],
    this.priority,
    this.model = const {},
    this.pendingInstructions = const [],
    this.background = false,
    this.startedAt = 0,
    this.completedAt = 0,
    this.output = '',
    this.artifacts = const [],
    this.error = '',
    this.wakeReason = '',
    this.lastControl = const {},
  });

  bool get isTerminal => const {
    'completed',
    'failed',
    'cancelled',
    'timed_out',
    'blocked',
  }.contains(state);

  bool get isRunning =>
      state == 'running' || state == 'queued' || state == 'recovering';

  bool get isControllable => !isTerminal && state != 'unknown';

  /// Older relay snapshots could contain a phantom `<nil>` node when the
  /// originating event omitted node_id. Keep legitimate recovery states, but
  /// never expose that placeholder as a task.
  bool get hasValidNodeId {
    final value = nodeId.trim().toLowerCase();
    return value.isNotEmpty &&
        value != '<nil>' &&
        value != 'nil' &&
        value != 'null';
  }

  DateTime? get startedAtDate =>
      startedAt > 0 ? DateTime.fromMillisecondsSinceEpoch(startedAt) : null;

  DateTime? get completedAtDate =>
      completedAt > 0 ? DateTime.fromMillisecondsSinceEpoch(completedAt) : null;

  factory SubagentNode.fromJson(Map<String, dynamic> json) => SubagentNode(
    nodeId: json['node_id'] as String? ?? '',
    planId: json['plan_id'] as String? ?? '',
    sessionId: json['session_id'] as String? ?? '',
    childSessionId: json['child_session_id'] as String? ?? '',
    role: json['role'] as String? ?? '',
    resolvedAgent: json['resolved_agent'] as String? ?? '',
    title: json['title'] as String? ?? '',
    state: json['state'] as String? ?? 'unknown',
    attempt: (json['attempt'] as num?)?.toInt() ?? 0,
    dependsOn: _stringList(json['depends_on']),
    blockedBy: _stringList(json['blocked_by']),
    priority: (json['priority'] as num?)?.toInt(),
    model: json['model'] is Map
        ? Map<String, dynamic>.from(json['model'] as Map)
        : const {},
    pendingInstructions: _stringList(json['pending_instructions']),
    background: json['background'] as bool? ?? false,
    startedAt: (json['started_at'] as num?)?.toInt() ?? 0,
    completedAt: (json['completed_at'] as num?)?.toInt() ?? 0,
    output: json['output'] as String? ?? '',
    artifacts: (json['artifacts'] as List<dynamic>? ?? const [])
        .whereType<Map>()
        .map((item) => Map<String, dynamic>.from(item))
        .toList(growable: false),
    error: json['error'] as String? ?? '',
    wakeReason: json['wake_reason'] as String? ?? '',
    lastControl: json['last_control'] is Map
        ? Map<String, dynamic>.from(json['last_control'] as Map)
        : const {},
  );
}

List<String> _stringList(Object? value) => (value as List<dynamic>? ?? const [])
    .whereType<String>()
    .where((item) => item.isNotEmpty)
    .toList(growable: false);

class SubagentTreeSnapshot {
  final String taskId;
  final List<SubagentNode> nodes;

  const SubagentTreeSnapshot({required this.taskId, this.nodes = const []});

  factory SubagentTreeSnapshot.fromJson(Map<String, dynamic> json) =>
      SubagentTreeSnapshot(
        taskId: json['task_id'] as String? ?? '',
        nodes: (json['nodes'] as List<dynamic>? ?? const [])
            .whereType<Map>()
            .map(
              (item) => SubagentNode.fromJson(Map<String, dynamic>.from(item)),
            )
            .where((node) => node.hasValidNodeId)
            .toList(growable: false),
      );
}

class SubagentLogEvent {
  final String type;
  final String content;
  final String error;
  final String sentAt;

  const SubagentLogEvent({
    this.type = '',
    this.content = '',
    this.error = '',
    this.sentAt = '',
  });

  factory SubagentLogEvent.fromJson(Map<String, dynamic> json) =>
      SubagentLogEvent(
        type: json['type'] as String? ?? '',
        content: json['content'] as String? ?? '',
        error: json['error'] as String? ?? '',
        sentAt: json['sent_at'] as String? ?? '',
      );
}
