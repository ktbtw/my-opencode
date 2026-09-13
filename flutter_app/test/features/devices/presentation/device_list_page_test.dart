import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/devices/data/device_model.dart';
import 'package:chat_codex_app/features/devices/presentation/device_list_page.dart';
import 'package:chat_codex_app/features/devices/presentation/device_provider.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    await AppStorage.init();
  });

  testWidgets('scheme 2 hides identifiers and long press enters sort mode', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final devices = [
      DeviceModel.fromJson({
        'machine_id': 'internal-machine-id',
        'hostname': 'DESKTOP-INTERNAL-HOST',
        'display_name': '用于验证超长设备名称可以稳定显示并且不会把操作按钮挤出卡片',
        'status': 'online',
        'agents': [
          {
            'agent_id': 'agent-a',
            'status': 'running',
            'current_task_id': 'task-a',
            'projects': [
              {'project_id': 'Android 逆向项目'},
            ],
          },
        ],
      }),
    ];

    await tester.pumpWidget(
      ProviderScope(
        overrides: [deviceListProvider.overrideWith((ref) async => devices)],
        child: const MaterialApp(home: DeviceListPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('任务中'), findsOneWidget);
    expect(find.text('Android 逆向项目 正在运行'), findsOneWidget);
    expect(find.text('internal-machine-id'), findsNothing);
    expect(find.text('DESKTOP-INTERNAL-HOST'), findsNothing);
    expect(find.byTooltip('设备操作'), findsOneWidget);
    expect(tester.takeException(), isNull);

    await tester.longPress(find.textContaining('用于验证超长设备名称'));
    await tester.pumpAndSettle();
    expect(find.byTooltip('完成排序'), findsOneWidget);
    expect(find.byType(FloatingActionButton), findsOneWidget);
    expect(find.byIcon(Icons.drag_indicator_rounded), findsWidgets);
    expect(tester.takeException(), isNull);

    await tester.tap(find.byTooltip('完成排序'));
    await tester.pumpAndSettle();
    expect(find.byTooltip('完成排序'), findsNothing);
    expect(find.byType(FloatingActionButton), findsNothing);
    expect(find.byIcon(Icons.drag_indicator_rounded), findsNothing);
    expect(find.byTooltip('设备操作'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('dragging a device near the viewport edge scrolls the grid', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final devices = List.generate(
      12,
      (index) => DeviceModel.fromJson({
        'machine_id': 'machine-$index',
        'hostname': 'HOST-$index',
        'display_name': '设备 $index',
        'status': 'offline',
        'agents': const [],
      }),
    );

    await tester.pumpWidget(
      ProviderScope(
        overrides: [deviceListProvider.overrideWith((ref) async => devices)],
        child: const MaterialApp(home: DeviceListPage()),
      ),
    );
    await tester.pumpAndSettle();

    final scrollView = tester.widget<CustomScrollView>(
      find.byType(CustomScrollView),
    );
    expect(scrollView.controller!.offset, 0);

    final gesture = await tester.startGesture(
      tester.getCenter(find.text('设备 0')),
    );
    await tester.pump(kLongPressTimeout + const Duration(milliseconds: 100));
    expect(find.byTooltip('完成排序'), findsOneWidget);
    await gesture.up();
    await tester.pumpAndSettle();

    final reorderGesture = await tester.startGesture(
      tester.getCenter(find.text('设备 0')),
    );
    await reorderGesture.moveBy(const Offset(0, 32));
    await tester.pump();
    expect(find.text('设备 0'), findsNWidgets(2));
    await reorderGesture.moveTo(const Offset(195, 838));
    await tester.pump(const Duration(milliseconds: 400));

    expect(scrollView.controller!.offset, greaterThan(0));
    await reorderGesture.up();
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
  });

  testWidgets('shows the recent agent status and opens its chat', (
    tester,
  ) async {
    final devices = [
      DeviceModel.fromJson({
        'machine_id': 'machine-a',
        'hostname': 'HOST-A',
        'display_name': '设备 A',
        'status': 'online',
        'agents': [
          {
            'agent_id': 'agent-busy',
            'name': '造梦八荒',
            'semantic_agent_name': '逆向专家-安卓',
            'status': 'running',
            'current_task_id': 'task-a',
            'seen_at': '2026-08-22T08:00:00Z',
            'projects': [
              {'project_id': 'project-a', 'root': '/workspace/project-a'},
            ],
          },
          {
            'agent_id': 'agent-recent',
            'semantic_agent_name': '最近在线 Agent',
            'status': 'online',
            'seen_at': '2026-08-22T09:00:00Z',
            'projects': [
              {'project_id': 'project-b', 'root': '/workspace/project-b'},
            ],
          },
        ],
      }),
    ];
    final router = GoRouter(
      initialLocation: '/devices',
      routes: [
        GoRoute(path: '/devices', builder: (_, __) => const DeviceListPage()),
        GoRoute(
          path: '/devices/:machineId',
          builder: (_, __) => const Text('device detail'),
        ),
        GoRoute(path: '/chat', builder: (_, __) => const Text('agent chat')),
      ],
    );
    addTearDown(router.dispose);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [deviceListProvider.overrideWith((ref) async => devices)],
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('造梦八荒'), findsOneWidget);
    expect(find.text('逆向专家-安卓'), findsOneWidget);
    expect(
      find.ancestor(of: find.text('逆向专家-安卓'), matching: find.byType(FittedBox)),
      findsOneWidget,
    );
    expect(find.text('运行中'), findsOneWidget);
    expect(find.textContaining('最近 Agent'), findsNothing);

    await tester.tap(find.text('造梦八荒'));
    await tester.pumpAndSettle();
    expect(find.text('agent chat'), findsOneWidget);
    expect(find.text('device detail'), findsNothing);
  });

  testWidgets('shows multiple recent agents on desktop cards', (tester) async {
    tester.view.physicalSize = const Size(1200, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final devices = [
      DeviceModel.fromJson({
        'machine_id': 'machine-desktop',
        'hostname': 'HOST-DESKTOP',
        'display_name': '桌面设备',
        'status': 'online',
        'agents': [
          for (var index = 1; index <= 4; index++)
            {
              'agent_id': 'agent-$index',
              'name': 'Agent $index',
              'semantic_agent_name': '语义 Agent $index',
              'status': 'online',
              'seen_at': '2026-08-24T${10 + index}:00:00Z',
              'projects': [
                {
                  'project_id': 'project-$index',
                  'root': '/workspace/project-$index',
                },
              ],
            },
        ],
      }),
    ];

    await tester.pumpWidget(
      ProviderScope(
        overrides: [deviceListProvider.overrideWith((ref) async => devices)],
        child: const MaterialApp(home: DeviceListPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Agent 4'), findsOneWidget);
    expect(find.text('Agent 3'), findsOneWidget);
    expect(find.text('Agent 2'), findsOneWidget);
    expect(find.text('Agent 1'), findsNothing);
    expect(tester.takeException(), isNull);
  });
}
