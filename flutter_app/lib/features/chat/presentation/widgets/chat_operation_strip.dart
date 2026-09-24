import 'package:flutter/material.dart';
import '../../../../core/theme/app_colors.dart';
import '../chat_provider.dart';

/// Compact, inline operation feedback. It keeps transport stages visible
/// without interrupting the conversation with another progress card.
class ChatOperationStrip extends StatelessWidget {
  final String? title;
  final List<ChatOperationStep> steps;

  const ChatOperationStrip({
    super.key,
    required this.title,
    required this.steps,
  });

  @override
  Widget build(BuildContext context) {
    if (steps.isEmpty) return const SizedBox.shrink();
    return AnimatedSize(
      duration: MediaQuery.disableAnimationsOf(context)
          ? Duration.zero
          : const Duration(milliseconds: 180),
      alignment: Alignment.topCenter,
      child: Container(
        width: double.infinity,
        padding: const EdgeInsets.fromLTRB(20, 7, 20, 8),
        decoration: const BoxDecoration(
          color: AppColors.surface,
          border: Border(bottom: BorderSide(color: AppColors.border)),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(
                  Icons.route_rounded,
                  size: 14,
                  color: AppColors.primary,
                ),
                const SizedBox(width: 7),
                Expanded(
                  child: Text(
                    title ?? '正在处理',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      fontSize: 11,
                      fontWeight: FontWeight.w600,
                      color: AppColors.textSecondary,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 6),
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  for (var i = 0; i < steps.length; i++) ...[
                    if (i > 0)
                      const Padding(
                        padding: EdgeInsets.symmetric(horizontal: 6),
                        child: Icon(
                          Icons.chevron_right_rounded,
                          size: 14,
                          color: AppColors.textMuted,
                        ),
                      ),
                    _Step(step: steps[i]),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _Step extends StatelessWidget {
  final ChatOperationStep step;
  const _Step({required this.step});

  @override
  Widget build(BuildContext context) {
    final (icon, color) = switch (step.state) {
      ChatOperationStepState.done => (
        Icons.check_circle_rounded,
        AppColors.statusSuccess,
      ),
      ChatOperationStepState.failed => (
        Icons.error_rounded,
        AppColors.statusError,
      ),
      ChatOperationStepState.active => (Icons.sync_rounded, AppColors.primary),
      ChatOperationStepState.pending => (
        Icons.radio_button_unchecked_rounded,
        AppColors.textMuted,
      ),
    };
    return Semantics(
      label: '${step.label}：${step.detail}',
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 14, color: color),
          const SizedBox(width: 4),
          Text(
            '${step.label} ${step.detail}',
            style: TextStyle(
              fontSize: 10,
              color: step.state == ChatOperationStepState.pending
                  ? AppColors.textMuted
                  : AppColors.textSecondary,
            ),
          ),
        ],
      ),
    );
  }
}
