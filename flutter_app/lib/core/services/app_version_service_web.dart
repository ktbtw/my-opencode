import 'dart:convert';

import 'package:http/http.dart' as http;

import 'app_version_models.dart';

Future<AppVersionInfo> loadAppVersionInfo() async {
  try {
    final uri = Uri.base.resolve('version.json');
    final resp = await http.get(uri);
    if (resp.statusCode >= 200 && resp.statusCode < 300) {
      final json = jsonDecode(resp.body) as Map<String, dynamic>;
      return AppVersionInfo(
        appName: json['app_name'] as String? ?? 'Chat Codex',
        packageName: json['package_name'] as String? ?? 'chat_codex_app',
        version: json['version'] as String? ?? 'unknown',
        buildNumber: json['build_number'] as String? ?? '0',
      );
    }
  } catch (_) {}
  return const AppVersionInfo(
    appName: 'Chat Codex',
    packageName: 'chat_codex_app',
    version: 'unknown',
    buildNumber: '0',
  );
}
