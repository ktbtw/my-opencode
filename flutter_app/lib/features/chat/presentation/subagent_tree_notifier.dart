import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/chat_repository.dart';
import 'chat_provider.dart' show chatRepositoryProvider;
import '../../../core/services/app_foreground.dart';
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
  Timer? _refreshTimer;
  bool _refreshing = false;
  bool _refreshAgain = false;

  SubagentTreeNotifier({
    required this.taskId,
    required SubagentRepository repository,
    required ChatRepository chatRepository,
  }) : _repository = repository,
       _chatRepository = chatRepository,
       super(const SubagentTreeState());

  Future<void> start() async {
    if (_disposed) return;
    _events ??= _chatRepository
        .watchTaskSignals(taskId)
        .listen(
          (event) {
            final type = event['type'] as String? ?? '';
            if (type.startsWith('subagent_')) _scheduleRefresh();
          },
          onError: (Object error) {
            if (!_disposed) state = state.copyWith(error: error.toString());
          },
        );
    // Subscribe before the snapshot so changes during GET trigger another read.
    await refresh();
  }

  void _scheduleRefresh() {
    if (_disposed) return;
    _refreshTimer ??= Timer(const Duration(milliseconds: 150), () {
      _refreshTimer = null;
      unawaited(refresh(silent: true));
    });
  }

  Future<void> refresh({bool silent = false}) async {
    if (_disposed) return;
    if (_refreshing) {
      _refreshAgain = true;
      return;
    }
    _refreshing = true;
    if (!silent) state = state.copyWith(loading: true, error: '');
    try {
      final snapshot = await _repository.getTree(taskId);
      if (!_disposed) {
        state = state.copyWith(snapshot: snapshot, loading: false, error: '');
      }
    } catch (error) {
      if (!_disposed) {
        state = state.copyWith(loading: false, error: error.toString());
      }
    } finally {
      _refreshing = false;
      if (_refreshAgain && !_disposed) {
        _refreshAgain = false;
        _scheduleRefresh();
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
    _refreshTimer?.cancel();
    super.dispose();
  }
}

final subagentRepositoryProvider = Provider((ref) => SubagentRepository());

final subagentTreeProvider = StateNotifierProvider.autoDispose
    .family<SubagentTreeNotifier, SubagentTreeState, String>((ref, taskId) {
      final notifier = SubagentTreeNotifier(
        taskId: taskId,
        repository: ref.watch(subagentRepositoryProvider),
        chatRepository: ref.watch(chatRepositoryProvider),
      );
      if (ref.watch(appForegroundProvider)) unawaited(notifier.start());
      return notifier;
    });
