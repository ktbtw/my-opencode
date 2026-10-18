import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'dart:async';
import '../core/services/push_service.dart';
import '../core/services/global_overlay_service.dart';
import '../core/notifications/app_notification_host.dart';
import '../core/notifications/app_notification_controller.dart';
import '../core/storage/app_storage.dart';
import '../core/theme/app_theme.dart';
import '../features/devices/application/device_operation_notification_coordinator.dart';
import 'router.dart';
import '../features/devices/presentation/completed_agents_provider.dart';
import '../features/devices/presentation/device_live_updates.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await AppStorage.init();
  runApp(const ProviderScope(child: ChatCodexApp()));
}

class ChatCodexApp extends ConsumerStatefulWidget {
  const ChatCodexApp({super.key});

  @override
  ConsumerState<ChatCodexApp> createState() => _ChatCodexAppState();
}

class _ChatCodexAppState extends ConsumerState<ChatCodexApp> {
  bool _pushBootstrapped = false;
  StreamSubscription<PushLaunchPayload>? _notificationSubscription;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_pushBootstrapped) return;
    _pushBootstrapped = true;
    Future<void>(() async {
      try {
        await PushService.requestNotificationPermissionIfNeeded();
      } catch (error) {
        debugPrint('[TaskNotifications] permission request failed: $error');
      }
      // The task notification service must not depend on the optional JPush SDK.
      try {
        final started = await GlobalOverlayService.startTaskNotifications();
        debugPrint(
          '[TaskNotifications] service start requested started=$started',
        );
      } catch (error) {
        debugPrint('[TaskNotifications] service start failed: $error');
      }
      // 悬浮助手：用户开启过「全局助手」就随 App 启动自动拉起（对齐 Operit）。
      try {
        final overlay = await GlobalOverlayService.start();
        debugPrint('[Overlay] auto start requested started=$overlay');
      } catch (error) {
        debugPrint('[Overlay] auto start failed: $error');
      }
      try {
        await PushService.initialize();
        await PushService.syncRegistrationIfPossible();
      } catch (error) {
        debugPrint('[Push] optional JPush initialization failed: $error');
      }
      final launchPayload = await PushService.getLaunchPayload();
      if (!launchPayload.isEmpty) _openNotificationTarget(launchPayload);
      _notificationSubscription = PushService.notificationIntents.listen(
        _openNotificationTarget,
      );
    });
  }

  void _openNotificationTarget(PushLaunchPayload payload) {
    if (payload.type != 'task_completed' || payload.agentId == null) return;
    if (payload.taskId?.isNotEmpty == true) {
      ref
          .read(appNotificationControllerProvider.notifier)
          .markRead('chat-task:${payload.taskId}', synchronizeIfMissing: true);
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
    ref.watch(deviceOperationNotificationCoordinatorProvider);
    ref.watch(deviceLiveUpdatesProvider);
    // 常驻：设备详情页没打开时也要把"跑完了"登记到「完成」分类。
    ref.watch(completedAgentsProvider);
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
