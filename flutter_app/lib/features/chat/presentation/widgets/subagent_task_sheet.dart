import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';
import '../../data/chat_model.dart';
import 'subagent_task_tree.dart';

void showSubagentTaskSheet(
  BuildContext context, {
  required String taskId,
  List<ModelInfo> availableModels = const [],
}) {
  showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    builder: (context) => SafeArea(
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(context).height * .72,
        ),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Center(
                child: Container(
                  width: 36,
                  height: 4,
                  decoration: BoxDecoration(
                    color: AppColors.border,
                    borderRadius: BorderRadius.circular(2),
                  ),
                ),
              ),
              const SizedBox(height: 16),
              const Row(
                children: [
                  Icon(
                    Icons.account_tree_outlined,
                    size: 19,
                    color: AppColors.primary,
                  ),
                  SizedBox(width: 8),
                  Text(
                    '子代理任务',
                    style: TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
                  ),
                ],
              ),
              const SizedBox(height: 12),
              Flexible(
                child: SingleChildScrollView(
                  child: SubagentTaskTree(
                    taskId: taskId,
                    availableModels: availableModels,
                    showEmptyState: true,
                    showHeader: false,
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    ),
  );
}
