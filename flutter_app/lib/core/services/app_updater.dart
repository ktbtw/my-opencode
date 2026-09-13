import 'package:flutter/foundation.dart' show kIsWeb, visibleForTesting;
import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:permission_handler/permission_handler.dart';
import '../config/api_client.dart';
import '../storage/app_storage.dart';
import 'app_navigation.dart';
import 'app_update_executor.dart';
import 'app_update_models.dart';
import 'update_log_service.dart';

export 'app_update_models.dart'
    show AppReleaseInfo, AppUpdateAsset, AppUpdateInfo;

class AppUpdateCheckResult {
  final AppUpdateInfo? info;
  final String localVersion;
  final int localBuildNumber;
  final String? error;

  const AppUpdateCheckResult({
    required this.localVersion,
    required this.localBuildNumber,
    this.info,
    this.error,
  });

  bool get hasUpdate => info != null;
  bool get failed => error != null && error!.trim().isNotEmpty;
}

class AppUpdater {
  static const Duration _dialogShowDelay = Duration(milliseconds: 320);
  static Future<AppUpdateCheckResult>? _pendingCheck;
  static bool _dialogShowing = false;
  static int? _shownVersionCode;

  static Future<AppUpdateCheckResult> checkUpdateStatus() {
    final pending = _pendingCheck;
    if (pending != null) return pending;
    final future = _doCheckUpdate();
    _pendingCheck = future;
    future.whenComplete(() {
      if (identical(_pendingCheck, future)) {
        _pendingCheck = null;
      }
    });
    return future;
  }

  static Future<AppUpdateInfo?> checkUpdate() async {
    final result = await checkUpdateStatus();
    return result.info;
  }

  static Future<AppUpdateCheckResult> _doCheckUpdate() async {
    var localVersion = '';
    var localCode = 0;
    try {
      await UpdateLogService.append(
        'check_update_started',
        data: {
          'base_url': AppStorage.getBaseUrl(),
          'logged_in': AppStorage.isLoggedIn(),
        },
      );
      final data = await ApiClient.get('/api/app/version');
      final remote = AppUpdateInfo.fromJson(data);
      final pkg = await PackageInfo.fromPlatform();
      localVersion = pkg.version;
      localCode = int.tryParse(pkg.buildNumber) ?? 0;
      final remoteCode = remote.asset != null && remote.asset!.versionCode > 0
          ? remote.asset!.versionCode
          : remote.versionCode;
      final hasUpdate = remoteCode > localCode && remote.hasInstallAsset;
      await UpdateLogService.append(
        'check_update_result',
        data: {
          'remote_version': remote.version,
          'remote_version_code': remote.versionCode,
          'local_version': localVersion,
          'local_build_number': localCode,
          'has_update': hasUpdate,
          'raw_response': data,
        },
      );
      return AppUpdateCheckResult(
        info: hasUpdate ? remote : null,
        localVersion: localVersion,
        localBuildNumber: localCode,
      );
    } catch (e) {
      if (localVersion.isEmpty) {
        try {
          final pkg = await PackageInfo.fromPlatform();
          localVersion = pkg.version;
          localCode = int.tryParse(pkg.buildNumber) ?? 0;
        } catch (_) {}
      }
      await UpdateLogService.append(
        'check_update_failed',
        level: 'error',
        data: {'error': e.toString()},
      );
      return AppUpdateCheckResult(
        localVersion: localVersion,
        localBuildNumber: localCode,
        error: e.toString(),
      );
    }
  }

  static Future<void> maybeShowUpdateDialog() async {
    if (kIsWeb) {
      await UpdateLogService.append(
        'skip_update_dialog',
        data: {'reason': 'web'},
      );
      return;
    }
    if (!AppStorage.isLoggedIn()) {
      await UpdateLogService.append(
        'skip_update_dialog',
        data: {'reason': 'not_logged_in'},
      );
      return;
    }
    final result = await checkUpdateStatus();
    final info = result.info;
    if (info == null) {
      await UpdateLogService.append(
        'skip_update_dialog',
        data: {'reason': result.failed ? 'check_failed' : 'no_update'},
      );
      return;
    }
    await showUpdateDialogForInfo(info);
  }

  static Future<bool> showUpdateDialogForInfo(
    AppUpdateInfo info, {
    bool force = false,
  }) => _showUpdateDialog(info, force: force);

