import 'dart:convert';

import 'package:http/http.dart' as http;

import '../../../core/config/app_config.dart';
import '../domain/chat_models.dart';

class ChatApi {
  const ChatApi();

  Future<List<SessionInfo>> listSessions({
    required String accessToken,
    required String agentId,
  }) async {
    final response = await http.get(
      Uri.parse(
        '${AppConfig.apiBaseUrl}/api/sessions?agent_id=$agentId&limit=50',
      ),
      headers: _headers(accessToken),
    );
    if (response.statusCode != 200) {
      throw Exception('会话列表加载失败: ${response.statusCode}');
    }
    final raw = jsonDecode(response.body) as List<dynamic>;
    return raw
        .whereType<Map<String, dynamic>>()
        .map(SessionInfo.fromJson)
        .toList();
  }

  Future<List<ChatTurn>> listTurns({
    required String accessToken,
    required String sessionId,
  }) async {
    final response = await http.get(
      Uri.parse(
        '${AppConfig.apiBaseUrl}/api/tasks?session_id=$sessionId&limit=100',
      ),
      headers: _headers(accessToken),
    );
    if (response.statusCode != 200) {
      throw Exception('消息历史加载失败: ${response.statusCode}');
    }
    final raw = jsonDecode(response.body) as List<dynamic>;
    final turns = raw
        .whereType<Map<String, dynamic>>()
        .map(ChatTurn.fromJson)
        .toList();
    turns.sort((a, b) {
      final left = a.createdAt?.millisecondsSinceEpoch ?? 0;
      final right = b.createdAt?.millisecondsSinceEpoch ?? 0;
      return left.compareTo(right);
    });
    return turns;
  }

  Future<ChatTurn> createTurn({
    required String accessToken,
    required String agentId,
    required String projectId,
    required String sessionId,
    required List<TaskPart> parts,
    required String model,
  }) async {
    final response = await http.post(
      Uri.parse('${AppConfig.apiBaseUrl}/api/tasks'),
      headers: {..._headers(accessToken), 'Content-Type': 'application/json'},
      body: jsonEncode({
        'agent_id': agentId,
        'project_id': projectId,
        'session_id': sessionId,
        'parts': parts.map((e) => e.toJson()).toList(),
        'metadata': {'model': model},
      }),
    );
    if (response.statusCode != 202) {
      throw Exception('发送失败: ${response.statusCode}');
    }
    return ChatTurn.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<ChatTurn> getTurn({
    required String accessToken,
    required String taskId,
  }) async {
    final response = await http.get(
      Uri.parse('${AppConfig.apiBaseUrl}/api/tasks/$taskId'),
      headers: _headers(accessToken),
    );
    if (response.statusCode != 200) {
      throw Exception('任务查询失败: ${response.statusCode}');
    }
    return ChatTurn.fromJson(jsonDecode(response.body) as Map<String, dynamic>);
  }

  Future<void> approve({
    required String accessToken,
    required String taskId,
    required String permissionId,
    required String reply,
  }) async {
    final response = await http.post(
      Uri.parse('${AppConfig.apiBaseUrl}/api/tasks/$taskId/approval'),
      headers: {..._headers(accessToken), 'Content-Type': 'application/json'},
      body: jsonEncode({'permission_id': permissionId, 'reply': reply}),
    );
    if (response.statusCode != 202) {
      throw Exception('审批失败: ${response.statusCode}');
    }
  }

  Map<String, String> _headers(String accessToken) => {
    'Authorization': 'Bearer $accessToken',
  };
}
