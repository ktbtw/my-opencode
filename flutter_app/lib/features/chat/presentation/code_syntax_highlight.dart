import 'package:flutter/material.dart';

/// 附件查看页使用的语法高亮语言。
enum CodeLanguage { plain, json, markdown, yaml, xml, log, code, script }

CodeLanguage codeLanguageFromFilename(String filename) {
  final dot = filename.lastIndexOf('.');
  final extension = dot <= 0 || dot >= filename.length - 1
      ? ''
      : filename.substring(dot + 1).toLowerCase();
  return switch (extension) {
    'json' || 'jsonl' || 'ndjson' => CodeLanguage.json,
    'md' || 'markdown' || 'rst' => CodeLanguage.markdown,
    'yaml' ||
    'yml' ||
    'toml' ||
    'ini' ||
    'cfg' ||
    'conf' ||
    'properties' ||
    'env' => CodeLanguage.yaml,
    'xml' || 'html' || 'htm' || 'svg' || 'vue' || 'svelte' => CodeLanguage.xml,
    'log' => CodeLanguage.log,
    'py' ||
    'rb' ||
    'sh' ||
    'bash' ||
    'zsh' ||
    'ps1' ||
    'pl' ||
    'r' ||
    'mk' ||
    'makefile' ||
    'dockerfile' => CodeLanguage.script,
    'txt' ||
    'text' ||
    'csv' ||
    'tsv' ||
    'ini2' ||
    'srt' ||
    'vtt' ||
    '' => CodeLanguage.plain,
    _ => CodeLanguage.code,
  };
}

/// 超过这个长度不再高亮，按纯文本渲染，避免打开大文件时卡顿。
const _maxHighlightChars = 200 * 1024;

class _Styles {
  static const key = TextStyle(
    color: Color(0xFF2563EB),
    fontWeight: FontWeight.w600,
  );
  static const string = TextStyle(color: Color(0xFF0F766E));
  static const number = TextStyle(color: Color(0xFFB45309));
  static const constant = TextStyle(color: Color(0xFF7C3AED));
  static const comment = TextStyle(
    color: Color(0xFF8FA3BF),
    fontStyle: FontStyle.italic,
  );
  static const keyword = TextStyle(
    color: Color(0xFFBE185D),
    fontWeight: FontWeight.w600,
  );
  static const tagName = TextStyle(
    color: Color(0xFF2563EB),
    fontWeight: FontWeight.w600,
  );
  static const attribute = TextStyle(color: Color(0xFFB45309));
  static const punct = TextStyle(color: Color(0xFF64748B));
  static const heading = TextStyle(
    color: Color(0xFF1D4ED8),
    fontWeight: FontWeight.w700,
  );
  static const link = TextStyle(
    color: Color(0xFF0F766E),
    decoration: TextDecoration.underline,
  );
  static const marker = TextStyle(
    color: Color(0xFFBE185D),
    fontWeight: FontWeight.w700,
  );
  static const code = TextStyle(
    color: Color(0xFF0F766E),
    backgroundColor: Color(0xFFE8EDF3),
  );
  static const error = TextStyle(
    color: Color(0xFFDC2626),
    fontWeight: FontWeight.w700,
  );
  static const warn = TextStyle(
    color: Color(0xFFD97706),
    fontWeight: FontWeight.w600,
  );
  static const info = TextStyle(color: Color(0xFF2563EB));
}

class _Rule {
  final RegExp pattern;
  final TextStyle style;
  const _Rule(this.pattern, this.style);
}

const _keywordList =
    'abstract|as|assert|async|await|break|case|catch|class|const|continue|'
    'default|defer|delete|do|done|elif|else|elif|enum|export|extends|factory|'
    'final|finally|for|from|func|function|go|goto|if|implements|import|in|'
    'instanceof|interface|is|lambda|let|match|mod|new|not|or|package|pass|'
    'private|protected|public|raise|range|readonly|return|select|static|struct|'
    'super|switch|this|throw|try|type|typeof|use|var|void|when|where|while|'
    'with|yield|and|then|end|fn|impl|let|mut|pub|use|val|when';

