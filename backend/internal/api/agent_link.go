package api

import (
	"github.com/go-chi/chi/v5"
	"net/http"
)

func (a *API) AgentLinkLatency(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	sample, err := a.broker.AgentLinkLatency(r.Context(), operator.ID, chi.URLParam(r, "agentID"))
	if err != nil {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, http.StatusOK, sample)
}
