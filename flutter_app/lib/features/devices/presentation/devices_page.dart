import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/shell_card.dart';
import '../../../shared/widgets/surface_scaffold.dart';
import '../../auth/application/auth_controller.dart';
import '../../devices/data/devices_api.dart';
import '../domain/device_models.dart';

final devicesProvider = FutureProvider<List<DeviceInfo>>((ref) async {
  final auth = ref.watch(authControllerProvider);
  return ref.watch(devicesApiProvider).listDevices(auth.accessToken);
});

class DevicesPage extends ConsumerWidget {
  const DevicesPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final auth = ref.watch(authControllerProvider);
    final devicesValue = ref.watch(devicesProvider);

    return SurfaceScaffold(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          children: [
            _TopBar(
              operatorName: auth.operator?.name ?? '',
              operatorKey: auth.operator?.operatorKey ?? '',
              onLogout: () async {
                await ref.read(authControllerProvider.notifier).logout();
                if (context.mounted) context.go('/login');
              },
            ),
            const SizedBox(height: 20),
            Expanded(
              child: devicesValue.when(
                loading: () => const Center(child: CircularProgressIndicator()),
                error: (error, _) => Center(child: Text('$error')),
                data: (devices) => _DevicesGrid(devices: devices),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _TopBar extends StatelessWidget {
  const _TopBar({
    required this.operatorName,
    required this.operatorKey,
    required this.onLogout,
  });

  final String operatorName;
  final String operatorKey;
  final VoidCallback onLogout;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('设备', style: theme.textTheme.headlineMedium),
              const SizedBox(height: 6),
              Text('管理当前用户下的所有在线设备与 agent', style: theme.textTheme.bodyMedium),
            ],
          ),
        ),
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
          decoration: BoxDecoration(
            color: Colors.white.withValues(alpha: 0.8),
            borderRadius: BorderRadius.circular(20),
            border: Border.all(color: const Color(0xFFD4E5FF)),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(operatorName),
              const SizedBox(height: 4),
              Text(
                'Serve 归属 Key: $operatorKey',
                style: theme.textTheme.bodyMedium,
              ),
            ],
          ),
        ),
        const SizedBox(width: 12),
        FilledButton.tonal(
          onPressed: onLogout,
          child: const Text('退出'),
        ),
      ],
    );
  }
}

class _DevicesGrid extends StatelessWidget {
  const _DevicesGrid({required this.devices});

  final List<DeviceInfo> devices;

  @override
  Widget build(BuildContext context) {
    if (devices.isEmpty) {
      return const Center(child: Text('当前没有已归属的在线设备'));
    }

    return LayoutBuilder(
      builder: (context, constraints) {
        final width = constraints.maxWidth;
        final crossAxisCount = width > 1200 ? 3 : (width > 760 ? 2 : 1);
        return GridView.builder(
          itemCount: devices.length,
          gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: crossAxisCount,
            crossAxisSpacing: 16,
            mainAxisSpacing: 16,
            childAspectRatio: width > 760 ? 1.28 : 1.12,
          ),
          itemBuilder: (context, index) => _DeviceCard(device: devices[index]),
        );
      },
    );
  }
}

class _DeviceCard extends StatelessWidget {
  const _DeviceCard({required this.device});

  final DeviceInfo device;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return InkWell(
      borderRadius: BorderRadius.circular(28),
      onTap: () => context.go('/devices/${device.machineId}'),
      child: ShellCard(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Container(
                  width: 14,
                  height: 14,
                  decoration: BoxDecoration(
                    color: const Color(0xFF34C759),
                    borderRadius: BorderRadius.circular(999),
                  ),
                ),
                const Spacer(),
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                  decoration: BoxDecoration(
                    color: const Color(0xFFD9EAFF),
                    borderRadius: BorderRadius.circular(999),
                  ),
                  child: Text('${device.agents.length} 个 agent'),
                ),
              ],
            ),
            const SizedBox(height: 18),
            Text(device.machineId, style: theme.textTheme.titleLarge),
            const SizedBox(height: 8),
            Text(
              device.hostname.isEmpty ? '未上报主机名' : device.hostname,
              style: theme.textTheme.bodyLarge,
            ),
            const Spacer(),
            Wrap(
              spacing: 10,
              runSpacing: 10,
              children: [
                _DataPill(label: '状态', value: device.status),
                _DataPill(label: '最近在线', value: _formatDateTime(device.seenAt)),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _DataPill extends StatelessWidget {
  const _DataPill({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: const Color(0xFFF5F9FF),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: const Color(0xFFD4E5FF)),
      ),
      child: Text('$label：$value'),
    );
  }
}

String _formatDateTime(DateTime? value) {
  if (value == null) return '-';
  final month = value.month.toString().padLeft(2, '0');
  final day = value.day.toString().padLeft(2, '0');
  final hour = value.hour.toString().padLeft(2, '0');
  final minute = value.minute.toString().padLeft(2, '0');
  return '$month-$day $hour:$minute';
}
