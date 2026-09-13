import 'package:chat_codex_app/shared/widgets/widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('AppSelect opens custom menu and selects an option', (
    tester,
  ) async {
    var selected = 'responses';

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(
            builder: (context, setState) {
              return AppSelect<String>(
                label: '接口模式',
                value: selected,
                options: const [
                  AppSelectOption(value: 'responses', label: 'Responses API'),
                  AppSelectOption(value: 'chat', label: 'Chat API'),
                ],
                onChanged: (value) => setState(() => selected = value),
              );
            },
          ),
        ),
      ),
    );

    expect(find.text('Responses API'), findsOneWidget);
    expect(find.text('Chat API'), findsNothing);

    await tester.tap(find.text('Responses API'));
    await tester.pumpAndSettle();

    expect(find.text('Chat API'), findsOneWidget);

    await tester.tap(find.text('Chat API'));
    await tester.pumpAndSettle();

    expect(selected, 'chat');
    expect(find.text('Chat API'), findsOneWidget);
  });
}
