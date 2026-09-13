package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"launcher/internal/binarymeta"
	"launcher/internal/defaults"
	"launcher/internal/resumabledownload"
)

type Asset struct {
	Platform  string `json:"platform"`
	Filename  string `json:"filename"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256,omitempty"`
	Available bool   `json:"available"`
}

type VersionInfo struct {
	Version   string  `json:"version"`
	Channel   string  `json:"channel"`
	Changelog string  `json:"changelog,omitempty"`
	Downloads []Asset `json:"downloads"`
}

type CheckResult struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	TargetVersion   string `json:"target_version,omitempty"`
	Channel         string `json:"channel,omitempty"`
	Platform        string `json:"platform"`
	UpdateAvailable bool   `json:"update_available"`
	Asset           *Asset `json:"asset,omitempty"`
}

type Result struct {
	CheckResult
	Downloaded  bool   `json:"downloaded"`
	Applied     bool   `json:"applied"`
	DryRun      bool   `json:"dry_run,omitempty"`
	StagedPath  string `json:"staged_path,omitempty"`
	UpdaterPath string `json:"updater_path,omitempty"`
	Message     string `json:"message,omitempty"`
}

type Options struct {
	CurrentVersion string
	RuntimeDir     string
	BaseURL        string
	TargetVersion  string
	HealthURL      string
	Apply          bool
	Restart        bool
	RestartArgs    []string
	DryRun         bool
	Progress       func(received int64, total int64)
}

func Check(ctx context.Context, options Options) (CheckResult, error) {
	current := strings.TrimSpace(options.CurrentVersion)
	if current == "" {
		current = "dev"
	}
	base := versionBaseURL(options.BaseURL)
	body, err := fetchVersion(ctx, base)
	if err != nil {
		return CheckResult{}, err
	}
	latest := strings.TrimSpace(body.Version)
	target := strings.TrimSpace(options.TargetVersion)
	if target == "" {
		target = latest
	}
	if target == "" {
		return CheckResult{}, errors.New("launcher 版本源缺少 version")
	}
	if latest != "" && target != latest {
		return CheckResult{}, fmt.Errorf("版本源当前为 %s，不是目标版本 %s", latest, target)
	}
	asset, err := matchAsset(body.Downloads)
	if err != nil {
		return CheckResult{}, err
	}
	normalizeAssetURL(base, &asset)
	return CheckResult{
		CurrentVersion:  current,
		LatestVersion:   latest,
		TargetVersion:   target,
		Channel:         body.Channel,
		Platform:        currentPlatform(),
		UpdateAvailable: shouldUpdate(current, target),
		Asset:           &asset,
	}, nil
}

func shouldUpdate(current string, target string) bool {
	current = strings.TrimSpace(current)
	target = strings.TrimSpace(target)
	if current == target {
		return false
	}
	if cmp, ok := compareDottedVersion(current, target); ok {
		return cmp < 0
	}
	return current != target
}

func compareDottedVersion(left string, right string) (int, bool) {
	leftParts, leftOK := parseDottedVersion(left)
	rightParts, rightOK := parseDottedVersion(right)
	if !leftOK || !rightOK {
		return 0, false
	}
	maxLen := len(leftParts)
	if len(rightParts) > maxLen {
		maxLen = len(rightParts)
	}
	for i := 0; i < maxLen; i++ {
		var leftValue int
		var rightValue int
		if i < len(leftParts) {
			leftValue = leftParts[i]
		}
		if i < len(rightParts) {
			rightValue = rightParts[i]
		}
		if leftValue < rightValue {
			return -1, true
		}
		if leftValue > rightValue {
			return 1, true
		}
	}
	return 0, true
}

func parseDottedVersion(value string) ([]int, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	if value == "" {
		return nil, false
	}
	rawParts := strings.Split(value, ".")
	parts := make([]int, 0, len(rawParts))
	for _, raw := range rawParts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil, false
		}
		item, err := strconv.Atoi(raw)
		if err != nil {
			return nil, false
		}
		parts = append(parts, item)
	}
	return parts, len(parts) > 0
}

