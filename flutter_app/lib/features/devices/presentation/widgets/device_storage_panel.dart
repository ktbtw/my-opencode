import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/notifications/app_notification_feedback.dart';
import '../../../../core/theme/app_colors.dart';
import '../../../../shared/widgets/widgets.dart';
import '../../data/device_storage_model.dart';
import '../device_provider.dart';

/// 设备磁盘占用面板：展示 launcher 运行目录各类别的占用，并支持清理可再生成的缓存。
///
/// 体积统计需要遍历整个运行目录（常见数 GB、上万个文件），
/// 因此数据按需拉取，进入设备页时查一次，之后由用户手动刷新。
class DeviceStoragePanel extends ConsumerStatefulWidget {
  final String machineId;
  final bool deviceOnline;

  /// 由外层统一面板承载时置为 true：不再自绘卡片，只输出内容。
  final bool embedded;

  const DeviceStoragePanel({
    super.key,
    required this.machineId,
    this.deviceOnline = true,
    this.embedded = false,
  });

  @override
  ConsumerState<DeviceStoragePanel> createState() => _DeviceStoragePanelState();
}

class _DeviceStoragePanelState extends ConsumerState<DeviceStoragePanel> {
  DeviceStorageUsage? _usage;
  bool _loading = false;
  bool _clearing = false;
  String _error = '';

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _load();
    });
  }

  @override
  void didUpdateWidget(covariant DeviceStoragePanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    // 切换设备时重新统计，避免展示上一台设备的数据。
    if (oldWidget.machineId != widget.machineId) {
      _usage = null;
      _error = '';
      _load();
    }
  }

  Future<void> _load() async {
    if (_loading) return;
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      final usage = await ref
          .read(deviceRepositoryProvider)
          .getDeviceStorage(machineId: widget.machineId);
      if (!mounted) return;
      setState(() => _usage = usage);
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = _describeError(error));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _confirmClear() async {
    final usage = _usage;
    if (usage == null) return;
    final clearable = usage.clearableCategories;
    if (clearable.isEmpty) {
      showAppFeedback(context, message: '当前没有可清理的内容');
      return;
    }
    final names = clearable.map((item) => item.label).join('、');
    final ok = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('清理缓存'),
        content: Text(
          '将清理：$names\n\n'
          '预计可释放 ${formatStorageBytes(usage.clearableBytes)}。\n'
          '运行时组件、Agent 数据与程序文件不会被清理。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('确认清理'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;

    setState(() => _clearing = true);
    try {
      final result = await ref
          .read(deviceRepositoryProvider)
          .clearDeviceStorage(machineId: widget.machineId);
      if (!mounted) return;
      // 清理后重新统计，保证展示的是真实剩余体积。
      final usageAfter = await ref
          .read(deviceRepositoryProvider)
          .getDeviceStorage(machineId: widget.machineId);
      if (!mounted) return;
      setState(() => _usage = usageAfter);
      showAppFeedback(
        context,
        message: result.hasFreed
            ? '已释放 ${result.freedLabel}'
            : '没有可释放的空间',
      );
    } catch (error) {
      if (!mounted) return;
      showAppFeedback(context, message: _describeError(error), error: true);
    } finally {
      if (mounted) setState(() => _clearing = false);
    }
  }

  String _describeError(Object error) {
    final text = error.toString().replaceFirst('Exception: ', '');
    return text.isEmpty ? '操作失败' : text;
  }

  Widget _shell(Widget child) => widget.embedded
      ? child
      : PanelCard(padding: const EdgeInsets.all(20), child: child);

  @override
  Widget build(BuildContext context) {
    final usage = _usage;
    return _shell(
      Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Expanded(
                child: Text(
                  '磁盘占用',
                  style: TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w600,
                    color: AppColors.textPrimary,
                  ),
                ),
              ),
              if (usage != null)
                Text(
                  usage.totalLabel,
                  style: const TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                    color: AppColors.textSecondary,
                  ),
                ),
              const SizedBox(width: 4),
              IconButton(
                tooltip: '重新统计',
                visualDensity: VisualDensity.compact,
                iconSize: 18,
                onPressed: _loading ? null : _load,
                icon: _loading
                    ? const SizedBox(
                        width: 14,
                        height: 14,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.refresh_rounded),
              ),
            ],
          ),
          const SizedBox(height: 4),
          if (usage == null && _loading)
            const Padding(
              padding: EdgeInsets.symmetric(vertical: 14),
              child: Text(
                '正在统计磁盘占用…',
                style: TextStyle(fontSize: 13, color: AppColors.textMuted),
              ),
            )
          else if (usage == null)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 10),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    _error.isNotEmpty
                        ? _error
                        : (widget.deviceOnline ? '暂无占用数据' : '设备离线，无法统计'),
                    style: const TextStyle(
                      fontSize: 13,
                      color: AppColors.textMuted,
                    ),
                  ),
                  const SizedBox(height: 10),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: AppButton(
                      label: '重试',
                      outlined: true,
                      icon: Icons.refresh_rounded,
                      onPressed: _loading ? null : _load,
                    ),
                  ),
                ],
              ),
            )
          else ...[
            const SizedBox(height: 8),
            ..._buildRows(usage),
            const SizedBox(height: 14),
            const Divider(height: 1),
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: Text(
                    usage.clearableBytes > 0
                        ? '可清理 ${formatStorageBytes(usage.clearableBytes)}'
                        : '暂无可清理内容',
                    style: const TextStyle(
                      fontSize: 12,
                      color: AppColors.textMuted,
                    ),
                  ),
                ),
                AppButton(
                  label: '清理缓存',
                  outlined: true,
                  icon: Icons.cleaning_services_rounded,
                  loading: _clearing,
                  onPressed: (!widget.deviceOnline ||
                          _clearing ||
                          usage.clearableBytes <= 0)
                      ? null
                      : _confirmClear,
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }

  List<Widget> _buildRows(DeviceStorageUsage usage) {
    final items = usage.sortedCategories;
    // 以最大类别为基准画条形，使相对占比一眼可见。
    final maxBytes = items.fold<int>(1, (max, item) {
      return item.bytes > max ? item.bytes : max;
    });
    final rows = <Widget>[];
    for (var index = 0; index < items.length; index++) {
      final item = items[index];
      if (index > 0) rows.add(const SizedBox(height: 12));
      rows.add(_StorageRow(item: item, maxBytes: maxBytes));
    }
    return rows;
  }
}

