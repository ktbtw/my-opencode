import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/foundation.dart'
    show defaultTargetPlatform, kIsWeb, TargetPlatform;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter/services.dart';
import 'package:go_router/go_router.dart';
import '../../../core/services/app_updater.dart';
import '../../../core/notifications/app_notification_controller.dart';
import '../../../core/notifications/app_notification_feedback.dart';
import '../../../core/notifications/app_notification_host.dart';
import '../../../core/notifications/app_notification_model.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/storage/app_storage.dart';
import '../../../shared/widgets/drag_edge_auto_scroller.dart';
import '../../../shared/widgets/widgets.dart';
import '../../chat/data/chat_target.dart';
import '../data/device_model.dart';
import 'device_provider.dart';

const _desktopDeviceCardHeight = 320.0;
const _desktopAgentListHeight = 168.0;

class DeviceListPage extends ConsumerStatefulWidget {
  const DeviceListPage({super.key});

  @override
  ConsumerState<DeviceListPage> createState() => _DeviceListPageState();
}

class _DeviceListPageState extends ConsumerState<DeviceListPage>
    with TickerProviderStateMixin {
  String _searchQuery = '';
  bool _updateCheckScheduled = false;
  bool _sortingDevices = false;
  bool _savingDeviceOrder = false;
  bool _deviceOrderDirty = false;
  String? _draggingDeviceId;
  List<DeviceModel> _orderedDevices = const [];
  final ScrollController _deviceScrollController = ScrollController();
  final GlobalKey _deviceScrollViewportKey = GlobalKey();
  late final DragEdgeAutoScroller _deviceAutoScroller;

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
    _deviceAutoScroller = DragEdgeAutoScroller(
      controller: _deviceScrollController,
      viewportKey: _deviceScrollViewportKey,
    );
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || _updateCheckScheduled) return;
      _updateCheckScheduled = true;
      AppUpdater.maybeShowUpdateDialog();
    });
  }

  @override
  void dispose() {
    _deviceAutoScroller.dispose();
    _deviceScrollController.dispose();
    _fadeCtrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final devicesAsync = ref.watch(deviceListProvider);
    final displayName =
        AppStorage.getDisplayName() ?? AppStorage.getUsername() ?? '用户';

    return Scaffold(
      floatingActionButton: _sortingDevices
          ? FloatingActionButton.extended(
              heroTag: 'finish-device-sorting',
              onPressed: _finishDeviceSorting,
              tooltip: '完成排序',
              icon: const Icon(Icons.check_rounded, size: 20),
              label: const Text('完成排序'),
            )
          : null,
      body: SafeArea(
        child: PageBackground(
          child: Column(
            children: [
              _buildTopBar(
                context,
                displayName,
                hasNoDevices: devicesAsync.valueOrNull?.isEmpty ?? false,
              ),
              Expanded(
                child: FadeTransition(
                  opacity: _fadeAnim,
                  child: devicesAsync.when(
                    loading: () => const DeviceListSkeleton(),
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

  Widget _buildTopBar(
    BuildContext context,
    String displayName, {
    required bool hasNoDevices,
  }) {
    final compact = MediaQuery.sizeOf(context).width < 520;
    return Container(
      height: 56,
      decoration: const BoxDecoration(
        color: AppColors.surface,
        border: Border(bottom: BorderSide(color: AppColors.border)),
      ),
      padding: const EdgeInsets.symmetric(horizontal: 20),
      child: Row(
        children: [
          const Icon(
            Icons.terminal_rounded,
            color: AppColors.primary,
            size: 20,
          ),
          const SizedBox(width: 10),
          Text(
            'Chat Codex',
            style: Theme.of(
              context,
            ).textTheme.titleLarge?.copyWith(color: AppColors.primary),
          ),
          const Spacer(),
          const AppNotificationCenterButton(),
          if (compact)
            PopupMenuButton<_DeviceTopBarAction>(
              tooltip: '更多操作',
              icon: const Icon(
                Icons.more_vert_rounded,
                size: 20,
                color: AppColors.textSecondary,
              ),
              onSelected: (action) {
                switch (action) {
                  case _DeviceTopBarAction.profile:
                    _showProfileSheet(context);
                    break;
                  case _DeviceTopBarAction.settings:
                    context.push('/settings');
                    break;
                  case _DeviceTopBarAction.downloads:
                    _openLauncherEntry(context, hasNoDevices: hasNoDevices);
                    break;
                  case _DeviceTopBarAction.refresh:
                    ref.invalidate(deviceListProvider);
                    break;
                }
              },
              itemBuilder: (_) => [
                PopupMenuItem(
                  value: _DeviceTopBarAction.profile,
                  child: ListTile(
                    dense: true,
                    leading: const Icon(Icons.person_outline, size: 18),
                    title: Text(displayName),
                  ),
                ),
                PopupMenuItem(
                  value: _DeviceTopBarAction.settings,
                  child: ListTile(
                    dense: true,
                    leading: Icon(Icons.settings_outlined, size: 18),
                    title: Text('设置'),
                  ),
                ),
                PopupMenuItem(
                  value: _DeviceTopBarAction.downloads,
                  child: ListTile(
                    dense: true,
                    leading: Icon(
                      Icons.download_for_offline_outlined,
                      size: 18,
                    ),
                    title: Text(hasNoDevices ? 'Launcher（开始接入设备）' : 'Launcher'),
                  ),
                ),
                const PopupMenuItem(
                  value: _DeviceTopBarAction.refresh,
                  child: ListTile(
                    dense: true,
                    leading: Icon(Icons.refresh_rounded, size: 18),
                    title: Text('刷新'),
                  ),
                ),
              ],
            )
          else ...[
            IconButton(
              icon: const Icon(
                Icons.person_outline,
                size: 18,
                color: AppColors.textSecondary,
              ),
              tooltip: '个人信息 - $displayName',
              onPressed: () => _showProfileSheet(context),
            ),
            IconButton(
              icon: const Icon(
                Icons.settings_outlined,
                size: 18,
                color: AppColors.textSecondary,
              ),
              tooltip: '设置',
              onPressed: () => context.push('/settings'),
            ),
            IconButton(
              icon: const Icon(
                Icons.download_for_offline_outlined,
                size: 18,
                color: AppColors.textSecondary,
              ),
              tooltip: 'Launcher',
              onPressed: () =>
                  _openLauncherEntry(context, hasNoDevices: hasNoDevices),
            ),
            IconButton(
              icon: const Icon(
                Icons.refresh,
                size: 18,
                color: AppColors.textSecondary,
              ),
              tooltip: '刷新',
              onPressed: () => ref.invalidate(deviceListProvider),
            ),
          ],
        ],
      ),
    );
  }

  void _showProfileSheet(BuildContext context) {
    context.push('/profile');
  }

  bool get _isMobileDevice =>
      !kIsWeb &&
      (defaultTargetPlatform == TargetPlatform.android ||
          defaultTargetPlatform == TargetPlatform.iOS);

  String get _launcherGuideUrl {
    if (kIsWeb) {
      return Uri.base
          .replace(path: '/launcher-downloads', query: '', fragment: '')
          .toString();
    }
    final base = AppStorage.getBaseUrl().replaceFirst(RegExp(r'/+$'), '');
    return '$base/launcher-downloads';
  }

  void _openLauncherEntry(BuildContext context, {required bool hasNoDevices}) {
    if (!hasNoDevices || !_isMobileDevice) {
      context.push('/launcher-downloads');
      return;
    }
    showDialog<void>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        icon: const Icon(
          Icons.desktop_windows_outlined,
          color: AppColors.primary,
          size: 30,
        ),
        title: const Text('先在电脑上接入设备'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Launcher 需要运行在 Windows 或 macOS 电脑上。请在电脑浏览器打开下面的页面：',
              style: TextStyle(height: 1.5),
            ),
            const SizedBox(height: 14),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: AppColors.inputBackground,
                border: Border.all(color: AppColors.border),
                borderRadius: AppRadius.smRadius,
              ),
              child: SelectableText(
                _launcherGuideUrl,
                style: const TextStyle(
                  color: AppColors.primary,
                  fontFamily: 'monospace',
                  fontSize: 12,
                  height: 1.4,
                ),
              ),
            ),
            const SizedBox(height: 14),
            const Text(
              '在网页中下载对应电脑的 Launcher，启动后登录同一账号，设备就会出现在这里。',
              style: TextStyle(
                color: AppColors.textSecondary,
                fontSize: 13,
                height: 1.5,
              ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () async {
              await Clipboard.setData(ClipboardData(text: _launcherGuideUrl));
              if (!dialogContext.mounted) return;
              Navigator.of(dialogContext).pop();
              if (!mounted) return;
              showAppFeedback(context, message: '下载页地址已复制');
            },
            child: const Text('复制地址'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: const Text('知道了'),
          ),
        ],
      ),
    );
  }

  Widget _buildContent(List<DeviceModel> devices) {
    final latestById = {for (final device in devices) device.machineId: device};
    if (!_sortingDevices) {
      _orderedDevices = List<DeviceModel>.of(devices);
    } else {
      _orderedDevices = [
        for (final device in _orderedDevices)
          if (latestById.containsKey(device.machineId))
            latestById[device.machineId]!,
        for (final device in devices)
          if (!_orderedDevices.any(
            (item) => item.machineId == device.machineId,
          ))
            device,
      ];
    }
    final source = _orderedDevices;
    final filtered = _searchQuery.isEmpty
        ? source
        : source
              .where(
                (d) =>
                    d.displayName.contains(_searchQuery) ||
                    d.hostname.contains(_searchQuery) ||
                    d.machineId.contains(_searchQuery),
              )
              .toList();

    return CustomScrollView(
      key: _deviceScrollViewportKey,
      controller: _deviceScrollController,
      slivers: [
        SliverToBoxAdapter(child: _buildSearchBar(devices.length)),
        if (filtered.isEmpty && devices.isEmpty)
          SliverFillRemaining(
            hasScrollBody: false,
            child: _NoDevicesState(
              onOpenLauncher: () =>
                  _openLauncherEntry(context, hasNoDevices: true),
            ),
          )
        else if (filtered.isEmpty)
          const SliverFillRemaining(
            child: EmptyState(
              message: '没有匹配的设备',
              icon: Icons.search_off_rounded,
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
                hintText: '搜索设备名称或主机名',
                prefixIcon: const Icon(
                  Icons.search,
                  size: 18,
                  color: AppColors.textMuted,
                ),
                filled: true,
                fillColor: AppColors.inputBackground,
                contentPadding: const EdgeInsets.symmetric(
                  horizontal: 16,
                  vertical: 12,
                ),
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
                  borderSide: const BorderSide(
                    color: AppColors.primary,
                    width: 1.5,
                  ),
                ),
              ),
            ),
          ),
          const SizedBox(width: 12),
          Text('共 $total 台设备', style: Theme.of(context).textTheme.bodySmall),
        ],
      ),
    );
  }

  Widget _buildGrid(List<DeviceModel> devices) {
    final width = MediaQuery.of(context).size.width;
    final desktop = width >= AppBreakpoints.md;
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

    if (!desktop) {
      return SliverGrid(
        gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
          crossAxisCount: crossCount,
          mainAxisSpacing: 12,
          crossAxisSpacing: 12,
          childAspectRatio: 1.65,
        ),
        delegate: SliverChildBuilderDelegate(
          (context, i) => _buildSortableDeviceCard(devices[i]),
          childCount: devices.length,
        ),
      );
    }

    final rowCount = (devices.length + crossCount - 1) ~/ crossCount;
    return SliverList(
      delegate: SliverChildBuilderDelegate((context, rowIndex) {
        final start = rowIndex * crossCount;
        final end = math.min(start + crossCount, devices.length);
        return Padding(
          padding: EdgeInsets.only(bottom: rowIndex == rowCount - 1 ? 0 : 12),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              for (var index = start; index < end; index++) ...[
                if (index > start) const SizedBox(width: 12),
                Expanded(child: _buildSortableDeviceCard(devices[index])),
              ],
              for (var index = end; index < start + crossCount; index++) ...[
                const SizedBox(width: 12),
                const Expanded(child: SizedBox()),
              ],
            ],
          ),
        );
      }, childCount: rowCount),
    );
  }

  Widget _buildSortableDeviceCard(DeviceModel device) {
    final card = DragTarget<String>(
      onWillAcceptWithDetails: (details) {
        final sourceId = details.data;
        if (sourceId == device.machineId) return false;
        _moveDevice(sourceId, device.machineId);
        return true;
      },
      builder: (context, candidates, rejected) => Opacity(
        opacity: _draggingDeviceId == device.machineId ? 0.34 : 1,
        child: _DeviceCard(
          device: device,
          sorting: _sortingDevices,
          onRename: () => _renameDevice(device),
          onCopyIdentifier: () => _copyDeviceIdentifier(device),
        ),
      ),
    );
    Widget feedback() => Material(
      color: Colors.transparent,
      child: SizedBox(
        width: 320,
        height: AppBreakpoints.isDesktop(context)
            ? _desktopDeviceCardHeight
            : 232,
        child: _DeviceCard(device: device, sorting: true, preview: true),
      ),
    );
    if (_sortingDevices) {
      return Draggable<String>(
        data: device.machineId,
        feedback: feedback(),
        onDragStarted: () =>
            setState(() => _draggingDeviceId = device.machineId),
        onDragUpdate: (details) =>
            _deviceAutoScroller.update(details.globalPosition),
        onDragEnd: (_) => _completeDeviceDrag(),
        childWhenDragging: card,
        child: card,
      );
    }
    return LongPressDraggable<String>(
      data: device.machineId,
      feedback: feedback(),
      onDragUpdate: (details) =>
          _deviceAutoScroller.update(details.globalPosition),
      onDragStarted: () {
        setState(() {
          _sortingDevices = true;
          _draggingDeviceId = device.machineId;
          _searchQuery = '';
        });
      },
      onDragEnd: (_) => _completeDeviceDrag(),
      childWhenDragging: card,
      child: card,
    );
  }

  void _moveDevice(String sourceId, String targetId) {
    final from = _orderedDevices.indexWhere(
      (item) => item.machineId == sourceId,
    );
    final to = _orderedDevices.indexWhere((item) => item.machineId == targetId);
    if (from < 0 || to < 0 || from == to) return;
    setState(() {
      final next = List<DeviceModel>.of(_orderedDevices);
      final item = next.removeAt(from);
      next.insert(to, item);
      _orderedDevices = next;
      _deviceOrderDirty = true;
    });
  }

  void _completeDeviceDrag() {
    _deviceAutoScroller.stop();
    setState(() => _draggingDeviceId = null);
    if (_deviceOrderDirty) unawaited(_persistDeviceOrder());
  }

  void _finishDeviceSorting() {
    _deviceAutoScroller.stop();
    setState(() {
      _sortingDevices = false;
      _draggingDeviceId = null;
    });
    if (_deviceOrderDirty) unawaited(_persistDeviceOrder());
  }

  Future<void> _persistDeviceOrder() async {
    if (_savingDeviceOrder || !_deviceOrderDirty) return;
    setState(() {
      _savingDeviceOrder = true;
      _deviceOrderDirty = false;
    });
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    const operationId = 'devices:reorder';
    notifications.start(
      operationId: operationId,
      title: '正在同步设备顺序',
      message: '保存当前账号的卡片排列',
      progressMode: AppNotificationProgressMode.indeterminate,
      displayStyle: AppNotificationDisplayStyle.sync,
      kind: AppNotificationKind.system,
      scope: AppNotificationScope.synced,
    );
    try {
      await ref
          .read(deviceRepositoryProvider)
          .reorderDevices(
            _orderedDevices
                .map((item) => item.machineId)
                .toList(growable: false),
          );
      // Sorting is an inline editing operation; do not retain its progress
      // toast after the order has been saved.
      notifications.dismissOperation(operationId);
    } catch (error) {
      notifications.fail(operationId, title: '设备顺序同步失败', error: error);
      ref.invalidate(deviceListProvider);
    } finally {
      if (mounted) setState(() => _savingDeviceOrder = false);
      if (_deviceOrderDirty) unawaited(_persistDeviceOrder());
    }
  }

  Future<void> _renameDevice(DeviceModel device) async {
    final controller = TextEditingController(text: device.displayName);
    final name = await showDialog<String>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('修改设备名称'),
        content: TextField(
          controller: controller,
          autofocus: true,
          maxLength: 40,
          decoration: InputDecoration(
            labelText: '设备名称',
            hintText: device.hostname.isEmpty ? '输入设备名称' : device.hostname,
          ),
          onSubmitted: (value) => Navigator.of(dialogContext).pop(value),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(controller.text),
            child: const Text('保存'),
          ),
        ],
      ),
    );
    controller.dispose();
    if (name == null) return;
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    final operationId = 'device:rename:${device.machineId}';
    notifications.start(
      operationId: operationId,
      title: '正在更新设备名称',
      message: device.effectiveName,
      displayStyle: AppNotificationDisplayStyle.dots,
      kind: AppNotificationKind.system,
      scope: AppNotificationScope.synced,
    );
    try {
      await ref
          .read(deviceRepositoryProvider)
          .updateDeviceDisplayName(
            machineId: device.machineId,
            displayName: name.trim(),
          );
      notifications.succeed(operationId, title: '设备名称已更新');
      ref.invalidate(deviceListProvider);
      ref.invalidate(deviceDetailProvider(device.machineId));
    } catch (error) {
      notifications.fail(operationId, title: '设备名称更新失败', error: error);
    }
  }

  Future<void> _copyDeviceIdentifier(DeviceModel device) async {
    await Clipboard.setData(ClipboardData(text: device.machineId));
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    final operationId = 'device:copy:${device.machineId}';
    notifications.start(
      operationId: operationId,
      title: '设备标识已复制',
      message: device.effectiveName,
      progressMode: AppNotificationProgressMode.none,
      displayStyle: AppNotificationDisplayStyle.compact,
      kind: AppNotificationKind.system,
    );
    notifications.succeed(operationId, message: '可用于远程诊断和设备定位');
  }

  Widget _buildError(String message) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(
            Icons.error_outline,
            size: 40,
            color: AppColors.statusError,
          ),
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

