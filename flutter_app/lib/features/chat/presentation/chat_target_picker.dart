import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../devices/presentation/device_provider.dart';
import '../data/chat_target.dart';

Future<ChatTarget?> showChatTargetPicker(
  BuildContext context, {
  required ChatTarget current,
  required bool mobile,
}) {
  final picker = _ChatTargetPicker(current: current);
  if (mobile) {
    return showModalBottomSheet<ChatTarget>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      backgroundColor: AppColors.surface,
      builder: (_) => picker,
    );
  }
  return showDialog<ChatTarget>(
    context: context,
    builder: (_) =>
        Dialog(child: SizedBox(width: 430, height: 620, child: picker)),
  );
}

class _ChatTargetPicker extends ConsumerStatefulWidget {
  final ChatTarget current;

  const _ChatTargetPicker({required this.current});

  @override
  ConsumerState<_ChatTargetPicker> createState() => _ChatTargetPickerState();
}

class _ChatTargetPickerState extends ConsumerState<_ChatTargetPicker> {
  final _searchController = TextEditingController();
  String _query = '';

  @override
  void dispose() {
    _searchController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final devices = ref.watch(deviceListProvider);
    final query = _query.trim().toLowerCase();
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 12),
      child: Column(
        children: [
          Row(
            children: [
              const Text(
                '切换 Agent',
                style: TextStyle(fontSize: 17, fontWeight: FontWeight.w700),
              ),
              const Spacer(),
              IconButton(
                tooltip: '关闭',
                onPressed: () => Navigator.of(context).pop(),
                icon: const Icon(Icons.close),
              ),
            ],
          ),
          TextField(
            controller: _searchController,
            autofocus: true,
            onChanged: (value) => setState(() => _query = value),
            decoration: InputDecoration(
              hintText: '搜索设备、Agent 或项目',
              prefixIcon: const Icon(Icons.search),
              isDense: true,
              border: OutlineInputBorder(borderRadius: AppRadius.smRadius),
            ),
          ),
          const SizedBox(height: 12),
          Expanded(
            child: devices.when(
              loading: () => const _ChatTargetSkeleton(),
              error: (error, _) => _ChatTargetError(message: error.toString()),
              data: (items) {
                final targets = availableChatTargets(items)
                    .where((target) {
                      if (query.isEmpty) return true;
                      final text =
                          '${target.deviceName} ${target.agentName} ${target.semanticAgentName} ${target.projectName} ${target.agentId} ${target.projectId}'
                              .toLowerCase();
                      return text.contains(query);
                    })
                    .toList(growable: false);
                if (targets.isEmpty) {
                  return const Center(child: Text('暂无可用 Agent'));
                }
                return ListView.separated(
                  itemCount: targets.length,
                  separatorBuilder: (_, _) => const Divider(height: 1),
                  itemBuilder: (_, index) {
                    final target = targets[index];
                    final selected = target.matches(
                      machineId: widget.current.machineId,
                      agentId: widget.current.agentId,
                    );
                    return ListTile(
                      contentPadding: const EdgeInsets.symmetric(horizontal: 4),
                      leading: CircleAvatar(
                        radius: 18,
                        backgroundColor: selected
                            ? AppColors.primaryLight
                            : AppColors.inputBackground,
                        child: Icon(
                          Icons.smart_toy_outlined,
                          size: 19,
                          color: selected
                              ? AppColors.primary
                              : AppColors.textSecondary,
                        ),
                      ),
                      title: Text(
                        target.agentName,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                      subtitle: _TargetSubtitle(target: target),
                      isThreeLine: target.semanticAgentName.trim().isNotEmpty,
                      trailing: selected
                          ? const Icon(Icons.check, color: AppColors.primary)
                          : null,
                      onTap: () => Navigator.of(context).pop(target),
                    );
                  },
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

class _TargetSubtitle extends StatelessWidget {
  final ChatTarget target;

  const _TargetSubtitle({required this.target});

  @override
  Widget build(BuildContext context) {
    final semanticName = target.semanticAgentName.trim();
    final metadata = <String>[
      target.deviceName,
      if (target.projectName != target.agentName) target.projectName,
    ].join(' · ');
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (semanticName.isNotEmpty)
          SizedBox(
            width: double.infinity,
            child: FittedBox(
              fit: BoxFit.scaleDown,
              alignment: Alignment.centerLeft,
              child: Text(semanticName, maxLines: 1),
            ),
          ),
        Text(metadata, maxLines: 1, overflow: TextOverflow.ellipsis),
      ],
    );
  }
}

class _ChatTargetSkeleton extends StatelessWidget {
  const _ChatTargetSkeleton();

  @override
  Widget build(BuildContext context) {
    return ListView.separated(
      itemCount: 5,
      separatorBuilder: (_, _) => const SizedBox(height: 12),
      itemBuilder: (_, _) => Container(
        height: 58,
        decoration: BoxDecoration(
          color: AppColors.inputBackground,
          borderRadius: AppRadius.smRadius,
        ),
      ),
    );
  }
}

class _ChatTargetError extends StatelessWidget {
  final String message;

  const _ChatTargetError({required this.message});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Text(
        '加载 Agent 列表失败\n$message',
        textAlign: TextAlign.center,
        style: const TextStyle(color: AppColors.textSecondary),
      ),
    );
  }
}
