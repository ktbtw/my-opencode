import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;
import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/utils/provider_console_url.dart';
import '../../../core/config/feature_flags.dart';
import '../../../core/notifications/app_notification_host.dart';
import '../../../core/notifications/app_notification_controller.dart';
import '../../../core/notifications/app_notification_feedback.dart';
import '../../../core/notifications/app_notification_model.dart';
import '../../../core/services/global_overlay_service.dart';
import '../data/artifact_download_service.dart';
import '../data/attachment_upload.dart';
import '../data/clipboard_attachments.dart';
import '../data/chat_repository.dart';
import '../../../features/devices/presentation/device_provider.dart';
import '../../../features/devices/data/device_compaction_config_model.dart';
import '../../../shared/thinking_variant.dart';
import '../../../shared/widgets/widgets.dart';
import '../../../features/settings/settings_provider.dart';
import '../../../core/services/recent_task_resume.dart';
import '../data/chat_model.dart';
import '../data/chat_target.dart';
import 'attachment_drop_support.dart';
import 'attachment_drop_target.dart';
import 'chat_provider.dart';
import 'artifact_image_viewer_page.dart';
import 'artifact_download_notification.dart';
import 'chat_target_picker.dart';
import 'message_renderer.dart';
import 'widgets/subagent_invocation_card.dart';
import 'widgets/subagent_task_sheet.dart';
import 'widgets/subagent_task_tree.dart';

@visibleForTesting
bool isHistoryOnlyMessagePrepend(
  List<ChatMessage> previous,
  List<ChatMessage> next,
) {
  final prependedCount = next.length - previous.length;
  if (previous.isEmpty || prependedCount <= 0) return false;

  for (var i = 0; i < previous.length; i++) {
    if (next[prependedCount + i].id != previous[i].id) return false;
  }
  return true;
}

@visibleForTesting
bool shouldSuppressChatAutoScrollForHistory(
  ChatState? previous,
  ChatState next, {
  required bool restoringHistoryViewport,
}) {
  if (restoringHistoryViewport ||
      previous?.loadingEarlierHistory == true ||
      next.loadingEarlierHistory) {
    return true;
  }
  return previous != null &&
      isHistoryOnlyMessagePrepend(previous.messages, next.messages);
}

@visibleForTesting
bool shouldAutoScrollChatUpdate(
  ChatState? previous,
  ChatState next, {
  required bool restoringHistoryViewport,
  required bool followingLatest,
  required bool nearBottom,
}) {
  if (next.messages.isEmpty) return false;
  if (previous == null || previous.messages.isEmpty) return true;
  if (shouldSuppressChatAutoScrollForHistory(
    previous,
    next,
    restoringHistoryViewport: restoringHistoryViewport,
  )) {
    return false;
  }
  if (!followingLatest || !nearBottom) return false;

  if (next.messages.length != previous.messages.length) {
    return next.messages.length > previous.messages.length;
  }

  final previousLast = previous.messages.last;
  final nextLast = next.messages.last;
  if (previousLast.id != nextLast.id) return true;
  return !_sameMessageSurface(previousLast, nextLast);
}

@visibleForTesting
bool shouldPinInitialChatViewport({
  required bool positioningInitialViewport,
  required List<ChatMessage> messages,
}) {
  return positioningInitialViewport && messages.isNotEmpty;
}

@visibleForTesting
double preservedHistoryViewportOffset({
  required double currentOffset,
  required double previousMaxScrollExtent,
  required double currentMaxScrollExtent,
  required double minScrollExtent,
  required double maxScrollExtent,
  double previousViewportDimension = 0,
  double currentViewportDimension = 0,
}) {
  // maxScrollExtent also changes when the viewport itself resizes (keyboard,
  // approval/question banners, rotation). Add that delta back so only actual
  // content growth moves the reading anchor.
  final contentDelta =
      currentMaxScrollExtent -
      previousMaxScrollExtent +
      currentViewportDimension -
      previousViewportDimension;
  return (currentOffset + contentDelta)
      .clamp(minScrollExtent, maxScrollExtent)
      .toDouble();
}

bool _sameMessageSurface(ChatMessage a, ChatMessage b) {
  return a.id == b.id &&
      a.state == b.state &&
      a.content == b.content &&
      a.thinkingContent == b.thinkingContent &&
      a.statusHint == b.statusHint &&
      a.finalDeliveryPhase == b.finalDeliveryPhase &&
      a.imageGeneration?.modelName == b.imageGeneration?.modelName &&
      _sameGoalProgressSurface(a.goalProgress, b.goalProgress) &&
      a.artifacts.length == b.artifacts.length &&
      a.files.length == b.files.length;
}

bool _sameGoalProgressSurface(
  List<GoalProgressEntry> left,
  List<GoalProgressEntry> right,
) {
  if (left.length != right.length) return false;
  if (left.isEmpty) return true;
  return left.last.summary == right.last.summary &&
      left.last.type == right.last.type;
}

@visibleForTesting
String projectDirectoryLabel(String projectRoot, String projectId) {
  final segments = projectRoot
      .trim()
      .replaceAll('\\', '/')
      .split('/')
      .where((segment) => segment.isNotEmpty)
      .toList(growable: false);
  if (segments.isNotEmpty) return segments.last;
  final fallback = projectId.trim();
  return fallback.isEmpty ? '工作目录' : fallback;
}

@visibleForTesting
String? latestSubagentTaskId(List<ChatMessage> messages) {
  for (final message in messages.reversed) {
    for (final tool in message.toolCalls.reversed) {
      if (!isSubagentToolCall(tool)) continue;
      final taskId = tool.metadata['task_id']?.toString().trim() ?? '';
      if (taskId.isNotEmpty) return taskId;
    }
  }
  for (final message in messages.reversed) {
    final taskId = message.taskId?.trim() ?? '';
    if (taskId.isNotEmpty) return taskId;
  }
  return null;
}

class _PlanBatchEntry {
  final DateTime startedAt;
  final DateTime updatedAt;
  final PlanInfo plan;

  const _PlanBatchEntry({
    required this.startedAt,
    required this.updatedAt,
    required this.plan,
  });
}

class _SlashCommand {
  final String command;
  final String title;
  final String description;
  final String insertText;
  final IconData icon;

  const _SlashCommand({
    required this.command,
    required this.title,
    required this.description,
    required this.insertText,
    required this.icon,
  });
}

class _SlashCommandTrigger {
  final int start;
  final int end;
  final String query;

  const _SlashCommandTrigger({
    required this.start,
    required this.end,
    required this.query,
  });
}

class _GoalProgressSummary {
  final List<GoalProgressEntry> entries;
  final GoalProgressEntry? latest;
  final bool isStreaming;

  const _GoalProgressSummary({
    this.entries = const [],
    this.latest,
    this.isStreaming = false,
  });

  bool get hasEntries => entries.isNotEmpty;
  bool get isRunning => isStreaming || (latest != null && !latest!.isTerminal);

  int get currentIteration {
    final latestValue = latest;
    if (latestValue == null) return 0;
    if (latestValue.iteration > 0) return latestValue.iteration;
    if (entries.any((entry) => entry.type == 'goal_created')) return 1;
    final continuedCount = entries
        .where((entry) => entry.type == 'goal_continued')
        .length;
    return continuedCount == 0 ? 0 : continuedCount + 1;
  }

  int get maxIterations {
    final latestValue = latest;
    if (latestValue != null && latestValue.max > 0) return latestValue.max;
    for (final entry in entries.reversed) {
      if (entry.max > 0) return entry.max;
    }
    return GoalSettings.defaultMaxIterations;
  }

  String get roundLabel =>
      '${currentIteration.clamp(0, maxIterations)}/$maxIterations';
}

_GoalProgressSummary _collectGoalProgress(List<ChatMessage> messages) {
  final entries = <GoalProgressEntry>[];
  var isStreaming = false;
  for (final message in messages) {
    if (message.role != MessageRole.agent || message.goalProgress.isEmpty) {
      continue;
    }
    entries.addAll(message.goalProgress);
    if (message.state == MessageState.streaming &&
        message.goalProgress.any((entry) => !entry.isTerminal)) {
      isStreaming = true;
    }
  }
  return _GoalProgressSummary(
    entries: List<GoalProgressEntry>.unmodifiable(entries),
    latest: entries.isEmpty ? null : entries.last,
    isStreaming: isStreaming,
  );
}

GoalProgressEntry? latestGoalProgressEntry(List<ChatMessage> messages) =>
    _collectGoalProgress(messages).latest;

const List<_SlashCommand> _slashCommands = [
  _SlashCommand(
    command: '/goal',
    title: '目标模式',
    description: '持续执行目标直到完成',
    insertText: '/goal ',
    icon: Icons.flag_outlined,
  ),
];

class _CommandHighlightTextEditingController extends TextEditingController {
  static final RegExp _commandPattern = RegExp(r'/(goal)\b');
  static const Set<String> _knownCommands = {'/goal'};

  @override
  TextSpan buildTextSpan({
    required BuildContext context,
    TextStyle? style,
    required bool withComposing,
  }) {
    final text = value.text;
    final baseStyle = style ?? const TextStyle();
    if (text.isEmpty) {
      return TextSpan(style: baseStyle, text: text);
    }

    final children = <TextSpan>[];
    var cursor = 0;
    for (final match in _commandPattern.allMatches(text)) {
      final command = match.group(0)!;
      if (!_knownCommands.contains(command)) continue;
      if (text.substring(0, match.start).trim().isNotEmpty) continue;
      if (match.start > cursor) {
        children.add(TextSpan(text: text.substring(cursor, match.start)));
      }
      children.add(
        TextSpan(
          text: command,
          style: baseStyle.copyWith(
            color: AppColors.primary,
            fontWeight: FontWeight.w700,
            backgroundColor: AppColors.primaryLight,
          ),
        ),
      );
      cursor = match.end;
    }

    if (cursor == 0) {
      return TextSpan(style: baseStyle, text: text);
    }
    if (cursor < text.length) {
      children.add(TextSpan(text: text.substring(cursor)));
    }
    return TextSpan(style: baseStyle, children: children);
  }
}

String _planHistoryTimeText(DateTime time) {
  final local = time.toLocal();
  String two(int value) => value.toString().padLeft(2, '0');
  return '${two(local.month)}-${two(local.day)} ${two(local.hour)}:${two(local.minute)}';
}

String _formatGoalRuntime(Duration value) {
  final days = value.inDays;
  final hours = value.inHours.remainder(24);
  final minutes = value.inMinutes.remainder(60);
  final seconds = value.inSeconds.remainder(60);
  if (days > 0) return '$days天 $hours小时';
  if (hours > 0) return '$hours小时 $minutes分钟';
  if (minutes > 0) return '$minutes分钟 $seconds秒';
  return '$seconds秒';
}

Color _goalProgressColor(String? type) {
  switch (type) {
    case 'goal_completed':
      return AppColors.statusSuccess;
    case 'goal_paused':
    case 'goal_checkpoint':
      return AppColors.statusWarning;
    case 'goal_failed':
      return AppColors.statusError;
    default:
      return AppColors.primary;
  }
}

Color _goalProgressBackground(String? type) {
  switch (type) {
    case 'goal_completed':
      return AppColors.statusSuccessLight;
    case 'goal_paused':
    case 'goal_checkpoint':
      return AppColors.statusWarningLight;
    case 'goal_failed':
      return AppColors.statusErrorLight;
    default:
      return AppColors.primaryLight;
  }
}

IconData _goalProgressIcon(String? type, {bool running = false}) {
  if (running) return Icons.play_circle_outline;
  switch (type) {
    case 'goal_completed':
      return Icons.check_circle_outline;
    case 'goal_paused':
      return Icons.pause_circle_outline;
    case 'goal_failed':
      return Icons.error_outline;
    default:
      return Icons.flag_outlined;
  }
}

class ChatPage extends ConsumerStatefulWidget {
  final String agentId;
  final String projectId;
  final String projectRoot;
  final String machineId;
  final String projectScopeId;
  final String sessionId;

  const ChatPage({
    super.key,
    required this.agentId,
    required this.projectId,
    required this.machineId,
    this.projectScopeId = '',
    this.projectRoot = '',
    this.sessionId = '',
  });

  @override
  ConsumerState<ChatPage> createState() => _ChatPageState();
}

