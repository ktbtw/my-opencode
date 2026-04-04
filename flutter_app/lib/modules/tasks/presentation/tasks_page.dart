import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/panel_card.dart';
import '../application/tasks_provider.dart';
import '../domain/task_summary.dart';

class TasksPage extends ConsumerStatefulWidget {
  const TasksPage({super.key});

  @override
  ConsumerState<TasksPage> createState() => _TasksPageState();
}

class _TasksPageState extends ConsumerState<TasksPage> {
  TaskSummary? selected;

  @override
  Widget build(BuildContext context) {
    final tasksValue = ref.watch(tasksProvider);
    final theme = Theme.of(context);

    return tasksValue.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (error, _) => PanelCard(
        title: 'Tasks',
        subtitle: '任务台账加载失败，请先确认后端任务列表接口已联通。',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('$error', style: theme.textTheme.bodyLarge),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: () => ref.refresh(tasksProvider),
              child: const Text('重新加载'),
            ),
          ],
        ),
      ),
      data: (tasks) {
        if (tasks.isEmpty) {
          return PanelCard(
            title: 'Tasks',
            subtitle: '当前还没有任务记录。后续从 Agents 或 Dashboard 下发任务后会在这里沉淀。',
            child: const SizedBox(
              height: 240,
              child: Center(child: Text('暂无任务记录')),
            ),
          );
        }

        selected ??= tasks.first;
        final current = tasks.firstWhere(
          (item) => item.taskId == selected?.taskId,
          orElse: () => tasks.first,
        );
        selected = current;

        return LayoutBuilder(
          builder: (context, constraints) {
            final summary = _TaskStats.fromTasks(tasks);
            if (constraints.maxWidth < 980) {
              return Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  _TaskStatsPanel(summary: summary),
                  const SizedBox(height: 16),
                  _TaskList(
                    tasks: tasks,
                    selected: current,
                    onTap: (value) => setState(() => selected = value),
                  ),
                  const SizedBox(height: 16),
                  _TaskDetail(task: current),
                ],
              );
            }

            return Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  flex: 6,
                  child: Column(
                    children: [
                      _TaskStatsPanel(summary: summary),
                      const SizedBox(height: 16),
                      _TaskList(
                        tasks: tasks,
                        selected: current,
                        onTap: (value) => setState(() => selected = value),
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 16),
                Expanded(flex: 4, child: _TaskDetail(task: current)),
              ],
            );
          },
        );
      },
    );
  }
}

class _TaskStatsPanel extends StatelessWidget {
  const _TaskStatsPanel({required this.summary});

  final _TaskStats summary;

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      title: 'Task Ledger',
      subtitle: '任务是串起执行、会话、审批的主线。这里先做最近任务台账，下一阶段接详情流和重试动作。',
      child: Wrap(
        spacing: 12,
        runSpacing: 12,
        children: [
          _StatTile(
            label: '总任务数',
            value: '${summary.total}',
            tone: const Color(0xFFE8E2D4),
          ),
          _StatTile(
            label: '运行中',
            value: '${summary.running}',
            tone: const Color(0xFFFFE7BF),
          ),
          _StatTile(
            label: '待审批',
            value: '${summary.waitingApproval}',
            tone: const Color(0xFFFFE0C7),
          ),
          _StatTile(
            label: '已完成',
            value: '${summary.completed}',
            tone: const Color(0xFFE3F0E7),
          ),
          _StatTile(
            label: '失败',
            value: '${summary.failed}',
            tone: const Color(0xFFF9D8D1),
          ),
        ],
      ),
    );
  }
}

class _TaskList extends StatelessWidget {
  const _TaskList({
    required this.tasks,
    required this.selected,
    required this.onTap,
  });

