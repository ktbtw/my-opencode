// 原生平台 SSE 实现（使用 dart:io http client 流式读取）
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'sse_parser.dart';

Stream<String> sseStream(Uri uri, Map<String, String> headers) async* {
  await for (final event in sseEventStream(uri, headers)) {
    yield event.data;
  }
}

/// 保留 `event:` 类型的事件流。需要区分事件类型的调用方使用这个。
Stream<SseEvent> sseEventStream(Uri uri, Map<String, String> headers) async* {
  final client = HttpClient();
  client.connectionTimeout = const Duration(seconds: 30);
  try {
    final request = await client.getUrl(uri);
    headers.forEach((k, v) => request.headers.set(k, v));
    final response = await request.close();
    if (response.statusCode < 200 || response.statusCode >= 300) {
      await response.drain<void>();
      throw SseHttpException(response.statusCode);
    }

    final parser = SseDataParser();
    await for (final chunk in response.transform(utf8.decoder)) {
      for (final event in parser.addChunkEvents(chunk)) {
        yield event;
      }
    }
    for (final event in parser.closeEvents()) {
      yield event;
    }
  } finally {
    client.close();
  }
}
