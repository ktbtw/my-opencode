import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'dart:convert';
import 'dart:async';
import 'package:crypto/crypto.dart';
import '../../core/storage/app_storage.dart';
import '../../core/services/account_scope.dart';
import '../../core/config/api_client.dart';

class AppSettings {
  static const String downloadDirectoryKey = 'downloadDirectory';

  /// SAF 授权目录（content:// URI）的可读名称，仅用于展示。
  static const String downloadDirectoryLabelKey = 'downloadDirectoryLabel';
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
  final bool continuousReply;

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

  /// SAF 授权目录的可读名称；缺失时给出占位文案，避免把 content:// 原始标识
  /// 直接暴露在界面上（用户看不懂，也没法据此判断位置）。
  String get downloadDirectoryLabel {
    final label = downloadDirectoryLabelValue?.trim() ?? '';
    if (label.isNotEmpty) return label;
    final raw = downloadDirectory?.trim() ?? '';
    if (raw.isEmpty) return '';
    if (raw.startsWith('content://')) return '已选择自定义目录';
    return raw;
  }

  final String? downloadDirectoryLabelValue;
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
    this.continuousReply = false,
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
    this.downloadDirectoryLabelValue,
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
    bool? continuousReply,
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
      continuousReply: continuousReply ?? this.continuousReply,
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
      downloadDirectoryLabelValue:
          downloadDirectoryLabelValue ?? this.downloadDirectoryLabelValue,
    );
  }
}

class SettingsNotifier extends StateNotifier<AppSettings> {
  static const _statisticsRemoteKeys = <String, String>{
    'showFirstTokenTime': AppSettings.showFirstTokenTimeSettingKey,
    'showTokenCount': AppSettings.showTokenCountSettingKey,
    'showWordCount': AppSettings.showWordCountSettingKey,
  };

  final String _identity;
  final Future<Map<String, dynamic>> Function()? _readRemote;
  final Future<void> Function(Map<String, dynamic>)? _saveRemote;
  final _localReady = Completer<void>();
  Future<void> _writes = Future.value();
  Future<void>? _syncing;
  final Map<String, int> _revisions = {};

  SettingsNotifier({
    Future<Map<String, dynamic>> Function()? readRemote,
    Future<void> Function(Map<String, dynamic>)? saveRemote,
  }) : _identity = AppStorage.initialized ? AppStorage.storageIdentity : '',
       _readRemote = readRemote,
       _saveRemote = saveRemote,
       super(const AppSettings()) {
    _load();
  }

  bool get _active =>
      mounted &&
      _identity == (AppStorage.initialized ? AppStorage.storageIdentity : '');
  String _scoped(String key) =>
      'statistics.${sha256.convert(utf8.encode(_identity))}.$key';
  Future<void> _enqueue(Future<void> Function() action) {
    final next = _writes.then((_) => action());
    _writes = next.catchError((Object _) {});
    return next;
  }

  Future<Map<String, dynamic>> _fetchStatistics() =>
      _readRemote?.call() ??
      ApiClient.get(
        '/api/agents/${AppSettings.userSettingsAgentId}/settings',
        expectedIdentity: _identity,
      );

  Future<void> _flushStatistics() async {
    if (_syncing != null) return _syncing;
    final future = _drainStatistics();
    _syncing = future;
    try {
      await future;
    } finally {
      _syncing = null;
    }
  }

  Future<void> _drainStatistics() async {
    final prefs = await SharedPreferences.getInstance();
    while (_active) {
      await _writes;
      final pending = _readPendingStatistics(prefs);
      if (pending.isEmpty || !_active) return;
      try {
        if (_saveRemote != null) {
          await _saveRemote(pending);
        } else {
          await ApiClient.post(
            '/api/agents/${AppSettings.userSettingsAgentId}/settings',
            pending,
            expectedIdentity: _identity,
          );
        }
      } catch (_) {
        return;
      }
      await _enqueue(() async {
        if (!_active) return;
        final current = _readPendingStatistics(prefs);
        for (final entry in pending.entries) {
          if (current[entry.key] == entry.value) current.remove(entry.key);
        }
        await _writePendingStatistics(prefs, current);
      });
    }
  }

