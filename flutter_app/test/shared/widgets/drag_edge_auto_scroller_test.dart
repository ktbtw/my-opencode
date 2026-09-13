import 'package:chat_codex_app/shared/widgets/drag_edge_auto_scroller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('scrolls continuously near either edge and stops on request', (
    tester,
  ) async {
    final controller = ScrollController();
    final viewportKey = GlobalKey();
    final scroller = DragEdgeAutoScroller(
      controller: controller,
      viewportKey: viewportKey,
    );
    addTearDown(() {
      scroller.dispose();
      controller.dispose();
    });

    await tester.pumpWidget(
      MaterialApp(
        home: Center(
          child: SizedBox(
            height: 300,
            child: ListView(
              key: viewportKey,
              controller: controller,
              children: const [SizedBox(height: 1200)],
            ),
          ),
        ),
      ),
    );

    final viewport = tester.getRect(find.byKey(viewportKey));
    scroller.update(Offset(viewport.center.dx, viewport.bottom - 2));
    await tester.pump(const Duration(milliseconds: 320));
    expect(controller.offset, greaterThan(0));

    scroller.stop();
    final stoppedOffset = controller.offset;
    await tester.pump(const Duration(milliseconds: 160));
    expect(controller.offset, stoppedOffset);

    controller.jumpTo(300);
    scroller.update(Offset(viewport.center.dx, viewport.top + 2));
    await tester.pump(const Duration(milliseconds: 160));
    expect(controller.offset, lessThan(300));
    scroller.stop();
  });
}
