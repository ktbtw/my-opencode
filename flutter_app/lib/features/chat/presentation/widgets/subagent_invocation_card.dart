import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';
import '../../../../core/theme/app_theme.dart';
import '../../../../features/settings/settings_provider.dart';
import '../../data/chat_model.dart';
import '../../data/subagent_models.dart';
import '../message_renderer.dart';
import 'subagent_log_sheet.dart';

/// An inline, tool-like record of a delegated subagent. It stays in the
/// parent Agent's message stream instead of becoming a detached task card.
class SubagentInvocationCard extends StatefulWidget {
  final ToolCallInfo tool;
  final String injectedContent;
  final AppSettings settings;

  const SubagentInvocationCard({
    super.key,
    required this.tool,
    required this.settings,
    this.injectedContent = '',
  });

  bool get isInjection => injectedContent.trim().isNotEmpty;

  @override
  State<SubagentInvocationCard> createState() => _SubagentInvocationCardState();
}

class _SubagentInvocationCardState extends State<SubagentInvocationCard>
    with SingleTickerProviderStateMixin {
  late final AnimationController _rotationController;
  bool _expanded = false;

  bool get _isRunning =>
      _isLivePhase && widget.tool.isRunning && !widget.isInjection;
  bool get _failed => widget.tool.isError;
  bool get _isStartPhase =>
      (widget.tool.metadata['phase']?.toString().trim() ?? 'start') == 'start';
  bool get _isLivePhase =>
      (widget.tool.metadata['phase']?.toString().trim() ?? 'start') ==
      'running';
  bool get _canExpand => _resultText.isNotEmpty;

  @override
  void initState() {
    super.initState();
    _rotationController = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1500),
    );
    if (_isRunning) _rotationController.repeat();
  }

  @override
  void didUpdateWidget(covariant SubagentInvocationCard oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (_isRunning && !_rotationController.isAnimating) {
      _rotationController.repeat();
    } else if (!_isRunning && _rotationController.isAnimating) {
      _rotationController.stop();
      _rotationController.value = 0;
    }
  }

  @override
  void dispose() {
    _rotationController.dispose();
    super.dispose();
  }

  Color get _accent {
    if (_failed) return AppColors.statusError;
    if (_isRunning) return AppColors.primary;
    return AppColors.statusSuccess;
  }

  Color get _surface {
    if (_failed) return AppColors.statusErrorLight;
    if (_isRunning) return AppColors.primaryLight;
    return AppColors.statusSuccessLight;
  }

  String get _role {
    final raw = widget.tool.input['role']?.toString().trim() ?? '';
    return displaySubagentRole(raw);
  }

  String get _title {
    if (_isStartPhase) return '启动子代理 · $_role';
    if (_isLivePhase) return '正在执行 · $_role';
    return _failed ? '$_role 未完成' : '$_role 已完成';
  }

  String get _subtitle {
    final title = widget.tool.title.trim();
    return title.isEmpty ? '正在执行分配的工作' : title;
  }

  String get _resultText {
    final text = widget.injectedContent.trim().isNotEmpty
        ? widget.injectedContent.trim()
        : widget.tool.output.trim();
    const start = '<subagent_result>';
    const end = '</subagent_result>';
    final startIndex = text.indexOf(start);
    final endIndex = text.lastIndexOf(end);
    if (startIndex >= 0 && endIndex > startIndex) {
      return text.substring(startIndex + start.length, endIndex).trim();
    }
    return text;
  }

  void _openLog() {
    final taskId = widget.tool.metadata['task_id']?.toString().trim() ?? '';
    final nodeId = widget.tool.metadata['node_id']?.toString().trim() ?? '';
    if (taskId.isEmpty || nodeId.isEmpty) return;
    showSubagentLogSheet(
      context,
      taskId,
      SubagentNode(
        nodeId: nodeId,
        role: widget.tool.input['role']?.toString() ?? '',
        title: widget.tool.title,
        state: _failed ? 'failed' : (_isRunning ? 'running' : 'completed'),
        output: _resultText,
        error: widget.tool.error,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedContainer(
      duration: const Duration(milliseconds: 260),
      width: double.infinity,
      decoration: BoxDecoration(
        color: _surface,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: _accent.withValues(alpha: .24)),
      ),
      child: AnimatedSize(
        duration: const Duration(milliseconds: 190),
        curve: Curves.easeOutCubic,
        alignment: Alignment.topCenter,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            InkWell(
              onTap: _isRunning
                  ? _openLog
                  : _canExpand
                  ? () => setState(() => _expanded = !_expanded)
                  : _openLog,
              borderRadius: AppRadius.smRadius,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(10, 8, 7, 8),
                child: Row(
                  children: [
                    Container(
                      width: 24,
                      height: 24,
                      alignment: Alignment.center,
                      decoration: BoxDecoration(
                        color: _accent.withValues(alpha: .12),
                        borderRadius: AppRadius.smRadius,
                      ),
                      child: AnimatedSwitcher(
                        duration: const Duration(milliseconds: 280),
                        transitionBuilder: (child, animation) =>
                            ScaleTransition(scale: animation, child: child),
                        child: _isLivePhase && _isRunning
                            ? AnimatedBuilder(
                                key: const ValueKey('subagent-running'),
                                animation: _rotationController,
                                builder: (context, child) => Transform(
                                  alignment: Alignment.center,
                                  transform: Matrix4.identity()
                                    ..setEntry(3, 2, .0014)
                                    ..rotateY(
                                      _rotationController.value * math.pi * 2,
                                    ),
                                  child: child,
                                ),
                                child: CustomPaint(
                                  size: const Size.square(18),
                                  painter: _TechSubagentPainter(color: _accent),
                                ),
                              )
                            : Icon(
                                _failed
                                    ? Icons.error_outline_rounded
                                    : _isStartPhase
                                    ? Icons.account_tree_outlined
                                    : Icons.check_circle_rounded,
                                key: ValueKey(
                                  'subagent-${_failed ? 'failed' : 'done'}',
                                ),
                                size: 16,
                                color: _accent,
                              ),
                      ),
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            _title,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 13,
                              fontWeight: FontWeight.w700,
                              color: AppColors.textPrimary,
                            ),
                          ),
                          const SizedBox(height: 2),
                          Text(
                            _subtitle,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 12,
                              color: AppColors.textSecondary,
                            ),
                          ),
                        ],
                      ),
                    ),
                    IconButton(
                      tooltip: '查看子代理日志',
                      onPressed: _openLog,
                      visualDensity: VisualDensity.compact,
                      icon: const Icon(Icons.article_outlined, size: 18),
                      color: AppColors.textMuted,
                    ),
                    if (_canExpand)
                      Icon(
                        _expanded
                            ? Icons.expand_less_rounded
                            : Icons.expand_more_rounded,
                        size: 18,
                        color: AppColors.textMuted,
                      ),
                  ],
                ),
              ),
            ),
            if (_expanded) ...[
              Divider(height: 1, color: _accent.withValues(alpha: .18)),
              AnimatedOpacity(
                opacity: 1,
                duration: const Duration(milliseconds: 150),
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(11, 8, 11, 11),
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(maxHeight: 260),
                    child: SingleChildScrollView(
                      child: _resultText.isEmpty
                          ? const Text(
                              '子代理未返回可展示的输出。',
                              style: TextStyle(
                                fontSize: 12,
                                height: 1.5,
                                color: AppColors.textSecondary,
                              ),
                            )
                          : MessageRenderer(
                              content: _resultText,
                              isUser: false,
                              settings: widget.settings,
                            ),
                    ),
                  ),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// A symmetric node mark reads clearly while it rotates around the vertical
/// axis. The hexagonal frame keeps the motion technical without resembling a
/// generic loading spinner.
class _TechSubagentPainter extends CustomPainter {
  final Color color;

  const _TechSubagentPainter({required this.color});

  @override
  void paint(Canvas canvas, Size size) {
    final center = Offset(size.width / 2, size.height / 2);
    final frame = Paint()
      ..color = color
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.15
      ..strokeJoin = StrokeJoin.round;
    final radius = size.shortestSide * .34;
    final outer = Path();
    for (var i = 0; i < 6; i++) {
      final angle = -math.pi / 2 + i * math.pi / 3;
      final point = center + Offset(math.cos(angle), math.sin(angle)) * radius;
      if (i == 0) {
        outer.moveTo(point.dx, point.dy);
      } else {
        outer.lineTo(point.dx, point.dy);
      }
    }
    outer.close();
    canvas.drawPath(outer, frame);
    canvas.drawCircle(center, size.shortestSide * .19, frame);
    canvas.drawCircle(center, size.shortestSide * .075, Paint()..color = color);
    for (var i = 0; i < 6; i++) {
      final angle = -math.pi / 2 + i * math.pi / 3;
      final point = center + Offset(math.cos(angle), math.sin(angle)) * radius;
      canvas.drawCircle(
        point,
        size.shortestSide * .045,
        Paint()..color = color,
      );
    }
  }

  @override
  bool shouldRepaint(covariant _TechSubagentPainter oldDelegate) {
    return oldDelegate.color != color;
  }
}
