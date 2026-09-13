import 'package:package_info_plus/package_info_plus.dart';

import 'app_version_models.dart';

Future<AppVersionInfo> loadAppVersionInfo() async {
  try {
    final info = await PackageInfo.fromPlatform();
    return AppVersionInfo(
      appName: info.appName,
      packageName: info.packageName,
      version: info.version,
      buildNumber: info.buildNumber,
    );
  } catch (_) {
    return const AppVersionInfo(
      appName: 'Chat Codex',
      packageName: 'chat_codex_app',
      version: 'unknown',
      buildNumber: '0',
    );
  }
}
