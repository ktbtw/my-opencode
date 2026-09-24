import 'dart:async';
import 'package:chat_codex_app/features/chat/data/local_chat_store.dart';
import 'package:chat_codex_app/features/chat/data/local_event_writer.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'writes bounded transactions in order and flushes before close',
    () async {
      final store = _Store();
      final writer = LocalEventWriter(store);
      for (var i = 0; i < 1000; i++) {
        writer.add(LocalChatEvent('account', 'task', '$i', {'sequence': i}));
      }
      await writer.close();
      expect(store.batchSizes, everyElement(lessThanOrEqualTo(128)));
      expect(store.batchSizes, hasLength(8));
      expect(
        store.events.map((e) => e.eventKey),
        List.generate(1000, (i) => '$i'),
      );
      expect(store.closed, isTrue);
    },
  );

  test(
    'slow storage has a bounded cache backlog and retains account identity',
    () async {
      final store = _Store()..ready = Completer<void>();
      final writer = LocalEventWriter(store);
      writer.add(const LocalChatEvent('old', 'task', 'old', {}));
      final first = writer.flush();
      for (var i = 0; i < 5000; i++) {
        writer.add(LocalChatEvent('new', 'task', '$i', {}));
      }
      store.ready!.complete();
      await first;
      await writer.close();
      expect(store.events.length, 4096);
      expect(store.events.first.eventKey, '904');
      expect(store.events.last.eventKey, '4999');
      expect(store.events.every((e) => e.identity == 'new'), isTrue);
    },
  );
}

class _Store implements LocalChatStore, BulkLocalChatStore {
  final events = <LocalChatEvent>[];
  final batchSizes = <int>[];
  Completer<void>? ready;
  bool closed = false;
  @override
  Future<void> init() async {
    await ready?.future;
  }

  @override
  Future<void> recordEvents(List<LocalChatEvent> batch) async {
    expect(closed, isFalse);
    batchSizes.add(batch.length);
    events.addAll(batch);
  }

  @override
  Future<void> close() async {
    closed = true;
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}
