import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/devices/data/device_model.dart';
import 'package:chat_codex_app/core/config/sse_parser.dart';
import 'package:chat_codex_app/features/devices/presentation/completed_agents_provider.dart';
import 'package:chat_codex_app/features/devices/presentation/device_detail_page.dart';
import 'package:chat_codex_app/features/devices/presentation/device_provider.dart';
import 'package:chat_codex_app/features/devices/presentation/device_metrics_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  DeviceModel device() => DeviceModel.fromJson({
    'machine_id': 'machine-a',
    'hostname': 'MACBOOK-A',
    'display_name': '开发电脑',
    'status': 'online',
    'agents': [
      {
        'agent_id': 'a-busy-1',
        'status': 'online',
        'current_task_id': 'task-1',
        'projects': [
          {'project_id': 'p-busy-1'},
        ],
      },
      {
        'agent_id': 'a-busy-2',
        'status': 'online',
        'current_task_id': 'task-2',
        'projects': [
          {'project_id': 'p-busy-2'},
        ],
      },
      {
        'agent_id': 'a-idle',
        'status': 'online',
        'projects': [
          {'project_id': 'p-idle'},
        ],
      },
      {
        'agent_id': 'a-off',
        'status': 'offline',
        'projects': [
          {'project_id': 'p-off'},
        ],
      },
    ],
  });

  Future<void> pumpPage(WidgetTester tester, Size size) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          // Layout assertions use fixture state, not a live SSE connection.
          deviceMetricsLiveProvider(
            'machine-a',
          ).overrideWith((ref) => const Stream.empty()),
          deviceDetailProvider(
            'machine-a',
          ).overrideWith((ref) async => device()),
        ],
        child: const MaterialApp(
          home: DeviceDetailPage(machineId: 'machine-a'),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('toggles between full and paged layout and filters agents', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    await AppStorage.init();
    await pumpPage(tester, const Size(390, 844));

    // 默认就是分片（分页）布局，直接展示「设备」页
    expect(find.text('Agent 列表'), findsNothing);
    expect(find.text('设备'), findsOneWidget);
    expect(find.text('处理'), findsOneWidget);
    expect(find.text('完成'), findsOneWidget);
    expect(find.text('空闲'), findsOneWidget);
    expect(find.text('离线'), findsOneWidget);
    expect(find.text('AI配置'), findsOneWidget);
    expect(find.text('p-busy-1'), findsNothing);
    // 默认值不落盘，只有用户显式切换才写偏好
    expect(AppStorage.getString('device_detail_layout_mode_machine-a'), isNull);

    // 处理页只显示处理中的 Agent
    await tester.tap(find.text('处理'));
    await tester.pumpAndSettle();
    expect(find.text('p-busy-1'), findsOneWidget);
    expect(find.text('p-busy-2'), findsOneWidget);
    expect(find.text('p-idle'), findsNothing);
    expect(find.text('p-off'), findsNothing);

    // 空闲页
    await tester.tap(find.text('空闲'));
    await tester.pumpAndSettle();
    expect(find.text('p-idle'), findsOneWidget);
    expect(find.text('p-busy-1'), findsNothing);
    // 新增「完成」后空闲索引后移为 3
    expect(AppStorage.getString('device_detail_paged_tab_v2_machine-a'), '3');

    // 离线页
    await tester.tap(find.text('离线'));
    await tester.pumpAndSettle();
    expect(find.text('p-off'), findsOneWidget);
    expect(find.text('p-idle'), findsNothing);

    // 切到整页布局并落盘
    await tester.tap(find.byTooltip('切换为整页布局'));
    await tester.pumpAndSettle();
    expect(find.text('Agent 列表'), findsOneWidget);
    expect(AppStorage.getString('device_detail_layout_mode_machine-a'), 'full');

    // 再切回分片
    await tester.tap(find.byTooltip('切换为分页布局'));
    await tester.pumpAndSettle();
    expect(find.text('Agent 列表'), findsNothing);
    expect(
      AppStorage.getString('device_detail_layout_mode_machine-a'),
      'paged',
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('restores the last paged tab for each device', (tester) async {
    SharedPreferences.setMockInitialValues({
      'device_detail_layout_mode_machine-a': 'paged',
      'device_detail_paged_tab_v2_machine-a': '3',
    });
    await AppStorage.init();
    await pumpPage(tester, const Size(390, 844));

    expect(find.text('p-idle'), findsOneWidget);
    expect(find.text('p-busy-1'), findsNothing);
    expect(find.text('p-off'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('paged agent tab lays out two columns on desktop width', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({
      'device_detail_layout_mode_machine-a': 'paged',
    });
    await AppStorage.init();
    await pumpPage(tester, const Size(1280, 900));

    // 恢复分页偏好，直接处于分页布局
    expect(find.text('Agent 列表'), findsNothing);
    await tester.tap(find.text('处理'));
    await tester.pumpAndSettle();

    final first = tester.getTopLeft(find.text('p-busy-1'));
    final second = tester.getTopLeft(find.text('p-busy-2'));
    expect(first.dy, second.dy);
    expect(second.dx, greaterThan(first.dx));
    expect(tester.takeException(), isNull);
  });

  testWidgets('paged agent tab shows empty state when no agents match', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({
      'device_detail_layout_mode_machine-a': 'paged',
    });
    await AppStorage.init();
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          deviceMetricsLiveProvider(
            'machine-a',
          ).overrideWith((ref) => const Stream.empty()),
          deviceDetailProvider('machine-a').overrideWith(
            (ref) async => DeviceModel.fromJson({
              'machine_id': 'machine-a',
              'hostname': 'MACBOOK-A',
              'status': 'online',
              'agents': [
                {
                  'agent_id': 'a-idle',
                  'status': 'online',
                  'projects': [
                    {'project_id': 'p-idle'},
                  ],
                },
              ],
            }),
          ),
        ],
        child: const MaterialApp(
          home: DeviceDetailPage(machineId: 'machine-a'),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('离线'));
    await tester.pumpAndSettle();
    expect(find.text('当前没有离线的 Agent'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('跑完的 Agent 进「完成」，查看对话后回到「空闲」', (tester) async {
    SharedPreferences.setMockInitialValues({});
    await AppStorage.init();

    DeviceModel singleAgent({required bool running}) => DeviceModel.fromJson({
      'machine_id': 'machine-a',
      'hostname': 'MACBOOK-A',
      'status': 'online',
      'agents': [
        {
          'agent_id': 'a-run',
          'status': 'online',
          if (running) 'current_task_id': 'task-1',
          'projects': [
            {'project_id': 'p-run'},
          ],
        },
      ],
    });

    var current = singleAgent(running: true);
    final container = ProviderContainer(
      overrides: [
        deviceMetricsLiveProvider(
          'machine-a',
        ).overrideWith((ref) => const Stream.empty()),
        // 与线上一致：实时事件走 invalidate（保留上一帧数据），
        // 这样分页容器不会被骨架屏重建，才能观察到"跑完"的状态迁移。
        deviceDetailProvider('machine-a').overrideWith((ref) async => current),
      ],
    );
    addTearDown(container.dispose);

    final router = GoRouter(
      initialLocation: '/devices/machine-a',
      routes: [
        GoRoute(
          path: '/devices/:machineId',
          builder: (_, __) => const DeviceDetailPage(machineId: 'machine-a'),
        ),
        GoRoute(path: '/chat', builder: (_, __) => const Text('agent chat')),
      ],
    );
    addTearDown(router.dispose);

    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await tester.pumpAndSettle();

    // 跑任务时在「处理」
    await tester.tap(find.text('处理'));
    await tester.pumpAndSettle();
    expect(find.text('p-run'), findsOneWidget);

    // 任务结束事件到达 + 设备快照刷新 → 进「完成」，卡片状态显示已完成
    current = singleAgent(running: false);
    container.invalidate(deviceDetailProvider('machine-a'));
    container
        .read(completedAgentsProvider.notifier)
        .handleEvent(
          const SseEvent(
            type: 'task.updated',
            data:
                '{"machine_id":"machine-a","agent_id":"a-run",'
                '"task_id":"task-1","status":"completed"}',
          ),
        );
    await tester.pumpAndSettle();
    await tester.tap(find.text('完成'));
    await tester.pumpAndSettle();
    expect(find.text('p-run'), findsOneWidget);
    expect(find.text('已完成'), findsOneWidget);
    expect(
      AppStorage.getString('completed_agents_v1'),
      '{"machine-a":["a-run"]}',
    );

    // 查看对话后回到「空闲」
    await tester.tap(find.text('进入对话'));
    await tester.pumpAndSettle();
    expect(find.text('agent chat'), findsOneWidget);
    router.pop();
    await tester.pumpAndSettle();

    await tester.tap(find.text('空闲'));
    await tester.pumpAndSettle();
    expect(find.text('p-run'), findsOneWidget);
    expect(AppStorage.getString('completed_agents_v1'), '{}');

    // 「完成」页已清空
    await tester.tap(find.text('完成'));
    await tester.pumpAndSettle();
    expect(find.text('当前没有待查看的完成结果'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
