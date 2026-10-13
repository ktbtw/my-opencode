import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';
import '../../../../core/theme/app_theme.dart';
import '../../../../shared/widgets/widgets.dart';
import '../../application/ai_config_sync.dart';
import '../../data/device_model.dart';

/// 同步流程第一步：选择目标设备。
///
/// 只负责设备列表的加载、展示与选择；离线设备不可选。
class AiConfigSyncDeviceDialog extends StatefulWidget {
  final String currentMachineId;

  /// 设备列表加载器（由页面注入，失败时弹窗内提供重试）。
  final Future<List<DeviceModel>> Function() loadDevices;

  const AiConfigSyncDeviceDialog({
    super.key,
    required this.currentMachineId,
    required this.loadDevices,
  });

  @override
  State<AiConfigSyncDeviceDialog> createState() =>
      _AiConfigSyncDeviceDialogState();
}

class _AiConfigSyncDeviceDialogState extends State<AiConfigSyncDeviceDialog> {
  List<DeviceModel> _devices = const [];
  bool _loading = true;
  String _error = '';

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      final devices = await widget.loadDevices();
      if (!mounted) return;
      final others = devices
          .where((device) => device.machineId != widget.currentMachineId)
          .toList();
      final online = others.where((device) => device.online);
      final offline = others.where((device) => !device.online);
      setState(() {
        _devices = [...online, ...offline];
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _devices = const [];
        _error = error.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final viewport = MediaQuery.sizeOf(context);
    final dialogWidth = math.min(560.0, viewport.width - 32);
    final dialogHeight = math.min(600.0, math.max(320.0, viewport.height - 40));
    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
      shape: RoundedRectangleBorder(borderRadius: AppRadius.lgRadius),
      child: SizedBox(
        width: dialogWidth,
        height: dialogHeight,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  const Expanded(
                    child: Text(
                      '同步到其他设备',
                      style: TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  IconButton(
                    tooltip: '关闭',
                    onPressed: () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close_rounded, size: 20),
                  ),
                ],
              ),
              const Text(
                '选择接收本设备 AI 配置的目标设备，目标设备需要在线。',
                style: TextStyle(fontSize: 12, color: AppColors.textMuted),
              ),
              const SizedBox(height: 12),
              Expanded(child: _buildBody()),
              const SizedBox(height: 12),
              Row(
                children: [
                  Expanded(
                    child: AppButton(
                      label: '取消',
                      outlined: true,
                      onPressed: () => Navigator.of(context).pop(),
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildBody() {
    if (_loading) {
      return const LoadingState();
    }
    if (_error.isNotEmpty) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              _error,
              textAlign: TextAlign.center,
              style: const TextStyle(
                fontSize: 12,
                color: AppColors.statusError,
              ),
            ),
            const SizedBox(height: 12),
            AppButton(
              label: '重试',
              outlined: true,
              width: 120,
              onPressed: _load,
            ),
          ],
        ),
      );
    }
    if (_devices.isEmpty) {
      return const EmptyState(
        message: '没有可同步的其他设备',
        icon: Icons.devices_other_outlined,
      );
    }
    return ListView.separated(
      itemCount: _devices.length,
      separatorBuilder: (context, index) => const SizedBox(height: 8),
      itemBuilder: (context, index) => _DeviceRow(device: _devices[index]),
    );
  }
}

/// 设备列表行：在线可点，离线置灰并标记。
class _DeviceRow extends StatelessWidget {
  final DeviceModel device;

  const _DeviceRow({required this.device});

