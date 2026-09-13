package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAdminWebRootUsesConfiguredDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	t.Setenv("CHAT_CODEX_ADMIN_WEB_DIR", dir)

	if got := adminWebRoot(); got != dir {
		t.Fatalf("unexpected admin web root: %s", got)
	}
}

func TestAdminWebHandlerDisablesCache(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	t.Setenv("CHAT_CODEX_ADMIN_WEB_DIR", dir)

	request := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	recorder := httptest.NewRecorder()
	adminWebHandler().ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("unexpected cache header: %s", got)
	}
}
