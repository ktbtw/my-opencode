import '../../../core/services/connection_telemetry.dart';
import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../../../core/config/api_client.dart';
import '../../../core/config/sse_log.dart';
import '../../../core/services/account_scope.dart';
import '../../../core/services/app_log_service.dart';
import '../../../core/storage/app_storage.dart';
import '../../../features/settings/settings_provider.dart';
import '../data/artifact_cache_service.dart';
import '../data/chat_model.dart';
import '../data/chat_repository.dart';
import '../data/task_event_batcher.dart';
import '../data/local_chat_store.dart';
import '../data/local_event_writer.dart';
import 'chat_error_formatter.dart';

final chatRepositoryProvider = Provider((ref) {
  ref.watch(accountScopeProvider);
  final repository = ChatRepository();
  ref.onDispose(repository.dispose);
  return repository;
});

int _elapsedMsSince(DateTime startedAt) =>
    DateTime.now().difference(startedAt).inMilliseconds;

// A completed result is already available from the task index. Defer the
// expensive event/tool reconstruction for unusually large replies until the
// user asks for tool history explicitly.
const _maxAutoHydrateResultChars = 256 * 1024;

// 模型列表按机器加载，避免多设备时拿到其他 launcher 的供应商缓存。
final availableModelsProvider = FutureProvider.family<List<ModelInfo>, String>((
  ref,
  machineId,
) async {
  final repo = ref.watch(chatRepositoryProvider);
  return repo.getModels(machineId: machineId);
});

// 模型选择（按 账号+设备 级别持久化到后端）
class SelectedModelNotifier extends StateNotifier<ModelInfo?> {
  final String agentId;
  int _initGeneration = 0;

  SelectedModelNotifier(this.agentId) : super(null);

  /// 初始化：从后端加载该设备的模型设置，匹配可用模型列表
  Future<void> init(List<ModelInfo> models) async {
    if (models.isEmpty || agentId.isEmpty) return;
    final generation = ++_initGeneration;

    // 优先从后端获取
    try {
      final settings = await ApiClient.get('/api/agents/$agentId/settings');
      await AppLogService.log(
        'chat_model_init_loaded',
        data: {'agent_id': agentId, 'saved_model': settings['selected_model']},
      );
      if (generation != _initGeneration) return;
      final saved = settings['selected_model']?.toString().trim() ?? '';
      if (saved.isEmpty) {
        state = null;
        return;
      }
      final match = models.where((model) => model.metaKey == saved).firstOrNull;
      if (match != null) {
        state = match;
        return;
      }

      // 兼容历史保存值，避免 provider/model 组合被旧格式影响恢复。
      final separator = saved.indexOf('/');
      if (separator > 0 && separator < saved.length - 1) {
        final provider = saved.substring(0, separator);
        final modelID = saved.substring(separator + 1);
        state = models
            .where(
              (model) =>
                  model.providerID == provider && model.modelID == modelID,
            )
            .firstOrNull;
      }
    } catch (_) {}
  }

  Future<void> select(ModelInfo? model) async {
    _initGeneration++;
    final previous = state;
    state = model;
    await AppLogService.log(
      'chat_model_selected',
      data: {
        'agent_id': agentId,
        'previous_model': previous?.metaKey,
        'selected_model': model?.metaKey,
      },
    );
    // 同步保存到后端
    if (agentId.isNotEmpty) {
      try {
        await ApiClient.post('/api/agents/$agentId/settings', {
          'selected_model': model == null
              ? ''
              : '${model.providerID}/${model.modelID}',
        });
      } catch (_) {}
    }
  }
}

final selectedModelProvider =
    StateNotifierProvider.family<SelectedModelNotifier, ModelInfo?, String>(
      (ref, agentId) => SelectedModelNotifier(agentId),
    );

// 思考强度选择（按 账号+设备 级别持久化到后端）
class SelectedVariantNotifier extends StateNotifier<String?> {
  final String agentId;

  SelectedVariantNotifier(this.agentId) : super(null);

  Future<void> init() async {
    state = null;
    if (agentId.isEmpty) return;
    try {
      final settings = await ApiClient.get('/api/agents/$agentId/settings');
      await AppLogService.log(
        'chat_model_init_loaded',
        data: {'agent_id': agentId, 'saved_model': settings['selected_model']},
      );
      final saved = settings['selected_variant'] as String?;
      state = (saved != null && saved.isNotEmpty) ? saved : null;
    } catch (_) {}
  }

  Future<void> select(String? variant) async {
    final previous = state;
    state = variant;
    await AppLogService.log(
      'chat_variant_selected',
      data: {
        'agent_id': agentId,
        'previous_variant': previous,
        'selected_variant': variant,
      },
    );
    if (agentId.isNotEmpty) {
      try {
        await ApiClient.post('/api/agents/$agentId/settings', {
          'selected_variant': variant ?? '',
        });
      } catch (_) {}
    }
  }
}

final selectedVariantProvider =
    StateNotifierProvider.family<SelectedVariantNotifier, String?, String>(
      (ref, agentId) => SelectedVariantNotifier(agentId),
    );

class ProjectPromptNotifier extends StateNotifier<String> {
  static const _settingKey = 'project_prompt';

  final String agentId;

  ProjectPromptNotifier(this.agentId) : super('');

  Future<void> init() async {
    if (agentId.isEmpty) {
      state = '';
      return;
    }

    try {
      final settings = await ApiClient.get('/api/agents/$agentId/settings');
      await AppLogService.log(
        'chat_model_init_loaded',
        data: {'agent_id': agentId, 'saved_model': settings['selected_model']},
      );
      state = (settings[_settingKey] as String? ?? '').trim();
    } catch (_) {
      state = '';
    }
  }

  Future<void> save(String value) async {
    final normalized = value.trim();
    final previous = state;
    state = normalized;
    await AppLogService.log(
      'project_prompt_saved',
      data: {
        'agent_id': agentId,
        'previous_length': previous.length,
        'new_length': normalized.length,
      },
    );

    try {
      await ApiClient.post('/api/agents/$agentId/settings', {
        _settingKey: normalized,
      });
    } catch (e) {
      state = previous;
      rethrow;
    }
  }
}

final projectPromptProvider =
    StateNotifierProvider.family<ProjectPromptNotifier, String, String>(
      (ref, agentId) => ProjectPromptNotifier(agentId),
    );

// 全局审批模式（持久化）
// 可选值: 'ask' | 'auto-approve' | 'deny'
class PermissionModeNotifier extends StateNotifier<String> {
  static const _key = 'permission_mode';

  PermissionModeNotifier() : super('ask') {
    _init();
  }

  Future<void> _init() async {
    final prefs = await SharedPreferences.getInstance();
    final saved = prefs.getString(_key);
    if (saved == 'ask' || saved == 'auto-approve' || saved == 'deny') {
      state = saved!;
    }
  }

  Future<void> set(String mode) async {
    if (mode != 'ask' && mode != 'auto-approve' && mode != 'deny') return;
    state = mode;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_key, mode);
  }
}

final permissionModeProvider =
    StateNotifierProvider<PermissionModeNotifier, String>(
      (ref) => PermissionModeNotifier(),
    );

class GoalSettings {
  static const statusIdle = 'idle';
  static const statusRunning = 'running';
  static const statusPaused = 'paused';
  static const statusCompleted = 'completed';
  static const statusFailed = 'failed';
  static const defaultMaxIterations = 30;
  static const minMaxIterations = 1;
  static const maxMaxIterations = 100;

  final String content;
  final bool enabled;
  final String status;
  final int maxIterations;
  final bool optimizeEnabled;
  final DateTime? startedAt;
  final int elapsedSeconds;
  final DateTime? updatedAt;

  const GoalSettings({
    this.content = '',
    this.enabled = false,
    this.status = statusIdle,
    this.maxIterations = defaultMaxIterations,
    this.optimizeEnabled = false,
    this.startedAt,
    this.elapsedSeconds = 0,
    this.updatedAt,
  });

  bool get hasContent => content.trim().isNotEmpty;

  String get statusLabel {
    if (!hasContent) return '未设置';
    if (enabled) return '运行中';
    return switch (status) {
      statusCompleted => '已完成',
      statusFailed => '失败',
      statusPaused => '已暂停',
      _ => '已暂停',
    };
  }

  Duration runtime(DateTime now) {
    var seconds = elapsedSeconds;
    final start = startedAt;
    if (enabled && start != null) {
      final active = now.difference(start).inSeconds;
      if (active > 0) seconds += active;
    }
    return Duration(seconds: seconds < 0 ? 0 : seconds);
  }

  Map<String, dynamic> toJson() => {
    'content': content,
    'enabled': enabled,
    'status': status,
    'max_iterations': maxIterations,
    'optimize_enabled': optimizeEnabled,
    'started_at': startedAt?.toUtc().toIso8601String(),
    'elapsed_seconds': elapsedSeconds,
    'updated_at': updatedAt?.toUtc().toIso8601String(),
  };

  factory GoalSettings.fromJson(Object? raw) {
    if (raw is! Map) return const GoalSettings();
    final startedRaw = raw['started_at'] as String?;
    final updatedRaw = raw['updated_at'] as String?;
    return GoalSettings(
      content: (raw['content'] as String? ?? '').trim(),
      enabled: raw['enabled'] == true,
      status: _normalizeStatus(raw['status'] as String?),
      maxIterations: normalizeMaxIterations(
        raw['max_iterations'] ?? raw['goal_max_iterations'],
      ),
      optimizeEnabled: raw['optimize_enabled'] == true,
      startedAt: startedRaw == null ? null : DateTime.tryParse(startedRaw),
      elapsedSeconds: (raw['elapsed_seconds'] as num?)?.toInt() ?? 0,
      updatedAt: updatedRaw == null ? null : DateTime.tryParse(updatedRaw),
    );
  }

  static String _normalizeStatus(String? raw) {
    return switch (raw?.trim()) {
      statusRunning => statusRunning,
      statusPaused => statusPaused,
      statusCompleted => statusCompleted,
      statusFailed => statusFailed,
      _ => statusIdle,
    };
  }

  static int normalizeMaxIterations(Object? raw) {
    final value = switch (raw) {
      int v => v,
      num v => v.toInt(),
      String v => int.tryParse(v.trim()),
      _ => null,
    };
    if (value == null) return defaultMaxIterations;
    return value.clamp(minMaxIterations, maxMaxIterations).toInt();
  }

  GoalSettings withContent(
    String nextContent, {
    int? nextMaxIterations,
    DateTime? now,
  }) {
    final normalized = nextContent.trim();
    final timestamp = now ?? DateTime.now();
    final normalizedMax = normalizeMaxIterations(
      nextMaxIterations ?? maxIterations,
    );
    final resetRuntime =
        normalized != content || normalizedMax != maxIterations;
    return GoalSettings(
      content: normalized,
      enabled: normalized.isNotEmpty && enabled,
      status: normalized.isEmpty
          ? statusIdle
          : (enabled ? statusRunning : (resetRuntime ? statusPaused : status)),
      maxIterations: normalizedMax,
      optimizeEnabled: optimizeEnabled,
      startedAt: normalized.isNotEmpty && enabled
          ? (resetRuntime ? timestamp : (startedAt ?? timestamp))
          : null,
      elapsedSeconds: normalized.isEmpty || resetRuntime ? 0 : elapsedSeconds,
      updatedAt: timestamp,
    );
  }

  GoalSettings withEnabled(bool nextEnabled, {DateTime? now}) {
    final timestamp = now ?? DateTime.now();
    if (nextEnabled && !hasContent) return this;
    final restarting = nextEnabled && !enabled;
    return GoalSettings(
      content: content,
      enabled: nextEnabled,
      status: nextEnabled ? statusRunning : statusPaused,
      maxIterations: maxIterations,
      optimizeEnabled: optimizeEnabled,
      startedAt: nextEnabled ? timestamp : null,
      elapsedSeconds: nextEnabled
          ? (restarting ? 0 : elapsedSeconds)
          : runtime(timestamp).inSeconds,
      updatedAt: timestamp,
    );
  }

  GoalSettings withOptimizeEnabled(bool nextEnabled, {DateTime? now}) {
    return GoalSettings(
      content: content,
      enabled: enabled,
      status: status,
      maxIterations: maxIterations,
      optimizeEnabled: nextEnabled,
      startedAt: startedAt,
      elapsedSeconds: elapsedSeconds,
      updatedAt: now ?? DateTime.now(),
    );
  }

  GoalSettings withTerminalStatus(String nextStatus, {DateTime? now}) {
    final timestamp = now ?? DateTime.now();
    final normalizedStatus = _normalizeStatus(nextStatus);
    if (!hasContent) {
      return GoalSettings(status: normalizedStatus, updatedAt: timestamp);
    }
    return GoalSettings(
      content: content,
      enabled: false,
      status: normalizedStatus == statusIdle ? statusPaused : normalizedStatus,
      maxIterations: maxIterations,
      optimizeEnabled: optimizeEnabled,
      startedAt: null,
      elapsedSeconds: runtime(timestamp).inSeconds,
      updatedAt: timestamp,
    );
  }
}

bool shouldOptimizeGoalDraft({
  required bool optimizeEnabled,
  required String draft,
  required int maxIterations,
  required GoalSettings current,
}) {
  final normalized = draft.trim();
  if (!optimizeEnabled || normalized.isEmpty) return false;
  if (!current.optimizeEnabled) return true;
  if (normalized != current.content) return true;
  return maxIterations != current.maxIterations;
}

class GoalSettingsNotifier extends StateNotifier<GoalSettings> {
  static const _settingKey = 'goal_config';

  final String agentId;
  final String projectId;

  GoalSettingsNotifier(this.agentId, this.projectId)
    : super(const GoalSettings());

  Future<void> init() async {
    if (agentId.isEmpty || projectId.isEmpty) {
      state = const GoalSettings();
      return;
    }
    try {
      final config = await _loadConfig();
      state = _goalFromConfig(config);
    } catch (_) {
      state = const GoalSettings();
    }
  }

  Future<void> saveContent(String content, {int? maxIterations}) async {
    await _persist(
      state.withContent(content, nextMaxIterations: maxIterations),
    );
  }

  Future<void> setOptimizeEnabled(bool enabled) async {
    await _persist(state.withOptimizeEnabled(enabled));
  }

  Future<void> setEnabled(bool enabled) async {
    if (enabled && !state.hasContent) return;
    await _persist(state.withEnabled(enabled));
  }

  Future<void> markCompleted() async {
    await _persist(state.withTerminalStatus(GoalSettings.statusCompleted));
  }

  Future<void> markPaused() async {
    await _persist(state.withTerminalStatus(GoalSettings.statusPaused));
  }

  Future<void> markFailed() async {
    await _persist(state.withTerminalStatus(GoalSettings.statusFailed));
  }

  Future<void> clear() async {
    await _persist(GoalSettings(updatedAt: DateTime.now()));
  }

  Future<Map<String, dynamic>> _loadConfig() async {
    final settings = await ApiClient.get('/api/agents/$agentId/settings');
    return _decodeConfig(settings[_settingKey]);
  }

  Map<String, dynamic> _decodeConfig(Object? raw) {
    if (raw is Map) {
      return Map<String, dynamic>.from(raw);
    }
    if (raw is! String || raw.trim().isEmpty) return {};
    try {
      final decoded = jsonDecode(raw);
      return decoded is Map ? Map<String, dynamic>.from(decoded) : {};
    } catch (_) {
      return {};
    }
  }

  GoalSettings _goalFromConfig(Map<String, dynamic> config) {
    final projectValue = config[projectId];
    if (projectValue is Map) {
      return GoalSettings.fromJson(projectValue);
    }
    return GoalSettings.fromJson(config);
  }

  Future<void> _persist(GoalSettings next) async {
    final previous = state;
    state = next;
    try {
      final config = await _loadConfig();
      config[projectId] = next.toJson();
      await ApiClient.post('/api/agents/$agentId/settings', {
        _settingKey: jsonEncode(config),
      });
      await AppLogService.log(
        'goal_settings_saved',
        data: {
          'agent_id': agentId,
          'project_id': projectId,
          'enabled': next.enabled,
          'content_length': next.content.length,
        },
      );
    } catch (e) {
      state = previous;
      rethrow;
    }
  }
}

final goalSettingsProvider =
    StateNotifierProvider.family<
      GoalSettingsNotifier,
      GoalSettings,
      (String agentId, String projectId)
    >((ref, key) => GoalSettingsNotifier(key.$1, key.$2));

// 历史会话列表
final sessionListProvider = FutureProvider.autoDispose
    .family<List<SessionModel>, (String agentId, String projectId)>((
      ref,
      args,
    ) async {
      final repo = ref.watch(chatRepositoryProvider);
      final sessions = await repo.getSessions(agentId: args.$1);
      return sessions
          .where(
            (s) =>
                s.projectId == args.$2 &&
                !s.sessionId.startsWith(
                  ChatRepository.modelLatencyTestSessionPrefix,
                ),
          )
          .toList();
    });

enum ChatOperationStepState { pending, active, done, failed }

class ChatOperationStep {
  final String key;
  final String label;
  final String detail;
  final ChatOperationStepState state;

  const ChatOperationStep({
    required this.key,
    required this.label,
    this.detail = '',
    this.state = ChatOperationStepState.pending,
  });

  ChatOperationStep copyWith({String? detail, ChatOperationStepState? state}) =>
      ChatOperationStep(
        key: key,
        label: label,
        detail: detail ?? this.detail,
        state: state ?? this.state,
      );
}

// 对话控制器状态
class ChatState {
  final List<ChatMessage> messages;
  final bool sending;
  final bool taskActive;
  final String? currentSessionId;
  final ApprovalInfo? pendingApproval;
  final String? pendingApprovalTaskId;
  final QuestionRequestInfo? pendingQuestion;
  final String? pendingQuestionTaskId;
  final String? error;
  final List<AttachedFile> attachedFiles;
  final bool loadingEarlierHistory;
  final bool hasEarlierHistory;
  final ContextUsageInfo? contextUsage;
  final bool cancelRequested;
  final ChatQueueSnapshot queue;
  final bool queueLoading;
  final String? queueError;
  final String? operationTitle;
  final List<ChatOperationStep> operationSteps;

  const ChatState({
    this.messages = const [],
    this.sending = false,
    this.taskActive = false,
    this.currentSessionId,
    this.pendingApproval,
    this.pendingApprovalTaskId,
    this.pendingQuestion,
    this.pendingQuestionTaskId,
    this.error,
    this.attachedFiles = const [],
    this.loadingEarlierHistory = false,
    this.hasEarlierHistory = false,
    this.contextUsage,
    this.cancelRequested = false,
    this.queue = const ChatQueueSnapshot(),
    this.queueLoading = false,
    this.queueError,
    this.operationTitle,
    this.operationSteps = const [],
  });

  bool get canStop =>
      taskActive ||
      messages.any(
        (message) =>
            message.role == MessageRole.agent &&
            message.state == MessageState.streaming,
      );

  ChatState copyWith({
    List<ChatMessage>? messages,
    bool? sending,
    bool? taskActive,
    String? currentSessionId,
    ApprovalInfo? pendingApproval,
    String? pendingApprovalTaskId,
    QuestionRequestInfo? pendingQuestion,
    String? pendingQuestionTaskId,
    String? error,
    bool clearApproval = false,
    bool clearQuestion = false,
    bool clearError = false,
    List<AttachedFile>? attachedFiles,
    bool? loadingEarlierHistory,
    bool? hasEarlierHistory,
    ContextUsageInfo? contextUsage,
    bool clearContextUsage = false,
    bool? cancelRequested,
    ChatQueueSnapshot? queue,
    bool? queueLoading,
    String? queueError,
    bool clearQueueError = false,
    String? operationTitle,
    List<ChatOperationStep>? operationSteps,
    bool clearOperation = false,
  }) {
    return ChatState(
      messages: messages == null
          ? this.messages
          : chronologicalChatMessages(messages),
      sending: sending ?? this.sending,
      taskActive: taskActive ?? this.taskActive,
      currentSessionId: currentSessionId ?? this.currentSessionId,
      pendingApproval: clearApproval
          ? null
          : (pendingApproval ?? this.pendingApproval),
      pendingApprovalTaskId: clearApproval
          ? null
          : (pendingApprovalTaskId ?? this.pendingApprovalTaskId),
      pendingQuestion: clearQuestion
          ? null
          : (pendingQuestion ?? this.pendingQuestion),
      pendingQuestionTaskId: clearQuestion
          ? null
          : (pendingQuestionTaskId ?? this.pendingQuestionTaskId),
      error: clearError ? null : (error ?? this.error),
      attachedFiles: attachedFiles ?? this.attachedFiles,
      loadingEarlierHistory:
          loadingEarlierHistory ?? this.loadingEarlierHistory,
      hasEarlierHistory: hasEarlierHistory ?? this.hasEarlierHistory,
      contextUsage: clearContextUsage
          ? null
          : (contextUsage ?? this.contextUsage),
      cancelRequested: cancelRequested ?? this.cancelRequested,
      queue: queue ?? this.queue,
      queueLoading: queueLoading ?? this.queueLoading,
      queueError: clearQueueError ? null : (queueError ?? this.queueError),
      operationTitle: clearOperation
          ? null
          : (operationTitle ?? this.operationTitle),
      operationSteps: clearOperation
          ? const []
          : (operationSteps ?? this.operationSteps),
    );
  }
}

class ChatNotifier extends StateNotifier<ChatState> {
  static const int _sessionHistoryPageSize = 5;
  static const int _queueInitialSyncFailureLimit = 2;

  /// 应用进入后台时挂起长连接属于预期行为，期间产生的队列断开不应提示用户。
  bool _appInBackground = false;
  bool _pageVisible = true;
  int _liveVersion = 0;
  bool get _detailsEnabled => !_disposed && _pageVisible && !_appInBackground;

  void _beginOperation(
    String title,
    List<ChatOperationStep> steps, {
    bool autoClear = false,
  }) {
    _operationClearTimer?.cancel();
    state = state.copyWith(operationTitle: title, operationSteps: steps);
    if (autoClear) _scheduleOperationClear();
  }

  void _updateOperation(
    String key, {
    ChatOperationStepState? stepState,
    String? detail,
    String? title,
    bool autoClear = false,
  }) {
    final steps = [
      for (final step in state.operationSteps)
        step.key == key
            ? step.copyWith(state: stepState, detail: detail)
            : step,
    ];
    state = state.copyWith(
      operationTitle: title ?? state.operationTitle,
      operationSteps: steps,
    );
    if (autoClear) _scheduleOperationClear();
  }

  void _scheduleOperationClear() {
    _operationClearTimer?.cancel();
    _operationClearTimer = Timer(const Duration(seconds: 4), () {
      if (_disposed) return;
      state = state.copyWith(clearOperation: true);
      _operationClearTimer = null;
    });
  }

  void setPageVisible(bool visible) {
    if (_disposed || _pageVisible == visible) return;
    if (!visible) _flushEventDeltas?.call();
    _pageVisible = visible;
    if (visible) {
      _prepareResumeRecoveryState();
      if (!_appInBackground) unawaited(reconcileAfterResume());
    } else {
      _suspendDetails();
    }
  }

  void _suspendDetails() {
    // Pending raw deltas are recovered from the authoritative snapshot on return.
    _flushEventDeltas = null;
    _resumeReconciliationToken = null;
    _terminalProbeToken = null;
    _liveVersion++;
    _terminalReconcileTimer?.cancel();
    _terminalReconcileTimer = null;
    unawaited(_eventSub?.cancel());
    _eventSub = null;
    _cancelQueueSub();
    // Keep task identity and message state for stop/recovery; only connections
    // belong to page visibility. No server-side cancellation happens here.
  }

  /// 标记应用进入后台：后续队列连接断开按预期行为处理，不产生用户可见错误。
  void markAppBackgrounded() {
    // Accepted events have already advanced dedupe/persistence state. Flush
    // them before hiding the page so cancellation cannot lose their body.
    _flushEventDeltas?.call();
    _appInBackground = true;
    _resumeRecoveryWatchdog?.cancel();
    _resumeRecoveryWatchdog = null;
    _operationClearTimer?.cancel();
    _operationClearTimer = null;
    _suspendDetails();
  }

  void _prepareResumeRecoveryState() {
    final hasSyncing = state.messages.any(
      (message) =>
          message.role == MessageRole.agent &&
          message.streamRecovery == StreamRecovery.syncing,
    );
    if (!hasSyncing) return;
    // A navigation return must never inherit an unbounded "syncing" label.
    // The probe starts below; until it answers, show an honest reconnecting state.
    final messages = [
      for (final message in state.messages)
        if (message.role == MessageRole.agent &&
            message.streamRecovery == StreamRecovery.syncing)
          (message.copyForUpdate()
            ..streamRecovery = StreamRecovery.reconnecting)
        else
          message,
    ];
    state = state.copyWith(messages: messages);
  }

  void _armResumeRecoveryWatchdog(Object token) {
    _resumeRecoveryWatchdog?.cancel();
    _resumeRecoveryWatchdog = Timer(resumeRecoveryWatchdogTimeout, () {
      if (_disposed || !identical(_resumeReconciliationToken, token)) return;
      _resumeReconciliationToken = null;
      replySyncProgress?.finish('连接恢复超时');
      _updateOperation(
        'opencode',
        stepState: ChatOperationStepState.failed,
        detail: 'Agent 状态确认超时',
      );
      _updateOperation(
        'task',
        stepState: ChatOperationStepState.active,
        detail: '正在重连回复通道',
        title: '连接恢复中',
      );
      final messages = [
        for (final message in state.messages)
          if (message.role == MessageRole.agent &&
              message.streamRecovery == StreamRecovery.syncing)
            (message.copyForUpdate()
              ..streamRecovery = StreamRecovery.reconnecting)
          else
            message,
      ];
      state = state.copyWith(messages: messages);
      if (_currentTaskId != null && _detailsEnabled) {
        _listenEvents(
          _currentTaskId!,
          _activeTaskAgentMessageIds[_currentTaskId!] ?? '',
          DateTime.now(),
        );
      }
    });
  }

  final ChatRepository _repo;
  final LocalChatStore _localStore;
  late final _eventWriter = LocalEventWriter(_localStore);
  final String agentId;
  final String projectId;

