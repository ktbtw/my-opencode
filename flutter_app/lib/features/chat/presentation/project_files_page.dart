import 'dart:convert';
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/notifications/app_notification_controller.dart';
import '../../../core/notifications/app_notification_host.dart';
import '../../../core/notifications/app_notification_model.dart';
import '../../../features/devices/data/device_directory_model.dart';
import '../../../features/devices/presentation/device_provider.dart';
import '../../../shared/widgets/widgets.dart';
import '../data/artifact_download_saver.dart';

class ProjectFilesPage extends ConsumerStatefulWidget {
  final String machineId;
  final String agentId;
  final String projectId;

  const ProjectFilesPage({
    super.key,
    required this.machineId,
    required this.agentId,
    required this.projectId,
  });

  @override
  ConsumerState<ProjectFilesPage> createState() => _ProjectFilesPageState();
}

class _ProjectFilesPageState extends ConsumerState<ProjectFilesPage> {
  bool _loading = true;
  bool _uploading = false;
  bool _creating = false;
  bool _renaming = false;
  bool _deleting = false;
  String _currentPath = '';
  String _parentPath = '';
  String _error = '';
  List<DeviceDirectoryInfo> _entries = const [];
  final Set<String> _downloading = {};
  final Set<String> _selectedPaths = {};
  bool _editing = false;

  @override
  void initState() {
    super.initState();
    _loadPath('');
  }

  Future<void> _loadPath(String path) async {
    setState(() {
      _loading = true;
      _error = '';
      _editing = false;
      _selectedPaths.clear();
    });
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final result = await repo.getDeviceAgentFiles(
        machineId: widget.machineId,
        agentId: widget.agentId,
        path: path,
      );
      if (!mounted) return;
      setState(() {
        _currentPath = result.currentPath;
        _parentPath = result.parentPath;
        _entries = result.entries;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  bool get _busy => _uploading || _creating || _renaming || _deleting;

  List<DeviceDirectoryInfo> get _selectedItems {
    return _entries
        .where((item) => _selectedPaths.contains(item.path))
        .toList(growable: false);
  }

  Future<void> _upload() async {
    if (_uploading) return;
    final picked = await FilePicker.platform.pickFiles(withReadStream: true);
    final file = picked?.files.single;
    final name = file?.name;
    final readStream = file?.readStream;
    final totalBytes = file?.size;
    if (name == null || name.isEmpty || totalBytes == null) return;
    if (readStream == null) {
      if (!mounted) return;
      setState(() => _error = '当前平台无法读取所选文件');
      return;
    }

    final operationId =
        'file:upload:${widget.machineId}:${widget.agentId}:$name:${DateTime.now().microsecondsSinceEpoch}';
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
      stages: _fileTransferStages('准备上传'),
      actions: [_openFilesAction()],
    );

    setState(() {
      _uploading = true;
      _error = '';
    });
    final progress = ValueNotifier<_TransferProgressView>(
      _TransferProgressView(
        transferredBytes: 0,
        totalBytes: totalBytes,
        completedChunks: 0,
        totalChunks: 1,
        stage: '准备上传',
      ),
    );
    final uploadError = ValueNotifier<String>('');
    var dialogOpen = false;
    if (mounted) {
      dialogOpen = true;
      _showTransferProgressDialog(
        filename: name,
        mode: _TransferMode.upload,
        progress: progress,
        error: uploadError,
        onClosed: () => dialogOpen = false,
      );
    }
    try {
      final path = _joinPath(_currentPath, name);
      final repo = ref.read(deviceRepositoryProvider);
      // 分块大小与并发数由服务端按会员等级下发：普通 512KB 单并发，会员 5MB 多并发。
      final policy = await repo.fetchUploadPolicy();
      await repo.uploadDeviceAgentFileChunkedStreamed(
        machineId: widget.machineId,
        agentId: widget.agentId,
        path: path,
        totalBytes: totalBytes,
        openRead: () => readStream,
        chunkSize: policy.chunkSize,
        concurrency: policy.concurrency,
        resume: policy.resumeEnabled,
        onProgress: (value) {
          if (dialogOpen) progress.value = _TransferProgressView.upload(value);
          notifications.update(
            operationId: operationId,
            message: value.stage,
            progress: value.totalBytes <= 0
                ? 0
                : value.uploadedBytes / value.totalBytes,
            stages: _fileTransferStages(value.stage),
          );
        },
      );
      notifications.succeed(
        operationId,
        title: '文件上传完成',
        message: '$name 已上传到项目目录',
        actions: [_openFilesAction()],
      );
      if (!mounted) return;
      dialogOpen = false;
      Navigator.of(context, rootNavigator: true).maybePop();
      await _loadPath(_currentPath);
    } catch (e) {
      notifications.fail(
        operationId,
        title: '文件上传失败',
        error: e,
        actions: [_openFilesAction()],
      );
      if (!mounted) return;
      uploadError.value = e.toString();
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _uploading = false);
    }
  }

  void _showTransferProgressDialog({
    required String filename,
    required _TransferMode mode,
    required ValueNotifier<_TransferProgressView> progress,
    required ValueNotifier<String> error,
    VoidCallback? onClosed,
  }) {
    showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (context) {
        return _TransferProgressDialog(
          filename: filename,
          mode: mode,
          progress: progress,
          error: error,
        );
      },
    ).whenComplete(() {
      onClosed?.call();
      progress.dispose();
      error.dispose();
    });
  }

