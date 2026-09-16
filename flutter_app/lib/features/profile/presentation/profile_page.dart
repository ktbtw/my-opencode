import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:path_provider/path_provider.dart';
import '../../../core/config/api_client.dart';
import '../../../core/notifications/app_notification_feedback.dart';
import '../../../core/services/app_log_service.dart';
import '../../../core/services/app_updater.dart';
import '../../../core/services/app_version_service.dart';
import '../../../core/services/update_log_service.dart';
import '../../../core/storage/app_storage.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../chat/data/artifact_cache_service.dart';
import '../../../shared/widgets/widgets.dart';

class ProfilePage extends ConsumerStatefulWidget {
  const ProfilePage({super.key});

  @override
  ConsumerState<ProfilePage> createState() => _ProfilePageState();
}

class _ProfilePageState extends ConsumerState<ProfilePage> {
  String _version = '';
  bool _checkingUpdate = false;
  String _membershipTier = 'free';

  @override
  void initState() {
    super.initState();
    _membershipTier = AppStorage.getMembershipTier();
    AppVersionService.load().then((info) {
      if (mounted) {
        setState(() => _version = info.versionLabel);
      }
    });
    _loadOperatorProfile();
  }

  Future<void> _loadOperatorProfile() async {
    try {
      final data = await ApiClient.get('/api/auth/me');
      final operatorKey = data['operator_key'] as String? ?? '';
      final displayName =
          data['display_name'] as String? ??
          data['name'] as String? ??
          data['username'] as String? ??
          '';
      final membershipTier = (data['membership_tier'] as String?)?.trim();
      if (operatorKey.isNotEmpty) {
        await AppStorage.setOperatorKey(operatorKey);
      }
      if (displayName.isNotEmpty) {
        await AppStorage.setDisplayName(displayName);
      }
      if (membershipTier != null && membershipTier.isNotEmpty) {
        await AppStorage.setMembershipTier(membershipTier);
      }
      if (!mounted) return;
      setState(() {
        _membershipTier = AppStorage.getMembershipTier();
      });
    } catch (_) {}
  }

  String get _membershipLabel {
    switch (_membershipTier) {
      case 'plus':
        return 'Plus 会员';
      case 'pro':
        return 'Pro 会员';
      default:
        return '普通用户';
    }
  }

