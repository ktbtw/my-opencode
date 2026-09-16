import 'package:flutter_test/flutter_test.dart';

import 'package:chat_codex_app/features/devices/data/device_storage_model.dart';

void main() {
  group('DeviceStorageUsage', () {
    test('解析各类别占用与总量', () {
      final usage = DeviceStorageUsage.fromJson({
        'root': '/Users/demo/.my-opencode-launcher',
        'total_bytes': 8589934592,
        'categories': [
          {
            'key': 'caches',
            'label': '依赖缓存',
            'bytes': 4294967296,
            'clearable': true,
          },
          {
            'key': 'runtimes',
            'label': '运行时组件',
            'bytes': 2576980377,
            'clearable': false,
          },
          {
            'key': 'versions',
            'label': '历史版本',
            'detail': '保留当前版本 1.15.97',
            'bytes': 1717986919,
            'clearable': true,
          },
        ],
      });

      expect(usage.root, '/Users/demo/.my-opencode-launcher');
      expect(usage.totalBytes, 8589934592);
      expect(usage.categories.length, 3);
      expect(usage.totalLabel, '8.00 GB');
    });

    test('可清理体积只统计 clearable 类别', () {
      final usage = DeviceStorageUsage.fromJson({
        'total_bytes': 1000,
        'categories': [
          {'key': 'caches', 'label': '依赖缓存', 'bytes': 400, 'clearable': true},
          {
            'key': 'runtimes',
            'label': '运行时组件',
            'bytes': 300,
            'clearable': false,
          },
          {'key': 'logs', 'label': '运行日志', 'bytes': 100, 'clearable': true},
        ],
      });

      // 运行时组件不属于可释放空间。
      expect(usage.clearableBytes, 500);
      expect(
        usage.clearableCategories.map((item) => item.key).toList(),
        ['caches', 'logs'],
      );
    });

    test('按占用从大到小排序', () {
      final usage = DeviceStorageUsage.fromJson({
        'categories': [
          {'key': 'a', 'label': 'A', 'bytes': 10, 'clearable': true},
          {'key': 'b', 'label': 'B', 'bytes': 900, 'clearable': true},
          {'key': 'c', 'label': 'C', 'bytes': 100, 'clearable': true},
        ],
      });

      expect(
        usage.sortedCategories.map((item) => item.key).toList(),
        ['b', 'c', 'a'],
      );
    });

    test('字段缺失或类型异常时安全降级', () {
      final usage = DeviceStorageUsage.fromJson({
        'bytes': null,
        'total_bytes': '2048',
        'categories': [
          {'key': 'caches', 'label': '依赖缓存', 'bytes': '1024'},
          'not-a-map',
        ],
      });

      expect(usage.totalBytes, 2048);
      expect(usage.categories.length, 1);
      expect(usage.categories.first.bytes, 1024);
      // 没有 clearable 标记时按不可清理处理，避免误删。
      expect(usage.categories.first.clearable, isFalse);
      expect(usage.categories.first.detail, '');
    });

    test('空数据不抛异常', () {
      final usage = DeviceStorageUsage.fromJson(const {});
      expect(usage.categories, isEmpty);
      expect(usage.totalBytes, 0);
      expect(usage.totalLabel, '0 B');
      expect(usage.clearableBytes, 0);
      expect(usage.sortedCategories, isEmpty);
    });
  });

  group('DeviceStorageClearResult', () {
    test('解析释放量与跳过项', () {
      final result = DeviceStorageClearResult.fromJson({
        'freed_bytes': 3221225472,
        'categories': [
          {'key': 'caches', 'label': '依赖缓存', 'bytes': 0, 'clearable': true},
        ],
        'skipped': ['类别 runtimes 不允许清理'],
      });

      expect(result.freedBytes, 3221225472);
      expect(result.hasFreed, isTrue);
      expect(result.freedLabel, '3.00 GB');
      expect(result.hasSkipped, isTrue);
      expect(result.skipped.single, '类别 runtimes 不允许清理');
    });

    test('未释放空间时标记为 false', () {
      final result = DeviceStorageClearResult.fromJson(const {});
      expect(result.freedBytes, 0);
      expect(result.hasFreed, isFalse);
      expect(result.hasSkipped, isFalse);
      expect(result.freedLabel, '0 B');
    });
  });

  group('formatStorageBytes', () {
    test('按量级选择单位', () {
      expect(formatStorageBytes(0), '0 B');
      expect(formatStorageBytes(512), '512 B');
      expect(formatStorageBytes(1024), '1.00 KB');
      expect(formatStorageBytes(1024 * 1024), '1.00 MB');
      expect(formatStorageBytes(1024 * 1024 * 1024), '1.00 GB');
      expect(formatStorageBytes(5 * 1024 * 1024 * 1024), '5.00 GB');
      expect(formatStorageBytes(1024 * 1024 * 1024 * 1024), '1.00 TB');
    });

    test('大数值自动减少小数位', () {
      // 100 以上不保留小数，10~99 保留一位，避免文本抖动。
      expect(formatStorageBytes(150 * 1024), '150 KB');
      expect(formatStorageBytes(15 * 1024), '15.0 KB');
    });

    test('负数按 0 处理', () {
      expect(formatStorageBytes(-1), '0 B');
    });
  });
}
