import 'dart:io';

import 'package:chat_codex_app/core/services/app_foreground.dart';
import 'package:chat_codex_app/core/services/update_log_service.dart';
import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'inactive remains visible while hidden/paused/detached are background',
    () {
      expect(isForegroundLifecycleState(null), isTrue);
      expect(isForegroundLifecycleState(AppLifecycleState.resumed), isTrue);
      expect(isForegroundLifecycleState(AppLifecycleState.inactive), isTrue);
      expect(isForegroundLifecycleState(AppLifecycleState.hidden), isFalse);
      expect(isForegroundLifecycleState(AppLifecycleState.paused), isFalse);
      expect(isForegroundLifecycleState(AppLifecycleState.detached), isFalse);
    },
  );

  test(
    'both foreground directions are observable and saved in app logs',
    () async {
      final directory = await Directory.systemTemp.createTemp('lifecycle-log-');
      final logFile = File('${directory.path}/lifecycle.log');
      await logFile.writeAsString('');
      SharedPreferences.setMockInitialValues({
        'app_current_log_path': logFile.path,
        'app_log_batch_id': 'lifecycle-test',
      });
      await AppStorage.init();
      final foreground = AppForeground();
      addTearDown(() async {
        foreground.dispose();
        await UpdateLogService.list();
        await directory.delete(recursive: true);
      });
      foreground.didChangeAppLifecycleState(AppLifecycleState.resumed);
      foreground.didChangeAppLifecycleState(AppLifecycleState.inactive);
      expect(foreground.state, isTrue);
      foreground.didChangeAppLifecycleState(AppLifecycleState.hidden);
      foreground.didChangeAppLifecycleState(AppLifecycleState.paused);
      expect(foreground.state, isFalse);
      foreground.didChangeAppLifecycleState(AppLifecycleState.hidden);
      foreground.didChangeAppLifecycleState(AppLifecycleState.inactive);
      foreground.didChangeAppLifecycleState(AppLifecycleState.resumed);
      expect(foreground.state, isTrue);

      // Reads drain queued writes, including concurrent chat/network logs.
      await UpdateLogService.list();
      final content = await logFile.readAsString();
      expect(content, contains('app_lifecycle_changed'));
      expect(content, contains('前台 -> 后台'));
      expect(content, contains('后台 -> 前台'));
      expect(content, contains('"state":"resumed"'));
    },
  );

  test('concurrent diagnostic appends retain every complete record', () async {
    final directory = await Directory.systemTemp.createTemp('concurrent-log-');
    final logFile = File('${directory.path}/concurrent.log');
    await logFile.writeAsString('');
    SharedPreferences.setMockInitialValues({
      'app_current_log_path': logFile.path,
      'app_log_batch_id': 'concurrent-test',
    });
    await AppStorage.init();
    addTearDown(() => directory.delete(recursive: true));
    await Future.wait([
      for (var index = 0; index < 40; index++)
        UpdateLogService.append('concurrent_event', data: {'index': index}),
    ]);
    final entries = await UpdateLogService.list();
    expect(entries, hasLength(40));
    expect(
      entries.map((entry) => entry.data['index']),
      List.generate(40, (i) => i),
    );
  });

  test('a failed log operation reports its error and releases the write queue', () async {
    final directory = await Directory.systemTemp.createTemp('failed-log-');
    final logFile = File('${directory.path}/after-failure.log');
    await logFile.writeAsString('');
    SharedPreferences.setMockInitialValues({
      'app_current_log_path': logFile.path,
      'app_log_batch_id': 'failure-test',
    });
    await AppStorage.init();
    addTearDown(() => directory.delete(recursive: true));
    // PackageInfo is intentionally not installed in this unit-test fixture.
    await expectLater(UpdateLogService.startNewBatch(reason: 'fixture'), throwsException);
    await UpdateLogService.append('after_failure');
    expect((await UpdateLogService.list()).single.event, 'after_failure');
  });
}
