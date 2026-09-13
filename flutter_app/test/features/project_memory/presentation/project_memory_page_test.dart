import 'package:chat_codex_app/core/theme/app_colors.dart';
import 'package:chat_codex_app/features/devices/data/device_model.dart';
import 'package:chat_codex_app/features/devices/presentation/device_provider.dart';
import 'package:chat_codex_app/features/project_memory/data/project_memory_model.dart';
import 'package:chat_codex_app/features/project_memory/data/project_memory_repository.dart';
import 'package:chat_codex_app/features/project_memory/presentation/project_memory_page.dart';
import 'package:chat_codex_app/features/project_memory/presentation/project_memory_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

class _ProjectMemoryRepository extends ProjectMemoryRepository {
  _ProjectMemoryRepository({
    List<ProjectMemoryModel>? items,
    this.scope = defaultScope,
  }) : items = items ?? const [memory];

  final List<ProjectMemoryModel> items;
  final ProjectScopeModel scope;
  final List<({String beforeId, DateTime? beforeUpdatedAt, String kind})>
  listCalls = [];

  static const defaultScope = ProjectScopeModel(
    id: 'scope-1',
    machineId: 'machine-1',
    displayName: 'Offline project',
    currentRoot: '/project',
    status: 'active',
    bindingEpoch: 1,
    revision: 3,
  );

  static const memory = ProjectMemoryModel(
    id: 'memory-1',
    logicalId: 'logical-1',
    scopeId: 'scope-1',
    kind: 'verified_fact',
    subjectKey: 'build:command',
    statement: 'Use the verified release build command',
    status: 'active',
    confidence: 1,
    locked: false,
    sensitive: false,
    version: 1,
  );

  @override
  Future<ProjectMemoryOverviewModel> getOverview(
    String machineId,
    String scopeId,
  ) async =>
      ProjectMemoryOverviewModel(scope: scope, recent: items.take(20).toList());

  @override
  Future<List<ProjectMemoryModel>> listMemories({
    required String machineId,
    required String scopeId,
    String query = '',
    String kind = '',
    String status = '',
    String verification = '',
    bool? locked,
    String beforeId = '',
    DateTime? beforeUpdatedAt,
    int limit = 200,
  }) async {
    listCalls.add((
      beforeId: beforeId,
      beforeUpdatedAt: beforeUpdatedAt,
      kind: kind,
    ));
    final start = beforeId.isEmpty
        ? 0
        : items.indexWhere((item) => item.id == beforeId) + 1;
    if (start <= 0 && beforeId.isNotEmpty) return const [];
    return items.skip(start).take(limit).toList(growable: false);
  }

  @override
  Future<({ProjectMemoryModel memory, List<ProjectMemoryModel> history})>
  getMemory(String machineId, String scopeId, String memoryId) async {
    final item = items.firstWhere((memory) => memory.id == memoryId);
    return (memory: item, history: [item]);
  }
}

ProjectMemoryModel _memoryAt(int index) {
  final id = 'memory-${index.toString().padLeft(3, '0')}';
  return ProjectMemoryModel(
    id: id,
    logicalId: 'logical-$id',
    scopeId: 'scope-1',
    kind: index.isEven ? 'verified_fact' : 'inferred_fact',
    subjectKey: 'subject:$id',
    statement: 'Statement $id',
    status: 'active',
    confidence: 1,
    locked: false,
    sensitive: false,
    version: 1,
    updatedAt: DateTime.utc(2026, 8, 5).subtract(Duration(minutes: index)),
  );
}

