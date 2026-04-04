import 'package:flutter/material.dart';

import '../../../shared/widgets/panel_card.dart';

class SessionsPage extends StatelessWidget {
  const SessionsPage({super.key});

  @override
  Widget build(BuildContext context) {
    return const PanelCard(
      title: 'Sessions',
      subtitle: '后续这里展示会话列表、连续上下文和继续提问入口。',
      child: SizedBox(
        height: 260,
        child: Center(
          child: Text('会话模块将在后续阶段接入。'),
        ),
      ),
    );
  }
}
