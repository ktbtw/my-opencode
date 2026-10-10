import 'package:flutter_test/flutter_test.dart';

import 'package:chat_codex_app/features/devices/data/upload_diag.dart';

void main() {
  group('UploadDiag 瓶颈判定', () {
    /// _verdict 是私有实现，这里通过 summary 的公开入口间接验证：
    /// 由于 enabled 默认 false，改用直接断言阈值语义的方式——
    /// 把判定逻辑抽出来单独测，保证阈值边界不被改坏。
    test('完全串行时判定为串行瓶颈', () {
      // 5 个分块各 1000ms，并发 5，实测 5000ms → serial_ratio = 1.0
      final result = diagVerdictForTest(
        wallMs: 5000,
        chunkDurationsMs: const [1000, 1000, 1000, 1000, 1000],
        concurrency: 5,
      );
      expect(result['serial_ratio'], 1.0);
      expect(result['verdict'], contains('串行瓶颈'));
      expect(result['chunk_ms_sum'], 5000);
      expect(result['ideal_concurrent_ms'], 1000);
    });

    test('并发良好时判定为并发良好', () {
      // 5 个分块各 1000ms，并发 5，实测 1100ms → serial_ratio ≈ 0.22
      final result = diagVerdictForTest(
        wallMs: 1100,
        chunkDurationsMs: const [1000, 1000, 1000, 1000, 1000],
        concurrency: 5,
      );
      expect(result['serial_ratio'], 0.22);
      expect(result['verdict'], '并发良好');
    });

    test('部分并发落在中间区间', () {
      // 实测 3000ms / 总和 5000ms = 0.6
      final result = diagVerdictForTest(
        wallMs: 3000,
        chunkDurationsMs: const [1000, 1000, 1000, 1000, 1000],
        concurrency: 5,
      );
      expect(result['serial_ratio'], 0.6);
      expect(result['verdict'], '部分并发');
    });

    test('单并发时串行是预期行为，仍如实标记', () {
      // free 用户并发 1，实测≈总和属正常，判定仍按公式给出。
      final result = diagVerdictForTest(
        wallMs: 3000,
        chunkDurationsMs: const [1000, 1000, 1000],
        concurrency: 1,
      );
      expect(result['serial_ratio'], 1.0);
      expect(result['ideal_concurrent_ms'], 3000);
    });

    test('空分块列表不产生判定', () {
      final result = diagVerdictForTest(
        wallMs: 100,
        chunkDurationsMs: const [],
        concurrency: 3,
      );
      expect(result, isEmpty);
    });

    test('零耗时窗口不产生判定', () {
      final result = diagVerdictForTest(
        wallMs: 0,
        chunkDurationsMs: const [10],
        concurrency: 3,
      );
      expect(result, isEmpty);
    });
  });
}
