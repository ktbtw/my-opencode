// Web 平台 SSE 实现（使用 dart:html XHR）
import 'dart:async';
// ignore: avoid_web_libraries_in_flutter
import 'dart:html' as html;

Stream<String> sseStream(Uri uri, Map<String, String> headers) {
  final controller = StreamController<String>();
  final xhr = html.HttpRequest();
  xhr.open('GET', uri.toString(), async: true);
  headers.forEach((k, v) => xhr.setRequestHeader(k, v));

  int processedLength = 0;

  void processNewData() {
    final text = xhr.responseText;
    if (text == null || text.length <= processedLength) return;
    final newData = text.substring(processedLength);
    processedLength = text.length;
    for (final line in newData.split('\n')) {
      final trimmed = line.trim();
      if (trimmed.startsWith('data:')) {
        final payload = trimmed.substring(5).trim();
        if (payload.isNotEmpty && !controller.isClosed) {
          controller.add(payload);
        }
      }
    }
  }

  xhr.onProgress.listen((_) {
    if (xhr.readyState >= 3) processNewData();
  });

  xhr.onReadyStateChange.listen((_) {
    if (xhr.readyState == 4) {
      processNewData();
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
