import 'package:chat_codex_app/features/devices/application/ai_config_sync.dart';
import 'package:chat_codex_app/features/devices/data/device_model.dart';
import 'package:chat_codex_app/features/devices/presentation/widgets/ai_config_sync_dialogs.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('AiConfigSyncDeviceDialog', () {
    testWidgets('列出其他设备，离线设备不可选', (tester) async {
      final probe = await _openDialog<DeviceModel>(
        tester,
        (context) => AiConfigSyncDeviceDialog(
          currentMachineId: 'machine-a',
          loadDevices: () async => [
            _device('machine-a', name: '本机'),
            _device('machine-b', name: '设备B'),
            _device('machine-c', name: '设备C', online: false),
          ],
        ),
      );

      expect(find.text('设备B'), findsOneWidget);
      expect(find.text('设备C'), findsOneWidget);
      // 当前设备不出现在目标列表里
      expect(find.text('本机'), findsNothing);

      // 离线设备点击无响应
      await tester.tap(find.text('设备C'));
      await tester.pumpAndSettle();
      expect(probe.closed, isFalse);

      // 在线设备选中后返回
      await tester.tap(find.text('设备B'));
      await tester.pumpAndSettle();
      expect(probe.value?.machineId, 'machine-b');
      expect(probe.closed, isTrue);
    });

    testWidgets('设备列表加载失败可重试', (tester) async {
      var attempts = 0;
      await _openDialog<DeviceModel>(
        tester,
        (context) => AiConfigSyncDeviceDialog(
          currentMachineId: 'machine-a',
          loadDevices: () async {
            attempts++;
            if (attempts == 1) {
              throw Exception('设备列表加载失败');
            }
            return [_device('machine-b', name: '设备B')];
          },
        ),
      );

      expect(find.textContaining('设备列表加载失败'), findsOneWidget);
      await tester.tap(find.text('重试'));
      await tester.pumpAndSettle();
      expect(find.text('设备B'), findsOneWidget);
    });

    testWidgets('没有其他设备时显示空状态', (tester) async {
      await _openDialog<DeviceModel>(
        tester,
        (context) => AiConfigSyncDeviceDialog(
          currentMachineId: 'machine-a',
          loadDevices: () async => [_device('machine-a', name: '本机')],
        ),
      );
      expect(find.text('没有可同步的其他设备'), findsOneWidget);
    });
  });

  group('AiConfigSyncPlanDialog', () {
    testWidgets('冲突默认使用源设备配置，应用后关闭并返回结果', (tester) async {
      AiConfigSyncSelection? received;
      final probe = await _openDialog<AiConfigSyncApplyResult>(
        tester,
        (context) => AiConfigSyncPlanDialog(
          sourceLabel: '本设备',
          targetLabel: '设备B',
          loadPreview: () async => _preview(
            added: ['grok'],
            conflicts: ['openai'],
            identical: ['same'],
            targetOnly: ['local'],
            sourceModel: 'grok/grok-4',
            targetModel: 'openai/gpt-4o',
            diffs: {
              'openai': const AiConfigProviderDiff(
                id: 'openai',
                sourceSummary: 'https://api.a.example.com/v1 · sk-a****aaaa',
                targetSummary: 'https://api.old.example.com/v1 · sk-b****bbbb',
                changedFields: ['接口地址', 'API Key'],
              ),
            },
          ),
          onApply: (selection, preview) async {
            received = selection;
            return const AiConfigSyncApplyResult(
              addedCount: 1,
              updatedCount: 1,
              keptCount: 0,
              modelSynced: true,
            );
          },
        ),
      );

      expect(find.textContaining('将新增 1 个供应商：grok'), findsOneWidget);
      expect(find.textContaining('1 个供应商冲突'), findsOneWidget);
      expect(
        find.textContaining('https://api.a.example.com/v1'),
        findsOneWidget,
      );
      expect(find.textContaining('1 个供应商配置一致'), findsOneWidget);
      expect(find.textContaining('1 个供应商仅目标设备有'), findsOneWidget);
      expect(find.text('同步默认模型'), findsOneWidget);

      await tester.tap(find.text('开始同步'));
      await tester.pumpAndSettle();

      expect(received?.useSource['openai'], isTrue);
      expect(received?.syncDefaultModel, isTrue);
      expect(probe.value?.addedCount, 1);
      expect(probe.value?.updatedCount, 1);
      expect(probe.closed, isTrue);
    });

    testWidgets('冲突全部保留目标设备且无其它变化时禁用同步', (tester) async {
      await _openDialog<AiConfigSyncApplyResult>(
        tester,
        (context) => AiConfigSyncPlanDialog(
          sourceLabel: '本设备',
          targetLabel: '设备B',
          loadPreview: () async => _preview(
            conflicts: ['openai'],
            diffs: {
              'openai': const AiConfigProviderDiff(
                id: 'openai',
                sourceSummary: 'A',
                targetSummary: 'B',
                changedFields: ['接口地址'],
              ),
            },
          ),
          onApply: (selection, preview) async => const AiConfigSyncApplyResult(
            addedCount: 0,
            updatedCount: 0,
            keptCount: 1,
            modelSynced: false,
          ),
        ),
      );

      var button = tester.widget<ElevatedButton>(
        find.widgetWithText(ElevatedButton, '开始同步'),
      );
      expect(button.onPressed, isNotNull);

      await tester.tap(find.text('保留「设备B」的配置（跳过此项）'));
      await tester.pumpAndSettle();

      button = tester.widget<ElevatedButton>(
        find.widgetWithText(ElevatedButton, '开始同步'),
      );
      expect(button.onPressed, isNull);
    });

    testWidgets('两端配置一致时提示无需同步', (tester) async {
      await _openDialog<AiConfigSyncApplyResult>(
        tester,
        (context) => AiConfigSyncPlanDialog(
          sourceLabel: '本设备',
          targetLabel: '设备B',
          loadPreview: () async => _preview(identical: ['openai']),
          onApply: (selection, preview) async => throw StateError('不应被调用'),
        ),
      );

      expect(find.text('两台设备的 AI 配置已一致，无需同步'), findsOneWidget);
      expect(find.text('关闭'), findsOneWidget);
      expect(find.text('开始同步'), findsNothing);
    });

    testWidgets('读取两端配置失败可重试', (tester) async {
      var attempts = 0;
      await _openDialog<AiConfigSyncApplyResult>(
        tester,
        (context) => AiConfigSyncPlanDialog(
          sourceLabel: '本设备',
          targetLabel: '设备B',
          loadPreview: () async {
            attempts++;
            if (attempts == 1) {
              throw Exception('目标设备离线');
            }
            return _preview(added: ['grok']);
          },
          onApply: (selection, preview) async => const AiConfigSyncApplyResult(
            addedCount: 1,
            updatedCount: 0,
            keptCount: 0,
            modelSynced: false,
          ),
        ),
      );

      expect(find.textContaining('目标设备离线'), findsOneWidget);
      await tester.tap(find.text('重试'));
      await tester.pumpAndSettle();
      expect(find.textContaining('将新增 1 个供应商：grok'), findsOneWidget);
    });

    testWidgets('写入失败时留在弹窗内并可重试', (tester) async {
      var attempts = 0;
      final probe = await _openDialog<AiConfigSyncApplyResult>(
        tester,
        (context) => AiConfigSyncPlanDialog(
          sourceLabel: '本设备',
          targetLabel: '设备B',
          loadPreview: () async => _preview(added: ['grok']),
          onApply: (selection, preview) async {
            attempts++;
            if (attempts == 1) {
              throw Exception('写入目标设备失败');
            }
            return const AiConfigSyncApplyResult(
              addedCount: 1,
              updatedCount: 0,
              keptCount: 0,
              modelSynced: false,
            );
          },
        ),
      );

      await tester.tap(find.text('开始同步'));
      await tester.pumpAndSettle();
      expect(find.textContaining('写入目标设备失败'), findsOneWidget);
      expect(probe.closed, isFalse);

      await tester.tap(find.text('开始同步'));
      await tester.pumpAndSettle();
      expect(probe.value?.addedCount, 1);
      expect(probe.closed, isTrue);
    });
  });
}

