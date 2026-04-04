import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/panel_card.dart';
import '../application/sessions_provider.dart';
import '../domain/session_summary.dart';

class SessionsPage extends ConsumerStatefulWidget {
  const SessionsPage({super.key});

  @override
  ConsumerState<SessionsPage> createState() => _SessionsPageState();
}

class _SessionsPageState extends ConsumerState<SessionsPage> {
  SessionSummary? selected;

  @override
  Widget build(BuildContext context) {
    final sessionsValue = ref.watch(sessionsProvider);
    final theme = Theme.of(context);

    return sessionsValue.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (error, _) => PanelCard(
        title: '会话',
        subtitle: '加载失败',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('$error', style: theme.textTheme.bodyLarge),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: () => ref.refresh(sessionsProvider),
              child: const Text('重新加载'),
            ),
          ],
        ),
      ),
      data: (sessions) {
        if (sessions.isEmpty) {
          return const PanelCard(
            title: '会话',
            child: SizedBox(height: 240, child: Center(child: Text('暂无会话记录'))),
          );
        }

        selected ??= sessions.first;
        final current = sessions.firstWhere(
          (item) => item.sessionId == selected?.sessionId,
          orElse: () => sessions.first,
        );
        selected = current;

        return LayoutBuilder(
          builder: (context, constraints) {
            final content = constraints.maxWidth < 980
                ? Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      _SessionList(
                        sessions: sessions,
                        selected: current,
                        onTap: (value) => setState(() => selected = value),
                      ),
                      const SizedBox(height: 20),
                      _SessionDetail(session: current),
                    ],
                  )
                : Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(
                        flex: 7,
                        child: _SessionList(
                          sessions: sessions,
                          selected: current,
                          onTap: (value) => setState(() => selected = value),
                        ),
                      ),
                      const SizedBox(width: 20),
                      Expanded(
                        flex: 5,
                        child: _SessionDetail(session: current),
                      ),
                    ],
                  );
            return SingleChildScrollView(child: content);
          },
        );
      },
    );
  }
}

class _SessionList extends StatelessWidget {
  const _SessionList({
    required this.sessions,
    required this.selected,
    required this.onTap,
  });

  final List<SessionSummary> sessions;
  final SessionSummary selected;
  final ValueChanged<SessionSummary> onTap;

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      title: '会话列表',
      child: Column(
        children: sessions
            .map(
              (session) => Padding(
                padding: const EdgeInsets.only(bottom: 14),
                child: _SessionListItem(
                  session: session,
                  selected: selected.sessionId == session.sessionId,
                  onTap: () => onTap(session),
                ),
              ),
            )
            .toList(),
      ),
    );
  }
}

class _SessionListItem extends StatelessWidget {
  const _SessionListItem({
    required this.session,
    required this.selected,
    required this.onTap,
  });

