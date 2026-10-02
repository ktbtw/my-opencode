import 'package:chat_codex_app/core/theme/app_theme.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('question custom answers default to enabled when omitted', () {
    final item = QuestionItemInfo.fromJson({
      'question': '是否继续？',
      'header': '确认',
      'options': <Map<String, String>>[],
    });

    expect(item.custom, isTrue);
  });

  test('question custom answers can be explicitly disabled', () {
    final item = QuestionItemInfo.fromJson({
      'question': '是否继续？',
      'header': '确认',
      'options': <Map<String, String>>[],
      'custom': false,
    });

    expect(item.custom, isFalse);
  });

  testWidgets('single custom answer replaces the selected option', (
    tester,
  ) async {
    List<List<String>>? submitted;
    await tester.pumpWidget(
      _surface(
        question: _question(multiple: false),
        onSubmit: (_, answers) => submitted = answers,
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('稍后处理'));
    await tester.enterText(
      find.byKey(const ValueKey('question-custom-0')),
      '我已经完成操作',
    );
    await tester.tap(find.text('提交选择'));

    expect(submitted, [
      <String>['我已经完成操作'],
    ]);
  });

  testWidgets('multiple custom answer is submitted with selected options', (
    tester,
  ) async {
    List<List<String>>? submitted;
    await tester.pumpWidget(
      _surface(
        question: _question(multiple: true),
        onSubmit: (_, answers) => submitted = answers,
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('已完成'));
    await tester.enterText(
      find.byKey(const ValueKey('question-custom-0')),
      '附加说明',
    );
    await tester.tap(find.text('提交选择'));

    expect(submitted, [
      <String>['已完成', '附加说明'],
    ]);
  });

  testWidgets('multiple questions page horizontally instead of stacking', (
    tester,
  ) async {
    await tester.pumpWidget(
      _surface(
        question: _multiQuestion(),
        onSubmit: (_, _) {},
      ),
    );
    await tester.pumpAndSettle();

    // 第一题可见，第二题初始不可见（横向分页，不是纵向堆叠）
    expect(find.text('第一个问题？'), findsOneWidget);
    expect(find.text('第二个问题？'), findsNothing);

    // 顶部有题目定位点
    expect(find.byKey(const ValueKey('question-step-0')), findsOneWidget);
    expect(find.byKey(const ValueKey('question-step-1')), findsOneWidget);

    // 点定位点跳到第二题
    await tester.tap(find.byKey(const ValueKey('question-step-1')));
    await tester.pumpAndSettle();
    expect(find.text('第二个问题？'), findsOneWidget);
    expect(find.text('第一个问题？'), findsNothing);
  });

  testWidgets('swiping the page moves to the next question', (tester) async {
    await tester.pumpWidget(
      _surface(question: _multiQuestion(), onSubmit: (_, _) {}),
    );
    await tester.pumpAndSettle();

    expect(find.text('第一个问题？'), findsOneWidget);
    await tester.fling(
      find.byType(PageView),
      const Offset(-400, 0),
      1200,
    );
    await tester.pumpAndSettle();

    expect(find.text('第二个问题？'), findsOneWidget);
    expect(find.text('第一个问题？'), findsNothing);
  });

  testWidgets('submit stays disabled until every question is answered', (
    tester,
  ) async {
    List<List<String>>? submitted;
    await tester.pumpWidget(
      _surface(
        question: _multiQuestion(),
        onSubmit: (_, answers) => submitted = answers,
      ),
    );
    await tester.pumpAndSettle();

    // 只答第一题，第二题未答 -> 不能提交
    await tester.tap(find.text('甲'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('提交选择'));
    await tester.pumpAndSettle();
    expect(submitted, isNull);

    // 答完第二题 -> 可以提交
    await tester.tap(find.byKey(const ValueKey('question-step-1')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('丙'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('提交选择'));

    expect(submitted, [
      <String>['甲'],
      <String>['丙'],
    ]);
  });

  testWidgets('answered questions are marked in the stepper', (tester) async {
    await tester.pumpWidget(
      _surface(question: _multiQuestion(), onSubmit: (_, _) {}),
    );
    await tester.pumpAndSettle();

    Finder checkIcon() => find.descendant(
      of: find.byKey(const ValueKey('question-step-0')),
      matching: find.byIcon(Icons.check_rounded),
    );
    expect(checkIcon(), findsNothing, reason: '未作答时不应有勾');

    await tester.tap(find.text('甲'));
    await tester.pumpAndSettle();
    expect(checkIcon(), findsOneWidget, reason: '答完后应打勾');
  });
}

QuestionRequestInfo _multiQuestion() {
  return QuestionRequestInfo(
    requestId: 'question-multi',
    questions: [
      QuestionItemInfo(
        question: '第一个问题？',
        header: '第一项',
        options: const [
          QuestionOptionInfo(label: '甲', description: '选项甲'),
          QuestionOptionInfo(label: '乙', description: '选项乙'),
        ],
        multiple: false,
        custom: false,
      ),
      QuestionItemInfo(
        question: '第二个问题？',
        header: '第二项',
        options: const [
          QuestionOptionInfo(label: '丙', description: '选项丙'),
          QuestionOptionInfo(label: '丁', description: '选项丁'),
        ],
        multiple: false,
        custom: false,
      ),
    ],
  );
}

Widget _surface({
  required QuestionRequestInfo question,
  required void Function(String, List<List<String>>) onSubmit,
}) {
  return MaterialApp(
    theme: AppTheme.light,
    home: Scaffold(
      body: SingleChildScrollView(
        child: QuestionBanner(
          question: question,
          onSubmit: onSubmit,
          onReject: (_) {},
        ),
      ),
    ),
  );
}

QuestionRequestInfo _question({required bool multiple}) {
  return QuestionRequestInfo(
    requestId: 'question-1',
    questions: [
      QuestionItemInfo(
        question: '当前操作完成了吗？',
        header: '操作状态',
        options: const [
          QuestionOptionInfo(label: '已完成', description: '继续执行任务'),
          QuestionOptionInfo(label: '稍后处理', description: '暂时保持等待'),
        ],
        multiple: multiple,
        custom: true,
      ),
    ],
  );
}
