import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:http/http.dart' as http;

import '../../../core/config/app_config.dart';
import '../domain/session_summary.dart';

final sessionsProvider = FutureProvider<List<SessionSummary>>((ref) async {
  final uri = Uri.parse('${AppConfig.apiBaseUrl}/api/sessions?limit=50');
  final response = await http.get(uri);
  if (response.statusCode != 200) {
    throw Exception('加载会话失败: ${response.statusCode}');
  }

  final raw = jsonDecode(response.body) as List<dynamic>;
  return raw
      .whereType<Map<String, dynamic>>()
      .map(SessionSummary.fromJson)
      .toList();
});
