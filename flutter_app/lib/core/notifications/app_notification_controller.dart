import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../storage/app_storage.dart';
import '../services/global_overlay_service.dart';
import 'app_notification_model.dart';
import 'app_notification_redactor.dart';
import 'app_notification_repository.dart';
import 'app_notification_sync.dart';
import 'app_notification_lifecycle.dart';

class AppNotificationState {
  final List<AppNotificationRecord> active;
  final List<AppNotificationRecord> recent;
  final bool hydrated;
  final bool collapsed;
  final bool centerOpen;
  final AppNotificationCenterTab centerTab;
  final AppNotificationChatContext? foregroundChat;

  const AppNotificationState({
    this.active = const [],
    this.recent = const [],
    this.hydrated = false,
    this.collapsed = false,
    this.centerOpen = false,
    this.centerTab = AppNotificationCenterTab.active,
    this.foregroundChat,
  });

  int get unreadCount =>
      [...active, ...recent].where((item) => item.unread).length;

  AppNotificationState copyWith({
    List<AppNotificationRecord>? active,
    List<AppNotificationRecord>? recent,
    bool? hydrated,
    bool? collapsed,
    bool? centerOpen,
    AppNotificationCenterTab? centerTab,
    AppNotificationChatContext? foregroundChat,
    bool clearForegroundChat = false,
  }) {
    return AppNotificationState(
      active: active ?? this.active,
      recent: recent ?? this.recent,
      hydrated: hydrated ?? this.hydrated,
      collapsed: collapsed ?? this.collapsed,
      centerOpen: centerOpen ?? this.centerOpen,
      centerTab: centerTab ?? this.centerTab,
      foregroundChat: clearForegroundChat
          ? null
          : (foregroundChat ?? this.foregroundChat),
    );
  }
}

final appNotificationRepositoryProvider = Provider<AppNotificationRepository>(
  (_) => LocalAppNotificationRepository(),
);

final appNotificationControllerProvider =
    StateNotifierProvider<AppNotificationController, AppNotificationState>((
      ref,
    ) {
      final controller = AppNotificationController(
        repository: ref.watch(appNotificationRepositoryProvider),
        syncService: ref.watch(appNotificationSyncServiceProvider),
        hapticFeedback: HapticFeedback.lightImpact,
      );
      final authSubscription = AppStorage.authIdentityChanges.listen((_) {
        unawaited(controller.reloadForIdentity());
      });
      ref.onDispose(authSubscription.cancel);
      final nativeNotificationSubscription = GlobalOverlayService
          .notificationEvents
          .listen((event) {
            if (event['type'] != 'notification.updated') return;
            try {
              final raw = event['data'];
              final decoded = raw is String
                  ? (jsonDecode(raw) as Map?)
                  : (raw as Map?);
              if (decoded == null) return;
              controller.applyRemoteChange(
                AppNotificationRemoteChange.fromJson(
                  Map<String, dynamic>.from(decoded),
                ),
              );
            } catch (error) {
              // A malformed native event must not terminate the EventChannel.
              debugPrint('[NotificationBridge] invalid native event: $error');
            }
          });
      ref.onDispose(nativeNotificationSubscription.cancel);
      unawaited(controller.hydrate());
      // Initialize lifecycle manager for native notification reconciliation.
      ref.watch(appNotificationLifecycleManagerProvider);
      return controller;
    });

class AppNotificationController extends StateNotifier<AppNotificationState> {
  final AppNotificationRepository repository;
  final Duration collapseDelay;
  final Duration archiveDelay;
  final Duration persistenceDelay;
  final DateTime Function() now;
  final AppNotificationSyncService? syncService;
  final Future<void> Function()? hapticFeedback;

  Timer? _collapseTimer;
  Timer? _persistenceTimer;
  // Archive timers belong to an operation, not to a server record id. Synced
  // copies of one operation can legitimately receive a new record id.
  final Map<String, Timer> _archiveTimers = {};
  // Hiding an active notification must not dismiss the remote operation. Keep
  // this suppression across app restarts until the remote operation is done.
  final Set<String> _hiddenOperationIds = {};
  Future<void> _writeTail = Future.value();
  bool _disposed = false;
  int _hydrationGeneration = 0;

