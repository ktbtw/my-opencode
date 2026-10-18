package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

type diagLogRequest struct {
	Event string         `json:"event"`
	Data  map[string]any `json:"data"`
}

// ReportDiagLog 接收 App 端的关键诊断事件，直接写入服务端日志，
// 便于在没有 App 本地日志的情况下排查通知/轮询类问题。
func (a *API) ReportDiagLog(w http.ResponseWriter, r *http.Request) {
	var req diagLogRequest
	if err := decodeBoundedJSON(w, r, &req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid diag log"})
		return
	}
	event := strings.TrimSpace(req.Event)
	if event == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "missing event"})
		return
	}
	encoded, _ := json.Marshal(req.Data)
	payload := string(encoded)
	if len(payload) > 800 {
		payload = payload[:800] + "…"
	}
	log.Printf("[appdiag] %s %s", event, payload)
	write(w, http.StatusOK, map[string]bool{"ok": true})
}
