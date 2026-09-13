import 'package:flutter/material.dart';

/// 模型输入方式（modalities.input）编辑芯片组。
///
/// 勾选结果写入 opencode 配置的 `models[id].modalities.input`，
/// opencode 据此决定是否把对应类型的附件发送给模型。
class ModelInputModalitiesEditor extends StatelessWidget {
  final List<String> input;
  final ValueChanged<List<String>> onChanged;

  const ModelInputModalitiesEditor({
    super.key,
    required this.input,
    required this.onChanged,
  });

  static const List<(String, String)> _options = [
    ('image', '图片'),
    ('video', '视频'),
    ('audio', '音频'),
    ('pdf', 'PDF'),
  ];

  static const List<String> _order = ['text', 'image', 'video', 'audio', 'pdf'];

  void _toggle(String value) {
    final selected = input.toSet()..add('text');
    if (selected.contains(value)) {
      selected.remove(value);
    } else {
      selected.add(value);
    }
    onChanged([
      for (final item in _order)
        if (selected.contains(item)) item,
    ]);
  }

  @override
  Widget build(BuildContext context) {
    final selected = input.toSet();
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        const Tooltip(
          message: '对话模型必须保留文本输入',
          child: ChoiceChip(
            label: Text('文本'),
            selected: true,
            onSelected: null,
          ),
        ),
        for (final (value, label) in _options)
          ChoiceChip(
            label: Text(label),
            selected: selected.contains(value),
            onSelected: (_) => _toggle(value),
          ),
      ],
    );
  }
}
