package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"launcher/internal/binarymeta"
	"launcher/internal/config"
	"launcher/internal/mcpconfig"
	"launcher/internal/model"
	proc "launcher/internal/process"
	"launcher/internal/release"
	"launcher/internal/selfupdate"
	runstate "launcher/internal/state"
	launcherVersion "launcher/internal/version"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("LAUNCHER_SKIP_AUTOSTART_REPAIR", "1")
	_ = os.Setenv("LAUNCHER_SKIP_MAC_APP_INSTALL", "1")
	os.Exit(m.Run())
}

func TestRemoveAgentDeletesAgentDirectory(t *testing.T) {
	runtimeDir := t.TempDir()
	agentID := "agent_test"
	agentDir := filepath.Join(runtimeDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "config.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "stdout.log"), []byte("out\n"), 0o644); err != nil {
		t.Fatalf("write stdout log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "stderr.log"), []byte("err\n"), 0o644); err != nil {
		t.Fatalf("write stderr log: %v", err)
	}

	svc := &service{
		cfg: config.Config{RuntimeDir: runtimeDir},
		state: runstate.DeviceState{
			Status:     "running",
			AgentCount: 1,
			LastError:  "超过最大重启次数",
		},
		agents: []model.Agent{{
			AgentID:   agentID,
			Status:    "failed",
			LastError: "超过最大重启次数",
		}},
		processes: map[string]int{},
	}

	if err := prepareRuntimeFiles(runtimeDir); err != nil {
		t.Fatalf("prepare runtime files: %v", err)
	}

	if err := svc.RemoveAgent(agentID); err != nil {
		t.Fatalf("remove agent: %v", err)
	}
	if _, err := os.Stat(agentDir); !os.IsNotExist(err) {
		t.Fatalf("expected agent dir removed, got err=%v", err)
	}
	if len(svc.agents) != 0 {
		t.Fatalf("expected no agents remaining, got %d", len(svc.agents))
	}
	if svc.state.LastError != "" {
		t.Fatalf("expected state last_error cleared, got %s", svc.state.LastError)
	}
	stored, err := runstate.Load(runtimeDir)
	if err != nil {
		t.Fatalf("load stored state: %v", err)
	}
	if stored.LastError != "" {
		t.Fatalf("expected stored state last_error cleared, got %s", stored.LastError)
	}
}

func prepareRuntimeFiles(runtimeDir string) error {
	if err := saveAgents(runtimeDir, []model.Agent{}); err != nil {
		return err
	}
	return runstate.Save(runtimeDir, runstate.DeviceState{Status: "running"})
}

func TestRenameAgentPersistsDisplayNameWithoutRestart(t *testing.T) {
	runtimeDir := t.TempDir()
	agentID := "agent_test"
	agentDir := filepath.Join(runtimeDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := model.AgentConfig{AgentID: agentID, Name: "old", ProjectDir: "/work/demo", Port: 4096}
	if err := writeAgentConfig(agentDir, cfg); err != nil {
		t.Fatal(err)
	}
	agent := model.Agent{AgentID: agentID, Name: "old", ProjectDir: cfg.ProjectDir, PID: 1234, Status: "running"}
	if err := saveAgents(runtimeDir, []model.Agent{agent}); err != nil {
		t.Fatal(err)
	}
	svc := &service{
		cfg:       config.Config{RuntimeDir: runtimeDir},
		agents:    []model.Agent{agent},
		processes: map[string]int{agentID: 1234},
	}

	updated, err := svc.RenameAgent(model.RenameAgentInput{AgentID: agentID, Name: "逆向专家安卓"})
	if err != nil {
		t.Fatalf("rename agent: %v", err)
	}
	if updated.Name != "逆向专家安卓" || updated.PID != 1234 {
		t.Fatalf("unexpected renamed agent: %+v", updated)
	}
	storedConfig, err := readAgentConfig(runtimeDir, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if storedConfig.Name != "逆向专家安卓" {
		t.Fatalf("config name not persisted: %q", storedConfig.Name)
	}
	storedAgents, err := loadAgents(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(storedAgents) != 1 || storedAgents[0].Name != "逆向专家安卓" {
		t.Fatalf("agent state name not persisted: %+v", storedAgents)
	}
}

func TestCanonicalLauncherExecutablePrefersGUILauncher(t *testing.T) {
	runtimeDir := t.TempDir()
	binDir := filepath.Join(runtimeDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	opencodePath := filepath.Join(binDir, binarymeta.DefaultBinaryName())
	if err := os.WriteFile(opencodePath, []byte("agent"), 0o755); err != nil {
		t.Fatalf("write opencode: %v", err)
	}
	guiPath := filepath.Join(binDir, binarymeta.GUILauncherBinaryName())
	if err := os.WriteFile(guiPath, []byte("launcher"), 0o755); err != nil {
		t.Fatalf("write gui launcher: %v", err)
	}

	svc := &service{cfg: config.Config{RuntimeDir: runtimeDir}}
	got, err := svc.canonicalLauncherExecutable()
	if err != nil {
		t.Fatalf("canonical launcher: %v", err)
	}
	if got != guiPath {
		t.Fatalf("expected GUI launcher %q, got %q", guiPath, got)
	}
}

func TestAIConfigEnrichesClaudeVariantsFromRunningAgentProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	configPath := filepath.Join(root, "opencode.json")
	if err := os.WriteFile(configPath, []byte(`{
	  "provider": {
	    "性价比日卡": {
	      "options": {
	        "baseURL": "https://www.mcgrox.top/v1",
	        "apiKey": "sk-demo"
	      },
	      "models": {
	        "claude-opus-4-6": { "name": "claude-opus-4-6" }
	      }
	    }
	  },
	  "model": "性价比日卡/claude-opus-4-6"
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/provider" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "connected": ["性价比日卡"],
		  "all": [{
		    "id": "性价比日卡",
		    "models": {
		      "claude-opus-4-6": {
		        "id": "claude-opus-4-6",
		        "providerID": "性价比日卡",
		        "name": "claude-opus-4-6",
		        "limit": { "context": 200000 },
		        "capabilities": {
		          "input": { "text": true, "image": true },
		          "output": { "text": true }
		        },
		        "variants": {
		          "low": { "reasoningEffort": "low" },
		          "medium": { "reasoningEffort": "medium" },
		          "high": { "reasoningEffort": "high" }
		        }
		      }
		    }
		  }]
		}`))
	}))
	defer server.Close()
	portString := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	svc := &service{
		cfg: config.Config{RuntimeDir: t.TempDir()},
		agents: []model.Agent{{
			AgentID: "agent_test",
			Status:  "running",
			Port:    port,
			Enabled: true,
		}},
	}
	info, err := svc.AIConfig()
	if err != nil {
		t.Fatalf("ai config: %v", err)
	}
	if len(info.Models) != 1 {
		t.Fatalf("expected one active model, got %+v", info.Models)
	}
	if info.Models[0].Context != 200000 {
		t.Fatalf("expected runtime context, got %+v", info.Models[0])
	}
	if len(info.Models[0].Variants) != 3 {
		t.Fatalf("expected runtime variants, got %+v", info.Models[0].Variants)
	}
	if info.Models[0].Modalities == nil || len(info.Models[0].Modalities.Input) != 2 {
		t.Fatalf("expected runtime modalities, got %+v", info.Models[0].Modalities)
	}
}

func TestPrepareRuntimeEnvironmentReturnsExistingPreparingState(t *testing.T) {
	runtimeDir := t.TempDir()
	svc := &service{cfg: config.Config{RuntimeDir: runtimeDir}}
	svc.setRuntimeEnvironmentInfo(model.RuntimeEnvironmentInfo{
		RuntimeDir: runtimeDir,
		Preparing:  true,
		UpdatedAt:  time.Now().UTC(),
		Logs:       []string{"正在后台准备托管运行时。"},
	})
	svc.runtimeMu.Lock()
	defer svc.runtimeMu.Unlock()

	startedAt := time.Now()
	info, err := svc.PrepareRuntimeEnvironment()
	if err != nil {
		t.Fatalf("prepare runtime environment: %v", err)
	}
	if time.Since(startedAt) > 100*time.Millisecond {
		t.Fatalf("expected prepare call to return immediately while already preparing")
	}
	if !info.Preparing {
		t.Fatalf("expected preparing state, got %#v", info)
	}
	if info.RuntimeDir != runtimeDir {
		t.Fatalf("expected runtime dir %q, got %q", runtimeDir, info.RuntimeDir)
	}
}

func TestDiscoverDirectoriesHonorsMaxDepthOne(t *testing.T) {
	root := t.TempDir()
	level1 := filepath.Join(root, "workspace-a")
	level2 := filepath.Join(level1, "nested-b")
	if err := os.MkdirAll(level2, 0o755); err != nil {
		t.Fatalf("mkdir nested dirs: %v", err)
	}
	cfg := config.Config{
		Discovery: config.DiscoveryConfig{
			AllowedRoots: []string{root},
			MaxDepth:     1,
		},
	}
	items, err := discoverDirectories(cfg)
	if err != nil {
		t.Fatalf("discover directories: %v", err)
	}
	paths := map[string]bool{}
	for _, item := range items {
		paths[item.Path] = true
	}
	if !paths[root] || !paths[level1] {
		t.Fatalf("expected root and level1 to be discovered: %v", paths)
	}
	if paths[level2] {
		t.Fatalf("did not expect level2 with maxDepth=1: %v", paths)
	}
}

func TestDiscoverDirectoriesHonorsMaxDepthTwo(t *testing.T) {
	root := t.TempDir()
	level1 := filepath.Join(root, "workspace-a")
	level2 := filepath.Join(level1, "nested-b")
	if err := os.MkdirAll(level2, 0o755); err != nil {
		t.Fatalf("mkdir nested dirs: %v", err)
	}
	cfg := config.Config{
		Discovery: config.DiscoveryConfig{
			AllowedRoots: []string{root},
			MaxDepth:     2,
		},
	}
	items, err := discoverDirectories(cfg)
	if err != nil {
		t.Fatalf("discover directories: %v", err)
	}
	paths := map[string]bool{}
	for _, item := range items {
		paths[item.Path] = true
	}
	if !paths[level2] {
		t.Fatalf("expected level2 with maxDepth=2: %v", paths)
	}
}

func TestSystemRootCandidatesWindowsReturnsAvailableDrives(t *testing.T) {
	fakeStat := func(path string) (os.FileInfo, error) {
		switch path {
		case `C:\`, `D:\`:
			return fakeDirInfo{name: path}, nil
		default:
			return nil, errors.New("not found")
		}
	}

	roots := systemRootCandidates("windows", fakeStat)
	if len(roots) != 2 || roots[0] != `C:\` || roots[1] != `D:\` {
		t.Fatalf("expected C and D drives, got %#v", roots)
	}
}

func TestDirectoryNameUsesWindowsDriveLabel(t *testing.T) {
	if got := directoryName(`C:\`); got != "C:" {
		t.Fatalf("expected C: label, got %q", got)
	}
	if got := directoryName(`d:/`); got != "D:" {
		t.Fatalf("expected D: label, got %q", got)
	}
}

type fakeDirInfo struct {
	name string
}

func (f fakeDirInfo) Name() string       { return f.name }
func (f fakeDirInfo) Size() int64        { return 0 }
func (f fakeDirInfo) Mode() os.FileMode  { return os.ModeDir | 0o755 }
func (f fakeDirInfo) ModTime() time.Time { return time.Time{} }
func (f fakeDirInfo) IsDir() bool        { return true }
func (f fakeDirInfo) Sys() any           { return nil }

func TestProjectFileChunkedUploadCompletesWithHash(t *testing.T) {
	root := t.TempDir()
	svc := &service{
		agents: []model.Agent{{
			AgentID:    "agent_upload",
			ProjectDir: root,
		}},
		processes: map[string]int{},
	}

	data := []byte("hello chunked upload")
	hash := sha256Hex(data)
	if _, err := svc.CreateProjectUpload(model.ProjectFilesRequest{
		AgentID:     "agent_upload",
		Path:        "tmp/result.txt",
		UploadID:    "upload-test",
		Size:        int64(len(data)),
		TotalChunks: 2,
	}); err != nil {
		t.Fatalf("create upload: %v", err)
	}
	if _, err := svc.WriteProjectUploadChunk(model.ProjectFilesRequest{
		AgentID:    "agent_upload",
		Path:       "tmp/result.txt",
		UploadID:   "upload-test",
		ChunkIndex: 1,
		Offset:     6,
		Content:    "Y2h1bmtlZCB1cGxvYWQ=",
		Encoding:   "base64",
		SHA256:     sha256Hex([]byte("chunked upload")),
	}); err != nil {
		t.Fatalf("write second chunk: %v", err)
	}
	if _, err := svc.WriteProjectUploadChunk(model.ProjectFilesRequest{
		AgentID:    "agent_upload",
		Path:       "tmp/result.txt",
		UploadID:   "upload-test",
		ChunkIndex: 0,
		Offset:     0,
		Content:    "aGVsbG8g",
		Encoding:   "base64",
		SHA256:     sha256Hex([]byte("hello ")),
	}); err != nil {
		t.Fatalf("write first chunk: %v", err)
	}
	file, err := svc.CompleteProjectUpload(model.ProjectFilesRequest{
		AgentID:  "agent_upload",
		Path:     "tmp/result.txt",
		UploadID: "upload-test",
		Size:     int64(len(data)),
		SHA256:   hash,
	})
	if err != nil {
		t.Fatalf("complete upload: %v", err)
	}
	if file.Size != int64(len(data)) {
		t.Fatalf("unexpected file size: %d", file.Size)
	}
	written, err := os.ReadFile(filepath.Join(root, "tmp", "result.txt"))
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	if string(written) != string(data) {
		t.Fatalf("unexpected content: %q", string(written))
	}
	if _, err := os.Stat(filepath.Join(root, ".chat-codex-uploads", "upload-test")); !os.IsNotExist(err) {
		t.Fatalf("expected temp upload directory removed, err=%v", err)
	}
}

func TestDirectoryFileMutationsRespectAllowedRoot(t *testing.T) {
	root := t.TempDir()
	svc := &service{cfg: config.Config{Discovery: config.DiscoveryConfig{AllowedRoots: []string{root}}}}
	docs := filepath.Join(root, "docs")

	folder, err := svc.CreateDirectoryFolder(model.DirectoryFileRequest{Path: docs})
	if err != nil {
		t.Fatalf("create directory folder: %v", err)
	}
	if !folder.IsDir || folder.Path != docs {
		t.Fatalf("unexpected folder: %+v", folder)
	}
	emptyPath := filepath.Join(docs, "empty.txt")
	if _, err := svc.CreateDirectoryFile(model.DirectoryFileRequest{Path: emptyPath}); err != nil {
		t.Fatalf("create directory file: %v", err)
	}
	if _, err := svc.CreateDirectoryFile(model.DirectoryFileRequest{Path: emptyPath}); err == nil {
		t.Fatal("expected duplicate create rejected")
	}
	if _, err := svc.CreateDirectoryFolder(model.DirectoryFileRequest{Path: filepath.Join(filepath.Dir(root), "outside")}); err == nil {
		t.Fatal("expected outside create rejected")
	}
	if _, err := svc.DeleteDirectoryFile(model.DirectoryFileRequest{Path: root, IsDir: true}); err == nil {
		t.Fatal("expected allowed root delete rejected")
	}
	if _, err := svc.DeleteDirectoryFile(model.DirectoryFileRequest{Path: docs, IsDir: true}); err != nil {
		t.Fatalf("delete directory folder: %v", err)
	}
	if _, err := os.Stat(docs); !os.IsNotExist(err) {
		t.Fatalf("expected recursive delete, err=%v", err)
	}
}

func TestDirectoryFileChunkedUploadRejectsOverwrite(t *testing.T) {
	root := t.TempDir()
	svc := &service{cfg: config.Config{Discovery: config.DiscoveryConfig{AllowedRoots: []string{root}}}}
	target := filepath.Join(root, "upload.txt")
	data := []byte("directory upload")
	req := model.DirectoryFileRequest{
		Path:        target,
		UploadID:    "directory-upload",
		Size:        int64(len(data)),
		TotalChunks: 1,
	}
	if _, err := svc.CreateDirectoryUpload(req); err != nil {
		t.Fatalf("create directory upload: %v", err)
	}
	if _, err := svc.WriteDirectoryUploadChunk(model.DirectoryFileRequest{
		Path:       target,
		UploadID:   req.UploadID,
		ChunkIndex: 0,
		Content:    base64.StdEncoding.EncodeToString(data),
		Encoding:   "base64",
		SHA256:     sha256Hex(data),
	}); err != nil {
		t.Fatalf("write directory chunk: %v", err)
	}
	if _, err := svc.CompleteDirectoryUpload(model.DirectoryFileRequest{
		Path:     target,
		UploadID: req.UploadID,
		Size:     int64(len(data)),
		SHA256:   sha256Hex(data),
	}); err != nil {
		t.Fatalf("complete directory upload: %v", err)
	}
	written, err := os.ReadFile(target)
	if err != nil || string(written) != string(data) {
		t.Fatalf("unexpected uploaded file %q err=%v", written, err)
	}
	if _, err := svc.CreateDirectoryUpload(req); err == nil {
		t.Fatal("expected upload overwrite rejected")
	}
}

func TestDirectoryFileUploadRequiresChecksumsAndBindsSessionToTarget(t *testing.T) {
	root := t.TempDir()
	svc := &service{cfg: config.Config{Discovery: config.DiscoveryConfig{AllowedRoots: []string{root}}}}
	target := filepath.Join(root, "upload.txt")
	otherTarget := filepath.Join(root, "other.txt")
	data := []byte("verified")
	req := model.DirectoryFileRequest{
		Path:        target,
		UploadID:    "bound-upload",
		Size:        int64(len(data)),
		TotalChunks: 1,
	}
	if _, err := svc.CreateDirectoryUpload(req); err != nil {
		t.Fatalf("create directory upload: %v", err)
	}
	if _, err := svc.CreateDirectoryUpload(req); err == nil {
		t.Fatal("expected duplicate upload session rejected")
	}
	if _, err := svc.WriteDirectoryUploadChunk(model.DirectoryFileRequest{
		Path:       target,
		UploadID:   req.UploadID,
		ChunkIndex: 0,
		Content:    base64.StdEncoding.EncodeToString(data),
		Encoding:   "base64",
	}); err == nil {
		t.Fatal("expected missing chunk checksum rejected")
	}
	if _, err := svc.WriteDirectoryUploadChunk(model.DirectoryFileRequest{
		Path:       otherTarget,
		UploadID:   req.UploadID,
		ChunkIndex: 0,
		Content:    base64.StdEncoding.EncodeToString(data),
		Encoding:   "base64",
		SHA256:     sha256Hex(data),
	}); err == nil {
		t.Fatal("expected upload session target mismatch rejected")
	}
	if _, err := svc.WriteDirectoryUploadChunk(model.DirectoryFileRequest{
		Path:       target,
		UploadID:   req.UploadID,
		ChunkIndex: 0,
		Content:    base64.StdEncoding.EncodeToString(data),
		Encoding:   "base64",
		SHA256:     sha256Hex(data),
	}); err != nil {
		t.Fatalf("write verified chunk: %v", err)
	}
	if _, err := svc.CompleteDirectoryUpload(model.DirectoryFileRequest{
		Path:     target,
		UploadID: req.UploadID,
		Size:     int64(len(data)),
	}); err == nil {
		t.Fatal("expected missing file checksum rejected")
	}
}

func TestPublishDirectoryUploadDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	tempPath := filepath.Join(root, "payload.bin")
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(tempPath, []byte("new"), 0o600); err != nil {
		t.Fatalf("write temp upload: %v", err)
	}
	if err := os.WriteFile(target, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := publishDirectoryUpload(tempPath, target); err == nil {
		t.Fatal("expected atomic publish to reject existing target")
	}
	written, err := os.ReadFile(target)
	if err != nil || string(written) != "existing" {
		t.Fatalf("existing target changed to %q err=%v", written, err)
	}
}

func TestDirectoryFileMutationRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional Windows privileges")
	}
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	svc := &service{cfg: config.Config{Discovery: config.DiscoveryConfig{AllowedRoots: []string{root}}}}
	if _, err := svc.CreateDirectoryFile(model.DirectoryFileRequest{Path: filepath.Join(link, "escaped.txt")}); err == nil {
		t.Fatal("expected symlink escape rejected")
	}
}

func TestProjectFileChunkedDownloadReadsRange(t *testing.T) {
	root := t.TempDir()
	content := []byte("hello chunked download")
	if err := os.MkdirAll(filepath.Join(root, "tmp"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "tmp", "source.txt"), content, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	svc := &service{
		agents: []model.Agent{{
			AgentID:    "agent_download",
			ProjectDir: root,
		}},
		processes: map[string]int{},
	}

	meta, err := svc.CreateProjectDownload(model.ProjectFilesRequest{
		AgentID: "agent_download",
		Path:    "tmp/source.txt",
	})
	if err != nil {
		t.Fatalf("create download: %v", err)
	}
	if meta.Size != int64(len(content)) {
		t.Fatalf("unexpected meta size: %d", meta.Size)
	}
	if meta.SHA256 != sha256Hex(content) {
		t.Fatalf("unexpected meta sha256: %s", meta.SHA256)
	}

	chunk, err := svc.ReadProjectDownloadChunk(model.ProjectFilesRequest{
		AgentID:    "agent_download",
		Path:       "tmp/source.txt",
		ChunkIndex: 1,
		Offset:     6,
		Length:     7,
	})
	if err != nil {
		t.Fatalf("download chunk: %v", err)
	}
	data, err := base64.StdEncoding.DecodeString(chunk.Content)
	if err != nil {
		t.Fatalf("decode chunk: %v", err)
	}
	if string(data) != "chunked" {
		t.Fatalf("unexpected chunk content: %q", string(data))
	}
}

func TestProjectFileChunkedUploadRollbackKeepsExistingFileOnHashMismatch(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "tmp", "result.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.WriteFile(target, []byte("old content"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	svc := &service{
		agents: []model.Agent{{
			AgentID:    "agent_upload",
			ProjectDir: root,
		}},
		processes: map[string]int{},
	}

	if _, err := svc.CreateProjectUpload(model.ProjectFilesRequest{
		AgentID:     "agent_upload",
		Path:        "tmp/result.txt",
		UploadID:    "upload-test",
		Size:        3,
		TotalChunks: 1,
	}); err != nil {
		t.Fatalf("create upload: %v", err)
	}
	if _, err := svc.WriteProjectUploadChunk(model.ProjectFilesRequest{
		AgentID:    "agent_upload",
		Path:       "tmp/result.txt",
		UploadID:   "upload-test",
		ChunkIndex: 0,
		Offset:     0,
		Content:    "bmV3",
		Encoding:   "base64",
	}); err != nil {
		t.Fatalf("write chunk: %v", err)
	}
	if _, err := svc.CompleteProjectUpload(model.ProjectFilesRequest{
		AgentID:  "agent_upload",
		Path:     "tmp/result.txt",
		UploadID: "upload-test",
		Size:     3,
		SHA256:   "bad-hash",
	}); err == nil {
		t.Fatalf("expected hash mismatch")
	}
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(written) != "old content" {
		t.Fatalf("expected old content kept, got %q", string(written))
	}
}

func TestProjectFileMutationsStayInsideProjectRoot(t *testing.T) {
	root := t.TempDir()
	svc := &service{
		agents: []model.Agent{{
			AgentID:    "agent_files",
			ProjectDir: root,
		}},
		processes: map[string]int{},
	}

	folder, err := svc.CreateProjectFolder(model.ProjectFilesRequest{
		AgentID: "agent_files",
		Path:    "docs",
	})
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if !folder.IsDir || folder.Path != "docs" {
		t.Fatalf("unexpected folder: %+v", folder)
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); err != nil {
		t.Fatalf("stat docs: %v", err)
	}

	empty, err := svc.CreateProjectFile(model.ProjectFilesRequest{
		AgentID: "agent_files",
		Path:    "docs/empty.txt",
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if empty.IsDir || empty.Path != "docs/empty.txt" || empty.Size != 0 {
		t.Fatalf("unexpected empty file: %+v", empty)
	}
	existing := filepath.Join(root, "docs", "empty.txt")
	if err := os.WriteFile(existing, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}
	if _, err := svc.CreateProjectFile(model.ProjectFilesRequest{
		AgentID: "agent_files",
		Path:    "docs/empty.txt",
	}); err == nil {
		t.Fatalf("expected duplicate file create rejected")
	}
	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read existing file: %v", err)
	}
	if string(data) != "keep" {
		t.Fatalf("expected existing file content kept, got %q", string(data))
	}

	filePath := filepath.Join(root, "docs", "old.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	renamed, err := svc.RenameProjectFile(model.ProjectFilesRequest{
		AgentID: "agent_files",
		Path:    "docs/old.txt",
		Name:    "new.txt",
	})
	if err != nil {
		t.Fatalf("rename file: %v", err)
	}
	if renamed.Path != "docs/new.txt" || renamed.Name != "new.txt" {
		t.Fatalf("unexpected renamed file: %+v", renamed)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "new.txt")); err != nil {
		t.Fatalf("stat renamed file: %v", err)
	}

	deleted, err := svc.DeleteProjectFile(model.ProjectFilesRequest{
		AgentID: "agent_files",
		Path:    "docs/new.txt",
		IsDir:   false,
	})
	if err != nil {
		t.Fatalf("delete file: %v", err)
	}
	if deleted.IsDir || deleted.Path != "docs/new.txt" {
		t.Fatalf("unexpected deleted file: %+v", deleted)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, err=%v", err)
	}

	if _, err := svc.DeleteProjectFile(model.ProjectFilesRequest{
		AgentID: "agent_files",
		Path:    "docs/empty.txt",
		IsDir:   false,
	}); err != nil {
		t.Fatalf("delete created file: %v", err)
	}

	if _, err := svc.DeleteProjectFile(model.ProjectFilesRequest{
		AgentID: "agent_files",
		Path:    "docs",
		IsDir:   true,
	}); err != nil {
		t.Fatalf("delete folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Fatalf("expected folder removed, err=%v", err)
	}

	if _, err := svc.CreateProjectFolder(model.ProjectFilesRequest{
		AgentID: "agent_files",
		Path:    "../outside",
	}); err == nil {
		t.Fatalf("expected outside mkdir rejected")
	}
	if _, err := svc.RenameProjectFile(model.ProjectFilesRequest{
		AgentID: "agent_files",
		Path:    "missing.txt",
		Name:    "../bad",
	}); err == nil {
		t.Fatalf("expected bad rename name rejected")
	}
}

func TestCurrentAgentErrorReturnsFailedAgentError(t *testing.T) {
	got := currentAgentError([]model.Agent{
		{AgentID: "agent_running", Status: "running", LastError: "old"},
		{AgentID: "agent_failed", Status: "failed", LastError: "超过最大重启次数"},
	})
	if got != "超过最大重启次数" {
		t.Fatalf("expected failed agent error, got %s", got)
	}
}

func TestCurrentAgentErrorClearsRecoveredErrors(t *testing.T) {
	got := currentAgentError([]model.Agent{
		{AgentID: "agent_running", Status: "running", LastError: "old"},
		{AgentID: "agent_idle", Status: "idle"},
	})
	if got != "" {
		t.Fatalf("expected no current agent error, got %s", got)
	}
}

func TestMCPStatusUsesDedicatedMCPToolsEndpoint(t *testing.T) {
	port := freePortForTest(t)
	server := startMCPServerForTest(t, port)
	defer server.Close()

	svc := &service{
		agents: []model.Agent{{
			AgentID: "agent_mcp",
			Status:  "running",
			Port:    port,
		}},
	}

	status, err := svc.MCPStatus("agent_mcp")
	if err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if len(status.Servers) != 2 {
		t.Fatalf("expected two mcp servers, got %d", len(status.Servers))
	}

	toolsByServer := map[string][]model.MCPToolInfo{}
	for _, server := range status.Servers {
		toolsByServer[server.Name] = server.Tools
	}
	if got := toolsByServer["playwright"]; len(got) != 1 || got[0].ID != "playwright_browser_navigate" {
		t.Fatalf("expected playwright tool from /mcp/tools, got %#v", got)
	}
	if got := toolsByServer["verify"]; len(got) != 1 || got[0].ID != "verify_get_context" {
		t.Fatalf("expected verify tool from /mcp/tools, got %#v", got)
	}
}

func TestMCPStatusFriendlyRuntimeError(t *testing.T) {
	got := friendlyMCPRuntimeError(`Executable not found in $PATH: "npx"`)
	if !strings.Contains(got, "托管 Node 未就绪") {
		t.Fatalf("expected friendly node runtime error, got %q", got)
	}
	if strings.Contains(got, "Executable not found") {
		t.Fatalf("expected raw executable error to be hidden, got %q", got)
	}
	java := friendlyMCPRuntimeError(`Executable not found in $PATH: "java"`)
	if !strings.Contains(java, "托管 Java 未就绪") {
		t.Fatalf("expected friendly java runtime error, got %q", java)
	}
	unchanged := friendlyMCPRuntimeError("MCP server handshake failed")
	if unchanged != "MCP server handshake failed" {
		t.Fatalf("expected unrelated errors to remain unchanged, got %q", unchanged)
	}
}

func TestRestoreAgentsAdoptsHealthyExistingProcess(t *testing.T) {
	port := freePortForTest(t)
	server := startHealthServerForTest(t, port)
	defer server.Close()

	runtimeDir := t.TempDir()
	agentID := "agent_test"
	agentDir := filepath.Join(runtimeDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	cfg := model.AgentConfig{
		AgentID:    agentID,
		Name:       "demo",
		ProjectDir: runtimeDir,
		Port:       port,
		BinaryName: "does-not-matter",
	}
	if err := writeAgentConfig(agentDir, cfg); err != nil {
		t.Fatalf("write agent config: %v", err)
	}
	svc := &service{
		cfg: config.Config{RuntimeDir: runtimeDir},
		state: runstate.DeviceState{
			Status:     "running",
			AgentCount: 1,
		},
		agents: []model.Agent{{
			AgentID:    agentID,
			Name:       "demo",
			ProjectDir: runtimeDir,
			Status:     "failed",
			Restarts:   5,
			LastError:  "超过最大重启次数",
			Port:       port,
		}},
		processes:    map[string]int{},
		restoreOnRun: true,
	}

	if err := prepareRuntimeFiles(runtimeDir); err != nil {
		t.Fatalf("prepare runtime files: %v", err)
	}
	if err := svc.restoreAgents(); err != nil {
		t.Fatalf("restore agents: %v", err)
	}
	if len(svc.agents) != 1 {
		t.Fatalf("expected one agent, got %d", len(svc.agents))
	}
	if svc.agents[0].Status != "running" {
		t.Fatalf("expected running agent, got %s", svc.agents[0].Status)
	}
	if svc.agents[0].Restarts != 0 {
		t.Fatalf("expected restart counter reset, got %d", svc.agents[0].Restarts)
	}
	if svc.agents[0].LastError != "" {
		t.Fatalf("expected last error cleared, got %s", svc.agents[0].LastError)
	}
}

func TestRestoreAgentsSkipsDisabledAgent(t *testing.T) {
	port := freePortForTest(t)
	server := startHealthServerForTest(t, port)
	defer server.Close()

	runtimeDir := t.TempDir()
	agentID := "agent_disabled"
	agentDir := filepath.Join(runtimeDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	cfg := model.AgentConfig{
		AgentID:    agentID,
		Name:       "disabled",
		ProjectDir: runtimeDir,
		Port:       port,
		BinaryName: "does-not-matter",
	}
	if err := writeAgentConfig(agentDir, cfg); err != nil {
		t.Fatalf("write agent config: %v", err)
	}
	svc := &service{
		cfg: config.Config{RuntimeDir: runtimeDir},
		state: runstate.DeviceState{
			Status:     "running",
			AgentCount: 1,
		},
		agents: []model.Agent{{
			AgentID:    agentID,
			Name:       "disabled",
			ProjectDir: runtimeDir,
			Enabled:    false,
			Status:     "disabled",
			Port:       port,
		}},
		processes: map[string]int{},
	}

	if err := prepareRuntimeFiles(runtimeDir); err != nil {
		t.Fatalf("prepare runtime files: %v", err)
	}
	if err := svc.restoreAgents(); err != nil {
		t.Fatalf("restore agents: %v", err)
	}
	if len(svc.agents) != 1 {
		t.Fatalf("expected one agent, got %d", len(svc.agents))
	}
	if svc.agents[0].Enabled {
		t.Fatal("expected disabled agent to remain disabled")
	}
	if svc.agents[0].Status != "disabled" {
		t.Fatalf("expected disabled status, got %s", svc.agents[0].Status)
	}
	if svc.agents[0].PID != 0 {
		t.Fatalf("expected no adopted pid, got %d", svc.agents[0].PID)
	}
}

func TestNewServiceRecoversInterruptedUpgradeState(t *testing.T) {
	runtimeDir := t.TempDir()
	version := "1.0.0"
	if err := writeReleaseForTest(runtimeDir, version, os.Args[0]); err != nil {
		t.Fatalf("write release: %v", err)
	}
	if err := runstate.Save(runtimeDir, runstate.DeviceState{
		Status:         "upgrading",
		CurrentVersion: version,
		TargetVersion:  "1.1.0",
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	svc, err := NewService(config.Config{
		RuntimeDir: runtimeDir,
		Agent: config.AgentConfig{
			BinaryName: os.Args[0],
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if svc.state.Status != "failed" {
		t.Fatalf("expected interrupted upgrade to be marked failed, got %s", svc.state.Status)
	}
	if svc.state.TargetVersion != "" {
		t.Fatalf("expected target version cleared, got %s", svc.state.TargetVersion)
	}
	if !strings.Contains(svc.state.LastError, "上次升级未完成") {
		t.Fatalf("expected recovery error, got %s", svc.state.LastError)
	}
}

func TestNewServiceClearsCompletedTargetVersion(t *testing.T) {
	runtimeDir := t.TempDir()
	version := "1.0.0"
	if err := writeReleaseForTest(runtimeDir, version, os.Args[0]); err != nil {
		t.Fatalf("write release: %v", err)
	}
	if err := runstate.Save(runtimeDir, runstate.DeviceState{
		Status:         "running",
		CurrentVersion: version,
		TargetVersion:  version,
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	svc, err := NewService(config.Config{
		RuntimeDir: runtimeDir,
		Agent: config.AgentConfig{
			BinaryName: os.Args[0],
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if svc.state.TargetVersion != "" {
		t.Fatalf("expected completed target version cleared, got %s", svc.state.TargetVersion)
	}
}

func TestNewServiceRecoversStaleLauncherSelfUpdateLock(t *testing.T) {
	runtimeDir := t.TempDir()
	version := "1.0.0"
	if err := writeReleaseForTest(runtimeDir, version, os.Args[0]); err != nil {
		t.Fatalf("write release: %v", err)
	}
	if err := runstate.Save(runtimeDir, runstate.DeviceState{
		Status:          "running",
		CurrentVersion:  version,
		UpgradeLocked:   true,
		UpgradeStage:    upgradeStageSwitching,
		UpgradeProgress: 90,
		UpgradeMessage:  "launcher 自更新已调度，正在重启码控",
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	svc, err := NewService(config.Config{
		RuntimeDir: runtimeDir,
		Agent: config.AgentConfig{
			BinaryName: os.Args[0],
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if svc.state.UpgradeLocked {
		t.Fatalf("expected stale launcher self-update lock to be cleared")
	}
	if svc.state.UpgradeStage != upgradeStageCompleted {
		t.Fatalf("expected completed stage, got %s", svc.state.UpgradeStage)
	}
	if svc.state.UpgradeProgress != 100 {
		t.Fatalf("expected 100 progress, got %d", svc.state.UpgradeProgress)
	}
	if svc.state.UpgradeMessage != "launcher 自更新完成" {
		t.Fatalf("expected launcher self-update message, got %s", svc.state.UpgradeMessage)
	}
}

func TestSelfUpdateActiveTransactionDoesNotOverwriteProgress(t *testing.T) {
	runtimeDir := t.TempDir()
	lockDir := filepath.Join(runtimeDir, "self-updates")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockData, err := json.Marshal(map[string]any{
		"pid":        os.Getpid(),
		"created_at": time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockDir, "update.lock"), lockData, 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &service{
		cfg: config.Config{RuntimeDir: runtimeDir},
		state: runstate.DeviceState{
			Status:          "running",
			UpgradeLocked:   true,
			UpgradeStage:    upgradeStageSwitching,
			UpgradeProgress: 90,
			UpgradeMessage:  "launcher 自更新已调度，正在重启码控",
		},
	}

	_, err = svc.SelfUpdate(model.LauncherSelfUpdateInput{Apply: true, Restart: true})
	if !errors.Is(err, selfupdate.ErrUpdateInProgress) {
		t.Fatalf("expected ErrUpdateInProgress, got %v", err)
	}
	if svc.state.UpgradeStage != upgradeStageSwitching || svc.state.UpgradeProgress != 90 {
		t.Fatalf("active transaction state was overwritten: %+v", svc.state)
	}
	if svc.state.UpgradeMessage != "launcher 自更新已调度，正在重启码控" {
		t.Fatalf("active transaction message was overwritten: %s", svc.state.UpgradeMessage)
	}
}

func TestSelfUpdateStatusMonitorReconcilesUpdaterCompletion(t *testing.T) {
	runtimeDir := t.TempDir()
	statusPath := filepath.Join(runtimeDir, "self-updates", "apply-status.json")
	if err := selfupdate.WriteApplyStatus(statusPath, selfupdate.ApplyStatus{
		Version: launcherVersion.Value,
		State:   "applying",
		Message: "正在替换 launcher 并启动新版本",
	}); err != nil {
		t.Fatal(err)
	}
	svc := &service{
		cfg: config.Config{RuntimeDir: runtimeDir},
		state: runstate.DeviceState{
			Status:         "failed",
			UpgradeStage:   upgradeStageFailed,
			UpgradeMessage: selfupdate.ErrUpdateInProgress.Error(),
			LastError:      selfupdate.ErrUpdateInProgress.Error(),
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.startSelfUpdateStatusMonitor(ctx)
	if err := selfupdate.WriteApplyStatus(statusPath, selfupdate.ApplyStatus{
		Version: launcherVersion.Value,
		State:   "completed",
		Message: "launcher 自更新完成并通过健康检查",
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		svc.mu.RLock()
		completed := svc.state.Status == "running" && svc.state.UpgradeStage == upgradeStageCompleted
		lastError := svc.state.LastError
		svc.mu.RUnlock()
		if completed {
			if lastError != "" {
				t.Fatalf("expected concurrent update error to be cleared, got %s", lastError)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for completed self-update status")
}

func TestUpgradeNoopClearsCompletedTargetVersion(t *testing.T) {
	runtimeDir := t.TempDir()
	version := "1.0.0"
	if err := writeReleaseForTest(runtimeDir, version, os.Args[0]); err != nil {
		t.Fatalf("write release: %v", err)
	}
	svc := &service{
		cfg: config.Config{RuntimeDir: runtimeDir},
		state: runstate.DeviceState{
			Status:         "upgrading",
			CurrentVersion: version,
			TargetVersion:  version,
		},
		processes: map[string]int{},
	}
	if err := svc.Upgrade(model.UpgradeInput{TargetVersion: version}); err != nil {
		t.Fatalf("noop upgrade: %v", err)
	}
	if svc.state.Status != "running" {
		t.Fatalf("expected status running, got %s", svc.state.Status)
	}
	if svc.state.TargetVersion != "" {
		t.Fatalf("expected target version cleared, got %s", svc.state.TargetVersion)
	}
}

func TestStartUpgradeMarksProgressBeforeBackgroundWork(t *testing.T) {
	runtimeDir := t.TempDir()
	currentVersion := "1.0.0"
	targetVersion := "1.1.0"
	if err := writeReleaseForTest(runtimeDir, currentVersion, os.Args[0]); err != nil {
		t.Fatalf("write current release: %v", err)
	}
	if err := writeReleaseForTest(runtimeDir, targetVersion, os.Args[0]); err != nil {
		t.Fatalf("write target release: %v", err)
	}
	if err := release.Switch(runtimeDir, currentVersion); err != nil {
		t.Fatalf("switch current release: %v", err)
	}
	svc := &service{
		cfg: config.Config{RuntimeDir: runtimeDir},
		state: runstate.DeviceState{
			Status:         "running",
			CurrentVersion: currentVersion,
		},
		processes: map[string]int{},
	}
	if err := runstate.Save(runtimeDir, svc.state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	if err := svc.StartUpgrade(model.UpgradeInput{TargetVersion: targetVersion}); err != nil {
		t.Fatalf("start upgrade: %v", err)
	}
	state := svc.State()
	if state.Status != "upgrading" {
		t.Fatalf("expected upgrading state immediately, got %s", state.Status)
	}
	if !state.UpgradeLocked {
		t.Fatalf("expected upgrade lock")
	}
	if state.UpgradeStage == "" || state.UpgradeProgress <= 0 {
		t.Fatalf("expected progress state, got stage=%q progress=%d", state.UpgradeStage, state.UpgradeProgress)
	}
	if len(state.UpgradeLogs) == 0 {
		t.Fatalf("expected upgrade logs")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state = svc.State()
		if !state.UpgradeLocked {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if state.UpgradeLocked {
		t.Fatalf("expected background upgrade to finish")
	}
	if state.CurrentVersion != targetVersion {
		t.Fatalf("expected current version %s, got %s", targetVersion, state.CurrentVersion)
	}
	if state.UpgradeStage != upgradeStageCompleted {
		t.Fatalf("expected completed stage, got %s", state.UpgradeStage)
	}
}

func TestUpgradeSwitchesVersionEvenWhenOneAgentPortIsStuck(t *testing.T) {
	runtimeDir := t.TempDir()
	currentVersion := "1.0.0"
	targetVersion := "1.1.0"
	if err := writeReleaseForTest(runtimeDir, currentVersion, os.Args[0]); err != nil {
		t.Fatalf("write current release: %v", err)
	}
	if err := writeReleaseForTest(runtimeDir, targetVersion, os.Args[0]); err != nil {
		t.Fatalf("write target release: %v", err)
	}
	if err := release.Switch(runtimeDir, currentVersion); err != nil {
		t.Fatalf("switch back to current release: %v", err)
	}

	blockedPort := freePortForTest(t)
	blocker := startNonHealthTCPServerForTest(t, blockedPort)
	defer blocker.Close()
	agentID := "agent_blocked"
	agentDir := filepath.Join(runtimeDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	cfg := model.AgentConfig{
		AgentID:    agentID,
		Name:       "blocked",
		ProjectDir: runtimeDir,
		Port:       blockedPort,
		BinaryName: os.Args[0],
		Args:       []string{"-test.run=TestHelperServeHealth", "--", "--port", fmt.Sprintf("%d", blockedPort)},
		Env:        map[string]string{},
	}
	if err := writeAgentConfig(agentDir, cfg); err != nil {
		t.Fatalf("write agent config: %v", err)
	}

	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			Agent: config.AgentConfig{
				BinaryName: os.Args[0],
				Env: map[string]string{
					"GO_WANT_HELPER_HEALTH_SERVER": "1",
				},
			},
			Health: config.HealthConfig{
				TimeoutSeconds: 3,
				IntervalMillis: 50,
				MaxRestarts:    1,
				BackoffMillis:  10,
			},
		},
		state: runstate.DeviceState{
			Status:         "running",
			CurrentVersion: currentVersion,
			AgentCount:     1,
		},
		agents: []model.Agent{{
			AgentID:    agentID,
			Name:       "blocked",
			ProjectDir: runtimeDir,
			Enabled:    true,
			Status:     "running",
			Port:       blockedPort,
		}},
		processes: map[string]int{},
	}
	if err := saveAgents(runtimeDir, svc.agents); err != nil {
		t.Fatalf("save agents: %v", err)
	}
	if err := runstate.Save(runtimeDir, svc.state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	if err := svc.Upgrade(model.UpgradeInput{TargetVersion: targetVersion}); err != nil {
		t.Fatalf("upgrade should switch version even when one agent fails: %v", err)
	}
	current, err := release.Current(runtimeDir)
	if err != nil {
		t.Fatalf("read current version: %v", err)
	}
	if current != targetVersion {
		t.Fatalf("expected current version %s, got %s", targetVersion, current)
	}
	if svc.state.CurrentVersion != targetVersion {
		t.Fatalf("expected state current version %s, got %s", targetVersion, svc.state.CurrentVersion)
	}
	if svc.state.Status != "running" {
		t.Fatalf("expected launcher running after partial upgrade, got %s", svc.state.Status)
	}
	if len(svc.agents) != 1 {
		t.Fatalf("expected one agent, got %#v", svc.agents)
	}
	if svc.agents[0].Status != "running" {
		stdout, _ := os.ReadFile(filepath.Join(runtimeDir, "agents", agentID, "stdout.log"))
		stderr, _ := os.ReadFile(filepath.Join(runtimeDir, "agents", agentID, "stderr.log"))
		t.Logf("stdout:\n%s", stdout)
		t.Logf("stderr:\n%s", stderr)
		t.Fatalf("expected blocked agent to restart on reassigned port, got %#v", svc.agents[0])
	}
	if svc.agents[0].Port == blockedPort {
		t.Fatalf("expected agent port to be reassigned away from blocked port %d", blockedPort)
	}
	if svc.agents[0].PID > 0 {
		t.Cleanup(func() {
			previous, trackedPID, err := svc.beginAgentTransition(agentID, "stopping")
			if err == nil {
				_ = svc.stopAgentProcess(previous, trackedPID)
			}
			stdout, stderr := proc.DefaultBinaryLogs(runtimeDir, agentID)
			waitForFilesWritable(t, []string{stdout, stderr}, 3*time.Second)
		})
	}
	storedCfg, err := readAgentConfig(runtimeDir, agentID)
	if err != nil {
		t.Fatalf("read stored agent config: %v", err)
	}
	if storedCfg.Port != svc.agents[0].Port {
		t.Fatalf("expected stored config port %d, got %d", svc.agents[0].Port, storedCfg.Port)
	}
	if !hasPortArg(storedCfg.Args, storedCfg.Port) {
		t.Fatalf("expected stored args to include reassigned port %d, got %#v", storedCfg.Port, storedCfg.Args)
	}
}

func TestSpawnAgentRejectsDeadChildEvenIfPortAlreadyHealthy(t *testing.T) {
	port := freePortForTest(t)
	server := startHealthServerForTest(t, port)
	defer server.Close()

	runtimeDir := t.TempDir()
	agentDir := filepath.Join(runtimeDir, "agents", "agent_quick_exit")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	binaryName := "/bin/sh"
	if runtime.GOOS == "windows" {
		binaryName = "cmd"
	}
	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			Agent: config.AgentConfig{
				BinaryName: binaryName,
			},
			Health: config.HealthConfig{
				TimeoutSeconds: 1,
				IntervalMillis: 50,
				MaxRestarts:    1,
				BackoffMillis:  10,
			},
		},
		processes: map[string]int{},
	}

	cfg := model.AgentConfig{
		AgentID:    "agent_quick_exit",
		Name:       "quick-exit",
		ProjectDir: runtimeDir,
		Port:       port,
	}
	if runtime.GOOS == "windows" {
		cfg.BinaryName = "cmd"
		cfg.Args = []string{"/C", "exit 0"}
	} else {
		cfg.BinaryName = "/bin/sh"
		cfg.Args = []string{"-c", "exit 0"}
	}

	_, err := svc.spawnAgent(cfg)
	if err == nil {
		t.Fatal("expected spawnAgent to reject exited child")
	}
	if want := "立即退出"; !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error to contain %q, got %v", want, err)
	}
}

func TestSpawnAgentUsesLauncherWorkDir(t *testing.T) {
	port := freePortForTest(t)
	runtimeDir := t.TempDir()
	projectDir := filepath.Join(runtimeDir, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(runtimeDir, "agents", "agent_workdir"), 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	file := filepath.Join(runtimeDir, "cwd.txt")
	env := map[string]string{
		"GO_WANT_HELPER_HEALTH_SERVER": "1",
		"HELPER_HEALTH_PORT":           fmt.Sprintf("%d", port),
		"HELPER_CWD_FILE":              file,
	}
	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			Agent: config.AgentConfig{
				BinaryName: os.Args[0],
				Env:        env,
			},
			Health: config.HealthConfig{
				TimeoutSeconds: 2,
				IntervalMillis: 50,
				MaxRestarts:    1,
				BackoffMillis:  10,
			},
		},
		processes: map[string]int{},
	}

	agent, err := svc.spawnAgent(model.AgentConfig{
		AgentID:    "agent_workdir",
		Name:       "workdir",
		ProjectDir: projectDir,
		Port:       port,
		Args:       []string{"-test.run=TestHelperServeHealth"},
		Env:        env,
	})
	if err != nil {
		t.Fatalf("spawn agent: %v", err)
	}
	t.Cleanup(func() {
		stopAgentForTest(t, runtimeDir, agent.AgentID, agent.PID)
	})
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read cwd file: %v", err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(runtimeDir, "work"))
	if err != nil {
		t.Fatalf("eval work dir: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != want {
		t.Fatalf("expected cwd %q, got %q", want, got)
	}
}

func TestRestartAgentClearsStoredLastError(t *testing.T) {
	port := freePortForTest(t)
	runtimeDir := t.TempDir()
	agentID := "agent_restart"
	agentDir := filepath.Join(runtimeDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	cfg := model.AgentConfig{
		AgentID:    agentID,
		Name:       "restartable",
		ProjectDir: runtimeDir,
		Port:       port,
		BinaryName: os.Args[0],
		Args:       []string{"-test.run=TestHelperServeHealth"},
		Env: map[string]string{
			"GO_WANT_HELPER_HEALTH_SERVER": "1",
			"HELPER_HEALTH_PORT":           fmt.Sprintf("%d", port),
		},
	}
	if err := writeAgentConfig(agentDir, cfg); err != nil {
		t.Fatalf("write agent config: %v", err)
	}

	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			Agent: config.AgentConfig{
				BinaryName: os.Args[0],
				Env: map[string]string{
					"GO_WANT_HELPER_HEALTH_SERVER": "1",
					"HELPER_HEALTH_PORT":           fmt.Sprintf("%d", port),
				},
			},
			Health: config.HealthConfig{
				TimeoutSeconds: 2,
				IntervalMillis: 50,
				MaxRestarts:    1,
				BackoffMillis:  10,
			},
		},
		state: runstate.DeviceState{
			Status:     "running",
			AgentCount: 1,
			LastError:  "超过最大重启次数",
		},
		agents: []model.Agent{{
			AgentID:    agentID,
			Name:       "restartable",
			ProjectDir: runtimeDir,
			Status:     "failed",
			Port:       port,
			LastError:  "超过最大重启次数",
		}},
		processes: map[string]int{},
	}

	if err := saveAgents(runtimeDir, svc.agents); err != nil {
		t.Fatalf("save agents: %v", err)
	}
	if err := runstate.Save(runtimeDir, svc.state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	agent, err := svc.RestartAgent(agentID)
	if err != nil {
		t.Fatalf("restart agent: %v", err)
	}
	t.Cleanup(func() {
		svc.mu.Lock()
		delete(svc.processes, agent.AgentID)
		svc.mu.Unlock()
		if agent.PID > 0 {
			stopAgentForTest(t, runtimeDir, agent.AgentID, agent.PID)
		}
	})

	if agent.Status != "running" {
		t.Fatalf("expected running agent, got %s", agent.Status)
	}
	if svc.state.LastError != "" {
		t.Fatalf("expected cleared state last_error, got %s", svc.state.LastError)
	}

	stored, err := runstate.Load(runtimeDir)
	if err != nil {
		t.Fatalf("load stored state: %v", err)
	}
	if stored.LastError != "" {
		t.Fatalf("expected stored state last_error cleared, got %s", stored.LastError)
	}
}

func TestRestartAgentDoesNotBlockStateRead(t *testing.T) {
	port := freePortForTest(t)
	runtimeDir := t.TempDir()
	agentID := "agent_slow_restart"
	agentDir := filepath.Join(runtimeDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	helperEnv := map[string]string{
		"GO_WANT_HELPER_HEALTH_SERVER": "1",
		"HELPER_HEALTH_PORT":           fmt.Sprintf("%d", port),
		"HELPER_HEALTH_DELAY_MS":       "800",
	}
	cfg := model.AgentConfig{
		AgentID:    agentID,
		Name:       "slow-restart",
		ProjectDir: runtimeDir,
		Port:       port,
		BinaryName: os.Args[0],
		Args:       []string{"-test.run=TestHelperServeHealth"},
		Env:        helperEnv,
	}
	if err := writeAgentConfig(agentDir, cfg); err != nil {
		t.Fatalf("write agent config: %v", err)
	}

	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			Agent: config.AgentConfig{
				BinaryName: os.Args[0],
				Env:        helperEnv,
			},
			Health: config.HealthConfig{
				TimeoutSeconds: 3,
				IntervalMillis: 50,
				MaxRestarts:    1,
				BackoffMillis:  10,
			},
		},
		state: runstate.DeviceState{
			Status:     "running",
			AgentCount: 1,
		},
		agents: []model.Agent{{
			AgentID:    agentID,
			Name:       "slow-restart",
			ProjectDir: runtimeDir,
			Status:     "failed",
			Port:       port,
		}},
		processes: map[string]int{},
	}
	if err := saveAgents(runtimeDir, svc.agents); err != nil {
		t.Fatalf("save agents: %v", err)
	}
	if err := runstate.Save(runtimeDir, svc.state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	restartCh := make(chan struct {
		agent model.Agent
		err   error
	}, 1)
	go func() {
		agent, err := svc.RestartAgent(agentID)
		restartCh <- struct {
			agent model.Agent
			err   error
		}{agent: agent, err: err}
	}()

	waitUntil(t, 2*time.Second, func() bool {
		agents := svc.State().Agents
		return len(agents) == 1 && agents[0].Status == "restarting"
	})

	stateCh := make(chan model.DeviceView, 1)
	go func() {
		stateCh <- svc.State()
	}()
	select {
	case state := <-stateCh:
		if len(state.Agents) != 1 {
			t.Fatalf("expected one agent in state, got %d", len(state.Agents))
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("State blocked while RestartAgent was waiting for health")
	}

	select {
	case result := <-restartCh:
		if result.err != nil {
			t.Fatalf("restart agent: %v", result.err)
		}
		t.Cleanup(func() {
			svc.mu.Lock()
			delete(svc.processes, result.agent.AgentID)
			svc.mu.Unlock()
			if result.agent.PID > 0 {
				stopAgentForTest(t, runtimeDir, result.agent.AgentID, result.agent.PID)
			}
		})
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for slow restart")
	}
}

func TestRunServiceContextListensBeforeRestoringAgents(t *testing.T) {
	runtimeDir := t.TempDir()
	agentPort := freePortForTest(t)
	launcherPort := freePortForTest(t)
	agentID := "agent_slow_restore"
	agentDir := filepath.Join(runtimeDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	helperEnv := map[string]string{
		"GO_WANT_HELPER_HEALTH_SERVER": "1",
		"HELPER_HEALTH_PORT":           fmt.Sprintf("%d", agentPort),
		"HELPER_HEALTH_DELAY_MS":       "1000",
	}
	cfg := model.AgentConfig{
		AgentID:    agentID,
		Name:       "slow-restore",
		ProjectDir: runtimeDir,
		Port:       agentPort,
		BinaryName: os.Args[0],
		Args:       []string{"-test.run=TestHelperServeHealth"},
		Env:        helperEnv,
	}
	if err := writeAgentConfig(agentDir, cfg); err != nil {
		t.Fatalf("write agent config: %v", err)
	}

	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			ListenAddr: net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", launcherPort)),
			Agent: config.AgentConfig{
				BinaryName: os.Args[0],
				Env:        helperEnv,
			},
			Health: config.HealthConfig{
				TimeoutSeconds: 3,
				IntervalMillis: 50,
				MaxRestarts:    1,
				BackoffMillis:  10,
			},
		},
		state: runstate.DeviceState{
			Status:     "running",
			AgentCount: 1,
		},
		agents: []model.Agent{{
			AgentID:    agentID,
			Name:       "slow-restore",
			ProjectDir: runtimeDir,
			Enabled:    true,
			Status:     "stopped",
			Port:       agentPort,
		}},
		processes: map[string]int{},
	}
	if err := saveAgents(runtimeDir, svc.agents); err != nil {
		t.Fatalf("save agents: %v", err)
	}
	if err := runstate.Save(runtimeDir, svc.state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunServiceContext(ctx, svc)
	}()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer waitCancel()
	if err := proc.WaitForHealth(waitCtx, launcherPort, 20*time.Millisecond); err != nil {
		t.Fatalf("launcher should listen before restore finishes: %v", err)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run service context: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for launcher shutdown")
	}
}

func TestRunServiceContextStopsManagedAgentsOnContextCancel(t *testing.T) {
	runtimeDir := t.TempDir()
	agentPort := freePortForTest(t)
	launcherPort := freePortForTest(t)
	agentID := "agent_shutdown"
	agentDir := filepath.Join(runtimeDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	cfg := model.AgentConfig{
		AgentID:    agentID,
		Name:       "shutdownable",
		ProjectDir: runtimeDir,
		Port:       agentPort,
		BinaryName: os.Args[0],
		Args:       []string{"-test.run=TestHelperServeHealth"},
	}
	if err := writeAgentConfig(agentDir, cfg); err != nil {
		t.Fatalf("write agent config: %v", err)
	}
	helperEnv := map[string]string{
		"GO_WANT_HELPER_HEALTH_SERVER": "1",
		"HELPER_HEALTH_PORT":           fmt.Sprintf("%d", agentPort),
	}
	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			ListenAddr: net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", launcherPort)),
			Agent: config.AgentConfig{
				BinaryName: os.Args[0],
				Env:        helperEnv,
			},
			Health: config.HealthConfig{
				TimeoutSeconds: 2,
				IntervalMillis: 50,
				MaxRestarts:    1,
				BackoffMillis:  10,
			},
		},
		state: runstate.DeviceState{
			Status:     "running",
			AgentCount: 1,
		},
		agents: []model.Agent{{
			AgentID:    agentID,
			Name:       "shutdownable",
			ProjectDir: runtimeDir,
			Status:     "failed",
			Port:       agentPort,
		}},
		processes: map[string]int{},
	}
	if err := saveAgents(runtimeDir, svc.agents); err != nil {
		t.Fatalf("save agents: %v", err)
	}
	if err := runstate.Save(runtimeDir, svc.state); err != nil {
		t.Fatalf("save state: %v", err)
	}
	agent, err := svc.RestartAgent(agentID)
	if err != nil {
		t.Fatalf("restart agent: %v", err)
	}
	pid := agent.PID
	t.Cleanup(func() {
		stopAgentForTest(t, runtimeDir, agent.AgentID, pid)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunServiceContext(ctx, svc)
	}()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	if err := proc.WaitForHealth(waitCtx, launcherPort, 50*time.Millisecond); err != nil {
		t.Fatalf("wait for launcher health: %v", err)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run service context: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for launcher shutdown")
	}

	exitCtx, exitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer exitCancel()
	if err := proc.WaitForExit(exitCtx, pid, 100*time.Millisecond); err != nil {
		t.Fatalf("expected agent process %d to exit: %v", pid, err)
	}
	if len(svc.agents) != 1 {
		t.Fatalf("expected one managed agent, got %d", len(svc.agents))
	}
	if svc.agents[0].PID != 0 {
		t.Fatalf("expected stopped agent pid cleared, got %d", svc.agents[0].PID)
	}
	if svc.agents[0].Status != "stopped" {
		t.Fatalf("expected stopped agent status, got %s", svc.agents[0].Status)
	}
}

func TestRunServiceContextAutoSelfUpdateAppliesAndStops(t *testing.T) {
	t.Setenv("LAUNCHER_SKIP_AUTOSTART_REPAIR", "1")
	runtimeDir := t.TempDir()
	launcherPort := freePortForTest(t)
	calls := make(chan selfupdate.Options, 1)
	original := selfUpdateRunner
	selfUpdateRunner = func(ctx context.Context, options selfupdate.Options) (selfupdate.Result, error) {
		calls <- options
		return selfupdate.Result{
			CheckResult: selfupdate.CheckResult{
				CurrentVersion:  "0.1.0",
				LatestVersion:   "0.1.1",
				TargetVersion:   "0.1.1",
				Platform:        "test-platform",
				UpdateAvailable: true,
			},
			Downloaded: true,
			Applied:    true,
		}, nil
	}
	t.Cleanup(func() {
		selfUpdateRunner = original
	})
	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			ListenAddr: net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", launcherPort)),
			SelfUpdate: config.SelfUpdateConfig{
				Enabled:      true,
				BaseURL:      "http://127.0.0.1:9",
				InitialDelay: 0,
				Interval:     time.Hour,
				Timeout:      time.Second,
				Restart:      true,
			},
		},
		state:     runstate.DeviceState{Status: "running"},
		processes: map[string]int{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunServiceContext(ctx, svc)
	}()

	select {
	case options := <-calls:
		if !options.Apply || !options.Restart {
			t.Fatalf("expected auto updater to apply and restart: %+v", options)
		}
		if options.BaseURL != "http://127.0.0.1:9" {
			t.Fatalf("expected base url from config, got %s", options.BaseURL)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for auto self-update")
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run service context: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("service did not stop after applied self-update")
	}
}

func TestAutoSelfUpdateWaitsForConcurrentUpdate(t *testing.T) {
	svc := &service{cfg: config.Config{RuntimeDir: t.TempDir()}}
	svc.updateMu.Lock()
	called := make(chan struct{}, 1)
	original := selfUpdateRunner
	selfUpdateRunner = func(context.Context, selfupdate.Options) (selfupdate.Result, error) {
		called <- struct{}{}
		return selfupdate.Result{CheckResult: selfupdate.CheckResult{UpdateAvailable: true}, Applied: true}, nil
	}
	t.Cleanup(func() {
		selfUpdateRunner = original
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan bool, 1)
	go func() {
		done <- svc.runAutoSelfUpdateOnce(ctx, config.SelfUpdateConfig{Timeout: time.Second})
	}()
	select {
	case <-called:
		t.Fatal("auto self-update ran before the active update released the lock")
	case <-time.After(100 * time.Millisecond):
	}
	svc.updateMu.Unlock()
	select {
	case applied := <-done:
		if !applied {
			t.Fatal("expected auto self-update to continue after lock release")
		}
	case <-ctx.Done():
		t.Fatal("auto self-update did not resume after lock release")
	}
}

func TestRunServiceContextAutoSelfUpdateContinuesWhenAlreadyCurrent(t *testing.T) {
	runtimeDir := t.TempDir()
	launcherPort := freePortForTest(t)
	calls := make(chan selfupdate.Options, 3)
	original := selfUpdateRunner
	selfUpdateRunner = func(ctx context.Context, options selfupdate.Options) (selfupdate.Result, error) {
		calls <- options
		return selfupdate.Result{
			CheckResult: selfupdate.CheckResult{
				CurrentVersion:  "0.1.0",
				LatestVersion:   "0.1.0",
				TargetVersion:   "0.1.0",
				Platform:        "test-platform",
				UpdateAvailable: false,
			},
			Message: "launcher 已是最新版本",
		}, nil
	}
	t.Cleanup(func() {
		selfUpdateRunner = original
	})
	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			ListenAddr: net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", launcherPort)),
			SelfUpdate: config.SelfUpdateConfig{
				Enabled:      true,
				InitialDelay: 0,
				Interval:     30 * time.Millisecond,
				Timeout:      time.Second,
				Restart:      true,
			},
		},
		state:     runstate.DeviceState{Status: "running"},
		processes: map[string]int{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunServiceContext(ctx, svc)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-calls:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for periodic self-update check")
		}
	}
	select {
	case err := <-errCh:
		t.Fatalf("service should keep running when already current, err=%v", err)
	default:
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run service context: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for launcher shutdown")
	}
}

func TestRunCLIUpdateRepeatsAtConfiguredInterval(t *testing.T) {
	calls := make(chan struct{}, 3)
	original := cliUpdateRunner
	cliUpdateRunner = func(context.Context, config.Config, io.Writer) (string, bool, error) {
		calls <- struct{}{}
		return "1.0.0", false, nil
	}
	t.Cleanup(func() {
		cliUpdateRunner = original
	})
	svc := &service{
		cfg: config.Config{
			RuntimeDir: t.TempDir(),
			SelfUpdate: config.SelfUpdateConfig{Interval: 20 * time.Millisecond},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.runCLIUpdate(ctx)

	for i := 0; i < 2; i++ {
		select {
		case <-calls:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for repeated CLI update check")
		}
	}
}

func TestAgentEnvDisablesProjectConfigByDefault(t *testing.T) {
	svc := &service{
		cfg: config.Config{
			Relay: config.RelayConfig{
				URL:         "wss://example.com/ws/device",
				OperatorKey: "opk_test",
				MachineID:   "m_test",
			},
			Agent: config.AgentConfig{
				Env: map[string]string{},
			},
		},
	}

	env := svc.agentEnv(model.AgentConfig{AgentID: "agent_test", ProjectDir: "/tmp/chat-codex"})
	if got := env["OPENCODE_DISABLE_PROJECT_CONFIG"]; got != "1" {
		t.Fatalf("expected OPENCODE_DISABLE_PROJECT_CONFIG=1, got %q", got)
	}
	if got := env["OPENCODE_RELAY_PERMISSION_MODE"]; got != "auto-approve" {
		t.Fatalf("expected default permission mode auto-approve, got %q", got)
	}
	if got := env["OPENCODE_PROJECT_ROOT"]; got != "/tmp/chat-codex" {
		t.Fatalf("expected OPENCODE_PROJECT_ROOT=/tmp/chat-codex, got %q", got)
	}
	if got := env["OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS"]; got != "true" {
		t.Fatalf("expected background subagents enabled, got %q", got)
	}
	if got := env["OPENCODE_SEMANTIC_AGENT_ID"]; got != defaultSemanticAgentID {
		t.Fatalf("expected default semantic agent id, got %q", got)
	}
}

func TestAgentEnvPreservesExplicitProjectConfigSetting(t *testing.T) {
	svc := &service{
		cfg: config.Config{
			Relay: config.RelayConfig{
				URL:         "wss://example.com/ws/device",
				OperatorKey: "opk_test",
				MachineID:   "m_test",
			},
			Agent: config.AgentConfig{
				Env: map[string]string{
					"OPENCODE_DISABLE_PROJECT_CONFIG": "false",
				},
			},
		},
	}

	env := svc.agentEnv(model.AgentConfig{AgentID: "agent_test", ProjectDir: "/tmp/chat-codex"})
	if got := env["OPENCODE_DISABLE_PROJECT_CONFIG"]; got != "false" {
		t.Fatalf("expected explicit project config setting to be preserved, got %q", got)
	}
}

func TestAgentCompactionConfigContentPreservesExistingConfig(t *testing.T) {
	content, ok := agentCompactionConfigContent(model.AgentConfig{
		CompactionThresholdPercent: 65,
	}, `{"model":"demo/gpt-5","mcp":{"verify":{"enabled":true}},"compaction":{"reserved":2000}}`)
	if !ok {
		t.Fatal("expected compaction override")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("decode generated config: %v", err)
	}
	if parsed["model"] != "demo/gpt-5" {
		t.Fatalf("expected model to survive, got %#v", parsed["model"])
	}
	compaction := parsed["compaction"].(map[string]any)
	if compaction["threshold_percent"] != float64(65) || compaction["reserved"] != float64(2000) {
		t.Fatalf("expected compaction settings to merge, got %#v", compaction)
	}
	if _, ok := parsed["mcp"].(map[string]any); !ok {
		t.Fatal("expected MCP config to survive")
	}
}

func TestAgentCompactionConfigContentResetRemovesManagedThreshold(t *testing.T) {
	content, ok := agentCompactionConfigContent(model.AgentConfig{}, `{"model":"demo/gpt-5","compaction":{"threshold_percent":65,"reserved":2000}}`)
	if !ok {
		t.Fatal("expected reset to update generated config")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("decode reset config: %v", err)
	}
	compaction := parsed["compaction"].(map[string]any)
	if _, exists := compaction["threshold_percent"]; exists {
		t.Fatalf("expected threshold to be removed, got %#v", compaction)
	}
	if compaction["reserved"] != float64(2000) {
		t.Fatalf("expected unrelated compaction setting to survive, got %#v", compaction)
	}
}

func TestNormalizeCompactionThresholdPercent(t *testing.T) {
	for _, value := range []int{1, 50, 100} {
		if got, err := normalizeCompactionThresholdPercent(value); err != nil || got != value {
			t.Fatalf("threshold %d: got=%d err=%v", value, got, err)
		}
	}
	for _, value := range []int{-1, 101} {
		if _, err := normalizeCompactionThresholdPercent(value); err == nil {
			t.Fatalf("expected threshold %d to fail", value)
		}
	}
}

func TestEnvConfigRevisionRejectsStaleWrites(t *testing.T) {
	runtimeDir := t.TempDir()
	agentID := "agent_env_revision"
	if err := writeAgentConfig(filepath.Join(runtimeDir, "agents", agentID), model.AgentConfig{
		AgentID: agentID, ProjectDir: runtimeDir,
	}); err != nil {
		t.Fatalf("write agent config: %v", err)
	}
	svc := &service{
		cfg:    config.Config{RuntimeDir: runtimeDir, Agent: config.AgentConfig{Env: map[string]string{}}},
		agents: []model.Agent{{AgentID: agentID, Name: "Agent", ProjectDir: runtimeDir}},
	}
	initial, err := svc.EnvConfig()
	if err != nil || initial.Revision == "" {
		t.Fatalf("load initial env config: info=%+v err=%v", initial, err)
	}
	updated, err := svc.SaveEnvConfig(model.DeviceEnvConfigInput{
		GlobalEnvironment: map[string]string{"VERIFY_API_TOKEN": "vat_global"},
		AgentID:           agentID,
		AgentEnvironment:  map[string]string{"VERIFY_API_TOKEN": "vat_agent"},
		ExpectedRevision:  initial.Revision,
	})
	if err != nil {
		t.Fatalf("save env config: %v", err)
	}
	if updated.Revision == initial.Revision {
		t.Fatal("expected revision to change")
	}
	if _, err := svc.SaveEnvConfig(model.DeviceEnvConfigInput{
		GlobalEnvironment: map[string]string{"VERIFY_API_TOKEN": "vat_stale"},
		ExpectedRevision:  initial.Revision,
	}); err == nil || !strings.Contains(err.Error(), "其他客户端") {
		t.Fatalf("expected stale revision conflict, got %v", err)
	}
	current, err := svc.EnvConfig()
	if err != nil {
		t.Fatalf("reload env config: %v", err)
	}
	if current.GlobalEnvironment["VERIFY_API_TOKEN"] != "vat_global" {
		t.Fatalf("stale write changed global environment: %+v", current.GlobalEnvironment)
	}
}

func TestEnvConfigReadsRestrictExistingCredentialFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	runtimeDir := t.TempDir()
	envPath := filepath.Join(runtimeDir, envConfigFileName)
	if err := os.WriteFile(envPath, []byte(`{"global_environment":{"VERIFY_API_TOKEN":"secret"}}`), 0o644); err != nil {
		t.Fatalf("write env config: %v", err)
	}
	agentID := "agent_permissions"
	agentDir := filepath.Join(runtimeDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent config: %v", err)
	}
	agentPath := filepath.Join(agentDir, "config.json")
	if err := os.WriteFile(agentPath, []byte(`{"agent_id":"agent_permissions","user_env":{"VERIFY_API_TOKEN":"secret"}}`), 0o644); err != nil {
		t.Fatalf("write agent config: %v", err)
	}
	if _, err := loadEnvConfig(runtimeDir); err != nil {
		t.Fatalf("load env config: %v", err)
	}
	if _, err := readAgentConfig(runtimeDir, agentID); err != nil {
		t.Fatalf("read agent config: %v", err)
	}
	for _, path := range []string{envPath, agentPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("expected %s mode 0600, got %o", path, info.Mode().Perm())
		}
	}
}

func TestEnvConfigInvalidAgentDoesNotPartiallySaveGlobalEnvironment(t *testing.T) {
	runtimeDir := t.TempDir()
	if err := saveEnvConfig(runtimeDir, map[string]string{"KEEP": "original"}); err != nil {
		t.Fatalf("save initial env: %v", err)
	}
	svc := &service{cfg: config.Config{RuntimeDir: runtimeDir, Agent: config.AgentConfig{Env: map[string]string{}}}}
	if _, err := svc.SaveEnvConfig(model.DeviceEnvConfigInput{
		GlobalEnvironment: map[string]string{"KEEP": "changed"},
		AgentID:           "missing_agent",
	}); err == nil {
		t.Fatal("expected missing agent error")
	}
	stored, err := loadEnvConfig(runtimeDir)
	if err != nil {
		t.Fatalf("load env config: %v", err)
	}
	if stored["KEEP"] != "original" {
		t.Fatalf("global environment was partially saved: %+v", stored)
	}
}

func TestAgentEnvAppliesAgentMCPSelectionOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir opencode config: %v", err)
	}
	raw := `{
	  "mcp": {
		    "docs": {
		      "type": "remote",
		      "url": "https://docs.example.com/mcp"
		    },
		    "verify": {
		      "type": "local",
		      "command": ["node", "verify.js"],
		      "environment": {"VERIFY_API_TOKEN": "vat_real_token"},
		      "enabled": false
		    }
	  }
	}`
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write opencode config: %v", err)
	}
	svc := &service{
		cfg: config.Config{
			Relay: config.RelayConfig{
				URL:         "wss://example.com/ws/device",
				OperatorKey: "opk_test",
				MachineID:   "m_test",
			},
			Agent: config.AgentConfig{
				Env: map[string]string{},
			},
		},
	}
	existing := `{"model":"demo/gpt-5","provider":{"demo":{"name":"Demo"}},"mcp":{"legacy":{"enabled":true}}}`

	env := svc.agentEnv(model.AgentConfig{
		AgentID:    "agent_test",
		ProjectDir: "/tmp/chat-codex",
		UserEnv: map[string]string{
			"OPENCODE_CONFIG_CONTENT": existing,
		},
		MCPMode:    "custom",
		MCPServers: []string{"verify"},
		MCPServerConfigs: []model.DeviceMCPServerInfo{{
			Name:    "verify",
			Type:    "local",
			Enabled: true,
			Command: []string{"npx", "-y", "@ktbtw/verify-mcp"},
			Environment: map[string]string{
				"VERIFY_API_TOKEN": "vat_xxx_replace_me",
			},
		}},
	})

	content := env["OPENCODE_CONFIG_CONTENT"]
	if content == "" {
		t.Fatal("expected OPENCODE_CONFIG_CONTENT override")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("decode generated content: %v content=%s", err, content)
	}
	if got := parsed["model"]; got != "demo/gpt-5" {
		t.Fatalf("expected non-mcp config to be preserved, got model=%v content=%s", got, content)
	}
	if got := mcpEnabledForTest(t, parsed, "docs"); got {
		t.Fatalf("expected unselected global docs server to be isolated, content=%s", content)
	}
	if got := mcpEnabledForTest(t, parsed, "verify"); !got {
		t.Fatalf("expected verify to be enabled, content=%s", content)
	}
	if mcp := parsed["mcp"].(map[string]any); len(mcp) != 2 {
		t.Fatalf("expected only available mcp override entries, got %#v", mcp)
	}
	if strings.Contains(content, "replace_me") {
		t.Fatalf("expected semantic placeholder credentials not to override the global server, content=%s", content)
	}
	verifyOverride := parsed["mcp"].(map[string]any)["verify"].(map[string]any)
	if _, ok := verifyOverride["environment"]; ok {
		t.Fatalf("expected global verify environment to remain authoritative, content=%s", content)
	}

	agentTokenEnv := svc.agentEnv(model.AgentConfig{
		AgentID:    "agent_test",
		ProjectDir: "/tmp/chat-codex",
		UserEnv: map[string]string{
			"VERIFY_API_TOKEN":        "vat_agent_token",
			"OPENCODE_CONFIG_CONTENT": existing,
		},
		MCPMode:    "custom",
		MCPServers: []string{"verify"},
	})
	var agentTokenContent map[string]any
	if err := json.Unmarshal([]byte(agentTokenEnv["OPENCODE_CONFIG_CONTENT"]), &agentTokenContent); err != nil {
		t.Fatalf("decode agent credential override: %v", err)
	}
	agentVerify := agentTokenContent["mcp"].(map[string]any)["verify"].(map[string]any)
	agentVerifyEnv := agentVerify["environment"].(map[string]any)
	if got := agentVerifyEnv["VERIFY_API_TOKEN"]; got != "vat_agent_token" {
		t.Fatalf("expected per-Agent token to override shared MCP token, got %v", got)
	}

	emptyEnv := svc.agentEnv(model.AgentConfig{
		AgentID:    "agent_test",
		ProjectDir: "/tmp/chat-codex",
		MCPMode:    "custom",
	})
	var emptyParsed map[string]any
	if err := json.Unmarshal([]byte(emptyEnv["OPENCODE_CONFIG_CONTENT"]), &emptyParsed); err != nil {
		t.Fatalf("decode empty selection content: %v", err)
	}
	if got := mcpEnabledForTest(t, emptyParsed, "docs"); got {
		t.Fatalf("expected global docs server to be disabled for empty custom selection")
	}
	if got := mcpEnabledForTest(t, emptyParsed, "verify"); got {
		t.Fatalf("expected verify to be disabled for empty custom selection")
	}

	inheritEnv := svc.agentEnv(model.AgentConfig{
		AgentID:    "agent_test",
		ProjectDir: "/tmp/chat-codex",
		UserEnv: map[string]string{
			"OPENCODE_CONFIG_CONTENT": existing,
		},
		MCPMode: "inherit",
	})
	if got := inheritEnv["OPENCODE_CONFIG_CONTENT"]; got != existing {
		t.Fatalf("expected inherit mode to leave existing config content untouched, got %s", got)
	}
}

func TestAgentMCPServerContentDropsCredentialPlaceholders(t *testing.T) {
	content, err := agentMCPServerContent(model.DeviceMCPServerInfo{
		Name:    "verify",
		Type:    "local",
		Enabled: true,
		Command: []string{"npx", "-y", "@ktbtw/verify-mcp"},
		Environment: map[string]string{
			"VERIFY_API_TOKEN":     "vat_xxx_replace_me",
			"VERIFY_PROTECT_TOKEN": "vpt_xxx",
			"VERIFY_BASE_URL":      "https://verify.example.com",
		},
	})
	if err != nil {
		t.Fatalf("encode MCP server: %v", err)
	}
	env, ok := content["environment"].(map[string]string)
	if !ok {
		t.Fatalf("expected sanitized environment, got %#v", content["environment"])
	}
	if _, ok := env["VERIFY_API_TOKEN"]; ok {
		t.Fatalf("expected API token placeholder removed, got %+v", env)
	}
	if _, ok := env["VERIFY_PROTECT_TOKEN"]; ok {
		t.Fatalf("expected protect token placeholder removed, got %+v", env)
	}
	if env["VERIFY_BASE_URL"] != "https://verify.example.com" {
		t.Fatalf("expected base URL preserved, got %+v", env)
	}
}

func TestAgentEnvAppliesSemanticAgentIsolationAndSkills(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir opencode config: %v", err)
	}
	raw := `{
	  "mcp": {
	    "docs": {
	      "type": "remote",
	      "url": "https://docs.example.com/mcp"
	    },
	    "idalib-mcp": {
	      "type": "local",
	      "command": ["uvx", "idalib-mcp"]
	    }
	  }
	}`
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write opencode config: %v", err)
	}
	runtimeDir := t.TempDir()
	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			Relay: config.RelayConfig{
				URL:         "wss://example.com/ws/device",
				OperatorKey: "opk_test",
				MachineID:   "m_test",
			},
			Agent: config.AgentConfig{
				Env: map[string]string{},
			},
		},
	}

	env := svc.agentEnv(model.AgentConfig{
		AgentID:         "agent_reverse",
		ProjectDir:      "/tmp/chat-codex",
		SemanticAgentID: "reverse-expert",
		MCPMode:         "custom",
		MCPServers:      []string{"idalib-mcp"},
		UserEnv: map[string]string{
			"OPENCODE_CONFIG_CONTENT": `{"model":"demo/gpt-5"}`,
		},
	})

	if got := env["OPENCODE_DISABLE_EXTERNAL_SKILLS"]; got != "1" {
		t.Fatalf("expected external skills disabled, got %q", got)
	}
	if got := env["OPENCODE_SEMANTIC_AGENT_ISOLATION"]; got != "1" {
		t.Fatalf("expected semantic isolation flag, got %q", got)
	}
	content := env["OPENCODE_CONFIG_CONTENT"]
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("decode semantic content: %v content=%s", err, content)
	}
	if got := parsed["model"]; got != "demo/gpt-5" {
		t.Fatalf("expected existing model preserved, got %v content=%s", got, content)
	}
	if got := parsed["default_agent"]; got != "reverse-expert" {
		t.Fatalf("expected reverse-expert default agent, got %v content=%s", got, content)
	}
	if got := mcpEnabledForTest(t, parsed, "idalib-mcp"); !got {
		t.Fatalf("expected idalib-mcp enabled, content=%s", content)
	}
	if got := mcpEnabledForTest(t, parsed, "docs"); got {
		t.Fatalf("expected unrelated global docs server to be isolated, content=%s", content)
	}
	agentMap, ok := parsed["agent"].(map[string]any)
	if !ok {
		t.Fatalf("expected agent map, content=%s", content)
	}
	reverseAgent, ok := agentMap["reverse-expert"].(map[string]any)
	if !ok || strings.TrimSpace(fmt.Sprint(reverseAgent["prompt"])) == "" {
		t.Fatalf("expected reverse prompt, content=%s", content)
	}
	for _, required := range []string{"IDA MCP", "超过 60 分钟", "函数边界", "热更新", "native Hook callback"} {
		if !strings.Contains(fmt.Sprint(reverseAgent["prompt"]), required) {
			t.Errorf("reverse prompt is missing %q", required)
		}
	}
	skills, ok := parsed["skills"].(map[string]any)
	if !ok {
		t.Fatalf("expected skills map, content=%s", content)
	}
	paths, ok := skills["paths"].([]any)
	if !ok || len(paths) != 1 {
		t.Fatalf("expected one semantic skill path, got %#v content=%s", skills["paths"], content)
	}
	urls, ok := skills["urls"].([]any)
	if !ok || len(urls) != 0 {
		t.Fatalf("expected semantic skill urls to be cleared, got %#v content=%s", skills["urls"], content)
	}
	skillRoot := fmt.Sprint(paths[0])
	if _, err := os.Stat(filepath.Join(skillRoot, "reverse-android-analysis", "SKILL.md")); err != nil {
		t.Fatalf("expected generated semantic skill: %v", err)
	}
	androidSkill, err := os.ReadFile(filepath.Join(skillRoot, "reverse-android-analysis", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"IDA MCP", "超过 60 分钟", "函数边界", "热更新", "native callback"} {
		if !bytes.Contains(androidSkill, []byte(required)) {
			t.Errorf("reverse-android-analysis skill is missing %q", required)
		}
	}
	if !strings.HasPrefix(skillRoot, filepath.Join(runtimeDir, "semantic-agents")) {
		t.Fatalf("expected skill root under runtime dir, got %s", skillRoot)
	}
}

func TestAgentEnvAppliesSemanticMCPConfigsWithoutGlobalMCP(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("HOME", runtimeDir)
	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			Agent: config.AgentConfig{
				Env: map[string]string{},
			},
		},
	}
	env := svc.agentEnv(model.AgentConfig{
		AgentID:    "agent_semantic_mcp",
		ProjectDir: runtimeDir,
		MCPMode:    "custom",
		MCPServers: []string{"frida-mcp"},
		MCPServerConfigs: []model.DeviceMCPServerInfo{{
			Name:    "frida-mcp",
			Type:    "local",
			Enabled: true,
			Command: []string{"npx", "-y", "frida-mcp"},
		}},
	})
	content := env["OPENCODE_CONFIG_CONTENT"]
	if content == "" {
		t.Fatal("expected OPENCODE_CONFIG_CONTENT")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("decode generated content: %v content=%s", err, content)
	}
	if got := mcpEnabledForTest(t, parsed, "frida-mcp"); !got {
		t.Fatalf("expected frida-mcp enabled from semantic config, content=%s", content)
	}
	mcp := parsed["mcp"].(map[string]any)
	server := mcp["frida-mcp"].(map[string]any)
	if got := server["type"]; got != "local" {
		t.Fatalf("expected local server config, got %#v", server)
	}
	command, ok := server["command"].([]any)
	if !ok || len(command) != 3 || command[2] != "frida-mcp" {
		t.Fatalf("expected command from semantic config, got %#v", server["command"])
	}
}

func TestAgentEnvResolvesSemanticMCPHomeTemplate(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("HOME", runtimeDir)
	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			Agent: config.AgentConfig{
				Env: map[string]string{},
			},
		},
	}
	env := svc.agentEnv(model.AgentConfig{
		AgentID:    "agent_semantic_mcp_template",
		ProjectDir: runtimeDir,
		MCPMode:    "custom",
		MCPServerConfigs: []model.DeviceMCPServerInfo{{
			Name:    "frida-analykit",
			Type:    "local",
			Enabled: true,
			Command: []string{"frida-analykit-mcp", "--config", "${MCP_HOME}/mcp.toml"},
		}},
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &parsed); err != nil {
		t.Fatalf("decode generated content: %v", err)
	}
	mcp := parsed["mcp"].(map[string]any)
	server := mcp["frida-analykit"].(map[string]any)
	command := server["command"].([]any)
	want := filepath.Join(runtimeDir, "mcps", "frida-analykit", "mcp.toml")
	if command[2] != want {
		t.Fatalf("expected template resolved to %s, got %#v", want, command)
	}
}

func TestAgentEnvNormalizesJADXMCPToStdioTransport(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("HOME", runtimeDir)
	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			Agent: config.AgentConfig{
				Env: map[string]string{},
			},
		},
	}
	env := svc.agentEnv(model.AgentConfig{
		AgentID:    "agent_jadx_mcp_stdio",
		ProjectDir: runtimeDir,
		MCPMode:    "custom",
		MCPServerConfigs: []model.DeviceMCPServerInfo{{
			Name:    "jadx-mcp-server",
			Type:    "local",
			Enabled: true,
			Command: []string{"uv", "run", "${MCP_HOME}/jadx_mcp_server.py", "--http", "--host", "127.0.0.1", "--port", "8651"},
		}},
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &parsed); err != nil {
		t.Fatalf("decode generated content: %v", err)
	}
	mcp := parsed["mcp"].(map[string]any)
	server := mcp["jadx-mcp-server"].(map[string]any)
	command := server["command"].([]any)
	for _, arg := range command {
		switch arg {
		case "--http", "--host", "--port", "8651":
			t.Fatalf("expected JADX MCP stdio command without HTTP flags, got %#v", command)
		}
	}
	if got := command[len(command)-1]; got != filepath.Join(runtimeDir, "mcps", "jadx-mcp-server", "jadx_mcp_server.py") {
		t.Fatalf("expected resolved script path at end, got %#v", command)
	}
}

func TestAgentEnvSkipsUnresolvedSemanticMCPPlaceholder(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("HOME", runtimeDir)
	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			Agent: config.AgentConfig{
				Env: map[string]string{},
			},
		},
	}
	env := svc.agentEnv(model.AgentConfig{
		AgentID:    "agent_semantic_mcp_placeholder",
		ProjectDir: runtimeDir,
		MCPMode:    "custom",
		MCPServerConfigs: []model.DeviceMCPServerInfo{{
			Name:    "frida-analykit",
			Type:    "local",
			Enabled: true,
			Command: []string{"frida-analykit-mcp", "--config", "/ABSOLUTE/PATH/frida-analykit/mcp.toml"},
		}},
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &parsed); err != nil {
		t.Fatalf("decode generated content: %v", err)
	}
	mcp := parsed["mcp"].(map[string]any)
	if _, ok := mcp["frida-analykit"]; ok {
		t.Fatalf("expected unresolved MCP skipped, got %s", env["OPENCODE_CONFIG_CONTENT"])
	}
}

func TestSemanticAgentConfigContentUsesPersistedProfileSnapshot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	runtimeDir := t.TempDir()
	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			Relay: config.RelayConfig{
				URL:         "wss://example.com/ws/device",
				OperatorKey: "opk_test",
				MachineID:   "m_test",
			},
			Agent: config.AgentConfig{
				Env: map[string]string{},
			},
		},
	}
	env := svc.agentEnv(model.AgentConfig{
		AgentID:         "agent_snapshot",
		ProjectDir:      "/tmp/chat-codex",
		SemanticAgentID: "reverse-expert",
		SemanticAgentProfile: &model.SemanticAgentProfile{
			ID:           "reverse-expert",
			Name:         "逆向专家编辑版",
			Description:  "后台编辑后的配置",
			Color:        "primary",
			OpencodeName: "reverse-expert",
			Prompt:       "后台编辑后的逆向专家提示词",
			SkillDefinitions: []model.SemanticAgentSkill{{
				Name:        "reverse-custom",
				Description: "后台编辑后的 skill",
				Content:     "---\nname: reverse-custom\n---\n# 后台编辑后的 skill",
				PackageFiles: []model.SkillPackageFile{
					{Path: "references/guide.md", Content: "# Guide"},
					{Path: "../blocked.md", Content: "blocked"},
					{Path: "scripts/check.sh", Content: "#!/bin/sh\necho ok", Executable: true},
				},
			}},
			ToolPermissions: map[string]any{
				"read": "allow",
			},
		},
	})
	content := env["OPENCODE_CONFIG_CONTENT"]
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("decode semantic content: %v content=%s", err, content)
	}
	agentMap, ok := parsed["agent"].(map[string]any)
	if !ok {
		t.Fatalf("expected agent map, content=%s", content)
	}
	reverseAgent, ok := agentMap["reverse-expert"].(map[string]any)
	if !ok || fmt.Sprint(reverseAgent["prompt"]) != "后台编辑后的逆向专家提示词" {
		t.Fatalf("expected snapshot prompt, got %#v content=%s", reverseAgent, content)
	}
	skills, ok := parsed["skills"].(map[string]any)
	if !ok {
		t.Fatalf("expected skills map, content=%s", content)
	}
	paths, ok := skills["paths"].([]any)
	if !ok || len(paths) != 1 {
		t.Fatalf("expected one semantic skill path, got %#v content=%s", skills["paths"], content)
	}
	skillRoot := fmt.Sprint(paths[0])
	if _, err := os.Stat(filepath.Join(skillRoot, "reverse-custom", "SKILL.md")); err != nil {
		t.Fatalf("expected generated custom semantic skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillRoot, "reverse-custom", "references", "guide.md")); err != nil {
		t.Fatalf("expected generated package reference: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillRoot, "reverse-custom", "blocked.md")); !os.IsNotExist(err) {
		t.Fatalf("expected unsafe package path to be skipped, err=%v", err)
	}
}

func TestAgentEnvMergesExtraSkillsWithoutReplacingSemanticDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	svc := &service{
		cfg: config.Config{
			RuntimeDir: t.TempDir(),
			Relay: config.RelayConfig{
				URL:         "wss://example.com/ws/device",
				OperatorKey: "opk_test",
				MachineID:   "m_test",
			},
			Agent: config.AgentConfig{Env: map[string]string{}},
		},
	}
	env := svc.agentEnv(model.AgentConfig{
		AgentID:         "agent_reverse",
		ProjectDir:      "/tmp/chat-codex",
		SemanticAgentID: "reverse-expert",
		ExtraSkillDefinitions: []model.SemanticAgentSkill{
			{Name: "custom-review", Description: "额外审查", Content: "# Custom review"},
			{Name: "reverse-android-analysis", Content: "# Must not replace default"},
		},
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &parsed); err != nil {
		t.Fatalf("decode semantic content: %v", err)
	}
	skills, ok := parsed["skills"].(map[string]any)
	if !ok {
		t.Fatalf("expected skills config, got %#v", parsed["skills"])
	}
	paths, ok := skills["paths"].([]any)
	if !ok || len(paths) != 1 {
		t.Fatalf("expected one skill root, got %#v", skills["paths"])
	}
	root := fmt.Sprint(paths[0])
	if _, err := os.Stat(filepath.Join(root, "custom-review", "SKILL.md")); err != nil {
		t.Fatalf("expected extra skill to be written: %v", err)
	}
	defaultContent, err := os.ReadFile(filepath.Join(root, "reverse-android-analysis", "SKILL.md"))
	if err != nil {
		t.Fatalf("expected default skill to remain: %v", err)
	}
	if strings.Contains(string(defaultContent), "Must not replace default") {
		t.Fatal("extra skill must not replace a semantic agent default skill")
	}
}

func TestImportAgentSkillStoresOnDeviceAndMakesItSelectable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	configRoot := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	if err := os.MkdirAll(configRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configRoot, "opencode.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	agentID := "agent_import"
	if err := writeAgentConfig(filepath.Join(runtimeDir, "agents", agentID), model.AgentConfig{
		AgentID: agentID, ProjectDir: runtimeDir, SemanticAgentID: "coding-assistant", Env: map[string]string{},
	}); err != nil {
		t.Fatal(err)
	}
	svc := &service{
		cfg:    config.Config{RuntimeDir: runtimeDir, Agent: config.AgentConfig{Env: map[string]string{}}},
		agents: []model.Agent{{AgentID: agentID, Enabled: true}},
	}
	selection, err := svc.ImportAgentSkill(model.DeviceAgentSkillImportInput{
		AgentID: agentID,
		Skill: model.SemanticAgentSkill{
			Name:        "local-review",
			Description: "本地审查",
			Content:     "# Local review",
			PackageFiles: []model.SkillPackageFile{{
				Path: "references/checklist.md", Content: "# Checklist",
			}},
		},
	})
	if err != nil {
		t.Fatalf("import skill: %v", err)
	}
	if !containsString(selection.ExtraSkills, "local-review") {
		t.Fatalf("expected imported skill selected, got %+v", selection)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "skills", "local-review", "SKILL.md")); err != nil {
		t.Fatalf("expected local SKILL.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "skills", "local-review", "references", "checklist.md")); err != nil {
		t.Fatalf("expected local package file: %v", err)
	}
	otherID := "agent_other"
	if err := writeAgentConfig(filepath.Join(runtimeDir, "agents", otherID), model.AgentConfig{
		AgentID: otherID, ProjectDir: runtimeDir, SemanticAgentID: "coding-assistant", Env: map[string]string{},
	}); err != nil {
		t.Fatal(err)
	}
	svc.agents = append(svc.agents, model.Agent{AgentID: otherID, Enabled: true})
	other, err := svc.AgentSkillSelection(model.DeviceAgentSkillSelectionInput{AgentID: otherID})
	if err != nil {
		t.Fatalf("read local skill selection: %v", err)
	}
	if !hasSkillDefinition(other.AvailableSkills, "local-review") {
		t.Fatalf("expected local skill available to another agent, got %+v", other.AvailableSkills)
	}
}

func TestAgentMCPSelectionFiltersUnavailableServers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir opencode config: %v", err)
	}
	raw := `{
	  "mcp": {
	    "docs": {
	      "type": "remote",
	      "url": "https://docs.example.com/mcp"
	    },
	    "verify": {
	      "type": "local",
	      "command": ["node", "verify.js"]
	    }
	  }
	}`
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write opencode config: %v", err)
	}
	svc := &service{}

	info, err := svc.agentMCPSelectionFromConfig(model.AgentConfig{
		AgentID:    "agent_test",
		MCPMode:    "custom",
		MCPServers: []string{"missing", "verify", "docs", "verify"},
	})
	if err != nil {
		t.Fatalf("selection from config: %v", err)
	}
	if strings.Join(info.SelectedServers, ",") != "docs,verify" {
		t.Fatalf("expected unavailable and duplicate servers to be filtered, got %+v", info.SelectedServers)
	}
}

func mcpEnabledForTest(t *testing.T, parsed map[string]any, name string) bool {
	t.Helper()
	mcp, ok := parsed["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("missing mcp map in %#v", parsed)
	}
	server, ok := mcp[name].(map[string]any)
	if !ok {
		t.Fatalf("missing mcp server %s in %#v", name, mcp)
	}
	enabled, ok := server["enabled"].(bool)
	if !ok {
		t.Fatalf("missing enabled flag for %s in %#v", name, server)
	}
	return enabled
}

func TestRefreshManagedAgentEnvOverridesStaleRelayEnv(t *testing.T) {
	svc := &service{
		cfg: config.Config{
			Relay: config.RelayConfig{
				URL:         "wss://new.example.com/ws/device",
				OperatorKey: "opk_new",
				MachineID:   "m_new",
			},
			Agent: config.AgentConfig{
				Env: map[string]string{},
			},
		},
	}

	cfg := &model.AgentConfig{
		AgentID:    "agent_test",
		ProjectDir: "/tmp/chat-codex",
		Env: map[string]string{
			"OPENCODE_RELAY_URL":          "wss://old.example.com/ws/device",
			"OPENCODE_RELAY_OPERATOR_KEY": "opk_old",
		},
	}

	svc.refreshManagedAgentEnv(cfg)

	if got := cfg.Env["OPENCODE_RELAY_URL"]; got != "wss://new.example.com/ws/device" {
		t.Fatalf("expected refreshed relay url, got %q", got)
	}
	if got := cfg.Env["OPENCODE_RELAY_OPERATOR_KEY"]; got != "opk_new" {
		t.Fatalf("expected refreshed operator key, got %q", got)
	}
	if got := cfg.Env["OPENCODE_DISABLE_PROJECT_CONFIG"]; got != "1" {
		t.Fatalf("expected refreshed project config flag, got %q", got)
	}
}

func TestAgentSemanticPreflightInstallsArchiveRuntimeAndAppliesEnv(t *testing.T) {
	runtimeDir := t.TempDir()
	agentID := "agent_runtime"
	agentDir := filepath.Join(runtimeDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	if err := writeAgentConfig(agentDir, model.AgentConfig{
		AgentID:    agentID,
		Name:       "Runtime Agent",
		ProjectDir: t.TempDir(),
		Port:       freePortForTest(t),
	}); err != nil {
		t.Fatalf("write agent config: %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), "fake-runtime.zip")
	if err := writeFakeRuntimeZip(archivePath); err != nil {
		t.Fatalf("write runtime zip: %v", err)
	}
	sum, err := fileSHA256(archivePath)
	if err != nil {
		t.Fatalf("sha256: %v", err)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(filepath.Dir(archivePath))))
	defer server.Close()
	profile := model.SemanticAgentProfile{
		ID:           "runtime-test-agent",
		Name:         "运行时测试 Agent",
		Description:  "测试托管运行时预检",
		OpencodeName: "runtime-test-agent",
		Prompt:       "测试提示词",
		Enabled:      true,
		RuntimeRequirements: []model.SemanticAgentRuntime{{
			RuntimeID:         "fake-runtime",
			VersionConstraint: "1.0.0",
			Required:          true,
		}},
	}
	svc := &service{
		cfg: config.Config{
			RuntimeDir: runtimeDir,
			Relay: config.RelayConfig{
				URL:         "wss://relay.example.com/ws/device",
				OperatorKey: "opk_test",
				MachineID:   "m_test",
			},
			Agent: config.AgentConfig{Env: map[string]string{}},
		},
		state: runstate.DeviceState{Status: "running", AgentCount: 1},
		agents: []model.Agent{{
			AgentID:   agentID,
			Name:      "Runtime Agent",
			Enabled:   true,
			Status:    "running",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}},
		processes: map[string]int{},
	}
	var statuses []model.RuntimePreflightStatusPayload
	err = svc.StartAgentSemanticPreflight(model.RuntimePreflightStartPayload{
		JobID:               "job_runtime_test",
		MachineID:           "m_test",
		LauncherAgentID:     agentID,
		SemanticAgentID:     profile.ID,
		AutoRepair:          true,
		ApplyRecommendedMCP: true,
		SemanticAgent:       &profile,
		Runtimes: []model.RuntimeCatalogItem{{
			ID:              "fake-runtime",
			Name:            "Fake Runtime",
			InstallStrategy: "archive",
			Enabled:         true,
			ExecutableNames: []string{fakeRuntimeExecutableName()},
			Versions: []model.RuntimeVersion{{
				ID:              "fake-runtime-1",
				RuntimeID:       "fake-runtime",
				Version:         "1.0.0",
				DefaultSelected: true,
				Enabled:         true,
			}},
			Artifacts: []model.RuntimeArtifact{{
				ID:          "fake-runtime-darwin",
				RuntimeID:   "fake-runtime",
				VersionID:   "fake-runtime-1",
				Platform:    runtime.GOOS,
				Arch:        runtime.GOARCH,
				PackageKind: "zip",
				Filename:    filepath.Base(archivePath),
				SHA256:      sum,
				BinPaths:    []string{"bin"},
				Enabled:     true,
			}},
			Mirrors: []model.RuntimeMirror{{
				ID:          "fake-runtime-local",
				RuntimeID:   "fake-runtime",
				VersionID:   "fake-runtime-1",
				Name:        "本地测试镜像",
				BaseURL:     server.URL,
				URLTemplate: "{base}/{filename}",
				Platform:    runtime.GOOS,
				Arch:        runtime.GOARCH,
				Enabled:     true,
			}},
		}},
	}, func(status model.RuntimePreflightStatusPayload) error {
		statuses = append(statuses, status)
		return nil
	})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if len(statuses) == 0 || statuses[len(statuses)-1].Status != "completed" {
		t.Fatalf("expected completed status, got %+v", statuses)
	}
	manifest, ok := svc.bestRuntimeManifest("fake-runtime", "1.0.0")
	if !ok {
		t.Fatal("expected runtime manifest")
	}
	if !strings.Contains(manifest.InstallDir, filepath.Join("runtimes", "fake-runtime", "1.0.0")) {
		t.Fatalf("unexpected install dir: %s", manifest.InstallDir)
	}
	stored, err := readAgentConfig(runtimeDir, agentID)
	if err != nil {
		t.Fatalf("read stored config: %v", err)
	}
	pathValue := stored.Env[pathKey()]
	if !strings.Contains(pathValue, filepath.Join(manifest.InstallDir, "bin")) {
		t.Fatalf("expected managed runtime bin in PATH, got %s", pathValue)
	}
	if stored.SemanticAgentProfile == nil || stored.SemanticAgentProfile.ID != profile.ID {
		t.Fatalf("expected semantic profile snapshot, got %+v", stored.SemanticAgentProfile)
	}
}

func TestRuntimePreflightSelectsWindowsArtifactWhenPlatformIsWindows(t *testing.T) {
	original := runtimePlatformForPreflight
	runtimePlatformForPreflight = func() (string, string) { return "windows", "amd64" }
	t.Cleanup(func() { runtimePlatformForPreflight = original })

	svc := &service{cfg: config.Config{RuntimeDir: t.TempDir()}}
	runner := runtimePreflightRunner{service: svc}
	version := model.RuntimeVersion{
		ID:      "node-24",
		Version: "24.0.0",
		Enabled: true,
	}
	item := model.RuntimeCatalogItem{
		ID:              "node",
		Name:            "Node.js",
		InstallStrategy: "archive",
		Artifacts: []model.RuntimeArtifact{
			{
				ID:          "node-darwin",
				RuntimeID:   "node",
				VersionID:   "node-24",
				Platform:    "darwin",
				Arch:        "arm64",
				PackageKind: "tar.gz",
				Filename:    "node-darwin.tar.gz",
				SHA256:      "darwin",
				Enabled:     true,
			},
			{
				ID:           "node-windows",
				RuntimeID:    "node",
				VersionID:    "node-24",
				Platform:     "windows",
				Arch:         "amd64",
				PackageKind:  "zip",
				Filename:     "node-windows.zip",
				SHA256:       "windows",
				DownloadPath: "https://cdn.example.com/node-windows.zip",
				Enabled:      true,
			},
		},
		Mirrors: []model.RuntimeMirror{
			{
				ID:          "npm-registry",
				RuntimeID:   "node",
				VersionID:   "node-24",
				BaseURL:     "https://registry.npmmirror.com",
				URLTemplate: "{base}",
				Platform:    "windows",
				Arch:        "amd64",
				Enabled:     true,
				Priority:    0,
			},
			{
				ID:          "mirror-darwin",
				RuntimeID:   "node",
				VersionID:   "node-24",
				BaseURL:     "https://mirror.example.com/node",
				URLTemplate: "{base}/{platform}/{arch}/{filename}",
				Platform:    "darwin",
				Arch:        "arm64",
				Enabled:     true,
				Priority:    1,
			},
			{
				ID:          "mirror-windows",
				RuntimeID:   "node",
				VersionID:   "node-24",
				BaseURL:     "https://mirror.example.com/node",
				URLTemplate: "{base}/{platform}/{arch}/{filename}",
				Platform:    "windows",
				Arch:        "amd64",
				Enabled:     true,
				Priority:    1,
			},
		},
	}
	candidates := runner.runtimeCandidates(item, version)
	if len(candidates) != 2 {
		t.Fatalf("expected mirror and artifact candidates for windows, got %+v", candidates)
	}
	if candidates[0].sourceID != "mirror-windows" || !strings.Contains(candidates[0].url, "/windows/amd64/node-windows.zip") {
		t.Fatalf("expected windows mirror first, got %+v", candidates[0])
	}
	if candidates[1].sourceID != "node-windows" || candidates[1].kind != "zip" {
		t.Fatalf("expected windows artifact fallback, got %+v", candidates[1])
	}
	for _, candidate := range candidates {
		if candidate.sourceID == "npm-registry" {
			t.Fatalf("registry mirror should not be used as archive download candidate: %+v", candidates)
		}
	}
}

func TestRuntimePreflightJavaConstraintUsesStableLTS(t *testing.T) {
	item := model.RuntimeCatalogItem{
		ID: "java",
		Versions: []model.RuntimeVersion{
			{ID: "java-25", Version: "25", DefaultSelected: true, VersionOrder: 10, Enabled: true},
			{ID: "java-21", Version: "21", VersionOrder: 20, Enabled: true},
			{ID: "java-17", Version: "17", VersionOrder: 30, Enabled: true},
		},
	}
	version, err := selectRuntimeVersion(item, ">=17 <22")
	if err != nil {
		t.Fatalf("select java version: %v", err)
	}
	if version.ID != "java-21" {
		t.Fatalf("expected Java 21 for Android-compatible constraint, got %+v", version)
	}
}

func TestRuntimePreflightArchiveGroupDefaultsToSingleDownload(t *testing.T) {
	t.Setenv("LAUNCHER_RUNTIME_ARCHIVE_CONCURRENCY", "")
	group := runtimePreflightTaskGroup{
		name: "archive",
		tasks: []runtimePreflightTask{
			{runtimeID: "node"},
			{runtimeID: "java"},
			{runtimeID: "uv"},
		},
	}
	if got := runtimeGroupConcurrency(group); got != 1 {
		t.Fatalf("expected archive runtime concurrency 1, got %d", got)
	}

	t.Setenv("LAUNCHER_RUNTIME_ARCHIVE_CONCURRENCY", "3")
	if got := runtimeGroupConcurrency(group); got != 3 {
		t.Fatalf("expected configured archive runtime concurrency 3, got %d", got)
	}

	t.Setenv("LAUNCHER_RUNTIME_ARCHIVE_CONCURRENCY", "99")
	if got := runtimeGroupConcurrency(group); got != 4 {
		t.Fatalf("expected archive runtime concurrency cap 4, got %d", got)
	}
}

func TestRuntimePreflightDownloadTimeoutScalesForLargeArtifacts(t *testing.T) {
	small := runtimeDownloadTimeout(20 * 1024 * 1024)
	if small < runtimeDownloadMinTimeout {
		t.Fatalf("expected small artifact timeout at least %s, got %s", runtimeDownloadMinTimeout, small)
	}
	javaSized := runtimeDownloadTimeout(200 * 1024 * 1024)
	if javaSized < 14*time.Minute {
		t.Fatalf("expected large Java artifact timeout to be long enough, got %s", javaSized)
	}
	huge := runtimeDownloadTimeout(4 * 1024 * 1024 * 1024)
	if huge != runtimeDownloadMaxTimeout {
		t.Fatalf("expected huge artifact timeout capped at %s, got %s", runtimeDownloadMaxTimeout, huge)
	}
}

func TestRuntimePreflightDownloadUsesLauncherUserAgent(t *testing.T) {
	var observedUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedUA = r.UserAgent()
		if observedUA != runtimeDownloadUserAgent {
			http.Error(w, "blocked user agent", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("runtime archive"))
	}))
	t.Cleanup(server.Close)

	runner := runtimePreflightRunner{
		service:  &service{cfg: config.Config{RuntimeDir: t.TempDir()}},
		payload:  model.RuntimePreflightStartPayload{JobID: "job-download-ua"},
		reporter: func(model.RuntimePreflightStatusPayload) error { return nil },
	}
	target := filepath.Join(t.TempDir(), "runtime.tar.gz")
	err := runner.downloadRuntimeCandidate(runtimeInstallCandidate{
		name:     "test mirror",
		url:      server.URL,
		filename: "runtime.tar.gz",
		timeout:  time.Minute,
	}, target, "uv", 10)
	if err != nil {
		t.Fatalf("download runtime candidate: %v", err)
	}
	if observedUA != runtimeDownloadUserAgent {
		t.Fatalf("expected launcher user agent %q, got %q", runtimeDownloadUserAgent, observedUA)
	}
}

func TestRuntimePreflightDownloadRetriesAndResumesInterruptedResponse(t *testing.T) {
	payload := bytes.Repeat([]byte("jadx-runtime-archive-"), 8192)
	digest := sha256.Sum256(payload)
	expectedSHA256 := hex.EncodeToString(digest[:])
	requestCount := 0
	secondRange := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"jadx-test"`)
		if requestCount == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload[:len(payload)/2])
			return
		}
		secondRange = r.Header.Get("Range")
		var offset int
		if _, err := fmt.Sscanf(secondRange, "bytes=%d-", &offset); err != nil {
			http.Error(w, "missing range", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)-offset))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(payload)-1, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[offset:])
	}))
	t.Cleanup(server.Close)

	var events []model.RuntimeInstallEvent
	runner := runtimePreflightRunner{
		service: &service{cfg: config.Config{RuntimeDir: t.TempDir()}},
		payload: model.RuntimePreflightStartPayload{JobID: "job-download-resume"},
		reporter: func(payload model.RuntimePreflightStatusPayload) error {
			events = append(events, payload.Events...)
			return nil
		},
	}
	target := filepath.Join(t.TempDir(), "jadx-1.5.5.zip")
	err := runner.downloadRuntimeCandidate(runtimeInstallCandidate{
		name:     "Chat Codex JADX 镜像",
		url:      server.URL,
		filename: "jadx-1.5.5.zip",
		sha256:   expectedSHA256,
		timeout:  time.Minute,
	}, target, "jadx", 40)
	if err != nil {
		t.Fatalf("download runtime candidate: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("expected one retry, got %d requests", requestCount)
	}
	if secondRange == "" || secondRange == "bytes=0-" {
		t.Fatalf("expected resumed Range request, got %q", secondRange)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read downloaded archive: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("resumed archive content mismatch")
	}
	var retryEventFound bool
	for _, event := range events {
		if strings.Contains(event.Message, "从断点重试（2/4）") {
			retryEventFound = true
			break
		}
	}
	if !retryEventFound {
		t.Fatalf("expected retry event, got %+v", events)
	}
}