  StreamSubscription<Map<String, dynamic>>? _eventSub;
  void Function()? _flushEventDeltas;
  StreamSubscription<ChatQueueSnapshot>? _queueSub;
  Timer? _terminalReconcileTimer;
  Timer? _resumeRecoveryWatchdog;
  Timer? _operationClearTimer;
  String? _currentTaskId; // 当前正在进行的 task id，用于取消
  String? get activeTaskId => _currentTaskId;
  Completer<String>? _sessionReadyCompleter;
  String? _queueSessionId;
  Timer? _queueReconnectTimer;
  int _queueSyncVersion = 0;
  bool _queueInitialSyncPending = false;
  int _queueInitialSyncFailures = 0;
  bool _disposed = false;
  bool _pendingCancelBeforeTaskCreated = false;
  final Set<String> _cancelRequestedTaskIds = {};
  int _sessionLoadVersion = 0;
  final Map<String, Set<String>> _handledTaskEventKeys = {};
  final Map<String, String> _activeTaskAgentMessageIds = {};
  DateTime? _historyCursorCreatedAt;
  String? _historyCursorTaskId;
  int? _authoritativeSessionLoadVersion;
  Object? _resumeReconciliationToken;
  Object? _terminalProbeToken;
  ReplySyncProgress? replySyncProgress;

  @visibleForTesting
  Duration recoveryStatusTimeout = const Duration(seconds: 6);
  Duration resumeRecoveryWatchdogTimeout = const Duration(seconds: 9);
  @visibleForTesting
  Duration recoveryHistoryTimeout = const Duration(seconds: 4);
  @visibleForTesting
  Duration activeHistoryHydrationTimeout = const Duration(milliseconds: 750);
  @visibleForTesting
  Duration terminalHistoryTimeout = const Duration(seconds: 26);

  @visibleForTesting
  Duration terminalReconcileInterval = const Duration(seconds: 8);

  ChatNotifier({
    required ChatRepository repo,
    required this.agentId,
    required this.projectId,
    LocalChatStore? localStore,
    bool initiallyVisible = true,
  }) : _repo = repo,
       _localStore = localStore ?? createLocalChatStore(),
       _localIdentity = AppStorage.initialized
           ? AppStorage.storageIdentity
           : '',
       super(const ChatState()) {
    _pageVisible = initiallyVisible;
  }

  final String _localIdentity;

  Future<void> _loadCachedSession(String sessionId, int loadVersion) async {
    final identity = _localIdentity;
    if (identity.isEmpty) return;
    try {
      await _localStore.init();
      final cached = await _localStore.readTasks(
        identity: identity,
        agentId: agentId,
        projectId: projectId,
        sessionId: sessionId,
        limit: _sessionHistoryPageSize,
      );
      if (cached.isEmpty ||
          !_isCurrentSessionLoad(sessionId, loadVersion) ||
          _authoritativeSessionLoadVersion == loadVersion) {
        return;
      }
      final tasks = cached
          .map((item) => TaskModel.fromJson(item.payload))
          .where((task) => task.taskId.isNotEmpty)
          .toList();
      final sortedTasks = [...tasks]..sort(_compareTaskTimeline);
      final history = _messagesFromHistoryTasks(sortedTasks);
      final latestTask = sortedTasks.isNotEmpty ? sortedTasks.last : null;
      final latestActiveTask =
          latestTask != null && _isTaskActive(latestTask.status)
          ? latestTask
          : null;
      state = state.copyWith(
        messages: history.messages,
        sending: latestActiveTask != null,
        taskActive:
            latestTask != null &&
            (_isTaskActive(latestTask.status) ||
                latestTask.status == TaskStatus.cancelling),
        pendingApproval: latestActiveTask?.approval,
        pendingApprovalTaskId: latestActiveTask?.approval != null
            ? latestActiveTask?.taskId
            : null,
        pendingQuestion: latestActiveTask?.question,
        pendingQuestionTaskId: latestActiveTask?.question != null
            ? latestActiveTask?.taskId
            : null,
        hasEarlierHistory: cached.length >= _sessionHistoryPageSize,
      );
      // Restore usage before waiting for network reconciliation. The per-account
      // event cache already contains the last live measurement for this session.
      for (final task in sortedTasks.reversed.take(1)) {
        final snapshot = await _localSnapshotForTask(task.taskId);
        if (!_isCurrentSessionLoad(sessionId, loadVersion)) return;
        final usage = snapshot.contextUsage;
        if (usage == null || !usage.hasContextMeasurement) continue;
        state = state.copyWith(
          contextUsage: _mergeContextUsage(state.contextUsage, usage),
        );
        break;
      }
      await _reconcileTerminalStreamingMessages(
        sessionId: sessionId,
        loadVersion: loadVersion,
      );
      _logChatLatency(
        'local_history_loaded',
        data: {'message_count': history.messages.length},
      );
    } catch (error) {
      debugPrint('[Chat] local history read failed: $error');
    }
  }

  Future<bool> _cacheSessionTasks(
    String sessionId,
    List<Map<String, dynamic>> tasks,
  ) async {
    final identity = _localIdentity;
    if (identity.isEmpty || tasks.isEmpty) return false;
    try {
      await _localStore.init();
      await _localStore.saveTasks(
        identity: identity,
        agentId: agentId,
        projectId: projectId,
        sessionId: sessionId,
        tasks: tasks,
      );
      return true;
    } catch (error) {
      debugPrint('[Chat] local history write failed: $error');
      return false;
    }
  }

  String _taskRevision(Iterable<Map<String, dynamic>> tasks) {
    var revision = '';
    for (final task in tasks) {
      final updatedAt =
          task['updated_at']?.toString() ??
          task['created_at']?.toString() ??
          '';
      final taskId = task['task_id']?.toString() ?? '';
      if (updatedAt.isEmpty || taskId.isEmpty) continue;
      final candidate = '$updatedAt|$taskId';
      if (candidate.compareTo(revision) > 0) revision = candidate;
    }
    return revision;
  }

  Future<Map<String, dynamic>> _syncSessionDelta(
    String sessionId,
    String? revision,
  ) async {
    if (revision == null || revision.isEmpty) {
      final tasks = await _repo.getSessionHistoryJson(
        sessionId,
        limit: _sessionHistoryPageSize + 1,
      );
      final nextRevision = _taskRevision(tasks);
      final cached = await _cacheSessionTasks(sessionId, tasks);
      if (cached && nextRevision.isNotEmpty) {
        try {
          await _localStore.saveRevision(
            identity: _localIdentity,
            sessionId: sessionId,
            revision: nextRevision,
          );
        } catch (error) {
          debugPrint('[Chat] local history revision write failed: $error');
        }
      }
      return {
        'tasks': tasks,
        'has_more': tasks.length > _sessionHistoryPageSize,
      };
    }

    late final List<CachedChatTask> cached;
    try {
      cached = await _localStore.readTasks(
        identity: _localIdentity,
        agentId: agentId,
        projectId: projectId,
        sessionId: sessionId,
        limit: 200,
      );
    } catch (error) {
      debugPrint('[Chat] local history merge read failed: $error');
      return _syncSessionDelta(sessionId, null);
    }
    if (cached.isEmpty) {
      return _syncSessionDelta(sessionId, null);
    }

    final tasks = <Map<String, dynamic>>[];
    var deltaCursor = revision;
    var nextRevision = revision;
    var deltaHasMore = false;
    do {
      final delta = await _repo.getSessionTaskDeltaJson(
        sessionId,
        afterRevision: deltaCursor,
      );
      final page = (delta['tasks'] as List<dynamic>? ?? const [])
          .whereType<Map<String, dynamic>>()
          .toList(growable: false);
      tasks.addAll(page);
      deltaHasMore = delta['has_more'] == true;
      final pageRevision = delta['revision']?.toString() ?? '';
      if (pageRevision.isNotEmpty) nextRevision = pageRevision;
      if (!deltaHasMore || page.isEmpty) break;
      if (pageRevision.isEmpty || pageRevision == deltaCursor) {
        throw const FormatException('Invalid session delta cursor');
      }
      deltaCursor = pageRevision;
    } while (true);

    final cacheUpdated = tasks.isEmpty
        ? true
        : await _cacheSessionTasks(sessionId, tasks);
    final merged = <String, Map<String, dynamic>>{
      for (final item in cached) item.taskId: item.payload,
      for (final task in tasks)
        if (task['task_id']?.toString().isNotEmpty == true)
          task['task_id'].toString(): task,
    };
    if (cacheUpdated && nextRevision.isNotEmpty) {
      try {
        await _localStore.saveRevision(
          identity: _localIdentity,
          sessionId: sessionId,
          revision: nextRevision,
        );
      } catch (error) {
        debugPrint('[Chat] local history revision write failed: $error');
      }
    }
    return {
      'revision': nextRevision,
      'tasks': merged.values.toList(growable: false),
      'has_more': merged.length > _sessionHistoryPageSize,
    };
  }

  void _persistTaskEvent(
    String taskId,
    String eventKey,
    Map<String, dynamic> event,
  ) {
    final identity = _localIdentity;
    if (identity.isEmpty) return;
    _eventWriter.add(LocalChatEvent(identity, taskId, eventKey, event));
  }

  int _nextTaskMessageIndex(String taskId) {
    var next = 0;
    for (final message in state.messages) {
      if (message.taskId != taskId) continue;
      final index = message.taskMessageIndex;
      if (index != null && index >= next) next = index + 1;
    }
    return next;
  }

  void _logChatLatency(
    String stage, {
    String? taskId,
    DateTime? startedAt,
    Map<String, dynamic>? data,
  }) {
    final payload = <String, dynamic>{
      'stage': stage,
      'agent_id': agentId,
      'project_id': projectId,
      'session_id': state.currentSessionId,
      if (taskId != null && taskId.isNotEmpty) 'task_id': taskId,
      if (startedAt != null) 'elapsed_ms': _elapsedMsSince(startedAt),
      ...?data,
    };
    debugPrint('[Latency][Chat] $payload');
    unawaited(_writeChatDiagnostic('chat_latency', payload));
  }

  void _logChatDiag(
    String stage, {
    String? taskId,
    Map<String, dynamic>? data,
  }) {
    final sessionId = mounted ? state.currentSessionId : null;
    final payload = <String, dynamic>{
      'stage': stage,
      'session_id': sessionId,
      if (taskId != null && taskId.isNotEmpty) 'task_id': taskId,
      ...?data,
    };
    debugPrint('[ChatDiag] $stage $payload');
    unawaited(_writeChatDiagnostic('chat_diag', payload));
  }

  Future<void> _writeChatDiagnostic(
    String event,
    Map<String, dynamic> payload,
  ) async {
    try {
      await AppLogService.log(event, data: payload);
    } catch (error) {
      // A removed/rotated log file or full disk must not interrupt live chat.
      debugPrint('[ChatDiag] log write failed: $error');
    }
  }

  String _sseCountsSummary(Map<String, int> counts) {
    if (counts.isEmpty) return '无';
    final entries = counts.entries.toList()
      ..sort((a, b) => b.value.compareTo(a.value));
    return entries.map((entry) => '${entry.key}=${entry.value}').join('、');
  }

  GoalProgressEntry? _latestGoalProgressEntry() {
    for (final message in state.messages.reversed) {
      if (message.role != MessageRole.agent || message.goalProgress.isEmpty) {
        continue;
      }
      return message.goalProgress.last;
    }
    return null;
  }

  void addAttachedFile(AttachedFile file) {
    state = state.copyWith(attachedFiles: [...state.attachedFiles, file]);
  }

  void removeAttachedFile(int index) {
    final files = List<AttachedFile>.from(state.attachedFiles);
    files.removeAt(index);
    state = state.copyWith(attachedFiles: files);
  }

  void clearAttachedFiles() {
    state = state.copyWith(attachedFiles: const []);
  }

  List<AiArtifact> _parseArtifacts(dynamic raw) {
    if (raw is! List) return const [];
    return raw
        .whereType<Map<String, dynamic>>()
        .map(AiArtifact.fromJson)
        .where((artifact) => artifact.id.isNotEmpty)
        .toList();
  }

  List<AiOutputFile> _parseFiles(dynamic raw) {
    if (raw is! List) return const [];
    return raw
        .whereType<Map<String, dynamic>>()
        .map(AiOutputFile.fromJson)
        // 视频与图片一样是生成结果：只留图片会把生视频的结果丢掉，
        // 气泡里就什么都不显示。
        .where((file) => (file.isImage || file.isVideo) && file.url.isNotEmpty)
        .toList();
  }

  String _formatRetryHint(Map<String, dynamic> event) {
    final attempt = (event['attempt'] as num?)?.toInt();
    final message = (event['content'] as String?)?.trim().isNotEmpty == true
        ? (event['content'] as String).trim()
        : ((event['message'] as String?)?.trim() ?? '');
    final next = (event['next'] as num?)?.toInt();
    final prefix = attempt != null && attempt > 0
        ? '连接模型失败，正在重试（第 $attempt 次）'
        : '连接模型失败，正在重试';
    if (next != null) {
      final waitSeconds =
          ((next - DateTime.now().millisecondsSinceEpoch) / 1000).ceil();
      if (waitSeconds > 0) {
        if (message.isNotEmpty) {
          return '$prefix，约 ${waitSeconds}s 后继续：$message';
        }
        return '$prefix，约 ${waitSeconds}s 后继续';
      }
    }
    if (message.isNotEmpty) {
      return '$prefix：$message';
    }
    return prefix;
  }

  int _compareTaskTimeline(TaskModel a, TaskModel b) {
    final createdA = a.createdAt ?? DateTime.fromMillisecondsSinceEpoch(0);
    final createdB = b.createdAt ?? DateTime.fromMillisecondsSinceEpoch(0);
    final createdCompare = createdA.compareTo(createdB);
    if (createdCompare != 0) return createdCompare;
    return a.taskId.compareTo(b.taskId);
  }

  ({List<ChatMessage> messages, List<String> snapshotTaskIds})
  _messagesFromHistoryTasks(
    List<TaskModel> sortedTasks, {
    bool hydrateLatest = true,
  }) {
    final msgs = <ChatMessage>[];
    final snapshotTaskIds = <String>[];
    for (final task in sortedTasks) {
      final taskCreatedAt =
          task.createdAt ?? DateTime.fromMillisecondsSinceEpoch(0);
      final userText = task.userText;
      final attachedFiles = task.attachedFiles;
      // 只发附件、不写字的消息也要保留：靠附件才能还原这一轮的用户输入。
      if ((userText != null && userText.isNotEmpty) || attachedFiles.isNotEmpty) {
        msgs.add(
          ChatMessage(
            id: 'user_${task.taskId}',
            role: MessageRole.user,
            state: MessageState.done,
            content: userText ?? '',
            createdAt: taskCreatedAt,
            taskId: task.taskId,
            taskCreatedAt: taskCreatedAt,
            taskMessageIndex: 0,
            attachedFiles: attachedFiles,
          ),
        );
      }

      String agentContent = task.result ?? '';
      String thinkingContent = '';
      final isActive = _isTaskActive(task.status);
      String statusHint = '';
      // Only the newest task is hydrated automatically. Older bodies remain
      // immediately readable; their tool details are explicitly requested.
      if (hydrateLatest &&
          task.taskId == sortedTasks.last.taskId &&
          agentContent.length <= _maxAutoHydrateResultChars) {
        snapshotTaskIds.add(task.taskId);
      }
      final isCancelling = task.status == TaskStatus.cancelling;
      final isCancelled = task.status == TaskStatus.cancelled;
      if (agentContent.isEmpty &&
          (task.status == TaskStatus.failed || isCancelling || isCancelled) &&
          task.error != null &&
          task.error!.isNotEmpty) {
        agentContent = formatChatTaskError(task.error);
      }
      final msgState = task.status == TaskStatus.failed || isCancelling
          ? MessageState.failed
          : isCancelled
          ? MessageState.done
          : (isActive ? MessageState.streaming : MessageState.done);
      // 生图轮次在重建时必须还原生图状态：否则既没有生图进度卡片，
      // 气泡又停在流式态，看起来就是「一直等待新的回复」。
      final taskModelRef = task.modelRef;
      final imageGeneration =
          isActive &&
              taskModelRef.isNotEmpty &&
              looksLikeImageGenerationModel(
                taskModelRef.split('/').first,
                taskModelRef.split('/').last,
                taskModelRef,
              )
          ? ImageGenerationInfo.fromModelRef(taskModelRef, taskCreatedAt)
          : null;
      msgs.add(
        ChatMessage(
          id: 'agent_${task.taskId}',
          role: MessageRole.agent,
          state: msgState,
          content: agentContent,
          thinkingContent: thinkingContent,
          historyDetails: snapshotTaskIds.contains(task.taskId)
              ? HistoryDetailsState.loading
              : HistoryDetailsState.deferred,
          statusHint: statusHint,
          streamRecovery: isActive
              ? StreamRecovery.syncing
              : StreamRecovery.none,
          errorDetail: task.status == TaskStatus.failed
              ? _taskErrorDetail(task, const TaskEventSnapshot())
              : '',
          createdAt: taskCreatedAt,
          taskId: task.taskId,
          taskCreatedAt: taskCreatedAt,
          taskMessageIndex: 1,
          imageGeneration: imageGeneration,
          artifacts: task.artifacts,
          plan: task.plan,
          hasPlanBinding: task.plan != null && !task.plan!.isEmpty,
        ),
      );
    }
    return (messages: msgs, snapshotTaskIds: snapshotTaskIds);
  }

  void _updateHistoryCursor(List<TaskModel> sortedTasks) {
    if (sortedTasks.isEmpty) return;
    final oldestTask = sortedTasks.first;
    _historyCursorCreatedAt = oldestTask.createdAt;
    _historyCursorTaskId = oldestTask.taskId;
  }

  // 切换/加载历史会话
  Future<void> loadSession(String sessionId) async {
    await AppLogService.log(
      'chat_session_load_started',
      data: {
        'agent_id': agentId,
        'project_id': projectId,
        'session_id': sessionId,
      },
    );
    if (_disposed) return;
    // 重建会话前先留住直播间已经产出的内容：重建只依据任务索引（result），
    // 对活动任务直接替换会把思考/工具/正文一次清空。
    final previousMessages = state.messages;
    _cancelEventSub();
    _cancelQueueSub();
    replySyncProgress = null;
    final loadVersion = ++_sessionLoadVersion;
    _handledTaskEventKeys.clear();
    _activeTaskAgentMessageIds.clear();
    _snapshotRetryKeys.clear();
    _historyCursorCreatedAt = null;
    _historyCursorTaskId = null;
    _authoritativeSessionLoadVersion = null;
    state = ChatState(
      currentSessionId: sessionId,
      sending: true,
      queueLoading: true,
    );
    unawaited(_loadCachedSession(sessionId, loadVersion));
    try {
      String? revision;
      try {
        await _localStore.init();
        if (_localIdentity.isNotEmpty) {
          revision = await _localStore.readRevision(
            identity: _localIdentity,
            sessionId: sessionId,
          );
        }
      } catch (error) {
        // Local history is only a startup optimization. A plugin/database
        // failure must not suppress the authoritative server history.
        debugPrint('[Chat] local history cursor unavailable: $error');
      }
      final syncResult = await _syncSessionDelta(sessionId, revision);
      final taskJson = (syncResult['tasks'] as List<dynamic>? ?? const [])
          .whereType<Map<String, dynamic>>()
          .toList(growable: true);
      final tasks = taskJson.map(TaskModel.fromJson).toList(growable: false);
      if (!_isCurrentSessionLoad(sessionId, loadVersion)) return;
      final sortedTasks = [...tasks]..sort(_compareTaskTimeline);
      if (sortedTasks.length > _sessionHistoryPageSize) {
        sortedTasks.removeRange(
          0,
          sortedTasks.length - _sessionHistoryPageSize,
        );
      }
      _updateHistoryCursor(sortedTasks);
      var latestTask = sortedTasks.isNotEmpty ? sortedTasks.last : null;
      // The local revision is an optimization only. Refresh an apparently
      // active latest task before rendering it so a completed server task
      // cannot remain a streaming bubble when the delta cursor is stale.
      if (latestTask != null && _isTaskActive(latestTask.status)) {
        try {
          final refreshed = await _repo.getTask(latestTask.taskId);
          final index = sortedTasks.indexWhere(
            (task) => task.taskId == refreshed.taskId,
          );
          if (index >= 0) {
            sortedTasks[index] = refreshed;
            latestTask = refreshed;
          }
        } catch (_) {}
      }
      final history = _messagesFromHistoryTasks(sortedTasks);
      final latestTaskIsActive =
          latestTask != null &&
          (_isTaskActive(latestTask.status) ||
              latestTask.status == TaskStatus.cancelling);
      final latestActiveTask =
          latestTask != null && _isTaskActive(latestTask.status)
          ? latestTask
          : null;
      final liveTaskIds = <String>{
        for (final task in sortedTasks)
          if (_isTaskActive(task.status) ||
              task.status == TaskStatus.cancelling)
            task.taskId,
      };
      state = state.copyWith(
        messages: _mergeLiveTaskContent(
          history.messages,
          previousMessages,
          liveTaskIds,
        ),
        sending: latestActiveTask != null,
        taskActive: latestTaskIsActive,
        pendingApproval: latestActiveTask?.approval,
        pendingApprovalTaskId: latestActiveTask?.approval != null
            ? latestActiveTask?.taskId
            : null,
        pendingQuestion: latestActiveTask?.question,
        pendingQuestionTaskId: latestActiveTask?.question != null
            ? latestActiveTask?.taskId
            : null,
        hasEarlierHistory:
            (syncResult['has_more'] as bool?) ??
            tasks.length > _sessionHistoryPageSize,
      );
      _authoritativeSessionLoadVersion = loadVersion;
      await _reconcileTerminalStreamingMessages(
        sessionId: sessionId,
        loadVersion: loadVersion,
      );
      if (!_isCurrentSessionLoad(sessionId, loadVersion)) return;
      final taskToWatch = latestTaskIsActive ? latestTask : null;
      final agentMessageIdToWatch = taskToWatch != null
          ? 'agent_${taskToWatch.taskId}'
          : null;
      if (taskToWatch != null && agentMessageIdToWatch != null) {
        _currentTaskId = taskToWatch.taskId;
        // The live subscription is the source of truth for an active task.
        // Do not keep the bubble in the historical-sync animation while a
        // potentially large Agent history is being hydrated.
        _updateMessage(agentMessageIdToWatch, (message) {
          if (message.streamRecovery == StreamRecovery.syncing) {
            message.streamRecovery = StreamRecovery.following;
          }
        });
        _listenEvents(
          taskToWatch.taskId,
          agentMessageIdToWatch,
          taskToWatch.createdAt ?? DateTime.now(),
        );
      }
      unawaited(_startQueueSync(sessionId));
      await AppLogService.log(
        'chat_session_load_completed',
        data: {
          'agent_id': agentId,
          'project_id': projectId,
          'session_id': sessionId,
          'message_count': history.messages.length,
          'active_task_id': taskToWatch?.taskId,
          'has_earlier_history': state.hasEarlierHistory,
        },
      );
      if (history.snapshotTaskIds.isNotEmpty) {
        await _hydrateSessionSnapshots(
          sessionId: sessionId,
          loadVersion: loadVersion,
          taskIds: history.snapshotTaskIds,
        );
      }
    } catch (e) {
      await AppLogService.log(
        'chat_session_load_failed',
        level: 'error',
        data: {
          'agent_id': agentId,
          'project_id': projectId,
          'session_id': sessionId,
          'error': e.toString(),
        },
      );
      if (_isCurrentSessionLoad(sessionId, loadVersion)) {
        state = state.copyWith(
          sending: false,
          taskActive: false,
          queueLoading: false,
          error: '加载历史失败: $e',
        );
      }
    }
  }

  Future<int> loadEarlierSessionHistory() async {
    final sessionId = state.currentSessionId;
    final beforeCreatedAt = _historyCursorCreatedAt;
    final beforeTaskId = _historyCursorTaskId;
    if (sessionId == null ||
        sessionId.isEmpty ||
        beforeCreatedAt == null ||
        beforeTaskId == null ||
        state.loadingEarlierHistory ||
        !state.hasEarlierHistory) {
      return 0;
    }

    final loadVersion = _sessionLoadVersion;
    state = state.copyWith(loadingEarlierHistory: true, clearError: true);
    try {
      final tasks = await _repo.getSessionHistory(
        sessionId,
        limit: _sessionHistoryPageSize + 1,
        beforeCreatedAt: beforeCreatedAt,
        beforeTaskId: beforeTaskId,
      );
      if (!_isCurrentSessionLoad(sessionId, loadVersion)) return 0;

      final pageTasks = tasks.take(_sessionHistoryPageSize).toList();
      final sortedTasks = [...pageTasks]..sort(_compareTaskTimeline);
      _updateHistoryCursor(sortedTasks);
      final history = _messagesFromHistoryTasks(
        sortedTasks,
        hydrateLatest: false,
      );
      final existingIds = state.messages.map((message) => message.id).toSet();
      final prependMessages = history.messages
          .where((message) => !existingIds.contains(message.id))
          .toList(growable: false);
      state = state.copyWith(
        messages: [...prependMessages, ...state.messages],
        hasEarlierHistory: tasks.length > _sessionHistoryPageSize,
      );
      if (!_isCurrentSessionLoad(sessionId, loadVersion)) return 0;
      state = state.copyWith(loadingEarlierHistory: false);
      await AppLogService.log(
        'chat_session_earlier_history_loaded',
        data: {
          'agent_id': agentId,
          'project_id': projectId,
          'session_id': sessionId,
          'task_count': tasks.length,
          'message_count': prependMessages.length,
          'has_earlier_history': state.hasEarlierHistory,
        },
      );
      return prependMessages.length;
    } catch (e) {
      if (_isCurrentSessionLoad(sessionId, loadVersion)) {
        state = state.copyWith(
          loadingEarlierHistory: false,
          error: '加载更早历史失败: $e',
        );
      }
      return 0;
    } finally {
      // The request can become stale when sending, switching sessions, or
      // reconnecting increments _sessionLoadVersion. Clear the transaction
      // flag for the same session even on those early-return paths; otherwise
      // the UI can remain permanently stuck in "loading earlier history".
      if (!_disposed &&
          state.currentSessionId == sessionId &&
          state.loadingEarlierHistory) {
        state = state.copyWith(loadingEarlierHistory: false);
      }
    }
  }

