import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import '../../../core/config/api_client.dart';
import '../../../core/config/sse_parser.dart';
import '../../../shared/grok_context_limit.dart';
import 'chat_model.dart';

void _chatDiag(String stage, [Map<String, Object?> data = const {}]) {
  debugPrint('[ChatDiag] $stage $data');
}

const _taskEventSnapshotIdleTimeout = Duration(seconds: 20);
const compactionStatusHint = '正在压缩上下文';

class ChatRepository {
  static const modelLatencyTestSessionPrefix = 'model_test_';
  static const _modelLatencyPrompt = '请只回复“连接正常”，并保持流式输出。';
  final Map<String, int> _lastTaskEventSequences = {};
  final Map<String, String> _lastTaskEventCursors = {};

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
    String? variant,
    String? goal,
    int goalMaxIterations = 30,
    Map<String, String>? extraMetadata,
  }) async {
    final parts = _buildParts(text, attachedFiles);
    final metadata = _buildTaskMetadata(
      model: model,
      permissionMode: permissionMode,
      variant: variant,
      goal: goal,
      goalMaxIterations: goalMaxIterations,
      extraMetadata: extraMetadata,
    );
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

  Future<TaskModel> compactContext({
    required String agentId,
    required String projectId,
    required String sessionId,
    ModelInfo? model,
  }) async {
    final data = await ApiClient.post('/api/tasks', {
      'agent_id': agentId,
      'project_id': projectId,
      'session_id': sessionId,
      'parts': const <Map<String, dynamic>>[],
      'metadata': <String, String>{
        'task_command': 'compact',
        'permission_mode': 'ask',
        if (model != null) 'model': model.metaKey,
      },
    });
    return TaskModel.fromJson(data);
  }

  Future<ChatQueueSnapshot> getChatQueue(String sessionId) async {
    final data = await ApiClient.get(
      '/api/sessions/${Uri.encodeComponent(sessionId)}/queue',
    );
    return ChatQueueSnapshot.fromJson(data);
  }

  Stream<ChatQueueSnapshot> watchChatQueue(String sessionId) async* {
    await for (final raw in ApiClient.sse(
      '/api/sessions/${Uri.encodeComponent(sessionId)}/queue/events',
    )) {
      if (raw.isEmpty) continue;
      try {
        yield ChatQueueSnapshot.fromJson(
          jsonDecode(raw) as Map<String, dynamic>,
        );
      } catch (_) {}
    }
  }

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
    final data = await ApiClient.post(
      '/api/sessions/${Uri.encodeComponent(sessionId)}/queue',
      {
        'agent_id': agentId,
        'project_id': projectId,
        'parts': _buildParts(text, attachedFiles),
        'metadata': _buildTaskMetadata(
          model: model,
          permissionMode: permissionMode,
          variant: variant,
          goal: goal,
          goalMaxIterations: goalMaxIterations,
          extraMetadata: extraMetadata,
        ),
      },
    );
    return ChatQueueItem.fromJson(data);
  }

  Future<ChatQueueItem> updateChatQueueItem(
    ChatQueueItem item, {
    String model = '',
    String variant = '',
  }) async {
    final data = await ApiClient.patch(
      '/api/chat-queue/${Uri.encodeComponent(item.id)}',
      {'expected_version': item.version, 'model': model, 'variant': variant},
    );
    return ChatQueueItem.fromJson(data);
  }

  Future<void> deleteChatQueueItem(ChatQueueItem item) async {
    await ApiClient.delete(
      '/api/chat-queue/${Uri.encodeComponent(item.id)}',
      body: {'expected_version': item.version},
    );
  }

  Future<ChatQueueSnapshot> reorderChatQueue(
    String sessionId,
    List<ChatQueueItem> items,
  ) async {
    final data = await ApiClient.post(
      '/api/sessions/${Uri.encodeComponent(sessionId)}/queue/reorder',
      {
        'item_ids': items.map((item) => item.id).toList(growable: false),
        'expected_versions': {for (final item in items) item.id: item.version},
      },
    );
    return ChatQueueSnapshot.fromJson(data);
  }

  Future<ChatQueueItem> insertChatQueueItem(ChatQueueItem item) async {
    final data = await ApiClient.post(
      '/api/chat-queue/${Uri.encodeComponent(item.id)}/insert',
      {'expected_version': item.version},
    );
    return ChatQueueItem.fromJson(data);
  }

  Future<ChatQueueItem> sendChatQueueItem(ChatQueueItem item) async {
    final data = await ApiClient.post(
      '/api/chat-queue/${Uri.encodeComponent(item.id)}/send',
      const <String, dynamic>{},
    );
    return ChatQueueItem.fromJson(data);
  }

  List<Map<String, dynamic>> _buildParts(
    String text,
    List<AttachedFile>? attachedFiles,
  ) => [
    {'type': 'text', 'text': text},
    if (attachedFiles != null)
      for (final file in attachedFiles) file.toFilePart(),
  ];

  Map<String, String> _buildTaskMetadata({
    ModelInfo? model,
    String permissionMode = 'ask',
    String? variant,
    String? goal,
    int goalMaxIterations = 30,
    Map<String, String>? extraMetadata,
  }) => {
    'permission_mode': permissionMode,
    if (model != null) 'model': model.metaKey,
    if (variant != null && variant.isNotEmpty) 'variant': variant,
    if (goal != null && goal.trim().isNotEmpty) 'goal': goal.trim(),
    if (goal != null && goal.trim().isNotEmpty)
      'goal_max_iterations': goalMaxIterations.toString(),
    ...?extraMetadata,
  };

  Future<TaskModel> getTask(String taskId) async {
    final data = await ApiClient.get('/api/tasks/$taskId');
    return TaskModel.fromJson(data);
  }

  Future<void> cancelTask(String taskId) async {
    await ApiClient.post('/api/tasks/$taskId/cancel', {});
  }

  Future<ModelLatencyTestResult> testModelLatency({
    required String agentId,
    required String projectId,
    required ModelInfo model,
    String? variant,
    void Function(ModelLatencyLogEntry entry)? onLog,
    Duration timeout = const Duration(minutes: 3),
  }) async {
    final startedAt = DateTime.now();
    final logs = <ModelLatencyLogEntry>[];
    var sessionId = '';
    final modelRef = model.metaKey;
    String testId = '';
    Duration? createTaskTime;
    Duration? firstEventTime;
    Duration? firstReasoningTime;
    Duration? firstTextTime;
    var reasoningLength = 0;
    var textLength = 0;
    int? tokenCount;

    void addLog(String stage, String message, [Map<String, dynamic>? data]) {
      final entry = ModelLatencyLogEntry(
        at: DateTime.now(),
        stage: stage,
        message: message,
        data: data ?? const {},
      );
      logs.add(entry);
      onLog?.call(entry);
    }

    addLog('request', '开始测试模型连接', {
      'agent_id': agentId,
      'project_id': projectId,
      'model': modelRef,
      'variant': variant,
    });

    try {
      final createStartedAt = DateTime.now();
      final created = await ApiClient.post('/api/model-tests', {
        'agent_id': agentId,
        'project_id': projectId,
        'model': modelRef,
        if (variant != null && variant.isNotEmpty) 'variant': variant,
        'prompt': _modelLatencyPrompt,
      });
      testId = created['test_id'] as String? ?? '';
      createTaskTime = DateTime.now().difference(createStartedAt);
      addLog('task', '测试任务已创建', {
        'test_id': testId,
        'elapsed_ms': createTaskTime.inMilliseconds,
        'status': created['status'],
      });

      await for (final event in watchModelTestEvents(testId).timeout(timeout)) {
        final type = event['type'] as String? ?? 'unknown';
        if (firstEventTime == null) {
          firstEventTime = DateTime.now().difference(startedAt);
          addLog('sse', '收到首个事件', {
            'event_type': type,
            'elapsed_ms': firstEventTime.inMilliseconds,
            'event_sent_at': event['sent_at'],
          });
        }

        switch (type) {
          case 'started':
            final eventSessionId = (event['session_id'] as String?)?.trim();
            if (eventSessionId != null && eventSessionId.isNotEmpty) {
              sessionId = eventSessionId;
            }
            addLog('launcher', '设备端已开始模型测试', {
              'elapsed_ms': DateTime.now().difference(startedAt).inMilliseconds,
              'session_id': sessionId,
              'test_id': testId,
            });
            break;
          case 'progress':
            addLog('progress', event['content'] as String? ?? '模型测试进度', {
              'elapsed_ms': event['elapsed_ms'],
              'step_ms': event['step_ms'],
              'metadata': event['metadata'],
            });
            break;
          case 'delta':
            final field = event['field'] as String? ?? 'text';
            final content = event['content'] as String? ?? '';
            if (field == 'reasoning') {
              reasoningLength += content.length;
              if (firstReasoningTime == null && content.isNotEmpty) {
                firstReasoningTime = DateTime.now().difference(startedAt);
                addLog('reasoning', '收到首个思考片段', {
                  'elapsed_ms': firstReasoningTime.inMilliseconds,
                  'content_length': content.length,
                  'event_sent_at': event['sent_at'],
                });
              }
            } else {
              textLength += content.length;
              if (firstTextTime == null && content.isNotEmpty) {
                firstTextTime = DateTime.now().difference(startedAt);
                addLog('text', '收到首个正文片段', {
                  'elapsed_ms': firstTextTime.inMilliseconds,
                  'content_length': content.length,
                  'event_sent_at': event['sent_at'],
                });
              }
            }
            break;
          case 'completed':
            textLength = event['text_length'] as int? ?? textLength;
            reasoningLength =
                event['reasoning_length'] as int? ?? reasoningLength;
            tokenCount = event['token_count'] as int?;
            final completedAt = DateTime.now();
            final totalTime = completedAt.difference(startedAt);
            addLog('done', '模型测试完成', {
              'elapsed_ms': totalTime.inMilliseconds,
              'reasoning_length': reasoningLength,
              'text_length': textLength,
              'token_count': tokenCount,
            });
            return ModelLatencyTestResult(
              taskId: testId,
              sessionId: sessionId,
              model: modelRef,
              variant: variant,
              startedAt: startedAt,
              completedAt: completedAt,
              success: true,
              createTaskTime: createTaskTime,
              firstEventTime: firstEventTime,
              firstReasoningTime: firstReasoningTime,
              firstTextTime: firstTextTime,
              totalTime: totalTime,
              reasoningLength: reasoningLength,
              textLength: textLength,
              tokenCount: tokenCount,
              logs: List<ModelLatencyLogEntry>.unmodifiable(logs),
            );
          case 'failed':
          case 'cancelled':
            final error =
                event['error'] as String? ??
                event['content'] as String? ??
                '模型测试失败';
            final completedAt = DateTime.now();
            final totalTime = completedAt.difference(startedAt);
            addLog(type, error, {
              'elapsed_ms': totalTime.inMilliseconds,
              'error_detail': event['error_detail'],
            });
            return ModelLatencyTestResult(
              taskId: testId,
              sessionId: sessionId,
              model: modelRef,
              variant: variant,
              startedAt: startedAt,
              completedAt: completedAt,
              success: false,
              error: error,
              createTaskTime: createTaskTime,
              firstEventTime: firstEventTime,
              firstReasoningTime: firstReasoningTime,
              firstTextTime: firstTextTime,
              totalTime: totalTime,
              reasoningLength: reasoningLength,
              textLength: textLength,
              tokenCount: tokenCount,
              logs: List<ModelLatencyLogEntry>.unmodifiable(logs),
            );
        }
      }
      throw const ApiException('测试事件流提前结束', statusCode: 408);
    } catch (e) {
      final message = e is ApiException ? e.message : e.toString();
      final completedAt = DateTime.now();
      final totalTime = completedAt.difference(startedAt);
      addLog('error', message, {'elapsed_ms': totalTime.inMilliseconds});
      return ModelLatencyTestResult(
        taskId: testId,
        sessionId: sessionId,
        model: modelRef,
        variant: variant,
        startedAt: startedAt,
        completedAt: completedAt,
        success: false,
        error: message,
        createTaskTime: createTaskTime,
        firstEventTime: firstEventTime,
        firstReasoningTime: firstReasoningTime,
        firstTextTime: firstTextTime,
        totalTime: totalTime,
        reasoningLength: reasoningLength,
        textLength: textLength,
        tokenCount: tokenCount,
        logs: List<ModelLatencyLogEntry>.unmodifiable(logs),
      );
    }
  }

  Future<String> optimizeGoal({
    required String agentId,
    required String projectId,
    required String goal,
    required ModelInfo model,
    String? variant,
    int maxIterations = 30,
  }) async {
    final data = await ApiClient.post('/api/goal/optimize', {
      'agent_id': agentId,
      'project_id': projectId,
      'goal': goal,
      'model': model.metaKey,
      if (variant != null && variant.isNotEmpty) 'variant': variant,
      'max_iterations': maxIterations,
    });
    final optimized = data['goal'] as String? ?? '';
    if (optimized.trim().isEmpty) {
      throw const ApiException('模型未返回优化后的目标', statusCode: 409);
    }
    return optimized.trim();
  }

  Future<void> submitApproval(String taskId, String reply) async {
    await ApiClient.post('/api/tasks/$taskId/approval', {'reply': reply});
  }

  Future<void> submitQuestion(
    String taskId,
    String requestId,
    List<List<String>> answers, {
    bool rejected = false,
  }) async {
    await ApiClient.post('/api/tasks/$taskId/question', {
      'request_id': requestId,
      'answers': answers,
      'rejected': rejected,
    });
  }

  Stream<Map<String, dynamic>> watchTaskEvents(String taskId) async* {
    yield* _watchTaskEventsInternal(taskId);
  }

  /// Historical snapshot stream. Kept as a separate overridable method so
  /// lightweight repository fakes can continue supplying in-memory events.
  Stream<Map<String, dynamic>> watchTaskSnapshotEvents(String taskId) async* {
    if (runtimeType != ChatRepository) {
      yield* watchTaskEvents(taskId);
      return;
    }
    var yielded = false;
    try {
      await for (final event in _watchPagedTaskEvents(
        taskId,
        '/api/tasks/${Uri.encodeComponent(taskId)}/agent-history',
        source: 'agent-history',
      )) {
        yielded = true;
        yield event;
      }
    } on ApiException catch (error) {
      _chatDiag('history_fallback', {
        'task_id': taskId,
        'from': 'agent-history',
        'to': 'event-pages',
        'reason': 'api',
        'error': error.toString(),
        'yielded': yielded,
      });
      if (yielded) return;
      await for (final event in _watchPagedTaskEvents(
        taskId,
        '/api/tasks/${Uri.encodeComponent(taskId)}/event-pages',
        source: 'event-pages',
      )) {
        yield event;
      }
    } on FormatException catch (error) {
      _chatDiag('history_fallback', {
        'task_id': taskId,
        'from': 'agent-history',
        'to': 'event-pages',
        'reason': 'format',
        'error': error.toString(),
        'yielded': yielded,
      });
      if (yielded) return;
      await for (final event in _watchPagedTaskEvents(
        taskId,
        '/api/tasks/${Uri.encodeComponent(taskId)}/event-pages',
        source: 'event-pages',
      )) {
        yield event;
      }
    }
  }

  Stream<Map<String, dynamic>> _watchPagedTaskEvents(
    String taskId,
    String path, {
    String source = '',
  }) async* {
    String? cursor;
    var pageIndex = 0;
    var total = 0;
    final startedAt = DateTime.now();
    while (true) {
      pageIndex += 1;
      final pageStartedAt = DateTime.now();
      final query = Uri(
        queryParameters: {
          'limit': '200',
          if (cursor != null && cursor.isNotEmpty) 'cursor': cursor,
        },
      ).query;
      final page = await ApiClient.get('$path?$query');
      final events = (page['events'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map((event) => Map<String, dynamic>.from(event))
          .toList(growable: false);
      total += events.length;
      _chatDiag('history_page', {
        'task_id': taskId,
        'source': source,
        'page': pageIndex,
        'events': events.length,
        'total': total,
        'has_more': page['has_more'] == true,
        'page_ms': DateTime.now().difference(pageStartedAt).inMilliseconds,
        'elapsed_ms': DateTime.now().difference(startedAt).inMilliseconds,
      });
      for (final event in events) {
        yield event;
      }
      if (page['has_more'] != true) return;
      final nextCursor = page['next_cursor']?.toString() ?? '';
      if (nextCursor.isEmpty || nextCursor == cursor) {
        throw const FormatException('Invalid task event page cursor');
      }
      cursor = nextCursor;
    }
  }

  Stream<Map<String, dynamic>> _watchTaskEventsInternal(String taskId) async* {
    while (true) {
      final lastSequence = _lastTaskEventSequences[taskId];
      final lastCursor = _lastTaskEventCursors[taskId];
      var terminal = false;
      final connectStartedAt = DateTime.now();
      _chatDiag('sse_connect', {
        'task_id': taskId,
        'last_event_id': lastSequence,
        'has_cursor': lastCursor != null && lastCursor.isNotEmpty,
      });
      try {
        var first = true;
        var decodeFailures = 0;
        await for (final raw in ApiClient.sse(
          '/api/tasks/${Uri.encodeComponent(taskId)}/events',
          extraHeaders: {
            if (lastSequence != null && lastSequence > 0)
              'Last-Event-ID': '$lastSequence',
            if (lastCursor != null && lastCursor.isNotEmpty)
              'X-Task-Event-Cursor': lastCursor,
          },
        )) {
          if (raw.isEmpty) continue;
          try {
            final json = jsonDecode(raw) as Map<String, dynamic>;
            _rememberTaskEventCursor(taskId, json);
            if (first) {
              first = false;
              _chatDiag('sse_first_frame', {
                'task_id': taskId,
                'type': json['type'],
                'wait_ms': DateTime.now()
                    .difference(connectStartedAt)
                    .inMilliseconds,
                'sequence': json['sequence'],
              });
            }
            yield json;
            final type = json['type']?.toString();
            terminal =
                type == 'completed' || type == 'failed' || type == 'cancelled';
          } catch (error) {
            decodeFailures += 1;
            _chatDiag('sse_decode_failed', {
              'task_id': taskId,
              'count': decodeFailures,
              'error': error.toString(),
              'raw_len': raw.length,
            });
          }
        }
        if (terminal) return;
        try {
          final synthetic = _syntheticTerminalEvent(await getTask(taskId));
          if (synthetic != null) {
            yield synthetic;
            return;
          }
        } catch (_) {}
        _chatDiag('sse_reconnect', {
          'task_id': taskId,
          'reason': 'stream_ended',
          'terminal': terminal,
          'wait_ms': 1000,
        });
        await Future<void>.delayed(const Duration(seconds: 1));
      } on SseHttpException {
        rethrow;
      } catch (error) {
        _chatDiag('sse_reconnect', {
          'task_id': taskId,
          'reason': 'error',
          'error': error.toString(),
          'wait_ms': 1000,
        });
        await Future<void>.delayed(const Duration(seconds: 1));
      }
    }
  }

  void _rememberTaskEventCursor(String taskId, Map<String, dynamic> event) {
    final sequence = (event['sequence'] as num?)?.toInt();
    if (sequence != null && sequence > 0) {
      _lastTaskEventSequences[taskId] = sequence;
    }
    final sentAt = event['sent_at']?.toString();
    final id = (event['id'] as num?)?.toInt() ?? sequence;
    if (sentAt == null || sentAt.isEmpty || id == null || id < 0) return;
    _lastTaskEventCursors[taskId] = base64Url
        .encode(utf8.encode(jsonEncode({'sent_at': sentAt, 'id': id})))
        .replaceAll('=', '');
  }

  Stream<Map<String, dynamic>> _watchTaskSnapshotEvents(String taskId) async* {
    yield* watchTaskSnapshotEvents(taskId);
  }

  Stream<Map<String, dynamic>> watchModelTestEvents(String testId) async* {
    await for (final raw in ApiClient.sse('/api/model-tests/$testId/events')) {
      if (raw.isEmpty) continue;
      try {
        final json = jsonDecode(raw) as Map<String, dynamic>;
        yield json;
      } catch (_) {}
    }
  }

  Future<TaskEventSnapshot> getTaskEventSnapshot(String taskId) async {
    final events = <Map<String, dynamic>>[];
    final startedAt = DateTime.now();
    var timedOut = false;
    try {
      await for (final event in _watchTaskSnapshotEvents(taskId).timeout(
        _taskEventSnapshotIdleTimeout,
        onTimeout: (sink) {
          timedOut = true;
          sink.close();
        },
      )) {
        events.add(event);
      }
    } catch (error) {
      _chatDiag('history_snapshot_error', {
        'task_id': taskId,
        'error': error.toString(),
        'events': events.length,
      });
    }
    final snapshot = snapshotFromTaskEvents(taskId, events);
    final counts = <String, int>{};
    for (final event in events) {
      final type = event['type']?.toString() ?? 'unknown';
      counts[type] = (counts[type] ?? 0) + 1;
    }
    var textBlocks = 0;
    var toolBlocks = 0;
    for (final block in snapshot.blocks) {
      if (block.tool != null) {
        toolBlocks += 1;
      } else if (block.type == ChatMessageBlockType.text &&
          block.text.trim().isNotEmpty) {
        textBlocks += 1;
      }
    }
    _chatDiag('history_snapshot', {
      'task_id': taskId,
      'events': events.length,
      'counts': counts,
      'text_blocks': textBlocks,
      'tool_blocks': toolBlocks,
      'text_len': snapshot.text.length,
      'timed_out': timedOut,
      'elapsed_ms': DateTime.now().difference(startedAt).inMilliseconds,
    });
    return snapshot;
  }

  TaskEventSnapshot snapshotFromTaskEvents(
    String taskId,
    Iterable<Map<String, dynamic>> events,
  ) {
    final text = StringBuffer();
    final reasoning = StringBuffer();
    String statusHint = '';
    final goalProgress = <GoalProgressEntry>[];
    final planHistory = <PlanHistorySnapshot>[];
    final toolCalls = <ToolCallInfo>[];
    final blocks = <ChatMessageBlock>[];
    var errorDetail = '';
    ContextUsageInfo? contextUsage;
    var finalDeliveryPhase = FinalDeliveryPhase.idle;
    final planKeys = <String>{};
    final rounds = <_TaskEventRoundBuilder>[];
    final appliedInputs = <TaskInputAppliedInfo>[];
    // 任务是否已结束(completed/failed/cancelled)。任务结束后,残留在
    // running/pending 的工具(如执行较慢、completed 事件晚于任务终态的 read)
    // 应被视为已完成,避免历史会话里出现"工具一直调用中"的假状态。
    var sawTerminal = false;

    _TaskEventRoundBuilder currentRound(Map<String, dynamic> event) {
      if (rounds.isEmpty) {
        rounds.add(_TaskEventRoundBuilder(startedAt: _parseEventSentAt(event)));
      } else {
        rounds.last.startedAt ??= _parseEventSentAt(event);
      }
      return rounds.last;
    }

    _TaskEventRoundBuilder startGoalRound(Map<String, dynamic> event) {
      final startedAt = _parseEventSentAt(event);
      if (rounds.isEmpty || rounds.last.hasOutput) {
        rounds.add(_TaskEventRoundBuilder(startedAt: startedAt));
      } else {
        rounds.last.startedAt ??= startedAt;
      }
      return rounds.last;
    }

    void startInputRound(Map<String, dynamic> event) {
      final metadata = _asMap(event['metadata']);
      final source = metadata['source']?.toString() ?? 'user_queue';
      final contextType = metadata['context_type']?.toString() ?? 'queueInsert';
      final nodeId = metadata['node_id']?.toString().trim() ?? '';
      final isSubagent =
          contextType == 'subagentResult' || source == 'subagent';
      if (isSubagent && _validSubagentNodeID(nodeId)) {
        final resultMetadata = {...metadata, 'phase': 'result'};
        final tool = subagentToolCallFromMetadata(
          taskId: taskId,
          metadata: resultMetadata,
          status: metadata['status']?.toString() ?? 'completed',
        );
        _upsertToolCall(toolCalls, tool);
        _upsertToolBlock(blocks, tool);
        currentRound(event).upsertToolCall(tool);
      }
      appliedInputs.add(
        TaskInputAppliedInfo(
          queueItemId: metadata['queue_item_id']?.toString() ?? '',
          content: event['content'] as String? ?? '',
          injectionVersion:
              (metadata['injection_version'] as num?)?.toInt() ?? 0,
          appliedAt: _parseEventSentAt(event),
          afterRoundIndex: rounds.length - 1,
          contextSource: metadata['source']?.toString() ?? 'user_queue',
          contextType: metadata['context_type']?.toString() ?? 'queueInsert',
          contextMetadata: metadata,
        ),
      );
      rounds.add(_TaskEventRoundBuilder(startedAt: _parseEventSentAt(event)));
    }

    for (final event in events) {
      final type = event['type'] as String?;
      switch (type) {
        case 'delta':
          final content = event['content'] as String? ?? '';
          if (content.isEmpty) continue;
          final field = event['field'] as String? ?? 'text';
          if (field == 'reasoning') {
            reasoning.write(content);
            currentRound(event).reasoning.write(content);
          } else {
            text.write(content);
            _appendTextBlock(blocks, content);
            statusHint = '';
            final round = currentRound(event);
            round.text.write(content);
            round.appendTextBlock(content);
            round.statusHint = '';
          }
          break;
        case 'input_applied':
          startInputRound(event);
          break;
        case 'retrying':
          statusHint = event['content'] as String? ?? '';
          currentRound(event).statusHint = statusHint;
          break;
        case 'progress':
          final deliveryPhase = finalDeliveryPhaseFromProgressEvent(event);
          if (deliveryPhase != FinalDeliveryPhase.idle) {
            finalDeliveryPhase = deliveryPhase;
            statusHint = '';
            if (rounds.isNotEmpty) rounds.last.statusHint = '';
          }
          final usage = ContextUsageInfo.fromProgressEvent(event);
          if (usage != null) {
            contextUsage = contextUsage?.merge(usage) ?? usage;
            currentRound(event).recordUsage(usage);
          }
          break;
        case 'compaction_started':
          statusHint = compactionStatusHint;
          currentRound(event).statusHint = statusHint;
          break;
        case 'compaction_completed':
          if (statusHint == compactionStatusHint) {
            statusHint = '';
          }
          if (rounds.isNotEmpty &&
              rounds.last.statusHint == compactionStatusHint) {
            rounds.last.statusHint = '';
          }
          break;
        case 'tool_updated':
          final rawTool = event['tool'] as Map<String, dynamic>?;
          if (rawTool == null) break;
          final tool = normalizeSubagentToolCall(
            ToolCallInfo.fromJson(rawTool),
          );
          if (tool.isPlanOnly) break;
          _upsertToolCall(toolCalls, tool);
          _upsertToolBlock(blocks, tool);
          currentRound(event).upsertToolCall(tool);
          break;
        case 'subagent_started':
          final metadata = {..._asMap(event['metadata']), 'phase': 'running'};
          final nodeId = metadata['node_id']?.toString().trim() ?? '';
          if (!_validSubagentNodeID(nodeId)) break;
          final tool = subagentToolCallFromMetadata(
            taskId: taskId,
            metadata: metadata,
          );
          _upsertToolCall(toolCalls, tool);
          _upsertToolBlock(blocks, tool);
          currentRound(event).upsertToolCall(tool);
          break;
        case 'subagent_state':
          final rawState = event['state']?.toString().trim() ?? '';
          final terminal = const {
            'completed',
            'failed',
            'cancelled',
          }.contains(rawState);
          final metadata = {
            ..._asMap(event['metadata']),
            ...event,
            'phase': terminal ? 'result' : 'running',
          };
          final nodeId = metadata['node_id']?.toString().trim() ?? '';
          if (!_validSubagentNodeID(nodeId)) break;
          final tool = subagentToolCallFromMetadata(
            taskId: taskId,
            metadata: metadata,
            status: rawState.isNotEmpty ? rawState : 'running',
            error: event['error']?.toString() ?? '',
          );
          _upsertToolCall(toolCalls, tool);
          _upsertToolBlock(blocks, tool);
          currentRound(event).upsertToolCall(tool);
          break;
        case 'subagent_result':
          final metadata = {..._asMap(event['metadata']), 'phase': 'result'};
          final nodeId = metadata['node_id']?.toString().trim() ?? '';
          if (!_validSubagentNodeID(nodeId)) break;
          final error =
              event['error']?.toString() ?? metadata['error']?.toString() ?? '';
          final rawStatus = metadata['status']?.toString().trim() ?? '';
          final status = rawStatus.isNotEmpty
              ? rawStatus
              : (error.trim().isNotEmpty ? 'failed' : 'completed');
          final tool = subagentToolCallFromMetadata(
            taskId: taskId,
            metadata: metadata,
            status: status,
            output:
                event['content']?.toString() ??
                metadata['output']?.toString() ??
                '',
            error: error,
          );
          _upsertToolCall(toolCalls, tool);
          _upsertToolBlock(blocks, tool);
          currentRound(event).upsertToolCall(tool);
          break;
        case 'goal_created':
        case 'goal_continued':
          final entry = GoalProgressEntry.fromEvent(event);
          goalProgress.add(entry);
          startGoalRound(event).goalProgress.add(entry);
          break;
        case 'goal_checkpoint':
        case 'goal_completed':
        case 'goal_paused':
        case 'goal_failed':
          final entry = GoalProgressEntry.fromEvent(event);
          goalProgress.add(entry);
          currentRound(event).goalProgress.add(entry);
          if (type == 'goal_paused' || type == 'goal_failed') {
            finalDeliveryPhase = FinalDeliveryPhase.idle;
          }
          break;
        case 'waiting_approval':
        case 'completed':
        case 'failed':
        case 'cancelling':
          finalDeliveryPhase = FinalDeliveryPhase.idle;
          statusHint = '';
          if (rounds.isNotEmpty) rounds.last.statusHint = '';
          if (type == 'failed') {
            errorDetail = _taskEventErrorDetail(event);
            if (rounds.isNotEmpty) rounds.last.errorDetail = errorDetail;
          }
          if (type == 'completed') {
            final metadata = _asMap(event['metadata']);
            final usage = ContextUsageInfo.fromUsageMetadata(
              _asMap(metadata['usage']),
              updatedAt: _parseEventSentAt(event),
            );
            if (usage != null) {
              contextUsage = contextUsage?.merge(usage) ?? usage;
              currentRound(event).recordUsage(usage);
            }
          }
          break;
        case 'plan_updated':
          statusHint = '';
          final rawPlan = event['plan'] as Map<String, dynamic>?;
          if (rawPlan == null) break;
          final plan = PlanInfo.fromJson(rawPlan);
          if (plan.isEmpty) break;
          final capturedAt = _parseEventSentAt(event);
          final snapshot = PlanHistorySnapshot(
            capturedAt: capturedAt,
            plan: plan,
          );
          if (planKeys.add(snapshot.dedupeKey)) {
            planHistory.add(snapshot);
            currentRound(event).planHistory.add(snapshot);
          }
          break;
      }
      if (_isTaskEventTerminal(type)) {
        sawTerminal = true;
      }
    }
    // 任务已终态:把残留 running/pending 的工具回填为 completed,修复
    // "对话已完成但工具显示一直调用中"的历史快照问题。
    if (sawTerminal) {
      for (var i = 0; i < toolCalls.length; i++) {
        if (toolCalls[i].isRunning) {
          toolCalls[i] = toolCalls[i].copyWith(status: 'completed');
        }
      }
      for (var i = 0; i < blocks.length; i++) {
        final tool = blocks[i].tool;
        if (tool != null && tool.isRunning) {
          blocks[i] = blocks[i].copyWith(
            tool: tool.copyWith(status: 'completed'),
          );
        }
      }
      for (final round in rounds) {
        round.finalizeRunningTools();
      }
    }
    return TaskEventSnapshot(
      text: text.toString(),
      reasoning: reasoning.toString(),
      statusHint: statusHint,
      goalProgress: goalProgress,
      planHistory: planHistory,
      toolCalls: List<ToolCallInfo>.unmodifiable(toolCalls),
      blocks: List<ChatMessageBlock>.unmodifiable(blocks),
      errorDetail: errorDetail,
      rounds: rounds
          .map((round) => round.toSnapshot())
          .where((round) => appliedInputs.isNotEmpty || round.hasOutput)
          .toList(growable: false),
      contextUsage: contextUsage,
      appliedInputs: List<TaskInputAppliedInfo>.unmodifiable(appliedInputs),
      finalDeliveryPhase: sawTerminal
          ? FinalDeliveryPhase.idle
          : finalDeliveryPhase,
    );
  }

  bool _isTaskEventTerminal(String? type) {
    return type == 'completed' || type == 'failed' || type == 'cancelled';
  }

  Map<String, dynamic>? _syntheticTerminalEvent(TaskModel task) {
    switch (task.status) {
      case TaskStatus.completed:
        return {
          'type': 'completed',
          'content': task.result ?? '',
          'session_id': task.sessionId,
        };
      case TaskStatus.failed:
        return {
          'type': 'failed',
          'error': task.error?.trim().isNotEmpty == true
              ? task.error
              : '任务执行失败',
        };
      case TaskStatus.cancelled:
        return {
          'type': 'cancelled',
          'content': '已停止',
          'session_id': task.sessionId,
        };
      default:
        return null;
    }
  }

  bool _validSubagentNodeID(String value) {
    final normalized = value.trim().toLowerCase();
    return normalized.isNotEmpty &&
        normalized != '<nil>' &&
        normalized != 'nil' &&
        normalized != 'null';
  }

  String _taskEventErrorDetail(Map<String, dynamic> event) {
    final metadata = _asMap(event['metadata']);
    final direct = _firstString([
      event['error_detail'],
      event['detail'],
      metadata['error_detail'],
      metadata['detail'],
    ]);
    if (direct.isNotEmpty) return direct;
    final error = event['error'] as String?;
    final marker = error?.indexOf('\ndetail:') ?? -1;
    if (error == null || marker < 0) return '';
    return error.substring(marker + '\ndetail:'.length).trim();
  }

  Map<String, dynamic> _asMap(Object? value) {
    if (value is Map<String, dynamic>) return value;
    if (value is Map) return Map<String, dynamic>.from(value);
    return const {};
  }

  String _firstString(List<Object?> values) {
    for (final value in values) {
      if (value is String && value.trim().isNotEmpty) return value.trim();
    }
    return '';
  }

  DateTime _parseEventSentAt(Map<String, dynamic> event) {
    final raw = event['sent_at'] as String?;
    return DateTime.tryParse(raw ?? '') ?? DateTime.now();
  }

  void _upsertToolCall(List<ToolCallInfo> tools, ToolCallInfo tool) {
    final phase = tool.metadata['phase']?.toString().trim() ?? '';
    final nodeId = tool.metadata['node_id']?.toString().trim() ?? '';
    if (isSubagentToolCall(tool) && phase == 'result' && nodeId.isNotEmpty) {
      tools.removeWhere((item) {
        return isSubagentToolCall(item) &&
            (item.metadata['phase']?.toString().trim() ?? '') == 'running' &&
            item.metadata['node_id']?.toString().trim() == nodeId;
      });
    }
    final idx = tools.indexWhere((item) => item.sameIdentity(tool));
    if (idx >= 0) {
      tools[idx] = tools[idx].coalescedWith(tool);
      return;
    }
    tools.add(tool);
  }

  void _appendTextBlock(List<ChatMessageBlock> blocks, String content) {
    if (content.isEmpty) return;
    final lastIndex = blocks.length - 1;
    if (lastIndex >= 0 && blocks[lastIndex].type == ChatMessageBlockType.text) {
      final last = blocks[lastIndex];
      blocks[lastIndex] = last.copyWith(text: '${last.text}$content');
      return;
    }
    blocks.add(
      ChatMessageBlock.text(id: 'text_${blocks.length + 1}', text: content),
    );
  }

  void _upsertToolBlock(List<ChatMessageBlock> blocks, ToolCallInfo tool) {
    final phase = tool.metadata['phase']?.toString().trim() ?? '';
    final nodeId = tool.metadata['node_id']?.toString().trim() ?? '';
    if (isSubagentToolCall(tool) && phase == 'result' && nodeId.isNotEmpty) {
      blocks.removeWhere((block) {
        final existing = block.tool;
        return existing != null &&
            isSubagentToolCall(existing) &&
            (existing.metadata['phase']?.toString().trim() ?? '') ==
                'running' &&
            existing.metadata['node_id']?.toString().trim() == nodeId;
      });
    }
    final index = blocks.indexWhere(
      (block) =>
          block.type == ChatMessageBlockType.tool &&
          block.tool != null &&
          block.tool!.sameIdentity(tool),
    );
    if (index >= 0) {
      blocks[index] = blocks[index].copyWith(
        tool: blocks[index].tool!.coalescedWith(tool),
      );
      return;
    }
    final key = tool.stableKey.trim();
    final suffix = key.isEmpty ? '${blocks.length + 1}' : key;
    blocks.add(ChatMessageBlock.tool(id: 'tool_$suffix', tool: tool));
  }

  Future<String> getTaskResultFromEvents(String taskId) async {
    final snapshot = await getTaskEventSnapshot(taskId);
    return snapshot.text;
  }

  Future<List<TaskModel>> getSessionHistory(
    String sessionId, {
    int limit = 5,
    DateTime? beforeCreatedAt,
    String? beforeTaskId,
  }) async {
    final list = await getSessionHistoryJson(
      sessionId,
      limit: limit,
      beforeCreatedAt: beforeCreatedAt,
      beforeTaskId: beforeTaskId,
    );
    return list.map(TaskModel.fromJson).toList();
  }

  Future<List<Map<String, dynamic>>> getSessionHistoryJson(
    String sessionId, {
    int limit = 5,
    DateTime? beforeCreatedAt,
    String? beforeTaskId,
  }) async {
    final query = <String, String>{
      'session_id': sessionId,
      'limit': limit.toString(),
      'sort': 'created_desc',
      if (beforeCreatedAt != null)
        'before_created_at': beforeCreatedAt.toUtc().toIso8601String(),
      if (beforeTaskId != null && beforeTaskId.isNotEmpty)
        'before_task_id': beforeTaskId,
    };
    final list = await ApiClient.getList(
      '/api/tasks?${Uri(queryParameters: query).query}',
    );
    return list.whereType<Map<String, dynamic>>().toList(growable: false);
  }

  Future<Map<String, dynamic>> getSessionTaskDeltaJson(
    String sessionId, {
    String? afterRevision,
    int limit = 200,
  }) async {
    final query = <String, String>{
      'session_id': sessionId,
      'limit': limit.toString(),
      if (afterRevision != null && afterRevision.isNotEmpty)
        'after_revision': afterRevision,
    };
    return ApiClient.get(
      '/api/tasks/delta?${Uri(queryParameters: query).query}',
    );
  }

  Future<List<ModelInfo>> getModels({String machineId = ''}) async {
    final machine = machineId.trim();
    final path = machine.isEmpty
        ? '/api/models'
        : '/api/models?machine_id=${Uri.encodeQueryComponent(machine)}';
    final data = await ApiClient.get(path);
    return _parseModels(data);
  }

  Future<MCPStatusInfo> getMCPStatus({
    required String machineId,
    required String agentId,
  }) async {
    final data = await ApiClient.get(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/agents/${Uri.encodeComponent(agentId)}/mcp-tools',
    );
    return MCPStatusInfo.fromJson(
      (data['mcp_status'] as Map?)?.cast<String, dynamic>() ??
          const <String, dynamic>{},
    );
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
      final providerBaseUrl = _providerUrlValue(
        p['base_url'] ?? p['baseUrl'] ?? p['api'],
      );
      final providerConsoleUrl = _providerUrlValue(
        p['console_url'] ?? p['consoleUrl'],
      );
      final models = p['models'] as Map<String, dynamic>? ?? {};
      for (final entry in models.entries) {
        final m = entry.value;
        if (m is! Map<String, dynamic>) continue;
        final modelID = entry.key;
        final modelName = m['name'] as String? ?? modelID;
        final status = m['status'] as String? ?? 'active';
        if (status == 'deprecated') continue;
        final variants = <String>[];
        final variantsMap = m['variants'] as Map<String, dynamic>? ?? {};
        variants.addAll(variantsMap.keys);
        final modalities = m['modalities'] as Map<String, dynamic>? ?? {};
        final inputModalities = _stringList(modalities['input']);
        final outputModalities = _stringList(modalities['output']);
        final contextLimit =
            _modelContextLimit(m) ??
            inferGrokContextLimit(
              provider: providerID,
              modelID: modelID,
              modelName: modelName,
            );
        result.add(
          ModelInfo(
            providerID: providerID,
            providerBaseUrl: providerBaseUrl,
            providerConsoleUrl: providerConsoleUrl,
            modelID: modelID,
            name: '$name / $modelName',
            variants: variants,
            image: m['image'] == true,
            inputModalities: inputModalities,
            outputModalities: outputModalities,
            contextLimit: contextLimit,
          ),
        );
      }
    }
    return result;
  }

  String _providerUrlValue(Object? value) {
    if (value is String) return value.trim();
    return '';
  }

  int? _modelContextLimit(Map<String, dynamic> model) {
    final limit = model['limit'];
    if (limit is Map) {
      final value = _positiveInt(limit['context']);
      if (value != null) return value;
    }
    return _positiveInt(model['context_limit']) ??
        _positiveInt(model['context_length']) ??
        _positiveInt(model['context_window']);
  }

  int? _positiveInt(Object? value) {
    if (value is int && value > 0) return value;
    if (value is num && value > 0) return value.round();
    if (value is String) {
      final parsed = int.tryParse(value.trim());
      if (parsed != null && parsed > 0) return parsed;
    }
    return null;
  }

  List<String> _stringList(dynamic raw) {
    if (raw is! List) return const [];
    return raw
        .map((item) => item.toString().trim())
        .where((item) => item.isNotEmpty)
        .toList(growable: false);
  }
}