  static Future<bool> _showUpdateDialog(
    AppUpdateInfo info, {
    required bool force,
  }) async {
    if (_dialogShowing) {
      await UpdateLogService.append(
        'skip_update_dialog',
        data: {'reason': 'dialog_showing'},
      );
      return false;
    }
    if (!force && _shownVersionCode == info.versionCode) {
      await UpdateLogService.append(
        'skip_update_dialog',
        data: {
          'reason': 'same_version_already_shown',
          'version_code': info.versionCode,
        },
      );
      return false;
    }

    await Future<void>.delayed(_dialogShowDelay);
    final dialogContext = await _resolveDialogContext();
    if (dialogContext == null) {
      await UpdateLogService.append(
        'skip_update_dialog',
        data: {
          'reason': 'dialog_context_unavailable',
          'version_code': info.versionCode,
        },
      );
      return false;
    }

    if (!dialogContext.mounted) {
      await UpdateLogService.append(
        'skip_update_dialog',
        data: {
          'reason': 'dialog_context_unmounted',
          'version_code': info.versionCode,
        },
      );
      return false;
    }

    _dialogShowing = true;
    try {
      final dialogFuture = showDialog<void>(
        context: dialogContext,
        barrierDismissible: false,
        useRootNavigator: true,
        builder: (ctx) => buildAppUpdateDialog(info),
      );
      _shownVersionCode = info.versionCode;
      await UpdateLogService.append(
        'show_update_dialog',
        data: {
          'version': info.version,
          'version_code': info.versionCode,
          'force': force,
        },
      );
      await dialogFuture;
      await UpdateLogService.append(
        'update_dialog_closed',
        data: {'version': info.version, 'version_code': info.versionCode},
      );
      return true;
    } catch (e) {
      await UpdateLogService.append(
        'show_update_dialog_failed',
        level: 'error',
        data: {
          'version': info.version,
          'version_code': info.versionCode,
          'error': e.toString(),
        },
      );
      return false;
    } finally {
      _dialogShowing = false;
    }
  }

  static Future<BuildContext?> _resolveDialogContext() async {
    final immediate = appNavigatorContext;
    if (immediate != null) return immediate;
    for (var attempt = 0; attempt < 5; attempt++) {
      await Future<void>.delayed(const Duration(milliseconds: 120));
      final retry = appNavigatorContext;
      if (retry != null) return retry;
    }
    return null;
  }

  static void resetSessionState() {
    _pendingCheck = null;
    _dialogShowing = false;
    _shownVersionCode = null;
  }
}

@visibleForTesting
Widget buildAppUpdateDialog(AppUpdateInfo info) => _UpdateDialog(info: info);

class _UpdateDialog extends StatefulWidget {
  final AppUpdateInfo info;
  const _UpdateDialog({required this.info});

  @override
  State<_UpdateDialog> createState() => _UpdateDialogState();
}

