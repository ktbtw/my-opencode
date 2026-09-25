import 'package:chat_codex_app/features/chat/data/attachment_upload.dart';
import 'package:chat_codex_app/features/chat/presentation/code_syntax_highlight.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _base = TextStyle(fontSize: 13);

List<TextSpan> _childrenOf(TextSpan span) =>
    (span.children ?? const <InlineSpan>[]).whereType<TextSpan>().toList();

void main() {
  test('codeLanguageFromFilename maps common attachments', () {
    expect(codeLanguageFromFilename('data.json'), CodeLanguage.json);
    expect(codeLanguageFromFilename('data.jsonl'), CodeLanguage.json);
    expect(codeLanguageFromFilename('README.md'), CodeLanguage.markdown);
    expect(codeLanguageFromFilename('conf.yml'), CodeLanguage.yaml);
    expect(codeLanguageFromFilename('page.html'), CodeLanguage.xml);
    expect(codeLanguageFromFilename('server.log'), CodeLanguage.log);
    expect(codeLanguageFromFilename('main.dart'), CodeLanguage.code);
    expect(codeLanguageFromFilename('run.sh'), CodeLanguage.script);
    expect(codeLanguageFromFilename('notes.txt'), CodeLanguage.plain);
    expect(codeLanguageFromFilename('no-extension'), CodeLanguage.plain);
  });

  test('json highlighting colors keys and strings differently', () {
    final span = highlightCodeText(
      '{"name": "value", "count": 12}',
      CodeLanguage.json,
      _base,
    );
    final children = _childrenOf(span);
    final keySpan = children.firstWhere((item) => item.text == '"name"');
    final stringSpan = children.firstWhere((item) => item.text == '"value"');
    final numberSpan = children.firstWhere((item) => item.text == '12');
    expect(keySpan.style?.fontWeight, FontWeight.w600);
    expect(keySpan.style?.color, isNot(stringSpan.style?.color));
    expect(numberSpan.style?.color, isNot(keySpan.style?.color));
    expect(children.map((item) => item.text).join(), '{"name": "value", "count": 12}');
  });

  test('plain text attaches no highlight children', () {
    final span = highlightCodeText('just words', CodeLanguage.plain, _base);
    expect(span.text, 'just words');
    expect(span.children, isNull);
  });

  test('very large text falls back to plain span', () {
    final text = 'a' * (200 * 1024 + 1);
    final span = highlightCodeText(text, CodeLanguage.json, _base);
    expect(span.text, text);
    expect(span.children, isNull);
  });

  test('script language highlights hash comments', () {
    final span = highlightCodeText(
      '#!/bin/sh\necho "hi"\n',
      CodeLanguage.script,
      _base,
    );
    final children = _childrenOf(span);
    final comment = children.firstWhere((item) => item.text == '#!/bin/sh');
    expect(comment.style?.fontStyle, FontStyle.italic);
  });

  test('isTextAttachment splits text from binary and images', () {
    expect(isTextAttachment('notes.txt', 'text/plain'), isTrue);
    expect(isTextAttachment('data.json', 'application/json'), isTrue);
    expect(isTextAttachment('legacy.txt', 'application/octet-stream'), isTrue);
    expect(isTextAttachment('shot.png', 'image/png'), isFalse);
    expect(isTextAttachment('doc.pdf', 'application/pdf'), isFalse);
    expect(isTextAttachment('archive.zip', 'application/zip'), isFalse);
  });
}
