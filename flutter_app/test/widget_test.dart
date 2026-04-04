import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:flutter_app/app/app.dart';

void main() {
  testWidgets('登录页默认可见', (WidgetTester tester) async {
    await tester.pumpWidget(const ProviderScope(child: ChatCodexApp()));
    await tester.pumpAndSettle();

    expect(find.text('登录'), findsOneWidget);
    expect(find.text('进入控制台'), findsOneWidget);
  });
}
