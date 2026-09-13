package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"launcher/internal/backgroundcmd"
	"launcher/internal/model"
)

const (
	idaLicenseScriptName = "keygen-v2.py"
	idaLicenseFileName   = "idapro.hexlic"
	idaLicenseStartDate  = "2026-07-27"
	idaEULARegistryKey   = "EULA 90"
	idaInstallTimeout    = 45 * time.Minute
	idaCommandTimeout    = 5 * time.Minute
	idaSmokeTestTimeout  = 10 * time.Minute
	idaInitRetryDelay    = 750 * time.Millisecond
)

type exitCoder interface {
	ExitCode() int
}

func (r *runtimePreflightRunner) installIDARuntime(item model.RuntimeCatalogItem, version model.RuntimeVersion, progressBase int) (installedRuntimeManifest, error) {
	candidates := r.runtimeCandidates(item, version)
	if len(candidates) == 0 {
		return installedRuntimeManifest{}, fmt.Errorf("运行时 %s %s 缺少当前平台 %s/%s 的 IDA 安装包", item.ID, version.Version, currentRuntimePlatform(), currentRuntimeArch())
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
			failures = append(failures, candidate.name+": 缺少 sha256")
			continue
		}
		archivePath := filepath.Join(r.service.cfg.RuntimeDir, "downloads", "runtimes", item.ID, version.Version, candidate.filename)
		if err := r.downloadRuntimeCandidate(candidate, archivePath, item.ID, progressBase); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate.name, err))
			continue
		}
		manifest, err := r.installIDARuntimeCandidate(item, version, candidate, archivePath, progressBase)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate.name, err))
			continue
		}
		return manifest, nil
	}
	return installedRuntimeManifest{}, fmt.Errorf("运行时 %s %s 安装失败：%s", item.ID, version.Version, strings.Join(failures, "；"))
}

