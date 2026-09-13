package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"relay-server/internal/model"
)

type MySQLConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

type MySQLArchive struct {
	db                  *sql.DB
	projectMemoryCipher *projectMemoryFieldCipher
}

var legacyReverseMCPIDsForStore = []string{
	"idalib-mcp",
	"ida-pro-mcp",
	"ghidra-mcp",
	"frida-mcp",
	"apktool-mcp",
	"jadx-mcp",
	"jsreverser-mcp",
}

func NewTaskArchiveFromEnv() (TaskArchive, error) {
	cfg := MySQLConfig{
		Host:     env("MYSQL_HOST", "127.0.0.1"),
		Port:     envInt("MYSQL_PORT", 3306),
		User:     env("MYSQL_USER", "root"),
		Password: env("MYSQL_PASSWORD", "ymh20040825"),
		Database: env("MYSQL_DATABASE", "chat_codex"),
	}

	archive, err := NewMySQLArchive(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	return archive, nil
}

func NewMySQLArchive(ctx context.Context, cfg MySQLConfig) (*MySQLArchive, error) {
	serverDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8mb4&parseTime=true&multiStatements=true",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
	)
	serverDB, err := sql.Open("mysql", serverDSN)
	if err != nil {
		return nil, err
	}
	defer serverDB.Close()

	if err := serverDB.PingContext(ctx); err != nil {
		return nil, err
	}
	if _, err := serverDB.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+quoteIdent(cfg.Database)+" CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci"); err != nil {
		return nil, err
	}

	dbDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&multiStatements=true",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Database,
	)
	db, err := sql.Open("mysql", dbDSN)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := ensureSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureDefaultOperator(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	fieldCipher, err := newProjectMemoryFieldCipher(cfg)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &MySQLArchive{db: db, projectMemoryCipher: fieldCipher}, nil
}

func (m *MySQLArchive) UpsertTask(task *model.Task) error {
	if task == nil {
		return nil
	}
	persist := struct {
		Parts    []model.Part           `json:"parts,omitempty"`
		Metadata map[string]string      `json:"metadata,omitempty"`
		Approval *model.Approval        `json:"approval,omitempty"`
		Question *model.QuestionRequest `json:"question,omitempty"`
		Plan     *model.Plan            `json:"plan,omitempty"`
	}{
		Parts:    task.Parts,
		Metadata: task.Metadata,
		Approval: task.Approval,
		Question: task.Question,
		Plan:     task.Plan,
	}
	inputJSON, err := json.Marshal(persist)
	if err != nil {
		return err
	}
	artifactsJSON, err := json.Marshal(task.Artifacts)
	if err != nil {
		return err
	}

	query := `
INSERT INTO tasks (
  task_id, agent_id, machine_id, project_id, project_root, session_id,
  status, result_text, error_text, input_json, artifacts_json, created_at, updated_at, operator_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  agent_id = VALUES(agent_id),
  machine_id = VALUES(machine_id),
  project_id = VALUES(project_id),
  project_root = VALUES(project_root),
  session_id = VALUES(session_id),
  status = VALUES(status),
  result_text = VALUES(result_text),
  error_text = VALUES(error_text),
  input_json = VALUES(input_json),
  artifacts_json = VALUES(artifacts_json),
  updated_at = VALUES(updated_at)
`

	_, err = m.db.Exec(
		query,
		task.ID,
		task.AgentID,
		task.MachineID,
		task.ProjectID,
		task.ProjectRoot,
		task.SessionID,
		string(task.Status),
		nullString(task.Result),
		nullString(task.Error),
		string(inputJSON),
		string(artifactsJSON),
		task.CreatedAt.UTC(),
		task.UpdatedAt.UTC(),
		task.OperatorID,
	)
	return err
}

func (m *MySQLArchive) AppendEvent(event model.Event) error {
	return m.AppendEventWithID(&event)
}

func (m *MySQLArchive) AppendEventWithID(event *model.Event) error {
	if event == nil {
		return nil
	}
	// task_events.sent_at is DATETIME(0). The live SSE cursor must use the same
	// precision as the persisted key or later rows from that second can be lost.
	event.SentAt = event.SentAt.UTC().Truncate(time.Second)
	payloadJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}

	result, err := m.db.Exec(`
INSERT INTO task_events (
  task_id, event_type, session_id, permission_id, payload_json,
  content_text, error_text, sent_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`,
		event.TaskID,
		event.Type,
		event.SessionID,
		event.PermissionID,
		string(payloadJSON),
		nullString(event.Content),
		nullString(event.Error),
		event.SentAt.UTC(),
	)
	if err == nil && event.ID <= 0 {
		if id, idErr := result.LastInsertId(); idErr == nil {
			event.ID = id
		}
	}
	return err
}

func (m *MySQLArchive) ListEvents(taskID string) ([]model.Event, error) {
	rows, err := m.db.Query(`SELECT id, payload_json, sent_at FROM task_events WHERE task_id = ? ORDER BY sent_at DESC, id DESC LIMIT ?`, taskID, 200)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]model.Event, 0, 200)
	for rows.Next() {
		var id int64
		var payload sql.NullString
		var sentAt time.Time
		if err := rows.Scan(&id, &payload, &sentAt); err != nil {
			return nil, err
		}
		if !payload.Valid || strings.TrimSpace(payload.String) == "" {
			continue
		}
		var event model.Event
		if err := json.Unmarshal([]byte(payload.String), &event); err != nil {
			return nil, err
		}
		event.ID, event.SentAt = id, sentAt.UTC()
		if event.Sequence <= 0 {
			event.Sequence = id
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	return events, nil
}

func (m *MySQLArchive) ListEventsAfter(taskID string, afterSequence int64, limit int) ([]model.Event, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 200 {
		limit = 200
	}
	// Sequence is a per-task stream cursor used by older clients. Filter on the
	// persisted JSON sequence without OFFSET; new code should prefer
	// ListEventPage and its (sent_at,id) cursor.
	query := `SELECT id, payload_json, sent_at FROM task_events WHERE task_id = ?`
	args := []any{taskID}
	if afterSequence > 0 {
		query += ` AND COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.sequence')) AS UNSIGNED), id) > ?`
		args = append(args, afterSequence)
	}
	if afterSequence > 0 {
		query += ` ORDER BY sent_at ASC, id ASC LIMIT ?`
	} else {
		query += ` ORDER BY sent_at DESC, id DESC LIMIT ?`
	}
	args = append(args, limit)
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]model.Event, 0, limit)
	for rows.Next() {
		var id int64
		var payload sql.NullString
		var sentAt time.Time
		if err := rows.Scan(&id, &payload, &sentAt); err != nil {
			return nil, err
		}
		if !payload.Valid || strings.TrimSpace(payload.String) == "" {
			continue
		}
		var event model.Event
		if err := json.Unmarshal([]byte(payload.String), &event); err != nil {
			return nil, err
		}
		event.ID, event.SentAt = id, sentAt.UTC()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if afterSequence <= 0 {
		for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
			events[left], events[right] = events[right], events[left]
		}
	}
	return events, nil
}

// ListEventPage performs keyset pagination over (sent_at, id). A page reads
// one extra row to determine has_more and never retains rows in Memory.
func (m *MySQLArchive) ListEventPage(taskID string, after *EventCursor, limit int, eventTypes []string, nodeID string) (EventPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `SELECT id, payload_json, sent_at FROM task_events WHERE task_id = ?`
	args := []any{taskID}
	if after != nil && !after.SentAt.IsZero() {
		query += ` AND (sent_at > ? OR (sent_at = ? AND id > ?))`
		args = append(args, after.SentAt.UTC(), after.SentAt.UTC(), after.ID)
	}
	if len(eventTypes) > 0 {
		query += ` AND event_type IN (` + strings.TrimRight(strings.Repeat("?,", len(eventTypes)), ",") + `)`
		for _, eventType := range eventTypes {
			args = append(args, eventType)
		}
	}
	if strings.TrimSpace(nodeID) != "" {
		// node_id is stored in the JSON payload; this predicate is only used by
		// the dedicated log view, avoiding a full event decode in callers.
		query += ` AND JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.metadata.node_id')) = ?`
		args = append(args, strings.TrimSpace(nodeID))
	}
	query += ` ORDER BY sent_at ASC, id ASC LIMIT ?`
	args = append(args, limit+1)
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return EventPage{}, err
	}
	defer rows.Close()
	items := make([]model.Event, 0, limit)
	var last EventCursor
	readCount := 0
	for rows.Next() {
		var id int64
		var payload sql.NullString
		var sentAt time.Time
		if err := rows.Scan(&id, &payload, &sentAt); err != nil {
			return EventPage{}, err
		}
		if !payload.Valid || strings.TrimSpace(payload.String) == "" {
			continue
		}
		var event model.Event
		if err := json.Unmarshal([]byte(payload.String), &event); err != nil {
			return EventPage{}, err
		}
		event.ID = id
		event.SentAt = sentAt.UTC()
		if event.Sequence <= 0 {
			event.Sequence = id
		}
		readCount++
		if len(items) < limit {
			items = append(items, event)
			last = EventCursor{SentAt: event.SentAt, ID: id}
		}
	}
	if err := rows.Err(); err != nil {
		return EventPage{}, err
	}
	page := EventPage{Items: items, HasMore: readCount > limit}
	if len(items) > 0 {
		page.NextCursor = &last
	}
	return page, nil
}

