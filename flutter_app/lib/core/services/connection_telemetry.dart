import 'dart:collection';

/// Transport observations only. Reading this object never opens a connection.
enum LinkPhase { idle, connecting, connected, reconnecting }

class ConnectionTelemetry {
  LinkPhase phase = LinkPhase.idle;
  DateTime? lastActivity;
  DateTime? lastOutput;
  final _received = Queue<(DateTime, int)>();
  final DateTime Function() now;

  ConnectionTelemetry({DateTime Function()? clock})
    : now = clock ?? DateTime.now;

  void connecting() {
    phase = LinkPhase.connecting;
    lastActivity = null;
    lastOutput = null;
    _received.clear();
  }

  void activity() {
    phase = LinkPhase.connected;
    lastActivity = now();
  }

  void disconnected({bool retry = false}) {
    phase = retry ? LinkPhase.reconnecting : LinkPhase.idle;
    _received.clear();
  }

  void output(String text) {
    if (text.isEmpty) return;
    final time = now();
    lastOutput = time;
    // Merge into one bucket per second: bounded even under heavy token traffic.
    final second = DateTime.fromMillisecondsSinceEpoch(
      time.millisecondsSinceEpoch ~/ 1000 * 1000,
    );
    final count = text.runes.length;
    if (_received.isNotEmpty && _received.last.$1 == second) {
      final previous = _received.removeLast();
      _received.add((second, previous.$2 + count));
    } else {
      _received.add((second, count));
    }
    _prune(time);
  }

  void _prune(DateTime time) {
    final cutoff = time.subtract(const Duration(seconds: 3));
    while (_received.isNotEmpty && !_received.first.$1.isAfter(cutoff)) {
      _received.removeFirst();
    }
  }

  int get charactersPerSecond {
    _prune(now());
    return (_received.fold<int>(0, (sum, item) => sum + item.$2) / 3).round();
  }

  bool get healthy =>
      phase == LinkPhase.connected &&
      lastActivity != null &&
      now().difference(lastActivity!) < const Duration(seconds: 45);
}

/// Describes the current recovery operation, separately from wire latency.
class ReplySyncProgress {
  final String phase;
  final DateTime startedAt = DateTime.now();
  DateTime? finishedAt;
  String? outcome;
  ReplySyncProgress(this.phase);
  void finish(String label) {
    finishedAt = DateTime.now();
    outcome = label;
  }

  String get description {
    final elapsed =
        (finishedAt ?? DateTime.now()).difference(startedAt).inMilliseconds /
        1000;
    return '${outcome ?? phase} · ${elapsed.toStringAsFixed(1)} 秒';
  }
}
