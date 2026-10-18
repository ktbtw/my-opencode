import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// 把内部目录标识（如 SAF 的 content://）解析成可读路径，仅用于界面展示。
class DirectoryLabel {
  static const _channel = MethodChannel('chat_codex/artifact_directory');
  static final Map<String, String> _cache = <String, String>{};

  /// 同步取缓存；没有解析过返回 null（界面可先显示占位）。
  static String? cached(String raw) => _cache[raw.trim()];

  static Future<String> describe(String raw) async {
    final value = raw.trim();
    if (value.isEmpty) return value;
    if (!value.startsWith('content://')) return value;
    final hit = _cache[value];
    if (hit != null) return hit;
    if (kIsWeb || !Platform.isAndroid) {
      _cache[value] = value;
      return value;
    }
    try {
      final readable = await _channel.invokeMethod<String>(
        'describeDirectory',
        {'uri': value},
      );
      final result = (readable == null || readable.trim().isEmpty)
          ? value
          : readable.trim();
      _cache[value] = result;
      return result;
    } catch (_) {
      _cache[value] = value;
      return value;
    }
  }
}