func TestRuntimePreflightBuildsJavaDarwinArm64Candidate(t *testing.T) {
	original := runtimePlatformForPreflight
	runtimePlatformForPreflight = func() (string, string) { return "darwin", "arm64" }
	t.Cleanup(func() { runtimePlatformForPreflight = original })

	svc := &service{cfg: config.Config{RuntimeDir: t.TempDir()}}
	runner := runtimePreflightRunner{service: svc}
	version := model.RuntimeVersion{ID: "java-21", Version: "21", Enabled: true}
	item := model.RuntimeCatalogItem{
		ID:              "java",
		Name:            "Java",
		InstallStrategy: "archive",
		Artifacts: []model.RuntimeArtifact{{
			ID:          "java-21-darwin-arm64",
			RuntimeID:   "java",
			VersionID:   "java-21",
			Platform:    "darwin",
			Arch:        "arm64",
			PackageKind: "tar.gz",
			Filename:    "OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.11_10.tar.gz",
			SHA256:      "6ebcf221c9b41507b14c098e93c6ead6440b8d9bd154f8ec666c4c73abbdb201",
			ExtractRoot: "Contents/Home",
			BinPaths:    []string{"bin"},
			Enabled:     true,
		}},
		Mirrors: []model.RuntimeMirror{{
			ID:          "java-adoptium",
			RuntimeID:   "java",
			Name:        "Java 清华 Adoptium 镜像",
			BaseURL:     "https://mirrors.tuna.tsinghua.edu.cn/Adoptium",
			URLTemplate: "{base}/{version}/jdk/{adoptium_arch}/{adoptium_os}/{filename}",
			Headers:     map[string]string{"User-Agent": "chat-codex-launcher/0.1"},
			Enabled:     true,
			Priority:    10,
		}},
	}
	candidates := runner.runtimeCandidates(item, version)
	if len(candidates) != 1 {
		t.Fatalf("expected java darwin arm64 candidate, got %+v", candidates)
	}
	if candidates[0].filename != "OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.11_10.tar.gz" ||
		candidates[0].sha256 == "" ||
		candidates[0].extractRoot != "Contents/Home" ||
		!strings.Contains(candidates[0].url, "mirrors.tuna.tsinghua.edu.cn/Adoptium/21/jdk/aarch64/mac/") {
		t.Fatalf("unexpected java candidate: %+v", candidates[0])
	}
	if candidates[0].headers["User-Agent"] != "chat-codex-launcher/0.1" {
		t.Fatalf("expected mirror user agent header to be preserved, got %+v", candidates[0].headers)
	}
}