/// 打开弹窗并记录返回值 / 是否已关闭。
Future<_DialogProbe<T>> _openDialog<T>(
  WidgetTester tester,
  Widget Function(BuildContext context) builder,
) async {
  final probe = _DialogProbe<T>();
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => Center(
            child: ElevatedButton(
              onPressed: () async {
                probe.value = await showDialog<T>(
                  context: context,
                  builder: builder,
                );
                probe.closed = true;
              },
              child: const Text('open-dialog'),
            ),
          ),
        ),
      ),
    ),
  );
  await tester.tap(find.text('open-dialog'));
  await tester.pumpAndSettle();
  return probe;
}

class _DialogProbe<T> {
  T? value;
  bool closed = false;
}

DeviceModel _device(
  String machineId, {
  required String name,
  bool online = true,
}) {
  return DeviceModel.fromJson({
    'machine_id': machineId,
    'hostname': machineId,
    'display_name': name,
    'status': online ? 'online' : 'offline',
    'agents': const <dynamic>[],
  });
}

AiConfigSyncPreview _preview({
  List<String> added = const [],
  List<String> conflicts = const [],
  List<String> identical = const [],
  List<String> targetOnly = const [],
  String sourceModel = '',
  String targetModel = '',
  Map<String, AiConfigProviderDiff> diffs = const {},
}) {
  return AiConfigSyncPreview(
    sourceRaw: '{}\n',
    targetRaw: '{}\n',
    plan: AiConfigSyncPlan(
      addedProviders: added,
      conflictProviders: conflicts,
      identicalProviders: identical,
      targetOnlyProviders: targetOnly,
      sourceModel: sourceModel,
      targetModel: targetModel,
    ),
    providerDiffs: diffs,
  );
}
