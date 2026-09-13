package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"relay-server/internal/model"
)

type AppNotificationArchive interface {
	UpsertAppNotification(context.Context, int64, string, json.RawMessage) (*model.AppNotificationChange, error)
	ListAppNotificationChanges(context.Context, int64, int64, int) (model.AppNotificationChangePage, error)
	ListAppNotificationChangeFeed(context.Context, int64, int64, int) (model.AppNotificationChangePage, error)
	MarkAppNotificationsRead(context.Context, int64, []string) ([]model.AppNotificationChange, error)
	DismissAppNotifications(context.Context, int64, []string) ([]model.AppNotificationChange, error)
}

func cloneAppNotificationChange(change model.AppNotificationChange) model.AppNotificationChange {
	out := change
	out.Record = append(json.RawMessage(nil), change.Record...)
	return out
}

func appNotificationKey(operatorID int64, operationID string) string {
	return strconv.FormatInt(operatorID, 10) + "\x00" + strings.TrimSpace(operationID)
}

func (m *Memory) appNotificationArchive() (AppNotificationArchive, bool) {
	archive, ok := m.archive.(AppNotificationArchive)
	return archive, ok
}

func (m *Memory) UpsertAppNotification(
	ctx context.Context,
	operatorID int64,
	operationID string,
	record json.RawMessage,
) (*model.AppNotificationChange, error) {
	m.appNotificationWriteMu.Lock()
	defer m.appNotificationWriteMu.Unlock()
	if archive, ok := m.appNotificationArchive(); ok {
		change, err := archive.UpsertAppNotification(ctx, operatorID, operationID, record)
		if err == nil && change != nil && change.Applied {
			m.publishAppNotificationChange(operatorID, *change)
		}
		return change, err
	}
	operationID = strings.TrimSpace(operationID)
	if operatorID <= 0 || operationID == "" || len(record) == 0 || !json.Valid(record) {
		return nil, errors.New("invalid app notification")
	}
	m.mu.Lock()
	if current, found := m.appNotifications[appNotificationKey(operatorID, operationID)]; found && !current.Deleted {
		currentAt, currentOK := appNotificationRecordUpdatedAt(current.Record)
		incomingAt, incomingOK := appNotificationRecordUpdatedAt(record)
		if currentOK && incomingOK && incomingAt.Before(currentAt) {
			out := cloneAppNotificationChange(current)
			m.mu.Unlock()
			return &out, nil
		}
	}
	m.appNotificationVersions[operatorID]++
	change := model.AppNotificationChange{
		Version:     m.appNotificationVersions[operatorID],
		OperationID: operationID,
		Record:      append(json.RawMessage(nil), record...),
		UpdatedAt:   time.Now().UTC(),
		Applied:     true,
	}
	m.appNotifications[appNotificationKey(operatorID, operationID)] = cloneAppNotificationChange(change)
	m.appNotificationChanges[operatorID] = append(m.appNotificationChanges[operatorID], cloneAppNotificationChange(change))
	out := cloneAppNotificationChange(change)
	m.mu.Unlock()
	m.publishAppNotificationChange(operatorID, out)
	return &out, nil
}

func (m *Memory) ListAppNotificationChangeFeed(
	ctx context.Context,
	operatorID int64,
	after int64,
	limit int,
) (model.AppNotificationChangePage, error) {
	if archive, ok := m.appNotificationArchive(); ok {
		return archive.ListAppNotificationChangeFeed(ctx, operatorID, after, limit)
	}
	limit = normalizeAppNotificationLimit(limit)
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]model.AppNotificationChange, 0, limit)
	cursor := after
	for _, change := range m.appNotificationChanges[operatorID] {
		if change.Version <= after {
			continue
		}
		items = append(items, cloneAppNotificationChange(change))
		cursor = change.Version
		if len(items) == limit {
			break
		}
	}
	return model.AppNotificationChangePage{Items: items, Cursor: cursor}, nil
}

func (m *Memory) SetAppNotificationObserver(observer func(int64, model.AppNotificationChange)) {
	m.appNotificationObserverMu.Lock()
	m.appNotificationObserver = observer
	m.appNotificationObserverMu.Unlock()
}

func (m *Memory) publishAppNotificationChange(operatorID int64, change model.AppNotificationChange) {
	m.appNotificationObserverMu.RLock()
	observer := m.appNotificationObserver
	m.appNotificationObserverMu.RUnlock()
	if observer != nil {
		observer(operatorID, cloneAppNotificationChange(change))
	}
}

func appNotificationRecordUpdatedAt(record json.RawMessage) (time.Time, bool) {
	if len(record) == 0 {
		return time.Time{}, false
	}
	var value struct {
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.Unmarshal(record, &value); err != nil {
		return time.Time{}, false
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value.UpdatedAt))
	if err != nil {
		return time.Time{}, false
	}
	return updatedAt, true
}