func TestRuntimePreflightExtractRootUnderSingleChild(t *testing.T) {
	original := runtimePlatformForPreflight
	runtimePlatformForPreflight = func() (string, string) { return "darwin", "arm64" }
	t.Cleanup(func() { runtimePlatformForPreflight = original })

	runtimeDir := t.TempDir()
	archivePath := filepath.Join(t.TempDir(), "jdk-test.zip")
	if err := writeFakeJDKZip(archivePath); err != nil {
		t.Fatalf("write fake jdk zip: %v", err)
	}
	svc := &service{cfg: config.Config{RuntimeDir: runtimeDir}}
	runner := runtimePreflightRunner{
		service: svc,
		payload: model.RuntimePreflightStartPayload{JobID: "job-jdk"},
		reporter: func(model.RuntimePreflightStatusPayload) error {
			return nil
		},
	}
	manifest, err := runner.extractRuntimeCandidate(
		model.RuntimeCatalogItem{ID: "java", Name: "Java", ExecutableNames: []string{"java"}},
		model.RuntimeVersion{ID: "java-21", Version: "21", Enabled: true},
		runtimeInstallCandidate{
			sourceType:  "artifact",
			sourceID:    "java-21-darwin-arm64",
			name:        "JDK test",
			kind:        "zip",
			binPaths:    []string{"bin"},
			extractRoot: "Contents/Home",
		},
		archivePath,
		10,
	)
	if err != nil {
		t.Fatalf("extract java runtime: %v", err)
	}
	if filepath.Base(manifest.InstallDir) != "21" {
		t.Fatalf("expected version install dir, got %s", manifest.InstallDir)
	}
	if _, err := os.Stat(filepath.Join(manifest.InstallDir, "bin", "java")); err != nil {
		t.Fatalf("expected java executable under Contents/Home root: %v", err)
	}
}

