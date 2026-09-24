import 'dart:async';
import 'package:chat_codex_app/features/chat/data/task_event_batcher.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'coalesces running tool snapshots but preserves the final result',
    () async {
      final events = [
        for (var i = 0; i < 1000; i++)
          {
            'type': 'tool_updated',
            'tool': {
              'call_id': 'shell',
              'status': 'running',
              'output': 'progress $i',
            },
          },
        {
          'type': 'tool_updated',
          'tool': {
            'call_id': 'shell',
            'status': 'completed',
            'output': 'final output',
          },
        },
        {'type': 'completed'},
      ];
      final output = await batchTaskDeltas(
        Stream.fromIterable(events),
      ).toList();
      expect(output, hasLength(3));
      expect((output[0]['tool'] as Map)['output'], 'progress 999');
      expect((output[1]['tool'] as Map)['output'], 'final output');
      expect(output.last['type'], 'completed');
    },
  );

  test(
    '10000 rapid deltas retain every character with bounded output batches',
    () async {
      final source = StreamController<Map<String, dynamic>>();
      final result = batchTaskDeltas(source.stream).toList();
      for (var i = 0; i < 10000; i++) {
        source.add({'type': 'delta', 'field': 'text', 'content': '[$i]'});
      }
      source.add({'type': 'completed'});
      await source.close();
      final events = await result;
      expect(events.last['type'], 'completed');
      expect(events.length, lessThan(10));
      expect(
        events
            .where((e) => e['type'] == 'delta')
            .map((e) => e['content'])
            .join(),
        List.generate(10000, (i) => '[$i]').join(),
      );
    },
  );

  test(
    'reasoning, tools, approval and terminal are ordering barriers',
    () async {
      final input = [
        {'type': 'delta', 'field': 'reasoning', 'content': 'why'},
        {'type': 'delta', 'field': 'text', 'content': 'one'},
        {'type': 'delta', 'field': 'text', 'content': 'two'},
        {
          'type': 'tool_updated',
          'tool': {'id': 'tool'},
        },
        {'type': 'approval_required'},
        {'type': 'delta', 'field': 'text', 'content': 'three'},
        {'type': 'cancelled'},
      ];
      final output = await batchTaskDeltas(Stream.fromIterable(input)).toList();
      expect(output.map((e) => e['type']), [
        'delta',
        'delta',
        'tool_updated',
        'approval_required',
        'delta',
        'cancelled',
      ]);
      expect(output[0]['content'], 'why');
      expect(output[1]['content'], 'onetwo');
      expect(output[4]['content'], 'three');
    },
  );

  test(
    'flushes a quiet stream on deadline and cancellation stops timers',
    () async {
      var cancelled = false;
      final source = StreamController<Map<String, dynamic>>(
        onCancel: () => cancelled = true,
      );
      final output = <Map<String, dynamic>>[];
      final sub = batchTaskDeltas(
        source.stream,
        interval: const Duration(milliseconds: 10),
      ).listen(output.add);
      source.add({'type': 'delta', 'content': 'hello'});
      await Future<void>.delayed(const Duration(milliseconds: 30));
      expect(output.single['content'], 'hello');
      source.add({'type': 'delta', 'content': 'pending'});
      await pumpEventQueue();
      await sub.cancel();
      await Future<void>.delayed(const Duration(milliseconds: 20));
      expect(cancelled, isTrue);
      expect(output, hasLength(1));
      await source.close();
    },
  );
}
