import 'package:chat_codex_app/core/services/agent_link_latency.dart';
import 'package:chat_codex_app/core/services/app_live_events.dart';
import 'package:chat_codex_app/core/services/backend_latency.dart';
import 'package:chat_codex_app/core/services/connection_telemetry.dart';
import 'package:chat_codex_app/features/chat/presentation/widgets/chat_connection_status.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/chat_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:chat_codex_app/core/services/app_foreground.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets(
    'visible header shares one probe and releases it in background and off route',
    (tester) async {
      var starts = 0;
      var stops = 0;
      var probes = 0;
      var agentProbes = 0;
      var agentStops = 0;
      final container = ProviderContainer(
        overrides: [
          agentLinkLatencyProvider.overrideWith((ref, id) {
            final latency = AgentLinkLatency(
              id,
              probe: () async {
                agentProbes++;
                return {'status': 'connected', 'rtt_ms': 24};
              },
            )..start();
            ref.onDispose(() {
              agentStops++;
              latency.dispose();
            });
            return latency;
          }),
          appLiveStreamProvider.overrideWith((ref) => const Stream.empty()),
          backendLatencyProvider.overrideWith((ref) {
            starts++;
            final latency = BackendLatency(
              probe: () async {
                probes++;
              },
            )..start();
            ref.onDispose(() {
              stops++;
              latency.dispose();
            });
            return latency;
          }),
        ],
      );
      addTearDown(container.dispose);
      Widget surface(bool visible) => UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          home: Scaffold(
            body: Column(
              children: [
                ChatConnectionStatus(
                  agentId: 'a',
                  projectId: 'p',
                  visible: visible,
                ),
                ChatConnectionStatus(
                  agentId: 'a',
                  projectId: 'p',
                  visible: visible,
                ),
              ],
            ),
          ),
        ),
      );
      container
          .read(appForegroundProvider.notifier)
          .didChangeAppLifecycleState(AppLifecycleState.resumed);
      await tester.pumpWidget(surface(true));
      await tester.pump();
      expect(starts, 1);
      expect(probes, 1);
      expect(agentProbes, 1);
      container
          .read(appForegroundProvider.notifier)
          .didChangeAppLifecycleState(AppLifecycleState.paused);
      await tester.pump();
      await tester.pump();
      expect(stops, 1);
      expect(agentStops, 1);
      await tester.pump(const Duration(seconds: 30));
      expect(probes, 1);
      expect(agentProbes, 1);
      container
          .read(appForegroundProvider.notifier)
          .didChangeAppLifecycleState(AppLifecycleState.resumed);
      await tester.pump();
      await tester.pump();
      expect(starts, 2);
      await tester.pumpWidget(surface(false));
      await tester.pump();
      expect(stops, 2);
      expect(agentStops, 2);
      await tester.pump(const Duration(seconds: 30));
      expect(probes, 2);
      expect(agentProbes, 2);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  test('historical streaming bubbles do not impersonate the active task', () {
    final history = ChatMessage(
      id: 'old',
      role: MessageRole.agent,
      state: MessageState.streaming,
      content: '',
      taskId: 'old-task',
      createdAt: DateTime(2026),
      streamRecovery: StreamRecovery.syncing,
    );
    final idle = ChatState(messages: [history]);
    expect(connectionTaskState(idle, null), (false, null, false));
    final current = idle.copyWith(taskActive: true);
    expect(connectionTaskState(current, 'current-task'), (
      true,
      'current-task',
      false,
    ));
    expect(connectionTaskState(current, 'old-task'), (true, 'old-task', true));
  });

  test('healthy API and Agent do not mask a broken task stream', () {
    final global = ConnectionTelemetry()..activity();
    final task = ConnectionTelemetry()..connecting();
    final latency = BackendLatency()
      ..reachable = true
      ..milliseconds = 68;
    final agent = AgentConnectionStatus('online', DateTime.now());
    ConnectionStatusSnapshot snapshot({bool syncing = false}) =>
        ConnectionStatusSnapshot.capture(
          global: global,
          agent: agent,
          latency: latency,
          task: task,
          taskActive: true,
          syncing: syncing,
        );
    expect(snapshot().healthy, isFalse);
    expect(snapshot().label, '正在连接回复通道');
    task.activity();
    expect(snapshot().healthy, isTrue);
    expect(snapshot().latencyMs, 68);
    expect(snapshot(syncing: true).label, '正在同步对话');
    global.disconnected(retry: true);
    expect(snapshot().agent, '待确认');
    expect(snapshot().healthy, isFalse);
    expect(snapshot().latencyMs, isNull);
    latency.dispose();
  });

  test(
    'Agent work outside the tracked conversation is not reported as idle',
    () {
      final latency = BackendLatency()..reachable = true;
      final snapshot = ConnectionStatusSnapshot.capture(
        global: ConnectionTelemetry()..activity(),
        agent: AgentConnectionStatus(
          'online',
          DateTime.now(),
          taskId: 'other-task',
        ),
        latency: latency,
        taskActive: false,
        syncing: false,
      );
      expect(snapshot.label, 'Agent 正在执行任务');
      expect(snapshot.reply, '当前会话未接收实时回复');
      expect(snapshot.healthy, isFalse);
      latency.dispose();
    },
  );

  test('stale Agent activity is not shown as a current running task', () {
    final latency = BackendLatency()..reachable = true;
    final snapshot = ConnectionStatusSnapshot.capture(
      global: ConnectionTelemetry()..activity(),
      agent: AgentConnectionStatus(
        'online',
        DateTime.now().subtract(const Duration(minutes: 2)),
        taskId: 'stale-task',
      ),
      latency: latency,
      taskActive: false,
      syncing: false,
    );
    expect(snapshot.label, '正在确认 Agent 状态');
    expect(snapshot.agent, '待确认');
    expect(snapshot.reply, '当前会话未接收实时回复');
    latency.dispose();
  });

  test('offline Agent differs from an unreachable backend', () {
    final global = ConnectionTelemetry()..activity();
    final latency = BackendLatency()..reachable = true;
    ConnectionStatusSnapshot snapshot() => ConnectionStatusSnapshot.capture(
      global: global,
      agent: AgentConnectionStatus('offline', DateTime.now()),
      latency: latency,
      taskActive: false,
      syncing: false,
    );
    expect(snapshot().label, 'Agent 离线');
    global.disconnected(retry: true);
    latency.reachable = false;
    expect(snapshot().label, '服务连接中断');
    expect(snapshot().agent, '待确认');
    latency.dispose();
  });

  testWidgets('inline status expands on narrow screen with large text', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 720);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: MediaQuery(
            data: const MediaQueryData(textScaler: TextScaler.linear(1.8)),
            child: const ConnectionStatusBar(
              snapshot: ConnectionStatusSnapshot(
                label: '连接正常',
                healthy: true,
                latencyMs: 68,
                agentLatencyMs: 24,
                agentLatencyDetail: '24 ms 往返',
                syncDetail: '补齐历史详情 · 2.1 秒',
                rate: 1250,
                service: '已连接',
                agent: '在线',
                reply: '已连接',
                lastOutput: '2 秒前',
                confirmed: '5 分钟前',
              ),
            ),
          ),
        ),
      ),
    );
    expect(find.text('手机到服务端'), findsNothing);
    await tester.tap(find.byKey(const ValueKey('chat-connection-toggle')));
    await tester.pumpAndSettle();
    expect(find.text('手机到服务端'), findsOneWidget);
    expect(find.text('服务端到 Agent'), findsOneWidget);
    expect(find.text('回复接收'), findsOneWidget);
    expect(find.text('回复同步'), findsOneWidget);
    expect(find.text('补齐历史详情 · 2.1 秒'), findsOneWidget);
    expect(find.text('在线 · 24 ms 往返'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.tap(find.byKey(const ValueKey('chat-connection-toggle')));
    await tester.pumpAndSettle();
    expect(find.text('手机到服务端'), findsNothing);
  });
}