func (r *runtimePreflightRunner) installIDARuntimeCandidate(item model.RuntimeCatalogItem, version model.RuntimeVersion, candidate runtimeInstallCandidate, archivePath string, progressBase int) (installedRuntimeManifest, error) {
	targetRoot := filepath.Join(r.service.cfg.RuntimeDir, "runtimes", item.ID, version.Version)
	stagingDir := filepath.Join(r.service.cfg.RuntimeDir, "runtimes", item.ID, ".install-"+version.Version+"-"+fmt.Sprint(time.Now().UnixNano()))
	_ = os.RemoveAll(stagingDir)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return installedRuntimeManifest{}, err
	}
	defer os.RemoveAll(stagingDir)
	if normalizePackageKind(candidate.kind, archivePath) != "zip" {
		return installedRuntimeManifest{}, fmt.Errorf("IDA 安装包必须是 zip，实际为 %s", candidate.kind)
	}
	r.emit(preflightStatusRunning, "runtime", progressBase+18, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemRuntime,
		ItemID:          item.ID,
		Phase:           "extract",
		Status:          "running",
		Message:         "正在解压 IDA 安装包",
		ProgressPercent: progressBase + 18,
	}})
	if err := unzipRuntimeArchive(archivePath, stagingDir); err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("解压 IDA 外层安装包失败：%w", err)
	}
	if currentRuntimePlatform() == "darwin" {
		if err := expandNestedIDAInstaller(stagingDir); err != nil {
			return installedRuntimeManifest{}, err
		}
	}
	installer, err := findIDAInstaller(stagingDir, currentRuntimePlatform())
	if err != nil {
		return installedRuntimeManifest{}, err
	}
	licenseScript, err := findFileByBase(stagingDir, idaLicenseScriptName)
	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("IDA 安装包缺少 %s：%w", idaLicenseScriptName, err)
	}
	_ = os.RemoveAll(targetRoot)
	if err := os.MkdirAll(filepath.Dir(targetRoot), 0o755); err != nil {
		return installedRuntimeManifest{}, err
	}
	installTarget := idaInstallerTarget(targetRoot)
	windowsElevation := currentRuntimePlatform() == "windows"
	if windowsElevation {
		r.emitUserAction("runtime", progressBase+20, idaWindowsElevationAction(r.payload.JobID), []model.RuntimeInstallEvent{{
			ItemType:        preflightItemRuntime,
			ItemID:          item.ID,
			Phase:           "user_action_required",
			Status:          preflightStatusWaitingUserAction,
			Message:         "等待在 Windows 电脑上确认 IDA 安装器管理员权限",
			ProgressPercent: progressBase + 20,
		}})
	} else {
		r.emit(preflightStatusRunning, "runtime", progressBase+20, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemRuntime,
			ItemID:          item.ID,
			Phase:           "install",
			Status:          "running",
			Message:         "正在无界面安装 IDA Professional",
			ProgressPercent: progressBase + 20,
		}})
	}
	output, err := runIDAInstaller(installer, installTarget)
	if windowsElevation {
		status := "success"
		message := "电脑端管理员确认已结束，继续处理 IDA 安装结果"
		if err != nil {
			status = "error"
			message = "电脑端管理员确认或 IDA 安装器执行未完成"
		}
		r.emit(preflightStatusRunning, "runtime", progressBase+20, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemRuntime,
			ItemID:          item.ID,
			Phase:           "user_action_resolved",
			Status:          status,
			Message:         message,
			ProgressPercent: progressBase + 20,
		}})
	}
	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("IDA 安装器执行失败：%w；输出：%s", err, summarizeCommandOutput(output))
	}
	idaDir, err := findIDABinaryDir(targetRoot)
	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("IDA 安装完成后未找到 IDALib：%w", err)
	}
	idaDir, err = prepareIDARuntimePath(idaDir)
	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("准备 IDA 兼容运行路径失败：%w", err)
	}
	targetScript := filepath.Join(idaDir, idaLicenseScriptName)
	if err := copyRuntimeFile(licenseScript, targetScript, 0o644); err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("复制 IDA 许可证脚本失败：%w", err)
	}
	licenseOutput, err := r.runIDALicenseCommand(targetScript, version)
	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("生成 IDA 许可证失败：%w；输出：%s", err, summarizeCommandOutput(licenseOutput))
	}
	patchOutput, err := r.runIDAPatchCommand(targetScript, version)
	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("应用 IDA 运行补丁失败：%w；输出：%s", err, summarizeCommandOutput(patchOutput))
	}
	signOutput, err := resignPatchedIDALibraries(idaDir)
	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("重签 IDA 运行库失败：%w；输出：%s", err, summarizeCommandOutput(signOutput))
	}
	activationOutput, err := r.activateIDALibrary(idaDir)
	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("激活 IDALib Python 路径失败：%w；输出：%s", err, summarizeCommandOutput(activationOutput))
	}
	eulaOutput, err := r.acceptIDAEULA(idaDir, version)
	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("初始化 IDA 批处理许可状态失败：%w；输出：%s", err, summarizeCommandOutput(eulaOutput))
	}
	manifest := installedRuntimeManifest{
		RuntimeID:       item.ID,
		RuntimeName:     item.Name,
		VersionID:       version.ID,
		Version:         version.Version,
		Platform:        currentRuntimePlatform(),
		Arch:            currentRuntimeArch(),
		InstallDir:      idaDir,
		BinPaths:        []string{"."},
		ExecutableNames: cleanedStringList(item.ExecutableNames),
		EnvPatch:        renderRuntimeEnvPatch(candidate.envPatch, idaDir, r.service.cfg.RuntimeDir),
		SourceType:      candidate.sourceType,
		SourceID:        candidate.sourceID,
		SHA256:          strings.ToLower(strings.TrimSpace(candidate.sha256)),
		InstalledAt:     time.Now().UTC(),
	}
	if len(manifest.ExecutableNames) == 0 {
		manifest.ExecutableNames = []string{"ida"}
	}
	if manifest.EnvPatch == nil {
		manifest.EnvPatch = map[string]string{}
	}
	manifest.EnvPatch["IDADIR"] = idaDir
	if err := verifyIDARuntimeManifest(manifest); err != nil {
		return installedRuntimeManifest{}, err
	}
	smokeOutput, err := smokeTestIDARuntime(idaDir)
	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("IDA 无界面分析验证失败：%w；输出：%s", err, summarizeCommandOutput(smokeOutput))
	}
	if err := writeRuntimeManifest(r.service.cfg.RuntimeDir, manifest); err != nil {
		return installedRuntimeManifest{}, err
	}
	r.emit(preflightStatusRunning, "runtime", progressBase+24, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemRuntime,
		ItemID:          item.ID,
		Phase:           "install",
		Status:          "success",
		Message:         "IDA Professional 安装、许可证和无界面分析验证完成",
		ProgressPercent: progressBase + 24,
		Details: map[string]any{
			"install_dir": idaDir,
			"installer":   summarizeCommandOutput(output),
			"license":     summarizeCommandOutput(licenseOutput),
			"patch":       summarizeCommandOutput(patchOutput),
			"sign":        summarizeCommandOutput(signOutput),
			"activation":  summarizeCommandOutput(activationOutput),
			"eula":        summarizeCommandOutput(eulaOutput),
			"smoke_test":  summarizeCommandOutput(smokeOutput),
		},
	}})
	return manifest, nil
}

