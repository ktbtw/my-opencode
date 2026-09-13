import 'package:flutter/foundation.dart';

bool isAttachmentDropSupported({required bool isMobile}) {
  if (kIsWeb) return !isMobile;
  return switch (defaultTargetPlatform) {
    TargetPlatform.windows ||
    TargetPlatform.macOS ||
    TargetPlatform.linux => true,
    _ => false,
  };
}

bool get isClipboardFilePasteSupported {
  if (kIsWeb) return true;
  return switch (defaultTargetPlatform) {
    TargetPlatform.windows ||
    TargetPlatform.macOS ||
    TargetPlatform.linux => true,
    _ => false,
  };
}
