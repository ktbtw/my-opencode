import 'package:chat_codex_app/core/notifications/app_notification_controller.dart';
import 'package:chat_codex_app/core/notifications/app_notification_host.dart';
import 'package:chat_codex_app/core/notifications/app_notification_model.dart';
import 'package:chat_codex_app/core/notifications/app_notification_repository.dart';
import 'package:chat_codex_app/core/services/app_navigation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

void main() {
  testWidgets('full notification collapses into island and expands again', (
    tester,
  ) async {
    final repository = _MemoryRepository();
    final container = await _pumpHost(tester, repository: repository);
    final controller = container.read(
      appNotificationControllerProvider.notifier,
    );

    controller.start(
      operationId: 'agent:start:1',
      title: '正在启动 Android Agent',
      message: '正在加载运行时与设备配置',
      progressMode: AppNotificationProgressMode.determinate,
      displayStyle: AppNotificationDisplayStyle.ring,
      progress: 0.68,
      kind: AppNotificationKind.agent,
      stages: const [
        AppNotificationStage(label: '连接设备', completed: true),
        AppNotificationStage(label: '检查环境', completed: true),
        AppNotificationStage(label: '加载运行时', active: true),
        AppNotificationStage(label: '验证状态'),
      ],
    );
    await tester.pump();

    expect(find.text('正在启动 Android Agent'), findsOneWidget);
    expect(find.text('68%'), findsOneWidget);

    await tester.tap(find.byIcon(Icons.keyboard_arrow_up_rounded));
    await tester.pump(const Duration(milliseconds: 300));

    expect(controller.state.collapsed, isTrue);
    final collapsedButton = find.byKey(
      const ValueKey('collapsed-notification-button'),
    );
    expect(collapsedButton, findsOneWidget);
    expect(tester.getSize(collapsedButton), const Size.square(56));
    expect(find.text('1 个任务进行中'), findsNothing);

    await tester.tap(collapsedButton);
    await tester.pump(const Duration(milliseconds: 300));

    expect(controller.state.collapsed, isFalse);
    expect(find.text('正在加载运行时与设备配置'), findsOneWidget);
    await _disposeHost(tester, container);
  });

  testWidgets(
    'collapsed agent switch island still shows determinate progress',
    (tester) async {
      final repository = _MemoryRepository();
      final container = await _pumpHost(tester, repository: repository);
      final controller = container.read(
        appNotificationControllerProvider.notifier,
      );

      controller.start(
        operationId: 'semantic-preflight:machine-1:agent-1:job-1',
        title: '正在切换 Agent 助手',
        message: '正在下载 Node.js',
        progressMode: AppNotificationProgressMode.determinate,
        displayStyle: AppNotificationDisplayStyle.stages,
        progress: 0.42,
        stages: const [
          AppNotificationStage(label: '运行时', active: true),
          AppNotificationStage(label: 'MCP'),
        ],
      );
      await tester.pump();
      controller.collapse();
      await tester.pump();

      expect(
        find.byKey(const ValueKey('collapsed-notification-progress')),
        findsOneWidget,
      );
      await tester.tap(find.byIcon(Icons.close_rounded));
      await tester.pump();
      expect(controller.state.active, isEmpty);
      await _disposeHost(tester, container);
    },
  );

  testWidgets(
    'running notification can be hidden without cancelling the task',
    (tester) async {
      final repository = _MemoryRepository();
      final container = await _pumpHost(tester, repository: repository);
      final controller = container.read(
        appNotificationControllerProvider.notifier,
      );
      controller.start(
        operationId: 'semantic-preflight:machine-1:agent-1:job-1',
        title: '正在切换 Agent 助手',
        message: '预检任务已下发，等待 launcher 开始执行',
        scope: AppNotificationScope.synced,
      );
      await tester.pump();

      await tester.tap(find.byIcon(Icons.close_rounded).first);
      await tester.pump();

      expect(controller.state.active, isEmpty);
      await controller.flush();
      expect(
        repository.snapshot.hiddenOperationIds,
        contains('semantic-preflight:machine-1:agent-1:job-1'),
      );
      await _disposeHost(tester, container);
    },
  );

  testWidgets('current chat completion is retained but hidden from the toast', (
    tester,
  ) async {
    final repository = _MemoryRepository();
    final container = await _pumpHost(tester, repository: repository);
    final controller = container.read(
      appNotificationControllerProvider.notifier,
    );
    controller.setForegroundChat(
      const AppNotificationChatContext(
        machineId: 'machine-1',
        agentId: 'agent-1',
        projectId: 'project-1',
      ),
    );
    controller.start(
      operationId: 'chat-task:task-1',
      title: '当前 Agent · 任务已完成',
      message: '回复已生成',
      kind: AppNotificationKind.agent,
      metadata: const {
        'machine_id': 'machine-1',
        'agent_id': 'agent-1',
        'project_id': 'project-1',
      },
    );
    controller.succeed('chat-task:task-1', message: '回复已生成');
    await tester.pump();

    expect(find.text('当前 Agent · 任务已完成'), findsNothing);
    expect(controller.state.active, hasLength(1));
    await _disposeHost(tester, container);
  });

  testWidgets('linear notification does not combine stages and progress ring', (
    tester,
  ) async {
    final repository = _MemoryRepository();
    final container = await _pumpHost(tester, repository: repository);
    final controller = container.read(
      appNotificationControllerProvider.notifier,
    );

    controller.start(
      operationId: 'file:download:single-style',
      title: '正在下载 app-debug.apk',
      message: '正在接收 app-debug.apk',
      progressMode: AppNotificationProgressMode.determinate,
      displayStyle: AppNotificationDisplayStyle.linear,
      progress: 0.42,
      kind: AppNotificationKind.file,
      stages: const [
        AppNotificationStage(label: '连接文件服务', completed: true),
        AppNotificationStage(label: '下载文件', active: true),
        AppNotificationStage(label: '保存到本机'),
      ],
    );
    await tester.pump();

    expect(find.byType(LinearProgressIndicator), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);
    expect(find.text('42%'), findsOneWidget);
    expect(find.text('连接文件服务'), findsNothing);
    expect(find.text('下载文件'), findsNothing);
    expect(find.text('保存到本机'), findsNothing);
    await _disposeHost(tester, container);
  });

  testWidgets('failed notification can still collapse into the island', (
    tester,
  ) async {
    final repository = _MemoryRepository();
    final container = await _pumpHost(tester, repository: repository);
    final controller = container.read(
      appNotificationControllerProvider.notifier,
    );

    controller.start(
      operationId: 'agent:switch:failed',
      title: '正在切换 Agent 助手',
      message: '正在验证 MCP',
      displayStyle: AppNotificationDisplayStyle.stages,
    );
    controller.fail(
      'agent:switch:failed',
      error: '缺少 Verify Token',
      critical: true,
    );
    expect(controller.state.active, hasLength(1));
    expect(controller.state.collapsed, isFalse);
    await tester.pump();

    expect(find.text('正在切换 Agent 助手'), findsOneWidget);
    await tester.tap(find.byIcon(Icons.keyboard_arrow_up_rounded));
    await tester.pump(const Duration(milliseconds: 300));

    expect(controller.state.collapsed, isTrue);
    expect(
      find.byKey(const ValueKey('collapsed-notification-button')),
      findsOneWidget,
    );
    expect(find.text('1 个任务已完成'), findsNothing);
    await _disposeHost(tester, container);
  });

  testWidgets(
    'stage notification shows overall percent and a linear progress bar',
    (tester) async {
      final repository = _MemoryRepository();
      final container = await _pumpHost(tester, repository: repository);
      final controller = container.read(
        appNotificationControllerProvider.notifier,
      );

      controller.start(
        operationId: 'agent:switch:runtime-progress',
        title: '正在切换 Android 逆向助手',
        message: '正在下载 Chat Codex JADX 镜像 · 16.9 MB/58.4 MB · 29%',
        progressMode: AppNotificationProgressMode.determinate,
        displayStyle: AppNotificationDisplayStyle.stages,
        progress: 0.52,
        kind: AppNotificationKind.agent,
        stages: const [
          AppNotificationStage(label: '运行时', active: true),
          AppNotificationStage(label: 'MCP'),
          AppNotificationStage(label: 'Skill'),
          AppNotificationStage(label: '应用'),
        ],
      );
      await tester.pump();

      expect(
        find.text('正在下载 Chat Codex JADX 镜像 · 16.9 MB/58.4 MB · 29%'),
        findsOneWidget,
      );
      expect(find.text('阶段 1/4 · 运行时 · 总进度 52%'), findsOneWidget);
      expect(find.byType(LinearProgressIndicator), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsNothing);
      await _disposeHost(tester, container);
    },
  );

  testWidgets('notification center switches tabs and clears recent history', (
    tester,
  ) async {
    final now = DateTime.now().toUtc();
    final repository = _MemoryRepository(
      AppNotificationSnapshot(
        recent: [
          AppNotificationRecord(
            id: 'done-1',
            operationId: 'done-1',
            title: 'Agent 配置已保存',
            message: '配置已应用',
            status: AppNotificationStatus.succeeded,
            createdAt: now,
            updatedAt: now,
            completedAt: now,
          ),
        ],
      ),
    );
    final container = await _pumpHost(tester, repository: repository);
    final controller = container.read(
      appNotificationControllerProvider.notifier,
    );
    controller.start(
      operationId: 'mcp:sync:1',
      title: 'MCP 状态同步中',
      message: '等待设备响应',
      kind: AppNotificationKind.mcp,
    );
    await tester.pump();

    await tester.tap(find.byTooltip('通知中心').last);
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.text('当前任务'), findsOneWidget);

    await tester.tap(find.text('最近记录'));
    await tester.pump();
    expect(find.text('Agent 配置已保存'), findsOneWidget);
    final recentTab = find.byKey(
      const ValueKey('notification-center-tab-recent'),
    );
    final recentLabel = find.byKey(
      const ValueKey('notification-center-tab-label-recent'),
    );
    final recentIndicator = find.byKey(
      const ValueKey('notification-center-tab-indicator-recent'),
    );
    expect(tester.getSize(recentTab).height, greaterThanOrEqualTo(52));
    expect(
      tester.getSize(recentIndicator).width,
      closeTo(tester.getSize(recentLabel).width, 0.5),
    );
    expect(
      tester.getTopLeft(recentLabel).dy - tester.getTopLeft(recentTab).dy,
      greaterThan(10),
    );

    await tester.tap(find.text('清除'));
    await tester.pump();
    expect(find.text('暂无最近通知'), findsOneWidget);
    await _disposeHost(tester, container);
  });

  testWidgets('mobile center uses a bottom panel without overflow', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final repository = _MemoryRepository();
    final container = await _pumpHost(tester, repository: repository);
    final controller = container.read(
      appNotificationControllerProvider.notifier,
    );
    controller.start(
      operationId: 'file:download:1',
      title: '正在下载 IDA 运行时',
      message: '等待远端返回文件总大小',
      kind: AppNotificationKind.installation,
    );
    controller.openCenter();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('正在下载 IDA 运行时'), findsOneWidget);
    expect(tester.takeException(), isNull);
    final panelTop = tester.getTopLeft(find.text('通知')).dy;
    expect(panelTop, greaterThan(100));
    await _disposeHost(tester, container);
  });

  testWidgets('chat action navigates through the app root navigator', (
    tester,
  ) async {
    final repository = _MemoryRepository();
    final container = ProviderContainer(
      overrides: [
        appNotificationRepositoryProvider.overrideWithValue(repository),
      ],
    );
    final router = GoRouter(
      navigatorKey: appNavigatorKey,
      initialLocation: '/devices',
      routes: [
        GoRoute(
          path: '/devices',
          builder: (_, _) => const Scaffold(body: Text('设备页')),
        ),
        GoRoute(
          path: '/chat',
          builder: (_, state) => Scaffold(
            body: Text('对话页:${state.uri.queryParameters['agentId'] ?? ''}'),
          ),
        ),
      ],
    );
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp.router(
          routerConfig: router,
          builder: (context, child) =>
              AppNotificationHost(child: child ?? const SizedBox.shrink()),
        ),
      ),
    );
    await tester.pump();

    final controller = container.read(
      appNotificationControllerProvider.notifier,
    );
    controller.start(
      operationId: 'chat-task:task-1',
      title: 'Agent · 任务已完成',
      message: '回复已生成',
      actions: const [
        AppNotificationAction(
          label: '查看对话',
          type: AppNotificationActionType.openRoute,
          payload: {'route': '/chat?agentId=agent-1'},
          primary: true,
        ),
      ],
    );
    controller.succeed('chat-task:task-1', message: '回复已生成');
    await tester.pump();

    await tester.tap(find.text('查看对话'));
    await tester.pumpAndSettle();

    expect(find.text('对话页:agent-1'), findsOneWidget);
    expect(controller.state.active, isEmpty);
    expect(controller.state.recent.single.operationId, 'chat-task:task-1');
    expect(controller.state.collapsed, isFalse);
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
    router.dispose();
    container.dispose();
  });
}

