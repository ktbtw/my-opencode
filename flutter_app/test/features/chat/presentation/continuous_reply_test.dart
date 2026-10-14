import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_page.dart';
import 'package:chat_codex_app/features/chat/presentation/reply_display_group.dart';
import 'package:chat_codex_app/features/settings/settings_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

ChatMessage reply(
  String id, {
  String task = 'task',
  bool live = false,
  PlanInfo? plan,
}) {
  final message = ChatMessage(
    id: id,
    role: MessageRole.agent,
    taskId: task,
    content: id,
    state: live ? MessageState.streaming : MessageState.done,
    createdAt: DateTime(2026),
  );
  if (plan != null) {
    message.plan = plan;
    message.hasPlanBinding = true;
  }
  return message;
}
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
    // 用户插入不再把同任务正文切开，它嵌在前后回复中间。
    expect(groups.map((g) => g.map((m) => m.id).toList()), [
      ['A', 'job', 'B', 'user', 'C', 'job2'],
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
    // 显式 statusHint 覆盖内部文案：这里只应出现一次（来自 hint）。
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
    expect(find.text('正在恢复连接'), findsNothing);
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
      expect(find.text('历史尚未载入完整'), findsNothing);
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
  test('adjacent same-task replies merge into one surface', () {
    // 目标模式轮次、子代理唤醒轮次之间没有 background 事件，
    // 之前会因为「紧邻」被拆成多个气泡。
    final messages = [
      reply('round1'),
      reply('round2'),
      reply('round3', live: true),
      reply('other', task: 'other'),
      event('job', user: true),
      reply('after-user', task: 'task'),
    ];
    final groups = groupReplyDisplay(messages, continuous: true);
    expect(groups.map((g) => g.map((m) => m.id).toList()), [
      ['round1', 'round2', 'round3'],
      ['other'],
      ['job', 'after-user'],
    ]);
  });

  testWidgets('one group renders a single merged thinking block', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final first = reply('第一轮正文')
      ..thinkingContent = '第一轮推理';
    final last = reply('第二轮正文', live: true)
      ..thinkingContent = '第二轮推理';
    await tester.pumpWidget(surface([first, event('检查文件'), last]));
    await tester.pump();

    // 合并前：两段各渲染一个思考块，第二块夹在两条正文之间。
    expect(find.text('思考中...'), findsOneWidget);
    expect(find.text('已完成思考'), findsNothing);

    await tester.tap(find.text('思考中...'));
    await tester.pump();
    expect(find.text('第一轮推理\n\n第二轮推理'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('adjacent rounds share one thinking block too', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final first = reply('第一轮正文')..thinkingContent = '第一轮推理';
    final second = reply('第二轮正文', live: true)..thinkingContent = '第二轮推理';
    await tester.pumpWidget(surface([first, second]));
    await tester.pump();

    expect(find.text('思考中...'), findsOneWidget);
    expect(find.text('已完成思考'), findsNothing);
    await tester.tap(find.text('思考中...'));
    await tester.pump();
    expect(find.text('第一轮推理\n\n第二轮推理'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('one group renders the stats row once', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final messages = [
      reply('第一轮正文'),
      event('检查文件'),
      reply('第二轮正文'),
    ];
    await tester.pumpWidget(surface(messages));
    await tester.pump();

    // 两条已完成正文合并成一段后，统计行/复制按钮不该出现两次。
    expect(find.text('复制'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
  });

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

  test('plan card renders once and always at the end of the group', () {
    final plan = PlanInfo(
      id: 'plan-1',
      title: '拆解需求',
      mode: 'build',
      status: 'in_progress',
      items: const [
        PlanItemInfo(
          id: 'item-1',
          text: '读取配置',
          status: 'in_progress',
          priority: 'high',
        ),
      ],
    );
    final messages = [
      reply('A', plan: plan),
      reply('B', plan: plan),
      reply('C'),
    ];
    final layout = ReplyDisplayLayout.of(messages);
    // 同 task 三条消息合并成一段，任务卡片只渲染一次，且挂在最后一条正文上。
    expect(layout.planCards.length, 1);
    expect(layout.planCards.containsKey('C'), isTrue);
    expect(layout.planCards.containsKey('A'), isFalse);
    expect(layout.planCards.containsKey('B'), isFalse);
  });

  test('plan card stays at the tail when only an earlier round carries the plan', () {
    final plan = PlanInfo(
      id: 'plan-1',
      title: '拆解需求',
      mode: 'build',
      status: 'in_progress',
      items: const [
        PlanItemInfo(
          id: 'item-1',
          text: '读取配置',
          status: 'in_progress',
          priority: 'high',
        ),
      ],
    );
    final messages = [reply('A', plan: plan), reply('B'), reply('C')];
    final layout = ReplyDisplayLayout.of(messages);

    // 计划只出现在第一条：卡片不能挂在 A 上（那会插在两段正文中间），
    // 必须挂到整段末尾的 C，并沿用 A 的计划内容。
    expect(layout.planCards.containsKey('A'), isFalse);
    expect(layout.planCards.containsKey('B'), isFalse);
    expect(layout.planCards.containsKey('C'), isTrue);
    expect(layout.planCards['C']!.title, '拆解需求');
  });

  testWidgets('user insert stays inside the continuous reply surface', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final messages = [
      reply('此前的正文'),
      event('插一句', user: true),
      reply('接着继续回复'),
    ];
    await tester.pumpWidget(surface(messages));
    await tester.pump();

    expect(find.byKey(const ValueKey('continuous-reply-surface')), findsOneWidget);
    expect(find.text('你 · 已追加到当前任务'), findsOneWidget);
    final before = tester.getTopLeft(find.text('此前的正文', findRichText: true)).dy;
    final inserted = tester.getTopLeft(find.text('你 · 已追加到当前任务')).dy;
    final after = tester.getTopLeft(find.text('接着继续回复', findRichText: true)).dy;
    expect(before, lessThan(inserted));
    expect(inserted, lessThan(after));
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('continuous group shows one plan card, not one per message', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final plan = PlanInfo(
      id: 'plan-1',
      title: '拆解需求',
      mode: 'build',
      status: 'in_progress',
      items: const [
        PlanItemInfo(
          id: 'item-1',
          text: '读取配置',
          status: 'completed',
          priority: 'high',
        ),
      ],
    );
    final messages = [
      reply('第一条正文', plan: plan),
      reply('第二条正文'),
      reply('第三条正文'),
    ];
    await tester.pumpWidget(surface(messages));
    await tester.pump();

    // 合并成一段时任务卡片只出现一次，且挂在段末（在最后一条正文之下）。
    expect(find.textContaining('拆解需求'), findsOneWidget);
    final cardY = tester.getTopLeft(find.textContaining('拆解需求')).dy;
    final lastBodyY = tester
        .getTopLeft(find.text('第三条正文', findRichText: true))
        .dy;
    expect(cardY, greaterThan(lastBodyY));
    await tester.pumpWidget(const SizedBox.shrink());
  });
  for (final status in ['in_progress', 'completed']) {
    for (final tail in ['reply', 'insert', 'background', 'empty']) {
      testWidgets('one $status plan stays below the $tail tail', (tester) async {
        SharedPreferences.setMockInitialValues({});
        final plan = PlanInfo(
          id: 'tail-plan', title: '段尾计划', mode: 'build', status: status,
          items: const [PlanItemInfo(id: 'i', text: '检查顺序', status: 'completed', priority: 'high')],
        );
        final tailMessage = switch (tail) {
          'insert' => event('追加消息', user: true),
          'background' => event('后台结果'),
          'empty' => reply('等待回复', live: true)..content = '',
          _ => reply('最后正文', plan: plan),
        };
        final messages = [reply('第一段', plan: plan), reply('中间正文', plan: plan), tailMessage];
        await tester.pumpWidget(surface(messages));
        await tester.pump();
        expect(find.text('段尾计划'), findsOneWidget);
        final tailText = switch (tail) {
          'insert' => '你 · 已追加到当前任务',
          'background' => '后台任务已完成 · 后台结果',
          'empty' => '中间正文',
          _ => '最后正文',
        };
        expect(tester.getTopLeft(find.text('段尾计划')).dy,
          greaterThan(tester.getBottomLeft(find.text(tailText, findRichText: true)).dy));
        expect(ReplyDisplayLayout.of(messages).planCards.keys, [tailMessage.id]);
        await tester.pumpWidget(const SizedBox.shrink());
      });
    }
  }

}

