import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:photo_view/photo_view.dart';
import 'package:photo_view/photo_view_gallery.dart';
import 'package:share_plus/share_plus.dart';

import '../../../core/theme/app_colors.dart';
import '../../../shared/widgets/app_status_icon.dart';
import 'media_viewer_widgets.dart';

class GeneratedImageItem {
  const GeneratedImageItem({
    required this.filename,
    required this.mimeType,
    required this.loadBytes,
    this.sizeBytes = 0,
    this.heroTag,
  });

  final String filename;
  final String mimeType;
  final Future<Uint8List?> Function() loadBytes;
  final int sizeBytes;
  final String? heroTag;
}

/// 两类生成图片共享交互和布局，下载仍由各自的数据来源处理。
class GeneratedImageViewer extends StatefulWidget {
  const GeneratedImageViewer({
    super.key,
    required this.images,
    required this.onSave,
    this.initialIndex = 0,
  }) : assert(images.length > 0);

  final List<GeneratedImageItem> images;
  final int initialIndex;
  final Future<void> Function(int index, Uint8List bytes) onSave;

  @override
  State<GeneratedImageViewer> createState() => _GeneratedImageViewerState();
}

enum _CanvasBackground { grid, light, dark }

class _GeneratedImageViewerState extends State<GeneratedImageViewer> {
  late final PageController _pages;
  final ScrollController _thumbnailScroll = ScrollController();
  late final List<PhotoViewController> _views;
  late int _index;
  final Map<int, Uint8List> _bytes = {};
  final Map<int, Size> _sizes = {};
  final Set<int> _loading = {};
  final Set<int> _failed = {};
  final Set<int> _mirrored = {};
  bool _saving = false;
  bool _sharing = false;
  bool _immersive = false;
  bool _infoVisible = true;
  _CanvasBackground _background = _CanvasBackground.grid;
  Size _viewport = Size.zero;

  GeneratedImageItem get _current => widget.images[_index];
  PhotoViewController get _view => _views[_index];
  bool get _ready => _bytes.containsKey(_index);
  Color get _canvasColor => switch (_background) {
    _CanvasBackground.dark => const Color(0xFF111C30),
    _CanvasBackground.light => Colors.white,
    _CanvasBackground.grid => AppColors.background,
  };

  @override
  void initState() {
    super.initState();
    _index = widget.initialIndex.clamp(0, widget.images.length - 1);
    _pages = PageController(initialPage: _index);
    _views = List.generate(widget.images.length, (_) => PhotoViewController());
    _loadAround(_index);
    _revealThumbnail();
  }

  @override
  void dispose() {
    _pages.dispose();
    _thumbnailScroll.dispose();
    for (final view in _views) {
      view.dispose();
    }
    super.dispose();
  }

  void _loadAround(int index) {
    _load(index);
    if (index > 0) _load(index - 1);
    if (index + 1 < widget.images.length) _load(index + 1);
  }

  Future<void> _load(int index, {bool retry = false}) async {
    if (_loading.contains(index) ||
        _bytes.containsKey(index) ||
        (!retry && _failed.contains(index))) {
      return;
    }
    _loading.add(index);
    _failed.remove(index);
    ui.ImmutableBuffer? buffer;
    ui.ImageDescriptor? descriptor;
    try {
      final bytes = await widget.images[index].loadBytes();
      if (bytes == null || bytes.isEmpty) throw StateError('图片为空');
      buffer = await ui.ImmutableBuffer.fromUint8List(bytes);
      descriptor = await ui.ImageDescriptor.encoded(buffer);
      if (!mounted) return;
      setState(() {
        _bytes[index] = bytes;
        _sizes[index] = Size(
          descriptor!.width.toDouble(),
          descriptor.height.toDouble(),
        );
      });
    } catch (_) {
      if (mounted) setState(() => _failed.add(index));
    } finally {
      descriptor?.dispose();
      buffer?.dispose();
      _loading.remove(index);
    }
  }

  void _goTo(int index) {
    if (index < 0 || index >= widget.images.length) return;
    _pages.animateToPage(
      index,
      duration: const Duration(milliseconds: 230),
      curve: Curves.easeOutCubic,
    );
  }

