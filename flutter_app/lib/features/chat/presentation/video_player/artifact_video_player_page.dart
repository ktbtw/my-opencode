import 'dart:async';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';
import 'package:screen_brightness/screen_brightness.dart';
import 'package:share_plus/share_plus.dart';
import 'package:video_player/video_player.dart';
import 'package:volume_controller/volume_controller.dart';

import '../../../../core/notifications/app_notification_feedback.dart';
import '../../../../core/theme/app_colors.dart';
import '../../../../core/theme/app_theme.dart';
import '../../data/artifact_download_service.dart';
import '../../data/chat_model.dart';
import 'player_gesture_layer.dart';
import 'player_icons.dart';
import 'player_progress_store.dart';
import 'video_player_controls.dart';

/// 应用内播放生成的视频。
///
/// 视频源走我们自己的交付文件下载通道（带鉴权），不打开上游中转站的
/// 临时地址——那种地址需要鉴权且会过期，浏览器直接打开会 404。
class ArtifactVideoPlayerPage extends StatefulWidget {
  final String taskId;
  final AiArtifact artifact;

  /// 同一会话里可播放的其他视频，用于「下一个」。
  final List<AiArtifact> playlist;
  final int initialIndex;

  const ArtifactVideoPlayerPage({
    super.key,
    required this.taskId,
    required this.artifact,
    this.playlist = const [],
    this.initialIndex = 0,
  });

  @override
  State<ArtifactVideoPlayerPage> createState() =>
      _ArtifactVideoPlayerPageState();
}

class _ArtifactVideoPlayerPageState extends State<ArtifactVideoPlayerPage> {
  VideoPlayerController? _controller;
  String _error = '';
  bool _initializing = true;

  late int _index;

  /// 控制栏显隐与自动隐藏。
  bool _chromeVisible = true;
  Timer? _hideTimer;

  bool _buffering = false;
  bool _ended = false;

  /// 播放速度与循环。
  double _speed = 1;
  bool _looping = false;
  bool _speedMenuOpen = false;

  /// 画面比例：适应 / 填充 / 原始。
  BoxFit _fit = BoxFit.contain;

  /// 全屏。
  bool _fullscreen = false;

  /// 拖动进度条时的预览位置。
  Duration? _scrubPosition;

  /// 竖向手势提示。
  _VerticalHud? _verticalHud;
  Timer? _verticalHudTimer;

  /// 横向拖动进度（手势）。
  Duration? _gestureSeek;

  /// 画面所在边界：截取当前帧时从这里抓图。
  final GlobalKey _stageKey = GlobalKey();

  /// 原始亮度，退出时恢复。
  double? _originalBrightness;

  @override
  void initState() {
    super.initState();
    _index = widget.initialIndex.clamp(
      0,
      _playlist.isEmpty ? 0 : _playlist.length - 1,
    );
    _open();
  }

  List<AiArtifact> get _playlist =>
      widget.playlist.isEmpty ? [widget.artifact] : widget.playlist;

  AiArtifact get _current => _playlist[_index.clamp(0, _playlist.length - 1)];

  bool get _hasNext => _playlist.length > 1 && _index < _playlist.length - 1;

  Future<void> _open() async {
    final controller = VideoPlayerController.networkUrl(
      Uri.parse(ArtifactDownloadService.downloadUrl(widget.taskId, _current)),
      httpHeaders: ArtifactDownloadService.authHeaders(),
    );
    try {
      await controller.initialize();
      if (!mounted) {
        await controller.dispose();
        return;
      }
      controller.addListener(_onTick);
      await controller.setLooping(_looping);
      await controller.setPlaybackSpeed(_speed);
      setState(() {
        _controller = controller;
        _initializing = false;
        _error = '';
      });
      await _restorePosition(controller);
      await controller.play();
      _scheduleHide();
    } catch (error) {
      await controller.dispose();
      if (!mounted) return;
      setState(() {
        _initializing = false;
        _error = _friendlyError(error);
      });
    }
  }

  /// 续播：回到上次离开的位置（接近结尾则从头开始）。
  Future<void> _restorePosition(VideoPlayerController controller) async {
    final saved = await PlayerProgressStore.read(widget.taskId, _current.id);
    if (saved == null || saved <= Duration.zero) return;
    final duration = controller.value.duration;
    if (duration > Duration.zero &&
        saved >= duration - const Duration(seconds: 3)) {
      return;
    }
    await controller.seekTo(saved);
  }

  /// 不把底层异常原文直接抛给用户。
  String _friendlyError(Object error) {
    final text = error.toString().replaceFirst(RegExp(r'^Exception:\s*'), '');
    if (text.contains('404') || text.contains('not found')) {
      return '视频文件不存在或已被清理，请重新生成一次';
    }
    if (text.contains('401') || text.contains('403')) {
      return '没有权限读取该视频，请重新登录后再试';
    }
    if (text.contains('Connection') ||
        text.contains('SocketException') ||
        text.contains('timed out')) {
      return '网络连接失败，请检查网络后重试';
    }
    return '视频加载失败，请稍后重试';
  }

