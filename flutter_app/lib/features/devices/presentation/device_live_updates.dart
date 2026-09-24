import 'dart:async';
import 'dart:convert';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/services/app_live_events.dart';
import '../../chat/presentation/chat_provider.dart' show sessionListProvider;
import 'device_provider.dart';

/// Lifecycle changes invalidate only small REST snapshots, at most once per
/// window. Token deltas never enter this stream. Reconnect snapshots reconcile
/// everything visible, including changes missed while the app was backgrounded.
final deviceLiveUpdatesProvider = Provider<void>((ref) {
  Timer? refresh;
  var all = false;
  final machines = <String>{};
  final sessions = <(String, String)>{};
  ref.listen(appLiveStreamProvider, (_, next) {
    final event = next.valueOrNull;
    if (event == null ||
        !const {
          'snapshot',
          'agent.upsert',
          'agent.remove',
          'task.updated',
        }.contains(event.type)) {
      return;
    }
    if (event.type == 'snapshot') all = true;
    try {
      final data = jsonDecode(event.data) as Map<String, dynamic>;
      final machine = data['machine_id']?.toString() ?? '';
      if (machine.isNotEmpty) machines.add(machine);
      if (event.type == 'task.updated') {
        final agent = data['agent_id']?.toString() ?? '';
        final project = data['project_id']?.toString() ?? '';
        if (agent.isNotEmpty) sessions.add((agent, project));
      }
    } catch (_) {}
    refresh ??= Timer(const Duration(milliseconds: 250), () {
      refresh = null;
      ref.invalidate(deviceListProvider);
      if (all) {
        ref.invalidate(deviceDetailProvider);
        ref.invalidate(sessionListProvider);
      } else {
        for (final machine in machines) {
          ref.invalidate(deviceDetailProvider(machine));
        }
        for (final session in sessions) {
          ref.invalidate(sessionListProvider(session));
        }
      }
      all = false;
      machines.clear();
      sessions.clear();
    });
  });
  ref.onDispose(() => refresh?.cancel());
});
