import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';

class AuthBrandHeader extends StatelessWidget {
  final String eyebrow;
  final String title;
  final String description;
  final List<String> highlights;
  final bool compact;

  const AuthBrandHeader({
    super.key,
    required this.eyebrow,
    required this.title,
    required this.description,
    required this.highlights,
    this.compact = false,
  });

  @override
  Widget build(BuildContext context) {
    return CustomPaint(
      painter: const _AuthBrandPatternPainter(),
      child: Padding(
        padding: EdgeInsets.fromLTRB(
          compact ? 24 : 56,
          compact ? 30 : 56,
          compact ? 24 : 56,
          compact ? 24 : 56,
        ),
        child: compact ? _buildCompact() : _buildDesktop(),
      ),
    );
  }

  Widget _buildCompact() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const _AuthLogo(compact: true),
        const SizedBox(height: 22),
        Text(
          title,
          style: const TextStyle(
            color: AppColors.textPrimary,
            fontSize: 26,
            fontWeight: FontWeight.w700,
            height: 1.2,
          ),
        ),
        const SizedBox(height: 8),
        Text(
          description,
          style: const TextStyle(
            color: AppColors.textSecondary,
            fontSize: 13,
            height: 1.5,
          ),
        ),
      ],
    );
  }

  Widget _buildDesktop() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        const _AuthLogo(),
        const SizedBox(height: 52),
        Text(
          eyebrow,
          style: const TextStyle(
            color: AppColors.primary,
            fontFamily: 'monospace',
            fontSize: 11,
            fontWeight: FontWeight.w700,
            letterSpacing: 1.3,
          ),
        ),
        const SizedBox(height: 16),
        Text(
          title,
          style: const TextStyle(
            color: AppColors.textPrimary,
            fontSize: 40,
            fontWeight: FontWeight.w700,
            height: 1.15,
          ),
        ),
        const SizedBox(height: 18),
        ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 390),
          child: Text(
            description,
            style: const TextStyle(
              color: AppColors.textSecondary,
              fontSize: 15,
              height: 1.65,
            ),
          ),
        ),
        const SizedBox(height: 38),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: highlights
              .map(
                (item) => Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 10,
                    vertical: 8,
                  ),
                  decoration: BoxDecoration(
                    color: AppColors.surface.withValues(alpha: 0.72),
                    border: Border.all(color: AppColors.border),
                    borderRadius: BorderRadius.circular(7),
                  ),
                  child: Text(
                    item,
                    style: const TextStyle(
                      color: AppColors.textSecondary,
                      fontSize: 12,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
              )
              .toList(),
        ),
      ],
    );
  }
}

class _AuthLogo extends StatelessWidget {
  final bool compact;

  const _AuthLogo({this.compact = false});

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Container(
          width: compact ? 34 : 42,
          height: compact ? 34 : 42,
          decoration: BoxDecoration(
            color: AppColors.primaryLight,
            border: Border.all(color: AppColors.primaryMuted),
            borderRadius: BorderRadius.circular(compact ? 8 : 10),
          ),
          child: Icon(
            Icons.terminal_rounded,
            color: AppColors.primary,
            size: compact ? 18 : 22,
          ),
        ),
        SizedBox(width: compact ? 10 : 12),
        const Text(
          '码控',
          style: TextStyle(
            color: AppColors.textPrimary,
            fontSize: 24,
            fontWeight: FontWeight.w800,
            letterSpacing: 2,
          ),
        ),
        const SizedBox(width: 10),
        Text(
          'CODE CONTROL',
          style: TextStyle(
            color: AppColors.textMuted,
            fontFamily: 'monospace',
            fontSize: compact ? 8 : 9,
            fontWeight: FontWeight.w700,
            letterSpacing: 1,
          ),
        ),
      ],
    );
  }
}

class _AuthBrandPatternPainter extends CustomPainter {
  const _AuthBrandPatternPainter();

  @override
  void paint(Canvas canvas, Size size) {
    final gridPaint = Paint()
      ..color = AppColors.border.withValues(alpha: 0.28)
      ..strokeWidth = 1;
    for (var x = 0.0; x < size.width; x += 36) {
      canvas.drawLine(Offset(x, 0), Offset(x, size.height), gridPaint);
    }
    for (var y = 0.0; y < size.height; y += 36) {
      canvas.drawLine(Offset(0, y), Offset(size.width, y), gridPaint);
    }

    final tracePaint = Paint()
      ..color = AppColors.primary.withValues(alpha: 0.18)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.5;
    final path = Path()
      ..moveTo(size.width * 0.58, size.height * 0.18)
      ..lineTo(size.width * 0.82, size.height * 0.18)
      ..lineTo(size.width * 0.9, size.height * 0.28)
      ..lineTo(size.width, size.height * 0.28);
    canvas.drawPath(path, tracePaint);
    canvas.drawCircle(
      Offset(size.width * 0.58, size.height * 0.18),
      3,
      Paint()..color = AppColors.statusOnline.withValues(alpha: 0.75),
    );
  }

  @override
  bool shouldRepaint(covariant _AuthBrandPatternPainter oldDelegate) => false;
}
