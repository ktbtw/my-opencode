import 'package:flutter/material.dart';

import '../../../core/config/api_client.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import '../data/device_skill_model.dart';
import 'device_skill_detail_dialog.dart';

class DeviceSkillStorePage extends StatefulWidget {
  final List<DeviceGlobalSkillConfigItem> existingSkills;

  const DeviceSkillStorePage({super.key, required this.existingSkills});

  @override
  State<DeviceSkillStorePage> createState() => _DeviceSkillStorePageState();
}

class _DeviceSkillStorePageState extends State<DeviceSkillStorePage> {
  final Map<String, DeviceSkillCatalogItem> _selectedItems = {};
  final TextEditingController _searchController = TextEditingController();
  final FocusNode _searchFocusNode = FocusNode();
  List<DeviceSkillCatalogItem> _items = const [];
  String _query = '';
  String _category = '全部';
  String _error = '';
  bool _loading = true;
  bool _searchVisible = false;

  Set<String> get _existingIds =>
      widget.existingSkills.map((item) => item.id).toSet();

  List<String> get _categories {
    final categories =
        _items
            .map((item) => item.category.trim())
            .where((item) => item.isNotEmpty)
            .toSet()
            .toList()
          ..sort();
    return ['全部', ...categories];
  }

  List<DeviceSkillCatalogItem> get _filteredItems {
    final query = _query.trim().toLowerCase();
    return _items
        .where((item) {
          if (!item.enabled) return false;
          if (_category != '全部' && item.category != _category) return false;
          return query.isEmpty || item.searchableText.contains(query);
        })
        .toList(growable: false);
  }

  @override
  void initState() {
    super.initState();
    _searchController.addListener(() {
      if (mounted) setState(() => _query = _searchController.text);
    });
    _searchFocusNode.addListener(() {
      if (!mounted || _searchFocusNode.hasFocus) return;
      setState(() => _searchVisible = false);
    });
    _load();
  }

