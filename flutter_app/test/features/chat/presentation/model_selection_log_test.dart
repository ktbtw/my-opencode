import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/data/model_log.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_provider.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

ModelInfo _model(String provider, String id) => ModelInfo(
  providerID: provider,
  providerBaseUrl: '',
  providerConsoleUrl: '',
  modelID: id,
  name: '$provider / $id',
  variants: const ['low', 'high'],
  image: false,
  inputModalities: const ['text'],
  outputModalities: const ['text'],
  contextLimit: 128000,
);

/// 捕获 debugPrint 输出，用于断言流程日志。
Future<List<String>> _captureLogs(Future<void> Function() body) async {
  final logs = <String>[];
  final previous = debugPrint;
  debugPrint = (String? message, {int? wrapWidth}) {
    if (message != null) logs.add(message);
  };
  try {
    await body();
  } finally {
    debugPrint = previous;
  }
  return logs;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('SelectedModelNotifier 恢复流程日志', () {
    test('可用模型为空时记录跳过原因', () async {
      final notifier = SelectedModelNotifier('agent-1');
      final logs = await _captureLogs(() => notifier.init(const []));
      expect(
        logs.any((line) => line.contains('跳过') && line.contains('可用模型列表为空')),
        isTrue,
        reason: '应记录跳过原因：$logs',
      );
    });

    test('agentId 为空时记录跳过原因', () async {
      final notifier = SelectedModelNotifier('');
      final logs = await _captureLogs(
        () => notifier.init([_model('openai', 'gpt-5')]),
      );
      expect(
        logs.any((line) => line.contains('跳过') && line.contains('agentId 为空')),
        isTrue,
        reason: '应记录跳过原因：$logs',
      );
    });

    test('请求失败时记录失败原因（不再静默吞掉）', () async {
      // 后端地址为空 → 请求必然抛错，覆盖原先 catch(_) {} 的盲区。
      SharedPreferences.setMockInitialValues({});
      await AppStorage.init();
      await AppStorage.setBaseUrl('');
      final notifier = SelectedModelNotifier('agent-1');
      final logs = await _captureLogs(
        () => notifier.init([_model('openai', 'gpt-5')]),
      );
      expect(
        logs.any(
          (line) => line.contains('[模型][恢复]') && line.contains('查询后端保存值'),
        ),
        isTrue,
        reason: '应记录开始查询：$logs',
      );
      expect(
        logs.any((line) => line.contains('失败')),
        isTrue,
        reason: '请求失败必须可见：$logs',
      );
    });
  });

  group('ModelLog 输出格式', () {
    test('恢复与列表使用可区分的中文前缀', () async {
      final logs = await _captureLogs(() async {
        ModelLog.restoreStart('agent-1', 3);
        ModelLog.listRequested('/api/models');
      });
      expect(logs.any((line) => line.startsWith('[模型][恢复]')), isTrue);
      expect(logs.any((line) => line.startsWith('[模型][列表]')), isTrue);
    });

    test('恢复命中与未命中输出不同结论', () async {
      final logs = await _captureLogs(() async {
        ModelLog.restoreMatched(
          'agent-1',
          'openai/gpt-5',
          'openai/gpt-5',
          via: '完整键匹配',
        );
        ModelLog.restoreNotFound('agent-1', 'openai/gone', 3);
      });
      expect(logs.any((line) => line.contains('已还原为')), isTrue);
      expect(logs.any((line) => line.contains('无匹配')), isTrue);
    });

    test('列表解析统计包含过滤原因', () async {
      final logs = await _captureLogs(() async {
        ModelLog.listParsed(
          providers: 5,
          connectedProviders: 2,
          skippedProvider: 3,
          skippedDeprecated: 1,
          models: 7,
        );
      });
      final line = logs.firstWhere((l) => l.contains('解析结果'));
      expect(line, contains('供应商5个'));
      expect(line, contains('已连接2个'));
      expect(line, contains('因未连接跳过3个'));
      expect(line, contains('因已废弃跳过1个'));
      expect(line, contains('最终模型7个'));
    });
  });
}
