import 'dart:async';
import 'dart:convert';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/shell_card.dart';
import '../../../shared/widgets/surface_scaffold.dart';
import '../../auth/application/auth_controller.dart';
import '../data/chat_api.dart';
import '../domain/chat_models.dart';

final chatApiProvider = Provider((ref) => const ChatApi());
final chatSessionsProvider = FutureProvider.family<List<SessionInfo>, String>((ref, agentId) async {
  if (agentId.isEmpty) return const [];
  final auth = ref.watch(authControllerProvider);
  return ref.watch(chatApiProvider).listSessions(
        accessToken: auth.accessToken,
        agentId: agentId,
      );
});
final chatTurnsProvider = FutureProvider.family<List<ChatTurn>, String>((ref, sessionId) async {
  if (sessionId.isEmpty) return const [];
  final auth = ref.watch(authControllerProvider);
  return ref.watch(chatApiProvider).listTurns(
        accessToken: auth.accessToken,
        sessionId: sessionId,
      );
});

class ChatPage extends ConsumerStatefulWidget {
  const ChatPage({
    super.key,
    required this.machineId,
    required this.agentId,
    required this.projectId,
  });

  final String machineId;
  final String agentId;
  final String projectId;

  @override
  ConsumerState<ChatPage> createState() => _ChatPageState();
}

class _ChatPageState extends ConsumerState<ChatPage> {
  final inputController = TextEditingController();
  final scrollController = ScrollController();

  String currentSessionId = '';
  String currentModel = 'gpt-5.4-mini';
  bool sending = false;
  String? errorText;
  ChatTurn? approvalTurn;
  List<PickedAttachment> attachments = [];

