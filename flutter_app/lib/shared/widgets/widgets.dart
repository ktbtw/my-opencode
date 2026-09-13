import 'package:flutter/material.dart';
import '../../core/theme/app_colors.dart';
import '../../core/theme/app_theme.dart';
import '../../core/notifications/app_notification_host.dart';

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
  final String? subtitle;
  final List<Widget>? actions;
  final Widget? leading;
  final bool showDivider;

  const AppTopBar({
    super.key,
    required this.title,
    this.subtitle,
    this.actions,
    this.leading,
    this.showDivider = true,
  });

  @override
  Size get preferredSize =>
      Size.fromHeight(subtitle?.trim().isNotEmpty == true ? 72 : 56);

  @override
  Widget build(BuildContext context) {
    final hasSubtitle = subtitle?.trim().isNotEmpty == true;
    final mainRow = Row(
      children: [
        if (leading != null) ...[leading!, const SizedBox(width: 12)],
        Expanded(
          child: Text(
            title,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: Theme.of(context).textTheme.titleLarge,
          ),
        ),
        if (actions != null && actions!.isNotEmpty) ...[
          const SizedBox(width: 12),
          ...actions!,
        ],
        const AppNotificationCenterButton(),
      ],
    );
    return Container(
      height: hasSubtitle ? 72 : 56,
      decoration: BoxDecoration(
        color: AppColors.surface,
        border: showDivider
            ? const Border(bottom: BorderSide(color: AppColors.border))
            : null,
      ),
      padding: const EdgeInsets.symmetric(horizontal: 20),
      child: hasSubtitle
          ? Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                SizedBox(height: 52, child: mainRow),
                SizedBox(
                  height: 16,
                  child: Text(
                    subtitle!.trim(),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      color: AppColors.textMuted,
                      fontSize: 11,
                      height: 1.2,
                    ),
                  ),
                ),
              ],
            )
          : mainRow,
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
      StatusType.online => (
        AppColors.statusOnline,
        AppColors.statusOnlineLight,
      ),
      StatusType.processing => (
        AppColors.statusProcessing,
        AppColors.statusProcessingLight,
      ),
      StatusType.warning => (
        AppColors.statusWarning,
        AppColors.statusWarningLight,
      ),
      StatusType.error => (AppColors.statusError, AppColors.statusErrorLight),
      StatusType.offline => (
        AppColors.statusOffline,
        AppColors.statusOfflineLight,
      ),
    };

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(color: bg, borderRadius: AppRadius.smRadius),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 6,
            height: 6,
            decoration: BoxDecoration(color: color, shape: BoxShape.circle),
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
        Text(label, style: Theme.of(context).textTheme.labelSmall),
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
        ? SizedBox(
            width: 18,
            height: 18,
            child: CircularProgressIndicator(
              strokeWidth: 2,
              color: outlined ? AppColors.primary : AppColors.textOnPrimary,
            ),
          )
        : Row(
            mainAxisSize: MainAxisSize.min,
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              if (icon != null) ...[
                Icon(icon, size: 16),
                const SizedBox(width: 6),
              ],
              Text(
                label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                softWrap: false,
                textAlign: TextAlign.center,
              ),
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
          Text(label!, style: Theme.of(context).textTheme.labelLarge),
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

class AppSelectOption<T> {
  final T value;
  final String label;
  final String? description;
  final IconData? icon;
  final Color? color;

  const AppSelectOption({
    required this.value,
    required this.label,
    this.description,
    this.icon,
    this.color,
  });
}

typedef AppSelectTriggerBuilder<T> =
    Widget Function(
      BuildContext context,
      AppSelectOption<T>? selected,
      bool isOpen,
    );

typedef AppSelectOptionBuilder<T> =
    Widget Function(
      BuildContext context,
      AppSelectOption<T> option,
      bool selected,
    );

class AppSelect<T> extends StatelessWidget {
  final String? label;
  final String? placeholder;
  final String? errorText;
  final T? value;
  final List<AppSelectOption<T>> options;
  final ValueChanged<T>? onChanged;
  final double maxMenuHeight;

  const AppSelect({
    super.key,
    this.label,
    this.placeholder,
    this.errorText,
    required this.value,
    required this.options,
    required this.onChanged,
    this.maxMenuHeight = 280,
  });

  @override
  Widget build(BuildContext context) {
    final enabled = onChanged != null && options.isNotEmpty;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (label != null) ...[
          Text(label!, style: Theme.of(context).textTheme.labelLarge),
          const SizedBox(height: 6),
        ],
        _AppSelectAnchor<T>(
          value: value,
          options: options,
          onChanged: enabled ? onChanged : null,
          maxMenuHeight: maxMenuHeight,
          triggerBuilder: (context, selected, isOpen) {
            final display = selected?.label ?? placeholder ?? '请选择';
            return Container(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
              decoration: BoxDecoration(
                color: enabled
                    ? AppColors.inputBackground
                    : AppColors.inputBackground.withValues(alpha: 0.55),
                borderRadius: AppRadius.smRadius,
                border: Border.all(
                  color: errorText == null
                      ? (isOpen ? AppColors.inputFocused : AppColors.border)
                      : AppColors.statusError,
                  width: isOpen ? 1.5 : 1,
                ),
              ),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      display,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 14,
                        color: selected == null
                            ? AppColors.textMuted
                            : enabled
                            ? AppColors.textPrimary
                            : AppColors.textSecondary,
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  Icon(
                    isOpen ? Icons.expand_less : Icons.expand_more,
                    size: 18,
                    color: enabled
                        ? AppColors.textSecondary
                        : AppColors.textMuted,
                  ),
                ],
              ),
            );
          },
        ),
        if (errorText != null) ...[
          const SizedBox(height: 6),
          Text(
            errorText!,
            style: const TextStyle(fontSize: 12, color: AppColors.statusError),
          ),
        ],
      ],
    );
  }
}

