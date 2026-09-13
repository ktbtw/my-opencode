import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/devices/data/device_model.dart';
import 'package:chat_codex_app/features/devices/presentation/device_detail_page.dart';
import 'package:chat_codex_app/features/devices/presentation/device_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
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

    // 默认整页布局
    expect(find.text('Agent 列表'), findsOneWidget);
    expect(find.text('处理'), findsNothing);

    // 切到分页布局，默认展示「设备」页
    await tester.tap(find.byTooltip('切换为分页布局'));
    await tester.pumpAndSettle();
    expect(find.text('Agent 列表'), findsNothing);
    expect(find.text('设备'), findsOneWidget);
    expect(find.text('处理'), findsOneWidget);
    expect(find.text('空闲'), findsOneWidget);
    expect(find.text('离线'), findsOneWidget);
    expect(find.text('AI配置'), findsOneWidget);
    expect(find.text('p-busy-1'), findsNothing);

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
    expect(AppStorage.getString('device_detail_paged_tab_machine-a'), '2');

    // 离线页
    await tester.tap(find.text('离线'));
    await tester.pumpAndSettle();
    expect(find.text('p-off'), findsOneWidget);
    expect(find.text('p-idle'), findsNothing);

    // 布局偏好按设备持久化
    expect(
      AppStorage.getString('device_detail_layout_mode_machine-a'),
      'paged',
    );

    // 切回整页
    await tester.tap(find.byTooltip('切换为整页布局'));
    await tester.pumpAndSettle();
    expect(find.text('Agent 列表'), findsOneWidget);
    expect(AppStorage.getString('device_detail_layout_mode_machine-a'), 'full');
    expect(tester.takeException(), isNull);
  });

  testWidgets('restores the last paged tab for each device', (tester) async {
    SharedPreferences.setMockInitialValues({
      'device_detail_layout_mode_machine-a': 'paged',
      'device_detail_paged_tab_machine-a': '2',
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
}
