import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/data/chat_repository.dart';
import 'package:chat_codex_app/features/chat/data/local_chat_store.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_provider.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('GoalSettings restores persisted fields and active runtime', () {
    final startedAt = DateTime.utc(2026, 5, 23, 10);
    final settings = GoalSettings.fromJson({
      'content': '完成发布',
      'enabled': true,
      'optimize_enabled': true,
      'started_at': startedAt.toIso8601String(),
      'elapsed_seconds': 90,
      'updated_at': DateTime.utc(2026, 5, 23, 10, 1).toIso8601String(),
    });

    expect(settings.content, '完成发布');
    expect(settings.enabled, isTrue);
    expect(settings.optimizeEnabled, isTrue);
    expect(settings.maxIterations, GoalSettings.defaultMaxIterations);
    expect(settings.statusLabel, '运行中');
    expect(settings.runtime(DateTime.utc(2026, 5, 23, 10, 2)).inSeconds, 210);
  });

  test('GoalSettings handles paused and empty states', () {
    const paused = GoalSettings(
      content: '继续迁移',
      enabled: false,
      status: GoalSettings.statusPaused,
      elapsedSeconds: 3600,
    );
    const empty = GoalSettings();

    expect(paused.statusLabel, '已暂停');
    expect(paused.runtime(DateTime.utc(2026, 5, 23)).inSeconds, 3600);
    expect(empty.statusLabel, '未设置');
    expect(empty.runtime(DateTime.utc(2026, 5, 23)).inSeconds, 0);
  });

  test('GoalSettings terminal status stops runtime and updates label', () {
    final running = GoalSettings(
      content: '完成目标',
      enabled: true,
      status: GoalSettings.statusRunning,
      startedAt: DateTime.utc(2026, 5, 23, 10),
      elapsedSeconds: 120,
    );

    final completed = running.withTerminalStatus(
      GoalSettings.statusCompleted,
      now: DateTime.utc(2026, 5, 23, 10, 1),
    );
    final failed = running.withTerminalStatus(
      GoalSettings.statusFailed,
      now: DateTime.utc(2026, 5, 23, 10, 1),
    );

    expect(completed.enabled, isFalse);
    expect(completed.status, GoalSettings.statusCompleted);
    expect(completed.statusLabel, '已完成');
    expect(completed.runtime(DateTime.utc(2026, 5, 23, 10, 2)).inSeconds, 180);
    expect(failed.statusLabel, '失败');
  });

  test('GoalSettings resets elapsed runtime when restarted after stop', () {
    final running = GoalSettings(
      content: '继续迁移',
      enabled: true,
      startedAt: DateTime.utc(2026, 5, 23, 10),
    );
    final stopped = running.withEnabled(
      false,
      now: DateTime.utc(2026, 5, 23, 11),
    );

    expect(stopped.enabled, isFalse);
    expect(stopped.elapsedSeconds, 3600);
    expect(stopped.runtime(DateTime.utc(2026, 5, 23, 12)).inSeconds, 3600);

    final restarted = stopped.withEnabled(
      true,
      now: DateTime.utc(2026, 5, 23, 12),
    );

    expect(restarted.enabled, isTrue);
    expect(restarted.elapsedSeconds, 0);
    expect(restarted.startedAt, DateTime.utc(2026, 5, 23, 12));
    expect(restarted.runtime(DateTime.utc(2026, 5, 23, 12)).inSeconds, 0);
    expect(restarted.runtime(DateTime.utc(2026, 5, 23, 12, 0, 5)).inSeconds, 5);
  });

  test('GoalSettings keeps persisted status fields in json round trip', () {
    final startedAt = DateTime.utc(2026, 5, 23, 11, 20);
    final updatedAt = DateTime.utc(2026, 5, 23, 11, 30);
    final restored = GoalSettings.fromJson(
      GoalSettings(
        content: '长期运行目标',
        enabled: true,
        maxIterations: 42,
        optimizeEnabled: true,
        startedAt: startedAt,
        elapsedSeconds: 86400,
        updatedAt: updatedAt,
      ).toJson(),
    );

    expect(restored.content, '长期运行目标');
    expect(restored.enabled, isTrue);
    expect(restored.maxIterations, 42);
    expect(restored.optimizeEnabled, isTrue);
    expect(restored.startedAt?.toUtc(), startedAt);
    expect(restored.elapsedSeconds, 86400);
    expect(restored.updatedAt?.toUtc(), updatedAt);
  });

  test(
    'GoalSettings normalizes max iterations and resets runtime on edits',
    () {
      final active = GoalSettings.fromJson({
        'content': '旧目标',
        'enabled': true,
        'max_iterations': '150',
        'started_at': DateTime.utc(2026, 5, 23, 10).toIso8601String(),
        'elapsed_seconds': 3600,
      });

      expect(active.maxIterations, 100);
      expect(GoalSettings.normalizeMaxIterations('0'), 1);
      expect(GoalSettings.normalizeMaxIterations('abc'), 30);
    },
  );

  test('Goal optimization only runs for new or changed goal drafts', () {
    const current = GoalSettings(
      content: '已经优化后的目标',
      enabled: false,
      maxIterations: 30,
      optimizeEnabled: true,
    );

    expect(
      shouldOptimizeGoalDraft(
        optimizeEnabled: true,
        draft: '已经优化后的目标',
        maxIterations: 30,
        current: current,
      ),
      isFalse,
    );
    expect(
      shouldOptimizeGoalDraft(
        optimizeEnabled: true,
        draft: '用户重新编辑后的目标',
        maxIterations: 30,
        current: current,
      ),
      isTrue,
    );
    expect(
      shouldOptimizeGoalDraft(
        optimizeEnabled: true,
        draft: '已经优化后的目标',
        maxIterations: 40,
        current: current,
      ),
      isTrue,
    );
    expect(
      shouldOptimizeGoalDraft(
        optimizeEnabled: true,
        draft: '已有目标',
        maxIterations: 30,
        current: const GoalSettings(content: '已有目标'),
      ),
      isTrue,
    );
  });

  test('GoalProgressEntry reads nested checkpoint and summary metadata', () {
    final checkpoint = GoalProgressEntry.fromEvent({
      'type': 'goal_checkpoint',
      'metadata': {
        'iteration': 2,
        'max': 5,
        'metadata': {'checkpoint': '已完成前两步'},
      },
    });
    final completed = GoalProgressEntry.fromEvent({
      'type': 'goal_completed',
      'metadata': {
        'iteration': 2,
        'max': 5,
        'metadata': {'summary': '目标完成'},
      },
    });

    expect(checkpoint.title, '保存目标进度');
    expect(checkpoint.detail, '已完成前两步');
    expect(checkpoint.summary, '保存目标进度 2/5：已完成前两步');
    expect(completed.isTerminal, isTrue);
    expect(completed.detail, '目标完成');
  });

  test('TaskEventSnapshot groups goal events into iteration rounds', () async {
    final repo = _FakeGoalRoundRepo();
    repo.emit({'type': 'goal_created', 'iteration': 1, 'max': 2});
    repo.emit({'type': 'delta', 'field': 'text', 'content': '第一轮'});
    repo.emit({'type': 'goal_checkpoint', 'iteration': 1, 'max': 2});
    repo.emit({'type': 'goal_continued', 'iteration': 2, 'max': 2});
    repo.emit({'type': 'delta', 'field': 'reasoning', 'content': '思考'});
    repo.emit({'type': 'delta', 'field': 'text', 'content': '第二轮'});
    repo.emit({'type': 'goal_completed', 'iteration': 2, 'max': 2});
    await repo.close();

    final future = repo.getTaskEventSnapshot('task_goal');
    final snapshot = await future;
    expect(snapshot.text, '第一轮第二轮');
    expect(snapshot.reasoning, '思考');
    expect(snapshot.rounds, hasLength(2));
    expect(snapshot.rounds[0].text, '第一轮');
    expect(snapshot.rounds[0].goalProgress.map((entry) => entry.type), [
      'goal_created',
      'goal_checkpoint',
    ]);
    expect(snapshot.rounds[1].text, '第二轮');
    expect(snapshot.rounds[1].reasoning, '思考');
    expect(snapshot.rounds[1].goalProgress.map((entry) => entry.type), [
      'goal_continued',
      'goal_completed',
    ]);
  });

  test('TaskEventSnapshot tracks compaction status hint', () async {
    final repo = _FakeGoalRoundRepo();
    repo.emit({'type': 'delta', 'field': 'text', 'content': '已有正文'});
    repo.emit({'type': 'compaction_started'});
    await repo.close();

    final snapshot = await repo.getTaskEventSnapshot('task_goal');

    expect(snapshot.statusHint, compactionStatusHint);
    expect(snapshot.rounds, hasLength(1));
    expect(snapshot.rounds.single.statusHint, compactionStatusHint);
  });

  test('TaskEventSnapshot clears compaction status when finished', () async {
    final repo = _FakeGoalRoundRepo();
    repo.emit({'type': 'delta', 'field': 'text', 'content': '已有正文'});
    repo.emit({'type': 'compaction_started'});
    repo.emit({'type': 'compaction_completed'});
    await repo.close();

    final snapshot = await repo.getTaskEventSnapshot('task_goal');

    expect(snapshot.statusHint, isEmpty);
    expect(snapshot.rounds, hasLength(1));
    expect(snapshot.rounds.single.statusHint, isEmpty);
  });

  test(
    'TaskEventSnapshot restores only active final delivery checks',
    () async {
      final activeRepo = _FakeGoalRoundRepo();
      activeRepo.emit({
        'type': 'progress',
        'metadata': {
          'source': 'completion_guard',
          'reason': 'missing_artifact',
        },
      });
      await activeRepo.close();

      final activeSnapshot = await activeRepo.getTaskEventSnapshot('task_goal');
      expect(activeSnapshot.finalDeliveryPhase, FinalDeliveryPhase.validating);

      final completedRepo = _FakeGoalRoundRepo();
      completedRepo.emit({
        'type': 'progress',
        'metadata': {
          'source': 'completion_guard',
          'reason': 'missing_artifact',
        },
      });
      completedRepo.emit({'type': 'completed', 'content': '交付完成'});
      await completedRepo.close();

      final completedSnapshot = await completedRepo.getTaskEventSnapshot(
        'task_goal',
      );
      expect(completedSnapshot.finalDeliveryPhase, FinalDeliveryPhase.idle);
    },
  );

  test('TaskEventSnapshot preserves text and tool display order', () async {
    final repo = _FakeGoalRoundRepo();
    repo.emit({'type': 'delta', 'field': 'text', 'content': '先读文件'});
    repo.emit({
      'type': 'tool_updated',
      'tool': {
        'id': 'tool_1',
        'call_id': 'call_1',
        'tool': 'read',
        'status': 'running',
        'input': {'path': 'a.txt'},
      },
    });
    repo.emit({
      'type': 'tool_updated',
      'tool': {
        'id': 'tool_1',
        'call_id': 'call_1',
        'tool': 'read',
        'status': 'completed',
        'input': {'path': 'a.txt'},
        'output': 'done',
      },
    });
    repo.emit({'type': 'delta', 'field': 'text', 'content': '再总结'});
    await repo.close();

    final snapshot = await repo.getTaskEventSnapshot('task_tool_order');

    expect(snapshot.text, '先读文件再总结');
    expect(snapshot.toolCalls, hasLength(1));
    expect(snapshot.blocks, hasLength(3));
    expect(snapshot.blocks[0].type, ChatMessageBlockType.text);
    expect(snapshot.blocks[0].text, '先读文件');
    expect(snapshot.blocks[1].type, ChatMessageBlockType.tool);
    expect(snapshot.blocks[1].tool?.status, 'completed');
    expect(snapshot.blocks[2].type, ChatMessageBlockType.text);
    expect(snapshot.blocks[2].text, '再总结');
    expect(snapshot.rounds.single.blocks, hasLength(3));
  });

  test('TaskEventSnapshot ignores todowrite tool cards', () async {
    final repo = _FakeGoalRoundRepo();
    repo.emit({'type': 'delta', 'field': 'text', 'content': '开始'});
    repo.emit({
      'type': 'tool_updated',
      'tool': {
        'id': 'tool_todo',
        'call_id': 'call_todo',
        'tool': 'todowrite',
        'status': 'running',
        'input': {
          'todos': [
            {'content': '更新计划', 'status': 'in_progress', 'priority': 'high'},
          ],
        },
      },
    });
    repo.emit({'type': 'delta', 'field': 'text', 'content': '继续'});
    await repo.close();

    final snapshot = await repo.getTaskEventSnapshot('task_todo');

    expect(snapshot.text, '开始继续');
    expect(snapshot.toolCalls, isEmpty);
    expect(snapshot.blocks, hasLength(1));
    expect(snapshot.blocks.single.type, ChatMessageBlockType.text);
    expect(snapshot.blocks.single.text, '开始继续');
    expect(snapshot.rounds.single.toolCalls, isEmpty);
    expect(snapshot.rounds.single.blocks, hasLength(1));
  });

  test('Goal command immediately creates task with goal metadata', () async {
    await _initTestAppLog();
    final repo = _FakeGoalRoundRepo();
    final notifier = ChatNotifier(
      repo: repo,
      agentId: 'agent_test',
      projectId: 'project_test',
    );

    await notifier.sendMessage('/goal 自动完成目标');

    expect(repo.createTaskCount, 1);
    expect(repo.lastText, '自动完成目标');
    expect(repo.lastGoal, '自动完成目标');
    expect(repo.lastGoalMaxIterations, GoalSettings.defaultMaxIterations);
    expect(notifier.state.messages.first.content, '自动完成目标');

    await repo.close();
  });

  test('GoalSettings resets runtime when goal or max iterations changes', () {
    final changed =
        const GoalSettings(
          content: '旧目标',
          enabled: true,
          maxIterations: 30,
          startedAt: null,
          elapsedSeconds: 3600,
        ).withContent(
          '新目标',
          nextMaxIterations: 40,
          now: DateTime.utc(2026, 5, 24, 10),
        );

    expect(changed.content, '新目标');
    expect(changed.maxIterations, 40);
    expect(changed.elapsedSeconds, 0);
    expect(changed.startedAt, DateTime.utc(2026, 5, 24, 10));
  });

  test('ChatNotifier enters and clears the final delivery phase', () async {
    await _initTestAppLog();
    final repo = _FakeGoalRoundRepo();
    final notifier = ChatNotifier(
      repo: repo,
      agentId: 'agent_test',
      projectId: 'project_test',
    );

    await notifier.sendMessage('生成交付文件');
    repo.emit({
      'type': 'progress',
      'message': '任意展示文案',
      'metadata': {'source': 'completion_guard', 'reason': 'missing_artifact'},
    });
    await pumpEventQueue(times: 10);

    var agentMessage = notifier.state.messages.last;
    expect(agentMessage.finalDeliveryPhase, FinalDeliveryPhase.validating);
    expect(agentMessage.state, MessageState.streaming);

    repo.emit({'type': 'completed', 'content': '交付完成'});
    await pumpEventQueue(times: 10);

    agentMessage = notifier.state.messages.last;
    expect(agentMessage.finalDeliveryPhase, FinalDeliveryPhase.idle);
    expect(agentMessage.state, MessageState.done);
    await repo.close();
  });

  test(
    'Goal task renders every iteration as a separate agent bubble',
    () async {
      await _initTestAppLog();

      final repo = _FakeGoalRoundRepo();
      final notifier = ChatNotifier(
        repo: repo,
        agentId: 'agent_test',
        projectId: 'project_test',
      );

      await notifier.sendMessage('开始', goalOverride: '完成目标');
      repo.emit({'type': 'goal_created', 'iteration': 1, 'max': 2});
      repo.emit({'type': 'delta', 'field': 'text', 'content': '第一轮正文'});
      repo.emit({
        'type': 'goal_checkpoint',
        'iteration': 1,
        'max': 2,
        'metadata': {
          'metadata': {'checkpoint': '第一轮完成'},
        },
      });
      repo.emit({'type': 'goal_continued', 'iteration': 2, 'max': 2});
      repo.emit({'type': 'delta', 'field': 'reasoning', 'content': '第二轮思考'});
      repo.emit({'type': 'delta', 'field': 'text', 'content': '第二轮正文'});
      repo.emit({
        'type': 'goal_completed',
        'iteration': 2,
        'max': 2,
        'metadata': {
          'metadata': {'summary': '目标完成'},
        },
      });
      await pumpEventQueue(times: 20);

      final agentMessages = notifier.state.messages
          .where((message) => message.role == MessageRole.agent)
          .toList();
      expect(agentMessages, hasLength(2));
      expect(agentMessages[0].content, '第一轮正文');
      expect(agentMessages[0].state, MessageState.done);
      expect(agentMessages[0].goalProgress.map((entry) => entry.type), [
        'goal_created',
        'goal_checkpoint',
      ]);
      expect(agentMessages[1].content, '第二轮正文');
      expect(agentMessages[1].thinkingContent, '第二轮思考');
      expect(agentMessages[1].goalProgress.map((entry) => entry.type), [
        'goal_continued',
        'goal_completed',
      ]);

      await repo.close();
    },
  );

  test(
    'Goal task keeps thinking after completed event and stream close',
    () async {
      await _initTestAppLog();

      final repo = _FakeGoalRoundRepo();
      final notifier = ChatNotifier(
        repo: repo,
        agentId: 'agent_test',
        projectId: 'project_test',
      );

      await notifier.sendMessage('开始', goalOverride: '完成目标');
      repo.emit({'type': 'goal_created', 'iteration': 1, 'max': 2});
      repo.emit({'type': 'delta', 'field': 'reasoning', 'content': '过程思考'});
      repo.emit({'type': 'delta', 'field': 'text', 'content': '最终正文'});
      repo.emit({
        'type': 'goal_completed',
        'iteration': 1,
        'max': 2,
        'metadata': {
          'metadata': {'summary': '目标完成'},
        },
      });
      repo.taskStatus = TaskStatus.completed;
      repo.taskResult = '最终正文';
      repo.emit({'type': 'completed', 'content': '最终正文'});
      await repo.close();
      await pumpEventQueue(times: 30);

      final agentMessages = notifier.state.messages
          .where((message) => message.role == MessageRole.agent)
          .toList();
      expect(agentMessages, hasLength(1));
      expect(agentMessages.first.content, '最终正文');
      expect(agentMessages.first.thinkingContent, '过程思考');
      expect(agentMessages.first.state, MessageState.done);
    },
  );

  test(
    'Resuming app replaces partial streaming content with final result',
    () async {
      await _initTestAppLog();

      final repo = _FakeGoalRoundRepo();
      final notifier = ChatNotifier(
        repo: repo,
        agentId: 'agent_test',
        projectId: 'project_test',
      );

      await notifier.sendMessage('开始');
      repo.emit({'type': 'delta', 'field': 'reasoning', 'content': '前半段思考'});
      repo.emit({'type': 'delta', 'field': 'text', 'content': '前半段正文'});
      await pumpEventQueue(times: 10);

      repo.taskStatus = TaskStatus.completed;
      repo.taskResult = '前半段正文后半段正文';
      repo.snapshotOverride = const TaskEventSnapshot(
        text: '前半段正文后半段正文',
        reasoning: '前半段思考后半段思考',
      );

      await notifier.resumeActiveTaskIfNeeded();

      final agentMessage = notifier.state.messages
          .where((message) => message.role == MessageRole.agent)
          .single;
      expect(agentMessage.content, '前半段正文后半段正文');
      expect(agentMessage.thinkingContent, '前半段思考后半段思考');
      expect(agentMessage.state, MessageState.done);
      expect(notifier.state.sending, isFalse);

      await repo.close();
    },
  );

  test(
    'Terminal task status clears streaming when event snapshot is unavailable',
    () async {
      await _initTestAppLog();

      final repo = _FakeGoalRoundRepo();
      final notifier = ChatNotifier(
        repo: repo,
        agentId: 'agent_test',
        projectId: 'project_test',
      );

      await notifier.sendMessage('开始');
      repo.taskStatus = TaskStatus.completed;
      repo.taskResult = '服务端最终结果';
      repo.failSnapshotRead = true;

      await notifier.resumeActiveTaskIfNeeded();

      final agentMessage = notifier.state.messages
          .where((message) => message.role == MessageRole.agent)
          .single;
      expect(agentMessage.state, MessageState.done);
      expect(agentMessage.content, '服务端最终结果');
      expect(notifier.state.sending, isFalse);
      expect(notifier.state.taskActive, isFalse);

      await repo.close();
    },
  );

  test(
    'Resume reconciliation reloads the session after transient task reads',
    () async {
      await _initTestAppLog();

      final repo = _FakeGoalRoundRepo()..failTaskReadsRemaining = 2;
      final notifier = ChatNotifier(
        repo: repo,
        agentId: 'agent_test',
        projectId: 'project_test',
      );

      await notifier.sendMessage('开始');
      repo.taskStatus = TaskStatus.completed;
      repo.taskResult = '恢复后的最终结果';
      repo.snapshotOverride = const TaskEventSnapshot(text: '恢复后的最终结果');

      await notifier.reconcileAfterResume();

      final agentMessage = notifier.state.messages
          .where((message) => message.role == MessageRole.agent)
          .single;
      expect(agentMessage.state, MessageState.done);
      expect(agentMessage.content, '恢复后的最终结果');
      expect(notifier.state.sending, isFalse);

      await repo.close();
    },
  );

  test(
    'Image generation model marks streaming bubble with image progress',
    () async {
      await _initTestAppLog();

      final repo = _FakeGoalRoundRepo();
      final notifier = ChatNotifier(
        repo: repo,
        agentId: 'agent_test',
        projectId: 'project_test',
      );
      const model = ModelInfo(
        providerID: 'cheap',
        modelID: 'gpt-image-2',
        name: '超级便宜 / gpt-image-2',
        image: true,
        outputModalities: ['image'],
      );

      await notifier.sendMessage('画一张图', model: model);

      final agentMessage = notifier.state.messages
          .where((message) => message.role == MessageRole.agent)
          .single;
      expect(agentMessage.state, MessageState.streaming);
      expect(agentMessage.imageGeneration, isNotNull);
      expect(agentMessage.imageGeneration!.modelID, 'gpt-image-2');

      repo.taskStatus = TaskStatus.completed;
      repo.taskResult = '图片已生成';
      repo.emit({'type': 'completed', 'content': '图片已生成'});
      await repo.close();
      await pumpEventQueue(times: 20);

      final completed = notifier.state.messages
          .where((message) => message.role == MessageRole.agent)
          .single;
      expect(completed.imageGeneration, isNull);
      expect(completed.state, MessageState.done);
    },
  );

  test(
    'Compaction status is visible during live stream and clears after end',
    () async {
      await _initTestAppLog();

      final repo = _FakeGoalRoundRepo();
      final notifier = ChatNotifier(
        repo: repo,
        agentId: 'agent_test',
        projectId: 'project_test',
      );

      await notifier.sendMessage('开始');
      repo.emit({'type': 'compaction_started'});
      await pumpEventQueue(times: 10);

      var agentMessage = notifier.state.messages
          .where((message) => message.role == MessageRole.agent)
          .single;
      expect(agentMessage.statusHint, compactionStatusHint);
      expect(agentMessage.state, MessageState.streaming);
      expect(notifier.state.sending, isTrue);

      repo.emit({'type': 'compaction_completed'});
      await pumpEventQueue(times: 10);

      agentMessage = notifier.state.messages
          .where((message) => message.role == MessageRole.agent)
          .single;
      expect(agentMessage.statusHint, isEmpty);
      expect(agentMessage.state, MessageState.streaming);

      await repo.close();
    },
  );

  test('Completed event reads context usage from backend metadata', () async {
    await _initTestAppLog();

    final repo = _FakeGoalRoundRepo();
    final notifier = ChatNotifier(
      repo: repo,
      agentId: 'agent_test',
      projectId: 'project_test',
    );

    await notifier.sendMessage('开始');
    repo.emit({
      'type': 'completed',
      'content': '压缩上下文完成',
      'metadata': {
        'usage': {
          'stage': 'compaction_context_ready',
          'context_limit': 100000,
          'compaction_count_tokens': 12500,
          'context_usage_percent': 12.5,
        },
      },
    });
    await pumpEventQueue(times: 10);

    expect(notifier.state.contextUsage?.stage, 'compaction_context_ready');
    expect(notifier.state.contextUsage?.knownContextTokens, 12500);
    expect(notifier.state.contextUsage?.contextUsagePercent, 12.5);
    expect(notifier.state.sending, isFalse);
    expect(notifier.state.taskActive, isFalse);

    await repo.close();
  });

  test('Goal task ignores replayed SSE history after reconnect', () async {
    await _initTestAppLog();

    final repo = _FakeGoalRoundRepo();
    final notifier = ChatNotifier(
      repo: repo,
      agentId: 'agent_test',
      projectId: 'project_test',
    );

    final events = [
      {
        'type': 'goal_created',
        'iteration': 1,
        'max': 2,
        'sent_at': '2026-05-23T10:00:00Z',
      },
      {
        'type': 'delta',
        'field': 'text',
        'content': '第一轮正文',
        'sent_at': '2026-05-23T10:00:01Z',
      },
      {
        'type': 'goal_checkpoint',
        'iteration': 1,
        'max': 2,
        'sent_at': '2026-05-23T10:00:02Z',
        'metadata': {
          'metadata': {'checkpoint': '第一轮完成'},
        },
      },
      {
        'type': 'goal_continued',
        'iteration': 2,
        'max': 2,
        'sent_at': '2026-05-23T10:00:03Z',
      },
      {
        'type': 'delta',
        'field': 'text',
        'content': '第二轮正文',
        'sent_at': '2026-05-23T10:00:04Z',
      },
    ];

    await notifier.sendMessage('开始', goalOverride: '完成目标');
    for (final event in events) {
      repo.emit(event);
    }
    for (final event in events) {
      repo.emit(event);
    }
    await pumpEventQueue(times: 20);

    final agentMessages = notifier.state.messages
        .where((message) => message.role == MessageRole.agent)
        .toList();
    expect(agentMessages, hasLength(2));
    expect(agentMessages[0].content, '第一轮正文');
    expect(agentMessages[1].content, '第二轮正文');

    await repo.close();
  });

  test('Completed goal does not restart on the next normal message', () async {
    await _initTestAppLog();

    final repo = _FakeGoalRoundRepo();
    final notifier = ChatNotifier(
      repo: repo,
      agentId: 'agent_test',
      projectId: 'project_test',
    );

    await notifier.sendMessage('开始', goalOverride: '完成目标');
    repo.emit({'type': 'goal_created', 'iteration': 1, 'max': 2});
    repo.emit({
      'type': 'goal_completed',
      'iteration': 1,
      'max': 2,
      'metadata': {
        'metadata': {'summary': '目标完成'},
      },
    });
    repo.taskStatus = TaskStatus.completed;
    repo.taskResult = '目标完成';
    repo.emit({'type': 'completed', 'content': '目标完成'});
    await pumpEventQueue(times: 30);

    expect(repo.lastGoal, '完成目标');
    expect(notifier.state.sending, isFalse);

    await notifier.sendMessage('普通追问', goalOverride: '完成目标');
    expect(repo.createTaskCount, 2);
    expect(repo.lastText, '普通追问');
    expect(repo.lastGoal, isNull);

    await repo.close();
  });

  test('Goal history restores every iteration as a separate bubble', () async {
    await _initTestAppLog();
    final repo = _FakeGoalHistoryRepo();
    final notifier = ChatNotifier(
      repo: repo,
      agentId: 'agent_test',
      projectId: 'project_test',
    );

    await notifier.loadSession('session_goal');
    await pumpEventQueue(times: 20);

    final agentMessages = notifier.state.messages
        .where((message) => message.role == MessageRole.agent)
        .toList();
    expect(agentMessages, hasLength(2));
    expect(agentMessages[0].content, '历史第一轮');
    expect(agentMessages[0].goalProgress.map((entry) => entry.type), [
      'goal_created',
      'goal_checkpoint',
    ]);
    expect(agentMessages[1].content, '历史第二轮');
    expect(agentMessages[1].thinkingContent, '历史第二轮思考');
    expect(agentMessages[1].goalProgress.map((entry) => entry.type), [
      'goal_continued',
      'goal_completed',
    ]);
  });

  test(
    'Chat history loads latest five tasks and prepends earlier page',
    () async {
      await _initTestAppLog();
      final repo = _FakePagedHistoryRepo();
      final notifier = ChatNotifier(
        repo: repo,
        agentId: 'agent_test',
        projectId: 'project_test',
      );

      await notifier.loadSession('session_page');

      expect(repo.requests, hasLength(1));
      expect(repo.requests.first.limit, 6);
      expect(notifier.state.hasEarlierHistory, isTrue);
      expect(notifier.state.messages.where((m) => m.role == MessageRole.user), [
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 3'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 4'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 5'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 6'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 7'),
      ]);

      final addedMessages = await notifier.loadEarlierSessionHistory();

      expect(addedMessages, 6);
      expect(repo.requests, hasLength(2));
      expect(repo.requests.last.beforeTaskId, 'task_3');
      expect(notifier.state.hasEarlierHistory, isFalse);
      expect(notifier.state.messages.where((m) => m.role == MessageRole.user), [
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 0'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 1'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 2'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 3'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 4'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 5'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 6'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 7'),
      ]);
    },
  );

  test(
    'Cached history merges every delta page and keeps newest tasks',
    () async {
      await _initTestAppLog();
      final cachedTasks = [
        for (var i = 5; i >= 0; i--)
          _goalTaskJson(
            TaskModel(
              taskId: 'task_$i',
              agentId: 'agent_test',
              projectId: 'project_test',
              sessionId: 'session_delta',
              status: TaskStatus.completed,
              result: 'reply $i',
              userText: 'message $i',
              createdAt: DateTime.utc(2026, 6, 7, 12, i),
            ),
          ),
      ];
      final localStore = _FakeLocalChatStore(
        revision: 'revision_5',
        tasks: cachedTasks,
      );
      final repo = _FakeDeltaHistoryRepo();
      final notifier = ChatNotifier(
        repo: repo,
        agentId: 'agent_test',
        projectId: 'project_test',
        localStore: localStore,
      );

      await notifier.loadSession('session_delta');

      expect(repo.requestedRevisions, ['revision_5', 'revision_6']);
      expect(localStore.revision, 'revision_7');
      expect(notifier.state.hasEarlierHistory, isTrue);
      expect(notifier.state.messages.where((m) => m.role == MessageRole.user), [
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 3'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 4'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 5'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 6'),
        isA<ChatMessage>().having((m) => m.content, 'content', 'message 7'),
      ]);
    },
  );

  test(
    'Earlier history stays in its transaction until snapshot hydration finishes',
    () async {
      await _initTestAppLog();
      final repo = _FakeDelayedPagedHistoryRepo();
      final notifier = ChatNotifier(
        repo: repo,
        agentId: 'agent_test',
        projectId: 'project_test',
      );

      final sessionLoad = notifier.loadSession('session_page');
      await Future<void>.delayed(Duration.zero);
      repo.completeSnapshots();
      await sessionLoad;

      repo.resetSnapshots();
      final earlier = notifier.loadEarlierSessionHistory();
      await Future<void>.delayed(Duration.zero);

      expect(notifier.state.loadingEarlierHistory, isTrue);
      repo.completeSnapshots();

      expect(await earlier, 6);
      expect(notifier.state.loadingEarlierHistory, isFalse);
    },
  );

  test('Initial session load waits for snapshot hydration', () async {
    await _initTestAppLog();
    final repo = _FakeDelayedPagedHistoryRepo();
    final notifier = ChatNotifier(
      repo: repo,
      agentId: 'agent_test',
      projectId: 'project_test',
    );
    var completed = false;

    final loading = notifier.loadSession('session_page').then((_) {
      completed = true;
    });
    await Future<void>.delayed(Duration.zero);

    expect(completed, isFalse);
    repo.completeSnapshots();
    await loading;
    expect(completed, isTrue);
  });
}

