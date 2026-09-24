import 'dart:async';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Riverpod 2 retains a loading StreamProvider's subscription to complete its
/// `.future`. A quiet SSE may never emit that first value. Tie the transport
/// lease explicitly to disposal so leaving a page still releases the socket.
Stream<T> providerOwnedStream<T>(Ref ref, Stream<T> source) {
  late StreamController<T> controller;
  StreamSubscription<T>? subscription;
  var closed = false;
  Future<void> cancel() async {
    if (closed) return;
    closed = true;
    await subscription?.cancel();
    unawaited(controller.close());
  }

  controller = StreamController<T>(
    onListen: () {
      if (closed) return;
      subscription = source.listen(
        controller.add,
        onError: controller.addError,
        onDone: controller.close,
      );
    },
    onCancel: cancel,
  );
  ref.onDispose(() => unawaited(cancel()));
  return controller.stream;
}
