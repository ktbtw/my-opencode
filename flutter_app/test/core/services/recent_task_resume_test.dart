import 'package:flutter_test/flutter_test.dart';
import 'package:chat_codex_app/core/services/recent_task_resume.dart';

void main() {
  test('round trips a waiting user task', () {
    final value = RecentTaskResume(
      kind: RecentTaskResumeKind.waitingUser,
      machineId: 'machine-1',
      agentId: 'agent-1',
      projectId: 'project-1',
      projectRoot: '/work/project-1',
      projectScopeId: 'scope-1',
      sessionId: 'session-1',
      taskId: 'task-1',
      updatedAt: DateTime.utc(2026, 8, 23, 12, 30),
    );

    final decoded = RecentTaskResume.fromJson(value.toJson());

    expect(decoded.kind, RecentTaskResumeKind.waitingUser);
    expect(decoded.machineId, 'machine-1');
    expect(decoded.projectRoot, '/work/project-1');
    expect(decoded.taskId, 'task-1');
    expect(decoded.updatedAt, value.updatedAt);
  });

  test('rejects an unknown task kind', () {
    expect(
      () => RecentTaskResume.fromJson({
        'kind': 'unknown',
        'updated_at': '2026-08-23T12:30:00Z',
      }),
      throwsFormatException,
    );
  });
}
