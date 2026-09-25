import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/data/chat_repository.dart';
import 'package:chat_codex_app/features/chat/data/local_chat_store.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_provider.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late Directory logDirectory;
  final logDirectories = <Directory>[];

  setUp(() async {
    logDirectory = await Directory.systemTemp.createTemp('chat-queue-log-');
    logDirectories.add(logDirectory);
    final logFile = File('${logDirectory.path}/test.log');
    await logFile.writeAsString('');
    SharedPreferences.setMockInitialValues({
      'app_current_log_path': logFile.path,
      'app_log_batch_id': 'queue-test',
    });
    await AppStorage.init();
  });

  // Diagnostic writes are intentionally asynchronous; keep their directories
  // alive until the suite finishes rather than racing each test's teardown.
  tearDownAll(() async {
    for (final directory in logDirectories) {
      if (await directory.exists()) await directory.delete(recursive: true);
    }
  });

  test(
    'suspending flushes accepted batched text before dedupe advances',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('flush', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'mac',
        projectId: 'p',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      repository.taskEvents('flush').add({
        'type': 'delta',
        'sequence': 1,
        'content': '切后台前的尾字',
      });
      await Future<void>.delayed(Duration.zero);
      notifier.markAppBackgrounded();
      expect(_agentMessage(notifier, 'flush').content, '切后台前的尾字');
    },
  );

  test(
    'a stable recent SSE suffix is replaced by the full recovered body',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('suffix', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'mac',
        projectId: 'p',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      repository.taskEvents('suffix').add({
        'type': 'delta',
        'sequence': 5,
        'content': '尾部',
      });
      await _eventually(
        () => _agentMessage(notifier, 'suffix').content == '尾部',
      );
      notifier.markAppBackgrounded();
      repository.snapshots['suffix'] = const TaskEventSnapshot(
        text: '缺失的开头及尾部',
      );
      await notifier.reconcileAfterResume();
      await _eventually(
        () =>
            _agentMessage(notifier, 'suffix').historyDetails ==
            HistoryDetailsState.none,
      );
      final reply = _agentMessage(notifier, 'suffix');
      expect(reply.content, '缺失的开头及尾部');
      expect(
        reply.blocks
            .where((b) => b.type == ChatMessageBlockType.text)
            .map((b) => b.text)
            .join(),
        reply.content,
      );
    },
  );

  for (final resumed in [false, true]) {
    test(
      'slow active history still restores body after UI deadline resumed=$resumed',
      () async {
        final repository = _FakeChatRepository()
          ..history = [_task('late-body', status: TaskStatus.running)];
        final notifier = ChatNotifier(
          repo: repository,
          agentId: 'mac',
          projectId: 'project',
        )..activeHistoryHydrationTimeout = const Duration(milliseconds: 10);
        addTearDown(() async {
          notifier.dispose();
          await repository.dispose();
        });
        if (resumed) {
          await notifier.loadSession('session-1');
          notifier.markAppBackgrounded();
        }
        repository.snapshotCompleter = Completer<TaskEventSnapshot>();
        if (resumed) {
          await notifier.reconcileAfterResume();
        } else {
          await notifier.loadSession('session-1');
        }
        await Future<void>.delayed(const Duration(milliseconds: 25));
        expect(notifier.state.canStop, isTrue);
        expect(
          _agentMessage(notifier, 'late-body').historyDetails,
          HistoryDetailsState.loading,
        );
        repository.snapshotCompleter!.complete(
          const TaskEventSnapshot(
            text: '后台已经输出的正文',
            blocks: [ChatMessageBlock.text(id: 'restored', text: '后台已经输出的正文')],
          ),
        );
        await _eventually(
          () => _agentMessage(notifier, 'late-body').content == '后台已经输出的正文',
        );
        final reply = _agentMessage(notifier, 'late-body');
        expect(
          reply.blocks
              .where((b) => b.type == ChatMessageBlockType.text)
              .map((b) => b.text)
              .join(),
          reply.content,
        );
        expect(reply.historyDetails, HistoryDetailsState.none);
        expect(reply.streamRecovery, StreamRecovery.following);
      },
    );
  }

  test(
    'incomplete history shows an available prefix without claiming completion',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('prefix', status: TaskStatus.running)]
        ..snapshots['prefix'] = const TaskEventSnapshot(
          complete: false,
          text: '已收到的正文',
        );
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'mac',
        projectId: 'p',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      final reply = _agentMessage(notifier, 'prefix');
      expect(reply.content, '已收到的正文');
      expect(reply.historyDetails, HistoryDetailsState.partial);
      expect(notifier.state.canStop, isTrue);
    },
  );

  test('slow recovery remains isolated between two agents', () async {
    final a = _FakeChatRepository()
      ..history = [_task('a', status: TaskStatus.running)]
      ..snapshotCompleter = Completer<TaskEventSnapshot>();
    final b = _FakeChatRepository()
      ..history = [_task('b', status: TaskStatus.running)]
      ..snapshots['b'] = const TaskEventSnapshot(text: 'B 正文');
    final first = ChatNotifier(repo: a, agentId: 'mac', projectId: 'p1')
      ..activeHistoryHydrationTimeout = const Duration(milliseconds: 10);
    final second = ChatNotifier(repo: b, agentId: 'win', projectId: 'p2');
    addTearDown(() async {
      first.dispose();
      second.dispose();
      await a.dispose();
      await b.dispose();
    });
    await Future.wait([
      first.loadSession('session-1'),
      second.loadSession('session-1'),
    ]);
    expect(_agentMessage(second, 'b').content, 'B 正文');
    a.snapshotCompleter!.complete(const TaskEventSnapshot(text: 'A 正文'));
    await _eventually(() => _agentMessage(first, 'a').content == 'A 正文');
    expect(_agentMessage(second, 'b').content, 'B 正文');
  });

  test(
    'late failed retry status cannot restore a hint on a closed round',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('retry-race', status: TaskStatus.running)]
        ..snapshots['retry-race'] = const TaskEventSnapshot(complete: false);
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      repository.taskReadCompleter = Completer<TaskModel>();
      final retry = notifier.retryHistoryDetails('retry-race');
      final events = repository.taskEvents('retry-race');
      events.add({'type': 'delta', 'content': '第一段'});
      events.add({
        'type': 'input_applied',
        'content': '构建完成',
        'metadata': {
          'queue_item_id': 'job-retry',
          'injection_version': 1,
          'source': 'background_job',
          'context_type': 'backgroundJob',
        },
      });
      events.add({'type': 'delta', 'content': '第二段'});
      await _eventually(() => notifier.state.messages.last.content == '第二段');
      repository.taskReadCompleter!.completeError(StateError('offline'));
      await retry;
      final replies = notifier.state.messages.where(
        (m) => m.role == MessageRole.agent,
      );
      expect(
        replies.every((m) => m.historyDetails == HistoryDetailsState.none),
        isTrue,
      );
      expect(notifier.state.canStop, isTrue);
    },
  );

  for (final complete in [false, true]) {
    test(
      'late history complete=$complete cannot reopen a closed background round',
      () async {
        final repository = _FakeChatRepository()
          ..history = [_task('round-race', status: TaskStatus.running)]
          ..snapshotCompleter = Completer<TaskEventSnapshot>();
        final notifier = ChatNotifier(
          repo: repository,
          agentId: 'agent-1',
          projectId: 'project-1',
        );
        addTearDown(() async {
          notifier.dispose();
          await repository.dispose();
        });
        final load = notifier.loadSession('session-1');
        await _eventually(
          () => repository.snapshotReads.contains('round-race'),
        );
        final events = repository.taskEvents('round-race');
        events.add({'type': 'delta', 'content': '第一段'});
        await _eventually(() => notifier.state.messages.last.content == '第一段');
        events.add({
          'type': 'input_applied',
          'content': '构建完成',
          'metadata': {
            'queue_item_id': 'job-race',
            'injection_version': 1,
            'source': 'background_job',
            'context_type': 'backgroundJob',
          },
        });
        events.add({'type': 'delta', 'content': '第二段'});
        await _eventually(() => notifier.state.messages.last.content == '第二段');
        repository.snapshotCompleter!.complete(
          TaskEventSnapshot(complete: complete, text: '过期正文'),
        );
        await load;
        final replies = notifier.state.messages
            .where((m) => m.role == MessageRole.agent)
            .toList();
        expect(replies.map((m) => m.content), ['第一段', '第二段']);
        expect(replies.first.state, MessageState.done);
        expect(replies.first.historyDetails, HistoryDetailsState.none);
        expect(replies.first.streamRecovery, StreamRecovery.none);
        expect(replies.last.state, MessageState.streaming);
        expect(replies.last.historyDetails, HistoryDetailsState.none);
        expect(notifier.state.canStop, isTrue);
      },
    );
  }

  test(
    'background boundary clears old partial history and retry targets the last round only',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('round-partial', status: TaskStatus.running)]
        ..snapshots['round-partial'] = const TaskEventSnapshot(complete: false);
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      expect(
        notifier.state.messages.last.historyDetails,
        HistoryDetailsState.partial,
      );
      final events = repository.taskEvents('round-partial');
      events.add({'type': 'delta', 'content': '第一段'});
      events.add({
        'type': 'input_applied',
        'content': '构建完成',
        'metadata': {
          'queue_item_id': 'job-partial',
          'injection_version': 1,
          'source': 'background_job',
          'context_type': 'backgroundJob',
        },
      });
      events.add({'type': 'delta', 'content': '第二段'});
      await _eventually(() => notifier.state.messages.last.content == '第二段');
      await notifier.retryHistoryDetails('round-partial');
      final replies = notifier.state.messages
          .where((m) => m.role == MessageRole.agent)
          .toList();
      expect(replies.first.historyDetails, HistoryDetailsState.none);
      expect(replies.last.historyDetails, HistoryDetailsState.partial);
      repository.history = [
        _task('round-partial', status: TaskStatus.completed, result: '第二段'),
      ];
      await notifier.retryHistoryDetails('round-partial');
      final restored = notifier.state.messages
          .where((m) => m.role == MessageRole.agent)
          .toList();
      expect(restored.first.historyDetails, HistoryDetailsState.none);
      expect(restored.last.historyDetails, HistoryDetailsState.partial);
    },
  );

  test(
    'live real totals replace estimates and completion replay is idempotent',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('stats-live', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      final events = repository.taskEvents('stats-live');
      events.add({
        'type': 'delta',
        'field': 'reasoning',
        'content': 'thinking' * 1000,
        'sent_at': '2026-07-28T08:00:01Z',
      });
      await _eventually(
        () => notifier.state.messages.last.thinkingContent.isNotEmpty,
      );
      expect(notifier.state.messages.last.tokenCount, isNull);
      events.add({
        'type': 'delta',
        'content': 'answer',
        'sent_at': '2026-07-28T08:00:02Z',
        'metadata': {'reply_first_text_ms': 1234},
      });
      final usage = {
        'source': 'llm_usage',
        'stage': 'request_usage_ready',
        'message_id': 'one',
        'total_tokens': 150,
      };
      events.add({'type': 'progress', 'metadata': usage});
      events.add({
        'type': 'progress',
        'metadata': {...usage, 'message_id': 'two', 'total_tokens': 250},
      });
      events.add({
        'type': 'completed',
        'content': 'answer',
        'usage': {...usage, 'message_id': 'two', 'total_tokens': 250},
      });
      await _eventually(() => !notifier.state.taskActive);
      expect(notifier.state.messages.last.tokenCount, 400);
      expect(
        notifier.state.messages.last.firstTokenTime,
        const Duration(milliseconds: 1234),
      );
    },
  );

  test(
    'cold history merge keeps cached timing and distinct calls after server reconstruction',
    () async {
      final task = _task(
        'stats-cold',
        status: TaskStatus.completed,
        result: 'answer',
      );
      final repository = _FakeChatRepository()..history = [task];
      final local = _RecordingLocalChatStore();
      final raw = <Map<String, dynamic>>[
        {
          'type': 'delta',
          'content': 'answer',
          'sent_at': '2026-07-28T08:00:02Z',
          'metadata': {'reply_first_text_ms': 2345},
        },
        {
          'type': 'progress',
          'metadata': {
            'source': 'llm_usage',
            'message_id': 'a',
            'total_tokens': 100,
          },
        },
      ];
      for (var i = 0; i < raw.length; i++) {
        await local.recordEvent(
          identity: AppStorage.storageIdentity,
          taskId: task.taskId,
          eventKey: '$i',
          payload: raw[i],
        );
      }
      repository.snapshots[task.taskId] = repository.snapshotFromTaskEvents(
        task.taskId,
        [
          {
            'type': 'delta',
            'content': 'answer',
            '_reconstructed_history': true,
            'sent_at': '2026-09-19T00:00:00Z',
          },
          {
            'type': 'progress',
            'metadata': {
              'source': 'llm_usage',
              'message_id': 'b',
              'total_tokens': 200,
            },
          },
        ],
      );
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
        localStore: local,
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      final reply = notifier.state.messages.last;
      expect(reply.tokenCount, 300);
      expect(reply.firstTokenTime, const Duration(milliseconds: 2345));
    },
  );

  test(
    'cold cache restores context while server history is still pending',
    () async {
      final task = _task(
        'cached-context',
        status: TaskStatus.completed,
        result: '已有回复',
      );
      final local = _RecordingLocalChatStore()
        ..cachedTasks = [
          CachedChatTask(
            taskId: task.taskId,
            createdAt: task.createdAt!.toIso8601String(),
            payload: _taskJson(task),
          ),
        ];
      await local.recordEvent(
        identity: AppStorage.storageIdentity,
        taskId: task.taskId,
        eventKey: 'usage',
        payload: {
          'type': 'progress',
          'sent_at': '2026-09-19T00:00:00Z',
          'metadata': {
            'source': 'llm_usage',
            'context_tokens': 42000,
            'context_limit': 128000,
          },
        },
      );
      final repository = _FakeChatRepository()
        ..historyCompleter = Completer<List<Map<String, dynamic>>>();
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
        localStore: local,
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      final loading = notifier.loadSession('session-1');
      await _eventually(
        () => notifier.state.contextUsage?.knownContextTokens == 42000,
      );
      expect(repository.historyCompleter!.isCompleted, isFalse);
      repository.historyCompleter!.complete([_taskJson(task)]);
      await loading;
      expect(notifier.state.contextUsage?.knownContextTokens, 42000);
    },
  );

  test(
    'history user messages keep their attachments for the preview',
    () async {
      // 历史重建时只取文本会让附件预览消失，这里锁住附件回填。
      final taskJson = {
        'task_id': 'attach-1',
        'agent_id': 'agent-1',
        'project_id': 'project-1',
        'session_id': 'session-1',
        'status': 'completed',
        'result': '收到',
        'parts': [
          {'type': 'text', 'text': '看下附件'},
          {
            'type': 'file',
            'mime': 'text/plain',
            'filename': '新建文本文档.txt',
            'url':
                'data:text/plain;base64,${base64Encode(utf8.encode('hello world'))}',
          },
        ],
        'created_at': '2026-07-28T08:00:00Z',
        'updated_at': '2026-07-28T08:00:00Z',
      };
      final repository = _FakeChatRepository();
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await _loadRawHistory(notifier, repository, [taskJson]);

      final message = notifier.state.messages.firstWhere(
        (item) => item.role == MessageRole.user,
      );
      expect(message.attachedFiles, hasLength(1));
      expect(message.attachedFiles.single.filename, '新建文本文档.txt');
      expect(message.attachedFiles.single.mimeType, 'text/plain');
      expect(
        utf8.decode(base64Decode(message.attachedFiles.single.base64Data)),
        'hello world',
      );
    },
  );

  test('attachment-only history messages are not dropped', () async {
    final repository = _FakeChatRepository();
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });

    await _loadRawHistory(notifier, repository, [
      {
        'task_id': 'attach-only',
        'agent_id': 'agent-1',
        'project_id': 'project-1',
        'session_id': 'session-1',
        'status': 'completed',
        'result': '收到',
        'parts': [
          {
            'type': 'file',
            'mime': 'text/plain',
            'filename': 'notes.txt',
            'url':
                'data:text/plain;base64,${base64Encode(utf8.encode('only file'))}',
          },
        ],
        'created_at': '2026-07-28T08:00:00Z',
        'updated_at': '2026-07-28T08:00:00Z',
      },
    ]);

    final message = notifier.state.messages.firstWhere(
      (item) => item.role == MessageRole.user,
    );
    expect(message.content, '');
    expect(message.attachedFiles, hasLength(1));
    expect(message.attachedFiles.single.filename, 'notes.txt');
  });

  test(
    'cold session restores usage from history without sending and clears it on a new session',
    () async {
      final repository = _FakeChatRepository()
        ..history = [
          _task('task-context', status: TaskStatus.completed, result: '已有回复'),
        ];
      repository.snapshots['task-context'] = const TaskEventSnapshot(
        contextUsage: ContextUsageInfo(
          contextTokens: 42000,
          contextLimit: 128000,
        ),
      );
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      expect(notifier.state.contextUsage?.knownContextTokens, 42000);
      expect(notifier.state.contextUsage?.contextLimit, 128000);
      expect(repository.createTaskCompleter.isCompleted, isFalse);
      notifier.newSession();
      expect(notifier.state.contextUsage, isNull);
    },
  );

  test(
    'hidden conversation releases streams and restores terminal state',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      await _eventually(() => repository.queueEvents.hasListener);
      expect(repository.taskEvents('task-1').hasListener, isTrue);
      notifier.setPageVisible(false);
      await pumpEventQueue();
      expect(repository.taskEvents('task-1').hasListener, isFalse);
      expect(repository.queueEvents.hasListener, isFalse);
      expect(notifier.state.canStop, isTrue);
      await notifier.reconcileAfterResume();
      expect(repository.taskEvents('task-1').hasListener, isFalse);
      repository.history = [
        _task(
          'task-1',
          status: TaskStatus.completed,
          result: 'finished while hidden',
        ),
      ];
      notifier.setPageVisible(true);
      await _eventually(() => !notifier.state.canStop);
      expect(
        _agentMessage(notifier, 'task-1').content,
        'finished while hidden',
      );
      expect(repository.queueEvents.hasListener, isTrue);
    },
  );

  test('burst output batches UI updates without losing text or stop', () async {
    final repository = _FakeChatRepository()
      ..history = [_task('task-1', status: TaskStatus.running)];
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });
    await notifier.loadSession('session-1');
    var updates = 0;
    final remove = notifier.addListener(
      (_) => updates++,
      fireImmediately: false,
    );
    addTearDown(remove);
    final expected = StringBuffer();
    for (var i = 1; i <= 2000; i++) {
      expected.write('字$i');
      repository.taskEvents('task-1').add({
        'type': 'delta',
        'field': 'text',
        'content': '字$i',
        'sequence': i,
      });
    }
    await _eventually(
      () => _agentMessage(notifier, 'task-1').content == expected.toString(),
    );
    expect(updates, lessThan(20));
    expect(notifier.state.canStop, isTrue);
    repository.taskEvents('task-1').add({
      'type': 'cancelled',
      'sequence': 2001,
    });
    await _eventually(() => !notifier.state.canStop);
    expect(_agentMessage(notifier, 'task-1').content, expected.toString());
  });

  test(
    'a second foreground recovery supersedes a stalled old task read',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      notifier.markAppBackgrounded();
      final oldRead = Completer<TaskModel>();
      repository.taskReadCompleter = oldRead;
      final firstResume = notifier.reconcileAfterResume();
      await pumpEventQueue();
      notifier.markAppBackgrounded();
      repository.taskReadCompleter = null;
      repository.history = [
        _task('task-1', status: TaskStatus.completed, result: 'done'),
      ];
      await notifier.reconcileAfterResume();
      expect(notifier.state.canStop, isFalse);
      oldRead.complete(_task('task-1', status: TaskStatus.running));
      await firstResume;
      expect(notifier.state.canStop, isFalse);
      expect(_agentMessage(notifier, 'task-1').content, 'done');
    },
  );

  test('stop feedback is immediate and failure restores retry', () async {
    final repository = _FakeChatRepository()
      ..history = [_task('task-1', status: TaskStatus.running)]
      ..cancelCompleter = Completer<void>();
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });
    await notifier.loadSession('session-1');
    final stopping = notifier.cancelCurrentTask();
    expect(notifier.state.cancelRequested, isTrue);
    expect(_agentMessage(notifier, 'task-1').statusHint, '正在停止');
    await repository.cancelStarted.future;
    repository.cancelCompleter!.completeError(
      StateError('network interrupted'),
    );
    await stopping;
    expect(notifier.state.cancelRequested, isFalse);
    expect(notifier.state.canStop, isTrue);
    expect(_agentMessage(notifier, 'task-1').statusHint, isEmpty);
  });

  test('late cancel response does not resurrect completed task', () async {
    final repository = _FakeChatRepository()
      ..history = [_task('task-1', status: TaskStatus.running)]
      ..cancelCompleter = Completer<void>();
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });
    await notifier.loadSession('session-1');
    final stopping = notifier.cancelCurrentTask();
    await pumpEventQueue();
    repository.history = [
      _task('task-1', status: TaskStatus.completed, result: 'done'),
    ];
    notifier.markAppBackgrounded();
    await notifier.reconcileAfterResume();
    repository.cancelCompleter!.complete();
    await stopping;
    expect(notifier.state.cancelRequested, isFalse);
    expect(notifier.state.canStop, isFalse);
    expect(_agentMessage(notifier, 'task-1').state, MessageState.done);
  });

  test('history loading alone is not stoppable, a restored stream is', () {
    expect(const ChatState(sending: true).canStop, isFalse);
    expect(
      ChatState(
        messages: [
          ChatMessage(
            id: 'agent-task',
            role: MessageRole.agent,
            state: MessageState.streaming,
            content: '',
            taskId: 'task',
            createdAt: DateTime.now(),
          ),
        ],
      ).canStop,
      isTrue,
    );
  });

  test(
    'cold start keeps restored presentation after snapshot and SSE replay',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      expect(
        _agentMessage(notifier, 'task-1').streamRecovery,
        StreamRecovery.following,
      );
      repository.taskEvents('task-1').add({
        'type': 'started',
        'session_id': 'session-1',
      });
      repository.taskEvents('task-1').add({
        'type': 'delta',
        'content': '恢复后的输出',
      });
      await _eventually(
        () => _agentMessage(notifier, 'task-1').content.isNotEmpty,
      );
      expect(
        _agentMessage(notifier, 'task-1').streamRecovery,
        StreamRecovery.following,
      );
      expect(notifier.state.canStop, isTrue);
      await notifier.cancelCurrentTask();
      expect(repository.cancelledTasks, ['task-1']);
    },
  );

  test(
    'returning to the page cannot leave the reply in syncing forever',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('resume-watchdog', status: TaskStatus.running)];
      final notifier =
          ChatNotifier(
              repo: repository,
              agentId: 'agent-1',
              projectId: 'project-1',
            )
            ..resumeRecoveryWatchdogTimeout = const Duration(milliseconds: 20)
            ..recoveryHistoryTimeout = const Duration(milliseconds: 80);
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      notifier.setPageVisible(false);
      repository.snapshotCompleter = Completer<TaskEventSnapshot>();
      notifier.setPageVisible(true);
      await Future<void>.delayed(const Duration(milliseconds: 45));
      final reply = _agentMessage(notifier, 'resume-watchdog');
      // The watchdog first leaves the stale syncing state, then the restarted
      // fake event stream may immediately move it to following.
      expect(reply.streamRecovery, isNot(StreamRecovery.syncing));
      expect(notifier.state.taskActive, isTrue);
      expect(notifier.state.canStop, isTrue);
      repository.snapshotCompleter!.complete(
        const TaskEventSnapshot(complete: false),
      );
    },
  );

  test(
    'resume renders completed body without waiting for frozen history',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      repository.taskEvents('task-1').add({
        'type': 'delta',
        'content': '第一段',
        'sent_at': '2026-07-28T08:00:01Z',
      });
      await _eventually(
        () => _agentMessage(notifier, 'task-1').content == '第一段',
      );
      notifier.markAppBackgrounded();
      repository.history = [
        _task('task-1', status: TaskStatus.completed, result: '第一段，后台已完成的完整回复'),
      ];
      repository.snapshotCompleter = Completer<TaskEventSnapshot>();
      await notifier.reconcileAfterResume().timeout(
        const Duration(milliseconds: 300),
      );
      final reply = _agentMessage(notifier, 'task-1');
      expect(reply.state, MessageState.done);
      expect(reply.content, '第一段，后台已完成的完整回复');
      expect(
        reply.blocks
            .where((b) => b.type == ChatMessageBlockType.text)
            .map((b) => b.text)
            .join(),
        reply.content,
      );
      expect(reply.streamRecovery, StreamRecovery.none);
      expect(reply.historyDetails, HistoryDetailsState.loading);
      expect(notifier.state.canStop, isFalse);
      expect(repository.snapshotCompleter!.isCompleted, isFalse);
      repository.snapshotCompleter!.complete(
        const TaskEventSnapshot(
          statistics: ReplyStatistics(requests: {'m1': 234}),
        ),
      );
      await _eventually(
        () => _agentMessage(notifier, 'task-1').tokenCount == 234,
      );
      expect(_agentMessage(notifier, 'task-1').content, reply.content);
      expect(
        _agentMessage(notifier, 'task-1').historyDetails,
        HistoryDetailsState.none,
      );
    },
  );

  test('partial history never overwrites the already completed body', () async {
    final repository = _FakeChatRepository()
      ..history = [_task('task-1', status: TaskStatus.running)];
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });
    await notifier.loadSession('session-1');
    notifier.markAppBackgrounded();
    repository.history = [
      _task('task-1', status: TaskStatus.completed, result: '完整的最终正文'),
    ];
    repository.snapshots['task-1'] = const TaskEventSnapshot(
      complete: false,
      text: '只读到开头',
      blocks: [ChatMessageBlock.text(id: 'partial', text: '只读到开头')],
    );
    await notifier.reconcileAfterResume();
    await Future<void>.delayed(const Duration(milliseconds: 30));
    final reply = _agentMessage(notifier, 'task-1');
    expect(reply.content, '完整的最终正文');
    expect(
      reply.blocks
          .where((b) => b.type == ChatMessageBlockType.text)
          .map((b) => b.text)
          .join(),
      reply.content,
    );
    expect(notifier.replySyncProgress?.description, contains('历史详情未全部载入'));
    expect(reply.historyDetails, HistoryDetailsState.partial);
    repository.snapshots['task-1'] = const TaskEventSnapshot(text: '完整的最终正文');
    await notifier.retryHistoryDetails('task-1');
    expect(
      _agentMessage(notifier, 'task-1').historyDetails,
      HistoryDetailsState.none,
    );
    expect(_agentMessage(notifier, 'task-1').content, '完整的最终正文');
    expect(notifier.state.canStop, isFalse);
  });

  test(
    'failed resume status read times out and reconnects without a cold start',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('task-1', status: TaskStatus.running)];
      final notifier =
          ChatNotifier(
              repo: repository,
              agentId: 'agent-1',
              projectId: 'project-1',
            )
            ..recoveryStatusTimeout = const Duration(milliseconds: 40)
            ..terminalReconcileInterval = const Duration(milliseconds: 20);
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      notifier.markAppBackgrounded();
      final hanging = Completer<TaskModel>();
      repository.taskReadCompleter = hanging;
      final watches = repository.taskWatchCount;
      await notifier.reconcileAfterResume().timeout(
        const Duration(milliseconds: 300),
      );
      expect(repository.taskWatchCount, greaterThan(watches));
      expect(
        _agentMessage(notifier, 'task-1').streamRecovery,
        StreamRecovery.reconnecting,
      );
      repository.taskReadCompleter = null;
      repository.history = [
        _task('task-1', status: TaskStatus.completed, result: '网络恢复后取到结果'),
      ];
      await _eventually(() => !notifier.state.taskActive);
      expect(_agentMessage(notifier, 'task-1').content, '网络恢复后取到结果');
      // A timed-out response arriving late must not reopen a completed task.
      hanging.complete(_task('task-1', status: TaskStatus.running));
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(notifier.state.taskActive, isFalse);
      expect(_agentMessage(notifier, 'task-1').state, MessageState.done);
    },
  );

  test(
    'active resume bounds slow history and reconnects live events',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      )..recoveryHistoryTimeout = const Duration(milliseconds: 30);
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      notifier.markAppBackgrounded();
      repository.snapshotCompleter = Completer<TaskEventSnapshot>();
      final watches = repository.taskWatchCount;
      await notifier.reconcileAfterResume().timeout(
        const Duration(milliseconds: 300),
      );
      expect(repository.taskWatchCount, greaterThan(watches));
      repository.taskEvents('task-1').add({
        'type': 'completed',
        'content': '实时连接已恢复',
      });
      await _eventually(() => !notifier.state.taskActive);
      repository.snapshotCompleter!.complete(
        const TaskEventSnapshot(text: '过期的内容'),
      );
      await Future<void>.delayed(const Duration(milliseconds: 30));
      expect(_agentMessage(notifier, 'task-1').content, '实时连接已恢复');
    },
  );

  test(
    'slow terminal probes do not stack or overwrite a newer resumed task',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      )..terminalReconcileInterval = const Duration(milliseconds: 10);
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      final stale = Completer<TaskModel>();
      repository.taskReadCompleter = stale;
      final reads = repository.taskReadCount;
      await Future<void>.delayed(const Duration(milliseconds: 80));
      expect(repository.taskReadCount - reads, 1);
      notifier.markAppBackgrounded();
      repository.taskReadCompleter = null;
      repository.history = [
        _task('task-1', status: TaskStatus.completed, result: '正确结果'),
      ];
      await notifier.reconcileAfterResume();
      stale.complete(_task('task-1', status: TaskStatus.failed));
      await Future<void>.delayed(const Duration(milliseconds: 30));
      expect(_agentMessage(notifier, 'task-1').state, MessageState.done);
      expect(_agentMessage(notifier, 'task-1').content, '正确结果');
    },
  );

  test('late terminal details do not leak into a different session', () async {
    final repository = _FakeChatRepository()
      ..history = [_task('task-1', status: TaskStatus.running)];
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });
    await notifier.loadSession('session-1');
    notifier.markAppBackgrounded();
    repository.history = [
      _task('task-1', status: TaskStatus.completed, result: '完成'),
    ];
    repository.snapshotCompleter = Completer<TaskEventSnapshot>();
    await notifier.reconcileAfterResume();
    notifier.newSession();
    repository.snapshotCompleter!.complete(
      const TaskEventSnapshot(text: '旧会话详情'),
    );
    await Future<void>.delayed(const Duration(milliseconds: 30));
    expect(notifier.state.messages, isEmpty);
  });

  test(
    'resume reconciles terminal task while queue snapshot is pending',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      repository.queueSnapshotCompleter = Completer<ChatQueueSnapshot>();
      repository.history = [_task('task-1', status: TaskStatus.failed)];
      notifier.markAppBackgrounded();
      try {
        await notifier.reconcileAfterResume().timeout(
          const Duration(seconds: 1),
        );
        expect(_agentMessage(notifier, 'task-1').state, MessageState.failed);
        expect(notifier.state.canStop, isFalse);
      } finally {
        repository.queueSnapshotCompleter!.complete(
          const ChatQueueSnapshot(sessionId: 'session-1'),
        );
        await Future<void>.delayed(Duration.zero);
      }
    },
  );

  test(
    'rapid second send waits for the first session and enters queue',
    () async {
      final repository = _FakeChatRepository();
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      final first = notifier.sendMessage('第一条');
      await _eventually(() => notifier.state.taskActive);
      final second = notifier.sendMessage(
        '第二条',
        model: _modelA,
        variant: 'high',
      );
      await Future<void>.delayed(const Duration(milliseconds: 20));
      expect(repository.queuedTexts, isEmpty);

      repository.createTaskCompleter.complete(
        _task('task-1', status: TaskStatus.running),
      );

      expect(await first, isTrue);
      expect(await second, isTrue);
      expect(repository.queuedTexts, ['第二条']);
      expect(repository.queuedModels, ['provider/model-a']);
      expect(notifier.state.currentSessionId, 'session-1');
      expect(notifier.state.queue.queuedItems.single.text, '第二条');
    },
  );

  test(
    'an existing session sends through the durable queue even without a local active flag',
    () async {
      final repository = _FakeChatRepository()..history = const [];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      expect(notifier.state.taskActive, isFalse);
      repository.history = [_task('task-active', status: TaskStatus.running)];

      expect(await notifier.sendMessage('恢复后发送'), isTrue);
      expect(repository.queuedTexts, ['恢复后发送']);
      expect(repository.createTaskCompleter.isCompleted, isFalse);
      expect(notifier.state.queue.queuedItems.single.text, '恢复后发送');
    },
  );

  test(
    'dispatched queue snapshot attaches task bubbles and task event stream',
    () async {
      final repository = _FakeChatRepository();
      repository.history = const [];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      repository.queueEvents.add(
        ChatQueueSnapshot(
          sessionId: 'session-1',
          version: 2,
          items: [
            _queueItem(
              'queue-1',
              status: ChatQueueItemStatus.dispatched,
              taskId: 'task-queued',
            ),
          ],
        ),
      );
      await _eventually(
        () => notifier.state.messages.any(
          (message) => message.taskId == 'task-queued',
        ),
      );

      expect(notifier.state.taskActive, isTrue);
      expect(notifier.state.messages.map((message) => message.role), [
        MessageRole.user,
        MessageRole.agent,
      ]);
      expect(notifier.state.messages.first.taskId, 'task-queued');

      repository.taskEvents('task-queued').add({
        'type': 'delta',
        'field': 'text',
        'content': '队列任务回复',
        'sent_at': '2026-07-28T08:00:00Z',
      });
      await _eventually(() => notifier.state.messages.last.content == '队列任务回复');
    },
  );

  test(
    'input_applied creates a user bubble and a distinct next agent round',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      final events = repository.taskEvents('task-1');
      events.add({
        'type': 'delta',
        'field': 'text',
        'content': '第一轮回复',
        'sent_at': '2026-07-28T08:00:00Z',
      });
      events.add({
        'type': 'input_applied',
        'content': '现在插入这条',
        'metadata': {'queue_item_id': 'queue-1', 'injection_version': 1},
        'sent_at': '2026-07-28T08:00:01Z',
      });
      events.add({
        'type': 'delta',
        'field': 'text',
        'content': '第二轮流式回复',
        'sent_at': '2026-07-28T08:00:02Z',
      });
      events.add({
        'type': 'completed',
        'content': '第一轮回复第二轮完整回复',
        'metadata': {'round_result': '第二轮完整回复'},
        'sent_at': '2026-07-28T08:00:03Z',
      });
      await _eventually(() => !notifier.state.taskActive);

      expect(notifier.state.messages.map((message) => message.role), [
        MessageRole.user,
        MessageRole.agent,
        MessageRole.user,
        MessageRole.agent,
      ]);
      expect(notifier.state.messages[1].content, '第一轮回复');
      expect(notifier.state.messages[2].content, '现在插入这条');
      expect(notifier.state.messages[2].taskId, 'task-1');
      expect(notifier.state.messages[2].isInsertedContext, isTrue);
      expect(notifier.state.messages[3].content, '第二轮完整回复');
    },
  );

  test(
    'background job completion is retained as a distinct task context node',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [_task('task-job', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      final events = repository.taskEvents('task-job');
      events.add({
        'type': 'input_applied',
        'content': 'Background shell job completed: build',
        'metadata': {
          'queue_item_id': 'job-1',
          'injection_version': 2,
          'source': 'background_job',
          'context_type': 'backgroundJob',
          'job_status': 'completed',
          'job_title': 'build',
          'command': 'make build',
          'completed_at': 1785225661000,
        },
        'sent_at': '2026-07-28T08:00:01Z',
      });
      await _eventually(
        () => notifier.state.messages.any(
          (message) => message.id == 'background_job_job-1',
        ),
      );

      final context = notifier.state.messages.singleWhere(
        (message) => message.id == 'background_job_job-1',
      );
      expect(context.isInsertedContext, isTrue);
      expect(context.insertedContextSource, 'background_job');
      expect(context.insertedContextType, 'backgroundJob');
      expect(context.insertedContextMetadata['command'], 'make build');
    },
  );

  test(
    'background job continuation streams only into the next agent round',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [_task('task-job', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      final events = repository.taskEvents('task-job');
      events.add({
        'type': 'delta',
        'field': 'text',
        'content': '后台任务结束前的回复',
        'sent_at': '2026-07-28T08:00:00Z',
      });
      events.add({
        'type': 'input_applied',
        'content': 'Background shell job completed: build',
        'metadata': {
          'queue_item_id': 'job-1',
          'injection_version': 1785225661000,
          'source': 'background_job',
          'context_type': 'backgroundJob',
          'job_status': 'completed',
          'job_title': 'build',
        },
        'sent_at': '2026-07-28T08:00:01Z',
      });
      events.add({
        'type': 'delta',
        'field': 'text',
        'content': '后台任务结束后的新正文',
        'sent_at': '2026-07-28T08:00:02Z',
      });
      events.add({
        'type': 'completed',
        'content': '后台任务结束前的回复后台任务结束后的新正文',
        'metadata': {'round_result': '后台任务结束后的新正文'},
        'sent_at': '2026-07-28T08:00:03Z',
      });
      await _eventually(() => !notifier.state.taskActive);

      final taskMessages = notifier.state.messages
          .where((message) => message.taskId == 'task-job')
          .toList();
      expect(taskMessages.map((message) => message.role), [
        MessageRole.user,
        MessageRole.agent,
        MessageRole.user,
        MessageRole.agent,
      ]);
      expect(taskMessages[1].content, '后台任务结束前的回复');
      expect(taskMessages[1].state, MessageState.done);
      expect(taskMessages[2].insertedContextType, 'backgroundJob');
      expect(taskMessages[3].content, '后台任务结束后的新正文');
      expect(taskMessages[3].state, MessageState.done);
    },
  );

  test(
    'historical snapshot restores assistant-input-assistant ordering',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [
        _task('task-history', status: TaskStatus.completed, result: '第一轮第二轮'),
      ];
      repository.snapshots['task-history'] = TaskEventSnapshot(
        text: '第一轮第二轮',
        rounds: [
          const TaskEventRoundSnapshot(text: '第一轮'),
          const TaskEventRoundSnapshot(text: '第二轮'),
        ],
        appliedInputs: [
          TaskInputAppliedInfo(
            queueItemId: 'queue-history',
            content: '历史插入消息',
            injectionVersion: 3,
            afterRoundIndex: 0,
            appliedAt: DateTime.parse('2026-07-28T08:00:01Z'),
          ),
        ],
      );
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      await _eventually(
        () => notifier.state.messages.any(
          (message) => message.id == 'user_insert_queue-history',
        ),
      );

      expect(notifier.state.messages.map((message) => message.content), [
        '原始问题',
        '第一轮',
        '历史插入消息',
        '第二轮',
      ]);
      expect(
        notifier.state.messages.where(
          (message) => message.id == 'user_insert_queue-history',
        ),
        hasLength(1),
      );
      expect(
        notifier.state.messages
            .singleWhere((message) => message.id == 'user_insert_queue-history')
            .isInsertedContext,
        isTrue,
      );
    },
  );

  test(
    'reloaded snapshot keeps the first round tool calls after a second round starts',
    () async {
      const tool = ToolCallInfo(
        id: 'prt_1',
        callId: 'call_abc',
        tool: 'bash',
        status: 'completed',
        output: 'ok',
      );
      final repository = _FakeChatRepository();
      repository.history = [
        _task('task-history', status: TaskStatus.completed, result: '第一轮第二轮'),
      ];
      repository.snapshots['task-history'] = TaskEventSnapshot(
        text: '第一轮第二轮',
        toolCalls: const [tool],
        blocks: const [
          ChatMessageBlock.tool(id: 'tool_call_abc', tool: tool),
          ChatMessageBlock.text(id: 'text_1', text: '第一轮'),
        ],
        rounds: const [
          TaskEventRoundSnapshot(
            text: '第一轮',
            toolCalls: [tool],
            blocks: [
              ChatMessageBlock.tool(id: 'tool_call_abc', tool: tool),
              ChatMessageBlock.text(id: 'text_1', text: '第一轮'),
            ],
          ),
          TaskEventRoundSnapshot(text: '第二轮'),
        ],
        appliedInputs: [
          TaskInputAppliedInfo(
            queueItemId: 'queue-history',
            content: '历史插入消息',
            injectionVersion: 3,
            afterRoundIndex: 0,
            appliedAt: DateTime.parse('2026-07-28T08:00:01Z'),
          ),
        ],
      );
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      await _eventually(
        () => notifier.state.messages.any(
          (message) => message.id == 'user_insert_queue-history',
        ),
      );

      final firstAgent = notifier.state.messages.firstWhere(
        (message) => message.role == MessageRole.agent,
      );
      expect(firstAgent.content, '第一轮');
      expect(firstAgent.toolCalls, hasLength(1));
      expect(firstAgent.toolCalls.single.tool, 'bash');
      expect(firstAgent.toolCalls.single.status, 'completed');
      expect(
        firstAgent.blocks.any((block) => block.tool?.tool == 'bash'),
        isTrue,
      );
      expect(notifier.state.messages.last.content, '第二轮');
      expect(notifier.state.messages.last.toolCalls, isEmpty);
    },
  );

  test(
    'cold start separates unpositioned archive tools from the final body',
    () async {
      const tool = ToolCallInfo(
        id: 'read-1',
        callId: 'read-1',
        tool: 'read',
        status: 'completed',
      );
      final repository = _FakeChatRepository()
        ..history = [
          _task('legacy', status: TaskStatus.completed, result: '完整正文'),
        ];
      repository.snapshots['legacy'] = repository.snapshotFromTaskEvents(
        'legacy',
        [
          {
            'type': 'tool_updated',
            'tool': {'id': tool.id, 'tool': tool.tool, 'status': 'completed'},
          },
          {'type': 'completed', 'content': '完整正文'},
        ],
      );
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      final reply = _agentMessage(notifier, 'legacy');
      expect(reply.content, '完整正文');
      expect(reply.toolCalls.single.tool, 'read');
      expect(reply.toolOrderKnown, isFalse);
      expect(reply.historyDetails, HistoryDetailsState.none);
    },
  );

  test(
    'background enrichment restores original text/tool order after immediate result',
    () async {
      final repository = _FakeChatRepository()
        ..history = [_task('ordered', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });
      await notifier.loadSession('session-1');
      notifier.markAppBackgrounded();
      repository.history = [
        _task('ordered', status: TaskStatus.completed, result: '前文后文完整尾部'),
      ];
      repository.snapshotCompleter = Completer<TaskEventSnapshot>();
      await notifier.reconcileAfterResume();
      expect(_agentMessage(notifier, 'ordered').content, '前文后文完整尾部');
      repository.snapshotCompleter!.complete(
        repository.snapshotFromTaskEvents('ordered', [
          {'type': 'delta', 'content': '前文'},
          {
            'type': 'tool_updated',
            'tool': {'id': 'read', 'tool': 'read', 'status': 'completed'},
          },
          {'type': 'delta', 'content': '后文'},
          {'type': 'completed', 'content': '前文后文完整尾部'},
        ]),
      );
      await _eventually(
        () =>
            _agentMessage(notifier, 'ordered').historyDetails ==
            HistoryDetailsState.none,
      );
      final reply = _agentMessage(notifier, 'ordered');
      expect(reply.toolOrderKnown, isTrue);
      expect(reply.blocks.map((b) => b.type), [
        ChatMessageBlockType.text,
        ChatMessageBlockType.tool,
        ChatMessageBlockType.text,
      ]);
      expect(reply.blocks.first.text, '前文');
      expect(reply.blocks.last.text, '后文完整尾部');
      expect(notifier.state.canStop, isFalse);
    },
  );

  test(
    'history snapshot keeps tools before body instead of dumping them at the bottom',
    () async {
      const tool = ToolCallInfo(
        id: 'prt_1',
        callId: 'call_abc',
        tool: 'bash',
        status: 'completed',
        output: 'ok',
      );
      final repository = _FakeChatRepository();
      repository.history = [
        _task(
          'task-order',
          status: TaskStatus.completed,
          result: '这是一段更长的正式结论正文，用于触发保留当前正文。',
        ),
      ];
      repository.snapshots['task-order'] = const TaskEventSnapshot(
        text: '短正文',
        toolCalls: [tool],
        blocks: [
          ChatMessageBlock.tool(id: 'tool_call_abc', tool: tool),
          ChatMessageBlock.text(id: 'text_1', text: '短正文'),
        ],
      );
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      await _eventually(() {
        final agent = notifier.state.messages.where(
          (message) => message.role == MessageRole.agent,
        );
        return agent.isNotEmpty && agent.last.blocks.isNotEmpty;
      });

      final agent = notifier.state.messages.lastWhere(
        (message) => message.role == MessageRole.agent,
      );
      expect(agent.blocks, isNotEmpty);
      expect(agent.blocks.first.tool?.tool, 'bash');
      expect(
        agent.blocks.any(
          (block) =>
              block.type == ChatMessageBlockType.text &&
              block.text.contains('正式结论'),
        ),
        isTrue,
      );
      expect(agent.blocks.last.type, ChatMessageBlockType.text);
    },
  );

  test(
    'history snapshot keeps interleaved text-tool-text instead of dumping tools under the body',
    () async {
      const tool = ToolCallInfo(
        id: 'prt_1',
        callId: 'call_abc',
        tool: 'bash',
        status: 'completed',
        output: 'ok',
      );
      final repository = _FakeChatRepository();
      repository.history = [
        _task(
          'task-interleave',
          status: TaskStatus.completed,
          result: '第一段正文第二段更长的结论，用来触发保留当前正文。',
        ),
      ];
      repository.snapshots['task-interleave'] = const TaskEventSnapshot(
        text: '第一段正文第二段',
        toolCalls: [tool],
        blocks: [
          ChatMessageBlock.text(id: 'text_1', text: '第一段正文'),
          ChatMessageBlock.tool(id: 'tool_call_abc', tool: tool),
          ChatMessageBlock.text(id: 'text_2', text: '第二段'),
        ],
      );
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      await _eventually(() {
        final agent = notifier.state.messages.where(
          (message) => message.role == MessageRole.agent,
        );
        return agent.isNotEmpty && agent.last.blocks.length >= 3;
      });

      final agent = notifier.state.messages.lastWhere(
        (message) => message.role == MessageRole.agent,
      );
      expect(agent.blocks, hasLength(3));
      expect(agent.blocks[0].type, ChatMessageBlockType.text);
      expect(agent.blocks[0].text, contains('第一段'));
      expect(agent.blocks[1].tool?.tool, 'bash');
      expect(agent.blocks[1].tool?.status, 'completed');
      expect(agent.blocks[2].type, ChatMessageBlockType.text);
    },
  );

  test(
    're-entering a session keeps live text-tool insertion order instead of dumping tools on top',
    () async {
      const firstTool = ToolCallInfo(
        id: 'prt_a',
        callId: 'call_a',
        tool: 'mcp_call',
        status: 'completed',
        output: 'ok',
      );
      const secondTool = ToolCallInfo(
        id: 'prt_b',
        callId: 'call_b',
        tool: 'read',
        status: 'completed',
        output: 'src',
      );
      final localStore = _RecordingLocalChatStore();
      await localStore.init();
      final identity = AppStorage.storageIdentity;
      Future<void> record(String key, Map<String, dynamic> event) {
        return localStore.recordEvent(
          identity: identity,
          taskId: 'task-order-live',
          eventKey: key,
          payload: event,
        );
      }

      await record('d1', {
        'type': 'delta',
        'field': 'text',
        'content': '先核对上限。',
      });
      await record(
        't1',
        _toolUpdated(
          id: 'prt_a',
          callId: 'call_a',
          tool: 'mcp_call',
          status: 'completed',
          sentAt: '2026-07-28T08:00:02Z',
          output: 'ok',
        ),
      );
      await record('d2', {
        'type': 'delta',
        'field': 'text',
        'content': '再推测试包。',
      });
      await record(
        't2',
        _toolUpdated(
          id: 'prt_b',
          callId: 'call_b',
          tool: 'read',
          status: 'completed',
          sentAt: '2026-07-28T08:00:04Z',
          output: 'src',
        ),
      );
      await record('d3', {
        'type': 'delta',
        'field': 'text',
        'content': '测试包已写上。',
      });

      final repository = _FakeChatRepository();
      repository.history = [
        _task(
          'task-order-live',
          status: TaskStatus.completed,
          result: '先核对上限。再推测试包。测试包已写上。这是更长的任务结果。',
        ),
      ];
      repository.snapshots['task-order-live'] = const TaskEventSnapshot(
        text: '先核对上限。再推测试包。测试包已写上。',
        toolCalls: [firstTool, secondTool],
        blocks: [
          ChatMessageBlock.tool(id: 'tool_prt_a', tool: firstTool),
          ChatMessageBlock.tool(id: 'tool_prt_b', tool: secondTool),
          ChatMessageBlock.text(id: 'text_1', text: '先核对上限。再推测试包。测试包已写上。'),
        ],
      );
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
        localStore: localStore,
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
        await localStore.close();
      });

      await notifier.loadSession('session-1');
      await _eventually(() {
        final agent = notifier.state.messages.where(
          (message) => message.role == MessageRole.agent,
        );
        return agent.isNotEmpty &&
            agent.last.blocks.length >= 5 &&
            agent.last.blocks.first.type == ChatMessageBlockType.text;
      });

      final agent = notifier.state.messages.lastWhere(
        (message) => message.role == MessageRole.agent,
      );
      expect(agent.blocks, hasLength(5));
      expect(agent.blocks[0].type, ChatMessageBlockType.text);
      expect(agent.blocks[0].text, contains('先核对上限'));
      expect(agent.blocks[1].tool?.id, 'prt_a');
      expect(agent.blocks[2].type, ChatMessageBlockType.text);
      expect(agent.blocks[2].text, contains('再推测试包'));
      expect(agent.blocks[3].tool?.id, 'prt_b');
      expect(agent.blocks[4].type, ChatMessageBlockType.text);
      expect(agent.blocks[4].text, contains('测试包已写上'));
    },
  );

  test(
    'review: xiaomi rebuild must not stack all tools above the body',
    () async {
      final greps = [
        for (var i = 1; i <= 3; i++)
          ToolCallInfo(
            id: 'prt_g$i',
            callId: 'call_g$i',
            tool: 'grep',
            status: 'completed',
            output: 'ok',
          ),
      ];
      final reads = [
        for (var i = 1; i <= 4; i++)
          ToolCallInfo(
            id: 'prt_r$i',
            callId: 'call_r$i',
            tool: 'read',
            status: i == 2 ? 'error' : 'completed',
            output: 'src',
          ),
      ];
      final liveBlocks = <ChatMessageBlock>[
        const ChatMessageBlock.text(id: 't1', text: '先核对速通 max 协议的出售开关。'),
        ChatMessageBlock.tool(id: 'g1', tool: greps[0]),
        ChatMessageBlock.tool(id: 'g2', tool: greps[1]),
        ChatMessageBlock.tool(id: 'g3', tool: greps[2]),
        const ChatMessageBlock.text(id: 't2', text: '协议出售走同一套背包出售。'),
        ChatMessageBlock.tool(id: 'r1', tool: reads[0]),
        const ChatMessageBlock.text(id: 't3', text: '接着核对副本页品质设置。'),
        ChatMessageBlock.tool(id: 'r2', tool: reads[1]),
        ChatMessageBlock.tool(id: 'r3', tool: reads[2]),
        ChatMessageBlock.tool(id: 'r4', tool: reads[3]),
        const ChatMessageBlock.text(id: 't4', text: '所以副本页去掉金色仍可能卖掉。'),
      ];
      final repository = _FakeChatRepository();
      repository.history = [
        _task(
          'task-xiaomi',
          status: TaskStatus.completed,
          result:
              '先核对速通 max 协议的出售开关。协议出售走同一套背包出售。接着核对副本页品质设置。所以副本页去掉金色仍可能卖掉。更长的任务结果用于触发保留当前正文。',
        ),
      ];
      repository.snapshots['task-xiaomi'] = TaskEventSnapshot(
        text: '先核对速通 max 协议的出售开关。协议出售走同一套背包出售。接着核对副本页品质设置。所以副本页去掉金色仍可能卖掉。',
        toolCalls: [...greps, ...reads],
        blocks: liveBlocks,
      );
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      await _eventually(() {
        final agent = notifier.state.messages.where(
          (message) => message.role == MessageRole.agent,
        );
        return agent.isNotEmpty && agent.last.blocks.length >= 7;
      });

      final blocks = notifier.state.messages
          .lastWhere((message) => message.role == MessageRole.agent)
          .blocks;
      expect(blocks.first.type, ChatMessageBlockType.text);
      expect(blocks.first.text, contains('先核对'));
      expect(blocks[1].tool?.tool, 'grep');
      expect(
        blocks.where((block) => block.tool != null).length,
        greps.length + reads.length,
      );
      expect(blocks.last.type, ChatMessageBlockType.text);
      expect(blocks.last.text, contains('仍可能卖掉'));
      expect(
        blocks.take(3).every((block) => block.tool != null),
        isFalse,
        reason: 'xiaomi flatten: all tools stacked on top',
      );
    },
  );

  test(
    'review: plc110 rebuild must not dump leftover tools under a mixed prefix',
    () async {
      final greps = [
        for (var i = 1; i <= 3; i++)
          ToolCallInfo(
            id: 'prt_g$i',
            callId: 'call_g$i',
            tool: 'grep',
            status: 'completed',
            output: 'ok',
          ),
      ];
      final reads = [
        for (var i = 1; i <= 4; i++)
          ToolCallInfo(
            id: 'prt_r$i',
            callId: 'call_r$i',
            tool: 'read',
            status: 'completed',
            output: 'src',
          ),
      ];
      final localStore = _RecordingLocalChatStore();
      await localStore.init();
      final identity = AppStorage.storageIdentity;
      Future<void> record(String key, Map<String, dynamic> event) {
        return localStore.recordEvent(
          identity: identity,
          taskId: 'task-plc110',
          eventKey: key,
          payload: event,
        );
      }

      await record('d1', {
        'type': 'delta',
        'field': 'text',
        'content': '先核对速通 max 协议。',
      });
      for (var i = 1; i <= 3; i++) {
        await record(
          'g$i',
          _toolUpdated(
            id: 'prt_g$i',
            callId: 'call_g$i',
            tool: 'grep',
            status: 'completed',
            sentAt: '2026-07-28T08:00:0${i}Z',
            output: 'ok',
          ),
        );
      }
      await record('d2', {
        'type': 'delta',
        'field': 'text',
        'content': '协议出售走同一套背包。',
      });

      final repository = _FakeChatRepository();
      repository.history = [
        _task(
          'task-plc110',
          status: TaskStatus.completed,
          result: '先核对速通 max 协议。协议出售走同一套背包。接着核对副本页。所以仍可能卖掉。更长的任务结果。',
        ),
      ];
      repository.snapshots['task-plc110'] = TaskEventSnapshot(
        text: '先核对速通 max 协议。协议出售走同一套背包。接着核对副本页。所以仍可能卖掉。',
        toolCalls: [...greps, ...reads],
        blocks: [
          const ChatMessageBlock.text(id: 't1', text: '先核对速通 max 协议。'),
          ChatMessageBlock.tool(id: 'g1', tool: greps[0]),
          ChatMessageBlock.tool(id: 'g2', tool: greps[1]),
          ChatMessageBlock.tool(id: 'g3', tool: greps[2]),
          const ChatMessageBlock.text(id: 't2', text: '协议出售走同一套背包。'),
          ChatMessageBlock.tool(id: 'r1', tool: reads[0]),
          const ChatMessageBlock.text(id: 't3', text: '接着核对副本页。'),
          ChatMessageBlock.tool(id: 'r2', tool: reads[1]),
          ChatMessageBlock.tool(id: 'r3', tool: reads[2]),
          ChatMessageBlock.tool(id: 'r4', tool: reads[3]),
          const ChatMessageBlock.text(id: 't4', text: '所以仍可能卖掉。'),
        ],
      );
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
        localStore: localStore,
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
        await localStore.close();
      });

      await notifier.loadSession('session-1');
      await _eventually(() {
        final agent = notifier.state.messages.where(
          (message) => message.role == MessageRole.agent,
        );
        return agent.isNotEmpty && agent.last.blocks.length >= 9;
      });

      final blocks = notifier.state.messages
          .lastWhere((message) => message.role == MessageRole.agent)
          .blocks;
      expect(blocks.first.type, ChatMessageBlockType.text);
      expect(blocks.where((block) => block.tool?.tool == 'read').length, 4);
      expect(blocks.last.type, ChatMessageBlockType.text);
      expect(blocks.last.text, contains('仍可能卖掉'));
      final lastFour = blocks.sublist(blocks.length - 4);
      expect(
        lastFour.every((block) => block.tool != null),
        isFalse,
        reason: 'plc110 flatten: leftover reads dumped at the bottom',
      );
    },
  );

  test(
    'resume catch-up completes missed tools without moving them under the body',
    () async {
      const running = ToolCallInfo(
        id: 'prt_1',
        callId: 'call_abc',
        tool: 'bash',
        status: 'running',
      );
      const completed = ToolCallInfo(
        id: 'prt_1',
        callId: 'call_abc',
        tool: 'bash',
        status: 'completed',
        output: 'ok',
      );
      final repository = _FakeChatRepository();
      repository.history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      repository.taskEvents('task-1').add({
        'type': 'delta',
        'field': 'text',
        'content': '第一段',
        'sent_at': '2026-07-28T08:00:01Z',
      });
      repository
          .taskEvents('task-1')
          .add(
            _toolUpdated(
              id: 'prt_1',
              callId: 'call_abc',
              tool: 'bash',
              status: 'running',
              sentAt: '2026-07-28T08:00:02Z',
            ),
          );
      repository.taskEvents('task-1').add({
        'type': 'delta',
        'field': 'text',
        'content': '第二段',
        'sent_at': '2026-07-28T08:00:03Z',
      });
      await _eventually(() {
        final agent = _agentMessage(notifier, 'task-1');
        return agent.content.contains('第二段') &&
            agent.toolCalls.any((tool) => tool.isRunning);
      });

      repository.snapshots['task-1'] = const TaskEventSnapshot(
        toolCalls: [completed],
        blocks: [ChatMessageBlock.tool(id: 'tool_call_abc', tool: completed)],
      );
      await notifier.resumeActiveTaskIfNeeded();
      await _eventually(() {
        final agent = _agentMessage(notifier, 'task-1');
        return agent.toolCalls.any((tool) => tool.status == 'completed');
      });

      final agent = _agentMessage(notifier, 'task-1');
      expect(agent.content, contains('第一段'));
      expect(agent.content, contains('第二段'));
      expect(agent.toolCalls.single.status, 'completed');
      final toolIndex = agent.blocks.indexWhere((block) => block.tool != null);
      final textIndexes = [
        for (var i = 0; i < agent.blocks.length; i++)
          if (agent.blocks[i].type == ChatMessageBlockType.text) i,
      ];
      expect(toolIndex, greaterThan(textIndexes.first));
      expect(toolIndex, lessThan(textIndexes.last));
      expect(running.tool, 'bash');
    },
  );

  test(
    'active snapshot keeps an empty next round after background completion',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [
        _task('task-active-job', status: TaskStatus.running),
      ];
      repository.snapshots['task-active-job'] = TaskEventSnapshot(
        text: '后台任务结束前的回复',
        rounds: const [
          TaskEventRoundSnapshot(text: '后台任务结束前的回复'),
          TaskEventRoundSnapshot(),
        ],
        appliedInputs: [
          TaskInputAppliedInfo(
            queueItemId: 'job-active',
            content: 'Background shell job completed: build',
            injectionVersion: 1785225661000,
            afterRoundIndex: 0,
            contextSource: 'background_job',
            contextType: 'backgroundJob',
          ),
        ],
      );
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      await _eventually(
        () => notifier.state.messages.any(
          (message) => message.id == 'background_job_job-active',
        ),
      );

      final taskMessages = notifier.state.messages
          .where((message) => message.taskId == 'task-active-job')
          .toList();
      expect(taskMessages.map((message) => message.role), [
        MessageRole.user,
        MessageRole.agent,
        MessageRole.user,
        MessageRole.agent,
      ]);
      expect(taskMessages[1].content, '后台任务结束前的回复');
      expect(taskMessages[1].state, MessageState.done);
      expect(taskMessages[3].content, isEmpty);
      expect(taskMessages[3].state, MessageState.streaming);

      repository.taskEvents('task-active-job').add({
        'type': 'delta',
        'field': 'text',
        'content': '后台任务结束后的新正文',
        'sent_at': '2026-07-28T08:00:02Z',
      });
      await _eventually(() {
        final latest = notifier.state.messages.lastWhere(
          (message) => message.taskId == 'task-active-job',
        );
        return latest.content == '后台任务结束后的新正文';
      });

      final updatedTaskMessages = notifier.state.messages
          .where((message) => message.taskId == 'task-active-job')
          .toList();
      expect(updatedTaskMessages[1].content, '后台任务结束前的回复');
      expect(updatedTaskMessages[3].content, '后台任务结束后的新正文');
    },
  );

  test('updating one queued model preserves every other queue item', () async {
    final repository = _FakeChatRepository();
    repository.queueSnapshot = ChatQueueSnapshot(
      sessionId: 'session-1',
      version: 1,
      items: [_queueItem('queue-1'), _queueItem('queue-2')],
    );
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });

    await notifier.loadSession('session-1');
    await _eventually(() => notifier.state.queue.items.length == 2);
    await notifier.updateQueueItemModel(
      notifier.state.queue.items.first,
      model: _modelB,
      variant: 'low',
    );

    expect(notifier.state.queue.items[0].modelRef, 'provider/model-b');
    expect(notifier.state.queue.items[0].variant, 'low');
    expect(notifier.state.queue.items[1].modelRef, 'provider/model-a');
  });

  test(
    'late insert response does not revive an item removed by a newer queue snapshot',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [_task('task-1', status: TaskStatus.running)];
      repository.queueSnapshot = ChatQueueSnapshot(
        sessionId: 'session-1',
        version: 1,
        items: [_queueItem('queue-1')],
      );
      repository.insertQueueItemCompleter = Completer<ChatQueueItem>();
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      await _eventually(
        () =>
            notifier.state.taskActive && notifier.state.queue.items.length == 1,
      );
      final insert = notifier.insertQueueItem(
        notifier.state.queue.items.single,
      );
      await repository.insertQueueItemRequested.future;

      repository.queueEvents.add(
        const ChatQueueSnapshot(sessionId: 'session-1', version: 3),
      );
      await _eventually(() => notifier.state.queue.items.isEmpty);
      repository.insertQueueItemCompleter!.complete(
        _queueItem(
          'queue-1',
          status: ChatQueueItemStatus.inserting,
          version: 2,
        ),
      );
      await insert;

      expect(notifier.state.queue.items, isEmpty);
      expect(notifier.state.queue.version, 3);
    },
  );

  test(
    'initial queue stream error stays loading until the first snapshot succeeds',
    () async {
      final repository = _FakeChatRepository();
      repository.queueSnapshotCompleter = Completer<ChatQueueSnapshot>();
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      await _eventually(() => repository.queueWatchCount == 1);
      repository.queueEvents.addError(StateError('initial stream race'));
      await Future<void>.delayed(const Duration(milliseconds: 20));

      expect(notifier.state.queueLoading, isTrue);
      expect(notifier.state.queueError, isNull);

      repository.queueSnapshotCompleter!.complete(
        const ChatQueueSnapshot(sessionId: 'session-1', version: 1),
      );
      await _eventually(() => !notifier.state.queueLoading);

      expect(notifier.state.queueError, isNull);
      expect(notifier.state.queue.sessionId, 'session-1');
    },
  );

  test(
    'tool completion with swapped ids updates the running call instead of duplicating',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      final events = repository.taskEvents('task-1');
      events.add(
        _toolUpdated(
          id: 'prt_1',
          callId: 'call_abc',
          tool: 'bash',
          status: 'running',
          sentAt: '2026-07-28T08:00:01Z',
        ),
      );
      events.add(
        _toolUpdated(
          id: 'call_abc',
          callId: 'prt_1',
          tool: 'bash',
          status: 'completed',
          output: 'ok',
          sentAt: '2026-07-28T08:00:02Z',
        ),
      );
      await _eventually(() {
        final agent = _agentMessage(notifier, 'task-1');
        return agent.toolCalls.length == 1 &&
            agent.toolCalls.single.status == 'completed';
      });

      final agent = _agentMessage(notifier, 'task-1');
      expect(agent.toolCalls, hasLength(1));
      expect(agent.toolCalls.single.status, 'completed');
      expect(agent.toolCalls.single.output, 'ok');
      expect(agent.blocks.where((block) => block.tool != null), hasLength(1));
      expect(
        agent.blocks.singleWhere((block) => block.tool != null).tool!.status,
        'completed',
      );
    },
  );

  test('a later running event does not regress a completed tool', () async {
    final repository = _FakeChatRepository();
    repository.history = [_task('task-1', status: TaskStatus.running)];
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });

    await notifier.loadSession('session-1');
    final events = repository.taskEvents('task-1');
    events.add(
      _toolUpdated(
        id: 'prt_1',
        callId: 'call_abc',
        tool: 'read',
        status: 'completed',
        output: 'file contents',
        sentAt: '2026-07-28T08:00:01Z',
      ),
    );
    events.add(
      _toolUpdated(
        id: 'prt_1',
        callId: 'call_abc',
        tool: 'read',
        status: 'running',
        sentAt: '2026-07-28T08:00:02Z',
      ),
    );
    await _eventually(() {
      final agent = _agentMessage(notifier, 'task-1');
      return agent.toolCalls.any((tool) => tool.status == 'completed');
    });
    await Future<void>.delayed(const Duration(milliseconds: 30));

    final agent = _agentMessage(notifier, 'task-1');
    expect(agent.toolCalls, hasLength(1));
    expect(agent.toolCalls.single.status, 'completed');
    expect(agent.toolCalls.single.output, 'file contents');
  });

  test('completing one parallel tool leaves the other running', () async {
    final repository = _FakeChatRepository();
    repository.history = [_task('task-1', status: TaskStatus.running)];
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });

    await notifier.loadSession('session-1');
    final events = repository.taskEvents('task-1');
    events.add(
      _toolUpdated(
        id: 'prt_a',
        callId: 'call_a',
        tool: 'glob',
        status: 'running',
        sentAt: '2026-07-28T08:00:01Z',
      ),
    );
    events.add(
      _toolUpdated(
        id: 'prt_b',
        callId: 'call_b',
        tool: 'bash',
        status: 'running',
        sentAt: '2026-07-28T08:00:02Z',
      ),
    );
    events.add(
      _toolUpdated(
        id: 'prt_a',
        callId: 'call_a',
        tool: 'glob',
        status: 'completed',
        output: '*.dart',
        sentAt: '2026-07-28T08:00:03Z',
      ),
    );
    await _eventually(() {
      final agent = _agentMessage(notifier, 'task-1');
      return agent.toolCalls.length == 2 &&
          agent.toolCalls.any((tool) => tool.status == 'completed') &&
          agent.toolCalls.any((tool) => tool.status == 'running');
    });

    final agent = _agentMessage(notifier, 'task-1');
    expect(
      agent.toolCalls.map((tool) => '${tool.tool}:${tool.status}').toList(),
      ['glob:completed', 'bash:running'],
    );
  });

  test('input_applied finalizes the previous round of running tools', () async {
    final repository = _FakeChatRepository();
    repository.history = [_task('task-1', status: TaskStatus.running)];
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });

    await notifier.loadSession('session-1');
    final events = repository.taskEvents('task-1');
    events.add(
      _toolUpdated(
        id: 'prt_1',
        callId: 'call_abc',
        tool: 'mcp_search',
        status: 'running',
        sentAt: '2026-07-28T08:00:01Z',
      ),
    );
    events.add(
      _toolUpdated(
        id: 'prt_2',
        callId: 'call_def',
        tool: 'bash',
        status: 'completed',
        output: 'ok',
        sentAt: '2026-07-28T08:00:02Z',
      ),
    );
    events.add({
      'type': 'input_applied',
      'content': '现在开始第二轮',
      'metadata': {'queue_item_id': 'queue-1', 'injection_version': 1},
      'sent_at': '2026-07-28T08:00:03Z',
    });
    await _eventually(
      () => notifier.state.messages.any(
        (message) => message.id == 'user_insert_queue-1',
      ),
    );

    final firstAgent = notifier.state.messages.firstWhere(
      (message) =>
          message.role == MessageRole.agent && message.taskId == 'task-1',
    );
    expect(firstAgent.state, MessageState.done);
    expect(firstAgent.toolCalls, hasLength(2));
    expect(firstAgent.toolCalls.every((tool) => tool.isTerminal), isTrue);
    expect(
      firstAgent.blocks
          .where((block) => block.tool != null)
          .every((block) => block.tool!.isTerminal),
      isTrue,
    );
  });

  test(
    'subagent result injection keeps sibling running subagents alive',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      final events = repository.taskEvents('task-1');
      events.add(_subagentStarted('node-a', sentAt: '2026-07-28T08:00:01Z'));
      events.add(_subagentStarted('node-b', sentAt: '2026-07-28T08:00:02Z'));
      await _eventually(
        () => _subagentStatuses(notifier, 'task-1').length == 2,
      );
      final roundsBefore = _agentMessages(notifier, 'task-1').length;

      events.add(
        _subagentResultApplied('node-a', sentAt: '2026-07-28T08:00:03Z'),
      );
      await _eventually(
        () =>
            _subagentStatuses(notifier, 'task-1').contains('node-a:completed'),
      );

      expect(_subagentStatuses(notifier, 'task-1'), contains('node-b:running'));
      final agent = _agentMessage(notifier, 'task-1');
      expect(agent.state, MessageState.streaming);
      expect(_agentMessages(notifier, 'task-1'), hasLength(roundsBefore));
    },
  );

  test(
    'subagent result that wakes the main agent still opens a new round',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      final events = repository.taskEvents('task-1');
      events.add(_subagentStarted('node-a', sentAt: '2026-07-28T08:00:01Z'));
      await _eventually(
        () => _subagentStatuses(notifier, 'task-1').contains('node-a:running'),
      );
      final roundsBefore = _agentMessages(notifier, 'task-1').length;

      events.add(
        _subagentResultApplied(
          'node-a',
          sentAt: '2026-07-28T08:00:02Z',
          wakeReason: 'batch_complete',
        ),
      );
      await _eventually(
        () => _agentMessages(notifier, 'task-1').length == roundsBefore + 1,
      );

      final messages = _agentMessages(notifier, 'task-1');
      expect(messages.first.state, MessageState.done);
      expect(messages.last.state, MessageState.streaming);
    },
  );

  test(
    'text after a tool group does not mark still-running tools completed',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      final events = repository.taskEvents('task-1');
      events.add(
        _toolUpdated(
          id: 'prt_1',
          callId: 'call_abc',
          tool: 'read',
          status: 'running',
          sentAt: '2026-07-28T08:00:01Z',
        ),
      );
      events.add({
        'type': 'delta',
        'field': 'text',
        'content': '根因是工具状态没有收口',
        'sent_at': '2026-07-28T08:00:02Z',
      });
      await _eventually(() {
        final agent = _agentMessage(notifier, 'task-1');
        return agent.content.contains('根因') && agent.toolCalls.isNotEmpty;
      });

      final agent = _agentMessage(notifier, 'task-1');
      expect(agent.toolCalls, hasLength(1));
      expect(agent.toolCalls.single.status, 'running');
      expect(agent.state, MessageState.streaming);
    },
  );

  test('completed event finalizes leftover running tools', () async {
    final repository = _FakeChatRepository();
    repository.history = [_task('task-1', status: TaskStatus.running)];
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });

    await notifier.loadSession('session-1');
    final events = repository.taskEvents('task-1');
    events.add(
      _toolUpdated(
        id: 'prt_1',
        callId: 'call_abc',
        tool: 'glob',
        status: 'running',
        sentAt: '2026-07-28T08:00:01Z',
      ),
    );
    events.add({
      'type': 'completed',
      'content': '第一轮已经结束',
      'sent_at': '2026-07-28T08:00:02Z',
    });
    await _eventually(() => !notifier.state.taskActive);

    final agent = _agentMessage(notifier, 'task-1');
    expect(agent.state, MessageState.done);
    expect(agent.toolCalls, hasLength(1));
    expect(agent.toolCalls.single.status, 'completed');
  });

  test('late deltas after completed do not revive generating state', () async {
    final repository = _FakeChatRepository();
    repository.history = [_task('task-1', status: TaskStatus.running)];
    final notifier = ChatNotifier(
      repo: repository,
      agentId: 'agent-1',
      projectId: 'project-1',
    );
    addTearDown(() async {
      notifier.dispose();
      await repository.dispose();
    });

    await notifier.loadSession('session-1');
    final events = repository.taskEvents('task-1');
    events.add({
      'type': 'delta',
      'field': 'text',
      'content': '第一轮已经写完',
      'sent_at': '2026-07-28T08:00:01Z',
    });
    events.add(
      _toolUpdated(
        id: 'prt_1',
        callId: 'call_abc',
        tool: 'read',
        status: 'running',
        sentAt: '2026-07-28T08:00:02Z',
      ),
    );
    events.add({
      'type': 'completed',
      'content': '第一轮已经写完',
      'sent_at': '2026-07-28T08:00:03Z',
    });
    await _eventually(() => !notifier.state.taskActive);

    events.add({
      'type': 'delta',
      'field': 'text',
      'content': '迟到的正文不应再写入',
      'sent_at': '2026-07-28T08:00:04Z',
    });
    events.add(
      _toolUpdated(
        id: 'prt_1',
        callId: 'call_abc',
        tool: 'read',
        status: 'running',
        sentAt: '2026-07-28T08:00:05Z',
      ),
    );
    await Future<void>.delayed(const Duration(milliseconds: 40));

    final agent = _agentMessage(notifier, 'task-1');
    expect(agent.state, MessageState.done);
    expect(agent.content, '第一轮已经写完');
    expect(agent.content.contains('迟到'), isFalse);
    expect(agent.toolCalls.single.status, 'completed');
    expect(notifier.state.taskActive, isFalse);
  });

  test(
    'live stream without completed still closes when getTask is already done',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      notifier.terminalReconcileInterval = const Duration(milliseconds: 20);
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      repository.taskEvents('task-1').add({
        'type': 'delta',
        'field': 'text',
        'content': '正文已经写完',
        'sent_at': '2026-07-28T08:00:01Z',
      });
      await _eventually(() {
        final agent = _agentMessage(notifier, 'task-1');
        return agent.state == MessageState.streaming &&
            agent.content.contains('正文已经写完') &&
            notifier.state.taskActive;
      });

      repository.history = [
        _task('task-1', status: TaskStatus.completed, result: '正文已经写完'),
      ];
      await _eventually(() => !notifier.state.taskActive);

      final agent = _agentMessage(notifier, 'task-1');
      expect(agent.state, MessageState.done);
      expect(agent.content, contains('正文已经写完'));
      expect(notifier.state.taskActive, isFalse);
      expect(notifier.state.sending, isFalse);
    },
  );

  test(
    'late tool completion after the next round still updates the first round',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      final events = repository.taskEvents('task-1');
      events.add(
        _toolUpdated(
          id: 'prt_1',
          callId: 'call_abc',
          tool: 'bash',
          status: 'running',
          sentAt: '2026-07-28T08:00:01Z',
        ),
      );
      events.add({
        'type': 'input_applied',
        'content': '第二轮问题',
        'metadata': {'queue_item_id': 'queue-2', 'injection_version': 1},
        'sent_at': '2026-07-28T08:00:02Z',
      });
      await _eventually(
        () => notifier.state.messages.any(
          (message) => message.id == 'user_insert_queue-2',
        ),
      );
      events.add(
        _toolUpdated(
          id: 'call_abc',
          callId: 'prt_1',
          tool: 'bash',
          status: 'completed',
          output: 'done after round switch',
          sentAt: '2026-07-28T08:00:03Z',
        ),
      );
      await _eventually(() {
        final firstAgent = notifier.state.messages.firstWhere(
          (message) =>
              message.role == MessageRole.agent && message.taskId == 'task-1',
        );
        return firstAgent.toolCalls.single.output == 'done after round switch';
      });

      final firstAgent = notifier.state.messages.firstWhere(
        (message) =>
            message.role == MessageRole.agent && message.taskId == 'task-1',
      );
      final secondAgent = notifier.state.messages.lastWhere(
        (message) =>
            message.role == MessageRole.agent && message.taskId == 'task-1',
      );
      expect(firstAgent.state, MessageState.done);
      expect(firstAgent.toolCalls, hasLength(1));
      expect(firstAgent.toolCalls.single.status, 'completed');
      expect(secondAgent.toolCalls, isEmpty);
    },
  );

  test(
    'screenshot path: later completed mcp_call with a new part id leaves earlier tools running',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      final events = repository.taskEvents('task-1');
      events.add(
        _toolUpdated(
          id: 'prt_mcp_1',
          callId: 'call_mcp_1',
          tool: 'mcp_call',
          status: 'running',
          sentAt: '2026-07-28T08:00:01Z',
        ),
      );
      events.add(
        _toolUpdated(
          id: 'prt_read_1',
          callId: 'call_read_1',
          tool: 'read',
          status: 'running',
          sentAt: '2026-07-28T08:00:02Z',
        ),
      );
      events.add({
        'type': 'delta',
        'field': 'text',
        'content': '正式版 10 个配置已确认',
        'sent_at': '2026-07-28T08:00:03Z',
      });
      events.add(
        _toolUpdated(
          id: 'prt_mcp_2',
          callId: 'call_mcp_2',
          tool: 'mcp_call',
          status: 'completed',
          output: 'ok',
          sentAt: '2026-07-28T08:00:04Z',
        ),
      );
      events.add({
        'type': 'delta',
        'field': 'text',
        'content': '任务已提交',
        'sent_at': '2026-07-28T08:00:05Z',
      });
      await _eventually(() {
        final agent = _agentMessage(notifier, 'task-1');
        return agent.toolCalls.length == 3 && agent.content.contains('任务已提交');
      });

      final agent = _agentMessage(notifier, 'task-1');
      expect(agent.state, MessageState.streaming);
      expect(notifier.state.taskActive, isTrue);
      expect(
        agent.toolCalls.map((tool) => '${tool.tool}:${tool.status}').toList(),
        ['mcp_call:running', 'read:running', 'mcp_call:completed'],
      );
      expect(
        agent.blocks.map((block) {
          if (block.tool != null) {
            return 'tool:${block.tool!.tool}:${block.tool!.status}';
          }
          return 'text';
        }).toList(),
        [
          'tool:mcp_call:running',
          'tool:read:running',
          'text',
          'tool:mcp_call:completed',
          'text',
        ],
      );
    },
  );

  test(
    'screenshot path: task completed still finalizes the earlier running tools',
    () async {
      final repository = _FakeChatRepository();
      repository.history = [_task('task-1', status: TaskStatus.running)];
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      final events = repository.taskEvents('task-1');
      events.add(
        _toolUpdated(
          id: 'prt_mcp_1',
          callId: 'call_mcp_1',
          tool: 'mcp_call',
          status: 'running',
          sentAt: '2026-07-28T08:00:01Z',
        ),
      );
      events.add(
        _toolUpdated(
          id: 'prt_read_1',
          callId: 'call_read_1',
          tool: 'read',
          status: 'running',
          sentAt: '2026-07-28T08:00:02Z',
        ),
      );
      events.add({
        'type': 'delta',
        'field': 'text',
        'content': '正式版 10 个配置已确认',
        'sent_at': '2026-07-28T08:00:03Z',
      });
      events.add(
        _toolUpdated(
          id: 'prt_mcp_2',
          callId: 'call_mcp_2',
          tool: 'mcp_call',
          status: 'completed',
          output: 'ok',
          sentAt: '2026-07-28T08:00:04Z',
        ),
      );
      events.add({
        'type': 'completed',
        'content': '配置已写回',
        'sent_at': '2026-07-28T08:00:06Z',
      });
      await _eventually(() => !notifier.state.taskActive);

      final agent = _agentMessage(notifier, 'task-1');
      expect(agent.state, MessageState.done);
      expect(notifier.state.taskActive, isFalse);
      expect(agent.toolCalls.every((tool) => !tool.isRunning), isTrue);
    },
  );

  test(
    'initial queue request failure stays loading when the stream recovers',
    () async {
      final repository = _FakeChatRepository()..queueGetFailuresRemaining = 1;
      final notifier = ChatNotifier(
        repo: repository,
        agentId: 'agent-1',
        projectId: 'project-1',
      );
      addTearDown(() async {
        notifier.dispose();
        await repository.dispose();
      });

      await notifier.loadSession('session-1');
      await _eventually(() => repository.queueGetCount == 1);

      expect(notifier.state.queueLoading, isTrue);
      expect(notifier.state.queueError, isNull);

      repository.queueEvents.add(
        const ChatQueueSnapshot(sessionId: 'session-1', version: 1),
      );
      await _eventually(() => !notifier.state.queueLoading);

      expect(notifier.state.queueError, isNull);
      expect(notifier.state.queue.sessionId, 'session-1');
    },
  );
}

