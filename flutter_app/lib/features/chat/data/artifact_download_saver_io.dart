import 'dart:io';

import 'package:flutter/services.dart';
import 'package:open_filex/open_filex.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

Future<String?> saveArtifactBytes(
  List<int> bytes, {
  required String filename,
  required String mimeType,
  String? preferredDirectory,
}) async {
  final name = p
      .basename(filename.replaceAll('\\', '/'))
      .replaceAll(RegExp(r'[\x00-\x1f]'), '_');
  if (name.isEmpty || name == '.' || name == '..') throw ArgumentError('文件名无效');
  final customPath = preferredDirectory?.trim();
  if (Platform.isAndroid &&
      customPath != null &&
      customPath.startsWith('content://')) {
    return const MethodChannel(
      'chat_codex/artifact_directory',
    ).invokeMethod<String>('saveFile', {
      'directory': customPath,
      'filename': name,
      'mimeType': mimeType,
      'bytes': Uint8List.fromList(bytes),
    });
  }
  if (Platform.isAndroid && customPath != null && customPath.isNotEmpty) {
    // 分区存储下只有 SAF 授权目录（content://）可写。普通路径是历史遗留设置，
    // 直接写会抛系统权限错误，这里换成可执行的提示。
    if (!customPath.startsWith('content://')) {
      throw StateError('当前下载目录没有写入权限。请在「设置 → 下载目录」或「项目设置」里重新选择一个目录后重试。');
    }
  }
  final directory = customPath != null && customPath.isNotEmpty
      ? Directory(customPath)
      : await getDownloadsDirectory() ??
            await getApplicationDocumentsDirectory();
  // A selected location must either succeed or report the real error. Silently
  // falling back makes a project-specific directory appear to work when it did not.
  await directory.create(recursive: true);
  var file = File(p.join(directory.path, name));
  var suffix = 1;
  while (await file.exists()) {
    file = File(
      p.join(
        directory.path,
        '${p.basenameWithoutExtension(name)} (${suffix++})${p.extension(name)}',
      ),
    );
  }
  await file.writeAsBytes(bytes, flush: true);
  await OpenFilex.open(file.path);
  return file.path;
}

/// 从临时文件保存（流式复制，不把整个文件读进内存），用于大文件下载。
Future<String?> saveArtifactFile(
  String sourcePath, {
  required String filename,
  required String mimeType,
  String? preferredDirectory,
}) async {
  final name = p
      .basename(filename.replaceAll('\\', '/'))
      .replaceAll(RegExp(r'[\x00-\x1f]'), '_');
  if (name.isEmpty || name == '.' || name == '..') throw ArgumentError('文件名无效');
  final customPath = preferredDirectory?.trim();
  if (Platform.isAndroid &&
      customPath != null &&
      customPath.startsWith('content://')) {
    return const MethodChannel('chat_codex/artifact_directory')
        .invokeMethod<String>('saveFilePath', {
          'directory': customPath,
          'filename': name,
          'mimeType': mimeType,
          'sourcePath': sourcePath,
        });
  }
  if (Platform.isAndroid && customPath != null && customPath.isNotEmpty) {
    if (!customPath.startsWith('content://')) {
      throw StateError('当前下载目录没有写入权限。请在「设置 → 下载目录」或「项目设置」里重新选择一个目录后重试。');
    }
  }
  final directory = customPath != null && customPath.isNotEmpty
      ? Directory(customPath)
      : await getDownloadsDirectory() ??
            await getApplicationDocumentsDirectory();
  await directory.create(recursive: true);
  var target = File(p.join(directory.path, name));
  var suffix = 1;
  while (await target.exists()) {
    target = File(
      p.join(
        directory.path,
        '${p.basenameWithoutExtension(name)} (${suffix++})${p.extension(name)}',
      ),
    );
  }
  await File(sourcePath).copy(target.path);
  await OpenFilex.open(target.path);
  return target.path;
}
