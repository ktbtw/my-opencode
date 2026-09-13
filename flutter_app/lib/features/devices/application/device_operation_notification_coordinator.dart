import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/notifications/app_notification_controller.dart';
import '../../../core/notifications/app_notification_model.dart';
import '../data/device_launcher_model.dart';
import '../data/device_semantic_agent_model.dart';
import '../presentation/device_provider.dart';

const _launcherOperationPrefix = 'launcher:update:';
const _semanticPreflightOperationPrefix = 'semantic-preflight:';
const _machineIdKey = 'machine_id';
const _agentIdKey = 'agent_id';
const _jobIdKey = 'job_id';
const _targetVersionKey = 'target_version';

final deviceOperationNotificationCoordinatorProvider = Provider((ref) {
  final repository = ref.watch(deviceRepositoryProvider);
  final coordinator = DeviceOperationNotificationCoordinator(
    notifications: ref.watch(appNotificationControllerProvider.notifier),
    notificationState: () => ref.read(appNotificationControllerProvider),
    loadLauncherState: repository.getDeviceLauncherState,
    loadPreflightJob: ({required machineId, required jobId}) => repository
        .getDeviceAgentSemanticPreflightJob(machineId: machineId, jobId: jobId),
  )..start();
  ref.onDispose(coordinator.dispose);
  return coordinator;
});

typedef LauncherStateLoader =
    Future<DeviceLauncherState> Function(String machineId);
typedef PreflightJobLoader =
    Future<RuntimePreflightJob> Function({
      required String machineId,
      required String jobId,
    });

class DeviceOperationNotificationCoordinator {
  final AppNotificationController notifications;
  final AppNotificationState Function() notificationState;
  final LauncherStateLoader loadLauncherState;
  final PreflightJobLoader loadPreflightJob;
  final Duration pollInterval;
  final Duration unavailableTimeout;
  final Duration preflightStaleTimeout;
  final DateTime Function() now;

  Timer? _timer;
  final Map<String, DateTime> _unavailableSince = {};
  bool _running = false;
  bool _disposed = false;

  DeviceOperationNotificationCoordinator({
    required this.notifications,
    required this.notificationState,
    required this.loadLauncherState,
    required this.loadPreflightJob,
    this.pollInterval = const Duration(seconds: 4),
    this.unavailableTimeout = const Duration(minutes: 5),
    this.preflightStaleTimeout = const Duration(minutes: 2),
    DateTime Function()? now,
  }) : now = now ?? (() => DateTime.now().toUtc());

  void start() {
    if (_timer != null || _disposed) return;
    _timer = Timer.periodic(pollInterval, (_) => unawaited(reconcileNow()));
    unawaited(reconcileNow());
  }

  Future<void> reconcileNow() async {
    if (_running || _disposed) return;
    final snapshot = notificationState();
    if (!snapshot.hydrated) return;
    _running = true;
    try {
      final records = snapshot.active
          .where(
            (record) =>
                !record.isTerminal &&
                (record.operationId.startsWith(_launcherOperationPrefix) ||
                    record.operationId.startsWith(
                      _semanticPreflightOperationPrefix,
                    )),
          )
          .toList(growable: false);
      for (final record in records) {
        if (_disposed) return;
        if (record.operationId.startsWith(_launcherOperationPrefix)) {
          await _reconcileLauncher(record);
        } else {
          await _reconcilePreflight(record);
        }
      }
    } finally {
      _running = false;
    }
  }

  Future<void> _reconcileLauncher(AppNotificationRecord record) async {
    final operation = parseLauncherUpgradeOperation(record);
    if (operation == null) {
      notifications.fail(
        record.operationId,
        title: 'Launcher 升级状态异常',
        error: '升级通知缺少设备或目标版本信息',
      );
      return;
    }
    try {
      final state = await loadLauncherState(operation.machineId);
      if (_disposed) return;
      _unavailableSince.remove(record.operationId);
      reconcileLauncherUpgradeNotification(
        notifications: notifications,
        operationId: record.operationId,
        targetVersion: operation.targetVersion,
        state: state,
        now: now(),
      );
    } catch (error) {
      _handleUnavailable(record, error, title: 'Launcher 升级状态同步失败');
    }
  }

