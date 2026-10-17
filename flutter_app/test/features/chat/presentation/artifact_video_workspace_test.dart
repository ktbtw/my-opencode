import 'dart:async';

import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/core/theme/app_theme.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/video_player/artifact_video_player_page.dart';
import 'package:chat_codex_app/features/chat/presentation/video_player/player_progress_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:video_player/video_player.dart';
import 'package:video_player_platform_interface/video_player_platform_interface.dart';

const _first = AiArtifact(
  id: 'one',
  filename: '第一段.mp4',
  mimeType: 'video/mp4',
  sizeBytes: 1024000,
  relativePath: 'one.mp4',
);
const _second = AiArtifact(
  id: 'two',
  filename: '第二段.mp4',
  mimeType: 'video/mp4',
  sizeBytes: 2048000,
  relativePath: 'two.mp4',
);

class _VideoPlatform extends VideoPlayerPlatform {
  int _nextId = 0;
  final positions = <int, Duration>{};
  final sources = <String>[];
  double speed = 1;
  bool looping = false;
  bool fail = false;

  @override
  Future<void> init() async {}
  @override
  Future<int?> createWithOptions(VideoCreationOptions options) async {
    final id = _nextId++;
    sources.add(options.dataSource.uri!);
    return id;
  }

  @override
  Stream<VideoEvent> videoEventsFor(int playerId) => fail
      ? Stream.error(
          PlatformException(code: 'VideoError', message: 'network error'),
        )
      : Stream.value(
          VideoEvent(
            eventType: VideoEventType.initialized,
            size: const Size(1920, 1080),
            duration: const Duration(minutes: 2),
          ),
        );

  @override
  Future<void> dispose(int playerId) async {}
  @override
  Future<void> play(int playerId) async {}
  @override
  Future<void> pause(int playerId) async {}
  @override
  Future<void> setVolume(int playerId, double volume) async {}
  @override
  Future<void> setLooping(int playerId, bool value) async {
    looping = value;
  }

  @override
  Future<void> setPlaybackSpeed(int playerId, double value) async {
    speed = value;
  }

  @override
  Future<void> seekTo(int playerId, Duration position) async {
    positions[playerId] = position;
  }

  @override
  Future<Duration> getPosition(int playerId) async =>
      positions[playerId] ?? Duration.zero;
  @override
  Widget buildViewWithOptions(VideoViewOptions options) =>
      const ColoredBox(color: Color(0xFF203F63));
}

Future<void> _pump(
  WidgetTester tester, {
  Size size = const Size(1280, 850),
}) async {
  tester.view.devicePixelRatio = 1;
  tester.view.physicalSize = size;
  addTearDown(tester.view.resetDevicePixelRatio);
  addTearDown(tester.view.resetPhysicalSize);
  await tester.pumpWidget(
    MaterialApp(
      theme: AppTheme.light,
      home: const ArtifactVideoPlayerPage(
        taskId: 'task',
        artifact: _first,
        playlist: [_first, _second],
      ),
    ),
  );
  await tester.runAsync(() => Future<void>.delayed(Duration.zero));
  await tester.pumpAndSettle();
}

void main() {
  late _VideoPlatform platform;
  late VideoPlayerPlatform previous;
  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    await AppStorage.init();
    previous = VideoPlayerPlatform.instance;
    platform = _VideoPlatform();
    VideoPlayerPlatform.instance = platform;
  });
  tearDown(() {
    VideoPlayerPlatform.instance = previous;
  });

  testWidgets(
    'jump validation, markers and per-video resume survive playlist changes',
    (tester) async {
      await _pump(tester);
      expect(find.text('视频信息'), findsOneWidget);
      expect(find.text('本轮视频'), findsOneWidget);
      await tester.tap(find.text('精确跳转'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), '03:00');
      await tester.tap(find.text('跳转'));
      await tester.pumpAndSettle();
      expect(find.text('请输入视频时长范围内的秒数或 分:秒'), findsOneWidget);
      await tester.enterText(find.byType(TextField), '01:10');
      await tester.tap(find.text('跳转'));
      await tester.pumpAndSettle();
      expect(platform.positions[0], const Duration(seconds: 70));
      await tester.tap(find.text('添加标记'));
      await tester.pumpAndSettle();
      expect(find.widgetWithText(InputChip, '01:10'), findsOneWidget);
      await tester.ensureVisible(find.widgetWithText(ListTile, '第二段.mp4'));
      await tester.tap(find.widgetWithText(ListTile, '第二段.mp4'));
      await tester.runAsync(() => Future<void>.delayed(Duration.zero));
      await tester.pumpAndSettle();
      expect(platform.sources.last, contains('/two/download'));
      expect(
        await PlayerProgressStore.read('task', 'one'),
        const Duration(seconds: 70),
      );
      expect(find.byType(InputChip), findsNothing);
      await tester.ensureVisible(find.widgetWithText(ListTile, '第一段.mp4'));
      await tester.tap(find.widgetWithText(ListTile, '第一段.mp4'));
      await tester.runAsync(() => Future<void>.delayed(Duration.zero));
      await tester.pumpAndSettle();
      expect(platform.positions[2], const Duration(seconds: 70));
      expect(find.widgetWithText(InputChip, '01:10'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets('speed, loop and one-second review controls affect the player', (
    tester,
  ) async {
    await _pump(tester);
    await tester.tap(find.widgetWithText(ChoiceChip, '1.5×'));
    await tester.pumpAndSettle();
    expect(platform.speed, 1.5);
    await tester.tap(find.byType(Switch));
    await tester.pumpAndSettle();
    expect(platform.looping, isTrue);
    await tester.tap(find.text('前进 1 秒'));
    await tester.pumpAndSettle();
    expect(platform.positions[0], const Duration(seconds: 1));
    expect(
      tester
          .widget<VideoPlayer>(find.byType(VideoPlayer))
          .controller
          .value
          .isPlaying,
      isFalse,
    );
    await tester.tap(find.text('后退 1 秒'));
    await tester.pumpAndSettle();
    expect(platform.positions[0], Duration.zero);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('narrow screens expose tools and full screen exits with back', (
    tester,
  ) async {
    await _pump(tester, size: const Size(320, 568));
    expect(tester.takeException(), isNull);
    await tester.tap(find.byTooltip('全屏'));
    await tester.pumpAndSettle();
    expect(find.text('视频预览'), findsNothing);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.text('视频预览'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('initialization failure stays recoverable', (tester) async {
    platform.fail = true;
    await _pump(tester);
    expect(find.text('视频加载失败，请稍后重试'), findsOneWidget);
    platform.fail = false;
    await tester.tap(find.text('重试'));
    await tester.pumpAndSettle();
    expect(find.byType(VideoPlayer), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
}
