import 'dart:convert';

import 'package:crypto/crypto.dart';

import '../storage/app_storage.dart';
import 'app_notification_model.dart';

abstract class AppNotificationRepository {
  Future<AppNotificationSnapshot> load();
  Future<void> save(AppNotificationSnapshot snapshot);
}

class LocalAppNotificationRepository implements AppNotificationRepository {
  static const storageKey = 'app_notification_snapshot_v1';

  String get _accountStorageKey {
    final identity = AppStorage.storageIdentity;
    final digest = sha256.convert(utf8.encode(identity));
    return '$storageKey:$digest';
  }

  @override
  Future<AppNotificationSnapshot> load() async {
    if (!AppStorage.initialized) return const AppNotificationSnapshot();
    final raw = AppStorage.getString(_accountStorageKey);
    if (raw == null || raw.trim().isEmpty) {
      return const AppNotificationSnapshot();
    }
    try {
      final decoded = jsonDecode(raw);
      if (decoded is! Map) return const AppNotificationSnapshot();
      return AppNotificationSnapshot.fromJson(decoded.cast<String, dynamic>());
    } catch (_) {
      return const AppNotificationSnapshot();
    }
  }

  @override
  Future<void> save(AppNotificationSnapshot snapshot) async {
    if (!AppStorage.initialized) return;
    await AppStorage.setString(
      _accountStorageKey,
      jsonEncode(snapshot.toJson()),
    );
  }
}
