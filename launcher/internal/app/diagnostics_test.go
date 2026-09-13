package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"launcher/internal/model"
)

func TestTailDiagnosticFileLimitsLinesAndRedactsSecrets(t *testing.T) {
	runtimeDir := t.TempDir()
	path := filepath.Join(runtimeDir, "agents", "agent_test", "stderr.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		"first",
		"path=" + runtimeDir,
		"VERIFY_API_TOKEN=vat_fixture_secret",
		"last",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := tailDiagnosticFile(path, 3, runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "first") {
		t.Fatalf("line limit was not applied: %s", joined)
	}
	if strings.Contains(joined, runtimeDir) || strings.Contains(joined, "vat_fixture_secret") {
		t.Fatalf("diagnostic output leaked path or token: %s", joined)
	}
	for _, expected := range []string{"<RUNTIME_DIR>", "VERIFY_API_TOKEN=<redacted>", "last"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("diagnostic output missing %q: %s", expected, joined)
		}
	}
}

func TestRedactRuntimeDiagnosticsDropsEnvironment(t *testing.T) {
	runtimeDir := t.TempDir()
	info := redactRuntimeDiagnostics(model.RuntimeEnvironmentInfo{
		RuntimeDir:  runtimeDir,
		Environment: map[string]string{"VERIFY_API_TOKEN": "secret_value"},
		Tools: []model.RuntimeToolStatusInfo{{
			Name: "python", Path: filepath.Join(runtimeDir, "python"), Status: "available",
		}},
		Logs: []string{"VERIFY_API_TOKEN=secret_value"},
	}, runtimeDir)
	if info.Environment != nil {
		t.Fatalf("environment must not be included in remote diagnostics: %+v", info.Environment)
	}
	if strings.Contains(info.Tools[0].Path, runtimeDir) || strings.Contains(info.Logs[0], "secret_value") {
		t.Fatalf("runtime diagnostics were not redacted: %+v", info)
	}
}
