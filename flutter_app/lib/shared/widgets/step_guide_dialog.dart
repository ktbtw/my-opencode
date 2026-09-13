import 'package:flutter/material.dart';

import '../../core/theme/app_colors.dart';
import '../../core/theme/app_theme.dart';

class GuideStep {
  final String title;
  final String description;
  final IconData icon;
  final List<String> details;

  const GuideStep({
    required this.title,
    required this.description,
    required this.icon,
    this.details = const [],
  });
}

Future<bool> showStepGuideDialog(
  BuildContext context, {
  required String title,
  required String subtitle,
  required List<GuideStep> steps,
  String finalActionLabel = '开始操作',
}) async {
  if (steps.isEmpty) return false;
  return await showDialog<bool>(
        context: context,
        barrierDismissible: true,
        builder: (_) => _StepGuideDialog(
          title: title,
          subtitle: subtitle,
          steps: steps,
          finalActionLabel: finalActionLabel,
        ),
      ) ??
      false;
}

class _StepGuideDialog extends StatefulWidget {
  final String title;
  final String subtitle;
  final List<GuideStep> steps;
  final String finalActionLabel;

  const _StepGuideDialog({
    required this.title,
    required this.subtitle,
    required this.steps,
    required this.finalActionLabel,
  });

  @override
  State<_StepGuideDialog> createState() => _StepGuideDialogState();
}

class _StepGuideDialogState extends State<_StepGuideDialog> {
  var _index = 0;

  bool get _isLast => _index == widget.steps.length - 1;

  void _next() {
    if (_isLast) {
      Navigator.of(context).pop(true);
      return;
    }
    setState(() => _index += 1);
  }

  @override
  Widget build(BuildContext context) {
    final step = widget.steps[_index];
    return AlertDialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
      titlePadding: const EdgeInsets.fromLTRB(24, 22, 24, 0),
      contentPadding: const EdgeInsets.fromLTRB(24, 12, 24, 0),
      actionsPadding: const EdgeInsets.fromLTRB(16, 8, 16, 14),
      title: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 38,
            height: 38,
            decoration: BoxDecoration(
              color: AppColors.primaryLight,
              borderRadius: AppRadius.smRadius,
            ),
            child: const Icon(
              Icons.menu_book_outlined,
              color: AppColors.primary,
              size: 20,
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(widget.title, maxLines: 2),
                const SizedBox(height: 4),
                Text(
                  widget.subtitle,
                  style: const TextStyle(
                    fontSize: 12,
                    height: 1.4,
                    color: AppColors.textSecondary,
                    fontWeight: FontWeight.normal,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 500),
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: ClipRRect(
                      borderRadius: AppRadius.smRadius,
                      child: LinearProgressIndicator(
                        minHeight: 5,
                        value: (_index + 1) / widget.steps.length,
                        backgroundColor: AppColors.borderLight,
                        valueColor: const AlwaysStoppedAnimation(
                          AppColors.primary,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Text(
                    '${_index + 1}/${widget.steps.length}',
                    style: const TextStyle(
                      color: AppColors.textMuted,
                      fontSize: 12,
                      fontFamily: 'monospace',
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 20),
              Container(
                width: double.infinity,
                padding: const EdgeInsets.all(18),
                decoration: BoxDecoration(
                  color: AppColors.surfaceElevated,
                  borderRadius: AppRadius.mdRadius,
                  border: Border.all(color: AppColors.borderLight),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(step.icon, color: AppColors.primary, size: 30),
                    const SizedBox(height: 14),
                    Text(
                      step.title,
                      style: const TextStyle(
                        color: AppColors.textPrimary,
                        fontSize: 17,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 8),
                    Text(
                      step.description,
                      style: const TextStyle(
                        color: AppColors.textSecondary,
                        fontSize: 13,
                        height: 1.55,
                      ),
                    ),
                    if (step.details.isNotEmpty) ...[
                      const SizedBox(height: 14),
                      ...step.details.map(
                        (detail) => Padding(
                          padding: const EdgeInsets.only(bottom: 8),
                          child: Row(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              const Padding(
                                padding: EdgeInsets.only(top: 5),
                                child: Icon(
                                  Icons.check_circle,
                                  size: 13,
                                  color: AppColors.statusOnline,
                                ),
                              ),
                              const SizedBox(width: 8),
                              Expanded(
                                child: Text(
                                  detail,
                                  style: const TextStyle(
                                    color: AppColors.textPrimary,
                                    fontSize: 12,
                                    height: 1.45,
                                  ),
                                ),
                              ),
                            ],
                          ),
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
      actions: [
        SizedBox(
          width: double.infinity,
          child: Row(
            children: [
              Expanded(
                child: TextButton(
                  onPressed: () => Navigator.of(context).pop(false),
                  style: TextButton.styleFrom(
                    minimumSize: const Size(0, 40),
                    padding: const EdgeInsets.symmetric(horizontal: 4),
                  ),
                  child: const FittedBox(
                    fit: BoxFit.scaleDown,
                    child: Text('稍后查看'),
                  ),
                ),
              ),
              if (_index > 0) ...[
                const SizedBox(width: 6),
                Expanded(
                  child: TextButton(
                    onPressed: () => setState(() => _index -= 1),
                    style: TextButton.styleFrom(
                      minimumSize: const Size(0, 40),
                      padding: const EdgeInsets.symmetric(horizontal: 4),
                    ),
                    child: const Text('上一步'),
                  ),
                ),
              ],
              const SizedBox(width: 6),
              Expanded(
                child: FilledButton.icon(
                  onPressed: _next,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size(0, 40),
                    padding: const EdgeInsets.symmetric(horizontal: 4),
                  ),
                  icon: Icon(
                    _isLast ? Icons.arrow_forward_rounded : Icons.chevron_right,
                    size: 17,
                  ),
                  label: FittedBox(
                    fit: BoxFit.scaleDown,
                    child: Text(_isLast ? widget.finalActionLabel : '下一步'),
                  ),
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}
