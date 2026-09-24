import 'dart:async';
import 'dart:convert';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../config/api_client.dart';
import '../config/sse_parser.dart';
import '../storage/app_storage.dart';
import 'account_scope.dart';
import 'app_foreground.dart';
import 'shared_streams.dart';
import 'connection_telemetry.dart';
import 'provider_stream.dart';

/// A snapshot is sent by the server on every connection. Status consumers
/// reconcile from REST on that boundary, so transient event IDs need not be
/// mistaken for a durable cursor across a server restart.
class AppLiveEvents {
  final Stream<SseEvent> Function()? _connect;
  final _streams = SharedStreams<SseEvent>();
  final connection = ConnectionTelemetry();
  final agents = <String, AgentConnectionStatus>{};

  void _rememberAgents(SseEvent event) {
    if (!const {
      'snapshot',
      'agent.upsert',
      'agent.remove',
    }.contains(event.type)) {
      return;
    }
    try {
      final data = jsonDecode(event.data) as Map<String, dynamic>;
      if (event.type == 'snapshot') {
        agents.clear();
        for (final device in data['devices'] as List? ?? const []) {
          for (final agent in device['agents'] as List? ?? const []) {
            _rememberAgent(Map<String, dynamic>.from(agent));
          }
        }
      } else if (event.type == 'agent.remove') {
        final id = data['agent_id']?.toString() ?? '';
        agents[id] = AgentConnectionStatus('offline', DateTime.now());
      } else {
        _rememberAgent(data);
      }
    } catch (_) {
      /* An invalid status frame must not break the event stream. */
    }
  }

  void _rememberAgent(Map<String, dynamic> data) {
    final id = data['agent_id']?.toString() ?? '';
    if (id.isEmpty) return;
    agents[id] = AgentConnectionStatus(
      data['enabled'] == false
          ? 'disabled'
          : data['status']?.toString() ?? 'unknown',
      DateTime.now(),
      taskId: data['current_task_id']?.toString(),
    );
  }

  AppLiveEvents({Stream<SseEvent> Function()? connect}) : _connect = connect;

  Stream<SseEvent> watch() => _streams.watch('overlay', _open);

  Stream<SseEvent> _open() {
    late StreamController<SseEvent> controller;
    StreamSubscription<SseEvent>? subscription;
    Timer? retry;
    var closed = false;
    var attempt = 0;
    late void Function() connect;
    void reconnect() {
      if (closed || retry != null) return;
      connection.disconnected(retry: true);
      unawaited(subscription?.cancel());
      retry = Timer(Duration(seconds: (++attempt).clamp(1, 15)), () {
        retry = null;
        connect();
      });
    }

    connect = () {
      if (closed) return;
      connection.connecting();
      agents.clear();
      subscription =
          (_connect?.call() ??
                  ApiClient.sseEvents(
                    '/api/overlay/events',
                    onConnected: connection.activity,
                    onActivity: connection.activity,
                  ))
              .listen(
                (event) {
                  connection.activity();
                  _rememberAgents(event);
                  attempt = 0;
                  controller.add(event);
                },
                onError: (Object error, StackTrace stack) {
                  reconnect();
                },
                onDone: reconnect,
                cancelOnError: true,
              );
    };
    controller = StreamController<SseEvent>(
      onListen: connect,
      onCancel: () async {
        closed = true;
        connection.disconnected();
        retry?.cancel();
        await subscription?.cancel();
      },
    );
    return controller.stream;
  }

  void dispose() => _streams.dispose();
}

class AgentConnectionStatus {
  final String status;
  final DateTime confirmedAt;
  final String? taskId;
  const AgentConnectionStatus(this.status, this.confirmedAt, {this.taskId});

  bool get fresh =>
      DateTime.now().difference(confirmedAt).abs() <
      const Duration(seconds: 45);
  bool get online =>
      status == 'online' || status == 'busy' || status == 'running';
  bool get transitioning =>
      const {'starting', 'restarting', 'upgrading'}.contains(status);
}

final appLiveEventsProvider = Provider<AppLiveEvents>((ref) {
  ref.watch(accountScopeProvider);
  final events = AppLiveEvents();
  ref.onDispose(events.dispose);
  return events;
});

final appLiveStreamProvider = StreamProvider.autoDispose<SseEvent>((ref) {
  final foreground = ref.watch(appForegroundProvider);
  final events = ref.watch(appLiveEventsProvider);
  if (!foreground || !AppStorage.initialized || !AppStorage.isLoggedIn()) {
    return const Stream.empty();
  }
  return providerOwnedStream(ref, events.watch());
});
