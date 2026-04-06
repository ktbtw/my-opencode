import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../data/chat_model.dart';
import '../data/chat_repository.dart';

final chatRepositoryProvider = Provider((ref) => ChatRepository());

// 全局模型列表（启动时加载一次，不随 chat 实例销毁）
final availableModelsProvider = FutureProvider<List<ModelInfo>>((ref) async {
  final repo = ref.watch(chatRepositoryProvider);
  return repo.getModels();
});

// 全局模型选择（持久化到 localStorage，刷新后恢复）
class SelectedModelNotifier extends StateNotifier<ModelInfo?> {
  static const _key = 'selected_model_key';

  SelectedModelNotifier() : super(null);

  Future<void> init(List<ModelInfo> models) async {
    if (models.isEmpty) return;
    final prefs = await SharedPreferences.getInstance();
    final saved = prefs.getString(_key);
    if (saved != null && saved.isNotEmpty) {
      final parts = saved.split('/');
      if (parts.length == 2) {
        final match = models.where(
          (m) => m.providerID == parts[0] && m.modelID == parts[1],
        ).firstOrNull;
        if (match != null) state = match;
      }
    }
  }

  Future<void> select(ModelInfo? model) async {
    state = model;
    final prefs = await SharedPreferences.getInstance();
    if (model == null) {
      await prefs.remove(_key);
    } else {
      await prefs.setString(_key, '${model.providerID}/${model.modelID}');
    }
  }
}

final selectedModelProvider =
    StateNotifierProvider<SelectedModelNotifier, ModelInfo?>(
  (ref) => SelectedModelNotifier(),
);

// 全局审批模式（持久化）
// 可选值: 'ask' | 'auto-approve' | 'deny'
class PermissionModeNotifier extends StateNotifier<String> {
  static const _key = 'permission_mode';

  PermissionModeNotifier() : super('ask') {
    _init();
  }

  Future<void> _init() async {
    final prefs = await SharedPreferences.getInstance();
    final saved = prefs.getString(_key);
    if (saved == 'ask' || saved == 'auto-approve' || saved == 'deny') {
      state = saved!;
    }
  }

  Future<void> set(String mode) async {
    if (mode != 'ask' && mode != 'auto-approve' && mode != 'deny') return;
    state = mode;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_key, mode);
  }
}

final permissionModeProvider =
    StateNotifierProvider<PermissionModeNotifier, String>(
  (ref) => PermissionModeNotifier(),
);

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
  final List<AttachedFile> attachedFiles;

  const ChatState({
    this.messages = const [],
    this.sending = false,
    this.currentSessionId,
    this.pendingApproval,
    this.pendingApprovalTaskId,
    this.error,
    this.attachedFiles = const [],
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
    List<AttachedFile>? attachedFiles,
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
      attachedFiles: attachedFiles ?? this.attachedFiles,
    );
  }
}

class ChatNotifier extends StateNotifier<ChatState> {
  final ChatRepository _repo;
  final String agentId;
  final String projectId;

  StreamSubscription<Map<String, dynamic>>? _eventSub;
  String? _currentTaskId; // 当前正在进行的 task id，用于取消

  ChatNotifier({
    required ChatRepository repo,
    required this.agentId,
    required this.projectId,
  })  : _repo = repo,
        super(const ChatState());

  void addAttachedFile(AttachedFile file) {
    state = state.copyWith(attachedFiles: [...state.attachedFiles, file]);
  }

  void removeAttachedFile(int index) {
    final files = List<AttachedFile>.from(state.attachedFiles);
    files.removeAt(index);
    state = state.copyWith(attachedFiles: files);
  }

  void clearAttachedFiles() {
    state = state.copyWith(attachedFiles: const []);
  }

