import 'dart:convert';

import 'package:chat_codex_app/features/devices/application/ai_config_sync.dart';
import 'package:flutter_test/flutter_test.dart';

/// 源设备配置：openai 与目标设备冲突，grok 为目标设备没有的供应商，
/// same 与目标设备完全一致（键顺序不同），默认模型指向 grok。
const _sourceRaw = r'''
{
  // 设备 AI 配置（JSONC 允许注释与尾逗号）
  "provider": {
    "openai": {
      "name": "openai",
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "https://api.a.example.com/v1",
        "apiKey": "sk-aaaaaaaaaaaa",
        "apiMode": "responses"
      },
      "models": {
        "gpt-4o": {"name": "gpt-4o"}
      }
    },
    "grok": {
      "options": {
        "baseURL": "https://api.grok.example.com/v1",
        "apiKey": "xai-1234567890",
        "apiMode": "chat"
      },
      "models": {
        "grok-4": {"name": "grok-4"}
      },
    },
    "same": {
      "options": {
        "apiKey": "sk-cccccccccccc",
        "baseURL": "https://same.example.com/v1"
      },
      "models": {
        "m1": {"name": "m1"}
      }
    },
  },
  "model": "grok/grok-4",
}
''';

/// 目标设备配置：openai 内容不同（冲突），local 只有目标设备有，另有其它字段。
const _targetRaw = r'''
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "openai": {
      "name": "openai",
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "https://api.old.example.com/v1",
        "apiKey": "sk-bbbbbbbbbbbb",
        "apiMode": "chat"
      },
      "models": {
        "gpt-4o": {"name": "gpt-4o"}
      }
    },
    "local": {
      "options": {"baseURL": "http://127.0.0.1:11434/v1", "apiKey": "ollama"},
      "models": {"qwen3": {"name": "qwen3"}}
    },
    "same": {
      "models": {"m1": {"name": "m1"}},
      "options": {
        "baseURL": "https://same.example.com/v1",
        "apiKey": "sk-cccccccccccc"
      }
    }
  },
  "model": "openai/gpt-4o",
  "mcp": {"filesystem": {"type": "local"}}
}
''';

