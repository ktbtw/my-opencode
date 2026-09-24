import 'dart:async';
import 'package:chat_codex_app/features/chat/data/chat_repository.dart';
import 'package:flutter_test/flutter_test.dart';

class _SuspendedHistoryRepository extends ChatRepository {
  final release = Completer<void>();
  final cancelled = Completer<void>();
  @override
  Stream<Map<String, dynamic>> watchTaskSnapshotEvents(String taskId) async* {
    try {
      yield {'type': 'delta', 'content': 'already received'};
      await release
          .future; // Models the async* page waiting for suspended HTTP.
      yield {'type': 'delta', 'content': 'late data'};
    } finally {
      cancelled.complete();
    }
  }
}

class _BusyHistoryRepository extends ChatRepository {
  @override
  Stream<Map<String, dynamic>> watchTaskSnapshotEvents(String taskId) =>
      Stream.periodic(
        const Duration(milliseconds: 2),
        (_) => <String, dynamic>{'type': 'delta', 'content': 'x'},
      );
}

void main() {
  test(
    'concurrent snapshot consumers share one read and later requests refresh',
    () async {
      final repo = _SharedHistoryRepository();
      final first = repo.getTaskEventSnapshot('task');
      final second = repo.getTaskEventSnapshot('task');
      expect(identical(first, second), isTrue);
      repo.release.complete();
      expect((await first).text, 'task');
      expect((await second).text, 'task');
      expect(repo.reads, 1);
      await repo.getTaskEventSnapshot('task');
      expect(repo.reads, 2);
      repo.dispose();
    },
  );

  test(
    'snapshot deadline returns even while async generator cancellation is stuck',
    () async {
      final repo = _SuspendedHistoryRepository()
        ..taskSnapshotDeadline = const Duration(milliseconds: 30);
      addTearDown(() {
        if (!repo.release.isCompleted) repo.release.complete();
        repo.dispose();
      });
      final result = await repo
          .getTaskEventSnapshot('task')
          .timeout(const Duration(milliseconds: 300));
      expect(result.text, 'already received');
      expect(result.complete, isFalse);
      expect(repo.cancelled.isCompleted, isFalse);
      repo.release.complete();
      await repo.cancelled.future.timeout(const Duration(milliseconds: 300));
      expect(result.text, 'already received');
    },
  );

  test(
    'continuous history traffic cannot extend the total snapshot deadline',
    () async {
      final repo = _BusyHistoryRepository()
        ..taskSnapshotDeadline = const Duration(milliseconds: 40);
      addTearDown(repo.dispose);
      final result = await repo
          .getTaskEventSnapshot('task')
          .timeout(const Duration(milliseconds: 300));
      expect(result.text, isNotEmpty);
    },
  );
}

class _SharedHistoryRepository extends ChatRepository {
  int reads = 0;
  final release = Completer<void>();
  @override
  Stream<Map<String, dynamic>> watchTaskSnapshotEvents(String taskId) async* {
    reads++;
    await release.future;
    yield {'type': 'delta', 'content': taskId};
  }
}
