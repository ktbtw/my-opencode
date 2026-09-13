import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:open_filex/open_filex.dart';
import 'package:path_provider/path_provider.dart';

import 'app_update_executor_models.dart';
import 'app_update_models.dart';

Future<AppUpdateExecutionResult> downloadAndLaunchAppUpdate({
  required AppUpdateInfo info,
  required String baseUrl,
  required String token,
  AppUpdateProgress? onProgress,
}) async {
  final asset = info.asset;
  if (asset == null || !info.hasInstallAsset) {
    throw StateError('版本 ${info.version} 未发布 ${info.platform} 安装包');
  }
  _validateInstaller(asset);
  final uri = _resolveDownloadUri(baseUrl, asset.url);
  final dir = await getTemporaryDirectory();
  final filename = _safeFilename(asset.filename);
  final target = File('${dir.path}${Platform.pathSeparator}$filename');
  final partial = File('${target.path}.part');

  if (await target.exists() &&
      asset.sha256.isNotEmpty &&
      await _matchesSHA256(target, asset.sha256)) {
    final size = await target.length();
    onProgress?.call(size, size);
  } else {
    if (await target.exists()) await target.delete();
    await _downloadResumable(
      uri: uri,
      target: target,
      partial: partial,
      expectedSHA256: asset.sha256,
      token: token,
      onProgress: onProgress,
    );
  }

  if (Platform.isWindows) {
    await Process.start(
      target.path,
      const [],
      mode: ProcessStartMode.detached,
      runInShell: false,
    );
    // Let the installer take ownership of the current executable. Inno Setup
    // can close the app through Restart Manager, but an explicit exit avoids
    // leaving the old EXE locked when Flutter or a child process is still
    // shutting down.
    await Future<void>.delayed(const Duration(milliseconds: 350));
    exit(0);
  }
  if (Platform.isMacOS) {
    await Process.start('open', [target.path], mode: ProcessStartMode.detached);
    return AppUpdateExecutionResult(
      filePath: target.path,
      message: 'macOS 安装程序已打开',
    );
  }
  if (Platform.isAndroid) {
    final result = await OpenFilex.open(target.path);
    if (result.type != ResultType.done) {
      throw StateError('打开 Android 安装包失败: ${result.message}');
    }
    return AppUpdateExecutionResult(
      filePath: target.path,
      message: 'Android 安装程序已打开',
    );
  }
  throw UnsupportedError('当前平台不支持执行安装包');
}

Future<void> _downloadResumable({
  required Uri uri,
  required File target,
  required File partial,
  required String expectedSHA256,
  required String token,
  required AppUpdateProgress? onProgress,
}) async {
  final dio = Dio();
  var resetUsed = false;
  while (true) {
    var offset = await partial.exists() ? await partial.length() : 0;
    final headers = <String, String>{};
    if (token.trim().isNotEmpty) {
      headers['Authorization'] = 'Bearer ${token.trim()}';
    }
    if (offset > 0) headers['Range'] = 'bytes=$offset-';
    final response = await dio.get<ResponseBody>(
      uri.toString(),
      options: Options(
        responseType: ResponseType.stream,
        headers: headers,
        validateStatus: (status) =>
            status == 200 || status == 206 || status == 416,
      ),
    );
    final status = response.statusCode ?? 0;
    if (status == 416 && offset > 0) {
      final total = _rangeTotal(response.headers.value('content-range'));
      if (total == offset) {
        if (await _finalizeDownload(partial, target, expectedSHA256)) return;
      }
      if (resetUsed) throw StateError('服务端返回的续传范围无效');
      if (await partial.exists()) await partial.delete();
      resetUsed = true;
      continue;
    }
    if (status != 200 && status != 206) {
      throw HttpException('下载安装包失败: HTTP $status', uri: uri);
    }

    var append = status == 206 && offset > 0;
    var total = _contentLength(response.headers);
    if (status == 206) {
      final range = _parseContentRange(response.headers.value('content-range'));
      if (range == null || range.start != offset) {
        if (resetUsed) throw StateError('服务端返回的续传起点无效');
        if (await partial.exists()) await partial.delete();
        resetUsed = true;
        continue;
      }
      total = range.total > 0 ? range.total : offset + total;
    } else {
      offset = 0;
      append = false;
    }

    final sink = partial.openWrite(
      mode: append ? FileMode.append : FileMode.write,
    );
    var received = offset;
    onProgress?.call(received, total);
    try {
      final stream = response.data?.stream;
      if (stream == null) throw StateError('安装包响应为空');
      await for (final chunk in stream) {
        sink.add(chunk);
        received += chunk.length;
        onProgress?.call(received, total);
      }
      await sink.flush();
    } finally {
      await sink.close();
    }
    if (total > 0 && received != total) {
      throw StateError('安装包下载提前结束: $received/$total');
    }
    if (await _finalizeDownload(partial, target, expectedSHA256)) return;
    if (resetUsed) throw StateError('安装包 SHA-256 校验失败');
    if (await partial.exists()) await partial.delete();
    resetUsed = true;
  }
}

