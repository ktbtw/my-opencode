import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:photo_view/photo_view.dart';

import '../../../core/notifications/app_notification_controller.dart';
import '../data/chat_model.dart';
import 'artifact_download_notification.dart';

class ArtifactImageViewerPage extends ConsumerStatefulWidget {
  final String taskId;
  final AiArtifact artifact;
  final Uint8List imageBytes;
  final String heroTag;

  const ArtifactImageViewerPage({
    super.key,
    required this.taskId,
    required this.artifact,
    required this.imageBytes,
    required this.heroTag,
  });

  @override
  ConsumerState<ArtifactImageViewerPage> createState() =>
      _ArtifactImageViewerPageState();
}

class _ArtifactImageViewerPageState
    extends ConsumerState<ArtifactImageViewerPage> {
  var _quarterTurns = 0;
  var _downloading = false;

  Future<void> _download() async {
    if (_downloading) return;
    setState(() => _downloading = true);
    try {
      await downloadArtifactWithNotification(
        notifications: ref.read(appNotificationControllerProvider.notifier),
        taskId: widget.taskId,
        artifact: widget.artifact,
      );
    } catch (_) {
      // The shared download helper has already published the failure.
    } finally {
      if (mounted) setState(() => _downloading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final angle = _quarterTurns * math.pi / 2;
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        backgroundColor: Colors.black,
        foregroundColor: Colors.white,
        title: Text(
          widget.artifact.filename,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
        ),
        actions: [
          IconButton(
            tooltip: '旋转',
            onPressed: () =>
                setState(() => _quarterTurns = (_quarterTurns + 1) % 4),
            icon: const Icon(Icons.rotate_right_rounded),
          ),
          IconButton(
            tooltip: _downloading ? '下载中' : '下载',
            onPressed: _downloading ? null : _download,
            icon: _downloading
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: Colors.white,
                    ),
                  )
                : const Icon(Icons.download_rounded),
          ),
        ],
      ),
      body: Stack(
        children: [
          Positioned.fill(
            child: PhotoView.customChild(
              minScale: PhotoViewComputedScale.contained,
              maxScale: PhotoViewComputedScale.covered * 3,
              backgroundDecoration: const BoxDecoration(color: Colors.black),
              heroAttributes: PhotoViewHeroAttributes(tag: widget.heroTag),
              child: Transform.rotate(
                angle: angle,
                child: Image.memory(widget.imageBytes, fit: BoxFit.contain),
              ),
            ),
          ),
          Positioned(
            left: 16,
            right: 16,
            bottom: 20,
            child: DecoratedBox(
              decoration: BoxDecoration(
                color: Colors.black.withValues(alpha: 0.55),
                borderRadius: BorderRadius.circular(14),
                border: Border.all(color: Colors.white.withValues(alpha: 0.12)),
              ),
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 12,
                  vertical: 10,
                ),
                child: Row(
                  children: [
                    const Icon(
                      Icons.image_outlined,
                      size: 18,
                      color: Colors.white70,
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        '${widget.artifact.filename} · ${widget.artifact.mimeType}',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          fontSize: 12,
                          color: Colors.white70,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
