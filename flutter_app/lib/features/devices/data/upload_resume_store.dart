import 'dart:convert';

import 'package:crypto/crypto.dart';

import '../../../core/storage/app_storage.dart';

/// 记住某个上传目标正在进行的分块上传 id，让重试能够真正续传。
///
/// 设备端把续传会话目录按 upload_id 命名，而上传流程每次都会生成新 id，
/// 于是重选同一个文件时 `upload/status` 查到的是空会话，只能整份重传。
/// 这里按「上传接口 + 目标路径 + 文件大小」记住 id，重试时复用同一个会话，
/// 设备侧已经写好的分块就能被跳过。
class UploadResumeStore {
  static const _prefix = 'device_upload_resume_v1';

  static String key({
    required String basePath,
    required String path,
    required int size,
  }) {
    final digest = sha256.convert(utf8.encode('$basePath|$path|$size'));
    return '$_prefix:${digest.toString()}';
  }

  static String? read(String key) {
    if (!AppStorage.initialized) return null;
    final value = AppStorage.getString(key)?.trim();
    if (value == null || value.isEmpty) return null;
    return value;
  }

  static Future<void> save(String key, String uploadId) async {
    if (!AppStorage.initialized) return;
    await AppStorage.setString(key, uploadId);
  }

  static Future<void> clear(String key) async {
    if (!AppStorage.initialized) return;
    await AppStorage.remove(key);
  }
}
