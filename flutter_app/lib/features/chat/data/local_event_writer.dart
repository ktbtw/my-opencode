import 'dart:async';
import 'dart:collection';
import 'package:flutter/foundation.dart';
import 'local_chat_store.dart';

/// Local storage is a cache. Bound its backlog independently of live rendering,
/// and write small transactions instead of fsync + SELECT for every token.
class LocalEventWriter {
  final LocalChatStore store;
  final _pending = Queue<({LocalChatEvent event, int bytes})>();
  int _pendingBytes = 0;
  static const _maxBytes = 4 * 1024 * 1024;
  Timer? _timer;
  Future<void>? _writing;
  bool _closed = false;
  LocalEventWriter(this.store);

  void add(LocalChatEvent event) {
    if (_closed) return;
    final bytes = _payloadBytes(event.payload);
    if (bytes > _maxBytes) return;
    while (_pending.isNotEmpty &&
        (_pending.length >= 4096 || _pendingBytes + bytes > _maxBytes)) {
      _pendingBytes -= _pending.removeFirst().bytes;
    }
    _pending.add((event: event, bytes: bytes));
    _pendingBytes += bytes;
    _timer ??= Timer(const Duration(milliseconds: 100), () {
      _timer = null;
      unawaited(flush());
    });
  }

  Future<void> flush() async {
    _timer?.cancel();
    _timer = null;
    if (_writing != null) {
      await _writing;
      if (_pending.isNotEmpty) await flush();
      return;
    }
    if (_pending.isEmpty) return;
    final work = _drain();
    _writing = work;
    try {
      await work;
    } finally {
      _writing = null;
    }
  }

  Future<void> _drain() async {
    try {
      await store.init();
      while (_pending.isNotEmpty) {
        final batch = <LocalChatEvent>[];
        while (_pending.isNotEmpty && batch.length < 128) {
          final next = _pending.removeFirst();
          _pendingBytes -= next.bytes;
          batch.add(next.event);
        }
        final target = store;
        if (target is BulkLocalChatStore) {
          await (target as BulkLocalChatStore).recordEvents(batch);
        } else {
          for (final event in batch) {
            await store.recordEvent(
              identity: event.identity,
              taskId: event.taskId,
              eventKey: event.eventKey,
              payload: event.payload,
            );
          }
        }
        // Give frames and input a turn even during a large history replay.
        await Future<void>.delayed(Duration.zero);
      }
    } catch (error) {
      _pending.clear();
      _pendingBytes = 0;
      debugPrint('[Chat] local event cache write failed: $error');
    }
  }

  Future<void> close() async {
    _closed = true;
    await flush();
    await store.close();
  }
}

int _payloadBytes(Object? value) {
  if (value is String) return value.length * 2 + 32;
  if (value is Map) {
    return 64 +
        value.entries.fold<int>(
          0,
          (size, entry) =>
              size + _payloadBytes(entry.key) + _payloadBytes(entry.value),
        );
  }
  if (value is List) {
    return 32 + value.fold<int>(0, (size, item) => size + _payloadBytes(item));
  }
  return 16;
}