  AppNotificationController({
    required this.repository,
    this.collapseDelay = const Duration(seconds: 4),
    this.archiveDelay = const Duration(seconds: 3),
    this.persistenceDelay = const Duration(milliseconds: 250),
    this.syncService,
    this.hapticFeedback,
    DateTime Function()? now,
  }) : now = now ?? (() => DateTime.now().toUtc()),
       super(const AppNotificationState());

  Future<void> hydrate() async {
    final generation = ++_hydrationGeneration;
    final snapshot = await repository.load();
    if (_disposed || generation != _hydrationGeneration) return;
    _hiddenOperationIds
      ..clear()
      ..addAll(snapshot.hiddenOperationIds);
    final hadLocalChanges = state.active.isNotEmpty || state.recent.isNotEmpty;
    final recentByOperation = <String, AppNotificationRecord>{
      for (final item in snapshot.recent) item.operationId: item,
    };
    final activeByOperation = <String, AppNotificationRecord>{};
    for (final item in snapshot.active) {
      if (item.isTerminal) {
        recentByOperation[item.operationId] = item;
      } else {
        activeByOperation[item.operationId] = item;
      }
    }
    for (final item in state.recent) {
      activeByOperation.remove(item.operationId);
      recentByOperation[item.operationId] = item;
    }
    for (final item in state.active) {
      recentByOperation.remove(item.operationId);
      activeByOperation[item.operationId] = item;
    }
    final active = activeByOperation.values.toList();
    final recent = recentByOperation.values.toList();
    state = state.copyWith(
      active: _sortActive(active),
      recent: _trimRecent(recent),
      collapsed: hadLocalChanges
          ? state.collapsed
          : snapshot.collapsed || active.isNotEmpty,
      hydrated: true,
    );
    _schedulePersist();
    syncService?.start(applyRemoteChange);
  }

  Future<void> reloadForIdentity() async {
    if (_disposed) return;
    _hydrationGeneration++;
    _collapseTimer?.cancel();
    _persistenceTimer?.cancel();
    for (final timer in _archiveTimers.values) {
      timer.cancel();
    }
    _archiveTimers.clear();
    _hiddenOperationIds.clear();
    state = const AppNotificationState();
    await hydrate();
  }

  AppNotificationRecord start({
    required String operationId,
    required String title,
    required String message,
    AppNotificationProgressMode progressMode =
        AppNotificationProgressMode.indeterminate,
    AppNotificationDisplayStyle displayStyle =
        AppNotificationDisplayStyle.automatic,
    AppNotificationKind kind = AppNotificationKind.system,
    AppNotificationScope scope = AppNotificationScope.local,
    AppNotificationAttention attention = AppNotificationAttention.none,
    double? progress,
    List<AppNotificationStage> stages = const [],
    List<AppNotificationAction> actions = const [],
    Map<String, String>? metadata,
    String sourceLabel = '',
  }) {
    final timestamp = now();
    _hiddenOperationIds.remove(operationId);
    final existing = _findByOperationId(operationId);
    final restarting = existing?.isTerminal == true;
    final record = AppNotificationRecord(
      id: restarting
          ? _newId(operationId, timestamp)
          : existing?.id ?? _newId(operationId, timestamp),
      operationId: operationId,
      title: AppNotificationRedactor.text(title),
      message: AppNotificationRedactor.text(message),
      status: AppNotificationStatus.running,
      progressMode: progressMode,
      displayStyle: displayStyle,
      kind: kind,
      scope: scope,
      attention: attention,
      progress: progress,
      stages: stages,
      actions: actions,
      metadata: metadata ?? existing?.metadata ?? const {},
      sourceLabel: AppNotificationRedactor.text(sourceLabel),
      createdAt: restarting ? timestamp : existing?.createdAt ?? timestamp,
      updatedAt: timestamp,
    );
    upsert(record, force: existing?.isTerminal == true);
    return record;
  }

