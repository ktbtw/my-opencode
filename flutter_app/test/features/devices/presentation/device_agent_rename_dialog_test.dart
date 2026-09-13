import 'dart:async';

import 'package:chat_codex_app/features/devices/presentation/device_detail_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('Agent rename actions stay on one row on mobile', (tester) async {
    tester.view.physicalSize = const Size(320, 700);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            onPressed: () => unawaited(
              showAgentRenameDialog(
                context: context,
                initialName: '逆向专家安卓',
                projectDirectoryName: 'android-project',
              ),
            ),
            child: const Text('打开'),
          ),
        ),
      ),
    );
    await tester.tap(find.text('打开'));
    await tester.pumpAndSettle();

    final directoryY = tester.getCenter(find.text('使用目录名')).dy;
    final cancelY = tester.getCenter(find.text('取消')).dy;
    final saveY = tester.getCenter(find.text('保存')).dy;
    expect((directoryY - cancelY).abs(), lessThan(1));
    expect((cancelY - saveY).abs(), lessThan(1));
    expect(tester.takeException(), isNull);

    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
  });
}
