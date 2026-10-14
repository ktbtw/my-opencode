import 'dart:async';
import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/config/sse_parser.dart';
import '../../../core/notifications/app_notification_controller.dart';
import '../../../core/notifications/app_notification_model.dart';
import '../../../core/services/app_live_events.dart';
import '../../../core/storage/app_storage.dart';

/// 「跑完但还没被查看过」的 Agent：machineId → {agentId}。
///
/// 信号有权威顺序，启发式只补空、不覆盖权威记录：
/// 1. **显式终态**（最权威）：`task.updated` 的 completed/failed/cancelled，
///    或 `chat-task:` 开头的任务完成通知 → 直接登记；
/// 2. **显式开工**：`task.updated` 的非终态，或快照里 `current_task_id` 非空
///    → 撤回完成并记下运行态（用户又发起了一轮）；
/// 3. **快照补判**（启发式，仅补空）：只用于 SSE 断连期间（后台/被杀）错过终态事件时，
///    发现"记着在跑、快照里已经没有任务"才登记；
/// 4. **掉线/停用**：只清运行态。权威登记过的完成不会因为"暂时看不见"被抹掉。
///
/// 登记与"哪个页面正开着"无关：事件是全局的，设备详情页没挂载也能登记；
/// 运行态与完成态都落盘，App 被杀重开后仍能恢复。用户进入该 Agent 的对话即清除。
class CompletedAgentsNotifier extends StateNotifier<Map<String, Set<String>>> {
  CompletedAgentsNotifier() : super(_readCompleted());

  static const _completedKey = 'completed_agents_v1';
  static const _runningKey = 'running_agents_v1';

  /// 终态：任务结束（成功、失败、取消都算"跑完了，去看结果"）。
  static const _terminalStatuses = {'completed', 'failed', 'cancelled'};

  /// 这些状态下 Agent 不可能持有任务。
  static const _offlineStatuses = {
    'offline',
    'stopped',
    'failed',
    'unknown',
    'disabled',
  };

  /// 正在跑的任务：machineId → {agentId: taskId}。
  /// 重连快照里没有任务了、但这里记着它在跑，就说明任务是在"看不见的时候"结束的。
  Map<String, Map<String, String>> _running = _readRunning();

  /// 某台设备上待查看的完成结果。
  Set<String> completedFor(String machineId) =>
      state[machineId.trim()] ?? const <String>{};

  /// 用户查看了这个 Agent 的对话：从「完成」回到「空闲」。
  void markViewed(String machineId, String agentId) {
    final machine = machineId.trim();
    final agent = agentId.trim();
    if (machine.isEmpty || agent.isEmpty) return;
    final current = state[machine];
    if (current == null || !current.contains(agent)) return;
    _writeCompleted({
      ...state,
      machine: {...current}..remove(agent),
    });
  }

  /// 「任务完成」通知（`chat-task:` + kind=agent + succeeded，metadata 带
  /// machine_id/agent_id）：与通知中心同源，且带游标补拉，后台漏掉的事件也能补上。
  /// 只在 App 运行期间新到的通知上登记，历史通知不补登记，避免冷启动刷出一堆旧完成。
  void handleNotification(AppNotificationRecord record) {
    if (record.kind != AppNotificationKind.agent) return;
    if (record.status != AppNotificationStatus.succeeded) return;
    if (!record.operationId.startsWith('chat-task:')) return;
    final machine = _text(record.metadata['machine_id']);
    final agent = _text(record.metadata['agent_id']);
    if (machine.isEmpty || agent.isEmpty) return;
    _setRunning(machine, agent, null);
    _addCompleted(machine, agent);
  }

  /// 实时事件入口（task.updated / 重连 snapshot）。
  void handleEvent(SseEvent event) {
    final Object? decoded;
    try {
      decoded = jsonDecode(event.data);
    } catch (_) {
      return;
    }
    if (decoded is! Map) return;
    switch (event.type) {
      case 'task.updated':
        _handleTask(decoded.cast<String, dynamic>());
      case 'snapshot':
        _handleSnapshot(decoded.cast<String, dynamic>());
    }
  }

  void _handleTask(Map<String, dynamic> data) {
    final machine = _text(data['machine_id']);
    final agent = _text(data['agent_id']);
    if (machine.isEmpty || agent.isEmpty) return;
    final status = _text(data['status']).toLowerCase();
    if (status.isEmpty) return;
    if (_terminalStatuses.contains(status)) {
      _setRunning(machine, agent, null);
      _addCompleted(machine, agent);
      return;
    }
    if (status == 'pending') return;
    // 又开工了：从「完成」撤回，并记下正在跑的任务。
    _setRunning(machine, agent, _text(data['task_id']));
    _removeCompleted(machine, agent);
  }

