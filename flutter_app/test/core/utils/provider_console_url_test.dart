import 'package:chat_codex_app/core/utils/provider_console_url.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('deriveProviderConsoleUrl', () {
    test('keeps only scheme host and port', () {
      expect(
        deriveProviderConsoleUrl(
          'https://api.example.com:8443/openai/v1?token=x#models',
        ),
        'https://api.example.com:8443',
      );
    });

    test('supports common api paths', () {
      expect(
        deriveProviderConsoleUrl('https://example.com/v1'),
        'https://example.com',
      );
      expect(
        deriveProviderConsoleUrl('https://example.com/api/v1'),
        'https://example.com',
      );
    });

    test('rejects unsupported and invalid urls', () {
      expect(deriveProviderConsoleUrl('ftp://example.com/v1'), isNull);
      expect(deriveProviderConsoleUrl('example.com/v1'), isNull);
      expect(deriveProviderConsoleUrl('not a url'), isNull);
    });
  });

  test('custom console url takes priority over derived api origin', () {
    expect(
      resolveProviderConsoleUrl(
        consoleUrl: 'https://dashboard.example.com/account',
        baseUrl: 'https://api.example.com/v1',
      ),
      'https://dashboard.example.com/account',
    );
  });

  test('unsupported desktop platforms use the system browser', () {
    expect(providerConsoleUsesExternalBrowser(TargetPlatform.windows), isFalse);
    expect(providerConsoleUsesExternalBrowser(TargetPlatform.linux), isTrue);
    expect(providerConsoleUsesExternalBrowser(TargetPlatform.macOS), isFalse);
    expect(providerConsoleUsesExternalBrowser(TargetPlatform.android), isFalse);
  });
}
