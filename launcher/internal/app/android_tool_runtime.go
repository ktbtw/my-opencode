package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"launcher/internal/fileutil"
	"launcher/internal/model"
)

// installJavaJarRuntime wraps a Java CLI jar with platform-native launchers so
// the tool participates in the same managed PATH as archive-based runtimes.
func (r *runtimePreflightRunner) installJavaJarRuntime(item model.RuntimeCatalogItem, version model.RuntimeVersion, progressBase int) (installedRuntimeManifest, error) {
	candidates := r.runtimeCandidates(item, version)
	if len(candidates) == 0 {
		return installedRuntimeManifest{}, fmt.Errorf("运行时 %s %s 缺少可验证 JAR 下载源", item.ID, version.Version)
	}
	var failures []string
	for _, candidate := range candidates {
		r.emit(preflightStatusRunning, "runtime", progressBase+5, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemRuntime,
			ItemID:          item.ID,
			Phase:           "download",
			Status:          "info",
			Message:         fmt.Sprintf("尝试下载 %s：%s", candidate.name, candidate.url),
			ProgressPercent: progressBase + 5,
		}})
		if strings.TrimSpace(candidate.sha256) == "" {
			failures = append(failures, candidate.name+" 缺少 sha256")
			continue
		}
		jarPath := filepath.Join(r.service.cfg.RuntimeDir, "downloads", "runtimes", item.ID, version.Version, candidate.filename)
		if err := r.downloadRuntimeCandidate(candidate, jarPath, item.ID, progressBase); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate.name, err))
			continue
		}
		manifest, err := r.stageJavaJarRuntime(item, version, jarPath, candidate, progressBase)
		if err == nil {
			return manifest, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", candidate.name, err))
	}
	return installedRuntimeManifest{}, fmt.Errorf("运行时 %s %s 安装失败：%s", item.ID, version.Version, strings.Join(failures, "；"))
}

func (r *runtimePreflightRunner) stageJavaJarRuntime(item model.RuntimeCatalogItem, version model.RuntimeVersion, jarPath string, candidate runtimeInstallCandidate, progressBase int) (installedRuntimeManifest, error) {
	targetDir := filepath.Join(r.service.cfg.RuntimeDir, "runtimes", item.ID, version.Version)
	stagingDir := filepath.Join(r.service.cfg.RuntimeDir, "runtimes", item.ID, ".install-"+version.Version+"-"+fmt.Sprint(time.Now().UnixNano()))
	if err := os.RemoveAll(stagingDir); err != nil {
		return installedRuntimeManifest{}, err
	}
	defer os.RemoveAll(stagingDir)
	if err := os.MkdirAll(filepath.Join(stagingDir, "lib"), 0o755); err != nil {
		return installedRuntimeManifest{}, err
	}
	r.emit(preflightStatusRunning, "runtime", progressBase+18, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemRuntime,
		ItemID:          item.ID,
		Phase:           "install",
		Status:          "running",
		Message:         "正在生成 Java 工具启动器",
		ProgressPercent: progressBase + 18,
	}})
	input, err := os.Open(jarPath)
	if err != nil {
		return installedRuntimeManifest{}, err
	}
	outputPath := filepath.Join(stagingDir, "lib", item.ID+".jar")
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		input.Close()
		return installedRuntimeManifest{}, err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := errors.Join(input.Close(), output.Close())
	if copyErr != nil || closeErr != nil {
		return installedRuntimeManifest{}, errors.Join(copyErr, closeErr)
	}

	binDir := filepath.Join(stagingDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return installedRuntimeManifest{}, err
	}
	unixWrapper := "#!/bin/sh\nset -eu\nBASE=$(CDPATH= cd -- \"$(dirname -- \"$0\")/..\" && pwd)\nexec java -jar \"$BASE/lib/" + item.ID + ".jar\" \"$@\"\n"
	if err := fileutil.AtomicWriteFile(filepath.Join(binDir, item.ID), []byte(unixWrapper), 0o755); err != nil {
		return installedRuntimeManifest{}, err
	}
	windowsWrapper := "@echo off\r\nsetlocal\r\njava -jar \"%~dp0..\\lib\\" + item.ID + ".jar\" %*\r\n"
	if err := fileutil.AtomicWriteFile(filepath.Join(binDir, item.ID+".bat"), []byte(windowsWrapper), 0o644); err != nil {
		return installedRuntimeManifest{}, err
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return installedRuntimeManifest{}, err
	}
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return installedRuntimeManifest{}, err
	}
	if err := os.Rename(stagingDir, targetDir); err != nil {
		return installedRuntimeManifest{}, err
	}
	manifest := installedRuntimeManifest{
		RuntimeID:       item.ID,
		RuntimeName:     item.Name,
		VersionID:       version.ID,
		Version:         version.Version,
		Platform:        currentRuntimePlatform(),
		Arch:            currentRuntimeArch(),
		InstallDir:      targetDir,
		BinPaths:        []string{"bin"},
		ExecutableNames: cleanedStringList(item.ExecutableNames),
		SourceType:      candidate.sourceType,
		SourceID:        candidate.sourceID,
		SHA256:          strings.ToLower(strings.TrimSpace(candidate.sha256)),
		InstalledAt:     time.Now().UTC(),
	}
	if err := writeRuntimeManifest(r.service.cfg.RuntimeDir, manifest); err != nil {
		return installedRuntimeManifest{}, err
	}
	return manifest, nil
}