func idaWindowsElevationAction(jobID string) model.RuntimeUserAction {
	return model.RuntimeUserAction{
		ID:      "ida-uac:" + strings.TrimSpace(jobID),
		Kind:    "windows_uac",
		Title:   "IDA 安装需要管理员确认",
		Message: "请在目标 Windows 电脑上确认管理员权限提示，安装会在确认后自动继续。",
		Instructions: []string{
			"切换到正在安装 IDA 的 Windows 电脑。",
			"在“用户账户控制”窗口中核对 IDA 安装器后点击“是”。",
			"确认后保持 Launcher 运行，安装结果会自动同步。",
		},
		RequestedAt: time.Now().UTC(),
	}
}

func idaInstallerTarget(targetRoot string) string {
	// InstallBuilder creates the IDA Professional .app bundle below --prefix on macOS.
	return targetRoot
}

func expandNestedIDAInstaller(root string) error {
	var nested string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry == nil || entry.IsDir() {
			return walkErr
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".app.zip") {
			nested = path
			return io.EOF
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if nested == "" {
		return errors.New("macOS IDA 安装包缺少内层 .app.zip")
	}
	return unzipRuntimeArchive(nested, filepath.Join(root, "installer"))
}

func findIDAInstaller(root string, platform string) (string, error) {
	var installer string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry == nil || entry.IsDir() {
			return walkErr
		}
		name := strings.ToLower(entry.Name())
		matched := platform == "windows" && strings.HasPrefix(name, "ida-pro_") && strings.HasSuffix(name, ".exe")
		matched = matched || platform == "darwin" && name == "installbuilder.sh"
		if matched {
			installer = path
			return io.EOF
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if installer == "" {
		return "", fmt.Errorf("未找到 %s IDA 安装器", platform)
	}
	if platform != "windows" {
		_ = os.Chmod(installer, 0o755)
	}
	return installer, nil
}

func runIDAInstaller(installer string, target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), idaInstallTimeout)
	defer cancel()
	platform := currentRuntimePlatform()
	args := idaInstallerArgs(platform, target)
	if platform == "windows" {
		return runElevatedIDAWindowsInstaller(ctx, installer, args)
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", append([]string{installer}, args...)...)
	backgroundcmd.Configure(cmd)
	cmd.Dir = filepath.Dir(installer)
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return string(output), errors.New("IDA 安装器执行超时")
	}
	return string(output), err
}

func idaInstallerArgs(platform string, target string) []string {
	args := []string{"--mode", "unattended", "--unattendedmodeui", "none", "--prefix", target}
	if platform == "windows" {
		// IDA 9.2's Windows InstallBuilder package requires an explicit choice.
		args = append(args, "--install_python", "1")
	}
	return args
}

