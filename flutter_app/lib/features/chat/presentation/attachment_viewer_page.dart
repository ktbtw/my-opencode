import '../../../shared/widgets/app_status_icon.dart';
import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:photo_view/photo_view.dart';

import '../../../core/theme/app_colors.dart';
import '../data/artifact_download_saver.dart';
import '../data/attachment_upload.dart';
import '../data/chat_model.dart';
import 'code_syntax_highlight.dart';

/// 附件详情页：图片可缩放查看，文本类附件可编辑并带语法高亮。
class AttachmentViewerPage extends StatefulWidget {
  final AttachedFile file;

  const AttachmentViewerPage({super.key, required this.file});

  @override
  State<AttachmentViewerPage> createState() => _AttachmentViewerPageState();
}

class _AttachmentViewerPageState extends State<AttachmentViewerPage> {
  static const _editorTextStyle = TextStyle(
    fontFamily: 'monospace',
    fontFamilyFallback: ['Menlo', 'Consolas', 'Courier New', 'monospace'],
    fontSize: 13,
    height: 1.5,
    color: AppColors.codeText,
  );

  late final CodeLanguage _language = codeLanguageFromFilename(
    widget.file.filename,
  );
  late final bool _textual = isTextAttachment(
    widget.file.filename,
    widget.file.mimeType,
  );
  CodeHighlightController? _controller;

  var _editing = false;
  var _dirty = false;
  var _saving = false;
  var _quarterTurns = 0;

  @override
  void initState() {
    super.initState();
    if (_textual) {
      _controller = CodeHighlightController(
        text: _decodedText(),
        language: _language,
        baseStyle: _editorTextStyle,
      );
    }
  }

  @override
  void dispose() {
    _controller?.dispose();
    super.dispose();
  }

  Uint8List? get _bytes {
    if (widget.file.base64Data.isEmpty) return null;
    try {
      return base64Decode(widget.file.base64Data);
    } catch (_) {
      return null;
    }
  }

  String _decodedText() {
    final bytes = _bytes;
    if (bytes == null) return '';
    return utf8.decode(bytes, allowMalformed: true);
  }

  String get _sizeText {
    final bytes = widget.file.sizeBytes;
    if (bytes <= 0) return '大小未知';
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  }

