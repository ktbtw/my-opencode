import 'package:flutter_test/flutter_test.dart';
import 'package:chat_codex_app/shared/grok_context_limit.dart';

void main() {
  group('inferGrokContextLimit', () {
    test('infers grok-4.6 and grok-4.5 as 500k', () {
      expect(inferGrokContextLimit(modelID: 'grok-4.6'), 500000);
      expect(inferGrokContextLimit(modelID: 'grok-4.5'), 500000);
    });

    test('infers grok-4.3 and grok-4.20 as 1M', () {
      expect(inferGrokContextLimit(modelID: 'grok-4.3'), 1000000);
      expect(inferGrokContextLimit(modelID: 'grok-4.20'), 1000000);
    });

    test('infers other grok models as 256k', () {
      expect(inferGrokContextLimit(modelID: 'grok-3'), 256000);
    });

    test('infers Kun only for grok providers', () {
      expect(
        inferGrokContextLimit(
          provider: '订阅grok',
          modelID: 'Kun',
          modelName: 'Kun',
        ),
        500000,
      );
      expect(
        inferGrokContextLimit(
          provider: 'custom',
          modelID: 'Kun',
          modelName: 'Kun',
        ),
        isNull,
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
}
