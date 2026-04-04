import 'package:flutter/material.dart';

import '../../../shared/widgets/panel_card.dart';

class ApprovalsPage extends StatelessWidget {
  const ApprovalsPage({super.key});

  @override
  Widget build(BuildContext context) {
    return const PanelCard(
      title: '审批',
      child: SizedBox(height: 260, child: Center(child: Text('审批'))),
    );
  }
}
