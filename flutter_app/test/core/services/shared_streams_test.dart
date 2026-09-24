import 'dart:async';
import 'package:chat_codex_app/core/services/shared_streams.dart';
import 'package:chat_codex_app/core/services/app_live_events.dart';
import 'package:chat_codex_app/core/config/sse_parser.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('same task shares a socket and last consumer releases it', () async {
    var opens = 0;
    var cancels = 0;
    final source = StreamController<int>(onCancel: () => cancels++);
    final streams = SharedStreams<int>();
    Stream<int> open() {
      opens++;
      return source.stream;
    }

    final a = <int>[];
    final b = <int>[];
    final first = streams.watch('task', open).listen(a.add);
    final second = streams.watch('task', open).listen(b.add);
    source.add(1);
    await pumpEventQueue();
    expect(opens, 1);
    expect(a, [1]);
    expect(b, [1]);
    await first.cancel();
    expect(cancels, 0);
    source.add(2);
    await pumpEventQueue();
    expect(b, [1, 2]);
    await second.cancel();
    expect(cancels, 1);
    expect(streams.contains('task'), isFalse);
    await source.close();
    streams.dispose();
  });

  test('different tasks stay isolated and dispose closes all', () async {
    final streams = SharedStreams<int>();
    var cancels = 0;
    final a = StreamController<int>(onCancel: () => cancels++);
    final b = StreamController<int>(onCancel: () => cancels++);
    final first = <int>[];
    final second = <int>[];
    streams.watch('a', () => a.stream).listen(first.add);
    streams.watch('b', () => b.stream).listen(second.add);
    a.add(7);
    b.add(8);
    await pumpEventQueue();
    expect(first, [7]);
    expect(second, [8]);
    streams.dispose();
    await pumpEventQueue();
    expect(cancels, 2);
    await a.close();
    await b.close();
    expect(
      await streams.watch('a', () => throw StateError('closed')).toList(),
      isEmpty,
    );
  });

  test(
    'global statuses and multiple metrics consumers share one connection',
    () async {
      var opens = 0;
      var cancels = 0;
      final source = StreamController<SseEvent>(onCancel: () => cancels++);
      final events = AppLiveEvents(
        connect: () {
          opens++;
          return source.stream;
        },
      );
      final first = events.watch().listen((_) {});
      final second = events.watch().listen((_) {});
      final third = events.watch().listen((_) {});
      await pumpEventQueue();
      expect(opens, 1);
      await first.cancel();
      await second.cancel();
      expect(cancels, 0);
      await third.cancel();
      expect(cancels, 1);
      await source.close();
      events.dispose();
    },
  );
}
