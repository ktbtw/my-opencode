import 'dart:async';
import 'dart:io';

import 'package:chat_codex_app/core/notifications/app_notification_lifecycle.dart';
import 'package:chat_codex_app/core/notifications/app_notification_sync.dart';
import 'package:chat_codex_app/core/services/update_log_service.dart';
import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/settings/settings_provider.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _UnusedGateway extends Fake implements AppNotificationRemoteGateway {}

void main() {
  final binding = TestWidgetsFlutterBinding.ensureInitialized();
  const channel = MethodChannel('chat_codex/global_overlay');
  late Directory directory;
  late ProviderContainer container;
  late AppNotificationLifecycleManager manager;
  late List<MethodCall> calls;
  bool startResult = true;
  Completer<bool>? pendingStart;

  setUp(() async {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    directory = await Directory.systemTemp.createTemp(
      'notification-lifecycle-',
    );
    final log = File('${directory.path}/events.log');
    await log.writeAsString('');
    SharedPreferences.setMockInitialValues({
      'app_current_log_path': log.path,
      'app_log_batch_id': 'notification-test',
    });
    await AppStorage.init();
    calls = [];
    startResult = true;
    pendingStart = null;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call);
          return pendingStart == null
              ? startResult
              : await pendingStart!.future;
        });
    container = ProviderContainer(
      overrides: [
        appNotificationSyncServiceProvider.overrideWithValue(
          AppNotificationSyncService(gateway: _UnusedGateway()),
        ),
      ],
    );
    container.read(settingsProvider);
    await Future<void>.delayed(Duration.zero);
    manager = container.read(appNotificationLifecycleManagerProvider);
  });

  tearDown(() async {
    container.dispose();
    await UpdateLogService.list();
    await directory.delete(recursive: true);
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
    debugDefaultTargetPlatformOverride = null;
  });

  test(
    'hidden starts native notifications once; inactive stays visible',
    () async {
      manager.didChangeAppLifecycleState(AppLifecycleState.inactive);
      expect(calls, isEmpty);
      manager.didChangeAppLifecycleState(AppLifecycleState.hidden);
      manager.didChangeAppLifecycleState(AppLifecycleState.paused);
      await Future<void>.delayed(Duration.zero);
      expect(calls.map((call) => call.method), ['startTaskNotifications']);
      final entries = await UpdateLogService.list();
      expect(entries.last.event, 'background_notification_service_start');
      expect(entries.last.data['started'], isTrue);
    },
  );

  test('resume ensures native service and logs false start results', () async {
    manager.didChangeAppLifecycleState(AppLifecycleState.hidden);
    await Future<void>.delayed(Duration.zero);
    await UpdateLogService.list();
    startResult = false;
    manager.didChangeAppLifecycleState(AppLifecycleState.inactive);
    manager.didChangeAppLifecycleState(AppLifecycleState.resumed);
    await Future<void>.delayed(Duration.zero);
    expect(calls, hasLength(2));
    final entries = await UpdateLogService.list();
    expect(entries.last.data['started'], isFalse);
    expect(entries.last.level, 'warning');
  });

  test(
    'rapid lifecycle changes do not duplicate an in-flight native start',
    () async {
      pendingStart = Completer<bool>();
      manager.didChangeAppLifecycleState(AppLifecycleState.hidden);
      manager.didChangeAppLifecycleState(AppLifecycleState.inactive);
      manager.didChangeAppLifecycleState(AppLifecycleState.hidden);
      await Future<void>.delayed(Duration.zero);
      expect(calls, hasLength(1));
      pendingStart!.complete(true);
      await Future<void>.delayed(Duration.zero);
      await UpdateLogService.list();
    },
  );

  test(
    'disabled preference and disposed manager do not start the service',
    () async {
      await container
          .read(settingsProvider.notifier)
          .setBackgroundNotificationEnabled(false);
      calls
          .clear(); // The explicit setting change sends its own native command.
      manager.didChangeAppLifecycleState(AppLifecycleState.hidden);
      await Future<void>.delayed(Duration.zero);
      expect(calls, isEmpty);
      manager.dispose();
      manager.didChangeAppLifecycleState(AppLifecycleState.resumed);
      expect(calls, isEmpty);
    },
  );
}
