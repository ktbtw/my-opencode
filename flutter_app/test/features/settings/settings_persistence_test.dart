import 'dart:async';
import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/settings/settings_provider.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

Future<void> settle() async {
  for (var i = 0; i < 20; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() async {
    SharedPreferences.setMockInitialValues({'operator_key': 'account-a'});
    await AppStorage.init();
  });

  test(
    'continuous reply defaults off and survives an early toggle and restart',
    () async {
      final remote = Completer<Map<String, dynamic>>();
      final notifier = SettingsNotifier(
        readRemote: () => remote.future,
        saveRemote: (_) async {},
      );
      expect(notifier.state.continuousReply, isFalse);
      await notifier.toggle('continuousReply');
      remote.complete({});
      await settle();
      expect(notifier.state.continuousReply, isTrue);
      notifier.dispose();
      final restored = SettingsNotifier(
        readRemote: () async => {},
        saveRemote: (_) async {},
      );
      await settle();
      expect(restored.state.continuousReply, isTrue);
      await restored.toggle('continuousReply');
      expect(
        (await SharedPreferences.getInstance()).getBool('continuousReply'),
        isFalse,
      );
      restored.dispose();
    },
  );

  test(
    'late GET does not undo a saved local toggle, including after restart',
    () async {
      final remote = Completer<Map<String, dynamic>>();
      final notifier = SettingsNotifier(
        readRemote: () => remote.future,
        saveRemote: (_) async {},
      );
      await notifier.toggle('showTokenCount');
      remote.complete({'show_token_count': 'false'});
      await settle();
      expect(notifier.state.showTokenCount, isTrue);
      notifier.dispose();
      final restored = SettingsNotifier(
        readRemote: () async => {},
        saveRemote: (_) async {},
      );
      await settle();
      expect(restored.state.showTokenCount, isTrue);
      restored.dispose();
    },
  );

  test('rapid toggles serialize POSTs and retain other pending keys', () async {
    final gate = Completer<void>();
    final writes = <Map<String, dynamic>>[];
    var concurrent = 0;
    var maxConcurrent = 0;
    final notifier = SettingsNotifier(
      readRemote: () async => {},
      saveRemote: (values) async {
        concurrent++;
        if (concurrent > maxConcurrent) maxConcurrent = concurrent;
        writes.add(Map.from(values));
        if (writes.length == 1) await gate.future;
        concurrent--;
      },
    );
    final first = notifier.toggle('showTokenCount');
    await settle();
    final second = notifier.toggle('showTokenCount');
    final third = notifier.toggle('showWordCount');
    await settle();
    expect(notifier.state.showTokenCount, isFalse);
    expect(notifier.state.showWordCount, isTrue);
    gate.complete();
    await Future.wait([first, second, third]);
    expect(maxConcurrent, 1);
    expect(writes.last, {
      'show_token_count': 'false',
      'show_word_count': 'true',
    });
    notifier.dispose();
  });

  test(
    'offline outbox survives restart and retries even when GET fails',
    () async {
      final first = SettingsNotifier(
        readRemote: () async => {},
        saveRemote: (_) async => throw Exception('offline'),
      );
      await first.toggle('showFirstTokenTime');
      await first.toggle('showWordCount');
      first.dispose();
      final writes = <Map<String, dynamic>>[];
      final second = SettingsNotifier(
        readRemote: () async => throw Exception('GET offline'),
        saveRemote: (v) async {
          writes.add(v);
        },
      );
      await settle();
      expect(second.state.showFirstTokenTime, isTrue);
      expect(second.state.showWordCount, isTrue);
      expect(writes.single, {
        'show_first_token_time': 'true',
        'show_word_count': 'true',
      });
      second.dispose();
    },
  );

  test('account switch isolates old requests, cache and outbox', () async {
    final oldGet = Completer<Map<String, dynamic>>();
    final old = SettingsNotifier(
      readRemote: () => oldGet.future,
      saveRemote: (_) async => throw Exception('offline'),
    );
    await old.toggle('showTokenCount');
    await AppStorage.setOperatorKey('account-b');
    old.dispose();
    final writes = <Map<String, dynamic>>[];
    final next = SettingsNotifier(
      readRemote: () async => {},
      saveRemote: (v) async {
        writes.add(v);
      },
    );
    oldGet.complete({'show_word_count': 'true'});
    await settle();
    expect(next.state.showTokenCount, isFalse);
    expect(next.state.showWordCount, isFalse);
    expect(writes, isEmpty);
    next.dispose();
    await AppStorage.setOperatorKey('account-a');
    final restored = SettingsNotifier(
      readRemote: () async => {},
      saveRemote: (_) async {},
    );
    await settle();
    expect(restored.state.showTokenCount, isTrue);
    restored.dispose();
  });

  test('legacy device settings migrate to only the first account', () async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool('showWordCount', true);
    final first = SettingsNotifier(
      readRemote: () async => {},
      saveRemote: (_) async {},
    );
    await settle();
    expect(first.state.showWordCount, isTrue);
    first.dispose();
    await AppStorage.setOperatorKey('account-b');
    final second = SettingsNotifier(
      readRemote: () async => {},
      saveRemote: (_) async {},
    );
    await settle();
    expect(second.state.showWordCount, isFalse);
    second.dispose();
  });
}
