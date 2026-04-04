import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import 'auth_provider.dart';

class LoginPage extends ConsumerStatefulWidget {
  const LoginPage({super.key});

  @override
  ConsumerState<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends ConsumerState<LoginPage>
    with TickerProviderStateMixin {
  final _usernameCtrl = TextEditingController();
  final _passwordCtrl = TextEditingController();
  bool _showPassword = false;

  late final AnimationController _fadeCtrl;
  late final Animation<double> _fadeAnim;
  late final Animation<Offset> _slideAnim;

  @override
  void initState() {
    super.initState();
    _fadeCtrl = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 500),
    )..forward();
    _fadeAnim = CurvedAnimation(parent: _fadeCtrl, curve: Curves.easeOut);
    _slideAnim = Tween<Offset>(
      begin: const Offset(0, 0.06),
      end: Offset.zero,
    ).animate(CurvedAnimation(parent: _fadeCtrl, curve: Curves.easeOut));
  }

  @override
  void dispose() {
    _fadeCtrl.dispose();
    _usernameCtrl.dispose();
    _passwordCtrl.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    final username = _usernameCtrl.text.trim();
    final password = _passwordCtrl.text;
    if (username.isEmpty || password.isEmpty) return;

    final ok = await ref
        .read(authProvider.notifier)
        .login(username, password);
    if (ok && mounted) {
      context.go('/devices');
    }
  }

  @override
  Widget build(BuildContext context) {
    final auth = ref.watch(authProvider);
    final isMobile = AppBreakpoints.isMobile(context);

    return Scaffold(
      body: PageBackground(
        child: SafeArea(
          child: FadeTransition(
            opacity: _fadeAnim,
            child: SlideTransition(
              position: _slideAnim,
              child: isMobile
                  ? _buildMobileLayout(auth)
                  : _buildDesktopLayout(auth),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildDesktopLayout(AuthState auth) {
    return Row(
      children: [
        // 左侧品牌区
        Expanded(
          flex: 5,
          child: _buildBrandPanel(),
        ),
        // 右侧表单区
        Expanded(
          flex: 4,
          child: Center(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(48),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 380),
                child: _buildFormPanel(auth),
              ),
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildMobileLayout(AuthState auth) {
    return SingleChildScrollView(
      child: Column(
        children: [
          _buildBrandMobile(),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
            child: _buildFormPanel(auth),
          ),
        ],
      ),
    );
  }

  Widget _buildBrandPanel() {
    return Container(
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0xFF1E40AF), Color(0xFF2563EB)],
        ),
      ),
      padding: const EdgeInsets.all(60),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Spacer(flex: 2),
          Row(
            children: [
              Container(
                width: 40,
                height: 40,
                decoration: BoxDecoration(
                  color: Colors.white.withOpacity(0.15),
                  borderRadius: AppRadius.mdRadius,
                ),
                child: const Icon(
                  Icons.terminal_rounded,
                  color: Colors.white,
                  size: 22,
                ),
              ),
              const SizedBox(width: 12),
              const Text(
                'Chat Codex',
                style: TextStyle(
                  color: Colors.white,
                  fontSize: 22,
                  fontWeight: FontWeight.w700,
                  letterSpacing: -0.3,
                ),
              ),
            ],
          ),
          const SizedBox(height: 32),
          const Text(
            '远程设备\n管理控制台',
            style: TextStyle(
              color: Colors.white,
              fontSize: 42,
              fontWeight: FontWeight.w700,
              letterSpacing: -1,
              height: 1.15,
            ),
          ),
          const SizedBox(height: 20),
          Text(
            '统一管理多设备、多 Agent、多项目，\n让每一次对话都触达正确的执行器。',
            style: TextStyle(
              color: Colors.white.withOpacity(0.75),
              fontSize: 15,
              height: 1.6,
            ),
          ),
          const Spacer(flex: 3),
          _buildFeatureList(),
          const Spacer(flex: 1),
        ],
      ),
    );
  }

  Widget _buildFeatureList() {
    final features = [
      '多设备实时连接',
      '多 Agent 协同执行',
      '任务审批与权限管理',
      '历史会话完整追溯',
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: features
          .map((f) => Padding(
                padding: const EdgeInsets.symmetric(vertical: 5),
                child: Row(
                  children: [
                    Container(
                      width: 18,
                      height: 18,
                      decoration: BoxDecoration(
                        color: Colors.white.withOpacity(0.2),
                        shape: BoxShape.circle,
                      ),
                      child: const Icon(
                        Icons.check,
                        color: Colors.white,
                        size: 11,
                      ),
                    ),
                    const SizedBox(width: 10),
                    Text(
                      f,
                      style: TextStyle(
                        color: Colors.white.withOpacity(0.85),
                        fontSize: 13,
                      ),
                    ),
                  ],
                ),
              ))
          .toList(),
    );
  }

  Widget _buildBrandMobile() {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(24, 40, 24, 32),
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0xFF1E40AF), Color(0xFF2563EB)],
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 32,
                height: 32,
                decoration: BoxDecoration(
                  color: Colors.white.withOpacity(0.15),
                  borderRadius: AppRadius.smRadius,
                ),
                child: const Icon(
                  Icons.terminal_rounded,
                  color: Colors.white,
                  size: 18,
                ),
              ),
              const SizedBox(width: 10),
              const Text(
                'Chat Codex',
                style: TextStyle(
                  color: Colors.white,
                  fontSize: 18,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ],
          ),
          const SizedBox(height: 16),
          const Text(
            '远程设备管理控制台',
            style: TextStyle(
              color: Colors.white,
              fontSize: 24,
              fontWeight: FontWeight.w700,
              letterSpacing: -0.5,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildFormPanel(AuthState auth) {
    return PanelCard(
      padding: const EdgeInsets.all(32),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            '登录',
            style: Theme.of(context).textTheme.headlineSmall,
          ),
          const SizedBox(height: 6),
          Text(
            '使用你的管理员账号登录',
            style: Theme.of(context).textTheme.bodySmall,
          ),
          const SizedBox(height: 28),
          AppInput(
            label: '用户名',
            hint: '请输入用户名',
            controller: _usernameCtrl,
            autofocus: true,
            onSubmitted: _submit,
          ),
          const SizedBox(height: 16),
          AppInput(
            label: '密码',
            hint: '请输入密码',
            controller: _passwordCtrl,
            obscureText: !_showPassword,
            suffixIcon: IconButton(
              icon: Icon(
                _showPassword ? Icons.visibility_off : Icons.visibility,
                size: 18,
                color: AppColors.textMuted,
              ),
              onPressed: () => setState(() => _showPassword = !_showPassword),
            ),
            onSubmitted: _submit,
          ),
          if (auth.error != null) ...[
            const SizedBox(height: 16),
            _buildError(auth.error!),
          ],
          const SizedBox(height: 24),
          AppButton(
            label: '登录',
            loading: auth.loading,
            width: double.infinity,
            onPressed: _submit,
          ),
        ],
      ),
    );
  }

  Widget _buildError(String message) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      decoration: BoxDecoration(
        color: AppColors.statusErrorLight,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.statusError.withOpacity(0.3)),
      ),
      child: Row(
        children: [
          const Icon(
            Icons.error_outline,
            size: 16,
            color: AppColors.statusError,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message,
              style: const TextStyle(
                fontSize: 13,
                color: AppColors.statusError,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
