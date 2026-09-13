import 'dart:convert';

import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';
import '../../../../core/theme/app_theme.dart';
import '../../../../shared/thinking_variant.dart';
import '../../../../shared/widgets/widgets.dart';
import '../../data/device_ai_config_model.dart';
import 'model_input_modalities_editor.dart';

class ModelThinkingEditor extends StatefulWidget {
  final DeviceAIModelInfo model;
  final ValueChanged<DeviceAIModelInfo> onChanged;
  final VoidCallback? onBack;

  const ModelThinkingEditor({
    super.key,
    required this.model,
    required this.onChanged,
    this.onBack,
  });

  @override
  State<ModelThinkingEditor> createState() => _ModelThinkingEditorState();
}

class _ModelThinkingEditorState extends State<ModelThinkingEditor> {
  late bool _manual;
  late bool _supported;
  late String _control;
  late String _protocol;
  late List<_ThinkingLevelDraft> _levels;
  late TextEditingController _jsonController;
  final Map<String, TextEditingController> _nameControllers = {};
  final Map<String, TextEditingController> _valueControllers = {};
  int _levelSeq = 0;
  Map<String, dynamic>? _advancedVariants;
  String _message = '';
  bool _messageError = false;

  DeviceAIThinkingInfo get _baseThinking =>
      widget.model.thinking ??
      DeviceAIThinkingInfo(
        supported: widget.model.variants.isNotEmpty,
        source: widget.model.variants.isEmpty ? '' : 'manual',
        control: 'effort',
        protocol: 'custom',
        overrideEnabled: widget.model.variants.isNotEmpty,
        variants: widget.model.variants,
        overrideVariants: widget.model.variants,
      );

  @override
  void initState() {
    super.initState();
    _jsonController = TextEditingController();
    _reset();
  }

