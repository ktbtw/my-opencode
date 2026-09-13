class UpdateLogEntry {
  final String time;
  final String batchId;
  final String level;
  final String event;
  final Map<String, dynamic> data;

  const UpdateLogEntry({
    required this.time,
    required this.batchId,
    required this.level,
    required this.event,
    required this.data,
  });

  Map<String, dynamic> toJson() => {
    'time': time,
    'batch_id': batchId,
    'level': level,
    'event': event,
    'data': data,
  };

  factory UpdateLogEntry.fromJson(Map<String, dynamic> json) {
    return UpdateLogEntry(
      time: json['time'] as String? ?? '',
      batchId: json['batch_id'] as String? ?? '',
      level: json['level'] as String? ?? 'info',
      event: json['event'] as String? ?? '',
      data: (json['data'] as Map?)?.cast<String, dynamic>() ?? const {},
    );
  }
}
