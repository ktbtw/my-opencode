import 'package:chat_codex_app/core/theme/app_colors.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_page.dart';
import 'package:chat_codex_app/features/settings/settings_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  ChatMessage agentMessage() => ChatMessage(
    id: 'reply',
    role: MessageRole.agent,
    state: MessageState.done,
    content: '回复',
    createdAt: DateTime(2026),
  );

  Future<void> pumpSurface(WidgetTester tester, Widget surface) async {
    SharedPreferences.setMockInitialValues({});
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          settingsProvider.overrideWith(
            (ref) => SettingsNotifier(
              readRemote: () async => {},
              saveRemote: (_) async {},
            ),
          ),
        ],
        child: MaterialApp(
          home: Scaffold(body: SingleChildScrollView(child: surface)),
        ),
      ),
    );
    await tester.pump();
  }

  testWidgets('长按 agent 机器人图标会触发破甲模式切换', (tester) async {
    var toggled = 0;
    await pumpSurface(
      tester,
      buildChatMessageHistoryTestSurface(
        agentMessage(),
        onToggleJailbreak: () => toggled++,
      ),
    );

    await tester.longPress(find.byIcon(Icons.smart_toy_outlined));
    expect(toggled, 1);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('破甲模式开启时头像转为错误色', (tester) async {
    await pumpSurface(
      tester,
      buildChatMessageHistoryTestSurface(agentMessage(), jailbreakEnabled: true),
    );
    final active = tester.widget<Icon>(find.byIcon(Icons.smart_toy_outlined));
    expect(active.color, AppColors.statusError);
    await tester.pumpWidget(const SizedBox.shrink());

    await pumpSurface(
      tester,
      buildChatMessageHistoryTestSurface(agentMessage(), jailbreakEnabled: false),
    );
    final inactive = tester.widget<Icon>(find.byIcon(Icons.smart_toy_outlined));
    expect(inactive.color, AppColors.primary);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
