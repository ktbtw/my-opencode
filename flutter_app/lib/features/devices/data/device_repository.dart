import 'dart:async';
import 'dart:io';
import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';

import '../../../core/config/api_client.dart';
import '../../../core/services/app_log_service.dart';
import '../../chat/data/chat_model.dart';
import 'device_ai_config_model.dart';
import 'device_directory_model.dart';
import 'device_env_config_model.dart';
import 'device_compaction_config_model.dart';
import 'device_launcher_model.dart';
import 'device_mcp_config_model.dart';
import 'device_model.dart';
import 'device_project_memory_settings_model.dart';
import 'device_storage_model.dart';
import 'device_semantic_agent_model.dart';
import 'device_skill_model.dart';
import 'upload_policy.dart';
import 'upload_diag.dart';
import 'upload_resume_store.dart';
import '../../../core/services/diag_log.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

typedef DeviceUploadProgressCallback =
    void Function(DeviceUploadProgress progress);
typedef DeviceDownloadProgressCallback =
    void Function(DeviceDownloadProgress progress);

class _DigestSink implements Sink<Digest> {
  Digest? value;

  @override
  void add(Digest data) {
    value = data;
  }

  @override
  void close() {}
}

class DeviceRepository {
  static const int defaultUploadChunkSize = 512 * 1024;
  static const int defaultUploadConcurrency = 3;
  static const int defaultDownloadChunkSize = 512 * 1024;
  static const int defaultDownloadConcurrency = 3;
  static const Duration defaultDownloadCreateTimeout = Duration(seconds: 8);

  /// 单个分块的请求超时基准：在传输耗时之外额外留出的等待时间。
  /// 必须大于后端等待设备回执的超时（[app] 里 upload_chunk 为 2 分钟），
  /// 否则客户端会先放弃并重试，而重试又排在设备端同一把锁后面，越试越糟。
  static const Duration _chunkBaseTimeout = Duration(seconds: 130);

  /// 单个分块的最大重试次数。
  static const int _chunkMaxAttempts = 4;

  /// 缓存服务端下发的上传策略，避免每次上传都重复请求。
  static UploadPolicy? _cachedUploadPolicy;

  /// 清空缓存的上传策略（登录或会员变更后调用）。
  static void invalidateUploadPolicyCache() {
    _cachedUploadPolicy = null;
  }

  Future<List<DeviceModel>> getDevices() async {
    await AppLogService.log('device_list_load_started');
    final list = await _withDevicePreferenceRetry(
      () => ApiClient.getList('/api/devices'),
    );
    await AppLogService.log(
      'device_list_load_completed',
      data: {'device_count': list.length},
    );
    return list
        .map((d) => DeviceModel.fromJson(d as Map<String, dynamic>))
        .toList();
  }

  Future<DeviceModel> getDevice(String machineId) async {
    await AppLogService.log(
      'device_detail_load_started',
      data: {'machine_id': machineId},
    );
    final data = await _withDevicePreferenceRetry(
      () => ApiClient.get('/api/devices/$machineId'),
    );
    await AppLogService.log(
      'device_detail_load_completed',
      data: {
        'machine_id': machineId,
        'hostname': data['hostname'] ?? '',
        'agent_count': ((data['agents'] as List?) ?? const []).length,
      },
    );
    return DeviceModel.fromJson(data);
  }

  Future<T> _withDevicePreferenceRetry<T>(Future<T> Function() request) async {
    const retryDelays = [
      Duration(milliseconds: 180),
      Duration(milliseconds: 420),
    ];
    for (var attempt = 0; ; attempt++) {
      try {
        return await request();
      } catch (error) {
        if (!_isTransientDevicePreferenceError(error) ||
            attempt >= retryDelays.length) {
          rethrow;
        }
        await Future<void>.delayed(retryDelays[attempt]);
      }
    }
  }

  bool _isTransientDevicePreferenceError(Object error) {
    return error is ApiException &&
        error.statusCode >= 500 &&
        error.toString().trim() == 'device preferences unavailable';
  }

  Future<void> updateDeviceDisplayName({
    required String machineId,
    required String displayName,
  }) async {
    await ApiClient.patch(
      '/api/devices/${Uri.encodeComponent(machineId)}/profile',
      {'display_name': displayName},
    );
  }

  Future<void> reorderDevices(List<String> machineIds) async {
    await ApiClient.patch('/api/devices/order', {'machine_ids': machineIds});
  }

  Future<void> reorderDeviceAgents({
    required String machineId,
    required List<String> agentIds,
  }) async {
    await ApiClient.patch(
      '/api/devices/${Uri.encodeComponent(machineId)}/agents/order',
      {'agent_ids': agentIds},
    );
  }

  Future<DeviceProjectMemorySettingsModel> getDeviceProjectMemorySettings(
    String machineId,
  ) async {
    final json = await ApiClient.get(
      '/api/devices/${Uri.encodeComponent(machineId)}/project-memory/settings',
    );
    return DeviceProjectMemorySettingsModel.fromJson(json);
  }

  Future<DeviceProjectMemorySettingsModel> updateDeviceProjectMemorySettings({
    required String machineId,
    required String model,
    required String variant,
  }) async {
    final json = await ApiClient.patch(
      '/api/devices/${Uri.encodeComponent(machineId)}/project-memory/settings',
      {
        'project_memory_model': model.trim(),
        'project_memory_variant': variant.trim(),
      },
    );
    return DeviceProjectMemorySettingsModel.fromJson(json);
  }

  Future<DeviceAIConfigInfo> getDeviceAIConfig(String machineId) async {
    final data = await ApiClient.get('/api/devices/$machineId/ai-config');
    final cfg = (data['config'] as Map<String, dynamic>?) ?? data;
    return DeviceAIConfigInfo.fromJson(cfg);
  }

  Future<DeviceAIConfigInfo> listDeviceAIModels({
    required String machineId,
    required String provider,
    required String baseUrl,
    required String consoleUrl,
    required String apiKey,
    required String apiMode,
    required String model,
    required List<DeviceAIModelInfo> models,
  }) async {
    final data =
        await ApiClient.post('/api/devices/$machineId/ai-config/models', {
          'provider': provider,
          'base_url': baseUrl,
          'console_url': consoleUrl,
          'api_key': apiKey,
          'api_mode': apiMode,
          'model': model,
          'models': models.map((item) => item.toJson()).toList(),
        });
    final cfg = (data['config'] as Map<String, dynamic>?) ?? data;
    return DeviceAIConfigInfo.fromJson(cfg);
  }

  Future<DeviceAIConfigInfo> saveDeviceAIConfig({
    required String machineId,
    required String provider,
    required String baseUrl,
    required String consoleUrl,
    required String apiKey,
    required String apiMode,
    required String model,
    required List<DeviceAIModelInfo> models,
    bool force = false,
  }) async {
    final data =
        await ApiClient.post('/api/devices/$machineId/ai-config/save', {
          'provider': provider,
          'base_url': baseUrl,
          'console_url': consoleUrl,
          'api_key': apiKey,
          'api_mode': apiMode,
          'model': model,
          'models': models.map((item) => item.toJson()).toList(),
          'force': force,
        });
    final cfg = (data['config'] as Map<String, dynamic>?) ?? data;
    return DeviceAIConfigInfo.fromJson(cfg);
  }

  Future<DeviceAIConfigInfo> saveDeviceAIConfigText({
    required String machineId,
    required String text,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/ai-config/save-text',
      {'text': text},
    );
    final cfg = (data['config'] as Map<String, dynamic>?) ?? data;
    return DeviceAIConfigInfo.fromJson(cfg);
  }

