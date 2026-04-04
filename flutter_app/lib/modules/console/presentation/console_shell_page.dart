import 'package:flutter/material.dart';

import '../../../shared/widgets/panel_card.dart';
import '../../agents/presentation/agents_page.dart';
import '../../approvals/presentation/approvals_page.dart';
import '../../dashboard/presentation/dashboard_page.dart';
import '../../sessions/presentation/sessions_page.dart';
import '../../tasks/presentation/tasks_page.dart';

enum ConsoleTab {
  dashboard('总览', Icons.space_dashboard_outlined),
  agents('执行器', Icons.memory_outlined),
  tasks('任务', Icons.task_alt_outlined),
  sessions('会话', Icons.chat_bubble_outline),
  approvals('审批', Icons.admin_panel_settings_outlined);

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
            colors: [Color(0xFFECE8DE), Color(0xFFF6F3EB), Color(0xFFEAEFE8)],
          ),
        ),
        child: SafeArea(
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 1440),
              child: LayoutBuilder(
                builder: (context, constraints) {
                  final isMobile = constraints.maxWidth < 720;
                  final isTablet =
                      constraints.maxWidth >= 720 &&
                      constraints.maxWidth < 1180;

                  return Padding(
                    padding: const EdgeInsets.fromLTRB(20, 18, 20, 20),
                    child: Column(
                      children: [
                        Padding(
                          padding: const EdgeInsets.fromLTRB(4, 4, 4, 18),
                          child: Row(
                            children: [
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(
                                      '控制台',
                                      style: Theme.of(
                                        context,
                                      ).textTheme.headlineMedium,
                                    ),
                                    const SizedBox(height: 4),
                                    Text(
                                      '远程管理设备、执行器和任务',
                                      style: Theme.of(
                                        context,
                                      ).textTheme.bodyMedium,
                                    ),
                                  ],
                                ),
                              ),
                              if (!isMobile)
                                FilledButton.tonal(
                                  onPressed: () {
                                    setState(() => current = ConsoleTab.agents);
                                  },
                                  child: const Text('执行器'),
                                ),
                            ],
                          ),
                        ),
                        Expanded(
                          child: isMobile
                              ? _MobileShell(
                                  current: current,
                                  onSelect: (value) =>
                                      setState(() => current = value),
                                  child: _buildBody(),
                                )
                              : Row(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    SizedBox(
                                      width: isTablet ? 112 : 240,
                                      height: double.infinity,
                                      child: _Sidebar(
                                        extended: !isTablet,
                                        current: current,
                                        onSelect: (value) =>
                                            setState(() => current = value),
                                      ),
                                    ),
                                    const SizedBox(width: 20),
                                    Expanded(child: _buildBody()),
                                  ],
                                ),
                        ),
                      ],
                    ),
                  );
                },
              ),
            ),
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
    return PanelCard(
      title: '导航',
      expandChild: true,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          for (final tab in ConsoleTab.values) ...[
            _SidebarItem(
              tab: tab,
              selected: tab == current,
              extended: extended,
              onTap: () => onSelect(tab),
            ),
            if (tab != ConsoleTab.values.last) const SizedBox(height: 10),
          ],
          const Spacer(),
        ],
      ),
    );
  }
}

class _SidebarItem extends StatelessWidget {
  const _SidebarItem({
    required this.tab,
    required this.selected,
    required this.extended,
    required this.onTap,
  });

  final ConsoleTab tab;
  final bool selected;
  final bool extended;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return InkWell(
      borderRadius: BorderRadius.circular(18),
      onTap: onTap,
      child: Ink(
        width: double.infinity,
        padding: EdgeInsets.symmetric(
          horizontal: extended ? 12 : 0,
          vertical: 12,
        ),
        decoration: BoxDecoration(
          color: selected ? const Color(0xFFD7EBD8) : Colors.transparent,
          borderRadius: BorderRadius.circular(18),
        ),
        child: extended
            ? Row(
                children: [
                  Icon(tab.icon, size: 20),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(tab.label, style: theme.textTheme.titleMedium),
                  ),
                ],
              )
            : Column(
                children: [
                  Icon(tab.icon, size: 20),
                  const SizedBox(height: 8),
                  Text(
                    tab.label,
                    textAlign: TextAlign.center,
                    style: theme.textTheme.titleMedium,
                  ),
                ],
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
