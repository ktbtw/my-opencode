import 'dart:convert';

import 'device_mcp_config_model.dart';

class DeviceMCPImportResult {
  final List<DeviceMCPServerInfo> servers;
  final List<String> skippedNames;

  const DeviceMCPImportResult({
    required this.servers,
    this.skippedNames = const [],
  });
}

class DeviceMCPImportParser {
  static DeviceMCPImportResult parse(String input) {
    final text = input.trim();
    if (text.isEmpty) {
      throw const FormatException('请先粘贴 MCP 配置');
    }

    final dynamic decoded;
    try {
      decoded = jsonDecode(text);
    } catch (_) {
      throw const FormatException('MCP 配置不是有效的 JSON');
    }

    final root = _resolveRoot(decoded);
    if (root.isEmpty) {
      throw const FormatException('未识别到可导入的 MCP 节点');
    }

    final servers = <DeviceMCPServerInfo>[];
    final skippedNames = <String>[];
    for (final entry in root.entries) {
      final name = entry.key.trim();
      final server = _parseServer(name, entry.value);
      if (server == null) {
        skippedNames.add(name.isEmpty ? '未命名节点' : name);
        continue;
      }
      servers.add(server);
    }

    servers.sort((left, right) => left.name.compareTo(right.name));
    if (servers.isEmpty) {
      throw FormatException(
        skippedNames.isEmpty
            ? '未识别到可导入的 MCP 节点'
            : '以下 MCP 配置无法识别：${skippedNames.join('、')}',
      );
    }

    return DeviceMCPImportResult(servers: servers, skippedNames: skippedNames);
  }

  static Map<String, dynamic> _resolveRoot(dynamic decoded) {
    final root = _asMap(decoded);
    if (root == null) {
      throw const FormatException('MCP 配置顶层必须是 JSON 对象');
    }

    for (final key in const ['mcp', 'mcpServers', 'servers']) {
      final nested = _asMap(root[key]);
      if (nested != null && nested.isNotEmpty) {
        return nested;
      }
    }
    return root;
  }

  static DeviceMCPServerInfo? _parseServer(String fallbackName, dynamic value) {
    final raw = _asMap(value);
    if (raw == null) {
      return null;
    }

    final name = fallbackName.isNotEmpty
        ? fallbackName
        : _asString(raw['name']).trim();
    if (name.isEmpty) {
      return null;
    }

    final url = _asString(raw['url']).trim();
    final headers = _stringMap(raw['headers']);
    final environment = _stringMap(raw['environment']).isNotEmpty
        ? _stringMap(raw['environment'])
        : _stringMap(raw['env']);
    final command = _extractCommand(raw);

    final type =
        _normalizeType(raw['type']) ??
        _inferType(
          url: url,
          command: command,
          headers: headers,
          environment: environment,
        );
    if (type == null) {
      return null;
    }

    final timeout =
        _asInt(raw['timeout']) ??
        _asInt(raw['timeout_ms']) ??
        _asInt(raw['timeoutMs']) ??
        0;
    final connectTimeout =
        _asInt(raw['connect_timeout']) ?? _asInt(raw['connectTimeout']) ?? 0;
    final discoveryTimeout =
        _asInt(raw['discovery_timeout']) ??
        _asInt(raw['discoveryTimeout']) ??
        0;
    final toolTimeout =
        _asInt(raw['tool_timeout']) ?? _asInt(raw['toolTimeout']) ?? 0;
    final asyncTools = _stringList(raw['async_tools'] ?? raw['asyncTools']);

    var oauthMode = 'auto';
    var oauthClientId = '';
    var oauthClientSecret = '';
    var oauthScope = '';
    final oauthValue = raw['oauth'];
    if (oauthValue is bool) {
      oauthMode = oauthValue ? 'auto' : 'disabled';
    } else {
      final oauth = _asMap(oauthValue);
      if (oauth != null && oauth.isNotEmpty) {
        oauthMode = 'custom';
        oauthClientId = _asString(oauth['clientId']).trim().isNotEmpty
            ? _asString(oauth['clientId']).trim()
            : _asString(oauth['client_id']).trim();
        oauthClientSecret = _asString(oauth['clientSecret']).trim().isNotEmpty
            ? _asString(oauth['clientSecret']).trim()
            : _asString(oauth['client_secret']).trim();
        oauthScope = _asString(oauth['scope']).trim().isNotEmpty
            ? _asString(oauth['scope']).trim()
            : _asString(oauth['scopes']).trim();
      }
    }

    if (type == 'remote' && url.isEmpty) {
      return null;
    }
    if (type == 'local' && command.isEmpty) {
      return null;
    }

    return DeviceMCPServerInfo(
      name: name,
      type: type,
      enabled: _asBool(raw['enabled'], fallback: true),
      timeout: timeout > 0 ? timeout : 0,
      connectTimeout: connectTimeout > 0 ? connectTimeout : 0,
      discoveryTimeout: discoveryTimeout > 0 ? discoveryTimeout : 0,
      toolTimeout: toolTimeout > 0 ? toolTimeout : 0,
      asyncTools: asyncTools,
      url: url,
      headers: headers,
      command: command,
      environment: environment,
      oauthMode: type == 'remote' ? oauthMode : 'auto',
      oauthClientId: oauthClientId,
      oauthClientSecret: oauthClientSecret,
      oauthScope: oauthScope,
    );
  }