class AppSelectButton<T> extends StatelessWidget {
  final T? value;
  final List<AppSelectOption<T>> options;
  final ValueChanged<T>? onChanged;
  final String? tooltip;
  final double? menuWidth;
  final double maxMenuHeight;
  final AppSelectTriggerBuilder<T> triggerBuilder;
  final AppSelectOptionBuilder<T>? optionBuilder;

  const AppSelectButton({
    super.key,
    required this.value,
    required this.options,
    required this.onChanged,
    required this.triggerBuilder,
    this.optionBuilder,
    this.tooltip,
    this.menuWidth,
    this.maxMenuHeight = 280,
  });

  @override
  Widget build(BuildContext context) {
    final anchor = _AppSelectAnchor<T>(
      value: value,
      options: options,
      onChanged: onChanged,
      tooltip: tooltip,
      menuWidth: menuWidth,
      maxMenuHeight: maxMenuHeight,
      optionBuilder: optionBuilder,
      triggerBuilder: triggerBuilder,
    );
    return anchor;
  }
}

class _AppSelectAnchor<T> extends StatefulWidget {
  final T? value;
  final List<AppSelectOption<T>> options;
  final ValueChanged<T>? onChanged;
  final String? tooltip;
  final double? menuWidth;
  final double maxMenuHeight;
  final AppSelectTriggerBuilder<T> triggerBuilder;
  final AppSelectOptionBuilder<T>? optionBuilder;

  const _AppSelectAnchor({
    required this.value,
    required this.options,
    required this.onChanged,
    required this.triggerBuilder,
    this.optionBuilder,
    this.tooltip,
    this.menuWidth,
    required this.maxMenuHeight,
  });

  @override
  State<_AppSelectAnchor<T>> createState() => _AppSelectAnchorState<T>();
}

class _AppSelectAnchorState<T> extends State<_AppSelectAnchor<T>> {
  final LayerLink _layerLink = LayerLink();
  final GlobalKey _targetKey = GlobalKey();
  OverlayEntry? _entry;

  bool get _isOpen => _entry != null;

  AppSelectOption<T>? get _selected {
    for (final option in widget.options) {
      if (option.value == widget.value) return option;
    }
    return null;
  }