func Update(ctx context.Context, options Options) (Result, error) {
	check, err := Check(ctx, options)
	if err != nil {
		return Result{}, err
	}
	result := Result{CheckResult: check, DryRun: options.DryRun}
	if !check.UpdateAvailable {
		result.Message = "launcher 已是最新版本"
		return result, nil
	}
	if check.Asset == nil {
		return Result{}, errors.New("launcher 版本源缺少当前平台下载项")
	}
	if !check.Asset.Available {
		return Result{}, fmt.Errorf("launcher 安装包不可用: %s", check.Asset.Filename)
	}
	if options.DryRun {
		result.Message = "dry_run 已完成版本检测，未下载或替换"
		return result, nil
	}
	if strings.TrimSpace(options.RuntimeDir) == "" {
		return Result{}, errors.New("runtime dir 不能为空")
	}
	if strings.TrimSpace(check.Asset.SHA256) == "" {
		return Result{}, errors.New("launcher 版本源缺少当前平台安装包 SHA-256")
	}
	updateLock, err := acquireUpdateLock(options.RuntimeDir)
	if err != nil {
		return Result{}, err
	}
	lockHandedOff := false
	defer func() {
		if !lockHandedOff {
			updateLock.Release()
		}
	}()
	staged, err := downloadAndStage(ctx, options.RuntimeDir, check.TargetVersion, *check.Asset, options.Progress)
	if err != nil {
		return Result{}, err
	}
	result.Downloaded = true
	result.StagedPath = staged
	if !options.Apply {
		result.Message = "launcher 新版本已下载，尚未执行替换"
		return result, nil
	}
	updater, err := StartApply(ApplyOptions{
		RuntimeDir:      options.RuntimeDir,
		Source:          staged,
		Target:          updateTargetExecutable(options.RuntimeDir, currentExecutablePath()),
		Restart:         options.Restart,
		RestartArgs:     options.RestartArgs,
		WaitTimeout:     45 * time.Second,
		HealthTimeout:   45 * time.Second,
		ParentPID:       os.Getpid(),
		UpdaterLabel:    check.TargetVersion,
		ExpectedVersion: check.TargetVersion,
		HealthURL:       options.HealthURL,
		LockPath:        updateLock.Path(),
		StatusPath:      applyStatusPath(options.RuntimeDir),
	})
	if err != nil {
		return Result{}, err
	}
	result.Applied = true
	lockHandedOff = true
	result.UpdaterPath = updater
	result.Message = "launcher 自更新已调度，当前进程退出后完成替换"
	return result, nil
}

func versionBaseURL(input string) string {
	base := strings.TrimRight(strings.TrimSpace(input), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(os.Getenv("LAUNCHER_UPDATE_BASE_URL")), "/")
	}
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(os.Getenv("LAUNCHER_PUBLIC_BASE")), "/")
	}
	if base == "" {
		base = strings.TrimRight(defaults.PublicBase, "/")
	}
	return base
}

func fetchVersion(ctx context.Context, base string) (VersionInfo, error) {
	endpoint := base
	if !strings.HasSuffix(endpoint, "/api/launcher/version") {
		endpoint = strings.TrimRight(endpoint, "/") + "/api/launcher/version"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return VersionInfo{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return VersionInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return VersionInfo{}, fmt.Errorf("获取 launcher 版本信息失败: %s", resp.Status)
	}
	var body VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return VersionInfo{}, err
	}
	return body, nil
}

func matchAsset(list []Asset) (Asset, error) {
	return matchAssetForExecutable(list, currentExecutablePath())
}

func matchAssetForExecutable(list []Asset, executable string) (Asset, error) {
	platform := currentPlatform()
	candidates := make([]Asset, 0, len(list))
	for _, item := range list {
		if item.Platform == platform && !isTunnelHelperAsset(item) {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		return Asset{}, fmt.Errorf("未找到当前平台 %s 的 launcher 安装包", platform)
	}
	preferGUI := isGUILauncherExecutable(executable)
	for _, item := range candidates {
		if preferGUI == isGUILauncherAsset(item) {
			return item, nil
		}
	}
	return candidates[0], nil
}

func isGUILauncherExecutable(executable string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(executable)))
	return strings.Contains(base, "chat-codex-launcher")
}

func isGUILauncherAsset(asset Asset) bool {
	name := strings.ToLower(strings.TrimSpace(asset.Filename))
	return strings.Contains(name, "chat-codex-launcher")
}

func isTunnelHelperAsset(asset Asset) bool {
	name := strings.ToLower(strings.TrimSpace(asset.Filename))
	return strings.HasPrefix(name, "chat-codex-tunnel-")
}

