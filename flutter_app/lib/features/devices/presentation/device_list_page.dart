import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
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
      body: SafeArea(
        child: PageBackground(
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
          GestureDetector(
            onTap: () => _showProfileSheet(context),
            child: Container(
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
                  const SizedBox(width: 3),
                  const Icon(Icons.expand_more, size: 14, color: AppColors.primary),
                ],
              ),
            ),
          ),
          const SizedBox(width: 8),
          IconButton(
            icon: const Icon(Icons.settings_outlined, size: 18, color: AppColors.textSecondary),
            tooltip: '设置',
            onPressed: () => context.push('/settings'),
          ),
          IconButton(
            icon: const Icon(Icons.refresh, size: 18, color: AppColors.textSecondary),
            tooltip: '刷新',
            onPressed: () => ref.invalidate(deviceListProvider),
          ),
        ],
      ),
    );
  }

  Future<void> _logout() async {
    await AppStorage.clearAuth();
    if (mounted) context.go('/login');
  }

  void _showProfileSheet(BuildContext context) {
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => _ProfileSheet(onLogout: _logout),
    );
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

// 个人信息面板
class _ProfileSheet extends StatefulWidget {
  final Future<void> Function() onLogout;
  const _ProfileSheet({required this.onLogout});

  @override
  State<_ProfileSheet> createState() => _ProfileSheetState();
}

