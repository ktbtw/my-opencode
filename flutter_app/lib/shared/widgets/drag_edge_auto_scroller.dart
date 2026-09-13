import 'dart:async';

import 'package:flutter/material.dart';

/// Continuously scrolls a viewport while a dragged item stays near an edge.
class DragEdgeAutoScroller {
  DragEdgeAutoScroller({
    required this.controller,
    required this.viewportKey,
    this.edgeExtent = 88,
  });

  final ScrollController controller;
  final GlobalKey viewportKey;
  final double edgeExtent;

  Timer? _timer;
  double _pixelsPerTick = 0;

  void update(Offset globalPosition) {
    final viewportContext = viewportKey.currentContext;
    final renderObject = viewportContext?.findRenderObject();
    if (!controller.hasClients || renderObject is! RenderBox) {
      stop();
      return;
    }

    final localPosition = renderObject.globalToLocal(globalPosition);
    final height = renderObject.size.height;
    final activeExtent = edgeExtent.clamp(1.0, height / 3).toDouble();

    if (localPosition.dy < activeExtent) {
      _setVelocity(-_velocityForDistance(localPosition.dy, activeExtent));
    } else if (localPosition.dy > height - activeExtent) {
      _setVelocity(
        _velocityForDistance(height - localPosition.dy, activeExtent),
      );
    } else {
      stop();
    }
  }

  double _velocityForDistance(double distance, double extent) {
    final proximity = (1 - distance / extent).clamp(0.0, 1.0);
    return 3 + (15 * proximity * proximity);
  }

  void _setVelocity(double pixelsPerTick) {
    _pixelsPerTick = pixelsPerTick;
    _timer ??= Timer.periodic(
      const Duration(milliseconds: 16),
      (_) => _scrollOneFrame(),
    );
  }

  void _scrollOneFrame() {
    if (!controller.hasClients || _pixelsPerTick == 0) {
      stop();
      return;
    }
    final position = controller.position;
    final target = (controller.offset + _pixelsPerTick).clamp(
      position.minScrollExtent,
      position.maxScrollExtent,
    );
    if ((target - controller.offset).abs() < 0.01) return;
    controller.jumpTo(target.toDouble());
  }

  void stop() {
    _pixelsPerTick = 0;
    _timer?.cancel();
    _timer = null;
  }

  void dispose() => stop();
}