  // 切换/加载历史会话
  Future<void> loadSession(String sessionId) async {
    _cancelEventSub();
    state = ChatState(
      currentSessionId: sessionId,
      sending: true,
    );
    try {
      final tasks = await _repo.getSessionHistory(sessionId);
      // tasks 按时间降序（最新在前），需要反转为正序
      final msgs = <ChatMessage>[];
      for (final task in tasks.reversed) {
        // 用户消息
        if (task.userText != null && task.userText!.isNotEmpty) {
          msgs.add(ChatMessage(
            id: 'user_${task.taskId}',
            role: MessageRole.user,
            state: MessageState.done,
            content: task.userText!,
            createdAt: task.createdAt ?? DateTime.now(),
            taskId: task.taskId,
          ));
        }
        // AI 回复
        String agentContent = task.result ?? '';
        // result 为空时（任务中途退出），从 events 拼接 delta
        if (agentContent.isEmpty && task.status != TaskStatus.failed) {
          agentContent = await _repo.getTaskResultFromEvents(task.taskId);
        }
        msgs.add(ChatMessage(
          id: 'agent_${task.taskId}',
          role: MessageRole.agent,
          state: task.status == TaskStatus.failed ? MessageState.failed : MessageState.done,
          content: agentContent,
          createdAt: task.createdAt ?? DateTime.now(),
          taskId: task.taskId,
        ));
      }
      state = state.copyWith(messages: msgs, sending: false);
    } catch (e) {
      state = state.copyWith(sending: false, error: '加载历史失败: $e');
    }
  }

  void newSession() {
    _cancelEventSub();
    state = const ChatState();
  }

  Future<void> sendMessage(String text, {ModelInfo? model, String permissionMode = 'ask'}) async {
    debugPrint('[Chat] sendMessage called: "$text", sending=${state.sending}');
    if (text.trim().isEmpty || state.sending) return;

    final filesToSend = List<AttachedFile>.from(state.attachedFiles);
    final modelToSend = model;

    // 加入用户气泡
    final userMsg = ChatMessage(
      id: 'user_${DateTime.now().millisecondsSinceEpoch}',
      role: MessageRole.user,
      state: MessageState.done,
      content: text,
      createdAt: DateTime.now(),
      attachedFiles: filesToSend,
    );

    // 加入 agent 流式气泡（占位），记录发送时刻用于首字计时
    final sentAt = DateTime.now();
    final agentMsg = ChatMessage(
      id: 'agent_${DateTime.now().millisecondsSinceEpoch}',
      role: MessageRole.agent,
      state: MessageState.streaming,
      content: '',
      createdAt: sentAt,
    );

    state = state.copyWith(
      messages: [...state.messages, userMsg, agentMsg],
      sending: true,
      clearError: true,
      attachedFiles: const [],
    );

    try {
      debugPrint('[Chat] createTask agentId=$agentId projectId=$projectId sessionId=${state.currentSessionId}');
      final task = await _repo.createTask(
        agentId: agentId,
        projectId: projectId,
        text: text,
        sessionId: state.currentSessionId,
        attachedFiles: filesToSend.isNotEmpty ? filesToSend : null,
        model: modelToSend,
        permissionMode: permissionMode,
      );

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

      _currentTaskId = task.taskId;
      _listenEvents(task.taskId, newAgentMsg.id, sentAt);
    } catch (e, st) {
      debugPrint('[Chat] sendMessage error: $e\n$st');
      _updateMessage(agentMsg.id, (m) {
        m.state = MessageState.failed;
        m.content = '发送失败: $e';
      });
      state = state.copyWith(sending: false, error: e.toString());
    }
  }

