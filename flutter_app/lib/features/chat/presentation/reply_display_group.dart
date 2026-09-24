import '../data/chat_model.dart';

bool isBackgroundReplyEvent(ChatMessage message) =>
    message.isInsertedContext &&
    (message.insertedContextType == 'backgroundJob' ||
        message.insertedContextSource == 'background_job');

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
      if (next == i + 1 ||
          next >= messages.length ||
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