  /// 只在「缓冲」「播放结束」这类低频状态变化时重建整页；
  /// 进度 / 时间交给 ValueListenableBuilder 局部重建。
  void _onTick() {
    final controller = _controller;
    if (controller == null || !mounted) return;
    final value = controller.value;
    final buffering = value.isBuffering;
    final ended =
        value.duration > Duration.zero && value.position >= value.duration;
    if (buffering == _buffering && ended == _ended) return;
    setState(() {
      _buffering = buffering;
      _ended = ended;
      if (ended && !_chromeVisible) {
        _chromeVisible = true;
        _hideTimer?.cancel();
      }
    });
  }

  @override
  void dispose() {
    _hideTimer?.cancel();
    _verticalHudTimer?.cancel();
    final position = _controller?.value.position;
    if (position != null) {
      PlayerProgressStore.write(widget.taskId, _current.id, position);
    }
    _controller?.removeListener(_onTick);
    _controller?.dispose();
    if (_fullscreen) {
      SystemChrome.setPreferredOrientations(DeviceOrientation.values);
      SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
    }
    final brightness = _originalBrightness;
    if (brightness != null) {
      ScreenBrightness().resetApplicationScreenBrightness();
    }
    super.dispose();
  }

  // ---------------------------------------------------------------- 控制栏

  bool get _isDesktop => MediaQuery.of(context).size.width >= AppBreakpoints.md;

  void _scheduleHide() {
    _hideTimer?.cancel();
    if (_isDesktop) return; // 桌面端控制栏常显
    if (_controller?.value.isPlaying != true) return;
    _hideTimer = Timer(const Duration(seconds: 3), () {
      if (!mounted) return;
      setState(() {
        _chromeVisible = false;
        _speedMenuOpen = false;
      });
    });
  }

  void _showChrome() {
    setState(() => _chromeVisible = true);
    _scheduleHide();
  }

  void _togglePlay() {
    final controller = _controller;
    if (controller == null) return;
    final value = controller.value;
    final ended =
        value.duration > Duration.zero && value.position >= value.duration;
    setState(() {
      if (ended) {
        controller.seekTo(Duration.zero);
        controller.play();
      } else if (value.isPlaying) {
        controller.pause();
      } else {
        controller.play();
      }
    });
    _scheduleHide();
  }

  void _seekRelative(int seconds) {
    final controller = _controller;
    if (controller == null) return;
    final duration = controller.value.duration;
    var target = controller.value.position + Duration(seconds: seconds);
    if (target < Duration.zero) target = Duration.zero;
    if (duration > Duration.zero && target > duration) target = duration;
    controller.seekTo(target);
    _flashHud(seconds);
    _showChrome();
  }

  Future<void> _toggleFullscreen() async {
    final next = !_fullscreen;
    setState(() => _fullscreen = next);
    if (next) {
      await SystemChrome.setPreferredOrientations([
        DeviceOrientation.landscapeLeft,
        DeviceOrientation.landscapeRight,
      ]);
      await SystemChrome.setEnabledSystemUIMode(SystemUiMode.immersiveSticky);
    } else {
      await SystemChrome.setPreferredOrientations(DeviceOrientation.values);
      await SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
    }
    _showChrome();
  }

  void _setSpeed(double speed) {
    setState(() {
      _speed = speed;
      _speedMenuOpen = false;
    });
    _controller?.setPlaybackSpeed(speed);
    _showChrome();
  }

  void _toggleLoop() {
    setState(() => _looping = !_looping);
    _controller?.setLooping(_looping);
  }

  void _cycleFit() {
    setState(() {
      _fit = switch (_fit) {
        BoxFit.contain => BoxFit.cover,
        BoxFit.cover => BoxFit.fill,
        _ => BoxFit.contain,
      };
    });
  }

  String get _fitLabel => switch (_fit) {
    BoxFit.contain => '适应',
    BoxFit.cover => '填充',
    _ => '拉伸',
  };

  Future<void> _save() async {
    _controller?.pause();
    showAppFeedback(context, message: '正在保存视频…');
    try {
      final path = await ArtifactDownloadService.download(
        taskId: widget.taskId,
        artifact: _current,
      );
      if (!mounted) return;
      showAppFeedback(
        context,
        message: path == null ? '已交给浏览器下载' : '已保存到 $path',
      );
    } catch (error) {
      if (!mounted) return;
      showAppFeedback(
        context,
        message: '保存失败：${_friendlyError(error)}',
        error: true,
      );
    }
  }

  /// 分享：先把视频落到临时文件，再交给系统分享面板。
  Future<void> _share() async {
    _controller?.pause();
    showAppFeedback(context, message: '正在准备分享…');
    try {
      final bytes = await ArtifactDownloadService.fetchBytes(
        taskId: widget.taskId,
        artifact: _current,
      );
      final dir = await getTemporaryDirectory();
      final file = File(p.join(dir.path, _current.filename));
      await file.writeAsBytes(bytes, flush: true);
      await SharePlus.instance.share(
        ShareParams(files: [XFile(file.path, mimeType: _current.mimeType)]),
      );
    } catch (error) {
      if (!mounted) return;
      showAppFeedback(
        context,
        message: '分享失败：${_friendlyError(error)}',
        error: true,
      );
    }
  }

