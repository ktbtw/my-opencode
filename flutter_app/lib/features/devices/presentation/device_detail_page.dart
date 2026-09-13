import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:file_picker/file_picker.dart';
import '../../../core/services/app_log_service.dart';
import 'package:go_router/go_router.dart';
import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;
import 'dart:typed_data';
import 'package:url_launcher/url_launcher.dart';

import '../../../core/config/api_client.dart';
import '../../../core/notifications/app_notification_controller.dart';
import '../../../core/notifications/app_notification_feedback.dart';
import '../../../core/notifications/app_notification_host.dart';
import '../../../core/notifications/app_notification_model.dart';
import '../../../core/storage/app_storage.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/drag_edge_auto_scroller.dart';
import '../../../shared/widgets/widgets.dart';
import '../../chat/data/chat_model.dart';
import '../../chat/presentation/chat_provider.dart';
import '../application/device_operation_notification_coordinator.dart';
import '../data/device_repository.dart';
import '../data/device_launcher_model.dart';
import '../data/device_mcp_config_model.dart';
import '../data/device_model.dart';
import '../data/device_project_memory_settings_model.dart';
import '../data/device_semantic_agent_model.dart';
import '../data/device_skill_model.dart';
import '../data/device_skill_import_parser.dart';
import 'device_directory_page.dart';
import 'device_provider.dart';
import 'device_skill_detail_dialog.dart';
import '../../project_memory/presentation/project_memory_model_picker.dart';

@visibleForTesting
Future<String?> showAgentRenameDialog({
  required BuildContext context,
  required String initialName,
  required String projectDirectoryName,
}) {
  return showDialog<String>(
    context: context,
    builder: (dialogContext) => _AgentRenameDialog(
      initialName: initialName,
      projectDirectoryName: projectDirectoryName,
    ),
  );
}

class _AgentRenameDialog extends StatefulWidget {
  final String initialName;
  final String projectDirectoryName;

  const _AgentRenameDialog({
    required this.initialName,
    required this.projectDirectoryName,
  });

  @override
  State<_AgentRenameDialog> createState() => _AgentRenameDialogState();
}

class _AgentRenameDialogState extends State<_AgentRenameDialog> {
  late final TextEditingController _controller;
  String _validationError = '';

  @override
  void initState() {
    super.initState();
    _controller = TextEditingController(text: widget.initialName);
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _submit() {
    final value = _controller.text.trim();
    if (value.isEmpty) {
      setState(() => _validationError = '请输入 Agent 名称');
      return;
    }
    if (value.characters.length > 80) {
      setState(() => _validationError = 'Agent 名称不能超过 80 个字符');
      return;
    }
    Navigator.of(context).pop(value);
  }

  @override
  Widget build(BuildContext context) {
    final buttonStyle = TextButton.styleFrom(
      minimumSize: const Size(0, 40),
      padding: const EdgeInsets.symmetric(horizontal: 4),
    );
    return AlertDialog(
      title: const Text('修改 Agent 名称'),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 380),
        child: TextField(
          controller: _controller,
          autofocus: true,
          maxLength: 80,
          textInputAction: TextInputAction.done,
          onSubmitted: (_) => _submit(),
          decoration: InputDecoration(
            labelText: 'Agent 名称',
            hintText: widget.projectDirectoryName,
            errorText: _validationError.isEmpty ? null : _validationError,
            prefixIcon: const Icon(Icons.smart_toy_outlined),
          ),
        ),
      ),
      actionsPadding: const EdgeInsets.fromLTRB(16, 4, 16, 16),
      actions: [
        SizedBox(
          width: double.infinity,
          child: Row(
            children: [
              Expanded(
                flex: 2,
                child: TextButton(
                  onPressed: () {
                    _controller.text = widget.projectDirectoryName;
                    _controller.selection = TextSelection.collapsed(
                      offset: _controller.text.length,
                    );
                    setState(() => _validationError = '');
                  },
                  style: buttonStyle,
                  child: const Text('使用目录名'),
                ),
              ),
              const SizedBox(width: 6),
              Expanded(
                child: TextButton(
                  onPressed: () => Navigator.of(context).pop(),
                  style: buttonStyle,
                  child: const Text('取消'),
                ),
              ),
              const SizedBox(width: 6),
              Expanded(
                child: FilledButton(
                  onPressed: _submit,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size(0, 40),
                    padding: const EdgeInsets.symmetric(horizontal: 4),
                  ),
                  child: const Text('保存'),
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

class DeviceDetailPage extends ConsumerStatefulWidget {
  final String machineId;
  const DeviceDetailPage({super.key, required this.machineId});

  @override
  ConsumerState<DeviceDetailPage> createState() => _DeviceDetailPageState();
}

class _DeviceDetailPageState extends ConsumerState<DeviceDetailPage>
    with WidgetsBindingObserver {
  bool _opencodeUpdateCheckScheduled = false;
  bool _opencodeUpdateCheckCompleted = false;
  bool _opencodeUpdateDialogShowing = false;
  String _opencodePromptedVersion = '';
  bool _sortingAgents = false;
  bool _pagedLayout = false;

  String get _layoutModeKey => 'device_detail_layout_mode_${widget.machineId}';
  final GlobalKey<_AgentListPanelState> _agentListPanelKey = GlobalKey();
  final ScrollController _desktopAgentScrollController = ScrollController();
  final ScrollController _mobileScrollController = ScrollController();
  final GlobalKey _desktopAgentViewportKey = GlobalKey();
  final GlobalKey _mobileViewportKey = GlobalKey();
  late final DragEdgeAutoScroller _desktopAgentAutoScroller;
  late final DragEdgeAutoScroller _mobileAgentAutoScroller;

  @override
  void initState() {
    super.initState();
    _pagedLayout = AppStorage.getString(_layoutModeKey) == 'paged';
    WidgetsBinding.instance.addObserver(this);
    _desktopAgentAutoScroller = DragEdgeAutoScroller(
      controller: _desktopAgentScrollController,
      viewportKey: _desktopAgentViewportKey,
    );
    _mobileAgentAutoScroller = DragEdgeAutoScroller(
      controller: _mobileScrollController,
      viewportKey: _mobileViewportKey,
    );
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _desktopAgentAutoScroller.dispose();
    _mobileAgentAutoScroller.dispose();
    _desktopAgentScrollController.dispose();
    _mobileScrollController.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _refreshDeviceStatus();
    }
  }

  void _refreshDeviceStatus() {
    ref.invalidate(deviceListProvider);
    ref.invalidate(deviceDetailProvider(widget.machineId));
  }

  void _scheduleOpencodeUpdateCheck(DeviceModel device) {
    if (_opencodeUpdateCheckCompleted || _opencodeUpdateCheckScheduled) {
      return;
    }
    if (!device.online || device.machineId.trim().isEmpty) {
      return;
    }
    _opencodeUpdateCheckScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _opencodeUpdateCheckScheduled = false;
      if (!mounted || _opencodeUpdateCheckCompleted) {
        return;
      }
      unawaited(_maybePromptOpencodeUpdate(device));
    });
  }

  Future<void> _maybePromptOpencodeUpdate(DeviceModel device) async {
    if (!mounted) return;
    final machineId = device.machineId.trim();
    if (machineId.isEmpty || !device.online) {
      return;
    }
    _opencodeUpdateCheckCompleted = true;
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final state = await repo.getDeviceLauncherState(machineId);
      if (!mounted) {
        return;
      }
      if (state.isUpgradeRunning) {
        final targetVersion = state.targetVersion.trim();
        if (targetVersion.isNotEmpty) {
          final notifications = ref.read(
            appNotificationControllerProvider.notifier,
          );
          final operationId = launcherUpgradeOperationId(
            machineId,
            targetVersion,
          );
          if (notifications.findByOperationId(operationId) == null) {
            startLauncherUpgradeNotification(
              notifications: notifications,
              machineId: machineId,
              targetVersion: targetVersion,
              sourceLabel: device.hostname,
            );
          }
          reconcileLauncherUpgradeNotification(
            notifications: notifications,
            operationId: operationId,
            targetVersion: targetVersion,
            state: state,
          );
        }
        return;
      }
      CliVersionInfo? latestInfo;
      try {
        latestInfo = await repo.getLatestCliVersion();
      } catch (_) {
        latestInfo = null;
      }
      final currentVersion = state.currentVersion.trim();
      final targetVersion = (latestInfo?.version ?? '').trim();
      if (currentVersion.isEmpty ||
          targetVersion.isEmpty ||
          _compareDottedVersion(currentVersion, targetVersion) >= 0) {
        return;
      }
      if (AppStorage.getSkippedOpencodeVersion(machineId) == targetVersion) {
        return;
      }
      if (_opencodePromptedVersion == targetVersion ||
          _opencodeUpdateDialogShowing) {
        return;
      }
      _opencodePromptedVersion = targetVersion;
      _opencodeUpdateDialogShowing = true;
      final action = await _showOpencodeUpdatePrompt(
        currentVersion: currentVersion,
        targetVersion: targetVersion,
        changelog: latestInfo?.changelog ?? '',
      );
      _opencodeUpdateDialogShowing = false;
      if (!mounted || action == null) {
        return;
      }
      switch (action) {
        case _OpencodeUpdatePromptAction.later:
          await AppLogService.log(
            'device_opencode_update_prompt_later',
            data: {'machine_id': machineId, 'target_version': targetVersion},
          );
          break;
        case _OpencodeUpdatePromptAction.skip:
          await AppStorage.setSkippedOpencodeVersion(machineId, targetVersion);
          await AppLogService.log(
            'device_opencode_update_prompt_skip',
            data: {'machine_id': machineId, 'target_version': targetVersion},
          );
          break;
        case _OpencodeUpdatePromptAction.update:
          await AppStorage.clearSkippedOpencodeVersion(machineId);
          await _startOpencodeUpdate(
            machineId: machineId,
            currentVersion: currentVersion,
            targetVersion: targetVersion,
          );
          break;
      }
    } catch (error) {
      await AppLogService.log(
        'device_opencode_update_prompt_failed',
        level: 'error',
        data: {'machine_id': machineId, 'error': error.toString()},
      );
    } finally {
      _opencodeUpdateDialogShowing = false;
    }
  }

  Future<_OpencodeUpdatePromptAction?> _showOpencodeUpdatePrompt({
    required String currentVersion,
    required String targetVersion,
    required String changelog,
  }) {
    return showDialog<_OpencodeUpdatePromptAction>(
      context: context,
      barrierDismissible: false,
      builder: (_) => _OpencodeUpdatePromptDialog(
        currentVersion: currentVersion,
        targetVersion: targetVersion,
        changelog: changelog,
      ),
    );
  }

  Future<void> _startOpencodeUpdate({
    required String machineId,
    required String currentVersion,
    required String targetVersion,
  }) async {
    final operationId = launcherUpgradeOperationId(machineId, targetVersion);
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    startLauncherUpgradeNotification(
      notifications: notifications,
      machineId: machineId,
      targetVersion: targetVersion,
    );
    final repo = ref.read(deviceRepositoryProvider);
    final initialState = _optimisticOpencodeUpgradeState(
      currentVersion: currentVersion,
      targetVersion: targetVersion,
    );
    await AppLogService.log(
      'device_opencode_update_started',
      data: {'machine_id': machineId, 'target_version': targetVersion},
    );
    await _showOpencodeUpgradeProgressDialog(
      machineId: machineId,
      targetVersion: targetVersion,
      initialState: initialState,
      startUpdate: () async {
        await repo.upgradeDeviceLauncher(
          machineId: machineId,
          targetVersion: targetVersion,
        );
      },
      notificationOperationId: operationId,
    );
    _refreshDeviceStatus();
  }

  DeviceLauncherState _optimisticOpencodeUpgradeState({
    required String currentVersion,
    required String targetVersion,
  }) {
    final now = DateTime.now().toUtc();
    return DeviceLauncherState.fromJson(const {}).copyWith(
      status: 'upgrading',
      currentVersion: currentVersion,
      targetVersion: targetVersion,
      lastError: '',
      upgradeLocked: true,
      upgradeStage: 'checking_version',
      upgradeProgress: 3,
      upgradeMessage: '升级任务已启动',
      upgradeStartedAt: now,
      upgradeUpdatedAt: now,
      upgradeLogs: [
        DeviceLauncherUpgradeLog(
          time: now,
          stage: 'checking_version',
          message: '升级任务已启动',
        ),
      ],
    );
  }

  Future<void> _showOpencodeUpgradeProgressDialog({
    required String machineId,
    required String targetVersion,
    required DeviceLauncherState initialState,
    required Future<void> Function() startUpdate,
    required String notificationOperationId,
  }) async {
    final progress = ValueNotifier<DeviceLauncherState>(initialState);
    var dialogOpen = true;
    var commandCompleted = false;
    Object? commandError;
    var pollFailures = 0;
    const maxPollFailures = 8;

    final dialogFuture =
        showDialog<void>(
          context: context,
          barrierDismissible: false,
          builder: (_) => _LauncherUpgradeProgressDialog(
            targetVersion: targetVersion,
            progress: progress,
          ),
        ).whenComplete(() {
          dialogOpen = false;
          progress.dispose();
        });

    final repo = ref.read(deviceRepositoryProvider);
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    unawaited(
      (() async {
        try {
          await startUpdate();
          commandCompleted = true;
          try {
            final state = await repo.getDeviceLauncherState(machineId);
            if (mounted && dialogOpen) {
              progress.value = state;
              _updateLauncherNotification(
                notifications,
                notificationOperationId,
                state,
              );
            }
          } catch (_) {}
        } catch (error) {
          commandCompleted = true;
          commandError = error;
          await AppLogService.log(
            'device_opencode_update_apply_failed',
            level: 'error',
            data: {'machine_id': machineId, 'error': error.toString()},
          );
        }
      })(),
    );

    while (mounted && dialogOpen) {
      await Future<void>.delayed(const Duration(seconds: 1));
      if (!mounted || !dialogOpen) break;
      try {
        final next = await repo.getDeviceLauncherState(machineId);
        if (!mounted || !dialogOpen) break;
        pollFailures = 0;
        progress.value = next;
        _updateLauncherNotification(
          notifications,
          notificationOperationId,
          next,
        );

        final backendFailed =
            next.upgradeStage == 'failed' ||
            next.status.toLowerCase() == 'failed';
        if (!next.isUpgradeRunning && backendFailed) {
          notifications.fail(
            notificationOperationId,
            title: 'Launcher 升级失败',
            error: next.lastError.isEmpty
                ? _launcherUpgradeDisplayMessage(next)
                : next.lastError,
            critical: true,
          );
          break;
        }

        if (!next.isUpgradeRunning &&
            next.currentVersion.trim() == targetVersion) {
          notifications.succeed(
            notificationOperationId,
            title: 'Launcher 升级完成',
            message: '设备已切换到 $targetVersion 并完成验证',
          );
          await AppLogService.log(
            'device_opencode_update_completed',
            data: {'machine_id': machineId, 'target_version': targetVersion},
          );
          break;
        }

        if (!next.isUpgradeRunning &&
            commandCompleted &&
            commandError != null) {
          final failed = next.copyWith(
            status: 'failed',
            upgradeLocked: false,
            upgradeStage: 'failed',
            upgradeProgress: next.upgradeProgress.clamp(0, 99).round(),
            upgradeMessage: commandError.toString(),
            lastError: commandError.toString(),
          );
          progress.value = failed;
          notifications.fail(
            notificationOperationId,
            title: 'Launcher 升级失败',
            error: commandError!,
            critical: true,
          );
          break;
        }
      } catch (error) {
        if (!mounted || !dialogOpen) break;
        pollFailures += 1;
        final current = progress.value;
        if (pollFailures <= maxPollFailures) {
          progress.value = current.copyWith(
            upgradeMessage: '网络波动，正在重新获取升级状态',
            upgradeUpdatedAt: DateTime.now().toUtc(),
            upgradeLogs: _appendLauncherUpgradeLog(
              current.upgradeLogs,
              'poll_retry',
              '状态同步中断，正在重试（$pollFailures/$maxPollFailures）',
            ),
          );
          continue;
        }
        final failed = current.copyWith(
          status: 'failed',
          upgradeLocked: false,
          upgradeStage: 'failed',
          upgradeProgress: current.upgradeProgress.clamp(0, 99).round(),
          upgradeMessage: '升级状态同步失败',
          lastError: error.toString(),
          upgradeLogs: _appendLauncherUpgradeLog(
            current.upgradeLogs,
            'failed',
            '升级状态同步失败',
          ),
        );
        progress.value = failed;
        notifications.fail(
          notificationOperationId,
          title: 'Launcher 升级状态同步失败',
          error: error,
          critical: true,
        );
        await AppLogService.log(
          'device_opencode_update_poll_failed',
          level: 'error',
          data: {'machine_id': machineId, 'error': error.toString()},
        );
        break;
      }
    }
    await dialogFuture;
  }

  void _updateLauncherNotification(
    AppNotificationController notifications,
    String operationId,
    DeviceLauncherState launcher,
  ) {
    final record = notifications.findByOperationId(operationId);
    final operation = record == null
        ? null
        : parseLauncherUpgradeOperation(record);
    reconcileLauncherUpgradeNotification(
      notifications: notifications,
      operationId: operationId,
      targetVersion: operation?.targetVersion ?? launcher.targetVersion,
      state: launcher,
    );
  }

  @override
  Widget build(BuildContext context) {
    final deviceAsync = ref.watch(deviceDetailProvider(widget.machineId));
    final isMobile = AppBreakpoints.isMobile(context);

    return Scaffold(
      floatingActionButton: _sortingAgents && !_pagedLayout
          ? FloatingActionButton.extended(
              heroTag: 'finish-agent-sorting',
              onPressed: () =>
                  _agentListPanelKey.currentState?._finishSorting(),
              tooltip: '完成排序',
              icon: const Icon(Icons.check_rounded, size: 20),
              label: const Text('完成排序'),
            )
          : null,
      body: SafeArea(
        child: PageBackground(
          child: Column(
            children: [
              _buildTopBar(context),
              Expanded(
                child: deviceAsync.when(
                  loading: () => const DeviceDetailSkeleton(),
                  error: (e, _) => Center(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const Icon(
                          Icons.error_outline,
                          size: 40,
                          color: AppColors.statusError,
                        ),
                        const SizedBox(height: 12),
                        Text(
                          e.toString(),
                          style: const TextStyle(color: AppColors.statusError),
                        ),
                        const SizedBox(height: 16),
                        AppButton(
                          label: '重试',
                          outlined: true,
                          onPressed: _refreshDeviceStatus,
                        ),
                      ],
                    ),
                  ),
                  data: (device) {
                    _scheduleOpencodeUpdateCheck(device);
                    if (_pagedLayout) {
                      return _PagedDeviceDetail(device: device);
                    }
                    return isMobile
                        ? _buildMobileLayout(context, device)
                        : _buildDesktopLayout(context, device);
                  },
                ),
              ),
            ],
          ),
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
          const Spacer(),
          IconButton(
            tooltip: _pagedLayout ? '切换为整页布局' : '切换为分页布局',
            icon: Icon(
              _pagedLayout ? Icons.view_agenda_outlined : Icons.tab_outlined,
              size: 18,
            ),
            onPressed: _toggleLayoutMode,
          ),
          const AppNotificationCenterButton(),
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
            key: _desktopAgentViewportKey,
            controller: _desktopAgentScrollController,
            padding: const EdgeInsets.all(24),
            child: _AgentListPanel(
              key: _agentListPanelKey,
              device: device,
              autoScroller: _desktopAgentAutoScroller,
              onSortingChanged: _setAgentSorting,
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildMobileLayout(BuildContext context, DeviceModel device) {
    return SingleChildScrollView(
      key: _mobileViewportKey,
      controller: _mobileScrollController,
      padding: const EdgeInsets.all(16),
      child: Column(
        children: [
          _DeviceInfoPanel(device: device),
          const SizedBox(height: 16),
          _AgentListPanel(
            key: _agentListPanelKey,
            device: device,
            autoScroller: _mobileAgentAutoScroller,
            onSortingChanged: _setAgentSorting,
          ),
        ],
      ),
    );
  }

  void _setAgentSorting(bool sorting) {
    if (_sortingAgents == sorting || !mounted) return;
    if (!sorting) {
      _desktopAgentAutoScroller.stop();
      _mobileAgentAutoScroller.stop();
    }
    setState(() => _sortingAgents = sorting);
  }

  void _toggleLayoutMode() {
    final next = !_pagedLayout;
    if (next) {
      _desktopAgentAutoScroller.stop();
      _mobileAgentAutoScroller.stop();
    }
    setState(() {
      _pagedLayout = next;
      if (next) _sortingAgents = false;
    });
    unawaited(AppStorage.setString(_layoutModeKey, next ? 'paged' : 'full'));
  }
}

class _DeviceInfoPanel extends ConsumerWidget {
  final DeviceModel device;
  const _DeviceInfoPanel({required this.device});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
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
                      device.effectiveName,
                      style: Theme.of(context).textTheme.titleLarge,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
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
              IconButton(
                onPressed: () => _renameDevice(context, ref),
                icon: const Icon(Icons.edit_outlined, size: 18),
                tooltip: '修改设备名称',
              ),
            ],
          ),
          const SizedBox(height: 20),
          const Divider(),
          const SizedBox(height: 16),
          InfoBlock(
            label: '系统主机名',
            value: device.hostname.isEmpty ? '未知' : device.hostname,
          ),
          const SizedBox(height: 14),
          InfoBlock(
            label: 'Agent 数量',
            value:
                '${device.runningAgentCount} 启动 / ${device.totalAgentCount} 总计',
          ),
          if (device.lastSeen != null) ...[
            const SizedBox(height: 14),
            InfoBlock(label: '最近在线', value: _formatDateTime(device.lastSeen!)),
          ],
          const SizedBox(height: 18),
          LayoutBuilder(
            builder: (context, constraints) {
              final singleColumn = constraints.maxWidth < 260;
              final entries = [
                _DeviceEntryButton(
                  icon: Icons.tune_rounded,
                  label: 'AI配置',
                  onTap: () => context.push(_buildAIConfigRoute(device)),
                ),
                _DeviceEntryButton(
                  icon: Icons.extension_rounded,
                  label: 'MCP配置',
                  onTap: () => context.push(_buildMCPConfigRoute(device)),
                ),
                _DeviceEntryButton(
                  icon: Icons.auto_awesome_outlined,
                  label: 'Skill配置',
                  onTap: () => context.push(_buildSkillConfigRoute(device)),
                ),
                _DeviceEntryButton(
                  icon: Icons.terminal_rounded,
                  label: '环境变量',
                  onTap: () => context.push(_buildEnvConfigRoute(device)),
                ),
                _DeviceEntryButton(
                  icon: Icons.devices_rounded,
                  label: 'Launcher',
                  onTap: () => _showLauncherDialog(context, device),
                ),
                _DeviceEntryButton(
                  icon: Icons.memory_outlined,
                  label: '项目记忆默认',
                  onTap: () => _showProjectMemoryDefaults(context, ref, device),
                ),
              ];
              if (AppBreakpoints.isMobile(context)) {
                return SingleChildScrollView(
                  scrollDirection: Axis.horizontal,
                  child: Row(
                    children: [
                      for (var index = 0; index < entries.length; index++)
                        Padding(
                          padding: EdgeInsets.only(
                            right: index == entries.length - 1 ? 0 : 12,
                          ),
                          child: SizedBox(width: 140, child: entries[index]),
                        ),
                    ],
                  ),
                );
              }
              return Wrap(
                spacing: 12,
                runSpacing: 12,
                children: [
                  for (final entry in entries)
                    SizedBox(
                      width: singleColumn
                          ? constraints.maxWidth
                          : (constraints.maxWidth - 12) / 2,
                      child: entry,
                    ),
                ],
              );
            },
          ),
        ],
      ),
    );
  }

