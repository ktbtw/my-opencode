import 'dart:async';
import 'dart:io';

import 'package:chat_codex_app/core/config/sse_io.dart';
import 'package:chat_codex_app/core/config/sse_parser.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  late HttpServer server;
  setUp(() async {
    server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
  });
  tearDown(() async => server.close(force: true));

  Uri uri() => Uri.parse('http://127.0.0.1:${server.port}/events');

  test('cancellation closes a socket with only heartbeats promptly', () async {
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
    final subscription = sseStream(uri(), {}).listen((_) {});
    await connected.future;
    await subscription.cancel().timeout(const Duration(seconds: 1));
  });

  test(
    'silent broken stream times out even without a business frame',
    () async {
      server.listen((request) async {
        request.response.bufferOutput = false;
        request.response.headers.contentType = ContentType(
          'text',
          'event-stream',
        );
        request.response.write(': ping\n\n');
        await request.response.flush();
      });
      await expectLater(
        sseEventStream(
          uri(),
          {},
          idleTimeout: const Duration(milliseconds: 100),
        ),
        emitsInOrder([emitsError(isA<TimeoutException>()), emitsDone]),
      );
    },
  );

  test('heartbeats keep a quiet stream alive and data still parses', () async {
    server.listen((request) async {
      request.response.bufferOutput = false;
      request.response.headers.contentType = ContentType(
        'text',
        'event-stream',
      );
      for (var i = 0; i < 15; i++) {
        request.response.write(': ping\n\n');
        await request.response.flush();
        await Future<void>.delayed(const Duration(milliseconds: 100));
      }
      request.response.write('event: delta\ndata: hello\n\n');
      await request.response.close();
    });
    final events = await sseEventStream(
      uri(),
      {},
      idleTimeout: const Duration(seconds: 1),
    ).toList();
    expect(events.single.data, 'hello');
  });

  test(
    'transport callbacks observe comment heartbeats without fake data events',
    () async {
      final activity = Completer<void>();
      var connections = 0;
      final frames = <SseEvent>[];
      server.listen((request) async {
        request.response.bufferOutput = false;
        request.response.write(': heartbeat\n\n');
        await request.response.flush();
      });
      final subscription = sseEventStream(
        uri(),
        {},
        onConnected: () => connections++,
        onActivity: () {
          if (!activity.isCompleted) activity.complete();
        },
      ).listen(frames.add);
      await activity.future.timeout(const Duration(seconds: 2));
      expect(connections, 1);
      expect(frames, isEmpty);
      await subscription.cancel();
    },
  );

  test('HTTP errors propagate without waiting for the error body', () async {
    server.listen((request) async {
      request.response.statusCode = HttpStatus.unauthorized;
      request.response.write('expired');
      await request.response.flush();
    });
    await expectLater(
      sseEventStream(uri(), {}),
      emitsInOrder([emitsError(isA<SseHttpException>()), emitsDone]),
    );
  });
}
