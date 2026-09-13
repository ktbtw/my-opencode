import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/notifications/app_notification_feedback.dart';
import '../../../core/services/global_overlay_service.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../settings_provider.dart';

class SettingsPage extends ConsumerWidget {
  const SettingsPage({super.key});

  bool get _supportsDirectoryPicker =>
      !kIsWeb && defaultTargetPlatform != TargetPlatform.iOS;

  Future<void> _pickDownloadDirectory(
    BuildContext context,
    WidgetRef ref,
  ) async {
    final notifier = ref.read(settingsProvider.notifier);
    try {
      final selected = await FilePicker.platform.getDirectoryPath(
        dialogTitle: '选择下载目录',
      );
      if (selected == null || selected.trim().isEmpty) return;
      await notifier.setDownloadDirectory(selected);
      if (!context.mounted) return;
      showAppFeedback(context, message: '下载目录已更新');
    } catch (e) {
      if (!context.mounted) return;
      showAppFeedback(
        context,
        title: '选择目录失败',
        message: e.toString(),
        error: true,
      );
    }
  }

  Future<void> _resetDownloadDirectory(
    BuildContext context,
    WidgetRef ref,
  ) async {
    await ref.read(settingsProvider.notifier).setDownloadDirectory(null);
    if (!context.mounted) return;
    showAppFeedback(context, message: '已恢复系统默认下载目录');
  }

