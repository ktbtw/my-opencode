import 'dart:convert';

import 'package:chat_codex_app/core/config/sse_parser.dart';
import 'package:chat_codex_app/core/notifications/app_notification_model.dart';
import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/devices/presentation/completed_agents_provider.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

SseEvent taskEvent({
  required String status,
  String agent = 'a-1',
  String machine = 'machine-a',
  String task = 'task-1',
}) => SseEvent(
  type: 'task.updated',
  data:
      '{"machine_id":"$machine","agent_id":"$agent","task_id":"$task",'
      '"status":"$status"}',
);

SseEvent snapshotEvent(List<Map<String, Object?>> agents) => SseEvent(
  type: 'snapshot',
  data: jsonEncode({
    'devices': [
      {'machine_id': 'machine-a', 'agents': agents},
    ],
  }),
);

AppNotificationRecord taskCompletedNotification({
  String agent = 'a-1',
  String machine = 'machine-a',
}) => AppNotificationRecord(
  id: 'n-1',
  operationId: 'chat-task:task-1',
  title: '任务完成',
  message: '已完成',
  status: AppNotificationStatus.succeeded,
  kind: AppNotificationKind.agent,
  createdAt: DateTime(2026),
  updatedAt: DateTime(2026),
  metadata: {'machine_id': machine, 'agent_id': agent},
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    await AppStorage.init();
  });

  test('任务结束事件即登记，查看对话后清除并落盘', () {
    final notifier = CompletedAgentsNotifier();
    expect(notifier.completedFor('machine-a'), isEmpty);

    notifier.handleEvent(taskEvent(status: 'completed'));
    expect(notifier.completedFor('machine-a'), {'a-1'});

    notifier.markViewed('machine-a', 'a-1');
    expect(notifier.completedFor('machine-a'), isEmpty);
    expect(AppStorage.getString('completed_agents_v1'), '{}');
  });

  test('失败与取消也算跑完，重新开工则从完成里撤回', () {
    final notifier = CompletedAgentsNotifier();

    notifier.handleEvent(taskEvent(status: 'failed'));
    expect(notifier.completedFor('machine-a'), {'a-1'});

    notifier.handleEvent(taskEvent(status: 'cancelled', agent: 'a-2'));
    expect(notifier.completedFor('machine-a'), {'a-1', 'a-2'});

    notifier.handleEvent(taskEvent(status: 'running', task: 'task-9'));
    expect(notifier.completedFor('machine-a'), {'a-2'});
  });

  test('后台期间跑完：回到前台的重连快照能补判', () {
    final notifier = CompletedAgentsNotifier();

    // 进后台前：快照里它还有任务，记下运行态。
    notifier.handleEvent(
      snapshotEvent([
        {'agent_id': 'a-1', 'status': 'online', 'current_task_id': 'task-1'},
      ]),
    );
    expect(notifier.completedFor('machine-a'), isEmpty);

    // 后台期间任务结束（事件收不到），回到前台重连时快照里已经没有任务。
    notifier.handleEvent(
      snapshotEvent([
        {'agent_id': 'a-1', 'status': 'online'},
      ]),
    );
    expect(notifier.completedFor('machine-a'), {'a-1'});
  });

  test('权威记录不被启发式覆盖：掉线保留，重新开工才撤回', () {
    final notifier = CompletedAgentsNotifier();
    notifier.handleEvent(taskEvent(status: 'completed'));
    expect(notifier.completedFor('machine-a'), {'a-1'});

    // 掉线只是"暂时看不见"，不抹掉已登记的完成。
    notifier.handleEvent(
      snapshotEvent([
        {'agent_id': 'a-1', 'status': 'offline'},
      ]),
    );
    expect(notifier.completedFor('machine-a'), {'a-1'});

    // 又开工是显式信号：撤回完成，重新等这一轮结果。
    notifier.handleEvent(
      snapshotEvent([
        {'agent_id': 'a-1', 'status': 'online', 'current_task_id': 'task-2'},
      ]),
    );
    expect(notifier.completedFor('machine-a'), isEmpty);
  });

  test('任务完成通知（通知中心同源）也能登记，非完成类通知不登记', () {
    final notifier = CompletedAgentsNotifier();

    notifier.handleNotification(
      taskCompletedNotification().copyWith(
        status: AppNotificationStatus.running,
      ),
    );
    expect(notifier.completedFor('machine-a'), isEmpty);

    notifier.handleNotification(
      taskCompletedNotification().copyWith(operationId: 'agent-restart:1'),
    );
    expect(notifier.completedFor('machine-a'), isEmpty);

    notifier.handleNotification(taskCompletedNotification());
    expect(notifier.completedFor('machine-a'), {'a-1'});
  });

  test('被杀期间跑完：重开时补拉到的完成通知能登记（Agent 已掉线也算）', () {
    // 死之前：快照里它在跑，运行态落盘。
    CompletedAgentsNotifier().handleEvent(
      snapshotEvent([
        {'agent_id': 'a-1', 'status': 'online', 'current_task_id': 'task-1'},
      ]),
    );

    // 重开：新实例恢复运行态；水合阶段只认"死前在跑"的那条通知。
    final restarted = CompletedAgentsNotifier();
    restarted.handleNotification(
      taskCompletedNotification(),
      requireRunning: true,
    );
    expect(restarted.completedFor('machine-a'), {'a-1'});

    // 与运行态无关的历史完成通知不会因此被翻出来。
    restarted.handleNotification(
      taskCompletedNotification(agent: 'a-9'),
      requireRunning: true,
    );
    expect(restarted.completedFor('machine-a'), {'a-1'});

    // 设备离线也不会抹掉这条权威登记。
    restarted.handleEvent(
      snapshotEvent([
        {'agent_id': 'a-1', 'status': 'offline'},
      ]),
    );
    expect(restarted.completedFor('machine-a'), {'a-1'});
  });

  test('App 被杀后重开：运行态与完成态都从本地恢复', () {
    final first = CompletedAgentsNotifier();
    first.handleEvent(
      snapshotEvent([
        {'agent_id': 'a-1', 'status': 'online', 'current_task_id': 'task-1'},
      ]),
    );
    first.handleEvent(taskEvent(status: 'completed', agent: 'a-2'));

    // 重开：新实例从 AppStorage 恢复，再用重连快照补判被杀期间结束的任务。
    final second = CompletedAgentsNotifier();
    expect(second.completedFor('machine-a'), {'a-2'});
    second.handleEvent(
      snapshotEvent([
        {'agent_id': 'a-1', 'status': 'online'},
        {'agent_id': 'a-2', 'status': 'online'},
      ]),
    );
    expect(second.completedFor('machine-a'), {'a-1', 'a-2'});
  });
}