func currentPlatform() string {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "darwin-arm64"
		}
		if runtime.GOARCH == "amd64" {
			return "darwin-x64"
		}
	case "linux":
		if runtime.GOARCH == "amd64" {
			return "linux-x64"
		}
		if runtime.GOARCH == "arm64" {
			return "linux-arm64"
		}
	case "windows":
		if runtime.GOARCH == "amd64" {
			return "windows-x64"
		}
		if runtime.GOARCH == "arm64" {
			return "windows-arm64"
		}
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

func normalizeAssetURL(base string, asset *Asset) {
	if asset == nil {
		return
	}
	asset.URL = resolveURL(base, asset.URL)
}

func resolveURL(base string, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err == nil && parsed.IsAbs() {
		return parsed.String()
	}
	baseURL, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if strings.HasPrefix(raw, "/") {
		return (&url.URL{Scheme: baseURL.Scheme, Host: baseURL.Host, Path: ref.Path, RawQuery: ref.RawQuery}).String()
	}
	if !strings.HasSuffix(baseURL.Path, "/") {
		baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/"
	}
	return baseURL.ResolveReference(ref).String()
}

func downloadAndStage(ctx context.Context, runtimeDir string, version string, asset Asset, progress func(int64, int64)) (string, error) {
	if strings.TrimSpace(asset.URL) == "" {
		return "", errors.New("launcher 下载地址不能为空")
	}
	workDir := filepath.Join(runtimeDir, "self-updates", version)
	if err := os.RemoveAll(workDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", err
	}
	archivePath := filepath.Join(runtimeDir, "downloads", asset.Filename)
	if err := fetchFile(ctx, asset.URL, archivePath, asset.SHA256, progress); err != nil {
		return "", err
	}
	name, err := unpack(archivePath, workDir)
	if err != nil {
		return "", err
	}
	if err := installStagedTunnelHelper(workDir, runtimeDir); err != nil {
		return "", err
	}
	staged := filepath.Join(workDir, name)
	if err := os.Chmod(staged, 0o755); err != nil {
		return "", err
	}
	return staged, nil
}

func installStagedTunnelHelper(workDir, runtimeDir string) error {
	name := binarymeta.ExecutableName("chat-codex-tunnel")
	source := filepath.Join(workDir, name)
	info, err := os.Stat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("隧道 helper 不是普通文件: %s", source)
	}
	target := filepath.Join(runtimeDir, "bin", name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temporary := target + ".update"
	_ = os.Remove(temporary)
	if err := copyFile(source, temporary); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o755); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if runtime.GOOS == "windows" {
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(temporary)
			return fmt.Errorf("替换隧道 helper 失败，请关闭正在运行的远程 SSH 连接后重试: %w", err)
		}
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("安装隧道 helper 失败: %w", err)
	}
	return nil
}

func fetchFile(ctx context.Context, rawURL string, target string, expectedSHA256 string, progress func(int64, int64)) error {
	_, err := resumabledownload.Fetch(ctx, resumabledownload.Options{
		URL:            rawURL,
		Target:         target,
		ExpectedSHA256: expectedSHA256,
		Progress: func(received int64, total int64) error {
			if progress != nil {
				progress(received, total)
			}
			return nil
		},
	})
	if err != nil {
		return fmt.Errorf("下载 launcher 失败: %w", err)
	}
	return nil
}

func unpack(archivePath string, target string) (string, error) {
	if strings.HasSuffix(archivePath, ".zip") {
		return unpackZip(archivePath, target)
	}
	if strings.HasSuffix(archivePath, ".tar.gz") {
		return unpackTarGz(archivePath, target)
	}
	return "", errors.New("不支持的 launcher 压缩包格式")
}

func unpackZip(archivePath string, target string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	launcherName := ""
	for _, file := range reader.File {
		name := filepath.Base(file.Name)
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		if !binarymeta.MatchesLauncherExecutableName(name) && !isTunnelHelperName(name) {
			continue
		}
		path := filepath.Join(target, name)
		if err := writeZipFile(file, path); err != nil {
			return "", err
		}
		if binarymeta.MatchesLauncherExecutableName(name) {
			launcherName = name
		}
	}
	if launcherName != "" {
		return launcherName, nil
	}
	return "", errors.New("压缩包中没有 launcher 可执行文件")
}

func isTunnelHelperName(name string) bool {
	return strings.EqualFold(filepath.Base(strings.TrimSpace(name)), binarymeta.ExecutableName("chat-codex-tunnel"))
}

