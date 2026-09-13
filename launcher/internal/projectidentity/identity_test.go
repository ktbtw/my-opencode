package projectidentity

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestResolveRetainsIdentityAcrossRename(t *testing.T) {
	runtimeDir := t.TempDir()
	parent := t.TempDir()
	root := filepath.Join(parent, "before")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := Resolve(runtimeDir, "machine-a", root)
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(parent, "after")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(runtimeDir, "machine-a", moved)
	if err != nil {
		t.Fatal(err)
	}
	if second.ScopeID != first.ScopeID || second.InstanceNonce != first.InstanceNonce {
		t.Fatalf("rename changed identity: first=%+v second=%+v", first, second)
	}
}

func TestResolveRetainsCrossVolumeMoveAndForksReturningOldLocation(t *testing.T) {
	runtimeDir := t.TempDir()
	parent := t.TempDir()
	original := filepath.Join(parent, "original")
	moved := filepath.Join(parent, "moved")
	if err := os.Mkdir(original, 0o755); err != nil {
		t.Fatal(err)
	}
	identities := map[string][2]string{
		filepath.Clean(original): {"volume-a", "file-a"},
		filepath.Clean(moved):    {"volume-b", "file-b"},
	}
	identify := func(root string) (string, string, string, error) {
		root, _ = filepath.Abs(root)
		if _, err := os.Stat(root); err != nil {
			return "", "", "", err
		}
		value, found := identities[filepath.Clean(root)]
		if !found {
			return "", "", "", errors.New("missing synthetic filesystem identity")
		}
		return filepath.Clean(root), value[0], value[1], nil
	}
	first, err := resolveUnlockedWithDirectoryIdentity(runtimeDir, "machine-a", original, identify)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	second, err := resolveUnlockedWithDirectoryIdentity(runtimeDir, "machine-a", moved, identify)
	if err != nil {
		t.Fatal(err)
	}
	if second.ScopeID != first.ScopeID || second.InstanceNonce != first.InstanceNonce {
		t.Fatalf("cross-volume move changed identity: first=%+v second=%+v", first, second)
	}
	marker, err := os.ReadFile(filepath.Join(moved, ".chat-codex", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(original, ".chat-codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, ".chat-codex", "project.json"), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	returned, err := resolveUnlockedWithDirectoryIdentity(runtimeDir, "machine-a", original, identify)
	if err != nil {
		t.Fatal(err)
	}
	if returned.ScopeID == first.ScopeID || returned.LineageScopeID != first.ScopeID {
		t.Fatalf("returning old location did not fork deterministically: first=%+v returned=%+v", first, returned)
	}
}

func TestResolveUsesPathRegistryWithUnstableFilesystemIDs(t *testing.T) {
	runtimeDir := t.TempDir()
	root := t.TempDir()
	identify := func(value string) (string, string, string, error) {
		value, _ = filepath.Abs(value)
		return filepath.Clean(value), "", "", nil
	}
	first, err := resolveUnlockedWithDirectoryIdentity(runtimeDir, "machine-a", root, identify)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".chat-codex", "project.json")); err != nil {
		t.Fatal(err)
	}
	second, err := resolveUnlockedWithDirectoryIdentity(runtimeDir, "machine-a", root, identify)
	if err != nil {
		t.Fatal(err)
	}
	if second.ScopeID != first.ScopeID || second.InstanceNonce != first.InstanceNonce {
		t.Fatalf("unstable filesystem IDs changed path-bound identity: first=%+v second=%+v", first, second)
	}
}

func TestResolveForksCopiedDirectoryOnSameMachine(t *testing.T) {
	runtimeDir := t.TempDir()
	parent := t.TempDir()
	original := filepath.Join(parent, "original")
	copyRoot := filepath.Join(parent, "copy")
	if err := os.Mkdir(original, 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := Resolve(runtimeDir, "machine-a", original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(copyRoot, ".chat-codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(original, ".chat-codex", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copyRoot, ".chat-codex", "project.json"), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(runtimeDir, "machine-a", copyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if second.ScopeID == first.ScopeID {
		t.Fatalf("copy reused scope: %s", first.ScopeID)
	}
	if second.LineageScopeID != first.ScopeID {
		t.Fatalf("copy lineage=%q want=%q", second.LineageScopeID, first.ScopeID)
	}
}

func TestCorrectProjectIdentityKeepsOrForksScope(t *testing.T) {
	runtimeDir := t.TempDir()
	root := t.TempDir()
	first, err := Resolve(runtimeDir, "machine-a", root)
	if err != nil {
		t.Fatal(err)
	}
	forked, err := Correct(runtimeDir, "machine-a", root, "treat_as_new", "")
	if err != nil {
		t.Fatal(err)
	}
	if forked.ScopeID == first.ScopeID || forked.LineageScopeID != first.ScopeID {
		t.Fatalf("unexpected fork correction: first=%+v forked=%+v", first, forked)
	}
	kept, err := Correct(runtimeDir, "machine-a", root, "keep_memory_here", first.ScopeID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.ScopeID != first.ScopeID || kept.InstanceNonce == first.InstanceNonce {
		t.Fatalf("unexpected keep correction: first=%+v kept=%+v", first, kept)
	}
	resolved, err := Resolve(runtimeDir, "machine-a", root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ScopeID != kept.ScopeID || resolved.InstanceNonce != kept.InstanceNonce {
		t.Fatalf("correction was not persisted: kept=%+v resolved=%+v", kept, resolved)
	}
}

func TestResolveSerializesConcurrentRegistryUpdates(t *testing.T) {
	runtimeDir := t.TempDir()
	const count = 12
	roots := make([]string, count)
	for index := range roots {
		roots[index] = t.TempDir()
	}
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for _, root := range roots {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Resolve(runtimeDir, "machine-a", root)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	registry, err := readRegistry(filepath.Join(runtimeDir, "project_identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(registry.Machines["machine-a"]); got != count {
		t.Fatalf("concurrent registry records=%d want=%d", got, count)
	}
}

func TestResolveIsolatesMachinesWithoutOverwritingMarker(t *testing.T) {
	root := t.TempDir()
	first, err := Resolve(t.TempDir(), "machine-a", root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(t.TempDir(), "machine-b", root)
	if err != nil {
		t.Fatal(err)
	}
	if first.ScopeID == second.ScopeID {
		t.Fatalf("different machines shared scope: %s", first.ScopeID)
	}
	data, err := os.ReadFile(filepath.Join(root, ".chat-codex", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var marker markerFile
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatal(err)
	}
	if len(marker.Instances) != 2 {
		t.Fatalf("expected both machine instances, got %#v", marker.Instances)
	}
}

func TestResolveUsesRegistryWhenMarkerCannotBeRewritten(t *testing.T) {
	runtimeDir := t.TempDir()
	root := t.TempDir()
	first, err := Resolve(runtimeDir, "machine-a", root)
	if err != nil {
		t.Fatal(err)
	}
	markerDir := filepath.Join(root, ".chat-codex")
	if err := os.Remove(filepath.Join(markerDir, "project.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(markerDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(markerDir, 0o755) })
	second, err := Resolve(runtimeDir, "machine-a", root)
	if err != nil {
		t.Fatal(err)
	}
	if second.ScopeID != first.ScopeID || second.InstanceNonce != first.InstanceNonce {
		t.Fatalf("registry fallback changed identity: first=%+v second=%+v", first, second)
	}
}
