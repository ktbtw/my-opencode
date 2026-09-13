import 'package:chat_codex_app/shared/widgets/step_guide_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('step guide keeps three actions in one row on mobile', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(360, 760);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            onPressed: () => showStepGuideDialog(
              context,
              title: '配置教程',
              subtitle: '完成配置流程',
              steps: const [
                GuideStep(
                  title: '第一步',
                  description: '第一步说明',
                  icon: Icons.looks_one_outlined,
                ),
                GuideStep(
                  title: '第二步',
                  description: '第二步说明',
                  icon: Icons.looks_two_outlined,
                ),
              ],
            ),
            child: const Text('打开指南'),
          ),
        ),
      ),
    );

    await tester.tap(find.text('打开指南'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('下一步'));
    await tester.pumpAndSettle();

    final later = tester.getCenter(find.text('稍后查看'));
    final previous = tester.getCenter(find.text('上一步'));
    final finish = tester.getCenter(find.text('开始操作'));
    expect((later.dy - previous.dy).abs(), lessThan(1));
    expect((previous.dy - finish.dy).abs(), lessThan(1));
    expect(later.dx, lessThan(previous.dx));
    expect(previous.dx, lessThan(finish.dx));
  });

  testWidgets('step guide advances and returns start action', (tester) async {
    bool? started;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            onPressed: () async {
              started = await showStepGuideDialog(
                context,
                title: '测试指南',
                subtitle: '测试说明',
                steps: const [
                  GuideStep(
                    title: '第一步',
                    description: '第一步说明',
                    icon: Icons.looks_one_outlined,
                  ),
                  GuideStep(
                    title: '第二步',
                    description: '第二步说明',
                    icon: Icons.looks_two_outlined,
                  ),
                ],
                finalActionLabel: '开始',
              );
            },
            child: const Text('打开指南'),
          ),
        ),
      ),
    );

    await tester.tap(find.text('打开指南'));
    await tester.pumpAndSettle();
    expect(find.text('第一步'), findsOneWidget);

    await tester.tap(find.text('下一步'));
    await tester.pumpAndSettle();
    expect(find.text('第二步'), findsOneWidget);

    await tester.tap(find.text('开始'));
    await tester.pumpAndSettle();
    expect(started, isTrue);
  });
}