  void _listenEvents(String taskId, String agentMsgId, DateTime sentAt) {
    debugPrint('[Chat] _listenEvents taskId=$taskId agentMsgId=$agentMsgId');
    _cancelEventSub();
    bool _firstDelta = true;
    _eventSub = _repo.watchTaskEvents(taskId).listen(
      (event) {
        final type = event['type'] as String?;
        debugPrint('[Chat] SSE event type=$type');

        switch (type) {
          case 'started':
            final sessionId = event['session_id'] as String?;
            if (sessionId != null && sessionId.isNotEmpty) {
              state = state.copyWith(currentSessionId: sessionId);
            }

          case 'delta':
            final content = event['content'] as String? ?? '';
            final field = event['field'] as String? ?? 'text';
            _updateMessage(agentMsgId, (m) {
              // 记录首字耗时（仅第一个 text delta）
              if (_firstDelta && field == 'text' && content.isNotEmpty) {
                m.firstTokenTime = DateTime.now().difference(sentAt);
                _firstDelta = false;
              }
              if (field == 'reasoning') {
                m.thinkingContent += content;
              } else {
                m.content += content;
              }
              m.state = MessageState.streaming;
            });

          case 'waiting_approval':
            final approval = ApprovalInfo.fromJson(event);
            _updateMessage(agentMsgId, (m) {
              m.state = MessageState.streaming;
            });
            state = state.copyWith(
              pendingApproval: approval,
              pendingApprovalTaskId: taskId,
              sending: false,
            );

          case 'completed':
            final completedContent = event['content'] as String? ?? '';
            final usage = event['usage'] as Map<String, dynamic>?;
            final totalTokens = usage != null
                ? ((usage['input_tokens'] as int? ?? 0) + (usage['output_tokens'] as int? ?? 0))
                : null;
            _updateMessage(agentMsgId, (m) {
              if (m.content.isEmpty && completedContent.isNotEmpty) {
                m.content = completedContent;
              }
              m.state = MessageState.done;
              if (totalTokens != null && totalTokens > 0) m.tokenCount = totalTokens;
            });
            if (completedContent.isEmpty) {
              _fetchTaskResult(taskId, agentMsgId);
            }
            state = state.copyWith(sending: false, clearApproval: true);

          case 'failed':
            final error = event['error'] as String? ?? '任务执行失败';
            _updateMessage(agentMsgId, (m) {
              m.state = MessageState.failed;
              m.content = error;
            });
            state = state.copyWith(sending: false, clearApproval: true);
        }
      },
      onError: (e) {
        debugPrint('[Chat] SSE error: $e');
        _updateMessage(agentMsgId, (m) {
          m.state = MessageState.failed;
          if (m.content.isEmpty) m.content = '连接中断';
        });
        state = state.copyWith(sending: false);
      },
      onDone: () {
        debugPrint('[Chat] SSE stream done');
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

  Future<void> _fetchTaskResult(String taskId, String agentMsgId) async {
    try {
      final task = await _repo.getTask(taskId);
      if (task.result != null && task.result!.isNotEmpty) {
        _updateMessage(agentMsgId, (m) {
          if (m.content.isEmpty) m.content = task.result!;
        });
      }
    } catch (_) {}
  }

  Future<void> cancelCurrentTask() async {
    // 优先用记录的 taskId，兜底从最后一条 streaming 消息找
    String? taskId = _currentTaskId;
    if (taskId == null) {
      final msgs = state.messages;
      for (int i = msgs.length - 1; i >= 0; i--) {
        if (msgs[i].role == MessageRole.agent &&
            msgs[i].state == MessageState.streaming &&
            msgs[i].taskId != null) {
          taskId = msgs[i].taskId;
          break;
        }
      }
    }
    if (taskId == null) return;
    try {
      await _repo.cancelTask(taskId);
    } catch (_) {}
    _cancelEventSub();
    _currentTaskId = null;
    // 最后一条 agent 消息标记为已完成（取消）
    final msgs = List<ChatMessage>.from(state.messages);
    for (int i = msgs.length - 1; i >= 0; i--) {
      if (msgs[i].role == MessageRole.agent && msgs[i].state == MessageState.streaming) {
        if (msgs[i].content.isEmpty) msgs[i].content = '已停止';
        msgs[i].state = MessageState.done;
        break;
      }
    }
    state = state.copyWith(messages: msgs, sending: false, clearApproval: true);
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
    _currentTaskId = null;
  }

  @override
  void dispose() {
    _cancelEventSub();
    super.dispose();
  }
}

// 使用 family 按 agentId 区分不同对话页实例（不使用 autoDispose，保持对话状态跨页面切换）
final chatProvider = StateNotifierProvider
    .family<ChatNotifier, ChatState, (String agentId, String projectId)>(
  (ref, args) => ChatNotifier(
    repo: ref.watch(chatRepositoryProvider),
    agentId: args.$1,
    projectId: args.$2,
  ),
);
