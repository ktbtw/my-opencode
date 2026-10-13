import 'dart:convert';

/// AI 配置设备同步的纯逻辑实现（无 Flutter 依赖，便于单元测试）。
///
/// 设备 AI 配置保存在设备端 opencode 全局配置文件中，同步以「供应商对象」
/// 为单位进行：
/// - 比较：同名供应商按规范化 JSON 比较，完全一致视为无冲突；
/// - 合并：以目标设备配置为底，用源设备的供应商对象整体覆盖 / 新增，
///   密钥、模型清单、手填窗口、思考档位等字段原样携带；
/// - 默认模型：按需随同步写入目标设备。
///
/// JSONC 清洗算法与 launcher/internal/opencodeconfig 保持一致，
/// 保证端上解析结果与设备端一致。

/// 一次同步的差异计划。
class AiConfigSyncPlan {
  /// 源设备有、目标设备没有的供应商（将新增）。
  final List<String> addedProviders;

  /// 两边同名且配置不一致的供应商（需用户选择保留哪个）。
  final List<String> conflictProviders;

  /// 两边同名且配置完全一致的供应商（自动跳过）。
  final List<String> identicalProviders;

  /// 仅目标设备有的供应商（保持不动）。
  final List<String> targetOnlyProviders;

  /// 源设备默认模型引用（如 "provider/model"，取自配置原文）。
  final String sourceModel;

  /// 目标设备默认模型引用。
  final String targetModel;

  const AiConfigSyncPlan({
    required this.addedProviders,
    required this.conflictProviders,
    required this.identicalProviders,
    required this.targetOnlyProviders,
    required this.sourceModel,
    required this.targetModel,
  });

  /// 默认模型是否需要同步（源设备已设置且与目标设备不同）。
  bool get modelChanged => sourceModel.isNotEmpty && sourceModel != targetModel;

  /// 是否存在需要执行的变化。
  bool get hasWork =>
      addedProviders.isNotEmpty || conflictProviders.isNotEmpty || modelChanged;
}

/// 单个冲突供应商的差异描述（用于弹窗展示）。
class AiConfigProviderDiff {
  final String id;

  /// 源设备一侧的摘要，如 "https://a.example.com · sk-****ab · 5 个模型"。
  final String sourceSummary;

  /// 目标设备一侧的摘要。
  final String targetSummary;

  /// 存在差异的字段名，至少一项。
  final List<String> changedFields;

  const AiConfigProviderDiff({
    required this.id,
    required this.sourceSummary,
    required this.targetSummary,
    required this.changedFields,
  });
}

/// 一次同步的预览结果：计划 + 冲突详情 + 两端配置原文。
///
/// 应用阶段直接使用同一份原文，保证「看到什么就同步什么」。
class AiConfigSyncPreview {
  final String sourceRaw;
  final String targetRaw;
  final AiConfigSyncPlan plan;

  /// 冲突供应商 id → 差异描述。
  final Map<String, AiConfigProviderDiff> providerDiffs;

  const AiConfigSyncPreview({
    required this.sourceRaw,
    required this.targetRaw,
    required this.plan,
    required this.providerDiffs,
  });
}

/// 用户在同步确认弹窗里的选择结果。
class AiConfigSyncSelection {
  /// 冲突供应商 id → 是否选择「使用源设备的配置」。
  final Map<String, bool> useSource;

  /// 是否同步默认模型。
  final bool syncDefaultModel;

  const AiConfigSyncSelection({
    required this.useSource,
    required this.syncDefaultModel,
  });

  /// 本次实际要应用到目标设备的供应商集合（新增 + 冲突中选择源设备的）。
  Set<String> resolveApplyProviderIds(AiConfigSyncPlan plan) {
    return <String>{
      ...plan.addedProviders,
      for (final id in plan.conflictProviders)
        if (useSource[id] ?? false) id,
    };
  }
}

