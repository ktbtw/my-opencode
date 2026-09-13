import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:chat_codex_app/features/devices/data/device_agent_directory_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('DeviceAgentDirectoryStorage persists last path per machine', () async {
    SharedPreferences.setMockInitialValues({});
    await AppStorage.init();

    expect(DeviceAgentDirectoryStorage.getLastPath('machine_a'), '');

    await DeviceAgentDirectoryStorage.setLastPath(
      'machine_a',
      '/Users/admin/project',
    );
    await DeviceAgentDirectoryStorage.setLastPath('machine_b', 'C:\\Users');

    expect(
      DeviceAgentDirectoryStorage.getLastPath('machine_a'),
      '/Users/admin/project',
    );
    expect(DeviceAgentDirectoryStorage.getLastPath('machine_b'), 'C:\\Users');
  });
}
