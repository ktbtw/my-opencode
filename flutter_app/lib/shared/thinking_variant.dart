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

/// 思考强度的标准强弱顺序（从弱到强）。用于展示排序，越强越靠前。
const thinkingVariantStrength = <String>[
  'none',
  'off',
  'disabled',
  'minimal',
  'low',
  'medium',
  'mid',
  'high',
  'xhigh',
  'max',
  'maximum',
  'enabled',
  'on',
];

/// 取档位的强度序号（越大越强）；未知档位返回 -1。
int thinkingVariantStrengthOf(String? value) {
  final normalized = value?.trim().toLowerCase() ?? '';
  if (normalized.isEmpty) return -1;
  return thinkingVariantStrength.indexOf(normalized);
}

/// 按"越强越靠前"排序思考档位；未知档位排在最后并保持原有相对顺序。
List<String> sortThinkingVariants(Iterable<String> values) {
  final list = values.toList();
  final positions = <String, int>{
    for (var index = 0; index < list.length; index++) list[index]: index,
  };
  list.sort((a, b) {
    final ra = thinkingVariantStrengthOf(a);
    final rb = thinkingVariantStrengthOf(b);
    if (ra >= 0 && rb >= 0) return rb.compareTo(ra); // 强 → 弱
    if (ra >= 0) return -1; // 已知档位在前
    if (rb >= 0) return 1;
    return (positions[a] ?? 0).compareTo(positions[b] ?? 0); // 未知保持原序
  });
  return list;
}