  Future<void> _reconcilePreflight(AppNotificationRecord record) async {
    final machineId = record.metadata[_machineIdKey]?.trim() ?? '';
    final jobId = record.metadata[_jobIdKey]?.trim() ?? '';
    if (machineId.isEmpty || jobId.isEmpty) {
      if (now().difference(record.createdAt) > const Duration(seconds: 90)) {
        notifications.fail(
          record.operationId,
          title: 'Agent 助手切换已结束',
          error: '预检任务缺少恢复信息，请重新发起切换',
        );
      }
      return;
    }
    try {
      final job = await loadPreflightJob(machineId: machineId, jobId: jobId);
      if (_disposed) return;
      _unavailableSince.remove(record.operationId);
      reconcileSemanticPreflightNotification(
        notifications: notifications,
        operationId: record.operationId,
        job: job,
        staleTimeout: preflightStaleTimeout,
        now: now(),
      );
    } catch (error) {
      _handleUnavailable(record, error, title: 'Agent 预检状态同步失败');
    }
  }

  void _handleUnavailable(
    AppNotificationRecord record,
    Object error, {
    required String title,
  }) {
    final timestamp = now();
    final unavailableSince = _unavailableSince.putIfAbsent(
      record.operationId,
      () => timestamp,
    );
    if (timestamp.difference(unavailableSince) >= unavailableTimeout) {
      notifications.fail(record.operationId, title: title, error: error);
      _unavailableSince.remove(record.operationId);
      return;
    }
    notifications.waitForSync(
      record.operationId,
      message: '设备暂时离线，恢复连接后将继续确认结果',
    );
  }

  void dispose() {
    _disposed = true;
    _timer?.cancel();
    _timer = null;
    _unavailableSince.clear();
  }
}

class LauncherUpgradeOperation {
  final String machineId;
  final String targetVersion;

  const LauncherUpgradeOperation({
    required this.machineId,
    required this.targetVersion,
  });
}

String launcherUpgradeOperationId(String machineId, String targetVersion) {
  return '$_launcherOperationPrefix$machineId:$targetVersion';
}

LauncherUpgradeOperation? parseLauncherUpgradeOperation(
  AppNotificationRecord record,
) {
  var machineId = record.metadata[_machineIdKey]?.trim() ?? '';
  var targetVersion = record.metadata[_targetVersionKey]?.trim() ?? '';
  if (machineId.isEmpty || targetVersion.isEmpty) {
    final raw = record.operationId;
    if (!raw.startsWith(_launcherOperationPrefix)) return null;
    final value = raw.substring(_launcherOperationPrefix.length);
    final separator = value.lastIndexOf(':');
    if (separator <= 0 || separator == value.length - 1) return null;
    machineId = value.substring(0, separator).trim();
    targetVersion = value.substring(separator + 1).trim();
  }
  if (machineId.isEmpty || targetVersion.isEmpty) return null;
  return LauncherUpgradeOperation(
    machineId: machineId,
    targetVersion: targetVersion,
  );
}

void startLauncherUpgradeNotification({
  required AppNotificationController notifications,
  required String machineId,
  required String targetVersion,
  String sourceLabel = '',
}) {
  notifications.start(
    operationId: launcherUpgradeOperationId(machineId, targetVersion),
    title: '正在升级 Launcher',
    message: '正在检查 $targetVersion 的升级环境',
    progressMode: AppNotificationProgressMode.determinate,
    displayStyle: AppNotificationDisplayStyle.stages,
    progress: 0.03,
    kind: AppNotificationKind.installation,
    scope: AppNotificationScope.synced,
    sourceLabel: sourceLabel.isEmpty ? machineId : sourceLabel,
    metadata: {_machineIdKey: machineId, _targetVersionKey: targetVersion},
    stages: const [
      AppNotificationStage(label: '检查版本', active: true),
      AppNotificationStage(label: '下载'),
      AppNotificationStage(label: '切换版本'),
      AppNotificationStage(label: '验证'),
    ],
    actions: [
      AppNotificationAction(
        label: '查看设备',
        type: AppNotificationActionType.openRoute,
        payload: {'route': '/devices/$machineId'},
        primary: true,
      ),
    ],
  );
}