  Future<void> _showProjectMemoryDefaults(
    BuildContext context,
    WidgetRef ref,
    DeviceModel device,
  ) async {
    await showDialog<void>(
      context: context,
      builder: (_) =>
          _DeviceProjectMemoryDefaultsDialog(machineId: device.machineId),
    );
  }

  Future<void> _renameDevice(BuildContext context, WidgetRef ref) async {
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

  Future<void> _showLauncherDialog(
    BuildContext context,
    DeviceModel device,
  ) async {
    await showDialog<void>(
      context: context,
      builder: (dialogContext) {
        final height = MediaQuery.of(dialogContext).size.height;
        return Dialog(
          insetPadding: const EdgeInsets.symmetric(
            horizontal: 16,
            vertical: 24,
          ),
          backgroundColor: Colors.transparent,
          child: ConstrainedBox(
            constraints: BoxConstraints(
              maxWidth: 560,
              maxHeight: height * 0.86,
            ),
            child: Container(
              decoration: BoxDecoration(
                color: AppColors.surface,
                borderRadius: AppRadius.lgRadius,
                border: Border.all(color: AppColors.border),
                boxShadow: const [
                  BoxShadow(
                    color: Color(0x1A1A3A6A),
                    blurRadius: 24,
                    offset: Offset(0, 12),
                  ),
                ],
              ),
              child: SingleChildScrollView(
                padding: const EdgeInsets.all(24),
                child: _LauncherPanel(
                  device: device,
                  framed: false,
                  headerTrailing: IconButton(
                    tooltip: '关闭',
                    onPressed: () => Navigator.of(dialogContext).pop(),
                    icon: const Icon(Icons.close_rounded),
                  ),
                ),
              ),
            ),
          ),
        );
      },
    );
  }

  String _buildAIConfigRoute(DeviceModel device) {
    final target = resolveModelTestTarget(agents: device.agents);
    final buffer = StringBuffer(
      '/devices/${Uri.encodeComponent(device.machineId)}/ai-config',
    );
    if (target != null) {
      buffer.write(
        '?agentId=${Uri.encodeComponent(target.agentId)}&projectId=${Uri.encodeComponent(target.projectId)}',
      );
    }
    return buffer.toString();
  }

  String _buildMCPConfigRoute(DeviceModel device) {
    return '/devices/${Uri.encodeComponent(device.machineId)}/mcp-config';
  }

  String _buildSkillConfigRoute(DeviceModel device) {
    return '/devices/${Uri.encodeComponent(device.machineId)}/skill-config';
  }

  String _buildEnvConfigRoute(DeviceModel device) {
    return '/devices/${Uri.encodeComponent(device.machineId)}/env-config';
  }

  String _formatDateTime(DateTime dt) {
    return '${dt.year}-${dt.month.toString().padLeft(2, '0')}-${dt.day.toString().padLeft(2, '0')} '
        '${dt.hour.toString().padLeft(2, '0')}:${dt.minute.toString().padLeft(2, '0')}';
  }
}

class _DeviceProjectMemoryDefaultsDialog extends ConsumerStatefulWidget {
  final String machineId;

  const _DeviceProjectMemoryDefaultsDialog({required this.machineId});

  @override
  ConsumerState<_DeviceProjectMemoryDefaultsDialog> createState() =>
      _DeviceProjectMemoryDefaultsDialogState();
}

class _DeviceProjectMemoryDefaultsDialogState
    extends ConsumerState<_DeviceProjectMemoryDefaultsDialog> {
  DeviceProjectMemorySettingsModel _settings =
      const DeviceProjectMemorySettingsModel();
  List<ModelInfo> _models = const [];
  ModelInfo? _selectedModel;
  String _variant = '';
  bool _loading = true;
  bool _loadFailed = false;
  bool _saving = false;
  String _error = '';

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _loadFailed = false;
      _error = '';
    });
    try {
      final settingsFuture = ref
          .read(deviceRepositoryProvider)
          .getDeviceProjectMemorySettings(widget.machineId);
      final modelsFuture = ref.refresh(
        availableModelsProvider(widget.machineId).future,
      );
      final settings = await settingsFuture;
      final models = await modelsFuture;
      if (!mounted) return;
      setState(() {
        _settings = settings;
        _models = models;
        _selectedModel = _findModel(settings.model, models);
        _variant = settings.variant;
        _loading = false;
      });
    } catch (error) {
      if (mounted) {
        setState(() {
          _loading = false;
          _loadFailed = true;
          _error = error.toString();
        });
      }
    }
  }

  ModelInfo? _findModel(String key, List<ModelInfo> models) {
    final normalized = key.trim();
    if (normalized.isEmpty) return null;
    return models.where((model) => model.metaKey == normalized).firstOrNull;
  }

  Future<void> _chooseModel() async {
    final selected = await showProjectMemoryModelPicker(
      context,
      models: _models,
      selected: _selectedModel,
    );
    if (selected == null || !mounted) return;
    setState(() {
      _selectedModel = selected;
      if (!selected.variants.contains(_variant)) _variant = '';
    });
  }

  Future<void> _save() async {
    final model = _selectedModel?.metaKey ?? '';
    setState(() => _saving = true);
    try {
      await ref
          .read(deviceRepositoryProvider)
          .updateDeviceProjectMemorySettings(
            machineId: widget.machineId,
            model: model,
            variant: _variant,
          );
      if (mounted) Navigator.of(context).pop();
    } catch (error) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = error.toString();
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final compact = MediaQuery.sizeOf(context).width < 480;
    return AlertDialog(
      insetPadding: EdgeInsets.symmetric(
        horizontal: compact ? 12 : 24,
        vertical: compact ? 16 : 24,
      ),
      titlePadding: EdgeInsets.fromLTRB(
        compact ? 18 : 24,
        compact ? 18 : 22,
        compact ? 18 : 24,
        8,
      ),
      contentPadding: EdgeInsets.fromLTRB(
        compact ? 18 : 24,
        12,
        compact ? 18 : 24,
        8,
      ),
      actionsPadding: EdgeInsets.fromLTRB(
        compact ? 18 : 24,
        10,
        compact ? 18 : 24,
        compact ? 18 : 20,
      ),
      title: Row(
        children: [
          Container(
            width: 36,
            height: 36,
            decoration: BoxDecoration(
              color: AppColors.primaryLight,
              borderRadius: AppRadius.smRadius,
            ),
            child: const Icon(
              Icons.auto_awesome_outlined,
              color: AppColors.primary,
              size: 20,
            ),
          ),
          const SizedBox(width: 12),
          const Expanded(child: Text('项目记忆默认配置')),
        ],
      ),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 420),
        child: _loading
            ? const Padding(
                padding: EdgeInsets.symmetric(vertical: 8),
                child: DialogContentSkeleton(itemCount: 2, showHeader: false),
              )
            : _loadFailed
            ? _ProjectMemoryLoadError(error: _error, onRetry: _load)
            : Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (_error.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 10),
                      child: Text(
                        _error,
                        style: const TextStyle(color: AppColors.statusError),
                      ),
                    ),
                  Material(
                    color: AppColors.surfaceElevated,
                    borderRadius: AppRadius.mdRadius,
                    child: InkWell(
                      onTap: _models.isEmpty ? null : _chooseModel,
                      borderRadius: AppRadius.mdRadius,
                      child: Container(
                        width: double.infinity,
                        padding: const EdgeInsets.fromLTRB(16, 12, 8, 14),
                        decoration: BoxDecoration(
                          border: Border.all(color: AppColors.border),
                          borderRadius: AppRadius.mdRadius,
                        ),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              children: [
                                const Icon(
                                  Icons.model_training_outlined,
                                  size: 18,
                                  color: AppColors.primary,
                                ),
                                const SizedBox(width: 8),
                                Text(
                                  '整理模型',
                                  style: Theme.of(context).textTheme.labelLarge
                                      ?.copyWith(color: AppColors.textMuted),
                                ),
                                const Spacer(),
                                IconButton(
                                  tooltip: '搜索并选择模型',
                                  onPressed: _models.isEmpty
                                      ? null
                                      : _chooseModel,
                                  icon: const Icon(Icons.search_rounded),
                                ),
                              ],
                            ),
                            const SizedBox(height: 4),
                            Text(
                              _selectedModel?.name ??
                                  (_settings.model.isEmpty
                                      ? '跟随任务模型'
                                      : _settings.model),
                              style: Theme.of(context).textTheme.titleMedium,
                            ),
                            const SizedBox(height: 5),
                            Text(
                              _selectedModel?.metaKey ??
                                  (_settings.model.isEmpty
                                      ? '最近任务模型 / Agent 默认模型'
                                      : '当前配置不在设备模型列表中'),
                              maxLines: 3,
                              overflow: TextOverflow.ellipsis,
                              style: Theme.of(context).textTheme.bodySmall
                                  ?.copyWith(
                                    color: AppColors.textMuted,
                                    fontFamily: _selectedModel == null
                                        ? null
                                        : 'monospace',
                                    height: 1.35,
                                  ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                  if (_selectedModel?.hasVariants ?? false) ...[
                    const SizedBox(height: 16),
                    DropdownButtonFormField<String>(
                      value: _selectedModel!.variants.contains(_variant)
                          ? _variant
                          : null,
                      decoration: const InputDecoration(labelText: '思考强度'),
                      items: [
                        const DropdownMenuItem(value: '', child: Text('默认')),
                        ..._selectedModel!.variants.map(
                          (variant) => DropdownMenuItem(
                            value: variant,
                            child: Text(variant),
                          ),
                        ),
                      ],
                      onChanged: (value) =>
                          setState(() => _variant = value ?? ''),
                    ),
                  ],
                  if (_models.isEmpty)
                    const Padding(
                      padding: EdgeInsets.only(top: 8),
                      child: Text('当前设备没有返回可用模型，请先完成 AI 配置。'),
                    ),
                ],
              ),
      ),
      actions: [
        SizedBox(
          width: double.infinity,
          child: Row(
            children: [
              Expanded(
                child: OutlinedButton(
                  onPressed: _saving ? null : () => Navigator.of(context).pop(),
                  child: const Text('取消'),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: FilledButton(
                  onPressed: _loading || _saving || _loadFailed ? null : _save,
                  child: _saving
                      ? const SizedBox.square(
                          dimension: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Text('保存'),
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

class _ProjectMemoryLoadError extends StatelessWidget {
  final String error;
  final VoidCallback onRetry;

  const _ProjectMemoryLoadError({required this.error, required this.onRetry});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 12),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(
            Icons.cloud_off_outlined,
            size: 34,
            color: AppColors.statusError,
          ),
          const SizedBox(height: 10),
          const Text('设备模型加载失败'),
          const SizedBox(height: 6),
          Text(
            error,
            maxLines: 3,
            overflow: TextOverflow.ellipsis,
            textAlign: TextAlign.center,
            style: Theme.of(
              context,
            ).textTheme.bodySmall?.copyWith(color: AppColors.textMuted),
          ),
          const SizedBox(height: 12),
          OutlinedButton.icon(
            onPressed: onRetry,
            icon: const Icon(Icons.refresh_rounded, size: 18),
            label: const Text('重试'),
          ),
        ],
      ),
    );
  }
}

class _DeviceEntryButton extends StatelessWidget {
  final IconData icon;
  final String label;
  final VoidCallback onTap;

  const _DeviceEntryButton({
    required this.icon,
    required this.label,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      borderRadius: AppRadius.mdRadius,
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 14),
        decoration: BoxDecoration(
          color: AppColors.inputBackground,
          borderRadius: AppRadius.mdRadius,
          border: Border.all(color: AppColors.border),
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              width: 36,
              height: 36,
              decoration: BoxDecoration(
                color: AppColors.primaryLight,
                borderRadius: AppRadius.mdRadius,
              ),
              child: Icon(icon, color: AppColors.primary, size: 18),
            ),
            const SizedBox(height: 8),
            Text(
              label,
              style: Theme.of(context).textTheme.labelLarge,
              textAlign: TextAlign.center,
            ),
          ],
        ),
      ),
    );
  }
}

enum _OpencodeUpdatePromptAction { later, skip, update }

class _OpencodeUpdatePromptDialog extends StatelessWidget {
  final String currentVersion;
  final String targetVersion;
  final String changelog;

