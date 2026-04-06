import 'dart:convert';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import '../../../features/settings/settings_provider.dart';
import '../data/chat_model.dart';
import 'chat_provider.dart';
import 'message_renderer.dart';

class ChatPage extends ConsumerStatefulWidget {
  final String agentId;
  final String projectId;
  final String machineId;

  const ChatPage({
    super.key,
    required this.agentId,
    required this.projectId,
    required this.machineId,
  });

  @override
  ConsumerState<ChatPage> createState() => _ChatPageState();
}

class _ChatPageState extends ConsumerState<ChatPage> {
  final _inputCtrl = TextEditingController();
  final _scrollCtrl = ScrollController();
  final _inputFocus = FocusNode();
  bool _sessionPanelOpen = false;

  (String, String) get _chatKey => (widget.agentId, widget.projectId);

  @override
  void initState() {
    super.initState();
    // 进入页面时自动加载最近一条会话
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _autoLoadLatestSession();
    });
  }

  Future<void> _autoLoadLatestSession() async {
    final notifier = ref.read(chatProvider(_chatKey).notifier);
    // 如果已有消息（比如 provider 还没 dispose），不重复加载
    if (ref.read(chatProvider(_chatKey)).messages.isNotEmpty) return;
    try {
      final sessions = await ref.read(chatRepositoryProvider).getSessions(agentId: widget.agentId);
      if (sessions.isNotEmpty && mounted) {
        notifier.loadSession(sessions.first.sessionId);
      }
    } catch (_) {}
  }

  @override
  void dispose() {
    _inputCtrl.dispose();
    _scrollCtrl.dispose();
    _inputFocus.dispose();
    super.dispose();
  }

  void _scrollToBottom() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scrollCtrl.hasClients) {
        _scrollCtrl.animateTo(
          _scrollCtrl.position.maxScrollExtent,
          duration: const Duration(milliseconds: 250),
          curve: Curves.easeOut,
        );
      }
    });
  }

  Future<void> _send() async {
    final text = _inputCtrl.text.trim();
    if (text.isEmpty) return;
    _inputCtrl.clear();
    final model = ref.read(selectedModelProvider);
    final pMode = ref.read(permissionModeProvider);
    await ref.read(chatProvider(_chatKey).notifier).sendMessage(text, model: model, permissionMode: pMode);
    _scrollToBottom();
  }

  Future<void> _pickFiles() async {
    final result = await FilePicker.platform.pickFiles(
      allowMultiple: true,
      withData: true,
      type: FileType.custom,
      allowedExtensions: [
        'jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp',
        'pdf',
        'txt', 'md', 'dart', 'ts', 'js', 'py', 'go', 'java',
        'swift', 'kt', 'rs', 'c', 'cpp', 'h', 'cs', 'json',
        'yaml', 'yml', 'xml', 'html', 'css', 'sh', 'bash',
      ],
    );
    if (result == null) return;
    final notifier = ref.read(chatProvider(_chatKey).notifier);
    for (final file in result.files) {
      if (file.bytes == null) continue;
      final mime = _mimeFromExtension(file.extension ?? '');
      final base64Data = base64Encode(file.bytes!);
      notifier.addAttachedFile(AttachedFile(
        filename: file.name,
        mimeType: mime,
        base64Data: base64Data,
        sizeBytes: file.size,
      ));
    }
  }

  String _mimeFromExtension(String ext) {
    return switch (ext.toLowerCase()) {
      'jpg' || 'jpeg' => 'image/jpeg',
      'png' => 'image/png',
      'gif' => 'image/gif',
      'webp' => 'image/webp',
      'bmp' => 'image/bmp',
      'pdf' => 'application/pdf',
      'html' => 'text/html',
      'css' => 'text/css',
      'json' => 'application/json',
      'xml' => 'application/xml',
      _ => 'text/plain',
    };
  }

  @override
  Widget build(BuildContext context) {
    final chat = ref.watch(chatProvider(_chatKey));
    final selectedModel = ref.watch(selectedModelProvider);
    final availableModels = ref.watch(availableModelsProvider).valueOrNull ?? const [];
    final isMobile = AppBreakpoints.isMobile(context);

    // 模型列表加载完后恢复持久化的选择
    ref.listen(availableModelsProvider, (_, next) {
      final models = next.valueOrNull;
      if (models != null && models.isNotEmpty) {
        ref.read(selectedModelProvider.notifier).init(models);
      }
    });

    // 有新消息时滚动到底部
    ref.listen(chatProvider(_chatKey), (_, next) {
      if (next.messages.isNotEmpty) _scrollToBottom();
    });

    return Scaffold(
      body: SafeArea(
        child: PageBackground(
        child: Column(
          children: [
            _buildContextBar(context, chat),
            Expanded(
              child: Row(
                children: [
                  // 左侧历史会话栏（桌面常驻，移动抽屉）
                  if (!isMobile && _sessionPanelOpen)
                    _SessionSidebar(
                      agentId: widget.agentId,
                      currentSessionId: chat.currentSessionId,
                      onSelect: (sid) {
                        ref.read(chatProvider(_chatKey).notifier).loadSession(sid);
                      },
                      onNew: () {
                        ref.read(chatProvider(_chatKey).notifier).newSession();
                      },
                    ),
                  if (!isMobile && _sessionPanelOpen)
                    Container(width: 1, color: AppColors.border),
                  // 中央消息区 + 输入区
                  Expanded(
                    child: Column(
                      children: [
                        Expanded(
                          child: _MessageArea(
                            messages: chat.messages,
                            scrollController: _scrollCtrl,
                          ),
                        ),
                        if (chat.pendingApproval != null)
                          _ApprovalBanner(
                            approval: chat.pendingApproval!,
                            onApprove: (reply) {
                              ref.read(chatProvider(_chatKey).notifier).submitApproval(reply);
                            },
                          ),
                        _InputArea(
                          controller: _inputCtrl,
                          focusNode: _inputFocus,
                          sending: chat.sending || chat.messages.any((m) => m.state == MessageState.streaming),
                          sessionId: chat.currentSessionId,
                          attachedFiles: chat.attachedFiles,
                          selectedModel: selectedModel,
                          availableModels: availableModels,
                          onSend: _send,
                          onStop: () => ref.read(chatProvider(_chatKey).notifier).cancelCurrentTask(),
                          onPickFiles: _pickFiles,
                          onRemoveFile: (i) {
                            ref.read(chatProvider(_chatKey).notifier).removeAttachedFile(i);
                          },
                          onSelectModel: (m) {
                            ref.read(selectedModelProvider.notifier).select(m);
                          },
                          onAddFile: (f) {
                            ref.read(chatProvider(_chatKey).notifier).addAttachedFile(f);
                          },
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
      ),
      // 移动端抽屉
      drawer: isMobile
          ? Drawer(
              child: _SessionSidebar(
                agentId: widget.agentId,
                currentSessionId: chat.currentSessionId,
                onSelect: (sid) {
                  ref.read(chatProvider(_chatKey).notifier).loadSession(sid);
                  Navigator.of(context).pop();
                },
                onNew: () {
                  ref.read(chatProvider(_chatKey).notifier).newSession();
                  Navigator.of(context).pop();
                },
              ),
            )
          : null,
    );
  }

  Widget _buildContextBar(BuildContext context, ChatState chat) {
    final isMobile = AppBreakpoints.isMobile(context);
    return Builder(builder: (scaffoldContext) => Container(
      height: 52,
      decoration: const BoxDecoration(
        color: AppColors.surface,
        border: Border(bottom: BorderSide(color: AppColors.border)),
      ),
      padding: const EdgeInsets.symmetric(horizontal: 12),
      child: Row(
        children: [
          // 返回按钮
          IconButton(
            icon: const Icon(Icons.arrow_back_ios_new, size: 15),
            onPressed: () => context.go('/devices/${widget.machineId}'),
            tooltip: '返回设备详情',
          ),
          // 历史会话切换
          IconButton(
            icon: Icon(
              Icons.history,
              size: 18,
              color: _sessionPanelOpen ? AppColors.primary : AppColors.textSecondary,
            ),
            onPressed: isMobile
                ? () => Scaffold.of(scaffoldContext).openDrawer()
                : () => setState(() => _sessionPanelOpen = !_sessionPanelOpen),
            tooltip: '历史会话',
          ),
          Container(width: 1, height: 20, color: AppColors.border),
          const SizedBox(width: 10),
          // 设备 + agent 信息
          Expanded(
            child: SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  _ContextChip(
                    icon: Icons.computer_rounded,
                    label: widget.machineId,
                  ),
                  const SizedBox(width: 6),
                  _ContextChip(
                    icon: Icons.smart_toy_outlined,
                    label: widget.agentId,
                  ),
                  const SizedBox(width: 6),
                  _ContextChip(
                    icon: Icons.folder_outlined,
                    label: widget.projectId,
                  ),
                  if (chat.currentSessionId != null) ...[
                    const SizedBox(width: 6),
                    _ContextChip(
                      icon: Icons.chat_bubble_outline,
                      label: chat.currentSessionId!.length > 16
                          ? '...${chat.currentSessionId!.substring(chat.currentSessionId!.length - 12)}'
                          : chat.currentSessionId!,
                      highlight: true,
                    ),
                  ],
                ],
              ),
            ),
          ),
          const SizedBox(width: 8),
          // 新建对话
          TextButton.icon(
            onPressed: () {
              ref.read(chatProvider(_chatKey).notifier).newSession();
            },
            icon: const Icon(Icons.add, size: 15),
            label: const Text('新建对话', style: TextStyle(fontSize: 13)),
            style: TextButton.styleFrom(
              foregroundColor: AppColors.primary,
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
            ),
          ),
        ],
      ),
    ));
  }
}

class _ContextChip extends StatelessWidget {
  final IconData icon;
  final String label;
  final bool highlight;
  const _ContextChip({
    required this.icon,
    required this.label,
    this.highlight = false,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: highlight ? AppColors.primaryLight : AppColors.inputBackground,
        borderRadius: AppRadius.smRadius,
        border: Border.all(
          color: highlight ? AppColors.primaryMuted : AppColors.border,
        ),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon,
              size: 12,
              color: highlight ? AppColors.primary : AppColors.textMuted),
          const SizedBox(width: 4),
          Text(
            label,
            style: TextStyle(
              fontSize: 12,
              color: highlight ? AppColors.primary : AppColors.textSecondary,
              fontWeight: highlight ? FontWeight.w500 : FontWeight.w400,
            ),
          ),
        ],
      ),
    );
  }
}

// 消息区
class _MessageArea extends StatelessWidget {
  final List<ChatMessage> messages;
  final ScrollController scrollController;

  const _MessageArea({
    required this.messages,
    required this.scrollController,
  });

  @override
  Widget build(BuildContext context) {
    if (messages.isEmpty) {
      return const Center(
        child: EmptyState(
          message: '发送消息开始对话\n或从左侧历史记录切换会话',
          icon: Icons.chat_bubble_outline_rounded,
        ),
      );
    }

    return ListView.builder(
      controller: scrollController,
      padding: EdgeInsets.symmetric(
        horizontal: AppBreakpoints.isMobile(context) ? 12 : 24,
        vertical: 16,
      ),
      itemCount: messages.length,
      itemBuilder: (context, i) {
        final msg = messages[i];
        return _MessageBubble(message: msg, key: ValueKey(msg.id));
      },
    );
  }
}

class _MessageBubble extends StatelessWidget {
  final ChatMessage message;
  const _MessageBubble({required this.message, super.key});

  @override
  Widget build(BuildContext context) {
    final isUser = message.role == MessageRole.user;

    return Padding(
      padding: const EdgeInsets.only(bottom: 16),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisAlignment:
            isUser ? MainAxisAlignment.end : MainAxisAlignment.start,
        children: [
          if (!isUser) ...[
            _Avatar(isUser: false),
            const SizedBox(width: 10),
          ],
          Flexible(
            child: Column(
              crossAxisAlignment:
                  isUser ? CrossAxisAlignment.end : CrossAxisAlignment.start,
              children: [
                // 思考过程折叠区（仅 agent 消息）
                if (!isUser && message.thinkingContent.isNotEmpty)
                  _ThinkingBlock(
                    thinking: message.thinkingContent,
                    isStreaming: message.state == MessageState.streaming,
                  ),
                if (!isUser && message.thinkingContent.isNotEmpty)
                  const SizedBox(height: 6),
                // 正文气泡
                if (message.content.isNotEmpty || isUser || message.state == MessageState.failed)
                  _BubbleContent(message: message, isUser: isUser),
                // streaming 时且还没正文，显示跳动点
                if (!isUser && message.state == MessageState.streaming && message.content.isEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: _TypingIndicator(),
                  ),
                // 用户消息附件预览
                if (isUser && message.attachedFiles.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  _AttachmentPreview(files: message.attachedFiles),
                ],
              ],
            ),
          ),
          if (isUser) ...[
            const SizedBox(width: 10),
            _Avatar(isUser: true),
          ],
        ],
      ),
    );
  }
}

// 附件预览（气泡下方）
class _AttachmentPreview extends StatelessWidget {
  final List<AttachedFile> files;
  const _AttachmentPreview({required this.files});

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 6,
      runSpacing: 6,
      alignment: WrapAlignment.end,
      children: files.map((f) => _AttachmentChip(file: f)).toList(),
    );
  }
}

