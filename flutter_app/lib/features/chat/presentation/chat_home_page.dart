import 'package:flutter/material.dart';

import '../../../shared/widgets/section_card.dart';

class ChatHomePage extends StatelessWidget {
  const ChatHomePage({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 24),
          children: [
            Text(
              'Chat Codex',
              style: theme.textTheme.headlineMedium,
            ),
            const SizedBox(height: 8),
            Text(
              '移动端控制台骨架已建立，下一步可以继续接入任务列表、会话详情和设备状态。',
              style: theme.textTheme.bodyLarge?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 24),
            const SectionCard(
              title: '当前目标',
              subtitle: '先把目录、页面骨架和后端对接入口搭起来。',
              child: _StatusList(
                items: [
                  '根目录独立 Flutter 工程',
                  '后续可扩展的 app/core/features/shared 分层',
                  '默认首页替换为项目启动页',
                ],
              ),
            ),
            const SizedBox(height: 16),
            const SectionCard(
              title: '下一步建议',
              subtitle: '后续建议按这个顺序继续。',
              child: _StatusList(
                items: [
                  '接入后端 base URL 与环境配置',
                  '实现设备列表与任务列表接口层',
                  '接入会话详情和流式输出展示',
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _StatusList extends StatelessWidget {
  const _StatusList({required this.items});

  final List<String> items;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      children: items
          .map(
            (item) => Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Container(
                    width: 8,
                    height: 8,
                    margin: const EdgeInsets.only(top: 6),
                    decoration: BoxDecoration(
                      color: theme.colorScheme.primary,
                      borderRadius: BorderRadius.circular(999),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(
                      item,
                      style: theme.textTheme.bodyLarge,
                    ),
                  ),
                ],
              ),
            ),
          )
          .toList(),
    );
  }
}
