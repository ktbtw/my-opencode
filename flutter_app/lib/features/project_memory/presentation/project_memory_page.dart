import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/config/api_client.dart';
import '../../../core/notifications/app_notification_feedback.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import '../../chat/data/chat_model.dart';
import '../../chat/presentation/chat_provider.dart';
import '../../devices/presentation/device_provider.dart';
import '../data/project_memory_model.dart';
import '../data/project_memory_settings_model.dart';
import 'project_memory_job_presenter.dart';
import 'project_memory_model_picker.dart';
import 'project_memory_provider.dart';

class ProjectMemoryPage extends ConsumerStatefulWidget {
  final String machineId;
  final String scopeId;

  const ProjectMemoryPage({
    super.key,
    required this.machineId,
    required this.scopeId,
  });

  @override
  ConsumerState<ProjectMemoryPage> createState() => _ProjectMemoryPageState();
}

class _ProjectMemoryPageState extends ConsumerState<ProjectMemoryPage> {
  final _searchController = TextEditingController();
  Timer? _searchTimer;
  List<ProjectMemoryModel> _items = const [];
  ProjectMemoryModel? _selected;
  List<ProjectMemoryModel> _history = const [];
  bool _loading = true;
  bool _loadingMore = false;
  bool _hasMore = false;
  String _error = '';
  String _kind = '';
  String _status = '';
  String _verification = '';
  bool? _locked;

  static const _pageSize = 100;

