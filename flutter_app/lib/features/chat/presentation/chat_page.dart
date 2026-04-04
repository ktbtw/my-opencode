import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/theme/app_colors.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/widgets.dart';
import '../data/chat_model.dart';
import 'chat_provider.dart';

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
    await ref.read(chatProvider(_chatKey).notifier).sendMessage(text);
    _scrollToBottom();
  }

  @override
  Widget build(BuildContext context) {
    final chat = ref.watch(chatProvider(_chatKey));
    final isMobile = AppBreakpoints.isMobile(context);

    // 有新消息时滚动到底部
    ref.listen(chatProvider(_chatKey), (_, next) {
      if (next.messages.isNotEmpty) _scrollToBottom();
    });

    return Scaffold(
      body: PageBackground(
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
                          sending: chat.sending,
                          sessionId: chat.currentSessionId,
                          onSend: _send,
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
    return Container(
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
                ? () => Scaffold.of(context).openDrawer()
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
    );
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
                _BubbleContent(message: message, isUser: isUser),
                if (message.state == MessageState.streaming)
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: _TypingIndicator(),
                  ),
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

class _BubbleContent extends StatelessWidget {
  final ChatMessage message;
  final bool isUser;
  const _BubbleContent({required this.message, required this.isUser});

  @override
  Widget build(BuildContext context) {
    final isFailed = message.state == MessageState.failed;
    return Container(
      constraints: BoxConstraints(
        maxWidth: MediaQuery.of(context).size.width * 0.72,
      ),
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
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
                color: isFailed ? AppColors.statusError.withOpacity(0.3) : AppColors.border,
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
      child: message.content.isEmpty && !isFailed
          ? const SizedBox(height: 6)
          : SelectableText(
              message.content,
              style: TextStyle(
                fontSize: 14,
                height: 1.55,
                color: isUser
                    ? Colors.white
                    : isFailed
                        ? AppColors.statusError
                        : AppColors.textPrimary,
              ),
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
class _InputArea extends StatelessWidget {
  final TextEditingController controller;
  final FocusNode focusNode;
  final bool sending;
  final String? sessionId;
  final VoidCallback onSend;

  const _InputArea({
    required this.controller,
    required this.focusNode,
    required this.sending,
    required this.sessionId,
    required this.onSend,
  });

  @override
  Widget build(BuildContext context) {
    focusNode.onKeyEvent = (node, event) {
      if (event is KeyDownEvent &&
          event.logicalKey == LogicalKeyboardKey.enter &&
          HardwareKeyboard.instance.isControlPressed) {
        onSend();
        return KeyEventResult.handled;
      }
      return KeyEventResult.ignored;
    };
    return Container(
      decoration: const BoxDecoration(
        color: AppColors.surface,
        border: Border(top: BorderSide(color: AppColors.border)),
      ),
      padding: const EdgeInsets.fromLTRB(12, 10, 12, 12),
      child: Column(
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              Expanded(
                child: TextField(
                  controller: controller,
                  focusNode: focusNode,
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
              _SendButton(sending: sending, onSend: onSend),
            ],
          ),
          if (sessionId != null)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Row(
                children: [
                  const Icon(Icons.link, size: 11, color: AppColors.textMuted),
                  const SizedBox(width: 4),
                  Text(
                    '当前会话 $sessionId',
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

class _SendButton extends StatelessWidget {
  final bool sending;
  final VoidCallback onSend;
  const _SendButton({required this.sending, required this.onSend});

  @override
  Widget build(BuildContext context) {
    return AnimatedSwitcher(
      duration: const Duration(milliseconds: 180),
      child: sending
          ? Container(
              key: const ValueKey('loading'),
              width: 44,
              height: 44,
              decoration: BoxDecoration(
                color: AppColors.primaryLight,
                borderRadius: AppRadius.smRadius,
              ),
              child: const Padding(
                padding: EdgeInsets.all(12),
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  color: AppColors.primary,
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