  @override
  void didUpdateWidget(covariant ModelThinkingEditor oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.model.id != widget.model.id) {
      _reset();
    }
  }

  @override
  void dispose() {
    _jsonController.dispose();
    _disposeLevelControllers();
    super.dispose();
  }

  void _reset() {
    final thinking = _baseThinking;
    _manual = thinking.overrideEnabled;
    _supported = thinking.supported;
    _control = _validControl(thinking.control);
    _protocol = _validProtocol(thinking.protocol);
    final variants = thinking.overrideEnabled
        ? (thinking.overrideVariants.isNotEmpty
              ? thinking.overrideVariants
              : thinking.variants)
        : thinking.variants;
    _levels = _levelsFromVariants(variants);
    if (_levels.isEmpty && _supported) {
      _levels = _defaultLevels(_control);
    }
    _advancedVariants = null;
    _message = '';
    _messageError = false;
    _syncLevelControllers(forceText: true);
    _syncJson();
  }

  String _validControl(String value) {
    return const {'effort', 'budget', 'toggle'}.contains(value)
        ? value
        : 'effort';
  }

  String _validProtocol(String value) {
    return const {
          'openrouter',
          'reasoning',
          'openai-compatible',
          'anthropic',
          'google',
          'custom',
        }.contains(value)
        ? value
        : 'custom';
  }

  List<_ThinkingLevelDraft> _levelsFromVariants(Map<String, dynamic> variants) {
    return variants.entries.map((entry) {
      final options = _map(entry.value);
      var value = entry.key;
      final reasoning = _map(options['reasoning']);
      final thinking = _map(options['thinking']);
      final thinkingConfig = _map(options['thinkingConfig']);
      value =
          reasoning['effort']?.toString() ??
          options['reasoningEffort']?.toString() ??
          options['reasoning_effort']?.toString() ??
          options['effort']?.toString() ??
          thinkingConfig['thinkingLevel']?.toString() ??
          thinkingConfig['thinkingBudget']?.toString() ??
          thinking['budgetTokens']?.toString() ??
          value;
      return _levelDraft(name: entry.key, value: value);
    }).toList();
  }

  List<_ThinkingLevelDraft> _defaultLevels(String control) {
    return switch (control) {
      'budget' => [
        _levelDraft(name: 'high', value: '16000'),
        _levelDraft(name: 'max', value: '32000'),
      ],
      'toggle' => [
        _levelDraft(name: 'enabled', value: 'enabled'),
      ],
      _ => [
        _levelDraft(name: 'low', value: 'low'),
        _levelDraft(name: 'medium', value: 'medium'),
        _levelDraft(name: 'high', value: 'high'),
      ],
    };
  }

  _ThinkingLevelDraft _levelDraft({
    required String name,
    required String value,
    bool enabled = true,
  }) {
    return _ThinkingLevelDraft(
      id: 'level-${_levelSeq++}',
      name: name,
      value: value,
      enabled: enabled,
    );
  }

  bool _levelMatches(_ThinkingLevelDraft item, _ThinkingLevelDraft preset) {
    final name = item.name.trim().toLowerCase();
    final value = item.value.trim().toLowerCase();
    return name == preset.name.toLowerCase() ||
        value == preset.value.toLowerCase();
  }

  bool _presetAdded(_ThinkingLevelDraft preset) {
    return _levels.any((item) => item.enabled && _levelMatches(item, preset));
  }

  void _togglePreset(_ThinkingLevelDraft preset) {
    _structuredChanged(() {
      final indexes = [
        for (var i = 0; i < _levels.length; i++)
          if (_levelMatches(_levels[i], preset)) i,
      ];
      if (indexes.any((i) => _levels[i].enabled)) {
        for (final i in indexes.reversed) {
          _levels.removeAt(i);
        }
        return;
      }
      if (indexes.isNotEmpty) {
        _levels[indexes.first] = _levels[indexes.first].copyWith(
          enabled: true,
          name: _levels[indexes.first].name.trim().isEmpty
              ? preset.name
              : _levels[indexes.first].name,
          value: _levels[indexes.first].value.trim().isEmpty
              ? preset.value
              : _levels[indexes.first].value,
        );
        return;
      }
      _levels.add(
        _levelDraft(name: preset.name, value: preset.value),
      );
    });
  }

  void _addAllPresets() {
    _structuredChanged(() {
      for (final preset in _mainstreamPresets(_control)) {
        final index = _levels.indexWhere((item) => _levelMatches(item, preset));
        if (index >= 0) {
          _levels[index] = _levels[index].copyWith(enabled: true);
        } else {
          _levels.add(_levelDraft(name: preset.name, value: preset.value));
        }
      }
    });
  }

  String _presetChipLabel(_ThinkingLevelDraft preset) {
    final label = thinkingVariantLabel(preset.name);
    if (_control == 'budget') {
      return '$label ${preset.value}';
    }
    return label;
  }

  Map<String, dynamic> _buildVariants() {
    if (!_supported) return const {};
    if (_advancedVariants != null) return _advancedVariants!;
    final variants = <String, dynamic>{};
    for (final level in _levels.where((item) => item.enabled)) {
      final name = level.name.trim();
      final value = level.value.trim();
      if (name.isEmpty || value.isEmpty) continue;
      if (_control == 'toggle') {
        variants[name] = switch (_protocol) {
          'openrouter' || 'reasoning' => {
            'reasoning': {'enabled': true},
          },
          'anthropic' => {
            'thinking': {'type': 'adaptive'},
          },
          'google' => {
            'thinkingConfig': {'includeThoughts': true},
          },
          'openai-compatible' => {'includeReasoning': true},
          _ => {'reasoning': true},
        };
        continue;
      }
      variants[name] = switch (_protocol) {
        'openrouter' || 'reasoning' => {
          'reasoning': {'effort': value},
        },
        'openai-compatible' => {'reasoningEffort': value},
        'anthropic' when _control == 'budget' => {
          'thinking': {
            'type': 'enabled',
            'budgetTokens': int.tryParse(value) ?? 0,
          },
        },
        'anthropic' => {
          'thinking': {'type': 'adaptive'},
          'effort': value,
        },
        'google' when _control == 'budget' => {
          'thinkingConfig': {
            'includeThoughts': true,
            'thinkingBudget': int.tryParse(value) ?? 0,
          },
        },
        'google' => {
          'thinkingConfig': {'includeThoughts': true, 'thinkingLevel': value},
        },
        _ => {'reasoningEffort': value},
      };
    }
    return variants;
  }

  void _syncJson() {
    _jsonController.text = const JsonEncoder.withIndent(
      '  ',
    ).convert(_buildVariants());
  }

  void _setManual(bool value) {
    setState(() {
      _manual = value;
      _message = '';
      if (value) {
        _syncJson();
      }
    });
    _emit();
  }

  void _structuredChanged(VoidCallback change) {
    setState(() {
      change();
      _advancedVariants = null;
      _message = '';
      _syncLevelControllers();
      _syncJson();
    });
    _emit();
  }

  void _syncLevelControllers({bool forceText = false}) {
    final keep = _levels.map((item) => item.fieldKey).toSet();
    for (final id in _nameControllers.keys.where((id) => !keep.contains(id)).toList()) {
      _nameControllers.remove(id)?.dispose();
      _valueControllers.remove(id)?.dispose();
    }
    for (final level in _levels) {
      final name = _nameControllers.putIfAbsent(
        level.fieldKey,
        () => TextEditingController(text: level.name),
      );
      final value = _valueControllers.putIfAbsent(
        level.fieldKey,
        () => TextEditingController(text: level.value),
      );
      if (forceText) {
        if (name.text != level.name) name.text = level.name;
        if (value.text != level.value) value.text = level.value;
      }
    }
  }

  void _disposeLevelControllers() {
    for (final controller in _nameControllers.values) {
      controller.dispose();
    }
    for (final controller in _valueControllers.values) {
      controller.dispose();
    }
    _nameControllers.clear();
    _valueControllers.clear();
  }

  void _emit() {
    final current = _baseThinking;
    if (!_manual) {
      final variants = current.overrideEnabled
          ? const <String, dynamic>{}
          : current.variants;
      widget.onChanged(
        widget.model.copyWith(
          variants: variants,
          thinking: current.copyWith(
            source: current.overrideEnabled ? 'inferred' : current.source,
            overrideEnabled: false,
            variants: variants,
            overrideVariants: const {},
          ),
        ),
      );
      return;
    }
    final variants = _buildVariants();
    widget.onChanged(
      widget.model.copyWith(
        variants: variants,
        thinking: current.copyWith(
          supported: _supported,
          source: 'manual',
          control: _control,
          protocol: _protocol,
          overrideEnabled: true,
          variants: variants,
          overrideVariants: variants,
        ),
      ),
    );
  }

  void _inputModalitiesChanged(List<String> input) {
    final output = widget.model.outputModalities.isNotEmpty
        ? widget.model.outputModalities
        : const ['text'];
    widget.onChanged(
      widget.model.copyWith(inputModalities: input, outputModalities: output),
    );
  }

  void _applyJson() {
    try {
      final decoded = jsonDecode(_jsonController.text);
      if (decoded is! Map) {
        throw const FormatException('variants 必须是 JSON 对象');
      }
      final variants = Map<String, dynamic>.from(decoded);
      for (final entry in variants.entries) {
        if (entry.key.trim().isEmpty || entry.value is! Map) {
          throw const FormatException('每个档位都必须有名称和参数对象');
        }
      }
      setState(() {
        _advancedVariants = variants;
        _levels = _levelsFromVariants(variants);
        _syncLevelControllers(forceText: true);
        _jsonController.text = const JsonEncoder.withIndent(
          '  ',
        ).convert(variants);
        _message = '高级参数已应用';
        _messageError = false;
      });
      _emit();
    } on FormatException catch (error) {
      setState(() {
        _message = error.message;
        _messageError = true;
      });
    } catch (_) {
      setState(() {
        _message = 'JSON 格式有误，请检查括号和逗号';
        _messageError = true;
      });
    }
  }

  void _validate() {
    final variants = _buildVariants();
    var message = '配置结构校验通过';
    var isError = false;
    if (_manual && _supported && variants.isEmpty) {
      message = '至少保留一个可用档位';
      isError = true;
    }
    if (_manual && _supported && _control == 'budget') {
      final invalid = _levels.any(
        (item) => item.enabled && (int.tryParse(item.value.trim()) ?? 0) <= 0,
      );
      if (invalid) {
        message = 'Token 预算必须是正整数';
        isError = true;
      }
    }
    setState(() {
      _message = message;
      _messageError = isError;
    });
  }

  @override
  Widget build(BuildContext context) {
    final thinking = _baseThinking;
    return Column(
      children: [
        _EditorHeader(
          model: widget.model,
          thinking: thinking,
          manual: _manual,
          onBack: widget.onBack,
        ),
        Expanded(
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(20, 18, 20, 24),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                _SectionTitle(title: '输入方式', trailing: '控制可发送给模型的内容类型'),
                const SizedBox(height: 4),
                const Text(
                  '勾选后聊天中才会把对应类型的附件发送给该模型',
                  style: TextStyle(fontSize: 11, color: AppColors.textMuted),
                ),
                const SizedBox(height: 8),
                ModelInputModalitiesEditor(
                  input: widget.model.inputModalities,
                  onChanged: _inputModalitiesChanged,
                ),
                const SizedBox(height: 18),
                _SectionTitle(title: '配置来源', trailing: '手动配置始终优先'),
                const SizedBox(height: 10),
                _SourceSegment(manual: _manual, onChanged: _setManual),
                const SizedBox(height: 14),
                if (_manual)
                  _buildManual()
                else
                  _AutoThinkingSummary(
                    thinking: thinking,
                    variants: thinking.variants.isNotEmpty
                        ? thinking.variants
                        : widget.model.variants,
                  ),
              ],
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildManual() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            const Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '支持思考控制',
                    style: TextStyle(fontSize: 13, fontWeight: FontWeight.w700),
                  ),
                  SizedBox(height: 3),
                  Text(
                    '关闭后聊天界面不显示思考强度',
                    style: TextStyle(fontSize: 11, color: AppColors.textMuted),
                  ),
                ],
              ),
            ),
            Switch(
              value: _supported,
              onChanged: (value) =>
                  _structuredChanged(() => _supported = value),
            ),
          ],
        ),
        if (_supported) ...[
          const SizedBox(height: 14),
          LayoutBuilder(
            builder: (context, constraints) {
              final compact = constraints.maxWidth < 520;
              final fields = [
                AppSelect<String>(
                  label: '控制方式',
                  value: _control,
                  options: const [
                    AppSelectOption(value: 'effort', label: '思考强度'),
                    AppSelectOption(value: 'budget', label: 'Token 预算'),
                    AppSelectOption(value: 'toggle', label: '仅开关'),
                  ],
                  onChanged: (value) => _structuredChanged(() {
                    _control = value;
                    _levels = _defaultLevels(value);
                  }),
                ),
                AppSelect<String>(
                  label: '参数协议',
                  value: _protocol,
                  options: const [
                    AppSelectOption(
                      value: 'openrouter',
                      label: 'OpenRouter · reasoning.effort',
                    ),
                    AppSelectOption(
                      value: 'reasoning',
                      label: '通用嵌套 · reasoning.effort',
                    ),
                    AppSelectOption(
                      value: 'openai-compatible',
                      label: 'OpenAI 兼容 · reasoning_effort',
                    ),
                    AppSelectOption(
                      value: 'anthropic',
                      label: 'Anthropic · thinking',
                    ),
                    AppSelectOption(
                      value: 'google',
                      label: 'Google · thinkingConfig',
                    ),
                    AppSelectOption(value: 'custom', label: '自定义 variants'),
                  ],
                  onChanged: (value) =>
                      _structuredChanged(() => _protocol = value),
                ),
              ];
              if (compact) {
                return Column(
                  children: [
                    fields.first,
                    const SizedBox(height: 12),
                    fields.last,
                  ],
                );
              }
              return Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Expanded(child: fields.first),
                  const SizedBox(width: 12),
                  Expanded(child: fields.last),
                ],
              );
            },
          ),
          const SizedBox(height: 18),
          Row(
            children: [
              const Text(
                '可用档位',
                style: TextStyle(fontSize: 12, fontWeight: FontWeight.w700),
              ),
              const Spacer(),
              TextButton.icon(
                onPressed: () => _structuredChanged(
                  () => _levels.add(
                    _levelDraft(name: 'custom', value: 'custom'),
                  ),
                ),
                icon: const Icon(Icons.add_rounded, size: 15),
                label: const Text('添加档位'),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 7,
            runSpacing: 7,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              const Text(
                '主流预设',
                style: TextStyle(fontSize: 11, color: AppColors.textMuted),
              ),
              for (final preset in _mainstreamPresets(_control))
                _PresetChip(
                  key: ValueKey('thinking-preset-${preset.name}'),
                  label: _presetChipLabel(preset),
                  selected: _presetAdded(preset),
                  onTap: () => _togglePreset(preset),
                ),
              _PresetChip(
                key: const ValueKey('thinking-preset-add-all'),
                label: '全部添加',
                selected: false,
                emphasized: true,
                onTap: _addAllPresets,
              ),
            ],
          ),
          const SizedBox(height: 10),
          _LevelEditor(
            levels: _levels,
            budget: _control == 'budget',
            nameControllers: _nameControllers,
            valueControllers: _valueControllers,
            onChanged: (value) => _structuredChanged(() {
              final index = _levels.indexWhere(
                (item) => item.fieldKey == value.fieldKey,
              );
              if (index >= 0) _levels[index] = value;
            }),
            onDelete: (value) => _structuredChanged(
              () => _levels.removeWhere(
                (item) => item.fieldKey == value.fieldKey,
              ),
            ),
          ),
          const SizedBox(height: 14),
          ExpansionTile(
            tilePadding: EdgeInsets.zero,
            childrenPadding: EdgeInsets.zero,
            shape: const Border(),
            collapsedShape: const Border(),
            title: const Text(
              '高级参数',
              style: TextStyle(fontSize: 12, fontWeight: FontWeight.w700),
            ),
            subtitle: const Text(
              '仅编辑当前模型 variants',
              style: TextStyle(fontSize: 10, color: AppColors.textMuted),
            ),
            children: [
              TextField(
                key: const ValueKey('thinking-variants-json'),
                controller: _jsonController,
                minLines: 7,
                maxLines: 14,
                style: const TextStyle(
                  fontFamily: 'monospace',
                  fontSize: 11,
                  height: 1.45,
                ),
                decoration: const InputDecoration(
                  hintText: '请输入当前模型的 variants JSON',
                  alignLabelWithHint: true,
                ),
              ),
              const SizedBox(height: 8),
              Align(
                alignment: Alignment.centerRight,
                child: OutlinedButton.icon(
                  onPressed: _applyJson,
                  icon: const Icon(Icons.data_object_rounded, size: 15),
                  label: const Text('格式化并应用'),
                ),
              ),
            ],
          ),
        ],
        const SizedBox(height: 14),
        Row(
          children: [
            OutlinedButton.icon(
              onPressed: _validate,
              icon: const Icon(Icons.science_outlined, size: 16),
              label: const Text('校验配置'),
            ),
            if (_message.isNotEmpty) ...[
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  _message,
                  style: TextStyle(
                    fontSize: 11,
                    color: _messageError
                        ? AppColors.statusError
                        : AppColors.statusSuccess,
                  ),
                ),
              ),
            ],
          ],
        ),
      ],
    );
  }
}

