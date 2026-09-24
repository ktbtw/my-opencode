import 'dart:async';
import 'package:chat_codex_app/core/config/sse_parser.dart';
import 'package:chat_codex_app/core/services/account_scope.dart';
import 'package:chat_codex_app/core/services/app_foreground.dart';
import 'package:chat_codex_app/core/services/app_live_events.dart';
import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/devices/presentation/device_metrics_provider.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'status cache serves late observers and follows Agent removal',
    () async {
      final source = StreamController<SseEvent>();
      final events = AppLiveEvents(connect: () => source.stream);
      final subscription = events.watch().listen((_) {});
      source.add(
        const SseEvent(
          type: 'snapshot',
          data: '{"devices":[{"agents":[{"agent_id":"a","status":"online"}]}]}',
        ),
      );
      await pumpEventQueue();
      expect(events.agents['a']?.online, isTrue);
      expect(events.connection.healthy, isTrue);
      source.add(
        const SseEvent(type: 'agent.remove', data: '{"agent_id":"a"}'),
      );
      await pumpEventQueue();
      expect(events.agents['a']?.online, isFalse);
      source.add(
        const SseEvent(
          type: 'agent.upsert',
          data: '{"agent_id":"a","status":"online"}',
        ),
      );
      await pumpEventQueue();
      expect(events.agents['a']?.online, isTrue);
      await subscription.cancel();
      expect(events.connection.healthy, isFalse);
      await source.close();
      events.dispose();
    },
  );

  test(
    'account scope and lifecycle isolate the shared global connection',
    () async {
      SharedPreferences.setMockInitialValues({
        'access_token': 'fixture',
        'operator_key': 'first',
      });
      await AppStorage.init();
      final sources = <StreamController<SseEvent>>[];
      var cancellations = 0;
      final container = ProviderContainer(
        overrides: [
          appLiveEventsProvider.overrideWith((ref) {
            ref.watch(accountScopeProvider);
            final events = AppLiveEvents(
              connect: () {
                final source = StreamController<SseEvent>(
                  onCancel: () => cancellations++,
                );
                sources.add(source);
                return source.stream;
              },
            );
            ref.onDispose(events.dispose);
            return events;
          }),
        ],
      );
      addTearDown(() async {
        container.dispose();
        for (final source in sources) {
          await source.close();
        }
      });
      final states = container.listen(appLiveStreamProvider, (_, _) {});
      final a = container.listen(deviceMetricsLiveProvider('a'), (_, _) {});
      final b = container.listen(deviceMetricsLiveProvider('b'), (_, _) {});
      await pumpEventQueue();
      expect(sources, hasLength(1));
      container
          .read(appForegroundProvider.notifier)
          .didChangeAppLifecycleState(AppLifecycleState.paused);
      await pumpEventQueue();
      expect(cancellations, 1);
      container
          .read(appForegroundProvider.notifier)
          .didChangeAppLifecycleState(AppLifecycleState.resumed);
      await pumpEventQueue();
      expect(sources, hasLength(2));
      await AppStorage.setOperatorKey('second');
      await pumpEventQueue();
      expect(cancellations, 2);
      expect(sources, hasLength(3));
      // Old-account frames cannot leak into the newly created stream.
      sources[1].add(const SseEvent(type: 'task.updated', data: 'old-account'));
      sources[2].add(const SseEvent(type: 'snapshot', data: 'new-account'));
      await pumpEventQueue();
      expect(
        container.read(appLiveStreamProvider).valueOrNull?.data,
        'new-account',
      );
      states.close();
      a.close();
      b.close();
      await container.pump();
      await pumpEventQueue();
      expect(cancellations, 3);
    },
  );
}