func (m *MySQLArchive) ListLatestSubagentEvents(taskID string, limit int) ([]model.Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := m.db.Query(`
SELECT id, payload_json, sent_at
FROM task_events
WHERE task_id = ? AND event_type IN ('subagent_state', 'subagent_started', 'subagent_result', 'subagent_control_requested', 'subagent_control_applied')
ORDER BY sent_at DESC, id DESC
LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]model.Event, 0, limit)
	for rows.Next() {
		var id int64
		var payload sql.NullString
		var sentAt time.Time
		if err := rows.Scan(&id, &payload, &sentAt); err != nil {
			return nil, err
		}
		if !payload.Valid || strings.TrimSpace(payload.String) == "" {
			continue
		}
		var event model.Event
		if err := json.Unmarshal([]byte(payload.String), &event); err != nil {
			return nil, err
		}
		event.ID, event.SentAt = id, sentAt.UTC()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	return events, nil
}

func (m *MySQLArchive) LatestEventSequence(taskID string) (int64, error) {
	var id int64
	var payload sql.NullString
	err := m.db.QueryRow(`
SELECT id, payload_json
FROM task_events
WHERE task_id = ?
ORDER BY id DESC
LIMIT 1`, taskID).Scan(&id, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !payload.Valid || strings.TrimSpace(payload.String) == "" {
		return id, nil
	}
	var event model.Event
	if err := json.Unmarshal([]byte(payload.String), &event); err != nil {
		return 0, err
	}
	if event.Sequence > 0 {
		return event.Sequence, nil
	}
	return id, nil
}

func (m *MySQLArchive) LatestTerminalEvent(taskID string) (model.Event, bool, error) {
	row := m.db.QueryRow(`
SELECT id, payload_json, sent_at
FROM task_events
WHERE task_id = ? AND event_type IN ('completed', 'failed', 'cancelled')
ORDER BY sent_at DESC, id DESC
LIMIT 1`, taskID)
	var id int64
	var payload sql.NullString
	var sentAt time.Time
	if err := row.Scan(&id, &payload, &sentAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Event{}, false, nil
		}
		return model.Event{}, false, err
	}
	if !payload.Valid || strings.TrimSpace(payload.String) == "" {
		return model.Event{}, false, nil
	}
	var event model.Event
	if err := json.Unmarshal([]byte(payload.String), &event); err != nil {
		return model.Event{}, false, err
	}
	event.ID, event.SentAt = id, sentAt.UTC()
	return event, true, nil
}

func (m *MySQLArchive) ListProjectMemoryEvents(taskID string, limit int) ([]model.Event, error) {
	if limit <= 0 {
		limit = 64
	}
	if limit > 64 {
		limit = 64
	}
	rows, err := m.db.Query(`
SELECT payload_json
FROM task_events
WHERE task_id = ? AND event_type IN (?, ?, ?, ?, ?, ?, ?)
ORDER BY id DESC
LIMIT ?
`, taskID, "completed", "failed", "tool_updated", "goal_checkpoint", "goal_completed", "plan_updated", "compaction_completed", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]model.Event, 0, limit)
	for rows.Next() {
		var payload sql.NullString
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		if !payload.Valid || strings.TrimSpace(payload.String) == "" {
			continue
		}
		var event model.Event
		if err := json.Unmarshal([]byte(payload.String), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	return events, nil
}

func (m *MySQLArchive) LastEventAt(taskID string) (time.Time, bool, error) {
	row := m.db.QueryRow(`
SELECT sent_at
FROM task_events
WHERE task_id = ?
ORDER BY sent_at DESC, id DESC
LIMIT 1
`, taskID)

	var sentAt time.Time
	if err := row.Scan(&sentAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	return sentAt.UTC(), true, nil
}

func (m *MySQLArchive) GetTask(taskID string) (*model.Task, error) {
	row := m.db.QueryRow(`
SELECT task_id, agent_id, machine_id, project_id, project_root, session_id,
       status, result_text, error_text, input_json, artifacts_json, created_at, updated_at, operator_id
FROM tasks
WHERE task_id = ?
`, taskID)

	var (
		task          model.Task
		status        string
		result        sql.NullString
		errText       sql.NullString
		inputJSON     sql.NullString
		artifactsJSON sql.NullString
	)
	if err := row.Scan(
		&task.ID,
		&task.AgentID,
		&task.MachineID,
		&task.ProjectID,
		&task.ProjectRoot,
		&task.SessionID,
		&status,
		&result,
		&errText,
		&inputJSON,
		&artifactsJSON,
		&task.CreatedAt,
		&task.UpdatedAt,
		&task.OperatorID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	task.Status = model.TaskStatus(status)
	if result.Valid {
		task.Result = result.String
	}
	if errText.Valid {
		task.Error = errText.String
	}
	if inputJSON.Valid && inputJSON.String != "" {
		var persist struct {
			Parts    []model.Part           `json:"parts,omitempty"`
			Metadata map[string]string      `json:"metadata,omitempty"`
			Approval *model.Approval        `json:"approval,omitempty"`
			Question *model.QuestionRequest `json:"question,omitempty"`
			Plan     *model.Plan            `json:"plan,omitempty"`
		}
		if err := json.Unmarshal([]byte(inputJSON.String), &persist); err != nil {
			var legacy []model.Part
			if err := json.Unmarshal([]byte(inputJSON.String), &legacy); err != nil {
				return nil, err
			}
			task.Parts = legacy
		} else {
			task.Parts = persist.Parts
			task.Metadata = persist.Metadata
			task.Approval = persist.Approval
			task.Question = persist.Question
			task.Plan = persist.Plan
		}
	}
	if artifactsJSON.Valid && artifactsJSON.String != "" {
		if err := json.Unmarshal([]byte(artifactsJSON.String), &task.Artifacts); err != nil {
			return nil, err
		}
	}
	return &task, nil
}

func (m *MySQLArchive) ListTasks(filter model.TaskFilter) ([]*model.Task, error) {
	query := `
SELECT task_id, agent_id, machine_id, project_id, project_root, session_id,
       status, result_text, error_text, input_json, artifacts_json, created_at, updated_at, operator_id
FROM tasks
`
	clauses := make([]string, 0, 4)
	args := make([]any, 0, 5)

	if filter.OperatorID > 0 {
		clauses = append(clauses, "operator_id = ?")
		args = append(args, filter.OperatorID)
	}
	if filter.AgentID != "" {
		clauses = append(clauses, "agent_id = ?")
		args = append(args, filter.AgentID)
	}
	if filter.MachineID != "" {
		clauses = append(clauses, "machine_id = ?")
		args = append(args, filter.MachineID)
	}
	if filter.ProjectID != "" {
		clauses = append(clauses, "project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.SessionID != "" {
		clauses = append(clauses, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, string(filter.Status))
	}
	if !filter.BeforeCreatedAt.IsZero() {
		clauses = append(clauses, "(created_at < ? OR (created_at = ? AND task_id < ?))")
		args = append(args, filter.BeforeCreatedAt, filter.BeforeCreatedAt, filter.BeforeTaskID)
	}
	if !filter.AfterCreatedAt.IsZero() {
		clauses = append(clauses, "(created_at > ? OR (created_at = ? AND task_id > ?))")
		args = append(args, filter.AfterCreatedAt, filter.AfterCreatedAt, filter.AfterTaskID)
	}
	if !filter.AfterUpdatedAt.IsZero() {
		clauses = append(clauses, "(updated_at > ? OR (updated_at = ? AND task_id > ?))")
		args = append(args, filter.AfterUpdatedAt, filter.AfterUpdatedAt, filter.AfterUpdatedTaskID)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	if filter.CreatedAsc {
		query += ` ORDER BY created_at ASC, task_id ASC`
	} else if filter.CreatedDesc {
		query += ` ORDER BY created_at DESC, task_id DESC`
	} else {
		query += ` ORDER BY CASE
			WHEN status = 'running' THEN 0
			WHEN status IN ('pending', 'dispatched', 'cancelling', 'waiting_approval') THEN 1
			WHEN status = 'failed' THEN 2
			WHEN status IN ('completed', 'cancelled') THEN 3
			ELSE 4
		END ASC, updated_at DESC, created_at DESC`
	}
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*model.Task
	for rows.Next() {
		var (
			task          model.Task
			status        string
			result        sql.NullString
			errText       sql.NullString
			inputJSON     sql.NullString
			artifactsJSON sql.NullString
		)
		if err := rows.Scan(
			&task.ID,
			&task.AgentID,
			&task.MachineID,
			&task.ProjectID,
			&task.ProjectRoot,
			&task.SessionID,
			&status,
			&result,
			&errText,
			&inputJSON,
			&artifactsJSON,
			&task.CreatedAt,
			&task.UpdatedAt,
			&task.OperatorID,
		); err != nil {
			return nil, err
		}
		task.Status = model.TaskStatus(status)
		if result.Valid {
			task.Result = result.String
		}
		if errText.Valid {
			task.Error = errText.String
		}
		if inputJSON.Valid && inputJSON.String != "" {
			var persist struct {
				Parts    []model.Part           `json:"parts,omitempty"`
				Metadata map[string]string      `json:"metadata,omitempty"`
				Approval *model.Approval        `json:"approval,omitempty"`
				Question *model.QuestionRequest `json:"question,omitempty"`
				Plan     *model.Plan            `json:"plan,omitempty"`
			}
			if err := json.Unmarshal([]byte(inputJSON.String), &persist); err != nil {
				var legacy []model.Part
				if err := json.Unmarshal([]byte(inputJSON.String), &legacy); err != nil {
					return nil, err
				}
				task.Parts = legacy
			} else {
				task.Parts = persist.Parts
				task.Metadata = persist.Metadata
				task.Approval = persist.Approval
				task.Question = persist.Question
				task.Plan = persist.Plan
			}
		}
		if artifactsJSON.Valid && artifactsJSON.String != "" {
			if err := json.Unmarshal([]byte(artifactsJSON.String), &task.Artifacts); err != nil {
				return nil, err
			}
		}
		tasks = append(tasks, &task)
	}

	return tasks, rows.Err()
}

func (m *MySQLArchive) UpsertSession(session *model.Session) error {
	if session == nil {
		return nil
	}

	query := `
INSERT INTO sessions (
  session_id, agent_id, machine_id, project_id, status, last_task_id, summary, created_at, updated_at, operator_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  agent_id = VALUES(agent_id),
  machine_id = VALUES(machine_id),
  project_id = VALUES(project_id),
  status = VALUES(status),
  last_task_id = VALUES(last_task_id),
  summary = VALUES(summary),
  updated_at = VALUES(updated_at)
`
	_, err := m.db.Exec(
		query,
		session.ID,
		session.AgentID,
		session.MachineID,
		session.ProjectID,
		session.Status,
		nullString(session.LastTaskID),
		nullString(session.Summary),
		session.CreatedAt.UTC(),
		session.UpdatedAt.UTC(),
		session.OperatorID,
	)
	return err
}

func (m *MySQLArchive) UpsertChatQueueItem(item model.ChatQueueItem) error {
	payload, err := json.Marshal(struct {
		Parts    []model.Part      `json:"parts"`
		Metadata map[string]string `json:"metadata,omitempty"`
	}{Parts: item.Parts, Metadata: item.Metadata})
	if err != nil {
		return err
	}
	_, err = m.db.Exec(`
INSERT INTO chat_queue_items (
  queue_item_id, operator_id, session_id, agent_id, machine_id, project_id,
  project_root, payload_json, position_value, status, version_value, task_id,
  injection_version, error_text, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  operator_id = VALUES(operator_id),
  session_id = VALUES(session_id),
  agent_id = VALUES(agent_id),
  machine_id = VALUES(machine_id),
  project_id = VALUES(project_id),
  project_root = VALUES(project_root),
  payload_json = VALUES(payload_json),
  position_value = VALUES(position_value),
  status = VALUES(status),
  version_value = VALUES(version_value),
  task_id = VALUES(task_id),
  injection_version = VALUES(injection_version),
  error_text = VALUES(error_text),
  updated_at = VALUES(updated_at)
`,
		item.ID,
		item.OperatorID,
		item.SessionID,
		item.AgentID,
		item.MachineID,
		item.ProjectID,
		item.ProjectRoot,
		string(payload),
		item.Position,
		string(item.Status),
		item.Version,
		item.TaskID,
		item.InjectionVersion,
		nullString(item.Error),
		item.CreatedAt.UTC(),
		item.UpdatedAt.UTC(),
	)
	return err
}

func (m *MySQLArchive) DeleteChatQueueItem(itemID string) error {
	_, err := m.db.Exec("DELETE FROM chat_queue_items WHERE queue_item_id = ?", itemID)
	return err
}

func (m *MySQLArchive) ListChatQueueItems(operatorID int64, sessionID string) ([]model.ChatQueueItem, error) {
	rows, err := m.db.Query(`
SELECT queue_item_id, operator_id, session_id, agent_id, machine_id, project_id,
       project_root, payload_json, position_value, status, version_value, task_id,
       injection_version, error_text, created_at, updated_at
FROM chat_queue_items
WHERE operator_id = ? AND session_id = ?
ORDER BY position_value ASC, queue_item_id ASC
`, operatorID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.ChatQueueItem, 0)
	for rows.Next() {
		var (
			item      model.ChatQueueItem
			payload   string
			status    string
			errorText sql.NullString
		)
		if err := rows.Scan(
			&item.ID,
			&item.OperatorID,
			&item.SessionID,
			&item.AgentID,
			&item.MachineID,
			&item.ProjectID,
			&item.ProjectRoot,
			&payload,
			&item.Position,
			&status,
			&item.Version,
			&item.TaskID,
			&item.InjectionVersion,
			&errorText,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		var decoded struct {
			Parts    []model.Part      `json:"parts"`
			Metadata map[string]string `json:"metadata,omitempty"`
		}
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			return nil, err
		}
		item.Parts = decoded.Parts
		item.Metadata = decoded.Metadata
		item.Status = model.ChatQueueItemStatus(status)
		if errorText.Valid {
			item.Error = errorText.String
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (m *MySQLArchive) ListSessions(filter model.SessionFilter) ([]*model.Session, error) {
	query := `
SELECT session_id, agent_id, machine_id, project_id, status, last_task_id, summary, created_at, updated_at
FROM sessions
`
	clauses := make([]string, 0, 4)
	args := make([]any, 0, 5)
	if filter.OperatorID > 0 {
		clauses = append(clauses, "operator_id = ?")
		args = append(args, filter.OperatorID)
	}
	if filter.AgentID != "" {
		clauses = append(clauses, "agent_id = ?")
		args = append(args, filter.AgentID)
	}
	if filter.MachineID != "" {
		clauses = append(clauses, "machine_id = ?")
		args = append(args, filter.MachineID)
	}
	if filter.ProjectID != "" {
		clauses = append(clauses, "project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, filter.Status)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY updated_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*model.Session
	for rows.Next() {
		var (
			session    model.Session
			lastTaskID sql.NullString
			summary    sql.NullString
		)
		if err := rows.Scan(
			&session.ID,
			&session.AgentID,
			&session.MachineID,
			&session.ProjectID,
			&session.Status,
			&lastTaskID,
			&summary,
			&session.CreatedAt,
			&session.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if lastTaskID.Valid {
			session.LastTaskID = lastTaskID.String
		}
		if summary.Valid {
			session.Summary = summary.String
		}
		sessions = append(sessions, &session)
	}

	return sessions, rows.Err()
}

func (m *MySQLArchive) AuthenticateOperator(username, password string) (*model.Operator, error) {
	row := m.db.QueryRow(`
SELECT id, operator_uid, username, name, operator_key, password_hash
FROM operators
WHERE username = ?
`, username)

	var (
		operator     model.Operator
		passwordHash string
	)
	if err := row.Scan(
		&operator.ID,
		&operator.OperatorUID,
		&operator.Username,
		&operator.Name,
		&operator.OperatorKey,
		&passwordHash,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if passwordHash != hashPassword(password) {
		return nil, nil
	}
	operator.Email = strings.TrimSpace(operator.Email)
	return &operator, nil
}

func (m *MySQLArchive) GetOperatorByKey(operatorKey string) (*model.Operator, error) {
	row := m.db.QueryRow(`
SELECT id, operator_uid, username, name, operator_key
FROM operators
WHERE operator_key = ?
`, operatorKey)

	var operator model.Operator
	if err := row.Scan(
		&operator.ID,
		&operator.OperatorUID,
		&operator.Username,
		&operator.Name,
		&operator.OperatorKey,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &operator, nil
}

func (m *MySQLArchive) GetOperatorEmail(operatorID int64) (string, error) {
	row := m.db.QueryRow(`
SELECT email
FROM operators
WHERE id = ?
`, operatorID)

	var email string
	if err := row.Scan(&email); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(email), nil
}

func (m *MySQLArchive) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	return m.db.Close()
}

func (m *MySQLArchive) PingContext(ctx context.Context) error {
	if m == nil || m.db == nil {
		return errors.New("mysql archive is not initialized")
	}
	return m.db.PingContext(ctx)
}

func (m *MySQLArchive) UsernameExists(username string) (bool, error) {
	var count int
	if err := m.db.QueryRow("SELECT COUNT(*) FROM operators WHERE username = ?", username).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (m *MySQLArchive) EmailExists(email string) (bool, error) {
	var count int
	if err := m.db.QueryRow("SELECT COUNT(*) FROM operators WHERE email = ?", email).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (m *MySQLArchive) CreateOperator(username, password, email, name string) (*model.Operator, error) {
	uid := "op_" + username
	key, err := randomHex(24)
	if err != nil {
		return nil, err
	}
	opKey := "opk_" + key
	result, err := m.db.Exec(`
INSERT INTO operators (operator_uid, name, client_type, username, password_hash, operator_key, email)
VALUES (?, ?, 'web', ?, ?, ?, ?)
`, uid, name, username, hashPassword(password), opKey, email)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return &model.Operator{
		ID:          id,
		OperatorUID: uid,
		Username:    username,
		Name:        name,
		Email:       email,
		OperatorKey: opKey,
	}, nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	for _, stmt := range schemaStatements {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	// CREATE TABLE IF NOT EXISTS does not update indexes on an existing table.
	// Keep the event-read indexes as an idempotent migration so legacy databases
	// receive the same access paths as newly-created databases.
	if err := ensureTaskEventIndexes(ctx, db); err != nil {
		return err
	}
	for _, stmt := range projectMemorySchemaStatements {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	for _, stmt := range appNotificationSchemaStatements {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := ensureColumn(ctx, db, "project_memory_sources", "sha256", "ALTER TABLE project_memory_sources ADD COLUMN sha256 CHAR(64) NOT NULL DEFAULT '' AFTER relative_path"); err != nil {
		return err
	}
	projectMemoryColumns := []struct {
		table  string
		column string
		alter  string
	}{
		{"project_scope_locations", "binding_epoch", "ALTER TABLE project_scope_locations ADD COLUMN binding_epoch BIGINT NOT NULL DEFAULT 1 AFTER marker_nonce"},
		{"project_memories", "embedding_model", "ALTER TABLE project_memories ADD COLUMN embedding_model VARCHAR(255) NOT NULL DEFAULT '' AFTER content_hash"},
		{"project_memories", "embedding_version", "ALTER TABLE project_memories ADD COLUMN embedding_version VARCHAR(128) NOT NULL DEFAULT '' AFTER embedding_model"},
		{"project_memory_artifacts", "package_name", "ALTER TABLE project_memory_artifacts ADD COLUMN package_name VARCHAR(255) NOT NULL DEFAULT '' AFTER sha256"},
		{"project_memory_artifacts", "apk_sha256", "ALTER TABLE project_memory_artifacts ADD COLUMN apk_sha256 CHAR(64) NOT NULL DEFAULT '' AFTER package_name"},
		{"project_memory_artifacts", "ida_database_id", "ALTER TABLE project_memory_artifacts ADD COLUMN ida_database_id VARCHAR(255) NOT NULL DEFAULT '' AFTER ida_analysis_status"},
		{"project_memory_jobs", "checkpoint_cursor", "ALTER TABLE project_memory_jobs ADD COLUMN checkpoint_cursor VARCHAR(191) NOT NULL DEFAULT '' AFTER cursor_committed"},
		{"project_memory_jobs", "checkpoint_progress", "ALTER TABLE project_memory_jobs ADD COLUMN checkpoint_progress INT NOT NULL DEFAULT 0 AFTER checkpoint_cursor"},
		{"project_memory_jobs", "checkpoint_at", "ALTER TABLE project_memory_jobs ADD COLUMN checkpoint_at DATETIME(6) NULL AFTER checkpoint_progress"},
		{"project_memory_jobs", "pending_cursor_end", "ALTER TABLE project_memory_jobs ADD COLUMN pending_cursor_end VARCHAR(191) NOT NULL DEFAULT '' AFTER checkpoint_at"},
	}
	for _, column := range projectMemoryColumns {
		if err := ensureColumn(ctx, db, column.table, column.column, column.alter); err != nil {
			return err
		}
	}
	if err := ensureProjectMemoryFulltextIndex(ctx, db); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "operators", "username", "ALTER TABLE operators ADD COLUMN username VARCHAR(64) NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "operators", "password_hash", "ALTER TABLE operators ADD COLUMN password_hash VARCHAR(255) NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "operators", "operator_key", "ALTER TABLE operators ADD COLUMN operator_key VARCHAR(128) NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "operators", "email", "ALTER TABLE operators ADD COLUMN email VARCHAR(128) NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// 用户隔离：tasks 和 sessions 加 operator_id
	if err := ensureColumn(ctx, db, "tasks", "operator_id", "ALTER TABLE tasks ADD COLUMN operator_id BIGINT NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "tasks", "artifacts_json", "ALTER TABLE tasks ADD COLUMN artifacts_json JSON NULL"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "sessions", "operator_id", "ALTER TABLE sessions ADD COLUMN operator_id BIGINT NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "skills", "package_files_json", "ALTER TABLE skills ADD COLUMN package_files_json JSON NULL"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "skills", "category", "ALTER TABLE skills ADD COLUMN category VARCHAR(64) NOT NULL DEFAULT '通用' AFTER package_files_json"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "semantic_agents", "skill_ids_json", "ALTER TABLE semantic_agents ADD COLUMN skill_ids_json JSON NULL"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "semantic_agents", "mcp_ids_json", "ALTER TABLE semantic_agents ADD COLUMN mcp_ids_json JSON NULL"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "mcp_catalog", "source_url", "ALTER TABLE mcp_catalog ADD COLUMN source_url VARCHAR(1024) NOT NULL DEFAULT '' AFTER source"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "mcp_catalog", "category", "ALTER TABLE mcp_catalog ADD COLUMN category VARCHAR(64) NOT NULL DEFAULT '通用' AFTER credential_url"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "mcp_catalog", "install_json", "ALTER TABLE mcp_catalog ADD COLUMN install_json JSON NULL AFTER config_json"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "mcp_catalog", "launch_ready", "ALTER TABLE mcp_catalog ADD COLUMN launch_ready TINYINT(1) NOT NULL DEFAULT 1 AFTER install_json"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "mcp_catalog", "launch_block_reason", "ALTER TABLE mcp_catalog ADD COLUMN launch_block_reason TEXT NOT NULL AFTER launch_ready"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "runtime_install_jobs", "requires_user_action", "ALTER TABLE runtime_install_jobs ADD COLUMN requires_user_action TINYINT(1) NOT NULL DEFAULT 0 AFTER progress_percent"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "runtime_install_jobs", "user_action_json", "ALTER TABLE runtime_install_jobs ADD COLUMN user_action_json JSON NULL AFTER requires_user_action"); err != nil {
		return err
	}
	if err := seedRuntimeCatalogDefaults(ctx, db); err != nil {
		return err
	}
	if err := removeLegacyReverseMCPScaffold(ctx, db); err != nil {
		return err
	}
	return nil
}

func ensureTaskEventIndexes(ctx context.Context, db *sql.DB) error {
	indexes := []struct {
		name    string
		columns []string
	}{
		{name: "idx_task_events_task_sent_id", columns: []string{"task_id", "sent_at", "id"}},
		{name: "idx_task_events_task_id_id", columns: []string{"task_id", "id"}},
		{name: "idx_task_events_task_type_sent_id", columns: []string{"task_id", "event_type", "sent_at", "id"}},
	}
	for _, index := range indexes {
		if err := ensureIndex(ctx, db, "task_events", index.name, index.columns); err != nil {
			return err
		}
	}
	return nil
}

// ensureIndex adds an index only when neither the requested name nor an
// equivalent column sequence exists. This avoids duplicate indexes during
// repeated application starts while repairing CREATE TABLE legacy gaps.
func ensureIndex(ctx context.Context, db *sql.DB, tableName, indexName string, columns []string) error {
	rows, err := db.QueryContext(ctx, `SELECT INDEX_NAME, COLUMN_NAME
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
ORDER BY INDEX_NAME, SEQ_IN_INDEX`, tableName)
	if err != nil {
		return err
	}
	defer rows.Close()

	byName := make(map[string][]string)
	for rows.Next() {
		var name, column string
		if err := rows.Scan(&name, &column); err != nil {
			return err
		}
		byName[name] = append(byName[name], column)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, exists := byName[indexName]; exists {
		return nil
	}
	for _, existing := range byName {
		if slicesEqual(existing, columns) {
			return nil
		}
	}

	quotedColumns := make([]string, len(columns))
	for i, column := range columns {
		quotedColumns[i] = quoteIdent(column)
	}
	statement := "ALTER TABLE " + quoteIdent(tableName) +
		" ADD INDEX " + quoteIdent(indexName) + " (" + strings.Join(quotedColumns, ", ") + ")"
	_, err = db.ExecContext(ctx, statement)
	return err
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func ensureColumn(ctx context.Context, db *sql.DB, tableName, columnName, alterSQL string) error {
	var count int
	if err := db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`,
		tableName,
		columnName,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := db.ExecContext(ctx, alterSQL)
	return err
}

func ensureProjectMemoryFulltextIndex(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='project_memories' AND INDEX_NAME='ft_project_memory_search'
ORDER BY SEQ_IN_INDEX`)
	if err != nil {
		return err
	}
	columns := make([]string, 0, 2)
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			rows.Close()
			return err
		}
		columns = append(columns, column)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(columns) == 1 && columns[0] == "search_text" {
		return nil
	}
	if _, err := db.ExecContext(ctx, `UPDATE project_memories SET search_text=CONCAT_WS(' ', statement_text, search_text) WHERE `+projectMemorySensitiveColumn+`=0`); err != nil {
		return err
	}
	if len(columns) > 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE project_memories DROP INDEX ft_project_memory_search`); err != nil {
			return err
		}
	}
	_, err = db.ExecContext(ctx, `ALTER TABLE project_memories ADD FULLTEXT INDEX ft_project_memory_search (search_text)`)
	return err
}

func ensureDefaultOperator(ctx context.Context, db *sql.DB) error {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM operators WHERE username = ?", "admin").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	key, err := randomHex(24)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO operators (operator_uid, name, client_type, username, password_hash, operator_key)
VALUES (?, ?, ?, ?, ?, ?)
`, "op_admin", "管理员", "web", "admin", hashPassword("admin123456"), "opk_"+key)
	return err
}

func removeLegacyReverseMCPScaffold(ctx context.Context, db *sql.DB) error {
	aliases := cleanAliasList(legacyReverseMCPIDsForStore)
	if len(aliases) == 0 {
		return nil
	}
	archive := &MySQLArchive{db: db}
	if err := archive.removeMCPFromSemanticAgentSnapshots(aliases); err != nil {
		return err
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(aliases)), ",")
	args := make([]any, 0, len(aliases)+1)
	for _, id := range aliases {
		args = append(args, id)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM semantic_agent_mcps WHERE mcp_id IN ("+placeholders+")", args...); err != nil {
		return err
	}
	args = append(args, "内置逆向专家")
	if _, err := db.ExecContext(ctx, "DELETE FROM mcp_catalog WHERE id IN ("+placeholders+") AND source = ?", args...); err != nil {
		return err
	}
	return nil
}

func seedRuntimeCatalogDefaults(ctx context.Context, db *sql.DB) error {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM runtime_catalog").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	archive := &MySQLArchive{db: db}
	defaults := []model.RuntimeCatalogItem{
		{
			ID: "python", Name: "Python", Description: "Python 解释器和脚本运行环境",
			RuntimeKind: "language", DefaultVersionConstraint: ">=3.12 <3.15",
			ExecutableNames: []string{"python", "python3", "pip", "pip3"},
			EnvTemplate:     map[string]string{"PIP_CACHE_DIR": "{runtime_dir}/caches/pip", "PIP_INDEX_URL": "{python_index_url}"},
			InstallStrategy: "uv_python", Enabled: true, Tags: []string{"language", "mcp"}, SortOrder: 10, BuiltIn: true,
		},
		{
			ID: "node", Name: "Node.js", Description: "Node.js 和 npm/npx 工具链",
			RuntimeKind: "language", DefaultVersionConstraint: ">=22 <23",
			ExecutableNames: []string{"node", "npm", "npx"},
			EnvTemplate:     map[string]string{"NPM_CONFIG_CACHE": "{runtime_dir}/caches/npm", "NPM_CONFIG_REGISTRY": "{npm_registry}"},
			InstallStrategy: "archive", Enabled: true, Tags: []string{"language", "mcp"}, SortOrder: 20, BuiltIn: true,
		},
		{
			ID: "java", Name: "Java", Description: "JDK 运行时，用于 jadx、apktool 和 Java 工具链",
			RuntimeKind: "language", DefaultVersionConstraint: ">=17 <22",
			ExecutableNames: []string{"java", "javac"},
			EnvTemplate:     map[string]string{"JAVA_HOME": "{install_dir}"},
			InstallStrategy: "archive", Enabled: true, Tags: []string{"language", "reverse"}, SortOrder: 30, BuiltIn: true,
		},
		{
			ID: "go", Name: "Go", Description: "Go 语言工具链和模块缓存",
			RuntimeKind: "language", DefaultVersionConstraint: ">=1.25 <1.26",
			ExecutableNames: []string{"go"},
			EnvTemplate:     map[string]string{"GOPATH": "{runtime_dir}/caches/go", "GOMODCACHE": "{runtime_dir}/caches/go/pkg/mod", "GOPROXY": "{go_proxy}"},
			InstallStrategy: "archive", Enabled: true, Tags: []string{"language"}, SortOrder: 40, BuiltIn: true,
		},
		{
			ID: "uv", Name: "uv", Description: "Python 包和解释器管理工具",
			RuntimeKind: "tool", DefaultVersionConstraint: "latest",
			ExecutableNames: []string{"uv"},
			EnvTemplate:     map[string]string{"UV_CACHE_DIR": "{runtime_dir}/caches/uv", "UV_TOOL_DIR": "{runtime_dir}/runtimes/uv-tools"},
			InstallStrategy: "archive", Enabled: true, Tags: []string{"tool", "python"}, SortOrder: 50, BuiltIn: true,
		},
	}
	for _, item := range defaults {
		if _, err := archive.UpsertRuntimeCatalogItem(item); err != nil {
			return err
		}
	}
	versions := []model.RuntimeVersion{
		{ID: "python-3.12", RuntimeID: "python", Version: "3.12", Channel: "stable", Status: "recommended", VersionOrder: 10, DefaultSelected: true, Enabled: true, SortOrder: 10},
		{ID: "python-3.13", RuntimeID: "python", Version: "3.13", Channel: "stable", Status: "available", VersionOrder: 20, Enabled: true, SortOrder: 20},
		{ID: "python-3.14", RuntimeID: "python", Version: "3.14", Channel: "stable", Status: "available", VersionOrder: 30, DefaultSelected: false, Enabled: true, SortOrder: 30},
		{ID: "node-22.16.0", RuntimeID: "node", Version: "22.16.0", Channel: "maintenance-lts", Status: "recommended", VersionOrder: 10, DefaultSelected: true, Enabled: true, SortOrder: 10},
		{ID: "node-24", RuntimeID: "node", Version: "24", Channel: "active-lts", Status: "available", VersionOrder: 20, Enabled: false, SortOrder: 20},
		{ID: "node-26", RuntimeID: "node", Version: "26", Channel: "current", Status: "available", VersionOrder: 30, Enabled: false, SortOrder: 30},
		{ID: "java-21", RuntimeID: "java", Version: "21", Channel: "lts", Status: "recommended", VersionOrder: 10, DefaultSelected: true, Enabled: true, SortOrder: 10},
		{ID: "java-17", RuntimeID: "java", Version: "17", Channel: "lts", Status: "disabled", VersionOrder: 20, Enabled: false, SortOrder: 20},
		{ID: "java-25", RuntimeID: "java", Version: "25", Channel: "current", Status: "disabled", VersionOrder: 90, Enabled: false, SortOrder: 90},
		{ID: "go-1.25.4", RuntimeID: "go", Version: "1.25.4", Channel: "stable", Status: "recommended", VersionOrder: 10, DefaultSelected: true, Enabled: true, SortOrder: 10},
		{ID: "uv-latest", RuntimeID: "uv", Version: "latest", Channel: "latest", Status: "recommended", VersionOrder: 10, DefaultSelected: true, Enabled: true, SortOrder: 10},
	}
	for _, item := range versions {
		if _, err := archive.UpsertRuntimeVersion(item); err != nil {
			return err
		}
	}
	artifacts := []model.RuntimeArtifact{
		{
			ID: "node-22.16.0-darwin-arm64", RuntimeID: "node", VersionID: "node-22.16.0", Platform: "darwin", Arch: "arm64", PackageKind: "tar.gz",
			Filename: "node-v22.16.0-darwin-arm64.tar.gz", SHA256: "1d7f34ec4c03e12d8b33481e5c4560432d7dc31a0ef3ff5a4d9a8ada7cf6ecc9",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/node/download?artifact=node-v22.16.0-darwin-arm64.tar.gz",
			BinPaths:     []string{"bin"}, Priority: 10, Enabled: true,
		},
		{
			ID: "node-22.16.0-windows-amd64", RuntimeID: "node", VersionID: "node-22.16.0", Platform: "windows", Arch: "amd64", PackageKind: "zip",
			Filename: "node-v22.16.0-win-x64.zip", SHA256: "21c2d9735c80b8f86dab19305aa6a9f6f59bbc808f68de3eef09d5832e3bfbbd",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/node/download?artifact=node-v22.16.0-win-x64.zip",
			BinPaths:     []string{"bin"}, Priority: 10, Enabled: true,
		},
		{
			ID: "node-22.16.0-linux-amd64", RuntimeID: "node", VersionID: "node-22.16.0", Platform: "linux", Arch: "amd64", PackageKind: "tar.gz",
			Filename: "node-v22.16.0-linux-x64.tar.gz", SHA256: "fb870226119d47378fa9c92c4535389c72dae14fcc7b47e6fdcc82c43de5a547",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/node/download?artifact=node-v22.16.0-linux-x64.tar.gz",
			BinPaths:     []string{"bin"}, Priority: 10, Enabled: true,
		},
		{
			ID: "uv-latest-darwin-arm64", RuntimeID: "uv", VersionID: "uv-latest", Platform: "darwin", Arch: "arm64", PackageKind: "tar.gz",
			Filename: "uv-aarch64-apple-darwin.tar.gz", SHA256: "d8f59c38e8c4168ee468d423cd63184be12fa6995a4283d41ee1a14d003c9453",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/uv/download?artifact=uv-aarch64-apple-darwin.tar.gz",
			BinPaths:     []string{"."}, Priority: 10, Enabled: true,
		},
		{
			ID: "uv-latest-windows-amd64", RuntimeID: "uv", VersionID: "uv-latest", Platform: "windows", Arch: "amd64", PackageKind: "zip",
			Filename: "uv-x86_64-pc-windows-msvc.zip", SHA256: "1665fc8e37b5d70a134820d6d7891747471a2ac8bc940ee7af0b69fd03b28d61",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/uv/download?artifact=uv-x86_64-pc-windows-msvc.zip",
			BinPaths:     []string{"."}, Priority: 10, Enabled: true,
		},
		{
			ID: "uv-latest-linux-amd64", RuntimeID: "uv", VersionID: "uv-latest", Platform: "linux", Arch: "amd64", PackageKind: "tar.gz",
			Filename: "uv-x86_64-unknown-linux-gnu.tar.gz", SHA256: "7035608168e106375b36d0c818d537a889c51a8625fe7f8f7cad5e62b947c368",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/uv/download?artifact=uv-x86_64-unknown-linux-gnu.tar.gz",
			BinPaths:     []string{"."}, Priority: 10, Enabled: true,
		},
		{
			ID: "go-1.25.4-darwin-arm64", RuntimeID: "go", VersionID: "go-1.25.4", Platform: "darwin", Arch: "arm64", PackageKind: "tar.gz",
			Filename: "go1.25.4.darwin-arm64.tar.gz", SHA256: "c1b04e74251fe1dfbc5382e73d0c6d96f49642d8aebb7ee10a7ecd4cae36ebd2",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/go/download?artifact=go1.25.4.darwin-arm64.tar.gz",
			ExtractRoot:  "go", BinPaths: []string{"bin"}, EnvPatch: map[string]string{"GOROOT": "{install_dir}"}, Priority: 10, Enabled: true,
		},
		{
			ID: "go-1.25.4-darwin-amd64", RuntimeID: "go", VersionID: "go-1.25.4", Platform: "darwin", Arch: "amd64", PackageKind: "tar.gz",
			Filename: "go1.25.4.darwin-amd64.tar.gz", SHA256: "33ba03ff9973f5bd26d516eea35328832a9525ecc4d169b15937ffe2ce66a7d8",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/go/download?artifact=go1.25.4.darwin-amd64.tar.gz",
			ExtractRoot:  "go", BinPaths: []string{"bin"}, EnvPatch: map[string]string{"GOROOT": "{install_dir}"}, Priority: 10, Enabled: true,
		},
		{
			ID: "go-1.25.4-windows-amd64", RuntimeID: "go", VersionID: "go-1.25.4", Platform: "windows", Arch: "amd64", PackageKind: "zip",
			Filename: "go1.25.4.windows-amd64.zip", SHA256: "6dad204d42719795f22067553b2b042c0e710b32c5a00f6c67892865167fdfd0",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/go/download?artifact=go1.25.4.windows-amd64.zip",
			ExtractRoot:  "go", BinPaths: []string{"bin"}, EnvPatch: map[string]string{"GOROOT": "{install_dir}"}, Priority: 10, Enabled: true,
		},
		{
			ID: "go-1.25.4-linux-amd64", RuntimeID: "go", VersionID: "go-1.25.4", Platform: "linux", Arch: "amd64", PackageKind: "tar.gz",
			Filename: "go1.25.4.linux-amd64.tar.gz", SHA256: "9fa5ffeda4170de60f67f3aa0f824e426421ba724c21e133c1e35d6159ca1bec",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/go/download?artifact=go1.25.4.linux-amd64.tar.gz",
			ExtractRoot:  "go", BinPaths: []string{"bin"}, EnvPatch: map[string]string{"GOROOT": "{install_dir}"}, Priority: 10, Enabled: true,
		},
		{
			ID: "java-21-darwin-arm64", RuntimeID: "java", VersionID: "java-21", Platform: "darwin", Arch: "arm64", PackageKind: "tar.gz",
			Filename: "OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.11_10.tar.gz", SHA256: "6ebcf221c9b41507b14c098e93c6ead6440b8d9bd154f8ec666c4c73abbdb201",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/java/download?artifact=OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.11_10.tar.gz",
			ExtractRoot:  "Contents/Home", BinPaths: []string{"bin"}, Priority: 10, Enabled: true,
		},
		{
			ID: "java-21-darwin-amd64", RuntimeID: "java", VersionID: "java-21", Platform: "darwin", Arch: "amd64", PackageKind: "tar.gz",
			Filename: "OpenJDK21U-jdk_x64_mac_hotspot_21.0.11_10.tar.gz", SHA256: "34180eb03e6d207c388cce3da668f6cc7cd7508c185c24782fadac2c9c0e66f9",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/java/download?artifact=OpenJDK21U-jdk_x64_mac_hotspot_21.0.11_10.tar.gz",
			ExtractRoot:  "Contents/Home", BinPaths: []string{"bin"}, Priority: 10, Enabled: true,
		},
		{
			ID: "java-21-windows-amd64", RuntimeID: "java", VersionID: "java-21", Platform: "windows", Arch: "amd64", PackageKind: "zip",
			Filename: "OpenJDK21U-jdk_x64_windows_hotspot_21.0.11_10.zip", SHA256: "d3625e7cadf23787ea540229544b6e2ab494b3b54da1801879e583e1dfee0a64",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/java/download?artifact=OpenJDK21U-jdk_x64_windows_hotspot_21.0.11_10.zip",
			ExtractRoot:  "", BinPaths: []string{"bin"}, Priority: 10, Enabled: true,
		},
		{
			ID: "java-21-windows-arm64", RuntimeID: "java", VersionID: "java-21", Platform: "windows", Arch: "arm64", PackageKind: "zip",
			Filename: "OpenJDK21U-jdk_aarch64_windows_hotspot_21.0.11_10.zip", SHA256: "a6ef0829c261f856ef93d64724be6bc8fd8355c2bbddbd26d6544478a8353f87",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/java/download?artifact=OpenJDK21U-jdk_aarch64_windows_hotspot_21.0.11_10.zip",
			ExtractRoot:  "", BinPaths: []string{"bin"}, Priority: 10, Enabled: false,
		},
		{
			ID: "java-17-darwin-arm64", RuntimeID: "java", VersionID: "java-17", Platform: "darwin", Arch: "arm64", PackageKind: "tar.gz",
			Filename: "OpenJDK17U-jdk_aarch64_mac_hotspot_17.0.19_10.tar.gz", SHA256: "8fa1eff40bb637a33613b2ccb8b12c70dc3661cc22cf8e784943715769a05336",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/java/download?artifact=OpenJDK17U-jdk_aarch64_mac_hotspot_17.0.19_10.tar.gz",
			ExtractRoot:  "Contents/Home", BinPaths: []string{"bin"}, Priority: 20, Enabled: false,
		},
		{
			ID: "java-17-darwin-amd64", RuntimeID: "java", VersionID: "java-17", Platform: "darwin", Arch: "amd64", PackageKind: "tar.gz",
			Filename: "OpenJDK17U-jdk_x64_mac_hotspot_17.0.19_10.tar.gz", SHA256: "03632d1fbf139ab3719a9f4b47dc206251449b87557143c822336dbf8c06560f",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/java/download?artifact=OpenJDK17U-jdk_x64_mac_hotspot_17.0.19_10.tar.gz",
			ExtractRoot:  "Contents/Home", BinPaths: []string{"bin"}, Priority: 20, Enabled: false,
		},
		{
			ID: "java-17-windows-amd64", RuntimeID: "java", VersionID: "java-17", Platform: "windows", Arch: "amd64", PackageKind: "zip",
			Filename: "OpenJDK17U-jdk_x64_windows_hotspot_17.0.19_10.zip", SHA256: "b5b235c48adf6a081874b812c630b9f4b5f637b7a5ed18b9174d08a41ec4c235",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/java/download?artifact=OpenJDK17U-jdk_x64_windows_hotspot_17.0.19_10.zip",
			ExtractRoot:  "", BinPaths: []string{"bin"}, Priority: 20, Enabled: false,
		},
		{
			ID: "java-17-windows-arm64", RuntimeID: "java", VersionID: "java-17", Platform: "windows", Arch: "arm64", PackageKind: "zip",
			Filename:     "OpenJDK17U-jdk_aarch64_windows_hotspot_17.0.19_10.zip",
			DownloadPath: "https://www.xyapi.top/codex/api/runtime/java/download?artifact=OpenJDK17U-jdk_aarch64_windows_hotspot_17.0.19_10.zip",
			ExtractRoot:  "", BinPaths: []string{"bin"}, Priority: 20, Enabled: false,
		},
	}
	for _, item := range artifacts {
		if _, err := archive.UpsertRuntimeArtifact(item); err != nil {
			return err
		}
	}
	mirrors := []model.RuntimeMirror{
		{ID: "node-npmmirror", RuntimeID: "node", Name: "Node npmmirror", BaseURL: "https://cdn.npmmirror.com/binaries/node", URLTemplate: "{base}/v{version}/{filename}", Priority: 10, TimeoutSeconds: 60, Enabled: true, Tags: []string{"china", "mirror", "verified"}},
		{ID: "node-aliyun", RuntimeID: "node", Name: "Node 阿里云镜像", BaseURL: "https://mirrors.aliyun.com/nodejs-release", URLTemplate: "{base}/v{version}/{filename}", Priority: 20, TimeoutSeconds: 60, Enabled: true, Tags: []string{"china", "mirror", "verified"}},
		{ID: "node-tencent", RuntimeID: "node", Name: "Node 腾讯云镜像", BaseURL: "https://mirrors.tencent.com/nodejs-release", URLTemplate: "{base}/v{version}/{filename}", Priority: 30, TimeoutSeconds: 60, Enabled: true, Tags: []string{"china", "mirror", "verified"}},
		{ID: "npm-npmmirror", RuntimeID: "node", Name: "npm npmmirror", BaseURL: "https://registry.npmmirror.com", URLTemplate: "{base}", Priority: 10, TimeoutSeconds: 20, Enabled: true, Tags: []string{"registry", "china"}},
		{ID: "python-pypi-tsinghua", RuntimeID: "python", Name: "PyPI 清华镜像", BaseURL: "https://pypi.tuna.tsinghua.edu.cn/simple", URLTemplate: "{base}", Priority: 90, TimeoutSeconds: 20, Enabled: true, Tags: []string{"registry", "china"}},
		{ID: "python-standalone-npmmirror", RuntimeID: "python", Name: "Python standalone npmmirror", BaseURL: "https://registry.npmmirror.com/-/binary/python-build-standalone", URLTemplate: "{base}", Priority: 10, TimeoutSeconds: 60, Enabled: true, Tags: []string{"china", "mirror", "verified"}},
		{ID: "go-aliyun", RuntimeID: "go", Name: "Go 阿里云镜像", BaseURL: "https://mirrors.aliyun.com/golang", URLTemplate: "{base}/{filename}", Priority: 10, TimeoutSeconds: 60, Enabled: true, Tags: []string{"china", "mirror", "verified"}},
		{ID: "go-goproxy-cn", RuntimeID: "go", Name: "Go 模块 goproxy.cn", BaseURL: "https://goproxy.cn", URLTemplate: "{base}", Priority: 10, TimeoutSeconds: 20, Enabled: true, Tags: []string{"proxy", "china"}},
		{
			ID: "java-adoptium-tuna", RuntimeID: "java", VersionID: "java-21", Name: "Java 清华 Adoptium 镜像",
			BaseURL: "https://mirrors.tuna.tsinghua.edu.cn/Adoptium", URLTemplate: "{base}/{version}/jdk/{adoptium_arch}/{adoptium_os}/{filename}",
			Priority: 10, TimeoutSeconds: 60, Enabled: true, Headers: map[string]string{"User-Agent": "chat-codex-launcher/0.1"},
			Tags: []string{"china", "mirror", "verified"},
		},
		{
			ID: "uv-ustc", RuntimeID: "uv", Name: "uv USTC 镜像",
			BaseURL: "https://mirrors.ustc.edu.cn/github-release/astral-sh/uv/LatestRelease", URLTemplate: "{base}/{filename}",
			Priority: 10, TimeoutSeconds: 60, Enabled: true, Headers: map[string]string{"User-Agent": "chat-codex-launcher/0.1"},
			Tags: []string{"china", "mirror", "verified"},
		},
	}
	for _, item := range mirrors {
		if _, err := archive.UpsertRuntimeMirror(item); err != nil {
			return err
		}
	}
	return nil
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func quoteIdent(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS operators (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  operator_uid VARCHAR(64) NOT NULL,
  name VARCHAR(128) NOT NULL DEFAULT '',
  client_type VARCHAR(32) NOT NULL DEFAULT '',
  username VARCHAR(64) NOT NULL DEFAULT '',
  password_hash VARCHAR(255) NOT NULL DEFAULT '',
  operator_key VARCHAR(128) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_operator_uid (operator_uid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS machines (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  machine_id VARCHAR(128) NOT NULL,
  hostname VARCHAR(255) NOT NULL DEFAULT '',
  os_name VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT 'offline',
  last_seen_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_machine_id (machine_id),
  KEY idx_machines_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS agents (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  agent_id VARCHAR(191) NOT NULL,
  machine_id VARCHAR(128) NOT NULL,
  project_id VARCHAR(128) NOT NULL,
  project_root VARCHAR(1024) NOT NULL,
  hostname VARCHAR(255) NOT NULL DEFAULT '',
  version VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT 'offline',
  current_task_id VARCHAR(64) NOT NULL DEFAULT '',
  last_seen_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_agent_id (agent_id),
  KEY idx_agents_machine_id (machine_id),
  KEY idx_agents_project_id (project_id),
  KEY idx_agents_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS sessions (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  session_id VARCHAR(64) NOT NULL,
  agent_id VARCHAR(191) NOT NULL,
  machine_id VARCHAR(128) NOT NULL,
  project_id VARCHAR(128) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  last_task_id VARCHAR(64) NOT NULL DEFAULT '',
  summary TEXT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_session_id (session_id),
  KEY idx_sessions_agent_id (agent_id),
  KEY idx_sessions_project_id (project_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS tasks (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  task_id VARCHAR(64) NOT NULL,
  agent_id VARCHAR(191) NOT NULL,
  machine_id VARCHAR(128) NOT NULL,
  project_id VARCHAR(128) NOT NULL,
  project_root VARCHAR(1024) NOT NULL,
  session_id VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL,
  result_text LONGTEXT NULL,
  error_text LONGTEXT NULL,
  input_json JSON NOT NULL,
  artifacts_json JSON NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_task_id (task_id),
  KEY idx_tasks_agent_id (agent_id),
  KEY idx_tasks_machine_id (machine_id),
  KEY idx_tasks_project_id (project_id),
  KEY idx_tasks_status (status),
  KEY idx_tasks_session_id (session_id),
  KEY idx_tasks_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS task_events (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  task_id VARCHAR(64) NOT NULL,
  event_type VARCHAR(64) NOT NULL,
  session_id VARCHAR(64) NOT NULL DEFAULT '',
  permission_id VARCHAR(64) NOT NULL DEFAULT '',
  payload_json JSON NULL,
  content_text LONGTEXT NULL,
  error_text LONGTEXT NULL,
  sent_at DATETIME NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_task_events_task_sent_id (task_id, sent_at, id),
  KEY idx_task_events_task_id_id (task_id, id),
  KEY idx_task_events_task_type_sent_id (task_id, event_type, sent_at, id),
  KEY idx_task_events_sent_at (sent_at),
  KEY idx_task_events_event_type (event_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS chat_queue_items (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  queue_item_id VARCHAR(64) NOT NULL,
  operator_id BIGINT NOT NULL,
  session_id VARCHAR(64) NOT NULL,
  agent_id VARCHAR(191) NOT NULL,
  machine_id VARCHAR(128) NOT NULL DEFAULT '',
  project_id VARCHAR(128) NOT NULL,
  project_root VARCHAR(1024) NOT NULL DEFAULT '',
  payload_json JSON NOT NULL,
  position_value BIGINT NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'queued',
  version_value BIGINT NOT NULL DEFAULT 1,
  task_id VARCHAR(64) NOT NULL DEFAULT '',
  injection_version BIGINT NOT NULL DEFAULT 0,
  error_text TEXT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_chat_queue_item_id (queue_item_id),
  KEY idx_chat_queue_session (operator_id, session_id, position_value),
  KEY idx_chat_queue_task (task_id),
  KEY idx_chat_queue_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS task_approvals (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  task_id VARCHAR(64) NOT NULL,
  session_id VARCHAR(64) NOT NULL DEFAULT '',
  agent_id VARCHAR(191) NOT NULL,
  permission_id VARCHAR(64) NOT NULL,
  permission_name VARCHAR(64) NOT NULL DEFAULT '',
  reply VARCHAR(32) NOT NULL DEFAULT '',
  patterns_json JSON NULL,
  metadata_json JSON NULL,
  message_text TEXT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_task_permission (task_id, permission_id),
  KEY idx_task_approvals_agent_id (agent_id),
  KEY idx_task_approvals_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS device_settings (
  operator_id BIGINT NOT NULL,
  agent_id VARCHAR(128) NOT NULL,
  setting_key VARCHAR(64) NOT NULL,
  setting_value TEXT NOT NULL,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (operator_id, agent_id, setting_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS operator_device_order_counters (
  operator_id BIGINT NOT NULL,
  next_sort_order BIGINT NOT NULL DEFAULT 1,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (operator_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS operator_device_preferences (
  operator_id BIGINT NOT NULL,
  machine_id VARCHAR(128) NOT NULL,
  display_name VARCHAR(160) NOT NULL DEFAULT '',
  sort_order BIGINT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (operator_id, machine_id),
  UNIQUE KEY uk_operator_device_sort (operator_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS operator_agent_preferences (
  operator_id BIGINT NOT NULL,
  machine_id VARCHAR(128) NOT NULL,
  agent_id VARCHAR(191) NOT NULL,
  sort_order BIGINT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (operator_id, machine_id, agent_id),
  UNIQUE KEY uk_operator_agent_sort (operator_id, machine_id, sort_order),
  KEY idx_operator_agent_machine (operator_id, machine_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS push_devices (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  operator_id BIGINT NOT NULL,
  platform VARCHAR(32) NOT NULL DEFAULT 'android',
  vendor VARCHAR(32) NOT NULL DEFAULT 'jpush',
  registration_id VARCHAR(255) NOT NULL,
  device_id VARCHAR(191) NOT NULL DEFAULT '',
  device_brand VARCHAR(64) NOT NULL DEFAULT '',
  device_model VARCHAR(128) NOT NULL DEFAULT '',
  app_version VARCHAR(64) NOT NULL DEFAULT '',
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_push_operator_device (operator_id, device_id),
  KEY idx_push_operator_id (operator_id),
 KEY idx_push_registration_id (registration_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS skills (
	  id VARCHAR(128) PRIMARY KEY,
	  name VARCHAR(128) NOT NULL,
	  description TEXT NOT NULL,
	  content MEDIUMTEXT NOT NULL,
	  package_files_json JSON NULL,
	  category VARCHAR(64) NOT NULL DEFAULT '通用',
	  source VARCHAR(64) NOT NULL DEFAULT '',
	  enabled TINYINT(1) NOT NULL DEFAULT 1,
  tags_json JSON NOT NULL,
  sort_order INT NOT NULL DEFAULT 100,
  built_in TINYINT(1) NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_skills_enabled_sort (enabled, sort_order, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS mcp_catalog (
  id VARCHAR(128) PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  title VARCHAR(128) NOT NULL DEFAULT '',
  description TEXT NOT NULL,
  type VARCHAR(32) NOT NULL DEFAULT 'local',
  source VARCHAR(64) NOT NULL DEFAULT '',
  source_url VARCHAR(1024) NOT NULL DEFAULT '',
  credential_url VARCHAR(1024) NOT NULL DEFAULT '',
  category VARCHAR(64) NOT NULL DEFAULT '通用',
  recommended TINYINT(1) NOT NULL DEFAULT 0,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  tags_json JSON NOT NULL,
  sort_order INT NOT NULL DEFAULT 100,
  built_in TINYINT(1) NOT NULL DEFAULT 0,
  config_json JSON NOT NULL,
  install_json JSON NULL,
  launch_ready TINYINT(1) NOT NULL DEFAULT 1,
  launch_block_reason TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_mcp_catalog_name (name),
  KEY idx_mcp_catalog_enabled_sort (enabled, recommended, sort_order, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS environment_presets (
  id VARCHAR(128) PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  description TEXT NOT NULL,
  category VARCHAR(64) NOT NULL DEFAULT '通用',
  source VARCHAR(64) NOT NULL DEFAULT '',
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  tags_json JSON NOT NULL,
  variables_json JSON NOT NULL,
  sort_order INT NOT NULL DEFAULT 100,
  built_in TINYINT(1) NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	KEY idx_environment_presets_enabled_sort (enabled, sort_order, updated_at),
  KEY idx_environment_presets_category (category, enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS runtime_catalog (
  id VARCHAR(128) PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  description TEXT NOT NULL,
  runtime_kind VARCHAR(32) NOT NULL DEFAULT 'language',
  default_version_constraint VARCHAR(128) NOT NULL DEFAULT '',
  executable_names_json JSON NOT NULL,
  env_template_json JSON NOT NULL,
  install_strategy VARCHAR(32) NOT NULL DEFAULT 'archive',
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  tags_json JSON NOT NULL,
  sort_order INT NOT NULL DEFAULT 100,
  built_in TINYINT(1) NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_runtime_catalog_enabled_sort (enabled, sort_order, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS runtime_versions (
  id VARCHAR(128) PRIMARY KEY,
  runtime_id VARCHAR(128) NOT NULL,
  version VARCHAR(64) NOT NULL,
  channel VARCHAR(32) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT 'available',
  version_order INT NOT NULL DEFAULT 100,
  default_selected TINYINT(1) NOT NULL DEFAULT 0,
  min_os_version_json JSON NOT NULL,
  install_manifest_json JSON NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  sort_order INT NOT NULL DEFAULT 100,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_runtime_versions_runtime_version (runtime_id, version),
  KEY idx_runtime_versions_runtime_enabled (runtime_id, enabled, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS runtime_artifacts (
  id VARCHAR(128) PRIMARY KEY,
  runtime_id VARCHAR(128) NOT NULL,
  version_id VARCHAR(128) NOT NULL,
  platform VARCHAR(32) NOT NULL,
  arch VARCHAR(32) NOT NULL,
  package_kind VARCHAR(32) NOT NULL DEFAULT 'archive',
  filename VARCHAR(255) NOT NULL,
  size_bytes BIGINT NOT NULL DEFAULT 0,
  sha256 VARCHAR(128) NOT NULL DEFAULT '',
  storage_key VARCHAR(1024) NOT NULL DEFAULT '',
  download_path VARCHAR(1024) NOT NULL DEFAULT '',
  extract_root VARCHAR(255) NOT NULL DEFAULT '',
  bin_paths_json JSON NOT NULL,
  env_patch_json JSON NOT NULL,
  priority INT NOT NULL DEFAULT 100,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_runtime_artifacts_runtime_version (runtime_id, version_id, enabled, priority),
  KEY idx_runtime_artifacts_platform (platform, arch)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS runtime_mirrors (
  id VARCHAR(128) PRIMARY KEY,
  runtime_id VARCHAR(128) NOT NULL DEFAULT '',
  version_id VARCHAR(128) NOT NULL DEFAULT '',
  name VARCHAR(128) NOT NULL,
  description TEXT NOT NULL,
  base_url VARCHAR(1024) NOT NULL DEFAULT '',
  url_template VARCHAR(2048) NOT NULL DEFAULT '',
  checksum_url_template VARCHAR(2048) NOT NULL DEFAULT '',
  platform VARCHAR(32) NOT NULL DEFAULT '',
  arch VARCHAR(32) NOT NULL DEFAULT '',
  priority INT NOT NULL DEFAULT 100,
  timeout_seconds INT NOT NULL DEFAULT 30,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  headers_json JSON NOT NULL,
  tags_json JSON NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_runtime_mirrors_runtime_version (runtime_id, version_id, enabled, priority),
  KEY idx_runtime_mirrors_platform (platform, arch)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS tool_catalog (
  id VARCHAR(128) PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  description TEXT NOT NULL,
  category VARCHAR(64) NOT NULL DEFAULT '通用',
  permission_key VARCHAR(128) NOT NULL,
  pattern VARCHAR(255) NOT NULL DEFAULT '*',
  default_action VARCHAR(16) NOT NULL DEFAULT 'ask',
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  tags_json JSON NOT NULL,
  sort_order INT NOT NULL DEFAULT 100,
  built_in TINYINT(1) NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_tool_catalog_enabled_sort (enabled, sort_order, updated_at),
  KEY idx_tool_catalog_permission (permission_key, pattern)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS semantic_agents (
  id VARCHAR(128) PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  description TEXT NOT NULL,
  icon VARCHAR(64) NOT NULL DEFAULT '',
  color VARCHAR(64) NOT NULL DEFAULT '',
  opencode_name VARCHAR(128) NOT NULL DEFAULT '',
  prompt MEDIUMTEXT NOT NULL,
  skill_ids_json JSON NULL,
  mcp_ids_json JSON NULL,
  skills_json JSON NOT NULL,
  skill_definitions_json JSON NOT NULL,
  recommended_mcp_servers_json JSON NOT NULL,
  tool_permissions_json JSON NOT NULL,
  model VARCHAR(128) NOT NULL DEFAULT '',
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  sort_order INT NOT NULL DEFAULT 100,
  built_in TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_semantic_agents_enabled_sort (enabled, sort_order, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS semantic_agent_skills (
  agent_id VARCHAR(128) NOT NULL,
  skill_id VARCHAR(128) NOT NULL,
  sort_order INT NOT NULL DEFAULT 100,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (agent_id, skill_id),
  KEY idx_semantic_agent_skills_skill (skill_id),
  KEY idx_semantic_agent_skills_agent_sort (agent_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS semantic_agent_mcps (
  agent_id VARCHAR(128) NOT NULL,
  mcp_id VARCHAR(128) NOT NULL,
  sort_order INT NOT NULL DEFAULT 100,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (agent_id, mcp_id),
  KEY idx_semantic_agent_mcps_mcp (mcp_id),
  KEY idx_semantic_agent_mcps_agent_sort (agent_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS semantic_agent_runtimes (
  agent_id VARCHAR(128) NOT NULL,
  runtime_id VARCHAR(128) NOT NULL,
  version_constraint VARCHAR(128) NOT NULL DEFAULT '',
  required TINYINT(1) NOT NULL DEFAULT 1,
  purpose VARCHAR(255) NOT NULL DEFAULT '',
  sort_order INT NOT NULL DEFAULT 100,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (agent_id, runtime_id),
  KEY idx_semantic_agent_runtimes_runtime (runtime_id),
  KEY idx_semantic_agent_runtimes_agent_sort (agent_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS runtime_install_jobs (
  id VARCHAR(128) PRIMARY KEY,
  operator_id BIGINT NOT NULL DEFAULT 0,
  machine_id VARCHAR(128) NOT NULL,
  launcher_agent_id VARCHAR(191) NOT NULL,
  semantic_agent_id VARCHAR(128) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  current_step VARCHAR(32) NOT NULL DEFAULT '',
  progress_percent INT NOT NULL DEFAULT 0,
  requires_user_action TINYINT(1) NOT NULL DEFAULT 0,
  user_action_json JSON NULL,
  requested_by VARCHAR(128) NOT NULL DEFAULT '',
  started_at DATETIME NULL,
  completed_at DATETIME NULL,
  error TEXT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_runtime_jobs_operator_created (operator_id, created_at),
  KEY idx_runtime_jobs_machine_created (machine_id, created_at),
  KEY idx_runtime_jobs_status (status, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS runtime_user_action_notifications (
  operator_id BIGINT NOT NULL,
  job_id VARCHAR(128) NOT NULL,
  action_id VARCHAR(191) NOT NULL,
  channel VARCHAR(32) NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (operator_id, job_id, action_id, channel),
  KEY idx_runtime_action_notices_job (job_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS runtime_install_job_items (
  job_id VARCHAR(128) NOT NULL,
  item_type VARCHAR(32) NOT NULL,
  item_id VARCHAR(191) NOT NULL,
  name VARCHAR(255) NOT NULL DEFAULT '',
  required TINYINT(1) NOT NULL DEFAULT 1,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  progress_percent INT NOT NULL DEFAULT 0,
  error TEXT NULL,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (job_id, item_type, item_id),
  KEY idx_runtime_job_items_status (job_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS runtime_install_events (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  job_id VARCHAR(128) NOT NULL,
  sequence BIGINT NOT NULL,
  item_type VARCHAR(32) NOT NULL DEFAULT '',
  item_id VARCHAR(191) NOT NULL DEFAULT '',
  phase VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT '',
  message TEXT NULL,
  received_bytes BIGINT NOT NULL DEFAULT 0,
  total_bytes BIGINT NOT NULL DEFAULT 0,
  progress_percent INT NOT NULL DEFAULT 0,
  details_json JSON NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_runtime_events_job_sequence (job_id, sequence),
  KEY idx_runtime_events_job_id (job_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
}

func (m *MySQLArchive) ChangePassword(operatorID int64, newPassword string) error {
	_, err := m.db.Exec("UPDATE operators SET password_hash = ? WHERE id = ?", hashPassword(newPassword), operatorID)
	return err
}

func (m *MySQLArchive) DeleteOperator(operatorID int64) error {
	_, err := m.db.Exec("DELETE FROM operators WHERE id = ?", operatorID)
	if err != nil {
		return err
	}
	m.db.Exec("DELETE FROM sessions WHERE operator_id = ?", operatorID)
	m.db.Exec("DELETE FROM tasks WHERE operator_id = ?", operatorID)
	m.db.Exec("DELETE FROM chat_queue_items WHERE operator_id = ?", operatorID)
	m.db.Exec("DELETE FROM device_settings WHERE operator_id = ?", operatorID)
	m.db.Exec("DELETE FROM operator_device_preferences WHERE operator_id = ?", operatorID)
	m.db.Exec("DELETE FROM operator_device_order_counters WHERE operator_id = ?", operatorID)
	m.db.Exec("DELETE FROM operator_agent_preferences WHERE operator_id = ?", operatorID)
	m.db.Exec("DELETE FROM push_devices WHERE operator_id = ?", operatorID)
	m.db.Exec("DELETE FROM app_notifications WHERE operator_id = ?", operatorID)
	m.db.Exec("DELETE FROM app_notification_changes WHERE operator_id = ?", operatorID)
	return nil
}

func (m *MySQLArchive) SetDeviceSetting(operatorID int64, agentID, key, value string) error {
	_, err := m.db.Exec(`
INSERT INTO device_settings (operator_id, agent_id, setting_key, setting_value) VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value)
`, operatorID, agentID, key, value)
	return err
}

func (m *MySQLArchive) GetDeviceSettings(operatorID int64, agentID string) (map[string]string, error) {
	rows, err := m.db.Query("SELECT setting_key, setting_value FROM device_settings WHERE operator_id = ? AND agent_id = ?", operatorID, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, rows.Err()
}

func (m *MySQLArchive) EnsureDevicePreferences(operatorID int64, machineIDs []string) ([]model.DevicePreference, error) {
	ids := uniqueNonEmptyStrings(machineIDs)
	if len(ids) == 0 {
		return []model.DevicePreference{}, nil
	}
	tx, err := m.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT IGNORE INTO operator_device_order_counters (operator_id, next_sort_order) VALUES (?, 1)`, operatorID); err != nil {
		return nil, err
	}
	var nextOrder int64
	if err = tx.QueryRow(`SELECT next_sort_order FROM operator_device_order_counters WHERE operator_id = ? FOR UPDATE`, operatorID).Scan(&nextOrder); err != nil {
		return nil, err
	}

	rows, err := tx.Query(`SELECT machine_id, display_name, sort_order, created_at, updated_at FROM operator_device_preferences WHERE operator_id = ?`, operatorID)
	if err != nil {
		return nil, err
	}
	byMachine := make(map[string]model.DevicePreference, len(ids))
	for rows.Next() {
		preference := model.DevicePreference{OperatorID: operatorID}
		if err = rows.Scan(&preference.MachineID, &preference.DisplayName, &preference.SortOrder, &preference.CreatedAt, &preference.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		byMachine[preference.MachineID] = preference
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	now := time.Now()
	missing := make([]model.DevicePreference, 0, len(ids))
	for _, machineID := range ids {
		if _, exists := byMachine[machineID]; exists {
			continue
		}
		preference := model.DevicePreference{
			OperatorID: operatorID,
			MachineID:  machineID,
			SortOrder:  nextOrder,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		nextOrder++
		missing = append(missing, preference)
		byMachine[machineID] = preference
	}
	if len(missing) > 0 {
		placeholders := make([]string, 0, len(missing))
		args := make([]any, 0, len(missing)*4)
		for _, preference := range missing {
			placeholders = append(placeholders, "(?, ?, '', ?)")
			args = append(args, operatorID, preference.MachineID, preference.SortOrder)
		}
		query := `INSERT INTO operator_device_preferences (operator_id, machine_id, display_name, sort_order) VALUES ` + strings.Join(placeholders, ",")
		if _, err = tx.Exec(query, args...); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(`UPDATE operator_device_order_counters SET next_sort_order = ? WHERE operator_id = ?`, nextOrder, operatorID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	result := make([]model.DevicePreference, 0, len(ids))
	for _, machineID := range ids {
		result = append(result, byMachine[machineID])
	}
	return result, nil
}

func (m *MySQLArchive) UpdateDeviceDisplayName(operatorID int64, machineID, displayName string) (*model.DevicePreference, error) {
	preferences, err := m.EnsureDevicePreferences(operatorID, []string{machineID})
	if err != nil {
		return nil, err
	}
	if len(preferences) == 0 {
		return nil, errors.New("device preference not found")
	}
	if _, err = m.db.Exec(`UPDATE operator_device_preferences SET display_name = ? WHERE operator_id = ? AND machine_id = ?`, displayName, operatorID, machineID); err != nil {
		return nil, err
	}
	preference := preferences[0]
	preference.DisplayName = displayName
	preference.UpdatedAt = time.Now()
	return &preference, nil
}

func (m *MySQLArchive) ReorderDevicePreferences(operatorID int64, machineIDs []string) ([]model.DevicePreference, error) {
	ids := uniqueNonEmptyStrings(machineIDs)
	if _, err := m.EnsureDevicePreferences(operatorID, ids); err != nil {
		return nil, err
	}
	tx, err := m.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var nextOrder int64
	if err = tx.QueryRow(`SELECT next_sort_order FROM operator_device_order_counters WHERE operator_id = ? FOR UPDATE`, operatorID).Scan(&nextOrder); err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT machine_id, display_name, sort_order, created_at, updated_at FROM operator_device_preferences WHERE operator_id = ? ORDER BY sort_order, machine_id`, operatorID)
	if err != nil {
		return nil, err
	}
	byMachine := make(map[string]model.DevicePreference)
	existingOrder := make([]string, 0)
	for rows.Next() {
		preference := model.DevicePreference{OperatorID: operatorID}
		if err = rows.Scan(&preference.MachineID, &preference.DisplayName, &preference.SortOrder, &preference.CreatedAt, &preference.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		byMachine[preference.MachineID] = preference
		existingOrder = append(existingOrder, preference.MachineID)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	included := make(map[string]bool, len(ids))
	ordered := make([]string, 0, len(existingOrder))
	for _, machineID := range ids {
		if _, exists := byMachine[machineID]; exists {
			ordered = append(ordered, machineID)
			included[machineID] = true
		}
	}
	for _, machineID := range existingOrder {
		if !included[machineID] {
			ordered = append(ordered, machineID)
		}
	}
	if _, err = tx.Exec(`UPDATE operator_device_preferences SET sort_order = -sort_order WHERE operator_id = ?`, operatorID); err != nil {
		return nil, err
	}
	now := time.Now()
	result := make([]model.DevicePreference, 0, len(ordered))
	for index, machineID := range ordered {
		order := int64(index + 1)
		if _, err = tx.Exec(`UPDATE operator_device_preferences SET sort_order = ? WHERE operator_id = ? AND machine_id = ?`, order, operatorID, machineID); err != nil {
			return nil, err
		}
		preference := byMachine[machineID]
		preference.SortOrder = order
		preference.UpdatedAt = now
		result = append(result, preference)
	}
	if _, err = tx.Exec(`UPDATE operator_device_order_counters SET next_sort_order = ? WHERE operator_id = ?`, int64(len(ordered)+1), operatorID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *MySQLArchive) ListAgentPreferences(operatorID int64, machineIDs []string) ([]model.AgentPreference, error) {
	ids := uniqueNonEmptyStrings(machineIDs)
	if len(ids) == 0 {
		return []model.AgentPreference{}, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, operatorID)
	for index, machineID := range ids {
		placeholders[index] = "?"
		args = append(args, machineID)
	}
	query := `SELECT machine_id, agent_id, sort_order, created_at, updated_at FROM operator_agent_preferences WHERE operator_id = ? AND machine_id IN (` + strings.Join(placeholders, ",") + `) ORDER BY machine_id, sort_order, agent_id`
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.AgentPreference, 0)
	for rows.Next() {
		preference := model.AgentPreference{OperatorID: operatorID}
		if err := rows.Scan(&preference.MachineID, &preference.AgentID, &preference.SortOrder, &preference.CreatedAt, &preference.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, preference)
	}
	return result, rows.Err()
}

func (m *MySQLArchive) ReorderAgentPreferences(operatorID int64, machineID string, agentIDs []string) ([]model.AgentPreference, error) {
	ids := uniqueNonEmptyStrings(agentIDs)
	tx, err := m.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM operator_agent_preferences WHERE operator_id = ? AND machine_id = ?`, operatorID, machineID); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		placeholders := make([]string, 0, len(ids))
		args := make([]any, 0, len(ids)*4)
		for index, agentID := range ids {
			placeholders = append(placeholders, "(?, ?, ?, ?)")
			args = append(args, operatorID, machineID, agentID, int64(index+1))
		}
		query := `INSERT INTO operator_agent_preferences (operator_id, machine_id, agent_id, sort_order) VALUES ` + strings.Join(placeholders, ",")
		if _, err = tx.Exec(query, args...); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	now := time.Now()
	result := make([]model.AgentPreference, 0, len(ids))
	for index, agentID := range ids {
		result = append(result, model.AgentPreference{
			OperatorID: operatorID,
			MachineID:  machineID,
			AgentID:    agentID,
			SortOrder:  int64(index + 1),
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	return result, nil
}

func (m *MySQLArchive) DeleteDeviceSettings(operatorID int64, agentID string) error {
	_, err := m.db.Exec("DELETE FROM device_settings WHERE operator_id = ? AND agent_id = ?", operatorID, agentID)
	return err
}

func (m *MySQLArchive) ListDeviceSettingAgentIDs(operatorID int64, key string) ([]string, error) {
	rows, err := m.db.Query(
		"SELECT DISTINCT agent_id FROM device_settings WHERE operator_id = ? AND setting_key = ? AND LOWER(TRIM(setting_value)) = 'true'",
		operatorID,
		key,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var agentID string
		if err := rows.Scan(&agentID); err != nil {
			return nil, err
		}
		if agentID != "" {
			result = append(result, agentID)
		}
	}
	return result, rows.Err()
}

func (m *MySQLArchive) UpsertPushDevice(device model.PushDevice) (*model.PushDevice, error) {
	now := time.Now().UTC()
	if device.CreatedAt.IsZero() {
		device.CreatedAt = now
	}
	device.UpdatedAt = now
	device.LastSeenAt = now
	device.Enabled = true
	_, err := m.db.Exec(`
INSERT INTO push_devices (
  operator_id, platform, vendor, registration_id, device_id, device_brand, device_model,
  app_version, enabled, last_seen_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  registration_id = VALUES(registration_id),
  platform = VALUES(platform),
  vendor = VALUES(vendor),
  device_brand = VALUES(device_brand),
  device_model = VALUES(device_model),
  app_version = VALUES(app_version),
  enabled = VALUES(enabled),
  last_seen_at = VALUES(last_seen_at),
  updated_at = VALUES(updated_at)
`,
		device.OperatorID,
		device.Platform,
		device.Vendor,
		device.RegistrationID,
		device.DeviceID,
		device.DeviceBrand,
		device.DeviceModel,
		device.AppVersion,
		device.Enabled,
		device.LastSeenAt,
		device.CreatedAt,
		device.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	row := m.db.QueryRow(`
SELECT id, operator_id, platform, vendor, registration_id, device_id, device_brand, device_model,
       app_version, enabled, last_seen_at, created_at, updated_at
FROM push_devices
WHERE operator_id = ? AND ((device_id <> '' AND device_id = ?) OR registration_id = ?)
ORDER BY updated_at DESC, id DESC
LIMIT 1
`, device.OperatorID, device.DeviceID, device.RegistrationID)
	var stored model.PushDevice
	if err := row.Scan(
		&stored.ID,
		&stored.OperatorID,
		&stored.Platform,
		&stored.Vendor,
		&stored.RegistrationID,
		&stored.DeviceID,
		&stored.DeviceBrand,
		&stored.DeviceModel,
		&stored.AppVersion,
		&stored.Enabled,
		&stored.LastSeenAt,
		&stored.CreatedAt,
		&stored.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &stored, nil
}

func (m *MySQLArchive) ListPushDevices(operatorID int64) ([]*model.PushDevice, error) {
	rows, err := m.db.Query(`
SELECT id, operator_id, platform, vendor, registration_id, device_id, device_brand, device_model,
       app_version, enabled, last_seen_at, created_at, updated_at
FROM push_devices
WHERE operator_id = ?
ORDER BY updated_at DESC, id DESC
`, operatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.PushDevice{}
	for rows.Next() {
		var device model.PushDevice
		if err := rows.Scan(
			&device.ID,
			&device.OperatorID,
			&device.Platform,
			&device.Vendor,
			&device.RegistrationID,
			&device.DeviceID,
			&device.DeviceBrand,
			&device.DeviceModel,
			&device.AppVersion,
			&device.Enabled,
			&device.LastSeenAt,
			&device.CreatedAt,
			&device.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, &device)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) ListSkills(includeDisabled bool) ([]model.Skill, error) {
	query := `
SELECT id, name, description, content, package_files_json, category, source, enabled, tags_json, sort_order, built_in, created_at, updated_at
FROM skills`
	if !includeDisabled {
		query += " WHERE enabled = 1"
	}
	query += " ORDER BY sort_order ASC, updated_at DESC, id ASC"
	rows, err := m.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Skill
	for rows.Next() {
		item, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) UpsertSkill(item model.Skill) (*model.Skill, error) {
	item = normalizeSkillForStore(item)
	tagsJSON, err := json.Marshal(item.Tags)
	if err != nil {
		return nil, err
	}
	packageFilesJSON, err := json.Marshal(item.PackageFiles)
	if err != nil {
		return nil, err
	}
	_, err = m.db.Exec(`
	INSERT INTO skills (
	  id, name, description, content, package_files_json, category, source, enabled, tags_json, sort_order, built_in
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
	  name = VALUES(name),
	  description = VALUES(description),
	  content = VALUES(content),
	  package_files_json = VALUES(package_files_json),
	  category = VALUES(category),
	  source = VALUES(source),
	  enabled = VALUES(enabled),
	  tags_json = VALUES(tags_json),
	  sort_order = VALUES(sort_order),
	  built_in = VALUES(built_in)
	`, item.ID, item.Name, item.Description, item.Content, string(packageFilesJSON), item.Category, item.Source, item.Enabled, string(tagsJSON), item.SortOrder, item.BuiltIn)
	if err != nil {
		return nil, err
	}
	return m.getSkill(item.ID)
}

func (m *MySQLArchive) DeleteSkill(id string) error {
	id = strings.TrimSpace(id)
	result, err := m.db.Exec("DELETE FROM skills WHERE id = ?", id)
	if err != nil {
		return err
	}
	if _, err := m.db.Exec("DELETE FROM semantic_agent_skills WHERE skill_id = ?", id); err != nil {
		return err
	}
	if err := m.removeSkillFromSemanticAgentSnapshots(id); err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *MySQLArchive) removeSkillFromSemanticAgentSnapshots(skillID string) error {
	skillID = strings.TrimSpace(skillID)
	if skillID == "" {
		return nil
	}
	items, err := m.ListSemanticAgents(true)
	if err != nil {
		return err
	}
	for _, item := range items {
		nextSkillIDs := cleanStringList(removeString(item.SkillIDs, skillID))
		nextSkills := cleanStringList(removeString(item.Skills, skillID))
		nextSkillDefinitions := removeSemanticAgentSkillDefinition(item.SkillDefinitions, skillID)
		if sameStringList(nextSkillIDs, item.SkillIDs) &&
			sameStringList(nextSkills, item.Skills) &&
			sameSemanticAgentSkillDefinitions(nextSkillDefinitions, item.SkillDefinitions) {
			continue
		}
		skillIDsJSON, err := json.Marshal(nextSkillIDs)
		if err != nil {
			return err
		}
		skillsJSON, err := json.Marshal(nextSkills)
		if err != nil {
			return err
		}
		skillDefinitionsJSON, err := json.Marshal(nextSkillDefinitions)
		if err != nil {
			return err
		}
		if _, err := m.db.Exec(`
UPDATE semantic_agents
SET skill_ids_json = ?, skills_json = ?, skill_definitions_json = ?
WHERE id = ?
`, string(skillIDsJSON), string(skillsJSON), string(skillDefinitionsJSON), item.ID); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLArchive) removeMCPFromSemanticAgentSnapshots(aliases []string) error {
	aliases = cleanAliasList(aliases)
	if len(aliases) == 0 {
		return nil
	}
	items, err := m.ListSemanticAgents(true)
	if err != nil {
		return err
	}
	for _, item := range items {
		nextMCPIDs := cleanStringList(removeStrings(item.MCPIDs, aliases))
		nextRecommended := cleanStringList(removeStrings(item.RecommendedMCPServers, aliases))
		if sameStringList(nextMCPIDs, item.MCPIDs) && sameStringList(nextRecommended, item.RecommendedMCPServers) {
			continue
		}
		mcpIDsJSON, err := json.Marshal(nextMCPIDs)
		if err != nil {
			return err
		}
		recommendedJSON, err := json.Marshal(nextRecommended)
		if err != nil {
			return err
		}
		if _, err := m.db.Exec(`
UPDATE semantic_agents
SET mcp_ids_json = ?, recommended_mcp_servers_json = ?
WHERE id = ?
`, string(mcpIDsJSON), string(recommendedJSON), item.ID); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLArchive) removeRuntimeFromSemanticAgentSnapshots(runtimeID string) error {
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID == "" {
		return nil
	}
	items, err := m.ListSemanticAgents(true)
	if err != nil {
		return err
	}
	for _, item := range items {
		nextRuntimes := removeRuntimeRequirement(item.RuntimeRequirements, runtimeID)
		if sameSemanticAgentRuntimes(nextRuntimes, item.RuntimeRequirements) {
			continue
		}
		if err := m.replaceSemanticAgentRuntimeBindings(item.ID, nextRuntimes); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLArchive) removeToolPermissionFromSemanticAgentSnapshots(tool model.ToolCatalogItem) error {
	items, err := m.ListSemanticAgents(true)
	if err != nil {
		return err
	}
	for _, item := range items {
		nextPermissions, changed := removeToolPermission(item.ToolPermissions, tool)
		if !changed || sameAnyMap(nextPermissions, item.ToolPermissions) {
			continue
		}
		permissionsJSON, err := json.Marshal(nextPermissions)
		if err != nil {
			return err
		}
		if _, err := m.db.Exec(`
UPDATE semantic_agents
SET tool_permissions_json = ?
WHERE id = ?
`, string(permissionsJSON), item.ID); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLArchive) hasEquivalentToolPermission(tool model.ToolCatalogItem) (bool, error) {
	items, err := m.ListToolCatalog(true)
	if err != nil {
		return false, err
	}
	byID := map[string]model.ToolCatalogItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	delete(byID, tool.ID)
	return hasEquivalentToolPermission(byID, tool), nil
}

func (m *MySQLArchive) getSkill(id string) (*model.Skill, error) {
	row := m.db.QueryRow(`
SELECT id, name, description, content, package_files_json, category, source, enabled, tags_json, sort_order, built_in, created_at, updated_at
FROM skills
WHERE id = ?
`, id)
	item, err := scanSkill(row)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

type skillScanner interface {
	Scan(dest ...any) error
}

func scanSkill(scanner skillScanner) (model.Skill, error) {
	var item model.Skill
	var tagsJSON []byte
	var packageFilesJSON []byte
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.Description,
		&item.Content,
		&packageFilesJSON,
		&item.Category,
		&item.Source,
		&item.Enabled,
		&tagsJSON,
		&item.SortOrder,
		&item.BuiltIn,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.Skill{}, err
	}
	if len(tagsJSON) > 0 {
		if err := json.Unmarshal(tagsJSON, &item.Tags); err != nil {
			return model.Skill{}, err
		}
	}
	if len(packageFilesJSON) > 0 {
		if err := json.Unmarshal(packageFilesJSON, &item.PackageFiles); err != nil {
			return model.Skill{}, err
		}
	}
	item.PackageFiles = cleanSkillPackageFiles(item.PackageFiles)
	item.Description = normalizeAssetDescription(item.Description, item.Content)
	return item, nil
}

func normalizeSkillForStore(item model.Skill) model.Skill {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = firstText(strings.TrimSpace(item.Name), item.ID)
	item.Content = strings.TrimSpace(item.Content)
	item.PackageFiles = cleanSkillPackageFiles(item.PackageFiles)
	item.Description = normalizeAssetDescription(item.Description, item.Content)
	item.Category = firstText(strings.TrimSpace(item.Category), "通用")
	item.Source = strings.TrimSpace(item.Source)
	item.Tags = cleanStringList(item.Tags)
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return item
}

func (m *MySQLArchive) ListMCPCatalog(includeDisabled bool) ([]model.MCPCatalogItem, error) {
	query := `
SELECT id, name, title, description, type, source, source_url, credential_url, category, recommended, enabled, tags_json,
       sort_order, built_in, config_json, install_json, launch_ready, launch_block_reason, created_at, updated_at
FROM mcp_catalog`
	if !includeDisabled {
		query += " WHERE enabled = 1"
	}
	query += " ORDER BY recommended DESC, sort_order ASC, updated_at DESC, id ASC"
	rows, err := m.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.MCPCatalogItem
	for rows.Next() {
		item, err := scanMCPCatalogItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) UpsertMCPCatalogItem(item model.MCPCatalogItem) (*model.MCPCatalogItem, error) {
	item = normalizeMCPCatalogItemForStore(item)
	tagsJSON, err := json.Marshal(item.Tags)
	if err != nil {
		return nil, err
	}
	configJSON, err := json.Marshal(item.Config)
	if err != nil {
		return nil, err
	}
	installJSON, err := json.Marshal(item.Install)
	if err != nil {
		return nil, err
	}
	_, err = m.db.Exec(`
INSERT INTO mcp_catalog (
  id, name, title, description, type, source, source_url, credential_url, category, recommended, enabled,
  tags_json, sort_order, built_in, config_json, install_json, launch_ready, launch_block_reason
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  name = VALUES(name),
  title = VALUES(title),
  description = VALUES(description),
  type = VALUES(type),
  source = VALUES(source),
  source_url = VALUES(source_url),
  credential_url = VALUES(credential_url),
  category = VALUES(category),
  recommended = VALUES(recommended),
  enabled = VALUES(enabled),
  tags_json = VALUES(tags_json),
  sort_order = VALUES(sort_order),
  built_in = VALUES(built_in),
  config_json = VALUES(config_json),
  install_json = VALUES(install_json),
  launch_ready = VALUES(launch_ready),
  launch_block_reason = VALUES(launch_block_reason)
`, item.ID, item.Name, item.Title, item.Description, item.Type, item.Source, item.SourceURL, item.CredentialURL,
		item.Category, item.Recommended, item.Enabled, string(tagsJSON), item.SortOrder, item.BuiltIn, string(configJSON), string(installJSON),
		item.LaunchReady, item.LaunchBlockReason)
	if err != nil {
		return nil, err
	}
	return m.getMCPCatalogItem(item.ID)
}

func (m *MySQLArchive) DeleteMCPCatalogItem(id string) error {
	id = strings.TrimSpace(id)
	var aliases []string
	if item, err := m.getMCPCatalogItem(id); err == nil {
		aliases = mcpCatalogAliases(*item)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if len(aliases) == 0 {
		aliases = []string{id}
	}
	result, err := m.db.Exec("DELETE FROM mcp_catalog WHERE id = ?", id)
	if err != nil {
		return err
	}
	if _, err := m.db.Exec("DELETE FROM semantic_agent_mcps WHERE mcp_id = ?", id); err != nil {
		return err
	}
	if err := m.removeMCPFromSemanticAgentSnapshots(aliases); err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *MySQLArchive) getMCPCatalogItem(id string) (*model.MCPCatalogItem, error) {
	row := m.db.QueryRow(`
SELECT id, name, title, description, type, source, source_url, credential_url, category, recommended, enabled, tags_json,
       sort_order, built_in, config_json, install_json, launch_ready, launch_block_reason, created_at, updated_at
FROM mcp_catalog
WHERE id = ?
`, id)
	item, err := scanMCPCatalogItem(row)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

type mcpCatalogItemScanner interface {
	Scan(dest ...any) error
}

func scanMCPCatalogItem(scanner mcpCatalogItemScanner) (model.MCPCatalogItem, error) {
	var item model.MCPCatalogItem
	var tagsJSON []byte
	var configJSON []byte
	var installJSON []byte
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.Title,
		&item.Description,
		&item.Type,
		&item.Source,
		&item.SourceURL,
		&item.CredentialURL,
		&item.Category,
		&item.Recommended,
		&item.Enabled,
		&tagsJSON,
		&item.SortOrder,
		&item.BuiltIn,
		&configJSON,
		&installJSON,
		&item.LaunchReady,
		&item.LaunchBlockReason,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.MCPCatalogItem{}, err
	}
	if len(tagsJSON) > 0 {
		if err := json.Unmarshal(tagsJSON, &item.Tags); err != nil {
			return model.MCPCatalogItem{}, err
		}
	}
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &item.Config); err != nil {
			return model.MCPCatalogItem{}, err
		}
	}
	if len(installJSON) > 0 {
		if err := json.Unmarshal(installJSON, &item.Install); err != nil {
			return model.MCPCatalogItem{}, err
		}
	}
	item.Description = normalizeAssetDescription(item.Description, "")
	item.Install = normalizeMCPInstallInfo(item.Install)
	return item, nil
}

func normalizeMCPCatalogItemForStore(item model.MCPCatalogItem) model.MCPCatalogItem {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = strings.TrimSpace(item.Name)
	item.Title = firstText(strings.TrimSpace(item.Title), item.Name)
	item.Description = normalizeAssetDescription(item.Description, "")
	item.Type = firstText(strings.TrimSpace(item.Type), strings.TrimSpace(item.Config.Type))
	if item.Type == "" {
		item.Type = "local"
	}
	item.Source = strings.TrimSpace(item.Source)
	item.SourceURL = firstText(strings.TrimSpace(item.SourceURL), strings.TrimSpace(item.Install.SourceURL))
	item.CredentialURL = strings.TrimSpace(item.CredentialURL)
	item.Category = firstText(strings.TrimSpace(item.Category), "通用")
	item.Tags = cleanStringList(item.Tags)
	item.Install = normalizeMCPInstallInfo(item.Install)
	if item.Install.SourceURL == "" {
		item.Install.SourceURL = item.SourceURL
	}
	item.Config.Name = item.Name
	item.Config.Type = item.Type
	item.Config.Enabled = item.Enabled
	item.LaunchReady, item.LaunchBlockReason = evaluateMCPLaunchReadiness(item)
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return item
}

func normalizeMCPInstallInfo(info model.MCPInstallInfo) model.MCPInstallInfo {
	info.PackageManager = strings.TrimSpace(info.PackageManager)
	info.SourceURL = strings.TrimSpace(info.SourceURL)
	info.Assets = cleanMCPInstallAssetsForStore(info.Assets)
	info.Files = cleanMCPInstallFilesForStore(info.Files)
	info.InstallCommands = cleanStringList(info.InstallCommands)
	info.BuildCommands = cleanStringList(info.BuildCommands)
	info.RunCommandTemplate = cleanStringList(info.RunCommandTemplate)
	info.ConfigPathTemplate = strings.TrimSpace(info.ConfigPathTemplate)
	info.ExecutableNames = cleanStringList(info.ExecutableNames)
	info.Environment = cleanStringMap(info.Environment)
	info.InstallNotes = strings.TrimSpace(info.InstallNotes)
	return info
}

func cleanMCPInstallAssetsForStore(input []model.MCPInstallAsset) []model.MCPInstallAsset {
	out := make([]model.MCPInstallAsset, 0, len(input))
	for _, item := range input {
		item.Name = strings.TrimSpace(item.Name)
		item.URL = strings.TrimSpace(item.URL)
		item.Filename = strings.TrimSpace(item.Filename)
		item.SHA256 = strings.TrimSpace(item.SHA256)
		item.PackageKind = strings.TrimSpace(item.PackageKind)
		item.TargetDir = strings.TrimSpace(item.TargetDir)
		item.Headers = cleanStringMap(item.Headers)
		if item.URL == "" && item.Filename == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func cleanMCPInstallFilesForStore(input []model.MCPInstallFile) []model.MCPInstallFile {
	out := make([]model.MCPInstallFile, 0, len(input))
	for _, item := range input {
		item.Path = strings.TrimSpace(item.Path)
		if item.Path == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func evaluateMCPLaunchReadiness(item model.MCPCatalogItem) (bool, string) {
	server := item.Config
	server.Type = firstText(strings.TrimSpace(server.Type), strings.TrimSpace(item.Type))
	if server.Type == "" {
		server.Type = "local"
	}
	if server.Type == "remote" {
		if strings.TrimSpace(server.URL) == "" {
			return false, "远程 MCP 缺少 URL"
		}
		if containsMCPPlaceholder(server.URL) {
			return false, "远程 MCP URL 仍包含占位符"
		}
		return true, ""
	}
	if len(cleanStringList(server.Command)) == 0 {
		if len(cleanStringList(item.Install.RunCommandTemplate)) > 0 {
			return false, "缺少可直接启动命令，需要先根据安装模板解析真实路径"
		}
		return false, "本地 MCP 缺少启动命令"
	}
	if containsMCPPlaceholders(append(append([]string{}, server.Command...), mapValues(server.Environment)...)) {
		return false, "启动命令或环境变量仍包含占位路径，需要安装后解析真实路径"
	}
	return true, ""
}

func containsMCPPlaceholders(items []string) bool {
	for _, item := range items {
		if containsMCPPlaceholder(item) {
			return true
		}
	}
	return false
}

func containsMCPPlaceholder(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	upper := strings.ToUpper(value)
	return strings.Contains(upper, "/ABSOLUTE/PATH") ||
		strings.Contains(upper, "\\ABSOLUTE\\PATH") ||
		strings.Contains(upper, "ABSOLUTE/PATH") ||
		strings.Contains(upper, "ABSOLUTE\\PATH") ||
		strings.Contains(value, "${MCP_HOME}") ||
		strings.Contains(value, "${MCP_HOME:")
}

func mapValues(input map[string]string) []string {
	out := make([]string, 0, len(input))
	for _, value := range input {
		out = append(out, value)
	}
	return out
}

func (m *MySQLArchive) ListEnvironmentPresets(includeDisabled bool) ([]model.EnvironmentPreset, error) {
	query := `
SELECT id, name, description, category, source, enabled, tags_json, variables_json,
       sort_order, built_in, created_at, updated_at
FROM environment_presets`
	if !includeDisabled {
		query += " WHERE enabled = 1"
	}
	query += " ORDER BY sort_order ASC, updated_at DESC, id ASC"
	rows, err := m.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.EnvironmentPreset
	for rows.Next() {
		item, err := scanEnvironmentPreset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) UpsertEnvironmentPreset(item model.EnvironmentPreset) (*model.EnvironmentPreset, error) {
	item = normalizeEnvironmentPresetForStore(item)
	tagsJSON, err := json.Marshal(item.Tags)
	if err != nil {
		return nil, err
	}
	variablesJSON, err := json.Marshal(item.Variables)
	if err != nil {
		return nil, err
	}
	_, err = m.db.Exec(`
INSERT INTO environment_presets (
  id, name, description, category, source, enabled, tags_json, variables_json,
  sort_order, built_in
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  name = VALUES(name),
  description = VALUES(description),
  category = VALUES(category),
  source = VALUES(source),
  enabled = VALUES(enabled),
  tags_json = VALUES(tags_json),
  variables_json = VALUES(variables_json),
  sort_order = VALUES(sort_order),
  built_in = VALUES(built_in)
`, item.ID, item.Name, item.Description, item.Category, item.Source, item.Enabled,
		string(tagsJSON), string(variablesJSON), item.SortOrder, item.BuiltIn)
	if err != nil {
		return nil, err
	}
	return m.getEnvironmentPreset(item.ID)
}

func (m *MySQLArchive) DeleteEnvironmentPreset(id string) error {
	id = strings.TrimSpace(id)
	result, err := m.db.Exec("DELETE FROM environment_presets WHERE id = ?", id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *MySQLArchive) getEnvironmentPreset(id string) (*model.EnvironmentPreset, error) {
	row := m.db.QueryRow(`
SELECT id, name, description, category, source, enabled, tags_json, variables_json,
       sort_order, built_in, created_at, updated_at
FROM environment_presets
WHERE id = ?
`, id)
	item, err := scanEnvironmentPreset(row)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

type environmentPresetScanner interface {
	Scan(dest ...any) error
}

func scanEnvironmentPreset(scanner environmentPresetScanner) (model.EnvironmentPreset, error) {
	var item model.EnvironmentPreset
	var tagsJSON []byte
	var variablesJSON []byte
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.Description,
		&item.Category,
		&item.Source,
		&item.Enabled,
		&tagsJSON,
		&variablesJSON,
		&item.SortOrder,
		&item.BuiltIn,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.EnvironmentPreset{}, err
	}
	if len(tagsJSON) > 0 {
		if err := json.Unmarshal(tagsJSON, &item.Tags); err != nil {
			return model.EnvironmentPreset{}, err
		}
	}
	if len(variablesJSON) > 0 {
		if err := json.Unmarshal(variablesJSON, &item.Variables); err != nil {
			return model.EnvironmentPreset{}, err
		}
	}
	return item, nil
}

func normalizeEnvironmentPresetForStore(item model.EnvironmentPreset) model.EnvironmentPreset {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = firstText(strings.TrimSpace(item.Name), item.ID)
	item.Description = strings.TrimSpace(item.Description)
	item.Category = firstText(strings.TrimSpace(item.Category), "通用")
	item.Source = strings.TrimSpace(item.Source)
	item.Tags = cleanStringList(item.Tags)
	item.Variables = cleanStringMap(item.Variables)
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return item
}

func (m *MySQLArchive) ListRuntimeCatalog(includeDisabled bool) ([]model.RuntimeCatalogItem, error) {
	query := `
SELECT id, name, description, runtime_kind, default_version_constraint,
       executable_names_json, env_template_json, install_strategy, enabled, tags_json,
       sort_order, built_in, created_at, updated_at
FROM runtime_catalog`
	if !includeDisabled {
		query += " WHERE enabled = 1"
	}
	query += " ORDER BY sort_order ASC, updated_at DESC, id ASC"
	rows, err := m.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RuntimeCatalogItem
	for rows.Next() {
		item, err := scanRuntimeCatalogItem(rows)
		if err != nil {
			return nil, err
		}
		if err := m.hydrateRuntimeCatalogItem(&item, includeDisabled); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) UpsertRuntimeCatalogItem(item model.RuntimeCatalogItem) (*model.RuntimeCatalogItem, error) {
	item = normalizeRuntimeCatalogItemForStore(item)
	executableJSON, err := json.Marshal(item.ExecutableNames)
	if err != nil {
		return nil, err
	}
	envJSON, err := json.Marshal(item.EnvTemplate)
	if err != nil {
		return nil, err
	}
	tagsJSON, err := json.Marshal(item.Tags)
	if err != nil {
		return nil, err
	}
	_, err = m.db.Exec(`
INSERT INTO runtime_catalog (
  id, name, description, runtime_kind, default_version_constraint,
  executable_names_json, env_template_json, install_strategy, enabled, tags_json,
  sort_order, built_in
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  name = VALUES(name),
  description = VALUES(description),
  runtime_kind = VALUES(runtime_kind),
  default_version_constraint = VALUES(default_version_constraint),
  executable_names_json = VALUES(executable_names_json),
  env_template_json = VALUES(env_template_json),
  install_strategy = VALUES(install_strategy),
  enabled = VALUES(enabled),
  tags_json = VALUES(tags_json),
  sort_order = VALUES(sort_order),
  built_in = VALUES(built_in)
`, item.ID, item.Name, item.Description, item.RuntimeKind, item.DefaultVersionConstraint,
		string(executableJSON), string(envJSON), item.InstallStrategy, item.Enabled, string(tagsJSON),
		item.SortOrder, item.BuiltIn)
	if err != nil {
		return nil, err
	}
	return m.getRuntimeCatalogItem(item.ID)
}

func (m *MySQLArchive) DeleteRuntimeCatalogItem(id string) error {
	id = strings.TrimSpace(id)
	result, err := m.db.Exec("DELETE FROM runtime_catalog WHERE id = ?", id)
	if err != nil {
		return err
	}
	if _, err := m.db.Exec("DELETE FROM runtime_versions WHERE runtime_id = ?", id); err != nil {
		return err
	}
	if _, err := m.db.Exec("DELETE FROM runtime_artifacts WHERE runtime_id = ?", id); err != nil {
		return err
	}
	if _, err := m.db.Exec("DELETE FROM runtime_mirrors WHERE runtime_id = ?", id); err != nil {
		return err
	}
	if _, err := m.db.Exec("DELETE FROM semantic_agent_runtimes WHERE runtime_id = ?", id); err != nil {
		return err
	}
	if err := m.removeRuntimeFromSemanticAgentSnapshots(id); err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *MySQLArchive) getRuntimeCatalogItem(id string) (*model.RuntimeCatalogItem, error) {
	row := m.db.QueryRow(`
SELECT id, name, description, runtime_kind, default_version_constraint,
       executable_names_json, env_template_json, install_strategy, enabled, tags_json,
       sort_order, built_in, created_at, updated_at
FROM runtime_catalog
WHERE id = ?
`, id)
	item, err := scanRuntimeCatalogItem(row)
	if err != nil {
		return nil, err
	}
	if err := m.hydrateRuntimeCatalogItem(&item, true); err != nil {
		return nil, err
	}
	return &item, nil
}

type runtimeCatalogScanner interface {
	Scan(dest ...any) error
}

func scanRuntimeCatalogItem(scanner runtimeCatalogScanner) (model.RuntimeCatalogItem, error) {
	var item model.RuntimeCatalogItem
	var executableJSON []byte
	var envJSON []byte
	var tagsJSON []byte
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.Description,
		&item.RuntimeKind,
		&item.DefaultVersionConstraint,
		&executableJSON,
		&envJSON,
		&item.InstallStrategy,
		&item.Enabled,
		&tagsJSON,
		&item.SortOrder,
		&item.BuiltIn,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.RuntimeCatalogItem{}, err
	}
	if len(executableJSON) > 0 {
		if err := json.Unmarshal(executableJSON, &item.ExecutableNames); err != nil {
			return model.RuntimeCatalogItem{}, err
		}
	}
	if len(envJSON) > 0 {
		if err := json.Unmarshal(envJSON, &item.EnvTemplate); err != nil {
			return model.RuntimeCatalogItem{}, err
		}
	}
	if len(tagsJSON) > 0 {
		if err := json.Unmarshal(tagsJSON, &item.Tags); err != nil {
			return model.RuntimeCatalogItem{}, err
		}
	}
	return item, nil
}

func normalizeRuntimeCatalogItemForStore(item model.RuntimeCatalogItem) model.RuntimeCatalogItem {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = firstText(strings.TrimSpace(item.Name), item.ID)
	item.Description = strings.TrimSpace(item.Description)
	item.RuntimeKind = firstText(strings.TrimSpace(item.RuntimeKind), "language")
	item.DefaultVersionConstraint = strings.TrimSpace(item.DefaultVersionConstraint)
	item.ExecutableNames = cleanStringList(item.ExecutableNames)
	item.EnvTemplate = cleanStringMap(item.EnvTemplate)
	item.InstallStrategy = firstText(strings.TrimSpace(item.InstallStrategy), "archive")
	item.Tags = cleanStringList(item.Tags)
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return item
}

func (m *MySQLArchive) hydrateRuntimeCatalogItem(item *model.RuntimeCatalogItem, includeDisabled bool) error {
	if item == nil || strings.TrimSpace(item.ID) == "" {
		return nil
	}
	versions, err := m.listRuntimeVersionsByRuntime(item.ID, includeDisabled)
	if err != nil {
		return err
	}
	artifacts, err := m.listRuntimeArtifactsByRuntime(item.ID, includeDisabled)
	if err != nil {
		return err
	}
	mirrors, err := m.listRuntimeMirrorsByRuntime(item.ID, includeDisabled)
	if err != nil {
		return err
	}
	item.Versions = versions
	item.Artifacts = artifacts
	item.Mirrors = mirrors
	return nil
}

func (m *MySQLArchive) ListRuntimeVersions(includeDisabled bool) ([]model.RuntimeVersion, error) {
	return m.listRuntimeVersionsByRuntime("", includeDisabled)
}

func (m *MySQLArchive) listRuntimeVersionsByRuntime(runtimeID string, includeDisabled bool) ([]model.RuntimeVersion, error) {
	query := `
SELECT id, runtime_id, version, channel, status, version_order, default_selected,
       min_os_version_json, install_manifest_json, enabled, sort_order, created_at, updated_at
FROM runtime_versions`
	args := []any{}
	conditions := []string{}
	if strings.TrimSpace(runtimeID) != "" {
		conditions = append(conditions, "runtime_id = ?")
		args = append(args, strings.TrimSpace(runtimeID))
	}
	if !includeDisabled {
		conditions = append(conditions, "enabled = 1")
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY runtime_id ASC, default_selected DESC, version_order ASC, sort_order ASC, id ASC"
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RuntimeVersion
	for rows.Next() {
		item, err := scanRuntimeVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) UpsertRuntimeVersion(item model.RuntimeVersion) (*model.RuntimeVersion, error) {
	item = normalizeRuntimeVersionForStore(item)
	minOSJSON, err := json.Marshal(item.MinOSVersion)
	if err != nil {
		return nil, err
	}
	manifestJSON, err := json.Marshal(item.InstallManifest)
	if err != nil {
		return nil, err
	}
	_, err = m.db.Exec(`
INSERT INTO runtime_versions (
  id, runtime_id, version, channel, status, version_order, default_selected,
  min_os_version_json, install_manifest_json, enabled, sort_order
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  runtime_id = VALUES(runtime_id),
  version = VALUES(version),
  channel = VALUES(channel),
  status = VALUES(status),
  version_order = VALUES(version_order),
  default_selected = VALUES(default_selected),
  min_os_version_json = VALUES(min_os_version_json),
  install_manifest_json = VALUES(install_manifest_json),
  enabled = VALUES(enabled),
  sort_order = VALUES(sort_order)
`, item.ID, item.RuntimeID, item.Version, item.Channel, item.Status, item.VersionOrder, item.DefaultSelected,
		string(minOSJSON), string(manifestJSON), item.Enabled, item.SortOrder)
	if err != nil {
		return nil, err
	}
	return m.getRuntimeVersion(item.ID)
}

func (m *MySQLArchive) DeleteRuntimeVersion(id string) error {
	id = strings.TrimSpace(id)
	result, err := m.db.Exec("DELETE FROM runtime_versions WHERE id = ?", id)
	if err != nil {
		return err
	}
	if _, err := m.db.Exec("DELETE FROM runtime_artifacts WHERE version_id = ?", id); err != nil {
		return err
	}
	if _, err := m.db.Exec("DELETE FROM runtime_mirrors WHERE version_id = ?", id); err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *MySQLArchive) getRuntimeVersion(id string) (*model.RuntimeVersion, error) {
	row := m.db.QueryRow(`
SELECT id, runtime_id, version, channel, status, version_order, default_selected,
       min_os_version_json, install_manifest_json, enabled, sort_order, created_at, updated_at
FROM runtime_versions
WHERE id = ?
`, id)
	item, err := scanRuntimeVersion(row)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

type runtimeVersionScanner interface {
	Scan(dest ...any) error
}

func scanRuntimeVersion(scanner runtimeVersionScanner) (model.RuntimeVersion, error) {
	var item model.RuntimeVersion
	var minOSJSON []byte
	var manifestJSON []byte
	if err := scanner.Scan(
		&item.ID,
		&item.RuntimeID,
		&item.Version,
		&item.Channel,
		&item.Status,
		&item.VersionOrder,
		&item.DefaultSelected,
		&minOSJSON,
		&manifestJSON,
		&item.Enabled,
		&item.SortOrder,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.RuntimeVersion{}, err
	}
	if len(minOSJSON) > 0 {
		if err := json.Unmarshal(minOSJSON, &item.MinOSVersion); err != nil {
			return model.RuntimeVersion{}, err
		}
	}
	if len(manifestJSON) > 0 {
		if err := json.Unmarshal(manifestJSON, &item.InstallManifest); err != nil {
			return model.RuntimeVersion{}, err
		}
	}
	return item, nil
}

func normalizeRuntimeVersionForStore(item model.RuntimeVersion) model.RuntimeVersion {
	item.ID = strings.TrimSpace(item.ID)
	item.RuntimeID = strings.TrimSpace(item.RuntimeID)
	item.Version = strings.TrimSpace(item.Version)
	item.Channel = strings.TrimSpace(item.Channel)
	item.Status = firstText(strings.TrimSpace(item.Status), "available")
	item.MinOSVersion = cleanStringMap(item.MinOSVersion)
	if item.InstallManifest == nil {
		item.InstallManifest = map[string]any{}
	}
	if item.VersionOrder == 0 {
		item.VersionOrder = 100
	}
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return item
}

func (m *MySQLArchive) ListRuntimeArtifacts(includeDisabled bool) ([]model.RuntimeArtifact, error) {
	return m.listRuntimeArtifactsByRuntime("", includeDisabled)
}

func (m *MySQLArchive) listRuntimeArtifactsByRuntime(runtimeID string, includeDisabled bool) ([]model.RuntimeArtifact, error) {
	query := `
SELECT id, runtime_id, version_id, platform, arch, package_kind, filename, size_bytes,
       sha256, storage_key, download_path, extract_root, bin_paths_json, env_patch_json,
       priority, enabled, created_at, updated_at
FROM runtime_artifacts`
	args := []any{}
	conditions := []string{}
	if strings.TrimSpace(runtimeID) != "" {
		conditions = append(conditions, "runtime_id = ?")
		args = append(args, strings.TrimSpace(runtimeID))
	}
	if !includeDisabled {
		conditions = append(conditions, "enabled = 1")
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY runtime_id ASC, version_id ASC, priority ASC, platform ASC, arch ASC, id ASC"
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RuntimeArtifact
	for rows.Next() {
		item, err := scanRuntimeArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) UpsertRuntimeArtifact(item model.RuntimeArtifact) (*model.RuntimeArtifact, error) {
	item = normalizeRuntimeArtifactForStore(item)
	binPathsJSON, err := json.Marshal(item.BinPaths)
	if err != nil {
		return nil, err
	}
	envPatchJSON, err := json.Marshal(item.EnvPatch)
	if err != nil {
		return nil, err
	}
	_, err = m.db.Exec(`
INSERT INTO runtime_artifacts (
  id, runtime_id, version_id, platform, arch, package_kind, filename, size_bytes,
  sha256, storage_key, download_path, extract_root, bin_paths_json, env_patch_json,
  priority, enabled
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  runtime_id = VALUES(runtime_id),
  version_id = VALUES(version_id),
  platform = VALUES(platform),
  arch = VALUES(arch),
  package_kind = VALUES(package_kind),
  filename = VALUES(filename),
  size_bytes = VALUES(size_bytes),
  sha256 = VALUES(sha256),
  storage_key = VALUES(storage_key),
  download_path = VALUES(download_path),
  extract_root = VALUES(extract_root),
  bin_paths_json = VALUES(bin_paths_json),
  env_patch_json = VALUES(env_patch_json),
  priority = VALUES(priority),
  enabled = VALUES(enabled)
`, item.ID, item.RuntimeID, item.VersionID, item.Platform, item.Arch, item.PackageKind,
		item.Filename, item.SizeBytes, item.SHA256, item.StorageKey, item.DownloadPath,
		item.ExtractRoot, string(binPathsJSON), string(envPatchJSON), item.Priority, item.Enabled)
	if err != nil {
		return nil, err
	}
	return m.getRuntimeArtifact(item.ID)
}

func (m *MySQLArchive) DeleteRuntimeArtifact(id string) error {
	id = strings.TrimSpace(id)
	result, err := m.db.Exec("DELETE FROM runtime_artifacts WHERE id = ?", id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *MySQLArchive) getRuntimeArtifact(id string) (*model.RuntimeArtifact, error) {
	row := m.db.QueryRow(`
SELECT id, runtime_id, version_id, platform, arch, package_kind, filename, size_bytes,
       sha256, storage_key, download_path, extract_root, bin_paths_json, env_patch_json,
       priority, enabled, created_at, updated_at
FROM runtime_artifacts
WHERE id = ?
`, id)
	item, err := scanRuntimeArtifact(row)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

type runtimeArtifactScanner interface {
	Scan(dest ...any) error
}

func scanRuntimeArtifact(scanner runtimeArtifactScanner) (model.RuntimeArtifact, error) {
	var item model.RuntimeArtifact
	var binPathsJSON []byte
	var envPatchJSON []byte
	if err := scanner.Scan(
		&item.ID,
		&item.RuntimeID,
		&item.VersionID,
		&item.Platform,
		&item.Arch,
		&item.PackageKind,
		&item.Filename,
		&item.SizeBytes,
		&item.SHA256,
		&item.StorageKey,
		&item.DownloadPath,
		&item.ExtractRoot,
		&binPathsJSON,
		&envPatchJSON,
		&item.Priority,
		&item.Enabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.RuntimeArtifact{}, err
	}
	if len(binPathsJSON) > 0 {
		if err := json.Unmarshal(binPathsJSON, &item.BinPaths); err != nil {
			return model.RuntimeArtifact{}, err
		}
	}
	if len(envPatchJSON) > 0 {
		if err := json.Unmarshal(envPatchJSON, &item.EnvPatch); err != nil {
			return model.RuntimeArtifact{}, err
		}
	}
	return item, nil
}

func normalizeRuntimeArtifactForStore(item model.RuntimeArtifact) model.RuntimeArtifact {
	item.ID = strings.TrimSpace(item.ID)
	item.RuntimeID = strings.TrimSpace(item.RuntimeID)
	item.VersionID = strings.TrimSpace(item.VersionID)
	item.Platform = strings.ToLower(strings.TrimSpace(item.Platform))
	item.Arch = strings.ToLower(strings.TrimSpace(item.Arch))
	item.PackageKind = firstText(strings.TrimSpace(item.PackageKind), "archive")
	item.Filename = strings.TrimSpace(item.Filename)
	item.SHA256 = strings.ToLower(strings.TrimSpace(item.SHA256))
	item.StorageKey = strings.TrimSpace(item.StorageKey)
	item.DownloadPath = strings.TrimSpace(item.DownloadPath)
	item.ExtractRoot = strings.TrimSpace(item.ExtractRoot)
	item.BinPaths = cleanStringList(item.BinPaths)
	item.EnvPatch = cleanStringMap(item.EnvPatch)
	if item.Priority == 0 {
		item.Priority = 100
	}
	return item
}

func (m *MySQLArchive) ListRuntimeMirrors(includeDisabled bool) ([]model.RuntimeMirror, error) {
	return m.listRuntimeMirrorsByRuntime("", includeDisabled)
}

func (m *MySQLArchive) listRuntimeMirrorsByRuntime(runtimeID string, includeDisabled bool) ([]model.RuntimeMirror, error) {
	query := `
SELECT id, runtime_id, version_id, name, description, base_url, url_template,
       checksum_url_template, platform, arch, priority, timeout_seconds, enabled,
       headers_json, tags_json, created_at, updated_at
FROM runtime_mirrors`
	args := []any{}
	conditions := []string{}
	if strings.TrimSpace(runtimeID) != "" {
		conditions = append(conditions, "(runtime_id = ? OR runtime_id = '')")
		args = append(args, strings.TrimSpace(runtimeID))
	}
	if !includeDisabled {
		conditions = append(conditions, "enabled = 1")
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY runtime_id ASC, version_id ASC, priority ASC, id ASC"
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RuntimeMirror
	for rows.Next() {
		item, err := scanRuntimeMirror(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) UpsertRuntimeMirror(item model.RuntimeMirror) (*model.RuntimeMirror, error) {
	item = normalizeRuntimeMirrorForStore(item)
	headersJSON, err := json.Marshal(item.Headers)
	if err != nil {
		return nil, err
	}
	tagsJSON, err := json.Marshal(item.Tags)
	if err != nil {
		return nil, err
	}
	_, err = m.db.Exec(`
INSERT INTO runtime_mirrors (
  id, runtime_id, version_id, name, description, base_url, url_template,
  checksum_url_template, platform, arch, priority, timeout_seconds, enabled,
  headers_json, tags_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  runtime_id = VALUES(runtime_id),
  version_id = VALUES(version_id),
  name = VALUES(name),
  description = VALUES(description),
  base_url = VALUES(base_url),
  url_template = VALUES(url_template),
  checksum_url_template = VALUES(checksum_url_template),
  platform = VALUES(platform),
  arch = VALUES(arch),
  priority = VALUES(priority),
  timeout_seconds = VALUES(timeout_seconds),
  enabled = VALUES(enabled),
  headers_json = VALUES(headers_json),
  tags_json = VALUES(tags_json)
`, item.ID, item.RuntimeID, item.VersionID, item.Name, item.Description, item.BaseURL, item.URLTemplate,
		item.ChecksumURLTemplate, item.Platform, item.Arch, item.Priority, item.TimeoutSeconds,
		item.Enabled, string(headersJSON), string(tagsJSON))
	if err != nil {
		return nil, err
	}
	return m.getRuntimeMirror(item.ID)
}

func (m *MySQLArchive) DeleteRuntimeMirror(id string) error {
	id = strings.TrimSpace(id)
	result, err := m.db.Exec("DELETE FROM runtime_mirrors WHERE id = ?", id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *MySQLArchive) getRuntimeMirror(id string) (*model.RuntimeMirror, error) {
	row := m.db.QueryRow(`
SELECT id, runtime_id, version_id, name, description, base_url, url_template,
       checksum_url_template, platform, arch, priority, timeout_seconds, enabled,
       headers_json, tags_json, created_at, updated_at
FROM runtime_mirrors
WHERE id = ?
`, id)
	item, err := scanRuntimeMirror(row)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

type runtimeMirrorScanner interface {
	Scan(dest ...any) error
}

func scanRuntimeMirror(scanner runtimeMirrorScanner) (model.RuntimeMirror, error) {
	var item model.RuntimeMirror
	var headersJSON []byte
	var tagsJSON []byte
	if err := scanner.Scan(
		&item.ID,
		&item.RuntimeID,
		&item.VersionID,
		&item.Name,
		&item.Description,
		&item.BaseURL,
		&item.URLTemplate,
		&item.ChecksumURLTemplate,
		&item.Platform,
		&item.Arch,
		&item.Priority,
		&item.TimeoutSeconds,
		&item.Enabled,
		&headersJSON,
		&tagsJSON,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.RuntimeMirror{}, err
	}
	if len(headersJSON) > 0 {
		if err := json.Unmarshal(headersJSON, &item.Headers); err != nil {
			return model.RuntimeMirror{}, err
		}
	}
	if len(tagsJSON) > 0 {
		if err := json.Unmarshal(tagsJSON, &item.Tags); err != nil {
			return model.RuntimeMirror{}, err
		}
	}
	return item, nil
}

func normalizeRuntimeMirrorForStore(item model.RuntimeMirror) model.RuntimeMirror {
	item.ID = strings.TrimSpace(item.ID)
	item.RuntimeID = strings.TrimSpace(item.RuntimeID)
	item.VersionID = strings.TrimSpace(item.VersionID)
	item.Name = firstText(strings.TrimSpace(item.Name), item.ID)
	item.Description = strings.TrimSpace(item.Description)
	item.BaseURL = strings.TrimSpace(item.BaseURL)
	item.URLTemplate = strings.TrimSpace(item.URLTemplate)
	item.ChecksumURLTemplate = strings.TrimSpace(item.ChecksumURLTemplate)
	item.Platform = strings.ToLower(strings.TrimSpace(item.Platform))
	item.Arch = strings.ToLower(strings.TrimSpace(item.Arch))
	item.Headers = cleanStringMap(item.Headers)
	item.Tags = cleanStringList(item.Tags)
	if item.Priority == 0 {
		item.Priority = 100
	}
	if item.TimeoutSeconds == 0 {
		item.TimeoutSeconds = 30
	}
	return item
}

func (m *MySQLArchive) ListToolCatalog(includeDisabled bool) ([]model.ToolCatalogItem, error) {
	query := `
SELECT id, name, description, category, permission_key, pattern, default_action,
       enabled, tags_json, sort_order, built_in, created_at, updated_at
FROM tool_catalog`
	if !includeDisabled {
		query += " WHERE enabled = 1"
	}
	query += " ORDER BY sort_order ASC, updated_at DESC, id ASC"
	rows, err := m.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ToolCatalogItem
	for rows.Next() {
		item, err := scanToolCatalogItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) UpsertToolCatalogItem(item model.ToolCatalogItem) (*model.ToolCatalogItem, error) {
	item = normalizeToolCatalogItemForStore(item)
	tagsJSON, err := json.Marshal(item.Tags)
	if err != nil {
		return nil, err
	}
	_, err = m.db.Exec(`
INSERT INTO tool_catalog (
  id, name, description, category, permission_key, pattern, default_action,
  enabled, tags_json, sort_order, built_in
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  name = VALUES(name),
  description = VALUES(description),
  category = VALUES(category),
  permission_key = VALUES(permission_key),
  pattern = VALUES(pattern),
  default_action = VALUES(default_action),
  enabled = VALUES(enabled),
  tags_json = VALUES(tags_json),
  sort_order = VALUES(sort_order),
  built_in = VALUES(built_in)
`, item.ID, item.Name, item.Description, item.Category, item.PermissionKey, item.Pattern,
		item.DefaultAction, item.Enabled, string(tagsJSON), item.SortOrder, item.BuiltIn)
	if err != nil {
		return nil, err
	}
	return m.getToolCatalogItem(item.ID)
}

func (m *MySQLArchive) DeleteToolCatalogItem(id string) error {
	id = strings.TrimSpace(id)
	tool, err := m.getToolCatalogItem(id)
	if err != nil {
		return err
	}
	result, err := m.db.Exec("DELETE FROM tool_catalog WHERE id = ?", id)
	if err != nil {
		return err
	}
	if equivalent, err := m.hasEquivalentToolPermission(*tool); err != nil {
		return err
	} else if !equivalent {
		if err := m.removeToolPermissionFromSemanticAgentSnapshots(*tool); err != nil {
			return err
		}
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *MySQLArchive) getToolCatalogItem(id string) (*model.ToolCatalogItem, error) {
	row := m.db.QueryRow(`
SELECT id, name, description, category, permission_key, pattern, default_action,
       enabled, tags_json, sort_order, built_in, created_at, updated_at
FROM tool_catalog
WHERE id = ?
`, id)
	item, err := scanToolCatalogItem(row)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

type toolCatalogItemScanner interface {
	Scan(dest ...any) error
}

func scanToolCatalogItem(scanner toolCatalogItemScanner) (model.ToolCatalogItem, error) {
	var item model.ToolCatalogItem
	var tagsJSON []byte
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.Description,
		&item.Category,
		&item.PermissionKey,
		&item.Pattern,
		&item.DefaultAction,
		&item.Enabled,
		&tagsJSON,
		&item.SortOrder,
		&item.BuiltIn,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.ToolCatalogItem{}, err
	}
	if len(tagsJSON) > 0 {
		if err := json.Unmarshal(tagsJSON, &item.Tags); err != nil {
			return model.ToolCatalogItem{}, err
		}
	}
	return item, nil
}

func normalizeToolCatalogItemForStore(item model.ToolCatalogItem) model.ToolCatalogItem {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = firstText(strings.TrimSpace(item.Name), item.ID)
	item.Description = strings.TrimSpace(item.Description)
	item.Category = firstText(strings.TrimSpace(item.Category), "通用")
	item.PermissionKey = strings.TrimSpace(item.PermissionKey)
	item.Pattern = firstText(strings.TrimSpace(item.Pattern), "*")
	item.DefaultAction = normalizePermissionAction(item.DefaultAction)
	item.Tags = cleanStringList(item.Tags)
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return item
}

func (m *MySQLArchive) ListSemanticAgents(includeDisabled bool) ([]model.SemanticAgentProfile, error) {
	query := `
SELECT id, name, description, icon, color, opencode_name, prompt,
       skill_ids_json, mcp_ids_json, skills_json, skill_definitions_json,
       recommended_mcp_servers_json, tool_permissions_json,
       model, enabled, sort_order, built_in, created_at, updated_at
FROM semantic_agents`
	if !includeDisabled {
		query += " WHERE enabled = 1"
	}
	query += " ORDER BY sort_order ASC, updated_at DESC, id ASC"
	rows, err := m.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SemanticAgentProfile
	for rows.Next() {
		item, err := scanSemanticAgent(rows)
		if err != nil {
			return nil, err
		}
		if err := m.hydrateSemanticAgentBindings(&item, includeDisabled); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) UpsertSemanticAgent(item model.SemanticAgentProfile) (*model.SemanticAgentProfile, error) {
	item = normalizeSemanticAgentForStore(item)
	skillIDsJSON, err := json.Marshal(item.SkillIDs)
	if err != nil {
		return nil, err
	}
	mcpIDsJSON, err := json.Marshal(item.MCPIDs)
	if err != nil {
		return nil, err
	}
	skillsJSON, err := json.Marshal(item.Skills)
	if err != nil {
		return nil, err
	}
	skillDefinitionsJSON, err := json.Marshal(item.SkillDefinitions)
	if err != nil {
		return nil, err
	}
	recommendedJSON, err := json.Marshal(item.RecommendedMCPServers)
	if err != nil {
		return nil, err
	}
	permissionsJSON, err := json.Marshal(item.ToolPermissions)
	if err != nil {
		return nil, err
	}
	_, err = m.db.Exec(`
INSERT INTO semantic_agents (
  id, name, description, icon, color, opencode_name, prompt,
  skill_ids_json, mcp_ids_json, skills_json, skill_definitions_json, recommended_mcp_servers_json, tool_permissions_json,
  model, enabled, sort_order, built_in
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  name = VALUES(name),
  description = VALUES(description),
  icon = VALUES(icon),
  color = VALUES(color),
  opencode_name = VALUES(opencode_name),
  prompt = VALUES(prompt),
  skill_ids_json = VALUES(skill_ids_json),
  mcp_ids_json = VALUES(mcp_ids_json),
  skills_json = VALUES(skills_json),
  skill_definitions_json = VALUES(skill_definitions_json),
  recommended_mcp_servers_json = VALUES(recommended_mcp_servers_json),
  tool_permissions_json = VALUES(tool_permissions_json),
  model = VALUES(model),
  enabled = VALUES(enabled),
  sort_order = VALUES(sort_order),
  built_in = VALUES(built_in)
`, item.ID, item.Name, item.Description, item.Icon, item.Color, item.OpencodeName, item.Prompt,
		string(skillIDsJSON), string(mcpIDsJSON), string(skillsJSON), string(skillDefinitionsJSON), string(recommendedJSON), string(permissionsJSON),
		item.Model, item.Enabled, item.SortOrder, item.BuiltIn)
	if err != nil {
		return nil, err
	}
	if err := m.replaceSemanticAgentSkillBindings(item.ID, item.SkillIDs); err != nil {
		return nil, err
	}
	if err := m.replaceSemanticAgentMCPBindings(item.ID, item.MCPIDs); err != nil {
		return nil, err
	}
	if err := m.replaceSemanticAgentRuntimeBindings(item.ID, item.RuntimeRequirements); err != nil {
		return nil, err
	}
	return m.getSemanticAgent(item.ID)
}

func (m *MySQLArchive) DeleteSemanticAgent(id string) error {
	id = strings.TrimSpace(id)
	result, err := m.db.Exec("DELETE FROM semantic_agents WHERE id = ?", id)
	if err != nil {
		return err
	}
	if _, err := m.db.Exec("DELETE FROM semantic_agent_skills WHERE agent_id = ?", id); err != nil {
		return err
	}
	if _, err := m.db.Exec("DELETE FROM semantic_agent_mcps WHERE agent_id = ?", id); err != nil {
		return err
	}
	if _, err := m.db.Exec("DELETE FROM semantic_agent_runtimes WHERE agent_id = ?", id); err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *MySQLArchive) getSemanticAgent(id string) (*model.SemanticAgentProfile, error) {
	row := m.db.QueryRow(`
SELECT id, name, description, icon, color, opencode_name, prompt,
       skill_ids_json, mcp_ids_json, skills_json, skill_definitions_json,
       recommended_mcp_servers_json, tool_permissions_json,
       model, enabled, sort_order, built_in, created_at, updated_at
FROM semantic_agents
WHERE id = ?
`, id)
	item, err := scanSemanticAgent(row)
	if err != nil {
		return nil, err
	}
	if err := m.hydrateSemanticAgentBindings(&item, true); err != nil {
		return nil, err
	}
	return &item, nil
}

type semanticAgentScanner interface {
	Scan(dest ...any) error
}

func scanSemanticAgent(scanner semanticAgentScanner) (model.SemanticAgentProfile, error) {
	var item model.SemanticAgentProfile
	var skillIDsJSON []byte
	var mcpIDsJSON []byte
	var skillsJSON []byte
	var skillDefinitionsJSON []byte
	var recommendedJSON []byte
	var permissionsJSON []byte
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.Description,
		&item.Icon,
		&item.Color,
		&item.OpencodeName,
		&item.Prompt,
		&skillIDsJSON,
		&mcpIDsJSON,
		&skillsJSON,
		&skillDefinitionsJSON,
		&recommendedJSON,
		&permissionsJSON,
		&item.Model,
		&item.Enabled,
		&item.SortOrder,
		&item.BuiltIn,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.SemanticAgentProfile{}, err
	}
	if len(skillIDsJSON) > 0 {
		if err := json.Unmarshal(skillIDsJSON, &item.SkillIDs); err != nil {
			return model.SemanticAgentProfile{}, err
		}
	}
	if len(mcpIDsJSON) > 0 {
		if err := json.Unmarshal(mcpIDsJSON, &item.MCPIDs); err != nil {
			return model.SemanticAgentProfile{}, err
		}
	}
	if len(skillsJSON) > 0 {
		if err := json.Unmarshal(skillsJSON, &item.Skills); err != nil {
			return model.SemanticAgentProfile{}, err
		}
	}
	if len(skillDefinitionsJSON) > 0 {
		if err := json.Unmarshal(skillDefinitionsJSON, &item.SkillDefinitions); err != nil {
			return model.SemanticAgentProfile{}, err
		}
	}
	if len(recommendedJSON) > 0 {
		if err := json.Unmarshal(recommendedJSON, &item.RecommendedMCPServers); err != nil {
			return model.SemanticAgentProfile{}, err
		}
	}
	if len(permissionsJSON) > 0 {
		if err := json.Unmarshal(permissionsJSON, &item.ToolPermissions); err != nil {
			return model.SemanticAgentProfile{}, err
		}
	}
	return item, nil
}

func (m *MySQLArchive) replaceSemanticAgentSkillBindings(agentID string, skillIDs []string) error {
	if _, err := m.db.Exec("DELETE FROM semantic_agent_skills WHERE agent_id = ?", agentID); err != nil {
		return err
	}
	for index, skillID := range skillIDs {
		if strings.TrimSpace(skillID) == "" {
			continue
		}
		if _, err := m.db.Exec(`
INSERT INTO semantic_agent_skills (agent_id, skill_id, sort_order)
VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE sort_order = VALUES(sort_order)
`, agentID, skillID, (index+1)*10); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLArchive) replaceSemanticAgentMCPBindings(agentID string, mcpIDs []string) error {
	if _, err := m.db.Exec("DELETE FROM semantic_agent_mcps WHERE agent_id = ?", agentID); err != nil {
		return err
	}
	for index, mcpID := range mcpIDs {
		if strings.TrimSpace(mcpID) == "" {
			continue
		}
		if _, err := m.db.Exec(`
INSERT INTO semantic_agent_mcps (agent_id, mcp_id, sort_order)
VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE sort_order = VALUES(sort_order)
`, agentID, mcpID, (index+1)*10); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLArchive) hydrateSemanticAgentBindings(item *model.SemanticAgentProfile, includeDisabled bool) error {
	if item == nil || strings.TrimSpace(item.ID) == "" {
		return nil
	}
	skills, err := m.listSemanticAgentSkills(item.ID, includeDisabled)
	if err != nil {
		return err
	}
	item.SkillIDs = nil
	item.Skills = nil
	item.SkillDefinitions = nil
	if len(skills) > 0 {
		item.SkillIDs = make([]string, 0, len(skills))
		item.Skills = make([]string, 0, len(skills))
		item.SkillDefinitions = make([]model.SemanticAgentSkill, 0, len(skills))
		for _, skill := range skills {
			item.SkillIDs = append(item.SkillIDs, skill.ID)
			item.Skills = append(item.Skills, skill.ID)
			item.SkillDefinitions = append(item.SkillDefinitions, model.SemanticAgentSkill{
				Name:         skill.ID,
				Description:  skill.Description,
				Content:      skill.Content,
				PackageFiles: cloneSkillPackageFiles(skill.PackageFiles),
			})
		}
	}
	mcps, err := m.listSemanticAgentMCPS(item.ID, includeDisabled)
	if err != nil {
		return err
	}
	item.MCPIDs = nil
	item.RecommendedMCPServers = nil
	item.RecommendedMCPConfigs = nil
	item.MCPDependencies = nil
	if len(mcps) > 0 {
		item.MCPIDs = make([]string, 0, len(mcps))
		item.RecommendedMCPServers = make([]string, 0, len(mcps))
		item.RecommendedMCPConfigs = make([]model.DeviceMCPServer, 0, len(mcps))
		item.MCPDependencies = make([]model.SemanticAgentMCPDependency, 0, len(mcps))
		for _, mcp := range mcps {
			item.MCPIDs = append(item.MCPIDs, mcp.ID)
			item.RecommendedMCPServers = append(item.RecommendedMCPServers, mcp.Name)
			item.MCPDependencies = append(item.MCPDependencies, model.SemanticAgentMCPDependency{
				ID:                mcp.ID,
				Name:              mcp.Name,
				Title:             mcp.Title,
				Type:              mcp.Type,
				SourceURL:         mcp.SourceURL,
				Config:            mcp.Config,
				Install:           mcp.Install,
				LaunchReady:       mcp.LaunchReady,
				LaunchBlockReason: mcp.LaunchBlockReason,
			})
			if mcp.LaunchReady {
				item.RecommendedMCPConfigs = append(item.RecommendedMCPConfigs, mcp.Config)
			}
		}
	}
	runtimes, err := m.listSemanticAgentRuntimes(item.ID, includeDisabled)
	if err != nil {
		return err
	}
	item.RuntimeRequirements = runtimes
	return nil
}

func (m *MySQLArchive) listSemanticAgentSkills(agentID string, includeDisabled bool) ([]model.Skill, error) {
	query := `
SELECT s.id, s.name, s.description, s.content, s.package_files_json, s.category, s.source, s.enabled, s.tags_json, s.sort_order, s.built_in, s.created_at, s.updated_at
FROM semantic_agent_skills rel
JOIN skills s ON s.id = rel.skill_id
WHERE rel.agent_id = ?`
	if !includeDisabled {
		query += " AND s.enabled = 1"
	}
	query += " ORDER BY rel.sort_order ASC, s.sort_order ASC, s.updated_at DESC"
	rows, err := m.db.Query(query, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Skill
	for rows.Next() {
		item, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) listSemanticAgentMCPS(agentID string, includeDisabled bool) ([]model.MCPCatalogItem, error) {
	query := `
SELECT c.id, c.name, c.title, c.description, c.type, c.source, c.source_url, c.credential_url, c.category, c.recommended, c.enabled,
       c.tags_json, c.sort_order, c.built_in, c.config_json, c.install_json, c.launch_ready, c.launch_block_reason, c.created_at, c.updated_at
FROM semantic_agent_mcps rel
JOIN mcp_catalog c ON c.id = rel.mcp_id
WHERE rel.agent_id = ?`
	if !includeDisabled {
		query += " AND c.enabled = 1"
	}
	query += " ORDER BY rel.sort_order ASC, c.sort_order ASC, c.updated_at DESC"
	rows, err := m.db.Query(query, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.MCPCatalogItem
	for rows.Next() {
		item, err := scanMCPCatalogItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) replaceSemanticAgentRuntimeBindings(agentID string, runtimes []model.SemanticAgentRuntime) error {
	if _, err := m.db.Exec("DELETE FROM semantic_agent_runtimes WHERE agent_id = ?", agentID); err != nil {
		return err
	}
	for index, item := range runtimes {
		runtimeID := strings.TrimSpace(item.RuntimeID)
		if runtimeID == "" {
			continue
		}
		sortOrder := item.SortOrder
		if sortOrder == 0 {
			sortOrder = (index + 1) * 10
		}
		if _, err := m.db.Exec(`
INSERT INTO semantic_agent_runtimes (agent_id, runtime_id, version_constraint, required, purpose, sort_order)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  version_constraint = VALUES(version_constraint),
  required = VALUES(required),
  purpose = VALUES(purpose),
  sort_order = VALUES(sort_order)
`, agentID, runtimeID, strings.TrimSpace(item.VersionConstraint), item.Required, strings.TrimSpace(item.Purpose), sortOrder); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLArchive) listSemanticAgentRuntimes(agentID string, includeDisabled bool) ([]model.SemanticAgentRuntime, error) {
	query := `
SELECT rel.runtime_id, rel.version_constraint, rel.required, rel.purpose, rel.sort_order
FROM semantic_agent_runtimes rel`
	if !includeDisabled {
		query += " JOIN runtime_catalog r ON r.id = rel.runtime_id AND r.enabled = 1"
	}
	query += " WHERE rel.agent_id = ? ORDER BY rel.sort_order ASC, rel.runtime_id ASC"
	rows, err := m.db.Query(query, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SemanticAgentRuntime
	for rows.Next() {
		var item model.SemanticAgentRuntime
		if err := rows.Scan(&item.RuntimeID, &item.VersionConstraint, &item.Required, &item.Purpose, &item.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) UpsertRuntimeInstallJob(job model.RuntimeInstallJob) (*model.RuntimeInstallJob, error) {
	job = normalizeRuntimeInstallJobForStore(job)
	var userActionJSON any
	if job.UserAction != nil {
		data, err := json.Marshal(job.UserAction)
		if err != nil {
			return nil, err
		}
		userActionJSON = string(data)
	}
	_, err := m.db.Exec(`
INSERT INTO runtime_install_jobs (
  id, operator_id, machine_id, launcher_agent_id, semantic_agent_id, status,
  current_step, progress_percent, requires_user_action, user_action_json,
  requested_by, started_at, completed_at, error
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  operator_id = VALUES(operator_id),
  machine_id = VALUES(machine_id),
  launcher_agent_id = VALUES(launcher_agent_id),
  semantic_agent_id = VALUES(semantic_agent_id),
  status = VALUES(status),
  current_step = VALUES(current_step),
  progress_percent = VALUES(progress_percent),
  requires_user_action = VALUES(requires_user_action),
  user_action_json = VALUES(user_action_json),
  requested_by = VALUES(requested_by),
  started_at = VALUES(started_at),
  completed_at = VALUES(completed_at),
  error = VALUES(error)
`, job.ID, job.OperatorID, job.MachineID, job.LauncherAgentID, job.SemanticAgentID,
		job.Status, job.CurrentStep, job.ProgressPercent, job.RequiresUserAction, userActionJSON, job.RequestedBy,
		nullableTime(job.StartedAt), nullableTime(job.CompletedAt), nullableString(job.Error))
	if err != nil {
		return nil, err
	}
	return m.getRuntimeInstallJob(0, job.ID, true)
}

func (m *MySQLArchive) GetRuntimeInstallJob(operatorID int64, jobID string) (*model.RuntimeInstallJob, error) {
	return m.getRuntimeInstallJob(operatorID, jobID, true)
}

func (m *MySQLArchive) ListRuntimeInstallJobs(operatorID int64, machineID string, limit int) ([]model.RuntimeInstallJob, error) {
	query := `
SELECT id, operator_id, machine_id, launcher_agent_id, semantic_agent_id, status,
       current_step, progress_percent, requires_user_action, user_action_json,
       requested_by, started_at, completed_at, error,
       created_at, updated_at
FROM runtime_install_jobs`
	args := []any{}
	conditions := []string{}
	if operatorID > 0 {
		conditions = append(conditions, "operator_id = ?")
		args = append(args, operatorID)
	}
	if strings.TrimSpace(machineID) != "" {
		conditions = append(conditions, "machine_id = ?")
		args = append(args, strings.TrimSpace(machineID))
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC, id DESC"
	if limit <= 0 {
		limit = 50
	}
	query += " LIMIT ?"
	args = append(args, limit)
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.RuntimeInstallJob{}
	for rows.Next() {
		item, err := scanRuntimeInstallJob(rows)
		if err != nil {
			return nil, err
		}
		items, err := m.listRuntimeInstallJobItems(item.ID)
		if err != nil {
			return nil, err
		}
		item.Items = items
		out = append(out, item)
	}
	return out, rows.Err()
}

func (m *MySQLArchive) ReplaceRuntimeInstallJobItems(jobID string, items []model.RuntimeInstallJobItem) error {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return errors.New("预检任务 ID 不能为空")
	}
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM runtime_install_job_items WHERE job_id = ?", jobID); err != nil {
		return err
	}
	for _, item := range items {
		item = normalizeRuntimeInstallJobItemForStore(jobID, item)
		if item.ItemType == "" || item.ItemID == "" {
			continue
		}
		if _, err := tx.Exec(`
INSERT INTO runtime_install_job_items (
  job_id, item_type, item_id, name, required, status, progress_percent, error
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  name = VALUES(name),
  required = VALUES(required),
  status = VALUES(status),
  progress_percent = VALUES(progress_percent),
  error = VALUES(error)
`, item.JobID, item.ItemType, item.ItemID, item.Name, item.Required, item.Status,
			item.ProgressPercent, nullableString(item.Error)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (m *MySQLArchive) AppendRuntimeInstallEvent(event model.RuntimeInstallEvent) (*model.RuntimeInstallEvent, error) {
	event = normalizeRuntimeInstallEventForStore(event)
	if event.JobID == "" {
		return nil, errors.New("预检事件缺少 job_id")
	}
	detailsJSON, err := json.Marshal(event.Details)
	if err != nil {
		return nil, err
	}
	tx, err := m.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var locked string
	if err := tx.QueryRow("SELECT id FROM runtime_install_jobs WHERE id = ? FOR UPDATE", event.JobID).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if event.Sequence <= 0 {
		if err := tx.QueryRow("SELECT COALESCE(MAX(sequence), 0) + 1 FROM runtime_install_events WHERE job_id = ?", event.JobID).Scan(&event.Sequence); err != nil {
			return nil, err
		}
	}
	result, err := tx.Exec(`
INSERT INTO runtime_install_events (
  job_id, sequence, item_type, item_id, phase, status, message,
  received_bytes, total_bytes, progress_percent, details_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, event.JobID, event.Sequence, event.ItemType, event.ItemID, event.Phase, event.Status,
		event.Message, event.ReceivedBytes, event.TotalBytes, event.ProgressPercent,
		string(detailsJSON), event.CreatedAt)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	event.ID = id
	return &event, nil
}

func (m *MySQLArchive) ListRuntimeInstallEvents(jobID string, afterSequence int64) ([]model.RuntimeInstallEvent, error) {
	return m.listRuntimeInstallEvents(jobID, afterSequence, 0)
}

func (m *MySQLArchive) getRuntimeInstallJob(operatorID int64, jobID string, hydrate bool) (*model.RuntimeInstallJob, error) {
	jobID = strings.TrimSpace(jobID)
	query := `
SELECT id, operator_id, machine_id, launcher_agent_id, semantic_agent_id, status,
       current_step, progress_percent, requires_user_action, user_action_json,
       requested_by, started_at, completed_at, error,
       created_at, updated_at
FROM runtime_install_jobs
WHERE id = ?`
	args := []any{jobID}
	if operatorID > 0 {
		query += " AND operator_id = ?"
		args = append(args, operatorID)
	}
	item, err := scanRuntimeInstallJob(m.db.QueryRow(query, args...))
	if err != nil {
		return nil, err
	}
	if hydrate {
		items, err := m.listRuntimeInstallJobItems(item.ID)
		if err != nil {
			return nil, err
		}
		events, err := m.listRuntimeInstallEvents(item.ID, 0, 200)
		if err != nil {
			return nil, err
		}
		item.Items = items
		item.LatestEvents = events
	}
	return &item, nil
}

type runtimeInstallJobScanner interface {
	Scan(dest ...any) error
}

func scanRuntimeInstallJob(scanner runtimeInstallJobScanner) (model.RuntimeInstallJob, error) {
	var item model.RuntimeInstallJob
	var startedAt sql.NullTime
	var completedAt sql.NullTime
	var errorText sql.NullString
	var userActionJSON []byte
	if err := scanner.Scan(
		&item.ID,
		&item.OperatorID,
		&item.MachineID,
		&item.LauncherAgentID,
		&item.SemanticAgentID,
		&item.Status,
		&item.CurrentStep,
		&item.ProgressPercent,
		&item.RequiresUserAction,
		&userActionJSON,
		&item.RequestedBy,
		&startedAt,
		&completedAt,
		&errorText,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.RuntimeInstallJob{}, err
	}
	if startedAt.Valid {
		item.StartedAt = startedAt.Time
	}
	if completedAt.Valid {
		item.CompletedAt = completedAt.Time
	}
	if errorText.Valid {
		item.Error = errorText.String
	}
	if len(userActionJSON) > 0 {
		if err := json.Unmarshal(userActionJSON, &item.UserAction); err != nil {
			return model.RuntimeInstallJob{}, err
		}
	}
	return item, nil
}

func (m *MySQLArchive) ClaimRuntimeUserActionNotification(operatorID int64, jobID, actionID, channel string) (bool, error) {
	jobID = strings.TrimSpace(jobID)
	actionID = strings.TrimSpace(actionID)
	channel = strings.TrimSpace(channel)
	if operatorID <= 0 || jobID == "" || actionID == "" || channel == "" {
		return false, errors.New("用户操作通知缺少幂等键")
	}
	result, err := m.db.Exec(`
INSERT IGNORE INTO runtime_user_action_notifications (operator_id, job_id, action_id, channel)
VALUES (?, ?, ?, ?)
`, operatorID, jobID, actionID, channel)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (m *MySQLArchive) listRuntimeInstallJobItems(jobID string) ([]model.RuntimeInstallJobItem, error) {
	rows, err := m.db.Query(`
SELECT job_id, item_type, item_id, name, required, status, progress_percent, error, updated_at
FROM runtime_install_job_items
WHERE job_id = ?
ORDER BY FIELD(item_type, 'runtime', 'mcp', 'skill', 'apply'), item_type ASC, item_id ASC
`, strings.TrimSpace(jobID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.RuntimeInstallJobItem{}
	for rows.Next() {
		item, err := scanRuntimeInstallJobItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type runtimeInstallJobItemScanner interface {
	Scan(dest ...any) error
}

func scanRuntimeInstallJobItem(scanner runtimeInstallJobItemScanner) (model.RuntimeInstallJobItem, error) {
	var item model.RuntimeInstallJobItem
	var errorText sql.NullString
	if err := scanner.Scan(
		&item.JobID,
		&item.ItemType,
		&item.ItemID,
		&item.Name,
		&item.Required,
		&item.Status,
		&item.ProgressPercent,
		&errorText,
		&item.UpdatedAt,
	); err != nil {
		return model.RuntimeInstallJobItem{}, err
	}
	if errorText.Valid {
		item.Error = errorText.String
	}
	return item, nil
}

func (m *MySQLArchive) listRuntimeInstallEvents(jobID string, afterSequence int64, limit int) ([]model.RuntimeInstallEvent, error) {
	query := `
SELECT id, job_id, sequence, item_type, item_id, phase, status, message,
       received_bytes, total_bytes, progress_percent, details_json, created_at
FROM runtime_install_events
WHERE job_id = ? AND sequence > ?
ORDER BY sequence ASC`
	args := []any{strings.TrimSpace(jobID), afterSequence}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.RuntimeInstallEvent{}
	for rows.Next() {
		item, err := scanRuntimeInstallEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type runtimeInstallEventScanner interface {
	Scan(dest ...any) error
}

func scanRuntimeInstallEvent(scanner runtimeInstallEventScanner) (model.RuntimeInstallEvent, error) {
	var item model.RuntimeInstallEvent
	var message sql.NullString
	var detailsJSON []byte
	if err := scanner.Scan(
		&item.ID,
		&item.JobID,
		&item.Sequence,
		&item.ItemType,
		&item.ItemID,
		&item.Phase,
		&item.Status,
		&message,
		&item.ReceivedBytes,
		&item.TotalBytes,
		&item.ProgressPercent,
		&detailsJSON,
		&item.CreatedAt,
	); err != nil {
		return model.RuntimeInstallEvent{}, err
	}
	if message.Valid {
		item.Message = message.String
	}
	if len(detailsJSON) > 0 {
		if err := json.Unmarshal(detailsJSON, &item.Details); err != nil {
			return model.RuntimeInstallEvent{}, err
		}
	}
	return item, nil
}

func normalizeRuntimeInstallJobForStore(job model.RuntimeInstallJob) model.RuntimeInstallJob {
	job.ID = strings.TrimSpace(job.ID)
	job.MachineID = strings.TrimSpace(job.MachineID)
	job.LauncherAgentID = strings.TrimSpace(job.LauncherAgentID)
	job.SemanticAgentID = strings.TrimSpace(job.SemanticAgentID)
	job.Status = firstText(strings.TrimSpace(job.Status), "pending")
	job.CurrentStep = strings.TrimSpace(job.CurrentStep)
	job.ProgressPercent = clampStorePercent(job.ProgressPercent)
	job.RequestedBy = strings.TrimSpace(job.RequestedBy)
	job.Error = strings.TrimSpace(job.Error)
	if !job.RequiresUserAction {
		job.UserAction = nil
	} else if job.UserAction != nil {
		job.UserAction.ID = strings.TrimSpace(job.UserAction.ID)
		job.UserAction.Kind = strings.TrimSpace(job.UserAction.Kind)
		job.UserAction.Title = strings.TrimSpace(job.UserAction.Title)
		job.UserAction.Message = strings.TrimSpace(job.UserAction.Message)
		instructions := make([]string, 0, len(job.UserAction.Instructions))
		for _, instruction := range job.UserAction.Instructions {
			if instruction = strings.TrimSpace(instruction); instruction != "" {
				instructions = append(instructions, instruction)
			}
		}
		job.UserAction.Instructions = instructions
	}
	return job
}

func normalizeRuntimeInstallJobItemForStore(jobID string, item model.RuntimeInstallJobItem) model.RuntimeInstallJobItem {
	item.JobID = strings.TrimSpace(jobID)
	item.ItemType = strings.TrimSpace(item.ItemType)
	item.ItemID = strings.TrimSpace(item.ItemID)
	item.Name = strings.TrimSpace(item.Name)
	item.Status = firstText(strings.TrimSpace(item.Status), "pending")
	item.ProgressPercent = clampStorePercent(item.ProgressPercent)
	item.Error = strings.TrimSpace(item.Error)
	return item
}

func normalizeRuntimeInstallEventForStore(event model.RuntimeInstallEvent) model.RuntimeInstallEvent {
	event.JobID = strings.TrimSpace(event.JobID)
	event.ItemType = strings.TrimSpace(event.ItemType)
	event.ItemID = strings.TrimSpace(event.ItemID)
	event.Phase = strings.TrimSpace(event.Phase)
	event.Status = strings.TrimSpace(event.Status)
	event.Message = strings.TrimSpace(event.Message)
	event.ProgressPercent = clampStorePercent(event.ProgressPercent)
	if event.Details == nil {
		event.Details = map[string]any{}
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return event
}

func normalizeSemanticAgentForStore(item model.SemanticAgentProfile) model.SemanticAgentProfile {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = firstText(strings.TrimSpace(item.Name), item.ID)
	item.Description = strings.TrimSpace(item.Description)
	item.Icon = strings.TrimSpace(item.Icon)
	item.Color = strings.TrimSpace(item.Color)
	item.OpencodeName = strings.TrimSpace(item.OpencodeName)
	if item.OpencodeName == "" {
		item.OpencodeName = item.ID
	}
	item.Prompt = strings.TrimSpace(item.Prompt)
	item.Model = strings.TrimSpace(item.Model)
	item.SkillIDs = cleanStringList(item.SkillIDs)
	item.MCPIDs = cleanStringList(item.MCPIDs)
	item.Skills = cleanStringList(item.Skills)
	item.RecommendedMCPServers = cleanStringList(item.RecommendedMCPServers)
	cleanSkills := make([]model.SemanticAgentSkill, 0, len(item.SkillDefinitions))
	for _, skill := range item.SkillDefinitions {
		skill.Name = strings.TrimSpace(skill.Name)
		skill.Description = strings.TrimSpace(skill.Description)
		skill.Content = strings.TrimSpace(skill.Content)
		if skill.Name == "" {
			continue
		}
		cleanSkills = append(cleanSkills, skill)
	}
	item.SkillDefinitions = cleanSkills
	if item.ToolPermissions == nil {
		item.ToolPermissions = map[string]any{}
	}
	item.RuntimeRequirements = cleanSemanticAgentRuntimes(item.RuntimeRequirements)
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return item
}

func cleanSemanticAgentRuntimes(input []model.SemanticAgentRuntime) []model.SemanticAgentRuntime {
	if input == nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]model.SemanticAgentRuntime, 0, len(input))
	for index, item := range input {
		item.RuntimeID = strings.TrimSpace(item.RuntimeID)
		if item.RuntimeID == "" || seen[item.RuntimeID] {
			continue
		}
		seen[item.RuntimeID] = true
		item.VersionConstraint = strings.TrimSpace(item.VersionConstraint)
		item.Purpose = strings.TrimSpace(item.Purpose)
		if item.SortOrder == 0 {
			item.SortOrder = (index + 1) * 10
		}
		out = append(out, item)
	}
	return out
}

func cleanStringList(input []string) []string {
	if input == nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(input))
	for _, value := range input {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func sameStringList(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameSemanticAgentSkillDefinitions(left []model.SemanticAgentSkill, right []model.SemanticAgentSkill) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Name != right[index].Name ||
			left[index].Description != right[index].Description ||
			left[index].Content != right[index].Content ||
			!sameSkillPackageFiles(left[index].PackageFiles, right[index].PackageFiles) {
			return false
		}
	}
	return true
}

func cleanSkillPackageFiles(input []model.SkillPackageFile) []model.SkillPackageFile {
	if input == nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]model.SkillPackageFile, 0, len(input))
	for _, item := range input {
		item.Path = cleanSkillPackagePath(item.Path)
		if item.Path == "" || seen[item.Path] {
			continue
		}
		seen[item.Path] = true
		out = append(out, item)
	}
	return out
}

func cleanSkillPackagePath(input string) string {
	value := strings.TrimSpace(strings.ReplaceAll(input, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\x00") {
		return ""
	}
	parts := strings.Split(value, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			return ""
		}
		cleaned = append(cleaned, part)
	}
	value = strings.Join(cleaned, "/")
	if strings.EqualFold(value, "SKILL.md") {
		return ""
	}
	return value
}

func cloneSkillPackageFiles(input []model.SkillPackageFile) []model.SkillPackageFile {
	if input == nil {
		return nil
	}
	out := make([]model.SkillPackageFile, len(input))
	copy(out, input)
	return out
}

func sameSkillPackageFiles(left []model.SkillPackageFile, right []model.SkillPackageFile) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameSemanticAgentRuntimes(left []model.SemanticAgentRuntime, right []model.SemanticAgentRuntime) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].RuntimeID != right[index].RuntimeID ||
			left[index].VersionConstraint != right[index].VersionConstraint ||
			left[index].Required != right[index].Required ||
			left[index].Purpose != right[index].Purpose ||
			left[index].SortOrder != right[index].SortOrder {
			return false
		}
	}
	return true
}

func sameAnyMap(left map[string]any, right map[string]any) bool {
	leftJSON, err := json.Marshal(left)
	if err != nil {
		return false
	}
	rightJSON, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return string(leftJSON) == string(rightJSON)
}

func cleanStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	return out
}

func normalizePermissionAction(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow", "deny", "ask":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "ask"
	}
}

func clampStorePercent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func firstText(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func normalizeAssetDescription(description string, content string) string {
	description = compactInlineText(description)
	if !isPlaceholderDescription(description) {
		return description
	}
	fromContent := compactInlineText(extractFrontMatterDescription(content))
	if isPlaceholderDescription(fromContent) {
		return ""
	}
	return fromContent
}

func isPlaceholderDescription(value string) bool {
	switch strings.TrimSpace(value) {
	case "", ">", ">-", ">+", "|", "|-":
		return true
	default:
		return false
	}
}

func compactInlineText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func extractFrontMatterDescription(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for index := 1; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			return ""
		}
		if !strings.HasPrefix(trimmed, "description:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
		switch value {
		case ">", ">-", ">+", "|", "|-", "|+":
			return collectIndentedFrontMatterValue(lines, index+1)
		default:
			return strings.Trim(strings.Trim(value, `"`), `'`)
		}
	}
	return ""
}

func collectIndentedFrontMatterValue(lines []string, start int) string {
	var out []string
	for index := start; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			break
		}
		if trimmed == "" {
			continue
		}
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			break
		}
		out = append(out, trimmed)
	}
	return strings.Join(out, " ")
}
