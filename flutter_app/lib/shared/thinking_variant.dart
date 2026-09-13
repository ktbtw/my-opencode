String thinkingVariantLabel(String? value, {String automaticLabel = '自动'}) {
  final normalized = value?.trim().toLowerCase() ?? '';
  return switch (normalized) {
    '' || '_auto' || 'auto' => automaticLabel,
    'none' || 'off' || 'disabled' => '关闭',
    'minimal' => '最小',
    'low' => '低',
    'medium' => '中',
    'high' => '高',
    'xhigh' => '极高',
    'max' => '最大',
    'enabled' || 'on' => '开启',
    _ => value!.trim(),
  };
}

String? normalizeThinkingVariant(Iterable<String> variants, String? selected) {
  final value = selected?.trim() ?? '';
  if (value.isEmpty) return null;

  for (final variant in variants) {
    final candidate = variant.trim();
    if (candidate == value) return candidate;
  }
  final normalized = value.toLowerCase();
  for (final variant in variants) {
    final candidate = variant.trim();
    if (candidate.toLowerCase() == normalized) return candidate;
  }
  return null;
}