  @override
  void dispose() {
    inputController.dispose();
    scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final sessionsValue = ref.watch(chatSessionsProvider(widget.agentId));
    final turnsValue = ref.watch(chatTurnsProvider(currentSessionId));
    final mobile = MediaQuery.of(context).size.width < 980;

    ref.listen<AsyncValue<List<SessionInfo>>>(chatSessionsProvider(widget.agentId), (previous, next) {
      next.whenData((sessions) {
        if (currentSessionId.isEmpty && sessions.isNotEmpty) {
          setState(() => currentSessionId = sessions.first.sessionId);
        }
      });
    });

    return SurfaceScaffold(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          children: [
            _ChatHeader(
              machineId: widget.machineId,
              agentId: widget.agentId,
              projectId: widget.projectId,
              currentModel: currentModel,
              onModelChanged: (value) => setState(() => currentModel = value),
              onNewSession: _startNewSession,
            ),
            const SizedBox(height: 20),
            Expanded(
              child: mobile
                  ? Column(
                      children: [
                        SizedBox(
                          height: 220,
                          child: _SessionSidebar(
                            sessionsValue: sessionsValue,
                            currentSessionId: currentSessionId,
                            onSelect: (value) => setState(() => currentSessionId = value),
                          ),
                        ),
                        const SizedBox(height: 16),
                        Expanded(
                          child: _ChatBody(
                            turnsValue: turnsValue,
                            scrollController: scrollController,
                            composer: _buildComposer(),
                          ),
                        ),
                      ],
                    )
                  : Row(
                      children: [
                        SizedBox(
                          width: 320,
                          child: _SessionSidebar(
                            sessionsValue: sessionsValue,
                            currentSessionId: currentSessionId,
                            onSelect: (value) => setState(() => currentSessionId = value),
                          ),
                        ),
                        const SizedBox(width: 20),
                        Expanded(
                          child: _ChatBody(
                            turnsValue: turnsValue,
                            scrollController: scrollController,
                            composer: _buildComposer(),
                          ),
                        ),
                      ],
                    ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildComposer() {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (approvalTurn != null)
          _ApprovalStrip(
            turn: approvalTurn!,
            onApprove: () => _handleApproval('once'),
            onReject: () => _handleApproval('reject'),
          ),
        if (attachments.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(bottom: 12),
            child: Wrap(
              spacing: 10,
              runSpacing: 10,
              children: attachments
                  .map(
                    (file) => Chip(
                      label: Text(file.name),
                      onDeleted: () => setState(() => attachments.remove(file)),
                    ),
                  )
                  .toList(),
            ),
          ),
        ShellCard(
          padding: const EdgeInsets.all(18),
          child: Column(
            children: [
              if (errorText != null)
                Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: Align(
                    alignment: Alignment.centerLeft,
                    child: Text(errorText!, style: const TextStyle(color: Color(0xFFB42318))),
                  ),
                ),
              Row(
                children: [
                  FilledButton.tonal(
                    onPressed: sending ? null : _pickFiles,
                    child: const Text('添加文件'),
                  ),
                  const SizedBox(width: 10),
                  FilledButton.tonal(
                    onPressed: sending ? null : _pickImages,
                    child: const Text('添加图片'),
                  ),
                  const Spacer(),
                  Text('模型：$currentModel'),
                ],
              ),
              const SizedBox(height: 12),
              TextField(
                controller: inputController,
                minLines: 4,
                maxLines: 8,
                decoration: const InputDecoration(
                  hintText: '输入要交给当前 agent 的任务、问题或修改指令',
                ),
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  Expanded(
                    child: Text(
                      currentSessionId.isEmpty ? '新对话将自动创建会话' : '当前会话：$currentSessionId',
                    ),
                  ),
                  FilledButton(
                    onPressed: sending ? null : _send,
                    child: Text(sending ? '发送中...' : '发送'),
                  ),
                ],
              ),
            ],
          ),
        ),
      ],
    );
  }

  void _startNewSession() {
    setState(() {
      currentSessionId = 'sess_${DateTime.now().millisecondsSinceEpoch}';
      approvalTurn = null;
      errorText = null;
    });
    ref.invalidate(chatTurnsProvider(currentSessionId));
  }

  Future<void> _pickFiles() async {
    final result = await FilePicker.platform.pickFiles(withData: true, allowMultiple: true);
    if (result == null) return;
    setState(() {
      attachments.addAll(
        result.files
            .where((file) => file.bytes != null)
            .map((file) => PickedAttachment.fromFile(file, false)),
      );
    });
  }

  Future<void> _pickImages() async {
    final result = await FilePicker.platform.pickFiles(
      withData: true,
      allowMultiple: true,
      type: FileType.image,
    );
    if (result == null) return;
    setState(() {
      attachments.addAll(
        result.files
            .where((file) => file.bytes != null)
            .map((file) => PickedAttachment.fromFile(file, true)),
      );
    });
  }

  Future<void> _send() async {
    final auth = ref.read(authControllerProvider);
    final text = inputController.text.trim();
    if (text.isEmpty && attachments.isEmpty) return;
    final sessionId = currentSessionId.isEmpty ? 'sess_${DateTime.now().millisecondsSinceEpoch}' : currentSessionId;
    setState(() {
      currentSessionId = sessionId;
      sending = true;
      errorText = null;
      approvalTurn = null;
    });

    final parts = <TaskPart>[
      if (text.isNotEmpty) TaskPart(type: 'text', text: text, mime: '', filename: '', url: ''),
      ...attachments.map((file) => file.toPart()),
    ];

    try {
      final api = ref.read(chatApiProvider);
      final created = await api.createTurn(
        accessToken: auth.accessToken,
        agentId: widget.agentId,
        projectId: widget.projectId,
        sessionId: sessionId,
        parts: parts,
        model: currentModel,
      );
      inputController.clear();
      setState(() => attachments = []);
      await _pollTask(created.taskId);
      ref.invalidate(chatSessionsProvider(widget.agentId));
      ref.invalidate(chatTurnsProvider(sessionId));
    } catch (error) {
      setState(() => errorText = '$error');
    } finally {
      setState(() => sending = false);
    }
  }

  Future<void> _pollTask(String taskId) async {
    final auth = ref.read(authControllerProvider);
    final api = ref.read(chatApiProvider);

    for (var i = 0; i < 60; i++) {
      final turn = await api.getTurn(accessToken: auth.accessToken, taskId: taskId);
      if (!mounted) return;
      if (turn.status == 'waiting_approval' && turn.permissionId.isNotEmpty) {
        setState(() => approvalTurn = turn);
        return;
      }
      if (turn.status == 'completed' || turn.status == 'failed' || turn.status == 'cancelled') {
        setState(() => approvalTurn = null);
        return;
      }
      await Future<void>.delayed(const Duration(seconds: 1));
    }
  }

  Future<void> _handleApproval(String reply) async {
    final turn = approvalTurn;
    if (turn == null) return;
    final auth = ref.read(authControllerProvider);
    try {
      await ref.read(chatApiProvider).approve(
            accessToken: auth.accessToken,
            taskId: turn.taskId,
            permissionId: turn.permissionId,
            reply: reply,
          );
      setState(() => approvalTurn = null);
      await _pollTask(turn.taskId);
      ref.invalidate(chatTurnsProvider(currentSessionId));
    } catch (error) {
      setState(() => errorText = '$error');
    }
  }
}

class _ChatHeader extends StatelessWidget {
  const _ChatHeader({
    required this.machineId,
    required this.agentId,
    required this.projectId,
    required this.currentModel,
    required this.onModelChanged,
    required this.onNewSession,
  });