  Future<void> _checkForUpdate() async {
    if (_checkingUpdate) return;
    setState(() => _checkingUpdate = true);
    try {
      if (kIsWeb) {
        if (!mounted) return;
        showAppFeedback(context, message: 'Web 端当前无需检查安装包更新');
        return;
      }

      await UpdateLogService.append(
        'manual_check_update_started',
        data: {'source': 'profile_page'},
      );
      final result = await AppUpdater.checkUpdateStatus();
      if (!mounted) return;
      if (result.failed) {
        showAppFeedback(
          context,
          title: '检查更新失败',
          message: result.error ?? '未知错误',
          error: true,
        );
        return;
      }

      final info = result.info;
      if (info == null) {
        await UpdateLogService.append(
          'manual_check_update_no_update',
          data: {
            'source': 'profile_page',
            'local_version': result.localVersion,
            'local_build_number': result.localBuildNumber,
          },
        );
        if (!mounted) return;
        showAppFeedback(
          context,
          message: _version.isEmpty ? '当前已是最新版本' : '当前已是最新版本（$_version）',
        );
        return;
      }

      await UpdateLogService.append(
        'manual_check_update_has_update',
        data: {
          'source': 'profile_page',
          'remote_version': info.version,
          'remote_version_code': info.versionCode,
        },
      );
      final shown = await AppUpdater.showUpdateDialogForInfo(info, force: true);
      if (!mounted) return;
      if (!shown) {
        showAppFeedback(
          context,
          title: '更新提示不可用',
          message: '请稍后重试',
          error: true,
        );
      }
    } finally {
      if (mounted) {
        setState(() => _checkingUpdate = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final username = AppStorage.getUsername() ?? '';
    final displayName = AppStorage.getDisplayName() ?? username;

    return Scaffold(
      body: SafeArea(
        child: PageBackground(
          child: Column(
            children: [
              // 顶部栏
              Container(
                height: 56,
                decoration: const BoxDecoration(
                  color: AppColors.surface,
                  border: Border(bottom: BorderSide(color: AppColors.border)),
                ),
                padding: const EdgeInsets.symmetric(horizontal: 12),
                child: Row(
                  children: [
                    IconButton(
                      icon: const Icon(Icons.arrow_back_ios_new, size: 15),
                      onPressed: () {
                        if (context.canPop()) {
                          context.pop();
                          return;
                        }
                        context.go('/devices');
                      },
                    ),
                    const SizedBox(width: 8),
                    const Text(
                      '个人信息',
                      style: TextStyle(
                        fontSize: 17,
                        fontWeight: FontWeight.w600,
                        color: AppColors.textPrimary,
                      ),
                    ),
                  ],
                ),
              ),
              Expanded(
                child: SingleChildScrollView(
                  padding: const EdgeInsets.all(20),
                  child: Column(
                    children: [
                      // 用户头像和基本信息
                      SizedBox(
                        width: double.infinity,
                        child: PanelCard(
                          padding: const EdgeInsets.all(24),
                          child: Column(
                            children: [
                              CircleAvatar(
                                radius: 36,
                                backgroundColor: AppColors.primary,
                                child: Text(
                                  displayName.isNotEmpty
                                      ? displayName[0].toUpperCase()
                                      : 'U',
                                  style: const TextStyle(
                                    fontSize: 28,
                                    fontWeight: FontWeight.w700,
                                    color: Colors.white,
                                  ),
                                ),
                              ),
                              const SizedBox(height: 12),
                              Text(
                                displayName,
                                style: const TextStyle(
                                  fontSize: 18,
                                  fontWeight: FontWeight.w600,
                                  color: AppColors.textPrimary,
                                ),
                              ),
                              const SizedBox(height: 4),
                              Text(
                                '@$username',
                                style: const TextStyle(
                                  fontSize: 14,
                                  color: AppColors.textSecondary,
                                ),
                              ),
                              const SizedBox(height: 12),
                              _MembershipBadge(
                                tier: _membershipTier,
                                label: _membershipLabel,
                              ),
                            ],
                          ),
                        ),
                      ),
                      const SizedBox(height: 16),
                      // 操作列表
                      SizedBox(
                        width: double.infinity,
                        child: PanelCard(
                          padding: EdgeInsets.zero,
                          child: Column(
                            children: [
                              _MenuItem(
                                icon: Icons.lock_outline,
                                label: '修改密码',
                                onTap: () => _showChangePassword(context),
                              ),
                              const Divider(
                                height: 1,
                                indent: 52,
                                color: AppColors.borderLight,
                              ),
                              _MenuItem(
                                icon: _checkingUpdate
                                    ? Icons.sync_rounded
                                    : Icons.system_update_alt_outlined,
                                label: _checkingUpdate ? '检查更新中...' : '检查更新',
                                onTap: _checkForUpdate,
                              ),
                              const Divider(
                                height: 1,
                                indent: 52,
                                color: AppColors.borderLight,
                              ),
                              _MenuItem(
                                icon: Icons.history_rounded,
                                label: '更新历史',
                                onTap: _showReleaseHistory,
                              ),
                              const Divider(
                                height: 1,
                                indent: 52,
                                color: AppColors.borderLight,
                              ),
                              _MenuItem(
                                icon: Icons.preview_outlined,
                                label: '预览诊断日志',
                                onTap: _previewDiagnosticLogs,
                              ),
                              const Divider(
                                height: 1,
                                indent: 52,
                                color: AppColors.borderLight,
                              ),
                              _MenuItem(
                                icon: Icons.bug_report_outlined,
                                label: '上报诊断日志',
                                onTap: _reportUpdateLogs,
                              ),
                              const Divider(
                                height: 1,
                                indent: 52,
                                color: AppColors.borderLight,
                              ),
                              _MenuItem(
                                icon: Icons.cleaning_services_outlined,
                                label: '清理缓存',
                                onTap: _confirmClearCache,
                              ),
                              const Divider(
                                height: 1,
                                indent: 52,
                                color: AppColors.borderLight,
                              ),
                              _MenuItem(
                                icon: Icons.logout,
                                label: '退出登录',
                                onTap: () => _confirmLogout(context),
                              ),
                              const Divider(
                                height: 1,
                                indent: 52,
                                color: AppColors.borderLight,
                              ),
                              _MenuItem(
                                icon: Icons.delete_forever_outlined,
                                label: '注销账号',
                                color: AppColors.statusError,
                                onTap: () => _confirmDeleteAccount(context),
                              ),
                            ],
                          ),
                        ),
                      ),
                      const SizedBox(height: 24),
                      Text(
                        '版本 $_version',
                        style: const TextStyle(
                          fontSize: 12,
                          color: AppColors.textMuted,
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _previewDiagnosticLogs() async {
    final content = await UpdateLogService.exportText();
    if (!mounted) return;
    await showDialog<void>(
      context: context,
      builder: (dialogContext) =>
          _DiagnosticLogDialog(title: '诊断日志预览', content: content),
    );
  }

  void _showReleaseHistory() {
    context.push('/app-release-history');
  }

  Future<void> _reportUpdateLogs() async {
    final preview = await UpdateLogService.exportText();
    if (!mounted) return;
    final noteCtrl = TextEditingController();
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => _DiagnosticLogDialog(
        title: '上报诊断日志',
        content: preview,
        noteController: noteCtrl,
        confirmLabel: '确认上报',
      ),
    );
    if (confirmed != true) return;
    try {
      await UpdateLogService.append(
        'manual_report_requested',
        data: {'batch_id': UpdateLogService.currentBatchId()},
      );
      await UpdateLogService.report(note: noteCtrl.text.trim());
      if (!mounted) return;
      showAppFeedback(context, message: '诊断日志已上报');
    } catch (e) {
      if (!mounted) return;
      showAppFeedback(
        context,
        title: '诊断日志上报失败',
        message: e.toString(),
        error: true,
      );
    }
  }

  Future<void> _confirmClearCache() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('清理缓存'),
        content: const Text('将清理本地诊断日志、临时文件和 artifact 文件缓存，当前登录状态不会退出。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('确认清理'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await UpdateLogService.clear();
      await ArtifactCacheService.clear();
      final tempDir = await getTemporaryDirectory();
      if (await tempDir.exists()) {
        for (final entity in tempDir.listSync()) {
          try {
            entity.deleteSync(recursive: true);
          } catch (_) {}
        }
      }
      imageCache.clear();
      imageCache.clearLiveImages();
      await AppLogService.startNewBatch(reason: 'cache_cleared');
      await UpdateLogService.append('cache_cleared');
      if (!mounted) return;
      showAppFeedback(context, message: '缓存已清理');
    } catch (e) {
      if (!mounted) return;
      showAppFeedback(
        context,
        title: '清理缓存失败',
        message: e.toString(),
        error: true,
      );
    }
  }

  void _showChangePassword(BuildContext context) {
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => const _ChangePasswordSheet(),
    );
  }

  void _confirmLogout(BuildContext context) {
    showDialog(
      context: context,
      builder: (_) => AlertDialog(
        title: const Text('退出登录'),
        content: const Text('确定要退出登录吗？'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () async {
              await AppStorage.clearAuth();
              if (!context.mounted) return;
              context.go('/login');
            },
            child: const Text(
              '退出',
              style: TextStyle(color: AppColors.statusError),
            ),
          ),
        ],
      ),
    );
  }

  void _confirmDeleteAccount(BuildContext context) {
    showDialog(context: context, builder: (_) => _DeleteAccountDialog());
  }
}

class _DiagnosticLogDialog extends StatelessWidget {
  final String title;
  final String content;
  final TextEditingController? noteController;
  final String? confirmLabel;

  const _DiagnosticLogDialog({
    required this.title,
    required this.content,
    this.noteController,
    this.confirmLabel,
  });

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(title),
      content: SizedBox(
        width: double.maxFinite,
        height: 420,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '当前批次: ${UpdateLogService.currentBatchId()}',
              style: const TextStyle(fontSize: 12, color: AppColors.textMuted),
            ),
            const SizedBox(height: 4),
            Text(
              UpdateLogService.currentLogFilePath() ?? '当前日志文件未初始化',
              style: const TextStyle(fontSize: 12, color: AppColors.textMuted),
            ),
            const SizedBox(height: 12),
            if (noteController != null) ...[
              TextField(
                controller: noteController,
                minLines: 2,
                maxLines: 3,
                decoration: const InputDecoration(
                  labelText: '补充说明（可选）',
                  hintText: '描述你的操作步骤或异常现象',
                ),
              ),
              const SizedBox(height: 12),
            ],
            Expanded(
              child: Container(
                width: double.infinity,
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: AppColors.surface,
                  borderRadius: AppRadius.mdRadius,
                  border: Border.all(color: AppColors.border),
                ),
                child: SingleChildScrollView(
                  child: SelectableText(
                    content,
                    style: const TextStyle(
                      fontSize: 12,
                      color: AppColors.textPrimary,
                      height: 1.5,
                    ),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('关闭'),
        ),
        if (confirmLabel != null)
          TextButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(confirmLabel!),
          ),
      ],
    );
  }
}

// 菜单项
class _MembershipBadge extends StatelessWidget {
  final String tier;
  final String label;

  const _MembershipBadge({
    required this.tier,
    required this.label,
  });

  bool get _isMember => tier == 'plus' || tier == 'pro';

  @override
  Widget build(BuildContext context) {
    final Color foreground = _isMember
        ? AppColors.statusWarning
        : AppColors.textSecondary;
    final Color background = _isMember
        ? AppColors.statusWarningLight
        : AppColors.statusOfflineLight;

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 5),
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: foreground.withValues(alpha: 0.25)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            _isMember ? Icons.workspace_premium : Icons.person_outline,
            size: 15,
            color: foreground,
          ),
          const SizedBox(width: 5),
          Text(
            label,
            style: TextStyle(
              fontSize: 13,
              fontWeight: FontWeight.w600,
              color: foreground,
            ),
          ),
        ],
      ),
    );
  }
}

class _MenuItem extends StatelessWidget {
  final IconData icon;
  final String label;
  final VoidCallback onTap;
  final Color color;

  const _MenuItem({
    required this.icon,
    required this.label,
    required this.onTap,
    this.color = AppColors.textPrimary,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
        child: Row(
          children: [
            Icon(icon, size: 20, color: color),
            const SizedBox(width: 16),
            Expanded(
              child: Text(label, style: TextStyle(fontSize: 15, color: color)),
            ),
            Icon(Icons.chevron_right, size: 20, color: AppColors.textMuted),
          ],
        ),
      ),
    );
  }
}

// 修改密码面板
class _ChangePasswordSheet extends StatefulWidget {
  const _ChangePasswordSheet();

  @override
  State<_ChangePasswordSheet> createState() => _ChangePasswordSheetState();
}

class _ChangePasswordSheetState extends State<_ChangePasswordSheet> {
  final _emailCtrl = TextEditingController();
  final _captchaCtrl = TextEditingController();
  final _emailCodeCtrl = TextEditingController();
  final _newPwdCtrl = TextEditingController();
  final _confirmPwdCtrl = TextEditingController();

  String? _captchaId;
  Uint8List? _captchaImage;
  bool _captchaLoading = false;
  bool _sendingEmail = false;
  int _cooldown = 0;
  bool _loading = false;
  String? _error;
  String? _success;

  @override
  void initState() {
    super.initState();
    _loadCaptcha();
  }

  @override
  void dispose() {
    _emailCtrl.dispose();
    _captchaCtrl.dispose();
    _emailCodeCtrl.dispose();
    _newPwdCtrl.dispose();
    _confirmPwdCtrl.dispose();
    super.dispose();
  }

  Future<void> _loadCaptcha() async {
    setState(() => _captchaLoading = true);
    try {
      final data = await ApiClient.get('/api/auth/captcha');
      final b64 = data['captcha_image'] as String? ?? '';
      final raw = b64.contains(',') ? b64.split(',').last : b64;
      setState(() {
        _captchaId = data['captcha_id'] as String?;
        _captchaImage = base64Decode(raw);
      });
    } catch (_) {}
    setState(() => _captchaLoading = false);
  }

  Future<void> _sendEmailCode() async {
    if (_emailCtrl.text.trim().isEmpty || _captchaCtrl.text.trim().isEmpty) {
      setState(() => _error = '请输入邮箱和图形验证码');
      return;
    }
    setState(() {
      _sendingEmail = true;
      _error = null;
    });
    try {
      await ApiClient.post('/api/auth/email-code', {
        'email': _emailCtrl.text.trim(),
        'captcha_id': _captchaId,
        'captcha': _captchaCtrl.text.trim(),
        'purpose': 'reset_password',
      }, withAuth: false);
      setState(() => _cooldown = 60);
      _startCooldown();
      _loadCaptcha();
      _captchaCtrl.clear();
    } catch (e) {
      setState(() => _error = e.toString());
      _loadCaptcha();
      _captchaCtrl.clear();
    }
    setState(() => _sendingEmail = false);
  }

  void _startCooldown() {
    Future.doWhile(() async {
      await Future.delayed(const Duration(seconds: 1));
      if (!mounted) return false;
      setState(() => _cooldown--);
      return _cooldown > 0;
    });
  }

  Future<void> _submit() async {
    final newPwd = _newPwdCtrl.text;
    if (newPwd.length < 6) {
      setState(() => _error = '新密码至少 6 位');
      return;
    }
    if (newPwd != _confirmPwdCtrl.text) {
      setState(() => _error = '两次密码不一致');
      return;
    }
    if (_emailCodeCtrl.text.trim().isEmpty) {
      setState(() => _error = '请输入邮箱验证码');
      return;
    }

    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      await ApiClient.post('/api/auth/change-password', {
        'email': _emailCtrl.text.trim(),
        'email_code': _emailCodeCtrl.text.trim(),
        'new_password': newPwd,
      });
      await AppStorage.setPassword(newPwd);
      setState(() => _success = '密码修改成功');
      await Future.delayed(const Duration(seconds: 1));
      if (mounted) Navigator.pop(context);
    } catch (e) {
      setState(() => _error = e.toString());
    }
    setState(() => _loading = false);
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.only(
        bottom: MediaQuery.of(context).viewInsets.bottom,
      ),
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Center(
              child: Container(
                width: 36,
                height: 4,
                decoration: BoxDecoration(
                  color: AppColors.border,
                  borderRadius: BorderRadius.circular(2),
                ),
              ),
            ),
            const SizedBox(height: 16),
            const Text(
              '修改密码',
              style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
            ),
            const SizedBox(height: 20),
            AppInput(label: '注册邮箱', hint: '输入注册时的邮箱', controller: _emailCtrl),
            const SizedBox(height: 12),
            // 图形验证码
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                Expanded(
                  child: AppInput(
                    label: '图形验证码',
                    hint: '请输入',
                    controller: _captchaCtrl,
                  ),
                ),
                const SizedBox(width: 10),
                GestureDetector(
                  onTap: _captchaLoading ? null : _loadCaptcha,
                  child: Container(
                    height: 46,
                    width: 130,
                    decoration: BoxDecoration(
                      borderRadius: AppRadius.smRadius,
                      border: Border.all(color: AppColors.border),
                      color: AppColors.inputBackground,
                    ),
                    clipBehavior: Clip.antiAlias,
                    child: _captchaImage != null
                        ? Image.memory(_captchaImage!, fit: BoxFit.cover)
                        : const Center(
                            child: SizedBox(
                              width: 16,
                              height: 16,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            ),
                          ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
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
                  height: 46,
                  width: 110,
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
            const SizedBox(height: 12),
            AppInput(
              label: '新密码',
              hint: '至少 6 位',
              controller: _newPwdCtrl,
              obscureText: true,
            ),
            const SizedBox(height: 12),
            AppInput(
              label: '确认新密码',
              hint: '再次输入',
              controller: _confirmPwdCtrl,
              obscureText: true,
            ),
            if (_error != null) ...[
              const SizedBox(height: 12),
              Text(
                _error!,
                style: const TextStyle(
                  fontSize: 13,
                  color: AppColors.statusError,
                ),
              ),
            ],
            if (_success != null) ...[
              const SizedBox(height: 12),
              Text(
                _success!,
                style: const TextStyle(
                  fontSize: 13,
                  color: AppColors.statusSuccess,
                ),
              ),
            ],
            const SizedBox(height: 20),
            AppButton(
              label: '确认修改',
              loading: _loading,
              width: double.infinity,
              onPressed: _submit,
            ),
            const SizedBox(height: 16),
          ],
        ),
      ),
    );
  }
}

// 注销账号确认对话框
class _DeleteAccountDialog extends StatefulWidget {
  @override
  State<_DeleteAccountDialog> createState() => _DeleteAccountDialogState();
}

class _DeleteAccountDialogState extends State<_DeleteAccountDialog> {
  final _pwdCtrl = TextEditingController();
  bool _loading = false;
  String? _error;