class _AttachmentChip extends StatelessWidget {
  final AttachedFile file;
  const _AttachmentChip({required this.file});

  @override
  Widget build(BuildContext context) {
    final bytes = file.imageBytes;
    if (file.isImage && bytes != null) {
      return ClipRRect(
        borderRadius: AppRadius.smRadius,
        child: Image.memory(
          bytes,
          width: 100,
          height: 100,
          fit: BoxFit.cover,
          errorBuilder: (_, __, ___) => _FileChip(file: file),
        ),
      );
    }
    return _FileChip(file: file);
  }
}

class _FileChip extends StatelessWidget {
  final AttachedFile file;
  const _FileChip({required this.file});

  @override
  Widget build(BuildContext context) {
    final icon = file.isPdf
        ? Icons.picture_as_pdf_outlined
        : Icons.insert_drive_file_outlined;
    return Container(
      constraints: const BoxConstraints(maxWidth: 160),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      decoration: BoxDecoration(
        color: Colors.white.withValues(alpha: 0.15),
        borderRadius: AppRadius.smRadius,
        border: Border.all(color: Colors.white.withValues(alpha: 0.3)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 14, color: Colors.white70),
          const SizedBox(width: 5),
          Flexible(
            child: Text(
              file.filename,
              style: const TextStyle(fontSize: 11, color: Colors.white, overflow: TextOverflow.ellipsis),
              maxLines: 1,
            ),
          ),
        ],
      ),
    );
  }
}

