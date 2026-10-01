import 'package:flutter/material.dart';

/// 播放器图标：统一 24 视口、同一笔画风格，避免各处尺寸 / 粗细不一致。
///
/// 快退 / 快进采用标准「撤销 / 重做」造型，方向不会看错：
/// 快退 = 逆时针朝左，快进 = 顺时针朝右。
class PlayerIcons {
  PlayerIcons._();

  static Widget play(double size, Color color) =>
      Icon(Icons.play_arrow_rounded, size: size, color: color);

  static Widget pause(double size, Color color) =>
      Icon(Icons.pause_rounded, size: size, color: color);

  /// 快退 10 秒：撤销形，箭头朝左。
  static Widget skipBack(double size, Color color) =>
      Icon(Icons.replay_rounded, size: size, color: color);

  /// 快进 10 秒：重做形，箭头朝右。
  static Widget skipForward(double size, Color color) =>
      Icon(Icons.refresh_rounded, size: size, color: color);

  /// 播放结束后的重播。
  static Widget replay(double size, Color color) =>
      Icon(Icons.replay_rounded, size: size, color: color);

  static Widget fullscreen(double size, Color color) =>
      Icon(Icons.fullscreen_rounded, size: size, color: color);

  static Widget fullscreenExit(double size, Color color) =>
      Icon(Icons.fullscreen_exit_rounded, size: size, color: color);

  static Widget back(double size, Color color) =>
      Icon(Icons.chevron_left_rounded, size: size, color: color);

  static Widget save(double size, Color color) =>
      Icon(Icons.download_rounded, size: size, color: color);

  static Widget volume(double size, Color color) =>
      Icon(Icons.volume_up_rounded, size: size, color: color);

  static Widget mute(double size, Color color) =>
      Icon(Icons.volume_off_rounded, size: size, color: color);

  static Widget snapshot(double size, Color color) =>
      Icon(Icons.photo_camera_outlined, size: size, color: color);

  static Widget loop(double size, Color color) =>
      Icon(Icons.repeat_rounded, size: size, color: color);

  static Widget error(double size, Color color) =>
      Icon(Icons.error_outline_rounded, size: size, color: color);
}

/// 图标尺寸统一档位：整个播放器只用这几个值。
class PlayerMetrics {
  PlayerMetrics._();

  /// 行内图标（与 12–13px 文字同行）。
  static const double inline = 14;

  /// 紧凑图标（弹层行、提示、状态缩略图）。
  static const double compact = 16;

  /// 按钮图标（顶栏、控制栏）。
  static const double button = 18;

  /// 主播放键（移动端画面中央）。
  static const double hero = 24;

  /// 主播放键（桌面端画面更大）。
  static const double heroLarge = 28;

  /// 按钮触控目标。
  static const double touchTarget = 32;

  /// 按钮触控目标（桌面端）。
  static const double touchTargetDesktop = 34;
}
