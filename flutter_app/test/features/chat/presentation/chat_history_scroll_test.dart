import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_page.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_provider.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'uses the final path segment for the mobile project directory label',
    () {
      expect(
        projectDirectoryLabel(
          '/Users/yuminghao/Downloads/chat-codex/',
          'ignored',
        ),
        'chat-codex',
      );
      expect(
        projectDirectoryLabel(r'C:\Users\17150\workspace\verify', 'ignored'),
        'verify',
      );
      expect(projectDirectoryLabel('', 'chat-codex'), 'chat-codex');
    },
  );

  test('recognizes messages added only before the existing history', () {
    final previous = [_message('3'), _message('4'), _message('5')];
    final next = [_message('1'), _message('2'), ...previous];

    expect(isHistoryOnlyMessagePrepend(previous, next), isTrue);
  });

  test('does not classify a new conversation message as history prepend', () {
    final previous = [_message('1'), _message('2')];
    final next = [...previous, _message('3')];

    expect(isHistoryOnlyMessagePrepend(previous, next), isFalse);
  });

  test('does not classify a mixed or reordered update as history prepend', () {
    final previous = [_message('2'), _message('3')];
    final next = [_message('1'), _message('3'), _message('2')];

    expect(isHistoryOnlyMessagePrepend(previous, next), isFalse);
  });

  test('renders messages in chronological order while preserving ties', () {
    final older = _messageAt('older', DateTime.utc(2026, 8, 29, 10));
    final newer = _messageAt('newer', DateTime.utc(2026, 8, 29, 11));
    final sameTime = _messageAt('same-time', older.createdAt);

    expect(
      chronologicalChatMessages([
        newer,
        sameTime,
        older,
      ]).map((message) => message.id),
      ['same-time', 'older', 'newer'],
    );
  });

  test('keeps the user message before its agent placeholder', () {
    final sentAt = DateTime.utc(2026, 8, 29, 12);
    final user = ChatMessage(
      id: 'user-1',
      role: MessageRole.user,
      state: MessageState.done,
      content: 'hello',
      createdAt: sentAt,
    );
    final agent = ChatMessage(
      id: 'agent-1',
      role: MessageRole.agent,
      state: MessageState.streaming,
      content: '',
      createdAt: sentAt,
    );

    expect(
      chronologicalChatMessages([user, agent]).map((message) => message.id),
      ['user-1', 'agent-1'],
    );
  });

  test('keeps expanded rounds inside their task timeline', () {
    final oldTaskStartedAt = DateTime.utc(2026, 8, 29, 10);
    final newTaskStartedAt = DateTime.utc(2026, 8, 29, 11);
    final oldTaskLateRound = ChatMessage(
      id: 'agent_old_round_2',
      role: MessageRole.agent,
      state: MessageState.done,
      content: '我按最小回退范围处理',
      // This event arrived after the newer task was created.
      createdAt: DateTime.utc(2026, 8, 29, 12),
      taskId: 'task-old',
      taskCreatedAt: oldTaskStartedAt,
      taskMessageIndex: 2,
    );
    final newerTask = ChatMessage(
      id: 'agent_new',
      role: MessageRole.agent,
      state: MessageState.streaming,
      content: '我继续定位js发布链路',
      createdAt: newTaskStartedAt,
      taskId: 'task-new',
      taskCreatedAt: newTaskStartedAt,
      taskMessageIndex: 1,
    );

    expect(
      chronologicalChatMessages([
        newerTask,
        oldTaskLateRound,
      ]).map((message) => message.id),
      ['agent_old_round_2', 'agent_new'],
    );
  });

  test('auto-scroll follows the latest normalized message', () {
    final disconnect = _messageAt('disconnect', DateTime.utc(2026, 8, 29, 10));
    final replyBefore = _messageAt('reply', DateTime.utc(2026, 8, 29, 11));
    final replyAfter = _messageAt(
      'reply',
      DateTime.utc(2026, 8, 29, 11),
      content: 'new reply',
    );

    expect(
      shouldAutoScrollChatUpdate(
        ChatState(messages: [disconnect, replyBefore]),
        ChatState(messages: [disconnect, replyAfter]),
        restoringHistoryViewport: false,
        followingLatest: true,
        nearBottom: true,
      ),
      isTrue,
    );
  });

  test('initial viewport stays pinned while loaded messages expand', () {
    expect(
      shouldPinInitialChatViewport(
        positioningInitialViewport: true,
        messages: [_message('latest')],
      ),
      isTrue,
    );
    expect(
      shouldPinInitialChatViewport(
        positioningInitialViewport: false,
        messages: [_message('latest')],
      ),
      isFalse,
    );
    expect(
      shouldPinInitialChatViewport(
        positioningInitialViewport: true,
        messages: const [],
      ),
      isFalse,
    );
  });

  test('finds the latest task id from subagent metadata', () {
    final message = _message('agent')
      ..toolCalls = [
        ToolCallInfo(
          id: 'subagent-node-1',
          callId: 'subagent-node-1',
          tool: 'subagent',
          title: 'Explore repository',
          status: 'running',
          metadata: const {
            'source': 'subagent',
            'task_id': 'task-from-subagent',
            'node_id': 'node-1',
            'phase': 'running',
          },
        ),
      ];

    expect(latestSubagentTaskId([message]), 'task-from-subagent');
  });

  test('falls back to the parent message task id', () {
    final message = ChatMessage(
      id: 'agent',
      role: MessageRole.agent,
      state: MessageState.done,
      content: 'done',
      createdAt: DateTime.utc(2026, 8, 16),
      taskId: 'task-from-message',
    );

    expect(latestSubagentTaskId([message]), 'task-from-message');
  });

  test('prefers an older subagent task over a newer ordinary task', () {
    final subagentMessage = _message('older-subagent')
      ..toolCalls = [
        ToolCallInfo(
          id: 'subagent-node-1',
          callId: 'subagent-node-1',
          tool: 'subagent',
          title: 'Explore repository',
          status: 'completed',
          metadata: const {
            'source': 'subagent',
            'task_id': 'task-with-subagents',
            'node_id': 'node-1',
            'phase': 'completed',
          },
        ),
      ];
    final ordinaryMessage = ChatMessage(
      id: 'newer-ordinary',
      role: MessageRole.agent,
      state: MessageState.done,
      content: 'done',
      createdAt: DateTime.utc(2026, 8, 16),
      taskId: 'task-without-subagents',
    );

    expect(
      latestSubagentTaskId([subagentMessage, ordinaryMessage]),
      'task-with-subagents',
    );
  });

  test(
    'suppresses auto scroll while earlier history snapshots expand messages',
    () {
      final previous = ChatState(
        messages: [_message('3'), _message('4')],
        loadingEarlierHistory: true,
      );
      final next = ChatState(
        messages: [
          _message('1'),
          _message('2'),
          _message('3'),
          _message('3-round-2'),
          _message('4'),
        ],
        loadingEarlierHistory: true,
      );

      expect(
        isHistoryOnlyMessagePrepend(previous.messages, next.messages),
        isFalse,
      );
      expect(
        shouldSuppressChatAutoScrollForHistory(
          previous,
          next,
          restoringHistoryViewport: false,
        ),
        isTrue,
      );
    },
  );

  test('does not follow appended messages while the user reads history', () {
    final previous = ChatState(messages: [_message('1'), _message('2')]);
    final next = ChatState(
      messages: [_message('1'), _message('2'), _message('3')],
    );

    expect(
      shouldAutoScrollChatUpdate(
        previous,
        next,
        restoringHistoryViewport: false,
        followingLatest: false,
        nearBottom: false,
      ),
      isFalse,
    );
  });

  test('follows appended messages after the user returns to the bottom', () {
    final previous = ChatState(messages: [_message('1'), _message('2')]);
    final next = ChatState(
      messages: [_message('1'), _message('2'), _message('3')],
    );

    expect(
      shouldAutoScrollChatUpdate(
        previous,
        next,
        restoringHistoryViewport: false,
        followingLatest: true,
        nearBottom: true,
      ),
      isTrue,
    );
  });

  test('keeps the viewport locked during history restoration', () {
    final previous = ChatState(messages: [_message('3'), _message('4')]);
    final next = ChatState(
      messages: [_message('1'), _message('2'), _message('3'), _message('4')],
    );

    expect(
      shouldAutoScrollChatUpdate(
        previous,
        next,
        restoringHistoryViewport: true,
        followingLatest: true,
        nearBottom: true,
      ),
      isFalse,
    );
  });

  test(
    'preserves the reading anchor across delayed history height changes',
    () {
      var offset = preservedHistoryViewportOffset(
        currentOffset: 80,
        previousMaxScrollExtent: 5000,
        currentMaxScrollExtent: 7000,
        minScrollExtent: 0,
        maxScrollExtent: 7000,
      );
      expect(offset, 2080);

      offset = preservedHistoryViewportOffset(
        currentOffset: offset,
        previousMaxScrollExtent: 7000,
        currentMaxScrollExtent: 7420,
        minScrollExtent: 0,
        maxScrollExtent: 7420,
      );
      expect(offset, 2500);
      expect(offset, lessThan(7420));
    },
  );

  test('does not treat a viewport resize as prepended content', () {
    expect(
      preservedHistoryViewportOffset(
        currentOffset: 24,
        previousMaxScrollExtent: 800,
        currentMaxScrollExtent: 920,
        minScrollExtent: 0,
        maxScrollExtent: 1200,
        previousViewportDimension: 600,
        currentViewportDimension: 480,
      ),
      24,
    );
  });
}

ChatMessage _message(String id) {
  return _messageAt(id, DateTime.utc(2026, 7, 30));
}

ChatMessage _messageAt(String id, DateTime createdAt, {String? content}) {
  return ChatMessage(
    id: id,
    role: MessageRole.agent,
    state: MessageState.done,
    content: content ?? id,
    createdAt: createdAt,
  );
}