  const _OpencodeUpdatePromptDialog({
    required this.currentVersion,
    required this.targetVersion,
    required this.changelog,
  });

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
      title: const Text(
        '发现 opencode 新版本',
        style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
      ),
      content: ConstrainedBox(
        constraints: const BoxConstraints(minWidth: 320, maxWidth: 420),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (currentVersion.trim().isNotEmpty)
              Text(
                '当前版本: $currentVersion',
                style: const TextStyle(fontSize: 14, color: Colors.black87),
              ),
            Text(
              '新版本: $targetVersion',
              style: const TextStyle(fontSize: 14, color: Colors.black87),
            ),
            if (changelog.trim().isNotEmpty) ...[
              const SizedBox(height: 8),
              ConstrainedBox(
                constraints: const BoxConstraints(maxHeight: 156),
                child: Scrollbar(
                  thumbVisibility: changelog.length > 160,
                  child: SingleChildScrollView(
                    child: Text(
                      changelog,
                      style: const TextStyle(
                        fontSize: 13,
                        color: Colors.black54,
                        height: 1.35,
                      ),
                    ),
                  ),
                ),
              ),
            ],
          ],
        ),
      ),
      actionsPadding: const EdgeInsets.fromLTRB(20, 0, 20, 16),
      actions: [
        SizedBox(
          width: double.infinity,
          child: Row(
            children: [
              Expanded(
                child: TextButton(
                  onPressed: () => Navigator.of(
                    context,
                  ).pop(_OpencodeUpdatePromptAction.later),
                  style: TextButton.styleFrom(
                    minimumSize: const Size(0, 40),
                    padding: const EdgeInsets.symmetric(horizontal: 4),
                    textStyle: const TextStyle(fontSize: 13),
                  ),
                  child: const FittedBox(
                    fit: BoxFit.scaleDown,
                    child: Text('稍后再说'),
                  ),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: TextButton(
                  onPressed: () => Navigator.of(
                    context,
                  ).pop(_OpencodeUpdatePromptAction.skip),
                  style: TextButton.styleFrom(
                    minimumSize: const Size(0, 40),
                    padding: const EdgeInsets.symmetric(horizontal: 4),
                    textStyle: const TextStyle(fontSize: 13),
                  ),
                  child: const FittedBox(
                    fit: BoxFit.scaleDown,
                    child: Text('跳过此版本'),
                  ),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: ElevatedButton(
                  onPressed: () => Navigator.of(
                    context,
                  ).pop(_OpencodeUpdatePromptAction.update),
                  style: ElevatedButton.styleFrom(
                    minimumSize: const Size(0, 40),
                    padding: const EdgeInsets.symmetric(horizontal: 4),
                    backgroundColor: const Color(0xFF3B82F6),
                    foregroundColor: Colors.white,
                    textStyle: const TextStyle(fontSize: 13),
                    shape: RoundedRectangleBorder(
                      borderRadius: BorderRadius.circular(8),
                    ),
                  ),
                  child: const FittedBox(
                    fit: BoxFit.scaleDown,
                    child: Text('立即更新'),
                  ),
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

class _AgentListPanel extends ConsumerStatefulWidget {
  final DeviceModel device;
  final DragEdgeAutoScroller autoScroller;
  final ValueChanged<bool> onSortingChanged;

  const _AgentListPanel({
    super.key,
    required this.device,
    required this.autoScroller,
    required this.onSortingChanged,
  });

  @override
  ConsumerState<_AgentListPanel> createState() => _AgentListPanelState();
}

class _AgentListPanelState extends ConsumerState<_AgentListPanel> {
  bool _sorting = false;
  bool _saving = false;
  bool _dirty = false;
  String? _draggingAgentId;
  late List<AgentModel> _agents;

  DeviceModel get device => widget.device;

  @override
  void initState() {
    super.initState();
    _agents = List<AgentModel>.of(device.agents);
  }

  @override
  void didUpdateWidget(covariant _AgentListPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    final latest = {for (final agent in device.agents) agent.agentId: agent};
    if (!_sorting) {
      _agents = List<AgentModel>.of(device.agents);
      return;
    }
    _agents = [
      for (final agent in _agents)
        if (latest.containsKey(agent.agentId)) latest[agent.agentId]!,
      for (final agent in device.agents)
        if (!_agents.any((item) => item.agentId == agent.agentId)) agent,
    ];
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.only(bottom: 12),
          child: Row(
            children: [
              Text(
                'Agent 列表',
                style: Theme.of(context).textTheme.headlineSmall,
              ),
              const SizedBox(width: 10),
              StatusPill(
                label:
                    '${device.runningAgentCount} / ${device.totalAgentCount}',
                type: StatusType.processing,
              ),
            ],
          ),
        ),
        if (_agents.isEmpty)
          const EmptyState(
            message: '该设备暂无 Agent',
            icon: Icons.smart_toy_outlined,
          )
        else
          ..._agents.map(
            (agent) => Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: _buildSortableAgentCard(agent),
            ),
          ),
      ],
    );
  }

  Widget _buildSortableAgentCard(AgentModel agent) {
    final card = DragTarget<String>(
      onWillAcceptWithDetails: (details) {
        final sourceId = details.data;
        if (sourceId == agent.agentId) return false;
        _moveAgent(sourceId, agent.agentId);
        return true;
      },
      builder: (context, candidates, rejected) => Opacity(
        opacity: _draggingAgentId == agent.agentId ? 0.34 : 1,
        child: Stack(
          children: [
            _AgentCard(agent: agent, device: device),
            if (_sorting)
              const Positioned(
                top: 12,
                right: 12,
                child: Icon(
                  Icons.drag_indicator_rounded,
                  size: 20,
                  color: AppColors.textMuted,
                ),
              ),
          ],
        ),
      ),
    );
    Widget feedback() => Material(
      color: Colors.transparent,
      child: SizedBox(
        width: math.min(MediaQuery.sizeOf(context).width - 32, 620),
        child: _AgentCard(agent: agent, device: device),
      ),
    );
    if (_sorting) {
      return Draggable<String>(
        data: agent.agentId,
        feedback: feedback(),
        onDragStarted: () => setState(() => _draggingAgentId = agent.agentId),
        onDragUpdate: (details) =>
            widget.autoScroller.update(details.globalPosition),
        onDragEnd: (_) => _completeDrag(),
        childWhenDragging: card,
        child: card,
      );
    }
    return LongPressDraggable<String>(
      data: agent.agentId,
      feedback: feedback(),
      onDragUpdate: (details) =>
          widget.autoScroller.update(details.globalPosition),
      onDragStarted: () {
        setState(() {
          _sorting = true;
          _draggingAgentId = agent.agentId;
        });
        widget.onSortingChanged(true);
      },
      onDragEnd: (_) => _completeDrag(),
      childWhenDragging: card,
      child: card,
    );
  }

  void _moveAgent(String sourceId, String targetId) {
    final from = _agents.indexWhere((item) => item.agentId == sourceId);
    final to = _agents.indexWhere((item) => item.agentId == targetId);
    if (from < 0 || to < 0 || from == to) return;
    setState(() {
      final next = List<AgentModel>.of(_agents);
      final item = next.removeAt(from);
      next.insert(to, item);
      _agents = next;
      _dirty = true;
    });
  }

  void _completeDrag() {
    widget.autoScroller.stop();
    setState(() => _draggingAgentId = null);
    if (_dirty) unawaited(_persistOrder());
  }

  void _finishSorting() {
    widget.autoScroller.stop();
    setState(() {
      _sorting = false;
      _draggingAgentId = null;
    });
    widget.onSortingChanged(false);
    if (_dirty) unawaited(_persistOrder());
  }

  Future<void> _persistOrder() async {
    if (_saving || !_dirty) return;
    setState(() {
      _saving = true;
      _dirty = false;
    });
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    final operationId = 'agents:reorder:${device.machineId}';
    notifications.start(
      operationId: operationId,
      title: '正在同步 Agent 顺序',
      message: device.effectiveName,
      progressMode: AppNotificationProgressMode.indeterminate,
      displayStyle: AppNotificationDisplayStyle.sync,
      kind: AppNotificationKind.agent,
      scope: AppNotificationScope.synced,
    );
    try {
      await ref
          .read(deviceRepositoryProvider)
          .reorderDeviceAgents(
            machineId: device.machineId,
            agentIds: _agents
                .map((item) => item.agentId)
                .toList(growable: false),
          );
      // Sorting is an inline editing operation; do not retain its progress
      // toast after the order has been saved.
      notifications.dismissOperation(operationId);
      ref.invalidate(deviceListProvider);
      ref.invalidate(deviceDetailProvider(device.machineId));
    } catch (error) {
      notifications.fail(operationId, title: 'Agent 顺序同步失败', error: error);
      ref.invalidate(deviceDetailProvider(device.machineId));
    } finally {
      if (mounted) setState(() => _saving = false);
      if (_dirty) unawaited(_persistOrder());
    }
  }
}

class _LauncherPanel extends ConsumerStatefulWidget {
  final DeviceModel device;
  final bool framed;
  final Widget? headerTrailing;

  const _LauncherPanel({
    required this.device,
    this.framed = true,
    this.headerTrailing,
  });

  @override
  ConsumerState<_LauncherPanel> createState() => _LauncherPanelState();
}

class _LauncherPanelState extends ConsumerState<_LauncherPanel> {
  final _versionController = TextEditingController();
  DeviceLauncherState? _state;
  CliVersionInfo? _latestCliVersion;
  bool _loading = true;
  bool _upgrading = false;
  bool _rollingBack = false;
  bool _upgradeDialogActive = false;
  String _error = '';

  String get _machineId => widget.device.machineId;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    final targetVersion = _state?.targetVersion.trim() ?? '';
    if (_state?.isUpgradeRunning == true && targetVersion.isNotEmpty) {
      ref
          .read(appNotificationControllerProvider.notifier)
          .waitForSync(
            launcherUpgradeOperationId(_machineId, targetVersion),
            message: '升级仍在设备端执行，等待状态同步',
          );
    }
    _versionController.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    if (_machineId.isEmpty) {
      setState(() {
        _loading = false;
        _error = '当前设备不可用';
      });
      return;
    }
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final state = await repo.getDeviceLauncherState(_machineId);
      CliVersionInfo? cliVersion;
      try {
        cliVersion = await repo.getLatestCliVersion();
      } catch (_) {
        cliVersion = null;
      }
      setState(() {
        _state = state;
        _latestCliVersion = cliVersion;
        if (_versionController.text.trim().isEmpty &&
            cliVersion?.version.trim().isNotEmpty == true) {
          _versionController.text = cliVersion!.version.trim();
        }
      });
      if (state.isUpgradeRunning && state.targetVersion.trim().isNotEmpty) {
        final targetVersion = state.targetVersion.trim();
        final notifications = ref.read(
          appNotificationControllerProvider.notifier,
        );
        final operationId = launcherUpgradeOperationId(
          _machineId,
          targetVersion,
        );
        if (notifications.findByOperationId(operationId) == null) {
          startLauncherUpgradeNotification(
            notifications: notifications,
            machineId: _machineId,
            targetVersion: targetVersion,
            sourceLabel: widget.device.hostname,
          );
        }
        reconcileLauncherUpgradeNotification(
          notifications: notifications,
          operationId: operationId,
          targetVersion: targetVersion,
          state: state,
        );
      }
      if (state.isUpgradeRunning && !_upgradeDialogActive && mounted) {
        _upgradeDialogActive = true;
        unawaited(
          _showUpgradeProgressDialog(
            state.targetVersion.isNotEmpty
                ? state.targetVersion
                : _versionController.text.trim(),
          ).whenComplete(() => _upgradeDialogActive = false),
        );
      }
    } catch (e) {
      setState(() {
        _error = e.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  Future<void> _upgrade() async {
    final version = _versionController.text.trim();
    if (version.isEmpty) {
      setState(() {
        _error = '请输入目标版本';
      });
      return;
    }
    if (_upgradeDialogActive) {
      return;
    }
    final repo = ref.read(deviceRepositoryProvider);
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    startLauncherUpgradeNotification(
      notifications: notifications,
      machineId: _machineId,
      targetVersion: version,
      sourceLabel: widget.device.hostname,
    );
    final optimisticState = _optimisticUpgradeState(version);
    setState(() {
      _upgrading = true;
      _error = '';
      _state = optimisticState;
    });
    unawaited(
      AppLogService.log(
        'device_launcher_upgrade_started',
        data: {'machine_id': _machineId, 'target_version': version},
      ),
    );
    _upgradeDialogActive = true;
    unawaited(
      _showUpgradeProgressDialog(
        version,
        initialState: optimisticState,
        startUpgrade: () async {
          final state = await repo.upgradeDeviceLauncher(
            machineId: _machineId,
            targetVersion: version,
          );
          await AppLogService.log(
            'device_launcher_upgrade_dispatched',
            data: {
              'machine_id': _machineId,
              'status': state.status,
              'target_version': state.targetVersion,
              'current_version': state.currentVersion,
            },
          );
          return state;
        },
      ).whenComplete(() {
        _upgradeDialogActive = false;
        if (mounted) {
          setState(() {
            _upgrading = false;
          });
        }
      }),
    );
  }

  DeviceLauncherState _optimisticUpgradeState(String targetVersion) {
    final now = DateTime.now().toUtc();
    final base = _state ?? DeviceLauncherState.fromJson(const {});
    final logs = <DeviceLauncherUpgradeLog>[
      ...base.upgradeLogs,
      DeviceLauncherUpgradeLog(
        time: now,
        stage: 'checking_version',
        message: '正在发送升级命令',
      ),
    ];
    return base.copyWith(
      status: 'upgrading',
      targetVersion: targetVersion,
      lastError: '',
      upgradeLocked: true,
      upgradeStage: 'checking_version',
      upgradeProgress: base.upgradeProgress > 0 && base.isUpgradeRunning
          ? base.upgradeProgress
          : 1,
      upgradeMessage: '正在发送升级命令',
      upgradeStartedAt: base.upgradeStartedAt ?? now,
      upgradeUpdatedAt: now,
      upgradeLogs: logs,
    );
  }

  Future<void> _showUpgradeProgressDialog(
    String targetVersion, {
    DeviceLauncherState? initialState,
    Future<DeviceLauncherState> Function()? startUpgrade,
  }) async {
    final initial =
        initialState ?? _state ?? DeviceLauncherState.fromJson(const {});
    final progress = ValueNotifier<DeviceLauncherState>(initial);
    var dialogOpen = true;
    var commandCompleted = startUpgrade == null;
    Object? commandError;
    var pollFailures = 0;
    const maxPollFailures = 6;
    unawaited(
      showDialog<void>(
        context: context,
        barrierDismissible: false,
        builder: (_) => _LauncherUpgradeProgressDialog(
          targetVersion: targetVersion,
          progress: progress,
        ),
      ).whenComplete(() {
        dialogOpen = false;
        progress.dispose();
      }),
    );

    final repo = ref.read(deviceRepositoryProvider);
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    final operationId = launcherUpgradeOperationId(_machineId, targetVersion);
    if (startUpgrade != null) {
      unawaited(
        (() async {
          try {
            final state = await startUpgrade();
            commandCompleted = true;
            if (!mounted || !dialogOpen) return;
            setState(() => _state = state);
            progress.value = state;
            reconcileLauncherUpgradeNotification(
              notifications: notifications,
              operationId: operationId,
              targetVersion: targetVersion,
              state: state,
            );
          } catch (error) {
            commandCompleted = true;
            commandError = error;
            await AppLogService.log(
              'device_launcher_upgrade_failed',
              level: 'error',
              data: {'machine_id': _machineId, 'error': error.toString()},
            );
          }
        })(),
      );
    }
    while (mounted && dialogOpen) {
      await Future<void>.delayed(const Duration(seconds: 1));
      if (!mounted || !dialogOpen) break;
      try {
        final next = await repo.getDeviceLauncherState(_machineId);
        if (!mounted || !dialogOpen) break;
        pollFailures = 0;
        setState(() => _state = next);
        if (!next.isUpgradeRunning && !commandCompleted) {
          continue;
        }
        progress.value = next;
        reconcileLauncherUpgradeNotification(
          notifications: notifications,
          operationId: operationId,
          targetVersion: targetVersion,
          state: next,
        );
        if (!next.isUpgradeRunning) {
          if (commandError != null &&
              next.upgradeStage != 'completed' &&
              next.upgradeStage != 'already_latest') {
            final failed = next.copyWith(
              status: 'failed',
              upgradeLocked: false,
              upgradeStage: 'failed',
              upgradeProgress: next.upgradeProgress.clamp(0, 99).round(),
              upgradeMessage: commandError.toString(),
              lastError: commandError.toString(),
            );
            progress.value = failed;
            setState(() {
              _state = failed;
              _error = commandError.toString();
            });
            notifications.fail(
              operationId,
              title: 'Launcher 升级失败',
              error: commandError!,
              critical: true,
            );
            break;
          }
          await AppLogService.log(
            next.upgradeStage == 'failed'
                ? 'device_launcher_upgrade_failed'
                : 'device_launcher_upgrade_completed',
            level: next.upgradeStage == 'failed' ? 'error' : 'info',
            data: {
              'machine_id': _machineId,
              'status': next.status,
              'stage': next.upgradeStage,
              'target_version': next.targetVersion,
              'current_version': next.currentVersion,
              'error': next.lastError,
            },
          );
          break;
        }
      } catch (e) {
        if (!mounted || !dialogOpen) break;
        pollFailures += 1;
        final current = progress.value;
        if (current.isUpgradeRunning && pollFailures <= maxPollFailures) {
          final retryState = current.copyWith(
            upgradeMessage: '网络波动，正在重新获取升级状态',
            upgradeUpdatedAt: DateTime.now().toUtc(),
            upgradeLogs: _appendLauncherUpgradeLog(
              current.upgradeLogs,
              'poll_retry',
              '状态同步中断，正在重试（$pollFailures/$maxPollFailures）',
            ),
          );
          progress.value = retryState;
          setState(() {
            _state = retryState;
            _error = '';
          });
          continue;
        }
        final failed = current.copyWith(
          status: 'failed',
          upgradeLocked: false,
          upgradeStage: 'failed',
          upgradeProgress: current.upgradeProgress.clamp(0, 99).round(),
          upgradeMessage: '升级状态同步失败',
          lastError: e.toString(),
          upgradeLogs: _appendLauncherUpgradeLog(
            current.upgradeLogs,
            'failed',
            '升级状态同步失败',
          ),
        );
        progress.value = failed;
        setState(() {
          _state = failed;
          _error = e.toString();
        });
        notifications.fail(
          operationId,
          title: 'Launcher 升级状态同步失败',
          error: e,
          critical: true,
        );
        break;
      }
    }
  }

  Future<void> _rollback() async {
    setState(() {
      _rollingBack = true;
      _error = '';
    });
    try {
      await AppLogService.log(
        'device_launcher_rollback_started',
        data: {'machine_id': _machineId},
      );
      final repo = ref.read(deviceRepositoryProvider);
      final state = await repo.rollbackDeviceLauncher(machineId: _machineId);
      await AppLogService.log(
        'device_launcher_rollback_completed',
        data: {
          'machine_id': _machineId,
          'status': state.status,
          'current_version': state.currentVersion,
          'previous_version': state.previousVersion,
        },
      );
      setState(() {
        _state = state;
      });
      if (!mounted) return;
      showAppFeedback(context, message: '回滚命令已执行');
    } catch (e) {
      await AppLogService.log(
        'device_launcher_rollback_failed',
        level: 'error',
        data: {'machine_id': _machineId, 'error': e.toString()},
      );
      setState(() {
        _error = e.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _rollingBack = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final content = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Text('Launcher 管理', style: Theme.of(context).textTheme.titleMedium),
            const Spacer(),
            IconButton(
              onPressed: _loading ? null : _load,
              icon: const Icon(Icons.refresh_rounded),
            ),
            if (widget.headerTrailing != null) ...[
              const SizedBox(width: 4),
              widget.headerTrailing!,
            ],
          ],
        ),
        if (_loading)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: DialogContentSkeleton(itemCount: 3, showHeader: false),
          )
        else ...[
          LayoutBuilder(
            builder: (context, constraints) {
              final isSingleColumn = constraints.maxWidth < 280;
              final itemWidth = isSingleColumn
                  ? constraints.maxWidth
                  : (constraints.maxWidth - 16) / 2;
              return Wrap(
                spacing: 16,
                runSpacing: 12,
                children: [
                  _LauncherInfoItem(
                    width: itemWidth,
                    label: '状态',
                    value: _state?.status.isNotEmpty == true
                        ? _state!.status
                        : '-',
                  ),
                  _LauncherInfoItem(
                    width: itemWidth,
                    label: '当前版本',
                    value: _state?.currentVersion.isNotEmpty == true
                        ? _state!.currentVersion
                        : '-',
                  ),
                  _LauncherInfoItem(
                    width: itemWidth,
                    label: '最新 CLI 版本',
                    value: _latestCliVersion?.version.isNotEmpty == true
                        ? _latestCliVersion!.version
                        : '-',
                  ),
                  _LauncherInfoItem(
                    width: itemWidth,
                    label: '回滚版本',
                    value: _state?.previousVersion.isNotEmpty == true
                        ? _state!.previousVersion
                        : '-',
                  ),
                  _LauncherInfoItem(
                    width: itemWidth,
                    label: 'Agent 数量',
                    value: '${_state?.agentCount ?? 0}',
                  ),
                ],
              );
            },
          ),
          const SizedBox(height: 16),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('允许访问所有目录'),
            value: _state?.allowAllDirectories ?? false,
            onChanged: _loading || _upgrading || _rollingBack
                ? null
                : (value) async {
                    if (value) {
                      final changed = await showModalBottomSheet<bool>(
                        context: context,
                        isScrollControlled: true,
                        backgroundColor: Colors.transparent,
                        builder: (_) =>
                            _DirectoryAccessVerifySheet(machineId: _machineId),
                      );
                      if (changed == true) {
                        await _load();
                      }
                      return;
                    }
                    try {
                      final repo = ref.read(deviceRepositoryProvider);
                      final allowAll = await repo.updateDeviceDirectoryAccess(
                        machineId: _machineId,
                        allowAll: false,
                      );
                      setState(() {
                        _state =
                            (_state ?? DeviceLauncherState.fromJson(const {}))
                                .copyWith(allowAllDirectories: allowAll);
                      });
                    } catch (e) {
                      setState(() {
                        _error = e.toString();
                      });
                    }
                  },
          ),
          const SizedBox(height: 8),
          TextField(
            controller: _versionController,
            decoration: InputDecoration(
              labelText: '目标版本',
              hintText: _latestCliVersion?.version.isNotEmpty == true
                  ? '默认使用 ${_latestCliVersion!.version}'
                  : '输入版本号',
            ),
          ),
          const SizedBox(height: 12),
          LayoutBuilder(
            builder: (context, constraints) {
              final compact = constraints.maxWidth < 360;
              final buttonWidth = compact
                  ? (constraints.maxWidth - 12) / 2
                  : (constraints.maxWidth - 24) / 3;
              return Wrap(
                spacing: 12,
                runSpacing: 12,
                children: [
                  SizedBox(
                    width: buttonWidth,
                    child: AppButton(
                      label: _upgrading ? '升级中' : '升级',
                      icon: Icons.system_update_alt_rounded,
                      onPressed:
                          _upgrading || _rollingBack || _upgradeDialogActive
                          ? null
                          : _upgrade,
                    ),
                  ),
                  SizedBox(
                    width: buttonWidth,
                    child: AppButton(
                      label: _rollingBack ? '回滚中' : '回滚',
                      outlined: true,
                      icon: Icons.history_rounded,
                      onPressed: _upgrading || _rollingBack ? null : _rollback,
                    ),
                  ),
                  SizedBox(
                    width: buttonWidth,
                    child: AppButton(
                      label: '目录',
                      outlined: true,
                      icon: Icons.folder_open_rounded,
                      onPressed: () async {
                        final created = await Navigator.of(context).push<bool>(
                          MaterialPageRoute(
                            builder: (_) => DeviceDirectoryPage(
                              machineId: widget.device.machineId,
                            ),
                          ),
                        );
                        if (created != true || !mounted) return;
                        ref.invalidate(deviceListProvider);
                        ref.invalidate(
                          deviceDetailProvider(widget.device.machineId),
                        );
                      },
                    ),
                  ),
                ],
              );
            },
          ),
          if (_state?.lastError.isNotEmpty == true) ...[
            const SizedBox(height: 12),
            Text(
              _state!.lastError,
              style: const TextStyle(color: AppColors.statusError),
            ),
          ],
          if (_error.isNotEmpty) ...[
            const SizedBox(height: 12),
            Text(_error, style: const TextStyle(color: AppColors.statusError)),
          ],
        ],
      ],
    );

    if (!widget.framed) {
      return content;
    }

    return PanelCard(child: content);
  }
}

class _LauncherInfoItem extends StatelessWidget {
  final double width;
  final String label;
  final String value;

  const _LauncherInfoItem({
    required this.width,
    required this.label,
    required this.value,
  });

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: width,
      child: InfoBlock(label: label, value: value),
    );
  }
}

class _LauncherUpgradeProgressDialog extends StatelessWidget {
  final String targetVersion;
  final ValueNotifier<DeviceLauncherState> progress;

  const _LauncherUpgradeProgressDialog({
    required this.targetVersion,
    required this.progress,
  });

  @override
  Widget build(BuildContext context) {
    return Dialog(
      backgroundColor: AppColors.surface,
      insetPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
      shape: RoundedRectangleBorder(borderRadius: AppRadius.lgRadius),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 520),
        child: Padding(
          padding: const EdgeInsets.all(22),
          child: ValueListenableBuilder<DeviceLauncherState>(
            valueListenable: progress,
            builder: (context, state, _) {
              final failed =
                  state.upgradeStage == 'failed' ||
                  state.status.toLowerCase() == 'failed';
              final done = !state.isUpgradeRunning;
              final progressPercent = failed
                  ? state.upgradeProgress.clamp(0, 99).round()
                  : done
                  ? 100
                  : state.upgradeProgress.clamp(0, 100).round();
              final value = (progressPercent / 100).clamp(0.0, 1.0);
              final color = _launcherUpgradeStatusColor(state);
              final message = _launcherUpgradeDisplayMessage(state);
              final logs = _compactLauncherUpgradeLogs(state.upgradeLogs);
              return Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      SizedBox(
                        width: 42,
                        height: 42,
                        child: Stack(
                          alignment: Alignment.center,
                          children: [
                            AnimatedContainer(
                              duration: const Duration(milliseconds: 240),
                              width: 42,
                              height: 42,
                              decoration: BoxDecoration(
                                color: color.withValues(alpha: 0.12),
                                borderRadius: AppRadius.mdRadius,
                                border: Border.all(
                                  color: color.withValues(alpha: 0.18),
                                ),
                              ),
                            ),
                            if (!done && !failed)
                              SizedBox(
                                width: 42,
                                height: 42,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                  valueColor: AlwaysStoppedAnimation<Color>(
                                    color.withValues(alpha: 0.72),
                                  ),
                                  backgroundColor: color.withValues(
                                    alpha: 0.08,
                                  ),
                                ),
                              ),
                            Icon(
                              failed
                                  ? Icons.error_outline_rounded
                                  : done
                                  ? Icons.check_circle_outline_rounded
                                  : Icons.system_update_alt_rounded,
                              color: color,
                              size: 21,
                            ),
                          ],
                        ),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              failed
                                  ? '升级失败'
                                  : done
                                  ? '升级完成'
                                  : '正在升级 Launcher',
                              style: Theme.of(context).textTheme.titleMedium
                                  ?.copyWith(fontWeight: FontWeight.w700),
                            ),
                            const SizedBox(height: 2),
                            Text(
                              '目标版本 $targetVersion',
                              style: const TextStyle(
                                color: AppColors.textSecondary,
                                fontSize: 12,
                              ),
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 20),
                  _LauncherStageStrip(state: state, failed: failed),
                  const SizedBox(height: 18),
                  Container(
                    padding: const EdgeInsets.all(14),
                    decoration: BoxDecoration(
                      color: failed
                          ? AppColors.statusErrorLight.withValues(alpha: 0.72)
                          : done
                          ? AppColors.statusSuccessLight.withValues(alpha: 0.72)
                          : AppColors.primaryLight.withValues(alpha: 0.72),
                      borderRadius: AppRadius.mdRadius,
                      border: Border.all(color: color.withValues(alpha: 0.18)),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Expanded(
                              child: Text(
                                message,
                                maxLines: 2,
                                overflow: TextOverflow.ellipsis,
                                style: TextStyle(
                                  color: failed
                                      ? AppColors.statusError
                                      : AppColors.textPrimary,
                                  fontSize: 13,
                                  height: 1.45,
                                  fontWeight: FontWeight.w600,
                                ),
                              ),
                            ),
                            const SizedBox(width: 12),
                            AnimatedSwitcher(
                              duration: const Duration(milliseconds: 180),
                              child: Text(
                                '$progressPercent%',
                                key: ValueKey<int>(progressPercent),
                                style: TextStyle(
                                  color: color,
                                  fontSize: 20,
                                  fontWeight: FontWeight.w800,
                                ),
                              ),
                            ),
                          ],
                        ),
                        const SizedBox(height: 12),
                        TweenAnimationBuilder<double>(
                          tween: Tween<double>(begin: 0, end: value),
                          duration: const Duration(milliseconds: 360),
                          curve: Curves.easeOutCubic,
                          builder: (context, animatedValue, _) {
                            return ClipRRect(
                              borderRadius: AppRadius.smRadius,
                              child: LinearProgressIndicator(
                                minHeight: 8,
                                value: animatedValue,
                                color: color,
                                backgroundColor: AppColors.surface.withValues(
                                  alpha: 0.86,
                                ),
                              ),
                            );
                          },
                        ),
                      ],
                    ),
                  ),
                  if (state.lastError.isNotEmpty && failed) ...[
                    const SizedBox(height: 12),
                    Container(
                      width: double.infinity,
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: AppColors.surfaceElevated,
                        borderRadius: AppRadius.mdRadius,
                        border: Border.all(
                          color: AppColors.statusError.withValues(alpha: 0.18),
                        ),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          const Text(
                            '错误详情',
                            style: TextStyle(
                              color: AppColors.statusError,
                              fontSize: 12,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                          const SizedBox(height: 6),
                          SelectableText(
                            state.lastError,
                            maxLines: 4,
                            style: const TextStyle(
                              color: AppColors.textSecondary,
                              fontSize: 12,
                              height: 1.45,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                  if (logs.isNotEmpty) ...[
                    const SizedBox(height: 16),
                    Container(
                      width: double.infinity,
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: AppColors.surfaceElevated,
                        borderRadius: AppRadius.mdRadius,
                        border: Border.all(color: AppColors.border),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          const Text(
                            '最近动态',
                            style: TextStyle(
                              color: AppColors.textSecondary,
                              fontSize: 12,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                          const SizedBox(height: 10),
                          ...logs.map(
                            (item) => Padding(
                              padding: const EdgeInsets.only(bottom: 8),
                              child: Row(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Container(
                                    width: 7,
                                    height: 7,
                                    margin: const EdgeInsets.only(top: 5),
                                    decoration: BoxDecoration(
                                      color: _launcherLogColor(item.stage),
                                      shape: BoxShape.circle,
                                    ),
                                  ),
                                  const SizedBox(width: 9),
                                  Expanded(
                                    child: Text(
                                      item.message,
                                      maxLines: 2,
                                      overflow: TextOverflow.ellipsis,
                                      style: const TextStyle(
                                        color: AppColors.textSecondary,
                                        fontSize: 12,
                                        height: 1.4,
                                      ),
                                    ),
                                  ),
                                ],
                              ),
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                  const SizedBox(height: 18),
                  Row(
                    children: [
                      if (!done && !failed)
                        const Expanded(
                          child: Text(
                            '升级任务仍在设备端执行，正在同步状态',
                            style: TextStyle(
                              color: AppColors.textMuted,
                              fontSize: 12,
                            ),
                          ),
                        )
                      else
                        const Spacer(),
                      TextButton(
                        onPressed: done
                            ? () => Navigator.of(context).pop()
                            : null,
                        child: Text(done ? '完成' : '升级中'),
                      ),
                    ],
                  ),
                ],
              );
            },
          ),
        ),
      ),
    );
  }
}

class _LauncherStageStrip extends StatelessWidget {
  final DeviceLauncherState state;
  final bool failed;

  const _LauncherStageStrip({required this.state, required this.failed});

  @override
  Widget build(BuildContext context) {
    final done = !state.isUpgradeRunning && !failed;
    final activeIndex = _launcherUpgradeStageIndex(state.upgradeStage);
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        for (var index = 0; index < _launcherUpgradeStages.length; index++)
          _LauncherStageChip(
            label: _upgradeStageShortLabel(_launcherUpgradeStages[index]),
            icon: _upgradeStageIcon(_launcherUpgradeStages[index]),
            active: !done && !failed && index == activeIndex,
            completed: done || (!failed && activeIndex > index),
          ),
      ],
    );
  }
}

class _LauncherStageChip extends StatelessWidget {
  final String label;
  final IconData icon;
  final bool active;
  final bool completed;

  const _LauncherStageChip({
    required this.label,
    required this.icon,
    required this.active,
    required this.completed,
  });

  @override
  Widget build(BuildContext context) {
    final color = completed
        ? AppColors.statusSuccess
        : active
        ? AppColors.primary
        : AppColors.textMuted;
    final bg = completed
        ? AppColors.statusSuccessLight
        : active
        ? AppColors.primaryLight
        : AppColors.surfaceElevated;
    return AnimatedContainer(
      duration: const Duration(milliseconds: 220),
      curve: Curves.easeOutCubic,
      height: 32,
      padding: const EdgeInsets.symmetric(horizontal: 10),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: AppRadius.smRadius,
        border: Border.all(
          color: active || completed
              ? color.withValues(alpha: 0.22)
              : AppColors.borderLight,
        ),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(completed ? Icons.check_rounded : icon, size: 14, color: color),
          const SizedBox(width: 5),
          Text(
            label,
            style: TextStyle(
              color: color,
              fontSize: 12,
              fontWeight: active || completed
                  ? FontWeight.w700
                  : FontWeight.w500,
            ),
          ),
        ],
      ),
    );
  }
}

