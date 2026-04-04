import 'package:shared_preferences/shared_preferences.dart';

class AuthStorage {
  const AuthStorage();

  static const _tokenKey = 'chat_codex_access_token';
  static const _operatorKey = 'chat_codex_operator_key';
  static const _operatorNameKey = 'chat_codex_operator_name';
  static const _usernameKey = 'chat_codex_username';

  Future<Map<String, String>> read() async {
    final prefs = await SharedPreferences.getInstance();
    return {
      'access_token': prefs.getString(_tokenKey) ?? '',
      'operator_key': prefs.getString(_operatorKey) ?? '',
      'name': prefs.getString(_operatorNameKey) ?? '',
      'username': prefs.getString(_usernameKey) ?? '',
    };
  }

  Future<void> write({
    required String accessToken,
    required String operatorKey,
    required String name,
    required String username,
  }) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_tokenKey, accessToken);
    await prefs.setString(_operatorKey, operatorKey);
    await prefs.setString(_operatorNameKey, name);
    await prefs.setString(_usernameKey, username);
  }

  Future<void> clear() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_tokenKey);
    await prefs.remove(_operatorKey);
    await prefs.remove(_operatorNameKey);
    await prefs.remove(_usernameKey);
  }
}