  @override
  Widget build(BuildContext context) {
    final online = device.online;
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: AppRadius.mdRadius,
        onTap: online ? () => Navigator.of(context).pop(device) : null,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
          decoration: BoxDecoration(
            color: online ? AppColors.surface : AppColors.statusOfflineLight,
            border: Border.all(color: AppColors.border),
            borderRadius: AppRadius.mdRadius,
          ),
          child: Row(
            children: [
              Icon(
                Icons.computer_rounded,
                size: 20,
                color: online ? AppColors.primary : AppColors.textMuted,
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      device.effectiveName,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w600,
                        color: online
                            ? AppColors.textPrimary
                            : AppColors.textMuted,
                      ),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      device.machineId,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 11,
                        color: AppColors.textMuted,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 8),
              StatusPill(
                label: online ? '在线' : '离线',
                type: online ? StatusType.online : StatusType.offline,
              ),
              if (online) ...[
                const SizedBox(width: 4),
                const Icon(
                  Icons.chevron_right_rounded,
                  size: 18,
                  color: AppColors.textMuted,
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// 同步流程第二步：确认同步计划并处理供应商冲突。
///
/// 弹窗自身负责加载、错误重试、冲突选择与应用中状态；
/// 读取与应用逻辑由页面通过 [loadPreview] / [onApply] 注入。
class AiConfigSyncPlanDialog extends StatefulWidget {
  final String sourceLabel;
  final String targetLabel;
  final Future<AiConfigSyncPreview> Function() loadPreview;
  final Future<AiConfigSyncApplyResult> Function(
    AiConfigSyncSelection selection,
    AiConfigSyncPreview preview,
  )
  onApply;

  const AiConfigSyncPlanDialog({
    super.key,
    required this.sourceLabel,
    required this.targetLabel,
    required this.loadPreview,
    required this.onApply,
  });

  @override
  State<AiConfigSyncPlanDialog> createState() => _AiConfigSyncPlanDialogState();
}

class _AiConfigSyncPlanDialogState extends State<AiConfigSyncPlanDialog> {
  AiConfigSyncPreview? _preview;
  Map<String, bool> _useSource = const {};
  bool _syncModel = false;
  bool _loading = true;
  bool _applying = false;
  String _loadError = '';
  String _applyError = '';

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _loadError = '';
      _applyError = '';
    });
    try {
      final preview = await widget.loadPreview();
      if (!mounted) return;
      setState(() {
        _preview = preview;
        // 冲突项默认使用源设备的配置，用户可逐项改为保留目标设备。
        _useSource = {
          for (final id in preview.plan.conflictProviders) id: true,
        };
        // 默认模型存在差异时默认勾选同步。
        _syncModel = preview.plan.modelChanged;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _preview = null;
        _loadError = error.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  /// 当前选择下是否有实际要执行的变化。
  bool get _hasEffectiveWork {
    final preview = _preview;
    if (preview == null) return false;
    final plan = preview.plan;
    if (plan.addedProviders.isNotEmpty) return true;
    if (plan.conflictProviders.any((id) => _useSource[id] ?? false)) {
      return true;
    }
    return _syncModel && plan.modelChanged;
  }

  Future<void> _apply() async {
    final preview = _preview;
    if (preview == null) return;
    final selection = AiConfigSyncSelection(
      useSource: _useSource,
      syncDefaultModel: _syncModel,
    );
    setState(() {
      _applying = true;
      _applyError = '';
    });
    try {
      final result = await widget.onApply(selection, preview);
      if (!mounted) return;
      Navigator.of(context).pop(result);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _applying = false;
        _applyError = error.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final viewport = MediaQuery.sizeOf(context);
    final dialogWidth = math.min(680.0, viewport.width - 32);
    final dialogHeight = math.min(720.0, math.max(360.0, viewport.height - 40));
    final preview = _preview;
    final nothingToDo = preview != null && !preview.plan.hasWork;
    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
      shape: RoundedRectangleBorder(borderRadius: AppRadius.lgRadius),
      child: SizedBox(
        width: dialogWidth,
        height: dialogHeight,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  const Expanded(
                    child: Text(
                      '同步 AI 配置',
                      style: TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  IconButton(
                    tooltip: '关闭',
                    onPressed: _applying
                        ? null
                        : () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close_rounded, size: 20),
                  ),
                ],
              ),
              Text(
                '将「${widget.sourceLabel}」的 AI 配置同步到「${widget.targetLabel}」。',
                style: const TextStyle(
                  fontSize: 12,
                  color: AppColors.textMuted,
                ),
              ),
              const SizedBox(height: 12),
              Expanded(child: _buildBody()),
              const SizedBox(height: 12),
              if (_applyError.isNotEmpty) ...[
                Text(
                  _applyError,
                  style: const TextStyle(
                    fontSize: 12,
                    color: AppColors.statusError,
                  ),
                ),
                const SizedBox(height: 8),
              ],
              if (!_loading && _loadError.isEmpty)
                Row(
                  children: [
                    Expanded(
                      child: AppButton(
                        label: nothingToDo ? '关闭' : '取消',
                        outlined: true,
                        onPressed: _applying
                            ? null
                            : () => Navigator.of(context).pop(),
                      ),
                    ),
                    if (!nothingToDo) ...[
                      const SizedBox(width: 12),
                      Expanded(
                        child: AppButton(
                          label: '开始同步',
                          loading: _applying,
                          onPressed: _hasEffectiveWork ? _apply : null,
                        ),
                      ),
                    ],
                  ],
                ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildBody() {
    if (_loading) {
      return const LoadingState();
    }
    if (_loadError.isNotEmpty) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              _loadError,
              textAlign: TextAlign.center,
              style: const TextStyle(
                fontSize: 12,
                color: AppColors.statusError,
              ),
            ),
            const SizedBox(height: 12),
            AppButton(
              label: '重试',
              outlined: true,
              width: 120,
              onPressed: _load,
            ),
          ],
        ),
      );
    }
    final preview = _preview;
    if (preview == null) return const SizedBox.shrink();
    final plan = preview.plan;
    if (!plan.hasWork) {
      return const EmptyState(
        message: '两台设备的 AI 配置已一致，无需同步',
        icon: Icons.check_circle_outline_rounded,
      );
    }
    return SingleChildScrollView(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (plan.addedProviders.isNotEmpty)
            _SummaryLine(
              icon: Icons.add_circle_outline_rounded,
              color: AppColors.primary,
              text:
                  '将新增 ${plan.addedProviders.length} 个供应商：'
                  '${plan.addedProviders.join('、')}',
            ),
          if (plan.conflictProviders.isNotEmpty) ...[
            if (plan.addedProviders.isNotEmpty) const SizedBox(height: 10),
            Text(
              '${plan.conflictProviders.length} 个供应商冲突，请选择保留哪个：',
              style: const TextStyle(
                fontSize: 12.5,
                fontWeight: FontWeight.w600,
                color: AppColors.textPrimary,
              ),
            ),
            const SizedBox(height: 8),
            for (final id in plan.conflictProviders)
              Padding(
                padding: const EdgeInsets.only(bottom: 10),
                child: _ConflictCard(
                  diff: preview.providerDiffs[id],
                  sourceLabel: widget.sourceLabel,
                  targetLabel: widget.targetLabel,
                  useSource: _useSource[id] ?? true,
                  enabled: !_applying,
                  onChanged: (useSource) {
                    setState(() {
                      _useSource = {..._useSource, id: useSource};
                    });
                  },
                ),
              ),
          ],
          if (plan.identicalProviders.isNotEmpty) ...[
            const SizedBox(height: 2),
            _SummaryLine(
              icon: Icons.check_circle_outline_rounded,
              color: AppColors.statusOnline,
              text: '${plan.identicalProviders.length} 个供应商配置一致，将自动跳过',
            ),
          ],
          if (plan.targetOnlyProviders.isNotEmpty) ...[
            const SizedBox(height: 8),
            _SummaryLine(
              icon: Icons.info_outline_rounded,
              color: AppColors.textMuted,
              text:
                  '${plan.targetOnlyProviders.length} 个供应商仅目标设备有，保持不动：'
                  '${plan.targetOnlyProviders.join('、')}',
            ),
          ],
          if (plan.modelChanged) ...[
            const SizedBox(height: 12),
            _ModelSyncRow(
              sourceLabel: widget.sourceLabel,
              targetLabel: widget.targetLabel,
              sourceModel: plan.sourceModel,
              targetModel: plan.targetModel,
              value: _syncModel,
              enabled: !_applying,
              onChanged: (value) {
                setState(() {
                  _syncModel = value;
                });
              },
            ),
          ],
        ],
      ),
    );
  }
}

/// 概要行：图标 + 文案。
class _SummaryLine extends StatelessWidget {
  final IconData icon;
  final Color color;
  final String text;

  const _SummaryLine({
    required this.icon,
    required this.color,
    required this.text,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, size: 16, color: color),
        const SizedBox(width: 6),
        Expanded(
          child: Text(
            text,
            style: const TextStyle(
              fontSize: 12.5,
              color: AppColors.textPrimary,
              height: 1.4,
            ),
          ),
        ),
      ],
    );
  }
}

/// 冲突供应商卡片：两侧摘要 + 保留选择。
class _ConflictCard extends StatelessWidget {
  final AiConfigProviderDiff? diff;
  final String sourceLabel;
  final String targetLabel;
  final bool useSource;
  final bool enabled;
  final ValueChanged<bool> onChanged;

