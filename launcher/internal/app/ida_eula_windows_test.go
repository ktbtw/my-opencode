//go:build windows

package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func TestPrepareIDAEULAStateWritesCurrentUserRegistry(t *testing.T) {
	valueName := fmt.Sprintf("Launcher EULA test %d", os.Getpid())
	output, err := prepareIDAEULAState("", valueName)
	if err != nil {
		t.Fatal(err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, idaWindowsRegistryPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	defer key.DeleteValue(valueName)
	value, _, err := key.GetIntegerValue(valueName)
	if err != nil {
		t.Fatal(err)
	}
	if value != 1 {
		t.Fatalf("EULA registry value: got %d want 1", value)
	}
	if !strings.Contains(output, valueName+"=1") {
		t.Fatalf("missing registry verification output: %q", output)
	}
}

func TestManagedIDAWindowsIntegration(t *testing.T) {
	idaDir := strings.TrimSpace(os.Getenv("IDA_INTEGRATION_DIR"))
	pythonPath := strings.TrimSpace(os.Getenv("IDA_INTEGRATION_PYTHON"))
	if idaDir == "" || pythonPath == "" {
		t.Skip("set IDA_INTEGRATION_DIR and IDA_INTEGRATION_PYTHON to run")
	}
	preparationOutput, err := prepareIDAEULAState(idaDir, idaEULARegistryKey)
	if err != nil {
		t.Fatalf("prepare EULA state: %v; output=%s", err, preparationOutput)
	}
	ctx, cancel := context.WithTimeout(context.Background(), idaCommandTimeout)
	defer cancel()
	probeOutput, err := runIDAEULAProbe(ctx, pythonPath, idaDir, idaEULARegistryKey)
	if err != nil {
		t.Fatalf("run IDALib probe: %v; output=%s", err, probeOutput)
	}
	for _, marker := range []string{
		"IDA batch license probe: idapro imported",
		"IDA batch license state initialized",
	} {
		if !strings.Contains(probeOutput, marker) {
			t.Fatalf("IDALib probe output missing %q: %s", marker, probeOutput)
		}
	}

	smokeDone := make(chan struct{})
	var smokeOutput string
	var smokeErr error
	go func() {
		smokeOutput, smokeErr = smokeTestIDARuntime(idaDir)
		close(smokeDone)
	}()
	select {
	case <-smokeDone:
		if smokeErr != nil {
			t.Fatalf("run IDA smoke test: %v; output=%s", smokeErr, smokeOutput)
		}
	case <-time.After(idaSmokeTestTimeout + time.Minute):
		t.Fatal("IDA smoke test did not return")
	}
}