class _TaskEventRoundBuilder {
  final StringBuffer text = StringBuffer();
  final StringBuffer reasoning = StringBuffer();
  String statusHint = '';
  final List<GoalProgressEntry> goalProgress = [];
  final List<PlanHistorySnapshot> planHistory = [];
  final List<ToolCallInfo> toolCalls = [];
  final List<ChatMessageBlock> blocks = [];
  String errorDetail = '';
  DateTime? startedAt;
  int? tokenCount;

  _TaskEventRoundBuilder({this.startedAt});

  bool get hasOutput =>
      text.isNotEmpty ||
      reasoning.isNotEmpty ||
      statusHint.isNotEmpty ||
      goalProgress.isNotEmpty ||
      planHistory.isNotEmpty ||
      toolCalls.isNotEmpty ||
      blocks.isNotEmpty ||
      errorDetail.isNotEmpty;

  void appendTextBlock(String content) {
    if (content.isEmpty) return;
    final lastIndex = blocks.length - 1;
    if (lastIndex >= 0 && blocks[lastIndex].type == ChatMessageBlockType.text) {
      final last = blocks[lastIndex];
      blocks[lastIndex] = last.copyWith(text: '${last.text}$content');
      return;
    }
    blocks.add(
      ChatMessageBlock.text(id: 'text_${blocks.length + 1}', text: content),
    );
  }