  final String machineId;
  final String agentId;
  final String projectId;
  final String currentModel;
  final ValueChanged<String> onModelChanged;
  final VoidCallback onNewSession;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('对话', style: Theme.of(context).textTheme.headlineMedium),
              const SizedBox(height: 6),
              Text('设备 $machineId · 执行器 $agentId · 项目 $projectId'),
            ],
          ),
        ),
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
          decoration: BoxDecoration(
            color: Colors.white.withValues(alpha: 0.8),
            borderRadius: BorderRadius.circular(999),
            border: Border.all(color: const Color(0xFFD4E5FF)),
          ),
          child: DropdownButtonHideUnderline(
            child: DropdownButton<String>(
              value: currentModel,
              items: const [
                DropdownMenuItem(value: 'gpt-5.4-mini', child: Text('gpt-5.4-mini')),
                DropdownMenuItem(value: 'gpt-5.4', child: Text('gpt-5.4')),
                DropdownMenuItem(value: 'claude-sonnet', child: Text('claude-sonnet')),
              ],
              onChanged: (value) {
                if (value != null) onModelChanged(value);
              },
            ),
          ),
        ),
        const SizedBox(width: 12),
        FilledButton.tonal(
          onPressed: onNewSession,
          child: const Text('新建对话'),
        ),
      ],
    );
  }
}

class _SessionSidebar extends StatelessWidget {
  const _SessionSidebar({
    required this.sessionsValue,
    required this.currentSessionId,
    required this.onSelect,
  });

  final AsyncValue<List<SessionInfo>> sessionsValue;
  final String currentSessionId;
  final ValueChanged<String> onSelect;

  @override
  Widget build(BuildContext context) {
    return ShellCard(
      child: sessionsValue.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (error, _) => Center(child: Text('$error')),
        data: (sessions) {
          if (sessions.isEmpty) {
            return const Center(child: Text('还没有历史对话'));
          }
          return ListView.separated(
            itemCount: sessions.length,
            separatorBuilder: (_, _) => const SizedBox(height: 10),
            itemBuilder: (context, index) {
              final session = sessions[index];
              final selected = session.sessionId == currentSessionId;
              return InkWell(
                borderRadius: BorderRadius.circular(20),
                onTap: () => onSelect(session.sessionId),
                child: Ink(
                  padding: const EdgeInsets.all(14),
                  decoration: BoxDecoration(
                    color: selected ? const Color(0xFFDFF0FF) : const Color(0xFFF8FBFF),
                    borderRadius: BorderRadius.circular(20),
                    border: Border.all(color: const Color(0xFFD4E5FF)),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        session.summary.isEmpty ? session.sessionId : session.summary,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                      ),
                      const SizedBox(height: 6),
                      Text(
                        session.updatedAt == null ? '-' : _formatDate(session.updatedAt!),
                        style: Theme.of(context).textTheme.bodyMedium,
                      ),
                    ],
                  ),
                ),
              );
            },
          );
        },
      ),
    );
  }
}

