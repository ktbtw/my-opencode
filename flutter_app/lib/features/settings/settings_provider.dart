import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'dart:convert';
import '../../core/config/api_client.dart';

class AppSettings {
  static const String downloadDirectoryKey = 'downloadDirectory';
  static const String emailNotificationEnabledKey = 'emailNotificationEnabled';
  static const String userSettingsAgentId = '__user__';
  static const String emailNotificationSettingKey =
      'email_notification_enabled';
  static const String globalPromptSettingKey = 'global_prompt';
  static const String projectMemoryEnabledSettingKey = 'project_memory_enabled';
  static const String chatQueueContinueAfterCancelSettingKey =
      'chat_queue_continue_after_cancel';
  static const String subagentOrchestrationEnabledSettingKey =
      'subagent_orchestration_enabled';
  static const String subagentOrchestrationMaxConcurrentSettingKey =
      'subagent_orchestration_max_concurrent';
  static const String subagentOrchestrationRoleModelsSettingKey =
      'subagent_orchestration_role_models';
  static const String showFirstTokenTimeSettingKey = 'show_first_token_time';
  static const String showTokenCountSettingKey = 'show_token_count';
  static const String showWordCountSettingKey = 'show_word_count';
  static const String pendingStatisticsSettingsKey =
      'pending_statistics_settings';
  static const String autoOpenRecentTaskSettingKey =
      'auto_open_recent_task_on_start';
  static const String backgroundNotificationEnabledKey =
      'background_notification_enabled_v1';

  // 展示类
  final bool markdownRender;
  final bool latexRender;
  final bool mermaidRender;
  final bool htmlPreview;

  // 统计类
  final bool showFirstTokenTime;
  final bool showTokenCount;
  final bool showWordCount;

  // 输入类
  final bool pasteAsFile;
  final bool chatQueueContinueAfterCancel;
  final bool autoOpenRecentTask;
  final bool subagentOrchestrationEnabled;
  final int subagentOrchestrationMaxConcurrent;
  final Map<String, dynamic> subagentOrchestrationRoleModels;
  final String? downloadDirectory;
  static const int pasteAsFileThreshold = 500;

  // 通知类
  final bool emailNotificationEnabled;
  final bool backgroundNotificationEnabled;

  // 提示词
  final String globalPrompt;
  final bool projectMemoryEnabled;

  static const _unset = Object();

  const AppSettings({
    this.markdownRender = true,
    this.latexRender = true,
    this.mermaidRender = true,
    this.htmlPreview = true,
    this.showFirstTokenTime = false,
    this.showTokenCount = false,
    this.showWordCount = false,
    this.pasteAsFile = false,
    this.chatQueueContinueAfterCancel = false,
    this.autoOpenRecentTask = false,
    this.subagentOrchestrationEnabled = true,
    this.subagentOrchestrationMaxConcurrent = 5,
    this.subagentOrchestrationRoleModels = const {},
    this.downloadDirectory,
    this.emailNotificationEnabled = false,
    this.backgroundNotificationEnabled = true,
    this.globalPrompt = '',
    this.projectMemoryEnabled = true,
  });

  bool get hasCustomDownloadDirectory =>
      downloadDirectory != null && downloadDirectory!.trim().isNotEmpty;

