package app

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"launcher/internal/config"
	"launcher/internal/model"
)

func TestSemanticPreflightsAreSerializedPerLauncher(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	runtimeDir := t.TempDir()
	agentID := "agent_serial_preflight"
	if err := writeAgentConfig(filepath.Join(runtimeDir, "agents", agentID), model.AgentConfig{
		AgentID:    agentID,
		ProjectDir: runtimeDir,
	}); err != nil {
		t.Fatalf("write agent config: %v", err)
	}
	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			Relay: config.RelayConfig{
				URL:         "wss://example.com/ws/device",
				OperatorKey: "opk_test",
				MachineID:   "m_test",
			},
			Agent: config.AgentConfig{Env: map[string]string{}},
		},
		agents: []model.Agent{{AgentID: agentID, Enabled: true, Status: "running"}},
	}
	firstProfile := model.SemanticAgentProfile{ID: "serial-first", Name: "first", Enabled: true}
	secondProfile := model.SemanticAgentProfile{ID: "serial-second", Name: "second", Enabled: true}

	firstReporterEntered := make(chan struct{})
	releaseFirstReporter := make(chan struct{})
	firstDone := make(chan error, 1)
	var firstOnce sync.Once
	go func() {
		firstDone <- svc.StartAgentSemanticPreflight(preflightPayloadForSerialTest("job_serial_first", agentID, firstProfile), func(model.RuntimePreflightStatusPayload) error {
			firstOnce.Do(func() {
				close(firstReporterEntered)
				<-releaseFirstReporter
			})
			return nil
		})
	}()
	select {
	case <-firstReporterEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("first preflight reporter did not start")
	}

	secondReporterEntered := make(chan struct{})
	secondReporterStatuses := make(chan model.RuntimePreflightStatusPayload, 64)
	secondDone := make(chan error, 1)
	var secondOnce sync.Once
	go func() {
		secondDone <- svc.StartAgentSemanticPreflight(preflightPayloadForSerialTest("job_serial_second", agentID, secondProfile), func(status model.RuntimePreflightStatusPayload) error {
			secondReporterStatuses <- status
			secondOnce.Do(func() { close(secondReporterEntered) })
			return nil
		})
	}()
	select {
	case <-secondReporterEntered:
		select {
		case status := <-secondReporterStatuses:
			if len(status.Events) != 1 || status.Events[0].Phase != "queued" {
				t.Fatalf("unexpected queued status: %+v", status)
			}
		case <-time.After(time.Second):
			t.Fatal("second preflight did not report its queued state")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("second preflight did not report its queued state")
	}

	close(releaseFirstReporter)
	for name, done := range map[string]<-chan error{"first": firstDone, "second": secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s preflight: %v", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s preflight did not finish", name)
		}
	}
	select {
	case <-secondReporterEntered:
	default:
		t.Fatal("second preflight never started after the first preflight completed")
	}
}

func preflightPayloadForSerialTest(jobID string, agentID string, profile model.SemanticAgentProfile) model.RuntimePreflightStartPayload {
	return model.RuntimePreflightStartPayload{
		JobID:           jobID,
		MachineID:       "m_test",
		LauncherAgentID: agentID,
		SemanticAgentID: profile.ID,
		SemanticAgent:   &profile,
	}
}
