// 非 Web 平台渲染实现（使用 webview_flutter）
import 'package:flutter/material.dart';
import 'package:webview_flutter/webview_flutter.dart';
import '../../../core/theme/app_colors.dart';

void openUrl(String url) {
  // TODO: 可以集成 url_launcher 打开外部链接
}

Widget buildMermaid(String code) => _MermaidBlockNative(code: code);

Widget buildHtmlPreview(String htmlContent) =>
    _HtmlPreviewBlockNative(htmlContent: htmlContent);

// ─── Mermaid 原生 WebView 实现 ───────────────────────────────
class _MermaidBlockNative extends StatefulWidget {
  final String code;
  const _MermaidBlockNative({required this.code});

  @override
  State<_MermaidBlockNative> createState() => _MermaidBlockNativeState();
}

class _MermaidBlockNativeState extends State<_MermaidBlockNative> {
  late final WebViewController _controller;
  double _height = 200;

  @override
  void initState() {
    super.initState();
    final escaped = widget.code
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;');

    final html = '''<!DOCTYPE html>
<html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
  body{margin:0;padding:8px;background:#f8fafd;display:flex;justify-content:center;}
  .mermaid{max-width:100%;}
</style>
<script src="https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js"></script>
</head><body>
<div class="mermaid">$escaped</div>
<script>
mermaid.initialize({startOnLoad:true,theme:'default'});
window.addEventListener('load',function(){
  setTimeout(function(){
    MermaidHeight.postMessage(String(document.body.scrollHeight));
  },1000);
});
</script></body></html>''';

    _controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setBackgroundColor(const Color(0xFFF8FAFD))
      ..addJavaScriptChannel(
        'MermaidHeight',
        onMessageReceived: (msg) {
          final h = double.tryParse(msg.message);
          if (h != null && h > 50 && mounted) {
            setState(() => _height = h + 24);
          }
        },
      )
      ..loadHtmlString(html);
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.symmetric(vertical: 8),
      height: _height,
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: AppColors.border),
      ),
      clipBehavior: Clip.antiAlias,
      child: WebViewWidget(controller: _controller),
    );
  }
}

// ─── HTML Preview 原生 WebView 实现 ──────────────────────────
class _HtmlPreviewBlockNative extends StatefulWidget {
  final String htmlContent;
  const _HtmlPreviewBlockNative({required this.htmlContent});

  @override
  State<_HtmlPreviewBlockNative> createState() =>
      _HtmlPreviewBlockNativeState();
}

class _HtmlPreviewBlockNativeState extends State<_HtmlPreviewBlockNative> {
  late final WebViewController _controller;
  bool _showPreview = true;
  double _height = 400;

  @override
  void initState() {
    super.initState();
    _controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..addJavaScriptChannel(
        'PageHeight',
        onMessageReceived: (msg) {
          final h = double.tryParse(msg.message);
          if (h != null && h > 50 && mounted) {
            setState(() => _height = h.clamp(100, 600));
          }
        },
      )
      ..loadHtmlString(_wrapHtml(widget.htmlContent));
  }

  String _wrapHtml(String content) {
    // 如果已有完整 html 结构，直接注入高度上报脚本
    final script = '''<script>
window.addEventListener('load',function(){
  setTimeout(function(){
    PageHeight.postMessage(String(document.body.scrollHeight));
  },500);
});
</script>''';
    if (content.contains('</body>')) {
      return content.replaceFirst('</body>', '$script</body>');
    }
    return '$content$script';
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.symmetric(vertical: 8),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: AppColors.border),
      ),
      clipBehavior: Clip.antiAlias,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
            color: const Color(0xFFF8FAFD),
            child: Row(
              children: [
                const Icon(Icons.web_outlined,
                    size: 13, color: AppColors.textMuted),
                const SizedBox(width: 6),
                const Text('HTML 预览',
                    style: TextStyle(fontSize: 11, color: AppColors.textMuted)),
                const Spacer(),
                GestureDetector(
                  onTap: () => setState(() => _showPreview = !_showPreview),
                  child: Text(
                    _showPreview ? '收起' : '展开预览',
                    style: const TextStyle(
                        fontSize: 11, color: AppColors.primary),
                  ),
                ),
              ],
            ),
          ),
          const Divider(height: 1, color: AppColors.border),
          if (_showPreview)
            SizedBox(
              height: _height,
              child: WebViewWidget(controller: _controller),
            ),
        ],
      ),
    );
  }
}
