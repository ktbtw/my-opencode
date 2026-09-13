import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/core/theme/app_theme.dart';
import 'package:chat_codex_app/features/guide/presentation/guide_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('shows the Windows desktop installer and GUI Launcher', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    await AppStorage.init();

    tester.view.physicalSize = const Size(1280, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light,
        home: GuidePage(
          loadJson: (path) async {
            if (path == '/api/launcher/downloads') {
              return {
                'version': '0.1.134',
                'downloads': [
                  {
                    'id': 'chat-codex-launcher-windows-x64',
                    'title': 'Windows x64 GUI Launcher',
                    'platform': 'windows-x64',
                    'kind': 'launcher',
                    'filename': 'chat-codex-launcher-windows-x64.zip',
                    'url':
                        '/api/launcher/download?artifact=chat-codex-launcher-windows-x64.zip',
                    'available': true,
                  },
                ],
              };
            }
            return {
              'version': '1.6.15',
              'downloads': [
                {
                  'id': 'windows-x64',
                  'title': 'Windows x64 安装程序',
                  'platform': 'windows-x64',
                  'kind': 'desktop',
                  'filename': 'chat-codex-windows-x64-setup.exe',
                  'url':
                      'https://downloads.example.test/chat-codex-windows-x64-setup.exe',
                  'available': true,
                },
                {
                  'id': 'android-apk',
                  'title': 'Android APK',
                  'platform': 'android',
                  'kind': 'mobile',
                  'filename': 'chat-codex.apk',
                  'url': '/api/app/download',
                  'available': true,
                },
              ],
            };
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('chat-codex-windows-x64-setup.exe'), findsOneWidget);
    expect(find.text('chat-codex-launcher-windows-x64.zip'), findsOneWidget);
    expect(find.text('chat-codex.apk'), findsOneWidget);
    expect(find.text('0.1.134'), findsOneWidget);
    expect(find.text('1.6.15'), findsOneWidget);
    expect(find.text('Launcher'), findsOneWidget);
    expect(find.text('对话操作程序'), findsOneWidget);
  });
}
