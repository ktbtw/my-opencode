import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/services/app_log_service.dart';
import '../../../core/services/feature_guide_service.dart';
import '../../../core/notifications/app_notification_feedback.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../core/utils/provider_console_url.dart';
import '../../../shared/widgets/widgets.dart';
import '../../../shared/widgets/step_guide_dialog.dart';
import '../../chat/data/chat_model.dart';
import '../../chat/presentation/chat_provider.dart';
import '../data/device_ai_config_model.dart';
import '../data/device_model.dart';
import 'device_provider.dart';
import 'widgets/model_speed_test_button.dart';
import 'widgets/model_thinking_editor.dart';
import 'widgets/raw_config_dialog.dart';

class DeviceAIConfigPage extends ConsumerStatefulWidget {
  final String machineId;
  final String agentId;
  final String projectId;

  const DeviceAIConfigPage({
    super.key,
    required this.machineId,
    required this.agentId,
    required this.projectId,
  });

  @override
  ConsumerState<DeviceAIConfigPage> createState() => _DeviceAIConfigPageState();
}

class _DeviceAIConfigPageState extends ConsumerState<DeviceAIConfigPage> {
  DeviceAIConfigInfo? _config;
  bool _loading = true;
  bool _saving = false;
  bool _textSaving = false;
  bool _guideOpening = false;
  String _error = '';

  String get _pageTitle => '设备级全局配置';

  String get _pageSubtitle => 'Machine ID: ${widget.machineId}';

  String get _pageHint =>
      '这里管理的是设备全局 opencode 配置里的 AI 供应商。新增、编辑、删改模型都会影响这台设备上的所有 agent。';

  String get _activeProviderId => _config?.provider.trim() ?? '';
  String get _activeModelId => _config?.model.trim() ?? '';