class _EditorHeader extends StatelessWidget {
  final DeviceAIModelInfo model;
  final DeviceAIThinkingInfo thinking;
  final bool manual;
  final VoidCallback? onBack;

  const _EditorHeader({
    required this.model,
    required this.thinking,
    required this.manual,
    required this.onBack,
  });

  @override
  Widget build(BuildContext context) {
    final status = _thinkingStatus(thinking, manual: manual);
    return Container(
      constraints: const BoxConstraints(minHeight: 70),
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
      decoration: const BoxDecoration(
        border: Border(bottom: BorderSide(color: AppColors.borderLight)),
      ),
      child: Row(
        children: [
          if (onBack != null) ...[
            IconButton(
              tooltip: '返回模型列表',
              onPressed: onBack,
              icon: const Icon(Icons.arrow_back_rounded, size: 19),
            ),
            const SizedBox(width: 4),
          ] else ...[
            Container(
              width: 38,
              height: 38,
              decoration: BoxDecoration(
                color: AppColors.primaryLight,
                borderRadius: AppRadius.smRadius,
              ),
              child: const Icon(
                Icons.psychology_alt_outlined,
                size: 20,
                color: AppColors.primary,
              ),
            ),
            const SizedBox(width: 11),
          ],
          Expanded(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  model.name.isEmpty ? model.id : model.name,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 15,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 3),
                Text(
                  model.id,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 10,
                    color: AppColors.textMuted,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(width: 8),
          _ThinkingStatusPill(status: status),
        ],
      ),
    );
  }
}

class _SourceSegment extends StatelessWidget {
  final bool manual;
  final ValueChanged<bool> onChanged;

