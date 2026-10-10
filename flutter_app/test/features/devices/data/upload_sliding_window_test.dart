import 'dart:async';

import 'package:flutter_test/flutter_test.dart';

/// 验证「滑动窗口」的调度语义。
///
/// 真实上传走 HTTP，单测里用等价的调度器复现：只要保证
/// 「任意一个在途任务完成就立刻补新的」这一条，批处理空档就不会出现。
/// 这里用一个可精确控制完成顺序的假任务来断言。
class _SlidingWindow {
  _SlidingWindow(this.concurrency);
  final int concurrency;
  final inFlight = <Future<void>>{};
  final started = <int>[];
  Object? firstError;
  StackTrace? firstStack;

  Future<void> submit(int index, Future<void> Function() run) async {
    if (inFlight.length >= concurrency) {
      await Future.any(inFlight);
      if (firstError != null) throw firstError!;
    }
    started.add(index);
    late final Future<void> task;
    // 与生产实现同构：失败要留存首个错误，否则会被 inFlight 移除后丢失。
    task = run()
        .then<void>((_) {}, onError: (Object error, StackTrace stack) {
          firstError ??= error;
          firstStack ??= stack;
        })
        .whenComplete(() => inFlight.remove(task));
    inFlight.add(task);
  }

  Future<void> drain() async {
    await Future.wait(inFlight.toList());
    if (firstError != null) {
      Error.throwWithStackTrace(firstError!, firstStack ?? StackTrace.current);
    }
  }
}

void main() {
  group('滑动窗口调度', () {
    test('任意一块完成即补新块，不等整批', () async {
      final completers = <int, Completer<void>>{};
      final window = _SlidingWindow(3);

      Future<void> task(int i) {
        final c = Completer<void>();
        completers[i] = c;
        return c.future;
      }

      // 先填满 3 个通道。
      await window.submit(0, () => task(0));
      await window.submit(1, () => task(1));
      await window.submit(2, () => task(2));
      expect(window.started, [0, 1, 2]);

      // 只让「最快」的 0 完成——批处理实现会等到 1、2 也完成才继续，
      // 滑动窗口应立即补上第 3 块。
      completers[0]!.complete();
      await window.submit(3, () => task(3));
      expect(
        window.started,
        [0, 1, 2, 3],
        reason: '块 0 完成后必须立刻补块 3，而不是等 1、2',
      );

      // 收尾：完成剩余任务。
      completers[1]!.complete();
      completers[2]!.complete();
      completers[3]!.complete();
      await window.drain();
    });

    test('在途任务数不超过并发上限', () async {
      final completers = <int, Completer<void>>{};
      final window = _SlidingWindow(2);
      var maxInFlight = 0;

      Future<void> task(int i) {
        final c = Completer<void>();
        completers[i] = c;
        return c.future;
      }

      for (var i = 0; i < 2; i++) {
        await window.submit(i, () => task(i));
        maxInFlight = maxInFlight > window.inFlight.length
            ? maxInFlight
            : window.inFlight.length;
      }
      expect(maxInFlight, 2, reason: '内存占用上限 = 并发数 × 分块大小');

      completers[0]!.complete();
      await window.submit(2, () => task(2));
      expect(window.inFlight.length, 2);

      completers[1]!.complete();
      completers[2]!.complete();
      await window.drain();
      expect(window.inFlight, isEmpty);
    });

    test('失败会从在途集合移除并向上抛出', () async {
      final window = _SlidingWindow(2);
      await window.submit(0, () async {});
      await window.submit(1, () async => throw StateError('分块上传失败'));

      await expectLater(window.drain(), throwsA(isA<StateError>()));
      expect(window.inFlight, isEmpty, reason: '失败的块也要从在途集合清理');
    });
  });
}
