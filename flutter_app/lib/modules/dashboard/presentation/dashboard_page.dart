import 'package:flutter/material.dart';

import '../../../shared/widgets/panel_card.dart';

class DashboardPage extends StatelessWidget {
  const DashboardPage({super.key});

  @override
  Widget build(BuildContext context) {
    return const PanelCard(
      title: 'Dashboard',
      subtitle: '总览页后续接全局统计、任务趋势和审批队列。',
      child: SizedBox(
        height: 320,
        child: Center(
          child: Text('总览模块将在后续模块中接入真实统计数据。'),
        ),
      ),
    );
  }
}