void reconcileLauncherUpgradeNotification({
  required AppNotificationController notifications,
  required String operationId,
  required String targetVersion,
  required DeviceLauncherState state,
  DateTime? now,
}) {
  final record = notifications.findByOperationId(operationId);
  if (record == null || record.isTerminal) return;
  final failed =
      state.upgradeStage == 'failed' || state.status.toLowerCase() == 'failed';
  if (failed) {
    notifications.fail(
      operationId,
      title: 'Launcher 升级失败',
      error: state.lastError.isEmpty ? '设备报告升级失败' : state.lastError,
      critical: true,
    );
    return;
  }
  final completedStage =
      state.upgradeStage == 'completed' ||
      state.upgradeStage == 'already_latest';
  final targetInstalled =
      targetVersion.isNotEmpty && state.currentVersion.trim() == targetVersion;
  if (!state.isUpgradeRunning && (completedStage || targetInstalled)) {
    notifications.succeed(
      operationId,
      title: 'Launcher 升级完成',
      message: '设备已切换到 $targetVersion 并完成验证',
    );
    return;
  }
  if (!state.isUpgradeRunning) {
    final elapsed = (now ?? DateTime.now().toUtc()).difference(
      record.createdAt,
    );
    if (elapsed >= const Duration(minutes: 3)) {
      notifications.fail(
        operationId,
        title: 'Launcher 升级未完成',
        error: state.lastError.isEmpty
            ? '设备已结束升级流程，但当前版本仍为 ${state.currentVersion.isEmpty ? '未知' : state.currentVersion}'
            : state.lastError,
      );
      return;
    }
    notifications.waitForSync(
      operationId,
      message: state.upgradeStage.isEmpty && state.upgradeUpdatedAt == null
          ? '升级命令已发送，等待设备重连验证版本'
          : '等待设备确认升级结果',
    );
    return;
  }

  const stageOrder = [
    'checking_version',
    'downloading',
    'switching_version',
    'starting_agents',
  ];
  var activeIndex = stageOrder.indexOf(state.upgradeStage);
  if (activeIndex < 0) activeIndex = 0;
  notifications.update(
    operationId: operationId,
    status: AppNotificationStatus.running,
    message: state.upgradeMessage.isEmpty
        ? launcherUpgradeStageLabel(state.upgradeStage)
        : state.upgradeMessage,
    progressMode: AppNotificationProgressMode.determinate,
    progress: state.upgradeProgress.clamp(0, 100) / 100,
    stages: [
      for (var index = 0; index < stageOrder.length; index++)
        AppNotificationStage(
          label: launcherUpgradeStageLabel(stageOrder[index]),
          completed: index < activeIndex,
          active: index == activeIndex,
        ),
    ],
  );
}

void attachSemanticPreflightMetadata({
  required AppNotificationController notifications,
  required String operationId,
  required String machineId,
  required String agentId,
  required String jobId,
}) {
  notifications.update(
    operationId: operationId,
    metadata: {
      _machineIdKey: machineId,
      _agentIdKey: agentId,
      _jobIdKey: jobId,
    },
  );
}

