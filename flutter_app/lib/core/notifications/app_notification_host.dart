import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../services/app_navigation.dart';
import '../theme/app_colors.dart';
import 'app_notification_controller.dart';
import 'app_notification_model.dart';

class AppNotificationHost extends ConsumerWidget {
  final Widget child;

  const AppNotificationHost({super.key, required this.child});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(appNotificationControllerProvider);
    final controller = ref.read(appNotificationControllerProvider.notifier);
    final media = MediaQuery.of(context);
    final mobile = media.size.width < 600;
    final active = state.active
        .where(
          (record) =>
              !(state.foregroundChat?.matchesCompletion(record) ?? false),
        )
        .take(mobile ? 1 : 3)
        .toList(growable: false);

    return Stack(
      fit: StackFit.expand,
      children: [
        child,
        if (active.isNotEmpty && !state.centerOpen)
          Positioned(
            top: media.padding.top + 60,
            right: mobile ? 8 : 16,
            left: mobile ? 8 : null,
            child: Align(
              alignment: Alignment.topRight,
              child: state.collapsed
                  ? _NotificationIsland(
                      records: active,
                      onTap: controller.expand,
                      onDismiss: () => _dismissRecord(controller, active.first),
                    )
                  : _NotificationStack(
                      records: active,
                      mobile: mobile,
                      onCollapse: controller.collapse,
                      onOpenCenter: controller.openCenter,
                      onDismiss: (record) => _dismissRecord(controller, record),
                      onAction: (record, action) =>
                          _handleAction(controller, record, action),
                    ),
            ),
          ),
        if (state.centerOpen)
          _NotificationCenterOverlay(
            state: state,
            mobile: mobile,
            onClose: controller.closeCenter,
            onTabChanged: controller.selectCenterTab,
            onClearRecent: controller.clearRecent,
            onDismiss: (record) => _dismissRecord(controller, record),
            onAction: (record, action) =>
                _handleAction(controller, record, action),
          ),
      ],
    );
  }

  void _dismissRecord(
    AppNotificationController controller,
    AppNotificationRecord record,
  ) {
    if (record.isTerminal) {
      controller.dismiss(record.id);
    } else {
      controller.hideOperation(record.operationId);
    }
  }

  void _handleAction(
    AppNotificationController controller,
    AppNotificationRecord record,
    AppNotificationAction action,
  ) {
    switch (action.type) {
      case AppNotificationActionType.openRoute:
      case AppNotificationActionType.openLogs:
        final route = action.payload['route']?.trim() ?? '';
        if (route.isNotEmpty) {
          final navigationContext = appNavigatorContext;
          if (navigationContext == null) return;
          controller.markRead(record.operationId);
          try {
            if (record.operationId.startsWith('chat-task:')) {
              navigationContext.go(route);
            } else {
              navigationContext.push(route);
            }
            if (action.type == AppNotificationActionType.openRoute &&
                record.operationId.startsWith('chat-task:') &&
                record.isTerminal) {
              controller.archiveCompletedOperation(record.operationId);
            }
            controller.closeCenter();
          } catch (_) {
            // Keep the notification active when the destination cannot open.
          }
        }
        break;
      case AppNotificationActionType.retryOperation:
      case AppNotificationActionType.cancelOperation:
        break;
    }
  }
}

class AppNotificationCenterButton extends ConsumerWidget {
  final Color? color;

  const AppNotificationCenterButton({super.key, this.color});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(appNotificationControllerProvider);
    return IconButton(
      tooltip: '通知中心',
      onPressed: () =>
          ref.read(appNotificationControllerProvider.notifier).openCenter(),
      icon: Badge(
        isLabelVisible: state.unreadCount > 0,
        label: Text(state.unreadCount > 99 ? '99+' : '${state.unreadCount}'),
        child: Icon(
          Icons.notifications_none_rounded,
          size: 19,
          color: color ?? AppColors.textSecondary,
        ),
      ),
    );
  }
}

class _NotificationStack extends StatefulWidget {
  final List<AppNotificationRecord> records;
  final bool mobile;
  final VoidCallback onCollapse;
  final VoidCallback onOpenCenter;
  final ValueChanged<AppNotificationRecord> onDismiss;
  final void Function(
    AppNotificationRecord record,
    AppNotificationAction action,
  )
  onAction;

  const _NotificationStack({
    required this.records,
    required this.mobile,
    required this.onCollapse,
    required this.onOpenCenter,
    required this.onDismiss,
    required this.onAction,
  });

  @override
  State<_NotificationStack> createState() => _NotificationStackState();
}

