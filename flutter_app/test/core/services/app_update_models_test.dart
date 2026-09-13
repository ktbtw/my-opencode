import 'package:chat_codex_app/core/services/app_update_models.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('selects the Windows installer for the Windows platform', () {
    final info = AppUpdateInfo.fromJson({
      'version': '1.6.1',
      'version_code': 148,
      'changelog': 'update',
      'downloads': [
        {
          'platform': 'android',
          'filename': 'chat-codex.apk',
          'url': '/api/app/download',
          'sha256': List.filled(64, 'a').join(),
          'available': true,
        },
        {
          'platform': 'windows-x64',
          'filename': 'chat-codex-windows-x64-setup.exe',
          'url': '/api/app/download?artifact=chat-codex-windows-x64-setup.exe',
          'sha256': List.filled(64, 'b').join(),
          'available': true,
        },
      ],
    }, platform: 'windows-x64');

    expect(info.platform, 'windows-x64');
    expect(info.hasInstallAsset, isTrue);
    expect(info.asset?.filename, 'chat-codex-windows-x64-setup.exe');
    expect(info.downloadUrl, contains('chat-codex-windows-x64-setup.exe'));
  });

  test('does not fall back to the Android APK on Windows', () {
    final info = AppUpdateInfo.fromJson({
      'version': '1.6.1',
      'version_code': '148',
      'download_url': '/api/app/download',
      'downloads': [
        {
          'platform': 'android',
          'filename': 'chat-codex.apk',
          'url': '/api/app/download',
          'available': true,
        },
      ],
    }, platform: 'windows-x64');

    expect(info.versionCode, 148);
    expect(info.hasInstallAsset, isFalse);
    expect(info.asset, isNull);
  });

  test('selects the macOS disk image for Apple Silicon', () {
    final info = AppUpdateInfo.fromJson({
      'version': '1.6.2',
      'version_code': 149,
      'downloads': [
        {
          'platform': 'darwin-arm64',
          'filename': 'chat-codex-darwin-arm64.dmg',
          'url': '/api/app/download?artifact=chat-codex-darwin-arm64.dmg',
          'sha256': List.filled(64, 'c').join(),
          'available': true,
        },
      ],
    }, platform: 'darwin-arm64');

    expect(info.platform, 'darwin-arm64');
    expect(info.hasInstallAsset, isTrue);
    expect(info.asset?.filename, 'chat-codex-darwin-arm64.dmg');
  });

  test('keeps per-platform version_code on merged desktop assets', () {
    final info = AppUpdateInfo.fromJson({
      'version': '1.6.61',
      'version_code': 208,
      'downloads': [
        {
          'platform': 'windows-x64',
          'filename': 'chat-codex-windows-x64-setup.exe',
          'url': '/api/app/download?artifact=chat-codex-windows-x64-setup.exe',
          'sha256': List.filled(64, 'b').join(),
          'available': true,
          'version_code': 207,
        },
      ],
    }, platform: 'windows-x64');

    expect(info.versionCode, 208);
    expect(info.asset?.versionCode, 207);
    expect(info.hasInstallAsset, isTrue);
  });
}
