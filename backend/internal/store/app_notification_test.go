package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"relay-server/internal/model"
)

func TestAppNotificationVersioningIsolationAndTombstones(t *testing.T) {
	storage := NewMemory(nil)
	ctx := context.Background()
	first := json.RawMessage(`{"operation_id":"agent:1","unread":true,"message":"first"}`)
	second := json.RawMessage(`{"operation_id":"agent:1","unread":true,"message":"second"}`)
	other := json.RawMessage(`{"operation_id":"agent:1","unread":true,"message":"other"}`)

	change1, err := storage.UpsertAppNotification(ctx, 1, "agent:1", first)
	if err != nil {
		t.Fatal(err)
	}
	change2, err := storage.UpsertAppNotification(ctx, 1, "agent:1", second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.UpsertAppNotification(ctx, 2, "agent:1", other); err != nil {
		t.Fatal(err)
	}
	if change1.Version != 1 || change2.Version != 2 {
		t.Fatalf("unexpected versions: %d, %d", change1.Version, change2.Version)
	}

	initial, err := storage.ListAppNotificationChanges(ctx, 1, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Cursor != 2 || len(initial.Items) != 1 || string(initial.Items[0].Record) != string(second) {
		t.Fatalf("unexpected initial page: %+v", initial)
	}
	otherPage, err := storage.ListAppNotificationChanges(ctx, 2, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherPage.Items) != 1 || string(otherPage.Items[0].Record) != string(other) {
		t.Fatalf("operator data leaked or missing: %+v", otherPage)
	}

	readChanges, err := storage.MarkAppNotificationsRead(ctx, 1, []string{"agent:1", "agent:1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(readChanges) != 1 || readChanges[0].Version != 3 {
		t.Fatalf("unexpected read changes: %+v", readChanges)
	}
	var readRecord map[string]any
	if err := json.Unmarshal(readChanges[0].Record, &readRecord); err != nil {
		t.Fatal(err)
	}
	if readRecord["unread"] != false {
		t.Fatalf("notification was not marked read: %+v", readRecord)
	}

	dismissed, err := storage.DismissAppNotifications(ctx, 1, []string{"agent:1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(dismissed) != 1 || !dismissed[0].Deleted || dismissed[0].Version != 4 {
		t.Fatalf("unexpected tombstone: %+v", dismissed)
	}
	dismissedAgain, err := storage.DismissAppNotifications(ctx, 1, []string{"agent:1"})
	if err != nil || len(dismissedAgain) != 0 {
		t.Fatalf("repeated dismiss created another change: %+v err=%v", dismissedAgain, err)
	}
	incremental, err := storage.ListAppNotificationChanges(ctx, 1, 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	if incremental.Cursor != 4 || len(incremental.Items) != 2 || !incremental.Items[1].Deleted {
		t.Fatalf("unexpected incremental page: %+v", incremental)
	}
	emptyInitial, err := storage.ListAppNotificationChanges(ctx, 1, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(emptyInitial.Items) != 0 || emptyInitial.Cursor != 4 {
		t.Fatalf("dismissed notification returned in initial state: %+v", emptyInitial)
	}
	feed, err := storage.ListAppNotificationChangeFeed(ctx, 1, 0, 100)
	if err != nil || len(feed.Items) != 4 || feed.Cursor != 4 {
		t.Fatalf("zero cursor did not replay full change feed: %+v err=%v", feed, err)
	}
}

func TestAppNotificationObserverReceivesConcurrentWritesInVersionOrder(t *testing.T) {
	storage := NewMemory(nil)
	const count = 40
	observed := make(chan int64, count)
	storage.SetAppNotificationObserver(func(operatorID int64, change model.AppNotificationChange) {
		if operatorID != 7 {
			t.Errorf("unexpected operator: %d", operatorID)
		}
		observed <- change.Version
	})
	var group sync.WaitGroup
	for index := 0; index < count; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			record := json.RawMessage(fmt.Sprintf(`{"operation_id":"task:%d","message":"done"}`, index))
			if _, err := storage.UpsertAppNotification(context.Background(), 7, fmt.Sprintf("task:%d", index), record); err != nil {
				t.Errorf("upsert notification %d: %v", index, err)
			}
		}(index)
	}
	group.Wait()
	for want := int64(1); want <= count; want++ {
		select {
		case got := <-observed:
			if got != want {
				t.Fatalf("notification versions out of order: want=%d got=%d", want, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing notification version %d", want)
		}
	}
}

func TestAppNotificationRejectsOlderRecord(t *testing.T) {
	storage := NewMemory(nil)
	ctx := context.Background()
	observed := 0
	storage.SetAppNotificationObserver(func(int64, model.AppNotificationChange) { observed++ })
	newer := json.RawMessage(`{"operation_id":"agent:1","id":"new","updated_at":"2026-08-26T10:00:00Z","status":"succeeded"}`)
	older := json.RawMessage(`{"operation_id":"agent:1","id":"old","updated_at":"2026-08-26T09:59:00Z","status":"running"}`)

	first, err := storage.UpsertAppNotification(ctx, 1, "agent:1", newer)
	if err != nil {
		t.Fatal(err)
	}
	second, err := storage.UpsertAppNotification(ctx, 1, "agent:1", older)
	if err != nil {
		t.Fatal(err)
	}
	if second.Version != first.Version || string(second.Record) != string(newer) {
		t.Fatalf("older record replaced the newer record: first=%+v second=%+v", first, second)
	}
	page, err := storage.ListAppNotificationChanges(ctx, 1, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || !page.Items[0].UpdatedAt.After(time.Time{}) {
		t.Fatalf("unexpected notification history: %+v", page.Items)
	}
	if observed != 1 {
		t.Fatalf("older record published a synthetic change: observed=%d", observed)
	}
}