void main() {
  testWidgets(
    'offline project remains browsable while manual organization is disabled',
    (tester) async {
      tester.view.physicalSize = const Size(1280, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            projectMemoryRepositoryProvider.overrideWithValue(
              _ProjectMemoryRepository(),
            ),
            deviceDetailProvider.overrideWith((ref, machineId) async {
              return const DeviceModel(
                machineId: 'machine-1',
                hostname: 'offline',
                online: false,
                agents: [],
              );
            }),
          ],
          child: const MaterialApp(
            home: ProjectMemoryPage(machineId: 'machine-1', scopeId: 'scope-1'),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.text('Use the verified release build command'),
        findsOneWidget,
      );
      final organizeFinder = find.byKey(
        const ValueKey('project-memory-organize-button'),
      );
      expect(organizeFinder, findsOneWidget);
      final organize = tester.widget<FloatingActionButton>(organizeFinder);
      expect(organize.onPressed, isNull);
      expect(
        find.byKey(const ValueKey('project-memory-organize-icon')),
        findsOneWidget,
      );
      expect(find.textContaining('后台自动整理'), findsNothing);
    },
  );

  testWidgets('long project name moves below actions without displacement', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(360, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    const longName =
        '/workspace/projects/a-very-long-project-directory-name-that-needs-two-lines';
    const scope = ProjectScopeModel(
      id: 'scope-1',
      machineId: 'machine-1',
      displayName: longName,
      currentRoot: longName,
      status: 'active',
      bindingEpoch: 1,
      revision: 3,
    );

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          projectMemoryRepositoryProvider.overrideWithValue(
            _ProjectMemoryRepository(scope: scope),
          ),
          deviceDetailProvider.overrideWith((ref, machineId) async {
            return const DeviceModel(
              machineId: 'machine-1',
              hostname: 'online',
              online: true,
              agents: [],
            );
          }),
        ],
        child: const MaterialApp(
          home: ProjectMemoryPage(machineId: 'machine-1', scopeId: 'scope-1'),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final subtitle = tester.widget<Text>(find.text(longName));
    final subtitleRect = tester.getRect(find.text(longName));
    final organizeRect = tester.getRect(
      find.byKey(const ValueKey('project-memory-organize-button')),
    );
    expect(subtitle.maxLines, 1);
    expect(subtitle.style?.fontSize, 11);
    expect(subtitle.style?.color, AppColors.textMuted);
    expect(subtitleRect.width, greaterThan(280));
    expect(organizeRect.right, lessThanOrEqualTo(360));
    expect(organizeRect.bottom, lessThanOrEqualTo(800));
    expect(tester.takeException(), isNull);
  });

  testWidgets('loads all pages with a timestamp and id composite cursor', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1280, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final memories = List.generate(101, _memoryAt);
    final repository = _ProjectMemoryRepository(items: memories);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          projectMemoryRepositoryProvider.overrideWithValue(repository),
          deviceDetailProvider.overrideWith((ref, machineId) async {
            return const DeviceModel(
              machineId: 'machine-1',
              hostname: 'online',
              online: true,
              agents: [],
            );
          }),
        ],
        child: const MaterialApp(
          home: ProjectMemoryPage(machineId: 'machine-1', scopeId: 'scope-1'),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.text('加载更多'),
      700,
      scrollable: find.byType(Scrollable).last,
    );
    await tester.tap(find.text('加载更多'));
    await tester.pumpAndSettle();

    expect(repository.listCalls, hasLength(2));
    expect(repository.listCalls[1].beforeId, 'memory-099');
    expect(repository.listCalls[1].beforeUpdatedAt, memories[99].updatedAt);
    expect(find.text('Statement memory-100'), findsOneWidget);
    expect(find.text('加载更多'), findsNothing);
  });

  testWidgets('fact tab requests the server-side fact union', (tester) async {
    final repository = _ProjectMemoryRepository();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          projectMemoryRepositoryProvider.overrideWithValue(repository),
          deviceDetailProvider.overrideWith((ref, machineId) async {
            return const DeviceModel(
              machineId: 'machine-1',
              hostname: 'offline',
              online: false,
              agents: [],
            );
          }),
        ],
        child: const MaterialApp(
          home: ProjectMemoryPage(machineId: 'machine-1', scopeId: 'scope-1'),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('事实'));
    await tester.pumpAndSettle();

    expect(repository.listCalls.last.kind, 'fact');
  });

  testWidgets(
    'sensitive tombstones expose no invalid restore or mutation actions',
    (tester) async {
      tester.view.physicalSize = const Size(1280, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      const tombstone = ProjectMemoryModel(
        id: 'memory-deleted',
        logicalId: 'logical-deleted',
        scopeId: 'scope-1',
        kind: 'verified_fact',
        subjectKey: 'credential:name',
        statement: '',
        status: 'deleted',
        confidence: 1,
        locked: false,
        sensitive: true,
        version: 1,
      );
      final repository = _ProjectMemoryRepository(items: const [tombstone]);

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            projectMemoryRepositoryProvider.overrideWithValue(repository),
            deviceDetailProvider.overrideWith((ref, machineId) async {
              return const DeviceModel(
                machineId: 'machine-1',
                hostname: 'offline',
                online: false,
                agents: [],
              );
            }),
          ],
          child: const MaterialApp(
            home: ProjectMemoryPage(machineId: 'machine-1', scopeId: 'scope-1'),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('credential:name').first);
      await tester.pumpAndSettle();

      expect(find.byTooltip('编辑'), findsNothing);
      expect(find.byTooltip('状态操作'), findsNothing);
      expect(find.byTooltip('删除'), findsNothing);
    },
  );
}
