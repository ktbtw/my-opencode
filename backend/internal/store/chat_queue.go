package store

import (
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"relay-server/internal/model"
)

const chatQueuePositionStep int64 = 1024

type ChatQueueArchive interface {
	UpsertChatQueueItem(item model.ChatQueueItem) error
	DeleteChatQueueItem(itemID string) error
	ListChatQueueItems(operatorID int64, sessionID string) ([]model.ChatQueueItem, error)
}

func chatQueueKey(operatorID int64, sessionID string) string {
	return fmt.Sprintf("%d:%s", operatorID, strings.TrimSpace(sessionID))
}

func cloneChatQueueItem(item model.ChatQueueItem) model.ChatQueueItem {
	item.Parts = cloneParts(item.Parts)
	item.Metadata = cloneStringMap(item.Metadata)
	return item
}

func (m *Memory) ensureChatQueueLoaded(operatorID int64, sessionID string) {
	key := chatQueueKey(operatorID, sessionID)
	m.mu.RLock()
	loaded := m.chatQueueLoaded[key]
	m.mu.RUnlock()
	if loaded {
		return
	}

	archive, ok := m.archive.(ChatQueueArchive)
	if !ok {
		m.mu.Lock()
		m.chatQueueLoaded[key] = true
		m.mu.Unlock()
		return
	}
	items, err := archive.ListChatQueueItems(operatorID, sessionID)
	if err != nil {
		log.Printf("load chat queue failed: operator=%d session=%s err=%v", operatorID, sessionID, err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.chatQueueLoaded[key] {
		return
	}
	for _, item := range items {
		m.chatQueue[item.ID] = cloneChatQueueItem(item)
	}
	m.chatQueueLoaded[key] = true
}

func (m *Memory) persistChatQueueItem(item model.ChatQueueItem) {
	archive, ok := m.archive.(ChatQueueArchive)
	if !ok {
		return
	}
	if err := archive.UpsertChatQueueItem(cloneChatQueueItem(item)); err != nil {
		log.Printf("persist chat queue item failed: item=%s err=%v", item.ID, err)
	}
}

func (m *Memory) deleteArchivedChatQueueItem(itemID string) {
	archive, ok := m.archive.(ChatQueueArchive)
	if !ok {
		return
	}
	if err := archive.DeleteChatQueueItem(itemID); err != nil {
		log.Printf("delete chat queue item failed: item=%s err=%v", itemID, err)
	}
}

func (m *Memory) chatQueueSnapshotLocked(operatorID int64, sessionID string) model.ChatQueueSnapshot {
	key := chatQueueKey(operatorID, sessionID)
	items := make([]model.ChatQueueItem, 0)
	for _, item := range m.chatQueue {
		if item.OperatorID != operatorID || item.SessionID != sessionID {
			continue
		}
		items = append(items, cloneChatQueueItem(item))
	}
	slices.SortFunc(items, func(a, b model.ChatQueueItem) int {
		if a.Position < b.Position {
			return -1
		}
		if a.Position > b.Position {
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return model.ChatQueueSnapshot{SessionID: sessionID, Version: m.chatQueueVersions[key], Items: items}
}

func (m *Memory) ChatQueueSnapshot(operatorID int64, sessionID string) model.ChatQueueSnapshot {
	m.ensureChatQueueLoaded(operatorID, sessionID)
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.chatQueueSnapshotLocked(operatorID, sessionID)
}

func (m *Memory) publishChatQueue(operatorID int64, sessionID string) {
	snapshot := m.ChatQueueSnapshot(operatorID, sessionID)
	key := chatQueueKey(operatorID, sessionID)
	m.queueSubMu.Lock()
	subs := append([]chan model.ChatQueueSnapshot(nil), m.queueSubscribers[key]...)
	m.queueSubMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- snapshot:
		default:
		}
	}
}

func (m *Memory) SubscribeChatQueue(operatorID int64, sessionID string) (<-chan model.ChatQueueSnapshot, func()) {
	key := chatQueueKey(operatorID, sessionID)
	ch := make(chan model.ChatQueueSnapshot, 8)
	m.queueSubMu.Lock()
	m.queueSubscribers[key] = append(m.queueSubscribers[key], ch)
	m.queueSubMu.Unlock()
	return ch, func() {
		m.queueSubMu.Lock()
		defer m.queueSubMu.Unlock()
		items := m.queueSubscribers[key]
		for i, item := range items {
			if item == ch {
				m.queueSubscribers[key] = append(items[:i], items[i+1:]...)
				break
			}
		}
	}
}

func (m *Memory) CreateChatQueueItem(input model.ChatQueueItem) model.ChatQueueItem {
	m.ensureChatQueueLoaded(input.OperatorID, input.SessionID)
	m.mu.Lock()
	key := chatQueueKey(input.OperatorID, input.SessionID)
	var maxPosition int64
	for _, item := range m.chatQueue {
		if item.OperatorID == input.OperatorID && item.SessionID == input.SessionID && item.Position > maxPosition {
			maxPosition = item.Position
		}
	}
	now := time.Now().UTC()
	input.ID = fmt.Sprintf("queue_%d", time.Now().UnixNano())
	input.Position = maxPosition + chatQueuePositionStep
	input.Status = model.ChatQueueQueued
	input.Version = 1
	input.CreatedAt = now
	input.UpdatedAt = now
	input = cloneChatQueueItem(input)
	m.chatQueue[input.ID] = input
	m.chatQueueVersions[key]++
	m.mu.Unlock()
	m.persistChatQueueItem(input)
	m.publishChatQueue(input.OperatorID, input.SessionID)
	return cloneChatQueueItem(input)
}

func (m *Memory) GetChatQueueItem(operatorID int64, itemID string) (model.ChatQueueItem, bool) {
	m.mu.RLock()
	item, ok := m.chatQueue[itemID]
	m.mu.RUnlock()
	if ok && item.OperatorID == operatorID {
		return cloneChatQueueItem(item), true
	}
	return model.ChatQueueItem{}, false
}

func (m *Memory) UpdateChatQueueItem(operatorID int64, itemID string, expectedVersion int64, update func(*model.ChatQueueItem) error) (model.ChatQueueItem, bool, error) {
	m.mu.Lock()
	item, ok := m.chatQueue[itemID]
	if !ok || item.OperatorID != operatorID {
		m.mu.Unlock()
		return model.ChatQueueItem{}, false, nil
	}
	if expectedVersion > 0 && item.Version != expectedVersion {
		current := cloneChatQueueItem(item)
		m.mu.Unlock()
		return current, true, fmt.Errorf("queue item version conflict")
	}
	next := cloneChatQueueItem(item)
	if err := update(&next); err != nil {
		m.mu.Unlock()
		return model.ChatQueueItem{}, true, err
	}
	next.Version++
	next.UpdatedAt = time.Now().UTC()
	m.chatQueue[itemID] = next
	key := chatQueueKey(next.OperatorID, next.SessionID)
	m.chatQueueVersions[key]++
	m.mu.Unlock()
	m.persistChatQueueItem(next)
	m.publishChatQueue(next.OperatorID, next.SessionID)
	return cloneChatQueueItem(next), true, nil
}

func (m *Memory) DeleteChatQueueItem(operatorID int64, itemID string, expectedVersion int64) (model.ChatQueueItem, bool, error) {
	m.mu.Lock()
	item, ok := m.chatQueue[itemID]
	if !ok || item.OperatorID != operatorID {
		m.mu.Unlock()
		return model.ChatQueueItem{}, false, nil
	}
	if expectedVersion > 0 && item.Version != expectedVersion {
		current := cloneChatQueueItem(item)
		m.mu.Unlock()
		return current, true, fmt.Errorf("queue item version conflict")
	}
	delete(m.chatQueue, itemID)
	key := chatQueueKey(item.OperatorID, item.SessionID)
	m.chatQueueVersions[key]++
	m.mu.Unlock()
	m.deleteArchivedChatQueueItem(itemID)
	m.publishChatQueue(item.OperatorID, item.SessionID)
	return cloneChatQueueItem(item), true, nil
}

func (m *Memory) DeleteChatQueueItemByTask(taskID string) {
	var removed []model.ChatQueueItem
	m.mu.Lock()
	for id, item := range m.chatQueue {
		if item.TaskID != taskID {
			continue
		}
		removed = append(removed, item)
		delete(m.chatQueue, id)
		m.chatQueueVersions[chatQueueKey(item.OperatorID, item.SessionID)]++
	}
	m.mu.Unlock()
	for _, item := range removed {
		m.deleteArchivedChatQueueItem(item.ID)
		m.publishChatQueue(item.OperatorID, item.SessionID)
	}
}

func (m *Memory) ReorderChatQueue(operatorID int64, sessionID string, itemIDs []string, expectedVersions map[string]int64) (model.ChatQueueSnapshot, error) {
	m.ensureChatQueueLoaded(operatorID, sessionID)
	m.mu.Lock()
	queued := make([]model.ChatQueueItem, 0)
	for _, item := range m.chatQueue {
		if item.OperatorID == operatorID && item.SessionID == sessionID && item.Status == model.ChatQueueQueued {
			queued = append(queued, item)
		}
	}
	if len(queued) != len(itemIDs) {
		m.mu.Unlock()
		return model.ChatQueueSnapshot{}, fmt.Errorf("queue order changed")
	}
	seen := make(map[string]bool, len(itemIDs))
	changed := make([]model.ChatQueueItem, 0, len(itemIDs))
	for index, id := range itemIDs {
		item, ok := m.chatQueue[id]
		if !ok || seen[id] || item.OperatorID != operatorID || item.SessionID != sessionID || item.Status != model.ChatQueueQueued {
			m.mu.Unlock()
			return model.ChatQueueSnapshot{}, fmt.Errorf("queue order changed")
		}
		if expected := expectedVersions[id]; expected > 0 && item.Version != expected {
			m.mu.Unlock()
			return model.ChatQueueSnapshot{}, fmt.Errorf("queue item version conflict")
		}
		seen[id] = true
		item.Position = int64(index+1) * chatQueuePositionStep
		item.Version++
		item.UpdatedAt = time.Now().UTC()
		m.chatQueue[id] = item
		changed = append(changed, item)
	}
	key := chatQueueKey(operatorID, sessionID)
	m.chatQueueVersions[key]++
	snapshot := m.chatQueueSnapshotLocked(operatorID, sessionID)
	m.mu.Unlock()
	for _, item := range changed {
		m.persistChatQueueItem(item)
	}
	m.publishChatQueue(operatorID, sessionID)
	return snapshot, nil
}

func (m *Memory) TryBeginChatQueueDispatch(operatorID int64, sessionID string) bool {
	key := chatQueueKey(operatorID, sessionID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.chatQueueDispatching[key] {
		return false
	}
	m.chatQueueDispatching[key] = true
	return true
}

func (m *Memory) EndChatQueueDispatch(operatorID int64, sessionID string) {
	m.mu.Lock()
	delete(m.chatQueueDispatching, chatQueueKey(operatorID, sessionID))
	m.mu.Unlock()
}

func (m *Memory) ClaimNextChatQueueItem(operatorID int64, sessionID string) (model.ChatQueueItem, bool) {
	m.ensureChatQueueLoaded(operatorID, sessionID)
	m.mu.Lock()
	var selected *model.ChatQueueItem
	for _, raw := range m.chatQueue {
		item := raw
		if item.OperatorID != operatorID || item.SessionID != sessionID || item.Status != model.ChatQueueQueued {
			continue
		}
		if selected == nil || item.Position < selected.Position || (item.Position == selected.Position && item.ID < selected.ID) {
			copy := item
			selected = &copy
		}
	}
	if selected == nil {
		m.mu.Unlock()
		return model.ChatQueueItem{}, false
	}
	selected.Status = model.ChatQueueDispatching
	selected.Error = ""
	selected.Version++
	selected.UpdatedAt = time.Now().UTC()
	m.chatQueue[selected.ID] = *selected
	m.chatQueueVersions[chatQueueKey(operatorID, sessionID)]++
	m.mu.Unlock()
	m.persistChatQueueItem(*selected)
	m.publishChatQueue(operatorID, sessionID)
	return cloneChatQueueItem(*selected), true
}

func (m *Memory) ClaimChatQueueItem(operatorID int64, sessionID, itemID string) (model.ChatQueueItem, bool) {
	m.ensureChatQueueLoaded(operatorID, sessionID)
	m.mu.Lock()
	item, ok := m.chatQueue[itemID]
	if !ok || item.OperatorID != operatorID || item.SessionID != sessionID || item.Status != model.ChatQueueQueued {
		m.mu.Unlock()
		return model.ChatQueueItem{}, false
	}
	item.Status = model.ChatQueueDispatching
	item.Error = ""
	item.Version++
	item.UpdatedAt = time.Now().UTC()
	m.chatQueue[itemID] = item
	m.chatQueueVersions[chatQueueKey(operatorID, sessionID)]++
	m.mu.Unlock()
	m.persistChatQueueItem(item)
	m.publishChatQueue(operatorID, sessionID)
	return cloneChatQueueItem(item), true
}
