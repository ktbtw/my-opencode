import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/config/api_client.dart';
import '../../../core/services/app_log_service.dart';
import '../../../core/storage/app_storage.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import 'widgets/auth_brand_header.dart';

class RegisterPage extends ConsumerStatefulWidget {
  const RegisterPage({super.key});

  @override
  ConsumerState<RegisterPage> createState() => _RegisterPageState();
}

class _RegisterPageState extends ConsumerState<RegisterPage> {
  static const _verificationControlHeight = 48.0;

  final _usernameCtrl = TextEditingController();
  final _emailCtrl = TextEditingController();
  final _passwordCtrl = TextEditingController();
  final _confirmPwdCtrl = TextEditingController();
  final _captchaCtrl = TextEditingController();
  final _emailCodeCtrl = TextEditingController();

  bool _showPassword = false;
  bool _loading = false;
  String? _error;

  // 图形验证码
  String? _captchaId;
  Uint8List? _captchaImage;
  bool _captchaLoading = false;

  // 邮箱验证码
  bool _sendingEmail = false;
  int _cooldown = 0;

  @override
  void initState() {
    super.initState();
    _loadCaptcha();
  }

  @override
  void dispose() {
    _usernameCtrl.dispose();
    _emailCtrl.dispose();
    _passwordCtrl.dispose();
    _confirmPwdCtrl.dispose();
    _captchaCtrl.dispose();
    _emailCodeCtrl.dispose();
    super.dispose();
  }

  Future<void> _loadCaptcha() async {
    setState(() => _captchaLoading = true);
    try {
      final data = await ApiClient.get('/api/auth/captcha');
      final b64 = data['captcha_image'] as String? ?? '';
      // base64Captcha 返回带 data:image/png;base64, 前缀
      final raw = b64.contains(',') ? b64.split(',').last : b64;
      setState(() {
        _captchaId = data['captcha_id'] as String?;
        _captchaImage = base64Decode(raw);
      });
    } catch (e) {
      setState(() => _error = '加载验证码失败: $e');
    } finally {
      setState(() => _captchaLoading = false);
    }
  }

  Future<void> _sendEmailCode() async {
    final email = _emailCtrl.text.trim();
    final captcha = _captchaCtrl.text.trim();
    if (email.isEmpty) {
      setState(() => _error = '请输入邮箱');
      return;
    }
    if (captcha.isEmpty) {
      setState(() => _error = '请输入图形验证码');
      return;
    }
    if (_captchaId == null) {
      setState(() => _error = '请先加载图形验证码');
      return;
    }

    setState(() {
      _sendingEmail = true;
      _error = null;
    });
    try {
      await ApiClient.post('/api/auth/email-code', {
        'email': email,
        'captcha_id': _captchaId,
        'captcha': captcha,
      }, withAuth: false);
      // 发送成功，开始倒计时
      setState(() => _cooldown = 60);
      _startCooldown();
      // 图形验证码已消耗，刷新
      _loadCaptcha();
      _captchaCtrl.clear();
    } catch (e) {
      setState(() => _error = e.toString());
      // 验证码可能已失效，刷新
      _loadCaptcha();
      _captchaCtrl.clear();
    } finally {
      setState(() => _sendingEmail = false);
    }
  }

  void _startCooldown() {
    Future.doWhile(() async {
      await Future.delayed(const Duration(seconds: 1));
      if (!mounted) return false;
      setState(() => _cooldown--);
      return _cooldown > 0;
    });
  }

  Future<void> _register() async {
    final username = _usernameCtrl.text.trim();
    final email = _emailCtrl.text.trim();
    final password = _passwordCtrl.text;
    final confirmPwd = _confirmPwdCtrl.text;
    final emailCode = _emailCodeCtrl.text.trim();

    if (username.isEmpty ||
        email.isEmpty ||
        password.isEmpty ||
        emailCode.isEmpty) {
      setState(() => _error = '请填写所有必填项');
      return;
    }
    if (username.length < 3) {
      setState(() => _error = '用户名至少 3 位');
      return;
    }
    if (password.length < 6) {
      setState(() => _error = '密码至少 6 位');
      return;
    }
    if (password != confirmPwd) {
      setState(() => _error = '两次密码不一致');
      return;
    }

    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      await AppLogService.log(
        'auth_register_started',
        data: {'username': username, 'email': email},
      );
      final data = await ApiClient.post('/api/auth/register', {
        'username': username,
        'password': password,
        'email': email,
        'email_code': emailCode,
      }, withAuth: false);

      // 注册成功，保存登录信息
      final token = data['access_token'] as String? ?? '';
      final operator = data['operator'] as Map<String, dynamic>? ?? {};
      await AppStorage.setToken(token);
      await AppStorage.setUsername(username);
      await AppStorage.setPassword(password);
      await AppStorage.setOperatorKey(
        operator['operator_key'] as String? ?? '',
      );
      await AppStorage.setDisplayName(operator['name'] as String? ?? username);

      await AppLogService.log(
        'auth_register_succeeded',
        data: {'username': username, 'email': email},
      );
      if (mounted) context.go('/devices');
    } catch (e) {
      await AppLogService.log(
        'auth_register_failed',
        level: 'error',
        data: {'username': username, 'email': email, 'error': e.toString()},
      );
      setState(() => _error = e.toString());
    } finally {
      setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final isMobile = AppBreakpoints.isMobile(context);
    return Scaffold(
      body: PageBackground(
        child: SafeArea(
          child: isMobile ? _buildMobileLayout() : _buildDesktopLayout(),
        ),
      ),
    );
  }

