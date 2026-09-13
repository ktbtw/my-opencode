import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/services/app_updater.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import 'auth_provider.dart';
import 'widgets/auth_brand_header.dart';

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

    final ok = await ref.read(authProvider.notifier).login(username, password);
    if (ok && mounted) {
      AppUpdater.resetSessionState();
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
        Expanded(flex: 5, child: _buildBrandPanel()),
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
    return const AuthBrandHeader(
      eyebrow: 'WORKSPACE / SIGN IN',
      title: '进入你的\n工作台',
      description: '统一管理设备、Agent 与项目，让每一次对话都进入正确的执行环境。',
      highlights: ['多设备连接', 'Agent 协同', '会话可追溯'],
    );
  }

  Widget _buildBrandMobile() {
    return const AuthBrandHeader(
      compact: true,
      eyebrow: 'WORKSPACE / SIGN IN',
      title: '进入你的工作台',
      description: '设备、Agent 与项目，都在一个工作区内。',
      highlights: [],
    );
  }

  Widget _buildFormPanel(AuthState auth) {
    return PanelCard(
      padding: const EdgeInsets.all(32),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text('登录', style: Theme.of(context).textTheme.headlineSmall),
          const SizedBox(height: 6),
          Text('使用你的管理员账号登录', style: Theme.of(context).textTheme.bodySmall),
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
              tooltip: _showPassword ? '隐藏密码' : '显示密码',
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
            icon: Icons.login_rounded,
            loading: auth.loading,
            width: double.infinity,
            onPressed: _submit,
          ),
          const SizedBox(height: 16),
          Center(
            child: GestureDetector(
              onTap: () => context.go('/register'),
              child: RichText(
                text: const TextSpan(
                  text: '没有账号？',
                  style: TextStyle(
                    fontSize: 13,
                    color: AppColors.textSecondary,
                  ),
                  children: [
                    TextSpan(
                      text: '去注册',
                      style: TextStyle(
                        color: AppColors.primary,
                        fontWeight: FontWeight.w500,
                      ),
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

  Widget _buildError(String message) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      decoration: BoxDecoration(
        color: AppColors.statusErrorLight,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.statusError.withValues(alpha: 0.3)),
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
