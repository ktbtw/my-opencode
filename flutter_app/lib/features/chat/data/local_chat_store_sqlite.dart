import 'dart:convert';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';
import 'package:sqlite3/sqlite3.dart';
import 'local_chat_store.dart';

class _SqliteLocalChatStore implements LocalChatStore {
  Database? _database;

  Database get _db => _database!;

  @override
  Future<void> init() async {
    if (_database != null) return;
    final directory = await getApplicationSupportDirectory();
    _database = sqlite3.open(
      p.join(directory.path, 'chat_codex_messages.sqlite3'),
    );
    _db.execute('''
      CREATE TABLE IF NOT EXISTS cached_tasks (
        identity TEXT NOT NULL,
        agent_id TEXT NOT NULL,
        project_id TEXT NOT NULL,
        session_id TEXT NOT NULL,
        task_id TEXT NOT NULL,
        created_at TEXT NOT NULL,
        payload TEXT NOT NULL,
        updated_at INTEGER NOT NULL,
        PRIMARY KEY (identity, agent_id, project_id, session_id, task_id)
      )
    ''');
    _db.execute('''
      CREATE TABLE IF NOT EXISTS task_events (
        identity TEXT NOT NULL,
        task_id TEXT NOT NULL,
        event_key TEXT NOT NULL,
        payload TEXT NOT NULL,
        created_at INTEGER NOT NULL,
        PRIMARY KEY (identity, task_id, event_key)
      )
    ''');
    _db.execute('''
      CREATE TABLE IF NOT EXISTS session_sync_state (
        identity TEXT NOT NULL,
        session_id TEXT NOT NULL,
        revision TEXT NOT NULL,
        updated_at INTEGER NOT NULL,
        PRIMARY KEY (identity, session_id)
      )
    ''');
    _db.execute(
      'CREATE INDEX IF NOT EXISTS cached_tasks_session_idx '
      'ON cached_tasks(identity, agent_id, project_id, session_id, created_at DESC)',
    );
    _db.execute(
      'CREATE INDEX IF NOT EXISTS task_events_task_idx '
      'ON task_events(identity, task_id, created_at)',
    );
  }

  @override
  Future<String?> readRevision({
    required String identity,
    required String sessionId,
  }) async {
    final rows = _db.select(
      'SELECT revision FROM session_sync_state WHERE identity = ? AND session_id = ?',
      [identity, sessionId],
    );
    return rows.isEmpty ? null : rows.first['revision'] as String;
  }

  @override
  Future<void> saveRevision({
    required String identity,
    required String sessionId,
    required String revision,
  }) async {
    _db.execute(
      '''
      INSERT INTO session_sync_state(identity, session_id, revision, updated_at)
      VALUES (?, ?, ?, ?)
      ON CONFLICT(identity, session_id) DO UPDATE SET revision = excluded.revision,
        updated_at = excluded.updated_at
    ''',
      [identity, sessionId, revision, DateTime.now().millisecondsSinceEpoch],
    );
  }

  @override
  Future<List<CachedChatTask>> readTasks({
    required String identity,
    required String agentId,
    required String projectId,
    required String sessionId,
    int limit = 20,
  }) async {
    final rows = _db.select(
      '''
      SELECT task_id, created_at, payload FROM cached_tasks
      WHERE identity = ? AND agent_id = ? AND project_id = ? AND session_id = ?
      ORDER BY created_at DESC LIMIT ?
    ''',
      [identity, agentId, projectId, sessionId, limit],
    );
    return rows
        .map(
          (row) => CachedChatTask(
            taskId: row['task_id'] as String,
            createdAt: row['created_at'] as String,
            payload: Map<String, dynamic>.from(
              jsonDecode(row['payload'] as String) as Map,
            ),
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
    _db.execute('BEGIN');
    try {
      for (final task in tasks) {
        final taskId = task['task_id']?.toString() ?? '';
        final createdAt = task['created_at']?.toString() ?? '';
        if (taskId.isEmpty || createdAt.isEmpty) continue;
        _db.execute(
          '''
          INSERT INTO cached_tasks
            (identity, agent_id, project_id, session_id, task_id, created_at, payload, updated_at)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?)
          ON CONFLICT(identity, agent_id, project_id, session_id, task_id)
          DO UPDATE SET created_at = excluded.created_at,
            payload = excluded.payload, updated_at = excluded.updated_at
        ''',
          [
            identity,
            agentId,
            projectId,
            sessionId,
            taskId,
            createdAt,
            jsonEncode(task),
            DateTime.now().millisecondsSinceEpoch,
          ],
        );
      }
      _db.execute('COMMIT');
    } catch (_) {
      _db.execute('ROLLBACK');
      rethrow;
    }
  }

  @override
  Future<bool> recordEvent({
    required String identity,
    required String taskId,
    required String eventKey,
    required Map<String, dynamic> payload,
  }) async {
    final existing = _db.select(
      '''
      SELECT 1 FROM task_events
      WHERE identity = ? AND task_id = ? AND event_key = ? LIMIT 1
    ''',
      [identity, taskId, eventKey],
    );
    if (existing.isNotEmpty) return false;
    _db.execute(
      '''
      INSERT OR IGNORE INTO task_events(identity, task_id, event_key, payload, created_at)
      VALUES (?, ?, ?, ?, ?)
    ''',
      [
        identity,
        taskId,
        eventKey,
        jsonEncode(payload),
        DateTime.now().millisecondsSinceEpoch,
      ],
    );
    return true;
  }

  @override
  Future<List<Map<String, dynamic>>> readEvents({
    required String identity,
    required String taskId,
  }) async {
    final rows = _db.select(
      '''
      SELECT payload FROM task_events
      WHERE identity = ? AND task_id = ?
      ORDER BY created_at ASC, rowid ASC
    ''',
      [identity, taskId],
    );
    return rows
        .map((row) {
          final payload = jsonDecode(row['payload'] as String);
          if (payload is Map<String, dynamic>) return payload;
          if (payload is Map) return Map<String, dynamic>.from(payload);
          return <String, dynamic>{};
        })
        .where((event) => event.isNotEmpty)
        .toList(growable: false);
  }

  @override
  Future<void> close() async {
    _database?.dispose();
    _database = null;
  }
}

LocalChatStore createPlatformLocalChatStore() => _SqliteLocalChatStore();