  void newSession() {
    replySyncProgress = null;
    unawaited(
      AppLogService.log(
        'chat_new_session',
        data: {'agent_id': agentId, 'project_id': projectId},
      ),
    );
    _cancelEventSub();
    _cancelQueueSub();
    _sessionReadyCompleter = null;
    _sessionLoadVersion++;
    _historyCursorCreatedAt = null;
    _historyCursorTaskId = null;
    state = const ChatState();
  }

  Future<bool> compactContext({ModelInfo? model}) async {
    final sessionId = state.currentSessionId?.trim() ?? '';
    if (sessionId.isEmpty || state.taskActive || state.sending) return false;

    final startedAt = DateTime.now();
    final placeholderId =
        'agent_compaction_${startedAt.millisecondsSinceEpoch}';
    final placeholder = ChatMessage(
      id: placeholderId,
      role: MessageRole.agent,
      state: MessageState.streaming,
      content: '',
      statusHint: compactionStatusHint,
      createdAt: startedAt,
    );
    state = state.copyWith(
      messages: [...state.messages, placeholder],
      sending: true,
      taskActive: true,
      clearError: true,
    );

    try {
      final task = await _repo.compactContext(
        agentId: agentId,
        projectId: projectId,
        sessionId: sessionId,
        model: model,
      );
      final messages = [...state.messages];
      final index = messages.indexWhere(
        (message) => message.id == placeholderId,
      );
      if (index >= 0) {
        messages[index] = ChatMessage(
          id: placeholderId,
          role: MessageRole.agent,
          state: MessageState.streaming,
          content: '',
          statusHint: compactionStatusHint,
          createdAt: startedAt,
          taskId: task.taskId,
          taskCreatedAt: task.createdAt ?? startedAt,
          taskMessageIndex: 0,
        );
      }
      state = state.copyWith(
        messages: messages,
        currentSessionId: task.sessionId ?? sessionId,
        sending: true,
        taskActive: true,
      );
      _currentTaskId = task.taskId;
      _handledTaskEventKeys.remove(task.taskId);
      _listenEvents(task.taskId, placeholderId, startedAt);
      return true;
    } catch (error) {
      _updateMessage(placeholderId, (message) {
        message.statusHint = '';
        message.content = formatChatTaskError(error.toString());
        message.state = MessageState.failed;
      });
      state = state.copyWith(
        sending: false,
        taskActive: false,
        error: formatChatTaskError(error.toString()),
      );
      return false;
    }
  }

  /// 继续执行目标：目标被用户停止或达到最大轮数而暂停后，
  /// 从上次已完成的轮次接着跑，不从头开始。
  /// 实现上是一次普通的 task.run，metadata 里带 goal_resume=true +
  /// 原 goal_id + 下一轮 iteration，relay 侧据此从断点继续。
  Future<bool> resumeGoal({
    required GoalSettings settings,
    ModelInfo? model,
    String permissionMode = 'ask',
    String? variant,
  }) async {
    if (state.taskActive) return false;
    final objective = settings.content.trim();
    if (objective.isEmpty) return false;
    final latest = _latestGoalProgressEntry();
    if (latest == null || latest.type != 'goal_paused') return false;
    if (latest.isTransientRetry) return false;
    final nextIteration = latest.iteration + 1;
    return sendMessage(
      '继续执行目标：$objective',
      model: model,
      permissionMode: permissionMode,
      variant: variant,
      goalOverride: objective,
      goalMaxIterations: settings.maxIterations,
      forceGoal: true,
      extraMetadata: {
        'goal_resume': 'true',
        'goal_id': latest.goalId,
        'goal_iteration': nextIteration.toString(),
      },
    );
  }

  Future<bool> sendMessage(
    String text, {
    ModelInfo? model,
    String permissionMode = 'ask',
    String? variant,
    List<AttachedFile>? attachedFilesOverride,
    String? goalOverride,
    int? goalMaxIterations,
    ImageGenerationOptions? imageOptions,
    bool forceGoal = false,
    Map<String, String>? extraMetadata,
  }) async {
    debugPrint(
      '[Chat] sendMessage called: "$text", sending=${state.sending}, taskActive=${state.taskActive}',
    );
    if (text.trim().isEmpty) return false;

    final commandGoal = _parseGoalCommand(text);
    final configuredGoal = goalOverride?.trim();
    final latestGoal = _latestGoalProgressEntry();
    // 继续目标（forceGoal）要越过 terminal 检查：上次目标停在 paused
    // 正是继续的前提，不能被「目标已终止就不自动带 goal」挡掉。
    final allowConfiguredGoal =
        forceGoal || latestGoal?.isTerminal != true;
    final goal =
        commandGoal ??
        (allowConfiguredGoal &&
                configuredGoal != null &&
                configuredGoal.isNotEmpty
            ? configuredGoal
            : null);
    final sendText = commandGoal ?? text;
    // 生图/生视频参数走任务 metadata 的独立字段（见 ImageGenerationOptions
    // .toMetadata），不再拼进提示词：提示词保持用户输入原样，历史重建、
    // 复制和日志都不会带出 --n=2 这类开关。
    final promptText = sendText;
    final filesToSend = List<AttachedFile>.from(
      attachedFilesOverride ?? state.attachedFiles,
    );
    final modelToSend = model;
    // 加入 agent 流式气泡（占位），记录发送时刻用于首字计时
    final sentAt = DateTime.now();
    final imageGeneration =
        modelToSend?.supportsImageOutput == true ||
            modelToSend?.supportsVideoOutput == true
        ? ImageGenerationInfo.fromModel(modelToSend!, sentAt)
        : null;

    // Refresh the latest task when the local flag says idle. This covers a
    // reconnect/background race without turning every follow-up message in a
    // completed session into a queued message.
    var shouldQueue = state.taskActive;
    final sessionId = state.currentSessionId?.trim() ?? '';
    if (!shouldQueue && sessionId.isNotEmpty) {
      try {
        final latestTasks = await _repo.getSessionHistory(sessionId, limit: 1);
        shouldQueue = latestTasks.any((task) => _isTaskActive(task.status));
      } catch (_) {
        // Keep the existing direct-send path when the refresh is unavailable.
      }
    }
    if (shouldQueue) {
      state = state.copyWith(attachedFiles: const [], clearQueueError: true);
      final queued = await _enqueueMessage(
        text: promptText,
        attachedFiles: filesToSend,
        model: modelToSend,
        permissionMode: permissionMode,
        variant: variant,
        goal: goal,
        goalMaxIterations:
            goalMaxIterations ?? GoalSettings.defaultMaxIterations,
        imageOptions: imageOptions,
        extraMetadata: extraMetadata,
      );
      if (!queued) {
        final current = List<AttachedFile>.from(state.attachedFiles);
        final currentIds = current.map((file) => file.id).toSet();
        state = state.copyWith(
          attachedFiles: [
            ...filesToSend.where((file) => !currentIds.contains(file.id)),
            ...current,
          ],
        );
      }
      return queued;
    }

    if (state.currentSessionId == null || state.currentSessionId!.isEmpty) {
      _sessionReadyCompleter = Completer<String>();
    }

    // 加入用户气泡
    final prior = [
      for (final message in state.messages) message.copyForUpdate(),
    ];
    for (final message in prior) {
      if (message.role != MessageRole.agent) continue;
      _finalizeRunningTools(message);
      if (message.state == MessageState.streaming) {
        message.state = MessageState.done;
      }
    }

    final userMsg = ChatMessage(
      id: 'user_${DateTime.now().millisecondsSinceEpoch}',
      role: MessageRole.user,
      state: MessageState.done,
      content: sendText,
      // 用户消息和 Agent 占位消息属于同一轮，使用同一个时间基准，避免
      // State.copyWith 的时间排序将刚创建的 Agent 排到用户消息前面。
      createdAt: sentAt,
      attachedFiles: filesToSend,
    );

    final agentMsg = ChatMessage(
      id: 'agent_${DateTime.now().millisecondsSinceEpoch}',
      role: MessageRole.agent,
      state: MessageState.streaming,
      content: '',
      imageGeneration: imageGeneration,
      createdAt: sentAt,
    );

    state = state.copyWith(
      messages: [...prior, userMsg, agentMsg],
      sending: true,
      taskActive: true,
      clearError: true,
      attachedFiles: const [],
    );

    try {
      _logChatLatency(
        'send_started',
        startedAt: sentAt,
        data: {
          'text_length': sendText.length,
          'goal_mode': goal != null,
          'attached_file_count': filesToSend.length,
          'model': modelToSend?.metaKey,
          'permission_mode': permissionMode,
          'variant': variant,
        },
      );
      await AppLogService.log(
        'chat_send_started',
        data: {
          'agent_id': agentId,
          'project_id': projectId,
          'session_id': state.currentSessionId,
          'text_length': sendText.length,
          'goal_mode': goal != null,
          'attached_file_count': filesToSend.length,
          'model': modelToSend?.metaKey,
          'permission_mode': permissionMode,
          'variant': variant,
        },
      );
      debugPrint(
        '[Chat] createTask agentId=$agentId projectId=$projectId sessionId=${state.currentSessionId}',
      );
      final createTaskStartedAt = DateTime.now();
      final task = await _repo.createTask(
        agentId: agentId,
        projectId: projectId,
        text: promptText,
        sessionId: state.currentSessionId,
        attachedFiles: filesToSend.isNotEmpty ? filesToSend : null,
        model: modelToSend,
        permissionMode: permissionMode,
        variant: variant,
        goal: goal,
        goalMaxIterations:
            goalMaxIterations ?? GoalSettings.defaultMaxIterations,
        imageOptions: imageOptions,
        extraMetadata: extraMetadata,
      );
      _logChatLatency(
        'create_task_completed',
        taskId: task.taskId,
        startedAt: sentAt,
        data: {
          'create_task_ms': _elapsedMsSince(createTaskStartedAt),
          'task_status': task.status.name,
          'task_session_id': task.sessionId,
          'task_created_at': task.createdAt?.toIso8601String(),
        },
      );

      final taskCreatedAt = task.createdAt ?? sentAt;
      final newUserMsg = ChatMessage(
        id: userMsg.id,
        role: MessageRole.user,
        state: userMsg.state,
        content: userMsg.content,
        createdAt: userMsg.createdAt,
        taskId: task.taskId,
        taskCreatedAt: taskCreatedAt,
        taskMessageIndex: 0,
        attachedFiles: userMsg.attachedFiles,
      );

      // Update both bubbles with the server task identity and creation time.
      final newAgentMsg = ChatMessage(
        id: agentMsg.id,
        role: MessageRole.agent,
        state: MessageState.streaming,
        content: '',
        imageGeneration: imageGeneration,
        createdAt: agentMsg.createdAt,
        taskId: task.taskId,
        taskCreatedAt: taskCreatedAt,
        taskMessageIndex: 1,
      );

      final msgs = [...state.messages];
      final userIdx = msgs.indexWhere((m) => m.id == userMsg.id);
      if (userIdx >= 0) msgs[userIdx] = newUserMsg;
      final idx = msgs.indexWhere((m) => m.id == agentMsg.id);
      if (idx >= 0) msgs[idx] = newAgentMsg;

      state = state.copyWith(
        messages: msgs,
        currentSessionId: task.sessionId ?? state.currentSessionId,
        taskActive: true,
      );
      final taskSessionId = (task.sessionId ?? state.currentSessionId)?.trim();
      if (taskSessionId != null && taskSessionId.isNotEmpty) {
        _completeSessionReady(taskSessionId);
        unawaited(_startQueueSync(taskSessionId));
      }
      await AppLogService.log(
        'chat_send_task_created',
        data: {
          'agent_id': agentId,
          'project_id': projectId,
          'task_id': task.taskId,
          'session_id': task.sessionId ?? state.currentSessionId,
        },
      );

      _sessionLoadVersion++;

      _currentTaskId = task.taskId;
      _handledTaskEventKeys.remove(task.taskId);
      _listenEvents(task.taskId, newAgentMsg.id, sentAt);
      if (_pendingCancelBeforeTaskCreated || state.cancelRequested) {
        _pendingCancelBeforeTaskCreated = false;
        unawaited(_requestTaskCancel(task.taskId));
      }
      return true;
    } catch (e, st) {
      _pendingCancelBeforeTaskCreated = false;
      _logChatLatency(
        'send_failed',
        startedAt: sentAt,
        data: {'error': e.toString()},
      );
      await AppLogService.log(
        'chat_send_failed',
        level: 'error',
        data: {
          'agent_id': agentId,
          'project_id': projectId,
          'session_id': state.currentSessionId,
          'error': e.toString(),
        },
      );
      debugPrint('[Chat] sendMessage error: $e\n$st');
      _updateMessage(agentMsg.id, (m) {
        m.state = MessageState.failed;
        m.content = '发送失败: ${formatChatTaskError(e.toString())}';
      });
      state = state.copyWith(
        sending: false,
        taskActive: false,
        error: e.toString(),
        cancelRequested: false,
        attachedFiles: filesToSend,
      );
      _completeSessionReady('');
      return false;
    }
  }

  Future<bool> _enqueueMessage({
    required String text,
    required List<AttachedFile> attachedFiles,
    required ModelInfo? model,
    required String permissionMode,
    required String? variant,
    required String? goal,
    required int goalMaxIterations,
    ImageGenerationOptions? imageOptions,
    Map<String, String>? extraMetadata,
  }) async {
    try {
      var sessionId = state.currentSessionId?.trim() ?? '';
      if (sessionId.isEmpty) {
        final ready = _sessionReadyCompleter;
        if (ready == null) {
          throw StateError('当前会话尚未就绪');
        }
        sessionId = await ready.future;
      }
      if (sessionId.isEmpty) {
        throw StateError('首条消息发送失败，队列消息未创建');
      }
      final item = await _repo.createChatQueueItem(
        sessionId: sessionId,
        agentId: agentId,
        projectId: projectId,
        text: text,
        attachedFiles: attachedFiles.isEmpty ? null : attachedFiles,
        model: model,
        permissionMode: permissionMode,
        variant: variant,
        goal: goal,
        goalMaxIterations: goalMaxIterations,
        imageOptions: imageOptions,
        extraMetadata: extraMetadata,
      );
      _mergeQueueItem(item);
      unawaited(_startQueueSync(sessionId));
      await AppLogService.log(
        'chat_queue_item_created',
        data: {
          'queue_item_id': item.id,
          'session_id': sessionId,
          'agent_id': agentId,
          'project_id': projectId,
          'model': item.modelRef,
          'variant': item.variant,
        },
      );
      state = state.copyWith(attachedFiles: const []);
      return true;
    } catch (e) {
      state = state.copyWith(queueError: '加入发送队列失败: $e');
      return false;
    }
  }

  Future<void> deleteQueueItem(ChatQueueItem item) async {
    if (!item.isEditable) return;
    try {
      await _repo.deleteChatQueueItem(item);
      final nextItems = state.queue.items
          .where((current) => current.id != item.id)
          .toList(growable: false);
      state = state.copyWith(
        queue: ChatQueueSnapshot(
          sessionId: state.queue.sessionId,
          version: state.queue.version,
          items: nextItems,
        ),
        clearQueueError: true,
      );
    } catch (e) {
      state = state.copyWith(queueError: '删除队列消息失败: $e');
      await refreshQueue();
    }
  }

  Future<void> updateQueueItemModel(
    ChatQueueItem item, {
    ModelInfo? model,
    String? variant,
  }) async {
    if (!item.isEditable) return;
    try {
      final updated = await _repo.updateChatQueueItem(
        item,
        model: model?.metaKey ?? '',
        variant: variant ?? '',
      );
      _mergeQueueItem(updated);
    } catch (e) {
      state = state.copyWith(queueError: '更新队列模型失败: $e');
      await refreshQueue();
    }
  }

  Future<void> reorderQueueItems(List<ChatQueueItem> orderedItems) async {
    final sessionId = state.currentSessionId?.trim() ?? '';
    if (sessionId.isEmpty || orderedItems.any((item) => !item.isEditable)) {
      return;
    }
    try {
      final snapshot = await _repo.reorderChatQueue(sessionId, orderedItems);
      _applyQueueSnapshot(snapshot);
    } catch (e) {
      state = state.copyWith(queueError: '调整队列顺序失败: $e');
      await refreshQueue();
    }
  }

  Future<void> insertQueueItem(ChatQueueItem item) async {
    if (!state.taskActive || !item.isEditable) return;
    try {
      final updated = await _repo.insertChatQueueItem(item);
      _mergeQueueItem(updated, addIfMissing: false);
    } catch (e) {
      state = state.copyWith(queueError: '插入当前对话失败: $e');
      await refreshQueue();
    }
  }

  Future<void> sendQueueItem(ChatQueueItem item) async {
    if (state.taskActive || !item.isEditable) return;
    try {
      final dispatched = await _repo.sendChatQueueItem(item);
      _mergeQueueItem(dispatched);
    } catch (e) {
      state = state.copyWith(queueError: '发送队列消息失败: $e');
      await refreshQueue();
    }
  }

  Future<void> refreshQueue({int? syncVersion}) async {
    final sessionId = state.currentSessionId?.trim() ?? '';
    if (sessionId.isEmpty) return;
    try {
      final snapshot = await _repo.getChatQueue(sessionId);
      if (syncVersion != null && syncVersion != _queueSyncVersion) return;
      _applyQueueSnapshot(snapshot);
    } catch (e) {
      if (_queueSessionId == sessionId &&
          (syncVersion == null || syncVersion == _queueSyncVersion)) {
        if (syncVersion != null &&
            !_queueInitialSyncPending &&
            state.queue.sessionId == sessionId) {
          return;
        }
        if (syncVersion != null && _queueInitialSyncPending) {
          _queueInitialSyncFailures++;
          if (_queueInitialSyncFailures < _queueInitialSyncFailureLimit) {
            state = state.copyWith(queueLoading: true, clearQueueError: true);
            _scheduleQueueReconnect(sessionId);
            return;
          }
        }
        _queueInitialSyncPending = false;
        // 队列为空时不向用户暴露同步失败：没有待发送消息时，
        // 显示「队列状态异常」只会造成误解。
        if (state.queue.queuedItems.isEmpty) {
          state = state.copyWith(queueLoading: false, clearQueueError: true);
        } else {
          state = state.copyWith(
            queueLoading: false,
            queueError: '同步发送队列失败: $e',
          );
        }
      }
    }
  }

  void _completeSessionReady(String sessionId) {
    final ready = _sessionReadyCompleter;
    if (ready != null && !ready.isCompleted) ready.complete(sessionId);
  }

  Future<void> _startQueueSync(String sessionId, {bool force = false}) async {
    final normalized = sessionId.trim();
    if (!_detailsEnabled || normalized.isEmpty) return;
    if (!force && _queueSessionId == normalized && _queueSub != null) return;
    if (_queueSessionId != normalized) {
      _queueInitialSyncFailures = 0;
    }
    final syncVersion = ++_queueSyncVersion;
    _queueReconnectTimer?.cancel();
    await _queueSub?.cancel();
    if (!_detailsEnabled || syncVersion != _queueSyncVersion) return;
    _queueSessionId = normalized;
    _queueInitialSyncPending = true;
    state = state.copyWith(queueLoading: true, clearQueueError: true);
    var reconnectAfterInitialSync = false;
    _queueSub = _repo
        .watchChatQueue(normalized)
        .listen(
          (snapshot) {
            if (syncVersion != _queueSyncVersion) return;
            _applyQueueSnapshot(snapshot);
          },
          onError: (Object error, StackTrace stackTrace) {
            if (_queueSessionId != normalized ||
                _disposed ||
                syncVersion != _queueSyncVersion) {
              return;
            }
            if (_queueInitialSyncPending) {
              reconnectAfterInitialSync = true;
              return;
            }
            // 进入后台导致的长连接断开属于预期行为：保持静默并安排重连，
            // 不写入用户可见的错误，避免空队列被渲染成「队列状态异常」。
            if (_appInBackground) {
              if (state.queue.sessionId == normalized) {
                _scheduleQueueReconnect(normalized);
              }
              return;
            }
            // 只有确实存在待发送消息时，才把断连暴露给用户。
            if (state.queue.queuedItems.isEmpty) {
              state = state.copyWith(
                queueLoading: false,
                clearQueueError: true,
              );
            } else {
              state = state.copyWith(
                queueLoading: false,
                queueError: '发送队列连接已断开，正在重连',
              );
            }
            if (_queueSessionId == normalized) {
              _scheduleQueueReconnect(normalized);
            }
          },
          onDone: () {
            if (_queueInitialSyncPending && syncVersion == _queueSyncVersion) {
              reconnectAfterInitialSync = true;
              return;
            }
            // 以订阅目标会话为准判断是否需要重连，避免队列快照尚未建立时
            // 因 sessionId 不匹配而永久放弃重连。
            if (_queueSessionId == normalized &&
                syncVersion == _queueSyncVersion &&
                !_disposed) {
              _scheduleQueueReconnect(normalized);
            }
          },
        );
    await refreshQueue(syncVersion: syncVersion);
    if (reconnectAfterInitialSync &&
        !_disposed &&
        _queueSessionId == normalized &&
        syncVersion == _queueSyncVersion) {
      _scheduleQueueReconnect(normalized);
    }
  }

  void _scheduleQueueReconnect(String sessionId) {
    if (!_detailsEnabled) return;
    _queueReconnectTimer?.cancel();
    _queueReconnectTimer = Timer(const Duration(seconds: 2), () {
      if (!_disposed && _queueSessionId == sessionId) {
        unawaited(_startQueueSync(sessionId, force: true));
      }
    });
  }

  void _applyQueueSnapshot(ChatQueueSnapshot snapshot) {
    if (_disposed || snapshot.sessionId != state.currentSessionId) return;
    if (snapshot.version < state.queue.version &&
        snapshot.sessionId == state.queue.sessionId) {
      return;
    }
    _queueInitialSyncPending = false;
    _queueInitialSyncFailures = 0;
    _queueReconnectTimer?.cancel();
    _queueReconnectTimer = null;
    state = state.copyWith(
      queue: snapshot,
      queueLoading: false,
      clearQueueError: true,
    );
    for (final item in snapshot.items) {
      if (item.status == ChatQueueItemStatus.dispatched &&
          item.taskId.isNotEmpty &&
          item.taskId != _currentTaskId) {
        _attachDispatchedQueueTask(item);
      }
    }
  }

  void _mergeQueueItem(ChatQueueItem item, {bool addIfMissing = true}) {
    if (item.sessionId != state.currentSessionId) return;
    final items = List<ChatQueueItem>.from(state.queue.items);
    final index = items.indexWhere((current) => current.id == item.id);
    if (index >= 0) {
      if (items[index].version > item.version) return;
      items[index] = item;
    } else if (addIfMissing) {
      items.add(item);
    } else {
      return;
    }
    items.sort((left, right) => left.position.compareTo(right.position));
    _applyQueueSnapshot(
      ChatQueueSnapshot(
        sessionId: item.sessionId,
        version: state.queue.version,
        items: items,
      ),
    );
  }

  void _attachDispatchedQueueTask(ChatQueueItem item) {
    // Queue snapshots retain dispatched items after completion. An already
    // rendered task must not be appended or subscribed to again on replay.
    if (state.messages.any(
      (message) =>
          message.taskId == item.taskId && message.role == MessageRole.agent,
    )) {
      return;
    }
    final userMessageId = 'user_${item.taskId}';
    final agentMessageId = 'agent_${item.taskId}';
    // createdAt is the enqueue time, potentially before intervening tasks.
    final taskCreatedAt = item.updatedAt ?? DateTime.now();
    final messages = List<ChatMessage>.from(state.messages);
    if (!messages.any(
      (message) =>
          message.taskId == item.taskId &&
          message.role == MessageRole.user &&
          !message.isInsertedContext,
    )) {
      messages.add(
        ChatMessage(
          id: userMessageId,
          role: MessageRole.user,
          state: MessageState.done,
          content: item.text,
          createdAt: taskCreatedAt,
          taskId: item.taskId,
          taskCreatedAt: taskCreatedAt,
          taskMessageIndex: 0,
          attachedFiles: attachedFilesFromParts(item.parts),
        ),
      );
    }
    if (!messages.any((message) => message.id == agentMessageId)) {
      messages.add(
        ChatMessage(
          id: agentMessageId,
          role: MessageRole.agent,
          state: MessageState.streaming,
          content: '',
          createdAt: taskCreatedAt,
          taskId: item.taskId,
          taskCreatedAt: taskCreatedAt,
          taskMessageIndex: 1,
        ),
      );
    }
    state = state.copyWith(
      messages: messages,
      sending: true,
      taskActive: true,
      clearApproval: true,
      clearQuestion: true,
      cancelRequested: false,
    );
    _handledTaskEventKeys.remove(item.taskId);
    _listenEvents(item.taskId, agentMessageId, DateTime.now());
  }

