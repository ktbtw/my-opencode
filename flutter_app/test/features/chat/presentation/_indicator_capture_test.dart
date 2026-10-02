import 'dart:io';
import 'dart:typed_data';

import 'package:chat_codex_app/core/theme/app_theme.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_page.dart';
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
    // The test harness does not load the app's icon font by default, which
    // makes every Icon render as a tofu box. Load it so the capture reflects
    // what the real device actually draws.
    await _loadFont(
      'MaterialIcons',
      '/Users/yuminghao/development/flutter/bin/cache/artifacts/material_fonts/MaterialIcons-Regular.otf',
    );
    await _loadFont('CJK', '/System/Library/Fonts/Hiragino Sans GB.ttc');
  });

  testWidgets('capture indicator states', (tester) async {
    tester.view.physicalSize = const Size(780, 1560);
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final question = QuestionRequestInfo(
      requestId: 'q1',
      questions: [
        QuestionItemInfo(
          question: '这次希望改到什么程度？',
          header: '改动范围',
          options: const [
            QuestionOptionInfo(
              label: '只改最小范围',
              description: '仅调整通知卡片收起逻辑，不动其他代码',
            ),
            QuestionOptionInfo(
              label: '顺带优化交互',
              description: '同时调整展开动画与自动收起时长',
            ),
          ],
          multiple: false,
          custom: false,
        ),
      ],
    );

    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light.copyWith(
          textTheme: AppTheme.light.textTheme.apply(fontFamily: 'CJK'),
        ),
        home: Scaffold(
          backgroundColor: const Color(0xFFF2F6FC),
          body: Center(
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12),
              child: QuestionBanner(
                question: question,
                onSubmit: (_, _) {},
                onReject: (_) {},
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('只改最小范围'));
    await tester.pumpAndSettle();
    await expectLater(
      find.byType(QuestionBanner),
      matchesGoldenFile('$_outDir/indicator-radio-selected.png'),
    );
  });
}
