import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/notifications/app_notification_controller.dart';
import '../data/artifact_download_service.dart';
import '../data/chat_model.dart';
import 'artifact_download_notification.dart';
import 'generated_image_viewer.dart';

/// 按需加载某张交付图片的字节；测试可注入假实现避免真实网络请求。
typedef ArtifactBytesLoader = Future<Uint8List?> Function(AiArtifact artifact);

/// 交付图片查看页：同一轮交付的多张图片放在同一个横向分页里，
/// 左右滑动即可切换上一张 / 下一张，不需要退出后重新点击。
class ArtifactImageViewerPage extends ConsumerWidget {
  final String? project;
  final String taskId;

  /// 同一轮交付里的全部图片，用于左右滑动切换。
  final List<AiArtifact> images;

  /// 打开时展示的图片下标。
  final int initialIndex;

  /// 点击的那张图片已经加载好的字节，避免打开时闪一下。
  final Uint8List? initialBytes;

  /// 测试注入用；为空时走真实下载通道。
  final ArtifactBytesLoader? bytesLoader;

  const ArtifactImageViewerPage({
    super.key,
    this.project,
    required this.taskId,
    required this.images,
    this.initialIndex = 0,
    this.initialBytes,
    this.bytesLoader,
  }) : assert(images.length > 0, '至少要有一张图片');

  /// 与网格卡片一致的 Hero 标识，保证从卡片放大时有过渡动画。
  static String heroTagFor(String taskId, String artifactId) =>
      'artifact-image-$taskId-$artifactId';

  @override
  Widget build(BuildContext context, WidgetRef ref) => GeneratedImageViewer(
    initialIndex: initialIndex,
    images: [
      for (var i = 0; i < images.length; i++)
        GeneratedImageItem(
          filename: images[i].filename,
          mimeType: images[i].mimeType,
          sizeBytes: images[i].sizeBytes,
          heroTag: heroTagFor(taskId, images[i].id),
          loadBytes: () async {
            if (i == initialIndex.clamp(0, images.length - 1) &&
                initialBytes != null) {
              return initialBytes;
            }
            if (bytesLoader != null) return bytesLoader!(images[i]);
            return ArtifactDownloadService.fetchBytes(
              taskId: taskId,
              artifact: images[i],
            );
          },
        ),
    ],
    onSave: (index, _) => downloadArtifactWithNotification(
      notifications: ref.read(appNotificationControllerProvider.notifier),
      taskId: taskId,
      artifact: images[index],
      project: project,
    ),
  );
}