class _ChatPageState extends ConsumerState<ChatPage>
    with WidgetsBindingObserver {
  static final RegExp _inlineTextToken = RegExp(r'\[Text \d+字\]');
  final _inputCtrl = _CommandHighlightTextEditingController();
  final _scrollCtrl = ScrollController();
  final _inputFocus = FocusNode();
  int _bottomScrollGeneration = 0;
  int _initialViewportGeneration = 0;
  int _historyViewportGeneration = 0;
  bool _sessionPanelOpen = false;
  bool _subagentPanelOpen = false;
  String? _selectedSubagentTaskId;
  bool _autoLoadingEarlierHistory = false;
  bool _positioningInitialViewport = false;
  bool _followingLatest = true;
  DeviceAgentCompactionConfig _compactionConfig =
      const DeviceAgentCompactionConfig(agentId: '');
  bool _compactionConfigSaving = false;

  (String, String) get _chatKey => (widget.agentId, widget.projectId);
  (String, String) get _sessionListKey => (widget.agentId, widget.projectId);
  String get _modelsKey => widget.machineId.trim();

  ChatTarget get _currentChatTarget => ChatTarget(
    machineId: widget.machineId,
    agentId: widget.agentId,
    projectId: widget.projectId,
    projectScopeId: widget.projectScopeId,
    projectRoot: widget.projectRoot,
    deviceName: widget.machineId,
    agentName: widget.agentId,
    projectName: projectDirectoryLabel(widget.projectRoot, widget.projectId),
  );

  @override
  void initState() {
    super.initState();
    // 通知状态不能在 widget 构建期（含 initState）写入，延后到首帧之后
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      ref
          .read(appNotificationControllerProvider.notifier)
          .setForegroundChat(
            AppNotificationChatContext(
              machineId: widget.machineId,
              agentId: widget.agentId,
              projectId: widget.projectId,
            ),
          );
    });
    WidgetsBinding.instance.addObserver(this);
    // 进入页面时自动加载最近一条会话
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      ref.read(settingsProvider.notifier).loadGlobalPrompt();
      ref.read(selectedVariantProvider(widget.agentId).notifier).init();
      ref.read(projectPromptProvider(widget.agentId).notifier).init();
      ref.read(goalSettingsProvider(_chatKey).notifier).init();
      unawaited(_loadCompactionConfig());
      if (widget.sessionId.trim().isNotEmpty) {
        final generation = ++_initialViewportGeneration;
        _positioningInitialViewport = true;
        _cancelPendingBottomScrolls();
        try {
          await ref
              .read(chatProvider(_chatKey).notifier)
              .loadSession(widget.sessionId);
          if (mounted && generation == _initialViewportGeneration) {
            await _stabilizeToBottom();
          }
        } finally {
          if (mounted && generation == _initialViewportGeneration) {
            _positioningInitialViewport = false;
          }
        }
      } else {
        await _positionLatestMessage();
      }
      // 如果模型列表已加载，立即初始化模型选择
      final models = ref.read(availableModelsProvider(_modelsKey)).valueOrNull;
      if (models != null && models.isNotEmpty) {
        ref.read(selectedModelProvider(widget.agentId).notifier).init(models);
      }
    });
  }

  Future<void> _loadCompactionConfig() async {
    try {
      final config = await ref
          .read(deviceRepositoryProvider)
          .getDeviceAgentCompactionConfig(
            machineId: widget.machineId,
            agentId: widget.agentId,
          );
      if (mounted) setState(() => _compactionConfig = config);
    } catch (_) {}
  }

  Future<void> _showAgentSwitcher() async {
    final target = await showChatTargetPicker(
      context,
      current: _currentChatTarget,
      mobile: AppBreakpoints.isMobile(context),
    );
    if (target == null ||
        !mounted ||
        target.matches(machineId: widget.machineId, agentId: widget.agentId)) {
      return;
    }
    context.pushReplacement(chatTargetRoute(target));
  }

  Future<void> _toggleGlobalOverlay() async {
    if (!GlobalOverlayService.supported) {
      showAppFeedback(context, message: '系统悬浮窗目前仅支持 Android');
      return;
    }
    if (await GlobalOverlayService.isRunning()) {
      if (!mounted || !await confirmGlobalOverlayStop(context)) return;
      await GlobalOverlayService.stop();
      if (mounted) showAppFeedback(context, message: '全局助手已关闭');
      return;
    }
    if (!await GlobalOverlayService.canDrawOverlays()) {
      await GlobalOverlayService.requestPermission();
      if (mounted) {
        showAppFeedback(context, message: '请允许悬浮窗权限，然后再次点击全局助手');
      }
      return;
    }
    final started = await GlobalOverlayService.start();
    if (!started && mounted) {
      showAppFeedback(context, message: '悬浮助手启动失败，请检查系统悬浮窗权限');
    } else if (mounted) {
      showAppFeedback(context, message: '全局助手已开启');
    }
  }

  Future<void> _saveCompactionThreshold(int value) async {
    if (_compactionConfigSaving) return;
    setState(() => _compactionConfigSaving = true);
    try {
      final config = await ref
          .read(deviceRepositoryProvider)
          .saveDeviceAgentCompactionConfig(
            machineId: widget.machineId,
            agentId: widget.agentId,
            thresholdPercent: value,
          );
      if (!mounted) return;
      setState(() {
        _compactionConfig = config;
        _compactionConfigSaving = false;
      });
      await _showCompactionRestartDialog();
    } catch (error) {
      if (mounted) {
        showAppFeedback(context, message: '保存压缩比例失败：$error');
      }
    } finally {
      if (mounted) setState(() => _compactionConfigSaving = false);
    }
  }

  Future<void> _showCompactionRestartDialog() async {
    final restartNow = await showDialog<bool>(
      context: context,
      barrierDismissible: false,
      builder: (dialogContext) => AlertDialog(
        title: const Text('需要重启 Agent'),
        content: const Text('自动压缩比例已更新，需要重启 Agent 后才能生效。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('稍后重启'),
          ),
          FilledButton.icon(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            icon: const Icon(Icons.restart_alt_rounded, size: 18),
            label: const Text('立即重启'),
          ),
        ],
      ),
    );
    if (restartNow == true && mounted) {
      await _restartCurrentAgent();
    }
  }

  Future<void> _restartCurrentAgent() async {
    final operationId = 'agent:restart:${widget.machineId}:${widget.agentId}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在重启 Agent',
      message: '正在等待设备确认重启结果',
      kind: AppNotificationKind.agent,
      scope: AppNotificationScope.synced,
      sourceLabel: widget.machineId,
    );
    try {
      await ref
          .read(deviceRepositoryProvider)
          .restartDeviceAgent(
            machineId: widget.machineId,
            agentId: widget.agentId,
          );
      notifications.succeed(
        operationId,
        title: 'Agent 重启成功',
        message: '自动压缩比例将在 Agent 重启后生效',
      );
      if (mounted) {
        ref.invalidate(deviceDetailProvider(widget.machineId));
      }
    } catch (error) {
      notifications.fail(operationId, title: 'Agent 重启失败', error: error);
    }
  }

  Future<void> _compactContext(ModelInfo? model) async {
    final chat = ref.read(chatProvider(_chatKey));
    if (chat.currentSessionId?.trim().isEmpty ?? true) {
      showAppFeedback(context, message: '当前还没有可压缩的会话');
      return;
    }
    if (chat.taskActive || chat.sending) {
      showAppFeedback(context, message: '当前任务结束后再压缩上下文');
      return;
    }
    final started = await ref
        .read(chatProvider(_chatKey).notifier)
        .compactContext(model: model);
    if (!started && mounted) {
      showAppFeedback(context, message: '压缩上下文任务未启动');
    }
  }

  Future<void> _autoLoadLatestSession() async {
    final chatKey = _chatKey;
    final notifier = ref.read(chatProvider(chatKey).notifier);
    // 如果已有消息（比如 provider 还没 dispose），不重复加载
    if (ref.read(chatProvider(chatKey)).messages.isNotEmpty) return;
    try {
      final sessions = await ref.read(sessionListProvider(chatKey).future);
      if (sessions.isNotEmpty && mounted && _chatKey == chatKey) {
        await notifier.loadSession(sessions.first.sessionId);
      }
    } catch (_) {}
  }

  Future<void> _positionLatestMessage() async {
    final generation = ++_initialViewportGeneration;
    _positioningInitialViewport = true;
    _followingLatest = true;
    _cancelPendingBottomScrolls();
    try {
      await _autoLoadLatestSession();
      if (!mounted || generation != _initialViewportGeneration) return;
      await _stabilizeToBottom();
    } finally {
      if (mounted && generation == _initialViewportGeneration) {
        _positioningInitialViewport = false;
      }
    }
  }

  Future<void> _rememberRecentTask(ChatState? previous, ChatState next) async {
    if (previous == null) return;
    final waitingApproval =
        next.pendingApproval != null && previous.pendingApproval == null;
    final waitingQuestion =
        next.pendingQuestion != null && previous.pendingQuestion == null;
    if (waitingApproval || waitingQuestion) {
      final taskId = waitingApproval
          ? next.pendingApprovalTaskId
          : next.pendingQuestionTaskId;
      if (taskId == null || taskId.trim().isEmpty) return;
      await RecentTaskResumeStore.save(
        RecentTaskResume(
          kind: RecentTaskResumeKind.waitingUser,
          machineId: widget.machineId,
          agentId: widget.agentId,
          projectId: widget.projectId,
          projectRoot: widget.projectRoot,
          projectScopeId: widget.projectScopeId,
          sessionId: next.currentSessionId ?? '',
          taskId: taskId,
          updatedAt: DateTime.now().toUtc(),
        ),
      );
      return;
    }

    final taskStopped =
        (previous.taskActive || previous.sending) &&
        !next.taskActive &&
        !next.sending;
    if (!taskStopped) return;
    final message = next.messages.reversed
        .where((item) => item.role == MessageRole.agent)
        .firstOrNull;
    if (message == null ||
        message.state != MessageState.done ||
        message.taskId == null ||
        message.taskId!.trim().isEmpty ||
        message.content.trim().isEmpty ||
        message.content.trim() == '已停止') {
      await RecentTaskResumeStore.clearIfTask(message?.taskId ?? '');
      return;
    }
    await RecentTaskResumeStore.save(
      RecentTaskResume(
        kind: RecentTaskResumeKind.completed,
        machineId: widget.machineId,
        agentId: widget.agentId,
        projectId: widget.projectId,
        projectRoot: widget.projectRoot,
        projectScopeId: widget.projectScopeId,
        sessionId: next.currentSessionId ?? '',
        taskId: message.taskId!,
        updatedAt: DateTime.now().toUtc(),
      ),
    );
  }

  @override
  void dispose() {
    // dispose 同样不允许同步写 provider，捕获引用后异步清理
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    final chatContext = AppNotificationChatContext(
      machineId: widget.machineId,
      agentId: widget.agentId,
      projectId: widget.projectId,
    );
    unawaited(Future(() => notifications.clearForegroundChat(chatContext)));
    WidgetsBinding.instance.removeObserver(this);
    _initialViewportGeneration++;
    _cancelPendingBottomScrolls();
    _inputCtrl.dispose();
    _scrollCtrl.dispose();
    _inputFocus.dispose();
    super.dispose();
  }

  @override
  void didUpdateWidget(covariant ChatPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.machineId == widget.machineId &&
        oldWidget.agentId == widget.agentId &&
        oldWidget.projectId == widget.projectId) {
      return;
    }
    // didUpdateWidget 处于构建期，写入通知 provider 需要延后一帧
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      unawaited(_positionLatestMessage());
      unawaited(_loadCompactionConfig());
      final notifications = ref.read(
        appNotificationControllerProvider.notifier,
      );
      notifications.clearForegroundChat(
        AppNotificationChatContext(
          machineId: oldWidget.machineId,
          agentId: oldWidget.agentId,
          projectId: oldWidget.projectId,
        ),
      );
      notifications.setForegroundChat(
        AppNotificationChatContext(
          machineId: widget.machineId,
          agentId: widget.agentId,
          projectId: widget.projectId,
        ),
      );
    });
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    final notifier = ref.read(chatProvider(_chatKey).notifier);
    if (state == AppLifecycleState.resumed) {
      unawaited(notifier.reconcileAfterResume());
      return;
    }
    // 进入后台：长连接会被系统挂起，期间的断开按预期行为处理，
    // 不产生用户可见的队列错误提示。
    if (state == AppLifecycleState.paused ||
        state == AppLifecycleState.inactive ||
        state == AppLifecycleState.hidden) {
      notifier.markAppBackgrounded();
    }
  }

  void _openProjectFiles() {
    final query = Uri(
      queryParameters: {
        'machineId': widget.machineId,
        'agentId': widget.agentId,
        'projectId': widget.projectId,
      },
    ).query;
    context.push('/chat/files?$query');
  }

  void _scrollToBottom({bool animated = true}) {
    _followingLatest = true;
    _cancelPendingBottomScrolls();
    final generation = _bottomScrollGeneration;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (generation == _bottomScrollGeneration && _scrollCtrl.hasClients) {
        final offset = _scrollCtrl.position.maxScrollExtent;
        if (animated) {
          _scrollCtrl.animateTo(
            offset,
            duration: const Duration(milliseconds: 250),
            curve: Curves.easeOut,
          );
          return;
        }
        _scrollCtrl.jumpTo(offset);
      }
    });
  }

  void _cancelPendingBottomScrolls() {
    _bottomScrollGeneration++;
    _historyViewportGeneration++;
  }

  Future<void> _stabilizeToBottom() async {
    _followingLatest = true;
    _cancelPendingBottomScrolls();
    final generation = _bottomScrollGeneration;
    var previousCheckpoint = 0;
    const checkpoints = [0, 16, 48, 120, 260, 520, 900];
    for (final checkpoint in checkpoints) {
      final waitMs = checkpoint - previousCheckpoint;
      previousCheckpoint = checkpoint;
      if (waitMs > 0) {
        await Future<void>.delayed(Duration(milliseconds: waitMs));
      }
      await WidgetsBinding.instance.endOfFrame;
      if (!mounted ||
          generation != _bottomScrollGeneration ||
          !_scrollCtrl.hasClients) {
        return;
      }
      _scrollCtrl.jumpTo(_scrollCtrl.position.maxScrollExtent);
    }
  }

  bool _isNearBottom({double threshold = 96}) {
    if (!_scrollCtrl.hasClients) return true;
    return _scrollCtrl.position.maxScrollExtent - _scrollCtrl.offset <=
        threshold;
  }

  bool _isNearTop({double threshold = 96}) {
    if (!_scrollCtrl.hasClients) return false;
    return _scrollCtrl.offset <= threshold;
  }

  void _handleUserScroll(UserScrollNotification notification) {
    // Only a real user gesture can change follow mode or trigger pagination.
    // Programmatic jumps and layout changes must not reuse a stale direction.
    if (_positioningInitialViewport) {
      _initialViewportGeneration++;
      _positioningInitialViewport = false;
      _cancelPendingBottomScrolls();
    }
    if (!_scrollCtrl.hasClients) return;
    final direction = notification.direction;
    if (_autoLoadingEarlierHistory && direction != ScrollDirection.idle) {
      // A new gesture means the user has taken control of the viewport; an
      // older compensation task must not jump over that gesture later.
      _cancelPendingBottomScrolls();
    }
    if (direction != ScrollDirection.idle) {
      _followingLatest = _isNearBottom();
    }
    if (direction != ScrollDirection.forward) {
      return;
    }
    if (!_isNearTop()) return;
    final chat = ref.read(chatProvider(_chatKey));
    if (!chat.hasEarlierHistory || chat.loadingEarlierHistory) return;
    unawaited(_loadEarlierHistoryPreservingOffset());
  }

  Future<void> _loadEarlierHistoryPreservingOffset() async {
    if (_positioningInitialViewport ||
        _autoLoadingEarlierHistory ||
        !_scrollCtrl.hasClients) {
      return;
    }
    _followingLatest = false;
    _cancelPendingBottomScrolls();
    _autoLoadingEarlierHistory = true;
    final viewportGeneration = _historyViewportGeneration;
    final beforeExtent = _scrollCtrl.position.maxScrollExtent;
    final beforeViewport = _scrollCtrl.position.viewportDimension;
    final beforeOffset = _scrollCtrl.offset;
    try {
      final added = await ref
          .read(chatProvider(_chatKey).notifier)
          .loadEarlierSessionHistory();
      if (!mounted || added == 0) return;
      await _stabilizeEarlierHistoryViewport(
        beforeExtent: beforeExtent,
        beforeViewport: beforeViewport,
        beforeOffset: beforeOffset,
        generation: viewportGeneration,
      );
    } finally {
      _autoLoadingEarlierHistory = false;
    }
  }

  Future<void> _stabilizeEarlierHistoryViewport({
    required double beforeExtent,
    required double beforeViewport,
    required double beforeOffset,
    required int generation,
  }) async {
    var knownExtent = beforeExtent;
    var knownViewport = beforeViewport;
    var expectedOffset = beforeOffset;
    var previousCheckpoint = 0;
    const checkpoints = [0, 16, 48, 120, 260, 520];

    for (var index = 0; index < checkpoints.length; index++) {
      final checkpoint = checkpoints[index];
      final waitMs = checkpoint - previousCheckpoint;
      previousCheckpoint = checkpoint;
      if (waitMs > 0) {
        await Future<void>.delayed(Duration(milliseconds: waitMs));
      }
      await WidgetsBinding.instance.endOfFrame;
      if (!mounted ||
          generation != _historyViewportGeneration ||
          !_scrollCtrl.hasClients) {
        return;
      }

      final position = _scrollCtrl.position;
      final nextOffset = preservedHistoryViewportOffset(
        currentOffset: expectedOffset,
        previousMaxScrollExtent: knownExtent,
        currentMaxScrollExtent: position.maxScrollExtent,
        minScrollExtent: position.minScrollExtent,
        maxScrollExtent: position.maxScrollExtent,
        previousViewportDimension: knownViewport,
        currentViewportDimension: position.viewportDimension,
      );
      final actualOffset = position.pixels;
      final followsPreviousAnchor = (actualOffset - expectedOffset).abs() < 2;
      final alreadyAdjusted = (actualOffset - nextOffset).abs() < 2;
      if (index > 0 && !followsPreviousAnchor && !alreadyAdjusted) {
        return;
      }

      knownExtent = position.maxScrollExtent;
      knownViewport = position.viewportDimension;
      expectedOffset = nextOffset;
      if (!alreadyAdjusted) position.jumpTo(nextOffset);
    }
  }

  Future<void> _syncGoalTerminalStatus(GoalProgressEntry? latest) async {
    if (latest == null || !latest.isTerminal) return;
    final notifier = ref.read(goalSettingsProvider(_chatKey).notifier);
    final settings = ref.read(goalSettingsProvider(_chatKey));
    final targetStatus = switch (latest.type) {
      'goal_completed' => GoalSettings.statusCompleted,
      'goal_paused' => GoalSettings.statusPaused,
      'goal_failed' => GoalSettings.statusFailed,
      _ => null,
    };
    if (targetStatus == null) return;
    if (!settings.enabled && settings.status == targetStatus) return;
    try {
      switch (targetStatus) {
        case GoalSettings.statusCompleted:
          await notifier.markCompleted();
          break;
        case GoalSettings.statusPaused:
          await notifier.markPaused();
          break;
        case GoalSettings.statusFailed:
          await notifier.markFailed();
          break;
      }
    } catch (_) {}
  }

  bool _shouldAutoScroll(ChatState? previous, ChatState next) {
    return shouldAutoScrollChatUpdate(
      previous,
      next,
      restoringHistoryViewport: _autoLoadingEarlierHistory,
      followingLatest: _followingLatest,
      nearBottom: _isNearBottom(),
    );
  }

  void _refreshDeviceStatus() {
    if (widget.machineId.isEmpty) return;
    ref.invalidate(deviceListProvider);
    ref.invalidate(deviceDetailProvider(widget.machineId));
  }

  void _leaveChat() {
    _refreshDeviceStatus();
    if (context.canPop()) {
      context.pop();
      return;
    }
    context.go('/devices/${widget.machineId}');
  }

  Future<void> _send() async {
    final text = _sanitizeInput(_inputCtrl.text).trim();
    if (text.isEmpty) return;
    final commandGoal = _goalFromInput(text);
    if (commandGoal != null) {
      try {
        final goalNotifier = ref.read(goalSettingsProvider(_chatKey).notifier);
        await goalNotifier.saveContent(commandGoal);
        await goalNotifier.setEnabled(true);
      } catch (e) {
        if (!mounted) return;
        showAppFeedback(
          context,
          title: '保存目标失败',
          message: e.toString(),
          error: true,
        );
        return;
      }
    }
    final goalSettings = ref.read(goalSettingsProvider(_chatKey));
    final latestGoal = latestGoalProgressEntry(
      ref.read(chatProvider(_chatKey)).messages,
    );
    if (commandGoal == null && latestGoal?.isTerminal == true) {
      unawaited(_syncGoalTerminalStatus(latestGoal));
    }
    final activeGoal =
        commandGoal == null &&
            goalSettings.enabled &&
            latestGoal?.isTerminal != true &&
            goalSettings.content.trim().isNotEmpty
        ? goalSettings.content.trim()
        : null;
    _inputCtrl.clear();
    final model = ref.read(selectedModelProvider(widget.agentId));
    final pMode = ref.read(permissionModeProvider);
    final variant = _selectedVariantFor(model);
    final accepted = await ref
        .read(chatProvider(_chatKey).notifier)
        .sendMessage(
          text,
          model: model,
          permissionMode: pMode,
          variant: variant,
          goalOverride: activeGoal,
          goalMaxIterations: goalSettings.maxIterations,
        );
    if (!accepted && mounted && _inputCtrl.text.trim().isEmpty) {
      _inputCtrl.text = text;
      _inputCtrl.selection = TextSelection.collapsed(offset: text.length);
    }
    ref.invalidate(sessionListProvider(_sessionListKey));
    _scrollToBottom();
  }

  String _sanitizeInput(String value) {
    return value
        .replaceAll(_inlineTextToken, '')
        .replaceAll(RegExp(r' {2,}'), ' ');
  }

  String? _goalFromInput(String text) {
    final trimmed = text.trim();
    if (!trimmed.startsWith('/goal ')) return null;
    final goal = trimmed.substring('/goal '.length).trim();
    return goal.isEmpty ? null : goal;
  }

  Future<void> _resend(ChatMessage message) async {
    if (message.role != MessageRole.user) return;
    final model = ref.read(selectedModelProvider(widget.agentId));
    final pMode = ref.read(permissionModeProvider);
    final variant = _selectedVariantFor(model);
    await ref
        .read(chatProvider(_chatKey).notifier)
        .sendMessage(
          message.content,
          model: model,
          permissionMode: pMode,
          variant: variant,
          attachedFilesOverride: message.attachedFiles,
        );
    ref.invalidate(sessionListProvider(_sessionListKey));
    _scrollToBottom();
  }

  Future<bool> _startGoalTask(String goal, int maxIterations) async {
    final normalized = goal.trim();
    if (normalized.isEmpty) return false;
    if (ref.read(chatProvider(_chatKey)).sending) {
      if (mounted) {
        showAppFeedback(context, message: '当前已有任务运行中，目标已保存');
      }
      return false;
    }
    final model = ref.read(selectedModelProvider(widget.agentId));
    final pMode = ref.read(permissionModeProvider);
    final variant = _selectedVariantFor(model);
    await ref
        .read(chatProvider(_chatKey).notifier)
        .sendMessage(
          '/goal $normalized',
          model: model,
          permissionMode: pMode,
          variant: variant,
          goalMaxIterations: maxIterations,
        );
    ref.invalidate(sessionListProvider(_sessionListKey));
    _scrollToBottom();
    return true;
  }

  String? _selectedVariantFor(ModelInfo? model, {String? selected}) {
    return normalizeThinkingVariant(
      model?.variants ?? const <String>[],
      selected ?? ref.read(selectedVariantProvider(widget.agentId)),
    );
  }

  void _reconcileSelectedVariant(ModelInfo? model, {String? selected}) {
    if (model == null) return;
    final current =
        selected ?? ref.read(selectedVariantProvider(widget.agentId));
    final normalized = _selectedVariantFor(model, selected: current);
    if (current == normalized) return;
    unawaited(
      ref
          .read(selectedVariantProvider(widget.agentId).notifier)
          .select(normalized),
    );
  }

  void _warnModelContextSwitch(
    BuildContext context,
    ContextUsageInfo? usage,
    ModelInfo? model,
  ) {
    final limit = model?.contextLimit;
    final contextTokens = usage?.knownContextTokens ?? 0;
    if (limit == null || limit <= 0 || contextTokens <= 0) return;
    final effectiveThreshold = usage?.compactionThresholdTokens ?? 0;
    String? message;
    if (contextTokens >= limit) {
      message = '当前上下文已超过 ${model!.modelID} 的完整额度，发送前建议先压缩上下文';
    } else if (effectiveThreshold > 0 && contextTokens >= effectiveThreshold) {
      message = '切换后上下文将超过真实压缩阈值，下一次发送会触发压缩';
    }
    if (message == null) return;
    showAppFeedback(context, message: message);
  }

  void _showGoalSettings() {
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (_) => _GoalSettingsSheet(
        agentId: widget.agentId,
        projectId: widget.projectId,
        onStartGoal: _startGoalTask,
      ),
    );
  }

  Future<void> _editProjectPrompt() async {
    final notifier = ref.read(projectPromptProvider(widget.agentId).notifier);
    final controller = TextEditingController(
      text: ref.read(projectPromptProvider(widget.agentId)),
    );

    try {
      await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        backgroundColor: AppColors.surface,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
        ),
        builder: (sheetContext) {
          return Padding(
            padding: EdgeInsets.only(
              left: 16,
              right: 16,
              top: 16,
              bottom: MediaQuery.of(sheetContext).viewInsets.bottom + 16,
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  '项目提示词',
                  style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
                ),
                const SizedBox(height: 6),
                const Text(
                  '仅对当前聊天所属项目生效，适合填写项目背景、约束和协作约定',
                  style: TextStyle(
                    fontSize: 12,
                    color: AppColors.textSecondary,
                  ),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: controller,
                  minLines: 6,
                  maxLines: 10,
                  autofocus: true,
                  decoration: InputDecoration(
                    hintText: '输入项目提示词',
                    border: OutlineInputBorder(
                      borderRadius: AppRadius.mdRadius,
                      borderSide: const BorderSide(color: AppColors.border),
                    ),
                    enabledBorder: OutlineInputBorder(
                      borderRadius: AppRadius.mdRadius,
                      borderSide: const BorderSide(color: AppColors.border),
                    ),
                    focusedBorder: OutlineInputBorder(
                      borderRadius: AppRadius.mdRadius,
                      borderSide: const BorderSide(
                        color: AppColors.primary,
                        width: 1.5,
                      ),
                    ),
                    filled: true,
                    fillColor: AppColors.inputBackground,
                  ),
                ),
                const SizedBox(height: 12),
                Row(
                  children: [
                    TextButton(
                      onPressed: () async {
                        try {
                          await notifier.save('');
                          if (!sheetContext.mounted) return;
                          Navigator.of(sheetContext).pop();
                        } catch (e) {
                          if (!mounted) return;
                          showAppFeedback(
                            context,
                            title: '清空项目提示词失败',
                            message: e.toString(),
                            error: true,
                          );
                        }
                      },
                      child: const Text('清空'),
                    ),
                    const Spacer(),
                    TextButton(
                      onPressed: () => Navigator.of(sheetContext).pop(),
                      child: const Text('取消'),
                    ),
                    const SizedBox(width: 8),
                    FilledButton(
                      onPressed: () async {
                        try {
                          await notifier.save(controller.text);
                          if (!sheetContext.mounted) return;
                          Navigator.of(sheetContext).pop();
                        } catch (e) {
                          if (!mounted) return;
                          showAppFeedback(
                            context,
                            title: '保存项目提示词失败',
                            message: e.toString(),
                            error: true,
                          );
                        }
                      },
                      child: const Text('保存'),
                    ),
                  ],
                ),
              ],
            ),
          );
        },
      );
    } finally {
      controller.dispose();
    }
  }

  void _openProjectMemory() {
    final scopeId = widget.projectScopeId.trim();
    if (scopeId.isEmpty || widget.machineId.trim().isEmpty) {
      showAppFeedback(context, message: '当前 Agent 尚未上报项目记忆标识');
      return;
    }
    context.push(
      '/devices/${Uri.encodeComponent(widget.machineId)}/projects/${Uri.encodeComponent(scopeId)}/memory',
    );
  }

  Future<void> _pickFiles() async {
    final result = await FilePicker.platform.pickFiles(
      allowMultiple: true,
      withData: true,
    );
    if (result == null) return;
    final notifier = ref.read(chatProvider(_chatKey).notifier);
    for (final file in result.files) {
      if (file.bytes == null) continue;
      notifier.addAttachedFile(
        buildAttachedFile(filename: file.name, bytes: file.bytes!),
      );
    }
  }

  Future<void> _handleDroppedFiles(List<AttachmentDropFile> files) async {
    final notifier = ref.read(chatProvider(_chatKey).notifier);
    var added = 0;
    for (final file in files) {
      notifier.addAttachedFile(
        buildAttachedFile(
          filename: file.filename,
          bytes: file.bytes,
          mimeType: file.mimeType,
        ),
      );
      added += 1;
    }
    if (!mounted || added == 0) return;
    _inputFocus.requestFocus();
    showAppFeedback(context, message: '已添加 $added 个附件');
  }

  @override
  Widget build(BuildContext context) {
    final chat = ref.watch(chatProvider(_chatKey));
    final selectedModel = ref.watch(selectedModelProvider(widget.agentId));
    final availableModels =
        ref.watch(availableModelsProvider(_modelsKey)).valueOrNull ?? const [];
    final isMobile = AppBreakpoints.isMobile(context);
    final isDesktop = AppBreakpoints.isDesktop(context);
    final subagentTaskId = latestSubagentTaskId(chat.messages);
    final currentPlanMode = _resolveCurrentPlanMode(chat.messages);
    final planHistory = _collectPlanHistory(chat.messages);
    final goalSettings = ref.watch(goalSettingsProvider(_chatKey));
    final goalProgress = _collectGoalProgress(chat.messages);
    if (goalProgress.latest?.isTerminal == true) {
      unawaited(_syncGoalTerminalStatus(goalProgress.latest));
    }

    // 模型列表加载完后恢复持久化的选择（按设备级别）
    ref.listen(availableModelsProvider(_modelsKey), (_, next) {
      final models = next.valueOrNull;
      if (models != null && models.isNotEmpty) {
        ref.read(selectedModelProvider(widget.agentId).notifier).init(models);
      }
    });

    ref.listen(selectedModelProvider(widget.agentId), (_, next) {
      _reconcileSelectedVariant(next);
    });
    ref.listen(selectedVariantProvider(widget.agentId), (_, next) {
      _reconcileSelectedVariant(
        ref.read(selectedModelProvider(widget.agentId)),
        selected: next,
      );
    });

    // 仅在最后一条消息追加/更新时滚动，避免旧任务迟到状态抢焦点
    ref.listen(chatProvider(_chatKey), (previous, next) {
      unawaited(_rememberRecentTask(previous, next));
      if (shouldPinInitialChatViewport(
        positioningInitialViewport: _positioningInitialViewport,
        messages: next.messages,
      )) {
        _scrollToBottom(animated: false);
      } else if (_shouldAutoScroll(previous, next)) {
        _scrollToBottom();
      }
      final previousGoal = previous == null
          ? const _GoalProgressSummary()
          : _collectGoalProgress(previous.messages);
      final nextGoal = _collectGoalProgress(next.messages);
      if (nextGoal.latest == null ||
          nextGoal.latest?.type == previousGoal.latest?.type) {
        return;
      }
      unawaited(_syncGoalTerminalStatus(nextGoal.latest));
    });

    return PopScope(
      onPopInvokedWithResult: (didPop, _) {
        if (didPop) _refreshDeviceStatus();
      },
      child: Scaffold(
        body: SafeArea(
          child: AttachmentDropTarget(
            enabled: isAttachmentDropSupported(isMobile: isMobile),
            onDrop: _handleDroppedFiles,
            child: PageBackground(
              child: Column(
                children: [
                  _buildContextBar(context, chat),
                  Expanded(
                    child: Row(
                      children: [
                        // 左侧历史会话栏（桌面常驻，移动抽屉）
                        if (!isMobile && _sessionPanelOpen)
                          _SessionSidebar(
                            agentId: widget.agentId,
                            projectId: widget.projectId,
                            currentSessionId: chat.currentSessionId,
                            onSelect: (sid) async {
                              _clearSelectedSubagentTask();
                              final generation = ++_initialViewportGeneration;
                              _positioningInitialViewport = true;
                              _cancelPendingBottomScrolls();
                              try {
                                await ref
                                    .read(chatProvider(_chatKey).notifier)
                                    .loadSession(sid);
                                if (mounted &&
                                    generation == _initialViewportGeneration) {
                                  await _stabilizeToBottom();
                                }
                              } finally {
                                if (mounted &&
                                    generation == _initialViewportGeneration) {
                                  _positioningInitialViewport = false;
                                }
                              }
                            },
                            onNew: () {
                              _clearSelectedSubagentTask();
                              _initialViewportGeneration++;
                              _positioningInitialViewport = false;
                              _cancelPendingBottomScrolls();
                              ref
                                  .read(chatProvider(_chatKey).notifier)
                                  .newSession();
                              ref.invalidate(
                                sessionListProvider(_sessionListKey),
                              );
                            },
                          ),
                        if (!isMobile && _sessionPanelOpen)
                          Container(width: 1, color: AppColors.border),
                        // 中央消息区 + 输入区
                        Expanded(
                          child: Column(
                            children: [
                              Expanded(
                                child: _MessageArea(
                                  messages: chat.messages,
                                  scrollController: _scrollCtrl,
                                  loadingEarlierHistory:
                                      chat.loadingEarlierHistory,
                                  hasEarlierHistory: chat.hasEarlierHistory,
                                  onResend: _resend,
                                  onUserScroll: _handleUserScroll,
                                ),
                              ),
                              if (chat.pendingApproval != null)
                                _ApprovalBanner(
                                  approval: chat.pendingApproval!,
                                  onApprove: (reply) {
                                    ref
                                        .read(chatProvider(_chatKey).notifier)
                                        .submitApproval(reply);
                                  },
                                ),
                              if (chat.pendingQuestion != null)
                                QuestionBanner(
                                  question: chat.pendingQuestion!,
                                  onSubmit: (requestId, answers) {
                                    ref
                                        .read(chatProvider(_chatKey).notifier)
                                        .submitQuestion(requestId, answers);
                                  },
                                  onReject: (requestId) {
                                    ref
                                        .read(chatProvider(_chatKey).notifier)
                                        .submitQuestion(
                                          requestId,
                                          const [],
                                          rejected: true,
                                        );
                                  },
                                ),
                              _InputArea(
                                controller: _inputCtrl,
                                focusNode: _inputFocus,
                                sending:
                                    chat.sending ||
                                    chat.messages.any(
                                      (m) => m.state == MessageState.streaming,
                                    ),
                                taskActive: chat.taskActive,
                                sessionId: chat.currentSessionId,
                                queueItems: chat.queue.queuedItems,
                                queueLoading: chat.queueLoading,
                                queueError: chat.queueError,
                                attachedFiles: chat.attachedFiles,
                                agentId: widget.agentId,
                                projectId: widget.projectId,
                                projectRoot: widget.projectRoot,
                                projectPrompt: ref.watch(
                                  projectPromptProvider(widget.agentId),
                                ),
                                selectedModel: selectedModel,
                                availableModels: availableModels,
                                currentPlanMode: currentPlanMode,
                                planHistory: planHistory,
                                goalSettings: goalSettings,
                                goalProgress: goalProgress,
                                contextUsage: chat.contextUsage,
                                configuredCompactionThreshold:
                                    _compactionConfig.effectiveThresholdPercent,
                                compactionConfigSaving: _compactionConfigSaving,
                                machineId: widget.machineId,
                                onCompactContext: () =>
                                    _compactContext(selectedModel),
                                onSaveCompactionThreshold:
                                    _saveCompactionThreshold,
                                onSend: _send,
                                onStop: () => ref
                                    .read(chatProvider(_chatKey).notifier)
                                    .cancelCurrentTask(),
                                onDeleteQueueItem: (item) => ref
                                    .read(chatProvider(_chatKey).notifier)
                                    .deleteQueueItem(item),
                                onInsertQueueItem: (item) => ref
                                    .read(chatProvider(_chatKey).notifier)
                                    .insertQueueItem(item),
                                onSendQueueItem: (item) => ref
                                    .read(chatProvider(_chatKey).notifier)
                                    .sendQueueItem(item),
                                onReorderQueueItems: (items) => ref
                                    .read(chatProvider(_chatKey).notifier)
                                    .reorderQueueItems(items),
                                onUpdateQueueItemModel:
                                    (item, model, variant) => ref
                                        .read(chatProvider(_chatKey).notifier)
                                        .updateQueueItemModel(
                                          item,
                                          model: model,
                                          variant: variant,
                                        ),
                                onPickFiles: _pickFiles,
                                onRemoveFile: (i) {
                                  ref
                                      .read(chatProvider(_chatKey).notifier)
                                      .removeAttachedFile(i);
                                },
                                onSelectModel: (m) {
                                  _warnModelContextSwitch(
                                    context,
                                    chat.contextUsage,
                                    m,
                                  );
                                  final current = ref.read(
                                    selectedVariantProvider(widget.agentId),
                                  );
                                  final normalized = normalizeThinkingVariant(
                                    m?.variants ?? const <String>[],
                                    current,
                                  );
                                  if (current != normalized) {
                                    unawaited(
                                      ref
                                          .read(
                                            selectedVariantProvider(
                                              widget.agentId,
                                            ).notifier,
                                          )
                                          .select(normalized),
                                    );
                                  }
                                  ref
                                      .read(
                                        selectedModelProvider(
                                          widget.agentId,
                                        ).notifier,
                                      )
                                      .select(m);
                                },
                                onAddFile: (f) {
                                  ref
                                      .read(chatProvider(_chatKey).notifier)
                                      .addAttachedFile(f);
                                },
                                onEditProjectPrompt: _editProjectPrompt,
                                onOpenProjectMemory: _openProjectMemory,
                                onOpenGoalSettings: _showGoalSettings,
                              ),
                            ],
                          ),
                        ),
                        if (isDesktop &&
                            _subagentPanelOpen &&
                            subagentTaskId != null)
                          Container(width: 1, color: AppColors.border),
                        if (isDesktop &&
                            _subagentPanelOpen &&
                            subagentTaskId != null)
                          _SubagentSidebar(
                            taskId: subagentTaskId,
                            availableModels: availableModels,
                            onClose: () =>
                                setState(() => _subagentPanelOpen = false),
                          ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
        // 移动端抽屉
        drawer: isMobile
            ? Drawer(
                child: _SessionSidebar(
                  agentId: widget.agentId,
                  projectId: widget.projectId,
                  currentSessionId: chat.currentSessionId,
                  onSelect: (sid) async {
                    _clearSelectedSubagentTask();
                    final generation = ++_initialViewportGeneration;
                    _positioningInitialViewport = true;
                    _cancelPendingBottomScrolls();
                    try {
                      await ref
                          .read(chatProvider(_chatKey).notifier)
                          .loadSession(sid);
                      if (mounted && generation == _initialViewportGeneration) {
                        await _stabilizeToBottom();
                      }
                    } finally {
                      if (mounted && generation == _initialViewportGeneration) {
                        _positioningInitialViewport = false;
                      }
                    }
                    if (!context.mounted) return;
                    Navigator.of(context).pop();
                  },
                  onNew: () {
                    _clearSelectedSubagentTask();
                    _initialViewportGeneration++;
                    _positioningInitialViewport = false;
                    _cancelPendingBottomScrolls();
                    ref.read(chatProvider(_chatKey).notifier).newSession();
                    ref.invalidate(sessionListProvider(_sessionListKey));
                    Navigator.of(context).pop();
                  },
                ),
              )
            : null,
      ),
    );
  }

  String _resolveCurrentPlanMode(List<ChatMessage> messages) {
    for (int i = messages.length - 1; i >= 0; i--) {
      final mode = messages[i].plan?.mode.trim().toLowerCase();
      if (mode == 'plan' || mode == 'build') return mode!;
    }
    return 'build';
  }

  List<_PlanBatchEntry> _collectPlanHistory(List<ChatMessage> messages) {
    final allSnapshots = <PlanHistorySnapshot>[];
    for (final message in messages) {
      if (message.role != MessageRole.agent || message.plan == null) continue;
      if (message.planHistory.isNotEmpty) {
        allSnapshots.addAll(message.planHistory);
      } else {
        allSnapshots.add(
          PlanHistorySnapshot(
            capturedAt: message.createdAt,
            plan: message.plan!,
          ),
        );
      }
    }
    final snapshots = mergePlanHistorySnapshotsByBatch(const [], allSnapshots);
    final grouped = <String, PlanHistorySnapshot>{};
    final started = <String, DateTime>{};
    final updated = <String, DateTime>{};
    final latest = <String, PlanInfo>{};

    for (final snapshot in snapshots) {
      final batchKey = snapshot.batchKey;
      if (batchKey.isEmpty || snapshot.plan.isEmpty) continue;
      grouped[batchKey] = snapshot;
      final first = started[batchKey];
      if (first == null || snapshot.capturedAt.isBefore(first)) {
        started[batchKey] = snapshot.capturedAt;
      }
      final last = updated[batchKey];
      if (last == null || snapshot.capturedAt.isAfter(last)) {
        updated[batchKey] = snapshot.capturedAt;
        latest[batchKey] = snapshot.plan;
      }
    }

    final entries =
        grouped.entries
            .map(
              (entry) => _PlanBatchEntry(
                startedAt: started[entry.key]!,
                updatedAt: updated[entry.key]!,
                plan: latest[entry.key]!,
              ),
            )
            .toList()
          ..sort((a, b) => b.updatedAt.compareTo(a.updatedAt));
    return entries;
  }

  String? _taskCenterTaskId(ChatState chat) {
    final selected = _selectedSubagentTaskId?.trim() ?? '';
    return selected.isNotEmpty ? selected : latestSubagentTaskId(chat.messages);
  }

  void _clearSelectedSubagentTask() {
    _selectedSubagentTaskId = null;
    _subagentPanelOpen = false;
  }

  Widget _buildContextBar(BuildContext context, ChatState chat) {
    final isMobile = AppBreakpoints.isMobile(context);
    final isDesktop = AppBreakpoints.isDesktop(context);
    final taskId = _taskCenterTaskId(chat);
    final availableModels =
        ref.read(availableModelsProvider(_modelsKey)).valueOrNull ?? const [];
    return Builder(
      builder: (scaffoldContext) => Container(
        height: 52,
        decoration: const BoxDecoration(
          color: AppColors.surface,
          border: Border(bottom: BorderSide(color: AppColors.border)),
        ),
        padding: const EdgeInsets.symmetric(horizontal: 12),
        child: Row(
          children: [
            IconButton(
              icon: const Icon(Icons.arrow_back_ios_new, size: 15),
              onPressed: _leaveChat,
              tooltip: '返回设备详情',
            ),
            IconButton(
              icon: Icon(
                Icons.history,
                size: 18,
                color: _sessionPanelOpen
                    ? AppColors.primary
                    : AppColors.textSecondary,
              ),
              onPressed: isMobile
                  ? () => Scaffold.of(scaffoldContext).openDrawer()
                  : () =>
                        setState(() => _sessionPanelOpen = !_sessionPanelOpen),
              tooltip: '历史会话',
            ),
            if (!isMobile) ...[
              Container(width: 1, height: 20, color: AppColors.border),
              const SizedBox(width: 10),
              Expanded(
                child: SingleChildScrollView(
                  scrollDirection: Axis.horizontal,
                  child: Row(
                    children: [
                      _ContextChip(
                        icon: Icons.computer_rounded,
                        label: widget.machineId,
                      ),
                      const SizedBox(width: 6),
                      _ContextChip(
                        icon: Icons.smart_toy_outlined,
                        label: widget.agentId,
                      ),
                      const SizedBox(width: 6),
                      _ContextChip(
                        icon: Icons.folder_outlined,
                        label: widget.projectId,
                      ),
                      if (chat.currentSessionId != null) ...[
                        const SizedBox(width: 6),
                        _ContextChip(
                          icon: Icons.chat_bubble_outline,
                          label: chat.currentSessionId!.length > 16
                              ? '...${chat.currentSessionId!.substring(chat.currentSessionId!.length - 12)}'
                              : chat.currentSessionId!,
                          highlight: true,
                        ),
                      ],
                    ],
                  ),
                ),
              ),
            ] else
              const Spacer(),
            const SizedBox(width: 8),
            IconButton(
              tooltip: '全局助手',
              onPressed: _toggleGlobalOverlay,
              icon: const Icon(
                Icons.bubble_chart_outlined,
                size: 20,
                color: AppColors.primary,
              ),
            ),
            const AppNotificationCenterButton(),
            IconButton(
              tooltip: '新建对话',
              onPressed: () {
                ref.read(chatProvider(_chatKey).notifier).newSession();
              },
              icon: const Icon(
                Icons.add_comment_outlined,
                size: 20,
                color: AppColors.textSecondary,
              ),
            ),
            PopupMenuButton<_ChatMoreAction>(
              tooltip: '更多',
              icon: const Icon(
                Icons.more_horiz_rounded,
                size: 21,
                color: AppColors.textSecondary,
              ),
              onSelected: (action) {
                switch (action) {
                  case _ChatMoreAction.switchAgent:
                    _showAgentSwitcher();
                    break;
                  case _ChatMoreAction.subagents:
                    if (taskId == null) return;
                    _selectedSubagentTaskId = taskId;
                    if (isDesktop) {
                      setState(() => _subagentPanelOpen = !_subagentPanelOpen);
                    } else {
                      showSubagentTaskSheet(
                        context,
                        taskId: taskId,
                        availableModels: availableModels,
                      );
                    }
                    break;
                  case _ChatMoreAction.files:
                    _openProjectFiles();
                    break;
                }
              },
              itemBuilder: (_) => [
                const PopupMenuItem(
                  value: _ChatMoreAction.switchAgent,
                  child: ListTile(
                    dense: true,
                    leading: Icon(Icons.swap_horiz_rounded, size: 19),
                    title: Text('切换 Agent'),
                  ),
                ),
                PopupMenuItem(
                  value: _ChatMoreAction.subagents,
                  enabled: taskId != null,
                  child: ListTile(
                    dense: true,
                    leading: const Icon(Icons.account_tree_outlined, size: 19),
                    title: Text(
                      taskId == null
                          ? '子代理任务（暂无）'
                          : isDesktop && _subagentPanelOpen
                          ? '隐藏子代理任务'
                          : '子代理任务',
                    ),
                  ),
                ),
                const PopupMenuItem(
                  value: _ChatMoreAction.files,
                  child: ListTile(
                    dense: true,
                    leading: Icon(Icons.folder_open_rounded, size: 19),
                    title: Text('工作目录文件'),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

enum _ChatMoreAction { switchAgent, subagents, files }

class _ContextChip extends StatelessWidget {
  final IconData icon;
  final String label;
  final bool highlight;
  const _ContextChip({
    required this.icon,
    required this.label,
    this.highlight = false,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: highlight ? AppColors.primaryLight : AppColors.inputBackground,
        borderRadius: AppRadius.smRadius,
        border: Border.all(
          color: highlight ? AppColors.primaryMuted : AppColors.border,
        ),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            icon,
            size: 12,
            color: highlight ? AppColors.primary : AppColors.textMuted,
          ),
          const SizedBox(width: 4),
          Text(
            label,
            style: TextStyle(
              fontSize: 12,
              color: highlight ? AppColors.primary : AppColors.textSecondary,
              fontWeight: highlight ? FontWeight.w500 : FontWeight.w400,
            ),
          ),
        ],
      ),
    );
  }
}

// 消息区
class _MessageArea extends StatelessWidget {
  final List<ChatMessage> messages;
  final ScrollController scrollController;
  final bool loadingEarlierHistory;
  final bool hasEarlierHistory;
  final Future<void> Function(ChatMessage message) onResend;
  final ValueChanged<UserScrollNotification> onUserScroll;

  const _MessageArea({
    required this.messages,
    required this.scrollController,
    required this.loadingEarlierHistory,
    required this.hasEarlierHistory,
    required this.onResend,
    required this.onUserScroll,
  });

  @override
  Widget build(BuildContext context) {
    if (messages.isEmpty) {
      return const Center(
        child: EmptyState(
          message: '发送消息开始对话\n或从左侧历史记录切换会话',
          icon: Icons.chat_bubble_outline_rounded,
        ),
      );
    }

    final showHistoryLoader = loadingEarlierHistory || hasEarlierHistory;
    return NotificationListener<UserScrollNotification>(
      onNotification: (notification) {
        onUserScroll(notification);
        return false;
      },
      child: ListView.builder(
        controller: scrollController,
        padding: EdgeInsets.symmetric(
          horizontal: AppBreakpoints.isMobile(context) ? 12 : 24,
          vertical: 16,
        ),
        itemCount: messages.length + (showHistoryLoader ? 1 : 0),
        itemBuilder: (context, i) {
          if (showHistoryLoader && i == 0) {
            return _EarlierHistoryLoader(loading: loadingEarlierHistory);
          }
          final msg = messages[i - (showHistoryLoader ? 1 : 0)];
          return _MessageBubble(
            message: msg,
            onResend: onResend,
            key: ValueKey(msg.id),
          );
        },
      ),
    );
  }
}

class _EarlierHistoryLoader extends StatelessWidget {
  final bool loading;

  const _EarlierHistoryLoader({required this.loading});

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: 42,
      child: Center(
        child: loading
            ? const SizedBox(
                width: 18,
                height: 18,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : const SizedBox.shrink(),
      ),
    );
  }
}

class _MessageBubble extends StatelessWidget {
  final ChatMessage message;
  final Future<void> Function(ChatMessage message) onResend;
  const _MessageBubble({
    required this.message,
    required this.onResend,
    super.key,
  });

  @override
  Widget build(BuildContext context) {
    if (message.isInsertedContext) {
      return _InsertedContextNode(message: message);
    }

    final isUser = message.role == MessageRole.user;
    final hasStreamingOutput =
        message.content.trim().isNotEmpty ||
        message.thinkingContent.trim().isNotEmpty ||
        message.blocks.any(
          (block) =>
              block.type == ChatMessageBlockType.text &&
              block.text.trim().isNotEmpty,
        );

    return Padding(
      padding: const EdgeInsets.only(bottom: 16),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisAlignment: isUser
            ? MainAxisAlignment.end
            : MainAxisAlignment.start,
        children: [
          if (!isUser) ...[_Avatar(isUser: false), const SizedBox(width: 10)],
          Flexible(
            child: Column(
              crossAxisAlignment: isUser
                  ? CrossAxisAlignment.end
                  : CrossAxisAlignment.start,
              children: [
                // 思考过程折叠区（仅 agent 消息）
                if (!isUser && message.thinkingContent.isNotEmpty)
                  _ThinkingBlock(
                    thinking: message.thinkingContent,
                    isStreaming: message.state == MessageState.streaming,
                  ),
                if (!isUser && message.thinkingContent.isNotEmpty)
                  const SizedBox(height: 6),
                if (!isUser &&
                    message.state == MessageState.streaming &&
                    message.imageGeneration == null &&
                    !message.isFinalizingDelivery &&
                    !hasStreamingOutput) ...[
                  _StreamingProgressHint(message: message),
                  const SizedBox(height: 6),
                ],
                if (!isUser &&
                    message.statusHint.isNotEmpty &&
                    message.imageGeneration == null) ...[
                  _MessageStatusHint(text: message.statusHint),
                  const SizedBox(height: 6),
                ],
                // 正文气泡
                if (message.content.isNotEmpty ||
                    message.thinkingContent.isNotEmpty ||
                    message.blocks.isNotEmpty ||
                    message.toolCalls.isNotEmpty ||
                    message.imageGeneration != null ||
                    message.isFinalizingDelivery ||
                    message.artifacts.isNotEmpty ||
                    message.files.isNotEmpty ||
                    isUser ||
                    message.state == MessageState.failed)
                  _BubbleContent(message: message, isUser: isUser),
                // streaming 时且还没正文，显示跳动点
                if (!isUser &&
                    message.state == MessageState.streaming &&
                    message.content.isEmpty &&
                    message.thinkingContent.isEmpty &&
                    !message.blocks.any(
                      (block) =>
                          block.type == ChatMessageBlockType.text &&
                          block.text.trim().isNotEmpty,
                    ) &&
                    message.imageGeneration == null &&
                    !message.isFinalizingDelivery)
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: _TypingIndicator(),
                  ),
                // 用户消息附件预览
                if (isUser && message.attachedFiles.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  _AttachmentPreview(files: message.attachedFiles),
                ],
                if (isUser)
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: _UserMessageActions(
                      onResend: () => onResend(message),
                    ),
                  ),
              ],
            ),
          ),
          if (isUser) ...[const SizedBox(width: 10), _Avatar(isUser: true)],
        ],
      ),
    );
  }
}

class _InsertedContextNode extends StatelessWidget {
  final ChatMessage message;

  const _InsertedContextNode({required this.message});

  @override
  Widget build(BuildContext context) {
    if (message.insertedContextType == 'subagentResult' ||
        message.insertedContextSource == 'subagent') {
      return const SizedBox.shrink();
    }
    if (message.insertedContextType == 'backgroundJob' ||
        message.insertedContextSource == 'background_job') {
      return _BackgroundJobContextNode(message: message);
    }
    final text = message.content.trim();
    return Padding(
      padding: const EdgeInsets.only(bottom: 16),
      child: Stack(
        clipBehavior: Clip.none,
        children: [
          Positioned(
            left: 11,
            top: -14,
            bottom: -14,
            child: Container(width: 1, color: AppColors.primaryMuted),
          ),
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 24,
                height: 24,
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  color: AppColors.primaryLight,
                  shape: BoxShape.circle,
                  border: Border.all(color: AppColors.primaryMuted),
                ),
                child: const Icon(
                  Icons.subdirectory_arrow_right_rounded,
                  size: 14,
                  color: AppColors.primary,
                ),
              ),
              const SizedBox(width: 9),
              Expanded(
                child: Container(
                  padding: const EdgeInsets.fromLTRB(11, 8, 11, 8),
                  decoration: BoxDecoration(
                    color: AppColors.primaryLight,
                    border: Border(
                      left: const BorderSide(
                        color: AppColors.primary,
                        width: 2,
                      ),
                      top: BorderSide(
                        color: AppColors.primaryMuted.withValues(alpha: 0.9),
                      ),
                      right: BorderSide(
                        color: AppColors.primaryMuted.withValues(alpha: 0.9),
                      ),
                      bottom: BorderSide(
                        color: AppColors.primaryMuted.withValues(alpha: 0.9),
                      ),
                    ),
                    borderRadius: AppRadius.smRadius,
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          const Text(
                            '已追加到当前任务',
                            style: TextStyle(
                              fontSize: 11,
                              fontWeight: FontWeight.w700,
                              color: AppColors.primary,
                            ),
                          ),
                          const SizedBox(width: 7),
                          Text(
                            '任务上下文',
                            style: const TextStyle(
                              fontSize: 10,
                              color: AppColors.textMuted,
                            ),
                          ),
                        ],
                      ),
                      if (text.isNotEmpty) ...[
                        const SizedBox(height: 4),
                        Text(
                          text,
                          maxLines: 4,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontSize: 12,
                            height: 1.5,
                            color: Color(0xFF17356D),
                          ),
                        ),
                      ],
                      const SizedBox(height: 5),
                      const Text(
                        '来自发送队列 · 已进入下一轮上下文',
                        style: TextStyle(
                          fontSize: 10,
                          color: AppColors.textSecondary,
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _BackgroundJobContextNode extends StatelessWidget {
  final ChatMessage message;

  const _BackgroundJobContextNode({required this.message});

  @override
  Widget build(BuildContext context) {
    final metadata = message.insertedContextMetadata;
    final failed = metadata['job_status']?.toString() == 'error';
    final accent = failed ? const Color(0xFFB42318) : const Color(0xFF18794E);
    final surface = failed ? const Color(0xFFFFF1F0) : const Color(0xFFEFFAF3);
    final title = metadata['job_title']?.toString().trim();
    final command = metadata['command']?.toString().trim();
    final completedAt = (metadata['completed_at'] as num?)?.toInt();
    final completedAtText = completedAt == null
        ? ''
        : MaterialLocalizations.of(context).formatTimeOfDay(
            TimeOfDay.fromDateTime(
              DateTime.fromMillisecondsSinceEpoch(completedAt).toLocal(),
            ),
          );
    final output = message.content.trim();
    final heading = failed ? '后台任务失败' : '后台任务已完成';
    final subtitle = [
      if (title?.isNotEmpty == true) title!,
      if (command?.isNotEmpty == true) command!,
      if (completedAtText.isNotEmpty) completedAtText,
    ].join(' · ');

    return Padding(
      padding: const EdgeInsets.only(bottom: 16),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 24,
            height: 24,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: surface,
              shape: BoxShape.circle,
              border: Border.all(color: accent.withValues(alpha: 0.35)),
            ),
            child: Icon(Icons.terminal_rounded, size: 14, color: accent),
          ),
          const SizedBox(width: 9),
          Expanded(
            child: Container(
              decoration: BoxDecoration(
                color: surface,
                border: Border(left: BorderSide(color: accent, width: 2)),
                borderRadius: AppRadius.smRadius,
              ),
              child: ExpansionTile(
                tilePadding: const EdgeInsets.fromLTRB(11, 5, 8, 5),
                childrenPadding: const EdgeInsets.fromLTRB(11, 0, 11, 10),
                iconColor: accent,
                collapsedIconColor: accent,
                title: Text(
                  heading,
                  style: TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w700,
                    color: accent,
                  ),
                ),
                subtitle: Text(
                  subtitle.isNotEmpty ? subtitle : '结果已进入下一轮任务上下文',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 11,
                    color: AppColors.textSecondary,
                  ),
                ),
                children: [
                  if (command?.isNotEmpty == true)
                    Align(
                      alignment: Alignment.centerLeft,
                      child: Text(
                        command!,
                        style: const TextStyle(
                          fontSize: 11,
                          height: 1.4,
                          fontFamily: 'monospace',
                          color: AppColors.textSecondary,
                        ),
                      ),
                    ),
                  if (command?.isNotEmpty == true && output.isNotEmpty)
                    const SizedBox(height: 7),
                  if (output.isNotEmpty)
                    SelectableText(
                      output,
                      style: const TextStyle(
                        fontSize: 11,
                        height: 1.45,
                        fontFamily: 'monospace',
                        color: Color(0xFF24344D),
                      ),
                    ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _UserMessageActions extends StatelessWidget {
  final VoidCallback onResend;

  const _UserMessageActions({required this.onResend});

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.end,
      children: [
        InkWell(
          onTap: onResend,
          borderRadius: AppRadius.smRadius,
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
            decoration: BoxDecoration(
              color: AppColors.primaryLight,
              borderRadius: AppRadius.smRadius,
              border: Border.all(color: AppColors.border),
            ),
            child: const Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(Icons.refresh_rounded, size: 14, color: AppColors.primary),
                SizedBox(width: 4),
                Text(
                  '重新发送',
                  style: TextStyle(
                    fontSize: 11,
                    color: AppColors.primary,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

class _MCPToolsButton extends StatelessWidget {
  final VoidCallback onTap;

  const _MCPToolsButton({required this.onTap});

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: AppRadius.smRadius,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
        decoration: BoxDecoration(
          color: AppColors.inputBackground,
          borderRadius: AppRadius.smRadius,
          border: Border.all(color: AppColors.border),
        ),
        child: const Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.extension_outlined,
              size: 14,
              color: AppColors.textSecondary,
            ),
            SizedBox(width: 4),
            Text(
              'MCP工具',
              style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
            ),
          ],
        ),
      ),
    );
  }
}

class _MCPToolsSheet extends StatelessWidget {
  final Future<MCPStatusInfo> future;

  const _MCPToolsSheet({required this.future});

  @override
  Widget build(BuildContext context) {
    return FractionallySizedBox(
      heightFactor: 0.82,
      child: Container(
        decoration: const BoxDecoration(
          color: AppColors.surface,
          borderRadius: BorderRadius.vertical(top: Radius.circular(20)),
        ),
        child: Column(
          children: [
            Container(
              width: 36,
              height: 4,
              margin: const EdgeInsets.only(top: 10, bottom: 12),
              decoration: BoxDecoration(
                color: AppColors.border,
                borderRadius: BorderRadius.circular(99),
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 18),
              child: Row(
                children: [
                  const Icon(
                    Icons.extension_outlined,
                    color: AppColors.primary,
                  ),
                  const SizedBox(width: 8),
                  Text(
                    'MCP工具状态',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const Spacer(),
                  IconButton(
                    onPressed: () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close_rounded),
                  ),
                ],
              ),
            ),
            const Divider(height: 1),
            Expanded(
              child: FutureBuilder<MCPStatusInfo>(
                future: future,
                builder: (context, snapshot) {
                  if (snapshot.connectionState != ConnectionState.done) {
                    return const SingleChildScrollView(
                      padding: EdgeInsets.all(18),
                      child: DialogContentSkeleton(
                        itemCount: 4,
                        showHeader: false,
                      ),
                    );
                  }
                  if (snapshot.hasError) {
                    return EmptyState(
                      icon: Icons.error_outline_rounded,
                      message: '读取 MCP 工具状态失败\n${snapshot.error}',
                    );
                  }
                  final status = snapshot.data;
                  final servers =
                      status?.servers
                          .where((server) => server.visibleInChat)
                          .toList(growable: false) ??
                      const <MCPServerStatusInfo>[];
                  if (servers.isEmpty) {
                    return const EmptyState(
                      icon: Icons.extension_off_outlined,
                      message: '当前 Agent 没有启用的 MCP 工具',
                    );
                  }
                  return ListView.separated(
                    padding: const EdgeInsets.all(16),
                    itemCount: servers.length,
                    separatorBuilder: (_, __) => const SizedBox(height: 10),
                    itemBuilder: (context, index) {
                      final server = servers[index];
                      return _MCPServerTile(server: server);
                    },
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _MCPServerTile extends StatelessWidget {
  final MCPServerStatusInfo server;

  const _MCPServerTile({required this.server});

  @override
  Widget build(BuildContext context) {
    final type = switch (server.status) {
      'connected' => StatusType.online,
      'failed' => StatusType.error,
      'disabled' => StatusType.offline,
      'needs_auth' => StatusType.warning,
      _ => StatusType.processing,
    };
    return PanelCard(
      padding: EdgeInsets.zero,
      child: InkWell(
        onTap: () => _showDetail(context),
        borderRadius: AppRadius.lgRadius,
        child: Padding(
          padding: AppSpacing.cardPadding,
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      server.name,
                      style: const TextStyle(
                        fontSize: 15,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 6),
                    Text(
                      '工具 ${server.tools.length} 个',
                      style: const TextStyle(
                        fontSize: 12,
                        color: AppColors.textMuted,
                      ),
                    ),
                    if (server.error.isNotEmpty) ...[
                      const SizedBox(height: 6),
                      Text(
                        server.error,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          fontSize: 12,
                          color: AppColors.statusError,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
              StatusPill(label: _statusText(server.status), type: type),
              const SizedBox(width: 8),
              const Icon(
                Icons.chevron_right_rounded,
                color: AppColors.textMuted,
              ),
            ],
          ),
        ),
      ),
    );
  }

  String _statusText(String status) => switch (status) {
    'connected' => '已连接',
    'failed' => '失败',
    'disabled' => '已停用',
    'needs_auth' => '需授权',
    'needs_client_registration' => '需注册',
    _ => status,
  };

  void _showDetail(BuildContext context) {
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (_) => _MCPServerDetailSheet(
        server: server,
        statusText: _statusText(server.status),
      ),
    );
  }
}

class _MCPServerDetailSheet extends StatelessWidget {
  final MCPServerStatusInfo server;
  final String statusText;

  const _MCPServerDetailSheet({required this.server, required this.statusText});

  @override
  Widget build(BuildContext context) {
    return FractionallySizedBox(
      heightFactor: 0.78,
      child: Container(
        decoration: const BoxDecoration(
          color: AppColors.surface,
          borderRadius: BorderRadius.vertical(top: Radius.circular(20)),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(18, 18, 10, 12),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      server.name,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                  ),
                  IconButton(
                    onPressed: () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close_rounded),
                  ),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 18),
              child: Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  StatusPill(
                    label: statusText,
                    type: server.status == 'connected'
                        ? StatusType.online
                        : StatusType.error,
                  ),
                  StatusPill(
                    label: '工具 ${server.tools.length} 个',
                    type: StatusType.processing,
                  ),
                ],
              ),
            ),
            if (server.error.isNotEmpty)
              Padding(
                padding: const EdgeInsets.fromLTRB(18, 12, 18, 0),
                child: Text(
                  server.error,
                  style: const TextStyle(
                    color: AppColors.statusError,
                    fontSize: 13,
                  ),
                ),
              ),
            const SizedBox(height: 12),
            const Divider(height: 1),
            Expanded(
              child: server.tools.isEmpty
                  ? const EmptyState(
                      icon: Icons.build_circle_outlined,
                      message: '当前 MCP 没有返回工具',
                    )
                  : ListView.separated(
                      padding: const EdgeInsets.all(16),
                      itemCount: server.tools.length,
                      separatorBuilder: (_, __) => const SizedBox(height: 10),
                      itemBuilder: (context, index) {
                        final tool = server.tools[index];
                        return PanelCard(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                tool.id,
                                style: const TextStyle(
                                  fontSize: 14,
                                  fontWeight: FontWeight.w700,
                                ),
                              ),
                              if (tool.description.isNotEmpty) ...[
                                const SizedBox(height: 8),
                                Text(
                                  tool.description,
                                  style: const TextStyle(
                                    fontSize: 13,
                                    color: AppColors.textSecondary,
                                  ),
                                ),
                              ],
                            ],
                          ),
                        );
                      },
                    ),
            ),
          ],
        ),
      ),
    );
  }
}

class _MessageStatusHint extends StatelessWidget {
  final String text;

  const _MessageStatusHint({required this.text});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
      decoration: BoxDecoration(
        color: AppColors.primaryLight,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.border),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(
            Icons.sync_problem_outlined,
            size: 14,
            color: AppColors.primary,
          ),
          const SizedBox(width: 6),
          Flexible(
            child: Text(
              text,
              style: const TextStyle(
                fontSize: 12,
                height: 1.5,
                color: AppColors.textSecondary,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _StreamingProgressHint extends StatefulWidget {
  final ChatMessage message;

  const _StreamingProgressHint({required this.message});

  @override
  State<_StreamingProgressHint> createState() => _StreamingProgressHintState();
}

class _StreamingProgressHintState extends State<_StreamingProgressHint> {
  late final Timer _timer;
  DateTime _now = DateTime.now();

  @override
  void initState() {
    super.initState();
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) {
        setState(() => _now = DateTime.now());
      }
    });
  }

  @override
  void dispose() {
    _timer.cancel();
    super.dispose();
  }

  bool get _hasText {
    if (widget.message.content.trim().isNotEmpty) return true;
    return widget.message.blocks.any(
      (block) =>
          block.type == ChatMessageBlockType.text &&
          block.text.trim().isNotEmpty,
    );
  }

  bool get _hasThinking => widget.message.thinkingContent.trim().isNotEmpty;
  bool get _hasTools => widget.message.toolCalls.isNotEmpty;
  bool get _hasRunningTools =>
      widget.message.toolCalls.any((tool) => tool.isRunning);
  bool get _hasPlan =>
      widget.message.hasPlanBinding &&
      widget.message.plan != null &&
      !widget.message.plan!.isEmpty;

  String get _title {
    if (_hasText) return '正在生成正文';
    if (_hasRunningTools) return '正在等待工具返回';
    if (_hasTools) return '工具结果已同步，等待正文输出';
    if (_hasPlan) return '计划已更新，等待正文输出';
    if (_hasThinking) return '模型已开始思考，正在等待正文输出';
    if ((widget.message.taskId ?? '').isEmpty) return '正在创建任务';
    return '模型请求中，等待首个输出';
  }

  String get _subtitle {
    final parts = <String>[];
    final elapsed = _now.difference(widget.message.createdAt);
    parts.add('已用时 ${_formatDuration(elapsed)}');
    final firstToken = widget.message.firstTokenTime;
    if (firstToken != null) {
      parts.add('首字 ${_formatDuration(firstToken)}');
    } else if (!_hasText && elapsed.inSeconds >= 30) {
      parts.add('正文首包较慢');
    }
    if (_hasTools) {
      final running = widget.message.toolCalls
          .where((tool) => tool.isRunning)
          .length;
      final completed = widget.message.toolCalls
          .where((tool) => tool.isCompleted)
          .length;
      if (running > 0) {
        parts.add('$running 个工具运行中');
      } else if (completed > 0) {
        parts.add('$completed 个工具已完成');
      }
    }
    return parts.join(' · ');
  }

  List<_StreamingStage> get _stages {
    return [
      _StreamingStage('已发送', true, false),
      _StreamingStage('已连接', (widget.message.taskId ?? '').isNotEmpty, false),
      _StreamingStage('思考', _hasThinking, !_hasText && _hasThinking),
      _StreamingStage('正文', _hasText, _hasText),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final elapsed = _now.difference(widget.message.createdAt);
    final slow = !_hasText && elapsed.inSeconds >= 60;
    return Container(
      constraints: const BoxConstraints(maxWidth: 520),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
      decoration: BoxDecoration(
        color: slow ? AppColors.statusWarningLight : AppColors.primaryLight,
        borderRadius: AppRadius.smRadius,
        border: Border.all(
          color: slow
              ? AppColors.statusWarning.withValues(alpha: 0.28)
              : AppColors.border,
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Row(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Padding(
                padding: const EdgeInsets.only(top: 2),
                child: SizedBox(
                  width: 14,
                  height: 14,
                  child: CircularProgressIndicator(
                    strokeWidth: 1.8,
                    color: slow ? AppColors.statusWarning : AppColors.primary,
                  ),
                ),
              ),
              const SizedBox(width: 7),
              Flexible(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      _title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 12,
                        height: 1.35,
                        fontWeight: FontWeight.w600,
                        color: slow
                            ? AppColors.statusWarning
                            : AppColors.textSecondary,
                      ),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      _subtitle,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 11,
                        height: 1.4,
                        color: AppColors.textMuted,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: _stages
                .map((stage) {
                  final color = stage.active
                      ? AppColors.primary
                      : stage.done
                      ? AppColors.statusSuccess
                      : AppColors.textMuted;
                  return Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 7,
                      vertical: 3,
                    ),
                    decoration: BoxDecoration(
                      color: stage.active || stage.done
                          ? AppColors.surface
                          : AppColors.surfaceElevated,
                      borderRadius: BorderRadius.circular(999),
                      border: Border.all(
                        color: color.withValues(
                          alpha: stage.done ? 0.22 : 0.16,
                        ),
                      ),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Icon(
                          stage.done
                              ? Icons.check_circle_rounded
                              : Icons.radio_button_unchecked_rounded,
                          size: 11,
                          color: color,
                        ),
                        const SizedBox(width: 4),
                        Text(
                          stage.label,
                          style: TextStyle(
                            fontSize: 10,
                            height: 1.2,
                            fontWeight: stage.active
                                ? FontWeight.w600
                                : FontWeight.w500,
                            color: color,
                          ),
                        ),
                      ],
                    ),
                  );
                })
                .toList(growable: false),
          ),
        ],
      ),
    );
  }

  String _formatDuration(Duration duration) {
    final safe = duration.isNegative ? Duration.zero : duration;
    if (safe.inMilliseconds < 1000) return '${safe.inMilliseconds}ms';
    final minutes = safe.inMinutes;
    final seconds = safe.inSeconds.remainder(60).toString().padLeft(2, '0');
    if (minutes > 0) return '$minutes:$seconds';
    return '${safe.inSeconds}s';
  }
}

class _StreamingStage {
  final String label;
  final bool done;
  final bool active;

  const _StreamingStage(this.label, this.done, this.active);
}

// 附件预览（气泡下方）
class _AttachmentPreview extends StatelessWidget {
  final List<AttachedFile> files;
  const _AttachmentPreview({required this.files});

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 6,
      runSpacing: 6,
      alignment: WrapAlignment.end,
      children: files
          .map((f) => _AttachmentChip(key: ValueKey(f.id), file: f))
          .toList(growable: false),
    );
  }
}

class _ArtifactPreview extends StatelessWidget {
  final String? taskId;
  final List<AiArtifact> artifacts;

  const _ArtifactPreview({required this.taskId, required this.artifacts});

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Padding(
          padding: EdgeInsets.only(bottom: 8),
          child: Text(
            '生成文件',
            style: TextStyle(
              fontSize: 12,
              color: AppColors.textMuted,
              fontWeight: FontWeight.w600,
            ),
          ),
        ),
        ...artifacts.map(
          (artifact) => Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: _ArtifactTile(taskId: taskId, artifact: artifact),
          ),
        ),
      ],
    );
  }
}

class _InlineFilePreview extends StatelessWidget {
  final List<AiOutputFile> files;

  const _InlineFilePreview({required this.files});

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Padding(
          padding: EdgeInsets.only(bottom: 8),
          child: Text(
            '生成图片',
            style: TextStyle(
              fontSize: 12,
              color: AppColors.textMuted,
              fontWeight: FontWeight.w600,
            ),
          ),
        ),
        ...files.map(
          (file) => Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: _InlineImageCard(file: file),
          ),
        ),
      ],
    );
  }
}

class _ImageGenerationProgressCard extends StatefulWidget {
  final ImageGenerationInfo info;
  final String statusHint;

  const _ImageGenerationProgressCard({
    required this.info,
    required this.statusHint,
  });

  @override
  State<_ImageGenerationProgressCard> createState() =>
      _ImageGenerationProgressCardState();
}

class _ImageGenerationProgressCardState
    extends State<_ImageGenerationProgressCard>
    with SingleTickerProviderStateMixin {
  late final AnimationController _ctrl;
  late Timer _timer;
  Duration _elapsed = Duration.zero;

  @override
  void initState() {
    super.initState();
    _elapsed = DateTime.now().difference(widget.info.startedAt);
    _ctrl = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1400),
    )..repeat();
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) return;
      setState(() {
        _elapsed = DateTime.now().difference(widget.info.startedAt);
      });
    });
  }

  @override
  void dispose() {
    _timer.cancel();
    _ctrl.dispose();
    super.dispose();
  }

  String get _stageText {
    final hint = widget.statusHint.trim();
    if (hint.isNotEmpty) {
      if (hint.contains('重试')) return hint;
      return hint;
    }
    if (_elapsed.inSeconds < 3) return '准备提示词';
    if (_elapsed.inSeconds < 8) return '提交生图请求';
    if (_elapsed.inSeconds < 35) return '图片生成中';
    return '上游较慢，正在等待';
  }

  String get _elapsedText {
    final seconds = _elapsed.inSeconds.clamp(0, 24 * 60 * 60);
    if (seconds < 60) return '${seconds}s';
    final minutes = seconds ~/ 60;
    final rest = seconds % 60;
    return '${minutes}m ${rest}s';
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      decoration: BoxDecoration(
        color: AppColors.surfaceElevated,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          AspectRatio(
            aspectRatio: 1,
            child: ClipRRect(
              borderRadius: const BorderRadius.vertical(
                top: Radius.circular(8),
              ),
              child: AnimatedBuilder(
                animation: _ctrl,
                builder: (context, _) {
                  final t = _ctrl.value;
                  return Stack(
                    fit: StackFit.expand,
                    children: [
                      Container(color: AppColors.inputBackground),
                      FractionallySizedBox(
                        widthFactor: 0.28,
                        alignment: Alignment(-1.4 + t * 2.8, 0),
                        child: DecoratedBox(
                          decoration: BoxDecoration(
                            gradient: LinearGradient(
                              colors: [
                                Colors.white.withValues(alpha: 0),
                                Colors.white.withValues(alpha: 0.72),
                                Colors.white.withValues(alpha: 0),
                              ],
                            ),
                          ),
                        ),
                      ),
                      Center(
                        child: Container(
                          width: 52,
                          height: 52,
                          decoration: BoxDecoration(
                            color: AppColors.primaryLight,
                            borderRadius: BorderRadius.circular(26),
                            border: Border.all(
                              color: AppColors.primary.withValues(alpha: 0.18),
                            ),
                          ),
                          child: const Icon(
                            Icons.image_outlined,
                            color: AppColors.primary,
                            size: 26,
                          ),
                        ),
                      ),
                    ],
                  );
                },
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 10, 12, 12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                    const SizedBox(width: 8),
                    const Expanded(
                      child: Text(
                        '正在生成图片',
                        style: TextStyle(
                          fontSize: 13,
                          color: AppColors.textPrimary,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                    Text(
                      _elapsedText,
                      style: const TextStyle(
                        fontSize: 11,
                        color: AppColors.textMuted,
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 8),
                Text(
                  _stageText,
                  style: const TextStyle(
                    fontSize: 12,
                    color: AppColors.textSecondary,
                    height: 1.35,
                  ),
                ),
                const SizedBox(height: 6),
                Text(
                  widget.info.modelName,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 11,
                    color: AppColors.textMuted,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _ArtifactTile extends StatelessWidget {
  final String? taskId;
  final AiArtifact artifact;

  const _ArtifactTile({required this.taskId, required this.artifact});

  @override
  Widget build(BuildContext context) {
    if (artifact.isImage) {
      return _ArtifactImageCard(taskId: taskId, artifact: artifact);
    }
    return _ArtifactFileCard(taskId: taskId, artifact: artifact);
  }
}

class _InlineImageCard extends StatelessWidget {
  final AiOutputFile file;

  const _InlineImageCard({required this.file});

  Widget _image({double? width, double? height, BoxFit fit = BoxFit.cover}) {
    final bytes = file.imageBytes;
    if (bytes != null) {
      return Image.memory(
        bytes,
        width: width,
        height: height,
        fit: fit,
        errorBuilder: (_, __, ___) => const SizedBox(
          height: 220,
          child: Center(child: Icon(Icons.broken_image_outlined)),
        ),
      );
    }
    return Image.network(
      file.url,
      width: width,
      height: height,
      fit: fit,
      errorBuilder: (_, __, ___) => const SizedBox(
        height: 220,
        child: Center(child: Icon(Icons.broken_image_outlined)),
      ),
    );
  }

  Future<void> _open(BuildContext context) async {
    await showDialog<void>(
      context: context,
      barrierColor: Colors.black87,
      builder: (context) => Dialog(
        backgroundColor: Colors.black,
        insetPadding: const EdgeInsets.all(16),
        child: ConstrainedBox(
          constraints: BoxConstraints(
            maxWidth: MediaQuery.of(context).size.width * 0.92,
            maxHeight: MediaQuery.of(context).size.height * 0.82,
          ),
          child: Stack(
            children: [
              Positioned.fill(
                child: InteractiveViewer(
                  minScale: 0.5,
                  maxScale: 4,
                  child: Center(child: _image(fit: BoxFit.contain)),
                ),
              ),
              Positioned(
                top: 8,
                right: 8,
                child: IconButton(
                  onPressed: () => Navigator.of(context).pop(),
                  icon: const Icon(Icons.close_rounded, color: Colors.white),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: AppRadius.smRadius,
        onTap: () => _open(context),
        child: Ink(
          decoration: BoxDecoration(
            color: AppColors.surfaceElevated,
            borderRadius: AppRadius.smRadius,
            border: Border.all(color: AppColors.border),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              ClipRRect(
                borderRadius: const BorderRadius.vertical(
                  top: Radius.circular(10),
                ),
                child: _image(width: double.infinity, height: 220),
              ),
              Padding(
                padding: const EdgeInsets.fromLTRB(12, 10, 12, 12),
                child: Row(
                  children: [
                    const Icon(
                      Icons.image_outlined,
                      size: 18,
                      color: AppColors.primary,
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            file.filename,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 13,
                              color: AppColors.textPrimary,
                              fontWeight: FontWeight.w600,
                            ),
                          ),
                          const SizedBox(height: 4),
                          Text(
                            '模型直出图片 · 点击查看大图',
                            style: const TextStyle(
                              fontSize: 11,
                              color: AppColors.textSecondary,
                            ),
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(width: 8),
                    TextButton.icon(
                      onPressed: () => _open(context),
                      icon: const Icon(Icons.open_in_full_rounded, size: 16),
                      label: const Text('预览'),
                      style: TextButton.styleFrom(
                        foregroundColor: AppColors.primary,
                        padding: const EdgeInsets.symmetric(
                          horizontal: 10,
                          vertical: 8,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _ArtifactFileCard extends ConsumerStatefulWidget {
  final String? taskId;
  final AiArtifact artifact;

  const _ArtifactFileCard({required this.taskId, required this.artifact});

  @override
  ConsumerState<_ArtifactFileCard> createState() => _ArtifactFileCardState();
}

class _ArtifactFileCardState extends ConsumerState<_ArtifactFileCard> {
  bool _downloading = false;

  String _formatSize(int sizeBytes) {
    if (sizeBytes <= 0) return '-';
    if (sizeBytes < 1024) return '${sizeBytes}B';
    if (sizeBytes < 1024 * 1024) {
      return '${(sizeBytes / 1024).toStringAsFixed(1)}KB';
    }
    return '${(sizeBytes / (1024 * 1024)).toStringAsFixed(1)}MB';
  }

  Future<void> _download() async {
    final taskId = widget.taskId;
    if (taskId == null || taskId.isEmpty || _downloading) return;
    setState(() => _downloading = true);
    try {
      await downloadArtifactWithNotification(
        notifications: ref.read(appNotificationControllerProvider.notifier),
        taskId: taskId,
        artifact: widget.artifact,
      );
    } catch (_) {
      // The shared download helper has already published the failure.
    } finally {
      if (mounted) setState(() => _downloading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: AppColors.surfaceElevated,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.border),
      ),
      child: Row(
        children: [
          Container(
            width: 34,
            height: 34,
            decoration: BoxDecoration(
              color: AppColors.primaryLight,
              borderRadius: AppRadius.smRadius,
            ),
            child: const Icon(
              Icons.insert_drive_file_outlined,
              size: 18,
              color: AppColors.primary,
            ),
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  widget.artifact.filename,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 13,
                    color: AppColors.textPrimary,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  '${_formatSize(widget.artifact.sizeBytes)} · ${widget.artifact.mimeType}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 11,
                    color: AppColors.textSecondary,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(width: 8),
          TextButton.icon(
            onPressed: _downloading ? null : _download,
            icon: _downloading
                ? const SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.download_rounded, size: 15),
            label: Text(_downloading ? '下载中' : '下载'),
            style: TextButton.styleFrom(
              foregroundColor: AppColors.primary,
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
            ),
          ),
        ],
      ),
    );
  }
}

class _ArtifactImageCard extends StatefulWidget {
  final String? taskId;
  final AiArtifact artifact;

  const _ArtifactImageCard({required this.taskId, required this.artifact});

  @override
  State<_ArtifactImageCard> createState() => _ArtifactImageCardState();
}

class _ArtifactImageCardState extends State<_ArtifactImageCard> {
  Future<Uint8List>? _future;

  @override
  void initState() {
    super.initState();
    _future = _load();
  }

  @override
  void didUpdateWidget(covariant _ArtifactImageCard oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.taskId != widget.taskId ||
        oldWidget.artifact.id != widget.artifact.id) {
      _future = _load();
    }
  }

  Future<Uint8List> _load() async {
    final taskId = widget.taskId;
    if (taskId == null || taskId.isEmpty) {
      throw Exception('任务标识缺失');
    }
    return ArtifactDownloadService.fetchBytes(
      taskId: taskId,
      artifact: widget.artifact,
    );
  }

  String _heroTag() => 'artifact-image-${widget.taskId}-${widget.artifact.id}';

  String _formatSize(int sizeBytes) {
    if (sizeBytes <= 0) return '-';
    if (sizeBytes < 1024) return '${sizeBytes}B';
    if (sizeBytes < 1024 * 1024) {
      return '${(sizeBytes / 1024).toStringAsFixed(1)}KB';
    }
    return '${(sizeBytes / (1024 * 1024)).toStringAsFixed(1)}MB';
  }

  Future<void> _open(Uint8List bytes) async {
    final taskId = widget.taskId;
    if (taskId == null || taskId.isEmpty) return;
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => ArtifactImageViewerPage(
          taskId: taskId,
          artifact: widget.artifact,
          imageBytes: bytes,
          heroTag: _heroTag(),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<Uint8List>(
      future: _future,
      builder: (context, snapshot) {
        if (snapshot.hasData) {
          final bytes = snapshot.data!;
          return Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Material(
                color: Colors.transparent,
                child: InkWell(
                  borderRadius: AppRadius.smRadius,
                  onTap: () => _open(bytes),
                  child: Ink(
                    decoration: BoxDecoration(
                      color: AppColors.surfaceElevated,
                      borderRadius: AppRadius.smRadius,
                      border: Border.all(color: AppColors.border),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Hero(
                          tag: _heroTag(),
                          child: ClipRRect(
                            borderRadius: const BorderRadius.vertical(
                              top: Radius.circular(10),
                            ),
                            child: Image.memory(
                              bytes,
                              width: double.infinity,
                              height: 220,
                              fit: BoxFit.cover,
                              errorBuilder: (_, __, ___) => const SizedBox(
                                height: 220,
                                child: Center(
                                  child: Icon(Icons.broken_image_outlined),
                                ),
                              ),
                            ),
                          ),
                        ),
                        Padding(
                          padding: const EdgeInsets.fromLTRB(12, 10, 12, 12),
                          child: Row(
                            children: [
                              const Icon(
                                Icons.image_outlined,
                                size: 18,
                                color: AppColors.primary,
                              ),
                              const SizedBox(width: 8),
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(
                                      widget.artifact.filename,
                                      maxLines: 1,
                                      overflow: TextOverflow.ellipsis,
                                      style: const TextStyle(
                                        fontSize: 13,
                                        color: AppColors.textPrimary,
                                        fontWeight: FontWeight.w600,
                                      ),
                                    ),
                                    const SizedBox(height: 4),
                                    Text(
                                      '${_formatSize(widget.artifact.sizeBytes)} · 点击查看大图',
                                      style: const TextStyle(
                                        fontSize: 11,
                                        color: AppColors.textSecondary,
                                      ),
                                    ),
                                  ],
                                ),
                              ),
                              const SizedBox(width: 8),
                              TextButton.icon(
                                onPressed: () => _open(bytes),
                                icon: const Icon(
                                  Icons.open_in_full_rounded,
                                  size: 16,
                                ),
                                label: const Text('预览'),
                                style: TextButton.styleFrom(
                                  foregroundColor: AppColors.primary,
                                  padding: const EdgeInsets.symmetric(
                                    horizontal: 10,
                                    vertical: 8,
                                  ),
                                ),
                              ),
                            ],
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ],
          );
        }

        if (snapshot.hasError) {
          return _ArtifactFileCard(
            taskId: widget.taskId,
            artifact: widget.artifact,
          );
        }

        return Container(
          width: double.infinity,
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: AppColors.surfaceElevated,
            borderRadius: AppRadius.smRadius,
            border: Border.all(color: AppColors.border),
          ),
          child: const Row(
            children: [
              SizedBox(
                width: 18,
                height: 18,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
              SizedBox(width: 10),
              Expanded(
                child: Text(
                  '正在加载图片预览…',
                  style: TextStyle(
                    fontSize: 12,
                    color: AppColors.textSecondary,
                  ),
                ),
              ),
            ],
          ),
        );
      },
    );
  }
}

class _AttachmentChip extends StatelessWidget {
  final AttachedFile file;
  const _AttachmentChip({super.key, required this.file});

  @override
  Widget build(BuildContext context) {
    final bytes = file.imageBytes;
    if (file.isImage && bytes != null) {
      return ClipRRect(
        borderRadius: AppRadius.smRadius,
        child: Image.memory(
          bytes,
          width: 100,
          height: 100,
          fit: BoxFit.cover,
          gaplessPlayback: true,
          errorBuilder: (_, __, ___) => _FileChip(file: file),
        ),
      );
    }
    return _FileChip(file: file);
  }
}

class _FileChip extends StatelessWidget {
  final AttachedFile file;
  const _FileChip({required this.file});

  @override
  Widget build(BuildContext context) {
    final icon = file.isPdf
        ? Icons.picture_as_pdf_outlined
        : Icons.insert_drive_file_outlined;
    return Container(
      constraints: const BoxConstraints(maxWidth: 160),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      decoration: BoxDecoration(
        color: Colors.white.withValues(alpha: 0.15),
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: Colors.white.withValues(alpha: 0.3)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 14, color: Colors.white70),
          const SizedBox(width: 5),
          Flexible(
            child: Text(
              file.filename,
              style: const TextStyle(
                fontSize: 11,
                color: Colors.white,
                overflow: TextOverflow.ellipsis,
              ),
              maxLines: 1,
            ),
          ),
        ],
      ),
    );
  }
}

// 思考过程折叠块
class _ThinkingBlock extends StatefulWidget {
  final String thinking;
  final bool isStreaming;
  const _ThinkingBlock({required this.thinking, required this.isStreaming});

  @override
  State<_ThinkingBlock> createState() => _ThinkingBlockState();
}

class _ThinkingBlockState extends State<_ThinkingBlock> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(maxWidth: 520),
      decoration: BoxDecoration(
        color: AppColors.surfaceElevated,
        borderRadius: AppRadius.mdRadius,
        border: Border.all(color: AppColors.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // 标题栏
          InkWell(
            onTap: () => setState(() => _expanded = !_expanded),
            borderRadius: AppRadius.mdRadius,
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (widget.isStreaming)
                    Padding(
                      padding: const EdgeInsets.only(right: 6),
                      child: SizedBox(
                        width: 10,
                        height: 10,
                        child: CircularProgressIndicator(
                          strokeWidth: 1.5,
                          color: AppColors.primary,
                        ),
                      ),
                    )
                  else
                    const Icon(
                      Icons.lightbulb_outline,
                      size: 13,
                      color: AppColors.textMuted,
                    ),
                  const SizedBox(width: 5),
                  Text(
                    widget.isStreaming ? '思考中...' : '已完成思考',
                    style: const TextStyle(
                      fontSize: 12,
                      color: AppColors.textMuted,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                  const SizedBox(width: 6),
                  Icon(
                    _expanded ? Icons.expand_less : Icons.expand_more,
                    size: 14,
                    color: AppColors.textMuted,
                  ),
                ],
              ),
            ),
          ),
          // 展开内容
          if (_expanded)
            Container(
              width: double.infinity,
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 10),
              decoration: BoxDecoration(
                border: const Border(top: BorderSide(color: AppColors.border)),
              ),
              child: SelectableText(
                widget.thinking,
                style: const TextStyle(
                  fontSize: 12,
                  height: 1.6,
                  color: AppColors.textSecondary,
                  fontFamily: 'monospace',
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _Avatar extends StatelessWidget {
  final bool isUser;
  const _Avatar({required this.isUser});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 32,
      height: 32,
      decoration: BoxDecoration(
        color: isUser ? AppColors.primary : AppColors.primaryLight,
        borderRadius: AppRadius.smRadius,
      ),
      child: Icon(
        isUser ? Icons.person_outline : Icons.smart_toy_outlined,
        size: 16,
        color: isUser ? Colors.white : AppColors.primary,
      ),
    );
  }
}

class _BubbleContent extends ConsumerWidget {
  final ChatMessage message;
  final bool isUser;
  const _BubbleContent({required this.message, required this.isUser});

  bool get _shouldShowPlanSummary {
    final plan = message.plan;
    if (!message.hasPlanBinding || plan == null || plan.isEmpty) return false;
    return plan.status == 'completed' &&
        message.state != MessageState.streaming;
  }

  bool get _shouldShowPlanCard {
    final plan = message.plan;
    if (!message.hasPlanBinding || plan == null || plan.isEmpty) return false;
    return plan.status != 'completed' ||
        message.state == MessageState.streaming;
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final settings = ref.watch(settingsProvider);
    final isFailed = message.state == MessageState.failed;
    final hasFiles = message.artifacts.isNotEmpty || message.files.isNotEmpty;
    final showImageProgress =
        !isUser &&
        message.imageGeneration != null &&
        message.state == MessageState.streaming &&
        !hasFiles;
    final showInline =
        !isUser && message.artifacts.isEmpty && message.files.isNotEmpty;
    final showFinalDelivery =
        !isUser &&
        message.state == MessageState.streaming &&
        message.isFinalizingDelivery;
    final allBlocks = _displayBlocks;
    final runningSubagents = <ToolCallInfo>[];
    final contentBlocks = allBlocks
        .where((block) {
          final tool = block.tool;
          final isRunningSubagent =
              block.type == ChatMessageBlockType.tool &&
              tool != null &&
              isSubagentToolCall(tool) &&
              tool.isRunning &&
              (tool.metadata['phase']?.toString().trim() ?? '') == 'running';
          if (isRunningSubagent) {
            runningSubagents.add(tool);
            return false;
          }
          return true;
        })
        .toList(growable: false);
    final hasContentBlocks = contentBlocks.isNotEmpty;
    final hasAnyContentBlocks = allBlocks.isNotEmpty;
    final renderContentBlocks =
        hasContentBlocks && !_isOnlyFailedTextBlock(contentBlocks);
    final showInlineGeneration =
        !isUser &&
        !showFinalDelivery &&
        message.state == MessageState.streaming &&
        message.imageGeneration == null &&
        (message.content.trim().isNotEmpty ||
            message.thinkingContent.trim().isNotEmpty ||
            hasAnyContentBlocks ||
            runningSubagents.isNotEmpty);

    return Container(
      constraints: BoxConstraints(
        maxWidth: MediaQuery.of(context).size.width * 0.72,
      ),
      decoration: BoxDecoration(
        color: isUser
            ? AppColors.primary
            : isFailed
            ? AppColors.statusErrorLight
            : AppColors.surface,
        borderRadius: BorderRadius.only(
          topLeft: const Radius.circular(12),
          topRight: const Radius.circular(12),
          bottomLeft: Radius.circular(isUser ? 12 : 3),
          bottomRight: Radius.circular(isUser ? 3 : 12),
        ),
        border: isUser
            ? null
            : Border.all(
                color: isFailed
                    ? AppColors.statusError.withValues(alpha: 0.3)
                    : AppColors.border,
              ),
        boxShadow: isUser
            ? null
            : const [
                BoxShadow(
                  color: Color(0x061A3A6A),
                  blurRadius: 8,
                  offset: Offset(0, 2),
                ),
              ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // 正文内容
          if (renderContentBlocks)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
              child: _MessageBlockList(
                blocks: contentBlocks,
                isUser: isUser,
                settings: settings,
                messageDone: message.state == MessageState.done,
              ),
            )
          else if (!isFailed &&
              !showFinalDelivery &&
              (!hasFiles && !showImageProgress))
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
              child: message.content.isEmpty
                  ? const SizedBox(height: 6)
                  : MessageRenderer(
                      content: message.content,
                      isUser: isUser,
                      settings: settings,
                    ),
            ),
          if (runningSubagents.isNotEmpty)
            Padding(
              padding: const EdgeInsets.fromLTRB(14, 0, 14, 10),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  for (var i = 0; i < runningSubagents.length; i++)
                    Padding(
                      padding: EdgeInsets.only(
                        bottom: i == runningSubagents.length - 1 ? 0 : 8,
                      ),
                      child: SubagentInvocationCard(
                        tool: runningSubagents[i],
                        settings: settings,
                      ),
                    ),
                ],
              ),
            ),
          if (showFinalDelivery)
            Padding(
              padding: EdgeInsets.fromLTRB(
                14,
                renderContentBlocks || runningSubagents.isNotEmpty ? 0 : 10,
                14,
                12,
              ),
              child: const FinalDeliveryProgress(),
            ),
          if (showInlineGeneration)
            Padding(
              padding: EdgeInsets.fromLTRB(14, 0, 14, 10),
              child: _InlineGenerationProgress(tokenCount: message.tokenCount),
            ),
          if (isFailed)
            Padding(
              padding: EdgeInsets.fromLTRB(
                14,
                renderContentBlocks ? 0 : 10,
                14,
                10,
              ),
              child: _failedMessageText(
                context,
                text: message.content.trim().isEmpty ? '回复失败' : message.content,
                detail: message.errorDetail,
                settings: settings,
              ),
            ),
          if (showImageProgress)
            Padding(
              padding: EdgeInsets.fromLTRB(
                14,
                message.content.isEmpty ? 10 : 2,
                14,
                hasFiles ? 8 : 10,
              ),
              child: _ImageGenerationProgressCard(
                info: message.imageGeneration!,
                statusHint: message.statusHint,
              ),
            ),
          if (!isUser && _shouldShowPlanCard)
            Padding(
              padding: EdgeInsets.fromLTRB(
                14,
                hasContentBlocks ? 2 : 0,
                14,
                hasFiles ? 8 : 10,
              ),
              child: _PlanCard(plan: message.plan!),
            ),
          if (!isUser && _shouldShowPlanSummary)
            Padding(
              padding: EdgeInsets.fromLTRB(
                14,
                hasContentBlocks ? 2 : 0,
                14,
                hasFiles ? 8 : 10,
              ),
              child: _CompletedPlanSummary(plan: message.plan!),
            ),
          if (!isUser && message.artifacts.isNotEmpty)
            Padding(
              padding: const EdgeInsets.fromLTRB(14, 0, 14, 10),
              child: _ArtifactPreview(
                taskId: message.taskId,
                artifacts: message.artifacts,
              ),
            ),
          if (showInline)
            Padding(
              padding: const EdgeInsets.fromLTRB(14, 0, 14, 10),
              child: _InlineFilePreview(files: message.files),
            ),
          // 统计信息 + 复制按钮（仅 agent 已完成消息）
          if (!isUser &&
              message.state == MessageState.done &&
              message.content.isNotEmpty)
            _buildStats(settings),
        ],
      ),
    );
  }

  bool _isOnlyFailedTextBlock(List<ChatMessageBlock> blocks) {
    if (message.state != MessageState.failed) return false;
    final content = message.content.trim();
    if (content.isEmpty || blocks.length != 1) return false;
    final block = blocks.first;
    return block.type == ChatMessageBlockType.text &&
        block.text.trim() == content;
  }

  Widget _failedMessageText(
    BuildContext context, {
    required String text,
    required String detail,
    required AppSettings settings,
  }) {
    final cleanDetail = detail.trim();
    final body = SelectableText(
      text,
      style: const TextStyle(
        fontSize: 14,
        height: 1.55,
        color: AppColors.statusError,
      ),
    );
    if (cleanDetail.isEmpty) return body;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Expanded(child: body),
        const SizedBox(width: 8),
        Tooltip(
          message: '查看错误详情',
          child: IconButton(
            constraints: const BoxConstraints.tightFor(width: 28, height: 28),
            padding: EdgeInsets.zero,
            iconSize: 18,
            color: AppColors.statusError,
            onPressed: () => _showErrorDetailDialog(
              context,
              cleanDetail,
              settings: settings,
            ),
            icon: const Icon(Icons.info_outline_rounded),
          ),
        ),
      ],
    );
  }

  Future<void> _showErrorDetailDialog(
    BuildContext context,
    String detail, {
    required AppSettings settings,
  }) {
    return showDialog<void>(
      context: context,
      builder: (dialogContext) {
        final mediaQuery = MediaQuery.of(dialogContext);
        final isMobile = AppBreakpoints.isMobile(dialogContext);
        final maxHeight = mediaQuery.size.height * (isMobile ? 0.86 : 0.78);
        return Dialog(
          insetPadding: EdgeInsets.symmetric(
            horizontal: isMobile ? 12 : 32,
            vertical: isMobile ? 16 : 32,
          ),
          clipBehavior: Clip.antiAlias,
          child: ConstrainedBox(
            constraints: BoxConstraints(maxWidth: 720, maxHeight: maxHeight),
            child: SafeArea(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Padding(
                    padding: EdgeInsets.fromLTRB(isMobile ? 16 : 20, 16, 8, 14),
                    child: Row(
                      children: [
                        Container(
                          width: 36,
                          height: 36,
                          decoration: BoxDecoration(
                            color: AppColors.statusErrorLight,
                            borderRadius: AppRadius.smRadius,
                          ),
                          child: const Icon(
                            Icons.error_outline_rounded,
                            color: AppColors.statusError,
                            size: 20,
                          ),
                        ),
                        const SizedBox(width: 10),
                        const Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                '错误详情',
                                style: TextStyle(
                                  fontSize: 16,
                                  fontWeight: FontWeight.w700,
                                  color: AppColors.textPrimary,
                                ),
                              ),
                              SizedBox(height: 3),
                              Text(
                                '任务执行返回的信息',
                                style: TextStyle(
                                  fontSize: 12,
                                  color: AppColors.textMuted,
                                ),
                              ),
                            ],
                          ),
                        ),
                        IconButton(
                          tooltip: '关闭',
                          onPressed: () => Navigator.of(dialogContext).pop(),
                          icon: const Icon(Icons.close_rounded),
                        ),
                      ],
                    ),
                  ),
                  const Divider(height: 1),
                  Expanded(
                    child: SingleChildScrollView(
                      padding: EdgeInsets.all(isMobile ? 14 : 20),
                      child: Container(
                        width: double.infinity,
                        padding: EdgeInsets.all(isMobile ? 12 : 16),
                        decoration: BoxDecoration(
                          color: AppColors.inputBackground,
                          borderRadius: AppRadius.smRadius,
                          border: Border.all(color: AppColors.borderLight),
                        ),
                        child: MessageRenderer(
                          content: detail,
                          isUser: false,
                          settings: settings.copyWith(markdownRender: true),
                        ),
                      ),
                    ),
                  ),
                  const Divider(height: 1),
                  Padding(
                    padding: EdgeInsets.fromLTRB(
                      isMobile ? 14 : 20,
                      10,
                      isMobile ? 14 : 20,
                      10,
                    ),
                    child: Row(
                      mainAxisAlignment: MainAxisAlignment.end,
                      children: [
                        TextButton.icon(
                          onPressed: () =>
                              Clipboard.setData(ClipboardData(text: detail)),
                          icon: const Icon(Icons.copy_outlined, size: 17),
                          label: const Text('复制原文'),
                        ),
                        const SizedBox(width: 8),
                        FilledButton(
                          onPressed: () => Navigator.of(dialogContext).pop(),
                          child: const Text('关闭'),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }

  List<ChatMessageBlock> get _displayBlocks {
    if (isUser) {
      if (message.content.isEmpty) return const [];
      return [
        ChatMessageBlock.text(id: '${message.id}_text', text: message.content),
      ];
    }
    if (message.blocks.isNotEmpty) {
      final hasText = message.blocks.any(
        (block) =>
            block.type == ChatMessageBlockType.text &&
            block.text.trim().isNotEmpty,
      );
      if (!hasText && message.content.trim().isNotEmpty) {
        return [
          ...message.blocks,
          ChatMessageBlock.text(
            id: '${message.id}_text',
            text: message.content,
          ),
        ];
      }
      return message.blocks;
    }
    if (message.content.isEmpty && message.toolCalls.isEmpty) return const [];
    return [
      for (final tool in message.toolCalls)
        ChatMessageBlock.tool(
          id: '${message.id}_tool_${tool.stableKey}',
          tool: tool,
        ),
      if (message.content.isNotEmpty)
        ChatMessageBlock.text(id: '${message.id}_text', text: message.content),
    ];
  }

  Widget _buildStats(AppSettings settings) {
    final parts = <String>[];
    if (settings.showFirstTokenTime && message.firstTokenTime != null) {
      final ms = message.firstTokenTime!.inMilliseconds;
      parts.add(
        '首字 ${ms < 1000 ? "${ms}ms" : "${(ms / 1000).toStringAsFixed(1)}s"}',
      );
    }
    if (settings.showTokenCount && message.tokenCount != null) {
      parts.add('${message.tokenCount} tokens');
    }
    if (settings.showWordCount && message.content.isNotEmpty) {
      parts.add('${message.content.length} 字');
    }
    return Container(
      padding: const EdgeInsets.fromLTRB(14, 0, 14, 8),
      child: Row(
        children: [
          if (parts.isNotEmpty)
            Expanded(
              child: Wrap(
                spacing: 10,
                children: parts
                    .map(
                      (p) => Text(
                        p,
                        style: const TextStyle(
                          fontSize: 11,
                          color: AppColors.textMuted,
                        ),
                      ),
                    )
                    .toList(),
              ),
            )
          else
            const Spacer(),
          _CopyButton(content: message.content),
        ],
      ),
    );
  }
}

class FinalDeliveryProgress extends StatelessWidget {
  const FinalDeliveryProgress({super.key});

  @override
  Widget build(BuildContext context) {
    return Semantics(
      liveRegion: true,
      label: '正在完成最终交付，检查文件完整性与下载状态',
      child: Container(
        key: const ValueKey('final-delivery-progress'),
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: AppColors.primaryLight.withValues(alpha: 0.72),
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: AppColors.primary.withValues(alpha: 0.14)),
        ),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SizedBox(
              width: 34,
              height: 34,
              child: Stack(
                alignment: Alignment.center,
                children: [
                  Container(
                    width: 34,
                    height: 34,
                    decoration: BoxDecoration(
                      color: AppColors.surface,
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: const Icon(
                      Icons.inventory_2_outlined,
                      size: 18,
                      color: AppColors.primary,
                    ),
                  ),
                  const Positioned(
                    right: 0,
                    bottom: 0,
                    child: SizedBox(
                      width: 10,
                      height: 10,
                      child: CircularProgressIndicator(
                        strokeWidth: 1.8,
                        color: AppColors.primary,
                        backgroundColor: AppColors.surface,
                      ),
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(width: 10),
            const Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    '正在完成最终交付',
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 13,
                      fontWeight: FontWeight.w600,
                      color: AppColors.textPrimary,
                    ),
                  ),
                  SizedBox(height: 3),
                  Text(
                    '检查文件完整性与下载状态',
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 11,
                      height: 1.35,
                      color: AppColors.textSecondary,
                    ),
                  ),
                  SizedBox(height: 9),
                  ClipRRect(
                    borderRadius: BorderRadius.all(Radius.circular(2)),
                    child: LinearProgressIndicator(
                      minHeight: 3,
                      color: AppColors.primary,
                      backgroundColor: AppColors.primaryMuted,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _InlineGenerationProgress extends StatefulWidget {
  final int? tokenCount;

  const _InlineGenerationProgress({this.tokenCount});

  @override
  State<_InlineGenerationProgress> createState() =>
      _InlineGenerationProgressState();
}

class _InlineGenerationProgressState extends State<_InlineGenerationProgress>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 2800),
    )..repeat();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: _controller,
      builder: (context, _) => Container(
        height: 52,
        padding: const EdgeInsets.symmetric(horizontal: 11),
        decoration: BoxDecoration(
          color: AppColors.primaryLight.withValues(alpha: .46),
          borderRadius: AppRadius.smRadius,
          border: Border.all(color: AppColors.primary.withValues(alpha: .14)),
        ),
        child: Row(
          children: [
            const Text(
              '正在生成',
              style: TextStyle(
                fontSize: 12,
                fontWeight: FontWeight.w700,
                color: AppColors.primary,
              ),
            ),
            if (widget.tokenCount != null) ...[
              const SizedBox(width: 6),
              Text(
                '${widget.tokenCount} tokens',
                style: const TextStyle(
                  fontSize: 10,
                  fontFeatures: [FontFeature.tabularFigures()],
                  color: AppColors.textMuted,
                ),
              ),
            ],
            const SizedBox(width: 12),
            Expanded(
              child: RepaintBoundary(
                child: SizedBox(
                  height: 36,
                  child: CustomPaint(
                    painter: _GenerationMeteorPainter(
                      progress: _controller.value,
                    ),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _GenerationMeteorPainter extends CustomPainter {
  final double progress;

  static final TextPainter _robotGlyph = TextPainter(
    text: TextSpan(
      text: String.fromCharCode(Icons.smart_toy_outlined.codePoint),
      style: TextStyle(
        color: Color(0xFF0EA5E9),
        fontFamily: Icons.smart_toy_outlined.fontFamily,
        package: Icons.smart_toy_outlined.fontPackage,
        fontSize: 20,
      ),
    ),
    textDirection: TextDirection.ltr,
  )..layout();

  const _GenerationMeteorPainter({required this.progress});

  @override
  void paint(Canvas canvas, Size size) {
    final centerY = size.height / 2;
    final clip = RRect.fromRectAndRadius(
      Offset.zero & size,
      const Radius.circular(4),
    );
    canvas.save();
    canvas.clipRRect(clip);

    // A small deterministic emitter releases one fragment every frame slice.
    // The core moves faster, while older fragments shrink and fade behind it.
    final headX = -48 + (size.width + 96) * progress;
    const particles =
        <
          ({
            double behind,
            double vertical,
            double size,
            double opacity,
            Color color,
          })
        >[
          (
            behind: 8,
            vertical: 0,
            size: 6,
            opacity: .90,
            color: Color(0xFF06B6D4),
          ),
          (
            behind: 10,
            vertical: -4,
            size: 4,
            opacity: .78,
            color: Color(0xFF38BDF8),
          ),
          (
            behind: 10,
            vertical: 4,
            size: 4,
            opacity: .78,
            color: Color(0xFF14B8A6),
          ),
          (
            behind: 15,
            vertical: 0,
            size: 5,
            opacity: .70,
            color: Color(0xFF14B8A6),
          ),
          (
            behind: 17,
            vertical: -5,
            size: 3,
            opacity: .58,
            color: Color(0xFF38BDF8),
          ),
          (
            behind: 17,
            vertical: 5,
            size: 3,
            opacity: .58,
            color: Color(0xFF06B6D4),
          ),
          (
            behind: 21,
            vertical: -2,
            size: 4,
            opacity: .53,
            color: Color(0xFF0EA5E9),
          ),
          (
            behind: 22,
            vertical: 3,
            size: 3,
            opacity: .49,
            color: Color(0xFF14B8A6),
          ),
          (
            behind: 26,
            vertical: 0,
            size: 3.5,
            opacity: .43,
            color: Color(0xFF2563EB),
          ),
          (
            behind: 28,
            vertical: -4,
            size: 2,
            opacity: .34,
            color: Color(0xFFFBBF24),
          ),
          (
            behind: 29,
            vertical: 4,
            size: 2,
            opacity: .34,
            color: Color(0xFF38BDF8),
          ),
          (
            behind: 32,
            vertical: -1,
            size: 2.5,
            opacity: .28,
            color: Color(0xFF14B8A6),
          ),
          (
            behind: 35,
            vertical: 2,
            size: 1.5,
            opacity: .22,
            color: Color(0xFFFBBF24),
          ),
          (
            behind: 37,
            vertical: -3,
            size: 1.5,
            opacity: .18,
            color: Color(0xFF60A5FA),
          ),
        ];
    for (var i = particles.length - 1; i >= 0; i--) {
      final age = progress - i * .023;
      if (age < 0 || age > .50) continue;
      final life = age / .50;
      final distanceBehind = 8 + life * 52;
      final spread = .45 + life * 2.1;
      final vertical = math.sin(i * 2.1) * spread;
      final particleSize = 4.2 - life * 2.5 + (i % 2) * .25;
      final opacity = math.pow(1 - life, 1.65).toDouble() * .82;
      final color = switch (i % 5) {
        0 => const Color(0xFF14B8A6),
        1 => const Color(0xFF06B6D4),
        2 => const Color(0xFF38BDF8),
        3 => const Color(0xFF60A5FA),
        _ => life > .72 ? const Color(0xFFFBBF24) : const Color(0xFF60A5FA),
      };
      canvas.drawRect(
        Rect.fromLTWH(
          headX - distanceBehind,
          centerY + vertical - particleSize / 2,
          particleSize,
          particleSize,
        ),
        Paint()
          ..color = color.withValues(alpha: opacity)
          ..isAntiAlias = false,
      );
    }

    _robotGlyph.paint(
      canvas,
      Offset(headX - _robotGlyph.width / 2, centerY - _robotGlyph.height / 2),
    );
    canvas.restore();
  }

  @override
  bool shouldRepaint(covariant _GenerationMeteorPainter oldDelegate) {
    return oldDelegate.progress != progress;
  }
}

class _TypingIndicator extends StatefulWidget {
  @override
  State<_TypingIndicator> createState() => _TypingIndicatorState();
}

class _TypingIndicatorState extends State<_TypingIndicator>
    with SingleTickerProviderStateMixin {
  late AnimationController _ctrl;

  @override
  void initState() {
    super.initState();
    _ctrl = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 900),
    )..repeat();
  }

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: _ctrl,
      builder: (context, _) {
        return Row(
          mainAxisSize: MainAxisSize.min,
          children: List.generate(3, (i) {
            final delay = i * 0.3;
            final t = (_ctrl.value - delay).clamp(0.0, 1.0);
            final opacity = (t < 0.5 ? t * 2 : (1 - t) * 2).clamp(0.3, 1.0);
            return Container(
              width: 6,
              height: 6,
              margin: const EdgeInsets.only(right: 3),
              decoration: BoxDecoration(
                color: AppColors.primary.withValues(alpha: opacity),
                shape: BoxShape.circle,
              ),
            );
          }),
        );
      },
    );
  }
}

class _PlanCard extends StatelessWidget {
  final PlanInfo plan;

  const _PlanCard({required this.plan});

  Color _statusColor(String status) {
    switch (status) {
      case 'completed':
        return AppColors.statusSuccess;
      case 'in_progress':
        return AppColors.statusWarning;
      default:
        return AppColors.textMuted;
    }
  }

  Color _statusBg(String status) {
    switch (status) {
      case 'completed':
        return AppColors.statusSuccessLight;
      case 'in_progress':
        return AppColors.statusWarningLight;
      default:
        return AppColors.primaryLight;
    }
  }

  IconData _itemIcon(String status) {
    switch (status) {
      case 'completed':
        return Icons.check_circle_rounded;
      case 'in_progress':
        return Icons.timelapse_rounded;
      default:
        return Icons.radio_button_unchecked_rounded;
    }
  }

  @override
  Widget build(BuildContext context) {
    final mode = plan.mode.trim().toLowerCase();
    final modeLabel = mode == 'plan'
        ? 'Plan'
        : mode == 'build'
        ? 'Build'
        : '计划';
    final color = _statusColor(plan.status);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: AppColors.surfaceElevated,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.borderLight),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 30,
                height: 30,
                decoration: BoxDecoration(
                  color: AppColors.primaryLight,
                  borderRadius: AppRadius.smRadius,
                ),
                child: const Icon(
                  Icons.checklist_rounded,
                  size: 18,
                  color: AppColors.primary,
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      plan.title.isEmpty ? '执行计划' : plan.title,
                      style: const TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w700,
                        color: AppColors.textPrimary,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Wrap(
                      spacing: 6,
                      runSpacing: 6,
                      children: [
                        _PlanBadge(
                          label: modeLabel,
                          color: AppColors.primary,
                          background: AppColors.primaryLight,
                        ),
                        _PlanBadge(
                          label: _planStatusText(plan.status),
                          color: color,
                          background: _statusBg(plan.status),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          ...plan.items.map(
            (item) => Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Padding(
                    padding: const EdgeInsets.only(top: 1),
                    child: Icon(
                      _itemIcon(item.status),
                      size: 18,
                      color: _statusColor(item.status),
                    ),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      item.text,
                      style: TextStyle(
                        fontSize: 13,
                        height: 1.45,
                        color: item.status == 'completed'
                            ? AppColors.textSecondary
                            : AppColors.textPrimary,
                        decoration: item.status == 'completed'
                            ? TextDecoration.lineThrough
                            : TextDecoration.none,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  String _planStatusText(String status) {
    switch (status) {
      case 'completed':
        return '已完成';
      case 'in_progress':
        return '执行中';
      default:
        return '待开始';
    }
  }
}

class _CompletedPlanSummary extends StatelessWidget {
  final PlanInfo plan;

  const _CompletedPlanSummary({required this.plan});

  @override
  Widget build(BuildContext context) {
    final total = plan.items.length;
    final completed = plan.items
        .where((item) => item.status == 'completed')
        .length;

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      decoration: BoxDecoration(
        color: AppColors.statusSuccessLight,
        borderRadius: AppRadius.smRadius,
        border: Border.all(
          color: AppColors.statusSuccess.withValues(alpha: 0.18),
        ),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Padding(
            padding: EdgeInsets.only(top: 1),
            child: Icon(
              Icons.check_circle_rounded,
              size: 16,
              color: AppColors.statusSuccess,
            ),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  plan.title.isEmpty ? '执行计划' : plan.title,
                  style: const TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w700,
                    color: AppColors.statusSuccess,
                  ),
                ),
                const SizedBox(height: 2),
                Text(
                  total > 0 ? '已完成 $completed/$total 项，计划已收起' : '计划已完成',
                  style: const TextStyle(
                    fontSize: 12,
                    height: 1.45,
                    color: AppColors.textSecondary,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _PlanBadge extends StatelessWidget {
  final String label;
  final Color color;
  final Color background;

  const _PlanBadge({
    required this.label,
    required this.color,
    required this.background,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w600,
          color: color,
        ),
      ),
    );
  }
}

class _MessageBlockList extends StatelessWidget {
  final List<ChatMessageBlock> blocks;
  final bool isUser;
  final AppSettings settings;
  final bool messageDone;

  const _MessageBlockList({
    required this.blocks,
    required this.isUser,
    required this.settings,
    this.messageDone = false,
  });

  List<ToolCallInfo> _toolsForDisplay(List<ToolCallInfo> tools) {
    if (!messageDone || !tools.any((tool) => tool.isRunning)) return tools;
    return [
      for (final tool in tools)
        if (tool.isRunning) tool.copyWith(status: 'completed') else tool,
    ];
  }

  @override
  Widget build(BuildContext context) {
    final entries = <Widget>[];
    for (var i = 0; i < blocks.length; i++) {
      final block = blocks[i];
      if (block.type == ChatMessageBlockType.text) {
        if (block.text.trim().isEmpty) continue;
        entries.add(
          MessageRenderer(
            content: block.text,
            isUser: isUser,
            settings: settings,
          ),
        );
        continue;
      }
      final tools = <ToolCallInfo>[];
      var cursor = i;
      while (cursor < blocks.length &&
          blocks[cursor].type == ChatMessageBlockType.tool) {
        final tool = blocks[cursor].tool;
        if (tool != null) tools.add(tool);
        cursor += 1;
      }
      if (tools.isNotEmpty) {
        final visibleTools = _toolsForDisplay(tools);
        if (visibleTools.any(isSubagentToolCall)) {
          for (final tool in visibleTools) {
            entries.add(
              isSubagentToolCall(tool)
                  ? SubagentInvocationCard(tool: tool, settings: settings)
                  : _ToolCallGroup(tools: [tool]),
            );
          }
        } else {
          entries.add(_ToolCallGroup(tools: visibleTools));
        }
      }
      i = cursor - 1;
    }
    if (entries.isEmpty) return const SizedBox.shrink();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (var i = 0; i < entries.length; i++)
          Padding(
            padding: EdgeInsets.only(bottom: i == entries.length - 1 ? 0 : 10),
            child: entries[i],
          ),
      ],
    );
  }
}

class _ToolCallGroup extends StatefulWidget {
  final List<ToolCallInfo> tools;

  const _ToolCallGroup({required this.tools});

  @override
  State<_ToolCallGroup> createState() => _ToolCallGroupState();
}

class _ToolCallGroupState extends State<_ToolCallGroup> {
  bool _expanded = false;

  bool get _hasRunning => widget.tools.any((tool) => tool.isRunning);
  bool get _hasError => widget.tools.any((tool) => tool.isError);
  int get _runningCount => widget.tools.where((tool) => tool.isRunning).length;
  int get _errorCount => widget.tools.where((tool) => tool.isError).length;
  int get _successCount =>
      widget.tools.where((tool) => tool.isCompleted).length;

  Color get _statusColor {
    if (_hasError) return AppColors.statusError;
    if (_hasRunning) return AppColors.primary;
    return AppColors.statusSuccess;
  }

  Color get _statusBackground {
    if (_hasError) return AppColors.statusErrorLight;
    if (_hasRunning) return AppColors.primaryLight;
    return AppColors.statusSuccessLight;
  }

  IconData get _statusIcon {
    if (_hasError) return Icons.error_outline_rounded;
    if (_hasRunning) return Icons.sync_rounded;
    return Icons.check_circle_rounded;
  }

  String get _title {
    if (widget.tools.length == 1) {
      final tool = widget.tools.single;
      final name = displayToolName(tool.tool);
      if (tool.isError) return '调用失败 $name';
      if (tool.isCompleted) return '已调用 $name';
      return '正在调用 $name';
    }
    if (_hasError) {
      final parts = <String>[
        '${widget.tools.length} 个工具调用',
        '$_errorCount 个失败',
      ];
      if (_successCount > 0) parts.add('$_successCount 个成功');
      if (_runningCount > 0) parts.add('$_runningCount 个进行中');
      return parts.join('，');
    }
    if (_hasRunning) return '正在调用 ${widget.tools.length} 个工具';
    return '已调用 ${widget.tools.length} 个工具';
  }

  String get _subtitle {
    final names = widget.tools
        .map((tool) => displayToolName(tool.tool))
        .toList(growable: false);
    if (names.isEmpty) return '';
    return names.take(4).join('、') + (names.length > 4 ? ' 等' : '');
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      decoration: BoxDecoration(
        color: AppColors.surfaceElevated,
        borderRadius: AppRadius.smRadius,
        border: Border.all(
          color: _hasError
              ? AppColors.statusError.withValues(alpha: 0.25)
              : AppColors.borderLight,
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          InkWell(
            onTap: () => setState(() => _expanded = !_expanded),
            borderRadius: AppRadius.smRadius,
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
              child: Row(
                children: [
                  Container(
                    width: 24,
                    height: 24,
                    decoration: BoxDecoration(
                      color: _statusBackground,
                      borderRadius: AppRadius.smRadius,
                    ),
                    child: _hasRunning
                        ? Padding(
                            padding: const EdgeInsets.all(6),
                            child: CircularProgressIndicator(
                              strokeWidth: 1.7,
                              color: _statusColor,
                            ),
                          )
                        : Icon(_statusIcon, size: 15, color: _statusColor),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          _title,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontSize: 13,
                            fontWeight: FontWeight.w700,
                            color: AppColors.textPrimary,
                          ),
                        ),
                        if (_subtitle.isNotEmpty) ...[
                          const SizedBox(height: 2),
                          Text(
                            _subtitle,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 12,
                              color: AppColors.textSecondary,
                            ),
                          ),
                        ],
                      ],
                    ),
                  ),
                  Icon(
                    _expanded
                        ? Icons.expand_less_rounded
                        : Icons.expand_more_rounded,
                    size: 18,
                    color: AppColors.textMuted,
                  ),
                ],
              ),
            ),
          ),
          if (_expanded) ...[
            const Divider(height: 1, color: AppColors.borderLight),
            Padding(
              padding: const EdgeInsets.fromLTRB(10, 8, 10, 10),
              child: Column(
                children: [
                  for (var i = 0; i < widget.tools.length; i++)
                    Padding(
                      padding: EdgeInsets.only(
                        bottom: i == widget.tools.length - 1 ? 0 : 8,
                      ),
                      child: _ToolCallCard(tool: widget.tools[i]),
                    ),
                ],
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class _ToolCallCard extends StatefulWidget {
  final ToolCallInfo tool;

  const _ToolCallCard({required this.tool});

  @override
  State<_ToolCallCard> createState() => _ToolCallCardState();
}

class _ToolCallCardState extends State<_ToolCallCard> {
  bool _expanded = false;

  Color get _statusColor {
    if (widget.tool.isError) return AppColors.statusError;
    if (widget.tool.isCompleted) return AppColors.statusSuccess;
    return AppColors.primary;
  }

  Color get _statusBackground {
    if (widget.tool.isError) return AppColors.statusErrorLight;
    if (widget.tool.isCompleted) return AppColors.statusSuccessLight;
    return AppColors.primaryLight;
  }

  IconData get _statusIcon {
    if (widget.tool.isError) return Icons.error_outline_rounded;
    if (widget.tool.isCompleted) return Icons.check_circle_rounded;
    return Icons.sync_rounded;
  }

  String get _statusText {
    if (widget.tool.isError) return '调用失败';
    if (widget.tool.isCompleted) return '已调用';
    return '正在调用';
  }

  String get _subtitle {
    final title = displayToolTitle(widget.tool);
    if (title.isNotEmpty) return title;
    for (final key in const [
      'description',
      'query',
      'url',
      'filePath',
      'path',
      'pattern',
      'name',
      'command',
    ]) {
      final value = widget.tool.input[key];
      if (value is String && value.trim().isNotEmpty) return value.trim();
    }
    return '';
  }

  String get _inputPreview {
    if (widget.tool.tool == 'task_status') return '';
    if (widget.tool.input.isEmpty) return '';
    const encoder = JsonEncoder.withIndent('  ');
    return encoder.convert(widget.tool.input);
  }

  String get _outputText {
    if (widget.tool.isError) return widget.tool.error.trim();
    final output = widget.tool.output.trim();
    if (output.isEmpty) return '';
    if (!widget.tool.outputTruncated) return output;
    return '$output\n\n输出已截断，完整内容保留在工具执行上下文中。';
  }

  bool get _canExpand =>
      !widget.tool.isRunning &&
      (_inputPreview.isNotEmpty ||
          _outputText.isNotEmpty ||
          widget.tool.attachments.isNotEmpty);

  @override
  Widget build(BuildContext context) {
    final toolName = displayToolName(widget.tool.tool);
    return Container(
      width: double.infinity,
      decoration: BoxDecoration(
        color: AppColors.surfaceElevated,
        borderRadius: AppRadius.smRadius,
        border: Border.all(
          color: widget.tool.isError
              ? AppColors.statusError.withValues(alpha: 0.25)
              : AppColors.borderLight,
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          InkWell(
            onTap: _canExpand
                ? () => setState(() => _expanded = !_expanded)
                : null,
            borderRadius: AppRadius.smRadius,
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
              child: Row(
                children: [
                  Container(
                    width: 24,
                    height: 24,
                    decoration: BoxDecoration(
                      color: _statusBackground,
                      borderRadius: AppRadius.smRadius,
                    ),
                    child: widget.tool.isRunning
                        ? Padding(
                            padding: const EdgeInsets.all(6),
                            child: CircularProgressIndicator(
                              strokeWidth: 1.7,
                              color: _statusColor,
                            ),
                          )
                        : Icon(_statusIcon, size: 15, color: _statusColor),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Flexible(
                              child: Text(
                                '$_statusText $toolName',
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(
                                  fontSize: 13,
                                  fontWeight: FontWeight.w700,
                                  color: AppColors.textPrimary,
                                ),
                              ),
                            ),
                          ],
                        ),
                        if (_subtitle.isNotEmpty) ...[
                          const SizedBox(height: 2),
                          Text(
                            _subtitle,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 12,
                              color: AppColors.textSecondary,
                            ),
                          ),
                        ],
                      ],
                    ),
                  ),
                  if (_canExpand)
                    Icon(
                      _expanded
                          ? Icons.expand_less_rounded
                          : Icons.expand_more_rounded,
                      size: 18,
                      color: AppColors.textMuted,
                    ),
                ],
              ),
            ),
          ),
          if (_expanded) ...[
            const Divider(height: 1, color: AppColors.borderLight),
            Padding(
              padding: const EdgeInsets.fromLTRB(10, 8, 10, 10),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (_inputPreview.isNotEmpty)
                    _ToolDetailBlock(title: '输入', content: _inputPreview),
                  if (_outputText.isNotEmpty)
                    Padding(
                      padding: EdgeInsets.only(
                        top: _inputPreview.isEmpty ? 0 : 8,
                      ),
                      child: _ToolDetailBlock(
                        title: widget.tool.isError ? '错误' : '输出',
                        content: _outputText,
                        error: widget.tool.isError,
                      ),
                    ),
                  if (widget.tool.attachments.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(top: 8),
                      child: _InlineFilePreview(files: widget.tool.attachments),
                    ),
                ],
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class _ToolDetailBlock extends StatelessWidget {
  final String title;
  final String content;
  final bool error;

  const _ToolDetailBlock({
    required this.title,
    required this.content,
    this.error = false,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          title,
          style: TextStyle(
            fontSize: 11,
            fontWeight: FontWeight.w700,
            color: error ? AppColors.statusError : AppColors.textMuted,
          ),
        ),
        const SizedBox(height: 5),
        Container(
          width: double.infinity,
          constraints: const BoxConstraints(maxHeight: 220),
          padding: const EdgeInsets.all(9),
          decoration: BoxDecoration(
            color: AppColors.inputBackground,
            borderRadius: AppRadius.smRadius,
            border: Border.all(color: AppColors.borderLight),
          ),
          child: SingleChildScrollView(
            child: SelectableText(
              content,
              style: TextStyle(
                fontSize: 12,
                height: 1.45,
                color: error ? AppColors.statusError : AppColors.textSecondary,
                fontFamily: 'monospace',
              ),
            ),
          ),
        ),
      ],
    );
  }
}

// 审批条
class _ApprovalBanner extends StatefulWidget {
  final ApprovalInfo approval;
  final void Function(String reply) onApprove;

  const _ApprovalBanner({required this.approval, required this.onApprove});

  @override
  State<_ApprovalBanner> createState() => _ApprovalBannerState();
}

class _ApprovalBannerState extends State<_ApprovalBanner>
    with SingleTickerProviderStateMixin {
  late AnimationController _ctrl;
  late Animation<Offset> _slide;

  @override
  void initState() {
    super.initState();
    _ctrl = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 280),
    )..forward();
    _slide = Tween<Offset>(
      begin: const Offset(0, 0.3),
      end: Offset.zero,
    ).animate(CurvedAnimation(parent: _ctrl, curve: Curves.easeOut));
  }

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final approval = widget.approval;
    return SlideTransition(
      position: _slide,
      child: FadeTransition(
        opacity: _ctrl,
        child: Container(
          margin: const EdgeInsets.fromLTRB(12, 0, 12, 8),
          padding: const EdgeInsets.all(14),
          decoration: BoxDecoration(
            color: AppColors.statusWarningLight,
            borderRadius: AppRadius.mdRadius,
            border: Border.all(
              color: AppColors.statusWarning.withValues(alpha: 0.4),
            ),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  const Icon(
                    Icons.lock_outline,
                    size: 16,
                    color: AppColors.statusWarning,
                  ),
                  const SizedBox(width: 6),
                  const Text(
                    '需要审批',
                    style: TextStyle(
                      fontWeight: FontWeight.w600,
                      fontSize: 13,
                      color: AppColors.statusWarning,
                    ),
                  ),
                  const Spacer(),
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 7,
                      vertical: 2,
                    ),
                    decoration: BoxDecoration(
                      color: AppColors.statusWarning.withValues(alpha: 0.12),
                      borderRadius: AppRadius.smRadius,
                    ),
                    child: Text(
                      approval.permission,
                      style: const TextStyle(
                        fontSize: 11,
                        color: AppColors.statusWarning,
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                  ),
                ],
              ),
              if (approval.patterns.isNotEmpty) ...[
                const SizedBox(height: 8),
                Wrap(
                  spacing: 6,
                  runSpacing: 4,
                  children: approval.patterns.map((p) {
                    return Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 7,
                        vertical: 3,
                      ),
                      decoration: BoxDecoration(
                        color: Colors.white,
                        borderRadius: AppRadius.smRadius,
                        border: Border.all(color: AppColors.border),
                      ),
                      child: Text(
                        p,
                        style: const TextStyle(
                          fontSize: 11,
                          fontFamily: 'monospace',
                          color: AppColors.textSecondary,
                        ),
                      ),
                    );
                  }).toList(),
                ),
              ],
              const SizedBox(height: 12),
              Row(
                children: [
                  AppButton(
                    label: '批准一次',
                    onPressed: () => widget.onApprove('once'),
                  ),
                  const SizedBox(width: 8),
                  AppButton(
                    label: '全部批准',
                    outlined: true,
                    onPressed: () => widget.onApprove('always'),
                  ),
                  const SizedBox(width: 8),
                  AppButton(
                    label: '拒绝',
                    outlined: true,
                    onPressed: () => widget.onApprove('reject'),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class QuestionBanner extends StatefulWidget {
  final QuestionRequestInfo question;
  final void Function(String requestId, List<List<String>> answers) onSubmit;
  final void Function(String requestId) onReject;

  const QuestionBanner({
    super.key,
    required this.question,
    required this.onSubmit,
    required this.onReject,
  });

  @override
  State<QuestionBanner> createState() => _QuestionBannerState();
}

class _QuestionBannerState extends State<QuestionBanner>
    with SingleTickerProviderStateMixin {
  late AnimationController _ctrl;
  late Animation<Offset> _slide;
  late List<List<String>> _answers;
  late List<TextEditingController> _customAnswerControllers;

  @override
  void initState() {
    super.initState();
    _ctrl = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 280),
    )..forward();
    _slide = Tween<Offset>(
      begin: const Offset(0, 0.3),
      end: Offset.zero,
    ).animate(CurvedAnimation(parent: _ctrl, curve: Curves.easeOut));
    _answers = List.generate(
      widget.question.questions.length,
      (_) => <String>[],
    );
    _customAnswerControllers = _createCustomAnswerControllers();
  }

  @override
  void didUpdateWidget(covariant QuestionBanner oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.question.requestId != widget.question.requestId ||
        oldWidget.question.questions.length !=
            widget.question.questions.length) {
      for (final controller in _customAnswerControllers) {
        controller.dispose();
      }
      _answers = List.generate(
        widget.question.questions.length,
        (_) => <String>[],
      );
      _customAnswerControllers = _createCustomAnswerControllers();
      _ctrl.forward(from: 0);
    }
  }

  @override
  void dispose() {
    for (final controller in _customAnswerControllers) {
      controller.dispose();
    }
    _ctrl.dispose();
    super.dispose();
  }

  List<TextEditingController> _createCustomAnswerControllers() {
    return List.generate(widget.question.questions.length, (index) {
      final controller = TextEditingController();
      controller.addListener(() {
        if (!mounted) return;
        final item = widget.question.questions[index];
        if (!item.multiple && controller.text.trim().isNotEmpty) {
          _answers[index] = <String>[];
        }
        setState(() {});
      });
      return controller;
    });
  }

  List<List<String>> get _submittedAnswers {
    return List.generate(widget.question.questions.length, (index) {
      final item = widget.question.questions[index];
      final custom = item.custom
          ? _customAnswerControllers[index].text.trim()
          : '';
      if (!item.multiple) {
        return custom.isNotEmpty ? <String>[custom] : _answers[index];
      }
      final answer = List<String>.from(_answers[index]);
      if (custom.isNotEmpty && !answer.contains(custom)) answer.add(custom);
      return answer;
    });
  }

  bool get _canSubmit {
    if (widget.question.questions.isEmpty) return false;
    return _submittedAnswers.every((answer) => answer.isNotEmpty);
  }

  void _toggleMultiple(int index, String label, bool checked) {
    setState(() {
      final next = List<String>.from(_answers[index]);
      if (checked) {
        if (!next.contains(label)) next.add(label);
      } else {
        next.remove(label);
      }
      _answers[index] = next;
    });
  }

  void _selectSingle(int index, String label) {
    _customAnswerControllers[index].clear();
    setState(() {
      _answers[index] = [label];
    });
  }

  @override
  Widget build(BuildContext context) {
    final question = widget.question;
    return SlideTransition(
      position: _slide,
      child: FadeTransition(
        opacity: _ctrl,
        child: Container(
          margin: const EdgeInsets.fromLTRB(12, 0, 12, 8),
          padding: const EdgeInsets.all(14),
          decoration: BoxDecoration(
            color: AppColors.surface,
            borderRadius: AppRadius.mdRadius,
            border: Border.all(color: AppColors.primary.withValues(alpha: 0.2)),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  const Icon(
                    Icons.quiz_outlined,
                    size: 16,
                    color: AppColors.primary,
                  ),
                  const SizedBox(width: 6),
                  const Text(
                    '需要选择',
                    style: TextStyle(
                      fontWeight: FontWeight.w600,
                      fontSize: 13,
                      color: AppColors.primary,
                    ),
                  ),
                  const Spacer(),
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 7,
                      vertical: 2,
                    ),
                    decoration: BoxDecoration(
                      color: AppColors.primary.withValues(alpha: 0.08),
                      borderRadius: AppRadius.smRadius,
                    ),
                    child: Text(
                      '${question.questions.length} 题',
                      style: const TextStyle(
                        fontSize: 11,
                        color: AppColors.primary,
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 10),
              ...List.generate(question.questions.length, (index) {
                final item = question.questions[index];
                return Padding(
                  padding: EdgeInsets.only(
                    bottom: index == question.questions.length - 1 ? 0 : 14,
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        item.header.isNotEmpty
                            ? item.header
                            : '问题 ${index + 1}',
                        style: const TextStyle(
                          fontSize: 12,
                          fontWeight: FontWeight.w600,
                          color: AppColors.textPrimary,
                        ),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        item.question,
                        style: const TextStyle(
                          fontSize: 13,
                          color: AppColors.textSecondary,
                        ),
                      ),
                      const SizedBox(height: 8),
                      if (item.multiple)
                        ...item.options.map((option) {
                          final selected = _answers[index].contains(
                            option.label,
                          );
                          return CheckboxListTile(
                            value: selected,
                            dense: true,
                            contentPadding: EdgeInsets.zero,
                            controlAffinity: ListTileControlAffinity.leading,
                            title: Text(option.label),
                            subtitle: option.description.isNotEmpty
                                ? Text(option.description)
                                : null,
                            onChanged: (value) => _toggleMultiple(
                              index,
                              option.label,
                              value ?? false,
                            ),
                          );
                        })
                      else
                        ...item.options.map((option) {
                          return RadioListTile<String>(
                            value: option.label,
                            groupValue: _answers[index].isEmpty
                                ? null
                                : _answers[index].first,
                            dense: true,
                            contentPadding: EdgeInsets.zero,
                            title: Text(option.label),
                            subtitle: option.description.isNotEmpty
                                ? Text(option.description)
                                : null,
                            onChanged: (value) {
                              if (value != null) {
                                _selectSingle(index, value);
                              }
                            },
                          );
                        }),
                      if (item.custom) ...[
                        const SizedBox(height: 6),
                        TextField(
                          key: ValueKey('question-custom-$index'),
                          controller: _customAnswerControllers[index],
                          minLines: 1,
                          maxLines: 3,
                          textInputAction: TextInputAction.newline,
                          style: const TextStyle(
                            fontSize: 13,
                            color: AppColors.textPrimary,
                          ),
                          decoration: const InputDecoration(
                            hintText: '输入其他回复',
                            prefixIcon: Icon(Icons.edit_outlined, size: 17),
                            isDense: true,
                          ),
                        ),
                      ],
                    ],
                  ),
                );
              }),
              const SizedBox(height: 12),
              Row(
                children: [
                  Expanded(
                    child: AppButton(
                      label: '提交选择',
                      onPressed: _canSubmit
                          ? () => widget.onSubmit(
                              question.requestId,
                              _submittedAnswers,
                            )
                          : null,
                    ),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: AppButton(
                      label: '取消/拒绝',
                      outlined: true,
                      onPressed: () => widget.onReject(question.requestId),
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

// 输入区
class _InputArea extends ConsumerStatefulWidget {
  final TextEditingController controller;
  final FocusNode focusNode;
  final bool sending;
  final bool taskActive;
  final String? sessionId;
  final List<ChatQueueItem> queueItems;
  final bool queueLoading;
  final String? queueError;
  final List<AttachedFile> attachedFiles;
  final String agentId;
  final String projectId;
  final String projectRoot;
  final String machineId;
  final String projectPrompt;
  final ModelInfo? selectedModel;
  final List<ModelInfo> availableModels;
  final String currentPlanMode;
  final List<_PlanBatchEntry> planHistory;
  final GoalSettings goalSettings;
  final _GoalProgressSummary goalProgress;
  final ContextUsageInfo? contextUsage;
  final int configuredCompactionThreshold;
  final bool compactionConfigSaving;
  final VoidCallback onSend;
  final VoidCallback onStop;
  final ValueChanged<ChatQueueItem> onDeleteQueueItem;
  final ValueChanged<ChatQueueItem> onInsertQueueItem;
  final ValueChanged<ChatQueueItem> onSendQueueItem;
  final ValueChanged<List<ChatQueueItem>> onReorderQueueItems;
  final void Function(ChatQueueItem item, ModelInfo? model, String? variant)
  onUpdateQueueItemModel;
  final VoidCallback onPickFiles;
  final void Function(int index) onRemoveFile;
  final void Function(ModelInfo? model) onSelectModel;
  final void Function(AttachedFile file) onAddFile;
  final VoidCallback onEditProjectPrompt;
  final VoidCallback onOpenProjectMemory;
  final VoidCallback onOpenGoalSettings;
  final VoidCallback onCompactContext;
  final ValueChanged<int> onSaveCompactionThreshold;

  const _InputArea({
    required this.controller,
    required this.focusNode,
    required this.sending,
    required this.taskActive,
    required this.sessionId,
    required this.queueItems,
    required this.queueLoading,
    required this.queueError,
    required this.attachedFiles,
    required this.agentId,
    required this.projectId,
    required this.projectRoot,
    required this.machineId,
    required this.projectPrompt,
    required this.selectedModel,
    required this.availableModels,
    required this.currentPlanMode,
    required this.planHistory,
    required this.goalSettings,
    required this.goalProgress,
    required this.contextUsage,
    required this.configuredCompactionThreshold,
    required this.compactionConfigSaving,
    required this.onSend,
    required this.onStop,
    required this.onDeleteQueueItem,
    required this.onInsertQueueItem,
    required this.onSendQueueItem,
    required this.onReorderQueueItems,
    required this.onUpdateQueueItemModel,
    required this.onPickFiles,
    required this.onRemoveFile,
    required this.onSelectModel,
    required this.onAddFile,
    required this.onEditProjectPrompt,
    required this.onOpenProjectMemory,
    required this.onOpenGoalSettings,
    required this.onCompactContext,
    required this.onSaveCompactionThreshold,
  });

  @override
  ConsumerState<_InputArea> createState() => _InputAreaState();
}

class _InputAreaState extends ConsumerState<_InputArea> {
  bool _syncing = false;
  _SlashCommandTrigger? _slashTrigger;
  int _highlightedCommandIndex = 0;
  int _draftTokenEstimate = 0;

  @override
  void initState() {
    super.initState();
    widget.focusNode.onKeyEvent = _handleKeyEvent;
    widget.controller.addListener(_handleControllerChanged);
    _draftTokenEstimate = _estimateTokens(widget.controller.text);
  }

  @override
  void didUpdateWidget(covariant _InputArea oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller) {
      oldWidget.controller.removeListener(_handleControllerChanged);
      widget.controller.addListener(_handleControllerChanged);
      _draftTokenEstimate = _estimateTokens(widget.controller.text);
    }
    if (_syncing) return;
    _syncInputFromAttachments();
    _updateSlashTrigger();
  }

  @override
  void dispose() {
    widget.controller.removeListener(_handleControllerChanged);
    super.dispose();
  }

  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    final commands = _matchingSlashCommands;
    final desktopShortcuts = _usesDesktopShortcuts;
    if (event is KeyDownEvent && _slashTrigger != null && commands.isNotEmpty) {
      if (event.logicalKey == LogicalKeyboardKey.escape) {
        setState(() {
          _slashTrigger = null;
          _highlightedCommandIndex = 0;
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
        setState(() {
          _highlightedCommandIndex =
              (_highlightedCommandIndex + 1) % commands.length;
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
        setState(() {
          _highlightedCommandIndex =
              (_highlightedCommandIndex - 1 + commands.length) %
              commands.length;
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.enter ||
          event.logicalKey == LogicalKeyboardKey.tab) {
        if (event.logicalKey == LogicalKeyboardKey.tab ||
            !(desktopShortcuts && HardwareKeyboard.instance.isControlPressed)) {
          _selectSlashCommand(commands[_highlightedCommandIndex]);
          return KeyEventResult.handled;
        }
      }
    }

    if (event is KeyDownEvent &&
        event.logicalKey == LogicalKeyboardKey.enter &&
        desktopShortcuts) {
      if (HardwareKeyboard.instance.isControlPressed) {
        _insertNewline();
      } else if (!HardwareKeyboard.instance.isAltPressed &&
          !HardwareKeyboard.instance.isMetaPressed) {
        widget.onSend();
      } else {
        return KeyEventResult.ignored;
      }
      return KeyEventResult.handled;
    }

    if (event is KeyDownEvent &&
        event.logicalKey == LogicalKeyboardKey.enter &&
        HardwareKeyboard.instance.isControlPressed) {
      widget.onSend();
      return KeyEventResult.handled;
    }
    // 拦截 Ctrl+V / Meta+V：桌面端优先把文件/截图变成附件
    if (event is KeyDownEvent &&
        event.logicalKey == LogicalKeyboardKey.keyV &&
        (HardwareKeyboard.instance.isControlPressed ||
            HardwareKeyboard.instance.isMetaPressed)) {
      if (desktopShortcuts || isClipboardFilePasteSupported) {
        unawaited(_handlePaste());
        return KeyEventResult.handled;
      }
      final settings = ref.read(settingsProvider);
      if (settings.pasteAsFile) {
        unawaited(_handlePaste());
        return KeyEventResult.handled;
      }
    }
    return KeyEventResult.ignored;
  }

  List<_SlashCommand> get _matchingSlashCommands {
    final trigger = _slashTrigger;
    if (trigger == null) return const [];
    return _commandsForTrigger(trigger);
  }

  List<_SlashCommand> _commandsForTrigger(_SlashCommandTrigger trigger) {
    final query = trigger.query.trim().toLowerCase();
    return _slashCommands
        .where((item) {
          final name = item.command.substring(1).toLowerCase();
          return query.isEmpty || name.startsWith(query);
        })
        .toList(growable: false);
  }

  void _handleControllerChanged() {
    if (_syncing) return;
    _syncAttachmentsFromInput();
    _updateSlashTrigger();
    _updateDraftTokenEstimate();
  }

  bool get _usesDesktopShortcuts {
    if (kIsWeb) return !AppBreakpoints.isMobile(context);
    return switch (defaultTargetPlatform) {
      TargetPlatform.windows ||
      TargetPlatform.macOS ||
      TargetPlatform.linux => true,
      _ => false,
    };
  }

  void _insertNewline() {
    final value = widget.controller.value;
    final selection = value.selection.isValid
        ? value.selection
        : TextSelection.collapsed(offset: value.text.length);
    final start = selection.start.clamp(0, value.text.length).toInt();
    final end = selection.end.clamp(start, value.text.length).toInt();
    final text = value.text.replaceRange(start, end, '\n');
    widget.controller.value = value.copyWith(
      text: text,
      selection: TextSelection.collapsed(offset: start + 1),
      composing: TextRange.empty,
    );
  }

  int _estimateTokens(String text) {
    return (text.length / 4).round().clamp(0, 1 << 31);
  }

  void _updateDraftTokenEstimate() {
    final next = _estimateTokens(
      _ChatPageState._inlineTextToken
          .allMatches(widget.controller.text)
          .fold<String>(
            widget.controller.text,
            (value, match) => value.replaceAll(match.group(0)!, ''),
          ),
    );
    if (next == _draftTokenEstimate) return;
    if (!mounted) return;
    setState(() => _draftTokenEstimate = next);
  }

  _SlashCommandTrigger? _resolveSlashTrigger() {
    final ctrl = widget.controller;
    final selection = ctrl.selection;
    if (!selection.isValid || !selection.isCollapsed) return null;
    final text = ctrl.text;
    final cursor = selection.baseOffset;
    if (cursor < 0 || cursor > text.length) return null;

    var start = cursor;
    while (start > 0) {
      final previous = text[start - 1];
      if (previous.trim().isEmpty) break;
      start--;
    }
    if (text.substring(0, start).trim().isNotEmpty) return null;

    final token = text.substring(start, cursor);
    if (!token.startsWith('/')) return null;
    if (token.length > 32) return null;

    final trigger = _SlashCommandTrigger(
      start: start,
      end: cursor,
      query: token.substring(1).toLowerCase(),
    );
    return _commandsForTrigger(trigger).isEmpty ? null : trigger;
  }

  void _updateSlashTrigger() {
    final next = _resolveSlashTrigger();
    final nextLength = next == null ? 0 : _commandsForTrigger(next).length;
    final current = _slashTrigger;
    if (current?.start == next?.start &&
        current?.end == next?.end &&
        current?.query == next?.query &&
        (_highlightedCommandIndex < nextLength || nextLength == 0)) {
      return;
    }
    if (!mounted) return;
    setState(() {
      _slashTrigger = next;
      if (_highlightedCommandIndex >= nextLength) {
        _highlightedCommandIndex = 0;
      }
    });
  }

  void _selectSlashCommand(_SlashCommand command) {
    final trigger = _slashTrigger;
    if (trigger == null) return;
    final ctrl = widget.controller;
    final text = ctrl.text;
    if (trigger.start < 0 ||
        trigger.end < trigger.start ||
        trigger.end > text.length) {
      return;
    }
    final next = text.replaceRange(
      trigger.start,
      trigger.end,
      command.insertText,
    );
    _syncing = true;
    ctrl.value = TextEditingValue(
      text: next,
      selection: TextSelection.collapsed(
        offset: trigger.start + command.insertText.length,
      ),
    );
    _syncing = false;
    setState(() {
      _slashTrigger = null;
      _highlightedCommandIndex = 0;
    });
    widget.focusNode.requestFocus();
  }

  Future<void> _handlePaste() async {
    final settings = ref.read(settingsProvider);
    final result = await resolveClipboardPaste(
      readFiles: readClipboardFileAttachments,
      readImage: readClipboardImageAttachment,
      readText: () async {
        final data = await Clipboard.getData(Clipboard.kTextPlain);
        return data?.text ?? '';
      },
      pasteAsFile: settings.pasteAsFile,
      threshold: AppSettings.pasteAsFileThreshold,
    );
    if (!mounted) return;
    if (result.hasAttachments) {
      for (final file in result.attachments) {
        final token = file.inlineToken;
        if (token != null && token.isNotEmpty) {
          _insertInlineToken(token);
        }
        widget.onAddFile(file);
      }
      return;
    }
    final text = result.insertText ?? '';
    if (text.isEmpty) return;
    final ctrl = widget.controller;
    final sel = ctrl.selection;
    final newText = ctrl.text.replaceRange(
      sel.start < 0 ? ctrl.text.length : sel.start,
      sel.end < 0 ? ctrl.text.length : sel.end,
      text,
    );
    ctrl.value = TextEditingValue(
      text: newText,
      selection: TextSelection.collapsed(
        offset: (sel.start < 0 ? ctrl.text.length : sel.start) + text.length,
      ),
    );
  }

  void _insertInlineToken(String token) {
    final ctrl = widget.controller;
    final sel = ctrl.selection;
    final start = sel.start < 0 ? ctrl.text.length : sel.start;
    final end = sel.end < 0 ? ctrl.text.length : sel.end;
    final prefix = start > 0 && !ctrl.text[start - 1].contains(RegExp(r'\s'))
        ? ' '
        : '';
    final suffix =
        end < ctrl.text.length && !ctrl.text[end].contains(RegExp(r'\s'))
        ? ' '
        : '';
    final insert = '$prefix$token$suffix';
    final next = ctrl.text.replaceRange(start, end, insert);
    _syncing = true;
    ctrl.value = TextEditingValue(
      text: next,
      selection: TextSelection.collapsed(offset: start + insert.length),
    );
    _syncing = false;
  }

  void _syncAttachmentsFromInput() {
    if (_syncing) return;
    final files = widget.attachedFiles;
    if (files.isEmpty) return;
    final text = widget.controller.text;
    final remove = <int>[];
    for (var i = 0; i < files.length; i++) {
      final token = files[i].inlineToken;
      if (token == null || token.isEmpty) continue;
      if (text.contains(token)) continue;
      remove.add(i);
    }
    for (final i in remove.reversed) {
      widget.onRemoveFile(i);
    }
  }

  void _syncInputFromAttachments() {
    final ctrl = widget.controller;
    var text = ctrl.text;
    var changed = false;

    for (final file in widget.attachedFiles) {
      final token = file.inlineToken;
      if (token == null || token.isEmpty) continue;
      if (text.contains(token)) continue;
      text = text.trimRight();
      text = text.isEmpty ? token : '$text $token';
      changed = true;
    }

    final tokens = widget.attachedFiles
        .map((file) => file.inlineToken)
        .whereType<String>()
        .toSet();
    final matches = _ChatPageState._inlineTextToken
        .allMatches(text)
        .map((m) => m.group(0)!)
        .toList();
    for (final token in matches) {
      if (tokens.contains(token)) continue;
      text = text.replaceAll(token, '');
      changed = true;
    }

    if (!changed) return;
    text = text.replaceAll(RegExp(r'\s{2,}'), ' ').trim();
    _syncing = true;
    ctrl.value = TextEditingValue(
      text: text,
      selection: TextSelection.collapsed(offset: text.length),
    );
    _syncing = false;
  }

  void _showModelLatencyTest() {
    final model = widget.selectedModel;
    if (model == null) {
      showAppFeedback(context, message: '请先选择模型');
      return;
    }
    final variant = normalizeThinkingVariant(
      model.variants,
      ref.read(selectedVariantProvider(widget.agentId)),
    );
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => _ModelLatencyTestSheet(
        agentId: widget.agentId,
        projectId: widget.projectId,
        model: model,
        variant: variant,
      ),
    );
  }

  Future<void> _handleMenuPaste(EditableTextState state) async {
    await _handlePaste();
    if (!mounted) return;
    ContextMenuController.removeAny();
  }

  Widget _buildContextMenu(BuildContext context, EditableTextState state) {
    return AdaptiveTextSelectionToolbar.buttonItems(
      anchors: state.contextMenuAnchors,
      buttonItems: [
        ...state.contextMenuButtonItems.map((item) {
          if (item.type != ContextMenuButtonType.paste) return item;
          return ContextMenuButtonItem(
            onPressed: () => _handleMenuPaste(state),
            type: ContextMenuButtonType.paste,
          );
        }),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final slashCommands = _matchingSlashCommands;
    return Container(
      decoration: const BoxDecoration(
        color: AppColors.surface,
        border: Border(top: BorderSide(color: AppColors.border)),
      ),
      padding: const EdgeInsets.fromLTRB(12, 10, 12, 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // 只有在确实存在待发送消息时才展示队列条；
          // 连接层面的错误不应把空队列渲染成「队列状态异常」。
          if (widget.queueItems.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: _QueueTray(
                agentId: widget.agentId,
                projectId: widget.projectId,
                items: widget.queueItems,
                loading: widget.queueLoading,
                error: widget.queueError,
                taskActive: widget.taskActive,
                availableModels: widget.availableModels,
                onDelete: widget.onDeleteQueueItem,
                onInsert: widget.onInsertQueueItem,
                onSend: widget.onSendQueueItem,
                onReorder: widget.onReorderQueueItems,
                onUpdateModel: widget.onUpdateQueueItemModel,
              ),
            ),
          // 文件预览列表
          if (widget.attachedFiles.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: SizedBox(
                height: 68,
                child: ListView.builder(
                  scrollDirection: Axis.horizontal,
                  itemCount: widget.attachedFiles.length,
                  itemBuilder: (context, i) {
                    return _FilePreviewChip(
                      key: ValueKey(widget.attachedFiles[i].id),
                      file: widget.attachedFiles[i],
                      onRemove: () => widget.onRemoveFile(i),
                    );
                  },
                ),
              ),
            ),
          // 工具栏：附件 + 模型选择
          Padding(
            padding: const EdgeInsets.only(bottom: 6),
            child: SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  // 附件按钮
                  InkWell(
                    onTap: widget.onPickFiles,
                    borderRadius: AppRadius.smRadius,
                    child: Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 8,
                        vertical: 5,
                      ),
                      decoration: BoxDecoration(
                        color: AppColors.inputBackground,
                        borderRadius: AppRadius.smRadius,
                        border: Border.all(color: AppColors.border),
                      ),
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          const Icon(
                            Icons.attach_file,
                            size: 14,
                            color: AppColors.textSecondary,
                          ),
                          const SizedBox(width: 4),
                          Text(
                            widget.attachedFiles.isEmpty ? '附件' : '添加更多',
                            style: const TextStyle(
                              fontSize: 12,
                              color: AppColors.textSecondary,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),
                  const SizedBox(width: 6),
                  _ModeBadge(mode: widget.currentPlanMode),
                  const SizedBox(width: 8),
                  _GoalSettingsButton(
                    settings: widget.goalSettings,
                    progress: widget.goalProgress,
                    onTap: widget.onOpenGoalSettings,
                  ),
                  const SizedBox(width: 8),
                  // 模型选择器
                  _ModelSelector(
                    selectedModel: widget.selectedModel,
                    availableModels: widget.availableModels,
                    onSelect: widget.onSelectModel,
                  ),
                  const SizedBox(width: 6),
                  _ModelLatencyTestButton(
                    enabled: widget.selectedModel != null && !widget.sending,
                    onTap: _showModelLatencyTest,
                  ),
                  // 思考强度（仅当前模型支持时显示）
                  if (widget.selectedModel != null &&
                      widget.selectedModel!.hasVariants) ...[
                    const SizedBox(width: 6),
                    _VariantSelector(
                      variants: widget.selectedModel!.variants,
                      selected: normalizeThinkingVariant(
                        widget.selectedModel!.variants,
                        ref.watch(selectedVariantProvider(widget.agentId)),
                      ),
                      onSelect: (v) => ref
                          .read(
                            selectedVariantProvider(widget.agentId).notifier,
                          )
                          .select(v),
                    ),
                  ],
                  const SizedBox(width: 8),
                  // 审批模式
                  _PermissionModeButton(
                    mode: ref.watch(permissionModeProvider),
                    onChanged: (m) =>
                        ref.read(permissionModeProvider.notifier).set(m),
                  ),
                  const SizedBox(width: 8),
                  _ProjectPromptButton(
                    hasPrompt: widget.projectPrompt.trim().isNotEmpty,
                    onTap: widget.onEditProjectPrompt,
                  ),
                  if (projectMemoryFeatureEnabled) ...[
                    const SizedBox(width: 8),
                    _ProjectMemoryButton(onTap: widget.onOpenProjectMemory),
                  ],
                  const SizedBox(width: 8),
                  _PlanHistoryButton(
                    count: widget.planHistory.length,
                    onTap: () => _showPlanHistory(context),
                  ),
                  const SizedBox(width: 8),
                  _MCPToolsButton(onTap: () => _showMCPTools(context)),
                ],
              ),
            ),
          ),
          if (slashCommands.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: _SlashCommandMenu(
                commands: slashCommands,
                highlightedIndex: _highlightedCommandIndex,
                onHover: (index) {
                  if (_highlightedCommandIndex == index) return;
                  setState(() => _highlightedCommandIndex = index);
                },
                onSelect: _selectSlashCommand,
              ),
            ),
          // 输入行
          Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              Expanded(
                child: Stack(
                  children: [
                    TextField(
                      controller: widget.controller,
                      focusNode: widget.focusNode,
                      contextMenuBuilder: _buildContextMenu,
                      maxLines: 6,
                      minLines: 1,
                      textInputAction: TextInputAction.newline,
                      onSubmitted: null,
                      buildCounter:
                          (
                            context, {
                            required currentLength,
                            required isFocused,
                            required maxLength,
                          }) => null,
                      decoration: InputDecoration(
                        hintText: '输入消息',
                        border: OutlineInputBorder(
                          borderRadius: AppRadius.mdRadius,
                          borderSide: const BorderSide(color: AppColors.border),
                        ),
                        enabledBorder: OutlineInputBorder(
                          borderRadius: AppRadius.mdRadius,
                          borderSide: const BorderSide(color: AppColors.border),
                        ),
                        focusedBorder: OutlineInputBorder(
                          borderRadius: AppRadius.mdRadius,
                          borderSide: const BorderSide(
                            color: AppColors.primary,
                            width: 1.5,
                          ),
                        ),
                        contentPadding: const EdgeInsets.fromLTRB(
                          14,
                          10,
                          58,
                          10,
                        ),
                        filled: true,
                        fillColor: AppColors.inputBackground,
                      ),
                    ),
                    Positioned(
                      right: 10,
                      bottom: 8,
                      child: _ContextUsageButton(
                        usage: _contextUsageWithModelLimit(
                          widget.contextUsage,
                          widget.selectedModel?.contextLimit,
                        ),
                        draftTokens: _draftTokenEstimate,
                        configuredThresholdPercent:
                            widget.configuredCompactionThreshold,
                        savingThreshold: widget.compactionConfigSaving,
                        canCompact:
                            widget.sessionId?.trim().isNotEmpty == true &&
                            !widget.taskActive &&
                            !widget.sending,
                        onCompact: widget.onCompactContext,
                        onSaveThreshold: widget.onSaveCompactionThreshold,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 8),
              _ComposerActions(
                taskActive: widget.taskActive,
                onSend: widget.onSend,
                onStop: widget.onStop,
              ),
            ],
          ),
          if (AppBreakpoints.isMobile(context))
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Row(
                children: [
                  const Icon(
                    Icons.folder_outlined,
                    size: 12,
                    color: AppColors.textMuted,
                  ),
                  const SizedBox(width: 4),
                  Text(
                    projectDirectoryLabel(widget.projectRoot, widget.projectId),
                    style: const TextStyle(
                      fontSize: 11,
                      color: AppColors.textMuted,
                    ),
                  ),
                ],
              ),
            )
          else if (widget.sessionId != null)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Row(
                children: [
                  const Icon(Icons.link, size: 11, color: AppColors.textMuted),
                  const SizedBox(width: 4),
                  Text(
                    '当前会话 ${widget.sessionId}',
                    style: const TextStyle(
                      fontSize: 11,
                      color: AppColors.textMuted,
                    ),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }

  void _showPlanHistory(BuildContext context) {
    final entries = widget.planHistory;
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (sheetContext) {
        return FractionallySizedBox(
          heightFactor: 0.82,
          child: Container(
            decoration: const BoxDecoration(
              color: AppColors.surface,
              borderRadius: BorderRadius.vertical(top: Radius.circular(20)),
            ),
            child: Column(
              children: [
                Container(
                  width: 36,
                  height: 4,
                  margin: const EdgeInsets.only(top: 10, bottom: 12),
                  decoration: BoxDecoration(
                    color: AppColors.border,
                    borderRadius: BorderRadius.circular(999),
                  ),
                ),
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
                  child: Row(
                    children: [
                      const Text(
                        '计划历史',
                        style: TextStyle(
                          fontSize: 15,
                          fontWeight: FontWeight.w700,
                          color: AppColors.textPrimary,
                        ),
                      ),
                      const SizedBox(width: 8),
                      Container(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 8,
                          vertical: 3,
                        ),
                        decoration: BoxDecoration(
                          color: AppColors.primaryLight,
                          borderRadius: BorderRadius.circular(999),
                        ),
                        child: Text(
                          '${entries.length} 组',
                          style: const TextStyle(
                            fontSize: 11,
                            fontWeight: FontWeight.w600,
                            color: AppColors.primary,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
                const Divider(height: 1, color: AppColors.border),
                Expanded(
                  child: entries.isEmpty
                      ? const Center(
                          child: Text(
                            '当前会话还没有计划历史',
                            style: TextStyle(
                              fontSize: 13,
                              color: AppColors.textMuted,
                            ),
                          ),
                        )
                      : ListView.separated(
                          padding: const EdgeInsets.all(16),
                          itemCount: entries.length,
                          separatorBuilder: (_, __) =>
                              const SizedBox(height: 10),
                          itemBuilder: (context, index) {
                            final entry = entries[index];
                            return _PlanHistoryListItem(
                              entry: entry,
                              onTap: () => _showPlanDetail(context, entry),
                            );
                          },
                        ),
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  void _showPlanDetail(BuildContext context, _PlanBatchEntry entry) {
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (sheetContext) {
        return FractionallySizedBox(
          heightFactor: 0.86,
          child: Container(
            decoration: const BoxDecoration(
              color: AppColors.surface,
              borderRadius: BorderRadius.vertical(top: Radius.circular(20)),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Container(
                  width: 36,
                  height: 4,
                  margin: const EdgeInsets.only(top: 10, bottom: 12),
                  decoration: BoxDecoration(
                    color: AppColors.border,
                    borderRadius: BorderRadius.circular(999),
                  ),
                ),
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        entry.plan.title.isEmpty ? '执行计划' : entry.plan.title,
                        style: const TextStyle(
                          fontSize: 15,
                          fontWeight: FontWeight.w700,
                          color: AppColors.textPrimary,
                        ),
                      ),
                      const SizedBox(height: 6),
                      Wrap(
                        spacing: 6,
                        runSpacing: 6,
                        children: [
                          _PlanBadge(
                            label:
                                entry.plan.mode.trim().toLowerCase() == 'plan'
                                ? 'Plan'
                                : 'Build',
                            color: AppColors.primary,
                            background: AppColors.primaryLight,
                          ),
                          _PlanBadge(
                            label: _planStatusText(entry.plan.status),
                            color: _planStatusColor(entry.plan.status),
                            background: _planStatusBg(entry.plan.status),
                          ),
                          _PlanBadge(
                            label:
                                '${entry.plan.items.where((item) => item.status == 'completed').length}/${entry.plan.items.length}',
                            color: AppColors.textSecondary,
                            background: AppColors.inputBackground,
                          ),
                        ],
                      ),
                      const SizedBox(height: 8),
                      Text(
                        '开始于 ${_planHistoryTimeText(entry.startedAt)}',
                        style: const TextStyle(
                          fontSize: 12,
                          color: AppColors.textMuted,
                        ),
                      ),
                    ],
                  ),
                ),
                const Divider(height: 1, color: AppColors.border),
                Expanded(
                  child: ListView.separated(
                    padding: const EdgeInsets.all(16),
                    itemCount: entry.plan.items.length,
                    separatorBuilder: (_, __) => const SizedBox(height: 8),
                    itemBuilder: (context, index) {
                      final item = entry.plan.items[index];
                      return _PlanHistoryItemTile(
                        item: item,
                        onTap: () => _showPlanItemDetail(context, entry, item),
                      );
                    },
                  ),
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  void _showPlanItemDetail(
    BuildContext context,
    _PlanBatchEntry entry,
    PlanItemInfo item,
  ) {
    showModalBottomSheet<void>(
      context: context,
      backgroundColor: Colors.transparent,
      builder: (sheetContext) {
        return SafeArea(
          child: Container(
            decoration: const BoxDecoration(
              color: AppColors.surface,
              borderRadius: BorderRadius.vertical(top: Radius.circular(20)),
            ),
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 14, 16, 24),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Container(
                    width: 36,
                    height: 4,
                    margin: const EdgeInsets.only(bottom: 14),
                    decoration: BoxDecoration(
                      color: AppColors.border,
                      borderRadius: BorderRadius.circular(999),
                    ),
                  ),
                  Text(
                    item.text,
                    style: const TextStyle(
                      fontSize: 15,
                      fontWeight: FontWeight.w700,
                      color: AppColors.textPrimary,
                    ),
                  ),
                  const SizedBox(height: 10),
                  Wrap(
                    spacing: 6,
                    runSpacing: 6,
                    children: [
                      _PlanBadge(
                        label: _planItemStatusText(item.status),
                        color: _planStatusColor(item.status),
                        background: _planStatusBg(item.status),
                      ),
                      if (item.priority.isNotEmpty)
                        _PlanBadge(
                          label: _planPriorityText(item.priority),
                          color: AppColors.textSecondary,
                          background: AppColors.inputBackground,
                        ),
                    ],
                  ),
                  const SizedBox(height: 14),
                  _PlanDetailLine(label: '所属计划', value: entry.plan.title),
                  const SizedBox(height: 8),
                  _PlanDetailLine(
                    label: '开始时间',
                    value: _planHistoryTimeText(entry.startedAt),
                  ),
                  const SizedBox(height: 8),
                  _PlanDetailLine(
                    label: '最后更新',
                    value: _planHistoryTimeText(entry.updatedAt),
                  ),
                  const SizedBox(height: 8),
                  _PlanDetailLine(
                    label: '说明',
                    value: '当前版本先展示结构化计划项本身，后续可以继续补充与具体执行消息、工具调用和产物的关联。',
                  ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }

  Color _planStatusColor(String status) {
    switch (status) {
      case 'completed':
        return AppColors.statusSuccess;
      case 'in_progress':
        return AppColors.statusWarning;
      default:
        return AppColors.textMuted;
    }
  }

  Color _planStatusBg(String status) {
    switch (status) {
      case 'completed':
        return AppColors.statusSuccessLight;
      case 'in_progress':
        return AppColors.statusWarningLight;
      default:
        return AppColors.primaryLight;
    }
  }

  String _planStatusText(String status) {
    switch (status) {
      case 'completed':
        return '已完成';
      case 'in_progress':
        return '执行中';
      default:
        return '待开始';
    }
  }

  String _planItemStatusText(String status) {
    switch (status) {
      case 'completed':
        return '已完成';
      case 'in_progress':
        return '进行中';
      default:
        return '待开始';
    }
  }

  String _planPriorityText(String priority) {
    switch (priority) {
      case 'high':
        return '高优先级';
      case 'medium':
        return '中优先级';
      case 'low':
        return '低优先级';
      default:
        return priority;
    }
  }

  Future<void> _showMCPTools(BuildContext context) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (sheetContext) {
        return _MCPToolsSheet(
          future: ChatRepository().getMCPStatus(
            machineId: widget.machineId,
            agentId: widget.agentId,
          ),
        );
      },
    );
  }
}

class _GoalSettingsButton extends StatelessWidget {
  static const double _height = 28;

  final GoalSettings settings;
  final _GoalProgressSummary progress;
  final VoidCallback onTap;

  const _GoalSettingsButton({
    required this.settings,
    required this.progress,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final active = settings.enabled && settings.hasContent;
    final hasGoal = settings.hasContent;
    final running = active && progress.isRunning;
    final latestType = progress.latest?.type;
    final color = running
        ? AppColors.statusSuccess
        : (hasGoal && latestType != null
              ? _goalProgressColor(latestType)
              : (hasGoal ? AppColors.primary : AppColors.textSecondary));
    final background = running
        ? AppColors.statusSuccessLight
        : (hasGoal
              ? _goalProgressBackground(latestType)
              : AppColors.inputBackground);
    return InkWell(
      onTap: onTap,
      borderRadius: AppRadius.smRadius,
      child: SizedBox(
        height: _height,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 8),
          decoration: BoxDecoration(
            color: background,
            borderRadius: AppRadius.smRadius,
            border: Border.all(
              color: active
                  ? AppColors.statusSuccess.withValues(alpha: 0.24)
                  : (hasGoal ? AppColors.primaryMuted : AppColors.border),
            ),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                _goalProgressIcon(latestType, running: running),
                size: 14,
                color: color,
              ),
              const SizedBox(width: 4),
              Text(
                '目标',
                style: TextStyle(
                  fontSize: 12,
                  color: color,
                  fontWeight: hasGoal ? FontWeight.w600 : FontWeight.w400,
                  height: 1,
                ),
              ),
              if (active || progress.hasEntries) ...[
                const SizedBox(width: 6),
                _GoalRoundBadge(
                  label: progress.hasEntries
                      ? progress.roundLabel
                      : '0/${settings.maxIterations}',
                  color: color,
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _GoalRoundBadge extends StatelessWidget {
  final String label;
  final Color color;

  const _GoalRoundBadge({required this.label, required this.color});

  @override
  Widget build(BuildContext context) {
    return Container(
      height: 16,
      padding: const EdgeInsets.symmetric(horizontal: 5),
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: AppColors.surface,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 10,
          height: 1,
          fontWeight: FontWeight.w600,
          color: color,
        ),
      ),
    );
  }
}

class _GoalSettingsSheet extends ConsumerStatefulWidget {
  final String agentId;
  final String projectId;
  final Future<bool> Function(String goal, int maxIterations) onStartGoal;

  const _GoalSettingsSheet({
    required this.agentId,
    required this.projectId,
    required this.onStartGoal,
  });

  @override
  ConsumerState<_GoalSettingsSheet> createState() => _GoalSettingsSheetState();
}

class _GoalSettingsSheetState extends ConsumerState<_GoalSettingsSheet> {
  late final TextEditingController _controller;
  late final TextEditingController _maxController;
  Timer? _timer;
  bool _saving = false;
  bool _optimizing = false;

  (String, String) get _key => (widget.agentId, widget.projectId);
  bool get _busy => _saving || _optimizing;

  @override
  void initState() {
    super.initState();
    final settings = ref.read(goalSettingsProvider(_key));
    _controller = TextEditingController(text: settings.content);
    _maxController = TextEditingController(
      text: settings.maxIterations.toString(),
    );
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) setState(() {});
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    _controller.dispose();
    _maxController.dispose();
    super.dispose();
  }

  int _maxIterationsDraft() {
    return GoalSettings.normalizeMaxIterations(_maxController.text);
  }

  Future<void> _run(Future<void> Function() action) async {
    if (_busy) return;
    setState(() => _saving = true);
    try {
      await action();
    } catch (e) {
      if (!mounted) return;
      showAppFeedback(
        context,
        title: '保存目标失败',
        message: _errorText(e),
        error: true,
      );
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  String _errorText(Object error) {
    return error.toString().replaceFirst(RegExp(r'^Exception:\s*'), '');
  }

  Future<void> _optimizePrompt() async {
    if (_busy) return;
    final draft = _controller.text.trim();
    if (draft.isEmpty) {
      showAppFeedback(context, message: '请先填写目标内容');
      return;
    }
    final model = ref.read(selectedModelProvider(widget.agentId));
    if (model == null) {
      showAppFeedback(context, message: '请先选择模型后再优化目标');
      return;
    }
    final variant = model.hasVariants
        ? ref.read(selectedVariantProvider(widget.agentId))
        : null;
    final maxIterations = _maxIterationsDraft();
    setState(() => _optimizing = true);
    try {
      final optimized = await ref
          .read(chatRepositoryProvider)
          .optimizeGoal(
            agentId: widget.agentId,
            projectId: widget.projectId,
            goal: draft,
            model: model,
            variant: variant,
            maxIterations: maxIterations,
          );
      if (mounted) {
        _controller.value = _controller.value.copyWith(
          text: optimized,
          selection: TextSelection.collapsed(offset: optimized.length),
          composing: TextRange.empty,
        );
        showAppFeedback(context, message: '目标提示词已优化，请确认后保存');
      }
    } catch (e) {
      if (mounted) {
        showAppFeedback(
          context,
          title: '优化目标失败',
          message: _errorText(e),
          error: true,
        );
      }
    } finally {
      if (mounted) setState(() => _optimizing = false);
    }
  }

  Future<void> _toggle(bool enabled) async {
    final notifier = ref.read(goalSettingsProvider(_key).notifier);
    final draft = _controller.text.trim();
    if (enabled && draft.isEmpty) {
      showAppFeedback(context, message: '请先填写目标内容');
      return;
    }
    var started = false;
    await _run(() async {
      final current = ref.read(goalSettingsProvider(_key));
      final maxIterations = _maxIterationsDraft();
      final goal = draft;
      if (goal != current.content || maxIterations != current.maxIterations) {
        await notifier.saveContent(goal, maxIterations: maxIterations);
      }
      await notifier.setEnabled(enabled);
      if (enabled) {
        started = await widget.onStartGoal(goal, maxIterations);
        if (!started) {
          await notifier.setEnabled(false);
        }
      }
    });
    if (enabled && started && mounted) {
      Navigator.of(context).pop();
    }
  }

  Future<void> _save() async {
    final notifier = ref.read(goalSettingsProvider(_key).notifier);
    await _run(() async {
      final draft = _controller.text.trim();
      final maxIterations = _maxIterationsDraft();
      await notifier.saveContent(draft, maxIterations: maxIterations);
    });
  }

  Future<void> _clear() async {
    final notifier = ref.read(goalSettingsProvider(_key).notifier);
    await _run(() async {
      _controller.clear();
      _maxController.text = GoalSettings.defaultMaxIterations.toString();
      await notifier.clear();
    });
  }

  @override
  Widget build(BuildContext context) {
    final settings = ref.watch(goalSettingsProvider(_key));
    final progress = _collectGoalProgress(
      ref.watch(chatProvider(_key)).messages,
    );
    final latestGoalEntry = progress.latest;
    final terminal = latestGoalEntry?.isTerminal == true;
    final active = settings.enabled && settings.hasContent && !terminal;
    final statusColor = active
        ? AppColors.statusSuccess
        : switch (settings.status) {
            GoalSettings.statusCompleted => AppColors.statusSuccess,
            GoalSettings.statusFailed => AppColors.statusError,
            GoalSettings.statusPaused => AppColors.statusWarning,
            _ =>
              settings.hasContent
                  ? AppColors.statusWarning
                  : AppColors.textMuted,
          };
    final runtime = _formatGoalRuntime(settings.runtime(DateTime.now()));
    final goalColor = _goalProgressColor(latestGoalEntry?.type);
    final goalHistory = progress.entries.length > 8
        ? progress.entries.sublist(progress.entries.length - 8)
        : progress.entries;

    return SafeArea(
      child: Padding(
        padding: EdgeInsets.only(
          left: 12,
          right: 12,
          bottom: MediaQuery.of(context).viewInsets.bottom + 12,
        ),
        child: Container(
          decoration: const BoxDecoration(
            color: AppColors.surface,
            borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
          ),
          padding: const EdgeInsets.fromLTRB(16, 14, 16, 16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  const Icon(
                    Icons.flag_outlined,
                    size: 18,
                    color: AppColors.primary,
                  ),
                  const SizedBox(width: 8),
                  const Text(
                    '目标',
                    style: TextStyle(
                      fontSize: 16,
                      fontWeight: FontWeight.w700,
                      color: AppColors.textPrimary,
                    ),
                  ),
                  const Spacer(),
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 4,
                    ),
                    decoration: BoxDecoration(
                      color: statusColor.withValues(alpha: 0.12),
                      borderRadius: BorderRadius.circular(999),
                    ),
                    child: Text(
                      settings.statusLabel,
                      style: TextStyle(
                        fontSize: 12,
                        fontWeight: FontWeight.w600,
                        color: statusColor,
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 14),
              Container(
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: AppColors.inputBackground,
                  borderRadius: AppRadius.mdRadius,
                  border: Border.all(color: AppColors.border),
                ),
                child: Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          const Text(
                            '运行时间',
                            style: TextStyle(
                              fontSize: 12,
                              color: AppColors.textMuted,
                            ),
                          ),
                          const SizedBox(height: 4),
                          Text(
                            runtime,
                            style: const TextStyle(
                              fontSize: 16,
                              fontWeight: FontWeight.w700,
                              color: AppColors.textPrimary,
                            ),
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(width: 12),
                    Switch(value: active, onChanged: _busy ? null : _toggle),
                  ],
                ),
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _maxController,
                      keyboardType: TextInputType.number,
                      inputFormatters: [FilteringTextInputFormatter.digitsOnly],
                      enabled: !_busy,
                      decoration: InputDecoration(
                        labelText: '最大轮次',
                        border: OutlineInputBorder(
                          borderRadius: AppRadius.mdRadius,
                          borderSide: const BorderSide(color: AppColors.border),
                        ),
                        enabledBorder: OutlineInputBorder(
                          borderRadius: AppRadius.mdRadius,
                          borderSide: const BorderSide(color: AppColors.border),
                        ),
                        focusedBorder: OutlineInputBorder(
                          borderRadius: AppRadius.mdRadius,
                          borderSide: const BorderSide(
                            color: AppColors.primary,
                            width: 1.5,
                          ),
                        ),
                        filled: true,
                        fillColor: AppColors.inputBackground,
                      ),
                    ),
                  ),
                  const SizedBox(width: 12),
                  _PlanBadge(
                    label: progress.hasEntries
                        ? progress.roundLabel
                        : '0/${settings.maxIterations}',
                    color: goalColor,
                    background: _goalProgressBackground(latestGoalEntry?.type),
                  ),
                ],
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _controller,
                minLines: 4,
                maxLines: 7,
                autofocus: false,
                enabled: !_busy,
                decoration: InputDecoration(
                  hintText: '输入目标内容',
                  border: OutlineInputBorder(
                    borderRadius: AppRadius.mdRadius,
                    borderSide: const BorderSide(color: AppColors.border),
                  ),
                  enabledBorder: OutlineInputBorder(
                    borderRadius: AppRadius.mdRadius,
                    borderSide: const BorderSide(color: AppColors.border),
                  ),
                  focusedBorder: OutlineInputBorder(
                    borderRadius: AppRadius.mdRadius,
                    borderSide: const BorderSide(
                      color: AppColors.primary,
                      width: 1.5,
                    ),
                  ),
                  filled: true,
                  fillColor: AppColors.inputBackground,
                ),
              ),
              const SizedBox(height: 12),
              Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: 12,
                  vertical: 10,
                ),
                decoration: BoxDecoration(
                  color: AppColors.inputBackground,
                  borderRadius: AppRadius.mdRadius,
                  border: Border.all(color: AppColors.border),
                ),
                child: Row(
                  children: [
                    const Icon(
                      Icons.auto_fix_high_outlined,
                      size: 18,
                      color: AppColors.textSecondary,
                    ),
                    const SizedBox(width: 10),
                    const Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            '优化目标提示词',
                            style: TextStyle(
                              fontSize: 13,
                              fontWeight: FontWeight.w600,
                              color: AppColors.textPrimary,
                            ),
                          ),
                          SizedBox(height: 2),
                          Text(
                            '使用当前模型整理目标内容',
                            style: TextStyle(
                              fontSize: 12,
                              color: AppColors.textMuted,
                            ),
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(width: 12),
                    if (_optimizing)
                      const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    else
                      OutlinedButton.icon(
                        onPressed: _busy ? null : _optimizePrompt,
                        icon: const Icon(
                          Icons.auto_fix_high_outlined,
                          size: 16,
                        ),
                        label: const Text('立即优化'),
                      ),
                  ],
                ),
              ),
              if (goalHistory.isNotEmpty) ...[
                const SizedBox(height: 12),
                Container(
                  constraints: const BoxConstraints(maxHeight: 180),
                  decoration: BoxDecoration(
                    color: AppColors.inputBackground,
                    borderRadius: AppRadius.mdRadius,
                    border: Border.all(color: AppColors.border),
                  ),
                  padding: const EdgeInsets.fromLTRB(12, 8, 12, 8),
                  child: ListView.separated(
                    shrinkWrap: true,
                    itemCount: goalHistory.length,
                    separatorBuilder: (_, _) => const SizedBox(height: 6),
                    itemBuilder: (context, index) {
                      final entry = goalHistory[index];
                      final entryColor = _goalProgressColor(entry.type);
                      return Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Icon(
                            _goalProgressIcon(entry.type),
                            size: 14,
                            color: entryColor,
                          ),
                          const SizedBox(width: 8),
                          Expanded(
                            child: Text(
                              entry.summary,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                              style: const TextStyle(
                                fontSize: 12,
                                height: 1.45,
                                color: AppColors.textSecondary,
                              ),
                            ),
                          ),
                        ],
                      );
                    },
                  ),
                ),
              ],
              const SizedBox(height: 12),
              Row(
                children: [
                  TextButton(
                    onPressed: _busy ? null : _clear,
                    child: const Text('清空'),
                  ),
                  const Spacer(),
                  TextButton(
                    onPressed: _busy ? null : () => Navigator.of(context).pop(),
                    child: const Text('关闭'),
                  ),
                  const SizedBox(width: 8),
                  FilledButton(
                    onPressed: _busy ? null : _save,
                    child: Text(_optimizing ? '优化中' : (_saving ? '保存中' : '保存')),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _SlashCommandMenu extends StatelessWidget {
  final List<_SlashCommand> commands;
  final int highlightedIndex;
  final ValueChanged<int> onHover;
  final ValueChanged<_SlashCommand> onSelect;

  const _SlashCommandMenu({
    required this.commands,
    required this.highlightedIndex,
    required this.onHover,
    required this.onSelect,
  });

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.transparent,
      child: Container(
        width: double.infinity,
        constraints: const BoxConstraints(maxHeight: 190),
        decoration: BoxDecoration(
          color: AppColors.surface,
          borderRadius: AppRadius.mdRadius,
          border: Border.all(color: AppColors.border),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: 0.08),
              blurRadius: 16,
              offset: const Offset(0, 6),
            ),
          ],
        ),
        child: ListView.separated(
          padding: const EdgeInsets.symmetric(vertical: 6),
          shrinkWrap: true,
          itemCount: commands.length,
          separatorBuilder: (_, __) =>
              const Divider(height: 1, color: AppColors.border),
          itemBuilder: (context, index) {
            return _SlashCommandTile(
              command: commands[index],
              selected: index == highlightedIndex,
              onHover: () => onHover(index),
              onTap: () => onSelect(commands[index]),
            );
          },
        ),
      ),
    );
  }
}

class _SlashCommandTile extends StatelessWidget {
  final _SlashCommand command;
  final bool selected;
  final VoidCallback onHover;
  final VoidCallback onTap;

  const _SlashCommandTile({
    required this.command,
    required this.selected,
    required this.onHover,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final color = selected ? AppColors.primary : AppColors.textSecondary;
    return MouseRegion(
      onEnter: (_) => onHover(),
      child: InkWell(
        onTap: onTap,
        child: Container(
          constraints: const BoxConstraints(minHeight: 52),
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          color: selected ? AppColors.primaryLight : Colors.transparent,
          child: Row(
            children: [
              Container(
                width: 32,
                height: 32,
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  color: selected
                      ? AppColors.surface
                      : AppColors.inputBackground,
                  borderRadius: AppRadius.smRadius,
                  border: Border.all(
                    color: selected
                        ? AppColors.primary.withValues(alpha: 0.22)
                        : AppColors.border,
                  ),
                ),
                child: Icon(command.icon, size: 16, color: color),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Row(
                      children: [
                        Text(
                          command.command,
                          style: TextStyle(
                            fontSize: 13,
                            fontWeight: FontWeight.w700,
                            color: selected
                                ? AppColors.primary
                                : AppColors.textPrimary,
                          ),
                        ),
                        const SizedBox(width: 8),
                        Flexible(
                          child: Text(
                            command.title,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 12,
                              color: AppColors.textSecondary,
                            ),
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 2),
                    Text(
                      command.description,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 11,
                        color: AppColors.textMuted,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 8),
              Icon(Icons.keyboard_return, size: 16, color: color),
            ],
          ),
        ),
      ),
    );
  }
}

class _ModeBadge extends StatelessWidget {
  final String mode;

  const _ModeBadge({required this.mode});

  @override
  Widget build(BuildContext context) {
    final normalized = mode.trim().toLowerCase() == 'plan' ? 'plan' : 'build';
    final isPlan = normalized == 'plan';
    final color = isPlan ? AppColors.primary : AppColors.statusWarning;
    final background = isPlan
        ? AppColors.primaryLight
        : AppColors.statusWarningLight;

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
      decoration: BoxDecoration(
        color: background,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: color.withValues(alpha: 0.18)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            isPlan ? Icons.route_rounded : Icons.build_circle_outlined,
            size: 14,
            color: color,
          ),
          const SizedBox(width: 4),
          Text(
            isPlan ? 'Plan' : 'Build',
            style: TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w600,
              color: color,
            ),
          ),
        ],
      ),
    );
  }
}

class _PlanHistoryButton extends StatelessWidget {
  final int count;
  final VoidCallback onTap;

  const _PlanHistoryButton({required this.count, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final hasHistory = count > 0;
    return InkWell(
      onTap: onTap,
      borderRadius: AppRadius.smRadius,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        decoration: BoxDecoration(
          color: hasHistory
              ? AppColors.primaryLight
              : AppColors.inputBackground,
          borderRadius: AppRadius.smRadius,
          border: Border.all(
            color: hasHistory ? AppColors.primaryMuted : AppColors.border,
          ),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.history_toggle_off_rounded,
              size: 14,
              color: hasHistory ? AppColors.primary : AppColors.textSecondary,
            ),
            const SizedBox(width: 4),
            Text(
              '计划历史',
              style: TextStyle(
                fontSize: 12,
                color: hasHistory ? AppColors.primary : AppColors.textSecondary,
                fontWeight: hasHistory ? FontWeight.w500 : FontWeight.w400,
              ),
            ),
            if (hasHistory) ...[
              const SizedBox(width: 6),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                decoration: const BoxDecoration(
                  color: AppColors.primary,
                  borderRadius: BorderRadius.all(Radius.circular(999)),
                ),
                child: Text(
                  '$count',
                  style: const TextStyle(
                    fontSize: 10,
                    fontWeight: FontWeight.w700,
                    color: Colors.white,
                  ),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _PlanHistoryListItem extends StatelessWidget {
  final _PlanBatchEntry entry;
  final VoidCallback onTap;

  const _PlanHistoryListItem({required this.entry, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final completed = entry.plan.items
        .where((item) => item.status == 'completed')
        .length;
    final total = entry.plan.items.length;
    final statusColor = switch (entry.plan.status) {
      'completed' => AppColors.statusSuccess,
      'in_progress' => AppColors.statusWarning,
      _ => AppColors.textMuted,
    };
    final statusText = switch (entry.plan.status) {
      'completed' => '已完成',
      'in_progress' => '执行中',
      _ => '待开始',
    };

    return InkWell(
      onTap: onTap,
      borderRadius: AppRadius.mdRadius,
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: AppColors.surfaceElevated,
          borderRadius: AppRadius.mdRadius,
          border: Border.all(color: AppColors.borderLight),
        ),
        child: Row(
          children: [
            Container(
              width: 34,
              height: 34,
              decoration: BoxDecoration(
                color: AppColors.primaryLight,
                borderRadius: AppRadius.smRadius,
              ),
              child: const Icon(
                Icons.checklist_rounded,
                size: 18,
                color: AppColors.primary,
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    entry.plan.title.isEmpty ? '执行计划' : entry.plan.title,
                    style: const TextStyle(
                      fontSize: 13,
                      fontWeight: FontWeight.w700,
                      color: AppColors.textPrimary,
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    '$statusText  $completed/$total',
                    style: TextStyle(
                      fontSize: 12,
                      color: statusColor,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    '开始于 ${_planHistoryTimeText(entry.startedAt)}',
                    style: const TextStyle(
                      fontSize: 11,
                      color: AppColors.textMuted,
                    ),
                  ),
                ],
              ),
            ),
            const Icon(
              Icons.chevron_right_rounded,
              size: 18,
              color: AppColors.textMuted,
            ),
          ],
        ),
      ),
    );
  }
}

class _PlanHistoryItemTile extends StatelessWidget {
  final PlanItemInfo item;
  final VoidCallback onTap;

  const _PlanHistoryItemTile({required this.item, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final color = switch (item.status) {
      'completed' => AppColors.statusSuccess,
      'in_progress' => AppColors.statusWarning,
      _ => AppColors.textMuted,
    };
    final icon = switch (item.status) {
      'completed' => Icons.check_circle_rounded,
      'in_progress' => Icons.timelapse_rounded,
      _ => Icons.radio_button_unchecked_rounded,
    };

    return InkWell(
      onTap: onTap,
      borderRadius: AppRadius.smRadius,
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: AppColors.surfaceElevated,
          borderRadius: AppRadius.smRadius,
          border: Border.all(color: AppColors.borderLight),
        ),
        child: Row(
          children: [
            Icon(icon, size: 18, color: color),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                item.text,
                style: const TextStyle(
                  fontSize: 13,
                  height: 1.45,
                  color: AppColors.textPrimary,
                ),
              ),
            ),
            const SizedBox(width: 8),
            const Icon(
              Icons.chevron_right_rounded,
              size: 18,
              color: AppColors.textMuted,
            ),
          ],
        ),
      ),
    );
  }
}

class _PlanDetailLine extends StatelessWidget {
  final String label;
  final String value;

  const _PlanDetailLine({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          label,
          style: const TextStyle(
            fontSize: 11,
            fontWeight: FontWeight.w600,
            color: AppColors.textMuted,
          ),
        ),
        const SizedBox(height: 4),
        Text(
          value,
          style: const TextStyle(
            fontSize: 13,
            height: 1.45,
            color: AppColors.textPrimary,
          ),
        ),
      ],
    );
  }
}

// 文件预览缩略片
class _FilePreviewChip extends StatelessWidget {
  final AttachedFile file;
  final VoidCallback onRemove;
  const _FilePreviewChip({
    super.key,
    required this.file,
    required this.onRemove,
  });

  @override
  Widget build(BuildContext context) {
    final bytes = file.imageBytes;
    return Container(
      width: 64,
      margin: const EdgeInsets.only(right: 8),
      child: Stack(
        clipBehavior: Clip.none,
        children: [
          Container(
            width: 64,
            height: 64,
            decoration: BoxDecoration(
              color: AppColors.inputBackground,
              borderRadius: AppRadius.smRadius,
              border: Border.all(color: AppColors.border),
            ),
            child: bytes != null
                ? ClipRRect(
                    borderRadius: AppRadius.smRadius,
                    child: Image.memory(
                      bytes,
                      fit: BoxFit.cover,
                      gaplessPlayback: true,
                      errorBuilder: (_, __, ___) => _fileIcon(file),
                    ),
                  )
                : _fileIcon(file),
          ),
          // 文件名（底部）
          Positioned(
            bottom: 0,
            left: 0,
            right: 0,
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 3, vertical: 2),
              decoration: BoxDecoration(
                color: Colors.black.withValues(alpha: 0.45),
                borderRadius: const BorderRadius.only(
                  bottomLeft: Radius.circular(4),
                  bottomRight: Radius.circular(4),
                ),
              ),
              child: Text(
                file.filename,
                style: const TextStyle(fontSize: 9, color: Colors.white),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ),
          // 删除按钮
          Positioned(
            top: -6,
            right: -6,
            child: GestureDetector(
              onTap: onRemove,
              child: Container(
                width: 18,
                height: 18,
                decoration: const BoxDecoration(
                  color: AppColors.statusError,
                  shape: BoxShape.circle,
                ),
                child: const Icon(Icons.close, size: 11, color: Colors.white),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _fileIcon(AttachedFile file) {
    final icon = file.isPdf
        ? Icons.picture_as_pdf
        : Icons.insert_drive_file_outlined;
    final color = file.isPdf ? AppColors.statusError : AppColors.textSecondary;
    return Center(child: Icon(icon, size: 28, color: color));
  }
}

// 模型选择器
class _ModelSelector extends StatelessWidget {
  final ModelInfo? selectedModel;
  final List<ModelInfo> availableModels;
  final void Function(ModelInfo? model) onSelect;

  const _ModelSelector({
    required this.selectedModel,
    required this.availableModels,
    required this.onSelect,
  });

  @override
  Widget build(BuildContext context) {
    final label = selectedModel?.modelID ?? '默认模型';
    return InkWell(
      onTap: () => _showModelPicker(context),
      borderRadius: AppRadius.smRadius,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
        decoration: BoxDecoration(
          color: selectedModel != null
              ? AppColors.primaryLight
              : AppColors.inputBackground,
          borderRadius: AppRadius.smRadius,
          border: Border.all(
            color: selectedModel != null
                ? AppColors.primaryMuted
                : AppColors.border,
          ),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.auto_awesome_outlined,
              size: 13,
              color: selectedModel != null
                  ? AppColors.primary
                  : AppColors.textSecondary,
            ),
            const SizedBox(width: 4),
            Text(
              label,
              style: TextStyle(
                fontSize: 12,
                color: selectedModel != null
                    ? AppColors.primary
                    : AppColors.textSecondary,
              ),
            ),
            const SizedBox(width: 3),
            Icon(
              Icons.expand_more,
              size: 13,
              color: selectedModel != null
                  ? AppColors.primary
                  : AppColors.textMuted,
            ),
          ],
        ),
      ),
    );
  }

  void _showModelPicker(BuildContext context) {
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => _ModelPickerSheet(
        availableModels: availableModels,
        selectedModel: selectedModel,
        onSelect: (m) {
          Navigator.of(context).pop();
          onSelect(m);
        },
      ),
    );
  }
}

class _ModelLatencyTestButton extends StatelessWidget {
  final bool enabled;
  final VoidCallback onTap;

  const _ModelLatencyTestButton({required this.enabled, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final color = enabled ? AppColors.primary : AppColors.textMuted;
    return Tooltip(
      message: enabled ? '测试模型连接' : '请选择模型后测试',
      child: InkWell(
        onTap: enabled ? onTap : null,
        borderRadius: AppRadius.smRadius,
        child: Container(
          width: 28,
          height: 28,
          decoration: BoxDecoration(
            color: enabled ? AppColors.primaryLight : AppColors.inputBackground,
            borderRadius: AppRadius.smRadius,
            border: Border.all(
              color: enabled ? AppColors.primaryMuted : AppColors.border,
            ),
          ),
          child: Icon(Icons.speed_rounded, size: 15, color: color),
        ),
      ),
    );
  }
}

class _ModelLatencyTestSheet extends ConsumerStatefulWidget {
  final String agentId;
  final String projectId;
  final ModelInfo model;
  final String? variant;

  const _ModelLatencyTestSheet({
    required this.agentId,
    required this.projectId,
    required this.model,
    this.variant,
  });

  @override
  ConsumerState<_ModelLatencyTestSheet> createState() =>
      _ModelLatencyTestSheetState();
}

class _ModelLatencyTestSheetState
    extends ConsumerState<_ModelLatencyTestSheet> {
  final List<ModelLatencyLogEntry> _logs = [];
  ModelLatencyTestResult? _result;
  DateTime? _runStartedAt;
  Timer? _ticker;
  bool _running = false;
  int _runId = 0;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _startTest());
  }

  @override
  void dispose() {
    _runId++;
    _ticker?.cancel();
    super.dispose();
  }

  Future<void> _startTest() async {
    if (_running) return;
    final runId = ++_runId;
    setState(() {
      _running = true;
      _result = null;
      _runStartedAt = DateTime.now();
      _logs.clear();
    });
    _ticker?.cancel();
    _ticker = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted || runId != _runId || !_running) return;
      setState(() {});
    });
    final repo = ref.read(chatRepositoryProvider);
    final result = await repo.testModelLatency(
      agentId: widget.agentId,
      projectId: widget.projectId,
      model: widget.model,
      variant: widget.variant,
      onLog: (entry) {
        if (!mounted || runId != _runId) return;
        setState(() {
          _logs.add(entry);
        });
      },
    );
    if (!mounted || runId != _runId) return;
    _ticker?.cancel();
    setState(() {
      _running = false;
      _result = result;
      _runStartedAt = null;
      _logs
        ..clear()
        ..addAll(result.logs);
    });
  }

  @override
  Widget build(BuildContext context) {
    final modelLabel = widget.model.metaKey;
    final variant = widget.variant?.trim();
    final snapshot = _ModelLatencyLiveSnapshot.from(
      result: _result,
      logs: _logs,
      running: _running,
      runStartedAt: _runStartedAt,
    );
    return DraggableScrollableSheet(
      initialChildSize: 0.72,
      minChildSize: 0.42,
      maxChildSize: 0.92,
      expand: false,
      builder: (context, scrollController) {
        return Column(
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 10),
              child: Row(
                children: [
                  Container(
                    width: 34,
                    height: 34,
                    decoration: BoxDecoration(
                      color: AppColors.primaryLight,
                      borderRadius: AppRadius.smRadius,
                    ),
                    child: const Icon(
                      Icons.speed_rounded,
                      size: 18,
                      color: AppColors.primary,
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        const Text(
                          '模型连接测试',
                          style: TextStyle(
                            fontSize: 16,
                            fontWeight: FontWeight.w600,
                            color: AppColors.textPrimary,
                          ),
                        ),
                        const SizedBox(height: 3),
                        Text(
                          variant == null || variant.isEmpty
                              ? modelLabel
                              : '$modelLabel · ${thinkingVariantLabel(variant)}',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontSize: 12,
                            color: AppColors.textMuted,
                          ),
                        ),
                      ],
                    ),
                  ),
                  IconButton(
                    tooltip: '关闭',
                    onPressed: () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close_rounded),
                  ),
                ],
              ),
            ),
            const Divider(height: 1, color: AppColors.border),
            Expanded(
              child: ListView(
                controller: scrollController,
                padding: const EdgeInsets.all(16),
                children: [
                  _ModelLatencyPromptCard(running: _running),
                  const SizedBox(height: 12),
                  _ModelLatencyMetrics(snapshot: snapshot, running: _running),
                  const SizedBox(height: 12),
                  _ModelLatencyStageCard(
                    snapshot: snapshot,
                    logs: _logs,
                    running: _running,
                  ),
                  const SizedBox(height: 12),
                  _ModelLatencyLogCard(logs: _logs),
                ],
              ),
            ),
            const Divider(height: 1, color: AppColors.border),
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 10, 16, 12),
              child: Row(
                children: [
                  AppButton(
                    label: _running ? '测试中' : '重新测试',
                    onPressed: _running ? null : _startTest,
                  ),
                  const SizedBox(width: 8),
                  AppButton(
                    label: '复制日志',
                    outlined: true,
                    onPressed: _logs.isEmpty ? null : _copyLogs,
                  ),
                  const Spacer(),
                  AppButton(
                    label: '关闭',
                    outlined: true,
                    onPressed: () => Navigator.of(context).pop(),
                  ),
                ],
              ),
            ),
          ],
        );
      },
    );
  }

  Future<void> _copyLogs() async {
    await Clipboard.setData(ClipboardData(text: _buildLogText()));
    if (!mounted) return;
    showAppFeedback(context, message: '日志已复制');
  }

  String _buildLogText() {
    final buffer = StringBuffer();
    buffer.writeln('model=${widget.model.metaKey}');
    final variant = widget.variant?.trim();
    if (variant != null && variant.isNotEmpty) {
      buffer.writeln('variant=$variant');
    }
    final result = _result;
    if (result != null) {
      buffer.writeln('task_id=${result.taskId}');
      buffer.writeln('session_id=${result.sessionId}');
      buffer.writeln('success=${result.success}');
      if (result.error.isNotEmpty) buffer.writeln('error=${result.error}');
    }
    for (final entry in _logs) {
      buffer.writeln(
        '${_formatTime(entry.at)} [${entry.stage}] ${entry.message} ${entry.data}',
      );
    }
    return buffer.toString().trimRight();
  }
}

class _ModelLatencyPromptCard extends StatelessWidget {
  final bool running;

  const _ModelLatencyPromptCard({required this.running});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: AppColors.inputBackground,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.border),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            running ? Icons.sync_rounded : Icons.check_circle_outline_rounded,
            size: 16,
            color: running ? AppColors.primary : AppColors.statusSuccess,
          ),
          const SizedBox(width: 8),
          const Expanded(
            child: Text(
              '测试提示词：请只回复“连接正常”，并保持流式输出。',
              style: TextStyle(
                fontSize: 13,
                height: 1.45,
                color: AppColors.textSecondary,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _ModelLatencyMetrics extends StatelessWidget {
  final _ModelLatencyLiveSnapshot snapshot;
  final bool running;

  const _ModelLatencyMetrics({required this.snapshot, required this.running});

  @override
  Widget build(BuildContext context) {
    final items = [
      _MetricData('任务创建', snapshot.createTaskTime),
      _MetricData('首个事件', snapshot.firstEventTime),
      _MetricData('首字速度', snapshot.firstTextTime),
      _MetricData('总耗时', snapshot.totalTime),
    ];
    return LayoutBuilder(
      builder: (context, constraints) {
        final columns = constraints.maxWidth < 520 ? 2 : 4;
        return GridView.count(
          crossAxisCount: columns,
          shrinkWrap: true,
          physics: const NeverScrollableScrollPhysics(),
          crossAxisSpacing: 10,
          mainAxisSpacing: 10,
          childAspectRatio: columns == 2 ? 2.3 : 1.7,
          children: [
            for (final item in items)
              _ModelLatencyMetricTile(data: item, running: running),
          ],
        );
      },
    );
  }
}

class _ModelLatencyMetricTile extends StatelessWidget {
  final _MetricData data;
  final bool running;

  const _ModelLatencyMetricTile({required this.data, required this.running});

  @override
  Widget build(BuildContext context) {
    final value = data.duration == null
        ? (running ? '...' : '--')
        : _formatDuration(data.duration!);
    final color = data.duration == null
        ? AppColors.textMuted
        : data.duration!.inSeconds >= 10
        ? AppColors.statusWarning
        : AppColors.statusSuccess;
    return Container(
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: AppColors.surfaceElevated,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.borderLight),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Text(
            data.label,
            style: const TextStyle(fontSize: 11, color: AppColors.textMuted),
          ),
          const SizedBox(height: 5),
          Text(
            value,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(
              fontSize: 18,
              fontWeight: FontWeight.w700,
              color: color,
            ),
          ),
        ],
      ),
    );
  }
}

class _ModelLatencyStageCard extends StatelessWidget {
  final _ModelLatencyLiveSnapshot snapshot;
  final List<ModelLatencyLogEntry> logs;
  final bool running;

  const _ModelLatencyStageCard({
    required this.snapshot,
    required this.logs,
    required this.running,
  });

  @override
  Widget build(BuildContext context) {
    final rows = [
      _StageData('创建测试任务', snapshot.createTaskTime, _hasStage('task')),
      _StageData('设备端接收', null, _hasStage('launcher')),
      _StageData('打开事件流', snapshot.firstEventTime, _hasStage('sse')),
      _StageData('收到思考首包', snapshot.firstReasoningTime, _hasStage('reasoning')),
      _StageData('收到正文首字', snapshot.firstTextTime, _hasStage('text')),
      _StageData('完成测试', snapshot.totalTime, snapshot.completed),
    ];
    return Container(
      decoration: BoxDecoration(
        color: AppColors.surfaceElevated,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.borderLight),
      ),
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
            child: Row(
              children: [
                const Text(
                  '测试阶段',
                  style: TextStyle(
                    fontSize: 13,
                    fontWeight: FontWeight.w700,
                    color: AppColors.textPrimary,
                  ),
                ),
                const Spacer(),
                Text(
                  running
                      ? '测试中'
                      : !snapshot.completed
                      ? '待开始'
                      : snapshot.success
                      ? '连接正常'
                      : '测试失败',
                  style: TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                    color: running
                        ? AppColors.primary
                        : snapshot.success
                        ? AppColors.statusSuccess
                        : !snapshot.completed
                        ? AppColors.textMuted
                        : AppColors.statusError,
                  ),
                ),
              ],
            ),
          ),
          const Divider(height: 1, color: AppColors.borderLight),
          Padding(
            padding: const EdgeInsets.all(12),
            child: Column(
              children: [
                for (var i = 0; i < rows.length; i++)
                  Padding(
                    padding: EdgeInsets.only(
                      bottom: i == rows.length - 1 ? 0 : 10,
                    ),
                    child: _ModelLatencyStageRow(data: rows[i]),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  bool _hasStage(String stage) => logs.any((entry) => entry.stage == stage);
}

class _ModelLatencyStageRow extends StatelessWidget {
  final _StageData data;

  const _ModelLatencyStageRow({required this.data});

  @override
  Widget build(BuildContext context) {
    final color = data.done ? AppColors.statusSuccess : AppColors.textMuted;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(
          data.done
              ? Icons.check_circle_rounded
              : Icons.radio_button_unchecked_rounded,
          size: 16,
          color: color,
        ),
        const SizedBox(width: 8),
        Expanded(
          child: Text(
            data.label,
            style: TextStyle(
              fontSize: 13,
              height: 1.35,
              color: data.done ? AppColors.textPrimary : AppColors.textMuted,
            ),
          ),
        ),
        const SizedBox(width: 8),
        Text(
          data.duration == null ? '' : _formatDuration(data.duration!),
          style: const TextStyle(fontSize: 12, color: AppColors.textMuted),
        ),
      ],
    );
  }
}

class _ModelLatencyLogCard extends StatelessWidget {
  final List<ModelLatencyLogEntry> logs;

  const _ModelLatencyLogCard({required this.logs});

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: AppColors.surfaceElevated,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.borderLight),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Padding(
            padding: EdgeInsets.symmetric(horizontal: 12, vertical: 10),
            child: Text(
              '详细日志',
              style: TextStyle(
                fontSize: 13,
                fontWeight: FontWeight.w700,
                color: AppColors.textPrimary,
              ),
            ),
          ),
          const Divider(height: 1, color: AppColors.borderLight),
          if (logs.isEmpty)
            const Padding(
              padding: EdgeInsets.all(12),
              child: Text(
                '暂无日志',
                style: TextStyle(fontSize: 12, color: AppColors.textMuted),
              ),
            )
          else
            Padding(
              padding: const EdgeInsets.all(10),
              child: Column(
                children: [
                  for (var i = 0; i < logs.length; i++)
                    Padding(
                      padding: EdgeInsets.only(
                        bottom: i == logs.length - 1 ? 0 : 6,
                      ),
                      child: _ModelLatencyLogRow(entry: logs[i]),
                    ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class _ModelLatencyLogRow extends StatelessWidget {
  final ModelLatencyLogEntry entry;

  const _ModelLatencyLogRow({required this.entry});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      decoration: BoxDecoration(
        color: AppColors.surface,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.borderLight),
      ),
      child: SelectableText(
        '${_formatTime(entry.at)} [${entry.stage}] ${entry.message}'
        '${entry.data.isEmpty ? '' : ' ${entry.data}'}',
        style: const TextStyle(
          fontSize: 11,
          height: 1.45,
          color: AppColors.textSecondary,
          fontFamily: 'monospace',
        ),
      ),
    );
  }
}

class _MetricData {
  final String label;
  final Duration? duration;

  const _MetricData(this.label, this.duration);
}

class _ModelLatencyLiveSnapshot {
  final bool completed;
  final bool success;
  final Duration? createTaskTime;
  final Duration? firstEventTime;
  final Duration? firstReasoningTime;
  final Duration? firstTextTime;
  final Duration? totalTime;

  const _ModelLatencyLiveSnapshot({
    required this.completed,
    required this.success,
    this.createTaskTime,
    this.firstEventTime,
    this.firstReasoningTime,
    this.firstTextTime,
    this.totalTime,
  });

  factory _ModelLatencyLiveSnapshot.from({
    required ModelLatencyTestResult? result,
    required List<ModelLatencyLogEntry> logs,
    required bool running,
    required DateTime? runStartedAt,
  }) {
    final completed = result != null;
    return _ModelLatencyLiveSnapshot(
      completed: completed,
      success: result?.success ?? false,
      createTaskTime:
          result?.createTaskTime ?? _durationFromStage(logs, 'task'),
      firstEventTime: result?.firstEventTime ?? _durationFromStage(logs, 'sse'),
      firstReasoningTime:
          result?.firstReasoningTime ?? _durationFromStage(logs, 'reasoning'),
      firstTextTime: result?.firstTextTime ?? _durationFromStage(logs, 'text'),
      totalTime:
          result?.totalTime ?? _liveTotalTime(logs, running, runStartedAt),
    );
  }

  static Duration? _durationFromStage(
    List<ModelLatencyLogEntry> logs,
    String stage,
  ) {
    for (final entry in logs) {
      if (entry.stage != stage) continue;
      final value = entry.data['elapsed_ms'];
      if (value is int) return Duration(milliseconds: value);
      if (value is num) return Duration(milliseconds: value.round());
    }
    return null;
  }

  static Duration? _liveTotalTime(
    List<ModelLatencyLogEntry> logs,
    bool running,
    DateTime? runStartedAt,
  ) {
    final done =
        _durationFromStage(logs, 'done') ??
        _durationFromStage(logs, 'failed') ??
        _durationFromStage(logs, 'cancelled') ??
        _durationFromStage(logs, 'error');
    if (done != null) return done;
    if (running && runStartedAt != null) {
      return DateTime.now().difference(runStartedAt);
    }
    return null;
  }
}

class _StageData {
  final String label;
  final Duration? duration;
  final bool done;

  const _StageData(this.label, this.duration, this.done);
}

String _formatDuration(Duration duration) {
  final safe = duration.isNegative ? Duration.zero : duration;
  if (safe.inMilliseconds < 1000) return '${safe.inMilliseconds}ms';
  if (safe.inSeconds < 60) {
    return '${(safe.inMilliseconds / 1000).toStringAsFixed(1)}s';
  }
  final minutes = safe.inMinutes;
  final seconds = safe.inSeconds.remainder(60).toString().padLeft(2, '0');
  return '$minutes:$seconds';
}

ContextUsageInfo? _contextUsageWithModelLimit(
  ContextUsageInfo? usage,
  int? modelLimit,
) {
  return ContextUsageInfo.withModelLimit(usage, modelLimit);
}

String _formatTime(DateTime time) {
  final local = time.toLocal();
  String two(int value) => value.toString().padLeft(2, '0');
  String three(int value) => value.toString().padLeft(3, '0');
  return '${two(local.hour)}:${two(local.minute)}:${two(local.second)}.${three(local.millisecond)}';
}

class _ModelPickerSheet extends StatefulWidget {
  final List<ModelInfo> availableModels;
  final ModelInfo? selectedModel;
  final void Function(ModelInfo? model) onSelect;

  const _ModelPickerSheet({
    required this.availableModels,
    required this.selectedModel,
    required this.onSelect,
  });

  @override
  State<_ModelPickerSheet> createState() => _ModelPickerSheetState();
}

class _ModelPickerSheetState extends State<_ModelPickerSheet> {
  String _search = '';

  @override
  Widget build(BuildContext context) {
    final filtered = widget.availableModels.where((m) {
      if (_search.isEmpty) return true;
      final q = _search.toLowerCase();
      return m.modelID.toLowerCase().contains(q) ||
          m.providerID.toLowerCase().contains(q) ||
          m.name.toLowerCase().contains(q);
    }).toList();

    // 按 providerID 分组
    final grouped = <String, List<ModelInfo>>{};
    for (final m in filtered) {
      grouped.putIfAbsent(m.providerID, () => []).add(m);
    }

    return DraggableScrollableSheet(
      initialChildSize: 0.6,
      maxChildSize: 0.9,
      minChildSize: 0.3,
      expand: false,
      builder: (_, scrollCtrl) => Column(
        children: [
          // 标题栏
          Container(
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
            child: Column(
              children: [
                Row(
                  children: [
                    const Text(
                      '选择模型',
                      style: TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w600,
                        color: AppColors.textPrimary,
                      ),
                    ),
                    const Spacer(),
                    if (widget.selectedModel != null)
                      TextButton(
                        onPressed: () => widget.onSelect(null),
                        child: const Text('重置为默认'),
                      ),
                  ],
                ),
                const SizedBox(height: 8),
                TextField(
                  autofocus: false,
                  decoration: InputDecoration(
                    hintText: '搜索模型...',
                    prefixIcon: const Icon(Icons.search, size: 18),
                    border: OutlineInputBorder(
                      borderRadius: AppRadius.smRadius,
                      borderSide: const BorderSide(color: AppColors.border),
                    ),
                    enabledBorder: OutlineInputBorder(
                      borderRadius: AppRadius.smRadius,
                      borderSide: const BorderSide(color: AppColors.border),
                    ),
                    contentPadding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 8,
                    ),
                    isDense: true,
                    filled: true,
                    fillColor: AppColors.inputBackground,
                  ),
                  onChanged: (v) => setState(() => _search = v),
                ),
              ],
            ),
          ),
          const Divider(height: 1, color: AppColors.border),
          // 模型列表
          Expanded(
            child: filtered.isEmpty
                ? Center(
                    child: Text(
                      widget.availableModels.isEmpty
                          ? '暂无可用模型\n请确保 opencode 服务已启动'
                          : '无匹配模型',
                      textAlign: TextAlign.center,
                      style: const TextStyle(
                        color: AppColors.textMuted,
                        fontSize: 13,
                      ),
                    ),
                  )
                : ListView.builder(
                    controller: scrollCtrl,
                    itemCount: grouped.length,
                    itemBuilder: (_, gi) {
                      final provider = grouped.keys.elementAt(gi);
                      final models = grouped[provider]!;
                      return Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Padding(
                            padding: const EdgeInsets.fromLTRB(16, 8, 8, 2),
                            child: Row(
                              children: [
                                Expanded(
                                  child: Text(
                                    provider,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: const TextStyle(
                                      fontSize: 11,
                                      fontWeight: FontWeight.w600,
                                      color: AppColors.textMuted,
                                      letterSpacing: 0.5,
                                    ),
                                  ),
                                ),
                                IconButton(
                                  tooltip: '打开供应商控制台',
                                  visualDensity: VisualDensity.compact,
                                  iconSize: 18,
                                  icon: const Icon(
                                    Icons.open_in_browser_rounded,
                                  ),
                                  onPressed: () {
                                    final model = models.first;
                                    openProviderConsole(
                                      context,
                                      consoleUrl: model.providerConsoleUrl,
                                      baseUrl: model.providerBaseUrl,
                                    );
                                  },
                                ),
                              ],
                            ),
                          ),
                          ...models.map((m) {
                            final isSelected =
                                widget.selectedModel?.modelID == m.modelID &&
                                widget.selectedModel?.providerID ==
                                    m.providerID;
                            return InkWell(
                              onTap: () => widget.onSelect(m),
                              child: Container(
                                padding: const EdgeInsets.symmetric(
                                  horizontal: 16,
                                  vertical: 10,
                                ),
                                color: isSelected
                                    ? AppColors.primaryLight
                                    : Colors.transparent,
                                child: Row(
                                  children: [
                                    Expanded(
                                      child: Row(
                                        crossAxisAlignment:
                                            CrossAxisAlignment.baseline,
                                        textBaseline:
                                            TextBaseline.alphabetic,
                                        children: [
                                          Flexible(
                                            child: Text(
                                              m.modelID,
                                              maxLines: 1,
                                              overflow: TextOverflow.ellipsis,
                                              style: TextStyle(
                                                fontSize: 13,
                                                fontWeight: isSelected
                                                    ? FontWeight.w600
                                                    : FontWeight.w400,
                                                color: isSelected
                                                    ? AppColors.primary
                                                    : AppColors.textPrimary,
                                              ),
                                            ),
                                          ),
                                          if (m.contextWindowShortLabel !=
                                              '窗口未知') ...[
                                            const SizedBox(width: 6),
                                            Text(
                                              m.contextWindowShortLabel,
                                              style: const TextStyle(
                                                fontSize: 11,
                                                color: AppColors.textMuted,
                                              ),
                                            ),
                                          ],
                                        ],
                                      ),
                                    ),
                                    if (isSelected)
                                      const Icon(
                                        Icons.check,
                                        size: 16,
                                        color: AppColors.primary,
                                      ),
                                  ],
                                ),
                              ),
                            );
                          }),
                        ],
                      );
                    },
                  ),
          ),
        ],
      ),
    );
  }
}

class _ContextUsageButton extends StatelessWidget {
  final ContextUsageInfo? usage;
  final int draftTokens;
  final int configuredThresholdPercent;
  final bool savingThreshold;
  final bool canCompact;
  final VoidCallback onCompact;
  final ValueChanged<int> onSaveThreshold;

  const _ContextUsageButton({
    required this.usage,
    required this.draftTokens,
    required this.configuredThresholdPercent,
    required this.savingThreshold,
    required this.canCompact,
    required this.onCompact,
    required this.onSaveThreshold,
  });

  bool get _hasRealUsage => (usage?.knownContextTokens ?? 0) > 0;

  int get _contextTokens => _hasRealUsage ? usage!.knownContextTokens : 0;

  int get _contextLimit => usage?.contextLimit ?? 0;

  int? get _thresholdTokens {
    final fromUsage = usage?.compactionThresholdTokens;
    if (fromUsage != null && fromUsage > 0) return fromUsage;
    return null;
  }

  double get _percent {
    if (!_hasRealUsage || _contextLimit <= 0) return 0;
    return ((_contextTokens / _contextLimit) * 100).clamp(0, 999).toDouble();
  }

  double get _activeThresholdPercent =>
      usage?.compactionThresholdPercent ??
      configuredThresholdPercent.clamp(1, 100).toDouble();

  Color get _color {
    if (!_hasRealUsage || _contextLimit <= 0) {
      return AppColors.textMuted;
    }
    if (_percent >= _activeThresholdPercent) return AppColors.statusError;
    if (_percent >= _activeThresholdPercent - 10) {
      return AppColors.statusWarning;
    }
    return AppColors.primary;
  }

  String get _label {
    if (!_hasRealUsage || _contextLimit <= 0) return '--';
    return _percent.round().clamp(0, 999).toString();
  }

  void _showDetails(BuildContext context) {
    showDialog<void>(
      context: context,
      builder: (dialogContext) => Dialog(
        backgroundColor: Colors.transparent,
        elevation: 0,
        insetPadding: const EdgeInsets.all(16),
        child: _ContextUsagePanel(
          usage: usage,
          draftTokens: draftTokens,
          contextTokens: _contextTokens,
          contextLimit: _contextLimit,
          thresholdTokens: _thresholdTokens,
          percent: _percent,
          color: _color,
          configuredThresholdPercent: configuredThresholdPercent,
          savingThreshold: savingThreshold,
          canCompact: canCompact,
          onCompact: () {
            Navigator.of(dialogContext).pop();
            onCompact();
          },
          onSaveThreshold: onSaveThreshold,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final value = !_hasRealUsage || _contextLimit <= 0
        ? null
        : (_percent / 100).clamp(0, 1).toDouble();
    return Tooltip(
      message: '上下文用量',
      child: Semantics(
        button: true,
        label: '查看上下文用量',
        child: GestureDetector(
          behavior: HitTestBehavior.opaque,
          onTap: () => _showDetails(context),
          child: SizedBox(
            width: 34,
            height: 34,
            child: Center(
              child: SizedBox(
                width: 25,
                height: 25,
                child: Stack(
                  alignment: Alignment.center,
                  children: [
                    CircularProgressIndicator(
                      value: value,
                      strokeWidth: 4,
                      backgroundColor: AppColors.borderLight,
                      valueColor: AlwaysStoppedAnimation<Color>(_color),
                    ),
                    Text(
                      _label,
                      style: TextStyle(
                        fontSize: 9,
                        fontWeight: FontWeight.w700,
                        color: _contextLimit <= 0
                            ? AppColors.textMuted
                            : AppColors.textPrimary,
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _ContextUsagePanel extends StatefulWidget {
  final ContextUsageInfo? usage;
  final int draftTokens;
  final int contextTokens;
  final int contextLimit;
  final int? thresholdTokens;
  final double percent;
  final Color color;
  final int configuredThresholdPercent;
  final bool savingThreshold;
  final bool canCompact;
  final VoidCallback onCompact;
  final ValueChanged<int> onSaveThreshold;

  const _ContextUsagePanel({
    required this.usage,
    required this.draftTokens,
    required this.contextTokens,
    required this.contextLimit,
    required this.thresholdTokens,
    required this.percent,
    required this.color,
    required this.configuredThresholdPercent,
    required this.savingThreshold,
    required this.canCompact,
    required this.onCompact,
    required this.onSaveThreshold,
  });

  @override
  State<_ContextUsagePanel> createState() => _ContextUsagePanelState();
}

class _ContextUsagePanelState extends State<_ContextUsagePanel> {
  late double _configuredThreshold;

  @override
  void initState() {
    super.initState();
    _configuredThreshold = widget.configuredThresholdPercent
        .clamp(1, 100)
        .toDouble();
  }

  @override
  void didUpdateWidget(covariant _ContextUsagePanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.configuredThresholdPercent !=
        widget.configuredThresholdPercent) {
      _configuredThreshold = widget.configuredThresholdPercent
          .clamp(1, 100)
          .toDouble();
    }
  }

  double get _activeThresholdPercent =>
      widget.usage?.compactionThresholdPercent ??
      widget.configuredThresholdPercent.clamp(1, 100).toDouble();
  String _formatTokens(int? value) {
    if (value == null || value <= 0) return '--';
    if (value >= 1000) {
      final k = value / 1000;
      return '${k >= 10 ? k.toStringAsFixed(0) : k.toStringAsFixed(1)}k';
    }
    return value.toString();
  }

  String _formatPercent(num? value) {
    if (value == null || value <= 0) return '--';
    return '${value.toStringAsFixed(value >= 10 ? 0 : 1)}%';
  }

  String get _statusText {
    if (widget.contextLimit <= 0) {
      return '等待统计';
    }
    if (widget.contextTokens <= 0) {
      return '等待用量';
    }
    if (widget.percent >= _activeThresholdPercent) return '需要压缩';
    if (widget.percent >= _activeThresholdPercent - 10) return '接近阈值';
    return '正常';
  }

  Color get _statusBackground {
    if (widget.color == AppColors.textMuted) {
      return AppColors.statusOfflineLight;
    }
    if (widget.contextTokens <= 0) {
      return AppColors.statusOfflineLight;
    }
    if (widget.percent >= _activeThresholdPercent) {
      return AppColors.statusErrorLight;
    }
    if (widget.percent >= _activeThresholdPercent - 10) {
      return AppColors.statusWarningLight;
    }
    return AppColors.primaryLight;
  }

  String get _thresholdSourceText {
    if (widget.contextLimit <= 0) {
      return '等待后端返回模型限制';
    }
    return '圆环显示模型真实上下文使用比例，自动压缩阈值单独计算。';
  }

  @override
  Widget build(BuildContext context) {
    final threshold =
        widget.thresholdTokens ??
        (widget.contextLimit > 0
            ? (widget.contextLimit * _activeThresholdPercent / 100).floor()
            : null);
    final rawRemaining = threshold == null
        ? null
        : threshold - widget.contextTokens;
    final remaining = rawRemaining?.clamp(0, 1 << 31).toInt();
    final overThreshold = rawRemaining != null && rawRemaining <= 0;
    final hintText = widget.contextLimit <= 0
        ? '等待用量更新后，会显示自动压缩距离。'
        : widget.contextTokens <= 0
        ? '等待用量更新后显示真实上下文比例。'
        : overThreshold
        ? '当前上下文已超过真实压缩阈值，下一次发送会保留摘要和最近对话。'
        : '当前使用 ${_formatPercent(widget.percent)}，自动压缩阈值 ${_formatPercent(_activeThresholdPercent)}。';
    final panelWidth = (MediaQuery.sizeOf(context).width - 32).clamp(
      280.0,
      320.0,
    );
    return SingleChildScrollView(
      child: Container(
        width: panelWidth,
        decoration: BoxDecoration(
          color: AppColors.surface,
          borderRadius: AppRadius.mdRadius,
          border: Border.all(color: AppColors.border),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: 0.10),
              blurRadius: 30,
              offset: const Offset(0, 16),
            ),
          ],
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(14, 12, 14, 10),
              child: Row(
                children: [
                  const Expanded(
                    child: Text(
                      '上下文使用量',
                      style: TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w700,
                        color: AppColors.textPrimary,
                      ),
                    ),
                  ),
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 3,
                    ),
                    decoration: BoxDecoration(
                      color: _statusBackground,
                      borderRadius: BorderRadius.circular(999),
                    ),
                    child: Text(
                      _statusText,
                      style: TextStyle(
                        fontSize: 11,
                        fontWeight: FontWeight.w700,
                        color: widget.color,
                      ),
                    ),
                  ),
                ],
              ),
            ),
            const Divider(height: 1, color: AppColors.borderLight),
            Padding(
              padding: const EdgeInsets.all(14),
              child: Row(
                children: [
                  SizedBox(
                    width: 64,
                    height: 64,
                    child: Stack(
                      alignment: Alignment.center,
                      children: [
                        CircularProgressIndicator(
                          value:
                              widget.contextLimit <= 0 ||
                                  widget.contextTokens <= 0
                              ? null
                              : (widget.percent / 100).clamp(0, 1).toDouble(),
                          strokeWidth: 7,
                          backgroundColor: AppColors.borderLight,
                          valueColor: AlwaysStoppedAnimation<Color>(
                            widget.color,
                          ),
                        ),
                        Text(
                          widget.contextLimit <= 0
                              ? '--'
                              : widget.contextTokens <= 0
                              ? '--'
                              : _formatPercent(widget.percent),
                          style: const TextStyle(
                            fontSize: 15,
                            fontWeight: FontWeight.w800,
                            color: AppColors.textPrimary,
                          ),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(width: 14),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '${_formatTokens(widget.contextTokens)} / ${_formatTokens(widget.contextLimit)} tokens',
                          style: const TextStyle(
                            fontSize: 13,
                            fontWeight: FontWeight.w700,
                            color: AppColors.textPrimary,
                          ),
                        ),
                        const SizedBox(height: 4),
                        Text(
                          _thresholdSourceText,
                          style: TextStyle(
                            fontSize: 12,
                            height: 1.45,
                            color: AppColors.textSecondary,
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(14, 0, 14, 14),
              child: Column(
                children: [
                  _ContextUsageMetric(
                    label: '当前已用上下文',
                    value: _formatTokens(widget.contextTokens),
                    percent:
                        widget.contextLimit <= 0 || widget.contextTokens <= 0
                        ? 0
                        : widget.contextTokens / widget.contextLimit,
                    color: AppColors.primary,
                  ),
                  _ContextUsageMetric(
                    label: '本次输入',
                    value: _formatTokens(widget.draftTokens),
                    percent: widget.contextLimit <= 0
                        ? 0
                        : widget.draftTokens / widget.contextLimit,
                    color: AppColors.primary,
                  ),
                  _ContextUsageMetric(
                    label: '自动压缩阈值',
                    value: _formatPercent(_activeThresholdPercent),
                    percent: _activeThresholdPercent / 100,
                    color: widget.color,
                  ),
                  _ContextUsageMetric(
                    label: '距离压缩',
                    value: remaining == null
                        ? '--'
                        : '${_formatTokens(remaining)} · ${_formatPercent(((_activeThresholdPercent - widget.percent).clamp(0, 100)))}',
                    percent: widget.contextLimit <= 0 || remaining == null
                        ? 0
                        : remaining / widget.contextLimit,
                    color: widget.color,
                  ),
                  _ContextUsageMetric(
                    label: '模型最大上下文',
                    value: _formatTokens(widget.contextLimit),
                    percent: widget.contextLimit <= 0 ? 0 : 1,
                    color: AppColors.primary,
                  ),
                  Container(
                    width: double.infinity,
                    margin: const EdgeInsets.only(top: 8),
                    padding: const EdgeInsets.all(10),
                    decoration: BoxDecoration(
                      color: AppColors.primaryLight.withValues(alpha: 0.55),
                      borderRadius: AppRadius.smRadius,
                      border: Border.all(
                        color: AppColors.primary.withValues(alpha: 0.14),
                      ),
                    ),
                    child: Text(
                      hintText,
                      style: const TextStyle(
                        fontSize: 12,
                        height: 1.45,
                        color: AppColors.textSecondary,
                      ),
                    ),
                  ),
                  const SizedBox(height: 12),
                  Row(
                    children: [
                      const Expanded(
                        child: Text(
                          '自动压缩比例',
                          style: TextStyle(
                            fontSize: 12,
                            fontWeight: FontWeight.w700,
                            color: AppColors.textPrimary,
                          ),
                        ),
                      ),
                      Text(
                        '${_configuredThreshold.round()}%',
                        style: const TextStyle(
                          fontSize: 12,
                          fontWeight: FontWeight.w700,
                          color: AppColors.textPrimary,
                        ),
                      ),
                    ],
                  ),
                  Slider(
                    value: _configuredThreshold,
                    min: 1,
                    max: 100,
                    divisions: 99,
                    label: '${_configuredThreshold.round()}%',
                    onChanged: widget.savingThreshold
                        ? null
                        : (value) =>
                              setState(() => _configuredThreshold = value),
                    onChangeEnd: widget.savingThreshold
                        ? null
                        : (value) => widget.onSaveThreshold(value.round()),
                  ),
                  const Align(
                    alignment: Alignment.centerLeft,
                    child: Text(
                      '保存后在下次 Agent 启动时生效，不会中断当前任务。',
                      style: TextStyle(
                        fontSize: 11,
                        height: 1.4,
                        color: AppColors.textMuted,
                      ),
                    ),
                  ),
                  const SizedBox(height: 12),
                  SizedBox(
                    width: double.infinity,
                    child: FilledButton.icon(
                      onPressed: widget.canCompact ? widget.onCompact : null,
                      icon: const Icon(Icons.compress, size: 17),
                      label: const Text('压缩上下文'),
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ContextUsageMetric extends StatelessWidget {
  final String label;
  final String value;
  final double percent;
  final Color color;

  const _ContextUsageMetric({
    required this.label,
    required this.value,
    required this.percent,
    required this.color,
  });

  @override
  Widget build(BuildContext context) {
    final progress = percent.clamp(0, 1).toDouble();
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Column(
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  label,
                  style: const TextStyle(
                    fontSize: 12,
                    color: AppColors.textSecondary,
                  ),
                ),
              ),
              Text(
                value,
                style: const TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w700,
                  color: AppColors.textPrimary,
                ),
              ),
            ],
          ),
          const SizedBox(height: 5),
          ClipRRect(
            borderRadius: BorderRadius.circular(99),
            child: LinearProgressIndicator(
              value: progress,
              minHeight: 5,
              backgroundColor: AppColors.borderLight,
              valueColor: AlwaysStoppedAnimation<Color>(color),
            ),
          ),
        ],
      ),
    );
  }
}

class _QueueTray extends StatelessWidget {
  final String agentId;
  final String projectId;
  final List<ChatQueueItem> items;
  final bool loading;
  final String? error;
  final bool taskActive;
  final List<ModelInfo> availableModels;
  final ValueChanged<ChatQueueItem> onDelete;
  final ValueChanged<ChatQueueItem> onInsert;
  final ValueChanged<ChatQueueItem> onSend;
  final ValueChanged<List<ChatQueueItem>> onReorder;
  final void Function(ChatQueueItem item, ModelInfo? model, String? variant)
  onUpdateModel;

  const _QueueTray({
    required this.agentId,
    required this.projectId,
    required this.items,
    required this.loading,
    required this.error,
    required this.taskActive,
    required this.availableModels,
    required this.onDelete,
    required this.onInsert,
    required this.onSend,
    required this.onReorder,
    required this.onUpdateModel,
  });

  @override
  Widget build(BuildContext context) {
    // 队列为空时不展示队列条：连接层面的错误不应渲染成「队列状态异常」。
    if (items.isEmpty) return const SizedBox.shrink();

    if (AppBreakpoints.isMobile(context)) {
      final preview = items.first.text.replaceAll('\n', ' ');
      return Material(
        color: Colors.transparent,
        child: InkWell(
          onTap: () => _showMobileQueue(context),
          borderRadius: AppRadius.smRadius,
          child: Container(
            height: 38,
            padding: const EdgeInsets.symmetric(horizontal: 10),
            decoration: BoxDecoration(
              color: AppColors.inputBackground,
              borderRadius: AppRadius.smRadius,
              border: Border.all(
                color: error == null
                    ? AppColors.border
                    : AppColors.statusError.withValues(alpha: 0.45),
              ),
            ),
            child: Row(
              children: [
                const Icon(
                  Icons.schedule_send_outlined,
                  size: 15,
                  color: AppColors.textSecondary,
                ),
                const SizedBox(width: 7),
                Text(
                  '待发送 ${items.length}',
                  style: const TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                    color: AppColors.textPrimary,
                  ),
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    preview,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 11,
                      color: error == null
                          ? AppColors.textMuted
                          : AppColors.statusError,
                    ),
                  ),
                ),
                const Icon(
                  Icons.keyboard_arrow_up_rounded,
                  size: 18,
                  color: AppColors.textSecondary,
                ),
              ],
            ),
          ),
        ),
      );
    }

    return Container(
      decoration: BoxDecoration(
        color: AppColors.inputBackground,
        borderRadius: AppRadius.smRadius,
        border: Border.all(
          color: error == null
              ? AppColors.border
              : AppColors.statusError.withValues(alpha: 0.45),
        ),
      ),
      child: Column(
        children: [
          _QueueHeader(count: items.length, loading: loading, error: error),
          if (items.isNotEmpty) ...[
            const Divider(height: 1, color: AppColors.border),
            SizedBox(
              height: (items.length * 62.0).clamp(62.0, 186.0),
              child: _QueueList(
                items: items,
                taskActive: taskActive,
                availableModels: availableModels,
                onDelete: onDelete,
                onInsert: onInsert,
                onSend: onSend,
                onReorder: onReorder,
                onUpdateModel: onUpdateModel,
              ),
            ),
          ],
        ],
      ),
    );
  }

  void _showMobileQueue(BuildContext context) {
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: AppColors.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => Consumer(
        builder: (context, ref, _) {
          final chat = ref.watch(chatProvider((agentId, projectId)));
          final liveItems = chat.queue.queuedItems;
          return DraggableScrollableSheet(
            expand: false,
            initialChildSize: 0.58,
            minChildSize: 0.3,
            maxChildSize: 0.88,
            builder: (sheetContext, scrollController) => SafeArea(
              top: false,
              child: Column(
                children: [
                  Container(
                    width: 34,
                    height: 4,
                    margin: const EdgeInsets.only(top: 9, bottom: 7),
                    decoration: BoxDecoration(
                      color: AppColors.border,
                      borderRadius: BorderRadius.circular(2),
                    ),
                  ),
                  _QueueHeader(
                    count: liveItems.length,
                    loading: chat.queueLoading,
                    error: chat.queueError,
                  ),
                  const Divider(height: 1, color: AppColors.border),
                  Expanded(
                    child: liveItems.isEmpty
                        ? const Center(
                            child: Text(
                              '暂无待发送消息',
                              style: TextStyle(
                                fontSize: 13,
                                color: AppColors.textMuted,
                              ),
                            ),
                          )
                        : _QueueList(
                            items: liveItems,
                            taskActive: chat.taskActive,
                            availableModels: availableModels,
                            scrollController: scrollController,
                            onDelete: onDelete,
                            onInsert: onInsert,
                            onSend: onSend,
                            onReorder: onReorder,
                            onUpdateModel: onUpdateModel,
                          ),
                  ),
                ],
              ),
            ),
          );
        },
      ),
    );
  }
}

class _QueueHeader extends StatelessWidget {
  final int count;
  final bool loading;
  final String? error;

  const _QueueHeader({
    required this.count,
    required this.loading,
    required this.error,
  });

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: 38,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 10),
        child: Row(
          children: [
            const Icon(
              Icons.schedule_send_outlined,
              size: 15,
              color: AppColors.textSecondary,
            ),
            const SizedBox(width: 7),
            Text(
              '待发送 $count',
              style: const TextStyle(
                fontSize: 12,
                fontWeight: FontWeight.w600,
                color: AppColors.textPrimary,
              ),
            ),
            const Spacer(),
            if (error != null)
              Flexible(
                child: Text(
                  error!,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 11,
                    color: AppColors.statusError,
                  ),
                ),
              )
            else if (loading)
              const SizedBox(
                width: 14,
                height: 14,
                child: CircularProgressIndicator(strokeWidth: 1.5),
              ),
          ],
        ),
      ),
    );
  }
}

class _QueueList extends StatelessWidget {
  final List<ChatQueueItem> items;
  final bool taskActive;
  final List<ModelInfo> availableModels;
  final ScrollController? scrollController;
  final ValueChanged<ChatQueueItem> onDelete;
  final ValueChanged<ChatQueueItem> onInsert;
  final ValueChanged<ChatQueueItem> onSend;
  final ValueChanged<List<ChatQueueItem>> onReorder;
  final void Function(ChatQueueItem item, ModelInfo? model, String? variant)
  onUpdateModel;

  const _QueueList({
    required this.items,
    required this.taskActive,
    required this.availableModels,
    this.scrollController,
    required this.onDelete,
    required this.onInsert,
    required this.onSend,
    required this.onReorder,
    required this.onUpdateModel,
  });

  @override
  Widget build(BuildContext context) {
    final canReorder = items.every((item) => item.isEditable);
    return ReorderableListView.builder(
      scrollController: scrollController,
      buildDefaultDragHandles: false,
      padding: EdgeInsets.zero,
      itemCount: items.length,
      onReorder: canReorder
          ? (oldIndex, newIndex) {
              if (newIndex > oldIndex) newIndex -= 1;
              final next = List<ChatQueueItem>.from(items);
              final item = next.removeAt(oldIndex);
              next.insert(newIndex, item);
              onReorder(next);
            }
          : (_, _) {},
      itemBuilder: (context, index) {
        final item = items[index];
        return _QueueItemRow(
          key: ValueKey(item.id),
          item: item,
          index: index,
          canReorder: canReorder,
          taskActive: taskActive,
          availableModels: availableModels,
          onDelete: () => onDelete(item),
          onInsert: () => onInsert(item),
          onSend: () => onSend(item),
          onUpdateModel: (model, variant) =>
              onUpdateModel(item, model, variant),
        );
      },
    );
  }
}

class _QueueItemRow extends StatelessWidget {
  final ChatQueueItem item;
  final int index;
  final bool canReorder;
  final bool taskActive;
  final List<ModelInfo> availableModels;
  final VoidCallback onDelete;
  final VoidCallback onInsert;
  final VoidCallback onSend;
  final void Function(ModelInfo? model, String? variant) onUpdateModel;

  const _QueueItemRow({
    super.key,
    required this.item,
    required this.index,
    required this.canReorder,
    required this.taskActive,
    required this.availableModels,
    required this.onDelete,
    required this.onInsert,
    required this.onSend,
    required this.onUpdateModel,
  });

  @override
  Widget build(BuildContext context) {
    final inserting = item.status == ChatQueueItemStatus.inserting;
    final modelLabel = item.modelRef.isEmpty
        ? '默认模型'
        : item.modelRef.split('/').last;
    return Material(
      color: AppColors.surface,
      child: Container(
        constraints: const BoxConstraints(minHeight: 62),
        padding: const EdgeInsets.fromLTRB(8, 7, 7, 7),
        decoration: const BoxDecoration(
          border: Border(bottom: BorderSide(color: AppColors.borderLight)),
        ),
        child: Row(
          children: [
            if (canReorder)
              ReorderableDragStartListener(
                index: index,
                child: const Padding(
                  padding: EdgeInsets.symmetric(horizontal: 2, vertical: 10),
                  child: Icon(
                    Icons.drag_indicator_rounded,
                    size: 17,
                    color: AppColors.textMuted,
                  ),
                ),
              )
            else
              const SizedBox(width: 21),
            const SizedBox(width: 4),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Text(
                    item.text.isEmpty ? '附件消息' : item.text,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      fontSize: 12,
                      fontWeight: FontWeight.w500,
                      color: AppColors.textPrimary,
                    ),
                  ),
                  const SizedBox(height: 4),
                  Row(
                    children: [
                      InkWell(
                        onTap: item.isEditable
                            ? () => _showQueueModelEditor(context)
                            : null,
                        borderRadius: AppRadius.smRadius,
                        child: Padding(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 4,
                            vertical: 2,
                          ),
                          child: Text(
                            item.variant.isEmpty
                                ? modelLabel
                                : '$modelLabel · ${thinkingVariantLabel(item.variant)}',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 10,
                              color: AppColors.primary,
                            ),
                          ),
                        ),
                      ),
                      if (item.attachmentCount > 0) ...[
                        const SizedBox(width: 6),
                        const Icon(
                          Icons.attach_file_rounded,
                          size: 11,
                          color: AppColors.textMuted,
                        ),
                        Text(
                          '${item.attachmentCount}',
                          style: const TextStyle(
                            fontSize: 10,
                            color: AppColors.textMuted,
                          ),
                        ),
                      ],
                      if (item.error.isNotEmpty) ...[
                        const SizedBox(width: 6),
                        Expanded(
                          child: Text(
                            item.error,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 10,
                              color: AppColors.statusError,
                            ),
                          ),
                        ),
                      ],
                    ],
                  ),
                ],
              ),
            ),
            const SizedBox(width: 6),
            if (inserting)
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 9),
                child: SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 1.6),
                ),
              )
            else ...[
              Tooltip(
                message: '直接发送',
                child: IconButton(
                  onPressed: item.isEditable && !taskActive ? onSend : null,
                  visualDensity: VisualDensity.compact,
                  constraints: const BoxConstraints.tightFor(
                    width: 34,
                    height: 34,
                  ),
                  icon: const Icon(Icons.send_rounded, size: 18),
                  color: AppColors.primary,
                  disabledColor: AppColors.textMuted,
                ),
              ),
              Tooltip(
                message: '插入当前对话',
                child: IconButton(
                  onPressed: taskActive && item.isEditable ? onInsert : null,
                  visualDensity: VisualDensity.compact,
                  constraints: const BoxConstraints.tightFor(
                    width: 34,
                    height: 34,
                  ),
                  icon: const Icon(Icons.keyboard_return_rounded, size: 18),
                  color: AppColors.primary,
                  disabledColor: AppColors.textMuted,
                ),
              ),
              Tooltip(
                message: '删除',
                child: IconButton(
                  onPressed: item.isEditable ? onDelete : null,
                  visualDensity: VisualDensity.compact,
                  constraints: const BoxConstraints.tightFor(
                    width: 34,
                    height: 34,
                  ),
                  icon: const Icon(Icons.delete_outline_rounded, size: 18),
                  color: AppColors.textSecondary,
                  disabledColor: AppColors.textMuted,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  void _showQueueModelEditor(BuildContext context) {
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: AppColors.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => _QueueItemModelSheet(
        item: item,
        availableModels: availableModels,
        onApply: onUpdateModel,
      ),
    );
  }
}

class _QueueItemModelSheet extends StatefulWidget {
  final ChatQueueItem item;
  final List<ModelInfo> availableModels;
  final void Function(ModelInfo? model, String? variant) onApply;

  const _QueueItemModelSheet({
    required this.item,
    required this.availableModels,
    required this.onApply,
  });

  @override
  State<_QueueItemModelSheet> createState() => _QueueItemModelSheetState();
}

class _QueueItemModelSheetState extends State<_QueueItemModelSheet> {
  ModelInfo? _model;
  String? _variant;

  @override
  void initState() {
    super.initState();
    for (final model in widget.availableModels) {
      if (model.metaKey == widget.item.modelRef) {
        _model = model;
        break;
      }
    }
    _variant = normalizeThinkingVariant(
      _model?.variants ?? const <String>[],
      widget.item.variant,
    );
  }

  @override
  Widget build(BuildContext context) {
    final variants = _model?.variants ?? const <String>[];
    final label =
        _model?.modelID ??
        (widget.item.modelRef.isEmpty ? '默认模型' : widget.item.modelRef);
    return SafeArea(
      top: false,
      child: Padding(
        padding: EdgeInsets.fromLTRB(
          16,
          16,
          16,
          MediaQuery.viewInsetsOf(context).bottom + 16,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              '队列消息模型',
              style: TextStyle(
                fontSize: 16,
                fontWeight: FontWeight.w600,
                color: AppColors.textPrimary,
              ),
            ),
            const SizedBox(height: 14),
            InkWell(
              onTap: _showModelPicker,
              borderRadius: AppRadius.smRadius,
              child: Container(
                height: 44,
                padding: const EdgeInsets.symmetric(horizontal: 12),
                decoration: BoxDecoration(
                  color: AppColors.inputBackground,
                  borderRadius: AppRadius.smRadius,
                  border: Border.all(color: AppColors.border),
                ),
                child: Row(
                  children: [
                    const Icon(
                      Icons.auto_awesome_outlined,
                      size: 16,
                      color: AppColors.primary,
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        label,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          fontSize: 13,
                          color: AppColors.textPrimary,
                        ),
                      ),
                    ),
                    const Icon(
                      Icons.chevron_right_rounded,
                      size: 18,
                      color: AppColors.textMuted,
                    ),
                  ],
                ),
              ),
            ),
            if (variants.isNotEmpty) ...[
              const SizedBox(height: 14),
              const Text(
                '思考强度',
                style: TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w600,
                  color: AppColors.textSecondary,
                ),
              ),
              const SizedBox(height: 8),
              Wrap(
                spacing: 7,
                runSpacing: 7,
                children: [
                  ChoiceChip(
                    label: const Text('自动'),
                    selected: _variant == null,
                    onSelected: (_) => setState(() => _variant = null),
                  ),
                  for (final variant in variants)
                    ChoiceChip(
                      label: Text(thinkingVariantLabel(variant)),
                      selected: _variant == variant,
                      onSelected: (_) => setState(() => _variant = variant),
                    ),
                ],
              ),
            ],
            const SizedBox(height: 18),
            Row(
              children: [
                TextButton(
                  onPressed: () => setState(() {
                    _model = null;
                    _variant = null;
                  }),
                  child: const Text('使用默认'),
                ),
                const Spacer(),
                FilledButton.icon(
                  onPressed: () {
                    Navigator.of(context).pop();
                    widget.onApply(_model, _variant);
                  },
                  icon: const Icon(Icons.check_rounded, size: 17),
                  label: const Text('应用'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  void _showModelPicker() {
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: AppColors.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (pickerContext) => _ModelPickerSheet(
        availableModels: widget.availableModels,
        selectedModel: _model,
        onSelect: (model) {
          Navigator.of(pickerContext).pop();
          setState(() {
            _model = model;
            if (model == null || !model.variants.contains(_variant)) {
              _variant = null;
            }
          });
        },
      ),
    );
  }
}

class _ComposerActions extends StatelessWidget {
  final bool taskActive;
  final VoidCallback onSend;
  final VoidCallback onStop;
  const _ComposerActions({
    required this.taskActive,
    required this.onSend,
    required this.onStop,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.end,
      children: [
        if (taskActive) ...[
          Tooltip(
            message: '停止',
            child: InkWell(
              key: const ValueKey('stop'),
              onTap: onStop,
              borderRadius: AppRadius.smRadius,
              child: Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(
                  color: AppColors.statusError.withValues(alpha: 0.06),
                  borderRadius: AppRadius.smRadius,
                  border: Border.all(
                    color: AppColors.statusError.withValues(alpha: 0.55),
                  ),
                ),
                child: const Icon(
                  Icons.stop_rounded,
                  size: 19,
                  color: AppColors.statusError,
                ),
              ),
            ),
          ),
          const SizedBox(width: 6),
        ],
        Tooltip(
          message: taskActive ? '加入发送队列' : '发送',
          child: InkWell(
            key: const ValueKey('send'),
            onTap: onSend,
            borderRadius: AppRadius.smRadius,
            child: Container(
              width: 44,
              height: 44,
              decoration: BoxDecoration(
                color: AppColors.primary,
                borderRadius: AppRadius.smRadius,
              ),
              child: const Icon(
                Icons.send_rounded,
                size: 18,
                color: Colors.white,
              ),
            ),
          ),
        ),
      ],
    );
  }
}

@visibleForTesting
Widget buildChatQueueControlsTestSurface({
  required List<ChatQueueItem> items,
  required bool taskActive,
  required VoidCallback onSend,
  required VoidCallback onStop,
  ValueChanged<ChatQueueItem>? onInsert,
  ValueChanged<ChatQueueItem>? onQueueSend,
  bool queueLoading = false,
  String? queueError,
}) {
  return MaterialApp(
    theme: AppTheme.light,
    home: Scaffold(
      body: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            _QueueTray(
              agentId: 'test-agent',
              projectId: 'test-project',
              items: items,
              loading: queueLoading,
              error: queueError,
              taskActive: taskActive,
              availableModels: const [],
              onDelete: (_) {},
              onInsert: onInsert ?? (_) {},
              onSend: onQueueSend ?? (_) {},
              onReorder: (_) {},
              onUpdateModel: (_, _, _) {},
            ),
            const SizedBox(height: 12),
            Align(
              alignment: Alignment.centerRight,
              child: _ComposerActions(
                taskActive: taskActive,
                onSend: onSend,
                onStop: onStop,
              ),
            ),
          ],
        ),
      ),
    ),
  );
}

// 审批模式切换按钮
class _VariantSelector extends StatelessWidget {
  static const _autoKey = '_auto';
  final List<String> variants;
  final String? selected;
  final ValueChanged<String?> onSelect;

  const _VariantSelector({
    required this.variants,
    required this.selected,
    required this.onSelect,
  });

  IconData _icon(String? v) => switch (v) {
    'low' || 'none' || 'minimal' => Icons.speed_outlined,
    'medium' => Icons.psychology_outlined,
    'high' || 'xhigh' || 'max' => Icons.psychology,
    _ => Icons.auto_awesome_outlined,
  };

  @override
  Widget build(BuildContext context) {
    return PopupMenuButton<String>(
      onSelected: (v) => onSelect(v == _autoKey ? null : v),
      tooltip: '思考强度',
      offset: const Offset(0, -200),
      shape: RoundedRectangleBorder(borderRadius: AppRadius.mdRadius),
      itemBuilder: (_) => [
        PopupMenuItem(value: _autoKey, child: _item(null)),
        for (final v in variants) PopupMenuItem(value: v, child: _item(v)),
      ],
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        decoration: BoxDecoration(
          color: selected != null
              ? AppColors.primaryLight
              : AppColors.inputBackground,
          borderRadius: AppRadius.smRadius,
          border: Border.all(
            color: selected != null ? AppColors.primaryMuted : AppColors.border,
          ),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              _icon(selected),
              size: 14,
              color: selected != null ? AppColors.primary : AppColors.textMuted,
            ),
            const SizedBox(width: 4),
            Text(
              thinkingVariantLabel(selected),
              style: TextStyle(
                fontSize: 11,
                color: selected != null
                    ? AppColors.primary
                    : AppColors.textSecondary,
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _item(String? v) {
    final isSelected = v == selected;
    return Row(
      children: [
        Icon(
          _icon(v),
          size: 16,
          color: isSelected ? AppColors.primary : AppColors.textSecondary,
        ),
        const SizedBox(width: 8),
        Text(
          thinkingVariantLabel(v),
          style: TextStyle(
            fontSize: 13,
            color: isSelected ? AppColors.primary : AppColors.textPrimary,
            fontWeight: isSelected ? FontWeight.w600 : FontWeight.w400,
          ),
        ),
      ],
    );
  }
}

class _PermissionModeButton extends StatelessWidget {
  final String mode;
  final void Function(String mode) onChanged;

  const _PermissionModeButton({required this.mode, required this.onChanged});

  @override
  Widget build(BuildContext context) {
    final label = switch (mode) {
      'auto-approve' => '自动批准',
      'deny' => '全部拒绝',
      _ => '需要审批',
    };
    final color = switch (mode) {
      'auto-approve' => AppColors.statusSuccess,
      'deny' => AppColors.statusError,
      _ => AppColors.statusWarning,
    };
    return InkWell(
      onTap: () => _showPicker(context),
      borderRadius: AppRadius.smRadius,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        decoration: BoxDecoration(
          color: color.withValues(alpha: 0.1),
          borderRadius: AppRadius.smRadius,
          border: Border.all(color: color.withValues(alpha: 0.4)),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.security_outlined, size: 13, color: color),
            const SizedBox(width: 4),
            Text(
              label,
              style: TextStyle(
                fontSize: 12,
                color: color,
                fontWeight: FontWeight.w500,
              ),
            ),
            const SizedBox(width: 2),
            Icon(Icons.expand_more, size: 13, color: color),
          ],
        ),
      ),
    );
  }

  void _showPicker(BuildContext context) {
    final options = [
      (
        'ask',
        '需要审批',
        '执行敏感操作时弹窗让您确认',
        AppColors.statusWarning,
        Icons.help_outline,
      ),
      (
        'auto-approve',
        '自动批准',
        '自动通过所有权限请求（风险较高）',
        AppColors.statusSuccess,
        Icons.check_circle_outline,
      ),
      (
        'deny',
        '全部拒绝',
        '拒绝所有权限请求，任务会因缺权限失败',
        AppColors.statusError,
        Icons.block_outlined,
      ),
    ];
    showModalBottomSheet(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text(
                '审批模式',
                style: TextStyle(fontWeight: FontWeight.w600, fontSize: 15),
              ),
              const SizedBox(height: 4),
              const Text(
                '控制 AI 执行需要权限的操作（如文件写入、命令执行）时的行为',
                style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
              ),
              const SizedBox(height: 12),
              ...options.map((opt) {
                final (value, title, desc, color, icon) = opt;
                final selected = mode == value;
                return InkWell(
                  onTap: () {
                    Navigator.of(context).pop();
                    onChanged(value);
                  },
                  borderRadius: AppRadius.mdRadius,
                  child: Container(
                    margin: const EdgeInsets.only(bottom: 8),
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 10,
                    ),
                    decoration: BoxDecoration(
                      color: selected
                          ? color.withValues(alpha: 0.08)
                          : Colors.transparent,
                      borderRadius: AppRadius.mdRadius,
                      border: Border.all(
                        color: selected
                            ? color.withValues(alpha: 0.5)
                            : AppColors.border,
                      ),
                    ),
                    child: Row(
                      children: [
                        Icon(
                          icon,
                          size: 18,
                          color: selected ? color : AppColors.textSecondary,
                        ),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                title,
                                style: TextStyle(
                                  fontSize: 13,
                                  fontWeight: FontWeight.w500,
                                  color: selected
                                      ? color
                                      : AppColors.textPrimary,
                                ),
                              ),
                              Text(
                                desc,
                                style: const TextStyle(
                                  fontSize: 11,
                                  color: AppColors.textSecondary,
                                ),
                              ),
                            ],
                          ),
                        ),
                        if (selected) Icon(Icons.check, size: 16, color: color),
                      ],
                    ),
                  ),
                );
              }),
            ],
          ),
        ),
      ),
    );
  }
}

class _ProjectPromptButton extends StatelessWidget {
  final bool hasPrompt;
  final VoidCallback onTap;

  const _ProjectPromptButton({required this.hasPrompt, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final color = hasPrompt ? AppColors.primary : AppColors.textSecondary;
    return InkWell(
      onTap: onTap,
      borderRadius: AppRadius.smRadius,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        decoration: BoxDecoration(
          color: hasPrompt ? AppColors.primaryLight : AppColors.inputBackground,
          borderRadius: AppRadius.smRadius,
          border: Border.all(
            color: hasPrompt ? AppColors.primaryMuted : AppColors.border,
          ),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Stack(
              clipBehavior: Clip.none,
              children: [
                Icon(Icons.edit_note_outlined, size: 14, color: color),
                if (hasPrompt)
                  Positioned(
                    right: -1,
                    top: -1,
                    child: Container(
                      width: 6,
                      height: 6,
                      decoration: const BoxDecoration(
                        color: AppColors.primary,
                        shape: BoxShape.circle,
                      ),
                    ),
                  ),
              ],
            ),
            const SizedBox(width: 4),
            Text(
              '项目提示词',
              style: TextStyle(
                fontSize: 12,
                color: color,
                fontWeight: hasPrompt ? FontWeight.w500 : FontWeight.w400,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ProjectMemoryButton extends StatelessWidget {
  final VoidCallback onTap;
  const _ProjectMemoryButton({required this.onTap});

  @override
  Widget build(BuildContext context) => Tooltip(
    message: '项目记忆',
    child: InkWell(
      onTap: onTap,
      borderRadius: AppRadius.smRadius,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        decoration: BoxDecoration(
          color: AppColors.inputBackground,
          borderRadius: AppRadius.smRadius,
          border: Border.all(color: AppColors.border),
        ),
        child: const Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.memory_outlined,
              size: 14,
              color: AppColors.textSecondary,
            ),
            SizedBox(width: 4),
            Text(
              '项目记忆',
              style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
            ),
          ],
        ),
      ),
    ),
  );
}

class _SubagentSidebar extends StatelessWidget {
  final String taskId;
  final List<ModelInfo> availableModels;
  final VoidCallback onClose;

  const _SubagentSidebar({
    required this.taskId,
    required this.availableModels,
    required this.onClose,
  });

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 320,
      child: ColoredBox(
        color: AppColors.surfaceElevated,
        child: Column(
          children: [
            Container(
              height: 48,
              padding: const EdgeInsets.only(left: 14, right: 6),
              decoration: const BoxDecoration(
                border: Border(bottom: BorderSide(color: AppColors.border)),
              ),
              child: Row(
                children: [
                  const Icon(
                    Icons.account_tree_outlined,
                    size: 17,
                    color: AppColors.primary,
                  ),
                  const SizedBox(width: 8),
                  const Expanded(
                    child: Text(
                      '子代理任务',
                      style: TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w700,
                        color: AppColors.textPrimary,
                      ),
                    ),
                  ),
                  IconButton(
                    tooltip: '关闭子代理任务',
                    onPressed: onClose,
                    icon: const Icon(Icons.close_rounded, size: 18),
                    color: AppColors.textSecondary,
                  ),
                ],
              ),
            ),
            Expanded(
              child: SingleChildScrollView(
                padding: const EdgeInsets.fromLTRB(12, 12, 12, 20),
                child: SubagentTaskTree(
                  taskId: taskId,
                  availableModels: availableModels,
                  showEmptyState: true,
                  showHeader: false,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

// 历史会话侧边栏
class _SessionSidebar extends ConsumerWidget {
  final String agentId;
  final String projectId;
  final String? currentSessionId;
  final void Function(String sid) onSelect;
  final VoidCallback onNew;

  const _SessionSidebar({
    required this.agentId,
    required this.projectId,
    required this.currentSessionId,
    required this.onSelect,
    required this.onNew,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final sessionsAsync = ref.watch(sessionListProvider((agentId, projectId)));

    return SizedBox(
      width: 240,
      child: Container(
        color: AppColors.surfaceElevated,
        child: Column(
          children: [
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
              decoration: const BoxDecoration(
                border: Border(bottom: BorderSide(color: AppColors.border)),
              ),
              child: Row(
                children: [
                  const Text(
                    '历史会话',
                    style: TextStyle(
                      fontSize: 13,
                      fontWeight: FontWeight.w600,
                      color: AppColors.textPrimary,
                    ),
                  ),
                  const Spacer(),
                  GestureDetector(
                    onTap: onNew,
                    child: Container(
                      padding: const EdgeInsets.all(5),
                      decoration: BoxDecoration(
                        color: AppColors.primaryLight,
                        borderRadius: AppRadius.smRadius,
                      ),
                      child: const Icon(
                        Icons.add,
                        size: 14,
                        color: AppColors.primary,
                      ),
                    ),
                  ),
                ],
              ),
            ),
            Expanded(
              child: sessionsAsync.when(
                loading: () => const LoadingState(),
                error: (e, _) => Center(
                  child: Text(
                    '加载失败',
                    style: const TextStyle(
                      fontSize: 12,
                      color: AppColors.textMuted,
                    ),
                  ),
                ),
                data: (sessions) {
                  if (sessions.isEmpty) {
                    return const Center(
                      child: Text(
                        '暂无历史会话',
                        style: TextStyle(
                          fontSize: 12,
                          color: AppColors.textMuted,
                        ),
                      ),
                    );
                  }
                  return ListView.builder(
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    itemCount: sessions.length,
                    itemBuilder: (context, i) {
                      final session = sessions[i];
                      final isActive = session.sessionId == currentSessionId;
                      return _SessionListItem(
                        session: session,
                        isActive: isActive,
                        onTap: () => onSelect(session.sessionId),
                      );
                    },
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _SessionListItem extends StatelessWidget {
  final SessionModel session;
  final bool isActive;
  final VoidCallback onTap;

  const _SessionListItem({
    required this.session,
    required this.isActive,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: onTap,
      child: Container(
        margin: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
        decoration: BoxDecoration(
          color: isActive ? AppColors.primaryLight : Colors.transparent,
          borderRadius: AppRadius.smRadius,
          border: isActive ? Border.all(color: AppColors.primaryMuted) : null,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              session.summary?.isNotEmpty == true
                  ? session.summary!
                  : session.sessionId,
              style: TextStyle(
                fontSize: 12,
                fontWeight: isActive ? FontWeight.w600 : FontWeight.w400,
                color: isActive ? AppColors.primary : AppColors.textPrimary,
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
            if (session.updatedAt != null) ...[
              const SizedBox(height: 3),
              Text(
                _formatTime(session.updatedAt!),
                style: const TextStyle(
                  fontSize: 11,
                  color: AppColors.textMuted,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  String _formatTime(DateTime dt) {
    final diff = DateTime.now().difference(dt);
    if (diff.inMinutes < 60) return '${diff.inMinutes} 分钟前';
    if (diff.inHours < 24) return '${diff.inHours} 小时前';
    return '${dt.month}/${dt.day}';
  }
}

// 复制全文按钮
class _CopyButton extends StatefulWidget {
  final String content;
  const _CopyButton({required this.content});

  @override
  State<_CopyButton> createState() => _CopyButtonState();
}

class _CopyButtonState extends State<_CopyButton> {
  bool _copied = false;

  Future<void> _copy() async {
    await Clipboard.setData(ClipboardData(text: widget.content));
    setState(() => _copied = true);
    await Future.delayed(const Duration(seconds: 2));
    if (mounted) setState(() => _copied = false);
  }

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: _copy,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            _copied ? Icons.check : Icons.copy_outlined,
            size: 13,
            color: _copied ? AppColors.statusSuccess : AppColors.textMuted,
          ),
          const SizedBox(width: 3),
          Text(
            _copied ? '已复制' : '复制',
            style: TextStyle(
              fontSize: 11,
              color: _copied ? AppColors.statusSuccess : AppColors.textMuted,
            ),
          ),
        ],
      ),
    );
  }
}