  Future<DeviceAIConfigInfo> clearDeviceAIProvider({
    required String machineId,
    required String provider,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/ai-config/clear-provider',
      {'provider': provider},
    );
    final cfg = (data['config'] as Map<String, dynamic>?) ?? data;
    return DeviceAIConfigInfo.fromJson(cfg);
  }

  Future<DeviceMCPConfigInfo> getDeviceMCPConfig(String machineId) async {
    final data = await ApiClient.get('/api/devices/$machineId/mcp-config');
    final cfg = (data['config'] as Map<String, dynamic>?) ?? data;
    return DeviceMCPConfigInfo.fromJson(cfg);
  }

  Future<DeviceMCPConfigInfo> saveDeviceMCPConfig({
    required String machineId,
    required List<DeviceMCPServerInfo> servers,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/mcp-config/save',
      {'servers': servers.map((item) => item.toJson()).toList()},
    );
    final cfg = (data['config'] as Map<String, dynamic>?) ?? data;
    return DeviceMCPConfigInfo.fromJson(cfg);
  }

  Future<DeviceMCPConfigInfo> removeDeviceMCPConfig({
    required String machineId,
    required String name,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/mcp-config/remove',
      {'name': name},
    );
    final cfg = (data['config'] as Map<String, dynamic>?) ?? data;
    return DeviceMCPConfigInfo.fromJson(cfg);
  }

  Future<MCPStatusInfo> getDeviceAgentMCPStatus({
    required String machineId,
    required String agentId,
  }) async {
    final data = await ApiClient.get(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/agents/${Uri.encodeComponent(agentId)}/mcp-tools',
    );
    return MCPStatusInfo.fromJson(
      (data['mcp_status'] as Map?)?.cast<String, dynamic>() ??
          <String, dynamic>{},
    );
  }

  Future<DeviceAgentMCPSelectionInfo> getDeviceAgentMCPSelection({
    required String machineId,
    required String agentId,
  }) async {
    final data = await ApiClient.get(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/agents/${Uri.encodeComponent(agentId)}/mcp-selection',
    );
    return DeviceAgentMCPSelectionInfo.fromJson(
      (data['selection'] as Map?)?.cast<String, dynamic>() ??
          <String, dynamic>{},
    );
  }

  Future<DeviceAgentMCPSelectionInfo> saveDeviceAgentMCPSelection({
    required String machineId,
    required String agentId,
    required String mode,
    required List<String> servers,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/agents/${Uri.encodeComponent(agentId)}/mcp-selection',
      {'mode': mode, 'servers': servers},
    );
    return DeviceAgentMCPSelectionInfo.fromJson(
      (data['selection'] as Map?)?.cast<String, dynamic>() ??
          <String, dynamic>{},
    );
  }

  Future<DeviceSkillCatalogResult> getSkillCatalog() async {
    final data = await ApiClient.get('/api/skills');
    return DeviceSkillCatalogResult.fromJson(data);
  }

  Future<DeviceGlobalSkillConfigInfo> getDeviceSkillConfig(
    String machineId,
  ) async {
    final data = await ApiClient.get(
      '/api/devices/${Uri.encodeComponent(machineId)}/skills-config',
    );
    return DeviceGlobalSkillConfigInfo.fromJson(data);
  }

  Future<DeviceGlobalSkillConfigInfo> saveDeviceSkillConfig({
    required String machineId,
    required DeviceGlobalSkillConfigInfo config,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/${Uri.encodeComponent(machineId)}/skills-config/save',
      config.toJson(),
    );
    return DeviceGlobalSkillConfigInfo.fromJson(data);
  }

  Future<DeviceGlobalSkillConfigInfo> importDeviceSkill({
    required String machineId,
    required DeviceSkillImport skill,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/${Uri.encodeComponent(machineId)}/skills-config/import',
      {'skill': skill.toJson(), 'overwrite': true},
    );
    return DeviceGlobalSkillConfigInfo.fromJson(data);
  }

  Future<DeviceAgentSkillSelectionInfo> getDeviceAgentSkillSelection({
    required String machineId,
    required String agentId,
  }) async {
    final data = await ApiClient.get(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/agents/${Uri.encodeComponent(agentId)}/skills',
    );
    return DeviceAgentSkillSelectionInfo.fromJson(
      (data['skill_selection'] as Map?)?.cast<String, dynamic>() ??
          <String, dynamic>{},
    );
  }

  Future<DeviceAgentSkillSelectionInfo> saveDeviceAgentSkillSelection({
    required String machineId,
    required String agentId,
    required List<String> extraSkillIds,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/agents/${Uri.encodeComponent(agentId)}/skills',
      {'extra_skill_ids': extraSkillIds},
    );
    return DeviceAgentSkillSelectionInfo.fromJson(
      (data['skill_selection'] as Map?)?.cast<String, dynamic>() ??
          <String, dynamic>{},
    );
  }

  Future<DeviceAgentSkillSelectionInfo> importDeviceAgentSkill({
    required String machineId,
    required String agentId,
    required DeviceSkillImport skill,
    bool overwrite = false,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/agents/${Uri.encodeComponent(agentId)}/skills/import',
      {'skill': skill.toJson(), 'overwrite': overwrite},
    );
    return DeviceAgentSkillSelectionInfo.fromJson(
      (data['skill_selection'] as Map?)?.cast<String, dynamic>() ??
          <String, dynamic>{},
    );
  }

  Future<DeviceAgentSemanticSelectionInfo> getDeviceAgentSemanticSelection({
    required String machineId,
    required String agentId,
  }) async {
    final data = await ApiClient.get(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/agents/${Uri.encodeComponent(agentId)}/semantic-agent',
    );
    return DeviceAgentSemanticSelectionInfo.fromJson(
      (data['semantic_selection'] as Map?)?.cast<String, dynamic>() ??
          <String, dynamic>{},
    );
  }

  Future<DeviceAgentSemanticSelectionInfo> saveDeviceAgentSemanticSelection({
    required String machineId,
    required String agentId,
    required String semanticAgentId,
    bool applyRecommendedMcp = true,
    bool disableVerifyMcp = false,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/agents/${Uri.encodeComponent(agentId)}/semantic-agent',
      {
        'semantic_agent_id': semanticAgentId,
        'apply_recommended_mcp': applyRecommendedMcp,
        'disable_verify_mcp': disableVerifyMcp,
      },
    );
    return DeviceAgentSemanticSelectionInfo.fromJson(
      (data['semantic_selection'] as Map?)?.cast<String, dynamic>() ??
          <String, dynamic>{},
    );
  }

  Future<RuntimePreflightJob> startDeviceAgentSemanticPreflight({
    required String machineId,
    required String agentId,
    required String semanticAgentId,
    bool applyRecommendedMcp = true,
    bool disableVerifyMcp = false,
    bool autoRepair = true,
    bool restartAfterApply = true,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/agents/${Uri.encodeComponent(agentId)}/semantic-agent/preflight',
      {
        'semantic_agent_id': semanticAgentId,
        'apply_recommended_mcp': applyRecommendedMcp,
        'disable_verify_mcp': disableVerifyMcp,
        'auto_repair': autoRepair,
        'restart_after_apply': restartAfterApply,
      },
    );
    return RuntimePreflightJob.fromJson(
      (data['job'] as Map?)?.cast<String, dynamic>() ?? <String, dynamic>{},
    );
  }

  Future<RuntimePreflightJob> getDeviceAgentSemanticPreflightJob({
    required String machineId,
    required String jobId,
  }) async {
    final data = await ApiClient.get(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/preflight-jobs/${Uri.encodeComponent(jobId)}',
    );
    return RuntimePreflightJob.fromJson(
      (data['job'] as Map?)?.cast<String, dynamic>() ?? <String, dynamic>{},
    );
  }

