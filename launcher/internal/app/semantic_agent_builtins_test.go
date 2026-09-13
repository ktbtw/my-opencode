package app

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"launcher/internal/config"
	"launcher/internal/model"
)

func TestBuiltinSemanticAgentCatalogPayloadIsCompressed(t *testing.T) {
	if len(builtinSemanticCatalogGZIP) < 2 || !bytes.Equal(builtinSemanticCatalogGZIP[:2], []byte{0x1f, 0x8b}) {
		t.Fatal("expected built-in semantic Agent catalog to use gzip payload")
	}
	for _, signature := range [][]byte{[]byte("mimikatz"), []byte("Invoke-Mimikatz")} {
		if bytes.Contains(bytes.ToLower(builtinSemanticCatalogGZIP), bytes.ToLower(signature)) {
			t.Fatalf("compressed payload contains raw security-tool signature %q", signature)
		}
	}
	decoded, err := readBuiltinSemanticCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(decoded, []byte(`"version": 4`)) {
		t.Fatal("decoded catalog is missing version 4")
	}
}

func TestBuiltinSemanticAgentCatalogContainsExpandedProfiles(t *testing.T) {
	if len(semanticAgentDefinitions) != 6 {
		t.Fatalf("expected 6 built-in semantic agents, got %d", len(semanticAgentDefinitions))
	}
	if len(semanticAgentProfiles()) != 5 {
		t.Fatalf("expected 5 enabled built-in semantic agents, got %d", len(semanticAgentProfiles()))
	}
	item, ok := semanticAgentByID("reverse-windows")
	if !ok {
		t.Fatal("expected reverse-windows built-in agent")
	}
	profile := semanticAgentProfile(item)
	if len(profile.SkillDefinitions) != 1 || profile.SkillDefinitions[0].Name != "ida-reverse" {
		t.Fatalf("expected expanded ida-reverse skill, got %+v", profile.SkillDefinitions)
	}
	if strings.TrimSpace(profile.SkillDefinitions[0].Content) == "" || len(profile.SkillDefinitions[0].PackageFiles) == 0 {
		t.Fatal("expected built-in skill content and package files")
	}
	if len(profile.MCPDependencies) != 2 || len(profile.RecommendedMCPServers) != 2 {
		t.Fatalf("expected expanded Windows MCP dependencies, got %+v", profile.MCPDependencies)
	}
	var binaryDependency *model.SemanticAgentMCPDependency
	for index := range profile.MCPDependencies {
		if profile.MCPDependencies[index].ID == "windows-sarks0-binary-mcp" {
			binaryDependency = &profile.MCPDependencies[index]
		}
	}
	if binaryDependency == nil || len(binaryDependency.Install.Assets) == 0 || len(binaryDependency.Install.InstallCommands) == 0 {
		t.Fatalf("expected binary-mcp install definition, got %+v", binaryDependency)
	}
	var hasJava, hasGhidra, hasIDA bool
	for _, requirement := range profile.RuntimeRequirements {
		switch requirement.RuntimeID {
		case "java":
			hasJava = requirement.Required
		case "ghidra":
			hasGhidra = requirement.Required
		case "ida":
			hasIDA = requirement.Required
		}
	}
	if !hasJava || !hasGhidra || !hasIDA {
		t.Fatalf("expected Windows reverse agent to require Java, Ghidra, and IDA, got %+v", profile.RuntimeRequirements)
	}
}

func TestBuiltinAndroidAgentRequiresIDAAndRuntimeHookEvidence(t *testing.T) {
	item, ok := semanticAgentByID("reverse-android")
	if !ok {
		t.Fatal("expected reverse-android built-in agent")
	}
	profile := semanticAgentProfile(item)
	for _, required := range []string{
		"IDA MCP",
		"full auto-analysis",
		"60 minutes",
		"function boundaries",
		"hot-update",
		"native hook callback",
		"JAVA / FRIDA HOOK OPERATING RULES",
		"ClassLoader",
		"complete method signature",
		"scope-limited observation hook",
		"candidate -> located -> installed -> hit -> verified",
		"继续",
		"接着做",
	} {
		if !strings.Contains(profile.Prompt, required) {
			t.Errorf("reverse-android prompt is missing %q", required)
		}
	}

	var apkSkill string
	for _, skill := range profile.SkillDefinitions {
		if skill.Name == "apk-reverse" {
			apkSkill = skill.Content
			break
		}
	}
	for _, required := range []string{"IDA MCP", "全量自动分析", "函数边界", "热更新", "native callback"} {
		if !strings.Contains(apkSkill, required) {
			t.Errorf("apk-reverse skill is missing %q", required)
		}
	}
	runtimes := map[string]bool{}
	for _, requirement := range profile.RuntimeRequirements {
		if requirement.Required {
			runtimes[requirement.RuntimeID] = true
		}
	}
	for _, runtimeID := range []string{"jadx", "apktool", "adb"} {
		if !runtimes[runtimeID] {
			t.Errorf("reverse-android must require managed runtime %q", runtimeID)
		}
	}
	var idaMCP *model.SemanticAgentMCPDependency
	for index := range profile.MCPDependencies {
		if profile.MCPDependencies[index].ID == "ida-idalib-mcp" {
			idaMCP = &profile.MCPDependencies[index]
			break
		}
	}
	if idaMCP == nil {
		t.Fatal("reverse-android must include ida-idalib-mcp")
	}
	command := strings.Join(idaMCP.Config.Command, " ")
	for _, required := range []string{"--python 3.13", "--no-index", "ida_pro_mcp-2.0.0-py3-none-any.whl"} {
		if !strings.Contains(command, required) {
			t.Errorf("ida-idalib-mcp command is missing %q: %s", required, command)
		}
	}
	for _, key := range []string{"IDADIR"} {
		if _, ok := idaMCP.Config.Environment[key]; !ok {
			t.Errorf("ida-idalib-mcp environment is missing %q", key)
		}
	}
}

