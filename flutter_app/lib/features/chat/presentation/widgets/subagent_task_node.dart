import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';
import '../../data/chat_model.dart';
import '../../data/subagent_models.dart';
import 'subagent_control_bar.dart';
import 'subagent_log_sheet.dart';

class SubagentTaskNode extends StatelessWidget {
  final String taskId;
  final SubagentNode node;
  final SubagentControlHandler onControl;
  final List<ModelInfo> availableModels;

  const SubagentTaskNode({
    super.key,
    required this.taskId,
    required this.node,
    required this.onControl,
    this.availableModels = const [],
  });

  @override
  Widget build(BuildContext context) {
    final color = switch (node.state) {
      'completed' => Colors.green,
      'failed' || 'timed_out' => AppColors.statusError,
      'cancelled' || 'blocked' => AppColors.textSecondary,
      'running' => AppColors.primary,
      _ => Colors.amber.shade800,
    };
    final label = node.title.isNotEmpty
        ? node.title
        : (node.role.isNotEmpty ? node.role : node.nodeId);
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: BorderRadius.circular(6),
        onTap: () => showSubagentLogSheet(context, taskId, node),
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 5),
          child: Row(
            children: [
              Icon(
                node.isRunning ? Icons.sync_rounded : Icons.circle,
                size: 14,
                color: color,
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      label,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 12,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    Text(
                      [
                        node.state,
                        if (node.attempt > 0) '第 ${node.attempt} 次',
                        if (node.dependsOn.isNotEmpty)
                          '依赖 ${node.dependsOn.join(', ')}',
                        if (node.blockedBy.isNotEmpty)
                          '阻塞于 ${node.blockedBy.join(', ')}',
                      ].join(' · '),
                      style: TextStyle(fontSize: 11, color: color),
                    ),
                  ],
                ),
              ),
              SubagentControlBar(
                node: node,
                onControl: onControl,
                availableModels: availableModels,
              ),
            ],
          ),
        ),
      ),
    );
  }
}