  Future<List<RuntimePreflightEvent>> getDeviceAgentSemanticPreflightEvents({
    required String machineId,
    required String jobId,
    int afterSequence = 0,
  }) async {
    final suffix = afterSequence > 0 ? '?after=$afterSequence' : '';
    final data = await ApiClient.get(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/preflight-jobs/${Uri.encodeComponent(jobId)}/events$suffix',
    );
    return (data['items'] as List<dynamic>? ?? const [])
        .whereType<Map>()
        .map(
          (item) =>
              RuntimePreflightEvent.fromJson(item.cast<String, dynamic>()),
        )
        .toList();
  }

  /// 查询设备上某个受管运行时组件（例如 ffmpeg）是否已安装。
  Future<DeviceRuntimeStatus> getDeviceRuntimeStatus({
    required String machineId,
    required String runtimeId,
  }) async {
    final data = await ApiClient.get(
      '/api/devices/${Uri.encodeComponent(machineId)}/runtimes/${Uri.encodeComponent(runtimeId)}/status',
    );
    return DeviceRuntimeStatus.fromJson(
      (data['runtime'] as Map?)?.cast<String, dynamic>() ?? <String, dynamic>{},
    );
  }

  /// 请求设备安装某个运行时组件，返回可轮询的安装任务。
  Future<RuntimePreflightJob> installDeviceRuntime({
    required String machineId,
    required String runtimeId,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/${Uri.encodeComponent(machineId)}/runtimes/${Uri.encodeComponent(runtimeId)}/install',
      const <String, dynamic>{},
    );
    return RuntimePreflightJob.fromJson(
      (data['job'] as Map?)?.cast<String, dynamic>() ?? <String, dynamic>{},
    );
  }

  Future<DeviceEnvConfigInfo> getDeviceEnvConfig(String machineId) async {
    final data = await ApiClient.get('/api/devices/$machineId/env-config');
    final cfg = (data['config'] as Map<String, dynamic>?) ?? data;
    return DeviceEnvConfigInfo.fromJson(cfg);
  }

  Future<DeviceAgentCompactionConfig> getDeviceAgentCompactionConfig({
    required String machineId,
    required String agentId,
  }) async {
    final data = await ApiClient.get(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/agents/${Uri.encodeComponent(agentId)}/compaction-config',
    );
    return DeviceAgentCompactionConfig.fromJson(
      (data['compaction_config'] as Map?)?.cast<String, dynamic>() ??
          <String, dynamic>{},
    );
  }

  Future<DeviceAgentCompactionConfig> saveDeviceAgentCompactionConfig({
    required String machineId,
    required String agentId,
    required int thresholdPercent,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/${Uri.encodeComponent(machineId)}/launcher/agents/${Uri.encodeComponent(agentId)}/compaction-config',
      {'threshold_percent': thresholdPercent},
    );
    return DeviceAgentCompactionConfig.fromJson(
      (data['compaction_config'] as Map?)?.cast<String, dynamic>() ??
          <String, dynamic>{},
    );
  }

  Future<List<EnvironmentPresetModel>> getEnvironmentPresets() async {
    final data = await ApiClient.get('/api/environment-presets');
    final items = (data['items'] as List<dynamic>?) ?? const [];
    return items
        .whereType<Map<String, dynamic>>()
        .map(EnvironmentPresetModel.fromJson)
        .where((item) => item.enabled && item.variables.isNotEmpty)
        .toList();
  }

  Future<DeviceEnvConfigInfo> saveDeviceEnvConfig({
    required String machineId,
    required Map<String, String> globalEnvironment,
    String agentId = '',
    Map<String, String> agentEnvironment = const {},
    String expectedRevision = '',
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/env-config/save',
      {
        'global_environment': globalEnvironment,
        if (agentId.isNotEmpty) 'agent_id': agentId,
        if (agentId.isNotEmpty) 'agent_environment': agentEnvironment,
        if (expectedRevision.isNotEmpty) 'expected_revision': expectedRevision,
      },
    );
    final cfg = (data['config'] as Map<String, dynamic>?) ?? data;
    return DeviceEnvConfigInfo.fromJson(cfg);
  }

  Future<DeviceLauncherState> getDeviceLauncherState(String machineId) async {
    final data = await ApiClient.get('/api/devices/$machineId/launcher');
    final state = (data['state'] as Map<String, dynamic>?) ?? data;
    return DeviceLauncherState.fromJson(state);
  }

  Future<CliVersionInfo> getLatestCliVersion() async {
    final data = await ApiClient.get('/api/cli/version');
    return CliVersionInfo.fromJson(data);
  }

  Future<DeviceLauncherState> upgradeDeviceLauncher({
    required String machineId,
    required String targetVersion,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/launcher/upgrade',
      {'target_version': targetVersion},
    );
    final state = (data['state'] as Map<String, dynamic>?) ?? data;
    return DeviceLauncherState.fromJson(state);
  }

  Future<DeviceLauncherState> rollbackDeviceLauncher({
    required String machineId,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/launcher/rollback',
      {},
    );
    final state = (data['state'] as Map<String, dynamic>?) ?? data;
    return DeviceLauncherState.fromJson(state);
  }

  /// 查询设备 launcher 运行目录的磁盘占用。
  /// 需要遍历整个运行目录，耗时较长，因此放宽超时。
  Future<DeviceStorageUsage> getDeviceStorage({
    required String machineId,
  }) async {
    final data = await ApiClient.get(
      '/api/devices/$machineId/launcher/storage',
      timeout: const Duration(seconds: 120),
    );
    final storage = data['storage'];
    if (storage is Map) {
      return DeviceStorageUsage.fromJson(Map<String, dynamic>.from(storage));
    }
    return const DeviceStorageUsage();
  }

  /// 清理设备上可再生成的缓存。
  /// keys 为空时清理全部可清理类别。
  Future<DeviceStorageClearResult> clearDeviceStorage({
    required String machineId,
    List<String> keys = const [],
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/launcher/storage/clear',
      {'keys': keys},
      timeout: const Duration(seconds: 120),
    );
    final storage = data['storage'];
    if (storage is Map) {
      return DeviceStorageClearResult.fromJson(
        Map<String, dynamic>.from(storage),
      );
    }
    return const DeviceStorageClearResult();
  }

  Future<DeviceDirectoryResult> getDeviceDirectories({
    required String machineId,
    String path = '',
  }) async {
    final suffix = path.isEmpty
        ? ''
        : '?path=${Uri.encodeQueryComponent(path)}';
    final data = await ApiClient.get(
      '/api/devices/$machineId/directories$suffix',
    );
    return DeviceDirectoryResult.fromJson(data);
  }

  Future<DeviceDirectoryResult> getDeviceAgentFiles({
    required String machineId,
    required String agentId,
    String path = '',
  }) async {
    final suffix = path.isEmpty
        ? ''
        : '?path=${Uri.encodeQueryComponent(path)}';
    final data = await ApiClient.get(
      '/api/devices/$machineId/launcher/agents/$agentId/files$suffix',
    );
    return DeviceDirectoryResult.fromJson(data);
  }

  Future<DeviceProjectFile> downloadDeviceAgentFile({
    required String machineId,
    required String agentId,
    required String path,
  }) async {
    final data = await ApiClient.get(
      '/api/devices/$machineId/launcher/agents/$agentId/files/download?path=${Uri.encodeQueryComponent(path)}',
    );
    final file = (data['file'] as Map<String, dynamic>?) ?? data;
    return DeviceProjectFile.fromJson(file);
  }

  Future<DeviceProjectFile> createDeviceAgentFileDownload({
    required String machineId,
    required String agentId,
    required String path,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/launcher/agents/$agentId/files/download/create',
      {'path': path},
    );
    final file = (data['file'] as Map<String, dynamic>?) ?? data;
    return DeviceProjectFile.fromJson(file);
  }

