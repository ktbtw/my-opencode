int? inferGrokContextLimit({
  String provider = '',
  String modelID = '',
  String modelName = '',
}) {
  final aliases = <String>[
    modelID,
    modelName,
    ...modelID.split('/').where((item) => item.trim().isNotEmpty),
    ...modelName.split('/').where((item) => item.trim().isNotEmpty),
  ];
  for (final alias in aliases) {
    final value = grokContextLimitForAlias(alias);
    if (value != null) return value;
  }
  if (provider.toLowerCase().contains('grok')) {
    for (final alias in aliases) {
      if (alias.trim().toLowerCase() == 'kun') return 500000;
    }
  }
  return null;
}

int? grokContextLimitForAlias(String value) {
  final alias = value.trim().toLowerCase();
  if (RegExp(r'^grok[-_.]?4[-_.]?[56](?:$|[-_.])').hasMatch(alias)) {
    return 500000;
  }
  if (RegExp(r'^grok[-_.]?4[-_.]?(?:3|20)(?:$|[-_.])').hasMatch(alias)) {
    return 1000000;
  }
  if (alias.startsWith('grok')) return 256000;
  return null;
}

String formatContextWindow(int? value) {
  if (value == null || value <= 0) return '窗口未知';
  if (value >= 1000) {
    final k = value / 1000;
    return '${k >= 10 ? k.toStringAsFixed(0) : k.toStringAsFixed(1)}k 窗口';
  }
  return '$value 窗口';
}
