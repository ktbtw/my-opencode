import 'dart:io';
import 'package:chat_codex_app/features/chat/data/local_chat_store.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'concurrent init and batch replay use one SQLite store without duplicates',
    () async {
      final directory = await Directory.systemTemp.createTemp('chat-batch-');
      const channel = MethodChannel('plugins.flutter.io/path_provider');
      var initCalls = 0;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            initCalls++;
            return directory.path;
          });
      final store = createLocalChatStore();
      addTearDown(() async {
        await store.close();
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(channel, null);
        await directory.delete(recursive: true);
      });
      await Future.wait(List.generate(20, (_) => store.init()));
      expect(initCalls, 1);
      final events = List.generate(
        256,
        (i) => LocalChatEvent('account', 'task', '$i', {'sequence': i}),
      );
      await (store as BulkLocalChatStore).recordEvents(events);
      await (store as BulkLocalChatStore).recordEvents(events);
      final saved = await store.readEvents(identity: 'account', taskId: 'task');
      expect(saved.map((e) => e['sequence']), List.generate(256, (i) => i));
      expect(
        await store.readEvents(identity: 'other', taskId: 'task'),
        isEmpty,
      );
    },
  );
}
