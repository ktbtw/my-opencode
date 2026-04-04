import 'dart:convert';
import 'package:http/http.dart' as http;
import '../storage/app_storage.dart';

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

  static Future<Map<String, dynamic>> get(String path) async {
    final uri = Uri.parse('$baseUrl$path');
    final resp = await http.get(uri, headers: _headers());
    return _handle(resp);
  }

  static Future<Map<String, dynamic>> post(
    String path,
    Map<String, dynamic> body, {
    bool withAuth = true,
  }) async {
    final uri = Uri.parse('$baseUrl$path');
    final resp = await http.post(
      uri,
      headers: _headers(withAuth: withAuth),
      body: jsonEncode(body),
    );
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

  // SSE 事件流
  static Stream<String> sse(String path) async* {
    final uri = Uri.parse('$baseUrl$path');
    final client = http.Client();
    try {
      final request = http.Request('GET', uri);
      final token = AppStorage.getToken();
      if (token != null && token.isNotEmpty) {
        request.headers['Authorization'] = 'Bearer $token';
      }
      request.headers['Accept'] = 'text/event-stream';
      final response = await client.send(request);
      await for (final chunk in response.stream.transform(utf8.decoder)) {
        for (final line in chunk.split('\n')) {
          if (line.startsWith('data:')) {
            yield line.substring(5).trim();
          }
        }
      }
    } finally {
      client.close();
    }
  }
}

class ApiException implements Exception {
  final String message;
  final int statusCode;

  const ApiException(this.message, {required this.statusCode});

  @override
  String toString() => message;
}
