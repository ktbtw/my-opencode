import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/theme/app_colors.dart';
import '../../../../core/theme/app_theme.dart';
import '../../../../shared/widgets/widgets.dart';
import '../../data/chat_model.dart';
import '../../data/subagent_models.dart';
import '../../data/subagent_repository.dart';
import '../message_renderer.dart';
import '../../../../features/settings/settings_provider.dart';

void showSubagentLogSheet(
  BuildContext context,
  String taskId,
  SubagentNode node,
) {
  showModalBottomSheet<void>(
    context: context,
    useRootNavigator: true,
    isScrollControlled: true,
    backgroundColor: Colors.transparent,
    barrierColor: Colors.black.withValues(alpha: .5),
    sheetAnimationStyle: const AnimationStyle(
      duration: Duration(milliseconds: 180),
      reverseDuration: Duration(milliseconds: 140),
    ),
    builder: (context) => _SubagentResultSheet(taskId: taskId, node: node),
  );
}

class _SubagentResultSheet extends ConsumerWidget {
  final String taskId;
  final SubagentNode node;

  const _SubagentResultSheet({required this.taskId, required this.node});

  bool get _failed => const {
    'failed',
    'timed_out',
    'cancelled',
    'blocked',
  }.contains(node.state);

  Color get _accent =>
      _failed ? AppColors.statusError : AppColors.statusSuccess;

  String get _statusText => switch (node.state) {
    'running' => '运行中',
    'queued' => '排队中',
    'failed' => '执行失败',
    'timed_out' => '执行超时',
    'cancelled' => '已取消',
    'blocked' => '被阻塞',
    'completed' => '已完成',
    _ => '已结束',
  };

  String get _role {
    return displaySubagentRole(node.role);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final title = node.title.trim().isEmpty ? _role : node.title.trim();
    return SafeArea(
      top: false,
      child: FractionallySizedBox(
        widthFactor: 1,
        heightFactor: .78,
        child: Container(
          width: double.infinity,
          decoration: const BoxDecoration(
            color: AppColors.surface,
            borderRadius: BorderRadius.vertical(top: Radius.circular(20)),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Center(
                child: Container(
                  width: 38,
                  height: 4,
                  margin: const EdgeInsets.only(top: 10),
                  decoration: BoxDecoration(
                    color: AppColors.border,
                    borderRadius: BorderRadius.circular(2),
                  ),
                ),
              ),
              Padding(
                padding: const EdgeInsets.fromLTRB(18, 15, 10, 12),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Container(
                      width: 34,
                      height: 34,
                      alignment: Alignment.center,
                      decoration: BoxDecoration(
                        color: _accent.withValues(alpha: .1),
                        borderRadius: AppRadius.smRadius,
                      ),
                      child: Icon(
                        _failed
                            ? Icons.error_outline_rounded
                            : Icons.account_tree_outlined,
                        color: _accent,
                        size: 20,
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            title,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 16,
                              fontWeight: FontWeight.w700,
                              color: AppColors.textPrimary,
                            ),
                          ),
                          const SizedBox(height: 4),
                          Text(
                            _role,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 12,
                              color: AppColors.textSecondary,
                            ),
                          ),
                        ],
                      ),
                    ),
                    IconButton(
                      tooltip: '关闭',
                      onPressed: () => Navigator.of(context).pop(),
                      icon: const Icon(Icons.close_rounded),
                      color: AppColors.textMuted,
                    ),
                  ],
                ),
              ),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 18),
                child: Row(
                  children: [
                    Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 8,
                        vertical: 4,
                      ),
                      decoration: BoxDecoration(
                        color: _accent.withValues(alpha: .1),
                        borderRadius: AppRadius.smRadius,
                      ),
                      child: Text(
                        _statusText,
                        style: TextStyle(
                          fontSize: 11,
                          fontWeight: FontWeight.w700,
                          color: _accent,
                        ),
                      ),
                    ),
                    if (node.attempt > 0) ...[
                      const SizedBox(width: 8),
                      Text(
                        '第 ${node.attempt} 次执行',
                        style: const TextStyle(
                          fontSize: 11,
                          color: AppColors.textMuted,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
              const SizedBox(height: 14),
              const Divider(height: 1, color: AppColors.borderLight),
              Expanded(
                child: _SubagentLogs(taskId: taskId, node: node),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _SubagentLogs extends ConsumerWidget {
  final String taskId;
  final SubagentNode node;

  const _SubagentLogs({required this.taskId, required this.node});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return FutureBuilder<SubagentLogPage>(
      future: SubagentRepository().getLogs(taskId, node.nodeId),
      builder: (context, snapshot) {
        final logs = snapshot.data?.items ?? const <SubagentLogEvent>[];
        final output = _cleanSubagentOutput(_displayOutput(logs));
        return ListView(
          padding: const EdgeInsets.fromLTRB(18, 16, 18, 24),
          children: [
            const Text(
              '执行输出',
              style: TextStyle(
                fontSize: 12,
                fontWeight: FontWeight.w700,
                color: AppColors.textSecondary,
              ),
            ),
            const SizedBox(height: 8),
            if (snapshot.connectionState == ConnectionState.waiting &&
                node.output.trim().isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 8),
                child: DialogContentSkeleton(itemCount: 3, showHeader: false),
              )
            else
              Container(
                width: double.infinity,
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: AppColors.surfaceElevated,
                  borderRadius: AppRadius.smRadius,
                  border: Border.all(color: AppColors.borderLight),
                ),
                child: MessageRenderer(
                  content: output,
                  isUser: false,
                  settings: ref.watch(settingsProvider),
                ),
              ),
            if (node.error.trim().isNotEmpty) ...[
              const SizedBox(height: 16),
              Text(
                '错误详情',
                style: TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w700,
                  color: AppColors.statusError,
                ),
              ),
              const SizedBox(height: 8),
              Text(
                node.error,
                style: const TextStyle(
                  fontSize: 12,
                  height: 1.5,
                  color: AppColors.statusError,
                ),
              ),
            ],
            if (snapshot.hasError) ...[
              const SizedBox(height: 12),
              const Text(
                '未能加载完整日志，当前展示已同步的结果。',
                style: TextStyle(fontSize: 11, color: AppColors.textMuted),
              ),
            ],
          ],
        );
      },
    );
  }

  String _displayOutput(List<SubagentLogEvent> logs) {
    if (logs.isNotEmpty) {
      final content = logs
          .map(
            (event) => [
              event.content,
              event.error,
            ].where((value) => value.trim().isNotEmpty).join('\n'),
          )
          .where((value) => value.isNotEmpty)
          .join('\n\n');
      if (content.trim().isNotEmpty) return content.trim();
    }
    final fallback = node.output.trim();
    return fallback.isEmpty ? '暂无输出' : fallback;
  }

  String _cleanSubagentOutput(String value) {
    const start = '<subagent_result>';
    const end = '</subagent_result>';
    final text = value.trim();
    final startIndex = text.indexOf(start);
    final endIndex = text.lastIndexOf(end);
    if (startIndex >= 0 && endIndex > startIndex) {
      return text.substring(startIndex + start.length, endIndex).trim();
    }
    return text;
  }
}