func TestRuntimePreflightUntarPreservesNodeBinSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows Node runtime uses zip artifacts without POSIX symlinks")
	}
	runtimeDir := t.TempDir()
	archivePath := filepath.Join(t.TempDir(), "node-test.tar.gz")
	if err := writeFakeNodeTarGz(archivePath); err != nil {
		t.Fatalf("write fake node archive: %v", err)
	}
	extractDir := filepath.Join(runtimeDir, "extract")
	if err := untarRuntimeArchive(archivePath, extractDir); err != nil {
		t.Fatalf("untar node archive: %v", err)
	}
	installDir := filepath.Join(extractDir, "node-vtest")
	npmPath := filepath.Join(installDir, "bin", "npm")
	info, err := os.Lstat(npmPath)
	if err != nil {
		t.Fatalf("expected npm symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected npm to be a symlink, got mode %s", info.Mode())
	}
	runner := runtimePreflightRunner{
		service: &service{cfg: config.Config{RuntimeDir: runtimeDir}},
	}
	if err := runner.verifyRuntimeManifest(installedRuntimeManifest{
		RuntimeID:       "node",
		Version:         "22.16.0",
		InstallDir:      installDir,
		BinPaths:        []string{"bin"},
		ExecutableNames: []string{"node", "npm", "npx"},
	}); err != nil {
		t.Fatalf("verify node manifest with symlinked npm/npx: %v", err)
	}
}

