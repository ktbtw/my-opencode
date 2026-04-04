import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:http/http.dart' as http;

import '../../../core/config/app_config.dart';
import '../domain/device_models.dart';

class DevicesApi {
  const DevicesApi();

  Future<List<DeviceInfo>> listDevices(String accessToken) async {
    final response = await http.get(
      Uri.parse('${AppConfig.apiBaseUrl}/api/devices'),
      headers: _headers(accessToken),
    );
    if (response.statusCode != 200) {
      throw Exception('设备列表加载失败: ${response.statusCode}');
    }
    final raw = jsonDecode(response.body) as List<dynamic>;
    return raw
        .whereType<Map<String, dynamic>>()
        .map(DeviceInfo.fromJson)
        .toList();
  }

  Future<DeviceInfo> getDevice(String accessToken, String machineId) async {
    final response = await http.get(
      Uri.parse('${AppConfig.apiBaseUrl}/api/devices/$machineId'),
      headers: _headers(accessToken),
    );
    if (response.statusCode != 200) {
      throw Exception('设备详情加载失败: ${response.statusCode}');
    }
    return DeviceInfo.fromJson(
      jsonDecode(response.body) as Map<String, dynamic>,
    );
  }

  Map<String, String> _headers(String accessToken) => {
    'Authorization': 'Bearer $accessToken',
  };
}

final devicesApiProvider = Provider((ref) => const DevicesApi());
