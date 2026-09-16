import '../../../shared/grok_context_limit.dart';

export '../../../shared/grok_context_limit.dart'
    show formatContextWindow, inferGrokContextLimit;

class DeviceAIModelInfo {
  final String id;
  final String name;
  final String ownedBy;

  /// 供应商接口返回的窗口值，只读，优先级最高。
  final int? upstreamContextLimit;

  /// 供应商接口返回的输出上限，只读，优先级最高。
  final int? upstreamOutputLimit;

  /// 用户在界面上手填的窗口值。
  final int? manualContextLimit;

  /// 用户在界面上手填的输出上限。
  final int? manualOutputLimit;

  /// 按模型名推断出来的窗口值，优先级最低。
  final int? inferredContextLimit;

  final Map<String, dynamic> variants;
  final DeviceAIThinkingInfo? thinking;
  final List<String> inputModalities;
  final List<String> outputModalities;

  const DeviceAIModelInfo({
    required this.id,
    required this.name,
    required this.ownedBy,
    this.upstreamContextLimit,
    this.upstreamOutputLimit,
    this.manualContextLimit,
    this.manualOutputLimit,
    this.inferredContextLimit,
    this.variants = const {},
    this.thinking,
    this.inputModalities = const [],
    this.outputModalities = const [],
  });

  /// 生效的窗口值：供应商返回 > 手填 > 预设推断。
  int? get contextLimit =>
      _firstPositive([upstreamContextLimit, manualContextLimit, inferredContextLimit]);

  /// 生效的输出上限：供应商返回 > 手填，留空交由 runtime 处理。
  int? get outputLimit => _firstPositive([upstreamOutputLimit, manualOutputLimit]);

