import 'package:chat_codex_app/core/theme/app_theme.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('final delivery phase uses structured completion guard metadata', () {
    expect(
      finalDeliveryPhaseFromProgressEvent({
        'type': 'progress',
        'message': '文案可以变化',
        'metadata': {
          'source': 'completion_guard',
          'reason': 'missing_artifact',
        },
      }),
      FinalDeliveryPhase.validating,
    );
    expect(
      finalDeliveryPhaseFromProgressEvent({
        'type': 'progress',
        'message': '正在校验交付文件',
        'metadata': {'source': 'llm_usage'},
      }),
      FinalDeliveryPhase.idle,
    );
  });

  testWidgets('final delivery progress fits a narrow mobile bubble', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light,
        home: const Scaffold(
          body: Center(
            child: SizedBox(width: 176, child: FinalDeliveryProgress()),
          ),
        ),
      ),
    );
    await tester.pump(const Duration(milliseconds: 100));

    expect(
      find.byKey(const ValueKey('final-delivery-progress')),
      findsOneWidget,
    );
    expect(find.text('正在完成最终交付'), findsOneWidget);
    expect(find.text('检查文件完整性与下载状态'), findsOneWidget);
    expect(find.byIcon(Icons.inventory_2_outlined), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
