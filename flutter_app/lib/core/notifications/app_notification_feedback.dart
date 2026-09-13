import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'app_notification_controller.dart';
import 'app_notification_model.dart';

int _feedbackSequence = 0;

void showAppFeedback(
  BuildContext context, {
  required String message,
  String title = '提示',
  bool error = false,
  AppNotificationKind kind = AppNotificationKind.system,
}) {
  final notifications = ProviderScope.containerOf(
    context,
    listen: false,
  ).read(appNotificationControllerProvider.notifier);
  final operationId =
      'feedback:${DateTime.now().microsecondsSinceEpoch}:${_feedbackSequence++}';
  notifications.start(
    operationId: operationId,
    title: title,
    message: message,
    progressMode: AppNotificationProgressMode.none,
    displayStyle: AppNotificationDisplayStyle.compact,
    kind: kind,
  );
  if (error) {
    notifications.fail(operationId, title: title, error: message);
  } else {
    notifications.succeed(operationId, title: title, message: message);
  }
}