  @override
  void initState() {
    super.initState();
    _load();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _showApiGuide(automatic: true);
    });
  }

  @override
  void didUpdateWidget(covariant DeviceAIConfigPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.machineId != widget.machineId) {
      _config = null;
      _load();
    }
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      await AppLogService.log(
        'device_ai_config_load_started',
        data: {'machine_id': widget.machineId},
      );
      final repo = ref.read(deviceRepositoryProvider);
      final config = await repo.getDeviceAIConfig(widget.machineId);
      await AppLogService.log(
        'device_ai_config_load_completed',
        data: {
          'machine_id': widget.machineId,
          'provider_count': config.providers.length,
          'selected_provider': config.provider,
          'selected_model': config.model,
        },
      );
      if (!mounted) return;
      setState(() {
        _config = config;
      });
    } catch (error) {
      await AppLogService.log(
        'device_ai_config_load_failed',
        level: 'error',
        data: {'machine_id': widget.machineId, 'error': error.toString()},
      );
      if (!mounted) return;
      setState(() {
        _config = null;
        _error = error.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  void _showMessage(String text) {
    showAppFeedback(context, message: text);
  }

  Future<void> _showApiGuide({bool automatic = false}) async {
    if (_guideOpening || !mounted) return;
    if (automatic &&
        await FeatureGuideService.hasSeen(
          FeatureGuideService.apiConfigGuideKey,
        )) {
      return;
    }
    if (!mounted) return;
    _guideOpening = true;
    final startProviderSetup = await showStepGuideDialog(
      context,
      title: 'AI 配置指南',
      subtitle: '按下面顺序完成一次配置，设备上的所有 Agent 都可以使用。',
      finalActionLabel: '开始新增供应商',
      steps: const [
        GuideStep(
          title: '先确认配置范围',
          description: '这里是设备级全局 AI 配置。供应商、模型和默认模型会作用于当前设备上的所有 Agent。',
          icon: Icons.devices_other_outlined,
          details: [
            '先确保对应设备在线，且 Launcher 已完成登录和接管。',
            '如果只想影响单个 Agent，请在 Agent 对话或 Agent 配置中使用单独的模型设置。',
          ],
        ),
        GuideStep(
          title: '新增 AI 供应商',
          description: '点击页面中的“新增供应商”，填写供应商名称、接口地址、API Key 和 API 模式。',
          icon: Icons.add_business_outlined,
          details: [
            '接口地址填写供应商的 API 根地址，不要把模型名称拼到地址末尾。',
            'Responses API 和 Chat API 按供应商实际支持的接口选择。',
            'API Key 只保存在设备配置中，编辑已有供应商时留空表示保留原 Key。',
          ],
        ),
        GuideStep(
          title: '同步并选择模型',
          description: '供应商保存后，在对应卡片中打开模型管理，刷新供应商返回的模型列表，再勾选要提供给 Agent 的模型。',
          icon: Icons.view_list_outlined,
          details: [
            '第三方中转或自定义模型没有自动返回时，可以使用原始配置编辑器补充模型信息。',
            '模型列表只保留实际需要的模型，能让对话页的模型选择更清晰。',
          ],
        ),
        GuideStep(
          title: '设置默认模型并验证',
          description: '在模型管理中指定默认模型，回到对话页发送一条短消息确认配置已经生效。',
          icon: Icons.check_circle_outline,
          details: [
            '默认模型是没有单独选择模型时的回退模型。',
            '如果模型列表为空，优先检查接口地址、API Key、API 模式和设备在线状态。',
          ],
        ),
      ],
    );
    await FeatureGuideService.markSeen(FeatureGuideService.apiConfigGuideKey);
    if (!mounted) return;
    _guideOpening = false;
    if (startProviderSetup) {
      await _openProviderEditor(continueToModels: true);
    }
  }

  void _refreshModelCaches() {
    ref.invalidate(availableModelsProvider(widget.machineId.trim()));
    ref.invalidate(availableModelsProvider(''));
  }

  Future<DeviceAIProviderInfo?> _openProviderEditor({
    DeviceAIProviderInfo? current,
    bool continueToModels = false,
  }) async {
    final draft = await showDialog<_ProviderDraft>(
      context: context,
      builder: (context) => _ProviderEditorDialog(initial: current),
    );
    if (draft == null) return null;
    final provider = await _saveProvider(draft, current);
    if (continueToModels && current == null && provider != null && mounted) {
      await _openModelsEditor(provider);
    }
    return provider;
  }

  Future<DeviceAIProviderInfo?> _saveProvider(
    _ProviderDraft draft,
    DeviceAIProviderInfo? current,
  ) async {
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      await AppLogService.log(
        current == null
            ? 'device_ai_provider_create_started'
            : 'device_ai_provider_update_started',
        data: {
          'machine_id': widget.machineId,
          'provider': draft.id,
          'base_url': draft.baseUrl,
          'api_mode': draft.apiMode,
        },
      );
      final repo = ref.read(deviceRepositoryProvider);
      final config = await repo.saveDeviceAIConfig(
        machineId: widget.machineId,
        provider: draft.id,
        baseUrl: draft.baseUrl,
        consoleUrl: draft.consoleUrl,
        apiKey: draft.apiKey,
        apiMode: draft.apiMode,
        model: '',
        models: current?.models ?? const [],
      );
      await AppLogService.log(
        current == null
            ? 'device_ai_provider_create_completed'
            : 'device_ai_provider_update_completed',
        data: {'machine_id': widget.machineId, 'provider': draft.id},
      );
      if (!mounted) return null;
      setState(() {
        _config = config;
      });
      _refreshModelCaches();
      _showMessage(current == null ? '供应商已新增' : '供应商已更新');
      return config.providers.firstWhere(
        (item) => item.id == draft.id,
        orElse: () => DeviceAIProviderInfo(
          id: draft.id,
          baseUrl: draft.baseUrl,
          consoleUrl: draft.consoleUrl,
          apiMode: draft.apiMode,
        ),
      );
    } catch (error) {
      await AppLogService.log(
        current == null
            ? 'device_ai_provider_create_failed'
            : 'device_ai_provider_update_failed',
        level: 'error',
        data: {
          'machine_id': widget.machineId,
          'provider': draft.id,
          'error': error.toString(),
        },
      );
      if (!mounted) return null;
      setState(() {
        _error = error.toString();
      });
      return null;
    } finally {
      if (mounted) {
        setState(() {
          _saving = false;
        });
      }
    }
  }

  Future<List<DeviceAIModelInfo>> _fetchProviderModels(
    DeviceAIProviderInfo provider,
  ) async {
    final repo = ref.read(deviceRepositoryProvider);
    final config = await repo.listDeviceAIModels(
      machineId: widget.machineId,
      provider: provider.id,
      baseUrl: provider.baseUrl,
      consoleUrl: provider.consoleUrl,
      apiKey: '',
      apiMode: provider.apiMode,
      model: provider.id == _activeProviderId ? _activeModelId : '',
      models: provider.models,
    );
    for (final item in config.providers) {
      if (item.id == provider.id) {
        return item.models;
      }
    }
    return config.models;
  }

  Future<ModelTestTarget> _requireModelTestTarget() async {
    final device = await ref.read(deviceDetailProvider(widget.machineId).future);
    final target = resolveModelTestTarget(
      preferredAgentId: widget.agentId,
      preferredProjectId: widget.projectId,
      agents: device.agents,
    );
    if (target == null) {
      throw StateError('当前设备没有在线 Agent，无法测试模型连接');
    }
    return target;
  }

  Future<ModelLatencyTestResult> _testProviderModel(
    DeviceAIProviderInfo provider,
    DeviceAIModelInfo item,
  ) async {
    final target = await _requireModelTestTarget();
    return ref.read(chatRepositoryProvider).testModelLatency(
      agentId: target.agentId,
      projectId: target.projectId,
      model: ModelInfo(
        providerID: provider.id,
        providerBaseUrl: provider.baseUrl,
        providerConsoleUrl: provider.consoleUrl,
        modelID: item.id,
        name: item.name.isEmpty ? item.id : item.name,
        variants: item.variants.keys.toList(),
      ),
    );
  }

  Future<void> _openModelsEditor(DeviceAIProviderInfo provider) async {
    final result = await showDialog<_ProviderModelResult>(
      context: context,
      builder: (context) => ProviderModelsDialog(
        provider: provider,
        items: provider.models,
        initialSelected: provider.models.map((item) => item.id).toSet(),
        initialDefault: provider.id == _activeProviderId ? _activeModelId : '',
        onRefresh: () => _fetchProviderModels(provider),
        onTestModel: (item) => _testProviderModel(provider, item),
      ),
    );
    if (result == null) return;
    await _saveProviderModels(provider, result);
  }

  Future<void> _saveProviderModels(
    DeviceAIProviderInfo provider,
    _ProviderModelResult result,
  ) async {
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      await AppLogService.log(
        'device_ai_provider_models_save_started',
        data: {
          'machine_id': widget.machineId,
          'provider': provider.id,
          'model_count': result.selectedModels.length,
          'default_model': result.defaultModel,
        },
      );
      final repo = ref.read(deviceRepositoryProvider);
      final config = await repo.saveDeviceAIConfig(
        machineId: widget.machineId,
        provider: provider.id,
        baseUrl: provider.baseUrl,
        consoleUrl: provider.consoleUrl,
        apiKey: '',
        apiMode: provider.apiMode,
        model: result.defaultModel,
        models: result.selectedModels,
        force: true,
      );
      await AppLogService.log(
        'device_ai_provider_models_save_completed',
        data: {
          'machine_id': widget.machineId,
          'provider': provider.id,
          'model_count': result.selectedModels.length,
          'default_model': result.defaultModel,
        },
      );
      if (!mounted) return;
      setState(() {
        _config = config;
      });
      _refreshModelCaches();
      _showMessage('模型配置已保存');
    } catch (error) {
      await AppLogService.log(
        'device_ai_provider_models_save_failed',
        level: 'error',
        data: {
          'machine_id': widget.machineId,
          'provider': provider.id,
          'error': error.toString(),
        },
      );
      if (!mounted) return;
      setState(() {
        _error = error.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _saving = false;
        });
      }
    }
  }

  Future<void> _removeProvider(DeviceAIProviderInfo provider) async {
    final deletingDefault = provider.id == _activeProviderId;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除供应商'),
        content: Text(
          deletingDefault
              ? '确定删除“${provider.id}”吗？当前默认模型也会一起清空。'
              : '确定删除“${provider.id}”吗？',
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
    if (confirmed != true) return;
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      await AppLogService.log(
        'device_ai_provider_remove_started',
        data: {'machine_id': widget.machineId, 'provider': provider.id},
      );
      final repo = ref.read(deviceRepositoryProvider);
      final config = await repo.clearDeviceAIProvider(
        machineId: widget.machineId,
        provider: provider.id,
      );
      await AppLogService.log(
        'device_ai_provider_remove_completed',
        data: {'machine_id': widget.machineId, 'provider': provider.id},
      );
      if (!mounted) return;
      setState(() {
        _config = config;
      });
      _refreshModelCaches();
      _showMessage('供应商已删除');
    } catch (error) {
      await AppLogService.log(
        'device_ai_provider_remove_failed',
        level: 'error',
        data: {
          'machine_id': widget.machineId,
          'provider': provider.id,
          'error': error.toString(),
        },
      );
      if (!mounted) return;
      setState(() {
        _error = error.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _saving = false;
        });
      }
    }
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
      _textSaving = true;
      _error = '';
    });
    try {
      await AppLogService.log(
        'device_ai_config_text_save_started',
        data: {'machine_id': widget.machineId, 'text_length': text.length},
      );
      final repo = ref.read(deviceRepositoryProvider);
      final config = await repo.saveDeviceAIConfigText(
        machineId: widget.machineId,
        text: text,
      );
      await AppLogService.log(
        'device_ai_config_text_save_completed',
        data: {'machine_id': widget.machineId},
      );
      if (!mounted) return;
      setState(() {
        _config = config;
      });
      _refreshModelCaches();
      _showMessage('全局配置文件已保存');
    } catch (error) {
      await AppLogService.log(
        'device_ai_config_text_save_failed',
        level: 'error',
        data: {'machine_id': widget.machineId, 'error': error.toString()},
      );
      if (!mounted) return;
      setState(() {
        _error = error.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _textSaving = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: PageBackground(
          child: Column(
            children: [
              AppTopBar(
                title: 'AI配置',
                leading: IconButton(
                  icon: const Icon(Icons.arrow_back_ios_new, size: 16),
                  onPressed: () => Navigator.of(context).pop(),
                ),
                actions: [
                  IconButton(
                    tooltip: '编辑全局配置',
                    onPressed: _loading || _saving || _textSaving
                        ? null
                        : _openRawEditor,
                    icon: const Icon(Icons.edit_note_rounded),
                  ),
                  IconButton(
                    tooltip: '新增供应商',
                    onPressed: _loading || _saving || _textSaving
                        ? null
                        : () => _openProviderEditor(),
                    icon: const Icon(Icons.add_rounded),
                  ),
                ],
              ),
              Expanded(
                child: _loading && _config == null
                    ? const DeviceAIConfigSkeleton()
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
                                    Align(
                                      alignment: Alignment.centerLeft,
                                      child: TextButton.icon(
                                        onPressed:
                                            _loading || _saving || _textSaving
                                            ? null
                                            : _showApiGuide,
                                        icon: const Icon(
                                          Icons.menu_book_outlined,
                                          size: 16,
                                        ),
                                        label: const Text('查看配置指南'),
                                      ),
                                    ),
                                    if (_config != null &&
                                        _config!.configPath.isNotEmpty) ...[
                                      const SizedBox(height: 10),
                                      Text(
                                        '配置文件：${_config!.configPath}',
                                        style: const TextStyle(
                                          fontSize: 12,
                                          color: AppColors.textMuted,
                                        ),
                                      ),
                                    ],
                                    if (_config != null &&
                                        _config!.warning.isNotEmpty) ...[
                                      const SizedBox(height: 8),
                                      Text(
                                        _config!.warning,
                                        style: const TextStyle(
                                          fontSize: 12,
                                          color: AppColors.statusWarning,
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
                              const SizedBox(height: 16),
                              if (_config != null &&
                                  _config!.providers.isNotEmpty)
                                ..._config!.providers.map(
                                  (provider) => Padding(
                                    padding: const EdgeInsets.only(bottom: 12),
                                    child: _ProviderCard(
                                      provider: provider,
                                      isCurrent:
                                          provider.id == _activeProviderId,
                                      currentModel:
                                          provider.id == _activeProviderId
                                          ? _activeModelId
                                          : '',
                                      busy: _saving || _textSaving,
                                      onEdit: () => _openProviderEditor(
                                        current: provider,
                                      ),
                                      onManageModels: () =>
                                          _openModelsEditor(provider),
                                      onDelete: () => _removeProvider(provider),
                                    ),
                                  ),
                                )
                              else
                                const EmptyState(
                                  message: '当前设备还没有配置 AI 供应商',
                                  icon: Icons.cloud_off_outlined,
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

class _ProviderCard extends StatelessWidget {
  final DeviceAIProviderInfo provider;
  final bool isCurrent;
  final String currentModel;
  final bool busy;
  final VoidCallback onEdit;
  final VoidCallback onManageModels;
  final VoidCallback onDelete;

  const _ProviderCard({
    required this.provider,
    required this.isCurrent,
    required this.currentModel,
    required this.busy,
    required this.onEdit,
    required this.onManageModels,
    required this.onDelete,
  });

  @override
  Widget build(BuildContext context) {
    return PanelCard(
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
                      provider.id,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 6),
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      children: [
                        StatusPill(
                          label: isCurrent ? '当前默认' : '已配置',
                          type: isCurrent
                              ? StatusType.online
                              : StatusType.processing,
                        ),
                        StatusPill(
                          label: '模型 ${provider.models.length.toString()} 个',
                          type: provider.models.isEmpty
                              ? StatusType.offline
                              : StatusType.warning,
                        ),
                        StatusPill(
                          label: provider.apiMode == 'responses'
                              ? 'Responses'
                              : 'Chat',
                          type: provider.apiMode == 'responses'
                              ? StatusType.warning
                              : StatusType.processing,
                        ),
                      ],
                    ),
                  ],
                ),
              ),
              IconButton(
                tooltip: '管理模型',
                onPressed: busy ? null : onManageModels,
                icon: const Icon(Icons.layers_outlined),
              ),
              IconButton(
                tooltip: '编辑供应商',
                onPressed: busy ? null : onEdit,
                icon: const Icon(Icons.edit_outlined),
              ),
              IconButton(
                tooltip: '删除供应商',
                onPressed: busy ? null : onDelete,
                icon: const Icon(Icons.delete_outline_rounded),
              ),
            ],
          ),
          const SizedBox(height: 12),
          _InfoRow(
            label: '接口地址',
            value: provider.baseUrl.isEmpty ? '未设置' : provider.baseUrl,
          ),
          const SizedBox(height: 8),
          _InfoRow(
            label: 'API Key',
            value: provider.apiKeyMasked.isEmpty
                ? '未设置'
                : provider.apiKeyMasked,
          ),
          const SizedBox(height: 8),
          _InfoRow(
            label: '接口模式',
            value: provider.apiMode == 'responses'
                ? 'Responses API'
                : 'Chat API',
          ),
          const SizedBox(height: 8),
          _InfoRow(
            label: '默认模型',
            value: isCurrent
                ? (currentModel.isEmpty ? '未设置' : currentModel)
                : '未切换到该供应商',
          ),
        ],
      ),
    );
  }
}

class _InfoRow extends StatelessWidget {
  final String label;
  final String value;

  const _InfoRow({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 68,
          child: Text(
            label,
            style: const TextStyle(fontSize: 12, color: AppColors.textMuted),
          ),
        ),
        Expanded(
          child: Text(
            value,
            style: const TextStyle(
              fontSize: 12,
              color: AppColors.textSecondary,
              height: 1.4,
            ),
          ),
        ),
      ],
    );
  }
}

