import 'dart:async';
import 'dart:io';

import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/data/chat_repository.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_provider.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 覆盖「应用切后台再回前台」时发送队列的状态恢复。
/// 关键约束：后台断连属于预期行为，空队列不得渲染成「队列状态异常」。
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late Directory logDirectory;

  setUp(() async {
    logDirectory = await Directory.systemTemp.createTemp('chat-queue-bg-');
    final logFile = File('${logDirectory.path}/test.log');
    await logFile.writeAsString('');
    SharedPreferences.setMockInitialValues({
      'app_current_log_path': logFile.path,
      'app_log_batch_id': 'queue-bg-test',
    });
    await AppStorage.init();
  });

  tearDown(() async {
    if (await logDirectory.exists()) {
      await logDirectory.delete(recursive: true);
    }
  });

  test('后台期间的队列断连不产生用户可见错误（空队列）', () async {
    final repository = _FakeChatRepository();
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });

    await notifier.loadSession('session-1');
    await _eventually(() => repository.queueWatchCount == 1);
    await _eventually(() => !notifier.state.queueLoading);

    // 应用进入后台，长连接被系统挂起。
    notifier.markAppBackgrounded();
    repository.queueEvents.addError(StateError('socket suspended'));

    // 等待事件被处理。
    await Future<void>.delayed(const Duration(milliseconds: 30));

    // 空队列不应出现错误提示，否则会渲染成「队列状态异常」。
    expect(
      notifier.state.queueError,
      isNull,
      reason: '后台断连不应产生用户可见的队列错误',
    );
    expect(notifier.state.queue.queuedItems, isEmpty);
  });

  test('回到前台会清除后台残留的队列错误状态', () async {
    final repository = _FakeChatRepository();
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });

    await notifier.loadSession('session-1');
    await _eventually(() => repository.queueWatchCount == 1);
    await _eventually(() => !notifier.state.queueLoading);

    // 模拟后台期间已经残留了错误（例如旧版本行为或异常路径）。
    notifier.markAppBackgrounded();
    repository.queueEvents.addError(StateError('socket suspended'));
    await Future<void>.delayed(const Duration(milliseconds: 30));

    // 回到前台应重建订阅并清除残留错误。
    await notifier.reconcileAfterResume();
    await _eventually(() => notifier.state.queueError == null);

    expect(
      notifier.state.queueError,
      isNull,
      reason: '回到前台后不应残留队列错误',
    );
    expect(notifier.state.queueLoading, isFalse);
  });

  test('前台且队列非空时仍提示断连', () async {
    final repository = _FakeChatRepository();
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });

    await notifier.loadSession('session-1');
    await _eventually(() => repository.queueWatchCount == 1);
    await _eventually(() => !notifier.state.queueLoading);

    // 队列里确实有待发送消息。
    repository.queueEvents.add(
      ChatQueueSnapshot(
        sessionId: 'session-1',
        version: 1,
        items: [_queueItem()],
      ),
    );
    await _eventually(() => notifier.state.queue.queuedItems.isNotEmpty);

    // 前台断连：此时应当提示用户，因为确实有消息待发送。
    repository.queueEvents.addError(StateError('connection lost'));
    await _eventually(() => notifier.state.queueError != null);

    expect(
      notifier.state.queueError,
      isNotNull,
      reason: '前台且有待发送消息时应提示断连',
    );
  });

  test('回到前台后队列断连仍能自动重连', () async {
    final repository = _FakeChatRepository();
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });

    await notifier.loadSession('session-1');
    await _eventually(() => repository.queueWatchCount == 1);
    await _eventually(() => !notifier.state.queueLoading);

    final watchesBefore = repository.queueWatchCount;
    await notifier.reconcileAfterResume();

    // 恢复前台必须重建队列订阅，否则队列会永久停留在旧状态。
    await _eventually(() => repository.queueWatchCount > watchesBefore);
    expect(repository.queueWatchCount, greaterThan(watchesBefore));
  });
}

ChatQueueItem _queueItem() {
  return const ChatQueueItem(
    id: 'queue-1',
    sessionId: 'session-1',
    agentId: 'agent-1',
    projectId: 'project-1',
    parts: [
      {'type': 'text', 'text': '待发送内容'},
    ],
    status: ChatQueueItemStatus.queued,
    version: 1,
  );
}

Future<void> _eventually(bool Function() predicate) async {
  final deadline = DateTime.now().add(const Duration(seconds: 3));
  while (!predicate()) {
    if (DateTime.now().isAfter(deadline)) {
      fail('condition was not met before timeout');
    }
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
}

class _FakeChatRepository extends ChatRepository {
  final queueEvents = StreamController<ChatQueueSnapshot>.broadcast();
  int queueWatchCount = 0;
  List<TaskModel> history = const [];
  ChatQueueSnapshot queueSnapshot = const ChatQueueSnapshot(
    sessionId: 'session-1',
  );

  @override
  Stream<ChatQueueSnapshot> watchChatQueue(String sessionId) {
    queueWatchCount++;
    return queueEvents.stream;
  }

  @override
  Future<ChatQueueSnapshot> getChatQueue(String sessionId) async {
    return queueSnapshot;
  }

  @override
  Future<List<TaskModel>> getSessionHistory(
    String sessionId, {
    int limit = 5,
    DateTime? beforeCreatedAt,
    String? beforeTaskId,
  }) async => history;

  @override
  Future<List<Map<String, dynamic>>> getSessionHistoryJson(
    String sessionId, {
    int limit = 5,
    DateTime? beforeCreatedAt,
    String? beforeTaskId,
  }) async => const [];

  @override
  Future<TaskModel> getTask(String taskId) async {
    for (final task in history) {
      if (task.taskId == taskId) return task;
    }
    throw StateError('task not found');
  }

  @override
  Future<TaskEventSnapshot> getTaskEventSnapshot(String taskId) async {
    return const TaskEventSnapshot();
  }

  @override
  Stream<Map<String, dynamic>> watchTaskEvents(String taskId) {
    return const Stream<Map<String, dynamic>>.empty();
  }

  Future<void> dispose() async {
    await queueEvents.close();
  }
}
