import 'package:chat_codex_app/features/project_memory/data/project_memory_model.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('parses project scope lineage for identity diagnostics', () {
    final scope = ProjectScopeModel.fromJson({
      'project_scope_id': 'scope-copy',
      'machine_id': 'machine-a',
      'display_name': 'copy',
      'lineage_project_scope_id': 'scope-original',
      'binding_epoch': 2,
      'revision': 1,
    });
    expect(scope.lineageScopeId, 'scope-original');
  });

  test('parses native hook verification and immutable version fields', () {
    final memory = ProjectMemoryModel.fromJson({
      'memory_id': 'mem-2',
      'logical_memory_id': 'logical-1',
      'project_scope_id': 'scope-1',
      'kind': 'verified_fact',
      'subject_key': 'native_hook:libgame.so:tick',
      'statement': 'tick is hooked',
      'status': 'active',
      'confidence': 0.99,
      'locked': true,
      'version': 2,
      'supersedes_memory_id': 'mem-1',
      'verification': {
        'status': 'verified',
        'method': 'runtime_log',
        'evidence_refs': ['shadowhook.log'],
      },
      'artifact_refs': [
        {
          'module_name': 'libgame.so',
          'sha256': 'abc',
          'package_name': 'com.example.game',
          'apk_sha256': 'apk-hash',
          'build_id': 'build',
          'abi': 'arm64-v8a',
          'relative_offset': '0x100',
          'function_start': '0x100',
          'instruction_set': 'aarch64',
          'ida_analysis_status': 'complete',
          'ida_database_id': 'ida-db-1',
          'hot_update_status': 'not_detected',
          'shadowhook_status': 'verified',
          'ui_call_status': 'verified',
          'native_call_status': 'verified',
        },
      ],
    });

    expect(memory.version, 2);
    expect(memory.supersedesId, 'mem-1');
    expect(memory.verification.status, 'verified');
    expect(memory.verification.evidenceRefs, ['shadowhook.log']);
    expect(memory.artifacts.single.idaStatus, 'complete');
    expect(memory.artifacts.single.packageName, 'com.example.game');
    expect(memory.artifacts.single.apkSha256, 'apk-hash');
    expect(memory.artifacts.single.idaDatabaseId, 'ida-db-1');
    expect(memory.artifacts.single.hotUpdateStatus, 'not_detected');
    expect(memory.artifacts.single.shadowHookStatus, 'verified');
    expect(memory.artifacts.single.uiCallStatus, 'verified');
    expect(memory.artifacts.single.nativeCallStatus, 'verified');
  });

  test('parses resumable job checkpoint fields', () {
    final job = ProjectMemoryJobModel.fromJson({
      'job_id': 'job-1',
      'project_scope_id': 'scope-1',
      'status': 'failed',
      'checkpoint_cursor': 'task-42',
      'checkpoint_progress': 73,
      'checkpoint_at': '2026-08-05T12:00:00Z',
    });
    expect(job.checkpointCursor, 'task-42');
    expect(job.progress, 73);
    expect(job.checkpointAt, isNotNull);
  });

  test('parses detached project scope for offline server-side browsing', () {
    final overview = ProjectMemoryOverviewModel.fromJson({
      'scope': {
        'project_scope_id': 'scope-1',
        'machine_id': 'machine-1',
        'display_name': 'game',
        'status': 'detached',
        'revision': 8,
      },
      'brief': {'content': 'Project brief', 'source_revision': 8},
      'recent': [],
    });

    expect(overview.scope.status, 'detached');
    expect(overview.scope.revision, 8);
    expect(overview.brief?.content, 'Project brief');
  });
}
