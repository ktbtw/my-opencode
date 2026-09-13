import 'package:chat_codex_app/core/notifications/app_notification_model.dart';
import 'package:chat_codex_app/core/notifications/app_notification_sync.dart';
import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() async {
    SharedPreferences.setMockInitialValues({
      'access_token': 'test-token',
      'operator_key': 'operator-one',
      'base_url': 'https://example.test',
    });
    await AppStorage.init();
  });

  test(
    'throttled queue uploads only the newest synced operation state',
    () async {
      final gateway = _FakeGateway();
      final service = AppNotificationSyncService(
        gateway: gateway,
        pollInterval: const Duration(days: 1),
        pushDelay: const Duration(days: 1),
      );

      service.queueUpsert(_record('synced', progress: 0.1));
      service.queueUpsert(_record('synced', progress: 0.8));
      service.queueUpsert(
        _record('local', scope: AppNotificationScope.local, progress: 0.4),
      );
      await service.syncNow();

      expect(gateway.upserts, hasLength(1));
      expect(gateway.upserts.single.operationId, 'synced');
      expect(gateway.upserts.single.progress, 0.8);
      service.dispose();
    },
  );

  test('pull coalesces multiple remote versions by operation ID', () async {
    final gateway = _FakeGateway()
      ..seed(_change(1, _record('remote', progress: 0.2)))
      ..seed(_change(2, _record('remote', progress: 0.9)));
    final received = <AppNotificationRemoteChange>[];
    final service = AppNotificationSyncService(
      gateway: gateway,
      pollInterval: const Duration(days: 1),
      pushDelay: const Duration(days: 1),
    );

    service.start(received.add);
    await service.syncNow();

    expect(received, hasLength(1));
    expect(received.single.version, 2);
    expect(received.single.record?.progress, 0.9);
    service.dispose();
  });

  test(
    'account switch drops pending writes from the previous account',
    () async {
      final gateway = _FakeGateway();
      final service = AppNotificationSyncService(
        gateway: gateway,
        pollInterval: const Duration(days: 1),
        pushDelay: const Duration(days: 1),
      );

      service.queueUpsert(_record('old-account'));
      await AppStorage.setOperatorKey('operator-two');
      service.queueUpsert(_record('new-account'));
      await service.syncNow();

      expect(gateway.upserts.map((item) => item.operationId), ['new-account']);
      service.dispose();
    },
  );

  test('read and dismiss commands are batched without duplicates', () async {
    final gateway = _FakeGateway();
    final service = AppNotificationSyncService(
      gateway: gateway,
      pollInterval: const Duration(days: 1),
      pushDelay: const Duration(days: 1),
    );

    service.queueRead(['one', 'one', 'two']);
    service.queueDismiss(['two', 'three', 'three']);
    await service.syncNow();

    expect(gateway.readBatches.single, ['one']);
    expect(gateway.dismissBatches.single.toSet(), {'two', 'three'});
    service.dispose();
  });
}

AppNotificationRecord _record(
  String operationId, {
  AppNotificationScope scope = AppNotificationScope.synced,
  double progress = 0,
}) {
  final timestamp = DateTime.utc(2026, 8, 16, 12, 0, 0, progress.round());
  return AppNotificationRecord(
    id: 'id-$operationId',
    operationId: operationId,
    title: operationId,
    message: '处理中',
    status: AppNotificationStatus.running,
    progressMode: AppNotificationProgressMode.determinate,
    scope: scope,
    progress: progress,
    createdAt: timestamp,
    updatedAt: timestamp,
  );
}

AppNotificationRemoteChange _change(
  int version,
  AppNotificationRecord record,
) => AppNotificationRemoteChange(
  version: version,
  operationId: record.operationId,
  record: record,
);

class _FakeGateway implements AppNotificationRemoteGateway {
  final List<AppNotificationRecord> upserts = [];
  final List<List<String>> readBatches = [];
  final List<List<String>> dismissBatches = [];
  final List<AppNotificationRemoteChange> _changes = [];
  int _version = 0;

  void seed(AppNotificationRemoteChange change) {
    _changes.add(change);
    if (change.version > _version) _version = change.version;
  }

  @override
  Future<AppNotificationRemoteChange> upsert(
    AppNotificationRecord record,
  ) async {
    upserts.add(record);
    final change = _change(++_version, record);
    _changes.add(change);
    return change;
  }

  @override
  Future<AppNotificationRemotePage> list({
    required int after,
    int limit = 200,
  }) async {
    final items = _changes
        .where((item) => item.version > after)
        .take(limit)
        .toList(growable: false);
    return AppNotificationRemotePage(
      items: items,
      cursor: items.isEmpty ? after : items.last.version,
    );
  }

  @override
  Future<void> markRead(List<String> operationIds) async {
    readBatches.add(List.of(operationIds));
  }

  @override
  Future<void> dismiss(List<String> operationIds) async {
    dismissBatches.add(List.of(operationIds));
  }
}
