import 'dart:typed_data';

import 'artifact_cache_service_io.dart'
    if (dart.library.html) 'artifact_cache_service_web.dart'
    as impl;
import 'chat_model.dart';

class ArtifactCacheService {
  ArtifactCacheService._();

  static Future<Uint8List?> read({
    required String taskId,
    required AiArtifact artifact,
  }) =>
      impl.ArtifactCachePlatform.read(taskId: taskId, artifactId: artifact.id);

  static Future<void> write({
    required String taskId,
    required AiArtifact artifact,
    required List<int> bytes,
  }) => impl.ArtifactCachePlatform.write(
    taskId: taskId,
    artifactId: artifact.id,
    bytes: bytes,
  );

  static Future<void> clear() => impl.ArtifactCachePlatform.clear();

  static Future<void> warmUpImagesFromCompletedPayload({
    required String taskId,
    required List<AiArtifact> artifacts,
    required List<AiOutputFile> files,
  }) async {
    if (taskId.isEmpty || artifacts.isEmpty || files.isEmpty) return;
    final imageArtifacts = artifacts
        .where((artifact) => artifact.isImage && artifact.id.isNotEmpty)
        .toList(growable: false);
    final candidates = files
        .where((file) => file.isImage && file.isDataUrl)
        .toList(growable: true);
    if (imageArtifacts.isEmpty || candidates.isEmpty) return;

    for (final artifact in imageArtifacts) {
      final matched = _takeBestMatch(candidates, artifact.filename);
      if (matched == null) continue;
      final bytes = matched.imageBytes;
      if (bytes == null || bytes.isEmpty) continue;
      await write(taskId: taskId, artifact: artifact, bytes: bytes);
    }
  }

  static AiOutputFile? _takeBestMatch(
    List<AiOutputFile> files,
    String artifactFilename,
  ) {
    if (files.isEmpty) return null;
    final normalizedArtifact = _normalizeFilename(artifactFilename);
    final exactIndex = files.indexWhere(
      (file) => _normalizeFilename(file.filename) == normalizedArtifact,
    );
    if (exactIndex >= 0) {
      return files.removeAt(exactIndex);
    }
    return files.removeAt(0);
  }

  static String _normalizeFilename(String input) => input.trim().toLowerCase();
}
