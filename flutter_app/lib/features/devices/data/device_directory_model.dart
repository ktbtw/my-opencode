class DeviceDirectoryInfo {
  final String path;
  final String name;
  final String kind;
  final bool isDir;
  final int size;

  const DeviceDirectoryInfo({
    required this.path,
    required this.name,
    required this.kind,
    required this.isDir,
    this.size = 0,
  });

  factory DeviceDirectoryInfo.fromJson(Map<String, dynamic> json) {
    return DeviceDirectoryInfo(
      path: json['path'] as String? ?? '',
      name: json['name'] as String? ?? '',
      kind: json['kind'] as String? ?? '',
      isDir: json['is_dir'] as bool? ?? false,
      size: (json['size'] as num?)?.toInt() ?? 0,
    );
  }
}

class DeviceProjectFile {
  final String path;
  final String name;
  final String content;
  final String encoding;
  final int size;
  final String sha256;

  const DeviceProjectFile({
    required this.path,
    required this.name,
    this.content = '',
    this.encoding = 'base64',
    this.size = 0,
    this.sha256 = '',
  });

  factory DeviceProjectFile.fromJson(Map<String, dynamic> json) {
    return DeviceProjectFile(
      path: json['path'] as String? ?? '',
      name: json['name'] as String? ?? '',
      content: json['content'] as String? ?? '',
      encoding: json['encoding'] as String? ?? 'base64',
      size: (json['size'] as num?)?.toInt() ?? 0,
      sha256: json['sha256'] as String? ?? '',
    );
  }
}

class DeviceUploadProgress {
  final String uploadId;
  final int uploadedBytes;
  final int totalBytes;
  final int completedChunks;
  final int totalChunks;
  final String stage;

  const DeviceUploadProgress({
    required this.uploadId,
    required this.uploadedBytes,
    required this.totalBytes,
    required this.completedChunks,
    required this.totalChunks,
    required this.stage,
  });

  double get ratio {
    if (totalBytes <= 0) return completedChunks >= totalChunks ? 1 : 0;
    return (uploadedBytes / totalBytes).clamp(0, 1);
  }

  int get percent => (ratio * 100).round().clamp(0, 100);
}

class DeviceDownloadProgress {
  final int downloadedBytes;
  final int totalBytes;
  final int completedChunks;
  final int totalChunks;
  final String stage;

  const DeviceDownloadProgress({
    required this.downloadedBytes,
    required this.totalBytes,
    required this.completedChunks,
    required this.totalChunks,
    required this.stage,
  });

  double get ratio {
    if (totalBytes <= 0) return completedChunks >= totalChunks ? 1 : 0;
    return (downloadedBytes / totalBytes).clamp(0, 1);
  }

  int get percent => (ratio * 100).round().clamp(0, 100);
}

class DeviceDirectoryResult {
  final String currentPath;
  final String parentPath;
  final List<DeviceDirectoryInfo> entries;

  const DeviceDirectoryResult({
    required this.currentPath,
    required this.parentPath,
    required this.entries,
  });

  factory DeviceDirectoryResult.fromJson(Map<String, dynamic> json) {
    final entries = (json['entries'] as List<dynamic>? ?? [])
        .whereType<Map<String, dynamic>>()
        .map(DeviceDirectoryInfo.fromJson)
        .toList();
    return DeviceDirectoryResult(
      currentPath: json['current_path'] as String? ?? '',
      parentPath: json['parent_path'] as String? ?? '',
      entries: entries,
    );
  }
}