const List<String> _launcherUpgradeStages = [
  'checking_version',
  'downloading',
  'stopping_agents',
  'switching_version',
  'starting_agents',
];

List<DeviceLauncherUpgradeLog> _appendLauncherUpgradeLog(
  List<DeviceLauncherUpgradeLog> logs,
  String stage,
  String message,
) {
  if (logs.isNotEmpty &&
      logs.last.stage == stage &&
      logs.last.message == message) {
    return logs;
  }
  return [
    ...logs,
    DeviceLauncherUpgradeLog(
      time: DateTime.now().toUtc(),
      stage: stage,
      message: message,
    ),
  ];
}

List<DeviceLauncherUpgradeLog> _compactLauncherUpgradeLogs(
  List<DeviceLauncherUpgradeLog> logs,
) {
  final compacted = <DeviceLauncherUpgradeLog>[];
  String? lastKey;
  for (final item in logs) {
    final message = _normalizeLauncherUpgradeLog(item.message);
    if (message.isEmpty) {
      continue;
    }
    final key = '${item.stage}|$message';
    if (key == lastKey) {
      continue;
    }
    lastKey = key;
    compacted.add(
      DeviceLauncherUpgradeLog(
        time: item.time,
        stage: item.stage,
        message: message,
      ),
    );
  }
  if (compacted.length <= 4) {
    return compacted;
  }
  return compacted.sublist(compacted.length - 4);
}

String _normalizeLauncherUpgradeLog(String message) {
  return message
      .replaceAll(RegExp(r'\s+\d{1,3}(?:\.\d+)?%.*$'), '')
      .replaceAll(RegExp(r'（\d+/\d+）'), '')
      .trim();
}

String _launcherUpgradeDisplayMessage(DeviceLauncherState state) {
  final failed =
      state.upgradeStage == 'failed' || state.status.toLowerCase() == 'failed';
  if (failed) {
    final error = state.lastError.isNotEmpty
        ? state.lastError
        : state.upgradeMessage;
    return _friendlyLauncherUpgradeError(error);
  }
  if (state.upgradeMessage.trim().isNotEmpty) {
    return state.upgradeMessage.trim();
  }
  return _upgradeStageLabel(state.upgradeStage);
}

String _friendlyLauncherUpgradeError(String error) {
  final lower = error.toLowerCase();
  if (lower.contains('connection closed before full header') ||
      lower.contains('connection reset') ||
      lower.contains('connection terminated')) {
    return '状态同步连接中断，设备端升级可能仍在继续，请稍后刷新设备状态确认结果';
  }
  if (lower.contains('timeout') || lower.contains('timed out')) {
    return '状态同步超时，已停止等待结果，请刷新设备状态或稍后重试';
  }
  if (error.trim().isEmpty) {
    return '升级失败，请查看错误详情';
  }
  return error.trim();
}

Color _launcherUpgradeStatusColor(DeviceLauncherState state) {
  final failed =
      state.upgradeStage == 'failed' || state.status.toLowerCase() == 'failed';
  if (failed) {
    return AppColors.statusError;
  }
  if (!state.isUpgradeRunning) {
    return AppColors.statusSuccess;
  }
  if (state.upgradeStage == 'poll_retry') {
    return AppColors.statusWarning;
  }
  return AppColors.primary;
}

Color _launcherLogColor(String stage) {
  return switch (stage) {
    'failed' => AppColors.statusError,
    'poll_retry' => AppColors.statusWarning,
    'completed' || 'already_latest' => AppColors.statusSuccess,
    _ => AppColors.primary,
  };
}

int _compareDottedVersion(String current, String target) {
  final currentParts = _parseDottedVersion(current);
  final targetParts = _parseDottedVersion(target);
  if (currentParts.isEmpty || targetParts.isEmpty) {
    return current.trim() == target.trim() ? 0 : -1;
  }
  final length = math.max(currentParts.length, targetParts.length);
  for (var index = 0; index < length; index++) {
    final left = index < currentParts.length ? currentParts[index] : 0;
    final right = index < targetParts.length ? targetParts[index] : 0;
    if (left < right) return -1;
    if (left > right) return 1;
  }
  return 0;
}

List<int> _parseDottedVersion(String version) {
  final trimmed = version.trim();
  if (trimmed.isEmpty) return const [];
  final match = RegExp(r'^\d+(?:\.\d+)*').firstMatch(trimmed);
  if (match == null) return const [];
  return match
      .group(0)!
      .split('.')
      .map((part) => int.tryParse(part) ?? 0)
      .toList(growable: false);
}

int _launcherUpgradeStageIndex(String stage) {
  if (stage == 'completed' || stage == 'already_latest') {
    return _launcherUpgradeStages.length;
  }
  final index = _launcherUpgradeStages.indexOf(stage);
  return index < 0 ? 0 : index;
}

String _upgradeStageShortLabel(String stage) {
  return switch (stage) {
    'checking_version' => '检查',
    'downloading' => '下载',
    'stopping_agents' => '停止',
    'switching_version' => '切换',
    'starting_agents' => '恢复',
    _ => '准备',
  };
}

IconData _upgradeStageIcon(String stage) {
  return switch (stage) {
    'checking_version' => Icons.manage_search_rounded,
    'downloading' => Icons.file_download_rounded,
    'stopping_agents' => Icons.pause_circle_outline_rounded,
    'switching_version' => Icons.swap_horiz_rounded,
    'starting_agents' => Icons.play_circle_outline_rounded,
    _ => Icons.more_horiz_rounded,
  };
}

String _upgradeStageLabel(String stage) {
  return switch (stage) {
    'checking_version' => '正在检查版本',
    'downloading' => '正在下载更新包',
    'stopping_agents' => '正在停止当前服务',
    'switching_version' => '正在切换版本',
    'starting_agents' => '正在恢复服务',
    'completed' => '升级完成',
    'already_latest' => '当前已经是目标版本',
    'failed' => '升级失败',
    _ => '准备升级',
  };
}

class _DirectoryAccessVerifySheet extends StatefulWidget {
  final String machineId;
  const _DirectoryAccessVerifySheet({required this.machineId});

  @override
  State<_DirectoryAccessVerifySheet> createState() =>
      _DirectoryAccessVerifySheetState();
}