// 思考过程折叠块
class _ThinkingBlock extends StatefulWidget {
  final String thinking;
  final bool isStreaming;
  const _ThinkingBlock({required this.thinking, required this.isStreaming});

  @override
  State<_ThinkingBlock> createState() => _ThinkingBlockState();
}

class _ThinkingBlockState extends State<_ThinkingBlock> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(maxWidth: 520),
      decoration: BoxDecoration(
        color: AppColors.surfaceElevated,
        borderRadius: AppRadius.mdRadius,
        border: Border.all(color: AppColors.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // 标题栏
          InkWell(
            onTap: () => setState(() => _expanded = !_expanded),
            borderRadius: AppRadius.mdRadius,
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (widget.isStreaming)
                    Padding(
                      padding: const EdgeInsets.only(right: 6),
                      child: SizedBox(
                        width: 10,
                        height: 10,
                        child: CircularProgressIndicator(
                          strokeWidth: 1.5,
                          color: AppColors.primary,
                        ),
                      ),
                    )
                  else
                    const Icon(Icons.lightbulb_outline,
                        size: 13, color: AppColors.textMuted),
                  const SizedBox(width: 5),
                  Text(
                    widget.isStreaming ? '思考中...' : '已完成思考',
                    style: const TextStyle(
                      fontSize: 12,
                      color: AppColors.textMuted,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                  const SizedBox(width: 6),
                  Icon(
                    _expanded ? Icons.expand_less : Icons.expand_more,
                    size: 14,
                    color: AppColors.textMuted,
                  ),
                ],
              ),
            ),
          ),
          // 展开内容
          if (_expanded)
            Container(
              width: double.infinity,
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 10),
              decoration: BoxDecoration(
                border: const Border(top: BorderSide(color: AppColors.border)),
              ),
              child: SelectableText(
                widget.thinking,
                style: const TextStyle(
                  fontSize: 12,
                  height: 1.6,
                  color: AppColors.textSecondary,
                  fontFamily: 'monospace',
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _Avatar extends StatelessWidget {
  final bool isUser;
  const _Avatar({required this.isUser});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 32,
      height: 32,
      decoration: BoxDecoration(
        color: isUser ? AppColors.primary : AppColors.primaryLight,
        borderRadius: AppRadius.smRadius,
      ),
      child: Icon(
        isUser ? Icons.person_outline : Icons.smart_toy_outlined,
        size: 16,
        color: isUser ? Colors.white : AppColors.primary,
      ),
    );
  }
}

class _BubbleContent extends ConsumerWidget {
  final ChatMessage message;
  final bool isUser;
  const _BubbleContent({required this.message, required this.isUser});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final settings = ref.watch(settingsProvider);
    final isFailed = message.state == MessageState.failed;

    return Container(
      constraints: BoxConstraints(
        maxWidth: MediaQuery.of(context).size.width * 0.72,
      ),
      decoration: BoxDecoration(
        color: isUser
            ? AppColors.primary
            : isFailed
                ? AppColors.statusErrorLight
                : AppColors.surface,
        borderRadius: BorderRadius.only(
          topLeft: const Radius.circular(12),
          topRight: const Radius.circular(12),
          bottomLeft: Radius.circular(isUser ? 12 : 3),
          bottomRight: Radius.circular(isUser ? 3 : 12),
        ),
        border: isUser
            ? null
            : Border.all(
                color: isFailed
                    ? AppColors.statusError.withValues(alpha: 0.3)
                    : AppColors.border,
              ),
        boxShadow: isUser
            ? null
            : const [
                BoxShadow(
                  color: Color(0x061A3A6A),
                  blurRadius: 8,
                  offset: Offset(0, 2),
                ),
              ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // 正文内容
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
            child: message.content.isEmpty && !isFailed
                ? const SizedBox(height: 6)
                : isFailed
                    ? SelectableText(
                        message.content.isEmpty ? '回复失败' : message.content,
                        style: const TextStyle(fontSize: 14, height: 1.55, color: AppColors.statusError),
                      )
                    : MessageRenderer(
                        content: message.content,
                        isUser: isUser,
                        settings: settings,
                      ),
          ),
          // 统计信息 + 复制按钮（仅 agent 已完成消息）
          if (!isUser && message.state == MessageState.done && message.content.isNotEmpty)
            _buildStats(settings),
        ],
      ),
    );
  }

  Widget _buildStats(AppSettings settings) {
    final parts = <String>[];
    if (settings.showFirstTokenTime && message.firstTokenTime != null) {
      final ms = message.firstTokenTime!.inMilliseconds;
      parts.add('首字 ${ms < 1000 ? "${ms}ms" : "${(ms / 1000).toStringAsFixed(1)}s"}');
    }
    if (settings.showTokenCount && message.tokenCount != null) {
      parts.add('${message.tokenCount} tokens');
    }
    if (settings.showWordCount && message.content.isNotEmpty) {
      parts.add('${message.content.length} 字');
    }
    return Container(
      padding: const EdgeInsets.fromLTRB(14, 0, 14, 8),
      child: Row(
        children: [
          if (parts.isNotEmpty)
            Expanded(
              child: Wrap(
                spacing: 10,
                children: parts
                    .map((p) => Text(p,
                        style: const TextStyle(fontSize: 11, color: AppColors.textMuted)))
                    .toList(),
              ),
            )
          else
            const Spacer(),
          _CopyButton(content: message.content),
        ],
      ),
    );
  }
}

