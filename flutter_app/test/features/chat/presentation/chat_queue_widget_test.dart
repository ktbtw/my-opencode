import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('active composer keeps stop and send as independent buttons', (
    tester,
  ) async {
    var sends = 0;
    var stops = 0;
    await tester.pumpWidget(
      buildChatQueueControlsTestSurface(
        items: [_item()],
        taskActive: true,
        onSend: () => sends++,
        onStop: () => stops++,
      ),
    );

    expect(find.byKey(const ValueKey('stop')), findsOneWidget);
    expect(find.byKey(const ValueKey('send')), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('stop')));
    await tester.tap(find.byKey(const ValueKey('send')));
    expect(stops, 1);
    expect(sends, 1);
  });

  testWidgets('desktop tray renders queue item and return-arrow insertion', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1200, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      buildChatQueueControlsTestSurface(
        items: [_item()],
        taskActive: true,
        onSend: () {},
        onStop: () {},
      ),
    );

    expect(find.text('待发送 1'), findsOneWidget);
    expect(find.text('排队消息'), findsOneWidget);
    expect(find.textContaining('高'), findsOneWidget);
    expect(find.byIcon(Icons.keyboard_return_rounded), findsOneWidget);
  });

  testWidgets('queue send button is enabled only when idle', (tester) async {
    var queueSends = 0;
    await tester.pumpWidget(
      buildChatQueueControlsTestSurface(
        items: [_item()],
        taskActive: false,
        onSend: () {},
        onStop: () {},
        onQueueSend: (_) => queueSends++,
      ),
    );

    final send = find.byTooltip('直接发送');
    final insert = find.byTooltip('插入当前对话');
    expect(send, findsOneWidget);
    expect(insert, findsOneWidget);
    expect(
      tester.getCenter(send).dx,
      lessThan(tester.getCenter(insert).dx),
    );
    await tester.tap(send);
    expect(queueSends, 1);
  });

  testWidgets('queue send button is disabled while a task is active', (
    tester,
  ) async {
    var queueSends = 0;
    await tester.pumpWidget(
      buildChatQueueControlsTestSurface(
        items: [_item()],
        taskActive: true,
        onSend: () {},
        onStop: () {},
        onQueueSend: (_) => queueSends++,
      ),
    );

    await tester.tap(find.byTooltip('直接发送'));
    expect(queueSends, 0);
  });

  testWidgets('mobile tray collapses to the pending-message row', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      buildChatQueueControlsTestSurface(
        items: [_item()],
        taskActive: true,
        onSend: () {},
        onStop: () {},
      ),
    );

    expect(find.text('待发送 1'), findsOneWidget);
    expect(find.byIcon(Icons.schedule_send_outlined), findsOneWidget);
    expect(find.byIcon(Icons.keyboard_arrow_up_rounded), findsOneWidget);
    expect(find.byIcon(Icons.keyboard_return_rounded), findsNothing);
  });

  testWidgets('empty queue keeps the composer clear', (tester) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      buildChatQueueControlsTestSurface(
        items: const [],
        taskActive: false,
        onSend: () {},
        onStop: () {},
      ),
    );

    expect(find.text('待发送 0'), findsNothing);
    expect(find.text('同步中'), findsNothing);
    expect(find.text('暂无待发送消息'), findsNothing);
    expect(find.text('队列状态异常'), findsNothing);
  });

  testWidgets('empty queue stays hidden while background sync is running', (
    tester,
  ) async {
    await tester.pumpWidget(
      buildChatQueueControlsTestSurface(
        items: const [],
        taskActive: false,
        queueLoading: true,
        onSend: () {},
        onStop: () {},
      ),
    );

    expect(find.text('待发送 0'), findsNothing);
    expect(find.text('同步中'), findsNothing);
  });
}

ChatQueueItem _item() {
  return const ChatQueueItem(
    id: 'queue-1',
    sessionId: 'session-1',
    agentId: 'agent-1',
    projectId: 'project-1',
    parts: [
      {'type': 'text', 'text': '排队消息'},
    ],
    metadata: {'model': 'provider/model-a', 'variant': 'high'},
    position: 1024,
    status: ChatQueueItemStatus.queued,
    version: 1,
  );
}