  Future<void> _load() async {
    final prefs = await SharedPreferences.getInstance();
    if (!_active) {
      _localReady.complete();
      return;
    }
    // Migrate the old device-wide values once, without exposing them to the
    // next account signing in on the same device.
    const ownerKey = 'statistics_migration_owner_v1';
    if (!prefs.containsKey(ownerKey)) {
      await prefs.setString(ownerKey, _identity);
      for (final key in _statisticsRemoteKeys.keys) {
        final value = prefs.getBool(key);
        if (value != null) await prefs.setBool(_scoped(key), value);
      }
      final pending = prefs.getString(AppSettings.pendingStatisticsSettingsKey);
      if (pending != null) {
        await prefs.setString(
          _scoped(AppSettings.pendingStatisticsSettingsKey),
          pending,
        );
      }
    }
    if (!_active) {
      _localReady.complete();
      return;
    }
    state = AppSettings(
      markdownRender: prefs.getBool('markdownRender') ?? true,
      latexRender: prefs.getBool('latexRender') ?? true,
      mermaidRender: prefs.getBool('mermaidRender') ?? true,
      htmlPreview: prefs.getBool('htmlPreview') ?? true,
      continuousReply: prefs.getBool('continuousReply') ?? false,
      showFirstTokenTime: prefs.getBool(_scoped('showFirstTokenTime')) ?? false,
      showTokenCount: prefs.getBool(_scoped('showTokenCount')) ?? false,
      showWordCount: prefs.getBool(_scoped('showWordCount')) ?? false,
      pasteAsFile: prefs.getBool('pasteAsFile') ?? false,
      autoOpenRecentTask:
          prefs.getBool(AppSettings.autoOpenRecentTaskSettingKey) ?? false,
      emailNotificationEnabled:
          prefs.getBool(AppSettings.emailNotificationEnabledKey) ?? false,
      backgroundNotificationEnabled:
          prefs.getBool(AppSettings.backgroundNotificationEnabledKey) ?? true,
      downloadDirectory: prefs.getString(AppSettings.downloadDirectoryKey),
      downloadDirectoryLabelValue: prefs.getString(
        AppSettings.downloadDirectoryLabelKey,
      ),
    );

    _localReady.complete();
    final revisions = Map<String, int>.from(_revisions);
    try {
      final settings = await _fetchStatistics();
      if (!_active) return;
      await _enqueue(() async {
        if (!_active) return;
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
          if (pendingStatistics.containsKey(entry.value) ||
              _revisions[entry.key] != revisions[entry.key]) {
            continue;
          }
          final value = _nullableBool(settings[entry.value]);
          if (value == null) continue;
          statistics[entry.key] = value;
          await prefs.setBool(_scoped(entry.key), value);
        }
        await prefs.setBool(AppSettings.emailNotificationEnabledKey, enabled);
        if (!_active) return;
        statistics.removeWhere((key, _) => _revisions[key] != revisions[key]);
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
      });
    } catch (_) {}
    if (_active) await _flushStatistics();
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
    await _localReady.future;
    if (!_active) return;
    final next = !_get(key);
    state = _set(key, next);
    _revisions[key] = (_revisions[key] ?? 0) + 1;
    final remoteKey = _statisticsRemoteKeys[key];
    await _enqueue(() async {
      if (!_active) return;
      final prefs = await SharedPreferences.getInstance();
      final localKey = remoteKey != null
          ? _scoped(key)
          : key == 'autoOpenRecentTask'
          ? AppSettings.autoOpenRecentTaskSettingKey
          : key;
      await prefs.setBool(localKey, next);
      if (remoteKey != null) {
        final pending = _readPendingStatistics(prefs)
          ..[remoteKey] = next.toString();
        await _writePendingStatistics(prefs, pending);
      }
    });
    if (remoteKey != null && _active) await _flushStatistics();
  }

  Future<void> setDownloadDirectory(String? path, {String? label}) async {
    final prefs = await SharedPreferences.getInstance();
    final normalized = path?.trim();
    if (normalized == null || normalized.isEmpty) {
      await prefs.remove(AppSettings.downloadDirectoryKey);
      await prefs.remove(AppSettings.downloadDirectoryLabelKey);
      state = state.copyWith(downloadDirectory: null);
      return;
    }
    await prefs.setString(AppSettings.downloadDirectoryKey, normalized);
    final normalizedLabel = label?.trim() ?? '';
    if (normalizedLabel.isEmpty) {
      await prefs.remove(AppSettings.downloadDirectoryLabelKey);
    } else {
      await prefs.setString(
        AppSettings.downloadDirectoryLabelKey,
        normalizedLabel,
      );
    }
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
    'continuousReply' => state.continuousReply,
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
    'continuousReply' => state.copyWith(continuousReply: value),
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
    final raw = prefs.getString(
      _scoped(AppSettings.pendingStatisticsSettingsKey),
    );
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
      await prefs.remove(_scoped(AppSettings.pendingStatisticsSettingsKey));
      return;
    }
    await prefs.setString(
      _scoped(AppSettings.pendingStatisticsSettingsKey),
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

final settingsProvider = StateNotifierProvider<SettingsNotifier, AppSettings>((
  ref,
) {
  ref.watch(accountScopeProvider);
  return SettingsNotifier();
});
