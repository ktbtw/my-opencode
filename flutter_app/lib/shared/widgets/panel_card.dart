import 'package:flutter/material.dart';

class PanelCard extends StatelessWidget {
  const PanelCard({
    super.key,
    required this.title,
    required this.child,
    this.subtitle,
    this.expandChild = false,
    this.padding = const EdgeInsets.all(24),
  });

  final String title;
  final String? subtitle;
  final Widget child;
  final bool expandChild;
  final EdgeInsetsGeometry padding;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Card(
      child: Padding(
        padding: padding,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: expandChild ? MainAxisSize.max : MainAxisSize.min,
          children: [
            Text(title, style: theme.textTheme.titleLarge),
            if (subtitle != null && subtitle!.trim().isNotEmpty) ...[
              const SizedBox(height: 8),
              Text(subtitle!, style: theme.textTheme.bodyMedium),
              const SizedBox(height: 18),
            ] else
              const SizedBox(height: 18),
            if (expandChild) Expanded(child: child) else child,
          ],
        ),
      ),
    );
  }
}
