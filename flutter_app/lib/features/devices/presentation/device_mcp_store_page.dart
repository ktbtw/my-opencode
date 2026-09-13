import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../../core/notifications/app_notification_feedback.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import '../data/device_mcp_config_model.dart';
import '../data/device_mcp_store_catalog.dart';

enum _StoreConfigFilter { all, unconfigured, configured }

extension _StoreConfigFilterLabel on _StoreConfigFilter {
  String get label {
    return switch (this) {
      _StoreConfigFilter.all => '全部状态',
      _StoreConfigFilter.unconfigured => '未配置',
      _StoreConfigFilter.configured => '已配置',
    };
  }
}

class DeviceMCPStorePage extends StatefulWidget {
  final List<DeviceMCPServerInfo> existingServers;

  const DeviceMCPStorePage({super.key, required this.existingServers});

  @override
  State<DeviceMCPStorePage> createState() => _DeviceMCPStorePageState();
}

class _DeviceMCPStorePageState extends State<DeviceMCPStorePage> {
  final Map<String, DeviceMCPServerInfo> _selectedServers = {};
  final TextEditingController _searchController = TextEditingController();
  List<DeviceMCPStoreItem> _items = const [];
  bool _loading = true;
  String _category = '全部';
  String _query = '';
  _StoreConfigFilter _configFilter = _StoreConfigFilter.all;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _searchController.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final result = await loadDeviceMCPStoreCatalog();
    if (!mounted) return;
    setState(() {
      _items = result.items;
      _loading = false;
    });
    if (result.warning.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted) return;
        _showLoadWarning(result.warning);
      });
    }
  }

  List<String> get _categories {
    final values = _items.map((item) => item.category).toSet().toList()..sort();
    values.remove('推荐');
    return ['全部', '推荐', ...values];
  }

  bool _matchesCategory(DeviceMCPStoreItem item, String category) {
    if (category == '全部') return true;
    if (category == '推荐') {
      return item.id.startsWith('recommended:') || item.category == '推荐';
    }
    return item.category == category;
  }

  List<DeviceMCPStoreItem> get _filteredItems {
    final query = _query.trim().toLowerCase();
    return _items.where((item) {
      if (!_matchesCategory(item, _category)) return false;
      final configured = _configuredServerFor(item) != null;
      if (_configFilter == _StoreConfigFilter.configured && !configured) {
        return false;
      }
      if (_configFilter == _StoreConfigFilter.unconfigured && configured) {
        return false;
      }
      if (query.isEmpty) return true;
      final detail = item.server.type == 'local'
          ? item.server.command.join(' ')
          : item.server.url;
      return [
        item.title,
        item.category,
        item.description,
        item.source,
        item.server.name,
        item.server.type,
        detail,
      ].any((value) => value.toLowerCase().contains(query));
    }).toList();
  }

  Future<void> _openDetail(DeviceMCPStoreItem item) async {
    final server = await Navigator.of(context).push<DeviceMCPServerInfo>(
      MaterialPageRoute(
        builder: (context) => _StoreItemDetailPage(
          item: item,
          existing: _configuredServerFor(item),
          selected: _selectedServers[item.id],
        ),
      ),
    );
    if (server == null || !mounted) return;
    setState(() {
      _selectedServers[item.id] = server;
    });
  }

  void _removeSelected(DeviceMCPStoreItem item) {
    if (_configuredServerFor(item) != null) return;
    setState(() {
      _selectedServers.remove(item.id);
    });
  }

  void _submit() {
    Navigator.of(context).pop(_selectedServers.values.toList());
  }

  Future<void> _showLoadWarning(String warning) async {
    await showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('MCP 目录加载提示'),
        content: Text('部分联网目录加载失败，当前已显示可用的内置和已加载目录。\n\n$warning'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('知道了'),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final filtered = _filteredItems;
    return Scaffold(
      floatingActionButton: _selectedServers.isEmpty
          ? null
          : FloatingActionButton.extended(
              onPressed: _submit,
              icon: const Icon(Icons.add_rounded),
              label: const Text('添加选中'),
            ),
      floatingActionButtonLocation: FloatingActionButtonLocation.endFloat,
      body: SafeArea(
        child: PageBackground(
          child: Column(
            children: [
              AppTopBar(
                title: 'MCP商店',
                leading: IconButton(
                  icon: const Icon(Icons.arrow_back_ios_new, size: 16),
                  onPressed: () => Navigator.of(context).pop(),
                ),
                actions: [
                  Padding(
                    padding: const EdgeInsets.only(right: 12),
                    child: Center(
                      child: Text(
                        '已选 ${_selectedServers.length}',
                        style: const TextStyle(
                          fontSize: 12,
                          color: AppColors.textMuted,
                        ),
                      ),
                    ),
                  ),
                ],
              ),
              Expanded(
                child: _loading
                    ? const DeviceMCPStoreSkeleton()
                    : LayoutBuilder(
                        builder: (context, constraints) {
                          return Column(
                            children: [
                              _StoreHeader(
                                controller: _searchController,
                                categories: _categories,
                                selectedCategory: _category,
                                total: _items.length,
                                visible: filtered.length,
                                configured: _configuredCount,
                                unconfigured: _unconfiguredCount,
                                configFilter: _configFilter,
                                itemCountOf: _categoryCount,
                                onChanged: (value) =>
                                    setState(() => _query = value),
                                onCategoryChanged: (value) =>
                                    setState(() => _category = value),
                                onConfigFilterChanged: (value) =>
                                    setState(() => _configFilter = value),
                              ),
                              Expanded(
                                child: _StoreList(
                                  items: filtered,
                                  configuredResolver: _configuredServerFor,
                                  selectedIds: _selectedServers.keys.toSet(),
                                  onOpenDetail: _openDetail,
                                  onRemoveSelected: _removeSelected,
                                ),
                              ),
                            ],
                          );
                        },
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  int _categoryCount(String category) {
    return _items.where((item) => _matchesCategory(item, category)).length;
  }

  int get _configuredCount {
    return _items.where((item) => _configuredServerFor(item) != null).length;
  }

  int get _unconfiguredCount => _items.length - _configuredCount;

  DeviceMCPServerInfo? _configuredServerFor(DeviceMCPStoreItem item) {
    for (final server in widget.existingServers) {
      if (_matchesStoreItem(server, item)) return server;
    }
    return null;
  }
}

class _StoreHeader extends StatelessWidget {
  final TextEditingController controller;
  final List<String> categories;
  final String selectedCategory;
  final int total;
  final int visible;
  final int configured;
  final int unconfigured;
  final _StoreConfigFilter configFilter;
  final int Function(String category) itemCountOf;
  final ValueChanged<String> onChanged;
  final ValueChanged<String> onCategoryChanged;
  final ValueChanged<_StoreConfigFilter> onConfigFilterChanged;

  const _StoreHeader({
    required this.controller,
    required this.categories,
    required this.selectedCategory,
    required this.total,
    required this.visible,
    required this.configured,
    required this.unconfigured,
    required this.configFilter,
    required this.itemCountOf,
    required this.onChanged,
    required this.onCategoryChanged,
    required this.onConfigFilterChanged,
  });

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.fromLTRB(
        AppBreakpoints.isMobile(context) ? 16 : 24,
        16,
        AppBreakpoints.isMobile(context) ? 16 : 24,
        12,
      ),
      child: PanelCard(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            TextField(
              controller: controller,
              onChanged: onChanged,
              decoration: InputDecoration(
                hintText: '搜索 MCP 名称、分类、描述、命令或 URL',
                prefixIcon: const Icon(
                  Icons.search,
                  size: 18,
                  color: AppColors.textMuted,
                ),
                suffixIcon: controller.text.isEmpty
                    ? null
                    : IconButton(
                        tooltip: '清空搜索',
                        icon: const Icon(Icons.close_rounded, size: 18),
                        onPressed: () {
                          controller.clear();
                          onChanged('');
                        },
                      ),
              ),
            ),
            const SizedBox(height: 10),
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  StatusPill(label: '共 $total 个', type: StatusType.processing),
                  const SizedBox(width: 8),
                  StatusPill(label: '当前 $visible 个', type: StatusType.online),
                  const SizedBox(width: 8),
                  StatusPill(
                    label: '已配置 $configured 个',
                    type: StatusType.offline,
                  ),
                  const SizedBox(width: 8),
                  StatusPill(
                    label: '未配置 $unconfigured 个',
                    type: StatusType.warning,
                  ),
                ],
              ),
            ),
            const SizedBox(height: 10),
            SizedBox(
              width: double.infinity,
              child: SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                child: Row(
                  children: [
                    _FilterChip(
                      label: '全部 ${itemCountOf('全部')}',
                      selected: selectedCategory == '全部',
                      onSelected: () => onCategoryChanged('全部'),
                    ),
                    _FilterChip(
                      label: '${_StoreConfigFilter.all.label} $total',
                      selected: configFilter == _StoreConfigFilter.all,
                      onSelected: () =>
                          onConfigFilterChanged(_StoreConfigFilter.all),
                    ),
                    ...[
                      _StoreConfigFilter.unconfigured,
                      _StoreConfigFilter.configured,
                    ].map((filter) {
                      final count = switch (filter) {
                        _StoreConfigFilter.all => total,
                        _StoreConfigFilter.unconfigured => unconfigured,
                        _StoreConfigFilter.configured => configured,
                      };
                      return _FilterChip(
                        label: '${filter.label} $count',
                        selected: filter == configFilter,
                        onSelected: () => onConfigFilterChanged(filter),
                      );
                    }),
                    ...categories.where((category) => category != '全部').map((
                      category,
                    ) {
                      return Padding(
                        padding: const EdgeInsets.only(right: 8),
                        child: ChoiceChip(
                          label: Text('$category ${itemCountOf(category)}'),
                          selected: category == selectedCategory,
                          onSelected: (_) => onCategoryChanged(category),
                        ),
                      );
                    }),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _FilterChip extends StatelessWidget {
  final String label;
  final bool selected;
  final VoidCallback onSelected;

  const _FilterChip({
    required this.label,
    required this.selected,
    required this.onSelected,
  });

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(right: 8),
      child: ChoiceChip(
        label: Text(label),
        selected: selected,
        onSelected: (_) => onSelected(),
      ),
    );
  }
}

class _StoreList extends StatelessWidget {
  final List<DeviceMCPStoreItem> items;
  final DeviceMCPServerInfo? Function(DeviceMCPStoreItem item)
  configuredResolver;
  final Set<String> selectedIds;
  final ValueChanged<DeviceMCPStoreItem> onOpenDetail;
  final ValueChanged<DeviceMCPStoreItem> onRemoveSelected;

  const _StoreList({
    required this.items,
    required this.configuredResolver,
    required this.selectedIds,
    required this.onOpenDetail,
    required this.onRemoveSelected,
  });

  @override
  Widget build(BuildContext context) {
    if (items.isEmpty) {
      return const EmptyState(
        message: '没有匹配的 MCP',
        icon: Icons.search_off_rounded,
      );
    }
    return ListView.separated(
      padding: EdgeInsets.fromLTRB(
        AppBreakpoints.isMobile(context) ? 16 : 24,
        0,
        AppBreakpoints.isMobile(context) ? 16 : 24,
        24,
      ),
      itemCount: items.length,
      separatorBuilder: (_, __) => const SizedBox(height: 10),
      itemBuilder: (context, index) {
        final item = items[index];
        final exists = configuredResolver(item) != null;
        final checked = selectedIds.contains(item.id);
        return _StoreItemCard(
          item: item,
          exists: exists,
          checked: checked,
          onOpenDetail: () => onOpenDetail(item),
          onRemoveSelected: checked ? () => onRemoveSelected(item) : null,
        );
      },
    );
  }
}

class _StoreItemCard extends StatelessWidget {
  final DeviceMCPStoreItem item;
  final bool exists;
  final bool checked;
  final VoidCallback onOpenDetail;
  final VoidCallback? onRemoveSelected;

  const _StoreItemCard({
    required this.item,
    required this.exists,
    required this.checked,
    required this.onOpenDetail,
    required this.onRemoveSelected,
  });

  @override
  Widget build(BuildContext context) {
    final detail = item.server.type == 'local'
        ? item.server.command.join(' ')
        : item.server.url;
    return InkWell(
      onTap: onOpenDetail,
      borderRadius: AppRadius.mdRadius,
      child: PanelCard(
        padding: const EdgeInsets.all(12),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _StoreSelectionBadge(
              exists: exists,
              checked: checked,
              onRemoveSelected: onRemoveSelected,
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          item.title,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontSize: 14,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                      ),
                      StatusPill(
                        label: exists
                            ? '已配置'
                            : checked
                            ? '已选'
                            : '未配置',
                        type: exists
                            ? StatusType.offline
                            : checked
                            ? StatusType.online
                            : StatusType.warning,
                      ),
                    ],
                  ),
                  const SizedBox(height: 6),
                  Text(
                    item.description.isEmpty
                        ? item.server.name
                        : item.description,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      fontSize: 12,
                      color: AppColors.textSecondary,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      StatusPill(
                        label: item.category,
                        type: StatusType.processing,
                      ),
                      StatusPill(
                        label: item.server.type == 'local' ? '本地' : '远程',
                        type: StatusType.processing,
                      ),
                      StatusPill(
                        label: item.source,
                        type: StatusType.processing,
                      ),
                    ],
                  ),
                  if (detail.isNotEmpty) ...[
                    const SizedBox(height: 8),
                    Text(
                      detail,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 12,
                        fontFamily: 'monospace',
                        color: AppColors.textMuted,
                      ),
                    ),
                  ],
                  if (item.server.environment.isNotEmpty) ...[
                    const SizedBox(height: 6),
                    Text(
                      '需要环境变量：${item.server.environment.keys.join('、')}',
                      style: const TextStyle(
                        fontSize: 12,
                        color: AppColors.statusWarning,
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _StoreSelectionBadge extends StatelessWidget {
  final bool exists;
  final bool checked;
  final VoidCallback? onRemoveSelected;

  const _StoreSelectionBadge({
    required this.exists,
    required this.checked,
    required this.onRemoveSelected,
  });

  @override
  Widget build(BuildContext context) {
    if (exists) {
      return const SizedBox(
        width: 32,
        height: 32,
        child: Icon(
          Icons.check_circle_rounded,
          size: 22,
          color: AppColors.statusOffline,
        ),
      );
    }
    if (checked) {
      return IconButton(
        tooltip: '取消选中',
        icon: const Icon(
          Icons.check_circle_rounded,
          size: 22,
          color: AppColors.statusOnline,
        ),
        onPressed: onRemoveSelected,
      );
    }
    return const SizedBox(
      width: 32,
      height: 32,
      child: Icon(
        Icons.info_outline_rounded,
        size: 20,
        color: AppColors.textMuted,
      ),
    );
  }
}

class _StoreItemDetailPage extends StatefulWidget {
  final DeviceMCPStoreItem item;
  final DeviceMCPServerInfo? existing;
  final DeviceMCPServerInfo? selected;

  const _StoreItemDetailPage({
    required this.item,
    required this.existing,
    required this.selected,
  });

  @override
  State<_StoreItemDetailPage> createState() => _StoreItemDetailPageState();
}

class _StoreItemDetailPageState extends State<_StoreItemDetailPage> {
  final Map<String, TextEditingController> _environmentControllers = {};

  bool get _exists => widget.existing != null;

  DeviceMCPServerInfo get _server =>
      widget.selected ?? widget.existing ?? widget.item.server;

  @override
  void initState() {
    super.initState();
    for (final key in _environmentKeys) {
      _environmentControllers[key] = TextEditingController(
        text: _initialEnvironmentValue(key),
      );
    }
  }

  @override
  void dispose() {
    for (final controller in _environmentControllers.values) {
      controller.dispose();
    }
    super.dispose();
  }

  bool get _hasRequiredEnvironment => _environmentControllers.isNotEmpty;

  bool get _canAdd {
    if (_exists) return false;
    for (final controller in _environmentControllers.values) {
      if (controller.text.trim().isEmpty) return false;
    }
    return true;
  }

  List<String> get _environmentKeys {
    return {
      ...widget.item.server.environment.keys,
      ...?widget.existing?.environment.keys,
      ...?widget.selected?.environment.keys,
    }.toList();
  }

  String _initialEnvironmentValue(String key) {
    return resolveMCPStoreEnvironmentInitialValue(
      key: key,
      templateValue: widget.item.server.environment[key] ?? '',
      selectedValue: widget.selected?.environment[key],
      existingValue: widget.existing?.environment[key],
    );
  }

  String get _detail {
    return _server.type == 'local'
        ? _server.command.join(' ')
        : _server.url.trim();
  }

  String get _credentialUrl {
    if (widget.item.credentialUrl.trim().isNotEmpty) {
      return widget.item.credentialUrl.trim();
    }
    for (final value in _server.environment.values) {
      final trimmed = value.trim();
      if (!_isPlaceholderValue(trimmed) &&
          (trimmed.startsWith('https://') || trimmed.startsWith('http://'))) {
        return trimmed;
      }
    }
    return '';
  }

  void _onEnvironmentChanged(String _) {
    setState(() {});
  }

  Future<void> _openCredentialUrl() async {
    final url = _credentialUrl;
    if (url.isEmpty) return;
    try {
      final opened = await launchUrl(
        Uri.parse(url),
        mode: LaunchMode.platformDefault,
      );
      if (opened) return;
    } catch (_) {}
    await Clipboard.setData(ClipboardData(text: url));
    if (!mounted) return;
    showAppFeedback(context, message: '获取地址已复制到剪贴板');
  }

  void _add() {
    if (!_canAdd) return;
    final environment = <String, String>{};
    for (final entry in _environmentControllers.entries) {
      environment[entry.key] = entry.value.text.trim();
    }
    Navigator.of(context).pop(_server.copyWith(environment: environment));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: PageBackground(
          child: Column(
            children: [
              AppTopBar(
                title: widget.item.title,
                leading: IconButton(
                  icon: const Icon(Icons.arrow_back_ios_new, size: 16),
                  onPressed: () => Navigator.of(context).pop(),
                ),
                actions: [
                  AppButton(
                    label: _exists ? '已配置' : '添加',
                    icon: _exists
                        ? Icons.check_circle_outline_rounded
                        : Icons.add_rounded,
                    onPressed: _canAdd ? _add : null,
                  ),
                ],
              ),
              Expanded(
                child: ListView(
                  padding: EdgeInsets.fromLTRB(
                    AppBreakpoints.isMobile(context) ? 16 : 24,
                    16,
                    AppBreakpoints.isMobile(context) ? 16 : 24,
                    24,
                  ),
                  children: [
                    PanelCard(
                      padding: const EdgeInsets.all(16),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(
                                      widget.item.title,
                                      style: const TextStyle(
                                        fontSize: 18,
                                        fontWeight: FontWeight.w700,
                                        color: AppColors.textPrimary,
                                      ),
                                    ),
                                    const SizedBox(height: 6),
                                    Text(
                                      widget.item.description.isEmpty
                                          ? _server.name
                                          : widget.item.description,
                                      style: const TextStyle(
                                        fontSize: 13,
                                        height: 1.5,
                                        color: AppColors.textSecondary,
                                      ),
                                    ),
                                  ],
                                ),
                              ),
                              const SizedBox(width: 12),
                              StatusPill(
                                label: _exists
                                    ? '已配置'
                                    : widget.selected == null
                                    ? '未配置'
                                    : '已选',
                                type: _exists
                                    ? StatusType.offline
                                    : widget.selected == null
                                    ? StatusType.warning
                                    : StatusType.online,
                              ),
                            ],
                          ),
                          const SizedBox(height: 14),
                          Wrap(
                            spacing: 8,
                            runSpacing: 8,
                            children: [
                              StatusPill(
                                label: widget.item.category,
                                type: StatusType.processing,
                              ),
                              StatusPill(
                                label: _server.type == 'local' ? '本地' : '远程',
                                type: StatusType.processing,
                              ),
                              StatusPill(
                                label: widget.item.source,
                                type: StatusType.processing,
                              ),
                            ],
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(height: 12),
                    _StoreDetailSection(
                      title: '配置详情',
                      children: [
                        _DetailText(label: '名称', value: _server.name),
                        _DetailText(label: '类型', value: _server.type),
                        if (_detail.isNotEmpty)
                          _DetailText(label: '启动参数', value: _detail),
                      ],
                    ),
                    if (_hasRequiredEnvironment) ...[
                      const SizedBox(height: 12),
                      _StoreDetailSection(
                        title: '必填配置',
                        children: [
                          const Text(
                            '这些值会写入 MCP 的环境变量，用于访问对应服务。',
                            style: TextStyle(
                              fontSize: 12,
                              color: AppColors.textMuted,
                            ),
                          ),
                          if (_credentialUrl.isNotEmpty) ...[
                            const SizedBox(height: 10),
                            Align(
                              alignment: Alignment.centerLeft,
                              child: TextButton.icon(
                                onPressed: _openCredentialUrl,
                                icon: const Icon(
                                  Icons.open_in_new_rounded,
                                  size: 16,
                                ),
                                label: const Text('打开获取地址'),
                              ),
                            ),
                          ],
                          const SizedBox(height: 8),
                          ..._environmentControllers.entries.map((entry) {
                            return Padding(
                              padding: const EdgeInsets.only(bottom: 10),
                              child: TextField(
                                controller: entry.value,
                                onChanged: _onEnvironmentChanged,
                                decoration: InputDecoration(
                                  labelText: entry.key,
                                  hintText: '请输入 ${entry.key}',
                                ),
                              ),
                            );
                          }),
                        ],
                      ),
                    ],
                    if (_exists) ...[
                      const SizedBox(height: 12),
                      const _StoreNotice(text: '该 MCP 已在设备全局配置中存在，不能重复添加。'),
                    ] else if (!_canAdd && _hasRequiredEnvironment) ...[
                      const SizedBox(height: 12),
                      const _StoreNotice(text: '请先填写必填配置，再添加到待添加列表。'),
                    ],
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

class _StoreDetailSection extends StatelessWidget {
  final String title;
  final List<Widget> children;

  const _StoreDetailSection({required this.title, required this.children});

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            title,
            style: const TextStyle(
              fontSize: 14,
              fontWeight: FontWeight.w700,
              color: AppColors.textPrimary,
            ),
          ),
          const SizedBox(height: 12),
          ...children,
        ],
      ),
    );
  }
}

class _DetailText extends StatelessWidget {
  final String label;
  final String value;

  const _DetailText({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: Theme.of(context).textTheme.labelSmall),
          const SizedBox(height: 4),
          SelectableText(
            value,
            style: const TextStyle(
              fontSize: 12,
              height: 1.45,
              fontFamily: 'monospace',
              color: AppColors.textSecondary,
            ),
          ),
        ],
      ),
    );
  }
}

class _StoreNotice extends StatelessWidget {
  final String text;

  const _StoreNotice({required this.text});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: AppColors.statusWarningLight,
        borderRadius: AppRadius.mdRadius,
        border: Border.all(
          color: AppColors.statusWarning.withValues(alpha: 0.25),
        ),
      ),
      child: Text(
        text,
        style: const TextStyle(fontSize: 12, color: AppColors.statusWarning),
      ),
    );
  }
}

bool _isPlaceholderValue(String value) {
  final normalized = value.trim().toUpperCase();
  if (normalized.isEmpty) return true;
  return normalized.startsWith('YOUR_') ||
      normalized.contains('REPLACE_ME') ||
      normalized.contains('_XXX') ||
      normalized.endsWith('_XXX') ||
      normalized.contains('API_KEY') ||
      normalized.contains('TOKEN') ||
      normalized == 'KEY' ||
      normalized == 'SECRET';
}

String resolveMCPStoreEnvironmentInitialValue({
  required String key,
  required String templateValue,
  String? selectedValue,
  String? existingValue,
}) {
  String usableValue(String? value) {
    final trimmed = value?.trim() ?? '';
    if (trimmed.isEmpty || _isPlaceholderValue(trimmed)) return '';
    return trimmed;
  }

  final selected = usableValue(selectedValue);
  if (selected.isNotEmpty) return selected;

  final existing = usableValue(existingValue);
  if (existing.isNotEmpty) return existing;

  if (_looksSensitiveKey(key) || _isPlaceholderValue(templateValue)) {
    return '';
  }
  return templateValue.trim();
}

bool _matchesStoreItem(DeviceMCPServerInfo server, DeviceMCPStoreItem item) {
  if (server.name == item.server.name) return true;
  return false;
}

bool _looksSensitiveKey(String key) {
  final normalized = key.toLowerCase();
  return normalized.contains('key') ||
      normalized.contains('token') ||
      normalized.contains('secret') ||
      normalized.contains('password');
}
