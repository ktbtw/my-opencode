import 'dart:io';
import 'dart:typed_data';

import 'package:desktop_drop/desktop_drop.dart';
import 'package:flutter/material.dart';

import '../data/attachment_upload.dart';
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
  bool _hovering = false;
  bool _loading = false;

  Future<void> _handleDrop(DropDoneDetails details) async {
    if (!widget.enabled) return;
    setState(() {
      _hovering = false;
      _loading = true;
    });
    final dropped = await _readFiles(details);
    if (!mounted) return;
    setState(() => _loading = false);
    if (dropped.isEmpty) return;
    await widget.onDrop(dropped);
  }

  Future<List<AttachmentDropFile>> _readFiles(DropDoneDetails details) async {
    final result = <AttachmentDropFile>[];
    for (final file in details.files) {
      if (file is DropItemDirectory) continue;
      final path = file.path;
      if (path.isNotEmpty) {
        try {
          final type = FileSystemEntity.typeSync(path, followLinks: true);
          if (type == FileSystemEntityType.directory) continue;
        } catch (_) {
          continue;
        }
      }
      final Uint8List bytes;
      try {
        bytes = await file.readAsBytes();
      } catch (_) {
        continue;
      }
      if (bytes.isEmpty) continue;
      final filename = file.name.trim().isNotEmpty
          ? file.name
          : (path.isNotEmpty ? path.split(RegExp(r'[\\/]')).last : 'dropped.bin');
      result.add((
        filename: filename,
        mimeType: file.mimeType?.trim().isNotEmpty == true
            ? file.mimeType!
            : attachmentMimeFromName(
                filename,
                fallbackMimeType: 'application/octet-stream',
              ),
        sizeBytes: bytes.length,
        bytes: bytes,
      ));
    }
    return result;
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.enabled) return widget.child;
    return DropTarget(
      onDragEntered: (_) {
        if (_hovering) return;
        setState(() => _hovering = true);
      },
      onDragExited: (_) {
        if (!_hovering && !_loading) return;
        setState(() => _hovering = false);
      },
      onDragDone: _handleDrop,
      child: Stack(
        fit: StackFit.expand,
        children: [
          widget.child,
          if (_hovering || _loading) AttachmentDropOverlay(loading: _loading),
        ],
      ),
    );
  }
}
