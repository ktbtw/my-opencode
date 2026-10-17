import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:chat_codex_app/features/chat/presentation/video_player/player_icons.dart';
import 'package:chat_codex_app/features/chat/presentation/video_player/video_player_controls.dart';

void main() {
  group('精确跳转时间', () {
    test('支持秒数、分秒与时分秒', () {
      expect(
        VideoPlayerControls.parseTimestamp(' 90 '),
        const Duration(seconds: 90),
      );
      expect(
        VideoPlayerControls.parseTimestamp('01:30'),
        const Duration(seconds: 90),
      );
      expect(
        VideoPlayerControls.parseTimestamp('1:02:03'),
        const Duration(seconds: 3723),
      );
      expect(
        VideoPlayerControls.parseTimestamp('61:00'),
        const Duration(minutes: 61),
      );
      expect(VideoPlayerControls.parseTimestamp('0'), Duration.zero);
    });
    test('拒绝无效或溢出的分秒', () {
      for (final input in [
        '',
        '-1',
        '1.5',
        '1:60',
        '1:99:00',
        'abc',
        ':10',
        '1:2:3:4',
        '999999999999999999999',
      ]) {
        expect(
          VideoPlayerControls.parseTimestamp(input),
          isNull,
          reason: input,
        );
      }
    });
  });
  group('时长格式化', () {
    test('补零到 mm:ss', () {
      expect(
        VideoPlayerControls.formatDuration(const Duration(seconds: 0)),
        '00:00',
      );
      expect(
        VideoPlayerControls.formatDuration(const Duration(seconds: 7)),
        '00:07',
      );
      expect(
        VideoPlayerControls.formatDuration(const Duration(seconds: 72)),
        '01:12',
      );
    });

    test('超过一小时按分钟累计，不截断', () {
      expect(
        VideoPlayerControls.formatDuration(const Duration(minutes: 61)),
        '61:00',
      );
    });

    test('负值不产生负数时间', () {
      expect(
        VideoPlayerControls.formatDuration(const Duration(seconds: -5)),
        '00:00',
      );
    });
  });

  group('图标尺寸档位', () {
    test('档位递进且互不相同', () {
      final sizes = <double>{
        PlayerMetrics.inline,
        PlayerMetrics.compact,
        PlayerMetrics.button,
        PlayerMetrics.hero,
        PlayerMetrics.heroLarge,
      };
      expect(sizes.length, 5, reason: '每个档位都应是独立尺寸');
      expect(PlayerMetrics.inline < PlayerMetrics.compact, isTrue);
      expect(PlayerMetrics.compact < PlayerMetrics.button, isTrue);
      expect(PlayerMetrics.button < PlayerMetrics.hero, isTrue);
      expect(PlayerMetrics.hero < PlayerMetrics.heroLarge, isTrue);
    });

    test('触控目标不小于图标尺寸', () {
      expect(PlayerMetrics.touchTarget >= PlayerMetrics.button, isTrue);
      expect(
        PlayerMetrics.touchTargetDesktop >= PlayerMetrics.touchTarget,
        isTrue,
      );
    });
  });

  group('快退 / 快进方向', () {
    test('快退用朝左的撤销造型，快进用朝右的重做造型', () {
      // 方向搞反会让用户按「快退」却前进，这里锁住语义。
      final back = PlayerIcons.skipBack(18, Colors.white);
      final forward = PlayerIcons.skipForward(18, Colors.white);
      expect((back as Icon).icon, Icons.replay_rounded);
      expect((forward as Icon).icon, Icons.refresh_rounded);
      expect(back.icon, isNot(forward.icon));
    });
  });
}
