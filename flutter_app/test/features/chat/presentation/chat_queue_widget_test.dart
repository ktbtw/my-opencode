import 'package:chat_codex_app/core/theme/app_colors.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_page.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_provider.dart';
import 'package:chat_codex_app/features/settings/settings_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets(
    'restored task shows sync then live status without initial stages',
    (tester) async {
      final message = ChatMessage(
        id: 'agent-task',
        role: MessageRole.agent,
        state: MessageState.streaming,
        content: '',
        taskId: 'task',
        createdAt: DateTime.now(),
        streamRecovery: StreamRecovery.syncing,
      );
      await tester.pumpWidget(buildChatStreamingProgressTestSurface(message));
      expect(find.text('正在同步回复'), findsOneWidget);
      expect(find.text('已发送'), findsNothing);
      final updated = message.copyForUpdate()
        ..streamRecovery = StreamRecovery.following;
      await tester.pumpWidget(buildChatStreamingProgressTestSurface(updated));
      expect(find.text('正在等待回复'), findsOneWidget);
      expect(find.text('已接上正在运行的任务'), findsOneWidget);
      expect(find.text('已连接'), findsNothing);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  testWidgets('recovery flow folds into the hint line', (tester) async {
    // 恢复流程不再单独占进度条：主行只放标题，当前阶段（卡在哪一跳）
    // 放副标题，待处理阶段不逐条铺开。
    await tester.pumpWidget(
      buildChatStreamingProgressTestSurface(
        ChatMessage(
          id: 'agent-task',
          role: MessageRole.agent,
          state: MessageState.streaming,
          content: '',
          taskId: 'task',
          createdAt: DateTime.now(),
          streamRecovery: StreamRecovery.syncing,
        ),
        operationTitle: '正在恢复回复',
        operationSteps: const [
          ChatOperationStep(
            key: 'phone',
            label: '手机',
            detail: '状态请求完成',
            state: ChatOperationStepState.done,
          ),
          ChatOperationStep(
            key: 'server',
            label: '服务端',
            detail: '已接收',
            state: ChatOperationStepState.active,
          ),
          ChatOperationStep(
            key: 'opencode',
            label: 'OpenCode',
            detail: '等待 Agent 状态',
          ),
          ChatOperationStep(key: 'task', label: '回复', detail: '等待恢复'),
        ],
      ),
    );

    expect(find.text('正在恢复回复'), findsOneWidget);
    // 当前阶段保留在副标题：排障要看卡在哪一跳。
    expect(find.text('服务端 已接收'), findsOneWidget);
    // 待处理阶段不再逐条铺开，避免和这一行重复。
    expect(find.textContaining('OpenCode 等待 Agent 状态'), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('failed flow shows its reason in the hint line', (tester) async {
    await tester.pumpWidget(
      buildChatStreamingProgressTestSurface(
        ChatMessage(
          id: 'agent-task',
          role: MessageRole.agent,
          state: MessageState.streaming,
          content: '',
          taskId: 'task',
          createdAt: DateTime.now(),
        ),
        operationTitle: '正在停止任务',
        operationSteps: const [
          ChatOperationStep(
            key: 'phone',
            label: '手机',
            detail: '停止指令已发送',
            state: ChatOperationStepState.done,
          ),
          ChatOperationStep(
            key: 'server',
            label: '服务端',
            detail: '发送失败，连接已断开',
            state: ChatOperationStepState.failed,
          ),
          ChatOperationStep(key: 'opencode', label: 'OpenCode', detail: '等待接收'),
          ChatOperationStep(key: 'task', label: '任务', detail: '等待停止'),
        ],
      ),
    );

    // 主行只放标题，失败原因作为阶段细节进副标题（错误色标在主行）。
    final label = find.text('正在停止任务');
    expect(label, findsOneWidget);
    expect(tester.widget<Text>(label).style?.color, AppColors.statusError);
    expect(find.text('服务端 发送失败，连接已断开'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('finished bubble still shows the running flow', (tester) async {
    // 回归：恢复流程写在已完成、又没有历史详情的气泡上时，合并前靠顶部进度条
    // 兜底；合并后必须由轻提示接手，不能整条流程消失。
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
            body: Align(
              alignment: Alignment.topLeft,
              child: buildChatMessageBubbleTestSurface(
                ChatMessage(
                  id: 'agent-done',
                  role: MessageRole.agent,
                  state: MessageState.done,
                  content: '已完成',
                  taskId: 'task',
                  createdAt: DateTime.now(),
                  streamRecovery: StreamRecovery.syncing,
                ),
                operationTitle: '正在恢复回复',
                operationSteps: const [
                  ChatOperationStep(
                    key: 'phone',
                    label: '手机',
                    detail: '状态请求完成',
                    state: ChatOperationStepState.done,
                  ),
                  ChatOperationStep(
                    key: 'server',
                    label: '服务端',
                    detail: '已接收',
                    state: ChatOperationStepState.active,
                  ),
                  ChatOperationStep(
                    key: 'opencode',
                    label: 'OpenCode',
                    detail: '等待 Agent 状态',
                  ),
                  ChatOperationStep(key: 'task', label: '回复', detail: '等待恢复'),
                ],
              ),
            ),
          ),
        ),
      ),
    );

    expect(find.text('正在恢复回复'), findsOneWidget);
    expect(find.text('服务端 已接收'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('stop flow folds into the hint line', (tester) async {
    await tester.pumpWidget(
      buildChatStreamingProgressTestSurface(
        ChatMessage(
          id: 'agent-task',
          role: MessageRole.agent,
          state: MessageState.streaming,
          content: '',
          taskId: 'task',
          createdAt: DateTime.now(),
        ),
        operationTitle: '正在停止任务',
        operationSteps: const [
          ChatOperationStep(
            key: 'phone',
            label: '手机',
            detail: '发送停止指令',
            state: ChatOperationStepState.active,
          ),
          ChatOperationStep(key: 'server', label: '服务端', detail: '等待接收'),
          ChatOperationStep(
            key: 'opencode',
            label: 'OpenCode',
            detail: '等待 Agent 接收',
          ),
          ChatOperationStep(key: 'task', label: '任务', detail: '等待停止'),
        ],
      ),
    );

    expect(find.text('正在停止任务'), findsOneWidget);
    // 当前阶段（手机 · 发送停止指令）保留在副标题。
    expect(find.text('手机 发送停止指令'), findsOneWidget);
    expect(find.text('任务 等待停止'), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('new task uses inline status without progress cards', (
    tester,
  ) async {
    await tester.pumpWidget(
      buildChatStreamingProgressTestSurface(
        ChatMessage(
          id: 'agent-task',
          role: MessageRole.agent,
          state: MessageState.streaming,
          content: '',
          taskId: 'task',
          createdAt: DateTime.now(),
        ),
      ),
    );
    expect(find.text('正在等待回复'), findsOneWidget);
    expect(find.text('已发送'), findsNothing);
    expect(find.text('正文'), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
  });

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
    expect(tester.getCenter(send).dx, lessThan(tester.getCenter(insert).dx));
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
