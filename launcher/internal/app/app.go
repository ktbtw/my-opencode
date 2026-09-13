package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"launcher/internal/aiconfig"
	"launcher/internal/autostart"
	"launcher/internal/binarymeta"
	"launcher/internal/bootstrap"
	"launcher/internal/config"
	"launcher/internal/control"
	"launcher/internal/download"
	"launcher/internal/fileutil"
	"launcher/internal/launcherlog"
	"launcher/internal/macapp"
	"launcher/internal/mcpconfig"
	"launcher/internal/model"
	"launcher/internal/opencodeconfig"
	proc "launcher/internal/process"
	"launcher/internal/projectidentity"
	"launcher/internal/relay"
	"launcher/internal/release"
	launcherRuntime "launcher/internal/runtime"
	"launcher/internal/selfupdate"
	runstate "launcher/internal/state"
	"launcher/internal/toolchain"
	launcherVersion "launcher/internal/version"
)

type service struct {
	cfg                   config.Config
	mu                    sync.RWMutex
	opsMu                 sync.Mutex
	updateMu              sync.Mutex
	directoryFilesMu      sync.RWMutex
	state                 runstate.DeviceState
	agents                []model.Agent
	processes             map[string]int
	runtimeMu             sync.Mutex
	semanticPreflightMu   sync.Mutex
	runtimeInfo           model.RuntimeEnvironmentInfo
	forceRespawnOnRestore bool
	restoreOnRun          bool
	shutdown              context.CancelFunc
}

type Service = service

type selfUpdateRunnerFunc func(context.Context, selfupdate.Options) (selfupdate.Result, error)
type cliUpdateRunnerFunc func(context.Context, config.Config, io.Writer) (string, bool, error)

var selfUpdateRunner selfUpdateRunnerFunc = selfupdate.Update
var cliUpdateRunner cliUpdateRunnerFunc = bootstrap.CheckAndDownloadLatestCLI

const (
	upgradeStageChecking      = "checking_version"
	upgradeStageDownloading   = "downloading"
	upgradeStageStopping      = "stopping_agents"
	upgradeStageSwitching     = "switching_version"
	upgradeStageStarting      = "starting_agents"
	upgradeStageCompleted     = "completed"
	upgradeStageFailed        = "failed"
	upgradeStageAlreadyLatest = "already_latest"
	maxUpgradeLogEntries      = 12
)

const defaultCompactionThresholdPercent = 80

func launcherLatencyLog(stage string, fields map[string]any) {
	payload := map[string]any{
		"component": "launcher",
		"stage":     stage,
	}
	for key, value := range fields {
		payload[key] = value
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[latency][launcher] stage=%s", stage)
		return
	}
	log.Printf("[latency][launcher] %s", string(data))
}

func NewService(cfg config.Config) (*service, error) {
	if err := launcherRuntime.Ensure(cfg.RuntimeDir); err != nil {
		return nil, err
	}
	if _, err := opencodeconfig.RemoveProviderWhitelist(); err != nil {
		return nil, err
	}
	if err := macapp.RepairInstalledMetadata(launcherVersion.Value); err != nil {
		log.Printf("[launcher.macapp] 修复码控应用元数据失败: %v", err)
	}
	localVersion, err := release.EnsureLocal(cfg.RuntimeDir, cfg.Agent.BinaryName)
	if err != nil {
		return nil, err
	}
	currentVersion, err := release.Current(cfg.RuntimeDir)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(currentVersion) == "" {
		currentVersion = localVersion
	}
	st, err := runstate.Load(cfg.RuntimeDir)
	if err != nil {
		return nil, err
	}
	if st.Status == "" {
		st.Status = "running"
	}
	if strings.EqualFold(st.Status, "upgrading") {
		st.Status = "failed"
		st.TargetVersion = ""
		st.UpgradeLocked = false
		st.UpgradeStage = upgradeStageFailed
		st.UpgradeProgress = 100
		st.UpgradeMessage = "上次升级未完成，已恢复为可重试状态"
		st.UpgradeUpdatedAt = time.Now().UTC()
		st.UpgradeLogs = appendUpgradeLog(st.UpgradeLogs, st.UpgradeStage, st.UpgradeMessage)
		if strings.TrimSpace(st.LastError) == "" {
			st.LastError = "上次升级未完成，已恢复为可重试状态"
		}
	}
	recoverStaleUpgradeLock(&st)
	applySelfUpdateStatus(&st, cfg.RuntimeDir)
	forceRespawnOnRestore := false
	if strings.TrimSpace(st.CurrentVersion) == "" {
		st.CurrentVersion = currentVersion
	} else if st.CurrentVersion != currentVersion {
		st.PreviousVersion = st.CurrentVersion
		st.CurrentVersion = currentVersion
		forceRespawnOnRestore = true
	}
	if strings.TrimSpace(st.TargetVersion) == strings.TrimSpace(st.CurrentVersion) {
		st.TargetVersion = ""
	}
	agents, err := loadAgents(cfg.RuntimeDir)
	if err != nil {
		return nil, err
	}
	st.AgentCount = len(agents)
	if err := runstate.Save(cfg.RuntimeDir, st); err != nil {
		return nil, err
	}
	svc := &service{
		cfg:                   cfg,
		state:                 st,
		agents:                agents,
		processes:             map[string]int{},
		forceRespawnOnRestore: forceRespawnOnRestore,
		restoreOnRun:          true,
	}
	return svc, nil
}

func recoverStaleUpgradeLock(st *runstate.DeviceState) {
	if st == nil || !st.UpgradeLocked || strings.EqualFold(st.Status, "upgrading") {
		return
	}
	message := "上次升级状态已恢复，可以重新操作"
	if st.UpgradeStage == upgradeStageSwitching && strings.Contains(st.UpgradeMessage, "launcher") {
		message = "launcher 自更新完成"
	}
	st.TargetVersion = ""
	st.UpgradeLocked = false
	st.UpgradeStage = upgradeStageCompleted
	st.UpgradeProgress = 100
	st.UpgradeMessage = message
	st.UpgradeUpdatedAt = time.Now().UTC()
	st.UpgradeLogs = appendUpgradeLog(st.UpgradeLogs, st.UpgradeStage, st.UpgradeMessage)
}

func applySelfUpdateStatus(st *runstate.DeviceState, runtimeDir string) {
	if st == nil {
		return
	}
	status, ok := selfupdate.ReadApplyStatus(runtimeDir)
	if !ok || !selfupdate.ApplyStatusAppliesToVersion(status, launcherVersion.Value) {
		return
	}
	applySelfUpdateStatusRecord(st, status)
}

func applySelfUpdateStatusRecord(st *runstate.DeviceState, status selfupdate.ApplyStatus) {
	if st == nil {
		return
	}
	previousUpgradeMessage := strings.TrimSpace(st.UpgradeMessage)
	switch strings.ToLower(strings.TrimSpace(status.State)) {
	case "failed":
		st.Status = "failed"
		st.UpgradeLocked = false
		st.UpgradeStage = upgradeStageFailed
		st.UpgradeProgress = 100
		st.UpgradeMessage = strings.TrimSpace(status.Message)
		st.LastError = strings.TrimSpace(status.Message)
		st.UpgradeUpdatedAt = status.UpdatedAt
		st.UpgradeLogs = appendUpgradeLog(st.UpgradeLogs, st.UpgradeStage, st.UpgradeMessage)
	case "completed":
		st.Status = "running"
		st.TargetVersion = ""
		st.UpgradeLocked = false
		st.UpgradeStage = upgradeStageCompleted
		st.UpgradeProgress = 100
		st.UpgradeMessage = strings.TrimSpace(status.Message)
		st.UpgradeUpdatedAt = status.UpdatedAt
		st.UpgradeLogs = appendUpgradeLog(st.UpgradeLogs, st.UpgradeStage, st.UpgradeMessage)
		if strings.TrimSpace(st.LastError) == previousUpgradeMessage || strings.TrimSpace(st.LastError) == selfupdate.ErrUpdateInProgress.Error() {
			st.LastError = ""
		}
	}
}

func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	svc, err := NewService(cfg)
	if err != nil {
		return err
	}
	return RunService(svc)
}

func RunService(svc *service) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return RunServiceContext(ctx, svc)
}

func RunServiceContext(ctx context.Context, svc *service) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	svc.setShutdown(stop)
	handler := control.NewHandler(svc)
	server := &http.Server{
		Addr:              svc.cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()
	fmt.Printf("launcher listening on http://%s\n", svc.cfg.ListenAddr)
	selfupdate.CleanupAsync(svc.cfg.RuntimeDir)
	svc.repairUpdaterAutostart()
	relay.Start(ctx, svc.cfg, svc)
	svc.startWarmup(ctx)
	svc.startCLIUpdate(ctx)
	svc.startSelfUpdateStatusMonitor(ctx)
	if svc.consumeRestoreOnRun() {
		svc.restoreAgentsAsync(ctx)
	}
	svc.startAutoSelfUpdate(ctx)

	select {
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var result error
		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.Canceled) {
			result = err
		}
		if err := svc.Shutdown(); err != nil {
			if result != nil {
				return errors.Join(result, err)
			}
			return err
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			if result != nil {
				return errors.Join(result, err)
			}
			return err
		}
		return result
	}
}

func (s *service) startWarmup(ctx context.Context) {
	go func() {
		info, err := s.prepareRuntimeEnvironment(ctx, true)
		if err != nil && ctx.Err() == nil {
			s.markLauncherOperationFailed(err)
			return
		}
		if strings.TrimSpace(info.LastError) == "" {
			s.clearLauncherOperationError("后台运行时准备失败")
		}
	}()
}

func (s *service) RuntimeEnvironment() (model.RuntimeEnvironmentInfo, error) {
	s.mu.RLock()
	info := cloneRuntimeEnvironmentInfo(s.runtimeInfo)
	s.mu.RUnlock()
	if !info.UpdatedAt.IsZero() || info.Preparing {
		return info, nil
	}
	return model.RuntimeEnvironmentInfo{
		RuntimeDir:  s.cfg.RuntimeDir,
		UpdatedAt:   time.Now().UTC(),
		Environment: cloneEnv(s.cfg.Agent.Env),
		Tools:       []model.RuntimeToolStatusInfo{},
		Logs:        []string{"运行时预热尚未完成，可点击准备运行时重新检测。"},
	}, nil
}

const (
	defaultDiagnosticLogLines = 200
	maxDiagnosticLogLines     = 1000
	maxDiagnosticLogBytes     = 256 << 10
)

var diagnosticSecretPattern = regexp.MustCompile(`(?i)(token|secret|password|api[_-]?key)(\s*[=:]\s*)([^\s,;]+)`)

func (s *service) Diagnostics(input model.LauncherDiagnosticsInput) (model.LauncherDiagnostics, error) {
	lines := input.LogLines
	if lines <= 0 {
		lines = defaultDiagnosticLogLines
	}
	if lines > maxDiagnosticLogLines {
		lines = maxDiagnosticLogLines
	}
	state := s.State()
	runtimeInfo, _ := s.RuntimeEnvironment()
	result := model.LauncherDiagnostics{
		CollectedAt:     time.Now().UTC(),
		Platform:        runtime.GOOS,
		Architecture:    runtime.GOARCH,
		LauncherVersion: launcherVersion.Value,
		RuntimeDir:      redactDiagnosticText(s.cfg.RuntimeDir, s.cfg.RuntimeDir),
		State:           state,
		Agents:          s.Agents(),
		Runtime:         redactRuntimeDiagnostics(runtimeInfo, s.cfg.RuntimeDir),
	}
	result.State = redactDeviceViewDiagnostics(result.State, s.cfg.RuntimeDir)
	for index := range result.Agents {
		result.Agents[index].ProjectDir = redactDiagnosticText(result.Agents[index].ProjectDir, s.cfg.RuntimeDir)
	}
	agentID := strings.TrimSpace(input.AgentID)
	if agentID != "" {
		status, err := s.MCPStatus(agentID)
		if err != nil {
			result.Warnings = append(result.Warnings, "MCP 状态采集失败："+redactDiagnosticText(err.Error(), s.cfg.RuntimeDir))
		} else {
			result.MCPStatus = status
		}
	}
	if input.IncludeLogs {
		result.Logs = map[string][]string{}
		launcherLines, err := tailDiagnosticFile(launcherlog.Path(s.cfg.RuntimeDir), lines, s.cfg.RuntimeDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			result.Warnings = append(result.Warnings, "launcher 日志读取失败："+err.Error())
		} else if len(launcherLines) > 0 {
			result.Logs["launcher"] = launcherLines
		}
		agents := result.Agents
		if agentID != "" {
			agents = nil
			for _, agent := range result.Agents {
				if agent.AgentID == agentID {
					agents = append(agents, agent)
					break
				}
			}
		}
		for _, agent := range agents {
			stdoutPath, stderrPath := proc.DefaultBinaryLogs(s.cfg.RuntimeDir, agent.AgentID)
			for label, path := range map[string]string{
				agent.AgentID + "/stdout": stdoutPath,
				agent.AgentID + "/stderr": stderrPath,
			} {
				logLines, err := tailDiagnosticFile(path, lines, s.cfg.RuntimeDir)
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					result.Warnings = append(result.Warnings, label+" 读取失败："+err.Error())
					continue
				}
				if len(logLines) > 0 {
					result.Logs[label] = logLines
				}
			}
		}
	}
	return result, nil
}

func tailDiagnosticFile(path string, lineLimit int, runtimeDir string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	start := stat.Size() - maxDiagnosticLogBytes
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDiagnosticLogBytes))
	if err != nil {
		return nil, err
	}
	items := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if start > 0 && len(items) > 0 {
		items = items[1:]
	}
	if len(items) > lineLimit {
		items = items[len(items)-lineLimit:]
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimRight(item, "\r"); item != "" {
			out = append(out, redactDiagnosticText(item, runtimeDir))
		}
	}
	return out, nil
}

func redactRuntimeDiagnostics(info model.RuntimeEnvironmentInfo, runtimeDir string) model.RuntimeEnvironmentInfo {
	info.RuntimeDir = redactDiagnosticText(info.RuntimeDir, runtimeDir)
	info.Environment = nil
	for index := range info.Tools {
		info.Tools[index].Path = redactDiagnosticText(info.Tools[index].Path, runtimeDir)
		info.Tools[index].Error = redactDiagnosticText(info.Tools[index].Error, runtimeDir)
	}
	for index := range info.Logs {
		info.Logs[index] = redactDiagnosticText(info.Logs[index], runtimeDir)
	}
	info.LastError = redactDiagnosticText(info.LastError, runtimeDir)
	return info
}

func redactDeviceViewDiagnostics(view model.DeviceView, runtimeDir string) model.DeviceView {
	view.LastError = redactDiagnosticText(view.LastError, runtimeDir)
	view.UpgradeMessage = redactDiagnosticText(view.UpgradeMessage, runtimeDir)
	for index := range view.UpgradeLogs {
		view.UpgradeLogs[index].Message = redactDiagnosticText(view.UpgradeLogs[index].Message, runtimeDir)
	}
	if view.Autostart != nil {
		copy := *view.Autostart
		copy.Path = redactDiagnosticText(copy.Path, runtimeDir)
		copy.Command = redactDiagnosticText(copy.Command, runtimeDir)
		copy.Error = redactDiagnosticText(copy.Error, runtimeDir)
		view.Autostart = &copy
	}
	return view
}

func redactDiagnosticText(value string, runtimeDir string) string {
	if value == "" {
		return ""
	}
	if runtimeDir = strings.TrimSpace(runtimeDir); runtimeDir != "" {
		value = strings.ReplaceAll(value, runtimeDir, "<RUNTIME_DIR>")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		value = strings.ReplaceAll(value, home, "<HOME>")
	}
	return diagnosticSecretPattern.ReplaceAllString(value, "$1$2<redacted>")
}

