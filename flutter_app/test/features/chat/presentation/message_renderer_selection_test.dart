import 'package:chat_codex_app/core/theme/app_colors.dart';
import 'package:chat_codex_app/features/chat/presentation/message_renderer.dart';
import 'package:chat_codex_app/features/settings/settings_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets(
    'markdown message uses one SelectionArea and no nested SelectableText in code',
    (tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: MessageRenderer(
              content: 'before\n\n```dart\nprint(1);\n```\n\nafter',
              isUser: false,
              settings: AppSettings(
                mermaidRender: false,
                htmlPreview: false,
                latexRender: false,
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byType(SelectionArea), findsOneWidget);
      final selectionStyle = tester.widget<DefaultSelectionStyle>(
        find
            .ancestor(
              of: find.byType(SelectionArea),
              matching: find.byType(DefaultSelectionStyle),
            )
            .first,
      );
      expect(selectionStyle.selectionColor, AppColors.textSelection);
      final markdown = tester.widget<MarkdownBody>(find.byType(MarkdownBody));
      expect(markdown.selectable, isFalse);
      expect(find.byType(SelectableText), findsNothing);
      expect(find.text('before'), findsOneWidget);
      expect(find.text('print(1);'), findsOneWidget);
      expect(find.text('after'), findsOneWidget);
    },
  );
}
