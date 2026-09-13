package binarymeta

import "testing"

func TestExecutableName(t *testing.T) {
	got := ExecutableName("opencode")
	if got == "" {
		t.Fatal("expected executable name")
	}
}

func TestMatchesExecutableName(t *testing.T) {
	if !MatchesExecutableName(DefaultBinaryName()) {
		t.Fatalf("expected default binary name to match")
	}
	if MatchesExecutableName("other-binary") {
		t.Fatalf("did not expect other binary to match")
	}
}

func TestMatchesLauncherExecutableName(t *testing.T) {
	if !MatchesLauncherExecutableName(LauncherBinaryName()) {
		t.Fatalf("expected launcher binary name to match")
	}
	if !MatchesLauncherExecutableName(GUILauncherBinaryName()) {
		t.Fatalf("expected GUI launcher binary name to match")
	}
	if MatchesLauncherExecutableName("opencode") {
		t.Fatalf("did not expect opencode to match launcher binary")
	}
}