  const _SourceSegment({required this.manual, required this.onChanged});

  @override
  Widget build(BuildContext context) {
    return Container(
      height: 43,
      padding: const EdgeInsets.all(3),
      decoration: BoxDecoration(
        color: AppColors.inputBackground,
        borderRadius: AppRadius.mdRadius,
        border: Border.all(color: AppColors.border),
      ),
      child: Row(
        children: [
          Expanded(
            child: _SegmentButton(
              label: '自动识别',
              selected: !manual,
              onTap: () => onChanged(false),
            ),
          ),
          const SizedBox(width: 3),
          Expanded(
            child: _SegmentButton(
              label: '手动覆盖',
              selected: manual,
              onTap: () => onChanged(true),
            ),
          ),
        ],
      ),
    );
  }
}

class _SegmentButton extends StatelessWidget {
  final String label;
  final bool selected;
  final VoidCallback onTap;

  const _SegmentButton({
    required this.label,
    required this.selected,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Material(
      color: selected ? AppColors.surface : Colors.transparent,
      borderRadius: AppRadius.smRadius,
      child: InkWell(
        onTap: onTap,
        borderRadius: AppRadius.smRadius,
        child: Center(
          child: Text(
            label,
            style: TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w700,
              color: selected ? AppColors.primary : AppColors.textSecondary,
            ),
          ),
        ),
      ),
    );
  }
}

class _AutoThinkingSummary extends StatelessWidget {
  final DeviceAIThinkingInfo thinking;
  final Map<String, dynamic> variants;

