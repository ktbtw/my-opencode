import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:chat_codex_app/core/config/api_client.dart';
import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/chat/data/chat_repository.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late HttpServer server;
  HttpOverrides? previous;
  setUp(() async {
    previous = HttpOverrides.current;
    HttpOverrides.global = null;
    SharedPreferences.setMockInitialValues({});
    await AppStorage.init();
    server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    await AppStorage.setBaseUrl('http://127.0.0.1:${server.port}');
  });
  tearDown(() async {
    await server.close(force: true);
    HttpOverrides.global = previous;
  });

  test(
    'suspended status request has a deadline and a subsequent request succeeds',
    () async {
      final requested = Completer<void>();
      server.listen((request) async {
        if (request.uri.path == '/suspended') {
          requested.complete();
          return;
        }
        request.response.headers.contentType = ContentType.json;
        request.response.write(jsonEncode({'status': 'completed'}));
        await request.response.close();
      });
      final request = ApiClient.get(
        '/suspended',
        timeout: const Duration(milliseconds: 40),
      );
      final assertion = expectLater(request, throwsA(isA<TimeoutException>()));
      await requested.future;
      await assertion.timeout(const Duration(milliseconds: 400));
      final recovered = await ApiClient.get(
        '/healthy',
        timeout: const Duration(seconds: 1),
      );
      expect(recovered['status'], 'completed');
    },
  );

  for (final kind in ['api', 'task', 'queue']) {
    test('$kind subscription cancels before first business frame', () async {
      final connected = Completer<void>();
      server.listen((request) async {
        request.response.bufferOutput = false;
        request.response.headers.contentType = ContentType(
          'text',
          'event-stream',
        );
        request.response.write(': ping\n\n');
        await request.response.flush();
        connected.complete();
      });
      final repo = ChatRepository();
      final Stream<dynamic> stream = switch (kind) {
        'api' => ApiClient.sse('/events'),
        'task' => repo.watchTaskEvents('task-1'),
        _ => repo.watchChatQueue('session-1'),
      };
      final subscription = stream.listen((_) {});
      await connected.future;
      await subscription.cancel().timeout(const Duration(seconds: 1));
    });
  }

  test('late conversation catches history on the shared task socket', () async {
    final connected = Completer<HttpResponse>();
    final snapshotRequested = Completer<void>();
    final finishSnapshot = Completer<void>();
    var opens = 0;
    Map<String, dynamic> delta(int id) => {
      'type': 'delta',
      'content': '$id',
      'sequence': id,
      'sent_at': '2026-09-19T00:00:0${id}Z',
    };
    server.listen((request) async {
      if (request.uri.path.endsWith('/event-pages')) {
        snapshotRequested.complete();
        await finishSnapshot.future;
        request.response.headers.contentType = ContentType.json;
        request.response.write(
          jsonEncode({
            'events': [delta(1)],
            'has_more': false,
          }),
        );
        await request.response.close();
      } else {
        opens++;
        request.response.bufferOutput = false;
        request.response.headers.contentType = ContentType(
          'text',
          'event-stream',
        );
        request.response.write('data: ${jsonEncode(delta(1))}\n\n');
        await request.response.flush();
        connected.complete(request.response);
      }
    });
    final repo = ChatRepository();
    addTearDown(repo.dispose);
    final signals = <Map<String, dynamic>>[];
    final panel = repo
        .watchTaskSignals('task-1')
        .where((event) => event['type'] != 'connection_ready')
        .listen(signals.add);
    final response = await connected.future;
    await Future<void>.delayed(const Duration(milliseconds: 20));
    final output = <Map<String, dynamic>>[];
    final conversation = repo.watchTaskEvents('task-1').listen(output.add);
    await snapshotRequested.future;
    response.write('data: ${jsonEncode(delta(2))}\n\n');
    await response.flush();
    await Future<void>.delayed(const Duration(milliseconds: 20));
    finishSnapshot.complete();
    for (var i = 0; output.length < 2 && i < 100; i++) {
      await Future<void>.delayed(const Duration(milliseconds: 10));
    }
    expect(opens, 1);
    expect(output.map((event) => event['sequence']), [1, 2]);
    await conversation.cancel();
    response.write('data: ${jsonEncode(delta(3))}\n\n');
    await response.flush();
    for (var i = 0; signals.length < 3 && i < 100; i++) {
      await Future<void>.delayed(const Duration(milliseconds: 10));
    }
    expect(signals.map((event) => event['sequence']), [1, 2, 3]);
    await panel.cancel().timeout(const Duration(seconds: 1));
  });

  test('task reconnect sends cursor and receives terminal event', () async {
    var connections = 0;
    String? cursor;
    server.listen((request) async {
      if (!request.uri.path.endsWith('/events')) {
        request.response.headers.contentType = ContentType.json;
        request.response.write(
          jsonEncode({'task_id': 'task-1', 'status': 'running'}),
        );
      } else {
        connections++;
        request.response.headers.contentType = ContentType(
          'text',
          'event-stream',
        );
        if (connections == 1) {
          request.response.write(
            'data: {"type":"delta","content":"hello","sequence":7,"sent_at":"2026-09-18T12:00:00Z"}\n\n',
          );
        } else {
          cursor = request.headers.value('Last-Event-ID');
          request.response.write(
            'data: {"type":"completed","content":"hello","sequence":8}\n\n',
          );
        }
      }
      await request.response.close();
    });
    final events = await ChatRepository()
        .watchTaskEvents('task-1')
        .toList()
        .timeout(const Duration(seconds: 5));
    expect(connections, 2);
    expect(cursor, '7');
    expect(events.map((e) => e['type']), [
      'connection_ready',
      'delta',
      'connection_ready',
      'completed',
    ]);
  });

  test('API refreshes expired authentication once before SSE retry', () async {
    await AppStorage.setUsername('fixture');
    await AppStorage.setPassword('fixture');
    await AppStorage.setToken('old');
    final tokens = <String?>[];
    var logins = 0;
    server.listen((request) async {
      await request.drain<void>();
      if (request.uri.path == '/api/auth/login') {
        logins++;
        request.response.headers.contentType = ContentType.json;
        request.response.write('{"access_token":"new"}');
      } else {
        tokens.add(request.headers.value('Authorization'));
        if (tokens.length == 1) {
          request.response.statusCode = HttpStatus.unauthorized;
        } else {
          request.response.headers.contentType = ContentType(
            'text',
            'event-stream',
          );
          request.response.write('data: resumed\n\n');
        }
      }
      await request.response.close();
    });
    expect(await ApiClient.sse('/events').toList(), ['resumed']);
    expect(tokens, ['Bearer old', 'Bearer new']);
    expect(logins, 1);
  });
}
