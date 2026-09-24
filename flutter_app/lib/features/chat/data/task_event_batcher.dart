import 'dart:async';

/// Merge adjacent deltas before copying messages or parsing Markdown. Control
/// events are ordering barriers: flush text first, then deliver them immediately.
/// Bounded chunks also prevent a stalled frame from accumulating an entire reply.
Stream<Map<String, dynamic>> batchTaskDeltas(
  Stream<Map<String, dynamic>> source, {
  Duration interval = const Duration(milliseconds: 50),
  int maxCharacters = 16384,
  void Function(void Function())? onFlushReady,
}) {
  late StreamController<Map<String, dynamic>> controller;
  StreamSubscription<Map<String, dynamic>>? subscription;
  Timer? timer;
  Map<String, dynamic>? pending;
  var content = StringBuffer();
  void flush() {
    timer?.cancel();
    timer = null;
    if (pending == null) return;
    final event = pending!['type'] == 'delta'
        ? {...pending!, 'content': content.toString()}
        : pending!;
    pending = null;
    content = StringBuffer();
    controller.add(event);
  }

  controller = StreamController<Map<String, dynamic>>(
    sync: true,
    onListen: () {
      onFlushReady?.call(flush);
      subscription = source.listen(
        (event) {
          final toolKey = _runningToolKey(event);
          if (toolKey != null) {
            if (pending != null && _runningToolKey(pending!) != toolKey) {
              flush();
            }
            pending = event;
            timer ??= Timer(interval, flush);
            return;
          }
          if (event['type'] != 'delta') {
            flush();
            controller.add(event);
            return;
          }
          if (pending != null &&
              (pending!['type'] != 'delta' ||
                  pending!['field'] != event['field'])) {
            flush();
          }
          final firstAt =
              pending?['_first_delta_sent_at'] ??
              pending?['sent_at'] ??
              event['sent_at'];
          final firstMetadata =
              pending?['_first_delta_metadata'] ??
              pending?['metadata'] ??
              event['metadata'];
          pending = {
            ...event,
            '_first_delta_sent_at': firstAt,
            if (firstMetadata != null) '_first_delta_metadata': firstMetadata,
          };
          content.write(event['content'] as String? ?? '');
          if (content.length >= maxCharacters) {
            flush();
          } else {
            timer ??= Timer(interval, flush);
          }
        },
        onError: (Object error, StackTrace stack) {
          flush();
          controller.addError(error, stack);
        },
        onDone: () {
          flush();
          unawaited(controller.close());
        },
      );
    },
    onCancel: () async {
      timer?.cancel();
      pending = null;
      content = StringBuffer();
      await subscription?.cancel();
    },
    onPause: () => subscription?.pause(),
    onResume: () => subscription?.resume(),
  );
  return controller.stream;
}

String? _runningToolKey(Map<String, dynamic> event) {
  if (event['type'] != 'tool_updated') return null;
  final tool = event['tool'];
  if (tool is! Map || tool['status'] != 'running') return null;
  final id = (tool['call_id'] ?? tool['id'])?.toString() ?? '';
  return id.isEmpty ? null : id;
}
