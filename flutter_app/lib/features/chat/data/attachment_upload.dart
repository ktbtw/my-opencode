import 'dart:convert';
import 'chat_model.dart';

String attachmentExtensionFromName(String filename) {
  final index = filename.lastIndexOf('.');
  if (index <= 0 || index >= filename.length - 1) return '';
  return filename.substring(index + 1).toLowerCase();
}

// 明确属于二进制/需要原样透传的扩展名。
const _binaryMimeByExtension = <String, String>{
  'jpg': 'image/jpeg',
  'jpeg': 'image/jpeg',
  'png': 'image/png',
  'gif': 'image/gif',
  'webp': 'image/webp',
  'bmp': 'image/bmp',
  'pdf': 'application/pdf',
};

// 文本类扩展名一律按 text/plain 发送。Agent 只会把 mime 恰好为 text/plain 的
// data: 附件解码成可读文本（opencode prompt.ts），其它 mime 会被当成不透明的
// file part 透传给模型，多数模型会直接以英文 “not supported” 报错。
const _textExtensions = <String>{
  'txt', 'text', 'log', 'md', 'markdown', 'rst',
  'csv', 'tsv', 'json', 'jsonl', 'ndjson', 'xml', 'html', 'htm', 'css',
  'yaml', 'yml', 'toml', 'ini', 'cfg', 'conf', 'properties', 'env',
  'sql', 'sh', 'bash', 'zsh', 'bat', 'cmd', 'ps1',
  'dart', 'js', 'jsx', 'ts', 'tsx', 'vue', 'svelte',
  'py', 'rb', 'php', 'java', 'kt', 'kts', 'gradle', 'scala', 'groovy',
  'go', 'rs', 'c', 'h', 'cc', 'cpp', 'cxx', 'hpp', 'cs', 'swift', 'm', 'mm',
  'lua', 'pl', 'r', 'tex', 'srt', 'vtt', 'diff', 'patch', 'pem', 'crt',
};

String attachmentMimeFromName(String filename, {String? fallbackMimeType}) {
  final extension = attachmentExtensionFromName(filename);
  final binary = _binaryMimeByExtension[extension];
  if (binary != null) return binary;
  if (_textExtensions.contains(extension)) return 'text/plain';
  final fallback = fallbackMimeType?.trim() ?? '';
  return fallback.isNotEmpty ? fallback : 'text/plain';
}

/// 判断附件能否按文本打开（可预览/编辑）。图片与 PDF 走各自的查看方式。
bool isTextAttachment(String filename, String mimeType) {
  if (mimeType.startsWith('image/')) return false;
  if (mimeType == 'application/pdf') return false;
  if (mimeType.startsWith('text/')) return true;
  if (mimeType == 'application/json' ||
      mimeType.endsWith('+json') ||
      mimeType == 'application/xml' ||
      mimeType.endsWith('+xml') ||
      mimeType == 'application/javascript' ||
      mimeType == 'application/x-yaml') {
    return true;
  }
  return _textExtensions.contains(attachmentExtensionFromName(filename));
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