  static const _tabs = <(String, String)>[
    ('全部', ''),
    ('规则', 'locked_rule'),
    ('事实', 'fact'),
    ('流程', 'procedure'),
    ('决策', 'decision'),
    ('问题', 'issue'),
  ];

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _searchTimer?.cancel();
    _searchController.dispose();
    super.dispose();
  }

  Future<void> _load({bool append = false}) async {
    if (append && (_loading || _loadingMore || !_hasMore)) return;
    setState(() {
      if (append) {
        _loadingMore = true;
      } else {
        _loading = true;
      }
      _error = '';
    });
    try {
      final repo = ref.read(projectMemoryRepositoryProvider);
      final items = await repo.listMemories(
        machineId: widget.machineId,
        scopeId: widget.scopeId,
        query: _searchController.text,
        kind: _kind,
        status: _status,
        verification: _verification,
        locked: _locked,
        beforeId: append && _items.isNotEmpty ? _items.last.id : '',
        beforeUpdatedAt: append && _items.isNotEmpty
            ? _items.last.updatedAt
            : null,
        limit: _pageSize,
      );
      final hasMore = items.length == _pageSize;
      if (!mounted) return;
      setState(() {
        if (append) {
          final existing = _items.map((item) => item.id).toSet();
          _items = [..._items, ...items.where((item) => existing.add(item.id))];
        } else {
          _items = items;
        }
        _hasMore = hasMore;
        if (_selected != null) {
          _selected = _items
              .where((item) => item.id == _selected!.id)
              .firstOrNull;
        }
      });
      ref.invalidate(
        projectMemoryOverviewProvider((
          machineId: widget.machineId,
          scopeId: widget.scopeId,
        )),
      );
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
          _loadingMore = false;
        });
      }
    }
  }

  void _searchChanged(String _) {
    _searchTimer?.cancel();
    _searchTimer = Timer(const Duration(milliseconds: 350), _load);
  }

  Future<void> _openMemory(ProjectMemoryModel memory) async {
    try {
      final detail = await ref
          .read(projectMemoryRepositoryProvider)
          .getMemory(widget.machineId, widget.scopeId, memory.id);
      if (!mounted) return;
      setState(() {
        _selected = detail.memory;
        _history = detail.history;
      });
      if (AppBreakpoints.isMobile(context)) {
        await showModalBottomSheet<void>(
          context: context,
          isScrollControlled: true,
          backgroundColor: AppColors.surface,
          shape: const RoundedRectangleBorder(
            borderRadius: BorderRadius.vertical(top: Radius.circular(14)),
          ),
          builder: (context) => FractionallySizedBox(
            heightFactor: .9,
            child: _MemoryDetail(
              memory: detail.memory,
              history: detail.history,
              onEdit: () => _edit(detail.memory),
              onToggleLock: () => _toggleLock(detail.memory),
              onDelete: () => _delete(detail.memory),
              onResolve: (status) => _resolve(detail.memory, status),
            ),
          ),
        );
      }
    } catch (error) {
      _showError(error);
    }
  }

  Future<void> _edit(ProjectMemoryModel memory) async {
    final controller = TextEditingController(text: memory.statement);
    final value = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('编辑记忆'),
        content: SizedBox(
          width: 520,
          child: TextField(
            controller: controller,
            minLines: 6,
            maxLines: 12,
            maxLength: 2000,
            autofocus: true,
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, controller.text.trim()),
            child: const Text('保存'),
          ),
        ],
      ),
    );
    controller.dispose();
    if (value == null || value.isEmpty || value == memory.statement) return;
    await _mutate(
      () => ref
          .read(projectMemoryRepositoryProvider)
          .updateMemory(
            machineId: widget.machineId,
            scopeId: widget.scopeId,
            memory: memory,
            statement: value,
          ),
    );
  }

  Future<void> _toggleLock(ProjectMemoryModel memory) async {
    await _mutate(
      () => ref
          .read(projectMemoryRepositoryProvider)
          .updateMemory(
            machineId: widget.machineId,
            scopeId: widget.scopeId,
            memory: memory,
            locked: !memory.locked,
          ),
    );
  }

  Future<void> _delete(ProjectMemoryModel memory) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除记忆'),
        content: const Text('该版本将保留同步墓碑，默认列表和 Agent 上下文中不再使用。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await _mutate(
      () => ref
          .read(projectMemoryRepositoryProvider)
          .deleteMemory(widget.machineId, widget.scopeId, memory),
    );
  }

  Future<void> _resolve(ProjectMemoryModel memory, String status) async {
    await _mutate(
      () => ref
          .read(projectMemoryRepositoryProvider)
          .resolveMemory(widget.machineId, widget.scopeId, memory, status),
    );
  }

  Future<void> _mutate(Future<void> Function() action) async {
    try {
      await action();
      if (!mounted) return;
      Navigator.of(context).maybePop();
      setState(() => _selected = null);
      await _load();
    } catch (error) {
      _showError(
        error is ApiException && error.statusCode == 409
            ? '记忆已被其他操作更新，列表已刷新'
            : error,
      );
      await _load();
    }
  }

  void _showError(Object error) {
    if (!mounted) return;
    showAppFeedback(
      context,
      title: '项目记忆操作失败',
      message: error.toString(),
      error: true,
    );
  }

  Future<void> _organize() async {
    try {
      final repo = ref.read(projectMemoryRepositoryProvider);
      final job = await repo.organizeNow(widget.machineId, widget.scopeId);
      if (!mounted) return;
      final retry = await showDialog<bool>(
        context: context,
        barrierDismissible: false,
        builder: (context) => _OrganizationDialog(
          initial: job,
          events: repo.watchJob(widget.machineId, widget.scopeId, job.id),
          loadFinal: () =>
              repo.getJob(widget.machineId, widget.scopeId, job.id),
        ),
      );
      if (retry == true) {
        await _organize();
        return;
      }
      await _load();
    } catch (error) {
      _showError(error);
    }
  }

  Future<void> _editSettings(ProjectMemorySettingsModel settings) async {
    List<ModelInfo> models;
    try {
      models = await ref.read(availableModelsProvider(widget.machineId).future);
    } catch (error) {
      if (!mounted) return;
      _showError(error);
      return;
    }
    if (!mounted) return;
    ModelInfo? selectedModel = models
        .where((model) => model.metaKey == settings.projectModel)
        .firstOrNull;
    String projectVariant = settings.projectVariant;
    var followGlobalEnabled = settings.followsGlobalEnabled;
    var projectEnabled = settings.projectEnabled ?? settings.effectiveEnabled;
    var followGlobalModel = settings.followsGlobalModel;
    await showDialog<void>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: const Text('项目记忆设置'),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                SwitchListTile.adaptive(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('跟随全局整理开关'),
                  subtitle: Text(
                    settings.globalEnabled ? '当前全局已开启' : '当前全局已关闭',
                  ),
                  value: followGlobalEnabled,
                  onChanged: (value) {
                    setDialogState(() {
                      followGlobalEnabled = value;
                      if (!value) projectEnabled = settings.effectiveEnabled;
                    });
                  },
                ),
                SwitchListTile.adaptive(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('项目整理机'),
                  subtitle: Text(followGlobalEnabled ? '由全局开关控制' : '仅控制当前项目'),
                  value: followGlobalEnabled
                      ? settings.effectiveEnabled
                      : projectEnabled,
                  onChanged: followGlobalEnabled
                      ? null
                      : (value) => setDialogState(() => projectEnabled = value),
                ),
                const Divider(),
                SwitchListTile.adaptive(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('跟随设备默认整理模型'),
                  subtitle: Text(
                    settings.globalModel.isEmpty
                        ? '当前设备未指定，使用任务模型或 Agent 默认模型'
                        : settings.globalModel,
                  ),
                  value: followGlobalModel,
                  onChanged: (value) {
                    setDialogState(() {
                      followGlobalModel = value;
                      if (!value && selectedModel == null) {
                        selectedModel = models
                            .where(
                              (model) =>
                                  model.metaKey == settings.effectiveModel,
                            )
                            .firstOrNull;
                      }
                    });
                  },
                ),
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  enabled: !followGlobalModel && models.isNotEmpty,
                  title: Text(
                    selectedModel?.name ??
                        (settings.projectModel.isEmpty
                            ? '选择项目整理模型'
                            : settings.projectModel),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                  subtitle: Text(
                    selectedModel?.metaKey ??
                        (models.isEmpty ? '设备没有可用模型' : '点击搜索并选择设备模型'),
                  ),
                  trailing: const Icon(Icons.search),
                  onTap: !followGlobalModel && models.isNotEmpty
                      ? () async {
                          final chosen = await showProjectMemoryModelPicker(
                            context,
                            models: models,
                            selected: selectedModel,
                          );
                          if (chosen != null) {
                            setDialogState(() {
                              selectedModel = chosen;
                              if (!chosen.variants.contains(projectVariant)) {
                                projectVariant = '';
                              }
                            });
                          }
                        }
                      : null,
                ),
                if (selectedModel?.hasVariants ?? false)
                  DropdownButtonFormField<String>(
                    value: selectedModel!.variants.contains(projectVariant)
                        ? projectVariant
                        : null,
                    decoration: const InputDecoration(labelText: '思考强度'),
                    items: [
                      const DropdownMenuItem(value: '', child: Text('默认')),
                      ...selectedModel!.variants.map(
                        (variant) => DropdownMenuItem(
                          value: variant,
                          child: Text(variant),
                        ),
                      ),
                    ],
                    onChanged: (value) =>
                        setDialogState(() => projectVariant = value ?? ''),
                  ),
                const SizedBox(height: 10),
                Text(
                  '当前有效：${settings.effectiveModel.isEmpty ? '任务模型或 Agent 默认模型' : settings.effectiveModel}${settings.effectiveVariant.isEmpty ? '' : ' · ${settings.effectiveVariant}'}',
                  style: const TextStyle(
                    fontSize: 12,
                    color: AppColors.textSecondary,
                  ),
                ),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(dialogContext).pop(),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () async {
                try {
                  await ref
                      .read(projectMemoryRepositoryProvider)
                      .updateSettings(
                        machineId: widget.machineId,
                        scopeId: widget.scopeId,
                        projectEnabled: followGlobalEnabled
                            ? null
                            : projectEnabled,
                        projectModel: followGlobalModel
                            ? ''
                            : (selectedModel?.metaKey ?? ''),
                        projectVariant: followGlobalModel ? '' : projectVariant,
                      );
                  if (!mounted) return;
                  ref
                    ..invalidate(
                      projectMemorySettingsProvider((
                        machineId: widget.machineId,
                        scopeId: widget.scopeId,
                      )),
                    )
                    ..invalidate(
                      projectMemoryOverviewProvider((
                        machineId: widget.machineId,
                        scopeId: widget.scopeId,
                      )),
                    );
                  if (dialogContext.mounted) Navigator.of(dialogContext).pop();
                } catch (error) {
                  _showError(error);
                }
              },
              child: const Text('保存'),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _correctIdentity(ProjectScopeModel scope) async {
    final action = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('项目身份'),
        content: SelectableText(scope.currentRoot),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('取消'),
          ),
          if (scope.lineageScopeId.isNotEmpty)
            OutlinedButton(
              onPressed: () => Navigator.pop(context, 'keep_memory_here'),
              child: const Text('在此保留记忆'),
            ),
          FilledButton(
            onPressed: () => Navigator.pop(context, 'treat_as_new'),
            child: const Text('视为新项目'),
          ),
        ],
      ),
    );
    if (action == null) return;
    try {
      final nextScope = await ref
          .read(projectMemoryRepositoryProvider)
          .correctIdentity(widget.machineId, widget.scopeId, action);
      if (!mounted) return;
      if (nextScope != widget.scopeId) {
        context.go(
          '/devices/${Uri.encodeComponent(widget.machineId)}/projects/${Uri.encodeComponent(nextScope)}/memory',
        );
        return;
      }
      ref.invalidate(
        projectMemoryOverviewProvider((
          machineId: widget.machineId,
          scopeId: widget.scopeId,
        )),
      );
    } catch (error) {
      _showError(error);
    }
  }

  @override
  Widget build(BuildContext context) {
    final key = (machineId: widget.machineId, scopeId: widget.scopeId);
    final overview = ref.watch(projectMemoryOverviewProvider(key));
    final memorySettings = ref.watch(projectMemorySettingsProvider(key));
    final device = ref.watch(deviceDetailProvider(widget.machineId));
    final machineOnline = device.valueOrNull?.online ?? false;
    final scope = overview.valueOrNull?.scope;
    final organizeEnabled =
        machineOnline &&
        scope?.status == 'active' &&
        (memorySettings.valueOrNull?.effectiveEnabled ?? true);

    return Scaffold(
      backgroundColor: Colors.transparent,
      floatingActionButton: Tooltip(
        message: organizeEnabled ? '整理项目记忆' : '项目设备离线或不可用',
        child: FloatingActionButton(
          key: const ValueKey('project-memory-organize-button'),
          onPressed: organizeEnabled ? _organize : null,
          child: const _OrganizeMemoryIcon(
            key: ValueKey('project-memory-organize-icon'),
          ),
        ),
      ),
      body: PageBackground(
        child: SafeArea(
          child: Column(
            children: [
              AppTopBar(
                title: '项目记忆',
                subtitle: scope?.displayName,
                leading: IconButton(
                  tooltip: '返回',
                  onPressed: () => Navigator.of(context).maybePop(),
                  icon: const Icon(Icons.arrow_back),
                ),
                actions: [
                  PopupMenuButton<String>(
                    tooltip: '项目操作',
                    onSelected: (value) {
                      if (value == 'identity' &&
                          machineOnline &&
                          scope != null) {
                        _correctIdentity(scope);
                      } else if (value == 'settings' &&
                          memorySettings.valueOrNull != null) {
                        _editSettings(memorySettings.valueOrNull!);
                      }
                    },
                    itemBuilder: (context) => [
                      PopupMenuItem<String>(
                        value: 'identity',
                        enabled: machineOnline && scope != null,
                        child: const ListTile(
                          contentPadding: EdgeInsets.zero,
                          leading: Icon(Icons.folder_copy_outlined),
                          title: Text('项目身份'),
                        ),
                      ),
                      PopupMenuItem<String>(
                        value: 'settings',
                        enabled: memorySettings.valueOrNull != null,
                        child: const ListTile(
                          contentPadding: EdgeInsets.zero,
                          leading: Icon(Icons.tune_outlined),
                          title: Text('项目记忆设置'),
                        ),
                      ),
                    ],
                    icon: const Icon(Icons.more_vert),
                  ),
                ],
              ),
              Expanded(
                child: Padding(
                  padding: AppBreakpoints.isMobile(context)
                      ? AppSpacing.pagePaddingMobile
                      : AppSpacing.pagePadding,
                  child: Column(
                    children: [
                      _HeaderBand(
                        overview: overview.valueOrNull,
                        online: machineOnline,
                      ),
                      const SizedBox(height: 12),
                      _controls(),
                      const SizedBox(height: 10),
                      _tabsBar(),
                      const Divider(),
                      Expanded(child: _body()),
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

  Widget _controls() {
    final search = TextField(
      controller: _searchController,
      onChanged: _searchChanged,
      decoration: const InputDecoration(
        prefixIcon: Icon(Icons.search),
        hintText: '搜索内容、符号、路径或指纹',
        isDense: true,
      ),
    );
    final filters = PopupMenuButton<String>(
      tooltip: '筛选',
      icon: Badge(
        isLabelVisible:
            _status.isNotEmpty || _verification.isNotEmpty || _locked != null,
        child: const Icon(Icons.filter_list),
      ),
      onSelected: (value) {
        setState(() {
          switch (value) {
            case 'all':
              _status = '';
              _verification = '';
              _locked = null;
            case 'active':
              _status = 'active';
            case 'stale':
              _status = 'stale';
            case 'disputed':
              _status = 'disputed';
            case 'deleted':
              _status = 'deleted';
            case 'verified':
              _verification = 'verified';
            case 'unverified':
              _verification = 'unverified';
            case 'locked':
              _locked = true;
          }
        });
        _load();
      },
      itemBuilder: (context) => const [
        PopupMenuItem(value: 'all', child: Text('清除筛选')),
        PopupMenuItem(value: 'active', child: Text('当前有效')),
        PopupMenuItem(value: 'stale', child: Text('已陈旧')),
        PopupMenuItem(value: 'disputed', child: Text('有冲突')),
        PopupMenuItem(value: 'deleted', child: Text('已删除')),
        PopupMenuItem(value: 'verified', child: Text('已验证')),
        PopupMenuItem(value: 'unverified', child: Text('未验证')),
        PopupMenuItem(value: 'locked', child: Text('已锁定')),
      ],
    );
    return Row(
      children: [
        Expanded(child: search),
        const SizedBox(width: 8),
        filters,
        IconButton(
          tooltip: '刷新',
          onPressed: _load,
          icon: const Icon(Icons.refresh),
        ),
      ],
    );
  }

  Widget _tabsBar() => SizedBox(
    height: 38,
    child: ListView.separated(
      scrollDirection: Axis.horizontal,
      itemCount: _tabs.length,
      separatorBuilder: (_, _) => const SizedBox(width: 6),
      itemBuilder: (context, index) {
        final tab = _tabs[index];
        final selected = tab.$2 == _kind;
        return ChoiceChip(
          label: Text(tab.$1),
          selected: selected,
          onSelected: (_) {
            setState(() => _kind = tab.$2);
            _load();
          },
          shape: RoundedRectangleBorder(borderRadius: AppRadius.smRadius),
        );
      },
    ),
  );

  Widget _body() {
    if (_loading && _items.isEmpty) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_error.isNotEmpty && _items.isEmpty) {
      return _EmptyState(
        icon: Icons.sync_problem,
        title: '读取失败',
        detail: _error,
        action: _load,
      );
    }
    if (_items.isEmpty) {
      return _EmptyState(
        icon: Icons.memory_outlined,
        title: '没有符合条件的记忆',
        detail: '当前项目还没有可用的记忆。',
        action: null,
      );
    }
    final list = ListView.separated(
      itemCount: _items.length + (_hasMore ? 1 : 0),
      separatorBuilder: (_, _) => const Divider(),
      itemBuilder: (context, index) {
        if (index == _items.length) {
          return Center(
            child: TextButton.icon(
              onPressed: _loadingMore ? null : () => _load(append: true),
              icon: _loadingMore
                  ? const SizedBox.square(
                      dimension: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.expand_more),
              label: const Text('加载更多'),
            ),
          );
        }
        return _MemoryRow(
          memory: _items[index],
          selected: _selected?.id == _items[index].id,
          onTap: () => _openMemory(_items[index]),
        );
      },
    );
    if (!AppBreakpoints.isDesktop(context)) return list;
    return Row(
      children: [
        Expanded(flex: 5, child: list),
        const VerticalDivider(width: 24),
        Expanded(
          flex: 4,
          child: _selected == null
              ? const _EmptyState(
                  icon: Icons.subject_outlined,
                  title: '选择一条记忆',
                  detail: '查看验证、产物与来源详情。',
                  action: null,
                )
              : _MemoryDetail(
                  memory: _selected!,
                  history: _history,
                  onEdit: () => _edit(_selected!),
                  onToggleLock: () => _toggleLock(_selected!),
                  onDelete: () => _delete(_selected!),
                  onResolve: (status) => _resolve(_selected!, status),
                ),
        ),
      ],
    );
  }
}

class _HeaderBand extends StatelessWidget {
  final ProjectMemoryOverviewModel? overview;
  final bool online;
  const _HeaderBand({required this.overview, required this.online});

  @override
  Widget build(BuildContext context) {
    final scope = overview?.scope;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      decoration: const BoxDecoration(
        color: AppColors.surface,
        border: Border(bottom: BorderSide(color: AppColors.border)),
      ),
      child: Row(
        children: [
          StatusPill(
            label: online ? '设备在线' : '设备离线',
            type: online ? StatusType.online : StatusType.offline,
          ),
          const SizedBox(width: 10),
          StatusPill(
            label: _scopeStatus(scope?.status ?? 'active'),
            type: scope?.status == 'active'
                ? StatusType.processing
                : StatusType.warning,
          ),
          const Spacer(),
          Text(
            'Revision ${scope?.revision ?? 0}',
            style: const TextStyle(
              fontFamily: 'monospace',
              fontSize: 12,
              color: AppColors.textSecondary,
            ),
          ),
        ],
      ),
    );
  }

  static String _scopeStatus(String value) => switch (value) {
    'detached' => '已分离',
    'missing' => '目录缺失',
    'deleted' => '已删除',
    _ => '当前项目',
  };
}

class _MemoryRow extends StatelessWidget {
  final ProjectMemoryModel memory;
  final bool selected;
  final VoidCallback onTap;
  const _MemoryRow({
    required this.memory,
    required this.selected,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) => InkWell(
    onTap: onTap,
    child: Container(
      color: selected ? AppColors.primaryLight : Colors.transparent,
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 11),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            _kindIcon(memory.kind),
            size: 18,
            color: memory.locked
                ? AppColors.statusWarning
                : AppColors.textSecondary,
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        memory.subjectKey,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          fontSize: 12,
                          fontWeight: FontWeight.w600,
                          fontFamily: 'monospace',
                        ),
                      ),
                    ),
                    _VerificationMark(status: memory.verification.status),
                  ],
                ),
                const SizedBox(height: 4),
                Text(
                  memory.statement,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(fontSize: 13, height: 1.35),
                ),
                const SizedBox(height: 5),
                Text(
                  '${_kindLabel(memory.kind)} · ${_statusLabel(memory.status)} · v${memory.version}',
                  style: const TextStyle(
                    fontSize: 11,
                    color: AppColors.textMuted,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(width: 6),
          const Icon(Icons.chevron_right, size: 18, color: AppColors.textMuted),
        ],
      ),
    ),
  );
}

class _MemoryDetail extends StatelessWidget {
  final ProjectMemoryModel memory;
  final List<ProjectMemoryModel> history;
  final VoidCallback onEdit;
  final VoidCallback onToggleLock;
  final VoidCallback onDelete;
  final ValueChanged<String> onResolve;
  const _MemoryDetail({
    required this.memory,
    required this.history,
    required this.onEdit,
    required this.onToggleLock,
    required this.onDelete,
    required this.onResolve,
  });

  @override
  Widget build(BuildContext context) {
    final tombstone = memory.status == 'deleted';
    final historical = memory.status == 'superseded';
    final mutable = !tombstone && !historical;
    final canChangeStatus = !historical && (!tombstone || !memory.sensitive);
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 8, 8),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  memory.subjectKey,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w600,
                    fontFamily: 'monospace',
                  ),
                ),
              ),
              if (mutable) ...[
                IconButton(
                  tooltip: '编辑',
                  onPressed: onEdit,
                  icon: const Icon(Icons.edit_outlined),
                ),
                IconButton(
                  tooltip: memory.locked ? '解锁' : '锁定',
                  onPressed: onToggleLock,
                  icon: Icon(
                    memory.locked
                        ? Icons.lock_open_outlined
                        : Icons.lock_outline,
                  ),
                ),
              ],
              if (canChangeStatus)
                PopupMenuButton<String>(
                  tooltip: '状态操作',
                  onSelected: onResolve,
                  itemBuilder: (context) => [
                    if (memory.status == 'disputed')
                      const PopupMenuItem(
                        value: 'active',
                        child: Text('解决冲突并采用'),
                      )
                    else if (memory.status != 'active')
                      const PopupMenuItem(
                        value: 'active',
                        child: Text('恢复为当前有效'),
                      ),
                    if (!tombstone && memory.status != 'archived')
                      const PopupMenuItem(value: 'archived', child: Text('归档')),
                  ],
                ),
              if (mutable)
                IconButton(
                  tooltip: '删除',
                  onPressed: onDelete,
                  icon: const Icon(
                    Icons.delete_outline,
                    color: AppColors.statusError,
                  ),
                ),
            ],
          ),
        ),
        const Divider(),
        Expanded(
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              SelectableText(
                memory.statement,
                style: const TextStyle(fontSize: 14, height: 1.5),
              ),
              const SizedBox(height: 16),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  _MetaChip(
                    icon: Icons.category_outlined,
                    text: _kindLabel(memory.kind),
                  ),
                  _MetaChip(
                    icon: Icons.layers_outlined,
                    text: '${_statusLabel(memory.status)} · v${memory.version}',
                  ),
                  _MetaChip(
                    icon: memory.locked
                        ? Icons.lock_outline
                        : Icons.lock_open_outlined,
                    text: memory.locked ? '已锁定' : '未锁定',
                  ),
                  _MetaChip(
                    icon: Icons.verified_outlined,
                    text: _verificationLabel(memory.verification.status),
                  ),
                  _MetaChip(
                    icon: Icons.speed_outlined,
                    text: '${(memory.confidence * 100).round()}%',
                  ),
                ],
              ),
              if (memory.verification.method.isNotEmpty ||
                  memory.verification.evidenceRefs.isNotEmpty) ...[
                const _SectionTitle('验证'),
                _KeyValue(label: '方法', value: memory.verification.method),
                if (memory.verification.evidenceRefs.isNotEmpty)
                  _KeyValue(
                    label: '证据',
                    value: memory.verification.evidenceRefs.join('\n'),
                  ),
              ],
              if (memory.artifacts.isNotEmpty) ...[
                const _SectionTitle('产物与 Native Hook'),
                for (final artifact in memory.artifacts)
                  _ArtifactBlock(artifact: artifact),
              ],
              if (memory.sources.isNotEmpty) ...[
                const _SectionTitle('来源'),
                for (final source in memory.sources)
                  _SourceBlock(source: source),
              ],
              const _SectionTitle('版本'),
              _KeyValue(label: 'Logical ID', value: memory.logicalId),
              _KeyValue(label: 'Memory ID', value: memory.id),
              if (memory.supersedesId.isNotEmpty)
                _KeyValue(label: '替代版本', value: memory.supersedesId),
              _KeyValue(label: '创建者', value: memory.createdBy),
              if (history.length > 1) ...[
                const _SectionTitle('历史版本'),
                for (final version in history)
                  _KeyValue(
                    label:
                        'v${version.version} · ${_statusLabel(version.status)}',
                    value: version.statement,
                  ),
              ],
            ],
          ),
        ),
      ],
    );
  }
}

