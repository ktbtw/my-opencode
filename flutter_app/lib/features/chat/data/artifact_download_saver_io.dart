import 'dart:io';

import 'package:open_filex/open_filex.dart';
import 'package:path_provider/path_provider.dart';

Future<String?> saveArtifactBytes(
  List<int> bytes, {
  required String filename,
  required String mimeType,
  String? preferredDirectory,
}) async {
  Future<String> writeInto(String dirPath) async {
    final directory = Directory(dirPath);
    if (!await directory.exists()) {
      await directory.create(recursive: true);
    }
    final file = File('${directory.path}${Platform.pathSeparator}$filename');
    await file.writeAsBytes(bytes, flush: true);
    return file.path;
  }

  String savedPath;
  final customPath = preferredDirectory?.trim();
  if (customPath != null && customPath.isNotEmpty) {
    try {
      savedPath = await writeInto(customPath);
    } catch (_) {
      final fallback =
          await getDownloadsDirectory() ?? await getApplicationDocumentsDirectory();
      savedPath = await writeInto(fallback.path);
    }
  } else {
    final fallback =
        await getDownloadsDirectory() ?? await getApplicationDocumentsDirectory();
    savedPath = await writeInto(fallback.path);
  }

  await OpenFilex.open(savedPath);
  return savedPath;
}