  factory DeviceAIModelInfo.fromJson(Map<String, dynamic> json) {
    final modalities = json['modalities'] as Map<String, dynamic>? ?? {};
    final variants = Map<String, dynamic>.from(
      json['variants'] as Map<String, dynamic>? ?? const {},
    );
    final thinkingJson = json['thinking'];
    final id = json['id'] as String? ?? '';
    final name = json['name'] as String? ?? id;
    final ownedBy = json['owned_by'] as String? ?? '';
    final limit = json['limit'] as Map<String, dynamic>?;
    // 供应商返回值优先；没有来源标记时把 limit 当作供应商值。
    final upstreamContext =
        _positiveInt(json['upstream_context_limit']) ??
        _positiveInt(json['context_limit']) ??
        _positiveInt(limit?['context']);
    final upstreamOutput =
        _positiveInt(json['upstream_output_limit']) ??
        _positiveInt(json['output_limit']) ??
        _positiveInt(limit?['output']);
    final manualContext = _positiveInt(json['manual_context_limit']);
    final manualOutput = _positiveInt(json['manual_output_limit']);
    return DeviceAIModelInfo(
      id: id,
      name: name,
      ownedBy: ownedBy,
      upstreamContextLimit: upstreamContext,
      upstreamOutputLimit: upstreamOutput,
      manualContextLimit: manualContext,
      manualOutputLimit: manualOutput,
      inferredContextLimit: inferGrokContextLimit(
        provider: ownedBy,
        modelID: id,
        modelName: name,
      ),
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
    final upstreamContext = upstreamContextLimit;
    final upstreamOutput = upstreamOutputLimit;
    final manualContext = manualContextLimit;
    final manualOutput = manualOutputLimit;
    // 带上来源标记，刷新时才能保持 供应商返回 > 手填 > 预设 的优先级。
    if (manualContext != null && manualContext > 0) {
      data['manual_context_limit'] = manualContext;
    }
    if (manualOutput != null && manualOutput > 0) {
      data['manual_output_limit'] = manualOutput;
    }
    if (upstreamContext != null && upstreamContext > 0) {
      data['upstream_context_limit'] = upstreamContext;
    }
    if (upstreamOutput != null && upstreamOutput > 0) {
      data['upstream_output_limit'] = upstreamOutput;
    }
    final effectiveContext = contextLimit;
    if (effectiveContext != null && effectiveContext > 0) {
      final limit = <String, dynamic>{'context': effectiveContext};
      final effectiveOutput = outputLimit;
      if (effectiveOutput != null && effectiveOutput > 0) {
        limit['output'] = effectiveOutput;
      }
      data['context_limit'] = effectiveContext;
      data['limit'] = limit;
    }
    final effectiveOutput = outputLimit;
    if (effectiveOutput != null && effectiveOutput > 0) {
      data['output_limit'] = effectiveOutput;
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
    int? upstreamContextLimit,
    int? upstreamOutputLimit,
    int? manualContextLimit,
    int? manualOutputLimit,
    int? inferredContextLimit,
    bool clearManualContextLimit = false,
    bool clearManualOutputLimit = false,
    Map<String, dynamic>? variants,
    DeviceAIThinkingInfo? thinking,
    List<String>? inputModalities,
    List<String>? outputModalities,
  }) {
    return DeviceAIModelInfo(
      id: id ?? this.id,
      name: name ?? this.name,
      ownedBy: ownedBy ?? this.ownedBy,
      upstreamContextLimit: upstreamContextLimit ?? this.upstreamContextLimit,
      upstreamOutputLimit: upstreamOutputLimit ?? this.upstreamOutputLimit,
      manualContextLimit: clearManualContextLimit
          ? null
          : manualContextLimit ?? this.manualContextLimit,
      manualOutputLimit: clearManualOutputLimit
          ? null
          : manualOutputLimit ?? this.manualOutputLimit,
      inferredContextLimit: inferredContextLimit ?? this.inferredContextLimit,
      variants: variants ?? this.variants,
      thinking: thinking ?? this.thinking,
      inputModalities: inputModalities ?? this.inputModalities,
      outputModalities: outputModalities ?? this.outputModalities,
    );
  }
}

int? _firstPositive(List<int?> values) {
  for (final value in values) {
    if (value != null && value > 0) return value;
  }
  return null;
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
          .map(
            (item) => DeviceAIModelInfo.fromJson({
              ...item,
              if ((item['owned_by'] as String?)?.trim().isNotEmpty != true)
                'owned_by': json['id'] as String? ?? '',
            }),
          )
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
          .map(
            (item) => DeviceAIModelInfo.fromJson({
              ...item,
              if ((item['owned_by'] as String?)?.trim().isNotEmpty != true)
                'owned_by': json['provider'] as String? ?? '',
            }),
          )
          .toList(),
      providers: (json['providers'] as List<dynamic>? ?? [])
          .whereType<Map<String, dynamic>>()
          .map(DeviceAIProviderInfo.fromJson)
          .toList(),
    );
  }
}

String defaultModelLabel({
  required List<DeviceAIModelInfo> models,
  required String currentModel,
}) {
  if (currentModel.isEmpty) return '未设置';
  final match = matchDeviceAIModel(models, currentModel);
  final window = formatContextWindow(match?.contextLimit);
  if (window == '窗口未知') return currentModel;
  return '${match?.id ?? currentModel} · $window';
}

DeviceAIModelInfo? matchDeviceAIModel(
  List<DeviceAIModelInfo> models,
  String currentModel,
) {
  final target = currentModel.trim();
  if (target.isEmpty) return null;
  for (final item in models) {
    if (item.id == target || item.name == target) {
      return item;
    }
  }
  final alias = target.contains('/')
      ? target.substring(target.lastIndexOf('/') + 1)
      : target;
  if (alias.isEmpty || alias == target) return null;
  for (final item in models) {
    if (item.id == alias || item.name == alias) {
      return item;
    }
  }
  return null;
}

String _normalizeApiMode(String? value) {
  return value?.trim().toLowerCase() == 'chat' ? 'chat' : 'responses';
}

int? _positiveInt(Object? value) {
  final parsed = _asInt(value);
  return parsed != null && parsed > 0 ? parsed : null;
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
