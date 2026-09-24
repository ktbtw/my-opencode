import '../../project_settings/project_settings_provider.dart';
import 'package:dio/dio.dart';
import 'dart:typed_data';
import 'package:shared_preferences/shared_preferences.dart';

import '../../../core/config/api_client.dart';
import '../../../core/storage/app_storage.dart';
import 'artifact_download_saver.dart';
import 'artifact_cache_service.dart';
import 'chat_model.dart';
import '../../settings/settings_provider.dart';

class ArtifactDownloadService {
  ArtifactDownloadService._();

  static Options _options() {
    final token = AppStorage.getToken();
    return Options(
      responseType: ResponseType.bytes,
      headers: {
        if (token != null && token.isNotEmpty) 'Authorization': 'Bearer $token',
      },
    );
  }

  static String _url(String taskId, AiArtifact artifact) =>
      '${ApiClient.baseUrl}/api/tasks/$taskId/artifacts/${artifact.id}/download';

  static Future<Uint8List> fetchBytes({
    required String taskId,
    required AiArtifact artifact,
    void Function(int received, int total)? onReceiveProgress,
  }) async {
    final cached = await ArtifactCacheService.read(
      taskId: taskId,
      artifact: artifact,
    );
    if (cached != null && cached.isNotEmpty) {
      onReceiveProgress?.call(cached.length, cached.length);
      return cached;
    }

    final response = await Dio().get<List<int>>(
      _url(taskId, artifact),
      options: _options(),
      onReceiveProgress: onReceiveProgress,
    );
    final bytes = response.data;
    if (bytes == null || bytes.isEmpty) {
      throw Exception('文件内容为空');
    }
    final result = Uint8List.fromList(bytes);
    await ArtifactCacheService.write(
      taskId: taskId,
      artifact: artifact,
      bytes: result,
    );
    return result;
  }

  static Future<String?> download({
    required String taskId,
    String? project,
    required AiArtifact artifact,
    void Function(int received, int total)? onReceiveProgress,
  }) async {
    final identity = AppStorage.storageIdentity;
    final prefs = await SharedPreferences.getInstance();
    final directory = project == null
        ? null
        : ProjectSettings.read(prefs, identity, project).downloadDirectory;
    final preferredDirectory =
        directory ?? prefs.getString(AppSettings.downloadDirectoryKey);
    final bytes = await fetchBytes(
      taskId: taskId,
      artifact: artifact,
      onReceiveProgress: onReceiveProgress,
    );
    if (AppStorage.storageIdentity != identity) throw StateError('账号已切换，请重新下载');
    return saveArtifactBytes(
      bytes,
      filename: artifact.filename,
      mimeType: artifact.mimeType,
      preferredDirectory: preferredDirectory,
    );
  }
}
