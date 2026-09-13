DateTime? _date(dynamic value) =>
    value is String ? DateTime.tryParse(value) : null;
String _text(dynamic value) => value?.toString() ?? '';
int _integer(dynamic value) =>
    value is num ? value.toInt() : int.tryParse(_text(value)) ?? 0;
double _decimal(dynamic value) =>
    value is num ? value.toDouble() : double.tryParse(_text(value)) ?? 0;
bool _boolean(dynamic value) => value == true || value == 1;
Map<String, dynamic> _map(dynamic value) =>
    value is Map ? value.cast<String, dynamic>() : <String, dynamic>{};
List<dynamic> _list(dynamic value) => value is List ? value : const [];

class ProjectScopeModel {
  final String id;
  final String machineId;
  final String displayName;
  final String currentRoot;
  final String lineageScopeId;
  final String status;
  final int bindingEpoch;
  final int revision;
  final DateTime? updatedAt;

  const ProjectScopeModel({
    required this.id,
    required this.machineId,
    required this.displayName,
    required this.currentRoot,
    this.lineageScopeId = '',
    required this.status,
    required this.bindingEpoch,
    required this.revision,
    this.updatedAt,
  });

  factory ProjectScopeModel.fromJson(Map<String, dynamic> json) =>
      ProjectScopeModel(
        id: _text(json['project_scope_id']),
        machineId: _text(json['machine_id']),
        displayName: _text(json['display_name']),
        currentRoot: _text(json['current_root']),
        lineageScopeId: _text(json['lineage_project_scope_id']),
        status: _text(json['status']).isEmpty
            ? 'active'
            : _text(json['status']),
        bindingEpoch: _integer(json['binding_epoch']),
        revision: _integer(json['revision']),
        updatedAt: _date(json['updated_at']),
      );
}

class ProjectMemoryVerificationModel {
  final String status;
  final String method;
  final List<String> evidenceRefs;
  final DateTime? verifiedAt;

  const ProjectMemoryVerificationModel({
    this.status = 'unverified',
    this.method = '',
    this.evidenceRefs = const [],
    this.verifiedAt,
  });

  factory ProjectMemoryVerificationModel.fromJson(Map<String, dynamic> json) =>
      ProjectMemoryVerificationModel(
        status: _text(json['status']).isEmpty
            ? 'unverified'
            : _text(json['status']),
        method: _text(json['method']),
        evidenceRefs: _list(
          json['evidence_refs'],
        ).map(_text).where((value) => value.isNotEmpty).toList(growable: false),
        verifiedAt: _date(json['verified_at']),
      );
}

class ProjectMemoryArtifactModel {
  final String path;
  final String sha256;
  final String packageName;
  final String apkSha256;
  final String buildId;
  final String abi;
  final String appVersion;
  final String moduleName;
  final String symbol;
  final String relativeOffset;
  final String functionStart;
  final String instructionSet;
  final String idaStatus;
  final String idaDatabaseId;
  final String hotUpdateStatus;
  final String shadowHookStatus;
  final String uiCallStatus;
  final String nativeCallStatus;

  const ProjectMemoryArtifactModel({
    this.path = '',
    this.sha256 = '',
    this.packageName = '',
    this.apkSha256 = '',
    this.buildId = '',
    this.abi = '',
    this.appVersion = '',
    this.moduleName = '',
    this.symbol = '',
    this.relativeOffset = '',
    this.functionStart = '',
    this.instructionSet = '',
    this.idaStatus = '',
    this.idaDatabaseId = '',
    this.hotUpdateStatus = '',
    this.shadowHookStatus = '',
    this.uiCallStatus = '',
    this.nativeCallStatus = '',
  });

