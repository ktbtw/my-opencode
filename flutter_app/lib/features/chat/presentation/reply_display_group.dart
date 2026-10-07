import '../data/chat_model.dart';

bool isBackgroundReplyEvent(ChatMessage message) =>
    message.isInsertedContext &&
    (message.insertedContextType == 'backgroundJob' ||
        message.insertedContextSource == 'background_job');

/// 用户在任务进行中插入的一句。连续回复里它嵌在同一段正文中间，
/// 不另起气泡；后台任务和子代理结果不走这条。
bool isInlineUserInsert(ChatMessage message) =>
    message.isInsertedContext &&
    message.insertedContextType == 'queueInsert' &&
    message.insertedContextSource != 'background_job' &&
    message.insertedContextSource != 'subagent';

/// 能作为连续回复段起点的消息：Agent 回复本身，或是任务中插入的一句
/// （插入后面往往接着同任务的回复，它们同属一个气泡）。
bool startsReplyGroup(ChatMessage message) =>
    message.role == MessageRole.agent || isInlineUserInsert(message);

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
    message.plan != null ||
    message.state == MessageState.failed;

/// Presentation only: keep message identities, tool positions and task data.
/// An ordinary user message or a different task starts a separate surface.
List<List<ChatMessage>> groupReplyDisplay(
  List<ChatMessage> messages, {
  required bool continuous,
}) {
  final groups = <List<ChatMessage>>[];
  for (var i = 0; i < messages.length; i++) {
    final first = messages[i];
    final group = <ChatMessage>[first];
    groups.add(group);
    if (!continuous || first.taskId == null || !startsReplyGroup(first)) {
      continue;
    }
    while (i + 1 < messages.length) {
      var next = i + 1;
      while (next < messages.length &&
          messages[next].taskId == first.taskId &&
          (isBackgroundReplyEvent(messages[next]) ||
              isInlineUserInsert(messages[next]))) {
        next++;
      }
      // 相邻的同任务回复同样合并：目标模式轮次、子代理唤醒轮次之间不会插入
      // background 事件，之前会因为 next == i + 1 被拆成多个气泡。
      if (next >= messages.length ||
          messages[next].role != MessageRole.agent ||
          messages[next].taskId != first.taskId) {
        break;
      }
      if (messages[next].isInsertedContext &&
          !isInlineUserInsert(messages[next])) {
        break;
      }
      group.addAll(messages.getRange(i + 1, next + 1));
      i = next;
    }
    while (i + 1 < messages.length &&
        messages[i + 1].taskId == first.taskId &&
        (isInlineUserInsert(messages[i + 1]) ||
            isBackgroundReplyEvent(messages[i + 1]))) {
      i++;
      group.add(messages[i]);
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

  /// 无定位任务卡片（执行计划）的归属：键为渲染位置的消息 id，值为该段
  /// 要渲染的计划。位置固定在整段末尾，避免卡片插在两段正文中间。
  final Map<String, PlanInfo> planCards;

  const ReplyDisplayLayout({
    this.boundaries = const {},
    this.thinking = const {},
    this.thinkingStreaming = const {},
    this.toolHeaderOwners = const {},
    this.planCards = const {},
  });

  static ReplyDisplayLayout of(List<ChatMessage> messages) {
    final boundaries = <String, (bool, bool, bool)>{};
    final thinking = <String, String>{};
    final thinkingStreaming = <String>{};
    final toolHeaderOwners = <String>{};
    final planCards = <String, PlanInfo>{};
    for (final group in groupReplyDisplay(messages, continuous: true)) {
      if (!startsReplyGroup(group.first)) continue;
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
      // Render the plan after every group member, including trailing inserts,
      // background events and an empty streaming round.
      PlanInfo? groupPlan;
      for (final item in members) {
        final plan = item.plan;
        if (plan != null && !plan.isEmpty) groupPlan = plan;
      }
      if (groupPlan != null) {
        planCards[group.last.id] = groupPlan;
      }
      final surfaces = group
          .where(
            (item) => hasReplySurface(item) || planCards.containsKey(item.id),
          )
          .toList(growable: false);
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
      planCards: planCards,
    );
  }
}
