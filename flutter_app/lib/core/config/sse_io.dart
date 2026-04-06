// 原生平台 SSE 实现（使用 dart:io http client 流式读取）
import 'dart:async';
import 'dart:convert';
import 'dart:io';

Stream<String> sseStream(Uri uri, Map<String, String> headers) async* {
  final client = HttpClient();
  client.connectionTimeout = const Duration(seconds: 30);
  try {
    final request = await client.getUrl(uri);
    headers.forEach((k, v) => request.headers.set(k, v));
    final response = await request.close();

    final buffer = StringBuffer();
    await for (final chunk in response.transform(utf8.decoder)) {
      buffer.write(chunk);
      // 处理 buffer 中已有的完整行
      while (true) {
        final str = buffer.toString();
        final idx = str.indexOf('\n');
        if (idx < 0) break;
        final line = str.substring(0, idx).trim();
        buffer.clear();
        buffer.write(str.substring(idx + 1));
        if (line.startsWith('data:')) {
          final payload = line.substring(5).trim();
          if (payload.isNotEmpty) yield payload;
        }
      }
    }
    // 处理最后剩余内容
    final remaining = buffer.toString().trim();
    if (remaining.startsWith('data:')) {
      final payload = remaining.substring(5).trim();
      if (payload.isNotEmpty) yield payload;
    }
  } finally {
    client.close();
  }
}