  void _listenEvents(
    String taskId,
    String agentMsgId,
    DateTime sentAt, {
    bool fastActiveRecovery = false,
  }) {
    debugPrint('[Chat] _listenEvents taskId=$taskId agentMsgId=$agentMsgId');
    _cancelEventSub(clearPendingCancel: false);
    _currentTaskId = taskId;
    if (!_detailsEnabled) return;
    final liveVersion = ++_liveVersion;
    _activeTaskAgentMessageIds.putIfAbsent(taskId, () => agentMsgId);
    String activeAgentMessageId() =>
        _activeTaskAgentMessageIds[taskId] ?? agentMsgId;
    void setActiveAgentMessageId(String messageId) {
      _activeTaskAgentMessageIds[taskId] = messageId;
    }

    var activeRoundStartedAt = sentAt;
    final sseListenStartedAt = DateTime.now();
    var firstSseEvent = true;
    var firstTextDeltaLogged = false;
    var firstReasoningDeltaLogged = false;
    var reachedTerminal = false;
    var goalRoundIndex = 0;
    var firstGoalRoundSeen = false;
    int? activeGoalIteration;
    final openedGoalIterations = <int>{};
    final handledEventKeys = _handledTaskEventKeys.putIfAbsent(
      taskId,
      () => <String>{},
    );

    DateTime eventTime(Map<String, dynamic> event) {
      return DateTime.tryParse(event['sent_at'] as String? ?? '') ??
          DateTime.now();
    }

    void finishActiveRound() {
      _updateMessage(activeAgentMessageId(), (m) {
        _finalizeRunningTools(m);
        if (m.state == MessageState.streaming) {
          m.statusHint = '';
          m.imageGeneration = null;
          m.finalDeliveryPhase = FinalDeliveryPhase.idle;
          m.state = MessageState.done;
        }
      });
    }

    void startGoalRound(Map<String, dynamic> event) {
      final eventIteration = _goalEventIteration(event);
      if (eventIteration != null &&
          activeGoalIteration == eventIteration &&
          openedGoalIterations.contains(eventIteration)) {
        return;
      }
      final startedAt = eventTime(event);
      final current = _messageById(activeAgentMessageId());
      final canReuseInitialBubble =
          !firstGoalRoundSeen &&
          current != null &&
          current.id == agentMsgId &&
          current.content.isEmpty &&
          current.thinkingContent.isEmpty &&
          current.goalProgress.isEmpty &&
          current.planHistory.isEmpty;

      goalRoundIndex += 1;
      if (canReuseInitialBubble) {
        firstGoalRoundSeen = true;
        activeGoalIteration = eventIteration ?? goalRoundIndex;
        openedGoalIterations.add(activeGoalIteration!);
        activeRoundStartedAt = startedAt;
        return;
      }

      finishActiveRound();
      firstGoalRoundSeen = true;
      final nextId = _nextGoalRoundMessageId(taskId, goalRoundIndex);
      final nextMessage = ChatMessage(
        id: nextId,
        role: MessageRole.agent,
        state: MessageState.streaming,
        content: '',
        createdAt: startedAt,
        taskId: taskId,
        taskCreatedAt: current?.taskCreatedAt ?? current?.createdAt ?? sentAt,
        taskMessageIndex: _nextTaskMessageIndex(taskId),
      );
      state = state.copyWith(messages: [...state.messages, nextMessage]);
      setActiveAgentMessageId(nextId);
      activeGoalIteration = eventIteration ?? goalRoundIndex;
      openedGoalIterations.add(activeGoalIteration!);
      activeRoundStartedAt = startedAt;
    }

    _logChatLatency('sse_listen_started', taskId: taskId, startedAt: sentAt);
    SseLog.subscribed(
      SseChannel.task(taskId),
      '对话正文、思考过程与工具调用（task_id=$taskId）',
    );
    _startTerminalReconcileTimer(taskId);
    final sseCounts = <String, int>{};
    var dupDropped = 0;

    void logSseSummary(String reason) {
      final total = sseCounts.values.fold<int>(0, (sum, count) => sum + count);
      if (total == 0) return;
      SseLog.summary(
        SseChannel.task(taskId),
        '$reason，共收到$total条事件（去重丢弃$dupDropped条），'
        '监听时长${_elapsedMsSince(sseListenStartedAt)}毫秒，'
        '分类：${_sseCountsSummary(sseCounts)}',
      );
    }

    final rawEvents = _repo.watchTaskEvents(taskId).where((event) {
      if (!_detailsEnabled || liveVersion != _liveVersion) return false;
      if (event['type'] == 'connection_ready') {
        SseLog.step(
          SseChannel.task(taskId),
          '底层通道已就绪，耗时${_elapsedMsSince(sseListenStartedAt)}毫秒',
        );
        _updateMessage(activeAgentMessageId(), (m) {
          m.streamRecovery = StreamRecovery.following;
        });
        if (state.operationTitle == '正在恢复回复') {
          _updateOperation(
            'opencode',
            stepState: ChatOperationStepState.done,
            detail: '实时通道已连接',
          );
        }
        return false; // Transport status is neither a task event nor history.
      }
      final eventKey = _taskEventKey(event);
      if (!handledEventKeys.add(eventKey)) {
        dupDropped += 1;
        return false;
      }
      _persistTaskEvent(taskId, eventKey, event);
      return true;
    });
    _eventSub =
        batchTaskDeltas(
          rawEvents,
          onFlushReady: (flush) => _flushEventDeltas = flush,
        ).listen(
          (event) {
            if (!_detailsEnabled || liveVersion != _liveVersion) return;
            final type = event['type'] as String? ?? 'unknown';
            sseCounts[type] = (sseCounts[type] ?? 0) + 1;
            if (firstSseEvent) {
              firstSseEvent = false;
              _updateMessage(activeAgentMessageId(), (m) {
                if (m.streamRecovery == StreamRecovery.syncing ||
                    m.streamRecovery == StreamRecovery.reconnecting) {
                  m.streamRecovery = StreamRecovery.following;
                }
              });
              _logChatLatency(
                'first_sse_event',
                taskId: taskId,
                startedAt: sentAt,
                data: {
                  'listen_wait_ms': _elapsedMsSince(sseListenStartedAt),
                  'event_type': type,
                  'event_sent_at': event['sent_at'],
                },
              );
              _logChatDiag(
                'sse_first_event',
                taskId: taskId,
                data: {
                  'listen_wait_ms': _elapsedMsSince(sseListenStartedAt),
                  'type': type,
                },
              );
            }

            switch (type) {
              case 'started':
                final sessionId = event['session_id'] as String?;
                if (sessionId != null && sessionId.isNotEmpty) {
                  state = state.copyWith(
                    currentSessionId: sessionId,
                    taskActive: true,
                  );
                  _completeSessionReady(sessionId);
                  unawaited(_startQueueSync(sessionId));
                }
                _logChatLatency(
                  'task_started_event',
                  taskId: taskId,
                  startedAt: sentAt,
                  data: {
                    'task_session_id': sessionId,
                    'event_sent_at': event['sent_at'],
                  },
                );
                break;

              case 'progress':
                final deliveryPhase = finalDeliveryPhaseFromProgressEvent(
                  event,
                );
                if (deliveryPhase != FinalDeliveryPhase.idle &&
                    !reachedTerminal) {
                  _updateMessage(activeAgentMessageId(), (m) {
                    if (_isClosedMessage(m)) return;
                    m.finalDeliveryPhase = deliveryPhase;
                    m.statusHint = '';
                    _markStreaming(m);
                  });
                }
                final usage = ContextUsageInfo.fromProgressEvent(event);
                if (usage != null) {
                  state = state.copyWith(
                    contextUsage: _mergeContextUsage(state.contextUsage, usage),
                  );
                  _updateMessage(activeAgentMessageId(), (m) {
                    _applyReplyStatistics(
                      m,
                      const ReplyStatistics().recordUsage(
                        Map<String, dynamic>.from(event['metadata'] as Map),
                      ),
                    );
                  });
                }
                break;

              case 'subagent_started':
                if (reachedTerminal) break;
                final metadata = {
                  if (event['metadata'] is Map)
                    ...Map<String, dynamic>.from(event['metadata'] as Map),
                  'phase': 'running',
                };
                final nodeId = metadata['node_id']?.toString().trim() ?? '';
                if (nodeId.isEmpty ||
                    const {
                      '<nil>',
                      'nil',
                      'null',
                    }.contains(nodeId.toLowerCase())) {
                  break;
                }
                final tool = subagentToolCallFromMetadata(
                  taskId: taskId,
                  metadata: metadata,
                );
                _updateMessage(activeAgentMessageId(), (message) {
                  _upsertSubagentTool(message, tool);
                  _markStreaming(message);
                });
                break;

              case 'subagent_result':
                final metadata = {
                  if (event['metadata'] is Map)
                    ...Map<String, dynamic>.from(event['metadata'] as Map),
                  'phase': 'result',
                };
                final nodeId = metadata['node_id']?.toString().trim() ?? '';
                if (nodeId.isEmpty ||
                    const {
                      '<nil>',
                      'nil',
                      'null',
                    }.contains(nodeId.toLowerCase())) {
                  break;
                }
                final error =
                    event['error']?.toString() ??
                    metadata['error']?.toString() ??
                    '';
                final rawStatus = metadata['status']?.toString().trim();
                final status = rawStatus?.isNotEmpty == true
                    ? rawStatus!
                    : (error.trim().isNotEmpty ? 'failed' : 'completed');
                final tool = subagentToolCallFromMetadata(
                  taskId: taskId,
                  metadata: metadata,
                  status: status,
                  output:
                      event['content']?.toString() ??
                      metadata['output']?.toString() ??
                      '',
                  error: error,
                );
                final messages = List<ChatMessage>.from(state.messages);
                var updated = false;
                for (var i = messages.length - 1; i >= 0; i--) {
                  final message = messages[i];
                  if (message.taskId != taskId ||
                      !message.toolCalls.any(isSubagentToolCall)) {
                    continue;
                  }
                  final mutable = message.copyForUpdate();
                  _upsertSubagentTool(mutable, tool);
                  messages[i] = mutable;
                  updated = true;
                  break;
                }
                if (!updated) {
                  final activeIndex = messages.indexWhere(
                    (message) => message.id == activeAgentMessageId(),
                  );
                  if (activeIndex >= 0) {
                    final mutable = messages[activeIndex].copyForUpdate();
                    _upsertSubagentTool(mutable, tool);
                    messages[activeIndex] = mutable;
                    updated = true;
                  }
                }
                if (updated) {
                  state = state.copyWith(messages: messages);
                }
                break;

              case 'input_applied':
                if (reachedTerminal) break;
                final content = event['content'] as String? ?? '';
                final metadata = event['metadata'] is Map
                    ? Map<String, dynamic>.from(event['metadata'] as Map)
                    : const <String, dynamic>{};
                final queueItemId =
                    metadata['queue_item_id']?.toString() ??
                    'input_${event['sent_at'] ?? DateTime.now().microsecondsSinceEpoch}';
                final injectionVersion =
                    (metadata['injection_version'] as num?)?.toInt() ?? 0;
                final contextSource =
                    metadata['source']?.toString() ?? 'user_queue';
                final contextType =
                    metadata['context_type']?.toString() ?? 'queueInsert';
                final isBackgroundJob =
                    contextType == 'backgroundJob' ||
                    contextSource == 'background_job';
                final isSubagent =
                    contextType == 'subagentResult' ||
                    contextSource == 'subagent';
                final inputMessageId = isBackgroundJob
                    ? 'background_job_$queueItemId'
                    : isSubagent
                    ? ''
                    : 'user_insert_$queueItemId';
                final nextAgentMessageId = _inputRoundAgentMessageId(
                  taskId,
                  queueItemId,
                  injectionVersion,
                );
                final messages = List<ChatMessage>.from(state.messages);
                if (isSubagent) {
                  final resultMetadata = {...metadata, 'phase': 'result'};
                  final nodeId = metadata['node_id']?.toString().trim() ?? '';
                  var subagentUpdated = false;
                  if (nodeId.isNotEmpty) {
                    final tool = subagentToolCallFromMetadata(
                      taskId: taskId,
                      metadata: resultMetadata,
                      status: metadata['status']?.toString() ?? 'completed',
                    );
                    for (var i = messages.length - 1; i >= 0; i--) {
                      final message = messages[i];
                      if (message.taskId != taskId ||
                          !message.toolCalls.any(isSubagentToolCall)) {
                        continue;
                      }
                      final mutable = message.copyForUpdate();
                      _upsertSubagentTool(mutable, tool);
                      messages[i] = mutable;
                      subagentUpdated = true;
                      break;
                    }
                  }
                  if (!subagentUpdated) {
                    final activeIndex = messages.indexWhere(
                      (message) => message.id == activeAgentMessageId(),
                    );
                    if (activeIndex >= 0) {
                      final mutable = messages[activeIndex].copyForUpdate();
                      _upsertSubagentTool(
                        mutable,
                        subagentToolCallFromMetadata(
                          taskId: taskId,
                          metadata: resultMetadata,
                          status: metadata['status']?.toString() ?? 'completed',
                        ),
                      );
                      messages[activeIndex] = mutable;
                    }
                  }
                }
                // A subagent result injected into a still-running parent round
                // must not close that round: doing so finalizes sibling
                // subagents that are still running and drops the live stream
                // hint, which reads as a finished turn. Only the relay's wake
                // signal actually opens a new round.
                final startsNewRound =
                    !isSubagent ||
                    (metadata['wake_reason']?.toString().trim().isNotEmpty ??
                        false);
                if (startsNewRound) {
                  final currentIndex = messages.indexWhere(
                    (message) => message.id == activeAgentMessageId(),
                  );
                  if (currentIndex >= 0) {
                    final current = messages[currentIndex];
                    final isEmptyPlaceholder =
                        current.content.isEmpty &&
                        current.thinkingContent.isEmpty &&
                        current.toolCalls.isEmpty &&
                        current.goalProgress.isEmpty &&
                        current.blocks.isEmpty;
                    if (isEmptyPlaceholder) {
                      messages.removeAt(currentIndex);
                    }
                  }
                  for (var i = 0; i < messages.length; i++) {
                    final message = messages[i];
                    if (message.role != MessageRole.agent) continue;
                    if (message.id == nextAgentMessageId) continue;
                    if (message.taskId != null && message.taskId != taskId) {
                      continue;
                    }
                    final mutable = message.copyForUpdate();
                    _finalizeRunningTools(mutable);
                    // A live round boundary is not a history recovery failure.
                    mutable.historyDetails = HistoryDetailsState.none;
                    mutable.streamRecovery = StreamRecovery.none;
                    if (mutable.state == MessageState.streaming) {
                      mutable.statusHint = '';
                      mutable.imageGeneration = null;
                      mutable.finalDeliveryPhase = FinalDeliveryPhase.idle;
                      mutable.state = MessageState.done;
                    }
                    messages[i] = mutable;
                  }
                  DateTime? taskCreatedAt;
                  for (final message in messages) {
                    if (message.taskId == taskId &&
                        message.taskCreatedAt != null) {
                      taskCreatedAt = message.taskCreatedAt;
                      break;
                    }
                  }
                  taskCreatedAt ??= sentAt;
                  final taskMessageIndex = _nextTaskMessageIndex(taskId);
                  if (!isSubagent &&
                      !messages.any(
                        (message) => message.id == inputMessageId,
                      )) {
                    messages.add(
                      ChatMessage(
                        id: inputMessageId,
                        role: MessageRole.user,
                        state: MessageState.done,
                        content: content,
                        createdAt: eventTime(event),
                        taskId: taskId,
                        taskCreatedAt: taskCreatedAt,
                        taskMessageIndex: taskMessageIndex,
                        isInsertedContext: true,
                        insertedContextSource: contextSource,
                        insertedContextType: contextType,
                        insertedContextMetadata: metadata,
                      ),
                    );
                  }
                  if (!messages.any(
                    (message) => message.id == nextAgentMessageId,
                  )) {
                    messages.add(
                      ChatMessage(
                        id: nextAgentMessageId,
                        role: MessageRole.agent,
                        state: MessageState.streaming,
                        content: '',
                        createdAt: eventTime(event),
                        taskId: taskId,
                        taskCreatedAt: taskCreatedAt,
                        taskMessageIndex:
                            taskMessageIndex + (isSubagent ? 0 : 1),
                      ),
                    );
                  }
                  setActiveAgentMessageId(nextAgentMessageId);
                  activeRoundStartedAt = eventTime(event);
                }
                state = startsNewRound
                    ? state.copyWith(
                        messages: messages,
                        sending: true,
                        taskActive: true,
                      )
                    : state.copyWith(messages: messages);
                break;

              case 'delta':
                if (reachedTerminal) break;
                final live = _messageById(activeAgentMessageId());
                if (live == null || _isClosedMessage(live)) break;
                final content = event['content'] as String? ?? '';
                final field = event['field'] as String? ?? 'text';
                _updateMessage(activeAgentMessageId(), (m) {
                  if (field == 'text' &&
                      content.isNotEmpty &&
                      event['_reconstructed_history'] != true) {
                    _applyReplyStatistics(
                      m,
                      const ReplyStatistics()
                          .recordUsage(
                            Map<String, dynamic>.from(
                              (event['_first_delta_metadata'] ??
                                      event['metadata'] ??
                                      const {})
                                  as Map,
                            ),
                          )
                          .recordFirstText(
                            DateTime.tryParse(
                              (event['_first_delta_sent_at'] ??
                                          event['sent_at'])
                                      ?.toString() ??
                                  '',
                            ),
                          ),
                    );
                  }
                  if (!firstReasoningDeltaLogged &&
                      field == 'reasoning' &&
                      content.isNotEmpty) {
                    firstReasoningDeltaLogged = true;
                    _logChatLatency(
                      'first_reasoning_delta',
                      taskId: taskId,
                      startedAt: sentAt,
                      data: {
                        'round_elapsed_ms': _elapsedMsSince(
                          activeRoundStartedAt,
                        ),
                        'content_length': content.length,
                        'event_sent_at': event['sent_at'],
                      },
                    );
                  }
                  if (!firstTextDeltaLogged &&
                      field == 'text' &&
                      content.isNotEmpty) {
                    firstTextDeltaLogged = true;
                    _logChatLatency(
                      'first_text_delta',
                      taskId: taskId,
                      startedAt: sentAt,
                      data: {
                        'round_elapsed_ms': _elapsedMsSince(
                          activeRoundStartedAt,
                        ),
                        'content_length': content.length,
                        'event_sent_at': event['sent_at'],
                      },
                    );
                  }
                  if (field == 'reasoning') {
                    m.thinkingContent += content;
                  } else {
                    m.content += content;
                    _appendTextBlock(m, content);
                    m.statusHint = '';
                  }
                  _markStreaming(m);
                });
                break;

              case 'retrying':
                if (reachedTerminal) break;
                _updateMessage(activeAgentMessageId(), (m) {
                  if (_isClosedMessage(m)) return;
                  m.statusHint = _formatRetryHint(event);
                  _markStreaming(m);
                });
                state = state.copyWith(sending: true, taskActive: true);
                break;

              case 'compaction_started':
                if (reachedTerminal) break;
                _updateMessage(activeAgentMessageId(), (m) {
                  if (_isClosedMessage(m)) return;
                  m.statusHint = compactionStatusHint;
                  _markStreaming(m);
                });
                state = state.copyWith(sending: true, taskActive: true);
                break;

              case 'compaction_completed':
                if (reachedTerminal) break;
                _updateMessage(activeAgentMessageId(), (m) {
                  if (_isClosedMessage(m)) return;
                  if (m.statusHint == compactionStatusHint) {
                    m.statusHint = '';
                  }
                  _markStreaming(m);
                });
                state = state.copyWith(sending: true, taskActive: true);
                break;

              case 'tool_updated':
                final rawTool = event['tool'] as Map<String, dynamic>?;
                if (rawTool == null) break;
                final tool = normalizeSubagentToolCall(
                  ToolCallInfo.fromJson(rawTool),
                );
                if (tool.isPlanOnly) break;
                final ownerId = _agentMessageIdForTool(taskId, tool);
                final targetId = ownerId ?? activeAgentMessageId();
                _updateMessage(targetId, (m) {
                  _upsertToolCall(m, tool);
                  _upsertToolBlock(m, tool);
                  if (!reachedTerminal && !_isClosedMessage(m)) {
                    _markStreaming(m);
                  }
                });
                break;

              case 'goal_created':
              case 'goal_continued':
              case 'goal_checkpoint':
              case 'goal_paused':
              case 'goal_completed':
              case 'goal_failed':
                final goalEventType = type;
                final goalEntry = GoalProgressEntry.fromEvent(event);
                if (goalEventType == 'goal_created' ||
                    goalEventType == 'goal_continued') {
                  startGoalRound(event);
                }
                _updateMessage(activeAgentMessageId(), (m) {
                  m.goalProgress = [...m.goalProgress, goalEntry];
                  m.statusHint = _formatGoalHint(goalEventType, event);
                  if (goalEventType == 'goal_completed') {
                    m.statusHint = goalEntry.hasDetail ? goalEntry.detail : '';
                  }
                  if (goalEventType == 'goal_paused' ||
                      goalEventType == 'goal_failed') {
                    m.finalDeliveryPhase = FinalDeliveryPhase.idle;
                  }
                  if (goalEventType == 'goal_failed') {
                    m.state = MessageState.failed;
                  } else {
                    _markStreaming(m);
                  }
                });
                if (goalEventType == 'goal_paused' ||
                    goalEventType == 'goal_failed') {
                  state = state.copyWith(sending: false);
                }
                break;

              case 'cancelling':
                final reason = formatChatTaskError(
                  event['error'] as String? ??
                      event['content'] as String? ??
                      '任务超时，正在终止执行',
                );
                if (_cancelRequestedTaskIds.contains(taskId) ||
                    state.cancelRequested) {
                  _updateOperation(
                    'server',
                    stepState: ChatOperationStepState.done,
                    detail: '已转发到 Agent',
                  );
                  _updateOperation(
                    'opencode',
                    stepState: ChatOperationStepState.active,
                    detail: 'Agent 已接收停止指令 · 停止中',
                  );
                  _updateOperation(
                    'task',
                    stepState: ChatOperationStepState.active,
                    detail: '正在停止',
                  );
                  _updateMessage(activeAgentMessageId(), (m) {
                    if (!_isClosedMessage(m)) {
                      m.statusHint = 'Agent 已接收停止指令 · 正在停止';
                    }
                  });
                  state = state.copyWith(
                    sending: false,
                    taskActive: true,
                    cancelRequested: true,
                  );
                  break;
                }
                _updateMessage(activeAgentMessageId(), (m) {
                  m.statusHint = '';
                  if (m.content.isEmpty) {
                    m.content = reason;
                  }
                  m.imageGeneration = null;
                  m.finalDeliveryPhase = FinalDeliveryPhase.idle;
                  m.state = MessageState.failed;
                });
                state = state.copyWith(
                  sending: false,
                  taskActive: true,
                  clearApproval: true,
                  cancelRequested: false,
                );
                break;

              case 'waiting_approval':
                if (reachedTerminal) break;
                final approval = ApprovalInfo.fromJson(event);
                _updateMessage(activeAgentMessageId(), (m) {
                  if (_isClosedMessage(m)) return;
                  m.statusHint = '';
                  m.finalDeliveryPhase = FinalDeliveryPhase.idle;
                  _markStreaming(m);
                });
                state = state.copyWith(
                  pendingApproval: approval,
                  pendingApprovalTaskId: taskId,
                  sending: false,
                  taskActive: true,
                );
                break;

              case 'question_asked':
                if (reachedTerminal) break;
                final question = QuestionRequestInfo.fromJson(
                  (event['question'] as Map<String, dynamic>?) ?? event,
                );
                _updateMessage(activeAgentMessageId(), (m) {
                  if (_isClosedMessage(m)) return;
                  m.statusHint = '';
                  m.finalDeliveryPhase = FinalDeliveryPhase.idle;
                  _markStreaming(m);
                });
                state = state.copyWith(
                  pendingQuestion: question,
                  pendingQuestionTaskId: taskId,
                  sending: false,
                  taskActive: true,
                );
                break;

              case 'question_replied':
              case 'question_rejected':
                if (reachedTerminal) break;
                _updateMessage(activeAgentMessageId(), (m) {
                  if (_isClosedMessage(m)) return;
                  m.statusHint = '';
                  m.finalDeliveryPhase = FinalDeliveryPhase.idle;
                  _markStreaming(m);
                });
                state = state.copyWith(
                  clearQuestion: true,
                  sending: true,
                  taskActive: true,
                );
                break;

              case 'plan_updated':
                final rawPlan = event['plan'] as Map<String, dynamic>?;
                if (rawPlan == null) break;
                final plan = PlanInfo.fromJson(rawPlan);
                _updateMessage(activeAgentMessageId(), (m) {
                  m.plan = plan;
                  m.hasPlanBinding = true;
                  if (!plan.isEmpty) {
                    final capturedAt =
                        DateTime.tryParse(event['sent_at'] as String? ?? '') ??
                        DateTime.now();
                    _applyPlanHistorySnapshots(m, [
                      PlanHistorySnapshot(capturedAt: capturedAt, plan: plan),
                    ]);
                  }
                });
                break;

              case 'completed':
                reachedTerminal = true;
                _logChatLatency(
                  'task_completed_event',
                  taskId: taskId,
                  startedAt: sentAt,
                  data: {
                    'has_first_text_delta': firstTextDeltaLogged,
                    'event_sent_at': event['sent_at'],
                  },
                );
                final completedContent = event['content'] as String? ?? '';
                final completedMetadata = event['metadata'] is Map
                    ? Map<String, dynamic>.from(event['metadata'] as Map)
                    : const <String, dynamic>{};
                final roundContent =
                    completedMetadata['round_result'] as String? ?? '';
                var visibleCompletedContent = roundContent.isNotEmpty
                    ? roundContent
                    : completedContent;
                if (roundContent.isEmpty) {
                  final preceding = state.messages
                      .where(
                        (m) =>
                            m.role == MessageRole.agent && m.taskId == taskId,
                      )
                      .takeWhile((m) => m.id != activeAgentMessageId())
                      .map((m) => m.content)
                      .join();
                  if (preceding.isNotEmpty &&
                      visibleCompletedContent.startsWith(preceding)) {
                    visibleCompletedContent = visibleCompletedContent.substring(
                      preceding.length,
                    );
                  }
                }
                final artifacts = _parseArtifacts(event['artifacts']);
                final files = _parseFiles(event['files']);
                final usage = event['usage'] is Map
                    ? Map<String, dynamic>.from(event['usage'] as Map)
                    : completedMetadata['usage'] is Map
                    ? Map<String, dynamic>.from(
                        completedMetadata['usage'] as Map,
                      )
                    : null;
                if (usage != null) {
                  final completedUsage = ContextUsageInfo.fromUsageMetadata(
                    usage,
                    updatedAt: DateTime.tryParse(
                      event['sent_at']?.toString() ?? '',
                    ),
                  );
                  if (completedUsage != null) {
                    state = state.copyWith(
                      contextUsage: _mergeContextUsage(
                        state.contextUsage,
                        completedUsage,
                      ),
                    );
                  }
                }
                if (artifacts.isEmpty && files.isNotEmpty) {
                  unawaited(
                    AppLogService.log(
                      'chat_task_completed_inline_files',
                      data: {
                        'task_id': taskId,
                        'file_count': files.length,
                        'artifact_count': artifacts.length,
                      },
                    ),
                  );
                }
                if (artifacts.isNotEmpty && files.isNotEmpty) {
                  unawaited(
                    ArtifactCacheService.warmUpImagesFromCompletedPayload(
                      taskId: taskId,
                      artifacts: artifacts,
                      files: files,
                    ),
                  );
                }
                _updateMessage(activeAgentMessageId(), (m) {
                  m.statusHint = '';
                  if (visibleCompletedContent.isNotEmpty) {
                    if (roundContent.isNotEmpty) {
                      _setRecoveredBody(m, visibleCompletedContent);
                    } else {
                      _mergeRecoveredRoundBody(
                        m,
                        visibleCompletedContent,
                        streaming: false,
                      );
                    }
                  }
                  if (m.content.isEmpty) {
                    final goalFallback = _goalCompletionContent(m);
                    if (goalFallback.isNotEmpty) {
                      m.content = goalFallback;
                      if (!_blocksHaveVisibleText(m.blocks)) {
                        _appendTextBlock(m, goalFallback);
                      }
                    }
                  }
                  if (artifacts.isNotEmpty) {
                    m.artifacts = artifacts;
                    m.files = const [];
                  }
                  if (artifacts.isEmpty && files.isNotEmpty) {
                    m.files = files;
                  }
                  m.imageGeneration = null;
                  m.finalDeliveryPhase = FinalDeliveryPhase.idle;
                  _finalizeRunningTools(m);
                  m.state = MessageState.done;
                  if (usage != null) {
                    _applyReplyStatistics(
                      m,
                      const ReplyStatistics().recordUsage(usage),
                    );
                  }
                });
                _closeStreamingAgentMessagesForTask(taskId);
                if (visibleCompletedContent.isEmpty) {
                  _fetchTaskResult(taskId, activeAgentMessageId());
                }
                if (_currentTaskId == taskId) {
                  state = state.copyWith(
                    sending: false,
                    taskActive: false,
                    clearApproval: true,
                    clearQuestion: true,
                    cancelRequested: false,
                  );
                  _currentTaskId = null;
                }
                break;

              case 'failed':
                reachedTerminal = true;
                _logChatLatency(
                  'task_failed_event',
                  taskId: taskId,
                  startedAt: sentAt,
                  data: {
                    'has_first_text_delta': firstTextDeltaLogged,
                    'event_sent_at': event['sent_at'],
                    'error': event['error'],
                  },
                );
                final error = formatChatTaskError(
                  event['error'] as String? ?? '任务执行失败',
                );
                final errorDetail = _eventErrorDetail(event);
                // 用户主动停止时服务端也可能上报 failed（请求被中止）。这种情况
                // 必须保留已经产生的正文 / 思考 / 工具，只把停止作为中性提示，
                // 否则气泡会被失败文案整段替换。
                final stopped =
                    _cancelRequestedTaskIds.contains(taskId) ||
                    state.cancelRequested;
                _updateMessage(activeAgentMessageId(), (m) {
                  m.statusHint = '';
                  m.imageGeneration = null;
                  m.finalDeliveryPhase = FinalDeliveryPhase.idle;
                  if (stopped) {
                    m.state = MessageState.done;
                    m.statusHint = '任务已停止';
                    m.errorDetail = errorDetail;
                    _finalizeRunningTools(m);
                    return;
                  }
                  // 历史恢复 / 任务结果得到的正文只写在 content（blocks 为空），
                  // 直接覆盖会丢掉整段正文，先把它固化成块再追加失败文案。
                  if (m.content.trim().isNotEmpty &&
                      !_blocksHaveVisibleText(m.blocks)) {
                    _appendTextBlock(m, m.content);
                  }
                  m.state = MessageState.failed;
                  m.content = error;
                  m.errorDetail = errorDetail;
                  if (!_blocksHaveVisibleText(m.blocks)) {
                    _appendTextBlock(m, error);
                  }
                  _finalizeRunningTools(m);
                });
                _closeStreamingAgentMessagesForTask(taskId);
                if (_currentTaskId == taskId) {
                  state = state.copyWith(
                    sending: false,
                    taskActive: false,
                    clearApproval: true,
                    clearQuestion: true,
                    cancelRequested: false,
                  );
                  _currentTaskId = null;
                }
                break;

              case 'cancelled':
                reachedTerminal = true;
                if (_cancelRequestedTaskIds.contains(taskId) ||
                    state.cancelRequested) {
                  _updateOperation(
                    'opencode',
                    stepState: ChatOperationStepState.done,
                    detail: 'Agent 已停止执行',
                  );
                  _updateOperation(
                    'task',
                    stepState: ChatOperationStepState.done,
                    detail: '任务已停止',
                    title: '任务已停止',
                    autoClear: true,
                  );
                }
                _updateMessage(activeAgentMessageId(), (m) {
                  m.statusHint = state.cancelRequested ? '任务已停止' : '';
                  m.imageGeneration = null;
                  m.finalDeliveryPhase = FinalDeliveryPhase.idle;
                  if (m.content.isEmpty) {
                    m.content = event['content'] as String? ?? '已停止';
                  }
                  _finalizeRunningTools(m);
                  m.state = MessageState.done;
                });
                _closeStreamingAgentMessagesForTask(taskId);
                if (_currentTaskId == taskId) {
                  state = state.copyWith(
                    sending: false,
                    taskActive: false,
                    clearApproval: true,
                    clearQuestion: true,
                    cancelRequested: false,
                  );
                }
                _cancelRequestedTaskIds.remove(taskId);
                if (_currentTaskId == taskId) _currentTaskId = null;
                break;
            }
          },
          onError: (e) {
            if (!_detailsEnabled || liveVersion != _liveVersion) return;
            logSseSummary('中断');
            _logChatLatency(
              'sse_error',
              taskId: taskId,
              startedAt: sentAt,
              data: {'error': e.toString()},
            );
            unawaited(
              _recoverTaskAfterStreamBreak(
                taskId,
                activeAgentMessageId(),
                activeRoundStartedAt,
                fastActiveRecovery: fastActiveRecovery,
              ),
            );
          },
          onDone: () {
            if (!_detailsEnabled || liveVersion != _liveVersion) return;
            logSseSummary('结束');
            _logChatLatency(
              'sse_done',
              taskId: taskId,
              startedAt: sentAt,
              data: {'has_first_text_delta': firstTextDeltaLogged},
            );
            unawaited(
              _recoverTaskAfterStreamBreak(
                taskId,
                activeAgentMessageId(),
                activeRoundStartedAt,
                fastActiveRecovery: fastActiveRecovery,
              ),
            );
          },
        );
  }

