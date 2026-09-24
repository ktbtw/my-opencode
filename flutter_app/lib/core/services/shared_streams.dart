import 'dart:async';

/// One upstream per key. Consumers own only their subscription; the last
/// cancellation releases the socket (including a quiet SSE connection).
class SharedStreams<T> {
  final _entries = <String, _SharedEntry<T>>{};
  bool _disposed = false;

  Stream<T> watch(String key, Stream<T> Function() open) {
    return Stream<T>.multi((consumer) {
      if (_disposed) {
        consumer.close();
        return;
      }
      final entry = _entries.putIfAbsent(key, () => _SharedEntry<T>());
      entry.consumers.add(consumer);
      consumer.onCancel = () {
        entry.consumers.remove(consumer);
        if (entry.consumers.isEmpty && identical(_entries[key], entry)) {
          _entries.remove(key);
          return entry.source?.cancel();
        }
        return null;
      };
      if (entry.started) return;
      entry.started = true;
      void finish() {
        if (identical(_entries[key], entry)) _entries.remove(key);
        for (final listener in entry.consumers.toList()) {
          listener.close();
        }
        entry.consumers.clear();
      }

      try {
        entry.source = open().listen(
          (event) {
            for (final listener in entry.consumers.toList()) {
              listener.add(event);
            }
          },
          onError: (Object error, StackTrace stack) {
            for (final listener in entry.consumers.toList()) {
              listener.addError(error, stack);
            }
          },
          onDone: finish,
        );
      } catch (error, stack) {
        consumer.addError(error, stack);
        finish();
      }
    });
  }

  bool contains(String key) => _entries.containsKey(key);

  void dispose() {
    _disposed = true;
    for (final entry in _entries.values.toList()) {
      unawaited(entry.source?.cancel());
      for (final consumer in entry.consumers.toList()) {
        consumer.close();
      }
      entry.consumers.clear();
    }
    _entries.clear();
  }
}

class _SharedEntry<T> {
  bool started = false;
  StreamSubscription<T>? source;
  final consumers = <MultiStreamController<T>>{};
}