  const _AutoThinkingSummary({required this.thinking, required this.variants});

  @override
  Widget build(BuildContext context) {
    final detected = thinking.supported || variants.isNotEmpty;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Container(
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: AppColors.inputBackground,
            borderRadius: AppRadius.mdRadius,
            border: Border.all(color: AppColors.border),
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 28,
                height: 28,
                decoration: BoxDecoration(
                  color: detected
                      ? AppColors.statusSuccessLight
                      : AppColors.statusOfflineLight,
                  borderRadius: AppRadius.smRadius,
                ),
                child: Icon(
                  detected
                      ? Icons.verified_outlined
                      : Icons.help_outline_rounded,
                  size: 16,
                  color: detected
                      ? AppColors.statusSuccess
                      : AppColors.statusOffline,
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      detected
                          ? _detectionTitle(thinking.source)
                          : '未检测到思考控制能力',
                      style: const TextStyle(
                        fontSize: 12,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      detected
                          ? '当前档位来自 ${_sourceLabel(thinking.source)}，刷新模型后会自动更新。'
                          : '接口和运行时均未返回可调档位，可使用手动覆盖。',
                      style: const TextStyle(
                        fontSize: 11,
                        height: 1.5,
                        color: AppColors.textSecondary,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
        if (thinking.supportedParameters.isNotEmpty) ...[
          const SizedBox(height: 10),
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: thinking.supportedParameters
                .map((item) => _ParameterChip(label: item))
                .toList(),
          ),
        ],
        const SizedBox(height: 12),
        LayoutBuilder(
          builder: (context, constraints) {
            final items = [
              _InfoCell(label: '控制方式', value: _controlLabel(thinking.control)),
              _InfoCell(
                label: '参数协议',
                value: _protocolLabel(thinking.protocol),
              ),
            ];
            if (constraints.maxWidth < 420) {
              return Column(
                children: [items.first, const SizedBox(height: 8), items.last],
              );
            }
            return Row(
              children: [
                Expanded(child: items.first),
                const SizedBox(width: 8),
                Expanded(child: items.last),
              ],
            );
          },
        ),
        const SizedBox(height: 14),
        const Text(
          '聊天界面将显示',
          style: TextStyle(fontSize: 11, fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 8),
        Wrap(
          spacing: 7,
          runSpacing: 7,
          children: variants.isEmpty
              ? const [_VariantChip(label: '默认')]
              : variants.keys
                    .map(
                      (item) => _VariantChip(label: thinkingVariantLabel(item)),
                    )
                    .toList(),
        ),
      ],
    );
  }
}

class _LevelEditor extends StatelessWidget {
  final List<_ThinkingLevelDraft> levels;
  final bool budget;
  final Map<String, TextEditingController> nameControllers;
  final Map<String, TextEditingController> valueControllers;
  final ValueChanged<_ThinkingLevelDraft> onChanged;
  final ValueChanged<_ThinkingLevelDraft> onDelete;

  const _LevelEditor({
    required this.levels,
    required this.budget,
    required this.nameControllers,
    required this.valueControllers,
    required this.onChanged,
    required this.onDelete,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        border: Border.all(color: AppColors.border),
        borderRadius: AppRadius.mdRadius,
      ),
      child: Column(
        children: [
          for (var index = 0; index < levels.length; index++)
            _LevelRow(
              key: ValueKey('thinking-level-${levels[index].fieldKey}'),
              level: levels[index],
              last: index == levels.length - 1,
              budget: budget,
              nameController: nameControllers[levels[index].fieldKey],
              valueController: valueControllers[levels[index].fieldKey],
              onChanged: onChanged,
              onDelete: onDelete,
            ),
        ],
      ),
    );
  }
}

class _LevelRow extends StatelessWidget {
  final _ThinkingLevelDraft level;
  final bool last;
  final bool budget;
  final TextEditingController? nameController;
  final TextEditingController? valueController;
  final ValueChanged<_ThinkingLevelDraft> onChanged;
  final ValueChanged<_ThinkingLevelDraft> onDelete;

