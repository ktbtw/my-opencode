import 'dart:io';
import 'dart:typed_data';

import 'package:path_provider/path_provider.dart';

class ArtifactCachePlatform {
  ArtifactCachePlatform._();

  static const _cacheDirName = 'artifact-cache';

  static Future<Uint8List?> read({
    required String taskId,
    required String artifactId,
  }) async {
    try {
      final file = await _cacheFile(taskId: taskId, artifactId: artifactId);
      if (!await file.exists()) return null;
      final bytes = await file.readAsBytes();
      if (bytes.isEmpty) {
        await file.delete();
        return null;
      }
      return bytes;
    } catch (_) {
      return null;
    }
  }

  static Future<void> write({
    required String taskId,
    required String artifactId,
    required List<int> bytes,
  }) async {
    if (bytes.isEmpty) return;
    try {
      final file = await _cacheFile(taskId: taskId, artifactId: artifactId);
      await file.writeAsBytes(bytes, flush: false);
    } catch (_) {}
  }

  static Future<void> clear() async {
    try {
      final dir = await _cacheDirectory();
      if (await dir.exists()) {
        await dir.delete(recursive: true);
      }
    } catch (_) {}
  }

  static Future<Directory> _cacheDirectory() async {
    final baseDir = await getTemporaryDirectory();
    final dir = Directory(
      '${baseDir.path}${Platform.pathSeparator}$_cacheDirName',
    );
    if (!await dir.exists()) {
      await dir.create(recursive: true);
    }
    return dir;
  }

  static Future<File> _cacheFile({
    required String taskId,
    required String artifactId,
  }) async {
    final dir = await _cacheDirectory();
    return File(
      '${dir.path}${Platform.pathSeparator}${_safe(taskId)}__${_safe(artifactId)}.bin',
    );
  }

  static String _safe(String input) =>
      input.replaceAll(RegExp(r'[^A-Za-z0-9._-]+'), '_');
}
