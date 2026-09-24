import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../../core/services/app_foreground.dart';
import '../../../../core/services/app_live_events.dart';
import '../../../../core/services/backend_latency.dart';
import '../../../../core/services/agent_link_latency.dart';
import '../../../../core/services/connection_telemetry.dart';
import '../../../../core/services/provider_stream.dart';
import '../../../../core/theme/app_colors.dart';
import '../../data/chat_model.dart';
import '../chat_provider.dart';

final _connectionTickProvider = StreamProvider.autoDispose<int>(
  (ref) => providerOwnedStream(
    ref,
    Stream.periodic(const Duration(seconds: 1), (tick) => tick),
  ),
);

class ChatConnectionStatus extends ConsumerWidget {
  final String agentId;
  final String projectId;
  final bool visible;
  const ChatConnectionStatus({
    super.key,
    required this.agentId,
    required this.projectId,
    required this.visible,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final foreground = ref.watch(appForegroundProvider);
    if (!visible || !foreground) return const SizedBox.shrink();
    ref.watch(_connectionTickProvider);
    ref.watch(appLiveStreamProvider.select((value) => value.hasError));
    final events = ref.watch(appLiveEventsProvider);
    final latency = ref.watch(backendLatencyProvider);
    final agentLatency = ref.watch(agentLinkLatencyProvider(agentId));
    final key = (agentId, projectId);
    final activeTaskId = ref.watch(chatProvider(key).notifier).activeTaskId;
    final task = ref.watch(
      chatProvider(
        key,
      ).select((chat) => connectionTaskState(chat, activeTaskId)),
    );
    final repository = ref.watch(chatRepositoryProvider);
    final telemetry = task.$2 == null
        ? null
        : repository.taskConnection(task.$2!);
    return ConnectionStatusBar(
      snapshot: ConnectionStatusSnapshot.capture(
        global: events.connection,
        agent: events.agents[agentId],
        latency: latency,
        agentLatency: agentLatency,
        syncDetail: ref
            .read(chatProvider(key).notifier)
            .replySyncProgress
            ?.description,
        task: telemetry,
        taskActive: task.$1,
        syncing: task.$3,
      ),
    );
  }
}

/// Historical bubbles may retain a streaming flag while snapshots reconcile.
/// The header follows the task actually owned by the conversation controller.
(bool, String?, bool) connectionTaskState(
  ChatState chat,
  String? activeTaskId,
) {
  final active = chat.taskActive || chat.sending;
  final syncing =
      active &&
      activeTaskId != null &&
      chat.messages.any(
        (message) =>
            message.taskId == activeTaskId &&
            message.role == MessageRole.agent &&
            message.state == MessageState.streaming &&
            message.streamRecovery == StreamRecovery.syncing,
      );
  return (active, active ? activeTaskId : null, syncing);
}

class ConnectionStatusSnapshot {
  final String label;
  final bool healthy;
  final bool warning;
  final int? latencyMs;
  final int? agentLatencyMs;
  final String? agentLatencyDetail;
  final String? syncDetail;
  final int rate;
  final String service;
  final String agent;
  final String reply;
  final String lastOutput;
  final String confirmed;
  const ConnectionStatusSnapshot({
    required this.label,
    required this.healthy,
    this.warning = false,
    this.latencyMs,
    this.agentLatencyMs,
    this.agentLatencyDetail,
    this.syncDetail,
    this.rate = 0,
    required this.service,
    required this.agent,
    required this.reply,
    required this.lastOutput,
    required this.confirmed,
  });

  factory ConnectionStatusSnapshot.capture({
    required ConnectionTelemetry global,
    required AgentConnectionStatus? agent,
    required BackendLatency latency,
    AgentLinkLatency? agentLatency,
    String? syncDetail,
    ConnectionTelemetry? task,
    required bool taskActive,
    required bool syncing,
  }) {
    final globalHealthy = global.healthy;
    final agentKnown = globalHealthy && agent != null && agent.fresh;
    final agentOnline = agentKnown && agent.online;
    final taskHealthy = !taskActive || (task?.healthy ?? false);
    final otherTask =
        agentOnline && !taskActive && (agent.taskId?.isNotEmpty ?? false);
    final agentProbeSlow =
        agentOnline &&
        (agentLatency?.fresh ?? false) &&
        agentLatency?.status == 'timeout';
    final agentProbeMissing =
        agentOnline &&
        (agentLatency?.fresh ?? false) &&
        agentLatency?.status == 'offline';
    final healthy =
        globalHealthy &&
        agentOnline &&
        taskHealthy &&
        !syncing &&
        !otherTask &&
        !agentProbeSlow &&
        !agentProbeMissing;
    final serviceLost = !globalHealthy && latency.reachable == false;
    final agentOffline =
        agentKnown &&
        !agent.online &&
        !agent.transitioning &&
        agent.status != 'unknown';
    final label = serviceLost
        ? '服务连接中断'
        : !globalHealthy
        ? '正在恢复连接'
        : agentOffline
        ? 'Agent 离线'
        : agentKnown && agent.transitioning
        ? 'Agent 正在启动'
        : !agentOnline
        ? '正在确认 Agent 状态'
        : agentProbeSlow
        ? 'Agent 连接响应较慢'
        : agentProbeMissing
        ? '正在确认 Agent 连接'
        : otherTask
        ? 'Agent 正在执行任务'
        : syncing
        ? '正在同步对话'
        : !taskHealthy
        ? '正在连接回复通道'
        : '连接正常';
    return ConnectionStatusSnapshot(
      label: label,
      healthy: healthy,
      warning: serviceLost || agentOffline || agentProbeSlow,
      latencyMs: globalHealthy && latency.reachable == true
          ? latency.milliseconds
          : null,
      agentLatencyMs: agentOnline && (agentLatency?.fresh ?? false)
          ? agentLatency?.milliseconds
          : null,
      agentLatencyDetail: agentLatency?.description,
      syncDetail: syncDetail,
      rate: taskActive && !syncing && (task?.healthy ?? false)
          ? task!.charactersPerSecond
          : 0,
      service: globalHealthy
          ? '已连接'
          : serviceLost
          ? '连接中断'
          : '正在重连',
      agent: !agentKnown
          ? '待确认'
          : agentOnline
          ? '在线'
          : agent.transitioning
          ? '正在启动'
          : agent.status == 'unknown'
          ? '待确认'
          : '离线',
      reply: !taskActive
          ? '当前会话未接收实时回复'
          : syncing
          ? '正在同步'
          : taskHealthy
          ? '已连接'
          : '正在连接',
      lastOutput: task?.lastOutput == null
          ? '暂未收到新正文'
          : '${_age(task!.lastOutput!)}前',
      confirmed: !agentKnown ? '等待最新状态' : '${_age(agent.confirmedAt)}前',
    );
  }