  Future<DeviceProjectFile> downloadDeviceAgentFileChunk({
    required String machineId,
    required String agentId,
    required String path,
    required int chunkIndex,
    required int offset,
    required int length,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/launcher/agents/$agentId/files/download/chunk',
      {
        'path': path,
        'chunk_index': chunkIndex,
        'offset': offset,
        'length': length,
      },
    );
    final file = (data['file'] as Map<String, dynamic>?) ?? data;
    return DeviceProjectFile.fromJson(file);
  }

  /// 流式下载到临时文件：分块到达即写盘，不把整个文件读进内存。
  /// 大文件（GB 级）用这个，避免 base64 解码 + 全量拼接导致 OOM。
  /// 流式下载到临时文件，支持断点续传。
  ///
  /// 分块到达即写入磁盘（不进内存），同时把已写入的块号记在旁路清单里；
  /// 中断后再次下载同一文件会从清单继续，不再从头开始。
  /// 流式下载到临时文件，支持断点续传。
  ///
  /// 每个分块单独落盘（`<临时目录>/dl_<key>/<index>.bin`），因此中断后重试
  /// 只需补齐缺失的分块，再按顺序拼接成完整文件；内存占用恒定，不会 OOM。
  Future<({String path, int size})> downloadDeviceAgentFileToFile({
    required String machineId,
    required String agentId,
    required String path,
    int chunkSize = defaultDownloadChunkSize,
    int concurrency = defaultDownloadConcurrency,
    bool resume = true,
    DeviceDownloadProgressCallback? onProgress,
  }) async {
    if (chunkSize <= 0) {
      throw ArgumentError.value(chunkSize, 'chunkSize', '必须大于 0');
    }
    final meta = await createDeviceAgentFileDownload(
      machineId: machineId,
      agentId: agentId,
      path: path,
    );
    final totalBytes = meta.size;
    final totalChunks = max(1, (totalBytes / chunkSize).ceil());
    final tempDir = await getTemporaryDirectory();
    // 续传标识只跟「哪个设备上的哪个文件、多大」有关，重试时能命中同一批分块。
    final resumeKey = sha256
        .convert(utf8.encode('$machineId|$agentId|$path|$totalBytes'))
        .toString();
    final chunkDir = Directory(p.join(tempDir.path, 'dl_$resumeKey'));
    final partPath = p.join(tempDir.path, 'dl_$resumeKey.part');

    final received = <int>{};
    if (resume && await chunkDir.exists()) {
      await for (final entity in chunkDir.list()) {
        if (entity is! File) continue;
        final name = p.basenameWithoutExtension(entity.path);
        final index = int.tryParse(name);
        if (index == null || index < 0 || index >= totalChunks) continue;
        // 分块长度必须与预期一致，避免半截块污染结果。
        final expected = min(chunkSize, totalBytes - index * chunkSize);
        if (await entity.length() == expected) received.add(index);
      }
    }
    await chunkDir.create(recursive: true);

    var downloadedBytes = 0;
    for (final index in received) {
      final offset = index * chunkSize;
      downloadedBytes += min(chunkSize, totalBytes - offset);
    }
    var completedChunks = received.length;
    var nextChunk = 0;

    void progress(String stage) {
      onProgress?.call(
        DeviceDownloadProgress(
          downloadedBytes: downloadedBytes,
          totalBytes: totalBytes,
          completedChunks: completedChunks,
          totalChunks: totalChunks,
          stage: stage,
        ),
      );
    }

    progress(received.isEmpty ? '下载分块' : '续传：已有 ${received.length}/$totalChunks 分块');

    try {
      final workerCount = min(max(1, concurrency), totalChunks);
      Future<void> worker() async {
        while (true) {
          final chunkIndex = nextChunk;
          nextChunk += 1;
          if (chunkIndex >= totalChunks) return;
          if (received.contains(chunkIndex)) continue;
          final offset = chunkIndex * chunkSize;
          final length = min(chunkSize, totalBytes - offset);
          final chunk = await downloadDeviceAgentFileChunk(
            machineId: machineId,
            agentId: agentId,
            path: path,
            chunkIndex: chunkIndex,
            offset: offset,
            length: length,
          );
          final bytes = Uint8List.fromList(base64Decode(chunk.content));
          // 先写临时块再 rename：半截块不会被视为「已下载」。
          final target = File(p.join(chunkDir.path, '$chunkIndex.bin'));
          final staging = File('${target.path}.tmp');
          await staging.writeAsBytes(bytes, flush: true);
          await staging.rename(target.path);
          received.add(chunkIndex);
          downloadedBytes += bytes.length;
          completedChunks += 1;
          progress('下载分块');
        }
      }

      await Future.wait(List.generate(workerCount, (_) => worker()));

      // 按顺序拼接成完整文件（流式，不占内存）。
      progress('正在合并分块');
      final partFile = File(partPath);
      final sink = await partFile.open(mode: FileMode.write);
      try {
        for (var index = 0; index < totalChunks; index++) {
          final chunkFile = File(p.join(chunkDir.path, '$index.bin'));
          if (!await chunkFile.exists()) {
            throw StateError('分块 $index 缺失，请重试');
          }
          await sink.setPosition(index * chunkSize);
          await sink.writeFrom(await chunkFile.readAsBytes());
        }
        await sink.flush();
      } finally {
        await sink.close();
      }
      progress('下载完成');
      return (path: partPath, size: totalBytes);
    } catch (error) {
      // 已下载的分块保留，下次同文件下载会从缺失处继续。
      rethrow;
    }
  }


  /// 下载并保存完成后清理续传用的分块目录与合并文件。
  Future<void> clearDownloadResume({
    required String machineId,
    required String agentId,
    required String path,
    required int size,
  }) async {
    try {
      final tempDir = await getTemporaryDirectory();
      final key = sha256
          .convert(utf8.encode('$machineId|$agentId|$path|$size'))
          .toString();
      final chunkDir = Directory(p.join(tempDir.path, 'dl_$key'));
      if (await chunkDir.exists()) await chunkDir.delete(recursive: true);
      final partFile = File(p.join(tempDir.path, 'dl_$key.part'));
      if (await partFile.exists()) await partFile.delete();
    } catch (_) {}
  }