func writeZipFile(file *zip.File, path string) error {
	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	writer, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer writer.Close()
	_, err = io.Copy(writer, reader)
	return err
}

func unpackTarGz(archivePath string, target string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		name := filepath.Base(header.Name)
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		if !binarymeta.MatchesLauncherExecutableName(name) {
			continue
		}
		path := filepath.Join(target, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		writer, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(writer, reader); err != nil {
			writer.Close()
			return "", err
		}
		writer.Close()
		return name, nil
	}
	return "", errors.New("压缩包中没有 launcher 可执行文件")
}

type ApplyOptions struct {
	RuntimeDir      string
	Source          string
	Target          string
	Restart         bool
	RestartArgs     []string
	WaitTimeout     time.Duration
	HealthTimeout   time.Duration
	ParentPID       int
	UpdaterLabel    string
	ExpectedVersion string
	HealthURL       string
	LockPath        string
	StatusPath      string
}

func StartApply(options ApplyOptions) (string, error) {
	if strings.TrimSpace(options.RuntimeDir) == "" {
		return "", errors.New("runtime dir 不能为空")
	}
	sourceRaw := strings.TrimSpace(options.Source)
	targetRaw := strings.TrimSpace(options.Target)
	if sourceRaw == "" || targetRaw == "" {
		return "", errors.New("source 和 target 不能为空")
	}
	source, err := filepath.Abs(sourceRaw)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(targetRaw)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(source); err != nil {
		return "", err
	}
	ownedSource, err := prepareApplyPayload(source, target, options.UpdaterLabel)
	if err != nil {
		return "", err
	}
	started := false
	defer func() {
		if !started {
			_ = os.Remove(ownedSource)
		}
	}()
	updater, err := copyUpdater(options.RuntimeDir, options.UpdaterLabel)
	if err != nil {
		return "", err
	}
	args := []string{
		"self-update-apply",
		"--source", ownedSource,
		"--cleanup-source",
		"--target", target,
		"--parent", strconv.Itoa(options.ParentPID),
		"--timeout", strconv.Itoa(int(options.WaitTimeout.Seconds())),
		"--health-timeout", strconv.Itoa(int(options.HealthTimeout.Seconds())),
	}
	if strings.TrimSpace(options.ExpectedVersion) != "" {
		args = append(args, "--expected-version", strings.TrimSpace(options.ExpectedVersion))
	}
	if strings.TrimSpace(options.HealthURL) != "" {
		args = append(args, "--health-url", strings.TrimSpace(options.HealthURL))
	}
	if strings.TrimSpace(options.LockPath) != "" {
		args = append(args, "--lock", strings.TrimSpace(options.LockPath))
	}
	if strings.TrimSpace(options.StatusPath) != "" {
		args = append(args, "--status", strings.TrimSpace(options.StatusPath))
	}
	if options.Restart {
		args = append(args, "--restart")
	}
	if len(options.RestartArgs) > 0 {
		args = append(args, "--")
		args = append(args, options.RestartArgs...)
	}
	pid, err := startDetachedCommand(updater, args)
	if err != nil {
		return "", err
	}
	started = true
	if strings.TrimSpace(options.LockPath) != "" && pid > 0 {
		// The updater also claims the lock at process start. This write only narrows
		// the handoff window and must not revoke an already running updater.
		_ = setUpdateLockOwner(options.LockPath, pid)
	}
	return updater, nil
}

func prepareApplyPayload(source, target, label string) (string, error) {
	label = sanitizeLabel(label)
	if label == "" {
		label = "update"
	}
	name := filepath.Base(target) + ".pending-" + label + "-" + strconv.Itoa(os.Getpid())
	payload := filepath.Join(filepath.Dir(target), name)
	_ = os.Remove(payload)
	if err := os.MkdirAll(filepath.Dir(payload), 0o755); err != nil {
		return "", err
	}
	if err := copyFile(source, payload); err != nil {
		return "", fmt.Errorf("准备 launcher 独立更新载荷失败: %w", err)
	}
	if err := os.Chmod(payload, 0o755); err != nil {
		_ = os.Remove(payload)
		return "", err
	}
	return payload, nil
}

func updateTargetExecutable(runtimeDir, current string) string {
	current = strings.TrimSpace(current)
	if !isGUILauncherExecutable(current) || strings.TrimSpace(runtimeDir) == "" {
		return current
	}
	return filepath.Join(strings.TrimSpace(runtimeDir), "bin", binarymeta.GUILauncherBinaryName())
}

