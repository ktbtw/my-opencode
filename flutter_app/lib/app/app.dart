import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/theme/app_theme.dart';
import '../modules/console/presentation/console_shell_page.dart';

class ChatCodexApp extends ConsumerWidget {
  const ChatCodexApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return MaterialApp(
      title: 'Chat Codex Console',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light(),
      home: const ConsoleShellPage(),
    );
  }
}
