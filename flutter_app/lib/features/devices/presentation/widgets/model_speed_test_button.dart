import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';
import '../../../../core/theme/app_theme.dart';
import '../../../chat/data/chat_model.dart';

Duration? modelSpeedTestDuration(ModelLatencyTestResult result) {
  if (result.firstTextTime != null) return result.firstTextTime;
  return result.totalTime;
}

String formatModelSpeedDuration(Duration duration) {
  final safe = duration.isNegative ? Duration.zero : duration;
  if (safe.inMilliseconds < 1000) return '${safe.inMilliseconds}ms';
  if (safe.inSeconds < 60) {
    return '${(safe.inMilliseconds / 1000).toStringAsFixed(1)}s';
  }
  final minutes = safe.inMinutes;
  final seconds = safe.inSeconds.remainder(60).toString().padLeft(2, '0');
  return '$minutes:$seconds';
}

class ModelSpeedTestButton extends StatelessWidget {
  final bool testing;
  final bool failed;
  final Duration? duration;
  final VoidCallback? onPressed;
  final String tooltip;

  const ModelSpeedTestButton({
    super.key,
    required this.testing,
    this.failed = false,
    this.duration,
    this.onPressed,
    this.tooltip = '测试模型连接',
  });

  @override
  Widget build(BuildContext context) {
    if (testing) {
      return Tooltip(
        message: '测试中',
        child: GestureDetector(
          onTap: () {},
          behavior: HitTestBehavior.opaque,
          child: const SizedBox(
            width: 44,
            height: 36,
            child: Center(
              child: SizedBox(
                width: 14,
                height: 14,
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  color: AppColors.primary,
                ),
              ),
            ),
          ),
        ),
      );
    }

    final completed = duration != null || failed;
    if (completed) {
      final label = failed && duration == null
          ? '失败'
          : formatModelSpeedDuration(duration ?? Duration.zero);
      final color = failed
          ? AppColors.statusError
          : (duration != null && duration!.inSeconds >= 10)
          ? AppColors.statusWarning
          : AppColors.statusSuccess;
      return Tooltip(
        message: failed ? '测试失败，点击重试' : '点击重新测试',
        child: InkWell(
          onTap: onPressed,
          borderRadius: AppRadius.smRadius,
          child: Container(
            constraints: const BoxConstraints(minWidth: 44, minHeight: 28),
            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 5),
            alignment: Alignment.center,
            child: Text(
              label,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: 10,
                fontWeight: FontWeight.w700,
                color: color,
              ),
            ),
          ),
        ),
      );
    }

    return IconButton(
      tooltip: tooltip,
      visualDensity: VisualDensity.compact,
      onPressed: onPressed,
      icon: Icon(
        Icons.speed_rounded,
        size: 17,
        color: onPressed == null ? AppColors.textMuted : AppColors.primary,
      ),
    );
  }
}