  void update({
    required String operationId,
    String? title,
    String? message,
    AppNotificationStatus? status,
    AppNotificationProgressMode? progressMode,
    AppNotificationDisplayStyle? displayStyle,
    double? progress,
    List<AppNotificationStage>? stages,
    List<AppNotificationAction>? actions,
    Map<String, String>? metadata,
    String? errorCode,
    AppNotificationAttention? attention,
  }) {
    final current = _findByOperationId(operationId);
    if (current == null) return;
    final nextStatus = status ?? current.status;
    final timestamp = now();
    upsert(
      current.copyWith(
        title: title == null ? null : AppNotificationRedactor.text(title),
        message: message == null ? null : AppNotificationRedactor.text(message),
        status: nextStatus,
        progressMode: progressMode,
        displayStyle: displayStyle,
        progress: progress ?? current.progress,
        stages: stages,
        actions: actions,
        metadata: metadata,
        errorCode: errorCode,
        attention: attention,
        updatedAt: timestamp,
        completedAt: _isTerminal(nextStatus)
            ? (current.isTerminal
                  ? current.completedAt ?? timestamp
                  : timestamp)
            : null,
        unread: true,
      ),
      // Progress updates must never undo a collapse chosen by the user.
      expand: false,
    );
  }

  void succeed(
    String operationId, {
    String? title,
    String message = '操作已完成',
    List<AppNotificationAction>? actions,
  }) {
    final current = _findByOperationId(operationId);
    update(
      operationId: operationId,
      title: title,
      message: message,
      status: AppNotificationStatus.succeeded,
      progressMode: AppNotificationProgressMode.determinate,
      progress: 1,
      stages: current?.stages
          .map(
            (stage) =>
                AppNotificationStage(label: stage.label, completed: true),
          )
          .toList(growable: false),
      actions: actions,
    );
  }

  void fail(
    String operationId, {
    String? title,
    required Object error,
    String errorCode = '',
    List<AppNotificationAction>? actions,
    bool critical = false,
  }) {
    update(
      operationId: operationId,
      title: title,
      message: AppNotificationRedactor.text(error.toString()),
      status: AppNotificationStatus.failed,
      errorCode: errorCode,
      actions: actions,
      attention: critical ? AppNotificationAttention.critical : null,
    );
  }

  void cancel(String operationId, {String message = '操作已取消'}) {
    update(
      operationId: operationId,
      message: message,
      status: AppNotificationStatus.cancelled,
    );
  }

  void waitForSync(String operationId, {String message = '等待同步'}) {
    final current = _findByOperationId(operationId);
    update(
      operationId: operationId,
      message: message,
      status: AppNotificationStatus.waitingSync,
      progressMode:
          current?.progressMode == AppNotificationProgressMode.determinate
          ? AppNotificationProgressMode.determinate
          : AppNotificationProgressMode.indeterminate,
    );
  }

  void upsert(
    AppNotificationRecord record, {
    bool force = false,
    bool synchronize = true,
    bool expand = true,
  }) {
    final existing = _findByOperationId(record.operationId);
    if (!force &&
        existing != null &&
        !_isValidTransition(existing.status, record.status)) {
      return;
    }

    // Repeated terminal sync updates must not postpone the archive deadline.
    if (existing?.isTerminal == true &&
        record.isTerminal &&
        existing!.completedAt != null) {
      record = record.copyWith(completedAt: existing.completedAt);
    }

    if (_hiddenOperationIds.contains(record.operationId) &&
        !record.isTerminal) {
      return;
    }
    if (record.isTerminal) {
      _hiddenOperationIds.remove(record.operationId);
    }

    final isNewOperation = existing == null;
    final isRestartedOperation = force && existing?.isTerminal == true;
    final shouldExpand = expand && (isNewOperation || isRestartedOperation);

    _maybeSignalAttention(existing, record);
    if (isRestartedOperation) {
      _archiveTimers.remove(record.operationId)?.cancel();
    }
    final active = [...state.active]
      ..removeWhere(
        (item) =>
            item.id == record.id || item.operationId == record.operationId,
      );
    final recent = [...state.recent]
      ..removeWhere(
        (item) =>
            item.id == record.id || item.operationId == record.operationId,
      );
    active.add(record);
    state = state.copyWith(
      active: _sortActive(active),
      recent: _trimRecent(recent),
      collapsed: shouldExpand ? false : state.collapsed,
    );
    if (shouldExpand) _scheduleCollapse();
    if (record.isTerminal) _scheduleArchive(record.operationId);
    _schedulePersist();
    if (synchronize && record.scope == AppNotificationScope.synced) {
      syncService?.queueUpsert(record);
    }
  }