class _ArtifactBlock extends StatelessWidget {
  final ProjectMemoryArtifactModel artifact;
  const _ArtifactBlock({required this.artifact});
  @override
  Widget build(BuildContext context) => Container(
    margin: const EdgeInsets.only(bottom: 8),
    padding: const EdgeInsets.all(10),
    decoration: BoxDecoration(
      color: AppColors.inputBackground,
      border: Border.all(color: AppColors.border),
      borderRadius: AppRadius.smRadius,
    ),
    child: Column(
      children: [
        _KeyValue(
          label: '模块',
          value: artifact.moduleName.isNotEmpty
              ? artifact.moduleName
              : artifact.path,
        ),
        _KeyValue(
          label: 'SHA256 / Build ID',
          value: '${artifact.sha256}\n${artifact.buildId}',
        ),
        if (artifact.packageName.isNotEmpty || artifact.apkSha256.isNotEmpty)
          _KeyValue(
            label: '包名 / APK SHA256',
            value: '${artifact.packageName}\n${artifact.apkSha256}',
          ),
        _KeyValue(
          label: 'ABI / 版本',
          value: '${artifact.abi} · ${artifact.appVersion}',
        ),
        if (artifact.relativeOffset.isNotEmpty)
          _KeyValue(
            label: '符号 / 偏移 / 函数起点',
            value:
                '${artifact.symbol}\n${artifact.relativeOffset} / ${artifact.functionStart} (${artifact.instructionSet})',
          ),
        if (artifact.idaStatus.isNotEmpty)
          _KeyValue(
            label: 'IDA / 数据库 / 热更新',
            value:
                '${artifact.idaStatus} / ${artifact.idaDatabaseId} / ${artifact.hotUpdateStatus}',
          ),
        if (artifact.shadowHookStatus.isNotEmpty)
          _KeyValue(
            label: 'ShadowHook / UI / Native',
            value:
                '${artifact.shadowHookStatus} / ${artifact.uiCallStatus} / ${artifact.nativeCallStatus}',
          ),
      ],
    ),
  );
}

