import '../data/chat_model.dart';

bool isBackgroundReplyEvent(ChatMessage message) =>
    message.isInsertedContext &&
    (message.insertedContextType == 'backgroundJob' ||
        message.insertedContextSource == 'background_job');

/// 工具调用没有顺序信息：连续回复合并成一段时标题只需出现一次。
bool hasUnpositionedTools(ChatMessage message) =>
    message.toolCalls.isNotEmpty &&
    (!message.toolOrderKnown ||
        (message.blocks.isEmpty && message.content.isNotEmpty));

bool hasReplySurface(ChatMessage message) =>
    isBackgroundReplyEvent(message) ||
    message.content.isNotEmpty ||
    message.blocks.isNotEmpty ||
    message.toolCalls.isNotEmpty ||
    message.imageGeneration != null ||
    message.isFinalizingDelivery ||
    message.artifacts.isNotEmpty ||
    message.files.isNotEmpty ||
    message.state == MessageState.failed;

/// Presentation only: keep message identities, tool positions and task data.
/// A user insertion or a different task always starts a separate surface.
List<List<ChatMessage>> groupReplyDisplay(
  List<ChatMessage> messages, {
  required bool continuous,
}) {
  final groups = <List<ChatMessage>>[];
  for (var i = 0; i < messages.length; i++) {
    final first = messages[i];
    final group = <ChatMessage>[first];
    groups.add(group);
    if (!continuous ||
        first.role != MessageRole.agent ||
        first.isInsertedContext ||
        first.taskId == null) {
      continue;
    }
    while (i + 1 < messages.length) {
      var next = i + 1;
      while (next < messages.length &&
          messages[next].taskId == first.taskId &&
          isBackgroundReplyEvent(messages[next])) {
        next++;
      }
      // 相邻的同任务回复同样合并：目标模式轮次、子代理唤醒轮次之间不会插入
      // background 事件，之前会因为 next == i + 1 被拆成多个气泡。
      if (next >= messages.length ||
          messages[next].role != MessageRole.agent ||
          messages[next].isInsertedContext ||
          messages[next].taskId != first.taskId) {
        break;
      }
      group.addAll(messages.getRange(i + 1, next + 1));
      i = next;
    }
  }
  return groups;
}

/// 连续回复合并成一段后，需要「整段只出现一次」的内容归属。
class ReplyDisplayLayout {
  /// 每条消息在所属段里的位置（只有真正的连续段才有条目）。
  final Map<String, (bool first, bool last, bool surfaceLast)> boundaries;

  /// 组内合并后的思考文本，挂在组内第一条消息上。
  final Map<String, String> thinking;
  final Set<String> thinkingStreaming;

  /// 无定位工具记录的标题归属（组内第一条带此类工具的消息）。
  final Set<String> toolHeaderOwners;

  const ReplyDisplayLayout({
    this.boundaries = const {},
    this.thinking = const {},
    this.thinkingStreaming = const {},
    this.toolHeaderOwners = const {},
  });

  static ReplyDisplayLayout of(List<ChatMessage> messages) {
    final boundaries = <String, (bool, bool, bool)>{};
    final thinking = <String, String>{};
    final thinkingStreaming = <String>{};
    final toolHeaderOwners = <String>{};
    for (final group in groupReplyDisplay(messages, continuous: true)) {
      if (group.first.role != MessageRole.agent ||
          group.first.isInsertedContext) {
        continue;
      }
      final members = group
          .where((item) => !isBackgroundReplyEvent(item))
          .toList(growable: false);
      final merged = members
          .map((item) => item.thinkingContent.trim())
          .where((text) => text.isNotEmpty)
          .join('\n\n');
      if (merged.isNotEmpty) {
        thinking[group.first.id] = merged;
        if (members.any((item) => item.state == MessageState.streaming)) {
          thinkingStreaming.add(group.first.id);
        }
      }
      for (final item in members) {
        if (hasUnpositionedTools(item)) {
          toolHeaderOwners.add(item.id);
          break;
        }
      }
      final surfaces = group.where(hasReplySurface).toList(growable: false);
      for (var i = 0; i < group.length; i++) {
        boundaries[group[i].id] = (
          i == 0,
          i == group.length - 1,
          surfaces.isNotEmpty && identical(group[i], surfaces.last),
        );
      }
    }
    return ReplyDisplayLayout(
      boundaries: boundaries,
      thinking: thinking,
      thinkingStreaming: thinkingStreaming,
      toolHeaderOwners: toolHeaderOwners,
    );
  }
}