func (r *runtimePreflightRunner) runIDALicenseCommand(script string, version model.RuntimeVersion) (string, error) {
	return r.runIDAPythonCommand(script, idaLicenseArgs(version), "IDA 许可证生成")
}

func (r *runtimePreflightRunner) runIDAPatchCommand(script string, version model.RuntimeVersion) (string, error) {
	return r.runIDAPythonCommand(script, idaPatchArgs(version), "IDA 运行补丁")
}

func (r *runtimePreflightRunner) runIDAPythonCommand(script string, args []string, operation string) (string, error) {
	pythonPath, err := r.managedPythonPath()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), idaCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, pythonPath, append([]string{filepath.Base(script)}, args...)...)
	backgroundcmd.Configure(cmd)
	cmd.Dir = filepath.Dir(script)
	output, runErr := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return string(output), fmt.Errorf("%s超时", operation)
	}
	return string(output), runErr
}

func (r *runtimePreflightRunner) activateIDALibrary(idaDir string) (string, error) {
	activationScript := filepath.Join(idaDir, "idalib", "python", "py-activate-idalib.py")
	if !pathExists(activationScript) {
		return "", fmt.Errorf("IDA 目录缺少 IDALib 激活脚本：%s", activationScript)
	}
	return r.runIDAPythonCommand(activationScript, []string{"-d", idaDir}, "IDALib 激活")
}

func (r *runtimePreflightRunner) acceptIDAEULA(idaDir string, version model.RuntimeVersion) (string, error) {
	pythonPath, err := r.managedPythonPath()
	if err != nil {
		return "", err
	}
	eulaKey := idaEULAKey(version)
	ctx, cancel := context.WithTimeout(context.Background(), idaCommandTimeout)
	defer cancel()
	return initializeIDAEULAWithRetry(
		ctx,
		func() (string, error) {
			return prepareIDAEULAState(idaDir, eulaKey)
		},
		func(ctx context.Context) (string, error) {
			return runIDAEULAProbe(ctx, pythonPath, idaDir, eulaKey)
		},
		idaInitRetryDelay,
	)
}

func runIDAEULAProbe(ctx context.Context, pythonPath string, idaDir string, eulaKey string) (string, error) {
	code := strings.Join([]string{
		"import os, sys",
		"ida_dir = sys.argv[1]",
		"sys.path.insert(0, os.path.join(ida_dir, 'idalib', 'python'))",
		"print('IDA batch license probe: importing idapro', flush=True)",
		"import idapro, ida_registry",
		"print('IDA batch license probe: idapro imported', flush=True)",
		"ida_registry.reg_write_bool(sys.argv[2], True)",
		"assert ida_registry.reg_read_bool(sys.argv[2], False), 'IDA batch license state was not persisted'",
		"print('IDA batch license state initialized')",
	}, "; ")
	cmd := exec.CommandContext(ctx, pythonPath, "-c", code, idaDir, eulaKey)
	backgroundcmd.Configure(cmd)
	cmd.Dir = idaDir
	cmd.Env = append(os.Environ(),
		"IDADIR="+idaDir,
		"PYTHONUNBUFFERED=1",
	)
	output, runErr := cmd.CombinedOutput()
	return string(output), runErr
}

