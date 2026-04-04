import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/shell_card.dart';
import '../../../shared/widgets/surface_scaffold.dart';
import '../../auth/application/auth_controller.dart';
import '../data/devices_api.dart';
import '../domain/device_models.dart';

final deviceDetailProvider = FutureProvider.family<DeviceInfo, String>((
  ref,
  machineId,
) async {
  final auth = ref.watch(authControllerProvider);
  return ref.watch(devicesApiProvider).getDevice(auth.accessToken, machineId);
});

class DeviceDetailPage extends ConsumerWidget {
  const DeviceDetailPage({super.key, required this.machineId});

  final String machineId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final deviceValue = ref.watch(deviceDetailProvider(machineId));
    return SurfaceScaffold(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: deviceValue.when(
          loading: () => const Center(child: CircularProgressIndicator()),
          error: (error, _) => Center(child: Text('$error')),
          data: (device) => Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  FilledButton.tonal(
                    onPressed: () => context.go('/devices'),
                    child: const Text('返回设备'),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(
                      device.machineId,
                      style: Theme.of(context).textTheme.headlineMedium,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 20),
              Expanded(
                child: LayoutBuilder(
                  builder: (context, constraints) {
                    final mobile = constraints.maxWidth < 960;
                    final content = mobile
                        ? Column(
                            children: [
                              _DeviceInfoCard(device: device),
                              const SizedBox(height: 20),
                              _AgentPanel(device: device),
                            ],
                          )
                        : Row(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Expanded(
                                flex: 5,
                                child: _DeviceInfoCard(device: device),
                              ),
                              const SizedBox(width: 20),
                              Expanded(
                                flex: 7,
                                child: _AgentPanel(device: device),
                              ),
                            ],
                          );
                    return SingleChildScrollView(child: content);
                  },
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _DeviceInfoCard extends StatelessWidget {
  const _DeviceInfoCard({required this.device});

  final DeviceInfo device;

  @override
  Widget build(BuildContext context) {
    return ShellCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('设备信息', style: Theme.of(context).textTheme.titleLarge),
          const SizedBox(height: 18),
          _InfoLine(label: '设备 ID', value: device.machineId),
          _InfoLine(label: '主机名', value: device.hostname),
          _InfoLine(label: '状态', value: device.status),
          _InfoLine(label: 'Agent 数量', value: '${device.agents.length}'),
          _InfoLine(label: '最近在线', value: _formatDateTime(device.seenAt)),
        ],
      ),
    );
  }
}

class _AgentPanel extends StatelessWidget {
  const _AgentPanel({required this.device});

  final DeviceInfo device;

  @override
  Widget build(BuildContext context) {
    return ShellCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('执行器', style: Theme.of(context).textTheme.titleLarge),
          const SizedBox(height: 18),
          for (final agent in device.agents) ...[
            _AgentCard(agent: agent, machineId: device.machineId),
            if (agent != device.agents.last) const SizedBox(height: 14),
          ],
        ],
      ),
    );
  }
}

class _AgentCard extends StatelessWidget {
  const _AgentCard({required this.agent, required this.machineId});

  final AgentInfo agent;
  final String machineId;

  @override
  Widget build(BuildContext context) {
    final projectId = agent.projects.isNotEmpty
        ? agent.projects.first.projectId
        : '';
    final root = agent.projects.isNotEmpty ? agent.projects.first.root : '';
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: const Color(0xFFF7FBFF),
        borderRadius: BorderRadius.circular(24),
        border: Border.all(color: const Color(0xFFD4E5FF)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  agent.agentId,
                  style: Theme.of(context).textTheme.titleMedium,
                ),
              ),
              FilledButton(
                onPressed: () => context.go(
                  '/chat?machine_id=$machineId&agent_id=${agent.agentId}&project_id=$projectId',
                ),
                child: const Text('进入对话'),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Text('项目：${projectId.isEmpty ? '-' : projectId}'),
          const SizedBox(height: 6),
          Text('目录：${root.isEmpty ? '-' : root}'),
          const SizedBox(height: 6),
          Text('版本：${agent.version.isEmpty ? '-' : agent.version}'),
          const SizedBox(height: 6),
          Text(
            '当前任务：${agent.currentTaskId.isEmpty ? '空闲' : agent.currentTaskId}',
          ),
        ],
      ),
    );
  }
}

class _InfoLine extends StatelessWidget {
  const _InfoLine({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: Theme.of(context).textTheme.bodyMedium),
          const SizedBox(height: 4),
          SelectableText(
            value.isEmpty ? '-' : value,
            style: Theme.of(context).textTheme.titleMedium,
          ),
        ],
      ),
    );
  }
}

String _formatDateTime(DateTime? value) {
  if (value == null) return '-';
  final month = value.month.toString().padLeft(2, '0');
  final day = value.day.toString().padLeft(2, '0');
  final hour = value.hour.toString().padLeft(2, '0');
  final minute = value.minute.toString().padLeft(2, '0');
  return '${value.year}-$month-$day $hour:$minute';
}
