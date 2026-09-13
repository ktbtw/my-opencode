import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/data/chat_repository.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('sameIdentity does not collapse parallel tools that share a call id', () {
    const first = ToolCallInfo(
      id: 'prt_1',
      callId: 'call_shared',
      tool: 'read',
      status: 'running',
    );
    const second = ToolCallInfo(
      id: 'prt_2',
      callId: 'call_shared',
      tool: 'read',
      status: 'running',
    );

    expect(first.sameIdentity(second), isFalse);
    expect(second.sameIdentity(first), isFalse);
  });

  test('coalescedWith never regresses completed back to running', () {
    const completed = ToolCallInfo(
      id: 'prt_1',
      callId: 'call_abc',
      tool: 'bash',
      status: 'completed',
      output: 'ok',
    );
    const running = ToolCallInfo(
      id: 'prt_1',
      callId: 'call_abc',
      tool: 'bash',
      status: 'running',
    );

    expect(completed.coalescedWith(running).status, 'completed');
    expect(completed.coalescedWith(running).output, 'ok');
    expect(running.coalescedWith(completed).status, 'completed');
  });

  test('sameIdentity matches call id against part id', () {
    const running = ToolCallInfo(
      id: 'prt_1',
      callId: 'call_abc',
      tool: 'bash',
      status: 'running',
    );
    const completed = ToolCallInfo(
      id: 'call_abc',
      callId: 'prt_1',
      tool: 'bash',
      status: 'completed',
    );

    expect(running.sameIdentity(completed), isTrue);
    expect(completed.sameIdentity(running), isTrue);
    expect(running.isTerminal, isFalse);
    expect(completed.isTerminal, isTrue);
  });

  test('fromJson infers completed from ended_at when status is missing', () {
    final tool = ToolCallInfo.fromJson({
      'id': 'prt_1',
      'call_id': 'call_abc',
      'tool': 'glob',
      'ended_at': 1710000000000,
    });

    expect(tool.status, 'completed');
    expect(tool.isTerminal, isTrue);
  });

  test('fromJson infers error from error text when status is missing', () {
    final tool = ToolCallInfo.fromJson({
      'id': 'prt_1',
      'tool': 'bash',
      'error': 'command failed',
    });

    expect(tool.status, 'error');
    expect(tool.isError, isTrue);
  });

  test('snapshotFromTaskEvents keeps text-tool insertion order', () {
    final snapshot = ChatRepository().snapshotFromTaskEvents('task-1', [
      {
        'type': 'delta',
        'field': 'text',
        'content': '先核对上限。',
      },
      {
        'type': 'tool_updated',
        'tool': {
          'id': 'prt_a',
          'call_id': 'call_a',
          'tool': 'mcp_call',
          'status': 'completed',
        },
      },
      {
        'type': 'delta',
        'field': 'text',
        'content': '再推测试包。',
      },
      {
        'type': 'tool_updated',
        'tool': {
          'id': 'prt_b',
          'call_id': 'call_b',
          'tool': 'read',
          'status': 'completed',
        },
      },
      {
        'type': 'delta',
        'field': 'text',
        'content': '测试包已写上。',
      },
    ]);

    expect(snapshot.blocks, hasLength(5));
    expect(snapshot.blocks[0].text, '先核对上限。');
    expect(snapshot.blocks[1].tool?.id, 'prt_a');
    expect(snapshot.blocks[2].text, '再推测试包。');
    expect(snapshot.blocks[3].tool?.id, 'prt_b');
    expect(snapshot.blocks[4].text, '测试包已写上。');
  });
}
