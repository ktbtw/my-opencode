import 'dart:convert';

import 'package:http/http.dart' as http;

import '../../../core/config/app_config.dart';
import '../domain/operator_profile.dart';

class LoginResult {
  LoginResult({
    required this.accessToken,
    required this.operator,
  });

  final String accessToken;
  final OperatorProfile operator;
}

class AuthApi {
  const AuthApi();

  Future<LoginResult> login({
    required String username,
    required String password,
  }) async {
    final response = await http.post(
      Uri.parse('${AppConfig.apiBaseUrl}/api/auth/login'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({
        'username': username,
        'password': password,
      }),
    );

    if (response.statusCode != 200) {
      throw Exception('登录失败: ${response.statusCode}');
    }

    final json = jsonDecode(response.body) as Map<String, dynamic>;
    return LoginResult(
      accessToken: json['access_token'] as String? ?? '',
      operator: OperatorProfile.fromJson(json['operator'] as Map<String, dynamic>),
    );
  }
}