  void applyRemoteChange(AppNotificationRemoteChange change) {
    if (_disposed || change.operationId.isEmpty) return;
    if (change.deleted) {
      _hiddenOperationIds.remove(change.operationId);
      _removeWithoutSync(change.operationId);
      return;
    }
    final record = change.record;
    if (record == null ||
        record.scope != AppNotificationScope.synced ||
        record.operationId != change.operationId) {
      return;
    }
    if (_hiddenOperationIds.contains(change.operationId)) {
      if (!record.isTerminal) return;
      _hiddenOperationIds.remove(change.operationId);
    }
    final existing = _findByOperationId(change.operationId);
    if (existing != null && record.updatedAt.isBefore(existing.updatedAt)) {
      return;
    }
    final activeIndex = state.active.indexWhere(
      (item) => item.operationId == change.operationId,
    );
    final recentIndex = state.recent.indexWhere(
      (item) => item.operationId == change.operationId,
    );
    if (recentIndex >= 0 || (record.isTerminal && !record.unread)) {
      _archiveTimers.remove(change.operationId)?.cancel();
      final active = [...state.active]
        ..removeWhere((item) => item.operationId == change.operationId);
      final recent = [...state.recent]
        ..removeWhere((item) => item.operationId == change.operationId)
        ..add(record);
      state = state.copyWith(
        active: _sortActive(active),
        recent: _trimRecent(recent),
        collapsed: active.isEmpty ? false : state.collapsed,
      );
      _schedulePersist();
      return;
    }
    if (activeIndex >= 0 || !record.isTerminal || record.unread) {
      final needsRestart =
          existing != null &&
          !_isValidTransition(existing.status, record.status);
      if (needsRestart && record.id == existing.id) return;
      // A remote update refreshes the existing operation. It must not reopen a
      // notification the user already collapsed, especially when an older run
      // arrives with a different record id.
      upsert(
        record,
        force: needsRestart,
        synchronize: false,
        expand: existing == null,
      );
    }
  }

  void collapse() {
    _collapseTimer?.cancel();
    state = state.copyWith(collapsed: true);
    _schedulePersist();
  }

  void expand() {
    if (state.active.isEmpty) return;
    state = state.copyWith(collapsed: false);
    _scheduleCollapse();
    _schedulePersist();
  }

  void openCenter({AppNotificationCenterTab? tab}) {
    _collapseTimer?.cancel();
    AppNotificationRecord markRead(AppNotificationRecord item) =>
        item.unread ? item.copyWith(unread: false) : item;
    state = state.copyWith(
      centerOpen: true,
      centerTab:
          tab ??
          (state.active.isEmpty
              ? AppNotificationCenterTab.recent
              : AppNotificationCenterTab.active),
      active: state.active.map(markRead).toList(growable: false),
      recent: state.recent.map(markRead).toList(growable: false),
    );
    _schedulePersist();
    syncService?.queueRead(
      [...state.active, ...state.recent]
          .where((item) => item.scope == AppNotificationScope.synced)
          .map((item) => item.operationId),
    );
  }

  void closeCenter() {
    state = state.copyWith(centerOpen: false);
    if (state.active.isNotEmpty && !state.collapsed) {
      _scheduleCollapse();
    }
    _schedulePersist();
  }

