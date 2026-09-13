class DeviceAgentCompactionConfig {
  final String agentId;
  final int thresholdPercent;
  final int defaultThresholdPercent;
  final int effectiveThresholdPercent;

  const DeviceAgentCompactionConfig({
    required this.agentId,
    this.thresholdPercent = 0,
    this.defaultThresholdPercent = 80,
    this.effectiveThresholdPercent = 80,
  });

  factory DeviceAgentCompactionConfig.fromJson(Map<String, dynamic> json) {
    int value(Object? raw, int fallback) => raw is num
        ? raw.toInt()
        : int.tryParse(raw?.toString() ?? '') ?? fallback;
    return DeviceAgentCompactionConfig(
      agentId: json['agent_id']?.toString() ?? '',
      thresholdPercent: value(json['threshold_percent'], 0),
      defaultThresholdPercent: value(json['default_threshold_percent'], 80),
      effectiveThresholdPercent: value(
        json['effective_threshold_percent'],
        value(json['default_threshold_percent'], 80),
      ),
    );
  }
}
