String _settingText(dynamic value) => value?.toString() ?? '';

class ProjectMemorySettingsModel {
  final bool globalEnabled;
  final bool globalEnabledSet;
  final String globalModel;
  final String globalVariant;
  final bool? projectEnabled;
  final String projectModel;
  final String projectVariant;
  final bool effectiveEnabled;
  final String effectiveModel;
  final String effectiveVariant;
  final String enabledSource;
  final String modelSource;

  const ProjectMemorySettingsModel({
    required this.globalEnabled,
    required this.globalEnabledSet,
    this.globalModel = '',
    this.globalVariant = '',
    this.projectEnabled,
    this.projectModel = '',
    this.projectVariant = '',
    required this.effectiveEnabled,
    this.effectiveModel = '',
    this.effectiveVariant = '',
    this.enabledSource = 'default',
    this.modelSource = 'agent_default',
  });

  factory ProjectMemorySettingsModel.fromJson(Map<String, dynamic> json) {
    final global = json['global'] is Map
        ? (json['global'] as Map).cast<String, dynamic>()
        : const <String, dynamic>{};
    final project = json['project'] is Map
        ? (json['project'] as Map).cast<String, dynamic>()
        : const <String, dynamic>{};
    final effective = json['effective'] is Map
        ? (json['effective'] as Map).cast<String, dynamic>()
        : const <String, dynamic>{};
    final projectEnabled = project.containsKey('enabled')
        ? project['enabled'] == true
        : null;
    return ProjectMemorySettingsModel(
      globalEnabled: global['enabled'] != false,
      globalEnabledSet: global['enabled_set'] == true,
      globalModel: _settingText(global['model']),
      globalVariant: _settingText(global['variant']),
      projectEnabled: projectEnabled,
      projectModel: _settingText(project['model']),
      projectVariant: _settingText(project['variant']),
      effectiveEnabled: effective['enabled'] != false,
      effectiveModel: _settingText(effective['model']),
      effectiveVariant: _settingText(effective['variant']),
      enabledSource: _settingText(effective['enabled_source']),
      modelSource: _settingText(effective['model_source']),
    );
  }

  bool get followsGlobalEnabled => projectEnabled == null;
  bool get followsGlobalModel => projectModel.trim().isEmpty;
}
