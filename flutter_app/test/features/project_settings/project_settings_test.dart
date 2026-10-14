import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/project_settings/project_settings_provider.dart';
import 'package:chat_codex_app/features/project_settings/presentation/project_settings_page.dart';
import 'package:chat_codex_app/features/settings/settings_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() async {
    SharedPreferences.setMockInitialValues({'operator_key': 'account-a'});
    await AppStorage.init();
  });

  test('project/account/server isolation and cold start persistence', () async {
    final a = ProjectSettingsNotifier('server/account-a', 'project-a');
    await a.update(
      showConnectionStatus: false,
      directory: 'content://downloads/tree/project-a',
      label: 'Downloads/project-a',
    );
    a.dispose();
    final restored = ProjectSettingsNotifier('server/account-a', 'project-a');
    await restored.ready;
    expect(restored.state.showConnectionStatus, isFalse);
    expect(restored.state.directoryLabel, 'Downloads/project-a');
    for (final pair in [
      ('server/account-a', 'project-b'),
      ('server/account-b', 'project-a'),
      ('other-server/account-a', 'project-a'),
    ]) {
      final other = ProjectSettingsNotifier(pair.$1, pair.$2);
      await other.ready;
      expect(other.state.downloadDirectory, isNull);
      expect(other.state.showConnectionStatus, isTrue);
      other.dispose();
    }
    await restored.update(resetDirectory: true);
    expect(restored.state.downloadDirectory, isNull);
    expect(restored.state.showConnectionStatus, isFalse);
    restored.dispose();
  });

  test('updates during loading serialize and keep unrelated fields', () async {
    final notifier = ProjectSettingsNotifier('account', 'project');
    await Future.wait([
      notifier.update(showConnectionStatus: false),
      notifier.update(directory: '/downloads/a', label: '/downloads/a'),
      notifier.update(showConnectionStatus: true),
    ]);
    expect(notifier.state.showConnectionStatus, isTrue);
    expect(notifier.state.downloadDirectory, '/downloads/a');
    notifier.dispose();
    final restored = ProjectSettingsNotifier('account', 'project');
    await restored.ready;
    expect(restored.state.downloadDirectory, '/downloads/a');
    restored.dispose();
  });

  testWidgets(
    'project page saves toggle and Android tree selection, reset follows global',
    (tester) async {
      const channel = MethodChannel('chat_codex/artifact_directory');
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(channel, (
        call,
      ) async {
        expect(call.method, 'pickDirectory');
        return {
          'uri': 'content://downloads/tree/project',
          'label': 'Download/造梦西游',
        };
      });
      addTearDown(
        () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          channel,
          null,
        ),
      );
      final container = ProviderContainer(
        overrides: [
          settingsProvider.overrideWith(
            (ref) => SettingsNotifier(
              readRemote: () async => {},
              saveRemote: (_) async {},
            ),
          ),
        ],
      );
      addTearDown(container.dispose);
      final keep = container.listen(
        projectSettingsProvider('project'),
        (_, _) {},
      );
      addTearDown(keep.close);
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: const MaterialApp(
            home: ProjectSettingsPage(project: 'project', projectName: '造梦西游'),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();
      expect(
        container.read(projectSettingsProvider('project')).showConnectionStatus,
        isFalse,
      );
      await tester.tap(find.text('交付文件下载目录'));
      await tester.pumpAndSettle();
      expect(find.text('Download/造梦西游'), findsOneWidget);
      expect(
        container.read(projectSettingsProvider('project')).downloadDirectory,
        'content://downloads/tree/project',
      );
      await tester.tap(find.text('恢复为全局下载目录'));
      await tester.pumpAndSettle();
      expect(
        container.read(projectSettingsProvider('project')).downloadDirectory,
        isNull,
      );
      expect(
        container.read(projectSettingsProvider('project')).showConnectionStatus,
        isFalse,
      );
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );
}