  Future<void> _download(DeviceDirectoryInfo item) async {
    if (item.isDir || _downloading.contains(item.path)) return;
    final operationId =
        'file:download:${widget.machineId}:${widget.agentId}:${item.name}:${DateTime.now().microsecondsSinceEpoch}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在下载 ${item.name}',
      message: '准备下载文件',
      progressMode: AppNotificationProgressMode.determinate,
      displayStyle: AppNotificationDisplayStyle.linear,
      progress: 0,
      kind: AppNotificationKind.file,
      scope: AppNotificationScope.synced,
      sourceLabel: widget.machineId,
      stages: _fileTransferStages('准备下载'),
      actions: [_openFilesAction()],
    );
    setState(() => _downloading.add(item.path));
    final progress = ValueNotifier<_TransferProgressView>(
      _TransferProgressView(
        transferredBytes: 0,
        totalBytes: item.size,
        completedChunks: 0,
        totalChunks: 1,
        stage: '准备下载',
      ),
    );
    final downloadError = ValueNotifier<String>('');
    var dialogOpen = false;
    String? localSaveOperationId;
    if (mounted) {
      dialogOpen = true;
      _showTransferProgressDialog(
        filename: item.name,
        mode: _TransferMode.download,
        progress: progress,
        error: downloadError,
        onClosed: () => dialogOpen = false,
      );
    }
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final file = await repo.downloadDeviceAgentFileChunked(
        machineId: widget.machineId,
        agentId: widget.agentId,
        path: item.path,
        onProgress: (value) {
          if (dialogOpen) {
            progress.value = _TransferProgressView.download(value);
          }
          notifications.update(
            operationId: operationId,
            message: value.stage,
            progress: value.totalBytes <= 0
                ? 0
                : value.downloadedBytes / value.totalBytes,
            stages: _fileTransferStages(value.stage),
          );
        },
      );
      notifications.succeed(
        operationId,
        title: '设备文件传输完成',
        message: '${item.name} 已从设备传输完成',
        actions: [_openFilesAction()],
      );
      localSaveOperationId = '$operationId:local-save';
      notifications.start(
        operationId: localSaveOperationId,
        title: '正在保存 ${item.name}',
        message: '正在写入当前设备',
        progressMode: AppNotificationProgressMode.indeterminate,
        displayStyle: AppNotificationDisplayStyle.dots,
        kind: AppNotificationKind.file,
        scope: AppNotificationScope.local,
        actions: [_openFilesAction()],
      );
      final bytes = Uint8List.fromList(base64Decode(file.content));
      final saved = await saveArtifactBytes(
        bytes,
        filename: file.name.isNotEmpty ? file.name : item.name,
        mimeType: 'application/octet-stream',
      );
      notifications.succeed(
        localSaveOperationId,
        title: saved == null ? '文件下载已开始' : '文件下载完成',
        message: saved == null ? '${item.name} 已交给浏览器下载' : '${item.name} 已保存',
        actions: [_openFilesAction()],
      );
      if (!mounted) return;
      dialogOpen = false;
      Navigator.of(context, rootNavigator: true).maybePop();
    } catch (e) {
      notifications.fail(
        localSaveOperationId ?? operationId,
        title: localSaveOperationId == null ? '文件下载失败' : '文件保存失败',
        error: e,
        actions: [_openFilesAction()],
      );
      if (!mounted) return;
      downloadError.value = e.toString();
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _downloading.remove(item.path));
    }
  }

  String _validateEntryName(String name) {
    final value = name.trim();
    if (value.isEmpty) return '名称不能为空';
    if (value.contains('/') || value.contains('\\')) return '名称不能包含路径分隔符';
    if (value == '.' || value == '..') return '名称不能为 . 或 ..';
    return '';
  }

  Future<String?> _showNameDialog({
    required String title,
    required String confirmLabel,
    required String hintText,
    String initialValue = '',
  }) async {
    final controller = TextEditingController(text: initialValue);
    try {
      return showDialog<String>(
        context: context,
        builder: (context) {
          String error = '';
          return StatefulBuilder(
            builder: (context, setDialogState) {
              void submit() {
                final value = controller.text.trim();
                final nextError = _validateEntryName(value);
                if (nextError.isNotEmpty) {
                  setDialogState(() => error = nextError);
                  return;
                }
                Navigator.of(context).pop(value);
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
                    onPressed: () => Navigator.of(context).pop(),
                    child: const Text('取消'),
                  ),
                  TextButton(onPressed: submit, child: Text(confirmLabel)),
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

  Future<void> _showCreateMenu() async {
    if (_busy) return;
    final action = await showMenu<_CreateAction>(
      context: context,
      position: const RelativeRect.fromLTRB(1000, 80, 8, 0),
      items: const [
        PopupMenuItem(
          value: _CreateAction.file,
          child: ListTile(
            dense: true,
            contentPadding: EdgeInsets.zero,
            leading: Icon(Icons.note_add_outlined),
            title: Text('新建文件'),
          ),
        ),
        PopupMenuItem(
          value: _CreateAction.folder,
          child: ListTile(
            dense: true,
            contentPadding: EdgeInsets.zero,
            leading: Icon(Icons.create_new_folder_outlined),
            title: Text('新建文件夹'),
          ),
        ),
      ],
    );
    if (!mounted || action == null) return;
    if (action == _CreateAction.file) {
      await _createEmptyFile();
    } else {
      await _createFolder();
    }
  }

  Future<void> _createEmptyFile() async {
    final name = await _showNameDialog(
      title: '新建文件',
      confirmLabel: '创建',
      hintText: '例如 notes.txt',
    );
    if (name == null) return;
    final operationId =
        'file:create:${widget.machineId}:${widget.agentId}:$name:${DateTime.now().microsecondsSinceEpoch}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在创建文件',
      message: '正在创建 $name',
      kind: AppNotificationKind.file,
      scope: AppNotificationScope.synced,
      actions: [_openFilesAction()],
    );
    setState(() {
      _creating = true;
      _error = '';
    });
    try {
      final repo = ref.read(deviceRepositoryProvider);
      await repo.createDeviceAgentEmptyFile(
        machineId: widget.machineId,
        agentId: widget.agentId,
        path: _joinPath(_currentPath, name),
      );
      notifications.succeed(
        operationId,
        title: '文件已创建',
        message: '$name 已添加到项目目录',
        actions: [_openFilesAction()],
      );
      if (!mounted) return;
      await _loadPath(_currentPath);
    } catch (e) {
      notifications.fail(
        operationId,
        title: '文件创建失败',
        error: e,
        actions: [_openFilesAction()],
      );
      if (!mounted) return;
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _creating = false);
    }
  }

  Future<void> _createFolder() async {
    final name = await _showNameDialog(
      title: '新建文件夹',
      confirmLabel: '创建',
      hintText: '例如 docs',
    );
    if (name == null) return;
    final operationId =
        'folder:create:${widget.machineId}:${widget.agentId}:$name:${DateTime.now().microsecondsSinceEpoch}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在创建文件夹',
      message: '正在创建 $name',
      kind: AppNotificationKind.file,
      scope: AppNotificationScope.synced,
      actions: [_openFilesAction()],
    );
    setState(() {
      _creating = true;
      _error = '';
    });
    try {
      final repo = ref.read(deviceRepositoryProvider);
      await repo.createDeviceAgentFolder(
        machineId: widget.machineId,
        agentId: widget.agentId,
        path: _joinPath(_currentPath, name),
      );
      notifications.succeed(
        operationId,
        title: '文件夹已创建',
        message: '$name 已添加到项目目录',
        actions: [_openFilesAction()],
      );
      if (!mounted) return;
      await _loadPath(_currentPath);
    } catch (e) {
      notifications.fail(
        operationId,
        title: '文件夹创建失败',
        error: e,
        actions: [_openFilesAction()],
      );
      if (!mounted) return;
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _creating = false);
    }
  }

  void _enterEditMode(DeviceDirectoryInfo item) {
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
      if (_selectedPaths.isEmpty) {
        _editing = false;
      }
    });
  }

  void _exitEditMode() {
    setState(() {
      _editing = false;
      _selectedPaths.clear();
    });
  }

  Future<void> _renameSelected() async {
    final items = _selectedItems;
    if (items.length != 1 || _busy) return;
    final item = items.first;
    final name = await _showNameDialog(
      title: '重命名',
      confirmLabel: '保存',
      hintText: '请输入新名称',
      initialValue: item.name,
    );
    if (name == null || name == item.name) return;
    final operationId =
        'file:rename:${widget.machineId}:${widget.agentId}:${item.name}:${DateTime.now().microsecondsSinceEpoch}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在重命名',
      message: '${item.name} → $name',
      kind: AppNotificationKind.file,
      scope: AppNotificationScope.synced,
      actions: [_openFilesAction()],
    );
    setState(() {
      _renaming = true;
      _error = '';
    });
    try {
      final repo = ref.read(deviceRepositoryProvider);
      await repo.renameDeviceAgentFile(
        machineId: widget.machineId,
        agentId: widget.agentId,
        path: item.path,
        name: name,
      );
      notifications.succeed(
        operationId,
        title: '重命名完成',
        message: '已重命名为 $name',
        actions: [_openFilesAction()],
      );
      if (!mounted) return;
      await _loadPath(_currentPath);
    } catch (e) {
      notifications.fail(
        operationId,
        title: '重命名失败',
        error: e,
        actions: [_openFilesAction()],
      );
      if (!mounted) return;
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _renaming = false);
    }
  }

  Future<void> _deleteSelected() async {
    final items = _selectedItems;
    if (items.isEmpty || _busy) return;
    final folderCount = items.where((item) => item.isDir).length;
    final ok = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('确认删除'),
        content: Text(
          folderCount > 0
              ? '将删除 ${items.length} 项，其中包含 $folderCount 个文件夹。删除后不可恢复。'
              : '将删除 ${items.length} 个文件。删除后不可恢复。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (ok != true) return;

    final operationId =
        'file:delete:${widget.machineId}:${widget.agentId}:${DateTime.now().microsecondsSinceEpoch}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在删除 ${items.length} 项',
      message: '正在同步项目目录变更',
      kind: AppNotificationKind.file,
      scope: AppNotificationScope.synced,
      actions: [_openFilesAction()],
    );

    setState(() {
      _deleting = true;
      _error = '';
    });
    try {
      final repo = ref.read(deviceRepositoryProvider);
      for (final item in items) {
        await repo.deleteDeviceAgentFile(
          machineId: widget.machineId,
          agentId: widget.agentId,
          path: item.path,
          isDir: item.isDir,
        );
      }
      notifications.succeed(
        operationId,
        title: '删除完成',
        message: '已删除 ${items.length} 项',
        actions: [_openFilesAction()],
      );
      if (!mounted) return;
      await _loadPath(_currentPath);
    } catch (e) {
      notifications.fail(
        operationId,
        title: '删除失败',
        error: e,
        actions: [_openFilesAction()],
      );
      if (!mounted) return;
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _deleting = false);
    }
  }

  String _joinPath(String dir, String name) {
    final cleanName = name
        .replaceAll('\\', '/')
        .split('/')
        .where((e) => e.isNotEmpty)
        .last;
    if (dir.isEmpty) return cleanName;
    return '$dir/$cleanName';
  }

  AppNotificationAction _openFilesAction() {
    final uri = Uri(
      path: '/chat/files',
      queryParameters: {
        'machineId': widget.machineId,
        'agentId': widget.agentId,
        'projectId': widget.projectId,
      },
    );
    return AppNotificationAction(
      label: '查看文件',
      type: AppNotificationActionType.openRoute,
      payload: {'route': uri.toString()},
      primary: true,
    );
  }

  List<AppNotificationStage> _fileTransferStages(String stage) {
    final normalized = stage.trim();
    final activeIndex = normalized.contains('准备')
        ? 0
        : normalized.contains('完成') || normalized.contains('保存')
        ? 2
        : 1;
    return [
      AppNotificationStage(
        label: '准备',
        completed: activeIndex > 0,
        active: activeIndex == 0,
      ),
      AppNotificationStage(
        label: '传输',
        completed: activeIndex > 1,
        active: activeIndex == 1,
      ),
      AppNotificationStage(label: '校验保存', active: activeIndex == 2),
    ];
  }

  String _formatSize(int size) {
    if (size <= 0) return '';
    if (size < 1024) return '$size B';
    if (size < 1024 * 1024) return '${(size / 1024).toStringAsFixed(1)} KB';
    return '${(size / 1024 / 1024).toStringAsFixed(1)} MB';
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.projectId.isEmpty ? '工作目录' : widget.projectId),
        actions: [
          const AppNotificationCenterButton(),
          IconButton(
            tooltip: _uploading ? '上传中' : '上传文件',
            onPressed: _busy ? null : _upload,
            icon: _uploading
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.upload_file_rounded),
          ),
          IconButton(
            tooltip: _creating ? '创建中' : '新建',
            onPressed: _busy ? null : _showCreateMenu,
            icon: _creating
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.add_rounded),
          ),
          IconButton(
            tooltip: '刷新',
            onPressed: _loading || _busy ? null : () => _loadPath(_currentPath),
            icon: const Icon(Icons.refresh_rounded),
          ),
          const SizedBox(width: 8),
        ],
      ),
      body: PageBackground(
        child: _loading
            ? const LoadingState()
            : _error.isNotEmpty
            ? Center(
                child: Text(
                  _error,
                  style: const TextStyle(color: AppColors.statusError),
                ),
              )
            : Stack(
                children: [
                  Column(
                    children: [
                      Container(
                        width: double.infinity,
                        margin: const EdgeInsets.fromLTRB(16, 16, 16, 8),
                        padding: const EdgeInsets.all(14),
                        decoration: BoxDecoration(
                          color: AppColors.surface,
                          borderRadius: AppRadius.lgRadius,
                          border: Border.all(color: AppColors.border),
                        ),
                        child: Text(
                          _currentPath.isEmpty ? '/' : _currentPath,
                          style: Theme.of(context).textTheme.titleSmall,
                        ),
                      ),
                      if (_editing)
                        Container(
                          width: double.infinity,
                          margin: const EdgeInsets.fromLTRB(16, 0, 16, 8),
                          padding: const EdgeInsets.symmetric(
                            horizontal: 12,
                            vertical: 8,
                          ),
                          decoration: BoxDecoration(
                            color: AppColors.primaryLight,
                            borderRadius: AppRadius.mdRadius,
                            border: Border.all(color: AppColors.primaryMuted),
                          ),
                          child: const Text(
                            '已进入编辑模式：点击条目选择，单选可重命名，选中后可删除。',
                            style: TextStyle(
                              fontSize: 12,
                              color: AppColors.textSecondary,
                            ),
                          ),
                        ),
                      Expanded(
                        child: ListView.separated(
                          padding: EdgeInsets.fromLTRB(
                            16,
                            0,
                            16,
                            _editing ? 88 : 0,
                          ),
                          itemCount:
                              _entries.length +
                              (_parentPath.isNotEmpty || _currentPath.isNotEmpty
                                  ? 1
                                  : 0),
                          separatorBuilder: (_, _) => const Divider(height: 1),
                          itemBuilder: (context, index) {
                            final hasParent =
                                _parentPath.isNotEmpty ||
                                _currentPath.isNotEmpty;
                            if (hasParent && index == 0) {
                              return ListTile(
                                leading: const Icon(Icons.arrow_upward_rounded),
                                title: const Text('上一级'),
                                enabled: !_editing && !_busy,
                                onTap: _editing || _busy
                                    ? null
                                    : () => _loadPath(_parentPath),
                              );
                            }
                            final item =
                                _entries[hasParent ? index - 1 : index];
                            final downloading = _downloading.contains(
                              item.path,
                            );
                            final selected = _selectedPaths.contains(item.path);
                            return ListTile(
                              selected: selected,
                              selectedTileColor: AppColors.primaryLight,
                              leading: _editing
                                  ? Icon(
                                      selected
                                          ? Icons.check_circle_rounded
                                          : Icons
                                                .radio_button_unchecked_rounded,
                                      color: selected
                                          ? AppColors.primary
                                          : AppColors.textMuted,
                                    )
                                  : Icon(
                                      item.isDir
                                          ? Icons.folder_rounded
                                          : Icons.description_rounded,
                                      color: item.isDir
                                          ? AppColors.primary
                                          : AppColors.textMuted,
                                    ),
                              title: Text(item.name),
                              subtitle: Text(
                                item.isDir
                                    ? item.path
                                    : [
                                        item.path,
                                        _formatSize(item.size),
                                      ].where((v) => v.isNotEmpty).join(' · '),
                              ),
                              trailing: _editing
                                  ? null
                                  : item.isDir
                                  ? const Icon(Icons.chevron_right_rounded)
                                  : IconButton(
                                      tooltip: downloading ? '下载中' : '下载',
                                      onPressed: downloading || _busy
                                          ? null
                                          : () => _download(item),
                                      icon: downloading
                                          ? const SizedBox(
                                              width: 18,
                                              height: 18,
                                              child: CircularProgressIndicator(
                                                strokeWidth: 2,
                                              ),
                                            )
                                          : const Icon(Icons.download_rounded),
                                    ),
                              onTap: _editing
                                  ? () => _toggleSelection(item)
                                  : item.isDir && !_busy
                                  ? () => _loadPath(item.path)
                                  : null,
                              onLongPress: _busy
                                  ? null
                                  : () => _enterEditMode(item),
                            );
                          },
                        ),
                      ),
                    ],
                  ),
                  if (_editing)
                    Positioned(
                      left: 16,
                      right: 16,
                      bottom: 12,
                      child: _ProjectFilesEditBar(
                        selectedCount: _selectedPaths.length,
                        busy: _busy,
                        canRename: _selectedPaths.length == 1,
                        renaming: _renaming,
                        deleting: _deleting,
                        onCancel: _exitEditMode,
                        onRename: _renameSelected,
                        onDelete: _deleteSelected,
                      ),
                    ),
                ],
              ),
      ),
    );
  }
}

