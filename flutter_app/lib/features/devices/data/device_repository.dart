import '../../../core/config/api_client.dart';
import 'device_model.dart';

class DeviceRepository {
  Future<List<DeviceModel>> getDevices() async {
    final data = await ApiClient.get('/api/devices');
    final list = data['devices'] as List<dynamic>? ?? [];
    return list
        .map((d) => DeviceModel.fromJson(d as Map<String, dynamic>))
        .toList();
  }

  Future<DeviceModel> getDevice(String machineId) async {
    final data = await ApiClient.get('/api/devices/$machineId');
    return DeviceModel.fromJson(data);
  }
}