  void _revealThumbnail() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || !_thumbnailScroll.hasClients) return;
      final position = _thumbnailScroll.position;
      final target = (_index * 72 - (position.viewportDimension - 64) / 2)
          .clamp(0.0, position.maxScrollExtent);
      _thumbnailScroll.animateTo(
        target,
        duration: const Duration(milliseconds: 230),
        curve: Curves.easeOutCubic,
      );
    });
  }

  double get _fitScale {
    final size = _sizes[_index];
    if (size == null || _viewport.isEmpty) return 1;
    final rotated = (_view.rotation / (math.pi / 2)).round().isOdd;
    return math.min(
      _viewport.width / (rotated ? size.height : size.width),
      _viewport.height / (rotated ? size.width : size.height),
    );
  }

  void _zoom(double factor) {
    if (!_ready) return;
    _view.scale = ((_view.scale ?? _fitScale) * factor).clamp(
      math.min(_fitScale * .1, 1),
      math.max(_fitScale * 8, 8),
    );
  }

  void _fit() => _view.updateMultiple(position: Offset.zero, scale: _fitScale);

  void _rotate() {
    _view.rotation += math.pi / 2;
    _fit();
  }

  void _reset() {
    setState(() => _mirrored.remove(_index));
    _view.updateMultiple(position: Offset.zero, rotation: 0);
    _fit();
  }

  void _notice(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text(message)));
  }

  Future<void> _save() async {
    final index = _index;
    final bytes = _bytes[index];
    if (bytes == null || _saving) return;
    setState(() => _saving = true);
    try {
      await widget.onSave(index, bytes);
    } catch (_) {
      _notice('保存失败，请检查下载目录和网络后重试');
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _share() async {
    final bytes = _bytes[_index];
    if (bytes == null || _sharing) return;
    final item = _current;
    final box = context.findRenderObject() as RenderBox?;
    setState(() => _sharing = true);
    try {
      await SharePlus.instance.share(
        ShareParams(
          files: [XFile.fromData(bytes, mimeType: item.mimeType)],
          fileNameOverrides: [item.filename.replaceAll(RegExp(r'[/\\]'), '_')],
          sharePositionOrigin: box == null
              ? null
              : box.localToGlobal(Offset.zero) & box.size,
        ),
      );
    } catch (_) {
      _notice('当前设备无法分享此图片，请先保存原图');
    } finally {
      if (mounted) setState(() => _sharing = false);
    }
  }

  KeyEventResult _onKey(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent) return KeyEventResult.ignored;
    switch (event.logicalKey) {
      case LogicalKeyboardKey.arrowLeft:
        _goTo(_index - 1);
      case LogicalKeyboardKey.arrowRight:
        _goTo(_index + 1);
      case LogicalKeyboardKey.equal:
      case LogicalKeyboardKey.add:
        _zoom(1.25);
      case LogicalKeyboardKey.minus:
        _zoom(.8);
      case LogicalKeyboardKey.digit0:
        if (_ready) _reset();
      case LogicalKeyboardKey.keyR:
        if (_ready) _rotate();
      case LogicalKeyboardKey.escape:
        if (!_immersive) return KeyEventResult.ignored;
        setState(() => _immersive = false);
      default:
        return KeyEventResult.ignored;
    }
    return KeyEventResult.handled;
  }

  List<(String, String)> get _info {
    final size = _sizes[_index];
    return [
      ('文件名', _current.filename),
      (
        '分辨率',
        size == null ? '读取中' : '${size.width.round()} × ${size.height.round()}',
      ),
      ('格式', _current.mimeType.replaceFirst('image/', '').toUpperCase()),
      ('大小', mediaFileSize(_bytes[_index]?.length ?? _current.sizeBytes)),
      if (size != null)
        ('宽高比', '${(size.width / size.height).toStringAsFixed(2)} : 1'),
    ];
  }

  void _showInfo() {
    if (MediaQuery.sizeOf(context).width >= 1000) {
      setState(() => _infoVisible = !_infoVisible);
      return;
    }
    showModalBottomSheet<void>(
      context: context,
      backgroundColor: AppColors.background,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (_) => SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(20, 0, 20, 24),
          child: MediaPanel(
            title: '图片信息',
            child: MediaInfoList(entries: _info),
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final wide = MediaQuery.sizeOf(context).width >= 1000;
    return Scaffold(
      backgroundColor: AppColors.background,
      appBar: _immersive
          ? null
          : AppBar(
              leading: const AppStatusBackButton(),
              titleSpacing: 0,
              title: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text(
                    '图片预览',
                    style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600),
                  ),
                  Text(
                    _current.filename,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      fontSize: 11,
                      color: AppColors.textMuted,
                    ),
                  ),
                ],
              ),
              actions: [
                IconButton(
                  tooltip: '图片信息',
                  onPressed: _showInfo,
                  icon: const AppStatusIcon(Icons.info_outline),
                ),
                IconButton(
                  tooltip: _sharing ? '正在分享' : '分享原图',
                  onPressed: _ready && !_sharing ? _share : null,
                  icon: const AppStatusIcon(Icons.ios_share_outlined),
                ),
                Padding(
                  padding: const EdgeInsets.only(right: 12, left: 4),
                  child: FilledButton.icon(
                    onPressed: _ready && !_saving ? _save : null,
                    icon: _saving
                        ? const SizedBox(
                            width: 16,
                            height: 16,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const AppStatusIcon(Icons.download_rounded, size: 17),
                    label: Text(
                      _saving ? '保存中' : '保存原图',
                      style: const TextStyle(fontSize: 12),
                    ),
                    style: FilledButton.styleFrom(
                      padding: const EdgeInsets.symmetric(horizontal: 12),
                    ),
                  ),
                ),
              ],
            ),
      body: Focus(
        autofocus: true,
        onKeyEvent: _onKey,
        child: SafeArea(
          child: Row(
            children: [
              Expanded(
                child: Column(
                  children: [
                    Expanded(
                      child: Padding(
                        padding: EdgeInsets.fromLTRB(
                          _immersive ? 0 : 16,
                          _immersive ? 0 : 16,
                          _immersive ? 0 : 16,
                          0,
                        ),
                        child: _gallery(),
                      ),
                    ),
                    if (!_immersive) ...[
                      _toolbar(),
                      if (widget.images.length > 1) _thumbnails(),
                      Padding(
                        padding: const EdgeInsets.only(bottom: 12),
                        child: Text(
                          wide
                              ? '滚轮或双指缩放 · ← → 切换 · R 旋转 · 0 重置'
                              : '双指缩放 · 左右滑动切换图片',
                          style: const TextStyle(
                            fontSize: 10,
                            color: AppColors.textMuted,
                          ),
                        ),
                      ),
                    ],
                  ],
                ),
              ),
              if (wide && !_immersive && _infoVisible)
                SizedBox(
                  width: 280,
                  child: ListView(
                    padding: const EdgeInsets.fromLTRB(0, 16, 16, 16),
                    children: [
                      MediaPanel(
                        title: '图片信息',
                        trailing: const MediaBadge('原图'),
                        child: MediaInfoList(entries: _info),
                      ),
                      const SizedBox(height: 12),
                      MediaPanel(
                        title: '画布背景',
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            _backgroundPicker(),
                            const SizedBox(height: 12),
                            const Text(
                              '切换背景，检查透明区域与画面边缘。',
                              style: TextStyle(
                                fontSize: 12,
                                height: 1.6,
                                color: AppColors.textMuted,
                              ),
                            ),
                          ],
                        ),
                      ),
                      const SizedBox(height: 12),
                      const Padding(
                        padding: EdgeInsets.all(8),
                        child: Text(
                          '旋转和镜像仅影响预览，保存与分享始终使用原图。',
                          style: TextStyle(
                            fontSize: 11,
                            height: 1.7,
                            color: AppColors.textMuted,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _gallery() => ClipRRect(
    borderRadius: BorderRadius.circular(_immersive ? 0 : 16),
    child: LayoutBuilder(
      builder: (context, constraints) {
        _viewport = constraints.biggest;
        final dark = _immersive || _background == _CanvasBackground.dark;
        final foreground = dark ? Colors.white : AppColors.textSecondary;
        return Stack(
          fit: StackFit.expand,
          children: [
            ColoredBox(
              color: _immersive ? const Color(0xFF111C30) : _canvasColor,
            ),
            if (!_immersive && _background == _CanvasBackground.grid)
              const CustomPaint(painter: _CheckerPainter()),
            PhotoViewGallery.builder(
              pageController: _pages,
              itemCount: widget.images.length,
              backgroundDecoration: const BoxDecoration(
                color: Colors.transparent,
              ),
              onPageChanged: (index) {
                setState(() => _index = index);
                _loadAround(index);
                _revealThumbnail();
              },
              builder: (_, index) {
                final bytes = _bytes[index];
                final size = _sizes[index];
                if (bytes == null || size == null) {
                  return PhotoViewGalleryPageOptions.customChild(
                    disableGestures: true,
                    child: Center(
                      child: _failed.contains(index)
                          ? Column(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                Icon(
                                  Icons.broken_image_outlined,
                                  size: 32,
                                  color: foreground,
                                ),
                                const SizedBox(height: 12),
                                Text(
                                  '图片加载失败',
                                  style: TextStyle(
                                    color: foreground,
                                    fontSize: 13,
                                  ),
                                ),
                                TextButton(
                                  onPressed: () {
                                    setState(() => _failed.remove(index));
                                    _load(index, retry: true);
                                  },
                                  child: const Text('重新加载'),
                                ),
                              ],
                            )
                          : const CircularProgressIndicator(strokeWidth: 2),
                    ),
                  );
                }
                return PhotoViewGalleryPageOptions.customChild(
                  controller: _views[index],
                  childSize: size,
                  minScale: PhotoViewComputedScale.contained * .1,
                  initialScale: PhotoViewComputedScale.contained,
                  maxScale: math.max(
                    8.0,
                    math.max(
                          _viewport.width / size.width,
                          _viewport.height / size.height,
                        ) *
                        8,
                  ),
                  heroAttributes: widget.images[index].heroTag == null
                      ? null
                      : PhotoViewHeroAttributes(
                          tag: widget.images[index].heroTag!,
                        ),
                  child: Transform.flip(
                    flipX: _mirrored.contains(index),
                    child: Image.memory(
                      bytes,
                      fit: BoxFit.contain,
                      gaplessPlayback: true,
                    ),
                  ),
                );
              },
            ),
            if (widget.images.length > 1) ...[
              Positioned(
                left: 12,
                top: 12,
                child: MediaBadge('${_index + 1} / ${widget.images.length}'),
              ),
              Align(
                alignment: Alignment.centerLeft,
                child: Padding(
                  padding: const EdgeInsets.all(8),
                  child: _navButton(
                    '上一张',
                    Icons.chevron_left,
                    _index > 0 ? () => _goTo(_index - 1) : null,
                  ),
                ),
              ),
              Align(
                alignment: Alignment.centerRight,
                child: Padding(
                  padding: const EdgeInsets.all(8),
                  child: _navButton(
                    '下一张',
                    Icons.chevron_right,
                    _index + 1 < widget.images.length
                        ? () => _goTo(_index + 1)
                        : null,
                  ),
                ),
              ),
            ],
            Positioned(
              top: 8,
              right: 8,
              child: _navButton(
                _immersive ? '退出沉浸预览' : '沉浸预览',
                _immersive ? Icons.fullscreen_exit : Icons.fullscreen,
                () => setState(() => _immersive = !_immersive),
              ),
            ),
          ],
        );
      },
    ),
  );

  Widget _navButton(String tooltip, IconData icon, VoidCallback? onPressed) =>
      IconButton.filledTonal(
        tooltip: tooltip,
        onPressed: onPressed,
        style: IconButton.styleFrom(
          backgroundColor: AppColors.surface.withValues(alpha: .9),
          foregroundColor: AppColors.textSecondary,
        ),
        icon: AppStatusIcon(icon, size: 18),
      );

  Widget _toolbar() => Container(
    margin: const EdgeInsets.fromLTRB(16, 12, 16, 12),
    padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
    decoration: BoxDecoration(
      color: AppColors.surface,
      border: Border.all(color: AppColors.borderLight),
      borderRadius: BorderRadius.circular(14),
    ),
    child: SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          MediaToolButton(
            icon: Icons.remove,
            label: '缩小',
            onPressed: _ready ? () => _zoom(.8) : null,
          ),
          StreamBuilder<PhotoViewControllerValue>(
            key: ValueKey(_index),
            stream: _view.outputStateStream,
            builder: (_, snapshot) => SizedBox(
              width: 58,
              child: Center(
                child: Text(
                  _ready
                      ? '${((snapshot.data?.scale ?? _view.scale ?? _fitScale) * 100).round()}%'
                      : '—',
                  style: const TextStyle(
                    fontSize: 12,
                    color: AppColors.textSecondary,
                  ),
                ),
              ),
            ),
          ),
          MediaToolButton(
            icon: Icons.add_outlined,
            label: '放大',
            onPressed: _ready ? () => _zoom(1.25) : null,
          ),
          MediaToolButton(
            icon: Icons.fit_screen_outlined,
            label: '适应',
            onPressed: _ready ? _fit : null,
          ),
          MediaToolButton(
            icon: Icons.crop_original_outlined,
            label: '原始尺寸',
            onPressed: _ready
                ? () => _view.updateMultiple(scale: 1, position: Offset.zero)
                : null,
          ),
          MediaToolButton(
            icon: Icons.rotate_right_rounded,
            label: '旋转',
            onPressed: _ready ? _rotate : null,
          ),
          MediaToolButton(
            icon: Icons.flip_outlined,
            label: '镜像',
            selected: _mirrored.contains(_index),
            onPressed: _ready
                ? () => setState(() {
                    if (!_mirrored.remove(_index)) _mirrored.add(_index);
                  })
                : null,
          ),
          MediaToolButton(
            icon: Icons.restart_alt,
            label: '重置',
            onPressed: _ready ? _reset : null,
          ),
          PopupMenuButton<_CanvasBackground>(
            tooltip: '画布背景',
            initialValue: _background,
            onSelected: (value) => setState(() => _background = value),
            itemBuilder: (_) => [
              for (final value in _CanvasBackground.values)
                PopupMenuItem(
                  value: value,
                  child: Text(_backgroundLabel(value)),
                ),
            ],
            child: const Padding(
              padding: EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              child: Column(
                children: [
                  AppStatusIcon(Icons.contrast, size: 19),
                  SizedBox(height: 5),
                  Text(
                    '背景',
                    style: TextStyle(
                      fontSize: 11,
                      color: AppColors.textSecondary,
                    ),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    ),
  );

  String _backgroundLabel(_CanvasBackground value) => switch (value) {
    _CanvasBackground.grid => '透明网格',
    _CanvasBackground.light => '纯白',
    _CanvasBackground.dark => '深蓝',
  };

  Widget _backgroundPicker() => Wrap(
    spacing: 6,
    children: [
      for (final value in _CanvasBackground.values)
        ChoiceChip(
          label: Text(
            _backgroundLabel(value),
            style: const TextStyle(fontSize: 11),
          ),
          selected: _background == value,
          selectedColor: AppColors.primaryLight,
          backgroundColor: AppColors.surface,
          labelStyle: TextStyle(
            color: _background == value
                ? AppColors.primary
                : AppColors.textSecondary,
          ),
          side: BorderSide(
            color: _background == value
                ? AppColors.primaryMuted
                : AppColors.borderLight,
          ),
          showCheckmark: false,
          onSelected: (_) => setState(() => _background = value),
        ),
    ],
  );

  Widget _thumbnails() => SizedBox(
    height: 70,
    child: ListView.separated(
      controller: _thumbnailScroll,
      scrollDirection: Axis.horizontal,
      padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
      itemCount: widget.images.length,
      separatorBuilder: (_, _) => const SizedBox(width: 8),
      itemBuilder: (_, index) {
        _load(index);
        return Tooltip(
          message: widget.images[index].filename,
          child: Semantics(
            button: true,
            selected: index == _index,
            label: '查看第 ${index + 1} 张图片',
            child: InkWell(
              onTap: () => _goTo(index),
              borderRadius: BorderRadius.circular(9),
              child: Container(
                width: 64,
                padding: const EdgeInsets.all(3),
                decoration: BoxDecoration(
                  color: AppColors.surface,
                  borderRadius: BorderRadius.circular(9),
                  border: Border.all(
                    color: index == _index
                        ? AppColors.primary
                        : AppColors.borderLight,
                    width: index == _index ? 2 : 1,
                  ),
                ),
                child: ClipRRect(
                  borderRadius: BorderRadius.circular(5),
                  child: _bytes[index] == null
                      ? Center(
                          child: Text(
                            '${index + 1}',
                            style: const TextStyle(color: AppColors.textMuted),
                          ),
                        )
                      : Image.memory(
                          _bytes[index]!,
                          fit: BoxFit.cover,
                          cacheWidth: 128,
                        ),
                ),
              ),
            ),
          ),
        );
      },
    ),
  );
}

class _CheckerPainter extends CustomPainter {
  const _CheckerPainter();

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()..color = AppColors.borderLight;
    const cell = 16.0;
    for (var y = 0; y * cell < size.height; y++) {
      for (var x = y % 2; x * cell < size.width; x += 2) {
        canvas.drawRect(Rect.fromLTWH(x * cell, y * cell, cell, cell), paint);
      }
    }
  }

  @override
  bool shouldRepaint(_CheckerPainter oldDelegate) => false;
}
