import 'package:flutter/material.dart';
import 'package:video_player/video_player.dart';

import '../../../../core/theme/app_colors.dart';
import '../../../../core/theme/app_theme.dart';
import 'player_icons.dart';

/// 底部控制栏：进度条 + 播放控制。
///
/// 移动端与桌面端共用同一套控件，桌面端多一个音量滑条、且控制栏常显。
class VideoPlayerControls extends StatelessWidget {
  final VideoPlayerController controller;
  final bool isDesktop;
  final double speed;
  final bool looping;
  final bool speedMenuOpen;
  final Duration? scrubPosition;
  final ValueChanged<Duration> onScrub;
  final ValueChanged<Duration> onScrubEnd;
  final VoidCallback onTogglePlay;
  final ValueChanged<int> onSkip;
  final VoidCallback onToggleSpeedMenu;
  final VoidCallback onToggleFullscreen;
  final bool fullscreen;
  final ValueChanged<double> onVolume;

  const VideoPlayerControls({
    super.key,
    required this.controller,
    required this.isDesktop,
    required this.speed,
    required this.looping,
    required this.speedMenuOpen,
    required this.scrubPosition,
    required this.onScrub,
    required this.onScrubEnd,
    required this.onTogglePlay,
    required this.onSkip,
    required this.onToggleSpeedMenu,
    required this.onToggleFullscreen,
    required this.fullscreen,
    required this.onVolume,
  });

  static String formatDuration(Duration value) {
    // 负值（例如拖动越界）要显示成 00:00，不能靠取模得到 00:55 这种假时间。
    final total = value.inSeconds < 0 ? 0 : value.inSeconds;
    final minutes = (total ~/ 60).toString().padLeft(2, '0');
    final seconds = (total % 60).toString().padLeft(2, '0');
    return '$minutes:$seconds';
  }

  /// 支持秒数、分:秒与时:分:秒；拒绝溢出的分秒和负数。
  static Duration? parseTimestamp(String input) {
    final parts = input.trim().split(':');
    if (parts.isEmpty ||
        parts.length > 3 ||
        parts.any((part) => !RegExp(r'^\d+$').hasMatch(part))) {
      return null;
    }
    final numbers = parts.map(int.tryParse).toList();
    if (numbers.any((value) => value == null)) return null;
    if (numbers.skip(1).any((value) => value! >= 60)) return null;
    var seconds = 0;
    for (final number in numbers) {
      seconds = seconds * 60 + number!;
      if (seconds > 31536000) return null;
    }
    return Duration(seconds: seconds);
  }

