Future<String?> saveArtifactBytes(
  List<int> bytes, {
  required String filename,
  required String mimeType,
  String? preferredDirectory,
}) async {
  return null;
}

/// 浏览器端没有文件系统，调用方应使用 saveArtifactBytes。
Future<String?> saveArtifactFile(
  String sourcePath, {
  required String filename,
  required String mimeType,
  String? preferredDirectory,
}) async {
  throw UnsupportedError('当前平台不支持按文件路径保存');
}
