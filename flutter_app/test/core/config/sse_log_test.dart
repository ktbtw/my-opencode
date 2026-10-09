import 'package:chat_codex_app/core/config/sse_log.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('SseChannel.fromPath 识别各条事件流', () {
    test('识别全局状态流', () {
      expect(SseChannel.fromPath('/api/overlay/events').name, '全局状态流');
    });

    test('识别对话任务流并提取 taskId', () {
      final channel = SseChannel.fromPath('/api/tasks/task_123/events');
      expect(channel.name, '对话任务流');
      expect(channel.scope, 'task_123');
    });

    test('识别待发送队列流并提取 sessionId', () {
      final channel = SseChannel.fromPath('/api/sessions/ses_abc/queue/events');
      expect(channel.name, '待发送队列流');
      expect(channel.scope, 'ses_abc');
    });

    test('识别项目记忆流并提取 jobId', () {
      final channel = SseChannel.fromPath(
        '/api/devices/m_1/projects/proj_1/memory-jobs/job_9/events',
      );
      expect(channel.name, '项目记忆流');
      expect(channel.scope, 'job_9');
    });

    test('识别模型测速流并提取 testId', () {
      final channel = SseChannel.fromPath('/api/model-tests/test_7/events');
      expect(channel.name, '模型测速流');
      expect(channel.scope, 'test_7');
    });

    test('服务器带路径前缀时仍能识别（生产环境 /codex）', () {
      // 后端地址形如 https://host/codex，真实路径会带上 /codex 前缀。
      expect(
        SseChannel.fromPath('/codex/api/overlay/events').name,
        '全局状态流',
      );
      expect(
        SseChannel.fromPath('/codex/api/tasks/task_123/events').scope,
        'task_123',
      );
      expect(
        SseChannel.fromPath(
          '/codex/api/sessions/ses_abc/queue/events',
        ).scope,
        'ses_abc',
      );
      expect(
        SseChannel.fromPath(
          '/codex/api/devices/m_1/projects/proj_1/memory-jobs/job_9/events',
        ).scope,
        'job_9',
      );
      expect(
        SseChannel.fromPath('/codex/api/model-tests/test_7/events').scope,
        'test_7',
      );
    });

    test('多层前缀同样适用', () {
      expect(
        SseChannel.fromPath('/a/b/c/api/overlay/events').name,
        '全局状态流',
      );
    });

    test('未知路径保留原始路径便于发现新端点', () {
      final channel = SseChannel.fromPath('/api/unknown/events');
      expect(channel.name, '未识别事件流');
      expect(channel.scope, '/api/unknown/events');
    });
  });

  group('SseChannel.label', () {
    test('无业务标识时只显示名称', () {
      expect(SseChannel.overlay.label, '全局状态流');
    });

    test('有业务标识时附加在方括号内', () {
      expect(SseChannel.task('task_1').label, '对话任务流[task_1]');
    });
  });
}