  Future<void> _save() async {
    if (_saving) return;
    final bytes = _textual && _controller != null
        ? Uint8List.fromList(utf8.encode(_controller!.text))
        : _bytes;
    if (bytes == null || bytes.isEmpty) {
      _notify('附件内容未保留，无法保存');
      return;
    }
    setState(() => _saving = true);
    try {
      final path = await saveArtifactBytes(
        bytes,
        filename: widget.file.filename,
        mimeType: widget.file.mimeType,
      );
      if (!mounted) return;
      setState(() => _dirty = false);
      _notify(path == null ? '已交给系统下载' : '已保存到 $path');
    } catch (error) {
      if (!mounted) return;
      _notify('保存失败：$error');
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _copyAll() async {
    final controller = _controller;
    if (controller == null || controller.text.isEmpty) {
      _notify('没有可复制的内容');
      return;
    }
    await Clipboard.setData(ClipboardData(text: controller.text));
    if (!mounted) return;
    _notify('已复制全部内容');
  }

  void _notify(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(
        SnackBar(
          content: Text(message),
          behavior: SnackBarBehavior.floating,
          duration: const Duration(seconds: 2),
        ),
      );
  }

  @override
  Widget build(BuildContext context) {
    final isImage = widget.file.isImage && _bytes != null;
    return Scaffold(
      backgroundColor: isImage ? Colors.black : AppColors.background,
      appBar: AppBar(
        leading: Navigator.of(context).canPop()
            ? const AppStatusBackButton()
            : null,
        backgroundColor: isImage ? Colors.black : AppColors.surface,
        foregroundColor: isImage ? Colors.white : AppColors.textPrimary,
        title: Text(
          widget.file.filename,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
        ),
        actions: _actions(isImage),
      ),
      body: isImage ? _imageBody() : _contentBody(),
    );
  }

  List<Widget> _actions(bool isImage) {
    if (isImage) {
      return [
        IconButton(
          tooltip: '旋转',
          onPressed: () =>
              setState(() => _quarterTurns = (_quarterTurns + 1) % 4),
          icon: const AppStatusIcon(Icons.rotate_right_rounded),
        ),
        IconButton(
          tooltip: '保存到本地',
          onPressed: _saving ? null : _save,
          icon: _saving
              ? const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    color: Colors.white,
                  ),
                )
              : const AppStatusIcon(Icons.download_rounded),
        ),
      ];
    }
    return [
      if (_textual)
        IconButton(
          tooltip: '复制全部',
          onPressed: _copyAll,
          icon: const AppStatusIcon(Icons.copy_all_rounded),
        ),
      if (_textual)
        IconButton(
          tooltip: _editing ? '完成编辑' : '编辑',
          onPressed: () => setState(() => _editing = !_editing),
          icon: AppStatusIcon(
            _editing ? Icons.check_rounded : Icons.edit_outlined,
          ),
        ),
      IconButton(
        tooltip: '保存到本地',
        onPressed: _saving ? null : _save,
        icon: _saving
            ? const SizedBox(
                width: 18,
                height: 18,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : const AppStatusIcon(Icons.download_rounded),
      ),
    ];
  }

  Widget _imageBody() {
    final angle = _quarterTurns * math.pi / 2;
    final bytes = _bytes!;
    return Stack(
      children: [
        Positioned.fill(
          child: PhotoView.customChild(
            minScale: PhotoViewComputedScale.contained,
            maxScale: PhotoViewComputedScale.covered * 4,
            backgroundDecoration: const BoxDecoration(color: Colors.black),
            child: Transform.rotate(
              angle: angle,
              child: Image.memory(
                bytes,
                fit: BoxFit.contain,
                errorBuilder: (_, __, ___) => const Padding(
                  padding: EdgeInsets.all(24),
                  child: Text(
                    '图片内容无法解码',
                    style: TextStyle(color: Colors.white70),
                  ),
                ),
              ),
            ),
          ),
        ),
        Positioned(
          left: 16,
          right: 16,
          bottom: 20,
          child: _footer('${widget.file.mimeType} · $_sizeText · 双指缩放'),
        ),
      ],
    );
  }

  Widget _footer(String text) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: Colors.black.withValues(alpha: 0.55),
        borderRadius: BorderRadius.circular(14),
        border: Border.all(color: Colors.white.withValues(alpha: 0.12)),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        child: Row(
          children: [
            Expanded(
              child: Text(
                text,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(fontSize: 12, color: Colors.white70),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _contentBody() {
    if (!_textual) {
      return _binaryNotice();
    }
    final controller = _controller;
    if (controller == null) return _binaryNotice();
    if (_bytes == null) {
      return _emptyContentNotice();
    }
    return Column(
      children: [
        _attachmentMeta(),
        const Divider(height: 1, color: AppColors.divider),
        Expanded(
          child: Container(
            width: double.infinity,
            color: AppColors.codeBackground,
            padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
            child: TextField(
              controller: controller,
              readOnly: !_editing,
              maxLines: null,
              expands: true,
              textAlignVertical: TextAlignVertical.top,
              keyboardType: TextInputType.multiline,
              style: _editorTextStyle,
              cursorColor: AppColors.primary,
              onChanged: (_) {
                if (!_dirty) setState(() => _dirty = true);
              },
              decoration: const InputDecoration(
                border: InputBorder.none,
                isDense: true,
                contentPadding: EdgeInsets.zero,
              ),
            ),
          ),
        ),
      ],
    );
  }

  Widget _attachmentMeta() {
    final parts = <String>[
      widget.file.mimeType,
      _sizeText,
      _language == CodeLanguage.plain ? '纯文本' : '高亮：${_language.name}',
      _editing ? (_dirty ? '编辑中 · 未保存' : '编辑中') : '点击右上角编辑',
    ];
    return Container(
      width: double.infinity,
      color: AppColors.surface,
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      child: Text(
        parts.join(' · '),
        maxLines: 2,
        style: const TextStyle(fontSize: 12, color: AppColors.textMuted),
      ),
    );
  }

  Widget _emptyContentNotice() {
    return _notice(
      icon: Icons.inventory_2_outlined,
      title: '内容未保留',
      message: '这条历史附件过大，当时的原文没有缓存到本地，无法预览或编辑。新发送的附件不受影响。',
    );
  }

  Widget _binaryNotice() {
    return _notice(
      icon: Icons.insert_drive_file_outlined,
      title: widget.file.filename,
      message:
          '${widget.file.mimeType} · $_sizeText\n'
          '该格式暂不支持在应用内预览，可保存到本地后用其他应用打开。',
    );
  }

  Widget _notice({
    required IconData icon,
    required String title,
    required String message,
  }) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 40, color: AppColors.textMuted),
            const SizedBox(height: 12),
            Text(
              title,
              textAlign: TextAlign.center,
              style: const TextStyle(
                fontSize: 14,
                fontWeight: FontWeight.w600,
                color: AppColors.textPrimary,
              ),
            ),
            const SizedBox(height: 8),
            Text(
              message,
              textAlign: TextAlign.center,
              style: const TextStyle(
                fontSize: 12,
                height: 1.6,
                color: AppColors.textSecondary,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
