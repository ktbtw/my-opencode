import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';
import 'package:webview_flutter/webview_flutter.dart';
import 'package:webview_windows/webview_windows.dart' as windows_webview;

class ProviderConsolePage extends StatefulWidget {
  final String url;

  const ProviderConsolePage({super.key, required this.url});

  @override
  State<ProviderConsolePage> createState() => _ProviderConsolePageState();
}

class _ProviderConsolePageState extends State<ProviderConsolePage> {
  late final WebViewController _mobileController;
  windows_webview.WebviewController? _windowsController;
  StreamSubscription<windows_webview.LoadingState>? _windowsLoadingSubscription;
  StreamSubscription<windows_webview.WebErrorStatus>? _windowsErrorSubscription;
  int _progress = 0;
  bool _loading = true;
  bool _failed = false;
  bool _fallbackOpening = false;

  static Future<void>? _windowsEnvironment;

  bool get _isWindows => defaultTargetPlatform == TargetPlatform.windows;

  @override
  void initState() {
    super.initState();
    if (_isWindows) {
      unawaited(_initializeWindowsWebview());
    } else {
      _initializeMobileWebview();
    }
  }

  void _initializeMobileWebview() {
    _mobileController = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setNavigationDelegate(
        NavigationDelegate(
          onProgress: (progress) {
            if (mounted) setState(() => _progress = progress);
          },
          onPageStarted: (_) {
            if (mounted) {
              setState(() {
                _loading = true;
                _failed = false;
              });
            }
          },
          onPageFinished: (_) {
            if (mounted) setState(() => _loading = false);
          },
          onWebResourceError: (error) {
            if (error.isForMainFrame != true) return;
            _markFailedAndFallback();
          },
        ),
      )
      ..loadRequest(Uri.parse(widget.url));
  }

  Future<void> _initializeWindowsWebview() async {
    try {
      final environment = _windowsEnvironment ??=
          _initializeWindowsEnvironment();
      await environment;
      final controller = windows_webview.WebviewController();
      _windowsController = controller;
      _windowsLoadingSubscription = controller.loadingState.listen((state) {
        if (!mounted) return;
        setState(() {
          _loading = state != windows_webview.LoadingState.navigationCompleted;
        });
      });
      _windowsErrorSubscription = controller.onLoadError.listen((_) {
        _markFailedAndFallback();
      });
      await controller.initialize();
      if (!mounted) return;
      setState(() {});
      await controller.loadUrl(widget.url);
    } catch (_) {
      _windowsEnvironment = null;
      _markFailedAndFallback();
    }
  }

  static Future<void> _initializeWindowsEnvironment() async {
    final version = await windows_webview.WebviewController.getWebViewVersion();
    if (version == null || version.trim().isEmpty) {
      throw StateError('WebView2 Runtime 未安装');
    }
    await windows_webview.WebviewController.initializeEnvironment();
  }

  void _markFailedAndFallback() {
    if (mounted) {
      setState(() {
        _loading = false;
        _failed = true;
      });
    }
    unawaited(_openExternal(showError: false));
  }

  Future<void> _openExternal({bool showError = true}) async {
    if (_fallbackOpening) return;
    _fallbackOpening = true;
    final uri = Uri.parse(widget.url);
    try {
      final opened = await launchUrl(uri, mode: LaunchMode.externalApplication);
      if (!opened && showError && mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('控制台网页打开失败')));
      }
    } catch (_) {
      if (showError && mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('控制台网页打开失败')));
      }
    } finally {
      _fallbackOpening = false;
    }
  }

  Future<void> _reload() async {
    if (_isWindows) {
      final controller = _windowsController;
      if (controller == null || !controller.value.isInitialized) {
        unawaited(_initializeWindowsWebview());
        return;
      }
      setState(() {
        _loading = true;
        _failed = false;
      });
      await controller.reload();
      return;
    }
    await _mobileController.reload();
  }

  @override
  void dispose() {
    unawaited(_windowsLoadingSubscription?.cancel());
    unawaited(_windowsErrorSubscription?.cancel());
    final controller = _windowsController;
    if (controller != null) {
      unawaited(controller.dispose().catchError((_) {}));
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('供应商控制台'),
        actions: [
          IconButton(
            tooltip: '刷新',
            icon: const Icon(Icons.refresh_rounded),
            onPressed: _reload,
          ),
          IconButton(
            tooltip: '外部打开',
            icon: const Icon(Icons.open_in_new_rounded),
            onPressed: _openExternal,
          ),
        ],
        bottom: _loading
            ? PreferredSize(
                preferredSize: const Size.fromHeight(2),
                child: LinearProgressIndicator(
                  value: _isWindows ? null : _progress / 100,
                ),
              )
            : null,
      ),
      body: Stack(
        children: [
          if (_isWindows)
            _windowsController?.value.isInitialized == true
                ? windows_webview.Webview(_windowsController!)
                : const Center(child: CircularProgressIndicator())
          else
            WebViewWidget(controller: _mobileController),
          if (_failed)
            Center(
              child: Material(
                color: Theme.of(context).scaffoldBackgroundColor,
                child: Padding(
                  padding: const EdgeInsets.all(24),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Icon(Icons.cloud_off_rounded, size: 32),
                      const SizedBox(height: 10),
                      const Text('网页加载失败'),
                      const SizedBox(height: 12),
                      FilledButton.icon(
                        onPressed: _openExternal,
                        icon: const Icon(Icons.open_in_new_rounded),
                        label: const Text('使用系统浏览器打开'),
                      ),
                    ],
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}
