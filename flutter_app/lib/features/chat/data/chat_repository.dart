import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import '../../../core/config/api_client.dart';
import '../../../core/config/sse_log.dart';
import '../../../core/config/sse_parser.dart';
import '../../../core/services/shared_streams.dart';
import '../../../core/services/connection_telemetry.dart';
import '../../../shared/grok_context_limit.dart';
import 'chat_model.dart';
import 'model_log.dart';

void _chatDiag(String stage, [Map<String, Object?> data = const {}]) {
  debugPrint('[ChatDiag] $stage $data');
}

const compactionStatusHint = '正在压缩上下文';

class ChatRepository {
  static const _maxHistoryEvents = 1200;
  static const _maxHistoryBytes = 8 * 1024 * 1024;
  @visibleForTesting
  Duration taskSnapshotDeadline = const Duration(seconds: 25);

  final _snapshotReads = <String, Future<TaskEventSnapshot>>{};

  final _taskStreams = SharedStreams<Map<String, dynamic>>();
  final _queueStreams = SharedStreams<ChatQueueSnapshot>();
  final _taskConnections = <String, ConnectionTelemetry>{};
  ConnectionTelemetry? taskConnection(String taskId) =>
      _taskConnections[taskId];

  void dispose() {
    _snapshotReads.clear();
    _taskStreams.dispose();
    _queueStreams.dispose();
    _lastTaskEventSequences.clear();
    _lastTaskEventCursors.clear();
    _taskConnections.clear();
  }

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
    ImageGenerationOptions? imageOptions,
    Map<String, String>? extraMetadata,
  }) async {
    final parts = _buildParts(text, attachedFiles);
    final metadata = _buildTaskMetadata(
      model: model,
      permissionMode: permissionMode,
      variant: variant,
      goal: goal,
      goalMaxIterations: goalMaxIterations,
      imageOptions: imageOptions,
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

  Stream<ChatQueueSnapshot> watchChatQueue(String sessionId) =>
      _queueStreams.watch(sessionId, () => _watchChatQueue(sessionId));

  Stream<ChatQueueSnapshot> _watchChatQueue(String sessionId) {
    SseLog.subscribed(
      SseChannel.queue(sessionId),
      '待发送队列状态（session_id=$sessionId）',
    );
    return ApiClient.sse(
      '/api/sessions/${Uri.encodeComponent(sessionId)}/queue/events',
    ).where((raw) => raw.isNotEmpty).expand((raw) {
      try {
        return [
          ChatQueueSnapshot.fromJson(jsonDecode(raw) as Map<String, dynamic>),
        ];
      } catch (_) {
        return <ChatQueueSnapshot>[];
      }
    });
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
    ImageGenerationOptions? imageOptions,
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
          imageOptions: imageOptions,
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
    ImageGenerationOptions? imageOptions,
    Map<String, String>? extraMetadata,
  }) => {
    'permission_mode': permissionMode,
    if (model != null) 'model': model.metaKey,
    if (variant != null && variant.isNotEmpty) 'variant': variant,
    if (goal != null && goal.trim().isNotEmpty) 'goal': goal.trim(),
    if (goal != null && goal.trim().isNotEmpty)
      'goal_max_iterations': goalMaxIterations.toString(),
    ...?imageOptions?.toMetadata(),
    ...?extraMetadata,
  };

  Future<TaskModel> getTask(String taskId) async {
    final data = await ApiClient.get(
      '/api/tasks/$taskId',
      timeout: const Duration(seconds: 6),
    );
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

  /// Signals consumers fetch their own snapshot after subscribing. They share
  /// transport with the conversation without replaying every text delta.
  Stream<Map<String, dynamic>> watchTaskSignals(String taskId) =>
      _taskStreams.watch(taskId, () => _watchTaskEventsInternal(taskId));

  Stream<Map<String, dynamic>> watchTaskEvents(String taskId) {
    late StreamController<Map<String, dynamic>> controller;
    StreamSubscription<Map<String, dynamic>>? live;
    StreamSubscription<Map<String, dynamic>>? history;
    final pending = <Map<String, dynamic>>[];
    final seen = <String>{};
    var catchingUp = false;
    var closed = false;
    var liveDone = false;
    var pendingBytes = 0;

    String key(Map<String, dynamic> event) =>
        '${event['id'] ?? event['sequence']}|${event['sent_at']}';
    void stop() {
      closed = true;
      pending.clear();
      seen.clear();
      unawaited(live?.cancel());
      unawaited(history?.cancel());
    }

    controller = StreamController<Map<String, dynamic>>(
      onListen: () {
        catchingUp = _taskStreams.contains(taskId);
        // Attach first: events arriving during the HTTP snapshot are buffered.
        live = watchTaskSignals(taskId).listen(
          (event) {
            if (closed) return;
            if (!catchingUp) {
              controller.add(event);
              return;
            }
            pendingBytes += jsonEncode(event).length * 2;
            if (pending.length >= 4096 || pendingBytes > 4 * 1024 * 1024) {
              controller.addError(StateError('Task catch-up buffer exceeded'));
              stop();
              unawaited(controller.close());
              return;
            }
            pending.add(event);
          },
          onError: controller.addError,
          onDone: () {
            liveDone = true;
            if (!catchingUp && !closed) unawaited(controller.close());
          },
        );
        if (!catchingUp) return;
        history =
            _watchPagedTaskEvents(
              taskId,
              '/api/tasks/${Uri.encodeComponent(taskId)}/event-pages',
              source: 'shared-stream-catch-up',
            ).listen(
              (event) {
                if (closed) return;
                // Retain only keys overlapping the bounded live buffer. Historical
                // events need no unbounded replay cache in the subscription manager.
                if (pending.any((item) => key(item) == key(event))) {
                  seen.add(key(event));
                }
                controller.add(event);
              },
              onError: (Object error, StackTrace stack) {
                if (closed) return;
                controller.addError(error, stack);
                stop();
                unawaited(controller.close());
              },
              onDone: () {
                if (closed) return;
                catchingUp = false;
                for (final event in pending) {
                  if (!seen.contains(key(event))) controller.add(event);
                }
                pending.clear();
                seen.clear();
                if (liveDone) unawaited(controller.close());
              },
            );
      },
      onCancel: () async {
        closed = true;
        pending.clear();
        seen.clear();
        await Future.wait([
          if (live != null) live!.cancel(),
          if (history != null) history!.cancel(),
        ]);
      },
    );
    return controller.stream;
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
      if (yielded) rethrow;
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
      if (yielded) rethrow;
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
    String? historySource;
    while (true) {
      final query = Uri(
        queryParameters: {
          'limit': '200',
          if (cursor != null && cursor.isNotEmpty) 'cursor': cursor,
        },
      ).query;
      final page = await ApiClient.get(
        '$path?$query',
        timeout: const Duration(seconds: 18),
      );
      final currentSource = page['source']?.toString();
      if (historySource != null && currentSource != historySource) {
        throw const FormatException('History source changed during pagination');
      }
      historySource = currentSource;
      final events = (page['events'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map((event) => Map<String, dynamic>.from(event))
          .toList(growable: false);
      for (final event in events) {
        yield {
          ...event,
          if (page['source'] == 'agent_local') '_reconstructed_history': true,
        };
      }
      if (page['has_more'] != true) {
        if (page['complete'] == false) {
          throw const FormatException('History is incomplete');
        }
        return;
      }
      final nextCursor = page['next_cursor']?.toString() ?? '';
      if (nextCursor.isEmpty || nextCursor == cursor) {
        throw const FormatException('Invalid task event page cursor');
      }
      cursor = nextCursor;
    }
  }

  Stream<Map<String, dynamic>> _watchTaskEventsInternal(String taskId) {
    // A new shared source may belong to a rebuilt conversation. Its body and
    // dedupe state were reset; a cursor retained from an older source would
    // skip all existing output. Reconnects within this source still resume.
    _lastTaskEventSequences.remove(taskId);
    _lastTaskEventCursors.remove(taskId);
    final telemetry = ConnectionTelemetry();
    _taskConnections[taskId] = telemetry;
    var cancelled = false;
    StreamSubscription<SseEvent>? subscription;
    Timer? reconnect;
    late final StreamController<Map<String, dynamic>> controller;
    late void Function() connect;

    void retry(String reason, [Object? error]) {
      if (cancelled) return;
      telemetry.disconnected(retry: true);
      SseLog.retry(
        SseChannel.task(taskId),
        1,
        1000,
        '$reason${error == null ? '' : '（$error）'}',
      );
      reconnect?.cancel();
      reconnect = Timer(const Duration(seconds: 1), connect);
    }

    connect = () {
      if (cancelled) return;
      telemetry.connecting();
      final lastSequence = _lastTaskEventSequences[taskId];
      final lastCursor = _lastTaskEventCursors[taskId];
      final startedAt = DateTime.now();
      var first = true;
      var terminal = false;
      var decodeFailures = 0;
      subscription =
          ApiClient.sseEvents(
            '/api/tasks/${Uri.encodeComponent(taskId)}/events',
            onConnected: () {
              telemetry.activity();
              if (!cancelled) {
                controller.add({'type': 'connection_ready', 'task_id': taskId});
              }
            },
            onActivity: telemetry.activity,
            extraHeaders: {
              if (lastSequence != null && lastSequence > 0)
                'Last-Event-ID': '$lastSequence',
              if (lastCursor != null && lastCursor.isNotEmpty)
                'X-Task-Event-Cursor': lastCursor,
            },
          ).listen(
            (frame) {
              final raw = frame.data;
              if (cancelled || raw.isEmpty) return;
              try {
                final event = jsonDecode(raw) as Map<String, dynamic>;
                telemetry.activity();
                if (event['type'] == 'delta' && event['field'] == 'text') {
                  telemetry.output(event['content'] as String? ?? '');
                }
                _rememberTaskEventCursor(taskId, event);
                if (first) {
                  first = false;
                  _chatDiag('sse_first_frame', {
                    'task_id': taskId,
                    'type': event['type'],
                    'wait_ms': DateTime.now()
                        .difference(startedAt)
                        .inMilliseconds,
                    'sequence': event['sequence'],
                  });
                }
                controller.add(event);
                terminal = _isTaskEventTerminal(event['type']?.toString());
              } catch (error) {
                _chatDiag('sse_decode_failed', {
                  'task_id': taskId,
                  'count': ++decodeFailures,
                  'error': error.toString(),
                  'raw_len': raw.length,
                });
              }
            },
            onError: (Object error, StackTrace stack) {
              if (cancelled) return;
              telemetry.disconnected(retry: true);
              if (error is SseHttpException) {
                controller.addError(error, stack);
                unawaited(controller.close());
                return;
              }
              retry('error', error);
            },
            onDone: () async {
              if (cancelled) return;
              telemetry.disconnected();
              if (terminal) {
                unawaited(controller.close());
                return;
              }
              try {
                final synthetic = _syntheticTerminalEvent(
                  await getTask(taskId),
                );
                if (cancelled) return;
                if (synthetic != null) {
                  controller.add(synthetic);
                  unawaited(controller.close());
                  return;
                }
              } catch (_) {}
              retry('stream_ended');
            },
            cancelOnError: true,
          );
      if (controller.isPaused) subscription?.pause();
    };
    controller = StreamController<Map<String, dynamic>>(
      onListen: connect,
      onPause: () => subscription?.pause(),
      onResume: () => subscription?.resume(),
      onCancel: () async {
        cancelled = true;
        telemetry.disconnected();
        if (identical(_taskConnections[taskId], telemetry)) {
          _taskConnections.remove(taskId);
        }
        reconnect?.cancel();
        await subscription?.cancel();
      },
    );
    return controller.stream;
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
    SseLog.subscribed(
      SseChannel.modelTest(testId),
      '模型测速过程与结果（test_id=$testId）',
    );
    await for (final raw in ApiClient.sse('/api/model-tests/$testId/events')) {
      if (raw.isEmpty) continue;
      try {
        final json = jsonDecode(raw) as Map<String, dynamic>;
        yield json;
      } catch (_) {}
    }
  }

  Future<TaskEventSnapshot> getTaskEventSnapshot(String taskId) {
    final active = _snapshotReads[taskId];
    if (active != null) return active;
    late final Future<TaskEventSnapshot> pending;
    pending = _collectTaskEventSnapshot(taskId).whenComplete(() {
      if (identical(_snapshotReads[taskId], pending)) {
        _snapshotReads.remove(taskId);
      }
    });
    _snapshotReads[taskId] = pending;
    return pending;
  }

  Future<TaskEventSnapshot> _collectTaskEventSnapshot(String taskId) async {
    final events = <Map<String, dynamic>>[];
    var timedOut = false;
    var readFailed = false;
    var truncated = false;
    var eventBytes = 0;
    final finished = Completer<void>();
    StreamSubscription<Map<String, dynamic>>? subscription;
    void finish() {
      if (!finished.isCompleted) finished.complete();
    }

    // A Stream.timeout followed by await-for still awaits async* cancellation.
    // That cancellation may be waiting for an HTTP response which never comes.
    // Complete the result independently and ignore every late event.
    final deadline = Timer(taskSnapshotDeadline, () {
      timedOut = true;
      finish();
    });
    try {
      subscription = _watchTaskSnapshotEvents(taskId).listen(
        (event) {
          if (finished.isCompleted) return;
          final weight = _historyEventWeight(event);
          if (events.length >= _maxHistoryEvents ||
              eventBytes + weight > _maxHistoryBytes) {
            truncated = true;
            finish();
            unawaited(subscription?.cancel());
            return;
          }
          eventBytes += weight;
          events.add(event);
        },
        onError: (Object error, StackTrace stack) {
          if (finished.isCompleted) return;
          readFailed = true;
          _chatDiag('history_snapshot_error', {
            'task_id': taskId,
            'error': error.toString(),
            'events': events.length,
          });
          finish();
        },
        onDone: finish,
      );
      await finished.future;
    } finally {
      deadline.cancel();
      final cancellation = subscription?.cancel();
      if (cancellation != null) {
        unawaited(
          cancellation.catchError((Object error) {
            _chatDiag('history_cancel_error', {
              'task_id': taskId,
              'error': error.toString(),
            });
          }),
        );
      }
    }
    final snapshot = snapshotFromTaskEvents(
      taskId,
      events,
      complete: !timedOut && !readFailed && !truncated,
    );
    return snapshot;
  }

  int _historyEventWeight(Map<String, dynamic> event) {
    var total = 64 + (event['content']?.toString().length ?? 0);
    final tool = event['tool'];
    if (tool is Map) {
      total += tool['output']?.toString().length ?? 0;
      total += tool['error']?.toString().length ?? 0;
    }
    return total;
  }

  TaskEventSnapshot snapshotFromTaskEvents(
    String taskId,
    Iterable<Map<String, dynamic>> events, {
    bool complete = true,
  }) {
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
    final supplementalStatistics = <Map<String, dynamic>>[];
    // 任务是否已结束(completed/failed/cancelled)。任务结束后,残留在
    // running/pending 的工具(如执行较慢、completed 事件晚于任务终态的 read)
    // 应被视为已完成,避免历史会话里出现"工具一直调用中"的假状态。
    var sawTerminal = false;
    var sawTextDelta = false;

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
      // A subagent result only opens a new round when the relay woke the main
      // agent. Otherwise it is injected into the round that is still running,
      // so no round boundary and no standalone applied input are recorded.
      if (isSubagent &&
          (metadata['wake_reason']?.toString().trim().isEmpty ?? true)) {
        return;
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
      final rawMetadata = _asMap(event['metadata']);
      if (type == 'progress' && rawMetadata['_archived_statistics'] == true) {
        supplementalStatistics.add(event);
        final usage = ContextUsageInfo.fromProgressEvent(event);
        if (usage != null) contextUsage = contextUsage?.merge(usage) ?? usage;
        continue;
      }
      switch (type) {
        case 'delta':
          final content = event['content'] as String? ?? '';
          if (content.isEmpty) continue;
          final field = event['field'] as String? ?? 'text';
          if (field == 'reasoning') {
            final round = currentRound(event);
            reasoning.write(content);
            round.reasoning.write(content);
          } else {
            sawTextDelta = true;
            text.write(content);
            _appendTextBlock(blocks, content);
            statusHint = '';
            final round = currentRound(event);
            if (event['_reconstructed_history'] != true) {
              round.statistics = round.statistics
                  .recordFirstText(
                    DateTime.tryParse(
                      (event['_first_delta_sent_at'] ?? event['sent_at'])
                              ?.toString() ??
                          '',
                    ),
                  )
                  .recordUsage(rawMetadata);
            }
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
          }
          if (rawMetadata['source'] == 'llm_usage') {
            currentRound(event).recordUsage(rawMetadata);
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
            var completedBody = metadata['round_result'] as String? ?? '';
            final hasRoundResult = completedBody.trim().isNotEmpty;
            if (completedBody.trim().isEmpty) {
              completedBody = event['content'] as String? ?? '';
              final preceding = rounds
                  .take(rounds.isEmpty ? 0 : rounds.length - 1)
                  .map((round) => round.text.toString())
                  .join();
              if (preceding.isNotEmpty && completedBody.startsWith(preceding)) {
                completedBody = completedBody.substring(preceding.length);
              }
            }
            if (completedBody.trim().isNotEmpty) {
              // The terminal payload may contain text that never arrived as
              // deltas (or was omitted by reconstructed history). Recover it
              // in the last round, preserving earlier rounds and tool order.
              currentRound(
                event,
              ).recoverBody(completedBody, authoritative: hasRoundResult);
              text
                ..clear()
                ..writeAll(rounds.map((round) => round.text.toString()));
              blocks
                ..clear()
                ..addAll(rounds.expand((round) => round.blocks));
            }
            final usage = ContextUsageInfo.fromUsageMetadata(
              _asMap(event['usage']).isNotEmpty
                  ? _asMap(event['usage'])
                  : _asMap(metadata['usage']),
              updatedAt: _parseEventSentAt(event),
            );
            if (usage != null) {
              contextUsage = contextUsage?.merge(usage) ?? usage;
              supplementalStatistics.add({
                ...event,
                'metadata': _asMap(event['usage']).isNotEmpty
                    ? _asMap(event['usage'])
                    : _asMap(metadata['usage']),
              });
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
    // Archive supplements arrive after reconstructed text to preserve its
    // ordering. Attribute them to their original reply, never the last bubble.
    final orderedStatistics = [
      ...supplementalStatistics.where(
        (e) => _asMap(e['metadata'])['reply_round_index'] != null,
      ),
      ...supplementalStatistics.where(
        (e) => _asMap(e['metadata'])['reply_round_index'] == null,
      ),
    ];
    for (final event in orderedStatistics) {
      final metadata = _asMap(event['metadata']);
      if (rounds.isEmpty) currentRound(event);
      final requestId = metadata['message_id']?.toString();
      var index = requestId == null
          ? -1
          : rounds.indexWhere(
              (r) => r.statistics.requests.containsKey(requestId),
            );
      final recordedIndex = (metadata['reply_round_index'] as num?)?.toInt();
      if (index < 0 &&
          recordedIndex != null &&
          recordedIndex >= 0 &&
          recordedIndex < rounds.length) {
        index = recordedIndex;
      }
      if (index < 0) {
        final at =
            DateTime.tryParse(
              metadata['reply_round_started_at']?.toString() ?? '',
            ) ??
            _parseEventSentAt(event);
        index = 0;
        for (var i = 1; i < rounds.length; i++) {
          if (rounds[i].startedAt != null &&
              !rounds[i].startedAt!.isAfter(at)) {
            index = i;
          }
        }
      }
      rounds[index].recordUsage(metadata);
    }
    // 供应商把思考内容又当正文发一遍时（deepseek 系经代理出现过 reasoning
    // 与 text 逐字节相同），正文气泡会显示成思考过程。按"本轮正文 == 本轮思考"
    // 剔除这段正文，思考本身保留；实时与历史重建都走这里。
    var droppedReasoningEcho = false;
    for (final round in rounds) {
      final body = round.text.toString().trim();
      final think = round.reasoning.toString().trim();
      if (body.isEmpty || think.isEmpty || body != think) continue;
      round.text.clear();
      round.blocks.removeWhere((block) => block.isText);
      droppedReasoningEcho = true;
    }
    if (droppedReasoningEcho) {
      text.clear();
      text.writeAll(rounds.map((round) => round.text.toString()));
      blocks.clear();
      blocks.addAll(rounds.expand((round) => round.blocks));
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
      complete: complete,
      preservesToolPositions: toolCalls.isEmpty || sawTextDelta,
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
      statistics: rounds.fold<ReplyStatistics>(
        const ReplyStatistics(),
        (stats, round) => stats.merge(round.statistics),
      ),
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
    final startedAt = DateTime.now();
    ModelLog.listRequested(path);
    final Map<String, dynamic> data;
    try {
      data = await ApiClient.get(path);
    } catch (error) {
      ModelLog.listFailed(
        path,
        error,
        DateTime.now().difference(startedAt).inMilliseconds,
      );
      rethrow;
    }
    final models = _parseModels(data);
    ModelLog.listLoaded(
      path,
      models.length,
      DateTime.now().difference(startedAt).inMilliseconds,
    );
    return models;
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
    var skippedProvider = 0;
    var skippedDeprecated = 0;

    for (final p in allProviders) {
      if (p is! Map<String, dynamic>) continue;
      final providerID = p['id'] as String? ?? '';
      if (providerID.isEmpty || !connected.contains(providerID)) {
        skippedProvider += 1;
        continue;
      }
      final name = p['name'] as String? ?? providerID;
      // 会员 provider：平台会打 member 标记，App 端置顶并用金色展示
      final memberProvider = p['member'] == true;
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
        final memberModel = memberProvider || m['member'] == true;
        final armorModel = m['armor'] == true;
        final status = m['status'] as String? ?? 'active';
        if (status == 'deprecated') {
          skippedDeprecated += 1;
          continue;
        }
        final variants = <String>[];
        final variantsMap = m['variants'] as Map<String, dynamic>? ?? {};
        variants.addAll(variantsMap.keys);
        final modalities = m['modalities'] as Map<String, dynamic>? ?? {};
        final inputModalities = _stringList(modalities['input']);
        final outputModalities = _stringList(modalities['output']);
        final imageCapabilitiesJson =
            m['image_capabilities'] as Map<String, dynamic>?;
        final imageCapabilities = imageCapabilitiesJson == null
            ? null
            : ImageGenerationCapabilities.fromJson(imageCapabilitiesJson);
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
            providerName: name,
            providerBaseUrl: providerBaseUrl,
            providerConsoleUrl: providerConsoleUrl,
            modelID: modelID,
            modelName: modelName,
            member: memberModel,
            armor: armorModel,
            name: '$name / $modelName',
            variants: variants,
            image: m['image'] == true,
            inputModalities: inputModalities,
            outputModalities: outputModalities,
            contextLimit: contextLimit,
            imageCapabilities: imageCapabilities,
          ),
        );
      }
    }
    ModelLog.listParsed(
      providers: allProviders.length,
      connectedProviders: connected.length,
      skippedProvider: skippedProvider,
      skippedDeprecated: skippedDeprecated,
      models: result.length,
    );
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
  ReplyStatistics statistics = const ReplyStatistics();

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

  void recoverBody(String content, {required bool authoritative}) {
    final existing = text.toString();
    if (existing == content) return;
    if (content.startsWith(existing)) {
      appendTextBlock(content.substring(existing.length));
      text
        ..clear()
        ..write(content);
    } else if (authoritative || content.contains(existing)) {
      final recovered = <ChatMessageBlock>[];
      var inserted = false;
      for (final block in blocks) {
        if (!block.isText) {
          recovered.add(block);
        } else if (!inserted) {
          recovered.add(block.copyWith(text: content));
          inserted = true;
        }
      }
      blocks
        ..clear()
        ..addAll(recovered);
      if (!inserted) appendTextBlock(content);
      text
        ..clear()
        ..write(content);
    } else if (!existing.contains(content)) {
      appendTextBlock('\n\n$content');
      text.write('\n\n$content');
    }
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

  void recordUsage(Map<String, dynamic> usage) {
    statistics = statistics.recordUsage(usage);
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
      tokenCount: statistics.totalTokens,
      statistics: statistics,
    );
  }
}
