import 'dart:convert';

import '../../../core/config/api_client.dart';
import 'project_memory_model.dart';
import 'project_memory_settings_model.dart';

class ProjectMemoryRepository {
  String _base(String machineId, String scopeId) =>
      '/api/devices/${Uri.encodeComponent(machineId)}/projects/${Uri.encodeComponent(scopeId)}';

  Future<ProjectMemoryOverviewModel> getOverview(
    String machineId,
    String scopeId,
  ) async {
    final json = await ApiClient.get('${_base(machineId, scopeId)}/memory');
    return ProjectMemoryOverviewModel.fromJson(json);
  }

  Future<ProjectMemorySettingsModel> getSettings(
    String machineId,
    String scopeId,
  ) async {
    final json = await ApiClient.get(
      '${_base(machineId, scopeId)}/memory/settings',
    );
    return ProjectMemorySettingsModel.fromJson(json);
  }

  Future<ProjectMemorySettingsModel> updateSettings({
    required String machineId,
    required String scopeId,
    required bool? projectEnabled,
    required String projectModel,
    required String projectVariant,
  }) async {
    final json =
        await ApiClient.patch('${_base(machineId, scopeId)}/memory/settings', {
          'project_memory_enabled': projectEnabled,
          'project_memory_model': projectModel.trim(),
          'project_memory_variant': projectVariant.trim(),
        });
    return ProjectMemorySettingsModel.fromJson(json);
  }

  Future<List<ProjectMemoryModel>> listMemories({
    required String machineId,
    required String scopeId,
    String query = '',
    String kind = '',
    String status = '',
    String verification = '',
    bool? locked,
    String beforeId = '',
    DateTime? beforeUpdatedAt,
    int limit = 200,
  }) async {
    final parameters = <String, String>{
      if (query.trim().isNotEmpty) 'query': query.trim(),
      if (kind.isNotEmpty) 'kind': kind,
      if (status.isNotEmpty) 'status': status,
      if (verification.isNotEmpty) 'verification': verification,
      if (locked != null) 'locked': '$locked',
      if (beforeId.isNotEmpty) 'before_id': beforeId,
      if (beforeId.isNotEmpty && beforeUpdatedAt != null)
        'before_updated_at': beforeUpdatedAt.toUtc().toIso8601String(),
      'limit': '$limit',
    };
    final path = Uri(
      path: '${_base(machineId, scopeId)}/memories',
      queryParameters: parameters,
    ).toString();
    final values = await ApiClient.getList(path);
    return values
        .whereType<Map>()
        .map(
          (item) => ProjectMemoryModel.fromJson(item.cast<String, dynamic>()),
        )
        .toList(growable: false);
  }

  Future<({ProjectMemoryModel memory, List<ProjectMemoryModel> history})>
  getMemory(String machineId, String scopeId, String memoryId) async {
    final json = await ApiClient.get(
      '${_base(machineId, scopeId)}/memories/${Uri.encodeComponent(memoryId)}',
    );
    final memoryJson = json['memory'] is Map
        ? (json['memory'] as Map).cast<String, dynamic>()
        : json;
    final history = (json['history'] as List<dynamic>? ?? const [])
        .whereType<Map>()
        .map(
          (item) => ProjectMemoryModel.fromJson(item.cast<String, dynamic>()),
        )
        .toList(growable: false);
    return (memory: ProjectMemoryModel.fromJson(memoryJson), history: history);
  }

  Future<void> updateMemory({
    required String machineId,
    required String scopeId,
    required ProjectMemoryModel memory,
    String? statement,
    String? kind,
    double? confidence,
    bool? locked,
    String? status,
  }) async {
    await ApiClient.patch(
      '${_base(machineId, scopeId)}/memories/${Uri.encodeComponent(memory.id)}',
      {
        'version': memory.version,
        if (statement != null) 'statement': statement,
        if (kind != null) 'kind': kind,
        if (confidence != null) 'confidence': confidence,
        if (locked != null) 'locked': locked,
        if (status != null) 'status': status,
      },
    );
  }

  Future<void> deleteMemory(
    String machineId,
    String scopeId,
    ProjectMemoryModel memory,
  ) async {
    await ApiClient.delete(
      '${_base(machineId, scopeId)}/memories/${Uri.encodeComponent(memory.id)}',
      body: {'version': memory.version},
    );
  }

  Future<void> resolveMemory(
    String machineId,
    String scopeId,
    ProjectMemoryModel memory,
    String status,
  ) async {
    await ApiClient.post(
      '${_base(machineId, scopeId)}/memories/${Uri.encodeComponent(memory.id)}/resolve',
      {'version': memory.version, 'status': status},
    );
  }

  Future<ProjectMemoryJobModel> organizeNow(
    String machineId,
    String scopeId,
  ) async {
    final json = await ApiClient.post(
      '${_base(machineId, scopeId)}/memory-jobs',
      {'trigger': 'manual'},
    );
    return ProjectMemoryJobModel.fromJson(json);
  }

  Future<String> correctIdentity(
    String machineId,
    String scopeId,
    String action,
  ) async {
    final json = await ApiClient.post('${_base(machineId, scopeId)}/identity', {
      'action': action,
    });
    return json['scope']?.toString() ?? scopeId;
  }

  Future<ProjectMemoryJobModel> getJob(
    String machineId,
    String scopeId,
    String jobId,
  ) async {
    final json = await ApiClient.get(
      '${_base(machineId, scopeId)}/memory-jobs/${Uri.encodeComponent(jobId)}',
    );
    return ProjectMemoryJobModel.fromJson(json);
  }

  Stream<ProjectMemoryJobEventModel> watchJob(
    String machineId,
    String scopeId,
    String jobId,
  ) {
    final path =
        '${_base(machineId, scopeId)}/memory-jobs/${Uri.encodeComponent(jobId)}/events';
    return ApiClient.sse(path).map(
      (payload) => ProjectMemoryJobEventModel.fromJson(
        (jsonDecode(payload) as Map).cast<String, dynamic>(),
      ),
    );
  }
}