  const _LevelRow({
    super.key,
    required this.level,
    required this.last,
    required this.budget,
    required this.nameController,
    required this.valueController,
    required this.onChanged,
    required this.onDelete,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 6),
      decoration: BoxDecoration(
        border: last
            ? null
            : const Border(bottom: BorderSide(color: AppColors.borderLight)),
      ),
      child: Row(
        children: [
          Checkbox(
            value: level.enabled,
            onChanged: (value) =>
                onChanged(level.copyWith(enabled: value ?? false)),
          ),
          Expanded(
            child: TextFormField(
              key: ValueKey('thinking-name-${level.fieldKey}'),
              controller: nameController,
              decoration: const InputDecoration(
                hintText: '档位名称',
                isDense: true,
              ),
              style: const TextStyle(fontSize: 11),
              onChanged: (value) => onChanged(level.copyWith(name: value)),
            ),
          ),
          const SizedBox(width: 7),
          Expanded(
            child: TextFormField(
              key: ValueKey('thinking-value-${level.fieldKey}'),
              controller: valueController,
              keyboardType: budget
                  ? TextInputType.number
                  : TextInputType.text,
              decoration: InputDecoration(
                hintText: budget ? 'Token 数量' : '参数值',
                isDense: true,
              ),
              style: const TextStyle(fontSize: 11),
              onChanged: (value) => onChanged(level.copyWith(value: value)),
            ),
          ),
          IconButton(
            key: ValueKey('thinking-delete-${level.fieldKey}'),
            tooltip: '删除档位',
            onPressed: () => onDelete(level),
            icon: const Icon(Icons.delete_outline_rounded, size: 17),
            color: AppColors.textMuted,
          ),
        ],
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  final String title;
  final String trailing;

  const _SectionTitle({required this.title, required this.trailing});

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Text(
          title,
          style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w700),
        ),
        const Spacer(),
        Text(
          trailing,
          style: const TextStyle(fontSize: 10, color: AppColors.textMuted),
        ),
      ],
    );
  }
}

