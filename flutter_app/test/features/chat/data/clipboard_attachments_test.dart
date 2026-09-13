import 'dart:typed_data';

import 'package:chat_codex_app/features/chat/data/attachment_upload.dart';
import 'package:chat_codex_app/features/chat/data/clipboard_attachments.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('normalizeClipboardPath unwraps file URIs', () {
    expect(
      normalizeClipboardPath('file:///tmp/a.png'),
      Uri.parse('file:///tmp/a.png').toFilePath(),
    );
    expect(normalizeClipboardPath('  D:\\shots\\b.jpg  '), 'D:\\shots\\b.jpg');
  });

  test('collectClipboardAttachments prefers copied files over bitmap preview',
      () async {
    final files = await collectClipboardAttachments(
      readPaths: () async => ['C:/tmp/note.txt'],
      readImage: () async => Uint8List.fromList(const [0x89, 0x50, 0x4E, 0x47]),
      readFileBytes: (path) async {
        expect(path, 'C:/tmp/note.txt');
        return Uint8List.fromList('hello'.codeUnits);
      },
    );

    expect(files, hasLength(1));
    expect(files.single.filename, 'note.txt');
    expect(files.single.mimeType, 'text/plain');
    expect(files.single.sizeBytes, 5);
  });

  test('collectClipboardAttachments uses clipboard image when no files',
      () async {
    final png = Uint8List.fromList(const [
      0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
    ]);
    final files = await collectClipboardAttachments(
      readPaths: () async => const [],
      readImage: () async => png,
      readFileBytes: (_) async => null,
      clock: () => DateTime.fromMillisecondsSinceEpoch(1700000000000),
    );

    expect(files, hasLength(1));
    expect(files.single.filename, 'pasted_1700000000000.png');
    expect(files.single.mimeType, 'image/png');
    expect(files.single.sizeBytes, png.length);
  });

  test('collectClipboardAttachments skips missing files and falls back to image',
      () async {
    final jpeg = Uint8List.fromList(const [0xFF, 0xD8, 0xFF, 0xE0]);
    final files = await collectClipboardAttachments(
      readPaths: () async => ['C:/missing.bin'],
      readImage: () async => jpeg,
      readFileBytes: (_) async => null,
      clock: () => DateTime.fromMillisecondsSinceEpoch(42),
    );

    expect(files.single.filename, 'pasted_42.jpg');
    expect(files.single.mimeType, 'image/jpeg');
  });

  test('resolveClipboardPaste attaches copied files before inserting text', () async {
    final result = await resolveClipboardPaste(
      readFiles: () async => [
        buildAttachedFile(filename: 'note.txt', bytes: const [1, 2, 3]),
      ],
      readImage: () async =>
          buildAttachedFile(filename: 'preview.png', bytes: const [4, 5]),
      readText: () async => 'should not insert',
      pasteAsFile: false,
      threshold: 500,
    );

    expect(result.hasAttachments, isTrue);
    expect(result.insertText, isNull);
    expect(result.attachments.single.filename, 'note.txt');
  });

  test('resolveClipboardPaste prefers text over a clipboard bitmap preview',
      () async {
    final result = await resolveClipboardPaste(
      readFiles: () async => const [],
      readImage: () async =>
          buildAttachedFile(filename: 'preview.png', bytes: const [4, 5]),
      readText: () async => 'hello',
      pasteAsFile: false,
      threshold: 500,
    );

    expect(result.hasAttachments, isFalse);
    expect(result.insertText, 'hello');
  });

  test('resolveClipboardPaste attaches screenshot when clipboard has no text',
      () async {
    final result = await resolveClipboardPaste(
      readFiles: () async => const [],
      readImage: () async =>
          buildAttachedFile(filename: 'shot.png', bytes: const [1, 2, 3]),
      readText: () async => '',
      pasteAsFile: false,
      threshold: 500,
    );

    expect(result.attachments.single.filename, 'shot.png');
    expect(result.insertText, isNull);
  });

  test('resolveClipboardPaste converts long text when enabled', () async {
    final result = await resolveClipboardPaste(
      readFiles: () async => const [],
      readImage: () async => null,
      readText: () async => 'a' * 501,
      pasteAsFile: true,
      threshold: 500,
      clock: () => DateTime.fromMillisecondsSinceEpoch(9),
    );

    expect(result.attachments, hasLength(1));
    expect(result.attachments.single.filename, 'pasted_9.txt');
    expect(result.attachments.single.inlineToken, '[Text 501字]');
    expect(result.insertText, isNull);
  });

  test('resolveClipboardPaste inserts short text', () async {
    final result = await resolveClipboardPaste(
      readFiles: () async => const [],
      readImage: () async => null,
      readText: () async => 'hello',
      pasteAsFile: true,
      threshold: 500,
    );

    expect(result.hasAttachments, isFalse);
    expect(result.insertText, 'hello');
  });
}