  /// 截取当前帧为图片并保存。
  ///
  /// 从画面所在的 RepaintBoundary 抓取，得到的是用户此刻看到的那一帧。
  Future<void> _captureFrame() async {
    final boundary =
        _stageKey.currentContext?.findRenderObject() as RenderRepaintBoundary?;
    if (boundary == null) {
      showAppFeedback(context, message: '画面还没准备好，请稍后再试', error: true);
      return;
    }
    showAppFeedback(context, message: '正在截取当前帧…');
    try {
      final image = await boundary.toImage(pixelRatio: 2);
      final data = await image.toByteData(format: ui.ImageByteFormat.png);
      if (data == null) throw StateError('截帧失败');
      final name = _current.filename.replaceAll(RegExp(r'\.[^.]+$'), '');
      final path = await _saveBytes(
        data.buffer.asUint8List(),
        '$name-${DateTime.now().millisecondsSinceEpoch}.png',
      );
      if (!mounted) return;
      showAppFeedback(context, message: '已保存当前帧到 $path');
    } catch (error) {
      if (!mounted) return;
      showAppFeedback(
        context,
        message: '截取当前帧失败：${_friendlyError(error)}',
        error: true,
      );
    }
  }

  Future<String> _saveBytes(Uint8List bytes, String filename) async {
    final dir = await getApplicationDocumentsDirectory();
    final file = File(p.join(dir.path, filename));
    await file.writeAsBytes(bytes, flush: true);
    return file.path;
  }

  /// 画中画：Android 走原生入口，其它平台提示不支持。
  Future<void> _enterPip() async {
    if (!Platform.isAndroid) {
      showAppFeedback(context, message: '当前平台暂不支持画中画');
      return;
    }
    try {
      final supported =
          await _pipChannel.invokeMethod<bool>('isSupported') ?? false;
      if (!supported) {
        showAppFeedback(context, message: '当前设备暂不支持画中画');
        return;
      }
      await _pipChannel.invokeMethod<bool>('enter');
    } catch (_) {
      if (!mounted) return;
      showAppFeedback(context, message: '进入画中画失败', error: true);
    }
  }

  static const MethodChannel _pipChannel = MethodChannel('chat_codex/pip');

  // ------------------------------------------------------------ 键盘快捷键

