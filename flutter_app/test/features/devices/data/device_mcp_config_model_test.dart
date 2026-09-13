import 'package:flutter_test/flutter_test.dart';
import 'package:chat_codex_app/features/devices/data/device_mcp_config_model.dart';

void main() {
  group('DeviceMCPServerInfo', () {
    test('round trips MCP lifecycle and asynchronous tool settings', () {
      const server = DeviceMCPServerInfo(
        name: 'idalib-mcp',
        type: 'local',
        enabled: true,
        timeout: 30000,
        connectTimeout: 31000,
        discoveryTimeout: 32000,
        toolTimeout: 3600000,
        asyncTools: ['idb_open'],
        command: ['uvx', 'idalib-mcp'],
      );

      final json = server.toJson();
      expect(json['connect_timeout'], 31000);
      expect(json['discovery_timeout'], 32000);
      expect(json['tool_timeout'], 3600000);
      expect(json['async_tools'], ['idb_open']);

      final restored = DeviceMCPServerInfo.fromJson(json);
      expect(restored.connectTimeout, 31000);
      expect(restored.discoveryTimeout, 32000);
      expect(restored.toolTimeout, 3600000);
      expect(restored.asyncTools, ['idb_open']);
    });
  });

  group('shouldLoadAgentMCPStatus', () {
    test('skips when not in agent mode', () {
      final config = DeviceMCPConfigInfo(
        exists: true,
        configPath: '',
        rawJson: '',
        previewJson: '',
        servers: const [
          DeviceMCPServerInfo(name: 'docs', type: 'remote', enabled: true),
        ],
      );

      expect(
        shouldLoadAgentMCPStatus(agentMode: false, config: config),
        isFalse,
      );
    });

    test('skips empty, disabled and unnamed servers', () {
      const emptyConfig = DeviceMCPConfigInfo(
        exists: true,
        configPath: '',
        rawJson: '',
        previewJson: '',
        servers: [],
      );
      final disabledConfig = DeviceMCPConfigInfo(
        exists: true,
        configPath: '',
        rawJson: '',
        previewJson: '',
        servers: const [
          DeviceMCPServerInfo(name: 'docs', type: 'remote', enabled: false),
          DeviceMCPServerInfo(name: '   ', type: 'local', enabled: true),
        ],
      );

      expect(shouldLoadAgentMCPStatus(agentMode: true, config: null), isFalse);
      expect(
        shouldLoadAgentMCPStatus(agentMode: true, config: emptyConfig),
        isFalse,
      );
      expect(
        shouldLoadAgentMCPStatus(agentMode: true, config: disabledConfig),
        isFalse,
      );
    });

    test('loads when an enabled named server exists', () {
      final config = DeviceMCPConfigInfo(
        exists: true,
        configPath: '',
        rawJson: '',
        previewJson: '',
        servers: const [
          DeviceMCPServerInfo(name: 'docs', type: 'remote', enabled: true),
        ],
      );

      expect(shouldLoadAgentMCPStatus(agentMode: true, config: config), isTrue);
    });
  });
}