  Future<void> _editGlobalPrompt(BuildContext context, WidgetRef ref) async {
    final notifier = ref.read(settingsProvider.notifier);
    final controller = TextEditingController(
      text: ref.read(settingsProvider).globalPrompt,
    );

    try {
      await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        backgroundColor: AppColors.surface,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
        ),
        builder: (sheetContext) {
          return Padding(
            padding: EdgeInsets.only(
              left: 16,
              right: 16,
              top: 16,
              bottom: MediaQuery.of(sheetContext).viewInsets.bottom + 16,
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  '全局提示词',
                  style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
                ),
                const SizedBox(height: 6),
                const Text(
                  '对所有新任务默认生效，可用于长期偏好和通用规则',
                  style: TextStyle(
                    fontSize: 12,
                    color: AppColors.textSecondary,
                  ),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: controller,
                  minLines: 6,
                  maxLines: 10,
                  autofocus: true,
                  decoration: InputDecoration(
                    hintText: '输入全局提示词',
                    border: OutlineInputBorder(
                      borderRadius: AppRadius.mdRadius,
                      borderSide: const BorderSide(color: AppColors.border),
                    ),
                    enabledBorder: OutlineInputBorder(
                      borderRadius: AppRadius.mdRadius,
                      borderSide: const BorderSide(color: AppColors.border),
                    ),
                    focusedBorder: OutlineInputBorder(
                      borderRadius: AppRadius.mdRadius,
                      borderSide: const BorderSide(
                        color: AppColors.primary,
                        width: 1.5,
                      ),
                    ),
                    filled: true,
                    fillColor: AppColors.inputBackground,
                  ),
                ),
                const SizedBox(height: 12),
                Row(
                  children: [
                    TextButton(
                      onPressed: () async {
                        try {
                          await notifier.saveGlobalPrompt('');
                          if (!sheetContext.mounted) return;
                          Navigator.of(sheetContext).pop();
                        } catch (e) {
                          if (!context.mounted) return;
                          showAppFeedback(
                            context,
                            title: '清空全局提示词失败',
                            message: e.toString(),
                            error: true,
                          );
                        }
                      },
                      child: const Text('清空'),
                    ),
                    const Spacer(),
                    TextButton(
                      onPressed: () => Navigator.of(sheetContext).pop(),
                      child: const Text('取消'),
                    ),
                    const SizedBox(width: 8),
                    FilledButton(
                      onPressed: () async {
                        try {
                          await notifier.saveGlobalPrompt(controller.text);
                          if (!sheetContext.mounted) return;
                          Navigator.of(sheetContext).pop();
                        } catch (e) {
                          if (!context.mounted) return;
                          showAppFeedback(
                            context,
                            title: '保存全局提示词失败',
                            message: e.toString(),
                            error: true,
                          );
                        }
                      },
                      child: const Text('保存'),
                    ),
                  ],
                ),
              ],
            ),
          );
        },
      );
    } finally {
      controller.dispose();
    }
  }

  Future<void> _editSubagentRoleModels(
    BuildContext context,
    WidgetRef ref,
  ) async {
    final notifier = ref.read(settingsProvider.notifier);
    final controller = TextEditingController(
      text: const JsonEncoder.withIndent(
        '  ',
      ).convert(ref.read(settingsProvider).subagentOrchestrationRoleModels),
    );
    try {
      await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        backgroundColor: AppColors.surface,
        builder: (sheetContext) => Padding(
          padding: EdgeInsets.fromLTRB(
            16,
            16,
            16,
            MediaQuery.of(sheetContext).viewInsets.bottom + 16,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text(
                '子代理默认模型',
                style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
              ),
              const SizedBox(height: 6),
              const Text(
                '为不同工作角色设置启动时使用的模型，仅影响之后启动的子代理；未设置的角色使用系统默认模型。',
                style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
              ),
              const SizedBox(height: 12),
              ConstrainedBox(
                constraints: const BoxConstraints(maxHeight: 220),
                child: TextField(
                  controller: controller,
                  autofocus: true,
                  minLines: 1,
                  maxLines: 6,
                  expands: false,
                  decoration: const InputDecoration(
                    hintText: '{"探索分析": {"模型": "..."}}',
                    border: OutlineInputBorder(),
                  ),
                ),
              ),
              Align(
                alignment: Alignment.centerRight,
                child: IconButton(
                  tooltip: '保存',
                  icon: const Icon(Icons.save_outlined),
                  onPressed: () async {
                    try {
                      final decoded = jsonDecode(controller.text);
                      if (decoded is! Map) {
                        throw const FormatException('模型映射必须是对象');
                      }
                      final value = Map<String, dynamic>.from(decoded);
                      for (final entry in value.entries) {
                        if (entry.value is! Map) {
                          throw const FormatException('每个角色都需要模型对象');
                        }
                        final model = Map<String, dynamic>.from(
                          entry.value as Map,
                        );
                        if ((model['providerID'] as String? ?? '')
                                .trim()
                                .isEmpty ||
                            (model['modelID'] as String? ?? '')
                                .trim()
                                .isEmpty) {
                          throw const FormatException(
                            '每个角色都需要 providerID 与 modelID',
                          );
                        }
                      }
                      await notifier.setSubagentOrchestrationRoleModels(value);
                      if (sheetContext.mounted) {
                        Navigator.of(sheetContext).pop();
                      }
                    } catch (error) {
                      if (context.mounted) {
                        showAppFeedback(
                          context,
                          title: '模型映射格式错误',
                          message: error.toString(),
                          error: true,
                        );
                      }
                    }
                  },
                ),
              ),
            ],
          ),
        ),
      );
    } finally {
      controller.dispose();
    }
  }

  Future<void> _editSubagentMaxConcurrent(
    BuildContext context,
    WidgetRef ref,
  ) async {
    var value = ref
        .read(settingsProvider)
        .subagentOrchestrationMaxConcurrent
        .toDouble();
    final selected = await showDialog<int>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (context, setState) => AlertDialog(
          title: Text('最大并发 ${value.round()}'),
          content: SizedBox(
            width: 300,
            height: 52,
            child: Slider(
              value: value,
              min: 1,
              max: 5,
              divisions: 4,
              label: value.round().toString(),
              onChanged: (next) => setState(() => value = next),
            ),
          ),
          actions: [
            IconButton(
              tooltip: '确认',
              icon: const Icon(Icons.check_rounded),
              onPressed: () => Navigator.of(dialogContext).pop(value.round()),
            ),
          ],
        ),
      ),
    );
    if (selected == null) return;
    try {
      await ref
          .read(settingsProvider.notifier)
          .setSubagentOrchestrationMaxConcurrent(selected);
    } catch (error) {
      if (context.mounted) {
        showAppFeedback(
          context,
          title: '更新子代理并发失败',
          message: error.toString(),
          error: true,
        );
      }
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final settings = ref.watch(settingsProvider);
    final notifier = ref.read(settingsProvider.notifier);

    return Scaffold(
      backgroundColor: AppColors.background,
      appBar: AppBar(
        backgroundColor: AppColors.surface,
        elevation: 0,
        surfaceTintColor: Colors.transparent,
        leading: IconButton(
          icon: const Icon(Icons.arrow_back_ios_new, size: 16),
          onPressed: () => Navigator.of(context).pop(),
        ),
        title: const Text(
          '设置',
          style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
        ),
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(1),
          child: Container(height: 1, color: AppColors.border),
        ),
      ),
      body: ListView(
        padding: const EdgeInsets.symmetric(vertical: 16, horizontal: 16),
        children: [
          _SectionHeader(title: '消息渲染'),
          _SettingsTile(
            icon: Icons.text_snippet_outlined,
            title: 'Markdown 渲染',
            subtitle: '将消息中的 Markdown 语法渲染为富文本',
            value: settings.markdownRender,
            onChanged: (_) => notifier.toggle('markdownRender'),
          ),
          _SettingsTile(
            icon: Icons.functions_outlined,
            title: 'LaTeX 渲染',
            subtitle: '渲染行内与块级 LaTeX 数学公式',
            value: settings.latexRender,
            onChanged: (_) => notifier.toggle('latexRender'),
            enabled: settings.markdownRender,
            disabledHint: '需要先开启 Markdown 渲染',
          ),
          _SettingsTile(
            icon: Icons.account_tree_outlined,
            title: 'Mermaid 图表',
            subtitle: '渲染 Mermaid 流程图、时序图等',
            value: settings.mermaidRender,
            onChanged: (_) => notifier.toggle('mermaidRender'),
            enabled: settings.markdownRender,
            disabledHint: '需要先开启 Markdown 渲染',
          ),
          _SettingsTile(
            icon: Icons.web_outlined,
            title: '自动预览生成物',
            subtitle: '将消息中含 CSS/JS 的 HTML 代码块渲染为可交互预览',
            value: settings.htmlPreview,
            onChanged: (_) => notifier.toggle('htmlPreview'),
            enabled: settings.markdownRender,
            disabledHint: '需要先开启 Markdown 渲染',
          ),
          const SizedBox(height: 8),
          _SectionHeader(title: '统计信息'),
          _SettingsTile(
            icon: Icons.timer_outlined,
            title: '显示首字耗时',
            subtitle: '在消息底部显示从发送到收到首个字符的时间',
            value: settings.showFirstTokenTime,
            onChanged: (_) => notifier.toggle('showFirstTokenTime'),
          ),
          _SettingsTile(
            icon: Icons.token_outlined,
            title: '显示 Token 消耗',
            subtitle: '在消息底部显示本次请求消耗的 Token 数',
            value: settings.showTokenCount,
            onChanged: (_) => notifier.toggle('showTokenCount'),
          ),
          _SettingsTile(
            icon: Icons.text_fields_outlined,
            title: '显示字数统计',
            subtitle: '在消息底部显示消息字数',
            value: settings.showWordCount,
            onChanged: (_) => notifier.toggle('showWordCount'),
          ),
          const SizedBox(height: 8),
          _SectionHeader(title: '输入增强'),
          _SettingsTile(
            icon: Icons.content_paste_outlined,
            title: '粘贴长文本为文件',
            subtitle:
                '粘贴超过 ${AppSettings.pasteAsFileThreshold} 字符的文本时，自动转为 .txt 附件',
            value: settings.pasteAsFile,
            onChanged: (_) => notifier.toggle('pasteAsFile'),
          ),
          _SettingsTile(
            icon: Icons.playlist_play_rounded,
            title: '终止后继续队列',
            subtitle: '停止当前任务后，自动开始下一条待发送消息',
            value: settings.chatQueueContinueAfterCancel,
            onChanged: (value) async {
              try {
                await notifier.setChatQueueContinueAfterCancel(value);
              } catch (e) {
                if (!context.mounted) return;
                showAppFeedback(
                  context,
                  title: '更新队列设置失败',
                  message: e.toString(),
                  error: true,
                );
              }
            },
          ),
          _SettingsTile(
            icon: Icons.open_in_new_rounded,
            title: '启动时进入最近任务',
            subtitle: '开启后优先进入等待你处理的问题，否则进入最近完成的 Agent 对话',
            value: settings.autoOpenRecentTask,
            onChanged: (_) => notifier.toggle('autoOpenRecentTask'),
          ),
          const SizedBox(height: 8),
          _SectionHeader(title: '项目记忆'),
          _SettingsTile(
            icon: Icons.memory_outlined,
            title: '全局项目记忆整理',
            subtitle: '作为所有项目的默认开关；项目可单独覆盖',
            value: settings.projectMemoryEnabled,
            onChanged: (value) async {
              try {
                await notifier.setProjectMemoryEnabled(value);
              } catch (error) {
                if (context.mounted) {
                  showAppFeedback(
                    context,
                    title: '更新项目记忆开关失败',
                    message: error.toString(),
                    error: true,
                  );
                }
              }
            },
          ),
          const SizedBox(height: 8),
          _SectionHeader(title: '子代理编排'),
          _SettingsTile(
            icon: Icons.account_tree_outlined,
            title: '自动子代理编排',
            subtitle: '主 Agent 可将探索、审查和测试任务拆分为受限子代理',
            value: settings.subagentOrchestrationEnabled,
            onChanged: (value) async {
              try {
                await notifier.setSubagentOrchestrationEnabled(value);
              } catch (error) {
                if (context.mounted) {
                  showAppFeedback(
                    context,
                    title: '更新子代理设置失败',
                    message: error.toString(),
                    error: true,
                  );
                }
              }
            },
          ),
          _SettingsActionTile(
            icon: Icons.speed_outlined,
            title: '最大并发',
            subtitle:
                '单个主任务最多同时运行 ${settings.subagentOrchestrationMaxConcurrent} 个子代理',
            actionLabel: '调整',
            onTap: settings.subagentOrchestrationEnabled
                ? () => _editSubagentMaxConcurrent(context, ref)
                : null,
          ),
          _SettingsActionTile(
            icon: Icons.tune_rounded,
            title: '子代理默认模型',
            subtitle: settings.subagentOrchestrationRoleModels.isEmpty
                ? '使用各内置子角色的默认模型'
                : '已为 ${settings.subagentOrchestrationRoleModels.length} 个角色设置模型',
            actionLabel: '编辑',
            onTap: settings.subagentOrchestrationEnabled
                ? () => _editSubagentRoleModels(context, ref)
                : null,
          ),
          const SizedBox(height: 8),
          _SectionHeader(title: '提示词'),
          _SettingsActionTile(
            icon: Icons.rule_folder_outlined,
            title: '全局提示词',
            subtitle: settings.globalPrompt.isNotEmpty
                ? settings.globalPrompt
                : '对所有新任务默认生效，可用于长期偏好和通用规则',
            actionLabel: settings.globalPrompt.isNotEmpty ? '已设置' : '编辑',
            onTap: () => _editGlobalPrompt(context, ref),
          ),
          const SizedBox(height: 8),
          _SectionHeader(title: '全局助手'),
          const _GlobalOverlayTile(),
          const SizedBox(height: 8),
          _SectionHeader(title: '通知提醒'),
          _SettingsTile(
            icon: Icons.email_outlined,
            title: '启用邮箱通知',
            subtitle: '开启后，AI 回复完成或等待审批时会发送邮件提醒',
            value: settings.emailNotificationEnabled,
            onChanged: (value) async {
              try {
                await notifier.setEmailNotificationEnabled(value);
              } catch (e) {
                if (!context.mounted) return;
                showAppFeedback(
                  context,
                  title: '更新邮箱通知设置失败',
                  message: e.toString(),
                  error: true,
                );
              }
            },
          ),
          if (defaultTargetPlatform == TargetPlatform.android)
            _SettingsTile(
              icon: Icons.notifications_active_outlined,
              title: '后台通知提醒',
              subtitle: 'App 切到后台时接收任务完成通知（需要前台服务权限）',
              value: settings.backgroundNotificationEnabled,
              onChanged: (value) async {
                try {
                  await notifier.setBackgroundNotificationEnabled(value);
                  if (!context.mounted) return;
                  if (value) {
                    showAppFeedback(
                      context,
                      message: '已启用后台通知，App 后台时会收到任务完成提醒',
                    );
                  } else {
                    showAppFeedback(
                      context,
                      message: '已关闭后台通知',
                    );
                  }
                } catch (e) {
                  if (!context.mounted) return;
                  showAppFeedback(
                    context,
                    title: '更新后台通知设置失败',
                    message: e.toString(),
                    error: true,
                  );
                }
              },
            ),
          const SizedBox(height: 8),
          _SectionHeader(title: '下载管理'),
          _SettingsActionTile(
            icon: Icons.folder_outlined,
            title: '下载目录',
            subtitle: settings.hasCustomDownloadDirectory
                ? settings.downloadDirectory!
                : _supportsDirectoryPicker
                ? '当前使用系统默认下载目录'
                : '当前使用浏览器默认下载目录',
            actionLabel: _supportsDirectoryPicker ? '选择目录' : '不可设置',
            onTap: _supportsDirectoryPicker
                ? () => _pickDownloadDirectory(context, ref)
                : null,
          ),
          if (_supportsDirectoryPicker && settings.hasCustomDownloadDirectory)
            _SettingsActionTile(
              icon: Icons.restart_alt_outlined,
              title: '恢复默认目录',
              subtitle: '清除自定义下载目录，回退到系统默认位置',
              actionLabel: '恢复默认',
              onTap: () => _resetDownloadDirectory(context, ref),
            ),
        ],
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

class _SettingsTile extends StatelessWidget {
  final IconData icon;
  final String title;
  final String subtitle;
  final bool value;
  final void Function(bool) onChanged;
  final bool enabled;
  final String? disabledHint;

  const _SettingsTile({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.value,
    required this.onChanged,
    this.enabled = true,
    this.disabledHint,
  });

  @override
  Widget build(BuildContext context) {
    final effectiveEnabled = enabled;
    return Opacity(
      opacity: effectiveEnabled ? 1.0 : 0.5,
      child: Container(
        margin: const EdgeInsets.only(bottom: 8),
        decoration: BoxDecoration(
          color: AppColors.surface,
          borderRadius: AppRadius.mdRadius,
          border: Border.all(color: AppColors.border),
        ),
        child: InkWell(
          onTap: effectiveEnabled
              ? () => onChanged(!value)
              : disabledHint != null
              ? () => showAppFeedback(context, message: disabledHint!)
              : null,
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
                          color: AppColors.textSecondary,
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 8),
                Switch(
                  value: value && effectiveEnabled,
                  onChanged: effectiveEnabled ? onChanged : null,
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

class _SettingsActionTile extends StatelessWidget {
  final IconData icon;
  final String title;
  final String subtitle;
  final String actionLabel;
  final VoidCallback? onTap;

  const _SettingsActionTile({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.actionLabel,
    this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final enabled = onTap != null;
    return Opacity(
      opacity: enabled ? 1.0 : 0.6,
      child: Container(
        margin: const EdgeInsets.only(bottom: 8),
        decoration: BoxDecoration(
          color: AppColors.surface,
          borderRadius: AppRadius.mdRadius,
          border: Border.all(color: AppColors.border),
        ),
        child: InkWell(
          onTap: onTap,
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
                          color: AppColors.textSecondary,
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 8),
                Text(
                  actionLabel,
                  style: TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                    color: enabled ? AppColors.primary : AppColors.textMuted,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _GlobalOverlayTile extends StatefulWidget {
  const _GlobalOverlayTile();

  @override
  State<_GlobalOverlayTile> createState() => _GlobalOverlayTileState();
}

class _GlobalOverlayTileState extends State<_GlobalOverlayTile> {
  bool _running = false;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final running = await GlobalOverlayService.isRunning();
    if (!mounted) return;
    setState(() {
      _running = running;
      _loading = false;
    });
  }

  Future<void> _toggle(bool value) async {
    if (_loading) return;
    if (!GlobalOverlayService.supported) {
      showAppFeedback(context, message: '系统悬浮窗目前仅支持 Android');
      return;
    }
    if (!value && _running && !await confirmGlobalOverlayStop(context)) return;
    if (!mounted) return;
    setState(() => _loading = true);
    try {
      if (value) {
        if (!await GlobalOverlayService.canDrawOverlays()) {
          await GlobalOverlayService.requestPermission();
          if (mounted) {
            showAppFeedback(context, message: '请在系统设置中允许悬浮窗权限，然后再次开启');
          }
          return;
        }
        final started = await GlobalOverlayService.start();
        if (!started && mounted) {
          showAppFeedback(context, message: '悬浮窗启动失败，请检查系统权限');
        }
      } else {
        await GlobalOverlayService.stop();
      }
      await _load();
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final supported = GlobalOverlayService.supported;
    return Opacity(
      opacity: supported ? 1 : 0.55,
      child: Container(
        margin: const EdgeInsets.only(bottom: 8),
        decoration: BoxDecoration(
          color: AppColors.surface,
          borderRadius: AppRadius.mdRadius,
          border: Border.all(color: AppColors.border),
        ),
        child: InkWell(
          onTap: supported && !_loading ? () => _toggle(!_running) : null,
          borderRadius: AppRadius.mdRadius,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
            child: Row(
              children: [
                const Icon(
                  Icons.open_in_new_rounded,
                  size: 18,
                  color: AppColors.textSecondary,
                ),
                const SizedBox(width: 12),
                const Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '全局悬浮对话',
                        style: TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.w500,
                          color: AppColors.textPrimary,
                        ),
                      ),
                      SizedBox(height: 2),
                      Text(
                        '离开应用后仍显示悬浮图标，可快速选择 Agent 并进入对话',
                        style: TextStyle(
                          fontSize: 12,
                          color: AppColors.textSecondary,
                        ),
                      ),
                    ],
                  ),
                ),
                if (_loading)
                  const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                else
                  Switch(
                    value: _running,
                    onChanged: supported ? _toggle : null,
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