void reconcileSemanticPreflightNotification({
  required AppNotificationController notifications,
  required String operationId,
  required RuntimePreflightJob job,
  List<RuntimePreflightEvent> events = const [],
  Duration staleTimeout = const Duration(minutes: 2),
  DateTime? now,
}) {
  final record = notifications.findByOperationId(operationId);
  if (record == null) return;
  final expectedJobId = record.metadata[_jobIdKey]?.trim() ?? '';
  if (expectedJobId.isNotEmpty && expectedJobId != job.id.trim()) return;

  if (job.completed) {
    notifications.succeed(
      operationId,
      title: 'Agent 助手切换完成',
      message: '运行时、MCP 和 Skill 已验证生效',
    );
    return;
  }
  if (job.status == 'cancelled') {
    notifications.cancel(operationId, message: '预检任务已取消');
    return;
  }
  if (job.failed) {
    notifications.fail(
      operationId,
      title: 'Agent 助手切换失败',
      error: job.error.isEmpty ? '运行时或 MCP 预检失败' : job.error,
      critical: true,
    );
    return;
  }
  final timestamp = now ?? DateTime.now().toUtc();
  final lastUpdate = job.updatedAt ?? job.createdAt;
  if (!job.waitingForUserAction &&
      lastUpdate != null &&
      !timestamp.isBefore(lastUpdate) &&
      timestamp.difference(lastUpdate) >= staleTimeout) {
    notifications.fail(
      operationId,
      title: 'Agent 助手切换已中断',
      error: 'Launcher 已超过 ${staleTimeout.inMinutes} 分钟未回传预检状态',
    );
    return;
  }

  const stageOrder = ['runtime', 'mcp', 'skill', 'apply'];
  final effectiveStep = effectiveRuntimePreflightStep(
    job,
    supplementalEvents: events,
  );
  var activeIndex = stageOrder.indexOf(effectiveStep);
  if (activeIndex < 0) activeIndex = 0;
  notifications.update(
    operationId: operationId,
    status: AppNotificationStatus.running,
    message: job.waitingForUserAction
        ? (job.userAction?.message.isNotEmpty == true
              ? job.userAction!.message
              : '等待电脑端确认')
        : semanticPreflightProgressMessage(job, events: events),
    progressMode: AppNotificationProgressMode.determinate,
    progress:
        runtimePreflightDisplayProgress(job, supplementalEvents: events) / 100,
    stages: [
      for (var index = 0; index < stageOrder.length; index++)
        AppNotificationStage(
          label: semanticPreflightStepLabel(stageOrder[index]),
          completed: index < activeIndex,
          active: index == activeIndex,
        ),
    ],
    attention: job.waitingForUserAction
        ? AppNotificationAttention.userAction
        : AppNotificationAttention.none,
  );
}

String semanticPreflightProgressMessage(
  RuntimePreflightJob job, {
  List<RuntimePreflightEvent> events = const [],
}) {
  final effectiveStep = effectiveRuntimePreflightStep(
    job,
    supplementalEvents: events,
  );
  RuntimePreflightEvent? latest;
  final availableEvents = events.isEmpty ? job.latestEvents : events;
  for (final event in availableEvents.reversed) {
    if (event.message.trim().isEmpty) continue;
    if (normalizeRuntimePreflightStep(event.itemType) == effectiveStep) {
      latest = event;
      break;
    }
    latest ??= event;
  }
  if (latest == null) {
    return '${semanticPreflightStatusLabel(job.status)} · ${semanticPreflightStepLabel(effectiveStep)}';
  }
  final message = latest.message.trim().replaceFirst('正在下载或续传', '正在下载');
  if (latest.totalBytes > 0) {
    final progress = (latest.receivedBytes * 100 / latest.totalBytes)
        .round()
        .clamp(0, 100);
    return '$message · ${formatNotificationBytes(latest.receivedBytes)}/${formatNotificationBytes(latest.totalBytes)} · $progress%';
  }
  if (latest.receivedBytes > 0) {
    return '$message · 已下载 ${formatNotificationBytes(latest.receivedBytes)}';
  }
  return message;
}

String launcherUpgradeStageLabel(String stage) {
  return switch (stage) {
    'checking_version' => '检查版本',
    'downloading' => '下载',
    'stopping_agents' || 'switching_version' => '切换版本',
    'starting_agents' => '验证',
    'completed' || 'already_latest' => '完成',
    'failed' => '失败',
    _ => '准备中',
  };
}

String semanticPreflightStatusLabel(String status) {
  return switch (status) {
    'pending' => '等待执行',
    'running' => '检测中',
    'waiting_user_action' => '等待确认',
    'completed' => '已完成',
    'failed' => '失败',
    'cancelled' => '已取消',
    _ => status.isEmpty ? '准备中' : status,
  };
}

String semanticPreflightStepLabel(String step) {
  return switch (step) {
    'runtime' => '运行时',
    'mcp' || 'mcp_ready' => 'MCP',
    'skill' => 'Skill',
    'apply' => '应用',
    'completed' => '完成',
    _ => step.isEmpty ? '准备中' : step,
  };
}

String formatNotificationBytes(int value) {
  if (value <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  var size = value.toDouble();
  var index = 0;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index++;
  }
  final digits = size >= 100 || index == 0 ? 0 : 1;
  return '${size.toStringAsFixed(digits)} ${units[index]}';
}
