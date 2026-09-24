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
