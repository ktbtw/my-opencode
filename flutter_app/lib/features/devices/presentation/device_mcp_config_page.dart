import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/config/api_client.dart';
import '../../../core/notifications/app_notification_feedback.dart';
import '../../../core/notifications/app_notification_controller.dart';
import '../../../core/notifications/app_notification_model.dart';
import '../../../core/services/app_log_service.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import '../../chat/data/chat_model.dart';
import '../data/device_mcp_config_model.dart';
import '../data/device_mcp_import_parser.dart';
import 'device_provider.dart';
import 'device_mcp_store_page.dart';
import 'widgets/raw_config_dialog.dart';

class DeviceMCPConfigPage extends ConsumerStatefulWidget {
  final String machineId;
  final String agentId;
  final String projectId;

  const DeviceMCPConfigPage({
    super.key,
    required this.machineId,
    required this.agentId,
    required this.projectId,
  });

  @override
  ConsumerState<DeviceMCPConfigPage> createState() =>
      _DeviceMCPConfigPageState();
}

class _DeviceMCPConfigPageState extends ConsumerState<DeviceMCPConfigPage> {
  DeviceMCPConfigInfo? _config;
  bool _loading = true;
  bool _saving = false;
  bool _removing = false;
  bool _statusLoading = false;
  bool _routeMissing = false;
  String _error = '';
  MCPStatusInfo? _status;

  bool get _agentMode => widget.agentId.trim().isNotEmpty;

  String get _pageTitle => _agentMode ? 'Agent MCP' : '设备级全局配置';

  String get _pageSubtitle => _agentMode
      ? (widget.projectId.trim().isNotEmpty
            ? widget.projectId.trim()
            : widget.agentId.trim())
      : 'Machine ID: ${widget.machineId}';

