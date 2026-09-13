package bootstrap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"launcher/internal/config"
	"launcher/internal/defaults"
	"launcher/internal/download"
	"launcher/internal/mcpconfig"
	"launcher/internal/release"
	launcherRuntime "launcher/internal/runtime"
	"launcher/internal/toolchain"
)

type Console struct {
	In          io.Reader
	Out         io.Writer
	Interactive bool
}

type PrepareOptions struct {
	SkipCLIUpdate  bool
	SkipToolchains bool
}

func Prepare(cfg *config.Config, console Console) error {
	return PrepareWithOptions(cfg, console, PrepareOptions{})
}

func PrepareWithOptions(cfg *config.Config, console Console, options PrepareOptions) error {
	if cfg == nil {
		return errors.New("launcher 配置不能为空")
	}
	if console.In == nil {
		console.In = os.Stdin
	}
	if console.Out == nil {
		console.Out = os.Stdout
	}
	if err := launcherRuntime.Ensure(cfg.RuntimeDir); err != nil {
		return err
	}
	key, err := ensureOperatorKey(cfg, console)
	if err != nil {
		return err
	}
	cfg.Relay.OperatorKey = key
	if err := ensureCLIWithOptions(cfg, console.Out, options); err != nil {
		return err
	}
	if !options.SkipToolchains {
		_, err := EnsureToolchains(cfg, console.Out)
		if err != nil {
			fmt.Fprintf(console.Out, "托管运行时检测未完全通过: %v\n", err)
		}
	}
	return nil
}

func Warmup(cfg config.Config, out io.Writer) error {
	_, err := WarmupEnv(cfg, out)
	return err
}

func WarmupEnv(cfg config.Config, out io.Writer) (map[string]string, error) {
	patch, _, err := WarmupEnvDiagnostics(cfg, out)
	return patch, err
}

func WarmupEnvDiagnostics(cfg config.Config, out io.Writer) (map[string]string, toolchain.Diagnostics, error) {
	if out == nil {
		out = io.Discard
	}
	if cfg.Agent.Env == nil {
		cfg.Agent.Env = map[string]string{}
	}
	patch, diagnostics, err := ensureToolchains(&cfg, out)
	printToolchainDiagnostics(out, diagnostics)
	return patch.Env, diagnostics, err
}

func ApplyEnvPatch(target map[string]string, patch map[string]string) {
	mergeEnv(target, patch)
}

func EnsureToolchains(cfg *config.Config, out io.Writer) (map[string]string, error) {
	patch, diagnostics, err := ensureToolchains(cfg, out)
	printToolchainDiagnostics(out, diagnostics)
	return patch.Env, err
}

