import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/notifications/app_notification_feedback.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/app_status_icon.dart';
import '../../../shared/widgets/widgets.dart';
import '../../settings/settings_provider.dart';
import '../project_settings_provider.dart';
import '../../../core/services/directory_label.dart';

/// 项目设置：只放"这个项目独有"的开关，页头先回答"改的是哪个项目"。
///
/// 与设置页保持同一套语言：分区标题 + 卡片式条目 + 同一套开关配色，
/// 而不是把裸 ListTile / SwitchListTile 直接铺在页面上。
class ProjectSettingsPage extends ConsumerStatefulWidget {
  final String project;
  final String projectName;
  const ProjectSettingsPage({
    super.key,
    required this.project,
    required this.projectName,
  });

  @override
  ConsumerState<ProjectSettingsPage> createState() =>
      _ProjectSettingsPageState();
}

class _ProjectSettingsPageState extends ConsumerState<ProjectSettingsPage> {
  // SAF 的 content:// 目录解析结果（仅用于展示）。
  String? _resolvedDirectory;
  String? _resolvedFrom;

  void _ensureDirectoryResolved(String raw) {
    if (raw.isEmpty || _resolvedFrom == raw) return;
    _resolvedFrom = raw;
    final cached = DirectoryLabel.cached(raw);
    if (cached != null) {
      _resolvedDirectory = cached;
      return;
    }
    _resolvedDirectory = null;
    DirectoryLabel.describe(raw).then((value) {
      if (!mounted) return;
      setState(() => _resolvedDirectory = value);
    });
  }

  bool _saving = false;
  bool get _directorySupported =>
      !kIsWeb && defaultTargetPlatform != TargetPlatform.iOS;

  Future<void> _save(
    Future<void> Function(ProjectSettingsNotifier) action,
  ) async {
    setState(() => _saving = true);
    final notifier = ref.read(projectSettingsProvider(widget.project).notifier);
    try {
      await action(notifier);
    } catch (error) {
      if (mounted) {
        showAppFeedback(
          context,
          title: '设置未保存',
          message: error.toString(),
          error: true,
        );
      }
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _pickDirectory(ProjectSettingsNotifier notifier) async {
    if (defaultTargetPlatform == TargetPlatform.android) {
      final picked = await const MethodChannel(
        'chat_codex/artifact_directory',
      ).invokeMapMethod<String, dynamic>('pickDirectory');
      final uri = picked?['uri'] as String?;
      if (uri != null && uri.isNotEmpty) {
        await notifier.update(
          directory: uri,
          label: picked?['label'] as String? ?? uri,
        );
      }
      return;
    }
    final path = await FilePicker.platform.getDirectoryPath(
      dialogTitle: '选择本项目交付文件下载目录',
    );
    if (path != null && path.trim().isNotEmpty) {
      await notifier.update(directory: path, label: path);
    }
  }

  @override
  Widget build(BuildContext context) {
    final settings = ref.watch(projectSettingsProvider(widget.project));
    final global = ref.watch(settingsProvider);
    final enabled = settings.loaded && !_saving;
    final customDirectory = settings.downloadDirectory?.trim() ?? '';
    _ensureDirectoryResolved(customDirectory);
    final hasCustomDirectory = customDirectory.isNotEmpty;
    final directoryValue = !_directorySupported
        ? (kIsWeb ? '由浏览器管理下载位置' : '使用应用默认目录')
        : settings.directoryLabel?.trim().isNotEmpty == true
        ? settings.directoryLabel!.trim()
        : hasCustomDirectory
        ? (_resolvedDirectory ?? '正在解析目录…')
        : '沿用全局：${global.downloadDirectoryLabel.isNotEmpty ? global.downloadDirectoryLabel : '系统默认下载目录'}';

    return Scaffold(
      backgroundColor: AppColors.background,
      appBar: AppBar(
        backgroundColor: AppColors.surface,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        leading: Navigator.of(context).canPop()
            ? const AppStatusBackButton()
            : null,
        title: const Text(
          '项目设置',
          style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
        ),
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(1),
          child: Container(height: 1, color: AppColors.border),
        ),
      ),
      // 内容区用统一的页面渐变底（与设备配置页、项目记忆页一致），
      // 白卡片靠底色深浅与描边浮起来，而不是白压白。
      body: PageBackground(
        child: ListView(
          padding: const EdgeInsets.symmetric(
            vertical: AppSpacing.md,
            horizontal: AppSpacing.md,
          ),
          children: [
            _ProjectHeaderCard(
              name: widget.projectName,
              project: widget.project,
            ),
            const SizedBox(height: AppSpacing.lg),
            const _SectionHeader(title: '对话显示'),
            _ProjectSwitchTile(
              icon: Icons.speed_outlined,
              title: '显示连接状态',
              subtitle: '在对话顶部显示连接延迟、接收速率和同步进度',
              value: settings.showConnectionStatus,
              enabled: enabled,
              onChanged: (value) => _save(
                (notifier) => notifier.update(showConnectionStatus: value),
              ),
            ),
            const SizedBox(height: AppSpacing.md),
            const _SectionHeader(title: '文件与下载'),
            _ProjectActionTile(
              icon: Icons.download_outlined,
              title: '交付文件下载目录',
              subtitle: directoryValue,
              // 已覆盖时值就是自定义目录，此时动作是"更改"。
              actionLabel: !_directorySupported
                  ? null
                  : hasCustomDirectory
                  ? '更改'
                  : '选择',
              enabled: enabled && _directorySupported,
              onTap: () => _save(_pickDirectory),
            ),
            if (hasCustomDirectory) ...[
              const SizedBox(height: AppSpacing.sm),
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton.icon(
                  onPressed: enabled
                      ? () => _save(
                          (notifier) => notifier.update(resetDirectory: true),
                        )
                      : null,
                  icon: const Icon(
                    Icons.settings_backup_restore_rounded,
                    size: 16,
                  ),
                  label: const Text('恢复为全局下载目录'),
                  style: TextButton.styleFrom(
                    foregroundColor: AppColors.textSecondary,
                    padding: const EdgeInsets.symmetric(horizontal: 8),
                    minimumSize: const Size(44, 40),
                    tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                  ),
                ),
              ),
            ],
            const SizedBox(height: AppSpacing.sm),
            const _HintNote(text: '项目目录优先于全局目录。新的下载会使用此位置，已有文件保留原位。'),
            if (_saving) ...[
              const SizedBox(height: AppSpacing.md),
              const _SavingIndicator(),
            ],
          ],
        ),
      ),
    );
  }
}

