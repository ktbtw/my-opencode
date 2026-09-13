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
import 'device_semantic_agent_model.dart';
import 'device_skill_model.dart';

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
        await ApiClient.post('$basePath/chunk', {
          'path': path,
          'upload_id': actualUploadId,
          'chunk_index': chunkIndex,
          'total_chunks': totalChunks,
          'offset': start,
          'content': base64Encode(chunk),
          'encoding': 'base64',
          'sha256': chunkHash,
        });
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
    int chunkSize = defaultUploadChunkSize,
    DeviceUploadProgressCallback? onProgress,
  }) {
    return _uploadFileChunkedStreamed(
      basePath: '/api/devices/$machineId/launcher/agents/$agentId/files/upload',
      path: path,
      totalBytes: totalBytes,
      openRead: openRead,
      uploadId: uploadId,
      chunkSize: chunkSize,
      onProgress: onProgress,
    );
  }

  Future<DeviceProjectFile> uploadDeviceDirectoryFileChunkedStreamed({
    required String machineId,
    required String path,
    required int totalBytes,
    required Stream<List<int>> Function() openRead,
    String? uploadId,
    int chunkSize = defaultUploadChunkSize,
    DeviceUploadProgressCallback? onProgress,
  }) {
    return _uploadFileChunkedStreamed(
      basePath: '/api/devices/$machineId/directories/files/upload',
      path: path,
      totalBytes: totalBytes,
      openRead: openRead,
      uploadId: uploadId,
      chunkSize: chunkSize,
      onProgress: onProgress,
    );
  }

  Future<DeviceProjectFile> _uploadFileChunkedStreamed({
    required String basePath,
    required String path,
    required int totalBytes,
    required Stream<List<int>> Function() openRead,
    String? uploadId,
    int chunkSize = defaultUploadChunkSize,
    DeviceUploadProgressCallback? onProgress,
  }) async {
    if (chunkSize <= 0) {
      throw ArgumentError.value(chunkSize, 'chunkSize', '必须大于 0');
    }
    if (totalBytes < 0) {
      throw ArgumentError.value(totalBytes, 'totalBytes', '不能小于 0');
    }

    final actualUploadId = uploadId ?? _newUploadId();
    final totalChunks = max(1, (totalBytes / chunkSize).ceil());
    final uploaded = List<int>.filled(totalChunks, 0);
    final digestSink = _DigestSink();
    final digestInput = sha256.startChunkedConversion(digestSink);
    var uploadedBytes = 0;
    var completedChunks = 0;
    var nextChunkIndex = 0;

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

    Future<void> uploadChunk(List<int> bytes) async {
      if (nextChunkIndex >= totalChunks) {
        throw StateError('文件读取大小超过声明大小');
      }
      final chunk = Uint8List.fromList(bytes);
      final chunkIndex = nextChunkIndex;
      final offset = uploadedBytes;
      digestInput.add(chunk);
      final chunkHash = sha256.convert(chunk).toString();
      await ApiClient.post('$basePath/chunk', {
        'path': path,
        'upload_id': actualUploadId,
        'chunk_index': chunkIndex,
        'total_chunks': totalChunks,
        'offset': offset,
        'content': base64Encode(chunk),
        'encoding': 'base64',
        'sha256': chunkHash,
      });
      uploaded[chunkIndex] = chunk.length;
      uploadedBytes += chunk.length;
      completedChunks += 1;
      nextChunkIndex += 1;
      progress(
        uploadedBytes: uploadedBytes,
        completedChunks: completedChunks,
        stage: '上传分块',
      );
    }

    progress(uploadedBytes: 0, completedChunks: 0, stage: '准备上传');
    await ApiClient.post('$basePath/create', {
      'path': path,
      'upload_id': actualUploadId,
      'size': totalBytes,
      'total_chunks': totalChunks,
    });

    progress(uploadedBytes: 0, completedChunks: 0, stage: '上传分块');
    var pending = <int>[];
    await for (final data in openRead()) {
      if (data.isEmpty) continue;
      pending.addAll(data);
      while (pending.length >= chunkSize) {
        final chunk = pending.sublist(0, chunkSize);
        pending = pending.sublist(chunkSize);
        await uploadChunk(chunk);
      }
    }
    if (pending.isNotEmpty) {
      await uploadChunk(pending);
    } else if (totalBytes == 0 && completedChunks == 0) {
      await uploadChunk(const []);
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
    });
    progress(
      uploadedBytes: totalBytes,
      completedChunks: totalChunks,
      stage: '上传完成',
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