Future<bool> _finalizeDownload(
  File partial,
  File target,
  String expectedSHA256,
) async {
  if (!await partial.exists()) return false;
  if (expectedSHA256.isNotEmpty &&
      !await _matchesSHA256(partial, expectedSHA256)) {
    return false;
  }
  if (await target.exists()) await target.delete();
  await partial.rename(target.path);
  return true;
}

Future<bool> _matchesSHA256(File file, String expected) async {
  final digest = await sha256.bind(file.openRead()).first;
  return digest.toString().toLowerCase() == expected.trim().toLowerCase();
}

Uri _resolveDownloadUri(String baseUrl, String raw) {
  final uri = Uri.parse(raw);
  if (uri.hasScheme) return uri;
  final base = Uri.parse(baseUrl.endsWith('/') ? baseUrl : '$baseUrl/');
  return base.resolveUri(uri);
}

String _safeFilename(String raw) {
  final normalized = raw.replaceAll('\\', '/');
  final filename = normalized.split('/').last.trim();
  if (filename.isEmpty || filename == '.' || filename == '..') {
    throw StateError('安装包文件名无效');
  }
  return filename;
}

void _validateInstaller(AppUpdateAsset asset) {
  final name = asset.filename.toLowerCase();
  if (!RegExp(r'^[0-9a-fA-F]{64}$').hasMatch(asset.sha256.trim())) {
    throw StateError('安装包缺少有效的 SHA-256，已停止更新');
  }
  if (Platform.isWindows && !name.endsWith('.exe')) {
    throw StateError('Windows 更新资源不是 EXE 安装程序');
  }
  if (Platform.isAndroid && !name.endsWith('.apk')) {
    throw StateError('Android 更新资源不是 APK 安装包');
  }
  if (Platform.isMacOS &&
      !name.endsWith('.dmg') &&
      !name.endsWith('.pkg') &&
      !name.endsWith('.zip')) {
    throw StateError('macOS 更新资源格式不受支持');
  }
}

int _contentLength(Headers headers) {
  return int.tryParse(headers.value('content-length') ?? '') ?? -1;
}

int _rangeTotal(String? raw) {
  if (raw == null) return -1;
  final slash = raw.lastIndexOf('/');
  if (slash < 0) return -1;
  return int.tryParse(raw.substring(slash + 1).trim()) ?? -1;
}

({int start, int total})? _parseContentRange(String? raw) {
  if (raw == null) return null;
  final match = RegExp(r'^bytes\s+(\d+)-\d+/(\d+|\*)$').firstMatch(raw.trim());
  if (match == null) return null;
  return (
    start: int.parse(match.group(1)!),
    total: int.tryParse(match.group(2)!) ?? -1,
  );
}