  bool _isTaskActive(TaskStatus status) {
    return status == TaskStatus.dispatched ||
        status == TaskStatus.started ||
        status == TaskStatus.running ||
        status == TaskStatus.waitingApproval;
  }

  bool _isCurrentSessionLoad(String sessionId, int loadVersion) {
    return !_disposed &&
        state.currentSessionId == sessionId &&
        _sessionLoadVersion == loadVersion;
  }

  List<PlanHistorySnapshot> _mergePlanHistorySnapshots(
    List<PlanHistorySnapshot> current,
    List<PlanHistorySnapshot> incoming,
  ) {
    return mergePlanHistorySnapshotsByBatch(current, incoming);
  }

  bool _samePlanHistorySnapshots(
    List<PlanHistorySnapshot> left,
    List<PlanHistorySnapshot> right,
  ) {
    if (identical(left, right)) return true;
    if (left.length != right.length) return false;
    for (var i = 0; i < left.length; i++) {
      if (left[i].batchKey != right[i].batchKey ||
          left[i].dedupeKey != right[i].dedupeKey) {
        return false;
      }
    }
    return true;
  }

  bool _sameGoalProgress(
    List<GoalProgressEntry> left,
    List<GoalProgressEntry> right,
  ) {
    if (identical(left, right)) return true;
    if (left.length != right.length) return false;
    for (var i = 0; i < left.length; i++) {
      if (left[i].type != right[i].type ||
          left[i].summary != right[i].summary ||
          left[i].iteration != right[i].iteration ||
          left[i].max != right[i].max) {
        return false;
      }
    }
    return true;
  }

  bool _sameToolCalls(List<ToolCallInfo> left, List<ToolCallInfo> right) {
    if (identical(left, right)) return true;
    if (left.length != right.length) return false;
    for (var i = 0; i < left.length; i++) {
      if (left[i].stableKey != right[i].stableKey ||
          left[i].status != right[i].status ||
          left[i].output != right[i].output ||
          left[i].error != right[i].error) {
        return false;
      }
    }
    return true;
  }

  bool _sameBlocks(List<ChatMessageBlock> left, List<ChatMessageBlock> right) {
    if (identical(left, right)) return true;
    if (left.length != right.length) return false;
    for (var i = 0; i < left.length; i++) {
      final a = left[i];
      final b = right[i];
      if (a.type != b.type || a.id != b.id || a.text != b.text) return false;
      final at = a.tool;
      final bt = b.tool;
      if (at == null || bt == null) {
        if (at != bt) return false;
        continue;
      }
      if (at.stableKey != bt.stableKey ||
          at.status != bt.status ||
          at.output != bt.output ||
          at.error != bt.error) {
        return false;
      }
    }
    return true;
  }

  Future<void> _reconcileTerminalStreamingMessages({
    required String sessionId,
    required int loadVersion,
  }) async {
    final taskIds = state.messages
        .where(
          (message) =>
              message.role == MessageRole.agent &&
              message.state == MessageState.streaming &&
              message.historyDetails != HistoryDetailsState.deferred &&
              message.taskId?.trim().isNotEmpty == true,
        )
        .map((message) => message.taskId!.trim())
        .toSet()
        .toList(growable: false);
    if (taskIds.isEmpty) return;

    final tasks = await Future.wait(
      taskIds.map((taskId) async {
        try {
          return await _repo.getTask(taskId);
        } catch (_) {
          return null;
        }
      }),
    );
    for (var i = 0; i < tasks.length; i++) {
      if (!_isCurrentSessionLoad(sessionId, loadVersion)) return;
      final task = tasks[i];
      if (task == null ||
          _isTaskActive(task.status) ||
          (task.status != TaskStatus.completed &&
              task.status != TaskStatus.failed &&
              task.status != TaskStatus.cancelling &&
              task.status != TaskStatus.cancelled)) {
        continue;
      }

      final taskId = taskIds[i];
      if (task.status == TaskStatus.completed) {
        final snapshot = await _readTaskEventSnapshot(taskId);
        if (!_isCurrentSessionLoad(sessionId, loadVersion)) return;
        final firstAgent = state.messages
            .where(
              (message) =>
                  message.role == MessageRole.agent && message.taskId == taskId,
            )
            .firstOrNull;
        if (firstAgent != null) {
          var completedMessageId = firstAgent.id;
          if (snapshot.appliedInputs.isNotEmpty) {
            completedMessageId = _restoreTaskRoundMessages(
              taskId,
              firstAgent.id,
              snapshot,
            );
          }
          _updateMessage(
            completedMessageId,
            (message) => _applyCompletedTaskSnapshot(message, task, snapshot),
          );
          _closeStreamingAgentMessagesForTask(taskId);
        }
      } else {
        final failed =
            task.status == TaskStatus.failed ||
            task.status == TaskStatus.cancelling;
        final cancelled = task.status == TaskStatus.cancelled;
        final snapshot = await _readTaskEventSnapshot(taskId);
        final messageIds = state.messages
            .where(
              (message) =>
                  message.role == MessageRole.agent &&
                  message.taskId == taskId &&
                  message.state == MessageState.streaming,
            )
            .map((message) => message.id)
            .toList(growable: false);
        for (final messageId in messageIds) {
          _updateMessage(messageId, (message) {
            _applyHistorySnapshotToMessage(
              message,
              snapshot,
              finalizeTools: true,
            );
            message.statusHint = '';
            message.imageGeneration = null;
            message.finalDeliveryPhase = FinalDeliveryPhase.idle;
            message.state = failed ? MessageState.failed : MessageState.done;
            if (message.content.isEmpty) {
              message.content = failed
                  ? formatChatTaskError(
                      task.error?.isNotEmpty == true ? task.error! : '任务执行失败',
                    )
                  : (cancelled ? '已停止' : '任务已完成');
              if (message.blocks.isEmpty) {
                _appendTextBlock(message, message.content);
              }
            }
            if (snapshot.errorDetail.isNotEmpty) {
              message.errorDetail = snapshot.errorDetail;
            }
          });
        }
      }

      if (_currentTaskId == taskId) {
        _cancelEventSub();
        state = state.copyWith(
          sending: false,
          taskActive: false,
          clearApproval: true,
          clearQuestion: true,
          cancelRequested: false,
        );
      }
    }
  }

  Future<TaskEventSnapshot> _readTaskEventSnapshot(String taskId) async {
    TaskEventSnapshot server;
    try {
      server = await _repo.getTaskEventSnapshot(taskId);
    } catch (_) {
      // The task status is authoritative. An unavailable event archive must
      // not keep a terminal task rendered as an active stream.
      server = const TaskEventSnapshot(complete: false);
    }
    final local = await _localSnapshotForTask(taskId);
    return _mergeSnapshotsPreferringInsertionOrder(
      local,
      server,
      taskId: taskId,
    );
  }

  Future<TaskEventSnapshot> _localSnapshotForTask(String taskId) async {
    final identity = _localIdentity;
    if (identity.isEmpty) {
      _logChatDiag(
        'history_local_skip',
        taskId: taskId,
        data: {'reason': 'empty_identity'},
      );
      return const TaskEventSnapshot();
    }
    try {
      await _localStore.init();
      final events = await _localStore.readEvents(
        identity: identity,
        taskId: taskId,
      );
      if (events.isEmpty) {
        _logChatDiag('history_local_empty', taskId: taskId);
        return const TaskEventSnapshot();
      }
      final snapshot = _repo.snapshotFromTaskEvents(taskId, events);
      return snapshot;
    } catch (error) {
      _logChatDiag(
        'history_local_failed',
        taskId: taskId,
        data: {'error': error.toString()},
      );
      return const TaskEventSnapshot();
    }
  }

  TaskEventSnapshot _mergeSnapshotsPreferringInsertionOrder(
    TaskEventSnapshot local,
    TaskEventSnapshot server, {
    String? taskId,
  }) {
    final localMixed = _blocksHaveMixedInsertion(local.blocks);
    final serverMixed = _blocksHaveMixedInsertion(server.blocks);
    final localTools = _toolsFromBlocks(local.blocks).length;
    final serverTools = _toolsFromBlocks(server.blocks).length;
    final localTexts = _visibleTextBlockCount(local.blocks);
    final serverTexts = _visibleTextBlockCount(server.blocks);
    final useServerOrder =
        server.blocks.isNotEmpty &&
        (!localMixed ||
            (serverMixed &&
                (serverTools > localTools ||
                    (serverTexts >= localTexts && serverTools >= localTools))));
    final ordered = useServerOrder
        ? server.blocks
        : (local.blocks.isNotEmpty ? local.blocks : server.blocks);
    final used = useServerOrder || local.blocks.isEmpty ? 'server' : 'local';
    final mergedTools = _mergeToolLists(
      _toolsFromBlocks(local.blocks),
      server.toolCalls.isNotEmpty
          ? server.toolCalls
          : _toolsFromBlocks(server.blocks),
    );
    final preferServerText = used == 'server' && server.text.isNotEmpty;
    return TaskEventSnapshot(
      complete: server.complete,
      preservesToolPositions: used == 'server'
          ? server.preservesToolPositions
          : local.preservesToolPositions,
      text: preferServerText
          ? server.text
          : (local.text.isNotEmpty ? local.text : server.text),
      reasoning: server.reasoning.isNotEmpty
          ? server.reasoning
          : local.reasoning,
      statusHint: server.statusHint.isNotEmpty
          ? server.statusHint
          : local.statusHint,
      goalProgress: server.goalProgress.isNotEmpty
          ? server.goalProgress
          : local.goalProgress,
      planHistory: server.planHistory.isNotEmpty
          ? server.planHistory
          : local.planHistory,
      toolCalls: mergedTools,
      blocks: _blocksWithMergedTools(ordered, mergedTools),
      errorDetail: server.errorDetail.isNotEmpty
          ? server.errorDetail
          : local.errorDetail,
      rounds: _mergeRoundStatistics(local.rounds, server.rounds),
      contextUsage: server.contextUsage ?? local.contextUsage,
      statistics: local.statistics.merge(server.statistics),
      appliedInputs: server.appliedInputs.isNotEmpty
          ? server.appliedInputs
          : local.appliedInputs,
      finalDeliveryPhase: server.finalDeliveryPhase,
    );
  }

  List<TaskEventRoundSnapshot> _mergeRoundStatistics(
    List<TaskEventRoundSnapshot> local,
    List<TaskEventRoundSnapshot> server,
  ) {
    if (server.isEmpty) return local;
    // Equal boundaries establish an index mapping. With a partial local cache,
    // only merge a round when its start time or request identity matches.
    return [
      for (var i = 0; i < server.length; i++)
        (() {
          final round = server[i];
          TaskEventRoundSnapshot? cached;
          if (local.length == server.length) {
            cached = local[i];
          } else {
            for (final candidate in local) {
              if ((round.startedAt != null &&
                      candidate.startedAt == round.startedAt) ||
                  round.statistics.requests.keys.any(
                    candidate.statistics.requests.containsKey,
                  )) {
                cached = candidate;
                break;
              }
            }
          }
          final stats = (cached?.statistics ?? const ReplyStatistics()).merge(
            round.statistics,
          );
          return TaskEventRoundSnapshot(
            text: round.text,
            reasoning: round.reasoning,
            statusHint: round.statusHint,
            goalProgress: round.goalProgress,
            planHistory: round.planHistory,
            toolCalls: round.toolCalls,
            blocks: round.blocks,
            errorDetail: round.errorDetail,
            startedAt: round.startedAt,
            statistics: stats,
            tokenCount: stats.totalTokens,
          );
        })(),
    ];
  }

  ReplyStatistics _statisticsForMessage(
    ChatMessage message,
    TaskEventSnapshot snapshot,
  ) {
    if (snapshot.rounds.length <= 1) return snapshot.statistics;
    final candidates = snapshot.rounds
        .where((round) => round.hasOutput)
        .toList();
    final messages = state.messages
        .where((m) => m.role == MessageRole.agent && m.taskId == message.taskId)
        .toList();
    final index = messages.indexWhere((m) => m.id == message.id);
    if (index >= 0 && messages.length == candidates.length) {
      return candidates[index].statistics;
    }
    // During initial hydration only the task summary bubble may exist. Wait
    // for round expansion instead of assigning whole-task totals to it.
    return const ReplyStatistics();
  }

  String _goalCompletionContent(ChatMessage message) {
    for (final entry in message.goalProgress.reversed) {
      if (entry.type == 'goal_completed' && entry.hasDetail) {
        return entry.detail.trim();
      }
    }
    for (final entry in message.goalProgress.reversed) {
      if (entry.type == 'goal_checkpoint' && entry.hasDetail) {
        return entry.detail.trim();
      }
    }
    return '';
  }

  bool _applyPlanHistorySnapshots(
    ChatMessage message,
    List<PlanHistorySnapshot> incoming,
  ) {
    if (incoming.isEmpty) return false;
    final merged = _mergePlanHistorySnapshots(message.planHistory, incoming);
    var changed = false;
    if (!_samePlanHistorySnapshots(message.planHistory, merged)) {
      message.planHistory = merged;
      changed = true;
    }
    if (merged.isNotEmpty) {
      final latestPlan = merged.last.plan;
      final currentSignature = message.plan == null
          ? null
          : buildPlanHistorySignature(message.plan!);
      final latestSignature = buildPlanHistorySignature(latestPlan);
      if (currentSignature != latestSignature) {
        message.plan = latestPlan;
        changed = true;
      }
      if (!message.hasPlanBinding) {
        message.hasPlanBinding = true;
        changed = true;
      }
    }
    return changed;
  }

  ChatMessage? _lastTaskReply(String taskId) {
    for (final message in state.messages.reversed) {
      if (message.role == MessageRole.agent && message.taskId == taskId) {
        return message;
      }
    }
    return null;
  }

  /// 会话重建时保留活动任务的直播间内容：重建只依据任务索引的 `result`，
  /// 对仍在执行/正在终止的任务直接替换会把已产出的正文、思考、工具一次清空。
  List<ChatMessage> _mergeLiveTaskContent(
    List<ChatMessage> rebuilt,
    List<ChatMessage> previous,
    Set<String> activeTaskIds,
  ) {
    if (previous.isEmpty || activeTaskIds.isEmpty) return rebuilt;
    final live = <String, ChatMessage>{};
    for (final message in previous) {
      if (message.role != MessageRole.agent) continue;
      final taskId = message.taskId?.trim() ?? '';
      if (taskId.isEmpty || !activeTaskIds.contains(taskId)) continue;
      live[taskId] = message;
    }
    if (live.isEmpty) return rebuilt;
    return [
      for (final message in rebuilt)
        _mergeLiveMessage(message, live[message.taskId?.trim()]),
    ];
  }

  ChatMessage _mergeLiveMessage(ChatMessage rebuilt, ChatMessage? live) {
    if (live == null || rebuilt.role != MessageRole.agent) return rebuilt;
    final merged = rebuilt.copyForUpdate();
    if ((live.state == MessageState.streaming && live.content.isNotEmpty) ||
        live.content.length > merged.content.length) {
      merged.content = live.content;
    }
    if (live.thinkingContent.length > merged.thinkingContent.length) {
      merged.thinkingContent = live.thinkingContent;
    }
    if (merged.toolCalls.isEmpty && live.toolCalls.isNotEmpty) {
      merged.toolCalls = List<ToolCallInfo>.from(live.toolCalls);
    }
    if (merged.blocks.isEmpty && live.blocks.isNotEmpty) {
      merged.blocks = List<ChatMessageBlock>.from(live.blocks);
    }
    if (merged.state != MessageState.streaming &&
        live.state == MessageState.streaming) {
      merged.state = MessageState.streaming;
    }
    return merged;
  }

  final Map<String, Future<void>> _historyHydrations = {};

  /// 快照恢复一次都没成功时的自动重试标记（每个会话加载只重试一次）。
  final Set<String> _snapshotRetryKeys = {};

  Future<void> _hydrateSessionSnapshots({
    required String sessionId,
    required int loadVersion,
    required List<String> taskIds,
  }) async {
    await Future.wait(
      taskIds.map((taskId) async {
        final key = '$loadVersion:$sessionId:$taskId';
        final work = _historyHydrations.putIfAbsent(key, () async {
          try {
            await _applySessionSnapshot(
              sessionId: sessionId,
              loadVersion: loadVersion,
              taskIds: [taskId],
            );
          } finally {
            _historyHydrations.remove(key);
          }
        });
        if (_lastTaskReply(taskId)?.state == MessageState.streaming) {
          // Bound the caller's wait, not the lifetime of the hydration. The
          // original read must still apply its result after a slow first page.
          await work.timeout(activeHistoryHydrationTimeout, onTimeout: () {});
        } else {
          await work;
        }
      }),
    );
  }

