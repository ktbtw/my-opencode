import 'package:chat_codex_app/core/notifications/app_notification_controller.dart';
import 'package:chat_codex_app/core/notifications/app_notification_model.dart';
import 'package:chat_codex_app/core/notifications/app_notification_repository.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('markRead updates a notification without removing its route action', () {
    final controller = AppNotificationController(
      repository: _MemoryNotificationRepository(),
      now: () => DateTime.utc(2026, 8, 22),
    );
    controller.start(
      operationId: 'chat-task:task-1',
      title: 'Agent · 任务已完成',
      message: 'project · done',
      kind: AppNotificationKind.agent,
      scope: AppNotificationScope.local,
      actions: const [
        AppNotificationAction(
          label: '查看对话',
          type: AppNotificationActionType.openRoute,
          payload: {'route': '/chat?agentId=agent-1'},
          primary: true,
        ),
      ],
    );

    controller.markRead('chat-task:task-1');
    final record = controller.state.active.single;
    expect(record.unread, isFalse);
    expect(record.actions.single.payload['route'], '/chat?agentId=agent-1');
  });
}

class _MemoryNotificationRepository implements AppNotificationRepository {
  @override
  Future<AppNotificationSnapshot> load() async =>
      const AppNotificationSnapshot();

  @override
  Future<void> save(AppNotificationSnapshot snapshot) async {}
}
