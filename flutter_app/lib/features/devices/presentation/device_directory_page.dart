import 'dart:async';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/services/app_log_service.dart';
import '../../../core/services/feature_guide_service.dart';
import '../../../core/notifications/app_notification_controller.dart';
import '../../../core/notifications/app_notification_model.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import '../../../shared/widgets/step_guide_dialog.dart';
import '../data/device_agent_directory_storage.dart';
import '../data/device_directory_model.dart';
import 'device_provider.dart';

enum _DirectoryCreateAction { file, folder }

class DeviceDirectoryPage extends ConsumerStatefulWidget {
  final String machineId;
  const DeviceDirectoryPage({super.key, required this.machineId});

  @override
  ConsumerState<DeviceDirectoryPage> createState() =>
      _DeviceDirectoryPageState();
}

class _DeviceDirectoryPageState extends ConsumerState<DeviceDirectoryPage> {
  final _pathController = TextEditingController();
  final _searchController = TextEditingController();
  bool _loading = true;
  bool _uploading = false;
  bool _creatingEntry = false;
  bool _deleting = false;
  bool _editing = false;
  bool _guideOpening = false;
  String _creatingPath = '';
  String _currentPath = '';
  String _parentPath = '';
  String _error = '';
  List<DeviceDirectoryInfo> _entries = const [];
  final Set<String> _selectedPaths = {};

  bool get _busy =>
      _loading ||
      _uploading ||
      _creatingEntry ||
      _deleting ||
      _creatingPath.isNotEmpty;

  List<DeviceDirectoryInfo> get _selectedItems => _entries
      .where((item) => _selectedPaths.contains(item.path))
      .toList(growable: false);

