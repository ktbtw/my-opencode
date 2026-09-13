import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:go_router/go_router.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../../core/config/api_client.dart';
import '../../../core/notifications/app_notification_feedback.dart';
import '../../../core/storage/app_storage.dart';
import '../../../core/utils/browser_download.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';

typedef GuideJsonLoader = Future<Map<String, dynamic>> Function(String path);

class GuidePage extends StatefulWidget {
  const GuidePage({super.key, this.loadJson = ApiClient.get});

  final GuideJsonLoader loadJson;

  @override
  State<GuidePage> createState() => _GuidePageState();
}

String _guideDownloadUrl(String path) {
  final parsed = Uri.tryParse(path);
  if (parsed != null && parsed.hasScheme && parsed.host.isNotEmpty) {
    return path;
  }
  final baseUrl = AppStorage.getBaseUrl().replaceFirst(RegExp(r'/+$'), '');
  return path.startsWith('/') ? '$baseUrl$path' : '$baseUrl/$path';
}

class _GuidePageState extends State<GuidePage> {
  late Future<_GuidePayload> _payloadFuture;

  @override
  void initState() {
    super.initState();
    _payloadFuture = _loadPayload();
  }

  Future<_GuidePayload> _loadPayload() async {
    try {
      final responses = await Future.wait([
        widget.loadJson('/api/launcher/downloads'),
        widget.loadJson('/api/app/downloads'),
      ]);
      final launcherData = responses[0];
      final appData = responses[1];
      final launcherDownloads = _assetsFromResponse(
        launcherData,
        predicate: (item) =>
            item.kind == 'launcher' && item.platform != 'linux-x64',
      );
      final appDownloads = _assetsFromResponse(
        appData,
        predicate: (item) => item.kind == 'desktop' || item.kind == 'mobile',
      );
      final normalizedApps = appDownloads.map(_normalizeAppAsset).toList();
      return _GuidePayload(
        launcherVersion: launcherData['version'] as String? ?? '',
        appVersion: appData['version'] as String? ?? '',
        launcherDownloads: launcherDownloads.isEmpty
            ? _GuideDownloadAsset.fallback
                  .where((item) => item.kind == 'launcher')
                  .toList()
            : launcherDownloads,
        appDownloads: normalizedApps.isEmpty
            ? _GuideDownloadAsset.fallback
                  .where((item) => item.kind != 'launcher')
                  .toList()
            : normalizedApps,
      );
    } catch (_) {
      return _GuidePayload(
        launcherVersion: '',
        appVersion: '',
        launcherDownloads: _GuideDownloadAsset.fallback
            .where((item) => item.kind == 'launcher')
            .toList(),
        appDownloads: _GuideDownloadAsset.fallback
            .where((item) => item.kind != 'launcher')
            .toList(),
      );
    }
  }

  List<_GuideDownloadAsset> _assetsFromResponse(
    Map<String, dynamic> data, {
    required bool Function(_GuideDownloadAsset item) predicate,
  }) {
    return ((data['downloads'] as List?) ?? const [])
        .whereType<Map>()
        .map(
          (item) => _GuideDownloadAsset.fromJson(item.cast<String, dynamic>()),
        )
        .where(predicate)
        .toList();
  }

  _GuideDownloadAsset _normalizeAppAsset(_GuideDownloadAsset asset) {
    if (asset.platform == 'android') {
      return asset.copyWith(platform: 'android-apk');
    }
    return asset;
  }

  Future<void> _copyText(String label, String value) async {
    await Clipboard.setData(ClipboardData(text: value));
    if (!mounted) return;
    showAppFeedback(context, message: '$label 已复制到剪贴板');
  }

  String _downloadUrl(String path) {
    return _guideDownloadUrl(path);
  }

