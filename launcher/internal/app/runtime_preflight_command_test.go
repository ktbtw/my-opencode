package app

import "testing"

func TestIsWindowsCommandScript(t *testing.T) {
	for _, path := range []string{`C:\node\npm.cmd`, `C:\node\npx.BAT`} {
		if !isWindowsCommandScript(path) {
			t.Fatalf("expected Windows command script: %s", path)
		}
	}
	if isWindowsCommandScript(`C:\node\node.exe`) {
		t.Fatal("node.exe should still use the background version probe")
	}
}
