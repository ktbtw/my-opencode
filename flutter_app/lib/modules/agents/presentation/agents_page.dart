import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/panel_card.dart';
import '../application/agents_provider.dart';
import '../domain/agent_summary.dart';

class AgentsPage extends ConsumerStatefulWidget {
  const AgentsPage({super.key});

  @override
  ConsumerState<AgentsPage> createState() => _AgentsPageState();
}

class _AgentsPageState extends ConsumerState<AgentsPage> {
  AgentSummary? selected;

  @override
  Widget build(BuildContext context) {
    final agentsValue = ref.watch(agentsProvider);
    final theme = Theme.of(context);

    return agentsValue.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (error, _) => PanelCard(
        title: '执行器',
        subtitle: '加载失败',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('$error', style: theme.textTheme.bodyLarge),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: () => ref.refresh(agentsProvider),
              child: const Text('重新加载'),
            ),
          ],
        ),
      ),
      data: (agents) {
        if (agents.isEmpty) {
          return const PanelCard(
            title: '执行器',
            child: SizedBox(height: 220, child: Center(child: Text('暂无在线执行器'))),
          );
        }

        selected ??= agents.first;
        final current = agents.firstWhere(
          (item) => item.agentId == selected?.agentId,
          orElse: () => agents.first,
        );
        selected = current;

        return LayoutBuilder(
          builder: (context, constraints) {
            final content = constraints.maxWidth < 920
                ? Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      _AgentsList(
                        agents: agents,
                        selected: current,
                        onTap: (value) => setState(() => selected = value),
                      ),
                      const SizedBox(height: 20),
                      _AgentDetail(agent: current),
                    ],
                  )
                : Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(
                        flex: 7,
                        child: _AgentsList(
                          agents: agents,
                          selected: current,
                          onTap: (value) => setState(() => selected = value),
                        ),
                      ),
                      const SizedBox(width: 20),
                      Expanded(flex: 5, child: _AgentDetail(agent: current)),
                    ],
                  );

            return SingleChildScrollView(child: content);
          },
        );
      },
    );
  }
}

class _AgentsList extends StatelessWidget {
  const _AgentsList({
    required this.agents,
    required this.selected,
    required this.onTap,
  });

  final List<AgentSummary> agents;
  final AgentSummary selected;
  final ValueChanged<AgentSummary> onTap;

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      title: '执行器',
      subtitle: '在线列表',
      child: Column(
        children: agents
            .map(
              (agent) => Padding(
                padding: const EdgeInsets.only(bottom: 14),
                child: _AgentListItem(
                  agent: agent,
                  selected: selected.agentId == agent.agentId,
                  onTap: () => onTap(agent),
                ),
              ),
            )
            .toList(),
      ),
    );
  }
}

class _AgentListItem extends StatelessWidget {
  const _AgentListItem({
    required this.agent,
    required this.selected,
    required this.onTap,
  });

  final AgentSummary agent;
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
          color: selected ? const Color(0xFFE8F3EB) : const Color(0xFFF8F6EF),
          borderRadius: BorderRadius.circular(24),
          border: Border.all(
            color: selected ? const Color(0xFF14532D) : const Color(0xFFD9D2C3),
          ),
        ),
        child: Padding(
          padding: const EdgeInsets.all(18),
          child: ConstrainedBox(
            constraints: const BoxConstraints(minHeight: 116),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Container(
                  width: 12,
                  height: 12,
                  margin: const EdgeInsets.only(top: 4),
                  decoration: BoxDecoration(
                    color: agent.currentTaskId.isEmpty
                        ? const Color(0xFF14532D)
                        : const Color(0xFFCC7A00),
                    borderRadius: BorderRadius.circular(999),
                  ),
                ),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(agent.agentId, style: theme.textTheme.titleMedium),
                      const SizedBox(height: 10),
                      Wrap(
                        spacing: 10,
                        runSpacing: 8,
                        children: [
                          _MetaChip(label: '设备', value: agent.machineId),
                          _MetaChip(
                            label: '项目',
                            value: agent.projects
                                .map((e) => e.projectId)
                                .join(', '),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 12),
                _StateBadge(text: agent.currentTaskId.isEmpty ? '空闲' : '运行中'),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _AgentDetail extends StatelessWidget {
  const _AgentDetail({required this.agent});

  final AgentSummary agent;

  @override
  Widget build(BuildContext context) {
    final project = agent.projects.isNotEmpty ? agent.projects.first : null;

    return PanelCard(
      title: '执行器详情',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _InfoSection(
            title: '基础信息',
            children: [
              _DetailRow(label: '执行器 ID', value: agent.agentId),
              _DetailRow(label: '设备 ID', value: agent.machineId),
              _DetailRow(label: '主机名', value: agent.hostname),
              _DetailRow(label: '版本', value: agent.version),
              _DetailRow(
                label: '当前任务',
                value: agent.currentTaskId.isEmpty ? '空闲' : agent.currentTaskId,
                compact: true,
              ),
            ],
          ),
          const SizedBox(height: 16),
          _InfoSection(
            title: '项目',
            children: [
              if (project != null)
                _DetailRow(label: '项目 ID', value: project.projectId),
              if (project != null)
                _DetailRow(label: '目录', value: project.root, compact: true),
              if (project == null) const Text('暂无项目信息'),
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

class _StateBadge extends StatelessWidget {
  const _StateBadge({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: text == '空闲' ? const Color(0xFFE3F0E7) : const Color(0xFFFFE7BF),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        text,
        style: Theme.of(context).textTheme.bodyMedium?.copyWith(
          color: const Color(0xFF1A2433),
          fontWeight: FontWeight.w700,
        ),
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