func TestRuntimePreflightInstallsPythonWithManagedUV(t *testing.T) {
	runtimeDir := t.TempDir()
	uvDir := filepath.Join(runtimeDir, "runtimes", "uv", "latest")
	if err := os.MkdirAll(uvDir, 0o755); err != nil {
		t.Fatalf("mkdir uv dir: %v", err)
	}
	if err := writeFakeUVExecutable(filepath.Join(uvDir, executableFileName("uv")), runtime.GOOS); err != nil {
		t.Fatalf("write fake uv: %v", err)
	}
	if err := writeRuntimeManifest(runtimeDir, installedRuntimeManifest{
		RuntimeID:       "uv",
		RuntimeName:     "uv",
		VersionID:       "uv-latest",
		Version:         "latest",
		Platform:        runtime.GOOS,
		Arch:            runtime.GOARCH,
		InstallDir:      uvDir,
		BinPaths:        []string{"."},
		ExecutableNames: []string{"uv"},
		InstalledAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write uv manifest: %v", err)
	}
	svc := &service{cfg: config.Config{RuntimeDir: runtimeDir}}
	runner := runtimePreflightRunner{
		service: svc,
		payload: model.RuntimePreflightStartPayload{JobID: "job-python"},
		reporter: func(model.RuntimePreflightStatusPayload) error {
			return nil
		},
	}
	item := model.RuntimeCatalogItem{
		ID:              "python",
		Name:            "Python",
		InstallStrategy: "uv_python",
		ExecutableNames: []string{"python", "pip"},
		Mirrors: []model.RuntimeMirror{{
			ID:        "python-standalone-npmmirror",
			RuntimeID: "python",
			Name:      "Python standalone npmmirror",
			BaseURL:   "https://registry.npmmirror.com/-/binary/python-build-standalone",
			Priority:  10,
			Enabled:   true,
		}},
	}
	version := model.RuntimeVersion{ID: "python-3.12", RuntimeID: "python", Version: "3.12", Enabled: true}
	manifest, err := runner.installRuntime(item, version, 10)
	if err != nil {
		t.Fatalf("install python with uv: %v", err)
	}
	if manifest.SourceType != "uv_python" {
		t.Fatalf("expected uv_python source, got %+v", manifest)
	}
	if filepath.Base(manifest.InstallDir) != "3.12" {
		t.Fatalf("expected normalized install dir ending 3.12, got %s", manifest.InstallDir)
	}
	if err := runner.verifyRuntimeManifest(manifest); err != nil {
		t.Fatalf("verify python manifest: %v", err)
	}
	stored, ok := svc.bestRuntimeManifest("python", ">=3.12 <3.13")
	if !ok {
		t.Fatal("expected stored python manifest")
	}
	if !strings.Contains(stored.EnvPatch["UV_PYTHON_INSTALL_MIRROR"], "npmmirror") {
		t.Fatalf("expected python standalone mirror in env patch, got %+v", stored.EnvPatch)
	}
}

func TestPythonRuntimeExecutableNamesKeepsOnlyExistingCommands(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir python bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, executableFileName("python")), []byte(fakeRuntimeExecutableContent()), 0o755); err != nil {
		t.Fatalf("write python executable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, executableFileName("pip")), []byte(fakeRuntimeExecutableContent()), 0o755); err != nil {
		t.Fatalf("write pip executable: %v", err)
	}

	got := pythonRuntimeExecutableNames(root, []string{"bin"}, []string{"python", "python3", "pip", "pip3"})
	if !sameStringSet(got, []string{"python", "pip"}) {
		t.Fatalf("expected only installed python commands, got %+v", got)
	}
}