  Future<void> _applySessionSnapshot({
    required String sessionId,
    required int loadVersion,
    required List<String> taskIds,
  }) async {
    final owners = {for (final id in taskIds) id: _lastTaskReply(id)};
    final startedAt = DateTime.now();
    final snapshots = await Future.wait(
      taskIds.map((taskId) async {
        try {
          final snapshot = await _readTaskEventSnapshot(
            taskId,
          ).timeout(terminalHistoryTimeout);
          return MapEntry(taskId, snapshot);
        } catch (_) {
          // SSE only carries a recent window; it is not proof of complete
          // body recovery. Expose an explicit retryable historical gap.
          return MapEntry(taskId, const TaskEventSnapshot(complete: false));
        }
      }),
    );
    if (!_isCurrentSessionLoad(sessionId, loadVersion)) return;

    final snapshotMap = <String, TaskEventSnapshot>{};
    for (final entry in snapshots) {
      if (entry.key.isEmpty) continue;
      final owner = owners[entry.key];
      final current = _lastTaskReply(entry.key);
      // A snapshot requested before a new live round must not replace it.
      if (owner == null ||
          current?.id != owner.id ||
          current?.state != owner.state) {
        continue;
      }
      if (owner.state == MessageState.streaming &&
          (current!.content != owner.content ||
              current.blocks.length != owner.blocks.length ||
              current.state != owner.state)) {
        // 重连补发先到，不代表历史没用：思考与工具记录照常合并进来，只保留
        // 直播间的正文，避免整份快照被丢弃导致思考/工具记录全部消失。
        final fallback = entry.value;
        _updateMessage(owner.id, (m) {
          _applyHistorySnapshotToMessage(
            m,
            fallback,
            finalizeTools: false,
            keepLongerThinking: true,
          );
          m.historyDetails = HistoryDetailsState.partial;
          if (m.streamRecovery == StreamRecovery.syncing) {
            m.streamRecovery = StreamRecovery.following;
          }
        });
        continue;
      }
      snapshotMap[entry.key] = entry.value;
    }
    if (snapshotMap.isEmpty) {
      // 活动任务一份快照都没应用上（读不完整或被实时变更挡掉）时自动重试一次，
      // 不再要求用户手动点「重试」才能看到思考与工具记录。
      final needsRetry = taskIds.any((taskId) {
        final message = _lastTaskReply(taskId);
        if (message == null) return false;
        if (message.state == MessageState.streaming) return true;
        if (message.historyDetails == HistoryDetailsState.partial) return true;
        return _messageNeedsCatchUp(message);
      });
      final retryKey = '$loadVersion:$sessionId:${taskIds.join(",")}';
      if (needsRetry &&
          _isCurrentSessionLoad(sessionId, loadVersion) &&
          _snapshotRetryKeys.add(retryKey)) {
        unawaited(
          Future<void>.delayed(const Duration(milliseconds: 1200), () async {
            if (_disposed || !_isCurrentSessionLoad(sessionId, loadVersion)) {
              return;
            }
            await _applySessionSnapshot(
              sessionId: sessionId,
              loadVersion: loadVersion,
              taskIds: taskIds,
            );
          }),
        );
      }
      if (taskIds.contains(_currentTaskId) &&
          state.operationTitle == '正在恢复回复') {
        replySyncProgress?.finish('实时回复已更新，历史详情可重试');
        _updateOperation(
          'task',
          stepState: ChatOperationStepState.failed,
          detail: '已保留实时回复，历史详情可重试',
        );
      }
      return;
    }
    _snapshotRetryKeys.clear();

    final msgs = List<ChatMessage>.from(state.messages);
    var changed = false;
    var nextContextUsage = state.contextUsage;
    final replacedRoundTaskIds = <String>{};
    for (var i = 0; i < msgs.length; i++) {
      final original = msgs[i];
      if (original.role != MessageRole.agent || original.taskId == null) {
        continue;
      }
      if (replacedRoundTaskIds.contains(original.taskId)) continue;
      final snapshot = snapshotMap[original.taskId!];
      if (snapshot == null) continue;
      if (!snapshot.complete) {
        final partial = original.copyForUpdate()
          ..historyDetails = original.id == owners[original.taskId]?.id
              ? HistoryDetailsState.partial
              : HistoryDetailsState.none;
        // A bounded read can still contain useful body text. Show a verified
        // single-round prefix only in an empty bubble; never replace a fuller
        // live reply or guess a boundary across inserted/goal rounds.
        if (partial.content.isEmpty &&
            snapshot.text.isNotEmpty &&
            snapshot.appliedInputs.isEmpty &&
            snapshot.rounds.length <= 1) {
          _applyHistorySnapshotToMessage(
            partial,
            snapshot,
            finalizeTools: partial.state != MessageState.streaming,
          );
          _setRecoveredBody(partial, snapshot.text);
        }
        msgs[i] = partial;
        changed = true;
        continue;
      }
      final mutableMessage = original.copyForUpdate();
      msgs[i] = mutableMessage;
      final msg = mutableMessage;
      msg.historyDetails = HistoryDetailsState.none;
      msg.toolOrderKnown =
          snapshot.preservesToolPositions ||
          (msg.toolOrderKnown && _blocksAreInterleaved(msg.blocks));
      changed = true;
      final previousTokens = msg.tokenCount;
      final previousLatency = msg.firstTokenTime;
      _applyReplyStatistics(msg, _statisticsForMessage(msg, snapshot));
      if (previousTokens != msg.tokenCount ||
          previousLatency != msg.firstTokenTime) {
        changed = true;
      }
      if (msg.streamRecovery == StreamRecovery.syncing) {
        msg.streamRecovery = StreamRecovery.following;
        changed = true;
      }
      if (snapshot.contextUsage != null) {
        nextContextUsage = _mergeContextUsage(
          nextContextUsage,
          snapshot.contextUsage!,
        );
      }
      final hydratedDeliveryPhase = msg.state == MessageState.streaming
          ? snapshot.finalDeliveryPhase
          : FinalDeliveryPhase.idle;
      if (msg.finalDeliveryPhase != hydratedDeliveryPhase) {
        msg.finalDeliveryPhase = hydratedDeliveryPhase;
        changed = true;
      }
      if ((_shouldRenderSnapshotAsGoalRounds(snapshot) ||
              snapshot.appliedInputs.isNotEmpty) &&
          replacedRoundTaskIds.add(msg.taskId!)) {
        final firstIndex = msgs.indexWhere(
          (m) => m.role == MessageRole.agent && m.taskId == msg.taskId,
        );
        if (firstIndex >= 0) {
          var startIndex = firstIndex;
          while (startIndex > 0 &&
              msgs[startIndex - 1].taskId == msg.taskId &&
              msgs[startIndex - 1].isInsertedContext) {
            startIndex--;
          }
          var endIndex = firstIndex + 1;
          while (endIndex < msgs.length &&
              msgs[endIndex].taskId == msg.taskId) {
            endIndex += 1;
          }
          final roundMessages = _messagesFromTaskRoundSnapshot(
            msgs[firstIndex].copyForUpdate()
              ..state = owners[msg.taskId]!.state
              ..streamRecovery = msg.streamRecovery
              ..historyDetails = HistoryDetailsState.none,
            snapshot,
            currentReply: owners[msg.taskId],
            contentSpansRounds: owners[msg.taskId]?.id == msgs[firstIndex].id,
          );
          if (roundMessages.isNotEmpty) {
            msgs.replaceRange(startIndex, endIndex, roundMessages);
            if (_currentTaskId == msg.taskId) {
              for (final roundMessage in roundMessages.reversed) {
                if (roundMessage.role == MessageRole.agent) {
                  _activeTaskAgentMessageIds[msg.taskId!] = roundMessage.id;
                  break;
                }
              }
            }
            changed = true;
            i = startIndex + roundMessages.length - 1;
            continue;
          }
        }
      }
      if (snapshot.text.isNotEmpty &&
          (msg.content.isEmpty ||
              (msg.state == MessageState.streaming &&
                  snapshot.appliedInputs.isEmpty &&
                  snapshot.rounds.length <= 1))) {
        // The unchanged-owner guard above establishes the live boundary. A
        // complete single-round snapshot supplies the whole body, not merely
        // the recent SSE suffix; keep content and rendered blocks in sync.
        _setRecoveredBody(msg, snapshot.text);
        changed = true;
      }
      final previousBlocks = msg.blocks;
      final previousTools = msg.toolCalls;
      _applyHistorySnapshotToMessage(
        msg,
        snapshot,
        finalizeTools: msg.state != MessageState.streaming,
      );
      if (!_sameBlocks(previousBlocks, msg.blocks) ||
          !_sameToolCalls(previousTools, msg.toolCalls)) {
        changed = true;
      } else if (msg.blocks.isEmpty && msg.content.isNotEmpty) {
        _appendTextBlock(msg, msg.content);
        changed = true;
      }
      if (snapshot.reasoning.isNotEmpty &&
          msg.thinkingContent != snapshot.reasoning) {
        msg.thinkingContent = snapshot.reasoning;
        changed = true;
      }
      if (msg.statusHint != snapshot.statusHint) {
        msg.statusHint = snapshot.statusHint;
        changed = true;
      }
      if (snapshot.goalProgress.isNotEmpty &&
          !_sameGoalProgress(msg.goalProgress, snapshot.goalProgress)) {
        msg.goalProgress = snapshot.goalProgress;
        changed = true;
      }
      if (snapshot.errorDetail.isNotEmpty &&
          msg.errorDetail != snapshot.errorDetail) {
        msg.errorDetail = snapshot.errorDetail;
        changed = true;
      }
    }
    final usageChanged = nextContextUsage != state.contextUsage;
    if ((changed || usageChanged) &&
        _isCurrentSessionLoad(sessionId, loadVersion)) {
      state = state.copyWith(
        messages: changed ? msgs : null,
        contextUsage: nextContextUsage,
      );
    }
    if (_isCurrentSessionLoad(sessionId, loadVersion) &&
        taskIds.contains(_currentTaskId) &&
        state.operationTitle == '正在恢复回复') {
      final reply = _lastTaskReply(_currentTaskId!);
      final complete = reply?.historyDetails == HistoryDetailsState.none;
      replySyncProgress?.finish(complete ? '已有回复已恢复' : '历史回复尚未补齐');
      _updateOperation(
        'task',
        stepState: complete
            ? ChatOperationStepState.done
            : ChatOperationStepState.failed,
        detail: complete ? '已有回复已恢复' : '历史回复尚未补齐，可重试',
        autoClear: complete,
      );
    }
    _logChatDiag(
      'history_hydrate_end',
      data: {
        'tasks': snapshotMap.length,
        'changed': changed,
        'elapsed_ms': _elapsedMsSince(startedAt),
      },
    );
  }

  bool _shouldRenderSnapshotAsGoalRounds(TaskEventSnapshot snapshot) {
    return snapshot.rounds.length > 1 &&
        snapshot.rounds.any((round) => round.hasGoalProgress);
  }

  List<ChatMessage> _messagesFromTaskRoundSnapshot(
    ChatMessage base,
    TaskEventSnapshot snapshot, {
    ChatMessage? currentReply,
    bool contentSpansRounds = true,
  }) {
    base = base.copyForUpdate()
      ..toolOrderKnown =
          snapshot.preservesToolPositions ||
          (base.toolOrderKnown && _blocksAreInterleaved(base.blocks));
    final taskId = base.taskId;
    if (taskId == null) return const [];
    final bodySource = currentReply ?? base;
    base.artifacts = bodySource.artifacts;
    base.files = bodySource.files;
    base.errorDetail = bodySource.errorDetail;
    base.plan = bodySource.plan ?? base.plan;
    final rounds = snapshot.rounds;
    final visibleRoundIndexes = <int>[
      for (var index = 0; index < rounds.length; index++)
        if (rounds[index].hasOutput) index,
    ];
    if (visibleRoundIndexes.length <= 1 && snapshot.appliedInputs.isEmpty) {
      return const [];
    }
    final lastVisibleRound = visibleRoundIndexes.isEmpty
        ? -1
        : visibleRoundIndexes.last;
    int? activePlaceholderRound;
    if (rounds.isNotEmpty) {
      final candidate = rounds.length - 1;
      if (!rounds[candidate].hasOutput &&
          snapshot.appliedInputs.any(
            (input) => input.afterRoundIndex == candidate - 1,
          ) &&
          (base.state == MessageState.streaming ||
              _bodyAfterSnapshotRounds(
                bodySource.content,
                contentSpansRounds ? rounds.take(candidate) : const [],
              ).trim().isNotEmpty)) {
        activePlaceholderRound = candidate;
      }
    }
    final result = <ChatMessage>[];
    var visibleIndex = 0;

    void appendInputsAfter(int roundIndex) {
      for (final input in snapshot.appliedInputs.where(
        (input) => input.afterRoundIndex == roundIndex,
      )) {
        final isBackgroundJob =
            input.contextType == 'backgroundJob' ||
            input.contextSource == 'background_job';
        final isSubagent =
            input.contextType == 'subagentResult' ||
            input.contextSource == 'subagent';
        // The delegated result is rendered as the completed inline call on
        // the preceding Agent bubble, never as a standalone context bubble.
        if (isSubagent) continue;
        final stableSuffix = input.queueItemId.isEmpty
            ? input.injectionVersion.toString()
            : input.queueItemId;
        final id = isBackgroundJob
            ? (input.queueItemId.isEmpty
                  ? 'background_job_${taskId}_${input.injectionVersion}'
                  : 'background_job_${input.queueItemId}')
            : isSubagent
            ? 'subagent_context_${taskId}_${stableSuffix}_${input.injectionVersion}'
            : (input.queueItemId.isEmpty
                  ? 'user_insert_${taskId}_${input.injectionVersion}'
                  : 'user_insert_${input.queueItemId}');
        result.add(
          ChatMessage(
            id: id,
            role: MessageRole.user,
            state: MessageState.done,
            content: input.content,
            createdAt:
                input.appliedAt ??
                base.createdAt.add(Duration(milliseconds: result.length + 1)),
            taskId: taskId,
            taskCreatedAt: base.taskCreatedAt ?? base.createdAt,
            // Index zero belongs to the task's original user message.
            taskMessageIndex: result.length + 1,
            isInsertedContext: true,
            insertedContextSource: input.contextSource,
            insertedContextType: input.contextType,
            insertedContextMetadata: input.contextMetadata,
          ),
        );
      }
    }

    appendInputsAfter(-1);
    for (var roundIndex = 0; roundIndex < rounds.length; roundIndex++) {
      final round = rounds[roundIndex];
      TaskInputAppliedInfo? precedingInput;
      if (roundIndex > 0) {
        for (final input in snapshot.appliedInputs) {
          if (input.afterRoundIndex == roundIndex - 1) {
            precedingInput = input;
          }
        }
      }
      final isActiveInputPlaceholder =
          precedingInput != null && roundIndex == activePlaceholderRound;
      if (round.hasOutput || isActiveInputPlaceholder) {
        final lastRenderedRound = activePlaceholderRound ?? lastVisibleRound;
        final isLastRenderedRound = roundIndex == lastRenderedRound;
        result.add(
          _messageFromTaskRoundSnapshot(
            base: base,
            taskId: taskId,
            round: round,
            visibleIndex: visibleIndex,
            timelineIndex: result.length + 1,
            isLast: isLastRenderedRound,
            messageId: precedingInput == null
                ? null
                : _inputRoundAgentMessageId(
                    taskId,
                    precedingInput.queueItemId,
                    precedingInput.injectionVersion,
                  ),
          ),
        );
        visibleIndex += 1;
      }
      appendInputsAfter(roundIndex);
    }
    if (visibleRoundIndexes.isEmpty && result.isEmpty) result.add(base);
    // The task index and live reply can be ahead of the detailed archive.
    // Rebuilding rounds restores boundaries/tools, not permission to discard
    // the final body. A task-level result may include the preceding rounds;
    // strip only their exact known prefix, never a guessed length/boundary.
    var lastAgentIndex = result.lastIndexWhere(
      (message) => message.role == MessageRole.agent,
    );
    if (lastAgentIndex < 0 && bodySource.content.trim().isNotEmpty) {
      result.add(
        _messageFromTaskRoundSnapshot(
          base: base,
          taskId: taskId,
          round: TaskEventRoundSnapshot(
            text: bodySource.content,
            reasoning: bodySource.thinkingContent,
            toolCalls: bodySource.toolCalls,
            blocks: bodySource.blocks,
            goalProgress: bodySource.goalProgress,
            planHistory: bodySource.planHistory,
            statistics: bodySource.statistics,
          ),
          visibleIndex: 0,
          timelineIndex: result.length + 1,
          isLast: true,
        ),
      );
      lastAgentIndex = result.length - 1;
    }
    if (lastAgentIndex >= 0) {
      final lastRoundIndex = activePlaceholderRound ?? lastVisibleRound;
      final body = _bodyAfterSnapshotRounds(
        bodySource.content,
        contentSpansRounds && lastRoundIndex > 0
            ? rounds.take(lastRoundIndex)
            : const [],
      );
      _mergeRecoveredRoundBody(
        result[lastAgentIndex],
        body,
        streaming: base.state == MessageState.streaming,
      );
    }
    return result;
  }

  String _bodyAfterSnapshotRounds(
    String content,
    Iterable<TaskEventRoundSnapshot> precedingRounds,
  ) {
    final prefix = precedingRounds.map((round) => round.text).join();
    return prefix.isNotEmpty && content.startsWith(prefix)
        ? content.substring(prefix.length)
        : content;
  }

  void _mergeRecoveredRoundBody(
    ChatMessage message,
    String body, {
    required bool streaming,
  }) {
    if (body.trim().isEmpty) return;
    final existing = message.content;
    if (existing.isEmpty || body.startsWith(existing)) {
      _setRecoveredBody(message, body);
    } else if (existing.contains(body)) {
      // The archive is already fuller, or this final result is rendered.
      return;
    } else if (body.contains(existing)) {
      _setRecoveredBody(message, body);
    } else if (!streaming) {
      // Some terminal indexes contain only the summary, not earlier
      // commentary. Keep both and place the missing summary after the tools.
      _appendTextBlock(message, '\n\n$body');
      message.content = '$existing\n\n$body';
    } else {
      // The unchanged-owner guard establishes this live round's boundary.
      // Length is not freshness: a newer round can be shorter than an older
      // snapshot's commentary, while exact contained prefixes/suffixes above
      // already retain a fuller archive body.
      _setRecoveredBody(message, body);
    }
  }

  String _restoreTaskRoundMessages(
    String taskId,
    String fallbackAgentMessageId,
    TaskEventSnapshot snapshot,
  ) {
    if ((snapshot.appliedInputs.isEmpty &&
            !_shouldRenderSnapshotAsGoalRounds(snapshot)) ||
        snapshot.rounds.isEmpty) {
      return fallbackAgentMessageId;
    }
    final messages = List<ChatMessage>.from(state.messages);
    final firstAgentIndex = messages.indexWhere(
      (message) =>
          message.taskId == taskId && message.role == MessageRole.agent,
    );
    if (firstAgentIndex < 0) return fallbackAgentMessageId;

    var startIndex = firstAgentIndex;
    while (startIndex > 0 &&
        messages[startIndex - 1].taskId == taskId &&
        messages[startIndex - 1].isInsertedContext) {
      startIndex--;
    }
    var endIndex = firstAgentIndex + 1;
    while (endIndex < messages.length && messages[endIndex].taskId == taskId) {
      endIndex += 1;
    }
    final restored = _messagesFromTaskRoundSnapshot(
      messages[firstAgentIndex].copyForUpdate()
        ..state = (_lastTaskReply(taskId) ?? messages[firstAgentIndex]).state
        ..historyDetails = (_lastTaskReply(taskId) ?? messages[firstAgentIndex])
            .historyDetails,
      snapshot,
      currentReply: _lastTaskReply(taskId),
      contentSpansRounds:
          _lastTaskReply(taskId)?.id == messages[firstAgentIndex].id,
    );
    if (restored.isEmpty) return fallbackAgentMessageId;
    messages.replaceRange(startIndex, endIndex, restored);
    state = state.copyWith(messages: messages);
    for (final message in restored.reversed) {
      if (message.role == MessageRole.agent) {
        _activeTaskAgentMessageIds[taskId] = message.id;
        return message.id;
      }
    }
    return fallbackAgentMessageId;
  }

  ChatMessage _messageFromTaskRoundSnapshot({
    required ChatMessage base,
    required String taskId,
    required TaskEventRoundSnapshot round,
    required int visibleIndex,
    required int timelineIndex,
    required bool isLast,
    String? messageId,
  }) {
    final roundContent = round.text.isNotEmpty
        ? round.text
        : _goalRoundCompletionContent(round);
    final visibleContent = roundContent;
    final plan = round.planHistory.isNotEmpty
        ? round.planHistory.last.plan
        : null;
    final message = ChatMessage(
      id:
          messageId ??
          (visibleIndex == 0
              ? base.id
              : 'agent_${taskId}_round_${visibleIndex + 1}'),
      role: MessageRole.agent,
      state: isLast ? base.state : MessageState.done,
      content: visibleContent,
      thinkingContent: round.reasoning,
      statusHint: isLast ? round.statusHint : '',
      streamRecovery: isLast ? base.streamRecovery : StreamRecovery.none,
      historyDetails: isLast ? base.historyDetails : HistoryDetailsState.none,
      toolOrderKnown: base.toolOrderKnown,
      errorDetail: isLast
          ? (round.errorDetail.isNotEmpty
                ? round.errorDetail
                : base.errorDetail)
          : round.errorDetail,
      goalProgress: round.goalProgress,
      createdAt:
          round.startedAt ??
          base.createdAt.add(Duration(milliseconds: visibleIndex + 1)),
      taskId: taskId,
      taskCreatedAt: base.taskCreatedAt ?? base.createdAt,
      taskMessageIndex: timelineIndex,
      toolCalls: round.toolCalls,
      blocks: round.blocks.isNotEmpty
          ? round.blocks
          : _blocksFromLegacyContentAndTools(visibleContent, round.toolCalls),
      artifacts: isLast ? base.artifacts : const [],
      files: isLast ? base.files : const [],
      plan: plan ?? (isLast ? base.plan : null),
      planHistory: round.planHistory,
      hasPlanBinding:
          round.planHistory.isNotEmpty ||
          (isLast && base.plan != null && !base.plan!.isEmpty),
      tokenCount: round.statistics.totalTokens ?? round.tokenCount,
      statistics: round.statistics,
      finalDeliveryPhase: isLast
          ? base.finalDeliveryPhase
          : FinalDeliveryPhase.idle,
      firstTokenTime:
          round.statistics.latencyFrom(
            visibleIndex == 0
                ? (base.taskCreatedAt ?? base.createdAt)
                : (round.startedAt ?? base.createdAt),
          ) ??
          (visibleIndex == 0 ? base.firstTokenTime : null),
    );
    _setRecoveredBody(message, visibleContent);
    if (base.state != MessageState.streaming) _finalizeRunningTools(message);
    return message;
  }

  String _goalRoundCompletionContent(TaskEventRoundSnapshot round) {
    for (final entry in round.goalProgress.reversed) {
      if (entry.type == 'goal_completed' && entry.hasDetail) {
        return entry.detail.trim();
      }
    }
    for (final entry in round.goalProgress.reversed) {
      if (entry.type == 'goal_checkpoint' && entry.hasDetail) {
        return entry.detail.trim();
      }
    }
    return '';
  }

  String _finalTaskContent(TaskModel task, TaskEventSnapshot snapshot) {
    final result = task.result ?? '';
    if (result.trim().isNotEmpty) {
      return _bodyAfterSnapshotRounds(
        result,
        snapshot.rounds.take(
          snapshot.rounds.isEmpty ? 0 : snapshot.rounds.length - 1,
        ),
      );
    }
    if (snapshot.appliedInputs.isNotEmpty ||
        _shouldRenderSnapshotAsGoalRounds(snapshot)) {
      for (final round in snapshot.rounds.reversed) {
        if (!round.hasOutput) continue;
        if (round.text.trim().isNotEmpty) return round.text;
        final goalContent = _goalRoundCompletionContent(round);
        if (goalContent.isNotEmpty) return goalContent;
      }
    }
    final text = snapshot.text;
    if (text.trim().isNotEmpty) return text;
    return '';
  }

  String _taskErrorDetail(TaskModel task, TaskEventSnapshot snapshot) {
    if (snapshot.errorDetail.trim().isNotEmpty) {
      return snapshot.errorDetail.trim();
    }
    final raw = task.error ?? '';
    final marker = raw.indexOf('\ndetail:');
    if (marker < 0) return '';
    return raw.substring(marker + '\ndetail:'.length).trim();
  }

  String _eventErrorDetail(Map<String, dynamic> event) {
    final metadata = event['metadata'] is Map<String, dynamic>
        ? event['metadata'] as Map<String, dynamic>
        : event['metadata'] is Map
        ? Map<String, dynamic>.from(event['metadata'] as Map)
        : const <String, dynamic>{};
    for (final value in [
      event['error_detail'],
      event['detail'],
      metadata['error_detail'],
      metadata['detail'],
    ]) {
      if (value is String && value.trim().isNotEmpty) return value.trim();
    }
    final raw = event['error'] as String? ?? '';
    final marker = raw.indexOf('\ndetail:');
    if (marker < 0) return '';
    return raw.substring(marker + '\ndetail:'.length).trim();
  }

  void _applyReplyStatistics(ChatMessage message, ReplyStatistics incoming) {
    message.statistics = message.statistics.merge(incoming);
    message.tokenCount = message.statistics.totalTokens;
    final start = (message.taskMessageIndex ?? 1) <= 1
        ? (message.taskCreatedAt ?? message.createdAt)
        : message.createdAt;
    message.firstTokenTime =
        message.statistics.latencyFrom(start) ?? message.firstTokenTime;
  }

  ContextUsageInfo _mergeContextUsage(
    ContextUsageInfo? current,
    ContextUsageInfo next,
  ) {
    final currentTime = current?.updatedAt;
    final nextTime = next.updatedAt;
    if (current != null &&
        currentTime != null &&
        nextTime != null &&
        nextTime.isBefore(currentTime)) {
      return current;
    }
    return current?.merge(next) ?? next;
  }

  List<ToolCallInfo> _finalizedToolCalls(List<ToolCallInfo> tools) {
    return [
      for (final tool in tools)
        if (tool.isRunning) tool.copyWith(status: 'completed') else tool,
    ];
  }

  ToolCallInfo _coalesceToolCall(ToolCallInfo current, ToolCallInfo incoming) {
    return current.coalescedWith(incoming);
  }

  int _indexOfTool(List<ToolCallInfo> tools, ToolCallInfo tool) {
    return tools.indexWhere((item) => item.sameIdentity(tool));
  }

  ToolCallInfo _coalesceAgainstBlocks(
    List<ChatMessageBlock> current,
    ToolCallInfo incoming,
  ) {
    for (final block in current) {
      final existing = block.tool;
      if (existing != null && existing.sameIdentity(incoming)) {
        return _coalesceToolCall(existing, incoming);
      }
    }
    return incoming;
  }

  String? _agentMessageIdForTool(String taskId, ToolCallInfo tool) {
    for (final message in state.messages.reversed) {
      if (message.role != MessageRole.agent) continue;
      if (message.taskId != null && message.taskId != taskId) continue;
      if (_indexOfTool(message.toolCalls, tool) >= 0) return message.id;
      if (message.blocks.any(
        (block) => block.tool != null && block.tool!.sameIdentity(tool),
      )) {
        return message.id;
      }
    }
    return null;
  }

  List<ToolCallInfo> _mergeToolLists(
    List<ToolCallInfo> current,
    List<ToolCallInfo> incoming,
  ) {
    final merged = List<ToolCallInfo>.from(current);
    for (final tool in incoming) {
      final idx = _indexOfTool(merged, tool);
      if (idx >= 0) {
        merged[idx] = _coalesceToolCall(merged[idx], tool);
      } else {
        merged.add(tool);
      }
    }
    return merged;
  }

  void _finalizeRunningTools(ChatMessage message) {
    if (!message.toolCalls.any((tool) => tool.isRunning) &&
        !message.blocks.any(
          (block) => block.tool != null && block.tool!.isRunning,
        )) {
      return;
    }
    message.toolCalls = _finalizedToolCalls(message.toolCalls);
    message.blocks = _finalizedBlocks(message.blocks);
  }

  bool _isClosedMessage(ChatMessage message) {
    return message.state == MessageState.done ||
        message.state == MessageState.failed;
  }

  void _markStreaming(ChatMessage message) {
    if (_isClosedMessage(message)) return;
    message.state = MessageState.streaming;
  }

  void _closeStreamingAgentMessagesForTask(
    String taskId, {
    MessageState endState = MessageState.done,
  }) {
    final msgs = List<ChatMessage>.from(state.messages);
    var changed = false;
    for (var i = 0; i < msgs.length; i++) {
      final message = msgs[i];
      if (message.role != MessageRole.agent) continue;
      if (message.taskId != null && message.taskId != taskId) continue;
      final needsClose = message.state == MessageState.streaming;
      final needsTools =
          message.toolCalls.any((tool) => tool.isRunning) ||
          message.blocks.any(
            (block) => block.tool != null && block.tool!.isRunning,
          );
      if (!needsClose && !needsTools) continue;
      final next = message.copyForUpdate();
      _finalizeRunningTools(next);
      if (needsClose) {
        next.statusHint = '';
        next.imageGeneration = null;
        next.finalDeliveryPhase = FinalDeliveryPhase.idle;
        next.state = endState;
      }
      msgs[i] = next;
      changed = true;
    }
    if (changed) {
      state = state.copyWith(messages: msgs);
    }
  }

  List<ChatMessageBlock> _finalizedBlocks(List<ChatMessageBlock> blocks) {
    return [
      for (final block in blocks)
        if (block.tool != null && block.tool!.isRunning)
          block.copyWith(tool: block.tool!.copyWith(status: 'completed'))
        else
          block,
    ];
  }

