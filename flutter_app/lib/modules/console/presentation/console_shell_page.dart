import 'package:flutter/material.dart';

import '../../../shared/widgets/panel_card.dart';
import '../../agents/presentation/agents_page.dart';
import '../../approvals/presentation/approvals_page.dart';
import '../../dashboard/presentation/dashboard_page.dart';
import '../../sessions/presentation/sessions_page.dart';
import '../../tasks/presentation/tasks_page.dart';

enum ConsoleTab {
  dashboard('总览', Icons.space_dashboard_outlined),
  agents('Agents', Icons.memory_outlined),
  tasks('Tasks', Icons.task_alt_outlined),
  sessions('Sessions', Icons.chat_bubble_outline),
  approvals('Approvals', Icons.admin_panel_settings_outlined);

  const ConsoleTab(this.label, this.icon);

  final String label;
  final IconData icon;
}

class ConsoleShellPage extends StatefulWidget {
  const ConsoleShellPage({super.key});

  @override
  State<ConsoleShellPage> createState() => _ConsoleShellPageState();
}

class _ConsoleShellPageState extends State<ConsoleShellPage> {
  ConsoleTab current = ConsoleTab.agents;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: DecoratedBox(
        decoration: const BoxDecoration(
          gradient: LinearGradient(
            begin: Alignment.topLeft,
            end: Alignment.bottomRight,
            colors: [
              Color(0xFFECE8DE),
              Color(0xFFF6F3EB),
              Color(0xFFEAEFE8),
            ],
          ),
        ),
        child: SafeArea(
          child: LayoutBuilder(
            builder: (context, constraints) {
              final isMobile = constraints.maxWidth < 720;
              final isTablet = constraints.maxWidth >= 720 && constraints.maxWidth < 1180;

              return Column(
                children: [
                  Padding(
                    padding: const EdgeInsets.fromLTRB(20, 18, 20, 12),
                    child: PanelCard(
                      title: 'Chat Codex Control Surface',
                      subtitle: '不是聊天页，而是多机多 agent 的调度控制台。第一模块先落地 ConsoleShell 与 Agents。',
                      child: Row(
                        children: [
                          Expanded(
                            child: Wrap(
                              spacing: 12,
                              runSpacing: 12,
                              children: const [
                                _StatChip(label: 'Mode', value: 'Web Console'),
                                _StatChip(label: 'Layout', value: 'Responsive'),
                                _StatChip(label: 'Primary Unit', value: 'Agent'),
                              ],
                            ),
                          ),
                          if (!isMobile)
                            FilledButton.tonal(
                              onPressed: () {
                                setState(() => current = ConsoleTab.agents);
                              },
                              child: const Text('进入 Agents'),
                            ),
                        ],
                      ),
                    ),
                  ),
                  Expanded(
                    child: isMobile
                        ? _MobileShell(
                            current: current,
                            onSelect: (value) => setState(() => current = value),
                            child: _buildBody(),
                          )
                        : Row(
                            children: [
                              Padding(
                                padding: const EdgeInsets.fromLTRB(20, 0, 16, 20),
                                child: _Sidebar(
                                  extended: !isTablet,
                                  current: current,
                                  onSelect: (value) => setState(() => current = value),
                                ),
                              ),
                              Expanded(
                                child: Padding(
                                  padding: const EdgeInsets.fromLTRB(0, 0, 20, 20),
                                  child: _buildBody(),
                                ),
                              ),
                            ],
                          ),
                  ),
                ],
              );
            },
          ),
        ),
      ),
    );
  }

  Widget _buildBody() {
    switch (current) {
      case ConsoleTab.dashboard:
        return const DashboardPage();
      case ConsoleTab.agents:
        return const AgentsPage();
      case ConsoleTab.tasks:
        return const TasksPage();
      case ConsoleTab.sessions:
        return const SessionsPage();
      case ConsoleTab.approvals:
        return const ApprovalsPage();
    }
  }
}

class _Sidebar extends StatelessWidget {
  const _Sidebar({
    required this.extended,
    required this.current,
    required this.onSelect,
  });

  final bool extended;
  final ConsoleTab current;
  final ValueChanged<ConsoleTab> onSelect;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: double.infinity,
      child: PanelCard(
        title: 'Console',
        subtitle: '模块化控制面板',
        expandChild: true,
        child: NavigationRail(
          extended: extended,
          selectedIndex: ConsoleTab.values.indexOf(current),
          onDestinationSelected: (index) => onSelect(ConsoleTab.values[index]),
          labelType: extended ? NavigationRailLabelType.none : NavigationRailLabelType.all,
          destinations: ConsoleTab.values
              .map(
                (tab) => NavigationRailDestination(
                  icon: Icon(tab.icon),
                  selectedIcon: Icon(tab.icon),
                  label: Text(tab.label),
                ),
              )
              .toList(),
        ),
      ),
    );
  }
}

class _MobileShell extends StatelessWidget {
  const _MobileShell({
    required this.current,
    required this.onSelect,
    required this.child,
  });

  final ConsoleTab current;
  final ValueChanged<ConsoleTab> onSelect;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        Expanded(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 12),
            child: child,
          ),
        ),
        NavigationBar(
          selectedIndex: ConsoleTab.values.indexOf(current),
          onDestinationSelected: (index) => onSelect(ConsoleTab.values[index]),
          destinations: ConsoleTab.values
              .map(
                (tab) => NavigationDestination(
                  icon: Icon(tab.icon),
                  label: tab.label,
                ),
              )
              .toList(),
        ),
      ],
    );
  }
}

class _StatChip extends StatelessWidget {
  const _StatChip({
    required this.label,
    required this.value,
  });

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      decoration: BoxDecoration(
        color: const Color(0xFFF7F3EA),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: const Color(0xFFD9D2C3)),
      ),
      child: RichText(
        text: TextSpan(
          style: theme.textTheme.bodyMedium,
          children: [
            TextSpan(text: '$label ', style: const TextStyle(fontWeight: FontWeight.w700)),
            TextSpan(text: value),
          ],
        ),
      ),
    );
  }
}
