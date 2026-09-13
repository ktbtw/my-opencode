import 'package:flutter_test/flutter_test.dart';
import 'package:chat_codex_app/features/devices/data/device_model.dart';

void main() {
  group('AgentModel', () {
    test('parses the regular agent name separately from semantic agent', () {
      final agent = AgentModel.fromJson({
        'agent_id': 'agent_game',
        'name': '造梦八荒',
        'semantic_agent_name': '逆向专家',
      });

      expect(agent.displayName, '造梦八荒');
      expect(agent.semanticAgentName, '逆向专家');
    });

    test('parses project memory scope from hello project', () {
      final agent = AgentModel.fromJson({
        'agent_id': 'agent',
        'projects': [
          {
            'project_id': 'game',
            'root': '/work/game',
            'project_scope_id': 'scope-1',
          },
        ],
      });

      expect(agent.projectScopeId, 'scope-1');
    });

    test('uses the project directory name when agent name is missing', () {
      final agent = AgentModel.fromJson({
        'agent_id': 'agent_random',
        'project_id': 'fallback-project',
        'project_root': '/Users/test/projects/operit',
      });

      expect(agent.displayName, 'operit');
    });

    test('defaults missing enabled field to true for old payloads', () {
      final agent = AgentModel.fromJson({
        'agent_id': 'agent_old',
        'status': 'offline',
      });

      expect(agent.enabled, isTrue);
      expect(agent.isDisabled, isFalse);
      expect(agent.isOnline, isFalse);
    });

    test('treats disabled status as disabled', () {
      final agent = AgentModel.fromJson({
        'agent_id': 'agent_disabled',
        'status': 'disabled',
      });

      expect(agent.enabled, isFalse);
      expect(agent.isDisabled, isTrue);
      expect(agent.isOnline, isFalse);
    });

    test('treats restarting status as online transition instead of idle', () {
      final agent = AgentModel.fromJson({
        'agent_id': 'agent_restarting',
        'status': 'restarting',
      });

      expect(agent.isDisabled, isFalse);
      expect(agent.isTransitioning, isTrue);
      expect(agent.isOnline, isTrue);
      expect(agent.isBusy, isTrue);
    });

    test('treats stopped and failed statuses as not online', () {
      final stopped = AgentModel.fromJson({
        'agent_id': 'agent_stopped',
        'status': 'stopped',
      });
      final failed = AgentModel.fromJson({
        'agent_id': 'agent_failed',
        'status': 'failed',
      });

      expect(stopped.isOnline, isFalse);
      expect(failed.isOnline, isFalse);
      expect(failed.isFailed, isTrue);
    });
  });

  group('DeviceModel', () {
    test(
      'uses account display name before hostname and parses stable order',
      () {
        final named = DeviceModel.fromJson({
          'machine_id': 'device-1',
          'hostname': 'DESKTOP-HOST',
          'display_name': '办公室电脑',
          'sort_order': 7,
        });
        final fallback = DeviceModel.fromJson({
          'machine_id': 'device-2',
          'hostname': 'MACBOOK-HOST',
        });

        expect(named.effectiveName, '办公室电脑');
        expect(named.sortOrder, 7);
        expect(fallback.effectiveName, 'MACBOOK-HOST');
      },
    );

    test('reports started and total agent counts independently', () {
      final device = DeviceModel.fromJson({
        'machine_id': 'device-1',
        'hostname': 'test-device',
        'status': 'online',
        'agents': [
          {'agent_id': 'running', 'status': 'running', 'enabled': true},
          {'agent_id': 'restarting', 'status': 'restarting', 'enabled': true},
          {'agent_id': 'stopped', 'status': 'stopped', 'enabled': true},
          {'agent_id': 'disabled', 'status': 'disabled', 'enabled': false},
        ],
      });

      expect(device.runningAgentCount, 2);
      expect(device.totalAgentCount, 4);
    });
  });

  group('resolveModelTestTarget', () {
    test('keeps an explicit agent and project', () {
      final target = resolveModelTestTarget(
        preferredAgentId: 'agent_a',
        preferredProjectId: 'proj_a',
        agents: [
          AgentModel.fromJson({
            'agent_id': 'agent_b',
            'project_id': 'proj_b',
            'status': 'running',
          }),
        ],
      );

      expect(target?.agentId, 'agent_a');
      expect(target?.projectId, 'proj_a');
    });

    test('picks the first online agent that owns a project', () {
      final target = resolveModelTestTarget(
        agents: [
          AgentModel.fromJson({
            'agent_id': 'offline',
            'project_id': 'proj_offline',
            'status': 'stopped',
          }),
          AgentModel.fromJson({
            'agent_id': 'online',
            'project_id': 'proj_online',
            'status': 'running',
          }),
        ],
      );

      expect(target?.agentId, 'online');
      expect(target?.projectId, 'proj_online');
    });

    test('returns null when no online agent has a project', () {
      expect(
        resolveModelTestTarget(
          agents: [
            AgentModel.fromJson({
              'agent_id': 'offline',
              'project_id': 'proj',
              'status': 'stopped',
            }),
          ],
        ),
        isNull,
      );
    });
  });
}