enum _CreateAction { file, folder }

class _ProjectFilesEditBar extends StatelessWidget {
  final int selectedCount;
  final bool busy;
  final bool canRename;
  final bool renaming;
  final bool deleting;
  final VoidCallback onCancel;
  final VoidCallback onRename;
  final VoidCallback onDelete;

  const _ProjectFilesEditBar({
    required this.selectedCount,
    required this.busy,
    required this.canRename,
    required this.renaming,
    required this.deleting,
    required this.onCancel,
    required this.onRename,
    required this.onDelete,
  });

  @override
  Widget build(BuildContext context) {
    return Material(
      color: AppColors.surface,
      elevation: 8,
      shadowColor: AppColors.textPrimary.withValues(alpha: 0.12),
      borderRadius: AppRadius.lgRadius,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 8),
        decoration: BoxDecoration(
          borderRadius: AppRadius.lgRadius,
          border: Border.all(color: AppColors.border),
        ),
        child: Row(
          children: [
            TextButton(
              onPressed: busy ? null : onCancel,
              child: const Text('取消'),
            ),
            Expanded(
              child: Text(
                '已选择 $selectedCount 项',
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(
                  fontSize: 13,
                  color: AppColors.textSecondary,
                ),
              ),
            ),
            TextButton(
              onPressed: busy || !canRename ? null : onRename,
              child: renaming
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Text('重命名'),
            ),
            TextButton(
              onPressed: busy || selectedCount == 0 ? null : onDelete,
              child: deleting
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Text(
                      '删除',
                      style: TextStyle(color: AppColors.statusError),
                    ),
            ),
          ],
        ),
      ),
    );
  }
}