  @override
  void initState() {
    super.initState();
    _searchController.addListener(() {
      if (mounted) setState(() {});
    });
    _load();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _showAgentCreateGuide(automatic: true);
    });
  }

  @override
  void dispose() {
    _pathController.dispose();
    _searchController.dispose();
    super.dispose();
  }

  Future<void> _createAgent(DeviceDirectoryInfo item) async {
    final operationId =
        'agent:create:${widget.machineId}:${item.name}:${DateTime.now().microsecondsSinceEpoch}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在创建 Agent',
      message: '正在向 ${widget.machineId} 提交创建任务',
      kind: AppNotificationKind.agent,
      scope: AppNotificationScope.synced,
      actions: [
        AppNotificationAction(
          label: '查看设备',
          type: AppNotificationActionType.openRoute,
          payload: {'route': '/devices/${widget.machineId}'},
          primary: true,
        ),
      ],
    );
    setState(() {
      _creatingPath = item.path;
      _error = '';
    });
    try {
      await AppLogService.log(
        'device_agent_create_confirmed',
        data: {
          'machine_id': widget.machineId,
          'name': item.name,
          'project_dir': item.path,
        },
      );
      final repo = ref.read(deviceRepositoryProvider);
      await repo.createDeviceAgent(
        machineId: widget.machineId,
        name: item.name,
        projectDir: item.path,
      );
      notifications.succeed(
        operationId,
        title: 'Agent 创建任务已启动',
        message: '${item.name} 已提交到设备',
      );
      if (!mounted) return;
      Navigator.of(context).pop(true);
    } catch (e) {
      notifications.fail(operationId, title: 'Agent 创建失败', error: e);
      await AppLogService.log(
        'device_agent_create_failed',
        level: 'error',
        data: {
          'machine_id': widget.machineId,
          'name': item.name,
          'project_dir': item.path,
          'error': e.toString(),
        },
      );
      setState(() {
        _error = e.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _creatingPath = '';
        });
      }
    }
  }

  Future<void> _load() async {
    final savedPath = DeviceAgentDirectoryStorage.getLastPath(widget.machineId);
    await _loadPath(savedPath, fallbackToRoot: savedPath.isNotEmpty);
  }

  Future<void> _loadPath(String path, {bool fallbackToRoot = false}) async {
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      unawaited(
        AppLogService.log(
          'device_directories_load_started',
          data: {'machine_id': widget.machineId, 'path': path},
        ),
      );
      final repo = ref.read(deviceRepositoryProvider);
      final result = await repo.getDeviceDirectories(
        machineId: widget.machineId,
        path: path,
      );
      unawaited(
        AppLogService.log(
          'device_directories_load_completed',
          data: {
            'machine_id': widget.machineId,
            'path': result.currentPath,
            'entry_count': result.entries.length,
          },
        ),
      );
      await DeviceAgentDirectoryStorage.setLastPath(
        widget.machineId,
        result.currentPath,
      );
      if (!mounted) return;
      setState(() {
        _currentPath = result.currentPath;
        _parentPath = result.parentPath;
        _entries = result.entries;
        _pathController.text = result.currentPath;
      });
    } catch (e) {
      if (fallbackToRoot && path.trim().isNotEmpty) {
        await AppLogService.log(
          'device_directories_restore_failed_fallback_root',
          level: 'warning',
          data: {
            'machine_id': widget.machineId,
            'path': path,
            'error': e.toString(),
          },
        );
        if (!mounted) return;
        await _loadPath('');
        return;
      }
      if (!mounted) return;
      setState(() {
        _error = e.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  String get _selectedCreatePath => _currentPath.isNotEmpty ? _currentPath : '';

  List<DeviceDirectoryInfo> get _visibleEntries {
    final query = _searchController.text.trim().toLowerCase();
    if (query.isEmpty) return _entries;
    return _entries.where((item) {
      return item.name.toLowerCase().contains(query) ||
          item.path.toLowerCase().contains(query);
    }).toList();
  }

  Future<void> _jumpToInputPath() async {
    final path = _pathController.text.trim();
    FocusScope.of(context).unfocus();
    await _loadPath(path);
  }

  Future<void> _createAgentHere() async {
    if (_selectedCreatePath.isEmpty) return;
    final name = _selectedCreatePath.split('/').where((e) => e.isNotEmpty).last;
    final ok = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('创建 Agent'),
        content: Text('确认在当前目录创建 Agent 吗？\n\n$_selectedCreatePath'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('确认'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    await _createAgent(
      DeviceDirectoryInfo(
        path: _selectedCreatePath,
        name: name,
        kind: '目录',
        isDir: true,
      ),
    );
  }

  String _joinPath(String directory, String name) {
    final separator = directory.contains('\\') && !directory.contains('/')
        ? '\\'
        : '/';
    if (directory.endsWith('/') || directory.endsWith('\\')) {
      return '$directory$name';
    }
    return '$directory$separator$name';
  }

  String _validateEntryName(String name) {
    final value = name.trim();
    if (value.isEmpty) return '名称不能为空';
    if (value.startsWith('.')) return '名称不能以句点开头';
    if (value.contains('/') || value.contains('\\')) return '名称不能包含路径分隔符';
    if (value == '.' || value == '..') return '名称不能为 . 或 ..';
    return '';
  }

  Future<String?> _showNameDialog({
    required String title,
    required String hintText,
  }) async {
    final controller = TextEditingController();
    try {
      return showDialog<String>(
        context: context,
        builder: (dialogContext) {
          var error = '';
          return StatefulBuilder(
            builder: (context, setDialogState) {
              void submit() {
                final value = controller.text.trim();
                final nextError = _validateEntryName(value);
                if (nextError.isNotEmpty) {
                  setDialogState(() => error = nextError);
                  return;
                }
                Navigator.of(dialogContext).pop(value);
              }

              return AlertDialog(
                title: Text(title),
                content: TextField(
                  controller: controller,
                  autofocus: true,
                  textInputAction: TextInputAction.done,
                  decoration: InputDecoration(
                    hintText: hintText,
                    errorText: error.isEmpty ? null : error,
                  ),
                  onSubmitted: (_) => submit(),
                ),
                actions: [
                  TextButton(
                    onPressed: () => Navigator.of(dialogContext).pop(),
                    child: const Text('取消'),
                  ),
                  FilledButton(onPressed: submit, child: const Text('创建')),
                ],
              );
            },
          );
        },
      );
    } finally {
      controller.dispose();
    }
  }

  Future<void> _createEntry(_DirectoryCreateAction action) async {
    if (_currentPath.isEmpty || _busy) return;
    final isFolder = action == _DirectoryCreateAction.folder;
    final name = await _showNameDialog(
      title: isFolder ? '新建文件夹' : '新建文件',
      hintText: isFolder ? '例如 docs' : '例如 notes.txt',
    );
    if (name == null || !mounted) return;
    final operationId =
        'directory:create:${widget.machineId}:$name:${DateTime.now().microsecondsSinceEpoch}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: isFolder ? '正在创建文件夹' : '正在创建文件',
      message: name,
      progressMode: AppNotificationProgressMode.indeterminate,
      displayStyle: AppNotificationDisplayStyle.dots,
      kind: AppNotificationKind.file,
      scope: AppNotificationScope.synced,
      sourceLabel: widget.machineId,
    );
    setState(() => _creatingEntry = true);
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final path = _joinPath(_currentPath, name);
      if (isFolder) {
        await repo.createDeviceDirectoryFolder(
          machineId: widget.machineId,
          path: path,
        );
      } else {
        await repo.createDeviceDirectoryEmptyFile(
          machineId: widget.machineId,
          path: path,
        );
      }
      notifications.succeed(
        operationId,
        title: isFolder ? '文件夹已创建' : '文件已创建',
        message: name,
      );
      if (!mounted) return;
      await _loadPath(_currentPath);
    } catch (error) {
      notifications.fail(
        operationId,
        title: isFolder ? '文件夹创建失败' : '文件创建失败',
        error: error,
      );
    } finally {
      if (mounted) setState(() => _creatingEntry = false);
    }
  }

  Future<void> _uploadFile() async {
    if (_currentPath.isEmpty || _busy) return;
    final picked = await FilePicker.platform.pickFiles(withReadStream: true);
    final file = picked?.files.single;
    final name = file?.name.trim() ?? '';
    final readStream = file?.readStream;
    final totalBytes = file?.size;
    if (name.isEmpty || readStream == null || totalBytes == null) return;
    final nameError = _validateEntryName(name);
    if (nameError.isNotEmpty) return;

    final operationId =
        'directory:upload:${widget.machineId}:$name:${DateTime.now().microsecondsSinceEpoch}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在上传 $name',
      message: '准备上传文件',
      progressMode: AppNotificationProgressMode.determinate,
      displayStyle: AppNotificationDisplayStyle.linear,
      progress: 0,
      kind: AppNotificationKind.file,
      scope: AppNotificationScope.synced,
      sourceLabel: widget.machineId,
    );
    setState(() => _uploading = true);
    try {
      final repo = ref.read(deviceRepositoryProvider);
      // 分块大小与并发数由服务端按会员等级下发：普通 512KB 单并发，会员 5MB 多并发。
      final policy = await repo.fetchUploadPolicy();
      await repo.uploadDeviceDirectoryFileChunkedStreamed(
            machineId: widget.machineId,
            path: _joinPath(_currentPath, name),
            totalBytes: totalBytes,
            openRead: () => readStream,
            chunkSize: policy.chunkSize,
            concurrency: policy.concurrency,
            resume: policy.resumeEnabled,
            onProgress: (value) {
              notifications.update(
                operationId: operationId,
                message: value.stage,
                progress: value.totalBytes <= 0
                    ? 0
                    : value.uploadedBytes / value.totalBytes,
              );
            },
          );
      notifications.succeed(operationId, title: '文件上传完成', message: name);
      if (!mounted) return;
      await _loadPath(_currentPath);
    } catch (error) {
      notifications.fail(operationId, title: '文件上传失败', error: error);
    } finally {
      if (mounted) setState(() => _uploading = false);
    }
  }

  void _enterEditMode(DeviceDirectoryInfo item) {
    if (_currentPath.isEmpty || _busy) return;
    setState(() {
      _editing = true;
      _selectedPaths
        ..clear()
        ..add(item.path);
    });
  }

  void _toggleSelection(DeviceDirectoryInfo item) {
    if (!_editing) return;
    setState(() {
      if (_selectedPaths.contains(item.path)) {
        _selectedPaths.remove(item.path);
      } else {
        _selectedPaths.add(item.path);
      }
      if (_selectedPaths.isEmpty) _editing = false;
    });
  }

  void _exitEditMode() {
    setState(() {
      _editing = false;
      _selectedPaths.clear();
    });
  }

  Future<void> _showAgentCreateGuide({bool automatic = false}) async {
    if (_guideOpening || !mounted) return;
    if (automatic &&
        await FeatureGuideService.hasSeen(
          FeatureGuideService.agentCreateGuideKey,
        )) {
      return;
    }
    if (!mounted) return;
    _guideOpening = true;
    await showStepGuideDialog(
      context,
      title: '创建 Agent 指南',
      subtitle: 'Agent 会绑定一个项目目录，创建后就可以从设备详情页进入对话。',
      finalActionLabel: '开始选择目录',
      steps: const [
        GuideStep(
          title: '进入项目目录',
          description: '使用上方路径输入框、返回按钮或目录列表，进入你准备交给 Agent 管理的项目根目录。',
          icon: Icons.folder_open_outlined,
          details: [
            '建议选择项目的根目录，而不是临时文件夹或单个文件。',
            '当前路径会显示在顶部，创建 Agent 时使用的就是这个当前目录。',
          ],
        ),
        GuideStep(
          title: '点击创建 Agent',
          description: '确认当前路径正确后，点击右上角的“创建 Agent”图标。Agent 名称默认使用当前项目目录名。',
          icon: Icons.add_box_outlined,
          details: [
            '创建入口只在当前目录有效；如果按钮不可用，先完成目录加载或进入一个有效目录。',
            '默认名称可以在设备详情页的 Agent 卡片中修改。',
          ],
        ),
        GuideStep(
          title: '确认并等待设备处理',
          description: '确认项目路径后，系统会把创建任务提交给设备上的 Launcher。',
          icon: Icons.task_alt_outlined,
          details: [
            '提交成功后会出现“Agent 创建任务已启动”的通知。',
            '创建期间不要重复点击；可以从通知中的“查看设备”进入设备详情页。',
          ],
        ),
        GuideStep(
          title: '配置并开始对话',
          description: '创建完成后，在 Agent 卡片中检查模型、MCP、Skill 等配置，然后进入对话页面开始工作。',
          icon: Icons.forum_outlined,
          details: [
            '如果设备还没有模型，先完成设备 AI 配置，再进入 Agent 对话。',
            'Agent 离线时先检查 Launcher 状态，不要连续重复创建同一个目录。',
          ],
        ),
      ],
    );
    await FeatureGuideService.markSeen(FeatureGuideService.agentCreateGuideKey);
    if (mounted) _guideOpening = false;
  }

  Future<void> _deleteSelected() async {
    final items = _selectedItems;
    if (items.isEmpty || _busy) return;
    final folderCount = items.where((item) => item.isDir).length;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('确认删除'),
        content: Text(
          folderCount > 0
              ? '将递归删除 ${items.length} 项，其中包含 $folderCount 个文件夹。删除后不可恢复。'
              : '将删除 ${items.length} 个文件。删除后不可恢复。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    final operationId =
        'directory:delete:${widget.machineId}:${DateTime.now().microsecondsSinceEpoch}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在删除 ${items.length} 项',
      message: '正在同步 Launcher 目录',
      progressMode: AppNotificationProgressMode.determinate,
      displayStyle: AppNotificationDisplayStyle.linear,
      progress: 0,
      kind: AppNotificationKind.file,
      scope: AppNotificationScope.synced,
      sourceLabel: widget.machineId,
    );
    setState(() => _deleting = true);
    try {
      final repo = ref.read(deviceRepositoryProvider);
      for (var index = 0; index < items.length; index++) {
        final item = items[index];
        await repo.deleteDeviceDirectoryFile(
          machineId: widget.machineId,
          path: item.path,
          isDir: item.isDir,
        );
        notifications.update(
          operationId: operationId,
          message: '已删除 ${index + 1}/${items.length}',
          progress: (index + 1) / items.length,
        );
      }
      notifications.succeed(
        operationId,
        title: '删除完成',
        message: '已删除 ${items.length} 项',
      );
      if (!mounted) return;
      _exitEditMode();
      await _loadPath(_currentPath);
    } catch (error) {
      notifications.fail(operationId, title: '删除失败', error: error);
      if (mounted) await _loadPath(_currentPath);
    } finally {
      if (mounted) setState(() => _deleting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final creatingCurrent =
        _creatingPath.isNotEmpty && _creatingPath == _selectedCreatePath;
    return Scaffold(
      appBar: AppBar(
        title: Text(_editing ? '已选择 ${_selectedPaths.length} 项' : '设备目录'),
        actions: _editing
            ? [
                IconButton(
                  tooltip: _deleting ? '删除中' : '删除',
                  onPressed: _deleting ? null : _deleteSelected,
                  icon: _deleting
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.delete_outline_rounded),
                ),
                IconButton(
                  tooltip: '退出编辑',
                  onPressed: _deleting ? null : _exitEditMode,
                  icon: const Icon(Icons.close_rounded),
                ),
                const SizedBox(width: 8),
              ]
            : [
                IconButton(
                  tooltip: '创建 Agent 指南',
                  onPressed: _busy ? null : _showAgentCreateGuide,
                  icon: const Icon(Icons.help_outline_rounded),
                ),
                IconButton(
                  tooltip: _uploading ? '上传中' : '上传文件',
                  onPressed: _currentPath.isEmpty || _busy ? null : _uploadFile,
                  icon: _uploading
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.upload_file_rounded),
                ),
                PopupMenuButton<_DirectoryCreateAction>(
                  tooltip: _creatingEntry ? '创建中' : '新建',
                  enabled: _currentPath.isNotEmpty && !_busy,
                  icon: _creatingEntry
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.add_rounded),
                  onSelected: _createEntry,
                  itemBuilder: (_) => const [
                    PopupMenuItem(
                      value: _DirectoryCreateAction.file,
                      child: ListTile(
                        dense: true,
                        contentPadding: EdgeInsets.zero,
                        leading: Icon(Icons.note_add_outlined),
                        title: Text('新建文件'),
                      ),
                    ),
                    PopupMenuItem(
                      value: _DirectoryCreateAction.folder,
                      child: ListTile(
                        dense: true,
                        contentPadding: EdgeInsets.zero,
                        leading: Icon(Icons.create_new_folder_outlined),
                        title: Text('新建文件夹'),
                      ),
                    ),
                  ],
                ),
                IconButton(
                  tooltip: creatingCurrent ? '创建中...' : '创建 Agent',
                  onPressed: _selectedCreatePath.isEmpty || _busy
                      ? null
                      : _createAgentHere,
                  icon: creatingCurrent
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.add_box_rounded),
                ),
                const SizedBox(width: 8),
              ],
      ),
      body: PageBackground(
        child: Column(
          children: [
            _DirectoryToolbar(
              pathController: _pathController,
              searchController: _searchController,
              loading: _loading,
              onJump: _jumpToInputPath,
              onRoot: () => _loadPath(''),
            ),
            Expanded(
              child: _loading
                  ? const LoadingState()
                  : _error.isNotEmpty
                  ? _DirectoryError(
                      message: _error,
                      onRetry: () => _loadPath(_pathController.text.trim()),
                    )
                  : _DirectoryList(
                      entries: _visibleEntries,
                      parentPath: _parentPath,
                      searchText: _searchController.text,
                      editing: _editing,
                      selectedPaths: _selectedPaths,
                      allowEditing: _currentPath.isNotEmpty && !_busy,
                      onOpen: _loadPath,
                      onToggle: _toggleSelection,
                      onLongPress: _enterEditMode,
                    ),
            ),
          ],
        ),
      ),
    );
  }
}