func TestDetectRuntimeNormalizesOldPythonManifestExecutableNames(t *testing.T) {
	runtimeDir := t.TempDir()
	root := filepath.Join(runtimeDir, "runtimes", "python", "3.12")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir python bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, executableFileName("python")), []byte(fakeRuntimeExecutableContent()), 0o755); err != nil {
		t.Fatalf("write python executable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, executableFileName("pip")), []byte(fakeRuntimeExecutableContent()), 0o755); err != nil {
		t.Fatalf("write pip executable: %v", err)
	}
	if err := writeRuntimeManifest(runtimeDir, installedRuntimeManifest{
		RuntimeID:       "python",
		RuntimeName:     "Python",
		VersionID:       "python-3.12",
		Version:         "3.12",
		Platform:        runtime.GOOS,
		Arch:            runtime.GOARCH,
		InstallDir:      root,
		BinPaths:        []string{"bin"},
		ExecutableNames: []string{"python", "python3", "pip", "pip3"},
		InstalledAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write python manifest: %v", err)
	}

	runner := runtimePreflightRunner{service: &service{cfg: config.Config{RuntimeDir: runtimeDir}}}
	manifest, ok := runner.detectRuntime(
		model.RuntimeCatalogItem{ID: "python", Name: "Python"},
		model.RuntimeVersion{ID: "python-3.12", Version: "3.12"},
	)
	if !ok {
		t.Fatal("expected old python manifest to verify after normalization")
	}
	if !sameStringSet(manifest.ExecutableNames, []string{"python", "pip"}) {
		t.Fatalf("expected normalized executable names, got %+v", manifest.ExecutableNames)
	}
	data, err := os.ReadFile(runtimeManifestPath(runtimeDir, "python", "3.12"))
	if err != nil {
		t.Fatalf("read rewritten manifest: %v", err)
	}
	if strings.Contains(string(data), "python3") || strings.Contains(string(data), "pip3") {
		t.Fatalf("expected rewritten manifest to drop unavailable python3/pip3 commands: %s", string(data))
	}
}

func TestRuntimePreflightPythonInstallMirrorSkipsPackageIndexMirrors(t *testing.T) {
	runner := runtimePreflightRunner{}
	item := model.RuntimeCatalogItem{
		ID: "python",
		Mirrors: []model.RuntimeMirror{
			{
				ID:        "python-pypi-tsinghua",
				RuntimeID: "python",
				BaseURL:   "https://pypi.tuna.tsinghua.edu.cn/simple",
				Priority:  1,
				Enabled:   true,
				Tags:      []string{"registry", "china"},
			},
			{
				ID:        "python-standalone-npmmirror",
				RuntimeID: "python",
				BaseURL:   "https://registry.npmmirror.com/-/binary/python-build-standalone",
				Priority:  20,
				Enabled:   true,
				Tags:      []string{"china", "mirror", "verified"},
			},
		},
	}
	got := runner.pythonInstallMirror(item, model.RuntimeVersion{ID: "python-3.12", Version: "3.12"})
	if got != "https://registry.npmmirror.com/-/binary/python-build-standalone" {
		t.Fatalf("expected python standalone mirror, got %q", got)
	}
}

func TestFindUVManagedPythonRootSupportsWindowsRootExecutable(t *testing.T) {
	installParent := t.TempDir()
	root := filepath.Join(installParent, "cpython-3.12-test")
	if err := os.MkdirAll(filepath.Join(root, "Scripts"), 0o755); err != nil {
		t.Fatalf("mkdir python root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "python.exe"), []byte("python"), 0o755); err != nil {
		t.Fatalf("write python exe: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "Scripts", "pip.exe"), []byte("pip"), 0o755); err != nil {
		t.Fatalf("write pip exe: %v", err)
	}
	gotRoot, binPaths := findUVManagedPythonRoot(installParent, "3.12")
	if gotRoot != root {
		t.Fatalf("expected python root %s, got %s", root, gotRoot)
	}
	if !containsString(binPaths, ".") || !containsString(binPaths, "Scripts") {
		t.Fatalf("expected root and Scripts bin paths, got %+v", binPaths)
	}
}

func TestRuntimePreflightReportsPerMCPInstallProgress(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	runtimeDir := t.TempDir()
	agentID := "agent_mcp_progress"
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
	profile := model.SemanticAgentProfile{
		ID:                    "reverse-android",
		Name:                  "逆向专家-安卓",
		Enabled:               true,
		MCPIDs:                []string{"android-frida"},
		RecommendedMCPServers: []string{"frida-mcp"},
		RecommendedMCPConfigs: []model.DeviceMCPServerInfo{{
			Name:    "frida-mcp",
			Type:    "local",
			Enabled: true,
			Command: []string{"npx", "-y", "frida-mcp"},
		}},
	}
	var statuses []model.RuntimePreflightStatusPayload
	err := svc.StartAgentSemanticPreflight(model.RuntimePreflightStartPayload{
		JobID:               "job_mcp_progress",
		MachineID:           "m_test",
		LauncherAgentID:     agentID,
		SemanticAgentID:     profile.ID,
		ApplyRecommendedMCP: true,
		SemanticAgent:       &profile,
	}, func(status model.RuntimePreflightStatusPayload) error {
		statuses = append(statuses, status)
		return nil
	})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if len(statuses) == 0 {
		t.Fatal("expected statuses")
	}
	var sawDetect, sawInstalled bool
	for _, status := range statuses {
		for _, event := range status.Events {
			if event.ItemType != "mcp" || event.ItemID != "android-frida" {
				continue
			}
			if event.Phase == "detect" {
				sawDetect = true
			}
			if event.Phase == "installed" && event.Status == "success" {
				sawInstalled = true
			}
		}
	}
	if !sawDetect || !sawInstalled {
		t.Fatalf("expected per-mcp detect and installed events, got %+v", statuses)
	}
	finalItems := statuses[len(statuses)-1].Items
	found := false
	for _, item := range finalItems {
		if item.ItemType == "mcp" && item.ItemID == "android-frida" {
			found = true
			if item.Status != "completed" || item.ProgressPercent != 100 {
				t.Fatalf("expected mcp completed, got %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("expected mcp item in final status, got %+v", finalItems)
	}
}

func TestRuntimePreflightCompletesGlobalMCPDisabledButAgentEnabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	runtimeDir := t.TempDir()
	agentID := "agent_mcp_global_disabled"
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
	profile := model.SemanticAgentProfile{
		ID:                    "reverse-android",
		Name:                  "逆向专家-安卓",
		Enabled:               true,
		MCPIDs:                []string{"android-frida"},
		RecommendedMCPServers: []string{"frida-mcp"},
		RecommendedMCPConfigs: []model.DeviceMCPServerInfo{{
			Name:    "frida-mcp",
			Type:    "local",
			Enabled: true,
			Command: []string{"npx", "-y", "frida-mcp"},
		}},
	}
	if err := svc.StartAgentSemanticPreflight(model.RuntimePreflightStartPayload{
		JobID:               "job_mcp_global_disabled",
		MachineID:           "m_test",
		LauncherAgentID:     agentID,
		SemanticAgentID:     profile.ID,
		ApplyRecommendedMCP: true,
		SemanticAgent:       &profile,
	}, func(status model.RuntimePreflightStatusPayload) error {
		return nil
	}); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	info, err := mcpconfig.Load()
	if err != nil {
		t.Fatalf("load mcp config: %v", err)
	}
	if len(info.Servers) != 1 || info.Servers[0].Name != "frida-mcp" {
		t.Fatalf("expected frida-mcp in global config, got %+v", info.Servers)
	}
	if info.Servers[0].Enabled {
		t.Fatalf("expected preflight-completed global mcp to be disabled, got %+v", info.Servers[0])
	}
	agentCfg, err := readAgentConfig(runtimeDir, agentID)
	if err != nil {
		t.Fatalf("read agent config: %v", err)
	}
	env := svc.agentEnv(*agentCfg)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &parsed); err != nil {
		t.Fatalf("decode agent config content: %v content=%s", err, env["OPENCODE_CONFIG_CONTENT"])
	}
	if got := mcpEnabledForTest(t, parsed, "frida-mcp"); !got {
		t.Fatalf("expected semantic agent mcp to stay enabled in agent override, content=%s", env["OPENCODE_CONFIG_CONTENT"])
	}
}

func TestSaveAgentSemanticSelectionEnablesConfiguredAndPreparedMCPs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	runtimeDir := t.TempDir()
	agentID := "agent_mcp_mixed_sources"
	if _, err := mcpconfig.Save(model.DeviceMCPConfigInfo{Servers: []model.DeviceMCPServerInfo{
		{
			Name:    "idalib-mcp",
			Type:    "local",
			Enabled: false,
			Command: []string{"uvx", "idalib-mcp"},
		},
		{
			Name:    "verify",
			Type:    "local",
			Enabled: false,
			Command: []string{"npx", "-y", "@ktbtw/verify-mcp"},
		},
		{
			Name:    "docs",
			Type:    "remote",
			Enabled: true,
			URL:     "https://docs.example.com/mcp",
		},
	}}); err != nil {
		t.Fatalf("save global mcp config: %v", err)
	}
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
	profile := model.SemanticAgentProfile{
		ID:                    "reverse-mixed-mcp",
		Name:                  "混合 MCP Agent",
		OpencodeName:          "reverse-mixed-mcp",
		Enabled:               true,
		RecommendedMCPServers: []string{"idalib-mcp", "verify"},
		RecommendedMCPConfigs: []model.DeviceMCPServerInfo{{
			Name:    "verify",
			Type:    "local",
			Enabled: true,
			Command: []string{"npx", "-y", "@ktbtw/verify-mcp"},
		}},
	}
	if _, err := svc.SaveAgentSemanticSelection(model.DeviceAgentSemanticSelectionInput{
		AgentID:             agentID,
		SemanticAgentID:     profile.ID,
		ApplyRecommendedMCP: true,
		SemanticAgent:       &profile,
	}); err != nil {
		t.Fatalf("save semantic selection: %v", err)
	}
	stored, err := readAgentConfig(runtimeDir, agentID)
	if err != nil {
		t.Fatalf("read agent config: %v", err)
	}
	if got := strings.Join(stored.MCPServers, ","); got != "idalib-mcp,verify" {
		t.Fatalf("expected configured and prepared MCPs selected, got %q", got)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stored.Env["OPENCODE_CONFIG_CONTENT"]), &parsed); err != nil {
		t.Fatalf("decode agent config content: %v", err)
	}
	if !mcpEnabledForTest(t, parsed, "idalib-mcp") {
		t.Fatalf("expected prepared idalib-mcp enabled, content=%s", stored.Env["OPENCODE_CONFIG_CONTENT"])
	}
	if !mcpEnabledForTest(t, parsed, "verify") {
		t.Fatalf("expected configured verify enabled, content=%s", stored.Env["OPENCODE_CONFIG_CONTENT"])
	}
	if mcpEnabledForTest(t, parsed, "docs") {
		t.Fatalf("expected unrelated docs MCP disabled, content=%s", stored.Env["OPENCODE_CONFIG_CONTENT"])
	}
}

func TestSaveAgentSemanticSelectionDisablesVerifyMCPWithoutTokens(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	runtimeDir := t.TempDir()
	agentID := "agent_verify_disabled"
	if _, err := mcpconfig.Save(model.DeviceMCPConfigInfo{Servers: []model.DeviceMCPServerInfo{
		{
			Name:    "idalib-mcp",
			Type:    "local",
			Enabled: true,
			Command: []string{"uvx", "idalib-mcp"},
		},
		{
			Name:    "verify",
			Type:    "local",
			Enabled: true,
			Command: []string{"npx", "-y", "@ktbtw/verify-mcp"},
		},
	}}); err != nil {
		t.Fatalf("save global mcp config: %v", err)
	}
	if err := writeAgentConfig(filepath.Join(runtimeDir, "agents", agentID), model.AgentConfig{
		AgentID:    agentID,
		ProjectDir: runtimeDir,
		UserEnv: map[string]string{
			"VERIFY_API_TOKEN":     "api-token",
			"VERIFY_PROTECT_TOKEN": "protect-token",
		},
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
	profile := model.SemanticAgentProfile{
		ID:                    "reverse-android",
		Name:                  "逆向专家-安卓",
		Enabled:               true,
		RecommendedMCPServers: []string{"idalib-mcp", "verify"},
		RecommendedMCPConfigs: []model.DeviceMCPServerInfo{{
			Name:    "verify",
			Type:    "local",
			Enabled: true,
			Command: []string{"npx", "-y", "@ktbtw/verify-mcp"},
		}},
	}
	if _, err := svc.SaveAgentSemanticSelection(model.DeviceAgentSemanticSelectionInput{
		AgentID:             agentID,
		SemanticAgentID:     profile.ID,
		ApplyRecommendedMCP: true,
		DisableVerifyMCP:    true,
		SemanticAgent:       &profile,
	}); err != nil {
		t.Fatalf("save semantic selection: %v", err)
	}
	stored, err := readAgentConfig(runtimeDir, agentID)
	if err != nil {
		t.Fatalf("read agent config: %v", err)
	}
	if !stored.DisableVerifyMCP {
		t.Fatal("expected Verify MCP disabled in agent config")
	}
	if got := strings.Join(stored.MCPServers, ","); got != "idalib-mcp" {
		t.Fatalf("expected Verify MCP removed from selected servers, got %q", got)
	}
	if _, ok := stored.Env["VERIFY_API_TOKEN"]; ok {
		t.Fatalf("expected API token removed from effective agent environment: %+v", stored.Env)
	}
	if _, ok := stored.Env["VERIFY_PROTECT_TOKEN"]; ok {
		t.Fatalf("expected Protect token removed from effective agent environment: %+v", stored.Env)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stored.Env["OPENCODE_CONFIG_CONTENT"]), &parsed); err != nil {
		t.Fatalf("decode agent config content: %v", err)
	}
	if mcpEnabledForTest(t, parsed, "verify") {
		t.Fatalf("expected Verify MCP disabled in agent override, content=%s", stored.Env["OPENCODE_CONFIG_CONTENT"])
	}
	if !mcpEnabledForTest(t, parsed, "idalib-mcp") {
		t.Fatalf("expected other recommended MCP to remain enabled, content=%s", stored.Env["OPENCODE_CONFIG_CONTENT"])
	}
}

func TestBuildPreflightItemsSkipsVerifyMCPWhenDisabled(t *testing.T) {
	profile := model.SemanticAgentProfile{
		ID:                    "reverse-android",
		MCPIDs:                []string{"idalib-mcp", "verify"},
		RecommendedMCPServers: []string{"idalib-mcp", "verify"},
		RecommendedMCPConfigs: []model.DeviceMCPServerInfo{{Name: "verify", Type: "local"}},
	}
	items := buildPreflightItems(model.RuntimePreflightStartPayload{
		JobID:            "job_verify_disabled",
		SemanticAgent:    &profile,
		DisableVerifyMCP: true,
	})
	for _, item := range items {
		if item.ItemType == preflightItemMCP && isVerifyMCPName(item.Name+" "+item.ItemID) {
			t.Fatalf("Verify MCP should not be included in preflight items: %+v", item)
		}
	}
}

func TestRuntimePreflightPreservesExistingGlobalMCPEnabledState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir opencode config: %v", err)
	}
	raw := `{
	  "mcp": {
	    "frida-mcp": {
	      "type": "local",
	      "command": ["node", "old-frida.js"]
	    }
	  }
	}`
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write opencode config: %v", err)
	}
	runtimeDir := t.TempDir()
	agentID := "agent_mcp_existing_enabled"
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
	profile := model.SemanticAgentProfile{
		ID:                    "reverse-android",
		Name:                  "逆向专家-安卓",
		Enabled:               true,
		MCPIDs:                []string{"frida-mcp"},
		RecommendedMCPServers: []string{"frida-mcp"},
		RecommendedMCPConfigs: []model.DeviceMCPServerInfo{{
			Name:    "frida-mcp",
			Type:    "local",
			Enabled: true,
			Command: []string{"node", "new-frida.js"},
		}},
	}
	if err := svc.StartAgentSemanticPreflight(model.RuntimePreflightStartPayload{
		JobID:               "job_mcp_existing_enabled",
		MachineID:           "m_test",
		LauncherAgentID:     agentID,
		SemanticAgentID:     profile.ID,
		ApplyRecommendedMCP: true,
		SemanticAgent:       &profile,
	}, func(status model.RuntimePreflightStatusPayload) error {
		return nil
	}); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	info, err := mcpconfig.Load()
	if err != nil {
		t.Fatalf("load mcp config: %v", err)
	}
	if len(info.Servers) != 1 {
		t.Fatalf("expected one global mcp, got %+v", info.Servers)
	}
	if !info.Servers[0].Enabled {
		t.Fatalf("expected existing enabled global mcp to remain enabled, got %+v", info.Servers[0])
	}
	if got := strings.Join(info.Servers[0].Command, " "); !strings.Contains(got, "new-frida.js") {
		t.Fatalf("expected global mcp command to be refreshed, got %+v", info.Servers[0].Command)
	}
}