  Future<DeviceProjectFile> downloadDeviceAgentFileChunked({
    required String machineId,
    required String agentId,
    required String path,
    int chunkSize = defaultDownloadChunkSize,
    int concurrency = defaultDownloadConcurrency,
    DeviceDownloadProgressCallback? onProgress,
  }) async {
    if (chunkSize <= 0) {
      throw ArgumentError.value(chunkSize, 'chunkSize', '必须大于 0');
    }
    final DeviceProjectFile meta;
    try {
      meta = await createDeviceAgentFileDownload(
        machineId: machineId,
        agentId: agentId,
        path: path,
      ).timeout(defaultDownloadCreateTimeout);
    } catch (_) {
      onProgress?.call(
        const DeviceDownloadProgress(
          downloadedBytes: 0,
          totalBytes: 0,
          completedChunks: 0,
          totalChunks: 1,
          stage: '兼容旧版下载',
        ),
      );
      return downloadDeviceAgentFile(
        machineId: machineId,
        agentId: agentId,
        path: path,
      );
    }
    final totalBytes = meta.size;
    final totalChunks = max(1, (totalBytes / chunkSize).ceil());

    void progress({
      required int downloadedBytes,
      required int completedChunks,
      required String stage,
    }) {
      onProgress?.call(
        DeviceDownloadProgress(
          downloadedBytes: downloadedBytes,
          totalBytes: totalBytes,
          completedChunks: completedChunks,
          totalChunks: totalChunks,
          stage: stage,
        ),
      );
    }

    progress(downloadedBytes: 0, completedChunks: 0, stage: '准备下载');
    if (totalBytes <= 0) {
      progress(downloadedBytes: 0, completedChunks: totalChunks, stage: '下载完成');
      return DeviceProjectFile(
        path: meta.path,
        name: meta.name,
        size: 0,
        encoding: 'base64',
        content: '',
        sha256: meta.sha256,
      );
    }

    progress(downloadedBytes: 0, completedChunks: 0, stage: '下载分块');
    final chunks = List<Uint8List?>.filled(totalChunks, null);
    final downloaded = List<int>.filled(totalChunks, 0);
    var completedChunks = 0;
    var nextChunk = 0;
    final workerCount = min(max(1, concurrency), totalChunks);

    Future<void> worker() async {
      while (true) {
        final chunkIndex = nextChunk;
        nextChunk += 1;
        if (chunkIndex >= totalChunks) return;

        final offset = chunkIndex * chunkSize;
        final length = min(chunkSize, totalBytes - offset);
        final file = await downloadDeviceAgentFileChunk(
          machineId: machineId,
          agentId: agentId,
          path: path,
          chunkIndex: chunkIndex,
          offset: offset,
          length: length,
        );
        final bytes = Uint8List.fromList(base64Decode(file.content));
        chunks[chunkIndex] = bytes;
        downloaded[chunkIndex] = bytes.length;
        completedChunks += 1;
        progress(
          downloadedBytes: downloaded.fold<int>(0, (sum, value) => sum + value),
          completedChunks: completedChunks,
          stage: '下载分块',
        );
      }
    }

    await Future.wait(List.generate(workerCount, (_) => worker()));

    final builder = BytesBuilder(copy: false);
    for (final chunk in chunks) {
      if (chunk == null) {
        throw StateError('下载分块缺失');
      }
      builder.add(chunk);
    }
    final bytes = builder.takeBytes();
    if (bytes.length != totalBytes) {
      throw StateError('文件大小校验失败');
    }
    if (meta.sha256.isNotEmpty &&
        sha256.convert(bytes).toString().toLowerCase() !=
            meta.sha256.toLowerCase()) {
      throw StateError('文件整体校验失败');
    }
    progress(
      downloadedBytes: totalBytes,
      completedChunks: totalChunks,
      stage: '下载完成',
    );
    return DeviceProjectFile(
      path: meta.path,
      name: meta.name,
      size: totalBytes,
      content: base64Encode(bytes),
      encoding: 'base64',
      sha256: meta.sha256,
    );
  }

  Future<DeviceProjectFile> uploadDeviceAgentFile({
    required String machineId,
    required String agentId,
    required String path,
    required String content,
    String encoding = 'base64',
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/launcher/agents/$agentId/files/upload',
      {'path': path, 'content': content, 'encoding': encoding},
    );
    final file = (data['file'] as Map<String, dynamic>?) ?? data;
    return DeviceProjectFile.fromJson(file);
  }

  Future<DeviceDirectoryInfo> createDeviceAgentEmptyFile({
    required String machineId,
    required String agentId,
    required String path,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/launcher/agents/$agentId/files/create',
      {'path': path},
    );
    final item =
        (data['file'] as Map<String, dynamic>?) ??
        (data['item'] as Map<String, dynamic>?) ??
        data;
    return DeviceDirectoryInfo.fromJson(item);
  }

  Future<DeviceDirectoryInfo> createDeviceAgentFolder({
    required String machineId,
    required String agentId,
    required String path,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/launcher/agents/$agentId/files/folders/create',
      {'path': path},
    );
    final item =
        (data['file'] as Map<String, dynamic>?) ??
        (data['item'] as Map<String, dynamic>?) ??
        data;
    return DeviceDirectoryInfo.fromJson(item);
  }

  Future<DeviceDirectoryInfo> renameDeviceAgentFile({
    required String machineId,
    required String agentId,
    required String path,
    required String name,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/launcher/agents/$agentId/files/rename',
      {'path': path, 'name': name},
    );
    final item =
        (data['file'] as Map<String, dynamic>?) ??
        (data['item'] as Map<String, dynamic>?) ??
        data;
    return DeviceDirectoryInfo.fromJson(item);
  }

  Future<void> deleteDeviceAgentFile({
    required String machineId,
    required String agentId,
    required String path,
    required bool isDir,
  }) async {
    await ApiClient.post(
      '/api/devices/$machineId/launcher/agents/$agentId/files/delete',
      {'path': path, 'is_dir': isDir},
    );
  }

  Future<DeviceProjectFile> uploadDeviceAgentFileChunked({
    required String machineId,
    required String agentId,
    required String path,
    required Uint8List bytes,
    String? uploadId,
    int chunkSize = defaultUploadChunkSize,
    int concurrency = defaultUploadConcurrency,
    DeviceUploadProgressCallback? onProgress,
  }) async {
    if (chunkSize <= 0) {
      throw ArgumentError.value(chunkSize, 'chunkSize', '必须大于 0');
    }
    final normalizedConcurrency = max(1, concurrency);
    final actualUploadId = uploadId ?? _newUploadId();
    final totalBytes = bytes.length;
    final totalChunks = max(1, (totalBytes / chunkSize).ceil());
    final fileHash = sha256.convert(bytes).toString();
    final basePath =
        '/api/devices/$machineId/launcher/agents/$agentId/files/upload';

    void progress({
      required int uploadedBytes,
      required int completedChunks,
      required String stage,
    }) {
      onProgress?.call(
        DeviceUploadProgress(
          uploadId: actualUploadId,
          uploadedBytes: uploadedBytes,
          totalBytes: totalBytes,
          completedChunks: completedChunks,
          totalChunks: totalChunks,
          stage: stage,
        ),
      );
    }

    progress(uploadedBytes: 0, completedChunks: 0, stage: '准备上传');
    await ApiClient.post('$basePath/create', {
      'path': path,
      'upload_id': actualUploadId,
      'size': totalBytes,
      'total_chunks': totalChunks,
      'sha256': fileHash,
    });

    progress(uploadedBytes: 0, completedChunks: 0, stage: '上传分块');
    final uploaded = List<int>.filled(totalChunks, 0);
    var completedChunks = 0;
    var nextChunk = 0;

    Future<void> worker() async {
      while (true) {
        final chunkIndex = nextChunk;
        nextChunk += 1;
        if (chunkIndex >= totalChunks) return;

        final start = chunkIndex * chunkSize;
        final end = min(start + chunkSize, totalBytes);
        final chunk = bytes.sublist(start, end);
        final chunkHash = sha256.convert(chunk).toString();
        // 分块以 multipart 二进制提交，省掉 base64 的 33% 膨胀。
        await ApiClient.postChunkMultipart(
          '$basePath/chunk',
          fields: {
            'path': path,
            'upload_id': actualUploadId,
            'chunk_index': '$chunkIndex',
            'total_chunks': '$totalChunks',
            'offset': '$start',
            'encoding': 'binary',
            'sha256': chunkHash,
          },
          chunk: chunk,
        );
        uploaded[chunkIndex] = chunk.length;
        completedChunks += 1;
        progress(
          uploadedBytes: uploaded.fold<int>(0, (sum, value) => sum + value),
          completedChunks: completedChunks,
          stage: '上传分块',
        );
      }
    }

    final workerCount = min(normalizedConcurrency, totalChunks);
    await Future.wait(List.generate(workerCount, (_) => worker()));

    progress(
      uploadedBytes: totalBytes,
      completedChunks: completedChunks,
      stage: '等待设备合并',
    );
    final data = await ApiClient.post('$basePath/complete', {
      'path': path,
      'upload_id': actualUploadId,
      'size': totalBytes,
      'total_chunks': totalChunks,
      'sha256': fileHash,
    });
    progress(
      uploadedBytes: totalBytes,
      completedChunks: totalChunks,
      stage: '上传完成',
    );
    final file = (data['file'] as Map<String, dynamic>?) ?? data;
    return DeviceProjectFile.fromJson(file);
  }

