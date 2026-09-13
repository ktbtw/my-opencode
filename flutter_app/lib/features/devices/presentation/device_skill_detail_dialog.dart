import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:markdown/markdown.dart' as md;

import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import '../data/device_skill_model.dart';

Future<void> showDeviceSkillDetailDialog({
  required BuildContext context,
  required String title,
  String description = '',
  String content = '',
  List<String> tags = const [],
  String category = '',
  String source = '',
  List<DeviceSkillPackageFile> packageFiles = const [],
}) {
  return showDialog<void>(
    context: context,
    builder: (context) => DeviceSkillDetailDialog(
      title: title,
      description: description,
      content: content,
      tags: tags,
      category: category,
      source: source,
      packageFiles: packageFiles,
    ),
  );
}

class DeviceSkillDetailDialog extends StatelessWidget {
  final String title;
  final String description;
  final String content;
  final List<String> tags;
  final String category;
  final String source;
  final List<DeviceSkillPackageFile> packageFiles;

  const DeviceSkillDetailDialog({
    super.key,
    required this.title,
    this.description = '',
    this.content = '',
    this.tags = const [],
    this.category = '',
    this.source = '',
    this.packageFiles = const [],
  });

  @override
  Widget build(BuildContext context) {
    final viewport = MediaQuery.sizeOf(context);
    final width = math.min(
      math.max(viewport.width - 24, 280).toDouble(),
      620.0,
    );
    final height = math.min(
      math.max(viewport.height - 36, 280).toDouble(),
      720.0,
    );
    final metadata = [
      category,
      source,
    ].where((value) => value.trim().isNotEmpty).toList(growable: false);

    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 12, vertical: 18),
      backgroundColor: AppColors.surface,
      shape: const RoundedRectangleBorder(borderRadius: AppRadius.lgRadius),
      child: SizedBox(
        width: width,
        height: height,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 18, 20, 14),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Container(
                    width: 38,
                    height: 38,
                    decoration: const BoxDecoration(
                      color: AppColors.primaryLight,
                      borderRadius: AppRadius.smRadius,
                    ),
                    child: const Icon(
                      Icons.auto_awesome_outlined,
                      color: AppColors.primary,
                      size: 20,
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          title,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                        if (metadata.isNotEmpty) ...[
                          const SizedBox(height: 4),
                          Text(
                            metadata.join(' · '),
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: Theme.of(context).textTheme.labelSmall,
                          ),
                        ],
                      ],
                    ),
                  ),
                  IconButton(
                    tooltip: '关闭',
                    onPressed: () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close_rounded),
                    visualDensity: VisualDensity.compact,
                  ),
                ],
              ),
              if (description.trim().isNotEmpty) ...[
                const SizedBox(height: 14),
                Container(
                  width: double.infinity,
                  padding: const EdgeInsets.all(12),
                  decoration: const BoxDecoration(
                    color: AppColors.background,
                    borderRadius: AppRadius.smRadius,
                  ),
                  child: Text(
                    description,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ),
              ],
              if (tags.isNotEmpty) ...[
                const SizedBox(height: 10),
                Wrap(
                  spacing: 6,
                  runSpacing: 6,
                  children: tags
                      .where((tag) => tag.trim().isNotEmpty)
                      .map(
                        (tag) =>
                            StatusPill(label: tag, type: StatusType.processing),
                      )
                      .toList(),
                ),
              ],
              const SizedBox(height: 14),
              Expanded(
                child: SingleChildScrollView(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      if (content.trim().isNotEmpty) ...[
                        Text(
                          'SKILL.md',
                          style: Theme.of(context).textTheme.labelLarge,
                        ),
                        const SizedBox(height: 8),
                        MarkdownBody(
                          data: content,
                          selectable: true,
                          extensionSet: md.ExtensionSet.gitHubFlavored,
                          styleSheet: _markdownStyleSheet(context),
                        ),
                      ] else
                        const Text(
                          '此 Skill 没有可展示的 SKILL.md 内容。',
                          style: TextStyle(color: AppColors.textMuted),
                        ),
                      if (packageFiles.isNotEmpty) ...[
                        const SizedBox(height: 18),
                        Text(
                          '附带文件',
                          style: Theme.of(context).textTheme.labelLarge,
                        ),
                        const SizedBox(height: 8),
                        Container(
                          width: double.infinity,
                          padding: const EdgeInsets.all(10),
                          decoration: const BoxDecoration(
                            color: AppColors.background,
                            borderRadius: AppRadius.smRadius,
                          ),
                          child: SelectableText(
                            packageFiles
                                .map((file) => file.path)
                                .where((path) => path.trim().isNotEmpty)
                                .join('\n'),
                            style: const TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 12,
                              height: 1.5,
                              color: AppColors.textSecondary,
                            ),
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 10),
              const Divider(height: 1),
              Align(
                alignment: Alignment.centerRight,
                child: TextButton(
                  onPressed: () => Navigator.of(context).pop(),
                  child: const Text('关闭'),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  MarkdownStyleSheet _markdownStyleSheet(BuildContext context) {
    final base = Theme.of(context).textTheme;
    return MarkdownStyleSheet(
      p: base.bodyMedium?.copyWith(color: AppColors.textPrimary, height: 1.55),
      h1: base.titleLarge?.copyWith(
        color: AppColors.textPrimary,
        fontWeight: FontWeight.w700,
      ),
      h2: base.titleMedium?.copyWith(
        color: AppColors.textPrimary,
        fontWeight: FontWeight.w700,
      ),
      h3: base.titleSmall?.copyWith(
        color: AppColors.textPrimary,
        fontWeight: FontWeight.w700,
      ),
      listBullet: base.bodyMedium?.copyWith(color: AppColors.textPrimary),
      code: const TextStyle(
        fontFamily: 'monospace',
        fontSize: 12,
        color: AppColors.codeText,
      ),
      codeblockDecoration: BoxDecoration(
        color: AppColors.codeBackground,
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: AppColors.border),
      ),
      blockquoteDecoration: const BoxDecoration(
        color: AppColors.primaryLight,
        border: Border(left: BorderSide(color: AppColors.primary, width: 3)),
      ),
      blockquote: base.bodyMedium?.copyWith(
        color: AppColors.textSecondary,
        fontStyle: FontStyle.italic,
      ),
      tableHead: base.bodySmall?.copyWith(fontWeight: FontWeight.w700),
      tableBody: base.bodySmall,
      tableBorder: TableBorder.all(color: AppColors.border),
      tableCellsPadding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
    );
  }
}
