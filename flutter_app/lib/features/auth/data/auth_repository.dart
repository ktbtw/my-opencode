import '../../../core/config/api_client.dart';
import '../../../core/storage/app_storage.dart';

class AuthRepository {
  Future<void> login(String username, String password) async {
    final data = await ApiClient.post(
      '/api/auth/login',
      {'username': username, 'password': password},
      withAuth: false,
    );

    final token = data['access_token'] as String?;
    if (token == null || token.isEmpty) {
      throw const ApiException('登录响应中缺少 access_token', statusCode: 200);
    }

    await AppStorage.setToken(token);
    await AppStorage.setUsername(username);

    final operator = data['operator'] as Map<String, dynamic>?;
    if (operator != null) {
      final operatorKey = operator['operator_key'] as String?;
      final displayName = operator['display_name'] as String? ??
          operator['username'] as String? ??
          username;
      if (operatorKey != null) {
        await AppStorage.setOperatorKey(operatorKey);
      }
      await AppStorage.setDisplayName(displayName);
    }
  }

  Future<void> logout() async {
    await AppStorage.clearAuth();
  }
}
