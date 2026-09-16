import 'package:flutter_test/flutter_test.dart';
import 'package:chat_codex_app/core/config/sse_parser.dart';

void main() {
  group('SseDataParser 事件类型', () {
    test('保留 event: 类型并配对 data', () {
      final parser = SseDataParser();
      final events = parser.addChunkEvents(
        'event: device.metrics\ndata: {"machine_id":"m_1"}\n\n',
      );
      expect(events.length, 1);
      expect(events.first.type, 'device.metrics');
      expect(events.first.data, '{"machine_id":"m_1"}');
    });

    test('无 event 行时类型为空字符串', () {
      final parser = SseDataParser();
      final events = parser.addChunkEvents('data: {"type":"completed"}\n\n');
      expect(events.length, 1);
      expect(events.first.type, '');
      expect(events.first.data, '{"type":"completed"}');
    });

    test('一条数据后不继承上一事件的类型', () {
      final parser = SseDataParser();
      final events = parser.addChunkEvents(
        'event: snapshot\ndata: {"a":1}\n\ndata: {"b":2}\n\n',
      );
      expect(events.length, 2);
      expect(events[0].type, 'snapshot');
      expect(events[1].type, '', reason: '第二个事件没有 event 行，类型应为空');
    });

    test('忽略注释行（SSE 心跳）', () {
      final parser = SseDataParser();
      final events = parser.addChunkEvents(
        ': heartbeat 123\n\nevent: device.metrics\ndata: {"a":1}\n\n',
      );
      expect(events.length, 1);
      expect(events.first.type, 'device.metrics');
    });

    test('跨 chunk 分片能正确拼接', () {
      final parser = SseDataParser();
      expect(parser.addChunkEvents('event: device.met'), isEmpty);
      expect(parser.addChunkEvents('rics\ndata: {"m'), isEmpty);
      final events = parser.addChunkEvents('achine_id":"m_1"}\n\n');
      expect(events.length, 1);
      expect(events.first.type, 'device.metrics');
      expect(events.first.data, '{"machine_id":"m_1"}');
    });

    test('多个事件连续到达按顺序返回', () {
      final parser = SseDataParser();
      final events = parser.addChunkEvents(
        'event: snapshot\ndata: {"a":1}\n\n'
        'event: device.metrics\ndata: {"b":2}\n\n'
        'event: task.updated\ndata: {"c":3}\n\n',
      );
      expect(events.map((e) => e.type).toList(), [
        'snapshot',
        'device.metrics',
        'task.updated',
      ]);
    });
  });

  group('SseDataParser 向后兼容', () {
    test('addChunk 仍只返回 data 字符串', () {
      final parser = SseDataParser();
      final payloads = parser.addChunk(
        'event: device.metrics\ndata: {"machine_id":"m_1"}\n\n',
      );
      expect(payloads, ['{"machine_id":"m_1"}']);
    });

    test('close 返回未以换行结尾的残留数据', () {
      final parser = SseDataParser();
      expect(parser.addChunk('data: {"x":1}'), isEmpty);
      expect(parser.close(), ['{"x":1}']);
    });

    test('closeEvents 返回残留事件并保留类型', () {
      final parser = SseDataParser();
      // event 行完整到达，data 行为残留（无换行）。
      parser.addChunkEvents('event: snapshot\ndata: {"x":1}');
      final events = parser.closeEvents();
      expect(events.length, 1);
      expect(events.first.type, 'snapshot');
      expect(events.first.data, '{"x":1}');
    });

    test('带换行的完整行会立即产出', () {
      final parser = SseDataParser();
      expect(parser.addChunk('data: {"x":1}\n'), ['{"x":1}']);
    });

    test('空数据行被忽略', () {
      final parser = SseDataParser();
      expect(parser.addChunkEvents('data:\n\n'), isEmpty);
    });
  });
}