/// 单类别占用行：名称 + 体积 + 相对占比条。
class _StorageRow extends StatelessWidget {
  final DeviceStorageCategory item;
  final int maxBytes;

  const _StorageRow({required this.item, required this.maxBytes});

  @override
  Widget build(BuildContext context) {
    final ratio = maxBytes <= 0 ? 0.0 : (item.bytes / maxBytes).clamp(0.0, 1.0);
    // 可清理的用主色以提示“可以释放”，不可清理的用中性灰说明是固定成本。
    final color = item.clearable ? AppColors.primary : AppColors.statusOffline;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Expanded(
              child: Row(
                children: [
                  Flexible(
                    child: Text(
                      item.label,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 13,
                        color: AppColors.textPrimary,
                      ),
                    ),
                  ),
                  if (item.clearable) ...[
                    const SizedBox(width: 6),
                    Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 5,
                        vertical: 1,
                      ),
                      decoration: BoxDecoration(
                        color: AppColors.primaryLight,
                        borderRadius: BorderRadius.circular(4),
                      ),
                      child: const Text(
                        '可清理',
                        style: TextStyle(
                          fontSize: 10,
                          color: AppColors.primary,
                        ),
                      ),
                    ),
                  ],
                ],
              ),
            ),
            Text(
              formatStorageBytes(item.bytes),
              style: const TextStyle(
                fontSize: 12,
                fontWeight: FontWeight.w600,
                color: AppColors.textSecondary,
              ),
            ),
          ],
        ),
        if (item.detail.isNotEmpty) ...[
          const SizedBox(height: 2),
          Text(
            item.detail,
            style: const TextStyle(fontSize: 11, color: AppColors.textMuted),
          ),
        ],
        const SizedBox(height: 5),
        ClipRRect(
          borderRadius: BorderRadius.circular(99),
          child: LinearProgressIndicator(
            value: ratio,
            minHeight: 5,
            backgroundColor: AppColors.inputBackground,
            valueColor: AlwaysStoppedAnimation<Color>(color),
          ),
        ),
      ],
    );
  }
}
