import 'package:flutter/material.dart';

class PanelCard extends StatelessWidget {
  const PanelCard({
    super.key,
    required this.title,
    required this.subtitle,
    required this.child,
    this.expandChild = false,
  });

  final String title;
  final String subtitle;
  final Widget child;
  final bool expandChild;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: expandChild ? MainAxisSize.max : MainAxisSize.min,
          children: [
            Text(title, style: theme.textTheme.titleLarge),
            const SizedBox(height: 6),
            Text(subtitle, style: theme.textTheme.bodyMedium),
            const SizedBox(height: 20),
            if (expandChild) Expanded(child: child) else child,
          ],
        ),
      ),
    );
  }
}
