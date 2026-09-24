import 'package:characters/characters.dart';

int replyCharacterCount(String content) =>
    content.characters.where((c) => c.trim().isNotEmpty).length;

/// Immutable measurements shared by live messages and restored history.
/// Request IDs make progress, completion and replay of one usage idempotent.
class ReplyStatistics {
  final Map<String, int> requests;
  final DateTime? firstTextAt;
  final Duration? firstTextLatency;
  const ReplyStatistics({
    this.requests = const {},
    this.firstTextAt,
    this.firstTextLatency,
  });

  int? get totalTokens =>
      requests.isEmpty ? null : requests.values.fold<int>(0, (a, b) => a + b);

  ReplyStatistics recordUsage(Map<String, dynamic> usage) {
    int? number(Object? value) {
      final n = value is num ? value.toInt() : int.tryParse('$value');
      return n != null && n >= 0 ? n : null;
    }

    final latency = number(usage['reply_first_text_ms']);
    final measured = latency == null
        ? this
        : ReplyStatistics(
            requests: requests,
            firstTextAt: firstTextAt,
            firstTextLatency: Duration(milliseconds: latency),
          );
    final stage = usage['stage']?.toString() ?? '';
    final reported = number(usage['total_tokens']);
    final components = [
      'input_tokens',
      'output_tokens',
      'reasoning_tokens',
      'cache_read_tokens',
      'cache_write_tokens',
    ].map((key) => number(usage[key])).toList();
    final hasTokens = reported != null || components.any((n) => n != null);
    // Context estimates and request preparation can carry the previous call's usage.
    if (!hasTokens || (stage.isNotEmpty && stage != 'request_usage_ready')) {
      return measured;
    }
    final count =
        reported ?? components.fold<int>(0, (sum, n) => sum + (n ?? 0));
    final requestId = usage['message_id']?.toString().trim() ?? '';
    final next = {...requests};
    if (requestId.isNotEmpty) {
      next.remove('legacy');
      next[requestId] = count;
    } else if (next.keys.every((key) => key == 'legacy')) {
      // Old payloads have no per-request identity: retain one reported sample,
      // rather than summing repeated progress and completion copies.
      next['legacy'] = count;
    }
    return ReplyStatistics(
      requests: Map.unmodifiable(next),
      firstTextAt: firstTextAt,
      firstTextLatency: latency == null
          ? firstTextLatency
          : Duration(milliseconds: latency),
    );
  }

  ReplyStatistics recordFirstText(DateTime? at) {
    if (at == null || (firstTextAt != null && !at.isBefore(firstTextAt!))) {
      return this;
    }
    return ReplyStatistics(
      requests: requests,
      firstTextAt: at,
      firstTextLatency: firstTextLatency,
    );
  }

  ReplyStatistics merge(ReplyStatistics other) {
    final next = {...requests, ...other.requests};
    if (next.keys.any((key) => key != 'legacy')) next.remove('legacy');
    final at = firstTextAt == null
        ? other.firstTextAt
        : other.firstTextAt == null || firstTextAt!.isBefore(other.firstTextAt!)
        ? firstTextAt
        : other.firstTextAt;
    return ReplyStatistics(
      requests: Map.unmodifiable(next),
      firstTextAt: at,
      firstTextLatency: firstTextLatency ?? other.firstTextLatency,
    );
  }

  Duration? latencyFrom(DateTime start) {
    if (firstTextLatency != null) return firstTextLatency;
    if (firstTextAt == null || firstTextAt!.isBefore(start)) return null;
    return firstTextAt!.difference(start);
  }
}
