import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../core/storage/app_storage.dart';
import '../../features/auth/presentation/login_page.dart';
import '../../features/devices/presentation/device_list_page.dart';
import '../../features/devices/presentation/device_detail_page.dart';
import '../../features/chat/presentation/chat_page.dart';

final appRouter = GoRouter(
  initialLocation: '/login',
  redirect: (context, state) {
    final isLoggedIn = AppStorage.isLoggedIn();
    final isLoginPage = state.matchedLocation == '/login';
    if (!isLoggedIn && !isLoginPage) return '/login';
    if (isLoggedIn && isLoginPage) return '/devices';
    return null;
  },
  routes: [
    GoRoute(
      path: '/login',
      pageBuilder: (context, state) => _buildPage(
        state,
        const LoginPage(),
      ),
    ),
    GoRoute(
      path: '/devices',
      pageBuilder: (context, state) => _buildPage(
        state,
        const DeviceListPage(),
      ),
    ),
    GoRoute(
      path: '/devices/:machineId',
      pageBuilder: (context, state) => _buildPage(
        state,
        DeviceDetailPage(machineId: state.pathParameters['machineId']!),
      ),
    ),
    GoRoute(
      path: '/chat/:agentId',
      pageBuilder: (context, state) => _buildPage(
        state,
        ChatPage(
          agentId: state.pathParameters['agentId']!,
          projectId: state.uri.queryParameters['projectId'] ?? '',
          machineId: state.uri.queryParameters['machineId'] ?? '',
        ),
      ),
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
        opacity: CurvedAnimation(
          parent: animation,
          curve: Curves.easeInOut,
        ),
        child: child,
      );
    },
  );
}
