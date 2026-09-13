import 'dart:convert';

import '../config/api_client.dart';
import '../storage/app_storage.dart';
import 'app_version_service.dart';
import 'update_log_models.dart';

class UpdateLogServicePlatform {
  static const _batchKey = 'app_log_batch_id';
  static const _currentLogPathKey = 'app_current_log_path';
  static const _legacyStorageKey = 'app_update_debug_logs';
  static const _logIndexKey = 'app_log_file_index';
  static const _logContentPrefix = 'app_log_file_content::';

  static String newBatchId() {
    final now = DateTime.now();
    final millis = now.millisecondsSinceEpoch;
    return 'batch_${now.year.toString().padLeft(4, '0')}${now.month.toString().padLeft(2, '0')}${now.day.toString().padLeft(2, '0')}_$millis';
  }

  static String currentBatchId() {
    final saved = AppStorage.getString(_batchKey);
    if (saved != null && saved.isNotEmpty) return saved;
    return 'batch_uninitialized';
  }

  static String? currentLogFilePath() =>
      AppStorage.getString(_currentLogPathKey);

  static String _contentKey(String path) => '$_logContentPrefix$path';

  static List<String> _readIndex() {
    final raw = AppStorage.getString(_logIndexKey);
    if (raw == null || raw.isEmpty) return const [];
    try {
      final decoded = jsonDecode(raw);
      if (decoded is List) {
        return decoded.whereType<String>().toList(growable: false);
      }
    } catch (_) {}
    return const [];
  }

  static Future<void> _writeIndex(List<String> paths) async {
    await AppStorage.setString(_logIndexKey, jsonEncode(paths));
  }

  static Future<void> _saveContent(String path, String content) async {
    await AppStorage.setString(_contentKey(path), content);
  }

  static String _readContent(String path) =>
      AppStorage.getString(_contentKey(path)) ?? '';

  static Future<String> startNewBatch({String reason = ''}) async {
    final batchId = newBatchId();
    await AppStorage.setString(_batchKey, batchId);
    final pkg = await AppVersionService.load();
    final now = DateTime.now();
    final filename =
        '${now.year.toString().padLeft(4, '0')}${now.month.toString().padLeft(2, '0')}${now.day.toString().padLeft(2, '0')}_'
        '${now.hour.toString().padLeft(2, '0')}${now.minute.toString().padLeft(2, '0')}${now.second.toString().padLeft(2, '0')}_'
        '$batchId.log';
    final path = 'web://diagnostic-logs/$filename';
    final header = StringBuffer()
      ..writeln('app_version=${pkg.versionLabel}')
      ..writeln('base_url=${AppStorage.getBaseUrl()}')
      ..writeln('logged_in=${AppStorage.isLoggedIn()}')
      ..writeln('current_batch_id=$batchId')
      ..writeln('started_at=${DateTime.now().toIso8601String()}')
      ..writeln('reason=$reason')
      ..writeln();
    await _saveContent(path, header.toString());
    await AppStorage.setString(_currentLogPathKey, path);
    await AppStorage.remove(_legacyStorageKey);
    final index = _readIndex().where((item) => item != path).toList();
    index.add(path);
    await _writeIndex(index);
    await append(
      'log_batch_started',
      data: {'batch_id': batchId, 'reason': reason, 'log_file': path},
    );
    return batchId;
  }

  static Future<void> append(
    String event, {
    String level = 'info',
    Map<String, dynamic>? data,
  }) async {
    var path = currentLogFilePath();
    if (path == null || path.isEmpty) {
      await startNewBatch(reason: 'auto_recover');
      path = currentLogFilePath();
    }
    if (path == null || path.isEmpty) return;
    final entry = UpdateLogEntry(
      time: DateTime.now().toIso8601String(),
      batchId: currentBatchId(),
      level: level,
      event: event,
      data: data ?? const {},
    );
    final line =
        '[${entry.time}] [${entry.level}] [${entry.batchId}] ${entry.event} ${jsonEncode(entry.data)}\n';
    final content = _readContent(path);
    await _saveContent(path, '$content$line');
  }

  static Future<List<UpdateLogEntry>> list() async {
    final content = await exportText();
    final lines = content.split('\n');
    final result = <UpdateLogEntry>[];
    for (final line in lines) {
      if (!line.startsWith('[')) continue;
      final match = RegExp(
        r'^\[(.*?)\] \[(.*?)\] \[(.*?)\] ([^ ]+) (.*)$',
      ).firstMatch(line);
      if (match == null) continue;
      try {
        final json = jsonDecode(match.group(5)!) as Map<String, dynamic>;
        result.add(
          UpdateLogEntry(
            time: match.group(1)!,
            level: match.group(2)!,
            batchId: match.group(3)!,
            event: match.group(4)!,
            data: json,
          ),
        );
      } catch (_) {}
    }
    return result;
  }

  static Future<void> clear() async {
    final index = _readIndex();
    for (final path in index) {
      await AppStorage.remove(_contentKey(path));
    }
    await AppStorage.remove(_logIndexKey);
    await AppStorage.remove(_batchKey);
    await AppStorage.remove(_currentLogPathKey);
    await AppStorage.remove(_legacyStorageKey);
  }

  static Future<String> exportText() async {
    var path = currentLogFilePath();
    if (path == null || path.isEmpty) {
      final index = _readIndex();
      if (index.isNotEmpty) {
        path = index.last;
        await AppStorage.setString(_currentLogPathKey, path);
      }
    }
    if (path == null || path.isEmpty) {
      await startNewBatch(reason: 'auto_recover');
      path = currentLogFilePath();
    }
    if (path == null || path.isEmpty) {
      return 'log_file=\n';
    }
    final content = _readContent(path);
    return 'log_file=$path\n$content';
  }

  static Future<void> report({String note = ''}) async {
    final pkg = await AppVersionService.load();
    final content = await exportText();
    final payload = {
      'category': 'app_update',
      'app_version': pkg.versionLabel,
      'base_url': AppStorage.getBaseUrl(),
      'note': note,
      'content': content,
    };
    await ApiClient.post('/api/support/logs', payload);
  }
}
