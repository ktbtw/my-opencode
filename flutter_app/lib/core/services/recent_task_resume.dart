import 'dart:convert';

import '../storage/app_storage.dart';

enum RecentTaskResumeKind { waitingUser, completed }

class RecentTaskResume {
  final RecentTaskResumeKind kind;
  final String machineId;
  final String agentId;
  final String projectId;
  final String projectRoot;
  final String projectScopeId;
  final String sessionId;
  final String taskId;
  final DateTime updatedAt;

  const RecentTaskResume({
    required this.kind,
    required this.machineId,
    required this.agentId,
    required this.projectId,
    required this.projectRoot,
    required this.projectScopeId,
    required this.sessionId,
    required this.taskId,
    required this.updatedAt,
  });

  bool get waitingForUser => kind == RecentTaskResumeKind.waitingUser;

  Map<String, dynamic> toJson() => {
    'kind': kind.name,
    'machine_id': machineId,
    'agent_id': agentId,
    'project_id': projectId,
    'project_root': projectRoot,
    'project_scope_id': projectScopeId,
    'session_id': sessionId,
    'task_id': taskId,
    'updated_at': updatedAt.toUtc().toIso8601String(),
  };

  factory RecentTaskResume.fromJson(Map<String, dynamic> json) {
    final kind = switch (json['kind']) {
      'waitingUser' => RecentTaskResumeKind.waitingUser,
      'completed' => RecentTaskResumeKind.completed,
      _ => throw const FormatException('未知的最近任务类型'),
    };
    final updatedAt = DateTime.tryParse(json['updated_at']?.toString() ?? '');
    if (updatedAt == null) throw const FormatException('最近任务时间无效');
    return RecentTaskResume(
      kind: kind,
      machineId: json['machine_id']?.toString() ?? '',
      agentId: json['agent_id']?.toString() ?? '',
      projectId: json['project_id']?.toString() ?? '',
      projectRoot: json['project_root']?.toString() ?? '',
      projectScopeId: json['project_scope_id']?.toString() ?? '',
      sessionId: json['session_id']?.toString() ?? '',
      taskId: json['task_id']?.toString() ?? '',
      updatedAt: updatedAt,
    );
  }
}

class RecentTaskResumeStore {
  static String get _key =>
      'recent_task_resume_v1:${Uri.encodeComponent(AppStorage.storageIdentity)}';

  static RecentTaskResume? load() {
    final raw = AppStorage.getString(_key);
    if (raw == null || raw.trim().isEmpty) return null;
    try {
      final decoded = jsonDecode(raw);
      if (decoded is! Map) return null;
      return RecentTaskResume.fromJson(decoded.cast<String, dynamic>());
    } catch (_) {
      return null;
    }
  }

  static Future<void> save(RecentTaskResume value) async {
    await AppStorage.setString(_key, jsonEncode(value.toJson()));
  }

  static Future<void> clearIfTask(String taskId) async {
    final current = load();
    if (current?.taskId == taskId && taskId.trim().isNotEmpty) {
      await AppStorage.remove(_key);
    }
  }
}
