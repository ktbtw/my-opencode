package model

import (
	"encoding/json"
	"time"
)

// AppNotificationChange is an operator-scoped, versioned notification update.
// Record is kept as JSON so older servers can relay fields added by newer apps.
type AppNotificationChange struct {
	Version     int64           `json:"version"`
	OperationID string          `json:"operation_id"`
	Record      json.RawMessage `json:"record,omitempty"`
	Deleted     bool            `json:"deleted"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Applied     bool            `json:"-"`
}

type AppNotificationChangePage struct {
	Items  []AppNotificationChange `json:"items"`
	Cursor int64                   `json:"cursor"`
}
