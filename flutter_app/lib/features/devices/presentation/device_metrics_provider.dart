import 'dart:async';
import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/config/api_client.dart';
import '../../../core/services/app_log_service.dart';
import '../data/device_metrics_model.dart';

/// 设备指标的实时更新流。
///
/// 复用服务端的 overlay SSE：设备每上报一次心跳，服务端就广播一条
/// `device.metrics` 事件，这里按 machine_id 过滤出目标设备。
///
/// 断线时按秒数退避重连；设备离线或服务端未推送时，
/// 界面回退到 REST 快照并显示数据新鲜度。
Stream<DeviceMetrics> watchDeviceMetrics(String machineId) {
  final target = machineId.trim();
  if (target.isEmpty) return const Stream.empty();
  return _deviceMetricsStream(target);
}

Stream<DeviceMetrics> _deviceMetricsStream(String machineId) async* {
  var attempt = 0;
  while (true) {
    try {
      await for (final event in ApiClient.sseEvents('/api/overlay/events')) {
        attempt = 0;
        // 只处理指标事件，其余类型交给其它消费者。
        if (event.type != 'device.metrics') continue;
        final metrics = _metricsFromEvent(event.data, machineId);
        if (metrics != null) yield metrics;
      }
      // 服务端正常关闭流，交由调用方的重连策略处理。
      return;
    } catch (error) {
      attempt += 1;
      unawaited(
        AppLogService.log(
          'device_metrics_stream_error',
          data: {'machine_id': machineId, 'attempt': attempt},
        ),
      );
      // 退避封顶 15 秒，避免设备长时间离线时空转。
      await Future<void>.delayed(Duration(seconds: attempt.clamp(1, 15)));
    }
  }
}

DeviceMetrics? _metricsFromEvent(String raw, String machineId) {
  if (raw.isEmpty) return null;
  try {
    final json = jsonDecode(raw);
    if (json is! Map<String, dynamic>) return null;
    final id = (json['machine_id'] as String? ?? '').trim();
    if (id != machineId) return null;
    final rawMetrics = json['metrics'];
    if (rawMetrics is! Map<String, dynamic>) return null;
    return DeviceMetrics.fromJson(rawMetrics);
  } catch (_) {
    return null;
  }
}

/// 订阅指定设备的指标更新。
/// 没有推送时不产生任何值，调用方应结合 REST 快照一起使用。
final deviceMetricsLiveProvider =
    StreamProvider.autoDispose.family<DeviceMetrics, String>((ref, machineId) {
      return watchDeviceMetrics(machineId);
    });