class _SourceBlock extends StatelessWidget {
  final ProjectMemorySourceModel source;
  const _SourceBlock({required this.source});
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 10),
    child: Column(
      children: [
        _KeyValue(
          label: '任务 / 会话',
          value: '${source.taskId}\n${source.sessionId}',
        ),
        if (source.relativePath.isNotEmpty)
          _KeyValue(label: '路径', value: source.relativePath),
        if (source.sha256.isNotEmpty)
          _KeyValue(label: 'SHA-256', value: source.sha256),
        if (source.status.isNotEmpty)
          _KeyValue(label: '可用性', value: source.status),
        if (source.excerpt.isNotEmpty)
          _KeyValue(label: '摘录', value: source.excerpt),
      ],
    ),
  );
}

class _OrganizationDialog extends StatefulWidget {
  final ProjectMemoryJobModel initial;
  final Stream<ProjectMemoryJobEventModel> events;
  final Future<ProjectMemoryJobModel> Function() loadFinal;
  const _OrganizationDialog({
    required this.initial,
    required this.events,
    required this.loadFinal,
  });
  @override
  State<_OrganizationDialog> createState() => _OrganizationDialogState();
}

class _OrganizationDialogState extends State<_OrganizationDialog> {
  StreamSubscription<ProjectMemoryJobEventModel>? _subscription;
  String _stage = '已排队';
  String _message = '';
  String _error = '';
  int _progress = 0;
  bool _done = false;
  bool _hasCandidateResult = false;
  bool _changed = false;
  int _candidateCount = 0;
  int _acceptedCount = 0;

