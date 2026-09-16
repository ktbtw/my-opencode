import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/data/chat_repository.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  test(
    'ChatRepository parses image model modalities from /api/models',
    () async {
      SharedPreferences.setMockInitialValues({'access_token': 'test-token'});
      await AppStorage.init();

      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      await AppStorage.setBaseUrl(
        'http://${server.address.host}:${server.port}',
      );

      server.listen((request) async {
        expect(request.uri.path, '/api/models');
        request.response.headers.contentType = ContentType.json;
        request.response.write(
          jsonEncode({
            'connected': ['cheap', '订阅grok'],
            'all': [
              {
                'id': 'cheap',
                'name': '超级便宜',
                'base_url': 'https://api.example.com/v1',
                'console_url': 'https://console.example.com',
                'models': {
                  'gpt-image-2': {
                    'name': 'gpt-image-2',
                    'image': true,
                    'limit': {'context': 128000},
                    'modalities': {
                      'input': ['text'],
                      'output': ['image'],
                    },
                  },
                  'gpt-5.4': {
                    'name': 'gpt-5.4',
                    'context_limit': 1050000,
                    'modalities': {
                      'input': ['text', 'image'],
                      'output': ['text'],
                    },
                  },
                },
              },
              {
                'id': '订阅grok',
                'name': '订阅grok',
                'models': {
                  'Kun': {
                    'name': 'Kun',
                    'modalities': {
                      'input': ['text', 'image', 'video'],
                      'output': ['text'],
                    },
                  },
                },
              },
            ],
          }),
        );
        await request.response.close();
      });

      final models = await ChatRepository().getModels(machineId: 'machine-a');

      final imageModel = models.singleWhere((m) => m.modelID == 'gpt-image-2');
      expect(imageModel.image, isTrue);
      expect(imageModel.providerBaseUrl, 'https://api.example.com/v1');
      expect(imageModel.providerConsoleUrl, 'https://console.example.com');
      expect(imageModel.outputModalities, contains('image'));
      expect(imageModel.contextLimit, 128000);
      expect(imageModel.supportsImageOutput, isTrue);

      final textModel = models.singleWhere((m) => m.modelID == 'gpt-5.4');
      expect(textModel.outputModalities, contains('text'));
      expect(textModel.contextLimit, 1050000);
      expect(textModel.supportsImageOutput, isFalse);

      final kunModel = models.singleWhere((m) => m.modelID == 'Kun');
      expect(kunModel.contextLimit, 500000);
      expect(kunModel.contextWindowLabel, '500k 窗口');
      expect(textModel.contextWindowLabel, '1050k 窗口');
      expect(imageModel.contextWindowLabel, '128k 窗口');

      // 选择模型列表内只显示容量数字，不带“窗口”二字。
      expect(kunModel.contextWindowShortLabel, '500k');
      expect(textModel.contextWindowShortLabel, '1050k');
      expect(imageModel.contextWindowShortLabel, '128k');
    },
  );

  test('ChatRepository surfaces model endpoint failures', () async {
    SharedPreferences.setMockInitialValues({'access_token': 'test-token'});
    await AppStorage.init();

    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(server.close);
    await AppStorage.setBaseUrl('http://${server.address.host}:${server.port}');

    server.listen((request) async {
      expect(request.uri.path, '/api/models');
      request.response.statusCode = HttpStatus.serviceUnavailable;
      request.response.headers.contentType = ContentType.json;
      request.response.write(jsonEncode({'error': 'launcher unavailable'}));
      await request.response.close();
    });

    await expectLater(
      ChatRepository().getModels(machineId: 'machine-a'),
      throwsA(anything),
    );
  });

  test('ContextUsageInfo parses llm usage progress metadata', () {
    final usage = ContextUsageInfo.fromProgressEvent({
      'type': 'progress',
      'sent_at': '2026-06-11T12:00:00Z',
      'metadata': {
        'source': 'llm_usage',
        'stage': 'request_context_ready',
        'provider_id': 'cheap',
        'model_id': 'gpt-5.4',
        'agent': 'build',
        'context_limit': 128000,
        'context_tokens': 53800,
        'compaction_count_tokens': 90200,
        'context_usage_percent': 42.0,
        'compaction_threshold_tokens': 107520,
        'compaction_threshold_percent': 84.0,
      },
    });

    expect(usage, isNotNull);
    expect(usage!.stage, 'request_context_ready');
    expect(usage.providerID, 'cheap');
    expect(usage.modelID, 'gpt-5.4');
    expect(usage.contextLimit, 128000);
    expect(usage.contextTokens, 53800);
    expect(usage.compactionCountTokens, 90200);
    expect(usage.knownContextTokens, 90200);
    expect(usage.contextUsagePercent, 42.0);
    expect(usage.compactionThresholdTokens, 107520);
    expect(usage.compactionThresholdPercent, 84.0);
    expect(usage.updatedAt, DateTime.parse('2026-06-11T12:00:00Z'));
  });

  test('ContextUsageInfo parses persisted completed-event usage', () {
    final usage = ContextUsageInfo.fromUsageMetadata({
      'input_tokens': 120,
      'output_tokens': 480,
      'reasoning_tokens': 60,
      'total_tokens': 660,
      'finish_reason': 'stop',
    }, updatedAt: DateTime.parse('2026-06-11T12:02:00Z'));

    expect(usage, isNotNull);
    expect(usage!.inputTokens, 120);
    expect(usage.outputTokens, 480);
    expect(usage.reasoningTokens, 60);
    expect(usage.totalTokens, 660);
    expect(usage.finishReason, 'stop');
    expect(usage.updatedAt, DateTime.parse('2026-06-11T12:02:00Z'));
  });

  test('ContextUsageInfo falls back to selected model context limit', () {
    final usage = ContextUsageInfo.fromUsageMetadata({
      'source': 'llm_usage',
      'stage': 'request_usage_ready',
      'provider_id': '订阅grok',
      'model_id': 'Kun',
      'context_tokens': 12000,
    });

    expect(usage, isNotNull);
    expect(usage!.contextLimit, isNull);
    expect(usage.withFallbackLimit(500000).contextLimit, 500000);
    expect(usage.withFallbackLimit(500000).contextTokens, 12000);
    expect(
      ContextUsageInfo.withModelLimit(usage, 500000)?.contextLimit,
      500000,
    );
    expect(ContextUsageInfo.withModelLimit(null, 500000)?.contextLimit, 500000);
    expect(ContextUsageInfo.withModelLimit(null, null), isNull);
  });

  test(
    'ChatRepository does not send compaction threshold task metadata',
    () async {
      SharedPreferences.setMockInitialValues({'access_token': 'test-token'});
      await AppStorage.init();

      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      await AppStorage.setBaseUrl(
        'http://${server.address.host}:${server.port}',
      );

      final bodySeen = Completer<Map<String, dynamic>>();
      server.listen((request) async {
        expect(request.method, 'POST');
        expect(request.uri.path, '/api/tasks');
        final body = jsonDecode(await utf8.decoder.bind(request).join());
        bodySeen.complete(Map<String, dynamic>.from(body as Map));
        request.response.headers.contentType = ContentType.json;
        request.response.write(
          jsonEncode({
            'task_id': 'task_threshold',
            'agent_id': 'agent_test',
            'project_id': 'project_test',
            'session_id': 'session_test',
            'status': 'running',
            'created_at': '2026-06-12T10:00:00Z',
          }),
        );
        await request.response.close();
      });

      final task = await ChatRepository().createTask(
        agentId: 'agent_test',
        projectId: 'project_test',
        text: '测试压缩阈值',
      );

      final body = await bodySeen.future;
      expect(task.taskId, 'task_threshold');
      expect(body['metadata'], isNot(contains('compaction_threshold_percent')));
    },
  );

  test(
    'ChatRepository sends manual compaction as a dedicated task command',
    () async {
      SharedPreferences.setMockInitialValues({'access_token': 'test-token'});
      await AppStorage.init();

      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      await AppStorage.setBaseUrl(
        'http://${server.address.host}:${server.port}',
      );

      final bodySeen = Completer<Map<String, dynamic>>();
      server.listen((request) async {
        final body = jsonDecode(await utf8.decoder.bind(request).join());
        bodySeen.complete(Map<String, dynamic>.from(body as Map));
        request.response.headers.contentType = ContentType.json;
        request.response.write(
          jsonEncode({
            'task_id': 'task_compact',
            'agent_id': 'agent_test',
            'project_id': 'project_test',
            'session_id': 'session_test',
            'status': 'running',
            'created_at': '2026-06-12T10:00:00Z',
          }),
        );
        await request.response.close();
      });

      final task = await ChatRepository().compactContext(
        agentId: 'agent_test',
        projectId: 'project_test',
        sessionId: 'session_test',
      );
      final body = await bodySeen.future;
      expect(task.taskId, 'task_compact');
      expect(body['session_id'], 'session_test');
      expect(body['parts'], isEmpty);
      expect(
        (body['metadata'] as Map<String, dynamic>)['task_command'],
        'compact',
      );
    },
  );

  group('plan history merge', () {
    test('keeps only the latest revision for the same real plan batch', () {
      final startedAt = DateTime.parse('2026-06-06T01:00:00Z');
      final completedAt = DateTime.parse('2026-06-06T01:02:00Z');
      final pending = _plan(
        id: 'plan_real_1',
        status: 'pending',
        items: [
          _item(id: 'todo_1', text: '检查计划历史', status: 'pending'),
          _item(id: 'todo_2', text: '修复分组规则', status: 'pending'),
        ],
      );
      final completed = _plan(
        id: 'plan_real_1',
        status: 'completed',
        items: [
          _item(id: 'todo_1', text: '检查计划历史', status: 'completed'),
          _item(id: 'todo_2', text: '修复分组规则', status: 'completed'),
        ],
      );

      final merged = mergePlanHistorySnapshotsByBatch(const [], [
        PlanHistorySnapshot(capturedAt: startedAt, plan: pending),
        PlanHistorySnapshot(capturedAt: completedAt, plan: completed),
      ]);

      expect(merged, hasLength(1));
      expect(merged.single.capturedAt, completedAt);
      expect(merged.single.plan.status, 'completed');
      expect(
        merged.single.plan.items.map((item) => item.status),
        everyElement('completed'),
      );
    });

    test('keeps different real plan batches separate', () {
      final first = _plan(
        id: 'plan_real_1',
        status: 'completed',
        items: [_item(text: '完成第一批计划', status: 'completed')],
      );
      final second = _plan(
        id: 'plan_real_2',
        status: 'pending',
        items: [_item(text: '开始第二批计划', status: 'pending')],
      );

      final merged = mergePlanHistorySnapshotsByBatch(const [], [
        PlanHistorySnapshot(
          capturedAt: DateTime.parse('2026-06-06T01:00:00Z'),
          plan: first,
        ),
        PlanHistorySnapshot(
          capturedAt: DateTime.parse('2026-06-06T01:05:00Z'),
          plan: second,
        ),
      ]);

      expect(merged, hasLength(2));
      expect(merged[0].plan.items.single.text, '完成第一批计划');
      expect(merged[1].plan.items.single.text, '开始第二批计划');
    });

    test('ignores plan history snapshots without a real plan id', () {
      final missingID = _plan(
        id: '',
        status: 'pending',
        items: [_item(id: 'todo_1', text: '同步计划批次', status: 'pending')],
      );
      final real = _plan(
        id: 'plan_real_1',
        status: 'completed',
        items: [_item(id: 'todo_1', text: '同步计划批次', status: 'completed')],
      );

      final merged = mergePlanHistorySnapshotsByBatch(const [], [
        PlanHistorySnapshot(
          capturedAt: DateTime.parse('2026-06-06T01:00:00Z'),
          plan: missingID,
        ),
        PlanHistorySnapshot(
          capturedAt: DateTime.parse('2026-06-06T01:03:00Z'),
          plan: real,
        ),
      ]);

      expect(merged, hasLength(1));
      expect(merged.single.plan.id, 'plan_real_1');
      expect(merged.single.plan.status, 'completed');
    });
  });
}

PlanInfo _plan({
  required String id,
  required String status,
  required List<PlanItemInfo> items,
}) {
  return PlanInfo(
    id: id,
    title: '执行计划',
    mode: 'build',
    status: status,
    sessionId: 'ses_1',
    items: items,
  );
}

PlanItemInfo _item({
  String id = 'todo_1',
  required String text,
  required String status,
  String priority = 'medium',
}) {
  return PlanItemInfo(id: id, text: text, status: status, priority: priority);
}
