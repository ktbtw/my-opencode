import 'dart:math' as math;
import 'package:flutter/material.dart';
import '../../../../core/theme/app_colors.dart';
import '../../data/chat_model.dart';

/// A small live status within the conversation flow. Only the 18px glyph
/// repaints on animation ticks; message text and Markdown are not rebuilt.
class AgentActivityIndicator extends StatefulWidget {
  final ChatMessage message;
  final bool stopping;
  final String statusHint;
  final VoidCallback? onRetryHistory;

  const AgentActivityIndicator({
    super.key,
    required this.message,
    this.stopping = false,
    this.statusHint = '',
    this.onRetryHistory,
  });

  @override
  State<AgentActivityIndicator> createState() => _AgentActivityIndicatorState();
}

class _AgentActivityIndicatorState extends State<AgentActivityIndicator>
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
  void didUpdateWidget(covariant AgentActivityIndicator oldWidget) {
    super.didUpdateWidget(oldWidget);
    _updateMotion();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _foreground = state == AppLifecycleState.resumed;
    _updateMotion();
  }

  void _updateMotion() {
    final active =
        (widget.message.state == MessageState.streaming &&
            widget.message.historyDetails != HistoryDetailsState.deferred) ||
        widget.message.historyDetails == HistoryDetailsState.loading;
    if (active && _foreground && _tickerEnabled && !_reduceMotion) {
      if (!_motion.isAnimating) _motion.repeat();
    } else {
      _motion.stop();
      _motion.value = .25;
    }
  }

  bool get _hasText =>
      widget.message.content.trim().isNotEmpty ||
      widget.message.blocks.any(
        (block) =>
            block.type == ChatMessageBlockType.text &&
            block.text.trim().isNotEmpty,
      );

  String get _label {
    final message = widget.message;
    if (widget.stopping || message.statusHint == '正在停止') return '正在停止';
    if (widget.statusHint.trim().isNotEmpty) return widget.statusHint.trim();
    if (message.state != MessageState.streaming) {
      if (message.historyDetails == HistoryDetailsState.loading) {
        return '正在恢复工具调用历史';
      }
      if (message.historyDetails == HistoryDetailsState.partial) {
        return '工具调用历史尚未完整载入';
      }
    }
    if (message.streamRecovery == StreamRecovery.syncing) return '正在同步最新回复';
    if (message.streamRecovery == StreamRecovery.reconnecting) {
      return '连接恢复中，正在重试';
    }
    if (message.historyDetails == HistoryDetailsState.loading) {
      return _hasText ? '正在补齐历史回复' : '正在恢复已有回复';
    }
    if (message.historyDetails == HistoryDetailsState.partial) {
      return _hasText ? '历史详情尚未补齐' : '历史回复尚未载入';
    }
    if (message.toolCalls.any((tool) => tool.isRunning)) return '正在使用工具';
    if (_hasText) return '正在生成回复';
    if (message.thinkingContent.trim().isNotEmpty) return '正在思考';
    if (message.toolCalls.isNotEmpty) return '正在整理工具结果';
    if (message.hasPlanBinding && message.plan?.isEmpty == false) {
      return '正在继续执行计划';
    }
    if ((message.taskId ?? '').isEmpty) return '正在发送消息';
    if (message.streamRecovery == StreamRecovery.following) return '等待新的回复';
    return '正在等待回复';
  }

  @override
  Widget build(BuildContext context) {
    if (widget.message.state != MessageState.streaming &&
        widget.message.historyDetails == HistoryDetailsState.none) {
      return const SizedBox.shrink();
    }
    if (widget.message.historyDetails == HistoryDetailsState.deferred) {
      return Align(
        alignment: Alignment.centerLeft,
        child: TextButton.icon(
          onPressed: widget.onRetryHistory,
          icon: const Icon(Icons.history_rounded, size: 16),
          label: const Text('查看工具调用', style: TextStyle(fontSize: 12)),
          style: TextButton.styleFrom(
            foregroundColor: AppColors.textSecondary,
            padding: const EdgeInsets.symmetric(horizontal: 8),
            minimumSize: const Size(44, 44),
          ),
        ),
      );
    }
    final label = _label;
    final recovering = widget.message.streamRecovery == StreamRecovery.syncing;
    final showHint =
        recovering &&
        widget.message.statusHint != '正在停止' &&
        !widget.stopping &&
        !_hasText &&
        widget.message.thinkingContent.isEmpty;

    final canRetry =
        widget.message.historyDetails == HistoryDetailsState.partial &&
        widget.onRetryHistory != null;
    return Semantics(
      liveRegion: true,
      label: canRetry ? '$label，重试' : label,
      button: canRetry,
      onTap: canRetry ? widget.onRetryHistory : null,
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
                      painter: _ActivityGlyph(_motion),
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
                        label,
                        key: ValueKey(label),
                        style: const TextStyle(
                          fontSize: 12,
                          height: 1.6,
                          fontWeight: FontWeight.w500,
                          color: AppColors.textSecondary,
                        ),
                      ),
                    ),
                  ),
                  if (canRetry) ...[
                    const SizedBox(width: 4),
                    TextButton(
                      onPressed: widget.onRetryHistory,
                      style: TextButton.styleFrom(
                        padding: const EdgeInsets.symmetric(horizontal: 6),
                        minimumSize: const Size(44, 44),
                        tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                      ),
                      child: const Text(
                        '重试',
                        style: TextStyle(
                          fontSize: 12,
                          height: 1.6,
                          fontWeight: FontWeight.w500,
                        ),
                      ),
                    ),
                  ],
                ],
              ),
              if (showHint)
                const Padding(
                  padding: EdgeInsets.only(left: 27, top: 3),
                  child: Text(
                    '稍等片刻，接上刚才的进度',
                    style: TextStyle(
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

class _ActivityGlyph extends CustomPainter {
  final Animation<double> motion;
  _ActivityGlyph(this.motion) : super(repaint: motion);

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
  bool shouldRepaint(covariant _ActivityGlyph oldDelegate) =>
      oldDelegate.motion != motion;
}
