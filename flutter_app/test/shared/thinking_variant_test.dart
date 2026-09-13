import 'package:chat_codex_app/shared/thinking_variant.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('uses consistent labels for known and custom thinking variants', () {
    expect(thinkingVariantLabel(null), '自动');
    expect(thinkingVariantLabel('minimal'), '最小');
    expect(thinkingVariantLabel('medium'), '中');
    expect(thinkingVariantLabel('xhigh'), '极高');
    expect(thinkingVariantLabel('turbo'), 'turbo');
  });

  test('normalizes a saved variant against the selected model', () {
    expect(normalizeThinkingVariant(['low', 'high'], 'High'), 'high');
    expect(normalizeThinkingVariant(['low', 'high'], 'medium'), isNull);
    expect(normalizeThinkingVariant(const [], 'high'), isNull);
    expect(normalizeThinkingVariant(['low'], null), isNull);
  });
}
