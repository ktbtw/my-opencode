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

  test('会员策略下按 5MB 分块并发上传', () async {
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
      final bodyText = await utf8.decoder.bind(request).join();
      final body = bodyText.isEmpty
          ? <String, dynamic>{}
          : jsonDecode(bodyText) as Map<String, dynamic>;
      requests.add(
        _CapturedRequest(path: request.uri.path, body: body),
      );
      request.response.headers.contentType = ContentType.json;
      if (request.uri.path.endsWith('/upload-policy')) {
        request.response.write(
          jsonEncode({
            'policy': {
              'tier': 'plus',
              'is_member': true,
              'chunk_size': 5 * 1024 * 1024,
              'max_chunk_size': 5 * 1024 * 1024,
              'concurrency': 3,
              'target_bytes_per_second': 5 * 1024 * 1024,
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
    expect(policy.chunkSize, 5 * 1024 * 1024);
    expect(policy.concurrency, 3);

    await repo.uploadDeviceAgentFileChunkedStreamed(
      machineId: 'm',
      agentId: 'a',
      path: 'big.bin',
      totalBytes: totalBytes,
      openRead: () => Stream<List<int>>.value(bytes),
    );

    // 5MB 分块 + 3MB 文件 => 单块，不会并发。
    expect(maxInFlight, 1);

    // 换成 3 个 5MB 块，验证并发确实发生。
    requests.clear();
    inFlight = 0;
    maxInFlight = 0;
    const threeChunks = 3 * 5 * 1024 * 1024;
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
      final bodyText = await utf8.decoder.bind(request).join();
      final body = bodyText.isEmpty
          ? <String, dynamic>{}
          : jsonDecode(bodyText) as Map<String, dynamic>;
      requests.add(_CapturedRequest(path: request.uri.path, body: body));
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
    expect(chunks.single.body['chunk_index'], 1);
    expect(chunks.single.body['offset'], 2);
    expect(
      utf8.decode(base64Decode(chunks.single.body['content'] as String)),
      'll',
    );

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
      final bodyText = await utf8.decoder.bind(request).join();
      final body = bodyText.isEmpty
          ? <String, dynamic>{}
          : jsonDecode(bodyText) as Map<String, dynamic>;
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
      final bodyText = await utf8.decoder.bind(request).join();
      final body = bodyText.isEmpty
          ? <String, dynamic>{}
          : jsonDecode(bodyText) as Map<String, dynamic>;
      requests.add(_CapturedRequest(path: request.uri.path, body: body));
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
              'target_bytes_per_second': 512 * 1024,
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
    // 每块 base64 后长度约为 512KB 的 4/3 倍。
    for (final chunk in chunks) {
      final content = chunk.body['content'] as String;
      expect(content.length, lessThanOrEqualTo(700000));
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
      await utf8.decoder.bind(request).join();
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
    expect(policy.chunkSize, 5 * 1024 * 1024);
    expect(policy.concurrency, greaterThan(1));
  });
}

class _CapturedRequest {
  final String path;
  final Map<String, dynamic> body;

  const _CapturedRequest({required this.path, required this.body});
}
