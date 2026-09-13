import 'package:chat_codex_app/features/devices/presentation/device_mcp_store_page.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('resolveMCPStoreEnvironmentInitialValue', () {
    test(
      'keeps non-sensitive template default when saved values are blank',
      () {
        final value = resolveMCPStoreEnvironmentInitialValue(
          key: 'VERIFY_BASE_URL',
          templateValue: 'https://www.xyapi.top/verfiy',
          selectedValue: '',
          existingValue: '   ',
        );

        expect(value, 'https://www.xyapi.top/verfiy');
      },
    );

    test('does not prefill sensitive placeholder token values', () {
      final value = resolveMCPStoreEnvironmentInitialValue(
        key: 'VERIFY_API_TOKEN',
        templateValue: 'vat_xxx_replace_me',
      );

      expect(value, isEmpty);
    });

    test('keeps real user value before template default', () {
      final value = resolveMCPStoreEnvironmentInitialValue(
        key: 'VERIFY_BASE_URL',
        templateValue: 'https://www.xyapi.top/verfiy',
        selectedValue: 'https://verify.example.com',
        existingValue: 'https://old.example.com',
      );

      expect(value, 'https://verify.example.com');
    });
  });
}