  bool _blocksHaveVisibleText(List<ChatMessageBlock> blocks) {
    return blocks.any(
      (block) =>
          block.type == ChatMessageBlockType.text &&
          block.text.trim().isNotEmpty,
    );
  }

  bool _blocksAreInterleaved(List<ChatMessageBlock> blocks) {
    return _blocksHaveVisibleText(blocks) &&
        blocks.any((block) => block.tool != null);
  }

  bool _blocksHaveMixedInsertion(List<ChatMessageBlock> blocks) {
    var sawText = false;
    var sawTool = false;
    var sawTextAfterTool = false;
    var sawToolAfterText = false;
    for (final block in blocks) {
      if (block.tool != null) {
        if (sawText) sawToolAfterText = true;
        sawTool = true;
        continue;
      }
      if (block.type == ChatMessageBlockType.text &&
          block.text.trim().isNotEmpty) {
        if (sawTool) sawTextAfterTool = true;
        sawText = true;
      }
    }
    return sawTextAfterTool && sawToolAfterText;
  }

  int _visibleTextBlockCount(List<ChatMessageBlock> blocks) {
    return blocks
        .where(
          (block) =>
              block.type == ChatMessageBlockType.text &&
              block.text.trim().isNotEmpty,
        )
        .length;
  }

  List<ToolCallInfo> _toolsFromBlocks(List<ChatMessageBlock> blocks) {
    return [
      for (final block in blocks)
        if (block.tool != null) block.tool!,
    ];
  }

  int _visibleTextLength(List<ChatMessageBlock> blocks, String content) {
    final fromBlocks = blocks
        .where((block) => block.type == ChatMessageBlockType.text)
        .map((block) => block.text)
        .join()
        .trim()
        .length;
    if (fromBlocks > 0) return fromBlocks;
    return content.trim().length;
  }

  List<ChatMessageBlock> _mergeHistoryBlocks({
    required List<ChatMessageBlock> current,
    required List<ChatMessageBlock> snapshot,
    required String currentContent,
    required List<ToolCallInfo> snapshotTools,
    required bool finalizeTools,
  }) {
    final tools = finalizeTools
        ? _finalizedToolCalls(snapshotTools)
        : snapshotTools;
    final incoming = finalizeTools ? _finalizedBlocks(snapshot) : snapshot;
    if (incoming.isEmpty) {
      if (current.isNotEmpty) return current;
      if (currentContent.trim().isEmpty && tools.isEmpty) return current;
      return _blocksFromLegacyContentAndTools(currentContent, tools);
    }
    final snapshotHasText = _blocksHaveVisibleText(incoming);
    final currentHasText =
        _blocksHaveVisibleText(current) || currentContent.trim().isNotEmpty;
    final keepCurrentText =
        currentHasText &&
        (!snapshotHasText ||
            _visibleTextLength(current, currentContent) >
                _visibleTextLength(incoming, ''));
    final currentTools = _toolsFromBlocks(current);
    final mergedTools = _mergeToolLists(currentTools, tools);
    final incomingInterleaved = _blocksAreInterleaved(incoming);
    final currentInterleaved = _blocksAreInterleaved(current);
    if (_blocksHaveMixedInsertion(incoming) &&
        _toolsFromBlocks(incoming).length >= _toolsFromBlocks(current).length) {
      return _blocksWithMergedTools(
        incoming,
        mergedTools,
        fallbackContent: keepCurrentText ? currentContent : '',
      );
    }
    if (_blocksHaveMixedInsertion(current)) {
      return _blocksWithMergedTools(
        current,
        mergedTools,
        fallbackContent: currentContent,
      );
    }
    if (_blocksHaveMixedInsertion(incoming)) {
      return _blocksWithMergedTools(
        incoming,
        mergedTools,
        fallbackContent: keepCurrentText ? currentContent : '',
      );
    }
    if (incomingInterleaved && !(keepCurrentText && currentInterleaved)) {
      return _blocksWithMergedTools(
        incoming,
        mergedTools,
        fallbackContent: keepCurrentText ? currentContent : '',
        preferFallbackText:
            keepCurrentText && _visibleTextBlockCount(incoming) <= 1,
      );
    }
    if (keepCurrentText) {
      if (current.isNotEmpty) {
        return _blocksWithMergedTools(
          current,
          mergedTools,
          fallbackContent: currentContent,
        );
      }
      if (incoming.isNotEmpty) {
        return _blocksWithMergedTools(
          incoming,
          mergedTools,
          fallbackContent: currentContent,
          preferFallbackText: true,
        );
      }
      return _blocksFromLegacyContentAndTools(currentContent, mergedTools);
    }
    if (current.isEmpty) return incoming;
    final mergedIncoming = [
      for (final block in incoming)
        if (block.tool != null)
          block.copyWith(tool: _coalesceAgainstBlocks(current, block.tool!))
        else
          block,
    ];
    final incomingTools = [
      for (final block in mergedIncoming)
        if (block.tool != null) block.tool!,
    ];
    final extraTools = [
      for (final block in current)
        if (block.tool != null && _indexOfTool(incomingTools, block.tool!) < 0)
          finalizeTools && block.tool!.isRunning
              ? block.tool!.copyWith(status: 'completed')
              : block.tool!,
    ];
    if (extraTools.isEmpty) return mergedIncoming;
    return [
      ...mergedIncoming,
      for (final tool in extraTools)
        ChatMessageBlock.tool(
          id: 'tool_${tool.stableKey.isEmpty ? tool.tool : tool.stableKey}',
          tool: tool,
        ),
    ];
  }

  void _applyHistorySnapshotToMessage(
    ChatMessage message,
    TaskEventSnapshot snapshot, {
    required bool finalizeTools,
    bool keepLongerThinking = false,
  }) {
    _applyReplyStatistics(message, _statisticsForMessage(message, snapshot));
    message.toolOrderKnown =
        snapshot.preservesToolPositions ||
        (message.toolOrderKnown && _blocksAreInterleaved(message.blocks));
    final tools = finalizeTools
        ? _finalizedToolCalls(snapshot.toolCalls)
        : snapshot.toolCalls;
    if (snapshot.reasoning.isNotEmpty &&
        (!keepLongerThinking ||
            snapshot.reasoning.length > message.thinkingContent.length) &&
        message.thinkingContent != snapshot.reasoning) {
      message.thinkingContent = snapshot.reasoning;
    }
    if (snapshot.blocks.isNotEmpty || snapshot.toolCalls.isNotEmpty) {
      message.blocks = _mergeHistoryBlocks(
        current: message.blocks,
        snapshot: snapshot.blocks,
        currentContent: message.content,
        snapshotTools: snapshot.toolCalls,
        finalizeTools: finalizeTools,
      );
    }
    if (tools.isNotEmpty) {
      message.toolCalls = _mergeToolLists(message.toolCalls, tools);
      if (message.blocks.isEmpty) {
        message.blocks = _blocksFromLegacyContentAndTools(
          message.content,
          message.toolCalls,
        );
      }
    }
    _applyPlanHistorySnapshots(message, snapshot.planHistory);
  }

  void _applyCompletedTaskSnapshot(
    ChatMessage message,
    TaskModel task,
    TaskEventSnapshot snapshot,
  ) {
    _applyReplyStatistics(message, _statisticsForMessage(message, snapshot));
    message.toolOrderKnown =
        snapshot.preservesToolPositions ||
        (message.toolOrderKnown && _blocksAreInterleaved(message.blocks));
    TaskEventRoundSnapshot? finalRound;
    if (snapshot.appliedInputs.isNotEmpty ||
        _shouldRenderSnapshotAsGoalRounds(snapshot)) {
      for (final round in snapshot.rounds.reversed) {
        if (round.hasOutput) {
          finalRound = round;
          break;
        }
      }
    }
    final finalContent = _finalTaskContent(task, snapshot);
    final reasoning = finalRound?.reasoning ?? snapshot.reasoning;
    final blocks = finalRound?.blocks ?? snapshot.blocks;
    final toolCalls = _finalizedToolCalls(
      finalRound?.toolCalls ?? snapshot.toolCalls,
    );
    final goalProgress = finalRound?.goalProgress ?? snapshot.goalProgress;
    final planHistory = finalRound?.planHistory ?? snapshot.planHistory;
    if (reasoning.isNotEmpty) {
      message.thinkingContent = reasoning;
    }
    if (snapshot.errorDetail.isNotEmpty) {
      message.errorDetail = snapshot.errorDetail;
    }
    message.blocks = _mergeHistoryBlocks(
      current: message.blocks,
      snapshot: blocks,
      currentContent: message.content,
      snapshotTools: toolCalls,
      finalizeTools: true,
    );
    _applyPlanHistorySnapshots(message, planHistory);
    if (toolCalls.isNotEmpty) {
      message.toolCalls = _mergeToolLists(message.toolCalls, toolCalls);
      if (message.blocks.isEmpty) {
        message.blocks = _blocksFromLegacyContentAndTools(
          message.content,
          message.toolCalls,
        );
      }
    }
    _finalizeRunningTools(message);
    if (goalProgress.isNotEmpty &&
        !_sameGoalProgress(message.goalProgress, goalProgress)) {
      message.goalProgress = goalProgress;
    }
    message.statusHint = '';
    if (finalContent.isNotEmpty) {
      _mergeRecoveredRoundBody(message, finalContent, streaming: false);
    } else if (message.content.isEmpty) {
      final goalFallback = _goalCompletionContent(message);
      if (goalFallback.isNotEmpty) {
        message.content = goalFallback;
        if (!_blocksHaveVisibleText(message.blocks)) {
          _appendTextBlock(message, goalFallback);
        }
      }
    }
    if (task.artifacts.isNotEmpty) {
      message.artifacts = task.artifacts;
      message.files = const [];
    }
    message.imageGeneration = null;
    message.finalDeliveryPhase = FinalDeliveryPhase.idle;
    message.state = MessageState.done;
  }

  void _setRecoveredBody(ChatMessage message, String content) {
    if (content.isEmpty) return;
    final rendered = message.blocks
        .where((b) => b.type == ChatMessageBlockType.text)
        .map((b) => b.text)
        .join();
    if (content.startsWith(rendered)) {
      _appendTextBlock(message, content.substring(rendered.length));
    } else {
      // Keep tool positions; replace old body fragments with the authoritative
      // result until the detailed interleaved snapshot arrives.
      final blocks = <ChatMessageBlock>[];
      var inserted = false;
      for (final block in message.blocks) {
        if (block.type == ChatMessageBlockType.text) {
          if (!inserted) {
            blocks.add(block.copyWith(text: content));
            inserted = true;
          }
        } else {
          blocks.add(block);
        }
      }
      if (!inserted) {
        blocks.add(
          ChatMessageBlock.text(id: '${message.id}_result', text: content),
        );
      }
      message.blocks = blocks;
    }
    message.content = content;
  }

  void _finishRecoveredTask(TaskModel task, String agentMsgId) {
    final taskId = task.taskId;
    final wasCancelRequested =
        state.cancelRequested || _cancelRequestedTaskIds.contains(taskId);
    final failed = task.status == TaskStatus.failed;
    final completed = task.status == TaskStatus.completed;
    _updateMessage(agentMsgId, (message) {
      var result = completed ? (task.result ?? '') : '';
      // A task result can span inserted-input rounds. Remove only an exact
      // known preceding prefix; detailed history will restore missing rounds.
      final previous = state.messages
          .where((m) => m.role == MessageRole.agent && m.taskId == taskId)
          .takeWhile((m) => m.id != agentMsgId)
          .map((m) => m.content)
          .join();
      if (previous.isNotEmpty && result.startsWith(previous)) {
        result = result.substring(previous.length);
      }
      if (result.isNotEmpty) _setRecoveredBody(message, result);
      if (message.content.isEmpty && !completed) {
        _setRecoveredBody(
          message,
          failed ? formatChatTaskError(task.error ?? '任务执行失败') : '已停止',
        );
      }
      if (task.artifacts.isNotEmpty) {
        message.artifacts = task.artifacts;
        message.files = const [];
      }
      message.errorDetail = _taskErrorDetail(task, const TaskEventSnapshot());
      message.statusHint = '';
      message.streamRecovery = StreamRecovery.none;
      message.historyDetails = HistoryDetailsState.loading;
      message.imageGeneration = null;
      message.finalDeliveryPhase = FinalDeliveryPhase.idle;
      message.state = failed ? MessageState.failed : MessageState.done;
      _finalizeRunningTools(message);
    });
    _closeStreamingAgentMessagesForTask(
      taskId,
      endState: failed ? MessageState.failed : MessageState.done,
    );
    _cancelEventSub();
    if (wasCancelRequested || task.status == TaskStatus.cancelled) {
      _updateOperation(
        'opencode',
        stepState: ChatOperationStepState.done,
        detail: 'Agent 已停止执行',
      );
      _updateOperation(
        'task',
        stepState: ChatOperationStepState.done,
        detail: '任务已停止',
        title: '任务已停止',
        autoClear: true,
      );
    }
    state = state.copyWith(
      sending: false,
      taskActive: false,
      cancelRequested: false,
      clearApproval: true,
      clearQuestion: true,
    );
    _logChatDiag(
      'resume_terminal_applied',
      taskId: taskId,
      data: {
        'status': task.status.name,
        'result_length': task.result?.length ?? 0,
      },
    );
    unawaited(_enrichRecoveredTask(task, agentMsgId, _sessionLoadVersion));
  }

  Future<void> retryHistoryDetails(String taskId) async {
    final messages = state.messages
        .where((m) => m.role == MessageRole.agent && m.taskId == taskId)
        .toList();
    if (messages.isEmpty ||
        messages.any((m) => m.historyDetails == HistoryDetailsState.loading)) {
      return;
    }
    final version = _sessionLoadVersion;
    final messageId = messages.last.id;
    bool ownsRound() =>
        !_disposed &&
        version == _sessionLoadVersion &&
        _lastTaskReply(taskId)?.id == messageId;
    _updateMessage(
      messageId,
      (m) => m.historyDetails = HistoryDetailsState.loading,
    );
    try {
      final task = await _repo.getTask(taskId).timeout(recoveryStatusTimeout);
      if (!ownsRound()) return;
      if (_isTaskActive(task.status)) {
        final sessionId = state.currentSessionId;
        if (sessionId != null) {
          await _hydrateSessionSnapshots(
            sessionId: sessionId,
            loadVersion: version,
            taskIds: [taskId],
          );
        }
      } else {
        await _enrichRecoveredTask(task, messageId, version);
      }
    } catch (_) {
      if (ownsRound()) {
        _updateMessage(
          messageId,
          (m) => m.historyDetails = HistoryDetailsState.partial,
        );
      }
    }
  }

  Future<void> _enrichRecoveredTask(
    TaskModel task,
    String agentMsgId,
    int loadVersion,
  ) async {
    var ownerId = _lastTaskReply(task.taskId)?.id ?? agentMsgId;
    bool ownsRound() =>
        !_disposed &&
        _sessionLoadVersion == loadVersion &&
        _lastTaskReply(task.taskId)?.id == ownerId;
    final progress = ReplySyncProgress('补齐历史详情');
    replySyncProgress = progress;
    void setDetails(HistoryDetailsState value) {
      if (!ownsRound()) return;
      state = state.copyWith(
        messages: [
          for (final message in state.messages)
            if (message.role == MessageRole.agent &&
                message.taskId == task.taskId)
              message.copyForUpdate()
                ..historyDetails = message.id == ownerId
                    ? value
                    : HistoryDetailsState.none
            else
              message,
        ],
      );
    }

    setDetails(HistoryDetailsState.loading);
    try {
      final snapshot = await _readTaskEventSnapshot(
        task.taskId,
      ).timeout(terminalHistoryTimeout);
      if (!ownsRound() || _messageById(agentMsgId) == null) {
        return;
      }
      if (!snapshot.complete) {
        progress.finish('正文已就绪，历史详情未全部载入');
        setDetails(HistoryDetailsState.partial);
        _logChatDiag('resume_details_partial', taskId: task.taskId);
        return;
      }
      var messageId = ownerId;
      if (snapshot.appliedInputs.isNotEmpty ||
          _shouldRenderSnapshotAsGoalRounds(snapshot)) {
        messageId = _restoreTaskRoundMessages(
          task.taskId,
          agentMsgId,
          snapshot,
        );
      }
      ownerId = messageId;
      _updateMessage(messageId, (message) {
        if (task.status == TaskStatus.completed &&
            (snapshot.text.isNotEmpty ||
                snapshot.rounds.any((round) => round.hasOutput))) {
          _applyCompletedTaskSnapshot(message, task, snapshot);
        } else {
          _applyHistorySnapshotToMessage(
            message,
            snapshot,
            finalizeTools: true,
          );
          if (snapshot.text.isNotEmpty) {
            _setRecoveredBody(message, snapshot.text);
          }
          message.errorDetail = _taskErrorDetail(task, snapshot);
        }
        message.streamRecovery = StreamRecovery.none;
      });
      if (snapshot.contextUsage != null) {
        state = state.copyWith(
          contextUsage: _mergeContextUsage(
            state.contextUsage,
            snapshot.contextUsage!,
          ),
        );
      }
      setDetails(HistoryDetailsState.none);
      progress.finish('历史详情已补齐');
      _logChatDiag('resume_details_applied', taskId: task.taskId);
    } catch (error) {
      progress.finish('正文已就绪，详情稍后同步');
      setDetails(HistoryDetailsState.partial);
      // Body and terminal state are already visible; detail retrieval never
      // keeps the composer or the reply in a pending state.
      if (!_disposed) {
        _logChatDiag(
          'resume_details_deferred',
          taskId: task.taskId,
          data: {'error': error.toString()},
        );
      }
    }
  }

  Future<void> _recoverTaskAfterStreamBreak(
    String taskId,
    String agentMsgId,
    DateTime sentAt, {
    bool fastActiveRecovery = false,
  }) async {
    final loadVersion = _sessionLoadVersion;
    final liveVersion = _liveVersion;
    bool isCurrent() =>
        _detailsEnabled &&
        _liveVersion == liveVersion &&
        _currentTaskId == taskId &&
        _sessionLoadVersion == loadVersion;
    if (!isCurrent()) return;
    final recoveryStartedAt = DateTime.now();
    final progress = ReplySyncProgress('确认任务状态');
    replySyncProgress = progress;
    _beginOperation('正在恢复回复', const [
      ChatOperationStep(
        key: 'phone',
        label: '手机',
        detail: '请求任务状态',
        state: ChatOperationStepState.active,
      ),
      ChatOperationStep(key: 'server', label: '服务端', detail: '等待响应'),
      ChatOperationStep(
        key: 'opencode',
        label: 'OpenCode',
        detail: '等待 Agent 状态',
      ),
      ChatOperationStep(key: 'task', label: '回复', detail: '等待恢复'),
    ]);
    try {
      _logChatDiag('resume_status_start', taskId: taskId);
      final task = await _repo.getTask(taskId).timeout(recoveryStatusTimeout);
      _logChatDiag(
        'resume_status_received',
        taskId: taskId,
        data: {
          'status': task.status.name,
          'elapsed_ms': DateTime.now()
              .difference(recoveryStartedAt)
              .inMilliseconds,
        },
      );
      if (!isCurrent()) return;
      _updateOperation(
        'phone',
        stepState: ChatOperationStepState.done,
        detail: '状态请求完成',
      );
      _updateOperation(
        'server',
        stepState: ChatOperationStepState.done,
        detail: '已返回任务状态',
      );
      _updateOperation(
        'opencode',
        stepState: ChatOperationStepState.active,
        detail: 'Agent 状态：${task.status.name}',
      );
      if (task.status == TaskStatus.completed ||
          task.status == TaskStatus.failed ||
          task.status == TaskStatus.cancelled) {
        _finishRecoveredTask(task, agentMsgId);
        _updateOperation(
          'opencode',
          stepState: ChatOperationStepState.done,
          detail: 'Agent 已返回终态',
        );
        _updateOperation(
          'task',
          stepState: task.status == TaskStatus.failed
              ? ChatOperationStepState.failed
              : ChatOperationStepState.done,
          detail: task.status == TaskStatus.completed
              ? '任务已完成'
              : task.status == TaskStatus.cancelled
              ? '任务已停止'
              : '任务失败',
          title: task.status == TaskStatus.completed ? '回复已恢复' : '任务状态已恢复',
          autoClear: true,
        );
        return;
      }
      if (task.status == TaskStatus.cancelling) {
        state = state.copyWith(
          sending: false,
          taskActive: true,
          cancelRequested: true,
        );
        _updateMessage(agentMsgId, (m) {
          m.state = MessageState.streaming;
          m.streamRecovery = StreamRecovery.following;
        });
        _listenEvents(
          taskId,
          agentMsgId,
          sentAt,
          fastActiveRecovery: fastActiveRecovery,
        );
        _logChatDiag(
          'resume_live_subscription_started',
          taskId: taskId,
          data: {
            'elapsed_ms': DateTime.now()
                .difference(recoveryStartedAt)
                .inMilliseconds,
            'fast': fastActiveRecovery,
          },
        );
        return;
      }

      if (fastActiveRecovery) {
        // Foreground recovery must re-establish the live path before reading
        // historical pages. Agent history is intentionally eventual here:
        // the resumed SSE stream replays the bounded event archive and keeps
        // receiving new deltas, while the full detail hydration can happen
        // later without blocking the composer or the recovery indicator.
        final activeMessage = _messageById(agentMsgId);
        if (activeMessage != null) {
          _updateMessage(agentMsgId, (m) {
            m.streamRecovery = StreamRecovery.following;
            m.statusHint = '';
            if (m.state != MessageState.failed) {
              m.state = MessageState.streaming;
            }
          });
        }
        if (task.status == TaskStatus.waitingApproval &&
            task.approval != null) {
          state = state.copyWith(
            sending: false,
            taskActive: true,
            pendingApproval: task.approval,
            pendingApprovalTaskId: taskId,
          );
        } else if (task.question != null) {
          state = state.copyWith(
            sending: false,
            taskActive: true,
            pendingQuestion: task.question,
            pendingQuestionTaskId: taskId,
          );
        } else {
          state = state.copyWith(sending: true, taskActive: true);
        }
        // Task status is not proof that the SSE handshake or body replay
        // completed. Keep transport and historical content states separate.
        _updateMessage(agentMsgId, (m) {
          m.historyDetails = HistoryDetailsState.loading;
        });
        _updateOperation(
          'opencode',
          stepState: ChatOperationStepState.active,
          detail: '正在接续实时通道',
        );
        _updateOperation(
          'task',
          stepState: ChatOperationStepState.active,
          detail: '正在读取已有回复',
        );
        _listenEvents(
          taskId,
          agentMsgId,
          sentAt,
          fastActiveRecovery: fastActiveRecovery,
        );
        final sessionId = state.currentSessionId;
        if (sessionId != null) {
          unawaited(
            _hydrateSessionSnapshots(
              sessionId: sessionId,
              loadVersion: loadVersion,
              taskIds: [taskId],
            ),
          );
        }
        _logChatDiag(
          'resume_live_subscription_started',
          taskId: taskId,
          data: {
            'elapsed_ms': DateTime.now()
                .difference(recoveryStartedAt)
                .inMilliseconds,
            'fast': fastActiveRecovery,
          },
        );
        return;
      }

      replySyncProgress = ReplySyncProgress('读取历史回复');
      final loadedSnapshot = await _readTaskEventSnapshot(taskId).timeout(
        recoveryHistoryTimeout,
        onTimeout: () => const TaskEventSnapshot(complete: false),
      );
      if (!isCurrent()) return;
      final snapshot = loadedSnapshot.complete
          ? loadedSnapshot
          : const TaskEventSnapshot();
      var recoveredMessageId = agentMsgId;
      final restoredTaskRounds = snapshot.appliedInputs.isNotEmpty;
      if (restoredTaskRounds) {
        recoveredMessageId = _restoreTaskRoundMessages(
          taskId,
          agentMsgId,
          snapshot,
        );
      }
      if (task.status == TaskStatus.waitingApproval && task.approval != null) {
        _updateMessage(recoveredMessageId, (m) {
          if (!restoredTaskRounds &&
              m.thinkingContent.isEmpty &&
              snapshot.reasoning.isNotEmpty) {
            m.thinkingContent = snapshot.reasoning;
          }
          if (!restoredTaskRounds) {
            _applyHistorySnapshotToMessage(m, snapshot, finalizeTools: false);
          }
          m.statusHint = '';
          m.state = MessageState.streaming;
        });
        state = state.copyWith(
          sending: false,
          taskActive: true,
          pendingApproval: task.approval,
          pendingApprovalTaskId: taskId,
        );
      } else if (task.question != null) {
        _updateMessage(recoveredMessageId, (m) {
          if (!restoredTaskRounds &&
              m.thinkingContent.isEmpty &&
              snapshot.reasoning.isNotEmpty) {
            m.thinkingContent = snapshot.reasoning;
          }
          if (!restoredTaskRounds) {
            _applyHistorySnapshotToMessage(m, snapshot, finalizeTools: false);
          }
          m.statusHint = '';
          m.state = MessageState.streaming;
        });
        state = state.copyWith(
          sending: false,
          taskActive: true,
          pendingQuestion: task.question,
          pendingQuestionTaskId: taskId,
        );
      } else {
        state = state.copyWith(sending: true, taskActive: true);
        _updateMessage(recoveredMessageId, (m) {
          if (!restoredTaskRounds &&
              snapshot.reasoning.isNotEmpty &&
              m.thinkingContent != snapshot.reasoning) {
            m.thinkingContent = snapshot.reasoning;
          }
          if (!restoredTaskRounds && snapshot.text.isNotEmpty) {
            if (m.content.isEmpty) {
              m.content = snapshot.text;
              if (m.blocks.isEmpty) {
                _appendTextBlock(m, snapshot.text);
              }
            } else if (snapshot.text.length > m.content.length) {
              m.content = snapshot.text;
            }
          }
          if (!restoredTaskRounds) {
            _applyHistorySnapshotToMessage(m, snapshot, finalizeTools: false);
          }
          m.statusHint = snapshot.statusHint;
          m.finalDeliveryPhase = snapshot.finalDeliveryPhase;
        });
      }
      _updateMessage(recoveredMessageId, (m) {
        m.streamRecovery = StreamRecovery.following;
        if (m.state != MessageState.failed) {
          m.state = MessageState.streaming;
        }
      });
      if (isCurrent()) {
        _updateMessage(recoveredMessageId, (m) {
          m.historyDetails = loadedSnapshot.complete
              ? HistoryDetailsState.none
              : HistoryDetailsState.partial;
        });
        replySyncProgress?.finish(
          loadedSnapshot.complete ? '已有回复已恢复' : '历史回复尚未补齐',
        );
        _updateOperation(
          'opencode',
          stepState: ChatOperationStepState.active,
          detail: '正在接续实时通道',
        );
        _updateOperation(
          'task',
          stepState: loadedSnapshot.complete
              ? ChatOperationStepState.done
              : ChatOperationStepState.failed,
          detail: loadedSnapshot.complete ? '已有回复已恢复' : '历史回复尚未补齐，可重试',
        );
        _listenEvents(
          taskId,
          recoveredMessageId,
          sentAt,
          fastActiveRecovery: fastActiveRecovery,
        );
      }
    } catch (error) {
      if (!isCurrent()) return;
      replySyncProgress?.finish('连接恢复中，正在重试');
      _updateOperation(
        'opencode',
        stepState: ChatOperationStepState.failed,
        detail: 'Agent 状态暂不可确认',
      );
      _updateOperation(
        'task',
        stepState: ChatOperationStepState.active,
        detail: '正在重试连接',
        title: '回复连接重试中',
      );
      _logChatDiag(
        'resume_retry',
        taskId: taskId,
        data: {'error': error.toString()},
      );
      _updateMessage(agentMsgId, (m) {
        if (m.state != MessageState.failed) {
          m.state = MessageState.streaming;
          m.streamRecovery = StreamRecovery.reconnecting;
        }
      });
      // The old subscription was released in the background. A failed HTTP
      // probe must still reconnect and restart bounded terminal polling.
      _listenEvents(
        taskId,
        agentMsgId,
        sentAt,
        fastActiveRecovery: fastActiveRecovery,
      );
    }
  }