  Widget _buildDesktopLayout() {
    return Row(
      children: [
        Expanded(flex: 5, child: _buildBrandPanel()),
        Expanded(
          flex: 4,
          child: Center(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(48),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 420),
                child: _buildForm(),
              ),
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildMobileLayout() {
    return SingleChildScrollView(
      child: Column(
        children: [
          _buildBrandMobile(),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
            child: _buildForm(),
          ),
        ],
      ),
    );
  }

  Widget _buildBrandPanel() {
    return const AuthBrandHeader(
      eyebrow: 'WORKSPACE / CREATE ACCOUNT',
      title: '建立你的\n工作台账号',
      description: '创建账号后，即可连接设备、配置 Agent 并持续追踪任务。',
      highlights: ['设备配置', 'Skill 与 MCP', '任务历史'],
    );
  }

  Widget _buildBrandMobile() {
    return const AuthBrandHeader(
      compact: true,
      eyebrow: 'WORKSPACE / CREATE ACCOUNT',
      title: '建立你的工作台账号',
      description: '连接设备，配置 Agent，持续追踪任务。',
      highlights: [],
    );
  }

  Widget _buildForm() {
    return PanelCard(
      padding: const EdgeInsets.all(28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text('注册', style: Theme.of(context).textTheme.headlineSmall),
          const SizedBox(height: 6),
          Text('创建新账号', style: Theme.of(context).textTheme.bodySmall),
          const SizedBox(height: 24),
          AppInput(
            label: '用户名',
            hint: '3-32 位',
            controller: _usernameCtrl,
            autofocus: true,
          ),
          const SizedBox(height: 14),
          AppInput(label: '邮箱', hint: '用于接收验证码', controller: _emailCtrl),
          const SizedBox(height: 14),
          AppInput(
            label: '密码',
            hint: '至少 6 位',
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
          ),
          const SizedBox(height: 14),
          AppInput(
            label: '确认密码',
            hint: '再次输入密码',
            controller: _confirmPwdCtrl,
            obscureText: !_showPassword,
          ),
          const SizedBox(height: 14),

          // 图形验证码
          Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              Expanded(
                child: AppInput(
                  label: '图形验证码',
                  hint: '请输入验证码',
                  controller: _captchaCtrl,
                ),
              ),
              const SizedBox(width: 10),
              GestureDetector(
                onTap: _captchaLoading ? null : _loadCaptcha,
                child: Container(
                  height: _verificationControlHeight,
                  width: 140,
                  decoration: BoxDecoration(
                    borderRadius: AppRadius.smRadius,
                    border: Border.all(color: AppColors.border),
                    color: AppColors.inputBackground,
                  ),
                  clipBehavior: Clip.antiAlias,
                  child: _captchaImage != null
                      ? Image.memory(_captchaImage!, fit: BoxFit.cover)
                      : Center(
                          child: _captchaLoading
                              ? const SizedBox(
                                  width: 16,
                                  height: 16,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2,
                                  ),
                                )
                              : const Text(
                                  '点击加载',
                                  style: TextStyle(
                                    fontSize: 12,
                                    color: AppColors.textMuted,
                                  ),
                                ),
                        ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 14),

          // 邮箱验证码
          Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              Expanded(
                child: AppInput(
                  label: '邮箱验证码',
                  hint: '6 位数字',
                  controller: _emailCodeCtrl,
                ),
              ),
              const SizedBox(width: 10),
              SizedBox(
                height: _verificationControlHeight,
                width: 120,
                child: ElevatedButton(
                  onPressed: (_sendingEmail || _cooldown > 0)
                      ? null
                      : _sendEmailCode,
                  style: ElevatedButton.styleFrom(
                    backgroundColor: AppColors.primary,
                    foregroundColor: Colors.white,
                    disabledBackgroundColor: AppColors.border,
                    shape: RoundedRectangleBorder(
                      borderRadius: AppRadius.smRadius,
                    ),
                    padding: EdgeInsets.zero,
                  ),
                  child: _sendingEmail
                      ? const SizedBox(
                          width: 16,
                          height: 16,
                          child: CircularProgressIndicator(
                            strokeWidth: 2,
                            color: Colors.white,
                          ),
                        )
                      : Text(
                          _cooldown > 0 ? '${_cooldown}s' : '发送验证码',
                          style: const TextStyle(fontSize: 13),
                        ),
                ),
              ),
            ],
          ),

          if (_error != null) ...[
            const SizedBox(height: 16),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
              decoration: BoxDecoration(
                color: AppColors.statusErrorLight,
                borderRadius: AppRadius.smRadius,
                border: Border.all(
                  color: AppColors.statusError.withValues(alpha: 0.3),
                ),
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
                      _error!,
                      style: const TextStyle(
                        fontSize: 13,
                        color: AppColors.statusError,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ],
          const SizedBox(height: 24),
          AppButton(
            label: '注册',
            icon: Icons.person_add_alt_1_rounded,
            loading: _loading,
            width: double.infinity,
            onPressed: _register,
          ),
          const SizedBox(height: 16),
          Center(
            child: GestureDetector(
              onTap: () => context.go('/login'),
              child: RichText(
                text: const TextSpan(
                  text: '已有账号？',
                  style: TextStyle(
                    fontSize: 13,
                    color: AppColors.textSecondary,
                  ),
                  children: [
                    TextSpan(
                      text: '去登录',
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
}
