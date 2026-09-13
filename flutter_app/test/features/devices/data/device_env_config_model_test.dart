import 'package:flutter_test/flutter_test.dart';
import 'package:chat_codex_app/features/devices/data/device_env_config_model.dart';

void main() {
  test('parses device environment config payload', () {
    final info = DeviceEnvConfigInfo.fromJson({
      'global_environment': {'HTTP_PROXY': 'http://127.0.0.1:7897'},
      'revision': 'revision-1',
      'agents': [
        {
          'agent_id': 'agent_chat_codex',
          'name': 'chat-codex',
          'project_dir': '/tmp/chat-codex',
          'environment': {'OPENAI_API_KEY': 'sk-test'},
        },
      ],
    });

    expect(info.globalEnvironment['HTTP_PROXY'], 'http://127.0.0.1:7897');
    expect(info.revision, 'revision-1');
    expect(info.agents, hasLength(1));
    expect(info.agentById('agent_chat_codex')?.name, 'chat-codex');
    expect(
      info.agentById('agent_chat_codex')?.environment['OPENAI_API_KEY'],
      'sk-test',
    );
  });
}