class _DirectoryAccessVerifySheetState
    extends State<_DirectoryAccessVerifySheet> {
  final _repo = DeviceRepository();
  final _captchaCtrl = TextEditingController();
  final _emailCodeCtrl = TextEditingController();
  String? _captchaId;
  Uint8List? _captchaImage;
  bool _captchaLoading = false;
  bool _sendingEmail = false;
  bool _saving = false;
  int _cooldown = 0;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadCaptcha();
  }

  @override
  void dispose() {
    _captchaCtrl.dispose();
    _emailCodeCtrl.dispose();
    super.dispose();
  }

  Future<void> _loadCaptcha() async {
    setState(() => _captchaLoading = true);
    try {
      final data = await ApiClient.get('/api/auth/captcha');
      final b64 = data['captcha_image'] as String? ?? '';
      final raw = b64.contains(',') ? b64.split(',').last : b64;
      setState(() {
        _captchaId = data['captcha_id'] as String?;
        _captchaImage = base64Decode(raw);
      });
    } catch (_) {}
    if (mounted) {
      setState(() => _captchaLoading = false);
    }
  }

  Future<void> _sendEmailCode() async {
    if (_captchaCtrl.text.trim().isEmpty || _captchaId == null) {
      setState(() => _error = '请输入图形验证码');
      return;
    }
    setState(() {
      _sendingEmail = true;
      _error = null;
    });
    try {
      await _repo.sendDeviceDirectoryAccessEmailCode(
        machineId: widget.machineId,
        captchaId: _captchaId!,
        captcha: _captchaCtrl.text.trim(),
      );
      setState(() => _cooldown = 60);
      _startCooldown();
      _captchaCtrl.clear();
      await _loadCaptcha();
    } catch (e) {
      setState(() => _error = e.toString());
      _captchaCtrl.clear();
      await _loadCaptcha();
    }
    if (mounted) {
      setState(() => _sendingEmail = false);
    }
  }

  void _startCooldown() {
    Future.doWhile(() async {
      await Future.delayed(const Duration(seconds: 1));
      if (!mounted) return false;
      setState(() => _cooldown--);
      return _cooldown > 0;
    });
  }

  Future<void> _submit() async {
    if (_emailCodeCtrl.text.trim().isEmpty) {
      setState(() => _error = '请输入邮箱验证码');
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await _repo.updateDeviceDirectoryAccess(
        machineId: widget.machineId,
        allowAll: true,
        emailCode: _emailCodeCtrl.text.trim(),
      );
      if (!mounted) return;
      Navigator.of(context).pop(true);
    } catch (e) {
      setState(() => _error = e.toString());
    }
    if (mounted) {
      setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.only(
        bottom: MediaQuery.of(context).viewInsets.bottom,
      ),
      child: PanelCard(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              '开启访问所有目录',
              style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
            ),
            const SizedBox(height: 12),
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                Expanded(
                  child: TextField(
                    controller: _captchaCtrl,
                    decoration: const InputDecoration(labelText: '图形验证码'),
                  ),
                ),
                const SizedBox(width: 10),
                GestureDetector(
                  onTap: _captchaLoading ? null : _loadCaptcha,
                  child: Container(
                    height: 46,
                    width: 130,
                    decoration: BoxDecoration(
                      borderRadius: AppRadius.smRadius,
                      border: Border.all(color: AppColors.border),
                      color: AppColors.inputBackground,
                    ),
                    clipBehavior: Clip.antiAlias,
                    child: _captchaImage != null
                        ? Image.memory(_captchaImage!, fit: BoxFit.cover)
                        : const Center(
                            child: SizedBox(
                              width: 16,
                              height: 16,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            ),
                          ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                Expanded(
                  child: TextField(
                    controller: _emailCodeCtrl,
                    decoration: const InputDecoration(labelText: '邮箱验证码'),
                  ),
                ),
                const SizedBox(width: 10),
                SizedBox(
                  height: 46,
                  width: 110,
                  child: ElevatedButton(
                    onPressed: (_sendingEmail || _cooldown > 0)
                        ? null
                        : _sendEmailCode,
                    child: _sendingEmail
                        ? const SizedBox(
                            width: 16,
                            height: 16,
                            child: CircularProgressIndicator(
                              strokeWidth: 2,
                              color: Colors.white,
                            ),
                          )
                        : Text(_cooldown > 0 ? '${_cooldown}s' : '发送验证码'),
                  ),
                ),
              ],
            ),
            if (_error != null) ...[
              const SizedBox(height: 12),
              Text(
                _error!,
                style: const TextStyle(color: AppColors.statusError),
              ),
            ],
            const SizedBox(height: 16),
            Row(
              children: [
                Expanded(
                  child: AppButton(
                    label: _saving ? '开启中...' : '确认开启',
                    onPressed: _saving ? null : _submit,
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _AgentCard extends ConsumerWidget {
  final AgentModel agent;
  final DeviceModel device;
  const _AgentCard({required this.agent, required this.device});

  String _statusLabel(AgentModel agent) {
    if (agent.isDisabled) return '已停用';
    return switch (agent.normalizedStatus) {
      'starting' => '启动中',
      'restarting' => '重启中',
      'upgrading' => '升级中',
      'running' => agent.isBusy ? '处理中' : '空闲',
      'stopped' => '已停止',
      'failed' => '异常',
      'offline' => '离线',
      _ => agent.isBusy ? '处理中' : '空闲',
    };
  }

  StatusType _statusType(AgentModel agent) {
    if (agent.isDisabled) return StatusType.warning;
    if (agent.isTransitioning || agent.isBusy) return StatusType.processing;
    if (agent.isFailed) return StatusType.error;
    if (!agent.isOnline) return StatusType.offline;
    return StatusType.online;
  }

  Future<void> _showAgentMCPSelectionDialog(
    BuildContext context,
    WidgetRef ref,
  ) async {
    await showDialog<void>(
      context: context,
      builder: (dialogContext) =>
          _AgentMCPSelectionDialog(device: device, agent: agent),
    );
    ref.invalidate(deviceDetailProvider(device.machineId));
  }

  Future<void> _showAgentSkillSelectionDialog(
    BuildContext context,
    WidgetRef ref,
  ) async {
    await showDialog<void>(
      context: context,
      builder: (dialogContext) =>
          _AgentSkillSelectionDialog(device: device, agent: agent),
    );
    ref.invalidate(deviceDetailProvider(device.machineId));
  }

  Future<void> _showAgentSemanticSelectionDialog(
    BuildContext context,
    WidgetRef ref,
  ) async {
    await showDialog<void>(
      context: context,
      builder: (dialogContext) =>
          _AgentSemanticSelectionDialog(device: device, agent: agent),
    );
    ref.invalidate(deviceDetailProvider(device.machineId));
  }

  void _openAgentEnv(BuildContext context) {
    context.push(
      '/devices/${Uri.encodeComponent(device.machineId)}/env-config'
      '?agentId=${Uri.encodeComponent(agent.agentId)}'
      '&projectId=${Uri.encodeComponent(agent.projectId)}',
    );
  }

  void _openChat(BuildContext context) {
    context.push(
      '/chat'
      '?agentId=${Uri.encodeComponent(agent.agentId)}'
      '&projectId=${Uri.encodeComponent(agent.projectId)}'
      '&projectRoot=${Uri.encodeComponent(agent.projectRoot)}'
      '&projectScopeId=${Uri.encodeComponent(agent.projectScopeId)}'
      '&machineId=${Uri.encodeComponent(device.machineId)}',
    );
  }

  String _projectDirectoryName() {
    final normalized = agent.projectRoot.trim().replaceAll('\\', '/');
    final segments = normalized
        .split('/')
        .where((segment) => segment.isNotEmpty)
        .toList(growable: false);
    if (segments.isNotEmpty) return segments.last;
    final project = agent.projectId.trim();
    return project.isEmpty ? 'Agent' : project;
  }

  Future<void> _renameAgent(BuildContext context, WidgetRef ref) async {
    final name = await showAgentRenameDialog(
      context: context,
      initialName: agent.displayName,
      projectDirectoryName: _projectDirectoryName(),
    );
    if (name == null || name == agent.name.trim()) return;

    final operationId = 'agent:rename:${device.machineId}:${agent.agentId}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在更新 Agent 名称',
      message: '正在同步到 ${device.effectiveName}',
      kind: AppNotificationKind.agent,
      scope: AppNotificationScope.synced,
      sourceLabel: device.hostname,
    );
    try {
      await ref
          .read(deviceRepositoryProvider)
          .renameDeviceAgent(
            machineId: device.machineId,
            agentId: agent.agentId,
            name: name,
          );
      notifications.succeed(operationId, title: 'Agent 名称已更新', message: name);
      ref.invalidate(deviceDetailProvider(device.machineId));
      ref.invalidate(deviceListProvider);
    } catch (error) {
      notifications.fail(operationId, title: 'Agent 名称更新失败', error: error);
    }
  }

  Future<void> _restartAgent(BuildContext context, WidgetRef ref) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('重启 Agent'),
        content: Text('确认重启 Agent 吗？\n\n${agent.projectId}'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('确认'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    final operationId = 'agent:restart:${device.machineId}:${agent.agentId}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在重启 Agent',
      message: '正在等待设备确认重启结果',
      kind: AppNotificationKind.agent,
      scope: AppNotificationScope.synced,
      sourceLabel: device.hostname,
    );
    try {
      final repo = ref.read(deviceRepositoryProvider);
      await repo.restartDeviceAgent(
        machineId: device.machineId,
        agentId: agent.agentId,
      );
      notifications.succeed(
        operationId,
        title: 'Agent 重启成功',
        message: '${agent.projectId} 已重新启动',
      );
      if (!context.mounted) return;
      ref.invalidate(deviceDetailProvider(device.machineId));
    } catch (e) {
      notifications.fail(operationId, title: 'Agent 重启失败', error: e);
    }
  }

  Future<void> _removeAgent(BuildContext context, WidgetRef ref) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('注销 Agent'),
        content: Text(
          '确认注销 Agent 吗？\n\n${agent.projectId}\n\n该操作会从设备页移除该 Agent 并清空其配置，但不会删除历史聊天记录。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('确认'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    final operationId = 'agent:remove:${device.machineId}:${agent.agentId}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在注销 Agent',
      message: '正在清理 ${agent.projectId} 的设备配置',
      kind: AppNotificationKind.agent,
      scope: AppNotificationScope.synced,
      sourceLabel: device.hostname,
    );
    try {
      final repo = ref.read(deviceRepositoryProvider);
      await repo.removeDeviceAgent(
        machineId: device.machineId,
        agentId: agent.agentId,
      );
      notifications.succeed(
        operationId,
        title: 'Agent 已注销',
        message: '历史聊天记录已保留',
      );
      if (!context.mounted) return;
      ref.invalidate(deviceDetailProvider(device.machineId));
    } catch (e) {
      notifications.fail(operationId, title: 'Agent 注销失败', error: e);
    }
  }

  List<Widget> _agentActions(
    BuildContext context,
    WidgetRef ref,
    bool disabled,
  ) {
    return [
      _AgentActionButton(
        tooltip: '语义 Agent',
        icon: const _SemanticAgentGlyphIcon(),
        color: AppColors.primary,
        backgroundColor: AppColors.primaryLight,
        onPressed: () => _showAgentSemanticSelectionDialog(context, ref),
      ),
      _AgentActionButton(
        tooltip: 'Agent MCP',
        icon: const _AgentGlyphIcon(),
        color: AppColors.primary,
        backgroundColor: AppColors.primaryLight,
        onPressed: () => _showAgentMCPSelectionDialog(context, ref),
      ),
      _AgentActionButton(
        tooltip: 'Agent Skill',
        icon: const Icon(Icons.auto_awesome_outlined),
        color: AppColors.primary,
        backgroundColor: AppColors.primaryLight,
        onPressed: () => _showAgentSkillSelectionDialog(context, ref),
      ),
      _AgentActionButton(
        tooltip: 'Agent 环境变量',
        icon: const Icon(Icons.terminal_rounded),
        color: AppColors.primary,
        backgroundColor: AppColors.primaryLight,
        onPressed: disabled ? null : () => _openAgentEnv(context),
      ),
      _AgentActionButton(
        tooltip: '重启 Agent',
        icon: const Icon(Icons.restart_alt_rounded),
        color: AppColors.primary,
        backgroundColor: AppColors.primaryLight,
        onPressed: disabled ? null : () => _restartAgent(context, ref),
      ),
      _AgentActionButton(
        tooltip: '注销 Agent',
        danger: true,
        icon: const Icon(Icons.delete_outline_rounded),
        color: AppColors.primary,
        backgroundColor: AppColors.primaryLight,
        onPressed: () => _removeAgent(context, ref),
      ),
    ];
  }

  List<Widget> _spacedActions(List<Widget> actions) {
    return [
      for (var index = 0; index < actions.length; index++) ...[
        if (index > 0) const SizedBox(width: 8),
        actions[index],
      ],
    ];
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final disabled = agent.isDisabled;
    final actions = _agentActions(context, ref, disabled);
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
                    Row(
                      children: [
                        Flexible(
                          child: Text(
                            agent.displayName,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: Theme.of(context).textTheme.titleMedium,
                          ),
                        ),
                        const SizedBox(width: 2),
                        IconButton(
                          tooltip: '修改 Agent 名称',
                          onPressed: () => _renameAgent(context, ref),
                          icon: const Icon(Icons.edit_outlined, size: 17),
                          visualDensity: VisualDensity.compact,
                          constraints: const BoxConstraints.tightFor(
                            width: 34,
                            height: 34,
                          ),
                        ),
                      ],
                    ),
                    Text(
                      agent.agentId,
                      style: Theme.of(
                        context,
                      ).textTheme.labelSmall?.copyWith(fontFamily: 'monospace'),
                    ),
                  ],
                ),
              ),
              Column(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  StatusPill(
                    label: _statusLabel(agent),
                    type: _statusType(agent),
                  ),
                  const SizedBox(height: 6),
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Text(
                        '启用',
                        style: TextStyle(
                          fontSize: 12,
                          color: AppColors.textMuted,
                        ),
                      ),
                      Switch.adaptive(
                        value: agent.enabled,
                        onChanged: (value) async {
                          final operationId =
                              'agent:enabled:${device.machineId}:${agent.agentId}';
                          final notifications = ref.read(
                            appNotificationControllerProvider.notifier,
                          );
                          notifications.start(
                            operationId: operationId,
                            title: value ? '正在启用 Agent' : '正在停用 Agent',
                            message: '正在同步 ${agent.projectId} 的运行状态',
                            kind: AppNotificationKind.agent,
                            scope: AppNotificationScope.synced,
                            sourceLabel: device.hostname,
                          );
                          try {
                            final repo = ref.read(deviceRepositoryProvider);
                            await repo.setDeviceAgentEnabled(
                              machineId: device.machineId,
                              agentId: agent.agentId,
                              enabled: value,
                            );
                            notifications.succeed(
                              operationId,
                              title: value ? 'Agent 已启用' : 'Agent 已停用',
                              message: '${agent.projectId} 状态已同步',
                            );
                            if (!context.mounted) return;
                            ref.invalidate(
                              deviceDetailProvider(device.machineId),
                            );
                            ref.invalidate(deviceListProvider);
                          } catch (e) {
                            notifications.fail(
                              operationId,
                              title: value ? 'Agent 启用失败' : 'Agent 停用失败',
                              error: e,
                            );
                          }
                        },
                      ),
                    ],
                  ),
                ],
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
                  value: agent.projectRoot.isEmpty ? '-' : agent.projectRoot,
                  mono: true,
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          LayoutBuilder(
            builder: (context, constraints) {
              final semanticName = agent.semanticAgentName.isEmpty
                  ? '编码助手'
                  : agent.semanticAgentName;
              final blocks = <Widget>[
                InfoBlock(label: '语义 Agent', value: semanticName),
                InfoBlock(
                  label: '版本',
                  value: agent.version.isEmpty ? '-' : agent.version,
                ),
                if (agent.isBusy)
                  InfoBlock(
                    label: '当前任务',
                    value: agent.runningTaskId ?? '-',
                    mono: true,
                  ),
              ];
              if (constraints.maxWidth < 420) {
                return Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    for (var index = 0; index < blocks.length; index++) ...[
                      if (index > 0) const SizedBox(height: 10),
                      blocks[index],
                    ],
                  ],
                );
              }
              return Row(
                children: [
                  for (var index = 0; index < blocks.length; index++) ...[
                    if (index > 0) const SizedBox(width: 12),
                    Expanded(child: blocks[index]),
                  ],
                ],
              );
            },
          ),
          const SizedBox(height: 16),
          LayoutBuilder(
            builder: (context, constraints) {
              final compact = constraints.maxWidth < 520;
              final actionBar = SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                clipBehavior: Clip.none,
                child: Row(children: _spacedActions(actions)),
              );
              final chatButton = AppButton(
                label: '进入对话',
                icon: Icons.chat_bubble_outline,
                width: compact ? double.infinity : 156,
                onPressed: disabled ? null : () => _openChat(context),
              );
              if (compact) {
                return Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [actionBar, const SizedBox(height: 12), chatButton],
                );
              }
              return Row(
                children: [
                  Expanded(child: actionBar),
                  const SizedBox(width: 12),
                  chatButton,
                ],
              );
            },
          ),
        ],
      ),
    );
  }
}

class _AgentActionButton extends StatelessWidget {
  final String tooltip;
  final Widget icon;
  final VoidCallback? onPressed;
  final bool danger;
  final Color? color;
  final Color? backgroundColor;

  const _AgentActionButton({
    required this.tooltip,
    required this.icon,
    required this.onPressed,
    this.danger = false,
    this.color,
    this.backgroundColor,
  });

  @override
  Widget build(BuildContext context) {
    final enabled = onPressed != null;
    final activeColor =
        color ?? (danger ? AppColors.statusError : AppColors.primary);
    final activeBackground =
        backgroundColor ??
        (danger ? AppColors.statusErrorLight : AppColors.primaryLight);
    final iconColor = !enabled ? AppColors.textMuted : activeColor;
    final buttonBackground = !enabled
        ? AppColors.statusOfflineLight
        : activeBackground;
    return Tooltip(
      message: tooltip,
      child: Material(
        color: buttonBackground,
        borderRadius: AppRadius.mdRadius,
        child: InkWell(
          onTap: onPressed,
          borderRadius: AppRadius.mdRadius,
          child: Container(
            width: 44,
            height: 44,
            decoration: BoxDecoration(
              borderRadius: AppRadius.mdRadius,
              border: Border.all(
                color: enabled ? AppColors.borderLight : AppColors.border,
              ),
            ),
            alignment: Alignment.center,
            child: IconTheme.merge(
              data: IconThemeData(color: iconColor, size: 20),
              child: icon,
            ),
          ),
        ),
      ),
    );
  }
}

class _AgentGlyphIcon extends StatelessWidget {
  final double size;
  final Color? color;

  const _AgentGlyphIcon({this.size = 22, this.color});

  @override
  Widget build(BuildContext context) {
    final paintColor =
        color ?? IconTheme.of(context).color ?? AppColors.primary;
    return SizedBox(
      width: size,
      height: size,
      child: CustomPaint(painter: _AgentGlyphPainter(color: paintColor)),
    );
  }
}

class _AgentGlyphPainter extends CustomPainter {
  final Color color;

  const _AgentGlyphPainter({required this.color});

  @override
  void paint(Canvas canvas, Size size) {
    final scale = size.shortestSide / 22;
    Offset p(double x, double y) => Offset(x * scale, y * scale);
    final linePaint = Paint()
      ..color = color.withValues(alpha: 0.35)
      ..strokeWidth = 2.1 * scale
      ..strokeCap = StrokeCap.round
      ..style = PaintingStyle.stroke;
    final nodePaint = Paint()
      ..color = color
      ..style = PaintingStyle.fill;
    final centerPaint = Paint()
      ..color = AppColors.surface
      ..style = PaintingStyle.fill;

    final center = p(11, 11);
    final nodes = [p(5.2, 5.5), p(16.8, 6.4), p(5.7, 16.7), p(17.1, 16)];
    for (final node in nodes) {
      canvas.drawLine(center, node, linePaint);
    }
    canvas.drawRRect(
      RRect.fromRectAndRadius(
        Rect.fromCenter(
          center: center,
          width: 8.7 * scale,
          height: 8.7 * scale,
        ),
        Radius.circular(2.6 * scale),
      ),
      nodePaint,
    );
    canvas.drawCircle(center, 2.1 * scale, centerPaint);
    for (final node in nodes) {
      canvas.drawCircle(node, 3.1 * scale, nodePaint);
      canvas.drawCircle(node, 1.15 * scale, centerPaint);
    }
  }

  @override
  bool shouldRepaint(covariant _AgentGlyphPainter oldDelegate) {
    return oldDelegate.color != color;
  }
}

class _SemanticAgentGlyphIcon extends StatelessWidget {
  final double size;
  final Color? color;

  const _SemanticAgentGlyphIcon({this.size = 22, this.color});

  @override
  Widget build(BuildContext context) {
    final paintColor =
        color ?? IconTheme.of(context).color ?? AppColors.primary;
    return SizedBox(
      width: size,
      height: size,
      child: CustomPaint(
        painter: _SemanticAgentGlyphPainter(color: paintColor),
      ),
    );
  }
}

class _SemanticAgentGlyphPainter extends CustomPainter {
  final Color color;

  const _SemanticAgentGlyphPainter({required this.color});

  @override
  void paint(Canvas canvas, Size size) {
    final scale = size.shortestSide / 22;
    Offset p(double x, double y) => Offset(x * scale, y * scale);
    final stroke = Paint()
      ..color = color
      ..strokeWidth = 1.9 * scale
      ..strokeCap = StrokeCap.round
      ..strokeJoin = StrokeJoin.round
      ..style = PaintingStyle.stroke;
    final softStroke = Paint()
      ..color = color.withValues(alpha: 0.32)
      ..strokeWidth = 1.7 * scale
      ..strokeCap = StrokeCap.round
      ..style = PaintingStyle.stroke;
    final fill = Paint()
      ..color = color
      ..style = PaintingStyle.fill;
    final cutout = Paint()
      ..color = AppColors.surface
      ..style = PaintingStyle.fill;

    final head = RRect.fromRectAndRadius(
      Rect.fromLTWH(5.2 * scale, 3.4 * scale, 11.6 * scale, 10.2 * scale),
      Radius.circular(3.6 * scale),
    );
    canvas.drawRRect(head, stroke);
    canvas.drawCircle(p(8.7, 8.1), 1.05 * scale, fill);
    canvas.drawCircle(p(13.3, 8.1), 1.05 * scale, fill);
    canvas.drawLine(p(9.2, 11.2), p(12.8, 11.2), softStroke);
    canvas.drawLine(p(11, 3.4), p(11, 1.9), softStroke);
    canvas.drawCircle(p(11, 1.7), 1.2 * scale, fill);

    final badge = RRect.fromRectAndRadius(
      Rect.fromLTWH(6.1 * scale, 15 * scale, 9.8 * scale, 4.6 * scale),
      Radius.circular(2.3 * scale),
    );
    canvas.drawRRect(badge, fill);
    canvas.drawCircle(p(8.6, 17.3), 0.9 * scale, cutout);
    canvas.drawLine(
      p(11, 17.3),
      p(14, 17.3),
      softStroke..color = AppColors.surface,
    );
  }

  @override
  bool shouldRepaint(covariant _SemanticAgentGlyphPainter oldDelegate) {
    return oldDelegate.color != color;
  }
}

class _AgentSemanticSelectionDialog extends ConsumerStatefulWidget {
  final DeviceModel device;
  final AgentModel agent;

  const _AgentSemanticSelectionDialog({
    required this.device,
    required this.agent,
  });

  @override
  ConsumerState<_AgentSemanticSelectionDialog> createState() =>
      _AgentSemanticSelectionDialogState();
}

class _AgentSemanticSelectionDialogState
    extends ConsumerState<_AgentSemanticSelectionDialog> {
  bool _loading = true;
  bool _saving = false;
  String _error = '';
  String _selectedId = '';
  List<DeviceSemanticAgentProfile> _profiles = const [];

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final selection = await repo.getDeviceAgentSemanticSelection(
        machineId: widget.device.machineId,
        agentId: widget.agent.agentId,
      );
      if (!mounted) return;
      setState(() {
        _selectedId = selection.semanticAgentId.isNotEmpty
            ? selection.semanticAgentId
            : widget.agent.semanticAgentId;
        if (_selectedId.isEmpty) {
          _selectedId = 'coding-assistant';
        }
        _profiles = selection.availableAgents;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  Future<void> _save() async {
    if (_saving || _selectedId.isEmpty) return;
    final repo = ref.read(deviceRepositoryProvider);
    final profile = _profiles
        .where((item) => item.id == _selectedId)
        .firstOrNull;
    final usesVerifyMcp = profile?.usesVerifyMcp == true;
    var disableVerifyMcp = false;
    if (usesVerifyMcp) {
      try {
        final resolution = await _ensureVerifyToken(
          context: context,
          repository: repo,
          machineId: widget.device.machineId,
          agentId: widget.agent.agentId,
          requireProtectToken: _requiresVerifyProtectToken(_selectedId),
        );
        if (!mounted || resolution == _VerifyTokenResolution.cancelled) return;
        disableVerifyMcp = resolution == _VerifyTokenResolution.disabled;
      } catch (e) {
        if (!mounted) return;
        setState(() => _error = e.toString());
        return;
      }
    }
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    var operationId = '';
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      final job = await repo.startDeviceAgentSemanticPreflight(
        machineId: widget.device.machineId,
        agentId: widget.agent.agentId,
        semanticAgentId: _selectedId,
        applyRecommendedMcp: true,
        disableVerifyMcp: disableVerifyMcp,
        autoRepair: true,
        restartAfterApply: true,
      );
      operationId = _semanticPreflightOperationId(
        widget.device.machineId,
        widget.agent.agentId,
        job.id,
      );
      notifications.start(
        operationId: operationId,
        title: '正在切换 Agent 助手',
        message: '正在创建运行时与 MCP 预检任务',
        progressMode: AppNotificationProgressMode.determinate,
        displayStyle: AppNotificationDisplayStyle.stages,
        progress: 0,
        kind: AppNotificationKind.mcp,
        scope: AppNotificationScope.synced,
        sourceLabel: widget.device.hostname,
        metadata: {
          'machine_id': widget.device.machineId,
          'agent_id': widget.agent.agentId,
        },
        stages: const [
          AppNotificationStage(label: '运行时', active: true),
          AppNotificationStage(label: 'MCP'),
          AppNotificationStage(label: 'Skill'),
          AppNotificationStage(label: '应用'),
        ],
        actions: [
          AppNotificationAction(
            label: '查看设备',
            type: AppNotificationActionType.openRoute,
            payload: {'route': '/devices/${widget.device.machineId}'},
            primary: true,
          ),
        ],
      );
      attachSemanticPreflightMetadata(
        notifications: notifications,
        operationId: operationId,
        machineId: widget.device.machineId,
        agentId: widget.agent.agentId,
        jobId: job.id,
      );
      _syncSemanticPreflightNotification(notifications, operationId, job);
      if (!mounted) return;
      final completed = await showDialog<bool>(
        context: context,
        barrierDismissible: false,
        builder: (_) => _AgentSemanticPreflightDialog(
          device: widget.device,
          agent: widget.agent,
          initialJob: job,
          operationId: operationId,
          disableVerifyMcp: disableVerifyMcp,
        ),
      );
      if (!mounted) return;
      ref.invalidate(deviceDetailProvider(widget.device.machineId));
      ref.invalidate(deviceListProvider);
      if (completed == true) {
        Navigator.of(context).pop();
      }
    } catch (e) {
      if (operationId.isEmpty) {
        operationId = _semanticPreflightOperationId(
          widget.device.machineId,
          widget.agent.agentId,
          'request-${DateTime.now().microsecondsSinceEpoch}',
        );
        notifications.start(
          operationId: operationId,
          title: '正在切换 Agent 助手',
          message: '预检任务创建失败',
          kind: AppNotificationKind.mcp,
          scope: AppNotificationScope.synced,
        );
      }
      notifications.fail(operationId, title: 'Agent 助手切换失败', error: e);
      if (!mounted) return;
      setState(() {
        _error = e.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _saving = false;
        });
      }
    }
  }

  Widget _profileTile(DeviceSemanticAgentProfile profile) {
    final selected = profile.id == _selectedId;
    return Material(
      color: selected ? AppColors.primaryLight : AppColors.surfaceElevated,
      borderRadius: AppRadius.mdRadius,
      child: InkWell(
        onTap: _saving
            ? null
            : () => setState(() {
                _selectedId = profile.id;
              }),
        borderRadius: AppRadius.mdRadius,
        child: Container(
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            borderRadius: AppRadius.mdRadius,
            border: Border.all(
              color: selected ? AppColors.primaryMuted : AppColors.borderLight,
            ),
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Radio<String>(
                value: profile.id,
                groupValue: _selectedId,
                onChanged: _saving
                    ? null
                    : (value) => setState(() {
                        _selectedId = value ?? profile.id;
                      }),
              ),
              const SizedBox(width: 4),
              Container(
                width: 38,
                height: 38,
                decoration: const BoxDecoration(
                  color: AppColors.surface,
                  borderRadius: AppRadius.smRadius,
                ),
                child: Center(
                  child: profile.id == 'reverse-expert'
                      ? const _SemanticAgentGlyphIcon(
                          size: 22,
                          color: AppColors.primary,
                        )
                      : const Icon(
                          Icons.code_rounded,
                          size: 21,
                          color: AppColors.primary,
                        ),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      profile.name,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    if (profile.description.isNotEmpty) ...[
                      const SizedBox(height: 3),
                      Text(
                        profile.description,
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                    ],
                    if (profile.skills.isNotEmpty ||
                        profile.recommendedMcpServers.isNotEmpty ||
                        profile.runtimeRequirements.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      Wrap(
                        spacing: 6,
                        runSpacing: 6,
                        children: [
                          if (profile.skills.isNotEmpty)
                            _SemanticMetaChip(
                              label: 'Skill ${profile.skills.length}',
                            ),
                          if (profile.recommendedMcpServers.isNotEmpty)
                            _SemanticMetaChip(
                              label:
                                  'MCP ${profile.recommendedMcpServers.length}',
                            ),
                          if (profile.runtimeRequirements.isNotEmpty)
                            _SemanticMetaChip(
                              label: '环境 ${profile.runtimeRequirements.length}',
                            ),
                        ],
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final dialogWidth = math.min(
      math.max(MediaQuery.sizeOf(context).width - 40, 0.0),
      540.0,
    );
    final listHeight = math.min(
      math.max(MediaQuery.sizeOf(context).height * 0.36, 220.0),
      360.0,
    );
    final maxDialogHeight = math.max(
      360.0,
      MediaQuery.sizeOf(context).height - 48,
    );
    final profileContentHeight = _profiles.isEmpty
        ? 128.0
        : math.min(
            listHeight,
            _profiles.length * 124.0 + math.max(0, _profiles.length - 1) * 10.0,
          );
    final dialogHeight = math.min(
      maxDialogHeight,
      math.max(
        420.0,
        234.0 + profileContentHeight + (_error.isNotEmpty ? 58.0 : 0.0),
      ),
    );
    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
      backgroundColor: AppColors.surface,
      shape: const RoundedRectangleBorder(borderRadius: AppRadius.lgRadius),
      child: SizedBox(
        width: dialogWidth,
        height: dialogHeight,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 18, 20, 18),
          child: _loading
              ? const Padding(
                  padding: EdgeInsets.symmetric(vertical: 8),
                  child: DialogContentSkeleton(itemCount: 3),
                )
              : Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Container(
                          width: 40,
                          height: 40,
                          decoration: const BoxDecoration(
                            color: AppColors.primaryLight,
                            borderRadius: AppRadius.mdRadius,
                          ),
                          child: const Center(
                            child: _SemanticAgentGlyphIcon(
                              size: 24,
                              color: AppColors.primary,
                            ),
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                '语义 Agent',
                                style: Theme.of(context).textTheme.titleLarge,
                              ),
                              const SizedBox(height: 2),
                              Text(
                                widget.agent.projectId,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: Theme.of(context).textTheme.labelSmall
                                    ?.copyWith(fontFamily: 'monospace'),
                              ),
                            ],
                          ),
                        ),
                        IconButton(
                          tooltip: '关闭',
                          onPressed: _saving
                              ? null
                              : () => Navigator.of(context).pop(),
                          icon: const Icon(Icons.close_rounded),
                        ),
                      ],
                    ),
                    const SizedBox(height: 16),
                    if (_profiles.isEmpty)
                      const _SemanticAgentEmptyState()
                    else
                      ConstrainedBox(
                        constraints: BoxConstraints(maxHeight: listHeight),
                        child: ListView.separated(
                          shrinkWrap: true,
                          itemCount: _profiles.length,
                          separatorBuilder: (_, _) =>
                              const SizedBox(height: 10),
                          itemBuilder: (context, index) =>
                              _profileTile(_profiles[index]),
                        ),
                      ),
                    if (_error.isNotEmpty) ...[
                      const SizedBox(height: 12),
                      Container(
                        width: double.infinity,
                        padding: const EdgeInsets.all(10),
                        decoration: const BoxDecoration(
                          color: AppColors.statusErrorLight,
                          borderRadius: AppRadius.smRadius,
                        ),
                        child: Text(
                          _error,
                          style: const TextStyle(
                            color: AppColors.statusError,
                            fontSize: 13,
                          ),
                        ),
                      ),
                    ],
                    const SizedBox(height: 16),
                    const Divider(),
                    const SizedBox(height: 10),
                    Row(
                      mainAxisAlignment: MainAxisAlignment.end,
                      children: [
                        TextButton(
                          onPressed: _saving
                              ? null
                              : () => Navigator.of(context).pop(),
                          child: const Text('取消'),
                        ),
                      ],
                    ),
                    const SizedBox(height: 8),
                    SizedBox(
                      width: double.infinity,
                      height: 46,
                      child: ElevatedButton(
                        onPressed:
                            _loading ||
                                _saving ||
                                widget.agent.isDisabled ||
                                _profiles.isEmpty
                            ? null
                            : _save,
                        child: Text(_saving ? '验证中...' : '应用并重启'),
                      ),
                    ),
                  ],
                ),
        ),
      ),
    );
  }
}

