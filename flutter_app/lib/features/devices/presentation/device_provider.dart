import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../data/device_model.dart';
import '../data/device_repository.dart';

final deviceRepositoryProvider = Provider((ref) => DeviceRepository());

// 设备列表
final deviceListProvider =
    FutureProvider.autoDispose<List<DeviceModel>>((ref) async {
  final repo = ref.watch(deviceRepositoryProvider);
  return repo.getDevices();
});

// 设备详情
final deviceDetailProvider =
    FutureProvider.autoDispose.family<DeviceModel, String>((ref, machineId) async {
  final repo = ref.watch(deviceRepositoryProvider);
  return repo.getDevice(machineId);
});
