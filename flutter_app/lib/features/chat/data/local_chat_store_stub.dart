import 'local_chat_store.dart';

class _MemoryLocalChatStore implements LocalChatStore {
  final Map<String, Map<String, dynamic>> _tasks = {};
  final List<Map<String, dynamic>> _events = [];
  final Map<String, String> _revisions = {};

  @override
  Future<void> init() async {}

  @override
  Future<String?> readRevision({
    required String identity,
    required String sessionId,
  }) async => _revisions['$identity|$sessionId'];

  @override
  Future<void> saveRevision({
    required String identity,
    required String sessionId,
    required String revision,
  }) async {
    _revisions['$identity|$sessionId'] = revision;
  }

  String _taskKey(
    String identity,
    String agentId,
    String projectId,
    String sessionId,
    String taskId,
  ) => '$identity|$agentId|$projectId|$sessionId|$taskId';

  @override
  Future<List<CachedChatTask>> readTasks({
    required String identity,
    required String agentId,
    required String projectId,
    required String sessionId,
    int limit = 20,
  }) async {
    final rows =
        _tasks.entries
            .where(
              (entry) => entry.key.startsWith(
                '$identity|$agentId|$projectId|$sessionId|',
              ),
            )
            .map((entry) => entry.value)
            .toList()
          ..sort(
            (a, b) => (b['created_at'] as String).compareTo(
              a['created_at'] as String,
            ),
          );
    return rows
        .take(limit)
        .map(
          (row) => CachedChatTask(
            taskId: row['task_id'] as String,
            createdAt: row['created_at'] as String,
            payload: Map<String, dynamic>.from(row['payload'] as Map),
          ),
        )
        .toList(growable: false);
  }

  @override
  Future<void> saveTasks({
    required String identity,
    required String agentId,
    required String projectId,
    required String sessionId,
    required List<Map<String, dynamic>> tasks,
  }) async {
    for (final task in tasks) {
      final taskId = task['task_id']?.toString() ?? '';
      final createdAt = task['created_at']?.toString() ?? '';
      if (taskId.isEmpty || createdAt.isEmpty) continue;
      _tasks[_taskKey(identity, agentId, projectId, sessionId, taskId)] = {
        'task_id': taskId,
        'created_at': createdAt,
        'payload': Map<String, dynamic>.from(task),
      };
    }
  }

  @override
  Future<bool> recordEvent({
    required String identity,
    required String taskId,
    required String eventKey,
    required Map<String, dynamic> payload,
  }) async {
    final key = '$identity|$taskId|$eventKey';
    if (_events.any((event) => event['key'] == key)) return false;
    _events.add({
      'key': key,
      'identity': identity,
      'task_id': taskId,
      'payload': Map<String, dynamic>.from(payload),
    });
    return true;
  }

  @override
  Future<List<Map<String, dynamic>>> readEvents({
    required String identity,
    required String taskId,
  }) async {
    return _events
        .where(
          (event) =>
              event['identity'] == identity && event['task_id'] == taskId,
        )
        .map((event) => Map<String, dynamic>.from(event['payload'] as Map))
        .toList(growable: false);
  }

  @override
  Future<void> close() async {}
}

LocalChatStore createPlatformLocalChatStore() => _MemoryLocalChatStore();
