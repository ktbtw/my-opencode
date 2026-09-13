import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../../core/services/recent_task_resume.dart';
import '../../../core/storage/app_storage.dart';
import '../../../core/theme/app_colors.dart';
import '../../devices/presentation/device_provider.dart';
import '../../settings/settings_provider.dart';
import '../../chat/data/chat_target.dart';

class MobileStartupPage extends ConsumerStatefulWidget {
  const MobileStartupPage({super.key});

  @override
  ConsumerState<MobileStartupPage> createState() => _MobileStartupPageState();
}

class _MobileStartupPageState extends ConsumerState<MobileStartupPage> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _resolve());
  }

  bool get _isMobilePlatform =>
      !kIsWeb &&
      (defaultTargetPlatform == TargetPlatform.android ||
          defaultTargetPlatform == TargetPlatform.iOS);

  Future<void> _resolve() async {
    if (!_isMobilePlatform) {
      _go(AppStorage.isLoggedIn() ? '/devices' : '/login');
      return;
    }
    if (!AppStorage.isMobileOnboardingCompleted()) {
      _go('/onboarding');
      return;
    }
    if (!AppStorage.isLoggedIn()) {
      _go('/login');
      return;
    }

    final prefs = await ref.read(sharedPreferencesProvider.future);
    final autoOpen =
        prefs.getBool(AppSettings.autoOpenRecentTaskSettingKey) ?? false;
    if (!autoOpen) {
      _go('/devices');
      return;
    }
    final route = await _resolveRecentTask();
    _go(route ?? '/devices');
  }

  Future<String?> _resolveRecentTask() async {
    final record = RecentTaskResumeStore.load();
    if (record == null || record.machineId.isEmpty || record.agentId.isEmpty) {
      return null;
    }
    final age = DateTime.now().toUtc().difference(record.updatedAt.toUtc());
    if (age.isNegative || age > const Duration(days: 30)) return null;
    try {
      final device = await ref
          .read(deviceRepositoryProvider)
          .getDevice(record.machineId);
      final target = availableChatTargets([device])
          .where(
            (item) =>
                item.machineId == record.machineId &&
                item.agentId == record.agentId &&
                item.projectId == record.projectId,
          )
          .firstOrNull;
      return target == null ? null : chatTargetRoute(target);
    } catch (_) {
      return null;
    }
  }

  void _go(String route) {
    if (!mounted) return;
    context.go(route);
  }

  @override
  Widget build(BuildContext context) {
    return const Scaffold(
      backgroundColor: AppColors.background,
      body: Center(
        child: SizedBox(
          width: 24,
          height: 24,
          child: CircularProgressIndicator(strokeWidth: 2.5),
        ),
      ),
    );
  }
}

final sharedPreferencesProvider = FutureProvider(
  (_) => SharedPreferences.getInstance(),
);
