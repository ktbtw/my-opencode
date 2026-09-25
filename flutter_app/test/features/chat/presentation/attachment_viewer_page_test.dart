import 'dart:convert';

import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/attachment_viewer_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

AttachedFile _file({
  required String filename,
  required String mimeType,
  String content = '',
  List<int>? bytes,
}) {
  return AttachedFile(
    id: 'test',
    filename: filename,
    mimeType: mimeType,
    base64Data: base64Encode(bytes ?? utf8.encode(content)),
    sizeBytes: bytes?.length ?? utf8.encode(content).length,
  );
}

Future<void> _pump(WidgetTester tester, AttachedFile file) async {
  await tester.pumpWidget(MaterialApp(home: AttachmentViewerPage(file: file)));
  await tester.pump();
}

void main() {
  testWidgets('text attachment opens highlighted and is editable', (
    tester,
  ) async {
    await _pump(
      tester,
      _file(
        filename: 'data.json',
        mimeType: 'application/json',
        content: '{\n  "name": "value"\n}',
      ),
    );

    expect(find.text('data.json'), findsOneWidget);
    expect(find.textContaining('高亮：json'), findsOneWidget);

    final field = tester.widget<TextField>(find.byType(TextField));
    expect(field.readOnly, isTrue);

    // 点击编辑后可以改写内容，并标记未保存。
    await tester.tap(find.byTooltip('编辑'));
    await tester.pump();
    final editable = tester.widget<TextField>(find.byType(TextField));
    expect(editable.readOnly, isFalse);

    await tester.enterText(find.byType(TextField), '{"name": "edited"}');
    await tester.pump();
    expect(find.textContaining('未保存'), findsOneWidget);
  });

  testWidgets('oversized historical attachment explains missing content', (
    tester,
  ) async {
    await _pump(
      tester,
      AttachedFile(
        id: 'test',
        filename: 'big.txt',
        mimeType: 'text/plain',
        base64Data: '',
        sizeBytes: 0,
      ),
    );

    expect(find.text('内容未保留'), findsOneWidget);
    expect(find.textContaining('无法预览或编辑'), findsOneWidget);
  });

  testWidgets('binary attachment offers save instead of preview', (
    tester,
  ) async {
    await _pump(
      tester,
      _file(
        filename: 'archive.zip',
        mimeType: 'application/zip',
        bytes: const [0x50, 0x4b, 0x03, 0x04],
      ),
    );

    expect(find.text('archive.zip'), findsWidgets);
    expect(find.textContaining('暂不支持在应用内预览'), findsOneWidget);
    expect(find.byType(TextField), findsNothing);
  });

  testWidgets('image attachment renders a zoomable viewer', (tester) async {
    // 1x1 透明 PNG，保证解码路径真实可用。
    final png = base64Decode(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFAAH/q842iQAAAABJRU5ErkJggg==',
    );
    await _pump(
      tester,
      _file(
        filename: 'shot.png',
        mimeType: 'image/png',
        bytes: png,
      ),
    );

    expect(find.textContaining('双指缩放'), findsOneWidget);
    expect(find.byTooltip('旋转'), findsOneWidget);
    expect(find.byType(TextField), findsNothing);
  });
}
