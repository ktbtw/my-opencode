import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

const _outDir =
    '/Users/yuminghao/Downloads/chat-codex/.chatcodex-artifacts/task_1790937072949017808';

Future<void> _loadFont(String family, String path) async {
  final file = File(path);
  if (!file.existsSync()) return;
  final bytes = await file.readAsBytes();
  final loader = FontLoader(family)
    ..addFont(Future.value(ByteData.view(Uint8List.fromList(bytes).buffer)));
  await loader.load();
}

void main() {
  setUpAll(() async {
    TestWidgetsFlutterBinding.ensureInitialized();
    await _loadFont(
      'MaterialIcons',
      '/Users/yuminghao/development/flutter/bin/cache/artifacts/material_fonts/MaterialIcons-Regular.otf',
    );
  });

  testWidgets('glyph probe', (tester) async {
    tester.view.physicalSize = const Size(1600, 500);
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    Widget cell(String name, IconData icon) => Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 72, color: Colors.black),
            Text(name, style: const TextStyle(fontSize: 11)),
          ],
        );

    await tester.pumpWidget(
      MaterialApp(
        home: ColoredBox(
          color: Colors.white,
          child: Center(
            child: Row(
              mainAxisAlignment: MainAxisAlignment.spaceEvenly,
              children: [
                cell('circle', Icons.circle),
                cell('fiber_manual', Icons.fiber_manual_record),
                cell('check_rounded', Icons.check_rounded),
                cell('check', Icons.check),
              ],
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await expectLater(
      find.byType(ColoredBox),
      matchesGoldenFile('$_outDir/_glyph_probe.png'),
    );
  });
}
