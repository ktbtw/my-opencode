import '../../../core/storage/app_storage.dart';

class DeviceAgentDirectoryStorage {
  static String key(String machineId) =>
      'device_agent_directory_last_path:$machineId';

  static String getLastPath(String machineId) {
    return AppStorage.getString(key(machineId))?.trim() ?? '';
  }

  static Future<bool> setLastPath(String machineId, String path) {
    return AppStorage.setString(key(machineId), path);
  }
}