/// 同步应用结果（用于成功提示）。
class AiConfigSyncApplyResult {
  final int addedCount;
  final int updatedCount;
  final int keptCount;
  final bool modelSynced;

  const AiConfigSyncApplyResult({
    required this.addedCount,
    required this.updatedCount,
    required this.keptCount,
    required this.modelSynced,
  });

  /// 是否没有任何实际变化。
  bool get isEmpty => addedCount == 0 && updatedCount == 0 && !modelSynced;
}

/// 解析设备配置原文（JSONC），返回顶层对象。
Map<String, dynamic> parseDeviceConfigRaw(String raw) {
  if (raw.trim().isEmpty) {
    throw const FormatException('设备返回的配置内容为空');
  }
  final decoded = jsonDecode(stripJsonc(raw));
  if (decoded is! Map<String, dynamic>) {
    throw const FormatException('设备配置顶层必须是 JSON 对象');
  }
  return decoded;
}

/// JSONC 清洗：去掉注释与尾逗号，得到标准 JSON 文本。
String stripJsonc(String raw) => stripTrailingCommas(stripJsonComments(raw));

/// 去掉 JSONC 注释（`//` 行注释与 `/* */` 块注释），字符串内部不处理。
String stripJsonComments(String raw) {
  final out = StringBuffer();
  var inString = false;
  var escaped = false;
  for (var i = 0; i < raw.length; i++) {
    final ch = raw[i];
    if (inString) {
      out.write(ch);
      if (escaped) {
        escaped = false;
        continue;
      }
      if (ch == '\\') {
        escaped = true;
        continue;
      }
      if (ch == '"') {
        inString = false;
      }
      continue;
    }
    if (ch == '"') {
      inString = true;
      out.write(ch);
      continue;
    }
    if (ch == '/' && i + 1 < raw.length) {
      final next = raw[i + 1];
      if (next == '/') {
        i += 2;
        for (; i < raw.length; i++) {
          if (raw[i] == '\n') {
            out.write('\n');
            break;
          }
        }
        continue;
      }
      if (next == '*') {
        i += 2;
        for (; i + 1 < raw.length; i++) {
          if (raw[i] == '*' && raw[i + 1] == '/') {
            i++;
            break;
          }
        }
        continue;
      }
    }
    out.write(ch);
  }
  return out.toString();
}

/// 去掉 `}` 或 `]` 之前多余的逗号，字符串内部不处理。
String stripTrailingCommas(String raw) {
  final out = StringBuffer();
  var inString = false;
  var escaped = false;
  for (var i = 0; i < raw.length; i++) {
    final ch = raw[i];
    if (inString) {
      out.write(ch);
      if (escaped) {
        escaped = false;
        continue;
      }
      if (ch == '\\') {
        escaped = true;
        continue;
      }
      if (ch == '"') {
        inString = false;
      }
      continue;
    }
    if (ch == '"') {
      inString = true;
      out.write(ch);
      continue;
    }
    if (ch == ',') {
      var j = i + 1;
      for (; j < raw.length; j++) {
        final next = raw[j];
        if (next == ' ' || next == '\n' || next == '\r' || next == '\t') {
          continue;
        }
        break;
      }
      if (j < raw.length && (raw[j] == '}' || raw[j] == ']')) {
        continue;
      }
    }
    out.write(ch);
  }
  return out.toString();
}

/// 规范化 JSON：对象键递归排序后序列化，用于忽略键顺序的等价比较。
String canonicalJson(Object? value) => jsonEncode(_canonicalize(value));

Object? _canonicalize(Object? value) {
  if (value is Map) {
    final keys = value.keys.map((key) => key.toString()).toList()..sort();
    return <String, dynamic>{
      for (final key in keys) key: _canonicalize(value[key]),
    };
  }
  if (value is List) {
    return <Object?>[for (final item in value) _canonicalize(item)];
  }
  return value;
}

