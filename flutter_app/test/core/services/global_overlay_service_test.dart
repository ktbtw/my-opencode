import 'package:chat_codex_app/core/services/global_overlay_service.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('global overlay stop requires explicit confirmation', (
    tester,
  ) async {
    bool? confirmed;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            onPressed: () async {
              confirmed = await confirmGlobalOverlayStop(context);
            },
            child: const Text('关闭入口'),
          ),
        ),
      ),
    );

    await tester.tap(find.text('关闭入口'));
    await tester.pumpAndSettle();

    expect(find.text('关闭全局助手？'), findsOneWidget);
    expect(find.textContaining('不会在应用启动时自动恢复'), findsOneWidget);
    expect(find.widgetWithText(OutlinedButton, '取消'), findsOneWidget);
    expect(find.widgetWithText(FilledButton, '关闭'), findsOneWidget);

    await tester.tap(find.widgetWithText(FilledButton, '关闭'));
    await tester.pumpAndSettle();

    expect(confirmed, isTrue);
  });
}
