import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/widgets/agent_activity_indicator.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

ChatMessage message({
  String content = '',
  StreamRecovery recovery = StreamRecovery.none,
  MessageState state = MessageState.streaming,
}) => ChatMessage(
  id: 'reply',
  role: MessageRole.agent,
  state: state,
  content: content,
  taskId: 'task',
  createdAt: DateTime(2026),
  streamRecovery: recovery,
);

Widget surface(
  ChatMessage reply, {
  bool reduced = false,
  bool visible = true,
  double width = 320,
  double scale = 1,
}) => MaterialApp(
  home: Scaffold(
    body: MediaQuery(
      data: MediaQueryData(
        disableAnimations: reduced,
        textScaler: TextScaler.linear(scale),
      ),
      child: TickerMode(
        enabled: visible,
        child: SizedBox(
          width: width,
          child: AgentActivityIndicator(message: reply),
        ),
      ),
    ),
  ),
);

void main() {
  testWidgets(
    'connected active reply distinguishes history loading and retry',
    (tester) async {
      var retries = 0;
      final reply = message(recovery: StreamRecovery.following)
        ..historyDetails = HistoryDetailsState.loading;
      await tester.pumpWidget(surface(reply, reduced: true));
      expect(find.text('正在载入历史'), findsOneWidget);
      // 主行收敛成一条，细节（在拉什么）放副标题。
      expect(find.text('恢复已有回复'), findsOneWidget);
      expect(find.text('正在等待回复'), findsNothing);
      reply.historyDetails = HistoryDetailsState.partial;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentActivityIndicator(
              message: reply,
              onRetryHistory: () => retries++,
            ),
          ),
        ),
      );
      await tester.pump(const Duration(milliseconds: 250));
      expect(find.text('历史尚未载入完整'), findsOneWidget);
      expect(find.text('回复正文尚未载入'), findsOneWidget);
      await tester.tap(find.text('重试'));
      expect(retries, 1);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'partial history glyph, label and retry share a center and text baseline',
    (tester) async {
      for (final scale in [1.0, 1.5, 2.0]) {
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: MediaQuery(
                data: MediaQueryData(textScaler: TextScaler.linear(scale)),
                child: SizedBox(
                  width: scale == 1 ? 360 : 240,
                  child: AgentActivityIndicator(
                    message: message(state: MessageState.done)
                      ..historyDetails = HistoryDetailsState.partial,
                    onRetryHistory: () {},
                  ),
                ),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        final glyph = tester.getCenter(
          find.byKey(const ValueKey('agent-activity-glyph')),
        );
        expect(find.text('工具调用记录尚未完整'), findsOneWidget);
        final label = tester.getCenter(find.text('历史尚未载入完整'));
        final retry = tester.getCenter(find.text('重试'));
        expect(glyph.dy, closeTo(label.dy, .1));
        expect(retry.dy, closeTo(label.dy, .1));
        expect(tester.takeException(), isNull);
      }
    },
  );

  testWidgets('deferred tools are a manual action without loading animation', (
    tester,
  ) async {
    var loads = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentActivityIndicator(
            message: message(state: MessageState.done, content: '旧正文')
              ..historyDetails = HistoryDetailsState.deferred,
            onRetryHistory: () => loads++,
          ),
        ),
      ),
    );
    expect(find.text('正在载入历史'), findsNothing);
    expect(find.byKey(const ValueKey('agent-activity-glyph')), findsNothing);
    await tester.pumpAndSettle();
    expect(tester.binding.hasScheduledFrame, isFalse);
    await tester.tap(find.text('查看工具调用'));
    expect(loads, 1);
  });

  testWidgets(
    'completed reply keeps a small detail recovery hint, partial offers retry',
    (tester) async {
      final reply = message(state: MessageState.done, content: '完整正文')
        ..historyDetails = HistoryDetailsState.loading;
      await tester.pumpWidget(surface(reply, reduced: true));
      expect(find.text('正在载入历史'), findsOneWidget);
      expect(find.text('工具调用记录'), findsOneWidget);
      expect(find.text('正在等待回复'), findsNothing);
      var retries = 0;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentActivityIndicator(
              message: reply.copyForUpdate()
                ..historyDetails = HistoryDetailsState.partial,
              onRetryHistory: () => retries++,
            ),
          ),
        ),
      );
      expect(find.text('历史尚未载入完整'), findsOneWidget);
      expect(find.text('工具调用记录尚未完整'), findsOneWidget);
      await tester.tap(find.text('重试'));
      expect(retries, 1);
      await tester.pumpWidget(
        surface(
          reply.copyForUpdate()..historyDetails = HistoryDetailsState.none,
        ),
      );
      expect(find.byKey(const ValueKey('agent-activity-glyph')), findsNothing);
    },
  );

  testWidgets(
    'network recovery shows retrying rather than endless history syncing',
    (tester) async {
      await tester.pumpWidget(
        surface(
          message(content: '已有正文', recovery: StreamRecovery.reconnecting),
          reduced: true,
        ),
      );
      expect(find.text('正在恢复连接'), findsOneWidget);
      expect(find.text('正在同步回复'), findsNothing);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  testWidgets(
    'stopping replaces waiting and recovery in the single status line',
    (tester) async {
      for (final recovery in StreamRecovery.values) {
        final reply = message(recovery: recovery)..statusHint = '正在停止';
        await tester.pumpWidget(surface(reply, reduced: true));
        expect(find.text('正在停止'), findsOneWidget);
        expect(find.text('正在等待回复'), findsNothing);
        expect(find.text('正在同步回复'), findsNothing);
        expect(find.text('稍等片刻，接上刚才的进度'), findsNothing);
      }
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  testWidgets('partial reply keeps recovery status until live output resumes', (
    tester,
  ) async {
    final reply = message(content: '已有的回复', recovery: StreamRecovery.syncing);
    await tester.pumpWidget(surface(reply));
    expect(find.text('正在同步回复'), findsOneWidget);
    expect(find.text('稍等片刻，接上刚才的进度'), findsNothing);
    await tester.pumpWidget(
      surface(reply.copyForUpdate()..streamRecovery = StreamRecovery.following),
    );
    await tester.pump(const Duration(milliseconds: 250));
    expect(find.text('正在同步回复'), findsNothing);
    expect(find.text('正在生成回复'), findsOneWidget);
    for (final state in [MessageState.done, MessageState.failed]) {
      await tester.pumpWidget(surface(message(state: state)));
      expect(find.byKey(const ValueKey('agent-activity-glyph')), findsNothing);
      await tester.pump();
      expect(tester.binding.hasScheduledFrame, isFalse);
    }
  });

  testWidgets('explicit message hint replaces the activity label in one line', (
    tester,
  ) async {
    final reply = message(content: '已有正文');
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentActivityIndicator(message: reply, statusHint: '正在等待新的回复'),
        ),
      ),
    );
    expect(find.text('正在等待新的回复'), findsOneWidget);
    expect(find.text('正在生成回复'), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('status fits narrow widths and large accessibility text', (
    tester,
  ) async {
    final reply = message(recovery: StreamRecovery.syncing)..tokenCount = 12000;
    await tester.pumpWidget(surface(reply, width: 180, scale: 2));
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(
      surface(
        reply.copyForUpdate()..streamRecovery = StreamRecovery.following,
        width: 180,
        scale: 2,
      ),
    );
    await tester.pump(const Duration(milliseconds: 250));
    expect(
      find.text('12.0k tokens'),
      findsNothing,
    ); // Statistics obey the footer settings.
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('reduced motion and hidden routes stop repeating frames', (
    tester,
  ) async {
    final reply = message();
    await tester.pumpWidget(surface(reply));
    await tester.pump(const Duration(milliseconds: 50));
    expect(tester.binding.hasScheduledFrame, isTrue);
    await tester.pumpWidget(surface(reply, reduced: true));
    await tester.pump();
    expect(tester.binding.hasScheduledFrame, isFalse);
    expect(find.text('正在等待回复'), findsOneWidget);
    await tester.pumpWidget(surface(reply, visible: false));
    await tester.pump();
    expect(tester.binding.hasScheduledFrame, isFalse);
    await tester.pumpWidget(surface(reply));
    await tester.pump();
    expect(tester.binding.hasScheduledFrame, isTrue);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('background pauses motion and resume restarts it', (
    tester,
  ) async {
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await tester.pumpWidget(surface(message()));
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    await tester.pump();
    expect(tester.binding.hasScheduledFrame, isFalse);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await tester.pump();
    expect(tester.binding.hasScheduledFrame, isTrue);
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
