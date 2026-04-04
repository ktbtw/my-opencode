import 'package:flutter/material.dart';
import '../../core/theme/app_colors.dart';
import '../../core/theme/app_theme.dart';

// 页面背景渐变容器
class PageBackground extends StatelessWidget {
  final Widget child;
  const PageBackground({super.key, required this.child});

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0xFFEFF4FB), Color(0xFFF7FAFF)],
        ),
      ),
      child: child,
    );
  }
}

// 面板卡片
class PanelCard extends StatelessWidget {
  final Widget child;
  final EdgeInsetsGeometry? padding;
  final double? width;
  final BorderRadius? borderRadius;

  const PanelCard({
    super.key,
    required this.child,
    this.padding,
    this.width,
    this.borderRadius,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      width: width,
      decoration: BoxDecoration(
        color: AppColors.surface,
        borderRadius: borderRadius ?? AppRadius.lgRadius,
        border: Border.all(color: AppColors.border),
        boxShadow: [
          const BoxShadow(
            color: Color(0x0A1A3A6A),
            blurRadius: 12,
            offset: Offset(0, 4),
          ),
        ],
      ),
      padding: padding ?? AppSpacing.cardPadding,
      child: child,
    );
  }
}

// 顶部栏
class AppTopBar extends StatelessWidget implements PreferredSizeWidget {
  final String title;
  final List<Widget>? actions;
  final Widget? leading;
  final bool showDivider;

  const AppTopBar({
    super.key,
    required this.title,
    this.actions,
    this.leading,
    this.showDivider = true,
  });

  @override
  Size get preferredSize => const Size.fromHeight(56);

  @override
  Widget build(BuildContext context) {
    return Container(
      height: 56,
      decoration: BoxDecoration(
        color: AppColors.surface,
        border: showDivider
            ? const Border(
                bottom: BorderSide(color: AppColors.border),
              )
            : null,
      ),
      padding: const EdgeInsets.symmetric(horizontal: 20),
      child: Row(
        children: [
          if (leading != null) ...[leading!, const SizedBox(width: 12)],
          Text(
            title,
            style: Theme.of(context).textTheme.titleLarge,
          ),
          const Spacer(),
          if (actions != null) ...actions!,
        ],
      ),
    );
  }
}

// 状态标签
enum StatusType { online, processing, warning, error, offline }

class StatusPill extends StatelessWidget {
  final String label;
  final StatusType type;

  const StatusPill({super.key, required this.label, required this.type});

  @override
  Widget build(BuildContext context) {
    final (color, bg) = switch (type) {
      StatusType.online => (AppColors.statusOnline, AppColors.statusOnlineLight),
      StatusType.processing =>
        (AppColors.statusProcessing, AppColors.statusProcessingLight),
      StatusType.warning =>
        (AppColors.statusWarning, AppColors.statusWarningLight),
      StatusType.error => (AppColors.statusError, AppColors.statusErrorLight),
      StatusType.offline =>
        (AppColors.statusOffline, AppColors.statusOfflineLight),
    };

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: AppRadius.smRadius,
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 6,
            height: 6,
            decoration: BoxDecoration(
              color: color,
              shape: BoxShape.circle,
            ),
          ),
          const SizedBox(width: 5),
          Text(
            label,
            style: TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w500,
              color: color,
            ),
          ),
        ],
      ),
    );
  }
}

// 信息块（标签+值）
class InfoBlock extends StatelessWidget {
  final String label;
  final String value;
  final bool mono;

  const InfoBlock({
    super.key,
    required this.label,
    required this.value,
    this.mono = false,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          label,
          style: Theme.of(context).textTheme.labelSmall,
        ),
        const SizedBox(height: 3),
        Text(
          value,
          style: TextStyle(
            fontSize: 13,
            fontWeight: FontWeight.w500,
            color: AppColors.textPrimary,
            fontFamily: mono ? 'monospace' : null,
          ),
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
        ),
      ],
    );
  }
}

// 通用按钮
class AppButton extends StatelessWidget {
  final String label;
  final VoidCallback? onPressed;
  final bool loading;
  final bool outlined;
  final IconData? icon;
  final double? width;

  const AppButton({
    super.key,
    required this.label,
    this.onPressed,
    this.loading = false,
    this.outlined = false,
    this.icon,
    this.width,
  });

  @override
  Widget build(BuildContext context) {
    Widget child = loading
        ? const SizedBox(
            width: 18,
            height: 18,
            child: CircularProgressIndicator(
              strokeWidth: 2,
              color: AppColors.textOnPrimary,
            ),
          )
        : Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (icon != null) ...[
                Icon(icon, size: 16),
                const SizedBox(width: 6),
              ],
              Text(label),
            ],
          );

    if (outlined) {
      return SizedBox(
        width: width,
        child: OutlinedButton(
          onPressed: loading ? null : onPressed,
          child: child,
        ),
      );
    }
    return SizedBox(
      width: width,
      child: ElevatedButton(
        onPressed: loading ? null : onPressed,
        child: child,
      ),
    );
  }
}

// 通用输入框
class AppInput extends StatelessWidget {
  final String? label;
  final String? hint;
  final TextEditingController? controller;
  final bool obscureText;
  final TextInputType keyboardType;
  final String? errorText;
  final ValueChanged<String>? onChanged;
  final int maxLines;
  final Widget? suffixIcon;
  final bool autofocus;
  final FocusNode? focusNode;
  final VoidCallback? onSubmitted;

  const AppInput({
    super.key,
    this.label,
    this.hint,
    this.controller,
    this.obscureText = false,
    this.keyboardType = TextInputType.text,
    this.errorText,
    this.onChanged,
    this.maxLines = 1,
    this.suffixIcon,
    this.autofocus = false,
    this.focusNode,
    this.onSubmitted,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (label != null) ...[
          Text(
            label!,
            style: Theme.of(context).textTheme.labelLarge,
          ),
          const SizedBox(height: 6),
        ],
        TextField(
          controller: controller,
          obscureText: obscureText,
          keyboardType: keyboardType,
          maxLines: maxLines,
          autofocus: autofocus,
          focusNode: focusNode,
          onChanged: onChanged,
          onSubmitted: onSubmitted != null ? (_) => onSubmitted!() : null,
          decoration: InputDecoration(
            hintText: hint,
            errorText: errorText,
            suffixIcon: suffixIcon,
          ),
        ),
      ],
    );
  }
}

// 分割线带文字
class DividerWithText extends StatelessWidget {
  final String text;
  const DividerWithText({super.key, required this.text});

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        const Expanded(child: Divider()),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12),
          child: Text(
            text,
            style: Theme.of(context).textTheme.labelSmall,
          ),
        ),
        const Expanded(child: Divider()),
      ],
    );
  }
}

// 空状态提示
class EmptyState extends StatelessWidget {
  final String message;
  final IconData icon;
  const EmptyState({super.key, required this.message, required this.icon});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 48, color: AppColors.textMuted),
          const SizedBox(height: 12),
          Text(
            message,
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                  color: AppColors.textMuted,
                ),
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }
}

// 加载占位
class LoadingState extends StatelessWidget {
  const LoadingState({super.key});

  @override
  Widget build(BuildContext context) {
    return const Center(
      child: CircularProgressIndicator(
        color: AppColors.primary,
        strokeWidth: 2,
      ),
    );
  }
}
