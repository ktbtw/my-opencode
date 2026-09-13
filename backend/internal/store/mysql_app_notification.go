package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"relay-server/internal/model"
)

var appNotificationSchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS app_notifications (
  operator_id BIGINT NOT NULL,
  operation_id VARCHAR(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  record_json JSON NULL,
  version BIGINT NOT NULL,
  deleted TINYINT(1) NOT NULL DEFAULT 0,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (operator_id, operation_id),
  KEY idx_app_notifications_version (operator_id, version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
	`CREATE TABLE IF NOT EXISTS app_notification_changes (
  version BIGINT PRIMARY KEY AUTO_INCREMENT,
  operator_id BIGINT NOT NULL,
  operation_id VARCHAR(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  record_json JSON NULL,
  deleted TINYINT(1) NOT NULL DEFAULT 0,
  updated_at DATETIME(6) NOT NULL,
  KEY idx_app_notification_changes_operator (operator_id, version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
}

func (m *MySQLArchive) UpsertAppNotification(
	ctx context.Context,
	operatorID int64,
	operationID string,
	record json.RawMessage,
) (*model.AppNotificationChange, error) {
	if operatorID <= 0 || strings.TrimSpace(operationID) == "" || len(record) == 0 || !json.Valid(record) {
		return nil, errors.New("invalid app notification")
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	change, err := upsertAppNotificationTx(ctx, tx, operatorID, operationID, record, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &change, nil
}

func (m *MySQLArchive) ListAppNotificationChanges(
	ctx context.Context,
	operatorID int64,
	after int64,
	limit int,
) (model.AppNotificationChangePage, error) {
	limit = normalizeAppNotificationLimit(limit)
	if after <= 0 {
		return m.listLatestAppNotifications(ctx, operatorID, limit)
	}
	rows, err := m.db.QueryContext(ctx, `SELECT version, operation_id, record_json, deleted, updated_at
FROM app_notification_changes WHERE operator_id=? AND version>? ORDER BY version LIMIT ?`, operatorID, after, limit)
	if err != nil {
		return model.AppNotificationChangePage{}, err
	}
	defer rows.Close()
	items, err := scanAppNotificationChanges(rows)
	if err != nil {
		return model.AppNotificationChangePage{}, err
	}
	cursor := after
	if len(items) > 0 {
		cursor = items[len(items)-1].Version
	}
	return model.AppNotificationChangePage{Items: items, Cursor: cursor}, nil
}

func (m *MySQLArchive) ListAppNotificationChangeFeed(
	ctx context.Context,
	operatorID int64,
	after int64,
	limit int,
) (model.AppNotificationChangePage, error) {
	limit = normalizeAppNotificationLimit(limit)
	rows, err := m.db.QueryContext(ctx, `SELECT version, operation_id, record_json, deleted, updated_at
FROM app_notification_changes WHERE operator_id=? AND version>? ORDER BY version LIMIT ?`, operatorID, after, limit)
	if err != nil {
		return model.AppNotificationChangePage{}, err
	}
	defer rows.Close()
	items, err := scanAppNotificationChanges(rows)
	if err != nil {
		return model.AppNotificationChangePage{}, err
	}
	cursor := after
	if len(items) > 0 {
		cursor = items[len(items)-1].Version
	}
	return model.AppNotificationChangePage{Items: items, Cursor: cursor}, nil
}

func (m *MySQLArchive) listLatestAppNotifications(
	ctx context.Context,
	operatorID int64,
	limit int,
) (model.AppNotificationChangePage, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT version, operation_id, record_json, deleted, updated_at
FROM app_notifications WHERE operator_id=? AND deleted=0 ORDER BY version DESC LIMIT ?`, operatorID, limit)
	if err != nil {
		return model.AppNotificationChangePage{}, err
	}
	items, scanErr := scanAppNotificationChanges(rows)
	closeErr := rows.Close()
	if scanErr != nil {
		return model.AppNotificationChangePage{}, scanErr
	}
	if closeErr != nil {
		return model.AppNotificationChangePage{}, closeErr
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Version < items[j].Version })
	var cursor int64
	if err := m.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM app_notification_changes WHERE operator_id=?`, operatorID).Scan(&cursor); err != nil {
		return model.AppNotificationChangePage{}, err
	}
	return model.AppNotificationChangePage{Items: items, Cursor: cursor}, nil
}

func (m *MySQLArchive) MarkAppNotificationsRead(
	ctx context.Context,
	operatorID int64,
	operationIDs []string,
) ([]model.AppNotificationChange, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	changes := make([]model.AppNotificationChange, 0, len(operationIDs))
	for _, operationID := range uniqueAppNotificationIDs(operationIDs) {
		var recordJSON []byte
		var deleted bool
		err := tx.QueryRowContext(ctx, `SELECT record_json, deleted FROM app_notifications
WHERE operator_id=? AND operation_id=? FOR UPDATE`, operatorID, operationID).Scan(&recordJSON, &deleted)
		if err == sql.ErrNoRows || deleted {
			continue
		}
		if err != nil {
			return nil, err
		}
		var record map[string]any
		if err := json.Unmarshal(recordJSON, &record); err != nil {
			return nil, err
		}
		if unread, ok := record["unread"].(bool); ok && !unread {
			continue
		}
		record["unread"] = false
		encoded, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		change, err := upsertAppNotificationTx(ctx, tx, operatorID, operationID, encoded, false)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return changes, nil
}

func (m *MySQLArchive) DismissAppNotifications(
	ctx context.Context,
	operatorID int64,
	operationIDs []string,
) ([]model.AppNotificationChange, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	changes := make([]model.AppNotificationChange, 0, len(operationIDs))
	for _, operationID := range uniqueAppNotificationIDs(operationIDs) {
		var deleted bool
		err := tx.QueryRowContext(ctx, `SELECT deleted FROM app_notifications
WHERE operator_id=? AND operation_id=? FOR UPDATE`, operatorID, operationID).Scan(&deleted)
		if err == nil && deleted {
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		change, err := upsertAppNotificationTx(ctx, tx, operatorID, operationID, nil, true)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return changes, nil
}

func upsertAppNotificationTx(
	ctx context.Context,
	tx *sql.Tx,
	operatorID int64,
	operationID string,
	record json.RawMessage,
	deleted bool,
) (model.AppNotificationChange, error) {
	now := time.Now().UTC()
	var recordValue any
	if len(record) > 0 {
		recordValue = []byte(record)
	}
	var currentRecord []byte
	var currentVersion int64
	var currentDeleted bool
	var currentUpdatedAt time.Time
	currentErr := tx.QueryRowContext(ctx, `SELECT record_json, version, deleted, updated_at
FROM app_notifications WHERE operator_id=? AND operation_id=? FOR UPDATE`, operatorID, operationID).
		Scan(&currentRecord, &currentVersion, &currentDeleted, &currentUpdatedAt)
	if currentErr != nil && !errors.Is(currentErr, sql.ErrNoRows) {
		return model.AppNotificationChange{}, currentErr
	}
	if currentErr == nil && !currentDeleted {
		storedAt, storedOK := appNotificationRecordUpdatedAt(currentRecord)
		incomingAt, incomingOK := appNotificationRecordUpdatedAt(record)
		if storedOK && incomingOK && incomingAt.Before(storedAt) {
			return model.AppNotificationChange{
				Version: currentVersion, OperationID: operationID,
				Record:  append(json.RawMessage(nil), currentRecord...),
				Deleted: currentDeleted, UpdatedAt: currentUpdatedAt,
			}, nil
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO app_notification_changes
(operator_id, operation_id, record_json, deleted, updated_at) VALUES (?, ?, ?, ?, ?)`,
		operatorID, operationID, recordValue, deleted, now)
	if err != nil {
		return model.AppNotificationChange{}, err
	}
	version, err := result.LastInsertId()
	if err != nil {
		return model.AppNotificationChange{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO app_notifications
(operator_id, operation_id, record_json, version, deleted, updated_at) VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE record_json=VALUES(record_json), version=VALUES(version),
deleted=VALUES(deleted), updated_at=VALUES(updated_at)`,
		operatorID, operationID, recordValue, version, deleted, now)
	if err != nil {
		return model.AppNotificationChange{}, err
	}
	return model.AppNotificationChange{
		Version: version, OperationID: operationID,
		Record: append(json.RawMessage(nil), record...), Deleted: deleted, UpdatedAt: now, Applied: true,
	}, nil
}

type appNotificationRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanAppNotificationChanges(rows appNotificationRows) ([]model.AppNotificationChange, error) {
	items := make([]model.AppNotificationChange, 0)
	for rows.Next() {
		var item model.AppNotificationChange
		var recordJSON []byte
		if err := rows.Scan(&item.Version, &item.OperationID, &recordJSON, &item.Deleted, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Record = append(json.RawMessage(nil), recordJSON...)
		items = append(items, item)
	}
	return items, rows.Err()
}
