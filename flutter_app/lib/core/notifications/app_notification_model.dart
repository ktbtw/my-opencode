enum AppNotificationStatus {
  pending,
  running,
  waitingSync,
  succeeded,
  failed,
  cancelled,
}

enum AppNotificationProgressMode { none, determinate, indeterminate }

enum AppNotificationDisplayStyle {
  automatic,
  compact,
  linear,
  ring,
  stages,
  sync,
  dots,
}

enum AppNotificationKind { agent, mcp, installation, file, system }

enum AppNotificationScope { local, synced }

enum AppNotificationAttention { none, critical, userAction }

enum AppNotificationActionType {
  openRoute,
  retryOperation,
  cancelOperation,
  openLogs,
}

enum AppNotificationCenterTab { active, recent }

class AppNotificationChatContext {
  final String machineId;
  final String agentId;
  final String projectId;

  const AppNotificationChatContext({
    required this.machineId,
    required this.agentId,
    required this.projectId,
  });

  bool sameAs(AppNotificationChatContext other) =>
      machineId == other.machineId &&
      agentId == other.agentId &&
      projectId == other.projectId;

  bool matchesCompletion(AppNotificationRecord record) {
    if (record.kind != AppNotificationKind.agent ||
        record.status != AppNotificationStatus.succeeded ||
        !record.operationId.startsWith('chat-task:')) {
      return false;
    }

    String value(Map<String, String> values, String key) =>
        values[key]?.trim() ?? '';

    final metadataMachine = value(record.metadata, 'machine_id');
    final metadataAgent = value(record.metadata, 'agent_id');
    final metadataProject = value(record.metadata, 'project_id');
    if (metadataMachine.isNotEmpty &&
        metadataAgent.isNotEmpty &&
        metadataProject.isNotEmpty) {
      return metadataMachine == machineId &&
          metadataAgent == agentId &&
          metadataProject == projectId;
    }

    // Older records have no metadata. Their route still carries the chat target.
    for (final action in record.actions) {
      final route = action.payload['route']?.trim() ?? '';
      final uri = Uri.tryParse(route);
      if (uri == null || uri.path != '/chat') continue;
      if (uri.queryParameters['machineId'] == machineId &&
          uri.queryParameters['agentId'] == agentId &&
          uri.queryParameters['projectId'] == projectId) {
        return true;
      }
    }
    return false;
  }
}

class AppNotificationAction {
  final String label;
  final AppNotificationActionType type;
  final Map<String, String> payload;
  final bool primary;

  const AppNotificationAction({
    required this.label,
    required this.type,
    this.payload = const {},
    this.primary = false,
  });

  Map<String, dynamic> toJson() => {
    'label': label,
    'type': type.name,
    'payload': payload,
    'primary': primary,
  };

  factory AppNotificationAction.fromJson(Map<String, dynamic> json) {
    return AppNotificationAction(
      label: json['label'] as String? ?? '',
      type: _enumByName(
        AppNotificationActionType.values,
        json['type'],
        AppNotificationActionType.openRoute,
      ),
      payload:
          (json['payload'] as Map?)?.map(
            (key, value) => MapEntry(key.toString(), value.toString()),
          ) ??
          const {},
      primary: json['primary'] as bool? ?? false,
    );
  }
}

class AppNotificationStage {
  final String label;
  final bool completed;
  final bool active;

  const AppNotificationStage({
    required this.label,
    this.completed = false,
    this.active = false,
  });

  Map<String, dynamic> toJson() => {
    'label': label,
    'completed': completed,
    'active': active,
  };

  factory AppNotificationStage.fromJson(Map<String, dynamic> json) {
    return AppNotificationStage(
      label: json['label'] as String? ?? '',
      completed: json['completed'] as bool? ?? false,
      active: json['active'] as bool? ?? false,
    );
  }
}

