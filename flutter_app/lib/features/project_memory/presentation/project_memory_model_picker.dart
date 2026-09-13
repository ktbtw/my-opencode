import 'package:flutter/material.dart';

import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../chat/data/chat_model.dart';

Future<ModelInfo?> showProjectMemoryModelPicker(
  BuildContext context, {
  required List<ModelInfo> models,
  ModelInfo? selected,
}) {
  return showModalBottomSheet<ModelInfo>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    backgroundColor: AppColors.surface,
    builder: (_) =>
        _ProjectMemoryModelPicker(models: models, selected: selected),
  );
}

class _ProjectMemoryModelPicker extends StatefulWidget {
  final List<ModelInfo> models;
  final ModelInfo? selected;

  const _ProjectMemoryModelPicker({required this.models, this.selected});

  @override
  State<_ProjectMemoryModelPicker> createState() =>
      _ProjectMemoryModelPickerState();
}

class _ProjectMemoryModelPickerState extends State<_ProjectMemoryModelPicker> {
  final _searchController = TextEditingController();
  String _query = '';

  @override
  void dispose() {
    _searchController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final query = _query.trim().toLowerCase();
    final filtered = widget.models
        .where((model) {
          if (query.isEmpty) return true;
          return model.metaKey.toLowerCase().contains(query) ||
              model.name.toLowerCase().contains(query);
        })
        .toList(growable: false);
    return Padding(
      padding: EdgeInsets.only(
        left: 16,
        right: 16,
        top: 14,
        bottom: MediaQuery.viewInsetsOf(context).bottom + 12,
      ),
      child: SizedBox(
        height: MediaQuery.sizeOf(context).height * .72,
        child: Column(
          children: [
            Row(
              children: [
                const Text(
                  '选择整理模型',
                  style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
                ),
                const Spacer(),
                Text(
                  '${filtered.length}/${widget.models.length}',
                  style: const TextStyle(
                    color: AppColors.textMuted,
                    fontSize: 12,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _searchController,
              autofocus: true,
              onChanged: (value) => setState(() => _query = value),
              decoration: InputDecoration(
                hintText: '搜索模型名称、供应商或 ID',
                prefixIcon: const Icon(Icons.search),
                isDense: true,
                border: OutlineInputBorder(borderRadius: AppRadius.smRadius),
              ),
            ),
            const SizedBox(height: 8),
            Expanded(
              child: filtered.isEmpty
                  ? const Center(child: Text('没有匹配的设备模型'))
                  : ListView.separated(
                      itemCount: filtered.length,
                      separatorBuilder: (_, _) => const Divider(height: 1),
                      itemBuilder: (_, index) {
                        final model = filtered[index];
                        final isSelected =
                            model.metaKey == widget.selected?.metaKey;
                        return ListTile(
                          contentPadding: EdgeInsets.zero,
                          title: Text(
                            model.name.isEmpty ? model.metaKey : model.name,
                          ),
                          subtitle: Text(model.metaKey),
                          trailing: isSelected
                              ? const Icon(
                                  Icons.check,
                                  color: AppColors.primary,
                                )
                              : null,
                          onTap: () => Navigator.of(context).pop(model),
                        );
                      },
                    ),
            ),
          ],
        ),
      ),
    );
  }
}
