import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:flutter_math_fork/flutter_math.dart';
import 'package:markdown/markdown.dart' as md;
import '../../../core/theme/app_colors.dart';
import '../../../features/settings/settings_provider.dart';

// Web 平台条件导入（Mermaid/HTML iframe 渲染）
import 'renderer_web.dart' if (dart.library.io) 'renderer_stub.dart' as renderer_platform;

// ─── 顶层入口：根据设置决定渲染方式 ──────────────────────────────
class MessageRenderer extends StatelessWidget {
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
  Widget build(BuildContext context) {
    if (isUser || !settings.markdownRender) {
      return SelectableText(
        content,
        style: TextStyle(
          fontSize: 14,
          height: 1.55,
          color: isUser ? Colors.white : AppColors.textPrimary,
        ),
      );
    }
    return _MarkdownRenderer(content: content, settings: settings);
  }
}

// ─── Markdown 渲染器 ─────────────────────────────────────────────
class _MarkdownRenderer extends StatelessWidget {
  final String content;
  final AppSettings settings;

  const _MarkdownRenderer({required this.content, required this.settings});

  @override
  Widget build(BuildContext context) {
    return MarkdownBody(
      data: content,
      selectable: true,
      extensionSet: md.ExtensionSet.gitHubFlavored,
      styleSheet: _buildStyleSheet(),
      builders: {
        'code': _CodeBlockBuilder(settings: settings),
      },
      onTapLink: (text, href, title) {
        if (href != null) renderer_platform.openUrl(href);
      },
    );
  }

  MarkdownStyleSheet _buildStyleSheet() {
    return MarkdownStyleSheet(
      p: const TextStyle(fontSize: 14, height: 1.6, color: AppColors.textPrimary),
      h1: const TextStyle(fontSize: 20, fontWeight: FontWeight.w700, color: AppColors.textPrimary),
      h2: const TextStyle(fontSize: 18, fontWeight: FontWeight.w600, color: AppColors.textPrimary),
      h3: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: AppColors.textPrimary),
      code: const TextStyle(
        fontFamily: 'monospace',
        fontSize: 13,
        color: Color(0xFF1A56DB),
        backgroundColor: Color(0xFFEFF6FF),
      ),
      codeblockDecoration: BoxDecoration(
        color: const Color(0xFFF8FAFD),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: AppColors.border),
      ),
      blockquoteDecoration: BoxDecoration(
        border: const Border(left: BorderSide(color: AppColors.primary, width: 3)),
        color: AppColors.primaryLight,
        borderRadius: BorderRadius.circular(4),
      ),
      blockquote: const TextStyle(fontSize: 14, color: AppColors.textSecondary, fontStyle: FontStyle.italic),
      tableHead: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13),
      tableBody: const TextStyle(fontSize: 13),
      tableBorder: TableBorder.all(color: AppColors.border),
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
    // 只处理有 class 的 code 元素（即带语言标注的代码块）
    final lang = element.attributes['class']?.replaceFirst('language-', '') ?? '';
    final code = element.textContent.trim();

    if (lang.isEmpty) return null; // 行内 code，不处理

    // Mermaid
    if (lang == 'mermaid' && settings.mermaidRender) {
      return _MermaidBlock(code: code);
    }

    // HTML 预览
    if ((lang == 'html' || lang == 'htm') && settings.htmlPreview && _isRichHtml(code)) {
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
        color: const Color(0xFFF8FAFD),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: AppColors.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
            decoration: const BoxDecoration(
              border: Border(bottom: BorderSide(color: AppColors.border)),
            ),
            child: Row(
              children: [
                if (widget.lang.isNotEmpty)
                  Text(widget.lang,
                      style: const TextStyle(
                          fontSize: 11, color: AppColors.textMuted, fontFamily: 'monospace')),
                const Spacer(),
                GestureDetector(
                  onTap: _copy,
                  child: Row(
                    children: [
                      Icon(
                        _copied ? Icons.check : Icons.copy_outlined,
                        size: 13,
                        color: _copied ? AppColors.statusSuccess : AppColors.textMuted,
                      ),
                      const SizedBox(width: 4),
                      Text(
                        _copied ? '已复制' : '复制',
                        style: const TextStyle(fontSize: 11, color: AppColors.textMuted),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.all(12),
            child: SelectableText(
              widget.code,
              style: const TextStyle(
                  fontFamily: 'monospace', fontSize: 13, height: 1.5, color: Color(0xFF1E293B)),
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
        color: const Color(0xFFF8FAFD),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: AppColors.border),
      ),
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Math.tex(
          code,
          textStyle: const TextStyle(fontSize: 16, color: AppColors.textPrimary),
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
      onErrorFallback: (e) =>
          Text('\$$tex\$', style: const TextStyle(fontFamily: 'monospace', fontSize: 13)),
    );
  }
}