  @override
  void initState() {
    super.initState();
    _subscription = widget.events.listen(
      (event) {
        if (!mounted) return;
        setState(() {
          _stage = projectMemoryEventLabel(event.type);
          if (event.type == 'candidates') {
            _hasCandidateResult = true;
            _candidateCount = event.metadataInt('candidate_count');
            _acceptedCount = event.metadataInt('accepted_count');
            _changed = event.metadataBool('changed');
          }
          final localizedMessage = projectMemoryEventMessage(event);
          if (event.type == 'completed' && _hasCandidateResult) {
            _message = _completionMessage();
          } else if (localizedMessage.isNotEmpty) {
            _message = localizedMessage;
          }
          _progress = event.progress.clamp(0, 100);
          _done = event.type == 'completed';
          if (event.type == 'retrying') _error = '';
        });
      },
      onError: (Object error) {
        if (mounted) setState(() => _error = error.toString());
      },
      onDone: _loadFinal,
    );
  }

  Future<void> _loadFinal() async {
    try {
      final job = await widget.loadFinal();
      if (!mounted) return;
      setState(() {
        _done = job.status == 'completed';
        _error = job.status == 'failed'
            ? (job.error.isEmpty ? '整理失败' : job.error)
            : _error;
        _stage = _done ? '已完成' : (job.status == 'failed' ? '失败' : _stage);
        if (_done) {
          _progress = 100;
          if (_message.isEmpty) _message = '项目记忆整理完成';
        }
      });
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  @override
  void dispose() {
    _subscription?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('整理项目记忆'),
    content: SizedBox(
      width: 420,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(_stage, style: const TextStyle(fontWeight: FontWeight.w600)),
          const SizedBox(height: 12),
          LinearProgressIndicator(
            value: _progress > 0 ? _progress / 100 : null,
          ),
          if (_message.isNotEmpty) ...[
            const SizedBox(height: 10),
            Text(
              _message,
              style: const TextStyle(color: AppColors.textSecondary),
            ),
          ],
          if (_error.isNotEmpty) ...[
            const SizedBox(height: 10),
            Text(_error, style: const TextStyle(color: AppColors.statusError)),
          ],
        ],
      ),
    ),
    actions: [
      if (_error.isNotEmpty)
        TextButton(
          onPressed: () => Navigator.pop(context, true),
          child: const Text('继续'),
        ),
      TextButton(
        onPressed: _done || _error.isNotEmpty
            ? () => Navigator.pop(context, false)
            : null,
        child: const Text('关闭'),
      ),
    ],
  );