  Future<DeviceProjectFile> uploadDeviceAgentFileChunkedStreamed({
    required String machineId,
    required String agentId,
    required String path,
    required int totalBytes,
    required Stream<List<int>> Function() openRead,
    String? uploadId,
    int? chunkSize,
    int? concurrency,
    bool resume = false,
    DeviceUploadProgressCallback? onProgress,
  }) {
    return _uploadFileChunkedStreamed(
      basePath: '/api/devices/$machineId/launcher/agents/$agentId/files/upload',
      path: path,
      totalBytes: totalBytes,
      openRead: openRead,
      uploadId: uploadId,
      chunkSize: chunkSize,
      concurrency: concurrency,
      resume: resume,
      onProgress: onProgress,
    );
  }

  Future<DeviceProjectFile> uploadDeviceDirectoryFileChunkedStreamed({
    required String machineId,
    required String path,
    required int totalBytes,
    required Stream<List<int>> Function() openRead,
    String? uploadId,
    int? chunkSize,
    int? concurrency,
    bool resume = false,
    DeviceUploadProgressCallback? onProgress,
  }) {
    return _uploadFileChunkedStreamed(
      basePath: '/api/devices/$machineId/directories/files/upload',
      path: path,
      totalBytes: totalBytes,
      openRead: openRead,
      uploadId: uploadId,
      chunkSize: chunkSize,
      concurrency: concurrency,
      resume: resume,
      onProgress: onProgress,
    );
  }

  /// 拉取服务端下发的上传策略（分块大小与并发数按会员等级决定）。
  /// 网络异常时回退到本地缓存的会员等级，保证上传功能可用。
  Future<UploadPolicy> fetchUploadPolicy({bool force = false}) async {
    if (!force && _cachedUploadPolicy != null) {
      return _cachedUploadPolicy!;
    }
    UploadPolicy policy;
    try {
      final data = await ApiClient.get('/api/upload-policy');
      final raw = data['policy'] as Map<String, dynamic>?;
      policy = raw != null
          ? UploadPolicy.fromJson(raw)
          : UploadPolicy.fromLocalMembership();
    } catch (_) {
      policy = UploadPolicy.fromLocalMembership();
    }
    _cachedUploadPolicy = policy;
    return policy;
  }

  /// 查询设备侧某次上传已收到的分块，用于断点续传。
  /// 查询失败返回空集合，调用方按全新上传处理。
  Future<({Set<int> received, int receivedBytes, bool resumable})>
  _queryUploadStatus({
    required String basePath,
    required String path,
    required String uploadId,
    required int size,
    required int totalChunks,
  }) async {
    const empty = (received: <int>{}, receivedBytes: 0, resumable: false);
    try {
      final data = await ApiClient.post('$basePath/status', {
        'path': path,
        'upload_id': uploadId,
        'size': size,
        'total_chunks': totalChunks,
      }, timeout: const Duration(seconds: 30));
      final raw = data['status'];
      final status = raw is Map<String, dynamic>
          ? raw
          : (data['file'] is Map<String, dynamic>
                ? (data['file'] as Map<String, dynamic>)['status']
                : null);
      if (status is! Map<String, dynamic>) return empty;
      // 会话必须与本次文件一致，否则不能复用已有分块。
      final statusSize = status['size'];
      final statusTotal = status['total_chunks'];
      if (statusSize is num && statusSize.toInt() != size) return empty;
      if (statusTotal is num && statusTotal.toInt() != totalChunks) {
        return empty;
      }
      final received = status['received_chunks'];
      if (received is! List) return empty;
      final result = <int>{};
      for (final item in received) {
        if (item is num) result.add(item.toInt());
      }
      final bytes = status['received_bytes'];
      return (
        received: result,
        receivedBytes: bytes is num ? bytes.toInt() : 0,
        resumable: status['resumable'] == true || result.isNotEmpty,
      );
    } catch (_) {
      return empty;
    }
  }

  /// 按已收到的分块下标计算已传字节数。
  ///
  /// 不能用设备上报的 `received_bytes`：临时文件在 create 阶段就按声明大小
  /// 预分配（Truncate），其大小恒等于总大小，会算出 100%。这里按分块下标
  /// 累加，末块按剩余字节数计算，保证与真实进度一致。
  int _receivedBytesFor({
    required Set<int> received,
    required int totalBytes,
    required int totalChunks,
    required int chunkSize,
  }) {
    if (received.isEmpty || totalBytes <= 0) return 0;
    var bytes = 0;
    for (final index in received) {
      if (index < 0 || index >= totalChunks) continue;
      final start = index * chunkSize;
      final end = (start + chunkSize).clamp(0, totalBytes);
      if (end > start) bytes += end - start;
    }
    return bytes.clamp(0, totalBytes);
  }

  /// 上传单个分块，失败时按指数退避重试。
  /// 分块写入使用固定 offset，重试同一分块是幂等的。
  ///
  /// 分块以 multipart 二进制提交，不再做 base64 编码：
  /// 这一步省掉客户端到后端链路上 33% 的传输量。
  Future<void> _postChunkWithRetry({
    required String url,
    required Map<String, String> fields,
    required Uint8List chunk,
    required Duration timeout,
    void Function(int attempt, Object error)? onRetry,
  }) async {
    Object? lastError;
    for (var attempt = 0; attempt < _chunkMaxAttempts; attempt++) {
      try {
        await ApiClient.postChunkMultipart(
          url,
          fields: fields,
          chunk: chunk,
          timeout: timeout,
        );
        return;
      } catch (error) {
        lastError = error;
        if (attempt == _chunkMaxAttempts - 1) break;
        // 重试次数是判断「链路抖动」还是「设备端排队」的关键信号：
        // 若重试集中在锁竞争激烈时，说明重试又排在设备锁后面，越试越糟。
        onRetry?.call(attempt + 1, error);
        // 退避 1s、2s、4s，避免在链路抖动时持续打满上行。
        await Future<void>.delayed(Duration(seconds: 1 << attempt));
      }
    }
    throw ApiException(
      '分块上传失败（已重试 $_chunkMaxAttempts 次）：$lastError',
      statusCode: 0,
    );
  }

