import 'dart:typed_data';

class ArtifactCachePlatform {
  ArtifactCachePlatform._();

  static Future<Uint8List?> read({
    required String taskId,
    required String artifactId,
  }) async => null;

  static Future<void> write({
    required String taskId,
    required String artifactId,
    required List<int> bytes,
  }) async {}

  static Future<void> clear() async {}
}