func TestRenderRuntimeEnvPatchExpandsManagedDirectories(t *testing.T) {
	installDir := filepath.Join(t.TempDir(), "runtimes", "ghidra", "12.1.2")
	runtimeDir := filepath.Dir(filepath.Dir(filepath.Dir(installDir)))
	got := renderRuntimeEnvPatch(map[string]string{
		"GHIDRA_HOME": "{install_dir}",
		"CACHE_DIR":   "{runtime_dir}/caches/ghidra",
	}, installDir, runtimeDir)
	if got["GHIDRA_HOME"] != installDir {
		t.Fatalf("expected install dir expansion, got %q", got["GHIDRA_HOME"])
	}
	if got["CACHE_DIR"] != filepath.Join(runtimeDir, "caches", "ghidra") {
		t.Fatalf("expected runtime dir expansion, got %q", got["CACHE_DIR"])
	}
}

func TestDiscoverIDADirFromProgramFiles(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "IDA Professional 9.2")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(want, "idalib.dll"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IDADIR", "")
	t.Setenv("IDA_HOME", "")
	t.Setenv("IDA_INSTALL_DIR", "")
	t.Setenv("ProgramFiles", root)
	t.Setenv("ProgramW6432", "")
	t.Setenv("ProgramFiles(x86)", "")
	if got := discoverIDADir(root); got != want {
		t.Fatalf("discover IDA dir: got %q want %q", got, want)
	}
}

