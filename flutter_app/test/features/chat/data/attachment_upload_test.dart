import 'package:chat_codex_app/features/chat/data/attachment_upload.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('attachmentExtensionFromName handles file names', () {
    expect(attachmentExtensionFromName('demo.png'), 'png');
    expect(attachmentExtensionFromName('README.MD'), 'md');
    expect(attachmentExtensionFromName('archive.zip'), 'zip');
    expect(attachmentExtensionFromName('no_extension'), '');
  });

  test('attachmentMimeFromName prefers known extension mapping', () {
    expect(attachmentMimeFromName('demo.jpg'), 'image/jpeg');
    expect(attachmentMimeFromName('demo.json'), 'application/json');
    expect(
      attachmentMimeFromName(
        'demo.unknown',
        fallbackMimeType: 'application/octet-stream',
      ),
      'application/octet-stream',
    );
  });

  test('buildAttachedFile encodes payload and size', () {
    final file = buildAttachedFile(
      filename: 'hello.txt',
      bytes: 'hello'.codeUnits,
    );

    expect(file.filename, 'hello.txt');
    expect(file.mimeType, 'text/plain');
    expect(file.sizeBytes, 5);
    expect(file.base64Data, 'aGVsbG8=');
  });

  test('sniffImageMime recognizes common image headers', () {
    expect(
      sniffImageMime(const [0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]),
      'image/png',
    );
    expect(sniffImageMime(const [0xFF, 0xD8, 0xFF, 0xE0]), 'image/jpeg');
    expect(sniffImageMime(const [0x47, 0x49, 0x46, 0x38, 0x39, 0x61]), 'image/gif');
    expect(sniffImageMime(const [0x42, 0x4D, 0x00, 0x00]), 'image/bmp');
  });

  test('pastedImageFilename uses sniffed extension', () {
    expect(
      pastedImageFilename(
        const [0xFF, 0xD8, 0xFF],
        now: DateTime.fromMillisecondsSinceEpoch(100),
      ),
      'pasted_100.jpg',
    );
  });

  test('image attachment reuses decoded preview bytes', () {
    final file = buildAttachedFile(
      filename: 'preview.png',
      bytes: [137, 80, 78, 71],
    );

    final first = file.imageBytes;
    expect(first, isNotNull);
    expect(file.imageBytes, same(first));
  });
}
