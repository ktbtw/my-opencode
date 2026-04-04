class AppConfig {
  const AppConfig._();

  static const apiBaseUrl = String.fromEnvironment(
    'CHAT_CODEX_API_BASE',
    defaultValue: 'http://127.0.0.1:8080',
  );
}
