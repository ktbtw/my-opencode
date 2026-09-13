import 'dart:convert';
import 'dart:io';

import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/data/chat_repository.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  setUp(() async {
    SharedPreferences.setMockInitialValues({'access_token': 'queue-test'});
    await AppStorage.init();
  });

  test('parses queue metadata, file parts, versions, and statuses', () {
    final snapshot = ChatQueueSnapshot.fromJson({
      'session_id': 'session-1',
      'version': 9,
      'items': [
        {
          'queue_item_id': 'queue-1',
          'session_id': 'session-1',
          'agent_id': 'agent-1',
          'machine_id': 'machine-1',
          'project_id': 'project-1',
          'parts': [
            {'type': 'text', 'text': '继续检查'},
            {
              'type': 'file',
              'mime': 'text/plain',
              'filename': 'notes.txt',
              'url': 'data:text/plain;base64,bm90ZXM=',
            },
          ],
          'metadata': {
            'model': 'provider/model-a',
            'variant': 'high',
            'permission_mode': 'ask',
          },
          'position': 2048,
          'status': 'inserting',
          'version': 4,
          'task_id': 'task-1',
          'injection_version': 33,
          'created_at': '2026-07-28T08:00:00Z',
        },
      ],
    });

    expect(snapshot.sessionId, 'session-1');
    expect(snapshot.version, 9);
    expect(snapshot.queuedItems, hasLength(1));
    final item = snapshot.items.single;
    expect(item.text, '继续检查');
    expect(item.attachmentCount, 1);
    expect(item.modelRef, 'provider/model-a');
    expect(item.variant, 'high');
    expect(item.status, ChatQueueItemStatus.inserting);
    expect(item.version, 4);
    expect(item.injectionVersion, 33);
    expect(item.isEditable, isFalse);
  });

  test('sends queue CRUD, reorder, and insert request contracts', () async {
    final requests =
        <({String method, String path, Map<String, dynamic> body})>[];
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(server.close);
    await AppStorage.setBaseUrl('http://${server.address.host}:${server.port}');

    server.listen((request) async {
      final text = await utf8.decoder.bind(request).join();
      final body = text.isEmpty
          ? <String, dynamic>{}
          : Map<String, dynamic>.from(jsonDecode(text) as Map);
      requests.add((
        method: request.method,
        path: request.uri.path,
        body: body,
      ));
      request.response.headers.contentType = ContentType.json;
      if (request.uri.path.endsWith('/reorder')) {
        request.response.write(
          jsonEncode({
            'session_id': 'session-1',
            'version': 8,
            'items': [
              _queueItemJson('queue-2', version: 6, position: 1024),
              _queueItemJson('queue-1', version: 5, position: 2048),
            ],
          }),
        );
      } else if (request.method == 'DELETE') {
        request.response.write('{}');
      } else if (request.uri.path.endsWith('/insert')) {
        request.response.write(
          jsonEncode(
            _queueItemJson(
              'queue-1',
              version: 5,
              status: 'inserting',
              taskId: 'task-active',
            ),
          ),
        );
      } else if (request.method == 'PATCH') {
        request.response.write(
          jsonEncode(
            _queueItemJson(
              'queue-1',
              version: 5,
              metadata: {'model': 'provider/model-b', 'variant': 'low'},
            ),
          ),
        );
      } else {
        request.response.write(jsonEncode(_queueItemJson('queue-1')));
      }
      await request.response.close();
    });

    final repository = ChatRepository();
    final attachment = AttachedFile(
      id: 'file-1',
      filename: 'notes.txt',
      mimeType: 'text/plain',
      base64Data: 'bm90ZXM=',
      sizeBytes: 5,
    );
    final model = const ModelInfo(
      providerID: 'provider',
      modelID: 'model-a',
      name: 'Model A',
      variants: ['high'],
    );
    final created = await repository.createChatQueueItem(
      sessionId: 'session-1',
      agentId: 'agent-1',
      projectId: 'project-1',
      text: '排队消息',
      attachedFiles: [attachment],
      model: model,
      variant: 'high',
    );
    final updated = await repository.updateChatQueueItem(
      created,
      model: 'provider/model-b',
      variant: 'low',
    );
    await repository.deleteChatQueueItem(updated);
    await repository.reorderChatQueue('session-1', [
      ChatQueueItem.fromJson(_queueItemJson('queue-2', version: 5)),
      ChatQueueItem.fromJson(_queueItemJson('queue-1', version: 4)),
    ]);
    final inserted = await repository.insertChatQueueItem(created);

    expect(inserted.status, ChatQueueItemStatus.inserting);
    expect(requests.map((request) => request.method), [
      'POST',
      'PATCH',
      'DELETE',
      'POST',
      'POST',
    ]);
    expect(requests[0].path, '/api/sessions/session-1/queue');
    expect(
      requests[0].body['metadata'],
      containsPair('model', 'provider/model-a'),
    );
    expect(requests[0].body['metadata'], containsPair('variant', 'high'));
    expect(requests[0].body['parts'], hasLength(2));
    expect(requests[1].body, {
      'expected_version': 1,
      'model': 'provider/model-b',
      'variant': 'low',
    });
    expect(requests[2].body, {'expected_version': 5});
    expect(requests[3].path, '/api/sessions/session-1/queue/reorder');
    expect(requests[3].body['item_ids'], ['queue-2', 'queue-1']);
    expect(requests[3].body['expected_versions'], {'queue-2': 5, 'queue-1': 4});
    expect(requests[4].path, '/api/chat-queue/queue-1/insert');
    expect(requests[4].body, {'expected_version': 1});
  });

  test('splits task snapshot at each applied input', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(server.close);
    await AppStorage.setBaseUrl('http://${server.address.host}:${server.port}');

    server.listen((request) async {
      expect(request.uri.path, '/api/tasks/task-1/agent-history');
      request.response.headers.contentType = ContentType.json;
      request.response.write(
        jsonEncode({
          'events': [
            {
              'type': 'delta',
              'field': 'text',
              'content': '第一轮',
              'sent_at': '2026-07-28T08:00:00Z',
            },
            {
              'type': 'input_applied',
              'content': '立即补充',
              'metadata': {'queue_item_id': 'queue-1', 'injection_version': 7},
              'sent_at': '2026-07-28T08:00:01Z',
            },
            {
              'type': 'delta',
              'field': 'text',
              'content': '第二轮',
              'sent_at': '2026-07-28T08:00:02Z',
            },
            {
              'type': 'completed',
              'content': '第一轮第二轮',
              'sent_at': '2026-07-28T08:00:03Z',
            },
          ],
          'next_cursor': '',
          'has_more': false,
        }),
      );
      await request.response.close();
    });

    final snapshot = await ChatRepository().getTaskEventSnapshot('task-1');

    expect(snapshot.rounds, hasLength(2));
    expect(snapshot.rounds[0].text, '第一轮');
    expect(snapshot.rounds[1].text, '第二轮');
    expect(snapshot.appliedInputs, hasLength(1));
    expect(snapshot.appliedInputs.single.queueItemId, 'queue-1');
    expect(snapshot.appliedInputs.single.content, '立即补充');
    expect(snapshot.appliedInputs.single.afterRoundIndex, 0);
  });
}

Map<String, dynamic> _queueItemJson(
  String id, {
  int version = 1,
  int position = 1024,
  String status = 'queued',
  String taskId = '',
  Map<String, String> metadata = const {'model': 'provider/model-a'},
}) {
  return {
    'queue_item_id': id,
    'session_id': 'session-1',
    'agent_id': 'agent-1',
    'machine_id': 'machine-1',
    'project_id': 'project-1',
    'parts': [
      {'type': 'text', 'text': '排队消息'},
    ],
    'metadata': metadata,
    'position': position,
    'status': status,
    'version': version,
    'task_id': taskId,
  };
}
