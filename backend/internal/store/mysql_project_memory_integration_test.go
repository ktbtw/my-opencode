package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"relay-server/internal/model"
)

type mysqlProjectMemoryIntegrationFixture struct {
	cfg      MySQLConfig
	serverDB *sql.DB
	archive  *MySQLArchive
}

func newMySQLProjectMemoryIntegrationFixture(t *testing.T, prepare func(*sql.DB)) *mysqlProjectMemoryIntegrationFixture {
	t.Helper()
	if os.Getenv("PROJECT_MEMORY_MYSQL_INTEGRATION") != "1" {
		t.Skip("set PROJECT_MEMORY_MYSQL_INTEGRATION=1 to run the MySQL integration test")
	}
	cfg := MySQLConfig{
		Host:     env("MYSQL_HOST", "127.0.0.1"),
		Port:     envInt("MYSQL_PORT", 3306),
		User:     env("MYSQL_USER", "root"),
		Password: env("MYSQL_PASSWORD", "ymh20040825"),
		Database: fmt.Sprintf("chat_codex_pm_it_%d", time.Now().UnixNano()),
	}
	serverDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8mb4&parseTime=true&multiStatements=true", cfg.User, cfg.Password, cfg.Host, cfg.Port)
	serverDB, err := sql.Open("mysql", serverDSN)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := serverDB.Exec("CREATE DATABASE " + quoteIdent(cfg.Database) + " CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci"); err != nil {
		serverDB.Close()
		t.Fatal(err)
	}
	if prepare != nil {
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&multiStatements=true", cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			t.Fatal(err)
		}
		prepare(db)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	archive, err := NewMySQLArchive(ctx, cfg)
	if err != nil {
		serverDB.Exec("DROP DATABASE IF EXISTS " + quoteIdent(cfg.Database))
		serverDB.Close()
		t.Fatal(err)
	}
	fixture := &mysqlProjectMemoryIntegrationFixture{cfg: cfg, serverDB: serverDB, archive: archive}
	t.Cleanup(func() {
		if fixture.archive != nil {
			_ = fixture.archive.db.Close()
		}
		if _, err := fixture.serverDB.Exec("DROP DATABASE IF EXISTS " + quoteIdent(cfg.Database)); err != nil {
			t.Errorf("drop integration database: %v", err)
		}
		_ = fixture.serverDB.Close()
	})
	return fixture
}

