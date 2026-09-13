import 'app_version_models.dart';
import 'app_version_service_io.dart'
    if (dart.library.html) 'app_version_service_web.dart'
    as impl;

export 'app_version_models.dart';

class AppVersionService {
  static Future<AppVersionInfo> load() => impl.loadAppVersionInfo();
}
