import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../settings_provider.dart';

class SettingsPage extends ConsumerWidget {
  const SettingsPage({super.key});

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
            subtitle: '粘贴超过 ${AppSettings.pasteAsFileThreshold} 字符的文本时，自动转为 .txt 附件',
            value: settings.pasteAsFile,
            onChanged: (_) => notifier.toggle('pasteAsFile'),
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
                  ? () => ScaffoldMessenger.of(context).showSnackBar(
                        SnackBar(
                          content: Text(disabledHint!),
                          duration: const Duration(seconds: 2),
                        ),
                      )
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
