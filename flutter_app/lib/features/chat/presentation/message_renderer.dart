import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:flutter_math_fork/flutter_math.dart';
import 'package:markdown/markdown.dart' as md;
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../features/settings/settings_provider.dart';

// Web 平台条件导入（Mermaid/HTML iframe 渲染）
import 'renderer_web.dart'
    if (dart.library.io) 'renderer_stub.dart'
    as renderer_platform;

// ─── 顶层入口：根据设置决定渲染方式 ──────────────────────────────
class MessageRenderer extends StatefulWidget {
  /// Parsing a very large reply into one Markdown syntax tree can allocate
  /// several orders of magnitude more memory than the source text. The full
  /// source remains visible; only its presentation falls back to plain text.
  static const largeBodyMarkdownThreshold = 64 * 1024;

  final String content;
  final bool isUser;
  final AppSettings settings;

  const MessageRenderer({
    super.key,
    required this.content,
    required this.isUser,
    required this.settings,
  });

  @override
  State<MessageRenderer> createState() => _MessageRendererState();
}

class _MessageRendererState extends State<MessageRenderer> {
  Widget? _rendered;

  @override
  void didUpdateWidget(covariant MessageRenderer oldWidget) {
    super.didUpdateWidget(oldWidget);
    final before = oldWidget.settings;
    final after = widget.settings;
    if (oldWidget.content != widget.content ||
        oldWidget.isUser != widget.isUser ||
        before.markdownRender != after.markdownRender ||
        before.latexRender != after.latexRender ||
        before.mermaidRender != after.mermaidRender ||
        before.htmlPreview != after.htmlPreview) {
      _rendered = null;
    }
  }

  @override
  Widget build(BuildContext context) => _rendered ??= _buildContent();

  Widget _buildContent() {
    final content = widget.content;
    final settings = widget.settings;
    final isUser = widget.isUser;

    if (content.length > MessageRenderer.largeBodyMarkdownThreshold) {
      return _LargeBodyRenderer(content: content, isUser: isUser);
    }
    if (isUser || !settings.markdownRender) {
      return DefaultSelectionStyle(
        selectionColor: AppColors.textSelection,
        child: SelectableText(
          content,
          style: TextStyle(
            fontSize: 14,
            height: 1.55,
            color: isUser ? Colors.white : AppColors.textPrimary,
          ),
        ),
      );
    }
    // DefaultSelectionStyle must wrap SelectionArea so Text in code fences
    // inherits the same blue highlight as body copy.
    return DefaultSelectionStyle(
      selectionColor: AppColors.textSelection,
      child: SelectionArea(
        child: _MarkdownRenderer(content: content, settings: settings),
      ),
    );
  }
}

/// A page based viewer keeps every character available without asking the
/// engine to shape a million character paragraph in one frame.
class _LargeBodyRenderer extends StatefulWidget {
  final String content;
  final bool isUser;

  const _LargeBodyRenderer({required this.content, required this.isUser});

  @override
  State<_LargeBodyRenderer> createState() => _LargeBodyRendererState();
}

class _LargeBodyRendererState extends State<_LargeBodyRenderer> {
  static const _pageChars = 32 * 1024;
  late List<String> _pages;
  var _page = 0;

  @override
  void initState() {
    super.initState();
    _pages = _split(widget.content);
  }

