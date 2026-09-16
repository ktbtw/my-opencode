import 'dart:async';
import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../config/api_client.dart';
import '../storage/app_storage.dart';
import 'app_notification_model.dart';

class AppNotificationRemoteChange {
  final int version;
  final String operationId;
  final AppNotificationRecord? record;
  final bool deleted;

  const AppNotificationRemoteChange({
    required this.version,
    required this.operationId,
    this.record,
    this.deleted = false,
  });

  factory AppNotificationRemoteChange.fromJson(Map<String, dynamic> json) {
    final rawRecord = json['record'];
    return AppNotificationRemoteChange(
      version: (json['version'] as num?)?.toInt() ?? 0,
      operationId: json['operation_id']?.toString() ?? '',
      record: rawRecord is Map
          ? AppNotificationRecord.fromJson(rawRecord.cast<String, dynamic>())
          : null,
      deleted: json['deleted'] as bool? ?? false,
    );
  }
}

class AppNotificationRemotePage {
  final List<AppNotificationRemoteChange> items;
  final int cursor;

  const AppNotificationRemotePage({required this.items, required this.cursor});
}

/// 同步请求的超时时间。
///
/// [AppNotificationSyncService] 把所有同步串在一条链上，任何一个请求挂住
/// 都会让后续同步（包括上传成功/失败这类终态）永远排不到，所以这里必须
/// 保证每次调用都会结束。
const Duration _syncRequestTimeout = Duration(seconds: 20);

abstract class AppNotificationRemoteGateway {
  Future<AppNotificationRemoteChange> upsert(AppNotificationRecord record);
  Future<AppNotificationRemotePage> list({required int after, int limit = 200});
  Future<void> markRead(List<String> operationIds);
  Future<void> dismiss(List<String> operationIds);
}

class ApiAppNotificationRemoteGateway implements AppNotificationRemoteGateway {
  @override
  Future<AppNotificationRemoteChange> upsert(
    AppNotificationRecord record,
  ) async {
    final json = await ApiClient.post(
      '/api/notifications/sync',
      {'record': record.toJson()},
      timeout: _syncRequestTimeout,
    );
    return AppNotificationRemoteChange.fromJson(
      (json['change'] as Map).cast<String, dynamic>(),
    );
  }

  @override
  Future<AppNotificationRemotePage> list({
    required int after,
    int limit = 200,
  }) async {
    final uri = Uri(
      path: '/api/notifications',
      queryParameters: {'after': '$after', 'limit': '$limit'},
    );
    final json = await ApiClient.get(
      uri.toString(),
      timeout: _syncRequestTimeout,
    );
    final items = (json['items'] as List<dynamic>? ?? const [])
        .whereType<Map>()
        .map(
          (item) => AppNotificationRemoteChange.fromJson(
            item.cast<String, dynamic>(),
          ),
        )
        .where((item) => item.version > 0 && item.operationId.isNotEmpty)
        .toList(growable: false);
    return AppNotificationRemotePage(
      items: items,
      cursor: (json['cursor'] as num?)?.toInt() ?? after,
    );
  }

  @override
  Future<void> markRead(List<String> operationIds) async {
    await ApiClient.post(
      '/api/notifications/read',
      {'operation_ids': operationIds},
      timeout: _syncRequestTimeout,
    );
  }

  @override
  Future<void> dismiss(List<String> operationIds) async {
    await ApiClient.post(
      '/api/notifications/dismiss',
      {'operation_ids': operationIds},
      timeout: _syncRequestTimeout,
    );
  }
}

final appNotificationRemoteGatewayProvider =
    Provider<AppNotificationRemoteGateway>(
      (_) => ApiAppNotificationRemoteGateway(),
    );

final appNotificationSyncServiceProvider = Provider<AppNotificationSyncService>(
  (ref) {
    final service = AppNotificationSyncService(
      gateway: ref.watch(appNotificationRemoteGatewayProvider),
    );
    ref.onDispose(service.dispose);
    return service;
  },
);

class AppNotificationSyncService {
  final AppNotificationRemoteGateway gateway;
  final Duration pushDelay;

  /// Kept for source compatibility with callers of the former polling API.
  final Duration? pollInterval;

  final Map<String, AppNotificationRecord> _pendingUpserts = {};
  final Set<String> _pendingRead = {};
  final Set<String> _pendingDismiss = {};
  Timer? _pushTimer;
  Future<void> _syncTail = Future.value();
  void Function(AppNotificationRemoteChange change)? _onChange;
  String _identity = '';
  int _cursor = 0;
  bool _started = false;
  bool _disposed = false;

  AppNotificationSyncService({
    required this.gateway,
    this.pushDelay = const Duration(milliseconds: 500),
    this.pollInterval,
  });

  void start(void Function(AppNotificationRemoteChange change) onChange) {
    _onChange = onChange;
    if (_started) return;
    _started = true;
    unawaited(syncNow());
  }