func (s *service) PrepareRuntimeEnvironment() (model.RuntimeEnvironmentInfo, error) {
	if !s.runtimeMu.TryLock() {
		return s.RuntimeEnvironment()
	}
	info := s.runtimePreparingInfo("正在后台准备托管运行时。")
	s.setRuntimeEnvironmentInfo(info)
	go func() {
		defer s.runtimeMu.Unlock()
		result, err := s.prepareRuntimeEnvironmentLocked(context.Background(), true)
		if err != nil {
			s.markLauncherOperationFailed(err)
			return
		}
		if strings.TrimSpace(result.LastError) == "" {
			s.clearLauncherOperationError("后台运行时准备失败")
		}
	}()
	return info, nil
}

func (s *service) prepareRuntimeEnvironment(ctx context.Context, reloadAgents bool) (model.RuntimeEnvironmentInfo, error) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	return s.prepareRuntimeEnvironmentLocked(ctx, reloadAgents)
}

func (s *service) prepareRuntimeEnvironmentLocked(ctx context.Context, reloadAgents bool) (model.RuntimeEnvironmentInfo, error) {
	if err := ctx.Err(); err != nil {
		return model.RuntimeEnvironmentInfo{}, err
	}
	s.setRuntimeEnvironmentInfo(s.runtimePreparingInfo("正在检测托管运行时。"))
	var buf bytes.Buffer
	patch, diagnostics, err := bootstrap.WarmupEnvDiagnostics(s.cfg, &buf)
	info := runtimeEnvironmentInfoFromDiagnostics(s.cfg.RuntimeDir, patch, diagnostics, buf.String(), err)
	s.setRuntimeEnvironmentInfo(info)
	changed := false
	if len(patch) > 0 {
		changed = s.applyAgentEnvPatch(patch)
	}
	if changed && reloadAgents && ctx.Err() == nil {
		if reloadErr := s.refreshAgentsAfterRuntimeEnvReady(ctx); reloadErr != nil {
			if !errors.Is(reloadErr, context.Canceled) {
				info.LastError = friendlyRuntimeError(reloadErr.Error())
				info.Logs = append(info.Logs, "运行时环境已更新，但重载 Agent 失败："+info.LastError)
				s.setRuntimeEnvironmentInfo(info)
			}
			return info, reloadErr
		}
		info.Logs = append(info.Logs, "运行时环境已写入 Agent 环境，Agent 已重载。")
		s.setRuntimeEnvironmentInfo(info)
	}
	return info, err
}

func (s *service) runtimePreparingInfo(message string) model.RuntimeEnvironmentInfo {
	return model.RuntimeEnvironmentInfo{
		RuntimeDir:  s.cfg.RuntimeDir,
		Preparing:   true,
		UpdatedAt:   time.Now().UTC(),
		Environment: cloneEnv(s.cfg.Agent.Env),
		Logs:        []string{message},
	}
}

func (s *service) setRuntimeEnvironmentInfo(info model.RuntimeEnvironmentInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeInfo = cloneRuntimeEnvironmentInfo(info)
}

func (s *service) applyAgentEnvPatch(patch map[string]string) bool {
	if len(patch) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Agent.Env == nil {
		s.cfg.Agent.Env = map[string]string{}
	}
	before := cloneEnv(s.cfg.Agent.Env)
	bootstrap.ApplyEnvPatch(s.cfg.Agent.Env, patch)
	return !envMapsEqual(before, s.cfg.Agent.Env)
}

func (s *service) refreshAgentsAfterRuntimeEnvReady(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.reloadAgentsWithOpsLocked()
}

func (s *service) startCLIUpdate(ctx context.Context) {
	go s.runCLIUpdate(ctx)
}

func (s *service) runCLIUpdate(ctx context.Context) {
	s.runCLIUpdateOnce(ctx)
	if s.cfg.SelfUpdate.Interval <= 0 {
		return
	}
	ticker := time.NewTicker(s.cfg.SelfUpdate.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runCLIUpdateOnce(ctx)
		}
	}
}

func (s *service) runCLIUpdateOnce(ctx context.Context) {
	if ctx.Err() != nil || !s.updateMu.TryLock() {
		return
	}
	defer s.updateMu.Unlock()
	latest, updated, err := cliUpdateRunner(ctx, s.cfg, os.Stdout)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("[launcher.cli-update] 后台检查失败: %v", err)
		}
		return
	}
	if !updated || ctx.Err() != nil {
		return
	}
	if err := s.switchToDownloadedVersion(ctx, latest); err != nil && ctx.Err() == nil {
		log.Printf("[launcher.cli-update] 后台切换失败: %v", err)
	}
}

func (s *service) switchToDownloadedVersion(ctx context.Context, version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := release.Current(s.cfg.RuntimeDir)
	if err != nil {
		return err
	}
	current = strings.TrimSpace(current)
	if current == version {
		return nil
	}
	if _, err := release.ManifestFor(s.cfg.RuntimeDir, version); err != nil {
		return err
	}
	if err := s.beginUpgradeState(current, version, upgradeStageSwitching, "后台已下载新版本，准备切换", 55); err != nil {
		return err
	}
	s.updateUpgradeProgress(upgradeStageStopping, "正在停止 agent", 65)
	stopErr := s.stopAllAgentProcesses("stopping")
	s.updateUpgradeProgress(upgradeStageSwitching, "正在切换 opencode 版本", 80)
	if err := release.Switch(s.cfg.RuntimeDir, version); err != nil {
		s.markUpgradeFailed(current, err)
		return err
	}
	s.updateUpgradeProgress(upgradeStageStarting, "正在启动 agent", 90)
	startErr := s.startEnabledAgentsWithOpsLocked("starting")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.CurrentVersion = version
	s.state.TargetVersion = ""
	s.state.Status = "running"
	s.state.UpgradeLocked = false
	s.state.UpgradeStage = upgradeStageCompleted
	s.state.UpgradeProgress = 100
	s.state.UpgradeMessage = "opencode 升级完成"
	s.state.UpgradeUpdatedAt = time.Now().UTC()
	s.state.UpgradeLogs = appendUpgradeLog(s.state.UpgradeLogs, s.state.UpgradeStage, s.state.UpgradeMessage)
	s.syncStateLastErrorLocked()
	if s.state.LastError == "" {
		if stopErr != nil {
			s.state.LastError = stopErr.Error()
		} else if startErr != nil {
			s.state.LastError = startErr.Error()
		}
	}
	if err := saveAgents(s.cfg.RuntimeDir, s.agents); err != nil {
		return err
	}
	return runstate.Save(s.cfg.RuntimeDir, s.state)
}

func (s *service) setShutdown(cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdown = cancel
}

func (s *service) consumeRestoreOnRun() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.restoreOnRun {
		return false
	}
	s.restoreOnRun = false
	return true
}

func (s *service) restoreAgents() error {
	return s.restoreAgentsContext(context.Background())
}

func (s *service) restoreAgentsAsync(ctx context.Context) {
	go func() {
		err := s.restoreAgentsContext(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			s.markLauncherOperationFailed(err)
		}
	}()
}

func (s *service) startAutoSelfUpdate(ctx context.Context) {
	cfg := s.cfg.SelfUpdate
	if !cfg.Enabled {
		return
	}
	go s.runAutoSelfUpdate(ctx, cfg)
}

func (s *service) runAutoSelfUpdate(ctx context.Context, cfg config.SelfUpdateConfig) {
	if cfg.InitialDelay > 0 {
		timer := time.NewTimer(cfg.InitialDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	if s.runAutoSelfUpdateOnce(ctx, cfg) {
		return
	}
	if cfg.Interval <= 0 {
		return
	}
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.runAutoSelfUpdateOnce(ctx, cfg) {
				return
			}
		}
	}
}

func (s *service) runAutoSelfUpdateOnce(ctx context.Context, cfg config.SelfUpdateConfig) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	for !s.updateMu.TryLock() {
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
	defer s.updateMu.Unlock()
	if err := ctx.Err(); err != nil {
		return false
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	updateCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := selfUpdateRunner(updateCtx, selfupdate.Options{
		CurrentVersion: launcherVersion.Value,
		RuntimeDir:     s.cfg.RuntimeDir,
		BaseURL:        cfg.BaseURL,
		HealthURL:      LauncherHealthURL(s.cfg.ListenAddr),
		Apply:          true,
		Restart:        cfg.Restart,
		RestartArgs:    os.Args[1:],
	})
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("[launcher.self-update] 自动检查失败: %v", err)
		}
		return false
	}
	if !result.UpdateAvailable {
		return false
	}
	if !result.Applied {
		log.Printf("[launcher.self-update] 检测到新版本 %s，但未完成自动替换: %s", result.TargetVersion, result.Message)
		return false
	}
	if err := selfupdate.MarkRestartPending(s.cfg.RuntimeDir); err != nil {
		log.Printf("[launcher.self-update] 写入自更新重启标记失败: %v", err)
	}
	log.Printf("[launcher.self-update] 已下载新版本 %s 并调度替换，准备重启", result.TargetVersion)
	s.requestShutdownAfter(300 * time.Millisecond)
	return true
}

func (s *service) restoreAgentsContext(ctx context.Context) error {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()

	s.mu.Lock()
	normalizeAgentEnabledDefaults(s.agents)
	agents := make([]model.Agent, len(s.agents))
	copy(agents, s.agents)
	forceRespawn := s.forceRespawnOnRestore
	s.mu.Unlock()

	for _, existing := range agents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !existing.Enabled {
			s.markAgentDisabled(existing.AgentID, "")
			continue
		}
		cfg, err := readAgentConfig(s.cfg.RuntimeDir, existing.AgentID)
		if err != nil {
			s.markAgentOperationFailed(existing.AgentID, err, false)
			continue
		}
		if err := s.refreshAndPersistManagedAgentEnv(cfg); err != nil {
			s.markAgentOperationFailed(existing.AgentID, err, false)
			continue
		}
		agent, adopted, err := s.restoreAgent(ctx, *cfg, forceRespawn)
		if err != nil {
			s.markAgentOperationFailed(existing.AgentID, err, false)
			continue
		}
		if adopted {
			agent.Restarts = 0
		}
		agent.Enabled = true
		if _, err := s.commitAgent(existing.AgentID, agent); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.AgentCount = len(s.agents)
	s.forceRespawnOnRestore = false
	s.syncStateLastErrorLocked()
	if err := saveAgents(s.cfg.RuntimeDir, s.agents); err != nil {
		return err
	}
	return runstate.Save(s.cfg.RuntimeDir, s.state)
}

func (s *service) Shutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	var stopErr error
	for i := range s.agents {
		if err := s.stopAgentProcessLocked(s.agents[i]); err != nil {
			stopErr = errors.Join(stopErr, err)
			s.agents[i].LastError = err.Error()
			s.agents[i].UpdatedAt = now
			s.agents[i].Status = "failed"
			continue
		}
		s.agents[i].PID = 0
		s.agents[i].UpdatedAt = now
		s.agents[i].LastError = ""
		if strings.EqualFold(s.agents[i].Status, "running") {
			s.agents[i].Status = "stopped"
		}
	}
	s.syncStateLastErrorLocked()
	if err := saveAgents(s.cfg.RuntimeDir, s.agents); err != nil {
		if stopErr != nil {
			return errors.Join(stopErr, err)
		}
		return err
	}
	if err := runstate.Save(s.cfg.RuntimeDir, s.state); err != nil {
		if stopErr != nil {
			return errors.Join(stopErr, err)
		}
		return err
	}
	return stopErr
}

func (s *service) State() model.DeviceView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agents := make([]model.Agent, len(s.agents))
	copy(agents, s.agents)
	autostartInfo := convertAutostartInfo(autostart.Status())
	return model.DeviceView{
		Status:           s.state.Status,
		CurrentVersion:   s.state.CurrentVersion,
		TargetVersion:    s.state.TargetVersion,
		PreviousVersion:  s.state.PreviousVersion,
		LastError:        s.state.LastError,
		AgentCount:       len(s.agents),
		UpgradeLocked:    s.state.UpgradeLocked,
		UpgradeStage:     s.state.UpgradeStage,
		UpgradeProgress:  s.state.UpgradeProgress,
		UpgradeMessage:   s.state.UpgradeMessage,
		UpgradeStartedAt: s.state.UpgradeStartedAt,
		UpgradeUpdatedAt: s.state.UpgradeUpdatedAt,
		UpgradeLogs:      convertUpgradeLogs(s.state.UpgradeLogs),
		LauncherVersion:  launcherVersion.Value,
		Autostart:        &autostartInfo,
		Agents:           agents,
	}
}

func (s *service) AutostartStatus() model.LauncherAutostartInfo {
	return convertAutostartInfo(autostart.Status())
}

func (s *service) RefreshAutostartStatus() model.LauncherAutostartInfo {
	return convertAutostartInfo(autostart.Refresh())
}

func (s *service) EnableAutostart(input model.LauncherAutostartInput) (model.LauncherAutostartInfo, error) {
	executable, err := s.canonicalLauncherExecutable()
	if err != nil {
		return model.LauncherAutostartInfo{}, err
	}
	info, err := autostart.Enable(autostart.Options{
		Executable: executable,
		Args:       []string{"--background"},
		Env:        s.autostartEnv(),
		DryRun:     input.DryRun,
		KeepAlive:  true,
	})
	return convertAutostartInfo(info), err
}

func (s *service) repairUpdaterAutostart() {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("LAUNCHER_SKIP_AUTOSTART_REPAIR")), "1") {
		return
	}
	info := autostart.Status()
	if !info.Enabled {
		return
	}
	if autostart.CommandUsesTerminalCompat(info.Command) {
		return
	}
	executable, err := s.canonicalLauncherExecutable()
	if err != nil || autostart.IsUpdaterExecutablePath(executable) {
		return
	}
	if !autostart.CommandUsesUpdaterExecutable(info.Command) && sameCommandExecutable(info.Command, executable) {
		return
	}
	_, _ = autostart.Enable(autostart.Options{
		Executable: executable,
		Args:       []string{"--background"},
		Env:        s.autostartEnv(),
		KeepAlive:  true,
	})
}

func (s *service) canonicalLauncherExecutable() (string, error) {
	if executable, ok := macapp.PreferredExecutablePath(); ok {
		return executable, nil
	}
	target := filepath.Join(s.cfg.RuntimeDir, "bin", binarymeta.GUILauncherBinaryName())
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		return target, nil
	}
	current, err := os.Executable()
	if err != nil {
		return "", err
	}
	if err := copyFileIfChanged(current, target); err != nil {
		return "", err
	}
	return target, nil
}

func sameCommandExecutable(command string, executable string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	var first string
	if strings.HasPrefix(command, "\"") {
		end := strings.Index(command[1:], "\"")
		if end >= 0 {
			first = command[1 : end+1]
		}
	} else if idx := strings.IndexAny(command, " \t"); idx >= 0 {
		first = command[:idx]
	} else {
		first = command
	}
	first = strings.TrimSpace(first)
	if first == "" {
		return false
	}
	left, leftErr := filepath.Abs(first)
	right, rightErr := filepath.Abs(executable)
	return leftErr == nil && rightErr == nil && left == right
}

func copyFileIfChanged(src string, dst string) error {
	srcData, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if dstData, err := os.ReadFile(dst); err == nil && bytes.Equal(srcData, dstData) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, srcData, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(dst, 0o755)
}

