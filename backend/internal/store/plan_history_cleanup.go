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

type PlanHistoryCleanupOptions struct {
	Apply      bool
	BackupPath string
}

type PlanHistoryCleanupStats struct {
	EventsScanned   int      `json:"events_scanned"`
	PlanEvents      int      `json:"plan_events"`
	DuplicatesFound int      `json:"duplicates_found"`
	EventsDeleted   int      `json:"events_deleted"`
	Tasks           []string `json:"tasks"`
	BackupPath      string   `json:"backup_path,omitempty"`
	StartedAt       string   `json:"started_at"`
	FinishedAt      string   `json:"finished_at"`
}

type planHistoryCleanupBackup struct {
	CreatedAt string                          `json:"created_at"`
	Events    []planHistoryCleanupEventBackup `json:"events"`
}

type planHistoryCleanupEventBackup struct {
	ID          int64  `json:"id"`
	TaskID      string `json:"task_id"`
	SessionID   string `json:"session_id"`
	SentAt      string `json:"sent_at"`
	PayloadJSON string `json:"payload_json"`
}

type planHistoryCleanupEventRow struct {
	ID          int64
	TaskID      string
	SessionID   string
	SentAt      time.Time
	PayloadJSON string
}

const (
	planHistoryCleanupReadBatch   = 1000
	planHistoryCleanupDeleteBatch = 200
)

func planHistoryContentKey(evt model.Event) string {
	if evt.Plan == nil {
		return ""
	}
	parts := make([]string, 0, len(evt.Plan.Items)+3)
	parts = append(parts, strings.TrimSpace(evt.TaskID))
	parts = append(parts, strings.TrimSpace(evt.Plan.ID))
	parts = append(parts, strings.TrimSpace(evt.Plan.SessionID))
	for _, item := range evt.Plan.Items {
		parts = append(parts,
			strings.TrimSpace(item.ID),
			strings.TrimSpace(item.Text),
			strings.TrimSpace(item.Status),
			strings.TrimSpace(item.Priority),
		)
	}
	return strings.Join(parts, "\x1f")
}

func parsePlanHistoryEvent(raw string) (model.Event, string, bool, error) {
	var evt model.Event
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		return model.Event{}, "", false, err
	}
	if evt.Type != "plan_updated" || evt.Plan == nil || len(evt.Plan.Items) == 0 {
		return evt, "", false, nil
	}
	key := planHistoryContentKey(evt)
	if key == "" {
		return evt, "", false, nil
	}
	return evt, key, true, nil
}

func (m *MySQLArchive) CleanupPlanHistory(ctx context.Context, opts PlanHistoryCleanupOptions) (*PlanHistoryCleanupStats, error) {
	start := time.Now().UTC()
	stats := &PlanHistoryCleanupStats{
		StartedAt: start.Format(time.RFC3339),
	}
	taskIDs := map[string]struct{}{}
	type canonicalEvent struct {
		id     int64
		sentAt time.Time
		row    planHistoryCleanupEventRow
	}
	canonical := map[string]canonicalEvent{}
	duplicateByID := map[int64]planHistoryCleanupEventRow{}
	backup := planHistoryCleanupBackup{
		CreatedAt: start.Format(time.RFC3339),
	}

	// Scan by primary-key order in bounded batches. The old query ordered the
	// entire event-type result by (task_id, sent_at, id), forcing MySQL to sort
	// a large slice of task_events and retain it for the lifetime of a transaction.
	var lastID int64
	for {
		rows, err := m.db.QueryContext(ctx, `
SELECT id, task_id, session_id, sent_at, payload_json
FROM task_events
	WHERE event_type = ? AND id > ?
ORDER BY id ASC
LIMIT ?
`, "plan_updated", lastID, planHistoryCleanupReadBatch)
		if err != nil {
			return nil, err
		}
		batchCount := 0
		for rows.Next() {
			var id int64
			var taskID string
			var sessionID string
			var sentAt time.Time
			var payloadJSON sql.NullString
			if err := rows.Scan(&id, &taskID, &sessionID, &sentAt, &payloadJSON); err != nil {
				rows.Close()
				return nil, err
			}
			lastID = id
			batchCount++
			stats.EventsScanned++
			if !payloadJSON.Valid || strings.TrimSpace(payloadJSON.String) == "" {
				continue
			}
			evt, key, ok, err := parsePlanHistoryEvent(payloadJSON.String)
			if err != nil {
				rows.Close()
				return nil, err
			}
			if !ok {
				continue
			}
			stats.PlanEvents++
			if taskID != "" {
				taskIDs[taskID] = struct{}{}
			}
			if evt.TaskID != "" {
				taskIDs[evt.TaskID] = struct{}{}
			}
			row := planHistoryCleanupEventRow{
				ID:          id,
				TaskID:      taskID,
				SessionID:   sessionID,
				SentAt:      sentAt,
				PayloadJSON: payloadJSON.String,
			}
			previous, exists := canonical[key]
			if !exists || sentAt.Before(previous.sentAt) || (sentAt.Equal(previous.sentAt) && id < previous.id) {
				if exists {
					duplicateByID[previous.id] = previous.row
				}
				canonical[key] = canonicalEvent{id: id, sentAt: sentAt, row: row}
			} else {
				duplicateByID[id] = row
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if batchCount == 0 {
			break
		}
	}

	duplicates := make([]planHistoryCleanupEventRow, 0, len(duplicateByID))
	for _, row := range duplicateByID {
		duplicates = append(duplicates, row)
	}
	slices.SortFunc(duplicates, func(a, b planHistoryCleanupEventRow) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	stats.DuplicatesFound = len(duplicates)
	for _, row := range duplicates {
		backup.Events = append(backup.Events, planHistoryCleanupEventBackup{
			ID: row.ID, TaskID: row.TaskID, SessionID: row.SessionID,
			SentAt: row.SentAt.UTC().Format(time.RFC3339), PayloadJSON: row.PayloadJSON,
		})
	}

	for taskID := range taskIDs {
		stats.Tasks = append(stats.Tasks, taskID)
	}
	slices.Sort(stats.Tasks)

	if opts.BackupPath != "" && len(duplicates) > 0 {
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
		for start := 0; start < len(duplicates); start += planHistoryCleanupDeleteBatch {
			end := start + planHistoryCleanupDeleteBatch
			if end > len(duplicates) {
				end = len(duplicates)
			}
			tx, err := m.db.BeginTx(ctx, nil)
			if err != nil {
				return nil, err
			}
			for _, row := range duplicates[start:end] {
				if _, err := tx.ExecContext(ctx, `DELETE FROM task_events WHERE id = ?`, row.ID); err != nil {
					_ = tx.Rollback()
					return nil, err
				}
				stats.EventsDeleted++
			}
			if err := tx.Commit(); err != nil {
				return nil, err
			}
		}
	}

	stats.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	return stats, nil
}