  @override
  void didUpdateWidget(covariant _LargeBodyRenderer oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.content != widget.content) {
      _pages = _split(widget.content);
      _page = 0;
    }
  }

  List<String> _split(String value) {
    if (value.isEmpty) return const [''];
    final pages = <String>[];
    var start = 0;
    while (start < value.length) {
      var end = (start + _pageChars).clamp(0, value.length) as int;
      if (end < value.length) {
        final lineBreak = value.lastIndexOf('\n', end);
        if (lineBreak > start) end = lineBreak + 1;
      }
      pages.add(value.substring(start, end));
      start = end;
    }
    return pages;
  }

  @override
  Widget build(BuildContext context) {
    final textColor = widget.isUser ? Colors.white : AppColors.textPrimary;
    final pageCount = _pages.length;
    return DefaultSelectionStyle(
      selectionColor: AppColors.textSelection,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SelectableText(
            _pages[_page],
            style: TextStyle(fontSize: 14, height: 1.55, color: textColor),
          ),
          if (pageCount > 1) ...[
            const SizedBox(height: 8),
            Row(
              children: [
                Text(
                  '正文分片 ${_page + 1} / $pageCount',
                  style: const TextStyle(
                    fontSize: 11,
                    color: AppColors.textMuted,
                  ),
                ),
                const Spacer(),
                IconButton(
                  tooltip: '上一段',
                  onPressed: _page == 0
                      ? null
                      : () => setState(() => _page -= 1),
                  icon: const Icon(Icons.chevron_left_rounded, size: 20),
                  visualDensity: VisualDensity.compact,
                ),
                IconButton(
                  tooltip: '下一段',
                  onPressed: _page + 1 >= pageCount
                      ? null
                      : () => setState(() => _page += 1),
                  icon: const Icon(Icons.chevron_right_rounded, size: 20),
                  visualDensity: VisualDensity.compact,
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

// ─── Markdown 渲染器 ─────────────────────────────────────────────
class _MarkdownRenderer extends StatelessWidget {
  static final _narrowStyle = _buildStyleSheet(isNarrow: true);
  static final _wideStyle = _buildStyleSheet(isNarrow: false);
  final String content;
  final AppSettings settings;

  const _MarkdownRenderer({required this.content, required this.settings});

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final isNarrow =
            constraints.maxWidth.isFinite &&
            constraints.maxWidth < AppBreakpoints.sm;

        return MarkdownBody(
          key: ValueKey((
            settings.latexRender,
            settings.mermaidRender,
            settings.htmlPreview,
          )),
          data: content,
          selectable: false,
          extensionSet: md.ExtensionSet.gitHubFlavored,
          styleSheet: isNarrow ? _narrowStyle : _wideStyle,
          builders: {'code': _CodeBlockBuilder(settings: settings)},
          onTapLink: (text, href, title) {
            if (href != null) renderer_platform.openUrl(href);
          },
        );
      },
    );
  }

  static MarkdownStyleSheet _buildStyleSheet({required bool isNarrow}) {
    return MarkdownStyleSheet(
      p: const TextStyle(
        fontSize: 14,
        height: 1.6,
        color: AppColors.textPrimary,
      ),
      h1: const TextStyle(
        fontSize: 20,
        fontWeight: FontWeight.w700,
        color: AppColors.textPrimary,
      ),
      h2: const TextStyle(
        fontSize: 18,
        fontWeight: FontWeight.w600,
        color: AppColors.textPrimary,
      ),
      h3: const TextStyle(
        fontSize: 16,
        fontWeight: FontWeight.w600,
        color: AppColors.textPrimary,
      ),
      code: const TextStyle(
        fontFamily: 'monospace',
        fontSize: 13,
        color: AppColors.codeText,
      ),
      codeblockDecoration: BoxDecoration(
        color: AppColors.codeBackground,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: AppColors.border),
      ),
      blockquoteDecoration: BoxDecoration(
        border: const Border(
          left: BorderSide(color: AppColors.primary, width: 3),
        ),
        color: AppColors.primaryLight,
        borderRadius: BorderRadius.circular(4),
      ),
      blockquote: const TextStyle(
        fontSize: 14,
        color: AppColors.textSecondary,
        fontStyle: FontStyle.italic,
      ),
      tableHead: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13),
      tableBody: const TextStyle(fontSize: 13),
      tableBorder: TableBorder.all(color: AppColors.border),
      tableColumnWidth: isNarrow ? const IntrinsicColumnWidth() : null,
      tableCellsPadding: isNarrow
          ? const EdgeInsets.symmetric(horizontal: 10, vertical: 6)
          : null,
      tablePadding: isNarrow ? const EdgeInsets.only(bottom: 10) : null,
      tableScrollbarThumbVisibility: isNarrow ? true : null,
    );
  }
}

// ─── 代码块构建器：处理 mermaid / html / latex ──────────────────
class _CodeBlockBuilder extends MarkdownElementBuilder {
  final AppSettings settings;
  _CodeBlockBuilder({required this.settings});

  @override
  Widget? visitElementAfterWithContext(
    BuildContext context,
    md.Element element,
    TextStyle? preferredStyle,
    TextStyle? parentStyle,
  ) {
    final lang =
        element.attributes['class']?.replaceFirst('language-', '') ?? '';
    final raw = element.textContent;
    final code = raw.trim();
    final isFenced = lang.isNotEmpty || raw.contains('\n');

    if (!isFenced) {
      return Container(
        padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 1),
        decoration: BoxDecoration(
          color: AppColors.codeInlineBackground,
          borderRadius: BorderRadius.circular(4),
        ),
        child: Text(
          code,
          style: const TextStyle(
            fontFamily: 'monospace',
            fontSize: 13,
            height: 1.5,
            color: AppColors.codeText,
            backgroundColor: Colors.transparent,
          ),
        ),
      );
    }

    // Mermaid
    if (lang == 'mermaid' && settings.mermaidRender) {
      return _MermaidBlock(code: code);
    }

    // HTML 预览
    if ((lang == 'html' || lang == 'htm') &&
        settings.htmlPreview &&
        _isRichHtml(code)) {
      return _HtmlPreviewBlock(htmlContent: code);
    }

    // LaTeX 块
    if ((lang == 'latex' || lang == 'math') && settings.latexRender) {
      return _LatexBlock(code: code);
    }

    // 默认：自定义代码块（带复制按钮）
    return _CodeBlock(code: code, lang: lang);
  }

  bool _isRichHtml(String code) {
    final l = code.toLowerCase();
    return l.contains('<html') ||
        l.contains('<body') ||
        l.contains('<style') ||
        l.contains('<script') ||
        l.contains('tailwind') ||
        (l.contains('<div') && l.contains('<!doctype'));
  }
}

