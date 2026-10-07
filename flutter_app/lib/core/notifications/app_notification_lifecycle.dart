import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'app_notification_sync.dart';
import '../../features/settings/settings_provider.dart';
import '../services/app_foreground.dart';
import '../services/global_overlay_service.dart';
import '../services/app_log_service.dart';
import '../storage/app_storage.dart';

/// Manages notification sync based on app lifecycle state
class AppNotificationLifecycleManager with WidgetsBindingObserver {
  final AppNotificationSyncService syncService;
  final Ref ref;
  bool _foreground;
  bool _nativeStartInFlight = false;
  bool _disposed = false;

  AppNotificationLifecycleManager(this.syncService, this.ref)
    : _foreground = isForegroundLifecycleState(
        WidgetsBinding.instance.lifecycleState,
      ) {
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (_disposed) return;
    final wasForeground = _foreground;
    _foreground = isForegroundLifecycleState(state);
    debugPrint(
      '[NotificationLifecycle] state=$state foreground=$_foreground '
      '(was=$wasForeground)',
    );

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
        // Keep this idempotent call on the transition as well. It covers the
        // case where Android reclaimed the service while the app was visible
        // or it started before credentials were written to shared prefs.
        _ensureNativeNotificationService();
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
    if (_disposed || _nativeStartInFlight) return;
    _nativeStartInFlight = true;
    unawaited(_restartNativeNotificationService());
  }

  Future<void> _restartNativeNotificationService() async {
    try {
      final started = await GlobalOverlayService.startTaskNotifications();
      debugPrint('[NotificationLifecycle] native service started=$started');
      await _logServiceStart(started: started);
    } catch (error) {
      debugPrint(
        '[NotificationLifecycle] native service restart failed: $error',
      );
      await _logServiceStart(started: false, error: error.toString());
    } finally {
      _nativeStartInFlight = false;
    }
  }

  Future<void> _logServiceStart({required bool started, String? error}) async {
    if (!AppStorage.initialized) return;
    try {
      await AppLogService.log(
        'background_notification_service_start',
        level: started ? 'info' : 'warning',
        data: {
          'started': started,
          'foreground': _foreground,
          if (error != null) 'error': error,
        },
      );
    } catch (error) {
      debugPrint('[NotificationLifecycle] diagnostic write failed: $error');
    }
  }

  void dispose() {
    _disposed = true;
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