/// 页头：先回答"改的是哪个项目"，再说明作用范围。
class _ProjectHeaderCard extends StatelessWidget {
  final String name;
  final String project;

  const _ProjectHeaderCard({required this.name, required this.project});

  @override
  Widget build(BuildContext context) {
    final title = name.trim().isEmpty ? project.trim() : name.trim();
    final identifier = project.trim();
    return PanelCard(
      padding: const EdgeInsets.all(16),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 42,
            height: 42,
            decoration: BoxDecoration(
              color: AppColors.primaryLight,
              borderRadius: AppRadius.mdRadius,
            ),
            child: const Icon(
              Icons.folder_special_outlined,
              size: 20,
              color: AppColors.primary,
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Flexible(
                      child: Text(
                        title,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w600,
                          color: AppColors.textPrimary,
                        ),
                      ),
                    ),
                    const SizedBox(width: 8),
                    const _Pill(label: '当前项目'),
                  ],
                ),
                if (identifier.isNotEmpty && identifier != title) ...[
                  const SizedBox(height: 4),
                  Text(
                    identifier,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      fontSize: 12,
                      color: AppColors.textMuted,
                    ),
                  ),
                ],
                const SizedBox(height: 6),
                const Text(
                  '仅应用于当前账号在本设备上的这个项目',
                  style: TextStyle(
                    fontSize: 12,
                    height: 1.5,
                    color: AppColors.textSecondary,
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

class _Pill extends StatelessWidget {
  final String label;

  const _Pill({required this.label});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: AppColors.primaryLight,
        borderRadius: AppRadius.smRadius,
      ),
      child: Text(
        label,
        style: const TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w600,
          color: AppColors.primary,
        ),
      ),
    );
  }
}