class _UpdateDialogState extends State<_UpdateDialog> {
  bool _downloading = false;
  double _progress = 0;
  String? _error;

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
      title: const Text(
        '发现新版本',
        style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
      ),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 420),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '版本: ${widget.info.version}+${widget.info.versionCode}',
              style: const TextStyle(fontSize: 14, color: Colors.black87),
            ),
            if (_latestRelease.items.isNotEmpty) ...[
              const SizedBox(height: 8),
              ConstrainedBox(
                constraints: const BoxConstraints(maxHeight: 220),
                child: Scrollbar(
                  thumbVisibility: _latestRelease.items.length > 6,
                  child: SingleChildScrollView(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '本次更新内容',
                          style: const TextStyle(
                            fontSize: 13,
                            fontWeight: FontWeight.w600,
                            color: Colors.black87,
                          ),
                        ),
                        const SizedBox(height: 4),
                        ..._latestRelease.items.map(
                          (item) => Padding(
                            padding: const EdgeInsets.only(bottom: 4),
                            child: Text(
                              '• $item',
                              style: const TextStyle(
                                fontSize: 13,
                                color: Colors.black54,
                                height: 1.35,
                              ),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ],
            if (_latestRelease.items.isEmpty &&
                widget.info.changelog.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text(
                widget.info.changelog,
                style: const TextStyle(
                  fontSize: 13,
                  color: Colors.black54,
                  height: 1.35,
                ),
              ),
            ],
            if (_latestRelease.version.isNotEmpty &&
                _latestRelease.versionCode > 0 &&
                (_latestRelease.version != widget.info.version ||
                    _latestRelease.versionCode != widget.info.versionCode)) ...[
              const SizedBox(height: 4),
              Text(
                '发布版本: ${_latestRelease.versionLabel}',
                style: const TextStyle(fontSize: 12, color: Colors.black45),
              ),
            ],
            if (_downloading) ...[
              const SizedBox(height: 16),
              LinearProgressIndicator(value: _progress),
              const SizedBox(height: 6),
              Text(
                '${(_progress * 100).toStringAsFixed(1)}%',
                style: const TextStyle(fontSize: 12, color: Colors.black54),
              ),
            ],
            if (_error != null) ...[
              const SizedBox(height: 8),
              Text(
                _error!,
                style: const TextStyle(fontSize: 12, color: Colors.red),
              ),
            ],
          ],
        ),
      ),
      actionsPadding: const EdgeInsets.fromLTRB(16, 4, 16, 16),
      actions: _downloading
          ? const []
          : [
              SizedBox(
                width: double.infinity,
                child: LayoutBuilder(
                  builder: (context, constraints) {
                    final compact = constraints.maxWidth < 320;
                    final compactButtonStyle = TextButton.styleFrom(
                      minimumSize: const Size(0, 40),
                      padding: const EdgeInsets.symmetric(horizontal: 6),
                    );
                    return Row(
                      children: [
                        if (compact)
                          IconButton(
                            onPressed: _openReleaseHistory,
                            tooltip: '更新历史',
                            icon: const Icon(Icons.history_rounded, size: 20),
                          )
                        else
                          TextButton.icon(
                            onPressed: _openReleaseHistory,
                            icon: const Icon(Icons.history_rounded, size: 16),
                            label: const Text('历史记录'),
                          ),
                        const SizedBox(width: 6),
                        Expanded(
                          child: TextButton(
                            onPressed: () => Navigator.of(context).pop(),
                            style: compactButtonStyle,
                            child: const Text('稍后再说'),
                          ),
                        ),
                        const SizedBox(width: 6),
                        Expanded(
                          child: ElevatedButton(
                            onPressed: _startUpdate,
                            style: ElevatedButton.styleFrom(
                              minimumSize: const Size(0, 40),
                              padding: const EdgeInsets.symmetric(
                                horizontal: 6,
                              ),
                              backgroundColor: const Color(0xFF3B82F6),
                              foregroundColor: Colors.white,
                              shape: RoundedRectangleBorder(
                                borderRadius: BorderRadius.circular(8),
                              ),
                            ),
                            child: const Text('立即更新'),
                          ),
                        ),
                      ],
                    );
                  },
                ),
              ),
            ],
    );
  }

  void _openReleaseHistory() {
    final router = GoRouter.of(context);
    Navigator.of(context).pop();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      router.push('/app-release-history');
    });
  }

  AppReleaseInfo get _latestRelease {
    for (final release in widget.info.releases) {
      if (release.versionCode == widget.info.versionCode) return release;
    }
    return widget.info.releases.isNotEmpty
        ? widget.info.releases.first
        : const AppReleaseInfo(
            version: '',
            versionCode: 0,
            releasedAt: '',
            items: [],
          );
  }

  Future<void> _startUpdate() async {
    if (!widget.info.hasInstallAsset) {
      setState(() {
        _error = '版本 ${widget.info.version} 未发布 ${widget.info.platform} 安装包';
      });
      return;
    }
    if (currentAppUpdatePlatform() == 'android') {
      final status = await Permission.requestInstallPackages.status;
      if (!status.isGranted) {
        final result = await Permission.requestInstallPackages.request();
        if (!result.isGranted) {
          setState(() => _error = '需要"安装未知应用"权限，请在系统设置中开启');
          await openAppSettings();
          return;
        }
      }
    }

    setState(() {
      _downloading = true;
      _progress = 0;
      _error = null;
    });

    try {
      final baseUrl = AppStorage.getBaseUrl();
      final downloadUrl = widget.info.downloadUrl;
      await UpdateLogService.append(
        'start_update_download',
        data: {
          'version': widget.info.version,
          'version_code': widget.info.versionCode,
          'platform': widget.info.platform,
          'filename': widget.info.asset?.filename ?? '',
          'sha256': widget.info.asset?.sha256 ?? '',
          'download_url': downloadUrl,
        },
      );
      final result = await downloadAndLaunchAppUpdate(
        info: widget.info,
        baseUrl: baseUrl,
        token: AppStorage.getToken() ?? '',
        onProgress: (received, total) {
          if (total > 0 && mounted) {
            setState(() => _progress = received / total);
          }
        },
      );
      await UpdateLogService.append(
        'launch_app_installer_result',
        data: {
          'message': result.message,
          'file_path': result.filePath,
          'platform': widget.info.platform,
        },
      );
      if (mounted) {
        Navigator.of(context).pop();
      }
    } catch (e) {
      await UpdateLogService.append(
        'update_download_failed',
        level: 'error',
        data: {
          'error': e.toString(),
          'version': widget.info.version,
          'version_code': widget.info.versionCode,
        },
      );
      if (mounted) {
        setState(() {
          _downloading = false;
          _error = '下载失败: $e';
        });
      }
    }
  }
}