Future<ProviderContainer> _pumpHost(
  WidgetTester tester, {
  required AppNotificationRepository repository,
}) async {
  final container = ProviderContainer(
    overrides: [
      appNotificationRepositoryProvider.overrideWithValue(repository),
    ],
  );
  await tester.pumpWidget(
    UncontrolledProviderScope(
      container: container,
      child: MaterialApp(
        builder: (context, child) =>
            AppNotificationHost(child: child ?? const SizedBox.shrink()),
        home: const Scaffold(
          appBar: _TestTopBar(),
          body: Center(child: Text('页面内容')),
        ),
      ),
    ),
  );
  await tester.pump();
  return container;
}

Future<void> _disposeHost(
  WidgetTester tester,
  ProviderContainer container,
) async {
  await tester.pumpWidget(const SizedBox.shrink());
  container.dispose();
  await tester.pump();
}

class _TestTopBar extends StatelessWidget implements PreferredSizeWidget {
  const _TestTopBar();

  @override
  Size get preferredSize => const Size.fromHeight(56);

  @override
  Widget build(BuildContext context) {
    return AppBar(
      title: const Text('测试页面'),
      actions: const [AppNotificationCenterButton()],
    );
  }
}

class _MemoryRepository implements AppNotificationRepository {
  AppNotificationSnapshot snapshot;

  _MemoryRepository([this.snapshot = const AppNotificationSnapshot()]);

  @override
  Future<AppNotificationSnapshot> load() async => snapshot;

  @override
  Future<void> save(AppNotificationSnapshot next) async {
    snapshot = next;
  }
}