  AppSettings copyWith({
    bool? markdownRender,
    bool? latexRender,
    bool? mermaidRender,
    bool? htmlPreview,
    bool? showFirstTokenTime,
    bool? showTokenCount,
    bool? showWordCount,
    bool? pasteAsFile,
    bool? chatQueueContinueAfterCancel,
    bool? autoOpenRecentTask,
    bool? subagentOrchestrationEnabled,
    int? subagentOrchestrationMaxConcurrent,
    Map<String, dynamic>? subagentOrchestrationRoleModels,
    bool? emailNotificationEnabled,
    bool? backgroundNotificationEnabled,
    String? globalPrompt,
    bool? projectMemoryEnabled,
    Object? downloadDirectory = _unset,
  }) {
    return AppSettings(
      markdownRender: markdownRender ?? this.markdownRender,
      latexRender: latexRender ?? this.latexRender,
      mermaidRender: mermaidRender ?? this.mermaidRender,
      htmlPreview: htmlPreview ?? this.htmlPreview,
      showFirstTokenTime: showFirstTokenTime ?? this.showFirstTokenTime,
      showTokenCount: showTokenCount ?? this.showTokenCount,
      showWordCount: showWordCount ?? this.showWordCount,
      pasteAsFile: pasteAsFile ?? this.pasteAsFile,
      chatQueueContinueAfterCancel:
          chatQueueContinueAfterCancel ?? this.chatQueueContinueAfterCancel,
      autoOpenRecentTask: autoOpenRecentTask ?? this.autoOpenRecentTask,
      subagentOrchestrationEnabled:
          subagentOrchestrationEnabled ?? this.subagentOrchestrationEnabled,
      subagentOrchestrationMaxConcurrent:
          subagentOrchestrationMaxConcurrent ??
          this.subagentOrchestrationMaxConcurrent,
      subagentOrchestrationRoleModels:
          subagentOrchestrationRoleModels ??
          this.subagentOrchestrationRoleModels,
      emailNotificationEnabled:
          emailNotificationEnabled ?? this.emailNotificationEnabled,
      backgroundNotificationEnabled:
          backgroundNotificationEnabled ?? this.backgroundNotificationEnabled,
      globalPrompt: globalPrompt ?? this.globalPrompt,
      projectMemoryEnabled: projectMemoryEnabled ?? this.projectMemoryEnabled,
      downloadDirectory: identical(downloadDirectory, _unset)
          ? this.downloadDirectory
          : downloadDirectory as String?,
    );
  }
}

class SettingsNotifier extends StateNotifier<AppSettings> {
  static const _statisticsRemoteKeys = <String, String>{
    'showFirstTokenTime': AppSettings.showFirstTokenTimeSettingKey,
    'showTokenCount': AppSettings.showTokenCountSettingKey,
    'showWordCount': AppSettings.showWordCountSettingKey,
  };

  SettingsNotifier() : super(const AppSettings()) {
    _load();
  }

