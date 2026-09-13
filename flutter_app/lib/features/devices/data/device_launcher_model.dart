class DeviceLauncherState {
  final String status;
  final String currentVersion;
  final String targetVersion;
  final String previousVersion;
  final String lastError;
  final int agentCount;
  final bool allowAllDirectories;
  final bool upgradeLocked;
  final String upgradeStage;
  final int upgradeProgress;
  final String upgradeMessage;
  final DateTime? upgradeStartedAt;
  final DateTime? upgradeUpdatedAt;
  final List<DeviceLauncherUpgradeLog> upgradeLogs;

  const DeviceLauncherState({
    required this.status,
    required this.currentVersion,
    required this.targetVersion,
    required this.previousVersion,
    required this.lastError,
    required this.agentCount,
    required this.allowAllDirectories,
    required this.upgradeLocked,
    required this.upgradeStage,
    required this.upgradeProgress,
    required this.upgradeMessage,
    required this.upgradeStartedAt,
    required this.upgradeUpdatedAt,
    required this.upgradeLogs,
  });

  factory DeviceLauncherState.fromJson(Map<String, dynamic> json) {
    final logs = ((json['upgrade_logs'] as List?) ?? const [])
        .whereType<Map>()
        .map(
          (item) => DeviceLauncherUpgradeLog.fromJson(
            Map<String, dynamic>.from(item),
          ),
        )
        .toList(growable: false);
    return DeviceLauncherState(
      status: json['status'] as String? ?? '',
      currentVersion: json['current_version'] as String? ?? '',
      targetVersion: json['target_version'] as String? ?? '',
      previousVersion: json['previous_version'] as String? ?? '',
      lastError: json['last_error'] as String? ?? '',
      agentCount: json['agent_count'] as int? ?? 0,
      allowAllDirectories: json['allow_all_directories'] as bool? ?? false,
      upgradeLocked: json['upgrade_locked'] as bool? ?? false,
      upgradeStage: json['upgrade_stage'] as String? ?? '',
      upgradeProgress: json['upgrade_progress'] as int? ?? 0,
      upgradeMessage: json['upgrade_message'] as String? ?? '',
      upgradeStartedAt: _parseDate(json['upgrade_started_at']),
      upgradeUpdatedAt: _parseDate(json['upgrade_updated_at']),
      upgradeLogs: logs,
    );
  }

  DeviceLauncherState copyWith({
    String? status,
    String? currentVersion,
    String? targetVersion,
    String? previousVersion,
    String? lastError,
    int? agentCount,
    bool? allowAllDirectories,
    bool? upgradeLocked,
    String? upgradeStage,
    int? upgradeProgress,
    String? upgradeMessage,
    DateTime? upgradeStartedAt,
    DateTime? upgradeUpdatedAt,
    List<DeviceLauncherUpgradeLog>? upgradeLogs,
  }) {
    return DeviceLauncherState(
      status: status ?? this.status,
      currentVersion: currentVersion ?? this.currentVersion,
      targetVersion: targetVersion ?? this.targetVersion,
      previousVersion: previousVersion ?? this.previousVersion,
      lastError: lastError ?? this.lastError,
      agentCount: agentCount ?? this.agentCount,
      allowAllDirectories: allowAllDirectories ?? this.allowAllDirectories,
      upgradeLocked: upgradeLocked ?? this.upgradeLocked,
      upgradeStage: upgradeStage ?? this.upgradeStage,
      upgradeProgress: upgradeProgress ?? this.upgradeProgress,
      upgradeMessage: upgradeMessage ?? this.upgradeMessage,
      upgradeStartedAt: upgradeStartedAt ?? this.upgradeStartedAt,
      upgradeUpdatedAt: upgradeUpdatedAt ?? this.upgradeUpdatedAt,
      upgradeLogs: upgradeLogs ?? this.upgradeLogs,
    );
  }

  bool get isUpgradeRunning {
    return upgradeLocked ||
        status.toLowerCase() == 'upgrading' ||
        upgradeStage == 'downloading' ||
        upgradeStage == 'stopping_agents' ||
        upgradeStage == 'switching_version' ||
        upgradeStage == 'starting_agents';
  }
}

class DeviceLauncherUpgradeLog {
  final DateTime? time;
  final String stage;
  final String message;

  const DeviceLauncherUpgradeLog({
    required this.time,
    required this.stage,
    required this.message,
  });

  factory DeviceLauncherUpgradeLog.fromJson(Map<String, dynamic> json) {
    return DeviceLauncherUpgradeLog(
      time: _parseDate(json['time']),
      stage: json['stage'] as String? ?? '',
      message: json['message'] as String? ?? '',
    );
  }
}

DateTime? _parseDate(Object? value) {
  if (value is! String || value.trim().isEmpty) {
    return null;
  }
  return DateTime.tryParse(value);
}

class CliVersionInfo {
  final String version;
  final String channel;
  final String changelog;

  const CliVersionInfo({
    required this.version,
    required this.channel,
    required this.changelog,
  });

  factory CliVersionInfo.fromJson(Map<String, dynamic> json) {
    return CliVersionInfo(
      version: json['version'] as String? ?? '',
      channel: json['channel'] as String? ?? '',
      changelog: json['changelog'] as String? ?? '',
    );
  }
}
