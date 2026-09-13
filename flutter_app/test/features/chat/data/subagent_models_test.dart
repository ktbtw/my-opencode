import 'package:chat_codex_app/features/chat/data/subagent_models.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('parses durable subagent nodes and terminal state', () {
    final snapshot = SubagentTreeSnapshot.fromJson({
      'task_id': 'task-1',
      'nodes': [
        {
          'node_id': 'node-1',
          'plan_id': 'plan-1',
          'child_session_id': 'child-1',
          'role': 'repo-explorer',
          'title': 'Inspect repository',
          'state': 'completed',
          'attempt': 2,
          'depends_on': ['scan'],
          'blocked_by': [],
          'priority': 75,
          'model': {'providerID': 'test', 'modelID': 'test-model'},
          'pending_instructions': ['keep output concise'],
          'started_at': 100,
          'completed_at': 200,
          'output': 'evidence',
          'artifacts': [
            {
              'id': 'artifact-1',
              'filename': 'report.txt',
              'relative_path':
                  '.chatcodex-artifacts/task-1/subagents/node-1/report.txt',
            },
          ],
          'last_control': {'action': 'cancel'},
        },
      ],
    });

    expect(snapshot.taskId, 'task-1');
    expect(snapshot.nodes, hasLength(1));
    expect(snapshot.nodes.single.isTerminal, isTrue);
    expect(snapshot.nodes.single.output, 'evidence');
    expect(snapshot.nodes.single.lastControl['action'], 'cancel');
    expect(snapshot.nodes.single.planId, 'plan-1');
    expect(snapshot.nodes.single.childSessionId, 'child-1');
    expect(snapshot.nodes.single.attempt, 2);
    expect(snapshot.nodes.single.dependsOn, ['scan']);
    expect(snapshot.nodes.single.priority, 75);
    expect(snapshot.nodes.single.model['modelID'], 'test-model');
    expect(snapshot.nodes.single.pendingInstructions, ['keep output concise']);
    expect(snapshot.nodes.single.artifacts.single['filename'], 'report.txt');
  });

  test('keeps unknown node states visible for recovery', () {
    final node = SubagentNode.fromJson({
      'node_id': 'node-2',
      'state': 'missing',
    });
    expect(node.state, 'missing');
    expect(node.isTerminal, isFalse);
  });

  test('filters legacy placeholder nodes with a missing node id', () {
    final snapshot = SubagentTreeSnapshot.fromJson({
      'task_id': 'task-1',
      'nodes': [
        {'node_id': '<nil>', 'state': 'unknown'},
        {'node_id': 'nil', 'state': 'unknown'},
        {'node_id': 'actual-node', 'state': 'queued'},
      ],
    });

    expect(snapshot.nodes.map((node) => node.nodeId), ['actual-node']);
  });

  test('parses paginated subagent log events including terminal errors', () {
    final event = SubagentLogEvent.fromJson({
      'type': 'subagent_result',
      'content': 'verification output',
      'error': 'timeout details',
      'sent_at': '2026-07-31T10:00:00Z',
    });

    expect(event.type, 'subagent_result');
    expect(event.content, 'verification output');
    expect(event.error, 'timeout details');
    expect(event.sentAt, '2026-07-31T10:00:00Z');
  });

  test('keeps protocol tool ids out of user-facing orchestration labels', () {
    expect(displayToolName('task_status'), '查看进度');
    expect(displayToolName('orchestrate'), '安排协作');
    expect(displayToolName('task'), '协作处理');

    const running = ToolCallInfo(
      id: 'status-1',
      callId: 'status-1',
      tool: 'task_status',
      status: 'running',
    );
    const completed = ToolCallInfo(
      id: 'status-2',
      callId: 'status-2',
      tool: 'task_status',
      status: 'completed',
    );
    expect(displayToolTitle(running), '正在查看进度');
    expect(displayToolTitle(completed), '进度已更新');
  });

  test('maps runtime agent ids to user-facing role labels', () {
    expect(displaySubagentRole('explore'), '探索分析');
    expect(displaySubagentRole('repo-explorer'), '仓库探索');
    expect(displaySubagentRole('unregistered-role'), '专项处理');
  });

  test('builds a terminal subagent card from a result event', () {
    final tool = subagentToolCallFromMetadata(
      taskId: 'task-1',
      metadata: {
        'node_id': 'node-1',
        'subagent_type': 'explore',
        'title': 'Inspect files',
        'phase': 'result',
      },
      status: 'completed',
      output: 'finished',
    );
    expect(tool.isCompleted, isTrue);
    expect(tool.metadata['phase'], 'result');
    expect(tool.output, 'finished');
    expect(displaySubagentRole(tool.input['role'] as String), '探索分析');
  });

  test('keeps a started relay subagent in the running phase', () {
    final tool = subagentToolCallFromMetadata(
      taskId: 'task-1',
      metadata: {
        'node_id': 'node-1',
        'subagent_type': 'explore',
        'title': 'Inspect files',
        'phase': 'running',
      },
    );

    expect(tool.isRunning, isTrue);
    expect(tool.metadata['phase'], 'running');
    expect(tool.stableKey, contains('_running'));
  });
}
