import 'package:flutter/material.dart';

/// 通用「灯带」边框：正在执行的任务按边框有一条安静流转的光。
///
/// 设计成 1.8s 一圈的呼吸灯，安静、不跳闪——目的只是让「正在运行」一眼
/// 能被看见，与发送按钮之外的指示灯审美保持一致。
class RunningGlowBorder extends StatefulWidget {
  final Widget child;
  final bool active;

  /// 边框颜色与光源颜色，不传时走主题主色。
  final Color? color;
  final Color? background;
  final BorderRadiusGeometry borderRadius;
  final double borderWidth;

  const RunningGlowBorder({
    super.key,
    required this.child,
    required this.active,
    this.color,
    this.background,
    this.borderRadius = const BorderRadius.all(Radius.circular(10)),
    this.borderWidth = 1.5,
  });

  @override
  State<RunningGlowBorder> createState() => _RunningGlowBorderState();
}

class _RunningGlowBorderState extends State<RunningGlowBorder>
    with SingleTickerProviderStateMixin {
  static const _sweepDuration = Duration(milliseconds: 1800);
  static const _pulseDuration = Duration(milliseconds: 1100);
  late final AnimationController _sweep = AnimationController(
    vsync: this,
    duration: _sweepDuration,
  );
  late final AnimationController _pulse = AnimationController(
    vsync: this,
    duration: _pulseDuration,
  );

  @override
  void initState() {
    super.initState();
    if (widget.active) {
      _sweep.repeat();
      _pulse.repeat(reverse: true);
    }
  }

  @override
  void didUpdateWidget(covariant RunningGlowBorder oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.active && !oldWidget.active) {
      _sweep.repeat();
      _pulse.repeat(reverse: true);
    } else if (!widget.active && oldWidget.active) {
      _sweep.stop();
      _pulse.stop();
    }
  }

  @override
  void dispose() {
    _sweep.dispose();
    _pulse.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final baseColor =
        widget.color ?? Theme.of(context).colorScheme.primary;
    final radii = widget.borderRadius;
    if (!widget.active) {
      return DecoratedBox(
        decoration: BoxDecoration(
          color: widget.background,
          borderRadius: radii,
          border: Border.all(color: baseColor.withValues(alpha: 0.3)),
        ),
        child: widget.child,
      );
    }
    return AnimatedBuilder(
      animation: Listenable.merge([_sweep, _pulse]),
      builder: (context, child) {
        return CustomPaint(
          painter: _RunningGlowPainter(
            color: baseColor,
            sweep: _sweep.value,
            pulse: _pulse.value,
            borderWidth: widget.borderWidth,
            borderRadius: radii,
            background: widget.background,
          ),
          child: child,
        );
      },
      child: widget.child,
    );
  }
}

class _RunningGlowPainter extends CustomPainter {
  final Color color;
  final double sweep;
  final double pulse;
  final double borderWidth;
  final BorderRadiusGeometry borderRadius;
  final Color? background;

  const _RunningGlowPainter({
    required this.color,
    required this.sweep,
    required this.pulse,
    required this.borderWidth,
    required this.borderRadius,
    this.background,
  });

  @override
  void paint(Canvas canvas, Size size) {
    final rect = Offset.zero & size;
    final resolved = borderRadius.resolve(TextDirection.ltr);
    final rrect = resolved.toRRect(rect.deflate(borderWidth / 2));

    // 底层：稳定的底边框，不抢眼。
    if (background != null) {
      canvas.drawRRect(
        rrect.inflate(borderWidth / 2),
        Paint()..color = background!,
      );
    }
    canvas.drawRRect(
      rrect,
      Paint()
        ..color = color.withValues(alpha: 0.32 + pulse * 0.18)
        ..style = PaintingStyle.stroke
        ..strokeWidth = borderWidth,
    );

    // 上层：绕着边框转的一小段光。
    // 直接用实色笔触 + 模糊光晕，不依赖 SweepGradient 的角度对齐——
    // 路径起点在顶边而渐变零度在 3 点钟方向，之前两者错位导致光带落在
    // 渐变透明区，看起来永远只有底边框。
    final path = Path()..addRRect(rrect);
    final metric = path.computeMetrics().first;
    final start = metric.length * sweep;
    const spanRatio = 0.18;
    final span = metric.length * spanRatio;

    final segment = Path();
    for (var i = 0; i <= 30; i++) {
      final t = i / 30;
      final position = (start + span * t) % metric.length;
      final tangent = metric.getTangentForOffset(position);
      if (tangent == null) continue;
      final p = tangent.position;
      if (i == 0) {
        segment.moveTo(p.dx, p.dy);
      } else {
        segment.lineTo(p.dx, p.dy);
      }
    }

    // 光晕：宽而模糊的笔触
    canvas.drawPath(
      segment,
      Paint()
        ..color = color.withValues(alpha: 0.5)
        ..style = PaintingStyle.stroke
        ..strokeWidth = borderWidth * 2.6
        ..strokeCap = StrokeCap.round
        ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 2),
    );
    // 亮芯：实心窄笔触
    canvas.drawPath(
      segment,
      Paint()
        ..color = color.withValues(alpha: 1)
        ..style = PaintingStyle.stroke
        ..strokeWidth = borderWidth * 1.2
        ..strokeCap = StrokeCap.round,
    );
  }

  @override
  bool shouldRepaint(covariant _RunningGlowPainter oldDelegate) =>
      oldDelegate.sweep != sweep ||
      oldDelegate.pulse != pulse ||
      oldDelegate.color != color ||
      oldDelegate.borderWidth != borderWidth ||
      oldDelegate.background != background;
}
