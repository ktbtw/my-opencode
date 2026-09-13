package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxAppNotificationBodyBytes = 64 << 10

type appNotificationUpsertRequest struct {
	Record json.RawMessage `json:"record"`
}

type appNotificationIDsRequest struct {
	OperationIDs []string `json:"operation_ids"`
}

type appNotificationValidationRecord struct {
	ID           string            `json:"id"`
	OperationID  string            `json:"operation_id"`
	Title        string            `json:"title"`
	Message      string            `json:"message"`
	Status       string            `json:"status"`
	ProgressMode string            `json:"progress_mode"`
	Kind         string            `json:"kind"`
	Scope        string            `json:"scope"`
	Stages       []json.RawMessage `json:"stages"`
	Actions      []json.RawMessage `json:"actions"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`
	Attention    string            `json:"attention"`
}

func (a *API) ListAppNotificationChanges(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	after, err := parseNonNegativeInt64(r.URL.Query().Get("after"))
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid notification cursor"})
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value <= 0 || value > 200 {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid notification limit"})
			return
		}
		limit = value
	}
	page, err := a.store.ListAppNotificationChanges(r.Context(), operator.ID, after, limit)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "list notifications failed"})
		return
	}
	write(w, http.StatusOK, page)
}

func (a *API) UpsertAppNotification(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req appNotificationUpsertRequest
	if err := decodeBoundedJSON(w, r, &req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid notification"})
		return
	}
	operationID, ok := validateSyncedAppNotification(req.Record)
	if !ok {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid synced notification"})
		return
	}
	change, err := a.store.UpsertAppNotification(r.Context(), operator.ID, operationID, req.Record)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "save notification failed"})
		return
	}
	write(w, http.StatusOK, map[string]any{"change": change})
}

func (a *API) MarkAppNotificationsRead(w http.ResponseWriter, r *http.Request) {
	a.mutateAppNotifications(w, r, false)
}

func (a *API) DismissAppNotifications(w http.ResponseWriter, r *http.Request) {
	a.mutateAppNotifications(w, r, true)
}

func (a *API) mutateAppNotifications(w http.ResponseWriter, r *http.Request, dismiss bool) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req appNotificationIDsRequest
	if err := decodeBoundedJSON(w, r, &req); err != nil || !validAppNotificationIDs(req.OperationIDs) {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid notification operation IDs"})
		return
	}
	var (
		changes any
		err     error
	)
	if dismiss {
		changes, err = a.store.DismissAppNotifications(r.Context(), operator.ID, req.OperationIDs)
	} else {
		changes, err = a.store.MarkAppNotificationsRead(r.Context(), operator.ID, req.OperationIDs)
	}
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "update notifications failed"})
		return
	}
	write(w, http.StatusOK, map[string]any{"changes": changes})
}

func decodeBoundedJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxAppNotificationBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return io.ErrUnexpectedEOF
		}
		return err
	}
	return nil
}

func parseNonNegativeInt64(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, strconv.ErrSyntax
	}
	return value, nil
}

func validateSyncedAppNotification(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || len(raw) > maxAppNotificationBodyBytes || !json.Valid(raw) {
		return "", false
	}
	var record appNotificationValidationRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return "", false
	}
	record.OperationID = strings.TrimSpace(record.OperationID)
	if !validAppNotificationID(record.OperationID) || strings.TrimSpace(record.ID) == "" || len(record.ID) > 255 {
		return "", false
	}
	if record.Scope != "synced" || strings.TrimSpace(record.Title) == "" ||
		utf8.RuneCountInString(record.Title) > 200 || utf8.RuneCountInString(record.Message) > 2000 {
		return "", false
	}
	if !oneOf(record.Status, "pending", "running", "waitingSync", "succeeded", "failed", "cancelled") ||
		!oneOf(record.ProgressMode, "none", "determinate", "indeterminate") ||
		!oneOf(record.Kind, "agent", "mcp", "installation", "file", "system") ||
		(record.Attention != "" && !oneOf(record.Attention, "none", "critical", "userAction")) {
		return "", false
	}
	if len(record.Stages) > 12 || len(record.Actions) > 4 {
		return "", false
	}
	if _, err := time.Parse(time.RFC3339Nano, record.CreatedAt); err != nil {
		return "", false
	}
	if _, err := time.Parse(time.RFC3339Nano, record.UpdatedAt); err != nil {
		return "", false
	}
	return record.OperationID, true
}

func validAppNotificationIDs(values []string) bool {
	if len(values) == 0 || len(values) > 100 {
		return false
	}
	for _, value := range values {
		if !validAppNotificationID(strings.TrimSpace(value)) {
			return false
		}
	}
	return true
}

func validAppNotificationID(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > 512 {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