  @override
  void didUpdateWidget(covariant _AppSelectAnchor<T> oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.onChanged == null || widget.options.isEmpty) {
      _hideMenu();
    }
  }

  @override
  void dispose() {
    _entry?.remove();
    _entry = null;
    super.dispose();
  }

  void _toggleMenu() {
    if (widget.onChanged == null || widget.options.isEmpty) return;
    if (_isOpen) {
      _hideMenu();
    } else {
      _openMenu();
    }
  }

  void _openMenu() {
    final renderObject = _targetKey.currentContext?.findRenderObject();
    if (renderObject is! RenderBox) return;
    final size = renderObject.size;
    final overlay = Overlay.of(context);
    _entry = OverlayEntry(
      builder: (context) => Stack(
        children: [
          Positioned.fill(
            child: GestureDetector(
              behavior: HitTestBehavior.translucent,
              onTap: _hideMenu,
              child: const SizedBox.expand(),
            ),
          ),
          CompositedTransformFollower(
            link: _layerLink,
            showWhenUnlinked: false,
            targetAnchor: Alignment.bottomLeft,
            followerAnchor: Alignment.topLeft,
            offset: const Offset(0, 6),
            child: Material(
              color: Colors.transparent,
              child: ConstrainedBox(
                constraints: BoxConstraints(
                  minWidth: widget.menuWidth ?? size.width,
                  maxWidth: widget.menuWidth ?? size.width,
                  maxHeight: widget.maxMenuHeight,
                ),
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    color: AppColors.surface,
                    borderRadius: AppRadius.mdRadius,
                    border: Border.all(color: AppColors.border),
                    boxShadow: const [
                      BoxShadow(
                        color: Color(0x1A1A3A6A),
                        blurRadius: 18,
                        offset: Offset(0, 8),
                      ),
                    ],
                  ),
                  child: ClipRRect(
                    borderRadius: AppRadius.mdRadius,
                    child: ListView.builder(
                      padding: const EdgeInsets.symmetric(vertical: 6),
                      shrinkWrap: true,
                      itemCount: widget.options.length,
                      itemBuilder: (context, index) {
                        final option = widget.options[index];
                        final selected = option.value == widget.value;
                        return InkWell(
                          onTap: () {
                            _hideMenu();
                            widget.onChanged?.call(option.value);
                          },
                          child:
                              widget.optionBuilder?.call(
                                context,
                                option,
                                selected,
                              ) ??
                              _defaultOption(context, option, selected),
                        );
                      },
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
    overlay.insert(_entry!);
    setState(() {});
  }

  void _hideMenu() {
    final current = _entry;
    if (current == null) return;
    current.remove();
    _entry = null;
    if (mounted) setState(() {});
  }

  Widget _defaultOption(
    BuildContext context,
    AppSelectOption<T> option,
    bool selected,
  ) {
    final color = option.color ?? AppColors.primary;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      color: selected ? color.withValues(alpha: 0.08) : Colors.transparent,
      child: Row(
        children: [
          if (option.icon != null) ...[
            Icon(
              option.icon,
              size: 17,
              color: selected ? color : AppColors.textSecondary,
            ),
            const SizedBox(width: 8),
          ],
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  option.label,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    fontSize: 13,
                    fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
                    color: selected ? color : AppColors.textPrimary,
                  ),
                ),
                if (option.description != null) ...[
                  const SizedBox(height: 2),
                  Text(
                    option.description!,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      fontSize: 11,
                      color: AppColors.textSecondary,
                    ),
                  ),
                ],
              ],
            ),
          ),
          if (selected) ...[
            const SizedBox(width: 8),
            Icon(Icons.check, size: 16, color: color),
          ],
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    Widget child = CompositedTransformTarget(
      link: _layerLink,
      child: GestureDetector(
        key: _targetKey,
        behavior: HitTestBehavior.opaque,
        onTap: _toggleMenu,
        child: widget.triggerBuilder(context, _selected, _isOpen),
      ),
    );

    if (widget.tooltip != null) {
      child = Tooltip(message: widget.tooltip!, child: child);
    }
    return child;
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
          child: Text(text, style: Theme.of(context).textTheme.labelSmall),
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
            style: Theme.of(
              context,
            ).textTheme.bodyMedium?.copyWith(color: AppColors.textMuted),
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

class _SkeletonBox extends StatelessWidget {
  final double? width;
  final double height;

  const _SkeletonBox({this.width, required this.height});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: width,
      height: height,
      decoration: BoxDecoration(
        color: AppColors.border.withValues(alpha: 0.55),
        borderRadius: const BorderRadius.all(Radius.circular(6)),
      ),
    );
  }
}

class DialogContentSkeleton extends StatelessWidget {
  final int itemCount;
  final bool showHeader;

  const DialogContentSkeleton({
    super.key,
    this.itemCount = 3,
    this.showHeader = true,
  });

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: '正在加载',
      child: ExcludeSemantics(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (showHeader) ...[
              const _SkeletonBox(width: 176, height: 20),
              const SizedBox(height: 10),
              const _SkeletonBox(width: 238, height: 13),
              const SizedBox(height: 20),
            ],
            for (var index = 0; index < itemCount; index++) ...[
              const Row(
                children: [
                  _SkeletonBox(width: 38, height: 38),
                  SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        _SkeletonBox(height: 15),
                        SizedBox(height: 8),
                        _SkeletonBox(width: 156, height: 12),
                      ],
                    ),
                  ),
                ],
              ),
              if (index < itemCount - 1) const SizedBox(height: 16),
            ],
          ],
        ),
      ),
    );
  }
}

