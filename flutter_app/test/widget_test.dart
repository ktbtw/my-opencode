import 'package:flutter_test/flutter_test.dart';

import 'package:flutter_app/app/app.dart';

void main() {
  testWidgets('app shell renders', (tester) async {
    await tester.pumpWidget(const ChatCodexApp());

    expect(find.text('Chat Codex'), findsOneWidget);
    expect(find.text('当前目标'), findsOneWidget);
    expect(find.text('下一步建议'), findsOneWidget);
  });
}