  void _updateMessage(String id, void Function(ChatMessage m) updater) {
    final msgs = List<ChatMessage>.from(state.messages);
    final idx = msgs.indexWhere((m) => m.id == id);
    if (idx < 0) return;
    final next = msgs[idx].copyForUpdate();
    updater(next);
    msgs[idx] = next;
    state = state.copyWith(messages: msgs);
  }

  ChatMessage? _messageById(String id) {
    for (final message in state.messages) {
      if (message.id == id) return message;
    }
    return null;
  }

  void _upsertToolCall(ChatMessage message, ToolCallInfo tool) {
    final tools = List<ToolCallInfo>.from(message.toolCalls);
    final idx = _indexOfTool(tools, tool);
    if (idx >= 0) {
      tools[idx] = _coalesceToolCall(tools[idx], tool);
    } else {
      tools.add(tool);
    }
    message.toolCalls = tools;
  }

  void _upsertSubagentTool(ChatMessage message, ToolCallInfo latest) {
    final next = normalizeSubagentToolCall(latest);
    final nodeId = next.metadata['node_id']?.toString().trim() ?? '';
    final phase = next.metadata['phase']?.toString().trim() ?? 'start';
    final tools = List<ToolCallInfo>.from(message.toolCalls);
    if (phase == 'result' && nodeId.isNotEmpty) {
      tools.removeWhere((item) {
        return isSubagentToolCall(item) &&
            (item.metadata['phase']?.toString().trim() ?? '') == 'running' &&
            item.metadata['node_id']?.toString().trim() == nodeId;
      });
      final blocks = List<ChatMessageBlock>.from(message.blocks);
      blocks.removeWhere((block) {
        final existing = block.tool;
        return existing != null &&
            isSubagentToolCall(existing) &&
            (existing.metadata['phase']?.toString().trim() ?? '') ==
                'running' &&
            existing.metadata['node_id']?.toString().trim() == nodeId;
      });
      message.blocks = blocks;
    }
    final index = tools.lastIndexWhere((item) {
      if (!isSubagentToolCall(item)) return false;
      final existingNodeId = item.metadata['node_id']?.toString().trim() ?? '';
      final existingPhase =
          item.metadata['phase']?.toString().trim() ?? 'start';
      return existingNodeId == nodeId && existingPhase == phase;
    });
    final resolved = index >= 0
        ? mergeSubagentToolCall(tools[index], next)
        : next;
    if (index >= 0) {
      tools[index] = resolved;
    } else {
      tools.add(resolved);
    }
    message.toolCalls = tools;
    _upsertToolBlock(message, resolved);
  }

  void _appendTextBlock(ChatMessage message, String content) {
    if (content.isEmpty) return;
    final blocks = List<ChatMessageBlock>.from(message.blocks);
    final textIndex = blocks.isEmpty ? -1 : blocks.length - 1;
    if (textIndex >= 0 && blocks[textIndex].type == ChatMessageBlockType.text) {
      final last = blocks[textIndex];
      blocks[textIndex] = last.copyWith(text: '${last.text}$content');
    } else {
      blocks.add(
        ChatMessageBlock.text(id: 'text_${blocks.length + 1}', text: content),
      );
    }
    message.blocks = blocks;
  }

  void _upsertToolBlock(ChatMessage message, ToolCallInfo tool) {
    final blocks = List<ChatMessageBlock>.from(message.blocks);
    final idx = blocks.indexWhere(
      (block) =>
          block.type == ChatMessageBlockType.tool &&
          block.tool != null &&
          block.tool!.sameIdentity(tool),
    );
    if (idx >= 0) {
      final existing = blocks[idx].tool!;
      blocks[idx] = blocks[idx].copyWith(
        tool: _coalesceToolCall(existing, tool),
      );
    } else {
      final suffix = tool.stableKey.trim().isEmpty
          ? '${blocks.length + 1}'
          : tool.stableKey.trim();
      blocks.add(ChatMessageBlock.tool(id: 'tool_$suffix', tool: tool));
    }
    message.blocks = blocks;
  }

  List<ChatMessageBlock> _blocksFromLegacyContentAndTools(
    String content,
    List<ToolCallInfo> tools,
  ) {
    final blocks = <ChatMessageBlock>[];
    for (final tool in tools) {
      final key = tool.stableKey.trim();
      final suffix = key.isEmpty ? '${blocks.length + 1}' : key;
      blocks.add(ChatMessageBlock.tool(id: 'tool_$suffix', tool: tool));
    }
    if (content.isNotEmpty) {
      blocks.add(ChatMessageBlock.text(id: 'text_1', text: content));
    }
    return List<ChatMessageBlock>.unmodifiable(blocks);
  }

  List<ChatMessageBlock> _blocksWithMergedTools(
    List<ChatMessageBlock> ordered,
    List<ToolCallInfo> mergedTools, {
    String fallbackContent = '',
    bool preferFallbackText = false,
  }) {
    final used = List<bool>.filled(mergedTools.length, false);
    final result = <ChatMessageBlock>[];
    var replacedText = false;
    for (final block in ordered) {
      final existing = block.tool;
      if (existing == null) {
        if (preferFallbackText &&
            !replacedText &&
            block.type == ChatMessageBlockType.text &&
            fallbackContent.trim().isNotEmpty) {
          result.add(block.copyWith(text: fallbackContent));
          replacedText = true;
        } else {
          result.add(block);
        }
        continue;
      }
      final idx = _indexOfTool(mergedTools, existing);
      if (idx >= 0) {
        result.add(block.copyWith(tool: mergedTools[idx]));
        used[idx] = true;
      } else {
        result.add(block);
      }
    }
    final renderedText = result
        .where((b) => b.type == ChatMessageBlockType.text)
        .map((b) => b.text)
        .join();
    if (renderedText.isNotEmpty &&
        fallbackContent.startsWith(renderedText) &&
        fallbackContent.length > renderedText.length) {
      final suffix = fallbackContent.substring(renderedText.length);
      if (result.last.type == ChatMessageBlockType.text) {
        result[result.length - 1] = result.last.copyWith(
          text: '${result.last.text}$suffix',
        );
      } else {
        result.add(
          ChatMessageBlock.text(id: 'text_recovered_tail', text: suffix),
        );
      }
    }
    if (!_blocksHaveVisibleText(result) && fallbackContent.trim().isNotEmpty) {
      result.add(
        ChatMessageBlock.text(id: 'text_content', text: fallbackContent),
      );
    }
    for (var i = 0; i < mergedTools.length; i++) {
      if (used[i]) continue;
      final tool = mergedTools[i];
      result.add(
        ChatMessageBlock.tool(
          id: 'tool_${tool.stableKey.isEmpty ? tool.tool : tool.stableKey}',
          tool: tool,
        ),
      );
    }
    return result;
  }

  String _nextGoalRoundMessageId(String taskId, int roundIndex) {
    final baseId = 'agent_${taskId}_goal_$roundIndex';
    final existing = state.messages.map((message) => message.id).toSet();
    if (!existing.contains(baseId)) return baseId;
    var suffix = 2;
    while (existing.contains('${baseId}_$suffix')) {
      suffix += 1;
    }
    return '${baseId}_$suffix';
  }

  Future<void> _fetchTaskResult(String taskId, String agentMsgId) async {
    try {
      final task = await _repo.getTask(taskId);
      _updateMessage(agentMsgId, (m) {
        if (task.result != null &&
            task.result!.trim().isNotEmpty &&
            m.content != task.result) {
          m.content = task.result!;
        }
        if (task.artifacts.isNotEmpty) {
          m.artifacts = task.artifacts;
          m.files = const [];
        }
      });
    } catch (_) {}
  }

  Future<void> cancelCurrentTask() async {
    // 优先用记录的 taskId，兜底从最后一条 streaming 消息找
    String? taskId = _currentTaskId;
    if (taskId == null) {
      final msgs = state.messages;
      for (int i = msgs.length - 1; i >= 0; i--) {
        if (msgs[i].role == MessageRole.agent &&
            msgs[i].state == MessageState.streaming &&
            msgs[i].taskId != null) {
          taskId = msgs[i].taskId;
          break;
        }
      }
    }
    if (taskId == null) {
      _pendingCancelBeforeTaskCreated = true;
      _markCurrentStreamingMessageStopping();
      _beginOperation('正在停止任务', const [
        ChatOperationStep(
          key: 'phone',
          label: '手机',
          detail: '等待任务创建后发送',
          state: ChatOperationStepState.active,
        ),
        ChatOperationStep(key: 'server', label: '服务端', detail: '等待任务编号'),
        ChatOperationStep(
          key: 'opencode',
          label: 'OpenCode',
          detail: '等待 Agent 接收',
        ),
        ChatOperationStep(key: 'task', label: '任务', detail: '等待停止'),
      ]);
      await AppLogService.log(
        'chat_cancel_queued',
        data: {'agent_id': agentId, 'project_id': projectId},
      );
      return;
    }
    await _requestTaskCancel(taskId);
  }

  Future<void> _requestTaskCancel(String taskId) async {
    if (!_cancelRequestedTaskIds.add(taskId)) {
      _markCurrentStreamingMessageStopping();
      return;
    }
    _markCurrentStreamingMessageStopping();
    _beginOperation('正在停止任务', const [
      ChatOperationStep(
        key: 'phone',
        label: '手机',
        detail: '发送停止指令',
        state: ChatOperationStepState.active,
      ),
      ChatOperationStep(key: 'server', label: '服务端', detail: '等待接收'),
      ChatOperationStep(
        key: 'opencode',
        label: 'OpenCode',
        detail: '等待 Agent 接收',
      ),
      ChatOperationStep(key: 'task', label: '任务', detail: '等待停止'),
    ]);
    try {
      unawaited(
        AppLogService.log(
          'chat_cancel_requested',
          data: {
            'agent_id': agentId,
            'project_id': projectId,
            'task_id': taskId,
          },
        ),
      );
      await _repo.cancelTask(taskId);
      _updateOperation(
        'phone',
        stepState: ChatOperationStepState.done,
        detail: '停止指令已发出',
      );
      _updateOperation(
        'server',
        stepState: ChatOperationStepState.active,
        detail: '服务端已接收，等待 Agent',
      );
    } catch (e) {
      _cancelRequestedTaskIds.remove(taskId);
      if (_disposed || _currentTaskId != taskId) return;
      unawaited(
        AppLogService.log(
          'chat_cancel_failed',
          level: 'error',
          data: {
            'agent_id': agentId,
            'project_id': projectId,
            'task_id': taskId,
            'error': e.toString(),
          },
        ),
      );
      final messages = state.messages
          .map((message) {
            if (message.taskId != taskId || message.statusHint != '正在停止') {
              return message;
            }
            return message.copyForUpdate()..statusHint = '';
          })
          .toList(growable: false);
      state = state.copyWith(
        messages: messages,
        cancelRequested: false,
        error: '取消任务失败: $e',
      );
      _updateOperation(
        'phone',
        stepState: ChatOperationStepState.failed,
        detail: '发送失败',
        title: '停止失败',
        autoClear: true,
      );
    }
  }

  void _markCurrentStreamingMessageStopping() {
    final msgs = List<ChatMessage>.from(state.messages);
    for (int i = msgs.length - 1; i >= 0; i--) {
      if (msgs[i].role == MessageRole.agent &&
          msgs[i].state == MessageState.streaming) {
        msgs[i] = msgs[i].copyForUpdate()..statusHint = '正在停止';
        break;
      }
    }
    state = state.copyWith(
      messages: msgs,
      sending: false,
      taskActive: true,
      clearApproval: true,
      clearQuestion: true,
      cancelRequested: true,
    );
  }

  Future<void> submitApproval(String reply) async {
    final taskId = state.pendingApprovalTaskId;
    if (taskId == null) return;
    try {
      await AppLogService.log(
        'chat_approval_submitted',
        data: {
          'agent_id': agentId,
          'project_id': projectId,
          'task_id': taskId,
          'reply': reply,
        },
      );
      await _repo.submitApproval(taskId, reply);
      state = state.copyWith(
        clearApproval: true,
        sending: true,
        taskActive: true,
      );
    } catch (e) {
      state = state.copyWith(error: '审批提交失败: $e');
    }
  }

  Future<void> submitQuestion(
    String requestId,
    List<List<String>> answers, {
    bool rejected = false,
  }) async {
    final taskId = state.pendingQuestionTaskId;
    if (taskId == null) return;
    try {
      await AppLogService.log(
        'chat_question_submitted',
        data: {
          'agent_id': agentId,
          'project_id': projectId,
          'task_id': taskId,
          'request_id': requestId,
          'rejected': rejected,
          'answer_group_count': answers.length,
        },
      );
      await _repo.submitQuestion(
        taskId,
        requestId,
        answers,
        rejected: rejected,
      );
      state = state.copyWith(
        clearQuestion: true,
        sending: !rejected,
        taskActive: true,
      );
    } catch (e) {
      state = state.copyWith(error: '问题选择提交失败: $e');
    }
  }

  bool _messageNeedsCatchUp(ChatMessage message) {
    if (message.role != MessageRole.agent) return false;
    if (message.state == MessageState.streaming) return true;
    if (message.toolCalls.any((tool) => tool.isRunning)) return true;
    return message.blocks.any(
      (block) => block.tool != null && block.tool!.isRunning,
    );
  }

  Future<void> resumeActiveTaskIfNeeded({
    bool fastActiveRecovery = false,
  }) async {
    if (!_detailsEnabled) return;
    String? taskId = _currentTaskId;
    String? agentMsgId;
    DateTime? sentAt;
    final msgs = state.messages;
    for (int i = msgs.length - 1; i >= 0; i--) {
      final msg = msgs[i];
      if (msg.role != MessageRole.agent || msg.taskId == null) continue;
      if (_messageNeedsCatchUp(msg)) {
        taskId ??= msg.taskId;
        if (taskId == msg.taskId) {
          agentMsgId = msg.id;
          sentAt = msg.createdAt;
          break;
        }
      }
    }
    if (taskId == null || agentMsgId == null || sentAt == null) return;

    _updateMessage(agentMsgId, (message) {
      if (message.state == MessageState.streaming) {
        message.streamRecovery = StreamRecovery.syncing;
      }
    });

    _currentTaskId = taskId;
    await _recoverTaskAfterStreamBreak(
      taskId,
      agentMsgId,
      sentAt,
      fastActiveRecovery: fastActiveRecovery,
    );
  }

  /// Reconcile after the app returns from background. Android may suspend the
  /// SSE socket without delivering an error, so one resume callback is not
  /// enough to establish the task terminal state.
  Future<void> reconcileAfterResume() async {
    if (_disposed) return;
    _appInBackground = false;
    if (!_pageVisible || _resumeReconciliationToken != null) return;
    final token = Object();
    _resumeReconciliationToken = token;
    _armResumeRecoveryWatchdog(token);
    // 回到前台：先解除后台标记并重同步发送队列。
    // 后台期间的长连接断开属于预期行为，这里主动重建订阅并清除残留状态，
    // 避免空队列被渲染成「队列状态异常」。
    _appInBackground = false;
    try {
      // Queue cancellation can wait on a suspended socket. Task reconciliation
      // must proceed independently so a completed task is never held by it.
      unawaited(_resyncQueueAfterResume());
      for (var attempt = 0; attempt < 3; attempt++) {
        if (!_detailsEnabled || !identical(_resumeReconciliationToken, token)) {
          return;
        }
        if (!state.messages.any(_messageNeedsCatchUp)) {
          return;
        }
        final previousLiveVersion = _liveVersion;
        // A live task does not need a complete Agent history before the SSE
        // subscription is restored. The history endpoint can page through
        // hundreds of records while the task is still running, which makes
        // the foreground resume indicator appear stuck. Let the live stream
        // catch up first and hydrate details in the background.
        await resumeActiveTaskIfNeeded(fastActiveRecovery: true);
        if (!_detailsEnabled ||
            !identical(_resumeReconciliationToken, token) ||
            (_eventSub != null && _liveVersion != previousLiveVersion)) {
          return;
        }
        if (!state.messages.any(_messageNeedsCatchUp)) {
          return;
        }
        if (attempt < 2) {
          await Future<void>.delayed(const Duration(milliseconds: 700));
        }
      }

      // A successful active-task read already reconnected SSE. Avoid a
      // destructive session reload while that task is still running.
      if (_currentTaskId != null) return;
      final sessionId = state.currentSessionId?.trim() ?? '';
      if (sessionId.isNotEmpty) {
        await loadSession(sessionId);
      }
    } finally {
      if (identical(_resumeReconciliationToken, token)) {
        _resumeReconciliationToken = null;
        _resumeRecoveryWatchdog?.cancel();
        _resumeRecoveryWatchdog = null;
      }
    }
  }

  /// 回到前台后重建发送队列订阅并清除后台残留的错误状态。
  /// 队列为空时只做静默同步，不产生用户可见提示。
  Future<void> _resyncQueueAfterResume() async {
    final sessionId = state.currentSessionId?.trim() ?? '';
    if (sessionId.isEmpty) return;
    // 先清掉后台期间可能残留的加载态与错误，避免界面停留在「同步中」或异常提示。
    if (state.queueLoading || state.queueError != null) {
      state = state.copyWith(queueLoading: false, clearQueueError: true);
    }
    await _startQueueSync(sessionId, force: true);
  }

  void _startTerminalReconcileTimer(String taskId) {
    _terminalReconcileTimer?.cancel();
    final interval = terminalReconcileInterval;
    if (interval <= Duration.zero) return;
    _terminalReconcileTimer = Timer.periodic(interval, (_) {
      if (!_detailsEnabled || _currentTaskId != taskId) return;
      unawaited(_reconcileLiveTaskIfTerminal(taskId));
    });
  }

  Future<void> _reconcileLiveTaskIfTerminal(String taskId) async {
    if (!_detailsEnabled ||
        _currentTaskId != taskId ||
        _terminalProbeToken != null) {
      return;
    }
    final token = Object();
    _terminalProbeToken = token;
    final liveVersion = _liveVersion;
    final loadVersion = _sessionLoadVersion;
    try {
      final task = await _repo.getTask(taskId).timeout(recoveryStatusTimeout);
      if (!_detailsEnabled ||
          _currentTaskId != taskId ||
          liveVersion != _liveVersion ||
          loadVersion != _sessionLoadVersion) {
        return;
      }
      if (task.status != TaskStatus.completed &&
          task.status != TaskStatus.failed &&
          task.status != TaskStatus.cancelled) {
        return;
      }
      var agentMsgId = _activeTaskAgentMessageIds[taskId];
      if (agentMsgId == null || _messageById(agentMsgId) == null) {
        for (final message in state.messages.reversed) {
          if (message.role == MessageRole.agent && message.taskId == taskId) {
            agentMsgId = message.id;
            break;
          }
        }
      }
      if (agentMsgId != null) _finishRecoveredTask(task, agentMsgId);
    } catch (error) {
      if (_detailsEnabled &&
          _currentTaskId == taskId &&
          liveVersion == _liveVersion) {
        _logChatDiag(
          'terminal_probe_retry',
          taskId: taskId,
          data: {'error': error.toString()},
        );
      }
    } finally {
      if (identical(_terminalProbeToken, token)) _terminalProbeToken = null;
    }
  }

  void _cancelEventSub({bool clearPendingCancel = true}) {
    _flushEventDeltas?.call();
    _flushEventDeltas = null;
    _liveVersion++;
    _terminalProbeToken = null;
    _terminalReconcileTimer?.cancel();
    _terminalReconcileTimer = null;
    _resumeRecoveryWatchdog?.cancel();
    _resumeRecoveryWatchdog = null;
    _eventSub?.cancel();
    _eventSub = null;
    _currentTaskId = null;
    if (clearPendingCancel) {
      _pendingCancelBeforeTaskCreated = false;
    }
  }

  void _cancelQueueSub() {
    _queueSyncVersion++;
    _queueInitialSyncPending = false;
    _queueInitialSyncFailures = 0;
    _queueReconnectTimer?.cancel();
    _queueReconnectTimer = null;
    unawaited(_queueSub?.cancel());
    _queueSub = null;
    _queueSessionId = null;
  }

  @override
  void dispose() {
    _disposed = true;
    _operationClearTimer?.cancel();
    _operationClearTimer = null;
    _cancelEventSub();
    _cancelQueueSub();
    unawaited(_eventWriter.close());
    super.dispose();
  }
}

// 使用 family 按 agentId 区分不同对话页实例（不使用 autoDispose，保持对话状态跨页面切换）
final chatProvider =
    StateNotifierProvider.family<
      ChatNotifier,
      ChatState,
      (String agentId, String projectId)
    >(
      (ref, args) => ChatNotifier(
        repo: ref.watch(chatRepositoryProvider),
        agentId: args.$1,
        projectId: args.$2,
        initiallyVisible: false,
      ),
    );

final globalPromptProvider = Provider<String>(
  (ref) => ref.watch(settingsProvider).globalPrompt,
);

String? _parseGoalCommand(String text) {
  final trimmed = text.trim();
  if (!trimmed.startsWith('/goal ')) return null;
  final goal = trimmed.substring('/goal '.length).trim();
  return goal.isEmpty ? null : goal;
}

int? _goalEventIteration(Map<String, dynamic> event) {
  final metadata = event['metadata'] is Map<String, dynamic>
      ? event['metadata'] as Map<String, dynamic>
      : const <String, dynamic>{};
  final raw = event['iteration'] ?? metadata['iteration'];
  if (raw is num) return raw.toInt();
  if (raw is String) return int.tryParse(raw);
  return null;
}

String _taskEventKey(Map<String, dynamic> event) {
  final sequence = (event['sequence'] as num?)?.toInt();
  if (sequence != null && sequence > 0) {
    // Stable source sequence is safer than hashing content: two distinct
    // deltas may legitimately have identical text and timestamps.
    return 'sequence:$sequence';
  }
  final type = event['type']?.toString() ?? '';
  final sentAt = event['sent_at']?.toString() ?? '';
  final sessionId = event['session_id']?.toString() ?? '';
  final field = event['field']?.toString() ?? '';
  final content = event['content']?.toString() ?? '';
  final iteration = _goalEventIteration(event)?.toString() ?? '';
  final metadata = event['metadata'] == null
      ? ''
      : jsonEncode(event['metadata']);
  final tool = event['tool'] is Map<String, dynamic>
      ? event['tool'] as Map<String, dynamic>
      : const <String, dynamic>{};
  final toolKey = [
    tool['call_id']?.toString() ?? '',
    tool['id']?.toString() ?? '',
    tool['status']?.toString() ?? '',
    tool['output']?.toString() ?? '',
    tool['error']?.toString() ?? '',
  ].join('\u{1e}');
  final fallback = [
    type,
    sentAt,
    sessionId,
    field,
    content,
    iteration,
    metadata,
    toolKey,
  ].join('\u{1f}');
  return 'hash:${sha256.convert(utf8.encode(fallback))}';
}

String _inputRoundAgentMessageId(
  String taskId,
  String queueItemId,
  int injectionVersion,
) {
  final stableInputId = queueItemId.trim().isEmpty
      ? 'input_$injectionVersion'
      : queueItemId.trim();
  final versionSuffix = injectionVersion > 0 ? '_$injectionVersion' : '';
  return 'agent_${taskId}_after_$stableInputId$versionSuffix';
}

String _formatGoalHint(String type, Map<String, dynamic> event) {
  final metadata = event['metadata'] is Map<String, dynamic>
      ? event['metadata'] as Map<String, dynamic>
      : const <String, dynamic>{};
  final iteration =
      (event['iteration'] as num?)?.toInt() ??
      (metadata['iteration'] as num?)?.toInt() ??
      0;
  final max =
      (event['max'] as num?)?.toInt() ??
      (metadata['max'] as num?)?.toInt() ??
      0;
  final reason =
      event['reason'] as String? ?? metadata['reason'] as String? ?? '';
  final suffix = max > 0 ? '（$iteration/$max）' : '';
  return switch (type) {
    'goal_created' => '目标模式已启动$suffix',
    'goal_continued' => '目标未完成，继续执行$suffix',
    'goal_checkpoint' => '目标进度已保存$suffix',
    'goal_paused' => reason.isEmpty ? '目标已暂停$suffix' : '目标已暂停：$reason',
    'goal_failed' => reason.isEmpty ? '目标执行失败' : '目标执行失败：$reason',
    _ => '',
  };
}
