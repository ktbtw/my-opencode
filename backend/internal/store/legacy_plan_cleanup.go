package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"relay-server/internal/model"
)

type LegacyPlanCleanupOptions struct {
	Apply      bool
	BackupPath string
}

type LegacyPlanCleanupStats struct {
	TasksMatched  int      `json:"tasks_matched"`
	TasksUpdated  int      `json:"tasks_updated"`
	EventsMatched int      `json:"events_matched"`
	EventsDeleted int      `json:"events_deleted"`
	Sessions      []string `json:"sessions"`
	BackupPath    string   `json:"backup_path,omitempty"`
	StartedAt     string   `json:"started_at"`
	FinishedAt    string   `json:"finished_at"`
}

type legacyPlanCleanupBackup struct {
	CreatedAt string                         `json:"created_at"`
	Tasks     []legacyPlanCleanupTaskBackup  `json:"tasks"`
	Events    []legacyPlanCleanupEventBackup `json:"events"`
}

type legacyPlanCleanupTaskBackup struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	InputJSON string `json:"input_json"`
}

type legacyPlanCleanupEventBackup struct {
	ID          int64  `json:"id"`
	TaskID      string `json:"task_id"`
	SessionID   string `json:"session_id"`
	PayloadJSON string `json:"payload_json"`
}

type legacyPlanCleanupTaskRow struct {
	ID        int64
	TaskID    string
	SessionID string
	InputJSON string
}

type legacyPlanCleanupEventRow struct {
	ID          int64
	TaskID      string
	SessionID   string
	PayloadJSON string
}

func isLegacyPlanID(id string) bool {
	return strings.HasPrefix(id, "plan_ses_")
}

func cleanLegacyTaskInput(raw string) (string, string, bool, error) {
	var persist struct {
		Parts    []model.Part           `json:"parts,omitempty"`
		Approval *model.Approval        `json:"approval,omitempty"`
		Question *model.QuestionRequest `json:"question,omitempty"`
		Plan     *model.Plan            `json:"plan,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &persist); err != nil {
		return "", "", false, err
	}
	if persist.Plan == nil || !isLegacyPlanID(persist.Plan.ID) {
		return raw, "", false, nil
	}
	sid := persist.Plan.SessionID
	persist.Plan = nil
	buf, err := json.Marshal(persist)
	if err != nil {
		return "", "", false, err
	}
	return string(buf), sid, true, nil
}

func legacyEventPlanID(raw string) (string, string, bool, error) {
	var evt model.Event
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		return "", "", false, err
	}
	if evt.Plan == nil || !isLegacyPlanID(evt.Plan.ID) {
		return "", "", false, nil
	}
	return evt.Plan.ID, evt.SessionID, true, nil
}

func (m *MySQLArchive) CleanupLegacyPlans(ctx context.Context, opts LegacyPlanCleanupOptions) (*LegacyPlanCleanupStats, error) {
	start := time.Now().UTC()
	stats := &LegacyPlanCleanupStats{
		StartedAt: start.Format(time.RFC3339),
	}
	seen := map[string]struct{}{}
	backup := legacyPlanCleanupBackup{
		CreatedAt: start.Format(time.RFC3339),
	}
	taskRows := []legacyPlanCleanupTaskRow{}
	eventRows := []legacyPlanCleanupEventRow{}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
SELECT id, task_id, session_id, input_json
FROM tasks
WHERE input_json LIKE '%plan_ses_%'
ORDER BY id
`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var taskID string
		var sessionID string
		var inputJSON string
		if err := rows.Scan(&id, &taskID, &sessionID, &inputJSON); err != nil {
			rows.Close()
			return nil, err
		}
		next, sid, ok, err := cleanLegacyTaskInput(inputJSON)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if !ok {
			continue
		}
		stats.TasksMatched++
		backup.Tasks = append(backup.Tasks, legacyPlanCleanupTaskBackup{
			ID:        id,
			TaskID:    taskID,
			SessionID: sessionID,
			InputJSON: inputJSON,
		})
		if sessionID != "" {
			seen[sessionID] = struct{}{}
		}
		if sid != "" {
			seen[sid] = struct{}{}
		}
		taskRows = append(taskRows, legacyPlanCleanupTaskRow{
			ID:        id,
			TaskID:    taskID,
			SessionID: sessionID,
			InputJSON: next,
		})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = tx.QueryContext(ctx, `
SELECT id, task_id, session_id, payload_json
FROM task_events
WHERE event_type = 'plan_updated' AND payload_json LIKE '%plan_ses_%'
ORDER BY id
`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var taskID string
		var sessionID string
		var payloadJSON sql.NullString
		if err := rows.Scan(&id, &taskID, &sessionID, &payloadJSON); err != nil {
			rows.Close()
			return nil, err
		}
		if !payloadJSON.Valid || payloadJSON.String == "" {
			continue
		}
		_, sid, ok, err := legacyEventPlanID(payloadJSON.String)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if !ok {
			continue
		}
		stats.EventsMatched++
		backup.Events = append(backup.Events, legacyPlanCleanupEventBackup{
			ID:          id,
			TaskID:      taskID,
			SessionID:   sessionID,
			PayloadJSON: payloadJSON.String,
		})
		if sessionID != "" {
			seen[sessionID] = struct{}{}
		}
		if sid != "" {
			seen[sid] = struct{}{}
		}
		eventRows = append(eventRows, legacyPlanCleanupEventRow{
			ID:          id,
			TaskID:      taskID,
			SessionID:   sessionID,
			PayloadJSON: payloadJSON.String,
		})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for sid := range seen {
		stats.Sessions = append(stats.Sessions, sid)
	}
	slices.Sort(stats.Sessions)

	if opts.BackupPath != "" && (stats.TasksMatched > 0 || stats.EventsMatched > 0) {
		if err := os.MkdirAll(filepath.Dir(opts.BackupPath), 0o755); err != nil {
			return nil, err
		}
		buf, err := json.MarshalIndent(backup, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(opts.BackupPath, buf, 0o644); err != nil {
			return nil, err
		}
		stats.BackupPath = opts.BackupPath
	}

	if opts.Apply {
		for _, row := range taskRows {
			if _, err := tx.ExecContext(ctx, `UPDATE tasks SET input_json = ?, updated_at = updated_at WHERE id = ?`, row.InputJSON, row.ID); err != nil {
				return nil, err
			}
			stats.TasksUpdated++
		}
		for _, row := range eventRows {
			if _, err := tx.ExecContext(ctx, `DELETE FROM task_events WHERE id = ?`, row.ID); err != nil {
				return nil, err
			}
			stats.EventsDeleted++
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}

	stats.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	return stats, nil
}
