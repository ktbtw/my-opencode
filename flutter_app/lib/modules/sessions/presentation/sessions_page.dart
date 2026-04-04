import 'package:flutter/material.dart';

import '../../../shared/widgets/panel_card.dart';

class SessionsPage extends StatelessWidget {
  const SessionsPage({super.key});

  @override
  Widget build(BuildContext context) {
    return const PanelCard(
      title: '会话',
      child: SizedBox(height: 260, child: Center(child: Text('会话'))),
    );
  }
}
