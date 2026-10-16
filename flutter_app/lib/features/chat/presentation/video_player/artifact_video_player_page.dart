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
import '../../../../shared/widgets/app_status_icon.dart';
import '../../data/artifact_download_service.dart';
import '../../data/chat_model.dart';
import '../media_viewer_widgets.dart';
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
  int _openGeneration = 0;
  bool _saving = false;
  bool _sharing = false;
  bool _capturing = false;
  final Map<String, List<Duration>> _markers = {};

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
    final generation = ++_openGeneration;
    final controller = VideoPlayerController.networkUrl(
      Uri.parse(ArtifactDownloadService.downloadUrl(widget.taskId, _current)),
      httpHeaders: ArtifactDownloadService.authHeaders(),
    );
    try {
      await controller.initialize();
      if (!mounted || generation != _openGeneration) {
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
      if (!mounted ||
          generation != _openGeneration ||
          controller != _controller) {
        return;
      }
      await controller.play();
      _scheduleHide();
    } catch (error) {
      debugPrint('TRACE catch before dispose: $error');
      await controller.dispose();
      debugPrint('TRACE catch after dispose');
      if (!mounted || generation != _openGeneration) return;
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
    _openGeneration++;
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
    if (!_fullscreen) return;
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
    if (_saving) return;
    final artifact = _current;
    setState(() => _saving = true);
    _controller?.pause();
    showAppFeedback(context, message: '正在保存视频…');
    try {
      final path = await ArtifactDownloadService.download(
        taskId: widget.taskId,
        artifact: artifact,
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
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  /// 分享：先把视频落到临时文件，再交给系统分享面板。
  Future<void> _share() async {
    if (_sharing) return;
    final artifact = _current;
    final box = context.findRenderObject() as RenderBox?;
    final origin = box == null
        ? null
        : box.localToGlobal(Offset.zero) & box.size;
    setState(() => _sharing = true);
    _controller?.pause();
    showAppFeedback(context, message: '正在准备分享…');
    try {
      final bytes = await ArtifactDownloadService.fetchBytes(
        taskId: widget.taskId,
        artifact: artifact,
      );
      final dir = await getTemporaryDirectory();
      final file = File(p.join(dir.path, p.basename(artifact.filename)));
      await file.writeAsBytes(bytes, flush: true);
      await SharePlus.instance.share(
        ShareParams(
          files: [XFile(file.path, mimeType: artifact.mimeType)],
          sharePositionOrigin: origin,
        ),
      );
    } catch (error) {
      if (!mounted) return;
      showAppFeedback(
        context,
        message: '分享失败：${_friendlyError(error)}',
        error: true,
      );
    } finally {
      if (mounted) setState(() => _sharing = false);
    }
  }

  /// 截取当前帧为图片并保存。
  ///
  /// 从画面所在的 RepaintBoundary 抓取，得到的是用户此刻看到的那一帧。
  Future<void> _captureFrame() async {
    if (_capturing) return;
    final boundary =
        _stageKey.currentContext?.findRenderObject() as RenderRepaintBoundary?;
    if (boundary == null) {
      showAppFeedback(context, message: '画面还没准备好，请稍后再试', error: true);
      return;
    }
    final artifact = _current;
    setState(() => _capturing = true);
    showAppFeedback(context, message: '正在截取当前帧…');
    try {
      final image = await boundary.toImage(pixelRatio: 2);
      final ByteData? data;
      try {
        data = await image.toByteData(format: ui.ImageByteFormat.png);
      } finally {
        image.dispose();
      }
      if (data == null) throw StateError('截帧失败');
      final name = p
          .basename(artifact.filename)
          .replaceAll(RegExp(r'\.[^.]+$'), '');
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
    } finally {
      if (mounted) setState(() => _capturing = false);
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
      if (!mounted) return;
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
    return PopScope(
      canPop: !_fullscreen,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop && _fullscreen) _toggleFullscreen();
      },
      child: Scaffold(
        backgroundColor: AppColors.background,
        appBar: _fullscreen
            ? null
            : AppBar(
                leading: const AppStatusBackButton(),
                titleSpacing: 0,
                title: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      '视频预览',
                      style: TextStyle(
                        fontSize: 15,
                        fontWeight: FontWeight.w600,
                      ),
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
                    tooltip: _sharing ? '正在分享' : '分享视频',
                    onPressed: controller != null && !_sharing ? _share : null,
                    icon: const AppStatusIcon(Icons.ios_share_outlined),
                  ),
                  Padding(
                    padding: const EdgeInsets.only(right: 12, left: 4),
                    child: FilledButton.icon(
                      onPressed: !_saving ? _save : null,
                      icon: const AppStatusIcon(
                        Icons.download_rounded,
                        size: 17,
                      ),
                      label: Text(
                        _saving ? '保存中' : '保存视频',
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                  ),
                ],
              ),
        body: Focus(
          autofocus: true,
          onKeyEvent: _onKey,
          child: _initializing
              ? const Center(
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      CircularProgressIndicator(strokeWidth: 2),
                      SizedBox(height: 18),
                      Text(
                        '正在准备视频…',
                        style: TextStyle(
                          fontSize: 13,
                          color: AppColors.textSecondary,
                        ),
                      ),
                    ],
                  ),
                )
              : _error.isNotEmpty
              ? _ErrorView(message: _error, onRetry: _retry)
              : _fullscreen
              ? _buildPlayer(controller!)
              : _buildWorkspace(controller!),
        ),
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

  Widget _buildWorkspace(VideoPlayerController controller) => SafeArea(
    child: LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= 1000;
        final player = ClipRRect(
          borderRadius: BorderRadius.circular(16),
          child: ColoredBox(
            color: Colors.black,
            child: _buildDesktopPlayer(controller),
          ),
        );
        final details = Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            _playbackPanel(controller),
            const SizedBox(height: 12),
            MediaPanel(
              title: '视频信息',
              trailing: MediaBadge(
                _current.mimeType.replaceFirst('video/', '').toUpperCase(),
              ),
              child: MediaInfoList(
                entries: [
                  ('文件名', _current.filename),
                  (
                    '时长',
                    VideoPlayerControls.formatDuration(
                      controller.value.duration,
                    ),
                  ),
                  (
                    '分辨率',
                    '${controller.value.size.width.round()} × ${controller.value.size.height.round()}',
                  ),
                  ('大小', mediaFileSize(_current.sizeBytes)),
                ],
              ),
            ),
            if (_playlist.length > 1) ...[
              const SizedBox(height: 12),
              _playlistPanel(),
            ],
          ],
        );
        if (wide) {
          return Padding(
            padding: const EdgeInsets.all(20),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Column(
                    children: [
                      Expanded(child: player),
                      const SizedBox(height: 12),
                      _tools(controller),
                      const Padding(
                        padding: EdgeInsets.only(top: 14),
                        child: Text(
                          '空格 播放 / 暂停 · ← → 快退 / 快进 · M 静音 · F 全屏',
                          style: TextStyle(
                            fontSize: 11,
                            color: AppColors.textMuted,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 16),
                SizedBox(
                  width: 290,
                  child: SingleChildScrollView(child: details),
                ),
              ],
            ),
          );
        }
        final playerHeight = (constraints.maxHeight * .48).clamp(130.0, 440.0);
        return Column(
          children: [
            SizedBox(
              height: playerHeight,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(12, 12, 12, 0),
                child: player,
              ),
            ),
            Padding(
              padding: const EdgeInsets.all(12),
              child: _tools(controller),
            ),
            Expanded(
              child: SingleChildScrollView(
                padding: const EdgeInsets.fromLTRB(12, 0, 12, 16),
                child: details,
              ),
            ),
          ],
        );
      },
    ),
  );

  Widget _tools(VideoPlayerController controller) => Container(
    decoration: BoxDecoration(
      color: AppColors.surface,
      border: Border.all(color: AppColors.borderLight),
      borderRadius: BorderRadius.circular(14),
    ),
    padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 4),
    child: SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          MediaToolButton(
            icon: Icons.photo_camera_outlined,
            label: _capturing ? '截取中' : '截取画面',
            onPressed: _capturing ? null : _captureFrame,
          ),
          MediaToolButton(
            icon: Icons.schedule_outlined,
            label: '精确跳转',
            onPressed: () => _jumpToTime(controller),
          ),
          MediaToolButton(
            icon: Icons.bookmark_add_outlined,
            label: '添加标记',
            onPressed: () => _addMarker(controller),
          ),
          MediaToolButton(
            icon: Icons.replay_outlined,
            label: '后退 1 秒',
            onPressed: () => _stepSecond(controller, -1),
          ),
          MediaToolButton(
            icon: Icons.arrow_forward_outlined,
            label: '前进 1 秒',
            onPressed: () => _stepSecond(controller, 1),
          ),
          MediaToolButton(
            icon: Icons.aspect_ratio_outlined,
            label: _fitLabel,
            onPressed: _cycleFit,
          ),
          MediaToolButton(
            icon: Icons.repeat_rounded,
            label: '循环播放',
            selected: _looping,
            onPressed: _toggleLoop,
          ),
          if (Platform.isAndroid)
            MediaToolButton(
              icon: Icons.picture_in_picture_alt_outlined,
              label: '画中画',
              onPressed: _enterPip,
            ),
        ],
      ),
    ),
  );

  Widget _playbackPanel(VideoPlayerController controller) => MediaPanel(
    title: '播放设置',
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text(
          '播放速度',
          style: TextStyle(fontSize: 11, color: AppColors.textMuted),
        ),
        const SizedBox(height: 8),
        Wrap(
          spacing: 6,
          runSpacing: 6,
          children: [
            for (final speed in [.5, .75, 1.0, 1.25, 1.5, 2.0])
              ChoiceChip(
                label: Text(
                  '${speed == 1 ? '1' : speed}×',
                  style: const TextStyle(fontSize: 11),
                ),
                selected: speed == _speed,
                showCheckmark: false,
                visualDensity: VisualDensity.compact,
                onSelected: (_) => _setSpeed(speed),
              ),
          ],
        ),
        const SizedBox(height: 14),
        Row(
          children: [
            const Expanded(
              child: Text(
                '循环播放',
                style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
              ),
            ),
            Switch(value: _looping, onChanged: (_) => _toggleLoop()),
          ],
        ),
        const Divider(color: AppColors.borderLight, height: 22),
        Row(
          children: [
            const Expanded(
              child: Text(
                '时间标记',
                style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
              ),
            ),
            IconButton(
              tooltip: '标记当前位置',
              onPressed: () => _addMarker(controller),
              icon: const AppStatusIcon(Icons.add_outlined, size: 17),
            ),
          ],
        ),
        if ((_markers[_current.id] ?? []).isEmpty)
          const Text(
            '标记精彩片段，点击时间即可回看。',
            style: TextStyle(
              fontSize: 11,
              height: 1.6,
              color: AppColors.textMuted,
            ),
          )
        else
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: [
              for (final time in _markers[_current.id]!)
                InputChip(
                  label: Text(
                    VideoPlayerControls.formatDuration(time),
                    style: const TextStyle(fontSize: 11),
                  ),
                  onPressed: () => controller.seekTo(time),
                  onDeleted: () =>
                      setState(() => _markers[_current.id]!.remove(time)),
                ),
            ],
          ),
      ],
    ),
  );

  Widget _playlistPanel() => MediaPanel(
    title: '本轮视频',
    trailing: MediaBadge('${_playlist.length} 个'),
    child: Column(
      children: [
        for (var i = 0; i < _playlist.length; i++)
          Padding(
            padding: const EdgeInsets.only(bottom: 6),
            child: ListTile(
              selected: i == _index,
              selectedTileColor: AppColors.primaryLight,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(9),
              ),
              contentPadding: const EdgeInsets.symmetric(horizontal: 10),
              leading: Icon(
                i == _index ? Icons.play_circle_outline : Icons.movie_outlined,
                size: 22,
                color: i == _index ? AppColors.primary : AppColors.textMuted,
              ),
              title: Text(
                _playlist[i].filename,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(fontSize: 12),
              ),
              subtitle: Text(
                i == _index ? '当前视频' : mediaFileSize(_playlist[i].sizeBytes),
                style: const TextStyle(
                  fontSize: 10,
                  color: AppColors.textMuted,
                ),
              ),
              onTap: () => _playAt(i),
            ),
          ),
      ],
    ),
  );

  void _addMarker(VideoPlayerController controller) {
    final time = Duration(seconds: controller.value.position.inSeconds);
    setState(() {
      final markers = _markers.putIfAbsent(_current.id, () => []);
      if (!markers.contains(time)) markers.add(time);
      markers.sort();
    });
  }

  Future<void> _stepSecond(
    VideoPlayerController controller,
    int seconds,
  ) async {
    await controller.pause();
    if (!mounted || controller != _controller) return;
    final target = (controller.value.position.inMilliseconds + seconds * 1000)
        .clamp(0, controller.value.duration.inMilliseconds);
    await controller.seekTo(Duration(milliseconds: target));
  }

  Future<void> _jumpToTime(VideoPlayerController controller) async {
    final input = TextEditingController(
      text: VideoPlayerControls.formatDuration(controller.value.position),
    );
    String? error;
    final target = await showDialog<Duration>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (context, update) => AlertDialog(
          title: const Text('跳转到指定时间'),
          content: SizedBox(
            width: 300,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                TextField(
                  controller: input,
                  autofocus: true,
                  keyboardType: TextInputType.datetime,
                  decoration: InputDecoration(
                    labelText: '时间',
                    hintText: '01:30 或 90',
                    helperText:
                        '视频总时长 ${VideoPlayerControls.formatDuration(controller.value.duration)}',
                    errorText: error,
                  ),
                ),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialogContext),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () {
                final time = VideoPlayerControls.parseTimestamp(input.text);
                if (time == null || time > controller.value.duration) {
                  update(() => error = '请输入视频时长范围内的秒数或 分:秒');
                  return;
                }
                Navigator.pop(dialogContext, time);
              },
              child: const Text('跳转'),
            ),
          ],
        ),
      ),
    );
    // 等待对话框退场后再释放输入框控制器。
    await Future<void>.delayed(const Duration(milliseconds: 250));
    input.dispose();
    if (target != null && mounted && controller == _controller) {
      await controller.seekTo(target);
    }
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
                child: _videoSurface(controller),
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
    if (_hasNext) await _playAt(_index + 1);
  }

  Future<void> _playAt(int index) async {
    if (_initializing ||
        index == _index ||
        index < 0 ||
        index >= _playlist.length) {
      return;
    }
    final old = _controller;
    if (old != null) {
      unawaited(
        PlayerProgressStore.write(
          widget.taskId,
          _current.id,
          old.value.position,
        ),
      );
    }
    _hideTimer?.cancel();
    setState(() {
      _controller = null;
      _index = index;
      _initializing = true;
      _scrubPosition = null;
      _gestureSeek = null;
      _speedMenuOpen = false;
      _buffering = false;
      _ended = false;
    });
    old?.removeListener(_onTick);
    debugPrint('TRACE before dispose');
    await old?.dispose();
    debugPrint('TRACE after dispose');
    if (mounted) await _open();
  }

  Widget _videoSurface(VideoPlayerController controller) => ColoredBox(
    color: Colors.black,
    child: SizedBox.expand(
      child: ClipRect(
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
  );

  /// 桌面端：控制栏常显在画面下方，再下面是文件信息，不遮挡画面。
  Widget _buildDesktopPlayer(VideoPlayerController controller) {
    return Column(
      children: [
        Expanded(
          child: Stack(
            fit: StackFit.expand,
            children: [
              Container(color: Colors.black),
              RepaintBoundary(key: _stageKey, child: _videoSurface(controller)),
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
              if (_fullscreen)
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
              isDesktop: _isDesktop,
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
                child: PlayerIcons.error(20, AppColors.statusError),
              ),
            ),
            const SizedBox(height: 16),
            Text(
              message,
              textAlign: TextAlign.center,
              style: const TextStyle(
                color: AppColors.textPrimary,
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
