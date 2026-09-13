import 'dart:io';
import 'dart:typed_data';

Future<Uint8List?> readAttachmentFileBytes(String path) async {
  try {
    final type = FileSystemEntity.typeSync(path, followLinks: true);
    if (type != FileSystemEntityType.file) return null;
    return await File(path).readAsBytes();
  } catch (_) {
    return null;
  }
}