Future<Directory> _initTestAppLog() async {
  final tempDir = await Directory.systemTemp.createTemp('chat_goal_test_');
  final logFile = File('${tempDir.path}/app.log');
  await logFile.writeAsString('');
  SharedPreferences.setMockInitialValues({
    'app_log_batch_id': 'test_batch',
    'app_current_log_path': logFile.path,
  });
  await AppStorage.init();
  return tempDir;
}

class _FakeGoalRoundRepo extends ChatRepository {
  final _events = StreamController<Map<String, dynamic>>.broadcast();
  final _history = <Map<String, dynamic>>[];
  int createTaskCount = 0;
  String? lastText;
  String? lastGoal;
  int? lastGoalMaxIterations;
  TaskStatus taskStatus = TaskStatus.running;
  String taskResult = '';
  TaskEventSnapshot? snapshotOverride;
  bool failSnapshotRead = false;
  int failTaskReadsRemaining = 0;

  @override
  Future<TaskModel> createTask({
    required String agentId,
    required String projectId,
    required String text,
    String? sessionId,
    List<AttachedFile>? attachedFiles,
    ModelInfo? model,
    String permissionMode = 'ask',
    String? variant,
    String? goal,
    int goalMaxIterations = 30,
    Map<String, String>? extraMetadata,
  }) async {
    createTaskCount += 1;
    lastText = text;
    lastGoal = goal;
    lastGoalMaxIterations = goalMaxIterations;
    return TaskModel(
      taskId: 'task_goal',
      agentId: agentId,
      projectId: projectId,
      sessionId: 'session_goal',
      status: TaskStatus.running,
      createdAt: DateTime.utc(2026, 5, 23, 10),
    );
  }

