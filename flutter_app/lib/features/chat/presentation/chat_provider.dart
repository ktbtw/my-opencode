import 'dart:async';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../data/chat_model.dart';
import '../data/chat_repository.dart';

final chatRepositoryProvider = Provider((ref) => ChatRepository());

// 历史会话列表
final sessionListProvider =
    FutureProvider.autoDispose.family<List<SessionModel>, String>(
  (ref, agentId) async {
    final repo = ref.watch(chatRepositoryProvider);
    return repo.getSessions(agentId: agentId);
  },
);

// 对话控制器状态
class ChatState {
  final List<ChatMessage> messages;
  final bool sending;
  final String? currentSessionId;
  final ApprovalInfo? pendingApproval;
  final String? pendingApprovalTaskId;
  final String? error;

  const ChatState({
    this.messages = const [],
    this.sending = false,
    this.currentSessionId,
    this.pendingApproval,
    this.pendingApprovalTaskId,
    this.error,
  });

  ChatState copyWith({
    List<ChatMessage>? messages,
    bool? sending,
    String? currentSessionId,
    ApprovalInfo? pendingApproval,
    String? pendingApprovalTaskId,
    String? error,
    bool clearApproval = false,
    bool clearError = false,
  }) {
    return ChatState(
      messages: messages ?? this.messages,
      sending: sending ?? this.sending,
      currentSessionId: currentSessionId ?? this.currentSessionId,
      pendingApproval: clearApproval ? null : (pendingApproval ?? this.pendingApproval),
      pendingApprovalTaskId: clearApproval
          ? null
          : (pendingApprovalTaskId ?? this.pendingApprovalTaskId),
      error: clearError ? null : (error ?? this.error),
    );
  }
}

class ChatNotifier extends StateNotifier<ChatState> {
  final ChatRepository _repo;
  final String agentId;
  final String projectId;

  StreamSubscription<Map<String, dynamic>>? _eventSub;

  ChatNotifier({
    required ChatRepository repo,
    required this.agentId,
    required this.projectId,
  })  : _repo = repo,
        super(const ChatState());

  // 切换/加载历史会话
  Future<void> loadSession(String sessionId) async {
    _cancelEventSub();
    state = ChatState(currentSessionId: sessionId);
    // 历史消息通过 tasks 列表重建暂不实现 — 从空开始
  }

  void newSession() {
    _cancelEventSub();
    state = const ChatState();
  }

  Future<void> sendMessage(String text, {List<Map<String, dynamic>>? extraParts}) async {
    if (text.trim().isEmpty || state.sending) return;

    // 加入用户气泡
    final userMsg = ChatMessage(
      id: 'user_${DateTime.now().millisecondsSinceEpoch}',
      role: MessageRole.user,
      state: MessageState.done,
      content: text,
      createdAt: DateTime.now(),
    );

    // 加入 agent 流式气泡（占位）
    final agentMsg = ChatMessage(
      id: 'agent_${DateTime.now().millisecondsSinceEpoch}',
      role: MessageRole.agent,
      state: MessageState.streaming,
      content: '',
      createdAt: DateTime.now(),
    );

    state = state.copyWith(
      messages: [...state.messages, userMsg, agentMsg],
      sending: true,
      clearError: true,
    );

    try {
      final task = await _repo.createTask(
        agentId: agentId,
        projectId: projectId,
        text: text,
        sessionId: state.currentSessionId,
        extraParts: extraParts,
      );

      agentMsg.taskId == null;
      // 更新 agentMsg 的 taskId（通过替换整个列表）
      final newAgentMsg = ChatMessage(
        id: agentMsg.id,
        role: MessageRole.agent,
        state: MessageState.streaming,
        content: '',
        createdAt: agentMsg.createdAt,
        taskId: task.taskId,
      );

      final msgs = [...state.messages];
      final idx = msgs.indexWhere((m) => m.id == agentMsg.id);
      if (idx >= 0) msgs[idx] = newAgentMsg;

      state = state.copyWith(
        messages: msgs,
        currentSessionId: task.sessionId ?? state.currentSessionId,
      );

      _listenEvents(task.taskId, newAgentMsg.id);
    } catch (e) {
      _updateMessage(agentMsg.id, (m) {
        m.state = MessageState.failed;
        m.content = '发送失败: $e';
      });
      state = state.copyWith(sending: false, error: e.toString());
    }
  }

  void _listenEvents(String taskId, String agentMsgId) {
    _cancelEventSub();
    _eventSub = _repo.watchTaskEvents(taskId).listen(
      (event) {
        final type = event['type'] as String?;
        final payload = event['payload'] as Map<String, dynamic>? ?? {};

        switch (type) {
          case 'started':
            final sessionId = payload['session_id'] as String?;
            if (sessionId != null && sessionId.isNotEmpty) {
              state = state.copyWith(currentSessionId: sessionId);
            }

          case 'delta':
            final content = payload['content'] as String? ?? '';
            _updateMessage(agentMsgId, (m) {
              m.content += content;
              m.state = MessageState.streaming;
            });

          case 'waiting_approval':
            final approval = ApprovalInfo.fromJson(payload);
            _updateMessage(agentMsgId, (m) {
              m.state = MessageState.streaming;
            });
            state = state.copyWith(
              pendingApproval: approval,
              pendingApprovalTaskId: taskId,
              sending: false,
            );

          case 'completed':
            final result = payload['result'] as String?;
            _updateMessage(agentMsgId, (m) {
              if (m.content.isEmpty && result != null) {
                m.content = result;
              }
              m.state = MessageState.done;
            });
            state = state.copyWith(
              sending: false,
              clearApproval: true,
            );

          case 'failed':
            final error = payload['error'] as String? ?? '任务执行失败';
            _updateMessage(agentMsgId, (m) {
              m.state = MessageState.failed;
              m.content = error;
            });
            state = state.copyWith(sending: false, clearApproval: true);
        }
      },
      onError: (e) {
        _updateMessage(agentMsgId, (m) {
          m.state = MessageState.failed;
          if (m.content.isEmpty) m.content = '连接中断';
        });
        state = state.copyWith(sending: false);
      },
      onDone: () {
        _updateMessage(agentMsgId, (m) {
          if (m.state == MessageState.streaming) {
            m.state = MessageState.done;
          }
        });
        state = state.copyWith(sending: false);
      },
    );
  }

  void _updateMessage(String id, void Function(ChatMessage m) updater) {
    final msgs = List<ChatMessage>.from(state.messages);
    final idx = msgs.indexWhere((m) => m.id == id);
    if (idx < 0) return;
    updater(msgs[idx]);
    state = state.copyWith(messages: msgs);
  }

  Future<void> submitApproval(String reply) async {
    final taskId = state.pendingApprovalTaskId;
    if (taskId == null) return;
    try {
      await _repo.submitApproval(taskId, reply);
      state = state.copyWith(clearApproval: true, sending: true);
    } catch (e) {
      state = state.copyWith(error: '审批提交失败: $e');
    }
  }

  void _cancelEventSub() {
    _eventSub?.cancel();
    _eventSub = null;
  }

  @override
  void dispose() {
    _cancelEventSub();
    super.dispose();
  }
}

// 使用 family 按 agentId 区分不同对话页实例
final chatProvider = StateNotifierProvider.autoDispose
    .family<ChatNotifier, ChatState, (String agentId, String projectId)>(
  (ref, args) => ChatNotifier(
    repo: ref.watch(chatRepositoryProvider),
    agentId: args.$1,
    projectId: args.$2,
  ),
);
