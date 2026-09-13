import 'dart:convert';
import 'dart:io';

import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/data/chat_repository.dart';
import 'package:chat_codex_app/features/chat/data/subagent_repository.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  setUp(() async {
    SharedPreferences.setMockInitialValues({'access_token': 'event-test'});
    await AppStorage.init();
  });

  test('loads all historical display events through cursor pages', () async {
    final requests = <Uri>[];
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(server.close);
    await AppStorage.setBaseUrl('http://${server.address.host}:${server.port}');

    server.listen((request) async {
      requests.add(request.uri);
      request.response.headers.contentType = ContentType.json;
      final cursor = request.uri.queryParameters['cursor'];
      final events = cursor == null
          ? List.generate(
              200,
              (index) => {
                'id': index + 1,
                'sequence': index + 1,
                'type': 'delta',
                'field': 'text',
                'content': 'a',
                'sent_at': '2026-01-01T00:00:00Z',
              },
            )
          : [
              {
                'id': 201,
                'sequence': 201,
                'type': 'delta',
                'field': 'text',
                'content': 'b',
                'sent_at': '2026-01-01T00:00:01Z',
              },
              {
                'id': 202,
                'sequence': 202,
                'type': 'completed',
                'content': 'done',
                'sent_at': '2026-01-01T00:00:02Z',
              },
            ];
      request.response.write(
        jsonEncode({
          'events': events,
          'next_cursor': cursor == null ? 'page-2' : '',
          'has_more': cursor == null,
        }),
      );
      await request.response.close();
    });

    final snapshot = await ChatRepository().getTaskEventSnapshot(
      'task-history',
    );
    expect(snapshot.text, '${List.filled(200, 'a').join()}b');
    expect(requests, hasLength(2));
    expect(requests[0].path, '/api/tasks/task-history/agent-history');
    expect(requests[0].queryParameters, {'limit': '200'});
    expect(requests[1].queryParameters, {'limit': '200', 'cursor': 'page-2'});
  });

  test('falls back to event-pages when agent-history is unavailable', () async {
    final requests = <Uri>[];
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(server.close);
    await AppStorage.setBaseUrl('http://${server.address.host}:${server.port}');
    server.listen((request) async {
      requests.add(request.uri);
      request.response.headers.contentType = ContentType.json;
      if (request.uri.path.endsWith('/agent-history')) {
        request.response.statusCode = 404;
        request.response.write(jsonEncode({'error': 'not found'}));
        await request.response.close();
        return;
      }
      request.response.write(
        jsonEncode({
          'events': [
            {
              'id': 1,
              'sequence': 1,
              'type': 'delta',
              'field': 'text',
              'content': 'legacy',
              'sent_at': '2026-01-01T00:00:00Z',
            },
          ],
          'next_cursor': '',
          'has_more': false,
        }),
      );
      await request.response.close();
    });

    final snapshot = await ChatRepository().getTaskEventSnapshot('task-legacy');
    expect(snapshot.text, 'legacy');
    expect(requests.map((uri) => uri.path).toList(), [
      '/api/tasks/task-legacy/agent-history',
      '/api/tasks/task-legacy/event-pages',
    ]);
  });

  test('keeps tools running when agent-history has no terminal event', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(server.close);
    await AppStorage.setBaseUrl('http://${server.address.host}:${server.port}');
    server.listen((request) async {
      request.response.headers.contentType = ContentType.json;
      request.response.write(
        jsonEncode({
          'events': [
            {
              'type': 'delta',
              'field': 'text',
              'content': '正文还在',
              'sent_at': '2026-01-01T00:00:00Z',
            },
            {
              'type': 'tool_updated',
              'tool': {'id': 't1', 'call_id': 'c1', 'tool': 'read', 'status': 'running'},
              'sent_at': '2026-01-01T00:00:01Z',
            },
          ],
          'next_cursor': '',
          'has_more': false,
        }),
      );
      await request.response.close();
    });

    final snapshot = await ChatRepository().getTaskEventSnapshot('task-running-tool');
    expect(snapshot.text, '正文还在');
    expect(snapshot.toolCalls, hasLength(1));
    expect(snapshot.toolCalls.single.isRunning, isTrue);
    expect(
      snapshot.blocks.where((block) => block.type == ChatMessageBlockType.text),
      isNotEmpty,
    );
  });

  test('defaults a tool with missing status to running', () {
    final tool = ToolCallInfo.fromJson({'id': 't1', 'call_id': 'c1', 'tool': 'read'});
    expect(tool.status, 'running');
    expect(tool.isRunning, isTrue);
  });

  test('finalizes running tools only after a terminal history event', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(server.close);
    await AppStorage.setBaseUrl('http://${server.address.host}:${server.port}');
    server.listen((request) async {
      request.response.headers.contentType = ContentType.json;
      request.response.write(
        jsonEncode({
          'events': [
            {
              'type': 'tool_updated',
              'tool': {'id': 't1', 'call_id': 'c1', 'tool': 'read', 'status': 'running'},
              'sent_at': '2026-01-01T00:00:01Z',
            },
            {
              'type': 'completed',
              'content': 'done',
              'sent_at': '2026-01-01T00:00:02Z',
            },
          ],
          'next_cursor': '',
          'has_more': false,
        }),
      );
      await request.response.close();
    });

    final snapshot = await ChatRepository().getTaskEventSnapshot('task-terminal-tool');
    expect(snapshot.toolCalls.single.isRunning, isFalse);
    expect(snapshot.toolCalls.single.isCompleted, isTrue);
  });

  test('keeps events that arrive after a terminal history event', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(server.close);
    await AppStorage.setBaseUrl('http://${server.address.host}:${server.port}');
    server.listen((request) async {
      request.response.headers.contentType = ContentType.json;
      request.response.write(
        jsonEncode({
          'events': [
            {
              'type': 'delta',
              'field': 'text',
              'content': 'before',
              'sent_at': '2026-01-01T00:00:00Z',
            },
            {
              'type': 'completed',
              'content': 'done',
              'sent_at': '2026-01-01T00:00:01Z',
            },
            {
              'type': 'delta',
              'field': 'text',
              'content': 'after',
              'sent_at': '2026-01-01T00:00:02Z',
            },
          ],
          'next_cursor': '',
          'has_more': false,
        }),
      );
      await request.response.close();
    });

    final snapshot = await ChatRepository().getTaskEventSnapshot(
      'task-after-terminal',
    );
    expect(snapshot.text, 'beforeafter');
    expect(
      snapshot.blocks
          .where((block) => block.type == ChatMessageBlockType.text)
          .map((block) => block.text)
          .join(),
      'beforeafter',
    );
  });

  test(
    'requests subagent log pages by cursor and stops at the page boundary',
    () async {
      final requests = <Uri>[];
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      await AppStorage.setBaseUrl(
        'http://${server.address.host}:${server.port}',
      );
      server.listen((request) async {
        requests.add(request.uri);
        request.response.headers.contentType = ContentType.json;
        final hasCursor = request.uri.queryParameters.containsKey('cursor');
        request.response.write(
          jsonEncode({
            'events': [
              {
                'type': 'subagent_result',
                'content': hasCursor ? 'second' : 'first',
                'sent_at': '2026-01-01T00:00:00Z',
              },
            ],
            'next_cursor': hasCursor ? '' : 'next-log-page',
            'has_more': !hasCursor,
          }),
        );
        await request.response.close();
      });

      final logs = await SubagentRepository().getLogs('task-1', 'node-1');
      expect(logs.items.map((event) => event.content), ['first', 'second']);
      expect(logs.hasMore, isFalse);
      expect(requests, hasLength(2));
      expect(requests[0].queryParameters, {'limit': '50'});
      expect(requests[1].queryParameters['cursor'], 'next-log-page');
      expect(requests[1].queryParameters.containsKey('offset'), isFalse);
    },
  );

  test(
    'reconnects task SSE with sequence and composite cursor headers',
    () async {
      final headers = <Map<String, String>>[];
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      await AppStorage.setBaseUrl(
        'http://${server.address.host}:${server.port}',
      );
      var requestCount = 0;
      server.listen((request) async {
        requestCount++;
        headers.add({
          'sequence': request.headers.value('Last-Event-ID') ?? '',
          'cursor': request.headers.value('X-Task-Event-Cursor') ?? '',
        });
        request.response.headers.contentType = ContentType(
          'text',
          'event-stream',
        );
        final sequence = requestCount;
        request.response.write(
          'data: ${jsonEncode({'id': sequence, 'sequence': sequence, 'type': 'completed', 'sent_at': '2026-01-01T00:00:0$sequence'
              'Z'})}\n\n',
        );
        await request.response.close();
      });

      final repository = ChatRepository();
      await repository.watchTaskEvents('task-sse').drain<void>();
      await repository.watchTaskEvents('task-sse').drain<void>();
      expect(headers, hasLength(2));
      expect(headers[0]['sequence'], isEmpty);
      expect(headers[0]['cursor'], isEmpty);
      expect(headers[1]['sequence'], '1');
      expect(headers[1]['cursor'], isNotEmpty);
    },
  );
}