  KeyEventResult _onKey(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent) return KeyEventResult.ignored;
    switch (event.logicalKey) {
      case LogicalKeyboardKey.space:
      case LogicalKeyboardKey.mediaPlayPause:
        _togglePlay();
        return KeyEventResult.handled;
      case LogicalKeyboardKey.arrowLeft:
        _seekRelative(-5);
        return KeyEventResult.handled;
      case LogicalKeyboardKey.arrowRight:
        _seekRelative(5);
        return KeyEventResult.handled;
      case LogicalKeyboardKey.arrowUp:
        _adjustVolume(0.05);
        return KeyEventResult.handled;
      case LogicalKeyboardKey.arrowDown:
        _adjustVolume(-0.05);
        return KeyEventResult.handled;
      case LogicalKeyboardKey.keyF:
        _toggleFullscreen();
        return KeyEventResult.handled;
      case LogicalKeyboardKey.keyM:
        final controller = _controller;
        if (controller != null) {
          setState(() {
            controller.setVolume(controller.value.volume > 0 ? 0 : 1);
          });
        }
        return KeyEventResult.handled;
      case LogicalKeyboardKey.escape:
        if (_fullscreen) {
          _toggleFullscreen();
          return KeyEventResult.handled;
        }
        return KeyEventResult.ignored;
      default:
        return KeyEventResult.ignored;
    }
  }

  // -------------------------------------------------------------- 竖向手势

  void _handleVerticalDrag({required bool brightness, required double delta}) {
    if (brightness) {
      // 亮度与音量各自独立，不能共用一个值，否则先调亮度再调音量会串。
      final next = ((_brightnessValue ?? 0.5) + delta).clamp(0.0, 1.0);
      _brightnessValue = next;
      ScreenBrightness()
          .setApplicationScreenBrightness(next)
          .catchError((_) {});
      _showVerticalHud(_VerticalKind.brightness, next);
      return;
    }
    final next = ((_volumeValue ?? 0.5) + delta).clamp(0.0, 1.0);
    _volumeValue = next;
    VolumeController.instance.setVolume(next).catchError((_) {});
    _showVerticalHud(_VerticalKind.volume, next);
  }

  double? _brightnessValue;
  double? _volumeValue;

  void _adjustVolume(double delta) {
    final controller = _controller;
    if (controller == null) return;
    final next = (controller.value.volume + delta).clamp(0.0, 1.0);
    controller.setVolume(next);
    _volumeValue = next;
    _showVerticalHud(_VerticalKind.volume, next);
  }

  void _showVerticalHud(_VerticalKind kind, double value) {
    setState(() => _verticalHud = _VerticalHud(kind: kind, value: value));
    _verticalHudTimer?.cancel();
    _verticalHudTimer = Timer(const Duration(milliseconds: 900), () {
      if (mounted) setState(() => _verticalHud = null);
    });
  }

  /// 竖向手势开始时，先取到当前真实的亮度 / 音量作为基准。
  Future<void> _primeVerticalValue(_VerticalKind kind) async {
    try {
      if (kind == _VerticalKind.brightness) {
        final value = await ScreenBrightness().application;
        _brightnessValue = value;
        _originalBrightness ??= value;
        return;
      }
      _volumeValue = await VolumeController.instance.getVolume();
    } catch (_) {
      // 取不到就退回 0.5 基准，不影响手势可用。
    }
  }

  // -------------------------------------------------------------- 手势反馈

  int _hudSeconds = 0;
  bool _hudForward = true;
  Timer? _hudTimer;
  int? get _hud => _hudSeconds == 0 ? null : _hudSeconds;

  void _flashHud(int seconds) {
    setState(() {
      _hudSeconds = seconds.abs();
      _hudForward = seconds > 0;
    });
    _hudTimer?.cancel();
    _hudTimer = Timer(const Duration(milliseconds: 760), () {
      if (mounted) setState(() => _hudSeconds = 0);
    });
  }

  // ------------------------------------------------------------------ 构建

  @override
  Widget build(BuildContext context) {
    final controller = _controller;
    return Scaffold(
      backgroundColor: Colors.black,
      body: Focus(
        autofocus: true,
        onKeyEvent: _onKey,
        child: _initializing
            ? const Center(
                child: SizedBox(
                  width: 30,
                  height: 30,
                  child: CircularProgressIndicator(
                    strokeWidth: 2.5,
                    color: Colors.white,
                  ),
                ),
              )
            : (_error.isNotEmpty
                  ? _ErrorView(message: _error, onRetry: _retry)
                  : _buildPlayer(controller!)),
      ),
    );
  }

  Future<void> _retry() async {
    setState(() {
      _initializing = true;
      _error = '';
    });
    await _open();
  }

  Widget _buildPlayer(VideoPlayerController controller) {
    if (_isDesktop) return _buildDesktopPlayer(controller);
    return LayoutBuilder(
      builder: (context, constraints) {
        return Stack(
          fit: StackFit.expand,
          children: [
            PlayerGestureLayer(
              onTap: () {
                if (!_chromeVisible) {
                  _showChrome();
                } else {
                  setState(() => _chromeVisible = false);
                  _hideTimer?.cancel();
                }
              },
              onDoubleTapSeek: _seekRelative,
              onVerticalDrag: _handleVerticalDrag,
              onVerticalDragStart: (brightness) => _primeVerticalValue(
                brightness ? _VerticalKind.brightness : _VerticalKind.volume,
              ),
              onHorizontalSeek: (ratio) {
                final duration = controller.value.duration;
                if (duration <= Duration.zero) return;
                setState(() {
                  _gestureSeek = Duration(
                    milliseconds: (duration.inMilliseconds * ratio).round(),
                  );
                });
              },
              onHorizontalSeekEnd: () {
                final target = _gestureSeek;
                if (target != null) controller.seekTo(target);
                setState(() => _gestureSeek = null);
              },
              child: RepaintBoundary(
                key: _stageKey,
                child: Center(
                  child: AspectRatio(
                    aspectRatio: _aspectRatio(controller),
                    child: FittedBox(
                      fit: _fit,
                      child: SizedBox(
                        width: controller.value.size.width,
                        height: controller.value.size.height,
                        child: VideoPlayer(controller),
                      ),
                    ),
                  ),
                ),
              ),
            ),

            if (_buffering) const _BufferingIndicator(),

            // 顶栏
            AnimatedOpacity(
              opacity: _chromeVisible ? 1 : 0,
              duration: const Duration(milliseconds: 220),
              child: IgnorePointer(
                ignoring: !_chromeVisible,
                child: _TopBar(
                  title: _current.filename,
                  subtitle: _metaLine(controller),
                  onBack: () => Navigator.of(context).maybePop(),
                  onSave: _save,
                  onShare: _share,
                  onCapture: _captureFrame,
                  onPip: _enterPip,
                  onFit: _cycleFit,
                  fitLabel: _fitLabel,
                  hasNext: _hasNext,
                  onNext: _playNext,
                ),
              ),
            ),

            // 中央主播放键 / 重播
            ValueListenableBuilder<VideoPlayerValue>(
              valueListenable: controller,
              builder: (context, value, _) => _CenterButton(
                heroSize: PlayerMetrics.hero,
                visible: !value.isPlaying,
                ended: _hasEnded(controller),
                onTap: _togglePlay,
              ),
            ),

            _GestureHud(
              visible: _hud != null,
              seconds: _hudSeconds,
              forward: _hudForward,
            ),

            if (_verticalHud != null) _VerticalHudView(hud: _verticalHud!),

            if (_gestureSeek != null)
              _SeekPreview(
                position: _gestureSeek!,
                duration: controller.value.duration,
              ),

            if (_speedMenuOpen)
              Positioned(
                right: 12,
                bottom: 96,
                child: _SpeedMenu(
                  speed: _speed,
                  looping: _looping,
                  onSelect: _setSpeed,
                  onToggleLoop: _toggleLoop,
                ),
              ),

            // 底部控制栏
            AnimatedOpacity(
              opacity: _chromeVisible ? 1 : 0,
              duration: const Duration(milliseconds: 220),
              child: IgnorePointer(
                ignoring: !_chromeVisible,
                child: ValueListenableBuilder<VideoPlayerValue>(
                  valueListenable: controller,
                  builder: (context, _, __) => VideoPlayerControls(
                    controller: controller,
                    isDesktop: _isDesktop,
                    speed: _speed,
                    looping: _looping,
                    speedMenuOpen: _speedMenuOpen,
                    scrubPosition: _scrubPosition,
                    onScrub: (value) => setState(() => _scrubPosition = value),
                    onScrubEnd: (value) {
                      controller.seekTo(value);
                      setState(() => _scrubPosition = null);
                      _showChrome();
                    },
                    onTogglePlay: _togglePlay,
                    onSkip: _seekRelative,
                    onToggleSpeedMenu: () {
                      setState(() => _speedMenuOpen = !_speedMenuOpen);
                      _showChrome();
                    },
                    onToggleFullscreen: _toggleFullscreen,
                    fullscreen: _fullscreen,
                    onVolume: (value) => controller.setVolume(value),
                  ),
                ),
              ),
            ),
          ],
        );
      },
    );
  }

  Future<void> _playNext() async {
    if (!_hasNext) return;
    final old = _controller;
    setState(() {
      _index += 1;
      _initializing = true;
    });
    old?.removeListener(_onTick);
    await old?.dispose();
    await _open();
  }

  double _aspectRatio(VideoPlayerController controller) {
    final ratio = controller.value.aspectRatio;
    return ratio == 0 ? 16 / 9 : ratio;
  }

  /// 桌面端：控制栏常显在画面下方，再下面是文件信息，不遮挡画面。
  Widget _buildDesktopPlayer(VideoPlayerController controller) {
    return Column(
      children: [
        Expanded(
          child: Stack(
            fit: StackFit.expand,
            children: [
              Container(color: Colors.black),
              RepaintBoundary(
                key: _stageKey,
                child: Center(
                  child: AspectRatio(
                    aspectRatio: _aspectRatio(controller),
                    child: FittedBox(
                      fit: _fit,
                      child: SizedBox(
                        width: controller.value.size.width,
                        height: controller.value.size.height,
                        child: VideoPlayer(controller),
                      ),
                    ),
                  ),
                ),
              ),
              if (_buffering) const _BufferingIndicator(),
              ValueListenableBuilder<VideoPlayerValue>(
                valueListenable: controller,
                builder: (context, value, _) => _CenterButton(
                  heroSize: PlayerMetrics.heroLarge,
                  visible: !value.isPlaying,
                  ended: _hasEnded(controller),
                  onTap: _togglePlay,
                ),
              ),
              _TopBar(
                title: _current.filename,
                subtitle: _metaLine(controller),
                onBack: () => Navigator.of(context).maybePop(),
                onSave: _save,
                onShare: _share,
                onCapture: _captureFrame,
                onPip: _enterPip,
                onFit: _cycleFit,
                fitLabel: _fitLabel,
                hasNext: _hasNext,
                onNext: _playNext,
              ),
              _GestureHud(
                visible: _hud != null,
                seconds: _hudSeconds,
                forward: _hudForward,
              ),
              if (_verticalHud != null) _VerticalHudView(hud: _verticalHud!),
              if (_speedMenuOpen)
                Positioned(
                  right: 12,
                  bottom: 96,
                  child: _SpeedMenu(
                    speed: _speed,
                    looping: _looping,
                    onSelect: _setSpeed,
                    onToggleLoop: _toggleLoop,
                  ),
                ),
            ],
          ),
        ),
        Container(
          color: const Color(0xFF0A0E14),
          child: ValueListenableBuilder<VideoPlayerValue>(
            valueListenable: controller,
            builder: (context, _, __) => VideoPlayerControls(
              controller: controller,
              isDesktop: true,
              speed: _speed,
              looping: _looping,
              speedMenuOpen: _speedMenuOpen,
              scrubPosition: _scrubPosition,
              onScrub: (value) => setState(() => _scrubPosition = value),
              onScrubEnd: (value) {
                controller.seekTo(value);
                setState(() => _scrubPosition = null);
              },
              onTogglePlay: _togglePlay,
              onSkip: _seekRelative,
              onToggleSpeedMenu: () =>
                  setState(() => _speedMenuOpen = !_speedMenuOpen),
              onToggleFullscreen: _toggleFullscreen,
              fullscreen: _fullscreen,
              onVolume: (value) => controller.setVolume(value),
            ),
          ),
        ),
        _DesktopMeta(artifact: _current, controller: controller),
      ],
    );
  }

  bool _hasEnded(VideoPlayerController controller) {
    final value = controller.value;
    return value.duration > Duration.zero &&
        value.position >= value.duration &&
        !value.isPlaying;
  }

  String _metaLine(VideoPlayerController controller) {
    final parts = <String>[];
    final size = controller.value.size;
    if (size.width > 0 && size.height > 0) {
      parts.add('${size.width.round()} × ${size.height.round()}');
    }
    final bytes = _current.sizeBytes;
    if (bytes > 0) parts.add(formatBytes(bytes));
    return parts.join(' · ');
  }
}

