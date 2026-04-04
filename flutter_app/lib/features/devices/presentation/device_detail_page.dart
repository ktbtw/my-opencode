import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import '../data/device_model.dart';
import 'device_provider.dart';

class DeviceDetailPage extends ConsumerWidget {
  final String machineId;
  const DeviceDetailPage({super.key, required this.machineId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final deviceAsync = ref.watch(deviceDetailProvider(machineId));
    final isMobile = AppBreakpoints.isMobile(context);

    return Scaffold(
      body: PageBackground(
        child: Column(
          children: [
            _buildTopBar(context),
            Expanded(
              child: deviceAsync.when(
                loading: () => const LoadingState(),
                error: (e, _) => Center(
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Icon(Icons.error_outline,
                          size: 40, color: AppColors.statusError),
                      const SizedBox(height: 12),
                      Text(e.toString(),
                          style: const TextStyle(color: AppColors.statusError)),
                      const SizedBox(height: 16),
                      AppButton(
                        label: '重试',
                        outlined: true,
                        onPressed: () =>
                            ref.invalidate(deviceDetailProvider(machineId)),
                      ),
                    ],
                  ),
                ),
                data: (device) => isMobile
                    ? _buildMobileLayout(context, device)
                    : _buildDesktopLayout(context, device),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildTopBar(BuildContext context) {
    return Container(
      height: 56,
      decoration: const BoxDecoration(
        color: AppColors.surface,
        border: Border(bottom: BorderSide(color: AppColors.border)),
      ),
      padding: const EdgeInsets.symmetric(horizontal: 20),
      child: Row(
        children: [
          IconButton(
            icon: const Icon(Icons.arrow_back_ios_new, size: 16),
            onPressed: () => context.go('/devices'),
          ),
          const SizedBox(width: 4),
          Text('设备详情', style: Theme.of(context).textTheme.titleLarge),
        ],
      ),
    );
  }

  Widget _buildDesktopLayout(BuildContext context, DeviceModel device) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // 左侧设备信息
        SizedBox(
          width: 320,
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: _DeviceInfoPanel(device: device),
          ),
        ),
        Container(width: 1, color: AppColors.border),
        // 右侧 Agent 列表
        Expanded(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: _AgentListPanel(device: device),
          ),
        ),
      ],
    );
  }

  Widget _buildMobileLayout(BuildContext context, DeviceModel device) {
    return SingleChildScrollView(
      padding: const EdgeInsets.all(16),
      child: Column(
        children: [
          _DeviceInfoPanel(device: device),
          const SizedBox(height: 16),
          _AgentListPanel(device: device),
        ],
      ),
    );
  }
}

class _DeviceInfoPanel extends StatelessWidget {
  final DeviceModel device;
  const _DeviceInfoPanel({required this.device});

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(
                  color: AppColors.primaryLight,
                  borderRadius: AppRadius.mdRadius,
                ),
                child: const Icon(
                  Icons.computer_rounded,
                  color: AppColors.primary,
                  size: 22,
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      device.hostname,
                      style: Theme.of(context).textTheme.titleLarge,
                    ),
                    const SizedBox(height: 4),
                    StatusPill(
                      label: device.online ? '在线' : '离线',
                      type: device.online
                          ? StatusType.online
                          : StatusType.offline,
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 20),
          const Divider(),
          const SizedBox(height: 16),
          InfoBlock(label: 'Machine ID', value: device.machineId, mono: true),
          const SizedBox(height: 14),
          InfoBlock(
            label: 'Agent 数量',
            value: '${device.agents.length} 个',
          ),
          if (device.lastSeen != null) ...[
            const SizedBox(height: 14),
            InfoBlock(
              label: '最近在线',
              value: _formatDateTime(device.lastSeen!),
            ),
          ],
        ],
      ),
    );
  }

  String _formatDateTime(DateTime dt) {
    return '${dt.year}-${dt.month.toString().padLeft(2, '0')}-${dt.day.toString().padLeft(2, '0')} '
        '${dt.hour.toString().padLeft(2, '0')}:${dt.minute.toString().padLeft(2, '0')}';
  }
}

class _AgentListPanel extends StatelessWidget {
  final DeviceModel device;
  const _AgentListPanel({required this.device});

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.only(bottom: 12),
          child: Row(
            children: [
              Text('Agent 列表', style: Theme.of(context).textTheme.headlineSmall),
              const SizedBox(width: 10),
              StatusPill(
                label: '${device.agents.length}',
                type: StatusType.processing,
              ),
            ],
          ),
        ),
        if (device.agents.isEmpty)
          const EmptyState(
            message: '该设备暂无 Agent',
            icon: Icons.smart_toy_outlined,
          )
        else
          ...device.agents.map(
            (agent) => Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: _AgentCard(agent: agent, device: device),
            ),
          ),
      ],
    );
  }
}

class _AgentCard extends StatelessWidget {
  final AgentModel agent;
  final DeviceModel device;
  const _AgentCard({required this.agent, required this.device});

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 36,
                height: 36,
                decoration: BoxDecoration(
                  color: AppColors.primaryLight,
                  borderRadius: AppRadius.smRadius,
                ),
                child: const Icon(
                  Icons.smart_toy_outlined,
                  color: AppColors.primary,
                  size: 18,
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      agent.projectId,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    Text(
                      agent.agentId,
                      style: Theme.of(context).textTheme.labelSmall?.copyWith(
                            fontFamily: 'monospace',
                          ),
                    ),
                  ],
                ),
              ),
              StatusPill(
                label: agent.isBusy ? '处理中' : '空闲',
                type: agent.isBusy
                    ? StatusType.processing
                    : StatusType.online,
              ),
            ],
          ),
          const SizedBox(height: 14),
          const Divider(),
          const SizedBox(height: 12),
          Row(
            children: [
              Expanded(
                child: InfoBlock(
                  label: '项目路径',
                  value: agent.projectRoot,
                  mono: true,
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Row(
            children: [
              Expanded(
                child: InfoBlock(label: '版本', value: agent.version),
              ),
              if (agent.isBusy)
                Expanded(
                  child: InfoBlock(
                    label: '当前任务',
                    value: agent.runningTaskId ?? '-',
                    mono: true,
                  ),
                ),
            ],
          ),
          const SizedBox(height: 16),
          Align(
            alignment: Alignment.centerRight,
            child: AppButton(
              label: '进入对话',
              icon: Icons.chat_bubble_outline,
              onPressed: () {
                context.push(
                  '/chat/${Uri.encodeComponent(agent.agentId)}'
                  '?projectId=${Uri.encodeComponent(agent.projectId)}'
                  '&machineId=${Uri.encodeComponent(device.machineId)}',
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}