  void markRead(String operationId) {
    final normalized = operationId.trim();
    if (normalized.isEmpty) return;
    AppNotificationRecord mark(AppNotificationRecord item) {
      return item.operationId == normalized && item.unread
          ? item.copyWith(unread: false)
          : item;
    }

    state = state.copyWith(
      active: state.active.map(mark).toList(growable: false),
      recent: state.recent.map(mark).toList(growable: false),
    );
    _schedulePersist();
    final record = [
      ...state.active,
      ...state.recent,
    ].where((item) => item.operationId == normalized).firstOrNull;
    if (record?.scope == AppNotificationScope.synced) {
      syncService?.queueRead([normalized]);
    }
  }

  void selectCenterTab(AppNotificationCenterTab tab) {
    state = state.copyWith(centerTab: tab);
  }

  void dismiss(String id) {
    AppNotificationRecord? record;
    for (final item in [...state.active, ...state.recent]) {
      if (item.id == id) {
        record = item;
        break;
      }
    }
    _archiveTimers.remove(record?.operationId)?.cancel();
    state = state.copyWith(
      active: state.active.where((item) => item.id != id).toList(),
      recent: state.recent.where((item) => item.id != id).toList(),
    );
    _schedulePersist();
    if (record?.scope == AppNotificationScope.synced) {
      syncService?.queueDismiss([record!.operationId]);
    }
  }

  void dismissOperation(String operationId) {
    final normalized = operationId.trim();
    if (normalized.isEmpty) return;
    final record = _findByOperationId(normalized);
    if (record == null) return;
    dismiss(record.id);
  }

  /// Hides an active notification locally while leaving the remote operation
  /// and its later terminal result intact.
  void hideOperation(String operationId) {
    final normalized = operationId.trim();
    if (normalized.isEmpty) return;
    final record = _findByOperationId(normalized);
    if (record == null || record.isTerminal) return;
    _hiddenOperationIds.add(normalized);
    _archiveTimers.remove(normalized)?.cancel();
    final active = state.active
        .where((item) => item.operationId != normalized)
        .toList(growable: false);
    state = state.copyWith(
      active: active,
      recent: state.recent
          .where((item) => item.operationId != normalized)
          .toList(growable: false),
      collapsed: active.isEmpty ? false : state.collapsed,
    );
    _schedulePersist();
  }

  /// Moves a completed operation out of the transient notification surface
  /// while keeping it available from the notification history.
  bool archiveCompletedOperation(String operationId) {
    final normalized = operationId.trim();
    if (normalized.isEmpty) return false;
    final index = state.active.indexWhere(
      (item) => item.operationId == normalized,
    );
    if (index < 0) return false;

    final record = state.active[index];
    if (!record.isTerminal) return false;

    _archiveTimers.remove(normalized)?.cancel();
    final active = [...state.active]..removeAt(index);
    state = state.copyWith(
      active: active,
      recent: _trimRecent([record, ...state.recent]),
      collapsed: active.isEmpty ? false : state.collapsed,
    );
    _schedulePersist();
    return true;
  }

  void clearRecent() {
    final syncedOperationIds = state.recent
        .where((item) => item.scope == AppNotificationScope.synced)
        .map((item) => item.operationId)
        .toList(growable: false);
    state = state.copyWith(recent: const []);
    _schedulePersist();
    syncService?.queueDismiss(syncedOperationIds);
  }

  Future<void> flush() async {
    _persistenceTimer?.cancel();
    await _persist();
    await _writeTail;
  }

  AppNotificationRecord? findByOperationId(String operationId) {
    return _findByOperationId(operationId);
  }

  void setForegroundChat(AppNotificationChatContext context) {
    if (state.foregroundChat?.sameAs(context) == true) return;
    state = state.copyWith(foregroundChat: context);
  }

  void clearForegroundChat(AppNotificationChatContext context) {
    if (state.foregroundChat?.sameAs(context) != true) return;
    state = state.copyWith(clearForegroundChat: true);
  }

  AppNotificationRecord? _findByOperationId(String operationId) {
    for (final item in [...state.active, ...state.recent]) {
      if (item.operationId == operationId) return item;
    }
    return null;
  }

