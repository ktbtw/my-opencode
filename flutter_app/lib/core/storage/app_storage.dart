import 'package:shared_preferences/shared_preferences.dart';

class AppStorage {
  static const _keyToken = 'access_token';
  static const _keyOperatorKey = 'operator_key';
  static const _keyUsername = 'username';
  static const _keyDisplayName = 'display_name';
  static const _keyBaseUrl = 'base_url';

  static SharedPreferences? _prefs;

  static Future<void> init() async {
    _prefs = await SharedPreferences.getInstance();
  }

  static SharedPreferences get _p {
    assert(_prefs != null, 'AppStorage.init() 必须在使用前调用');
    return _prefs!;
  }

  // Token
  static String? getToken() => _p.getString(_keyToken);
  static Future<void> setToken(String token) => _p.setString(_keyToken, token);

  // OperatorKey
  static String? getOperatorKey() => _p.getString(_keyOperatorKey);
  static Future<void> setOperatorKey(String key) =>
      _p.setString(_keyOperatorKey, key);

  // 用户名
  static String? getUsername() => _p.getString(_keyUsername);
  static Future<void> setUsername(String name) =>
      _p.setString(_keyUsername, name);

  // 显示名称
  static String? getDisplayName() => _p.getString(_keyDisplayName);
  static Future<void> setDisplayName(String name) =>
      _p.setString(_keyDisplayName, name);

  // 后端地址
  static String getBaseUrl() =>
      _p.getString(_keyBaseUrl) ?? 'http://127.0.0.1:8080';
  static Future<void> setBaseUrl(String url) => _p.setString(_keyBaseUrl, url);

  // 是否已登录
  static bool isLoggedIn() {
    final token = getToken();
    return token != null && token.isNotEmpty;
  }

  // 清除登录信息
  static Future<void> clearAuth() async {
    await _p.remove(_keyToken);
    await _p.remove(_keyOperatorKey);
    await _p.remove(_keyUsername);
    await _p.remove(_keyDisplayName);
  }
}