  String get _pageHint => _agentMode
      ? '这里展示当前 Agent 的 MCP 运行状态；配置仍写入设备全局 opencode mcp 节点，保存后会影响该设备上的所有 agent。'
      : '当前页面操作的是这台设备唯一的全局 opencode 配置文件中的 mcp 节点，会影响该设备上的所有 agent。';

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didUpdateWidget(covariant DeviceMCPConfigPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.machineId != widget.machineId ||
        oldWidget.agentId != widget.agentId) {
      _config = null;
      _status = null;
      _load();
    }
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = '';
      _routeMissing = false;
      _status = null;
      _statusLoading = false;
    });
    try {
      await AppLogService.log(
        'device_mcp_config_load_started',
        data: {'machine_id': widget.machineId},
      );
      final repo = ref.read(deviceRepositoryProvider);
      final config = await repo.getDeviceMCPConfig(widget.machineId);
      await AppLogService.log(
        'device_mcp_config_load_completed',
        data: {
          'machine_id': widget.machineId,
          'server_count': config.servers.length,
        },
      );
      if (!mounted) return;
      final loadStatus = shouldLoadAgentMCPStatus(
        agentMode: _agentMode,
        config: config,
      );
      setState(() {
        _config = config;
        _routeMissing = false;
        if (_agentMode && !loadStatus) {
          _status = null;
          _statusLoading = false;
        }
      });
      if (loadStatus) {
        await _loadStatus();
      }
    } catch (error) {
      final routeMissing = _isRouteMissingError(error);
      await AppLogService.log(
        'device_mcp_config_load_failed',
        level: 'error',
        data: {'machine_id': widget.machineId, 'error': error.toString()},
      );
      if (!mounted) return;
      setState(() {
        _config = null;
        _routeMissing = routeMissing;
        _error = _readableError(error);
      });
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  Future<void> _loadStatus() async {
    if (!_agentMode) return;
    if (!shouldLoadAgentMCPStatus(agentMode: _agentMode, config: _config)) {
      setState(() {
        _status = null;
        _statusLoading = false;
        if (_error.startsWith('读取 Agent MCP 状态失败')) {
          _error = '';
        }
      });
      return;
    }
    setState(() {
      _statusLoading = true;
    });
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final status = await repo.getDeviceAgentMCPStatus(
        machineId: widget.machineId,
        agentId: widget.agentId,
      );
      if (!mounted) return;
      setState(() {
        _status = status;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _status = null;
        _error = _error.isEmpty ? '读取 Agent MCP 状态失败：$error' : _error;
      });
    } finally {
      if (mounted) {
        setState(() {
          _statusLoading = false;
        });
      }
    }
  }

  Future<void> _restartCurrentAgent() async {
    if (!_agentMode) return;
    final operationId = 'mcp:restart:${widget.machineId}:${widget.agentId}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在重启 Agent',
      message: '正在重新加载 MCP 配置',
      kind: AppNotificationKind.mcp,
      scope: AppNotificationScope.synced,
      sourceLabel: widget.machineId,
    );
    try {
      final repo = ref.read(deviceRepositoryProvider);
      await repo.restartDeviceAgent(
        machineId: widget.machineId,
        agentId: widget.agentId,
      );
      notifications.succeed(
        operationId,
        title: 'MCP 配置已重新加载',
        message: 'Agent 已重启，正在读取工具状态',
      );
      if (!mounted) return;
      await _loadStatus();
    } catch (error) {
      notifications.fail(operationId, title: 'MCP 配置重载失败', error: error);
    }
  }

  Future<void> _saveServers(
    List<DeviceMCPServerInfo> servers, {
    String successMessage = 'MCP配置已同步到设备',
  }) async {
    final operationId = 'mcp:save:${widget.machineId}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在同步 MCP 配置',
      message: '正在写入 ${servers.length} 个 MCP 服务',
      kind: AppNotificationKind.mcp,
      scope: AppNotificationScope.synced,
      sourceLabel: widget.machineId,
    );
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final config = await repo.saveDeviceMCPConfig(
        machineId: widget.machineId,
        servers: servers,
      );
      notifications.succeed(
        operationId,
        title: 'MCP 配置同步完成',
        message: successMessage,
      );
      await AppLogService.log(
        'device_mcp_config_saved',
        data: {'machine_id': widget.machineId, 'server_count': servers.length},
      );
      if (!mounted) return;
      final loadStatus = shouldLoadAgentMCPStatus(
        agentMode: _agentMode,
        config: config,
      );
      setState(() {
        _config = config;
        _routeMissing = false;
        if (_agentMode && !loadStatus) {
          _status = null;
          _statusLoading = false;
        }
      });
    } catch (error) {
      notifications.fail(operationId, title: 'MCP 配置同步失败', error: error);
      final routeMissing = _isRouteMissingError(error);
      await AppLogService.log(
        'device_mcp_config_save_failed',
        level: 'error',
        data: {'machine_id': widget.machineId, 'error': error.toString()},
      );
      if (!mounted) return;
      setState(() {
        _routeMissing = routeMissing;
        _error = _readableError(error);
      });
    } finally {
      if (mounted) {
        setState(() {
          _saving = false;
        });
      }
    }
  }

  Future<void> _editServer({
    DeviceMCPServerInfo? current,
    String type = 'remote',
  }) async {
    final next = await showDialog<DeviceMCPServerInfo>(
      context: context,
      builder: (context) => _MCPServerDialog(
        initial: current,
        defaultType: current?.type ?? type,
      ),
    );
    if (next == null || _config == null) return;
    final servers = List<DeviceMCPServerInfo>.from(_config!.servers);
    final index = servers.indexWhere((item) => item.name == current?.name);
    if (index >= 0) {
      servers[index] = next;
    } else {
      servers.add(next);
      servers.sort((left, right) => left.name.compareTo(right.name));
    }
    await _saveServers(
      servers,
      successMessage: current == null ? 'MCP已添加' : 'MCP已更新',
    );
  }

  Future<void> _importServers() async {
    if (_config == null) return;
    final result = await showDialog<DeviceMCPImportResult>(
      context: context,
      builder: (context) => const _MCPImportDialog(),
    );
    if (result == null) return;
    await _saveServers(
      _mergeServers(result.servers),
      successMessage: result.skippedNames.isEmpty
          ? '已导入 ${result.servers.length} 个 MCP'
          : '已导入 ${result.servers.length} 个 MCP，忽略 ${result.skippedNames.length} 个无法识别项',
    );
  }

  Future<void> _openStore() async {
    if (_config == null) return;
    final servers = await Navigator.of(context).push<List<DeviceMCPServerInfo>>(
      MaterialPageRoute(
        builder: (context) =>
            DeviceMCPStorePage(existingServers: _config!.servers),
      ),
    );
    if (servers == null || servers.isEmpty) return;
    await _saveServers(
      _mergeServers(servers),
      successMessage: '已从商店添加 ${servers.length} 个 MCP',
    );
  }

  Future<void> _openRawEditor() async {
    final cfg = _config;
    if (cfg == null) return;
    final text = await showDialog<String>(
      context: context,
      builder: (context) => RawConfigDialog(
        path: cfg.configPath,
        warning: cfg.warning,
        initialText: cfg.rawJson.isEmpty ? '{}' : cfg.rawJson,
      ),
    );
    if (text == null) return;
    await _saveRawText(text);
  }

  Future<void> _saveRawText(String text) async {
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      await AppLogService.log(
        'device_mcp_config_text_save_started',
        data: {'machine_id': widget.machineId, 'text_length': text.length},
      );
      final repo = ref.read(deviceRepositoryProvider);
      await repo.saveDeviceAIConfigText(
        machineId: widget.machineId,
        text: text,
      );
      final config = await repo.getDeviceMCPConfig(widget.machineId);
      await AppLogService.log(
        'device_mcp_config_text_save_completed',
        data: {
          'machine_id': widget.machineId,
          'server_count': config.servers.length,
        },
      );
      if (!mounted) return;
      final loadStatus = shouldLoadAgentMCPStatus(
        agentMode: _agentMode,
        config: config,
      );
      setState(() {
        _config = config;
        _routeMissing = false;
        if (_agentMode && !loadStatus) {
          _status = null;
          _statusLoading = false;
        }
      });
      showAppFeedback(context, message: '全局配置文件已保存');
    } catch (error) {
      final routeMissing = _isRouteMissingError(error);
      await AppLogService.log(
        'device_mcp_config_text_save_failed',
        level: 'error',
        data: {'machine_id': widget.machineId, 'error': error.toString()},
      );
      if (!mounted) return;
      setState(() {
        _routeMissing = routeMissing;
        _error = _readableError(error);
      });
    } finally {
      if (mounted) {
        setState(() {
          _saving = false;
        });
      }
    }
  }

  Future<void> _toggleServerEnabled(
    DeviceMCPServerInfo server,
    bool enabled,
  ) async {
    if (_config == null) return;
    final servers = List<DeviceMCPServerInfo>.from(_config!.servers);
    final index = servers.indexWhere((item) => item.name == server.name);
    if (index < 0) return;
    servers[index] = servers[index].copyWith(enabled: enabled);
    await _saveServers(servers, successMessage: enabled ? 'MCP已启用' : 'MCP已停用');
  }

  Future<void> _removeServer(DeviceMCPServerInfo server) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除 MCP'),
        content: Text('确定删除“${server.name}”吗？'),
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
    if (confirmed != true) return;
    final operationId = 'mcp:remove:${widget.machineId}:${server.name}';
    final notifications = ref.read(appNotificationControllerProvider.notifier);
    notifications.start(
      operationId: operationId,
      title: '正在删除 MCP',
      message: '正在从设备配置移除 ${server.name}',
      kind: AppNotificationKind.mcp,
      scope: AppNotificationScope.synced,
      sourceLabel: widget.machineId,
    );
    setState(() {
      _removing = true;
      _error = '';
    });
    try {
      final repo = ref.read(deviceRepositoryProvider);
      final config = await repo.removeDeviceMCPConfig(
        machineId: widget.machineId,
        name: server.name,
      );
      notifications.succeed(
        operationId,
        title: 'MCP 已删除',
        message: '${server.name} 已从设备配置移除',
      );
      await AppLogService.log(
        'device_mcp_config_removed',
        data: {'machine_id': widget.machineId, 'name': server.name},
      );
      if (!mounted) return;
      final loadStatus = shouldLoadAgentMCPStatus(
        agentMode: _agentMode,
        config: config,
      );
      setState(() {
        _config = config;
        _routeMissing = false;
        if (_agentMode && !loadStatus) {
          _status = null;
          _statusLoading = false;
        }
      });
    } catch (error) {
      notifications.fail(operationId, title: 'MCP 删除失败', error: error);
      final routeMissing = _isRouteMissingError(error);
      await AppLogService.log(
        'device_mcp_config_remove_failed',
        level: 'error',
        data: {
          'machine_id': widget.machineId,
          'name': server.name,
          'error': error.toString(),
        },
      );
      if (!mounted) return;
      setState(() {
        _routeMissing = routeMissing;
        _error = _readableError(error);
      });
    } finally {
      if (mounted) {
        setState(() {
          _removing = false;
        });
      }
    }
  }

  List<DeviceMCPServerInfo> _mergeServers(List<DeviceMCPServerInfo> incoming) {
    final merged = <String, DeviceMCPServerInfo>{
      for (final server in _config?.servers ?? const <DeviceMCPServerInfo>[])
        server.name: server,
    };
    for (final server in incoming) {
      merged[server.name] = server;
    }
    final servers = merged.values.toList()
      ..sort((left, right) => left.name.compareTo(right.name));
    return servers;
  }

  bool _isRouteMissingError(Object error) {
    return (error is ApiException && error.statusCode == 404) ||
        error.toString().contains('请求失败 (404)');
  }

  String _readableError(Object error) {
    if (_isRouteMissingError(error)) {
      return '当前服务器尚未部署 MCP 配置接口，请先更新后端后再使用该页面。';
    }
    return error.toString();
  }

  @override
  Widget build(BuildContext context) {
    final actionsEnabled =
        !_saving && !_removing && !_routeMissing && _config != null;
    return Scaffold(
      body: SafeArea(
        child: PageBackground(
          child: Column(
            children: [
              AppTopBar(
                title: 'MCP配置',
                leading: IconButton(
                  icon: const Icon(Icons.arrow_back_ios_new, size: 16),
                  onPressed: () => Navigator.of(context).pop(),
                ),
                actions: [
                  IconButton(
                    tooltip: 'MCP商店',
                    onPressed: actionsEnabled ? _openStore : null,
                    icon: const Icon(Icons.storefront_outlined),
                  ),
                  IconButton(
                    tooltip: '编辑全局配置',
                    onPressed: actionsEnabled ? _openRawEditor : null,
                    icon: const Icon(Icons.edit_note_rounded),
                  ),
                ],
              ),
              Expanded(
                child: _loading && _config == null
                    ? DeviceMCPConfigSkeleton(agentMode: _agentMode)
                    : PageLoadingOverlay(
                        loading: _loading,
                        child: SingleChildScrollView(
                          padding: const EdgeInsets.all(16),
                          child: Column(
                            children: [
                              PanelCard(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(
                                      _pageTitle,
                                      style: Theme.of(
                                        context,
                                      ).textTheme.titleMedium,
                                    ),
                                    const SizedBox(height: 6),
                                    Text(
                                      _pageSubtitle,
                                      style: const TextStyle(
                                        fontSize: 12,
                                        color: AppColors.textMuted,
                                      ),
                                    ),
                                    const SizedBox(height: 8),
                                    Text(
                                      _pageHint,
                                      style: const TextStyle(
                                        fontSize: 12,
                                        color: AppColors.textMuted,
                                      ),
                                    ),
                                    const SizedBox(height: 16),
                                    LayoutBuilder(
                                      builder: (context, constraints) {
                                        final singleColumn =
                                            constraints.maxWidth < 760;
                                        final buttonWidth = singleColumn
                                            ? constraints.maxWidth
                                            : (constraints.maxWidth - 24) / 3;
                                        return Wrap(
                                          spacing: 12,
                                          runSpacing: 12,
                                          children: [
                                            SizedBox(
                                              width: buttonWidth,
                                              child: AppButton(
                                                label: '粘贴导入',
                                                icon:
                                                    Icons.content_paste_rounded,
                                                outlined: true,
                                                onPressed: actionsEnabled
                                                    ? _importServers
                                                    : null,
                                              ),
                                            ),
                                            SizedBox(
                                              width: buttonWidth,
                                              child: AppButton(
                                                label: '新增远程 MCP',
                                                icon: Icons.cloud_outlined,
                                                outlined: true,
                                                onPressed: actionsEnabled
                                                    ? () => _editServer(
                                                        type: 'remote',
                                                      )
                                                    : null,
                                              ),
                                            ),
                                            SizedBox(
                                              width: buttonWidth,
                                              child: AppButton(
                                                label: '新增本地 MCP',
                                                icon: Icons.terminal_rounded,
                                                outlined: true,
                                                onPressed: actionsEnabled
                                                    ? () => _editServer(
                                                        type: 'local',
                                                      )
                                                    : null,
                                              ),
                                            ),
                                          ],
                                        );
                                      },
                                    ),
                                    if (_routeMissing) ...[
                                      const SizedBox(height: 12),
                                      const Text(
                                        '检测到当前服务器还没有部署 MCP 接口，更新后端后此页面即可正常读取和保存配置。',
                                        style: TextStyle(
                                          color: AppColors.statusWarning,
                                          fontSize: 12,
                                        ),
                                      ),
                                    ],
                                    if (_agentMode) ...[
                                      const SizedBox(height: 16),
                                      _AgentMCPStatusCard(
                                        loading: _statusLoading,
                                        status: _status,
                                        onRefresh: _loadStatus,
                                        onRestart: _restartCurrentAgent,
                                        onOpenEnv: () {
                                          context.push(
                                            '/devices/${Uri.encodeComponent(widget.machineId)}/env-config'
                                            '?agentId=${Uri.encodeComponent(widget.agentId)}'
                                            '&projectId=${Uri.encodeComponent(widget.projectId)}',
                                          );
                                        },
                                      ),
                                    ],
                                    if (_error.isNotEmpty) ...[
                                      const SizedBox(height: 12),
                                      Text(
                                        _error,
                                        style: TextStyle(
                                          color: _routeMissing
                                              ? AppColors.statusWarning
                                              : AppColors.statusError,
                                          fontSize: 12,
                                        ),
                                      ),
                                    ],
                                  ],
                                ),
                              ),
                              const SizedBox(height: 16),
                              if (_config == null)
                                PanelCard(
                                  child: Column(
                                    crossAxisAlignment:
                                        CrossAxisAlignment.start,
                                    children: [
                                      Text(
                                        _routeMissing
                                            ? 'MCP接口未部署'
                                            : '读取 MCP 配置失败',
                                        style: Theme.of(
                                          context,
                                        ).textTheme.titleMedium,
                                      ),
                                      const SizedBox(height: 8),
                                      Text(
                                        _error.isEmpty
                                            ? '当前未能读取到设备的 MCP 配置。'
                                            : _error,
                                        style: const TextStyle(
                                          fontSize: 12,
                                          color: AppColors.textSecondary,
                                        ),
                                      ),
                                      const SizedBox(height: 12),
                                      AppButton(
                                        label: '重新加载',
                                        outlined: true,
                                        icon: Icons.refresh_rounded,
                                        onPressed: _loading ? null : _load,
                                      ),
                                    ],
                                  ),
                                )
                              else if (_config!.servers.isNotEmpty)
                                ..._config!.servers.map(
                                  (server) => Padding(
                                    padding: const EdgeInsets.only(bottom: 12),
                                    child: PanelCard(
                                      child: Column(
                                        crossAxisAlignment:
                                            CrossAxisAlignment.start,
                                        children: [
                                          Row(
                                            crossAxisAlignment:
                                                CrossAxisAlignment.start,
                                            children: [
                                              Expanded(
                                                child: Column(
                                                  crossAxisAlignment:
                                                      CrossAxisAlignment.start,
                                                  children: [
                                                    Text(
                                                      server.name,
                                                      style: Theme.of(
                                                        context,
                                                      ).textTheme.titleMedium,
                                                    ),
                                                    const SizedBox(height: 6),
                                                    Wrap(
                                                      spacing: 8,
                                                      runSpacing: 8,
                                                      children: [
                                                        StatusPill(
                                                          label:
                                                              server.type ==
                                                                  'local'
                                                              ? '本地'
                                                              : '远程',
                                                          type: StatusType
                                                              .processing,
                                                        ),
                                                        StatusPill(
                                                          label: server.enabled
                                                              ? '已启用'
                                                              : '已停用',
                                                          type: server.enabled
                                                              ? StatusType
                                                                    .online
                                                              : StatusType
                                                                    .offline,
                                                        ),
                                                      ],
                                                    ),
                                                  ],
                                                ),
                                              ),
                                              Wrap(
                                                spacing: 4,
                                                runSpacing: 4,
                                                crossAxisAlignment:
                                                    WrapCrossAlignment.center,
                                                alignment: WrapAlignment.end,
                                                children: [
                                                  Row(
                                                    mainAxisSize:
                                                        MainAxisSize.min,
                                                    children: [
                                                      const Text(
                                                        '启用',
                                                        style: TextStyle(
                                                          fontSize: 12,
                                                          color: AppColors
                                                              .textMuted,
                                                        ),
                                                      ),
                                                      Switch.adaptive(
                                                        value: server.enabled,
                                                        onChanged:
                                                            _saving || _removing
                                                            ? null
                                                            : (value) =>
                                                                  _toggleServerEnabled(
                                                                    server,
                                                                    value,
                                                                  ),
                                                      ),
                                                    ],
                                                  ),
                                                  IconButton(
                                                    tooltip: '编辑',
                                                    onPressed:
                                                        _saving || _removing
                                                        ? null
                                                        : () => _editServer(
                                                            current: server,
                                                          ),
                                                    icon: const Icon(
                                                      Icons.edit_outlined,
                                                    ),
                                                  ),
                                                  IconButton(
                                                    tooltip: '删除',
                                                    onPressed:
                                                        _saving || _removing
                                                        ? null
                                                        : () => _removeServer(
                                                            server,
                                                          ),
                                                    icon: const Icon(
                                                      Icons
                                                          .delete_outline_rounded,
                                                    ),
                                                  ),
                                                ],
                                              ),
                                            ],
                                          ),
                                          const SizedBox(height: 12),
                                          Text(
                                            server.type == 'local'
                                                ? '命令：${server.command.join(' ')}'
                                                : 'URL：${server.url}',
                                            maxLines: 3,
                                            overflow: TextOverflow.ellipsis,
                                            style: const TextStyle(
                                              fontSize: 12,
                                              color: AppColors.textSecondary,
                                            ),
                                          ),
                                          if (server.type == 'local' &&
                                              server
                                                  .environment
                                                  .isNotEmpty) ...[
                                            const SizedBox(height: 8),
                                            Text(
                                              '环境变量：${server.environment.length} 项',
                                              style: const TextStyle(
                                                fontSize: 12,
                                                color: AppColors.textMuted,
                                              ),
                                            ),
                                          ],
                                          if (server.type == 'remote' &&
                                              server.headers.isNotEmpty) ...[
                                            const SizedBox(height: 8),
                                            Text(
                                              '请求头：${server.headers.length} 项',
                                              style: const TextStyle(
                                                fontSize: 12,
                                                color: AppColors.textMuted,
                                              ),
                                            ),
                                          ],
                                          if (server.type == 'remote' &&
                                              server.oauthMode != 'auto') ...[
                                            const SizedBox(height: 8),
                                            Text(
                                              server.oauthMode == 'disabled'
                                                  ? 'OAuth：已禁用'
                                                  : 'OAuth：自定义',
                                              style: const TextStyle(
                                                fontSize: 12,
                                                color: AppColors.textMuted,
                                              ),
                                            ),
                                          ],
                                        ],
                                      ),
                                    ),
                                  ),
                                )
                              else
                                const EmptyState(
                                  message: '当前设备还没有配置 MCP',
                                  icon: Icons.extension_off_outlined,
                                ),
                            ],
                          ),
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

class _AgentMCPStatusCard extends StatelessWidget {
  final bool loading;
  final MCPStatusInfo? status;
  final VoidCallback onRefresh;
  final VoidCallback onRestart;
  final VoidCallback onOpenEnv;

  const _AgentMCPStatusCard({
    required this.loading,
    required this.status,
    required this.onRefresh,
    required this.onRestart,
    required this.onOpenEnv,
  });

  @override
  Widget build(BuildContext context) {
    final servers = status?.servers ?? const <MCPServerStatusInfo>[];
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: AppColors.inputBackground,
        borderRadius: AppRadius.mdRadius,
        border: Border.all(color: AppColors.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(
                '当前 Agent 状态',
                style: Theme.of(context).textTheme.titleSmall,
              ),
              const Spacer(),
              IconButton(
                tooltip: '刷新 MCP 状态',
                onPressed: loading ? null : onRefresh,
                icon: const Icon(Icons.refresh_rounded),
              ),
              IconButton(
                tooltip: '重启 Agent 并重连 MCP',
                onPressed: loading ? null : onRestart,
                icon: const Icon(Icons.restart_alt_rounded),
              ),
              IconButton(
                tooltip: 'Agent 环境变量',
                onPressed: onOpenEnv,
                icon: const Icon(Icons.terminal_rounded),
              ),
            ],
          ),
          const SizedBox(height: 8),
          if (loading)
            const Padding(
              padding: EdgeInsets.symmetric(vertical: 12),
              child: LoadingState(),
            )
          else if (servers.isEmpty)
            const EmptyState(
              icon: Icons.extension_off_outlined,
              message: '当前 Agent 没有返回 MCP 状态',
            )
          else
            ...servers.map(
              (server) => Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: _AgentMCPServerStatusTile(server: server),
              ),
            ),
        ],
      ),
    );
  }
}

class _AgentMCPServerStatusTile extends StatelessWidget {
  final MCPServerStatusInfo server;

  const _AgentMCPServerStatusTile({required this.server});

  @override
  Widget build(BuildContext context) {
    final type = _mcpStatusType(server.status);
    final toolCount = server.tools.length;
    return Container(
      decoration: BoxDecoration(
        color: AppColors.surface,
        borderRadius: AppRadius.mdRadius,
        border: Border.all(color: AppColors.border),
      ),
      child: ExpansionTile(
        tilePadding: const EdgeInsets.symmetric(horizontal: 12),
        childrenPadding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
        leading: const Icon(Icons.extension_rounded, color: AppColors.primary),
        title: Text(
          server.name,
          overflow: TextOverflow.ellipsis,
          style: const TextStyle(fontWeight: FontWeight.w700),
        ),
        subtitle: Text(
          toolCount > 0 ? '工具 $toolCount 个' : '未返回工具',
          style: const TextStyle(fontSize: 12, color: AppColors.textMuted),
        ),
        trailing: StatusPill(label: _mcpStatusText(server.status), type: type),
        children: [
          if (server.error.isNotEmpty)
            Align(
              alignment: Alignment.centerLeft,
              child: Text(
                server.error,
                style: const TextStyle(
                  color: AppColors.statusError,
                  fontSize: 12,
                  height: 1.4,
                ),
              ),
            ),
          if (server.error.isNotEmpty && server.tools.isNotEmpty)
            const SizedBox(height: 10),
          if (server.tools.isEmpty && server.error.isEmpty)
            const Align(
              alignment: Alignment.centerLeft,
              child: Text(
                'MCP 已连接但没有返回工具，请确认服务端 tools/list 是否正常。',
                style: TextStyle(fontSize: 12, color: AppColors.textMuted),
              ),
            )
          else
            ...server.tools.map(
              (tool) => Container(
                width: double.infinity,
                margin: const EdgeInsets.only(bottom: 8),
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: AppColors.inputBackground,
                  borderRadius: AppRadius.smRadius,
                  border: Border.all(color: AppColors.border),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      tool.id,
                      style: const TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    if (tool.description.isNotEmpty) ...[
                      const SizedBox(height: 6),
                      Text(
                        tool.description,
                        style: const TextStyle(
                          fontSize: 12,
                          color: AppColors.textSecondary,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            ),
        ],
      ),
    );
  }
}

StatusType _mcpStatusType(String status) {
  return switch (status) {
    'connected' => StatusType.online,
    'failed' => StatusType.error,
    'disabled' => StatusType.offline,
    'needs_auth' => StatusType.warning,
    'needs_client_registration' => StatusType.warning,
    _ => StatusType.processing,
  };
}

String _mcpStatusText(String status) {
  return switch (status) {
    'connected' => '已连接',
    'failed' => '失败',
    'disabled' => '已停用',
    'needs_auth' => '需授权',
    'needs_client_registration' => '需注册',
    _ => status.isEmpty ? '未知' : status,
  };
}

class _MCPImportDialog extends StatefulWidget {
  const _MCPImportDialog();

  @override
  State<_MCPImportDialog> createState() => _MCPImportDialogState();
}

class _MCPImportDialogState extends State<_MCPImportDialog> {
  late final TextEditingController _controller;
  String _error = '';

  @override
  void initState() {
    super.initState();
    _controller = TextEditingController();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _submit() {
    try {
      final result = DeviceMCPImportParser.parse(_controller.text);
      Navigator.of(context).pop(result);
    } on FormatException catch (error) {
      setState(() {
        _error = error.message.toString();
      });
    } catch (error) {
      setState(() {
        _error = error.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final viewport = MediaQuery.sizeOf(context);
    final dialogHeight = math.min(760.0, math.max(320.0, viewport.height - 40));
    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
      shape: RoundedRectangleBorder(borderRadius: AppRadius.lgRadius),
      child: Container(
        constraints: BoxConstraints(maxWidth: 680, maxHeight: dialogHeight),
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              '粘贴导入 MCP',
              style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 6),
            const Text(
              '支持直接粘贴 `{ 名称: {...} }`、`{ mcp: {...} }`，也支持标准 `command + args + env` 结构。',
              style: TextStyle(fontSize: 12, color: AppColors.textMuted),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: TextField(
                controller: _controller,
                autofocus: true,
                minLines: 16,
                maxLines: null,
                decoration: const InputDecoration(
                  labelText: 'MCP 配置 JSON',
                  hintText:
                      '{\n  "verify-api-token-11": {\n    "command": "npx",\n    "args": ["-y", "@ktbtw/verify-mcp"],\n    "env": {\n      "VERIFY_BASE_URL": "https://www.xyapi.top/verfiy"\n    }\n  }\n}',
                ),
              ),
            ),
            if (_error.isNotEmpty) ...[
              const SizedBox(height: 12),
              Text(
                _error,
                style: const TextStyle(
                  fontSize: 12,
                  color: AppColors.statusError,
                ),
              ),
            ],
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: AppButton(
                    label: '取消',
                    outlined: true,
                    onPressed: () => Navigator.of(context).pop(),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: AppButton(
                    label: '识别并导入',
                    icon: Icons.file_download_done_rounded,
                    onPressed: _submit,
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _MCPServerDialog extends StatefulWidget {
  final DeviceMCPServerInfo? initial;
  final String defaultType;

  const _MCPServerDialog({this.initial, required this.defaultType});

  @override
  State<_MCPServerDialog> createState() => _MCPServerDialogState();
}

class _MCPServerDialogState extends State<_MCPServerDialog> {
  late final TextEditingController _nameController;
  late final TextEditingController _urlController;
  late final TextEditingController _timeoutController;
  late final TextEditingController _connectTimeoutController;
  late final TextEditingController _discoveryTimeoutController;
  late final TextEditingController _toolTimeoutController;
  late final TextEditingController _asyncToolsController;
  late final TextEditingController _headersController;
  late final TextEditingController _commandController;
  late final TextEditingController _environmentController;
  late final TextEditingController _oauthClientIdController;
  late final TextEditingController _oauthClientSecretController;
  late final TextEditingController _oauthScopeController;
  late String _type;
  late bool _enabled;
  late String _oauthMode;
  late String _savedSecretMasked;
  bool _secretDirty = false;
  String _error = '';

  @override
  void initState() {
    super.initState();
    final current = widget.initial;
    _type = current?.type ?? widget.defaultType;
    _enabled = current?.enabled ?? true;
    _oauthMode = _type == 'remote' ? (current?.oauthMode ?? 'auto') : 'auto';
    _savedSecretMasked = current?.oauthClientSecretMasked ?? '';
    _nameController = TextEditingController(text: current?.name ?? '');
    _urlController = TextEditingController(text: current?.url ?? '');
    _timeoutController = TextEditingController(
      text: current != null && current.timeout > 0 ? '${current.timeout}' : '',
    );
    _connectTimeoutController = TextEditingController(
      text: current != null && current.connectTimeout > 0
          ? '${current.connectTimeout}'
          : '',
    );
    _discoveryTimeoutController = TextEditingController(
      text: current != null && current.discoveryTimeout > 0
          ? '${current.discoveryTimeout}'
          : '',
    );
    _toolTimeoutController = TextEditingController(
      text: current != null && current.toolTimeout > 0
          ? '${current.toolTimeout}'
          : '',
    );
    _asyncToolsController = TextEditingController(
      text: (current?.asyncTools ?? const []).join('\n'),
    );
    _headersController = TextEditingController(
      text: _formatMap(current?.headers ?? const {}),
    );
    _commandController = TextEditingController(
      text: (current?.command ?? const []).join('\n'),
    );
    _environmentController = TextEditingController(
      text: _formatMap(current?.environment ?? const {}),
    );
    _oauthClientIdController = TextEditingController(
      text: current?.oauthClientId ?? '',
    );
    _oauthClientSecretController = TextEditingController(
      text: _savedSecretMasked,
    );
    _oauthScopeController = TextEditingController(
      text: current?.oauthScope ?? '',
    );
    _oauthClientSecretController.addListener(() {
      final dirty =
          _savedSecretMasked.isEmpty ||
          _oauthClientSecretController.text != _savedSecretMasked;
      if (dirty == _secretDirty) return;
      setState(() {
        _secretDirty = dirty;
      });
    });
  }

  @override
  void dispose() {
    _nameController.dispose();
    _urlController.dispose();
    _timeoutController.dispose();
    _connectTimeoutController.dispose();
    _discoveryTimeoutController.dispose();
    _toolTimeoutController.dispose();
    _asyncToolsController.dispose();
    _headersController.dispose();
    _commandController.dispose();
    _environmentController.dispose();
    _oauthClientIdController.dispose();
    _oauthClientSecretController.dispose();
    _oauthScopeController.dispose();
    super.dispose();
  }

  String _secretValue() {
    final current = _oauthClientSecretController.text.trim();
    if (!_secretDirty && current == _savedSecretMasked) {
      return '';
    }
    return current;
  }

  static String _formatMap(Map<String, String> value) {
    if (value.isEmpty) return '';
    final keys = value.keys.toList()..sort();
    return keys.map((key) => '$key=${value[key] ?? ''}').join('\n');
  }

  static Map<String, String> _parseMap(String input) {
    final result = <String, String>{};
    for (final rawLine in input.split('\n')) {
      final line = rawLine.trim();
      if (line.isEmpty) continue;
      final eq = line.indexOf('=');
      final colon = line.indexOf(':');
      final splitAt = eq >= 0 ? eq : colon;
      if (splitAt <= 0) continue;
      final key = line.substring(0, splitAt).trim();
      final value = line.substring(splitAt + 1).trim();
      if (key.isEmpty || value.isEmpty) continue;
      result[key] = value;
    }
    return result;
  }

  static List<String> _parseList(String input) {
    return input
        .split('\n')
        .map((item) => item.trim())
        .where((item) => item.isNotEmpty)
        .toList();
  }

  void _submit() {
    final name = _nameController.text.trim();
    if (name.isEmpty) {
      setState(() {
        _error = '请先填写 MCP 名称';
      });
      return;
    }
    final timeout = int.tryParse(_timeoutController.text.trim()) ?? 0;
    final connectTimeout =
        int.tryParse(_connectTimeoutController.text.trim()) ?? 0;
    final discoveryTimeout =
        int.tryParse(_discoveryTimeoutController.text.trim()) ?? 0;
    final toolTimeout = int.tryParse(_toolTimeoutController.text.trim()) ?? 0;
    final server = DeviceMCPServerInfo(
      name: name,
      type: _type,
      enabled: _enabled,
      timeout: timeout > 0 ? timeout : 0,
      connectTimeout: connectTimeout > 0 ? connectTimeout : 0,
      discoveryTimeout: discoveryTimeout > 0 ? discoveryTimeout : 0,
      toolTimeout: toolTimeout > 0 ? toolTimeout : 0,
      asyncTools: _parseList(_asyncToolsController.text),
      url: _urlController.text.trim(),
      headers: _parseMap(_headersController.text),
      command: _parseList(_commandController.text),
      environment: _parseMap(_environmentController.text),
      oauthMode: _type == 'remote' ? _oauthMode : 'auto',
      oauthClientId: _oauthClientIdController.text.trim(),
      oauthClientSecret: _secretValue(),
      oauthClientSecretMasked: _savedSecretMasked,
      oauthScope: _oauthScopeController.text.trim(),
    );
    if (_type == 'remote' && server.url.isEmpty) {
      setState(() {
        _error = '远程 MCP 必须填写 URL';
      });
      return;
    }
    if (_type == 'local' && server.command.isEmpty) {
      setState(() {
        _error = '本地 MCP 必须填写启动命令';
      });
      return;
    }
    Navigator.of(context).pop(server);
  }

  @override
  Widget build(BuildContext context) {
    final viewport = MediaQuery.sizeOf(context);
    final dialogHeight = math.min(760.0, math.max(360.0, viewport.height - 40));
    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
      shape: RoundedRectangleBorder(borderRadius: AppRadius.lgRadius),
      child: Container(
        constraints: BoxConstraints(maxWidth: 620, maxHeight: dialogHeight),
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              widget.initial == null ? '新增 MCP' : '编辑 MCP',
              style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 6),
            const Text(
              '保存后会同步到设备的唯一全局 opencode 配置文件。',
              style: TextStyle(fontSize: 12, color: AppColors.textMuted),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: SingleChildScrollView(
                child: Column(
                  children: [
                    TextField(
                      controller: _nameController,
                      decoration: const InputDecoration(labelText: 'MCP 名称'),
                    ),
                    const SizedBox(height: 12),
                    AppSelect<String>(
                      value: _type,
                      label: '类型',
                      options: const [
                        AppSelectOption(value: 'remote', label: '远程'),
                        AppSelectOption(value: 'local', label: '本地'),
                      ],
                      onChanged: (value) {
                        setState(() {
                          _type = value;
                          if (_type != 'remote') {
                            _oauthMode = 'auto';
                          }
                        });
                      },
                    ),
                    const SizedBox(height: 8),
                    SwitchListTile.adaptive(
                      contentPadding: EdgeInsets.zero,
                      title: const Text('启用'),
                      value: _enabled,
                      onChanged: (value) {
                        setState(() {
                          _enabled = value;
                        });
                      },
                    ),
                    TextField(
                      controller: _timeoutController,
                      keyboardType: TextInputType.number,
                      decoration: const InputDecoration(
                        labelText: '兼容超时（毫秒，可选）',
                      ),
                    ),
                    const SizedBox(height: 12),
                    Row(
                      children: [
                        Expanded(
                          child: TextField(
                            controller: _connectTimeoutController,
                            keyboardType: TextInputType.number,
                            decoration: const InputDecoration(
                              labelText: '连接超时',
                            ),
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: TextField(
                            controller: _discoveryTimeoutController,
                            keyboardType: TextInputType.number,
                            decoration: const InputDecoration(
                              labelText: '发现超时',
                            ),
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: _toolTimeoutController,
                      keyboardType: TextInputType.number,
                      decoration: const InputDecoration(
                        labelText: '工具执行超时（毫秒）',
                      ),
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: _asyncToolsController,
                      minLines: 2,
                      maxLines: 4,
                      decoration: const InputDecoration(
                        labelText: '异步工具',
                        hintText: '一行一个工具名，例如 idb_open',
                      ),
                    ),
                    if (_type == 'remote') ...[
                      const SizedBox(height: 12),
                      TextField(
                        controller: _urlController,
                        decoration: const InputDecoration(labelText: 'URL'),
                      ),
                      const SizedBox(height: 12),
                      TextField(
                        controller: _headersController,
                        minLines: 3,
                        maxLines: 6,
                        decoration: const InputDecoration(
                          labelText: '请求头',
                          hintText: '一行一个，支持 key=value 或 key:value',
                        ),
                      ),
                      const SizedBox(height: 12),
                      AppSelect<String>(
                        value: _oauthMode,
                        label: 'OAuth 模式',
                        options: const [
                          AppSelectOption(value: 'auto', label: '自动检测'),
                          AppSelectOption(value: 'disabled', label: '关闭'),
                          AppSelectOption(value: 'custom', label: '自定义'),
                        ],
                        onChanged: (value) {
                          setState(() {
                            _oauthMode = value;
                          });
                        },
                      ),
                      if (_oauthMode == 'custom') ...[
                        const SizedBox(height: 12),
                        TextField(
                          controller: _oauthClientIdController,
                          decoration: const InputDecoration(
                            labelText: 'OAuth Client ID',
                          ),
                        ),
                        const SizedBox(height: 12),
                        TextField(
                          controller: _oauthClientSecretController,
                          decoration: const InputDecoration(
                            labelText: 'OAuth Client Secret',
                            hintText: '留空表示沿用已有 Secret',
                          ),
                        ),
                        const SizedBox(height: 12),
                        TextField(
                          controller: _oauthScopeController,
                          decoration: const InputDecoration(
                            labelText: 'OAuth Scope',
                          ),
                        ),
                      ],
                    ] else ...[
                      const SizedBox(height: 12),
                      TextField(
                        controller: _commandController,
                        minLines: 3,
                        maxLines: 6,
                        decoration: const InputDecoration(
                          labelText: '启动命令',
                          hintText:
                              '一行一个参数，例如\nnpx\n-y\n@modelcontextprotocol/server-filesystem',
                        ),
                      ),
                      const SizedBox(height: 12),
                      TextField(
                        controller: _environmentController,
                        minLines: 3,
                        maxLines: 6,
                        decoration: const InputDecoration(
                          labelText: '环境变量',
                          hintText: '一行一个，支持 KEY=value',
                        ),
                      ),
                    ],
                    if (_error.isNotEmpty) ...[
                      const SizedBox(height: 12),
                      Text(
                        _error,
                        style: const TextStyle(
                          fontSize: 12,
                          color: AppColors.statusError,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: AppButton(
                    label: '取消',
                    outlined: true,
                    onPressed: () => Navigator.of(context).pop(),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: AppButton(label: '保存', onPressed: _submit),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