  const _ConflictCard({
    required this.diff,
    required this.sourceLabel,
    required this.targetLabel,
    required this.useSource,
    required this.enabled,
    required this.onChanged,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: AppColors.surfaceElevated,
        border: Border.all(color: AppColors.border),
        borderRadius: AppRadius.mdRadius,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  diff?.id ?? '',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 13,
                    fontWeight: FontWeight.w700,
                    color: AppColors.textPrimary,
                  ),
                ),
              ),
              const SizedBox(width: 8),
              for (final field in diff?.changedFields ?? const <String>[])
                Padding(
                  padding: const EdgeInsets.only(left: 4),
                  child: Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 6,
                      vertical: 2,
                    ),
                    decoration: BoxDecoration(
                      color: AppColors.statusWarningLight,
                      borderRadius: AppRadius.smRadius,
                    ),
                    child: Text(
                      field,
                      style: const TextStyle(
                        fontSize: 10.5,
                        color: AppColors.statusWarning,
                      ),
                    ),
                  ),
                ),
            ],
          ),
          const SizedBox(height: 8),
          _SideLine(label: sourceLabel, value: diff?.sourceSummary ?? '—'),
          const SizedBox(height: 4),
          _SideLine(label: targetLabel, value: diff?.targetSummary ?? '—'),
          const SizedBox(height: 10),
          _SyncChoiceTile(
            label: '使用「$sourceLabel」的配置',
            selected: useSource,
            enabled: enabled,
            onTap: () => onChanged(true),
          ),
          const SizedBox(height: 6),
          _SyncChoiceTile(
            label: '保留「$targetLabel」的配置（跳过此项）',
            selected: !useSource,
            enabled: enabled,
            onTap: () => onChanged(false),
          ),
        ],
      ),
    );
  }
}

