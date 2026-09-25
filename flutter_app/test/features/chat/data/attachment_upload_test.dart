import 'package:chat_codex_app/features/chat/data/attachment_upload.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('attachmentExtensionFromName handles file names', () {
    expect(attachmentExtensionFromName('demo.png'), 'png');
    expect(attachmentExtensionFromName('README.MD'), 'md');
    expect(attachmentExtensionFromName('archive.zip'), 'zip');
    expect(attachmentExtensionFromName('no_extension'), '');
  });

  test('attachmentMimeFromName keeps binary mappings and fallback', () {
    expect(attachmentMimeFromName('demo.jpg'), 'image/jpeg');
    expect(attachmentMimeFromName('demo.pdf'), 'application/pdf');
    expect(
      attachmentMimeFromName(
        'demo.unknown',
        fallbackMimeType: 'application/octet-stream',
      ),
      'application/octet-stream',
    );
  });

  test('text attachments stay text/plain even with a binary drop fallback', () {
    // 拖拽/网页入口会以 application/octet-stream 兜底；文本文件必须仍然归类为
    // text/plain，否则 Agent 会把它当不透明 file part 交给模型并报英文错误。
    const fallback = 'application/octet-stream';
    for (final name in [
      'notes.txt',
      'README.md',
      'data.csv',
      'app.log',
      'conf.yaml',
      'main.dart',
      'a.json',
    ]) {
      expect(
        attachmentMimeFromName(name, fallbackMimeType: fallback),
        'text/plain',
        reason: name,
      );
    }
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