class _SemanticMetaChip extends StatelessWidget {
  final String label;

  const _SemanticMetaChip({required this.label});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
      decoration: const BoxDecoration(
        color: AppColors.surface,
        borderRadius: AppRadius.smRadius,
      ),
      child: Text(
        label,
        style: Theme.of(context).textTheme.labelSmall?.copyWith(
          color: AppColors.primary,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }
}

class _AgentSemanticPreflightDialog extends ConsumerStatefulWidget {
  final DeviceModel device;
  final AgentModel agent;
  final RuntimePreflightJob initialJob;
  final String operationId;
  final bool disableVerifyMcp;

  const _AgentSemanticPreflightDialog({
    required this.device,
    required this.agent,
    required this.initialJob,
    required this.operationId,
    this.disableVerifyMcp = false,
  });

  @override
  ConsumerState<_AgentSemanticPreflightDialog> createState() =>
      _AgentSemanticPreflightDialogState();
}

class _AgentSemanticPreflightDialogState
    extends ConsumerState<_AgentSemanticPreflightDialog> {
  Timer? _timer;
  RuntimePreflightJob? _job;
  final List<RuntimePreflightEvent> _events = [];
  final Set<String> _presentedUserActionIds = <String>{};
  int _lastSequence = 0;
  String _pollError = '';
  bool _userActionDialogShowing = false;
  bool _polling = false;
  bool _handlingVerifyToken = false;
  late bool _disableVerifyMcp;

  @override
  void initState() {
    super.initState();
    _disableVerifyMcp = widget.disableVerifyMcp;
    _job = widget.initialJob;
    _syncSemanticPreflightNotification(
      ref.read(appNotificationControllerProvider.notifier),
      widget.operationId,
      widget.initialJob,
    );
    _events.addAll(widget.initialJob.latestEvents);
    _lastSequence = _events.fold<int>(
      0,
      (value, event) => math.max(value, event.sequence),
    );
    _startPolling();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      unawaited(_maybePresentUserAction(widget.initialJob));
    });
    unawaited(_poll());
  }

  @override
  void dispose() {
    _stopPolling();
    if (_job?.finished != true) {
      ref
          .read(appNotificationControllerProvider.notifier)
          .waitForSync(widget.operationId, message: '预检仍在设备端执行，等待状态同步');
    }
    super.dispose();
  }

  void _startPolling() {
    _timer?.cancel();
    _timer = Timer.periodic(const Duration(milliseconds: 900), (_) {
      unawaited(_poll());
    });
  }

  void _stopPolling() {
    _timer?.cancel();
    _timer = null;
  }

  Future<void> _poll() async {
    if (_polling) return;
    final job = _job;
    if (job == null || job.finished) {
      _stopPolling();
      return;
    }
    _polling = true;
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final latest = await repo.getDeviceAgentSemanticPreflightJob(
        machineId: widget.device.machineId,
        jobId: job.id,
      );
      final events = await repo.getDeviceAgentSemanticPreflightEvents(
        machineId: widget.device.machineId,
        jobId: job.id,
        afterSequence: _lastSequence,
      );
      if (!mounted) return;
      setState(() {
        _job = latest;
        _pollError = '';
        for (final event in events) {
          _events.add(event);
          _lastSequence = math.max(_lastSequence, event.sequence);
        }
        if (_events.length > 80) {
          _events.removeRange(0, _events.length - 80);
        }
      });
      if (await _maybeRecoverMissingVerifyToken(latest)) {
        return;
      }
      if (latest.finished) {
        _stopPolling();
      }
      _syncSemanticPreflightNotification(
        ref.read(appNotificationControllerProvider.notifier),
        widget.operationId,
        latest,
        events: _events,
      );
      unawaited(_maybePresentUserAction(latest));
    } catch (e) {
      ref
          .read(appNotificationControllerProvider.notifier)
          .waitForSync(widget.operationId, message: '预检状态同步中断，正在重试');
      if (!mounted) return;
      setState(() {
        _pollError = e.toString();
      });
    } finally {
      _polling = false;
    }
  }

  Future<bool> _maybeRecoverMissingVerifyToken(RuntimePreflightJob job) async {
    if (_disableVerifyMcp ||
        !job.missingVerifyTokenFailure ||
        _handlingVerifyToken ||
        !mounted) {
      return false;
    }
    _handlingVerifyToken = true;
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    final operationId = widget.operationId;
    notifications.update(
      operationId: operationId,
      status: AppNotificationStatus.running,
      message: '需要配置 Verify Token 后继续验证',
      attention: AppNotificationAttention.userAction,
    );
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final resolution = await _ensureVerifyToken(
        context: context,
        repository: repo,
        machineId: widget.device.machineId,
        agentId: widget.agent.agentId,
        requireProtectToken: _requiresVerifyProtectToken(job.semanticAgentId),
        forcePrompt: true,
      );
      if (!mounted || resolution == _VerifyTokenResolution.cancelled) {
        _syncSemanticPreflightNotification(notifications, operationId, job);
        return false;
      }
      final disabled = resolution == _VerifyTokenResolution.disabled;
      if (disabled) {
        _disableVerifyMcp = true;
      }
      notifications.start(
        operationId: operationId,
        title: '正在切换 Agent 助手',
        message: disabled
            ? '已关闭当前 Agent 的 Verify MCP，正在重新验证'
            : 'Verify Token 已保存，正在重新验证',
        progressMode: AppNotificationProgressMode.determinate,
        displayStyle: AppNotificationDisplayStyle.stages,
        progress: 0,
        kind: AppNotificationKind.mcp,
        scope: AppNotificationScope.synced,
        attention: AppNotificationAttention.none,
        sourceLabel: widget.device.hostname,
        metadata: {
          'machine_id': widget.device.machineId,
          'agent_id': widget.agent.agentId,
        },
        stages: const [
          AppNotificationStage(label: '运行时', active: true),
          AppNotificationStage(label: 'MCP'),
          AppNotificationStage(label: 'Skill'),
          AppNotificationStage(label: '应用'),
        ],
        actions: [
          AppNotificationAction(
            label: '查看设备',
            type: AppNotificationActionType.openRoute,
            payload: {'route': '/devices/${widget.device.machineId}'},
            primary: true,
          ),
        ],
      );
      final restarted = await repo.startDeviceAgentSemanticPreflight(
        machineId: widget.device.machineId,
        agentId: widget.agent.agentId,
        semanticAgentId: job.semanticAgentId,
        applyRecommendedMcp: true,
        disableVerifyMcp: _disableVerifyMcp,
        autoRepair: true,
        restartAfterApply: true,
      );
      attachSemanticPreflightMetadata(
        notifications: notifications,
        operationId: operationId,
        machineId: widget.device.machineId,
        agentId: widget.agent.agentId,
        jobId: restarted.id,
      );
      if (!mounted) return true;
      setState(() {
        _job = restarted;
        _events
          ..clear()
          ..addAll(restarted.latestEvents);
        _lastSequence = _events.fold<int>(
          0,
          (value, event) => math.max(value, event.sequence),
        );
        _pollError = '';
      });
      _presentedUserActionIds.clear();
      _syncSemanticPreflightNotification(notifications, operationId, restarted);
      _startPolling();
      return true;
    } catch (e) {
      notifications.fail(operationId, title: 'Verify Token 保存失败', error: e);
      if (mounted) setState(() => _pollError = e.toString());
      return true;
    } finally {
      _handlingVerifyToken = false;
    }
  }

  void _close() {
    final job = _job;
    Navigator.of(context).pop(job?.completed == true);
  }

  Future<void> _maybePresentUserAction(RuntimePreflightJob job) async {
    final action = job.userAction;
    if (!mounted ||
        !job.waitingForUserAction ||
        action == null ||
        _userActionDialogShowing ||
        _presentedUserActionIds.contains(action.id)) {
      return;
    }
    _presentedUserActionIds.add(action.id);
    _userActionDialogShowing = true;
    try {
      await showDialog<void>(
        context: context,
        barrierDismissible: false,
        builder: (dialogContext) => _RuntimeUserActionDialog(
          deviceName: widget.device.hostname.isEmpty
              ? widget.device.machineId
              : widget.device.hostname,
          action: action,
        ),
      );
    } finally {
      _userActionDialogShowing = false;
    }
  }

  @override
  Widget build(BuildContext context) {
    final job = _job ?? widget.initialJob;
    final dialogWidth = math.min(
      math.max(MediaQuery.sizeOf(context).width - 36, 0.0),
      560.0,
    );
    final maxDialogHeight = math.max(
      MediaQuery.sizeOf(context).height - 48,
      320.0,
    );
    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 18, vertical: 24),
      backgroundColor: AppColors.surface,
      shape: const RoundedRectangleBorder(borderRadius: AppRadius.lgRadius),
      child: SizedBox(
        width: dialogWidth,
        height: math.min(maxDialogHeight, 720.0),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(18, 18, 18, 16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Container(
                    width: 40,
                    height: 40,
                    decoration: const BoxDecoration(
                      color: AppColors.primaryLight,
                      borderRadius: AppRadius.mdRadius,
                    ),
                    child: const Center(
                      child: _SemanticAgentGlyphIcon(
                        size: 23,
                        color: AppColors.primary,
                      ),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '切换检测',
                          style: Theme.of(context).textTheme.titleLarge,
                        ),
                        const SizedBox(height: 2),
                        Text(
                          widget.agent.projectId.isEmpty
                              ? widget.agent.agentId
                              : widget.agent.projectId,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: Theme.of(context).textTheme.labelSmall,
                        ),
                      ],
                    ),
                  ),
                  IconButton(
                    tooltip: '关闭并转入后台',
                    onPressed: _close,
                    icon: const Icon(Icons.close_rounded),
                  ),
                ],
              ),
              const SizedBox(height: 16),
              ClipRRect(
                borderRadius: AppRadius.smRadius,
                child: LinearProgressIndicator(
                  minHeight: 8,
                  value:
                      (runtimePreflightDisplayProgress(
                        job,
                        supplementalEvents: _events,
                      ).clamp(0, 100)) /
                      100,
                  backgroundColor: AppColors.surfaceElevated,
                  color: job.failed ? AppColors.statusError : AppColors.primary,
                ),
              ),
              const SizedBox(height: 8),
              Row(
                children: [
                  StatusPill(
                    label: _preflightStatusText(job.status),
                    type: job.failed
                        ? StatusType.error
                        : job.completed
                        ? StatusType.online
                        : StatusType.warning,
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Text(
                      '${runtimePreflightDisplayProgress(job, supplementalEvents: _events).clamp(0, 100)}% · ${_preflightStepText(job.currentStep)}',
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 14),
              Expanded(
                child: ListView(
                  children: [
                    if (job.waitingForUserAction && job.userAction != null) ...[
                      _RuntimeUserActionBanner(
                        deviceName: widget.device.hostname.isEmpty
                            ? widget.device.machineId
                            : widget.device.hostname,
                        action: job.userAction!,
                      ),
                      const SizedBox(height: 12),
                    ],
                    ConstrainedBox(
                      constraints: const BoxConstraints(maxHeight: 240),
                      child: ListView.separated(
                        shrinkWrap: true,
                        itemCount: job.items.length,
                        separatorBuilder: (_, _) => const SizedBox(height: 8),
                        itemBuilder: (context, index) {
                          final item = job.items[index];
                          return _PreflightItemTile(
                            item: item,
                            latestEvent: _latestEventForItem(item),
                          );
                        },
                      ),
                    ),
                    if (_events.isNotEmpty) ...[
                      const SizedBox(height: 14),
                      Text(
                        '实时记录',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(height: 8),
                      ConstrainedBox(
                        constraints: const BoxConstraints(maxHeight: 170),
                        child: ListView.separated(
                          shrinkWrap: true,
                          itemCount: _events.length,
                          separatorBuilder: (_, _) => const SizedBox(height: 6),
                          itemBuilder: (context, index) =>
                              _PreflightEventRow(event: _events[index]),
                        ),
                      ),
                    ],
                    if (job.error.isNotEmpty || _pollError.isNotEmpty) ...[
                      const SizedBox(height: 12),
                      ConstrainedBox(
                        constraints: const BoxConstraints(maxHeight: 190),
                        child: Container(
                          width: double.infinity,
                          padding: const EdgeInsets.all(10),
                          decoration: const BoxDecoration(
                            color: AppColors.statusErrorLight,
                            borderRadius: AppRadius.smRadius,
                          ),
                          child: SingleChildScrollView(
                            child: Text(
                              job.error.isNotEmpty ? job.error : _pollError,
                              style: const TextStyle(
                                color: AppColors.statusError,
                                fontSize: 13,
                              ),
                            ),
                          ),
                        ),
                      ),
                    ],
                  ],
                ),
              ),
              const SizedBox(height: 16),
              SizedBox(
                width: double.infinity,
                height: 44,
                child: ElevatedButton(
                  onPressed: job.finished
                      ? _close
                      : () => Navigator.of(context).pop(false),
                  child: Text(
                    job.completed
                        ? '完成'
                        : job.waitingForUserAction
                        ? '等待电脑确认'
                        : '转入后台运行',
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  RuntimePreflightEvent? _latestEventForItem(RuntimePreflightItem item) {
    for (final event in _events.reversed) {
      if (event.itemType == item.itemType && event.itemId == item.itemId) {
        return event;
      }
    }
    return null;
  }
}

class _RuntimeUserActionBanner extends StatelessWidget {
  final String deviceName;
  final RuntimeUserAction action;

  const _RuntimeUserActionBanner({
    required this.deviceName,
    required this.action,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: AppColors.statusWarningLight,
        borderRadius: AppRadius.mdRadius,
        border: Border.all(color: AppColors.statusWarning),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Icon(
            Icons.desktop_windows_outlined,
            size: 21,
            color: AppColors.statusWarning,
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  action.title.isEmpty ? '需要在电脑上确认' : action.title,
                  style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: AppColors.textPrimary,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                const SizedBox(height: 3),
                Text(
                  '$deviceName · ${action.message}',
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: AppColors.textSecondary,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _RuntimeUserActionDialog extends StatelessWidget {
  final String deviceName;
  final RuntimeUserAction action;

  const _RuntimeUserActionDialog({
    required this.deviceName,
    required this.action,
  });

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      icon: const Icon(
        Icons.desktop_windows_outlined,
        color: AppColors.statusWarning,
      ),
      title: Text(action.title.isEmpty ? '需要在电脑上确认' : action.title),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 420),
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                deviceName,
                style: Theme.of(
                  context,
                ).textTheme.labelLarge?.copyWith(color: AppColors.primary),
              ),
              const SizedBox(height: 8),
              Text(
                action.message.isEmpty ? '请在目标电脑完成系统提示的操作。' : action.message,
              ),
              if (action.instructions.isNotEmpty) ...[
                const SizedBox(height: 14),
                for (
                  var index = 0;
                  index < action.instructions.length;
                  index++
                ) ...[
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      SizedBox(width: 24, child: Text('${index + 1}.')),
                      Expanded(child: Text(action.instructions[index])),
                    ],
                  ),
                  if (index + 1 < action.instructions.length)
                    const SizedBox(height: 7),
                ],
              ],
            ],
          ),
        ),
      ),
      actions: [
        FilledButton.icon(
          onPressed: () => Navigator.of(context).pop(),
          icon: const Icon(Icons.check_rounded),
          label: const Text('我知道了'),
        ),
      ],
    );
  }
}

class _VerifyTokenSubmission {
  final Map<String, String> values;
  final bool overrideGlobal;
  final bool disableVerifyMcp;

  const _VerifyTokenSubmission({
    required this.values,
    required this.overrideGlobal,
    this.disableVerifyMcp = false,
  });
}

enum _VerifyTokenResolution { saved, disabled, cancelled }

class _VerifyTokenInputDialog extends StatefulWidget {
  const _VerifyTokenInputDialog();

  @override
  State<_VerifyTokenInputDialog> createState() =>
      _VerifyTokenInputDialogState();
}

class _VerifyTokenInputDialogState extends State<_VerifyTokenInputDialog> {
  final TextEditingController _apiController = TextEditingController();
  final TextEditingController _protectController = TextEditingController();
  String _error = '';
  bool _obscure = true;
  bool _overrideGlobal = false;

  Future<void> _openVerifyCredentialPage() async {
    try {
      final opened = await launchUrl(
        Uri.parse(_verifyCredentialUrl),
        mode: LaunchMode.platformDefault,
      );
      if (!opened && mounted) {
        showAppFeedback(context, message: 'Verify 凭证页面打开失败');
      }
    } catch (_) {
      if (mounted) {
        showAppFeedback(context, message: 'Verify 凭证页面打开失败');
      }
    }
  }

  @override
  void dispose() {
    _apiController.dispose();
    _protectController.dispose();
    super.dispose();
  }

  void _submit() {
    final values = <String, String>{
      if (_apiController.text.trim().isNotEmpty)
        'VERIFY_API_TOKEN': _apiController.text.trim(),
      if (_protectController.text.trim().isNotEmpty)
        'VERIFY_PROTECT_TOKEN': _protectController.text.trim(),
    };
    if (values.isEmpty) {
      setState(() => _error = '至少输入一个 Token');
      return;
    }
    Navigator.of(context).pop(
      _VerifyTokenSubmission(values: values, overrideGlobal: _overrideGlobal),
    );
  }

  void _disableVerifyMcp() {
    Navigator.of(context).pop(
      const _VerifyTokenSubmission(
        values: {},
        overrideGlobal: false,
        disableVerifyMcp: true,
      ),
    );
  }

  Widget _tokenField({
    required TextEditingController controller,
    required String label,
    required String hint,
  }) {
    return TextField(
      controller: controller,
      obscureText: _obscure,
      enableSuggestions: false,
      autocorrect: false,
      textInputAction: TextInputAction.next,
      decoration: InputDecoration(
        labelText: label,
        hintText: hint,
        suffixIcon: IconButton(
          tooltip: _obscure ? '显示 Token' : '隐藏 Token',
          onPressed: () => setState(() => _obscure = !_obscure),
          icon: Icon(
            _obscure
                ? Icons.visibility_outlined
                : Icons.visibility_off_outlined,
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      icon: const Icon(Icons.key_rounded, color: AppColors.primary),
      title: const Text('配置 Verify Token'),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 420),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text(
              'API Token 和 Protect Token 可以同时填写；留空的字段会保留已有配置。',
              style: TextStyle(fontSize: 12, color: AppColors.textMuted),
            ),
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                onPressed: _openVerifyCredentialPage,
                icon: const Icon(Icons.open_in_new_rounded, size: 16),
                label: const Text('打开 Verify 凭证网站'),
              ),
            ),
            const SizedBox(height: 12),
            _tokenField(
              controller: _apiController,
              label: 'VERIFY_API_TOKEN · API Token',
              hint: '用于 Verify API、配置和 MCP 访问',
            ),
            const SizedBox(height: 10),
            _tokenField(
              controller: _protectController,
              label: 'VERIFY_PROTECT_TOKEN · Protect Token',
              hint: '用于保护、加固和逆向相关任务',
            ),
            const SizedBox(height: 4),
            CheckboxListTile(
              value: _overrideGlobal,
              onChanged: (value) {
                setState(() => _overrideGlobal = value ?? false);
              },
              contentPadding: EdgeInsets.zero,
              controlAffinity: ListTileControlAffinity.leading,
              title: const Text('是否覆盖全局'),
              subtitle: const Text('勾选后写入全局，并清除当前 Agent 的同名 Token'),
              dense: true,
            ),
            if (_error.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text(
                _error,
                style: const TextStyle(
                  fontSize: 12,
                  color: AppColors.statusError,
                ),
              ),
            ],
          ],
        ),
      ),
      actions: [
        SizedBox(
          width: double.infinity,
          child: Row(
            children: [
              Expanded(
                child: TextButton(
                  onPressed: () => Navigator.of(context).pop(),
                  style: TextButton.styleFrom(
                    minimumSize: const Size(0, 40),
                    padding: const EdgeInsets.symmetric(horizontal: 4),
                  ),
                  child: const FittedBox(
                    fit: BoxFit.scaleDown,
                    child: Text('取消切换'),
                  ),
                ),
              ),
              const SizedBox(width: 6),
              Expanded(
                child: TextButton.icon(
                  onPressed: _disableVerifyMcp,
                  style: TextButton.styleFrom(
                    minimumSize: const Size(0, 40),
                    padding: const EdgeInsets.symmetric(horizontal: 4),
                  ),
                  icon: const Icon(Icons.block_outlined, size: 16),
                  label: const FittedBox(
                    fit: BoxFit.scaleDown,
                    child: Text('关闭 Verify MCP'),
                  ),
                ),
              ),
              const SizedBox(width: 6),
              Expanded(
                child: FilledButton.icon(
                  onPressed: _submit,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size(0, 40),
                    padding: const EdgeInsets.symmetric(horizontal: 4),
                  ),
                  icon: const Icon(Icons.play_arrow_rounded, size: 16),
                  label: const FittedBox(
                    fit: BoxFit.scaleDown,
                    child: Text('保存并继续'),
                  ),
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

const _verifyCredentialUrl = 'https://www.xyapi.top';

Future<_VerifyTokenResolution> _ensureVerifyToken({
  required BuildContext context,
  required DeviceRepository repository,
  required String machineId,
  required String agentId,
  bool requireProtectToken = false,
  bool forcePrompt = false,
}) async {
  final config = await repository.getDeviceEnvConfig(machineId);
  final agentEnvironment = Map<String, String>.from(
    config.agentById(agentId)?.environment ?? const {},
  );
  final effectiveEnvironment = <String, String>{
    ...config.globalEnvironment,
    ...agentEnvironment,
  };
  final hasApiToken = _isUsableVerifyToken(
    effectiveEnvironment['VERIFY_API_TOKEN'],
  );
  final hasProtectToken = _isUsableVerifyToken(
    effectiveEnvironment['VERIFY_PROTECT_TOKEN'],
  );
  final ready = requireProtectToken
      ? hasApiToken && hasProtectToken
      : hasApiToken || hasProtectToken;
  if (!forcePrompt && ready) {
    return _VerifyTokenResolution.saved;
  }
  if (!context.mounted) return _VerifyTokenResolution.cancelled;
  final submission = await showDialog<_VerifyTokenSubmission>(
    context: context,
    barrierDismissible: false,
    builder: (_) => const _VerifyTokenInputDialog(),
  );
  if (submission == null) return _VerifyTokenResolution.cancelled;
  if (submission.disableVerifyMcp) {
    return _VerifyTokenResolution.disabled;
  }
  for (var attempt = 0; attempt < 2; attempt++) {
    final latest = await repository.getDeviceEnvConfig(machineId);
    final latestAgentEnvironment = Map<String, String>.from(
      latest.agentById(agentId)?.environment ?? const {},
    );
    final latestGlobalEnvironment = Map<String, String>.from(
      latest.globalEnvironment,
    );
    if (submission.overrideGlobal) {
      latestGlobalEnvironment.addAll(submission.values);
      for (final key in submission.values.keys) {
        latestAgentEnvironment.remove(key);
      }
    } else {
      latestAgentEnvironment.addAll(submission.values);
    }
    try {
      await repository.saveDeviceEnvConfig(
        machineId: machineId,
        globalEnvironment: latestGlobalEnvironment,
        agentId: agentId,
        agentEnvironment: latestAgentEnvironment,
        expectedRevision: latest.revision,
      );
      return _VerifyTokenResolution.saved;
    } catch (error) {
      final conflict = error.toString().contains('配置已被其他客户端修改');
      if (!conflict || attempt == 1) rethrow;
    }
  }
  return _VerifyTokenResolution.cancelled;
}

bool _requiresVerifyProtectToken(String semanticAgentId) {
  return semanticAgentId.trim().toLowerCase() == 'reverse-android';
}

bool _isUsableVerifyToken(String? value) {
  final normalized = value?.trim().toLowerCase() ?? '';
  if (normalized.isEmpty ||
      normalized.contains('replace_me') ||
      normalized.contains('<redacted>') ||
      normalized.contains('****')) {
    return false;
  }
  return !RegExp(r'^(vat|vpt)_x+$').hasMatch(normalized);
}

class _PreflightItemTile extends StatelessWidget {
  final RuntimePreflightItem item;
  final RuntimePreflightEvent? latestEvent;

  const _PreflightItemTile({required this.item, this.latestEvent});

  @override
  Widget build(BuildContext context) {
    final failed = item.status == 'failed' || item.status == 'cancelled';
    final completed = item.status == 'completed';
    final canShowProgress =
        item.itemType == 'runtime' || item.itemType == 'mcp';
    final eventProgress = latestEvent?.progressPercent ?? 0;
    final progress = math.max(
      item.progressPercent.clamp(0, 100),
      eventProgress.clamp(0, 100),
    );
    final latestMessage = latestEvent?.message.trim() ?? '';
    final latestSuffix = latestEvent == null || latestEvent!.totalBytes <= 0
        ? ''
        : ' ${_formatBytes(latestEvent!.receivedBytes)}/${_formatBytes(latestEvent!.totalBytes)}';
    return Container(
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: AppColors.surfaceElevated,
        borderRadius: AppRadius.mdRadius,
        border: Border.all(color: AppColors.borderLight),
      ),
      child: Row(
        children: [
          Icon(
            failed
                ? Icons.error_outline_rounded
                : completed
                ? Icons.check_circle_outline_rounded
                : Icons.sync_rounded,
            size: 20,
            color: failed
                ? AppColors.statusError
                : completed
                ? AppColors.statusOnline
                : AppColors.primary,
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  item.name.isEmpty ? item.itemId : item.name,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(
                    context,
                  ).textTheme.bodyMedium?.copyWith(fontWeight: FontWeight.w600),
                ),
                if (item.error.isNotEmpty) ...[
                  const SizedBox(height: 3),
                  Text(
                    item.error,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: AppColors.statusError,
                    ),
                  ),
                ] else if (canShowProgress && latestMessage.isNotEmpty) ...[
                  const SizedBox(height: 3),
                  Text(
                    '$latestMessage$latestSuffix',
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
                if (canShowProgress) ...[
                  const SizedBox(height: 8),
                  ClipRRect(
                    borderRadius: AppRadius.smRadius,
                    child: LinearProgressIndicator(
                      minHeight: 5,
                      value: progress / 100,
                      backgroundColor: AppColors.borderLight,
                      color: failed
                          ? AppColors.statusError
                          : completed
                          ? AppColors.statusOnline
                          : AppColors.primary,
                    ),
                  ),
                ],
              ],
            ),
          ),
          const SizedBox(width: 10),
          Text(
            _preflightStatusText(item.status),
            style: Theme.of(context).textTheme.labelSmall,
          ),
        ],
      ),
    );
  }
}

class _PreflightEventRow extends StatelessWidget {
  final RuntimePreflightEvent event;

  const _PreflightEventRow({required this.event});

  @override
  Widget build(BuildContext context) {
    final message = event.message.isEmpty ? event.phase : event.message;
    final prefix = _preflightEventPrefix(event);
    final suffix = event.totalBytes > 0
        ? ' ${_formatBytes(event.receivedBytes)}/${_formatBytes(event.totalBytes)}'
        : '';
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Container(
          width: 7,
          height: 7,
          margin: const EdgeInsets.only(top: 7),
          decoration: BoxDecoration(
            color: event.status == 'error'
                ? AppColors.statusError
                : event.status == 'success'
                ? AppColors.statusOnline
                : AppColors.primary,
            shape: BoxShape.circle,
          ),
        ),
        const SizedBox(width: 8),
        Expanded(
          child: Text(
            '$prefix$message$suffix',
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ),
      ],
    );
  }
}

String _preflightEventPrefix(RuntimePreflightEvent event) {
  final type = _preflightStepText(event.itemType);
  if (event.itemId.isEmpty) {
    return type.isEmpty ? '' : '$type · ';
  }
  return '$type ${event.itemId} · ';
}

String _preflightStatusText(String status) {
  switch (status) {
    case 'pending':
      return '等待';
    case 'running':
      return '进行中';
    case 'waiting_user_action':
      return '等待电脑确认';
    case 'completed':
      return '完成';
    case 'failed':
      return '失败';
    case 'cancelled':
      return '已取消';
    default:
      return status.isEmpty ? '未知' : status;
  }
}

String _preflightStepText(String step) {
  switch (step) {
    case 'runtime':
      return '运行时';
    case 'mcp':
      return 'MCP';
    case 'skill':
      return 'Skill';
    case 'apply':
      return '应用';
    case 'completed':
      return '完成';
    default:
      return step.isEmpty ? '准备中' : step;
  }
}

String _semanticPreflightOperationId(
  String machineId,
  String agentId,
  String jobId,
) {
  return 'semantic-preflight:$machineId:$agentId:$jobId';
}

void _syncSemanticPreflightNotification(
  AppNotificationController notifications,
  String operationId,
  RuntimePreflightJob job, {
  List<RuntimePreflightEvent> events = const [],
}) {
  reconcileSemanticPreflightNotification(
    notifications: notifications,
    operationId: operationId,
    job: job,
    events: events,
  );
}

String _formatBytes(int value) {
  if (value <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  var size = value.toDouble();
  var index = 0;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return '${size.toStringAsFixed(index == 0 ? 0 : 1)} ${units[index]}';
}

class _SemanticAgentEmptyState extends StatelessWidget {
  const _SemanticAgentEmptyState();

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(vertical: 24, horizontal: 16),
      decoration: const BoxDecoration(
        color: AppColors.surfaceElevated,
        borderRadius: AppRadius.mdRadius,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 48,
            height: 48,
            decoration: const BoxDecoration(
              color: AppColors.primaryLight,
              borderRadius: AppRadius.mdRadius,
            ),
            child: const Center(
              child: _SemanticAgentGlyphIcon(color: AppColors.primary),
            ),
          ),
          const SizedBox(height: 10),
          Text('暂无可选语义 Agent', style: Theme.of(context).textTheme.titleMedium),
        ],
      ),
    );
  }
}

