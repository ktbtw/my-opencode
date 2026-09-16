/// 设备 launcher 运行目录的磁盘占用模型。
///
/// 体积统计遍历开销较大，因此数据按需拉取，不随指标推送。
class DeviceStorageCategory {
  final String key;
  final String label;
  final String detail;
  final int bytes;

  /// 是否允许清理。运行时组件、Agent 数据与程序文件为 false。
  final bool clearable;

  const DeviceStorageCategory({
    required this.key,
    required this.label,
    this.detail = '',
    this.bytes = 0,
    this.clearable = false,
  });

  factory DeviceStorageCategory.fromJson(Map<String, dynamic> json) {
    return DeviceStorageCategory(
      key: (json['key'] ?? '').toString(),
      label: (json['label'] ?? '').toString(),
      detail: (json['detail'] ?? '').toString(),
      bytes: _asInt(json['bytes']),
      clearable: json['clearable'] == true,
    );
  }

}

/// 一次完整的占用统计。
class DeviceStorageUsage {
  final String root;
  final int totalBytes;
  final List<DeviceStorageCategory> categories;

  const DeviceStorageUsage({
    this.root = '',
    this.totalBytes = 0,
    this.categories = const [],
  });

  factory DeviceStorageUsage.fromJson(Map<String, dynamic> json) {
    final raw = json['categories'];
    final categories = <DeviceStorageCategory>[];
    if (raw is List) {
      for (final item in raw) {
        if (item is Map) {
          categories.add(
            DeviceStorageCategory.fromJson(Map<String, dynamic>.from(item)),
          );
        }
      }
    }
    return DeviceStorageUsage(
      root: (json['root'] ?? '').toString(),
      totalBytes: _asInt(json['total_bytes']),
      categories: categories,
    );
  }

  /// 可清理类别释放后的剩余体积合计，用于展示“可释放”提示。
  int get clearableBytes => categories
      .where((item) => item.clearable)
      .fold(0, (sum, item) => sum + item.bytes);

  List<DeviceStorageCategory> get clearableCategories =>
      categories.where((item) => item.clearable).toList();

  /// 按占用从大到小排序，便于界面把大头排前面。
  List<DeviceStorageCategory> get sortedCategories {
    final list = [...categories];
    list.sort((a, b) => b.bytes.compareTo(a.bytes));
    return list;
  }

  String get totalLabel => formatStorageBytes(totalBytes);
}

/// 清理结果。
class DeviceStorageClearResult {
  final int freedBytes;
  final List<DeviceStorageCategory> categories;
  final List<String> skipped;

  const DeviceStorageClearResult({
    this.freedBytes = 0,
    this.categories = const [],
    this.skipped = const [],
  });

  factory DeviceStorageClearResult.fromJson(Map<String, dynamic> json) {
    final raw = json['categories'];
    final categories = <DeviceStorageCategory>[];
    if (raw is List) {
      for (final item in raw) {
        if (item is Map) {
          categories.add(
            DeviceStorageCategory.fromJson(Map<String, dynamic>.from(item)),
          );
        }
      }
    }
    final rawSkipped = json['skipped'];
    final skipped = <String>[];
    if (rawSkipped is List) {
      for (final item in rawSkipped) {
        skipped.add(item.toString());
      }
    }
    return DeviceStorageClearResult(
      freedBytes: _asInt(json['freed_bytes']),
      categories: categories,
      skipped: skipped,
    );
  }

  bool get hasFreed => freedBytes > 0;

  String get freedLabel => formatStorageBytes(freedBytes);

  /// 清理后是否存在无法释放的类别，用于提示用户。
  bool get hasSkipped => skipped.isNotEmpty;
}

/// 把字节数格式化为易读文本。
/// 与运行状态面板保持同一量级习惯（B / KB / MB / GB / TB）。
String formatStorageBytes(int bytes) {
  if (bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  var value = bytes.toDouble();
  var unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  if (unit == 0) return '${value.toStringAsFixed(0)} ${units[unit]}';
  final digits = value >= 100 ? 0 : (value >= 10 ? 1 : 2);
  return '${value.toStringAsFixed(digits)} ${units[unit]}';
}

/// 宽容地把 JSON 中的数字字段转成 int。
int _asInt(Object? value) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  return int.tryParse('${value ?? ''}') ?? 0;
}