  factory ProjectMemoryArtifactModel.fromJson(Map<String, dynamic> json) =>
      ProjectMemoryArtifactModel(
        path: _text(json['path']),
        sha256: _text(json['sha256']),
        packageName: _text(json['package_name']),
        apkSha256: _text(json['apk_sha256']),
        buildId: _text(json['build_id']),
        abi: _text(json['abi']),
        appVersion: _text(json['app_version']),
        moduleName: _text(json['module_name']),
        symbol: _text(json['symbol']),
        relativeOffset: _text(json['relative_offset']),
        functionStart: _text(json['function_start']),
        instructionSet: _text(json['instruction_set']),
        idaStatus: _text(json['ida_analysis_status']),
        idaDatabaseId: _text(json['ida_database_id']),
        hotUpdateStatus: _text(json['hot_update_status']),
        shadowHookStatus: _text(json['shadowhook_status']),
        uiCallStatus: _text(json['ui_call_status']),
        nativeCallStatus: _text(json['native_call_status']),
      );
}

class ProjectMemorySourceModel {
  final String sessionId;
  final String taskId;
  final List<String> messageIds;
  final List<String> toolCallIds;
  final String relativePath;
  final String sha256;
  final String excerpt;
  final String status;

  const ProjectMemorySourceModel({
    this.sessionId = '',
    this.taskId = '',
    this.messageIds = const [],
    this.toolCallIds = const [],
    this.relativePath = '',
    this.sha256 = '',
    this.excerpt = '',
    this.status = '',
  });

  factory ProjectMemorySourceModel.fromJson(Map<String, dynamic> json) =>
      ProjectMemorySourceModel(
        sessionId: _text(json['session_id']),
        taskId: _text(json['task_id']),
        messageIds: _list(
          json['message_ids'],
        ).map(_text).toList(growable: false),
        toolCallIds: _list(
          json['tool_call_ids'],
        ).map(_text).toList(growable: false),
        relativePath: _text(json['relative_path']),
        sha256: _text(json['sha256']),
        excerpt: _text(json['excerpt']),
        status: _text(json['status']),
      );
}

class ProjectMemoryModel {
  final String id;
  final String logicalId;
  final String scopeId;
  final String kind;
  final String subjectKey;
  final String statement;
  final String status;
  final double confidence;
  final bool locked;
  final bool sensitive;
  final List<String> scopePaths;
  final List<ProjectMemoryArtifactModel> artifacts;
  final List<ProjectMemorySourceModel> sources;
  final ProjectMemoryVerificationModel verification;
  final String supersedesId;
  final int version;
  final String createdBy;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  const ProjectMemoryModel({
    required this.id,
    required this.logicalId,
    required this.scopeId,
    required this.kind,
    required this.subjectKey,
    required this.statement,
    required this.status,
    required this.confidence,
    required this.locked,
    required this.sensitive,
    this.scopePaths = const [],
    this.artifacts = const [],
    this.sources = const [],
    this.verification = const ProjectMemoryVerificationModel(),
    this.supersedesId = '',
    required this.version,
    this.createdBy = '',
    this.createdAt,
    this.updatedAt,
  });

  factory ProjectMemoryModel.fromJson(Map<String, dynamic> json) =>
      ProjectMemoryModel(
        id: _text(json['memory_id']),
        logicalId: _text(json['logical_memory_id']),
        scopeId: _text(json['project_scope_id']),
        kind: _text(json['kind']),
        subjectKey: _text(json['subject_key']),
        statement: _text(json['statement']),
        status: _text(json['status']),
        confidence: _decimal(json['confidence']),
        locked: _boolean(json['locked']),
        sensitive: _boolean(json['sensitive']),
        scopePaths: _list(
          json['scope_paths'],
        ).map(_text).toList(growable: false),
        artifacts: _list(json['artifact_refs'])
            .map((item) => ProjectMemoryArtifactModel.fromJson(_map(item)))
            .toList(growable: false),
        sources: _list(json['source_refs'])
            .map((item) => ProjectMemorySourceModel.fromJson(_map(item)))
            .toList(growable: false),
        verification: ProjectMemoryVerificationModel.fromJson(
          _map(json['verification']),
        ),
        supersedesId: _text(json['supersedes_memory_id']),
        version: _integer(json['version']),
        createdBy: _text(json['created_by']),
        createdAt: _date(json['created_at']),
        updatedAt: _date(json['updated_at']),
      );
}

