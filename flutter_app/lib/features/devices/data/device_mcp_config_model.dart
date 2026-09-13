class DeviceMCPServerInfo {
  final String name;
  final String type;
  final bool enabled;
  final int timeout;
  final int connectTimeout;
  final int discoveryTimeout;
  final int toolTimeout;
  final List<String> asyncTools;
  final String url;
  final Map<String, String> headers;
  final List<String> command;
  final Map<String, String> environment;
  final String oauthMode;
  final String oauthClientId;
  final String oauthClientSecret;
  final String oauthClientSecretMasked;
  final String oauthScope;

  const DeviceMCPServerInfo({
    required this.name,
    required this.type,
    required this.enabled,
    this.timeout = 0,
    this.connectTimeout = 0,
    this.discoveryTimeout = 0,
    this.toolTimeout = 0,
    this.asyncTools = const [],
    this.url = '',
    this.headers = const {},
    this.command = const [],
    this.environment = const {},
    this.oauthMode = 'auto',
    this.oauthClientId = '',
    this.oauthClientSecret = '',
    this.oauthClientSecretMasked = '',
    this.oauthScope = '',
  });

  factory DeviceMCPServerInfo.fromJson(Map<String, dynamic> json) {
    Map<String, String> parseMap(dynamic value) {
      if (value is! Map) return const {};
      return value.map(
        (key, item) => MapEntry(key.toString(), item?.toString() ?? ''),
      );
    }

    List<String> parseList(dynamic value) {
      if (value is! List) return const [];
      return value.map((item) => item.toString()).toList();
    }

    return DeviceMCPServerInfo(
      name: json['name'] as String? ?? '',
      type: json['type'] as String? ?? 'remote',
      enabled: json['enabled'] as bool? ?? true,
      timeout: json['timeout'] as int? ?? 0,
      connectTimeout: json['connect_timeout'] as int? ?? 0,
      discoveryTimeout: json['discovery_timeout'] as int? ?? 0,
      toolTimeout: json['tool_timeout'] as int? ?? 0,
      asyncTools: parseList(json['async_tools']),
      url: json['url'] as String? ?? '',
      headers: parseMap(json['headers']),
      command: parseList(json['command']),
      environment: parseMap(json['environment']),
      oauthMode: json['oauth_mode'] as String? ?? 'auto',
      oauthClientId: json['oauth_client_id'] as String? ?? '',
      oauthClientSecret: json['oauth_client_secret'] as String? ?? '',
      oauthClientSecretMasked:
          json['oauth_client_secret_masked'] as String? ?? '',
      oauthScope: json['oauth_scope'] as String? ?? '',
    );
  }

  DeviceMCPServerInfo copyWith({
    String? name,
    String? type,
    bool? enabled,
    int? timeout,
    int? connectTimeout,
    int? discoveryTimeout,
    int? toolTimeout,
    List<String>? asyncTools,
    String? url,
    Map<String, String>? headers,
    List<String>? command,
    Map<String, String>? environment,
    String? oauthMode,
    String? oauthClientId,
    String? oauthClientSecret,
    String? oauthClientSecretMasked,
    String? oauthScope,
  }) {
    return DeviceMCPServerInfo(
      name: name ?? this.name,
      type: type ?? this.type,
      enabled: enabled ?? this.enabled,
      timeout: timeout ?? this.timeout,
      connectTimeout: connectTimeout ?? this.connectTimeout,
      discoveryTimeout: discoveryTimeout ?? this.discoveryTimeout,
      toolTimeout: toolTimeout ?? this.toolTimeout,
      asyncTools: asyncTools ?? this.asyncTools,
      url: url ?? this.url,
      headers: headers ?? this.headers,
      command: command ?? this.command,
      environment: environment ?? this.environment,
      oauthMode: oauthMode ?? this.oauthMode,
      oauthClientId: oauthClientId ?? this.oauthClientId,
      oauthClientSecret: oauthClientSecret ?? this.oauthClientSecret,
      oauthClientSecretMasked:
          oauthClientSecretMasked ?? this.oauthClientSecretMasked,
      oauthScope: oauthScope ?? this.oauthScope,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'name': name,
      'type': type,
      'enabled': enabled,
      if (timeout > 0) 'timeout': timeout,
      if (connectTimeout > 0) 'connect_timeout': connectTimeout,
      if (discoveryTimeout > 0) 'discovery_timeout': discoveryTimeout,
      if (toolTimeout > 0) 'tool_timeout': toolTimeout,
      if (asyncTools.isNotEmpty) 'async_tools': asyncTools,
      if (url.isNotEmpty) 'url': url,
      if (headers.isNotEmpty) 'headers': headers,
      if (command.isNotEmpty) 'command': command,
      if (environment.isNotEmpty) 'environment': environment,
      if (oauthMode.isNotEmpty) 'oauth_mode': oauthMode,
      if (oauthClientId.isNotEmpty) 'oauth_client_id': oauthClientId,
      if (oauthClientSecret.isNotEmpty)
        'oauth_client_secret': oauthClientSecret,
      if (oauthScope.isNotEmpty) 'oauth_scope': oauthScope,
    };
  }
}