class _ProviderDraft {
  final String id;
  final String baseUrl;
  final String consoleUrl;
  final String apiKey;
  final String apiMode;

  const _ProviderDraft({
    required this.id,
    required this.baseUrl,
    required this.consoleUrl,
    required this.apiKey,
    required this.apiMode,
  });
}

class _ProviderEditorDialog extends StatefulWidget {
  final DeviceAIProviderInfo? initial;

  const _ProviderEditorDialog({this.initial});

  @override
  State<_ProviderEditorDialog> createState() => _ProviderEditorDialogState();
}

class _ProviderEditorDialogState extends State<_ProviderEditorDialog> {
  late final TextEditingController _idController;
  late final TextEditingController _baseUrlController;
  late final TextEditingController _consoleUrlController;
  late final TextEditingController _apiKeyController;
  late String _apiMode;
  late final String _savedMasked;
  bool _apiKeyDirty = false;
  String _error = '';

  @override
  void initState() {
    super.initState();
    _savedMasked = widget.initial?.apiKeyMasked ?? '';
    _idController = TextEditingController(text: widget.initial?.id ?? '');
    _baseUrlController = TextEditingController(
      text: widget.initial?.baseUrl ?? '',
    );
    _consoleUrlController = TextEditingController(
      text: widget.initial?.consoleUrl ?? '',
    );
    _apiMode = widget.initial?.apiMode == 'chat' ? 'chat' : 'responses';
    _apiKeyController = TextEditingController(text: _savedMasked);
    _apiKeyController.addListener(() {
      final dirty =
          _savedMasked.isEmpty || _apiKeyController.text != _savedMasked;
      if (dirty == _apiKeyDirty) return;
      setState(() {
        _apiKeyDirty = dirty;
      });
    });
  }