class PageLoadingOverlay extends StatelessWidget {
  final Widget child;
  final bool loading;

  const PageLoadingOverlay({
    super.key,
    required this.child,
    required this.loading,
  });

  @override
  Widget build(BuildContext context) {
    return Stack(
      fit: StackFit.expand,
      children: [
        child,
        if (loading)
          const Positioned(
            top: 0,
            left: 0,
            right: 0,
            child: LinearProgressIndicator(minHeight: 2),
          ),
      ],
    );
  }
}

class DeviceAIConfigSkeleton extends StatelessWidget {
  const DeviceAIConfigSkeleton({super.key});

  @override
  Widget build(BuildContext context) {
    return _PageSkeleton(
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          const _ConfigSummarySkeleton(lines: 3),
          const SizedBox(height: 16),
          for (var index = 0; index < 3; index++) ...[
            const _ProviderConfigSkeleton(),
            if (index < 2) const SizedBox(height: 12),
          ],
        ],
      ),
    );
  }
}

class DeviceMCPConfigSkeleton extends StatelessWidget {
  final bool agentMode;

  const DeviceMCPConfigSkeleton({super.key, this.agentMode = false});

  @override
  Widget build(BuildContext context) {
    return _PageSkeleton(
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          PanelCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const _SkeletonBox(width: 150, height: 20),
                const SizedBox(height: 10),
                const _SkeletonBox(width: 210, height: 13),
                const SizedBox(height: 8),
                const _SkeletonBox(height: 13),
                const SizedBox(height: 16),
                Wrap(
                  spacing: 12,
                  runSpacing: 12,
                  children: const [
                    _SkeletonBox(width: 150, height: 42),
                    _SkeletonBox(width: 150, height: 42),
                    _SkeletonBox(width: 150, height: 42),
                  ],
                ),
                if (agentMode) ...[
                  const SizedBox(height: 16),
                  const _SkeletonBox(width: 168, height: 18),
                  const SizedBox(height: 12),
                  const Row(
                    children: [
                      Expanded(child: _SkeletonBox(height: 42)),
                      SizedBox(width: 12),
                      Expanded(child: _SkeletonBox(height: 42)),
                    ],
                  ),
                ],
              ],
            ),
          ),
          const SizedBox(height: 16),
          for (var index = 0; index < 4; index++) ...[
            const _MCPServerConfigSkeleton(),
            if (index < 3) const SizedBox(height: 12),
          ],
        ],
      ),
    );
  }
}

