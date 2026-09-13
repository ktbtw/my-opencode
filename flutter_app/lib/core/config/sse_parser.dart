class SseHttpException implements Exception {
  final int statusCode;
  const SseHttpException(this.statusCode);
}

class SseDataParser {
  final _buffer = StringBuffer();

  List<String> addChunk(String chunk) {
    if (chunk.isEmpty) return const [];
    _buffer.write(chunk);
    return _drainCompleteLines();
  }

  List<String> close() {
    final payloads = _drainCompleteLines();
    final remaining = _buffer.toString();
    _buffer.clear();
    final payload = _payloadFromLine(remaining);
    if (payload == null) return payloads;
    return [...payloads, payload];
  }

  List<String> _drainCompleteLines() {
    final payloads = <String>[];
    while (true) {
      final value = _buffer.toString();
      final index = value.indexOf('\n');
      if (index < 0) break;
      final line = value.substring(0, index);
      _buffer
        ..clear()
        ..write(value.substring(index + 1));
      final payload = _payloadFromLine(line);
      if (payload != null) payloads.add(payload);
    }
    return payloads;
  }

  String? _payloadFromLine(String line) {
    final trimmed = line.trim();
    if (!trimmed.startsWith('data:')) return null;
    final payload = trimmed.substring(5).trim();
    return payload.isEmpty ? null : payload;
  }
}
