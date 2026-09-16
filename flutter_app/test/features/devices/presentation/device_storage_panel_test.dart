import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:chat_codex_app/features/devices/data/device_repository.dart';
import 'package:chat_codex_app/features/devices/data/device_storage_model.dart';
import 'package:chat_codex_app/features/devices/presentation/device_provider.dart';
import 'package:chat_codex_app/features/devices/presentation/widgets/device_storage_panel.dart';
import 'package:chat_codex_app/shared/widgets/widgets.dart';

/// 桩仓库：只覆盖存储相关方法，其余继承真实实现。
class _FakeDeviceRepository extends DeviceRepository {
  DeviceStorageUsage usage;
  DeviceStorageClearResult clearResult;
  int getStorageCalls = 0;
  int clearCalls = 0;
  List<String> lastClearKeys = const [];
  Object? throwOnGet;
  Object? throwOnClear;

  _FakeDeviceRepository({
    required this.usage,
    this.clearResult = const DeviceStorageClearResult(),
  });

  @override
  Future<DeviceStorageUsage> getDeviceStorage({
    required String machineId,
  }) async {
    getStorageCalls += 1;
    if (throwOnGet != null) throw throwOnGet!;
    return usage;
  }

  @override
  Future<DeviceStorageClearResult> clearDeviceStorage({
    required String machineId,
    List<String> keys = const [],
  }) async {
    clearCalls += 1;
    lastClearKeys = keys;
    if (throwOnClear != null) throw throwOnClear!;
    return clearResult;
  }
}

DeviceStorageUsage _usageFixture() {
  return DeviceStorageUsage.fromJson({
    'total_bytes': 12503856527,
    'categories': [
      {
        'key': 'caches',
        'label': '依赖缓存',
        'bytes': 4195806303,
        'clearable': true,
      },
      {
        'key': 'runtimes',
        'label': '运行时组件',
        'bytes': 2543324789,
        'clearable': false,
      },
      {
        'key': 'versions',
        'label': '历史版本',
        'detail': '保留当前版本 1.15.97',
        'bytes': 3802457717,
        'clearable': true,
      },
    ],
  });
}

Future<void> _pumpPanel(
  WidgetTester tester,
  _FakeDeviceRepository repo, {
  bool deviceOnline = true,
}) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [deviceRepositoryProvider.overrideWithValue(repo)],
      child: MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: DeviceStoragePanel(
              machineId: 'm_demo',
              deviceOnline: deviceOnline,
            ),
          ),
        ),
      ),
    ),
  );
  // 首帧后触发加载，等待异步结果落地。
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('展示各类别占用与总量', (tester) async {
    final repo = _FakeDeviceRepository(usage: _usageFixture());
    await _pumpPanel(tester, repo);

    expect(repo.getStorageCalls, 1);
    expect(find.text('磁盘占用'), findsOneWidget);
    expect(find.text('依赖缓存'), findsOneWidget);
    expect(find.text('运行时组件'), findsOneWidget);
    expect(find.text('历史版本'), findsOneWidget);
    // 总量按 GB 展示。
    expect(find.text('11.6 GB'), findsOneWidget);
    // 版本类别带保留提示。
    expect(find.text('保留当前版本 1.15.97'), findsOneWidget);
  });

  testWidgets('只有可清理类别显示标记', (tester) async {
    final repo = _FakeDeviceRepository(usage: _usageFixture());
    await _pumpPanel(tester, repo);

    // caches 与 versions 可清理，runtimes 不可。
    expect(find.text('可清理'), findsNWidgets(2));
  });

  testWidgets('展示可释放体积并按占用排序', (tester) async {
    final repo = _FakeDeviceRepository(usage: _usageFixture());
    await _pumpPanel(tester, repo);

    // caches(4195806303) + versions(3802457717) = 7998264020 字节 ≈ 7.45 GB
    expect(find.text('可清理 7.45 GB'), findsOneWidget);
  });

  testWidgets('无可清理内容时清理按钮禁用', (tester) async {
    final repo = _FakeDeviceRepository(
      usage: DeviceStorageUsage.fromJson({
        'total_bytes': 100,
        'categories': [
          {'key': 'runtimes', 'label': '运行时组件', 'bytes': 100},
        ],
      }),
    );
    await _pumpPanel(tester, repo);

    expect(find.text('暂无可清理内容'), findsOneWidget);
    final button = tester.widget<AppButton>(find.byType(AppButton));
    expect(button.onPressed, isNull);
  });

  testWidgets('设备离线时清理按钮禁用', (tester) async {
    final repo = _FakeDeviceRepository(usage: _usageFixture());
    await _pumpPanel(tester, repo, deviceOnline: false);

    final button = tester.widget<AppButton>(find.byType(AppButton));
    expect(button.onPressed, isNull);
  });

  testWidgets('清理前弹出确认对话框，取消则不发起请求', (tester) async {
    final repo = _FakeDeviceRepository(usage: _usageFixture());
    await _pumpPanel(tester, repo);

    await tester.tap(find.text('清理缓存'));
    await tester.pumpAndSettle();

    expect(find.text('确认清理'), findsOneWidget);
    // 对话框内说明不会清理受保护内容。
    expect(find.textContaining('运行时组件、Agent 数据与程序文件不会被清理'), findsOneWidget);

    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    expect(repo.clearCalls, 0);
  });

  testWidgets('确认后发起清理并刷新占用', (tester) async {
    final repo = _FakeDeviceRepository(
      usage: _usageFixture(),
      clearResult: const DeviceStorageClearResult(freedBytes: 4195806303),
    );
    await _pumpPanel(tester, repo);

    await tester.tap(find.text('清理缓存'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认清理'));
    await tester.pumpAndSettle();

    expect(repo.clearCalls, 1);
    // 清理不带 keys，表示清理全部可清理类别。
    expect(repo.lastClearKeys, isEmpty);
    // 清理后会重新统计一次（首屏一次 + 清理后一次）。
    expect(repo.getStorageCalls, 2);
  });

  testWidgets('统计失败时展示错误并可重试', (tester) async {
    final repo = _FakeDeviceRepository(usage: _usageFixture())
      ..throwOnGet = Exception('设备不在线或无权限');
    await _pumpPanel(tester, repo);

    expect(find.text('设备不在线或无权限'), findsOneWidget);
    expect(find.text('重试'), findsOneWidget);

    // 重试成功后恢复正常展示。
    repo.throwOnGet = null;
    await tester.tap(find.text('重试'));
    await tester.pumpAndSettle();
    expect(find.text('依赖缓存'), findsOneWidget);
  });
}
