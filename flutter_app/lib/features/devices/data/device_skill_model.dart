class DeviceSkillPackageFile {
  final String path;
  final String content;
  final bool executable;

  const DeviceSkillPackageFile({
    required this.path,
    this.content = '',
    this.executable = false,
  });

  factory DeviceSkillPackageFile.fromJson(Map<String, dynamic> json) {
    return DeviceSkillPackageFile(
      path: json['path'] as String? ?? '',
      content: json['content'] as String? ?? '',
      executable: json['executable'] as bool? ?? false,
    );
  }
}

class DeviceSkillImport {
  final String name;
  final String description;
  final String content;
  final List<DeviceSkillPackageFile> packageFiles;

  const DeviceSkillImport({
    required this.name,
    required this.content,
    this.description = '',
    this.packageFiles = const [],
  });

  Map<String, dynamic> toJson() {
    return {
      'name': name,
      'description': description,
      'content': content,
      if (packageFiles.isNotEmpty)
        'package_files': packageFiles
            .map(
              (file) => {
                'path': file.path,
                'content': file.content,
                if (file.executable) 'executable': true,
              },
            )
            .toList(),
    };
  }
}

class DeviceSkillCatalogItem {
  final String id;
  final String name;
  final String description;
  final String content;
  final String category;
  final String source;
  final List<String> tags;
  final bool enabled;
  final List<DeviceSkillPackageFile> packageFiles;

  const DeviceSkillCatalogItem({
    required this.id,
    required this.name,
    this.description = '',
    this.content = '',
    this.category = '',
    this.source = '',
    this.tags = const [],
    this.enabled = true,
    this.packageFiles = const [],
  });

  factory DeviceSkillCatalogItem.fromJson(Map<String, dynamic> json) {
    return DeviceSkillCatalogItem(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      description: json['description'] as String? ?? '',
      content: json['content'] as String? ?? '',
      category: json['category'] as String? ?? '',
      source: json['source'] as String? ?? '',
      tags: (json['tags'] as List<dynamic>? ?? const [])
          .map((item) => item.toString())
          .toList(),
      enabled: json['enabled'] as bool? ?? true,
      packageFiles: (json['package_files'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map(
            (item) =>
                DeviceSkillPackageFile.fromJson(item.cast<String, dynamic>()),
          )
          .toList(),
    );
  }

  String get displayName => name.isEmpty ? id : name;

  String get searchableText => [
    id,
    name,
    description,
    category,
    source,
    ...tags,
  ].join(' ').toLowerCase();
}

class DeviceSkillCatalogResult {
  final List<DeviceSkillCatalogItem> items;

  const DeviceSkillCatalogResult({this.items = const []});

  factory DeviceSkillCatalogResult.fromJson(Map<String, dynamic> json) {
    return DeviceSkillCatalogResult(
      items: (json['items'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map(
            (item) =>
                DeviceSkillCatalogItem.fromJson(item.cast<String, dynamic>()),
          )
          .where((item) => item.id.isNotEmpty)
          .toList(),
    );
  }
}

class DeviceGlobalSkillConfigItem {
  final String id;
  final String name;
  final String description;
  final String content;
  final List<String> tags;
  final bool enabled;
  final List<DeviceSkillPackageFile> packageFiles;

  const DeviceGlobalSkillConfigItem({
    required this.id,
    required this.name,
    this.description = '',
    this.content = '',
    this.tags = const [],
    this.enabled = false,
    this.packageFiles = const [],
  });

  factory DeviceGlobalSkillConfigItem.fromJson(Map<String, dynamic> json) {
    return DeviceGlobalSkillConfigItem(
      id: json['name'] as String? ?? json['id'] as String? ?? '',
      name: json['name'] as String? ?? json['id'] as String? ?? '',
      description: json['description'] as String? ?? '',
      content: json['content'] as String? ?? '',
      tags: (json['tags'] as List<dynamic>? ?? const [])
          .map((item) => item.toString())
          .toList(),
      enabled: json['enabled'] as bool? ?? false,
      packageFiles: (json['package_files'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map(
            (item) =>
                DeviceSkillPackageFile.fromJson(item.cast<String, dynamic>()),
          )
          .toList(),
    );
  }

  DeviceSkillCatalogItem toCatalogItem() {
    return DeviceSkillCatalogItem(
      id: id,
      name: name,
      description: description,
      content: content,
      tags: tags,
      source: '设备本地',
      packageFiles: packageFiles,
    );
  }
}

class DeviceGlobalSkillConfigInfo {
  final bool enabled;
  final List<DeviceGlobalSkillConfigItem> skills;

  const DeviceGlobalSkillConfigInfo({
    this.enabled = false,
    this.skills = const [],
  });

  factory DeviceGlobalSkillConfigInfo.fromJson(Map<String, dynamic> json) {
    return DeviceGlobalSkillConfigInfo(
      enabled: json['enabled'] as bool? ?? false,
      skills: (json['skills'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map(
            (item) => DeviceGlobalSkillConfigItem.fromJson(
              item.cast<String, dynamic>(),
            ),
          )
          .where((item) => item.id.isNotEmpty)
          .toList(),
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'enabled': enabled,
      'skills': skills
          .map(
            (item) => {
              'name': item.name,
              'description': item.description,
              'content': item.content,
              'enabled': item.enabled,
              if (item.packageFiles.isNotEmpty)
                'package_files': item.packageFiles
                    .map(
                      (file) => {
                        'path': file.path,
                        'content': file.content,
                        if (file.executable) 'executable': true,
                      },
                    )
                    .toList(),
            },
          )
          .toList(),
    };
  }
}

class DeviceAgentSkillSelectionInfo {
  final String agentId;
  final List<String> defaultSkills;
  final List<String> extraSkills;
  final List<String> effectiveSkills;
  final List<String> availableSkillNames;
  final List<DeviceSkillCatalogItem> availableSkills;

  const DeviceAgentSkillSelectionInfo({
    this.agentId = '',
    this.defaultSkills = const [],
    this.extraSkills = const [],
    this.effectiveSkills = const [],
    this.availableSkillNames = const [],
    this.availableSkills = const [],
  });

  factory DeviceAgentSkillSelectionInfo.fromJson(Map<String, dynamic> json) {
    List<String> parseList(dynamic value) {
      if (value is! List) return const [];
      return value.map((item) => item.toString()).toList();
    }

    final available = (json['available_skills'] as List<dynamic>? ?? const [])
        .whereType<Map>()
        .map((item) {
          final value = item.cast<String, dynamic>();
          final id = value['name']?.toString() ?? '';
          return DeviceSkillCatalogItem(
            id: id,
            name: id,
            description: value['description'] as String? ?? '',
            content: value['content'] as String? ?? '',
            category: '本设备',
            source: '设备本地',
            packageFiles: (value['package_files'] as List<dynamic>? ?? const [])
                .whereType<Map>()
                .map(
                  (file) => DeviceSkillPackageFile.fromJson(
                    file.cast<String, dynamic>(),
                  ),
                )
                .toList(),
          );
        })
        .where((item) => item.id.isNotEmpty)
        .toList();
    return DeviceAgentSkillSelectionInfo(
      agentId: json['agent_id'] as String? ?? '',
      defaultSkills: parseList(json['default_skills']),
      extraSkills: parseList(json['extra_skills']),
      effectiveSkills: parseList(json['effective_skills']),
      availableSkillNames: available.map((item) => item.id).toList(),
      availableSkills: available,
    );
  }
}