/// 计算一次同步的预览：分类供应商差异 + 默认模型差异。
AiConfigSyncPreview buildAiConfigSyncPreview({
  required String sourceRaw,
  required String targetRaw,
}) {
  final source = parseDeviceConfigRaw(sourceRaw);
  final target = parseDeviceConfigRaw(targetRaw);
  final sourceProviders = _providerMap(source);
  final targetProviders = _providerMap(target);

  final added = <String>[];
  final conflicts = <String>[];
  final identical = <String>[];
  for (final id in sourceProviders.keys) {
    if (!targetProviders.containsKey(id)) {
      added.add(id);
      continue;
    }
    if (canonicalJson(sourceProviders[id]) ==
        canonicalJson(targetProviders[id])) {
      identical.add(id);
    } else {
      conflicts.add(id);
    }
  }
  final targetOnly = <String>[
    for (final id in targetProviders.keys)
      if (!sourceProviders.containsKey(id)) id,
  ];
  added.sort();
  conflicts.sort();
  identical.sort();
  targetOnly.sort();

  return AiConfigSyncPreview(
    sourceRaw: sourceRaw,
    targetRaw: targetRaw,
    plan: AiConfigSyncPlan(
      addedProviders: added,
      conflictProviders: conflicts,
      identicalProviders: identical,
      targetOnlyProviders: targetOnly,
      sourceModel: _modelRef(source),
      targetModel: _modelRef(target),
    ),
    providerDiffs: <String, AiConfigProviderDiff>{
      for (final id in conflicts)
        id: _buildProviderDiff(
          id: id,
          source: _objectOf(sourceProviders[id]),
          target: _objectOf(targetProviders[id]),
        ),
    },
  );
}

/// 构建写入目标设备的合并配置文本。
///
/// - 以目标设备配置为底，保留其全部字段与未涉及供应商；
/// - [applyProviderIds] 中的供应商用源设备对象整体覆盖 / 新增；
/// - [syncDefaultModel] 为 true 时把源设备的默认模型写入目标设备。
String buildSyncedConfigText({
  required String targetRaw,
  required String sourceRaw,
  required Set<String> applyProviderIds,
  required bool syncDefaultModel,
}) {
  final target = parseDeviceConfigRaw(targetRaw);
  final source = parseDeviceConfigRaw(sourceRaw);
  final sourceProviders = _providerMap(source);
  final mergedProviders = <String, dynamic>{
    for (final entry in _providerMap(target).entries) entry.key: entry.value,
  };
  for (final id in applyProviderIds) {
    if (!sourceProviders.containsKey(id)) {
      throw ArgumentError.value(id, 'applyProviderIds', '源设备配置中不存在该供应商');
    }
    mergedProviders[id] = sourceProviders[id];
  }

  final merged = Map<String, dynamic>.from(target);
  if (mergedProviders.isEmpty) {
    merged.remove('provider');
  } else {
    merged['provider'] = mergedProviders;
  }

  if (syncDefaultModel) {
    final model = _modelRef(source);
    if (model.isEmpty) {
      throw ArgumentError('源设备未设置默认模型，无法同步默认模型');
    }
    merged['model'] = model;
  }

  return '${const JsonEncoder.withIndent('  ').convert(merged)}\n';
}

/// 读取顶层 provider 映射；字段缺失视为空，类型不对则直接报错。
Map<String, dynamic> _providerMap(Map<String, dynamic> config) {
  final providers = config['provider'];
  if (providers == null) return const <String, dynamic>{};
  if (providers is Map<String, dynamic>) return providers;
  throw const FormatException('设备配置的 provider 字段必须是 JSON 对象');
}

Map<String, dynamic> _objectOf(Object? value) {
  if (value is Map<String, dynamic>) return value;
  return const <String, dynamic>{};
}

String _modelRef(Map<String, dynamic> config) {
  final value = config['model'];
  return value is String ? value.trim() : '';
}

