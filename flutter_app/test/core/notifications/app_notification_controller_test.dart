import 'dart:async';

import 'package:chat_codex_app/core/notifications/app_notification_controller.dart';
import 'package:chat_codex_app/core/notifications/app_notification_model.dart';
import 'package:chat_codex_app/core/notifications/app_notification_repository.dart';
import 'package:chat_codex_app/core/notifications/app_notification_sync.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('stable operation ID updates one active notification', () async {
    final repository = _MemoryRepository();
    final controller = _controller(repository);
    await controller.hydrate();

    controller.start(
      operationId: 'agent:create:42',
      title: '正在创建 Agent',
      message: '准备中',
      progressMode: AppNotificationProgressMode.determinate,
      progress: 0.1,
    );
    controller.update(
      operationId: 'agent:create:42',
      message: '正在安装资源',
      progress: 0.6,
    );

    expect(controller.state.active, hasLength(1));
    expect(controller.state.active.single.progress, 0.6);
    expect(controller.state.active.single.message, '正在安装资源');
    controller.dispose();
  });

  test('progress updates preserve a manual collapse', () async {
    final repository = _MemoryRepository();
    final controller = _controller(repository);
    await controller.hydrate();

    controller.start(
      operationId: 'artifact:download:1',
      title: '正在下载 delivery.zip',
      message: '准备中',
      progressMode: AppNotificationProgressMode.determinate,
      progress: 0.1,
    );
    controller.collapse();
    controller.update(
      operationId: 'artifact:download:1',
      message: '正在接收 delivery.zip',
      progress: 0.6,
    );
    controller.succeed('artifact:download:1', message: 'delivery.zip 已保存');

    expect(controller.state.collapsed, isTrue);
    expect(controller.state.active.single.progress, 1);
    expect(
      controller.state.active.single.status,
      AppNotificationStatus.succeeded,
    );
    controller.dispose();
  });

  test('a new operation expands a collapsed notification island', () async {
    final repository = _MemoryRepository();
    final controller = _controller(repository);
    await controller.hydrate();

    controller.start(
      operationId: 'artifact:download:1',
      title: '正在下载 delivery.zip',
      message: '准备中',
    );
    controller.collapse();
    controller.start(
      operationId: 'agent:start:2',
      title: '正在启动 Agent',
      message: '检查环境',
    );

    expect(controller.state.collapsed, isFalse);
    expect(controller.state.active, hasLength(2));
    controller.dispose();
  });

  test('terminal notification cannot regress and moves to recent', () async {
    final repository = _MemoryRepository();
    final controller = _controller(repository);
    await controller.hydrate();

    controller.start(
      operationId: 'file:download:1',
      title: '正在下载',
      message: '下载中',
    );
    controller.succeed('file:download:1', message: '下载完成');
    controller.update(
      operationId: 'file:download:1',
      status: AppNotificationStatus.running,
      message: '过期状态',
    );

    expect(
      controller.state.active.single.status,
      AppNotificationStatus.succeeded,
    );
    await Future<void>.delayed(const Duration(milliseconds: 35));
    expect(controller.state.active, isEmpty);
    expect(
      controller.state.recent.single.status,
      AppNotificationStatus.succeeded,
    );
    controller.dispose();
  });

  test('archiving a completed operation keeps it in recent history', () async {
    final repository = _MemoryRepository();
    final controller = _controller(repository);
    await controller.hydrate();

    controller.start(
      operationId: 'chat-task:completed',
      title: 'Agent 任务已完成',
      message: '回复已生成',
    );
    controller.succeed('chat-task:completed', message: '回复已生成');

    expect(controller.archiveCompletedOperation('chat-task:completed'), isTrue);
    expect(controller.state.active, isEmpty);
    expect(controller.state.recent.single.operationId, 'chat-task:completed');
    expect(controller.state.collapsed, isFalse);
    expect(
      controller.archiveCompletedOperation('chat-task:completed'),
      isFalse,
    );
    controller.dispose();
  });

  test('running operations cannot be archived as completed', () async {
    final repository = _MemoryRepository();
    final controller = _controller(repository);
    await controller.hydrate();

    controller.start(
      operationId: 'chat-task:running',
      title: 'Agent 正在处理',
      message: '生成回复中',
    );

    expect(controller.archiveCompletedOperation('chat-task:running'), isFalse);
    expect(controller.state.active, hasLength(1));
    expect(controller.state.recent, isEmpty);
    controller.dispose();
  });

  test(
    'repeated terminal sync echoes do not postpone automatic archive',
    () async {
      final repository = _MemoryRepository();
      final controller = AppNotificationController(
        repository: repository,
        collapseDelay: const Duration(seconds: 30),
        archiveDelay: const Duration(milliseconds: 30),
      );
      await controller.hydrate();

      controller.start(
        operationId: 'synced:completed',
        title: '任务完成',
        message: '已完成',
        scope: AppNotificationScope.synced,
      );
      controller.succeed('synced:completed', message: '已完成');
      final completed = controller.state.active.single;

      for (var index = 0; index < 4; index++) {
        controller.applyRemoteChange(
          AppNotificationRemoteChange(
            version: index + 1,
            operationId: completed.operationId,
            record: completed.copyWith(
              id: 'remote-record-$index',
              updatedAt: completed.updatedAt.add(Duration(seconds: index + 1)),
            ),
          ),
        );
        await Future<void>.delayed(const Duration(milliseconds: 4));
      }

      await Future<void>.delayed(const Duration(milliseconds: 12));
      expect(controller.state.active, isEmpty);
      expect(controller.state.recent.single.operationId, 'synced:completed');
      controller.dispose();
    },
  );

  test('closing notification center restarts automatic collapse', () async {
    final repository = _MemoryRepository();
    final controller = AppNotificationController(
      repository: repository,
      collapseDelay: const Duration(milliseconds: 25),
      archiveDelay: const Duration(seconds: 1),
    );
    await controller.hydrate();

    controller.start(
      operationId: 'center-collapse',
      title: '正在处理',
      message: '处理中',
    );
    controller.openCenter();
    controller.closeCenter();

    await Future<void>.delayed(const Duration(milliseconds: 40));
    expect(controller.state.collapsed, isTrue);
    controller.dispose();
  });

  test(
    'starting the same operation restarts a terminal notification',
    () async {
      final repository = _MemoryRepository();
      final controller = _controller(repository);
      await controller.hydrate();

      controller.start(
        operationId: 'semantic-preflight:machine:agent',
        title: '正在切换 Agent 助手',
        message: '首次验证',
      );
      controller.fail(
        'semantic-preflight:machine:agent',
        error: '缺少 Verify Token',
      );
      final failedId = controller.state.active.single.id;
      controller.collapse();
      controller.start(
        operationId: 'semantic-preflight:machine:agent',
        title: '正在切换 Agent 助手',
        message: '正在重新验证',
      );

      expect(controller.state.active, hasLength(1));
      expect(
        controller.state.active.single.status,
        AppNotificationStatus.running,
      );
      expect(controller.state.active.single.message, '正在重新验证');
      expect(controller.state.active.single.id, isNot(failedId));
      expect(controller.state.collapsed, isFalse);
      controller.dispose();
    },
  );

  test(
    'a late remote running record cannot restart the same terminal run',
    () async {
      final repository = _MemoryRepository();
      final controller = _controller(repository);
      await controller.hydrate();

      controller.start(
        operationId: 'agent:switch:1',
        title: '正在切换 Agent',
        message: '运行中',
      );
      controller.succeed('agent:switch:1', message: '切换完成');
      final completed = controller.state.active.single;
      controller.applyRemoteChange(
        AppNotificationRemoteChange(
          version: 2,
          operationId: completed.operationId,
          record: completed.copyWith(
            status: AppNotificationStatus.running,
            message: '迟到的运行状态',
            updatedAt: completed.updatedAt.add(const Duration(minutes: 1)),
            completedAt: null,
          ),
        ),
      );

      expect(
        controller.state.active.single.status,
        AppNotificationStatus.succeeded,
      );
      expect(controller.state.active.single.message, '切换完成');
      controller.dispose();
    },
  );

  test(
    'a late remote record from an older run cannot reopen a collapsed notice',
    () async {
      final repository = _MemoryRepository();
      final controller = _controller(repository);
      await controller.hydrate();

      controller.start(
        operationId: 'agent:switch:older-run',
        title: '正在切换 Agent',
        message: '运行中',
      );
      final started = controller.state.active.single;
      controller.succeed('agent:switch:older-run', message: '切换完成');
      controller.collapse();

      controller.applyRemoteChange(
        AppNotificationRemoteChange(
          version: 2,
          operationId: started.operationId,
          record: started.copyWith(
            id: 'older-run-record',
            status: AppNotificationStatus.running,
            message: '旧任务仍在运行',
            updatedAt: started.updatedAt.subtract(const Duration(seconds: 1)),
            completedAt: null,
          ),
        ),
      );

      expect(controller.state.collapsed, isTrue);
      expect(
        controller.state.active.single.status,
        AppNotificationStatus.succeeded,
      );
      controller.dispose();
    },
  );

  test(
    'hydrate recovers running work and archives terminal snapshots',
    () async {
      final now = DateTime.utc(2026, 8, 16);
      final repository = _MemoryRepository(
        AppNotificationSnapshot(
          active: [
            _record('running', AppNotificationStatus.running, now),
            _record('done', AppNotificationStatus.succeeded, now),
          ],
        ),
      );
      final controller = _controller(repository);

      await controller.hydrate();

      expect(controller.state.hydrated, isTrue);
      expect(controller.state.active.single.operationId, 'running');
      expect(controller.state.recent.single.operationId, 'done');
      expect(controller.state.collapsed, isTrue);
      controller.dispose();
    },
  );

  test('recent history is capped at fifty records', () async {
    final now = DateTime.utc(2026, 8, 16);
    final repository = _MemoryRepository(
      AppNotificationSnapshot(
        recent: List.generate(
          67,
          (index) => _record(
            'operation-$index',
            AppNotificationStatus.succeeded,
            now.add(Duration(minutes: index)),
          ),
        ),
      ),
    );
    final controller = _controller(repository);

    await controller.hydrate();

    expect(controller.state.recent, hasLength(50));
    expect(controller.state.recent.first.operationId, 'operation-66');
    controller.dispose();
  });

  test('dismissOperation removes a transient operation immediately', () async {
    final repository = _MemoryRepository();
    final controller = _controller(repository);
    await controller.hydrate();

    controller.start(
      operationId: 'agents:reorder:machine-a',
      title: '正在同步 Agent 顺序',
      message: '设备 A',
      scope: AppNotificationScope.synced,
    );
    controller.dismissOperation('agents:reorder:machine-a');
    await controller.flush();

    expect(controller.state.active, isEmpty);
    expect(controller.state.recent, isEmpty);
    expect(repository.saved.active, isEmpty);
    expect(repository.saved.recent, isEmpty);
    controller.dispose();
  });

  test(
    'hiding an active operation keeps it recoverable without remote dismiss',
    () async {
      final repository = _MemoryRepository();
      final controller = _controller(repository);
      await controller.hydrate();

      controller.start(
        operationId: 'semantic-preflight:machine-1:agent-1:job-1',
        title: '正在切换 Agent 助手',
        message: '等待 launcher',
        scope: AppNotificationScope.synced,
      );
      controller.hideOperation('semantic-preflight:machine-1:agent-1:job-1');
      await controller.flush();

      expect(controller.state.active, isEmpty);
      expect(
        repository.saved.hiddenOperationIds,
        contains('semantic-preflight:machine-1:agent-1:job-1'),
      );

      final now = DateTime.utc(2026, 8, 22, 10);
      controller.applyRemoteChange(
        AppNotificationRemoteChange(
          version: 1,
          operationId: 'semantic-preflight:machine-1:agent-1:job-1',
          record: AppNotificationRecord(
            id: 'remote-running',
            operationId: 'semantic-preflight:machine-1:agent-1:job-1',
            title: '正在切换 Agent 助手',
            message: '运行中',
            status: AppNotificationStatus.running,
            scope: AppNotificationScope.synced,
            createdAt: now,
            updatedAt: now,
          ),
        ),
      );
      expect(controller.state.active, isEmpty);

      controller.applyRemoteChange(
        AppNotificationRemoteChange(
          version: 2,
          operationId: 'semantic-preflight:machine-1:agent-1:job-1',
          record: AppNotificationRecord(
            id: 'remote-completed',
            operationId: 'semantic-preflight:machine-1:agent-1:job-1',
            title: 'Agent 助手切换完成',
            message: '已完成',
            status: AppNotificationStatus.succeeded,
            scope: AppNotificationScope.synced,
            createdAt: now,
            updatedAt: now.add(const Duration(seconds: 1)),
            completedAt: now.add(const Duration(seconds: 1)),
          ),
        ),
      );
      expect(
        controller.state.active.single.status,
        AppNotificationStatus.succeeded,
      );
      controller.dispose();
    },
  );

  test(
    'local progress updates do not unhide a dismissed running operation',
    () async {
      final repository = _MemoryRepository();
      final controller = _controller(repository);
      await controller.hydrate();

      controller.start(
        operationId: 'semantic-preflight:machine-1:agent-1:job-1',
        title: '正在切换 Agent 助手',
        message: '检测中',
        progressMode: AppNotificationProgressMode.determinate,
        progress: 0.1,
      );
      controller.hideOperation('semantic-preflight:machine-1:agent-1:job-1');
      controller.update(
        operationId: 'semantic-preflight:machine-1:agent-1:job-1',
        message: '正在下载运行时',
        progress: 0.45,
      );
      controller.upsert(
        AppNotificationRecord(
          id: 'local-running',
          operationId: 'semantic-preflight:machine-1:agent-1:job-1',
          title: '正在切换 Agent 助手',
          message: '正在下载运行时',
          status: AppNotificationStatus.running,
          progressMode: AppNotificationProgressMode.determinate,
          progress: 0.45,
          createdAt: DateTime.utc(2026, 8, 22, 10),
          updatedAt: DateTime.utc(2026, 8, 22, 10, 1),
        ),
      );

      expect(controller.state.active, isEmpty);
      controller.dispose();
    },
  );

  test('flush persists the latest deduplicated snapshot', () async {
    final repository = _MemoryRepository();
    final controller = _controller(repository);
    await controller.hydrate();

    controller.start(
      operationId: 'mcp:sync:1',
      title: 'MCP 同步中',
      message: '准备同步',
    );
    controller.update(operationId: 'mcp:sync:1', message: '等待设备响应');
    await controller.flush();

    expect(repository.saved.active, hasLength(1));
    expect(repository.saved.active.single.message, '等待设备响应');
    controller.dispose();
  });

  test(
    'hydrate merges work started before repository load completes',
    () async {
      final now = DateTime.utc(2026, 8, 16);
      final repository = _DelayedMemoryRepository(
        AppNotificationSnapshot(
          active: [_record('restored', AppNotificationStatus.running, now)],
        ),
      );
      final controller = _controller(repository);

      final hydration = controller.hydrate();
      controller.start(
        operationId: 'new-operation',
        title: '新任务',
        message: '启动中',
      );
      repository.release();
      await hydration;

      expect(
        controller.state.active.map((item) => item.operationId),
        containsAll(['restored', 'new-operation']),
      );
      expect(controller.state.collapsed, isFalse);
      controller.dispose();
    },
  );

  test(
    'remote tombstone removes an operation without creating an echo',
    () async {
      final repository = _MemoryRepository();
      final controller = _controller(repository);
      await controller.hydrate();
      final timestamp = DateTime.utc(2026, 8, 16);
      final record = _record(
        'remote-operation',
        AppNotificationStatus.running,
        timestamp,
      ).copyWith(scope: AppNotificationScope.synced);

      controller.applyRemoteChange(
        AppNotificationRemoteChange(
          version: 1,
          operationId: record.operationId,
          record: record,
        ),
      );
      expect(controller.state.active, hasLength(1));
      controller.applyRemoteChange(
        const AppNotificationRemoteChange(
          version: 2,
          operationId: 'remote-operation',
          deleted: true,
        ),
      );

      expect(controller.state.active, isEmpty);
      expect(controller.state.recent, isEmpty);
      controller.dispose();
    },
  );

  test(
    'an older remote update cannot regress a completed notification',
    () async {
      final repository = _MemoryRepository();
      final controller = _controller(repository);
      await controller.hydrate();
      controller.start(
        operationId: 'semantic-preflight:machine:agent',
        title: '切换 Agent',
        message: '处理中',
        scope: AppNotificationScope.synced,
      );
      controller.succeed('semantic-preflight:machine:agent', message: '切换完成');
      final completed = controller.state.active.single;
      final stale = completed.copyWith(
        status: AppNotificationStatus.running,
        message: '迟到的运行状态',
        progress: 0.5,
        updatedAt: completed.updatedAt.subtract(const Duration(seconds: 1)),
        completedAt: null,
      );

      controller.applyRemoteChange(
        AppNotificationRemoteChange(
          version: 12,
          operationId: stale.operationId,
          record: stale,
        ),
      );

      expect(
        controller.state.active.single.status,
        AppNotificationStatus.succeeded,
      );
      expect(controller.state.active.single.message, '切换完成');
      controller.dispose();
    },
  );

  test(
    'attention haptic fires once per critical or user-action transition',
    () async {
      final repository = _MemoryRepository();
      var hapticCount = 0;
      final controller = AppNotificationController(
        repository: repository,
        hapticFeedback: () async {
          hapticCount++;
        },
        collapseDelay: const Duration(seconds: 30),
        archiveDelay: const Duration(seconds: 30),
      );
      await controller.hydrate();

      controller.start(
        operationId: 'user-action',
        title: '需要确认',
        message: '请在电脑端确认',
      );
      controller.update(
        operationId: 'user-action',
        attention: AppNotificationAttention.userAction,
      );
      controller.update(
        operationId: 'user-action',
        message: '仍在等待',
        attention: AppNotificationAttention.userAction,
      );
      controller.start(operationId: 'critical', title: '安装', message: '正在安装');
      controller.fail('critical', error: '安装失败', critical: true);
      await Future<void>.delayed(Duration.zero);

      expect(hapticCount, 2);
      controller.dispose();
    },
  );
}

