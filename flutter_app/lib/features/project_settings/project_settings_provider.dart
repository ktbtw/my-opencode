import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../core/services/account_scope.dart';

String projectSettingsScope({
  required String machineId,
  required String projectId,
  String projectRoot = '',
  String projectScopeId = '',
}) => jsonEncode(
  projectScopeId.trim().isNotEmpty
      ? ['scope', projectScopeId.trim()]
      : [
          'project',
          machineId,
          projectRoot.trim().isNotEmpty ? projectRoot : projectId,
        ],
);

class ProjectSettings {
  final bool showConnectionStatus;
  final String? downloadDirectory;
  final String? directoryLabel;
  final bool loaded;

  const ProjectSettings({
    this.showConnectionStatus = true,
    this.downloadDirectory,
    this.directoryLabel,
    this.loaded = false,
  });

  static String storageKey(String identity, String project) =>
      'project_preferences.v1.${sha256.convert(utf8.encode(jsonEncode([identity, project])))}';

  static ProjectSettings read(
    SharedPreferences prefs,
    String identity,
    String project,
  ) {
    try {
      final value = jsonDecode(
        prefs.getString(storageKey(identity, project)) ?? '{}',
      );
      if (value is Map) {
        return ProjectSettings(
          showConnectionStatus: value['show_connection'] != false,
          downloadDirectory: value['download_directory'] as String?,
          directoryLabel: value['directory_label'] as String?,
          loaded: true,
        );
      }
    } catch (_) {}
    return const ProjectSettings(loaded: true);
  }
}

class ProjectSettingsNotifier extends StateNotifier<ProjectSettings> {
  final String identity;
  final String project;
  late final Future<void> ready;
  Future<void> _writes = Future.value();

  ProjectSettingsNotifier(this.identity, this.project)
    : super(const ProjectSettings()) {
    ready = _load();
  }

  Future<void> _load() async {
    final prefs = await SharedPreferences.getInstance();
    if (mounted) state = ProjectSettings.read(prefs, identity, project);
  }

  Future<void> update({
    bool? showConnectionStatus,
    String? directory,
    String? label,
    bool resetDirectory = false,
  }) {
    final next = _writes.then((_) async {
      await ready;
      if (!mounted) return;
      final value = ProjectSettings(
        showConnectionStatus:
            showConnectionStatus ?? state.showConnectionStatus,
        downloadDirectory: resetDirectory
            ? null
            : directory ?? state.downloadDirectory,
        directoryLabel: resetDirectory ? null : label ?? state.directoryLabel,
        loaded: true,
      );
      final prefs = await SharedPreferences.getInstance();
      if (!mounted) return;
      final saved = await prefs.setString(
        ProjectSettings.storageKey(identity, project),
        jsonEncode({
          'show_connection': value.showConnectionStatus,
          'download_directory': value.downloadDirectory,
          'directory_label': value.directoryLabel,
        }),
      );
      if (!saved) throw StateError('项目设置保存失败，请重试');
      if (mounted) state = value;
    });
    _writes = next.catchError((Object _) {});
    return next;
  }
}

final projectSettingsProvider = StateNotifierProvider.autoDispose
    .family<ProjectSettingsNotifier, ProjectSettings, String>((ref, project) {
      return ProjectSettingsNotifier(ref.watch(accountScopeProvider), project);
    });

class ProjectPreferencesScope extends InheritedWidget {
  final String project;
  const ProjectPreferencesScope({
    super.key,
    required this.project,
    required super.child,
  });
  static String? of(BuildContext context) => context
      .dependOnInheritedWidgetOfExactType<ProjectPreferencesScope>()
      ?.project;
  @override
  bool updateShouldNotify(ProjectPreferencesScope oldWidget) =>
      project != oldWidget.project;
}
