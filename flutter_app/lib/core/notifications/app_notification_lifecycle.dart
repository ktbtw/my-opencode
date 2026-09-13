import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'app_notification_sync.dart';
import '../../features/settings/settings_provider.dart';
import '../services/global_overlay_service.dart';

/// Manages notification sync based on app lifecycle state
class AppNotificationLifecycleManager with WidgetsBindingObserver {
  final AppNotificationSyncService syncService;
  final Ref ref;
  bool _foreground = true;

  AppNotificationLifecycleManager(this.syncService, this.ref) {
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    final wasForeground = _foreground;
    _foreground = state == AppLifecycleState.resumed;

    if (_foreground && !wasForeground) {
      _ensureNativeNotificationService();
      // Native Android owns the live SSE; refresh the durable feed on resume.
      debugPrint('[NotificationLifecycle] foreground -> REST reconciliation');
      syncService.enableSSE();
    } else if (!_foreground && wasForeground) {
      // App went to background: check user preference
      final settings = ref.read(settingsProvider);
      if (settings.backgroundNotificationEnabled) {
        // The native foreground service owns background delivery.
        debugPrint(
          '[NotificationLifecycle] background -> native notification service',
        );
      } else {
        debugPrint(
          '[NotificationLifecycle] background -> notifications disabled',
        );
        syncService.disableSSE();
      }
    }
  }

  void _ensureNativeNotificationService() {
    if (!GlobalOverlayService.supported) return;
    final settings = ref.read(settingsProvider);
    if (!settings.backgroundNotificationEnabled) return;
    unawaited(_restartNativeNotificationService());
  }

  Future<void> _restartNativeNotificationService() async {
    try {
      await GlobalOverlayService.startTaskNotifications();
    } catch (error) {
      debugPrint(
        '[NotificationLifecycle] native service restart failed: $error',
      );
    }
  }

  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
  }
}

final appNotificationLifecycleManagerProvider =
    Provider<AppNotificationLifecycleManager>((ref) {
      final syncService = ref.watch(appNotificationSyncServiceProvider);
      final manager = AppNotificationLifecycleManager(syncService, ref);
      ref.onDispose(manager.dispose);
      return manager;
    });
