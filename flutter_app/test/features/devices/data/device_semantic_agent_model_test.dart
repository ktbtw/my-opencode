import 'package:flutter_test/flutter_test.dart';
import 'package:chat_codex_app/features/devices/data/device_semantic_agent_model.dart';

void main() {
  group('RuntimePreflightJob', () {
    test('parses a persisted computer-side user action', () {
      final job = RuntimePreflightJob.fromJson({
        'id': 'preflight_1',
        'status': 'waiting_user_action',
        'requires_user_action': true,
        'user_action': {
          'id': 'ida-uac:preflight_1',
          'kind': 'windows_uac',
          'title': 'IDA 安装需要管理员确认',
          'message': '请在目标电脑确认。',
          'instructions': ['打开电脑', '点击“是”'],
          'requested_at': '2026-07-30T02:00:00Z',
        },
      });

      expect(job.waitingForUserAction, isTrue);
      expect(job.userAction?.id, 'ida-uac:preflight_1');
      expect(job.userAction?.kind, 'windows_uac');
      expect(job.userAction?.instructions, ['打开电脑', '点击“是”']);
      expect(job.finished, isFalse);
    });

    test('advances to the latest event stage when current_step is stale', () {
      final job = RuntimePreflightJob.fromJson({
        'id': 'preflight_stage',
        'status': 'running',
        'current_step': 'runtime',
        'latest_events': [
          {
            'sequence': 8,
            'item_type': 'skill',
            'status': 'running',
            'message': '正在验证 Skill',
          },
        ],
      });

      expect(effectiveRuntimePreflightStep(job), 'skill');
    });

    test('uses item status when the persisted current step is stale', () {
      const job = RuntimePreflightJob(
        id: 'preflight_item_stage',
        status: 'running',
        currentStep: 'runtime',
        items: [
          RuntimePreflightItem(
            itemType: 'runtime',
            status: 'completed',
            progressPercent: 100,
          ),
          RuntimePreflightItem(itemType: 'mcp', status: 'running'),
        ],
      );

      expect(effectiveRuntimePreflightStep(job), 'mcp');
    });

    test('maps download events into overall preflight progress', () {
      const job = RuntimePreflightJob(
        id: 'preflight_progress',
        status: 'running',
        currentStep: 'runtime',
        progressPercent: 8,
        items: [
          RuntimePreflightItem(
            itemType: 'runtime',
            status: 'running',
            progressPercent: 20,
          ),
        ],
        latestEvents: [
          RuntimePreflightEvent(
            itemType: 'runtime',
            message: '正在下载 Node.js',
            receivedBytes: 50,
            totalBytes: 100,
            progressPercent: 50,
          ),
        ],
      );

      expect(runtimePreflightDisplayProgress(job), 20);
    });

    test('maps the legacy mcp_ready step to apply', () {
      const job = RuntimePreflightJob(
        id: 'preflight_ready',
        status: 'running',
        currentStep: 'mcp_ready',
      );

      expect(effectiveRuntimePreflightStep(job), 'apply');
    });

    test('recognizes a missing Verify token as recoverable input', () {
      const job = RuntimePreflightJob(
        id: 'preflight_verify',
        status: 'failed',
        error: '缺少环境变量 VERIFY_API_TOKEN 或 VERIFY_PROTECT_TOKEN',
      );

      expect(job.missingVerifyTokenFailure, isTrue);
    });
  });

  group('DeviceSemanticAgentProfile', () {
    test('uses mcp_ids when recommended server names are not expanded', () {
      final profile = DeviceSemanticAgentProfile.fromJson({
        'id': 'reverse-android',
        'name': '逆向专家-安卓',
        'mcp_ids': ['ida-idalib-mcp', 'verify-mcp'],
      });

      expect(profile.recommendedMcpServers, contains('verify-mcp'));
      expect(profile.usesVerifyMcp, isTrue);
    });
  });

  group('RuntimePreflightEvent', () {
    test('parses mcp progress details and byte counts', () {
      final event = RuntimePreflightEvent.fromJson({
        'sequence': 7,
        'item_type': 'mcp',
        'item_id': 'android-frida',
        'phase': 'download',
        'status': 'running',
        'message': '正在下载 MCP',
        'received_bytes': 512,
        'total_bytes': 1024,
        'progress_percent': 50,
        'details': {'mcp_name': 'frida-mcp'},
      });

      expect(event.itemType, 'mcp');
      expect(event.itemId, 'android-frida');
      expect(event.receivedBytes, 512);
      expect(event.totalBytes, 1024);
      expect(event.progressPercent, 50);
      expect(event.details['mcp_name'], 'frida-mcp');
    });
  });
}