func initializeIDAEULAWithRetry(
	ctx context.Context,
	prepare func() (string, error),
	probe func(context.Context) (string, error),
	retryDelay time.Duration,
) (string, error) {
	const maxAttempts = 2
	outputs := make([]string, 0, maxAttempts*2)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		preparationOutput, err := prepare()
		appendIDAAttemptOutput(&outputs, attempt, "EULA 注册表", preparationOutput)
		if err != nil {
			return strings.TrimSpace(strings.Join(outputs, "\n")), err
		}

		probeOutput, runErr := probe(ctx)
		appendIDAAttemptOutput(&outputs, attempt, "IDALib 导入探针", probeOutput)
		combinedOutput := strings.TrimSpace(strings.Join(outputs, "\n"))
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return combinedOutput, errors.New("IDA 批处理许可状态初始化超时")
		}
		if runErr == nil {
			return combinedOutput, nil
		}
		if attempt == maxAttempts || !isIDAFirstInitializationExit(runErr) {
			return combinedOutput, runErr
		}

		outputs = append(outputs, "IDALib 首次初始化返回退出码 255，重新确认 EULA 状态后重试")
		if retryDelay <= 0 {
			continue
		}
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return strings.TrimSpace(strings.Join(outputs, "\n")), errors.New("IDA 批处理许可状态初始化超时")
		case <-timer.C:
		}
	}
	return strings.TrimSpace(strings.Join(outputs, "\n")), errors.New("IDA 批处理许可状态初始化未完成")
}

func appendIDAAttemptOutput(outputs *[]string, attempt int, phase string, output string) {
	output = strings.TrimSpace(output)
	if output == "" {
		output = "(no output)"
	}
	*outputs = append(*outputs, fmt.Sprintf("IDA 初始化尝试 %d/2 - %s：\n%s", attempt, phase, output))
}

func isIDAFirstInitializationExit(err error) bool {
	var code exitCoder
	return errors.As(err, &code) && code.ExitCode() == 255
}

func (r *runtimePreflightRunner) managedPythonPath() (string, error) {
	manifest, ok := r.service.bestRuntimeManifest("python", ">=3.12 <3.15")
	if !ok {
		return "", errors.New("未找到托管 Python 3.12-3.14")
	}
	envPath := strings.Join(runtimeManifestBinDirs(manifest), string(os.PathListSeparator))
	for _, name := range []string{"python", "python3"} {
		if path, err := lookPathInDirs(name, envPath); err == nil {
			return path, nil
		}
	}
	return "", errors.New("托管 Python manifest 中没有可执行解释器")
}

func idaLicenseArgs(version model.RuntimeVersion) []string {
	if args := idaManifestArgs(version, "license_args"); len(args) > 0 {
		return args
	}
	return []string{"--license", "--start-date", idaLicenseStartDate}
}

func idaPatchArgs(version model.RuntimeVersion) []string {
	if args := idaManifestArgs(version, "patch_args"); len(args) > 0 {
		return args
	}
	return []string{"--patch", "--apply"}
}

func idaEULAKey(version model.RuntimeVersion) string {
	if value := strings.TrimSpace(fmt.Sprint(version.InstallManifest["eula_registry_key"])); value != "" && value != "<nil>" {
		return value
	}
	return idaEULARegistryKey
}

func idaManifestArgs(version model.RuntimeVersion, key string) []string {
	raw, ok := version.InstallManifest[key].([]any)
	if !ok {
		return nil
	}
	args := make([]string, 0, len(raw))
	for _, value := range raw {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
			args = append(args, text)
		}
	}
	return args
}

func resignPatchedIDALibraries(idaDir string) (string, error) {
	if currentRuntimePlatform() != "darwin" {
		return "Windows 不需要重签 IDA 运行库", nil
	}
	var outputs []string
	for _, name := range []string{"libida32.dylib", "libida.dylib"} {
		library := filepath.Join(idaDir, name)
		if !pathExists(library) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), idaCommandTimeout)
		cmd := exec.CommandContext(ctx, "/usr/bin/codesign", "--force", "--sign", "-", library)
		backgroundcmd.Configure(cmd)
		output, err := cmd.CombinedOutput()
		cancel()
		outputs = append(outputs, strings.TrimSpace(string(output)))
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return strings.Join(outputs, "\n"), fmt.Errorf("重签 %s 超时", name)
		}
		if err != nil {
			return strings.Join(outputs, "\n"), fmt.Errorf("重签 %s: %w", name, err)
		}
	}
	return strings.TrimSpace(strings.Join(outputs, "\n")), nil
}