final _rulesByLanguage = <CodeLanguage, List<_Rule>>{
  CodeLanguage.json: [
    _Rule(RegExp(r'"(?:[^"\\\n]|\\.)*"(?=\s*:)'), _Styles.key),
    _Rule(RegExp(r'"(?:[^"\\\n]|\\.)*"'), _Styles.string),
    _Rule(RegExp(r'-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?'), _Styles.number),
    _Rule(RegExp(r'\b(?:true|false|null)\b'), _Styles.constant),
    _Rule(RegExp(r'[{}\[\],:]'), _Styles.punct),
  ],
  CodeLanguage.yaml: [
    _Rule(RegExp(r'#[^\n]*'), _Styles.comment),
    _Rule(RegExp(r'"(?:[^"\\\n]|\\.)*"'), _Styles.string),
    _Rule(RegExp(r"'[^'\n]*'"), _Styles.string),
    _Rule(
      RegExp(r'^[ \t]*(?:-\s+)?[A-Za-z0-9_.\-/]+(?=\s*[:=])', multiLine: true),
      _Styles.key,
    ),
    _Rule(RegExp(r'-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?'), _Styles.number),
    _Rule(
      RegExp(r'\b(?:true|false|null|yes|no|on|off)\b', caseSensitive: false),
      _Styles.constant,
    ),
    _Rule(RegExp(r'[:\-=]'), _Styles.punct),
  ],
  CodeLanguage.xml: [
    _Rule(RegExp(r'<!--[\s\S]*?-->'), _Styles.comment),
    _Rule(RegExp(r'<!\[CDATA\[[\s\S]*?\]\]>'), _Styles.string),
    _Rule(RegExp(r'</?[A-Za-z][\w:.-]*'), _Styles.tagName),
    _Rule(RegExp(r'/?>'), _Styles.punct),
    _Rule(RegExp(r'"[^"\n]*"'), _Styles.string),
    _Rule(RegExp(r"'[^'\n]*'"), _Styles.string),
    _Rule(RegExp(r'[A-Za-z_:][\w:.-]*(?=\s*=)'), _Styles.attribute),
    _Rule(RegExp(r'&[a-zA-Z#0-9]+;'), _Styles.constant),
  ],
  CodeLanguage.markdown: [
    _Rule(RegExp(r'```[\s\S]*?(?:```|$)'), _Styles.code),
    _Rule(RegExp(r'`[^`\n]+`'), _Styles.code),
    _Rule(RegExp(r'^#{1,6}[^\n]*', multiLine: true), _Styles.heading),
    _Rule(RegExp(r'\*\*[^*\n]+\*\*'), _Styles.heading),
    _Rule(RegExp(r'\[[^\]\n]*\]\([^)\n]*\)'), _Styles.link),
    _Rule(RegExp(r'^>[^\n]*', multiLine: true), _Styles.comment),
    _Rule(
      RegExp(r'^[ \t]*(?:[-*+]|\d+\.)[ \t]', multiLine: true),
      _Styles.marker,
    ),
    _Rule(RegExp(r'\*[^*\n]+\*|_[^_\n]+_'), _Styles.comment),
  ],
  CodeLanguage.log: [
    _Rule(RegExp(r'"[^"\n]*"'), _Styles.string),
    _Rule(RegExp(r'\b(?:ERROR|FATAL|SEVERE|CRITICAL|错误)\b'), _Styles.error),
    _Rule(RegExp(r'\b(?:WARN|WARNING|警告)\b'), _Styles.warn),
    _Rule(RegExp(r'\b(?:INFO|NOTICE)\b'), _Styles.info),
    _Rule(RegExp(r'\b(?:DEBUG|TRACE|VERBOSE)\b'), _Styles.comment),
    _Rule(
      RegExp(r'\b\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?\b'),
      _Styles.comment,
    ),
    _Rule(RegExp(r'-?\d+(?:\.\d+)?'), _Styles.number),
  ],
};

