import '../../../core/config/api_client.dart';
import '../../../core/services/app_log_service.dart';
import '../../../core/services/push_service.dart';
import '../../../core/storage/app_storage.dart';

class AuthRepository {
  Future<void> login(String username, String password) async {
    await AppLogService.log(
      'auth_login_started',
      data: {'username': username.trim()},
    );
    final data = await ApiClient.post('/api/auth/login', {
      'username': username,
      'password': password,
    }, withAuth: false);

    final token = data['access_token'] as String?;
    if (token == null || token.isEmpty) {
      throw const ApiException('登录响应中缺少 access_token', statusCode: 200);
    }

    await AppStorage.setToken(token);
    await AppStorage.setUsername(username);
    await AppStorage.setPassword(password);

    final operator = data['operator'] as Map<String, dynamic>?;
    if (operator != null) {
      final operatorKey = operator['operator_key'] as String?;
      final displayName =
          operator['display_name'] as String? ??
          operator['username'] as String? ??
          username;
      final membershipTier = operator['membership_tier'] as String?;
      if (operatorKey != null) {
        await AppStorage.setOperatorKey(operatorKey);
      }
      await AppStorage.setDisplayName(displayName);
      if (membershipTier != null && membershipTier.trim().isNotEmpty) {
        await AppStorage.setMembershipTier(membershipTier);
      }
    }

    await refreshMembershipTier();

    await AppLogService.log(
      'auth_login_succeeded',
      data: {'username': username.trim(), 'has_operator': operator != null},
    );
    await PushService.requestNotificationPermissionIfNeeded();
    await PushService.initialize();
    await PushService.syncRegistrationIfPossible();
  }

  /// 刷新会员等级：以 /api/auth/me 为准，失败时保留本地缓存，不影响登录流程。
  Future<void> refreshMembershipTier() async {
    try {
      final me = await ApiClient.get('/api/auth/me');
      final tier = (me['membership_tier'] as String?)?.trim();
      if (tier != null && tier.isNotEmpty) {
        await AppStorage.setMembershipTier(tier);
      }
    } catch (_) {
      // 网络异常时沿用已有缓存，避免因会员信息拉取失败阻断登录。
    }
  }

  Future<void> logout() async {
    await AppLogService.log(
      'auth_logout',
      data: {'username': AppStorage.getUsername() ?? ''},
    );
    await AppStorage.clearAuth();
  }
}
