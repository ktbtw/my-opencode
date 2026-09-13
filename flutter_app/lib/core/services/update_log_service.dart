import 'update_log_models.dart';
import 'update_log_service_io.dart'
    if (dart.library.html) 'update_log_service_web.dart'
    as impl;

export 'update_log_models.dart';

class UpdateLogService {
  static String currentBatchId() =>
      impl.UpdateLogServicePlatform.currentBatchId();

  static String? currentLogFilePath() =>
      impl.UpdateLogServicePlatform.currentLogFilePath();

  static Future<String> startNewBatch({String reason = ''}) =>
      impl.UpdateLogServicePlatform.startNewBatch(reason: reason);

  static Future<void> append(
    String event, {
    String level = 'info',
    Map<String, dynamic>? data,
  }) => impl.UpdateLogServicePlatform.append(event, level: level, data: data);

  static Future<List<UpdateLogEntry>> list() =>
      impl.UpdateLogServicePlatform.list();

  static Future<void> clear() => impl.UpdateLogServicePlatform.clear();

  static Future<String> exportText() =>
      impl.UpdateLogServicePlatform.exportText();

  static Future<void> report({String note = ''}) =>
      impl.UpdateLogServicePlatform.report(note: note);
}