class DeviceMCPConfigInfo {
  final bool exists;
  final String configPath;
  final String rawJson;
  final String previewJson;
  final List<String> changedKeys;
  final String warning;
  final List<DeviceMCPServerInfo> servers;

  const DeviceMCPConfigInfo({
    required this.exists,
    required this.configPath,
    required this.rawJson,
    required this.previewJson,
    this.changedKeys = const [],
    this.warning = '',
    this.servers = const [],
  });

  factory DeviceMCPConfigInfo.fromJson(Map<String, dynamic> json) {
    return DeviceMCPConfigInfo(
      exists: json['exists'] as bool? ?? false,
      configPath: json['config_path'] as String? ?? '',
      rawJson:
          json['raw_json'] as String? ?? json['preview_json'] as String? ?? '',
      previewJson: json['preview_json'] as String? ?? '',
      changedKeys: (json['changed_keys'] as List<dynamic>? ?? [])
          .map((item) => item.toString())
          .toList(),
      warning: json['warning'] as String? ?? '',
      servers: (json['servers'] as List<dynamic>? ?? [])
          .whereType<Map<String, dynamic>>()
          .map(DeviceMCPServerInfo.fromJson)
          .toList(),
    );
  }
}

class DeviceAgentMCPSelectionInfo {
  final String agentId;
  final String mode;
  final List<String> selectedServers;
  final List<DeviceMCPServerInfo> availableServers;

  const DeviceAgentMCPSelectionInfo({
    required this.agentId,
    required this.mode,
    this.selectedServers = const [],
    this.availableServers = const [],
  });

  bool get isCustom => mode.trim().toLowerCase() == 'custom';

  factory DeviceAgentMCPSelectionInfo.fromJson(Map<String, dynamic> json) {
    return DeviceAgentMCPSelectionInfo(
      agentId: json['agent_id'] as String? ?? '',
      mode: json['mode'] as String? ?? 'inherit',
      selectedServers: (json['selected_servers'] as List<dynamic>? ?? const [])
          .map((item) => item.toString())
          .toList(),
      availableServers:
          (json['available_servers'] as List<dynamic>? ?? const [])
              .whereType<Map<String, dynamic>>()
              .map(DeviceMCPServerInfo.fromJson)
              .toList(),
    );
  }
}

bool shouldLoadAgentMCPStatus({
  required bool agentMode,
  required DeviceMCPConfigInfo? config,
}) {
  if (!agentMode) return false;
  final servers = config?.servers ?? const <DeviceMCPServerInfo>[];
  return servers.any(
    (server) => server.enabled && server.name.trim().isNotEmpty,
  );
}
