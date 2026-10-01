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

  test('生图错误按原因区分文案', () {
    // 供应商协议不支持（发请求前就被拦下）。
    expect(
      formatChatTaskError(
        'Image generation is not supported for provider: 北洛生图',
      ),
      contains('当前供应商不支持生图'),
    );
    // 缺 key。
    expect(
      formatChatTaskError('OpenAI API key is required for image generation'),
      contains('没有可用的 API Key'),
    );
    // 额度不足（北洛生图会直接回这句）。
    expect(
      formatChatTaskError('生图失败：Insufficient credits'),
      contains('生图额度不足'),
    );
  });

  test('非生图场景的余额不足不显示生图文案', () {
    // 供应商拉取模型列表、普通对话返回的余额不足，与生图无关。
    expect(
      formatChatTaskError('账户余额不足，请充值后继续使用'),
      allOf(contains('供应商额度不足'), isNot(contains('生图'))),
    );
    expect(
      formatChatTaskError('INSUFFICIENT_BALANCE: insufficient balance'),
      allOf(contains('供应商额度不足'), isNot(contains('生图'))),
    );
  });

  test('其它错误保持原文，不做误改', () {
    expect(formatChatTaskError('agent busy: task_1'), 'agent busy: task_1');
    expect(formatChatTaskError(null), '任务执行失败');
    expect(formatChatTaskError('   '), '任务执行失败');
  });
}
