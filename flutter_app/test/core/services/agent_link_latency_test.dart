import 'dart:async';
import 'package:chat_codex_app/core/services/agent_link_latency.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'uses server RTT instead of phone request elapsed time and clears failures',
    () async {
      var failed = false;
      final latency = AgentLinkLatency(
        'a',
        probe: () async {
          if (failed) throw StateError('HTTP offline');
          return {'status': 'connected', 'rtt_ms': 987};
        },
      );
      addTearDown(latency.dispose);
      await latency.measure();
      expect(latency.milliseconds, 987);
      expect(latency.description, '987 ms 往返');
      latency.receivedAt = DateTime.now().subtract(const Duration(seconds: 46));
      expect(latency.description, '等待测量');
      failed = true;
      await latency.measure();
      expect(latency.milliseconds, isNull);
      expect(latency.status, 'unavailable');
    },
  );

  test(
    'deduplicates concurrent measurements and ignores completion after dispose',
    () async {
      final result = Completer<Map<String, dynamic>>();
      var calls = 0;
      final latency = AgentLinkLatency(
        'a',
        probe: () {
          calls++;
          return result.future;
        },
      );
      final first = latency.measure();
      await latency.measure();
      expect(calls, 1);
      latency.dispose();
      result.complete({'status': 'connected', 'rtt_ms': 20});
      await first;
      expect(latency.milliseconds, isNull);
    },
  );
}
