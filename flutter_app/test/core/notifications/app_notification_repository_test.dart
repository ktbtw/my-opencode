import 'package:chat_codex_app/core/notifications/app_notification_model.dart';
import 'package:chat_codex_app/core/notifications/app_notification_repository.dart';
import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'local notification snapshots are isolated by operator identity',
    () async {
      SharedPreferences.setMockInitialValues({});
      await AppStorage.init();
      final repository = LocalAppNotificationRepository();
      final now = DateTime.utc(2026, 8, 16);

      await AppStorage.setBaseUrl('https://server-one.test');
      await AppStorage.setOperatorKey('operator-one');
      await repository.save(
        AppNotificationSnapshot(recent: [_record('operator-one-op', now)]),
      );

      await AppStorage.setOperatorKey('operator-two');
      expect((await repository.load()).recent, isEmpty);
      await repository.save(
        AppNotificationSnapshot(recent: [_record('operator-two-op', now)]),
      );

      await AppStorage.setOperatorKey('operator-one');
      expect(
        (await repository.load()).recent.single.operationId,
        'operator-one-op',
      );
      await AppStorage.setBaseUrl('https://server-two.test');
      expect((await repository.load()).recent, isEmpty);
      await AppStorage.setBaseUrl('https://server-one.test');
      await AppStorage.setOperatorKey('operator-two');
      expect(
        (await repository.load()).recent.single.operationId,
        'operator-two-op',
      );
    },
  );
}

AppNotificationRecord _record(String operationId, DateTime timestamp) {
  return AppNotificationRecord(
    id: 'id-$operationId',
    operationId: operationId,
    title: operationId,
    message: '完成',
    status: AppNotificationStatus.succeeded,
    createdAt: timestamp,
    updatedAt: timestamp,
    completedAt: timestamp,
  );
}
