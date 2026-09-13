import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../core/storage/app_storage.dart';
import '../core/services/app_navigation.dart';
import '../core/config/feature_flags.dart';
import '../../features/auth/presentation/login_page.dart';
import '../../features/auth/presentation/register_page.dart';
import '../../features/devices/presentation/device_list_page.dart';
import '../../features/devices/presentation/device_detail_page.dart';
import '../../features/devices/presentation/device_ai_config_page.dart';
import '../../features/devices/presentation/device_env_config_page.dart';
import '../../features/devices/presentation/device_mcp_config_page.dart';
import '../../features/devices/presentation/device_skill_config_page.dart';
import '../../features/chat/presentation/chat_page.dart';
import '../../features/chat/presentation/project_files_page.dart';
import '../../features/project_memory/presentation/project_memory_page.dart';
import '../../features/settings/presentation/settings_page.dart';
import '../../features/profile/presentation/profile_page.dart';
import '../../features/settings/presentation/app_release_history_page.dart';
import '../../features/guide/presentation/guide_page.dart';
import '../../features/onboarding/presentation/mobile_onboarding_page.dart';
import '../../features/onboarding/presentation/mobile_startup_page.dart';

final appRouter = GoRouter(
  navigatorKey: appNavigatorKey,
  initialLocation: '/startup',
  redirect: (context, state) {
    final isLoggedIn = AppStorage.isLoggedIn();
    final loc = state.matchedLocation;
    final isAuthPage = loc == '/login' || loc == '/register';
    final isPublicPage =
        isAuthPage || loc == '/startup' || loc == '/onboarding';
    if (!isLoggedIn && !isPublicPage) return '/login';
    if (isLoggedIn && isAuthPage) return '/devices';
    return null;
  },
  routes: [
    GoRoute(
      path: '/startup',
      pageBuilder: (context, state) =>
          _buildPage(state, const MobileStartupPage()),
    ),
    GoRoute(
      path: '/onboarding',
      pageBuilder: (context, state) =>
          _buildPage(state, const MobileOnboardingPage()),
    ),
    GoRoute(
      path: '/login',
      pageBuilder: (context, state) => _buildPage(state, const LoginPage()),
    ),
    GoRoute(
      path: '/register',
      pageBuilder: (context, state) => _buildPage(state, const RegisterPage()),
    ),
    GoRoute(
      path: '/devices',
      pageBuilder: (context, state) =>
          _buildPage(state, const DeviceListPage()),
    ),
    GoRoute(
      path: '/devices/:machineId',
      pageBuilder: (context, state) => _buildPage(
        state,
        DeviceDetailPage(machineId: state.pathParameters['machineId']!),
      ),
    ),
    GoRoute(
      path: '/devices/:machineId/ai-config',
      pageBuilder: (context, state) => _buildPage(
        state,
        DeviceAIConfigPage(
          machineId: state.pathParameters['machineId']!,
          agentId: state.uri.queryParameters['agentId'] ?? '',
          projectId: state.uri.queryParameters['projectId'] ?? '',
        ),
      ),
    ),
    GoRoute(
      path: '/devices/:machineId/mcp-config',
      pageBuilder: (context, state) => _buildPage(
        state,
        DeviceMCPConfigPage(
          machineId: state.pathParameters['machineId']!,
          agentId: state.uri.queryParameters['agentId'] ?? '',
          projectId: state.uri.queryParameters['projectId'] ?? '',
        ),
      ),
    ),
    GoRoute(
      path: '/devices/:machineId/skill-config',
      pageBuilder: (context, state) => _buildPage(
        state,
        DeviceSkillConfigPage(machineId: state.pathParameters['machineId']!),
      ),
    ),
    GoRoute(
      path: '/devices/:machineId/env-config',
      pageBuilder: (context, state) => _buildPage(
        state,
        DeviceEnvConfigPage(
          machineId: state.pathParameters['machineId']!,
          agentId: state.uri.queryParameters['agentId'] ?? '',
          projectId: state.uri.queryParameters['projectId'] ?? '',
        ),
      ),
    ),
    GoRoute(
      path: '/chat',
      pageBuilder: (context, state) => _buildPage(
        state,
        ChatPage(
          agentId: state.uri.queryParameters['agentId'] ?? '',
          projectId: state.uri.queryParameters['projectId'] ?? '',
          projectRoot: state.uri.queryParameters['projectRoot'] ?? '',
          machineId: state.uri.queryParameters['machineId'] ?? '',
          projectScopeId: state.uri.queryParameters['projectScopeId'] ?? '',
          sessionId: state.uri.queryParameters['sessionId'] ?? '',
        ),
      ),
    ),
    GoRoute(
      path: '/chat/files',
      pageBuilder: (context, state) => _buildPage(
        state,
        ProjectFilesPage(
          machineId: state.uri.queryParameters['machineId'] ?? '',
          agentId: state.uri.queryParameters['agentId'] ?? '',
          projectId: state.uri.queryParameters['projectId'] ?? '',
        ),
      ),
    ),
    if (projectMemoryFeatureEnabled)
      GoRoute(
        path: '/devices/:machineId/projects/:scopeId/memory',
        pageBuilder: (context, state) => _buildPage(
          state,
          ProjectMemoryPage(
            machineId: state.pathParameters['machineId']!,
            scopeId: state.pathParameters['scopeId']!,
          ),
        ),
      ),
    GoRoute(
      path: '/settings',
      pageBuilder: (context, state) => _buildPage(state, const SettingsPage()),
    ),
    GoRoute(
      path: '/profile',
      pageBuilder: (context, state) => _buildPage(state, const ProfilePage()),
    ),
    GoRoute(
      path: '/app-release-history',
      pageBuilder: (context, state) =>
          _buildPage(state, const AppReleaseHistoryPage()),
    ),
    GoRoute(
      path: '/launcher-downloads',
      pageBuilder: (context, state) => _buildPage(state, const GuidePage()),
    ),
    GoRoute(
      path: '/guide',
      redirect: (context, state) => '/launcher-downloads',
    ),
  ],
);

CustomTransitionPage _buildPage(GoRouterState state, Widget child) {
  return CustomTransitionPage(
    key: state.pageKey,
    child: child,
    transitionDuration: const Duration(milliseconds: 220),
    transitionsBuilder: (context, animation, secondaryAnimation, child) {
      return FadeTransition(
        opacity: CurvedAnimation(parent: animation, curve: Curves.easeInOut),
        child: child,
      );
    },
  );
}
