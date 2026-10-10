import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/devices/data/device_repository.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    // 策略有静态缓存，逐个用例隔离，避免互相影响。
    DeviceRepository.invalidateUploadPolicyCache();
  });

  test('会员策略下按 16MB 分块并发上传', () async {
    final previousOverride = HttpOverrides.current;
    HttpOverrides.global = null;
    addTearDown(() => HttpOverrides.global = previousOverride);

    SharedPreferences.setMockInitialValues({'access_token': 'test-token'});
    await AppStorage.init();

    final requests = <_CapturedRequest>[];
    var inFlight = 0;
    var maxInFlight = 0;
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final sub = server.listen((request) async {
      final raw = await _collectBytes(request);
      final captured = parseCapturedRequest(
        request.uri.path,
        request.headers.contentType?.toString() ?? '',
        raw,
      );
      final body = captured.body;
      requests.add(captured);
      request.response.headers.contentType = ContentType.json;
      if (request.uri.path.endsWith('/upload-policy')) {
        request.response.write(
          jsonEncode({
            'policy': {
              'tier': 'plus',
              'is_member': true,
              'chunk_size': 16 * 1024 * 1024,
              'max_chunk_size': 16 * 1024 * 1024,
              'concurrency': 3,
              'resume_enabled': true,
            },
          }),
        );
        await request.response.close();
        return;
      }
      if (request.uri.path.endsWith('/chunk')) {
        inFlight += 1;
        if (inFlight > maxInFlight) maxInFlight = inFlight;
        // 让请求重叠，用于观测并发度。
        await Future<void>.delayed(const Duration(milliseconds: 120));
        inFlight -= 1;
      }
      request.response.write(
        jsonEncode({
          'file': {'path': body['path'] ?? 'x.bin', 'name': 'x.bin', 'size': 0},
        }),
      );
      await request.response.close();
    });
    addTearDown(() async {
      await sub.cancel();
      await server.close(force: true);
    });
    await AppStorage.setBaseUrl('http://${server.address.host}:${server.port}');

    final repo = DeviceRepository();
    // 3 个分块，使用会员策略（未显式指定 chunkSize/concurrency）。
    const totalBytes = 3 * 1024 * 1024;
    final bytes = Uint8List(totalBytes);
    for (var i = 0; i < totalBytes; i++) {
      bytes[i] = i % 251;
    }

    final policy = await repo.fetchUploadPolicy();
    expect(policy.isMember, isTrue);
    expect(policy.chunkSize, 16 * 1024 * 1024);
    expect(policy.concurrency, 3);

    await repo.uploadDeviceAgentFileChunkedStreamed(
      machineId: 'm',
      agentId: 'a',
      path: 'big.bin',
      totalBytes: totalBytes,
      openRead: () => Stream<List<int>>.value(bytes),
    );

    // 16MB 分块 + 3MB 文件 => 单块，不会并发。
    expect(maxInFlight, 1);

    // 换成 3 个 16MB 块，验证并发确实发生。
    requests.clear();
    inFlight = 0;
    maxInFlight = 0;
    const threeChunks = 3 * 16 * 1024 * 1024;
    final bigBytes = Uint8List(threeChunks);
    await repo.uploadDeviceAgentFileChunkedStreamed(
      machineId: 'm',
      agentId: 'a',
      path: 'big3.bin',
      totalBytes: threeChunks,
      openRead: () => Stream<List<int>>.value(bigBytes),
    );

    final chunkRequests = requests
        .where((r) => r.path.endsWith('/chunk'))
        .toList();
    expect(chunkRequests, hasLength(3));
    expect(maxInFlight, greaterThan(1), reason: '会员上传应并发发送分块');
  });

  test('断点续传跳过设备已收到的分块', () async {
    final previousOverride = HttpOverrides.current;
    HttpOverrides.global = null;
    addTearDown(() => HttpOverrides.global = previousOverride);

    SharedPreferences.setMockInitialValues({'access_token': 'test-token'});
    await AppStorage.init();

    final requests = <_CapturedRequest>[];
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final sub = server.listen((request) async {
      final raw = await _collectBytes(request);
      final captured = parseCapturedRequest(
        request.uri.path,
        request.headers.contentType?.toString() ?? '',
        raw,
      );
      final body = captured.body;
      requests.add(captured);
      request.response.headers.contentType = ContentType.json;
      if (request.uri.path.endsWith('/status')) {
        // 设备侧已收到第 0、2 块，只剩第 1 块需要补传。
        request.response.write(
          jsonEncode({
            'status': {
              'path': body['path'],
              'upload_id': body['upload_id'],
              'size': body['size'],
              'total_chunks': body['total_chunks'],
              'received_chunks': [0, 2],
              'received_bytes': 6,
              'completed': false,
              'resumable': true,
            },
          }),
        );
      } else {
        request.response.write(
          jsonEncode({
            'file': {
              'path': body['path'] ?? 'x.bin',
              'name': 'x.bin',
              'size': 6,
            },
          }),
        );
      }
      await request.response.close();
    });
    addTearDown(() async {
      await sub.cancel();
      await server.close(force: true);
    });
    await AppStorage.setBaseUrl('http://${server.address.host}:${server.port}');

    final repo = DeviceRepository();
    final source = utf8.encode('hello');
    final file = await repo.uploadDeviceAgentFileChunkedStreamed(
      machineId: 'm',
      agentId: 'a',
      path: 'resume.bin',
      totalBytes: source.length,
      openRead: () => Stream<List<int>>.value(source),
      uploadId: 'up_resume',
      chunkSize: 2,
      concurrency: 1,
      resume: true,
    );

    expect(file.name, 'x.bin');
    // 先查询状态，再创建续传会话。
    expect(
      requests.map((r) => r.path.split('/').last),
      containsAllInOrder(['status', 'create']),
    );

    final create = requests.firstWhere((r) => r.path.endsWith('/create'));
    expect(create.body['resume'], isTrue);

    // 只应补传缺失的第 1 块，且 offset 必须按块下标计算。
    final chunks = requests.where((r) => r.path.endsWith('/chunk')).toList();
    expect(chunks, hasLength(1));
    // multipart 表单字段以字符串传输，数值断言按字符串比较。
    expect(chunks.single.body['chunk_index'], '1');
    expect(chunks.single.body['offset'], '2');
    // 分块以 multipart 原始字节提交，不再是 base64 字段。
    expect(utf8.decode(chunks.single.rawChunk!), 'll');

    // 已收到的分块也要计入进度。
    final complete = requests.firstWhere((r) => r.path.endsWith('/complete'));
    expect(complete.body['size'], source.length);
    expect(complete.body['sha256'], sha256.convert(source).toString());
  });

  test('分块上传失败时自动重试并最终成功', () async {
    final previousOverride = HttpOverrides.current;
    HttpOverrides.global = null;
    addTearDown(() => HttpOverrides.global = previousOverride);

    SharedPreferences.setMockInitialValues({'access_token': 'test-token'});
    await AppStorage.init();

    var chunkAttempts = 0;
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final sub = server.listen((request) async {
      final raw = await _collectBytes(request);
      final body = parseCapturedRequest(
        request.uri.path,
        request.headers.contentType?.toString() ?? '',
        raw,
      ).body;
      request.response.headers.contentType = ContentType.json;
      if (request.uri.path.endsWith('/chunk')) {
        chunkAttempts += 1;
        // 首次请求模拟链路失败，重试应成功。
        if (chunkAttempts == 1) {
          request.response.statusCode = HttpStatus.badGateway;
          request.response.write(jsonEncode({'error': 'upstream error'}));
          await request.response.close();
          return;
        }
      }
      request.response.write(
        jsonEncode({
          'file': {'path': body['path'] ?? 'x.bin', 'name': 'x.bin', 'size': 5},
        }),
      );
      await request.response.close();
    });
    addTearDown(() async {
      await sub.cancel();
      await server.close(force: true);
    });
    await AppStorage.setBaseUrl('http://${server.address.host}:${server.port}');

    final repo = DeviceRepository();
    final source = utf8.encode('hello');
    final file = await repo.uploadDeviceAgentFileChunkedStreamed(
      machineId: 'm',
      agentId: 'a',
      path: 'retry.bin',
      totalBytes: source.length,
      openRead: () => Stream<List<int>>.value(source),
      uploadId: 'up_retry',
      chunkSize: 2,
      concurrency: 1,
    );

    expect(file.name, 'x.bin');
    expect(chunkAttempts, greaterThan(1), reason: '首次失败后应重试');
  });

  test('普通用户保持 512KB 单并发', () async {
    final previousOverride = HttpOverrides.current;
    HttpOverrides.global = null;
    addTearDown(() => HttpOverrides.global = previousOverride);

    SharedPreferences.setMockInitialValues({
      'access_token': 'test-token',
      'membership_tier': 'free',
    });
    await AppStorage.init();

    final requests = <_CapturedRequest>[];
    var inFlight = 0;
    var maxInFlight = 0;
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final sub = server.listen((request) async {
      final raw = await _collectBytes(request);
      final captured = parseCapturedRequest(
        request.uri.path,
        request.headers.contentType?.toString() ?? '',
        raw,
      );
      final body = captured.body;
      requests.add(captured);
      request.response.headers.contentType = ContentType.json;
      if (request.uri.path.endsWith('/upload-policy')) {
        request.response.write(
          jsonEncode({
            'policy': {
              'tier': 'free',
              'is_member': false,
              'chunk_size': 512 * 1024,
              'max_chunk_size': 512 * 1024,
              'concurrency': 1,
              'resume_enabled': true,
            },
          }),
        );
        await request.response.close();
        return;
      }
      if (request.uri.path.endsWith('/chunk')) {
        inFlight += 1;
        if (inFlight > maxInFlight) maxInFlight = inFlight;
        await Future<void>.delayed(const Duration(milliseconds: 60));
        inFlight -= 1;
      }
      request.response.write(
        jsonEncode({
          'file': {'path': body['path'] ?? 'x.bin', 'name': 'x.bin', 'size': 0},
        }),
      );
      await request.response.close();
    });
    addTearDown(() async {
      await sub.cancel();
      await server.close(force: true);
    });
    await AppStorage.setBaseUrl('http://${server.address.host}:${server.port}');

    final repo = DeviceRepository();
    final policy = await repo.fetchUploadPolicy();
    expect(policy.isMember, isFalse);
    expect(policy.chunkSize, 512 * 1024);
    expect(policy.concurrency, 1);

    // 2MB => 4 个 512KB 分块，普通用户必须串行。
    const totalBytes = 2 * 1024 * 1024;
    final bytes = Uint8List(totalBytes);
    await repo.uploadDeviceAgentFileChunkedStreamed(
      machineId: 'm',
      agentId: 'a',
      path: 'free.bin',
      totalBytes: totalBytes,
      openRead: () => Stream<List<int>>.value(bytes),
    );

    final chunks = requests.where((r) => r.path.endsWith('/chunk')).toList();
    expect(chunks, hasLength(4));
    // 每块以原始字节传输，长度不应超过分块大小。
    for (final chunk in chunks) {
      expect(chunk.rawChunk, isNotNull);
      expect(chunk.rawChunk!.length, lessThanOrEqualTo(512 * 1024));
    }
    expect(maxInFlight, 1, reason: '普通用户上传应保持串行');
  });

  test('服务端策略接口不可用时回退到本地会员等级', () async {
    final previousOverride = HttpOverrides.current;
    HttpOverrides.global = null;
    addTearDown(() => HttpOverrides.global = previousOverride);

    SharedPreferences.setMockInitialValues({
      'access_token': 'test-token',
      'membership_tier': 'pro',
    });
    await AppStorage.init();

    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final sub = server.listen((request) async {
      await _collectBytes(request);
      // 策略接口直接失败，模拟网络异常。
      request.response.statusCode = HttpStatus.internalServerError;
      request.response.headers.contentType = ContentType.json;
      request.response.write(jsonEncode({'error': 'boom'}));
      await request.response.close();
    });
    addTearDown(() async {
      await sub.cancel();
      await server.close(force: true);
    });
    await AppStorage.setBaseUrl('http://${server.address.host}:${server.port}');

    final repo = DeviceRepository();
    final policy = await repo.fetchUploadPolicy(force: true);
    // 本地缓存为 pro，应回退为会员策略而不是普通策略。
    expect(policy.isMember, isTrue);
    expect(policy.chunkSize, 16 * 1024 * 1024);
    expect(policy.concurrency, greaterThan(1));
  });
}

