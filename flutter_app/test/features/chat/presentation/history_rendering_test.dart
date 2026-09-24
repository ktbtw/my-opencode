import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_page.dart';
import 'package:chat_codex_app/features/settings/settings_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  testWidgets(
    'missing tool positions render below body and detail hint is visible without a connection bar',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      const tool = ToolCallInfo(
        id: 'read',
        callId: 'read',
        tool: 'read',
        status: 'completed',
      );
      final message = ChatMessage(
        id: 'reply',
        role: MessageRole.agent,
        state: MessageState.done,
        content: '完整正文',
        createdAt: DateTime(2026),
        historyDetails: HistoryDetailsState.loading,
        toolOrderKnown: false,
        toolCalls: [tool],
        blocks: [
          const ChatMessageBlock.tool(id: 't', tool: tool),
          const ChatMessageBlock.text(id: 'text', text: '完整正文'),
        ],
      );
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
            home: Scaffold(
              body: SingleChildScrollView(
                child: buildChatMessageHistoryTestSurface(message),
              ),
            ),
          ),
        ),
      );
      await tester.pump();
      final body = find.text('完整正文', findRichText: true);
      final history = find.text('工具调用记录 · 原始位置未记录');
      expect(body, findsOneWidget);
      expect(history, findsOneWidget);
      expect(
        tester.getTopLeft(history).dy,
        greaterThan(tester.getTopLeft(body).dy),
      );
      expect(find.text('正在恢复工具调用历史'), findsOneWidget);
      expect(find.text('正在等待回复'), findsNothing);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );
}