class DeviceEnvConfigSkeleton extends StatelessWidget {
  final bool agentMode;

  const DeviceEnvConfigSkeleton({super.key, this.agentMode = false});

  @override
  Widget build(BuildContext context) {
    return _PageSkeleton(
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          const _ConfigSummarySkeleton(lines: 2),
          const SizedBox(height: 16),
          PanelCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    _SkeletonBox(width: agentMode ? 150 : 124, height: 20),
                    const SizedBox(width: 8),
                    const _SkeletonBox(width: 34, height: 22),
                  ],
                ),
                const SizedBox(height: 12),
                const _SkeletonBox(height: 220),
              ],
            ),
          ),
          const SizedBox(height: 16),
          const Align(
            alignment: Alignment.centerRight,
            child: _SkeletonBox(width: 140, height: 42),
          ),
        ],
      ),
    );
  }
}

class DeviceMCPStoreSkeleton extends StatelessWidget {
  const DeviceMCPStoreSkeleton({super.key});

  @override
  Widget build(BuildContext context) {
    final horizontalPadding = AppBreakpoints.isMobile(context) ? 16.0 : 24.0;
    return _PageSkeleton(
      child: ListView(
        padding: EdgeInsets.fromLTRB(
          horizontalPadding,
          16,
          horizontalPadding,
          24,
        ),
        children: [
          PanelCard(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const _SkeletonBox(height: 44),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: const [
                    _SkeletonBox(width: 82, height: 24),
                    _SkeletonBox(width: 92, height: 24),
                    _SkeletonBox(width: 86, height: 24),
                    _SkeletonBox(width: 98, height: 24),
                  ],
                ),
                const SizedBox(height: 12),
                const _SkeletonBox(height: 38),
              ],
            ),
          ),
          const SizedBox(height: 12),
          for (var index = 0; index < 5; index++) ...[
            const _MCPStoreItemSkeleton(),
            if (index < 4) const SizedBox(height: 10),
          ],
        ],
      ),
    );
  }
}

class DeviceSkillConfigSkeleton extends StatelessWidget {
  const DeviceSkillConfigSkeleton({super.key});

  @override
  Widget build(BuildContext context) {
    return _PageSkeleton(
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          PanelCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const _SkeletonBox(width: 170, height: 20),
                const SizedBox(height: 10),
                const _SkeletonBox(width: 130, height: 13),
                const SizedBox(height: 8),
                const _SkeletonBox(height: 13),
                const SizedBox(height: 16),
                const _SkeletonBox(height: 54),
              ],
            ),
          ),
          const SizedBox(height: 12),
          const _SkeletonBox(height: 52),
          const SizedBox(height: 12),
          for (var index = 0; index < 5; index++) ...[
            const _MCPStoreItemSkeleton(),
            if (index < 4) const SizedBox(height: 10),
          ],
        ],
      ),
    );
  }
}

class DeviceSkillStoreSkeleton extends StatelessWidget {
  const DeviceSkillStoreSkeleton({super.key});

  @override
  Widget build(BuildContext context) {
    return _PageSkeleton(
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          PanelCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const _SkeletonBox(width: 170, height: 20),
                const SizedBox(height: 10),
                const _SkeletonBox(width: 130, height: 13),
                const SizedBox(height: 16),
                const _SkeletonBox(height: 54),
                const SizedBox(height: 12),
                const _SkeletonBox(height: 48),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 8,
                  children: const [
                    _SkeletonBox(width: 64, height: 28),
                    _SkeletonBox(width: 82, height: 28),
                    _SkeletonBox(width: 72, height: 28),
                  ],
                ),
              ],
            ),
          ),
          const SizedBox(height: 12),
          for (var index = 0; index < 5; index++) ...[
            const _MCPStoreItemSkeleton(),
            if (index < 4) const SizedBox(height: 10),
          ],
        ],
      ),
    );
  }
}

class _PageSkeleton extends StatelessWidget {
  final Widget child;

  const _PageSkeleton({required this.child});

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: '正在加载',
      child: ExcludeSemantics(child: child),
    );
  }
}

