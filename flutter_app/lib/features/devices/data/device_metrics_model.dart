/// 设备运行状态模型：由 launcher 随心跳上报，服务端转发。
///
/// 所有字段都可能是缺失的——老版本 launcher 不上报指标，
/// 或某个平台采集失败。界面需要按"无数据"处理，不能假设字段存在。
library;

/// 单个磁盘分区的占用情况。
class DiskMetric {
  final String mount;
  final int totalBytes;
  final int usedBytes;
  final int freeBytes;
  final double usedPercent;

  const DiskMetric({
    required this.mount,
    required this.totalBytes,
    required this.usedBytes,
    required this.freeBytes,
    required this.usedPercent,
  });

  factory DiskMetric.fromJson(Map<String, dynamic> j) {
    return DiskMetric(
      mount: j['mount'] as String? ?? '',
      totalBytes: (j['total_bytes'] as num?)?.toInt() ?? 0,
      usedBytes: (j['used_bytes'] as num?)?.toInt() ?? 0,
      freeBytes: (j['free_bytes'] as num?)?.toInt() ?? 0,
      usedPercent: _clampPercent((j['used_percent'] as num?)?.toDouble()),
    );
  }

  /// 磁盘的显示名称：Windows 保留盘符，类 Unix 保留挂载点。
  String get label {
    final value = mount.trim();
    if (value.isEmpty) return '未知';
    return value;
  }
}

/// 设备运行状态快照。
class DeviceMetrics {
  final DateTime? collectedAt;
  final DateTime? receivedAt;
  final String platform;
  final String arch;
  final int uptimeSeconds;
  final int memoryTotalBytes;
  final int memoryUsedBytes;
  final double memoryPercent;
  final double cpuPercent;
  final int cpuCores;
  final double load1;
  final double load5;
  final double load15;
  final bool loadAvailable;
  final List<DiskMetric> disks;

  const DeviceMetrics({
    this.collectedAt,
    this.receivedAt,
    this.platform = '',
    this.arch = '',
    this.uptimeSeconds = 0,
    this.memoryTotalBytes = 0,
    this.memoryUsedBytes = 0,
    this.memoryPercent = 0,
    this.cpuPercent = 0,
    this.cpuCores = 0,
    this.load1 = 0,
    this.load5 = 0,
    this.load15 = 0,
    this.loadAvailable = false,
    this.disks = const [],
  });

  factory DeviceMetrics.fromJson(Map<String, dynamic> j) {
    final rawDisks = j['disks'] as List<dynamic>? ?? [];
    return DeviceMetrics(
      collectedAt: _parseTime(j['collected_at']),
      receivedAt: _parseTime(j['received_at']),
      platform: j['platform'] as String? ?? '',
      arch: j['architecture'] as String? ?? '',
      uptimeSeconds: (j['uptime_seconds'] as num?)?.toInt() ?? 0,
      memoryTotalBytes: (j['memory_total_bytes'] as num?)?.toInt() ?? 0,
      memoryUsedBytes: (j['memory_used_bytes'] as num?)?.toInt() ?? 0,
      memoryPercent: _clampPercent(
        (j['memory_used_percent'] as num?)?.toDouble(),
      ),
      cpuPercent: _clampPercent((j['cpu_used_percent'] as num?)?.toDouble()),
      cpuCores: (j['cpu_cores'] as num?)?.toInt() ?? 0,
      load1: (j['load_1'] as num?)?.toDouble() ?? 0,
      load5: (j['load_5'] as num?)?.toDouble() ?? 0,
      load15: (j['load_15'] as num?)?.toDouble() ?? 0,
      loadAvailable: j['load_available'] as bool? ?? false,
      disks: rawDisks
          .whereType<Map<String, dynamic>>()
          .map(DiskMetric.fromJson)
          .toList(),
    );
  }

  /// 是否包含任何可展示的数据。全空时界面显示占位而非零值。
  bool get hasAnyData =>
      memoryTotalBytes > 0 || disks.isNotEmpty || cpuPercent > 0;

  /// 占用率最高的分区，用于卡片主展示。
  DiskMetric? get primaryDisk {
    if (disks.isEmpty) return null;
    return disks.reduce(
      (a, b) => a.usedPercent >= b.usedPercent ? a : b,
    );
  }

  /// 数据新鲜度：距采集时间的秒数。优先用服务端接收时间，
  /// 避免设备时钟偏差导致显示"来自未来"。
  int? ageSeconds({DateTime? now}) {
    final reference = receivedAt ?? collectedAt;
    if (reference == null) return null;
    final elapsed = (now ?? DateTime.now()).toUtc().difference(
      reference.toUtc(),
    );
    final seconds = elapsed.inSeconds;
    return seconds < 0 ? 0 : seconds;
  }

  /// 数据是否已经过期，超过 60 秒视为过期。
  bool isStale({DateTime? now, int thresholdSeconds = 60}) {
    final age = ageSeconds(now: now);
    if (age == null) return true;
    return age > thresholdSeconds;
  }

  /// 运行时长，用于展示"已运行 X天X小时"。
  Duration get uptime => Duration(seconds: uptimeSeconds);
}

DateTime? _parseTime(Object? value) {
  if (value is! String || value.trim().isEmpty) return null;
  return DateTime.tryParse(value);
}

double _clampPercent(double? value) {
  if (value == null || value.isNaN) return 0;
  if (value < 0) return 0;
  if (value > 100) return 100;
  return value;
}

/// 把字节数格式化成人可读的大小，例如 "12.4 GB"。
String formatBytes(int bytes) {
  if (bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  var value = bytes.toDouble();
  var unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex++;
  }
  // 整数不显示小数位；非整数保留一位（15.8 GB 比 16 GB 更有信息量），
  // 到 100 以上零头已无意义，取整即可。
  final text = unitIndex == 0 || value >= 100 || value == value.roundToDouble()
      ? value.toStringAsFixed(0)
      : value.toStringAsFixed(1);
  return '$text ${units[unitIndex]}';
}

/// 把秒数格式化成人可读的运行时长。
String formatUptime(Duration duration) {
  final seconds = duration.inSeconds;
  if (seconds <= 0) return '未知';
  final days = duration.inDays;
  final hours = duration.inHours % 24;
  final minutes = duration.inMinutes % 60;
  if (days > 0) return '$days 天 $hours 小时';
  if (hours > 0) return '$hours 小时 $minutes 分钟';
  if (minutes > 0) return '$minutes 分钟';
  return '$seconds 秒';
}