  Future<void> _load() async {
    final prefs = await SharedPreferences.getInstance();
    state = AppSettings(
      markdownRender: prefs.getBool('markdownRender') ?? true,
      latexRender: prefs.getBool('latexRender') ?? true,
      mermaidRender: prefs.getBool('mermaidRender') ?? true,
      htmlPreview: prefs.getBool('htmlPreview') ?? true,
      showFirstTokenTime: prefs.getBool('showFirstTokenTime') ?? false,
      showTokenCount: prefs.getBool('showTokenCount') ?? false,
      showWordCount: prefs.getBool('showWordCount') ?? false,
      pasteAsFile: prefs.getBool('pasteAsFile') ?? false,
      autoOpenRecentTask:
          prefs.getBool(AppSettings.autoOpenRecentTaskSettingKey) ?? false,
      emailNotificationEnabled:
          prefs.getBool(AppSettings.emailNotificationEnabledKey) ?? false,
      backgroundNotificationEnabled:
          prefs.getBool(AppSettings.backgroundNotificationEnabledKey) ?? true,
      downloadDirectory: prefs.getString(AppSettings.downloadDirectoryKey),
    );

    try {
      final settings = await ApiClient.get(
        '/api/agents/${AppSettings.userSettingsAgentId}/settings',
      );
      final pendingStatistics = _readPendingStatistics(prefs);
      final enabled =
          settings[AppSettings.emailNotificationSettingKey] == 'true';
      final globalPrompt =
          (settings[AppSettings.globalPromptSettingKey] as String? ?? '')
              .trim();
      final projectMemoryEnabled =
          settings[AppSettings.projectMemoryEnabledSettingKey] != 'false';
      final continueAfterCancel =
          settings[AppSettings.chatQueueContinueAfterCancelSettingKey] ==
          'true';
      final orchestrationEnabled =
          settings[AppSettings.subagentOrchestrationEnabledSettingKey] !=
          'false';
      final orchestrationConcurrent = int.tryParse(
        settings[AppSettings.subagentOrchestrationMaxConcurrentSettingKey]
                as String? ??
            '',
      );
      final orchestrationRoleModels = _decodeRoleModels(
        settings[AppSettings.subagentOrchestrationRoleModelsSettingKey]
                as String? ??
            '',
      );
      final statistics = <String, bool>{};
      for (final entry in _statisticsRemoteKeys.entries) {
        if (pendingStatistics.containsKey(entry.value)) continue;
        final value = _nullableBool(settings[entry.value]);
        if (value == null) continue;
        statistics[entry.key] = value;
        await prefs.setBool(entry.key, value);
      }
      await prefs.setBool(AppSettings.emailNotificationEnabledKey, enabled);
      state = state.copyWith(
        emailNotificationEnabled: enabled,
        globalPrompt: globalPrompt,
        projectMemoryEnabled: projectMemoryEnabled,
        chatQueueContinueAfterCancel: continueAfterCancel,
        subagentOrchestrationEnabled: orchestrationEnabled,
        subagentOrchestrationMaxConcurrent: (orchestrationConcurrent ?? 5)
            .clamp(1, 5),
        subagentOrchestrationRoleModels: orchestrationRoleModels,
        showFirstTokenTime:
            statistics['showFirstTokenTime'] ?? state.showFirstTokenTime,
        showTokenCount: statistics['showTokenCount'] ?? state.showTokenCount,
        showWordCount: statistics['showWordCount'] ?? state.showWordCount,
      );
      if (pendingStatistics.isNotEmpty) {
        await ApiClient.post(
          '/api/agents/${AppSettings.userSettingsAgentId}/settings',
          pendingStatistics,
        );
        await prefs.remove(AppSettings.pendingStatisticsSettingsKey);
      }
    } catch (_) {}
  }

  Future<void> loadGlobalPrompt() async {
    try {
      final settings = await ApiClient.get(
        '/api/agents/${AppSettings.userSettingsAgentId}/settings',
      );
      state = state.copyWith(
        globalPrompt:
            (settings[AppSettings.globalPromptSettingKey] as String? ?? '')
                .trim(),
      );
    } catch (_) {}
  }

  Future<void> saveGlobalPrompt(String value) async {
    final normalized = value.trim();
    final previous = state.globalPrompt;
    state = state.copyWith(globalPrompt: normalized);

    try {
      await ApiClient.post(
        '/api/agents/${AppSettings.userSettingsAgentId}/settings',
        {AppSettings.globalPromptSettingKey: normalized},
      );
    } catch (e) {
      state = state.copyWith(globalPrompt: previous);
      rethrow;
    }
  }

  Future<void> setProjectMemoryEnabled(bool enabled) async {
    final previous = state.projectMemoryEnabled;
    state = state.copyWith(projectMemoryEnabled: enabled);
    try {
      await ApiClient.post(
        '/api/agents/${AppSettings.userSettingsAgentId}/settings',
        {AppSettings.projectMemoryEnabledSettingKey: enabled.toString()},
      );
    } catch (error) {
      state = state.copyWith(projectMemoryEnabled: previous);
      rethrow;
    }
  }

