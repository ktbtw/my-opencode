import 'dart:convert';

import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/project_settings/presentation/project_settings_page.dart';
import 'package:chat_codex_app/features/project_settings/project_settings_provider.dart';
import 'package:chat_codex_app/features/settings/settings_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<void> pumpPage(
    WidgetTester tester, {
    String? directory,
    Size size = const Size(390, 844),
    String projectName = 'chat-codex 工具链',
    double textScale = 1,
  }) async {
    SharedPreferences.setMockInitialValues({});
    await AppStorage.init();
    if (directory != null) {
      final key = ProjectSettings.storageKey(
        AppStorage.storageIdentity,
        'chat-codex',
      );
      await AppStorage.setString(
        key,
        jsonEncode({
          'show_connection': true,
          'download_directory': directory,
          'directory_label': directory,
        }),
      );
    }
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          settingsProvider.overrideWith(
            (ref) => SettingsNotifier(
              readRemote: () async => {},
              saveRemote: (_) async {},
            ),
          ),
        ],
        child: MaterialApp(
          home: MediaQuery(
            data: MediaQueryData(textScaler: TextScaler.linear(textScale)),
            child: ProjectSettingsPage(
              project: 'chat-codex',
              projectName: projectName,
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('项目设置页用分区卡片呈现，页头说明作用范围', (tester) async {
    await pumpPage(tester);

    expect(find.text('项目设置'), findsOneWidget);
    expect(find.text('chat-codex 工具链'), findsOneWidget);
    expect(find.text('当前项目'), findsOneWidget);
    expect(find.text('仅应用于当前账号在本设备上的这个项目'), findsOneWidget);
    expect(find.text('对话显示'), findsOneWidget);
    expect(find.text('显示连接状态'), findsOneWidget);
    expect(find.text('文件与下载'), findsOneWidget);
    expect(find.text('交付文件下载目录'), findsOneWidget);
    // 未覆盖时沿用全局：动作是"选择"，且不显示恢复入口。
    expect(find.text('选择'), findsOneWidget);
    expect(find.text('恢复为全局下载目录'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('已设置项目目录时显示路径与恢复入口', (tester) async {
    await pumpPage(tester, directory: '/Users/ktbtw/Downloads/交付文件');

    expect(find.text('/Users/ktbtw/Downloads/交付文件'), findsOneWidget);
    expect(find.text('更改'), findsOneWidget);
    expect(find.text('恢复为全局下载目录'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('窄屏与放大字号下不溢出', (tester) async {
    for (final size in [const Size(320, 720), const Size(390, 844)]) {
      for (final scale in [1.0, 1.6]) {
        await pumpPage(
          tester,
          size: size,
          projectName: '一个特别长的项目名字用来验证截断行为',
          textScale: scale,
        );
        expect(tester.takeException(), isNull);
      }
    }
  });
}
