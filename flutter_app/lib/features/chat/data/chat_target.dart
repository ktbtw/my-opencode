import '../../devices/data/device_model.dart';

class ChatTarget {
  final String machineId;
  final String agentId;
  final String projectId;
  final String projectScopeId;
  final String projectRoot;
  final String deviceName;
  final String agentName;
  final String semanticAgentName;
  final String projectName;

  const ChatTarget({
    required this.machineId,
    required this.agentId,
    required this.projectId,
    required this.projectScopeId,
    required this.projectRoot,
    required this.deviceName,
    required this.agentName,
    this.semanticAgentName = '',
    required this.projectName,
  });

  String get identity => '$machineId\x00$agentId\x00$projectId';

  bool matches({required String machineId, required String agentId}) =>
      this.machineId == machineId && this.agentId == agentId;
}

List<ChatTarget> availableChatTargets(List<DeviceModel> devices) {
  final targets = <ChatTarget>[];
  for (final device in devices) {
    if (!device.online) continue;
    for (final agent in device.agents) {
      if (!agent.enabled || !agent.isOnline || agent.projectId.trim().isEmpty) {
        continue;
      }
      final projectName = _projectName(agent.projectRoot, agent.projectId);
      targets.add(
        ChatTarget(
          machineId: device.machineId,
          agentId: agent.agentId,
          projectId: agent.projectId,
          projectScopeId: agent.projectScopeId,
          projectRoot: agent.projectRoot,
          deviceName: device.effectiveName,
          agentName: agent.displayName,
          semanticAgentName: agent.semanticAgentName.trim(),
          projectName: projectName,
        ),
      );
    }
  }
  return targets;
}

String chatTargetRoute(ChatTarget target) {
  final query = Uri(
    queryParameters: {
      'machineId': target.machineId,
      'agentId': target.agentId,
      'projectId': target.projectId,
      'projectRoot': target.projectRoot,
      'projectScopeId': target.projectScopeId,
    },
  ).query;
  return '/chat?$query';
}

String _projectName(String root, String projectId) {
  final normalized = root.trim().replaceAll('\\', '/');
  final segments = normalized
      .split('/')
      .where((segment) => segment.isNotEmpty)
      .toList(growable: false);
  if (segments.isNotEmpty) return segments.last;
  return projectId.trim().isEmpty ? '工作目录' : projectId.trim();
}
