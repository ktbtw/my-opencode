// 原生平台 SSE 实现（使用 dart:io http client 流式读取）
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'sse_log.dart';
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
  final channel = SseChannel.fromPath(uri.path);
  final client = HttpClient()..connectionTimeout = const Duration(seconds: 30);
  final parser = SseDataParser();
  StreamSubscription<String>? subscription;
  var closed = false;
  var sawFirstData = false;
  var sawFirstHeartbeat = false;
  var bytesReceived = 0;
  var eventsReceived = 0;
  var connectStartedAt = DateTime.now();
  var connectedAt = DateTime.now();
  late final StreamController<SseEvent> controller;

  void finish([Object? error, StackTrace? stack]) {
    if (closed) return;
    closed = true;
    client.close(force: true);
    if (error != null) {
      SseLog.error(channel, error);
    } else {
      SseLog.closed(
        channel,
        reason: '服务端关闭流',
        durationMs: DateTime.now().difference(connectedAt).inMilliseconds,
        bytes: bytesReceived,
        events: eventsReceived,
        heartbeats: parser.heartbeatCount,
      );
    }
    if (error != null) controller.addError(error, stack);
    unawaited(controller.close());
    unawaited(subscription?.cancel());
  }

  Future<void> connect() async {
    connectStartedAt = DateTime.now();
    SseLog.connecting(channel, uri.toString());
    try {
      final request = await client.getUrl(uri);
      if (closed) return;
      headers.forEach((key, value) => request.headers.set(key, value));
      final response = await request.close().timeout(
        const Duration(seconds: 30),
      );
      if (closed) return;
      SseLog.responded(channel, response.statusCode);
      if (response.statusCode < 200 || response.statusCode >= 300) {
        finish(SseHttpException(response.statusCode));
        return;
      }
      connectedAt = DateTime.now();
      SseLog.connected(
        channel,
        connectedAt.difference(connectStartedAt).inMilliseconds,
      );
      onConnected?.call();
      // Time out on missing bytes, not missing business events. SSE comments
      // are heartbeats and keep a healthy but quiet connection alive.
      subscription = response
          .timeout(idleTimeout)
          .transform(utf8.decoder)
          .listen(
            (chunk) {
              if (closed) return;
              bytesReceived += chunk.length;
              if (!sawFirstData) {
                sawFirstData = true;
                SseLog.firstData(channel, chunk.length);
              }
              onActivity?.call();
              final events = parser.addChunkEvents(chunk);
              if (!sawFirstHeartbeat && parser.heartbeatCount > 0) {
                sawFirstHeartbeat = true;
                SseLog.firstHeartbeat(channel);
              }
              eventsReceived += events.length;
              for (final event in events) {
                controller.add(event);
              }
            },
            onError: (Object error, StackTrace stack) {
              if (error is TimeoutException) {
                SseLog.idleTimeout(channel, idleTimeout.inSeconds);
              }
              finish(error, stack);
            },
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
    onPause: () {
      SseLog.step(channel, '数据流暂停（下游背压）');
      subscription?.pause();
    },
    onResume: () {
      SseLog.step(channel, '数据流恢复');
      subscription?.resume();
    },
    onCancel: () async {
      // An async* reader waits for another event before reaching finally.
      // Close the socket directly so background resume never waits on it.
      if (!closed) SseLog.cancelled(channel);
      closed = true;
      client.close(force: true);
      await subscription?.cancel();
    },
  );
  return controller.stream;
}
