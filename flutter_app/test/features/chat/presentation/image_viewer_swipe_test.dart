import 'dart:typed_data';

import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/artifact_image_viewer_page.dart';
import 'package:chat_codex_app/features/chat/presentation/inline_image_viewer_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:photo_view/photo_view_gallery.dart';

/// 1x1 透明 PNG，作为可解码的最小图片字节。
final Uint8List _pngBytes = Uint8List.fromList(<int>[
  0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, //
  0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
  0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
  0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
  0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
  0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
  0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
  0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
  0x42, 0x60, 0x82,
]);

AiArtifact _artifact(String id, String filename) => AiArtifact(
  id: id,
  filename: filename,
  mimeType: 'image/png',
  sizeBytes: _pngBytes.length,
  relativePath: filename,
);

AiOutputFile _outputFile(String id, String filename) => AiOutputFile(
  id: id,
  filename: filename,
  mimeType: 'image/png',
  url: 'data:image/png;base64,'
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAACklE'
      'QVR4nGMAAQAABQABDQottAAAAABJRU5ErkJggg==',
);

Future<void> _pumpArtifactViewer(
  WidgetTester tester, {
  required List<AiArtifact> images,
  int initialIndex = 0,
}) async {
  await tester.pumpWidget(
    ProviderScope(
      child: MaterialApp(
        home: ArtifactImageViewerPage(
          taskId: 'task-1',
          images: images,
          initialIndex: initialIndex,
          bytesLoader: (_) async => _pngBytes,
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('artifact viewer shows every image in one horizontal gallery', (
    tester,
  ) async {
    final images = [
      _artifact('a1', '第一张.png'),
      _artifact('a2', '第二张.png'),
      _artifact('a3', '第三张.png'),
    ];
    await _pumpArtifactViewer(tester, images: images);

    // 三张图片在同一个分页画廊里，而不是各自独立的页面。
    expect(find.byType(PhotoViewGallery), findsOneWidget);
    expect(find.textContaining('第一张.png'), findsWidgets);
    expect(find.text('1 / 3'), findsOneWidget);
  });

  testWidgets('artifact viewer swipes to the next image', (tester) async {
    final images = [
      _artifact('a1', '第一张.png'),
      _artifact('a2', '第二张.png'),
    ];
    await _pumpArtifactViewer(tester, images: images);

    expect(find.text('1 / 2'), findsOneWidget);

    await tester.fling(
      find.byType(PhotoViewGallery),
      const Offset(-400, 0),
      1200,
    );
    await tester.pumpAndSettle();

    // 左滑后切到第二张，无需退出重进。
    expect(find.text('2 / 2'), findsOneWidget);
    expect(find.textContaining('第二张.png'), findsWidgets);
  });

  testWidgets('artifact viewer swipes back to the previous image', (
    tester,
  ) async {
    final images = [
      _artifact('a1', '第一张.png'),
      _artifact('a2', '第二张.png'),
    ];
    await _pumpArtifactViewer(tester, images: images, initialIndex: 1);

    expect(find.text('2 / 2'), findsOneWidget);

    await tester.fling(
      find.byType(PhotoViewGallery),
      const Offset(400, 0),
      1200,
    );
    await tester.pumpAndSettle();

    expect(find.text('1 / 2'), findsOneWidget);
  });

  testWidgets('single artifact image hides the page counter', (tester) async {
    await _pumpArtifactViewer(
      tester,
      images: [_artifact('a1', '只有一张.png')],
    );

    expect(find.byType(PhotoViewGallery), findsOneWidget);
    expect(find.textContaining(' / '), findsNothing);
  });

  testWidgets('inline generated images swipe horizontally', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: InlineImageViewerPage(
          images: [
            _outputFile('i1', '生成图1.png'),
            _outputFile('i2', '生成图2.png'),
          ],
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('1 / 2'), findsOneWidget);

    await tester.fling(
      find.byType(PhotoViewGallery),
      const Offset(-400, 0),
      1200,
    );
    await tester.pumpAndSettle();

    expect(find.text('2 / 2'), findsOneWidget);
    expect(find.textContaining('生成图2.png'), findsWidgets);
  });

  testWidgets('inline viewer opens at the tapped image', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: InlineImageViewerPage(
          images: [
            _outputFile('i1', '生成图1.png'),
            _outputFile('i2', '生成图2.png'),
            _outputFile('i3', '生成图3.png'),
          ],
          initialIndex: 2,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('3 / 3'), findsOneWidget);
    expect(find.textContaining('生成图3.png'), findsWidgets);
  });
}
