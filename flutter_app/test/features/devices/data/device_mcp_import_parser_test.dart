import 'package:flutter_test/flutter_test.dart';
import 'package:chat_codex_app/features/devices/data/device_mcp_import_parser.dart';

void main() {
  group('DeviceMCPImportParser', () {
    test('parses local MCP from command, args and env aliases', () {
      final result = DeviceMCPImportParser.parse('''
{
  "verify-api-token-11": {
    "command": "npx",
    "args": ["-y", "@ktbtw/verify-mcp"],
    "env": {
      "VERIFY_BASE_URL": "https://www.xyapi.top/verfiy",
      "VERIFY_API_TOKEN": "vat_demo",
      "VERIFY_TIMEOUT_MS": "30000"
    }
  }
}
''');

      expect(result.servers, hasLength(1));
      final server = result.servers.single;
      expect(server.name, 'verify-api-token-11');
      expect(server.type, 'local');
      expect(server.command, ['npx', '-y', '@ktbtw/verify-mcp']);
      expect(
        server.environment['VERIFY_BASE_URL'],
        'https://www.xyapi.top/verfiy',
      );
      expect(server.enabled, isTrue);
    });

    test('parses nested mcp object and keeps enabled state', () {
      final result = DeviceMCPImportParser.parse('''
{
  "mcp": {
    "docs": {
      "type": "remote",
      "enabled": false,
      "url": "https://docs.example.com/mcp",
      "headers": {
        "Authorization": "Bearer demo"
      }
    }
  }
}
''');

      expect(result.servers, hasLength(1));
      final server = result.servers.single;
      expect(server.name, 'docs');
      expect(server.type, 'remote');
      expect(server.enabled, isFalse);
      expect(server.url, 'https://docs.example.com/mcp');
    });

    test('parses lifecycle timeouts and asynchronous tools', () {
      final result = DeviceMCPImportParser.parse('''
{
  "mcpServers": {
    "idalib-mcp": {
      "type": "local",
      "command": ["uvx", "idalib-mcp"],
      "connectTimeout": "30000",
      "discovery_timeout": 31000,
      "toolTimeout": 3600000,
      "async_tools": ["idb_open", "", " idb_open_many "]
    }
  }
}
''');

      final server = result.servers.single;
      expect(server.connectTimeout, 30000);
      expect(server.discoveryTimeout, 31000);
      expect(server.toolTimeout, 3600000);
      expect(server.asyncTools, ['idb_open', 'idb_open_many']);
    });

    test('throws for invalid json', () {
      expect(
        () => DeviceMCPImportParser.parse('{invalid'),
        throwsA(isA<FormatException>()),
      );
    });
  });
}