  String _completionMessage() {
    if (_changed) {
      return '整理完成：已校验 $_candidateCount 条候选记忆，接受 $_acceptedCount 条，项目记忆已更新';
    }
    return '整理完成：已校验 $_candidateCount 条候选记忆，没有需要更新的内容';
  }
}

class _OrganizeMemoryIcon extends StatelessWidget {
  const _OrganizeMemoryIcon({super.key});

  @override
  Widget build(BuildContext context) {
    final color = IconTheme.of(context).color ?? AppColors.textOnPrimary;
    return SizedBox.square(
      dimension: 18,
      child: CustomPaint(painter: _OrganizeMemoryIconPainter(color)),
    );
  }
}

class _OrganizeMemoryIconPainter extends CustomPainter {
  final Color color;

  const _OrganizeMemoryIconPainter(this.color);

  @override
  void paint(Canvas canvas, Size size) {
    final unit = size.shortestSide / 18;
    final stroke = Paint()
      ..color = color
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.5 * unit
      ..strokeCap = StrokeCap.round
      ..strokeJoin = StrokeJoin.round;
    final node = Paint()..color = color;

    Offset point(double x, double y) => Offset(x * unit, y * unit);

    const lanes = [(4.0, 14.8), (8.2, 12.2), (12.4, 9.2)];
    for (final lane in lanes) {
      canvas
        ..drawCircle(point(2.8, lane.$1), 1.05 * unit, node)
        ..drawLine(point(5, lane.$1), point(lane.$2, lane.$1), stroke);
    }

    final confirmation = Path()
      ..moveTo(10.3 * unit, 12.3 * unit)
      ..lineTo(12.3 * unit, 14.3 * unit)
      ..lineTo(16 * unit, 9.4 * unit);
    canvas.drawPath(
      confirmation,
      Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.75 * unit
        ..strokeCap = StrokeCap.round
        ..strokeJoin = StrokeJoin.round,
    );
  }

