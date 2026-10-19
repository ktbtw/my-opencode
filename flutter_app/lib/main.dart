import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:permission_handler/permission_handler.dart';
import 'core/services/app_log_service.dart';
import 'core/services/global_overlay_service.dart';
import 'core/services/push_service.dart';
import 'core/notifications/app_notification_host.dart';
import 'core/storage/app_storage.dart';
import 'core/theme/app_theme.dart';
import 'app/router.dart';
import 'features/devices/application/device_operation_notification_coordinator.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await AppStorage.init();
  await AppLogService.startNewBatch(reason: 'app_launch');
  runApp(const ProviderScope(child: ChatCodexApp()));
  unawaited(_startBackgroundTaskNotifications());
}

Future<void> _startBackgroundTaskNotifications() async {
  try {
    await PushService.requestNotificationPermissionIfNeeded();
  } catch (error) {
    debugPrint('[TaskNotifications] permission request failed: $error');
  }
  try {
    final started = await GlobalOverlayService.startTaskNotifications();
    debugPrint('[TaskNotifications] service start requested started=$started');
  } catch (error) {
    debugPrint('[TaskNotifications] service start failed: $error');
  }
  // 悬浮助手：用户开启过「全局助手」就随 App 启动自动拉起（对齐 Operit）。
  try {
    final overlay = await GlobalOverlayService.start();
    debugPrint('[Overlay] auto start requested started=$overlay');
    // 录音权限交给悬浮层的语音球在首次使用时申请：
    // 启动时就申请一次，用户一旦拒绝就被系统标记成「不再询问」，之后再也弹不出原生授权框。
  } catch (error) {
    debugPrint('[Overlay] auto start failed: $error');
  }
}

class ChatCodexApp extends ConsumerStatefulWidget {
  const ChatCodexApp({super.key});

  @override
  ConsumerState<ChatCodexApp> createState() => _ChatCodexAppState();
}

class _ChatCodexAppState extends ConsumerState<ChatCodexApp> {
  StreamSubscription<PushLaunchPayload>? _notificationSubscription;

  @override
  void initState() {
    super.initState();
    _notificationSubscription = PushService.notificationIntents.listen(
      _openNotificationTarget,
    );
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      final payload = await PushService.getLaunchPayload();
      if (!payload.isEmpty) _openNotificationTarget(payload);
    });
  }

  void _openNotificationTarget(PushLaunchPayload payload) {
    if (payload.type != 'task_completed' ||
        payload.agentId?.isNotEmpty != true) {
      return;
    }
    final query = <String, String>{
      'agentId': payload.agentId!,
      if (payload.machineId?.isNotEmpty == true)
        'machineId': payload.machineId!,
      if (payload.projectId?.isNotEmpty == true)
        'projectId': payload.projectId!,
      if (payload.projectRoot?.isNotEmpty == true)
        'projectRoot': payload.projectRoot!,
      if (payload.sessionId?.isNotEmpty == true)
        'sessionId': payload.sessionId!,
    };
    WidgetsBinding.instance.addPostFrameCallback((_) {
      appRouter.go(Uri(path: '/chat', queryParameters: query).toString());
    });
  }

  @override
  void dispose() {
    _notificationSubscription?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final ref = this.ref;
    ref.watch(deviceOperationNotificationCoordinatorProvider);
    return MaterialApp.router(
      title: 'Chat Codex',
      theme: AppTheme.light,
      routerConfig: appRouter,
      builder: (context, child) =>
          AppNotificationHost(child: child ?? const SizedBox.shrink()),
      debugShowCheckedModeBanner: false,
      locale: const Locale('zh', 'CN'),
      supportedLocales: const [Locale('zh', 'CN'), Locale('en', 'US')],
      localizationsDelegates: const [
        GlobalMaterialLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
      ],
    );
  }
}