func TestRuntimePreflightInstallsMCPDependencyAndReportsProgress(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	runtimeDir := t.TempDir()
	agentID := "agent_mcp_install"
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
	installCommand := `printf installed > "$MCP_HOME/installed.txt"`
	if runtime.GOOS == "windows" {
		installCommand = `echo installed > "%MCP_HOME%\installed.txt"`
	}
	profile := model.SemanticAgentProfile{
		ID:                    "reverse-android",
		Name:                  "逆向专家-安卓",
		Enabled:               true,
		MCPIDs:                []string{"android-local"},
		RecommendedMCPServers: []string{"android-local"},
		MCPDependencies: []model.SemanticAgentMCPDependency{{
			ID:          "android-local",
			Name:        "android-local",
			Type:        "local",
			LaunchReady: false,
			Install: model.MCPInstallInfo{
				InstallCommands:    []string{installCommand},
				RunCommandTemplate: []string{"node", "${MCP_HOME}/server.js"},
			},
			Config: model.DeviceMCPServerInfo{
				Name:    "android-local",
				Type:    "local",
				Enabled: true,
			},
		}},
	}
	var statuses []model.RuntimePreflightStatusPayload
	err := svc.StartAgentSemanticPreflight(model.RuntimePreflightStartPayload{
		JobID:               "job_mcp_install",
		MachineID:           "m_test",
		LauncherAgentID:     agentID,
		SemanticAgentID:     profile.ID,
		ApplyRecommendedMCP: true,
		AutoRepair:          true,
		SemanticAgent:       &profile,
	}, func(status model.RuntimePreflightStatusPayload) error {
		statuses = append(statuses, status)
		return nil
	})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	installRoot := mcpInstallRoot(runtimeDir, "android-local")
	if !pathExists(filepath.Join(installRoot, "installed.txt")) {
		t.Fatalf("expected mcp install marker in %s", installRoot)
	}
	info, err := mcpconfig.Load()
	if err != nil {
		t.Fatalf("load mcp config: %v", err)
	}
	if len(info.Servers) != 1 || info.Servers[0].Command[1] != filepath.Join(installRoot, "server.js") {
		t.Fatalf("expected resolved mcp config, got %+v", info.Servers)
	}
	var sawInstall, sawResolve, sawInstalled bool
	for _, status := range statuses {
		for _, event := range status.Events {
			if event.ItemType != "mcp" || event.ItemID != "android-local" {
				continue
			}
			if event.Phase == "install" && event.Status == "running" {
				sawInstall = true
			}
			if event.Phase == "resolve" && event.Status == "success" {
				sawResolve = true
			}
			if event.Phase == "installed" && event.Status == "success" {
				sawInstalled = true
			}
		}
	}
	if !sawInstall || !sawResolve || !sawInstalled {
		t.Fatalf("expected install/resolve/installed events, install=%v resolve=%v installed=%v statuses=%+v", sawInstall, sawResolve, sawInstalled, statuses)
	}
}

func TestRuntimePreflightReusesExistingMCPDependencyInstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	runtimeDir := t.TempDir()
	agentID := "agent_mcp_reuse"
	if err := writeAgentConfig(filepath.Join(runtimeDir, "agents", agentID), model.AgentConfig{
		AgentID:    agentID,
		ProjectDir: runtimeDir,
	}); err != nil {
		t.Fatalf("write agent config: %v", err)
	}
	installRoot := mcpInstallRoot(runtimeDir, "android-local")
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "server.js"), []byte("server"), 0o644); err != nil {
		t.Fatalf("write existing server: %v", err)
	}
	installMarker := filepath.Join(installRoot, "installed.txt")
	installCommand := `printf installed > "$MCP_HOME/installed.txt"`
	if runtime.GOOS == "windows" {
		installCommand = `echo installed > "%MCP_HOME%\installed.txt"`
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
	profile := model.SemanticAgentProfile{
		ID:                    "reverse-android",
		Name:                  "逆向专家-安卓",
		Enabled:               true,
		MCPIDs:                []string{"android-local"},
		RecommendedMCPServers: []string{"android-local"},
		MCPDependencies: []model.SemanticAgentMCPDependency{{
			ID:          "android-local",
			Name:        "android-local",
			Type:        "local",
			LaunchReady: false,
			Install: model.MCPInstallInfo{
				InstallCommands:    []string{installCommand},
				RunCommandTemplate: []string{"node", "${MCP_HOME}/server.js"},
			},
			Config: model.DeviceMCPServerInfo{
				Name:    "android-local",
				Type:    "local",
				Enabled: true,
			},
		}},
	}
	var statuses []model.RuntimePreflightStatusPayload
	err := svc.StartAgentSemanticPreflight(model.RuntimePreflightStartPayload{
		JobID:               "job_mcp_reuse",
		MachineID:           "m_test",
		LauncherAgentID:     agentID,
		SemanticAgentID:     profile.ID,
		ApplyRecommendedMCP: true,
		AutoRepair:          true,
		SemanticAgent:       &profile,
	}, func(status model.RuntimePreflightStatusPayload) error {
		statuses = append(statuses, status)
		return nil
	})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if pathExists(installMarker) {
		t.Fatalf("expected existing MCP install to be reused without running install command")
	}
	info, err := mcpconfig.Load()
	if err != nil {
		t.Fatalf("load mcp config: %v", err)
	}
	if len(info.Servers) != 1 || info.Servers[0].Command[1] != filepath.Join(installRoot, "server.js") {
		t.Fatalf("expected resolved reused mcp config, got %+v", info.Servers)
	}
	var sawReuse bool
	for _, status := range statuses {
		for _, event := range status.Events {
			if event.ItemType == "mcp" && event.ItemID == "android-local" && event.Phase == "reuse" && event.Status == "success" {
				sawReuse = true
			}
		}
	}
	if !sawReuse {
		t.Fatalf("expected reuse success event, got %+v", statuses)
	}
}

func TestRuntimePreflightReinstallsBrokenMCPDependencyInstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	runtimeDir := t.TempDir()
	agentID := "agent_mcp_reinstall"
	if err := writeAgentConfig(filepath.Join(runtimeDir, "agents", agentID), model.AgentConfig{
		AgentID:    agentID,
		ProjectDir: runtimeDir,
	}); err != nil {
		t.Fatalf("write agent config: %v", err)
	}
	installRoot := mcpInstallRoot(runtimeDir, "android-local")
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "stale.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale marker: %v", err)
	}
	installCommand := `printf installed > "$MCP_HOME/installed.txt"; printf server > "$MCP_HOME/server.js"`
	if runtime.GOOS == "windows" {
		installCommand = `echo installed > "%MCP_HOME%\installed.txt" && echo server > "%MCP_HOME%\server.js"`
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
	profile := model.SemanticAgentProfile{
		ID:                    "reverse-android",
		Name:                  "逆向专家-安卓",
		Enabled:               true,
		MCPIDs:                []string{"android-local"},
		RecommendedMCPServers: []string{"android-local"},
		MCPDependencies: []model.SemanticAgentMCPDependency{{
			ID:          "android-local",
			Name:        "android-local",
			Type:        "local",
			LaunchReady: false,
			Install: model.MCPInstallInfo{
				InstallCommands:    []string{installCommand},
				RunCommandTemplate: []string{"node", "${MCP_HOME}/server.js"},
			},
			Config: model.DeviceMCPServerInfo{
				Name:    "android-local",
				Type:    "local",
				Enabled: true,
			},
		}},
	}
	err := svc.StartAgentSemanticPreflight(model.RuntimePreflightStartPayload{
		JobID:               "job_mcp_reinstall",
		MachineID:           "m_test",
		LauncherAgentID:     agentID,
		SemanticAgentID:     profile.ID,
		ApplyRecommendedMCP: true,
		AutoRepair:          true,
		SemanticAgent:       &profile,
	}, func(status model.RuntimePreflightStatusPayload) error {
		return nil
	})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if pathExists(filepath.Join(installRoot, "stale.txt")) {
		t.Fatalf("expected stale MCP directory to be removed before reinstall")
	}
	if !pathExists(filepath.Join(installRoot, "installed.txt")) || !pathExists(filepath.Join(installRoot, "server.js")) {
		t.Fatalf("expected MCP reinstall outputs in %s", installRoot)
	}
}

func TestRuntimePreflightInstallsMCPNativeAssetsAndFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	runtimeDir := t.TempDir()
	agentID := "agent_mcp_native"
	if err := writeAgentConfig(filepath.Join(runtimeDir, "agents", agentID), model.AgentConfig{
		AgentID:    agentID,
		ProjectDir: runtimeDir,
	}); err != nil {
		t.Fatalf("write agent config: %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), "native-mcp.tar.gz")
	if err := writeFakeMCPTarGz(archivePath); err != nil {
		t.Fatalf("write fake mcp archive: %v", err)
	}
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read fake mcp archive: %v", err)
	}
	var observedUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedUA = r.UserAgent()
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Length", fmt.Sprint(len(archiveData)))
		_, _ = w.Write(archiveData)
	}))
	t.Cleanup(server.Close)
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
	profile := model.SemanticAgentProfile{
		ID:                    "reverse-android",
		Name:                  "逆向专家-安卓",
		Enabled:               true,
		MCPIDs:                []string{"native-mcp"},
		RecommendedMCPServers: []string{"native-mcp"},
		MCPDependencies: []model.SemanticAgentMCPDependency{{
			ID:          "native-mcp",
			Name:        "native-mcp",
			Type:        "local",
			LaunchReady: false,
			Install: model.MCPInstallInfo{
				Assets: []model.MCPInstallAsset{{
					Name:        "native archive",
					URL:         server.URL + "/native-mcp.tar.gz",
					Filename:    "native-mcp.tar.gz",
					Extract:     true,
					PackageKind: "tar.gz",
				}},
				Files: []model.MCPInstallFile{{
					Path:    "${MCP_HOME}/mcp.toml",
					Content: "[mcp]\nidle_timeout_seconds = 1200\n",
				}},
				RunCommandTemplate: []string{"node", "${MCP_HOME}/server.js", "--config", "${MCP_HOME}/mcp.toml"},
			},
			Config: model.DeviceMCPServerInfo{
				Name:    "native-mcp",
				Type:    "local",
				Enabled: true,
			},
		}},
	}
	var statuses []model.RuntimePreflightStatusPayload
	err = svc.StartAgentSemanticPreflight(model.RuntimePreflightStartPayload{
		JobID:               "job_mcp_native",
		MachineID:           "m_test",
		LauncherAgentID:     agentID,
		SemanticAgentID:     profile.ID,
		ApplyRecommendedMCP: true,
		AutoRepair:          true,
		SemanticAgent:       &profile,
	}, func(status model.RuntimePreflightStatusPayload) error {
		statuses = append(statuses, status)
		return nil
	})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if observedUA != runtimeDownloadUserAgent {
		t.Fatalf("expected launcher user agent %q, got %q", runtimeDownloadUserAgent, observedUA)
	}
	installRoot := mcpInstallRoot(runtimeDir, "native-mcp")
	if !pathExists(filepath.Join(installRoot, "server.js")) {
		t.Fatalf("expected native MCP archive extracted into %s", installRoot)
	}
	data, err := os.ReadFile(filepath.Join(installRoot, "mcp.toml"))
	if err != nil {
		t.Fatalf("read native mcp config: %v", err)
	}
	if !strings.Contains(string(data), "idle_timeout_seconds = 1200") {
		t.Fatalf("expected mcp.toml content to be written, got %q", string(data))
	}
	info, err := mcpconfig.Load()
	if err != nil {
		t.Fatalf("load mcp config: %v", err)
	}
	if len(info.Servers) != 1 {
		t.Fatalf("expected one mcp server, got %+v", info.Servers)
	}
	expectedCommand := []string{"node", filepath.Join(installRoot, "server.js"), "--config", filepath.Join(installRoot, "mcp.toml")}
	if strings.Join(info.Servers[0].Command, "\n") != strings.Join(expectedCommand, "\n") {
		t.Fatalf("expected resolved command %+v, got %+v", expectedCommand, info.Servers[0].Command)
	}
	var sawDownload, sawExtract, sawWriteFile bool
	for _, status := range statuses {
		for _, event := range status.Events {
			if event.ItemType != "mcp" || event.ItemID != "native-mcp" {
				continue
			}
			switch event.Phase {
			case "download":
				if event.Status == "success" {
					sawDownload = true
				}
			case "extract":
				if event.Status == "success" {
					sawExtract = true
				}
			case "write_file":
				if event.Status == "success" {
					sawWriteFile = true
				}
			}
		}
	}
	if !sawDownload || !sawExtract || !sawWriteFile {
		t.Fatalf("expected native download/extract/write_file events, download=%v extract=%v write=%v statuses=%+v", sawDownload, sawExtract, sawWriteFile, statuses)
	}
}

