import 'dart:async';
import 'package:chat_codex_app/core/services/connection_telemetry.dart';
import 'package:chat_codex_app/core/services/backend_latency.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'quiet healthy links expire without heartbeats and clear on reconnect',
    () {
      var now = DateTime(2026);
      final link = ConnectionTelemetry(clock: () => now)..connecting();
      expect(link.healthy, isFalse);
      link.activity();
      now = now.add(const Duration(seconds: 30));
      expect(link.healthy, isTrue);
      link.activity();
      now = now.add(const Duration(seconds: 46));
      expect(link.healthy, isFalse);
      link.disconnected(retry: true);
      expect(link.phase, LinkPhase.reconnecting);
      link.connecting();
      expect(link.lastActivity, isNull);
      link.activity();
      expect(link.healthy, isTrue);
    },
  );

  test('Unicode receive rate decays to zero and resets across connections', () {
    var now = DateTime(2026);
    final link = ConnectionTelemetry(clock: () => now)..activity();
    link.output('你好🌙');
    expect(link.charactersPerSecond, 1);
    for (var i = 0; i < 3000; i++) {
      link.output('a');
    }
    expect(link.charactersPerSecond, 1001);
    now = now.add(const Duration(seconds: 3));
    expect(link.charactersPerSecond, 0);
    link.output('abc');
    link.disconnected(retry: true);
    expect(link.charactersPerSecond, 0);
    link.connecting();
    expect(link.lastOutput, isNull);
  });

  test(
    'latency avoids overlapping requests and ignores disposed results',
    () async {
      var calls = 0;
      final pending = Completer<void>();
      final latency = BackendLatency(
        probe: () {
          calls++;
          return pending.future;
        },
      );
      final first = latency.measure();
      await latency.measure();
      expect(calls, 1);
      latency.dispose();
      pending.complete();
      await first;
      expect(latency.milliseconds, isNull);
      expect(latency.reachable, isNull);
    },
  );

  test('failed probe clears old latency then recovers', () async {
    var fail = false;
    final latency = BackendLatency(
      probe: () async {
        if (fail) throw StateError('offline');
      },
    );
    await latency.measure();
    expect(latency.reachable, isTrue);
    expect(latency.milliseconds, isNotNull);
    fail = true;
    await latency.measure();
    expect(latency.reachable, isFalse);
    expect(latency.milliseconds, isNull);
    fail = false;
    await latency.measure();
    expect(latency.reachable, isTrue);
    latency.dispose();
  });

  testWidgets('periodic probes stop on disposal', (tester) async {
    var calls = 0;
    final latency = BackendLatency(
      probe: () async {
        calls++;
      },
    );
    latency.start();
    await tester.pump();
    expect(calls, 1);
    await tester.pump(const Duration(seconds: 15));
    expect(calls, 2);
    latency.dispose();
    await tester.pump(const Duration(seconds: 30));
    expect(calls, 2);
  });
}
