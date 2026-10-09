import 'package:flutter/foundation.dart';

/// 一条 SSE 流的身份。
///
/// 通道名由请求路径统一推导，传输层（socket）与业务层（订阅方）使用同一个
/// 标识，因此同一个连接在两端打印的名称完全一致，可以直接对照排查。
class SseChannel {
  /// 中文业务名，例如「全局状态流」「对话任务流」。
  final String name;

  /// 业务标识（taskId / sessionId / jobId 等），全局流为空。
  final String scope;

  const SseChannel(this.name, [this.scope = '']);

  /// 日志前缀，例如 `对话任务流[task_123]`。
  String get label => scope.isEmpty ? name : '$name[$scope]';

  /// 全局状态流：设备列表、Agent 状态、任务状态、完成通知。
  static const overlay = SseChannel('全局状态流');

  /// 对话任务流：Agent 正文、思考过程、工具调用。
  static SseChannel task(String taskId) => SseChannel('对话任务流', taskId);

  /// 待发送队列流。
  static SseChannel queue(String sessionId) =>
      SseChannel('待发送队列流', sessionId);

  /// 项目记忆整理任务流。
  static SseChannel memoryJob(String jobId) => SseChannel('项目记忆流', jobId);

  /// 模型测速流。
  static SseChannel modelTest(String testId) =>
      SseChannel('模型测速流', testId);

  /// 按请求路径推导通道。
  ///
  /// 服务器地址可能带路径前缀（例如 `https://host/codex`），因此这里按路径
  /// **结尾**匹配，而不是从开头锚定。无法识别时保留原始路径，便于发现新增端点。
  static SseChannel fromPath(String path) {
    if (path.endsWith('/api/overlay/events')) return overlay;
    final task = RegExp(r'/api/tasks/([^/]+)/events$').firstMatch(path);
    if (task != null) return SseChannel.task(task.group(1)!);
    final queue = RegExp(
      r'/api/sessions/([^/]+)/queue/events$',
    ).firstMatch(path);
    if (queue != null) return SseChannel.queue(queue.group(1)!);
    final memory = RegExp(
      r'/api/devices/[^/]+/projects/[^/]+/memory-jobs/([^/]+)/events$',
    ).firstMatch(path);
    if (memory != null) return SseChannel.memoryJob(memory.group(1)!);
    final modelTest = RegExp(
      r'/api/model-tests/([^/]+)/events$',
    ).firstMatch(path);
    if (modelTest != null) return SseChannel.modelTest(modelTest.group(1)!);
    return SseChannel('未识别事件流', path);
  }
}

/// SSE 长连接全链路日志。
///
/// 记录一条事件流从「发起连接」到「建立 → 收数据 → 断开 → 重连」的完整周期。
/// 只在生命周期节点与关闭汇总处输出，不逐条打印业务事件，避免刷屏。
class SseLog {
  const SseLog._();

  static void step(SseChannel channel, String message) {
    debugPrint('[SSE][${channel.label}] $message');
  }

  /// 发起连接。
  static void connecting(SseChannel channel, String url) =>
      step(channel, '发起连接 → $url');

  /// 收到 HTTP 响应头。
  static void responded(SseChannel channel, int statusCode) =>
      step(channel, '服务端响应，状态码=$statusCode');

  /// 连接建立成功（握手完成）。
  static void connected(SseChannel channel, int handshakeMs) =>
      step(channel, '连接已建立，握手耗时$handshakeMs毫秒，等待事件推送');

  /// 首个数据块到达，标志连接真正开始工作。
  static void firstData(SseChannel channel, int bytes) =>
      step(channel, '收到首批数据（$bytes 字节），连接工作正常');

  /// 首个心跳（SSE 注释行），用于确认链路存活但暂无业务事件。
  static void firstHeartbeat(SseChannel channel) =>
      step(channel, '收到首个心跳，链路存活');

  /// 空闲超时（连心跳都没有）触发主动断开。
  static void idleTimeout(SseChannel channel, int seconds) =>
      step(channel, '空闲超时：$seconds秒内没有任何数据（含心跳），主动断开重连');

  /// 连接关闭，附带本次连接的统计。
  static void closed(
    SseChannel channel, {
    required String reason,
    required int durationMs,
    required int bytes,
    required int events,
    required int heartbeats,
  }) => step(
    channel,
    '连接结束（$reason）：持续$durationMs毫秒，'
    '收到$bytes字节 / $events个事件 / $heartbeats个心跳',
  );

  /// 安排一次重连退避。
  static void retry(
    SseChannel channel,
    int attempt,
    int delayMs,
    String reason,
  ) => step(
    channel,
    '准备第$attempt次重连，等待$delayMs毫秒后重试，原因：$reason',
  );

  /// 调用方主动取消连接。
  static void cancelled(SseChannel channel) =>
      step(channel, '连接已取消（订阅方主动关闭）');

  /// 连接异常。
  static void error(SseChannel channel, Object error) =>
      step(channel, '连接异常：$error');

  /// 鉴权失效，刷新令牌后重连。
  static void authRefresh(SseChannel channel) =>
      step(channel, '鉴权失效（401），刷新令牌后重连');

  /// 业务层订阅建立。
  static void subscribed(SseChannel channel, String detail) =>
      step(channel, '订阅开始：$detail');

  /// 首个业务事件到达。
  static void firstEvent(SseChannel channel, String type) =>
      step(channel, '收到首个业务事件 type=$type');

  /// 业务层汇总（连接中断或结束时）。
  static void summary(SseChannel channel, String message) =>
      step(channel, '汇总：$message');
}
