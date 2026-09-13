class AppNotificationRedactor {
  AppNotificationRedactor._();

  static final RegExp _token = RegExp(
    r'(token|authorization|api[_-]?key|password|secret)\s*[:=]\s*[^\s,;]+',
    caseSensitive: false,
  );
  static final RegExp _unixPath = RegExp(
    r'(^|\s)/(?:[^\s/]+/){2,}[^\s,;]*',
    multiLine: true,
  );
  static final RegExp _windowsPath = RegExp(
    r'\b[A-Z]:\\(?:[^\s\\]+\\){1,}[^\s,;]*',
    caseSensitive: false,
  );

  static String text(String value) {
    var result = value.replaceAllMapped(
      _token,
      (match) => '${match.group(1)}=[已隐藏]',
    );
    result = result.replaceAllMapped(_windowsPath, (match) {
      final raw = match.group(0)!;
      final parts = raw.split('\\').where((part) => part.isNotEmpty).toList();
      return parts.isEmpty ? '[路径已隐藏]' : '…\\${parts.last}';
    });
    result = result.replaceAllMapped(_unixPath, (match) {
      final raw = match.group(0)!;
      final prefix = match.group(1) ?? '';
      final parts = raw.split('/').where((part) => part.isNotEmpty).toList();
      return parts.isEmpty ? '$prefix[路径已隐藏]' : '$prefix…/${parts.last}';
    });
    return result;
  }
}