func (s *service) autostartEnv() map[string]string {
	env := map[string]string{
		"LAUNCHER_RUNTIME_DIR": s.cfg.RuntimeDir,
		"LAUNCHER_LISTEN_ADDR": s.cfg.ListenAddr,
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		env["HOME"] = home
	}
	if pathValue := strings.TrimSpace(os.Getenv("PATH")); pathValue != "" {
		env["PATH"] = pathValue
	} else {
		env["PATH"] = "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	}
	return env
}

func (s *service) DisableAutostart() (model.LauncherAutostartInfo, error) {
	info, err := autostart.Disable()
	return convertAutostartInfo(info), err
}

func (s *service) SelfUpdate(input model.LauncherSelfUpdateInput) (model.LauncherSelfUpdateResult, error) {
	if !s.updateMu.TryLock() {
		return model.LauncherSelfUpdateResult{}, selfupdate.ErrUpdateInProgress
	}
	defer s.updateMu.Unlock()
	inProgress, err := selfupdate.UpdateInProgress(s.cfg.RuntimeDir)
	if err != nil {
		return model.LauncherSelfUpdateResult{}, err
	}
	if inProgress {
		return model.LauncherSelfUpdateResult{}, selfupdate.ErrUpdateInProgress
	}
	now := time.Now().UTC()
	s.mu.Lock()
	s.state.UpgradeLocked = true
	s.state.UpgradeStage = upgradeStageChecking
	s.state.UpgradeProgress = 3
	s.state.UpgradeMessage = "正在检查 launcher 版本"
	s.state.UpgradeStartedAt = now
	s.state.UpgradeUpdatedAt = now
	s.state.UpgradeLogs = appendUpgradeLog(nil, s.state.UpgradeStage, s.state.UpgradeMessage)
	_ = runstate.Save(s.cfg.RuntimeDir, s.state)
	s.mu.Unlock()

	timeout := s.cfg.SelfUpdate.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	updateCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := selfupdate.Update(updateCtx, selfupdate.Options{
		CurrentVersion: launcherVersion.Value,
		RuntimeDir:     s.cfg.RuntimeDir,
		BaseURL:        input.BaseURL,
		TargetVersion:  input.TargetVersion,
		HealthURL:      LauncherHealthURL(s.cfg.ListenAddr),
		Apply:          input.Apply,
		Restart:        input.Restart,
		RestartArgs:    os.Args[1:],
		DryRun:         input.DryRun,
		Progress: func(received int64, total int64) {
			progress := 12
			message := "正在下载 launcher 更新包"
			if total > 0 {
				ratio := float64(received) / float64(total)
				progress = 12 + int(ratio*58)
				if progress > 70 {
					progress = 70
				}
				message = fmt.Sprintf("正在下载 launcher 更新包 %.1f%%", ratio*100)
			}
			s.updateUpgradeProgress(upgradeStageDownloading, message, progress)
		},
	})
	if err != nil {
		s.markUpgradeFailed("", err)
		return model.LauncherSelfUpdateResult{}, err
	}
	if !result.UpdateAvailable {
		s.clearLauncherSelfUpdateProgress("launcher 已是最新版本")
		return convertSelfUpdateResult(result), nil
	}
	if result.Downloaded {
		s.updateUpgradeProgress(upgradeStageDownloading, "launcher 更新包下载完成", 75)
	}
	if result.Applied {
		if err := selfupdate.MarkRestartPending(s.cfg.RuntimeDir); err != nil {
			s.markUpgradeFailed("", err)
			return model.LauncherSelfUpdateResult{}, err
		}
		s.updateUpgradeProgress(upgradeStageSwitching, "launcher 自更新已调度，正在重启码控", 90)
		s.requestShutdownAfter(300 * time.Millisecond)
	} else {
		s.clearLauncherSelfUpdateProgress("launcher 新版本已下载")
	}
	return convertSelfUpdateResult(result), nil
}

func (s *service) startSelfUpdateStatusMonitor(ctx context.Context) {
	status, ok := selfupdate.ReadApplyStatus(s.cfg.RuntimeDir)
	if !ok || !selfupdate.ApplyStatusAppliesToVersion(status, launcherVersion.Value) || !strings.EqualFold(strings.TrimSpace(status.State), "applying") {
		return
	}
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.NewTimer(time.Minute)
		defer timeout.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timeout.C:
				return
			case <-ticker.C:
				latest, exists := selfupdate.ReadApplyStatus(s.cfg.RuntimeDir)
				if !exists || !selfupdate.ApplyStatusAppliesToVersion(latest, launcherVersion.Value) {
					continue
				}
				state := strings.ToLower(strings.TrimSpace(latest.State))
				if state != "completed" && state != "failed" {
					continue
				}
				s.mu.Lock()
				applySelfUpdateStatusRecord(&s.state, latest)
				_ = runstate.Save(s.cfg.RuntimeDir, s.state)
				s.mu.Unlock()
				return
			}
		}
	}()
}

func LauncherHealthURL(listenAddr string) string {
	listenAddr = strings.TrimSpace(listenAddr)
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil || strings.TrimSpace(port) == "" {
		return "http://127.0.0.1:4071/state"
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/state"
}

func (s *service) clearLauncherSelfUpdateProgress(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.UpgradeLocked = false
	s.state.UpgradeStage = upgradeStageCompleted
	s.state.UpgradeProgress = 100
	s.state.UpgradeMessage = strings.TrimSpace(message)
	s.state.UpgradeUpdatedAt = time.Now().UTC()
	s.state.UpgradeLogs = appendUpgradeLog(s.state.UpgradeLogs, s.state.UpgradeStage, s.state.UpgradeMessage)
	s.syncStateLastErrorLocked()
	_ = runstate.Save(s.cfg.RuntimeDir, s.state)
}

func (s *service) requestShutdownAfter(delay time.Duration) {
	go func() {
		if delay > 0 {
			time.Sleep(delay)
		}
		s.mu.RLock()
		cancel := s.shutdown
		s.mu.RUnlock()
		if cancel != nil {
			cancel()
		}
	}()
}

func convertAutostartInfo(info autostart.Info) model.LauncherAutostartInfo {
	return model.LauncherAutostartInfo{
		Enabled: info.Enabled,
		Method:  info.Method,
		Path:    info.Path,
		Command: info.Command,
		Error:   info.Error,
	}
}

func convertSelfUpdateResult(result selfupdate.Result) model.LauncherSelfUpdateResult {
	out := model.LauncherSelfUpdateResult{
		CurrentVersion:  result.CurrentVersion,
		LatestVersion:   result.LatestVersion,
		TargetVersion:   result.TargetVersion,
		Channel:         result.Channel,
		Platform:        result.Platform,
		UpdateAvailable: result.UpdateAvailable,
		Downloaded:      result.Downloaded,
		Applied:         result.Applied,
		DryRun:          result.DryRun,
		StagedPath:      result.StagedPath,
		UpdaterPath:     result.UpdaterPath,
		Message:         result.Message,
	}
	if result.Asset != nil {
		out.Asset = &model.LauncherSelfUpdateAsset{
			Platform:  result.Asset.Platform,
			Filename:  result.Asset.Filename,
			URL:       result.Asset.URL,
			SHA256:    result.Asset.SHA256,
			Available: result.Asset.Available,
		}
	}
	return out
}

func (s *service) AIConfig() (*model.DeviceAIConfigInfo, error) {
	cfg, err := aiconfig.Load()
	if err != nil {
		return nil, err
	}
	return s.enrichAIConfigFromRuntime(cfg), nil
}

func (s *service) ListAIModels(input model.DeviceAIConfigInput) (*model.DeviceAIConfigInfo, error) {
	cfg, err := aiconfig.LoadWithModels(input)
	if err != nil {
		return nil, err
	}
	return s.enrichAIConfigFromRuntime(cfg), nil
}

func (s *service) SaveAIConfig(input model.DeviceAIConfigInfo) (*model.DeviceAIConfigInfo, error) {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	cfg, err := aiconfig.Save(input)
	if err != nil {
		return nil, err
	}
	if err := s.reloadAgentsWithOpsLocked(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (s *service) SaveAIConfigText(text string) (*model.DeviceAIConfigInfo, error) {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	cfg, err := aiconfig.SaveText(text)
	if err != nil {
		return nil, err
	}
	if err := s.reloadAgentsWithOpsLocked(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (s *service) ClearAIProvider(id string) (*model.DeviceAIConfigInfo, error) {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	cfg, err := aiconfig.ClearProvider(id)
	if err != nil {
		return nil, err
	}
	if err := s.reloadAgentsWithOpsLocked(); err != nil {
		return nil, err
	}
	return s.enrichAIConfigFromRuntime(cfg), nil
}

func (s *service) MCPConfig() (*model.DeviceMCPConfigInfo, error) {
	return mcpconfig.Load()
}

func (s *service) MCPStatus(agentID string) (*model.MCPStatusInfo, error) {
	s.mu.RLock()
	agent, ok := s.agentByIDLocked(agentID)
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("未找到 Agent")
	}
	if agent.Port <= 0 {
		return nil, fmt.Errorf("Agent 端口不可用")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	servers, err := fetchMCPStatus(ctx, agent.Port)
	if err != nil {
		return nil, err
	}
	tools, _ := fetchMCPTools(ctx, agent.Port)
	for i := range servers {
		servers[i].Error = friendlyMCPRuntimeError(servers[i].Error)
		servers[i].Tools = tools[servers[i].Name]
	}
	return &model.MCPStatusInfo{AgentID: agent.AgentID, Port: agent.Port, Servers: servers}, nil
}

func (s *service) AgentMCPSelection(agentID string) (model.DeviceAgentMCPSelectionInfo, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return model.DeviceAgentMCPSelectionInfo{}, proc.ErrAgentNotFound
	}
	if !s.hasAgent(agentID) {
		return model.DeviceAgentMCPSelectionInfo{}, proc.ErrAgentNotFound
	}
	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		return model.DeviceAgentMCPSelectionInfo{}, err
	}
	return s.agentMCPSelectionFromConfig(*cfg)
}

func (s *service) AgentSkillSelection(input model.DeviceAgentSkillSelectionInput) (model.DeviceAgentSkillSelectionInfo, error) {
	agentID := strings.TrimSpace(input.AgentID)
	if agentID == "" || !s.hasAgent(agentID) {
		return model.DeviceAgentSkillSelectionInfo{}, proc.ErrAgentNotFound
	}
	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		return model.DeviceAgentSkillSelectionInfo{}, err
	}
	item, _ := semanticAgentDefinitionFromConfig(*cfg)
	available := mergeSkillDefinitions(
		semanticAgentSkillDefinitions(item.skills),
		mergeSkillDefinitions(loadLocalSkillDefinitions(s.cfg.RuntimeDir), input.Skills),
	)
	return skillSelectionFromConfig(*cfg, available), nil
}

func (s *service) SaveAgentSkillSelection(input model.DeviceAgentSkillSelectionInput) (model.DeviceAgentSkillSelectionInfo, error) {
	agentID := strings.TrimSpace(input.AgentID)
	if agentID == "" {
		return model.DeviceAgentSkillSelectionInfo{}, proc.ErrAgentNotFound
	}
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if !s.hasAgent(agentID) {
		return model.DeviceAgentSkillSelectionInfo{}, proc.ErrAgentNotFound
	}
	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		return model.DeviceAgentSkillSelectionInfo{}, err
	}
	item, _ := semanticAgentDefinitionFromConfig(*cfg)
	defaultIDs := skillNames(item.skills)
	available := map[string]model.SemanticAgentSkill{}
	availableDefinitions := mergeSkillDefinitions(
		semanticAgentSkillDefinitions(item.skills),
		mergeSkillDefinitions(loadLocalSkillDefinitions(s.cfg.RuntimeDir), input.Skills),
	)
	for _, skill := range availableDefinitions {
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			continue
		}
		skill.Name = name
		skill.PackageFiles = cleanSkillPackageFiles(skill.PackageFiles)
		available[name] = skill
	}
	for _, skill := range cfg.ExtraSkillDefinitions {
		name := strings.TrimSpace(skill.Name)
		if name != "" {
			available[name] = skill
		}
	}
	selected := normalizeSkillNames(input.ExtraSkillIDs)
	selected = removeSkillNames(selected, defaultIDs)
	definitions := make([]model.SemanticAgentSkill, 0, len(selected))
	for _, name := range selected {
		skill, ok := available[name]
		if !ok {
			return model.DeviceAgentSkillSelectionInfo{}, fmt.Errorf("Skill 不存在或尚未加载：%s", name)
		}
		definitions = append(definitions, skill)
	}
	cfg.ExtraSkillIDs = selected
	cfg.ExtraSkillDefinitions = definitions
	cfg.Env = s.agentEnv(*cfg)
	if err := writeAgentConfig(filepath.Join(s.cfg.RuntimeDir, "agents", agentID), *cfg); err != nil {
		return model.DeviceAgentSkillSelectionInfo{}, err
	}
	return skillSelectionFromConfig(*cfg, availableDefinitions), nil
}

func (s *service) ImportAgentSkill(input model.DeviceAgentSkillImportInput) (model.DeviceAgentSkillSelectionInfo, error) {
	agentID := strings.TrimSpace(input.AgentID)
	if agentID == "" {
		return model.DeviceAgentSkillSelectionInfo{}, proc.ErrAgentNotFound
	}
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if !s.hasAgent(agentID) {
		return model.DeviceAgentSkillSelectionInfo{}, proc.ErrAgentNotFound
	}
	skill, err := normalizeImportedSkill(input.Skill)
	if err != nil {
		return model.DeviceAgentSkillSelectionInfo{}, err
	}
	if err := saveLocalSkill(s.cfg.RuntimeDir, skill, input.Overwrite); err != nil {
		return model.DeviceAgentSkillSelectionInfo{}, err
	}
	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		return model.DeviceAgentSkillSelectionInfo{}, err
	}
	item, _ := semanticAgentDefinitionFromConfig(*cfg)
	selected := append([]string{}, cfg.ExtraSkillIDs...)
	selected = appendUniqueSkillNames(selected, skill.Name)
	selected = removeSkillNames(normalizeSkillNames(selected), skillNames(item.skills))
	available := mergeSkillDefinitions(
		semanticAgentSkillDefinitions(item.skills),
		loadLocalSkillDefinitions(s.cfg.RuntimeDir),
	)
	byName := make(map[string]model.SemanticAgentSkill, len(available))
	for _, availableSkill := range available {
		byName[availableSkill.Name] = availableSkill
	}
	definitions := make([]model.SemanticAgentSkill, 0, len(selected))
	for _, name := range selected {
		definition, ok := byName[name]
		if !ok {
			return model.DeviceAgentSkillSelectionInfo{}, fmt.Errorf("Skill 不存在或尚未加载：%s", name)
		}
		definitions = append(definitions, definition)
	}
	cfg.ExtraSkillIDs = selected
	cfg.ExtraSkillDefinitions = definitions
	cfg.Env = s.agentEnv(*cfg)
	if err := writeAgentConfig(filepath.Join(s.cfg.RuntimeDir, "agents", agentID), *cfg); err != nil {
		return model.DeviceAgentSkillSelectionInfo{}, err
	}
	return skillSelectionFromConfig(*cfg, available), nil
}

func (s *service) GlobalSkillConfig() (*model.DeviceSkillConfigInfo, error) {
	settings, err := loadGlobalSkillConfig(s.cfg.RuntimeDir)
	if err != nil {
		return nil, err
	}
	entries := make([]model.DeviceSkillConfigEntry, 0)
	for _, skill := range loadLocalSkillDefinitions(s.cfg.RuntimeDir) {
		entries = append(entries, model.DeviceSkillConfigEntry{
			Name:         skill.Name,
			Description:  skill.Description,
			Content:      skill.Content,
			PackageFiles: skill.PackageFiles,
			Enabled:      settings.Skills[skill.Name],
		})
	}
	return &model.DeviceSkillConfigInfo{
		MachineID: s.cfg.Relay.MachineID,
		Enabled:   settings.Enabled,
		Skills:    entries,
	}, nil
}

func (s *service) SaveGlobalSkillConfig(input model.DeviceSkillConfigInfo) (*model.DeviceSkillConfigInfo, error) {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if err := s.saveGlobalSkillConfigLocked(input); err != nil {
		return nil, err
	}
	if err := s.reloadAgentsWithOpsLocked(); err != nil {
		return nil, err
	}
	return s.GlobalSkillConfig()
}

