import 'dart:convert';
import 'chat_model.dart';

String attachmentExtensionFromName(String filename) {
  final index = filename.lastIndexOf('.');
  if (index <= 0 || index >= filename.length - 1) return '';
  return filename.substring(index + 1).toLowerCase();
}

String attachmentMimeFromName(String filename, {String? fallbackMimeType}) {
  return switch (attachmentExtensionFromName(filename)) {
    'jpg' || 'jpeg' => 'image/jpeg',
    'png' => 'image/png',
    'gif' => 'image/gif',
    'webp' => 'image/webp',
    'bmp' => 'image/bmp',
    'pdf' => 'application/pdf',
    'html' => 'text/html',
    'css' => 'text/css',
    'json' => 'application/json',
    'xml' => 'application/xml',
    _ =>
      fallbackMimeType?.trim().isNotEmpty == true
          ? fallbackMimeType!.trim()
          : 'text/plain',
  };
}

AttachedFile buildAttachedFile({
  required String filename,
  required List<int> bytes,
  String? mimeType,
  String? inlineToken,
}) {
  return AttachedFile(
    id: '${DateTime.now().microsecondsSinceEpoch}_$filename',
    filename: filename,
    mimeType: attachmentMimeFromName(filename, fallbackMimeType: mimeType),
    base64Data: base64Encode(bytes),
    sizeBytes: bytes.length,
    inlineToken: inlineToken,
  );
}

String? sniffImageMime(List<int> bytes) {
  if (bytes.length >= 8 &&
      bytes[0] == 0x89 &&
      bytes[1] == 0x50 &&
      bytes[2] == 0x4E &&
      bytes[3] == 0x47) {
    return 'image/png';
  }
  if (bytes.length >= 3 &&
      bytes[0] == 0xFF &&
      bytes[1] == 0xD8 &&
      bytes[2] == 0xFF) {
    return 'image/jpeg';
  }
  if (bytes.length >= 6 &&
      bytes[0] == 0x47 &&
      bytes[1] == 0x49 &&
      bytes[2] == 0x46) {
    return 'image/gif';
  }
  if (bytes.length >= 12 &&
      bytes[0] == 0x52 &&
      bytes[1] == 0x49 &&
      bytes[2] == 0x46 &&
      bytes[3] == 0x46 &&
      bytes[8] == 0x57 &&
      bytes[9] == 0x45 &&
      bytes[10] == 0x42 &&
      bytes[11] == 0x50) {
    return 'image/webp';
  }
  if (bytes.length >= 2 && bytes[0] == 0x42 && bytes[1] == 0x4D) {
    return 'image/bmp';
  }
  return null;
}

String pastedImageFilename(List<int> bytes, {DateTime? now}) {
  final mime = sniffImageMime(bytes) ?? 'image/png';
  final ext = switch (mime) {
    'image/jpeg' => 'jpg',
    'image/gif' => 'gif',
    'image/webp' => 'webp',
    'image/bmp' => 'bmp',
    _ => 'png',
  };
  final stamp = (now ?? DateTime.now()).millisecondsSinceEpoch;
  return 'pasted_$stamp.$ext';
}
