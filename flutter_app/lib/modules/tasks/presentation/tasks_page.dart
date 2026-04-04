import 'package:flutter/material.dart';

import '../../../shared/widgets/panel_card.dart';

class TasksPage extends StatelessWidget {
  const TasksPage({super.key});

  @override
  Widget build(BuildContext context) {
    return const PanelCard(
      title: 'Tasks',
      subtitle: '后续这里会接任务表、任务详情和流式事件面板。',
      child: SizedBox(
        height: 260,
        child: Center(
          child: Text('任务模块将在后续阶段接入。'),
        ),
      ),
    );
  }
}