AppNotificationController _controller(_MemoryRepository repository) {
  return AppNotificationController(
    repository: repository,
    collapseDelay: const Duration(seconds: 30),
    archiveDelay: const Duration(milliseconds: 20),
    persistenceDelay: const Duration(milliseconds: 5),
  );
}

AppNotificationRecord _record(
  String operationId,
  AppNotificationStatus status,
  DateTime timestamp,
) {
  return AppNotificationRecord(
    id: 'id-$operationId',
    operationId: operationId,
    title: operationId,
    message: operationId,
    status: status,
    createdAt: timestamp,
    updatedAt: timestamp,
    completedAt: status == AppNotificationStatus.succeeded ? timestamp : null,
  );
}

class _MemoryRepository implements AppNotificationRepository {
  AppNotificationSnapshot saved;

  _MemoryRepository([this.saved = const AppNotificationSnapshot()]);

  @override
  Future<AppNotificationSnapshot> load() async => saved;

  @override
  Future<void> save(AppNotificationSnapshot snapshot) async {
    saved = snapshot;
  }
}

class _DelayedMemoryRepository extends _MemoryRepository {
  final _ready = Completer<void>();

  _DelayedMemoryRepository(super.saved);

  void release() => _ready.complete();

  @override
  Future<AppNotificationSnapshot> load() async {
    await _ready.future;
    return saved;
  }
}