  @override
  Stream<Map<String, dynamic>> watchTaskEvents(String taskId) async* {
    for (final event in _history) {
      yield event;
    }
    yield* _events.stream;
  }

  @override
  Future<TaskModel> getTask(String taskId) async {
    if (failTaskReadsRemaining > 0) {
      failTaskReadsRemaining -= 1;
      throw StateError('task unavailable');
    }
    return TaskModel(
      taskId: 'task_goal',
      agentId: 'agent_test',
      projectId: 'project_test',
      sessionId: 'session_goal',
      status: taskStatus,
      result: taskResult,
    );
  }

  @override
  Future<TaskEventSnapshot> getTaskEventSnapshot(String taskId) {
    if (failSnapshotRead) {
      return Future<TaskEventSnapshot>.error(
        StateError('snapshot unavailable'),
      );
    }
    final override = snapshotOverride;
    if (override != null) return Future.value(override);
    return super.getTaskEventSnapshot(taskId);
  }

  void emit(Map<String, dynamic> event) {
    _history.add(event);
    _events.add(event);
  }

  Future<void> close() => _events.close();
}

class _FakeGoalHistoryRepo extends ChatRepository {
  @override
  Future<List<TaskModel>> getSessionHistory(
    String sessionId, {
    int limit = 5,
    DateTime? beforeCreatedAt,
    String? beforeTaskId,
  }) {
    return Future.value([
      TaskModel(
        taskId: 'task_goal',
        agentId: 'agent_test',
        projectId: 'project_test',
        sessionId: sessionId,
        status: TaskStatus.completed,
        result: '聚合结果',
        userText: '开始',
        createdAt: DateTime.utc(2026, 5, 23, 10),
      ),
    ]);
  }