class AppNotificationRecord {
  final String id;
  final String operationId;
  final String title;
  final String message;
  final AppNotificationStatus status;
  final AppNotificationProgressMode progressMode;
  final AppNotificationDisplayStyle displayStyle;
  final AppNotificationKind kind;
  final AppNotificationScope scope;
  final AppNotificationAttention attention;
  final double? progress;
  final List<AppNotificationStage> stages;
  final List<AppNotificationAction> actions;
  final Map<String, String> metadata;
  final String sourceLabel;
  final String errorCode;
  final DateTime createdAt;
  final DateTime updatedAt;
  final DateTime? completedAt;
  final bool unread;

  const AppNotificationRecord({
    required this.id,
    required this.operationId,
    required this.title,
    required this.message,
    required this.status,
    required this.createdAt,
    required this.updatedAt,
    this.progressMode = AppNotificationProgressMode.none,
    this.displayStyle = AppNotificationDisplayStyle.automatic,
    this.kind = AppNotificationKind.system,
    this.scope = AppNotificationScope.local,
    this.attention = AppNotificationAttention.none,
    this.progress,
    this.stages = const [],
    this.actions = const [],
    this.metadata = const {},
    this.sourceLabel = '',
    this.errorCode = '',
    this.completedAt,
    this.unread = true,
  });

  bool get isTerminal => switch (status) {
    AppNotificationStatus.succeeded ||
    AppNotificationStatus.failed ||
    AppNotificationStatus.cancelled => true,
    _ => false,
  };

  double? get normalizedProgress => progress?.clamp(0, 1).toDouble();

  AppNotificationRecord copyWith({
    String? id,
    String? operationId,
    String? title,
    String? message,
    AppNotificationStatus? status,
    AppNotificationProgressMode? progressMode,
    AppNotificationDisplayStyle? displayStyle,
    AppNotificationKind? kind,
    AppNotificationScope? scope,
    AppNotificationAttention? attention,
    Object? progress = _unset,
    List<AppNotificationStage>? stages,
    List<AppNotificationAction>? actions,
    Map<String, String>? metadata,
    String? sourceLabel,
    String? errorCode,
    DateTime? createdAt,
    DateTime? updatedAt,
    Object? completedAt = _unset,
    bool? unread,
  }) {
    return AppNotificationRecord(
      id: id ?? this.id,
      operationId: operationId ?? this.operationId,
      title: title ?? this.title,
      message: message ?? this.message,
      status: status ?? this.status,
      progressMode: progressMode ?? this.progressMode,
      displayStyle: displayStyle ?? this.displayStyle,
      kind: kind ?? this.kind,
      scope: scope ?? this.scope,
      attention: attention ?? this.attention,
      progress: identical(progress, _unset)
          ? this.progress
          : progress as double?,
      stages: stages ?? this.stages,
      actions: actions ?? this.actions,
      metadata: metadata ?? this.metadata,
      sourceLabel: sourceLabel ?? this.sourceLabel,
      errorCode: errorCode ?? this.errorCode,
      createdAt: createdAt ?? this.createdAt,
      updatedAt: updatedAt ?? this.updatedAt,
      completedAt: identical(completedAt, _unset)
          ? this.completedAt
          : completedAt as DateTime?,
      unread: unread ?? this.unread,
    );
  }

  Map<String, dynamic> toJson() => {
    'id': id,
    'operation_id': operationId,
    'title': title,
    'message': message,
    'status': status.name,
    'progress_mode': progressMode.name,
    'display_style': displayStyle.name,
    'kind': kind.name,
    'scope': scope.name,
    'attention': attention.name,
    'progress': progress,
    'stages': stages.map((stage) => stage.toJson()).toList(),
    'actions': actions.map((action) => action.toJson()).toList(),
    'metadata': metadata,
    'source_label': sourceLabel,
    'error_code': errorCode,
    'created_at': createdAt.toUtc().toIso8601String(),
    'updated_at': updatedAt.toUtc().toIso8601String(),
    'completed_at': completedAt?.toUtc().toIso8601String(),
    'unread': unread,
  };