void main() {
  group('stripJsonc', () {
    test('去掉注释并保留字符串内的注释符', () {
      const raw = r'''
{
  // 行注释
  "a": 1, /* 块注释 */
  "url": "https://example.com//path",
  "text": "a /* b */ c"
}
''';
      final parsed = jsonDecode(stripJsonc(raw)) as Map<String, dynamic>;
      expect(parsed['a'], 1);
      expect(parsed['url'], 'https://example.com//path');
      expect(parsed['text'], 'a /* b */ c');
    });

    test('去掉对象与数组结尾多余的逗号', () {
      const raw = r'{"a": [1, 2, ], "b": {"c": 3, }, }';
      final parsed = jsonDecode(stripJsonc(raw)) as Map<String, dynamic>;
      expect(parsed['a'], [1, 2]);
      expect((parsed['b'] as Map<String, dynamic>)['c'], 3);
    });

    test('字符串内的转义引号不会提前结束字符串', () {
      const raw = r'{"s": "a\"b // 不是注释", "n": 1}';
      final parsed = jsonDecode(stripJsonc(raw)) as Map<String, dynamic>;
      expect(parsed['s'], 'a"b // 不是注释');
      expect(parsed['n'], 1);
    });

    test('字符串内的逗号与括号不受影响', () {
      const raw = r'{"s": "x,}", "t": "y,]"}';
      final parsed = jsonDecode(stripJsonc(raw)) as Map<String, dynamic>;
      expect(parsed['s'], 'x,}');
      expect(parsed['t'], 'y,]');
    });
  });

  group('parseDeviceConfigRaw', () {
    test('顶层不是对象时直接报错', () {
      expect(() => parseDeviceConfigRaw('[1, 2]'), throwsFormatException);
    });

    test('provider 字段类型不对时直接报错', () {
      expect(
        () => buildAiConfigSyncPreview(
          sourceRaw: '{"provider": []}',
          targetRaw: '{}\n',
        ),
        throwsFormatException,
      );
      expect(
        () => buildSyncedConfigText(
          targetRaw: '{"provider": []}',
          sourceRaw: '{}\n',
          applyProviderIds: const {},
          syncDefaultModel: false,
        ),
        throwsFormatException,
      );
    });
  });

  group('canonicalJson', () {
    test('忽略对象键顺序', () {
      expect(
        canonicalJson({
          'b': 1,
          'a': {
            'd': 2,
            'c': [1, 2],
          },
        }),
        canonicalJson({
          'a': {
            'c': [1, 2],
            'd': 2,
          },
          'b': 1,
        }),
      );
    });

    test('值不同则结果不同', () {
      expect(canonicalJson({'a': 1}), isNot(canonicalJson({'a': 2})));
    });
  });

  group('buildAiConfigSyncPreview', () {
    test('按新增 / 冲突 / 一致 / 仅目标设备分类', () {
      final preview = buildAiConfigSyncPreview(
        sourceRaw: _sourceRaw,
        targetRaw: _targetRaw,
      );
      expect(preview.plan.addedProviders, ['grok']);
      expect(preview.plan.conflictProviders, ['openai']);
      expect(preview.plan.identicalProviders, ['same']);
      expect(preview.plan.targetOnlyProviders, ['local']);
      expect(preview.plan.sourceModel, 'grok/grok-4');
      expect(preview.plan.targetModel, 'openai/gpt-4o');
      expect(preview.plan.modelChanged, isTrue);
      expect(preview.plan.hasWork, isTrue);
    });

    test('冲突项只列出存在差异的字段', () {
      final preview = buildAiConfigSyncPreview(
        sourceRaw: _sourceRaw,
        targetRaw: _targetRaw,
      );
      final diff = preview.providerDiffs['openai']!;
      expect(diff.changedFields, ['接口地址', 'API Key', 'API 模式']);
      expect(
        diff.sourceSummary,
        'https://api.a.example.com/v1 · sk-a****aaaa · 1 个模型',
      );
      expect(
        diff.targetSummary,
        'https://api.old.example.com/v1 · sk-b****bbbb · 1 个模型',
      );
    });

    test('模型清单不同时给出对应差异项', () {
      const source = r'''
{"provider": {"p1": {"options": {"baseURL": "https://x/v1"}, "models": {"m1": {}, "m2": {}}}}, "model": ""}
''';
      const target = r'''
{"provider": {"p1": {"options": {"baseURL": "https://x/v1"}, "models": {"m1": {}}}}, "model": ""}
''';
      final preview = buildAiConfigSyncPreview(
        sourceRaw: source,
        targetRaw: target,
      );
      expect(preview.plan.conflictProviders, ['p1']);
      expect(preview.providerDiffs['p1']!.changedFields, ['模型清单']);
    });

    test('目标设备没有配置时全部按新增处理', () {
      final preview = buildAiConfigSyncPreview(
        sourceRaw: _sourceRaw,
        targetRaw: '{}\n',
      );
      expect(preview.plan.addedProviders, ['grok', 'openai', 'same']);
      expect(preview.plan.conflictProviders, isEmpty);
      expect(preview.plan.identicalProviders, isEmpty);
      expect(preview.plan.targetOnlyProviders, isEmpty);
    });

    test('两端完全一致时没有需要执行的变化', () {
      final preview = buildAiConfigSyncPreview(
        sourceRaw: _sourceRaw,
        targetRaw: _sourceRaw,
      );
      expect(preview.plan.hasWork, isFalse);
      expect(preview.plan.identicalProviders, ['grok', 'openai', 'same']);
    });
  });

  group('buildSyncedConfigText', () {
    test('覆盖冲突项、新增供应商，并保留目标设备独有内容', () {
      final text = buildSyncedConfigText(
        targetRaw: _targetRaw,
        sourceRaw: _sourceRaw,
        applyProviderIds: {'openai', 'grok'},
        syncDefaultModel: false,
      );
      final merged = jsonDecode(text) as Map<String, dynamic>;
      final providers = merged['provider'] as Map<String, dynamic>;
      final openai = providers['openai'] as Map<String, dynamic>;
      final options = openai['options'] as Map<String, dynamic>;
      // 冲突项被源设备对象整体覆盖（含真实密钥）
      expect(options['apiKey'], 'sk-aaaaaaaaaaaa');
      expect(options['baseURL'], 'https://api.a.example.com/v1');
      expect(options['apiMode'], 'responses');
      // 新增供应商整体带入
      expect(providers.containsKey('grok'), isTrue);
      // 目标设备独有供应商与其它字段保持不动
      expect(providers.containsKey('local'), isTrue);
      expect(merged['mcp'], isNotNull);
      expect(merged[r'$schema'], 'https://opencode.ai/config.json');
      // 未勾选默认模型时保持目标设备原值
      expect(merged['model'], 'openai/gpt-4o');
    });

    test('勾选默认模型时写入源设备的默认模型', () {
      final merged =
          jsonDecode(
                buildSyncedConfigText(
                  targetRaw: _targetRaw,
                  sourceRaw: _sourceRaw,
                  applyProviderIds: {'grok'},
                  syncDefaultModel: true,
                ),
              )
              as Map<String, dynamic>;
      expect(merged['model'], 'grok/grok-4');
    });

    test('目标设备没有配置时可以从空配置合并', () {
      final merged =
          jsonDecode(
                buildSyncedConfigText(
                  targetRaw: '{}\n',
                  sourceRaw: _sourceRaw,
                  applyProviderIds: {'grok', 'openai', 'same'},
                  syncDefaultModel: true,
                ),
              )
              as Map<String, dynamic>;
      expect((merged['provider'] as Map<String, dynamic>).length, 3);
      expect(merged['model'], 'grok/grok-4');
    });

    test('应用源设备不存在的供应商时直接报错', () {
      expect(
        () => buildSyncedConfigText(
          targetRaw: _targetRaw,
          sourceRaw: _sourceRaw,
          applyProviderIds: {'nope'},
          syncDefaultModel: false,
        ),
        throwsArgumentError,
      );
    });

    test('源设备未设置默认模型时同步默认模型直接报错', () {
      expect(
        () => buildSyncedConfigText(
          targetRaw: _targetRaw,
          sourceRaw: '{"provider": {}}\n',
          applyProviderIds: const {},
          syncDefaultModel: true,
        ),
        throwsArgumentError,
      );
    });
  });

  group('AiConfigSyncSelection', () {
    test('冲突项的选择决定实际应用的供应商集合', () {
      const plan = AiConfigSyncPlan(
        addedProviders: ['added'],
        conflictProviders: ['c1', 'c2'],
        identicalProviders: [],
        targetOnlyProviders: [],
        sourceModel: '',
        targetModel: '',
      );
      const selection = AiConfigSyncSelection(
        useSource: {'c1': true, 'c2': false},
        syncDefaultModel: false,
      );
      expect(selection.resolveApplyProviderIds(plan), {'added', 'c1'});
    });
  });
}
