// 原生平台 SSE 实现（使用 dart:io http client 流式读取）
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'sse_parser.dart';

Stream<String> sseStream(Uri uri, Map<String, String> headers) =>
    sseEventStream(uri, headers).map((event) => event.data);

/// 保留 `event:` 类型的事件流。需要区分事件类型的调用方使用这个。
Stream<SseEvent> sseEventStream(
  Uri uri,
  Map<String, String> headers, {
  Duration idleTimeout = const Duration(seconds: 60),
  void Function()? onConnected,
  void Function()? onActivity,
}) {
  final client = HttpClient()..connectionTimeout = const Duration(seconds: 30);
  final parser = SseDataParser();
  StreamSubscription<String>? subscription;
  var closed = false;
  late final StreamController<SseEvent> controller;

  void finish([Object? error, StackTrace? stack]) {
    if (closed) return;
    closed = true;
    client.close(force: true);
    if (error != null) controller.addError(error, stack);
    unawaited(controller.close());
    unawaited(subscription?.cancel());
  }

  Future<void> connect() async {
    try {
      final request = await client.getUrl(uri);
      if (closed) return;
      headers.forEach((key, value) => request.headers.set(key, value));
      final response = await request.close().timeout(
        const Duration(seconds: 30),
      );
      if (closed) return;
      if (response.statusCode < 200 || response.statusCode >= 300) {
        finish(SseHttpException(response.statusCode));
        return;
      }
      onConnected?.call();
      // Time out on missing bytes, not missing business events. SSE comments
      // are heartbeats and keep a healthy but quiet connection alive.
      subscription = response
          .timeout(idleTimeout)
          .transform(utf8.decoder)
          .listen(
            (chunk) {
              if (closed) return;
              onActivity?.call();
              for (final event in parser.addChunkEvents(chunk)) {
                controller.add(event);
              }
            },
            onError: (Object error, StackTrace stack) => finish(error, stack),
            onDone: () {
              if (closed) return;
              for (final event in parser.closeEvents()) {
                controller.add(event);
              }
              finish();
            },
          );
      if (controller.isPaused) subscription?.pause();
    } catch (error, stack) {
      finish(error, stack);
    }
  }

  controller = StreamController<SseEvent>(
    onListen: () => unawaited(connect()),
    onPause: () => subscription?.pause(),
    onResume: () => subscription?.resume(),
    onCancel: () async {
      // An async* reader waits for another event before reaching finally.
      // Close the socket directly so background resume never waits on it.
      closed = true;
      client.close(force: true);
      await subscription?.cancel();
    },
  );
  return controller.stream;
}
