// 原生平台 SSE 实现（使用 dart:io http client 流式读取）
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'sse_parser.dart';

Stream<String> sseStream(Uri uri, Map<String, String> headers) async* {
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
      for (final payload in parser.addChunk(chunk)) {
        yield payload;
      }
    }
    for (final payload in parser.close()) {
      yield payload;
    }
  } finally {
    client.close();
  }
}