  Future<void> _openDownload(_GuideDownloadAsset asset) async {
    final url = _downloadUrl(asset.url);
    if (kIsWeb && await triggerBrowserDownload(url, filename: asset.filename)) {
      return;
    }
    try {
      final opened = await launchUrl(
        Uri.parse(url),
        mode: LaunchMode.platformDefault,
        webOnlyWindowName: '_self',
      );
      if (opened) return;
    } catch (_) {}
    await _copyText('${asset.title} 下载链接', url);
  }

  void _refresh() {
    setState(() {
      _payloadFuture = _loadPayload();
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: PageBackground(
          child: Column(
            children: [
              _buildTopBar(context),
              Expanded(
                child: FutureBuilder<_GuidePayload>(
                  future: _payloadFuture,
                  builder: (context, snapshot) {
                    if (snapshot.connectionState != ConnectionState.done) {
                      return const LoadingState();
                    }
                    if (snapshot.hasError) {
                      return _GuideErrorState(onRetry: _refresh);
                    }
                    final payload =
                        snapshot.data ??
                        _GuidePayload(
                          launcherVersion: '',
                          appVersion: '',
                          launcherDownloads: _GuideDownloadAsset.fallback
                              .where((item) => item.kind == 'launcher')
                              .toList(),
                          appDownloads: _GuideDownloadAsset.fallback
                              .where((item) => item.kind != 'launcher')
                              .toList(),
                        );
                    return _GuideBody(
                      payload: payload,
                      onCopy: _copyText,
                      onDownload: _openDownload,
                    );
                  },
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildTopBar(BuildContext context) {
    return Container(
      height: 56,
      decoration: const BoxDecoration(
        color: AppColors.surface,
        border: Border(bottom: BorderSide(color: AppColors.border)),
      ),
      padding: const EdgeInsets.symmetric(horizontal: 12),
      child: Row(
        children: [
          IconButton(
            icon: const Icon(Icons.arrow_back_ios_new, size: 15),
            onPressed: () => context.go('/devices'),
          ),
          const SizedBox(width: 8),
          const Text(
            '客户端下载',
            style: TextStyle(
              fontSize: 17,
              fontWeight: FontWeight.w600,
              color: AppColors.textPrimary,
            ),
          ),
          const Spacer(),
          IconButton(
            icon: const Icon(
              Icons.refresh,
              size: 18,
              color: AppColors.textSecondary,
            ),
            tooltip: '刷新',
            onPressed: _refresh,
          ),
        ],
      ),
    );
  }
}

class _GuideBody extends StatelessWidget {
  const _GuideBody({
    required this.payload,
    required this.onCopy,
    required this.onDownload,
  });

  final _GuidePayload payload;
  final Future<void> Function(String label, String value) onCopy;
  final Future<void> Function(_GuideDownloadAsset asset) onDownload;

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      padding: EdgeInsets.symmetric(
        horizontal: AppBreakpoints.isMobile(context) ? 16 : 24,
        vertical: 20,
      ),
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 1180),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              _GuideHeroCard(
                launcherVersion: payload.launcherVersion,
                appVersion: payload.appVersion,
              ),
              const SizedBox(height: 18),
              _ConnectionStepsCard(mobile: AppBreakpoints.isMobile(context)),
              const SizedBox(height: 22),
              _DownloadSection(
                icon: Icons.rocket_launch_rounded,
                title: 'Launcher',
                subtitle: '负责登录、凭证配置、后台服务接管和设备连接。',
                assets: payload.launcherDownloads,
                onCopy: onCopy,
                onDownload: onDownload,
              ),
              const SizedBox(height: 22),
              _DownloadSection(
                icon: Icons.forum_rounded,
                title: '对话操作程序',
                subtitle: '用于进入对话界面、管理项目和操作 Agent。',
                assets: payload.appDownloads,
                onCopy: onCopy,
                onDownload: onDownload,
              ),
              const SizedBox(height: 22),
              const _SectionTitle(
                icon: Icons.tips_and_updates_outlined,
                title: '说明',
                subtitle: 'Launcher 和对话操作程序可以按需分别安装。',
              ),
              const SizedBox(height: 12),
              const _GuideTipsCard(),
            ],
          ),
        ),
      ),
    );
  }
}

class _ConnectionStepsCard extends StatelessWidget {
  const _ConnectionStepsCard({required this.mobile});

