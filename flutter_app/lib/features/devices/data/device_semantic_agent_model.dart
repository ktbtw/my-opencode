class DeviceSemanticAgentProfile {
  final String id;
  final String name;
  final String description;
  final String icon;
  final String color;
  final List<String> skills;
  final List<String> recommendedMcpServers;
  final List<DeviceSemanticAgentRuntimeRequirement> runtimeRequirements;
  final Map<String, dynamic> toolPermissions;
  final String model;

  const DeviceSemanticAgentProfile({
    required this.id,
    required this.name,
    this.description = '',
    this.icon = '',
    this.color = '',
    this.skills = const [],
    this.recommendedMcpServers = const [],
    this.runtimeRequirements = const [],
    this.toolPermissions = const {},
    this.model = '',
  });

  factory DeviceSemanticAgentProfile.fromJson(Map<String, dynamic> json) {
    List<String> parseList(dynamic value) {
      if (value is! List) return const [];
      return value.map((item) => item.toString()).toList();
    }

    final recommendedMcpServers = <String>{
      ...parseList(json['recommended_mcp_servers']),
      ...parseList(json['mcp_ids']),
    }.toList(growable: false);
    return DeviceSemanticAgentProfile(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      description: json['description'] as String? ?? '',
      icon: json['icon'] as String? ?? '',
      color: json['color'] as String? ?? '',
      skills: parseList(json['skills']),
      recommendedMcpServers: recommendedMcpServers,
      runtimeRequirements:
          (json['runtime_requirements'] as List<dynamic>? ?? const [])
              .whereType<Map>()
              .map(
                (item) => DeviceSemanticAgentRuntimeRequirement.fromJson(
                  item.cast<String, dynamic>(),
                ),
              )
              .toList(),
      toolPermissions:
          (json['tool_permissions'] as Map?)?.cast<String, dynamic>() ??
          const {},
      model: json['model'] as String? ?? '',
    );
  }

  bool get usesVerifyMcp => recommendedMcpServers.any(
    (item) => item.trim().toLowerCase().contains('verify'),
  );
}

class DeviceSemanticAgentRuntimeRequirement {
  final String runtimeId;
  final String versionConstraint;
  final bool required;
  final String purpose;

  const DeviceSemanticAgentRuntimeRequirement({
    required this.runtimeId,
    this.versionConstraint = '',
    this.required = true,
    this.purpose = '',
  });

  factory DeviceSemanticAgentRuntimeRequirement.fromJson(
    Map<String, dynamic> json,
  ) {
    return DeviceSemanticAgentRuntimeRequirement(
      runtimeId: json['runtime_id'] as String? ?? '',
      versionConstraint: json['version_constraint'] as String? ?? '',
      required: json['required'] as bool? ?? true,
      purpose: json['purpose'] as String? ?? '',
    );
  }
}

class DeviceAgentSemanticSelectionInfo {
  final String agentId;
  final String semanticAgentId;
  final bool verifyMcpEnabled;
  final DeviceSemanticAgentProfile? profile;
  final List<DeviceSemanticAgentProfile> availableAgents;

  const DeviceAgentSemanticSelectionInfo({
    required this.agentId,
    required this.semanticAgentId,
    this.verifyMcpEnabled = true,
    this.profile,
    this.availableAgents = const [],
  });

