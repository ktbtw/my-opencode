import 'app_update_executor_models.dart';
import 'app_update_models.dart';

Future<AppUpdateExecutionResult> downloadAndLaunchAppUpdate({
  required AppUpdateInfo info,
  required String baseUrl,
  required String token,
  AppUpdateProgress? onProgress,
}) {
  throw UnsupportedError('当前平台不支持应用内安装更新');
}