  void _handleSnapshot(Map<String, dynamic> data) {
    final devices = data['devices'];
    if (devices is! List) return;
    for (final device in devices) {
      if (device is! Map) continue;
      final machine = _text(device['machine_id']);
      if (machine.isEmpty) continue;
      final agents = device['agents'];
      if (agents is! List) continue;
      for (final entry in agents) {
        if (entry is! Map) continue;
        final agent = _text(entry['agent_id']);
        if (agent.isEmpty) continue;
        final online =
            entry['enabled'] != false &&
            !_offlineStatuses.contains(_text(entry['status']).toLowerCase());
        if (!online) {
          // 掉线/停用只清运行态：显式登记过的完成是权威记录，
          // 不因为设备一时看不见就被启发式抹掉（列表本来就按在线过滤）。
          _setRunning(machine, agent, null);
          continue;
        }
        final task = _text(entry['current_task_id']);
        if (task.isNotEmpty) {
          _setRunning(machine, agent, task);
          _removeCompleted(machine, agent);
          continue;
        }
        // 快照补判（仅补空）：SSE 断连期间错过终态事件时，
        // "记着在跑 + 现在没任务"才登记；权威记录不会走到这里。
        final wasRunning = _running[machine]?[agent] ?? '';
        if (wasRunning.isEmpty) continue;
        _setRunning(machine, agent, null);
        _addCompleted(machine, agent);
      }
    }
  }

  void _addCompleted(String machine, String agent) {
    final current = state[machine] ?? const <String>{};
    if (current.contains(agent)) return;
    _writeCompleted({
      ...state,
      machine: {...current, agent},
    });
  }

  void _removeCompleted(String machine, String agent) {
    final current = state[machine];
    if (current == null || !current.contains(agent)) return;
    _writeCompleted({
      ...state,
      machine: {...current}..remove(agent),
    });
  }

  void _setRunning(String machine, String agent, String? task) {
    final current = {...?_running[machine]};
    final taskId = task?.trim() ?? '';
    if (taskId.isEmpty) {
      if (current.remove(agent) == null) return;
    } else {
      if (current[agent] == taskId) return;
      current[agent] = taskId;
    }
    _running = {..._running, machine: current};
    _writeRunning();
  }

  void _writeCompleted(Map<String, Set<String>> next) {
    state = next;
    if (!AppStorage.initialized) return;
    unawaited(
      AppStorage.setString(
        _completedKey,
        jsonEncode({
          for (final entry in next.entries)
            if (entry.value.isNotEmpty) entry.key: entry.value.toList(),
        }),
      ),
    );
  }

  void _writeRunning() {
    if (!AppStorage.initialized) return;
    unawaited(
      AppStorage.setString(
        _runningKey,
        jsonEncode({
          for (final entry in _running.entries)
            if (entry.value.isNotEmpty) entry.key: entry.value,
        }),
      ),
    );
  }

  static String _text(Object? value) => value?.toString().trim() ?? '';

  static Map<String, Set<String>> _readCompleted() {
    final decoded = _decode(
      AppStorage.initialized ? AppStorage.getString(_completedKey) : null,
    );
    if (decoded == null) return <String, Set<String>>{};
    return {
      for (final entry in decoded.entries)
        if (entry.value is List && (entry.value as List).isNotEmpty)
          entry.key: {
            for (final item in entry.value as List)
              if (item.toString().trim().isNotEmpty) item.toString().trim(),
          },
    };
  }

  static Map<String, Map<String, String>> _readRunning() {
    final decoded = _decode(
      AppStorage.initialized ? AppStorage.getString(_runningKey) : null,
    );
    if (decoded == null) return <String, Map<String, String>>{};
    return {
      for (final entry in decoded.entries)
        if (entry.value is Map && (entry.value as Map).isNotEmpty)
          entry.key: {
            for (final item in (entry.value as Map).entries)
              if (item.value.toString().trim().isNotEmpty)
                item.key.toString(): item.value.toString().trim(),
          },
    };
  }

  static Map<String, dynamic>? _decode(String? raw) {
    final text = raw?.trim() ?? '';
    if (text.isEmpty) return null;
    try {
      final decoded = jsonDecode(text);
      if (decoded is! Map) return null;
      return decoded.cast<String, dynamic>();
    } catch (_) {
      return null;
    }
  }
}

/// 全局常驻（在 app 根部 watch）：即使设备详情页没打开，任务结束事件也会被登记。
final completedAgentsProvider =
    StateNotifierProvider<CompletedAgentsNotifier, Map<String, Set<String>>>((
      ref,
    ) {
      final notifier = CompletedAgentsNotifier();
      ref.listen<AsyncValue<SseEvent>>(appLiveStreamProvider, (_, next) {
        final event = next.valueOrNull;
        if (event != null) notifier.handleEvent(event);
      });
      // 通知中心里新出现的"任务完成"：第二信号，覆盖设备快照看不到该 Agent 的情况。
      var hydrated = false;
      final seen = <String>{};
      ref.listen<AppNotificationState>(appNotificationControllerProvider, (
        _,
        next,
      ) {
        final records = [...next.active, ...next.recent];
        if (!hydrated) {
          // 首次（本地水合 + 补拉）只记已见，不补登记历史完成。
          hydrated = true;
          seen.addAll(records.map((record) => record.id));
          return;
        }
        for (final record in records) {
          if (!seen.add(record.id)) continue;
          notifier.handleNotification(record);
        }
      });
      return notifier;
    });