  Future<DeviceProjectFile> _uploadFileChunkedStreamed({
    required String basePath,
    required String path,
    required int totalBytes,
    required Stream<List<int>> Function() openRead,
    String? uploadId,
    int? chunkSize,
    int? concurrency,
    bool resume = false,
    DeviceUploadProgressCallback? onProgress,
  }) async {
    if (totalBytes < 0) {
      throw ArgumentError.value(totalBytes, 'totalBytes', '不能小于 0');
    }

    // 未显式指定分块大小时按会员等级取策略：普通 512KB/单并发，会员 5MB/多并发。
    // 显式指定分块时不做策略请求，并发缺省为 1，保持既有串行语义。
    UploadPolicy? policy;
    if (chunkSize == null) {
      policy = await fetchUploadPolicy();
    }
    final effectiveChunkSize = chunkSize ?? policy!.chunkSize;
    final effectiveConcurrency = max(
      1,
      concurrency ?? policy?.concurrency ?? 1,
    );
    if (effectiveChunkSize <= 0) {
      throw ArgumentError.value(effectiveChunkSize, 'chunkSize', '必须大于 0');
    }

    // 设备端按 upload_id 保存续传会话，复用同一个 id 才能让重试命中上次
    // 已写入的分块；每次新生成 id 会让 `upload/status` 永远查到空会话。
    final resumeKey = UploadResumeStore.key(
      basePath: basePath,
      path: path,
      size: totalBytes,
    );
    final storedUploadId = resume ? UploadResumeStore.read(resumeKey) : null;
    final actualUploadId = uploadId ?? storedUploadId ?? _newUploadId();
    DiagLog.emit('upload_resume_begin', {
      'path': path,
      'size': totalBytes,
      'resume_enabled': resume,
      'resume_key': resumeKey,
      'stored_upload_id': storedUploadId,
      'using_upload_id': actualUploadId,
      'reused': storedUploadId != null,
    });
    if (uploadId == null) {
      await UploadResumeStore.save(resumeKey, actualUploadId);
    }
    final totalChunks = max(1, (totalBytes / effectiveChunkSize).ceil());
    final digestSink = _DigestSink();
    final digestInput = sha256.startChunkedConversion(digestSink);
    var uploadedBytes = 0;
    var completedChunks = 0;
    // 诊断用：记录每个分块的往返耗时，供整体统计与瓶颈判定。
    final chunkDurationsMs = <int>[];
    var chunkRetries = 0;
    final uploadStartedAt = DateTime.now();

    void progress({
      required int uploadedBytes,
      required int completedChunks,
      required String stage,
    }) {
      onProgress?.call(
        DeviceUploadProgress(
          uploadId: actualUploadId,
          uploadedBytes: uploadedBytes,
          totalBytes: totalBytes,
          completedChunks: completedChunks,
          totalChunks: totalChunks,
          stage: stage,
        ),
      );
    }

    // 续传：查询设备侧已收到的分块，跳过这些分块的上传。
    // 设备侧已经存在同一会话时必须声明续传，否则 create 会直接报
    // 「上传会话已存在」，用户既看不到进度也无法继续。
    final status = resume
        ? await _queryUploadStatus(
            basePath: basePath,
            path: path,
            uploadId: actualUploadId,
            size: totalBytes,
            totalChunks: totalChunks,
          )
        : (received: <int>{}, receivedBytes: 0, resumable: false);
    final received = status.received;
    DiagLog.emit('upload_resume_status', {
      'path': path,
      'upload_id': actualUploadId,
      'resumable': status.resumable,
      'received_chunks': received.length,
      'received_bytes': status.receivedBytes,
      'total_chunks': totalChunks,
    });

    // 续传时从设备侧真实进度起步，让用户看到「已传多少」而不是从 0 开始。
    // 注意不能用设备上报的 received_bytes：临时文件按声明大小预分配，它的
    // 大小恒等于总大小，会算出 100%。按已收到的分块下标累加才准确。
    uploadedBytes = _receivedBytesFor(
      received: received,
      totalBytes: totalBytes,
      totalChunks: totalChunks,
      chunkSize: effectiveChunkSize,
    );
    completedChunks = received.length;
    progress(
      uploadedBytes: uploadedBytes,
      completedChunks: completedChunks,
      stage: status.resumable
          ? '续传：设备已有 ${received.length}/$totalChunks 分块'
          : '准备上传',
    );
    await ApiClient.post('$basePath/create', {
      'path': path,
      'upload_id': actualUploadId,
      'size': totalBytes,
      'total_chunks': totalChunks,
      // 只要设备侧已有同一会话就声明续传（即使还没收到分块），
      // 避免 create 被当成重复创建而失败。
      if (resume && status.resumable) 'resume': true,
    }, timeout: const Duration(seconds: 60));

    final chunkUrl = '$basePath/chunk';
    // 单块超时：按块大小估算传输时间，再叠加固定等待，避免误杀慢链路。
    final chunkTimeout =
        _chunkBaseTimeout +
        Duration(
          milliseconds: (effectiveChunkSize / 1024 / 1024 * 2000).round(),
        );

    Future<void> uploadChunk(int chunkIndex, Uint8List chunk) async {
      if (received.contains(chunkIndex)) {
        // 续传时这些分块已经在起始进度里计过，这里只报告进度，不再累加。
        progress(
          uploadedBytes: uploadedBytes,
          completedChunks: completedChunks,
          stage: '上传分块',
        );
        return;
      }
      // 记录起止时间戳：并发是否真正重叠，只能靠区间比对判断，
      // 单看总耗时分不清「并发被串行化」和「链路本身慢」。
      final chunkStartedAt = DateTime.now();
      final chunkBody = <String, String>{
        'path': path,
        'upload_id': actualUploadId,
        // 设备侧把它写进续传清单，缺失会让清单里的 size 变成 0，
        // 而续传校验要求清单 size 与本次一致，续传就会被判为过期重建。
        'size': '$totalBytes',
        'chunk_index': '$chunkIndex',
        'total_chunks': '$totalChunks',
        'offset': '${chunkIndex * effectiveChunkSize}',
        'encoding': 'binary',
        'sha256': sha256.convert(chunk).toString(),
      };
      try {
        await _postChunkWithRetry(
          url: chunkUrl,
          fields: chunkBody,
          chunk: chunk,
          timeout: chunkTimeout,
          onRetry: (attempt, error) {
            chunkRetries += 1;
            UploadDiag.chunk(
              uploadId: actualUploadId,
              chunkIndex: chunkIndex,
              payloadBytes: chunk.length,
              decodedBytes: chunk.length,
              startedAt: chunkStartedAt,
              endedAt: DateTime.now(),
              attempt: attempt,
              error: error.toString(),
            );
          },
        );
      } catch (error) {
        UploadDiag.chunk(
          uploadId: actualUploadId,
          chunkIndex: chunkIndex,
          payloadBytes: chunk.length,
          decodedBytes: chunk.length,
          startedAt: chunkStartedAt,
          endedAt: DateTime.now(),
          attempt: _chunkMaxAttempts,
          error: error.toString(),
        );
        rethrow;
      }
      final chunkEndedAt = DateTime.now();
      chunkDurationsMs.add(
        chunkEndedAt.difference(chunkStartedAt).inMilliseconds,
      );
      UploadDiag.chunk(
        uploadId: actualUploadId,
        chunkIndex: chunkIndex,
        payloadBytes: chunk.length,
        decodedBytes: chunk.length,
        startedAt: chunkStartedAt,
        endedAt: chunkEndedAt,
        attempt: 1,
      );
      uploadedBytes += chunk.length;
      completedChunks += 1;
      progress(
        uploadedBytes: uploadedBytes,
        completedChunks: completedChunks,
        stage: '上传分块',
      );
    }

    // 续传时保留设备侧已有进度，不要重置为 0。
    progress(
      uploadedBytes: uploadedBytes,
      completedChunks: completedChunks,
      stage: '上传分块',
    );
    var pending = <int>[];
    // 真滑动窗口：任意一块完成就立刻补一块新的，保持 concurrency 条通道始终满载。
    // 旧实现用「攒满一批 → 等整批完成」，批内最快与最慢的分块差多少就空转多少，
    // 实测 73.8MB 上传因此多花约 40% 时间。
    // 内存占用上限仍为 concurrency * chunkSize（在途块数不超过 concurrency）。
    final inFlight = <Future<void>>{};
    // 首个错误必须留存：失败的分块会立刻从 inFlight 移除，
    // 若只依赖最后的 Future.wait，错误会被漏掉而让上传「假装成功」。
    Object? firstError;
    StackTrace? firstStack;
    var nextIndex = 0;

    Future<void> awaitOneSlot() async {
      if (inFlight.length < effectiveConcurrency) return;
      // 等任意一块完成即可释放一个通道，而不是等整批。
      await Future.any(inFlight);
      // 被等待的那块若失败，此处抛出，避免错误只停留在闭包里。
      if (firstError != null) throw firstError!;
    }

    void submit(int index, Uint8List chunk) {
      late final Future<void> task;
      task = uploadChunk(index, chunk)
          .then<void>(
            (_) {},
            onError: (Object error, StackTrace stack) {
              firstError ??= error;
              firstStack ??= stack;
            },
          )
          .whenComplete(() => inFlight.remove(task));
      inFlight.add(task);
    }

    await for (final data in openRead()) {
      if (data.isEmpty) continue;
      pending.addAll(data);
      while (pending.length >= effectiveChunkSize) {
        final chunk = Uint8List.fromList(
          pending.sublist(0, effectiveChunkSize),
        );
        pending = pending.sublist(effectiveChunkSize);
        // 摘要按读取顺序计算，与上传并发无关。
        digestInput.add(chunk);
        await awaitOneSlot();
        submit(nextIndex, chunk);
        nextIndex += 1;
      }
    }
    if (pending.isNotEmpty) {
      final chunk = Uint8List.fromList(pending);
      digestInput.add(chunk);
      await awaitOneSlot();
      submit(nextIndex, chunk);
      nextIndex += 1;
    } else if (totalBytes == 0 && nextIndex == 0) {
      final chunk = Uint8List(0);
      digestInput.add(chunk);
      submit(0, chunk);
      nextIndex += 1;
    }
    // 排空所有在途分块；任何一块失败都在这里抛出。
    await Future.wait(inFlight.toList());
    if (firstError != null) {
      Error.throwWithStackTrace(firstError!, firstStack ?? StackTrace.current);
    }
    digestInput.close();

    if (uploadedBytes != totalBytes) {
      throw StateError('文件读取大小和声明大小不一致');
    }
    final fileHash = digestSink.value?.toString();
    if (fileHash == null || fileHash.isEmpty) {
      throw StateError('文件 sha256 计算失败');
    }

    progress(
      uploadedBytes: uploadedBytes,
      completedChunks: completedChunks,
      stage: '等待设备合并',
    );
    final data = await ApiClient.post('$basePath/complete', {
      'path': path,
      'upload_id': actualUploadId,
      'size': totalBytes,
      'total_chunks': totalChunks,
      'sha256': fileHash,
    }, timeout: const Duration(minutes: 3));
    progress(
      uploadedBytes: totalBytes,
      completedChunks: totalChunks,
      stage: '上传完成',
    );
    // 会话已经发布到目标目录，设备侧临时目录会被删掉，清掉续传记录，
    // 避免下次上传同一个文件时去查询一个已经不存在的会话。
    await UploadResumeStore.clear(resumeKey);
    UploadDiag.summary(
      uploadId: actualUploadId,
      totalBytes: totalBytes,
      totalChunks: totalChunks,
      chunkSize: effectiveChunkSize,
      concurrency: effectiveConcurrency,
      startedAt: uploadStartedAt,
      endedAt: DateTime.now(),
      chunkDurationsMs: chunkDurationsMs,
      retries: chunkRetries,
    );
    final file = (data['file'] as Map<String, dynamic>?) ?? data;
    return DeviceProjectFile.fromJson(file);
  }

