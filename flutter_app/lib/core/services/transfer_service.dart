import 'dart:async';

import 'package:flutter/services.dart';

/// 传输前台服务：上传/下载期间让系统保持 App 前台优先级，
/// 避免退到后台或锁屏后网络被暂停、进程被回收。
class TransferService {
  static const _channel = MethodChannel('chat_codex/transfer_service');

  static Future<void> start({
    required String title,
    String text = '保持后台运行',
  }) async {
    try {
      await _channel.invokeMethod<void>('start', {
        'title': title,
        'text': text,
      });
    } catch (_) {
      // 平台不支持（桌面/Web）或调用失败时忽略，不影响传输本身。
    }
  }

  /// 更新通知内容与进度（[progress] 为 0~100，-1 表示不确定进度）。
  static Future<void> update({
    required String title,
    String text = '保持后台运行',
    int progress = -1,
  }) async {
    try {
      await _channel.invokeMethod<void>('update', {
        'title': title,
        'text': text,
        'progress': progress,
      });
    } catch (_) {}
  }

  static Future<void> stop() async {
    try {
      await _channel.invokeMethod<void>('stop');
    } catch (_) {}
  }
}