class _TypingIndicator extends StatefulWidget {
  @override
  State<_TypingIndicator> createState() => _TypingIndicatorState();
}

class _TypingIndicatorState extends State<_TypingIndicator>
    with SingleTickerProviderStateMixin {
  late AnimationController _ctrl;

  @override
  void initState() {
    super.initState();
    _ctrl = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 900),
    )..repeat();
  }

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: _ctrl,
      builder: (context, _) {
        return Row(
          mainAxisSize: MainAxisSize.min,
          children: List.generate(3, (i) {
            final delay = i * 0.3;
            final t = (_ctrl.value - delay).clamp(0.0, 1.0);
            final opacity = (t < 0.5 ? t * 2 : (1 - t) * 2).clamp(0.3, 1.0);
            return Container(
              width: 6,
              height: 6,
              margin: const EdgeInsets.only(right: 3),
              decoration: BoxDecoration(
                color: AppColors.primary.withOpacity(opacity),
                shape: BoxShape.circle,
              ),
            );
          }),
        );
      },
    );
  }
}

// 审批条
class _ApprovalBanner extends StatefulWidget {
  final ApprovalInfo approval;
  final void Function(String reply) onApprove;

  const _ApprovalBanner({required this.approval, required this.onApprove});

  @override
  State<_ApprovalBanner> createState() => _ApprovalBannerState();
}

