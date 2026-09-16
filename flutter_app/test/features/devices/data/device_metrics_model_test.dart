import 'package:flutter_test/flutter_test.dart';
import 'package:chat_codex_app/features/devices/data/device_metrics_model.dart';

void main() {
  group('DeviceMetrics.fromJson', () {
    test('解析完整指标载荷', () {
      final metrics = DeviceMetrics.fromJson({
        'collected_at': '2026-09-16T04:00:00Z',
        'received_at': '2026-09-16T04:00:02Z',
        'platform': 'windows',
        'architecture': 'amd64',
        'uptime_seconds': 546192,
        'memory_total_bytes': 16952647680,
        'memory_used_bytes': 7294124032,
        'memory_used_percent': 43.0,
        'cpu_used_percent': 12.5,
        'cpu_cores': 8,
        'load_1': 1.5,
        'load_5': 1.2,
        'load_15': 1.0,
        'load_available': true,
        'disks': [
          {
            'mount': 'C:',
            'total_bytes': 511819714560,
            'used_bytes': 381860655104,
            'free_bytes': 129959059456,
            'used_percent': 74.61,
          },
        ],
      });

      expect(metrics.platform, 'windows');
      expect(metrics.arch, 'amd64');
      expect(metrics.memoryPercent, 43.0);
      expect(metrics.cpuPercent, 12.5);
      expect(metrics.cpuCores, 8);
      expect(metrics.loadAvailable, isTrue);
      expect(metrics.disks.length, 1);
      expect(metrics.disks.first.mount, 'C:');
      expect(metrics.disks.first.usedPercent, 74.61);
      expect(metrics.hasAnyData, isTrue);
    });

    test('缺失字段时安全降级为空值', () {
      final metrics = DeviceMetrics.fromJson({});
      expect(metrics.memoryPercent, 0);
      expect(metrics.disks, isEmpty);
      expect(metrics.hasAnyData, isFalse);
      expect(metrics.primaryDisk, isNull);
    });

    test('越界的百分比被收敛到 0-100', () {
      final metrics = DeviceMetrics.fromJson({
        'memory_used_percent': 150,
        'cpu_used_percent': -20,
      });
      expect(metrics.memoryPercent, 100);
      expect(metrics.cpuPercent, 0);
    });

    test('primaryDisk 返回占用率最高的分区', () {
      final metrics = DeviceMetrics.fromJson({
        'disks': [
          {'mount': '/', 'total_bytes': 100, 'used_percent': 20},
          {'mount': '/data', 'total_bytes': 100, 'used_percent': 85},
          {'mount': '/home', 'total_bytes': 100, 'used_percent': 50},
        ],
      });
      expect(metrics.primaryDisk?.mount, '/data');
      expect(metrics.primaryDisk?.usedPercent, 85);
    });

    test('ignores non-map disk entries', () {
      final metrics = DeviceMetrics.fromJson({
        'disks': [
          'not-a-map',
          {'mount': '/', 'total_bytes': 100, 'used_percent': 20},
        ],
      });
      expect(metrics.disks.length, 1);
    });
  });

  group('数据新鲜度', () {
    test('优先使用 received_at 计算 age', () {
      final metrics = DeviceMetrics.fromJson({
        'collected_at': '2026-09-16T04:00:00Z',
        'received_at': '2026-09-16T04:00:30Z',
      });
      final now = DateTime.parse('2026-09-16T04:00:40Z');
      expect(metrics.ageSeconds(now: now), 10);
    });

    test('缺失时间时 age 为 null 且视为过期', () {
      final metrics = DeviceMetrics.fromJson({});
      expect(metrics.ageSeconds(), isNull);
      expect(metrics.isStale(), isTrue);
    });

    test('超过阈值判定为过期', () {
      final metrics = DeviceMetrics.fromJson({
        'received_at': '2026-09-16T04:00:00Z',
      });
      final now = DateTime.parse('2026-09-16T04:01:30Z');
      expect(metrics.ageSeconds(now: now), 90);
      expect(metrics.isStale(now: now), isTrue);
      expect(metrics.isStale(now: now, thresholdSeconds: 200), isFalse);
    });

    test('时钟偏差导致未来时间时收敛为 0', () {
      final metrics = DeviceMetrics.fromJson({
        'received_at': '2026-09-16T05:00:00Z',
      });
      final now = DateTime.parse('2026-09-16T04:00:00Z');
      expect(metrics.ageSeconds(now: now), 0);
    });
  });

  group('格式化', () {
    test('formatBytes 按单位缩放', () {
      expect(formatBytes(0), '0 B');
      expect(formatBytes(512), '512 B');
      expect(formatBytes(1024), '1 KB');
      expect(formatBytes(1536), '1.5 KB');
      expect(formatBytes(16 * 1024 * 1024 * 1024), '16 GB');
      expect(formatBytes(16952647680), '15.8 GB');
      // 100 以上零头无意义，取整。
      expect(formatBytes(130 * 1024 * 1024 * 1024), '130 GB');
    });

    test('formatUptime 按量级选择单位', () {
      expect(formatUptime(Duration.zero), '未知');
      expect(formatUptime(const Duration(seconds: 45)), '45 秒');
      expect(formatUptime(const Duration(minutes: 30)), '30 分钟');
      expect(formatUptime(const Duration(hours: 5, minutes: 20)), '5 小时 20 分钟');
      expect(formatUptime(const Duration(days: 3, hours: 4)), '3 天 4 小时');
    });

    test('DiskMetric.label 空挂载点显示未知', () {
      expect(const DiskMetric(mount: 'C:', totalBytes: 1, usedBytes: 1, freeBytes: 0, usedPercent: 100).label, 'C:');
      expect(const DiskMetric(mount: '  ', totalBytes: 1, usedBytes: 1, freeBytes: 0, usedPercent: 100).label, '未知');
    });
  });
}
