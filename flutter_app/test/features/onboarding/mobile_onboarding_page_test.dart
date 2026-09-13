import 'package:chat_codex_app/features/onboarding/presentation/mobile_onboarding_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  setUp(() {
    TestWidgetsFlutterBinding.ensureInitialized();
  });

  testWidgets('immersive onboarding shows all built-in agents without skip', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 700);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(const MaterialApp(home: MobileOnboardingPage()));
    await tester.pump(const Duration(milliseconds: 100));

    expect(find.text('跳过'), findsNothing);
    expect(find.text('已有账号，直接登录'), findsNothing);
    expect(find.text('注册账号'), findsNothing);
    for (final name in const [
      '编码 Agent',
      '逆向专家-安卓',
      '逆向专家-ios',
      '逆向专家-mac',
      '逆向专家-web',
      '逆向专家-windows',
    ]) {
      expect(find.text(name), findsOneWidget);
    }
    expect(tester.takeException(), isNull);
  });

  testWidgets('login and register actions only appear on the final slide', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 700);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(const MaterialApp(home: MobileOnboardingPage()));
    await tester.pump(const Duration(milliseconds: 100));

    await tester.fling(find.byType(PageView), const Offset(-320, 0), 1000);
    await tester.pump(const Duration(seconds: 1));
    await tester.fling(find.byType(PageView), const Offset(-320, 0), 1000);
    await tester.pump(const Duration(seconds: 1));

    expect(find.text('已有账号，直接登录'), findsOneWidget);
    expect(find.text('注册账号'), findsOneWidget);
    expect(find.text('继续'), findsNothing);
    expect(tester.takeException(), isNull);
  });
}
