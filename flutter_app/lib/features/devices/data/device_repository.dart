import '../../../core/config/api_client.dart';
import 'device_model.dart';

class DeviceRepository {
  Future<List<DeviceModel>> getDevices() async {
    final list = await ApiClient.getList('/api/devices');
    return list
        .map((d) => DeviceModel.fromJson(d as Map<String, dynamic>))
        .toList();
  }

  Future<DeviceModel> getDevice(String machineId) async {
    final data = await ApiClient.get('/api/devices/$machineId');
    return DeviceModel.fromJson(data);
  }
}
