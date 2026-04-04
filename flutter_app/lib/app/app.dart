import 'package:flutter/material.dart';

import '../core/theme/app_theme.dart';
import '../features/chat/presentation/chat_home_page.dart';

class ChatCodexApp extends StatelessWidget {
  const ChatCodexApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Chat Codex',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light(),
      home: const ChatHomePage(),
    );
  }
}
