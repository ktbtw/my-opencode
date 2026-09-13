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

  testWidgets('Agent sort mode exits immediately when completed', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    await AppStorage.init();
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final device = DeviceModel.fromJson({
      'machine_id': 'machine-a',
      'hostname': 'MACBOOK-A',
      'display_name': '开发电脑',
      'status': 'offline',
      'agents': [
        {
          'agent_id': 'agent-a',
          'status': 'offline',
          'projects': [
            {'project_id': 'project-a'},
          ],
        },
      ],
    });

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          deviceDetailProvider('machine-a').overrideWith((ref) async => device),
        ],
        child: const MaterialApp(
          home: DeviceDetailPage(machineId: 'machine-a'),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('project-a'), findsOneWidget);
    expect(tester.takeException(), isNull);

    await tester.longPress(find.text('project-a'));
    await tester.pumpAndSettle();
    expect(find.byTooltip('完成排序'), findsOneWidget);
    expect(find.byType(FloatingActionButton), findsOneWidget);
    expect(find.byIcon(Icons.drag_indicator_rounded), findsWidgets);

    await tester.tap(find.byTooltip('完成排序'));
    await tester.pumpAndSettle();
    expect(find.byTooltip('完成排序'), findsNothing);
    expect(find.byType(FloatingActionButton), findsNothing);
    expect(find.byIcon(Icons.drag_indicator_rounded), findsNothing);
    expect(tester.takeException(), isNull);
  });
}
