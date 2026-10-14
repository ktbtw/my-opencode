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

  /// 当前流程阶段（手机 → 服务端 → OpenCode → 任务/回复）。
  /// 排障要看的"卡在哪一跳"就在这一句里，所以它进副标题而不是被合并掉。
  String? get _flowStage {
    final step = _flowStep;
    if (step == null) return null;
    final stage = '${step.label} ${step.detail}'.trim();
    return stage.isEmpty ? null : stage;
  }

  /// 主行只描述状态，不再把流程阶段拼进来：标题与阶段里各说一次"停止/恢复"
  /// 会让同一行出现两个同样的动词。阶段细节统一走 [_caption]。
  String get _label {
    if (operationSteps.isNotEmpty) {
      final title = operationTitle?.trim() ?? '';
      if (title.isNotEmpty) return title;
      return _flowStage ?? '正在处理';
    }
    if (stopping || message.statusHint == '正在停止') return '正在停止';
    if (statusHint.trim().isNotEmpty) return statusHint.trim();
    if (message.state != MessageState.streaming) {
      if (message.historyDetails == HistoryDetailsState.loading) {
        return '正在载入历史';
      }
      if (message.historyDetails == HistoryDetailsState.partial) {
        return '历史尚未载入完整';
      }
    }
    if (message.streamRecovery == StreamRecovery.syncing) return '正在同步回复';
    if (message.streamRecovery == StreamRecovery.reconnecting) return '正在恢复连接';
    if (message.historyDetails == HistoryDetailsState.loading) return '正在载入历史';
    if (message.historyDetails == HistoryDetailsState.partial) {
      return '历史尚未载入完整';
    }
    if (message.toolCalls.any((tool) => tool.isRunning)) return '正在使用工具';
    if (_hasText) return '正在生成回复';
    if (message.thinkingContent.trim().isNotEmpty) return '正在思考';
    if (message.toolCalls.isNotEmpty) return '正在整理工具结果';
    if (message.hasPlanBinding && message.plan?.isEmpty == false) {
      return '正在继续执行计划';
    }
    if ((message.taskId ?? '').isEmpty) return '正在发送消息';
    return '正在等待回复';
  }

  /// 副标题承载"具体在等什么/缺什么"：同一状态的多种措辞收敛到主行，
  /// 差异部分放这里，既不重复也不丢排障信息。
  String? get _caption {
    if (operationSteps.isNotEmpty) return _flowStage;
    if (stopping || message.statusHint == '正在停止') return null;
    if (statusHint.trim().isNotEmpty) return null;
    if (message.state != MessageState.streaming) {
      if (message.historyDetails == HistoryDetailsState.loading) {
        return '工具调用记录';
      }
      if (message.historyDetails == HistoryDetailsState.partial) {
        return '工具调用记录尚未完整';
      }
    }
    if (message.streamRecovery == StreamRecovery.syncing) {
      final quiet = !_hasText && message.thinkingContent.isEmpty;
      return quiet ? '稍等片刻，接上刚才的进度' : null;
    }
    if (message.streamRecovery == StreamRecovery.reconnecting) return null;
    if (message.historyDetails == HistoryDetailsState.loading) {
      return _hasText ? '补齐历史回复' : '恢复已有回复';
    }
    if (message.historyDetails == HistoryDetailsState.partial) {
      return _hasText ? '缺少部分回复正文' : '回复正文尚未载入';
    }
    if (message.streamRecovery == StreamRecovery.following) return '已接上正在运行的任务';
    return null;
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

    final canRetry =
        message.historyDetails == HistoryDetailsState.partial &&
        onRetryHistory != null;

    return InlineActivityHint(
      label: _label,
      emphasizeError: _flowFailed,
      caption: _caption,
      animate:
          (message.state == MessageState.streaming &&
              message.historyDetails != HistoryDetailsState.deferred) ||
          message.historyDetails == HistoryDetailsState.loading ||
          operationSteps.isNotEmpty,
      onRetry: canRetry ? onRetryHistory : null,
    );
  }
}
