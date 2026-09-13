class AppVersionInfo {
  final String appName;
  final String packageName;
  final String version;
  final String buildNumber;

  const AppVersionInfo({
    required this.appName,
    required this.packageName,
    required this.version,
    required this.buildNumber,
  });

  String get versionLabel => '$version+$buildNumber';
}
