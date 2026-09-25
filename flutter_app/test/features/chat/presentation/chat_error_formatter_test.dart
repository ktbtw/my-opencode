import 'package:chat_codex_app/features/chat/presentation/chat_error_formatter.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('把供应商的附件不支持英文报错换成中文提示', () {
    expect(
      formatChatTaskError(
        "'file part media type application/octet-stream' functionality not supported.",
      ),
      contains('附件格式不被当前模型支持'),
    );
    expect(
      formatChatTaskError(
        'ERROR: Cannot read "a.png" (this model does not support image input). Inform the user.',
      ),
      contains('附件格式不被当前模型支持'),
    );
  });

  test('其它错误保持原文，不做误改', () {
    expect(formatChatTaskError('agent busy: task_1'), 'agent busy: task_1');
    expect(formatChatTaskError(null), '任务执行失败');
    expect(formatChatTaskError('   '), '任务执行失败');
  });
}
