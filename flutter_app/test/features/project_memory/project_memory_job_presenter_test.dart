import 'package:chat_codex_app/features/project_memory/data/project_memory_model.dart';
import 'package:chat_codex_app/features/project_memory/presentation/project_memory_job_presenter.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('project memory event labels cover worker and retry states', () {
    expect(projectMemoryEventLabel('accepted'), '读取项目证据');
    expect(projectMemoryEventLabel('sources'), '校验来源文件');
    expect(projectMemoryEventLabel('retrying'), '正在重试');
    expect(projectMemoryEventLabel('unknown-stage'), '整理中');
  });

  test('legacy English project memory messages are localized', () {
    const running = ProjectMemoryJobEventModel(
      sequence: 1,
      type: 'progress',
      message: 'running',
    );
    const deferred = ProjectMemoryJobEventModel(
      sequence: 2,
      type: 'candidates',
      message: '3 changed candidates deferred',
    );
    expect(projectMemoryEventMessage(running), '正在分析项目证据');
    expect(projectMemoryEventMessage(deferred), '已暂缓 3 条来源发生变化的候选记忆');
  });

  test('project memory event metadata parses result counts', () {
    final event = ProjectMemoryJobEventModel.fromJson({
      'sequence': 3,
      'type': 'candidates',
      'metadata': {
        'candidate_count': 4,
        'accepted_count': '2',
        'changed': true,
      },
    });
    expect(event.metadataInt('candidate_count'), 4);
    expect(event.metadataInt('accepted_count'), 2);
    expect(event.metadataBool('changed'), isTrue);
  });
}
