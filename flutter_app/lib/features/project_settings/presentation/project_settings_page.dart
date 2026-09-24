import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/notifications/app_notification_feedback.dart';
import '../../../core/theme/app_colors.dart';
import '../../settings/settings_provider.dart';
import '../project_settings_provider.dart';

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
    return Scaffold(
      appBar: AppBar(title: const Text('项目设置')),
      body: ListView(
        padding: const EdgeInsets.symmetric(vertical: 16),
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 24),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  widget.projectName,
                  style: const TextStyle(
                    fontSize: 20,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                const SizedBox(height: 8),
                const Text(
                  '仅应用于当前账号在本设备上的这个项目。',
                  style: TextStyle(color: AppColors.textSecondary),
                ),
              ],
            ),
          ),
          SwitchListTile.adaptive(
            title: const Text('显示连接状态'),
            subtitle: const Text('在对话顶部显示连接延迟、接收速率和同步进度'),
            value: settings.showConnectionStatus,
            onChanged: enabled
                ? (value) => _save(
                    (notifier) => notifier.update(showConnectionStatus: value),
                  )
                : null,
          ),
          const Divider(height: 32, indent: 20, endIndent: 20),
          ListTile(
            leading: const Icon(Icons.folder_outlined),
            title: const Text('交付文件下载目录'),
            subtitle: Text(
              _directorySupported
                  ? settings.directoryLabel ??
                        settings.downloadDirectory ??
                        '沿用全局设置：${global.downloadDirectory ?? '系统默认下载目录'}'
                  : kIsWeb
                  ? '由浏览器管理下载位置'
                  : '使用应用默认目录',
            ),
            trailing: _directorySupported
                ? const Icon(Icons.chevron_right_rounded)
                : null,
            onTap: enabled && _directorySupported
                ? () => _save(_pickDirectory)
                : null,
          ),
          if (settings.downloadDirectory != null)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: Align(
                alignment: Alignment.centerLeft,
                child: TextButton(
                  onPressed: enabled
                      ? () => _save(
                          (notifier) => notifier.update(resetDirectory: true),
                        )
                      : null,
                  child: const Text('恢复为全局下载目录'),
                ),
              ),
            ),
          const Padding(
            padding: EdgeInsets.fromLTRB(20, 12, 20, 0),
            child: Text(
              '项目目录优先于全局目录。新的下载会使用此位置，已有文件保留原位。',
              style: TextStyle(fontSize: 13, color: AppColors.textSecondary),
            ),
          ),
          if (_saving)
            const Padding(
              padding: EdgeInsets.all(20),
              child: LinearProgressIndicator(),
            ),
        ],
      ),
    );
  }
}
