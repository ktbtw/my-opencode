package autostart

import (
	"os"
	"runtime"
	"testing"
)

func TestEnableDryRunReturnsInfo(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("executable: %v", err)
	}
	info, err := Enable(Options{Executable: executable, DryRun: true})
	if err != nil {
		t.Fatalf("dry run enable: %v", err)
	}
	if !info.Enabled {
		t.Fatalf("expected enabled dry-run result: %+v", info)
	}
	if info.Method == "" {
		t.Fatalf("expected method for %s: %+v", runtime.GOOS, info)
	}
}

func TestStatusCacheLoadsOnceUntilRefresh(t *testing.T) {
	var cache statusCache
	calls := 0
	load := func() Info {
		calls++
		return Info{Enabled: calls%2 == 1, Method: "test"}
	}

	first := cache.load(load, false)
	second := cache.load(load, false)
	if calls != 1 {
		t.Fatalf("expected one platform status read, got %d", calls)
	}
	if first != second {
		t.Fatalf("expected cached status, got first=%+v second=%+v", first, second)
	}

	refreshed := cache.load(load, true)
	if calls != 2 {
		t.Fatalf("expected explicit refresh to read platform status, got %d calls", calls)
	}
	if refreshed.Enabled == first.Enabled {
		t.Fatalf("expected refreshed status to replace cache: first=%+v refreshed=%+v", first, refreshed)
	}
}

func TestStatusCacheMutationReplacesCachedValue(t *testing.T) {
	var cache statusCache
	cache.load(func() Info { return Info{Enabled: false, Method: "test"} }, false)

	want := Info{Enabled: true, Method: "test", Command: "launcher --background"}
	got, err := cache.mutate(func() (Info, error) { return want, nil }, true)
	if err != nil {
		t.Fatalf("mutate cache: %v", err)
	}
	if got != want || cache.load(func() Info { return Info{} }, false) != want {
		t.Fatalf("expected mutation to update cache: got=%+v", got)
	}
}

func TestCommandUsesUpdaterExecutable(t *testing.T) {
	cases := []struct {
		name     string
		command  string
		expected bool
	}{
		{
			name:     "quoted updater",
			command:  `"C:\Users\Administrator\AppData\Local\my-opencode-launcher\self-updates\updater\launcher-updater-0.1.48.exe" "--background"`,
			expected: true,
		},
		{
			name:     "normal gui launcher",
			command:  `"D:\本地音乐五\chat-codex-launcher.exe" --background`,
			expected: false,
		},
		{
			name:     "normal cli launcher",
			command:  `/opt/chat-codex/launcher --background`,
			expected: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CommandUsesUpdaterExecutable(tc.command); got != tc.expected {
				t.Fatalf("CommandUsesUpdaterExecutable(%q) = %v, want %v", tc.command, got, tc.expected)
			}
		})
	}
}
