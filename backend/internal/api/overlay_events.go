package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"relay-server/internal/model"
)

const overlayNotificationCursorHeader = "X-Overlay-Notification-Cursor"

func (a *API) OverlayEvents(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if a.overlayHub == nil {
		write(w, http.StatusServiceUnavailable, map[string]string{"error": "overlay stream unavailable"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		write(w, http.StatusInternalServerError, map[string]string{"error": "streaming unavailable"})
		return
	}

	cursorHeader := strings.TrimSpace(r.Header.Get(overlayNotificationCursorHeader))
	cursorInitialized := cursorHeader != ""
	notificationCursor, err := parseOverlayNotificationCursor(cursorHeader)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid overlay notification cursor"})
		return
	}
	events, unsubscribe := a.overlayHub.Subscribe(operator.ID)
	defer unsubscribe()
	devices, err := a.listDevicesForOperator(r.Context(), operator.ID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "device snapshot unavailable"})
		return
	}
	if devices == nil {
		devices = []model.Machine{}
	}
	notificationChanges := make([]model.AppNotificationChange, 0)
	if !cursorInitialized {
		page, pageErr := a.store.ListAppNotificationChanges(r.Context(), operator.ID, 0, 1)
		if pageErr != nil {
			write(w, http.StatusInternalServerError, map[string]string{"error": "notification cursor unavailable"})
			return
		}
		notificationCursor = page.Cursor
	} else {
		for {
			page, pageErr := a.store.ListAppNotificationChangeFeed(r.Context(), operator.ID, notificationCursor, 200)
			if pageErr != nil {
				write(w, http.StatusInternalServerError, map[string]string{"error": "notification reconciliation unavailable"})
				return
			}
			notificationChanges = append(notificationChanges, page.Items...)
			if page.Cursor <= notificationCursor || len(page.Items) < 200 {
				notificationCursor = max(notificationCursor, page.Cursor)
				break
			}
			notificationCursor = page.Cursor
		}
	}
	notificationChanges = latestOverlayNotificationChanges(notificationChanges)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	for _, change := range notificationChanges {
		if err := writeOverlaySSE(w, "notification.updated", 0, change); err != nil {
			return
		}
		flusher.Flush()
	}
	if err := writeOverlaySSE(w, "snapshot", 0, map[string]any{
		"devices": devices, "notification_cursor": notificationCursor,
	}); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-events:
			if !open {
				return
			}
			if err := writeOverlaySSE(w, event.Type, event.ID, event.Data); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": heartbeat %d\n\n", time.Now().Unix()); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func latestOverlayNotificationChanges(changes []model.AppNotificationChange) []model.AppNotificationChange {
	latest := make(map[string]model.AppNotificationChange, len(changes))
	for _, change := range changes {
		if current, found := latest[change.OperationID]; !found || change.Version > current.Version {
			latest[change.OperationID] = change
		}
	}
	result := make([]model.AppNotificationChange, 0, len(latest))
	for _, change := range latest {
		result = append(result, change)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result
}

func parseOverlayNotificationCursor(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || cursor < 0 {
		return 0, fmt.Errorf("invalid notification cursor")
	}
	return cursor, nil
}

func writeOverlaySSE(w http.ResponseWriter, eventType string, id uint64, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if id > 0 {
		if _, err = fmt.Fprintf(w, "id: %d\n", id); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, payload)
	return err
}