/// 字节数格式化（控制栏与文件信息共用）。
String formatBytes(int bytes) {
  if (bytes >= 1024 * 1024 * 1024) {
    return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(1)} GB';
  }
  if (bytes >= 1024 * 1024) {
    return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  }
  if (bytes >= 1024) return '${(bytes / 1024).toStringAsFixed(0)} KB';
  return '$bytes B';
}

enum _VerticalKind { brightness, volume }

class _VerticalHud {
  final _VerticalKind kind;
  final double value;
  const _VerticalHud({required this.kind, required this.value});
}

/// 上下滑时的竖向提示：图标 + 进度条 + 百分比。
class _VerticalHudView extends StatelessWidget {
  final _VerticalHud hud;

  const _VerticalHudView({required this.hud});

  @override
  Widget build(BuildContext context) {
    final brightness = hud.kind == _VerticalKind.brightness;
    return Center(
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 13, vertical: 11),
        decoration: BoxDecoration(
          color: Colors.black.withValues(alpha: 0.72),
          borderRadius: BorderRadius.circular(AppRadius.lg),
          border: Border.all(color: Colors.white.withValues(alpha: 0.10)),
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              brightness
                  ? Icons.brightness_6_rounded
                  : (hud.value <= 0
                        ? Icons.volume_off_rounded
                        : Icons.volume_up_rounded),
              size: PlayerMetrics.compact,
              color: Colors.white,
            ),
            const SizedBox(height: 7),
            Container(
              width: 5,
              height: 74,
              decoration: BoxDecoration(
                color: Colors.white.withValues(alpha: 0.20),
                borderRadius: BorderRadius.circular(999),
              ),
              child: Align(
                alignment: Alignment.bottomCenter,
                child: FractionallySizedBox(
                  heightFactor: hud.value.clamp(0.0, 1.0),
                  child: Container(
                    decoration: BoxDecoration(
                      color: Colors.white,
                      borderRadius: BorderRadius.circular(999),
                    ),
                  ),
                ),
              ),
            ),
            const SizedBox(height: 6),
            Text(
              '${(hud.value * 100).round()}%',
              style: const TextStyle(
                color: Colors.white,
                fontSize: 10.5,
                fontWeight: FontWeight.w700,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// 横向拖动进度时的时间提示。
class _SeekPreview extends StatelessWidget {
  final Duration position;
  final Duration duration;

  const _SeekPreview({required this.position, required this.duration});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 9),
        decoration: BoxDecoration(
          color: Colors.black.withValues(alpha: 0.68),
          borderRadius: BorderRadius.circular(AppRadius.lg),
          border: Border.all(color: Colors.white.withValues(alpha: 0.10)),
        ),
        child: Text(
          '${VideoPlayerControls.formatDuration(position)}'
          ' / ${VideoPlayerControls.formatDuration(duration)}',
          style: const TextStyle(
            color: Colors.white,
            fontSize: 12,
            fontWeight: FontWeight.w700,
          ),
        ),
      ),
    );
  }
}