  @override
  void dispose() {
    _idController.dispose();
    _baseUrlController.dispose();
    _consoleUrlController.dispose();
    _apiKeyController.dispose();
    super.dispose();
  }

  void _submit() {
    final id = _idController.text.trim();
    final baseUrl = _baseUrlController.text.trim();
    final consoleUrl = _consoleUrlController.text.trim();
    if (id.isEmpty) {
      setState(() {
        _error = '请先填写供应商名称';
      });
      return;
    }
    if (baseUrl.isEmpty) {
      setState(() {
        _error = '请先填写接口地址';
      });
      return;
    }
    if (consoleUrl.isNotEmpty &&
        normalizeProviderConsoleUrl(consoleUrl) == null) {
      setState(() {
        _error = '控制台网址需要使用有效的 http 或 https 地址';
      });
      return;
    }
    var apiKey = _apiKeyController.text.trim();
    if (!_apiKeyDirty && apiKey == _savedMasked) {
      apiKey = '';
    }
    Navigator.of(context).pop(
      _ProviderDraft(
        id: id,
        baseUrl: baseUrl,
        consoleUrl: consoleUrl,
        apiKey: apiKey,
        apiMode: _apiMode,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final editing = widget.initial != null;
    final viewport = MediaQuery.sizeOf(context);
    final dialogWidth = math.min(620.0, viewport.width - 32);
    final dialogHeight = math.min(620.0, math.max(320.0, viewport.height - 40));
    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
      shape: RoundedRectangleBorder(borderRadius: AppRadius.lgRadius),
      child: SizedBox(
        width: dialogWidth,
        height: dialogHeight,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                editing ? '编辑供应商' : '新增供应商',
                style: const TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 6),
              const Text(
                '这里先维护供应商基础信息；模型启用和默认模型切换在卡片的模型按钮里完成。',
                style: TextStyle(fontSize: 12, color: AppColors.textMuted),
              ),
              const SizedBox(height: 14),
              Expanded(
                child: SingleChildScrollView(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      TextField(
                        controller: _idController,
                        readOnly: editing,
                        decoration: const InputDecoration(labelText: '供应商名称'),
                      ),
                      const SizedBox(height: 12),
                      TextField(
                        controller: _baseUrlController,
                        decoration: const InputDecoration(labelText: '接口地址'),
                      ),
                      const SizedBox(height: 12),
                      TextField(
                        controller: _consoleUrlController,
                        keyboardType: TextInputType.url,
                        decoration: InputDecoration(
                          labelText: '控制台网址（可选）',
                          hintText: '留空时从接口地址自动推导',
                          helperText:
                              deriveProviderConsoleUrl(
                                    _baseUrlController.text,
                                  ) ==
                                  null
                              ? null
                              : '自动推导：${deriveProviderConsoleUrl(_baseUrlController.text)}',
                        ),
                      ),
                      const SizedBox(height: 12),
                      AppSelect<String>(
                        value: _apiMode,
                        label: '接口模式',
                        options: const [
                          AppSelectOption(
                            value: 'responses',
                            label: 'Responses API',
                          ),
                          AppSelectOption(value: 'chat', label: 'Chat API'),
                        ],
                        onChanged: (value) {
                          setState(() {
                            _apiMode = value;
                          });
                        },
                      ),
                      const SizedBox(height: 12),
                      TextField(
                        controller: _apiKeyController,
                        decoration: InputDecoration(
                          labelText: 'API Key',
                          hintText: editing
                              ? '留空或保持脱敏值表示沿用已保存 Key'
                              : '请输入 API Key',
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
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 16),
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
                      label: editing ? '保存修改' : '新增供应商',
                      onPressed: _submit,
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _ProviderModelResult {
  final Set<String> selected;
  final List<DeviceAIModelInfo> selectedModels;
  final String defaultModel;

  const _ProviderModelResult({
    required this.selected,
    required this.selectedModels,
    required this.defaultModel,
  });
}

class ProviderModelsDialog extends StatefulWidget {
  final DeviceAIProviderInfo provider;
  final List<DeviceAIModelInfo> items;
  final Set<String> initialSelected;
  final String initialDefault;
  final Future<List<DeviceAIModelInfo>> Function()? onRefresh;
  final Future<ModelLatencyTestResult> Function(DeviceAIModelInfo model)?
  onTestModel;

  const ProviderModelsDialog({
    super.key,
    required this.provider,
    required this.items,
    required this.initialSelected,
    required this.initialDefault,
    this.onRefresh,
    this.onTestModel,
  });

  @override
  State<ProviderModelsDialog> createState() => _ProviderModelsDialogState();
}

class _ProviderModelsDialogState extends State<ProviderModelsDialog> {
  late List<DeviceAIModelInfo> _items = List<DeviceAIModelInfo>.from(
    widget.items,
  );
  late Set<String> _selected = Set<String>.from(widget.initialSelected);
  late String _defaultModel = widget.initialDefault;
  final TextEditingController _searchController = TextEditingController();
  final FocusNode _searchFocusNode = FocusNode();
  bool _searchVisible = false;
  bool _loading = false;
  bool _mobileEditing = false;
  bool _batchTesting = false;
  String _activeModelId = '';
  String _error = '';
  final Set<String> _testingModelIds = <String>{};
  final Map<String, ModelLatencyTestResult> _testResults = {};

  bool get _requireDefault => widget.initialDefault.isNotEmpty;

  @override
  void initState() {
    super.initState();
    _activeModelId = _items.any((item) => item.id == _defaultModel)
        ? _defaultModel
        : _items.isEmpty
        ? ''
        : _items.first.id;
    _searchFocusNode.addListener(_handleSearchFocusChange);
    if (_requireDefault && !_selected.contains(_defaultModel)) {
      _defaultModel = _firstSelectedId();
    }
    if (!_requireDefault && !_selected.contains(_defaultModel)) {
      _defaultModel = '';
    }
    if (_items.isEmpty && widget.onRefresh != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) {
          _refresh();
        }
      });
    }
  }

  @override
  void dispose() {
    _searchFocusNode
      ..removeListener(_handleSearchFocusChange)
      ..dispose();
    _searchController.dispose();
    super.dispose();
  }

  List<DeviceAIModelInfo> get _visibleItems {
    final query = _searchController.text.trim().toLowerCase();
    if (query.isEmpty) return _items;
    return _items.where((item) {
      return item.id.toLowerCase().contains(query) ||
          item.name.toLowerCase().contains(query);
    }).toList();
  }

  void _handleSearchFocusChange() {
    if (!_searchFocusNode.hasFocus && _searchVisible) {
      _hideSearch();
    }
  }

  void _showSearch() {
    setState(() {
      _searchVisible = true;
    });
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && _searchVisible) {
        _searchFocusNode.requestFocus();
      }
    });
  }

  void _hideSearch() {
    if (!mounted) return;
    _searchController.clear();
    setState(() {
      _searchVisible = false;
    });
  }

  void _clearSearch() {
    _searchController.clear();
    setState(() {});
    _searchFocusNode.requestFocus();
  }

  String _firstSelectedId() {
    for (final item in _items) {
      if (_selected.contains(item.id)) return item.id;
    }
    return '';
  }

  void _toggle(DeviceAIModelInfo item) {
    setState(() {
      if (_selected.contains(item.id)) {
        _selected.remove(item.id);
      } else {
        _selected.add(item.id);
      }
      if (!_selected.contains(_defaultModel)) {
        _defaultModel = _requireDefault ? _firstSelectedId() : '';
      }
      _error = '';
    });
  }

  Future<void> _refresh() async {
    final action = widget.onRefresh;
    if (action == null) return;
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      final items = await action();
      final ids = items.map((item) => item.id).toSet();
      setState(() {
        _items = items;
        _selected = _selected.where(ids.contains).toSet();
        if (!ids.contains(_activeModelId)) {
          _activeModelId = items.isEmpty ? '' : items.first.id;
          _mobileEditing = false;
        }
        if (_requireDefault && !_selected.contains(_defaultModel)) {
          _defaultModel = _firstSelectedId();
        }
        if (!_requireDefault && !_selected.contains(_defaultModel)) {
          _defaultModel = '';
        }
      });
    } catch (error) {
      setState(() {
        _error = error.toString();
      });
    } finally {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  DeviceAIModelInfo? get _activeModel {
    for (final item in _items) {
      if (item.id == _activeModelId) return item;
    }
    return _items.isEmpty ? null : _items.first;
  }

  Duration? _testDuration(String modelId) {
    final result = _testResults[modelId];
    if (result == null) return null;
    return modelSpeedTestDuration(result);
  }

  Future<void> _testModel(DeviceAIModelInfo item) async {
    final tester = widget.onTestModel;
    if (tester == null || _testingModelIds.contains(item.id)) return;
    setState(() {
      _testingModelIds.add(item.id);
      _testResults.remove(item.id);
      _error = '';
    });
    try {
      final result = await tester(item);
      if (!mounted) return;
      setState(() {
        _testingModelIds.remove(item.id);
        _testResults[item.id] = result;
      });
    } catch (error) {
      if (!mounted) return;
      final now = DateTime.now();
      final message = error.toString().replaceFirst('Bad state: ', '');
      setState(() {
        _testingModelIds.remove(item.id);
        _error = message;
        _testResults[item.id] = ModelLatencyTestResult(
          taskId: '',
          sessionId: '',
          model: '${widget.provider.id}/${item.id}',
          startedAt: now,
          completedAt: now,
          success: false,
          error: message,
        );
      });
    }
  }

  Future<void> _testSelectedModels() async {
    if (_batchTesting || widget.onTestModel == null) return;
    final selected = _items
        .where((item) => _selected.contains(item.id))
        .toList();
    if (selected.isEmpty) {
      setState(() {
        _error = '请先勾选要测试的模型';
      });
      return;
    }
    setState(() {
      _batchTesting = true;
      _error = '';
    });
    try {
      for (final item in selected) {
        if (!mounted) break;
        await _testModel(item);
      }
    } finally {
      if (mounted) {
        setState(() {
          _batchTesting = false;
        });
      }
    }
  }

  void _openThinking(DeviceAIModelInfo item, {required bool mobile}) {
    setState(() {
      _activeModelId = item.id;
      _mobileEditing = mobile;
    });
  }

  void _replaceModel(DeviceAIModelInfo updated) {
    final index = _items.indexWhere((item) => item.id == updated.id);
    if (index < 0) return;
    setState(() {
      _items[index] = updated;
    });
  }

  void _submit() {
    if (_requireDefault && _selected.isEmpty) {
      setState(() {
        _error = '当前默认供应商至少保留一个模型；如果想清空，请先切换默认模型到别的供应商。';
      });
      return;
    }
    if (_requireDefault &&
        (_defaultModel.isEmpty || !_selected.contains(_defaultModel))) {
      setState(() {
        _error = '请为当前默认供应商指定一个默认模型。';
      });
      return;
    }
    if (_defaultModel.isNotEmpty && !_selected.contains(_defaultModel)) {
      setState(() {
        _error = '默认模型必须来自当前已选模型。';
      });
      return;
    }
    Navigator.of(context).pop(
      _ProviderModelResult(
        selected: _selected,
        selectedModels: _items
            .where((item) => _selected.contains(item.id))
            .toList(),
        defaultModel: _defaultModel,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final viewport = MediaQuery.sizeOf(context);
    final mobileDialog = viewport.width < 720;
    return Dialog(
      insetPadding: mobileDialog
          ? EdgeInsets.zero
          : const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
      shape: RoundedRectangleBorder(
        borderRadius: mobileDialog ? BorderRadius.zero : AppRadius.lgRadius,
      ),
      child: SizedBox(
        width: mobileDialog ? viewport.width : 1000,
        height: mobileDialog ? viewport.height : 760,
        child: Column(
          children: [
            Container(
              constraints: const BoxConstraints(minHeight: 62),
              padding: const EdgeInsets.only(left: 18, right: 10),
              decoration: const BoxDecoration(
                border: Border(bottom: BorderSide(color: AppColors.border)),
              ),
              child: Row(
                children: [
                  Expanded(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '管理模型 · ${widget.provider.id}',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontSize: 16,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        if (!mobileDialog) ...[
                          const SizedBox(height: 3),
                          Text(
                            widget.provider.baseUrl.isEmpty
                                ? '请先为该供应商填写接口地址和 API Key。'
                                : widget.provider.baseUrl,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 10,
                              color: AppColors.textMuted,
                            ),
                          ),
                        ],
                      ],
                    ),
                  ),
                  if (!mobileDialog || !_mobileEditing)
                    IconButton(
                      tooltip: '搜索模型',
                      icon: const Icon(Icons.search_rounded),
                      onPressed: _showSearch,
                    ),
                  IconButton(
                    tooltip: '关闭',
                    icon: const Icon(Icons.close_rounded),
                    onPressed: () => Navigator.of(context).pop(),
                  ),
                ],
              ),
            ),
            Expanded(
              child: LayoutBuilder(
                builder: (context, constraints) {
                  final mobile = constraints.maxWidth < 720;
                  final active = _activeModel;
                  if (mobile) {
                    return AnimatedSwitcher(
                      duration: const Duration(milliseconds: 180),
                      child: _mobileEditing && active != null
                          ? ModelThinkingEditor(
                              key: ValueKey('thinking-${active.id}'),
                              model: active,
                              onChanged: _replaceModel,
                              onBack: () => setState(() {
                                _mobileEditing = false;
                              }),
                            )
                          : _buildModelPane(mobile: true),
                    );
                  }
                  return Row(
                    children: [
                      SizedBox(
                        width: 360,
                        child: _buildModelPane(mobile: false),
                      ),
                      const VerticalDivider(width: 1),
                      Expanded(
                        child: active == null
                            ? const Center(
                                child: Text(
                                  '选择一个模型查看能力配置',
                                  style: TextStyle(
                                    fontSize: 12,
                                    color: AppColors.textMuted,
                                  ),
                                ),
                              )
                            : ModelThinkingEditor(
                                key: ValueKey('thinking-${active.id}'),
                                model: active,
                                onChanged: _replaceModel,
                              ),
                      ),
                    ],
                  );
                },
              ),
            ),
            Container(
              constraints: const BoxConstraints(minHeight: 66),
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
              decoration: const BoxDecoration(
                color: AppColors.surfaceElevated,
                border: Border(top: BorderSide(color: AppColors.border)),
              ),
              child: Row(
                children: [
                  if (!mobileDialog)
                    const Expanded(
                      child: Row(
                        children: [
                          Icon(
                            Icons.shield_outlined,
                            size: 15,
                            color: AppColors.statusSuccess,
                          ),
                          SizedBox(width: 6),
                          Text(
                            '能力配置只作用于对应模型',
                            style: TextStyle(
                              fontSize: 10,
                              color: AppColors.textMuted,
                            ),
                          ),
                        ],
                      ),
                    )
                  else
                    const Spacer(),
                  SizedBox(
                    width: mobileDialog ? 116 : 100,
                    child: AppButton(
                      label: '取消',
                      outlined: true,
                      onPressed: () => Navigator.of(context).pop(),
                    ),
                  ),
                  const SizedBox(width: 10),
                  SizedBox(
                    width: 168,
                    child: AppButton(
                      label: '保存模型配置',
                      icon: Icons.save_outlined,
                      onPressed: _submit,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildModelPane({required bool mobile}) {
    final defaultItems = _items
        .where((item) => _selected.contains(item.id))
        .toList();
    final visibleItems = _visibleItems;
    return Container(
      color: AppColors.surfaceElevated,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Container(
            padding: EdgeInsets.fromLTRB(14, _searchVisible ? 12 : 10, 14, 12),
            decoration: const BoxDecoration(
              border: Border(bottom: BorderSide(color: AppColors.borderLight)),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                if (_searchVisible) ...[
                  AppInput(
                    controller: _searchController,
                    focusNode: _searchFocusNode,
                    hint: '搜索模型名称或 ID',
                    autofocus: true,
                    suffixIcon: _searchController.text.isEmpty
                        ? null
                        : IconButton(
                            tooltip: '清除搜索',
                            icon: const Icon(Icons.clear_rounded),
                            onPressed: _clearSearch,
                          ),
                    onChanged: (_) => setState(() {}),
                  ),
                  const SizedBox(height: 10),
                ],
                Wrap(
                  spacing: 7,
                  runSpacing: 7,
                  children: [
                    _QuickTextButton(
                      label: _loading ? '刷新中...' : '刷新模型',
                      onTap: _loading ? null : _refresh,
                    ),
                    _QuickTextButton(
                      label: '全选',
                      onTap: () => setState(() {
                        _selected = _items.map((item) => item.id).toSet();
                        if (_requireDefault &&
                            !_selected.contains(_defaultModel)) {
                          _defaultModel = _firstSelectedId();
                        }
                        _error = '';
                      }),
                    ),
                    _QuickTextButton(
                      label: '清空',
                      onTap: () => setState(() {
                        _selected = <String>{};
                        if (!_requireDefault) _defaultModel = '';
                        _error = '';
                      }),
                    ),
                    IconButton(
                      tooltip: '打开供应商控制台',
                      visualDensity: VisualDensity.compact,
                      icon: const Icon(Icons.open_in_browser_rounded, size: 19),
                      onPressed: () => openProviderConsole(
                        context,
                        consoleUrl: widget.provider.consoleUrl,
                        baseUrl: widget.provider.baseUrl,
                      ),
                    ),
                    ModelSpeedTestButton(
                      key: const ValueKey('model-speed-test-selected'),
                      testing: _batchTesting,
                      tooltip: '测试已勾选模型',
                      onPressed: _batchTesting || widget.onTestModel == null
                          ? null
                          : _testSelectedModels,
                    ),
                  ],
                ),
                const SizedBox(height: 10),
                AppSelect<String>(
                  value: _defaultModel,
                  label: _requireDefault ? '默认模型' : '切换默认模型（可选）',
                  options: [
                    if (!_requireDefault)
                      const AppSelectOption<String>(
                        value: '',
                        label: '保持当前默认模型不变',
                      ),
                    ...defaultItems.map(
                      (item) => AppSelectOption<String>(
                        value: item.id,
                        label: item.name.isEmpty ? item.id : item.name,
                      ),
                    ),
                  ],
                  onChanged: defaultItems.isEmpty
                      ? null
                      : (value) => setState(() {
                          _defaultModel = value;
                          _error = '';
                        }),
                ),
                if (_error.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text(
                    _error,
                    style: const TextStyle(
                      fontSize: 11,
                      color: AppColors.statusError,
                    ),
                  ),
                ],
              ],
            ),
          ),
          Expanded(
            child: _loading && _items.isEmpty
                ? const SingleChildScrollView(
                    padding: EdgeInsets.all(14),
                    child: DialogContentSkeleton(
                      itemCount: 4,
                      showHeader: false,
                    ),
                  )
                : _items.isEmpty
                ? const Center(
                    child: Padding(
                      padding: EdgeInsets.all(20),
                      child: Text(
                        '当前还没有模型，点击上方“刷新模型”从接口拉取。',
                        style: TextStyle(
                          fontSize: 12,
                          color: AppColors.textMuted,
                        ),
                        textAlign: TextAlign.center,
                      ),
                    ),
                  )
                : visibleItems.isEmpty
                ? const Center(
                    child: Text(
                      '没有匹配的模型',
                      style: TextStyle(
                        fontSize: 12,
                        color: AppColors.textMuted,
                      ),
                    ),
                  )
                : ListView.separated(
                    padding: const EdgeInsets.all(8),
                    itemCount: visibleItems.length,
                    separatorBuilder: (_, __) => const SizedBox(height: 4),
                    itemBuilder: (context, index) =>
                        _buildModelRow(visibleItems[index], mobile: mobile),
                  ),
          ),
        ],
      ),
    );
  }

  Widget _buildModelRow(DeviceAIModelInfo item, {required bool mobile}) {
    final checked = _selected.contains(item.id);
    final active = item.id == _activeModelId;
    final status = _modelThinkingStatus(item);
    return Material(
      color: active ? AppColors.primaryLight : Colors.transparent,
      borderRadius: AppRadius.mdRadius,
      child: InkWell(
        onTap: () => _openThinking(item, mobile: mobile),
        borderRadius: AppRadius.mdRadius,
        child: Container(
          constraints: const BoxConstraints(minHeight: 70),
          padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 8),
          decoration: BoxDecoration(
            borderRadius: AppRadius.mdRadius,
            border: Border.all(
              color: active ? AppColors.primaryMuted : Colors.transparent,
            ),
          ),
          child: Row(
            children: [
              Checkbox(value: checked, onChanged: (_) => _toggle(item)),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    Text(
                      item.name.isEmpty ? item.id : item.name,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      item.id,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 10,
                        color: AppColors.textMuted,
                      ),
                    ),
                    const SizedBox(height: 5),
                    Row(
                      children: [
                        Container(
                          width: 5,
                          height: 5,
                          decoration: BoxDecoration(
                            color: status.$2,
                            shape: BoxShape.circle,
                          ),
                        ),
                        const SizedBox(width: 5),
                        Expanded(
                          child: Text(
                            status.$1,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                              fontSize: 10,
                              fontWeight: FontWeight.w600,
                              color: status.$2,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
              ModelSpeedTestButton(
                key: ValueKey('model-speed-test-${item.id}'),
                testing: _testingModelIds.contains(item.id),
                failed: _testResults[item.id]?.success == false,
                duration: _testDuration(item.id),
                tooltip: '测试模型连接',
                onPressed: _testingModelIds.contains(item.id) ||
                        _batchTesting ||
                        widget.onTestModel == null
                    ? null
                    : () => _testModel(item),
              ),
              IconButton(
                tooltip: '配置思考能力',
                onPressed: () => _openThinking(item, mobile: mobile),
                icon: const Icon(Icons.tune_rounded, size: 17),
                color: active ? AppColors.primary : AppColors.textMuted,
              ),
            ],
          ),
        ),
      ),
    );
  }

  (String, Color) _modelThinkingStatus(DeviceAIModelInfo item) {
    final thinking = item.thinking;
    if (thinking?.overrideEnabled == true) {
      final count = thinking!.overrideVariants.isNotEmpty
          ? thinking.overrideVariants.length
          : item.variants.length;
      return (
        thinking.supported ? '手动覆盖 · $count 个档位' : '手动关闭思考控制',
        AppColors.statusWarning,
      );
    }
    final variants = thinking?.variants.isNotEmpty == true
        ? thinking!.variants
        : item.variants;
    if (thinking?.source == 'runtime') {
      return (
        'Runtime · ${variants.isEmpty ? '已识别' : '强度可调'}',
        AppColors.primary,
      );
    }
    if (thinking?.supported == true || variants.isNotEmpty) {
      return (
        '已检测 · ${variants.isEmpty ? '支持思考' : '强度可调'}',
        AppColors.statusSuccess,
      );
    }
    return ('未检测到思考能力', AppColors.statusOffline);
  }
}

class _QuickTextButton extends StatelessWidget {
  final String label;
  final VoidCallback? onTap;

  const _QuickTextButton({required this.label, required this.onTap});

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: AppRadius.smRadius,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
        decoration: BoxDecoration(
          color: AppColors.surfaceElevated,
          borderRadius: AppRadius.smRadius,
          border: Border.all(color: AppColors.borderLight),
        ),
        child: Text(
          label,
          style: TextStyle(
            fontSize: 12,
            color: onTap == null
                ? AppColors.textMuted
                : AppColors.textSecondary,
          ),
        ),
      ),
    );
  }
}
