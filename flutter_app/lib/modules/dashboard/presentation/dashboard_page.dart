import 'package:flutter/material.dart';

import '../../../shared/widgets/panel_card.dart';

class DashboardPage extends StatelessWidget {
  const DashboardPage({super.key});

  @override
  Widget build(BuildContext context) {
    return const PanelCard(
      title: '总览',
      child: SizedBox(height: 320, child: Center(child: Text('总览'))),
    );
  }
}
