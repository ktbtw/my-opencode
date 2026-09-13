class DeviceAIModelInfo {
  final String id;
  final String name;
  final String ownedBy;
  final int? contextLimit;
  final Map<String, dynamic> variants;
  final DeviceAIThinkingInfo? thinking;
  final List<String> inputModalities;
  final List<String> outputModalities;

  const DeviceAIModelInfo({
    required this.id,
    required this.name,
    required this.ownedBy,
    this.contextLimit,
    this.variants = const {},
    this.thinking,
    this.inputModalities = const [],
    this.outputModalities = const [],
  });

  factory DeviceAIModelInfo.fromJson(Map<String, dynamic> json) {
    final modalities = json['modalities'] as Map<String, dynamic>? ?? {};
    final variants = Map<String, dynamic>.from(
      json['variants'] as Map<String, dynamic>? ?? const {},
    );
    final thinkingJson = json['thinking'];
    return DeviceAIModelInfo(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? json['id'] as String? ?? '',
      ownedBy: json['owned_by'] as String? ?? '',
      contextLimit: _readContextLimit(json),
      variants: variants,
      thinking: thinkingJson is Map
          ? DeviceAIThinkingInfo.fromJson(
              Map<String, dynamic>.from(thinkingJson),
            )
          : variants.isEmpty
          ? null
          : DeviceAIThinkingInfo(
              supported: true,
              source: 'manual',
              control: 'effort',
              protocol: 'custom',
              overrideEnabled: true,
              variants: variants,
              overrideVariants: variants,
            ),
      inputModalities: _stringList(modalities['input']),
      outputModalities: _stringList(modalities['output']),
    );
  }

  Map<String, dynamic> toJson() {
    final data = <String, dynamic>{'id': id, 'name': name.isEmpty ? id : name};
    if (ownedBy.isNotEmpty) {
      data['owned_by'] = ownedBy;
    }
    final limit = contextLimit;
    if (limit != null && limit > 0) {
      data['context_limit'] = limit;
    }
    if (variants.isNotEmpty) {
      data['variants'] = variants;
    }
    if (thinking != null) {
      data['thinking'] = thinking!.toJson();
    }
    if (inputModalities.isNotEmpty || outputModalities.isNotEmpty) {
      data['modalities'] = <String, dynamic>{
        if (inputModalities.isNotEmpty) 'input': inputModalities,
        if (outputModalities.isNotEmpty) 'output': outputModalities,
      };
    }
    return data;
  }

  DeviceAIModelInfo copyWith({
    String? id,
    String? name,
    String? ownedBy,
    int? contextLimit,
    Map<String, dynamic>? variants,
    DeviceAIThinkingInfo? thinking,
    List<String>? inputModalities,
    List<String>? outputModalities,
  }) {
    return DeviceAIModelInfo(
      id: id ?? this.id,
      name: name ?? this.name,
      ownedBy: ownedBy ?? this.ownedBy,
      contextLimit: contextLimit ?? this.contextLimit,
      variants: variants ?? this.variants,
      thinking: thinking ?? this.thinking,
      inputModalities: inputModalities ?? this.inputModalities,
      outputModalities: outputModalities ?? this.outputModalities,
    );
  }
}

class DeviceAIThinkingInfo {
  final bool supported;
  final String source;
  final String control;
  final String protocol;
  final List<String> supportedParameters;
  final bool overrideEnabled;
  final Map<String, dynamic> variants;
  final Map<String, dynamic> overrideVariants;

  const DeviceAIThinkingInfo({
    required this.supported,
    this.source = '',
    this.control = '',
    this.protocol = '',
    this.supportedParameters = const [],
    this.overrideEnabled = false,
    this.variants = const {},
    this.overrideVariants = const {},
  });

  factory DeviceAIThinkingInfo.fromJson(Map<String, dynamic> json) {
    return DeviceAIThinkingInfo(
      supported: json['supported'] as bool? ?? false,
      source: json['source'] as String? ?? '',
      control: json['control'] as String? ?? '',
      protocol: json['protocol'] as String? ?? '',
      supportedParameters: _stringList(json['supported_parameters']),
      overrideEnabled: json['override_enabled'] as bool? ?? false,
      variants: _dynamicMap(json['variants']),
      overrideVariants: _dynamicMap(json['override_variants']),
    );
  }

  Map<String, dynamic> toJson() {
    return <String, dynamic>{
      'supported': supported,
      if (source.isNotEmpty) 'source': source,
      if (control.isNotEmpty) 'control': control,
      if (protocol.isNotEmpty) 'protocol': protocol,
      if (supportedParameters.isNotEmpty)
        'supported_parameters': supportedParameters,
      'override_enabled': overrideEnabled,
      if (variants.isNotEmpty) 'variants': variants,
      if (overrideVariants.isNotEmpty) 'override_variants': overrideVariants,
    };
  }

