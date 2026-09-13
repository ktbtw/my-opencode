import 'package:chat_codex_app/core/theme/app_colors.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('code backgrounds stay off the text-selection blue', () {
    expect(AppColors.codeInlineBackground, isNot(AppColors.textSelection));
    expect(AppColors.codeBackground, isNot(AppColors.textSelection));
    expect(AppColors.codeInlineBackground, isNot(AppColors.primaryLight));
    expect(AppColors.codeText, isNot(AppColors.primary));
    expect(AppColors.textSelection.alpha, greaterThan(0x50));
  });
}