class _NoDevicesState extends StatelessWidget {
  final VoidCallback onOpenLauncher;

  const _NoDevicesState({required this.onOpenLauncher});

  @override
  Widget build(BuildContext context) {
    final mobile = AppBreakpoints.isMobile(context);
    return Center(
      child: Padding(
        padding: EdgeInsets.symmetric(horizontal: mobile ? 24 : 40),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 520),
          child: PanelCard(
            padding: const EdgeInsets.fromLTRB(24, 28, 24, 22),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Container(
                  width: 58,
                  height: 58,
                  decoration: BoxDecoration(
                    color: AppColors.primaryLight,
                    borderRadius: BorderRadius.circular(16),
                  ),
                  child: const Icon(
                    Icons.devices_other_outlined,
                    color: AppColors.primary,
                    size: 30,
                  ),
                ),
                const SizedBox(height: 18),
                Text(
                  '还没有接入设备',
                  style: Theme.of(context).textTheme.headlineSmall,
                ),
                const SizedBox(height: 8),
                const Text(
                  '第一步：打开右上角菜单，进入 Launcher。\n在电脑网页下载并启动对应的 Launcher，登录同一账号后设备会自动出现。',
                  textAlign: TextAlign.center,
                  style: TextStyle(
                    color: AppColors.textSecondary,
                    fontSize: 13,
                    height: 1.6,
                  ),
                ),
                const SizedBox(height: 20),
                SizedBox(
                  width: double.infinity,
                  child: FilledButton.icon(
                    onPressed: onOpenLauncher,
                    icon: const Icon(Icons.rocket_launch_outlined, size: 18),
                    label: const Text('打开 Launcher 引导'),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _DeviceCard extends StatefulWidget {
  final DeviceModel device;
  final bool sorting;
  final bool preview;
  final VoidCallback? onRename;
  final VoidCallback? onCopyIdentifier;

  const _DeviceCard({
    required this.device,
    this.sorting = false,
    this.preview = false,
    this.onRename,
    this.onCopyIdentifier,
  });

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
    final recentAgents = _selectRecentAgents(
      device,
      limit: AppBreakpoints.isDesktop(context) ? 3 : 1,
    );
    final lastSeenStr = device.lastSeen != null
        ? _formatTime(device.lastSeen!)
        : '未知';
    final activeTasks = device.activeTaskCount;
    final statusColor = !device.online
        ? AppColors.textMuted
        : activeTasks > 0
        ? AppColors.statusWarning
        : AppColors.statusSuccess;
    final statusText = !device.online
        ? '离线'
        : activeTasks > 0
        ? '任务中'
        : '在线';
    final taskText = !device.online
        ? '等待设备重新连接'
        : activeTasks == 0
        ? '当前无运行任务'
        : activeTasks == 1
        ? '${device.agents.firstWhere((agent) => agent.isBusy).projectId.isEmpty ? 'Agent' : device.agents.firstWhere((agent) => agent.isBusy).projectId} 正在运行'
        : '$activeTasks 个任务正在运行';

    return MouseRegion(
      onEnter: (_) => _onHover(true),
      onExit: (_) => _onHover(false),
      cursor: widget.sorting
          ? SystemMouseCursors.grabbing
          : SystemMouseCursors.click,
      child: GestureDetector(
        onTap: widget.sorting || widget.preview
            ? null
            : () => context.push('/devices/${device.machineId}'),
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
              clipBehavior: Clip.antiAlias,
              child: child,
            );
          },
          child: SizedBox(
            height: AppBreakpoints.isDesktop(context)
                ? _desktopDeviceCardHeight
                : null,
            child: Row(
              children: [
                Container(width: 4, color: statusColor),
                Expanded(
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(16, 14, 12, 13),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text(
                                    device.effectiveName,
                                    style: Theme.of(
                                      context,
                                    ).textTheme.titleMedium,
                                    maxLines: 2,
                                    overflow: TextOverflow.ellipsis,
                                  ),
                                  const SizedBox(height: 4),
                                  Row(
                                    children: [
                                      Container(
                                        width: 7,
                                        height: 7,
                                        decoration: BoxDecoration(
                                          color: statusColor,
                                          shape: BoxShape.circle,
                                        ),
                                      ),
                                      const SizedBox(width: 6),
                                      Text(
                                        statusText,
                                        style: Theme.of(context)
                                            .textTheme
                                            .labelSmall
                                            ?.copyWith(color: statusColor),
                                      ),
                                    ],
                                  ),
                                ],
                              ),
                            ),
                            if (widget.sorting)
                              const Padding(
                                padding: EdgeInsets.only(left: 8, top: 2),
                                child: Icon(
                                  Icons.drag_indicator_rounded,
                                  size: 20,
                                  color: AppColors.textMuted,
                                ),
                              )
                            else if (!widget.preview)
                              PopupMenuButton<_DeviceCardAction>(
                                tooltip: '设备操作',
                                padding: EdgeInsets.zero,
                                icon: const Icon(
                                  Icons.more_horiz_rounded,
                                  size: 20,
                                  color: AppColors.textSecondary,
                                ),
                                onSelected: (action) {
                                  switch (action) {
                                    case _DeviceCardAction.rename:
                                      widget.onRename?.call();
                                      break;
                                    case _DeviceCardAction.copyIdentifier:
                                      widget.onCopyIdentifier?.call();
                                      break;
                                  }
                                },
                                itemBuilder: (_) => const [
                                  PopupMenuItem(
                                    value: _DeviceCardAction.rename,
                                    child: ListTile(
                                      dense: true,
                                      leading: Icon(
                                        Icons.edit_outlined,
                                        size: 18,
                                      ),
                                      title: Text('修改名称'),
                                    ),
                                  ),
                                  PopupMenuItem(
                                    value: _DeviceCardAction.copyIdentifier,
                                    child: ListTile(
                                      dense: true,
                                      leading: Icon(
                                        Icons.copy_rounded,
                                        size: 18,
                                      ),
                                      title: Text('复制设备标识'),
                                    ),
                                  ),
                                ],
                              ),
                          ],
                        ),
                        if (!AppBreakpoints.isDesktop(context)) const Spacer(),
                        if (AppBreakpoints.isDesktop(context))
                          SizedBox(
                            height: _desktopAgentListHeight,
                            child: Align(
                              alignment: Alignment.center,
                              child: _buildRecentAgents(
                                context,
                                device,
                                recentAgents,
                              ),
                            ),
                          )
                        else
                          _buildRecentAgents(context, device, recentAgents),
                        const SizedBox(height: 8),
                        Container(
                          width: double.infinity,
                          padding: const EdgeInsets.symmetric(
                            horizontal: 10,
                            vertical: 8,
                          ),
                          decoration: BoxDecoration(
                            color: AppColors.inputBackground,
                            borderRadius: AppRadius.smRadius,
                          ),
                          child: Row(
                            children: [
                              Icon(
                                activeTasks > 0
                                    ? Icons.bolt_rounded
                                    : Icons.schedule_rounded,
                                size: 15,
                                color: activeTasks > 0
                                    ? AppColors.statusWarning
                                    : AppColors.textMuted,
                              ),
                              const SizedBox(width: 7),
                              Expanded(
                                child: Text(
                                  taskText,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: Theme.of(context).textTheme.bodySmall,
                                ),
                              ),
                            ],
                          ),
                        ),
                        const SizedBox(height: 10),
                        Row(
                          children: [
                            const Icon(
                              Icons.smart_toy_outlined,
                              size: 14,
                              color: AppColors.textMuted,
                            ),
                            const SizedBox(width: 4),
                            Expanded(
                              child: Text(
                                '${device.runningAgentCount} 启动 / ${device.totalAgentCount} 总计',
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: Theme.of(context).textTheme.bodySmall,
                              ),
                            ),
                            const Icon(
                              Icons.access_time,
                              size: 13,
                              color: AppColors.textMuted,
                            ),
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
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildRecentAgents(
    BuildContext context,
    DeviceModel device,
    List<AgentModel> agents,
  ) {
    if (agents.isEmpty) {
      return _buildRecentAgent(context, device, null);
    }
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        for (var index = 0; index < agents.length; index++) ...[
          if (index > 0) const SizedBox(height: 6),
          _buildRecentAgent(context, device, agents[index]),
        ],
      ],
    );
  }

  Widget _buildRecentAgent(
    BuildContext context,
    DeviceModel device,
    AgentModel? agent,
  ) {
    final canOpenChat =
        !widget.sorting &&
        !widget.preview &&
        device.online &&
        agent != null &&
        agent.enabled &&
        agent.isOnline &&
        agent.projectId.trim().isNotEmpty;
    final status = _agentStatus(device, agent);
    final statusColor = _agentStatusColor(status);

    final content = Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
      decoration: BoxDecoration(
        color: canOpenChat
            ? AppColors.primary.withValues(alpha: 0.06)
            : AppColors.inputBackground,
        borderRadius: AppRadius.smRadius,
        border: Border.all(
          color: canOpenChat
              ? AppColors.primary.withValues(alpha: 0.18)
              : AppColors.border,
        ),
      ),
      child: Row(
        children: [
          Container(
            width: 28,
            height: 28,
            decoration: BoxDecoration(
              color: statusColor.withValues(alpha: 0.12),
              borderRadius: BorderRadius.circular(8),
            ),
            child: Icon(Icons.smart_toy_outlined, size: 16, color: statusColor),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: agent == null
                ? Text(
                    '暂无 Agent',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.bodySmall,
                  )
                : Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      _CompleteAgentName(
                        text: _agentDisplayName(agent),
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(height: 2),
                      _CompleteAgentName(
                        text: _agentSecondaryName(agent),
                        style: Theme.of(context).textTheme.labelSmall,
                      ),
                    ],
                  ),
          ),
          const SizedBox(width: 8),
          Container(
            constraints: const BoxConstraints(maxWidth: 68),
            padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 4),
            decoration: BoxDecoration(
              color: statusColor.withValues(alpha: 0.1),
              borderRadius: BorderRadius.circular(10),
            ),
            child: Text(
              status,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                color: statusColor,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
          if (canOpenChat) ...[
            const SizedBox(width: 2),
            const Icon(
              Icons.chevron_right_rounded,
              size: 18,
              color: AppColors.textMuted,
            ),
          ],
        ],
      ),
    );

    if (!canOpenChat) return content;
    return Tooltip(
      message: '进入 ${_agentDisplayName(agent)} 对话',
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTap: () => context.push(
          chatTargetRoute(
            ChatTarget(
              machineId: device.machineId,
              agentId: agent.agentId,
              projectId: agent.projectId,
              projectScopeId: agent.projectScopeId,
              projectRoot: agent.projectRoot,
              deviceName: device.effectiveName,
              agentName: _agentDisplayName(agent),
              semanticAgentName: agent.semanticAgentName.trim(),
              projectName: _projectDisplayName(agent),
            ),
          ),
        ),
        child: content,
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

class _CompleteAgentName extends StatelessWidget {
  final String text;
  final TextStyle? style;

  const _CompleteAgentName({required this.text, this.style});

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: double.infinity,
      child: FittedBox(
        fit: BoxFit.scaleDown,
        alignment: Alignment.centerLeft,
        child: Text(text, maxLines: 1, style: style),
      ),
    );
  }
}

List<AgentModel> _selectRecentAgents(DeviceModel device, {required int limit}) {
  if (device.agents.isEmpty || limit <= 0) return const [];
  final usable = device.agents
      .where((agent) => agent.projectId.trim().isNotEmpty)
      .toList();
  if (usable.isEmpty) return device.agents.take(limit).toList();

  final online = usable
      .where((agent) => agent.enabled && agent.isOnline)
      .toList();
  final candidates = online.isNotEmpty ? online : usable;
  candidates.sort((a, b) {
    final busyOrder = (b.isBusy ? 1 : 0).compareTo(a.isBusy ? 1 : 0);
    if (busyOrder != 0) return busyOrder;
    final aSeen = a.lastSeen;
    final bSeen = b.lastSeen;
    if (aSeen == null && bSeen == null) return 0;
    if (aSeen == null) return 1;
    if (bSeen == null) return -1;
    return bSeen.compareTo(aSeen);
  });
  return candidates.take(limit).toList(growable: false);
}

String _agentDisplayName(AgentModel agent) {
  return agent.displayName;
}

String _agentSecondaryName(AgentModel agent) {
  final semantic = agent.semanticAgentName.trim();
  return semantic.isEmpty ? _projectDisplayName(agent) : semantic;
}

String _projectDisplayName(AgentModel agent) {
  final normalized = agent.projectRoot.trim().replaceAll('\\', '/');
  final segments = normalized
      .split('/')
      .where((segment) => segment.isNotEmpty)
      .toList(growable: false);
  if (segments.isNotEmpty) return segments.last;
  return agent.projectId.trim().isEmpty ? '工作目录' : agent.projectId.trim();
}

String _agentStatus(DeviceModel device, AgentModel? agent) {
  if (agent == null) return '无 Agent';
  if (agent.isDisabled) return '已停用';
  if (agent.isFailed) return '异常';
  if (!device.online || agent.isStopped) return '离线';
  if (agent.isTransitioning) return '启动中';
  if (agent.isBusy) return '运行中';
  return '在线';
}

Color _agentStatusColor(String status) {
  switch (status) {
    case '运行中':
    case '启动中':
      return AppColors.statusWarning;
    case '在线':
      return AppColors.statusSuccess;
    case '异常':
      return AppColors.statusError;
    case '无 Agent':
      return AppColors.textMuted;
    case '已停用':
    case '离线':
    default:
      return AppColors.textSecondary;
  }
}

enum _DeviceCardAction { rename, copyIdentifier }

enum _DeviceTopBarAction { profile, settings, downloads, refresh }

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

  @override
  Widget build(BuildContext context) {
    final username = AppStorage.getUsername() ?? '-';
    final displayName = AppStorage.getDisplayName() ?? username;

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
                    child: const Icon(
                      Icons.person_outline,
                      size: 22,
                      color: AppColors.primary,
                    ),
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
                  // 后端地址
                  _editingUrl
                      ? Row(
                          children: [
                            const Icon(
                              Icons.dns_outlined,
                              size: 16,
                              color: AppColors.textMuted,
                            ),
                            const SizedBox(width: 10),
                            Expanded(
                              child: TextField(
                                controller: _baseUrlCtrl,
                                autofocus: true,
                                decoration: InputDecoration(
                                  hintText: 'http://127.0.0.1:8080',
                                  isDense: true,
                                  contentPadding: const EdgeInsets.symmetric(
                                    horizontal: 10,
                                    vertical: 8,
                                  ),
                                  border: OutlineInputBorder(
                                    borderRadius: AppRadius.smRadius,
                                    borderSide: const BorderSide(
                                      color: AppColors.border,
                                    ),
                                  ),
                                  enabledBorder: OutlineInputBorder(
                                    borderRadius: AppRadius.smRadius,
                                    borderSide: const BorderSide(
                                      color: AppColors.border,
                                    ),
                                  ),
                                  focusedBorder: OutlineInputBorder(
                                    borderRadius: AppRadius.smRadius,
                                    borderSide: const BorderSide(
                                      color: AppColors.primary,
                                      width: 1.5,
                                    ),
                                  ),
                                  filled: true,
                                  fillColor: AppColors.inputBackground,
                                ),
                                style: const TextStyle(
                                  fontSize: 13,
                                  fontFamily: 'monospace',
                                ),
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
                                        strokeWidth: 1.5,
                                      ),
                                    )
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
                            icon: const Icon(
                              Icons.edit_outlined,
                              size: 16,
                              color: AppColors.textMuted,
                            ),
                            tooltip: '修改',
                            onPressed: () => setState(() => _editingUrl = true),
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
                  icon: const Icon(
                    Icons.logout,
                    size: 16,
                    color: AppColors.statusError,
                  ),
                  label: const Text(
                    '退出登录',
                    style: TextStyle(color: AppColors.statusError),
                  ),
                  style: OutlinedButton.styleFrom(
                    side: BorderSide(
                      color: AppColors.statusError.withValues(alpha: 0.4),
                    ),
                    padding: const EdgeInsets.symmetric(vertical: 12),
                    shape: RoundedRectangleBorder(
                      borderRadius: AppRadius.smRadius,
                    ),
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
              style: const TextStyle(fontSize: 11, color: AppColors.textMuted),
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