  DeviceAIThinkingInfo copyWith({
    bool? supported,
    String? source,
    String? control,
    String? protocol,
    List<String>? supportedParameters,
    bool? overrideEnabled,
    Map<String, dynamic>? variants,
    Map<String, dynamic>? overrideVariants,
  }) {
    return DeviceAIThinkingInfo(
      supported: supported ?? this.supported,
      source: source ?? this.source,
      control: control ?? this.control,
      protocol: protocol ?? this.protocol,
      supportedParameters: supportedParameters ?? this.supportedParameters,
      overrideEnabled: overrideEnabled ?? this.overrideEnabled,
      variants: variants ?? this.variants,
      overrideVariants: overrideVariants ?? this.overrideVariants,
    );
  }
}

class DeviceAIProviderInfo {
  final String id;
  final String baseUrl;
  final String consoleUrl;
  final String apiKeyMasked;
  final String apiMode;
  final List<DeviceAIModelInfo> models;

  const DeviceAIProviderInfo({
    required this.id,
    this.baseUrl = '',
    this.consoleUrl = '',
    this.apiKeyMasked = '',
    this.apiMode = 'responses',
    this.models = const [],
  });

  factory DeviceAIProviderInfo.fromJson(Map<String, dynamic> json) {
    return DeviceAIProviderInfo(
      id: json['id'] as String? ?? '',
      baseUrl: json['base_url'] as String? ?? '',
      consoleUrl: json['console_url'] as String? ?? '',
      apiKeyMasked: json['api_key_masked'] as String? ?? '',
      apiMode: _normalizeApiMode(json['api_mode'] as String?),
      models: (json['models'] as List<dynamic>? ?? [])
          .whereType<Map<String, dynamic>>()
          .map(DeviceAIModelInfo.fromJson)
          .toList(),
    );
  }
}

class DeviceAIConfigInfo {
  final bool exists;
  final String configPath;
  final String provider;
  final String baseUrl;
  final String consoleUrl;
  final String apiKeyMasked;
  final String apiMode;
  final String model;
  final String rawJson;
  final String previewJson;
  final List<String> changedKeys;
  final String warning;
  final List<DeviceAIModelInfo> models;
  final List<DeviceAIProviderInfo> providers;

  const DeviceAIConfigInfo({
    required this.exists,
    required this.configPath,
    required this.provider,
    required this.baseUrl,
    this.consoleUrl = '',
    required this.apiKeyMasked,
    this.apiMode = 'responses',
    required this.model,
    required this.rawJson,
    required this.previewJson,
    this.changedKeys = const [],
    this.warning = '',
    this.models = const [],
    this.providers = const [],
  });

  factory DeviceAIConfigInfo.fromJson(Map<String, dynamic> json) {
    return DeviceAIConfigInfo(
      exists: json['exists'] as bool? ?? false,
      configPath: json['config_path'] as String? ?? '',
      provider: json['provider'] as String? ?? 'openai-compatible',
      baseUrl: json['base_url'] as String? ?? '',
      consoleUrl: json['console_url'] as String? ?? '',
      apiKeyMasked: json['api_key_masked'] as String? ?? '',
      apiMode: _normalizeApiMode(json['api_mode'] as String?),
      model: json['model'] as String? ?? '',
      rawJson:
          json['raw_json'] as String? ?? json['preview_json'] as String? ?? '',
      previewJson: json['preview_json'] as String? ?? '',
      changedKeys: (json['changed_keys'] as List<dynamic>? ?? [])
          .map((e) => e.toString())
          .toList(),
      warning: json['warning'] as String? ?? '',
      models: (json['models'] as List<dynamic>? ?? [])
          .whereType<Map<String, dynamic>>()
          .map(DeviceAIModelInfo.fromJson)
          .toList(),
      providers: (json['providers'] as List<dynamic>? ?? [])
          .whereType<Map<String, dynamic>>()
          .map(DeviceAIProviderInfo.fromJson)
          .toList(),
    );
  }
}

String _normalizeApiMode(String? value) {
  return value?.trim().toLowerCase() == 'chat' ? 'chat' : 'responses';
}

int? _readContextLimit(Map<String, dynamic> json) {
  final direct = _asInt(json['context_limit']) ?? _asInt(json['context']);
  if (direct != null && direct > 0) {
    return direct;
  }
  final limit = json['limit'] as Map<String, dynamic>?;
  final nested = _asInt(limit?['context']);
  return nested != null && nested > 0 ? nested : null;
}

int? _asInt(Object? value) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  if (value is String) return int.tryParse(value);
  return null;
}

List<String> _stringList(Object? value) {
  if (value is! List) return const [];
  return value
      .map((item) => item.toString().trim())
      .where((item) => item.isNotEmpty)
      .toList();
}

Map<String, dynamic> _dynamicMap(Object? value) {
  if (value is! Map) return const {};
  return Map<String, dynamic>.from(value);
}
