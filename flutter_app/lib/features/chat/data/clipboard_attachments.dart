import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:pasteboard/pasteboard.dart';
import 'package:path/path.dart' as p;

import '../presentation/attachment_drop_support.dart';
import 'attachment_file_bytes.dart';
import 'attachment_upload.dart';
import 'chat_model.dart';

class ClipboardPasteResult {
  final List<AttachedFile> attachments;
  final String? insertText;

  const ClipboardPasteResult({
    this.attachments = const [],
    this.insertText,
  });

  bool get hasAttachments => attachments.isNotEmpty;
  bool get hasInsertText => insertText != null && insertText!.isNotEmpty;
}

@visibleForTesting
Future<List<AttachedFile>> collectClipboardAttachments({
  required Future<List<String>> Function() readPaths,
  required Future<Uint8List?> Function() readImage,
  required Future<Uint8List?> Function(String path) readFileBytes,
  DateTime Function()? clock,
}) async {
  final files = <AttachedFile>[];
  for (final rawPath in await readPaths()) {
    final path = normalizeClipboardPath(rawPath);
    if (path.isEmpty) continue;
    final bytes = await readFileBytes(path);
    if (bytes == null || bytes.isEmpty) continue;
    files.add(
      buildAttachedFile(
        filename: p.basename(path),
        bytes: bytes,
      ),
    );
  }
  if (files.isNotEmpty) return files;

  final image = await readImage();
  if (image == null || image.isEmpty) return const [];
  return [
    buildAttachedFile(
      filename: pastedImageFilename(image, now: clock?.call()),
      bytes: image,
      mimeType: sniffImageMime(image) ?? 'image/png',
    ),
  ];
}

Future<ClipboardPasteResult> resolveClipboardPaste({
  required Future<List<AttachedFile>> Function() readFiles,
  required Future<AttachedFile?> Function() readImage,
  required Future<String> Function() readText,
  required bool pasteAsFile,
  required int threshold,
  DateTime Function()? clock,
}) async {
  final files = await readFiles();
  if (files.isNotEmpty) {
    return ClipboardPasteResult(attachments: files);
  }

  final text = await readText();
  if (text.isNotEmpty) {
    if (pasteAsFile && text.length > threshold) {
      final now = clock?.call() ?? DateTime.now();
      final bytes = utf8.encode(text);
      final label = '[Text ${text.characters.length}字]';
      return ClipboardPasteResult(
        attachments: [
          buildAttachedFile(
            filename: 'pasted_${now.millisecondsSinceEpoch}.txt',
            bytes: bytes,
            mimeType: 'text/plain',
            inlineToken: label,
          ),
        ],
      );
    }
    return ClipboardPasteResult(insertText: text);
  }

  final image = await readImage();
  if (image != null) {
    return ClipboardPasteResult(attachments: [image]);
  }
  return const ClipboardPasteResult();
}

Future<List<AttachedFile>> readClipboardFileAttachments() async {
  if (!isClipboardFilePasteSupported) return const [];
  return collectClipboardAttachments(
    readPaths: _readClipboardPaths,
    readImage: () async => null,
    readFileBytes: readAttachmentFileBytes,
  );
}

Future<AttachedFile?> readClipboardImageAttachment({
  DateTime Function()? clock,
}) async {
  if (!isClipboardFilePasteSupported) return null;
  final files = await collectClipboardAttachments(
    readPaths: () async => const [],
    readImage: _readClipboardImage,
    readFileBytes: readAttachmentFileBytes,
    clock: clock,
  );
  return files.isEmpty ? null : files.first;
}

@visibleForTesting
String normalizeClipboardPath(String path) {
  final value = path.trim();
  if (value.isEmpty) return '';
  if (value.startsWith('file:')) {
    try {
      return Uri.parse(value).toFilePath();
    } catch (_) {
      return value;
    }
  }
  return value;
}

Future<List<String>> _readClipboardPaths() async {
  try {
    return await Pasteboard.files();
  } catch (_) {
    return const [];
  }
}

Future<Uint8List?> _readClipboardImage() async {
  try {
    return await Pasteboard.image;
  } catch (_) {
    return null;
  }
}
