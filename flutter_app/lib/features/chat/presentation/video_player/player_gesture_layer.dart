import 'package:flutter/material.dart';

/// 播放器画面上的手势层。
///
/// - 单击：显示 / 隐藏控制栏（由外部处理）
/// - 双击左 / 右：快退 / 快进 10 秒
/// - 上下滑：左侧调亮度、右侧调音量
/// - 左右滑：拖动进度
///
/// 首击需要延迟判定，等一个双击窗口再决定是「单击」还是「双击」。
class PlayerGestureLayer extends StatefulWidget {
  final Widget child;

  /// 单击（非双击、非滑动）。
  final VoidCallback onTap;

  /// 双击快进退：负数为快退。
  final ValueChanged<int> onDoubleTapSeek;

  /// 竖向拖动：进度比例 0–1 与「左亮度 / 右音量」标识。
  final void Function({required bool brightness, required double delta})
  onVerticalDrag;

  /// 竖向拖动开始：通知是亮度（左半屏）还是音量（右半屏）。
  final ValueChanged<bool>? onVerticalDragStart;

  /// 横向拖动进度：比例 0–1。
  final ValueChanged<double>? onHorizontalSeek;
  final VoidCallback? onHorizontalSeekEnd;

  const PlayerGestureLayer({
    super.key,
    required this.child,
    required this.onTap,
    required this.onDoubleTapSeek,
    required this.onVerticalDrag,
    this.onVerticalDragStart,
    this.onHorizontalSeek,
    this.onHorizontalSeekEnd,
  });

  @override
  State<PlayerGestureLayer> createState() => _PlayerGestureLayerState();
}

class _PlayerGestureLayerState extends State<PlayerGestureLayer> {
  static const _doubleTapWindow = Duration(milliseconds: 260);

  DateTime? _lastTapAt;
  double? _lastTapX;
  bool _dragging = false;

  void _handleTapUp(TapUpDetails details) {
    final now = DateTime.now();
    final x = details.localPosition.dx;
    final last = _lastTapAt;
    if (last != null && now.difference(last) < _doubleTapWindow) {
      final width = context.size?.width ?? 0;
      final left = (_lastTapX ?? x) < width / 2;
      _lastTapAt = null;
      _lastTapX = null;
      widget.onDoubleTapSeek(left ? -10 : 10);
      return;
    }
    _lastTapAt = now;
    _lastTapX = x;
    // 延迟一拍，确认没有第二击后再当作单击。
    Future<void>.delayed(_doubleTapWindow, () {
      if (!mounted) return;
      if (_lastTapAt != now) return;
      _lastTapAt = null;
      _lastTapX = null;
      widget.onTap();
    });
  }

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onTapUp: _handleTapUp,
      onVerticalDragStart: (details) {
        _dragging = true;
        final width = context.size?.width ?? 0;
        widget.onVerticalDragStart?.call(details.localPosition.dx < width / 2);
      },
      onVerticalDragUpdate: (details) {
        final width = context.size?.width ?? 0;
        final brightness = details.localPosition.dx < width / 2;
        // 向上为正（增大），按屏幕高度归一。
        final height = context.size?.height ?? 1;
        widget.onVerticalDrag(
          brightness: brightness,
          delta: -details.delta.dy / height,
        );
      },
      onVerticalDragEnd: (_) => _dragging = false,
      onHorizontalDragStart: widget.onHorizontalSeek == null
          ? null
          : (_) => _dragging = true,
      onHorizontalDragUpdate: widget.onHorizontalSeek == null
          ? null
          : (details) {
              final width = context.size?.width ?? 1;
              widget.onHorizontalSeek!(
                (details.localPosition.dx / width).clamp(0.0, 1.0),
              );
            },
      onHorizontalDragEnd: widget.onHorizontalSeek == null
          ? null
          : (_) {
              _dragging = false;
              widget.onHorizontalSeekEnd?.call();
            },
      child: AbsorbPointer(absorbing: _dragging, child: widget.child),
    );
  }
}