  Future<void> toggle(String key) async {
    final prefs = await SharedPreferences.getInstance();
    final current = _get(key);
    final next = !current;
    final localKey = key == 'autoOpenRecentTask'
        ? AppSettings.autoOpenRecentTaskSettingKey
        : key;
    await prefs.setBool(localKey, next);
    state = _set(key, next);
    final remoteKey = _statisticsRemoteKeys[key];
    if (remoteKey == null) return;

    final pending = _readPendingStatistics(prefs)
      ..[remoteKey] = next.toString();
    await _writePendingStatistics(prefs, pending);
    try {
      await ApiClient.post(
        '/api/agents/${AppSettings.userSettingsAgentId}/settings',
        {remoteKey: next.toString()},
      );
      pending.remove(remoteKey);
      await _writePendingStatistics(prefs, pending);
    } catch (_) {
      // Keep the local value and retry the account sync during the next load.
    }
  }

  Future<void> setDownloadDirectory(String? path) async {
    final prefs = await SharedPreferences.getInstance();
    final normalized = path?.trim();
    if (normalized == null || normalized.isEmpty) {
      await prefs.remove(AppSettings.downloadDirectoryKey);
      state = state.copyWith(downloadDirectory: null);
      return;
    }
    await prefs.setString(AppSettings.downloadDirectoryKey, normalized);
    state = state.copyWith(downloadDirectory: normalized);
  }

  Future<void> setEmailNotificationEnabled(bool enabled) async {
    final prefs = await SharedPreferences.getInstance();
    final previous = state.emailNotificationEnabled;

    await prefs.setBool(AppSettings.emailNotificationEnabledKey, enabled);
    state = state.copyWith(emailNotificationEnabled: enabled);

    try {
      await ApiClient.post(
        '/api/agents/${AppSettings.userSettingsAgentId}/settings',
        {AppSettings.emailNotificationSettingKey: enabled.toString()},
      );
    } catch (e) {
      await prefs.setBool(AppSettings.emailNotificationEnabledKey, previous);
      state = state.copyWith(emailNotificationEnabled: previous);
      rethrow;
    }
  }

  Future<void> setBackgroundNotificationEnabled(bool enabled) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(AppSettings.backgroundNotificationEnabledKey, enabled);
    state = state.copyWith(backgroundNotificationEnabled: enabled);

