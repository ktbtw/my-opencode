import 'package:flutter_test/flutter_test.dart';
import 'package:chat_codex_app/features/devices/data/device_ai_config_model.dart';

void main() {
  group('DeviceAIModelInfo thinking capability', () {
    test('parses and serializes detected thinking metadata', () {
      final model = DeviceAIModelInfo.fromJson({
        'id': 'stealth/ox-alpha',
        'name': 'Ox Alpha',
        'owned_by': 'openrouter',
        'variants': {
          'low': {
            'reasoning': {'effort': 'low'},
          },
          'high': {
            'reasoning': {'effort': 'high'},
          },
        },
        'thinking': {
          'supported': true,
          'source': 'provider',
          'control': 'effort',
          'protocol': 'openrouter',
          'supported_parameters': ['reasoning', 'include_reasoning'],
          'override_enabled': false,
          'variants': {
            'low': {
              'reasoning': {'effort': 'low'},
            },
            'high': {
              'reasoning': {'effort': 'high'},
            },
          },
        },
      });

      expect(model.thinking?.supported, isTrue);
      expect(model.thinking?.source, 'provider');
      expect(model.thinking?.protocol, 'openrouter');
      expect(model.thinking?.supportedParameters, [
        'reasoning',
        'include_reasoning',
      ]);
      expect(model.thinking?.variants.keys, ['low', 'high']);

      final encoded = model.toJson();
      expect(encoded['thinking'], isA<Map<String, dynamic>>());
      expect(
        (encoded['thinking'] as Map<String, dynamic>)['override_enabled'],
        isFalse,
      );
    });

    test('treats legacy explicit variants as a manual override', () {
      final model = DeviceAIModelInfo.fromJson({
        'id': 'custom-model',
        'variants': {
          'deep': {'reasoningEffort': 'high'},
        },
      });

      expect(model.thinking?.source, 'manual');
      expect(model.thinking?.overrideEnabled, isTrue);
      expect(model.thinking?.overrideVariants.keys, ['deep']);
    });
  });

  group('DeviceAIConfigInfo', () {
    test('parses provider api mode from device config payload', () {
      final config = DeviceAIConfigInfo.fromJson({
        'exists': true,
        'config_path': '/tmp/opencode.json',
        'provider': 'ai2',
        'base_url': 'https://api.example.com/v1',
        'console_url': 'https://console.example.com',
        'api_key_masked': 'sk-****',
        'api_mode': 'responses',
        'model': 'gpt-5.4',
        'raw_json': '{}',
        'preview_json': '{}',
        'providers': [
          {
            'id': 'ai2',
            'base_url': 'https://api.example.com/v1',
            'console_url': 'https://console.example.com',
            'api_key_masked': 'sk-****',
            'api_mode': 'responses',
            'models': [
              {'id': 'gpt-5.4', 'name': 'gpt-5.4'},
            ],
          },
          {'id': 'cheap', 'base_url': 'https://cheap.example.com/v1'},
        ],
      });

      expect(config.apiMode, 'responses');
      expect(config.consoleUrl, 'https://console.example.com');
      expect(config.providers, hasLength(2));
      expect(config.providers[0].consoleUrl, 'https://console.example.com');
      expect(config.providers[0].apiMode, 'responses');
      expect(config.providers[1].apiMode, 'responses');
    });

    test('defaults missing or unknown api mode to responses', () {
      final provider = DeviceAIProviderInfo.fromJson({
        'id': 'demo',
        'api_mode': 'legacy',
      });
      final config = DeviceAIConfigInfo.fromJson({
        'exists': true,
        'provider': 'demo',
        'api_mode': 'legacy',
      });

      expect(provider.apiMode, 'responses');
      expect(config.apiMode, 'responses');
    });

    test('keeps explicit chat api mode', () {
      final provider = DeviceAIProviderInfo.fromJson({
        'id': 'demo',
        'api_mode': 'chat',
      });
      final config = DeviceAIConfigInfo.fromJson({
        'exists': true,
        'provider': 'demo',
        'api_mode': 'chat',
      });

      expect(provider.apiMode, 'chat');
      expect(config.apiMode, 'chat');
    });
  });

  group('DeviceAIModelInfo context window', () {
    test('parses nested limit.context for grok aliases', () {
      final model = DeviceAIModelInfo.fromJson({
        'id': 'Kun',
        'name': 'Kun',
        'owned_by': '订阅grok',
        'limit': {'context': 500000},
      });

      expect(model.contextLimit, 500000);
      expect(model.toJson()['context_limit'], 500000);
    });

    test('parses explicit context_limit', () {
      final model = DeviceAIModelInfo.fromJson({
        'id': 'grok-4.6',
        'context_limit': 500000,
      });

      expect(model.contextLimit, 500000);
    });

    test('infers grok subscription Kun context when fields are missing', () {
      final model = DeviceAIModelInfo.fromJson({
        'id': 'Kun',
        'name': 'Kun',
        'owned_by': '订阅grok',
      });

      expect(model.contextLimit, 500000);
      expect(model.toJson()['context_limit'], 500000);
    });

    test('does not infer Kun context for unrelated providers', () {
      final model = DeviceAIModelInfo.fromJson({
        'id': 'Kun',
        'owned_by': 'custom',
      });

      expect(model.contextLimit, isNull);
    });

    test('infers Kun context from provider id when owned_by is missing', () {
      final provider = DeviceAIProviderInfo.fromJson({
        'id': '订阅grok',
        'models': [
          {'id': 'Kun', 'name': 'Kun'},
        ],
      });

      expect(provider.models.single.ownedBy, '订阅grok');
      expect(provider.models.single.contextLimit, 500000);
    });

    test('infers Kun window from saved grok config shape', () {
      final model = DeviceAIModelInfo.fromJson({
        'id': 'Kun',
        'name': 'Kun',
        'owned_by': '订阅grok',
        'reasoning': true,
        'modalities': {
          'input': ['text', 'image', 'video'],
          'output': ['text'],
        },
        'variants': {
          'high': {'reasoningEffort': 'high'},
        },
        'x-operit-thinking': {
          'control': 'effort',
          'override_enabled': true,
          'protocol': 'custom',
          'source': 'manual',
          'supported': true,
        },
      });

      expect(model.contextLimit, 500000);
      expect(formatContextWindow(model.contextLimit), '500k 窗口');
    });

    test('infers Kun window from sparse device config payload', () {
      final config = DeviceAIConfigInfo.fromJson({
        'exists': true,
        'provider': '订阅grok',
        'providers': [
          {
            'id': '订阅grok',
            'models': [
              {'id': 'Kun', 'name': 'Kun'},
              {'id': 'deepseek-v4.1-flash', 'name': 'deepseek-v4.1-flash'},
            ],
          },
        ],
      });

      expect(config.providers.single.models.first.contextLimit, 500000);
      expect(config.providers.single.models.last.contextLimit, isNull);
      expect(
        formatContextWindow(config.providers.single.models.first.contextLimit),
        '500k 窗口',
      );
      expect(
        formatContextWindow(config.providers.single.models.last.contextLimit),
        '窗口未知',
      );
    });
  });

  group('formatContextWindow', () {
    test('formats known grok limits', () {
      expect(formatContextWindow(500000), '500k 窗口');
      expect(formatContextWindow(256000), '256k 窗口');
      expect(formatContextWindow(null), '窗口未知');
      expect(formatContextWindow(0), '窗口未知');
    });
  });

  group('matchDeviceAIModel', () {
    test('matches qualified grok Kun refs to the local model id', () {
      final models = [
        const DeviceAIModelInfo(id: 'Kun', name: 'Kun', ownedBy: '订阅grok'),
        const DeviceAIModelInfo(
          id: 'deepseek-v4.1-flash',
          name: 'deepseek-v4.1-flash',
          ownedBy: '订阅grok',
        ),
      ];

      expect(matchDeviceAIModel(models, 'Kun')?.id, 'Kun');
      expect(matchDeviceAIModel(models, '订阅grok/Kun')?.id, 'Kun');
      expect(
        matchDeviceAIModel(models, 'deepseek-v4.1-flash')?.id,
        'deepseek-v4.1-flash',
      );
      expect(matchDeviceAIModel(models, ''), isNull);
    });
  });

  group('defaultModelLabel', () {
    test('appends inferred Kun window for grok providers', () {
      final provider = DeviceAIProviderInfo.fromJson({
        'id': '订阅grok',
        'models': [
          {'id': 'Kun', 'name': 'Kun'},
          {'id': 'deepseek-v4.1-flash', 'name': 'deepseek-v4.1-flash'},
        ],
      });

      expect(
        defaultModelLabel(models: provider.models, currentModel: 'Kun'),
        'Kun · 500k 窗口',
      );
      expect(
        defaultModelLabel(models: provider.models, currentModel: '订阅grok/Kun'),
        'Kun · 500k 窗口',
      );
      expect(
        defaultModelLabel(
          models: provider.models,
          currentModel: 'deepseek-v4.1-flash',
        ),
        'deepseek-v4.1-flash',
      );
      expect(
        defaultModelLabel(models: provider.models, currentModel: ''),
        '未设置',
      );
    });
  });
}