class _SectionHeader extends StatelessWidget {
  final String title;
  const _SectionHeader({required this.title});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(left: 4, bottom: 8, top: 4),
      child: Text(
        title,
        style: const TextStyle(
          fontSize: 12,
          fontWeight: FontWeight.w600,
          color: AppColors.textMuted,
          letterSpacing: 0.5,
        ),
      ),
    );
  }
}

/// 与设置页同一套条目：卡片 + 图标 + 标题/说明 + 同一配色开关。
class _ProjectSwitchTile extends StatelessWidget {
  final IconData icon;
  final String title;
  final String subtitle;
  final bool value;
  final bool enabled;
  final ValueChanged<bool> onChanged;

  const _ProjectSwitchTile({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.value,
    required this.enabled,
    required this.onChanged,
  });

  @override
  Widget build(BuildContext context) {
    return Opacity(
      opacity: enabled ? 1 : 0.55,
      child: Container(
        decoration: BoxDecoration(
          color: AppColors.surface,
          borderRadius: AppRadius.mdRadius,
          border: Border.all(color: AppColors.border),
        ),
        child: InkWell(
          onTap: enabled ? () => onChanged(!value) : null,
          borderRadius: AppRadius.mdRadius,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
            child: Row(
              children: [
                Icon(icon, size: 18, color: AppColors.textSecondary),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        title,
                        style: const TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.w500,
                          color: AppColors.textPrimary,
                        ),
                      ),
                      const SizedBox(height: 2),
                      Text(
                        subtitle,
                        style: const TextStyle(
                          fontSize: 12,
                          height: 1.5,
                          color: AppColors.textSecondary,
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 8),
                Switch(
                  value: value && enabled,
                  onChanged: enabled ? onChanged : null,
                  activeColor: AppColors.primary,
                  materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// 打开型条目：右侧用动作词代替裸箭头，值单独一行便于长路径换行。
class _ProjectActionTile extends StatelessWidget {
  final IconData icon;
  final String title;
  final String subtitle;
  final String? actionLabel;
  final bool enabled;
  final VoidCallback onTap;

  const _ProjectActionTile({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.enabled,
    required this.onTap,
    this.actionLabel,
  });

  @override
  Widget build(BuildContext context) {
    return Opacity(
      opacity: enabled ? 1 : 0.55,
      child: Container(
        decoration: BoxDecoration(
          color: AppColors.surface,
          borderRadius: AppRadius.mdRadius,
          border: Border.all(color: AppColors.border),
        ),
        child: InkWell(
          onTap: enabled ? onTap : null,
          borderRadius: AppRadius.mdRadius,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Padding(
                  padding: const EdgeInsets.only(top: 1),
                  child: Icon(icon, size: 18, color: AppColors.textSecondary),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        title,
                        style: const TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.w500,
                          color: AppColors.textPrimary,
                        ),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        subtitle,
                        style: const TextStyle(
                          fontSize: 12,
                          height: 1.5,
                          color: AppColors.textSecondary,
                        ),
                      ),
                    ],
                  ),
                ),
                if (actionLabel != null) ...[
                  const SizedBox(width: 10),
                  Padding(
                    padding: const EdgeInsets.only(top: 1),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          actionLabel!,
                          style: const TextStyle(
                            fontSize: 12,
                            fontWeight: FontWeight.w600,
                            color: AppColors.primary,
                          ),
                        ),
                        const Icon(
                          Icons.chevron_right_rounded,
                          size: 18,
                          color: AppColors.primary,
                        ),
                      ],
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _HintNote extends StatelessWidget {
  final String text;
  const _HintNote({required this.text});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Padding(
            padding: EdgeInsets.only(top: 1),
            child: Icon(
              Icons.info_outline_rounded,
              size: 14,
              color: AppColors.textMuted,
            ),
          ),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              text,
              style: const TextStyle(
                fontSize: 12,
                height: 1.6,
                color: AppColors.textMuted,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _SavingIndicator extends StatelessWidget {
  const _SavingIndicator();

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.center,
      children: const [
        SizedBox(
          width: 14,
          height: 14,
          child: CircularProgressIndicator(
            strokeWidth: 1.6,
            color: AppColors.textMuted,
          ),
        ),
        SizedBox(width: 8),
        Text(
          '正在保存…',
          style: TextStyle(fontSize: 12, color: AppColors.textMuted),
        ),
      ],
    );
  }
}
