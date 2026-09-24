import 'dart:async';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../config/api_client.dart';
import '../storage/app_storage.dart';
import 'account_scope.dart';

/// Server-measured RTT over the Agent's existing socket. Only visible headers
/// watch this provider; same-agent consumers share a single polling timer.
class AgentLinkLatency {
  final String agentId;
  final Future<Map<String, dynamic>> Function()? probe;
  final Duration interval;
  Timer? _timer;
  bool _disposed = false;
  bool _inFlight = false;
  int? milliseconds;
  String status = 'measuring';
  DateTime? receivedAt;
  final String? _identity;

  AgentLinkLatency(
    this.agentId, {
    this.probe,
    this.interval = const Duration(seconds: 15),
  }) : _identity = AppStorage.initialized ? AppStorage.storageIdentity : null;

  bool get fresh =>
      receivedAt != null &&
      DateTime.now().difference(receivedAt!) < const Duration(seconds: 45);

  void start() {
    if (_disposed || _timer != null) return;
    unawaited(measure());
    _timer = Timer.periodic(interval, (_) => unawaited(measure()));
  }

  Future<void> measure() async {
    if (_disposed || _inFlight) return;
    _inFlight = true;
    try {
      final data =
          await (probe?.call() ??
                  ApiClient.get(
                    '/api/agents/${Uri.encodeComponent(agentId)}/connection-latency',
                    timeout: const Duration(seconds: 5),
                    expectedIdentity: _identity,
                  ))
              .timeout(const Duration(seconds: 5));
      if (_disposed) return;
      final ms = (data['rtt_ms'] as num?)?.toInt();
      status = data['status']?.toString() ?? 'unknown';
      milliseconds = status == 'connected' && ms != null && ms >= 0 ? ms : null;
      receivedAt = DateTime.now();
    } catch (_) {
      if (_disposed) return;
      status = 'unavailable';
      milliseconds = null;
      receivedAt = DateTime.now();
    } finally {
      _inFlight = false;
    }
  }

  String get description {
    if (!fresh) return status == 'measuring' ? '正在测量' : '等待测量';
    if (milliseconds != null) return '$milliseconds ms 往返';
    return switch (status) {
      'offline' => '当前未连接',
      'timeout' => '探测超时',
      'unavailable' => '暂未取得测量',
      _ => '等待最新连接',
    };
  }

  void dispose() {
    _disposed = true;
    _timer?.cancel();
  }
}

final agentLinkLatencyProvider = Provider.autoDispose
    .family<AgentLinkLatency, String>((ref, agentId) {
      ref.watch(accountScopeProvider);
      final latency = AgentLinkLatency(agentId);
      if (AppStorage.initialized && AppStorage.isLoggedIn()) latency.start();
      ref.onDispose(latency.dispose);
      return latency;
    });
