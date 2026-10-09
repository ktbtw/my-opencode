import 'package:chat_codex_app/core/services/app_updater.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const _info = AppUpdateInfo(
  version: '1.6.31',
  versionCode: 178,
  changelog: '',
  releases: [
    AppReleaseInfo(
      version: '1.6.31',
      versionCode: 178,
      releasedAt: '',
      items: ['修复移动端布局'],
    ),
  ],
  platform: 'android',
  asset: AppUpdateAsset(
    platform: 'android',
    filename: 'chat-codex-1.6.31.apk',
    url: '/api/app/download?v=178',
    sha256: '44b72807a26de524b469523d1c9ca5c255fc699b99ec2c84363b2ce758b6e30b',
    available: true,
  ),
);

Future<void> _openDialog(WidgetTester tester) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Builder(
        builder: (context) => TextButton(
          onPressed: () => showDialog<void>(
            context: context,
            builder: (_) => buildAppUpdateDialog(_info),
          ),
          child: const Text('打开'),
        ),
      ),
    ),
  );
  await tester.tap(find.text('打开'));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('update actions stay on one row on a narrow screen', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 700);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await _openDialog(tester);

    final historyY = tester.getCenter(find.byTooltip('更新历史')).dy;
    final laterY = tester.getCenter(find.text('稍后再说')).dy;
    final updateY = tester.getCenter(find.text('立即更新')).dy;
    expect((historyY - laterY).abs(), lessThan(1));
    expect((laterY - updateY).abs(), lessThan(1));
    expect(tester.takeException(), isNull);
  });

  testWidgets('dialog paints and keeps every action on a wide phone screen', (
    tester,
  ) async {
    // 1220x2712 @3x is a real Android phone (407 logical px wide). Past roughly
    // 360 logical px the dialog stops being width-constrained, so AlertDialog's
    // IntrinsicWidth queries child intrinsics. A LayoutBuilder in `actions`
    // cannot answer that query and throws in debug builds, leaving the dialog
    // pushed but never painted.
    tester.view.physicalSize = const Size(1220, 2712);
    tester.view.devicePixelRatio = 3.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await _openDialog(tester);

    expect(find.text('发现新版本'), findsOneWidget);
    expect(find.text('立即更新'), findsOneWidget);
    expect(find.text('稍后再说'), findsOneWidget);
    final historyAction =
        find.byTooltip('更新历史').evaluate().isNotEmpty ||
        find.text('历史记录').evaluate().isNotEmpty;
    expect(historyAction, isTrue);
    expect(tester.takeException(), isNull);
  });
}