  @override
  Future<List<Map<String, dynamic>>> getSessionHistoryJson(
    String sessionId, {
    int limit = 5,
    DateTime? beforeCreatedAt,
    String? beforeTaskId,
  }) async => (await getSessionHistory(
    sessionId,
    limit: limit,
    beforeCreatedAt: beforeCreatedAt,
    beforeTaskId: beforeTaskId,
  )).map(_goalTaskJson).toList(growable: false);

  @override
  Future<TaskEventSnapshot> getTaskEventSnapshot(String taskId) {
    return Future.value(
      TaskEventSnapshot(
        text: '历史第一轮历史第二轮',
        reasoning: '历史第二轮思考',
        goalProgress: [
          const GoalProgressEntry(type: 'goal_created', iteration: 1, max: 2),
          const GoalProgressEntry(
            type: 'goal_checkpoint',
            detail: '第一轮完成',
            iteration: 1,
            max: 2,
          ),
          const GoalProgressEntry(type: 'goal_continued', iteration: 2, max: 2),
          const GoalProgressEntry(
            type: 'goal_completed',
            detail: '目标完成',
            iteration: 2,
            max: 2,
          ),
        ],
        rounds: [
          TaskEventRoundSnapshot(
            text: '历史第一轮',
            goalProgress: [
              const GoalProgressEntry(
                type: 'goal_created',
                iteration: 1,
                max: 2,
              ),
              const GoalProgressEntry(
                type: 'goal_checkpoint',
                detail: '第一轮完成',
                iteration: 1,
                max: 2,
              ),
            ],
            startedAt: DateTime.utc(2026, 5, 23, 10),
          ),
          TaskEventRoundSnapshot(
            text: '历史第二轮',
            reasoning: '历史第二轮思考',
            goalProgress: [
              const GoalProgressEntry(
                type: 'goal_continued',
                iteration: 2,
                max: 2,
              ),
              const GoalProgressEntry(
                type: 'goal_completed',
                detail: '目标完成',
                iteration: 2,
                max: 2,
              ),
            ],
            startedAt: DateTime.utc(2026, 5, 23, 10, 1),
          ),
        ],
      ),
    );
  }
}