class _ApprovalBannerState extends State<_ApprovalBanner>
    with SingleTickerProviderStateMixin {
  late AnimationController _ctrl;
  late Animation<Offset> _slide;

  @override
  void initState() {
    super.initState();
    _ctrl = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 280),
    )..forward();
    _slide = Tween<Offset>(
      begin: const Offset(0, 0.3),
      end: Offset.zero,
    ).animate(CurvedAnimation(parent: _ctrl, curve: Curves.easeOut));
  }

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final approval = widget.approval;
    return SlideTransition(
      position: _slide,
      child: FadeTransition(
        opacity: _ctrl,
        child: Container(
          margin: const EdgeInsets.fromLTRB(12, 0, 12, 8),
          padding: const EdgeInsets.all(14),
          decoration: BoxDecoration(
            color: AppColors.statusWarningLight,
            borderRadius: AppRadius.mdRadius,
            border: Border.all(
              color: AppColors.statusWarning.withOpacity(0.4),
            ),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  const Icon(Icons.lock_outline,
                      size: 16, color: AppColors.statusWarning),
                  const SizedBox(width: 6),
                  const Text(
                    '需要审批',
                    style: TextStyle(
                      fontWeight: FontWeight.w600,
                      fontSize: 13,
                      color: AppColors.statusWarning,
                    ),
                  ),
                  const Spacer(),
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                    decoration: BoxDecoration(
                      color: AppColors.statusWarning.withOpacity(0.12),
                      borderRadius: AppRadius.smRadius,
                    ),
                    child: Text(
                      approval.permission,
                      style: const TextStyle(
                        fontSize: 11,
                        color: AppColors.statusWarning,
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                  ),
                ],
              ),
              if (approval.patterns.isNotEmpty) ...[
                const SizedBox(height: 8),
                Wrap(
                  spacing: 6,
                  runSpacing: 4,
                  children: approval.patterns.map((p) {
                    return Container(
                      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
                      decoration: BoxDecoration(
                        color: Colors.white,
                        borderRadius: AppRadius.smRadius,
                        border: Border.all(color: AppColors.border),
                      ),
                      child: Text(
                        p,
                        style: const TextStyle(
                          fontSize: 11,
                          fontFamily: 'monospace',
                          color: AppColors.textSecondary,
                        ),
                      ),
                    );
                  }).toList(),
                ),
              ],
              const SizedBox(height: 12),
              Row(
                children: [
                  AppButton(
                    label: '批准一次',
                    onPressed: () => widget.onApprove('once'),
                  ),
                  const SizedBox(width: 8),
                  AppButton(
                    label: '全部批准',
                    outlined: true,
                    onPressed: () => widget.onApprove('always'),
                  ),
                  const SizedBox(width: 8),
                  AppButton(
                    label: '拒绝',
                    outlined: true,
                    onPressed: () => widget.onApprove('reject'),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

// 输入区
class _InputArea extends ConsumerStatefulWidget {
  final TextEditingController controller;
  final FocusNode focusNode;
  final bool sending;
  final String? sessionId;
  final List<AttachedFile> attachedFiles;
  final ModelInfo? selectedModel;
  final List<ModelInfo> availableModels;
  final VoidCallback onSend;
  final VoidCallback onStop;
  final VoidCallback onPickFiles;
  final void Function(int index) onRemoveFile;
  final void Function(ModelInfo? model) onSelectModel;
  final void Function(AttachedFile file) onAddFile;

  const _InputArea({
    required this.controller,
    required this.focusNode,
    required this.sending,
    required this.sessionId,
    required this.attachedFiles,
    required this.selectedModel,
    required this.availableModels,
    required this.onSend,
    required this.onStop,
    required this.onPickFiles,
    required this.onRemoveFile,
    required this.onSelectModel,
    required this.onAddFile,
  });

  @override
  ConsumerState<_InputArea> createState() => _InputAreaState();
}

class _InputAreaState extends ConsumerState<_InputArea> {
  @override
  void initState() {
    super.initState();
    widget.focusNode.onKeyEvent = _handleKeyEvent;
  }

  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    if (event is KeyDownEvent &&
        event.logicalKey == LogicalKeyboardKey.enter &&
        HardwareKeyboard.instance.isControlPressed) {
      widget.onSend();
      return KeyEventResult.handled;
    }
    // 拦截 Ctrl+V / Meta+V 粘贴，处理长文本转文件
    if (event is KeyDownEvent &&
        event.logicalKey == LogicalKeyboardKey.keyV &&
        (HardwareKeyboard.instance.isControlPressed ||
            HardwareKeyboard.instance.isMetaPressed)) {
      final settings = ref.read(settingsProvider);
      if (settings.pasteAsFile) {
        _handlePaste();
        return KeyEventResult.handled;
      }
    }
    return KeyEventResult.ignored;
  }

  Future<void> _handlePaste() async {
    final data = await Clipboard.getData(Clipboard.kTextPlain);
    final text = data?.text ?? '';
    if (text.length > AppSettings.pasteAsFileThreshold) {
      // 长文本：转为 .txt 附件
      final bytes = const Utf8Encoder().convert(text);
      final base64Data = base64Encode(bytes);
      final filename = 'pasted_${DateTime.now().millisecondsSinceEpoch}.txt';
      widget.onAddFile(AttachedFile(
        filename: filename,
        mimeType: 'text/plain',
        base64Data: base64Data,
        sizeBytes: bytes.length,
      ));
    } else if (text.isNotEmpty) {
      // 短文本：正常插入
      final ctrl = widget.controller;
      final sel = ctrl.selection;
      final newText = ctrl.text.replaceRange(
        sel.start < 0 ? ctrl.text.length : sel.start,
        sel.end < 0 ? ctrl.text.length : sel.end,
        text,
      );
      ctrl.value = TextEditingValue(
        text: newText,
        selection: TextSelection.collapsed(
          offset: (sel.start < 0 ? ctrl.text.length : sel.start) + text.length,
        ),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: const BoxDecoration(
        color: AppColors.surface,
        border: Border(top: BorderSide(color: AppColors.border)),
      ),
      padding: const EdgeInsets.fromLTRB(12, 10, 12, 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // 文件预览列表
          if (widget.attachedFiles.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: SizedBox(
                height: 68,
                child: ListView.builder(
                  scrollDirection: Axis.horizontal,
                  itemCount: widget.attachedFiles.length,
                  itemBuilder: (context, i) {
                    return _FilePreviewChip(
                      file: widget.attachedFiles[i],
                      onRemove: () => widget.onRemoveFile(i),
                    );
                  },
                ),
              ),
            ),
          // 工具栏：附件 + 模型选择
          Padding(
            padding: const EdgeInsets.only(bottom: 6),
            child: Row(
              children: [
                // 附件按钮
                InkWell(
                  onTap: widget.onPickFiles,
                  borderRadius: AppRadius.smRadius,
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
                    decoration: BoxDecoration(
                      color: AppColors.inputBackground,
                      borderRadius: AppRadius.smRadius,
                      border: Border.all(color: AppColors.border),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const Icon(Icons.attach_file, size: 14, color: AppColors.textSecondary),
                        const SizedBox(width: 4),
                        Text(
                          widget.attachedFiles.isEmpty ? '附件' : '添加更多',
                          style: const TextStyle(
                            fontSize: 12,
                            color: AppColors.textSecondary,
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                // 模型选择器
                _ModelSelector(
                  selectedModel: widget.selectedModel,
                  availableModels: widget.availableModels,
                  onSelect: widget.onSelectModel,
                ),
                const SizedBox(width: 8),
                // 审批模式
                _PermissionModeButton(
                  mode: ref.watch(permissionModeProvider),
                  onChanged: (m) => ref.read(permissionModeProvider.notifier).set(m),
                ),
              ],
            ),
          ),
          // 输入行
          Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              Expanded(
                child: TextField(
                  controller: widget.controller,
                  focusNode: widget.focusNode,
                  maxLines: 6,
                  minLines: 1,
                  textInputAction: TextInputAction.newline,
                  onSubmitted: null,
                  decoration: InputDecoration(
                    hintText: '输入消息，Ctrl+Enter 发送',
                    border: OutlineInputBorder(
                      borderRadius: AppRadius.mdRadius,
                      borderSide: const BorderSide(color: AppColors.border),
                    ),
                    enabledBorder: OutlineInputBorder(
                      borderRadius: AppRadius.mdRadius,
                      borderSide: const BorderSide(color: AppColors.border),
                    ),
                    focusedBorder: OutlineInputBorder(
                      borderRadius: AppRadius.mdRadius,
                      borderSide: const BorderSide(
                          color: AppColors.primary, width: 1.5),
                    ),
                    contentPadding: const EdgeInsets.symmetric(
                        horizontal: 14, vertical: 10),
                    filled: true,
                    fillColor: AppColors.inputBackground,
                  ),
                ),
              ),
              const SizedBox(width: 8),
              _SendButton(sending: widget.sending, onSend: widget.onSend, onStop: widget.onStop),
            ],
          ),
          if (widget.sessionId != null)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Row(
                children: [
                  const Icon(Icons.link, size: 11, color: AppColors.textMuted),
                  const SizedBox(width: 4),
                  Text(
                    '当前会话 ${widget.sessionId}',
                    style: const TextStyle(
                      fontSize: 11,
                      color: AppColors.textMuted,
                    ),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

// 文件预览缩略片
class _FilePreviewChip extends StatelessWidget {
  final AttachedFile file;
  final VoidCallback onRemove;
  const _FilePreviewChip({required this.file, required this.onRemove});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 64,
      margin: const EdgeInsets.only(right: 8),
      child: Stack(
        clipBehavior: Clip.none,
        children: [
          Container(
            width: 64,
            height: 64,
            decoration: BoxDecoration(
              color: AppColors.inputBackground,
              borderRadius: AppRadius.smRadius,
              border: Border.all(color: AppColors.border),
            ),
            child: file.isImage
                ? ClipRRect(
                    borderRadius: AppRadius.smRadius,
                    child: Image.memory(
                      base64Decode(file.base64Data),
                      fit: BoxFit.cover,
                      errorBuilder: (_, __, ___) => _fileIcon(file),
                    ),
                  )
                : _fileIcon(file),
          ),
          // 文件名（底部）
          Positioned(
            bottom: 0,
            left: 0,
            right: 0,
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 3, vertical: 2),
              decoration: BoxDecoration(
                color: Colors.black.withOpacity(0.45),
                borderRadius: const BorderRadius.only(
                  bottomLeft: Radius.circular(4),
                  bottomRight: Radius.circular(4),
                ),
              ),
              child: Text(
                file.filename,
                style: const TextStyle(fontSize: 9, color: Colors.white),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ),
          // 删除按钮
          Positioned(
            top: -6,
            right: -6,
            child: GestureDetector(
              onTap: onRemove,
              child: Container(
                width: 18,
                height: 18,
                decoration: const BoxDecoration(
                  color: AppColors.statusError,
                  shape: BoxShape.circle,
                ),
                child: const Icon(Icons.close, size: 11, color: Colors.white),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _fileIcon(AttachedFile file) {
    final icon = file.isPdf ? Icons.picture_as_pdf : Icons.insert_drive_file_outlined;
    final color = file.isPdf ? AppColors.statusError : AppColors.textSecondary;
    return Center(child: Icon(icon, size: 28, color: color));
  }
}

// 模型选择器
class _ModelSelector extends StatelessWidget {
  final ModelInfo? selectedModel;
  final List<ModelInfo> availableModels;
  final void Function(ModelInfo? model) onSelect;

  const _ModelSelector({
    required this.selectedModel,
    required this.availableModels,
    required this.onSelect,
  });

  @override
  Widget build(BuildContext context) {
    final label = selectedModel?.modelID ?? '默认模型';
    return InkWell(
      onTap: () => _showModelPicker(context),
      borderRadius: AppRadius.smRadius,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
        decoration: BoxDecoration(
          color: selectedModel != null ? AppColors.primaryLight : AppColors.inputBackground,
          borderRadius: AppRadius.smRadius,
          border: Border.all(
            color: selectedModel != null ? AppColors.primaryMuted : AppColors.border,
          ),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.auto_awesome_outlined,
              size: 13,
              color: selectedModel != null ? AppColors.primary : AppColors.textSecondary,
            ),
            const SizedBox(width: 4),
            Text(
              label,
              style: TextStyle(
                fontSize: 12,
                color: selectedModel != null ? AppColors.primary : AppColors.textSecondary,
              ),
            ),
            const SizedBox(width: 3),
            Icon(
              Icons.expand_more,
              size: 13,
              color: selectedModel != null ? AppColors.primary : AppColors.textMuted,
            ),
          ],
        ),
      ),
    );
  }

  void _showModelPicker(BuildContext context) {
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => _ModelPickerSheet(
        availableModels: availableModels,
        selectedModel: selectedModel,
        onSelect: (m) {
          Navigator.of(context).pop();
          onSelect(m);
        },
      ),
    );
  }
}

class _ModelPickerSheet extends StatefulWidget {
  final List<ModelInfo> availableModels;
  final ModelInfo? selectedModel;
  final void Function(ModelInfo? model) onSelect;

  const _ModelPickerSheet({
    required this.availableModels,
    required this.selectedModel,
    required this.onSelect,
  });

  @override
  State<_ModelPickerSheet> createState() => _ModelPickerSheetState();
}

class _ModelPickerSheetState extends State<_ModelPickerSheet> {
  String _search = '';

  @override
  Widget build(BuildContext context) {
    final filtered = widget.availableModels.where((m) {
      if (_search.isEmpty) return true;
      final q = _search.toLowerCase();
      return m.modelID.toLowerCase().contains(q) ||
          m.providerID.toLowerCase().contains(q) ||
          m.name.toLowerCase().contains(q);
    }).toList();

    // 按 providerID 分组
    final grouped = <String, List<ModelInfo>>{};
    for (final m in filtered) {
      grouped.putIfAbsent(m.providerID, () => []).add(m);
    }

    return DraggableScrollableSheet(
      initialChildSize: 0.6,
      maxChildSize: 0.9,
      minChildSize: 0.3,
      expand: false,
      builder: (_, scrollCtrl) => Column(
        children: [
          // 标题栏
          Container(
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
            child: Column(
              children: [
                Row(
                  children: [
                    const Text(
                      '选择模型',
                      style: TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w600,
                        color: AppColors.textPrimary,
                      ),
                    ),
                    const Spacer(),
                    if (widget.selectedModel != null)
                      TextButton(
                        onPressed: () => widget.onSelect(null),
                        child: const Text('重置为默认'),
                      ),
                  ],
                ),
                const SizedBox(height: 8),
                TextField(
                  autofocus: false,
                  decoration: InputDecoration(
                    hintText: '搜索模型...',
                    prefixIcon: const Icon(Icons.search, size: 18),
                    border: OutlineInputBorder(
                      borderRadius: AppRadius.smRadius,
                      borderSide: const BorderSide(color: AppColors.border),
                    ),
                    enabledBorder: OutlineInputBorder(
                      borderRadius: AppRadius.smRadius,
                      borderSide: const BorderSide(color: AppColors.border),
                    ),
                    contentPadding: const EdgeInsets.symmetric(
                        horizontal: 12, vertical: 8),
                    isDense: true,
                    filled: true,
                    fillColor: AppColors.inputBackground,
                  ),
                  onChanged: (v) => setState(() => _search = v),
                ),
              ],
            ),
          ),
          const Divider(height: 1, color: AppColors.border),
          // 模型列表
          Expanded(
            child: filtered.isEmpty
                ? Center(
                    child: Text(
                      widget.availableModels.isEmpty ? '暂无可用模型\n请确保 opencode 服务已启动' : '无匹配模型',
                      textAlign: TextAlign.center,
                      style: const TextStyle(color: AppColors.textMuted, fontSize: 13),
                    ),
                  )
                : ListView.builder(
                    controller: scrollCtrl,
                    itemCount: grouped.length,
                    itemBuilder: (_, gi) {
                      final provider = grouped.keys.elementAt(gi);
                      final models = grouped[provider]!;
                      return Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Padding(
                            padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
                            child: Text(
                              provider,
                              style: const TextStyle(
                                fontSize: 11,
                                fontWeight: FontWeight.w600,
                                color: AppColors.textMuted,
                                letterSpacing: 0.5,
                              ),
                            ),
                          ),
                          ...models.map((m) {
                            final isSelected =
                                widget.selectedModel?.modelID == m.modelID &&
                                widget.selectedModel?.providerID == m.providerID;
                            return InkWell(
                              onTap: () => widget.onSelect(m),
                              child: Container(
                                padding: const EdgeInsets.symmetric(
                                    horizontal: 16, vertical: 10),
                                color: isSelected
                                    ? AppColors.primaryLight
                                    : Colors.transparent,
                                child: Row(
                                  children: [
                                    Expanded(
                                      child: Column(
                                        crossAxisAlignment:
                                            CrossAxisAlignment.start,
                                        children: [
                                          Text(
                                            m.modelID,
                                            style: TextStyle(
                                              fontSize: 13,
                                              fontWeight: isSelected
                                                  ? FontWeight.w600
                                                  : FontWeight.w400,
                                              color: isSelected
                                                  ? AppColors.primary
                                                  : AppColors.textPrimary,
                                            ),
                                          ),
                                          if (m.name != m.modelID)
                                            Text(
                                              m.name,
                                              style: const TextStyle(
                                                fontSize: 11,
                                                color: AppColors.textMuted,
                                              ),
                                            ),
                                        ],
                                      ),
                                    ),
                                    if (isSelected)
                                      const Icon(Icons.check,
                                          size: 16, color: AppColors.primary),
                                  ],
                                ),
                              ),
                            );
                          }),
                        ],
                      );
                    },
                  ),
          ),
        ],
      ),
    );
  }
}

class _SendButton extends StatelessWidget {
  final bool sending;
  final VoidCallback onSend;
  final VoidCallback onStop;
  const _SendButton({required this.sending, required this.onSend, required this.onStop});

  @override
  Widget build(BuildContext context) {
    return AnimatedSwitcher(
      duration: const Duration(milliseconds: 180),
      child: sending
          ? GestureDetector(
              key: const ValueKey('stop'),
              onTap: onStop,
              child: Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(
                  color: AppColors.statusError.withValues(alpha: 0.1),
                  borderRadius: AppRadius.smRadius,
                  border: Border.all(color: AppColors.statusError.withValues(alpha: 0.4)),
                ),
                child: Icon(
                  Icons.stop_rounded,
                  size: 20,
                  color: AppColors.statusError,
                ),
              ),
            )
          : GestureDetector(
              key: const ValueKey('send'),
              onTap: onSend,
              child: Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(
                  color: AppColors.primary,
                  borderRadius: AppRadius.smRadius,
                ),
                child: const Icon(
                  Icons.send_rounded,
                  size: 18,
                  color: Colors.white,
                ),
              ),
            ),
    );
  }
}

// 审批模式切换按钮
class _PermissionModeButton extends StatelessWidget {
  final String mode;
  final void Function(String mode) onChanged;

  const _PermissionModeButton({required this.mode, required this.onChanged});

  @override
  Widget build(BuildContext context) {
    final label = switch (mode) {
      'auto-approve' => '自动批准',
      'deny' => '全部拒绝',
      _ => '需要审批',
    };
    final color = switch (mode) {
      'auto-approve' => AppColors.statusSuccess,
      'deny' => AppColors.statusError,
      _ => AppColors.statusWarning,
    };
    return InkWell(
      onTap: () => _showPicker(context),
      borderRadius: AppRadius.smRadius,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        decoration: BoxDecoration(
          color: color.withValues(alpha: 0.1),
          borderRadius: AppRadius.smRadius,
          border: Border.all(color: color.withValues(alpha: 0.4)),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.security_outlined, size: 13, color: color),
            const SizedBox(width: 4),
            Text(label, style: TextStyle(fontSize: 12, color: color, fontWeight: FontWeight.w500)),
            const SizedBox(width: 2),
            Icon(Icons.expand_more, size: 13, color: color),
          ],
        ),
      ),
    );
  }

  void _showPicker(BuildContext context) {
    final options = [
      ('ask', '需要审批', '执行敏感操作时弹窗让您确认', AppColors.statusWarning, Icons.help_outline),
      ('auto-approve', '自动批准', '自动通过所有权限请求（风险较高）', AppColors.statusSuccess, Icons.check_circle_outline),
      ('deny', '全部拒绝', '拒绝所有权限请求，任务会因缺权限失败', AppColors.statusError, Icons.block_outlined),
    ];
    showModalBottomSheet(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('审批模式', style: TextStyle(fontWeight: FontWeight.w600, fontSize: 15)),
              const SizedBox(height: 4),
              const Text('控制 AI 执行需要权限的操作（如文件写入、命令执行）时的行为', style: TextStyle(fontSize: 12, color: AppColors.textSecondary)),
              const SizedBox(height: 12),
              ...options.map((opt) {
                final (value, title, desc, color, icon) = opt;
                final selected = mode == value;
                return InkWell(
                  onTap: () {
                    Navigator.of(context).pop();
                    onChanged(value);
                  },
                  borderRadius: AppRadius.mdRadius,
                  child: Container(
                    margin: const EdgeInsets.only(bottom: 8),
                    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                    decoration: BoxDecoration(
                      color: selected ? color.withValues(alpha: 0.08) : Colors.transparent,
                      borderRadius: AppRadius.mdRadius,
                      border: Border.all(
                        color: selected ? color.withValues(alpha: 0.5) : AppColors.border,
                      ),
                    ),
                    child: Row(
                      children: [
                        Icon(icon, size: 18, color: selected ? color : AppColors.textSecondary),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(title, style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500, color: selected ? color : AppColors.textPrimary)),
                              Text(desc, style: const TextStyle(fontSize: 11, color: AppColors.textSecondary)),
                            ],
                          ),
                        ),
                        if (selected) Icon(Icons.check, size: 16, color: color),
                      ],
                    ),
                  ),
                );
              }),
            ],
          ),
        ),
      ),
    );
  }
}