class _ProfileSheetState extends State<_ProfileSheet> {
  late final TextEditingController _baseUrlCtrl;
  bool _editingUrl = false;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _baseUrlCtrl = TextEditingController(text: AppStorage.getBaseUrl());
  }

  @override
  void dispose() {
    _baseUrlCtrl.dispose();
    super.dispose();
  }

  Future<void> _saveBaseUrl() async {
    final url = _baseUrlCtrl.text.trim();
    if (url.isEmpty) return;
    setState(() => _saving = true);
    await AppStorage.setBaseUrl(url);
    setState(() {
      _saving = false;
      _editingUrl = false;
    });
  }

  void _copyToClipboard(BuildContext context, String text) {
    Clipboard.setData(ClipboardData(text: text));
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(
        content: Text('已复制到剪贴板'),
        duration: Duration(seconds: 2),
        behavior: SnackBarBehavior.floating,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final username = AppStorage.getUsername() ?? '-';
    final displayName = AppStorage.getDisplayName() ?? username;
    final operatorKey = AppStorage.getOperatorKey() ?? '-';

    return Padding(
      padding: EdgeInsets.only(
        bottom: MediaQuery.of(context).viewInsets.bottom,
      ),
      child: Container(
        decoration: const BoxDecoration(
          color: AppColors.surface,
          borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            // 拖拽条
            Container(
              margin: const EdgeInsets.only(top: 10),
              width: 36,
              height: 4,
              decoration: BoxDecoration(
                color: AppColors.border,
                borderRadius: BorderRadius.circular(2),
              ),
            ),
            // 标题
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 16, 20, 8),
              child: Row(
                children: [
                  Container(
                    width: 44,
                    height: 44,
                    decoration: BoxDecoration(
                      color: AppColors.primaryLight,
                      borderRadius: AppRadius.mdRadius,
                    ),
                    child: const Icon(Icons.person_outline,
                        size: 22, color: AppColors.primary),
                  ),
                  const SizedBox(width: 14),
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        displayName,
                        style: const TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w600,
                          color: AppColors.textPrimary,
                        ),
                      ),
                      Text(
                        '@$username',
                        style: const TextStyle(
                          fontSize: 13,
                          color: AppColors.textMuted,
                        ),
                      ),
                    ],
                  ),
                ],
              ),
            ),
            const Divider(height: 1, color: AppColors.border),
            // 信息列表
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
              child: Column(
                children: [
                  // Operator Key
                  _InfoRow(
                    label: 'Operator Key',
                    value: operatorKey.length > 20
                        ? '${operatorKey.substring(0, 8)}...${operatorKey.substring(operatorKey.length - 8)}'
                        : operatorKey,
                    icon: Icons.key_outlined,
                    trailing: IconButton(
                      icon: const Icon(Icons.copy_outlined,
                          size: 16, color: AppColors.textMuted),
                      tooltip: '复制',
                      onPressed: () => _copyToClipboard(context, operatorKey),
                      padding: EdgeInsets.zero,
                      constraints: const BoxConstraints(),
                    ),
                  ),
                  const SizedBox(height: 12),
                  // 后端地址
                  _editingUrl
                      ? Row(
                          children: [
                            const Icon(Icons.dns_outlined,
                                size: 16, color: AppColors.textMuted),
                            const SizedBox(width: 10),
                            Expanded(
                              child: TextField(
                                controller: _baseUrlCtrl,
                                autofocus: true,
                                decoration: InputDecoration(
                                  hintText: 'http://127.0.0.1:8080',
                                  isDense: true,
                                  contentPadding: const EdgeInsets.symmetric(
                                      horizontal: 10, vertical: 8),
                                  border: OutlineInputBorder(
                                    borderRadius: AppRadius.smRadius,
                                    borderSide: const BorderSide(
                                        color: AppColors.border),
                                  ),
                                  enabledBorder: OutlineInputBorder(
                                    borderRadius: AppRadius.smRadius,
                                    borderSide: const BorderSide(
                                        color: AppColors.border),
                                  ),
                                  focusedBorder: OutlineInputBorder(
                                    borderRadius: AppRadius.smRadius,
                                    borderSide: const BorderSide(
                                        color: AppColors.primary, width: 1.5),
                                  ),
                                  filled: true,
                                  fillColor: AppColors.inputBackground,
                                ),
                                style: const TextStyle(
                                    fontSize: 13, fontFamily: 'monospace'),
                              ),
                            ),
                            const SizedBox(width: 8),
                            TextButton(
                              onPressed: _saving ? null : _saveBaseUrl,
                              child: _saving
                                  ? const SizedBox(
                                      width: 14,
                                      height: 14,
                                      child: CircularProgressIndicator(
                                          strokeWidth: 1.5))
                                  : const Text('保存'),
                            ),
                            TextButton(
                              onPressed: () =>
                                  setState(() => _editingUrl = false),
                              child: const Text('取消'),
                            ),
                          ],
                        )
                      : _InfoRow(
                          label: '后端地址',
                          value: AppStorage.getBaseUrl(),
                          icon: Icons.dns_outlined,
                          trailing: IconButton(
                            icon: const Icon(Icons.edit_outlined,
                                size: 16, color: AppColors.textMuted),
                            tooltip: '修改',
                            onPressed: () =>
                                setState(() => _editingUrl = true),
                            padding: EdgeInsets.zero,
                            constraints: const BoxConstraints(),
                          ),
                        ),
                ],
              ),
            ),
            const Divider(height: 1, color: AppColors.border),
            // 退出登录
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 12, 20, 24),
              child: SizedBox(
                width: double.infinity,
                child: OutlinedButton.icon(
                  onPressed: () async {
                    Navigator.of(context).pop();
                    await widget.onLogout();
                  },
                  icon: const Icon(Icons.logout, size: 16,
                      color: AppColors.statusError),
                  label: const Text(
                    '退出登录',
                    style: TextStyle(color: AppColors.statusError),
                  ),
                  style: OutlinedButton.styleFrom(
                    side: BorderSide(
                        color: AppColors.statusError.withOpacity(0.4)),
                    padding: const EdgeInsets.symmetric(vertical: 12),
                    shape: RoundedRectangleBorder(
                        borderRadius: AppRadius.smRadius),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _InfoRow extends StatelessWidget {
  final String label;
  final String value;
  final IconData icon;
  final Widget? trailing;

  const _InfoRow({
    required this.label,
    required this.value,
    required this.icon,
    this.trailing,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Icon(icon, size: 16, color: AppColors.textMuted),
        const SizedBox(width: 10),
        Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              label,
              style: const TextStyle(
                fontSize: 11,
                color: AppColors.textMuted,
              ),
            ),
            Text(
              value,
              style: const TextStyle(
                fontSize: 13,
                color: AppColors.textPrimary,
                fontFamily: 'monospace',
              ),
            ),
          ],
        ),
        const Spacer(),
        if (trailing != null) trailing!,
      ],
    );
  }
}
