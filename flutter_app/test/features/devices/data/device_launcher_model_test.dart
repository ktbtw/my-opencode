import 'package:chat_codex_app/features/devices/data/device_launcher_model.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('DeviceLauncherState parses opencode version', () {
    final state = DeviceLauncherState.fromJson({
      'status': 'online',
      'current_version': '1.15.45',
      'target_version': '1.15.46',
      'upgrade_locked': true,
      'upgrade_stage': 'checking_version',
      'upgrade_progress': 3,
      'upgrade_message': '正在检查版本',
    });

    expect(state.currentVersion, '1.15.45');
    expect(state.targetVersion, '1.15.46');
    expect(state.isUpgradeRunning, isTrue);
  });

  test('CliVersionInfo parses opencode version payload', () {
    final info = CliVersionInfo.fromJson({
      'version': '1.15.45',
      'channel': 'latest',
      'changelog': 'fix opencode update flow',
    });

    expect(info.version, '1.15.45');
    expect(info.channel, 'latest');
    expect(info.changelog, 'fix opencode update flow');
  });
}
