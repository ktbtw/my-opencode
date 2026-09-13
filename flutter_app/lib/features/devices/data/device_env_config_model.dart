class DeviceAgentEnvInfo {
  final String agentId;
  final String name;
  final String projectDir;
  final Map<String, String> environment;

  const DeviceAgentEnvInfo({
    required this.agentId,
    this.name = '',
    this.projectDir = '',
    this.environment = const {},
  });

  factory DeviceAgentEnvInfo.fromJson(Map<String, dynamic> json) {
    return DeviceAgentEnvInfo(
      agentId: json['agent_id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      projectDir: json['project_dir'] as String? ?? '',
      environment: _parseStringMap(json['environment']),
    );
  }

  DeviceAgentEnvInfo copyWith({Map<String, String>? environment}) {
    return DeviceAgentEnvInfo(
      agentId: agentId,
      name: name,
      projectDir: projectDir,
      environment: environment ?? this.environment,
    );
  }
}

class DeviceEnvConfigInfo {
  final Map<String, String> globalEnvironment;
  final List<DeviceAgentEnvInfo> agents;
  final String revision;

  const DeviceEnvConfigInfo({
    this.globalEnvironment = const {},
    this.agents = const [],
    this.revision = '',
  });

  factory DeviceEnvConfigInfo.fromJson(Map<String, dynamic> json) {
    return DeviceEnvConfigInfo(
      globalEnvironment: _parseStringMap(json['global_environment']),
      agents: (json['agents'] as List<dynamic>? ?? [])
          .whereType<Map<String, dynamic>>()
          .map(DeviceAgentEnvInfo.fromJson)
          .toList(),
      revision: json['revision'] as String? ?? '',
    );
  }

  DeviceAgentEnvInfo? agentById(String id) {
    for (final agent in agents) {
      if (agent.agentId == id) return agent;
    }
    return null;
  }

  DeviceEnvConfigInfo copyWith({
    Map<String, String>? globalEnvironment,
    List<DeviceAgentEnvInfo>? agents,
    String? revision,
  }) {
    return DeviceEnvConfigInfo(
      globalEnvironment: globalEnvironment ?? this.globalEnvironment,
      agents: agents ?? this.agents,
      revision: revision ?? this.revision,
    );
  }
}

Map<String, String> _parseStringMap(dynamic value) {
  if (value is! Map) return const {};
  return value.map(
    (key, item) => MapEntry(key.toString(), item?.toString() ?? ''),
  );
}

String _formatEnvMap(Map<String, String> env) {
  final entries = env.entries.toList()
    ..sort((left, right) => left.key.compareTo(right.key));
  return entries.map((entry) => '${entry.key}=${entry.value}').join('\n');
}

class EnvironmentPresetModel {
  final String id;
  final String name;
  final String description;
  final String category;
  final String source;
  final bool enabled;
  final List<String> tags;
  final Map<String, String> variables;
  final int sortOrder;
  final bool builtIn;

  const EnvironmentPresetModel({
    required this.id,
    required this.name,
    this.description = '',
    this.category = '通用',
    this.source = '',
    this.enabled = true,
    this.tags = const [],
    this.variables = const {},
    this.sortOrder = 100,
    this.builtIn = false,
  });

  factory EnvironmentPresetModel.fromJson(Map<String, dynamic> json) {
    return EnvironmentPresetModel(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      description: json['description'] as String? ?? '',
      category: json['category'] as String? ?? '通用',
      source: json['source'] as String? ?? '',
      enabled: json['enabled'] as bool? ?? true,
      tags: (json['tags'] as List<dynamic>? ?? const [])
          .map((item) => item.toString())
          .where((item) => item.trim().isNotEmpty)
          .toList(),
      variables: _parseStringMap(json['variables']),
      sortOrder: json['sort_order'] as int? ?? 100,
      builtIn: json['built_in'] as bool? ?? false,
    );
  }

  String get content => _formatEnvMap(variables);
}
