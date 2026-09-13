import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter/services.dart';

import '../../../core/config/api_client.dart';
import '../../../core/notifications/app_notification_feedback.dart';
import '../../../core/services/app_log_service.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import '../data/device_env_config_model.dart';
import '../data/device_model.dart';
import '../data/device_repository.dart';
import 'device_provider.dart';

class DeviceEnvConfigPage extends ConsumerStatefulWidget {
  final String machineId;
  final String agentId;
  final String projectId;

  const DeviceEnvConfigPage({
    super.key,
    required this.machineId,
    this.agentId = '',
    this.projectId = '',
  });

  @override
  ConsumerState<DeviceEnvConfigPage> createState() =>
      _DeviceEnvConfigPageState();
}

class _DeviceEnvConfigPageState extends ConsumerState<DeviceEnvConfigPage> {
  final _globalController = TextEditingController();
  final _agentController = TextEditingController();
  DeviceEnvConfigInfo? _config;
  String _selectedAgentId = '';
  bool _loading = true;
  bool _saving = false;
  bool _routeMissing = false;
  String _error = '';
  bool get _agentMode => widget.agentId.trim().isNotEmpty;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didUpdateWidget(covariant DeviceEnvConfigPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.machineId != widget.machineId ||
        oldWidget.agentId != widget.agentId) {
      _config = null;
      _selectedAgentId = widget.agentId.trim();
      _load();
    }
  }

  @override
  void dispose() {
    _globalController.dispose();
    _agentController.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = '';
      _routeMissing = false;
      _selectedAgentId = widget.agentId.trim();
    });
    try {
      await AppLogService.log(
        'device_env_config_load_started',
        data: {'machine_id': widget.machineId},
      );
      final repo = ref.read(deviceRepositoryProvider);
      final config = await repo.getDeviceEnvConfig(widget.machineId);
      await AppLogService.log(
        'device_env_config_load_completed',
        data: {
          'machine_id': widget.machineId,
          'global_count': config.globalEnvironment.length,
          'agent_count': config.agents.length,
        },
      );
      if (!mounted) return;
      setState(() {
        _config = config;
        _selectedAgentId = _chooseAgentId(config, _selectedAgentId);
        _syncControllers();
      });
    } catch (error) {
      final routeMissing = _isRouteMissingError(error);
      await AppLogService.log(
        'device_env_config_load_failed',
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
          _loading = false;
        });
      }
    }
  }

  Future<void> _save() async {
    final config = _config;
    if (config == null) {
      setState(() {
        _error = '环境变量配置尚未加载成功，请刷新后再保存。';
      });
      return;
    }
    final globalEnv = _agentMode
        ? Map<String, String>.from(config.globalEnvironment)
        : _parseEnvText(_globalController.text);
    final agentEnv = _agentMode
        ? _parseEnvText(_agentController.text)
        : const <String, String>{};
    final changed = _agentMode
        ? !_envMapEquals(
            config.agentById(_selectedAgentId)?.environment ?? const {},
            agentEnv,
          )
        : !_envMapEquals(config.globalEnvironment, globalEnv);
    if (!changed) {
      showAppFeedback(context, message: '环境变量没有变化');
      return;
    }
    if (_agentMode && config.agentById(_selectedAgentId) == null) {
      setState(() {
        _error = '当前 Agent 不在设备配置列表中，请返回设备详情刷新后重试。';
      });
      return;
    }
    final invalidKeys = _agentMode
        ? _reservedKeys(agentEnv)
        : _reservedKeys(globalEnv);
    if (invalidKeys.isNotEmpty) {
      setState(() {
        _error = '不能设置保留变量：${invalidKeys.toSet().join('、')}';
      });
      return;
    }

    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      await AppLogService.log(
        'device_env_config_save_started',
        data: {
          'machine_id': widget.machineId,
          'agent_id': _agentMode ? _selectedAgentId : '',
          'global_count': globalEnv.length,
          'agent_count': agentEnv.length,
        },
      );
      final repo = ref.read(deviceRepositoryProvider);
      final savedConfig = await repo.saveDeviceEnvConfig(
        machineId: widget.machineId,
        globalEnvironment: globalEnv,
        agentId: _agentMode ? _selectedAgentId : '',
        agentEnvironment: agentEnv,
        expectedRevision: config.revision,
      );
      final restartMessage = await _restartChangedAgents(repo);
      await AppLogService.log(
        'device_env_config_save_completed',
        data: {
          'machine_id': widget.machineId,
          'agent_id': _selectedAgentId,
          'restart_message': restartMessage,
        },
      );
      if (!mounted) return;
      setState(() {
        _config = savedConfig;
        _selectedAgentId = _chooseAgentId(savedConfig, _selectedAgentId);
        _syncControllers();
      });
      showAppFeedback(context, title: '环境变量已保存', message: restartMessage);
    } catch (error) {
      final routeMissing = _isRouteMissingError(error);
      await AppLogService.log(
        'device_env_config_save_failed',
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

  Future<String> _restartChangedAgents(DeviceRepository repo) async {
    if (_agentMode) {
      final device = await repo.getDevice(widget.machineId);
      AgentModel? target;
      for (final agent in device.agents) {
        if (agent.agentId == _selectedAgentId) {
          target = agent;
          break;
        }
      }
      if (target == null) return '当前 Agent 不在设备列表中，未重启';
      if (target.isDisabled) return '当前 Agent 已停用，未重启';
      try {
        await repo.restartDeviceAgent(
          machineId: widget.machineId,
          agentId: _selectedAgentId,
        );
      } catch (error) {
        ref.invalidate(deviceDetailProvider(widget.machineId));
        ref.invalidate(deviceListProvider);
        return '当前 Agent 已保存，但重启失败：$error';
      }
      ref.invalidate(deviceDetailProvider(widget.machineId));
      ref.invalidate(deviceListProvider);
      return '已重启当前 Agent';
    }

    final device = await repo.getDevice(widget.machineId);
    final agents = device.agents.where((agent) => !agent.isDisabled).toList();
    if (agents.isEmpty) return '没有需要重启的 Agent';
    final failures = <String>[];
    for (final agent in agents) {
      try {
        await repo.restartDeviceAgent(
          machineId: widget.machineId,
          agentId: agent.agentId,
        );
      } catch (error) {
        final name = agent.projectId.isEmpty ? agent.agentId : agent.projectId;
        failures.add('$name: $error');
      }
    }
    ref.invalidate(deviceDetailProvider(widget.machineId));
    ref.invalidate(deviceListProvider);
    if (failures.isNotEmpty) {
      return '环境变量已保存，但部分 Agent 重启失败：${failures.join('；')}';
    }
    return '已重启 ${agents.length} 个 Agent';
  }

  void _syncControllers() {
    final config = _config;
    _globalController.text = _formatEnvMap(
      config?.globalEnvironment ?? const {},
    );
    _agentController.text = _formatEnvMap(
      config?.agentById(_selectedAgentId)?.environment ?? const {},
    );
  }

  String _chooseAgentId(DeviceEnvConfigInfo config, String current) {
    final requested = widget.agentId.trim();
    if (requested.isNotEmpty && config.agentById(requested) != null) {
      return requested;
    }
    if (current.isNotEmpty && config.agentById(current) != null) {
      return current;
    }
    return _agentMode ? requested : '';
  }

  bool _isRouteMissingError(Object error) {
    return (error is ApiException && error.statusCode == 404) ||
        error.toString().contains('请求失败 (404)');
  }

  String _readableError(Object error) {
    if (_isRouteMissingError(error)) {
      return '当前服务器尚未部署环境变量配置接口，请先更新后端后再使用该页面。';
    }
    if (error.toString().contains('设备响应超时') ||
        error.toString().contains('connection abort')) {
      return '设备端没有及时返回环境变量配置。请确认 launcher 已更新到支持环境变量配置的版本，并且当前设备在线。';
    }
    return error.toString();
  }

  @override
  Widget build(BuildContext context) {
    final config = _config;
    final hasEditableTarget =
        config != null &&
        (!_agentMode || config.agentById(_selectedAgentId) != null);
    final actionsEnabled =
        !_loading && !_saving && !_routeMissing && hasEditableTarget;
    final title = _agentMode ? 'Agent 环境变量' : '环境变量';
    return Scaffold(
      body: SafeArea(
        child: PageBackground(
          child: Column(
            children: [
              AppTopBar(
                title: title,
                leading: IconButton(
                  icon: const Icon(Icons.arrow_back_ios_new, size: 16),
                  onPressed: () => Navigator.of(context).pop(),
                ),
                actions: [
                  IconButton(
                    tooltip: '常用指令',
                    onPressed: _showCommandDialog,
                    icon: const Icon(Icons.library_books_rounded),
                  ),
                  IconButton(
                    tooltip: '刷新',
                    onPressed: _saving ? null : _load,
                    icon: const Icon(Icons.refresh_rounded),
                  ),
                ],
              ),
              Expanded(
                child: _loading && _config == null
                    ? DeviceEnvConfigSkeleton(agentMode: _agentMode)
                    : _error.isNotEmpty && config == null
                    ? _ErrorPanel(
                        message: _error,
                        retrying: _loading,
                        onRetry: _load,
                      )
                    : PageLoadingOverlay(
                        loading: _loading,
                        child: SingleChildScrollView(
                          padding: const EdgeInsets.all(16),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.stretch,
                            children: [
                              _SummaryCard(
                                machineId: widget.machineId,
                                config: config,
                                error: _error,
                                agentMode: _agentMode,
                                agentId: _selectedAgentId,
                                projectId: widget.projectId,
                              ),
                              const SizedBox(height: 16),
                              if (_agentMode) ...[
                                if (config?.agentById(_selectedAgentId) == null)
                                  EmptyState(
                                    message:
                                        '当前 Agent 不在设备配置列表中：$_selectedAgentId',
                                    icon: Icons.smart_toy_outlined,
                                  )
                                else
                                  _EnvEditorCard(
                                    title: 'Agent 覆盖变量',
                                    count: _parseEnvText(
                                      _agentController.text,
                                    ).length,
                                    controller: _agentController,
                                    enabled: actionsEnabled,
                                  ),
                              ] else
                                _EnvEditorCard(
                                  title: '全局环境变量',
                                  count: _parseEnvText(
                                    _globalController.text,
                                  ).length,
                                  controller: _globalController,
                                  enabled: actionsEnabled,
                                ),
                              const SizedBox(height: 16),
                              Align(
                                alignment: Alignment.centerRight,
                                child: AppButton(
                                  label: _agentMode ? '保存 Agent 变量' : '保存配置',
                                  icon: Icons.save_outlined,
                                  loading: _saving,
                                  onPressed: actionsEnabled ? _save : null,
                                  width: _agentMode ? 170 : 140,
                                ),
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

  Future<void> _showCommandDialog() async {
    final selected = await showDialog<EnvironmentPresetModel>(
      context: context,
      builder: (context) =>
          _EnvCommandDialog(repository: ref.read(deviceRepositoryProvider)),
    );
    if (selected == null || selected.variables.isEmpty) return;
    final controller = _agentMode ? _agentController : _globalController;
    final merged = _parseEnvText(controller.text);
    merged.addAll(selected.variables);
    controller.text = _formatEnvMap(merged);
    if (!mounted) return;
    showAppFeedback(context, message: '已应用环境预设：${selected.name}');
  }
}

class _SummaryCard extends StatelessWidget {
  final String machineId;
  final DeviceEnvConfigInfo? config;
  final String error;
  final bool agentMode;
  final String agentId;
  final String projectId;

  const _SummaryCard({
    required this.machineId,
    required this.config,
    required this.error,
    required this.agentMode,
    required this.agentId,
    required this.projectId,
  });

  @override
  Widget build(BuildContext context) {
    final agent = config?.agentById(agentId);
    final displayName = projectId.trim().isNotEmpty
        ? projectId.trim()
        : (agent?.name.trim().isNotEmpty == true
              ? agent!.name.trim()
              : agentId);
    final title = agentMode ? 'Agent 变量' : '设备级变量';
    return PanelCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(title, style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 6),
          Text(
            agentMode ? displayName : 'Machine ID: $machineId',
            style: const TextStyle(fontSize: 12, color: AppColors.textMuted),
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
          ),
          if (agentMode && agentId.isNotEmpty) ...[
            const SizedBox(height: 4),
            Text(
              agentId,
              style: const TextStyle(
                fontSize: 12,
                color: AppColors.textMuted,
                fontFamily: 'monospace',
              ),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          ],
          const SizedBox(height: 12),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: agentMode
                ? [
                    StatusPill(
                      label: '继承 ${config?.globalEnvironment.length ?? 0}',
                      type: StatusType.processing,
                    ),
                    StatusPill(
                      label: '覆盖 ${agent?.environment.length ?? 0}',
                      type: StatusType.processing,
                    ),
                  ]
                : [
                    StatusPill(
                      label: '全局 ${config?.globalEnvironment.length ?? 0}',
                      type: StatusType.processing,
                    ),
                    StatusPill(
                      label: 'Agent ${config?.agents.length ?? 0}',
                      type: StatusType.processing,
                    ),
                  ],
          ),
          if (error.isNotEmpty) ...[
            const SizedBox(height: 12),
            Text(
              error,
              style: const TextStyle(
                fontSize: 12,
                color: AppColors.statusError,
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class _EnvEditorCard extends StatelessWidget {
  final String title;
  final int count;
  final TextEditingController controller;
  final bool enabled;

  const _EnvEditorCard({
    required this.title,
    required this.count,
    required this.controller,
    required this.enabled,
  });

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(title, style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(width: 8),
              StatusPill(label: '$count', type: StatusType.processing),
            ],
          ),
          const SizedBox(height: 12),
          TextField(
            controller: controller,
            enabled: enabled,
            minLines: 8,
            maxLines: 14,
            style: const TextStyle(fontFamily: 'monospace', fontSize: 13),
            decoration: const InputDecoration(
              hintText: 'HTTP_PROXY=http://127.0.0.1:7897',
              alignLabelWithHint: true,
            ),
          ),
        ],
      ),
    );
  }
}

class _ErrorPanel extends StatelessWidget {
  final String message;
  final bool retrying;
  final VoidCallback onRetry;

  const _ErrorPanel({
    required this.message,
    required this.retrying,
    required this.onRetry,
  });

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      padding: const EdgeInsets.all(16),
      child: PanelCard(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(
              Icons.error_outline_rounded,
              size: 44,
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
              loading: retrying,
              onPressed: retrying ? null : onRetry,
              width: 120,
            ),
          ],
        ),
      ),
    );
  }
}

class _EnvCommandDialog extends StatefulWidget {
  final DeviceRepository repository;

  const _EnvCommandDialog({required this.repository});

  @override
  State<_EnvCommandDialog> createState() => _EnvCommandDialogState();
}

class _EnvCommandDialogState extends State<_EnvCommandDialog> {
  late Future<List<EnvironmentPresetModel>> _future;

  @override
  void initState() {
    super.initState();
    _future = widget.repository.getEnvironmentPresets();
  }

  Future<void> _copy(String content) async {
    await Clipboard.setData(ClipboardData(text: content));
    if (!mounted) return;
    showAppFeedback(context, message: '已复制到剪贴板');
  }

  @override
  Widget build(BuildContext context) {
    final media = MediaQuery.sizeOf(context);
    final isMobile = AppBreakpoints.isMobile(context);
    final contentWidth = media.width > 640
        ? 600.0
        : (media.width - 24).clamp(280.0, 600.0).toDouble();
    final maxHeight = (media.height * (isMobile ? 0.76 : 0.72))
        .clamp(280.0, 680.0)
        .toDouble();
    final horizontalPadding = isMobile ? 16.0 : 24.0;
    return AlertDialog(
      insetPadding: EdgeInsets.symmetric(
        horizontal: isMobile ? 12 : 40,
        vertical: 24,
      ),
      titlePadding: EdgeInsets.fromLTRB(
        horizontalPadding,
        isMobile ? 16 : 22,
        horizontalPadding,
        4,
      ),
      contentPadding: EdgeInsets.fromLTRB(
        horizontalPadding,
        8,
        horizontalPadding,
        0,
      ),
      actionsPadding: EdgeInsets.fromLTRB(
        horizontalPadding,
        4,
        horizontalPadding,
        isMobile ? 10 : 16,
      ),
      title: const Text('环境预设'),
      content: SizedBox(
        width: contentWidth,
        height: maxHeight,
        child: FutureBuilder<List<EnvironmentPresetModel>>(
          future: _future,
          builder: (context, snapshot) {
            if (snapshot.connectionState == ConnectionState.waiting) {
              return const Padding(
                padding: EdgeInsets.symmetric(vertical: 8),
                child: DialogContentSkeleton(itemCount: 3, showHeader: false),
              );
            }
            if (snapshot.hasError) {
              return _EnvPresetDialogStatePanel(
                icon: Icons.error_outline_rounded,
                message: '环境预设加载失败，请确认后端已更新并且当前账号已登录。',
                actionLabel: '重试',
                onAction: () {
                  setState(() {
                    _future = widget.repository.getEnvironmentPresets();
                  });
                },
              );
            }
            final presets = snapshot.data ?? const [];
            if (presets.isEmpty) {
              return const _EnvPresetDialogStatePanel(
                icon: Icons.inventory_2_outlined,
                message: '暂无可用环境预设',
              );
            }
            return Scrollbar(
              child: ListView.separated(
                padding: const EdgeInsets.only(bottom: 4),
                itemCount: presets.length,
                separatorBuilder: (_, _) => const SizedBox(height: 10),
                itemBuilder: (context, index) => _EnvCommandTile(
                  item: presets[index],
                  onApply: (preset) => Navigator.of(context).pop(preset),
                  onCopy: _copy,
                ),
              ),
            );
          },
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('关闭'),
        ),
      ],
    );
  }
}

class _EnvCommandTile extends StatelessWidget {
  final EnvironmentPresetModel item;
  final ValueChanged<EnvironmentPresetModel> onApply;
  final Future<void> Function(String content) onCopy;

  const _EnvCommandTile({
    required this.item,
    required this.onApply,
    required this.onCopy,
  });

  @override
  Widget build(BuildContext context) {
    final isMobile = AppBreakpoints.isMobile(context);
    return Container(
      padding: EdgeInsets.all(isMobile ? 12 : 16),
      decoration: BoxDecoration(
        color: AppColors.inputBackground,
        borderRadius: BorderRadius.circular(isMobile ? 12 : 14),
        border: Border.all(color: AppColors.border),
      ),
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
                      item.name,
                      style: Theme.of(context).textTheme.titleSmall,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                    const SizedBox(height: 4),
                    Text(
                      item.description,
                      maxLines: isMobile ? 2 : 3,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 12,
                        color: AppColors.textMuted,
                      ),
                    ),
                  ],
                ),
              ),
              IconButton(
                tooltip: '复制',
                onPressed: () => onCopy(item.content),
                icon: const Icon(Icons.copy_outlined),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              StatusPill(label: item.category, type: StatusType.processing),
              StatusPill(
                label: '${item.variables.length} 项',
                type: StatusType.processing,
              ),
              if (item.source.trim().isNotEmpty)
                StatusPill(label: item.source, type: StatusType.processing),
            ],
          ),
          const SizedBox(height: 8),
          ConstrainedBox(
            constraints: BoxConstraints(maxHeight: isMobile ? 112 : 176),
            child: SingleChildScrollView(
              child: SelectableText(
                item.content,
                style: const TextStyle(
                  fontSize: 12,
                  height: 1.45,
                  fontFamily: 'monospace',
                  color: AppColors.textPrimary,
                ),
              ),
            ),
          ),
          const SizedBox(height: 10),
          Row(
            children: [
              Expanded(
                child: Text(
                  '${item.variables.length} 个变量',
                  style: const TextStyle(
                    fontSize: 12,
                    color: AppColors.textMuted,
                  ),
                ),
              ),
              AppButton(
                label: '应用',
                icon: Icons.add_circle_outline_rounded,
                onPressed: () => onApply(item),
                outlined: true,
                width: isMobile ? 96 : 104,
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _EnvPresetDialogStatePanel extends StatelessWidget {
  final IconData icon;
  final String message;
  final String actionLabel;
  final VoidCallback? onAction;

  const _EnvPresetDialogStatePanel({
    required this.icon,
    required this.message,
    this.actionLabel = '',
    this.onAction,
  });

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: 180,
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(icon, size: 42, color: AppColors.textMuted),
          const SizedBox(height: 12),
          Text(
            message,
            textAlign: TextAlign.center,
            style: const TextStyle(fontSize: 13, color: AppColors.textMuted),
          ),
          if (onAction != null && actionLabel.isNotEmpty) ...[
            const SizedBox(height: 14),
            AppButton(
              label: actionLabel,
              icon: Icons.refresh_rounded,
              outlined: true,
              onPressed: onAction,
              width: 110,
            ),
          ],
        ],
      ),
    );
  }
}

String _formatEnvMap(Map<String, String> env) {
  final entries = env.entries.toList()
    ..sort((left, right) => left.key.compareTo(right.key));
  return entries.map((entry) => '${entry.key}=${entry.value}').join('\n');
}

Map<String, String> _parseEnvText(String text) {
  final result = <String, String>{};
  for (final raw in text.split('\n')) {
    final line = raw.trim();
    if (line.isEmpty || line.startsWith('#')) continue;
    final index = line.indexOf('=');
    if (index <= 0) continue;
    final key = line.substring(0, index).trim();
    if (key.isEmpty) continue;
    result[key] = line.substring(index + 1).trim();
  }
  return result;
}

bool _envMapEquals(Map<String, String> left, Map<String, String> right) {
  if (left.length != right.length) return false;
  for (final entry in left.entries) {
    if (right[entry.key] != entry.value) return false;
  }
  return true;
}

List<String> _reservedKeys(Map<String, String> env) {
  return env.keys
      .where((key) => key.trim().toUpperCase().startsWith('OPENCODE_RELAY_'))
      .toList();
}
