import '../../../core/notifications/app_notification_controller.dart';
import '../../../core/notifications/app_notification_model.dart';
import '../data/artifact_download_service.dart';
import '../data/chat_model.dart';

typedef ArtifactDownloadRunner =
    Future<String?> Function({
      required String taskId,
      required AiArtifact artifact,
      void Function(int received, int total)? onReceiveProgress,
    });

Future<String?> downloadArtifactWithNotification({
  required AppNotificationController notifications,
  required String taskId,
  required AiArtifact artifact,
  ArtifactDownloadRunner? runner,
}) async {
  final operationId =
      'artifact:download:$taskId:${artifact.id}:${DateTime.now().microsecondsSinceEpoch}';
  const preparingStages = [
    AppNotificationStage(label: '连接文件服务', active: true),
    AppNotificationStage(label: '下载文件'),
    AppNotificationStage(label: '保存到本机'),
  ];
  notifications.start(
    operationId: operationId,
    title: '正在下载 ${artifact.filename}',
    message: '正在连接文件服务',
    progressMode: AppNotificationProgressMode.indeterminate,
    displayStyle: AppNotificationDisplayStyle.linear,
    kind: AppNotificationKind.file,
    scope: AppNotificationScope.local,
    stages: preparingStages,
  );

  try {
    final download = runner ?? ArtifactDownloadService.download;
    final saved = await download(
      taskId: taskId,
      artifact: artifact,
      onReceiveProgress: (received, total) {
        final complete = total > 0 && received >= total;
        final progress = total > 0
            ? (received / total * 0.9).clamp(0.0, 0.9).toDouble()
            : null;
        notifications.update(
          operationId: operationId,
          message: complete ? '文件已下载，正在保存到本机' : '正在接收 ${artifact.filename}',
          progressMode: total > 0
              ? AppNotificationProgressMode.determinate
              : AppNotificationProgressMode.indeterminate,
          displayStyle: AppNotificationDisplayStyle.linear,
          progress: progress,
          stages: [
            const AppNotificationStage(label: '连接文件服务', completed: true),
            AppNotificationStage(
              label: '下载文件',
              completed: complete,
              active: !complete,
            ),
            AppNotificationStage(label: '保存到本机', active: complete),
          ],
        );
      },
    );
    notifications.succeed(
      operationId,
      title: saved == null ? '文件下载已开始' : '文件下载完成',
      message: saved == null
          ? '${artifact.filename} 已交给浏览器下载'
          : '${artifact.filename} 已保存',
    );
    return saved;
  } catch (error) {
    notifications.fail(
      operationId,
      title: '文件下载失败',
      error: error,
      errorCode: 'artifact_download_failed',
    );
    rethrow;
  }
}