typedef _HistoryRequest = ({
  int limit,
  DateTime? beforeCreatedAt,
  String? beforeTaskId,
});

Map<String, dynamic> _goalTaskJson(TaskModel task) => {
  'task_id': task.taskId,
  'agent_id': task.agentId,
  'project_id': task.projectId,
  'session_id': task.sessionId,
  'status': task.status.name,
  'result': task.result,
  'error': task.error,
  'parts': [
    if (task.userText != null) {'type': 'text', 'text': task.userText},
  ],
  'created_at': task.createdAt?.toUtc().toIso8601String(),
  'updated_at': task.createdAt?.toUtc().toIso8601String(),
};

class _FakePagedHistoryRepo extends ChatRepository {
  final requests = <_HistoryRequest>[];
  late final List<TaskModel> _tasks = [
    for (var i = 7; i >= 0; i--)
      TaskModel(
        taskId: 'task_$i',
        agentId: 'agent_test',
        projectId: 'project_test',
        sessionId: 'session_page',
        status: TaskStatus.completed,
        result: 'reply $i',
        userText: 'message $i',
        createdAt: DateTime.utc(2026, 6, 7, 12, i),
      ),
  ];

  @override
  Future<List<TaskModel>> getSessionHistory(
    String sessionId, {
    int limit = 5,
    DateTime? beforeCreatedAt,
    String? beforeTaskId,
  }) async {
    requests.add((
      limit: limit,
      beforeCreatedAt: beforeCreatedAt,
      beforeTaskId: beforeTaskId,
    ));
    Iterable<TaskModel> page = _tasks;
    if (beforeCreatedAt != null && beforeTaskId != null) {
      page = page.where((task) {
        final createdAt = task.createdAt;
        if (createdAt == null) return false;
        if (createdAt.isBefore(beforeCreatedAt)) return true;
        return createdAt.isAtSameMomentAs(beforeCreatedAt) &&
            task.taskId.compareTo(beforeTaskId) < 0;
      });
    }
    return page.take(limit).toList();
  }