func seedMySQLProjectScope(t *testing.T, archive *MySQLArchive, operatorID int64, machineID, scopeID string) model.ProjectScope {
	t.Helper()
	scope := model.ProjectScope{
		ID: scopeID, OperatorID: operatorID, MachineID: machineID,
		DisplayName: scopeID, Status: model.ProjectScopeActive, Revision: 1,
	}
	if err := archive.UpsertProjectScope(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	return scope
}

func TestMySQLProjectMemorySourceAndRetentionIntegration(t *testing.T) {
	if os.Getenv("PROJECT_MEMORY_MYSQL_INTEGRATION") != "1" {
		t.Skip("set PROJECT_MEMORY_MYSQL_INTEGRATION=1 to run the MySQL integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cfg := MySQLConfig{
		Host:     env("MYSQL_HOST", "127.0.0.1"),
		Port:     envInt("MYSQL_PORT", 3306),
		User:     env("MYSQL_USER", "root"),
		Password: env("MYSQL_PASSWORD", "ymh20040825"),
		Database: fmt.Sprintf("chat_codex_pm_it_%d", time.Now().UnixNano()),
	}
	serverDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8mb4&parseTime=true&multiStatements=true", cfg.User, cfg.Password, cfg.Host, cfg.Port)
	serverDB, err := sql.Open("mysql", serverDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer serverDB.Close()
	defer func() {
		if _, err := serverDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+quoteIdent(cfg.Database)); err != nil {
			t.Errorf("drop integration database: %v", err)
		}
	}()

	archive, err := NewMySQLArchive(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.db.Close()
	if err := ensureSchema(ctx, archive.db); err != nil {
		t.Fatalf("repeat schema migration: %v", err)
	}

	scope := model.ProjectScope{
		ID: "scope-integration", OperatorID: 1, MachineID: "machine-integration",
		DisplayName: "integration", Status: model.ProjectScopeActive, Revision: 1,
	}
	if err := archive.UpsertProjectScope(ctx, scope); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Hour)
	statuses := []model.ProjectMemoryStatus{
		model.ProjectMemoryActive,
		model.ProjectMemoryActive,
		model.ProjectMemoryActive,
		model.ProjectMemoryArchived,
		model.ProjectMemoryStale,
		model.ProjectMemorySuperseded,
	}
	for index, status := range statuses {
		memory := model.ProjectMemory{
			ID: fmt.Sprintf("memory-%d", index), LogicalID: fmt.Sprintf("logical-%d", index),
			ScopeID: scope.ID, Kind: model.ProjectMemoryVerifiedFact,
			SubjectKey: fmt.Sprintf("subject-%d", index), Statement: fmt.Sprintf("statement-%d", index),
			Status: status, CreatedAt: now.Add(time.Duration(index) * time.Minute),
			UpdatedAt: now.Add(time.Duration(index) * time.Minute),
		}
		if index == 0 {
			memory.Sources = []model.ProjectMemorySource{{RelativePath: "src/main.go", SHA256: "abc", Status: "available"}}
		}
		if err := archive.UpsertProjectMemory(ctx, memory); err != nil {
			t.Fatal(err)
		}
	}

	changed, err := archive.UpdateProjectMemorySourceAvailability(ctx, 1, scope.ID, []model.ProjectMemorySourceAvailability{{
		MemoryID: "memory-0", RelativePath: "src/main.go", Status: "source_missing",
	}})
	if err != nil || changed != 1 {
		t.Fatalf("source availability: changed=%d err=%v", changed, err)
	}
	result, err := archive.MaintainProjectMemoryRetention(ctx, 2, 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.RolledUp != 2 || result.Archived != 3 || result.PurgedHistory != 1 {
		t.Fatalf("unexpected retention result: %+v", result)
	}
	items, err := archive.ListProjectMemories(ctx, model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: scope.MachineID, ScopeID: scope.ID, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, item := range items {
		if item.Status == model.ProjectMemoryActive || item.Status == model.ProjectMemoryDisputed {
			active++
		}
	}
	if active > 2 {
		t.Fatalf("active retention limit exceeded: active=%d", active)
	}
}

func TestMySQLProjectMemoryLegacySchemaMigrationIntegration(t *testing.T) {
	fixture := newMySQLProjectMemoryIntegrationFixture(t, func(db *sql.DB) {
		statements := []string{
			`CREATE TABLE project_memories (
memory_id VARCHAR(64) PRIMARY KEY,
statement_text TEXT NOT NULL,
search_text TEXT NOT NULL,
` + projectMemorySensitiveColumn + ` TINYINT(1) NOT NULL DEFAULT 0,
content_hash CHAR(64) NOT NULL DEFAULT '',
FULLTEXT KEY ft_project_memory_search (statement_text, search_text)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
			`CREATE TABLE project_scope_locations (location_id VARCHAR(64) PRIMARY KEY, marker_nonce VARCHAR(128) NOT NULL DEFAULT '') ENGINE=InnoDB`,
			`CREATE TABLE project_memory_sources (id BIGINT PRIMARY KEY AUTO_INCREMENT, relative_path VARCHAR(1024) NOT NULL DEFAULT '') ENGINE=InnoDB`,
			`CREATE TABLE project_memory_artifacts (id BIGINT PRIMARY KEY AUTO_INCREMENT, sha256 CHAR(64) NOT NULL DEFAULT '', ida_analysis_status VARCHAR(32) NOT NULL DEFAULT '') ENGINE=InnoDB`,
			`CREATE TABLE project_memory_jobs (job_id VARCHAR(64) PRIMARY KEY, cursor_committed VARCHAR(191) NOT NULL DEFAULT '') ENGINE=InnoDB`,
			`CREATE TABLE task_events (
id BIGINT PRIMARY KEY AUTO_INCREMENT,
task_id VARCHAR(64) NOT NULL,
event_type VARCHAR(64) NOT NULL,
session_id VARCHAR(64) NOT NULL DEFAULT '',
permission_id VARCHAR(64) NOT NULL DEFAULT '',
payload_json JSON NULL,
content_text LONGTEXT NULL,
error_text LONGTEXT NULL,
sent_at DATETIME NOT NULL,
created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		}
		for _, statement := range statements {
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	})

	wantColumns := map[string][]string{
		"project_scope_locations":  {"binding_epoch"},
		"project_memory_sources":   {"sha256"},
		"project_memories":         {"embedding_model", "embedding_version"},
		"project_memory_artifacts": {"package_name", "apk_sha256", "ida_database_id"},
		"project_memory_jobs":      {"checkpoint_cursor", "checkpoint_progress", "checkpoint_at", "pending_cursor_end"},
	}
	for table, columns := range wantColumns {
		for _, column := range columns {
			var count int
			if err := fixture.archive.db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`, table, column).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("migration did not add %s.%s", table, column)
			}
		}
	}
	rows, err := fixture.archive.db.Query(`SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='project_memories' AND INDEX_NAME='ft_project_memory_search'
ORDER BY SEQ_IN_INDEX`)
	if err != nil {
		t.Fatal(err)
	}
	var indexed []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal(err)
		}
		indexed = append(indexed, column)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(indexed) != 1 || indexed[0] != "search_text" {
		t.Fatalf("unexpected FULLTEXT columns after migration: %v", indexed)
	}
	for indexName, wantColumns := range map[string][]string{
		"idx_task_events_task_sent_id":      {"task_id", "sent_at", "id"},
		"idx_task_events_task_id_id":        {"task_id", "id"},
		"idx_task_events_task_type_sent_id": {"task_id", "event_type", "sent_at", "id"},
	} {
		rows, err := fixture.archive.db.Query(`SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='task_events' AND INDEX_NAME=?
ORDER BY SEQ_IN_INDEX`, indexName)
		if err != nil {
			t.Fatal(err)
		}
		var columns []string
		for rows.Next() {
			var column string
			if err := rows.Scan(&column); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			columns = append(columns, column)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if !slicesEqual(columns, wantColumns) {
			t.Fatalf("unexpected task_events index %s columns: got=%v want=%v", indexName, columns, wantColumns)
		}
	}
	if err := ensureSchema(context.Background(), fixture.archive.db); err != nil {
		t.Fatalf("migration is not idempotent: %v", err)
	}
}

func TestMySQLEventCursorPaginationAndSequenceIntegration(t *testing.T) {
	fixture := newMySQLProjectMemoryIntegrationFixture(t, nil)
	archive := fixture.archive
	now := time.Now().UTC().Truncate(time.Second).Add(789 * time.Millisecond)
	task := &model.Task{
		ID: "task-event-cursor", AgentID: "agent", MachineID: "machine",
		ProjectID: "project", ProjectRoot: "/tmp", SessionID: "session",
		Status: model.TaskRunning, CreatedAt: now, UpdatedAt: now, OperatorID: 1,
	}
	if err := archive.UpsertTask(task); err != nil {
		t.Fatal(err)
	}
	for index, content := range []string{"one", "two", "three"} {
		event := &model.Event{
			TaskID: task.ID, Type: "delta", Content: content,
			Sequence: int64(index + 1), SentAt: now,
		}
		if err := archive.AppendEventWithID(event); err != nil {
			t.Fatal(err)
		}
		if !event.SentAt.Equal(now.Truncate(time.Second)) {
			t.Fatalf("event cursor timestamp was not normalized: %s", event.SentAt)
		}
	}
	latest, err := archive.LatestEventSequence(task.ID)
	if err != nil || latest != 3 {
		t.Fatalf("unexpected latest sequence=%d err=%v", latest, err)
	}
	first, err := archive.ListEventPage(task.ID, nil, 2, nil, "")
	if err != nil || len(first.Items) != 2 || !first.HasMore || first.NextCursor == nil {
		t.Fatalf("unexpected first event page=%+v err=%v", first, err)
	}
	second, err := archive.ListEventPage(task.ID, first.NextCursor, 2, nil, "")
	if err != nil || len(second.Items) != 1 || second.Items[0].Content != "three" || second.HasMore {
		t.Fatalf("unexpected second event page=%+v err=%v", second, err)
	}

	rows, err := archive.db.Query(`EXPLAIN SELECT id, payload_json, sent_at
FROM task_events WHERE task_id = ? AND (sent_at > ? OR (sent_at = ? AND id > ?))
ORDER BY sent_at ASC, id ASC LIMIT 201`, task.ID, now, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var key string
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		for index, column := range columns {
			if column == "key" && values[index] != nil {
				switch value := values[index].(type) {
				case []byte:
					key = string(value)
				default:
					key = fmt.Sprint(value)
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if key != "idx_task_events_task_sent_id" {
		t.Fatalf("cursor query did not use task/sent/id index: %q", key)
	}
}

func TestMySQLProjectMemorySensitiveSearchIntegration(t *testing.T) {
	fixture := newMySQLProjectMemoryIntegrationFixture(t, nil)
	archive := fixture.archive
	seedMySQLProjectScope(t, archive, 1, "machine", "scope")
	memories := []model.ProjectMemory{
		{ID: "public", LogicalID: "public", ScopeID: "scope", Kind: model.ProjectMemoryVerifiedFact,
			SubjectKey: "build", Statement: "publiclookupneedle build succeeds", Status: model.ProjectMemoryActive},
		{ID: "subject", LogicalID: "subject", ScopeID: "scope", Kind: model.ProjectMemoryVerifiedFact,
			SubjectKey: "native:shadowhook:callbackneedle", Statement: "runtime callback confirmed", Status: model.ProjectMemoryActive},
		{ID: "sensitive", LogicalID: "sensitive", ScopeID: "scope", Kind: model.ProjectMemoryVerifiedFact,
			SubjectKey: "credential", Statement: "sensitivesecretneedle", Status: model.ProjectMemoryActive, Sensitive: true},
	}
	for _, memory := range memories {
		if err := archive.UpsertProjectMemory(context.Background(), memory); err != nil {
			t.Fatal(err)
		}
	}
	var sensitiveSearchText string
	if err := archive.db.QueryRow(`SELECT search_text FROM project_memories WHERE memory_id='sensitive'`).Scan(&sensitiveSearchText); err != nil {
		t.Fatal(err)
	}
	if sensitiveSearchText != "" {
		t.Fatalf("sensitive statement entered search document: %q", sensitiveSearchText)
	}
	public, err := archive.ListProjectMemories(context.Background(), model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: "machine", ScopeID: "scope", Query: "publiclookupneedle", Limit: 10,
	})
	if err != nil || len(public) != 1 || public[0].ID != "public" {
		t.Fatalf("public FULLTEXT lookup failed: items=%v err=%v", public, err)
	}
	subject, err := archive.ListProjectMemories(context.Background(), model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: "machine", ScopeID: "scope", Query: "callbackneedle", Limit: 10,
	})
	if err != nil || len(subject) != 1 || subject[0].ID != "subject" {
		t.Fatalf("subject-key FULLTEXT lookup failed: items=%v err=%v", subject, err)
	}
	secret, err := archive.ListProjectMemories(context.Background(), model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: "machine", ScopeID: "scope", Query: "sensitivesecretneedle", Limit: 10,
	})
	if err != nil || len(secret) != 0 {
		t.Fatalf("sensitive FULLTEXT lookup leaked memory: items=%v err=%v", secret, err)
	}

	seedMySQLProjectScope(t, archive, 2, "machine-b", "scope-b")
	if err := archive.RecordProjectMemoryUsage(context.Background(), model.ProjectMemoryUsage{
		ScopeID: "scope", TaskID: "task-a", Revision: 4,
		MemoryIDs: []string{"memory-a"}, TokenCount: 23,
	}); err != nil {
		t.Fatal(err)
	}
	if err := archive.RecordProjectMemoryAudit(context.Background(), model.ProjectMemoryAudit{
		ScopeID: "scope", MemoryID: "memory-a", OperatorID: 1,
		Action: "memory.view", Metadata: map[string]any{"source": "test"},
	}); err != nil {
		t.Fatal(err)
	}

	var usageScope string
	var usageRevision int64
	var usageMemoryIDs string
	if err := archive.db.QueryRow(`SELECT project_scope_id, revision, memory_ids_json
FROM project_memory_usages WHERE task_id='task-a'`).Scan(&usageScope, &usageRevision, &usageMemoryIDs); err != nil {
		t.Fatal(err)
	}
	if usageScope != "scope" || usageRevision != 4 || !strings.Contains(usageMemoryIDs, "memory-a") {
		t.Fatalf("usage lost scope/revision/memory association: scope=%q revision=%d ids=%s",
			usageScope, usageRevision, usageMemoryIDs)
	}
	var auditScope, auditMemory, auditAction, auditMetadata string
	var auditOperator int64
	if err := archive.db.QueryRow(`SELECT project_scope_id, memory_id, operator_id, action_name, metadata_json
FROM project_memory_audits WHERE memory_id='memory-a'`).Scan(
		&auditScope, &auditMemory, &auditOperator, &auditAction, &auditMetadata); err != nil {
		t.Fatal(err)
	}
	if auditScope != "scope" || auditMemory != "memory-a" || auditOperator != 1 ||
		auditAction != "memory.view" || !strings.Contains(auditMetadata, "test") {
		t.Fatalf("audit lost ownership association: scope=%q memory=%q operator=%d action=%q metadata=%s",
			auditScope, auditMemory, auditOperator, auditAction, auditMetadata)
	}
	var crossOperatorUsage, mismatchedAudit int
	if err := archive.db.QueryRow(`SELECT COUNT(*) FROM project_memory_usages u
JOIN project_scopes s ON s.project_scope_id=u.project_scope_id
WHERE u.task_id='task-a' AND s.operator_id=2`).Scan(&crossOperatorUsage); err != nil {
		t.Fatal(err)
	}
	if err := archive.db.QueryRow(`SELECT COUNT(*) FROM project_memory_audits
WHERE project_scope_id='scope' AND operator_id=2`).Scan(&mismatchedAudit); err != nil {
		t.Fatal(err)
	}
	if crossOperatorUsage != 0 || mismatchedAudit != 0 {
		t.Fatalf("usage/audit crossed ownership boundary: usage=%d audit=%d", crossOperatorUsage, mismatchedAudit)
	}
}

func TestMySQLProjectMemoryAtomicScheduleFinalizeIntegration(t *testing.T) {
	fixture := newMySQLProjectMemoryIntegrationFixture(t, nil)
	archive := fixture.archive
	seedMySQLProjectScope(t, archive, 1, "machine", "scope")
	now := time.Now().UTC()

	const workers = 24
	var wg sync.WaitGroup
	created := make(chan bool, workers)
	jobIDs := make(chan string, workers)
	errs := make(chan error, workers)
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			job, wasCreated, err := archive.ScheduleProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
				ID: fmt.Sprintf("job-%02d", index), OperatorID: 1, MachineID: "machine", ScopeID: "scope",
				Status: model.ProjectMemoryJobQueued, CursorStart: "cursor-0",
				CursorEnd: fmt.Sprintf("cursor-%02d", index+1), ManualVisible: index == 7,
				CreatedAt: now.Add(time.Duration(index) * time.Microsecond),
			})
			if err != nil {
				errs <- err
				return
			}
			created <- wasCreated
			jobIDs <- job.ID
		}(index)
	}
	wg.Wait()
	close(created)
	close(jobIDs)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	createdCount := 0
	for value := range created {
		if value {
			createdCount++
		}
	}
	ids := map[string]bool{}
	for id := range jobIDs {
		ids[id] = true
	}
	if createdCount != 1 || len(ids) != 1 {
		t.Fatalf("schedule was not atomic: created=%d ids=%v", createdCount, ids)
	}
	var activeID string
	for id := range ids {
		activeID = id
	}
	queued, err := archive.GetProjectMemoryJob(context.Background(), 1, activeID)
	if err != nil || queued == nil || !queued.ManualVisible {
		t.Fatalf("manual trigger did not attach: job=%+v err=%v", queued, err)
	}
	claimed, err := archive.ClaimProjectMemoryJob(context.Background(), activeID, "worker-1", now, now.Add(time.Minute), 2)
	if err != nil || claimed == nil {
		t.Fatalf("claim failed: job=%+v err=%v", claimed, err)
	}
	staleWorkerSnapshot := *claimed
	staleWorkerSnapshot.Status = model.ProjectMemoryJobCompleted
	staleWorkerSnapshot.CursorCommitted = "cursor-committed"
	staleWorkerSnapshot.LeaseOwner = ""
	staleWorkerSnapshot.LeaseExpiresAt = time.Time{}
	staleWorkerSnapshot.ManualVisible = false

	attached, wasCreated, err := archive.ScheduleProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
		ID: "deferred-trigger", OperatorID: 1, MachineID: "machine", ScopeID: "scope",
		Status: model.ProjectMemoryJobQueued, CursorEnd: "cursor-deferred", ManualVisible: true,
	})
	if err != nil || wasCreated || attached.PendingCursorEnd != "cursor-deferred" {
		t.Fatalf("running cursor was not deferred: job=%+v created=%v err=%v", attached, wasCreated, err)
	}
	successor, err := archive.FinalizeProjectMemoryJob(context.Background(), staleWorkerSnapshot, "successor")
	if err != nil || successor == nil {
		t.Fatalf("finalize lost deferred job: successor=%+v err=%v", successor, err)
	}
	if successor.CursorStart != "cursor-committed" || successor.CursorEnd != "cursor-deferred" {
		t.Fatalf("unexpected successor cursors: %+v", successor)
	}
	finalized, err := archive.GetProjectMemoryJob(context.Background(), 1, activeID)
	if err != nil || finalized == nil || !finalized.ManualVisible || finalized.PendingCursorEnd != "" {
		t.Fatalf("finalize overwrote concurrent scheduling state: job=%+v err=%v", finalized, err)
	}
}

func TestMySQLProjectMemoryAutomaticQuietWindowIntegration(t *testing.T) {
	fixture := newMySQLProjectMemoryIntegrationFixture(t, nil)
	archive := fixture.archive
	now := time.Now().UTC()
	seedMySQLProjectScope(t, archive, 1, "machine", "scope-auto")
	if _, _, err := archive.ScheduleProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
		ID: "mysql-auto-quiet", OperatorID: 1, MachineID: "machine", ScopeID: "scope-auto",
		Trigger: model.ProjectMemoryTriggerTaskComplete, Status: model.ProjectMemoryJobQueued,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	claimable, err := archive.ListClaimableProjectMemoryJobs(context.Background(), now, 10)
	if err != nil || len(claimable) != 0 {
		t.Fatalf("recent automatic job escaped quiet window: jobs=%+v err=%v", claimable, err)
	}
	claimable, err = archive.ListClaimableProjectMemoryJobs(context.Background(), now.Add(projectMemoryAutoQuietWindow), 10)
	if err != nil || len(claimable) != 1 || claimable[0].ID != "mysql-auto-quiet" {
		t.Fatalf("automatic job was not released after quiet window: jobs=%+v err=%v", claimable, err)
	}

	seedMySQLProjectScope(t, archive, 1, "machine", "scope-manual")
	if _, _, err := archive.ScheduleProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
		ID: "mysql-manual-now", OperatorID: 1, MachineID: "machine", ScopeID: "scope-manual",
		Trigger: model.ProjectMemoryTriggerManual, Status: model.ProjectMemoryJobQueued,
		CreatedAt: now, UpdatedAt: now, ManualVisible: true,
	}); err != nil {
		t.Fatal(err)
	}
	claimable, err = archive.ListClaimableProjectMemoryJobs(context.Background(), now, 10)
	if err != nil || len(claimable) != 1 || claimable[0].ID != "mysql-manual-now" {
		t.Fatalf("manual job was delayed: jobs=%+v err=%v", claimable, err)
	}
}

func TestMySQLProjectMemoryRestartLeaseAndCheckpointIntegration(t *testing.T) {
	fixture := newMySQLProjectMemoryIntegrationFixture(t, nil)
	archive := fixture.archive
	seedMySQLProjectScope(t, archive, 1, "machine", "scope")
	base := time.Now().UTC().Add(-10 * time.Minute)
	if _, _, err := archive.ScheduleProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
		ID: "restart-job", OperatorID: 1, MachineID: "machine", ScopeID: "scope",
		Status: model.ProjectMemoryJobQueued, CursorStart: "cursor-0", CursorEnd: "cursor-10", CreatedAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	first, err := archive.ClaimProjectMemoryJob(context.Background(), "restart-job", "worker-1", base, base.Add(time.Minute), 2)
	if err != nil || first == nil {
		t.Fatalf("first claim failed: job=%+v err=%v", first, err)
	}
	first.Status = model.ProjectMemoryJobRunning
	first.CheckpointCursor = "cursor-5"
	first.CheckpointProgress = 50
	first.CheckpointAt = base.Add(30 * time.Second)
	if err := archive.UpdateProjectMemoryJob(context.Background(), *first); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewMySQLArchive(context.Background(), fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.db.Close()
	claimable, err := restarted.ListClaimableProjectMemoryJobs(context.Background(), time.Now().UTC(), 10)
	if err != nil || len(claimable) != 1 || claimable[0].ID != "restart-job" {
		t.Fatalf("restart did not reconstruct expired job: jobs=%+v err=%v", claimable, err)
	}
	second, err := restarted.ClaimProjectMemoryJob(context.Background(), "restart-job", "worker-2", time.Now().UTC(), time.Now().UTC().Add(time.Minute), 2)
	if err != nil || second == nil {
		t.Fatalf("restart reclaim failed: job=%+v err=%v", second, err)
	}
	if second.FencingToken != first.FencingToken+1 || second.Attempt != first.Attempt+1 ||
		second.CheckpointCursor != "cursor-5" || second.CheckpointProgress != 50 || second.CursorCommitted != "" {
		t.Fatalf("restart lost checkpoint/fencing state: first=%+v second=%+v", first, second)
	}
	first.Status = model.ProjectMemoryJobCompleted
	if err := restarted.UpdateProjectMemoryJob(context.Background(), *first); !errors.Is(err, ErrProjectMemoryStaleJob) {
		t.Fatalf("stale pre-restart worker updated job: %v", err)
	}
}

func TestMySQLProjectMemoryReconcileConsistencyIntegration(t *testing.T) {
	fixture := newMySQLProjectMemoryIntegrationFixture(t, nil)
	archive := fixture.archive
	scope := seedMySQLProjectScope(t, archive, 1, "machine", "scope")
	candidate := model.ProjectMemoryCandidate{
		Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "native:tick", Statement: "tick uses offset 0x100",
		Confidence: 1, Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified},
		Sources:   []model.ProjectMemorySource{{TaskID: "task-1", RelativePath: "src/tick.cc", SHA256: strings.Repeat("a", 64)}},
		Artifacts: []model.ProjectMemoryArtifact{{Path: "lib/libgame.so", ModuleName: "libgame.so", SHA256: strings.Repeat("b", 64)}},
	}
	result, err := archive.ReconcileProjectMemories(context.Background(), 1, scope.ID, scope.Revision, []model.ProjectMemoryCandidate{candidate}, nil, nil)
	if err != nil || len(result.AcceptedIDs) != 1 || !result.Changed {
		t.Fatalf("initial reconcile failed: result=%+v err=%v", result, err)
	}
	firstID := result.AcceptedIDs[0]
	merged := candidate
	merged.Sources = []model.ProjectMemorySource{{TaskID: "task-2", RelativePath: "src/tick.cc", SHA256: strings.Repeat("a", 64)}}
	result, err = archive.ReconcileProjectMemories(context.Background(), 1, scope.ID, result.Revision, []model.ProjectMemoryCandidate{merged}, nil, nil)
	if err != nil || len(result.AcceptedIDs) != 1 || result.AcceptedIDs[0] != firstID || !result.Changed {
		t.Fatalf("source merge failed: result=%+v err=%v", result, err)
	}
	item, err := archive.GetProjectMemory(context.Background(), 1, scope.ID, firstID)
	if err != nil || item == nil || len(item.Sources) != 2 {
		t.Fatalf("merged sources not persisted: item=%+v err=%v", item, err)
	}
	if err := archive.MarkProjectMemoryDeleted(context.Background(), 1, scope.ID, firstID, 0); err != nil {
		t.Fatal(err)
	}
	latestScope, _ := archive.GetProjectScope(context.Background(), 1, "machine", scope.ID)
	result, err = archive.ReconcileProjectMemories(context.Background(), 1, scope.ID, latestScope.Revision, []model.ProjectMemoryCandidate{candidate}, nil, nil)
	if err != nil || len(result.AcceptedIDs) != 0 {
		t.Fatalf("tombstone was resurrected: result=%+v err=%v", result, err)
	}
	deleted, _ := archive.GetProjectMemory(context.Background(), 1, scope.ID, firstID)
	if deleted == nil || deleted.Status != model.ProjectMemoryDeleted {
		t.Fatalf("deleted memory changed after reconcile: %+v", deleted)
	}

	if err := archive.UpsertProjectMemoryBrief(context.Background(), model.ProjectMemoryBrief{
		ScopeID: scope.ID, Content: "new brief", SourceRevision: 20, GeneratedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := archive.UpsertProjectMemoryBrief(context.Background(), model.ProjectMemoryBrief{
		ScopeID: scope.ID, Content: "stale brief", SourceRevision: 10, GeneratedAt: time.Now().UTC().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	brief, err := archive.GetProjectMemoryBrief(context.Background(), 1, scope.ID)
	if err != nil || brief == nil || brief.Content != "new brief" || brief.SourceRevision != 20 {
		t.Fatalf("stale brief overwrote current brief: brief=%+v err=%v", brief, err)
	}
	if _, err := archive.ReconcileProjectMemories(context.Background(), 1, scope.ID, 1, nil, nil, nil); !errors.Is(err, ErrProjectMemoryRevisionConflict) {
		t.Fatalf("stale reconciliation revision was accepted: %v", err)
	}
}

func TestMySQLProjectMemoryArtifactChangeAndIsolationIntegration(t *testing.T) {
	fixture := newMySQLProjectMemoryIntegrationFixture(t, nil)
	archive := fixture.archive
	seedMySQLProjectScope(t, archive, 1, "machine-a", "scope-a")
	seedMySQLProjectScope(t, archive, 1, "machine-b", "scope-b")
	seedMySQLProjectScope(t, archive, 2, "machine-c", "scope-c")
	old := model.ProjectMemory{
		ID: "old-hook", LogicalID: "hook", ScopeID: "scope-a", Kind: model.ProjectMemoryVerifiedFact,
		SubjectKey: "native:tick", Statement: "old offset", Status: model.ProjectMemoryActive,
		Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified},
		Sources:      []model.ProjectMemorySource{{TaskID: "private-task", Excerpt: "private-source"}},
		Artifacts:    []model.ProjectMemoryArtifact{{ModuleName: "libgame.so", SHA256: strings.Repeat("a", 64)}},
	}
	if err := archive.UpsertProjectMemory(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	result, err := archive.ReconcileProjectMemories(context.Background(), 1, "scope-a", 1, []model.ProjectMemoryCandidate{{
		Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "native:tick", Statement: "new offset", Confidence: 1,
		Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified},
		Sources:      []model.ProjectMemorySource{{TaskID: "new-task"}},
		Artifacts:    []model.ProjectMemoryArtifact{{ModuleName: "libgame.so", SHA256: strings.Repeat("b", 64)}},
	}}, nil, nil)
	if err != nil || !result.Changed {
		t.Fatalf("artifact reconcile failed: result=%+v err=%v", result, err)
	}
	stale, err := archive.GetProjectMemory(context.Background(), 1, "scope-a", old.ID)
	if err != nil || stale == nil || stale.Status != model.ProjectMemoryStale || stale.Verification.Status != model.ProjectMemoryStaleCheck {
		t.Fatalf("old artifact memory not marked stale: item=%+v err=%v", stale, err)
	}
	checks := []model.ProjectMemoryFilter{
		{OperatorID: 1, MachineID: "machine-b", ScopeID: "scope-a", Limit: 10},
		{OperatorID: 2, MachineID: "machine-c", ScopeID: "scope-a", Limit: 10},
		{OperatorID: 1, MachineID: "machine-a", ScopeID: "scope-b", Query: "private-source", Limit: 10},
	}
	for _, filter := range checks {
		items, err := archive.ListProjectMemories(context.Background(), filter)
		if err != nil || len(items) != 0 {
			t.Fatalf("cross-boundary list leaked data for %+v: items=%v err=%v", filter, items, err)
		}
	}
	wrongOwner, err := archive.GetProjectMemory(context.Background(), 2, "scope-a", old.ID)
	if err != nil || wrongOwner != nil {
		t.Fatalf("cross-operator object lookup leaked memory: item=%+v err=%v", wrongOwner, err)
	}
}

func TestMySQLProjectMemoryStablePaginationAndDeleteCASIntegration(t *testing.T) {
	fixture := newMySQLProjectMemoryIntegrationFixture(t, nil)
	archive := fixture.archive
	scope := seedMySQLProjectScope(t, archive, 1, "machine", "scope-pagination")
	base := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	items := []model.ProjectMemory{
		{ID: "z-old", Kind: model.ProjectMemoryDecision, UpdatedAt: base},
		{ID: "a-new", Kind: model.ProjectMemoryDecision, UpdatedAt: base.Add(3 * time.Minute)},
		{ID: "m-mid", Kind: model.ProjectMemoryInferredFact, UpdatedAt: base.Add(2 * time.Minute)},
		{ID: "b-tie", Kind: model.ProjectMemoryDecision, UpdatedAt: base.Add(2 * time.Minute)},
		{ID: "z-tie", Kind: model.ProjectMemoryVerifiedFact, UpdatedAt: base.Add(2 * time.Minute)},
	}
	for _, item := range items {
		item.LogicalID = "logical-" + item.ID
		item.ScopeID = scope.ID
		item.SubjectKey = item.ID
		item.Statement = item.ID
		item.Status = model.ProjectMemoryActive
		item.CreatedAt = item.UpdatedAt
		if err := archive.UpsertProjectMemory(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	first, err := archive.ListProjectMemories(context.Background(), model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: "machine", ScopeID: scope.ID, Limit: 2,
	})
	if err != nil || len(first) != 2 || first[0].ID != "a-new" || first[1].ID != "z-tie" {
		t.Fatalf("unexpected MySQL first page: items=%+v err=%v", first, err)
	}
	cursor := first[1]
	cursor.UpdatedAt = base.Add(4 * time.Minute)
	if err := archive.UpsertProjectMemory(context.Background(), cursor); err != nil {
		t.Fatal(err)
	}
	second, err := archive.ListProjectMemories(context.Background(), model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: "machine", ScopeID: scope.ID, Limit: 2,
		BeforeID: first[1].ID, BeforeUpdatedAt: first[1].UpdatedAt,
	})
	if err != nil || len(second) != 2 || second[0].ID != "m-mid" || second[1].ID != "b-tie" {
		t.Fatalf("MySQL cursor update caused a gap or duplicate: items=%+v err=%v", second, err)
	}
	if err := archive.MarkProjectMemoryDeleted(context.Background(), 1, scope.ID, "m-mid", 9); !errors.Is(err, ErrProjectMemoryRevisionConflict) {
		t.Fatalf("stale MySQL delete returned %v", err)
	}
	if err := archive.MarkProjectMemoryDeleted(context.Background(), 2, scope.ID, "m-mid", 1); err == nil {
		t.Fatal("cross-owner MySQL delete succeeded")
	}
	history := model.ProjectMemory{
		ID: "history", LogicalID: "logical-history", ScopeID: scope.ID, Version: 1,
		Kind: model.ProjectMemoryDecision, SubjectKey: "history", Statement: "history",
		Status: model.ProjectMemorySuperseded,
	}
	if err := archive.UpsertProjectMemory(context.Background(), history); err != nil {
		t.Fatal(err)
	}
	if err := archive.MarkProjectMemoryDeleted(context.Background(), 1, scope.ID, history.ID, 1); !errors.Is(err, ErrProjectMemoryRevisionConflict) {
		t.Fatalf("historical MySQL delete returned %v", err)
	}
	if err := archive.MarkProjectMemoryDeleted(context.Background(), 1, scope.ID, "m-mid", 1); err != nil {
		t.Fatal(err)
	}
	afterDelete, _ := archive.GetProjectScope(context.Background(), 1, "machine", scope.ID)
	if err := archive.MarkProjectMemoryDeleted(context.Background(), 1, scope.ID, "m-mid", 1); err != nil {
		t.Fatal(err)
	}
	afterRetry, _ := archive.GetProjectScope(context.Background(), 1, "machine", scope.ID)
	if afterDelete == nil || afterRetry == nil || afterRetry.Revision != afterDelete.Revision {
		t.Fatalf("idempotent MySQL delete advanced revision: first=%+v retry=%+v", afterDelete, afterRetry)
	}
}

func TestMySQLProjectMemoryDeviceClaimLimitIntegration(t *testing.T) {
	fixture := newMySQLProjectMemoryIntegrationFixture(t, nil)
	archive := fixture.archive
	now := time.Now().UTC()
	for index := 0; index < 3; index++ {
		scopeID := fmt.Sprintf("scope-%d", index)
		seedMySQLProjectScope(t, archive, 1, "machine", scopeID)
		if _, _, err := archive.ScheduleProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
			ID: "job-" + scopeID, OperatorID: 1, MachineID: "machine", ScopeID: scopeID,
			Status: model.ProjectMemoryJobQueued, CreatedAt: now.Add(time.Duration(index) * time.Millisecond),
		}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	claimed := make(chan string, 3)
	for index := 0; index < 3; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			id := fmt.Sprintf("job-scope-%d", index)
			job, err := archive.ClaimProjectMemoryJob(context.Background(), id, fmt.Sprintf("worker-%d", index), now, now.Add(time.Minute), 2)
			if err != nil {
				claimed <- "error:" + err.Error()
				return
			}
			if job != nil {
				claimed <- job.ID
			}
		}(index)
	}
	wg.Wait()
	close(claimed)
	var ids []string
	for id := range claimed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) != 2 || strings.HasPrefix(strings.Join(ids, ","), "error:") {
		t.Fatalf("device claim limit was not atomic: %v", ids)
	}
}

func TestMySQLProjectMemoryTwentyThousandRetrievalPerformanceIntegration(t *testing.T) {
	if os.Getenv("PROJECT_MEMORY_PERFORMANCE") != "1" {
		t.Skip("set PROJECT_MEMORY_PERFORMANCE=1 to run the 20,000-memory performance test")
	}
	fixture := newMySQLProjectMemoryIntegrationFixture(t, nil)
	archive := fixture.archive
	seedMySQLProjectScope(t, archive, 1, "machine", "scope-performance")

	tx, err := archive.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	insertPrefix := `INSERT INTO project_memories (
memory_id, logical_memory_id, project_scope_id, kind, subject_key, statement_text,
search_text, status, confidence, locked, ` + projectMemorySensitiveColumn + `, verification_status, content_hash,
version, created_by, created_at, updated_at)
	VALUES `
	const batchSize = 500
	now := time.Now().UTC()
	for start := 0; start < 20_000; start += batchSize {
		var values strings.Builder
		args := make([]any, 0, batchSize*8)
		for index := start; index < start+batchSize && index < 20_000; index++ {
			if values.Len() > 0 {
				values.WriteByte(',')
			}
			values.WriteString(`(?, ?, 'scope-performance', 'verified_fact', ?, ?, ?,
'active', 1, 0, 0, 'verified', ?, 1, 'performance-test', ?, ?)`)
			id := fmt.Sprintf("perf-memory-%05d", index)
			subject := fmt.Sprintf("component:%05d", index)
			text := fmt.Sprintf("component %05d has a verified build procedure", index)
			if index%1000 == 0 {
				text += " targetretrievalneedle libgame.so arm64-v8a"
			}
			createdAt := now.Add(time.Duration(index) * time.Microsecond)
			args = append(args, id, "logical-"+id, subject, text, subject+" "+text,
				fmt.Sprintf("%064x", index+1), createdAt, createdAt)
		}
		if _, err := tx.Exec(insertPrefix+values.String(), args...); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	filter := model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: "machine", ScopeID: "scope-performance",
		Status: model.ProjectMemoryActive, Query: "targetretrievalneedle libgame.so arm64-v8a", Limit: 200,
	}
	if _, err := archive.ListProjectMemories(context.Background(), filter); err != nil {
		t.Fatal(err)
	}
	durations := make([]time.Duration, 25)
	for index := range durations {
		started := time.Now()
		items, err := archive.ListProjectMemories(context.Background(), filter)
		durations[index] = time.Since(started)
		if err != nil || len(items) != 20 {
			t.Fatalf("performance retrieval failed: count=%d err=%v", len(items), err)
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[(len(durations)*95+99)/100-1]
	t.Logf("20,000-memory MySQL retrieval p95=%s samples=%v", p95, durations)
	if p95 >= 500*time.Millisecond {
		t.Fatalf("20,000-memory retrieval p95 exceeded 500ms: %s", p95)
	}
}
