import 'local_chat_store_stub.dart'
    if (dart.library.io) 'local_chat_store_sqlite.dart';

class CachedChatTask {
  final String taskId;
  final String createdAt;
  final Map<String, dynamic> payload;

  const CachedChatTask({
    required this.taskId,
    required this.createdAt,
    required this.payload,
  });
}

abstract class LocalChatStore {
  Future<void> init();

  Future<String?> readRevision({
    required String identity,
    required String sessionId,
  });

  Future<void> saveRevision({
    required String identity,
    required String sessionId,
    required String revision,
  });

  Future<List<CachedChatTask>> readTasks({
    required String identity,
    required String agentId,
    required String projectId,
    required String sessionId,
    int limit = 20,
  });

  Future<void> saveTasks({
    required String identity,
    required String agentId,
    required String projectId,
    required String sessionId,
    required List<Map<String, dynamic>> tasks,
  });

  Future<bool> recordEvent({
    required String identity,
    required String taskId,
    required String eventKey,
    required Map<String, dynamic> payload,
  });

  Future<List<Map<String, dynamic>>> readEvents({
    required String identity,
    required String taskId,
  });

  Future<void> close();
}

LocalChatStore createLocalChatStore() => createPlatformLocalChatStore();
