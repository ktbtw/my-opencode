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
}
