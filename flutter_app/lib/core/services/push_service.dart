import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:permission_handler/permission_handler.dart';

import '../config/api_client.dart';
import '../storage/app_storage.dart';

class PushLaunchPayload {
  final String? type;
  final String? taskId;
  final String? sessionId;
  final String? agentId;
  final String? machineId;
  final String? projectId;
  final String? projectRoot;

  const PushLaunchPayload({
    this.type,
    this.taskId,
    this.sessionId,
    this.agentId,
    this.machineId,
    this.projectId,
    this.projectRoot,
  });

  bool get isEmpty =>
      (type == null || type!.isEmpty) &&
      (taskId == null || taskId!.isEmpty) &&
      (sessionId == null || sessionId!.isEmpty) &&
      (agentId == null || agentId!.isEmpty);

  factory PushLaunchPayload.fromMap(Map<dynamic, dynamic>? map) {
    if (map == null) return const PushLaunchPayload();
    return PushLaunchPayload(
      type: map['type']?.toString(),
      taskId: map['task_id']?.toString(),
      sessionId: map['session_id']?.toString(),
      agentId: map['agent_id']?.toString(),
      machineId: map['machine_id']?.toString(),
      projectId: map['project_id']?.toString(),
      projectRoot: map['project_root']?.toString(),
    );
  }
}

class PushService {
  static const MethodChannel _channel = MethodChannel(
    'chat_codex/push_service',
  );
  static const EventChannel _notificationEvents = EventChannel(
    'chat_codex/notification_intents',
  );

  static bool get _isAndroidNative =>
      !kIsWeb && defaultTargetPlatform == TargetPlatform.android;

  static Future<void> requestNotificationPermissionIfNeeded() async {
    if (!_isAndroidNative) return;
    final status = await Permission.notification.status;
    if (status.isGranted) return;
    await Permission.notification.request();
  }

  static Future<void> initialize() async {
    if (!_isAndroidNative) return;
    await _channel.invokeMethod<void>('initialize');
  }

  static Future<String?> getRegistrationId() async {
    if (!_isAndroidNative) return null;
    final value = await _channel.invokeMethod<String>('getRegistrationId');
    final registrationId = value?.trim() ?? '';
    if (!_isValidRegistrationId(registrationId)) {
      debugPrint('[Push] ignore invalid registrationId=$registrationId');
      return null;
    }
    return registrationId;
  }

  static bool _isValidRegistrationId(String value) {
    if (value.isEmpty) return false;
    if (value.startsWith('placeholder-')) return false;
    if (value.length < 16) return false;
    return true;
  }

  static Future<PushLaunchPayload> getLaunchPayload() async {
    if (!_isAndroidNative) return const PushLaunchPayload();
    final value = await _channel.invokeMapMethod<dynamic, dynamic>(
      'getLaunchPayload',
    );
    return PushLaunchPayload.fromMap(value);
  }

  static Stream<PushLaunchPayload> get notificationIntents {
    if (!_isAndroidNative) return const Stream.empty();
    return _notificationEvents.receiveBroadcastStream().map(
      (event) => PushLaunchPayload.fromMap(event as Map),
    );
  }

  static Future<void> syncRegistrationIfPossible() async {
    if (!_isAndroidNative) return;
    if (!AppStorage.isLoggedIn()) return;
    final registrationId = await getRegistrationId();
    if (registrationId == null) {
      debugPrint('[Push] registrationId unavailable, skip device sync');
      return;
    }

    final packageInfo = await PackageInfo.fromPlatform();
    final deviceId = await _channel.invokeMethod<String>('getDeviceId');
    final brand = await _channel.invokeMethod<String>('getDeviceBrand');
    final model = await _channel.invokeMethod<String>('getDeviceModel');

    await ApiClient.post('/api/push/devices', {
      'platform': 'android',
      'vendor': 'jpush',
      'registration_id': registrationId,
      'device_id': deviceId ?? '',
      'device_brand': brand ?? '',
      'device_model': model ?? '',
      'app_version': packageInfo.version,
    });
    debugPrint('[Push] synced registrationId=$registrationId');
  }
}
