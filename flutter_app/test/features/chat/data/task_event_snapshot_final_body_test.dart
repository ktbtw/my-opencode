import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/data/chat_repository.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  final repository = ChatRepository();
  const tool = {
    'id': 'final-tool',
    'call_id': 'final-tool',
    'tool': 'read',
    'status': 'running',
  };

  String rendered(List<ChatMessageBlock> blocks) =>
      blocks.where((block) => block.isText).map((block) => block.text).join();

  test('terminal payload supplies a body even without text deltas', () {
    final snapshot = repository.snapshotFromTaskEvents('task', [
      {'type': 'tool_updated', 'tool': tool},
      {'type': 'completed', 'content': '完整总结'},
    ]);
    expect(snapshot.text, '完整总结');
    expect(snapshot.rounds.single.text, '完整总结');
    expect(rendered(snapshot.blocks), snapshot.text);
    expect(snapshot.blocks.first.tool?.status, 'completed');
    expect(snapshot.blocks.last.text, '完整总结');
  });

  test(
    'terminal payload restores a missing suffix after interleaved tools',
    () {
      final snapshot = repository.snapshotFromTaskEvents('task', [
        {'type': 'delta', 'content': '前文'},
        {'type': 'tool_updated', 'tool': tool},
        {'type': 'delta', 'content': '总结开头'},
        {'type': 'completed', 'content': '前文总结开头及完整尾部'},
      ]);
      expect(snapshot.text, '前文总结开头及完整尾部');
      expect(snapshot.blocks.map((b) => b.type), [
        ChatMessageBlockType.text,
        ChatMessageBlockType.tool,
        ChatMessageBlockType.text,
      ]);
      expect(snapshot.blocks.first.text, '前文');
      expect(snapshot.blocks.last.text, '总结开头及完整尾部');
      expect(rendered(snapshot.rounds.single.blocks), snapshot.text);
    },
  );

  for (final roundResult in ['', '第二轮完整总结']) {
    test(
      'terminal result is scoped to the final input round ($roundResult)',
      () {
        final snapshot = repository.snapshotFromTaskEvents('task', [
          {'type': 'delta', 'content': '第一轮'},
          {
            'type': 'input_applied',
            'content': '继续',
            'metadata': {'queue_item_id': 'q'},
          },
          {'type': 'tool_updated', 'tool': tool},
          {
            'type': 'completed',
            'content': '第一轮第二轮完整总结',
            'metadata': {'round_result': roundResult},
          },
        ]);
        expect(snapshot.rounds.map((round) => round.text), ['第一轮', '第二轮完整总结']);
        expect(snapshot.text, '第一轮第二轮完整总结');
        expect(rendered(snapshot.blocks), snapshot.text);
        expect(snapshot.appliedInputs, hasLength(1));
        expect(snapshot.rounds.last.blocks.first.tool?.status, 'completed');
      },
    );
  }

  test(
    'goal rounds retain their boundaries when the terminal body arrives',
    () {
      final snapshot = repository.snapshotFromTaskEvents('task', [
        {'type': 'goal_created', 'iteration': 1},
        {'type': 'delta', 'content': '第一轮'},
        {'type': 'goal_continued', 'iteration': 2},
        {'type': 'tool_updated', 'tool': tool},
        {'type': 'completed', 'content': '第一轮最后总结'},
      ]);
      expect(snapshot.rounds.map((round) => round.text), ['第一轮', '最后总结']);
      expect(snapshot.rounds.last.blocks.first.tool?.status, 'completed');
      expect(rendered(snapshot.blocks), snapshot.text);
    },
  );

  test(
    'separate final summary follows commentary and is not duplicated on replay',
    () {
      final snapshot = repository.snapshotFromTaskEvents('task', [
        {'type': 'delta', 'content': '正在查看详细结果'},
        {'type': 'tool_updated', 'tool': tool},
        {'type': 'completed', 'content': '完成'},
        {'type': 'completed', 'content': '完成'},
      ]);
      expect(snapshot.text, '正在查看详细结果\n\n完成');
      expect(snapshot.blocks.map((b) => b.type), [
        ChatMessageBlockType.text,
        ChatMessageBlockType.tool,
        ChatMessageBlockType.text,
      ]);
      expect(rendered(snapshot.blocks), snapshot.text);
    },
  );

  test(
    'explicit round result replaces revised partial text without duplicates',
    () {
      final snapshot = repository.snapshotFromTaskEvents('task', [
        {'type': 'delta', 'content': '第一轮'},
        {'type': 'input_applied', 'content': '继续'},
        {'type': 'tool_updated', 'tool': tool},
        {'type': 'delta', 'content': '第二轮流式回复'},
        {
          'type': 'completed',
          'content': '第一轮第二轮完整回复',
          'metadata': {'round_result': '第二轮完整回复'},
        },
      ]);
      expect(snapshot.rounds.last.text, '第二轮完整回复');
      expect(snapshot.text, '第一轮第二轮完整回复');
      expect(rendered(snapshot.blocks), snapshot.text);
      expect(snapshot.rounds.last.blocks.first.tool?.status, 'completed');
    },
  );
}
