package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"launcher/internal/config"
	"launcher/internal/model"
)

func TestRuntimeTaskGroupsInstallJavaToolsAfterArchive(t *testing.T) {
	groups := runtimeTaskGroups([]runtimePreflightTask{
		{runtimeID: "java", item: model.RuntimeCatalogItem{InstallStrategy: "archive"}},
		{runtimeID: "jadx", item: model.RuntimeCatalogItem{InstallStrategy: "java_tool_archive"}},
		{runtimeID: "apktool", item: model.RuntimeCatalogItem{InstallStrategy: "java_jar"}},
		{runtimeID: "adb", item: model.RuntimeCatalogItem{InstallStrategy: "archive"}},
	})
	if len(groups) != 3 {
		t.Fatalf("expected archive, java_tool, and java_jar groups, got %+v", groups)
	}
	if groups[0].name != "archive" || groups[1].name != "java_tool" || groups[2].name != "java_jar" {
		t.Fatalf("unexpected runtime group order: %+v", groups)
	}
}

func TestStageJavaJarRuntimeCreatesCrossPlatformWrapper(t *testing.T) {
	runtimeDir := t.TempDir()
	jarPath := filepath.Join(t.TempDir(), "apktool.jar")
	if err := os.WriteFile(jarPath, []byte("fixture jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := runtimePreflightRunner{service: &service{cfg: config.Config{RuntimeDir: runtimeDir}}}
	item := model.RuntimeCatalogItem{ID: "apktool", Name: "Apktool", ExecutableNames: []string{"apktool"}}
	version := model.RuntimeVersion{ID: "apktool-3.0.2", Version: "3.0.2"}
	manifest, err := runner.stageJavaJarRuntime(item, version, jarPath, runtimeInstallCandidate{sourceID: "fixture", sha256: "fixture"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.InstallDir == "" || !pathExists(manifest.InstallDir) {
		t.Fatalf("expected installed runtime directory, got %+v", manifest)
	}
	unixWrapper, err := os.ReadFile(filepath.Join(manifest.InstallDir, "bin", "apktool"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unixWrapper), "java -jar") || !strings.Contains(string(unixWrapper), "apktool.jar") {
		t.Fatalf("unexpected Unix wrapper: %s", unixWrapper)
	}
	windowsWrapper, err := os.ReadFile(filepath.Join(manifest.InstallDir, "bin", "apktool.bat"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(windowsWrapper), "java -jar") || !strings.Contains(string(windowsWrapper), "apktool.jar") {
		t.Fatalf("unexpected Windows wrapper: %s", windowsWrapper)
	}
}