// 历史会话侧边栏
class _SessionSidebar extends ConsumerWidget {
  final String agentId;
  final String? currentSessionId;
  final void Function(String sid) onSelect;
  final VoidCallback onNew;

  const _SessionSidebar({
    required this.agentId,
    required this.currentSessionId,
    required this.onSelect,
    required this.onNew,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final sessionsAsync = ref.watch(sessionListProvider(agentId));

    return SizedBox(
      width: 240,
      child: Container(
        color: AppColors.surfaceElevated,
        child: Column(
          children: [
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
              decoration: const BoxDecoration(
                border: Border(bottom: BorderSide(color: AppColors.border)),
              ),
              child: Row(
                children: [
                  const Text(
                    '历史会话',
                    style: TextStyle(
                      fontSize: 13,
                      fontWeight: FontWeight.w600,
                      color: AppColors.textPrimary,
                    ),
                  ),
                  const Spacer(),
                  GestureDetector(
                    onTap: onNew,
                    child: Container(
                      padding: const EdgeInsets.all(5),
                      decoration: BoxDecoration(
                        color: AppColors.primaryLight,
                        borderRadius: AppRadius.smRadius,
                      ),
                      child: const Icon(Icons.add,
                          size: 14, color: AppColors.primary),
                    ),
                  ),
                ],
              ),
            ),
            Expanded(
              child: sessionsAsync.when(
                loading: () => const LoadingState(),
                error: (e, _) => Center(
                  child: Text(
                    '加载失败',
                    style: const TextStyle(
                        fontSize: 12, color: AppColors.textMuted),
                  ),
                ),
                data: (sessions) {
                  if (sessions.isEmpty) {
                    return const Center(
                      child: Text(
                        '暂无历史会话',
                        style: TextStyle(
                            fontSize: 12, color: AppColors.textMuted),
                      ),
                    );
                  }
                  return ListView.builder(
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    itemCount: sessions.length,
                    itemBuilder: (context, i) {
                      final session = sessions[i];
                      final isActive =
                          session.sessionId == currentSessionId;
                      return _SessionListItem(
                        session: session,
                        isActive: isActive,
                        onTap: () => onSelect(session.sessionId),
                      );
                    },
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _SessionListItem extends StatelessWidget {
  final SessionModel session;
  final bool isActive;
  final VoidCallback onTap;

  const _SessionListItem({
    required this.session,
    required this.isActive,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: onTap,
      child: Container(
        margin: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
        decoration: BoxDecoration(
          color: isActive ? AppColors.primaryLight : Colors.transparent,
          borderRadius: AppRadius.smRadius,
          border: isActive
              ? Border.all(color: AppColors.primaryMuted)
              : null,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              session.summary?.isNotEmpty == true
                  ? session.summary!
                  : session.sessionId,
              style: TextStyle(
                fontSize: 12,
                fontWeight: isActive ? FontWeight.w600 : FontWeight.w400,
                color:
                    isActive ? AppColors.primary : AppColors.textPrimary,
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
            if (session.updatedAt != null) ...[
              const SizedBox(height: 3),
              Text(
                _formatTime(session.updatedAt!),
                style: const TextStyle(
                  fontSize: 11,
                  color: AppColors.textMuted,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  String _formatTime(DateTime dt) {
    final diff = DateTime.now().difference(dt);
    if (diff.inMinutes < 60) return '${diff.inMinutes} 分钟前';
    if (diff.inHours < 24) return '${diff.inHours} 小时前';
    return '${dt.month}/${dt.day}';
  }
}

// 复制全文按钮
class _CopyButton extends StatefulWidget {
  final String content;
  const _CopyButton({required this.content});

  @override
  State<_CopyButton> createState() => _CopyButtonState();
}

class _CopyButtonState extends State<_CopyButton> {
  bool _copied = false;

  Future<void> _copy() async {
    await Clipboard.setData(ClipboardData(text: widget.content));
    setState(() => _copied = true);
    await Future.delayed(const Duration(seconds: 2));
    if (mounted) setState(() => _copied = false);
  }

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: _copy,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            _copied ? Icons.check : Icons.copy_outlined,
            size: 13,
            color: _copied ? AppColors.statusSuccess : AppColors.textMuted,
          ),
          const SizedBox(width: 3),
          Text(
            _copied ? '已复制' : '复制',
            style: TextStyle(
              fontSize: 11,
              color: _copied ? AppColors.statusSuccess : AppColors.textMuted,
            ),
          ),
        ],
      ),
    );
  }
}