  @override
  Future<List<Map<String, dynamic>>> getSessionHistoryJson(
    String sessionId, {
    int limit = 5,
    DateTime? beforeCreatedAt,
    String? beforeTaskId,
  }) async => (await getSessionHistory(
    sessionId,
    limit: limit,
    beforeCreatedAt: beforeCreatedAt,
    beforeTaskId: beforeTaskId,
  )).map(_goalTaskJson).toList(growable: false);

  @override
  Future<TaskEventSnapshot> getTaskEventSnapshot(String taskId) async =>
      const TaskEventSnapshot();
}

class _FakeDelayedPagedHistoryRepo extends _FakePagedHistoryRepo {
  Completer<TaskEventSnapshot> _snapshotGate = Completer<TaskEventSnapshot>();

  @override
  Future<TaskEventSnapshot> getTaskEventSnapshot(String taskId) =>
      _snapshotGate.future;

  void completeSnapshots() {
    if (!_snapshotGate.isCompleted) {
      _snapshotGate.complete(const TaskEventSnapshot());
    }
  }

  void resetSnapshots() {
    _snapshotGate = Completer<TaskEventSnapshot>();
  }
}

class _FakeDeltaHistoryRepo extends ChatRepository {
  final requestedRevisions = <String?>[];

  @override
  Future<Map<String, dynamic>> getSessionTaskDeltaJson(
    String sessionId, {
    String? afterRevision,
    int limit = 200,
  }) async {
    requestedRevisions.add(afterRevision);
    final index = afterRevision == 'revision_5' ? 6 : 7;
    return {
      'revision': 'revision_$index',
      'tasks': [
        _goalTaskJson(
          TaskModel(
            taskId: 'task_$index',
            agentId: 'agent_test',
            projectId: 'project_test',
            sessionId: sessionId,
            status: TaskStatus.completed,
            result: 'reply $index',
            userText: 'message $index',
            createdAt: DateTime.utc(2026, 6, 7, 12, index),
          ),
        ),
      ],
      'has_more': index == 6,
    };
  }

