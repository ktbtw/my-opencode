typedef AppUpdateProgress = void Function(int received, int total);

class AppUpdateExecutionResult {
  final String filePath;
  final String message;

  const AppUpdateExecutionResult({
    required this.filePath,
    required this.message,
  });
}
