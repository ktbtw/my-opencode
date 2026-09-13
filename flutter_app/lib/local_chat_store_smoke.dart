import 'dart:io';

import 'package:flutter/widgets.dart';

import 'features/chat/data/local_chat_store.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final store = createLocalChatStore();
  const identity = 'windows-smoke|admin';
  const agentId = 'agent_windows_smoke';
  const projectId = 'project_windows_smoke';
  const sessionId = 'session_windows_smoke';
  const taskId = 'task_windows_smoke';
  const eventKey = 'completed|windows-smoke';

  try {
    await store.init();
    await store.saveTasks(
      identity: identity,
      agentId: agentId,
      projectId: projectId,
      sessionId: sessionId,
      tasks: const [
        {
          'task_id': taskId,
          'agent_id': agentId,
          'project_id': projectId,
          'session_id': sessionId,
          'status': 'completed',
          'result': 'windows sqlite round trip',
          'created_at': '2026-08-31T12:00:00.000Z',
          'parts': [
            {'type': 'text', 'text': 'windows smoke'},
          ],
        },
      ],
    );

    final cached = await store.readTasks(
      identity: identity,
      agentId: agentId,
      projectId: projectId,
      sessionId: sessionId,
      limit: 5,
    );
    if (cached.length != 1 ||
        cached.single.taskId != taskId ||
        cached.single.payload['result'] != 'windows sqlite round trip') {
      throw StateError('cached task round trip failed');
    }

    final otherIdentity = await store.readTasks(
      identity: 'windows-smoke|other-account',
      agentId: agentId,
      projectId: projectId,
      sessionId: sessionId,
      limit: 5,
    );
    if (otherIdentity.isNotEmpty) {
      throw StateError('identity isolation failed');
    }

    final firstEvent = await store.recordEvent(
      identity: identity,
      taskId: taskId,
      eventKey: eventKey,
      payload: const {'type': 'completed', 'content': 'done'},
    );
    final duplicateEvent = await store.recordEvent(
      identity: identity,
      taskId: taskId,
      eventKey: eventKey,
      payload: const {'type': 'completed', 'content': 'done'},
    );
    if (!firstEvent || duplicateEvent) {
      throw StateError('event deduplication failed');
    }

    stdout.writeln('LOCAL_CHAT_STORE_WINDOWS_SMOKE_OK');
    await store.close();
    exitCode = 0;
  } catch (error, stackTrace) {
    stderr.writeln('LOCAL_CHAT_STORE_WINDOWS_SMOKE_FAILED: $error');
    stderr.writeln(stackTrace);
    await store.close();
    exitCode = 1;
  }
}
