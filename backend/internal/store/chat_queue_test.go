package store

import (
	"strings"
	"sync"
	"testing"

	"relay-server/internal/model"
)

func queueItem(operatorID int64, sessionID, text string) model.ChatQueueItem {
	return model.ChatQueueItem{
		OperatorID: operatorID,
		SessionID:  sessionID,
		AgentID:    "agent-1",
		MachineID:  "machine-1",
		ProjectID:  "project-1",
		Parts:      []model.Part{{Type: "text", Text: text}},
	}
}

func TestChatQueueReorderAndVersionConflicts(t *testing.T) {
	mem := NewMemory(nil)
	first := mem.CreateChatQueueItem(queueItem(7, "session-1", "first"))
	second := mem.CreateChatQueueItem(queueItem(7, "session-1", "second"))

	snapshot, err := mem.ReorderChatQueue(7, "session-1", []string{second.ID, first.ID}, map[string]int64{
		first.ID:  first.Version,
		second.ID: second.Version,
	})
	if err != nil {
		t.Fatalf("reorder queue: %v", err)
	}
	if len(snapshot.Items) != 2 || snapshot.Items[0].ID != second.ID || snapshot.Items[1].ID != first.ID {
		t.Fatalf("unexpected queue order: %+v", snapshot.Items)
	}
	if snapshot.Items[0].Version != second.Version+1 || snapshot.Items[1].Version != first.Version+1 {
		t.Fatalf("reorder did not advance item versions: %+v", snapshot.Items)
	}

	_, found, err := mem.UpdateChatQueueItem(7, first.ID, first.Version, func(item *model.ChatQueueItem) error {
		item.Metadata = map[string]string{"model": "provider/stale"}
		return nil
	})
	if !found || err == nil || !strings.Contains(err.Error(), "version conflict") {
		t.Fatalf("expected stale update conflict, found=%v err=%v", found, err)
	}
	latest, ok := mem.GetChatQueueItem(7, first.ID)
	if !ok || latest.Metadata["model"] != "" {
		t.Fatalf("stale update mutated queue item: %+v", latest)
	}

	_, err = mem.ReorderChatQueue(7, "session-1", []string{first.ID, second.ID}, map[string]int64{
		first.ID:  first.Version,
		second.ID: second.Version,
	})
	if err == nil || !strings.Contains(err.Error(), "version conflict") {
		t.Fatalf("expected stale reorder conflict, got %v", err)
	}
}

func TestChatQueueDispatchClaimIsSerializedAndOrdered(t *testing.T) {
	mem := NewMemory(nil)
	first := mem.CreateChatQueueItem(queueItem(9, "session-2", "first"))
	second := mem.CreateChatQueueItem(queueItem(9, "session-2", "second"))

	const contenders = 16
	var wg sync.WaitGroup
	winners := make(chan bool, contenders)
	for range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			winners <- mem.TryBeginChatQueueDispatch(9, "session-2")
		}()
	}
	wg.Wait()
	close(winners)
	winnerCount := 0
	for winner := range winners {
		if winner {
			winnerCount++
		}
	}
	if winnerCount != 1 {
		t.Fatalf("expected exactly one dispatch owner, got %d", winnerCount)
	}

	claimed, ok := mem.ClaimNextChatQueueItem(9, "session-2")
	if !ok || claimed.ID != first.ID || claimed.Status != model.ChatQueueDispatching {
		t.Fatalf("expected first item to be claimed, got %+v", claimed)
	}
	mem.EndChatQueueDispatch(9, "session-2")
	if !mem.TryBeginChatQueueDispatch(9, "session-2") {
		t.Fatal("dispatch ownership was not released")
	}
	next, ok := mem.ClaimNextChatQueueItem(9, "session-2")
	if !ok || next.ID != second.ID {
		t.Fatalf("expected second queued item, got %+v", next)
	}
}

func TestChatQueueSnapshotsCloneMutableFields(t *testing.T) {
	mem := NewMemory(nil)
	item := queueItem(11, "session-3", "original")
	item.Metadata = map[string]string{"model": "provider/model-a"}
	created := mem.CreateChatQueueItem(item)

	snapshot := mem.ChatQueueSnapshot(11, "session-3")
	snapshot.Items[0].Parts[0].Text = "mutated"
	snapshot.Items[0].Metadata["model"] = "provider/model-b"

	stored, ok := mem.GetChatQueueItem(11, created.ID)
	if !ok || stored.Parts[0].Text != "original" || stored.Metadata["model"] != "provider/model-a" {
		t.Fatalf("snapshot mutation leaked into store: %+v", stored)
	}
}
