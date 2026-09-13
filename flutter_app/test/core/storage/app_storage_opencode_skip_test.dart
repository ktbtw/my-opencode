import 'package:chat_codex_app/core/storage/app_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('AppStorage stores skipped opencode version by machine id', () async {
    SharedPreferences.setMockInitialValues({});
    await AppStorage.init();

    expect(AppStorage.getSkippedOpencodeVersion('machine-a'), isNull);

    await AppStorage.setSkippedOpencodeVersion('machine-a', '1.15.45');
    expect(AppStorage.getSkippedOpencodeVersion('machine-a'), '1.15.45');

    await AppStorage.clearSkippedOpencodeVersion('machine-a');
    expect(AppStorage.getSkippedOpencodeVersion('machine-a'), isNull);
  });
}