  void upsertToolCall(ToolCallInfo tool) {
    final idx = toolCalls.indexWhere((item) => item.sameIdentity(tool));
    if (idx >= 0) {
      toolCalls[idx] = toolCalls[idx].coalescedWith(tool);
    } else {
      toolCalls.add(tool);
    }
    final blockIndex = blocks.indexWhere(
      (block) =>
          block.type == ChatMessageBlockType.tool &&
          block.tool != null &&
          block.tool!.sameIdentity(tool),
    );
    if (blockIndex >= 0) {
      blocks[blockIndex] = blocks[blockIndex].copyWith(
        tool: blocks[blockIndex].tool!.coalescedWith(tool),
      );
      return;
    }
    final key = tool.stableKey.trim();
    final suffix = key.isEmpty ? '${blocks.length + 1}' : key;
    blocks.add(ChatMessageBlock.tool(id: 'tool_$suffix', tool: tool));
  }

  void recordUsage(ContextUsageInfo usage) {
    final generated = (usage.outputTokens ?? 0) + (usage.reasoningTokens ?? 0);
    if (generated <= 0 || (tokenCount ?? 0) >= generated) return;
    tokenCount = generated;
  }

  // 任务终态后,把本轮残留 running/pending 的工具回填为 completed。
  void finalizeRunningTools() {
    for (var i = 0; i < toolCalls.length; i++) {
      if (toolCalls[i].isRunning) {
        toolCalls[i] = toolCalls[i].copyWith(status: 'completed');
      }
    }
    for (var i = 0; i < blocks.length; i++) {
      final tool = blocks[i].tool;
      if (tool != null && tool.isRunning) {
        blocks[i] = blocks[i].copyWith(
          tool: tool.copyWith(status: 'completed'),
        );
      }
    }
  }

  TaskEventRoundSnapshot toSnapshot() {
    return TaskEventRoundSnapshot(
      text: text.toString(),
      reasoning: reasoning.toString(),
      statusHint: statusHint,
      goalProgress: List<GoalProgressEntry>.unmodifiable(goalProgress),
      planHistory: List<PlanHistorySnapshot>.unmodifiable(planHistory),
      toolCalls: List<ToolCallInfo>.unmodifiable(toolCalls),
      blocks: List<ChatMessageBlock>.unmodifiable(blocks),
      errorDetail: errorDetail,
      startedAt: startedAt,
      tokenCount: tokenCount,
    );
  }
}