/// 加载失败：中文原因 + 重试，不暴露底层异常。
class _ErrorView extends StatelessWidget {
  final String message;
  final Future<void> Function() onRetry;

  const _ErrorView({required this.message, required this.onRetry});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              width: 44,
              height: 44,
              decoration: BoxDecoration(
                color: AppColors.statusError.withValues(alpha: 0.16),
                borderRadius: BorderRadius.circular(AppRadius.md),
                border: Border.all(
                  color: AppColors.statusError.withValues(alpha: 0.4),
                ),
              ),
              child: Center(
                child: PlayerIcons.error(20, AppColors.statusErrorLight),
              ),
            ),
            const SizedBox(height: 16),
            Text(
              message,
              textAlign: TextAlign.center,
              style: const TextStyle(
                color: Colors.white,
                fontSize: 13,
                height: 1.6,
              ),
            ),
            const SizedBox(height: 18),
            FilledButton(
              onPressed: onRetry,
              style: FilledButton.styleFrom(
                backgroundColor: AppColors.primary,
                padding: const EdgeInsets.symmetric(
                  horizontal: 26,
                  vertical: 11,
                ),
                shape: RoundedRectangleBorder(
                  borderRadius: BorderRadius.circular(AppRadius.sm),
                ),
              ),
              child: const Text(
                '重试',
                style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// 桌面端文件信息：控制栏下方直接列出，便于核对。
class _DesktopMeta extends StatelessWidget {
  final AiArtifact artifact;
  final VideoPlayerController controller;

  const _DesktopMeta({required this.artifact, required this.controller});

  @override
  Widget build(BuildContext context) {
    final size = controller.value.size;
    final resolution = size.width > 0 && size.height > 0
        ? '${size.width.round()} × ${size.height.round()}'
        : '—';
    final duration = controller.value.duration;
    final entries = <(String, String)>[
      ('文件名', artifact.filename),
      (
        '时长',
        duration > Duration.zero
            ? VideoPlayerControls.formatDuration(duration)
            : '—',
      ),
      ('分辨率', resolution),
      ('大小', artifact.sizeBytes > 0 ? formatBytes(artifact.sizeBytes) : '—'),
      ('格式', artifact.mimeType.replaceFirst('video/', '').toUpperCase()),
    ];

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(16, 15, 16, 17),
      decoration: const BoxDecoration(
        color: Color(0xFF0A0E14),
        border: Border(top: BorderSide(color: Color(0x14FFFFFF))),
      ),
      child: Wrap(
        spacing: 26,
        runSpacing: 12,
        children: [
          for (final entry in entries)
            Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  entry.$1,
                  style: const TextStyle(
                    color: Colors.white38,
                    fontSize: 10.5,
                    fontWeight: FontWeight.w600,
                    letterSpacing: 0.4,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  entry.$2,
                  style: const TextStyle(
                    color: Colors.white,
                    fontSize: 12.5,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ],
            ),
        ],
      ),
    );
  }
}

class _BufferingIndicator extends StatelessWidget {
  const _BufferingIndicator();

  @override
  Widget build(BuildContext context) {
    return const IgnorePointer(
      child: Center(
        child: SizedBox(
          width: 30,
          height: 30,
          child: CircularProgressIndicator(
            strokeWidth: 2.5,
            color: Colors.white,
          ),
        ),
      ),
    );
  }
}

class _TopBar extends StatelessWidget {
  final String title;
  final String subtitle;
  final VoidCallback onBack;
  final VoidCallback onSave;
  final VoidCallback onShare;
  final VoidCallback onCapture;
  final VoidCallback onPip;
  final VoidCallback onFit;
  final String fitLabel;
  final bool hasNext;
  final VoidCallback onNext;

  const _TopBar({
    required this.title,
    required this.subtitle,
    required this.onBack,
    required this.onSave,
    required this.onShare,
    required this.onCapture,
    required this.onPip,
    required this.onFit,
    required this.fitLabel,
    required this.hasNext,
    required this.onNext,
  });

  @override
  Widget build(BuildContext context) {
    final top = MediaQuery.of(context).padding.top;
    return Align(
      alignment: Alignment.topCenter,
      child: Container(
        padding: EdgeInsets.fromLTRB(12, top + 10, 12, 22),
        decoration: const BoxDecoration(
          gradient: LinearGradient(
            begin: Alignment.topCenter,
            end: Alignment.bottomCenter,
            colors: [Color(0xCC04080E), Color(0x0004080E)],
          ),
        ),
        child: Row(
          children: [
            _ChromeButton(
              icon: PlayerIcons.back(PlayerMetrics.button, Colors.white),
              onTap: onBack,
              tooltip: '返回',
            ),
            const SizedBox(width: 9),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      color: Colors.white,
                      fontSize: 12.5,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  if (subtitle.isNotEmpty) ...[
                    const SizedBox(height: 2),
                    Text(
                      subtitle,
                      style: const TextStyle(
                        color: Colors.white54,
                        fontSize: 10.5,
                      ),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(width: 6),
            _ChromeButton(
              icon: PlayerIcons.save(PlayerMetrics.button, Colors.white),
              onTap: onSave,
              tooltip: '保存到本地',
            ),
            _MoreMenu(
              onShare: onShare,
              onCapture: onCapture,
              onPip: onPip,
              onFit: onFit,
              fitLabel: fitLabel,
              hasNext: hasNext,
              onNext: onNext,
            ),
          ],
        ),
      ),
    );
  }
}

/// 顶栏「更多」：分享、截帧、画中画、画面比例、下一个。
class _MoreMenu extends StatelessWidget {
  final VoidCallback onShare;
  final VoidCallback onCapture;
  final VoidCallback onPip;
  final VoidCallback onFit;
  final String fitLabel;
  final bool hasNext;
  final VoidCallback onNext;

  const _MoreMenu({
    required this.onShare,
    required this.onCapture,
    required this.onPip,
    required this.onFit,
    required this.fitLabel,
    required this.hasNext,
    required this.onNext,
  });

  @override
  Widget build(BuildContext context) {
    return PopupMenuButton<void>(
      tooltip: '更多',
      color: const Color(0xF50E141E),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AppRadius.md),
        side: BorderSide(color: Colors.white.withValues(alpha: 0.12)),
      ),
      icon: SizedBox(
        width: PlayerMetrics.touchTarget,
        height: PlayerMetrics.touchTarget,
        child: Center(
          child: Icon(
            Icons.more_vert_rounded,
            size: PlayerMetrics.button,
            color: Colors.white,
          ),
        ),
      ),
      itemBuilder: (context) => [
        _item(Icons.ios_share_rounded, '分享', onShare),
        _item(Icons.photo_camera_outlined, '截取当前帧', onCapture),
        _item(Icons.picture_in_picture_alt_rounded, '画中画', onPip),
        _item(Icons.aspect_ratio_rounded, '画面比例 · $fitLabel', onFit),
        if (hasNext) _item(Icons.skip_next_rounded, '下一个', onNext),
      ],
    );
  }

  PopupMenuItem<void> _item(IconData icon, String label, VoidCallback onTap) {
    return PopupMenuItem<void>(
      onTap: onTap,
      child: Row(
        children: [
          Icon(icon, size: PlayerMetrics.compact, color: Colors.white),
          const SizedBox(width: 10),
          Text(
            label,
            style: const TextStyle(color: Colors.white, fontSize: 12),
          ),
        ],
      ),
    );
  }
}

/// 顶栏 / 控制栏统一的方形按钮：固定触控目标，图标尺寸一致。
class _ChromeButton extends StatelessWidget {
  final Widget icon;
  final VoidCallback onTap;
  final String tooltip;

  const _ChromeButton({
    required this.icon,
    required this.onTap,
    required this.tooltip,
  });

  @override
  Widget build(BuildContext context) {
    return Tooltip(
      message: tooltip,
      child: Material(
        color: Colors.white.withValues(alpha: 0.10),
        borderRadius: BorderRadius.circular(AppRadius.sm),
        child: InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(AppRadius.sm),
          child: SizedBox(
            width: PlayerMetrics.touchTarget,
            height: PlayerMetrics.touchTarget,
            child: Center(child: icon),
          ),
        ),
      ),
    );
  }
}

