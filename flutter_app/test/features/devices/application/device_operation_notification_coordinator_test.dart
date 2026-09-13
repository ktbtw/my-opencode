import 'package:chat_codex_app/core/notifications/app_notification_controller.dart';
import 'package:chat_codex_app/core/notifications/app_notification_model.dart';
import 'package:chat_codex_app/core/notifications/app_notification_repository.dart';
import 'package:chat_codex_app/features/devices/application/device_operation_notification_coordinator.dart';
import 'package:chat_codex_app/features/devices/data/device_launcher_model.dart';
import 'package:chat_codex_app/features/devices/data/device_semantic_agent_model.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'old launcher state completes an upgrade when target is installed',
    () async {
      final now = DateTime.utc(2026, 8, 22, 10);
      final controller = await _controller(now);
      startLauncherUpgradeNotification(
        notifications: controller,
        machineId: 'machine-1',
        targetVersion: '1.2.3',
      );

      reconcileLauncherUpgradeNotification(
        notifications: controller,
        operationId: launcherUpgradeOperationId('machine-1', '1.2.3'),
        targetVersion: '1.2.3',
        state: DeviceLauncherState.fromJson({
          'status': 'online',
          'current_version': '1.2.3',
        }),
        now: now,
      );

      expect(
        controller.state.active.single.status,
        AppNotificationStatus.succeeded,
      );
      controller.dispose();
    },
  );

  test('launcher failure closes the active notification', () async {
    final now = DateTime.utc(2026, 8, 22, 10);
    final controller = await _controller(now);
    startLauncherUpgradeNotification(
      notifications: controller,
      machineId: 'machine-1',
      targetVersion: '1.2.3',
    );

    reconcileLauncherUpgradeNotification(
      notifications: controller,
      operationId: launcherUpgradeOperationId('machine-1', '1.2.3'),
      targetVersion: '1.2.3',
      state: DeviceLauncherState.fromJson({
        'status': 'failed',
        'upgrade_stage': 'failed',
        'last_error': 'launcher exited',
      }),
      now: now,
    );

    expect(controller.state.active.single.status, AppNotificationStatus.failed);
    expect(controller.state.active.single.message, contains('launcher exited'));
    controller.dispose();
  });

  test(
    'inconclusive old launcher state has a bounded waiting period',
    () async {
      final startedAt = DateTime.utc(2026, 8, 22, 10);
      final controller = await _controller(startedAt);
      startLauncherUpgradeNotification(
        notifications: controller,
        machineId: 'machine-1',
        targetVersion: '1.2.3',
      );
      final state = DeviceLauncherState.fromJson({
        'status': 'online',
        'current_version': '1.2.2',
      });

      reconcileLauncherUpgradeNotification(
        notifications: controller,
        operationId: launcherUpgradeOperationId('machine-1', '1.2.3'),
        targetVersion: '1.2.3',
        state: state,
        now: startedAt.add(const Duration(minutes: 1)),
      );
      expect(
        controller.state.active.single.status,
        AppNotificationStatus.waitingSync,
      );

      reconcileLauncherUpgradeNotification(
        notifications: controller,
        operationId: launcherUpgradeOperationId('machine-1', '1.2.3'),
        targetVersion: '1.2.3',
        state: state,
        now: startedAt.add(const Duration(minutes: 4)),
      );
      expect(
        controller.state.active.single.status,
        AppNotificationStatus.failed,
      );
      controller.dispose();
    },
  );

  test('stale preflight job is reported as interrupted', () async {
    final startedAt = DateTime.utc(2026, 8, 22, 10);
    final controller = await _controller(startedAt);
    controller.start(
      operationId: 'semantic-preflight:machine-1:agent-1',
      title: '正在切换 Agent 助手',
      message: '检测中',
    );

    reconcileSemanticPreflightNotification(
      notifications: controller,
      operationId: 'semantic-preflight:machine-1:agent-1',
      job: RuntimePreflightJob(
        id: 'job-1',
        status: 'running',
        updatedAt: startedAt,
      ),
      now: startedAt.add(const Duration(minutes: 3)),
    );

    expect(controller.state.active.single.status, AppNotificationStatus.failed);
    expect(controller.state.active.single.message, contains('2 分钟'));
    controller.dispose();
  });

  test('preflight notification progress follows download events', () async {
    final now = DateTime.utc(2026, 8, 22, 10);
    final controller = await _controller(now);
    controller.start(
      operationId: 'semantic-preflight:machine-1:agent-1:job-1',
      title: '正在切换 Agent 助手',
      message: '检测中',
      progressMode: AppNotificationProgressMode.determinate,
      progress: 0,
    );

    reconcileSemanticPreflightNotification(
      notifications: controller,
      operationId: 'semantic-preflight:machine-1:agent-1:job-1',
      job: const RuntimePreflightJob(
        id: 'job-1',
        status: 'running',
        currentStep: 'runtime',
        progressPercent: 8,
        items: [
          RuntimePreflightItem(
            itemType: 'runtime',
            status: 'running',
            progressPercent: 20,
          ),
        ],
      ),
      events: const [
        RuntimePreflightEvent(
          itemType: 'runtime',
          message: '正在下载 Node.js',
          receivedBytes: 50,
          totalBytes: 100,
          progressPercent: 50,
        ),
      ],
      now: now,
    );

    expect(controller.state.active.single.progress, 0.2);
    expect(controller.state.active.single.message, contains('正在下载 Node.js'));
    controller.dispose();
  });

  test(
    'coordinator restores a completed preflight after page disposal',
    () async {
      final now = DateTime.utc(2026, 8, 22, 10);
      final controller = await _controller(now);
      controller.start(
        operationId: 'semantic-preflight:machine-1:agent-1',
        title: '正在切换 Agent 助手',
        message: '等待同步',
        metadata: const {
          'machine_id': 'machine-1',
          'agent_id': 'agent-1',
          'job_id': 'job-1',
        },
      );
      final coordinator = DeviceOperationNotificationCoordinator(
        notifications: controller,
        notificationState: () => controller.state,
        loadLauncherState: (_) async => DeviceLauncherState.fromJson(const {}),
        loadPreflightJob: ({required machineId, required jobId}) async =>
            const RuntimePreflightJob(id: 'job-1', status: 'completed'),
        now: () => now,
      );

      await coordinator.reconcileNow();

      expect(
        controller.state.active.single.status,
        AppNotificationStatus.succeeded,
      );
      coordinator.dispose();
      controller.dispose();
    },
  );

  test(
    'ignores a response from an older preflight job on the same agent',
    () async {
      final now = DateTime.utc(2026, 8, 22, 10);
      final controller = await _controller(now);
      controller.start(
        operationId: 'semantic-preflight:machine-1:agent-1',
        title: '正在切换 Agent 助手',
        message: '新任务',
        metadata: const {
          'machine_id': 'machine-1',
          'agent_id': 'agent-1',
          'job_id': 'job-new',
        },
      );

      reconcileSemanticPreflightNotification(
        notifications: controller,
        operationId: 'semantic-preflight:machine-1:agent-1',
        job: const RuntimePreflightJob(
          id: 'job-old',
          status: 'running',
          currentStep: 'runtime',
        ),
        now: now,
      );

      expect(controller.state.active.single.message, '新任务');
      expect(
        controller.state.active.single.status,
        AppNotificationStatus.running,
      );
      controller.dispose();
    },
  );

  test('legacy preflight notification without job id is closed', () async {
    final startedAt = DateTime.utc(2026, 8, 22, 10);
    final controller = await _controller(startedAt);
    controller.start(
      operationId: 'semantic-preflight:machine-1:agent-1',
      title: '正在切换 Agent 助手',
      message: '等待同步',
    );
    final coordinator = DeviceOperationNotificationCoordinator(
      notifications: controller,
      notificationState: () => controller.state,
      loadLauncherState: (_) async => DeviceLauncherState.fromJson(const {}),
      loadPreflightJob: ({required machineId, required jobId}) async =>
          throw StateError('not called'),
      now: () => startedAt.add(const Duration(minutes: 2)),
    );

    await coordinator.reconcileNow();

    expect(controller.state.active.single.status, AppNotificationStatus.failed);
    expect(controller.state.active.single.message, contains('恢复信息'));
    coordinator.dispose();
    controller.dispose();
  });

  test(
    'temporary device outage only fails after a continuous timeout',
    () async {
      var clock = DateTime.utc(2026, 8, 22, 10);
      final controller = await _controller(clock);
      startLauncherUpgradeNotification(
        notifications: controller,
        machineId: 'machine-1',
        targetVersion: '1.2.3',
      );
      final coordinator = DeviceOperationNotificationCoordinator(
        notifications: controller,
        notificationState: () => controller.state,
        loadLauncherState: (_) async => throw StateError('offline'),
        loadPreflightJob: ({required machineId, required jobId}) async =>
            throw StateError('not called'),
        unavailableTimeout: const Duration(minutes: 1),
        now: () => clock,
      );

      await coordinator.reconcileNow();
      expect(
        controller.state.active.single.status,
        AppNotificationStatus.waitingSync,
      );

      clock = clock.add(const Duration(minutes: 2));
      await coordinator.reconcileNow();
      expect(
        controller.state.active.single.status,
        AppNotificationStatus.failed,
      );
      coordinator.dispose();
      controller.dispose();
    },
  );
}

Future<AppNotificationController> _controller(DateTime now) async {
  final controller = AppNotificationController(
    repository: _MemoryRepository(),
    now: () => now,
    collapseDelay: const Duration(hours: 1),
    archiveDelay: const Duration(hours: 1),
  );
  await controller.hydrate();
  return controller;
}

class _MemoryRepository implements AppNotificationRepository {
  AppNotificationSnapshot snapshot = const AppNotificationSnapshot();

  @override
  Future<AppNotificationSnapshot> load() async => snapshot;

  @override
  Future<void> save(AppNotificationSnapshot snapshot) async {
    this.snapshot = snapshot;
  }
}
