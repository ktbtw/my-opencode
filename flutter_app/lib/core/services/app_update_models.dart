import 'package:flutter/foundation.dart';

class AppUpdateAsset {
  final String platform;
  final String filename;
  final String url;
  final String sha256;
  final bool available;
  final int versionCode;

  const AppUpdateAsset({
    required this.platform,
    required this.filename,
    required this.url,
    required this.sha256,
    required this.available,
    this.versionCode = 0,
  });

  factory AppUpdateAsset.fromJson(Map<String, dynamic> json) {
    final rawVersionCode = json['version_code'];
    final versionCode = switch (rawVersionCode) {
      int value => value,
      String value => int.tryParse(value.trim()) ?? 0,
      _ => 0,
    };
    return AppUpdateAsset(
      platform: (json['platform'] as String? ?? '').trim(),
      filename: (json['filename'] as String? ?? '').trim(),
      url: (json['url'] as String? ?? '').trim(),
      sha256: (json['sha256'] as String? ?? '').trim(),
      available: json['available'] as bool? ?? true,
      versionCode: versionCode,
    );
  }
}

class AppReleaseInfo {
  final String version;
  final int versionCode;
  final String releasedAt;
  final List<String> items;

  const AppReleaseInfo({
    required this.version,
    required this.versionCode,
    required this.releasedAt,
    required this.items,
  });

  String get versionLabel => '$version+$versionCode';

  factory AppReleaseInfo.fromJson(Map<String, dynamic> json) {
    final rawVersionCode = json['version_code'];
    final versionCode = switch (rawVersionCode) {
      int value => value,
      String value => int.tryParse(value.trim()) ?? 0,
      _ => 0,
    };
    final items = (json['items'] as List<dynamic>? ?? const [])
        .whereType<String>()
        .map((item) => item.trim())
        .where((item) => item.isNotEmpty)
        .toList(growable: false);
    return AppReleaseInfo(
      version: (json['version'] as String? ?? '').trim(),
      versionCode: versionCode,
      releasedAt: (json['released_at'] as String? ?? '').trim(),
      items: items,
    );
  }
}

class AppUpdateInfo {
  final String version;
  final int versionCode;
  final String changelog;
  final List<AppReleaseInfo> releases;
  final String platform;
  final AppUpdateAsset? asset;

  const AppUpdateInfo({
    required this.version,
    required this.versionCode,
    required this.changelog,
    required this.releases,
    required this.platform,
    required this.asset,
  });

  String get downloadUrl => asset?.url ?? '';
  bool get hasInstallAsset =>
      asset != null &&
      asset!.available &&
      asset!.url.isNotEmpty &&
      asset!.filename.isNotEmpty;

  factory AppUpdateInfo.fromJson(
    Map<String, dynamic> json, {
    String? platform,
  }) {
    final selectedPlatform = (platform ?? currentAppUpdatePlatform()).trim();
    final rawVersionCode = json['version_code'];
    final versionCode = switch (rawVersionCode) {
      int value => value,
      String value => int.tryParse(value.trim()) ?? 0,
      _ => 0,
    };
    final downloads = (json['downloads'] as List<dynamic>? ?? const [])
        .whereType<Map<String, dynamic>>()
        .map(AppUpdateAsset.fromJson)
        .toList();
    AppUpdateAsset? selected;
    for (final item in downloads) {
      if (item.platform == selectedPlatform) {
        selected = item;
        break;
      }
    }

    // Older servers only published the Android APK through download_url.
    if (selected == null && selectedPlatform == 'android') {
      final legacyURL = (json['download_url'] as String? ?? '').trim();
      if (legacyURL.isNotEmpty) {
        selected = AppUpdateAsset(
          platform: 'android',
          filename: 'chat-codex.apk',
          url: legacyURL,
          sha256: '',
          available: true,
        );
      }
    }
    final parsedReleases = (json['releases'] as List<dynamic>? ?? const [])
        .whereType<Map<String, dynamic>>()
        .map(AppReleaseInfo.fromJson)
        .where((release) => release.version.isNotEmpty)
        .toList(growable: false);
    final releases = parsedReleases.isNotEmpty
        ? parsedReleases
        : [
            AppReleaseInfo(
              version: json['version'] as String? ?? '',
              versionCode: versionCode,
              releasedAt: '',
              items: _changelogItems(json['changelog'] as String? ?? ''),
            ),
          ];
    return AppUpdateInfo(
      version: json['version'] as String? ?? '',
      versionCode: versionCode,
      changelog: json['changelog'] as String? ?? '',
      releases: releases,
      platform: selectedPlatform,
      asset: selected,
    );
  }
}

List<String> _changelogItems(String changelog) {
  return changelog
      .split('\n')
      .map((line) => line.trim())
      .where((line) => line.isNotEmpty)
      .map((line) => line.startsWith('- ') ? line.substring(2).trim() : line)
      .toList(growable: false);
}

String currentAppUpdatePlatform() {
  if (kIsWeb) return 'web';
  return switch (defaultTargetPlatform) {
    TargetPlatform.android => 'android',
    TargetPlatform.windows => 'windows-x64',
    TargetPlatform.macOS => 'darwin-arm64',
    TargetPlatform.linux => 'linux-x64',
    TargetPlatform.iOS => 'ios',
    TargetPlatform.fuchsia => 'fuchsia',
  };
}
