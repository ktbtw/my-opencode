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
        title: '任务',
        subtitle: '加载失败',
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
          return const PanelCard(
            title: '任务',
            child: SizedBox(height: 240, child: Center(child: Text('暂无任务记录'))),
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
            final content = constraints.maxWidth < 980
                ? Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      _TaskList(
                        tasks: tasks,
                        selected: current,
                        onTap: (value) => setState(() => selected = value),
                      ),
                      const SizedBox(height: 20),
                      _TaskDetail(task: current),
                    ],
                  )
                : Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(
                        flex: 7,
                        child: _TaskList(
                          tasks: tasks,
                          selected: current,
                          onTap: (value) => setState(() => selected = value),
                        ),
                      ),
                      const SizedBox(width: 20),
                      Expanded(flex: 5, child: _TaskDetail(task: current)),
                    ],
                  );
            return SingleChildScrollView(child: content);
          },
        );
      },
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
      title: '任务列表',
      child: Column(
        children: tasks
            .map(
              (task) => Padding(
                padding: const EdgeInsets.only(bottom: 14),
                child: _TaskListItem(
                  task: task,
                  selected: selected.taskId == task.taskId,
                  onTap: () => onTap(task),
                ),
              ),
            )
            .toList(),
      ),
    );
  }
}

class _TaskListItem extends StatelessWidget {
  const _TaskListItem({
    required this.task,
    required this.selected,
    required this.onTap,
  });

  final TaskSummary task;
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
            constraints: const BoxConstraints(minHeight: 132),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(
                      child: Text(
                        task.primaryPrompt,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.titleMedium,
                      ),
                    ),
                    const SizedBox(width: 12),
                    _StatusBadge(status: task.status),
                  ],
                ),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 10,
                  runSpacing: 8,
                  children: [
                    _MetaChip(label: '任务', value: task.taskId),
                    _MetaChip(label: '执行器', value: task.agentId),
                    _MetaChip(label: '项目', value: task.projectId),
                  ],
                ),
                if (task.updatedAt != null) ...[
                  const SizedBox(height: 12),
                  Text(
                    '更新时间 ${_formatDateTime(task.updatedAt)}',
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

class _TaskDetail extends StatelessWidget {
  const _TaskDetail({required this.task});

  final TaskSummary task;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return PanelCard(
      title: '任务详情',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Wrap(
            spacing: 10,
            runSpacing: 10,
            children: [
              _StatusBadge(status: task.status),
              if (task.sessionId.isNotEmpty)
                _MetaChip(label: '会话', value: task.sessionId),
            ],
          ),
          const SizedBox(height: 16),
          _InfoSection(
            title: '基础信息',
            children: [
              _DetailRow(label: '任务 ID', value: task.taskId),
              _DetailRow(label: '执行器 ID', value: task.agentId),
              _DetailRow(
                label: '设备 ID',
                value: task.machineId.isEmpty ? '-' : task.machineId,
              ),
              _DetailRow(label: '项目 ID', value: task.projectId),
              _DetailRow(
                label: '项目目录',
                value: task.projectRoot.isEmpty ? '-' : task.projectRoot,
              ),
              _DetailRow(label: '创建时间', value: _formatDateTime(task.createdAt)),
              _DetailRow(
                label: '更新时间',
                value: _formatDateTime(task.updatedAt),
                compact: true,
              ),
            ],
          ),
          const SizedBox(height: 16),
          _InfoSection(
            title: '输入内容',
            children: task.parts.isEmpty
                ? const [Text('暂无输入内容')]
                : task.parts
                      .map(
                        (part) => Padding(
                          padding: const EdgeInsets.only(bottom: 10),
                          child: Container(
                            width: double.infinity,
                            padding: const EdgeInsets.all(14),
                            decoration: BoxDecoration(
                              color: const Color(0xFFFFFCF6),
                              borderRadius: BorderRadius.circular(18),
                              border: Border.all(
                                color: const Color(0xFFD9D2C3),
                              ),
                            ),
                            child: SelectableText(
                              _describePart(part),
                              style: theme.textTheme.bodyMedium,
                            ),
                          ),
                        ),
                      )
                      .toList(),
          ),
          const SizedBox(height: 16),
          _InfoSection(
            title: '执行结果',
            children: [
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
                child: SelectableText(
                  task.error.isNotEmpty
                      ? task.error
                      : (task.result.isEmpty ? '暂无结果' : task.result),
                  style: theme.textTheme.bodyLarge,
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

String _describePart(TaskPartSummary part) {
  switch (part.type) {
    case 'text':
      return part.text.isEmpty ? '文本' : part.text;
    case 'file':
      return '文件：${part.filename.isEmpty ? '未命名文件' : part.filename} (${part.mime.isEmpty ? '未知类型' : part.mime})';
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

String _statusLabel(String status) {
  switch (status) {
    case 'completed':
      return '已完成';
    case 'running':
      return '运行中';
    case 'dispatched':
      return '已派发';
    case 'waiting_approval':
      return '待审批';
    case 'failed':
      return '失败';
    case 'cancelled':
      return '已取消';
    case 'pending':
      return '等待中';
    default:
      return status;
  }
}