// ─── 普通代码块（带复制按钮）────────────────────────────────────
class _CodeBlock extends StatefulWidget {
  final String code;
  final String lang;
  const _CodeBlock({required this.code, required this.lang});

  @override
  State<_CodeBlock> createState() => _CodeBlockState();
}

class _CodeBlockState extends State<_CodeBlock> {
  bool _copied = false;

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.symmetric(vertical: 6),
      decoration: BoxDecoration(
        color: AppColors.codeBackground,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: AppColors.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SelectionContainer.disabled(
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
              decoration: const BoxDecoration(
                border: Border(bottom: BorderSide(color: AppColors.border)),
              ),
              child: Row(
                children: [
                  if (widget.lang.isNotEmpty)
                    Text(
                      widget.lang,
                      style: const TextStyle(
                        fontSize: 11,
                        color: AppColors.textMuted,
                        fontFamily: 'monospace',
                      ),
                    ),
                  const Spacer(),
                  GestureDetector(
                    onTap: _copy,
                    child: Row(
                      children: [
                        Icon(
                          _copied ? Icons.check : Icons.copy_outlined,
                          size: 13,
                          color: _copied
                              ? AppColors.statusSuccess
                              : AppColors.textMuted,
                        ),
                        const SizedBox(width: 4),
                        Text(
                          _copied ? '已复制' : '复制',
                          style: const TextStyle(
                            fontSize: 11,
                            color: AppColors.textMuted,
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.all(12),
            child: SizedBox(
              width: double.infinity,
              child: Text(
                widget.code,
                style: const TextStyle(
                  fontFamily: 'monospace',
                  fontSize: 13,
                  height: 1.5,
                  color: AppColors.codeText,
                  backgroundColor: Colors.transparent,
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _copy() async {
    await Clipboard.setData(ClipboardData(text: widget.code));
    setState(() => _copied = true);
    await Future.delayed(const Duration(seconds: 2));
    if (mounted) setState(() => _copied = false);
  }
}

// ─── LaTeX 块渲染 ────────────────────────────────────────────────
class _LatexBlock extends StatelessWidget {
  final String code;
  const _LatexBlock({required this.code});

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.symmetric(vertical: 8),
      padding: const EdgeInsets.all(12),
      width: double.infinity,
      decoration: BoxDecoration(
        color: AppColors.codeBackground,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: AppColors.border),
      ),
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Math.tex(
          code,
          textStyle: const TextStyle(
            fontSize: 16,
            color: AppColors.textPrimary,
          ),
          onErrorFallback: (e) => SelectableText(
            code,
            style: const TextStyle(fontFamily: 'monospace', fontSize: 13),
          ),
        ),
      ),
    );
  }
}

// ─── Mermaid 图表（Web 专属 iframe，非 Web 降级显示代码）────────
class _MermaidBlock extends StatelessWidget {
  final String code;
  const _MermaidBlock({required this.code});

  @override
  Widget build(BuildContext context) {
    return renderer_platform.buildMermaid(code);
  }
}

// ─── HTML 预览（Web 专属 sandboxed iframe，非 Web 降级显示代码）─
class _HtmlPreviewBlock extends StatelessWidget {
  final String htmlContent;
  const _HtmlPreviewBlock({required this.htmlContent});

  @override
  Widget build(BuildContext context) {
    return renderer_platform.buildHtmlPreview(htmlContent);
  }
}

// ─── LaTeX 行内渲染（用于消息统计展示等独立用途）───────────────
class InlineLatexRenderer extends StatelessWidget {
  final String tex;
  const InlineLatexRenderer({super.key, required this.tex});

  @override
  Widget build(BuildContext context) {
    return Math.tex(
      tex,
      textStyle: const TextStyle(fontSize: 14, color: AppColors.textPrimary),
      onErrorFallback: (e) => Text(
        '\$$tex\$',
        style: const TextStyle(fontFamily: 'monospace', fontSize: 13),
      ),
    );
  }
}
