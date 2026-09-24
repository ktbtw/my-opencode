import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

class AppForeground extends StateNotifier<bool> with WidgetsBindingObserver {
  AppForeground()
    : super(_isForeground(WidgetsBinding.instance.lifecycleState)) {
    WidgetsBinding.instance.addObserver(this);
  }

  static bool _isForeground(AppLifecycleState? value) =>
      value == null ||
      value == AppLifecycleState.resumed ||
      value == AppLifecycleState.inactive;

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // Permission dialogs and notification shade can be inactive without hiding
    // the app; avoid tearing down sockets for these brief transitions.
    this.state = _isForeground(state);
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
