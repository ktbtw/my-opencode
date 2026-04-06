import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:http/http.dart' as http;
import '../storage/app_storage.dart';

// Web 平台条件导入
import 'sse_web.dart' if (dart.library.io) 'sse_io.dart' as sse_impl;

class ApiClient {
  static String get baseUrl => AppStorage.getBaseUrl();

  static Map<String, String> _headers({bool withAuth = true}) {
    final headers = <String, String>{
      'Content-Type': 'application/json',
    };
    if (withAuth) {
      final token = AppStorage.getToken();
      if (token != null && token.isNotEmpty) {
        headers['Authorization'] = 'Bearer $token';
      }
    }
    return headers;
  }

  // 401 自动重登（使用保存的用户名密码）
  static bool _refreshing = false;
  static Future<bool> _tryRefreshToken() async {
    if (_refreshing) return false;
    _refreshing = true;
    try {
      final username = AppStorage.getUsername();
      final password = AppStorage.getPassword();
      if (username == null || password == null) return false;
      final uri = Uri.parse('$baseUrl/api/auth/login');
      final resp = await http.post(
        uri,
        headers: {'Content-Type': 'application/json'},
        body: jsonEncode({'username': username, 'password': password}),
      );
      if (resp.statusCode >= 200 && resp.statusCode < 300) {
        final data = jsonDecode(utf8.decode(resp.bodyBytes)) as Map<String, dynamic>;
        final newToken = data['access_token'] as String?;
        if (newToken != null && newToken.isNotEmpty) {
          await AppStorage.setToken(newToken);
          return true;
        }
      }
      return false;
    } catch (_) {
      return false;
    } finally {
      _refreshing = false;
    }
  }

  static Future<Map<String, dynamic>> get(String path) async {
    final uri = Uri.parse('$baseUrl$path');
    var resp = await http.get(uri, headers: _headers());
    if (resp.statusCode == 401 && await _tryRefreshToken()) {
      resp = await http.get(uri, headers: _headers());
    }
    return _handle(resp);
  }

  static Future<List<dynamic>> getList(String path) async {
    final uri = Uri.parse('$baseUrl$path');
    var resp = await http.get(uri, headers: _headers());
    if (resp.statusCode == 401 && await _tryRefreshToken()) {
      resp = await http.get(uri, headers: _headers());
    }
    final body = utf8.decode(resp.bodyBytes);
    if (resp.statusCode >= 200 && resp.statusCode < 300) {
      if (body.isEmpty) return [];
      final decoded = jsonDecode(body);
      if (decoded is List) return decoded;
      if (decoded is Map) {
        for (final v in decoded.values) {
          if (v is List) return v;
        }
      }
      return [];
    }
    String message = '请求失败 (${resp.statusCode})';
    try {
      final json = jsonDecode(body) as Map<String, dynamic>;
      message = json['error'] as String? ?? json['message'] as String? ?? message;
    } catch (_) {}
    throw ApiException(message, statusCode: resp.statusCode);
  }

  static Future<Map<String, dynamic>> post(
    String path,
    Map<String, dynamic> body, {
    bool withAuth = true,
  }) async {
    final uri = Uri.parse('$baseUrl$path');
    var resp = await http.post(
      uri,
      headers: _headers(withAuth: withAuth),
      body: jsonEncode(body),
    );
    if (resp.statusCode == 401 && withAuth && await _tryRefreshToken()) {
      resp = await http.post(
        uri,
        headers: _headers(withAuth: true),
        body: jsonEncode(body),
      );
    }
    return _handle(resp);
  }

  static Map<String, dynamic> _handle(http.Response resp) {
    final body = utf8.decode(resp.bodyBytes);
    if (resp.statusCode >= 200 && resp.statusCode < 300) {
      if (body.isEmpty) return {};
      return jsonDecode(body) as Map<String, dynamic>;
    }
    String message = '请求失败 (${resp.statusCode})';
    try {
      final json = jsonDecode(body) as Map<String, dynamic>;
      message = json['error'] as String? ??
          json['message'] as String? ??
          message;
    } catch (_) {}
    throw ApiException(message, statusCode: resp.statusCode);
  }

  // SSE 事件流 —— 平台自适应实现
  static Stream<String> sse(String path) {
    final uri = Uri.parse('$baseUrl$path');
    final token = AppStorage.getToken();
    final headers = <String, String>{
      'Accept': 'text/event-stream',
      if (token != null && token.isNotEmpty) 'Authorization': 'Bearer $token',
    };
    return sse_impl.sseStream(uri, headers);
  }
}

class ApiException implements Exception {
  final String message;
  final int statusCode;

  const ApiException(this.message, {required this.statusCode});

  @override
  String toString() => message;
}