class _InfoCell extends StatelessWidget {
  final String label;
  final String value;

  const _InfoCell({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(11),
      decoration: BoxDecoration(
        border: Border.all(color: AppColors.borderLight),
        borderRadius: AppRadius.mdRadius,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            label,
            style: const TextStyle(fontSize: 10, color: AppColors.textMuted),
          ),
          const SizedBox(height: 6),
          Text(
            value,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w700),
          ),
        ],
      ),
    );
  }
}

class _ParameterChip extends StatelessWidget {
  final String label;

  const _ParameterChip({required this.label});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 4),
      decoration: BoxDecoration(
        color: AppColors.surface,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.border),
      ),
      child: Text(
        label,
        style: const TextStyle(
          fontFamily: 'monospace',
          fontSize: 10,
          color: AppColors.textSecondary,
        ),
      ),
    );
  }
}

class _VariantChip extends StatelessWidget {
  final String label;

  const _VariantChip({required this.label});

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(minWidth: 58),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: AppColors.primaryLight,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.primaryMuted),
      ),
      child: Text(
        label,
        textAlign: TextAlign.center,
        style: const TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w700,
          color: AppColors.primary,
        ),
      ),
    );
  }
}

class _ThinkingStatusPill extends StatelessWidget {
  final _ThinkingStatus status;