  factory DeviceAgentSemanticSelectionInfo.fromJson(Map<String, dynamic> json) {
    return DeviceAgentSemanticSelectionInfo(
      agentId: json['agent_id'] as String? ?? '',
      semanticAgentId: json['semantic_agent_id'] as String? ?? '',
      verifyMcpEnabled: json['verify_mcp_enabled'] as bool? ?? true,
      profile: (json['profile'] as Map?) == null
          ? null
          : DeviceSemanticAgentProfile.fromJson(
              (json['profile'] as Map).cast<String, dynamic>(),
            ),
      availableAgents: (json['available_agents'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map((item) {
            return DeviceSemanticAgentProfile.fromJson(
              item.cast<String, dynamic>(),
            );
          })
          .toList(),
    );
  }
}

class RuntimeUserAction {
  final String id;
  final String kind;
  final String title;
  final String message;
  final List<String> instructions;
  final DateTime? requestedAt;

  const RuntimeUserAction({
    required this.id,
    this.kind = '',
    this.title = '',
    this.message = '',
    this.instructions = const [],
    this.requestedAt,
  });

  factory RuntimeUserAction.fromJson(Map<String, dynamic> json) {
    return RuntimeUserAction(
      id: json['id'] as String? ?? '',
      kind: json['kind'] as String? ?? '',
      title: json['title'] as String? ?? '',
      message: json['message'] as String? ?? '',
      instructions: (json['instructions'] as List<dynamic>? ?? const [])
          .whereType<String>()
          .where((item) => item.trim().isNotEmpty)
          .toList(),
      requestedAt: DateTime.tryParse(json['requested_at'] as String? ?? ''),
    );
  }
}

class RuntimePreflightJob {
  final String id;
  final String machineId;
  final String launcherAgentId;
  final String semanticAgentId;
  final String status;
  final String currentStep;
  final int progressPercent;
  final bool requiresUserAction;
  final RuntimeUserAction? userAction;
  final String error;
  final DateTime? createdAt;
  final DateTime? updatedAt;
  final DateTime? completedAt;
  final List<RuntimePreflightItem> items;
  final List<RuntimePreflightEvent> latestEvents;

  const RuntimePreflightJob({
    required this.id,
    this.machineId = '',
    this.launcherAgentId = '',
    this.semanticAgentId = '',
    this.status = '',
    this.currentStep = '',
    this.progressPercent = 0,
    this.requiresUserAction = false,
    this.userAction,
    this.error = '',
    this.createdAt,
    this.updatedAt,
    this.completedAt,
    this.items = const [],
    this.latestEvents = const [],
  });

  bool get completed => status == 'completed';
  bool get failed => status == 'failed' || status == 'cancelled';
  bool get finished => completed || failed;
  bool get waitingForUserAction =>
      requiresUserAction && userAction != null && userAction!.id.isNotEmpty;

  bool get missingVerifyTokenFailure {
    final messages = <String>[
      error,
      for (final item in items) item.error,
      for (final event in latestEvents) event.message,
    ].join('\n').toLowerCase();
    final namesToken =
        messages.contains('verify_api_token') ||
        messages.contains('verify_protect_token');
    final reportsMissing =
        messages.contains('缺少环境变量') ||
        messages.contains('missing environment') ||
        messages.contains('missing token') ||
        messages.contains('token is required');
    return failed && namesToken && reportsMissing;
  }

  factory RuntimePreflightJob.fromJson(Map<String, dynamic> json) {
    return RuntimePreflightJob(
      id: json['id'] as String? ?? '',
      machineId: json['machine_id'] as String? ?? '',
      launcherAgentId: json['launcher_agent_id'] as String? ?? '',
      semanticAgentId: json['semantic_agent_id'] as String? ?? '',
      status: json['status'] as String? ?? '',
      currentStep: json['current_step'] as String? ?? '',
      progressPercent: (json['progress_percent'] as num?)?.round() ?? 0,
      requiresUserAction: json['requires_user_action'] as bool? ?? false,
      userAction: json['user_action'] is Map
          ? RuntimeUserAction.fromJson(
              (json['user_action'] as Map).cast<String, dynamic>(),
            )
          : null,
      error: json['error'] as String? ?? '',
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? ''),
      updatedAt: DateTime.tryParse(json['updated_at'] as String? ?? ''),
      completedAt: DateTime.tryParse(json['completed_at'] as String? ?? ''),
      items: (json['items'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map(
            (item) =>
                RuntimePreflightItem.fromJson(item.cast<String, dynamic>()),
          )
          .toList(),
      latestEvents: (json['latest_events'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map(
            (item) =>
                RuntimePreflightEvent.fromJson(item.cast<String, dynamic>()),
          )
          .toList(),
    );
  }
}

String effectiveRuntimePreflightStep(
  RuntimePreflightJob job, {
  List<RuntimePreflightEvent> supplementalEvents = const [],
}) {
  const order = ['runtime', 'mcp', 'skill', 'apply'];
  var index = order.indexOf(normalizeRuntimePreflightStep(job.currentStep));
  if (index < 0) index = 0;
  final events = supplementalEvents.isEmpty
      ? job.latestEvents
      : supplementalEvents;
  for (final event in events) {
    final eventIndex = order.indexOf(
      normalizeRuntimePreflightStep(event.itemType),
    );
    if (eventIndex > index) index = eventIndex;
  }

  // A job snapshot and its event stream are persisted independently. During
  // that small consistency window, item status is the better signal than a
  // stale current_step value.
  for (final item in job.items) {
    final itemIndex = order.indexOf(
      normalizeRuntimePreflightStep(item.itemType),
    );
    if (itemIndex < 0) continue;
    final status = item.status.trim().toLowerCase();
    if (status == 'running' ||
        status == 'waiting_user_action' ||
        status == 'failed') {
      if (itemIndex > index) index = itemIndex;
    } else if (status == 'completed' &&
        itemIndex + 1 < order.length &&
        itemIndex + 1 > index) {
      index = itemIndex + 1;
    }
  }
  return order[index];
}

int runtimePreflightDisplayProgress(
  RuntimePreflightJob job, {
  List<RuntimePreflightEvent> supplementalEvents = const [],
}) {
  const order = ['runtime', 'mcp', 'skill', 'apply'];
  const stageStart = [0, 40, 70, 90];
  const stageSpan = [40, 30, 20, 10];
  var progress = job.progressPercent.clamp(0, 100);
  final step = effectiveRuntimePreflightStep(
    job,
    supplementalEvents: supplementalEvents,
  );
  var index = order.indexOf(step);
  if (index < 0) index = 0;
  var stagePercent = 0;
  final events = supplementalEvents.isEmpty
      ? job.latestEvents
      : supplementalEvents;
  for (final item in job.items) {
    if (normalizeRuntimePreflightStep(item.itemType) != step) continue;
    if (item.progressPercent > stagePercent) {
      stagePercent = item.progressPercent.clamp(0, 100);
    }
  }
  for (final event in events) {
    if (normalizeRuntimePreflightStep(event.itemType) != step) continue;
    if (event.progressPercent > stagePercent) {
      stagePercent = event.progressPercent.clamp(0, 100);
    }
    if (event.totalBytes > 0) {
      final downloaded = ((event.receivedBytes * 100) / event.totalBytes)
          .round()
          .clamp(0, 100);
      if (downloaded > stagePercent) stagePercent = downloaded;
    }
  }
  final estimated = stageStart[index] + (stageSpan[index] * stagePercent) ~/ 100;
  if (estimated > progress) progress = estimated;
  return progress.clamp(0, 100);
}

String normalizeRuntimePreflightStep(String value) {
  switch (value.trim().toLowerCase()) {
    case 'runtime':
    case 'mcp':
    case 'skill':
    case 'apply':
      return value.trim().toLowerCase();
    case 'agent_apply':
      return 'apply';
    case 'mcp_ready':
    case 'completed':
      return 'apply';
    default:
      return '';
  }
}

class RuntimePreflightItem {
  final String itemType;
  final String itemId;
  final String name;
  final bool required;
  final String status;
  final int progressPercent;
  final String error;

  const RuntimePreflightItem({
    this.itemType = '',
    this.itemId = '',
    this.name = '',
    this.required = true,
    this.status = '',
    this.progressPercent = 0,
    this.error = '',
  });

  factory RuntimePreflightItem.fromJson(Map<String, dynamic> json) {
    return RuntimePreflightItem(
      itemType: json['item_type'] as String? ?? '',
      itemId: json['item_id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      required: json['required'] as bool? ?? true,
      status: json['status'] as String? ?? '',
      progressPercent: (json['progress_percent'] as num?)?.round() ?? 0,
      error: json['error'] as String? ?? '',
    );
  }
}

class RuntimePreflightEvent {
  final int id;
  final int sequence;
  final String itemType;
  final String itemId;
  final String phase;
  final String status;
  final String message;
  final int receivedBytes;
  final int totalBytes;
  final int progressPercent;
  final Map<String, dynamic> details;

  const RuntimePreflightEvent({
    this.id = 0,
    this.sequence = 0,
    this.itemType = '',
    this.itemId = '',
    this.phase = '',
    this.status = '',
    this.message = '',
    this.receivedBytes = 0,
    this.totalBytes = 0,
    this.progressPercent = 0,
    this.details = const {},
  });

  factory RuntimePreflightEvent.fromJson(Map<String, dynamic> json) {
    return RuntimePreflightEvent(
      id: (json['id'] as num?)?.round() ?? 0,
      sequence: (json['sequence'] as num?)?.round() ?? 0,
      itemType: json['item_type'] as String? ?? '',
      itemId: json['item_id'] as String? ?? '',
      phase: json['phase'] as String? ?? '',
      status: json['status'] as String? ?? '',
      message: json['message'] as String? ?? '',
      receivedBytes: (json['received_bytes'] as num?)?.round() ?? 0,
      totalBytes: (json['total_bytes'] as num?)?.round() ?? 0,
      progressPercent: (json['progress_percent'] as num?)?.round() ?? 0,
      details: (json['details'] as Map?)?.cast<String, dynamic>() ?? const {},
    );
  }
}
