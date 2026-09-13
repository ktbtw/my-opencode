import 'package:chat_codex_app/core/notifications/app_notification_model.dart';
import 'package:chat_codex_app/core/notifications/app_notification_redactor.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('foreground chat matches only its own completed task notification', () {
    const context = AppNotificationChatContext(
      machineId: 'machine-1',
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    final now = DateTime.utc(2026, 8, 24);
    final record = AppNotificationRecord(
      id: 'chat-task_task-1',
      operationId: 'chat-task:task-1',
      title: 'Agent · 任务已完成',
      message: '完成',
      status: AppNotificationStatus.succeeded,
      kind: AppNotificationKind.agent,
      metadata: const {
        'machine_id': 'machine-1',
        'agent_id': 'agent-1',
        'project_id': 'project-1',
      },
      createdAt: now,
      updatedAt: now,
    );

    expect(context.matchesCompletion(record), isTrue);
    expect(
      const AppNotificationChatContext(
        machineId: 'machine-2',
        agentId: 'agent-1',
        projectId: 'project-1',
      ).matchesCompletion(record),
      isFalse,
    );
    expect(
      context.matchesCompletion(
        record.copyWith(status: AppNotificationStatus.running),
      ),
      isFalse,
    );
  });

  test('foreground chat matches legacy route-only records', () {
    const context = AppNotificationChatContext(
      machineId: 'machine-1',
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    final now = DateTime.utc(2026, 8, 24);
    final record = AppNotificationRecord(
      id: 'chat-task_task-2',
      operationId: 'chat-task:task-2',
      title: 'Agent · 任务已完成',
      message: '完成',
      status: AppNotificationStatus.succeeded,
      kind: AppNotificationKind.agent,
      actions: const [
        AppNotificationAction(
          label: '查看对话',
          type: AppNotificationActionType.openRoute,
          payload: {
            'route':
                '/chat?machineId=machine-1&agentId=agent-1&projectId=project-1',
          },
        ),
      ],
      createdAt: now,
      updatedAt: now,
    );

    expect(context.matchesCompletion(record), isTrue);
  });

  test('notification JSON round-trip keeps operation state', () {
    final now = DateTime.utc(2026, 8, 16, 10);
    final record = AppNotificationRecord(
      id: 'notice-1',
      operationId: 'agent:create:1',
      title: '正在创建 Agent',
      message: '正在检查环境',
      status: AppNotificationStatus.running,
      progressMode: AppNotificationProgressMode.determinate,
      displayStyle: AppNotificationDisplayStyle.stages,
      kind: AppNotificationKind.agent,
      scope: AppNotificationScope.synced,
      attention: AppNotificationAttention.userAction,
      progress: 0.42,
      stages: const [
        AppNotificationStage(label: '连接设备', completed: true),
        AppNotificationStage(label: '检查环境', active: true),
      ],
      actions: const [
        AppNotificationAction(
          label: '查看 Agent',
          type: AppNotificationActionType.openRoute,
          payload: {'route': '/devices/1'},
          primary: true,
        ),
      ],
      metadata: const {'machine_id': 'machine-1', 'job_id': 'job-1'},
      sourceLabel: '测试设备',
      createdAt: now,
      updatedAt: now,
    );

    final restored = AppNotificationRecord.fromJson(record.toJson());

    expect(restored.operationId, record.operationId);
    expect(restored.progress, 0.42);
    expect(restored.displayStyle, AppNotificationDisplayStyle.stages);
    expect(restored.stages, hasLength(2));
    expect(restored.actions.single.payload['route'], '/devices/1');
    expect(restored.metadata['job_id'], 'job-1');
    expect(restored.scope, AppNotificationScope.synced);
    expect(restored.attention, AppNotificationAttention.userAction);
  });

  test('redactor hides credentials and complete local paths', () {
    final text = AppNotificationRedactor.text(
      r'token=secret-value file C:\Users\demo\project\build.apk at /Users/demo/project/build.apk',
    );

    expect(text, isNot(contains('secret-value')));
    expect(text, isNot(contains(r'C:\Users\demo\project')));
    expect(text, isNot(contains('/Users/demo/project')));
    expect(text, contains('build.apk'));
  });

  test('unknown serialized enum values use compatible defaults', () {
    final now = DateTime.utc(2026).toIso8601String();
    final restored = AppNotificationRecord.fromJson({
      'id': 'notice-1',
      'operation_id': 'operation-1',
      'title': '通知',
      'message': '内容',
      'status': 'future_status',
      'progress_mode': 'future_mode',
      'kind': 'future_kind',
      'scope': 'future_scope',
      'created_at': now,
      'updated_at': now,
    });

    expect(restored.status, AppNotificationStatus.pending);
    expect(restored.progressMode, AppNotificationProgressMode.none);
    expect(restored.displayStyle, AppNotificationDisplayStyle.automatic);
    expect(restored.kind, AppNotificationKind.system);
    expect(restored.scope, AppNotificationScope.local);
    expect(restored.attention, AppNotificationAttention.none);
  });
}
