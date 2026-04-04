import 'dart:convert';
import '../../../core/config/api_client.dart';
import 'chat_model.dart';

class ChatRepository {
  Future<List<SessionModel>> getSessions({
    required String agentId,
    int limit = 20,
  }) async {
    final data = await ApiClient.get(
      '/api/sessions?agent_id=${Uri.encodeComponent(agentId)}&limit=$limit',
    );
    final list = data['sessions'] as List<dynamic>? ?? [];
    return list
        .map((s) => SessionModel.fromJson(s as Map<String, dynamic>))
        .toList();
  }

  Future<TaskModel> createTask({
    required String agentId,
    required String projectId,
    required String text,
    String? sessionId,
    List<Map<String, dynamic>>? extraParts,
  }) async {
    final parts = <Map<String, dynamic>>[
      {'type': 'text', 'text': text},
      ...?extraParts,
    ];
    final body = <String, dynamic>{
      'agent_id': agentId,
      'project_id': projectId,
      'parts': parts,
      if (sessionId != null && sessionId.isNotEmpty) 'session_id': sessionId,
    };
    final data = await ApiClient.post('/api/tasks', body);
    return TaskModel.fromJson(data);
  }

  Future<TaskModel> getTask(String taskId) async {
    final data = await ApiClient.get('/api/tasks/$taskId');
    return TaskModel.fromJson(data);
  }

  Future<void> submitApproval(String taskId, String reply) async {
    await ApiClient.post('/api/tasks/$taskId/approval', {'reply': reply});
  }

  Stream<Map<String, dynamic>> watchTaskEvents(String taskId) async* {
    await for (final raw in ApiClient.sse('/api/tasks/$taskId/events')) {
      if (raw.isEmpty) continue;
      try {
        final json = jsonDecode(raw) as Map<String, dynamic>;
        yield json;
      } catch (_) {}
    }
  }
}
