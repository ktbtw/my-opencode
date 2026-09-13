import 'package:flutter/widgets.dart';
import 'attachment_drop_file.dart';

class AttachmentDropTarget extends StatelessWidget {
  final Widget child;
  final bool enabled;
  final Future<void> Function(List<AttachmentDropFile> files) onDrop;

  const AttachmentDropTarget({
    super.key,
    required this.child,
    required this.enabled,
    required this.onDrop,
  });

  @override
  Widget build(BuildContext context) => child;
}
