import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';
import '../../data/chat_model.dart';
import '../chat_provider.dart';
import 'inline_activity_hint.dart';

/// A small live status within the conversation flow. Only the 18px glyph
/// repaints on animation ticks; message text and Markdown are not rebuilt.
class AgentActivityIndicator extends StatelessWidget {
  final ChatMessage message;
  final bool stopping;
  final String statusHint;
  final String? operationTitle;
  final List<ChatOperationStep> operationSteps;
  final VoidCallback? onRetryHistory;

  const AgentActivityIndicator({
    super.key,
    required this.message,
    this.stopping = false,
    this.statusHint = '',
    this.operationTitle,
    this.operationSteps = const [],
    this.onRetryHistory,
  });

  bool get _hasText =>
      message.content.trim().isNotEmpty ||
      message.blocks.any(
        (block) =>
            block.type == ChatMessageBlockType.text &&
            block.text.trim().isNotEmpty,
      );

  /// 当前最值得展示的那一步：失败 > 进行中 > 最后完成 > 第一步。
  ChatOperationStep? get _flowStep {
    if (operationSteps.isEmpty) return null;
    for (final step in operationSteps) {
      if (step.state == ChatOperationStepState.failed) return step;
    }
    for (final step in operationSteps) {
      if (step.state == ChatOperationStepState.active) return step;
    }
    for (final step in operationSteps.reversed) {
      if (step.state == ChatOperationStepState.done) return step;
    }
    return operationSteps.first;
  }

  bool get _flowFailed =>
      operationSteps.any((step) => step.state == ChatOperationStepState.failed);

  String get _label {
    // 恢复 / 停止的流程状态直接写进这一行：标题 · 当前阶段，
    // 不再在气泡下另铺一层阶段条目。
    if (operationSteps.isNotEmpty) {
      final title = operationTitle?.trim() ?? '';
      final step = _flowStep;
      final stage = step == null ? '' : '${step.label} ${step.detail}'.trim();
      if (title.isEmpty) return stage.isEmpty ? '正在处理' : stage;
      return stage.isEmpty ? title : '$title · $stage';
    }
    if (stopping || message.statusHint == '正在停止') return '正在停止';
    if (statusHint.trim().isNotEmpty) return statusHint.trim();
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
    // 恢复 / 停止的流程阶段挂着时，即使气泡本身已收尾也要显示轻提示，
    // 否则流程会随气泡一起消失（原先由顶部进度条兜底）。
    if (operationSteps.isEmpty &&
        message.state != MessageState.streaming &&
        message.historyDetails == HistoryDetailsState.none) {
      return const SizedBox.shrink();
    }
    if (message.historyDetails == HistoryDetailsState.deferred) {
      return Align(
        alignment: Alignment.centerLeft,
        child: TextButton.icon(
          onPressed: onRetryHistory,
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

    final recovering = message.streamRecovery == StreamRecovery.syncing;
    final caption =
        recovering &&
            message.statusHint != '正在停止' &&
            !stopping &&
            !_hasText &&
            message.thinkingContent.isEmpty
        ? '稍等片刻，接上刚才的进度'
        : null;
    final canRetry =
        message.historyDetails == HistoryDetailsState.partial &&
        onRetryHistory != null;

    return InlineActivityHint(
      label: _label,
      emphasizeError: _flowFailed,
      caption: caption,
      animate:
          (message.state == MessageState.streaming &&
              message.historyDetails != HistoryDetailsState.deferred) ||
          message.historyDetails == HistoryDetailsState.loading ||
          operationSteps.isNotEmpty,
      onRetry: canRetry ? onRetryHistory : null,
    );
  }
}
