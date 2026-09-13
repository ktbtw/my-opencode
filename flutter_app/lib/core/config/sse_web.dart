// Web 平台 SSE 实现（使用 dart:html XHR）
import 'dart:async';
// ignore: avoid_web_libraries_in_flutter
import 'dart:html' as html;
import 'sse_parser.dart';

Stream<String> sseStream(Uri uri, Map<String, String> headers) {
  final controller = StreamController<String>();
  final xhr = html.HttpRequest();
  xhr.open('GET', uri.toString(), async: true);
  headers.forEach((k, v) => xhr.setRequestHeader(k, v));

  int processedLength = 0;
  final parser = SseDataParser();

  void emitPayloads(List<String> payloads) {
    for (final payload in payloads) {
      if (!controller.isClosed) controller.add(payload);
    }
  }

  void processNewData({bool flush = false}) {
    final text = xhr.responseText;
    if (text != null && text.length > processedLength) {
      final newData = text.substring(processedLength);
      processedLength = text.length;
      emitPayloads(parser.addChunk(newData));
    }
    if (flush) emitPayloads(parser.close());
  }

  xhr.onProgress.listen((_) {
    if (xhr.readyState >= 3) processNewData();
  });

  xhr.onReadyStateChange.listen((_) {
    if (xhr.readyState == 4) {
      final status = xhr.status ?? 0;
      if (status < 200 || status >= 300) {
        if (!controller.isClosed) {
          controller.addError(SseHttpException(status));
          controller.close();
        }
        return;
      }
      processNewData(flush: true);
      if (!controller.isClosed) controller.close();
    }
  });

  xhr.onError.listen((_) {
    if (!controller.isClosed) {
      controller.addError(Exception('SSE connection error'));
      controller.close();
    }
  });

  xhr.send();
  controller.onCancel = () => xhr.abort();
  return controller.stream;
}