  void _removeWithoutSync(String operationId) {
    _hiddenOperationIds.remove(operationId);
    _archiveTimers.remove(operationId)?.cancel();
    final active = state.active
        .where((item) => item.operationId != operationId)
        .toList(growable: false);
    state = state.copyWith(
      active: active,
      recent: state.recent
          .where((item) => item.operationId != operationId)
          .toList(growable: false),
      collapsed: active.isEmpty ? false : state.collapsed,
    );
    _schedulePersist();
  }

  void _maybeSignalAttention(
    AppNotificationRecord? existing,
    AppNotificationRecord record,
  ) {
    final shouldSignal = switch (record.attention) {
      AppNotificationAttention.none => false,
      AppNotificationAttention.critical =>
        record.status == AppNotificationStatus.failed &&
            (existing?.status != AppNotificationStatus.failed ||
                existing?.attention != AppNotificationAttention.critical),
      AppNotificationAttention.userAction =>
        existing?.attention != AppNotificationAttention.userAction,
    };
    if (shouldSignal && hapticFeedback != null) {
      unawaited(hapticFeedback!().catchError((_) {}));
    }
  }

  void _scheduleCollapse() {
    _collapseTimer?.cancel();
    _collapseTimer = Timer(collapseDelay, () {
      if (!_disposed && state.active.isNotEmpty && !state.centerOpen) {
        collapse();
      }
    });
  }

  void _scheduleArchive(String operationId) {
    if (_archiveTimers.containsKey(operationId)) return;
    _archiveTimers[operationId] = Timer(archiveDelay, () {
      if (_disposed) return;
      _archiveTimers.remove(operationId);
      archiveCompletedOperation(operationId);
    });
  }

  void _schedulePersist() {
    _persistenceTimer?.cancel();
    _persistenceTimer = Timer(persistenceDelay, _persist);
  }

  Future<void> _persist() async {
    if (_disposed) return;
    final generation = _hydrationGeneration;
    final snapshot = AppNotificationSnapshot(
      active: state.active,
      recent: state.recent,
      hiddenOperationIds: _hiddenOperationIds.toList(growable: false),
      collapsed: state.collapsed,
    );
    _writeTail = _writeTail
        .catchError((_) {})
        .then((_) async {
          if (_disposed || generation != _hydrationGeneration) return;
          await repository.save(snapshot);
        })
        .catchError((_) {});
    await _writeTail;
  }

  List<AppNotificationRecord> _sortActive(List<AppNotificationRecord> records) {
    records.sort((a, b) => b.updatedAt.compareTo(a.updatedAt));
    return List.unmodifiable(records);
  }

  List<AppNotificationRecord> _trimRecent(List<AppNotificationRecord> records) {
    records.sort((a, b) => b.updatedAt.compareTo(a.updatedAt));
    return List.unmodifiable(records.take(math.min(records.length, 50)));
  }

  bool _isValidTransition(
    AppNotificationStatus from,
    AppNotificationStatus to,
  ) {
    if (from == to) return true;
    if (_isTerminal(from)) return false;
    return switch (from) {
      AppNotificationStatus.pending => true,
      AppNotificationStatus.running ||
      AppNotificationStatus.waitingSync => to != AppNotificationStatus.pending,
      _ => false,
    };
  }

  bool _isTerminal(AppNotificationStatus status) => switch (status) {
    AppNotificationStatus.succeeded ||
    AppNotificationStatus.failed ||
    AppNotificationStatus.cancelled => true,
    _ => false,
  };

  String _newId(String operationId, DateTime timestamp) {
    final normalized = operationId.replaceAll(RegExp(r'[^a-zA-Z0-9_-]'), '_');
    return '${normalized}_${timestamp.microsecondsSinceEpoch}';
  }

  @override
  void dispose() {
    _disposed = true;
    _collapseTimer?.cancel();
    _persistenceTimer?.cancel();
    for (final timer in _archiveTimers.values) {
      timer.cancel();
    }
    _archiveTimers.clear();
    super.dispose();
  }
}
