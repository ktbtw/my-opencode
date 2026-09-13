import 'package:flutter/material.dart';
import 'dart:math' as math;

import '../../../../core/theme/app_colors.dart';
import '../../../../core/theme/app_theme.dart';
import '../../../../shared/widgets/widgets.dart';

class RawConfigDialog extends StatefulWidget {
  final String path;
  final String warning;
  final String initialText;

  const RawConfigDialog({
    super.key,
    required this.path,
    required this.warning,
    required this.initialText,
  });

  @override
  State<RawConfigDialog> createState() => _RawConfigDialogState();
}

class _RawConfigDialogState extends State<RawConfigDialog> {
  late final TextEditingController _controller;

  @override
  void initState() {
    super.initState();
    _controller = TextEditingController(text: widget.initialText);
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final viewport = MediaQuery.sizeOf(context);
    final dialogHeight = math.min(820.0, math.max(360.0, viewport.height - 40));
    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
      shape: RoundedRectangleBorder(borderRadius: AppRadius.lgRadius),
      child: Container(
        constraints: BoxConstraints(maxWidth: 760, maxHeight: dialogHeight),
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              '编辑设备全局配置',
              style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 6),
            if (widget.path.isNotEmpty)
              Text(
                widget.path,
                style: const TextStyle(
                  fontSize: 12,
                  color: AppColors.textMuted,
                ),
              ),
            if (widget.warning.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text(
                widget.warning,
                style: const TextStyle(
                  fontSize: 12,
                  color: AppColors.statusWarning,
                ),
              ),
            ],
            const SizedBox(height: 12),
            Expanded(
              child: Container(
                width: double.infinity,
                decoration: BoxDecoration(
                  color: AppColors.inputBackground,
                  borderRadius: AppRadius.mdRadius,
                  border: Border.all(color: AppColors.border),
                ),
                child: TextField(
                  controller: _controller,
                  minLines: 20,
                  maxLines: null,
                  decoration: const InputDecoration(
                    hintText: '请输入完整的 opencode.json 内容',
                  ),
                  style: const TextStyle(
                    fontSize: 12,
                    height: 1.45,
                    fontFamily: 'monospace',
                  ),
                ),
              ),
            ),
            const SizedBox(height: 14),
            Row(
              children: [
                Expanded(
                  child: AppButton(
                    label: '取消',
                    outlined: true,
                    onPressed: () => Navigator.of(context).pop(),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: AppButton(
                    label: '保存配置',
                    onPressed: () =>
                        Navigator.of(context).pop(_controller.text),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
