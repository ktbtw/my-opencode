import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../settings/settings_provider.dart';
import '../data/artifact_download_saver.dart';
import '../data/chat_model.dart';
import 'generated_image_viewer.dart';

/// 应用内生成图片与交付图片使用同一套预览工具。
class InlineImageViewerPage extends StatelessWidget {
  final List<AiOutputFile> images;
  final int initialIndex;

  const InlineImageViewerPage({
    super.key,
    required this.images,
    this.initialIndex = 0,
  }) : assert(images.length > 0, '至少要有一张图片');

  @override
  Widget build(BuildContext context) => GeneratedImageViewer(
    initialIndex: initialIndex,
    images: [
      for (final file in images)
        GeneratedImageItem(
          filename: file.filename,
          mimeType: file.mimeType,
          loadBytes: () async {
            final local = file.imageBytes;
            if (local != null) return local;
            final response = await Dio().get<List<int>>(
              file.url,
              options: Options(responseType: ResponseType.bytes),
            );
            final data = response.data;
            return data == null ? null : Uint8List.fromList(data);
          },
        ),
    ],
    onSave: (index, bytes) async {
      final file = images[index];
      final prefs = await SharedPreferences.getInstance();
      final path = await saveArtifactBytes(
        bytes,
        filename: file.filename,
        mimeType: file.mimeType,
        preferredDirectory: prefs.getString(AppSettings.downloadDirectoryKey),
      );
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(path == null ? '已交给浏览器下载' : '已保存到 $path')),
      );
    },
  );
}