class _NotificationStackState extends State<_NotificationStack> {
  double _dragDistance = 0;

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onVerticalDragStart: (_) => _dragDistance = 0,
      onVerticalDragUpdate: (details) => _dragDistance += details.delta.dy,
      onVerticalDragEnd: (details) {
        if (_dragDistance < -34 || (details.primaryVelocity ?? 0) < -420) {
          widget.onCollapse();
        }
        _dragDistance = 0;
      },
      child: SizedBox(
        width: math.min(390, MediaQuery.sizeOf(context).width - 16),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            for (var index = 0; index < widget.records.length; index++) ...[
              _NotificationCard(
                record: widget.records[index],
                onCollapse: widget.onCollapse,
                onOpenCenter: widget.onOpenCenter,
                onDismiss: () => widget.onDismiss(widget.records[index]),
                onAction: (action) =>
                    widget.onAction(widget.records[index], action),
              ),
              if (index != widget.records.length - 1) const SizedBox(height: 8),
            ],
          ],
        ),
      ),
    );
  }
}

class _NotificationCard extends StatelessWidget {
  final AppNotificationRecord record;
  final VoidCallback onCollapse;
  final VoidCallback onOpenCenter;
  final VoidCallback onDismiss;
  final ValueChanged<AppNotificationAction> onAction;

  const _NotificationCard({
    required this.record,
    required this.onCollapse,
    required this.onOpenCenter,
    required this.onDismiss,
    required this.onAction,
  });

  @override
  Widget build(BuildContext context) {
    final statusColor = _statusColor(record.status);
    final visibleActions = _visibleNotificationActions(record);
    final displayStyle = _resolvedDisplayStyle(record);
    final compact = displayStyle == AppNotificationDisplayStyle.compact;

    return Semantics(
      liveRegion: true,
      label: '${record.title}，${record.message}',
      child: Material(
        color: Colors.transparent,
        child: Container(
          clipBehavior: Clip.antiAlias,
          decoration: BoxDecoration(
            color: AppColors.surface.withValues(alpha: 0.98),
            borderRadius: BorderRadius.circular(8),
            border: Border.all(color: const Color(0xFFABC2E1)),
            boxShadow: const [
              BoxShadow(
                color: Color(0x261D3865),
                blurRadius: 36,
                offset: Offset(0, 14),
              ),
              BoxShadow(
                color: Color(0x141D3865),
                blurRadius: 8,
                offset: Offset(0, 3),
              ),
            ],
          ),
          child: Stack(
            children: [
              Positioned(
                top: 0,
                bottom: 0,
                left: 0,
                child: Container(width: 3, color: statusColor),
              ),
              Padding(
                padding: EdgeInsets.fromLTRB(16, compact ? 12 : 14, 12, 12),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        _NotificationIcon(record: record, size: 30),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                record.title,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(
                                  color: AppColors.textPrimary,
                                  fontSize: 13,
                                  height: 1.25,
                                  fontWeight: FontWeight.w700,
                                ),
                              ),
                              const SizedBox(height: 3),
                              Text(
                                record.message,
                                maxLines: compact ? 2 : 3,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(
                                  color: AppColors.textSecondary,
                                  fontSize: 10.5,
                                  height: 1.4,
                                ),
                              ),
                            ],
                          ),
                        ),
                        const SizedBox(width: 4),
                        _SmallIconButton(
                          tooltip: '通知中心',
                          icon: Icons.notifications_none_rounded,
                          onPressed: onOpenCenter,
                        ),
                        _SmallIconButton(
                          tooltip: '收起通知',
                          icon: Icons.keyboard_arrow_up_rounded,
                          onPressed: onCollapse,
                        ),
                        _SmallIconButton(
                          tooltip: '关闭通知',
                          icon: Icons.close_rounded,
                          onPressed: onDismiss,
                        ),
                      ],
                    ),
                    if (!compact) ...[
                      const SizedBox(height: 13),
                      _NotificationProgress(
                        record: record,
                        displayStyle: displayStyle,
                      ),
                    ],
                    if (visibleActions.isNotEmpty || !compact) ...[
                      const SizedBox(height: 11),
                      Container(height: 1, color: AppColors.borderLight),
                      const SizedBox(height: 9),
                      Row(
                        children: [
                          for (final action in visibleActions.take(2)) ...[
                            _NotificationActionButton(
                              action: action,
                              onPressed: () => onAction(action),
                            ),
                            const SizedBox(width: 7),
                          ],
                          const Spacer(),
                          Text(
                            _relativeTime(record.updatedAt),
                            style: const TextStyle(
                              color: AppColors.textMuted,
                              fontSize: 9,
                            ),
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
}

class _NotificationProgress extends StatelessWidget {
  final AppNotificationRecord record;
  final AppNotificationDisplayStyle displayStyle;

  const _NotificationProgress({
    required this.record,
    required this.displayStyle,
  });

  @override
  Widget build(BuildContext context) {
    return switch (displayStyle) {
      AppNotificationDisplayStyle.linear => _LinearNotificationProgress(
        record: record,
      ),
      AppNotificationDisplayStyle.ring => _RingNotificationProgress(
        record: record,
      ),
      AppNotificationDisplayStyle.stages => _StageNotificationProgress(
        record: record,
      ),
      AppNotificationDisplayStyle.sync => _SyncNotificationProgress(
        record: record,
      ),
      AppNotificationDisplayStyle.dots => _DotsNotificationProgress(
        record: record,
      ),
      AppNotificationDisplayStyle.automatic ||
      AppNotificationDisplayStyle.compact => const SizedBox.shrink(),
    };
  }
}

class _LinearNotificationProgress extends StatelessWidget {
  final AppNotificationRecord record;

  const _LinearNotificationProgress({required this.record});

  @override
  Widget build(BuildContext context) {
    final progress = record.normalizedProgress ?? 0;
    final determinate =
        record.progressMode == AppNotificationProgressMode.determinate;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        TweenAnimationBuilder<double>(
          tween: Tween(end: progress),
          duration: _motionDuration(context, 650),
          curve: Curves.easeOutCubic,
          builder: (context, value, _) => ClipRRect(
            borderRadius: BorderRadius.circular(99),
            child: LinearProgressIndicator(
              value: determinate ? value : null,
              minHeight: 5,
              backgroundColor: const Color(0xFFE6EDF7),
              color: _statusColor(record.status),
            ),
          ),
        ),
        const SizedBox(height: 6),
        Row(
          children: [
            Expanded(
              child: Text(
                determinate ? '正在处理' : '等待进度',
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(color: AppColors.textMuted, fontSize: 9),
              ),
            ),
            if (determinate)
              Text(
                '${(progress * 100).round()}%',
                style: const TextStyle(
                  color: AppColors.textSecondary,
                  fontSize: 10,
                  fontWeight: FontWeight.w700,
                ),
              ),
          ],
        ),
      ],
    );
  }
}

class _RingNotificationProgress extends StatelessWidget {
  final AppNotificationRecord record;

  const _RingNotificationProgress({required this.record});

  @override
  Widget build(BuildContext context) {
    final determinate =
        record.progressMode == AppNotificationProgressMode.determinate;
    return Row(
      children: [
        Expanded(
          child: Text(
            determinate ? '任务进度' : '正在处理',
            style: const TextStyle(
              color: AppColors.textSecondary,
              fontSize: 10,
            ),
          ),
        ),
        determinate
            ? _ProgressRing(
                progress: record.normalizedProgress ?? 0,
                color: _statusColor(record.status),
                size: 48,
              )
            : SizedBox(
                width: 42,
                height: 42,
                child: CircularProgressIndicator(
                  strokeWidth: 4,
                  color: _statusColor(record.status),
                  backgroundColor: const Color(0xFFE6EDF7),
                ),
              ),
      ],
    );
  }
}

class _StageNotificationProgress extends StatelessWidget {
  final AppNotificationRecord record;

  const _StageNotificationProgress({required this.record});

  @override
  Widget build(BuildContext context) {
    if (record.stages.isEmpty) return const SizedBox.shrink();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _StageRail(stages: record.stages),
        const SizedBox(height: 8),
        _LinearNotificationProgress(record: record),
        const SizedBox(height: 7),
        Text(
          _activeStageCaption(
            record.stages,
            progress: record.normalizedProgress,
          ),
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: const TextStyle(color: AppColors.textMuted, fontSize: 9),
        ),
      ],
    );
  }
}

class _StageRail extends StatelessWidget {
  final List<AppNotificationStage> stages;

