import 'dart:convert';
import '../../../core/config/api_client.dart';
import 'chat_model.dart';

class ChatRepository {
  Future<List<SessionModel>> getSessions({
    required String agentId,
    int limit = 20,
  }) async {
    final list = await ApiClient.getList(
      '/api/sessions?agent_id=${Uri.encodeComponent(agentId)}&limit=$limit',
    );
    return list
        .map((s) => SessionModel.fromJson(s as Map<String, dynamic>))
        .toList();
  }

  Future<TaskModel> createTask({
    required String agentId,
    required String projectId,
    required String text,
    String? sessionId,
    List<AttachedFile>? attachedFiles,
    ModelInfo? model,
    String permissionMode = 'ask',
  }) async {
    final parts = <Map<String, dynamic>>[
      {'type': 'text', 'text': text},
      if (attachedFiles != null)
        for (final f in attachedFiles) f.toFilePart(),
    ];
    final metadata = <String, String>{
      'permission_mode': permissionMode,
      if (model != null) 'model': model.metaKey,
    };
    final body = <String, dynamic>{
      'agent_id': agentId,
      'project_id': projectId,
      'parts': parts,
      if (sessionId != null && sessionId.isNotEmpty) 'session_id': sessionId,
      'metadata': metadata,
    };
    final data = await ApiClient.post('/api/tasks', body);
    return TaskModel.fromJson(data);
  }

  Future<TaskModel> getTask(String taskId) async {
    final data = await ApiClient.get('/api/tasks/$taskId');
    return TaskModel.fromJson(data);
  }

  Future<void> cancelTask(String taskId) async {
    await ApiClient.post('/api/tasks/$taskId/cancel', {});
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

  Future<String> getTaskResultFromEvents(String taskId) async {
    try {
      final list = await ApiClient.getList('/api/tasks/$taskId/events');
      final buf = StringBuffer();
      for (final e in list) {
        if (e is! Map<String, dynamic>) continue;
        if (e['type'] == 'delta' && (e['field'] == null || e['field'] == 'text')) {
          buf.write(e['content'] as String? ?? '');
        }
      }
      return buf.toString();
    } catch (_) {
      return '';
    }
  }

  Future<List<TaskModel>> getSessionHistory(String sessionId, {int limit = 50}) async {
    final list = await ApiClient.getList(
      '/api/tasks?session_id=${Uri.encodeComponent(sessionId)}&limit=$limit',
    );
    return list
        .map((t) => TaskModel.fromJson(t as Map<String, dynamic>))
        .toList();
  }

  Future<List<ModelInfo>> getModels() async {
    try {
      final data = await ApiClient.get('/api/models');
      return _parseModels(data);
    } catch (_) {
      return [];
    }
  }

  List<ModelInfo> _parseModels(Map<String, dynamic> data) {
    final result = <ModelInfo>[];
    // opencode /provider 返回 { all: [...], connected: [...], default: {...} }
    final connected = (data['connected'] as List<dynamic>? ?? [])
        .map((e) => e.toString())
        .toSet();
    final allProviders = data['all'] as List<dynamic>? ?? [];

    for (final p in allProviders) {
      if (p is! Map<String, dynamic>) continue;
      final providerID = p['id'] as String? ?? '';
      if (providerID.isEmpty || !connected.contains(providerID)) continue;
      final name = p['name'] as String? ?? providerID;
      final models = p['models'] as Map<String, dynamic>? ?? {};
      for (final entry in models.entries) {
        final m = entry.value;
        if (m is! Map<String, dynamic>) continue;
        final modelID = entry.key;
        final modelName = m['name'] as String? ?? modelID;
        final status = m['status'] as String? ?? 'active';
        if (status == 'deprecated') continue;
        result.add(ModelInfo(
          providerID: providerID,
          modelID: modelID,
          name: '$name / $modelName',
        ));
      }
    }
    return result;
  }
}