class _ChatBody extends StatelessWidget {
  const _ChatBody({
    required this.turnsValue,
    required this.scrollController,
    required this.composer,
  });

  final AsyncValue<List<ChatTurn>> turnsValue;
  final ScrollController scrollController;
  final Widget composer;

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        Expanded(
          child: ShellCard(
            child: turnsValue.when(
              loading: () => const Center(child: CircularProgressIndicator()),
              error: (error, _) => Center(child: Text('$error')),
              data: (turns) {
                if (turns.isEmpty) {
                  return const Center(child: Text('从这里开始一段新对话'));
                }
                return ListView.separated(
                  controller: scrollController,
                  itemCount: turns.length,
                  separatorBuilder: (_, _) => const SizedBox(height: 16),
                  itemBuilder: (context, index) => _TurnView(turn: turns[index]),
                );
              },
            ),
          ),
        ),
        const SizedBox(height: 16),
        composer,
      ],
    );
  }
}

class _TurnView extends StatelessWidget {
  const _TurnView({required this.turn});

  final ChatTurn turn;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Align(
          alignment: Alignment.centerRight,
          child: Container(
            constraints: const BoxConstraints(maxWidth: 720),
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              color: const Color(0xFFDCEBFF),
              borderRadius: BorderRadius.circular(22),
            ),
            child: Text(turn.prompt.isEmpty ? '附件输入' : turn.prompt),
          ),
        ),
        const SizedBox(height: 10),
        Align(
          alignment: Alignment.centerLeft,
          child: Container(
            constraints: const BoxConstraints(maxWidth: 760),
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              color: const Color(0xFFFCFEFF),
              border: Border.all(color: const Color(0xFFD4E5FF)),
              borderRadius: BorderRadius.circular(22),
            ),
            child: Text(
              turn.error.isNotEmpty ? turn.error : (turn.result.isEmpty ? '处理中...' : turn.result),
            ),
          ),
        ),
      ],
    );
  }
}

class _ApprovalStrip extends StatelessWidget {
  const _ApprovalStrip({
    required this.turn,
    required this.onApprove,
    required this.onReject,
  });

  final ChatTurn turn;
  final VoidCallback onApprove;
  final VoidCallback onReject;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: ShellCard(
        padding: const EdgeInsets.all(16),
        child: Row(
          children: [
            Expanded(
              child: Text(
                '审批请求：${turn.permission.isEmpty ? '待确认操作' : turn.permission}',
              ),
            ),
            FilledButton.tonal(onPressed: onReject, child: const Text('拒绝')),
            const SizedBox(width: 10),
            FilledButton(onPressed: onApprove, child: const Text('批准')),
          ],
        ),
      ),
    );
  }
}

class PickedAttachment {
  PickedAttachment({
    required this.name,
    required this.mime,
    required this.base64Body,
  });

  final String name;
  final String mime;
  final String base64Body;

  factory PickedAttachment.fromFile(PlatformFile file, bool image) {
    final bytes = file.bytes ?? const <int>[];
    final mime = image ? 'image/${_ext(file.extension)}' : 'application/octet-stream';
    return PickedAttachment(
      name: file.name,
      mime: mime,
      base64Body: base64Encode(bytes),
    );
  }

  TaskPart toPart() {
    return TaskPart(
      type: 'file',
      text: '',
      mime: mime,
      filename: name,
      url: 'data:$mime;base64,$base64Body',
    );
  }

  static String _ext(String? ext) {
    if (ext == null || ext.isEmpty) return 'png';
    return ext.toLowerCase();
  }
}

String _formatDate(DateTime value) {
  final month = value.month.toString().padLeft(2, '0');
  final day = value.day.toString().padLeft(2, '0');
  final hour = value.hour.toString().padLeft(2, '0');
  final minute = value.minute.toString().padLeft(2, '0');
  return '$month-$day $hour:$minute';
}
