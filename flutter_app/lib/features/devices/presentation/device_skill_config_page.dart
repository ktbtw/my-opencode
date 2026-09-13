import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import '../data/device_skill_import_parser.dart';
import '../data/device_skill_model.dart';
import 'device_provider.dart';
import 'device_skill_detail_dialog.dart';
import 'device_skill_store_page.dart';

class DeviceSkillConfigPage extends ConsumerStatefulWidget {
  final String machineId;

  const DeviceSkillConfigPage({super.key, required this.machineId});

  @override
  ConsumerState<DeviceSkillConfigPage> createState() =>
      _DeviceSkillConfigPageState();
}

class _DeviceSkillConfigPageState extends ConsumerState<DeviceSkillConfigPage> {
  DeviceGlobalSkillConfigInfo? _config;
  final TextEditingController _searchController = TextEditingController();
  final FocusNode _searchFocusNode = FocusNode();
  String _query = '';
  String _error = '';
  bool _loading = true;
  bool _saving = false;
  bool _searchVisible = false;

  List<DeviceGlobalSkillConfigItem> get _filteredSkills {
    final query = _query.trim().toLowerCase();
    return (_config?.skills ?? const <DeviceGlobalSkillConfigItem>[])
        .where((item) {
          if (query.isEmpty) return true;
          return [
            item.id,
            item.name,
            item.description,
            ...item.tags,
          ].join(' ').toLowerCase().contains(query);
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
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      final config = await ref
          .read(deviceRepositoryProvider)
          .getDeviceSkillConfig(widget.machineId);
      if (!mounted) return;
      setState(() {
        _config = config;
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

  Future<void> _save(
    DeviceGlobalSkillConfigInfo config, {
    String successMessage = '设备 Skill 配置已更新',
  }) async {
    if (_saving) return;
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      final saved = await ref
          .read(deviceRepositoryProvider)
          .saveDeviceSkillConfig(machineId: widget.machineId, config: config);
      ref.invalidate(deviceDetailProvider(widget.machineId));
      ref.invalidate(deviceListProvider);
      if (!mounted) return;
      setState(() {
        _config = saved;
        _saving = false;
      });
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(successMessage)));
    } catch (error) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = error.toString();
        });
      }
    }
  }

  DeviceGlobalSkillConfigInfo _copyConfig({
    bool? enabled,
    List<DeviceGlobalSkillConfigItem>? skills,
  }) {
    final current = _config ?? const DeviceGlobalSkillConfigInfo();
    return DeviceGlobalSkillConfigInfo(
      enabled: enabled ?? true,
      skills: skills ?? current.skills,
    );
  }

  Future<void> _toggleSkill(
    DeviceGlobalSkillConfigItem item,
    bool enabled,
  ) async {
    final skills = (_config?.skills ?? const <DeviceGlobalSkillConfigItem>[])
        .map(
          (current) => current.id == item.id
              ? DeviceGlobalSkillConfigItem(
                  id: current.id,
                  name: current.name,
                  description: current.description,
                  content: current.content,
                  tags: current.tags,
                  enabled: enabled,
                  packageFiles: current.packageFiles,
                )
              : current,
        )
        .toList(growable: false);
    await _save(_copyConfig(skills: skills));
  }

  Future<void> _importSkill() async {
    if (_saving) return;
    final picked = await FilePicker.platform.pickFiles(
      type: FileType.custom,
      allowedExtensions: const ['md', 'zip'],
      withData: true,
    );
    final file = picked?.files.single;
    if (file == null || file.bytes == null || !mounted) return;
    try {
      final skill = DeviceSkillImportParser.parseFile(file.name, file.bytes!);
      final config = await ref
          .read(deviceRepositoryProvider)
          .importDeviceSkill(machineId: widget.machineId, skill: skill);
      ref.invalidate(deviceDetailProvider(widget.machineId));
      ref.invalidate(deviceListProvider);
      if (!mounted) return;
      setState(() => _config = config);
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Skill 已导入并启用，已有同名 Skill 已覆盖')),
      );
    } on FormatException catch (error) {
      if (mounted) setState(() => _error = error.message.toString());
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  Future<void> _openStore() async {
    if (_config == null) return;
    final skills = await Navigator.of(context)
        .push<List<DeviceSkillCatalogItem>>(
          MaterialPageRoute(
            builder: (context) =>
                DeviceSkillStorePage(existingSkills: _config!.skills),
          ),
        );
    if (skills == null || skills.isEmpty || _config == null) return;
    final entries = <String, DeviceGlobalSkillConfigItem>{
      for (final item in _config!.skills) item.id: item,
    };
    for (final item in skills) {
      entries[item.id] = DeviceGlobalSkillConfigItem(
        id: item.id,
        name: item.name,
        description: item.description,
        content: item.content,
        tags: item.tags,
        enabled: true,
        packageFiles: item.packageFiles,
      );
    }
    await _save(
      _copyConfig(skills: entries.values.toList(growable: false)),
      successMessage: '已从商店添加 ${skills.length} 个 Skill',
    );
  }

  void _showSearch() {
    setState(() => _searchVisible = true);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _searchFocusNode.requestFocus();
    });
  }

  Future<void> _showDetail(DeviceGlobalSkillConfigItem item) async {
    await showDeviceSkillDetailDialog(
      context: context,
      title: item.name,
      description: item.description,
      content: item.content,
      tags: item.tags,
      packageFiles: item.packageFiles,
    );
  }

  Widget _skillTile(DeviceGlobalSkillConfigItem item) {
    return PanelCard(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
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
                          item.name,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: Theme.of(context).textTheme.labelLarge,
                        ),
                      ),
                      IconButton(
                        tooltip: '查看详情',
                        onPressed: () => _showDetail(item),
                        icon: const Icon(Icons.info_outline_rounded, size: 18),
                        visualDensity: VisualDensity.compact,
                      ),
                    ],
                  ),
                  if (item.description.isNotEmpty)
                    Text(
                      item.description,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                  const SizedBox(height: 4),
                  Text(
                    item.enabled ? '已加载到设备 Agent' : '未加载',
                    style: Theme.of(context).textTheme.labelSmall,
                  ),
                ],
              ),
            ),
          ),
          Switch.adaptive(
            value: item.enabled,
            onChanged: _saving ? null : _toggleSkill.bind(item),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final config = _config;
    return Scaffold(
      body: SafeArea(
        child: PageBackground(
          child: Column(
            children: [
              AppTopBar(
                title: 'Skill配置',
                leading: IconButton(
                  icon: const Icon(Icons.arrow_back_ios_new, size: 16),
                  onPressed: () => context.pop(),
                ),
                actions: [
                  IconButton(
                    tooltip: '搜索 Skill',
                    onPressed: _showSearch,
                    icon: const Icon(Icons.search_rounded),
                  ),
                  IconButton(
                    tooltip: 'Skill商店',
                    onPressed: _saving ? null : _openStore,
                    icon: const Icon(Icons.storefront_outlined),
                  ),
                  IconButton(
                    tooltip: '导入 Skill',
                    onPressed: _saving ? null : _importSkill,
                    icon: const Icon(Icons.file_upload_outlined),
                  ),
                ],
              ),
              Expanded(
                child: _loading && config == null
                    ? const DeviceSkillConfigSkeleton()
                    : PageLoadingOverlay(
                        loading: _loading,
                        child: ListView(
                          padding: const EdgeInsets.all(16),
                          children: [
                            PanelCard(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text(
                                    '设备全局 Skill',
                                    style: Theme.of(
                                      context,
                                    ).textTheme.titleMedium,
                                  ),
                                  const SizedBox(height: 4),
                                  const Text(
                                    '下方已开启的 Skill 会加载到设备上的所有 Agent；关闭单项即可停用。',
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
                            if (_filteredSkills.isEmpty)
                              const EmptyState(
                                message: '当前设备还没有 Skill，请从商店添加或导入本地 Skill',
                                icon: Icons.auto_awesome_outlined,
                              )
                            else
                              for (final item in _filteredSkills) ...[
                                _skillTile(item),
                                const SizedBox(height: 10),
                              ],
                            const SizedBox(height: 16),
                          ],
                        ),
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

extension on Future<void> Function(DeviceGlobalSkillConfigItem, bool) {
  ValueChanged<bool> bind(DeviceGlobalSkillConfigItem item) =>
      (value) => this(item, value);
}
