String formatChatTaskError(String? raw) {
  final text = raw?.trim();
  if (text == null || text.isEmpty) return '任务执行失败';

  final normalized = _stripDetail(text);
  if (_isImage504(text)) {
    return '生图服务暂时超时（上游 504），系统已自动重试仍失败，请稍后重试或切换其他生图模型。';
  }
  if (_isImageTimeout(text)) {
    return '生图请求超时，系统已自动重试仍失败，请稍后重试。';
  }
  if (_isImageFailure(text)) {
    return '生图失败，系统已自动重试仍失败，请稍后重试。';
  }
  return normalized;
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
