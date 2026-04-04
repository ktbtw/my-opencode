import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/storage/app_storage.dart';
import '../../../shared/widgets/widgets.dart';
import '../data/device_model.dart';
import 'device_provider.dart';

class DeviceListPage extends ConsumerStatefulWidget {
  const DeviceListPage({super.key});

  @override
  ConsumerState<DeviceListPage> createState() => _DeviceListPageState();
}

class _DeviceListPageState extends ConsumerState<DeviceListPage>
    with TickerProviderStateMixin {
  String _searchQuery = '';

  late final AnimationController _fadeCtrl;
  late final Animation<double> _fadeAnim;

  @override
  void initState() {
    super.initState();
    _fadeCtrl = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 350),
    )..forward();
    _fadeAnim = CurvedAnimation(parent: _fadeCtrl, curve: Curves.easeOut);
  }

  @override
  void dispose() {
    _fadeCtrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final devicesAsync = ref.watch(deviceListProvider);
    final displayName = AppStorage.getDisplayName() ?? AppStorage.getUsername() ?? '用户';

    return Scaffold(
      body: PageBackground(
        child: Column(
          children: [
            _buildTopBar(context, displayName),
            Expanded(
              child: FadeTransition(
                opacity: _fadeAnim,
                child: devicesAsync.when(
                  loading: () => const LoadingState(),
                  error: (e, _) => _buildError(e.toString()),
                  data: (devices) => _buildContent(devices),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildTopBar(BuildContext context, String displayName) {
    return Container(
      height: 56,
      decoration: const BoxDecoration(
        color: AppColors.surface,
        border: Border(bottom: BorderSide(color: AppColors.border)),
      ),
      padding: const EdgeInsets.symmetric(horizontal: 20),
      child: Row(
        children: [
          const Icon(Icons.terminal_rounded, color: AppColors.primary, size: 20),
          const SizedBox(width: 10),
          Text(
            'Chat Codex',
            style: Theme.of(context).textTheme.titleLarge?.copyWith(
                  color: AppColors.primary,
                ),
          ),
          const Spacer(),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
            decoration: BoxDecoration(
              color: AppColors.primaryLight,
              borderRadius: AppRadius.smRadius,
            ),
            child: Row(
              children: [
                const Icon(Icons.person_outline, size: 14, color: AppColors.primary),
                const SizedBox(width: 5),
                Text(
                  displayName,
                  style: const TextStyle(
                    fontSize: 13,
                    color: AppColors.primary,
                    fontWeight: FontWeight.w500,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(width: 8),
          IconButton(
            icon: const Icon(Icons.refresh, size: 18, color: AppColors.textSecondary),
            tooltip: '刷新',
            onPressed: () => ref.invalidate(deviceListProvider),
          ),
          IconButton(
            icon: const Icon(Icons.logout, size: 18, color: AppColors.textSecondary),
            tooltip: '退出登录',
            onPressed: _logout,
          ),
        ],
      ),
    );
  }

  Future<void> _logout() async {
    await AppStorage.clearAuth();
    if (mounted) context.go('/login');
  }

  Widget _buildContent(List<DeviceModel> devices) {
    final filtered = _searchQuery.isEmpty
        ? devices
        : devices
            .where((d) =>
                d.hostname.contains(_searchQuery) ||
                d.machineId.contains(_searchQuery))
            .toList();

    return CustomScrollView(
      slivers: [
        SliverToBoxAdapter(
          child: _buildSearchBar(devices.length),
        ),
        if (filtered.isEmpty)
          const SliverFillRemaining(
            child: EmptyState(
              message: '暂无设备\n请确认设备是否已连接到后端',
              icon: Icons.devices_other_outlined,
            ),
          )
        else
          SliverPadding(
            padding: EdgeInsets.symmetric(
              horizontal: AppBreakpoints.isMobile(context) ? 16 : 24,
              vertical: 16,
            ),
            sliver: _buildGrid(filtered),
          ),
      ],
    );
  }

  Widget _buildSearchBar(int total) {
    return Container(
      padding: EdgeInsets.symmetric(
        horizontal: AppBreakpoints.isMobile(context) ? 16 : 24,
        vertical: 16,
      ),
      child: Row(
        children: [
          Expanded(
            child: TextField(
              onChanged: (v) => setState(() => _searchQuery = v),
              decoration: InputDecoration(
                hintText: '搜索设备名称或 Machine ID',
                prefixIcon: const Icon(Icons.search, size: 18, color: AppColors.textMuted),
                filled: true,
                fillColor: AppColors.inputBackground,
                contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                border: OutlineInputBorder(
                  borderRadius: AppRadius.mdRadius,
                  borderSide: const BorderSide(color: AppColors.border),
                ),
                enabledBorder: OutlineInputBorder(
                  borderRadius: AppRadius.mdRadius,
                  borderSide: const BorderSide(color: AppColors.border),
                ),
                focusedBorder: OutlineInputBorder(
                  borderRadius: AppRadius.mdRadius,
                  borderSide: const BorderSide(color: AppColors.primary, width: 1.5),
                ),
              ),
            ),
          ),
          const SizedBox(width: 12),
          Text(
            '共 $total 台设备',
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ],
      ),
    );
  }

  Widget _buildGrid(List<DeviceModel> devices) {
    final width = MediaQuery.of(context).size.width;
    int crossCount;
    if (width >= AppBreakpoints.lg) {
      crossCount = 3;
    } else if (width >= AppBreakpoints.md) {
      crossCount = 2;
    } else if (width >= AppBreakpoints.sm) {
      crossCount = 2;
    } else {
      crossCount = 1;
    }

    return SliverGrid(
      gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
        crossAxisCount: crossCount,
        mainAxisSpacing: 12,
        crossAxisSpacing: 12,
        childAspectRatio: crossCount == 1 ? 2.8 : 1.8,
      ),
      delegate: SliverChildBuilderDelegate(
        (context, i) => _DeviceCard(device: devices[i]),
        childCount: devices.length,
      ),
    );
  }

  Widget _buildError(String message) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(Icons.error_outline, size: 40, color: AppColors.statusError),
          const SizedBox(height: 12),
          Text(message, style: const TextStyle(color: AppColors.statusError)),
          const SizedBox(height: 16),
          AppButton(
            label: '重试',
            outlined: true,
            onPressed: () => ref.invalidate(deviceListProvider),
          ),
        ],
      ),
    );
  }
}

class _DeviceCard extends StatefulWidget {
  final DeviceModel device;
  const _DeviceCard({required this.device});

  @override
  State<_DeviceCard> createState() => _DeviceCardState();
}

class _DeviceCardState extends State<_DeviceCard>
    with SingleTickerProviderStateMixin {
  bool _hovered = false;
  late final AnimationController _hoverCtrl;
  late final Animation<double> _elevationAnim;

  @override
  void initState() {
    super.initState();
    _hoverCtrl = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 180),
    );
    _elevationAnim = CurvedAnimation(parent: _hoverCtrl, curve: Curves.easeOut);
  }

  @override
  void dispose() {
    _hoverCtrl.dispose();
    super.dispose();
  }

  void _onHover(bool hover) {
    setState(() => _hovered = hover);
    if (hover) {
      _hoverCtrl.forward();
    } else {
      _hoverCtrl.reverse();
    }
  }

  @override
  Widget build(BuildContext context) {
    final device = widget.device;
    final lastSeenStr = device.lastSeen != null
        ? _formatTime(device.lastSeen!)
        : '未知';

    return MouseRegion(
      onEnter: (_) => _onHover(true),
      onExit: (_) => _onHover(false),
      cursor: SystemMouseCursors.click,
      child: GestureDetector(
        onTap: () => context.push('/devices/${device.machineId}'),
        child: AnimatedBuilder(
          animation: _elevationAnim,
          builder: (context, child) {
            return Container(
              decoration: BoxDecoration(
                color: AppColors.surface,
                borderRadius: AppRadius.lgRadius,
                border: Border.all(
                  color: _hovered ? AppColors.primary : AppColors.border,
                  width: _hovered ? 1.5 : 1,
                ),
                boxShadow: [
                  BoxShadow(
                    color: Color.lerp(
                      const Color(0x0A1A3A6A),
                      const Color(0x1A2563EB),
                      _elevationAnim.value,
                    )!,
                    blurRadius: 8 + 12 * _elevationAnim.value,
                    offset: const Offset(0, 4),
                  ),
                ],
              ),
              padding: AppSpacing.cardPadding,
              child: child,
            );
          },
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      device.hostname,
                      style: Theme.of(context).textTheme.titleMedium,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  const SizedBox(width: 8),
                  StatusPill(
                    label: device.online ? '在线' : '离线',
                    type: device.online
                        ? StatusType.online
                        : StatusType.offline,
                  ),
                ],
              ),
              const SizedBox(height: 6),
              Text(
                device.machineId,
                style: Theme.of(context).textTheme.labelSmall?.copyWith(
                      fontFamily: 'monospace',
                    ),
              ),
              const Spacer(),
              Row(
                children: [
                  const Icon(Icons.smart_toy_outlined,
                      size: 14, color: AppColors.textMuted),
                  const SizedBox(width: 4),
                  Text(
                    '${device.agents.length} 个 Agent',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                  const Spacer(),
                  const Icon(Icons.access_time,
                      size: 13, color: AppColors.textMuted),
                  const SizedBox(width: 3),
                  Text(
                    lastSeenStr,
                    style: Theme.of(context).textTheme.labelSmall,
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }

  String _formatTime(DateTime dt) {
    final diff = DateTime.now().difference(dt);
    if (diff.inSeconds < 60) return '刚刚';
    if (diff.inMinutes < 60) return '${diff.inMinutes} 分钟前';
    if (diff.inHours < 24) return '${diff.inHours} 小时前';
    return '${diff.inDays} 天前';
  }
}