  const _StageRail({required this.stages});

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (var index = 0; index < stages.length; index++)
          Expanded(
            child: _StageRailItem(
              stage: stages[index],
              first: index == 0,
              last: index == stages.length - 1,
              previousCompleted: index > 0 && stages[index - 1].completed,
            ),
          ),
      ],
    );
  }
}

class _StageRailItem extends StatelessWidget {
  final AppNotificationStage stage;
  final bool first;
  final bool last;
  final bool previousCompleted;

  const _StageRailItem({
    required this.stage,
    required this.first,
    required this.last,
    required this.previousCompleted,
  });

  @override
  Widget build(BuildContext context) {
    final dotColor = stage.completed
        ? AppColors.statusSuccess
        : stage.active
        ? AppColors.primary
        : const Color(0xFFC8D5E7);
    return Column(
      children: [
        SizedBox(
          height: 10,
          child: Stack(
            alignment: Alignment.topCenter,
            children: [
              if (!first)
                Positioned(
                  top: 3,
                  left: 0,
                  right: 0.5 * 10,
                  child: Container(
                    height: 1,
                    color: previousCompleted
                        ? const Color(0xFF86D8A3)
                        : const Color(0xFFDBE5F2),
                  ),
                ),
              if (!last)
                Positioned(
                  top: 3,
                  left: 0.5 * 10,
                  right: 0,
                  child: Container(
                    height: 1,
                    color: stage.completed
                        ? const Color(0xFF86D8A3)
                        : const Color(0xFFDBE5F2),
                  ),
                ),
              Container(
                width: 8,
                height: 8,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: dotColor,
                  border: Border.all(color: AppColors.surface, width: 2),
                  boxShadow: stage.active
                      ? const [
                          BoxShadow(color: Color(0xFFBFDBFE), spreadRadius: 2),
                        ]
                      : null,
                ),
              ),
            ],
          ),
        ),
        const SizedBox(height: 3),
        Text(
          stage.label,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          textAlign: TextAlign.center,
          style: TextStyle(
            color: stage.active ? AppColors.primary : AppColors.textMuted,
            fontSize: 8,
            fontWeight: stage.active ? FontWeight.w700 : FontWeight.w400,
          ),
        ),
      ],
    );
  }
}