  const _ThinkingStatusPill({required this.status});

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(maxWidth: 130),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
      decoration: BoxDecoration(
        color: status.background,
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: status.border),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 6,
            height: 6,
            decoration: BoxDecoration(
              color: status.color,
              shape: BoxShape.circle,
            ),
          ),
          const SizedBox(width: 5),
          Flexible(
            child: Text(
              status.label,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: 10,
                fontWeight: FontWeight.w700,
                color: status.color,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _PresetChip extends StatelessWidget {
  final String label;
  final bool selected;
  final bool emphasized;
  final VoidCallback onTap;

  const _PresetChip({
    super.key,
    required this.label,
    required this.selected,
    required this.onTap,
    this.emphasized = false,
  });

  @override
  Widget build(BuildContext context) {
    final color = emphasized
        ? AppColors.primary
        : selected
        ? AppColors.primary
        : AppColors.textSecondary;
    final background = emphasized
        ? AppColors.primaryLight
        : selected
        ? AppColors.primaryLight
        : AppColors.surface;
    final border = emphasized
        ? AppColors.primaryMuted
        : selected
        ? AppColors.primaryMuted
        : AppColors.border;
    return Material(
      color: background,
      borderRadius: AppRadius.smRadius,
      child: InkWell(
        onTap: onTap,
        borderRadius: AppRadius.smRadius,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
          decoration: BoxDecoration(
            borderRadius: AppRadius.smRadius,
            border: Border.all(color: border),
          ),
          child: Text(
            label,
            style: TextStyle(
              fontSize: 11,
              fontWeight: FontWeight.w700,
              color: color,
            ),
          ),
        ),
      ),
    );
  }
}

class _ThinkingLevelDraft {
  final String id;
  final String name;
  final String value;
  final bool enabled;

  const _ThinkingLevelDraft({
    this.id = '',
    required this.name,
    required this.value,
    this.enabled = true,
  });

  String get fieldKey => id.isNotEmpty ? id : '$name|$value';

  _ThinkingLevelDraft copyWith({String? name, String? value, bool? enabled}) {
    return _ThinkingLevelDraft(
      id: id,
      name: name ?? this.name,
      value: value ?? this.value,
      enabled: enabled ?? this.enabled,
    );
  }
}

class _ThinkingStatus {
  final String label;
  final Color color;
  final Color background;
  final Color border;

  const _ThinkingStatus({
    required this.label,
    required this.color,
    required this.background,
    required this.border,
  });
}

_ThinkingStatus _thinkingStatus(DeviceAIThinkingInfo thinking, {bool? manual}) {
  if (manual ?? thinking.overrideEnabled) {
    return const _ThinkingStatus(
      label: '手动覆盖',
      color: AppColors.statusWarning,
      background: AppColors.statusWarningLight,
      border: Color(0xFFFED7AA),
    );
  }
  if (!thinking.supported && thinking.variants.isEmpty) {
    return const _ThinkingStatus(
      label: '未检测到',
      color: AppColors.statusOffline,
      background: AppColors.statusOfflineLight,
      border: AppColors.border,
    );
  }
  if (thinking.source == 'runtime') {
    return const _ThinkingStatus(
      label: 'Runtime 已确认',
      color: AppColors.primary,
      background: AppColors.primaryLight,
      border: AppColors.primaryMuted,
    );
  }
  return const _ThinkingStatus(
    label: '接口已检测',
    color: AppColors.statusSuccess,
    background: AppColors.statusSuccessLight,
    border: Color(0xFFBBF7D0),
  );
}

String _detectionTitle(String source) {
  return switch (source) {
    'runtime' => 'OpenCode Runtime 已确认该模型支持思考控制',
    'provider' => '模型接口声明该模型支持思考控制',
    _ => 'OpenCode 已识别该模型的思考控制能力',
  };
}

String _sourceLabel(String source) {
  return switch (source) {
    'runtime' => 'OpenCode Runtime',
    'provider' => '模型接口',
    'manual' => '手动覆盖',
    _ => 'OpenCode 推断',
  };
}

String _controlLabel(String value) {
  return switch (value) {
    'budget' => 'Token 预算',
    'toggle' => '仅开关',
    'effort' => '思考强度',
    _ => '未识别',
  };
}

String _protocolLabel(String value) {
  return switch (value) {
    'openrouter' => 'OpenRouter',
    'reasoning' => '通用 reasoning.effort',
    'openai-compatible' => 'OpenAI 兼容',
    'anthropic' => 'Anthropic',
    'google' => 'Google',
    'custom' => '自定义 variants',
    _ => '未识别',
  };
}

Map<String, dynamic> _map(Object? value) {
  if (value is! Map) return const {};
  return Map<String, dynamic>.from(value);
}

List<_ThinkingLevelDraft> _mainstreamPresets(String control) {
  return switch (control) {
    'budget' => const [
      _ThinkingLevelDraft(name: 'low', value: '4096'),
      _ThinkingLevelDraft(name: 'medium', value: '8000'),
      _ThinkingLevelDraft(name: 'high', value: '16000'),
      _ThinkingLevelDraft(name: 'max', value: '32000'),
    ],
    'toggle' => const [
      _ThinkingLevelDraft(name: 'enabled', value: 'enabled'),
    ],
    _ => const [
      _ThinkingLevelDraft(name: 'none', value: 'none'),
      _ThinkingLevelDraft(name: 'minimal', value: 'minimal'),
      _ThinkingLevelDraft(name: 'low', value: 'low'),
      _ThinkingLevelDraft(name: 'medium', value: 'medium'),
      _ThinkingLevelDraft(name: 'high', value: 'high'),
      _ThinkingLevelDraft(name: 'xhigh', value: 'xhigh'),
      _ThinkingLevelDraft(name: 'max', value: 'max'),
    ],
  };
}