const _modelA = ModelInfo(
  providerID: 'provider',
  modelID: 'model-a',
  name: 'Model A',
  variants: ['high'],
);

const _modelB = ModelInfo(
  providerID: 'provider',
  modelID: 'model-b',
  name: 'Model B',
  variants: ['low'],
);

Future<void> _loadRawHistory(
  ChatNotifier notifier,
  _FakeChatRepository repository,
  List<Map<String, dynamic>> tasks,
) async {
  // 历史 JSON 必须原样注入：测试替身重新序列化 TaskModel 时会丢掉 file part。
  final completer = Completer<List<Map<String, dynamic>>>();
  repository.historyCompleter = completer;
  final loading = notifier.loadSession('session-1');
  completer.complete(tasks);
  await loading;
}

TaskModel _task(String id, {required TaskStatus status, String result = ''}) {
  return TaskModel(
    taskId: id,
    agentId: 'agent-1',
    projectId: 'project-1',
    sessionId: 'session-1',
    status: status,
    result: result,
    userText: '原始问题',
    createdAt: DateTime.parse('2026-07-28T08:00:00Z'),
  );
}

Map<String, dynamic> _taskJson(TaskModel task) => {
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

ChatMessage _agentMessage(ChatNotifier notifier, String taskId) {
  return notifier.state.messages.firstWhere(
    (message) => message.role == MessageRole.agent && message.taskId == taskId,
  );
}

Map<String, dynamic> _subagentStarted(String nodeId, {required String sentAt}) {
  return {
    'type': 'subagent_started',
    'metadata': {
      'node_id': nodeId,
      'subagent_type': 'explore',
      'title': '子代理 $nodeId',
      'started_at': 1,
    },
    'sent_at': sentAt,
  };
}

/// The relay injects a delegated result through `input_applied`; the parent
/// round only advances when it also reports a wake reason.
Map<String, dynamic> _subagentResultApplied(
  String nodeId, {
  required String sentAt,
  String wakeReason = '',
}) {
  return {
    'type': 'input_applied',
    'content': '子代理 $nodeId 已完成',
    'metadata': {
      'source': 'subagent',
      'context_type': 'subagentResult',
      'node_id': nodeId,
      'queue_item_id': nodeId,
      'status': 'completed',
      if (wakeReason.isNotEmpty) 'wake_reason': wakeReason,
    },
    'sent_at': sentAt,
  };
}

List<ChatMessage> _agentMessages(ChatNotifier notifier, String taskId) {
  return notifier.state.messages
      .where(
        (message) =>
            message.role == MessageRole.agent && message.taskId == taskId,
      )
      .toList();
}

List<String> _subagentStatuses(ChatNotifier notifier, String taskId) {
  return _agentMessages(notifier, taskId)
      .expand((message) => message.toolCalls)
      .where(isSubagentToolCall)
      .map((tool) => '${tool.metadata['node_id']}:${tool.status}')
      .toList();
}

Map<String, dynamic> _toolUpdated({
  required String id,
  required String callId,
  required String tool,
  required String status,
  required String sentAt,
  String output = '',
}) {
  return {
    'type': 'tool_updated',
    'tool': {
      'id': id,
      'call_id': callId,
      'tool': tool,
      'status': status,
      if (output.isNotEmpty) 'output': output,
    },
    'sent_at': sentAt,
  };
}

ChatQueueItem _queueItem(
  String id, {
  ChatQueueItemStatus status = ChatQueueItemStatus.queued,
  String taskId = '',
  String model = 'provider/model-a',
  String variant = 'high',
  int version = 1,
}) {
  return ChatQueueItem(
    id: id,
    sessionId: 'session-1',
    agentId: 'agent-1',
    machineId: 'machine-1',
    projectId: 'project-1',
    parts: const [
      {'type': 'text', 'text': '排队消息'},
    ],
    metadata: {'model': model, 'variant': variant},
    position: id == 'queue-1' ? 1024 : 2048,
    status: status,
    version: version,
    taskId: taskId,
    createdAt: DateTime.parse('2026-07-28T08:00:00Z'),
  );
}

Future<void> _eventually(bool Function() predicate) async {
  final deadline = DateTime.now().add(const Duration(seconds: 2));
  while (!predicate()) {
    if (DateTime.now().isAfter(deadline)) {
      fail('condition was not met before timeout');
    }
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
}

class _RecordingLocalChatStore implements LocalChatStore {
  final List<Map<String, dynamic>> _events = [];
  List<CachedChatTask> cachedTasks = [];

  @override
  Future<void> init() async {}

  @override
  Future<String?> readRevision({
    required String identity,
    required String sessionId,
  }) async => null;

  @override
  Future<void> saveRevision({
    required String identity,
    required String sessionId,
    required String revision,
  }) async {}

  @override
  Future<List<CachedChatTask>> readTasks({
    required String identity,
    required String agentId,
    required String projectId,
    required String sessionId,
    int limit = 20,
  }) async => cachedTasks;

  @override
  Future<void> saveTasks({
    required String identity,
    required String agentId,
    required String projectId,
    required String sessionId,
    required List<Map<String, dynamic>> tasks,
  }) async {}

  @override
  Future<bool> recordEvent({
    required String identity,
    required String taskId,
    required String eventKey,
    required Map<String, dynamic> payload,
  }) async {
    _events.add({
      'identity': identity,
      'task_id': taskId,
      'event_key': eventKey,
      'payload': Map<String, dynamic>.from(payload),
    });
    return true;
  }

  @override
  Future<List<Map<String, dynamic>>> readEvents({
    required String identity,
    required String taskId,
  }) async {
    return _events
        .where(
          (event) =>
              event['identity'] == identity && event['task_id'] == taskId,
        )
        .map((event) => Map<String, dynamic>.from(event['payload'] as Map))
        .toList(growable: false);
  }

  @override
  Future<void> close() async {}
}

class _FakeChatRepository extends ChatRepository {
  final createTaskCompleter = Completer<TaskModel>();
  final queueEvents = StreamController<ChatQueueSnapshot>.broadcast();
  final Map<String, StreamController<Map<String, dynamic>>> _taskEvents = {};
  final Map<String, TaskEventSnapshot> snapshots = {};
  final List<String> snapshotReads = [];
  final List<String> cancelledTasks = [];
  Completer<void>? cancelCompleter;
  final cancelStarted = Completer<void>();

  @override
  Future<void> cancelTask(String taskId) async {
    cancelledTasks.add(taskId);
    if (!cancelStarted.isCompleted) cancelStarted.complete();
    await cancelCompleter?.future;
  }

  final List<String> queuedTexts = [];
  final List<String> queuedModels = [];
  List<TaskModel> history = const [];
  Completer<List<Map<String, dynamic>>>? historyCompleter;
  Completer<ChatQueueSnapshot>? queueSnapshotCompleter;
  Completer<TaskModel>? taskReadCompleter;
  Completer<TaskEventSnapshot>? snapshotCompleter;
  int taskReadCount = 0;
  int taskWatchCount = 0;
  Completer<ChatQueueItem>? insertQueueItemCompleter;
  final insertQueueItemRequested = Completer<void>();
  int queueWatchCount = 0;
  int queueGetCount = 0;
  int queueGetFailuresRemaining = 0;
  ChatQueueSnapshot queueSnapshot = const ChatQueueSnapshot(
    sessionId: 'session-1',
  );

  StreamController<Map<String, dynamic>> taskEvents(String taskId) {
    return _taskEvents.putIfAbsent(
      taskId,
      () => StreamController<Map<String, dynamic>>.broadcast(),
    );
  }

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
  }) {
    return createTaskCompleter.future;
  }

  @override
  Future<ChatQueueItem> createChatQueueItem({
    required String sessionId,
    required String agentId,
    required String projectId,
    required String text,
    List<AttachedFile>? attachedFiles,
    ModelInfo? model,
    String permissionMode = 'ask',
    String? variant,
    String? goal,
    int goalMaxIterations = 30,
    Map<String, String>? extraMetadata,
  }) async {
    queuedTexts.add(text);
    queuedModels.add(model?.metaKey ?? '');
    final item = ChatQueueItem(
      id: 'queue-${queuedTexts.length}',
      sessionId: sessionId,
      agentId: agentId,
      projectId: projectId,
      parts: [
        {'type': 'text', 'text': text},
      ],
      metadata: {
        if (model != null) 'model': model.metaKey,
        if (variant != null) 'variant': variant,
      },
      position: queuedTexts.length * 1024,
      status: ChatQueueItemStatus.queued,
      version: 1,
    );
    queueSnapshot = ChatQueueSnapshot(
      sessionId: sessionId,
      version: queueSnapshot.version + 1,
      items: [...queueSnapshot.items, item],
    );
    return item;
  }

  @override
  Future<List<TaskModel>> getSessionHistory(
    String sessionId, {
    int limit = 5,
    DateTime? beforeCreatedAt,
    String? beforeTaskId,
  }) async => history;

  @override
  Future<List<Map<String, dynamic>>> getSessionHistoryJson(
    String sessionId, {
    int limit = 5,
    DateTime? beforeCreatedAt,
    String? beforeTaskId,
  }) async => historyCompleter != null
      ? historyCompleter!.future
      : history.take(limit).map(_taskJson).toList(growable: false);

  @override
  Future<TaskModel> getTask(String taskId) async {
    taskReadCount++;
    if (taskReadCompleter != null) return taskReadCompleter!.future;
    for (final task in history) {
      if (task.taskId == taskId) return task;
    }
    throw StateError('task not found');
  }

  @override
  Future<TaskEventSnapshot> getTaskEventSnapshot(String taskId) async {
    snapshotReads.add(taskId);
    if (snapshotCompleter != null) return snapshotCompleter!.future;
    return snapshots[taskId] ?? const TaskEventSnapshot();
  }

  @override
  Stream<Map<String, dynamic>> watchTaskEvents(String taskId) {
    taskWatchCount++;
    return taskEvents(taskId).stream;
  }

  @override
  Stream<ChatQueueSnapshot> watchChatQueue(String sessionId) {
    queueWatchCount++;
    return queueEvents.stream;
  }

  @override
  Future<ChatQueueSnapshot> getChatQueue(String sessionId) async {
    queueGetCount++;
    if (queueGetFailuresRemaining > 0) {
      queueGetFailuresRemaining--;
      throw StateError('initial queue request failed');
    }
    if (queueSnapshotCompleter != null) {
      return queueSnapshotCompleter!.future;
    }
    return queueSnapshot;
  }

  @override
  Future<ChatQueueItem> updateChatQueueItem(
    ChatQueueItem item, {
    String model = '',
    String variant = '',
  }) async {
    return ChatQueueItem(
      id: item.id,
      sessionId: item.sessionId,
      agentId: item.agentId,
      machineId: item.machineId,
      projectId: item.projectId,
      parts: item.parts,
      metadata: {'model': model, 'variant': variant},
      position: item.position,
      status: item.status,
      version: item.version + 1,
      taskId: item.taskId,
      injectionVersion: item.injectionVersion,
      createdAt: item.createdAt,
      updatedAt: DateTime.now(),
    );
  }

  @override
  Future<ChatQueueItem> insertChatQueueItem(ChatQueueItem item) {
    if (!insertQueueItemRequested.isCompleted) {
      insertQueueItemRequested.complete();
    }
    return insertQueueItemCompleter?.future ?? Future.value(item);
  }

  @override
  Future<void> dispose() async {
    await queueEvents.close();
    for (final controller in _taskEvents.values) {
      await controller.close();
    }
  }
}
