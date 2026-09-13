import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/chat_repository.dart';
import '../data/subagent_models.dart';
import '../data/subagent_repository.dart';

class SubagentTreeState {
  final SubagentTreeSnapshot snapshot;
  final bool loading;
  final String error;

  const SubagentTreeState({
    this.snapshot = const SubagentTreeSnapshot(taskId: ''),
    this.loading = false,
    this.error = '',
  });

  SubagentTreeState copyWith({
    SubagentTreeSnapshot? snapshot,
    bool? loading,
    String? error,
  }) => SubagentTreeState(
    snapshot: snapshot ?? this.snapshot,
    loading: loading ?? this.loading,
    error: error ?? this.error,
  );
}

class SubagentTreeNotifier extends StateNotifier<SubagentTreeState> {
  final String taskId;
  final SubagentRepository _repository;
  final ChatRepository _chatRepository;
  StreamSubscription<Map<String, dynamic>>? _events;
  bool _disposed = false;

  SubagentTreeNotifier({
    required this.taskId,
    required SubagentRepository repository,
    required ChatRepository chatRepository,
  }) : _repository = repository,
       _chatRepository = chatRepository,
       super(const SubagentTreeState());

  Future<void> start() async {
    await refresh();
    _events ??= _chatRepository.watchTaskEvents(taskId).listen((event) {
      final type = event['type'] as String? ?? '';
      if (type.startsWith('subagent_')) unawaited(refresh(silent: true));
    });
  }

  Future<void> refresh({bool silent = false}) async {
    if (!silent) state = state.copyWith(loading: true, error: '');
    try {
      final snapshot = await _repository.getTree(taskId);
      if (!_disposed) {
        state = state.copyWith(snapshot: snapshot, loading: false);
      }
    } catch (error) {
      if (!_disposed) {
        state = state.copyWith(loading: false, error: error.toString());
      }
    }
  }

  Future<void> control(
    SubagentNode node,
    String action, {
    String instruction = '',
    int? priority,
    Map<String, dynamic>? model,
  }) async {
    await _repository.control(
      taskId: taskId,
      nodeId: node.nodeId,
      action: action,
      instruction: instruction,
      priority: priority,
      model: model,
    );
    await refresh(silent: true);
  }

  @override
  void dispose() {
    _disposed = true;
    _events?.cancel();
    super.dispose();
  }
}

final subagentRepositoryProvider = Provider((ref) => SubagentRepository());

final subagentTreeProvider = StateNotifierProvider.autoDispose
    .family<SubagentTreeNotifier, SubagentTreeState, String>((ref, taskId) {
      final notifier = SubagentTreeNotifier(
        taskId: taskId,
        repository: ref.watch(subagentRepositoryProvider),
        chatRepository: ChatRepository(),
      );
      unawaited(notifier.start());
      return notifier;
    });
