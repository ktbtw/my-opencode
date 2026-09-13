import 'package:flutter/material.dart';

import '../../../../shared/thinking_variant.dart';
import '../../data/chat_model.dart';
import '../../data/subagent_models.dart';

typedef SubagentControlHandler =
    Future<void> Function(
      String action, {
      String instruction,
      int? priority,
      Map<String, dynamic>? model,
    });

const _automaticVariant = '__automatic__';

class SubagentControlBar extends StatelessWidget {
  final SubagentNode node;
  final SubagentControlHandler onControl;
  final List<ModelInfo> availableModels;

  const SubagentControlBar({
    super.key,
    required this.node,
    required this.onControl,
    this.availableModels = const [],
  });

  @override
  Widget build(BuildContext context) {
    if (!node.isControllable) return const SizedBox.shrink();
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        IconButton(
          tooltip: node.state == 'paused' ? '继续子代理' : '暂停子代理',
          icon: Icon(
            node.state == 'paused'
                ? Icons.play_arrow_rounded
                : Icons.pause_rounded,
            size: 18,
          ),
          onPressed: () =>
              onControl(node.state == 'paused' ? 'resume' : 'pause'),
        ),
        IconButton(
          tooltip: '追加指导',
          icon: const Icon(Icons.add_comment_outlined, size: 18),
          onPressed: () => _instruction(context),
        ),
        IconButton(
          tooltip: '优先级',
          icon: const Icon(Icons.low_priority_rounded, size: 18),
          onPressed: () => _priority(context),
        ),
        IconButton(
          tooltip: '后续轮次模型',
          icon: const Icon(Icons.tune_rounded, size: 18),
          onPressed: () => _model(context),
        ),
        IconButton(
          tooltip: '停止子代理',
          icon: const Icon(Icons.stop_circle_outlined, size: 18),
          color: Theme.of(context).colorScheme.error,
          onPressed: () => onControl('cancel'),
        ),
      ],
    );
  }

  Future<void> _instruction(BuildContext context) async {
    final controller = TextEditingController();
    final text = await showDialog<String>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('追加指导'),
        content: TextField(
          controller: controller,
          autofocus: true,
          minLines: 2,
          maxLines: 6,
          decoration: const InputDecoration(hintText: '输入对当前子代理的补充要求'),
        ),
        actions: [
          IconButton(
            tooltip: '发送',
            icon: const Icon(Icons.send_rounded),
            onPressed: () =>
                Navigator.of(dialogContext).pop(controller.text.trim()),
          ),
        ],
      ),
    );
    controller.dispose();
    if (text == null || text.isEmpty) return;
    await onControl('instruction', instruction: text);
  }

  Future<void> _priority(BuildContext context) async {
    var value = (node.priority ?? 50).clamp(0, 100).toDouble();
    final priority = await showDialog<int>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (context, setState) => AlertDialog(
          title: Text('优先级 ${value.round()}'),
          content: Slider(
            value: value,
            min: 0,
            max: 100,
            divisions: 20,
            label: value.round().toString(),
            onChanged: (next) => setState(() => value = next),
          ),
          actions: [
            IconButton(
              tooltip: '确认',
              icon: const Icon(Icons.check_rounded),
              onPressed: () => Navigator.of(dialogContext).pop(value.round()),
            ),
          ],
        ),
      ),
    );
    if (priority == null) return;
    await onControl('priority', priority: priority);
  }

  Future<void> _model(BuildContext context) async {
    ModelInfo? initialModel;
    final currentProvider = node.model['providerID']?.toString() ?? '';
    final currentModel = node.model['modelID']?.toString() ?? '';
    for (final candidate in availableModels) {
      if (candidate.providerID == currentProvider &&
          candidate.modelID == currentModel) {
        initialModel = candidate;
        break;
      }
    }
    initialModel ??= availableModels.isNotEmpty ? availableModels.first : null;
    var currentVariant =
        normalizeThinkingVariant(
          initialModel?.variants ?? const <String>[],
          node.model['variant']?.toString(),
        ) ??
        '';
    final selected = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('后续轮次模型'),
        content: availableModels.isEmpty
            ? const Text('当前还没有可用模型，请先完成模型同步。')
            : StatefulBuilder(
                builder: (context, setState) {
                  final variants = initialModel?.variants ?? const <String>[];
                  final selectedVariant =
                      currentVariant.isNotEmpty &&
                          variants.contains(currentVariant)
                      ? currentVariant
                      : _automaticVariant;
                  return Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      DropdownButtonFormField<ModelInfo>(
                        value: initialModel,
                        isExpanded: true,
                        decoration: const InputDecoration(labelText: '模型'),
                        items: availableModels
                            .map(
                              (model) => DropdownMenuItem<ModelInfo>(
                                value: model,
                                child: Text(
                                  model.name.isNotEmpty
                                      ? model.name
                                      : model.metaKey,
                                  overflow: TextOverflow.ellipsis,
                                ),
                              ),
                            )
                            .toList(),
                        onChanged: (model) {
                          setState(() {
                            initialModel = model;
                            if (!(model?.variants.contains(currentVariant) ??
                                false)) {
                              currentVariant = '';
                            }
                          });
                        },
                      ),
                      if (variants.isNotEmpty) ...[
                        const SizedBox(height: 10),
                        DropdownButtonFormField<String>(
                          value: selectedVariant,
                          isExpanded: true,
                          decoration: const InputDecoration(
                            labelText: '思考强度（可选）',
                          ),
                          items:
                              variants
                                  .map(
                                    (variant) => DropdownMenuItem<String>(
                                      value: variant,
                                      child: Text(
                                        thinkingVariantLabel(variant),
                                      ),
                                    ),
                                  )
                                  .toList()
                                ..insert(
                                  0,
                                  const DropdownMenuItem<String>(
                                    value: _automaticVariant,
                                    child: Text('自动'),
                                  ),
                                ),
                          onChanged: (value) {
                            setState(() {
                              currentVariant = value == _automaticVariant
                                  ? ''
                                  : value ?? '';
                            });
                          },
                        ),
                      ],
                    ],
                  );
                },
              ),
        actions: [
          IconButton(
            tooltip: '确认',
            icon: const Icon(Icons.check_rounded),
            onPressed: initialModel == null
                ? null
                : () => Navigator.of(dialogContext).pop({
                    'providerID': initialModel!.providerID,
                    'modelID': initialModel!.modelID,
                    if (currentVariant.isNotEmpty) 'variant': currentVariant,
                  }),
          ),
        ],
      ),
    );
    if (selected == null ||
        (selected['providerID']?.toString().isEmpty ?? true) ||
        (selected['modelID']?.toString().isEmpty ?? true)) {
      return;
    }
    await onControl('model', model: selected);
  }
}
