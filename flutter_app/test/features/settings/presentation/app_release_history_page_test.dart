import 'package:chat_codex_app/core/services/app_update_models.dart';
import 'package:chat_codex_app/features/settings/presentation/app_release_history_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

void main() {
  testWidgets('renders versioned release content and refreshes', (
    tester,
  ) async {
    var loadCount = 0;
    final releases = [
      const AppReleaseInfo(
        version: '1.6.28',
        versionCode: 175,
        releasedAt: '2026-08-22',
        items: ['新增项目记忆设置'],
      ),
      const AppReleaseInfo(
        version: '1.6.27',
        versionCode: 174,
        releasedAt: '2026-08-21',
        items: ['优化表格渲染'],
      ),
    ];
    final router = GoRouter(
      initialLocation: '/history',
      routes: [
        GoRoute(
          path: '/history',
          builder: (_, __) => AppReleaseHistoryPage(
            loader: () async {
              loadCount++;
              return releases;
            },
          ),
        ),
      ],
    );
    addTearDown(router.dispose);

    await tester.pumpWidget(
      ProviderScope(child: MaterialApp.router(routerConfig: router)),
    );
    await tester.pumpAndSettle();

    expect(find.text('版本 1.6.28+175'), findsOneWidget);
    expect(find.text('新增项目记忆设置'), findsOneWidget);
    expect(find.text('当前版本'), findsOneWidget);
    expect(find.text('版本 1.6.27+174'), findsOneWidget);
    expect(loadCount, 1);

    await tester.tap(find.byTooltip('刷新更新历史'));
    await tester.pumpAndSettle();
    expect(loadCount, 2);
    expect(tester.takeException(), isNull);
  });
}