  final SessionSummary session;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return InkWell(
      borderRadius: BorderRadius.circular(24),
      onTap: onTap,
      child: Ink(
        decoration: BoxDecoration(
          color: selected ? const Color(0xFFEAF3E8) : const Color(0xFFF8F6EF),
          borderRadius: BorderRadius.circular(24),
          border: Border.all(
            color: selected ? const Color(0xFF14532D) : const Color(0xFFD9D2C3),
          ),
        ),
        child: Padding(
          padding: const EdgeInsets.all(18),
          child: ConstrainedBox(
            constraints: const BoxConstraints(minHeight: 126),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        session.summary.isEmpty
                            ? session.sessionId
                            : session.summary,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.titleMedium,
                      ),
                    ),
                    const SizedBox(width: 12),
                    _StatusBadge(status: session.status),
                  ],
                ),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 10,
                  runSpacing: 8,
                  children: [
                    _MetaChip(label: '会话', value: session.sessionId),
                    _MetaChip(label: '执行器', value: session.agentId),
                    _MetaChip(label: '项目', value: session.projectId),
                  ],
                ),
                if (session.updatedAt != null) ...[
                  const SizedBox(height: 12),
                  Text(
                    '更新时间 ${_formatDateTime(session.updatedAt)}',
                    style: theme.textTheme.bodyMedium,
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _SessionDetail extends StatelessWidget {
  const _SessionDetail({required this.session});

  final SessionSummary session;

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      title: '会话详情',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _InfoSection(
            title: '基础信息',
            children: [
              _DetailRow(label: '会话 ID', value: session.sessionId),
              _DetailRow(label: '执行器 ID', value: session.agentId),
              _DetailRow(label: '设备 ID', value: session.machineId),
              _DetailRow(label: '项目 ID', value: session.projectId),
              _DetailRow(label: '状态', value: _statusLabel(session.status)),
              _DetailRow(
                label: '最后任务',
                value: session.lastTaskId.isEmpty ? '-' : session.lastTaskId,
              ),
              _DetailRow(
                label: '创建时间',
                value: _formatDateTime(session.createdAt),
              ),
              _DetailRow(
                label: '更新时间',
                value: _formatDateTime(session.updatedAt),
                compact: true,
              ),
            ],
          ),
          const SizedBox(height: 16),
          _InfoSection(
            title: '摘要',
            children: [
              Container(
                width: double.infinity,
                padding: const EdgeInsets.all(16),
                decoration: BoxDecoration(
                  color: const Color(0xFFFFFCF6),
                  borderRadius: BorderRadius.circular(18),
                  border: Border.all(color: const Color(0xFFD9D2C3)),
                ),
                child: SelectableText(
                  session.summary.isEmpty ? '暂无摘要' : session.summary,
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _InfoSection extends StatelessWidget {
  const _InfoSection({required this.title, required this.children});

  final String title;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: const Color(0xFFF8F6EF),
        borderRadius: BorderRadius.circular(22),
        border: Border.all(color: const Color(0xFFD9D2C3)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(title, style: theme.textTheme.titleMedium),
          const SizedBox(height: 14),
          ...children,
        ],
      ),
    );
  }
}

class _StatusBadge extends StatelessWidget {
  const _StatusBadge({required this.status});

  final String status;

  @override
  Widget build(BuildContext context) {
    final tone = _statusTone(status);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: tone.$1,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        _statusLabel(status),
        style: Theme.of(context).textTheme.bodyMedium?.copyWith(
          color: tone.$2,
          fontWeight: FontWeight.w700,
        ),
      ),
    );
  }
}

class _MetaChip extends StatelessWidget {
  const _MetaChip({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: const Color(0xFFF1EDE3),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        '$label：$value',
        style: Theme.of(context).textTheme.bodyMedium,
      ),
    );
  }
}

class _DetailRow extends StatelessWidget {
  const _DetailRow({
    required this.label,
    required this.value,
    this.compact = false,
  });

  final String label;
  final String value;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: EdgeInsets.only(bottom: compact ? 0 : 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: theme.textTheme.bodyMedium),
          const SizedBox(height: 4),
          SelectableText(value, style: theme.textTheme.titleMedium),
        ],
      ),
    );
  }
}

String _formatDateTime(DateTime? value) {
  if (value == null) {
    return '-';
  }
  final month = value.month.toString().padLeft(2, '0');
  final day = value.day.toString().padLeft(2, '0');
  final hour = value.hour.toString().padLeft(2, '0');
  final minute = value.minute.toString().padLeft(2, '0');
  return '${value.year}-$month-$day $hour:$minute';
}

(Color, Color) _statusTone(String status) {
  switch (status) {
    case 'active':
      return (const Color(0xFFE3F0E7), const Color(0xFF14532D));
    case 'waiting_approval':
      return (const Color(0xFFFFE0C7), const Color(0xFF9A3412));
    case 'error':
      return (const Color(0xFFF9D8D1), const Color(0xFF991B1B));
    case 'cancelled':
      return (const Color(0xFFE7E5DF), const Color(0xFF4B5563));
    default:
      return (const Color(0xFFE7E5DF), const Color(0xFF4B5563));
  }
}

String _statusLabel(String status) {
  switch (status) {
    case 'active':
      return '进行中';
    case 'waiting_approval':
      return '待审批';
    case 'error':
      return '异常';
    case 'cancelled':
      return '已取消';
    default:
      return status;
  }
}