func (s *service) ImportGlobalSkill(input model.DeviceSkillConfigInput) (*model.DeviceSkillConfigInfo, error) {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	skill, err := normalizeImportedSkill(input.Skill)
	if err != nil {
		return nil, err
	}
	if err := saveLocalSkill(s.cfg.RuntimeDir, skill, true); err != nil {
		return nil, err
	}
	settings, err := loadGlobalSkillConfig(s.cfg.RuntimeDir)
	if err != nil {
		return nil, err
	}
	settings.Enabled = true
	settings.Skills[skill.Name] = true
	if err := writeGlobalSkillConfig(s.cfg.RuntimeDir, settings); err != nil {
		return nil, err
	}
	if err := s.reloadAgentsWithOpsLocked(); err != nil {
		return nil, err
	}
	return s.GlobalSkillConfig()
}

type globalSkillConfigFile struct {
	Enabled bool            `json:"enabled"`
	Skills  map[string]bool `json:"skills,omitempty"`
}

func loadGlobalSkillConfig(runtimeDir string) (globalSkillConfigFile, error) {
	result := globalSkillConfigFile{Skills: map[string]bool{}}
	data, err := os.ReadFile(filepath.Join(runtimeDir, "skills", "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	if result.Skills == nil {
		result.Skills = map[string]bool{}
	}
	return result, nil
}

func writeGlobalSkillConfig(runtimeDir string, settings globalSkillConfigFile) error {
	if settings.Skills == nil {
		settings.Skills = map[string]bool{}
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(runtimeDir, "skills"), 0o755); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(
		filepath.Join(runtimeDir, "skills", "config.json"),
		append(data, '\n'),
		0o600,
	)
}

func (s *service) saveGlobalSkillConfigLocked(input model.DeviceSkillConfigInfo) error {
	settings := globalSkillConfigFile{Enabled: input.Enabled, Skills: map[string]bool{}}
	for _, entry := range input.Skills {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		skill := model.SemanticAgentSkill{
			Name:         name,
			Description:  entry.Description,
			Content:      entry.Content,
			PackageFiles: entry.PackageFiles,
		}
		if strings.TrimSpace(skill.Content) != "" {
			normalized, err := normalizeImportedSkill(skill)
			if err != nil {
				return err
			}
			if err := saveLocalSkill(s.cfg.RuntimeDir, normalized, true); err != nil {
				return err
			}
		}
		settings.Skills[name] = entry.Enabled
	}
	return writeGlobalSkillConfig(s.cfg.RuntimeDir, settings)
}

func mergeSkillDefinitions(left, right []model.SemanticAgentSkill) []model.SemanticAgentSkill {
	out := make([]model.SemanticAgentSkill, 0, len(left)+len(right))
	seen := map[string]bool{}
	for _, skill := range append(append([]model.SemanticAgentSkill{}, left...), right...) {
		name := strings.TrimSpace(skill.Name)
		if name == "" || seen[name] {
			continue
		}
		skill.Name = name
		skill.PackageFiles = cleanSkillPackageFiles(skill.PackageFiles)
		seen[name] = true
		out = append(out, skill)
	}
	return out
}

func normalizeImportedSkill(input model.SemanticAgentSkill) (model.SemanticAgentSkill, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,95}$`).MatchString(input.Name) {
		return model.SemanticAgentSkill{}, errors.New("Skill 名称只能包含字母、数字、点、下划线和连字符")
	}
	input.Content = strings.TrimSpace(input.Content)
	if input.Content == "" {
		return model.SemanticAgentSkill{}, errors.New("SKILL.md 内容不能为空")
	}
	input.PackageFiles = cleanSkillPackageFiles(input.PackageFiles)
	return input, nil
}

func saveLocalSkill(runtimeDir string, skill model.SemanticAgentSkill, overwrite bool) error {
	root := filepath.Join(runtimeDir, "skills", skill.Name)
	if _, err := os.Stat(root); err == nil && !overwrite {
		return fmt.Errorf("设备上已存在 Skill：%s", skill.Name)
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(skill.Content+"\n"), 0o644); err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]string{"name": skill.Name, "description": skill.Description})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "skill.json"), append(metadata, '\n'), 0o644); err != nil {
		return err
	}
	for _, file := range skill.PackageFiles {
		rel := cleanSkillPackagePath(file.Path)
		if rel == "" {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(rel))
		if !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if file.Executable {
			mode = 0o755
		}
		if err := os.WriteFile(target, []byte(file.Content), mode); err != nil {
			return err
		}
	}
	return nil
}

func loadLocalSkillDefinitions(runtimeDir string) []model.SemanticAgentSkill {
	entries, err := os.ReadDir(filepath.Join(runtimeDir, "skills"))
	if err != nil {
		return nil
	}
	result := make([]model.SemanticAgentSkill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		root := filepath.Join(runtimeDir, "skills", entry.Name())
		content, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
		if err != nil {
			continue
		}
		name := entry.Name()
		description := ""
		if metadata, readErr := os.ReadFile(filepath.Join(root, "skill.json")); readErr == nil {
			var value struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			}
			if json.Unmarshal(metadata, &value) == nil {
				if strings.TrimSpace(value.Name) != "" {
					name = strings.TrimSpace(value.Name)
				}
				description = strings.TrimSpace(value.Description)
			}
		}
		skill := model.SemanticAgentSkill{Name: name, Description: description, Content: string(content)}
		_ = filepath.WalkDir(root, func(path string, item os.DirEntry, walkErr error) error {
			if walkErr != nil || item.IsDir() || item.Name() == "SKILL.md" || item.Name() == "skill.json" {
				return nil
			}
			if item.Type()&os.ModeSymlink != 0 {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}
			skill.PackageFiles = append(skill.PackageFiles, model.SkillPackageFile{Path: filepath.ToSlash(rel), Content: string(data)})
			return nil
		})
		result = append(result, skill)
	}
	return result
}

func skillSelectionFromConfig(cfg model.AgentConfig, available []model.SemanticAgentSkill) model.DeviceAgentSkillSelectionInfo {
	item, _ := semanticAgentDefinitionFromConfig(cfg)
	defaults := skillNames(item.skills)
	extra := normalizeSkillNames(cfg.ExtraSkillIDs)
	extra = removeSkillNames(extra, defaults)
	effective := append(append([]string{}, defaults...), extra...)
	return model.DeviceAgentSkillSelectionInfo{
		AgentID:         cfg.AgentID,
		DefaultSkills:   defaults,
		ExtraSkills:     extra,
		EffectiveSkills: effective,
		AvailableSkills: available,
	}
}

func skillNames(input []semanticAgentSkill) []string {
	result := make([]string, 0, len(input))
	for _, skill := range input {
		result = appendUniqueSkillNames(result, skill.name)
	}
	return result
}

func normalizeSkillNames(input []string) []string {
	result := make([]string, 0, len(input))
	for _, name := range input {
		result = appendUniqueSkillNames(result, name)
	}
	sort.Strings(result)
	return result
}

func appendUniqueSkillNames(input []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return input
	}
	for _, current := range input {
		if current == value {
			return input
		}
	}
	return append(input, value)
}

func removeSkillNames(input, removed []string) []string {
	set := map[string]bool{}
	for _, name := range removed {
		set[name] = true
	}
	result := make([]string, 0, len(input))
	for _, name := range input {
		if !set[name] {
			result = append(result, name)
		}
	}
	return result
}

func (s *service) SaveAgentMCPSelection(input model.DeviceAgentMCPSelectionInput) (model.DeviceAgentMCPSelectionInfo, error) {
	agentID := strings.TrimSpace(input.AgentID)
	if agentID == "" {
		return model.DeviceAgentMCPSelectionInfo{}, proc.ErrAgentNotFound
	}
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if !s.hasAgent(agentID) {
		return model.DeviceAgentMCPSelectionInfo{}, proc.ErrAgentNotFound
	}
	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		return model.DeviceAgentMCPSelectionInfo{}, err
	}
	if normalizedMCPMode(input.Mode) == "custom" {
		info, err := mcpconfig.Load()
		if err != nil {
			return model.DeviceAgentMCPSelectionInfo{}, err
		}
		cfg.MCPMode = "custom"
		cfg.MCPServers = selectedAvailableMCPServers(input.Servers, info.Servers)
	} else {
		cfg.MCPMode = ""
		cfg.MCPServers = nil
	}
	cfg.Env = s.agentEnv(*cfg)
	if err := writeAgentConfig(filepath.Join(s.cfg.RuntimeDir, "agents", agentID), *cfg); err != nil {
		return model.DeviceAgentMCPSelectionInfo{}, err
	}
	return s.agentMCPSelectionFromConfig(*cfg)
}

func (s *service) agentMCPSelectionFromConfig(cfg model.AgentConfig) (model.DeviceAgentMCPSelectionInfo, error) {
	info, err := mcpconfig.Load()
	if err != nil {
		return model.DeviceAgentMCPSelectionInfo{}, err
	}
	mode := normalizedMCPMode(cfg.MCPMode)
	selected := normalizeMCPServerNames(cfg.MCPServers)
	if mode != "custom" {
		selected = selectedDefaultMCPServers(info.Servers)
	} else {
		selected = selectedAvailableMCPServers(selected, info.Servers)
	}
	return model.DeviceAgentMCPSelectionInfo{
		AgentID:          cfg.AgentID,
		Mode:             mode,
		SelectedServers:  selected,
		AvailableServers: info.Servers,
	}, nil
}

func normalizedMCPMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "custom") {
		return "custom"
	}
	return "inherit"
}

func normalizeMCPServerNames(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func selectedAvailableMCPServers(items []string, servers []model.DeviceMCPServerInfo) []string {
	available := map[string]bool{}
	for _, server := range servers {
		available[server.Name] = true
	}
	selected := normalizeMCPServerNames(items)
	out := make([]string, 0, len(selected))
	for _, name := range selected {
		if available[name] {
			out = append(out, name)
		}
	}
	return out
}

func selectedDefaultMCPServers(servers []model.DeviceMCPServerInfo) []string {
	out := make([]string, 0, len(servers))
	for _, server := range servers {
		if server.Enabled {
			out = append(out, server.Name)
		}
	}
	sort.Strings(out)
	return out
}

func (s *service) SaveMCPConfig(input model.DeviceMCPConfigInfo) (*model.DeviceMCPConfigInfo, error) {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	names := make([]string, 0, len(input.Servers))
	for _, srv := range input.Servers {
		names = append(names, srv.Name)
	}
	log.Printf("[launcher][mcp] SaveMCPConfig 来源=前端保存 提交servers=%v", names)
	cfg, err := mcpconfig.Save(input)
	if err != nil {
		return nil, err
	}
	if err := s.reloadAgentsWithOpsLocked(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (s *service) RemoveMCPConfig(name string) (*model.DeviceMCPConfigInfo, error) {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	log.Printf("[launcher][mcp] RemoveMCPConfig 来源=前端删除 name=%s", name)
	cfg, err := mcpconfig.Remove(name)
	if err != nil {
		return nil, err
	}
	if err := s.reloadAgentsWithOpsLocked(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (s *service) Versions() ([]string, error) {
	return release.Versions(s.cfg.RuntimeDir)
}

func (s *service) StartUpgrade(input model.UpgradeInput) error {
	input.TargetVersion = strings.TrimSpace(input.TargetVersion)
	if input.TargetVersion == "" {
		return errors.New("目标版本不能为空")
	}
	s.mu.Lock()
	if s.state.UpgradeLocked || strings.EqualFold(s.state.Status, "upgrading") {
		s.mu.Unlock()
		return errors.New("已有升级任务正在执行")
	}
	now := time.Now().UTC()
	s.state.Status = "upgrading"
	s.state.TargetVersion = input.TargetVersion
	s.state.UpgradeLocked = true
	s.state.UpgradeStage = upgradeStageChecking
	s.state.UpgradeProgress = 3
	s.state.UpgradeMessage = "升级任务已启动"
	s.state.UpgradeStartedAt = now
	s.state.UpgradeUpdatedAt = now
	s.state.UpgradeLogs = appendUpgradeLog(nil, s.state.UpgradeStage, s.state.UpgradeMessage)
	err := runstate.Save(s.cfg.RuntimeDir, s.state)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	go func() {
		if err := s.Upgrade(input); err != nil {
			log.Printf("[launcher.upgrade] 升级失败: %v", err)
			s.markUpgradeFailed("", err)
		}
	}()
	return nil
}

func (s *service) Upgrade(input model.UpgradeInput) error {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	input.TargetVersion = strings.TrimSpace(input.TargetVersion)
	if input.TargetVersion == "" {
		return errors.New("目标版本不能为空")
	}
	s.updateUpgradeProgress(upgradeStageChecking, "正在检查当前版本", 8)
	current, err := release.Current(s.cfg.RuntimeDir)
	if err != nil {
		s.markUpgradeFailed("", err)
		return err
	}
	if current == input.TargetVersion {
		s.clearCompletedUpgradeTarget(current)
		return nil
	}
	if _, err := release.ManifestFor(s.cfg.RuntimeDir, input.TargetVersion); err != nil {
		s.updateUpgradeProgress(upgradeStageDownloading, "正在下载 opencode", 12)
		if err := download.FetchWithProgress(s.cfg.RuntimeDir, input.TargetVersion, func(received int64, total int64) {
			progress := 12
			if total > 0 {
				progress = 12 + int(float64(received)/float64(total)*43)
				if progress > 55 {
					progress = 55
				}
			}
			message := "正在下载 opencode"
			if total > 0 {
				message = fmt.Sprintf("正在下载 opencode %.1f%%", float64(received)*100/float64(total))
			}
			s.updateUpgradeProgress(upgradeStageDownloading, message, progress)
		}); err != nil {
			s.markUpgradeFailed(current, err)
			return err
		}
		s.updateUpgradeProgress(upgradeStageDownloading, "opencode 下载完成", 55)
	}
	if err := s.beginUpgradeState(current, input.TargetVersion, upgradeStageStopping, "正在停止 agent", 60); err != nil {
		return err
	}
	stopErr := s.stopAllAgentProcesses("stopping")
	s.updateUpgradeProgress(upgradeStageSwitching, "正在切换 opencode 版本", 78)
	if err := release.Switch(s.cfg.RuntimeDir, input.TargetVersion); err != nil {
		s.markUpgradeFailed(current, err)
		return err
	}
	s.updateUpgradeProgress(upgradeStageStarting, "正在启动 agent", 90)
	startErr := s.startEnabledAgentsWithOpsLocked("starting")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.CurrentVersion = input.TargetVersion
	s.state.TargetVersion = ""
	s.state.Status = "running"
	s.state.UpgradeLocked = false
	s.state.UpgradeStage = upgradeStageCompleted
	s.state.UpgradeProgress = 100
	s.state.UpgradeMessage = "opencode 升级完成"
	s.state.UpgradeUpdatedAt = time.Now().UTC()
	s.state.UpgradeLogs = appendUpgradeLog(s.state.UpgradeLogs, s.state.UpgradeStage, s.state.UpgradeMessage)
	s.syncStateLastErrorLocked()
	if s.state.LastError == "" {
		if stopErr != nil {
			s.state.LastError = stopErr.Error()
		} else if startErr != nil {
			s.state.LastError = startErr.Error()
		}
	}
	if err := saveAgents(s.cfg.RuntimeDir, s.agents); err != nil {
		return err
	}
	return runstate.Save(s.cfg.RuntimeDir, s.state)
}

func (s *service) findAgentIndexLocked(agentID string) int {
	for i, agent := range s.agents {
		if agent.AgentID == agentID {
			return i
		}
	}
	return -1
}

func (s *service) agentByIDLocked(agentID string) (model.Agent, bool) {
	index := s.findAgentIndexLocked(agentID)
	if index < 0 {
		return model.Agent{}, false
	}
	return s.agents[index], true
}

func (s *service) hasAgent(agentID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.findAgentIndexLocked(agentID) >= 0
}

func (s *service) markAgentDisabledLocked(index int, lastError string) {
	if index < 0 || index >= len(s.agents) {
		return
	}
	s.agents[index].Enabled = false
	s.agents[index].PID = 0
	s.agents[index].Status = "disabled"
	s.agents[index].LastError = lastError
	s.agents[index].UpdatedAt = time.Now().UTC()
	delete(s.processes, s.agents[index].AgentID)
}

func (s *service) markAgentDisabled(agentID string, lastError string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.findAgentIndexLocked(agentID)
	if index < 0 {
		return
	}
	s.markAgentDisabledLocked(index, lastError)
}

func (s *service) reloadAgentsWithOpsLocked() error {
	agents := s.Agents()
	for _, agent := range agents {
		if !agent.Enabled {
			if _, err := s.stopAndDisableAgent(agent.AgentID); err != nil {
				return err
			}
			continue
		}
		cfg, err := readAgentConfig(s.cfg.RuntimeDir, agent.AgentID)
		if err != nil {
			return err
		}
		if err := s.refreshAndPersistManagedAgentEnv(cfg); err != nil {
			return err
		}
		if _, err := s.restartAgentWithConfig(agent.AgentID, cfg, "restarting"); err != nil {
			return err
		}
	}
	return nil
}

func (s *service) beginAgentTransition(agentID string, status string) (model.Agent, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.findAgentIndexLocked(agentID)
	if index < 0 {
		return model.Agent{}, 0, proc.ErrAgentNotFound
	}
	previous := s.agents[index]
	trackedPID := s.processes[agentID]
	s.agents[index].PID = 0
	s.agents[index].Status = status
	s.agents[index].UpdatedAt = time.Now().UTC()
	delete(s.processes, agentID)
	return previous, trackedPID, nil
}

func (s *service) restartAgentWithConfig(agentID string, cfg *model.AgentConfig, status string) (model.Agent, error) {
	if err := s.reassignPortIfBlocked(cfg); err != nil {
		s.markAgentOperationFailed(agentID, err, false)
		return model.Agent{}, err
	}
	previous, trackedPID, err := s.beginAgentTransition(agentID, status)
	if err != nil {
		return model.Agent{}, err
	}
	previous.Enabled = true
	if err := s.stopAgentProcess(previous, trackedPID); err != nil {
		s.markAgentOperationFailed(agentID, err, false)
		return model.Agent{}, err
	}
	agent, _, err := s.adoptOrSpawnAgent(context.Background(), *cfg, previous)
	if err != nil {
		s.markAgentOperationFailed(agentID, err, false)
		return model.Agent{}, err
	}
	agent.Enabled = true
	return s.commitAgent(agentID, agent)
}

func (s *service) stopAndDisableAgent(agentID string) (model.Agent, error) {
	previous, trackedPID, err := s.beginAgentTransition(agentID, "stopping")
	if err != nil {
		return model.Agent{}, err
	}
	if err := s.stopAgentProcess(previous, trackedPID); err != nil {
		s.markAgentOperationFailed(agentID, err, false)
		return model.Agent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.findAgentIndexLocked(agentID)
	if index < 0 {
		return model.Agent{}, proc.ErrAgentNotFound
	}
	s.markAgentDisabledLocked(index, "")
	s.syncStateLastErrorLocked()
	if err := saveAgents(s.cfg.RuntimeDir, s.agents); err != nil {
		return model.Agent{}, err
	}
	if err := runstate.Save(s.cfg.RuntimeDir, s.state); err != nil {
		return model.Agent{}, err
	}
	return s.agents[index], nil
}

func (s *service) commitAgent(agentID string, agent model.Agent) (model.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.findAgentIndexLocked(agentID)
	if index < 0 {
		return model.Agent{}, proc.ErrAgentNotFound
	}
	s.agents[index] = agent
	if strings.EqualFold(agent.Status, "running") {
		s.state.Status = "running"
	}
	s.syncStateLastErrorLocked()
	if err := saveAgents(s.cfg.RuntimeDir, s.agents); err != nil {
		return model.Agent{}, err
	}
	if err := runstate.Save(s.cfg.RuntimeDir, s.state); err != nil {
		return model.Agent{}, err
	}
	return s.agents[index], nil
}

func (s *service) markAgentOperationFailed(agentID string, operationErr error, incrementRestarts bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.findAgentIndexLocked(agentID)
	if index < 0 {
		return
	}
	if incrementRestarts {
		s.agents[index].Restarts++
	}
	s.agents[index].Status = "failed"
	s.agents[index].LastError = operationErr.Error()
	s.agents[index].UpdatedAt = time.Now().UTC()
	s.state.LastError = operationErr.Error()
	s.syncStateLastErrorLocked()
	_ = runstate.Save(s.cfg.RuntimeDir, s.state)
	_ = saveAgents(s.cfg.RuntimeDir, s.agents)
}

type agentStopTarget struct {
	agent      model.Agent
	trackedPID int
}

func (s *service) stopAllAgentProcesses(status string) error {
	s.mu.Lock()
	targets := make([]agentStopTarget, 0, len(s.agents))
	now := time.Now().UTC()
	for i := range s.agents {
		agentID := s.agents[i].AgentID
		targets = append(targets, agentStopTarget{
			agent:      s.agents[i],
			trackedPID: s.processes[agentID],
		})
		if status != "" && s.agents[i].Enabled {
			s.agents[i].PID = 0
			s.agents[i].Status = status
			s.agents[i].UpdatedAt = now
		}
		delete(s.processes, agentID)
	}
	s.mu.Unlock()
	var result error
	for _, target := range targets {
		if err := s.stopAgentProcess(target.agent, target.trackedPID); err != nil {
			s.markAgentOperationFailed(target.agent.AgentID, err, false)
			result = errors.Join(result, err)
		}
	}
	return result
}

func (s *service) startEnabledAgentsWithOpsLocked(status string) error {
	agents := s.Agents()
	var result error
	for _, agent := range agents {
		if !agent.Enabled {
			continue
		}
		cfg, err := readAgentConfig(s.cfg.RuntimeDir, agent.AgentID)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if err := s.refreshAndPersistManagedAgentEnv(cfg); err != nil {
			result = errors.Join(result, err)
			continue
		}
		if _, err := s.restartAgentWithConfig(agent.AgentID, cfg, status); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func (s *service) reassignPortIfBlocked(cfg *model.AgentConfig) error {
	if cfg == nil || cfg.Port <= 0 {
		return nil
	}
	if !portAcceptsTCP(cfg.Port) {
		return nil
	}
	if proc.IsHealthy(cfg.Port, 500*time.Millisecond) {
		return nil
	}
	agents := s.Agents()
	newPort, err := nextAvailableRecoveryPort(agents)
	if err != nil {
		return fmt.Errorf("agent %s 端口 %d 被占用，分配恢复端口失败: %w", cfg.AgentID, cfg.Port, err)
	}
	if newPort == cfg.Port {
		return nil
	}
	oldPort := cfg.Port
	cfg.Port = newPort
	cfg.Args = replacePortArg(cfg.Args, newPort)
	agentDir := filepath.Join(s.cfg.RuntimeDir, "agents", cfg.AgentID)
	if err := writeAgentConfig(agentDir, *cfg); err != nil {
		return fmt.Errorf("agent %s 端口 %d 被占用，改用 %d 时保存配置失败: %w", cfg.AgentID, oldPort, newPort, err)
	}
	s.mu.Lock()
	if index := s.findAgentIndexLocked(cfg.AgentID); index >= 0 {
		s.agents[index].Port = newPort
		s.agents[index].LastError = fmt.Sprintf("原端口 %d 被占用，已自动切换到端口 %d", oldPort, newPort)
		s.agents[index].UpdatedAt = time.Now().UTC()
		_ = saveAgents(s.cfg.RuntimeDir, s.agents)
	}
	s.mu.Unlock()
	return nil
}

func (s *service) markLauncherOperationFailed(operationErr error) {
	if operationErr == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.LastError = operationErr.Error()
	_ = runstate.Save(s.cfg.RuntimeDir, s.state)
}

func (s *service) clearLauncherOperationError(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.LastError == message {
		s.state.LastError = ""
		_ = runstate.Save(s.cfg.RuntimeDir, s.state)
	}
}

func (s *service) beginUpgradeState(currentVersion string, targetVersion string, stage string, message string, progress int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.state.PreviousVersion = strings.TrimSpace(currentVersion)
	s.state.TargetVersion = strings.TrimSpace(targetVersion)
	s.state.Status = "upgrading"
	s.state.UpgradeLocked = true
	if s.state.UpgradeStartedAt.IsZero() {
		s.state.UpgradeStartedAt = now
	}
	s.state.UpgradeStage = stage
	s.state.UpgradeProgress = clampProgress(progress)
	s.state.UpgradeMessage = message
	s.state.UpgradeUpdatedAt = now
	s.state.UpgradeLogs = appendUpgradeLog(s.state.UpgradeLogs, stage, message)
	return runstate.Save(s.cfg.RuntimeDir, s.state)
}

func (s *service) updateUpgradeProgress(stage string, message string, progress int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if strings.TrimSpace(stage) != "" {
		s.state.UpgradeStage = stage
	}
	if strings.TrimSpace(message) != "" {
		s.state.UpgradeMessage = message
	}
	s.state.UpgradeProgress = clampProgress(progress)
	s.state.UpgradeUpdatedAt = now
	if s.state.UpgradeStartedAt.IsZero() {
		s.state.UpgradeStartedAt = now
	}
	s.state.UpgradeLogs = appendUpgradeLog(s.state.UpgradeLogs, s.state.UpgradeStage, s.state.UpgradeMessage)
	_ = runstate.Save(s.cfg.RuntimeDir, s.state)
}

func clampProgress(progress int) int {
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

func appendUpgradeLog(logs []runstate.UpgradeLogEntry, stage string, message string) []runstate.UpgradeLogEntry {
	message = strings.TrimSpace(message)
	if message == "" {
		return logs
	}
	entry := runstate.UpgradeLogEntry{
		Time:    time.Now().UTC(),
		Stage:   strings.TrimSpace(stage),
		Message: message,
	}
	if len(logs) > 0 {
		last := logs[len(logs)-1]
		if last.Stage == entry.Stage && last.Message == entry.Message {
			logs[len(logs)-1].Time = entry.Time
			return logs
		}
	}
	logs = append(logs, entry)
	if len(logs) > maxUpgradeLogEntries {
		logs = logs[len(logs)-maxUpgradeLogEntries:]
	}
	return logs
}

func convertUpgradeLogs(logs []runstate.UpgradeLogEntry) []model.UpgradeLogEntry {
	if len(logs) == 0 {
		return nil
	}
	result := make([]model.UpgradeLogEntry, 0, len(logs))
	for _, entry := range logs {
		result = append(result, model.UpgradeLogEntry{
			Time:    entry.Time,
			Stage:   entry.Stage,
			Message: entry.Message,
		})
	}
	return result
}

func (s *service) markUpgradeFailed(currentVersion string, operationErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(currentVersion) != "" {
		s.state.CurrentVersion = currentVersion
	}
	s.state.Status = "failed"
	s.state.UpgradeLocked = false
	s.state.UpgradeStage = upgradeStageFailed
	s.state.UpgradeProgress = 100
	s.state.UpgradeMessage = operationErr.Error()
	s.state.UpgradeUpdatedAt = time.Now().UTC()
	s.state.UpgradeLogs = appendUpgradeLog(s.state.UpgradeLogs, s.state.UpgradeStage, s.state.UpgradeMessage)
	s.state.LastError = operationErr.Error()
	_ = runstate.Save(s.cfg.RuntimeDir, s.state)
}

func (s *service) clearCompletedUpgradeTarget(currentVersion string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(currentVersion) != "" {
		s.state.CurrentVersion = currentVersion
	}
	if strings.TrimSpace(s.state.TargetVersion) == strings.TrimSpace(currentVersion) {
		s.state.TargetVersion = ""
	}
	if strings.EqualFold(s.state.Status, "upgrading") {
		s.state.Status = "running"
	}
	s.state.UpgradeLocked = false
	s.state.UpgradeStage = upgradeStageAlreadyLatest
	s.state.UpgradeProgress = 100
	s.state.UpgradeMessage = "当前已经是目标版本"
	s.state.UpgradeUpdatedAt = time.Now().UTC()
	s.state.UpgradeLogs = appendUpgradeLog(s.state.UpgradeLogs, s.state.UpgradeStage, s.state.UpgradeMessage)
	_ = runstate.Save(s.cfg.RuntimeDir, s.state)
}

func (s *service) Rollback() error {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	s.mu.RLock()
	previousVersion := s.state.PreviousVersion
	s.mu.RUnlock()
	if previousVersion == "" {
		return errors.New("没有可回滚版本")
	}
	return s.rollbackWithOpsLocked(nil)
}

func (s *service) rollbackWithOpsLocked(reason error) error {
	s.mu.RLock()
	previousVersion := s.state.PreviousVersion
	s.mu.RUnlock()
	if previousVersion == "" {
		if reason != nil {
			return reason
		}
		return errors.New("没有可回滚版本")
	}
	if err := s.stopAllAgentProcesses("stopping"); err != nil {
		return err
	}
	if err := release.Switch(s.cfg.RuntimeDir, previousVersion); err != nil {
		return err
	}
	if err := s.startEnabledAgentsWithOpsLocked("starting"); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.CurrentVersion = previousVersion
	s.state.TargetVersion = ""
	s.state.Status = "running"
	s.syncStateLastErrorLocked()
	if err := saveAgents(s.cfg.RuntimeDir, s.agents); err != nil {
		return err
	}
	return runstate.Save(s.cfg.RuntimeDir, s.state)
}

func (s *service) Directories(req model.DirectoryRequest) (model.DirectoryListResult, error) {
	s.directoryFilesMu.RLock()
	defer s.directoryFilesMu.RUnlock()
	return browseDirectory(s.cfg, strings.TrimSpace(req.Path), req.AllowAll)
}

func (s *service) enrichAIConfigFromRuntime(cfg *model.DeviceAIConfigInfo) *model.DeviceAIConfigInfo {
	if cfg == nil {
		return nil
	}
	metadata, err := s.runtimeProviderMetadata()
	if err != nil {
		return cfg
	}
	return aiconfig.EnrichWithRuntimeProviderMetadata(cfg, metadata)
}

func (s *service) runtimeProviderMetadata() (aiconfig.RuntimeProviderMetadata, error) {
	agents := s.Agents()
	if len(agents) == 0 {
		return aiconfig.RuntimeProviderMetadata{}, nil
	}
	connected := make([]string, 0, len(agents))
	runtimeModels := map[string]map[string]aiconfig.RuntimeModelMetadata{}
	for _, agent := range agents {
		if agent.Port <= 0 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(agent.Status), "running") {
			continue
		}
		var raw struct {
			All []struct {
				ID     string `json:"id"`
				Models map[string]struct {
					ID         string `json:"id"`
					ProviderID string `json:"providerID"`
					Name       string `json:"name"`
					Limit      struct {
						Context int64 `json:"context"`
					} `json:"limit"`
					Modalities   *model.DeviceAIModalities `json:"modalities"`
					Capabilities struct {
						Reasoning bool            `json:"reasoning"`
						Input     map[string]bool `json:"input"`
						Output    map[string]bool `json:"output"`
					} `json:"capabilities"`
					Variants map[string]any `json:"variants"`
				} `json:"models"`
			} `json:"all"`
			Connected []string `json:"connected"`
		}
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		err := getAgentJSON(ctx, agent.Port, "/provider", &raw)
		cancel()
		if err != nil {
			continue
		}
		if len(raw.Connected) > 0 {
			for _, id := range raw.Connected {
				id = strings.TrimSpace(id)
				if id != "" {
					connected = append(connected, id)
				}
			}
		}
		for _, provider := range raw.All {
			providerID := strings.TrimSpace(provider.ID)
			if providerID == "" {
				continue
			}
			models := runtimeModels[providerID]
			if models == nil {
				models = map[string]aiconfig.RuntimeModelMetadata{}
				runtimeModels[providerID] = models
			}
			for modelID, item := range provider.Models {
				modelID = strings.TrimSpace(modelID)
				if modelID == "" {
					continue
				}
				models[modelID] = aiconfig.RuntimeModelMetadata{
					ID:         strings.TrimSpace(item.ID),
					ProviderID: strings.TrimSpace(firstNonEmpty(item.ProviderID, providerID)),
					Name:       strings.TrimSpace(item.Name),
					Limit: aiconfig.RuntimeModelLimit{
						Context: item.Limit.Context,
					},
					Modalities: item.Modalities,
					Capabilities: aiconfig.RuntimeModelCapabilities{
						Reasoning: item.Capabilities.Reasoning,
						Input:     item.Capabilities.Input,
						Output:    item.Capabilities.Output,
					},
					Variants: cloneAnyMap(item.Variants),
				}
			}
		}
	}
	if len(runtimeModels) == 0 {
		return aiconfig.RuntimeProviderMetadata{Connected: uniqueStrings(connected)}, nil
	}
	all := make([]aiconfig.RuntimeProviderMetadataItem, 0, len(runtimeModels))
	for providerID, models := range runtimeModels {
		all = append(all, aiconfig.RuntimeProviderMetadataItem{
			ID:     providerID,
			Models: models,
		})
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].ID < all[j].ID
	})
	return aiconfig.RuntimeProviderMetadata{
		Connected: uniqueStrings(connected),
		All:       all,
	}, nil
}

func (s *service) Agents() []model.Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Agent, len(s.agents))
	copy(out, s.agents)
	return out
}

func (s *service) EnvConfig() (*model.DeviceEnvConfigInfo, error) {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	s.mu.RLock()
	agents := make([]model.Agent, len(s.agents))
	copy(agents, s.agents)
	s.mu.RUnlock()
	return envInfoFromAgents(s.cfg.RuntimeDir, agents)
}

func (s *service) SaveEnvConfig(input model.DeviceEnvConfigInput) (*model.DeviceEnvConfigInfo, error) {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	s.mu.RLock()
	agents := make([]model.Agent, len(s.agents))
	copy(agents, s.agents)
	s.mu.RUnlock()
	current, err := envInfoFromAgents(s.cfg.RuntimeDir, agents)
	if err != nil {
		return nil, err
	}
	if expected := strings.TrimSpace(input.ExpectedRevision); expected != "" && expected != current.Revision {
		return nil, errors.New("环境变量配置已被其他客户端修改，请刷新后重试")
	}
	globalEnv, err := normalizeEnvMap(input.GlobalEnvironment)
	if err != nil {
		return nil, err
	}
	agentEnv, err := normalizeEnvMap(input.AgentEnvironment)
	if err != nil {
		return nil, err
	}
	agentID := strings.TrimSpace(input.AgentID)
	var cfg *model.AgentConfig
	if agentID != "" {
		cfg, err = readAgentConfig(s.cfg.RuntimeDir, agentID)
		if err != nil {
			return nil, err
		}
	}
	if err := saveEnvConfig(s.cfg.RuntimeDir, globalEnv); err != nil {
		return nil, err
	}
	if cfg != nil {
		cfg.UserEnv = agentEnv
		cfg.Env = s.agentEnv(*cfg)
		if err := writeAgentConfig(filepath.Join(s.cfg.RuntimeDir, "agents", agentID), *cfg); err != nil {
			if rollbackErr := saveEnvConfig(s.cfg.RuntimeDir, current.GlobalEnvironment); rollbackErr != nil {
				return nil, fmt.Errorf("保存 Agent 环境失败：%v；回滚设备环境也失败：%w", err, rollbackErr)
			}
			return nil, err
		}
	}
	return envInfoFromAgents(s.cfg.RuntimeDir, agents)
}

func normalizeCompactionThresholdPercent(value int) (int, error) {
	if value == 0 {
		return 0, nil
	}
	if value < 1 || value > 100 {
		return 0, errors.New("上下文压缩比例必须在 1-100 之间")
	}
	return value, nil
}

func effectiveCompactionThresholdPercent(value int) int {
	if value < 1 || value > 100 {
		return defaultCompactionThresholdPercent
	}
	return value
}

func (s *service) AgentCompactionConfig(agentID string) (model.DeviceAgentCompactionConfig, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return model.DeviceAgentCompactionConfig{}, errors.New("agent_id 不能为空")
	}
	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		return model.DeviceAgentCompactionConfig{}, err
	}
	return model.DeviceAgentCompactionConfig{
		AgentID:                   agentID,
		ThresholdPercent:          cfg.CompactionThresholdPercent,
		DefaultThresholdPercent:   defaultCompactionThresholdPercent,
		EffectiveThresholdPercent: effectiveCompactionThresholdPercent(cfg.CompactionThresholdPercent),
	}, nil
}

func (s *service) SaveAgentCompactionConfig(input model.DeviceAgentCompactionConfigInput) (model.DeviceAgentCompactionConfig, error) {
	agentID := strings.TrimSpace(input.AgentID)
	if agentID == "" {
		return model.DeviceAgentCompactionConfig{}, errors.New("agent_id 不能为空")
	}
	threshold, err := normalizeCompactionThresholdPercent(input.ThresholdPercent)
	if err != nil {
		return model.DeviceAgentCompactionConfig{}, err
	}
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		return model.DeviceAgentCompactionConfig{}, err
	}
	cfg.CompactionThresholdPercent = threshold
	// Persist the effective environment for future launches without touching the
	// already running process. This keeps a setting change from interrupting work.
	cfg.Env = s.agentEnv(*cfg)
	if err := writeAgentConfig(filepath.Join(s.cfg.RuntimeDir, "agents", agentID), *cfg); err != nil {
		return model.DeviceAgentCompactionConfig{}, err
	}
	return model.DeviceAgentCompactionConfig{
		AgentID:                   agentID,
		ThresholdPercent:          threshold,
		DefaultThresholdPercent:   defaultCompactionThresholdPercent,
		EffectiveThresholdPercent: effectiveCompactionThresholdPercent(threshold),
	}, nil
}

func (s *service) CreateAgent(input model.CreateAgentInput) (model.Agent, error) {
	projectDir := filepath.Clean(input.ProjectDir)
	info, err := os.Stat(projectDir)
	if err != nil {
		return model.Agent{}, friendlyPathError(projectDir, err)
	}
	if !info.IsDir() {
		return model.Agent{}, errors.New("项目目录不存在")
	}
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	s.mu.Lock()
	for _, agent := range s.agents {
		if agent.ProjectDir == projectDir {
			s.mu.Unlock()
			return model.Agent{}, errors.New("该目录已存在 agent")
		}
	}
	agentID := nextAgentID()
	port := nextPort(s.agents)
	s.mu.Unlock()
	base := filepath.Join(s.cfg.RuntimeDir, "agents", agentID)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return model.Agent{}, err
	}
	agentName := strings.TrimSpace(input.Name)
	if agentName == "" {
		agentName = filepath.Base(projectDir)
	}
	if agentName == "" || agentName == "." || agentName == string(filepath.Separator) {
		agentName = "Agent"
	}
	agentCfg := model.AgentConfig{
		AgentID:         agentID,
		Name:            agentName,
		ProjectDir:      projectDir,
		Port:            port,
		Args:            append(append([]string{}, s.cfg.Agent.BaseArgs...), "--port", fmt.Sprintf("%d", port)),
		SemanticAgentID: defaultSemanticAgentID,
	}
	agentCfg.Env = s.agentEnv(agentCfg)
	agent, err := s.spawnAgent(agentCfg)
	if err != nil {
		return model.Agent{}, err
	}
	agent.Enabled = true
	s.mu.Lock()
	s.agents = append(s.agents, agent)
	s.state.Status = "running"
	s.state.AgentCount = len(s.agents)
	s.syncStateLastErrorLocked()
	if err := saveAgents(s.cfg.RuntimeDir, s.agents); err != nil {
		s.mu.Unlock()
		return model.Agent{}, err
	}
	if err := runstate.Save(s.cfg.RuntimeDir, s.state); err != nil {
		s.mu.Unlock()
		return model.Agent{}, err
	}
	s.mu.Unlock()
	_ = writeAgentConfig(base, agentCfg)
	return agent, nil
}

func (s *service) RenameAgent(input model.RenameAgentInput) (model.Agent, error) {
	agentID := strings.TrimSpace(input.AgentID)
	name := strings.TrimSpace(input.Name)
	if agentID == "" {
		return model.Agent{}, proc.ErrAgentNotFound
	}
	if name == "" {
		return model.Agent{}, errors.New("Agent 名称不能为空")
	}
	if utf8.RuneCountInString(name) > 80 {
		return model.Agent{}, errors.New("Agent 名称不能超过 80 个字符")
	}
	if strings.ContainsAny(name, "\r\n\x00") {
		return model.Agent{}, errors.New("Agent 名称不能包含换行或空字符")
	}

	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		return model.Agent{}, err
	}
	previousName := cfg.Name
	cfg.Name = name
	if err := writeAgentConfig(filepath.Join(s.cfg.RuntimeDir, "agents", agentID), *cfg); err != nil {
		return model.Agent{}, err
	}

	s.mu.Lock()
	index := s.findAgentIndexLocked(agentID)
	if index < 0 {
		s.mu.Unlock()
		cfg.Name = previousName
		_ = writeAgentConfig(filepath.Join(s.cfg.RuntimeDir, "agents", agentID), *cfg)
		return model.Agent{}, proc.ErrAgentNotFound
	}
	previous := s.agents[index]
	s.agents[index].Name = name
	s.agents[index].UpdatedAt = time.Now().UTC()
	updated := s.agents[index]
	if err := saveAgents(s.cfg.RuntimeDir, s.agents); err != nil {
		s.agents[index] = previous
		s.mu.Unlock()
		cfg.Name = previousName
		_ = writeAgentConfig(filepath.Join(s.cfg.RuntimeDir, "agents", agentID), *cfg)
		return model.Agent{}, err
	}
	s.mu.Unlock()
	return updated, nil
}

func (s *service) RestartAgent(agentID string) (model.Agent, error) {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if !s.hasAgent(agentID) {
		return model.Agent{}, proc.ErrAgentNotFound
	}
	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		return model.Agent{}, err
	}
	if err := s.refreshAndPersistManagedAgentEnv(cfg); err != nil {
		return model.Agent{}, err
	}
	return s.restartAgentWithConfig(agentID, cfg, "restarting")
}

func (s *service) CorrectProjectIdentity(input model.ProjectIdentityCorrectionInput) (model.ProjectIdentityCorrectionResult, error) {
	agentID := strings.TrimSpace(input.AgentID)
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if !s.hasAgent(agentID) {
		return model.ProjectIdentityCorrectionResult{}, proc.ErrAgentNotFound
	}
	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		return model.ProjectIdentityCorrectionResult{}, err
	}
	identity, err := projectidentity.Correct(
		s.cfg.RuntimeDir, s.cfg.Relay.MachineID, cfg.ProjectDir, input.Action, input.KeepScopeID,
	)
	result := model.ProjectIdentityCorrectionResult{
		ProjectScopeID: identity.ScopeID, InstanceNonce: identity.InstanceNonce,
		LineageProjectScopeID: identity.LineageScopeID, Root: identity.Root,
		MarkerWritable: identity.MarkerWritable,
	}
	if err != nil {
		return result, err
	}
	if err := s.refreshAndPersistManagedAgentEnv(cfg); err != nil {
		return result, err
	}
	if _, err := s.restartAgentWithConfig(agentID, cfg, "restarting"); err != nil {
		return result, err
	}
	return result, nil
}

func (s *service) SetAgentEnabled(input model.SetAgentEnabledInput) (model.Agent, error) {
	agentID := strings.TrimSpace(input.AgentID)
	if agentID == "" {
		return model.Agent{}, proc.ErrAgentNotFound
	}
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if !s.hasAgent(agentID) {
		return model.Agent{}, proc.ErrAgentNotFound
	}
	if !input.Enabled {
		return s.stopAndDisableAgent(agentID)
	}
	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		return model.Agent{}, err
	}
	if err := s.refreshAndPersistManagedAgentEnv(cfg); err != nil {
		return model.Agent{}, err
	}
	return s.restartAgentWithConfig(agentID, cfg, "starting")
}

func (s *service) RemoveAgent(agentID string) error {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	agentDir := filepath.Join(s.cfg.RuntimeDir, "agents", agentID)
	previous, trackedPID, err := s.beginAgentTransition(agentID, "stopping")
	if err != nil {
		return err
	}
	if err := s.stopAgentProcess(previous, trackedPID); err != nil {
		s.markAgentOperationFailed(agentID, err, false)
		return err
	}
	s.mu.Lock()
	index := s.findAgentIndexLocked(agentID)
	if index < 0 {
		s.mu.Unlock()
		return proc.ErrAgentNotFound
	}
	s.agents = append(s.agents[:index], s.agents[index+1:]...)
	delete(s.processes, agentID)
	s.state.AgentCount = len(s.agents)
	s.syncStateLastErrorLocked()
	if err := saveAgents(s.cfg.RuntimeDir, s.agents); err != nil {
		s.mu.Unlock()
		return err
	}
	if err := runstate.Save(s.cfg.RuntimeDir, s.state); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return os.RemoveAll(agentDir)
}

func (s *service) watchProcess(agentID string, pid int, waitCh <-chan error) {
	if waitCh == nil {
		return
	}
	_, _ = <-waitCh
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	s.mu.Lock()
	currentPID, ok := s.processes[agentID]
	if !ok || currentPID != pid {
		s.mu.Unlock()
		return
	}
	delete(s.processes, agentID)
	index := s.findAgentIndexLocked(agentID)
	if index < 0 {
		s.mu.Unlock()
		return
	}
	if !s.agents[index].Enabled {
		s.markAgentDisabledLocked(index, "")
		s.syncStateLastErrorLocked()
		_ = runstate.Save(s.cfg.RuntimeDir, s.state)
		_ = saveAgents(s.cfg.RuntimeDir, s.agents)
		s.mu.Unlock()
		return
	}
	previous := s.agents[index]
	restarts := previous.Restarts
	s.agents[index].PID = 0
	if restarts >= s.cfg.Health.MaxRestarts {
		s.agents[index].Status = "failed"
		s.agents[index].LastError = "超过最大重启次数"
		s.agents[index].UpdatedAt = time.Now().UTC()
		s.state.LastError = s.agents[index].LastError
		s.syncStateLastErrorLocked()
		_ = runstate.Save(s.cfg.RuntimeDir, s.state)
		_ = saveAgents(s.cfg.RuntimeDir, s.agents)
		s.mu.Unlock()
		return
	}
	s.agents[index].Status = "restarting"
	s.agents[index].UpdatedAt = time.Now().UTC()
	s.mu.Unlock()

	cfg, err := readAgentConfig(s.cfg.RuntimeDir, agentID)
	if err != nil {
		s.markAgentOperationFailed(agentID, err, true)
		return
	}
	s.refreshManagedAgentEnv(cfg)
	time.Sleep(time.Duration(s.cfg.Health.BackoffMillis) * time.Millisecond)
	restarted, _, err := s.adoptOrSpawnAgent(context.Background(), *cfg, previous)
	if err != nil {
		s.markAgentOperationFailed(agentID, err, true)
		return
	}
	restarted.Restarts = restarts + 1
	restarted.LastError = ""
	restarted.Enabled = true
	_, _ = s.commitAgent(agentID, restarted)
}

func (s *service) spawnAgent(cfg model.AgentConfig) (model.Agent, error) {
	previous := s.agentSnapshot(cfg.AgentID)
	agent, waitCh, err := s.spawnAgentProcess(cfg, previous)
	if err != nil {
		return model.Agent{}, err
	}
	s.mu.Lock()
	s.registerAgentProcessLocked(agent, waitCh)
	s.mu.Unlock()
	return agent, nil
}

func (s *service) spawnAgentProcess(cfg model.AgentConfig, previous model.Agent) (model.Agent, <-chan error, error) {
	return s.spawnAgentProcessContext(context.Background(), cfg, previous)
}

func (s *service) spawnAgentProcessContext(ctx context.Context, cfg model.AgentConfig, previous model.Agent) (model.Agent, <-chan error, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	startedAt := time.Now()
	stepStartedAt := startedAt
	cfg.BinaryName = s.binaryPath()
	dir := filepath.Join(s.cfg.RuntimeDir, "work")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return model.Agent{}, nil, err
	}
	stdoutPath, stderrPath := proc.DefaultBinaryLogs(s.cfg.RuntimeDir, cfg.AgentID)
	launcherLatencyLog("agent_process_starting", map[string]any{
		"agent_id":                cfg.AgentID,
		"project_dir":             cfg.ProjectDir,
		"port":                    cfg.Port,
		"semantic_agent_id":       cfg.SemanticAgentID,
		"binary":                  cfg.BinaryName,
		"opencode_config_content": cfg.Env["OPENCODE_CONFIG_CONTENT"] != "",
	})
	cmd, err := proc.Start(proc.LaunchInput{
		BinaryName: cfg.BinaryName,
		AgentID:    cfg.AgentID,
		Args:       append([]string{}, cfg.Args...),
		Dir:        dir,
		Env:        cloneEnv(cfg.Env),
		StdoutPath: stdoutPath,
		StderrPath: stderrPath,
		PrintLogs:  s.cfg.Agent.PrintLogs,
	})
	if err != nil {
		launcherLatencyLog("agent_process_start_failed", map[string]any{
			"agent_id":    cfg.AgentID,
			"project_dir": cfg.ProjectDir,
			"port":        cfg.Port,
			"elapsed_ms":  time.Since(startedAt).Milliseconds(),
			"step_ms":     time.Since(stepStartedAt).Milliseconds(),
			"error":       err.Error(),
		})
		return model.Agent{}, nil, err
	}
	launcherLatencyLog("agent_process_started", map[string]any{
		"agent_id":    cfg.AgentID,
		"project_dir": cfg.ProjectDir,
		"port":        cfg.Port,
		"pid":         cmd.Process.Pid,
		"elapsed_ms":  time.Since(startedAt).Milliseconds(),
		"step_ms":     time.Since(stepStartedAt).Milliseconds(),
	})
	stepStartedAt = time.Now()
	waitCh := proc.WaitAsync(cmd)
	healthCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.Health.TimeoutSeconds)*time.Second)
	defer cancel()
	interval := time.Duration(s.cfg.Health.IntervalMillis) * time.Millisecond
	if err := proc.WaitForReady(healthCtx, cfg.Port, interval); err != nil {
		_ = proc.Stop(cmd.Process.Pid)
		launcherLatencyLog("agent_healthcheck_failed", map[string]any{
			"agent_id":    cfg.AgentID,
			"project_dir": cfg.ProjectDir,
			"port":        cfg.Port,
			"pid":         cmd.Process.Pid,
			"elapsed_ms":  time.Since(startedAt).Milliseconds(),
			"step_ms":     time.Since(stepStartedAt).Milliseconds(),
			"error":       err.Error(),
		})
		return model.Agent{}, nil, fmt.Errorf("agent 健康检查失败: %w", err)
	}
	launcherLatencyLog("agent_healthcheck_ready", map[string]any{
		"agent_id":    cfg.AgentID,
		"project_dir": cfg.ProjectDir,
		"port":        cfg.Port,
		"pid":         cmd.Process.Pid,
		"elapsed_ms":  time.Since(startedAt).Milliseconds(),
		"step_ms":     time.Since(stepStartedAt).Milliseconds(),
	})
	stepStartedAt = time.Now()
	time.Sleep(250 * time.Millisecond)
	select {
	case waitErr := <-waitCh:
		if waitErr == nil {
			launcherLatencyLog("agent_process_exited_early", map[string]any{
				"agent_id":    cfg.AgentID,
				"project_dir": cfg.ProjectDir,
				"port":        cfg.Port,
				"pid":         cmd.Process.Pid,
				"elapsed_ms":  time.Since(startedAt).Milliseconds(),
				"step_ms":     time.Since(stepStartedAt).Milliseconds(),
			})
			return model.Agent{}, nil, fmt.Errorf("agent 启动后立即退出，端口 %d 可能已被旧进程占用", cfg.Port)
		}
		launcherLatencyLog("agent_process_exited_early", map[string]any{
			"agent_id":    cfg.AgentID,
			"project_dir": cfg.ProjectDir,
			"port":        cfg.Port,
			"pid":         cmd.Process.Pid,
			"elapsed_ms":  time.Since(startedAt).Milliseconds(),
			"step_ms":     time.Since(stepStartedAt).Milliseconds(),
			"error":       waitErr.Error(),
		})
		return model.Agent{}, nil, fmt.Errorf("agent 启动后立即退出: %w", waitErr)
	default:
	}
	now := time.Now().UTC()
	createdAt := now
	if !previous.CreatedAt.IsZero() {
		createdAt = previous.CreatedAt
	}
	agent := model.Agent{
		AgentID:    cfg.AgentID,
		Name:       cfg.Name,
		ProjectDir: cfg.ProjectDir,
		Enabled:    true,
		Status:     "running",
		PID:        cmd.Process.Pid,
		Port:       cfg.Port,
		LastError:  "",
		CreatedAt:  createdAt,
		UpdatedAt:  now,
	}
	applySemanticAgentFieldsFromConfig(&agent, cfg)
	return agent, waitCh, nil
}

func (s *service) restoreAgent(ctx context.Context, cfg model.AgentConfig, forceRespawn bool) (model.Agent, bool, error) {
	if forceRespawn {
		if err := s.reassignPortIfBlocked(&cfg); err != nil {
			return model.Agent{}, false, err
		}
	}
	previous, trackedPID, err := s.beginAgentTransition(cfg.AgentID, "starting")
	if err != nil {
		return model.Agent{}, false, err
	}
	previous.Enabled = true
	if forceRespawn {
		if err := s.stopAgentProcess(previous, trackedPID); err != nil {
			return model.Agent{}, false, err
		}
		agent, waitCh, err := s.spawnAgentProcessContext(ctx, cfg, previous)
		if err != nil {
			return model.Agent{}, false, err
		}
		s.mu.Lock()
		s.registerAgentProcessLocked(agent, waitCh)
		s.mu.Unlock()
		return agent, false, nil
	}
	return s.adoptOrSpawnAgent(ctx, cfg, previous)
}

func (s *service) adoptOrSpawnAgent(ctx context.Context, cfg model.AgentConfig, previous model.Agent) (model.Agent, bool, error) {
	if agent, ok := s.adoptRunningAgent(cfg, previous); ok {
		s.mu.Lock()
		if agent.PID > 0 {
			s.processes[cfg.AgentID] = agent.PID
		}
		s.mu.Unlock()
		return agent, true, nil
	}
	agent, waitCh, err := s.spawnAgentProcessContext(ctx, cfg, previous)
	if err != nil {
		return model.Agent{}, false, err
	}
	s.mu.Lock()
	s.registerAgentProcessLocked(agent, waitCh)
	s.mu.Unlock()
	return agent, false, nil
}

func (s *service) adoptRunningAgent(cfg model.AgentConfig, previous model.Agent) (model.Agent, bool) {
	if cfg.Port <= 0 || !proc.IsHealthy(cfg.Port, 700*time.Millisecond) {
		return model.Agent{}, false
	}
	now := time.Now().UTC()
	pid, _ := proc.FindListeningPID(cfg.Port)
	createdAt := previous.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	agent := model.Agent{
		AgentID:    cfg.AgentID,
		Name:       cfg.Name,
		ProjectDir: cfg.ProjectDir,
		Enabled:    true,
		Status:     "running",
		PID:        pid,
		Port:       cfg.Port,
		Restarts:   0,
		LastError:  "",
		CreatedAt:  createdAt,
		UpdatedAt:  now,
	}
	applySemanticAgentFieldsFromConfig(&agent, cfg)
	return agent, true
}

func (s *service) agentSnapshot(agentID string) model.Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index := s.findAgentIndexLocked(agentID)
	if index < 0 {
		return model.Agent{}
	}
	return s.agents[index]
}

func (s *service) registerAgentProcessLocked(agent model.Agent, waitCh <-chan error) {
	if agent.PID <= 0 {
		return
	}
	s.processes[agent.AgentID] = agent.PID
	go s.watchProcess(agent.AgentID, agent.PID, waitCh)
}

func (s *service) stopAgentProcessLocked(agent model.Agent) error {
	if err := s.stopAgentProcess(agent, s.processes[agent.AgentID]); err != nil {
		return err
	}
	delete(s.processes, agent.AgentID)
	return nil
}

func (s *service) stopAgentProcess(agent model.Agent, trackedPID int) error {
	interval := 200 * time.Millisecond
	targets := []int{}
	seen := map[int]struct{}{}
	for _, pid := range []int{agent.PID, trackedPID} {
		if pid <= 0 {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		targets = append(targets, pid)
	}
	if agent.Port > 0 {
		if pid, err := proc.FindListeningPID(agent.Port); err == nil && pid > 0 {
			if _, ok := seen[pid]; !ok {
				seen[pid] = struct{}{}
				if pid != os.Getpid() {
					targets = append(targets, pid)
				}
			}
		}
	}
	for _, pid := range targets {
		if err := proc.Stop(pid); err != nil {
			return fmt.Errorf("停止 agent %s 进程 %d 失败: %w", agent.AgentID, pid, err)
		}
	}
	if agent.Port > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := proc.StopListeningProcessUntilPortClosed(ctx, agent.Port, interval); err != nil {
			if pid, findErr := proc.FindListeningPID(agent.Port); findErr == nil && pid > 0 {
				return fmt.Errorf("停止 agent %s 后端口 %d 仍被进程 %d 占用: %w", agent.AgentID, agent.Port, pid, err)
			}
			return fmt.Errorf("停止 agent %s 后端口 %d 未释放: %w", agent.AgentID, agent.Port, err)
		}
	}
	return nil
}

func (s *service) agentEnv(cfg model.AgentConfig) map[string]string {
	env := cloneEnv(s.cfg.Agent.Env)
	if globalEnv, err := loadEnvConfig(s.cfg.RuntimeDir); err == nil {
		for key, value := range globalEnv {
			env[key] = value
		}
	}
	for key, value := range cfg.UserEnv {
		env[key] = value
	}
	env["OPENCODE_RELAY_URL"] = s.cfg.Relay.URL
	env["OPENCODE_RELAY_OPERATOR_KEY"] = s.cfg.Relay.OperatorKey
	env["OPENCODE_RELAY_AGENT_ID"] = cfg.AgentID
	env["OPENCODE_RELAY_MACHINE_ID"] = s.cfg.Relay.MachineID
	env["OPENCODE_RELAY_PROJECT_ID"] = filepath.Base(cfg.ProjectDir)
	env["OPENCODE_RELAY_PROJECT_ROOT"] = cfg.ProjectDir
	env["OPENCODE_PROJECT_ROOT"] = cfg.ProjectDir
	if identity, err := projectidentity.Resolve(s.cfg.RuntimeDir, s.cfg.Relay.MachineID, cfg.ProjectDir); err != nil {
		log.Printf("[launcher][project-memory] identity resolution failed agent_id=%s root=%s err=%v", cfg.AgentID, cfg.ProjectDir, err)
	} else {
		env["OPENCODE_RELAY_PROJECT_SCOPE_ID"] = identity.ScopeID
		env["OPENCODE_RELAY_PROJECT_INSTANCE_NONCE"] = identity.InstanceNonce
		env["OPENCODE_RELAY_PROJECT_LINEAGE_SCOPE_ID"] = identity.LineageScopeID
		env["OPENCODE_RELAY_PROJECT_VOLUME_ID"] = identity.VolumeID
		env["OPENCODE_RELAY_PROJECT_FILE_ID"] = identity.FileID
		env["OPENCODE_RELAY_PROJECT_DISPLAY_NAME"] = identity.DisplayName
	}
	if _, ok := env["OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS"]; !ok {
		env["OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS"] = "true"
	}
	if _, ok := env["OPENCODE_SEMANTIC_AGENT_ID"]; !ok {
		env["OPENCODE_SEMANTIC_AGENT_ID"] = normalizeSemanticAgentID(cfg.SemanticAgentID)
	}
	if _, ok := env["OPENCODE_DISABLE_PROJECT_CONFIG"]; !ok {
		env["OPENCODE_DISABLE_PROJECT_CONFIG"] = "1"
	}
	if strings.TrimSpace(cfg.SemanticAgentID) != "" {
		if _, ok := env["OPENCODE_DISABLE_EXTERNAL_SKILLS"]; !ok {
			env["OPENCODE_DISABLE_EXTERNAL_SKILLS"] = "1"
		}
		if _, ok := env["OPENCODE_SEMANTIC_AGENT_ISOLATION"]; !ok {
			env["OPENCODE_SEMANTIC_AGENT_ISOLATION"] = "1"
		}
		s.applySemanticRuntimeEnv(cfg, env)
		s.applyExternalToolEnvironment(env)
		if content, ok := s.agentSemanticConfigContent(cfg, env["OPENCODE_CONFIG_CONTENT"]); ok {
			env["OPENCODE_CONFIG_CONTENT"] = content
		}
	}
	if content, ok := s.globalSkillConfigContent(env["OPENCODE_CONFIG_CONTENT"]); ok {
		env["OPENCODE_CONFIG_CONTENT"] = content
	}
	if content, ok := s.agentMCPConfigContent(cfg, env["OPENCODE_CONFIG_CONTENT"]); ok {
		env["OPENCODE_CONFIG_CONTENT"] = content
	}
	if content, ok := agentCompactionConfigContent(cfg, env["OPENCODE_CONFIG_CONTENT"]); ok {
		env["OPENCODE_CONFIG_CONTENT"] = content
	}
	if cfg.DisableVerifyMCP {
		delete(env, "VERIFY_API_TOKEN")
		delete(env, "VERIFY_PROTECT_TOKEN")
	}
	if _, ok := env["OPENCODE_RELAY_PERMISSION_MODE"]; !ok {
		env["OPENCODE_RELAY_PERMISSION_MODE"] = "auto-approve"
	}
	return env
}

func (s *service) globalSkillConfigContent(existing string) (string, bool) {
	if _, err := os.Stat(filepath.Join(s.cfg.RuntimeDir, "skills", "config.json")); errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	settings, err := loadGlobalSkillConfig(s.cfg.RuntimeDir)
	if err != nil {
		log.Printf("[launcher][skills] failed to load global config err=%v", err)
		return "", false
	}
	content := map[string]any{}
	if strings.TrimSpace(existing) != "" {
		if err := opencodeconfig.Decode([]byte(existing), &content); err != nil {
			content = map[string]any{}
		}
	}
	skills, _ := content["skills"].(map[string]any)
	if skills == nil {
		skills = map[string]any{}
	}
	paths := make([]string, 0)
	if raw, ok := skills["paths"].([]any); ok {
		for _, item := range raw {
			path, ok := item.(string)
			if !ok || isGlobalSkillPath(path, s.cfg.RuntimeDir) {
				continue
			}
			paths = append(paths, path)
		}
	}
	if settings.Enabled {
		for _, skill := range loadLocalSkillDefinitions(s.cfg.RuntimeDir) {
			if settings.Skills[skill.Name] {
				paths = append(paths, filepath.Join(s.cfg.RuntimeDir, "skills", skill.Name))
			}
		}
	}
	if len(paths) == 0 {
		delete(skills, "paths")
	} else {
		skills["paths"] = paths
	}
	if len(skills) == 0 {
		delete(content, "skills")
	} else {
		content["skills"] = skills
	}
	data, err := json.Marshal(content)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func isGlobalSkillPath(path, runtimeDir string) bool {
	root := filepath.Clean(filepath.Join(runtimeDir, "skills"))
	clean := filepath.Clean(strings.TrimSpace(path))
	return clean == root || strings.HasPrefix(clean, root+string(filepath.Separator))
}

func agentCompactionConfigContent(cfg model.AgentConfig, existing string) (string, bool) {
	threshold, err := normalizeCompactionThresholdPercent(cfg.CompactionThresholdPercent)
	if err != nil {
		return "", false
	}
	content := map[string]any{}
	if strings.TrimSpace(existing) != "" {
		if err := opencodeconfig.Decode([]byte(existing), &content); err != nil {
			return "", false
		}
	}
	compaction, _ := content["compaction"].(map[string]any)
	if threshold == 0 {
		if compaction == nil {
			return "", false
		}
		if _, exists := compaction["threshold_percent"]; !exists {
			return "", false
		}
		delete(compaction, "threshold_percent")
		if len(compaction) == 0 {
			delete(content, "compaction")
		} else {
			content["compaction"] = compaction
		}
		data, err := json.Marshal(content)
		if err != nil {
			return "", false
		}
		return string(data), true
	}
	if compaction == nil {
		compaction = map[string]any{}
	}
	compaction["threshold_percent"] = threshold
	content["compaction"] = compaction
	data, err := json.Marshal(content)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func (s *service) agentMCPConfigContent(cfg model.AgentConfig, existing string) (string, bool) {
	if normalizedMCPMode(cfg.MCPMode) != "custom" {
		return "", false
	}
	selected := map[string]bool{}
	for _, name := range normalizeMCPServerNames(cfg.MCPServers) {
		selected[name] = true
	}
	mcp := map[string]any{}
	fileServers := map[string]bool{}
	info, err := mcpconfig.Load()
	if err != nil {
		log.Printf("[launcher] failed to load mcp config for agent override agent_id=%s err=%v", cfg.AgentID, err)
	} else {
		disabledByOverride := make([]string, 0)
		for _, server := range info.Servers {
			fileServers[server.Name] = true
			enabled := selected[server.Name]
			override := map[string]any{"enabled": enabled}
			if enabled {
				if env, changed := mcpconfig.EnvironmentWithAgentCredentialOverrides(server.Environment, cfg.UserEnv); changed {
					override["environment"] = env
				}
			}
			mcp[server.Name] = override
			if !enabled {
				disabledByOverride = append(disabledByOverride, server.Name)
			}
		}
		log.Printf("[launcher][mcp] agentMCPConfigContent agent_id=%s mode=custom selected=%v file_servers=%d isolated=%v",
			cfg.AgentID, normalizeMCPServerNames(cfg.MCPServers), len(info.Servers), disabledByOverride)
	}
	for _, server := range cfg.MCPServerConfigs {
		server.Name = strings.TrimSpace(server.Name)
		if fileServers[server.Name] {
			log.Printf("[launcher][mcp] agentMCPConfigContent agent_id=%s server=%s 使用全局用户配置，跳过语义 Agent 推荐模板", cfg.AgentID, server.Name)
			continue
		}
		resolved, err := resolveMCPServerForRuntime(s.cfg.RuntimeDir, server)
		if err != nil {
			log.Printf("[launcher] failed to resolve semantic mcp server agent_id=%s server=%s err=%v", cfg.AgentID, server.Name, err)
			continue
		}
		if env, changed := mcpconfig.EnvironmentWithAgentCredentialOverrides(resolved.Environment, cfg.UserEnv); changed {
			resolved.Environment = env
		}
		value, err := agentMCPServerContent(resolved)
		if err != nil {
			log.Printf("[launcher] failed to encode semantic mcp server agent_id=%s server=%s err=%v", cfg.AgentID, server.Name, err)
			continue
		}
		mcp[server.Name] = value
	}
	content := map[string]any{}
	if strings.TrimSpace(existing) != "" {
		if err := opencodeconfig.Decode([]byte(existing), &content); err != nil {
			log.Printf("[launcher] failed to decode existing OPENCODE_CONFIG_CONTENT agent_id=%s err=%v", cfg.AgentID, err)
			content = map[string]any{}
		}
	}
	content["mcp"] = mcp
	data, err := json.Marshal(content)
	if err != nil {
		log.Printf("[launcher] failed to encode mcp override agent_id=%s err=%v", cfg.AgentID, err)
		return "", false
	}
	return string(data), true
}

func agentMCPServerContent(server model.DeviceMCPServerInfo) (map[string]any, error) {
	server.Name = strings.TrimSpace(server.Name)
	server.Type = strings.TrimSpace(server.Type)
	if server.Name == "" {
		return nil, errors.New("MCP 名称不能为空")
	}
	if server.Type == "" {
		server.Type = "local"
	}
	switch server.Type {
	case "local", "remote":
	default:
		return nil, fmt.Errorf("不支持的 MCP 类型：%s", server.Type)
	}
	out := map[string]any{
		"type":    server.Type,
		"enabled": true,
	}
	if server.Timeout > 0 {
		out["timeout"] = server.Timeout
	}
	if server.ConnectTimeout > 0 {
		out["connect_timeout"] = server.ConnectTimeout
	}
	if server.DiscoveryTimeout > 0 {
		out["discovery_timeout"] = server.DiscoveryTimeout
	}
	if server.ToolTimeout > 0 {
		out["tool_timeout"] = server.ToolTimeout
	}
	if tools := cleanedStringList(server.AsyncTools); len(tools) > 0 {
		out["async_tools"] = tools
	}
	if server.Type == "remote" {
		if strings.TrimSpace(server.URL) == "" {
			return nil, errors.New("远程 MCP 必须填写 URL")
		}
		out["url"] = strings.TrimSpace(server.URL)
		if headers := cleanedStringMap(server.Headers); len(headers) > 0 {
			out["headers"] = headers
		}
		if strings.EqualFold(strings.TrimSpace(server.OAuthMode), "disabled") {
			out["oauth"] = false
		}
		return out, nil
	}
	command := cleanedStringList(server.Command)
	if len(command) == 0 {
		return nil, errors.New("本地 MCP 必须填写启动命令")
	}
	out["command"] = command
	if env := mcpconfig.SanitizeRuntimeEnvironment(server.Environment); len(env) > 0 {
		out["environment"] = env
	}
	return out, nil
}

func cleanedStringList(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func cleanedStringMap(items map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range items {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *service) refreshManagedAgentEnv(cfg *model.AgentConfig) {
	if cfg == nil {
		return
	}
	cfg.Env = s.agentEnv(*cfg)
}

func (s *service) refreshAndPersistManagedAgentEnv(cfg *model.AgentConfig) error {
	if cfg == nil {
		return nil
	}
	s.refreshManagedAgentEnv(cfg)
	return writeAgentConfig(filepath.Join(s.cfg.RuntimeDir, "agents", cfg.AgentID), *cfg)
}

func (s *service) binaryPath() string {
	s.mu.RLock()
	currentVersion := s.state.CurrentVersion
	s.mu.RUnlock()
	current, err := release.Current(s.cfg.RuntimeDir)
	if err == nil && strings.TrimSpace(current) != "" {
		path, pathErr := release.ExecutablePath(s.cfg.RuntimeDir, current)
		if pathErr == nil {
			return path
		}
	}
	if currentVersion == "" {
		return s.cfg.Agent.BinaryName
	}
	path, err := release.ExecutablePath(s.cfg.RuntimeDir, currentVersion)
	if err != nil {
		return s.cfg.Agent.BinaryName
	}
	return path
}

func (s *service) syncStateLastErrorLocked() {
	s.state.LastError = currentAgentError(s.agents)
}

func currentAgentError(agents []model.Agent) string {
	for _, agent := range agents {
		lastError := strings.TrimSpace(agent.LastError)
		if lastError == "" {
			continue
		}
		status := strings.TrimSpace(agent.Status)
		if strings.EqualFold(status, "running") {
			continue
		}
		return lastError
	}
	return ""
}

func fetchMCPStatus(ctx context.Context, port int) ([]model.MCPServerStatusInfo, error) {
	var raw map[string]struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := getAgentJSON(ctx, port, "/mcp", &raw); err != nil {
		return nil, err
	}
	servers := make([]model.MCPServerStatusInfo, 0, len(raw))
	for name, item := range raw {
		servers = append(servers, model.MCPServerStatusInfo{
			Name:   name,
			Status: item.Status,
			Error:  friendlyMCPRuntimeError(item.Error),
		})
	}
	return servers, nil
}

func fetchMCPTools(ctx context.Context, port int) (map[string][]model.MCPToolInfo, error) {
	var raw map[string][]struct {
		ID          string `json:"id"`
		Description string `json:"description"`
	}
	if err := getAgentJSON(ctx, port, "/mcp/tools", &raw); err != nil {
		return nil, err
	}
	result := map[string][]model.MCPToolInfo{}
	for name, items := range raw {
		for _, item := range items {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			result[name] = append(result[name], model.MCPToolInfo{ID: item.ID, Description: item.Description})
		}
	}
	return result, nil
}

func getAgentJSON(ctx context.Context, port int, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("Agent 接口返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func writeAgentConfig(base string, cfg model.AgentConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(base, "config.json"), append(data, '\n'), 0o600)
}

func readAgentConfig(runtimeDir string, agentID string) (*model.AgentConfig, error) {
	path := filepath.Join(runtimeDir, "agents", agentID, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	var cfg model.AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.BinaryName = ""
	return &cfg, nil
}

func cloneEnv(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func envMapsEqual(a map[string]string, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func runtimeEnvironmentInfoFromDiagnostics(runtimeDir string, patch map[string]string, diagnostics toolchain.Diagnostics, logs string, err error) model.RuntimeEnvironmentInfo {
	lastError := ""
	if err != nil {
		lastError = friendlyRuntimeError(err.Error())
	}
	return model.RuntimeEnvironmentInfo{
		RuntimeDir:  runtimeDir,
		Preparing:   false,
		UpdatedAt:   time.Now().UTC(),
		Environment: cloneEnv(patch),
		Tools:       buildRuntimeTools(diagnostics),
		Logs:        runtimeLogLines(logs),
		LastError:   lastError,
	}
}

func buildRuntimeTools(diagnostics toolchain.Diagnostics) []model.RuntimeToolStatusInfo {
	out := []model.RuntimeToolStatusInfo{}
	for _, status := range []toolchain.ToolStatus{
		diagnostics.Node,
		diagnostics.NPM,
		diagnostics.NPX,
		diagnostics.UV,
		diagnostics.Python,
		diagnostics.PIP,
		diagnostics.Java,
		diagnostics.Javac,
	} {
		item := runtimeToolStatusInfo(status)
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func runtimeToolStatusInfo(status toolchain.ToolStatus) model.RuntimeToolStatusInfo {
	if strings.TrimSpace(status.Name) == "" {
		return model.RuntimeToolStatusInfo{}
	}
	source := "system"
	if status.Managed {
		source = "managed"
	}
	state := "available"
	if strings.TrimSpace(status.Error) != "" {
		state = "unavailable"
	}
	return model.RuntimeToolStatusInfo{
		Name:    status.Name,
		Path:    status.Path,
		Version: status.Version,
		Source:  source,
		Status:  state,
		Error:   friendlyRuntimeError(status.Error),
	}
}

func runtimeLogLines(logs string) []string {
	lines := []string{}
	for _, line := range strings.Split(logs, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, friendlyRuntimeError(line))
	}
	if len(lines) == 0 {
		lines = append(lines, "运行时检测完成。")
	}
	return lines
}

func cloneRuntimeEnvironmentInfo(info model.RuntimeEnvironmentInfo) model.RuntimeEnvironmentInfo {
	out := info
	out.Environment = cloneEnv(info.Environment)
	if info.Tools != nil {
		out.Tools = append([]model.RuntimeToolStatusInfo{}, info.Tools...)
	}
	if info.Logs != nil {
		out.Logs = append([]string{}, info.Logs...)
	}
	return out
}

func friendlyRuntimeError(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "operation timed out") || strings.Contains(lower, "context deadline exceeded") {
		return "MCP 启动或工具列表获取超时，通常是 uv/npx 首次下载、构建或网络源较慢导致；请在 GUI 的环境页先准备运行时，完成后重启对应 Agent"
	}
	if strings.Contains(lower, "download") || strings.Contains(lower, "下载失败") || strings.Contains(lower, "403 forbidden") || strings.Contains(lower, "timeout") {
		return "运行时下载失败或网络超时；launcher 会按镜像源和托管服务器顺序重试，也可以在 GUI 环境页重新准备"
	}
	return friendlyMCPRuntimeError(raw)
}

func friendlyMCPRuntimeError(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "operation timed out") || strings.Contains(lower, "context deadline exceeded") {
		return "MCP 启动或工具列表获取超时，通常是 uv/npx 首次下载、构建或网络源较慢导致；请在 GUI 的环境页先准备运行时，完成后重启对应 Agent"
	}
	if !(strings.Contains(lower, "executable not found") ||
		strings.Contains(lower, "not found in $path") ||
		strings.Contains(lower, "not found in path") ||
		strings.Contains(lower, "is not recognized")) {
		return raw
	}
	switch {
	case containsRuntimeName(lower, "npx") || containsRuntimeName(lower, "npm") || containsRuntimeName(lower, "node"):
		return "托管 Node 未就绪，npx/npm 暂不可用；launcher 会在运行时准备完成后自动刷新并重启 Agent"
	case containsRuntimeName(lower, "uvx") || containsRuntimeName(lower, "uv"):
		return "托管 uv 未就绪；launcher 会在运行时准备完成后自动刷新并重启 Agent"
	case containsRuntimeName(lower, "python") || containsRuntimeName(lower, "python3") || containsRuntimeName(lower, "pip"):
		return "托管 Python 未就绪；launcher 会在运行时准备完成后自动刷新并重启 Agent"
	case containsRuntimeName(lower, "java"):
		return "托管 Java 未就绪；launcher 会在检测到 Java MCP 后准备 JDK，并在完成后自动刷新并重启 Agent"
	default:
		return raw
	}
}

func containsRuntimeName(text string, name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	return strings.Contains(text, `"`+name+`"`) ||
		strings.Contains(text, "'"+name+"'") ||
		strings.Contains(text, " "+name+" ") ||
		strings.HasSuffix(text, " "+name)
}

func nextAgentID() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return "agent_" + hex.EncodeToString(buf)
}

func nextPort(agents []model.Agent) int {
	return nextAvailablePort(agents)
}

func nextAvailablePort(agents []model.Agent) int {
	port := 4096
	used := map[int]struct{}{}
	for _, agent := range agents {
		used[agent.Port] = struct{}{}
	}
	for {
		_, usedByAgent := used[port]
		if !usedByAgent && !portAcceptsTCP(port) {
			return port
		}
		port++
	}
}

func nextAvailableRecoveryPort(agents []model.Agent) (int, error) {
	used := map[int]struct{}{}
	for _, agent := range agents {
		used[agent.Port] = struct{}{}
	}
	for attempt := 0; attempt < 16; attempt++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		port := listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			return 0, err
		}
		if _, exists := used[port]; !exists {
			return port, nil
		}
	}
	return 0, errors.New("未找到可用恢复端口")
}

func replacePortArg(args []string, port int) []string {
	out := append([]string{}, args...)
	value := fmt.Sprintf("%d", port)
	for i := 0; i < len(out); i++ {
		if out[i] == "--port" {
			if i+1 < len(out) {
				out[i+1] = value
				return out
			}
			return append(out, value)
		}
		if strings.HasPrefix(out[i], "--port=") {
			out[i] = "--port=" + value
			return out
		}
	}
	return append(out, "--port", value)
}

func portAcceptsTCP(port int) bool {
	if port <= 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)), 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
