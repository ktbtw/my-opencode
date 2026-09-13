import 'app_update_executor_stub.dart'
    if (dart.library.io) 'app_update_executor_io.dart'
    as impl;
import 'app_update_executor_models.dart';
import 'app_update_models.dart';

export 'app_update_executor_models.dart';

Future<AppUpdateExecutionResult> downloadAndLaunchAppUpdate({
  required AppUpdateInfo info,
  required String baseUrl,
  required String token,
  AppUpdateProgress? onProgress,
}) {
  return impl.downloadAndLaunchAppUpdate(
    info: info,
    baseUrl: baseUrl,
    token: token,
    onProgress: onProgress,
  );
}