class _DirectoryToolbar extends StatelessWidget {
  final TextEditingController pathController;
  final TextEditingController searchController;
  final bool loading;
  final Future<void> Function() onJump;
  final VoidCallback onRoot;

  const _DirectoryToolbar({
    required this.pathController,
    required this.searchController,
    required this.loading,
    required this.onJump,
    required this.onRoot,
  });

  @override
  Widget build(BuildContext context) {
    final isMobile = AppBreakpoints.isMobile(context);
    final pathField = TextField(
      controller: pathController,
      enabled: !loading,
      textInputAction: TextInputAction.go,
      onSubmitted: (_) => onJump(),
      decoration: const InputDecoration(
        prefixIcon: Icon(Icons.route_rounded),
        hintText: '输入路径定位，例如 C:\\Users 或 /Users/name',
      ),
    );
    final searchField = TextField(
      controller: searchController,
      enabled: !loading,
      decoration: InputDecoration(
        prefixIcon: const Icon(Icons.search_rounded),
        hintText: '搜索当前目录',
        suffixIcon: searchController.text.isEmpty
            ? null
            : IconButton(
                tooltip: '清空搜索',
                onPressed: searchController.clear,
                icon: const Icon(Icons.close_rounded),
              ),
      ),
    );
    final actions = Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        IconButton(
          tooltip: '定位路径',
          onPressed: loading ? null : onJump,
          icon: const Icon(Icons.subdirectory_arrow_right_rounded),
        ),
        IconButton(
          tooltip: '回到根节点',
          onPressed: loading ? null : onRoot,
          icon: const Icon(Icons.home_rounded),
        ),
      ],
    );
    return Container(
      margin: const EdgeInsets.fromLTRB(16, 16, 16, 8),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: AppColors.surface,
        borderRadius: AppRadius.lgRadius,
        border: Border.all(color: AppColors.border),
      ),
      child: isMobile
          ? Column(
              children: [
                pathField,
                const SizedBox(height: 10),
                searchField,
                const SizedBox(height: 8),
                Align(alignment: Alignment.centerRight, child: actions),
              ],
            )
          : Row(
              children: [
                Expanded(flex: 3, child: pathField),
                const SizedBox(width: 10),
                Expanded(flex: 2, child: searchField),
                const SizedBox(width: 4),
                actions,
              ],
            ),
    );
  }
}

