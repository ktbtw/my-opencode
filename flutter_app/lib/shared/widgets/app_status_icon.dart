import 'package:flutter/cupertino.dart';
import 'package:flutter/material.dart';

import '../../core/theme/app_theme.dart';

/// 顶部状态栏使用轻量空心图标，尺寸以通知铃铛为基准。
/// 保留 IconTheme 的颜色与透明度，让禁用按钮和深色查看器正常显示。
class AppStatusIcon extends StatelessWidget {
  const AppStatusIcon(
    this.icon, {
    super.key,
    this.size = AppIconMetrics.statusBar,
    this.color,
  });

  final IconData icon;
  final double size;
  final Color? color;

  @override
  Widget build(BuildContext context) =>
      Icon(_outline(icon), size: size, color: color);

  static IconData _outline(IconData icon) => switch (icon) {
    Icons.arrow_back ||
    Icons.arrow_back_ios_new ||
    Icons.chevron_left_rounded => CupertinoIcons.chevron_back,
    Icons.history || Icons.history_outlined => CupertinoIcons.clock,
    Icons.settings_outlined => CupertinoIcons.gear,
    Icons.more_horiz_rounded ||
    Icons.more_horiz_outlined ||
    Icons.more_vert ||
    Icons.more_vert_rounded ||
    Icons.more_vert_outlined => CupertinoIcons.ellipsis,
    Icons.refresh ||
    Icons.refresh_rounded ||
    Icons.refresh_outlined => CupertinoIcons.arrow_clockwise,
    Icons.rotate_right_rounded => CupertinoIcons.rotate_right,
    Icons.add_rounded || Icons.add_outlined => CupertinoIcons.plus,
    Icons.add_comment_outlined => CupertinoIcons.plus_bubble,
    Icons.bubble_chart_outlined => CupertinoIcons.bubble_left_bubble_right,
    Icons.person_outline => CupertinoIcons.person,
    Icons.download_for_offline_outlined ||
    Icons.download_rounded => CupertinoIcons.arrow_down_to_line,
    Icons.upload_file_rounded ||
    Icons.upload_file_outlined => CupertinoIcons.arrow_up_doc,
    Icons.file_upload_outlined => CupertinoIcons.square_arrow_up,
    Icons.open_in_new_rounded => CupertinoIcons.arrow_up_right_square,
    Icons.edit_note_rounded || Icons.edit_outlined => CupertinoIcons.pencil,
    Icons.copy_all_rounded => CupertinoIcons.doc_on_doc,
    Icons.check_rounded => CupertinoIcons.checkmark,
    Icons.check_circle_outline_rounded => CupertinoIcons.checkmark_circle,
    Icons.close_rounded => CupertinoIcons.xmark,
    Icons.delete_outline_rounded => CupertinoIcons.trash,
    Icons.help_outline_rounded => CupertinoIcons.question_circle,
    Icons.search_rounded => CupertinoIcons.search,
    Icons.storefront_outlined => CupertinoIcons.bag,
    Icons.library_books_rounded => CupertinoIcons.book,
    Icons.sync_alt_rounded ||
    Icons.swap_horiz_rounded => CupertinoIcons.arrow_left_right,
    Icons.view_agenda_outlined => CupertinoIcons.rectangle_grid_1x2,
    Icons.tab_outlined => CupertinoIcons.square_on_square,
    Icons.folder_open_rounded || Icons.folder_outlined => CupertinoIcons.folder,
    Icons.folder_copy_outlined => CupertinoIcons.folder,
    Icons.tune_outlined => CupertinoIcons.slider_horizontal_3,
    Icons.computer_rounded => CupertinoIcons.desktopcomputer,
    Icons.smart_toy_outlined => CupertinoIcons.cube,
    Icons.chat_bubble_outline => CupertinoIcons.chat_bubble,
    Icons.account_tree_outlined => CupertinoIcons.flowchart,
    Icons.terminal_rounded => CupertinoIcons.chevron_left_slash_chevron_right,
    _ => icon,
  };
}

/// AppBar 默认返回按钮也使用同一套细线样式。
class AppStatusBackButton extends StatelessWidget {
  const AppStatusBackButton({super.key});

  @override
  Widget build(BuildContext context) => IconButton(
    tooltip: MaterialLocalizations.of(context).backButtonTooltip,
    icon: const AppStatusIcon(Icons.arrow_back),
    onPressed: () => Navigator.of(context).maybePop(),
  );
}
