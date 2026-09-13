//go:build darwin

package autostart

import "testing"

func TestLaunchAgentProgramArguments(t *testing.T) {
	content, err := launchAgentPlist(Options{
		Executable: "/Users/demo/.my-opencode-launcher/bin/chat-codex-launcher",
		Args:       []string{"--background"},
		Env:        map[string]string{"LAUNCHER_RUNTIME_DIR": "/Users/demo/.my-opencode-launcher"},
	})
	if err != nil {
		t.Fatalf("build plist: %v", err)
	}
	args := launchAgentProgramArguments(content)
	if len(args) != 2 {
		t.Fatalf("expected 2 program args, got %#v", args)
	}
	if args[0] != "/Users/demo/.my-opencode-launcher/bin/chat-codex-launcher" || args[1] != "--background" {
		t.Fatalf("unexpected program args: %#v", args)
	}
}
