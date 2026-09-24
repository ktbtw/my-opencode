import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  const modelA = ModelInfo(
    providerID: 'provider',
    modelID: 'a',
    name: 'A',
    contextLimit: 100000,
  );
  const modelB = ModelInfo(
    providerID: 'provider',
    modelID: 'b',
    name: 'B',
    contextLimit: 200000,
  );
  const usage = ContextUsageInfo(
    providerID: 'provider',
    modelID: 'a',
    contextTokens: 40000,
    contextLimit: 100000,
    compactionThresholdTokens: 80000,
  );

  testWidgets(
    'model switch immediately recalculates ring without a new reply',
    (tester) async {
      await tester.pumpWidget(buildContextUsageTestSurface(usage, modelA));
      expect(find.text('40'), findsOneWidget);
      expect(
        tester
            .widget<CircularProgressIndicator>(
              find.byType(CircularProgressIndicator),
            )
            .value,
        .4,
      );
      await tester.pumpWidget(buildContextUsageTestSurface(usage, modelB));
      expect(find.text('20'), findsOneWidget);
      expect(
        tester
            .widget<CircularProgressIndicator>(
              find.byType(CircularProgressIndicator),
            )
            .value,
        .2,
      );
      final projected = ContextUsageInfo.withModelLimit(usage, 200000);
      expect(projected!.knownContextTokens, 40000);
      expect(projected.compactionThresholdTokens, isNull);
      expect(usage.contextLimit, 100000);
    },
  );

  testWidgets('unknown usage is static and a measured zero displays zero', (
    tester,
  ) async {
    await tester.pumpWidget(buildContextUsageTestSurface(null, modelA));
    expect(find.text('--'), findsOneWidget);
    expect(
      tester
          .widget<CircularProgressIndicator>(
            find.byType(CircularProgressIndicator),
          )
          .value,
      0,
    );
    await tester.pump(const Duration(seconds: 1));
    expect(tester.binding.hasScheduledFrame, isFalse);
    await tester.pumpWidget(
      buildContextUsageTestSurface(
        const ContextUsageInfo(contextTokens: 0),
        modelA,
      ),
    );
    expect(find.text('0'), findsOneWidget);
    expect(
      tester
          .widget<CircularProgressIndicator>(
            find.byType(CircularProgressIndicator),
          )
          .value,
      0,
    );
  });

  test('older archived usage never overwrites a newer measurement', () {
    final current = usage.copyWith(updatedAt: DateTime.utc(2026, 9, 20));
    final old = usage.copyWith(
      contextTokens: 2000,
      updatedAt: DateTime.utc(2026, 9, 19),
    );
    expect(current.merge(old).knownContextTokens, 40000);
    expect(
      current
          .merge(current.copyWith(compactionCountTokens: 0))
          .knownContextTokens,
      0,
    );
    final sameWindowOtherModel = ContextUsageInfo.withModelLimit(
      usage,
      100000,
      selectedProviderID: 'provider',
      selectedModelID: 'b',
    );
    expect(sameWindowOtherModel!.compactionThresholdTokens, isNull);
  });
}
