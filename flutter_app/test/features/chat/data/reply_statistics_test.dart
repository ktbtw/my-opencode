import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/data/chat_repository.dart';
import 'package:chat_codex_app/features/chat/data/task_event_batcher.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'real usage sums distinct calls and replay/completion is idempotent',
    () {
      final first = {
        'message_id': 'a',
        'total_tokens': 123,
        'output_tokens': 7,
      };
      final stats = const ReplyStatistics()
          .recordUsage(first)
          .recordUsage({
            'message_id': 'b',
            'input_tokens': 80,
            'output_tokens': 10,
            'reasoning_tokens': 2,
            'cache_read_tokens': 8,
            'cache_write_tokens': 5,
          })
          .recordUsage(first)
          .recordUsage({
            ...first,
            'stage': 'request_preparing',
            'total_tokens': 99999,
          });
      expect(stats.totalTokens, 228);
      expect(stats.merge(stats).totalTokens, 228);
      expect(
        const ReplyStatistics().recordUsage({'total_tokens': 0}).totalTokens,
        0,
      );
      expect(const ReplyStatistics().totalTokens, isNull);
      expect(
        const ReplyStatistics().recordUsage({'total_tokens': -1}).totalTokens,
        isNull,
      );
    },
  );

  test('grapheme count excludes whitespace without splitting joined emoji', () {
    expect(replyCharacterCount('你 好\nA 👨‍👩‍👧‍👦 👍🏽 e\u0301\t'), 6);
  });

  test('batch retains first text timing and last cursor', () async {
    final out = await batchTaskDeltas(
      Stream.fromIterable([
        {
          'type': 'delta',
          'content': 'a',
          'sent_at': '2026-01-01T00:00:02Z',
          'id': 1,
          'metadata': {'reply_first_text_ms': 1500},
        },
        {
          'type': 'delta',
          'content': 'b',
          'sent_at': '2026-01-01T00:00:05Z',
          'id': 2,
        },
      ]),
    ).toList();
    expect(out.single['id'], 2);
    expect(out.single['_first_delta_sent_at'], '2026-01-01T00:00:02Z');
    expect(
      (out.single['_first_delta_metadata'] as Map)['reply_first_text_ms'],
      1500,
    );
  });

  test(
    'cold reconstructed history restores per-round archived usage and timing',
    () {
      final repo = ChatRepository();
      final snapshot = repo.snapshotFromTaskEvents('t', [
        {
          'type': 'delta',
          'content': 'first',
          '_reconstructed_history': true,
          'sent_at': '2026-01-01T00:00:50Z',
        },
        {
          'type': 'input_applied',
          'content': 'next',
          'sent_at': '2026-01-01T00:01:00Z',
        },
        {
          'type': 'delta',
          'content': 'second',
          '_reconstructed_history': true,
          'sent_at': '2026-01-01T00:01:50Z',
        },
        {'type': 'completed'},
        for (var i = 0; i < 2; i++)
          {
            'type': 'progress',
            'metadata': {
              'source': 'llm_usage',
              '_archived_statistics': true,
              'stage': 'request_usage_ready',
              'reply_round_index': i,
              'message_id': 'm$i',
              'total_tokens': (i + 1) * 100,
              'reply_first_text_ms': 1200 + i * 100,
            },
          },
        {
          'type': 'progress',
          'metadata': {
            'source': 'llm_usage',
            '_archived_statistics': true,
            'message_id': 'm1',
            'total_tokens': 200,
          },
        },
      ]);
      expect(snapshot.rounds.map((r) => r.statistics.totalTokens), [100, 200]);
      expect(
        snapshot.rounds.map(
          (r) => r.statistics.firstTextLatency?.inMilliseconds,
        ),
        [1200, 1300],
      );
      expect(snapshot.statistics.totalTokens, 300);
      expect(snapshot.rounds.first.statistics.firstTextAt, isNull);
    },
  );

  test(
    'first delta replay retains original time and missing legacy timing stays unknown',
    () {
      final start = DateTime.utc(2026);
      final stats = const ReplyStatistics()
          .recordFirstText(start.add(const Duration(seconds: 2)))
          .recordFirstText(start.add(const Duration(hours: 2)));
      expect(stats.latencyFrom(start), const Duration(seconds: 2));
      final snapshot = ChatRepository().snapshotFromTaskEvents('t', [
        {
          'type': 'delta',
          'content': 'old',
          '_reconstructed_history': true,
          'sent_at': '2026-01-01T10:00:00Z',
        },
      ]);
      expect(snapshot.statistics.latencyFrom(start), isNull);
      expect(
        const ReplyStatistics().recordUsage({
          'stage': 'reply_timing_ready',
          'reply_first_text_ms': 30,
        }).firstTextLatency,
        const Duration(milliseconds: 30),
      );
    },
  );
}
