import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../features/auth/application/auth_controller.dart';
import '../features/auth/presentation/login_page.dart';
import '../features/chat/presentation/chat_page.dart';
import '../features/devices/presentation/device_detail_page.dart';
import '../features/devices/presentation/devices_page.dart';

final appRouterProvider = Provider<GoRouter>((ref) {
  final authState = ref.watch(authControllerProvider);

  return GoRouter(
    initialLocation: '/devices',
    redirect: (context, state) {
      final authenticated = authState.isAuthenticated;
      final loggingIn = state.matchedLocation == '/login';

      if (!authenticated && !loggingIn) {
        return '/login';
      }
      if (authenticated && loggingIn) {
        return '/devices';
      }
      return null;
    },
    routes: [
      GoRoute(
        path: '/login',
        builder: (context, state) => const LoginPage(),
      ),
      GoRoute(
        path: '/devices',
        builder: (context, state) => const DevicesPage(),
      ),
      GoRoute(
        path: '/devices/:machineId',
        builder: (context, state) => DeviceDetailPage(
          machineId: state.pathParameters['machineId']!,
        ),
      ),
      GoRoute(
        path: '/chat',
        builder: (context, state) => ChatPage(
          machineId: state.uri.queryParameters['machine_id'] ?? '',
          agentId: state.uri.queryParameters['agent_id'] ?? '',
          projectId: state.uri.queryParameters['project_id'] ?? '',
        ),
      ),
    ],
  );
});