enum _TransferMode { upload, download }

class _TransferProgressView {
  final int transferredBytes;
  final int totalBytes;
  final int completedChunks;
  final int totalChunks;
  final String stage;

  const _TransferProgressView({
    required this.transferredBytes,
    required this.totalBytes,
    required this.completedChunks,
    required this.totalChunks,
    required this.stage,
  });

  factory _TransferProgressView.upload(DeviceUploadProgress value) {
    return _TransferProgressView(
      transferredBytes: value.uploadedBytes,
      totalBytes: value.totalBytes,
      completedChunks: value.completedChunks,
      totalChunks: value.totalChunks,
      stage: value.stage,
    );
  }

  factory _TransferProgressView.download(DeviceDownloadProgress value) {
    return _TransferProgressView(
      transferredBytes: value.downloadedBytes,
      totalBytes: value.totalBytes,
      completedChunks: value.completedChunks,
      totalChunks: value.totalChunks,
      stage: value.stage,
    );
  }

  double get ratio {
    if (totalBytes <= 0) return completedChunks >= totalChunks ? 1 : 0;
    return (transferredBytes / totalBytes).clamp(0, 1);
  }

  int get percent => (ratio * 100).round().clamp(0, 100);
}

class _TransferProgressDialog extends StatelessWidget {
  final String filename;
  final _TransferMode mode;
  final ValueNotifier<_TransferProgressView> progress;
  final ValueNotifier<String> error;