class ProjectMemoryBriefModel {
  final String content;
  final int tokenCount;
  final int sourceRevision;
  final DateTime? generatedAt;

  const ProjectMemoryBriefModel({
    this.content = '',
    this.tokenCount = 0,
    this.sourceRevision = 0,
    this.generatedAt,
  });

  factory ProjectMemoryBriefModel.fromJson(Map<String, dynamic> json) =>
      ProjectMemoryBriefModel(
        content: _text(json['content']),
        tokenCount: _integer(json['token_count']),
        sourceRevision: _integer(json['source_revision']),
        generatedAt: _date(json['generated_at']),
      );
}

class ProjectMemoryOverviewModel {
  final ProjectScopeModel scope;
  final ProjectMemoryBriefModel? brief;
  final List<ProjectMemoryModel> recent;

  const ProjectMemoryOverviewModel({
    required this.scope,
    this.brief,
    this.recent = const [],
  });

  factory ProjectMemoryOverviewModel.fromJson(Map<String, dynamic> json) =>
      ProjectMemoryOverviewModel(
        scope: ProjectScopeModel.fromJson(_map(json['scope'])),
        brief: json['brief'] == null
            ? null
            : ProjectMemoryBriefModel.fromJson(_map(json['brief'])),
        recent: _list(json['recent'])
            .map((item) => ProjectMemoryModel.fromJson(_map(item)))
            .toList(growable: false),
      );
}

class ProjectMemoryJobModel {
  final String id;
  final String scopeId;
  final String status;
  final int attempt;
  final int progress;
  final int resultRevision;
  final String checkpointCursor;
  final DateTime? checkpointAt;
  final String error;
  final bool manualVisible;

  const ProjectMemoryJobModel({
    required this.id,
    required this.scopeId,
    required this.status,
    this.attempt = 0,
    this.progress = 0,
    this.resultRevision = 0,
    this.checkpointCursor = '',
    this.checkpointAt,
    this.error = '',
    this.manualVisible = false,
  });

  bool get terminal =>
      status == 'completed' || status == 'failed' || status == 'cancelled';

  factory ProjectMemoryJobModel.fromJson(Map<String, dynamic> json) =>
      ProjectMemoryJobModel(
        id: _text(json['job_id']),
        scopeId: _text(json['project_scope_id']),
        status: _text(json['status']),
        attempt: _integer(json['attempt']),
        progress: _integer(json['progress'] ?? json['checkpoint_progress']),
        resultRevision: _integer(json['result_revision']),
        checkpointCursor: _text(json['checkpoint_cursor']),
        checkpointAt: _date(json['checkpoint_at']),
        error: _text(json['error']),
        manualVisible: _boolean(json['manual_visible']),
      );
}

class ProjectMemoryJobEventModel {
  final int sequence;
  final String type;
  final String message;
  final int progress;
  final Map<String, dynamic> metadata;
  final DateTime? createdAt;

  const ProjectMemoryJobEventModel({
    required this.sequence,
    required this.type,
    this.message = '',
    this.progress = 0,
    this.metadata = const {},
    this.createdAt,
  });

  factory ProjectMemoryJobEventModel.fromJson(Map<String, dynamic> json) =>
      ProjectMemoryJobEventModel(
        sequence: _integer(json['sequence']),
        type: _text(json['type']),
        message: _text(json['message']),
        progress: _integer(json['progress']),
        metadata: _map(json['metadata']),
        createdAt: _date(json['created_at']),
      );

  int metadataInt(String key) => _integer(metadata[key]);

  bool metadataBool(String key) => _boolean(metadata[key]);
}
