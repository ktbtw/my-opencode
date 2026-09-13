import 'package:flutter_test/flutter_test.dart';
import 'package:chat_codex_app/core/config/sse_parser.dart';

void main() {
  test('SseDataParser keeps partial data lines across chunks', () {
    final parser = SseDataParser();

    expect(parser.addChunk('event: delta\n'), isEmpty);
    expect(parser.addChunk('data: {"type":"delta","field":"reas'), isEmpty);
    expect(parser.addChunk('oning","content":"ok"}\n\n'), [
      '{"type":"delta","field":"reasoning","content":"ok"}',
    ]);
    expect(parser.close(), isEmpty);
  });

  test(
    'SseDataParser ignores comments and flushes final complete data line',
    () {
      final parser = SseDataParser();

      expect(parser.addChunk(': ping\n\n'), isEmpty);
      expect(parser.addChunk('data: {"type":"completed"}'), isEmpty);
      expect(parser.close(), ['{"type":"completed"}']);
    },
  );
}
