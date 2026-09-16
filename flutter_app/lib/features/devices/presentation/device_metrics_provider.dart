import 'dart:async';
import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/config/api_client.dart';
import '../../../core/config/sse_parser.dart';
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

/// 用显式订阅实现，而不是 async*。
///
/// async* 生成器在 await 期间无法被取消，取消只在下一个 yield 时才生效：
/// 退出设备页后退避定时器仍会跑完并额外发起一次 SSE 连接。
/// 显式订阅可以立刻停掉定时器与底层连接，不留悬挂等待。
Stream<DeviceMetrics> _deviceMetricsStream(String machineId) {
  final controller = StreamController<DeviceMetrics>();
  Timer? backoff;
  Completer<void>? sleeping;
  Completer<void>? connected;
  StreamSubscription<SseEvent>? subscription;
  var attempt = 0;
  var closed = false;

  // 让等待退避的循环立刻恢复，从而走到 closed 判断并退出。
  void wake(Completer<void>? target) {
    if (target != null && !target.isCompleted) target.complete();
  }

  Future<void> loop() async {
    while (!closed) {
      final done = Completer<void>();
      connected = done;
      subscription = ApiClient.sseEvents('/api/overlay/events').listen(
        (event) {
          if (closed) return;
          attempt = 0;
          // 只处理指标事件，其余类型交给其它消费者。
          if (event.type != 'device.metrics') return;
          final metrics = _metricsFromEvent(event.data, machineId);
          if (metrics != null && !controller.isClosed) controller.add(metrics);
        },
        onError: (Object error) {
          attempt += 1;
          unawaited(
            AppLogService.log(
              'device_metrics_stream_error',
              data: {'machine_id': machineId, 'attempt': attempt},
            ),
          );
          wake(done);
        },
        // 服务端关闭连接（重启、网络切换）也要重连，
        // 否则页面开着时指标推送会永久中断。
        onDone: () {
          attempt += 1;
          wake(done);
        },
        cancelOnError: false,
      );

      await done.future;
      final current = subscription;
      subscription = null;
      connected = null;
      await current?.cancel();
      if (closed) return;

      // 退避封顶 15 秒，避免设备离线或服务端不可达时空转。
      final waited = Completer<void>();
      sleeping = waited;
      backoff = Timer(Duration(seconds: attempt.clamp(1, 15)), () {
        wake(waited);
      });
      await waited.future;
      backoff = null;
      sleeping = null;
    }
  }

  controller.onListen = () => unawaited(loop());
  controller.onCancel = () async {
    closed = true;
    backoff?.cancel();
    backoff = null;
    wake(sleeping);
    wake(connected);
    final current = subscription;
    subscription = null;
    await current?.cancel();
  };
  return controller.stream;
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