  factory AppNotificationRecord.fromJson(Map<String, dynamic> json) {
    final now = DateTime.now().toUtc();
    return AppNotificationRecord(
      id: json['id'] as String? ?? '',
      operationId: json['operation_id'] as String? ?? '',
      title: json['title'] as String? ?? '',
      message: json['message'] as String? ?? '',
      status: _enumByName(
        AppNotificationStatus.values,
        json['status'],
        AppNotificationStatus.pending,
      ),
      progressMode: _enumByName(
        AppNotificationProgressMode.values,
        json['progress_mode'],
        AppNotificationProgressMode.none,
      ),
      displayStyle: _enumByName(
        AppNotificationDisplayStyle.values,
        json['display_style'],
        AppNotificationDisplayStyle.automatic,
      ),
      kind: _enumByName(
        AppNotificationKind.values,
        json['kind'],
        AppNotificationKind.system,
      ),
      scope: _enumByName(
        AppNotificationScope.values,
        json['scope'],
        AppNotificationScope.local,
      ),
      attention: _enumByName(
        AppNotificationAttention.values,
        json['attention'],
        AppNotificationAttention.none,
      ),
      progress: (json['progress'] as num?)?.toDouble(),
      stages: (json['stages'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map(
            (item) =>
                AppNotificationStage.fromJson(item.cast<String, dynamic>()),
          )
          .toList(growable: false),
      actions: (json['actions'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map(
            (item) =>
                AppNotificationAction.fromJson(item.cast<String, dynamic>()),
          )
          .toList(growable: false),
      metadata:
          (json['metadata'] as Map?)?.map(
            (key, value) => MapEntry(key.toString(), value.toString()),
          ) ??
          const {},
      sourceLabel: json['source_label'] as String? ?? '',
      errorCode: json['error_code'] as String? ?? '',
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? '') ?? now,
      updatedAt: DateTime.tryParse(json['updated_at'] as String? ?? '') ?? now,
      completedAt: DateTime.tryParse(json['completed_at'] as String? ?? ''),
      unread: json['unread'] as bool? ?? true,
    );
  }
}

class AppNotificationSnapshot {
  final List<AppNotificationRecord> active;
  final List<AppNotificationRecord> recent;
  final List<String> hiddenOperationIds;
  final bool collapsed;

  const AppNotificationSnapshot({
    this.active = const [],
    this.recent = const [],
    this.hiddenOperationIds = const [],
    this.collapsed = false,
  });

  Map<String, dynamic> toJson() => {
    'version': 1,
    'active': active.map((item) => item.toJson()).toList(),
    'recent': recent.map((item) => item.toJson()).toList(),
    'hidden_operation_ids': hiddenOperationIds,
    'collapsed': collapsed,
  };

  factory AppNotificationSnapshot.fromJson(Map<String, dynamic> json) {
    List<AppNotificationRecord> parseList(Object? raw) {
      return (raw as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map(
            (item) =>
                AppNotificationRecord.fromJson(item.cast<String, dynamic>()),
          )
          .where((item) => item.id.isNotEmpty && item.operationId.isNotEmpty)
          .toList(growable: false);
    }

    final hiddenOperationIds =
        (json['hidden_operation_ids'] as List<dynamic>? ?? const [])
            .map((item) => item.toString().trim())
            .where((item) => item.isNotEmpty)
            .toSet()
            .toList(growable: false);

    return AppNotificationSnapshot(
      active: parseList(json['active']),
      recent: parseList(json['recent']),
      hiddenOperationIds: hiddenOperationIds,
      collapsed: json['collapsed'] as bool? ?? false,
    );
  }
}

T _enumByName<T extends Enum>(List<T> values, Object? raw, T fallback) {
  final name = raw?.toString();
  for (final value in values) {
    if (value.name == name) return value;
  }
  return fallback;
}

const Object _unset = Object();