    // Restart Android notification service to apply new settings
    try {
      if (defaultTargetPlatform == TargetPlatform.android) {
        final channel = MethodChannel('chat_codex/global_overlay');
        // The same idempotent command applies the new preference to an
        // existing service and also starts it when notifications are enabled.
        await channel.invokeMethod('startTaskNotifications');
      }
    } catch (e) {
      // Ignore method channel errors on non-Android platforms
    }
  }

  Future<void> setChatQueueContinueAfterCancel(bool enabled) async {
    final previous = state.chatQueueContinueAfterCancel;
    state = state.copyWith(chatQueueContinueAfterCancel: enabled);
    try {
      await ApiClient.post(
        '/api/agents/${AppSettings.userSettingsAgentId}/settings',
        {
          AppSettings.chatQueueContinueAfterCancelSettingKey: enabled
              .toString(),
        },
      );
    } catch (e) {
      state = state.copyWith(chatQueueContinueAfterCancel: previous);
      rethrow;
    }
  }

  Future<void> setSubagentOrchestrationEnabled(bool enabled) async {
    final previous = state.subagentOrchestrationEnabled;
    state = state.copyWith(subagentOrchestrationEnabled: enabled);
    try {
      await ApiClient.post(
        '/api/agents/${AppSettings.userSettingsAgentId}/settings',
        {
          AppSettings.subagentOrchestrationEnabledSettingKey: enabled
              .toString(),
        },
      );
    } catch (error) {
      state = state.copyWith(subagentOrchestrationEnabled: previous);
      rethrow;
    }
  }

  Future<void> setSubagentOrchestrationMaxConcurrent(int value) async {
    final normalized = value.clamp(1, 5);
    final previous = state.subagentOrchestrationMaxConcurrent;
    state = state.copyWith(subagentOrchestrationMaxConcurrent: normalized);
    try {
      await ApiClient.post(
        '/api/agents/${AppSettings.userSettingsAgentId}/settings',
        {
          AppSettings.subagentOrchestrationMaxConcurrentSettingKey: normalized
              .toString(),
        },
      );
    } catch (error) {
      state = state.copyWith(subagentOrchestrationMaxConcurrent: previous);
      rethrow;
    }
  }

  Future<void> setSubagentOrchestrationRoleModels(
    Map<String, dynamic> value,
  ) async {
    final previous = state.subagentOrchestrationRoleModels;
    state = state.copyWith(subagentOrchestrationRoleModels: value);
    try {
      await ApiClient.post(
        '/api/agents/${AppSettings.userSettingsAgentId}/settings',
        {
          AppSettings.subagentOrchestrationRoleModelsSettingKey: jsonEncode(
            value,
          ),
        },
      );
    } catch (error) {
      state = state.copyWith(subagentOrchestrationRoleModels: previous);
      rethrow;
    }
  }

  bool _get(String key) => switch (key) {
    'markdownRender' => state.markdownRender,
    'latexRender' => state.latexRender,
    'mermaidRender' => state.mermaidRender,
    'htmlPreview' => state.htmlPreview,
    'showFirstTokenTime' => state.showFirstTokenTime,
    'showTokenCount' => state.showTokenCount,
    'showWordCount' => state.showWordCount,
    'pasteAsFile' => state.pasteAsFile,
    'autoOpenRecentTask' => state.autoOpenRecentTask,
    'emailNotificationEnabled' => state.emailNotificationEnabled,
    'chatQueueContinueAfterCancel' => state.chatQueueContinueAfterCancel,
    _ => false,
  };

  AppSettings _set(String key, bool value) => switch (key) {
    'markdownRender' => state.copyWith(markdownRender: value),
    'latexRender' => state.copyWith(latexRender: value),
    'mermaidRender' => state.copyWith(mermaidRender: value),
    'htmlPreview' => state.copyWith(htmlPreview: value),
    'showFirstTokenTime' => state.copyWith(showFirstTokenTime: value),
    'showTokenCount' => state.copyWith(showTokenCount: value),
    'showWordCount' => state.copyWith(showWordCount: value),
    'pasteAsFile' => state.copyWith(pasteAsFile: value),
    'autoOpenRecentTask' => state.copyWith(autoOpenRecentTask: value),
    'emailNotificationEnabled' => state.copyWith(
      emailNotificationEnabled: value,
    ),
    'chatQueueContinueAfterCancel' => state.copyWith(
      chatQueueContinueAfterCancel: value,
    ),
    _ => state,
  };

  Map<String, String> _readPendingStatistics(SharedPreferences prefs) {
    final raw = prefs.getString(AppSettings.pendingStatisticsSettingsKey);
    if (raw == null || raw.isEmpty) return <String, String>{};
    try {
      final decoded = jsonDecode(raw);
      if (decoded is Map) {
        return decoded.map(
          (key, value) => MapEntry(key.toString(), value.toString()),
        );
      }
    } catch (_) {}
    return <String, String>{};
  }

  Future<void> _writePendingStatistics(
    SharedPreferences prefs,
    Map<String, String> values,
  ) async {
    if (values.isEmpty) {
      await prefs.remove(AppSettings.pendingStatisticsSettingsKey);
      return;
    }
    await prefs.setString(
      AppSettings.pendingStatisticsSettingsKey,
      jsonEncode(values),
    );
  }

  bool? _nullableBool(Object? value) {
    if (value == null) return null;
    final normalized = value.toString().trim().toLowerCase();
    if (normalized == 'true') return true;
    if (normalized == 'false') return false;
    return null;
  }
}

Map<String, dynamic> _decodeRoleModels(String raw) {
  try {
    final decoded = jsonDecode(raw);
    if (decoded is Map) return Map<String, dynamic>.from(decoded);
  } catch (_) {}
  return const {};
}

final settingsProvider = StateNotifierProvider<SettingsNotifier, AppSettings>(
  (ref) => SettingsNotifier(),
);
