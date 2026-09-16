import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../../../core/config/api_client.dart';
import '../../../core/services/app_log_service.dart';
import '../../../core/storage/app_storage.dart';
import '../../../features/settings/settings_provider.dart';
import '../data/artifact_cache_service.dart';
import '../data/chat_model.dart';
import '../data/chat_repository.dart';
import '../data/local_chat_store.dart';
import 'chat_error_formatter.dart';

final chatRepositoryProvider = Provider((ref) => ChatRepository());

int _elapsedMsSince(DateTime startedAt) =>
    DateTime.now().difference(startedAt).inMilliseconds;

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
  });

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
    );
  }
}

class ChatNotifier extends StateNotifier<ChatState> {
  static const int _sessionHistoryPageSize = 5;
  static const int _queueInitialSyncFailureLimit = 2;

  /// 应用进入后台时挂起长连接属于预期行为，期间产生的队列断开不应提示用户。
  bool _appInBackground = false;

  /// 标记应用进入后台：后续队列连接断开按预期行为处理，不产生用户可见错误。
  void markAppBackgrounded() {
    _appInBackground = true;
  }

  final ChatRepository _repo;
  final LocalChatStore _localStore;
  final String agentId;
  final String projectId;

  StreamSubscription<Map<String, dynamic>>? _eventSub;
  StreamSubscription<ChatQueueSnapshot>? _queueSub;
  Timer? _terminalReconcileTimer;
  String? _currentTaskId; // 当前正在进行的 task id，用于取消
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
  bool _resumeReconciliationInFlight = false;

  @visibleForTesting
  Duration terminalReconcileInterval = const Duration(seconds: 8);

  ChatNotifier({
    required ChatRepository repo,
    required this.agentId,
    required this.projectId,
    LocalChatStore? localStore,
  }) : _repo = repo,
       _localStore = localStore ?? createLocalChatStore(),
       super(const ChatState());

  String get _localIdentity {
    try {
      return AppStorage.storageIdentity;
    } catch (_) {
      return '';
    }
  }

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

  final Map<String, int> _persistOkCounts = {};

  void _persistTaskEvent(
    String taskId,
    String eventKey,
    Map<String, dynamic> event,
  ) {
    final identity = _localIdentity;
    if (identity.isEmpty) {
      _logChatDiag(
        'persist_skip',
        taskId: taskId,
        data: {'reason': 'empty_identity'},
      );
      return;
    }
    unawaited(() async {
      try {
        await _localStore.init();
        final inserted = await _localStore.recordEvent(
          identity: identity,
          taskId: taskId,
          eventKey: eventKey,
          payload: event,
        );
        if (!inserted) return;
        final count = (_persistOkCounts[taskId] ?? 0) + 1;
        _persistOkCounts[taskId] = count;
        final type = event['type']?.toString() ?? '';
        if (count == 1 ||
            count % 25 == 0 ||
            type == 'tool_updated' ||
            type == 'completed' ||
            type == 'failed' ||
            type == 'cancelled') {
          _logChatDiag(
            'persist_ok',
            taskId: taskId,
            data: {'count': count, 'type': type},
          );
        }
      } catch (error) {
        debugPrint('[Chat] local event write failed: $error');
        _logChatDiag(
          'persist_failed',
          taskId: taskId,
          data: {'error': error.toString()},
        );
      }
    }());
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
    unawaited(AppLogService.log('chat_latency', data: payload));
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
    unawaited(AppLogService.log('chat_diag', data: payload));
  }