func CheckAndDownloadLatestCLI(ctx context.Context, cfg config.Config, out io.Writer) (string, bool, error) {
	if out == nil {
		out = io.Discard
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	current, _, err := release.CurrentExecutable(cfg.RuntimeDir)
	if err != nil {
		return "", false, err
	}
	current = strings.TrimSpace(current)
	if current == "" || current == "local" {
		return current, false, nil
	}
	latest, err := download.Latest()
	if err != nil {
		return current, false, nil
	}
	latest = strings.TrimSpace(latest)
	if latest == "" || latest == current {
		return current, false, nil
	}
	if err := ctx.Err(); err != nil {
		return current, false, err
	}
	fmt.Fprintf(out, "检测到新版本 opencode，准备后台自动更新: %s -> %s\n", current, latest)
	progress := newProgressPrinter(out)
	if err := download.FetchWithProgress(cfg.RuntimeDir, latest, progress.Update); err != nil {
		progress.Finish()
		return current, false, err
	}
	progress.Finish()
	if err := ctx.Err(); err != nil {
		return current, false, err
	}
	return latest, true, nil
}

func ensureToolchains(cfg *config.Config, out io.Writer) (toolchain.EnvPatch, toolchain.Diagnostics, error) {
	if cfg.Agent.Env == nil {
		cfg.Agent.Env = map[string]string{}
	}
	patch, diagnostics, err := toolchain.Ensure(toolchain.Options{
		RuntimeDir:  cfg.RuntimeDir,
		InstallJava: mcpConfigNeedsJava(),
		Progress: func(message string) {
			fmt.Fprintln(out, message)
		},
	})
	mergeEnv(cfg.Agent.Env, patch.Env)
	return patch, diagnostics, err
}

func mergeEnv(target map[string]string, patch map[string]string) {
	if target == nil {
		return
	}
	for key, value := range patch {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if existingKey, ok := envKeyInMap(target, key); ok {
			if isPathEnvKey(key) {
				target[existingKey] = mergePathValue(value, target[existingKey])
			}
			continue
		}
		target[key] = value
	}
}

func envKeyInMap(env map[string]string, key string) (string, bool) {
	for existing := range env {
		if isPathEnvKey(existing) && isPathEnvKey(key) {
			return existing, true
		}
		if existing == key {
			return existing, true
		}
	}
	return "", false
}

func isPathEnvKey(key string) bool {
	return strings.EqualFold(key, "PATH") || strings.EqualFold(key, "Path")
}

func mergePathValue(primary string, secondary string) string {
	seen := map[string]struct{}{}
	var parts []string
	for _, value := range []string{primary, secondary} {
		for _, item := range strings.Split(value, string(os.PathListSeparator)) {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			key := strings.ToLower(item)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			parts = append(parts, item)
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func printToolchainDiagnostics(out io.Writer, diagnostics toolchain.Diagnostics) {
	for _, item := range []toolchain.ToolStatus{
		diagnostics.Node,
		diagnostics.NPM,
		diagnostics.NPX,
		diagnostics.Python,
		diagnostics.PIP,
		diagnostics.UV,
		diagnostics.Java,
		diagnostics.Javac,
	} {
		if item.Name == "" {
			continue
		}
		if item.Error != "" {
			fmt.Fprintf(out, "运行时检测: %s unavailable: %s\n", item.Name, item.Error)
			continue
		}
		source := "system"
		if item.Managed {
			source = "managed"
		}
		fmt.Fprintf(out, "运行时检测: %s %s (%s) path=%s\n", item.Name, item.Version, source, item.Path)
	}
}

func mcpConfigNeedsJava() bool {
	cfg, err := mcpconfig.Load()
	if err != nil || cfg == nil {
		return false
	}
	for _, server := range cfg.Servers {
		if !server.Enabled || server.Type != "local" {
			continue
		}
		if commandUsesRuntime(server.Command, "java") {
			return true
		}
	}
	return false
}

func commandUsesRuntime(command []string, runtimeName string) bool {
	if len(command) == 0 {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(runtimeName))
	first := strings.ToLower(strings.TrimSpace(filepath.Base(command[0])))
	first = strings.TrimSuffix(first, ".exe")
	first = strings.TrimSuffix(first, ".cmd")
	first = strings.TrimSuffix(first, ".bat")
	return first == name
}

func IsInteractive(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func ensureOperatorKey(cfg *config.Config, console Console) (string, error) {
	if key := strings.TrimSpace(cfg.Relay.OperatorKey); key != "" {
		return key, nil
	}
	if key, err := readStoredOperatorKey(cfg.RuntimeDir); err == nil && key != "" {
		fmt.Fprintf(console.Out, "已读取本地 operator key: %s\n", maskKey(key))
		return key, nil
	}
	guide := guideURL(*cfg)
	fmt.Fprintf(console.Out, "未检测到 operator key。\n")
	fmt.Fprintf(console.Out, "请前往操作指引页获取: %s\n", guide)
	if runtime.GOOS == "windows" {
		fmt.Fprintln(console.Out, `也可以先执行: setx OPENCODE_RELAY_OPERATOR_KEY "你的 operator key"`)
	} else {
		fmt.Fprintln(console.Out, `也可以先执行: export OPENCODE_RELAY_OPERATOR_KEY="你的 operator key"`)
	}
	if !console.Interactive {
		return "", fmt.Errorf("缺少 operator key，请访问 %s 获取后重新启动", guide)
	}
	reader := bufio.NewReader(console.In)
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Fprint(console.Out, "请输入 operator key（直接回车退出）: ")
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		key := strings.TrimSpace(line)
		if key == "" {
			return "", fmt.Errorf("缺少 operator key，请访问 %s 获取后重新启动", guide)
		}
		if !strings.HasPrefix(key, "opk_") {
			fmt.Fprintln(console.Out, "operator key 格式看起来不正确，应以 opk_ 开头。")
			if errors.Is(err, io.EOF) {
				return "", fmt.Errorf("operator key 格式不正确")
			}
			continue
		}
		if saveErr := StoreOperatorKey(cfg.RuntimeDir, key); saveErr != nil {
			return "", saveErr
		}
		fmt.Fprintf(console.Out, "operator key 已保存: %s\n", maskKey(key))
		return key, nil
	}
	return "", fmt.Errorf("operator key 多次输入无效，请访问 %s 获取后重新启动", guide)
}

func ensureCLI(cfg *config.Config, out io.Writer) error {
	return ensureCLIWithOptions(cfg, out, PrepareOptions{})
}

func ensureCLIWithOptions(cfg *config.Config, out io.Writer, options PrepareOptions) error {
	if version, executable, err := release.CurrentExecutable(cfg.RuntimeDir); err == nil {
		if !options.SkipCLIUpdate {
			updated, latest, updateErr := ensureLatestCLI(cfg, out, version)
			if updateErr != nil {
				return updateErr
			}
			if updated {
				version = latest
				executable, err = release.ExecutablePath(cfg.RuntimeDir, version)
				if err != nil {
					return err
				}
			}
		}
		fmt.Fprintf(out, "已使用本地 opencode: version=%s path=%s\n", version, executable)
		return nil
	}
	version, err := release.EnsureLocal(cfg.RuntimeDir, cfg.Agent.BinaryName)
	if err == nil {
		fmt.Fprintf(out, "已接入系统 opencode: version=%s\n", version)
		return nil
	}
	if !errors.Is(err, release.ErrExecutableNotFound) {
		return err
	}
	fmt.Fprintln(out, "未检测到 opencode，可执行文件，开始自动下载当前平台版本...")
	progress := newProgressPrinter(out)
	version, err = download.FetchLatest(cfg.RuntimeDir, progress.Update)
	progress.Finish()
	if err != nil {
		return err
	}
	if err := release.Switch(cfg.RuntimeDir, version); err != nil {
		return err
	}
	_, executable, err := release.CurrentExecutable(cfg.RuntimeDir)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "opencode 下载完成: version=%s path=%s\n", version, executable)
	return nil
}

func ensureLatestCLI(cfg *config.Config, out io.Writer, current string) (bool, string, error) {
	current = strings.TrimSpace(current)
	if current == "" || current == "local" {
		return false, current, nil
	}
	latest, err := download.Latest()
	if err != nil {
		return false, current, nil
	}
	latest = strings.TrimSpace(latest)
	if latest == "" || latest == current {
		return false, current, nil
	}
	fmt.Fprintf(out, "检测到新版本 opencode，准备自动更新: %s -> %s\n", current, latest)
	progress := newProgressPrinter(out)
	if err := download.FetchWithProgress(cfg.RuntimeDir, latest, progress.Update); err != nil {
		progress.Finish()
		return false, current, err
	}
	progress.Finish()
	if err := release.Switch(cfg.RuntimeDir, latest); err != nil {
		return false, current, err
	}
	fmt.Fprintf(out, "opencode 已自动更新到: %s\n", latest)
	return true, latest, nil
}

func readStoredOperatorKey(runtimeDir string) (string, error) {
	data, err := os.ReadFile(operatorKeyPath(runtimeDir))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func StoreOperatorKey(runtimeDir string, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("operator key 不能为空")
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(operatorKeyPath(runtimeDir), []byte(key+"\n"), 0o600)
}

func operatorKeyPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "operator_key")
}

func guideURL(cfg config.Config) string {
	base := strings.TrimSpace(os.Getenv("LAUNCHER_PUBLIC_BASE"))
	if base == "" {
		base = strings.TrimSpace(cfg.Relay.URL)
		base = strings.TrimSuffix(base, "/ws/device")
		base = strings.TrimPrefix(base, "wss://")
		base = strings.TrimPrefix(base, "ws://")
		if strings.TrimSpace(base) != "" {
			if strings.HasPrefix(cfg.Relay.URL, "wss://") {
				base = "https://" + base
			} else {
				base = "http://" + base
			}
		}
	}
	base = strings.TrimRight(base, "/")
	if base == "" {
		base = defaults.PublicBase
	}
	return base + "/#/guide"
}

func maskKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return key
	}
	return key[:4] + "****" + key[len(key)-4:]
}

type progressPrinter struct {
	out         io.Writer
	lastPercent int64
	lastMiB     int64
}

func newProgressPrinter(out io.Writer) *progressPrinter {
	return &progressPrinter{
		out:         out,
		lastPercent: -1,
		lastMiB:     -1,
	}
}

func (p *progressPrinter) Update(received int64, total int64) {
	if total > 0 {
		percent := received * 100 / total
		if percent == p.lastPercent {
			return
		}
		if percent < 100 && p.lastPercent >= 0 && percent-p.lastPercent < 5 {
			return
		}
		p.lastPercent = percent
		fmt.Fprintf(p.out, "\r下载中: %3d%% (%s/%s)", percent, formatBytes(received), formatBytes(total))
		return
	}
	mib := received / (1024 * 1024)
	if mib == p.lastMiB && received > 0 {
		return
	}
	p.lastMiB = mib
	fmt.Fprintf(p.out, "\r下载中: %s", formatBytes(received))
}

func (p *progressPrinter) Finish() {
	fmt.Fprintln(p.out)
}

func formatBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	if size < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(size)/(1024*1024*1024))
}
