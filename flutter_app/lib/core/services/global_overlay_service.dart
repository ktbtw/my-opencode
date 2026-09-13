import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../storage/app_storage.dart';

class GlobalOverlayService {
  GlobalOverlayService._();

  static const _channel = MethodChannel('chat_codex/global_overlay');
  static const _events = EventChannel('chat_codex/global_overlay_events');
  static const _notificationEvents = EventChannel(
    'chat_codex/notification_events',
  );
  static const _enabledKey = 'global_agent_overlay_enabled_v1';

  static bool get supported =>
      !kIsWeb && defaultTargetPlatform == TargetPlatform.android;

  static Stream<Map<String, dynamic>> get events {
    if (!supported) return const Stream.empty();
    return _events.receiveBroadcastStream().map(
      (event) => Map<String, dynamic>.from(event as Map),
    );
  }

  /// Native background notification changes. The Android foreground service
  /// owns this stream; Flutter consumes it only to refresh the visible center.
  static Stream<Map<String, dynamic>> get notificationEvents {
    if (!supported) return const Stream.empty();
    return _notificationEvents.receiveBroadcastStream().map(
      (event) => Map<String, dynamic>.from(event as Map),
    );
  }

  static Future<bool> canDrawOverlays() async {
    if (!supported) return false;
    return await _channel.invokeMethod<bool>('canDrawOverlays') ?? false;
  }

  static Future<void> requestPermission() async {
    if (!supported) return;
    await _channel.invokeMethod<void>('requestPermission');
  }

  static Future<bool> start() async {
    if (!supported || !await canDrawOverlays()) {
      return false;
    }
    final started = await _channel.invokeMethod<bool>('start') ?? false;
    if (started && AppStorage.initialized) {
      await AppStorage.setString(_enabledKey, 'true');
    }
    return started;
  }

  static Future<bool> startTaskNotifications() async {
    if (!supported) return false;
    return await _channel.invokeMethod<bool>('startTaskNotifications') ?? false;
  }

  static Future<void> stop() async {
    if (!supported) return;
    await _channel.invokeMethod<void>('stop');
    if (AppStorage.initialized) {
      await AppStorage.setString(_enabledKey, 'false');
    }
  }

  static Future<bool> isRunning() async {
    if (!supported) return false;
    return await _channel.invokeMethod<bool>('isRunning') ?? false;
  }

  static Future<void> restoreIfEnabled() async {
    if (!supported || !AppStorage.initialized) return;
    if (AppStorage.getString(_enabledKey) != 'true') return;
    if (await canDrawOverlays()) {
      await _channel.invokeMethod<bool>('start');
    }
  }
}

Future<bool> confirmGlobalOverlayStop(BuildContext context) async {
  return await showDialog<bool>(
        context: context,
        builder: (dialogContext) => Dialog(
          backgroundColor: Colors.transparent,
          insetPadding: const EdgeInsets.symmetric(horizontal: 28),
          child: Container(
            decoration: BoxDecoration(
              color: Theme.of(dialogContext).colorScheme.surface,
              borderRadius: BorderRadius.circular(14),
              border: Border.all(
                color: Theme.of(dialogContext).colorScheme.outline,
              ),
            ),
            padding: const EdgeInsets.fromLTRB(22, 20, 22, 16),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Container(
                      width: 40,
                      height: 40,
                      decoration: BoxDecoration(
                        color: Theme.of(
                          dialogContext,
                        ).colorScheme.errorContainer,
                        borderRadius: BorderRadius.circular(10),
                      ),
                      child: Icon(
                        Icons.layers_clear_outlined,
                        color: Theme.of(dialogContext).colorScheme.error,
                        size: 21,
                      ),
                    ),
                    const SizedBox(width: 12),
                    const Expanded(
                      child: Padding(
                        padding: EdgeInsets.only(top: 1),
                        child: Text(
                          '关闭全局助手？',
                          style: TextStyle(
                            fontSize: 17,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 14),
                Text(
                  '关闭后将不再显示悬浮图标，也不会在应用启动时自动恢复。',
                  style: Theme.of(dialogContext).textTheme.bodyMedium,
                ),
                const SizedBox(height: 20),
                Row(
                  mainAxisAlignment: MainAxisAlignment.end,
                  children: [
                    OutlinedButton(
                      onPressed: () => Navigator.of(dialogContext).pop(false),
                      child: const Text('取消'),
                    ),
                    const SizedBox(width: 10),
                    FilledButton(
                      style: FilledButton.styleFrom(
                        backgroundColor: Theme.of(
                          dialogContext,
                        ).colorScheme.error,
                        foregroundColor: Theme.of(
                          dialogContext,
                        ).colorScheme.onError,
                      ),
                      onPressed: () => Navigator.of(dialogContext).pop(true),
                      child: const Text('关闭'),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ) ??
      false;
}