final _slashComment = RegExp(r'//[^\n]*');
final _hashComment = RegExp(r'#[^\n]*');
final _blockComment = RegExp(r'/\*[\s\S]*?\*/');
final _doubleQuoted = RegExp(r'"(?:[^"\\\n]|\\.)*"');
final _singleQuoted = RegExp(r"'(?:[^'\\\n]|\\.)*'");
final _backtickQuoted = RegExp(r'`(?:[^`\\]|\\.)*`');
final _numberLiteral = RegExp(
  r'0[xX][0-9a-fA-F]+|\d+(?:\.\d+)?(?:[eE][+-]?\d+)?',
);
final _constantLiteral = RegExp(
  r'\b(?:true|false|null|nil|None|True|False|undefined|NaN|self)\b',
);
final _keywordLiteral = RegExp('\\b(?:$_keywordList)\\b');
final _operatorRun = RegExp(r'[{}()\[\],;:.=+\-*/%<>!&|?^~@]+');

List<_Rule> _codeRules({required bool hashComments}) => [
  _Rule(_blockComment, _Styles.comment),
  _Rule(hashComments ? _hashComment : _slashComment, _Styles.comment),
  _Rule(_doubleQuoted, _Styles.string),
  _Rule(_singleQuoted, _Styles.string),
  _Rule(_backtickQuoted, _Styles.string),
  _Rule(_numberLiteral, _Styles.number),
  _Rule(_constantLiteral, _Styles.constant),
  _Rule(_keywordLiteral, _Styles.keyword),
  _Rule(_operatorRun, _Styles.punct),
];

final _rulesCache = <CodeLanguage, List<_Rule>>{
  ..._rulesByLanguage,
  CodeLanguage.code: _codeRules(hashComments: false),
  CodeLanguage.script: _codeRules(hashComments: true),
};

List<_Rule>? _rulesFor(CodeLanguage language) {
  if (language == CodeLanguage.plain) return null;
  return _rulesCache[language];
}

/// 按语言把 [text] 切成带颜色的 span。未识别的语言直接返回纯文本。
TextSpan highlightCodeText(
  String text,
  CodeLanguage language,
  TextStyle baseStyle,
) {
  final rules = _rulesFor(language);
  if (rules == null || text.isEmpty || text.length > _maxHighlightChars) {
    return TextSpan(text: text, style: baseStyle);
  }

  final spans = <TextSpan>[];
  final plain = StringBuffer();
  void flush() {
    if (plain.isEmpty) return;
    spans.add(TextSpan(text: plain.toString()));
    plain.clear();
  }

  var index = 0;
  while (index < text.length) {
    _Rule? hit;
    Match? match;
    for (final rule in rules) {
      final candidate = rule.pattern.matchAsPrefix(text, index);
      if (candidate == null || candidate.end == index) continue;
      hit = rule;
      match = candidate;
      break;
    }
    if (hit == null || match == null) {
      plain.write(text[index]);
      index += 1;
      continue;
    }
    flush();
    spans.add(TextSpan(text: match[0], style: baseStyle.merge(hit.style)));
    index = match.end;
  }
  flush();

  return TextSpan(style: baseStyle, children: spans);
}

/// 编辑态用的高亮控制器：语义高亮叠在输入框上，不改变真实文本。
class CodeHighlightController extends TextEditingController {
  CodeHighlightController({
    super.text,
    required this.language,
    required this.baseStyle,
  });

  final CodeLanguage language;
  final TextStyle baseStyle;

  @override
  TextSpan buildTextSpan({
    required BuildContext context,
    TextStyle? style,
    required bool withComposing,
  }) {
    return highlightCodeText(value.text, language, baseStyle);
  }
}
