import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../devices/data/device_semantic_agent_model.dart';
import '../../devices/presentation/device_provider.dart';

const _ffmpegRuntimeId = 'ffmpeg';

// 同一次会话里已确认可用的设备：避免每次切模型都重新查询、重复弹窗。
final _readyDevices = <String>{};
// 已经问过且用户选择「稍后」的设备：本次会话不再追问。
final _dismissedDevices = <String>{};

/// 切到视频模型时的 FFmpeg 检查。
///
/// 设备上没有 FFmpeg 就引导用户下载：确认后原地切换成安装进度框。
/// 没有 FFmpeg 不影响视频生成，只是抽不出首帧封面，所以用户拒绝或
/// 查询失败都不阻断使用。
Future<void> ensureFfmpegForVideo({
  required BuildContext context,
  required WidgetRef ref,
  required String machineId,
}) async {
  final id = machineId.trim();
  if (id.isEmpty ||
      _readyDevices.contains(id) ||
      _dismissedDevices.contains(id)) {
    return;
  }
  final repo = ref.read(deviceRepositoryProvider);
  DeviceRuntimeStatus status;
  try {
    status = await repo.getDeviceRuntimeStatus(
      machineId: id,
      runtimeId: _ffmpegRuntimeId,
    );
  } catch (_) {
    // 设备离线或 launcher 版本过旧：不打扰用户。
    return;
  }
  if (status.installed) {
    _readyDevices.add(id);
    return;
  }
  if (!context.mounted) return;

  final confirmed = await showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      title: const Text('需要安装 FFmpeg'),
      content: const Text(
        '生成视频需要设备上的 FFmpeg 来抽取首帧封面。\n\n'
        '安装包约 20-30MB，下载后保存在设备的运行时组件目录里，'
        '不会影响视频本身（不装也能生成，只是没有封面）。',
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(dialogContext).pop(false),
          child: const Text('稍后'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(dialogContext).pop(true),
          child: const Text('下载并安装'),
        ),
      ],
    ),
  );
  if (confirmed != true) {
    _dismissedDevices.add(id);
    return;
  }
  if (!context.mounted) return;
  await showDialog<void>(
    context: context,
    barrierDismissible: false,
    builder: (_) => _FfmpegInstallDialog(machineId: id),
  );
}

class _FfmpegInstallDialog extends ConsumerStatefulWidget {
  final String machineId;

  const _FfmpegInstallDialog({required this.machineId});

  @override
  ConsumerState<_FfmpegInstallDialog> createState() =>
      _FfmpegInstallDialogState();
}

class _FfmpegInstallDialogState extends ConsumerState<_FfmpegInstallDialog> {
  Timer? _timer;
  RuntimePreflightJob? _job;
  final List<RuntimePreflightEvent> _events = [];
  int _lastSequence = 0;
  String _error = '';
  bool _starting = true;
  bool _finished = false;
  bool _succeeded = false;