  final bool mobile;

  @override
  Widget build(BuildContext context) {
    const steps = [
      ('01', '在电脑浏览器打开本页', '使用电脑访问当前下载页面。'),
      ('02', '下载对应的 Launcher', 'Windows 或 macOS 按平台选择 Launcher。'),
      ('03', '启动并登录同一账号', 'Launcher 接管设备后，设备会自动同步到工作台。'),
    ];
    return PanelCard(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(
                Icons.alt_route_rounded,
                color: AppColors.primary,
                size: 20,
              ),
              const SizedBox(width: 9),
              Expanded(
                child: Text(
                  mobile ? '请在电脑上完成设备接入' : '三步完成设备接入',
                  style: Theme.of(context).textTheme.titleLarge,
                ),
              ),
            ],
          ),
          const SizedBox(height: 14),
          LayoutBuilder(
            builder: (context, constraints) {
              final stacked = mobile || constraints.maxWidth < 720;
              return Flex(
                direction: stacked ? Axis.vertical : Axis.horizontal,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  for (var index = 0; index < steps.length; index++) ...[
                    if (index > 0)
                      stacked
                          ? const SizedBox(height: 9)
                          : const SizedBox(width: 9),
                    if (stacked)
                      _ConnectionStep(
                        number: steps[index].$1,
                        title: steps[index].$2,
                        description: steps[index].$3,
                      )
                    else
                      Expanded(
                        child: _ConnectionStep(
                          number: steps[index].$1,
                          title: steps[index].$2,
                          description: steps[index].$3,
                        ),
                      ),
                  ],
                ],
              );
            },
          ),
        ],
      ),
    );
  }
}

class _ConnectionStep extends StatelessWidget {
  const _ConnectionStep({
    required this.number,
    required this.title,
    required this.description,
  });

  final String number;
  final String title;
  final String description;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(13),
      decoration: BoxDecoration(
        color: AppColors.surfaceElevated,
        border: Border.all(color: AppColors.borderLight),
        borderRadius: AppRadius.smRadius,
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            number,
            style: const TextStyle(
              color: AppColors.primary,
              fontFamily: 'monospace',
              fontSize: 12,
              fontWeight: FontWeight.w800,
            ),
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: const TextStyle(
                    color: AppColors.textPrimary,
                    fontSize: 13,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  description,
                  style: const TextStyle(
                    color: AppColors.textSecondary,
                    fontSize: 11,
                    height: 1.45,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _DownloadSection extends StatelessWidget {
  const _DownloadSection({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.assets,
    required this.onCopy,
    required this.onDownload,
  });

  final IconData icon;
  final String title;
  final String subtitle;
  final List<_GuideDownloadAsset> assets;
  final Future<void> Function(String label, String value) onCopy;
  final Future<void> Function(_GuideDownloadAsset asset) onDownload;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _SectionTitle(icon: icon, title: title, subtitle: subtitle),
        const SizedBox(height: 12),
        _ResponsiveWrap(
          children: assets
              .map(
                (asset) => _DownloadCard(
                  asset: asset,
                  onPressed: asset.available ? () => onDownload(asset) : null,
                  onCopyLink: () => onCopy(
                    '${asset.title} 下载链接',
                    _guideDownloadUrl(asset.url),
                  ),
                ),
              )
              .toList(),
        ),
      ],
    );
  }
}

class _GuideHeroCard extends StatelessWidget {
  const _GuideHeroCard({
    required this.launcherVersion,
    required this.appVersion,
  });