func (m *Memory) ListAppNotificationChanges(
	ctx context.Context,
	operatorID int64,
	after int64,
	limit int,
) (model.AppNotificationChangePage, error) {
	if archive, ok := m.appNotificationArchive(); ok {
		return archive.ListAppNotificationChanges(ctx, operatorID, after, limit)
	}
	limit = normalizeAppNotificationLimit(limit)
	m.mu.RLock()
	defer m.mu.RUnlock()
	cursor := m.appNotificationVersions[operatorID]
	items := make([]model.AppNotificationChange, 0, limit)
	if after <= 0 {
		prefix := strconv.FormatInt(operatorID, 10) + "\x00"
		for key, change := range m.appNotifications {
			if !strings.HasPrefix(key, prefix) || change.Deleted {
				continue
			}
			items = append(items, cloneAppNotificationChange(change))
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Version > items[j].Version })
		if len(items) > limit {
			items = items[:limit]
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Version < items[j].Version })
		return model.AppNotificationChangePage{Items: items, Cursor: cursor}, nil
	}
	for _, change := range m.appNotificationChanges[operatorID] {
		if change.Version <= after {
			continue
		}
		items = append(items, cloneAppNotificationChange(change))
		if len(items) == limit {
			break
		}
	}
	if len(items) > 0 {
		cursor = items[len(items)-1].Version
	} else {
		cursor = after
	}
	return model.AppNotificationChangePage{Items: items, Cursor: cursor}, nil
}

func (m *Memory) MarkAppNotificationsRead(
	ctx context.Context,
	operatorID int64,
	operationIDs []string,
) ([]model.AppNotificationChange, error) {
	m.appNotificationWriteMu.Lock()
	defer m.appNotificationWriteMu.Unlock()
	if archive, ok := m.appNotificationArchive(); ok {
		changes, err := archive.MarkAppNotificationsRead(ctx, operatorID, operationIDs)
		if err == nil {
			for _, change := range changes {
				m.publishAppNotificationChange(operatorID, change)
			}
		}
		return changes, err
	}
	m.mu.Lock()
	changes := make([]model.AppNotificationChange, 0, len(operationIDs))
	for _, operationID := range uniqueAppNotificationIDs(operationIDs) {
		key := appNotificationKey(operatorID, operationID)
		current, found := m.appNotifications[key]
		if !found || current.Deleted || len(current.Record) == 0 {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(current.Record, &record); err != nil {
			continue
		}
		if unread, ok := record["unread"].(bool); ok && !unread {
			continue
		}
		record["unread"] = false
		encoded, err := json.Marshal(record)
		if err != nil {
			m.mu.Unlock()
			return nil, err
		}
		change := m.appendAppNotificationChangeLocked(operatorID, operationID, encoded, false)
		m.appNotifications[key] = cloneAppNotificationChange(change)
		changes = append(changes, cloneAppNotificationChange(change))
	}
	m.mu.Unlock()
	for _, change := range changes {
		m.publishAppNotificationChange(operatorID, change)
	}
	return changes, nil
}

func (m *Memory) DismissAppNotifications(
	ctx context.Context,
	operatorID int64,
	operationIDs []string,
) ([]model.AppNotificationChange, error) {
	m.appNotificationWriteMu.Lock()
	defer m.appNotificationWriteMu.Unlock()
	if archive, ok := m.appNotificationArchive(); ok {
		changes, err := archive.DismissAppNotifications(ctx, operatorID, operationIDs)
		if err == nil {
			for _, change := range changes {
				m.publishAppNotificationChange(operatorID, change)
			}
		}
		return changes, err
	}
	m.mu.Lock()
	changes := make([]model.AppNotificationChange, 0, len(operationIDs))
	for _, operationID := range uniqueAppNotificationIDs(operationIDs) {
		key := appNotificationKey(operatorID, operationID)
		if current, found := m.appNotifications[key]; found && current.Deleted {
			continue
		}
		change := m.appendAppNotificationChangeLocked(operatorID, operationID, nil, true)
		m.appNotifications[key] = cloneAppNotificationChange(change)
		changes = append(changes, cloneAppNotificationChange(change))
	}
	m.mu.Unlock()
	for _, change := range changes {
		m.publishAppNotificationChange(operatorID, change)
	}
	return changes, nil
}

func (m *Memory) appendAppNotificationChangeLocked(
	operatorID int64,
	operationID string,
	record json.RawMessage,
	deleted bool,
) model.AppNotificationChange {
	m.appNotificationVersions[operatorID]++
	change := model.AppNotificationChange{
		Version:     m.appNotificationVersions[operatorID],
		OperationID: operationID,
		Record:      append(json.RawMessage(nil), record...),
		Deleted:     deleted,
		UpdatedAt:   time.Now().UTC(),
		Applied:     true,
	}
	m.appNotificationChanges[operatorID] = append(m.appNotificationChanges[operatorID], cloneAppNotificationChange(change))
	return change
}

func normalizeAppNotificationLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func uniqueAppNotificationIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if len(out) == 100 {
			break
		}
	}
	return out
}