  Future<DeviceDirectoryInfo> createDeviceDirectoryEmptyFile({
    required String machineId,
    required String path,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/directories/files/create',
      {'path': path},
    );
    final item = (data['file'] as Map<String, dynamic>?) ?? data;
    return DeviceDirectoryInfo.fromJson(item);
  }

  Future<DeviceDirectoryInfo> createDeviceDirectoryFolder({
    required String machineId,
    required String path,
  }) async {
    final data = await ApiClient.post(
      '/api/devices/$machineId/directories/folders/create',
      {'path': path},
    );
    final item = (data['file'] as Map<String, dynamic>?) ?? data;
    return DeviceDirectoryInfo.fromJson(item);
  }

  Future<void> deleteDeviceDirectoryFile({
    required String machineId,
    required String path,
    required bool isDir,
  }) async {
    await ApiClient.post('/api/devices/$machineId/directories/files/delete', {
      'path': path,
      'is_dir': isDir,
    });
  }

  static String _newUploadId() {
    final now = DateTime.now().microsecondsSinceEpoch;
    final random = Random.secure();
    final high = random.nextInt(1 << 20);
    final low = random.nextInt(1 << 20);
    return 'up_${now.toRadixString(36)}_${high.toRadixString(36)}${low.toRadixString(36)}';
  }

  Future<bool> getDeviceDirectoryAccess(String machineId) async {
    final data = await ApiClient.get(
      '/api/devices/$machineId/directory-access',
    );
    return data['allow_all'] as bool? ?? false;
  }

  Future<void> sendDeviceDirectoryAccessEmailCode({
    required String machineId,
    required String captchaId,
    required String captcha,
  }) async {
    await AppLogService.log(
      'device_directory_access_email_code_started',
      data: {'machine_id': machineId},
    );
    await ApiClient.post(
      '/api/devices/$machineId/directory-access/email-code',
      {'machine_id': machineId, 'captcha_id': captchaId, 'captcha': captcha},
    );
  }

  Future<bool> updateDeviceDirectoryAccess({
    required String machineId,
    required bool allowAll,
    String emailCode = '',
  }) async {
    await AppLogService.log(
      'device_directory_access_update_started',
      data: {
        'machine_id': machineId,
        'allow_all': allowAll,
        'has_email_code': emailCode.isNotEmpty,
      },
    );
    final data = await ApiClient.post(
      '/api/devices/$machineId/directory-access',
      {'allow_all': allowAll, 'email_code': emailCode},
    );
    await AppLogService.log(
      'device_directory_access_updated',
      data: {
        'machine_id': machineId,
        'allow_all': data['allow_all'] as bool? ?? false,
      },
    );
    return data['allow_all'] as bool? ?? false;
  }

  Future<void> createDeviceAgent({
    required String machineId,
    required String name,
    required String projectDir,
  }) async {
    await AppLogService.log(
      'device_agent_create_started',
      data: {'machine_id': machineId, 'name': name, 'project_dir': projectDir},
    );
    await ApiClient.post('/api/devices/$machineId/launcher/agents', {
      'name': name,
      'project_dir': projectDir,
    });
  }

  Future<void> restartDeviceAgent({
    required String machineId,
    required String agentId,
  }) async {
    await AppLogService.log(
      'device_agent_restart_started',
      data: {'machine_id': machineId, 'agent_id': agentId},
    );
    await ApiClient.post('/api/devices/$machineId/launcher/agents/restart', {
      'agent_id': agentId,
    });
  }

  Future<void> renameDeviceAgent({
    required String machineId,
    required String agentId,
    required String name,
  }) async {
    await AppLogService.log(
      'device_agent_rename_started',
      data: {'machine_id': machineId, 'agent_id': agentId, 'name': name},
    );
    await ApiClient.post('/api/devices/$machineId/launcher/agents/rename', {
      'agent_id': agentId,
      'name': name,
    });
  }

  Future<void> setDeviceAgentEnabled({
    required String machineId,
    required String agentId,
    required bool enabled,
  }) async {
    await AppLogService.log(
      'device_agent_enabled_update_started',
      data: {'machine_id': machineId, 'agent_id': agentId, 'enabled': enabled},
    );
    await ApiClient.post('/api/devices/$machineId/launcher/agents/enabled', {
      'agent_id': agentId,
      'enabled': enabled,
    });
  }

  Future<void> removeDeviceAgent({
    required String machineId,
    required String agentId,
  }) async {
    await AppLogService.log(
      'device_agent_remove_started',
      data: {'machine_id': machineId, 'agent_id': agentId},
    );
    await ApiClient.post('/api/devices/$machineId/launcher/agents/remove', {
      'agent_id': agentId,
    });
  }
}
