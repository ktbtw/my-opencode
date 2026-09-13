import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';
import '../../shared/presentation/provider_console_page.dart';

bool providerConsoleUsesExternalBrowser([TargetPlatform? platform]) {
  if (kIsWeb) return true;
  final target = platform ?? defaultTargetPlatform;
  return target == TargetPlatform.linux;
}

String? normalizeProviderConsoleUrl(String? value) {
  final text = value?.trim() ?? '';
  if (text.isEmpty) return null;
  final uri = Uri.tryParse(text);
  if (uri == null ||
      !uri.hasAuthority ||
      (uri.scheme != 'http' && uri.scheme != 'https')) {
    return null;
  }
  return uri.toString();
}

String? deriveProviderConsoleUrl(String? baseUrl) {
  final normalized = normalizeProviderConsoleUrl(baseUrl);
  if (normalized == null) return null;
  final uri = Uri.parse(normalized);
  return Uri(
    scheme: uri.scheme,
    host: uri.host,
    port: uri.hasPort ? uri.port : null,
  ).toString();
}

String? resolveProviderConsoleUrl({String? consoleUrl, String? baseUrl}) {
  return normalizeProviderConsoleUrl(consoleUrl) ??
      deriveProviderConsoleUrl(baseUrl);
}

Future<bool> openProviderConsole(
  BuildContext context, {
  String? consoleUrl,
  String? baseUrl,
}) async {
  final resolved = resolveProviderConsoleUrl(
    consoleUrl: consoleUrl,
    baseUrl: baseUrl,
  );
  if (resolved == null) {
    ScaffoldMessenger.maybeOf(
      context,
    )?.showSnackBar(const SnackBar(content: Text('该供应商尚未配置可打开的网址')));
    return false;
  }

  final uri = Uri.parse(resolved);
  if (providerConsoleUsesExternalBrowser()) {
    return _launchProviderConsoleExternally(context, uri);
  }
  try {
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => ProviderConsolePage(url: resolved),
      ),
    );
    return true;
  } catch (_) {
    // Some desktop platforms do not expose the embedded WebView plugin.
  }
  if (!context.mounted) return false;
  return _launchProviderConsoleExternally(context, uri);
}

Future<bool> _launchProviderConsoleExternally(
  BuildContext context,
  Uri uri,
) async {
  try {
    if (await launchUrl(uri, mode: LaunchMode.externalApplication)) return true;
  } catch (_) {
    // Surface one consistent message below.
  }
  if (!context.mounted) return false;
  ScaffoldMessenger.maybeOf(
    context,
  )?.showSnackBar(const SnackBar(content: Text('控制台网页打开失败')));
  return false;
}