func smokeTestIDARuntime(idaDir string) (string, error) {
	idatPath, err := findIDATextExecutable(idaDir)
	if err != nil {
		return "", err
	}
	inputPath, err := idaSmokeTestInput()
	if err != nil {
		return "", err
	}
	testDir, err := os.MkdirTemp(filepath.Dir(idaDir), ".ida-smoke-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(testDir)
	targetInput := filepath.Join(testDir, "input"+filepath.Ext(inputPath))
	if err := copyRuntimeFile(inputPath, targetInput, 0o755); err != nil {
		return "", fmt.Errorf("准备分析样本: %w", err)
	}
	logPath := filepath.Join(testDir, "idat.log")
	ctx, cancel := context.WithTimeout(context.Background(), idaSmokeTestTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, idatPath, "-A", "-L"+logPath, targetInput)
	backgroundcmd.Configure(cmd)
	cmd.Dir = testDir
	output, runErr := cmd.CombinedOutput()
	logOutput, _ := os.ReadFile(logPath)
	combined := strings.TrimSpace(strings.Join([]string{string(output), string(logOutput)}, "\n"))
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return combined, errors.New("idat 分析超时")
	}
	if runErr != nil {
		return combined, runErr
	}
	return combined, nil
}

func findIDATextExecutable(idaDir string) (string, error) {
	names := []string{"idat", "idat64"}
	if currentRuntimePlatform() == "windows" {
		names = []string{"idat.exe", "idat64.exe"}
	}
	for _, name := range names {
		candidate := filepath.Join(idaDir, name)
		if pathExists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("IDA 目录缺少文本模式可执行文件：%s", idaDir)
}

func idaSmokeTestInput() (string, error) {
	var candidates []string
	switch currentRuntimePlatform() {
	case "windows":
		windowsDir := strings.TrimSpace(os.Getenv("WINDIR"))
		if windowsDir != "" {
			candidates = append(candidates,
				filepath.Join(windowsDir, "System32", "where.exe"),
				filepath.Join(windowsDir, "System32", "cmd.exe"),
			)
		}
	case "darwin":
		candidates = append(candidates, "/usr/bin/true", "/bin/ls")
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, executable)
	}
	for _, candidate := range candidates {
		if pathExists(candidate) {
			return candidate, nil
		}
	}
	return "", errors.New("未找到 IDA 冒烟测试输入文件")
}

func findIDABinaryDir(root string) (string, error) {
	var result string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry == nil || entry.IsDir() {
			return walkErr
		}
		switch strings.ToLower(entry.Name()) {
		case "idalib.dll", "libidalib.dylib", "libidalib.so":
			result = filepath.Dir(path)
			return io.EOF
		default:
			return nil
		}
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if result == "" {
		return "", os.ErrNotExist
	}
	return result, nil
}

func findFileByBase(root string, name string) (string, error) {
	var result string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry == nil || entry.IsDir() {
			return walkErr
		}
		if strings.EqualFold(entry.Name(), name) {
			result = path
			return io.EOF
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if result == "" {
		return "", os.ErrNotExist
	}
	return result, nil
}

func copyRuntimeFile(source string, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	return errors.Join(copyErr, closeErr)
}

func verifyIDARuntimeManifest(manifest installedRuntimeManifest) error {
	if !validIDADir(manifest.InstallDir) {
		return fmt.Errorf("IDA 目录缺少 IDALib 动态库：%s", manifest.InstallDir)
	}
	if !pathExists(filepath.Join(manifest.InstallDir, idaLicenseFileName)) {
		return fmt.Errorf("IDA 目录缺少许可证文件 %s", idaLicenseFileName)
	}
	if currentRuntimePlatform() == "windows" {
		if !pathExists(filepath.Join(manifest.InstallDir, "ida.exe")) && !pathExists(filepath.Join(manifest.InstallDir, "ida64.exe")) {
			return errors.New("IDA Windows 安装目录缺少 ida.exe")
		}
	} else if !pathExists(filepath.Join(manifest.InstallDir, "ida")) {
		return errors.New("IDA macOS 安装目录缺少 ida")
	}
	return nil
}