  @override
  void dispose() {
    _searchController.dispose();
    _searchFocusNode.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final data = await ApiClient.get('/api/skills');
      final result = DeviceSkillCatalogResult.fromJson(data);
      if (!mounted) return;
      setState(() {
        _items = result.items;
        _loading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = error.toString();
      });
    }
  }

  void _toggle(DeviceSkillCatalogItem item, bool selected) {
    if (_existingIds.contains(item.id)) return;
    setState(() {
      if (selected) {
        _selectedItems[item.id] = item;
      } else {
        _selectedItems.remove(item.id);
      }
    });
  }

  void _showSearch() {
    setState(() => _searchVisible = true);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _searchFocusNode.requestFocus();
    });
  }

  Future<void> _showDetail(DeviceSkillCatalogItem item) async {
    await showDeviceSkillDetailDialog(
      context: context,
      title: item.displayName,
      description: item.description,
      content: item.content,
      tags: item.tags,
      category: item.category,
      source: item.source,
      packageFiles: item.packageFiles,
    );
  }

  Widget _itemTile(DeviceSkillCatalogItem item) {
    final configured = _existingIds.contains(item.id);
    final selected = _selectedItems.containsKey(item.id);
    return InkWell(
      onTap: configured ? null : () => _toggle(item, !selected),
      borderRadius: AppRadius.mdRadius,
      child: PanelCard(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Checkbox(
                value: configured || selected,
                onChanged: configured
                    ? null
                    : (value) => _toggle(item, value ?? false),
                visualDensity: VisualDensity.compact,
              ),
            ),
            Container(
              width: 36,
              height: 36,
              margin: const EdgeInsets.only(top: 6),
              decoration: const BoxDecoration(
                color: AppColors.primaryLight,
                borderRadius: AppRadius.smRadius,
              ),
              child: const Icon(
                Icons.auto_awesome_outlined,
                color: AppColors.primary,
                size: 18,
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.only(top: 7),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            item.displayName,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: Theme.of(context).textTheme.labelLarge,
                          ),
                        ),
                        StatusPill(
                          label: configured
                              ? '已配置'
                              : selected
                              ? '已选'
                              : '未配置',
                          type: configured
                              ? StatusType.offline
                              : selected
                              ? StatusType.online
                              : StatusType.warning,
                        ),
                        IconButton(
                          tooltip: '查看详情',
                          onPressed: () => _showDetail(item),
                          icon: const Icon(
                            Icons.info_outline_rounded,
                            size: 18,
                          ),
                          visualDensity: VisualDensity.compact,
                        ),
                      ],
                    ),
                    if (item.description.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Text(
                        item.description,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                    ],
                    if (item.category.isNotEmpty || item.source.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Text(
                        [
                          item.category,
                          item.source,
                        ].where((value) => value.isNotEmpty).join(' · '),
                        style: Theme.of(context).textTheme.labelSmall,
                      ),
                    ],
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final items = _filteredItems;
    return Scaffold(
      floatingActionButton: _selectedItems.isEmpty
          ? null
          : FloatingActionButton.extended(
              onPressed: () =>
                  Navigator.of(context).pop(_selectedItems.values.toList()),
              icon: const Icon(Icons.add_rounded),
              label: const Text('添加选中'),
            ),
      floatingActionButtonLocation: FloatingActionButtonLocation.endFloat,
      body: SafeArea(
        child: PageBackground(
          child: Column(
            children: [
              AppTopBar(
                title: 'Skill商店',
                leading: IconButton(
                  icon: const Icon(Icons.arrow_back_ios_new, size: 16),
                  onPressed: () => Navigator.of(context).pop(),
                ),
              ),
              Expanded(
                child: _loading
                    ? const DeviceSkillStoreSkeleton()
                    : ListView(
                        padding: const EdgeInsets.all(16),
                        children: [
                          PanelCard(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Row(
                                  children: [
                                    Expanded(
                                      child: Text(
                                        '从商店添加 Skill',
                                        style: Theme.of(
                                          context,
                                        ).textTheme.titleMedium,
                                      ),
                                    ),
                                    Text(
                                      '已选 ${_selectedItems.length}',
                                      style: const TextStyle(
                                        fontSize: 12,
                                        color: AppColors.textMuted,
                                      ),
                                    ),
                                    IconButton(
                                      tooltip: '搜索 Skill',
                                      onPressed: _showSearch,
                                      icon: const Icon(Icons.search_rounded),
                                      visualDensity: VisualDensity.compact,
                                    ),
                                  ],
                                ),
                                const SizedBox(height: 5),
                                const Text(
                                  '选择后点击“添加选中”，返回设备 Skill 配置页统一保存。',
                                  style: TextStyle(
                                    fontSize: 12,
                                    color: AppColors.textMuted,
                                  ),
                                ),
                                if (_searchVisible) ...[
                                  const SizedBox(height: 12),
                                  TextField(
                                    controller: _searchController,
                                    focusNode: _searchFocusNode,
                                    decoration: InputDecoration(
                                      hintText: '搜索 Skill',
                                      suffixIcon: IconButton(
                                        onPressed: () {
                                          _searchController.clear();
                                          _searchFocusNode.unfocus();
                                        },
                                        icon: const Icon(Icons.close_rounded),
                                      ),
                                    ),
                                  ),
                                ],
                                const SizedBox(height: 12),
                                SingleChildScrollView(
                                  scrollDirection: Axis.horizontal,
                                  child: Row(
                                    children: [
                                      for (final category in _categories)
                                        Padding(
                                          padding: const EdgeInsets.only(
                                            right: 8,
                                          ),
                                          child: ChoiceChip(
                                            label: Text(category),
                                            selected: _category == category,
                                            onSelected: (_) => setState(
                                              () => _category = category,
                                            ),
                                          ),
                                        ),
                                    ],
                                  ),
                                ),
                              ],
                            ),
                          ),
                          if (_error.isNotEmpty) ...[
                            const SizedBox(height: 12),
                            Container(
                              width: double.infinity,
                              padding: const EdgeInsets.all(12),
                              decoration: const BoxDecoration(
                                color: AppColors.statusErrorLight,
                                borderRadius: AppRadius.smRadius,
                              ),
                              child: Text(
                                _error,
                                style: const TextStyle(
                                  color: AppColors.statusError,
                                  fontSize: 12,
                                ),
                              ),
                            ),
                          ],
                          const SizedBox(height: 12),
                          if (items.isEmpty)
                            const EmptyState(
                              message: '没有匹配的 Skill',
                              icon: Icons.search_off_rounded,
                            )
                          else
                            for (final item in items) ...[
                              _itemTile(item),
                              const SizedBox(height: 10),
                            ],
                          const SizedBox(height: 16),
                        ],
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
