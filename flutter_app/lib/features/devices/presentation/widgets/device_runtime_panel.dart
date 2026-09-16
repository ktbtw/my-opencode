import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';
import '../../../../shared/widgets/widgets.dart';
import '../../data/device_metrics_model.dart';

/// 设备运行状态面板：展示内存、磁盘、CPU 与运行环境。
///
/// 指标由设备主动推送，可能缺失（老版本 launcher）或过期（设备离线）。
/// 界面按"有无数据"分为三态：无数据占位、正常展示、过期置灰。
class DeviceRuntimePanel extends StatelessWidget {
  final DeviceMetrics? metrics;
  final bool deviceOnline;
  final bool compact;

  /// 数据过期阈值：超过该秒数视为陈旧，界面降级展示。
  static const int staleThresholdSeconds = 45;

  const DeviceRuntimePanel({
    super.key,
    required this.metrics,
    this.deviceOnline = true,
    this.compact = false,
  });

  @override
  Widget build(BuildContext context) {
    final data = metrics;
    // 完全没有数据时不展示一张空卡片，只给一行说明，避免视觉噪音。
    if (data == null || !data.hasAnyData) {
      return PanelCard(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const _PanelHeader(title: '运行状态'),
            const SizedBox(height: 14),
            Text(
              deviceOnline ? '设备尚未上报运行数据' : '设备离线，暂无运行数据',
              style: const TextStyle(
                fontSize: 13,
                color: AppColors.textMuted,
              ),
            ),
            const SizedBox(height: 6),
            const Text(
              '需要设备端升级后支持',
              style: TextStyle(fontSize: 12, color: AppColors.textMuted),
            ),
          ],
        ),
      );
    }

    final stale = deviceOnline
        ? data.isStale(thresholdSeconds: staleThresholdSeconds)
        : true;

    return PanelCard(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _PanelHeader(
            title: '运行状态',
            trailing: _FreshnessLabel(
              metrics: data,
              deviceOnline: deviceOnline,
            ),
          ),
          const SizedBox(height: 16),
          _MetricBar(
            icon: Icons.memory_rounded,
            label: '内存',
            valueText: data.memoryTotalBytes > 0
                ? '${formatBytes(data.memoryUsedBytes)} / ${formatBytes(data.memoryTotalBytes)}'
                : '暂无数据',
            percent: data.memoryPercent,
            dimmed: stale,
          ),
          if (data.primaryDisk != null) ...[
            const SizedBox(height: 14),
            _DiskSection(
              metrics: data,
              compact: compact,
              dimmed: stale,
            ),
          ],
          const SizedBox(height: 14),
          _MetricBar(
            icon: Icons.speed_rounded,
            label: 'CPU',
            valueText: data.cpuPercent > 0
                ? '${data.cpuPercent.toStringAsFixed(1)}%'
                : '暂无数据',
            percent: data.cpuPercent,
            dimmed: stale,
          ),
          if (_hasEnvironment(data)) ...[
            const SizedBox(height: 18),
            const Divider(),
            const SizedBox(height: 14),
            _EnvironmentFacts(metrics: data, dimmed: stale),
          ],
        ],
      ),
    );
  }

  /// 是否有值得展示的运行环境信息。
  bool _hasEnvironment(DeviceMetrics data) {
    return data.uptimeSeconds > 0 || data.platform.isNotEmpty;
  }
}

/// 磁盘区：主分区用进度条，多余分区折叠为一行摘要。
class _DiskSection extends StatefulWidget {
  final DeviceMetrics metrics;
  final bool compact;
  final bool dimmed;

  const _DiskSection({
    required this.metrics,
    required this.compact,
    required this.dimmed,
  });

  @override
  State<_DiskSection> createState() => _DiskSectionState();
}