  @override
  Future<List<Map<String, dynamic>>> getSessionHistoryJson(
    String sessionId, {
    int limit = 5,
    DateTime? beforeCreatedAt,
    String? beforeTaskId,
  }) => Future.error(StateError('unexpected full history request'));

  @override
  Future<TaskEventSnapshot> getTaskEventSnapshot(String taskId) async =>
      const TaskEventSnapshot();
}

class _FakeLocalChatStore implements LocalChatStore {
  String? revision;
  final Map<String, Map<String, dynamic>> _tasks;

  _FakeLocalChatStore({
    required this.revision,
    required List<Map<String, dynamic>> tasks,
  }) : _tasks = {for (final task in tasks) task['task_id'].toString(): task};

  @override
  Future<void> init() async {}

  @override
  Future<String?> readRevision({
    required String identity,
    required String sessionId,
  }) async => revision;

  @override
  Future<void> saveRevision({
    required String identity,
    required String sessionId,
    required String revision,
  }) async {
    this.revision = revision;
  }

  @override
  Future<List<CachedChatTask>> readTasks({
    required String identity,
    required String agentId,
    required String projectId,
    required String sessionId,
    int limit = 20,
  }) async {
    final tasks = _tasks.values.toList()
      ..sort(
        (left, right) => right['created_at'].toString().compareTo(
          left['created_at'].toString(),
        ),
      );
    return tasks
        .take(limit)
        .map(
          (task) => CachedChatTask(
            taskId: task['task_id'].toString(),
            createdAt: task['created_at'].toString(),
            payload: Map<String, dynamic>.from(task),
          ),
        )
        .toList(growable: false);
  }

  @override
  Future<void> saveTasks({
    required String identity,
    required String agentId,
    required String projectId,
    required String sessionId,
    required List<Map<String, dynamic>> tasks,
  }) async {
    for (final task in tasks) {
      _tasks[task['task_id'].toString()] = Map<String, dynamic>.from(task);
    }
  }

  @override
  Future<bool> recordEvent({
    required String identity,
    required String taskId,
    required String eventKey,
    required Map<String, dynamic> payload,
  }) async => true;

  @override
  Future<List<Map<String, dynamic>>> readEvents({
    required String identity,
    required String taskId,
  }) async => const [];

  @override
  Future<void> close() async {}
}