func TestRuntimePreflightResolvesMCPHomeInInstallCommands(t *testing.T) {
	installRoot := filepath.Join(t.TempDir(), "mcps", "sample")
	command, err := resolveMCPInstallCommand("uvx --from ${MCP_HOME}/package.tar.gz sample --help", installRoot)
	if err != nil {
		t.Fatalf("resolve command: %v", err)
	}
	expected := "uvx --from " + filepath.Join(installRoot, "package.tar.gz") + " sample --help"
	if command != expected {
		t.Fatalf("expected %q, got %q", expected, command)
	}
	if _, err := resolveMCPInstallCommand("echo /ABSOLUTE/PATH/demo", installRoot); err == nil {
		t.Fatal("expected unresolved example placeholder to fail")
	}
}

func TestRuntimePreflightMCPInstallEnvIncludesManagedRuntimeMirrors(t *testing.T) {
	runtimeDir := t.TempDir()
	pythonRoot := filepath.Join(runtimeDir, "runtimes", "python", "3.12")
	if err := os.MkdirAll(filepath.Join(pythonRoot, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir python root: %v", err)
	}
	manifest := installedRuntimeManifest{
		RuntimeID:  "python",
		VersionID:  "python-3.12",
		Version:    "3.12",
		Platform:   currentRuntimePlatform(),
		Arch:       currentRuntimeArch(),
		InstallDir: pythonRoot,
		BinPaths:   []string{"bin"},
		EnvPatch: map[string]string{
			"PIP_INDEX_URL":    "https://pypi.tuna.tsinghua.edu.cn/simple",
			"UV_INDEX_URL":     "https://pypi.tuna.tsinghua.edu.cn/simple",
			"UV_DEFAULT_INDEX": "https://pypi.tuna.tsinghua.edu.cn/simple",
			"UV_CACHE_DIR":     filepath.Join(runtimeDir, "caches", "uv"),
		},
	}
	if err := writeRuntimeManifest(runtimeDir, manifest); err != nil {
		t.Fatalf("write runtime manifest: %v", err)
	}
	runner := runtimePreflightRunner{
		service: &service{cfg: config.Config{RuntimeDir: runtimeDir}},
		payload: model.RuntimePreflightStartPayload{
			SemanticAgent: &model.SemanticAgentProfile{
				RuntimeRequirements: []model.SemanticAgentRuntime{{
					RuntimeID:         "python",
					VersionConstraint: ">=3.12 <3.15",
					Required:          true,
				}},
			},
		},
	}
	env := runner.mcpInstallEnv(model.SemanticAgentMCPDependency{ID: "mcp", Name: "mcp"}, filepath.Join(runtimeDir, "mcps", "mcp"))
	envMap := envListToMapForTest(env)
	if got := envMap["PIP_INDEX_URL"]; got != "https://pypi.tuna.tsinghua.edu.cn/simple" {
		t.Fatalf("expected pip mirror in MCP install env, got %q", got)
	}
	if got := envMap["UV_INDEX_URL"]; got != "https://pypi.tuna.tsinghua.edu.cn/simple" {
		t.Fatalf("expected uv mirror in MCP install env, got %q", got)
	}
	if got := envMap["UV_CACHE_DIR"]; got != filepath.Join(runtimeDir, "caches", "uv") {
		t.Fatalf("expected uv cache dir in MCP install env, got %q", got)
	}
}

func TestRuntimePreflightCompletedMCPItemStaysCompletedWhenAnotherMCPFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	runtimeDir := t.TempDir()
	agentID := "agent_mcp_partial"
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
	profile := model.SemanticAgentProfile{
		ID:                    "reverse-partial",
		Name:                  "部分失败测试",
		Enabled:               true,
		MCPIDs:                []string{"ready-mcp", "broken-mcp"},
		RecommendedMCPServers: []string{"ready-mcp", "broken-mcp"},
		MCPDependencies: []model.SemanticAgentMCPDependency{{
			ID:          "ready-mcp",
			Name:        "ready-mcp",
			Type:        "local",
			LaunchReady: true,
			Config: model.DeviceMCPServerInfo{
				Name:    "ready-mcp",
				Type:    "local",
				Enabled: true,
				Command: []string{"node", "ready-server.js"},
			},
		}, {
			ID:          "broken-mcp",
			Name:        "broken-mcp",
			Type:        "local",
			LaunchReady: false,
			Install: model.MCPInstallInfo{
				RunCommandTemplate: []string{"node", "${MCP_HOME}/missing.js"},
			},
			Config: model.DeviceMCPServerInfo{
				Name:    "broken-mcp",
				Type:    "local",
				Enabled: true,
			},
		}},
	}
	var statuses []model.RuntimePreflightStatusPayload
	err := svc.StartAgentSemanticPreflight(model.RuntimePreflightStartPayload{
		JobID:               "job_mcp_partial",
		MachineID:           "m_test",
		LauncherAgentID:     agentID,
		SemanticAgentID:     profile.ID,
		ApplyRecommendedMCP: true,
		AutoRepair:          true,
		SemanticAgent:       &profile,
	}, func(status model.RuntimePreflightStatusPayload) error {
		statuses = append(statuses, status)
		return nil
	})
	if err == nil {
		t.Fatal("expected preflight to fail because one MCP has no install command")
	}
	var finalReady *model.RuntimeInstallJobItem
	for i := range statuses[len(statuses)-1].Items {
		item := statuses[len(statuses)-1].Items[i]
		if item.ItemType == "mcp" && item.ItemID == "ready-mcp" {
			finalReady = &item
			break
		}
	}
	if finalReady == nil {
		t.Fatalf("expected ready mcp item in final status, got %+v", statuses[len(statuses)-1].Items)
	}
	if finalReady.Status != "completed" || finalReady.ProgressPercent != 100 {
		t.Fatalf("expected successful MCP item to stay completed, got %+v", *finalReady)
	}
}

func TestRuntimePreflightInstallsMCPDependenciesConcurrently(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	runtimeDir := t.TempDir()
	agentID := "agent_mcp_parallel"
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
	startedA := filepath.Join(runtimeDir, "started-a")
	startedB := filepath.Join(runtimeDir, "started-b")
	release := filepath.Join(runtimeDir, "release")
	profile := model.SemanticAgentProfile{
		ID:                    "reverse-parallel",
		Name:                  "并发 MCP 测试",
		Enabled:               true,
		MCPIDs:                []string{"parallel-a", "parallel-b"},
		RecommendedMCPServers: []string{"parallel-a", "parallel-b"},
		MCPDependencies: []model.SemanticAgentMCPDependency{
			parallelMCPDependencyForTest("parallel-a", startedA, release),
			parallelMCPDependencyForTest("parallel-b", startedB, release),
		},
	}
	done := make(chan error, 1)
	var statusesMu sync.Mutex
	var statuses []model.RuntimePreflightStatusPayload
	go func() {
		done <- svc.StartAgentSemanticPreflight(model.RuntimePreflightStartPayload{
			JobID:               "job_mcp_parallel",
			MachineID:           "m_test",
			LauncherAgentID:     agentID,
			SemanticAgentID:     profile.ID,
			ApplyRecommendedMCP: true,
			AutoRepair:          true,
			SemanticAgent:       &profile,
		}, func(status model.RuntimePreflightStatusPayload) error {
			statusesMu.Lock()
			statuses = append(statuses, status)
			statusesMu.Unlock()
			return nil
		})
	}()
	waitForPathForTest(t, startedA, 3*time.Second)
	waitForPathForTest(t, startedB, 3*time.Second)
	if err := os.WriteFile(release, []byte("go"), 0o644); err != nil {
		t.Fatalf("write release file: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("preflight: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("preflight did not finish after releasing parallel installs")
	}
	info, err := mcpconfig.Load()
	if err != nil {
		t.Fatalf("load mcp config: %v", err)
	}
	if len(info.Servers) != 2 || info.Servers[0].Name != "parallel-a" || info.Servers[1].Name != "parallel-b" {
		t.Fatalf("expected stable mcp config order, got %+v", info.Servers)
	}
	statusesMu.Lock()
	defer statusesMu.Unlock()
	var sawParallelStart bool
	for _, status := range statuses {
		for _, event := range status.Events {
			if event.ItemType == "mcp" && event.Phase == "parallel_start" {
				sawParallelStart = true
			}
		}
	}
	if !sawParallelStart {
		t.Fatalf("expected mcp parallel_start event, got %+v", statuses)
	}
}

func parallelMCPDependencyForTest(id string, startedPath string, releasePath string) model.SemanticAgentMCPDependency {
	installCommand := fmt.Sprintf("printf started > %s; while [ ! -f %s ]; do sleep 0.05; done", shellQuoteForTest(startedPath), shellQuoteForTest(releasePath))
	if runtime.GOOS == "windows" {
		installCommand = fmt.Sprintf("powershell -NoProfile -Command \"Set-Content -LiteralPath '%s' -Value started; while (!(Test-Path -LiteralPath '%s')) { Start-Sleep -Milliseconds 50 }\"", strings.ReplaceAll(startedPath, "'", "''"), strings.ReplaceAll(releasePath, "'", "''"))
	}
	return model.SemanticAgentMCPDependency{
		ID:          id,
		Name:        id,
		Type:        "local",
		LaunchReady: false,
		Install: model.MCPInstallInfo{
			InstallCommands:    []string{installCommand},
			RunCommandTemplate: []string{"node", "${MCP_HOME}/server.js"},
		},
		Config: model.DeviceMCPServerInfo{
			Name:    id,
			Type:    "local",
			Enabled: true,
		},
	}
}

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func envListToMapForTest(items []string) map[string]string {
	out := map[string]string{}
	for _, item := range items {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}

func waitForPathForTest(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pathExists(path) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func freePortForTest(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func writeReleaseForTest(runtimeDir string, version string, executable string) error {
	name := filepath.Base(executable)
	dir := filepath.Join(runtimeDir, "versions", version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	target := filepath.Join(dir, name)
	data, err := os.ReadFile(executable)
	if err != nil {
		return err
	}
	if err := os.WriteFile(target, data, 0o755); err != nil {
		return err
	}
	if err := release.WriteManifest(runtimeDir, version, release.Manifest{
		Version:    version,
		Executable: name,
	}); err != nil {
		return err
	}
	return release.Switch(runtimeDir, version)
}

func writeFakeRuntimeZip(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	defer writer.Close()
	header := &zip.FileHeader{Name: "fake-runtime/bin/" + fakeRuntimeExecutableName()}
	header.SetMode(0o755)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = entry.Write([]byte(fakeRuntimeExecutableContent()))
	return err
}

func writeFakeJDKZip(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	defer writer.Close()
	header := &zip.FileHeader{Name: "OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.1_12.jdk/Contents/Home/bin/java"}
	header.SetMode(0o755)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = entry.Write([]byte(fakeRuntimeExecutableContent()))
	return err
}

func writeFakeNodeTarGz(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	defer gz.Close()
	writer := tar.NewWriter(gz)
	defer writer.Close()
	entries := []struct {
		name    string
		content string
	}{
		{"node-vtest/bin/node", "#!/bin/sh\necho v22.16.0\n"},
		{"node-vtest/lib/node_modules/npm/bin/npm-cli.js", "#!/bin/sh\necho 10.9.2\n"},
		{"node-vtest/lib/node_modules/npm/bin/npx-cli.js", "#!/bin/sh\necho 10.9.2\n"},
	}
	for _, entry := range entries {
		header := &tar.Header{
			Name: entry.name,
			Mode: 0o755,
			Size: int64(len(entry.content)),
		}
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if _, err := writer.Write([]byte(entry.content)); err != nil {
			return err
		}
	}
	links := []struct {
		name string
		link string
	}{
		{"node-vtest/bin/npm", "../lib/node_modules/npm/bin/npm-cli.js"},
		{"node-vtest/bin/npx", "../lib/node_modules/npm/bin/npx-cli.js"},
	}
	for _, link := range links {
		if err := writer.WriteHeader(&tar.Header{
			Name:     link.name,
			Mode:     0o755,
			Typeflag: tar.TypeSymlink,
			Linkname: link.link,
		}); err != nil {
			return err
		}
	}
	return nil
}

func writeFakeMCPTarGz(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	defer gz.Close()
	writer := tar.NewWriter(gz)
	defer writer.Close()
	content := "console.log('native mcp');\n"
	header := &tar.Header{
		Name: "server.js",
		Mode: 0o644,
		Size: int64(len(content)),
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	_, err = writer.Write([]byte(content))
	return err
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasSkillDefinition(values []model.SemanticAgentSkill, target string) bool {
	for _, value := range values {
		if value.Name == target {
			return true
		}
	}
	return false
}

func fakeRuntimeExecutableName() string {
	if runtime.GOOS == "windows" {
		return "fake-runtime.cmd"
	}
	return "fake-runtime"
}

func fakeRuntimeExecutableContent() string {
	if runtime.GOOS == "windows" {
		return "@echo off\r\necho fake-runtime 1.0.0\r\n"
	}
	return "#!/bin/sh\necho fake-runtime 1.0.0\n"
}

func executableFileName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".cmd"
	}
	return name
}

func writeFakeUVExecutable(path string, goos string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var content string
	if goos == "windows" {
		content = "@echo off\r\nsetlocal enabledelayedexpansion\r\nif \"%1\"==\"--version\" (echo uv 0.0.0& exit /b 0)\r\nset VERSION=\r\nset INSTALL_DIR=\r\n:loop\r\nif \"%1\"==\"\" goto done\r\nif \"%1\"==\"install\" set VERSION=%2\r\nif \"%1\"==\"--install-dir\" set INSTALL_DIR=%2\r\nshift\r\ngoto loop\r\n:done\r\nif \"%INSTALL_DIR%\"==\"\" exit /b 2\r\nif \"%VERSION%\"==\"\" set VERSION=3.12\r\nset ROOT=%INSTALL_DIR%\\cpython-%VERSION%-test\r\nmkdir \"%ROOT%\\bin\" >nul 2>nul\r\n>\"%ROOT%\\bin\\python.cmd\" echo @echo off\r\n>>\"%ROOT%\\bin\\python.cmd\" echo echo Python %VERSION%\r\n>\"%ROOT%\\bin\\pip.cmd\" echo @echo off\r\n>>\"%ROOT%\\bin\\pip.cmd\" echo echo pip %VERSION%\r\nexit /b 0\r\n"
	} else {
		content = "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'uv 0.0.0'; exit 0; fi\nversion=\"\"\ninstall_dir=\"\"\nprev=\"\"\nfor arg in \"$@\"; do\n  if [ \"$prev\" = \"install\" ]; then version=\"$arg\"; fi\n  if [ \"$prev\" = \"--install-dir\" ]; then install_dir=\"$arg\"; fi\n  prev=\"$arg\"\ndone\n[ -n \"$version\" ] || version=\"3.12\"\n[ -n \"$install_dir\" ] || exit 2\nroot=\"$install_dir/cpython-$version-test\"\nmkdir -p \"$root/bin\"\nprintf '#!/bin/sh\\necho Python %s\\n' \"$version\" > \"$root/bin/python\"\nprintf '#!/bin/sh\\necho pip %s\\n' \"$version\" > \"$root/bin/pip\"\nchmod +x \"$root/bin/python\" \"$root/bin/pip\"\n"
	}
	return os.WriteFile(path, []byte(content), 0o755)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func startNonHealthTCPServerForTest(t *testing.T, port int) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)))
	if err != nil {
		t.Fatalf("listen non-health server: %v", err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return listener
}

func hasPortArg(args []string, port int) bool {
	value := fmt.Sprintf("%d", port)
	for i, arg := range args {
		if arg == "--port" && i+1 < len(args) && args[i+1] == value {
			return true
		}
		if arg == "--port="+value {
			return true
		}
	}
	return false
}

func startHealthServerForTest(t *testing.T, port int) *http.Server {
	t.Helper()
	server := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)),
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}),
	}
	go func() {
		_ = server.ListenAndServe()
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if proc.IsHealthy(port, 100*time.Millisecond) {
			return server
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("health server on port %d did not start", port)
	return nil
}

func startMCPServerForTest(t *testing.T, port int) *http.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"playwright":{"status":"connected"},"verify":{"status":"connected"}}`))
	})
	mux.HandleFunc("/mcp/tools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"playwright":[{"id":"playwright_browser_navigate","description":"Navigate to a URL"}],"verify":[{"id":"verify_get_context","description":"读取当前上下文"}]}`))
	})
	server := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)),
		ReadHeaderTimeout: time.Second,
		Handler:           mux,
	}
	go func() {
		_ = server.ListenAndServe()
	}()
	deadline := time.Now().Add(2 * time.Second)
	url := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return server
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("mcp server on port %d did not start", port)
	return nil
}

func waitUntil(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func stopAgentForTest(t *testing.T, runtimeDir string, agentID string, pid int) {
	t.Helper()
	if pid > 0 {
		_ = proc.Stop(pid)
	}
	stdout, stderr := proc.DefaultBinaryLogs(runtimeDir, agentID)
	waitForFilesWritable(t, []string{stdout, stderr}, 3*time.Second)
}

func waitForFilesWritable(t *testing.T, paths []string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		allReady := true
		for _, path := range paths {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				continue
			}
			probe := path + ".release-check"
			_ = os.Remove(probe)
			err := os.Rename(path, probe)
			if err != nil {
				allReady = false
				break
			}
			if err := os.Rename(probe, path); err != nil {
				allReady = false
				break
			}
		}
		if allReady {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestHelperServeHealth(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_HEALTH_SERVER") != "1" {
		return
	}
	port := strings.TrimSpace(os.Getenv("HELPER_HEALTH_PORT"))
	if port == "" {
		for i, arg := range os.Args {
			if arg == "--port" && i+1 < len(os.Args) {
				port = strings.TrimSpace(os.Args[i+1])
				break
			}
			if strings.HasPrefix(arg, "--port=") {
				port = strings.TrimSpace(strings.TrimPrefix(arg, "--port="))
				break
			}
		}
	}
	if port == "" {
		t.Fatal("missing HELPER_HEALTH_PORT")
	}
	if rawDelay := strings.TrimSpace(os.Getenv("HELPER_HEALTH_DELAY_MS")); rawDelay != "" {
		delay, err := time.ParseDuration(rawDelay + "ms")
		if err != nil {
			t.Fatalf("invalid HELPER_HEALTH_DELAY_MS: %v", err)
		}
		time.Sleep(delay)
	}
	if file := strings.TrimSpace(os.Getenv("HELPER_CWD_FILE")); file != "" {
		dir, err := os.Getwd()
		if err != nil {
			t.Fatalf("get cwd: %v", err)
		}
		if err := os.WriteFile(file, []byte(dir+"\n"), 0o644); err != nil {
			t.Fatalf("write cwd file: %v", err)
		}
	}
	server := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", port),
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}),
	}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatalf("helper health server failed: %v", err)
	}
}

func TestEmitUserActionReportsBeforeReturning(t *testing.T) {
	reported := make(chan model.RuntimePreflightStatusPayload, 2)
	runner := runtimePreflightRunner{
		payload: model.RuntimePreflightStartPayload{
			JobID:           "preflight_1",
			MachineID:       "m_windows",
			LauncherAgentID: "agent_windows",
			SemanticAgentID: "reverse-windows",
		},
		reporter: func(payload model.RuntimePreflightStatusPayload) error {
			time.Sleep(20 * time.Millisecond)
			reported <- payload
			return nil
		},
	}
	runner.startReporter()
	defer runner.stopReporter()

	action := idaWindowsElevationAction(runner.payload.JobID)
	runner.emitUserAction("runtime", 49, action, nil)
	select {
	case payload := <-reported:
		if payload.Status != preflightStatusWaitingUserAction || !payload.RequiresUserAction || payload.UserAction == nil {
			t.Fatalf("unexpected user action payload: %+v", payload)
		}
		if payload.UserAction.ID != action.ID {
			t.Fatalf("unexpected action ID: %q", payload.UserAction.ID)
		}
	default:
		t.Fatal("emitUserAction returned before the reporter completed")
	}
}
