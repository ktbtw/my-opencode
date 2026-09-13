package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"relay-server/internal/model"
)

const projectMemorySensitiveColumn = "`sensitive`"

var projectMemorySchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS project_scopes (
  project_scope_id VARCHAR(64) PRIMARY KEY,
  operator_id BIGINT NOT NULL,
  machine_id VARCHAR(128) NOT NULL,
  display_name VARCHAR(255) NOT NULL DEFAULT '',
  current_root MEDIUMTEXT NULL,
  filesystem_volume_id VARCHAR(255) NOT NULL DEFAULT '',
  filesystem_file_id VARCHAR(255) NOT NULL DEFAULT '',
  instance_nonce VARCHAR(128) NOT NULL DEFAULT '',
  lineage_project_scope_id VARCHAR(64) NOT NULL DEFAULT '',
  binding_epoch BIGINT NOT NULL DEFAULT 1,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  revision BIGINT NOT NULL DEFAULT 1,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  KEY idx_project_scopes_owner (operator_id, machine_id, status),
  KEY idx_project_scopes_fs (operator_id, machine_id, filesystem_volume_id, filesystem_file_id),
  KEY idx_project_scopes_nonce (operator_id, machine_id, instance_nonce)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS project_scope_locations (
  location_id VARCHAR(64) PRIMARY KEY,
  project_scope_id VARCHAR(64) NOT NULL,
  operator_id BIGINT NOT NULL,
  machine_id VARCHAR(128) NOT NULL,
  root_path MEDIUMTEXT NULL,
  display_name VARCHAR(255) NOT NULL DEFAULT '',
  filesystem_volume_id VARCHAR(255) NOT NULL DEFAULT '',
  filesystem_file_id VARCHAR(255) NOT NULL DEFAULT '',
	  marker_nonce VARCHAR(128) NOT NULL DEFAULT '',
	  binding_epoch BIGINT NOT NULL DEFAULT 1,
  claim_state VARCHAR(32) NOT NULL DEFAULT '',
  reachable TINYINT(1) NOT NULL DEFAULT 1,
  first_seen_at DATETIME(6) NOT NULL,
  last_seen_at DATETIME(6) NOT NULL,
  KEY idx_project_locations_scope (project_scope_id, last_seen_at),
  KEY idx_project_locations_fs (operator_id, machine_id, filesystem_volume_id, filesystem_file_id),
  KEY idx_project_locations_nonce (operator_id, machine_id, marker_nonce)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS project_agent_bindings (
  operator_id BIGINT NOT NULL,
  machine_id VARCHAR(128) NOT NULL,
  agent_id VARCHAR(191) NOT NULL,
  project_scope_id VARCHAR(64) NOT NULL,
  binding_epoch BIGINT NOT NULL DEFAULT 1,
  active TINYINT(1) NOT NULL DEFAULT 1,
  attached_at DATETIME(6) NOT NULL,
  detached_at DATETIME(6) NULL,
  PRIMARY KEY (operator_id, machine_id, agent_id),
  KEY idx_project_bindings_scope (project_scope_id, active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS project_memories (
  memory_id VARCHAR(64) PRIMARY KEY,
  logical_memory_id VARCHAR(64) NOT NULL,
  project_scope_id VARCHAR(64) NOT NULL,
  kind VARCHAR(32) NOT NULL,
  subject_key VARCHAR(512) NOT NULL,
  statement_text TEXT NOT NULL,
  search_text TEXT NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  confidence DOUBLE NOT NULL DEFAULT 0,
  locked TINYINT(1) NOT NULL DEFAULT 0,
  ` + projectMemorySensitiveColumn + ` TINYINT(1) NOT NULL DEFAULT 0,
  scope_paths_json JSON NULL,
  verification_status VARCHAR(32) NOT NULL DEFAULT 'unverified',
  verification_method VARCHAR(128) NOT NULL DEFAULT '',
  verification_evidence_json JSON NULL,
  verified_at DATETIME(6) NULL,
	  content_hash CHAR(64) NOT NULL,
	  embedding_model VARCHAR(255) NOT NULL DEFAULT '',
	  embedding_version VARCHAR(128) NOT NULL DEFAULT '',
  supersedes_memory_id VARCHAR(64) NOT NULL DEFAULT '',
  version BIGINT NOT NULL DEFAULT 1,
  created_by VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  UNIQUE KEY uk_project_memory_content (project_scope_id, content_hash),
  KEY idx_project_memory_subject (project_scope_id, subject_key(191), status),
  KEY idx_project_memory_filter (project_scope_id, kind, status, verification_status, locked),
  KEY idx_project_memory_updated (project_scope_id, updated_at),
	  FULLTEXT KEY ft_project_memory_search (search_text)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS project_memory_sources (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  memory_id VARCHAR(64) NOT NULL,
  source_order INT NOT NULL,
  session_id VARCHAR(64) NOT NULL DEFAULT '',
  task_id VARCHAR(64) NOT NULL DEFAULT '',
  message_ids_json JSON NULL,
  tool_call_ids_json JSON NULL,
  relative_path VARCHAR(1024) NOT NULL DEFAULT '',
	  sha256 CHAR(64) NOT NULL DEFAULT '',
	  package_name VARCHAR(255) NOT NULL DEFAULT '',
	  apk_sha256 CHAR(64) NOT NULL DEFAULT '',
  excerpt_text MEDIUMTEXT NULL,
  source_status VARCHAR(32) NOT NULL DEFAULT '',
  UNIQUE KEY uk_project_memory_source_order (memory_id, source_order),
  KEY idx_project_memory_sources_task (task_id),
  KEY idx_project_memory_sources_session (session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS project_memory_artifacts (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  memory_id VARCHAR(64) NOT NULL,
  artifact_order INT NOT NULL,
  relative_path VARCHAR(1024) NOT NULL DEFAULT '',
  sha256 CHAR(64) NOT NULL DEFAULT '',
  build_id VARCHAR(255) NOT NULL DEFAULT '',
  abi VARCHAR(64) NOT NULL DEFAULT '',
  app_version VARCHAR(128) NOT NULL DEFAULT '',
  module_name VARCHAR(255) NOT NULL DEFAULT '',
  symbol_name VARCHAR(512) NOT NULL DEFAULT '',
  relative_offset VARCHAR(64) NOT NULL DEFAULT '',
  function_start VARCHAR(64) NOT NULL DEFAULT '',
  instruction_set VARCHAR(64) NOT NULL DEFAULT '',
	  ida_analysis_status VARCHAR(32) NOT NULL DEFAULT '',
	  ida_database_id VARCHAR(255) NOT NULL DEFAULT '',
  hot_update_status VARCHAR(32) NOT NULL DEFAULT '',
  shadowhook_status VARCHAR(32) NOT NULL DEFAULT '',
  ui_call_status VARCHAR(32) NOT NULL DEFAULT '',
  native_call_status VARCHAR(32) NOT NULL DEFAULT '',
  UNIQUE KEY uk_project_memory_artifact_order (memory_id, artifact_order),
  KEY idx_project_memory_artifact_hash (sha256),
  KEY idx_project_memory_artifact_build (build_id, abi)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS project_memory_jobs (
  job_id VARCHAR(64) PRIMARY KEY,
  operator_id BIGINT NOT NULL,
  machine_id VARCHAR(128) NOT NULL,
  project_scope_id VARCHAR(64) NOT NULL,
  trigger_type VARCHAR(32) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'queued',
  cursor_start VARCHAR(191) NOT NULL DEFAULT '',
  cursor_end VARCHAR(191) NOT NULL DEFAULT '',
	  cursor_committed VARCHAR(191) NOT NULL DEFAULT '',
	  checkpoint_cursor VARCHAR(191) NOT NULL DEFAULT '',
	  checkpoint_progress INT NOT NULL DEFAULT 0,
	  checkpoint_at DATETIME(6) NULL,
	  pending_cursor_end VARCHAR(191) NOT NULL DEFAULT '',
  attempt INT NOT NULL DEFAULT 0,
  lease_owner VARCHAR(191) NOT NULL DEFAULT '',
  lease_expires_at DATETIME(6) NULL,
  fencing_token BIGINT NOT NULL DEFAULT 0,
  input_revision BIGINT NOT NULL DEFAULT 0,
  result_revision BIGINT NOT NULL DEFAULT 0,
  error_text TEXT NULL,
  manual_visible TINYINT(1) NOT NULL DEFAULT 0,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  KEY idx_project_memory_jobs_scope (operator_id, project_scope_id, status, updated_at),
	  KEY idx_project_memory_jobs_device (operator_id, machine_id, status, lease_expires_at),
  KEY idx_project_memory_jobs_lease (status, lease_expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS project_memory_device_locks (
  operator_id BIGINT NOT NULL,
  machine_id VARCHAR(128) NOT NULL,
  touched_at DATETIME(6) NOT NULL,
  PRIMARY KEY (operator_id, machine_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS project_memory_job_events (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  job_id VARCHAR(64) NOT NULL,
  sequence_value BIGINT NOT NULL,
  event_type VARCHAR(64) NOT NULL,
  message_text TEXT NULL,
  progress_value INT NOT NULL DEFAULT 0,
  metadata_json JSON NULL,
  created_at DATETIME(6) NOT NULL,
  UNIQUE KEY uk_project_memory_job_sequence (job_id, sequence_value),
  KEY idx_project_memory_job_events_created (job_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS project_briefs (
  project_scope_id VARCHAR(64) PRIMARY KEY,
  content_text MEDIUMTEXT NOT NULL,
  token_count INT NOT NULL DEFAULT 0,
  source_revision BIGINT NOT NULL,
  model_name VARCHAR(255) NOT NULL DEFAULT '',
  generated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS project_memory_usages (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  project_scope_id VARCHAR(64) NOT NULL,
  task_id VARCHAR(64) NOT NULL,
  revision BIGINT NOT NULL,
  memory_ids_json JSON NULL,
  token_count INT NOT NULL DEFAULT 0,
  created_at DATETIME(6) NOT NULL,
  KEY idx_project_memory_usage_scope (project_scope_id, created_at),
  KEY idx_project_memory_usage_task (task_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS project_memory_audits (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  project_scope_id VARCHAR(64) NOT NULL,
  memory_id VARCHAR(64) NOT NULL DEFAULT '',
  job_id VARCHAR(64) NOT NULL DEFAULT '',
  operator_id BIGINT NOT NULL,
  action_name VARCHAR(64) NOT NULL,
  metadata_json JSON NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  KEY idx_project_memory_audit_scope (operator_id, project_scope_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
}

func marshalProjectMemoryJSON(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func (m *MySQLArchive) UpsertProjectScope(ctx context.Context, scope model.ProjectScope) error {
	scope = normalizeProjectScope(scope)
	encryptedRoot, err := m.projectMemoryCipher.encrypt(scope.CurrentRoot, "project_scope.current_root")
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `
INSERT INTO project_scopes (
  project_scope_id, operator_id, machine_id, display_name, current_root,
  filesystem_volume_id, filesystem_file_id, instance_nonce,
  lineage_project_scope_id, binding_epoch, status, revision, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  display_name = VALUES(display_name), current_root = VALUES(current_root),
  filesystem_volume_id = VALUES(filesystem_volume_id), filesystem_file_id = VALUES(filesystem_file_id),
  instance_nonce = VALUES(instance_nonce), lineage_project_scope_id = VALUES(lineage_project_scope_id),
  binding_epoch = GREATEST(binding_epoch, VALUES(binding_epoch)), status = VALUES(status),
  revision = GREATEST(revision, VALUES(revision)), updated_at = VALUES(updated_at)
`, scope.ID, scope.OperatorID, scope.MachineID, scope.DisplayName, encryptedRoot,
		scope.VolumeID, scope.FileID, scope.InstanceNonce, scope.LineageScopeID,
		scope.BindingEpoch, scope.Status, scope.Revision, scope.CreatedAt, scope.UpdatedAt)
	return err
}

func (m *MySQLArchive) scanProjectScope(scanner interface{ Scan(...any) error }) (*model.ProjectScope, error) {
	var scope model.ProjectScope
	err := scanner.Scan(&scope.ID, &scope.OperatorID, &scope.MachineID, &scope.DisplayName,
		&scope.CurrentRoot, &scope.VolumeID, &scope.FileID, &scope.InstanceNonce,
		&scope.LineageScopeID, &scope.BindingEpoch, &scope.Status, &scope.Revision,
		&scope.CreatedAt, &scope.UpdatedAt)
	if err != nil {
		return nil, err
	}
	scope.CurrentRoot, err = m.projectMemoryCipher.decrypt(scope.CurrentRoot, "project_scope.current_root")
	if err != nil {
		return nil, err
	}
	return &scope, nil
}

const projectScopeSelect = `SELECT project_scope_id, operator_id, machine_id, display_name,
current_root, filesystem_volume_id, filesystem_file_id, instance_nonce,
lineage_project_scope_id, binding_epoch, status, revision, created_at, updated_at
FROM project_scopes`

func (m *MySQLArchive) GetProjectScope(ctx context.Context, operatorID int64, machineID, scopeID string) (*model.ProjectScope, error) {
	query := projectScopeSelect + ` WHERE operator_id = ? AND machine_id = ? AND project_scope_id = ?`
	scope, err := m.scanProjectScope(m.db.QueryRowContext(ctx, query, operatorID, machineID, scopeID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return scope, err
}

func (m *MySQLArchive) ListProjectScopes(ctx context.Context, operatorID int64, machineID string) ([]model.ProjectScope, error) {
	query := projectScopeSelect + ` WHERE operator_id = ?`
	args := []any{operatorID}
	if strings.TrimSpace(machineID) != "" {
		query += ` AND machine_id = ?`
		args = append(args, machineID)
	}
	query += ` ORDER BY updated_at DESC`
	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ProjectScope{}
	for rows.Next() {
		scope, err := m.scanProjectScope(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *scope)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) UpsertProjectScopeLocation(ctx context.Context, location model.ProjectScopeLocation) error {
	if location.ID == "" {
		location.ID = newProjectMemoryID("loc")
	}
	now := time.Now().UTC()
	if location.FirstSeenAt.IsZero() {
		location.FirstSeenAt = now
	}
	if location.LastSeenAt.IsZero() {
		location.LastSeenAt = now
	}
	encryptedRoot, err := m.projectMemoryCipher.encrypt(location.Root, "project_scope_location.root")
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO project_scope_locations (
	location_id, project_scope_id, operator_id, machine_id, root_path, display_name,
	filesystem_volume_id, filesystem_file_id, marker_nonce, binding_epoch, claim_state, reachable,
	first_seen_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE root_path=VALUES(root_path), display_name=VALUES(display_name),
	binding_epoch=GREATEST(binding_epoch, VALUES(binding_epoch)), claim_state=VALUES(claim_state),
	reachable=VALUES(reachable), last_seen_at=VALUES(last_seen_at)`,
		location.ID, location.ScopeID, location.OperatorID, location.MachineID, encryptedRoot,
		location.DisplayName, location.VolumeID, location.FileID, location.MarkerNonce,
		location.BindingEpoch, location.ClaimState, location.Reachable, location.FirstSeenAt, location.LastSeenAt)
	return err
}

func (m *MySQLArchive) UpsertProjectAgentBinding(ctx context.Context, binding model.ProjectAgentBinding) error {
	if binding.AttachedAt.IsZero() {
		binding.AttachedAt = time.Now().UTC()
	}
	var detached any
	if !binding.DetachedAt.IsZero() {
		detached = binding.DetachedAt
	}
	_, err := m.db.ExecContext(ctx, `INSERT INTO project_agent_bindings (
operator_id, machine_id, agent_id, project_scope_id, binding_epoch, active, attached_at, detached_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE project_scope_id=VALUES(project_scope_id), binding_epoch=VALUES(binding_epoch),
active=VALUES(active), attached_at=VALUES(attached_at), detached_at=VALUES(detached_at)`,
		binding.OperatorID, binding.MachineID, binding.AgentID, binding.ScopeID,
		binding.BindingEpoch, binding.Active, binding.AttachedAt, detached)
	return err
}

func (m *MySQLArchive) GetProjectScopeForAgent(ctx context.Context, operatorID int64, machineID, agentID string) (*model.ProjectScope, error) {
	query := `SELECT s.project_scope_id, s.operator_id, s.machine_id, s.display_name,
s.current_root, s.filesystem_volume_id, s.filesystem_file_id, s.instance_nonce,
s.lineage_project_scope_id, s.binding_epoch, s.status, s.revision, s.created_at, s.updated_at
FROM project_agent_bindings b JOIN project_scopes s ON s.project_scope_id = b.project_scope_id
WHERE b.operator_id=? AND b.machine_id=? AND b.agent_id=? AND b.active=1
AND s.operator_id=b.operator_id AND s.machine_id=b.machine_id`
	scope, err := m.scanProjectScope(m.db.QueryRowContext(ctx, query, operatorID, machineID, agentID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return scope, err
}

func projectMemorySearchText(memory model.ProjectMemory) string {
	if memory.Sensitive {
		return ""
	}
	parts := []string{memory.Statement, memory.SubjectKey}
	parts = append(parts, memory.ScopePaths...)
	for _, artifact := range memory.Artifacts {
		parts = append(parts, artifact.Path, artifact.SHA256, artifact.PackageName, artifact.APKSHA256,
			artifact.BuildID, artifact.ABI, artifact.ModuleName, artifact.Symbol, artifact.IDADatabaseID)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func projectMemoryBooleanQuery(value string) string {
	fields := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127 || r == '_')
	})
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len([]rune(field)) < 2 {
			continue
		}
		terms = append(terms, field+"*")
		if len(terms) == 16 {
			break
		}
	}
	return strings.Join(terms, " ")
}

func nullableProjectMemoryTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func (m *MySQLArchive) upsertProjectMemoryTx(ctx context.Context, tx *sql.Tx, memory model.ProjectMemory) error {
	memory = normalizeProjectMemory(memory)
	statement := memory.Statement
	if memory.Sensitive {
		var err error
		statement, err = m.projectMemoryCipher.encrypt(statement, "project_memory.statement")
		if err != nil {
			return err
		}
	}
	scopePaths, err := marshalProjectMemoryJSON(memory.ScopePaths)
	if err != nil {
		return err
	}
	evidence, err := marshalProjectMemoryJSON(memory.Verification.EvidenceRefs)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_memories (
memory_id, logical_memory_id, project_scope_id, kind, subject_key, statement_text,
search_text, status, confidence, locked, `+projectMemorySensitiveColumn+`, scope_paths_json,
	verification_status, verification_method, verification_evidence_json, verified_at,
	content_hash, embedding_model, embedding_version, supersedes_memory_id, version, created_by, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE status=VALUES(status), locked=VALUES(locked), `+projectMemorySensitiveColumn+`=VALUES(`+projectMemorySensitiveColumn+`),
statement_text=VALUES(statement_text), search_text=VALUES(search_text), confidence=VALUES(confidence),
scope_paths_json=VALUES(scope_paths_json), verification_status=VALUES(verification_status),
verification_method=VALUES(verification_method), verification_evidence_json=VALUES(verification_evidence_json),
	verified_at=VALUES(verified_at), embedding_model=VALUES(embedding_model),
	embedding_version=VALUES(embedding_version), supersedes_memory_id=VALUES(supersedes_memory_id),
version=VALUES(version), updated_at=VALUES(updated_at)`,
		memory.ID, memory.LogicalID, memory.ScopeID, memory.Kind, memory.SubjectKey,
		statement, projectMemorySearchText(memory), memory.Status, memory.Confidence,
		memory.Locked, memory.Sensitive, scopePaths, memory.Verification.Status,
		memory.Verification.Method, evidence, nullableProjectMemoryTime(memory.Verification.VerifiedAt),
		memory.ContentHash, memory.EmbeddingModel, memory.EmbeddingVersion,
		memory.SupersedesID, memory.Version, memory.CreatedBy,
		memory.CreatedAt, memory.UpdatedAt)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_memory_sources WHERE memory_id=?`, memory.ID); err != nil {
		return err
	}
	for index, source := range memory.Sources {
		excerpt, err := m.projectMemoryCipher.encrypt(source.Excerpt, "project_memory_source.excerpt")
		if err != nil {
			return err
		}
		messageIDs, err := marshalProjectMemoryJSON(source.MessageIDs)
		if err != nil {
			return err
		}
		toolIDs, err := marshalProjectMemoryJSON(source.ToolCallIDs)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_memory_sources (
memory_id, source_order, session_id, task_id, message_ids_json, tool_call_ids_json,
relative_path, sha256, excerpt_text, source_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			memory.ID, index, source.SessionID, source.TaskID, messageIDs, toolIDs,
			source.RelativePath, source.SHA256, excerpt, source.Status); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_memory_artifacts WHERE memory_id=?`, memory.ID); err != nil {
		return err
	}
	for index, artifact := range memory.Artifacts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_memory_artifacts (
	memory_id, artifact_order, relative_path, sha256, package_name, apk_sha256, build_id, abi, app_version, module_name,
	symbol_name, relative_offset, function_start, instruction_set, ida_analysis_status,
	ida_database_id, hot_update_status, shadowhook_status, ui_call_status, native_call_status)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			memory.ID, index, artifact.Path, artifact.SHA256, artifact.PackageName, artifact.APKSHA256,
			artifact.BuildID, artifact.ABI,
			artifact.AppVersion, artifact.ModuleName, artifact.Symbol, artifact.RelativeOffset,
			artifact.FunctionStart, artifact.InstructionSet, artifact.IDAStatus, artifact.IDADatabaseID,
			artifact.HotUpdateStatus, artifact.ShadowHookStatus, artifact.UICallStatus,
			artifact.NativeCallStatus); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLArchive) UpsertProjectMemory(ctx context.Context, memory model.ProjectMemory) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := m.upsertProjectMemoryTx(ctx, tx, memory); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *MySQLArchive) EditProjectMemoryVersion(ctx context.Context, operatorID int64, scopeID, memoryID string, next model.ProjectMemory) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous model.ProjectMemory
	var scopePaths, evidence []byte
	var verifiedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT m.memory_id, m.logical_memory_id, m.project_scope_id,
m.kind, m.subject_key, m.statement_text, m.status, m.confidence, m.locked, m.`+projectMemorySensitiveColumn+`,
m.scope_paths_json, m.verification_status, m.verification_method,
	m.verification_evidence_json, m.verified_at, m.content_hash, m.embedding_model,
	m.embedding_version, m.supersedes_memory_id,
m.version, m.created_by, m.created_at, m.updated_at FROM project_memories m
JOIN project_scopes s ON s.project_scope_id=m.project_scope_id
WHERE s.operator_id=? AND s.project_scope_id=? AND m.memory_id=? FOR UPDATE`,
		operatorID, scopeID, memoryID).Scan(&previous.ID, &previous.LogicalID, &previous.ScopeID,
		&previous.Kind, &previous.SubjectKey, &previous.Statement, &previous.Status, &previous.Confidence,
		&previous.Locked, &previous.Sensitive, &scopePaths, &previous.Verification.Status,
		&previous.Verification.Method, &evidence, &verifiedAt, &previous.ContentHash,
		&previous.EmbeddingModel, &previous.EmbeddingVersion,
		&previous.SupersedesID, &previous.Version, &previous.CreatedBy, &previous.CreatedAt, &previous.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("project memory not found")
		}
		return err
	}
	_ = json.Unmarshal(scopePaths, &previous.ScopePaths)
	_ = json.Unmarshal(evidence, &previous.Verification.EvidenceRefs)
	if verifiedAt.Valid {
		previous.Verification.VerifiedAt = verifiedAt.Time
	}
	if next.Version != 0 && next.Version != previous.Version {
		return ErrProjectMemoryRevisionConflict
	}
	if previous.Status == model.ProjectMemorySuperseded ||
		(previous.Status == model.ProjectMemoryDeleted && (next.Status != model.ProjectMemoryActive || strings.TrimSpace(next.Statement) == "")) {
		return ErrProjectMemoryRevisionConflict
	}
	next = normalizeProjectMemory(next)
	next.ID = newProjectMemoryID("mem")
	next.LogicalID = previous.LogicalID
	next.ScopeID = scopeID
	next.Version = previous.Version + 1
	next.SupersedesID = previous.ID
	next.CreatedBy = "user"
	now := time.Now().UTC()
	next.CreatedAt = now
	next.UpdatedAt = now
	if _, err := tx.ExecContext(ctx, `UPDATE project_memories SET status='superseded', updated_at=? WHERE memory_id=?`, now, previous.ID); err != nil {
		return err
	}
	if err := m.upsertProjectMemoryTx(ctx, tx, next); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_scopes SET revision=revision+1, updated_at=? WHERE operator_id=? AND project_scope_id=?`, now, operatorID, scopeID); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *MySQLArchive) scanProjectMemory(scanner interface{ Scan(...any) error }) (*model.ProjectMemory, error) {
	var memory model.ProjectMemory
	var scopePaths, evidence []byte
	var verifiedAt sql.NullTime
	err := scanner.Scan(&memory.ID, &memory.LogicalID, &memory.ScopeID, &memory.Kind,
		&memory.SubjectKey, &memory.Statement, &memory.Status, &memory.Confidence,
		&memory.Locked, &memory.Sensitive, &scopePaths, &memory.Verification.Status,
		&memory.Verification.Method, &evidence, &verifiedAt, &memory.ContentHash,
		&memory.EmbeddingModel, &memory.EmbeddingVersion,
		&memory.SupersedesID, &memory.Version, &memory.CreatedBy, &memory.CreatedAt, &memory.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(scopePaths, &memory.ScopePaths)
	_ = json.Unmarshal(evidence, &memory.Verification.EvidenceRefs)
	if verifiedAt.Valid {
		memory.Verification.VerifiedAt = verifiedAt.Time
	}
	if memory.Sensitive {
		memory.Statement, err = m.projectMemoryCipher.decrypt(memory.Statement, "project_memory.statement")
		if err != nil {
			return nil, err
		}
	}
	return &memory, nil
}

const projectMemorySelect = `SELECT m.memory_id, m.logical_memory_id, m.project_scope_id,
m.kind, m.subject_key, m.statement_text, m.status, m.confidence, m.locked, m.` + projectMemorySensitiveColumn + `,
m.scope_paths_json, m.verification_status, m.verification_method,
	m.verification_evidence_json, m.verified_at, m.content_hash, m.embedding_model,
	m.embedding_version, m.supersedes_memory_id,
m.version, m.created_by, m.created_at, m.updated_at FROM project_memories m`

func (m *MySQLArchive) hydrateProjectMemory(ctx context.Context, memory *model.ProjectMemory) error {
	rows, err := m.db.QueryContext(ctx, `SELECT session_id, task_id, message_ids_json,
tool_call_ids_json, relative_path, sha256, excerpt_text, source_status FROM project_memory_sources
WHERE memory_id=? ORDER BY source_order`, memory.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var source model.ProjectMemorySource
		var messageIDs, toolIDs []byte
		if err := rows.Scan(&source.SessionID, &source.TaskID, &messageIDs, &toolIDs,
			&source.RelativePath, &source.SHA256, &source.Excerpt, &source.Status); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal(messageIDs, &source.MessageIDs)
		_ = json.Unmarshal(toolIDs, &source.ToolCallIDs)
		source.Excerpt, err = m.projectMemoryCipher.decrypt(source.Excerpt, "project_memory_source.excerpt")
		if err != nil {
			rows.Close()
			return err
		}
		memory.Sources = append(memory.Sources, source)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	artifactRows, err := m.db.QueryContext(ctx, `SELECT relative_path, sha256, package_name, apk_sha256, build_id, abi,
	app_version, module_name, symbol_name, relative_offset, function_start, instruction_set,
	ida_analysis_status, ida_database_id, hot_update_status, shadowhook_status, ui_call_status, native_call_status
FROM project_memory_artifacts WHERE memory_id=? ORDER BY artifact_order`, memory.ID)
	if err != nil {
		return err
	}
	defer artifactRows.Close()
	for artifactRows.Next() {
		var artifact model.ProjectMemoryArtifact
		if err := artifactRows.Scan(&artifact.Path, &artifact.SHA256, &artifact.PackageName,
			&artifact.APKSHA256, &artifact.BuildID,
			&artifact.ABI, &artifact.AppVersion, &artifact.ModuleName, &artifact.Symbol,
			&artifact.RelativeOffset, &artifact.FunctionStart, &artifact.InstructionSet,
			&artifact.IDAStatus, &artifact.IDADatabaseID, &artifact.HotUpdateStatus, &artifact.ShadowHookStatus,
			&artifact.UICallStatus, &artifact.NativeCallStatus); err != nil {
			return err
		}
		memory.Artifacts = append(memory.Artifacts, artifact)
	}
	return artifactRows.Err()
}

func (m *MySQLArchive) hydrateProjectMemories(ctx context.Context, memories []model.ProjectMemory) error {
	if len(memories) == 0 {
		return nil
	}
	index := make(map[string]int, len(memories))
	args := make([]any, 0, len(memories))
	for position := range memories {
		index[memories[position].ID] = position
		args = append(args, memories[position].ID)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	rows, err := m.db.QueryContext(ctx, `SELECT memory_id, session_id, task_id, message_ids_json,
tool_call_ids_json, relative_path, sha256, excerpt_text, source_status FROM project_memory_sources
WHERE memory_id IN (`+placeholders+`) ORDER BY memory_id, source_order`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var memoryID string
		var source model.ProjectMemorySource
		var messageIDs, toolIDs []byte
		if err := rows.Scan(&memoryID, &source.SessionID, &source.TaskID, &messageIDs, &toolIDs,
			&source.RelativePath, &source.SHA256, &source.Excerpt, &source.Status); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal(messageIDs, &source.MessageIDs)
		_ = json.Unmarshal(toolIDs, &source.ToolCallIDs)
		source.Excerpt, err = m.projectMemoryCipher.decrypt(source.Excerpt, "project_memory_source.excerpt")
		if err != nil {
			rows.Close()
			return err
		}
		if position, found := index[memoryID]; found {
			memories[position].Sources = append(memories[position].Sources, source)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	artifactRows, err := m.db.QueryContext(ctx, `SELECT memory_id, relative_path, sha256, package_name,
apk_sha256, build_id, abi, app_version, module_name, symbol_name, relative_offset, function_start,
instruction_set, ida_analysis_status, ida_database_id, hot_update_status, shadowhook_status,
ui_call_status, native_call_status FROM project_memory_artifacts WHERE memory_id IN (`+placeholders+`)
ORDER BY memory_id, artifact_order`, args...)
	if err != nil {
		return err
	}
	defer artifactRows.Close()
	for artifactRows.Next() {
		var memoryID string
		var artifact model.ProjectMemoryArtifact
		if err := artifactRows.Scan(&memoryID, &artifact.Path, &artifact.SHA256, &artifact.PackageName,
			&artifact.APKSHA256, &artifact.BuildID, &artifact.ABI, &artifact.AppVersion,
			&artifact.ModuleName, &artifact.Symbol, &artifact.RelativeOffset, &artifact.FunctionStart,
			&artifact.InstructionSet, &artifact.IDAStatus, &artifact.IDADatabaseID,
			&artifact.HotUpdateStatus, &artifact.ShadowHookStatus, &artifact.UICallStatus,
			&artifact.NativeCallStatus); err != nil {
			return err
		}
		if position, found := index[memoryID]; found {
			memories[position].Artifacts = append(memories[position].Artifacts, artifact)
		}
	}
	return artifactRows.Err()
}

func (m *MySQLArchive) ListProjectMemories(ctx context.Context, filter model.ProjectMemoryFilter) ([]model.ProjectMemory, error) {
	query := projectMemorySelect + ` JOIN project_scopes s ON s.project_scope_id=m.project_scope_id
WHERE s.operator_id=? AND s.machine_id=? AND s.project_scope_id=?`
	args := []any{filter.OperatorID, filter.MachineID, filter.ScopeID}
	if !filter.IncludeDeleted {
		query += ` AND m.status <> 'deleted'`
	}
	if filter.Kind != "" {
		query += ` AND m.kind=?`
		args = append(args, filter.Kind)
	}
	if len(filter.Kinds) > 0 {
		query += ` AND m.kind IN (` + strings.TrimRight(strings.Repeat("?,", len(filter.Kinds)), ",") + `)`
		for _, kind := range filter.Kinds {
			args = append(args, kind)
		}
	}
	if filter.Status != "" {
		query += ` AND m.status=?`
		args = append(args, filter.Status)
	}
	if filter.Verification != "" {
		query += ` AND m.verification_status=?`
		args = append(args, filter.Verification)
	}
	if filter.Locked != nil {
		query += ` AND m.locked=?`
		args = append(args, *filter.Locked)
	}
	if strings.TrimSpace(filter.Query) != "" {
		booleanQuery := projectMemoryBooleanQuery(filter.Query)
		if booleanQuery != "" {
			query += ` AND MATCH(m.search_text) AGAINST (? IN BOOLEAN MODE)`
			args = append(args, booleanQuery)
		}
	}
	if filter.RelativePath != "" {
		query += ` AND JSON_SEARCH(m.scope_paths_json, 'one', ?) IS NOT NULL`
		args = append(args, "%"+filter.RelativePath+"%")
	}
	if filter.ArtifactHash != "" {
		query += ` AND EXISTS (SELECT 1 FROM project_memory_artifacts a WHERE a.memory_id=m.memory_id AND a.sha256=?)`
		args = append(args, filter.ArtifactHash)
	}
	if filter.BeforeID != "" {
		beforeUpdatedAt := filter.BeforeUpdatedAt
		if beforeUpdatedAt.IsZero() {
			err := m.db.QueryRowContext(ctx, `SELECT m.updated_at FROM project_memories m
JOIN project_scopes s ON s.project_scope_id=m.project_scope_id
WHERE s.operator_id=? AND s.machine_id=? AND s.project_scope_id=? AND m.memory_id=?`,
				filter.OperatorID, filter.MachineID, filter.ScopeID, filter.BeforeID).Scan(&beforeUpdatedAt)
			if errors.Is(err, sql.ErrNoRows) {
				return []model.ProjectMemory{}, nil
			}
			if err != nil {
				return nil, err
			}
		}
		query += ` AND (m.updated_at < ? OR (m.updated_at = ? AND m.memory_id < ?))`
		args = append(args, beforeUpdatedAt, beforeUpdatedAt, filter.BeforeID)
	}
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query += ` ORDER BY m.updated_at DESC, m.memory_id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	items := []model.ProjectMemory{}
	for rows.Next() {
		memory, err := m.scanProjectMemory(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, *memory)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := m.hydrateProjectMemories(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (m *MySQLArchive) GetProjectMemory(ctx context.Context, operatorID int64, scopeID, memoryID string) (*model.ProjectMemory, error) {
	query := projectMemorySelect + ` JOIN project_scopes s ON s.project_scope_id=m.project_scope_id
WHERE s.operator_id=? AND s.project_scope_id=? AND m.memory_id=?`
	memory, err := m.scanProjectMemory(m.db.QueryRowContext(ctx, query, operatorID, scopeID, memoryID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := m.hydrateProjectMemory(ctx, memory); err != nil {
		return nil, err
	}
	return memory, nil
}

func (m *MySQLArchive) ListProjectMemoryVersions(ctx context.Context, operatorID int64, scopeID, logicalID string) ([]model.ProjectMemory, error) {
	query := projectMemorySelect + ` JOIN project_scopes s ON s.project_scope_id=m.project_scope_id
WHERE s.operator_id=? AND s.project_scope_id=? AND m.logical_memory_id=? ORDER BY m.version DESC`
	rows, err := m.db.QueryContext(ctx, query, operatorID, scopeID, logicalID)
	if err != nil {
		return nil, err
	}
	items := []model.ProjectMemory{}
	for rows.Next() {
		memory, err := m.scanProjectMemory(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, *memory)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := m.hydrateProjectMemories(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (m *MySQLArchive) MarkProjectMemoryDeleted(ctx context.Context, operatorID int64, scopeID, memoryID string, expectedVersion int64) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentVersion int64
	var currentStatus model.ProjectMemoryStatus
	if err := tx.QueryRowContext(ctx, `SELECT m.version, m.status FROM project_memories m
JOIN project_scopes s ON s.project_scope_id=m.project_scope_id
WHERE s.operator_id=? AND s.project_scope_id=? AND m.memory_id=? FOR UPDATE`,
		operatorID, scopeID, memoryID).Scan(&currentVersion, &currentStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("project memory not found")
		}
		return err
	}
	if expectedVersion > 0 && currentVersion != expectedVersion {
		return ErrProjectMemoryRevisionConflict
	}
	if currentStatus == model.ProjectMemorySuperseded {
		return ErrProjectMemoryRevisionConflict
	}
	if currentStatus == model.ProjectMemoryDeleted {
		return nil
	}
	result, err := tx.ExecContext(ctx, `UPDATE project_memories m JOIN project_scopes s
ON s.project_scope_id=m.project_scope_id SET m.status='deleted',
m.statement_text=IF(m.`+projectMemorySensitiveColumn+`=1, '', m.statement_text), m.updated_at=?
WHERE s.operator_id=? AND s.project_scope_id=? AND m.memory_id=?`,
		time.Now().UTC(), operatorID, scopeID, memoryID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return errors.New("project memory not found")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_memory_sources src
JOIN project_memories m ON m.memory_id=src.memory_id
SET src.excerpt_text=IF(m.`+projectMemorySensitiveColumn+`=1, '', src.excerpt_text)
WHERE m.memory_id=?`, memoryID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_scopes SET revision=revision+1,
updated_at=? WHERE operator_id=? AND project_scope_id=?`, time.Now().UTC(), operatorID, scopeID); err != nil {
		return err
	}
	return tx.Commit()
}

type projectMemoryExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertProjectMemoryJob(ctx context.Context, execer projectMemoryExecer, job model.ProjectMemoryJob) error {
	job = normalizeProjectMemoryJob(job)
	_, err := execer.ExecContext(ctx, `INSERT INTO project_memory_jobs (
	job_id, operator_id, machine_id, project_scope_id, trigger_type, status, cursor_start,
	cursor_end, cursor_committed, checkpoint_cursor, checkpoint_progress, checkpoint_at,
	pending_cursor_end, attempt, lease_owner, lease_expires_at, fencing_token,
	input_revision, result_revision, error_text, manual_visible, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.OperatorID, job.MachineID, job.ScopeID, job.Trigger, job.Status,
		job.CursorStart, job.CursorEnd, job.CursorCommitted, job.CheckpointCursor,
		job.CheckpointProgress, nullableProjectMemoryTime(job.CheckpointAt), job.PendingCursorEnd,
		job.Attempt, job.LeaseOwner,
		nullableProjectMemoryTime(job.LeaseExpiresAt), job.FencingToken, job.InputRevision,
		job.ResultRevision, job.Error, job.ManualVisible, job.CreatedAt, job.UpdatedAt)
	return err
}

func (m *MySQLArchive) CreateProjectMemoryJob(ctx context.Context, job model.ProjectMemoryJob) error {
	return insertProjectMemoryJob(ctx, m.db, job)
}

func (m *MySQLArchive) ScheduleProjectMemoryJob(ctx context.Context, job model.ProjectMemoryJob) (*model.ProjectMemoryJob, bool, error) {
	job = normalizeProjectMemoryJob(job)
	if job.ID == "" || job.OperatorID == 0 || job.MachineID == "" || job.ScopeID == "" {
		return nil, false, errors.New("invalid project memory job")
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM project_scopes
WHERE operator_id=? AND machine_id=? AND project_scope_id=? FOR UPDATE`,
		job.OperatorID, job.MachineID, job.ScopeID).Scan(&revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, errors.New("project scope not found")
		}
		return nil, false, err
	}
	existing, err := scanProjectMemoryJob(tx.QueryRowContext(ctx, projectMemoryJobSelect+`
WHERE operator_id=? AND project_scope_id=? AND status IN ('queued','claimed','running')
ORDER BY created_at LIMIT 1 FOR UPDATE`, job.OperatorID, job.ScopeID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	if existing != nil {
		if job.ManualVisible {
			existing.ManualVisible = true
		}
		if existing.Status == model.ProjectMemoryJobQueued {
			if existing.CursorStart == "" {
				existing.CursorStart = job.CursorStart
			}
			if job.CursorEnd != "" {
				existing.CursorEnd = job.CursorEnd
			}
		} else if job.CursorEnd != "" && job.CursorEnd != existing.CursorEnd {
			existing.PendingCursorEnd = job.CursorEnd
		}
		existing.UpdatedAt = time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `UPDATE project_memory_jobs SET cursor_start=?, cursor_end=?,
pending_cursor_end=?, manual_visible=?, updated_at=? WHERE job_id=?`, existing.CursorStart,
			existing.CursorEnd, existing.PendingCursorEnd, existing.ManualVisible, existing.UpdatedAt, existing.ID); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return existing, false, nil
	}
	job.InputRevision = revision
	if err := insertProjectMemoryJob(ctx, tx, job); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return &job, true, nil
}

func scanProjectMemoryJob(scanner interface{ Scan(...any) error }) (*model.ProjectMemoryJob, error) {
	var job model.ProjectMemoryJob
	var checkpointAt, lease sql.NullTime
	err := scanner.Scan(&job.ID, &job.OperatorID, &job.MachineID, &job.ScopeID, &job.Trigger,
		&job.Status, &job.CursorStart, &job.CursorEnd, &job.CursorCommitted,
		&job.CheckpointCursor, &job.CheckpointProgress, &checkpointAt, &job.PendingCursorEnd, &job.Attempt,
		&job.LeaseOwner, &lease, &job.FencingToken, &job.InputRevision, &job.ResultRevision,
		&job.Error, &job.ManualVisible, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if lease.Valid {
		job.LeaseExpiresAt = lease.Time
	}
	if checkpointAt.Valid {
		job.CheckpointAt = checkpointAt.Time
	}
	return &job, nil
}

const projectMemoryJobSelect = `SELECT job_id, operator_id, machine_id, project_scope_id,
	trigger_type, status, cursor_start, cursor_end, cursor_committed, checkpoint_cursor,
	checkpoint_progress, checkpoint_at, pending_cursor_end, attempt, lease_owner,
lease_expires_at, fencing_token, input_revision, result_revision, error_text,
manual_visible, created_at, updated_at FROM project_memory_jobs`

func (m *MySQLArchive) GetProjectMemoryJob(ctx context.Context, operatorID int64, jobID string) (*model.ProjectMemoryJob, error) {
	job, err := scanProjectMemoryJob(m.db.QueryRowContext(ctx, projectMemoryJobSelect+` WHERE operator_id=? AND job_id=?`, operatorID, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return job, err
}

func (m *MySQLArchive) ListProjectMemoryJobs(ctx context.Context, operatorID int64, scopeID string, limit int) ([]model.ProjectMemoryJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := m.db.QueryContext(ctx, projectMemoryJobSelect+` WHERE operator_id=? AND project_scope_id=? ORDER BY updated_at DESC LIMIT ?`, operatorID, scopeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ProjectMemoryJob{}
	for rows.Next() {
		job, err := scanProjectMemoryJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) ListClaimableProjectMemoryJobs(ctx context.Context, now time.Time, limit int) ([]model.ProjectMemoryJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	quietCutoff := now.Add(-projectMemoryAutoQuietWindow)
	rows, err := m.db.QueryContext(ctx, projectMemoryJobSelect+` WHERE
	((status='queued' AND (
	  (attempt=0 AND (manual_visible=1 OR trigger_type NOT IN ('task_complete','goal_checkpoint') OR updated_at<=?))
	  OR (attempt>0 AND updated_at<=DATE_SUB(?, INTERVAL LEAST(POW(2,attempt),64) SECOND))))
	OR (status IN ('claimed','running') AND lease_expires_at IS NOT NULL AND lease_expires_at<?))
	ORDER BY created_at LIMIT ?`, quietCutoff, now, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ProjectMemoryJob{}
	for rows.Next() {
		job, err := scanProjectMemoryJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) ClaimProjectMemoryJob(ctx context.Context, jobID, owner string, now, leaseUntil time.Time, maxPerDevice int) (*model.ProjectMemoryJob, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	job, err := scanProjectMemoryJob(tx.QueryRowContext(ctx, projectMemoryJobSelect+` WHERE job_id=? FOR UPDATE`, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	claimable := job.Status == model.ProjectMemoryJobQueued ||
		(!job.LeaseExpiresAt.IsZero() && job.LeaseExpiresAt.Before(now) && job.Status != model.ProjectMemoryJobCompleted && job.Status != model.ProjectMemoryJobCancelled)
	if !claimable {
		return nil, nil
	}
	if maxPerDevice > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_memory_device_locks
(operator_id, machine_id, touched_at) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE touched_at=VALUES(touched_at)`,
			job.OperatorID, job.MachineID, now); err != nil {
			return nil, err
		}
		var lockMachine string
		if err := tx.QueryRowContext(ctx, `SELECT machine_id FROM project_memory_device_locks
WHERE operator_id=? AND machine_id=? FOR UPDATE`, job.OperatorID, job.MachineID).Scan(&lockMachine); err != nil {
			return nil, err
		}
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_memory_jobs
WHERE operator_id=? AND machine_id=? AND job_id<>? AND status IN ('claimed','running')
AND lease_expires_at IS NOT NULL AND lease_expires_at>=?`, job.OperatorID, job.MachineID, job.ID, now).Scan(&active); err != nil {
			return nil, err
		}
		if active >= maxPerDevice {
			return nil, nil
		}
	}
	job.Status = model.ProjectMemoryJobClaimed
	job.LeaseOwner = owner
	job.LeaseExpiresAt = leaseUntil
	job.FencingToken++
	job.Attempt++
	job.UpdatedAt = now.UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE project_memory_jobs SET status=?, lease_owner=?,
lease_expires_at=?, fencing_token=?, attempt=?, updated_at=? WHERE job_id=?`,
		job.Status, job.LeaseOwner, job.LeaseExpiresAt, job.FencingToken, job.Attempt,
		job.UpdatedAt, job.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return job, nil
}

func (m *MySQLArchive) RenewProjectMemoryJob(ctx context.Context, jobID, owner string, fencingToken int64, leaseUntil time.Time) (bool, error) {
	now := time.Now().UTC()
	result, err := m.db.ExecContext(ctx, `UPDATE project_memory_jobs SET lease_expires_at=?, updated_at=?
WHERE job_id=? AND lease_owner=? AND fencing_token=? AND status IN ('claimed','running')
AND lease_expires_at IS NOT NULL AND lease_expires_at>=?`,
		leaseUntil, now, jobID, owner, fencingToken, now)
	if err != nil {
		return false, err
	}
	count, _ := result.RowsAffected()
	return count == 1, nil
}

func (m *MySQLArchive) UpdateProjectMemoryJob(ctx context.Context, job model.ProjectMemoryJob) error {
	result, err := m.db.ExecContext(ctx, `UPDATE project_memory_jobs SET status=?, cursor_start=?,
	cursor_end=?, cursor_committed=?, checkpoint_cursor=?, checkpoint_progress=?, checkpoint_at=?,
	pending_cursor_end=?, lease_owner=?, lease_expires_at=?, input_revision=?,
	result_revision=?, error_text=?, manual_visible=?, updated_at=?
	WHERE job_id=? AND fencing_token=?`, job.Status, job.CursorStart, job.CursorEnd,
		job.CursorCommitted, job.CheckpointCursor, job.CheckpointProgress,
		nullableProjectMemoryTime(job.CheckpointAt), job.PendingCursorEnd,
		job.LeaseOwner, nullableProjectMemoryTime(job.LeaseExpiresAt),
		job.InputRevision, job.ResultRevision, job.Error, job.ManualVisible,
		time.Now().UTC(), job.ID, job.FencingToken)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrProjectMemoryStaleJob
	}
	return nil
}

func (m *MySQLArchive) FinalizeProjectMemoryJob(ctx context.Context, job model.ProjectMemoryJob, successorID string) (*model.ProjectMemoryJob, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM project_scopes
WHERE operator_id=? AND machine_id=? AND project_scope_id=? FOR UPDATE`,
		job.OperatorID, job.MachineID, job.ScopeID).Scan(&revision); err != nil {
		return nil, err
	}
	current, err := scanProjectMemoryJob(tx.QueryRowContext(ctx, projectMemoryJobSelect+`
WHERE operator_id=? AND project_scope_id=? AND job_id=? FOR UPDATE`, job.OperatorID, job.ScopeID, job.ID))
	if err != nil {
		return nil, err
	}
	if current.FencingToken != job.FencingToken {
		return nil, ErrProjectMemoryStaleJob
	}
	// Scheduling may attach a manual request or extend the cursor after the
	// worker loaded its snapshot. Preserve those fields from the locked row.
	job.ManualVisible = job.ManualVisible || current.ManualVisible
	pending := strings.TrimSpace(current.PendingCursorEnd)
	if pending == "" {
		pending = strings.TrimSpace(job.PendingCursorEnd)
	}
	job.PendingCursorEnd = ""
	job.UpdatedAt = time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE project_memory_jobs SET status=?, cursor_start=?,
cursor_end=?, cursor_committed=?, checkpoint_cursor=?, checkpoint_progress=?, checkpoint_at=?,
pending_cursor_end='', lease_owner=?, lease_expires_at=?, input_revision=?, result_revision=?,
error_text=?, manual_visible=?, updated_at=? WHERE job_id=? AND fencing_token=?`,
		job.Status, job.CursorStart, job.CursorEnd, job.CursorCommitted, job.CheckpointCursor,
		job.CheckpointProgress, nullableProjectMemoryTime(job.CheckpointAt), job.LeaseOwner,
		nullableProjectMemoryTime(job.LeaseExpiresAt), job.InputRevision, job.ResultRevision,
		job.Error, job.ManualVisible, job.UpdatedAt, job.ID, job.FencingToken)
	if err != nil {
		return nil, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return nil, ErrProjectMemoryStaleJob
	}
	var successor *model.ProjectMemoryJob
	if pending != "" {
		next := normalizeProjectMemoryJob(model.ProjectMemoryJob{
			ID: successorID, OperatorID: job.OperatorID, MachineID: job.MachineID, ScopeID: job.ScopeID,
			Trigger: model.ProjectMemoryTriggerTaskComplete, Status: model.ProjectMemoryJobQueued,
			CursorStart: job.CursorCommitted, CursorEnd: pending, InputRevision: revision,
		})
		if err := insertProjectMemoryJob(ctx, tx, next); err != nil {
			return nil, err
		}
		successor = &next
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return successor, nil
}

func (m *MySQLArchive) MarkProjectMemoryJobManualVisible(ctx context.Context, operatorID int64, scopeID, jobID string) (bool, error) {
	result, err := m.db.ExecContext(ctx, `UPDATE project_memory_jobs SET manual_visible=1, updated_at=?
WHERE operator_id=? AND project_scope_id=? AND job_id=? AND status IN ('queued','claimed','running')`,
		time.Now().UTC(), operatorID, scopeID, jobID)
	if err != nil {
		return false, err
	}
	count, _ := result.RowsAffected()
	return count == 1, nil
}

func (m *MySQLArchive) AppendProjectMemoryJobEvent(ctx context.Context, event model.ProjectMemoryJobEvent) error {
	metadata, err := marshalProjectMemoryJSON(event.Metadata)
	if err != nil {
		return err
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockedJob string
	if err := tx.QueryRowContext(ctx, `SELECT job_id FROM project_memory_jobs WHERE job_id=? FOR UPDATE`, event.JobID).Scan(&lockedJob); err != nil {
		return err
	}
	if event.Sequence <= 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence_value),0)+1 FROM project_memory_job_events WHERE job_id=?`, event.JobID).Scan(&event.Sequence); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_memory_job_events
(job_id, sequence_value, event_type, message_text, progress_value, metadata_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, event.JobID, event.Sequence, event.Type, event.Message,
		event.Progress, metadata, event.CreatedAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (m *MySQLArchive) ListProjectMemoryJobEvents(ctx context.Context, operatorID int64, jobID string, afterSequence int64, visibleOnly bool) ([]model.ProjectMemoryJobEvent, error) {
	query := `SELECT e.id, e.job_id, e.sequence_value, e.event_type, e.message_text,
e.progress_value, e.metadata_json, e.created_at FROM project_memory_job_events e
JOIN project_memory_jobs j ON j.job_id=e.job_id WHERE j.operator_id=? AND e.job_id=?
AND e.sequence_value>?`
	if visibleOnly {
		query += ` AND j.manual_visible=1`
	}
	query += ` ORDER BY e.sequence_value`
	rows, err := m.db.QueryContext(ctx, query, operatorID, jobID, afterSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ProjectMemoryJobEvent{}
	for rows.Next() {
		var event model.ProjectMemoryJobEvent
		var metadata []byte
		if err := rows.Scan(&event.ID, &event.JobID, &event.Sequence, &event.Type,
			&event.Message, &event.Progress, &metadata, &event.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metadata, &event.Metadata)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) mergeProjectMemorySourcesTx(ctx context.Context, tx *sql.Tx, memoryID string, incoming []model.ProjectMemorySource) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT source_order, session_id, task_id, message_ids_json,
tool_call_ids_json, relative_path, sha256 FROM project_memory_sources WHERE memory_id=? ORDER BY source_order`, memoryID)
	if err != nil {
		return false, err
	}
	seen := map[string]struct{}{}
	nextOrder := 0
	for rows.Next() {
		var order int
		var source model.ProjectMemorySource
		var messageIDs, toolIDs []byte
		if err := rows.Scan(&order, &source.SessionID, &source.TaskID, &messageIDs, &toolIDs,
			&source.RelativePath, &source.SHA256); err != nil {
			rows.Close()
			return false, err
		}
		_ = json.Unmarshal(messageIDs, &source.MessageIDs)
		_ = json.Unmarshal(toolIDs, &source.ToolCallIDs)
		seen[projectMemorySourceKey(source)] = struct{}{}
		if order >= nextOrder {
			nextOrder = order + 1
		}
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	changed := false
	for _, source := range incoming {
		if nextOrder >= 32 {
			break
		}
		key := projectMemorySourceKey(source)
		if _, found := seen[key]; found {
			continue
		}
		messageIDs, err := marshalProjectMemoryJSON(source.MessageIDs)
		if err != nil {
			return false, err
		}
		toolIDs, err := marshalProjectMemoryJSON(source.ToolCallIDs)
		if err != nil {
			return false, err
		}
		excerpt, err := m.projectMemoryCipher.encrypt(source.Excerpt, "project_memory_source.excerpt")
		if err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_memory_sources
(memory_id, source_order, session_id, task_id, message_ids_json, tool_call_ids_json,
relative_path, sha256, excerpt_text, source_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			memoryID, nextOrder, source.SessionID, source.TaskID, messageIDs, toolIDs,
			source.RelativePath, source.SHA256, excerpt, source.Status); err != nil {
			return false, err
		}
		seen[key] = struct{}{}
		nextOrder++
		changed = true
	}
	if changed {
		_, err = tx.ExecContext(ctx, `UPDATE project_memories SET updated_at=? WHERE memory_id=?`, time.Now().UTC(), memoryID)
	}
	return changed, err
}

func (m *MySQLArchive) ReconcileProjectMemories(ctx context.Context, operatorID int64, scopeID string, expectedRevision int64, candidates []model.ProjectMemoryCandidate, invalidations []model.ProjectMemoryInvalidation, brief *model.ProjectMemoryBriefCandidate) (model.ProjectMemoryReconcileResult, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return model.ProjectMemoryReconcileResult{}, err
	}
	defer tx.Rollback()
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM project_scopes WHERE operator_id=? AND project_scope_id=? FOR UPDATE`, operatorID, scopeID).Scan(&revision); err != nil {
		return model.ProjectMemoryReconcileResult{}, err
	}
	if expectedRevision > 0 && revision != expectedRevision {
		return model.ProjectMemoryReconcileResult{}, ErrProjectMemoryRevisionConflict
	}
	result := model.ProjectMemoryReconcileResult{Revision: revision, AcceptedIDs: []string{}, InvalidatedIDs: []string{}}
	for _, invalidation := range invalidations {
		var contentHash, createdBy string
		var status model.ProjectMemoryStatus
		var kind model.ProjectMemoryKind
		var locked bool
		err := tx.QueryRowContext(ctx, `SELECT content_hash, status, locked, created_by, kind
		FROM project_memories WHERE project_scope_id=? AND memory_id=? FOR UPDATE`, scopeID, invalidation.MemoryID).
			Scan(&contentHash, &status, &locked, &createdBy, &kind)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return model.ProjectMemoryReconcileResult{}, err
		}
		if contentHash != invalidation.ContentHash ||
			(status != model.ProjectMemoryActive && status != model.ProjectMemoryDisputed) || kind == model.ProjectMemoryLockedRule {
			continue
		}
		target := invalidation.Status
		if locked || createdBy == "user" {
			target = model.ProjectMemoryDisputed
		}
		if target != model.ProjectMemoryStale && target != model.ProjectMemoryDisputed {
			continue
		}
		if status == target {
			continue
		}
		now := time.Now().UTC()
		if target == model.ProjectMemoryStale {
			evidence, marshalErr := marshalProjectMemoryJSON(invalidation.EvidenceRefs)
			if marshalErr != nil {
				return model.ProjectMemoryReconcileResult{}, marshalErr
			}
			if _, err := tx.ExecContext(ctx, `UPDATE project_memories SET status='stale', verification_status='stale',
			verification_method='curator_invalidation', verification_evidence_json=?, updated_at=? WHERE memory_id=?`,
				evidence, now, invalidation.MemoryID); err != nil {
				return model.ProjectMemoryReconcileResult{}, err
			}
		} else if _, err := tx.ExecContext(ctx, `UPDATE project_memories SET status='disputed', updated_at=? WHERE memory_id=?`, now, invalidation.MemoryID); err != nil {
			return model.ProjectMemoryReconcileResult{}, err
		}
		result.InvalidatedIDs = append(result.InvalidatedIDs, invalidation.MemoryID)
		result.Changed = true
	}
	for _, candidate := range candidates {
		memory := normalizeProjectMemory(model.ProjectMemory{
			ID: newProjectMemoryID("mem"), LogicalID: newProjectMemoryID("logical"), ScopeID: scopeID,
			Kind: candidate.Kind, SubjectKey: candidate.SubjectKey, Statement: candidate.Statement,
			Confidence: candidate.Confidence, ScopePaths: candidate.ScopePaths, Artifacts: candidate.Artifacts,
			Sources: candidate.Sources, Verification: candidate.Verification, Sensitive: candidate.Sensitive,
			CreatedBy: "curator",
		})
		if memory.SubjectKey == "" || memory.Statement == "" {
			continue
		}
		for _, artifact := range memory.Artifacts {
			if artifact.SHA256 == "" && artifact.APKSHA256 == "" {
				continue
			}
			resultStale, err := tx.ExecContext(ctx, `UPDATE project_memories old
JOIN project_memory_artifacts a ON a.memory_id=old.memory_id
SET old.status='stale', old.verification_status='stale', old.updated_at=?
			WHERE old.project_scope_id=? AND old.status IN ('active','disputed') AND (
			  (a.sha256<>'' AND ?<>'' AND a.sha256<>? AND ((?<>'' AND a.module_name=?) OR (?<>'' AND a.relative_path=?)))
			  OR (a.apk_sha256<>'' AND ?<>'' AND a.apk_sha256<>? AND ?<>'' AND a.package_name=?))`,
				time.Now().UTC(), scopeID,
				artifact.SHA256, artifact.SHA256, artifact.ModuleName, artifact.ModuleName, artifact.Path, artifact.Path,
				artifact.APKSHA256, artifact.APKSHA256, artifact.PackageName, artifact.PackageName)
			if err != nil {
				return model.ProjectMemoryReconcileResult{}, err
			}
			if count, _ := resultStale.RowsAffected(); count > 0 {
				result.Changed = true
			}
		}
		var existingID, existingHash, existingLogical, existingCreatedBy string
		var existingStatus model.ProjectMemoryStatus
		var existingVerification model.ProjectMemoryVerificationStatus
		var existingLocked bool
		var existingUpdatedAt time.Time
		err := tx.QueryRowContext(ctx, `SELECT memory_id, content_hash, logical_memory_id, status,
	verification_status, locked, created_by, updated_at FROM project_memories WHERE project_scope_id=? AND subject_key=?
	ORDER BY updated_at DESC LIMIT 1 FOR UPDATE`, scopeID, memory.SubjectKey).
			Scan(&existingID, &existingHash, &existingLogical, &existingStatus, &existingVerification,
				&existingLocked, &existingCreatedBy, &existingUpdatedAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return model.ProjectMemoryReconcileResult{}, err
		}
		if err == nil && existingStatus == model.ProjectMemoryDeleted {
			continue
		}
		if err == nil && existingHash == memory.ContentHash {
			changed, mergeErr := m.mergeProjectMemorySourcesTx(ctx, tx, existingID, memory.Sources)
			if mergeErr != nil {
				return model.ProjectMemoryReconcileResult{}, mergeErr
			}
			result.Changed = result.Changed || changed
			result.AcceptedIDs = append(result.AcceptedIDs, existingID)
			continue
		}
		if err == nil {
			memory.LogicalID = existingLogical
			memory.SupersedesID = existingID
			if existingStatus == model.ProjectMemoryStale {
				// A new artifact fingerprint replaces the stale version while preserving history.
			} else if candidateIsNewerHumanEvidence := memory.Verification.Status == model.ProjectMemoryVerified &&
				!memory.Verification.VerifiedAt.IsZero() && memory.Verification.VerifiedAt.After(existingUpdatedAt); existingLocked || (existingCreatedBy == "user" && !candidateIsNewerHumanEvidence) ||
				(existingVerification == model.ProjectMemoryVerified && memory.Verification.Status == model.ProjectMemoryVerified) {
				memory.Status = model.ProjectMemoryDisputed
				if _, err := tx.ExecContext(ctx, `UPDATE project_memories SET status='disputed', updated_at=? WHERE memory_id=?`, time.Now().UTC(), existingID); err != nil {
					return model.ProjectMemoryReconcileResult{}, err
				}
			} else if memory.Verification.Status == model.ProjectMemoryVerified {
				if _, err := tx.ExecContext(ctx, `UPDATE project_memories SET status='superseded', updated_at=? WHERE memory_id=?`, time.Now().UTC(), existingID); err != nil {
					return model.ProjectMemoryReconcileResult{}, err
				}
			}
			_ = existingStatus
		}
		if err := m.upsertProjectMemoryTx(ctx, tx, memory); err != nil {
			return model.ProjectMemoryReconcileResult{}, err
		}
		result.AcceptedIDs = append(result.AcceptedIDs, memory.ID)
		result.Changed = true
	}
	if result.Changed || brief != nil {
		revision++
		if _, err := tx.ExecContext(ctx, `UPDATE project_scopes SET revision=?, updated_at=? WHERE project_scope_id=?`, revision, time.Now().UTC(), scopeID); err != nil {
			return model.ProjectMemoryReconcileResult{}, err
		}
		result.Revision = revision
	}
	if brief != nil && strings.TrimSpace(brief.Content) != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_briefs
(project_scope_id, content_text, token_count, source_revision, model_name, generated_at)
VALUES (?, ?, ?, ?, '', ?) ON DUPLICATE KEY UPDATE content_text=VALUES(content_text),
token_count=VALUES(token_count), source_revision=VALUES(source_revision), generated_at=VALUES(generated_at)`,
			scopeID, brief.Content, 0, revision, time.Now().UTC()); err != nil {
			return model.ProjectMemoryReconcileResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.ProjectMemoryReconcileResult{}, err
	}
	return result, nil
}

func (m *MySQLArchive) GetProjectMemoryBrief(ctx context.Context, operatorID int64, scopeID string) (*model.ProjectMemoryBrief, error) {
	var brief model.ProjectMemoryBrief
	err := m.db.QueryRowContext(ctx, `SELECT b.project_scope_id, b.content_text, b.token_count,
b.source_revision, b.model_name, b.generated_at FROM project_briefs b
JOIN project_scopes s ON s.project_scope_id=b.project_scope_id
WHERE s.operator_id=? AND b.project_scope_id=?`, operatorID, scopeID).
		Scan(&brief.ScopeID, &brief.Content, &brief.TokenCount, &brief.SourceRevision,
			&brief.Model, &brief.GeneratedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &brief, err
}

func (m *MySQLArchive) UpsertProjectMemoryBrief(ctx context.Context, brief model.ProjectMemoryBrief) error {
	_, err := m.db.ExecContext(ctx, `INSERT INTO project_briefs
(project_scope_id, content_text, token_count, source_revision, model_name, generated_at)
VALUES (?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE
content_text=IF(source_revision<=VALUES(source_revision),VALUES(content_text),content_text),
token_count=IF(source_revision<=VALUES(source_revision),VALUES(token_count),token_count),
model_name=IF(source_revision<=VALUES(source_revision),VALUES(model_name),model_name),
generated_at=IF(source_revision<=VALUES(source_revision),VALUES(generated_at),generated_at),
source_revision=GREATEST(source_revision,VALUES(source_revision))`, brief.ScopeID, brief.Content,
		brief.TokenCount, brief.SourceRevision, brief.Model, brief.GeneratedAt)
	return err
}

func (m *MySQLArchive) RecordProjectMemoryUsage(ctx context.Context, usage model.ProjectMemoryUsage) error {
	memoryIDs, err := marshalProjectMemoryJSON(usage.MemoryIDs)
	if err != nil {
		return err
	}
	if usage.CreatedAt.IsZero() {
		usage.CreatedAt = time.Now().UTC()
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO project_memory_usages
(project_scope_id, task_id, revision, memory_ids_json, token_count, created_at)
VALUES (?, ?, ?, ?, ?, ?)`, usage.ScopeID, usage.TaskID, usage.Revision, memoryIDs,
		usage.TokenCount, usage.CreatedAt)
	return err
}

func (m *MySQLArchive) RecordProjectMemoryAudit(ctx context.Context, audit model.ProjectMemoryAudit) error {
	metadata, err := marshalProjectMemoryJSON(audit.Metadata)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO project_memory_audits
(project_scope_id, memory_id, job_id, operator_id, action_name, metadata_json)
VALUES (?, ?, ?, ?, ?, ?)`, audit.ScopeID, audit.MemoryID, audit.JobID,
		audit.OperatorID, audit.Action, metadata)
	return err
}

func (m *MySQLArchive) UpdateProjectMemorySourceAvailability(ctx context.Context, operatorID int64, scopeID string, updates []model.ProjectMemorySourceAvailability) (int64, error) {
	if len(updates) == 0 {
		return 0, nil
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM project_scopes WHERE operator_id=? AND project_scope_id=? FOR UPDATE`, operatorID, scopeID).Scan(&revision); err != nil {
		return 0, err
	}
	var changed int64
	for _, update := range updates {
		result, err := tx.ExecContext(ctx, `UPDATE project_memory_sources s
JOIN project_memories m ON m.memory_id=s.memory_id
SET s.source_status=?, s.sha256=IF(?='',s.sha256,?)
WHERE m.project_scope_id=? AND s.memory_id=? AND s.relative_path=?
AND (s.source_status<>? OR (?<>'' AND s.sha256<>?))`,
			update.Status, update.SHA256, update.SHA256, scopeID, update.MemoryID,
			update.RelativePath, update.Status, update.SHA256, update.SHA256)
		if err != nil {
			return 0, err
		}
		count, _ := result.RowsAffected()
		changed += count
	}
	if changed > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE project_scopes SET revision=revision+1, updated_at=? WHERE operator_id=? AND project_scope_id=?`, time.Now().UTC(), operatorID, scopeID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}

func (m *MySQLArchive) MaintainProjectMemoryRetention(ctx context.Context, activeLimit, historyLimit, maxScopes int) (model.ProjectMemoryRetentionResult, error) {
	if activeLimit <= 0 {
		activeLimit = 20_000
	}
	if historyLimit <= 0 {
		historyLimit = 200_000
	}
	if maxScopes <= 0 {
		maxScopes = 100
	}
	rows, err := m.db.QueryContext(ctx, `SELECT project_scope_id FROM project_scopes WHERE status<>'deleted' ORDER BY updated_at LIMIT ?`, maxScopes)
	if err != nil {
		return model.ProjectMemoryRetentionResult{}, err
	}
	scopeIDs := []string{}
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			rows.Close()
			return model.ProjectMemoryRetentionResult{}, err
		}
		scopeIDs = append(scopeIDs, scopeID)
	}
	if err := rows.Close(); err != nil {
		return model.ProjectMemoryRetentionResult{}, err
	}
	result := model.ProjectMemoryRetentionResult{}
	for _, scopeID := range scopeIDs {
		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return result, err
		}
		var revision int64
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM project_scopes WHERE project_scope_id=? FOR UPDATE`, scopeID).Scan(&revision); err != nil {
			tx.Rollback()
			return result, err
		}
		changed := false
		loadOldest := func(where string, limit int) ([]model.ProjectMemory, error) {
			query := `SELECT memory_id, kind, subject_key, statement_text, status, locked, updated_at
FROM project_memories WHERE project_scope_id=? AND ` + where + ` ORDER BY updated_at, memory_id LIMIT ? FOR UPDATE`
			selected, err := tx.QueryContext(ctx, query, scopeID, limit)
			if err != nil {
				return nil, err
			}
			items := []model.ProjectMemory{}
			for selected.Next() {
				var item model.ProjectMemory
				item.ScopeID = scopeID
				if err := selected.Scan(&item.ID, &item.Kind, &item.SubjectKey, &item.Statement, &item.Status, &item.Locked, &item.UpdatedAt); err != nil {
					selected.Close()
					return nil, err
				}
				items = append(items, item)
			}
			return items, selected.Close()
		}

		var historyCount int
		historyWhere := `locked=0 AND kind<>'locked_rule' AND status NOT IN ('active','disputed','deleted')`
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_memories WHERE project_scope_id=? AND `+historyWhere, scopeID).Scan(&historyCount); err != nil {
			tx.Rollback()
			return result, err
		}
		if historyCount > historyLimit {
			count := historyCount - historyLimit
			if count > 1_000 {
				count = 1_000
			}
			items, err := loadOldest(historyWhere, count)
			if err != nil {
				tx.Rollback()
				return result, err
			}
			for _, item := range items {
				if _, err := tx.ExecContext(ctx, `DELETE FROM project_memory_sources WHERE memory_id=?`, item.ID); err != nil {
					tx.Rollback()
					return result, err
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM project_memory_artifacts WHERE memory_id=?`, item.ID); err != nil {
					tx.Rollback()
					return result, err
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM project_memories WHERE memory_id=?`, item.ID); err != nil {
					tx.Rollback()
					return result, err
				}
			}
			if len(items) > 0 {
				if err := m.upsertProjectMemoryTx(ctx, tx, projectMemoryRollup(scopeID, items, "history")); err != nil {
					tx.Rollback()
					return result, err
				}
				result.PurgedHistory += int64(len(items))
				result.RolledUp++
				changed = true
			}
		}

		var activeCount int
		activeWhere := `locked=0 AND kind<>'locked_rule' AND status IN ('active','disputed')`
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_memories WHERE project_scope_id=? AND `+activeWhere, scopeID).Scan(&activeCount); err != nil {
			tx.Rollback()
			return result, err
		}
		if activeCount > activeLimit {
			count := activeCount - activeLimit + 1
			if count > 1_000 {
				count = 1_000
			}
			items, err := loadOldest(activeWhere, count)
			if err != nil {
				tx.Rollback()
				return result, err
			}
			for _, item := range items {
				if _, err := tx.ExecContext(ctx, `UPDATE project_memories SET status='archived', updated_at=? WHERE memory_id=?`, time.Now().UTC(), item.ID); err != nil {
					tx.Rollback()
					return result, err
				}
			}
			if len(items) > 0 {
				if err := m.upsertProjectMemoryTx(ctx, tx, projectMemoryRollup(scopeID, items, "active")); err != nil {
					tx.Rollback()
					return result, err
				}
				result.Archived += int64(len(items))
				result.RolledUp++
				changed = true
			}
		}
		if changed {
			if _, err := tx.ExecContext(ctx, `UPDATE project_scopes SET revision=revision+1, updated_at=? WHERE project_scope_id=?`, time.Now().UTC(), scopeID); err != nil {
				tx.Rollback()
				return result, err
			}
		}
		if err := tx.Commit(); err != nil {
			return result, err
		}
		result.ScopesProcessed++
	}
	return result, nil
}

func (m *MySQLArchive) PurgeDeletedProjectMemories(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > 10_000 {
		limit = 1_000
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT memory_id FROM project_memories
WHERE status='deleted' AND updated_at<? ORDER BY updated_at LIMIT ? FOR UPDATE`, before, limit)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil || len(ids) == 0 {
		return 0, err
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for index, id := range ids {
		args[index] = id
	}
	for _, table := range []string{"project_memory_sources", "project_memory_artifacts"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE memory_id IN (`+placeholders+`)`, args...); err != nil {
			return 0, err
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM project_memories WHERE memory_id IN (`+placeholders+`) AND status='deleted' AND updated_at<?`, append(args, before)...)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	count, _ := result.RowsAffected()
	return count, nil
}

var _ ProjectMemoryArchive = (*MySQLArchive)(nil)

func (m *MySQLArchive) projectMemoryDebugSummary(ctx context.Context, scopeID string) (string, error) {
	var count int
	if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_memories WHERE project_scope_id=?`, scopeID).Scan(&count); err != nil {
		return "", err
	}
	return fmt.Sprintf("scope=%s memories=%d", scopeID, count), nil
}
