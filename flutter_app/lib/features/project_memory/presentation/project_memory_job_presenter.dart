import '../data/project_memory_model.dart';

String projectMemoryEventLabel(String type) => switch (type) {
  'queued' => '等待调度',
  'manual_attached' => '等待当前整理任务',
  'dispatched' || 'accepted' => '读取项目证据',
  'sources' => '校验来源文件',
  'progress' => '分析中',
  'checkpoint' => '保存检查点',
  'candidates' => '合并候选记忆',
  'retrying' => '正在重试',
  'completed' => '已完成',
  'failed' => '失败',
  'cancelled' => '已取消',
  _ => '整理中',
};

String projectMemoryEventMessage(ProjectMemoryJobEventModel event) {
  final message = event.message.trim();
  if (message.isEmpty) {
    return switch (event.type) {
      'queued' => '项目记忆整理任务已进入队列',
      'dispatched' => '整理任务已下发到项目 Agent',
      'accepted' => '已接受整理任务',
      'progress' => '正在分析项目证据',
      'checkpoint' => '整理进度已保存',
      'completed' => '项目记忆整理完成',
      _ => '',
    };
  }

  return switch (message) {
    'Manual organization attached to active job' => '已关联正在执行的整理任务',
    'Manual project memory organization queued' => '项目记忆整理任务已进入队列',
    'Project memory job dispatched' => '整理任务已下发到项目 Agent',
    'accepted' => '已接受整理任务',
    'running' => '正在分析项目证据',
    'source recheck completed' => '来源文件复查完成',
    'Deferred project memory source queued' => '已排队处理整理期间新增的项目内容',
    _ => _translateChangedCandidates(message),
  };
}

String _translateChangedCandidates(String message) {
  final match = RegExp(
    r'^(\d+) changed candidates deferred$',
  ).firstMatch(message);
  if (match == null) return message;
  return '已暂缓 ${match.group(1)} 条来源发生变化的候选记忆';
}
