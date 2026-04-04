import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/shell_card.dart';
import '../../../shared/widgets/surface_scaffold.dart';
import '../application/auth_controller.dart';

class LoginPage extends ConsumerStatefulWidget {
  const LoginPage({super.key});

  @override
  ConsumerState<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends ConsumerState<LoginPage> {
  final usernameController = TextEditingController(text: 'admin');
  final passwordController = TextEditingController(text: 'admin123456');
  String? errorText;

  @override
  void dispose() {
    usernameController.dispose();
    passwordController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final authState = ref.watch(authControllerProvider);
    final theme = Theme.of(context);

    ref.listen<AuthState>(authControllerProvider, (previous, next) {
      if (next.isAuthenticated) {
        context.go('/devices');
      }
    });

    return SurfaceScaffold(
      child: LayoutBuilder(
        builder: (context, constraints) {
          final mobile = constraints.maxWidth < 980;
          return Padding(
            padding: const EdgeInsets.all(24),
            child: mobile
                ? Column(
                    children: [
                      const SizedBox(height: 24),
                      _HeroPanel(compact: true),
                      const SizedBox(height: 20),
                      _LoginCard(
                        usernameController: usernameController,
                        passwordController: passwordController,
                        loading: authState.loading,
                        errorText: errorText,
                        onLogin: _submit,
                      ),
                    ],
                  )
                : Row(
                    children: [
                      const Expanded(
                        flex: 6,
                        child: _HeroPanel(),
                      ),
                      const SizedBox(width: 24),
                      Expanded(
                        flex: 5,
                        child: _LoginCard(
                          usernameController: usernameController,
                          passwordController: passwordController,
                          loading: authState.loading,
                          errorText: errorText,
                          onLogin: _submit,
                        ),
                      ),
                    ],
                  ),
          );
        },
      ),
    );
  }

  Future<void> _submit() async {
    setState(() => errorText = null);
    try {
      await ref.read(authControllerProvider.notifier).login(
            usernameController.text.trim(),
            passwordController.text,
          );
    } catch (error) {
      setState(() => errorText = '$error');
    }
  }
}

class _HeroPanel extends StatelessWidget {
  const _HeroPanel({this.compact = false});

  final bool compact;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ShellCard(
      padding: EdgeInsets.all(compact ? 28 : 36),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
            decoration: BoxDecoration(
              color: const Color(0xFFD9EAFF),
              borderRadius: BorderRadius.circular(999),
            ),
            child: Text(
              'Chat Codex Console',
              style: theme.textTheme.titleMedium,
            ),
          ),
          SizedBox(height: compact ? 22 : 32),
          Text(
            '用一套轻量而干净的控制面板，管理你的设备、执行器与远程对话。',
            style: compact ? theme.textTheme.headlineMedium : theme.textTheme.headlineLarge,
          ),
          const SizedBox(height: 18),
          Text(
            '支持设备归属、项目级 agent、会话历史、文件输入和审批处理。',
            style: theme.textTheme.bodyLarge,
          ),
          SizedBox(height: compact ? 24 : 36),
          Wrap(
            spacing: 12,
            runSpacing: 12,
            children: const [
              _HeroTag(label: '浅蓝专业风格'),
              _HeroTag(label: '多设备多项目'),
              _HeroTag(label: '对话与审批一体'),
            ],
          ),
          if (!compact) ...[
            const SizedBox(height: 36),
            Expanded(
              child: Container(
                width: double.infinity,
                decoration: BoxDecoration(
                  gradient: const LinearGradient(
                    begin: Alignment.topLeft,
                    end: Alignment.bottomRight,
                    colors: [Color(0xFFB6D7FF), Color(0xFFEAF5FF)],
                  ),
                  borderRadius: BorderRadius.circular(28),
                ),
                child: const Stack(
                  children: [
                    Positioned(top: 32, left: 32, child: _OrbitCard(title: '设备矩阵')),
                    Positioned(top: 96, right: 32, child: _OrbitCard(title: '项目执行器')),
                    Positioned(bottom: 42, left: 72, child: _OrbitCard(title: '对话审批流')),
                  ],
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class _HeroTag extends StatelessWidget {
  const _HeroTag({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      decoration: BoxDecoration(
        color: Colors.white.withValues(alpha: 0.72),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: const Color(0xFFD4E5FF)),
      ),
      child: Text(label),
    );
  }
}

class _OrbitCard extends StatelessWidget {
  const _OrbitCard({required this.title});

  final String title;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 160,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.white.withValues(alpha: 0.86),
        borderRadius: BorderRadius.circular(22),
        border: Border.all(color: const Color(0xFFD4E5FF)),
      ),
      child: Text(title, textAlign: TextAlign.center),
    );
  }
}

class _LoginCard extends StatelessWidget {
  const _LoginCard({
    required this.usernameController,
    required this.passwordController,
    required this.loading,
    required this.errorText,
    required this.onLogin,
  });

  final TextEditingController usernameController;
  final TextEditingController passwordController;
  final bool loading;
  final String? errorText;
  final Future<void> Function() onLogin;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ShellCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('登录', style: theme.textTheme.headlineMedium),
          const SizedBox(height: 8),
          Text('进入你的设备与对话控制台', style: theme.textTheme.bodyLarge),
          const SizedBox(height: 24),
          TextField(
            controller: usernameController,
            decoration: const InputDecoration(labelText: '用户名'),
          ),
          const SizedBox(height: 16),
          TextField(
            controller: passwordController,
            obscureText: true,
            decoration: const InputDecoration(labelText: '密码'),
          ),
          if (errorText != null) ...[
            const SizedBox(height: 16),
            Text(errorText!, style: const TextStyle(color: Color(0xFFB42318))),
          ],
          const SizedBox(height: 20),
          SizedBox(
            width: double.infinity,
            child: FilledButton(
              onPressed: loading ? null : onLogin,
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 14),
                child: Text(loading ? '登录中...' : '进入控制台'),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
