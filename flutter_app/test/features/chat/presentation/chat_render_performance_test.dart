import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_page.dart';
import 'package:chat_codex_app/features/chat/presentation/message_renderer.dart';
import 'package:chat_codex_app/features/settings/settings_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  testWidgets(
    'very large replies keep the complete body without Markdown parsing',
    (tester) async {
      final body = '${'正文行，保留完整内容。\n' * 6000}末尾标记';
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SingleChildScrollView(
              child: MessageRenderer(
                content: body,
                isUser: false,
                settings: const AppSettings(),
              ),
            ),
          ),
        ),
      );
      await tester.pump();
      expect(find.byType(MarkdownBody), findsNothing);
      expect(find.byType(SelectableText), findsOneWidget);
      expect(find.textContaining('正文分片 1 /'), findsOneWidget);
      for (var i = 0; i < 10; i++) {
        final next = find.byTooltip('下一段');
        if (next.evaluate().isEmpty) break;
        await tester.ensureVisible(next);
        await tester.tap(next);
        await tester.pump();
      }
      expect(find.textContaining('末尾标记', findRichText: true), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'unchanged long body stays out of rebuilds while status/settings update',
    (tester) async {
      final body = List.generate(
        70,
        (i) => '### 检查 $i\n\n正文 **保留样式**，工具状态更新不应重新解析这些内容。',
      ).join('\n\n');
      var markdownBuilds = 0;
      debugOnRebuildDirtyWidget = (element, builtOnce) {
        if (element.widget is MarkdownBody) markdownBuilds++;
      };
      addTearDown(() => debugOnRebuildDirtyWidget = null);
      Future<void> render(String text, AppSettings settings) =>
          tester.pumpWidget(
            MaterialApp(
              home: Scaffold(
                body: SingleChildScrollView(
                  child: MessageRenderer(
                    content: text,
                    isUser: false,
                    settings: settings,
                  ),
                ),
              ),
            ),
          );
      await render(body, const AppSettings());
      await tester.pumpAndSettle();
      markdownBuilds = 0;
      final watch = Stopwatch()..start();
      for (var i = 0; i < 20; i++) {
        await render(body, AppSettings(showTokenCount: i.isEven));
      }
      watch.stop();
      expect(
        markdownBuilds,
        0,
        reason: 'Unchanged body must not reparse during status updates.',
      );
      debugPrint(
        'stable body: 20 updates, ${watch.elapsedMilliseconds}ms, $markdownBuilds Markdown builds',
      );
      await render('$body\n\n最新追加', const AppSettings());
      expect(find.text('最新追加'), findsOneWidget);
      expect(markdownBuilds, greaterThan(0));
      await render(body, const AppSettings(markdownRender: false));
      expect(find.byType(MarkdownBody), findsNothing);
      expect(find.byType(SelectableText), findsOneWidget);
    },
  );

  for (final continuous in [false, true]) {
    testWidgets(
      'long history remains lazy while entering and scrolling, continuous=$continuous',
      (tester) async {
        SharedPreferences.setMockInitialValues({'continuousReply': continuous});
        final messages = <ChatMessage>[];
        for (var i = 0; i < 80; i++) {
          if (i > 0) {
            messages.add(
              ChatMessage(
                id: 'event$i',
                role: MessageRole.user,
                state: MessageState.done,
                content: '后台结果 $i',
                taskId: 'task',
                createdAt: DateTime(2026),
                isInsertedContext: true,
                insertedContextType: 'backgroundJob',
                insertedContextMetadata: {'job_title': '检查 $i'},
              ),
            );
          }
          messages.add(
            ChatMessage(
              id: 'reply$i',
              role: MessageRole.agent,
              state: MessageState.done,
              content: '### 回复 $i\n\n${'这一段正文保留 Markdown 展示与原始工具顺序。' * 8}',
              taskId: 'task',
              createdAt: DateTime(2026),
            ),
          );
        }
        final controller = ScrollController();
        addTearDown(controller.dispose);
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
                body: buildChatMessageListTestSurface(messages, controller),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        final entering = find.byType(MarkdownBody).evaluate().length;
        expect(
          entering,
          lessThan(10),
          reason: 'Offscreen segments must not mount with the visible reply.',
        );
        expect(find.text('回复 79'), findsNothing);
        await tester.drag(find.byType(ListView), const Offset(0, -1600));
        await tester.pumpAndSettle();
        expect(controller.offset, greaterThan(0));
        final scrolled = find.byType(MarkdownBody).evaluate().length;
        expect(scrolled, lessThan(10));
        expect(tester.takeException(), isNull);
        debugPrint(
          'continuous=$continuous: mounted Markdown on entry=$entering, after scrolling=$scrolled, total=80',
        );
        await tester.pumpWidget(const SizedBox.shrink());
      },
    );
  }
}
