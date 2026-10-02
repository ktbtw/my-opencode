import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:photo_view/photo_view.dart';
import 'package:photo_view/photo_view_gallery.dart';

import '../../../core/notifications/app_notification_controller.dart';
import '../data/artifact_download_service.dart';
import '../data/chat_model.dart';
import 'artifact_download_notification.dart';

/// 按需加载某张交付图片的字节；测试可注入假实现避免真实网络请求。
typedef ArtifactBytesLoader = Future<Uint8List?> Function(AiArtifact artifact);

/// 交付图片查看页：同一轮交付的多张图片放在同一个横向分页里，
/// 左右滑动即可切换上一张 / 下一张，不需要退出后重新点击。
class ArtifactImageViewerPage extends ConsumerStatefulWidget {
  final String? project;
  final String taskId;

  /// 同一轮交付里的全部图片，用于左右滑动切换。
  final List<AiArtifact> images;

  /// 打开时展示的图片下标。
  final int initialIndex;

  /// 点击的那张图片已经加载好的字节，避免打开时闪一下。
  final Uint8List? initialBytes;

  /// 测试注入用；为空时走真实下载通道。
  final ArtifactBytesLoader? bytesLoader;

  const ArtifactImageViewerPage({
    super.key,
    this.project,
    required this.taskId,
    required this.images,
    this.initialIndex = 0,
    this.initialBytes,
    this.bytesLoader,
  }) : assert(images.length > 0, '至少要有一张图片');

  /// 与网格卡片一致的 Hero 标识，保证从卡片放大时有过渡动画。
  static String heroTagFor(String taskId, String artifactId) =>
      'artifact-image-$taskId-$artifactId';

  @override
  ConsumerState<ArtifactImageViewerPage> createState() =>
      _ArtifactImageViewerPageState();
}

class _ArtifactImageViewerPageState
    extends ConsumerState<ArtifactImageViewerPage> {
  late final PageController _pageController;
  late int _index;

  /// 每张图片各自的旋转角度（四分之一圈数）。
  final Map<int, int> _quarterTurns = {};

  List<Uint8List?>? _bytesList;
  var _loading = true;
  var _downloading = false;

  AiArtifact get _current => widget.images[_index];

  int get _turns => _quarterTurns[_index] ?? 0;

  @override
  void initState() {
    super.initState();
    _index = widget.initialIndex.clamp(0, widget.images.length - 1);
    _pageController = PageController(initialPage: _index);
    _loadAll();
  }

  @override
  void dispose() {
    _pageController.dispose();
    super.dispose();
  }

  Future<Uint8List?> _loadBytes(AiArtifact artifact) async {
    final loader = widget.bytesLoader;
    if (loader != null) return loader(artifact);
    try {
      return await ArtifactDownloadService.fetchBytes(
        taskId: widget.taskId,
        artifact: artifact,
      );
    } catch (_) {
      return null;
    }
  }

  Future<void> _loadAll() async {
    final results = await Future.wait(
      widget.images.map((artifact) async {
        // 点击的那张已经有字节，直接用，避免重复请求与闪烁。
        if (widget.initialBytes != null &&
            artifact.id == widget.images[_index].id) {
          return widget.initialBytes;
        }
        return _loadBytes(artifact);
      }),
    );
    if (!mounted) return;
    setState(() {
      _bytesList = results;
      _loading = false;
    });
  }

  Future<void> _download() async {
    if (_downloading) return;
    setState(() => _downloading = true);
    try {
      await downloadArtifactWithNotification(
        notifications: ref.read(appNotificationControllerProvider.notifier),
        taskId: widget.taskId,
        artifact: _current,
        project: widget.project,
      );
    } catch (_) {
      // 下载助手内部已经提示失败原因。
    } finally {
      if (mounted) setState(() => _downloading = false);
    }
  }

  void _rotate() {
    setState(() {
      _quarterTurns[_index] = (_turns + 1) % 4;
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        backgroundColor: Colors.black,
        foregroundColor: Colors.white,
        title: Text(
          _current.filename,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
        ),
        actions: [
          IconButton(
            tooltip: '旋转',
            onPressed: _rotate,
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
      body: _loading ? const _ViewerLoading() : _buildGallery(),
    );
  }

  Widget _buildGallery() {
    final bytesList = _bytesList ?? const <Uint8List?>[];
    return Stack(
      children: [
        Positioned.fill(
          child: PhotoViewGallery.builder(
            pageController: _pageController,
            itemCount: widget.images.length,
            backgroundDecoration: const BoxDecoration(color: Colors.black),
            onPageChanged: (index) => setState(() => _index = index),
            builder: (context, index) {
              final artifact = widget.images[index];
              final bytes =
                  index < bytesList.length ? bytesList[index] : null;
              return PhotoViewGalleryPageOptions.customChild(
                heroAttributes: PhotoViewHeroAttributes(
                  tag: ArtifactImageViewerPage.heroTagFor(
                    widget.taskId,
                    artifact.id,
                  ),
                ),
                minScale: PhotoViewComputedScale.contained,
                maxScale: PhotoViewComputedScale.covered * 3,
                child: bytes == null
                    ? const _ImageLoadFailed()
                    : Transform.rotate(
                        angle: (_quarterTurns[index] ?? 0) * math.pi / 2,
                        child: Image.memory(bytes, fit: BoxFit.contain),
                      ),
              );
            },
          ),
        ),
        Positioned(
          left: 16,
          right: 16,
          bottom: 20,
          child: _ViewerFooter(
            filename: _current.filename,
            mimeType: _current.mimeType,
            index: _index,
            total: widget.images.length,
          ),
        ),
      ],
    );
  }
}

class _ViewerLoading extends StatelessWidget {
  const _ViewerLoading();

  @override
  Widget build(BuildContext context) {
    return const Center(
      child: SizedBox(
        width: 26,
        height: 26,
        child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white70),
      ),
    );
  }
}

class _ImageLoadFailed extends StatelessWidget {
  const _ImageLoadFailed();

  @override
  Widget build(BuildContext context) {
    return const Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.broken_image_outlined, color: Colors.white54, size: 40),
          SizedBox(height: 10),
          Text(
            '图片加载失败',
            style: TextStyle(fontSize: 12, color: Colors.white70),
          ),
        ],
      ),
    );
  }
}

class _ViewerFooter extends StatelessWidget {
  final String filename;
  final String mimeType;
  final int index;
  final int total;

  const _ViewerFooter({
    required this.filename,
    required this.mimeType,
    required this.index,
    required this.total,
  });

  @override
  Widget build(BuildContext context) {
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
            const Icon(
              Icons.image_outlined,
              size: 18,
              color: Colors.white70,
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                '$filename · $mimeType',
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(fontSize: 12, color: Colors.white70),
              ),
            ),
            if (total > 1) ...[
              const SizedBox(width: 8),
              Text(
                '${index + 1} / $total',
                style: const TextStyle(
                  fontSize: 12,
                  color: Colors.white,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
