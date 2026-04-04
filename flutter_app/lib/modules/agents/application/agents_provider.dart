import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:http/http.dart' as http;

import '../../../core/config/app_config.dart';
import '../domain/agent_summary.dart';

final agentsProvider = FutureProvider<List<AgentSummary>>((ref) async {
  final uri = Uri.parse('${AppConfig.apiBaseUrl}/api/agents');
  final response = await http.get(uri);
  if (response.statusCode != 200) {
    throw Exception('加载 Agents 失败: ${response.statusCode}');
  }

  final raw = jsonDecode(response.body) as List<dynamic>;
  return raw
      .map((item) => AgentSummary.fromJson(item as Map<String, dynamic>))
      .toList()
    ..sort((a, b) => a.agentId.compareTo(b.agentId));
});