  const _TransferProgressDialog({
    required this.filename,
    required this.mode,
    required this.progress,
    required this.error,
  });

  String _formatSize(int size) {
    if (size <= 0) return '0 B';
    if (size < 1024) return '$size B';
    if (size < 1024 * 1024) return '${(size / 1024).toStringAsFixed(1)} KB';
    return '${(size / 1024 / 1024).toStringAsFixed(1)} MB';
  }

  @override
  Widget build(BuildContext context) {
    return Dialog(
      shape: RoundedRectangleBorder(borderRadius: AppRadius.lgRadius),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 420),
        child: Padding(
          padding: const EdgeInsets.all(18),
          child: ValueListenableBuilder<String>(
            valueListenable: error,
            builder: (context, errorText, _) {
              return ValueListenableBuilder<_TransferProgressView>(
                valueListenable: progress,
                builder: (context, value, _) {
                  final failed = errorText.isNotEmpty;
                  final percent = value.percent;
                  final upload = mode == _TransferMode.upload;
                  return Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Icon(
                            failed
                                ? Icons.error_outline_rounded
                                : upload
                                ? Icons.cloud_upload_outlined
                                : Icons.cloud_download_outlined,
                            color: failed
                                ? AppColors.statusError
                                : AppColors.primary,
                          ),
                          const SizedBox(width: 8),
                          Expanded(
                            child: Text(
                              failed
                                  ? (upload ? '上传失败' : '下载失败')
                                  : (upload ? '上传文件' : '下载文件'),
                              style: Theme.of(context).textTheme.titleLarge,
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 14),
                      Text(
                        filename,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.w600,
                          color: AppColors.textPrimary,
                        ),
                      ),
                      const SizedBox(height: 12),
                      ClipRRect(
                        borderRadius: AppRadius.smRadius,
                        child: LinearProgressIndicator(
                          minHeight: 8,
                          value: value.ratio,
                          backgroundColor: AppColors.borderLight,
                          color: failed
                              ? AppColors.statusError
                              : AppColors.primary,
                        ),
                      ),
                      const SizedBox(height: 10),
                      Row(
                        children: [
                          Expanded(
                            child: Text(
                              failed ? errorText : value.stage,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(
                                fontSize: 12,
                                color: failed
                                    ? AppColors.statusError
                                    : AppColors.textSecondary,
                              ),
                            ),
                          ),
                          const SizedBox(width: 12),
                          Text(
                            '$percent%',
                            style: TextStyle(
                              fontSize: 13,
                              fontWeight: FontWeight.w700,
                              color: failed
                                  ? AppColors.statusError
                                  : AppColors.primary,
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 8),
                      Text(
                        '${_formatSize(value.transferredBytes)} / ${_formatSize(value.totalBytes)} · ${value.completedChunks}/${value.totalChunks} 块',
                        style: const TextStyle(
                          fontSize: 11,
                          color: AppColors.textMuted,
                        ),
                      ),
                      if (failed) ...[
                        const SizedBox(height: 16),
                        Align(
                          alignment: Alignment.centerRight,
                          child: AppButton(
                            label: '关闭',
                            outlined: true,
                            onPressed: () => Navigator.of(context).pop(),
                          ),
                        ),
                      ],
                    ],
                  );
                },
              );
            },
          ),
        ),
      ),
    );
  }
}