class _AgentMCPSelectionDialog extends ConsumerStatefulWidget {
  final DeviceModel device;
  final AgentModel agent;

  const _AgentMCPSelectionDialog({required this.device, required this.agent});

  @override
  ConsumerState<_AgentMCPSelectionDialog> createState() =>
      _AgentMCPSelectionDialogState();
}

class _AgentMCPSelectionDialogState
    extends ConsumerState<_AgentMCPSelectionDialog> {
  bool _loading = true;
  bool _saving = false;
  bool _custom = false;
  String _error = '';
  List<DeviceMCPServerInfo> _servers = const [];
  Set<String> _selected = {};

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final selection = await repo.getDeviceAgentMCPSelection(
        machineId: widget.device.machineId,
        agentId: widget.agent.agentId,
      );
      final selected = selection.selectedServers.toSet();
      if (!selection.isCustom && selected.isEmpty) {
        selected.addAll(
          selection.availableServers
              .where((server) => server.enabled)
              .map((server) => server.name),
        );
      }
      if (!mounted) return;
      setState(() {
        _custom = selection.isCustom;
        _servers = selection.availableServers;
        _selected = selected;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  Future<void> _save({required bool restart}) async {
    if (_saving) return;
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final servers = _selected.toList()..sort();
      await repo.saveDeviceAgentMCPSelection(
        machineId: widget.device.machineId,
        agentId: widget.agent.agentId,
        mode: _custom ? 'custom' : 'inherit',
        servers: _custom ? servers : const [],
      );
      if (restart && !widget.agent.isDisabled) {
        await repo.restartDeviceAgent(
          machineId: widget.device.machineId,
          agentId: widget.agent.agentId,
        );
      }
      ref.invalidate(deviceDetailProvider(widget.device.machineId));
      ref.invalidate(deviceListProvider);
      if (!mounted) return;
      Navigator.of(context).pop();
      showAppFeedback(
        context,
        message: restart ? '已保存并重启 Agent' : '已保存 Agent MCP',
      );
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _saving = false;
        });
      }
    }
  }

  void _setCustom(bool value) {
    setState(() {
      _custom = value;
      if (!value) {
        _selected = _servers
            .where((server) => server.enabled)
            .map((server) => server.name)
            .toSet();
      }
    });
  }

  void _toggleServer(String name, bool selected) {
    setState(() {
      final next = Set<String>.from(_selected);
      if (selected) {
        next.add(name);
      } else {
        next.remove(name);
      }
      _selected = next;
    });
  }

  Widget _modeChip({
    required String label,
    required bool selected,
    required VoidCallback onTap,
  }) {
    return ChoiceChip(
      label: Text(label),
      selected: selected,
      onSelected: _saving ? null : (value) => value ? onTap() : null,
      selectedColor: AppColors.primaryLight,
      backgroundColor: AppColors.surfaceElevated,
      side: BorderSide(
        color: selected ? AppColors.primaryMuted : AppColors.border,
      ),
      labelStyle: TextStyle(
        color: selected ? AppColors.primary : AppColors.textSecondary,
        fontWeight: FontWeight.w600,
      ),
    );
  }

  Widget _serverTile(DeviceMCPServerInfo server) {
    final selected = _selected.contains(server.name);
    final enabled = _custom && !_saving;
    final muted = !enabled;
    return InkWell(
      onTap: enabled ? () => _toggleServer(server.name, !selected) : null,
      borderRadius: AppRadius.mdRadius,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Row(
          children: [
            Checkbox.adaptive(
              value: selected,
              onChanged: enabled
                  ? (value) => _toggleServer(server.name, value ?? false)
                  : null,
            ),
            Container(
              width: 34,
              height: 34,
              decoration: BoxDecoration(
                color: muted
                    ? AppColors.statusOfflineLight
                    : AppColors.primaryLight,
                borderRadius: AppRadius.smRadius,
              ),
              child: Icon(
                server.type == 'remote'
                    ? Icons.cloud_outlined
                    : Icons.terminal_rounded,
                size: 18,
                color: muted ? AppColors.textMuted : AppColors.primary,
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    server.name,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.labelLarge?.copyWith(
                      color: muted
                          ? AppColors.textSecondary
                          : AppColors.textPrimary,
                    ),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    server.enabled ? '设备默认启用' : '设备默认停用',
                    style: Theme.of(context).textTheme.labelSmall,
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final dialogWidth = math.min(
      math.max(MediaQuery.sizeOf(context).width - 40, 0.0),
      520.0,
    );
    final listHeight = math.min(
      math.max(MediaQuery.sizeOf(context).height * 0.32, 180.0),
      320.0,
    );
    final maxDialogHeight = math.max(
      360.0,
      MediaQuery.sizeOf(context).height - 48,
    );
    final serverContentHeight = _servers.isEmpty
        ? 126.0
        : math.min(
            listHeight,
            _servers.length * 78.0 + math.max(0, _servers.length - 1) * 1.0,
          );
    final dialogHeight = math.min(
      maxDialogHeight,
      math.max(
        420.0,
        316.0 + serverContentHeight + (_error.isNotEmpty ? 58.0 : 0.0),
      ),
    );
    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
      backgroundColor: AppColors.surface,
      shape: const RoundedRectangleBorder(borderRadius: AppRadius.lgRadius),
      child: SizedBox(
        width: dialogWidth,
        height: dialogHeight,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 18, 20, 18),
          child: _loading
              ? const Padding(
                  padding: EdgeInsets.symmetric(vertical: 8),
                  child: DialogContentSkeleton(itemCount: 3),
                )
              : Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Container(
                          width: 40,
                          height: 40,
                          decoration: const BoxDecoration(
                            color: AppColors.primaryLight,
                            borderRadius: AppRadius.mdRadius,
                          ),
                          child: const Center(
                            child: _AgentGlyphIcon(
                              size: 24,
                              color: AppColors.primary,
                            ),
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                'Agent MCP',
                                style: Theme.of(context).textTheme.titleLarge,
                              ),
                              const SizedBox(height: 2),
                              Text(
                                widget.agent.projectId,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: Theme.of(context).textTheme.labelSmall
                                    ?.copyWith(fontFamily: 'monospace'),
                              ),
                            ],
                          ),
                        ),
                        IconButton(
                          tooltip: '关闭',
                          onPressed: _saving
                              ? null
                              : () => Navigator.of(context).pop(),
                          icon: const Icon(Icons.close_rounded),
                        ),
                      ],
                    ),
                    const SizedBox(height: 16),
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      children: [
                        _modeChip(
                          label: '跟随默认',
                          selected: !_custom,
                          onTap: () => _setCustom(false),
                        ),
                        _modeChip(
                          label: '自定义选择',
                          selected: _custom,
                          onTap: () => _setCustom(true),
                        ),
                      ],
                    ),
                    const SizedBox(height: 8),
                    Text(
                      _custom ? '仅此 Agent 加载选中的 MCP' : '使用设备默认启用项',
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                    const SizedBox(height: 14),
                    const Divider(),
                    const SizedBox(height: 8),
                    if (_servers.isEmpty)
                      const _AgentMCPEmptyState()
                    else
                      ConstrainedBox(
                        constraints: BoxConstraints(maxHeight: listHeight),
                        child: ListView.separated(
                          shrinkWrap: true,
                          itemCount: _servers.length,
                          separatorBuilder: (_, _) =>
                              const Divider(color: AppColors.borderLight),
                          itemBuilder: (context, index) =>
                              _serverTile(_servers[index]),
                        ),
                      ),
                    if (_error.isNotEmpty) ...[
                      const SizedBox(height: 12),
                      Container(
                        width: double.infinity,
                        padding: const EdgeInsets.all(10),
                        decoration: const BoxDecoration(
                          color: AppColors.statusErrorLight,
                          borderRadius: AppRadius.smRadius,
                        ),
                        child: Text(
                          _error,
                          style: const TextStyle(
                            color: AppColors.statusError,
                            fontSize: 13,
                          ),
                        ),
                      ),
                    ],
                    const SizedBox(height: 16),
                    const Divider(),
                    const SizedBox(height: 10),
                    Row(
                      mainAxisAlignment: MainAxisAlignment.end,
                      children: [
                        TextButton(
                          onPressed: _saving
                              ? null
                              : () => Navigator.of(context).pop(),
                          child: const Text('取消'),
                        ),
                        const SizedBox(width: 8),
                        TextButton(
                          onPressed: _loading || _saving
                              ? null
                              : () => _save(restart: false),
                          child: Text(_saving ? '保存中...' : '保存'),
                        ),
                      ],
                    ),
                    const SizedBox(height: 8),
                    SizedBox(
                      width: double.infinity,
                      height: 46,
                      child: ElevatedButton(
                        onPressed:
                            _loading || _saving || widget.agent.isDisabled
                            ? null
                            : () => _save(restart: true),
                        child: const Text('保存并重启'),
                      ),
                    ),
                  ],
                ),
        ),
      ),
    );
  }
}