func copyUpdater(runtimeDir string, label string) (string, error) {
	current := currentExecutablePath()
	if strings.TrimSpace(current) == "" {
		return "", errors.New("无法获取当前 launcher 可执行文件路径")
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = strconv.Itoa(os.Getpid())
	}
	name := "launcher-updater-" + sanitizeLabel(label)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(runtimeDir, "self-updates", "updater", name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := copyFile(current, target); err != nil {
		return "", err
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return "", err
	}
	return target, nil
}

func currentExecutablePath() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	if strings.TrimSpace(path) == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func copyFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func sanitizeLabel(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "update"
	}
	return b.String()
}

type ApplyCommand struct {
	Source          string
	Target          string
	ParentPID       int
	Restart         bool
	RestartArgs     []string
	Timeout         time.Duration
	HealthTimeout   time.Duration
	ExpectedVersion string
	HealthURL       string
}

func MarkRestartPending(runtimeDir string) error {
	path := restartPendingMarkerPath(runtimeDir)
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
}

func ConsumeRestartPending(runtimeDir string) bool {
	path := restartPendingMarkerPath(runtimeDir)
	if strings.TrimSpace(path) == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	_ = os.Remove(path)
	return true
}

func restartPendingMarkerPath(runtimeDir string) string {
	runtimeDir = strings.TrimSpace(runtimeDir)
	if runtimeDir == "" {
		return ""
	}
	return filepath.Join(runtimeDir, "self-updates", "restart-pending")
}

func Apply(command ApplyCommand) error {
	if strings.TrimSpace(command.Source) == "" || strings.TrimSpace(command.Target) == "" {
		return errors.New("source 和 target 不能为空")
	}
	if command.Timeout <= 0 {
		command.Timeout = 45 * time.Second
	}
	if command.HealthTimeout <= 0 {
		command.HealthTimeout = 45 * time.Second
	}
	deadline := time.Now().Add(command.Timeout)
	time.Sleep(250 * time.Millisecond)
	var lastErr error
	for time.Now().Before(deadline) {
		replacement, err := replaceExecutable(command.Source, command.Target)
		if err != nil {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if command.Restart {
			pid, err := startDetachedCommand(command.Target, command.RestartArgs)
			if err != nil {
				rollbackErr := rollbackExecutable(replacement)
				return errors.Join(fmt.Errorf("启动新版 launcher 失败: %w", err), rollbackErr)
			}
			if strings.TrimSpace(command.HealthURL) != "" && strings.TrimSpace(command.ExpectedVersion) != "" {
				if err := waitLauncherHealthy(command.HealthURL, command.ExpectedVersion, command.HealthTimeout); err != nil {
					if process, findErr := os.FindProcess(pid); findErr == nil {
						_ = process.Kill()
					}
					rollbackErr := rollbackExecutable(replacement)
					restartErr := restartExecutable(command.Target, command.RestartArgs)
					return errors.Join(fmt.Errorf("新版 launcher 健康检查失败，已执行回滚: %w", err), rollbackErr, restartErr)
				}
			}
		}
		finalizeReplacement(replacement)
		return nil
	}
	if lastErr == nil {
		lastErr = errors.New("等待替换 launcher 超时")
	}
	return lastErr
}

type executableReplacement struct {
	Source string
	Target string
	Staged string
	Backup string
}

func replaceExecutable(source string, target string) (executableReplacement, error) {
	replacement := executableReplacement{
		Source: source,
		Target: target,
		Staged: target + ".update",
		Backup: target + ".bak",
	}
	_ = os.Remove(replacement.Staged)
	if err := copyFile(source, replacement.Staged); err != nil {
		return executableReplacement{}, fmt.Errorf("复制新版 launcher 到目标目录失败: %w", err)
	}
	if err := os.Chmod(replacement.Staged, 0o755); err != nil {
		_ = os.Remove(replacement.Staged)
		return executableReplacement{}, err
	}
	if err := os.Remove(replacement.Backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(replacement.Staged)
		return executableReplacement{}, err
	}
	if err := os.Rename(target, replacement.Backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(replacement.Staged)
		return executableReplacement{}, err
	}
	if err := os.Rename(replacement.Staged, target); err != nil {
		_ = rollbackExecutable(replacement)
		return executableReplacement{}, err
	}
	_ = os.Chmod(target, 0o755)
	return replacement, nil
}