  final String launcherVersion;
  final String appVersion;

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      padding: const EdgeInsets.all(0),
      child: Container(
        decoration: BoxDecoration(
          borderRadius: AppRadius.lgRadius,
          gradient: const LinearGradient(
            begin: Alignment.topLeft,
            end: Alignment.bottomRight,
            colors: [Color(0xFFF9FBFF), Color(0xFFEAF2FF)],
          ),
        ),
        padding: const EdgeInsets.all(22),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Wrap(
              spacing: 12,
              runSpacing: 12,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                Container(
                  width: 46,
                  height: 46,
                  decoration: BoxDecoration(
                    color: AppColors.primary,
                    borderRadius: BorderRadius.circular(14),
                    boxShadow: const [
                      BoxShadow(
                        color: Color(0x142563EB),
                        blurRadius: 16,
                        offset: Offset(0, 8),
                      ),
                    ],
                  ),
                  child: const Icon(
                    Icons.menu_book_outlined,
                    color: Colors.white,
                    size: 22,
                  ),
                ),
                Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      'Launcher 与对话操作程序',
                      style: Theme.of(context).textTheme.headlineSmall,
                    ),
                    const SizedBox(height: 4),
                    Text(
                      '按用途选择下载产物：Launcher 负责设备接入，对话操作程序负责使用工作区。',
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: AppColors.textSecondary,
                        height: 1.5,
                      ),
                    ),
                  ],
                ),
              ],
            ),
            const SizedBox(height: 18),
            Wrap(
              spacing: 12,
              runSpacing: 12,
              children: [
                _HeroInfoChip(
                  label: '码控版本',
                  value: appVersion.isEmpty ? '未读取到' : appVersion,
                  accent: AppColors.primaryLight,
                  textColor: AppColors.primary,
                ),
                _HeroInfoChip(
                  label: 'Launcher 版本',
                  value: launcherVersion.isEmpty ? '未读取到' : launcherVersion,
                  accent: AppColors.primaryLight,
                  textColor: AppColors.primary,
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _HeroInfoChip extends StatelessWidget {
  const _HeroInfoChip({
    required this.label,
    required this.value,
    required this.accent,
    required this.textColor,
  });

  final String label;
  final String value;
  final Color accent;
  final Color textColor;

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(minWidth: 220, maxWidth: 520),
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      decoration: BoxDecoration(
        color: Colors.white.withValues(alpha: 0.84),
        borderRadius: AppRadius.mdRadius,
        border: Border.all(color: AppColors.borderLight),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(label, style: Theme.of(context).textTheme.labelSmall),
                const SizedBox(height: 4),
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 10,
                    vertical: 7,
                  ),
                  decoration: BoxDecoration(
                    color: accent,
                    borderRadius: AppRadius.smRadius,
                  ),
                  child: Text(
                    value,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: textColor,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _DownloadCard extends StatelessWidget {
  const _DownloadCard({
    required this.asset,
    required this.onPressed,
    required this.onCopyLink,
  });

  final _GuideDownloadAsset asset;
  final VoidCallback? onPressed;
  final VoidCallback onCopyLink;

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 40,
                height: 40,
                decoration: BoxDecoration(
                  color: _platformTint(asset.platform),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Icon(
                  _platformIcon(asset.platform),
                  size: 20,
                  color: AppColors.textPrimary,
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      asset.title,
                      style: Theme.of(context).textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      asset.filename,
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: AppColors.textSecondary,
                      ),
                    ),
                  ],
                ),
              ),
              StatusPill(
                label: asset.available ? '可下载' : '未上传',
                type: asset.available ? StatusType.online : StatusType.warning,
              ),
            ],
          ),
          const SizedBox(height: 16),
          Text(
            _platformDescription(asset),
            style: Theme.of(context).textTheme.bodySmall?.copyWith(height: 1.5),
          ),
          const SizedBox(height: 16),
          Row(
            children: [
              Expanded(
                child: AppButton(
                  label: asset.available ? '立即下载' : '等待部署',
                  icon: Icons.download_rounded,
                  onPressed: onPressed,
                ),
              ),
              const SizedBox(width: 10),
              SizedBox(
                width: 100,
                child: AppButton(
                  label: '复制链接',
                  outlined: true,
                  onPressed: onCopyLink,
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  static IconData _platformIcon(String platform) {
    switch (platform) {
      case 'windows-x64':
        return Icons.window_outlined;
      case 'darwin-arm64':
        return Icons.laptop_mac_outlined;
      case 'android-apk':
        return Icons.android_rounded;
      default:
        return Icons.computer_outlined;
    }
  }

  static Color _platformTint(String platform) {
    switch (platform) {
      case 'windows-x64':
        return const Color(0xFFE8F0FF);
      case 'darwin-arm64':
        return const Color(0xFFF1F5F9);
      case 'android-apk':
        return const Color(0xFFEAF7EE);
      default:
        return const Color(0xFFEEF9F1);
    }
  }

  static String _platformDescription(_GuideDownloadAsset asset) {
    if (asset.kind == 'desktop') {
      switch (asset.platform) {
        case 'windows-x64':
          return '适用于 64 位 Windows 设备，下载 EXE 安装程序后直接运行即可。';
        case 'darwin-arm64':
          return '适用于 Apple Silicon Mac，下载 DMG 安装镜像后打开并安装。';
      }
    }
    switch (asset.platform) {
      case 'windows-x64':
        return '适用于 64 位 Windows 设备，下载后解压，直接运行 Launcher 即可。';
      case 'darwin-arm64':
        return '适用于 Apple Silicon Mac，下载解压后给 Launcher 增加执行权限，再直接运行。';
      case 'android-apk':
        return '适用于 Android 设备，下载后直接安装即可；如果已有旧版本，可用于手动升级。';
      default:
        return '下载后打开 Launcher，登录账号即可自动接管后台服务。';
    }
  }
}

class _GuideTipsCard extends StatelessWidget {
  const _GuideTipsCard();

  @override
  Widget build(BuildContext context) {
    const tips = [
      '打开 GUI Launcher 后登录账号，系统会自动完成本机凭证配置和后台服务接管。',
      '如果本机已经有新版后台服务在运行，GUI 会优先接管现有服务，不会重复启动多个实例。',
      '需要开机自启动时，在 Launcher 登录或设置页开启即可，不需要再手动配置终端环境变量。',
      '创建 Agent 时直接在 GUI 中选择项目目录，后续可在设备详情页查看状态、升级和管理运行记录。',
      '若连接异常，可优先检查后端地址、账号登录状态和网络连通性。',
    ];

    return PanelCard(
      child: Column(
        children: tips
            .map(
              (tip) => Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Container(
                      width: 8,
                      height: 8,
                      margin: const EdgeInsets.only(top: 6),
                      decoration: const BoxDecoration(
                        color: AppColors.primary,
                        shape: BoxShape.circle,
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        tip,
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: AppColors.textSecondary,
                          height: 1.6,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            )
            .toList(),
      ),
    );
  }
}

class _GuideErrorState extends StatelessWidget {
  const _GuideErrorState({required this.onRetry});

  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: PanelCard(
        width: 360,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(
              Icons.error_outline,
              size: 34,
              color: AppColors.statusError,
            ),
            const SizedBox(height: 12),
            const Text(
              '下载信息加载失败',
              style: TextStyle(
                fontSize: 16,
                fontWeight: FontWeight.w600,
                color: AppColors.textPrimary,
              ),
            ),
            const SizedBox(height: 6),
            const Text(
              '请检查后端是否可访问，然后重新刷新。',
              textAlign: TextAlign.center,
              style: TextStyle(
                fontSize: 13,
                color: AppColors.textSecondary,
                height: 1.5,
              ),
            ),
            const SizedBox(height: 16),
            AppButton(label: '重新加载', onPressed: onRetry),
          ],
        ),
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle({
    required this.icon,
    required this.title,
    required this.subtitle,
  });

  final IconData icon;
  final String title;
  final String subtitle;

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Container(
          width: 36,
          height: 36,
          decoration: BoxDecoration(
            color: AppColors.primaryLight,
            borderRadius: BorderRadius.circular(10),
            border: Border.all(color: AppColors.primaryMuted),
          ),
          child: Icon(icon, size: 18, color: AppColors.primaryHover),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(title, style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: 3),
              Text(
                subtitle,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: AppColors.textSecondary,
                  height: 1.5,
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

class _ResponsiveWrap extends StatelessWidget {
  const _ResponsiveWrap({required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        int columns = 1;
        if (constraints.maxWidth >= 1080) {
          columns = 3;
        } else if (constraints.maxWidth >= 700) {
          columns = 2;
        }
        const gap = 14.0;
        final width = columns == 1
            ? constraints.maxWidth
            : (constraints.maxWidth - gap * (columns - 1)) / columns;
        return Wrap(
          spacing: gap,
          runSpacing: gap,
          children: children
              .map((child) => SizedBox(width: width, child: child))
              .toList(),
        );
      },
    );
  }
}

class _GuidePayload {
  const _GuidePayload({
    required this.launcherVersion,
    required this.appVersion,
    required this.launcherDownloads,
    required this.appDownloads,
  });

  final String launcherVersion;
  final String appVersion;
  final List<_GuideDownloadAsset> launcherDownloads;
  final List<_GuideDownloadAsset> appDownloads;
}

class _GuideDownloadAsset {
  const _GuideDownloadAsset({
    required this.id,
    required this.title,
    required this.platform,
    required this.kind,
    required this.filename,
    required this.url,
    required this.available,
  });

  factory _GuideDownloadAsset.fromJson(Map<String, dynamic> json) {
    return _GuideDownloadAsset(
      id: json['id'] as String? ?? '',
      title: json['title'] as String? ?? '',
      platform: json['platform'] as String? ?? '',
      kind: json['kind'] as String? ?? '',
      filename: json['filename'] as String? ?? '',
      url: json['url'] as String? ?? '',
      available: json['available'] == true,
    );
  }

  _GuideDownloadAsset copyWith({String? platform}) {
    return _GuideDownloadAsset(
      id: id,
      title: title,
      platform: platform ?? this.platform,
      kind: kind,
      filename: filename,
      url: url,
      available: available,
    );
  }

  final String id;
  final String title;
  final String platform;
  final String kind;
  final String filename;
  final String url;
  final bool available;

  static const apk = _GuideDownloadAsset(
    id: 'android-apk',
    title: 'Android APK',
    platform: 'android-apk',
    kind: 'apk',
    filename: 'chat-codex.apk',
    url: '/api/app/download',
    available: true,
  );

  static const fallback = [
    _GuideDownloadAsset(
      id: 'windows-x64',
      title: 'Windows x64 安装程序',
      platform: 'windows-x64',
      kind: 'desktop',
      filename: 'chat-codex-windows-x64-setup.exe',
      url: '/api/app/download?artifact=chat-codex-windows-x64-setup.exe',
      available: false,
    ),
    _GuideDownloadAsset(
      id: 'darwin-arm64',
      title: 'macOS Apple Silicon 安装镜像',
      platform: 'darwin-arm64',
      kind: 'desktop',
      filename: 'chat-codex-darwin-arm64.dmg',
      url: '/api/app/download?artifact=chat-codex-darwin-arm64.dmg',
      available: false,
    ),
    apk,
    _GuideDownloadAsset(
      id: 'chat-codex-launcher-windows-x64',
      title: 'Windows x64 GUI Launcher',
      platform: 'windows-x64',
      kind: 'launcher',
      filename: 'chat-codex-launcher-windows-x64.zip',
      url:
          '/api/launcher/download?artifact=chat-codex-launcher-windows-x64.zip',
      available: false,
    ),
    _GuideDownloadAsset(
      id: 'chat-codex-launcher-darwin-arm64',
      title: 'macOS Apple Silicon GUI Launcher',
      platform: 'darwin-arm64',
      kind: 'launcher',
      filename: 'chat-codex-launcher-darwin-arm64.zip',
      url:
          '/api/launcher/download?artifact=chat-codex-launcher-darwin-arm64.zip',
      available: false,
    ),
  ];
}
