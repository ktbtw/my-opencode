import 'update_log_service.dart';

class AppLogService {
  static String currentBatchId() => UpdateLogService.currentBatchId();

  static Future<String> startNewBatch({String reason = ''}) =>
      UpdateLogService.startNewBatch(reason: reason);

  static Future<void> log(
    String event, {
    String level = 'info',
    Map<String, dynamic>? data,
  }) {
    return UpdateLogService.append(event, level: level, data: data);
  }

  static Future<void> report({String note = ''}) {
    return UpdateLogService.report(note: note);
  }
}