class _DiskSectionState extends State<_DiskSection> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    final disks = widget.metrics.disks;
    final primary = widget.metrics.primaryDisk!;
    final others = disks.where((disk) => disk != primary).toList();

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _MetricBar(
          icon: Icons.storage_rounded,
          label: disks.length > 1 ? '磁盘 ${primary.label}' : '磁盘',
          valueText:
              '${formatBytes(primary.usedBytes)} / ${formatBytes(primary.totalBytes)}',
          percent: primary.usedPercent,
          dimmed: widget.dimmed,
        ),
        if (others.isNotEmpty) ...[
          const SizedBox(height: 8),
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton(
              onPressed: () => setState(() => _expanded = !_expanded),
              style: TextButton.styleFrom(
                padding: EdgeInsets.zero,
                minimumSize: const Size(0, 28),
                tapTargetSize: MaterialTapTargetSize.shrinkWrap,
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    _expanded ? '收起其他分区' : '查看其他 ${others.length} 个分区',
                    style: const TextStyle(
                      fontSize: 12,
                      color: AppColors.primary,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                  Icon(
                    _expanded
                        ? Icons.keyboard_arrow_up_rounded
                        : Icons.keyboard_arrow_down_rounded,
                    size: 16,
                    color: AppColors.primary,
                  ),
                ],
              ),
            ),
          ),
          if (_expanded)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Column(
                children: [
                  for (final disk in others)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 10),
                      child: _MetricBar(
                        icon: Icons.sd_storage_outlined,
                        label: disk.label,
                        valueText:
                            '${formatBytes(disk.usedBytes)} / ${formatBytes(disk.totalBytes)}',
                        percent: disk.usedPercent,
                        dimmed: widget.dimmed,
                        dense: true,
                      ),
                    ),
                ],
              ),
            ),
        ],
      ],
    );
  }
}

/// 单条指标：图标 + 标签 + 数值 + 分级配色进度条。
class _MetricBar extends StatelessWidget {
  final IconData icon;
  final String label;
  final String valueText;
  final double percent;
  final bool dimmed;
  final bool dense;

  const _MetricBar({
    required this.icon,
    required this.label,
    required this.valueText,
    required this.percent,
    required this.dimmed,
    this.dense = false,
  });

  /// 按占用率分级配色：正常主色、偏高警告、接近上限危险。
  Color _barColor() {
    if (percent >= 90) return AppColors.statusError;
    if (percent >= 70) return AppColors.statusWarning;
    return AppColors.primary;
  }

  @override
  Widget build(BuildContext context) {
    final color = dimmed ? AppColors.statusOffline : _barColor();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Icon(
              icon,
              size: dense ? 14 : 15,
              color: dimmed ? AppColors.textMuted : AppColors.textSecondary,
            ),
            const SizedBox(width: 6),
            Expanded(
              child: Text(
                label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  fontSize: dense ? 12 : 13,
                  fontWeight: FontWeight.w500,
                  color: dimmed
                      ? AppColors.textMuted
                      : AppColors.textSecondary,
                ),
              ),
            ),
            const SizedBox(width: 8),
            Text(
              valueText,
              style: TextStyle(
                fontSize: dense ? 12 : 13,
                fontWeight: FontWeight.w600,
                color: dimmed ? AppColors.textMuted : AppColors.textPrimary,
              ),
            ),
          ],
        ),
        const SizedBox(height: 8),
        ClipRRect(
          borderRadius: BorderRadius.circular(99),
          child: LinearProgressIndicator(
            value: (percent / 100).clamp(0.0, 1.0),
            minHeight: dense ? 5 : 6,
            backgroundColor: AppColors.inputBackground,
            valueColor: AlwaysStoppedAnimation<Color>(color),
          ),
        ),
        const SizedBox(height: 4),
        Text(
          '${percent.toStringAsFixed(percent >= 10 ? 0 : 1)}%',
          style: TextStyle(
            fontSize: 11,
            color: dimmed ? AppColors.textMuted : AppColors.textMuted,
          ),
        ),
      ],
    );
  }
}

