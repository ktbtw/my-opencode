import 'package:chat_codex_app/core/theme/app_theme.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('render question banner states', (tester) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
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
            QuestionOptionInfo(
              label: '整体重构通知模块',
              description: '重新梳理状态机，改动较大需要回归测试',
            ),
          ],
          multiple: false,
          custom: true,
        ),
        QuestionItemInfo(
          question: '改完之后怎么发布？（可多选）',
          header: '发布方式',
          options: const [
            QuestionOptionInfo(
              label: '先出 APK 真机验证',
              description: '安装到设备确认效果后再决定',
            ),
            QuestionOptionInfo(
              label: '直接提交到主分支',
              description: '走正常发布流程',
            ),
            QuestionOptionInfo(
              label: '先开一个分支备份',
              description: '保留当前工作区状态',
            ),
          ],
          multiple: true,
          custom: true,
        ),
        QuestionItemInfo(
          question: '老版本已经装到设备上，需要兼容哪些情况？',
          header: '兼容处理',
          options: const [
            QuestionOptionInfo(
              label: '已登录状态保留',
              description: '覆盖安装不清除登录与设备列表',
            ),
            QuestionOptionInfo(
              label: '本地通知快照保留',
              description: '升级后不丢失历史通知记录',
            ),
            QuestionOptionInfo(
              label: '旧快照缺少新字段',
              description: '读取旧版本数据时不报错，按默认值处理',
            ),
            QuestionOptionInfo(
              label: '多设备同步一致',
              description: '其他设备升级后收起行为保持一致',
            ),
          ],
          multiple: false,
          custom: true,
        ),
      ],
    );

    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light,
        home: Scaffold(
          backgroundColor: const Color(0xFFF2F6FC),
          body: Center(
            child: QuestionBanner(
              question: question,
              onSubmit: (_, _) {},
              onReject: (_) {},
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('这次希望改到什么程度？'), findsOneWidget);
    expect(find.byKey(const ValueKey('question-step-0')), findsOneWidget);

    // 答第一题后跳到第二题
    await tester.tap(find.text('只改最小范围'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('question-step-1')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('先出 APK 真机验证'));
    await tester.tap(find.text('直接提交到主分支'));
    await tester.pumpAndSettle();
    expect(find.text('改完之后怎么发布？（可多选）'), findsOneWidget);

    // 跳到第三题
    await tester.tap(find.byKey(const ValueKey('question-step-2')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('已登录状态保留'));
    await tester.pumpAndSettle();
    expect(find.text('老版本已经装到设备上，需要兼容哪些情况？'), findsOneWidget);
  });
}