  static String _age(DateTime time) {
    final seconds = DateTime.now().difference(time).inSeconds.clamp(0, 8640000);
    if (seconds < 60) return '$seconds 秒';
    if (seconds < 3600) return '${seconds ~/ 60} 分钟';
    return '${seconds ~/ 3600} 小时';
  }
}

/// Refreshes independently of the message list and expands in place.
class ConnectionStatusBar extends StatefulWidget {
  final ConnectionStatusSnapshot snapshot;
  const ConnectionStatusBar({super.key, required this.snapshot});
  @override
  State<ConnectionStatusBar> createState() => _ConnectionStatusBarState();
}

class _ConnectionStatusBarState extends State<ConnectionStatusBar> {
  bool _expanded = false;
  @override
  Widget build(BuildContext context) {
    final data = widget.snapshot;
    final color = data.healthy
        ? AppColors.statusSuccess
        : data.warning
        ? AppColors.statusError
        : AppColors.textMuted;
    return Material(
      color: AppColors.surface,
      child: DecoratedBox(
        decoration: const BoxDecoration(
          border: Border(bottom: BorderSide(color: AppColors.border)),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Semantics(
              button: true,
              expanded: _expanded,
              child: InkWell(
                key: const ValueKey('chat-connection-toggle'),
                onTap: () => setState(() => _expanded = !_expanded),
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(20, 8, 16, 10),
                  child: Row(
                    children: [
                      Icon(
                        data.warning ? Icons.wifi_off_rounded : Icons.circle,
                        size: data.warning ? 13 : 6,
                        color: color,
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(
                          [
                            data.label,
                            if (data.latencyMs != null)
                              '手机 ${data.latencyMs} ms',
                            if (data.agentLatencyMs != null)
                              'Agent ${data.agentLatencyMs} ms',
                            if (data.rate > 0) '接收 ${data.rate} 字/秒',
                          ].join(' · '),
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontSize: 11,
                            height: 1.5,
                            color: color,
                            fontFeatures: const [FontFeature.tabularFigures()],
                          ),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Icon(
                        _expanded
                            ? Icons.expand_less_rounded
                            : Icons.expand_more_rounded,
                        size: 16,
                        color: AppColors.textMuted,
                      ),
                    ],
                  ),
                ),
              ),
            ),
            AnimatedSize(
              duration: MediaQuery.disableAnimationsOf(context)
                  ? Duration.zero
                  : const Duration(milliseconds: 180),
              alignment: Alignment.topCenter,
              child: !_expanded
                  ? const SizedBox.shrink()
                  : Padding(
                      padding: const EdgeInsets.fromLTRB(34, 2, 20, 14),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          _detail(
                            '手机到服务端',
                            '${data.service}${data.latencyMs == null ? '' : ' · ${data.latencyMs} ms'}',
                          ),
                          _detail(
                            '服务端到 Agent',
                            '${data.agent}${data.agentLatencyDetail == null ? '' : ' · ${data.agentLatencyDetail}'}',
                          ),
                          if (data.syncDetail != null)
                            _detail('回复同步', data.syncDetail!),
                          _detail('回复接收', data.reply),
                          _detail('最近收到正文', data.lastOutput),
                          _detail('Agent 状态确认', data.confirmed),
                          const SizedBox(height: 7),
                          const Text(
                            '手机延迟为请求往返；Agent 延迟为服务端在现有连接上的 Ping/Pong 往返。同步耗时包含历史读取；接收速度为近 3 秒正文均值，包含恢复补传。',
                            style: TextStyle(
                              fontSize: 10,
                              height: 1.6,
                              color: AppColors.textMuted,
                            ),
                          ),
                        ],
                      ),
                    ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _detail(String label, String value) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 4),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Expanded(
          child: Text(
            label,
            style: const TextStyle(
              fontSize: 11,
              height: 1.5,
              color: AppColors.textMuted,
            ),
          ),
        ),
        const SizedBox(width: 12),
        Flexible(
          child: Text(
            value,
            textAlign: TextAlign.end,
            style: const TextStyle(
              fontSize: 11,
              height: 1.5,
              color: AppColors.textSecondary,
              fontFeatures: [FontFeature.tabularFigures()],
            ),
          ),
        ),
      ],
    ),
  );
}