  @override
  void initState() {
    super.initState();
    unawaited(_start());
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _start() async {
    try {
      final job = await ref
          .read(deviceRepositoryProvider)
          .installDeviceRuntime(
            machineId: widget.machineId,
            runtimeId: _ffmpegRuntimeId,
          );
      if (!mounted) return;
      setState(() {
        _job = job;
        _starting = false;
      });
      _timer = Timer.periodic(
        const Duration(milliseconds: 900),
        (_) => unawaited(_poll()),
      );
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _starting = false;
        _error = _errorText(error);
      });
    }
  }

  Future<void> _poll() async {
    final jobId = _job?.id ?? '';
    if (jobId.isEmpty || _finished) return;
    final repo = ref.read(deviceRepositoryProvider);
    try {
      final job = await repo.getDeviceAgentSemanticPreflightJob(
        machineId: widget.machineId,
        jobId: jobId,
      );
      final events = await repo.getDeviceAgentSemanticPreflightEvents(
        machineId: widget.machineId,
        jobId: jobId,
        afterSequence: _lastSequence,
      );
      if (!mounted) return;
      setState(() {
        _job = job;
        _events.addAll(events);
        if (_events.length > 60) {
          _events.removeRange(0, _events.length - 60);
        }
        for (final event in events) {
          if (event.sequence > _lastSequence) _lastSequence = event.sequence;
        }
        if (job.finished) {
          _finished = true;
          _succeeded = job.completed;
          if (job.failed && job.error.trim().isNotEmpty) {
            _error = job.error;
          }
          if (job.completed) _readyDevices.add(widget.machineId);
          _timer?.cancel();
        }
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = _errorText(error));
      _timer?.cancel();
    }
  }

  String _errorText(Object error) =>
      error.toString().replaceFirst(RegExp(r'^Exception:\s*'), '');

  String _stageText(String step) {
    switch (step) {
      case 'runtime':
        return '安装运行时组件';
      case 'completed':
        return '安装完成';
      case 'failed':
        return '安装失败';
      default:
        return step.isEmpty ? '准备中' : step;
    }
  }

  String _phaseText(String phase) {
    switch (phase) {
      case 'check':
        return '检查';
      case 'download':
        return '下载中';
      case 'extract':
        return '解压中';
      case 'install':
        return '安装中';
      case 'verify':
        return '校验中';
      case 'installed':
        return '已安装';
      case 'skip':
        return '已存在';
      case 'failed':
        return '失败';
      default:
        return phase;
    }
  }

  String _formatBytes(int bytes) {
    if (bytes <= 0) return '';
    if (bytes < 1024) return '${bytes}B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)}KB';
    return '${(bytes / (1024 * 1024)).toStringAsFixed(1)}MB';
  }

  @override
  Widget build(BuildContext context) {
    final job = _job;
    final percent = (job?.progressPercent ?? 0).clamp(0, 100);
    final visibleEvents = _events.length > 11
        ? _events.sublist(_events.length - 11)
        : _events;
    return AlertDialog(
      title: const Text('安装 FFmpeg'),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            LinearProgressIndicator(
              value: _finished ? 1 : percent / 100,
              minHeight: 4,
              color: _succeeded
                  ? AppColors.statusSuccess
                  : (_error.isNotEmpty
                        ? AppColors.statusError
                        : AppColors.primary),
              backgroundColor: AppColors.inputBackground,
            ),
            const SizedBox(height: 8),
            Text(
              _starting
                  ? '正在下发安装任务…'
                  : (_succeeded
                        ? 'FFmpeg 已安装'
                        : (_error.isNotEmpty
                              ? _error
                              : '$percent% · ${_stageText(job?.currentStep ?? '')}')),
              style: TextStyle(
                fontSize: 13,
                height: 1.5,
                color: _error.isNotEmpty
                    ? AppColors.statusError
                    : AppColors.textPrimary,
              ),
            ),
            const SizedBox(height: 10),
            Container(
              constraints: const BoxConstraints(maxHeight: 190),
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
              decoration: BoxDecoration(
                color: AppColors.inputBackground,
                borderRadius: AppRadius.smRadius,
                border: Border.all(color: AppColors.border),
              ),
              child: visibleEvents.isEmpty
                  ? const Text(
                      '等待 launcher 上报进度…',
                      style: TextStyle(
                        fontSize: 12,
                        color: AppColors.textMuted,
                      ),
                    )
                  : ListView.builder(
                      shrinkWrap: true,
                      itemCount: visibleEvents.length,
                      itemBuilder: (context, index) {
                        final event = visibleEvents[index];
                        final bytes = event.totalBytes > 0
                            ? ' ${_formatBytes(event.receivedBytes)}/${_formatBytes(event.totalBytes)}'
                            : '';
                        return Padding(
                          padding: const EdgeInsets.only(bottom: 4),
                          child: Text(
                            '[${_phaseText(event.phase)}] ${event.message}$bytes',
                            style: const TextStyle(
                              fontSize: 12,
                              height: 1.4,
                              color: AppColors.textSecondary,
                            ),
                          ),
                        );
                      },
                    ),
            ),
          ],
        ),
      ),
      actions: [
        if (_finished || _error.isNotEmpty)
          FilledButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('完成'),
          )
        else
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('后台安装'),
          ),
      ],
    );
  }
}
