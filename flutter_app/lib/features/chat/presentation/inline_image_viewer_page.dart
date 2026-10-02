import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:photo_view/photo_view.dart';
import 'package:photo_view/photo_view_gallery.dart';

import '../data/chat_model.dart';

/// 应用内生成图片的全屏查看页：同一轮生成的多张图片放在一个横向分页里，
/// 左右滑动即可切换上一张 / 下一张，不需要退出后重新点击。
class InlineImageViewerPage extends StatefulWidget {
  final List<AiOutputFile> images;
  final int initialIndex;

  const InlineImageViewerPage({
    super.key,
    required this.images,
    this.initialIndex = 0,
  }) : assert(images.length > 0, '至少要有一张图片');

  @override
  State<InlineImageViewerPage> createState() => _InlineImageViewerPageState();
}

class _InlineImageViewerPageState extends State<InlineImageViewerPage> {
  late final PageController _pageController;
  late int _index;

  /// 每张图片各自的旋转角度（四分之一圈数）。
  final Map<int, int> _quarterTurns = {};

  AiOutputFile get _current => widget.images[_index];

  @override
  void initState() {
    super.initState();
    _index = widget.initialIndex.clamp(0, widget.images.length - 1);
    _pageController = PageController(initialPage: _index);
  }

  @override
  void dispose() {
    _pageController.dispose();
    super.dispose();
  }

  void _rotate() {
    setState(() {
      _quarterTurns[_index] = ((_quarterTurns[_index] ?? 0) + 1) % 4;
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
        ],
      ),
      body: Stack(
        children: [
          Positioned.fill(
            child: PhotoViewGallery.builder(
              pageController: _pageController,
              itemCount: widget.images.length,
              backgroundDecoration: const BoxDecoration(color: Colors.black),
              onPageChanged: (index) => setState(() => _index = index),
              builder: (context, index) {
                final file = widget.images[index];
                final angle = (_quarterTurns[index] ?? 0) * math.pi / 2;
                final bytes = file.imageBytes;
                if (bytes != null) {
                  return PhotoViewGalleryPageOptions.customChild(
                    minScale: PhotoViewComputedScale.contained,
                    maxScale: PhotoViewComputedScale.covered * 3,
                    child: Transform.rotate(
                      angle: angle,
                      child: Image.memory(bytes, fit: BoxFit.contain),
                    ),
                  );
                }
                return PhotoViewGalleryPageOptions(
                  imageProvider: NetworkImage(file.url),
                  minScale: PhotoViewComputedScale.contained,
                  maxScale: PhotoViewComputedScale.covered * 3,
                  errorBuilder: (_, __, ___) => const _ImageLoadFailed(),
                );
              },
            ),
          ),
          Positioned(
            left: 16,
            right: 16,
            bottom: 20,
            child: _InlineViewerFooter(
              filename: _current.filename,
              mimeType: _current.mimeType,
              index: _index,
              total: widget.images.length,
            ),
          ),
        ],
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

class _InlineViewerFooter extends StatelessWidget {
  final String filename;
  final String mimeType;
  final int index;
  final int total;

  const _InlineViewerFooter({
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