  String _blockOrderSummary(List<ChatMessageBlock> blocks) {
    final parts = <String>[];
    var textBlocks = 0;
    var toolBlocks = 0;
    for (final block in blocks) {
      if (block.tool != null) {
        toolBlocks += 1;
        parts.add(block.tool!.tool);
      } else if (block.type == ChatMessageBlockType.text &&
          block.text.trim().isNotEmpty) {
        textBlocks += 1;
        parts.add('text(${block.text.trim().length})');
      }
    }
    final preview = parts.length <= 12
        ? parts.join('>')
        : '${parts.take(8).join('>')}...(${parts.length})';
    return 'text=$textBlocks tools=$toolBlocks mixed=${_blocksHaveMixedInsertion(blocks)} $preview';
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
        .where((file) => file.isImage && file.url.isNotEmpty)
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
  _messagesFromHistoryTasks(List<TaskModel> sortedTasks) {
    final msgs = <ChatMessage>[];
    final snapshotTaskIds = <String>[];
    for (final task in sortedTasks) {
      final taskCreatedAt =
          task.createdAt ?? DateTime.fromMillisecondsSinceEpoch(0);
      if (task.userText != null && task.userText!.isNotEmpty) {
        msgs.add(
          ChatMessage(
            id: 'user_${task.taskId}',
            role: MessageRole.user,
            state: MessageState.done,
            content: task.userText!,
            createdAt: taskCreatedAt,
            taskId: task.taskId,
            taskCreatedAt: taskCreatedAt,
            taskMessageIndex: 0,
          ),
        );
      }

      String agentContent = task.result ?? '';
      String thinkingContent = '';
      String statusHint = '';
      final needsEventSnapshot =
          task.status != TaskStatus.dispatched &&
          task.status != TaskStatus.started &&
          task.status != TaskStatus.running;
      final isActive = _isTaskActive(task.status);
      if (needsEventSnapshot ||
          task.status == TaskStatus.waitingApproval ||
          isActive) {
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
      msgs.add(
        ChatMessage(
          id: 'agent_${task.taskId}',
          role: MessageRole.agent,
          state: msgState,
          content: agentContent,
          thinkingContent: thinkingContent,
          statusHint: statusHint,
          errorDetail: task.status == TaskStatus.failed
              ? _taskErrorDetail(task, const TaskEventSnapshot())
              : '',
          createdAt: taskCreatedAt,
          taskId: task.taskId,
          taskCreatedAt: taskCreatedAt,
          taskMessageIndex: 1,
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
    _cancelEventSub();
    _cancelQueueSub();
    final loadVersion = ++_sessionLoadVersion;
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
      state = state.copyWith(
        messages: history.messages,
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
      final history = _messagesFromHistoryTasks(sortedTasks);
      final existingIds = state.messages.map((message) => message.id).toSet();
      final prependMessages = history.messages
          .where((message) => !existingIds.contains(message.id))
          .toList(growable: false);
      state = state.copyWith(
        messages: [...prependMessages, ...state.messages],
        hasEarlierHistory: tasks.length > _sessionHistoryPageSize,
      );
      // Snapshot hydration can expand one historical task into multiple visual
      // messages. Keep the history transaction active until that expansion is
      // complete so the UI never treats it as new conversation output.
      if (history.snapshotTaskIds.isNotEmpty) {
        await _hydrateSessionSnapshots(
          sessionId: sessionId,
          loadVersion: loadVersion,
          taskIds: history.snapshotTaskIds,
        );
      }
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

  Future<bool> sendMessage(
    String text, {
    ModelInfo? model,
    String permissionMode = 'ask',
    String? variant,
    List<AttachedFile>? attachedFilesOverride,
    String? goalOverride,
    int? goalMaxIterations,
  }) async {
    debugPrint(
      '[Chat] sendMessage called: "$text", sending=${state.sending}, taskActive=${state.taskActive}',
    );
    if (text.trim().isEmpty) return false;

    final commandGoal = _parseGoalCommand(text);
    final configuredGoal = goalOverride?.trim();
    final latestGoal = _latestGoalProgressEntry();
    final allowConfiguredGoal = latestGoal?.isTerminal != true;
    final goal =
        commandGoal ??
        (allowConfiguredGoal &&
                configuredGoal != null &&
                configuredGoal.isNotEmpty
            ? configuredGoal
            : null);
    final sendText = commandGoal ?? text;
    final filesToSend = List<AttachedFile>.from(
      attachedFilesOverride ?? state.attachedFiles,
    );
    final modelToSend = model;
    // 加入 agent 流式气泡（占位），记录发送时刻用于首字计时
    final sentAt = DateTime.now();
    final imageGeneration = modelToSend?.supportsImageOutput == true
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
        text: sendText,
        attachedFiles: filesToSend,
        model: modelToSend,
        permissionMode: permissionMode,
        variant: variant,
        goal: goal,
        goalMaxIterations:
            goalMaxIterations ?? GoalSettings.defaultMaxIterations,
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
        text: sendText,
        sessionId: state.currentSessionId,
        attachedFiles: filesToSend.isNotEmpty ? filesToSend : null,
        model: modelToSend,
        permissionMode: permissionMode,
        variant: variant,
        goal: goal,
        goalMaxIterations:
            goalMaxIterations ?? GoalSettings.defaultMaxIterations,
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
    if (_disposed || normalized.isEmpty) return;
    if (!force && _queueSessionId == normalized && _queueSub != null) return;
    if (_queueSessionId != normalized) {
      _queueInitialSyncFailures = 0;
    }
    final syncVersion = ++_queueSyncVersion;
    _queueReconnectTimer?.cancel();
    await _queueSub?.cancel();
    if (_disposed || syncVersion != _queueSyncVersion) return;
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
              state = state.copyWith(queueLoading: false, clearQueueError: true);
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
    final userMessageId = 'user_queue_${item.id}';
    final agentMessageId = 'agent_${item.taskId}';
    final taskCreatedAt = item.createdAt ?? DateTime.now();
    final messages = List<ChatMessage>.from(state.messages);
    if (!messages.any((message) => message.id == userMessageId)) {
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

  void _listenEvents(String taskId, String agentMsgId, DateTime sentAt) {
    debugPrint('[Chat] _listenEvents taskId=$taskId agentMsgId=$agentMsgId');
    _cancelEventSub(clearPendingCancel: false);
    _currentTaskId = taskId;
    _activeTaskAgentMessageIds.putIfAbsent(taskId, () => agentMsgId);
    String activeAgentMessageId() =>
        _activeTaskAgentMessageIds[taskId] ?? agentMsgId;
    void setActiveAgentMessageId(String messageId) {
      _activeTaskAgentMessageIds[taskId] = messageId;
    }

    var activeRoundStartedAt = sentAt;
    final sseListenStartedAt = DateTime.now();
    bool firstDelta = true;
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
        firstDelta = true;
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
      firstDelta = true;
    }

    _logChatLatency('sse_listen_started', taskId: taskId, startedAt: sentAt);
    _logChatDiag('sse_listen_started', taskId: taskId);
    _startTerminalReconcileTimer(taskId);
    final sseCounts = <String, int>{};
    var persistedCount = 0;
    var dupDropped = 0;
    var lastSseSummaryAt = DateTime.now();

    void logSseSummary({required bool force}) {
      final total = sseCounts.values.fold<int>(0, (sum, count) => sum + count);
      if (total == 0) return;
      final elapsed = DateTime.now().difference(lastSseSummaryAt);
      if (!force && elapsed.inMilliseconds < 2000 && total % 40 != 0) {
        return;
      }
      lastSseSummaryAt = DateTime.now();
      _logChatDiag(
        'sse_summary',
        taskId: taskId,
        data: {
          'counts': Map<String, int>.from(sseCounts),
          'persisted': persistedCount,
          'dup_dropped': dupDropped,
          'listen_ms': _elapsedMsSince(sseListenStartedAt),
        },
      );
    }

    _eventSub = _repo
        .watchTaskEvents(taskId)
        .listen(
          (event) {
            final eventKey = _taskEventKey(event);
            if (!handledEventKeys.add(eventKey)) {
              dupDropped += 1;
              return;
            }
            persistedCount += 1;
            _persistTaskEvent(taskId, eventKey, event);
            final type = event['type'] as String? ?? 'unknown';
            sseCounts[type] = (sseCounts[type] ?? 0) + 1;
            final notable =
                type == 'tool_updated' ||
                type == 'completed' ||
                type == 'failed' ||
                type == 'cancelled' ||
                type == 'started';
            if (notable) {
              _logChatDiag(
                'sse_event',
                taskId: taskId,
                data: {
                  'type': type,
                  'tool': type == 'tool_updated'
                      ? (event['tool'] is Map
                            ? (event['tool'] as Map)['tool']
                            : null)
                      : null,
                  'status': type == 'tool_updated'
                      ? (event['tool'] is Map
                            ? (event['tool'] as Map)['status']
                            : null)
                      : null,
                  'listen_ms': _elapsedMsSince(sseListenStartedAt),
                },
              );
            }
            logSseSummary(force: notable);
            if (firstSseEvent) {
              firstSseEvent = false;
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
                  final generated =
                      (usage.outputTokens ?? 0) + (usage.reasoningTokens ?? 0);
                  if (generated > 0) {
                    _updateMessage(activeAgentMessageId(), (m) {
                      m.tokenCount = math.max(m.tokenCount ?? 0, generated);
                    });
                  }
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
                      !messages.any((message) => message.id == inputMessageId)) {
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
                  firstDelta = true;
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
                  // 记录首字耗时（仅第一个 text delta）
                  if (firstDelta && field == 'text' && content.isNotEmpty) {
                    m.firstTokenTime = DateTime.now().difference(
                      activeRoundStartedAt,
                    );
                    firstDelta = false;
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
                  final generatedChars =
                      m.content.runes.length + m.thinkingContent.runes.length;
                  if (generatedChars > 0) {
                    final estimated = (generatedChars + 3) ~/ 4;
                    if ((m.tokenCount ?? 0) < estimated) {
                      m.tokenCount = estimated;
                    }
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
                final visibleCompletedContent = roundContent.isNotEmpty
                    ? roundContent
                    : completedContent;
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
                final totalTokens = usage != null
                    ? ((usage['output_tokens'] as num?)?.toInt() ?? 0) +
                          ((usage['reasoning_tokens'] as num?)?.toInt() ?? 0)
                    : null;
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
                    m.content = visibleCompletedContent;
                    if (!_blocksHaveVisibleText(m.blocks)) {
                      _appendTextBlock(m, visibleCompletedContent);
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
                  if (totalTokens != null && totalTokens > 0) {
                    m.tokenCount = math.max(m.tokenCount ?? 0, totalTokens);
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
                _updateMessage(activeAgentMessageId(), (m) {
                  m.statusHint = '';
                  m.imageGeneration = null;
                  m.finalDeliveryPhase = FinalDeliveryPhase.idle;
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
                _updateMessage(activeAgentMessageId(), (m) {
                  m.statusHint = '';
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
            debugPrint('[Chat] SSE error: $e');
            logSseSummary(force: true);
            _logChatDiag(
              'sse_error',
              taskId: taskId,
              data: {'error': e.toString()},
            );
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
              ),
            );
          },
          onDone: () {
            debugPrint('[Chat] SSE stream done');
            logSseSummary(force: true);
            _logChatDiag(
              'sse_done',
              taskId: taskId,
              data: {
                'has_first_text_delta': firstTextDeltaLogged,
                'persisted': persistedCount,
              },
            );
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
    return state.currentSessionId == sessionId &&
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
      server = const TaskEventSnapshot();
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
      _logChatDiag(
        'history_local_loaded',
        taskId: taskId,
        data: {
          'events': events.length,
          'order': _blockOrderSummary(snapshot.blocks),
        },
      );
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
    _logChatDiag(
      'history_order',
      taskId: taskId,
      data: {
        'used': used,
        'local': _blockOrderSummary(local.blocks),
        'server': _blockOrderSummary(server.blocks),
      },
    );
    if (ordered.isEmpty) {
      // A snapshot can carry round/input/lifecycle data without materialized
      // blocks (for example, a completed task restored from the event log).
      // Do not discard that structured history merely because both block
      // arrays are empty.
      return _snapshotHasStructuredData(server) ? server : local;
    }
    final preferServerText = used == 'server' && server.text.isNotEmpty;
    return TaskEventSnapshot(
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
      rounds: server.rounds.isNotEmpty ? server.rounds : local.rounds,
      contextUsage: server.contextUsage ?? local.contextUsage,
      appliedInputs: server.appliedInputs.isNotEmpty
          ? server.appliedInputs
          : local.appliedInputs,
      finalDeliveryPhase: server.finalDeliveryPhase,
    );
  }

  bool _snapshotHasStructuredData(TaskEventSnapshot snapshot) {
    return snapshot.text.isNotEmpty ||
        snapshot.reasoning.isNotEmpty ||
        snapshot.statusHint.isNotEmpty ||
        snapshot.goalProgress.isNotEmpty ||
        snapshot.planHistory.isNotEmpty ||
        snapshot.toolCalls.isNotEmpty ||
        snapshot.rounds.isNotEmpty ||
        snapshot.appliedInputs.isNotEmpty ||
        snapshot.errorDetail.isNotEmpty ||
        snapshot.contextUsage != null ||
        snapshot.finalDeliveryPhase != FinalDeliveryPhase.idle;
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

  Future<void> _hydrateSessionSnapshots({
    required String sessionId,
    required int loadVersion,
    required List<String> taskIds,
  }) async {
    final startedAt = DateTime.now();
    _logChatDiag('history_hydrate_start', data: {'tasks': taskIds.length});
    final snapshots = await Future.wait(
      taskIds.map((taskId) async {
        try {
          final snapshot = await _readTaskEventSnapshot(taskId);
          return MapEntry(taskId, snapshot);
        } catch (_) {
          return const MapEntry('', TaskEventSnapshot());
        }
      }),
    );
    if (!_isCurrentSessionLoad(sessionId, loadVersion)) return;

    final snapshotMap = <String, TaskEventSnapshot>{};
    for (final entry in snapshots) {
      if (entry.key.isEmpty) continue;
      snapshotMap[entry.key] = entry.value;
    }
    if (snapshotMap.isEmpty) {
      _logChatDiag(
        'history_hydrate_end',
        data: {'tasks': 0, 'elapsed_ms': _elapsedMsSince(startedAt)},
      );
      return;
    }

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
      final mutableMessage = original.copyForUpdate();
      msgs[i] = mutableMessage;
      final msg = mutableMessage;
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
          var endIndex = firstIndex + 1;
          while (endIndex < msgs.length &&
              msgs[endIndex].taskId == msg.taskId) {
            endIndex += 1;
          }
          final roundMessages = _messagesFromTaskRoundSnapshot(
            msgs[firstIndex],
            snapshot,
          );
          if (roundMessages.isNotEmpty) {
            msgs.replaceRange(firstIndex, endIndex, roundMessages);
            if (_currentTaskId == msg.taskId) {
              for (final roundMessage in roundMessages.reversed) {
                if (roundMessage.role == MessageRole.agent) {
                  _activeTaskAgentMessageIds[msg.taskId!] = roundMessage.id;
                  break;
                }
              }
            }
            changed = true;
            i = firstIndex + roundMessages.length - 1;
            continue;
          }
        }
      }
      if (msg.content.isEmpty && snapshot.text.isNotEmpty) {
        msg.content = snapshot.text;
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
    _logChatDiag(
      'history_hydrate_end',
      data: {
        'tasks': snapshotMap.length,
        'changed': changed,
        'elapsed_ms': _elapsedMsSince(startedAt),
        'orders': {
          for (final message in msgs)
            if (message.role == MessageRole.agent &&
                message.taskId != null &&
                snapshotMap.containsKey(message.taskId))
              message.taskId!: _blockOrderSummary(message.blocks),
        },
      },
    );
  }

  bool _shouldRenderSnapshotAsGoalRounds(TaskEventSnapshot snapshot) {
    return snapshot.rounds.length > 1 &&
        snapshot.rounds.any((round) => round.hasGoalProgress);
  }

  List<ChatMessage> _messagesFromTaskRoundSnapshot(
    ChatMessage base,
    TaskEventSnapshot snapshot,
  ) {
    final taskId = base.taskId;
    if (taskId == null) return const [];
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
    if (base.state == MessageState.streaming && rounds.isNotEmpty) {
      final candidate = rounds.length - 1;
      if (!rounds[candidate].hasOutput &&
          snapshot.appliedInputs.any(
            (input) => input.afterRoundIndex == candidate - 1,
          )) {
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
            taskMessageIndex: result.length,
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
            timelineIndex: result.length,
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
    return result;
  }

  String _restoreTaskRoundMessages(
    String taskId,
    String fallbackAgentMessageId,
    TaskEventSnapshot snapshot,
  ) {
    if (snapshot.appliedInputs.isEmpty || snapshot.rounds.isEmpty) {
      return fallbackAgentMessageId;
    }
    final messages = List<ChatMessage>.from(state.messages);
    final firstAgentIndex = messages.indexWhere(
      (message) =>
          message.taskId == taskId && message.role == MessageRole.agent,
    );
    if (firstAgentIndex < 0) return fallbackAgentMessageId;

    var endIndex = firstAgentIndex + 1;
    while (endIndex < messages.length && messages[endIndex].taskId == taskId) {
      endIndex += 1;
    }
    final restored = _messagesFromTaskRoundSnapshot(
      messages[firstAgentIndex],
      snapshot,
    );
    if (restored.isEmpty) return fallbackAgentMessageId;
    messages.replaceRange(firstAgentIndex, endIndex, restored);
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
    final canReuseBaseContent = messageId == null && isLast;
    final visibleContent = roundContent.isNotEmpty
        ? roundContent
        : (canReuseBaseContent ? base.content : '');
    final plan = round.planHistory.isNotEmpty
        ? round.planHistory.last.plan
        : null;
    return ChatMessage(
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
      tokenCount: round.tokenCount ?? (isLast ? base.tokenCount : null),
      finalDeliveryPhase: isLast
          ? base.finalDeliveryPhase
          : FinalDeliveryPhase.idle,
      firstTokenTime: visibleIndex == 0 ? base.firstTokenTime : null,
    );
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
    if (snapshot.appliedInputs.isNotEmpty) {
      for (final round in snapshot.rounds.reversed) {
        if (!round.hasOutput) continue;
        if (round.text.trim().isNotEmpty) return round.text;
        final goalContent = _goalRoundCompletionContent(round);
        if (goalContent.isNotEmpty) return goalContent;
      }
    }
    final result = task.result ?? '';
    if (result.trim().isNotEmpty) return result;
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
  }) {
    final tools = finalizeTools
        ? _finalizedToolCalls(snapshot.toolCalls)
        : snapshot.toolCalls;
    if (snapshot.reasoning.isNotEmpty &&
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
    TaskEventRoundSnapshot? finalRound;
    if (snapshot.appliedInputs.isNotEmpty) {
      for (final round in snapshot.rounds.reversed) {
        if (round.hasOutput) {
          finalRound = round;
          break;
        }
      }
    }
    final finalContent = finalRound == null
        ? _finalTaskContent(task, snapshot)
        : (finalRound.text.isNotEmpty
              ? finalRound.text
              : _goalRoundCompletionContent(finalRound));
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
      message.content = finalContent;
      if (!_blocksHaveVisibleText(message.blocks)) {
        _appendTextBlock(message, finalContent);
      }
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

  Future<void> _recoverTaskAfterStreamBreak(
    String taskId,
    String agentMsgId,
    DateTime sentAt,
  ) async {
    if (_currentTaskId != taskId) return;
    try {
      final task = await _repo.getTask(taskId);
      if (task.status == TaskStatus.cancelling ||
          task.status == TaskStatus.cancelled) {
        final snapshot = await _readTaskEventSnapshot(taskId);
        _updateMessage(agentMsgId, (m) {
          if (m.thinkingContent.isEmpty && snapshot.reasoning.isNotEmpty) {
            m.thinkingContent = snapshot.reasoning;
          }
          _applyHistorySnapshotToMessage(m, snapshot, finalizeTools: true);
          m.statusHint = '';
          m.errorDetail = _taskErrorDetail(task, snapshot);
          m.imageGeneration = null;
          m.finalDeliveryPhase = FinalDeliveryPhase.idle;
          if (m.content.isEmpty) {
            if (snapshot.text.isNotEmpty) {
              m.content = snapshot.text;
              if (m.blocks.isEmpty) {
                _appendTextBlock(m, snapshot.text);
              }
            } else if (task.error?.isNotEmpty == true) {
              m.content = formatChatTaskError(task.error);
              if (m.blocks.isEmpty) {
                _appendTextBlock(m, m.content);
              }
            } else if (task.status == TaskStatus.cancelled) {
              m.content = '已停止';
              if (m.blocks.isEmpty) {
                _appendTextBlock(m, m.content);
              }
            } else {
              m.content = '任务超时，正在终止执行';
              if (m.blocks.isEmpty) {
                _appendTextBlock(m, m.content);
              }
            }
          }
          m.state = task.status == TaskStatus.cancelled
              ? MessageState.done
              : MessageState.failed;
        });
        state = state.copyWith(
          sending: false,
          taskActive: task.status == TaskStatus.cancelling,
          clearApproval: true,
          clearQuestion: true,
        );
        if (task.status == TaskStatus.cancelling) {
          await Future<void>.delayed(const Duration(milliseconds: 600));
          if (_currentTaskId == taskId) {
            _listenEvents(taskId, agentMsgId, sentAt);
          }
        }
        return;
      }
      if (task.status == TaskStatus.completed) {
        final snapshot = await _readTaskEventSnapshot(taskId);
        var completedMessageId = agentMsgId;
        if (snapshot.appliedInputs.isNotEmpty) {
          completedMessageId = _restoreTaskRoundMessages(
            taskId,
            agentMsgId,
            snapshot,
          );
        }
        _updateMessage(
          completedMessageId,
          (m) => _applyCompletedTaskSnapshot(m, task, snapshot),
        );
        _closeStreamingAgentMessagesForTask(taskId);
        _cancelEventSub();
        state = state.copyWith(
          sending: false,
          taskActive: false,
          clearApproval: true,
          clearQuestion: true,
        );
        return;
      }
      if (task.status == TaskStatus.failed) {
        final snapshot = await _readTaskEventSnapshot(taskId);
        _updateMessage(agentMsgId, (m) {
          _applyHistorySnapshotToMessage(m, snapshot, finalizeTools: true);
          m.statusHint = '';
          m.imageGeneration = null;
          m.finalDeliveryPhase = FinalDeliveryPhase.idle;
          m.errorDetail = _taskErrorDetail(task, snapshot);
          m.state = MessageState.failed;
          if (m.content.isEmpty) {
            m.content = formatChatTaskError(
              task.error?.isNotEmpty == true ? task.error! : '任务执行失败',
            );
            if (m.blocks.isEmpty) {
              _appendTextBlock(m, m.content);
            }
          }
        });
        _cancelEventSub();
        state = state.copyWith(
          sending: false,
          taskActive: false,
          clearApproval: true,
          clearQuestion: true,
        );
        return;
      }
      final snapshot = await _readTaskEventSnapshot(taskId);
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
        if (m.state != MessageState.failed) {
          m.state = MessageState.streaming;
        }
      });
      await Future<void>.delayed(const Duration(milliseconds: 600));
      if (_currentTaskId == taskId) {
        _listenEvents(taskId, recoveredMessageId, sentAt);
      }
    } catch (_) {
      _updateMessage(agentMsgId, (m) {
        if (m.state != MessageState.failed) {
          m.state = MessageState.streaming;
        }
      });
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
      await AppLogService.log(
        'chat_cancel_queued',
        data: {'agent_id': agentId, 'project_id': projectId},
      );
      _markCurrentStreamingMessageStopping();
      return;
    }
    await _requestTaskCancel(taskId);
  }

  Future<void> _requestTaskCancel(String taskId) async {
    if (!_cancelRequestedTaskIds.add(taskId)) {
      _markCurrentStreamingMessageStopping();
      return;
    }
    try {
      await AppLogService.log(
        'chat_cancel_requested',
        data: {'agent_id': agentId, 'project_id': projectId, 'task_id': taskId},
      );
      await _repo.cancelTask(taskId);
    } catch (e) {
      _cancelRequestedTaskIds.remove(taskId);
      await AppLogService.log(
        'chat_cancel_failed',
        level: 'error',
        data: {
          'agent_id': agentId,
          'project_id': projectId,
          'task_id': taskId,
          'error': e.toString(),
        },
      );
      state = state.copyWith(error: '取消任务失败: $e');
    }
    _markCurrentStreamingMessageStopping();
  }

  void _markCurrentStreamingMessageStopping() {
    final msgs = List<ChatMessage>.from(state.messages);
    for (int i = msgs.length - 1; i >= 0; i--) {
      if (msgs[i].role == MessageRole.agent &&
          msgs[i].state == MessageState.streaming) {
        msgs[i].statusHint = '正在停止';
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

  Future<void> resumeActiveTaskIfNeeded() async {
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

    try {
      final task = await _repo.getTask(taskId);
      if (!_isTaskActive(task.status)) {
        if (task.status == TaskStatus.completed) {
          final snapshot = await _readTaskEventSnapshot(taskId);
          _updateMessage(
            agentMsgId,
            (m) => _applyCompletedTaskSnapshot(m, task, snapshot),
          );
          _closeStreamingAgentMessagesForTask(taskId);
          state = state.copyWith(
            sending: false,
            taskActive: false,
            clearApproval: true,
            clearQuestion: true,
          );
        } else if (task.status == TaskStatus.failed ||
            task.status == TaskStatus.cancelling ||
            task.status == TaskStatus.cancelled) {
          final snapshot = await _readTaskEventSnapshot(taskId);
          _updateMessage(agentMsgId, (m) {
            _applyHistorySnapshotToMessage(m, snapshot, finalizeTools: true);
            m.statusHint = '';
            m.finalDeliveryPhase = FinalDeliveryPhase.idle;
            m.errorDetail = _taskErrorDetail(task, snapshot);
            m.state = task.status == TaskStatus.cancelled
                ? MessageState.done
                : MessageState.failed;
            if (m.content.isEmpty) {
              m.content = task.status == TaskStatus.cancelled
                  ? '已停止'
                  : formatChatTaskError(
                      task.error?.isNotEmpty == true ? task.error! : '任务执行失败',
                    );
              if (m.blocks.isEmpty) {
                _appendTextBlock(m, m.content);
              }
            }
          });
          state = state.copyWith(
            sending: false,
            taskActive: false,
            clearApproval: true,
            clearQuestion: true,
          );
        }
        _cancelEventSub();
        return;
      }
    } catch (_) {}

    _currentTaskId = taskId;
    await _recoverTaskAfterStreamBreak(taskId, agentMsgId, sentAt);
  }

  /// Reconcile after the app returns from background. Android may suspend the
  /// SSE socket without delivering an error, so one resume callback is not
  /// enough to establish the task terminal state.
  Future<void> reconcileAfterResume() async {
    if (_resumeReconciliationInFlight) return;
    _resumeReconciliationInFlight = true;
    // 回到前台：先解除后台标记并重同步发送队列。
    // 后台期间的长连接断开属于预期行为，这里主动重建订阅并清除残留状态，
    // 避免空队列被渲染成「队列状态异常」。
    _appInBackground = false;
    try {
      await _resyncQueueAfterResume();
      for (var attempt = 0; attempt < 3; attempt++) {
        if (!state.messages.any(_messageNeedsCatchUp)) {
          return;
        }
        await resumeActiveTaskIfNeeded();
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
      _resumeReconciliationInFlight = false;
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
      if (_disposed || _currentTaskId != taskId) return;
      unawaited(_reconcileLiveTaskIfTerminal(taskId));
    });
  }

  Future<void> _reconcileLiveTaskIfTerminal(String taskId) async {
    if (_disposed || _currentTaskId != taskId) return;
    final TaskModel task;
    try {
      task = await _repo.getTask(taskId);
    } catch (_) {
      return;
    }
    if (_disposed || _currentTaskId != taskId) return;
    if (_isTaskActive(task.status) ||
        (task.status != TaskStatus.completed &&
            task.status != TaskStatus.failed &&
            task.status != TaskStatus.cancelling &&
            task.status != TaskStatus.cancelled)) {
      return;
    }

    var agentMsgId = _activeTaskAgentMessageIds[taskId];
    if (agentMsgId == null || _messageById(agentMsgId) == null) {
      for (var i = state.messages.length - 1; i >= 0; i--) {
        final message = state.messages[i];
        if (message.role == MessageRole.agent && message.taskId == taskId) {
          agentMsgId = message.id;
          break;
        }
      }
    }
    if (agentMsgId != null) {
      if (task.status == TaskStatus.completed) {
        final snapshot = await _readTaskEventSnapshot(taskId);
        if (_disposed || _currentTaskId != taskId) return;
        var completedMessageId = agentMsgId;
        if (snapshot.appliedInputs.isNotEmpty) {
          completedMessageId = _restoreTaskRoundMessages(
            taskId,
            agentMsgId,
            snapshot,
          );
        }
        _updateMessage(
          completedMessageId,
          (message) => _applyCompletedTaskSnapshot(message, task, snapshot),
        );
        _closeStreamingAgentMessagesForTask(taskId);
      } else {
        final snapshot = await _readTaskEventSnapshot(taskId);
        if (_disposed || _currentTaskId != taskId) return;
        final failed =
            task.status == TaskStatus.failed ||
            task.status == TaskStatus.cancelling;
        _updateMessage(agentMsgId, (message) {
          _applyHistorySnapshotToMessage(
            message,
            snapshot,
            finalizeTools: true,
          );
          message.statusHint = '';
          message.imageGeneration = null;
          message.finalDeliveryPhase = FinalDeliveryPhase.idle;
          message.errorDetail = _taskErrorDetail(task, snapshot);
          message.state = failed ? MessageState.failed : MessageState.done;
          if (message.content.isEmpty) {
            message.content = failed
                ? formatChatTaskError(
                    task.error?.isNotEmpty == true ? task.error! : '任务执行失败',
                  )
                : '已停止';
            if (message.blocks.isEmpty) {
              _appendTextBlock(message, message.content);
            }
          }
        });
        _closeStreamingAgentMessagesForTask(
          taskId,
          endState: failed ? MessageState.failed : MessageState.done,
        );
      }
    }
    if (_currentTaskId != taskId) return;
    _cancelEventSub();
    state = state.copyWith(
      sending: false,
      taskActive: false,
      clearApproval: true,
      clearQuestion: true,
      cancelRequested: false,
    );
  }

  void _cancelEventSub({bool clearPendingCancel = true}) {
    _terminalReconcileTimer?.cancel();
    _terminalReconcileTimer = null;
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
    _cancelEventSub();
    _cancelQueueSub();
    unawaited(_localStore.close());
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
  return [
    type,
    sentAt,
    sessionId,
    field,
    content,
    iteration,
    metadata,
    toolKey,
  ].join('\u{1f}');
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