  @override
  Widget build(BuildContext context) {
    final value = controller.value;
    final duration = value.duration;
    final position = scrubPosition ?? value.position;
    final progress = duration.inMilliseconds == 0
        ? 0.0
        : (position.inMilliseconds / duration.inMilliseconds).clamp(0.0, 1.0);
    final buffered = duration.inMilliseconds == 0
        ? 0.0
        : (value.buffered.isEmpty
                  ? 0.0
                  : value.buffered.last.end.inMilliseconds /
                        duration.inMilliseconds)
              .clamp(0.0, 1.0);

    final bottom = MediaQuery.of(context).padding.bottom;
    final buttonSize = isDesktop
        ? PlayerMetrics.touchTargetDesktop
        : PlayerMetrics.touchTarget;

    return Align(
      alignment: Alignment.bottomCenter,
      child: Container(
        padding: EdgeInsets.fromLTRB(12, 40, 12, bottom + 13),
        decoration: const BoxDecoration(
          gradient: LinearGradient(
            begin: Alignment.bottomCenter,
            end: Alignment.topCenter,
            colors: [Color(0xE604080E), Color(0x6B04080E), Color(0x0004080E)],
            stops: [0, 0.46, 1],
          ),
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (!isDesktop)
              Align(
                alignment: Alignment.centerLeft,
                child: _TimeLabel(
                  position: formatDuration(position),
                  duration: formatDuration(duration),
                ),
              ),
            _SeekBar(
              progress: progress,
              buffered: buffered,
              duration: duration,
              scrubPosition: scrubPosition,
              onScrub: onScrub,
              onScrubEnd: onScrubEnd,
            ),
            Row(
              children: [
                _ControlButton(
                  icon: value.isPlaying
                      ? PlayerIcons.pause(PlayerMetrics.button, Colors.white)
                      : PlayerIcons.play(PlayerMetrics.button, Colors.white),
                  onTap: onTogglePlay,
                  size: buttonSize,
                ),
                _ControlButton(
                  icon: PlayerIcons.skipBack(
                    PlayerMetrics.button,
                    Colors.white,
                  ),
                  onTap: () => onSkip(-10),
                  size: buttonSize,
                  tooltip: '快退 10 秒',
                ),
                _ControlButton(
                  icon: PlayerIcons.skipForward(
                    PlayerMetrics.button,
                    Colors.white,
                  ),
                  onTap: () => onSkip(10),
                  size: buttonSize,
                  tooltip: '快进 10 秒',
                ),
                const SizedBox(width: 6),
                if (isDesktop)
                  _TimeLabel(
                    position: formatDuration(position),
                    duration: formatDuration(duration),
                  ),

                // 桌面端：进度条已在上方，这里放音量
                if (isDesktop) ...[
                  const SizedBox(width: 14),
                  Expanded(
                    child: _VolumeSlider(
                      volume: value.volume,
                      onChanged: onVolume,
                    ),
                  ),
                ] else
                  const Spacer(),

                _ControlButton(
                  label:
                      '${speed % 1 == 0 ? speed.toStringAsFixed(1) : speed}×',
                  active: speedMenuOpen || (speed - 1).abs() > 0.001,
                  onTap: onToggleSpeedMenu,
                  size: buttonSize,
                  tooltip: '播放速度',
                ),
                _ControlButton(
                  icon: fullscreen
                      ? PlayerIcons.fullscreenExit(
                          PlayerMetrics.button,
                          Colors.white,
                        )
                      : PlayerIcons.fullscreen(
                          PlayerMetrics.button,
                          Colors.white,
                        ),
                  onTap: onToggleFullscreen,
                  size: buttonSize,
                  tooltip: fullscreen ? '退出全屏' : '全屏',
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

/// 进度条：缓冲段 + 已播段 + 拖动时间气泡。
class _SeekBar extends StatefulWidget {
  final double progress;
  final double buffered;
  final Duration duration;
  final Duration? scrubPosition;
  final ValueChanged<Duration> onScrub;
  final ValueChanged<Duration> onScrubEnd;

  const _SeekBar({
    required this.progress,
    required this.buffered,
    required this.duration,
    required this.scrubPosition,
    required this.onScrub,
    required this.onScrubEnd,
  });

  @override
  State<_SeekBar> createState() => _SeekBarState();
}

class _SeekBarState extends State<_SeekBar> {
  bool _dragging = false;

  void _update(double dx, double width) {
    if (widget.duration <= Duration.zero) return;
    final ratio = (dx / width).clamp(0.0, 1.0);
    widget.onScrub(
      Duration(milliseconds: (widget.duration.inMilliseconds * ratio).round()),
    );
  }

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final width = constraints.maxWidth;
        return MouseRegion(
          cursor: SystemMouseCursors.click,
          child: GestureDetector(
            behavior: HitTestBehavior.opaque,
            onHorizontalDragStart: (details) {
              setState(() => _dragging = true);
              _update(details.localPosition.dx, width);
            },
            onHorizontalDragUpdate: (details) =>
                _update(details.localPosition.dx, width),
            onHorizontalDragEnd: (_) {
              setState(() => _dragging = false);
              if (widget.scrubPosition != null) {
                widget.onScrubEnd(widget.scrubPosition!);
              }
            },
            onTapDown: (details) => _update(details.localPosition.dx, width),
            onTapUp: (_) {
              if (widget.scrubPosition != null) {
                widget.onScrubEnd(widget.scrubPosition!);
              }
            },
            child: SizedBox(
              height: 24,
              child: Stack(
                clipBehavior: Clip.none,
                alignment: Alignment.centerLeft,
                children: [
                  // 轨道
                  Container(
                    height: _dragging ? 6 : 4,
                    decoration: BoxDecoration(
                      color: Colors.white.withValues(alpha: 0.10),
                      borderRadius: BorderRadius.circular(999),
                    ),
                  ),
                  // 缓冲
                  FractionallySizedBox(
                    widthFactor: widget.buffered,
                    child: Container(
                      height: _dragging ? 6 : 4,
                      decoration: BoxDecoration(
                        color: Colors.white.withValues(alpha: 0.24),
                        borderRadius: BorderRadius.circular(999),
                      ),
                    ),
                  ),
                  // 已播
                  FractionallySizedBox(
                    widthFactor: widget.progress,
                    child: Container(
                      height: _dragging ? 6 : 4,
                      decoration: BoxDecoration(
                        color: AppColors.primary,
                        borderRadius: BorderRadius.circular(999),
                      ),
                    ),
                  ),
                  // 拖动手柄
                  Positioned(
                    left: width * widget.progress - 6.5,
                    child: AnimatedOpacity(
                      opacity: _dragging ? 1 : 0,
                      duration: const Duration(milliseconds: 150),
                      child: Container(
                        width: 13,
                        height: 13,
                        decoration: BoxDecoration(
                          color: Colors.white,
                          shape: BoxShape.circle,
                          boxShadow: [
                            BoxShadow(
                              color: AppColors.primary.withValues(alpha: 0.30),
                              spreadRadius: 4,
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                  // 时间气泡
                  if (_dragging && widget.scrubPosition != null)
                    Positioned(
                      left: (width * widget.progress - 26).clamp(
                        0.0,
                        width - 52,
                      ),
                      top: -26,
                      child: Container(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 8,
                          vertical: 4,
                        ),
                        decoration: BoxDecoration(
                          color: const Color(0xEB080C14),
                          borderRadius: BorderRadius.circular(AppRadius.sm),
                          border: Border.all(
                            color: Colors.white.withValues(alpha: 0.12),
                          ),
                        ),
                        child: Text(
                          VideoPlayerControls.formatDuration(
                            widget.scrubPosition!,
                          ),
                          style: const TextStyle(
                            color: Colors.white,
                            fontSize: 11,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                      ),
                    ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }
}

class _ControlButton extends StatelessWidget {
  final Widget? icon;
  final String? label;
  final VoidCallback onTap;
  final double size;
  final bool active;
  final String? tooltip;

  const _ControlButton({
    required this.onTap,
    required this.size,
    this.icon,
    this.label,
    this.active = false,
    this.tooltip,
  });

  @override
  Widget build(BuildContext context) {
    final button = Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(AppRadius.sm),
        child: SizedBox(
          height: size,
          width: label == null ? size : null,
          child: Padding(
            padding: label == null
                ? EdgeInsets.zero
                : const EdgeInsets.symmetric(horizontal: 7),
            child: Center(
              child:
                  icon ??
                  Text(
                    label!,
                    style: TextStyle(
                      color: active ? AppColors.primaryMuted : Colors.white,
                      fontSize: 11.5,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
            ),
          ),
        ),
      ),
    );
    return tooltip == null ? button : Tooltip(message: tooltip!, child: button);
  }
}

class _TimeLabel extends StatelessWidget {
  final String position;
  final String duration;

  const _TimeLabel({required this.position, required this.duration});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 6),
      child: Text.rich(
        TextSpan(
          children: [
            TextSpan(
              text: position,
              style: const TextStyle(color: Colors.white, fontSize: 11.5),
            ),
            const TextSpan(
              text: ' / ',
              style: TextStyle(color: Colors.white24, fontSize: 11.5),
            ),
            TextSpan(
              text: duration,
              style: const TextStyle(color: Colors.white54, fontSize: 11.5),
            ),
          ],
        ),
      ),
    );
  }
}

class _VolumeSlider extends StatelessWidget {
  final double volume;
  final ValueChanged<double> onChanged;

  const _VolumeSlider({required this.volume, required this.onChanged});

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        PlayerIcons.volume(PlayerMetrics.compact, Colors.white),
        const SizedBox(width: 8),
        Expanded(
          child: SliderTheme(
            data: SliderTheme.of(context).copyWith(
              trackHeight: 3,
              activeTrackColor: Colors.white,
              inactiveTrackColor: Colors.white.withValues(alpha: 0.22),
              thumbColor: Colors.white,
              thumbShape: const RoundSliderThumbShape(enabledThumbRadius: 6),
              overlayShape: const RoundSliderOverlayShape(overlayRadius: 12),
            ),
            child: Slider(value: volume.clamp(0, 1), onChanged: onChanged),
          ),
        ),
      ],
    );
  }
}
