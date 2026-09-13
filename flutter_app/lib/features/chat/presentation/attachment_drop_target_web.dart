import 'dart:async';
import 'dart:html' as html;
import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'attachment_drop_file.dart';
import 'attachment_drop_overlay.dart';

class AttachmentDropTarget extends StatefulWidget {
  final Widget child;
  final bool enabled;
  final Future<void> Function(List<AttachmentDropFile> files) onDrop;

  const AttachmentDropTarget({
    super.key,
    required this.child,
    required this.enabled,
    required this.onDrop,
  });

  @override
  State<AttachmentDropTarget> createState() => _AttachmentDropTargetState();
}

class _AttachmentDropTargetState extends State<AttachmentDropTarget> {
  final List<StreamSubscription<dynamic>> _subs = [];
  int _depth = 0;
  bool _hovering = false;
  bool _loading = false;

  @override
  void initState() {
    super.initState();
    _bind();
  }

  @override
  void didUpdateWidget(covariant AttachmentDropTarget oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.enabled == widget.enabled) return;
    _unbind();
    _depth = 0;
    _hovering = false;
    _loading = false;
    _bind();
  }

  @override
  void dispose() {
    _unbind();
    super.dispose();
  }

  void _bind() {
    if (!widget.enabled) return;
    _subs.addAll([
      html.document.onDragEnter.listen(_handleEnter),
      html.document.onDragOver.listen(_handleOver),
      html.document.onDragLeave.listen(_handleLeave),
      html.document.onDrop.listen(_handleDrop),
    ]);
  }

  void _unbind() {
    for (final sub in _subs) {
      sub.cancel();
    }
    _subs.clear();
  }

  bool _hasFiles(dynamic event) {
    final transfer = event.dataTransfer;
    final types = transfer?.types;
    if (types is Iterable) {
      for (final type in types) {
        if ('$type' == 'Files') return true;
      }
    }
    final files = transfer?.files;
    return files != null && files.length > 0;
  }

  void _prevent(dynamic event) {
    event.preventDefault();
    event.stopPropagation();
  }

  void _handleEnter(dynamic event) {
    if (!_hasFiles(event)) return;
    _prevent(event);
    _depth += 1;
    if (_hovering) return;
    setState(() => _hovering = true);
  }

  void _handleOver(dynamic event) {
    if (!_hasFiles(event)) return;
    _prevent(event);
    if (_hovering) return;
    setState(() => _hovering = true);
  }

  void _handleLeave(dynamic event) {
    if (!_hasFiles(event)) return;
    _prevent(event);
    if (_depth > 0) _depth -= 1;
    if (_depth > 0 || !_hovering) return;
    setState(() => _hovering = false);
  }

  Future<void> _handleDrop(dynamic event) async {
    if (!_hasFiles(event)) return;
    _prevent(event);
    final files = event.dataTransfer?.files;
    _depth = 0;
    if (mounted) {
      setState(() {
        _hovering = false;
        _loading = true;
      });
    }
    final dropped = await _readFiles(files);
    if (!mounted) return;
    setState(() => _loading = false);
    if (dropped.isEmpty) return;
    await widget.onDrop(dropped);
  }

  Future<List<AttachmentDropFile>> _readFiles(dynamic files) async {
    final result = <AttachmentDropFile>[];
    final length = files?.length;
    if (length is! int || length <= 0) return result;
    for (var index = 0; index < length; index++) {
      final file = files[index];
      if (file is! html.File) continue;
      final bytes = await _readBytes(file);
      if (bytes == null) continue;
      result.add((
        filename: file.name,
        mimeType: file.type.isNotEmpty ? file.type : 'application/octet-stream',
        sizeBytes: file.size,
        bytes: bytes,
      ));
    }
    return result;
  }

  Future<Uint8List?> _readBytes(html.File file) {
    final completer = Completer<Uint8List?>();
    final reader = html.FileReader();

    void finish(Uint8List? bytes) {
      if (!completer.isCompleted) completer.complete(bytes);
    }

    reader.onAbort.listen((_) => finish(null));
    reader.onError.listen((_) => finish(null));
    reader.onLoad.listen((_) {
      final result = reader.result;
      if (result is ByteBuffer) {
        finish(Uint8List.view(result));
        return;
      }
      if (result is Uint8List) {
        finish(result);
        return;
      }
      if (result is List<int>) {
        finish(Uint8List.fromList(result));
        return;
      }
      finish(null);
    });

    reader.readAsArrayBuffer(file);
    return completer.future;
  }

  @override
  Widget build(BuildContext context) {
    return Stack(
      fit: StackFit.expand,
      children: [
        widget.child,
        if (_hovering || _loading) AttachmentDropOverlay(loading: _loading),
      ],
    );
  }
}
