/// 上下文窗口预设，点击即可填入，避免手写 JSON。
const contextWindowPresets = <int>[
  128000,
  200000,
  256000,
  400000,
  500000,
  1000000,
  2000000,
];

/// 最大输出预设。留空表示不手填，交由 runtime 处理。
const outputTokenPresets = <int>[4096, 8192, 16384, 32000];

String formatTokenCount(int? value) {
  if (value == null || value <= 0) return '未设置';
  if (value >= 1000000) {
    final m = value / 1000000;
    return '${m == m.roundToDouble() ? m.toStringAsFixed(0) : m.toStringAsFixed(1)}M';
  }
  if (value >= 1000) {
    final k = value / 1000;
    return '${k == k.roundToDouble() ? k.toStringAsFixed(0) : k.toStringAsFixed(1)}k';
  }
  return value.toString();
}

/// 校验手工填写的 token 数量，返回错误文案；合法时返回 null。
String? validateTokenInput(String raw, {required String label}) {
  final trimmed = raw.trim();
  if (trimmed.isEmpty) return null;
  final parsed = int.tryParse(trimmed);
  if (parsed == null || parsed <= 0) {
    return '$label必须是正整数';
  }
  if (parsed > 100000000) {
    return '$label数值过大';
  }
  return null;
}
