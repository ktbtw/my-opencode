import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../core/theme/app_colors.dart';
import '../../../../core/notifications/app_notification_feedback.dart';
import '../../data/chat_model.dart';
import '../subagent_tree_notifier.dart';
import 'subagent_task_node.dart';

class SubagentTaskTree extends ConsumerWidget {
  final String taskId;
  final String focusNodeId;
  final bool showEmptyState;
  final bool showHeader;
  final List<ModelInfo> availableModels;

  const SubagentTaskTree({
    super.key,
    required this.taskId,
    this.focusNodeId = '',
    this.showEmptyState = false,
    this.showHeader = true,
    this.availableModels = const [],
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(subagentTreeProvider(taskId));
    final nodes = state.snapshot.nodes;
    if (nodes.isEmpty && state.loading) {
      return const SizedBox(
        height: 28,
        child: Center(child: CircularProgressIndicator(strokeWidth: 2)),
      );
    }
    if (nodes.isEmpty) {
      if (!showEmptyState) return const SizedBox.shrink();
      return const Center(
        child: Padding(
          padding: EdgeInsets.symmetric(vertical: 28),
          child: Text(
            '当前任务还没有子代理',
            style: TextStyle(fontSize: 12, color: AppColors.textMuted),
          ),
        ),
      );
    }
    return Container(
      margin: const EdgeInsets.only(bottom: 14),
      padding: const EdgeInsets.fromLTRB(11, 8, 8, 8),
      decoration: BoxDecoration(
        color: AppColors.primaryMuted.withValues(alpha: .28),
        border: Border(left: BorderSide(color: AppColors.primary, width: 2)),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (showHeader) ...[
            const Row(
              children: [
                Icon(
                  Icons.account_tree_outlined,
                  size: 16,
                  color: AppColors.primary,
                ),
                SizedBox(width: 7),
                Text(
                  '子代理任务',
                  style: TextStyle(fontSize: 12, fontWeight: FontWeight.w700),
                ),
              ],
            ),
            const SizedBox(height: 4),
          ],
          for (final node in nodes)
            SubagentTaskNode(
              taskId: taskId,
              node: node,
              availableModels: availableModels,
              onControl: (action, {instruction = '', priority, model}) async {
                try {
                  await ref
                      .read(subagentTreeProvider(taskId).notifier)
                      .control(
                        node,
                        action,
                        instruction: instruction,
                        priority: priority,
                        model: model,
                      );
                } catch (error) {
                  if (context.mounted) {
                    showAppFeedback(
                      context,
                      title: '子代理操作失败',
                      message: error.toString(),
                      error: true,
                    );
                  }
                }
              },
            ),
        ],
      ),
    );
  }
}