class _SyncNotificationProgress extends StatelessWidget {
  final AppNotificationRecord record;

  const _SyncNotificationProgress({required this.record});

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        const SizedBox(width: 40),
        _SpinningIcon(
          icon: record.status == AppNotificationStatus.waitingSync
              ? Icons.cloud_sync_outlined
              : Icons.sync_rounded,
        ),
        const SizedBox(width: 8),
        Expanded(
          child: Text(
            record.status == AppNotificationStatus.waitingSync
                ? '等待同步'
                : '正在处理',
            style: const TextStyle(
              color: AppColors.textSecondary,
              fontSize: 10,
            ),
          ),
        ),
        Text(
          _relativeTime(record.updatedAt),
          style: const TextStyle(color: AppColors.textMuted, fontSize: 9),
        ),
      ],
    );
  }
}

class _DotsNotificationProgress extends StatelessWidget {
  final AppNotificationRecord record;

  const _DotsNotificationProgress({required this.record});

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        const SizedBox(width: 40),
        Expanded(
          child: Text(
            record.status == AppNotificationStatus.waitingSync
                ? '等待同步'
                : '正在处理',
            style: const TextStyle(
              color: AppColors.textSecondary,
              fontSize: 10,
            ),
          ),
        ),
        const _BouncingDots(),
        const SizedBox(width: 4),
        Text(
          _relativeTime(record.updatedAt),
          style: const TextStyle(color: AppColors.textMuted, fontSize: 9),
        ),
      ],
    );
  }
}

class _NotificationIsland extends StatelessWidget {
  final List<AppNotificationRecord> records;
  final VoidCallback onTap;
  final VoidCallback onDismiss;

  const _NotificationIsland({
    required this.records,
    required this.onTap,
    required this.onDismiss,
  });

