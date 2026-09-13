import 'dart:async';

import 'package:chat_codex_app/core/notifications/app_notification_controller.dart';
import 'package:chat_codex_app/core/notifications/app_notification_model.dart';
import 'package:chat_codex_app/core/notifications/app_notification_repository.dart';
import 'package:chat_codex_app/features/chat/data/chat_model.dart';
import 'package:chat_codex_app/features/chat/presentation/artifact_download_notification.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('Agent artifact download reports progress and completion', () async {
    final controller = AppNotificationController(
      repository: _MemoryRepository(),
    );
    const artifact = AiArtifact(
      id: 'artifact-1',
      filename: 'delivery.zip',
      relativePath: '.chatcodex-artifacts/task-1/delivery.zip',
      mimeType: 'application/zip',
      sizeBytes: 100,
    );

    final saved = await downloadArtifactWithNotification(
      notifications: controller,
      taskId: 'task-1',
      artifact: artifact,
      runner: ({required taskId, required artifact, onReceiveProgress}) async {
        onReceiveProgress?.call(40, 100);
        onReceiveProgress?.call(100, 100);
        return '/downloads/delivery.zip';
      },
    );

    expect(saved, '/downloads/delivery.zip');
    expect(controller.state.active, hasLength(1));
    final record = controller.state.active.single;
    expect(record.status, AppNotificationStatus.succeeded);
    expect(record.progress, 1);
    expect(record.title, '文件下载完成');
    expect(record.stages.every((stage) => stage.completed), isTrue);
    controller.dispose();
  });

  test(
    'artifact progress does not reopen a manually collapsed notice',
    () async {
      final controller = AppNotificationController(
        repository: _MemoryRepository(),
        collapseDelay: const Duration(seconds: 30),
        archiveDelay: const Duration(seconds: 30),
      );
      const artifact = AiArtifact(
        id: 'artifact-2',
        filename: 'delivery.apk',
        relativePath: '.chatcodex-artifacts/task-2/delivery.apk',
        mimeType: 'application/vnd.android.package-archive',
        sizeBytes: 100,
      );
      final firstProgress = Completer<void>();
      final continueDownload = Completer<void>();

      final download = downloadArtifactWithNotification(
        notifications: controller,
        taskId: 'task-2',
        artifact: artifact,
        runner:
            ({required taskId, required artifact, onReceiveProgress}) async {
              onReceiveProgress?.call(40, 100);
              firstProgress.complete();
              await continueDownload.future;
              onReceiveProgress?.call(100, 100);
              return '/downloads/delivery.apk';
            },
      );

      await firstProgress.future;
      controller.collapse();
      continueDownload.complete();
      await download;

      expect(controller.state.collapsed, isTrue);
      expect(
        controller.state.active.single.status,
        AppNotificationStatus.succeeded,
      );
      controller.dispose();
    },
  );
}

class _MemoryRepository implements AppNotificationRepository {
  @override
  Future<AppNotificationSnapshot> load() async =>
      const AppNotificationSnapshot();

  @override
  Future<void> save(AppNotificationSnapshot snapshot) async {}
}