class _ConfigSummarySkeleton extends StatelessWidget {
  final int lines;

  const _ConfigSummarySkeleton({required this.lines});

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const _SkeletonBox(width: 170, height: 20),
          const SizedBox(height: 10),
          const _SkeletonBox(width: 220, height: 13),
          const SizedBox(height: 8),
          const _SkeletonBox(height: 13),
          for (var index = 0; index < lines - 2; index++) ...[
            const SizedBox(height: 8),
            const _SkeletonBox(width: 190, height: 13),
          ],
        ],
      ),
    );
  }
}

class _ProviderConfigSkeleton extends StatelessWidget {
  const _ProviderConfigSkeleton();

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Row(
            children: [
              Expanded(child: _SkeletonBox(width: 170, height: 19)),
              SizedBox(width: 12),
              _SkeletonBox(width: 28, height: 28),
              SizedBox(width: 6),
              _SkeletonBox(width: 28, height: 28),
              SizedBox(width: 6),
              _SkeletonBox(width: 28, height: 28),
            ],
          ),
          const SizedBox(height: 14),
          const _InfoSkeletonRow(),
          const SizedBox(height: 9),
          const _InfoSkeletonRow(width: 150),
          const SizedBox(height: 9),
          const _InfoSkeletonRow(width: 110),
          const SizedBox(height: 9),
          const _InfoSkeletonRow(width: 180),
        ],
      ),
    );
  }
}

class _MCPServerConfigSkeleton extends StatelessWidget {
  const _MCPServerConfigSkeleton();

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Row(
            children: [
              Expanded(child: _SkeletonBox(width: 150, height: 19)),
              SizedBox(width: 12),
              _SkeletonBox(width: 28, height: 28),
              SizedBox(width: 6),
              _SkeletonBox(width: 28, height: 28),
            ],
          ),
          const SizedBox(height: 14),
          const _InfoSkeletonRow(width: 220),
          const SizedBox(height: 9),
          const _InfoSkeletonRow(width: 180),
          const SizedBox(height: 9),
          const _SkeletonBox(width: 260, height: 14),
        ],
      ),
    );
  }
}

class _InfoSkeletonRow extends StatelessWidget {
  final double width;

  const _InfoSkeletonRow({this.width = 260});

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const _SkeletonBox(width: 68, height: 14),
        const SizedBox(width: 8),
        Expanded(child: _SkeletonBox(width: width, height: 14)),
      ],
    );
  }
}

class _MCPStoreItemSkeleton extends StatelessWidget {
  const _MCPStoreItemSkeleton();

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      padding: const EdgeInsets.all(12),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const _SkeletonBox(width: 32, height: 32),
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Row(
                  children: [
                    Expanded(child: _SkeletonBox(width: 180, height: 17)),
                    SizedBox(width: 12),
                    _SkeletonBox(width: 60, height: 22),
                  ],
                ),
                const SizedBox(height: 9),
                const _SkeletonBox(width: 260, height: 13),
                const SizedBox(height: 7),
                const _SkeletonBox(width: 200, height: 13),
                const SizedBox(height: 10),
                Wrap(
                  spacing: 8,
                  children: const [
                    _SkeletonBox(width: 58, height: 22),
                    _SkeletonBox(width: 58, height: 22),
                    _SkeletonBox(width: 72, height: 22),
                  ],
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class DeviceListSkeleton extends StatelessWidget {
  const DeviceListSkeleton({super.key});

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final width = constraints.maxWidth;
        final crossCount = width >= AppBreakpoints.lg
            ? 3
            : width >= AppBreakpoints.sm
            ? 2
            : 1;
        final horizontalPadding = AppBreakpoints.isMobile(context)
            ? 16.0
            : 24.0;
        return CustomScrollView(
          slivers: [
            SliverToBoxAdapter(
              child: Padding(
                padding: EdgeInsets.fromLTRB(
                  horizontalPadding,
                  16,
                  horizontalPadding,
                  4,
                ),
                child: const Row(
                  children: [
                    Expanded(child: _SkeletonBox(height: 46)),
                    SizedBox(width: 12),
                    _SkeletonBox(width: 72, height: 18),
                  ],
                ),
              ),
            ),
            SliverPadding(
              padding: EdgeInsets.all(horizontalPadding),
              sliver: SliverGrid(
                gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                  crossAxisCount: crossCount,
                  mainAxisSpacing: 12,
                  crossAxisSpacing: 12,
                  childAspectRatio: 1.65,
                ),
                delegate: SliverChildBuilderDelegate(
                  (context, index) => const _DeviceCardSkeleton(),
                  childCount: crossCount * 2,
                ),
              ),
            ),
          ],
        );
      },
    );
  }
}

