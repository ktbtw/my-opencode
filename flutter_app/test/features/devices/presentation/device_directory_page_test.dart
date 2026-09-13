import 'dart:io';

import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/devices/data/device_directory_model.dart';
import 'package:chat_codex_app/features/devices/data/device_repository.dart';
import 'package:chat_codex_app/features/devices/presentation/device_directory_page.dart';
import 'package:chat_codex_app/features/devices/presentation/device_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('device directory supports create upload and recursive delete', (
    tester,
  ) async {
    final logFile = File(
      '${Directory.systemTemp.path}/device_directory_page_test.log',
    );
    logFile.writeAsStringSync('');
    addTearDown(() {
      if (logFile.existsSync()) {
        logFile.deleteSync();
      }
    });
    SharedPreferences.setMockInitialValues({
      'app_log_batch_id': 'test-batch',
      'app_current_log_path': logFile.path,
      'feature_guide_agent_create_v1': true,
    });
    await tester.runAsync(AppStorage.init);
    final repository = _DirectoryRepository();

    await tester.pumpWidget(
      ProviderScope(
        overrides: [deviceRepositoryProvider.overrideWithValue(repository)],
        child: const MaterialApp(
          home: DeviceDirectoryPage(machineId: 'machine-a'),
        ),
      ),
    );
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 100)),
    );
    await tester.pump();

    expect(find.byTooltip('上传文件'), findsOneWidget);
    expect(find.byTooltip('新建'), findsOneWidget);
    expect(repository.loads, 1);
    expect(find.text('notes.txt'), findsOneWidget);
    expect(find.text('docs'), findsOneWidget);

    await tester.longPress(find.text('notes.txt'));
    await tester.pump();
    expect(find.text('已选择 1 项'), findsOneWidget);

    await tester.tap(find.text('docs'));
    await tester.pump();
    expect(find.text('已选择 2 项'), findsOneWidget);

    await tester.tap(find.byTooltip('删除'));
    await tester.pump();
    expect(find.textContaining('递归删除 2 项'), findsOneWidget);
    await tester.tap(find.widgetWithText(FilledButton, '删除'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));

    expect(
      repository.deleted,
      unorderedEquals(['/workspace/notes.txt', '/workspace/docs']),
    );
  });
}

class _DirectoryRepository extends DeviceRepository {
  final List<String> deleted = [];
  int loads = 0;

  @override
  Future<DeviceDirectoryResult> getDeviceDirectories({
    required String machineId,
    String path = '',
  }) async {
    loads += 1;
    return const DeviceDirectoryResult(
      currentPath: '/workspace',
      parentPath: '/',
      entries: [
        DeviceDirectoryInfo(
          path: '/workspace/docs',
          name: 'docs',
          kind: '目录',
          isDir: true,
        ),
        DeviceDirectoryInfo(
          path: '/workspace/notes.txt',
          name: 'notes.txt',
          kind: '文件',
          isDir: false,
        ),
      ],
    );
  }

  @override
  Future<void> deleteDeviceDirectoryFile({
    required String machineId,
    required String path,
    required bool isDir,
  }) async {
    deleted.add(path);
  }
}
