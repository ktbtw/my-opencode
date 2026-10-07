import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'app_log_service.dart';
import '../storage/app_storage.dart';

/// Whether [value] counts as the app being in the foreground.
///
/// [AppLifecycleState.inactive] is treated as foreground: permission dialogs
/// and the notification shade make the app inactive without actually hiding
/// it, so sockets and reconciliation should stay alive for these brief
/// transitions.
bool isForegroundLifecycleState(AppLifecycleState? value) =>
    value == null ||
    value == AppLifecycleState.resumed ||
    value == AppLifecycleState.inactive;

class AppForeground extends StateNotifier<bool> with WidgetsBindingObserver {
  AppForeground()
    : super(
        isForegroundLifecycleState(WidgetsBinding.instance.lifecycleState),
      ) {
    WidgetsBinding.instance.addObserver(this);
    _recordLifecycle(WidgetsBinding.instance.lifecycleState, state);
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    final previous = this.state;
    this.state = isForegroundLifecycleState(state);
    _recordLifecycle(state, previous);
  }

  void _recordLifecycle(AppLifecycleState? lifecycle, bool previous) {
    final from = previous ? '前台' : '后台';
    final to = state ? '前台' : '后台';
    debugPrint('[AppLifecycle] state=$lifecycle $from -> $to');
    unawaited(
      _persistLifecycle({
        'state': lifecycle?.name ?? 'initial',
        'was_foreground': previous,
        'foreground': state,
        'transition': '$from -> $to',
      }),
    );
  }

  Future<void> _persistLifecycle(Map<String, dynamic> data) async {
    if (!AppStorage.initialized) return;
    try {
      await AppLogService.log('app_lifecycle_changed', data: data);
    } catch (error) {
      // Diagnostics must not interrupt providers during storage startup.
      debugPrint('[AppLifecycle] log write failed: $error');
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }
}

final appForegroundProvider = StateNotifierProvider<AppForeground, bool>(
  (ref) => AppForeground(),
);