/// 冲突卡片中的单侧摘要行。
class _SideLine extends StatelessWidget {
  final String label;
  final String value;

  const _SideLine({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 72,
          child: Text(
            label,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: const TextStyle(fontSize: 11.5, color: AppColors.textMuted),
          ),
        ),
        Expanded(
          child: Text(
            value,
            style: const TextStyle(
              fontSize: 11.5,
              color: AppColors.textSecondary,
              height: 1.4,
            ),
          ),
        ),
      ],
    );
  }
}

/// 二选一的保留项行。
class _SyncChoiceTile extends StatelessWidget {
  final String label;
  final bool selected;
  final bool enabled;
  final VoidCallback onTap;

  const _SyncChoiceTile({
    required this.label,
    required this.selected,
    required this.enabled,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: AppRadius.smRadius,
        onTap: enabled ? onTap : null,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 7),
          decoration: BoxDecoration(
            color: selected ? AppColors.primaryLight : AppColors.surface,
            border: Border.all(
              color: selected ? AppColors.primary : AppColors.border,
            ),
            borderRadius: AppRadius.smRadius,
          ),
          child: Row(
            children: [
              Icon(
                selected
                    ? Icons.radio_button_checked_rounded
                    : Icons.radio_button_unchecked_rounded,
                size: 16,
                color: selected ? AppColors.primary : AppColors.textMuted,
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  label,
                  style: TextStyle(
                    fontSize: 12,
                    color: selected
                        ? AppColors.textPrimary
                        : AppColors.textSecondary,
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// 默认模型同步开关行。
class _ModelSyncRow extends StatelessWidget {
  final String sourceLabel;
  final String targetLabel;
  final String sourceModel;
  final String targetModel;
  final bool value;
  final bool enabled;
  final ValueChanged<bool> onChanged;

  const _ModelSyncRow({
    required this.sourceLabel,
    required this.targetLabel,
    required this.sourceModel,
    required this.targetModel,
    required this.value,
    required this.enabled,
    required this.onChanged,
  });

  @override
  Widget build(BuildContext context) {
    final targetText = targetModel.isEmpty ? '未设置' : targetModel;
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: AppRadius.mdRadius,
        onTap: enabled ? () => onChanged(!value) : null,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
          decoration: BoxDecoration(
            border: Border.all(color: AppColors.border),
            borderRadius: AppRadius.mdRadius,
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Checkbox(
                value: value,
                visualDensity: VisualDensity.compact,
                onChanged: enabled ? (next) => onChanged(next ?? false) : null,
              ),
              const SizedBox(width: 6),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Padding(
                      padding: EdgeInsets.only(top: 8),
                      child: Text(
                        '同步默认模型',
                        style: TextStyle(
                          fontSize: 12.5,
                          fontWeight: FontWeight.w600,
                          color: AppColors.textPrimary,
                        ),
                      ),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      '$sourceLabel：$sourceModel\n$targetLabel 当前：$targetText',
                      style: const TextStyle(
                        fontSize: 11,
                        color: AppColors.textMuted,
                        height: 1.4,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