/// 运行环境摘要：平台、运行时长、CPU 核数、负载。
class _EnvironmentFacts extends StatelessWidget {
  final DeviceMetrics metrics;
  final bool dimmed;

  const _EnvironmentFacts({required this.metrics, required this.dimmed});

  @override
  Widget build(BuildContext context) {
    final facts = <Widget>[];
    if (metrics.platform.isNotEmpty) {
      final platform = metrics.arch.isEmpty
          ? metrics.platform
          : '${metrics.platform}/${metrics.arch}';
      facts.add(_Fact(label: '系统', value: _platformLabel(platform)));
    }
    if (metrics.uptimeSeconds > 0) {
      facts.add(_Fact(label: '已运行', value: formatUptime(metrics.uptime)));
    }
    if (metrics.cpuCores > 0) {
      facts.add(_Fact(label: 'CPU 核心', value: '${metrics.cpuCores}'));
    }
    // Windows 没有 load average 语义，仅在可用时展示。
    if (metrics.loadAvailable && metrics.load1 > 0) {
      facts.add(
        _Fact(
          label: '负载',
          value: metrics.load1.toStringAsFixed(2),
        ),
      );
    }

    return Wrap(
      spacing: 24,
      runSpacing: 12,
      children: facts,
    );
  }

  /// 把 GOOS/GOARCH 转为用户能看懂的系统名。
  String _platformLabel(String platform) {
    final lower = platform.toLowerCase();
    if (lower.startsWith('darwin')) return 'macOS';
    if (lower.startsWith('windows')) return 'Windows';
    if (lower.startsWith('linux')) return 'Linux';
    return platform;
  }
}

class _Fact extends StatelessWidget {
  final String label;
  final String value;

  const _Fact({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(
          label,
          style: const TextStyle(fontSize: 11, color: AppColors.textMuted),
        ),
        const SizedBox(height: 3),
        Text(
          value,
          style: const TextStyle(
            fontSize: 13,
            fontWeight: FontWeight.w600,
            color: AppColors.textPrimary,
          ),
        ),
      ],
    );
  }
}

/// 数据新鲜度：显示"X 秒前"，过期时变灰提示。
class _FreshnessLabel extends StatelessWidget {
  final DeviceMetrics metrics;
  final bool deviceOnline;

  const _FreshnessLabel({required this.metrics, required this.deviceOnline});

  @override
  Widget build(BuildContext context) {
    final age = metrics.ageSeconds();
    final String text;
    final bool stale;
    if (!deviceOnline) {
      text = '离线';
      stale = true;
    } else if (age == null) {
      text = '时间未知';
      stale = true;
    } else if (age < 5) {
      text = '刚刚';
      stale = false;
    } else if (age < 60) {
      text = '$age 秒前';
      stale = age > DeviceRuntimePanel.staleThresholdSeconds;
    } else if (age < 3600) {
      text = '${age ~/ 60} 分钟前';
      stale = true;
    } else {
      text = '${age ~/ 3600} 小时前';
      stale = true;
    }

    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(
          stale ? Icons.schedule_outlined : Icons.bolt_rounded,
          size: 13,
          color: stale ? AppColors.textMuted : AppColors.statusOnline,
        ),
        const SizedBox(width: 4),
        Text(
          text,
          style: TextStyle(
            fontSize: 11,
            color: stale ? AppColors.textMuted : AppColors.statusOnline,
            fontWeight: FontWeight.w500,
          ),
        ),
      ],
    );
  }
}

class _PanelHeader extends StatelessWidget {
  final String title;
  final Widget? trailing;

  const _PanelHeader({required this.title, this.trailing});

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Text(
          title,
          style: const TextStyle(
            fontSize: 14,
            fontWeight: FontWeight.w600,
            color: AppColors.textPrimary,
          ),
        ),
        const Spacer(),
        if (trailing != null) trailing!,
      ],
    );
  }
}
