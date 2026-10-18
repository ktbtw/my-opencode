import 'dart:async';

import '../config/api_client.dart';

/// 关键链路诊断日志：直接上报到服务端日志（`[appdiag]`），
/// 便于在拿不到 App 本地日志时排查通知/轮询类问题。
class DiagLog {
  static void emit(String event, [Map<String, dynamic>? data]) {
    unawaited(_send(event, data ?? const <String, dynamic>{}));
  }

  static Future<void> _send(String event, Map<String, dynamic> data) async {
    try {
      await ApiClient.post('/api/diag/log', {
        'event': event,
        'data': data,
      }).timeout(const Duration(seconds: 5));
    } catch (_) {
      // 诊断上报失败不影响主流程
    }
  }
}