class _CenterButton extends StatelessWidget {
  final double heroSize;
  final bool visible;
  final bool ended;
  final VoidCallback onTap;

  const _CenterButton({
    required this.heroSize,
    required this.visible,
    required this.ended,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    if (!visible) return const SizedBox.shrink();
    return Center(
      child: GestureDetector(
        onTap: onTap,
        child: Container(
          width: heroSize * 2.6,
          height: heroSize * 2.6,
          decoration: BoxDecoration(
            color: Colors.black.withValues(alpha: 0.42),
            shape: BoxShape.circle,
            border: Border.all(color: Colors.white.withValues(alpha: 0.42)),
          ),
          child: Center(
            child: ended
                ? PlayerIcons.replay(heroSize, Colors.white)
                : PlayerIcons.play(heroSize + 2, Colors.white),
          ),
        ),
      ),
    );
  }
}

class _GestureHud extends StatelessWidget {
  final bool visible;
  final int seconds;
  final bool forward;

  const _GestureHud({
    required this.visible,
    required this.seconds,
    required this.forward,
  });

  @override
  Widget build(BuildContext context) {
    if (!visible) return const SizedBox.shrink();
    return Align(
      alignment: forward ? Alignment.centerRight : Alignment.centerLeft,
      child: Padding(
        padding: EdgeInsets.only(
          left: forward ? 0 : 16,
          right: forward ? 16 : 0,
        ),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 13, vertical: 9),
          decoration: BoxDecoration(
            color: Colors.black.withValues(alpha: 0.68),
            borderRadius: BorderRadius.circular(999),
            border: Border.all(color: Colors.white.withValues(alpha: 0.10)),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (!forward) ...[
                PlayerIcons.skipBack(PlayerMetrics.inline, Colors.white),
                const SizedBox(width: 6),
              ],
              Text(
                '$seconds 秒',
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 11.5,
                  fontWeight: FontWeight.w600,
                ),
              ),
              if (forward) ...[
                const SizedBox(width: 6),
                PlayerIcons.skipForward(PlayerMetrics.inline, Colors.white),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// 倍速与循环。
class _SpeedMenu extends StatelessWidget {
  final double speed;
  final bool looping;
  final ValueChanged<double> onSelect;
  final VoidCallback onToggleLoop;

  const _SpeedMenu({
    required this.speed,
    required this.looping,
    required this.onSelect,
    required this.onToggleLoop,
  });

  static const _rates = [0.5, 0.75, 1.0, 1.25, 1.5, 2.0];

  String _label(double value) =>
      '${value % 1 == 0 ? value.toStringAsFixed(1) : value}×';

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.transparent,
      child: Container(
        constraints: const BoxConstraints(minWidth: 132),
        padding: const EdgeInsets.all(6),
        decoration: BoxDecoration(
          color: const Color(0xF50E141E),
          borderRadius: BorderRadius.circular(AppRadius.md),
          border: Border.all(color: Colors.white.withValues(alpha: 0.12)),
          boxShadow: const [
            BoxShadow(
              color: Color(0x7A000000),
              blurRadius: 22,
              offset: Offset(0, 8),
            ),
          ],
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Padding(
              padding: EdgeInsets.fromLTRB(9, 5, 9, 7),
              child: Text(
                '播放速度',
                style: TextStyle(
                  color: Colors.white38,
                  fontSize: 10,
                  fontWeight: FontWeight.w700,
                  letterSpacing: 0.6,
                ),
              ),
            ),
            for (final rate in _rates)
              _SpeedRow(
                label: _label(rate),
                active: (speed - rate).abs() < 0.001,
                onTap: () => onSelect(rate),
              ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 5),
              child: Container(
                height: 1,
                color: Colors.white.withValues(alpha: 0.09),
              ),
            ),
            _SpeedRow(
              label: '循环播放',
              active: looping,
              icon: PlayerIcons.loop(PlayerMetrics.compact, Colors.white),
              onTap: onToggleLoop,
            ),
          ],
        ),
      ),
    );
  }
}

class _SpeedRow extends StatelessWidget {
  final String label;
  final bool active;
  final VoidCallback onTap;
  final Widget? icon;

  const _SpeedRow({
    required this.label,
    required this.active,
    required this.onTap,
    this.icon,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(AppRadius.sm),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 8),
        child: Row(
          children: [
            if (icon != null) ...[icon!, const SizedBox(width: 8)],
            Text(
              label,
              style: TextStyle(
                color: active ? AppColors.primaryMuted : Colors.white,
                fontSize: 12,
                fontWeight: active ? FontWeight.w700 : FontWeight.w500,
              ),
            ),
            const Spacer(),
            if (active)
              Icon(
                Icons.check_rounded,
                size: PlayerMetrics.compact,
                color: AppColors.primaryMuted,
              ),
          ],
        ),
      ),
    );
  }
}