class _CapturedRequest {
  final String path;
  final Map<String, dynamic> body;
  /// multipart 分块请求携带的原始字节；JSON 请求为 null。
  final List<int>? rawChunk;

  const _CapturedRequest({
    required this.path,
    required this.body,
    this.rawChunk,
  });
}

/// 解析 multipart/form-data 请求体，取出表单字段与 chunk 原始字节。
///
/// 只覆盖本用例需要的简单形态（无嵌套、无 transfer-encoding），
/// 避免为测试引入额外依赖。
// ignore: library_private_types_in_public_api
_CapturedRequest parseCapturedRequest(String path, String contentType, List<int> raw) {
  if (!contentType.toLowerCase().startsWith('multipart/form-data')) {
    final text = utf8.decode(raw, allowMalformed: true);
    return _CapturedRequest(
      path: path,
      body: text.isEmpty
          ? <String, dynamic>{}
          : jsonDecode(text) as Map<String, dynamic>,
    );
  }

  final boundaryMatch = RegExp(
    r'boundary=(?:"([^"]+)"|([^;]+))',
  ).firstMatch(contentType);
  final boundary = (boundaryMatch?.group(1) ?? boundaryMatch?.group(2) ?? '')
      .trim();
  final fields = <String, dynamic>{};
  List<int>? chunkBytes;
  if (boundary.isEmpty) {
    return _CapturedRequest(path: path, body: fields);
  }

  final delimiter = utf8.encode('--$boundary');
  var index = 0;
  while (index < raw.length) {
    final start = _indexOfBytes(raw, delimiter, index);
    if (start < 0) break;
    var cursor = start + delimiter.length;
    // 结尾分隔符是 "--boundary--"。
    if (cursor + 1 < raw.length && raw[cursor] == 0x2d && raw[cursor + 1] == 0x2d) {
      break;
    }
    // 跳过 CRLF。
    if (cursor + 1 < raw.length && raw[cursor] == 0x0d && raw[cursor + 1] == 0x0a) {
      cursor += 2;
    }
    final headerEnd = _indexOfBytes(raw, [0x0d, 0x0a, 0x0d, 0x0a], cursor);
    if (headerEnd < 0) break;
    final headers = utf8.decode(raw.sublist(cursor, headerEnd));
    final bodyStart = headerEnd + 4;
    final nextDelimiter = _indexOfBytes(raw, delimiter, bodyStart);
    if (nextDelimiter < 0) break;
    // 去掉分隔符前的 CRLF。
    var bodyEnd = nextDelimiter;
    if (bodyEnd >= 2 && raw[bodyEnd - 2] == 0x0d && raw[bodyEnd - 1] == 0x0a) {
      bodyEnd -= 2;
    }
    final payload = raw.sublist(bodyStart, bodyEnd);
    final nameMatch = RegExp(r'name="([^"]*)"').firstMatch(headers);
    final name = nameMatch?.group(1) ?? '';
    if (name == 'chunk') {
      chunkBytes = payload;
    } else if (name.isNotEmpty) {
      fields[name] = utf8.decode(payload, allowMalformed: true);
    }
    index = nextDelimiter;
  }
  return _CapturedRequest(path: path, body: fields, rawChunk: chunkBytes);
}

int _indexOfBytes(List<int> haystack, List<int> needle, int from) {
  outer:
  for (var i = from; i <= haystack.length - needle.length; i++) {
    for (var j = 0; j < needle.length; j++) {
      if (haystack[i + j] != needle[j]) continue outer;
    }
    return i;
  }
  return -1;
}

/// 以字节形式收集请求体：分块走 multipart，二进制内容不能再按 UTF-8 解码。
Future<List<int>> _collectBytes(HttpRequest request) async {
  final chunks = <List<int>>[];
  await for (final part in request) {
    chunks.add(part);
  }
  return chunks.expand((part) => part).toList(growable: false);
}