AiConfigProviderDiff _buildProviderDiff({
  required String id,
  required Map<String, dynamic> source,
  required Map<String, dynamic> target,
}) {
  final changed = <String>[];
  if (_providerBaseUrl(source) != _providerBaseUrl(target)) {
    changed.add('接口地址');
  }
  if (_maskApiKey(_apiKey(source)) != _maskApiKey(_apiKey(target))) {
    changed.add('API Key');
  }
  if (_effectiveApiMode(source) != _effectiveApiMode(target)) {
    changed.add('API 模式');
  }
  if (_consoleUrl(source) != _consoleUrl(target)) {
    changed.add('控制台地址');
  }
  final sourceModels = _modelIds(source);
  final targetModels = _modelIds(target);
  if (!_sameStringList(sourceModels, targetModels)) {
    changed.add('模型清单');
  } else if (canonicalJson(_objectOf(source['models'])) !=
      canonicalJson(_objectOf(target['models']))) {
    changed.add('模型配置');
  }
  if (changed.isEmpty) {
    // 规范化比较认定不一致，但已知字段都相同：差异在其余字段上。
    changed.add('其他字段');
  }
  return AiConfigProviderDiff(
    id: id,
    sourceSummary: _providerSummary(source),
    targetSummary: _providerSummary(target),
    changedFields: changed,
  );
}

String _providerSummary(Map<String, dynamic> provider) {
  final parts = <String>[];
  final baseUrl = _providerBaseUrl(provider);
  if (baseUrl.isNotEmpty) parts.add(baseUrl);
  final masked = _maskApiKey(_apiKey(provider));
  if (masked.isNotEmpty) parts.add(masked);
  final modelCount = _modelIds(provider).length;
  if (modelCount > 0) parts.add('$modelCount 个模型');
  return parts.isEmpty ? '—' : parts.join(' · ');
}

/// 接口地址读取优先级与 launcher 端 providerBaseURL 一致。
String _providerBaseUrl(Map<String, dynamic> provider) {
  final options = _objectOf(provider['options']);
  for (final key in const ['baseURL', 'baseUrl', 'base_url']) {
    final value = options[key];
    if (value is String && value.trim().isNotEmpty) return value.trim();
  }
  final api = provider['api'];
  if (api is String && api.trim().isNotEmpty) return api.trim();
  final baseUrl = provider['base_url'];
  if (baseUrl is String && baseUrl.trim().isNotEmpty) return baseUrl.trim();
  return '';
}

String _consoleUrl(Map<String, dynamic> provider) {
  final value = provider['x-operit-console-url'];
  return value is String ? value.trim() : '';
}

String _apiKey(Map<String, dynamic> provider) {
  final value = _objectOf(provider['options'])['apiKey'];
  return value is String ? value.trim() : '';
}

/// 脱敏规则与 launcher 端 mask 一致：长度不足 8 位显示 ****，否则保留首尾各 4 位。
String _maskApiKey(String value) {
  final trimmed = value.trim();
  if (trimmed.isEmpty) return '';
  if (trimmed.length <= 8) return '****';
  return '${trimmed.substring(0, 4)}****${trimmed.substring(trimmed.length - 4)}';
}

final RegExp _gptApiModePattern = RegExp(
  r'(^|/)(chatgpt|gpt)([._-]|\d|$)',
  caseSensitive: false,
);

/// 生效 API 模式，计算规则与 launcher 端 effectiveAPIMode 一致。
String _effectiveApiMode(Map<String, dynamic> provider) {
  final raw = _objectOf(provider['options'])['apiMode'];
  final value = raw is String ? raw.trim().toLowerCase() : '';
  if (value == 'responses') return 'responses';
  if (value == 'chat') return 'chat';
  final ids = _modelIds(provider);
  if (ids.any((id) => _gptApiModePattern.hasMatch(id.trim()))) {
    return 'responses';
  }
  if (ids.isEmpty) return 'responses';
  return 'chat';
}

List<String> _modelIds(Map<String, dynamic> provider) {
  final models = _objectOf(provider['models']);
  return models.keys.map((key) => key.toString()).toList()..sort();
}

bool _sameStringList(List<String> a, List<String> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}
