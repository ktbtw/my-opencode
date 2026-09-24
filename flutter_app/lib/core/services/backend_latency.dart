import 'dart:async';
import 'dart:convert';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:http/http.dart' as http;
import '../config/api_client.dart';
import 'account_scope.dart';

/// One low-frequency probe shared by visible consumers in this account.
class BackendLatency {
  final Future<void> Function()? probe;
  final Duration interval;
  Timer? _timer;
  http.Client? _client;
  bool _disposed = false;
  bool _inFlight = false;
  int? milliseconds;
  bool? reachable;
  DateTime? measuredAt;

  BackendLatency({this.probe, this.interval = const Duration(seconds: 15)});

  void start() {
    if (_disposed || _timer != null) return;
    unawaited(measure());
    _timer = Timer.periodic(interval, (_) => unawaited(measure()));
  }

  Future<void> measure() async {
    if (_disposed || _inFlight) return;
    _inFlight = true;
    final watch = Stopwatch()..start();
    final client = probe == null ? http.Client() : null;
    _client = client;
    try {
      if (probe != null) {
        await probe!().timeout(const Duration(seconds: 5));
      } else {
        final response = await client!
            .get(
              Uri.parse(
                '${ApiClient.baseUrl}/healthz?t=${DateTime.now().millisecondsSinceEpoch}',
              ),
              headers: {'Cache-Control': 'no-cache'},
            )
            .timeout(const Duration(seconds: 5));
        if (response.statusCode != 200 ||
            jsonDecode(response.body)['status'] != 'ok') {
          throw const FormatException('Unexpected health response');
        }
      }
      if (_disposed) return;
      milliseconds = watch.elapsedMilliseconds;
      reachable = true;
      measuredAt = DateTime.now();
    } catch (_) {
      if (_disposed) return;
      milliseconds = null;
      reachable = false;
      measuredAt = DateTime.now();
    } finally {
      client?.close();
      if (identical(_client, client)) _client = null;
      _inFlight = false;
    }
  }

  void dispose() {
    _disposed = true;
    _timer?.cancel();
    _client?.close();
  }
}

final backendLatencyProvider = Provider.autoDispose<BackendLatency>((ref) {
  ref.watch(accountScopeProvider);
  final latency = BackendLatency()..start();
  ref.onDispose(latency.dispose);
  return latency;
});
