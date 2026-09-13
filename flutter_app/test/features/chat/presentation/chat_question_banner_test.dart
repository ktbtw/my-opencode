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
