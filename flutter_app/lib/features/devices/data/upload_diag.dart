import 'package:flutter/foundation.dart';

import '../../../core/services/app_log_service.dart';

/// 上传耗时诊断。
///
/// 默认关闭（每个分块一行，上传大文件会刷屏）；排查上传性能时用
/// `--dart-define=CHAT_CODEX_UPLOAD_DIAG=true` 打开。
///
/// 输出两条记录：
/// - `upload_chunk`：单个分块的往返耗时与起止时间戳，用来看并发区间是否重叠
/// - `upload_summary`：一次上传的整体统计，用来自动判定瓶颈类型
class UploadDiag {
  const UploadDiag._();

  static const bool enabled = bool.fromEnvironment(
    'CHAT_CODEX_UPLOAD_DIAG',
    defaultValue: false,
  );

  /// 单个分块的往返记录。
  static void chunk({
    required String uploadId,
    required int chunkIndex,
    required int payloadBytes,
    required int decodedBytes,
    required DateTime startedAt,
    required DateTime endedAt,
    required int attempt,
    String? error,
  }) {
    if (!enabled) return;
    final payload = <String, Object?>{
      'stage': 'upload_chunk',
      'upload_id': uploadId,
      'chunk_index': chunkIndex,
      'payload_bytes': payloadBytes,
      'decoded_bytes': decodedBytes,
      'total_ms': endedAt.difference(startedAt).inMilliseconds,
      'attempt': attempt,
      'start_ms': startedAt.millisecondsSinceEpoch,
      'end_ms': endedAt.millisecondsSinceEpoch,
      if (error != null) 'error': error,
    };
    debugPrint('[UploadDiag] chunk $payload');
    AppLogService.log('upload_chunk', data: payload);
  }

  /// 一次上传的整体统计与瓶颈判定。
  static void summary({
    required String uploadId,
    required int totalBytes,
    required int totalChunks,
    required int chunkSize,
    required int concurrency,
    required DateTime startedAt,
    required DateTime endedAt,
    required List<int> chunkDurationsMs,
    required int retries,
    String? error,
  }) {
    if (!enabled) return;
    final wallMs = endedAt.difference(startedAt).inMilliseconds;
    final payload = <String, Object?>{
      'stage': 'upload_summary',
      'upload_id': uploadId,
      'total_bytes': totalBytes,
      'total_chunks': totalChunks,
      'chunk_size': chunkSize,
      'concurrency': concurrency,
      'wall_ms': wallMs,
      'retries': retries,
      'sent_chunks': chunkDurationsMs.length,
      if (chunkDurationsMs.isNotEmpty)
        'chunk_ms_avg':
            chunkDurationsMs.reduce((a, b) => a + b) ~/ chunkDurationsMs.length,
      if (chunkDurationsMs.isNotEmpty)
        'chunk_ms_max': chunkDurationsMs.reduce((a, b) => a > b ? a : b),
      if (error != null) 'error': error,
      ..._verdict(
        wallMs: wallMs,
        totalChunks: totalChunks,
        chunkDurationsMs: chunkDurationsMs,
        concurrency: concurrency,
      ),
    };
    debugPrint('[UploadDiag] summary $payload');
    AppLogService.log('upload_summary', data: payload);
  }

  /// 用「实际耗时 vs 理论耗时」自动判定瓶颈，免去人工算。
  ///
  /// 判定依据：串行耗时 ≈ 各分块耗时之和；理想并发耗时 ≈ 总和 / 并发度。
  /// 若实测接近串行值，说明并发被服务端/设备端串行化了。
  static Map<String, Object?> _verdict({
    required int wallMs,
    required int totalChunks,
    required List<int> chunkDurationsMs,
    required int concurrency,
  }) {
    if (chunkDurationsMs.isEmpty || wallMs <= 0) return const {};
    final sum = chunkDurationsMs.fold<int>(0, (a, b) => a + b);
    final idealMs = (sum / concurrency).round();
    // 串行度：1.0 表示完全串行（实测≈各块之和），越低说明并发越有效。
    final serialRatio = sum == 0 ? 0.0 : wallMs / sum;
    final verdict = switch (serialRatio) {
      >= 0.85 => '串行瓶颈：并发未生效，检查设备端锁',
      <= 0.45 => '并发良好',
      _ => '部分并发',
    };
    return {
      'chunk_ms_sum': sum,
      'ideal_concurrent_ms': idealMs,
      'serial_ratio': (serialRatio * 100).round() / 100,
      'verdict': verdict,
    };
  }

  /// 测试入口：直接验证阈值语义。
  @visibleForTesting
  static Map<String, Object?> verdictForTest({
    required int wallMs,
    required List<int> chunkDurationsMs,
    required int concurrency,
  }) => _verdict(
    wallMs: wallMs,
    totalChunks: chunkDurationsMs.length,
    chunkDurationsMs: chunkDurationsMs,
    concurrency: concurrency,
  );
}

/// 暴露瓶颈判定给测试，保证阈值边界不被改坏。
@visibleForTesting
Map<String, Object?> diagVerdictForTest({
  required int wallMs,
  required List<int> chunkDurationsMs,
  required int concurrency,
}) => UploadDiag.verdictForTest(
  wallMs: wallMs,
  chunkDurationsMs: chunkDurationsMs,
  concurrency: concurrency,
);