class _DirectoryList extends StatelessWidget {
  final List<DeviceDirectoryInfo> entries;
  final String parentPath;
  final String searchText;
  final Future<void> Function(String path) onOpen;
  final bool editing;
  final Set<String> selectedPaths;
  final bool allowEditing;
  final ValueChanged<DeviceDirectoryInfo> onToggle;
  final ValueChanged<DeviceDirectoryInfo> onLongPress;

  const _DirectoryList({
    required this.entries,
    required this.parentPath,
    required this.searchText,
    required this.onOpen,
    required this.editing,
    required this.selectedPaths,
    required this.allowEditing,
    required this.onToggle,
    required this.onLongPress,
  });

  @override
  Widget build(BuildContext context) {
    if (entries.isEmpty && searchText.trim().isNotEmpty) {
      return const EmptyState(
        message: '当前目录没有匹配结果',
        icon: Icons.search_off_rounded,
      );
    }
    if (entries.isEmpty && parentPath.isEmpty) {
      return const EmptyState(message: '当前目录为空', icon: Icons.folder_off);
    }
    return ListView.separated(
      padding: const EdgeInsets.symmetric(horizontal: 16),
      itemCount: entries.length + (parentPath.isNotEmpty ? 1 : 0),
      separatorBuilder: (_, _) => const Divider(height: 1),
      itemBuilder: (context, index) {
        if (parentPath.isNotEmpty && index == 0) {
          return ListTile(
            leading: const Icon(Icons.arrow_upward_rounded),
            title: const Text('上一级'),
            onTap: () => onOpen(parentPath),
          );
        }
        final item = entries[parentPath.isNotEmpty ? index - 1 : index];
        final selected = selectedPaths.contains(item.path);
        return ListTile(
          selected: selected,
          leading: Icon(
            item.isDir ? Icons.folder_rounded : Icons.description_rounded,
            color: item.isDir ? AppColors.primary : AppColors.textMuted,
          ),
          title: Text(item.name),
          subtitle: Text(item.path),
          trailing: editing
              ? Checkbox(value: selected, onChanged: (_) => onToggle(item))
              : item.isDir
              ? const Icon(Icons.chevron_right_rounded)
              : null,
          onTap: editing
              ? () => onToggle(item)
              : item.isDir
              ? () => onOpen(item.path)
              : null,
          onLongPress: allowEditing ? () => onLongPress(item) : null,
        );
      },
    );
  }
}

class _DirectoryError extends StatelessWidget {
  final String message;
  final VoidCallback onRetry;

  const _DirectoryError({required this.message, required this.onRetry});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(
              Icons.error_outline_rounded,
              size: 40,
              color: AppColors.statusError,
            ),
            const SizedBox(height: 12),
            Text(
              message,
              style: const TextStyle(color: AppColors.statusError),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 16),
            AppButton(
              label: '重试',
              icon: Icons.refresh_rounded,
              outlined: true,
              onPressed: onRetry,
              width: 120,
            ),
          ],
        ),
      ),
    );
  }
}
