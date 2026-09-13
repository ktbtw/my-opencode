// ignore_for_file: deprecated_member_use, avoid_web_libraries_in_flutter

import 'dart:html' as html;

Future<bool> triggerBrowserDownload(String url, {String? filename}) async {
  final anchor = html.AnchorElement(href: url)
    ..style.display = 'none'
    ..target = '_self';
  if (filename != null && filename.isNotEmpty) {
    anchor.download = filename;
  }
  html.document.body?.append(anchor);
  anchor.click();
  anchor.remove();
  return true;
}