class _AgentMCPEmptyState extends StatelessWidget {
  const _AgentMCPEmptyState();

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(vertical: 24, horizontal: 16),
      decoration: const BoxDecoration(
        color: AppColors.surfaceElevated,
        borderRadius: AppRadius.mdRadius,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 48,
            height: 48,
            decoration: const BoxDecoration(
              color: AppColors.primaryLight,
              borderRadius: AppRadius.mdRadius,
            ),
            child: const Center(
              child: _AgentGlyphIcon(size: 28, color: AppColors.primary),
            ),
          ),
          const SizedBox(height: 10),
          Text(
            '设备暂无 MCP 配置',
            style: Theme.of(
              context,
            ).textTheme.bodyMedium?.copyWith(color: AppColors.textMuted),
          ),
        ],
      ),
    );
  }
}

class _AgentSkillSelectionDialog extends ConsumerStatefulWidget {
  final DeviceModel device;
  final AgentModel agent;

  const _AgentSkillSelectionDialog({required this.device, required this.agent});

  @override
  ConsumerState<_AgentSkillSelectionDialog> createState() =>
      _AgentSkillSelectionDialogState();
}

class _AgentSkillSelectionDialogState
    extends ConsumerState<_AgentSkillSelectionDialog> {
  final TextEditingController _searchController = TextEditingController();
  final FocusNode _searchFocusNode = FocusNode();
  bool _loading = true;
  bool _saving = false;
  bool _searchVisible = false;
  String _query = '';
  String _error = '';
  List<DeviceSkillCatalogItem> _items = const [];
  Set<String> _defaults = {};
  Set<String> _selected = {};

  @override
  void initState() {
    super.initState();
    _searchController.addListener(() {
      if (mounted) setState(() => _query = _searchController.text.trim());
    });
    _searchFocusNode.addListener(() {
      if (!mounted || _searchFocusNode.hasFocus) return;
      setState(() => _searchVisible = false);
    });
    unawaited(_load());
  }

  @override
  void dispose() {
    _searchController.dispose();
    _searchFocusNode.dispose();
    super.dispose();
  }

  void _showSearch() {
    setState(() => _searchVisible = true);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _searchFocusNode.requestFocus();
    });
  }

  Future<void> _load() async {
    DeviceAgentSkillSelectionInfo selection =
        const DeviceAgentSkillSelectionInfo();
    String? selectionError;
    try {
      final repo = ref.read(deviceRepositoryProvider);
      try {
        selection = await repo.getDeviceAgentSkillSelection(
          machineId: widget.device.machineId,
          agentId: widget.agent.agentId,
        );
      } catch (error) {
        selectionError = error.toString();
      }
      if (!mounted) return;
      setState(() {
        _items = selection.availableSkills;
        _defaults = selection.defaultSkills.toSet();
        _selected = selection.extraSkills.toSet();
        _error = selectionError != null
            ? 'Skill 配置不可用（${_shortRequestError(selectionError)}）'
            : '';
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _error = 'Skill 服务暂不可用';
        _loading = false;
      });
    }
  }

  String _shortRequestError(String? error) {
    final value = error?.trim() ?? '';
    final match = RegExp(r'\b(4\d\d|5\d\d)\b').firstMatch(value);
    return match == null ? '请稍后重试' : 'HTTP ${match.group(1)}';
  }

  List<DeviceSkillCatalogItem> get _filteredItems {
    final query = _query.toLowerCase();
    return _items.where((item) {
      if (query.isEmpty) return true;
      return item.searchableText.contains(query);
    }).toList();
  }

  void _toggle(DeviceSkillCatalogItem item, bool value) {
    if (_defaults.contains(item.id) || _saving) return;
    setState(() {
      final next = Set<String>.from(_selected);
      if (value) {
        next.add(item.id);
      } else {
        next.remove(item.id);
      }
      _selected = next;
    });
  }

  Future<void> _save({required bool restart}) async {
    if (_saving) return;
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final ids = _selected.toList()..sort();
      await repo.saveDeviceAgentSkillSelection(
        machineId: widget.device.machineId,
        agentId: widget.agent.agentId,
        extraSkillIds: ids,
      );
      if (restart && !widget.agent.isDisabled) {
        await repo.restartDeviceAgent(
          machineId: widget.device.machineId,
          agentId: widget.agent.agentId,
        );
      }
      ref.invalidate(deviceDetailProvider(widget.device.machineId));
      ref.invalidate(deviceListProvider);
      if (!mounted) return;
      Navigator.of(context).pop();
      showAppFeedback(
        context,
        message: restart ? 'Skill 已保存并重启 Agent' : 'Agent Skill 已保存',
      );
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _importSkill() async {
    if (_saving) return;
    final picked = await FilePicker.platform.pickFiles(
      type: FileType.custom,
      allowedExtensions: const ['md', 'zip'],
      withData: true,
    );
    final file = picked?.files.single;
    if (file == null || file.bytes == null) return;
    if (!mounted) return;
    try {
      final imported = DeviceSkillImportParser.parseFile(
        file.name,
        file.bytes!,
      );
      setState(() {
        _saving = true;
        _error = '';
      });
      final selection = await ref
          .read(deviceRepositoryProvider)
          .importDeviceAgentSkill(
            machineId: widget.device.machineId,
            agentId: widget.agent.agentId,
            skill: imported,
            overwrite: true,
          );
      final item = DeviceSkillCatalogItem(
        id: imported.name,
        name: imported.name,
        description: imported.description,
        content: imported.content,
        category: '本设备',
        source: '设备本地',
        packageFiles: imported.packageFiles,
      );
      if (!mounted) return;
      setState(() {
        _items = [..._items.where((current) => current.id != item.id), item];
        _selected = selection.extraSkills.toSet();
      });
      showAppFeedback(context, message: 'Skill 已导入当前设备并加入 Agent');
    } on FormatException catch (error) {
      if (mounted) setState(() => _error = error.message.toString());
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _showDetail(DeviceSkillCatalogItem item) async {
    await showDeviceSkillDetailDialog(
      context: context,
      title: item.displayName,
      description: item.description,
      content: item.content,
      tags: item.tags,
      category: item.category,
      source: item.source,
      packageFiles: item.packageFiles,
    );
  }

  Widget _skillTile(DeviceSkillCatalogItem item) {
    final isDefault = _defaults.contains(item.id);
    final checked = isDefault || _selected.contains(item.id);
    return InkWell(
      onTap: isDefault || _saving ? null : () => _toggle(item, !checked),
      borderRadius: AppRadius.mdRadius,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Checkbox.adaptive(
              value: checked,
              onChanged: isDefault || _saving
                  ? null
                  : (value) => _toggle(item, value ?? false),
            ),
            Container(
              width: 34,
              height: 34,
              margin: const EdgeInsets.only(top: 6),
              decoration: const BoxDecoration(
                color: AppColors.primaryLight,
                borderRadius: AppRadius.smRadius,
              ),
              child: const Icon(
                Icons.auto_awesome_outlined,
                size: 18,
                color: AppColors.primary,
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.only(top: 5),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            item.displayName,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: Theme.of(context).textTheme.labelLarge,
                          ),
                        ),
                        IconButton(
                          tooltip: '从设备导入 Skill',
                          onPressed: _saving ? null : _importSkill,
                          icon: const Icon(Icons.file_upload_outlined),
                          visualDensity: VisualDensity.compact,
                        ),
                        IconButton(
                          tooltip: '查看 Skill 内容',
                          onPressed: () => _showDetail(item),
                          icon: const Icon(
                            Icons.info_outline_rounded,
                            size: 18,
                          ),
                          visualDensity: VisualDensity.compact,
                        ),
                      ],
                    ),
                    if (isDefault)
                      const Padding(
                        padding: EdgeInsets.only(top: 2),
                        child: StatusPill(
                          label: '语义 Agent 默认',
                          type: StatusType.processing,
                        ),
                      ),
                    if (item.description.isNotEmpty) ...[
                      const SizedBox(height: 3),
                      Text(
                        item.description,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                    ],
                    if (item.category.isNotEmpty || item.source.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Text(
                        [
                          item.category,
                          item.source,
                        ].where((value) => value.isNotEmpty).join(' · '),
                        style: Theme.of(context).textTheme.labelSmall,
                      ),
                    ],
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final width = math.min(MediaQuery.sizeOf(context).width - 32, 560.0);
    final items = _filteredItems;
    final maxHeight = math.max(360.0, MediaQuery.sizeOf(context).height - 40);
    final skillListHeight = items.isEmpty
        ? 112.0
        : math.min(
            400.0,
            items.length * 92.0 + math.max(0, items.length - 1) * 1.0,
          );
    final height = math.min(
      maxHeight,
      math.max(
        420.0,
        300.0 +
            skillListHeight +
            (_searchVisible ? 58.0 : 0.0) +
            (_error.isNotEmpty ? 58.0 : 0.0),
      ),
    );
    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 20),
      backgroundColor: AppColors.surface,
      shape: const RoundedRectangleBorder(borderRadius: AppRadius.lgRadius),
      child: SizedBox(
        width: width,
        height: height,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 18, 20, 16),
          child: _loading
              ? const DialogContentSkeleton(itemCount: 4)
              : Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        const Icon(
                          Icons.auto_awesome_outlined,
                          color: AppColors.primary,
                        ),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                'Agent Skill',
                                style: Theme.of(context).textTheme.titleLarge,
                              ),
                              Text(
                                '默认 Skill 自动跟随语义 Agent，额外 Skill 仅作用于当前 Agent',
                                style: Theme.of(context).textTheme.bodySmall,
                              ),
                            ],
                          ),
                        ),
                        IconButton(
                          tooltip: '搜索 Skill',
                          onPressed: _showSearch,
                          icon: const Icon(Icons.search_rounded),
                        ),
                        IconButton(
                          tooltip: '关闭',
                          onPressed: _saving
                              ? null
                              : () => Navigator.of(context).pop(),
                          icon: const Icon(Icons.close_rounded),
                        ),
                      ],
                    ),
                    const SizedBox(height: 14),
                    if (_searchVisible)
                      TextField(
                        controller: _searchController,
                        focusNode: _searchFocusNode,
                        decoration: InputDecoration(
                          hintText: '搜索 Skill 名称、描述、分类或标签',
                          prefixIcon: const Icon(Icons.search_rounded),
                          suffixIcon: IconButton(
                            onPressed: () {
                              _searchController.clear();
                              _searchFocusNode.unfocus();
                            },
                            icon: const Icon(Icons.close_rounded),
                          ),
                        ),
                      ),
                    if (_searchVisible) const SizedBox(height: 10),
                    const SizedBox(height: 10),
                    Text(
                      '默认 ${_defaults.length} · 额外 ${_selected.length} · 共 ${_items.length}',
                      style: Theme.of(context).textTheme.labelSmall,
                    ),
                    const SizedBox(height: 8),
                    if (_error.isNotEmpty)
                      Container(
                        width: double.infinity,
                        margin: const EdgeInsets.only(bottom: 8),
                        padding: const EdgeInsets.all(10),
                        decoration: const BoxDecoration(
                          color: AppColors.statusErrorLight,
                          borderRadius: AppRadius.smRadius,
                        ),
                        child: Text(
                          _error,
                          style: const TextStyle(
                            color: AppColors.statusError,
                            fontSize: 12,
                          ),
                        ),
                      ),
                    Expanded(
                      child: items.isEmpty
                          ? const EmptyState(
                              message: '没有匹配的 Skill',
                              icon: Icons.search_off_rounded,
                            )
                          : ListView.separated(
                              itemCount: items.length,
                              separatorBuilder: (_, _) =>
                                  const Divider(color: AppColors.borderLight),
                              itemBuilder: (context, index) =>
                                  _skillTile(items[index]),
                            ),
                    ),
                    const SizedBox(height: 10),
                    const Divider(),
                    const SizedBox(height: 8),
                    LayoutBuilder(
                      builder: (context, constraints) {
                        final compact = constraints.maxWidth < 420;
                        final buttonStyle = ButtonStyle(
                          minimumSize: WidgetStateProperty.all(
                            const Size(0, 44),
                          ),
                          padding: WidgetStateProperty.all(
                            const EdgeInsets.symmetric(horizontal: 14),
                          ),
                        );
                        final cancel = TextButton.icon(
                          onPressed: _saving
                              ? null
                              : () => Navigator.of(context).pop(),
                          style: buttonStyle,
                          icon: const Icon(Icons.close_rounded, size: 17),
                          label: const Text('取消'),
                        );
                        final save = FilledButton.icon(
                          onPressed: _saving
                              ? null
                              : () => _save(restart: false),
                          style: buttonStyle,
                          icon: const Icon(Icons.check_rounded, size: 17),
                          label: Text(_saving ? '保存中...' : '保存'),
                        );
                        final saveAndRestart = OutlinedButton.icon(
                          onPressed: _saving || widget.agent.isDisabled
                              ? null
                              : () => _save(restart: true),
                          style: buttonStyle,
                          icon: const Icon(Icons.restart_alt_rounded, size: 17),
                          label: const Text('保存并重启'),
                        );
                        if (compact) {
                          return Column(
                            crossAxisAlignment: CrossAxisAlignment.stretch,
                            children: [
                              Row(
                                children: [
                                  Expanded(child: save),
                                  const SizedBox(width: 8),
                                  Expanded(child: saveAndRestart),
                                ],
                              ),
                              const SizedBox(height: 4),
                              Center(child: cancel),
                            ],
                          );
                        }
                        return Row(
                          children: [
                            cancel,
                            const Spacer(),
                            save,
                            const SizedBox(width: 8),
                            saveAndRestart,
                          ],
                        );
                      },
                    ),
                  ],
                ),
        ),
      ),
    );
  }
}

/// 分页布局：设备 / 处理 / 空闲 / 离线。
///
/// 分页归属：处理 = [AgentModel.isBusy]；空闲 = 在线且无任务；
/// 离线 = 非在线（含已停止/异常/已停用）。
class _PagedDeviceDetail extends StatefulWidget {
  final DeviceModel device;

  const _PagedDeviceDetail({required this.device});

  @override
  State<_PagedDeviceDetail> createState() => _PagedDeviceDetailState();
}

class _PagedDeviceDetailState extends State<_PagedDeviceDetail> {
  int _tab = 0;

  String get _tabKey =>
      'device_detail_paged_tab_${widget.device.machineId.trim()}';

  @override
  void initState() {
    super.initState();
    _tab = _readSavedTab();
  }

  @override
  void didUpdateWidget(covariant _PagedDeviceDetail oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.device.machineId != widget.device.machineId) {
      setState(() => _tab = _readSavedTab());
    }
  }

  int _readSavedTab() {
    final savedTab = int.tryParse(AppStorage.getString(_tabKey) ?? '');
    return savedTab != null && savedTab >= 0 && savedTab <= 3 ? savedTab : 0;
  }

  void _selectTab(int index) {
    if (index == _tab) return;
    setState(() => _tab = index);
    unawaited(AppStorage.setString(_tabKey, '$index'));
  }

  @override
  Widget build(BuildContext context) {
    final device = widget.device;
    final busy = device.agents.where((item) => item.isBusy).toList();
    final idle = device.agents
        .where((item) => item.isOnline && !item.isBusy)
        .toList();
    final offline = device.agents.where((item) => !item.isOnline).toList();
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
          child: _PagedSegment(
            index: _tab,
            busyCount: busy.length,
            idleCount: idle.length,
            offlineCount: offline.length,
            onChanged: _selectTab,
          ),
        ),
        Expanded(
          child: IndexedStack(
            index: _tab,
            children: [
              _PagedDeviceTab(device: device),
              _PagedAgentTab(
                device: device,
                agents: busy,
                emptyMessage: '当前没有处理中的 Agent',
              ),
              _PagedAgentTab(
                device: device,
                agents: idle,
                emptyMessage: '当前没有空闲的 Agent',
              ),
              _PagedAgentTab(
                device: device,
                agents: offline,
                emptyMessage: '当前没有离线的 Agent',
              ),
            ],
          ),
        ),
      ],
    );
  }
}

class _PagedSegment extends StatelessWidget {
  final int index;
  final int busyCount;
  final int idleCount;
  final int offlineCount;
  final ValueChanged<int> onChanged;

  const _PagedSegment({
    required this.index,
    required this.busyCount,
    required this.idleCount,
    required this.offlineCount,
    required this.onChanged,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      height: 43,
      padding: const EdgeInsets.all(3),
      decoration: BoxDecoration(
        color: AppColors.inputBackground,
        borderRadius: AppRadius.mdRadius,
        border: Border.all(color: AppColors.border),
      ),
      child: Row(
        children: [
          _PagedSegmentTab(
            label: '设备',
            selected: index == 0,
            onTap: () => onChanged(0),
          ),
          _PagedSegmentTab(
            label: '处理',
            count: busyCount,
            selected: index == 1,
            onTap: () => onChanged(1),
          ),
          _PagedSegmentTab(
            label: '空闲',
            count: idleCount,
            selected: index == 2,
            onTap: () => onChanged(2),
          ),
          _PagedSegmentTab(
            label: '离线',
            count: offlineCount,
            selected: index == 3,
            onTap: () => onChanged(3),
          ),
        ],
      ),
    );
  }
}

class _PagedSegmentTab extends StatelessWidget {
  final String label;
  final int? count;
  final bool selected;
  final VoidCallback onTap;

  const _PagedSegmentTab({
    required this.label,
    required this.selected,
    required this.onTap,
    this.count,
  });

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: Material(
        color: selected ? AppColors.surface : Colors.transparent,
        borderRadius: AppRadius.smRadius,
        child: InkWell(
          onTap: onTap,
          borderRadius: AppRadius.smRadius,
          child: Center(
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  label,
                  style: TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w700,
                    color: selected
                        ? AppColors.primary
                        : AppColors.textSecondary,
                  ),
                ),
                if (count != null) ...[
                  const SizedBox(width: 4),
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 6,
                      vertical: 1,
                    ),
                    decoration: BoxDecoration(
                      color: selected
                          ? AppColors.primaryLight
                          : AppColors.statusOfflineLight,
                      borderRadius: BorderRadius.circular(999),
                    ),
                    child: Text(
                      '$count',
                      style: TextStyle(
                        fontSize: 9,
                        fontWeight: FontWeight.w700,
                        color: selected
                            ? AppColors.primary
                            : AppColors.textMuted,
                      ),
                    ),
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

class _PagedDeviceTab extends StatelessWidget {
  final DeviceModel device;

  const _PagedDeviceTab({required this.device});

  @override
  Widget build(BuildContext context) {
    final padding = AppBreakpoints.isMobile(context) ? 16.0 : 24.0;
    return SingleChildScrollView(
      padding: EdgeInsets.all(padding),
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 720),
          child: _DeviceInfoPanel(device: device),
        ),
      ),
    );
  }
}

class _PagedAgentTab extends StatelessWidget {
  final DeviceModel device;
  final List<AgentModel> agents;
  final String emptyMessage;

  const _PagedAgentTab({
    required this.device,
    required this.agents,
    required this.emptyMessage,
  });

  @override
  Widget build(BuildContext context) {
    if (agents.isEmpty) {
      return EmptyState(message: emptyMessage, icon: Icons.smart_toy_outlined);
    }
    final padding = AppBreakpoints.isMobile(context) ? 16.0 : 24.0;
    return LayoutBuilder(
      builder: (context, constraints) {
        final twoColumns = constraints.maxWidth >= AppBreakpoints.md;
        return SingleChildScrollView(
          padding: EdgeInsets.all(padding),
          child: twoColumns
              ? Wrap(
                  spacing: 12,
                  runSpacing: 12,
                  children: [
                    for (final agent in agents)
                      SizedBox(
                        width: (constraints.maxWidth - padding * 2 - 12) / 2,
                        child: _AgentCard(agent: agent, device: device),
                      ),
                  ],
                )
              : Column(
                  children: [
                    for (final agent in agents)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 12),
                        child: _AgentCard(agent: agent, device: device),
                      ),
                  ],
                ),
        );
      },
    );
  }
}
