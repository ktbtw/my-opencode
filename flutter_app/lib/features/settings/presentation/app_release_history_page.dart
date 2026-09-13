import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../../../core/config/api_client.dart';
import '../../../core/services/app_update_models.dart';
import '../../../core/theme/app_colors.dart';
import '../../../shared/widgets/widgets.dart';

typedef AppReleaseHistoryLoader = Future<List<AppReleaseInfo>> Function();

class AppReleaseHistoryPage extends StatefulWidget {
  final AppReleaseHistoryLoader? loader;

  const AppReleaseHistoryPage({super.key, this.loader});

  @override
  State<AppReleaseHistoryPage> createState() => _AppReleaseHistoryPageState();
}

class _AppReleaseHistoryPageState extends State<AppReleaseHistoryPage> {
  late Future<List<AppReleaseInfo>> _releasesFuture;

  @override
  void initState() {
    super.initState();
    _releasesFuture = _loadReleases();
  }

  Future<List<AppReleaseInfo>> _loadReleases() async {
    if (widget.loader != null) return widget.loader!();
    final data = await ApiClient.get('/api/app/releases');
    final releases = (data['releases'] as List<dynamic>? ?? const [])
        .whereType<Map<String, dynamic>>()
        .map(AppReleaseInfo.fromJson)
        .where((release) => release.version.isNotEmpty)
        .toList();
    releases.sort((a, b) {
      final codeOrder = b.versionCode.compareTo(a.versionCode);
      if (codeOrder != 0) return codeOrder;
      return b.version.compareTo(a.version);
    });
    return releases;
  }

  void _refresh() {
    final future = _loadReleases();
    setState(() {
      _releasesFuture = future;
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: PageBackground(
          child: Column(
            children: [
              AppTopBar(
                title: '更新历史',
                leading: IconButton(
                  tooltip: '返回',
                  icon: const Icon(Icons.arrow_back_ios_new, size: 15),
                  onPressed: () {
                    if (context.canPop()) {
                      context.pop();
                    } else {
                      context.go('/devices');
                    }
                  },
                ),
                actions: [
                  IconButton(
                    tooltip: '刷新更新历史',
                    icon: const Icon(Icons.refresh_rounded, size: 19),
                    onPressed: _refresh,
                  ),
                ],
              ),
              Expanded(
                child: FutureBuilder<List<AppReleaseInfo>>(
                  future: _releasesFuture,
                  builder: (context, snapshot) {
                    if (snapshot.connectionState != ConnectionState.done) {
                      return const Center(child: CircularProgressIndicator());
                    }
                    if (snapshot.hasError) {
                      return _ErrorState(
                        message: snapshot.error.toString(),
                        onRetry: _refresh,
                      );
                    }
                    return _ReleaseList(releases: snapshot.data ?? const []);
                  },
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _ReleaseList extends StatelessWidget {
  final List<AppReleaseInfo> releases;

  const _ReleaseList({required this.releases});

  @override
  Widget build(BuildContext context) {
    if (releases.isEmpty) {
      return const EmptyState(
        message: '暂无更新历史',
        icon: Icons.history_toggle_off_rounded,
      );
    }

    return Align(
      alignment: Alignment.topCenter,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 820),
        child: ListView.separated(
          padding: const EdgeInsets.fromLTRB(16, 20, 16, 28),
          itemCount: releases.length,
          separatorBuilder: (_, __) => const SizedBox(height: 12),
          itemBuilder: (context, index) =>
              _ReleaseCard(release: releases[index], current: index == 0),
        ),
      ),
    );
  }
}

class _ReleaseCard extends StatelessWidget {
  final AppReleaseInfo release;
  final bool current;

  const _ReleaseCard({required this.release, required this.current});

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      padding: const EdgeInsets.fromLTRB(18, 16, 18, 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Text(
                  '版本 ${release.versionLabel}',
                  style: Theme.of(context).textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
              if (current)
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 4,
                  ),
                  decoration: BoxDecoration(
                    color: AppColors.primaryLight,
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: const Text(
                    '当前版本',
                    style: TextStyle(
                      color: AppColors.primary,
                      fontSize: 11,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
            ],
          ),
          if (release.releasedAt.isNotEmpty) ...[
            const SizedBox(height: 3),
            Text(
              release.releasedAt,
              style: Theme.of(context).textTheme.labelSmall,
            ),
          ],
          const SizedBox(height: 12),
          if (release.items.isEmpty)
            Text('暂无更新说明', style: Theme.of(context).textTheme.bodySmall)
          else
            ...release.items.map(
              (item) => Padding(
                padding: const EdgeInsets.only(bottom: 7),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Padding(
                      padding: EdgeInsets.only(top: 6),
                      child: Icon(
                        Icons.circle,
                        size: 5,
                        color: AppColors.primary,
                      ),
                    ),
                    const SizedBox(width: 9),
                    Expanded(
                      child: Text(
                        item,
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: AppColors.textSecondary,
                          height: 1.4,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  final String message;
  final VoidCallback onRetry;

  const _ErrorState({required this.message, required this.onRetry});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(
              Icons.cloud_off_rounded,
              size: 40,
              color: AppColors.textMuted,
            ),
            const SizedBox(height: 12),
            const Text(
              '更新历史加载失败',
              style: TextStyle(
                color: AppColors.textPrimary,
                fontWeight: FontWeight.w600,
              ),
            ),
            const SizedBox(height: 6),
            Text(
              message,
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
              textAlign: TextAlign.center,
              style: const TextStyle(
                color: AppColors.textSecondary,
                fontSize: 12,
              ),
            ),
            const SizedBox(height: 16),
            AppButton(
              label: '重试',
              icon: Icons.refresh_rounded,
              outlined: true,
              onPressed: onRetry,
            ),
          ],
        ),
      ),
    );
  }
}
