import 'dart:async';
import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/services/app_live_events.dart';
import '../../../core/services/provider_stream.dart';
import '../../../core/services/app_foreground.dart';
import '../../../core/storage/app_storage.dart';
import '../../../core/config/sse_parser.dart';
import '../data/device_metrics_model.dart';

/// Device consumers filter the one account-wide overlay subscription.
Stream<DeviceMetrics> watchDeviceMetrics(
  String machineId, {
  required Stream<SseEvent> events,
}) => events
    .where((event) => event.type == 'device.metrics')
    .map((event) => _metricsFromEvent(event.data, machineId))
    .where((metrics) => metrics != null)
    .cast<DeviceMetrics>();

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
final deviceMetricsLiveProvider = StreamProvider.autoDispose
    .family<DeviceMetrics, String>((ref, machineId) {
      final events = ref.watch(appLiveEventsProvider);
      final foreground = ref.watch(appForegroundProvider);
      if (!foreground || !AppStorage.initialized || !AppStorage.isLoggedIn()) {
        return const Stream.empty();
      }
      return providerOwnedStream(
        ref,
        watchDeviceMetrics(machineId, events: events.watch()),
      );
    });