  @override
  void dispose() {
    _pwdCtrl.dispose();
    super.dispose();
  }

  Future<void> _delete() async {
    if (_pwdCtrl.text.isEmpty) {
      setState(() => _error = '请输入密码');
      return;
    }
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      await ApiClient.post('/api/auth/delete-account', {
        'password': _pwdCtrl.text,
      });
      await AppStorage.clearAuth();
      if (mounted) {
        Navigator.pop(context);
        (context as Element).markNeedsBuild();
        GoRouter.of(context).go('/login');
      }
    } catch (e) {
      setState(() => _error = e.toString());
    }
    setState(() => _loading = false);
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('注销账号'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            '注销后所有数据将被永久删除，无法恢复。请输入密码确认：',
            style: TextStyle(fontSize: 14, color: AppColors.textSecondary),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _pwdCtrl,
            obscureText: true,
            decoration: InputDecoration(
              hintText: '输入密码确认',
              border: OutlineInputBorder(borderRadius: AppRadius.smRadius),
              contentPadding: const EdgeInsets.symmetric(
                horizontal: 12,
                vertical: 10,
              ),
            ),
          ),
          if (_error != null) ...[
            const SizedBox(height: 8),
            Text(
              _error!,
              style: const TextStyle(
                fontSize: 13,
                color: AppColors.statusError,
              ),
            ),
          ],
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('取消'),
        ),
        TextButton(
          onPressed: _loading ? null : _delete,
          child: _loading
              ? const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Text(
                  '确认注销',
                  style: TextStyle(color: AppColors.statusError),
                ),
        ),
      ],
    );
  }
}
