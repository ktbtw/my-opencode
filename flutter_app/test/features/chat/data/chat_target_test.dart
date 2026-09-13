import 'package:chat_codex_app/features/chat/data/chat_target.dart';
import 'package:chat_codex_app/features/devices/data/device_model.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('filters available agents across devices', () {
    final devices = [
      const DeviceModel(
        machineId: 'machine-a',
        hostname: 'mac',
        online: true,
        agents: [
          AgentModel(
            agentId: 'agent-a',
            name: '造梦八荒',
            projectId: 'project-a',
            projectRoot: '/work/project-a',
            semanticAgentName: '逆向专家',
            version: '1',
            status: 'online',
            enabled: true,
          ),
          AgentModel(
            agentId: 'agent-no-project',
            projectId: '',
            projectRoot: '/work/empty',
            version: '1',
            status: 'online',
            enabled: true,
          ),
        ],
      ),
      const DeviceModel(
        machineId: 'machine-b',
        hostname: 'windows',
        online: true,
        agents: [
          AgentModel(
            agentId: 'agent-b',
            projectId: 'project-b',
            projectRoot: 'C:\\work\\project-b',
            version: '1',
            status: 'online',
            enabled: true,
          ),
          AgentModel(
            agentId: 'agent-offline',
            projectId: 'project-c',
            projectRoot: '/work/project-c',
            version: '1',
            status: 'offline',
            enabled: true,
          ),
        ],
      ),
      const DeviceModel(
        machineId: 'machine-offline',
        hostname: 'offline',
        online: false,
        agents: [
          AgentModel(
            agentId: 'agent-d',
            projectId: 'project-d',
            projectRoot: '/work/project-d',
            version: '1',
            status: 'online',
            enabled: true,
          ),
        ],
      ),
    ];

    final targets = availableChatTargets(devices);
    expect(targets.map((target) => target.agentId), ['agent-a', 'agent-b']);
    expect(targets.first.agentName, '造梦八荒');
    expect(targets.first.semanticAgentName, '逆向专家');
    expect(targets[1].projectName, 'project-b');
    expect(targets[1].deviceName, 'windows');
  });

  test('builds a complete chat route for a target', () {
    const target = ChatTarget(
      machineId: 'machine-b',
      agentId: 'agent-b',
      projectId: 'project b',
      projectScopeId: 'scope-b',
      projectRoot: '/work/project b',
      deviceName: 'windows',
      agentName: 'Agent B',
      semanticAgentName: '编码助手',
      projectName: 'project b',
    );

    final route = chatTargetRoute(target);
    expect(route, contains('machineId=machine-b'));
    expect(route, contains('agentId=agent-b'));
    expect(route, contains('projectId=project+b'));
    expect(route, contains('projectScopeId=scope-b'));
    expect(route, contains('projectRoot=%2Fwork%2Fproject+b'));
  });
}
