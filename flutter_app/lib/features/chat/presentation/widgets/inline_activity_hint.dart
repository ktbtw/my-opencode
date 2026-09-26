import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';

/// 对话内联轻提示：正文气泡下方的统一状态展示（回复 / 同步 / 恢复 / 停止 /
/// 历史补齐）。恢复与停止的流程阶段直接写进文案里，不再单独堆叠进度条。
class InlineActivityHint extends StatefulWidget {
  final String label;

  /// 失败等异常状态用错误色显示这一行文案。
  final bool emphasizeError;
  final String? caption;
  final bool animate;
  final VoidCallback? onRetry;
  final String retryLabel;

  const InlineActivityHint({
    super.key,
    required this.label,
    this.emphasizeError = false,
    this.caption,
    this.animate = true,
    this.onRetry,
    this.retryLabel = '重试',
  });

  @override
  State<InlineActivityHint> createState() => _InlineActivityHintState();
}

class _InlineActivityHintState extends State<InlineActivityHint>
    with SingleTickerProviderStateMixin, WidgetsBindingObserver {
  late final AnimationController _motion;
  bool _foreground = true;
  bool _reduceMotion = false;
  bool _tickerEnabled = true;

  @override
  void initState() {
    super.initState();
    _motion = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1600),
    );
    final lifecycle = WidgetsBinding.instance.lifecycleState;
    _foreground = lifecycle == null || lifecycle == AppLifecycleState.resumed;
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _reduceMotion = MediaQuery.disableAnimationsOf(context);
    _tickerEnabled = TickerMode.of(context);
    _updateMotion();
  }

  @override
  void didUpdateWidget(covariant InlineActivityHint oldWidget) {
    super.didUpdateWidget(oldWidget);
    _updateMotion();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _foreground = state == AppLifecycleState.resumed;
    _updateMotion();
  }

  void _updateMotion() {
    if (widget.animate && _foreground && _tickerEnabled && !_reduceMotion) {
      if (!_motion.isAnimating) _motion.repeat();
    } else {
      _motion.stop();
      _motion.value = .25;
    }
  }

  @override
  Widget build(BuildContext context) {
    final canRetry = widget.onRetry != null;
    return Semantics(
      liveRegion: true,
      label: canRetry ? '${widget.label}，${widget.retryLabel}' : widget.label,
      button: canRetry,
      onTap: widget.onRetry,
      child: ExcludeSemantics(
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 3),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Row(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.center,
                children: [
                  RepaintBoundary(
                    child: CustomPaint(
                      key: const ValueKey('agent-activity-glyph'),
                      size: const Size(18, 18),
                      painter: ActivityGlyph(_motion),
                    ),
                  ),
                  const SizedBox(width: 9),
                  Flexible(
                    child: AnimatedSwitcher(
                      duration: _reduceMotion
                          ? Duration.zero
                          : const Duration(milliseconds: 220),
                      // Fade in place so switching states never shifts the baseline.
                      layoutBuilder: (current, previous) => Stack(
                        alignment: Alignment.centerLeft,
                        children: [...previous, if (current != null) current],
                      ),
                      child: Text(
                        widget.label,
                        key: ValueKey(widget.label),
                        style: TextStyle(
                          fontSize: 12,
                          height: 1.6,
                          fontWeight: FontWeight.w500,
                          color: widget.emphasizeError
                              ? AppColors.statusError
                              : AppColors.textSecondary,
                        ),
                      ),
                    ),
                  ),
                  if (canRetry) ...[
                    const SizedBox(width: 4),
                    TextButton(
                      onPressed: widget.onRetry,
                      style: TextButton.styleFrom(
                        padding: const EdgeInsets.symmetric(horizontal: 6),
                        minimumSize: const Size(44, 44),
                        tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                      ),
                      child: Text(
                        widget.retryLabel,
                        style: const TextStyle(
                          fontSize: 12,
                          height: 1.6,
                          fontWeight: FontWeight.w500,
                        ),
                      ),
                    ),
                  ],
                ],
              ),
              if (widget.caption != null && widget.caption!.trim().isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(left: 27, top: 3),
                  child: Text(
                    widget.caption!,
                    style: const TextStyle(
                      fontSize: 11,
                      height: 1.6,
                      color: AppColors.textMuted,
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _motion.dispose();
    super.dispose();
  }
}

class ActivityGlyph extends CustomPainter {
  final Animation<double> motion;
  ActivityGlyph(this.motion) : super(repaint: motion);

  @override
  void paint(Canvas canvas, Size size) {
    for (var i = 0; i < 3; i++) {
      final wave = (1 - math.cos((motion.value + i / 6) * math.pi * 2)) / 2;
      final height = 4.5 + 6.5 * wave;
      final x = size.width / 2 + (i - 1) * 5;
      final paint = Paint()
        ..color = AppColors.primary.withValues(alpha: .42 + .58 * wave)
        ..strokeWidth = 2.5
        ..strokeCap = StrokeCap.round;
      canvas.drawLine(
        Offset(x, (size.height - height) / 2),
        Offset(x, (size.height + height) / 2),
        paint,
      );
    }
  }

  @override
  bool shouldRepaint(covariant ActivityGlyph oldDelegate) =>
      oldDelegate.motion != motion;
}
