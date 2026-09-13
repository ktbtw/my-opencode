import 'dart:async';

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:shared_preferences/shared_preferences.dart';

class AppStorage {
  static const _compiledDefaultBaseUrl = String.fromEnvironment(
    'CHAT_CODEX_DEFAULT_BASE_URL',
    defaultValue: 'https://www.xyapi.top/codex',
  );

  static String get defaultBaseUrl {
    final compiledUri = Uri.tryParse(_compiledDefaultBaseUrl);
    if (kIsWeb &&
        compiledUri != null &&
        compiledUri.hasScheme &&
        compiledUri.host.isNotEmpty &&
        compiledUri.path.replaceAll('/', '').isNotEmpty) {
      return _compiledDefaultBaseUrl;
    }
    final uri = Uri.base;
    if (kIsWeb &&
        (uri.scheme == 'http' || uri.scheme == 'https') &&
        uri.host.isNotEmpty) {
      return uri.origin;
    }
    return _compiledDefaultBaseUrl;
  }

  static const _keyToken = 'access_token';
  static const _keyOperatorKey = 'operator_key';
  static const _keyUsername = 'username';
  static const _keyDisplayName = 'display_name';
  static const _keyBaseUrl = 'base_url';
  static const _keyPassword = 'password';
  static const _keyMobileOnboardingCompleted = 'mobile_onboarding_completed_v1';
  static const _opencodeSkippedVersionPrefix = 'opencode_skipped_version:';

  static SharedPreferences? _prefs;
  static final StreamController<String> _authIdentityChanges =
      StreamController<String>.broadcast();

  static bool get initialized => _prefs != null;
  static Stream<String> get authIdentityChanges => _authIdentityChanges.stream;

  static String get authIdentity {
    if (!initialized) return '';
    final operatorKey = getOperatorKey()?.trim() ?? '';
    if (operatorKey.isNotEmpty) return operatorKey;
    return getUsername()?.trim() ?? '';
  }

  static String get storageIdentity => '${getBaseUrl()}|$authIdentity';

  static Future<void> init() async {
    _prefs = await SharedPreferences.getInstance();
    await _migrateIpBaseUrl();
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
  static Future<void> setOperatorKey(String key) async {
    final previous = authIdentity;
    await _p.setString(_keyOperatorKey, key);
    final current = authIdentity;
    if (current != previous) _authIdentityChanges.add(current);
  }

  // 用户名
  static String? getUsername() => _p.getString(_keyUsername);
  static Future<void> setUsername(String name) =>
      _p.setString(_keyUsername, name);

  // 显示名称
  static String? getDisplayName() => _p.getString(_keyDisplayName);
  static Future<void> setDisplayName(String name) =>
      _p.setString(_keyDisplayName, name);

  // 后端地址
  static String getBaseUrl() => _p.getString(_keyBaseUrl) ?? defaultBaseUrl;
  static Future<void> setBaseUrl(String url) async {
    final previous = storageIdentity;
    await _p.setString(_keyBaseUrl, url);
    if (authIdentity.isNotEmpty && storageIdentity != previous) {
      _authIdentityChanges.add(storageIdentity);
    }
  }

  static Future<void> _migrateIpBaseUrl() async {
    final stored = _p.getString(_keyBaseUrl)?.trim();
    if (stored == null || stored.isEmpty) return;
    final uri = Uri.tryParse(stored);
    if (uri == null || !_isIpv4Host(uri.host)) return;
    await _p.setString(_keyBaseUrl, defaultBaseUrl);
  }

  static bool _isIpv4Host(String host) {
    final parts = host.split('.');
    if (parts.length != 4) return false;
    for (final part in parts) {
      final value = int.tryParse(part);
      if (value == null || value < 0 || value > 255) return false;
    }
    return true;
  }

  // 密码（用于 token 过期后自动重登）
  static String? getPassword() => _p.getString(_keyPassword);
  static Future<void> setPassword(String pwd) =>
      _p.setString(_keyPassword, pwd);

  // 是否已登录
  static bool isLoggedIn() {
    final token = getToken();
    return token != null && token.isNotEmpty;
  }

  // 清除登录信息
  static String? getString(String key) => _p.getString(key);
  static Future<bool> setString(String key, String value) =>
      _p.setString(key, value);
  static Future<bool> remove(String key) => _p.remove(key);

  static bool isMobileOnboardingCompleted() =>
      _p.getBool(_keyMobileOnboardingCompleted) ?? false;

  static Future<bool> completeMobileOnboarding() =>
      _p.setBool(_keyMobileOnboardingCompleted, true);

  static Future<bool> resetMobileOnboarding() =>
      _p.remove(_keyMobileOnboardingCompleted);

  static String _opencodeSkippedVersionKey(String machineId) =>
      '$_opencodeSkippedVersionPrefix${machineId.trim()}';

  static String? getSkippedOpencodeVersion(String machineId) {
    final key = _opencodeSkippedVersionKey(machineId);
    final value = _p.getString(key)?.trim();
    if (value == null || value.isEmpty) {
      return null;
    }
    return value;
  }

  static Future<bool> setSkippedOpencodeVersion(
    String machineId,
    String version,
  ) {
    return _p.setString(_opencodeSkippedVersionKey(machineId), version.trim());
  }

  static Future<bool> clearSkippedOpencodeVersion(String machineId) {
    return _p.remove(_opencodeSkippedVersionKey(machineId));
  }

  static Future<void> clearAuth() async {
    final previous = authIdentity;
    await _p.remove(_keyToken);
    await _p.remove(_keyOperatorKey);
    await _p.remove(_keyUsername);
    await _p.remove(_keyDisplayName);
    await _p.remove(_keyPassword);
    if (previous.isNotEmpty) _authIdentityChanges.add('');
  }
}
