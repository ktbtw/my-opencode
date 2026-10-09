class SseHttpException implements Exception {
  final int statusCode;
  const SseHttpException(this.statusCode);
}

/// 一条 SSE 事件：类型与数据。
///
/// [type] 可能为空——服务端不一定发送 `event:` 行。调用方需按空类型处理。
class SseEvent {
  final String type;
  final String data;

  const SseEvent({required this.type, required this.data});
}

class SseDataParser {
  final _buffer = StringBuffer();
  // 当前事件的 event: 值，遇到空行（事件结束）后重置。
  String _pendingEventType = '';

  /// 已解析的心跳（SSE 注释行）数量，仅用于连接诊断日志。
  int heartbeatCount = 0;

  List<String> addChunk(String chunk) {
    if (chunk.isEmpty) return const [];
    _buffer.write(chunk);
    return _drainCompleteLines()
        .map((event) => event.data)
        .toList(growable: false);
  }

  /// 按事件返回，保留 `event:` 类型，供需要区分事件类型的调用方使用。
  List<SseEvent> addChunkEvents(String chunk) {
    if (chunk.isEmpty) return const [];
    _buffer.write(chunk);
    return _drainCompleteLines();
  }

  List<String> close() {
    final events = _drainCompleteLines();
    final remaining = _buffer.toString();
    _buffer.clear();
    final trimmed = remaining.trim();
    if (trimmed.isEmpty) {
      return events.map((event) => event.data).toList(growable: false);
    }
    final payload = _payloadFromLine(trimmed);
    if (payload == null) {
      return events.map((event) => event.data).toList(growable: false);
    }
    return [...events.map((event) => event.data), payload];
  }

  /// 关闭并返回剩余事件，保留事件类型。
  List<SseEvent> closeEvents() {
    final events = _drainCompleteLines();
    final remaining = _buffer.toString().trim();
    _buffer.clear();
    if (remaining.isEmpty) return events;
    final payload = _payloadFromLine(remaining);
    if (payload == null) return events;
    return [...events, SseEvent(type: _pendingEventType, data: payload)];
  }

  List<SseEvent> _drainCompleteLines() {
    final events = <SseEvent>[];
    final value = _buffer.toString();
    var start = 0;
    while (true) {
      final index = value.indexOf('\n', start);
      if (index < 0) break;
      final line = value.substring(start, index);
      start = index + 1;
      final parsed = _parseLine(line);
      if (parsed != null) events.add(parsed);
    }
    _buffer
      ..clear()
      ..write(value.substring(start));
    return events;
  }

  /// 解析单行。空行代表事件结束，此时重置累积的事件类型。
  SseEvent? _parseLine(String line) {
    final trimmed = line.trim();
    if (trimmed.isEmpty) {
      _pendingEventType = '';
      return null;
    }
    if (trimmed.startsWith(':')) {
      heartbeatCount += 1;
      return null; // 注释/心跳
    }
    if (trimmed.startsWith('event:')) {
      _pendingEventType = trimmed.substring(6).trim();
      return null;
    }
    if (!trimmed.startsWith('data:')) return null;
    final payload = trimmed.substring(5).trim();
    if (payload.isEmpty) return null;
    final event = SseEvent(type: _pendingEventType, data: payload);
    // 数据已消费，类型随之失效，避免下一个事件继承旧类型。
    _pendingEventType = '';
    return event;
  }

  String? _payloadFromLine(String line) {
    final trimmed = line.trim();
    if (!trimmed.startsWith('data:')) return null;
    final payload = trimmed.substring(5).trim();
    return payload.isEmpty ? null : payload;
  }
}