func TestDiscoverIDADirFromManagedMacRuntime(t *testing.T) {
	runtimeDir := t.TempDir()
	want := filepath.Join(runtimeDir, "runtimes", "ida", "9.2", "IDA Professional 9.2.app", "Contents", "MacOS")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(want, "libidalib.dylib"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IDADIR", "")
	t.Setenv("IDA_HOME", "")
	t.Setenv("IDA_INSTALL_DIR", "")
	t.Setenv("ProgramFiles", "")
	t.Setenv("ProgramW6432", "")
	t.Setenv("ProgramFiles(x86)", "")
	if got := discoverIDADir(runtimeDir); got != want {
		t.Fatalf("discover managed mac IDA dir: got %q want %q", got, want)
	}
}

func TestApplyExternalToolEnvironmentPrefersInstalledIDAManifest(t *testing.T) {
	runtimeDir := t.TempDir()
	manifestDir := filepath.Join(runtimeDir, "runtimes", "ida", "9.1")
	discoveredDir := filepath.Join(runtimeDir, "runtimes", "ida", "9.9")
	for _, dir := range []string{manifestDir, discoveredDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, idalibFilenameForTest()), []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeRuntimeManifest(runtimeDir, installedRuntimeManifest{
		RuntimeID:  "ida",
		VersionID:  "ida-9.1",
		Version:    "9.1",
		Platform:   currentRuntimePlatform(),
		Arch:       currentRuntimeArch(),
		InstallDir: manifestDir,
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IDADIR", "")
	t.Setenv("IDA_HOME", "")
	t.Setenv("IDA_INSTALL_DIR", "")
	t.Setenv("ProgramFiles", "")
	t.Setenv("ProgramW6432", "")
	t.Setenv("ProgramFiles(x86)", "")

	env := map[string]string{"IDADIR": ""}
	(&service{cfg: config.Config{RuntimeDir: runtimeDir}}).applyExternalToolEnvironment(env)
	if got := env["IDADIR"]; got != manifestDir {
		t.Fatalf("IDADIR should use the verified runtime manifest: got %q want %q", got, manifestDir)
	}
}

func idalibFilenameForTest() string {
	switch currentRuntimePlatform() {
	case "windows":
		return "idalib.dll"
	case "darwin":
		return "libidalib.dylib"
	default:
		return "libidalib.so"
	}
}

func TestDiscoverNestedManagedGhidraHome(t *testing.T) {
	runtimeDir := t.TempDir()
	want := filepath.Join(runtimeDir, "tools", "ghidra", "ghidra_12.1.2_PUBLIC")
	if err := os.MkdirAll(filepath.Join(want, "support"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(want, "support", "analyzeHeadless.bat"), []byte("@echo off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHIDRA_HOME", "")
	t.Setenv("ProgramFiles", "")
	t.Setenv("ProgramW6432", "")
	t.Setenv("LOCALAPPDATA", "")
	if got := discoverGhidraHome(runtimeDir); got != want {
		t.Fatalf("discover Ghidra home: got %q want %q", got, want)
	}
}

func TestRepairMCPServerExternalToolEnvironment(t *testing.T) {
	runtimeDir := t.TempDir()
	want := filepath.Join(runtimeDir, "runtimes", "ghidra", "12.1.2")
	if err := os.MkdirAll(filepath.Join(want, "support"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(want, "support", "analyzeHeadless.bat"), []byte("@echo off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHIDRA_HOME", "")
	t.Setenv("ProgramFiles", "")
	t.Setenv("ProgramW6432", "")
	t.Setenv("LOCALAPPDATA", "")
	svc := &service{cfg: config.Config{RuntimeDir: runtimeDir}}
	server := svc.repairMCPServerExternalTools(model.DeviceMCPServerInfo{
		Name: "binary-mcp",
		Environment: map[string]string{
			"GHIDRA_HOME": "${MCP_HOME:ghidra}",
			"KEEP":        "value",
		},
	})
	if got := server.Environment["GHIDRA_HOME"]; got != want {
		t.Fatalf("repair GHIDRA_HOME: got %q want %q", got, want)
	}
	if got := server.Environment["KEEP"]; got != "value" {
		t.Fatalf("expected unrelated environment preserved, got %q", got)
	}
}

func TestWaitForSemanticMCPReadinessSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/mcp":
			_, _ = w.Write([]byte(`{"idalib-mcp":{"status":"connected"}}`))
		case "/mcp/tools":
			_, _ = w.Write([]byte(`{"idalib-mcp":[{"id":"idalib_open","description":"open sample"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, rawPort, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(rawPort, "%d", &port); err != nil {
		t.Fatal(err)
	}
	if err := waitForSemanticMCPReadiness(context.Background(), port, []semanticMCPReadinessTarget{{ItemID: "ida-idalib-mcp", Name: "idalib-mcp"}}); err != nil {
		t.Fatalf("wait for MCP readiness: %v", err)
	}
}

func TestAssessSemanticMCPReadinessReportsNamedFailures(t *testing.T) {
	targets := []semanticMCPReadinessTarget{
		{ItemID: "ida-idalib-mcp", Name: "idalib-mcp"},
		{ItemID: "windows-sarks0-binary-mcp", Name: "binary-mcp"},
	}
	ready, failures := assessSemanticMCPReadiness(targets, []model.MCPServerStatusInfo{
		{Name: "idalib-mcp", Status: "failed", Error: "IDA SDK missing"},
		{Name: "binary-mcp", Status: "connected"},
	}, map[string][]model.MCPToolInfo{})
	if ready {
		t.Fatal("expected MCP readiness failure")
	}
	joined := strings.Join(failures, "\n")
	if !strings.Contains(joined, "idalib-mcp：IDA SDK missing") {
		t.Fatalf("expected named startup failure, got %q", joined)
	}
	if !strings.Contains(joined, "binary-mcp：已连接但未暴露工具") {
		t.Fatalf("expected zero-tool failure, got %q", joined)
	}
}

func TestSemanticMCPReadinessAttributesOnlyFailedTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/mcp":
			_, _ = w.Write([]byte(`{"idalib-mcp":{"status":"disabled"},"verify":{"status":"connected"}}`))
		case "/mcp/tools":
			_, _ = w.Write([]byte(`{"verify":[{"id":"verify_compile","description":"compile"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, rawPort, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(rawPort, "%d", &port); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err = waitForSemanticMCPReadiness(ctx, port, []semanticMCPReadinessTarget{
		{ItemID: "ida-idalib-mcp", Name: "idalib-mcp"},
		{ItemID: "verify-mcp", Name: "verify"},
	})
	if err == nil {
		t.Fatal("expected readiness failure")
	}
	failures, attributed := semanticMCPReadinessItemFailures(err)
	if !attributed {
		t.Fatalf("expected per-target readiness failures, got %v", err)
	}
	if message := failures["ida-idalib-mcp"]; !strings.Contains(message, "状态=disabled") {
		t.Fatalf("expected idalib-mcp disabled failure, got %q", message)
	}
	if message, ok := failures["verify-mcp"]; ok {
		t.Fatalf("expected connected verify MCP to stay successful, got %q", message)
	}
}
