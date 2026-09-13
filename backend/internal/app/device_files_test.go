package app

import (
	"testing"
	"time"
)

func TestDirectoryRequestTimeoutUsesLongerWindowForFileTransfer(t *testing.T) {
	for _, envType := range []string{
		"device.project_files.upload",
		"device.project_files.download",
		"device.project_files.create_file",
		"device.project_files.mkdir",
		"device.project_files.delete",
		"device.project_files.rename",
	} {
		if got := directoryRequestTimeout(envType); got != 2*time.Minute {
			t.Fatalf("expected %s timeout 2m, got %s", envType, got)
		}
	}
	if got := directoryRequestTimeout("device.project_files.list"); got != 20*time.Second {
		t.Fatalf("expected list timeout 20s, got %s", got)
	}
}

func TestDeviceWebSocketReadLimitAllowsFileTransferPayloads(t *testing.T) {
	if deviceWebSocketReadLimit < 32<<20 {
		t.Fatalf("expected device websocket read limit to support large file payloads, got %d", deviceWebSocketReadLimit)
	}
}