  @override
  bool shouldRepaint(covariant _OrganizeMemoryIconPainter oldDelegate) {
    return oldDelegate.color != color;
  }
}

class _VerificationMark extends StatelessWidget {
  final String status;
  const _VerificationMark({required this.status});
  @override
  Widget build(BuildContext context) {
    final valid = status == 'verified';
    return Tooltip(
      message: _verificationLabel(status),
      child: Icon(
        valid ? Icons.verified : Icons.help_outline,
        size: 15,
        color: valid ? AppColors.statusSuccess : AppColors.statusWarning,
      ),
    );
  }
}

class _MetaChip extends StatelessWidget {
  final IconData icon;
  final String text;
  const _MetaChip({required this.icon, required this.text});
  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
    decoration: BoxDecoration(
      color: AppColors.inputBackground,
      borderRadius: AppRadius.smRadius,
      border: Border.all(color: AppColors.border),
    ),
    child: Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 13, color: AppColors.textSecondary),
        const SizedBox(width: 5),
        Text(text, style: const TextStyle(fontSize: 11)),
      ],
    ),
  );
}

class _SectionTitle extends StatelessWidget {
  final String text;
  const _SectionTitle(this.text);
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(top: 20, bottom: 8),
    child: Align(
      alignment: Alignment.centerLeft,
      child: Text(
        text,
        style: const TextStyle(
          fontSize: 12,
          fontWeight: FontWeight.w700,
          color: AppColors.textSecondary,
        ),
      ),
    ),
  );
}

