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
        title: 'Agents',
        subtitle: '当前无法加载执行器列表，请先确认后端和 Web 联调链路。',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '$error',
              style: theme.textTheme.bodyLarge,
            ),
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
          return PanelCard(
            title: 'Agents',
            subtitle: '当前没有在线 agent。先启动一个真实 serve 或 mock agent。',
            child: const SizedBox(
              height: 220,
              child: Center(
                child: Text('暂无在线执行器'),
              ),
            ),
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
            if (constraints.maxWidth < 900) {
              return Column(
                children: [
                  _AgentsList(
                    agents: agents,
                    selected: current,
                    onTap: (value) => setState(() => selected = value),
                  ),
                  const SizedBox(height: 16),
                  _AgentDetail(agent: current),
                ],
              );
            }

            return Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  flex: 5,
                  child: _AgentsList(
                    agents: agents,
                    selected: current,
                    onTap: (value) => setState(() => selected = value),
                  ),
                ),
                const SizedBox(width: 16),
                Expanded(
                  flex: 4,
                  child: _AgentDetail(agent: current),
                ),
              ],
            );
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
    final theme = Theme.of(context);

    return PanelCard(
      title: 'Agents',
      subtitle: '调度的真实最小单位。这里展示在线执行器，而不是机器本身。',
      child: Column(
        children: agents
            .map(
              (agent) => Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: InkWell(
                  borderRadius: BorderRadius.circular(20),
                  onTap: () => onTap(agent),
                  child: Ink(
                    decoration: BoxDecoration(
                      color: agent.agentId == selected.agentId
                          ? const Color(0xFFE8F3EB)
                          : const Color(0xFFF8F6EF),
                      borderRadius: BorderRadius.circular(20),
                      border: Border.all(
                        color: agent.agentId == selected.agentId
                            ? const Color(0xFF14532D)
                            : const Color(0xFFD9D2C3),
                      ),
                    ),
                    padding: const EdgeInsets.all(18),
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
                        const SizedBox(width: 14),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                agent.agentId,
                                style: theme.textTheme.titleMedium,
                              ),
                              const SizedBox(height: 6),
                              Text(
                                'Machine: ${agent.machineId}',
                                style: theme.textTheme.bodyMedium,
                              ),
                              const SizedBox(height: 4),
                              Text(
                                'Projects: ${agent.projects.map((e) => e.projectId).join(', ')}',
                                style: theme.textTheme.bodyMedium,
                              ),
                            ],
                          ),
                        ),
                        const SizedBox(width: 12),
                        Container(
                          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                          decoration: BoxDecoration(
                            color: agent.currentTaskId.isEmpty
                                ? const Color(0xFFE3F0E7)
                                : const Color(0xFFFFE7BF),
                            borderRadius: BorderRadius.circular(999),
                          ),
                          child: Text(
                            agent.currentTaskId.isEmpty ? 'Idle' : 'Busy',
                            style: theme.textTheme.bodyMedium?.copyWith(
                              color: const Color(0xFF1A2433),
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            )
            .toList(),
      ),
    );
  }
}

class _AgentDetail extends StatelessWidget {
  const _AgentDetail({required this.agent});

  final AgentSummary agent;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final project = agent.projects.isNotEmpty ? agent.projects.first : null;

    return PanelCard(
      title: 'Agent Detail',
      subtitle: '这里预留后续发任务、查看最近任务、进入会话的操作位。',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _DetailRow(label: 'Agent ID', value: agent.agentId),
          _DetailRow(label: 'Machine ID', value: agent.machineId),
          _DetailRow(label: 'Hostname', value: agent.hostname),
          _DetailRow(label: 'Version', value: agent.version),
          _DetailRow(label: 'Current Task', value: agent.currentTaskId.isEmpty ? 'Idle' : agent.currentTaskId),
          const SizedBox(height: 18),
          Text(
            'Project Scope',
            style: theme.textTheme.titleMedium,
          ),
          const SizedBox(height: 12),
          if (project != null)
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: const Color(0xFFF8F6EF),
                borderRadius: BorderRadius.circular(18),
                border: Border.all(color: const Color(0xFFD9D2C3)),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(project.projectId, style: theme.textTheme.titleMedium),
                  const SizedBox(height: 8),
                  Text(project.root, style: theme.textTheme.bodyMedium),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class _DetailRow extends StatelessWidget {
  const _DetailRow({
    required this.label,
    required this.value,
  });

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: theme.textTheme.bodyMedium),
          const SizedBox(height: 4),
          Text(value, style: theme.textTheme.titleMedium),
        ],
      ),
    );
  }
}
