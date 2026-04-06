// Web 平台渲染实现（使用 dart:html、dart:ui_web）
// ignore: avoid_web_libraries_in_flutter
import 'dart:html' as html_lib;
import 'dart:ui_web' as ui;
import 'package:flutter/material.dart';
import '../../../core/theme/app_colors.dart';

void openUrl(String url) {
  html_lib.window.open(url, '_blank');
}

Widget buildMermaid(String code) => _MermaidBlockWeb(code: code);

Widget buildHtmlPreview(String htmlContent) => _HtmlPreviewBlockWeb(htmlContent: htmlContent);

// ─── Mermaid Web 实现 ─────────────────────────────────────────
class _MermaidBlockWeb extends StatefulWidget {
  final String code;
  const _MermaidBlockWeb({required this.code});

  @override
  State<_MermaidBlockWeb> createState() => _MermaidBlockWebState();
}

class _MermaidBlockWebState extends State<_MermaidBlockWeb> {
  late final String _viewId;
  double _height = 200;

  @override
  void initState() {
    super.initState();
    _viewId = 'mermaid-${DateTime.now().millisecondsSinceEpoch}-${widget.code.hashCode.abs()}';
    _registerView();
  }

  void _registerView() {
    final escaped = widget.code
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;');
    final htmlContent = '''<!DOCTYPE html>
<html><head><meta charset="utf-8">
<style>body{margin:0;padding:8px;background:#f8fafd;display:flex;justify-content:center;}
.mermaid{max-width:100%;}</style>
<script src="https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js"></script>
</head><body>
<div class="mermaid">$escaped</div>
<script>
mermaid.initialize({startOnLoad:true,theme:'default'});
window.addEventListener('load',function(){
  setTimeout(function(){
    parent.postMessage({type:'mermaid-height',id:'$_viewId',height:document.body.scrollHeight},'*');
  },800);
});
</script></body></html>''';
    final blob = html_lib.Blob([htmlContent], 'text/html');
    final url = html_lib.Url.createObjectUrl(blob);
    ui.platformViewRegistry.registerViewFactory(_viewId, (int id) {
      return html_lib.IFrameElement()
        ..src = url
        ..style.border = 'none'
        ..style.width = '100%'
        ..style.height = '100%';
    });
    html_lib.window.addEventListener('message', (event) {
      final e = event as html_lib.MessageEvent;
      if (e.data is! Map) return;
      final data = e.data as Map;
      if (data['type'] == 'mermaid-height' && data['id'] == _viewId) {
        final h = (data['height'] as num?)?.toDouble();
        if (h != null && h > 50 && mounted) {
          setState(() => _height = h + 20);
        }
      }
    });
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
      child: HtmlElementView(viewType: _viewId),
    );
  }
}

// ─── HTML Preview Web 实现 ────────────────────────────────────
class _HtmlPreviewBlockWeb extends StatefulWidget {
  final String htmlContent;
  const _HtmlPreviewBlockWeb({required this.htmlContent});

  @override
  State<_HtmlPreviewBlockWeb> createState() => _HtmlPreviewBlockWebState();
}

class _HtmlPreviewBlockWebState extends State<_HtmlPreviewBlockWeb> {
  bool _showPreview = true;
  late final String _viewId;

  @override
  void initState() {
    super.initState();
    _viewId = 'html-preview-${DateTime.now().millisecondsSinceEpoch}-${widget.htmlContent.hashCode.abs()}';
    _registerView();
  }

  void _registerView() {
    final blob = html_lib.Blob([widget.htmlContent], 'text/html');
    final url = html_lib.Url.createObjectUrl(blob);
    ui.platformViewRegistry.registerViewFactory(_viewId, (int id) {
      return html_lib.IFrameElement()
        ..src = url
        ..sandbox!.add('allow-scripts')
        ..style.border = 'none'
        ..style.width = '100%'
        ..style.height = '100%';
    });
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
                const Icon(Icons.web_outlined, size: 13, color: AppColors.textMuted),
                const SizedBox(width: 6),
                const Text('HTML 预览', style: TextStyle(fontSize: 11, color: AppColors.textMuted)),
                const Spacer(),
                GestureDetector(
                  onTap: () => setState(() => _showPreview = !_showPreview),
                  child: Text(
                    _showPreview ? '收起' : '展开预览',
                    style: const TextStyle(fontSize: 11, color: AppColors.primary),
                  ),
                ),
              ],
            ),
          ),
          const Divider(height: 1, color: AppColors.border),
          if (_showPreview)
            SizedBox(
              height: 400,
              child: HtmlElementView(viewType: _viewId),
            ),
        ],
      ),
    );
  }
}
