import 'device_mcp_config_model.dart';
import '../../../core/config/api_client.dart';

class DeviceMCPStoreItem {
  final String id;
  final String title;
  final String category;
  final String description;
  final String source;
  final String credentialUrl;
  final DeviceMCPServerInfo server;

  const DeviceMCPStoreItem({
    required this.id,
    required this.title,
    required this.category,
    required this.description,
    this.source = '内置',
    this.credentialUrl = '',
    required this.server,
  });

  factory DeviceMCPStoreItem.fromJson(Map<String, dynamic> json) {
    return DeviceMCPStoreItem(
      id: json['id'] as String? ?? '',
      title: json['title'] as String? ?? '',
      category: json['category'] as String? ?? '在线目录',
      description: json['description'] as String? ?? '',
      source: json['source'] as String? ?? '在线',
      credentialUrl:
          json['credential_url'] as String? ??
          json['credentialUrl'] as String? ??
          '',
      server: DeviceMCPServerInfo.fromJson(
        (json['server'] as Map?)?.cast<String, dynamic>() ??
            const <String, dynamic>{},
      ),
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'title': title,
      'category': category,
      'description': description,
      'source': source,
      if (credentialUrl.isNotEmpty) 'credential_url': credentialUrl,
      'server': server.toJson(),
    };
  }
}

class DeviceMCPStoreCatalogResult {
  final List<DeviceMCPStoreItem> items;
  final String warning;

  const DeviceMCPStoreCatalogResult({required this.items, this.warning = ''});
}

Future<DeviceMCPStoreCatalogResult> loadDeviceMCPStoreCatalog() async {
  try {
    final data = await ApiClient.get('/api/mcp/catalog');
    final items = (data['items'] as List<dynamic>? ?? const [])
        .whereType<Map<String, dynamic>>()
        .map(DeviceMCPStoreItem.fromJson)
        .where((item) => item.id.isNotEmpty && item.server.name.isNotEmpty)
        .toList();
    return DeviceMCPStoreCatalogResult(
      items: items,
      warning: data['warning'] as String? ?? '',
    );
  } catch (error) {
    return DeviceMCPStoreCatalogResult(
      items: const [],
      warning: error.toString(),
    );
  }
}