  void enableSSE() {
    if (_disposed || !_started) return;
    // Native Android service owns the only live background SSE connection.
    // Foreground Flutter refreshes from the durable REST feed instead.
    unawaited(syncNow());
  }

  void disableSSE() {
    if (_disposed || !_started) return;
    // Kept as a lifecycle compatibility hook. There is no Flutter SSE to stop.
  }

  void queueUpsert(AppNotificationRecord record) {
    if (record.scope != AppNotificationScope.synced ||
        _disposed ||
        !_prepareQueueIdentity()) {
      return;
    }
    _pendingDismiss.remove(record.operationId);
    _pendingUpserts[record.operationId] = record;
    _schedulePush();
  }

  void queueRead(Iterable<String> operationIds) {
    if (_disposed || !_prepareQueueIdentity()) return;
    _pendingRead.addAll(operationIds.where((id) => id.isNotEmpty));
    _schedulePush();
  }

  void queueDismiss(Iterable<String> operationIds) {
    if (_disposed || !_prepareQueueIdentity()) return;
    for (final operationId in operationIds.where((id) => id.isNotEmpty)) {
      _pendingUpserts.remove(operationId);
      _pendingRead.remove(operationId);
      _pendingDismiss.add(operationId);
    }
    _schedulePush();
  }

  void _schedulePush() {
    _pushTimer?.cancel();
    _pushTimer = Timer(pushDelay, () => unawaited(syncNow()));
  }

  Future<void> syncNow() async {
    if (_disposed) return;
    _syncTail = _syncTail.catchError((_) {}).then((_) => _syncOnce());
    await _syncTail;
  }

  Future<void> _syncOnce() async {
    if (!AppStorage.initialized || !AppStorage.isLoggedIn()) return;
    _loadCursorForCurrentIdentity();

    try {
      await _pushPending();
      await _pullChanges();
    } catch (_) {
      // Pending state remains queued and retry will happen
    }
  }

  void _loadCursorForCurrentIdentity() {
    final identity = _currentIdentity();
    if (identity == _identity) return;
    _clearPending();
    _identity = identity;
    _cursor =
        int.tryParse(AppStorage.getString(_cursorKey(identity)) ?? '') ?? 0;
  }

  bool _prepareQueueIdentity() {
    if (!AppStorage.initialized || !AppStorage.isLoggedIn()) return false;
    _loadCursorForCurrentIdentity();
    return _identity.isNotEmpty;
  }

  void _clearPending() {
    _pendingUpserts.clear();
    _pendingRead.clear();
    _pendingDismiss.clear();
  }

  Future<void> _pushPending() async {
    final upserts = Map<String, AppNotificationRecord>.from(_pendingUpserts);
    for (final entry in upserts.entries) {
      await gateway.upsert(entry.value);
      final pending = _pendingUpserts[entry.key];
      if (pending?.updatedAt == entry.value.updatedAt) {
        _pendingUpserts.remove(entry.key);
      }
    }

    final read = _pendingRead.toList(growable: false);
    if (read.isNotEmpty) {
      await gateway.markRead(read);
      _pendingRead.removeAll(read);
    }

    final dismiss = _pendingDismiss.toList(growable: false);
    if (dismiss.isNotEmpty) {
      await gateway.dismiss(dismiss);
      _pendingDismiss.removeAll(dismiss);
    }
  }

  Future<void> _pullChanges() async {
    for (var pageIndex = 0; pageIndex < 5; pageIndex++) {
      final page = await gateway.list(after: _cursor);
      final latestByOperation = <String, AppNotificationRemoteChange>{};
      for (final change in page.items) {
        final existing = latestByOperation[change.operationId];
        if (existing == null || change.version > existing.version) {
          latestByOperation[change.operationId] = change;
        }
      }
      final ordered = latestByOperation.values.toList()
        ..sort((a, b) => a.version.compareTo(b.version));
      for (final change in ordered) {
        final pending = _pendingUpserts[change.operationId];
        final remote = change.record;
        if (pending != null &&
            remote != null &&
            !remote.updatedAt.isAfter(pending.updatedAt)) {
          continue;
        }
        _onChange?.call(change);
      }
      final nextCursor = page.cursor > _cursor ? page.cursor : _cursor;
      if (nextCursor != _cursor) {
        _cursor = nextCursor;
        await AppStorage.setString(_cursorKey(_identity), '$_cursor');
      }
      if (page.items.length < 200) {
        break;
      }
    }
  }

  String _currentIdentity() {
    final account =
        AppStorage.getOperatorKey() ?? AppStorage.getUsername() ?? '';
    return '${AppStorage.getBaseUrl()}|$account';
  }

  String _cursorKey(String identity) {
    final digest = sha256.convert(utf8.encode(identity));
    return 'app_notification_sync_cursor_v1:$digest';
  }

  void dispose() {
    _disposed = true;
    _pushTimer?.cancel();
    _clearPending();
  }
}
