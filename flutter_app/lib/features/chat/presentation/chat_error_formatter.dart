String formatChatTaskError(String? raw) {
  final text = raw?.trim();
  if (text == null || text.isEmpty) return '任务执行失败';

  final normalized = _stripDetail(text);
  // 生图前置检查失败：供应商协议不支持 / 没有可用 key。这两种都发生在请求
  // 发出之前，文案必须指向配置，而不是笼统的「生图失败」。
  if (_isImageProviderUnsupported(text)) {
    return '当前供应商不支持生图：生图需要走 OpenAI 协议的 /images 接口。请换用支持生图的供应商或模型。';
  }
  if (_isImageMissingKey(text)) {
    return '当前供应商没有可用的 API Key，无法生图。请在设备 AI 配置里为该供应商填写 Key 后重试。';
  }
  if (_isImageInsufficientCredits(text)) {
    return '生图额度不足（供应商返回 Insufficient credits）。请为该供应商充值或换用其他生图模型。';
  }
  if (_isInsufficientCredits(text)) {
    return '供应商额度不足，请求被拒绝。请为该供应商充值或更换供应商后重试。';
  }
  if (_isImage504(text)) {
    return '生图服务暂时超时（上游 504），系统已自动重试仍失败，请稍后重试或切换其他生图模型。';
  }
  if (_isImageTimeout(text)) {
    return '生图请求超时，系统已自动重试仍失败，请稍后重试。';
  }
  if (_isImageFailure(text)) {
    return '生图失败，系统已自动重试仍失败，请稍后重试。';
  }
  if (_isUnsupportedAttachment(text)) {
    return '附件格式不被当前模型支持，无法读取。请改用纯文本（.txt 等）附件，或更换模型后重试。';
  }
  return normalized;
}

// 模型无法消费附件媒体类型时，供应商返回的是一句英文（例如
// “'file part media type application/octet-stream' functionality not supported.”
// 或 “this model does not support image input”）。这里换成产品语种。
bool _isUnsupportedAttachment(String text) {
  final lower = text.toLowerCase();
  final unsupported =
      lower.contains('functionality not supported') ||
      lower.contains('not supported') ||
      lower.contains('does not support') ||
      lower.contains('unsupported');
  if (!unsupported) return false;
  return lower.contains('file part') ||
      lower.contains('media type') ||
      lower.contains('content part') ||
      lower.contains('this model does not support') ||
      lower.contains('附件');
}

String _stripDetail(String text) {
  final marker = text.indexOf('\ndetail:');
  if (marker <= 0) return text;
  final head = text.substring(0, marker).trim();
  return head.isEmpty ? text : head;
}

bool _isImage504(String text) {
  final lower = text.toLowerCase();
  return _isImageMessage(lower) &&
      (lower.contains('504') ||
          lower.contains('gateway time-out') ||
          lower.contains('gateway timeout'));
}

bool _isImageTimeout(String text) {
  final lower = text.toLowerCase();
  return _isImageMessage(lower) &&
      (lower.contains('timed out') ||
          lower.contains('timeout') ||
          lower.contains('fetch failed'));
}

bool _isImageFailure(String text) {
  final lower = text.toLowerCase();
  return _isImageMessage(lower) &&
      (lower.contains('image generation failed') ||
          lower.contains('生图失败') ||
          lower.contains('生图服务'));
}

bool _isImageMessage(String text) {
  return text.contains('/images/generations') ||
      text.contains('image generation') ||
      text.contains('gpt-image') ||
      text.contains('生图');
}

// 供应商协议不支持：opencode 在发请求前抛
// "Image generation is not supported for provider: xxx"。
bool _isImageProviderUnsupported(String text) {
  final lower = text.toLowerCase();
  return lower.contains('image generation is not supported') ||
      (lower.contains('not supported for provider') && lower.contains('image'));
}

// 缺 key："OpenAI API key is required for image generation"。
bool _isImageMissingKey(String text) {
  final lower = text.toLowerCase();
  return lower.contains('api key is required') ||
      (lower.contains('image') && lower.contains('api key'));
}

// 供应商额度不足：北洛等供应商会直接回 "Insufficient credits" / "余额不足"。
// 只有消息本身是生图请求时才显示「生图额度不足」——拉取模型列表、普通对话
// 返回的余额不足与生图无关，套上生图文案会误导用户。
bool _isImageInsufficientCredits(String text) {
  return _isInsufficientCredits(text) && _isImageMessage(text.toLowerCase());
}

bool _isInsufficientCredits(String text) {
  final lower = text.toLowerCase();
  return lower.contains('insufficient credits') ||
      lower.contains('insufficient_quota') ||
      lower.contains('insufficient balance') ||
      lower.contains('quota exceeded') ||
      lower.contains('余额不足') ||
      lower.contains('额度不足');
}
