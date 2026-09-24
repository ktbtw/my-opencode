import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_page.dart';
import 'package:chat_codex_app/features/chat/presentation/reply_display_group.dart';
import 'package:chat_codex_app/features/settings/settings_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

ChatMessage reply(String id, {String task = 'task', bool live = false}) =>
    ChatMessage(
      id: id,
      role: MessageRole.agent,
      taskId: task,
      content: id,
      state: live ? MessageState.streaming : MessageState.done,
      createdAt: DateTime(2026),
    );
ChatMessage event(String id, {String task = 'task', bool user = false}) =>
    ChatMessage(
      id: id,
      role: MessageRole.user,
      taskId: task,
      content: '结果详情 $id',
      state: MessageState.done,
      createdAt: DateTime(2026),
      isInsertedContext: true,
      insertedContextType: user ? 'queueInsert' : 'backgroundJob',
      insertedContextMetadata: {'job_title': id, 'job_status': 'completed'},
    );

Widget surface(List<ChatMessage> messages) => ProviderScope(
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
        child: buildContinuousReplyTestSurface(messages),
      ),
    ),
  ),
);

void main() {
  test('grouping preserves original objects, order and split mode', () {
    final messages = [
      reply('A'),
      event('job1'),
      event('job2'),
      reply('B'),
      event('job3'),
      reply('C'),
    ];
    expect(groupReplyDisplay(messages, continuous: false).length, 6);
    final group = groupReplyDisplay(messages, continuous: true).single;
    expect(group, messages);
    for (var i = 0; i < messages.length; i++) {
      expect(identical(group[i], messages[i]), isTrue);
    }
  });
  test('user insertions and different tasks are never merged', () {
    final messages = [
      reply('A'),
      event('job'),
      reply('B'),
      event('user', user: true),
      reply('C'),
      event('job2'),
      reply('D', task: 'other'),
    ];
    final groups = groupReplyDisplay(messages, continuous: true);
    expect(groups.map((g) => g.map((m) => m.id).toList()), [
      ['A', 'job', 'B'],
      ['user'],
      ['C'],
      ['job2'],
      ['D'],
    ]);
  });
  testWidgets('thinking, reconnect and stop hints stay in the reply footer', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final message = reply('正文仍在这里', live: true)
      ..thinkingContent = '推理细节'
      ..streamRecovery = StreamRecovery.reconnecting
      ..statusHint = '正在恢复连接';
    await tester.pumpWidget(surface([message]));
    await tester.pump();
    final body = find.byKey(const ValueKey('continuous-reply-surface'));
    final rect = tester.getRect(body);
    expect(tester.getBottomLeft(find.text('思考中...')).dy, lessThan(rect.top));
    expect(
      tester.getTopLeft(find.text('正在恢复连接')).dy,
      greaterThanOrEqualTo(rect.bottom),
    );
    expect(find.text('连接恢复中，正在重试'), findsNothing);
    expect(find.text('正在恢复连接'), findsOneWidget);
    expect(
      find.descendant(
        of: body,
        matching: find.byKey(const ValueKey('agent-activity-glyph')),
      ),
      findsNothing,
    );
    await tester.pumpWidget(
      surface([message.copyForUpdate()..statusHint = '正在停止']),
    );
    await tester.pump(const Duration(milliseconds: 250));
    expect(find.text('正在停止'), findsOneWidget);
    expect(
      tester.getTopLeft(find.text('正在停止')).dy,
      greaterThanOrEqualTo(tester.getRect(body).bottom),
    );
    expect(find.text('连接恢复中，正在重试'), findsNothing);
    await tester.pumpWidget(surface([reply('', live: true)]));
    await tester.pump();
    expect(
      find.byKey(const ValueKey('continuous-reply-surface')),
      findsNothing,
    );
    expect(find.text('正在等待回复'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets(
    'continuous surface keeps body order, inline details and one live indicator',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final first = reply('此前的正文')
        ..historyDetails = HistoryDetailsState.partial;
      final last = reply('接着继续回复', live: true);
      final messages = [first, event('检查文件'), last];
      await tester.pumpWidget(surface(messages));
      await tester.pump();
      expect(
        find.byKey(const ValueKey('continuous-reply-surface')),
        findsOneWidget,
      );
      expect(find.text('工具调用历史尚未完整载入'), findsNothing);
      expect(find.text('正在生成回复'), findsOneWidget);
      expect(find.text('结果详情 检查文件'), findsNothing);
      final before = tester
          .getTopLeft(find.text('此前的正文', findRichText: true))
          .dy;
      final middle = tester.getTopLeft(find.text('后台任务已完成 · 检查文件')).dy;
      final after = tester
          .getTopLeft(find.text('接着继续回复', findRichText: true))
          .dy;
      expect(before, lessThan(middle));
      expect(middle, lessThan(after));
      await tester.tap(find.text('展开'));
      await tester.pump(const Duration(milliseconds: 250));
      expect(find.text('结果详情 检查文件'), findsOneWidget);
      await tester.pumpWidget(
        surface([first, messages[1], last.copyForUpdate()..content = '新的正文']),
      );
      await tester.pump();
      expect(find.text('收起'), findsOneWidget);
      expect(find.text('结果详情 检查文件'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );
  testWidgets('restored events do not replay entry animation', (tester) async {
    SharedPreferences.setMockInitialValues({});
    await tester.pumpWidget(surface([reply('A'), event('job'), reply('B')]));
    final eventText = find.text('后台任务已完成 · job');
    final opacity = tester.widget<Opacity>(
      find.ancestor(of: eventText, matching: find.byType(Opacity)).first,
    );
    expect(opacity.opacity, 1);
    await tester.pumpAndSettle();
    expect(tester.binding.hasScheduledFrame, isFalse);
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