class _DeviceCardSkeleton extends StatelessWidget {
  const _DeviceCardSkeleton();

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: AppColors.surface,
        borderRadius: AppRadius.lgRadius,
        border: Border.all(color: AppColors.border),
      ),
      padding: const EdgeInsets.all(18),
      child: const Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              _SkeletonBox(width: 40, height: 40),
              SizedBox(width: 12),
              Expanded(child: _SkeletonBox(height: 18)),
            ],
          ),
          Spacer(),
          _SkeletonBox(width: 150, height: 13),
          SizedBox(height: 9),
          _SkeletonBox(width: 105, height: 13),
        ],
      ),
    );
  }
}

class DeviceDetailSkeleton extends StatelessWidget {
  const DeviceDetailSkeleton({super.key});

  @override
  Widget build(BuildContext context) {
    final isMobile = AppBreakpoints.isMobile(context);
    final info = const _DeviceInfoSkeleton();
    final agents = const _AgentListSkeleton();
    if (isMobile) {
      return SingleChildScrollView(
        padding: const EdgeInsets.all(16),
        child: Column(children: [info, const SizedBox(height: 16), agents]),
      );
    }
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 320,
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: info,
          ),
        ),
        Container(width: 1, color: AppColors.border),
        Expanded(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: agents,
          ),
        ),
      ],
    );
  }
}

class _DeviceInfoSkeleton extends StatelessWidget {
  const _DeviceInfoSkeleton();

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const _SkeletonBox(width: 44, height: 44),
              const SizedBox(width: 12),
              const Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    _SkeletonBox(width: 150, height: 18),
                    SizedBox(height: 8),
                    _SkeletonBox(width: 64, height: 14),
                  ],
                ),
              ),
              const _SkeletonBox(width: 28, height: 28),
            ],
          ),
          const SizedBox(height: 20),
          const Divider(),
          const SizedBox(height: 18),
          const _SkeletonBox(width: 90, height: 13),
          const SizedBox(height: 10),
          const _SkeletonBox(width: 190, height: 18),
          const SizedBox(height: 18),
          const _SkeletonBox(width: 90, height: 13),
          const SizedBox(height: 10),
          const _SkeletonBox(width: 150, height: 18),
          const SizedBox(height: 22),
          const _SkeletonBox(height: 42),
          const SizedBox(height: 12),
          const _SkeletonBox(height: 42),
        ],
      ),
    );
  }
}

class _AgentListSkeleton extends StatelessWidget {
  const _AgentListSkeleton();

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Row(
          children: [
            _SkeletonBox(width: 130, height: 22),
            Spacer(),
            _SkeletonBox(width: 92, height: 34),
          ],
        ),
        const SizedBox(height: 16),
        for (var index = 0; index < 4; index++) ...[
          const _AgentCardSkeleton(),
          if (index < 3) const SizedBox(height: 12),
        ],
      ],
    );
  }
}

class _AgentCardSkeleton extends StatelessWidget {
  const _AgentCardSkeleton();

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      child: Row(
        children: [
          const _SkeletonBox(width: 38, height: 38),
          const SizedBox(width: 12),
          const Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                _SkeletonBox(width: 170, height: 17),
                SizedBox(height: 9),
                _SkeletonBox(width: 120, height: 13),
              ],
            ),
          ),
          const SizedBox(width: 12),
          const _SkeletonBox(width: 28, height: 28),
        ],
      ),
    );
  }
}
