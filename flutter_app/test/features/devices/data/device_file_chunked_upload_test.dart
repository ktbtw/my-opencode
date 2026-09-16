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

  test(
    'downloadDeviceAgentFileChunked sends create chunk requests and merges bytes',
    () async {
      final previousOverride = HttpOverrides.current;
      HttpOverrides.global = null;
      addTearDown(() => HttpOverrides.global = previousOverride);

      SharedPreferences.setMockInitialValues({'access_token': 'test-token'});
      await AppStorage.init();

      final source = Uint8List.fromList(utf8.encode('hello'));
      final requests = <_CapturedRequest>[];
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      final sub = server.listen((request) async {
        final bodyText = await utf8.decoder.bind(request).join();
        final body = bodyText.isEmpty
            ? <String, dynamic>{}
            : jsonDecode(bodyText) as Map<String, dynamic>;
        requests.add(
          _CapturedRequest(
            path: request.uri.path,
            authorization: request.headers.value(
              HttpHeaders.authorizationHeader,
            ),
            body: body,
          ),
        );
        request.response.headers.contentType = ContentType.json;
        if (request.uri.path.endsWith('/download/create')) {
          request.response.write(
            jsonEncode({
              'file': {
                'path': body['path'],
                'name': 'hello.txt',
                'size': source.length,
                'sha256': sha256.convert(source).toString(),
              },
            }),
          );
        } else {
          final offset = body['offset'] as int;
          final length = body['length'] as int;
          final chunk = source.sublist(
            offset,
            (offset + length).clamp(0, source.length),
          );
          request.response.write(
            jsonEncode({
              'file': {
                'path': body['path'],
                'name': 'hello.txt',
                'size': chunk.length,
                'content': base64Encode(chunk),
                'encoding': 'base64',
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
      await AppStorage.setBaseUrl(
        'http://${server.address.host}:${server.port}',
      );

      final progress = <String>[];
      final repo = DeviceRepository();
      final file = await repo.downloadDeviceAgentFileChunked(
        machineId: 'machine_a',
        agentId: 'agent_a',
        path: 'tmp/hello.txt',
        chunkSize: 2,
        concurrency: 2,
        onProgress: (value) {
          progress.add(
            '${value.stage}:${value.completedChunks}:${value.percent}',
          );
        },
      );

      expect(file.path, 'tmp/hello.txt');
      expect(file.name, 'hello.txt');
      expect(file.size, source.length);
      expect(utf8.decode(base64Decode(file.content)), 'hello');
      expect(requests, hasLength(4));
      expect(requests.map((e) => e.path), [
        '/api/devices/machine_a/launcher/agents/agent_a/files/download/create',
        '/api/devices/machine_a/launcher/agents/agent_a/files/download/chunk',
        '/api/devices/machine_a/launcher/agents/agent_a/files/download/chunk',
        '/api/devices/machine_a/launcher/agents/agent_a/files/download/chunk',
      ]);
      expect(
        requests.every((e) => e.authorization == 'Bearer test-token'),
        isTrue,
      );
      final chunks =
          requests
              .where((e) => e.path.endsWith('/download/chunk'))
              .map((e) => e.body)
              .toList()
            ..sort(
              (a, b) =>
                  (a['chunk_index'] as int).compareTo(b['chunk_index'] as int),
            );
      expect(chunks.map((e) => e['offset']), [0, 2, 4]);
      expect(chunks.map((e) => e['length']), [2, 2, 1]);
      expect(progress.first, startsWith('准备下载:0:0'));
      expect(progress.last, '下载完成:3:100');
    },
  );

  test(
    'uploadDeviceAgentFileChunked sends create chunk and complete requests',
    () async {
      final previousOverride = HttpOverrides.current;
      HttpOverrides.global = null;
      addTearDown(() => HttpOverrides.global = previousOverride);

      SharedPreferences.setMockInitialValues({'access_token': 'test-token'});
      await AppStorage.init();

      final requests = <_CapturedRequest>[];
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      final sub = server.listen((request) async {
        final bodyText = await utf8.decoder.bind(request).join();
        final body = jsonDecode(bodyText) as Map<String, dynamic>;
        requests.add(
          _CapturedRequest(
            path: request.uri.path,
            authorization: request.headers.value(
              HttpHeaders.authorizationHeader,
            ),
            body: body,
          ),
        );
        request.response.headers.contentType = ContentType.json;
        request.response.write(
          jsonEncode({
            'file': {'path': body['path'], 'name': 'hello.txt', 'size': 5},
          }),
        );
        await request.response.close();
      });
      addTearDown(() async {
        await sub.cancel();
        await server.close(force: true);
      });
      await AppStorage.setBaseUrl(
        'http://${server.address.host}:${server.port}',
      );

      final progress = <String>[];
      final repo = DeviceRepository();
      final bytes = Uint8List.fromList(utf8.encode('hello'));
      final file = await repo.uploadDeviceAgentFileChunked(
        machineId: 'machine_a',
        agentId: 'agent_a',
        path: 'tmp/hello.txt',
        bytes: bytes,
        uploadId: 'upload-test',
        chunkSize: 2,
        concurrency: 2,
        onProgress: (value) {
          progress.add(
            '${value.stage}:${value.completedChunks}:${value.percent}',
          );
        },
      );

      expect(file.path, 'tmp/hello.txt');
      expect(file.size, 5);
      expect(requests, hasLength(5));
      expect(requests.map((e) => e.path), [
        '/api/devices/machine_a/launcher/agents/agent_a/files/upload/create',
        '/api/devices/machine_a/launcher/agents/agent_a/files/upload/chunk',
        '/api/devices/machine_a/launcher/agents/agent_a/files/upload/chunk',
        '/api/devices/machine_a/launcher/agents/agent_a/files/upload/chunk',
        '/api/devices/machine_a/launcher/agents/agent_a/files/upload/complete',
      ]);
      expect(
        requests.every((e) => e.authorization == 'Bearer test-token'),
        isTrue,
      );

      final create = requests.first.body;
      expect(create['upload_id'], 'upload-test');
      expect(create['size'], 5);
      expect(create['total_chunks'], 3);
      expect(create['sha256'], sha256.convert(bytes).toString());

      final chunks =
          requests
              .where((e) => e.path.endsWith('/chunk'))
              .map((e) => e.body)
              .toList()
            ..sort(
              (a, b) =>
                  (a['chunk_index'] as int).compareTo(b['chunk_index'] as int),
            );
      expect(chunks.map((e) => e['offset']), [0, 2, 4]);
      expect(
        chunks.map((e) => utf8.decode(base64Decode(e['content'] as String))),
        ['he', 'll', 'o'],
      );
      expect(chunks.map((e) => e['encoding']).toSet(), {'base64'});
      for (final chunk in chunks) {
        final decoded = base64Decode(chunk['content'] as String);
        expect(chunk['sha256'], sha256.convert(decoded).toString());
      }

      final complete = requests.last.body;
      expect(complete['upload_id'], 'upload-test');
      expect(complete['size'], 5);
      expect(complete['total_chunks'], 3);
      expect(complete['sha256'], sha256.convert(bytes).toString());
      expect(progress.first, startsWith('准备上传:0:0'));
      expect(progress.last, '上传完成:3:100');

      requests.clear();
      await repo.uploadDeviceDirectoryFileChunkedStreamed(
        machineId: 'machine_a',
        path: '/Users/demo/hello.txt',
        totalBytes: bytes.length,
        openRead: () => Stream<List<int>>.value(bytes),
        uploadId: 'directory-stream-test',
        chunkSize: 5,
      );
      expect(requests.map((e) => e.path), [
        '/api/devices/machine_a/directories/files/upload/create',
        '/api/devices/machine_a/directories/files/upload/chunk',
        '/api/devices/machine_a/directories/files/upload/complete',
      ]);
      expect(
        requests.every((e) => e.body['path'] == '/Users/demo/hello.txt'),
        isTrue,
      );
    },
  );

  test(
    'uploadDeviceAgentFileChunkedStreamed uploads chunks without full bytes',
    () async {
      final previousOverride = HttpOverrides.current;
      HttpOverrides.global = null;
      addTearDown(() => HttpOverrides.global = previousOverride);

      SharedPreferences.setMockInitialValues({'access_token': 'test-token'});
      await AppStorage.init();

      final requests = <_CapturedRequest>[];
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      final sub = server.listen((request) async {
        final bodyText = await utf8.decoder.bind(request).join();
        final body = jsonDecode(bodyText) as Map<String, dynamic>;
        requests.add(
          _CapturedRequest(
            path: request.uri.path,
            authorization: request.headers.value(
              HttpHeaders.authorizationHeader,
            ),
            body: body,
          ),
        );
        request.response.headers.contentType = ContentType.json;
        request.response.write(
          jsonEncode({
            'file': {'path': body['path'], 'name': 'hello.txt', 'size': 5},
          }),
        );
        await request.response.close();
      });
      addTearDown(() async {
        await sub.cancel();
        await server.close(force: true);
      });
      await AppStorage.setBaseUrl(
        'http://${server.address.host}:${server.port}',
      );

      final progress = <String>[];
      final repo = DeviceRepository();
      final source = utf8.encode('hello');
      final file = await repo.uploadDeviceAgentFileChunkedStreamed(
        machineId: 'machine_a',
        agentId: 'agent_a',
        path: 'tmp/hello.txt',
        totalBytes: source.length,
        openRead: () => Stream<List<int>>.fromIterable([
          utf8.encode('hel'),
          utf8.encode('lo'),
        ]),
        uploadId: 'upload-stream-test',
        chunkSize: 2,
        onProgress: (value) {
          progress.add(
            '${value.stage}:${value.completedChunks}:${value.percent}',
          );
        },
      );

      expect(file.path, 'tmp/hello.txt');
      expect(file.size, 5);
      expect(requests, hasLength(5));
      expect(requests.map((e) => e.path), [
        '/api/devices/machine_a/launcher/agents/agent_a/files/upload/create',
        '/api/devices/machine_a/launcher/agents/agent_a/files/upload/chunk',
        '/api/devices/machine_a/launcher/agents/agent_a/files/upload/chunk',
        '/api/devices/machine_a/launcher/agents/agent_a/files/upload/chunk',
        '/api/devices/machine_a/launcher/agents/agent_a/files/upload/complete',
      ]);

      final create = requests.first.body;
      expect(create['upload_id'], 'upload-stream-test');
      expect(create['size'], 5);
      expect(create['total_chunks'], 3);
      expect(create.containsKey('sha256'), isFalse);

      final chunks =
          requests
              .where((e) => e.path.endsWith('/chunk'))
              .map((e) => e.body)
              .toList()
            ..sort(
              (a, b) =>
                  (a['chunk_index'] as int).compareTo(b['chunk_index'] as int),
            );
      expect(chunks.map((e) => e['offset']), [0, 2, 4]);
      expect(
        chunks.map((e) => utf8.decode(base64Decode(e['content'] as String))),
        ['he', 'll', 'o'],
      );
      for (final chunk in chunks) {
        final decoded = base64Decode(chunk['content'] as String);
        expect(chunk['sha256'], sha256.convert(decoded).toString());
      }

      final complete = requests.last.body;
      expect(complete['upload_id'], 'upload-stream-test');
      expect(complete['size'], 5);
      expect(complete['total_chunks'], 3);
      expect(complete['sha256'], sha256.convert(source).toString());
      expect(progress.first, startsWith('准备上传:0:0'));
      expect(progress.last, '上传完成:3:100');
    },
  );

  test('chunked upload reuses the stored upload id so retries resume', () async {
    final previousOverride = HttpOverrides.current;
    HttpOverrides.global = null;
    addTearDown(() => HttpOverrides.global = previousOverride);

    SharedPreferences.setMockInitialValues({'access_token': 'test-token'});
    await AppStorage.init();

    final requests = <_CapturedRequest>[];
    // 第一次上传在 complete 阶段失败，续传记录才会留下；重试必须复用同一个
    // upload_id，并跳过设备侧已经收到的分块。
    var failComplete = true;
    String? seenUploadId;
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final sub = server.listen((request) async {
      final bodyText = await utf8.decoder.bind(request).join();
      final body = jsonDecode(bodyText) as Map<String, dynamic>;
      requests.add(
        _CapturedRequest(
          path: request.uri.path,
          authorization: request.headers.value(HttpHeaders.authorizationHeader),
          body: body,
        ),
      );
      request.response.headers.contentType = ContentType.json;
      final path = request.uri.path;
      if (path.endsWith('/upload/status')) {
        final received = body['upload_id'] == seenUploadId ? [0] : <int>[];
        request.response.write(
          jsonEncode({
            'status': {
              'received_chunks': received,
              'total_chunks': 3,
              'size': 5,
            },
          }),
        );
      } else if (path.endsWith('/upload/create')) {
        seenUploadId ??= body['upload_id'] as String;
        request.response.write(
          jsonEncode({
            'file': {'path': body['path'], 'name': 'hello.txt', 'size': 5},
          }),
        );
      } else if (path.endsWith('/upload/complete') && failComplete) {
        request.response.statusCode = HttpStatus.conflict;
        request.response.write(jsonEncode({'error': '文件整体校验失败'}));
      } else {
        request.response.write(
          jsonEncode({
            'file': {'path': body['path'], 'name': 'hello.txt', 'size': 5},
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

    Future<void> upload() => repo.uploadDeviceAgentFileChunkedStreamed(
      machineId: 'machine_a',
      agentId: 'agent_a',
      path: 'tmp/hello.txt',
      totalBytes: source.length,
      openRead: () => Stream<List<int>>.value(source),
      chunkSize: 2,
      resume: true,
    );

    await expectLater(upload(), throwsA(isA<Exception>()));
    final firstId =
        requests.firstWhere((e) => e.path.endsWith('/create')).body['upload_id'];

    failComplete = false;
    requests.clear();
    await upload();

    final create = requests.firstWhere((e) => e.path.endsWith('/create'));
    expect(create.body['upload_id'], firstId);
    expect(create.body['resume'], isTrue);
    final chunks = requests.where((e) => e.path.endsWith('/chunk')).toList();
    expect(chunks.map((e) => e.body['chunk_index']), [1, 2]);
    // 分块请求必须带上会话大小，否则设备侧清单里的 size 会变成 0。
    expect(chunks.every((e) => e.body['size'] == source.length), isTrue);

    // 上传成功后要清掉续传记录，下一次是全新会话。
    requests.clear();
    await upload();
    final nextId =
        requests.firstWhere((e) => e.path.endsWith('/create')).body['upload_id'];
    expect(nextId, isNot(firstId));
  });

  test(
    'downloadDeviceAgentFileChunked falls back to legacy download',
    () async {
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
        requests.add(
          _CapturedRequest(
            path: request.uri.path,
            authorization: request.headers.value(
              HttpHeaders.authorizationHeader,
            ),
            body: body,
          ),
        );
        request.response.headers.contentType = ContentType.json;
        if (request.uri.path.endsWith('/download/create')) {
          request.response.statusCode = HttpStatus.conflict;
          request.response.write(jsonEncode({'error': '不支持的项目文件操作'}));
        } else {
          request.response.write(
            jsonEncode({
              'file': {
                'path': request.uri.queryParameters['path'],
                'name': 'legacy.txt',
                'size': 6,
                'content': base64Encode(utf8.encode('legacy')),
                'encoding': 'base64',
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
      await AppStorage.setBaseUrl(
        'http://${server.address.host}:${server.port}',
      );

      final repo = DeviceRepository();
      final file = await repo.downloadDeviceAgentFileChunked(
        machineId: 'machine_a',
        agentId: 'agent_a',
        path: 'tmp/legacy.txt',
      );

      expect(file.name, 'legacy.txt');
      expect(utf8.decode(base64Decode(file.content)), 'legacy');
      expect(requests.map((e) => e.path), [
        '/api/devices/machine_a/launcher/agents/agent_a/files/download/create',
        '/api/devices/machine_a/launcher/agents/agent_a/files/download',
      ]);
    },
  );
}

class _CapturedRequest {
  final String path;
  final String? authorization;
  final Map<String, dynamic> body;

  const _CapturedRequest({
    required this.path,
    required this.authorization,
    required this.body,
  });
}