  @override
  Widget build(BuildContext context) {
    final primary = records.first;
    final progress = primary.normalizedProgress;
    return Material(
      color: Colors.transparent,
      child: Semantics(
        label: '${primary.title}，展开通知',
        button: true,
        child: InkWell(
          onTap: onTap,
          customBorder: const CircleBorder(),
          child: SizedBox(
            key: const ValueKey('collapsed-notification-button'),
            width: 56,
            height: 56,
            child: Stack(
              alignment: Alignment.center,
              clipBehavior: Clip.none,
              children: [
                Container(
                  width: 56,
                  height: 56,
                  decoration: BoxDecoration(
                    color: AppColors.surface,
                    shape: BoxShape.circle,
                    border: Border.all(color: _statusColor(primary.status)),
                    boxShadow: const [
                      BoxShadow(
                        color: Color(0x261D3865),
                        blurRadius: 18,
                        offset: Offset(0, 7),
                      ),
                    ],
                  ),
                  child: Stack(
                    alignment: Alignment.center,
                    children: [
                      if (progress != null &&
                          primary.progressMode ==
                              AppNotificationProgressMode.determinate)
                        _ProgressRing(
                          key: const ValueKey('collapsed-notification-progress'),
                          progress: progress,
                          color: _statusColor(primary.status),
                          size: 43,
                        ),
                      _NotificationIcon(record: primary, size: 27),
                    ],
                  ),
                ),
                if (records.length > 1)
                  Positioned(
                    top: -3,
                    right: -3,
                    child: Container(
                      constraints: const BoxConstraints(minWidth: 19),
                      height: 19,
                      padding: const EdgeInsets.symmetric(horizontal: 5),
                      alignment: Alignment.center,
                      decoration: const BoxDecoration(
                        color: AppColors.primary,
                        shape: BoxShape.circle,
                      ),
                      child: Text(
                        records.length > 9 ? '9+' : '${records.length}',
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 9,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    ),
                  ),
                Positioned(
                  right: -6,
                  bottom: -6,
                  child: Semantics(
                    label: '关闭通知',
                    button: true,
                    child: Material(
                      color: AppColors.surface,
                      shape: const CircleBorder(),
                      child: InkResponse(
                        onTap: onDismiss,
                        containedInkWell: true,
                        customBorder: const CircleBorder(),
                        child: const SizedBox(
                          width: 28,
                          height: 28,
                          child: Icon(
                            Icons.close_rounded,
                            size: 14,
                            color: AppColors.textMuted,
                          ),
                        ),
                      ),
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
}

class _NotificationCenterOverlay extends StatelessWidget {
  final AppNotificationState state;
  final bool mobile;
  final VoidCallback onClose;
  final ValueChanged<AppNotificationCenterTab> onTabChanged;
  final VoidCallback onClearRecent;
  final ValueChanged<AppNotificationRecord> onDismiss;
  final void Function(
    AppNotificationRecord record,
    AppNotificationAction action,
  )
  onAction;

  const _NotificationCenterOverlay({
    required this.state,
    required this.mobile,
    required this.onClose,
    required this.onTabChanged,
    required this.onClearRecent,
    required this.onDismiss,
    required this.onAction,
  });

  @override
  Widget build(BuildContext context) {
    final panel = _NotificationCenterPanel(
      state: state,
      onClose: onClose,
      onTabChanged: onTabChanged,
      onClearRecent: onClearRecent,
      onDismiss: onDismiss,
      onAction: onAction,
    );
    return Positioned.fill(
      child: Material(
        color: const Color(0x47111727),
        child: GestureDetector(
          behavior: HitTestBehavior.opaque,
          onTap: onClose,
          child: Align(
            alignment: mobile ? Alignment.bottomCenter : Alignment.centerRight,
            child: GestureDetector(
              onTap: () {},
              child: SizedBox(
                width: mobile ? double.infinity : 400,
                height: mobile
                    ? math.min(MediaQuery.sizeOf(context).height * 0.82, 680)
                    : double.infinity,
                child: panel,
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _NotificationCenterPanel extends StatelessWidget {
  final AppNotificationState state;
  final VoidCallback onClose;
  final ValueChanged<AppNotificationCenterTab> onTabChanged;
  final VoidCallback onClearRecent;
  final ValueChanged<AppNotificationRecord> onDismiss;
  final void Function(
    AppNotificationRecord record,
    AppNotificationAction action,
  )
  onAction;

  const _NotificationCenterPanel({
    required this.state,
    required this.onClose,
    required this.onTabChanged,
    required this.onClearRecent,
    required this.onDismiss,
    required this.onAction,
  });

  @override
  Widget build(BuildContext context) {
    final mobile = MediaQuery.sizeOf(context).width < 600;
    final records = state.centerTab == AppNotificationCenterTab.active
        ? state.active
        : state.recent;
    return Material(
      color: AppColors.surface,
      borderRadius: mobile
          ? const BorderRadius.vertical(top: Radius.circular(12))
          : BorderRadius.zero,
      clipBehavior: Clip.antiAlias,
      child: Column(
        children: [
          SizedBox(
            height: 58,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(17, 0, 10, 0),
              child: Row(
                children: [
                  const Text(
                    '通知',
                    style: TextStyle(
                      color: AppColors.textPrimary,
                      fontSize: 15,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  const SizedBox(width: 7),
                  Text(
                    '${state.unreadCount} 条未读',
                    style: const TextStyle(
                      color: AppColors.textMuted,
                      fontSize: 10,
                    ),
                  ),
                  const Spacer(),
                  Semantics(
                    label: '关闭通知中心',
                    button: true,
                    child: IconButton(
                      onPressed: onClose,
                      icon: const Icon(Icons.close_rounded, size: 19),
                    ),
                  ),
                ],
              ),
            ),
          ),
          Container(height: 1, color: AppColors.border),
          Padding(
            padding: const EdgeInsets.only(top: 10),
            child: SizedBox(
              height: 52,
              child: Row(
                children: [
                  Expanded(
                    child: _CenterTabButton(
                      id: 'active',
                      label: '进行中 ${state.active.length}',
                      selected:
                          state.centerTab == AppNotificationCenterTab.active,
                      onPressed: () =>
                          onTabChanged(AppNotificationCenterTab.active),
                    ),
                  ),
                  Expanded(
                    child: _CenterTabButton(
                      id: 'recent',
                      label: '最近记录',
                      selected:
                          state.centerTab == AppNotificationCenterTab.recent,
                      onPressed: () =>
                          onTabChanged(AppNotificationCenterTab.recent),
                    ),
                  ),
                ],
              ),
            ),
          ),
          Container(height: 1, color: AppColors.borderLight),
          Padding(
            padding: const EdgeInsets.fromLTRB(14, 12, 12, 5),
            child: Row(
              children: [
                Text(
                  state.centerTab == AppNotificationCenterTab.active
                      ? '当前任务'
                      : '最近完成',
                  style: const TextStyle(
                    color: AppColors.textMuted,
                    fontSize: 9,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const Spacer(),
                if (state.centerTab == AppNotificationCenterTab.recent &&
                    state.recent.isNotEmpty)
                  TextButton(
                    onPressed: onClearRecent,
                    style: TextButton.styleFrom(
                      minimumSize: const Size(44, 32),
                      padding: const EdgeInsets.symmetric(horizontal: 8),
                    ),
                    child: const Text('清除', style: TextStyle(fontSize: 9)),
                  ),
              ],
            ),
          ),
          Expanded(
            child: records.isEmpty
                ? _CenterEmpty(tab: state.centerTab)
                : ListView.separated(
                    padding: const EdgeInsets.symmetric(horizontal: 12),
                    itemCount: records.length,
                    separatorBuilder: (_, _) =>
                        Container(height: 1, color: AppColors.borderLight),
                    itemBuilder: (context, index) {
                      final record = records[index];
                      return _CenterRecord(
                        record: record,
                        onDismiss: () => onDismiss(record),
                        onAction: (action) => onAction(record, action),
                      );
                    },
                  ),
          ),
        ],
      ),
    );
  }
}

class _CenterTabButton extends StatelessWidget {
  final String id;
  final String label;
  final bool selected;
  final VoidCallback onPressed;

  const _CenterTabButton({
    required this.id,
    required this.label,
    required this.selected,
    required this.onPressed,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      key: ValueKey('notification-center-tab-$id'),
      onTap: onPressed,
      child: Center(
        child: IntrinsicWidth(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(
                label,
                key: ValueKey('notification-center-tab-label-$id'),
                style: TextStyle(
                  color: selected ? AppColors.primary : AppColors.textSecondary,
                  fontSize: 12,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 10),
              AnimatedContainer(
                key: ValueKey('notification-center-tab-indicator-$id'),
                duration: const Duration(milliseconds: 180),
                height: 2,
                width: double.infinity,
                color: selected ? AppColors.primary : Colors.transparent,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _CenterRecord extends StatelessWidget {
  final AppNotificationRecord record;
  final VoidCallback onDismiss;
  final ValueChanged<AppNotificationAction> onAction;

  const _CenterRecord({
    required this.record,
    required this.onDismiss,
    required this.onAction,
  });

  @override
  Widget build(BuildContext context) {
    final visibleActions = _visibleNotificationActions(record);
    final displayStyle = _resolvedDisplayStyle(record);
    return InkWell(
      onTap: visibleActions.isEmpty
          ? null
          : () => onAction(
              visibleActions.firstWhere(
                (action) => action.primary,
                orElse: () => visibleActions.first,
              ),
            ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 11),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _NotificationIcon(record: record, size: 28),
            const SizedBox(width: 9),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    record.title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      color: AppColors.textPrimary,
                      fontSize: 11,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  const SizedBox(height: 3),
                  Text(
                    record.message,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      color: AppColors.textSecondary,
                      fontSize: 9.5,
                      height: 1.45,
                    ),
                  ),
                  if (displayStyle == AppNotificationDisplayStyle.linear &&
                      record.progressMode !=
                          AppNotificationProgressMode.none) ...[
                    const SizedBox(height: 8),
                    ClipRRect(
                      borderRadius: BorderRadius.circular(99),
                      child: LinearProgressIndicator(
                        value:
                            record.progressMode ==
                                AppNotificationProgressMode.determinate
                            ? record.normalizedProgress ?? 0
                            : null,
                        minHeight: 3,
                        backgroundColor: const Color(0xFFE5EDF8),
                        color: _statusColor(record.status),
                      ),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(width: 8),
            Column(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                if (displayStyle == AppNotificationDisplayStyle.ring &&
                    record.progressMode ==
                        AppNotificationProgressMode.determinate)
                  _ProgressRing(
                    progress: record.normalizedProgress ?? 0,
                    color: _statusColor(record.status),
                    size: 28,
                  )
                else
                  Text(
                    displayStyle == AppNotificationDisplayStyle.linear &&
                            record.progressMode ==
                                AppNotificationProgressMode.determinate &&
                            record.normalizedProgress != null
                        ? '${(record.normalizedProgress! * 100).round()}%'
                        : _relativeTime(record.updatedAt),
                    style: const TextStyle(
                      color: AppColors.textMuted,
                      fontSize: 8,
                    ),
                  ),
                const SizedBox(height: 5),
                Semantics(
                  label: record.isTerminal ? '删除通知' : '隐藏通知',
                  button: true,
                  child: InkResponse(
                    onTap: onDismiss,
                    radius: 18,
                    child: const Padding(
                      padding: EdgeInsets.all(5),
                      child: Icon(
                        Icons.close_rounded,
                        size: 14,
                        color: AppColors.textMuted,
                      ),
                    ),
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

List<AppNotificationAction> _visibleNotificationActions(
  AppNotificationRecord record,
) {
  return record.actions
      .where(
        (action) =>
            action.type == AppNotificationActionType.openRoute ||
            action.type == AppNotificationActionType.openLogs,
      )
      .toList(growable: false);
}

AppNotificationDisplayStyle _resolvedDisplayStyle(
  AppNotificationRecord record,
) {
  final configured = record.displayStyle;
  if (record.progressMode == AppNotificationProgressMode.none) {
    return AppNotificationDisplayStyle.compact;
  }
  if (record.isTerminal &&
      (configured == AppNotificationDisplayStyle.sync ||
          configured == AppNotificationDisplayStyle.dots)) {
    return AppNotificationDisplayStyle.compact;
  }
  if (configured != AppNotificationDisplayStyle.automatic) return configured;
  if (record.stages.isNotEmpty) return AppNotificationDisplayStyle.stages;
  if (record.progressMode == AppNotificationProgressMode.determinate) {
    return AppNotificationDisplayStyle.linear;
  }
  return record.status == AppNotificationStatus.waitingSync
      ? AppNotificationDisplayStyle.sync
      : AppNotificationDisplayStyle.dots;
}

class _CenterEmpty extends StatelessWidget {
  final AppNotificationCenterTab tab;

  const _CenterEmpty({required this.tab});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(
            Icons.notifications_none_rounded,
            color: AppColors.textMuted,
            size: 28,
          ),
          const SizedBox(height: 9),
          Text(
            tab == AppNotificationCenterTab.active ? '当前没有进行中的任务' : '暂无最近通知',
            style: const TextStyle(color: AppColors.textMuted, fontSize: 11),
          ),
        ],
      ),
    );
  }
}

class _NotificationIcon extends StatelessWidget {
  final AppNotificationRecord record;
  final double size;

  const _NotificationIcon({required this.record, required this.size});

  @override
  Widget build(BuildContext context) {
    final color = _statusColor(record.status);
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: _statusBackground(record.status),
        borderRadius: BorderRadius.circular(7),
      ),
      child: Icon(
        record.isTerminal ? _statusIcon(record.status) : _kindIcon(record.kind),
        size: size * 0.53,
        color: color,
      ),
    );
  }
}

class _NotificationActionButton extends StatelessWidget {
  final AppNotificationAction action;
  final VoidCallback onPressed;

  const _NotificationActionButton({
    required this.action,
    required this.onPressed,
  });

  @override
  Widget build(BuildContext context) {
    if (action.primary) {
      return SizedBox(
        height: 30,
        child: FilledButton.icon(
          onPressed: onPressed,
          style: FilledButton.styleFrom(
            padding: const EdgeInsets.symmetric(horizontal: 10),
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(6),
            ),
            textStyle: const TextStyle(
              fontSize: 10,
              fontWeight: FontWeight.w700,
            ),
          ),
          icon: const Icon(Icons.arrow_outward_rounded, size: 13),
          label: Text(action.label),
        ),
      );
    }
    return SizedBox(
      height: 30,
      child: OutlinedButton(
        onPressed: onPressed,
        style: OutlinedButton.styleFrom(
          padding: const EdgeInsets.symmetric(horizontal: 9),
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(6)),
          textStyle: const TextStyle(fontSize: 10, fontWeight: FontWeight.w700),
        ),
        child: Text(action.label),
      ),
    );
  }
}

class _SmallIconButton extends StatelessWidget {
  final String tooltip;
  final IconData icon;
  final VoidCallback onPressed;

  const _SmallIconButton({
    required this.tooltip,
    required this.icon,
    required this.onPressed,
  });

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: tooltip,
      button: true,
      child: IconButton(
        onPressed: onPressed,
        constraints: const BoxConstraints.tightFor(width: 30, height: 30),
        padding: EdgeInsets.zero,
        visualDensity: VisualDensity.compact,
        icon: Icon(icon, size: 16, color: AppColors.textSecondary),
      ),
    );
  }
}

class _ProgressRing extends StatelessWidget {
  final double progress;
  final Color color;
  final double size;

  const _ProgressRing({
    super.key,
    required this.progress,
    required this.color,
    required this.size,
  });

  @override
  Widget build(BuildContext context) {
    return TweenAnimationBuilder<double>(
      tween: Tween(end: progress),
      duration: _motionDuration(context, 500),
      curve: Curves.easeOutCubic,
      builder: (context, value, _) => SizedBox(
        width: size,
        height: size,
        child: Stack(
          alignment: Alignment.center,
          children: [
            SizedBox(
              width: size,
              height: size,
              child: CircularProgressIndicator(
                value: value,
                strokeWidth: size < 32 ? 3 : 5,
                strokeCap: StrokeCap.round,
                color: color,
                backgroundColor: const Color(0xFFE5EDF8),
              ),
            ),
            Text(
              '${(value * 100).round()}${size < 32 ? '' : '%'}',
              style: TextStyle(
                color: AppColors.textPrimary,
                fontSize: size < 32 ? 7 : 10,
                fontWeight: FontWeight.w700,
                fontFeatures: const [FontFeature.tabularFigures()],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _SpinningIcon extends StatefulWidget {
  final IconData icon;

  const _SpinningIcon({required this.icon});

  @override
  State<_SpinningIcon> createState() => _SpinningIconState();
}

class _SpinningIconState extends State<_SpinningIcon>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1150),
    )..repeat();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (MediaQuery.of(context).disableAnimations) {
      return Icon(widget.icon, size: 15, color: AppColors.primary);
    }
    return RotationTransition(
      turns: _controller,
      child: Icon(widget.icon, size: 15, color: AppColors.primary),
    );
  }
}

class _BouncingDots extends StatefulWidget {
  const _BouncingDots();

  @override
  State<_BouncingDots> createState() => _BouncingDotsState();
}

class _BouncingDotsState extends State<_BouncingDots>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1100),
    )..repeat();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (MediaQuery.of(context).disableAnimations) {
      return const Text('...', style: TextStyle(color: AppColors.primary));
    }
    return AnimatedBuilder(
      animation: _controller,
      builder: (context, _) => Row(
        mainAxisSize: MainAxisSize.min,
        children: List.generate(3, (index) {
          final phase = (_controller.value - index * 0.12) % 1;
          final lift = math.sin(phase * math.pi * 2).clamp(0, 1).toDouble();
          return Transform.translate(
            offset: Offset(0, -3 * lift),
            child: Container(
              width: 4,
              height: 4,
              margin: const EdgeInsets.symmetric(horizontal: 1.5),
              decoration: BoxDecoration(
                color: AppColors.primary.withValues(alpha: 0.35 + lift * 0.65),
                shape: BoxShape.circle,
              ),
            ),
          );
        }),
      ),
    );
  }
}

Color _statusColor(AppNotificationStatus status) => switch (status) {
  AppNotificationStatus.succeeded => AppColors.statusSuccess,
  AppNotificationStatus.failed => AppColors.statusError,
  AppNotificationStatus.cancelled => AppColors.statusOffline,
  AppNotificationStatus.waitingSync => AppColors.statusWarning,
  _ => AppColors.primary,
};

Color _statusBackground(AppNotificationStatus status) => switch (status) {
  AppNotificationStatus.succeeded => AppColors.statusSuccessLight,
  AppNotificationStatus.failed => AppColors.statusErrorLight,
  AppNotificationStatus.cancelled => AppColors.statusOfflineLight,
  AppNotificationStatus.waitingSync => AppColors.statusWarningLight,
  _ => AppColors.primaryLight,
};

IconData _statusIcon(AppNotificationStatus status) => switch (status) {
  AppNotificationStatus.succeeded => Icons.check_circle_outline_rounded,
  AppNotificationStatus.failed => Icons.error_outline_rounded,
  AppNotificationStatus.cancelled => Icons.block_rounded,
  _ => Icons.notifications_none_rounded,
};

IconData _kindIcon(AppNotificationKind kind) => switch (kind) {
  AppNotificationKind.agent => Icons.smart_toy_outlined,
  AppNotificationKind.mcp => Icons.hub_outlined,
  AppNotificationKind.installation => Icons.system_update_alt_rounded,
  AppNotificationKind.file => Icons.description_outlined,
  AppNotificationKind.system => Icons.notifications_none_rounded,
};

String _activeStageCaption(
  List<AppNotificationStage> stages, {
  double? progress,
}) {
  if (stages.isEmpty) return '正在处理';
  final activeIndex = stages.indexWhere((stage) => stage.active);
  if (activeIndex < 0) {
    return stages.every((stage) => stage.completed) ? '全部阶段已完成' : '准备开始';
  }
  final progressText = progress == null
      ? ''
      : ' · 总进度 ${(progress * 100).round().clamp(0, 100)}%';
  return '阶段 ${activeIndex + 1}/${stages.length} · ${stages[activeIndex].label}$progressText';
}

String _relativeTime(DateTime timestamp) {
  final elapsed = DateTime.now().toUtc().difference(timestamp.toUtc());
  if (elapsed.inSeconds < 10) return '刚刚';
  if (elapsed.inMinutes < 1) return '${elapsed.inSeconds} 秒';
  if (elapsed.inHours < 1) return '${elapsed.inMinutes} 分钟';
  if (elapsed.inDays < 1) return '${elapsed.inHours} 小时';
  return '${elapsed.inDays} 天';
}

Duration _motionDuration(BuildContext context, int milliseconds) {
  return MediaQuery.of(context).disableAnimations
      ? Duration.zero
      : Duration(milliseconds: milliseconds);
}