class _KeyValue extends StatelessWidget {
  final String label;
  final String value;
  const _KeyValue({required this.label, required this.value});
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 7),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 112,
          child: Text(
            label,
            style: const TextStyle(fontSize: 11, color: AppColors.textMuted),
          ),
        ),
        Expanded(
          child: SelectableText(
            value.isEmpty ? '-' : value,
            style: const TextStyle(fontSize: 12, fontFamily: 'monospace'),
          ),
        ),
      ],
    ),
  );
}

class _EmptyState extends StatelessWidget {
  final IconData icon;
  final String title;
  final String detail;
  final VoidCallback? action;
  const _EmptyState({
    required this.icon,
    required this.title,
    required this.detail,
    required this.action,
  });
  @override
  Widget build(BuildContext context) => Center(
    child: ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 360),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 34, color: AppColors.textMuted),
          const SizedBox(height: 10),
          Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
          const SizedBox(height: 5),
          Text(
            detail,
            textAlign: TextAlign.center,
            style: const TextStyle(
              color: AppColors.textSecondary,
              fontSize: 12,
            ),
          ),
          if (action != null) ...[
            const SizedBox(height: 12),
            TextButton.icon(
              onPressed: action,
              icon: const Icon(Icons.refresh),
              label: const Text('重试'),
            ),
          ],
        ],
      ),
    ),
  );
}

IconData _kindIcon(String kind) => switch (kind) {
  'locked_rule' => Icons.rule_outlined,
  'verified_fact' || 'inferred_fact' => Icons.fact_check_outlined,
  'procedure' => Icons.account_tree_outlined,
  'decision' => Icons.gavel_outlined,
  'issue' => Icons.error_outline,
  'episode' => Icons.history_outlined,
  _ => Icons.subject_outlined,
};
String _kindLabel(String kind) => switch (kind) {
  'locked_rule' => '规则',
  'verified_fact' => '已验证事实',
  'inferred_fact' => '推断事实',
  'procedure' => '流程',
  'decision' => '决策',
  'issue' => '问题',
  'episode' => '阶段摘要',
  _ => kind,
};
String _statusLabel(String status) => switch (status) {
  'active' => '当前有效',
  'superseded' => '已替代',
  'disputed' => '有冲突',
  'stale' => '已陈旧',
  'archived' => '已归档',
  'deleted' => '已删除',
  'detached' => '已分离',
  'missing' => '来源缺失',
  _ => status,
};
String _verificationLabel(String status) => switch (status) {
  'verified' => '已验证',
  'failed' => '验证失败',
  'stale' => '验证已陈旧',
  _ => '未验证',
};
