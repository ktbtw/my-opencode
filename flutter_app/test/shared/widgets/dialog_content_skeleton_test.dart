import 'package:chat_codex_app/shared/widgets/widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('DialogContentSkeleton fits a narrow dialog', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: Center(
            child: SizedBox(
              width: 240,
              child: DialogContentSkeleton(itemCount: 4),
            ),
          ),
        ),
      ),
    );

    expect(find.bySemanticsLabel('正在加载'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
