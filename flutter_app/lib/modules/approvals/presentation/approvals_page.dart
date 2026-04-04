import 'package:flutter/material.dart';

import '../../../shared/widgets/panel_card.dart';

class ApprovalsPage extends StatelessWidget {
  const ApprovalsPage({super.key});

  @override
  Widget build(BuildContext context) {
    return const PanelCard(
      title: 'Approvals',
      subtitle: '后续集中展示所有等待审批的危险操作。',
      child: SizedBox(
        height: 260,
        child: Center(
          child: Text('审批中心将在后续模块接入真实审批流。'),
        ),
      ),
    );
  }
}
