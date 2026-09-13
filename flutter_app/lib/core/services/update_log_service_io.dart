import 'dart:convert';
import 'dart:io';

import 'package:package_info_plus/package_info_plus.dart';
import 'package:path_provider/path_provider.dart';

import '../config/api_client.dart';
import '../storage/app_storage.dart';
import 'update_log_models.dart';

class UpdateLogServicePlatform {
  static const _batchKey = 'app_log_batch_id';
  static const _currentLogPathKey = 'app_current_log_path';
  static const _legacyStorageKey = 'app_update_debug_logs';
  static const _logDirName = 'diagnostic-logs';

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

  static Future<Directory> _logDirectory() async {
    final baseDir = await getApplicationSupportDirectory();
    final logDir = Directory('${baseDir.path}/$_logDirName');
    if (!await logDir.exists()) {
      await logDir.create(recursive: true);
    }
    return logDir;
  }

  static Future<File> _latestLogFile() async {
    final currentPath = currentLogFilePath();
    if (currentPath != null && currentPath.isNotEmpty) {
      final currentFile = File(currentPath);
      if (await currentFile.exists()) return currentFile;
    }
    final logDir = await _logDirectory();
    final files = await logDir
        .list()
        .where((item) => item is File && item.path.endsWith('.log'))
        .cast<File>()
        .toList();
    files.sort((a, b) => b.path.compareTo(a.path));
    if (files.isNotEmpty) {
      await AppStorage.setString(_currentLogPathKey, files.first.path);
      return files.first;
    }
    await startNewBatch(reason: 'auto_recover');
    return File(currentLogFilePath()!);
  }

  static Future<File> _currentLogFile() async {
    final path = currentLogFilePath();
    if (path != null && path.isNotEmpty) {
      final file = File(path);
      if (await file.exists()) return file;
    }
    await startNewBatch(reason: 'auto_recover');
    return File(currentLogFilePath()!);
  }

  static Future<String> startNewBatch({String reason = ''}) async {
    final batchId = newBatchId();
    await AppStorage.setString(_batchKey, batchId);
    final pkg = await PackageInfo.fromPlatform();
    final logDir = await _logDirectory();
    final now = DateTime.now();
    final filename =
        '${now.year.toString().padLeft(4, '0')}${now.month.toString().padLeft(2, '0')}${now.day.toString().padLeft(2, '0')}_'
        '${now.hour.toString().padLeft(2, '0')}${now.minute.toString().padLeft(2, '0')}${now.second.toString().padLeft(2, '0')}_'
        '$batchId.log';
    final file = File('${logDir.path}/$filename');
    final header = StringBuffer()
      ..writeln('app_version=${pkg.version}+${pkg.buildNumber}')
      ..writeln('base_url=${AppStorage.getBaseUrl()}')
      ..writeln('logged_in=${AppStorage.isLoggedIn()}')
      ..writeln('current_batch_id=$batchId')
      ..writeln('started_at=${DateTime.now().toIso8601String()}')
      ..writeln('reason=$reason')
      ..writeln();
    await file.writeAsString(header.toString(), flush: true);
    await AppStorage.setString(_currentLogPathKey, file.path);
    await AppStorage.remove(_legacyStorageKey);
    await append(
      'log_batch_started',
      data: {'batch_id': batchId, 'reason': reason, 'log_file': file.path},
    );
    return batchId;
  }

  static Future<void> append(
    String event, {
    String level = 'info',
    Map<String, dynamic>? data,
  }) async {
    final entry = UpdateLogEntry(
      time: DateTime.now().toIso8601String(),
      batchId: currentBatchId(),
      level: level,
      event: event,
      data: data ?? const {},
    );
    final file = await _currentLogFile();
    final line =
        '[${entry.time}] [${entry.level}] [${entry.batchId}] ${entry.event} ${jsonEncode(entry.data)}\n';
    await file.writeAsString(line, mode: FileMode.append, flush: true);
  }

  static Future<List<UpdateLogEntry>> list() async {
    final file = await _latestLogFile();
    if (!await file.exists()) return [];
    final lines = await file.readAsLines();
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
    final logDir = await _logDirectory();
    if (await logDir.exists()) {
      await logDir.delete(recursive: true);
    }
    await AppStorage.remove(_batchKey);
    await AppStorage.remove(_currentLogPathKey);
    await AppStorage.remove(_legacyStorageKey);
  }

  static Future<String> exportText() async {
    final file = await _latestLogFile();
    final content = await file.readAsString();
    return 'log_file=${file.path}\n$content';
  }

  static Future<void> report({String note = ''}) async {
    final pkg = await PackageInfo.fromPlatform();
    final file = await _latestLogFile();
    final payload = {
      'category': 'app_update',
      'app_version': '${pkg.version}+${pkg.buildNumber}',
      'base_url': AppStorage.getBaseUrl(),
      'note': note,
      'content': 'log_file=${file.path}\n${await file.readAsString()}',
    };
    await ApiClient.post('/api/support/logs', payload);
  }
}