  static String? _inferType({
    required String url,
    required List<String> command,
    required Map<String, String> headers,
    required Map<String, String> environment,
  }) {
    if (url.isNotEmpty || headers.isNotEmpty) {
      return 'remote';
    }
    if (command.isNotEmpty || environment.isNotEmpty) {
      return 'local';
    }
    return null;
  }

  static String? _normalizeType(dynamic value) {
    final type = _asString(value).trim().toLowerCase();
    switch (type) {
      case 'remote':
      case 'http':
      case 'https':
        return 'remote';
      case 'local':
      case 'stdio':
      case 'command':
        return 'local';
      default:
        return null;
    }
  }

  static List<String> _extractCommand(Map<String, dynamic> raw) {
    final args = _stringList(raw['args']);
    final commandValue = raw['command'];
    if (commandValue is String) {
      final command = commandValue.trim();
      if (command.isEmpty) {
        return const [];
      }
      return [command, ...args];
    }
    if (commandValue is List) {
      return [..._stringList(commandValue), ...args];
    }
    return const [];
  }

  static Map<String, dynamic>? _asMap(dynamic value) {
    if (value is! Map) return null;
    return value.map((key, item) => MapEntry(key.toString(), item));
  }

  static String _asString(dynamic value) {
    if (value == null) return '';
    return value.toString();
  }

  static int? _asInt(dynamic value) {
    if (value == null) return null;
    if (value is int) return value;
    if (value is double) return value.toInt();
    return int.tryParse(value.toString().trim());
  }

  static bool _asBool(dynamic value, {required bool fallback}) {
    if (value is bool) return value;
    final text = value?.toString().trim().toLowerCase();
    if (text == 'true') return true;
    if (text == 'false') return false;
    return fallback;
  }

  static List<String> _stringList(dynamic value) {
    if (value is! List) return const [];
    return value
        .map((item) => item.toString().trim())
        .where((item) => item.isNotEmpty)
        .toList();
  }

  static Map<String, String> _stringMap(dynamic value) {
    if (value is! Map) return const {};
    final result = <String, String>{};
    value.forEach((key, item) {
      final mapKey = key.toString().trim();
      final mapValue = item?.toString().trim() ?? '';
      if (mapKey.isEmpty || mapValue.isEmpty) {
        return;
      }
      result[mapKey] = mapValue;
    });
    return result;
  }
}
