import 'package:chat_codex_app/core/theme/app_theme.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/devices/data/device_ai_config_model.dart';
import 'package:chat_codex_app/features/devices/presentation/device_ai_config_page.dart';
import 'package:chat_codex_app/features/devices/presentation/widgets/model_speed_test_button.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('prefers first-token duration for the speed chip', () {
    expect(
      modelSpeedTestDuration(
        _result(
          success: true,
          firstTextTime: const Duration(milliseconds: 1200),
          totalTime: const Duration(seconds: 4),
        ),
      ),
      const Duration(milliseconds: 1200),
    );
    expect(
      formatModelSpeedDuration(const Duration(milliseconds: 850)),
      '850ms',
    );
    expect(formatModelSpeedDuration(const Duration(milliseconds: 1200)), '1.2s');
  });

  testWidgets('shows a non-interactive spinner while testing', (tester) async {
    var tapped = false;
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light,
        home: Scaffold(
          body: ModelSpeedTestButton(
            testing: true,
            onPressed: () => tapped = true,
          ),
        ),
      ),
    );

    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    await tester.tap(find.byType(ModelSpeedTestButton));
    expect(tapped, isFalse);
  });

  testWidgets('replaces the icon with duration after a successful test', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light,
        home: Scaffold(
          body: ModelSpeedTestButton(
            testing: false,
            duration: const Duration(milliseconds: 1500),
            onPressed: () {},
          ),
        ),
      ),
    );

    expect(find.text('1.5s'), findsOneWidget);
    expect(find.byIcon(Icons.speed_rounded), findsNothing);
  });

  testWidgets('tests checked models one after another and writes durations', (
    tester,
  ) async {
    _setDesktopSurface(tester);
    final started = <String>[];
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light,
        home: ProviderModelsDialog(
          provider: const DeviceAIProviderInfo(id: 'openrouter'),
          items: const [
            DeviceAIModelInfo(id: 'alpha', name: 'Alpha', ownedBy: 'openrouter'),
            DeviceAIModelInfo(id: 'beta', name: 'Beta', ownedBy: 'openrouter'),
          ],
          initialSelected: const {'alpha', 'beta'},
          initialDefault: '',
          onTestModel: (model) async {
            started.add(model.id);
            await Future<void>.delayed(const Duration(milliseconds: 40));
            return _result(
              success: true,
              model: model.id,
              firstTextTime: Duration(milliseconds: model.id == 'alpha' ? 900 : 1500),
              totalTime: const Duration(seconds: 3),
            );
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const ValueKey('model-speed-test-selected')));
    await tester.pump();
    expect(started, ['alpha']);
    expect(find.byType(CircularProgressIndicator), findsWidgets);

    await tester.pump(const Duration(milliseconds: 50));
    await tester.pump();
    expect(started, ['alpha', 'beta']);

    await tester.pump(const Duration(milliseconds: 50));
    await tester.pumpAndSettle();

    expect(find.text('900ms'), findsOneWidget);
    expect(find.text('1.5s'), findsOneWidget);
  });

  testWidgets('single-model retest stays disabled while that model is running', (
    tester,
  ) async {
    _setDesktopSurface(tester);
    var calls = 0;
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light,
        home: ProviderModelsDialog(
          provider: const DeviceAIProviderInfo(id: 'openrouter'),
          items: const [
            DeviceAIModelInfo(id: 'alpha', name: 'Alpha', ownedBy: 'openrouter'),
          ],
          initialSelected: const {'alpha'},
          initialDefault: '',
          onTestModel: (_) async {
            calls += 1;
            await Future<void>.delayed(const Duration(milliseconds: 80));
            return _result(
              success: true,
              firstTextTime: const Duration(milliseconds: 700),
              totalTime: const Duration(seconds: 2),
            );
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const ValueKey('model-speed-test-alpha')));
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey('model-speed-test-alpha')));
    await tester.pump();
    expect(calls, 1);

    await tester.pump(const Duration(milliseconds: 100));
    await tester.pumpAndSettle();
    expect(find.text('700ms'), findsOneWidget);
  });
}

void _setDesktopSurface(WidgetTester tester) {
  tester.view.physicalSize = const Size(1200, 900);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
}

ModelLatencyTestResult _result({
  required bool success,
  String model = 'openrouter/alpha',
  Duration? firstTextTime,
  Duration? totalTime,
}) {
  final started = DateTime(2026, 1, 1);
  return ModelLatencyTestResult(
    taskId: 'test',
    sessionId: 'session',
    model: model,
    startedAt: started,
    completedAt: started.add(totalTime ?? Duration.zero),
    success: success,
    firstTextTime: firstTextTime,
    totalTime: totalTime,
  );
}