  final List<TaskSummary> tasks;
  final TaskSummary selected;
  final ValueChanged<TaskSummary> onTap;

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      title: 'Recent Tasks',
      subtitle: '按创建时间倒序。后续会补 agent、状态、项目等多维筛选。',
      child: Column(
        children: tasks
            .map(
              (task) => Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: InkWell(
                  borderRadius: BorderRadius.circular(22),
                  onTap: () => onTap(task),
                  child: Ink(
                    decoration: BoxDecoration(
                      color: selected.taskId == task.taskId
                          ? const Color(0xFFEAF3E8)
                          : const Color(0xFFF8F6EF),
                      borderRadius: BorderRadius.circular(22),
                      border: Border.all(
                        color: selected.taskId == task.taskId
                            ? const Color(0xFF14532D)
                            : const Color(0xFFD9D2C3),
                      ),
                    ),
                    padding: const EdgeInsets.all(18),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Expanded(
                              child: Text(
                                task.primaryPrompt,
                                maxLines: 2,
                                overflow: TextOverflow.ellipsis,
                                style: Theme.of(context).textTheme.titleMedium,
                              ),
                            ),
                            const SizedBox(width: 12),
                            _StatusBadge(status: task.status),
                          ],
                        ),
                        const SizedBox(height: 10),
                        Wrap(
                          spacing: 10,
                          runSpacing: 8,
                          children: [
                            _MetaChip(label: 'Task', value: task.taskId),
                            _MetaChip(label: 'Agent', value: task.agentId),
                            _MetaChip(label: 'Project', value: task.projectId),
                          ],
                        ),
                        if (task.updatedAt != null) ...[
                          const SizedBox(height: 10),
                          Text(
                            '更新于 ${_formatDateTime(task.updatedAt)}',
                            style: Theme.of(context).textTheme.bodyMedium,
                          ),
                        ],
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

class _TaskDetail extends StatelessWidget {
  const _TaskDetail({required this.task});

  final TaskSummary task;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return PanelCard(
      title: 'Task Detail',
      subtitle: '这里先放任务主信息、输入摘要和结果摘要。后续会在这里接事件流、取消、审批、继续对话。',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Wrap(
            spacing: 10,
            runSpacing: 10,
            children: [
              _StatusBadge(status: task.status),
              if (task.sessionId.isNotEmpty)
                _MetaChip(label: 'Session', value: task.sessionId),
            ],
          ),
          const SizedBox(height: 18),
          _DetailRow(label: 'Task ID', value: task.taskId),
          _DetailRow(label: 'Agent ID', value: task.agentId),
          _DetailRow(
            label: 'Machine ID',
            value: task.machineId.isEmpty ? '-' : task.machineId,
          ),
          _DetailRow(label: 'Project ID', value: task.projectId),
          _DetailRow(
            label: 'Project Root',
            value: task.projectRoot.isEmpty ? '-' : task.projectRoot,
          ),
          _DetailRow(label: 'Created', value: _formatDateTime(task.createdAt)),
          _DetailRow(label: 'Updated', value: _formatDateTime(task.updatedAt)),
          const SizedBox(height: 12),
          Text('输入 Parts', style: theme.textTheme.titleMedium),
          const SizedBox(height: 10),
          ...task.parts.map(
            (part) => Container(
              width: double.infinity,
              margin: const EdgeInsets.only(bottom: 10),
              padding: const EdgeInsets.all(14),
              decoration: BoxDecoration(
                color: const Color(0xFFF8F6EF),
                borderRadius: BorderRadius.circular(18),
                border: Border.all(color: const Color(0xFFD9D2C3)),
              ),
              child: Text(
                _describePart(part),
                style: theme.textTheme.bodyMedium,
              ),
            ),
          ),
          if (task.parts.isEmpty) const Text('当前没有记录到输入 parts。'),
          const SizedBox(height: 12),
          Text('执行结果', style: theme.textTheme.titleMedium),
          const SizedBox(height: 10),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              color: task.error.isEmpty
                  ? const Color(0xFFEFF4EC)
                  : const Color(0xFFFFF1EC),
              borderRadius: BorderRadius.circular(18),
              border: Border.all(
                color: task.error.isEmpty
                    ? const Color(0xFFCFE0D0)
                    : const Color(0xFFF2C7B8),
              ),
            ),
            child: Text(
              task.error.isNotEmpty
                  ? task.error
                  : (task.result.isEmpty ? '尚无结果输出' : task.result),
              style: theme.textTheme.bodyLarge,
            ),
          ),
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
        status,
        style: Theme.of(context).textTheme.bodyMedium?.copyWith(
          fontWeight: FontWeight.w700,
          color: tone.$2,
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
        '$label: $value',
        style: Theme.of(context).textTheme.bodyMedium,
      ),
    );
  }
}

class _DetailRow extends StatelessWidget {
  const _DetailRow({required this.label, required this.value});

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

class _StatTile extends StatelessWidget {
  const _StatTile({
    required this.label,
    required this.value,
    required this.tone,
  });

  final String label;
  final String value;
  final Color tone;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: 160,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: tone,
        borderRadius: BorderRadius.circular(20),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: theme.textTheme.bodyMedium),
          const SizedBox(height: 8),
          Text(value, style: theme.textTheme.headlineMedium),
        ],
      ),
    );
  }
}

class _TaskStats {
  const _TaskStats({
    required this.total,
    required this.running,
    required this.waitingApproval,
    required this.completed,
    required this.failed,
  });

  final int total;
  final int running;
  final int waitingApproval;
  final int completed;
  final int failed;

  factory _TaskStats.fromTasks(List<TaskSummary> tasks) {
    var running = 0;
    var waitingApproval = 0;
    var completed = 0;
    var failed = 0;
    for (final task in tasks) {
      switch (task.status) {
        case 'running':
        case 'dispatched':
          running++;
        case 'waiting_approval':
          waitingApproval++;
        case 'completed':
          completed++;
        case 'failed':
        case 'cancelled':
          failed++;
      }
    }
    return _TaskStats(
      total: tasks.length,
      running: running,
      waitingApproval: waitingApproval,
      completed: completed,
      failed: failed,
    );
  }
}

String _describePart(TaskPartSummary part) {
  switch (part.type) {
    case 'text':
      return part.text.isEmpty ? 'text part' : part.text;
    case 'file':
      return 'file: ${part.filename.isEmpty ? 'unnamed' : part.filename} (${part.mime.isEmpty ? 'unknown' : part.mime})';
    default:
      return part.type;
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
    case 'completed':
      return (const Color(0xFFE3F0E7), const Color(0xFF14532D));
    case 'running':
    case 'dispatched':
      return (const Color(0xFFFFE7BF), const Color(0xFF8A5300));
    case 'waiting_approval':
      return (const Color(0xFFFFE0C7), const Color(0xFF9A3412));
    case 'failed':
    case 'cancelled':
      return (const Color(0xFFF9D8D1), const Color(0xFF991B1B));
    default:
      return (const Color(0xFFE7E5DF), const Color(0xFF4B5563));
  }
}
