package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	urlpkg "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"launcher/internal/backgroundcmd"
	"launcher/internal/fileutil"
	"launcher/internal/mcpconfig"
	"launcher/internal/model"
	proc "launcher/internal/process"
	"launcher/internal/resumabledownload"
)

const (
	preflightStatusPending           = "pending"
	preflightStatusRunning           = "running"
	preflightStatusWaitingUserAction = "waiting_user_action"
	preflightStatusCompleted         = "completed"
	preflightStatusFailed            = "failed"
	preflightItemRuntime             = "runtime"
	preflightItemMCP                 = "mcp"
	preflightItemSkill               = "skill"
	preflightItemApply               = "apply"

	preflightReportBufferSize = 256
)

var runtimePlatformForPreflight = func() (string, string) {
	return runtime.GOOS, runtime.GOARCH
}

type preflightReporter func(model.RuntimePreflightStatusPayload) error

type preflightReport struct {
	payload model.RuntimePreflightStatusPayload
	done    chan struct{}
}

type runtimePreflightRunner struct {
	service      *service
	payload      model.RuntimePreflightStartPayload
	reporter     preflightReporter
	items        []model.RuntimeInstallJobItem
	itemMu       sync.Mutex
	progressMu   sync.Mutex
	lastProgress int
	reportCh     chan preflightReport
	reportDone   chan struct{}
	reportWg     sync.WaitGroup
	reportErrMu  sync.Mutex
	reportErr    error
}

type mcpPreflightTaskKind string

const (
	mcpPreflightTaskConfig     mcpPreflightTaskKind = "config"
	mcpPreflightTaskDependency mcpPreflightTaskKind = "dependency"
)

type mcpPreflightTask struct {
	index      int
	item       semanticPreflightMCPItem
	kind       mcpPreflightTaskKind
	server     model.DeviceMCPServerInfo
	dependency model.SemanticAgentMCPDependency
}

type mcpPreflightResult struct {
	index  int
	itemID string
	server model.DeviceMCPServerInfo
	err    error
}

type runtimePreflightTask struct {
	req          model.SemanticAgentRuntime
	item         model.RuntimeCatalogItem
	version      model.RuntimeVersion
	runtimeID    string
	progressBase int
}

type runtimePreflightTaskGroup struct {
	name  string
	tasks []runtimePreflightTask
}

type runtimeInstallCandidate struct {
	sourceType  string
	sourceID    string
	name        string
	url         string
	filename    string
	sha256      string
	kind        string
	priority    int
	headers     map[string]string
	timeout     time.Duration
	binPaths    []string
	envPatch    map[string]string
	extractRoot string
}

const (
	runtimeDownloadConnectTimeout = 15 * time.Second
	runtimeDownloadIdleTimeout    = 90 * time.Second
	runtimeDownloadMinTimeout     = 5 * time.Minute
	runtimeDownloadMaxTimeout     = 30 * time.Minute
	runtimeDownloadUserAgent      = "chat-codex-launcher/0.1"
	runtimeDownloadMaxAttempts    = 4
	runtimeDownloadRetryBaseDelay = 2 * time.Second
)

type installedRuntimeManifest struct {
	RuntimeID       string            `json:"runtime_id"`
	RuntimeName     string            `json:"runtime_name,omitempty"`
	VersionID       string            `json:"version_id"`
	Version         string            `json:"version"`
	Platform        string            `json:"platform"`
	Arch            string            `json:"arch"`
	InstallDir      string            `json:"install_dir"`
	BinPaths        []string          `json:"bin_paths,omitempty"`
	ExecutableNames []string          `json:"executable_names,omitempty"`
	EnvPatch        map[string]string `json:"env_patch,omitempty"`
	SourceType      string            `json:"source_type,omitempty"`
	SourceID        string            `json:"source_id,omitempty"`
	SHA256          string            `json:"sha256,omitempty"`
	InstalledAt     time.Time         `json:"installed_at"`
}

func (s *service) StartAgentSemanticPreflight(payload model.RuntimePreflightStartPayload, reporter func(model.RuntimePreflightStatusPayload) error) error {
	if reporter == nil {
		return errors.New("预检缺少状态上报器")
	}
	payload.LauncherAgentID = strings.TrimSpace(payload.LauncherAgentID)
	payload.SemanticAgentID = normalizeSemanticAgentID(payload.SemanticAgentID)
	if payload.JobID == "" {
		return errors.New("预检任务缺少 job_id")
	}
	if payload.LauncherAgentID == "" || !s.hasAgent(payload.LauncherAgentID) {
		return proc.ErrAgentNotFound
	}
	if payload.SemanticAgent == nil || strings.TrimSpace(payload.SemanticAgent.ID) == "" {
		return errors.New("预检缺少语义 Agent 配置快照")
	}
	if payload.SemanticAgent.ID != payload.SemanticAgentID {
		return errors.New("语义 Agent 配置快照与目标 ID 不一致")
	}
	if !s.semanticPreflightMu.TryLock() {
		if err := reporter(model.RuntimePreflightStatusPayload{
			JobID:           payload.JobID,
			MachineID:       payload.MachineID,
			LauncherAgentID: payload.LauncherAgentID,
			SemanticAgentID: payload.SemanticAgentID,
			Status:          preflightStatusRunning,
			CurrentStep:     "runtime",
			ProgressPercent: 3,
			Events: []model.RuntimeInstallEvent{{
				ItemType:        preflightItemRuntime,
				Phase:           "queued",
				Status:          "info",
				Message:         "当前已有 Agent 切换任务，已进入队列",
				ProgressPercent: 3,
			}},
		}); err != nil {
			log.Printf("[launcher][preflight] queued status report failed: %v", err)
		}
		s.semanticPreflightMu.Lock()
	}
	defer s.semanticPreflightMu.Unlock()
	runner := runtimePreflightRunner{
		service:  s,
		payload:  payload,
		reporter: reporter,
		items:    buildPreflightItems(payload),
	}
	runner.startReporter()
	defer runner.stopReporter()
	return runner.run()
}

func (s *service) applySemanticRuntimeEnv(cfg model.AgentConfig, env map[string]string) {
	if env == nil || cfg.SemanticAgentProfile == nil {
		return
	}
	var pathDirs []string
	for _, req := range cfg.SemanticAgentProfile.RuntimeRequirements {
		runtimeID := strings.TrimSpace(req.RuntimeID)
		if runtimeID == "" {
			continue
		}
		manifest, ok := s.bestRuntimeManifest(runtimeID, req.VersionConstraint)
		if !ok {
			continue
		}
		pathDirs = append(pathDirs, runtimeManifestBinDirs(manifest)...)
		for key, value := range manifest.EnvPatch {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			env[key] = value
		}
		switch runtimeID {
		case "java":
			env["JAVA_HOME"] = manifest.InstallDir
		}
	}
	if len(pathDirs) == 0 {
		return
	}
	pathDirs = uniqueStrings(pathDirs)
	key := pathKey()
	current := strings.TrimSpace(env[key])
	joined := strings.Join(pathDirs, string(os.PathListSeparator))
	if current != "" {
		joined += string(os.PathListSeparator) + current
	}
	env[key] = joined
	env["LAUNCHER_MANAGED_RUNTIME_DIR"] = filepath.Join(s.cfg.RuntimeDir, "runtimes")
}

func (s *service) bestRuntimeManifest(runtimeID string, constraint string) (installedRuntimeManifest, bool) {
	dir := filepath.Join(s.cfg.RuntimeDir, "runtime-manifests")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return installedRuntimeManifest{}, false
	}
	var manifests []installedRuntimeManifest
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var manifest installedRuntimeManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		platform, arch := runtimePlatformForPreflight()
		if manifest.RuntimeID != runtimeID || manifest.Platform != platform || manifest.Arch != arch {
			continue
		}
		if !runtimeVersionMatches(manifest.Version, constraint) {
			continue
		}
		if strings.TrimSpace(manifest.InstallDir) == "" || !isManagedRuntimeInstallDir(s.cfg.RuntimeDir, manifest) || !pathExists(manifest.InstallDir) {
			continue
		}
		manifests = append(manifests, manifest)
	}
	if len(manifests) == 0 {
		return installedRuntimeManifest{}, false
	}
	sort.SliceStable(manifests, func(i, j int) bool {
		return compareVersionParts(versionParts(manifests[i].Version), versionParts(manifests[j].Version)) > 0
	})
	return manifests[0], true
}

func (r *runtimePreflightRunner) run() error {
	r.emit(preflightStatusRunning, "runtime", 3, nil)
	if err := r.ensureRuntimes(); err != nil {
		r.fail("runtime", err)
		r.flushReports()
		return err
	}
	if err := r.ensureMCPs(); err != nil {
		r.fail("mcp", err)
		r.flushReports()
		return err
	}
	if err := r.ensureSkills(); err != nil {
		r.fail("skill", err)
		r.flushReports()
		return err
	}
	selection, err := r.applySemanticAgent()
	if err != nil {
		r.fail("apply", err)
		r.flushReports()
		return err
	}
	r.emit(preflightStatusCompleted, "completed", 100, []model.RuntimeInstallEvent{{
		ItemType:        "job",
		Phase:           "completed",
		Status:          "success",
		Message:         "语义 Agent 预检与切换完成",
		ProgressPercent: 100,
	}})
	r.enqueueReport(model.RuntimePreflightStatusPayload{
		JobID:             r.payload.JobID,
		MachineID:         r.payload.MachineID,
		LauncherAgentID:   r.payload.LauncherAgentID,
		SemanticAgentID:   r.payload.SemanticAgentID,
		Status:            preflightStatusCompleted,
		CurrentStep:       "completed",
		ProgressPercent:   100,
		Items:             r.itemsSnapshot(),
		SemanticSelection: &selection,
	})
	r.flushReports()
	if reportErr := r.reporterError(); reportErr != nil {
		return fmt.Errorf("预检最终状态上报失败：%w", reportErr)
	}
	return nil
}

func (r *runtimePreflightRunner) ensureRuntimes() error {
	requirements := r.payload.SemanticAgent.RuntimeRequirements
	if len(requirements) == 0 {
		r.emit(preflightStatusRunning, "runtime", 20, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemRuntime,
			Phase:           "skip",
			Status:          "info",
			Message:         "当前语义 Agent 没有声明托管运行时依赖",
			ProgressPercent: 20,
		}})
		return nil
	}
	runtimes := map[string]model.RuntimeCatalogItem{}
	for _, item := range r.payload.Runtimes {
		runtimes[strings.TrimSpace(item.ID)] = item
	}
	tasks := make([]runtimePreflightTask, 0, len(requirements))
	var requiredFailures []string
	for index, req := range requirements {
		runtimeID := strings.TrimSpace(req.RuntimeID)
		if runtimeID == "" {
			continue
		}
		item, ok := runtimes[runtimeID]
		if !ok {
			err := fmt.Errorf("运行时 %s 未随任务下发，无法检测安装", runtimeID)
			r.updateItem(preflightItemRuntime, runtimeID, preflightStatusFailed, 100, err.Error())
			if req.Required {
				requiredFailures = append(requiredFailures, err.Error())
			}
			continue
		}
		version, err := selectRuntimeVersion(item, req.VersionConstraint)
		if err != nil {
			r.updateItem(preflightItemRuntime, runtimeID, preflightStatusFailed, 100, err.Error())
			if req.Required {
				requiredFailures = append(requiredFailures, err.Error())
			}
			continue
		}
		progressBase := 5 + index*30/max(1, len(requirements))
		tasks = append(tasks, runtimePreflightTask{
			req:          req,
			item:         item,
			version:      version,
			runtimeID:    runtimeID,
			progressBase: progressBase,
		})
	}
	tasks, requiredFailures = r.expandRuntimeTaskDependencies(tasks, runtimes, requirements, requiredFailures)
	if len(requiredFailures) > 0 {
		return errors.New(strings.Join(requiredFailures, "；"))
	}
	if len(tasks) == 0 {
		return nil
	}
	groups := runtimeTaskGroups(tasks)
	for _, group := range groups {
		if len(group.tasks) == 0 {
			continue
		}
		r.emit(preflightStatusRunning, "runtime", 6, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemRuntime,
			Phase:           "parallel_start",
			Status:          "running",
			Message:         fmt.Sprintf("开始检测 %d 个运行时", len(group.tasks)),
			ProgressPercent: 6,
			Details: map[string]any{
				"group":         group.name,
				"runtime_count": len(group.tasks),
				"concurrency":   runtimeGroupConcurrency(group),
			},
		}})
		errCh := make(chan string, len(group.tasks))
		var wg sync.WaitGroup
		sem := make(chan struct{}, runtimeGroupConcurrency(group))
		for _, task := range group.tasks {
			task := task
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if err := r.ensureRuntimeTask(task); err != nil {
					if task.req.Required {
						errCh <- err.Error()
					}
				}
			}()
		}
		wg.Wait()
		close(errCh)
		for errText := range errCh {
			requiredFailures = append(requiredFailures, errText)
		}
		if len(requiredFailures) > 0 {
			return errors.New(strings.Join(requiredFailures, "；"))
		}
	}
	r.emit(preflightStatusRunning, "runtime", 45, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemRuntime,
		Phase:           "parallel_completed",
		Status:          "success",
		Message:         "运行时并行检测与修补完成",
		ProgressPercent: 45,
	}})
	return nil
}

func (r *runtimePreflightRunner) expandRuntimeTaskDependencies(tasks []runtimePreflightTask, runtimes map[string]model.RuntimeCatalogItem, requirements []model.SemanticAgentRuntime, requiredFailures []string) ([]runtimePreflightTask, []string) {
	needsUV := false
	needsPython := false
	needsJava := false
	needsNode := false
	hasUVTask := false
	hasPythonTask := false
	hasJavaTask := false
	hasNodeTask := false
	for _, task := range tasks {
		if task.runtimeID == "uv" {
			hasUVTask = true
		}
		if task.runtimeID == "python" {
			hasPythonTask = true
		}
		if task.runtimeID == "java" {
			hasJavaTask = true
		}
		if task.runtimeID == "node" {
			hasNodeTask = true
		}
		if normalizeRuntimeInstallStrategy(task.item.InstallStrategy) == "uv_python" {
			needsUV = true
		}
		if normalizeRuntimeInstallStrategy(task.item.InstallStrategy) == "npm_global_tool" {
			needsNode = true
		}
		if normalizeRuntimeInstallStrategy(task.item.InstallStrategy) == "ida_installer" {
			needsPython = true
		}
		strategy := normalizeRuntimeInstallStrategy(task.item.InstallStrategy)
		if strategy == "java_tool_archive" || strategy == "java_jar" {
			needsJava = true
		}
	}
	if needsUV && !hasUVTask {
		uvItem, ok := runtimes["uv"]
		if !ok {
			err := errors.New("Python 使用 uv_python 安装策略，但运行时快照未下发 uv")
			r.updateItem(preflightItemRuntime, "uv", preflightStatusFailed, 100, err.Error())
			return tasks, append(requiredFailures, err.Error())
		}
		version, err := selectRuntimeVersion(uvItem, "")
		if err != nil {
			r.updateItem(preflightItemRuntime, "uv", preflightStatusFailed, 100, err.Error())
			return tasks, append(requiredFailures, err.Error())
		}
		progressBase := 5 + len(requirements)*30/max(1, len(requirements)+1)
		tasks = append(tasks, runtimePreflightTask{
			req: model.SemanticAgentRuntime{
				RuntimeID: "uv",
				Required:  true,
				Purpose:   "Python uv_python 安装策略依赖",
			},
			item:         uvItem,
			version:      version,
			runtimeID:    "uv",
			progressBase: progressBase,
		})
	}
	if needsPython && !hasPythonTask {
		pythonItem, ok := runtimes["python"]
		if !ok {
			err := errors.New("IDA 安装策略依赖 Python，但运行时快照未下发 python")
			r.updateItem(preflightItemRuntime, "python", preflightStatusFailed, 100, err.Error())
			return tasks, append(requiredFailures, err.Error())
		}
		version, err := selectRuntimeVersion(pythonItem, ">=3.12 <3.15")
		if err != nil {
			r.updateItem(preflightItemRuntime, "python", preflightStatusFailed, 100, err.Error())
			return tasks, append(requiredFailures, err.Error())
		}
		progressBase := 5 + len(tasks)*30/max(1, len(tasks)+1)
		tasks = append(tasks, runtimePreflightTask{
			req: model.SemanticAgentRuntime{
				RuntimeID: "python",
				Required:  true,
				Purpose:   "IDA 安装后许可证生成依赖",
			},
			item:         pythonItem,
			version:      version,
			runtimeID:    "python",
			progressBase: progressBase,
		})
	}
	if needsJava && !hasJavaTask {
		javaItem, ok := runtimes["java"]
		if !ok {
			err := errors.New("Android Java 工具安装策略依赖 java，但运行时快照未下发 java")
			r.updateItem(preflightItemRuntime, "java", preflightStatusFailed, 100, err.Error())
			return tasks, append(requiredFailures, err.Error())
		}
		version, err := selectRuntimeVersion(javaItem, ">=17 <22")
		if err != nil {
			r.updateItem(preflightItemRuntime, "java", preflightStatusFailed, 100, err.Error())
			return tasks, append(requiredFailures, err.Error())
		}
		tasks = append(tasks, runtimePreflightTask{
			req: model.SemanticAgentRuntime{
				RuntimeID: "java",
				Required:  true,
				Purpose:   "JADX/Apktool 安装策略依赖",
			},
			item:         javaItem,
			version:      version,
			runtimeID:    "java",
			progressBase: 5 + len(tasks)*30/max(1, len(tasks)+1),
		})
	}
	if needsNode && !hasNodeTask {
		nodeItem, ok := runtimes["node"]
		if !ok {
			err := errors.New("npm_global_tool 安装策略依赖 Node.js，但运行时快照未下发 node")
			r.updateItem(preflightItemRuntime, "node", preflightStatusFailed, 100, err.Error())
			return tasks, append(requiredFailures, err.Error())
		}
		version, err := selectRuntimeVersion(nodeItem, "")
		if err != nil {
			r.updateItem(preflightItemRuntime, "node", preflightStatusFailed, 100, err.Error())
			return tasks, append(requiredFailures, err.Error())
		}
		tasks = append(tasks, runtimePreflightTask{
			req: model.SemanticAgentRuntime{
				RuntimeID: "node",
				Required:  true,
				Purpose:   "Git npm_global_tool 安装策略依赖",
			},
			item:         nodeItem,
			version:      version,
			runtimeID:    "node",
			progressBase: 5 + len(tasks)*30/max(1, len(tasks)+1),
		})
	}
	return tasks, requiredFailures
}

func runtimeTaskGroups(tasks []runtimePreflightTask) []runtimePreflightTaskGroup {
	var uvTasks []runtimePreflightTask
	var archiveTasks []runtimePreflightTask
	var javaToolTasks []runtimePreflightTask
	var javaJarTasks []runtimePreflightTask
	var pythonTasks []runtimePreflightTask
	var idaTasks []runtimePreflightTask
	var npmToolTasks []runtimePreflightTask
	for _, task := range tasks {
		strategy := normalizeRuntimeInstallStrategy(task.item.InstallStrategy)
		if strategy == "uv_python" {
			pythonTasks = append(pythonTasks, task)
			continue
		}
		if strategy == "npm_global_tool" {
			npmToolTasks = append(npmToolTasks, task)
			continue
		}
		if strategy == "ida_installer" {
			idaTasks = append(idaTasks, task)
			continue
		}
		if strategy == "java_jar" {
			javaJarTasks = append(javaJarTasks, task)
			continue
		}
		if strategy == "java_tool_archive" {
			javaToolTasks = append(javaToolTasks, task)
			continue
		}
		if task.runtimeID == "uv" {
			uvTasks = append(uvTasks, task)
			continue
		}
		archiveTasks = append(archiveTasks, task)
	}
	groups := make([]runtimePreflightTaskGroup, 0, 4)
	if len(uvTasks) > 0 {
		groups = append(groups, runtimePreflightTaskGroup{name: "uv", tasks: uvTasks})
	}
	if len(archiveTasks) > 0 {
		groups = append(groups, runtimePreflightTaskGroup{name: "archive", tasks: archiveTasks})
	}
	if len(javaToolTasks) > 0 {
		groups = append(groups, runtimePreflightTaskGroup{name: "java_tool", tasks: javaToolTasks})
	}
	if len(javaJarTasks) > 0 {
		groups = append(groups, runtimePreflightTaskGroup{name: "java_jar", tasks: javaJarTasks})
	}
	if len(pythonTasks) > 0 {
		groups = append(groups, runtimePreflightTaskGroup{name: "uv_python", tasks: pythonTasks})
	}
	if len(idaTasks) > 0 {
		groups = append(groups, runtimePreflightTaskGroup{name: "ida_installer", tasks: idaTasks})
	}
	if len(npmToolTasks) > 0 {
		groups = append(groups, runtimePreflightTaskGroup{name: "npm_global_tool", tasks: npmToolTasks})
	}
	return groups
}

func runtimeGroupConcurrency(group runtimePreflightTaskGroup) int {
	if len(group.tasks) <= 1 {
		return 1
	}
	if group.name == "archive" {
		return runtimeArchiveConcurrency()
	}
	return len(group.tasks)
}

func runtimeArchiveConcurrency() int {
	value := strings.TrimSpace(os.Getenv("LAUNCHER_RUNTIME_ARCHIVE_CONCURRENCY"))
	if value == "" {
		return 1
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 1
	}
	if parsed > 4 {
		return 4
	}
	return parsed
}

func (r *runtimePreflightRunner) ensureRuntimeTask(task runtimePreflightTask) error {
	runtimeID := task.runtimeID
	item := task.item
	version := task.version
	progressBase := task.progressBase
	r.updateItem(preflightItemRuntime, runtimeID, preflightStatusRunning, 10, "")
	r.emit(preflightStatusRunning, "runtime", progressBase, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemRuntime,
		ItemID:          runtimeID,
		Phase:           "check",
		Status:          "info",
		Message:         fmt.Sprintf("检测托管运行时 %s %s", item.Name, version.Version),
		ProgressPercent: progressBase,
	}})
	manifest, ready := r.detectRuntime(item, version)
	if ready {
		r.updateItem(preflightItemRuntime, runtimeID, preflightStatusCompleted, 100, "")
		r.emit(preflightStatusRunning, "runtime", progressBase+10, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemRuntime,
			ItemID:          runtimeID,
			Phase:           "ready",
			Status:          "success",
			Message:         fmt.Sprintf("托管运行时已就绪：%s", manifest.InstallDir),
			ProgressPercent: progressBase + 10,
		}})
		r.markRuntimeTaskProgress(progressBase + 25)
		return nil
	}
	if !r.payload.AutoRepair {
		err := fmt.Errorf("托管运行时 %s %s 未安装", item.Name, version.Version)
		r.updateItem(preflightItemRuntime, runtimeID, preflightStatusFailed, 100, err.Error())
		r.markRuntimeTaskProgress(progressBase + 25)
		return err
	}
	manifest, err := r.installRuntime(item, version, progressBase)
	if err != nil {
		r.updateItem(preflightItemRuntime, runtimeID, preflightStatusFailed, 100, err.Error())
		r.markRuntimeTaskProgress(progressBase + 25)
		return err
	}
	if err := r.verifyRuntimeManifest(manifest); err != nil {
		r.updateItem(preflightItemRuntime, runtimeID, preflightStatusFailed, 100, err.Error())
		r.markRuntimeTaskProgress(progressBase + 25)
		return err
	}
	r.updateItem(preflightItemRuntime, runtimeID, preflightStatusCompleted, 100, "")
	r.emit(preflightStatusRunning, "runtime", progressBase+25, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemRuntime,
		ItemID:          runtimeID,
		Phase:           "installed",
		Status:          "success",
		Message:         fmt.Sprintf("托管运行时安装完成：%s", manifest.InstallDir),
		ProgressPercent: progressBase + 25,
	}})
	r.markRuntimeTaskProgress(progressBase + 25)
	return nil
}

func (r *runtimePreflightRunner) detectRuntime(item model.RuntimeCatalogItem, version model.RuntimeVersion) (installedRuntimeManifest, bool) {
	manifestPath := runtimeManifestPath(r.service.cfg.RuntimeDir, item.ID, version.Version)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return installedRuntimeManifest{}, false
	}
	var manifest installedRuntimeManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return installedRuntimeManifest{}, false
	}
	if strings.TrimSpace(manifest.InstallDir) == "" {
		return installedRuntimeManifest{}, false
	}
	if !isManagedRuntimeInstallDir(r.service.cfg.RuntimeDir, manifest) {
		return installedRuntimeManifest{}, false
	}
	if manifest.RuntimeID == "python" {
		normalized := pythonRuntimeExecutableNames(manifest.InstallDir, manifest.BinPaths, manifest.ExecutableNames)
		if len(normalized) > 0 && !sameStringSet(normalized, manifest.ExecutableNames) {
			manifest.ExecutableNames = normalized
			_ = writeRuntimeManifest(r.service.cfg.RuntimeDir, manifest)
		}
	}
	if err := r.verifyRuntimeManifest(manifest); err != nil {
		r.emit(preflightStatusRunning, "runtime", 12, []model.RuntimeInstallEvent{{
			ItemType: preflightItemRuntime,
			ItemID:   item.ID,
			Phase:    "verify",
			Status:   "warning",
			Message:  "已有托管运行时验证失败，将重新修补：" + err.Error(),
		}})
		return installedRuntimeManifest{}, false
	}
	return manifest, true
}

func (r *runtimePreflightRunner) installRuntime(item model.RuntimeCatalogItem, version model.RuntimeVersion, progressBase int) (installedRuntimeManifest, error) {
	strategy := normalizeRuntimeInstallStrategy(item.InstallStrategy)
	if strategy == "uv_python" {
		return r.installPythonRuntimeWithUV(item, version, progressBase)
	}
	if strategy == "ida_installer" {
		return r.installIDARuntime(item, version, progressBase)
	}
	if strategy == "java_jar" {
		return r.installJavaJarRuntime(item, version, progressBase)
	}
	if strategy == "npm_global_tool" {
		return r.installNpmGlobalTool(item, version, progressBase)
	}
	if strategy != "archive" && strategy != "java_tool_archive" {
		return installedRuntimeManifest{}, fmt.Errorf("运行时 %s 使用安装策略 %s，当前预检只支持 archive 策略", item.ID, strategy)
	}
	candidates := r.runtimeCandidates(item, version)
	if len(candidates) == 0 {
		return installedRuntimeManifest{}, fmt.Errorf("运行时 %s %s 缺少当前平台 %s/%s 的可验证镜像源或平台包", item.ID, version.Version, currentRuntimePlatform(), currentRuntimeArch())
	}
	var failures []string
	for _, candidate := range candidates {
		event := model.RuntimeInstallEvent{
			ItemType:        preflightItemRuntime,
			ItemID:          item.ID,
			Phase:           "download",
			Status:          "info",
			Message:         fmt.Sprintf("尝试下载 %s：%s", candidate.name, candidate.url),
			ProgressPercent: progressBase + 5,
			Details: map[string]any{
				"source_type": candidate.sourceType,
				"source_id":   candidate.sourceID,
				"url":         candidate.url,
			},
		}
		r.emit(preflightStatusRunning, "runtime", progressBase+5, []model.RuntimeInstallEvent{event})
		if strings.TrimSpace(candidate.sha256) == "" {
			err := fmt.Errorf("%s 缺少 sha256，已拒绝安装", candidate.name)
			failures = append(failures, err.Error())
			continue
		}
		archivePath := filepath.Join(r.service.cfg.RuntimeDir, "downloads", "runtimes", item.ID, version.Version, candidate.filename)
		if err := r.downloadRuntimeCandidate(candidate, archivePath, item.ID, progressBase); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate.name, err))
			r.emit(preflightStatusRunning, "runtime", progressBase+8, []model.RuntimeInstallEvent{{
				ItemType:        preflightItemRuntime,
				ItemID:          item.ID,
				Phase:           "download",
				Status:          "warning",
				Message:         fmt.Sprintf("%s 下载失败：%v", candidate.name, err),
				ProgressPercent: progressBase + 8,
				Details: map[string]any{
					"source_type": candidate.sourceType,
					"source_id":   candidate.sourceID,
					"url":         candidate.url,
				},
			}})
			continue
		}
		manifest, err := r.extractRuntimeCandidate(item, version, candidate, archivePath, progressBase)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate.name, err))
			continue
		}
		return manifest, nil
	}
	return installedRuntimeManifest{}, fmt.Errorf("运行时 %s %s 安装失败：%s", item.ID, version.Version, strings.Join(failures, "；"))
}

func normalizeRuntimeInstallStrategy(strategy string) string {
	strategy = strings.ToLower(strings.TrimSpace(strategy))
	if strategy == "" {
		return "archive"
	}
	return strategy
}

func (r *runtimePreflightRunner) installPythonRuntimeWithUV(item model.RuntimeCatalogItem, version model.RuntimeVersion, progressBase int) (installedRuntimeManifest, error) {
	uvManifest, ok := r.service.bestRuntimeManifest("uv", "")
	if !ok {
		return installedRuntimeManifest{}, errors.New("Python 使用 uv_python 安装策略，但托管 uv 尚未安装完成")
	}
	envPath := strings.Join(runtimeManifestBinDirs(uvManifest), string(os.PathListSeparator))
	uvPath, err := lookPathInDirs("uv", envPath)
	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("Python 使用 uv_python 安装策略，但未找到托管 uv 可执行文件：%w", err)
	}
	installParent := filepath.Join(r.service.cfg.RuntimeDir, "runtimes", item.ID)
	targetDir := filepath.Join(installParent, version.Version)
	if err := os.MkdirAll(installParent, 0o755); err != nil {
		return installedRuntimeManifest{}, err
	}
	mirror := r.pythonInstallMirror(item, version)
	r.emit(preflightStatusRunning, "runtime", progressBase+5, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemRuntime,
		ItemID:          item.ID,
		Phase:           "install",
		Status:          "running",
		Message:         fmt.Sprintf("正在通过 uv 安装 Python %s", version.Version),
		ProgressPercent: progressBase + 5,
		Details: map[string]any{
			"strategy":    "uv_python",
			"uv":          uvPath,
			"install_dir": installParent,
			"mirror":      mirror,
		},
	}})
	args := []string{"python", "install", version.Version, "--install-dir", installParent, "--default", "--managed-python", "--native-tls"}
	if mirror != "" {
		args = append(args, "--mirror", mirror)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, uvPath, args...)
	backgroundcmd.Configure(cmd)
	cmd.Env = r.pythonInstallEnv(envPath, installParent, mirror)
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return installedRuntimeManifest{}, fmt.Errorf("uv python install 执行超时；输出：%s", summarizeCommandOutput(string(out)))
	}
	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("uv python install 失败：%v；输出：%s", err, summarizeCommandOutput(string(out)))
	}
	pythonRoot, binPaths := findUVManagedPythonRoot(installParent, version.Version)
	if pythonRoot == "" {
		return installedRuntimeManifest{}, fmt.Errorf("uv python install 完成，但未在 %s 找到 Python %s 可执行文件", installParent, version.Version)
	}
	if !sameCleanPath(pythonRoot, targetDir) {
		_ = os.RemoveAll(targetDir)
		if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
			return installedRuntimeManifest{}, err
		}
		if err := os.Rename(pythonRoot, targetDir); err != nil {
			return installedRuntimeManifest{}, err
		}
	}
	envPatch := map[string]string{
		"PIP_CACHE_DIR":         filepath.Join(r.service.cfg.RuntimeDir, "caches", "pip"),
		"PIP_INDEX_URL":         firstPythonIndexURL(),
		"UV_INDEX_URL":          firstPythonIndexURL(),
		"UV_DEFAULT_INDEX":      firstPythonIndexURL(),
		"UV_CACHE_DIR":          filepath.Join(r.service.cfg.RuntimeDir, "caches", "uv"),
		"UV_TOOL_DIR":           filepath.Join(r.service.cfg.RuntimeDir, "runtimes", "uv-tools"),
		"UV_PYTHON_DOWNLOADS":   "automatic",
		"UV_MANAGED_PYTHON":     "1",
		"UV_PYTHON_INSTALL_DIR": installParent,
	}
	if strings.TrimSpace(mirror) != "" {
		envPatch["UV_PYTHON_INSTALL_MIRROR"] = mirror
	}
	manifest := installedRuntimeManifest{
		RuntimeID:       item.ID,
		RuntimeName:     item.Name,
		VersionID:       version.ID,
		Version:         version.Version,
		Platform:        currentRuntimePlatform(),
		Arch:            currentRuntimeArch(),
		InstallDir:      targetDir,
		BinPaths:        binPaths,
		ExecutableNames: pythonRuntimeExecutableNames(targetDir, binPaths, item.ExecutableNames),
		EnvPatch:        envPatch,
		SourceType:      "uv_python",
		SourceID:        firstNonEmpty(mirror, "uv-python-install"),
		InstalledAt:     time.Now().UTC(),
	}
	if len(manifest.BinPaths) == 0 {
		manifest.BinPaths = inferRuntimeBinPaths(targetDir)
	}
	if len(manifest.ExecutableNames) == 0 {
		manifest.ExecutableNames = defaultPythonExecutableNames()
	}
	if err := writeRuntimeManifest(r.service.cfg.RuntimeDir, manifest); err != nil {
		return installedRuntimeManifest{}, err
	}
	r.emit(preflightStatusRunning, "runtime", progressBase+20, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemRuntime,
		ItemID:          item.ID,
		Phase:           "install",
		Status:          "success",
		Message:         "uv 托管 Python 安装完成",
		ProgressPercent: progressBase + 20,
		Details: map[string]any{
			"install_dir": targetDir,
			"output":      summarizeCommandOutput(string(out)),
		},
	}})
	return manifest, nil
}

func (r *runtimePreflightRunner) installNpmGlobalTool(item model.RuntimeCatalogItem, version model.RuntimeVersion, progressBase int) (installedRuntimeManifest, error) {
	// 1. 检查Node.js是否已安装
	nodeManifest, ok := r.service.bestRuntimeManifest("node", "")
	if !ok {
		return installedRuntimeManifest{}, errors.New("Git 使用 npm_global_tool 安装策略，但 Node.js 尚未安装")
	}

	// 2. 获取npm路径
	envPath := strings.Join(runtimeManifestBinDirs(nodeManifest), string(os.PathListSeparator))
	npmPath, err := lookPathInDirs("npm", envPath)
	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("未找到 npm 可执行文件：%w", err)
	}

	// 3. 读取npm包名和依赖runtime
	npmPackage := ""
	requiresRuntime := ""
	if manifest, ok := version.InstallManifest["npm_package"].(string); ok {
		npmPackage = manifest
	}
	if requires, ok := version.InstallManifest["requires_runtime"].(string); ok {
		requiresRuntime = requires
	}
	if npmPackage == "" {
		return installedRuntimeManifest{}, errors.New("install_manifest 中缺少 npm_package 字段")
	}
	if requiresRuntime != "node" {
		return installedRuntimeManifest{}, fmt.Errorf("npm_global_tool 策略要求 requires_runtime 为 node，当前为：%s", requiresRuntime)
	}

	// 4. 创建安装目录
	installParent := filepath.Join(r.service.cfg.RuntimeDir, "runtimes", item.ID)
	targetDir := filepath.Join(installParent, version.Version)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return installedRuntimeManifest{}, err
	}

	// 5. 通过npm安装
	r.emit(preflightStatusRunning, "runtime", progressBase+5, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemRuntime,
		ItemID:          item.ID,
		Phase:           "install",
		Status:          "running",
		Message:         fmt.Sprintf("正在通过 npm 安装 %s", npmPackage),
		ProgressPercent: progressBase + 5,
		Details: map[string]any{
			"strategy":    "npm_global_tool",
			"npm_package": npmPackage,
		},
	}})

	args := []string{"install", "--prefix", targetDir, "--no-save", npmPackage}

	// 支持npm镜像配置
	npmRegistry := os.Getenv("LAUNCHER_NPM_REGISTRY")
	if npmRegistry == "" {
		npmRegistry = os.Getenv("NPM_CONFIG_REGISTRY")
	}
	if npmRegistry != "" {
		args = append(args, "--registry", npmRegistry)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, npmPath, args...)
	cmd.Env = append(os.Environ(), "PATH="+envPath)
	out, err := cmd.CombinedOutput()

	if err != nil {
		return installedRuntimeManifest{}, fmt.Errorf("npm install 失败：%v；输出：%s", err, string(out))
	}

	// 6. 查找安装的可执行文件
	binPaths := []string{
		filepath.Join("node_modules", ".bin"),
		filepath.Join("node_modules", npmPackage, "bin"),
	}

	// 7. 创建manifest
	manifest := installedRuntimeManifest{
		RuntimeID:       item.ID,
		RuntimeName:     item.Name,
		VersionID:       version.ID,
		Version:         version.Version,
		Platform:        currentRuntimePlatform(),
		Arch:            currentRuntimeArch(),
		InstallDir:      targetDir,
		BinPaths:        binPaths,
		ExecutableNames: item.ExecutableNames,
		EnvPatch:        map[string]string{},
		SourceType:      "npm",
		SourceID:        npmPackage,
		InstalledAt:     time.Now().UTC(),
	}

	if err := writeRuntimeManifest(r.service.cfg.RuntimeDir, manifest); err != nil {
		return installedRuntimeManifest{}, err
	}

	r.emit(preflightStatusRunning, "runtime", progressBase+20, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemRuntime,
		ItemID:          item.ID,
		Phase:           "install",
		Status:          "success",
		Message:         fmt.Sprintf("npm 安装 %s 完成", npmPackage),
		ProgressPercent: progressBase + 20,
		Details: map[string]any{
			"install_dir": targetDir,
			"output":      summarizeCommandOutput(string(out)),
		},
	}})

	return manifest, nil
}

func (r *runtimePreflightRunner) pythonInstallMirror(item model.RuntimeCatalogItem, version model.RuntimeVersion) string {
	type mirrorCandidate struct {
		url      string
		priority int
		id       string
	}
	var candidates []mirrorCandidate
	for _, mirror := range item.Mirrors {
		if !mirror.Enabled || !matchesRuntimeVersion(mirror.RuntimeID, mirror.VersionID, item.ID, version.ID) || !matchesPlatformArch(mirror.Platform, mirror.Arch) {
			continue
		}
		if !isPythonInstallMirror(mirror) {
			continue
		}
		base := strings.TrimRight(strings.TrimSpace(mirror.BaseURL), "/")
		if base == "" {
			continue
		}
		candidates = append(candidates, mirrorCandidate{url: base, priority: mirror.Priority, id: mirror.ID})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		return candidates[i].id < candidates[j].id
	})
	if len(candidates) > 0 {
		return candidates[0].url
	}
	return os.Getenv("LAUNCHER_PYTHON_INSTALL_MIRROR")
}

func isPythonInstallMirror(mirror model.RuntimeMirror) bool {
	if hasTag(mirror.Tags, "registry") {
		return false
	}
	joined := strings.ToLower(strings.Join([]string{
		mirror.ID,
		mirror.Name,
		mirror.BaseURL,
		mirror.URLTemplate,
		strings.Join(mirror.Tags, " "),
	}, " "))
	if strings.Contains(joined, "pypi") || strings.Contains(joined, "/simple") {
		return false
	}
	return strings.Contains(joined, "python-build-standalone") ||
		strings.Contains(joined, "standalone") ||
		strings.Contains(joined, "python-install")
}

func hasTag(tags []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, tag := range tags {
		if strings.ToLower(strings.TrimSpace(tag)) == target {
			return true
		}
	}
	return false
}

func (r *runtimePreflightRunner) pythonInstallEnv(envPath string, installParent string, mirror string) []string {
	envMap := map[string]string{}
	for _, value := range os.Environ() {
		if key, val, ok := strings.Cut(value, "="); ok {
			envMap[key] = val
		}
	}
	key := pathKey()
	if current := strings.TrimSpace(envMap[key]); current != "" {
		envMap[key] = envPath + string(os.PathListSeparator) + current
	} else {
		envMap[key] = envPath
	}
	pythonIndex := firstPythonIndexURL()
	envMap["PIP_INDEX_URL"] = pythonIndex
	envMap["UV_INDEX_URL"] = pythonIndex
	envMap["UV_DEFAULT_INDEX"] = pythonIndex
	envMap["PIP_CACHE_DIR"] = filepath.Join(r.service.cfg.RuntimeDir, "caches", "pip")
	envMap["UV_CACHE_DIR"] = filepath.Join(r.service.cfg.RuntimeDir, "caches", "uv")
	envMap["UV_TOOL_DIR"] = filepath.Join(r.service.cfg.RuntimeDir, "runtimes", "uv-tools")
	envMap["UV_PYTHON_INSTALL_DIR"] = installParent
	envMap["UV_PYTHON_DOWNLOADS"] = "automatic"
	envMap["UV_MANAGED_PYTHON"] = "1"
	if strings.TrimSpace(mirror) != "" {
		envMap["UV_PYTHON_INSTALL_MIRROR"] = mirror
	}
	out := make([]string, 0, len(envMap))
	for key, value := range envMap {
		out = append(out, key+"="+value)
	}
	sort.Strings(out)
	return out
}

func firstPythonIndexURL() string {
	for _, key := range []string{"LAUNCHER_PYTHON_INDEX_URL", "PIP_INDEX_URL", "UV_INDEX_URL", "UV_DEFAULT_INDEX"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return "https://pypi.tuna.tsinghua.edu.cn/simple"
}

func findUVManagedPythonRoot(installParent string, version string) (string, []string) {
	installParent = filepath.Clean(strings.TrimSpace(installParent))
	if installParent == "" {
		return "", nil
	}
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	type candidate struct {
		root string
		bin  string
	}
	var candidates []candidate
	_ = filepath.WalkDir(installParent, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if name != "python" && name != "python.exe" && name != "python3" && name != "python3.exe" {
			return nil
		}
		binDir := filepath.Dir(path)
		binBase := filepath.Base(binDir)
		root := filepath.Dir(binDir)
		binPath := binBase
		if binBase != "bin" && binBase != "Scripts" {
			root = binDir
			binPath = "."
		}
		if !isPathUnder(installParent, root) {
			return nil
		}
		candidates = append(candidates, candidate{root: root, bin: binPath})
		return nil
	})
	sort.SliceStable(candidates, func(i, j int) bool {
		leftContains := strings.Contains(filepath.Base(candidates[i].root), version)
		rightContains := strings.Contains(filepath.Base(candidates[j].root), version)
		if leftContains != rightContains {
			return leftContains
		}
		return candidates[i].root < candidates[j].root
	})
	for _, item := range candidates {
		binPaths := []string{item.bin}
		for _, rel := range []string{"bin", "Scripts"} {
			if rel != item.bin && pathExists(filepath.Join(item.root, rel)) {
				binPaths = append(binPaths, rel)
			}
		}
		return item.root, uniqueStrings(binPaths)
	}
	return "", nil
}

func pythonRuntimeExecutableNames(installDir string, binPaths []string, configured []string) []string {
	configured = cleanedStringList(configured)
	if len(configured) == 0 {
		configured = defaultPythonExecutableNames()
	}
	envPath := strings.Join(runtimeBinDirsFromInstall(installDir, binPaths), string(os.PathListSeparator))
	if strings.TrimSpace(envPath) == "" {
		return configured
	}
	var existing []string
	for _, name := range configured {
		if _, err := lookPathInDirs(name, envPath); err == nil {
			existing = append(existing, name)
		}
	}
	if len(existing) > 0 {
		return uniqueStrings(existing)
	}
	return defaultPythonExecutableNames()
}

func defaultPythonExecutableNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"python", "pip"}
	}
	return []string{"python", "python3", "pip", "pip3"}
}

func runtimeBinDirsFromInstall(installDir string, binPaths []string) []string {
	installDir = strings.TrimSpace(installDir)
	if installDir == "" {
		return nil
	}
	binPaths = cleanedStringList(binPaths)
	if len(binPaths) == 0 {
		binPaths = inferRuntimeBinPaths(installDir)
	}
	var dirs []string
	for _, rel := range binPaths {
		if rel == "." {
			dirs = append(dirs, installDir)
			continue
		}
		if filepath.IsAbs(rel) {
			dirs = append(dirs, rel)
			continue
		}
		dirs = append(dirs, filepath.Join(installDir, rel))
	}
	return uniqueStrings(dirs)
}

func sameCleanPath(left string, right string) bool {
	left = filepath.Clean(strings.TrimSpace(left))
	right = filepath.Clean(strings.TrimSpace(right))
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func (r *runtimePreflightRunner) runtimeCandidates(item model.RuntimeCatalogItem, version model.RuntimeVersion) []runtimeInstallCandidate {
	artifactByID := map[string]model.RuntimeArtifact{}
	for _, artifact := range item.Artifacts {
		if !artifact.Enabled || artifact.VersionID != version.ID || !matchesPlatformArch(artifact.Platform, artifact.Arch) {
			continue
		}
		artifactByID[artifact.ID] = artifact
	}
	var out []runtimeInstallCandidate
	for _, mirror := range item.Mirrors {
		if !mirror.Enabled || !matchesRuntimeVersion(mirror.RuntimeID, mirror.VersionID, item.ID, version.ID) || !matchesPlatformArch(mirror.Platform, mirror.Arch) {
			continue
		}
		if !runtimeMirrorCanDownloadArtifact(mirror) {
			continue
		}
		for _, artifact := range artifactByID {
			rawURL := renderRuntimeURL(mirror, artifact, version)
			if strings.TrimSpace(rawURL) == "" {
				continue
			}
			timeout := time.Duration(mirror.TimeoutSeconds) * time.Second
			if timeout <= 0 {
				timeout = runtimeDownloadTimeout(artifact.SizeBytes)
			}
			out = append(out, runtimeInstallCandidate{
				sourceType:  "mirror",
				sourceID:    mirror.ID,
				name:        firstNonEmpty(mirror.Name, mirror.ID),
				url:         rawURL,
				filename:    firstNonEmpty(artifact.Filename, runtimeCandidateFilename(rawURL)),
				sha256:      artifact.SHA256,
				kind:        artifact.PackageKind,
				priority:    mirror.Priority,
				headers:     mirror.Headers,
				timeout:     timeout,
				binPaths:    artifact.BinPaths,
				envPatch:    artifact.EnvPatch,
				extractRoot: artifact.ExtractRoot,
			})
		}
	}
	for _, artifact := range artifactByID {
		rawURL := strings.TrimSpace(artifact.DownloadPath)
		if rawURL == "" {
			rawURL = strings.TrimSpace(artifact.StorageKey)
		}
		if rawURL == "" {
			continue
		}
		out = append(out, runtimeInstallCandidate{
			sourceType:  "artifact",
			sourceID:    artifact.ID,
			name:        firstNonEmpty(artifact.Filename, artifact.ID),
			url:         rawURL,
			filename:    firstNonEmpty(artifact.Filename, runtimeCandidateFilename(rawURL)),
			sha256:      artifact.SHA256,
			kind:        artifact.PackageKind,
			priority:    1000 + artifact.Priority,
			timeout:     runtimeDownloadTimeout(artifact.SizeBytes),
			binPaths:    artifact.BinPaths,
			envPatch:    artifact.EnvPatch,
			extractRoot: artifact.ExtractRoot,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].priority != out[j].priority {
			return out[i].priority < out[j].priority
		}
		return out[i].sourceID < out[j].sourceID
	})
	return out
}

func runtimeMirrorCanDownloadArtifact(mirror model.RuntimeMirror) bool {
	template := strings.TrimSpace(mirror.URLTemplate)
	if template == "" {
		return true
	}
	return strings.Contains(template, "{filename}")
}

func (r *runtimePreflightRunner) downloadRuntimeCandidate(candidate runtimeInstallCandidate, target string, runtimeID string, progressBase int) error {
	if !isHTTPURL(candidate.url) {
		return fmt.Errorf("下载地址不是 HTTP(S) URL：%s", candidate.url)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	effectiveTimeout := candidate.timeout
	if effectiveTimeout <= 0 {
		effectiveTimeout = runtimeDownloadMinTimeout
	}
	if effectiveTimeout > runtimeDownloadMaxTimeout {
		effectiveTimeout = runtimeDownloadMaxTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeDownloadMaxTimeout)
	defer cancel()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = runtimeDownloadIdleTimeout
	client := http.Client{
		Transport: transport,
		Timeout:   0,
	}
	headers := map[string]string{}
	for key, value := range candidate.headers {
		headers[key] = value
	}
	if strings.TrimSpace(headers["User-Agent"]) == "" {
		headers["User-Agent"] = runtimeDownloadUserAgent
	}
	start := time.Now()
	lastReport := time.Time{}
	var result resumabledownload.Result
	var err error
	var receivedBytes int64
	var totalBytes int64
	for attempt := 1; attempt <= runtimeDownloadMaxAttempts; attempt++ {
		result, err = resumabledownload.Fetch(ctx, resumabledownload.Options{
			Client:         &client,
			URL:            candidate.url,
			Target:         target,
			ExpectedSHA256: candidate.sha256,
			Headers:        headers,
			Progress: func(received int64, total int64) error {
				receivedBytes = received
				totalBytes = total
				if total > 0 {
					if dynamicTimeout := runtimeDownloadTimeout(total); dynamicTimeout > effectiveTimeout {
						effectiveTimeout = dynamicTimeout
					}
				}
				if time.Since(start) > effectiveTimeout {
					return fmt.Errorf("下载超过动态超时 %s，已保留断点", effectiveTimeout)
				}
				if !lastReport.IsZero() && time.Since(lastReport) < 500*time.Millisecond {
					return nil
				}
				lastReport = time.Now()
				r.emit(preflightStatusRunning, "runtime", progressBase+10, []model.RuntimeInstallEvent{{
					ItemType:        preflightItemRuntime,
					ItemID:          runtimeID,
					Phase:           "download",
					Status:          "running",
					Message:         fmt.Sprintf("正在下载 %s", candidate.name),
					ReceivedBytes:   received,
					TotalBytes:      total,
					ProgressPercent: progressBase + 10,
					Details: map[string]any{
						"source_type": candidate.sourceType,
						"source_id":   candidate.sourceID,
						"attempt":     attempt,
						"duration_ms": time.Since(start).Milliseconds(),
					},
				}})
				return nil
			},
		})
		if err == nil {
			break
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || time.Since(start) > effectiveTimeout {
			return fmt.Errorf("下载超过动态超时 %s，已保留断点", effectiveTimeout)
		}
		if attempt == runtimeDownloadMaxAttempts {
			return err
		}
		delay := runtimeDownloadRetryBaseDelay * time.Duration(1<<(attempt-1))
		r.emit(preflightStatusRunning, "runtime", progressBase+10, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemRuntime,
			ItemID:          runtimeID,
			Phase:           "download",
			Status:          "warning",
			Message:         fmt.Sprintf("下载中断，%d 秒后从断点重试（%d/%d）", int(delay/time.Second), attempt+1, runtimeDownloadMaxAttempts),
			ReceivedBytes:   receivedBytes,
			TotalBytes:      totalBytes,
			ProgressPercent: progressBase + 10,
			Details: map[string]any{
				"source_type": candidate.sourceType,
				"source_id":   candidate.sourceID,
				"attempt":     attempt,
				"error":       err.Error(),
			},
		}})
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("下载超过动态超时 %s，已保留断点", effectiveTimeout)
		case <-timer.C:
		}
	}
	r.emit(preflightStatusRunning, "runtime", progressBase+15, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemRuntime,
		ItemID:          runtimeID,
		Phase:           "download",
		Status:          "success",
		Message:         fmt.Sprintf("%s 下载完成并通过 SHA-256 校验", candidate.name),
		ReceivedBytes:   result.ReceivedBytes,
		TotalBytes:      result.TotalBytes,
		ProgressPercent: progressBase + 15,
		Details: map[string]any{
			"source_type":  candidate.sourceType,
			"source_id":    candidate.sourceID,
			"duration_ms":  time.Since(start).Milliseconds(),
			"resumed_from": result.ResumedFrom,
			"reused":       result.Reused,
		},
	}})
	return nil
}

func runtimeDownloadTimeout(sizeBytes int64) time.Duration {
	if sizeBytes <= 0 {
		return runtimeDownloadMinTimeout
	}
	// 按约 256KB/s 的弱网速度估算，同时给 TLS/磁盘写入留出余量。
	seconds := sizeBytes / (256 * 1024)
	if sizeBytes%(256*1024) != 0 {
		seconds++
	}
	timeout := time.Duration(seconds)*time.Second + 2*time.Minute
	if timeout < runtimeDownloadMinTimeout {
		return runtimeDownloadMinTimeout
	}
	if timeout > runtimeDownloadMaxTimeout {
		return runtimeDownloadMaxTimeout
	}
	return timeout
}

func (r *runtimePreflightRunner) extractRuntimeCandidate(item model.RuntimeCatalogItem, version model.RuntimeVersion, candidate runtimeInstallCandidate, archivePath string, progressBase int) (installedRuntimeManifest, error) {
	kind := normalizePackageKind(candidate.kind, archivePath)
	if kind != "zip" && kind != "tar.gz" && kind != "tgz" {
		return installedRuntimeManifest{}, fmt.Errorf("当前只支持 zip/tar.gz/tgz 归档包，实际包类型为 %s", kind)
	}
	targetDir := filepath.Join(r.service.cfg.RuntimeDir, "runtimes", item.ID, version.Version)
	stagingDir := filepath.Join(r.service.cfg.RuntimeDir, "runtimes", item.ID, ".install-"+version.Version+"-"+fmt.Sprint(time.Now().UnixNano()))
	_ = os.RemoveAll(stagingDir)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return installedRuntimeManifest{}, err
	}
	defer os.RemoveAll(stagingDir)
	r.emit(preflightStatusRunning, "runtime", progressBase+18, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemRuntime,
		ItemID:          item.ID,
		Phase:           "extract",
		Status:          "running",
		Message:         "正在解压托管运行时",
		ProgressPercent: progressBase + 18,
	}})
	if kind == "zip" {
		if err := unzipRuntimeArchive(archivePath, stagingDir); err != nil {
			return installedRuntimeManifest{}, err
		}
	} else {
		if err := untarRuntimeArchive(archivePath, stagingDir); err != nil {
			return installedRuntimeManifest{}, err
		}
	}
	installRoot := stagingDir
	if root := strings.TrimSpace(candidate.extractRoot); root != "" {
		cleanRoot := filepath.Clean(root)
		installRoot = filepath.Join(stagingDir, cleanRoot)
		if !pathExists(installRoot) {
			if single := singleChildDir(stagingDir); single != "" {
				installRoot = filepath.Join(single, cleanRoot)
			}
		}
	} else if single := singleChildDir(stagingDir); single != "" {
		installRoot = single
	}
	if !pathExists(installRoot) {
		if single := singleChildDir(stagingDir); single != "" {
			installRoot = single
		}
	}
	if !pathExists(installRoot) {
		return installedRuntimeManifest{}, fmt.Errorf("解压后未找到运行时根目录：%s", installRoot)
	}
	_ = os.RemoveAll(targetDir)
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return installedRuntimeManifest{}, err
	}
	if err := os.Rename(installRoot, targetDir); err != nil {
		return installedRuntimeManifest{}, err
	}
	if err := ensureRuntimeExecutablePermissions(targetDir, candidate.binPaths, item.ExecutableNames); err != nil {
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
		BinPaths:        cleanedStringList(candidate.binPaths),
		ExecutableNames: cleanedStringList(item.ExecutableNames),
		EnvPatch:        renderRuntimeEnvPatch(candidate.envPatch, targetDir, r.service.cfg.RuntimeDir),
		SourceType:      candidate.sourceType,
		SourceID:        candidate.sourceID,
		SHA256:          strings.ToLower(strings.TrimSpace(candidate.sha256)),
		InstalledAt:     time.Now().UTC(),
	}
	if len(manifest.BinPaths) == 0 {
		manifest.BinPaths = inferRuntimeBinPaths(targetDir)
	}
	if len(manifest.ExecutableNames) == 0 {
		manifest.ExecutableNames = []string{item.ID}
	}
	if err := writeRuntimeManifest(r.service.cfg.RuntimeDir, manifest); err != nil {
		return installedRuntimeManifest{}, err
	}
	return manifest, nil
}

func ensureRuntimeExecutablePermissions(installDir string, binPaths []string, executableNames []string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dirs := make([]string, 0, len(binPaths))
	for _, rel := range binPaths {
		rel = filepath.Clean(strings.TrimSpace(rel))
		if rel == "." || rel == "" {
			dirs = append(dirs, installDir)
			continue
		}
		dirs = append(dirs, filepath.Join(installDir, rel))
	}
	for _, name := range executableNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		for _, dir := range dirs {
			path := filepath.Join(dir, name)
			info, err := os.Stat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if info.IsDir() {
				continue
			}
			if err := os.Chmod(path, info.Mode()|0o111); err != nil {
				return err
			}
			break
		}
	}
	return nil
}

func renderRuntimeEnvPatch(input map[string]string, installDir string, runtimeDir string) map[string]string {
	out := cleanedStringMap(input)
	for key, value := range out {
		value = strings.ReplaceAll(value, "{install_dir}", installDir)
		value = strings.ReplaceAll(value, "{runtime_dir}", runtimeDir)
		out[key] = value
	}
	return out
}

func (r *runtimePreflightRunner) verifyRuntimeManifest(manifest installedRuntimeManifest) error {
	if strings.TrimSpace(manifest.InstallDir) == "" || !pathExists(manifest.InstallDir) {
		return fmt.Errorf("安装目录不存在：%s", manifest.InstallDir)
	}
	if !isManagedRuntimeInstallDir(r.service.cfg.RuntimeDir, manifest) {
		return fmt.Errorf("安装目录不在 launcher RuntimeDir 内：%s", manifest.InstallDir)
	}
	if manifest.RuntimeID == "ida" {
		return verifyIDARuntimeManifest(manifest)
	}
	pathDirs := runtimeManifestBinDirs(manifest)
	cmdEnv := os.Environ()
	if manifest.RuntimeID == "jadx" || manifest.RuntimeID == "apktool" {
		if javaManifest, ok := r.service.bestRuntimeManifest("java", ">=17 <22"); ok {
			pathDirs = append(pathDirs, runtimeManifestBinDirs(javaManifest)...)
			cmdEnv = append(cmdEnv, "JAVA_HOME="+javaManifest.InstallDir)
		}
	}
	pathDirs = uniqueStrings(pathDirs)
	envPath := strings.Join(pathDirs, string(os.PathListSeparator))
	if envPath == "" {
		return fmt.Errorf("运行时 %s %s 未找到 bin 目录", manifest.RuntimeID, manifest.Version)
	}
	for _, name := range manifest.ExecutableNames {
		path, err := lookPathInDirs(name, envPath)
		if err != nil {
			return fmt.Errorf("运行时 %s 未找到可执行文件 %s", manifest.RuntimeID, name)
		}
		if runtime.GOOS == "windows" && isWindowsCommandScript(path) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		cmd := exec.CommandContext(ctx, path, "--version")
		backgroundcmd.Configure(cmd)
		cmd.Env = append(cmdEnv, pathKey()+"="+envPath)
		out, runErr := cmd.CombinedOutput()
		cancel()
		if runErr != nil && strings.TrimSpace(string(out)) == "" {
			return fmt.Errorf("运行时 %s 可执行文件 %s 验证失败：%v", manifest.RuntimeID, name, runErr)
		}
	}
	return nil
}

func isManagedRuntimeInstallDir(runtimeDir string, manifest installedRuntimeManifest) bool {
	if isPathUnder(runtimeDir, manifest.InstallDir) {
		return true
	}
	return validIDACompatibilityInstallDir(runtimeDir, manifest)
}

func isWindowsCommandScript(path string) bool {
	extension := strings.ToLower(filepath.Ext(strings.TrimSpace(path)))
	return extension == ".cmd" || extension == ".bat"
}

func (r *runtimePreflightRunner) ensureMCPs() error {
	profile := r.payload.SemanticAgent
	if profile == nil {
		return errors.New("缺少语义 Agent 配置")
	}
	effectiveProfile := *profile
	if r.payload.DisableVerifyMCP {
		effectiveProfile = withoutVerifyMCPProfile(effectiveProfile)
	}
	profile = &effectiveProfile
	if len(profile.MCPIDs) == 0 && len(profile.RecommendedMCPServers) == 0 &&
		len(profile.RecommendedMCPConfigs) == 0 && len(profile.MCPDependencies) == 0 {
		r.emit(preflightStatusRunning, "mcp", 58, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemMCP,
			Phase:           "skip",
			Status:          "info",
			Message:         "当前语义 Agent 没有声明 MCP 依赖",
			ProgressPercent: 58,
		}})
		return nil
	}
	mcpItems := semanticPreflightMCPItems(profile)
	dependencies := semanticPreflightMCPDependencies(profile)
	if !r.payload.ApplyRecommendedMCP {
		for _, item := range mcpItems {
			r.updateItem(preflightItemMCP, item.id, preflightStatusCompleted, 100, "")
		}
		r.emit(preflightStatusRunning, "mcp", 60, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemMCP,
			Phase:           "skip",
			Status:          "info",
			Message:         "本次未要求启用推荐 MCP",
			ProgressPercent: 60,
		}})
		return nil
	}
	for _, item := range mcpItems {
		r.updateItem(preflightItemMCP, item.id, preflightStatusRunning, 10, "")
		r.emit(preflightStatusRunning, "mcp", 62, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemMCP,
			ItemID:          item.id,
			Phase:           "detect",
			Status:          "running",
			Message:         "正在检测 MCP：" + item.name,
			ProgressPercent: 10,
		}})
	}
	info, err := mcpconfig.Load()
	if err != nil {
		for _, item := range mcpItems {
			r.updateItem(preflightItemMCP, item.id, preflightStatusFailed, 100, err.Error())
		}
		return err
	}
	tasks := make([]mcpPreflightTask, 0, len(mcpItems))
	assigned := map[string]bool{}
	for _, server := range semanticRecommendedMCPConfigs(profile.RecommendedMCPConfigs) {
		itemID := semanticPreflightMCPConfigItemID(profile, server)
		if itemID == "" {
			itemID = semanticPreflightMCPItemID(server)
		}
		itemName := firstNonEmpty(server.Name, itemID)
		tasks = append(tasks, mcpPreflightTask{
			index:  len(tasks),
			item:   semanticPreflightMCPItem{id: itemID, name: itemName},
			kind:   mcpPreflightTaskConfig,
			server: server,
		})
		assigned[strings.ToLower(itemID)] = true
		assigned[strings.ToLower(itemName)] = true
	}
	for _, item := range mcpItems {
		if assigned[strings.ToLower(item.id)] || assigned[strings.ToLower(item.name)] {
			continue
		}
		dependency, ok := dependencies[strings.ToLower(item.id)]
		if !ok {
			dependency, ok = dependencies[strings.ToLower(item.name)]
		}
		if !ok {
			continue
		}
		tasks = append(tasks, mcpPreflightTask{
			index:      len(tasks),
			item:       item,
			kind:       mcpPreflightTaskDependency,
			dependency: dependency,
		})
		assigned[strings.ToLower(item.id)] = true
		assigned[strings.ToLower(item.name)] = true
	}
	readyMCPItems := map[string]bool{}
	results := make([]mcpPreflightResult, 0, len(tasks))
	failedResults := make([]mcpPreflightResult, 0)
	if len(tasks) > 0 {
		r.emit(preflightStatusRunning, "mcp", 63, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemMCP,
			Phase:           "parallel_start",
			Status:          "running",
			Message:         fmt.Sprintf("开始并行检测与安装 %d 个 MCP", len(tasks)),
			ProgressPercent: 15,
			Details: map[string]any{
				"mcp_count":   len(tasks),
				"concurrency": len(tasks),
			},
		}})
		resultCh := make(chan mcpPreflightResult, len(tasks))
		var wg sync.WaitGroup
		for _, task := range tasks {
			task := task
			wg.Add(1)
			go func() {
				defer wg.Done()
				resultCh <- r.ensureMCPTask(task)
			}()
		}
		wg.Wait()
		close(resultCh)
		for result := range resultCh {
			if result.err != nil {
				failedResults = append(failedResults, result)
				continue
			}
			results = append(results, result)
			readyMCPItems[result.itemID] = true
			readyMCPItems[strings.ToLower(result.itemID)] = true
			if strings.TrimSpace(result.server.Name) != "" {
				readyMCPItems[result.server.Name] = true
				readyMCPItems[strings.ToLower(result.server.Name)] = true
			}
		}
		sort.SliceStable(results, func(i, j int) bool {
			return results[i].index < results[j].index
		})
	}
	if len(failedResults) > 0 {
		sort.SliceStable(failedResults, func(i, j int) bool {
			return failedResults[i].index < failedResults[j].index
		})
		installFailures := make([]string, 0, len(failedResults))
		for _, result := range failedResults {
			installFailures = append(installFailures, result.err.Error())
		}
		return errors.New(strings.Join(installFailures, "；"))
	}
	var missingFailures []string
	for _, item := range mcpItems {
		if readyMCPItems[item.id] || readyMCPItems[strings.ToLower(item.id)] || readyMCPItems[item.name] || readyMCPItems[strings.ToLower(item.name)] {
			continue
		}
		errText := "MCP 缺少可启动配置，需要先安装或解析启动路径：" + item.name
		missingFailures = append(missingFailures, errText)
		r.updateItem(preflightItemMCP, item.id, preflightStatusFailed, 100, errText)
		r.emit(preflightStatusRunning, "mcp", 66, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemMCP,
			ItemID:          item.id,
			Phase:           "install_required",
			Status:          "error",
			Message:         errText,
			ProgressPercent: 100,
			Details:         map[string]any{"mcp_name": item.name},
		}})
	}
	if len(missingFailures) > 0 {
		return errors.New(strings.Join(missingFailures, "；"))
	}
	recommended := make([]model.DeviceMCPServerInfo, 0, len(results))
	for _, result := range results {
		recommended = append(recommended, result.server)
	}
	merged := mergeMCPServers(info.Servers, recommended, false)
	existingNames := make([]string, 0, len(info.Servers))
	for _, srv := range info.Servers {
		existingNames = append(existingNames, srv.Name)
	}
	mergedNames := make([]string, 0, len(merged))
	for _, srv := range merged {
		mergedNames = append(mergedNames, srv.Name)
	}
	log.Printf("[launcher][mcp] preflight Save existing=%v recommended=%d merged=%v", existingNames, len(recommended), mergedNames)
	if _, err := mcpconfig.Save(model.DeviceMCPConfigInfo{Servers: merged}); err != nil {
		for _, item := range mcpItems {
			r.updateItem(preflightItemMCP, item.id, preflightStatusFailed, 100, err.Error())
		}
		return err
	}
	for _, item := range mcpItems {
		r.updateItem(preflightItemMCP, item.id, preflightStatusCompleted, 100, "")
		r.emit(preflightStatusRunning, "mcp", 68, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemMCP,
			ItemID:          item.id,
			Phase:           "installed",
			Status:          "success",
			Message:         "MCP 已写入全局配置，默认保持停用：" + item.name,
			ProgressPercent: 100,
		}})
	}
	return nil
}

func (r *runtimePreflightRunner) ensureSkills() error {
	profile := r.payload.SemanticAgent
	item, err := semanticAgentDefinitionFromProfile(*profile)
	if err != nil {
		return err
	}
	if len(item.skills) == 0 {
		r.updateItem(preflightItemSkill, "semantic-skills", preflightStatusCompleted, 100, "")
		r.emit(preflightStatusRunning, "skill", 78, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemSkill,
			Phase:           "skip",
			Status:          "info",
			Message:         "当前语义 Agent 没有声明 Skill",
			ProgressPercent: 78,
		}})
		return nil
	}
	r.updateItem(preflightItemSkill, "semantic-skills", preflightStatusRunning, 30, "")
	root, ok := r.service.prepareSemanticAgentSkills(item)
	if !ok || strings.TrimSpace(root) == "" {
		err := errors.New("语义 Agent Skill 写入失败")
		r.updateItem(preflightItemSkill, "semantic-skills", preflightStatusFailed, 100, err.Error())
		return err
	}
	for _, skill := range item.skills {
		path := filepath.Join(root, skill.name, "SKILL.md")
		if !pathExists(path) {
			err := fmt.Errorf("Skill 未写入：%s", skill.name)
			r.updateItem(preflightItemSkill, "semantic-skills", preflightStatusFailed, 100, err.Error())
			return err
		}
	}
	r.updateItem(preflightItemSkill, "semantic-skills", preflightStatusCompleted, 100, "")
	r.emit(preflightStatusRunning, "skill", 84, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemSkill,
		ItemID:          "semantic-skills",
		Phase:           "installed",
		Status:          "success",
		Message:         "语义 Agent Skill 已装载",
		ProgressPercent: 84,
		Details:         map[string]any{"path": root},
	}})
	return nil
}

func (r *runtimePreflightRunner) ensureMCPTask(task mcpPreflightTask) mcpPreflightResult {
	item := task.item
	item.id = strings.TrimSpace(item.id)
	item.name = strings.TrimSpace(item.name)
	if item.id == "" {
		item.id = item.name
	}
	if item.name == "" {
		item.name = item.id
	}
	result := mcpPreflightResult{itemID: item.id}
	result.index = task.index
	switch task.kind {
	case mcpPreflightTaskConfig:
		r.updateItem(preflightItemMCP, item.id, preflightStatusRunning, 40, "")
		r.emit(preflightStatusRunning, "mcp", 64, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemMCP,
			ItemID:          item.id,
			Phase:           "resolve",
			Status:          "running",
			Message:         "正在解析 MCP 启动路径：" + item.name,
			ProgressPercent: 40,
		}})
		resolved, err := resolveMCPServerForRuntime(r.service.cfg.RuntimeDir, task.server)
		if err != nil {
			result.err = err
			r.updateItem(preflightItemMCP, item.id, preflightStatusFailed, 100, err.Error())
			r.emit(preflightStatusRunning, "mcp", 66, []model.RuntimeInstallEvent{{
				ItemType:        preflightItemMCP,
				ItemID:          item.id,
				Phase:           "resolve",
				Status:          "error",
				Message:         err.Error(),
				ProgressPercent: 100,
			}})
			return result
		}
		result.server = r.service.repairMCPServerExternalTools(resolved)
		r.markMCPTaskReady(item, "resolve", "MCP 启动路径已解析，等待写入配置："+item.name)
		return result
	case mcpPreflightTaskDependency:
		resolved, err := r.ensureMCPDependencyInstalled(item, task.dependency)
		if err != nil {
			result.err = err
			r.updateItem(preflightItemMCP, item.id, preflightStatusFailed, 100, err.Error())
			r.emit(preflightStatusRunning, "mcp", 66, []model.RuntimeInstallEvent{{
				ItemType:        preflightItemMCP,
				ItemID:          item.id,
				Phase:           "install",
				Status:          "error",
				Message:         err.Error(),
				ProgressPercent: 100,
			}})
			return result
		}
		result.server = r.service.repairMCPServerExternalTools(resolved)
		r.markMCPTaskReady(item, "resolve", "MCP 本地安装或解析完成，等待写入配置："+item.name)
		return result
	default:
		err := fmt.Errorf("未知 MCP 预检任务类型：%s", task.kind)
		result.err = err
		r.updateItem(preflightItemMCP, item.id, preflightStatusFailed, 100, err.Error())
		return result
	}
}

func (r *runtimePreflightRunner) ensureMCPDependencyInstalled(item semanticPreflightMCPItem, dependency model.SemanticAgentMCPDependency) (model.DeviceMCPServerInfo, error) {
	dependency.ID = strings.TrimSpace(dependency.ID)
	dependency.Name = strings.TrimSpace(dependency.Name)
	if dependency.ID == "" {
		dependency.ID = item.id
	}
	if dependency.Name == "" {
		dependency.Name = item.name
	}
	if dependency.Name == "" {
		dependency.Name = dependency.ID
	}
	installRoot := mcpInstallRoot(r.service.cfg.RuntimeDir, dependency.ID)
	if dependency.LaunchReady {
		resolved, err := resolveMCPServerForRuntimeID(r.service.cfg.RuntimeDir, dependency.ID, dependency.Config)
		if err == nil {
			r.emit(preflightStatusRunning, "mcp", 64, []model.RuntimeInstallEvent{{
				ItemType:        preflightItemMCP,
				ItemID:          item.id,
				Phase:           "resolve",
				Status:          "success",
				Message:         "MCP 启动配置已解析：" + dependency.Name,
				ProgressPercent: 65,
			}})
			return resolved, nil
		}
		r.emit(preflightStatusRunning, "mcp", 65, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemMCP,
			ItemID:          item.id,
			Phase:           "resolve",
			Status:          "warning",
			Message:         "MCP 启动配置需要修补：" + err.Error(),
			ProgressPercent: 65,
		}})
	}
	if mcpInstallRootHasContent(installRoot) {
		server, serverErr := mcpServerFromInstalledDependency(r.service.cfg.RuntimeDir, dependency)
		if serverErr == nil {
			resolved, resolveErr := resolveMCPServerForRuntimeID(r.service.cfg.RuntimeDir, dependency.ID, server)
			if resolveErr == nil {
				if reusable, reason := reusableMCPInstallRoot(installRoot, resolved); reusable {
					r.emit(preflightStatusRunning, "mcp", 65, []model.RuntimeInstallEvent{{
						ItemType:        preflightItemMCP,
						ItemID:          item.id,
						Phase:           "reuse",
						Status:          "success",
						Message:         "复用已有 MCP 安装目录：" + dependency.Name,
						ProgressPercent: 70,
						Details: map[string]any{
							"mcp_name":     dependency.Name,
							"install_root": installRoot,
						},
					}})
					return resolved, nil
				} else {
					r.emit(preflightStatusRunning, "mcp", 65, []model.RuntimeInstallEvent{{
						ItemType:        preflightItemMCP,
						ItemID:          item.id,
						Phase:           "reuse",
						Status:          "warning",
						Message:         "已有 MCP 安装目录不可复用，将清理后重装：" + reason,
						ProgressPercent: 45,
						Details: map[string]any{
							"mcp_name":     dependency.Name,
							"install_root": installRoot,
						},
					}})
				}
			} else {
				r.emit(preflightStatusRunning, "mcp", 65, []model.RuntimeInstallEvent{{
					ItemType:        preflightItemMCP,
					ItemID:          item.id,
					Phase:           "reuse",
					Status:          "warning",
					Message:         "已有 MCP 安装目录启动配置解析失败，将清理后重装：" + resolveErr.Error(),
					ProgressPercent: 45,
					Details: map[string]any{
						"mcp_name":     dependency.Name,
						"install_root": installRoot,
					},
				}})
			}
		} else {
			r.emit(preflightStatusRunning, "mcp", 65, []model.RuntimeInstallEvent{{
				ItemType:        preflightItemMCP,
				ItemID:          item.id,
				Phase:           "reuse",
				Status:          "warning",
				Message:         "已有 MCP 安装目录缺少可启动模板，将清理后重装：" + serverErr.Error(),
				ProgressPercent: 45,
				Details: map[string]any{
					"mcp_name":     dependency.Name,
					"install_root": installRoot,
				},
			}})
		}
	}
	nativeAssets := cleanedMCPInstallAssets(dependency.Install.Assets)
	nativeFiles := cleanedMCPInstallFiles(dependency.Install.Files)
	installCommands := cleanedStringList(dependency.Install.InstallCommands)
	buildCommands := cleanedStringList(dependency.Install.BuildCommands)
	if len(nativeAssets) == 0 && len(nativeFiles) == 0 && len(installCommands) == 0 && len(buildCommands) == 0 {
		reason := strings.TrimSpace(dependency.LaunchBlockReason)
		if reason == "" {
			reason = "缺少安装命令"
		}
		return model.DeviceMCPServerInfo{}, fmt.Errorf("MCP %s 需要安装，但 MCP 库没有完整安装信息：%s", dependency.Name, reason)
	}
	if !r.payload.AutoRepair {
		return model.DeviceMCPServerInfo{}, fmt.Errorf("MCP %s 未就绪，且本次未启用自动修补", dependency.Name)
	}
	if mcpInstallRootHasContent(installRoot) {
		if err := os.RemoveAll(installRoot); err != nil {
			return model.DeviceMCPServerInfo{}, err
		}
	}
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		return model.DeviceMCPServerInfo{}, err
	}
	r.emit(preflightStatusRunning, "mcp", 66, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemMCP,
		ItemID:          item.id,
		Phase:           "install",
		Status:          "running",
		Message:         "开始安装 MCP：" + dependency.Name,
		ProgressPercent: 45,
		Details: map[string]any{
			"mcp_name":     dependency.Name,
			"install_root": installRoot,
			"source_url":   dependency.SourceURL,
		},
	}})
	env := r.mcpInstallEnv(dependency, installRoot)
	if err := r.prepareMCPNativeInstall(item, dependency, nativeAssets, nativeFiles, installRoot); err != nil {
		return model.DeviceMCPServerInfo{}, err
	}
	if err := r.runMCPCommands(item, dependency, "install", installCommands, installRoot, env, 45, 58); err != nil {
		return model.DeviceMCPServerInfo{}, err
	}
	if err := r.runMCPCommands(item, dependency, "build", buildCommands, installRoot, env, 58, 66); err != nil {
		return model.DeviceMCPServerInfo{}, err
	}
	server, err := mcpServerFromInstalledDependency(r.service.cfg.RuntimeDir, dependency)
	if err != nil {
		return model.DeviceMCPServerInfo{}, err
	}
	resolved, err := resolveMCPServerForRuntimeID(r.service.cfg.RuntimeDir, dependency.ID, server)
	if err != nil {
		return model.DeviceMCPServerInfo{}, err
	}
	r.emit(preflightStatusRunning, "mcp", 67, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemMCP,
		ItemID:          item.id,
		Phase:           "resolve",
		Status:          "success",
		Message:         "MCP 安装后启动路径已解析：" + dependency.Name,
		ProgressPercent: 70,
		Details: map[string]any{
			"mcp_name":     dependency.Name,
			"install_root": installRoot,
		},
	}})
	return resolved, nil
}

func (r *runtimePreflightRunner) markMCPTaskReady(item semanticPreflightMCPItem, phase string, message string) {
	r.updateItem(preflightItemMCP, item.id, preflightStatusCompleted, 100, "")
	r.emit(preflightStatusRunning, "mcp", 66, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemMCP,
		ItemID:          item.id,
		Phase:           phase,
		Status:          "success",
		Message:         message,
		ProgressPercent: 100,
	}})
}

func (r *runtimePreflightRunner) mcpInstallEnv(dependency model.SemanticAgentMCPDependency, installRoot string) []string {
	envMap := map[string]string{}
	for _, value := range os.Environ() {
		if key, val, ok := strings.Cut(value, "="); ok {
			envMap[key] = val
		}
	}
	pathDirs := []string{}
	for _, req := range r.payload.SemanticAgent.RuntimeRequirements {
		if manifest, ok := r.service.bestRuntimeManifest(req.RuntimeID, req.VersionConstraint); ok {
			pathDirs = append(pathDirs, runtimeManifestBinDirs(manifest)...)
			for key, value := range manifest.EnvPatch {
				key = strings.TrimSpace(key)
				if key == "" || strings.TrimSpace(value) == "" {
					continue
				}
				envMap[key] = value
			}
		}
	}
	if len(pathDirs) > 0 {
		key := pathKey()
		joined := strings.Join(uniqueStrings(pathDirs), string(os.PathListSeparator))
		if current := strings.TrimSpace(envMap[key]); current != "" {
			joined += string(os.PathListSeparator) + current
		}
		envMap[key] = joined
	}
	for key, value := range dependency.Install.Environment {
		if strings.TrimSpace(key) == "" {
			continue
		}
		envMap[strings.TrimSpace(key)] = strings.ReplaceAll(value, mcpHomePlaceholder, installRoot)
	}
	envMap["MCP_HOME"] = installRoot
	envMap["MCP_ID"] = dependency.ID
	envMap["MCP_NAME"] = dependency.Name
	out := make([]string, 0, len(envMap))
	for key, value := range envMap {
		out = append(out, key+"="+value)
	}
	sort.Strings(out)
	return out
}

func (r *runtimePreflightRunner) prepareMCPNativeInstall(item semanticPreflightMCPItem, dependency model.SemanticAgentMCPDependency, assets []model.MCPInstallAsset, files []model.MCPInstallFile, installRoot string) error {
	for index, asset := range assets {
		if err := r.installMCPNativeAsset(item, dependency, asset, installRoot, index+1, len(assets)); err != nil {
			return err
		}
	}
	for index, file := range files {
		if err := r.writeMCPNativeFile(item, dependency, file, installRoot, index+1, len(files)); err != nil {
			return err
		}
	}
	return nil
}

func (r *runtimePreflightRunner) installMCPNativeAsset(item semanticPreflightMCPItem, dependency model.SemanticAgentMCPDependency, asset model.MCPInstallAsset, installRoot string, index int, total int) error {
	url := strings.TrimSpace(asset.URL)
	if !isHTTPURL(url) {
		return fmt.Errorf("MCP %s 原生资源下载地址不是 HTTP(S) URL：%s", dependency.Name, url)
	}
	filename := strings.TrimSpace(asset.Filename)
	if filename == "" {
		if parsed, err := urlpkg.Parse(url); err == nil {
			filename = filepath.Base(parsed.Path)
		}
	}
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		filename = dependency.ID + "-asset"
	}
	filename = safeMCPAssetFilename(filename, dependency.ID+"-asset")
	downloadPath, err := safeMCPInstallPath(installRoot, filepath.Join(".downloads", filename))
	if err != nil {
		return err
	}
	r.emit(preflightStatusRunning, "mcp", 66, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemMCP,
		ItemID:          item.id,
		Phase:           "download",
		Status:          "running",
		Message:         "正在下载 MCP 资源：" + firstNonEmpty(asset.Name, filename),
		ProgressPercent: 48,
		Details: map[string]any{
			"mcp_name": dependency.Name,
			"url":      url,
			"filename": filename,
			"index":    index,
			"total":    total,
		},
	}})
	download := mcpNativeDownload{
		name:     firstNonEmpty(asset.Name, filename),
		url:      url,
		filename: filename,
		sha256:   strings.TrimSpace(asset.SHA256),
		headers:  cleanedStringMap(asset.Headers),
		timeout:  runtimeDownloadMinTimeout,
	}
	if err := r.downloadMCPNativeAsset(download, downloadPath, item, dependency, 48); err != nil {
		return fmt.Errorf("MCP %s 资源下载失败：%w", dependency.Name, err)
	}
	if !asset.Extract {
		targetPath, err := safeMCPInstallPath(installRoot, firstNonEmpty(asset.TargetDir, filename))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		if err := copyFile(downloadPath, targetPath, 0o644); err != nil {
			return err
		}
		return nil
	}
	targetDir, err := safeMCPInstallPath(installRoot, strings.TrimSpace(asset.TargetDir))
	if err != nil {
		return err
	}
	if strings.TrimSpace(asset.TargetDir) == "" {
		targetDir = installRoot
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	kind := normalizePackageKind(asset.PackageKind, downloadPath)
	switch kind {
	case "zip":
		if err := unzipRuntimeArchive(downloadPath, targetDir); err != nil {
			return fmt.Errorf("MCP %s 资源解压失败：%w", dependency.Name, err)
		}
	case "tar.gz", "tgz":
		if err := untarRuntimeArchive(downloadPath, targetDir); err != nil {
			return fmt.Errorf("MCP %s 资源解压失败：%w", dependency.Name, err)
		}
	default:
		return fmt.Errorf("MCP %s 原生资源不支持的包类型：%s", dependency.Name, kind)
	}
	r.emit(preflightStatusRunning, "mcp", 66, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemMCP,
		ItemID:          item.id,
		Phase:           "extract",
		Status:          "success",
		Message:         "MCP 资源已解压：" + firstNonEmpty(asset.Name, filename),
		ProgressPercent: 55,
		Details: map[string]any{
			"mcp_name":   dependency.Name,
			"target_dir": targetDir,
			"index":      index,
			"total":      total,
		},
	}})
	return nil
}

func (r *runtimePreflightRunner) writeMCPNativeFile(item semanticPreflightMCPItem, dependency model.SemanticAgentMCPDependency, file model.MCPInstallFile, installRoot string, index int, total int) error {
	targetPath, err := safeMCPInstallPath(installRoot, file.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(file.Mode)
	if mode == 0 {
		mode = 0o644
	}
	if err := os.WriteFile(targetPath, []byte(file.Content), mode); err != nil {
		return err
	}
	r.emit(preflightStatusRunning, "mcp", 66, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemMCP,
		ItemID:          item.id,
		Phase:           "write_file",
		Status:          "success",
		Message:         "MCP 配置文件已写入：" + filepath.Base(targetPath),
		ProgressPercent: 56,
		Details: map[string]any{
			"mcp_name": dependency.Name,
			"path":     targetPath,
			"index":    index,
			"total":    total,
		},
	}})
	return nil
}

type mcpNativeDownload struct {
	name     string
	url      string
	filename string
	sha256   string
	headers  map[string]string
	timeout  time.Duration
}

func (r *runtimePreflightRunner) downloadMCPNativeAsset(download mcpNativeDownload, target string, item semanticPreflightMCPItem, dependency model.SemanticAgentMCPDependency, progress int) error {
	if !isHTTPURL(download.url) {
		return fmt.Errorf("下载地址不是 HTTP(S) URL：%s", download.url)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	effectiveTimeout := download.timeout
	if effectiveTimeout <= 0 {
		effectiveTimeout = runtimeDownloadMinTimeout
	}
	if effectiveTimeout > runtimeDownloadMaxTimeout {
		effectiveTimeout = runtimeDownloadMaxTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeDownloadMaxTimeout)
	defer cancel()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = runtimeDownloadIdleTimeout
	client := http.Client{
		Transport: transport,
		Timeout:   0,
	}
	headers := map[string]string{}
	for key, value := range download.headers {
		headers[key] = value
	}
	if strings.TrimSpace(headers["User-Agent"]) == "" {
		headers["User-Agent"] = runtimeDownloadUserAgent
	}
	start := time.Now()
	lastReport := time.Now()
	result, err := resumabledownload.Fetch(ctx, resumabledownload.Options{
		Client:         &client,
		URL:            download.url,
		Target:         target,
		ExpectedSHA256: download.sha256,
		Headers:        headers,
		Progress: func(received int64, totalBytes int64) error {
			if totalBytes > 0 {
				if dynamicTimeout := runtimeDownloadTimeout(totalBytes); dynamicTimeout > effectiveTimeout {
					effectiveTimeout = dynamicTimeout
				}
			}
			if time.Since(start) > effectiveTimeout {
				return fmt.Errorf("下载超过动态超时 %s，已保留断点", effectiveTimeout)
			}
			if time.Since(lastReport) < 500*time.Millisecond {
				return nil
			}
			lastReport = time.Now()
			r.emit(preflightStatusRunning, "mcp", 66, []model.RuntimeInstallEvent{{
				ItemType:        preflightItemMCP,
				ItemID:          item.id,
				Phase:           "download",
				Status:          "running",
				Message:         "正在下载或续传 MCP 资源：" + firstNonEmpty(download.name, download.filename, dependency.Name),
				ReceivedBytes:   received,
				TotalBytes:      totalBytes,
				ProgressPercent: progress,
				Details: map[string]any{
					"mcp_name":    dependency.Name,
					"url":         download.url,
					"filename":    download.filename,
					"duration_ms": time.Since(start).Milliseconds(),
				},
			}})
			return nil
		},
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("下载超过动态超时 %s，已保留断点", effectiveTimeout)
		}
		return err
	}
	r.emit(preflightStatusRunning, "mcp", 66, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemMCP,
		ItemID:          item.id,
		Phase:           "download",
		Status:          "success",
		Message:         "MCP 资源下载完成：" + firstNonEmpty(download.name, download.filename, dependency.Name),
		ReceivedBytes:   result.ReceivedBytes,
		TotalBytes:      result.TotalBytes,
		ProgressPercent: progress + 4,
		Details: map[string]any{
			"mcp_name":     dependency.Name,
			"url":          download.url,
			"filename":     download.filename,
			"duration_ms":  time.Since(start).Milliseconds(),
			"resumed_from": result.ResumedFrom,
			"reused":       result.Reused,
		},
	}})
	return nil
}

func safeMCPAssetFilename(filename string, fallback string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = fallback
	}
	base := filepath.Base(filepath.Clean(filename))
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = fallback
	}
	return base
}

func (r *runtimePreflightRunner) runMCPCommands(item semanticPreflightMCPItem, dependency model.SemanticAgentMCPDependency, phase string, commands []string, workDir string, env []string, startProgress int, endProgress int) error {
	commands = cleanedStringList(commands)
	if len(commands) == 0 {
		return nil
	}
	for index, command := range commands {
		resolvedCommand, err := resolveMCPInstallCommand(command, workDir)
		if err != nil {
			return fmt.Errorf("MCP %s %s命令模板无效：%w", dependency.Name, mcpPhaseText(phase), err)
		}
		progress := startProgress
		if len(commands) > 1 {
			progress = startProgress + index*(endProgress-startProgress)/len(commands)
		}
		r.emit(preflightStatusRunning, "mcp", 66, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemMCP,
			ItemID:          item.id,
			Phase:           phase,
			Status:          "running",
			Message:         fmt.Sprintf("正在执行 MCP %s：%s", mcpPhaseText(phase), dependency.Name),
			ProgressPercent: progress,
			Details: map[string]any{
				"mcp_name": dependency.Name,
				"command":  resolvedCommand,
				"index":    index + 1,
				"total":    len(commands),
			},
		}})
		output, err := runShellCommand(context.Background(), resolvedCommand, workDir, env, 10*time.Minute)
		if err != nil {
			return fmt.Errorf("MCP %s %s失败：%v；输出：%s", dependency.Name, mcpPhaseText(phase), err, summarizeCommandOutput(output))
		}
		r.emit(preflightStatusRunning, "mcp", 66, []model.RuntimeInstallEvent{{
			ItemType:        preflightItemMCP,
			ItemID:          item.id,
			Phase:           phase,
			Status:          "success",
			Message:         fmt.Sprintf("MCP %s完成：%s", mcpPhaseText(phase), dependency.Name),
			ProgressPercent: endProgress,
			Details: map[string]any{
				"mcp_name": dependency.Name,
				"output":   summarizeCommandOutput(output),
				"index":    index + 1,
				"total":    len(commands),
			},
		}})
	}
	return nil
}

func resolveMCPInstallCommand(command string, installRoot string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errors.New("命令不能为空")
	}
	if containsUnresolvedMCPPlaceholder(command) {
		return "", fmt.Errorf("仍包含示例占位路径 %q", command)
	}
	command = strings.ReplaceAll(command, mcpHomePlaceholder, installRoot)
	if strings.Contains(command, "${MCP_HOME") {
		return "", fmt.Errorf("MCP_HOME 模板格式不正确：%s", command)
	}
	return command, nil
}

func mcpServerFromInstalledDependency(runtimeDir string, dependency model.SemanticAgentMCPDependency) (model.DeviceMCPServerInfo, error) {
	server := dependency.Config
	server.Name = firstNonEmpty(server.Name, dependency.Name, dependency.ID)
	server.Type = firstNonEmpty(server.Type, dependency.Type, "local")
	server.Enabled = true
	if len(cleanedStringList(dependency.Install.RunCommandTemplate)) > 0 {
		server.Command = append([]string(nil), dependency.Install.RunCommandTemplate...)
	}
	if strings.TrimSpace(dependency.Install.ConfigPathTemplate) != "" {
		configPath, err := resolveMCPPathTemplate(runtimeDir, dependency.ID, dependency.Install.ConfigPathTemplate)
		if err != nil {
			return model.DeviceMCPServerInfo{}, err
		}
		if server.Environment == nil {
			server.Environment = map[string]string{}
		}
		server.Environment["MCP_CONFIG"] = configPath
	}
	if len(cleanedStringList(server.Command)) == 0 && server.Type != "remote" {
		return model.DeviceMCPServerInfo{}, fmt.Errorf("MCP %s 安装完成，但仍缺少启动命令模板", server.Name)
	}
	return server, nil
}

func mcpInstallRootHasContent(installRoot string) bool {
	entries, err := os.ReadDir(installRoot)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

func reusableMCPInstallRoot(installRoot string, server model.DeviceMCPServerInfo) (bool, string) {
	switch strings.TrimSpace(server.Type) {
	case "remote":
		if strings.TrimSpace(server.URL) == "" {
			return false, "远程 MCP 缺少 URL"
		}
		return true, ""
	case "", "local":
	default:
		return false, "不支持的 MCP 类型：" + server.Type
	}
	command := cleanedStringList(server.Command)
	if len(command) == 0 {
		return false, "本地 MCP 缺少启动命令"
	}
	checkedInstallPath := false
	for index, arg := range command {
		path, ok := mcpCommandPathUnderRoot(installRoot, arg)
		if !ok {
			continue
		}
		checkedInstallPath = true
		if index == 0 {
			if !isExecutable(path) {
				return false, "启动命令不存在或不可执行：" + path
			}
			continue
		}
		if !pathExists(path) {
			return false, "启动命令引用的文件不存在：" + path
		}
	}
	if checkedInstallPath {
		return true, ""
	}
	first := strings.TrimSpace(command[0])
	if filepath.IsAbs(first) && !isExecutable(first) {
		return false, "启动命令不存在或不可执行：" + first
	}
	return true, ""
}

func mcpCommandPathUnderRoot(installRoot string, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || !filepath.IsAbs(value) {
		return "", false
	}
	if !isPathUnder(installRoot, value) {
		return "", false
	}
	return value, true
}

func runShellCommand(ctx context.Context, command string, workDir string, env []string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(runCtx, "cmd", "/C", command)
	} else {
		shell := strings.TrimSpace(os.Getenv("SHELL"))
		if shell == "" {
			shell = "/bin/sh"
		}
		cmd = exec.CommandContext(runCtx, shell, "-lc", command)
	}
	backgroundcmd.Configure(cmd)
	cmd.Dir = workDir
	cmd.Env = env
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return output.String(), fmt.Errorf("命令执行超时")
	}
	return output.String(), err
}

func mcpPhaseText(phase string) string {
	switch phase {
	case "install":
		return "安装"
	case "build":
		return "构建"
	default:
		return phase
	}
}

func summarizeCommandOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	output = strings.ReplaceAll(output, "\r\n", "\n")
	if len(output) <= 1200 {
		return output
	}
	return output[len(output)-1200:]
}

func (r *runtimePreflightRunner) applySemanticAgent() (model.DeviceAgentSemanticSelectionInfo, error) {
	r.updateItem(preflightItemApply, "switch-agent", preflightStatusRunning, 30, "")
	r.emit(preflightStatusRunning, "apply", 90, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemApply,
		ItemID:          "switch-agent",
		Phase:           "apply",
		Status:          "running",
		Message:         "正在切换语义 Agent",
		ProgressPercent: 90,
	}})
	selection, err := r.service.SaveAgentSemanticSelection(model.DeviceAgentSemanticSelectionInput{
		AgentID:             r.payload.LauncherAgentID,
		SemanticAgentID:     r.payload.SemanticAgentID,
		ApplyRecommendedMCP: r.payload.ApplyRecommendedMCP,
		DisableVerifyMCP:    r.payload.DisableVerifyMCP,
		SemanticAgent:       r.payload.SemanticAgent,
	})
	if err != nil {
		r.updateItem(preflightItemApply, "switch-agent", preflightStatusFailed, 100, err.Error())
		return model.DeviceAgentSemanticSelectionInfo{}, err
	}
	if r.payload.RestartAfterApply {
		agent := r.service.agentSnapshot(r.payload.LauncherAgentID)
		if agent.Enabled {
			restarted, err := r.service.RestartAgent(r.payload.LauncherAgentID)
			if err != nil {
				r.updateItem(preflightItemApply, "switch-agent", preflightStatusFailed, 100, err.Error())
				return model.DeviceAgentSemanticSelectionInfo{}, err
			}
			if r.payload.ApplyRecommendedMCP {
				targetProfile := *r.payload.SemanticAgent
				if r.payload.DisableVerifyMCP {
					targetProfile = withoutVerifyMCPProfile(targetProfile)
				}
				targets := semanticMCPReadinessTargets(&targetProfile)
				for _, target := range targets {
					r.updateItem(preflightItemMCP, target.ItemID, preflightStatusRunning, 90, "")
				}
				if len(targets) > 0 {
					r.emit(preflightStatusRunning, "apply", 97, []model.RuntimeInstallEvent{{
						ItemType:        preflightItemMCP,
						Phase:           "handshake",
						Status:          "running",
						Message:         "正在验证 MCP initialize 与 tools/list",
						ProgressPercent: 90,
					}})
					ctx, cancel := context.WithTimeout(context.Background(), semanticMCPReadinessTimeout)
					err = waitForSemanticMCPReadiness(ctx, restarted.Port, targets)
					cancel()
					if err != nil {
						itemFailures, attributed := semanticMCPReadinessItemFailures(err)
						for _, target := range targets {
							if message, failed := itemFailures[target.ItemID]; attributed && failed {
								r.updateItem(preflightItemMCP, target.ItemID, preflightStatusFailed, 100, message)
							} else if attributed {
								r.updateItem(preflightItemMCP, target.ItemID, preflightStatusCompleted, 100, "")
							} else {
								r.updateItem(preflightItemMCP, target.ItemID, preflightStatusFailed, 100, err.Error())
							}
						}
						r.updateItem(preflightItemApply, "switch-agent", preflightStatusFailed, 100, err.Error())
						return model.DeviceAgentSemanticSelectionInfo{}, err
					}
					for _, target := range targets {
						r.updateItem(preflightItemMCP, target.ItemID, preflightStatusCompleted, 100, "")
					}
				}
			}
		}
	}
	r.updateItem(preflightItemApply, "switch-agent", preflightStatusCompleted, 100, "")
	r.emit(preflightStatusRunning, "apply", 96, []model.RuntimeInstallEvent{{
		ItemType:        preflightItemApply,
		ItemID:          "switch-agent",
		Phase:           "apply",
		Status:          "success",
		Message:         "语义 Agent 已应用，MCP 工具状态已验证",
		ProgressPercent: 96,
	}})
	return selection, nil
}

func (r *runtimePreflightRunner) fail(step string, err error) {
	if err == nil {
		return
	}
	r.emit(preflightStatusFailed, step, 100, []model.RuntimeInstallEvent{{
		ItemType:        "job",
		Phase:           "failed",
		Status:          "error",
		Message:         err.Error(),
		ProgressPercent: 100,
	}})
}

func (r *runtimePreflightRunner) startReporter() {
	r.reportCh = make(chan preflightReport, preflightReportBufferSize)
	r.reportDone = make(chan struct{})
	r.reportWg.Add(1)
	go func() {
		defer r.reportWg.Done()
		defer close(r.reportDone)
		for report := range r.reportCh {
			if strings.TrimSpace(report.payload.JobID) != "" {
				if err := r.reporter(report.payload); err != nil {
					r.setReportErr(err)
				} else {
					r.clearReportErr()
				}
			}
			if report.done != nil {
				close(report.done)
			}
		}
	}()
}

func (r *runtimePreflightRunner) stopReporter() {
	if r.reportCh != nil {
		close(r.reportCh)
		r.reportWg.Wait()
	}
}

func (r *runtimePreflightRunner) flushReports() {
	if r.reportCh == nil {
		return
	}
	done := make(chan struct{})
	r.reportCh <- preflightReport{done: done}
	<-done
}

func (r *runtimePreflightRunner) enqueueReport(payload model.RuntimePreflightStatusPayload) {
	if strings.TrimSpace(payload.JobID) == "" {
		return
	}
	if r.reportCh == nil {
		if err := r.reporter(payload); err != nil {
			r.setReportErr(err)
		}
		return
	}
	r.reportCh <- preflightReport{payload: payload}
}

func (r *runtimePreflightRunner) setReportErr(err error) {
	if err == nil {
		return
	}
	r.reportErrMu.Lock()
	defer r.reportErrMu.Unlock()
	if r.reportErr == nil {
		r.reportErr = err
	}
}

func (r *runtimePreflightRunner) clearReportErr() {
	r.reportErrMu.Lock()
	r.reportErr = nil
	r.reportErrMu.Unlock()
}

func (r *runtimePreflightRunner) reporterError() error {
	r.reportErrMu.Lock()
	defer r.reportErrMu.Unlock()
	return r.reportErr
}

func (r *runtimePreflightRunner) itemsSnapshot() []model.RuntimeInstallJobItem {
	r.itemMu.Lock()
	defer r.itemMu.Unlock()
	return clonePreflightItems(r.items)
}

func (r *runtimePreflightRunner) nextProgress(progress int) int {
	progress = clampProgress(progress)
	r.progressMu.Lock()
	defer r.progressMu.Unlock()
	if progress < r.lastProgress {
		return r.lastProgress
	}
	r.lastProgress = progress
	return progress
}

func (r *runtimePreflightRunner) markRuntimeTaskProgress(progress int) {
	_ = r.nextProgress(progress)
}

func (r *runtimePreflightRunner) emit(status string, step string, progress int, events []model.RuntimeInstallEvent) {
	progress = r.nextProgress(progress)
	for i := range events {
		events[i].JobID = r.payload.JobID
		if events[i].CreatedAt.IsZero() {
			events[i].CreatedAt = time.Now().UTC()
		}
		if events[i].ProgressPercent == 0 {
			events[i].ProgressPercent = clampProgress(progress)
		}
	}
	r.enqueueReport(model.RuntimePreflightStatusPayload{
		JobID:           r.payload.JobID,
		MachineID:       r.payload.MachineID,
		LauncherAgentID: r.payload.LauncherAgentID,
		SemanticAgentID: r.payload.SemanticAgentID,
		Status:          status,
		CurrentStep:     step,
		ProgressPercent: progress,
		Items:           r.itemsSnapshot(),
		Events:          events,
	})
}

func (r *runtimePreflightRunner) emitUserAction(step string, progress int, action model.RuntimeUserAction, events []model.RuntimeInstallEvent) {
	progress = r.nextProgress(progress)
	for i := range events {
		events[i].JobID = r.payload.JobID
		if events[i].CreatedAt.IsZero() {
			events[i].CreatedAt = time.Now().UTC()
		}
		if events[i].ProgressPercent == 0 {
			events[i].ProgressPercent = clampProgress(progress)
		}
	}
	r.enqueueReport(model.RuntimePreflightStatusPayload{
		JobID:              r.payload.JobID,
		MachineID:          r.payload.MachineID,
		LauncherAgentID:    r.payload.LauncherAgentID,
		SemanticAgentID:    r.payload.SemanticAgentID,
		Status:             preflightStatusWaitingUserAction,
		CurrentStep:        step,
		ProgressPercent:    progress,
		RequiresUserAction: true,
		UserAction:         &action,
		Items:              r.itemsSnapshot(),
		Events:             events,
	})
	r.flushReports()
}

func (r *runtimePreflightRunner) updateItem(itemType string, itemID string, status string, progress int, errText string) {
	now := time.Now().UTC()
	r.itemMu.Lock()
	defer r.itemMu.Unlock()
	for i := range r.items {
		if r.items[i].ItemType == itemType && r.items[i].ItemID == itemID {
			r.items[i].Status = status
			r.items[i].ProgressPercent = clampProgress(progress)
			r.items[i].Error = strings.TrimSpace(errText)
			r.items[i].UpdatedAt = now
			return
		}
	}
	r.items = append(r.items, model.RuntimeInstallJobItem{
		JobID:           r.payload.JobID,
		ItemType:        itemType,
		ItemID:          itemID,
		Name:            itemID,
		Required:        true,
		Status:          status,
		ProgressPercent: clampProgress(progress),
		Error:           strings.TrimSpace(errText),
		UpdatedAt:       now,
	})
}

func buildPreflightItems(payload model.RuntimePreflightStartPayload) []model.RuntimeInstallJobItem {
	now := time.Now().UTC()
	var items []model.RuntimeInstallJobItem
	runtimeNames := map[string]string{}
	for _, runtimeItem := range payload.Runtimes {
		runtimeNames[runtimeItem.ID] = firstNonEmpty(runtimeItem.Name, runtimeItem.ID)
	}
	if payload.SemanticAgent != nil {
		hasUV := false
		needsUV := false
		for _, req := range payload.SemanticAgent.RuntimeRequirements {
			runtimeID := strings.TrimSpace(req.RuntimeID)
			if runtimeID == "" {
				continue
			}
			if runtimeID == "uv" {
				hasUV = true
			}
			if item, ok := runtimeItemByID(payload.Runtimes, runtimeID); ok && normalizeRuntimeInstallStrategy(item.InstallStrategy) == "uv_python" {
				needsUV = true
			}
			items = append(items, model.RuntimeInstallJobItem{
				JobID:     payload.JobID,
				ItemType:  preflightItemRuntime,
				ItemID:    runtimeID,
				Name:      firstNonEmpty(runtimeNames[runtimeID], runtimeID),
				Required:  req.Required,
				Status:    preflightStatusPending,
				UpdatedAt: now,
			})
		}
		if needsUV && !hasUV {
			items = append(items, model.RuntimeInstallJobItem{
				JobID:     payload.JobID,
				ItemType:  preflightItemRuntime,
				ItemID:    "uv",
				Name:      firstNonEmpty(runtimeNames["uv"], "uv"),
				Required:  true,
				Status:    preflightStatusPending,
				UpdatedAt: now,
			})
		}
		profile := payload.SemanticAgent
		if payload.DisableVerifyMCP {
			filtered := withoutVerifyMCPProfile(*profile)
			profile = &filtered
		}
		for _, item := range semanticPreflightMCPItems(profile) {
			items = append(items, model.RuntimeInstallJobItem{
				JobID:     payload.JobID,
				ItemType:  preflightItemMCP,
				ItemID:    item.id,
				Name:      item.name,
				Required:  true,
				Status:    preflightStatusPending,
				UpdatedAt: now,
			})
		}
		if len(payload.SemanticAgent.SkillDefinitions) > 0 || len(payload.SemanticAgent.Skills) > 0 {
			items = append(items, model.RuntimeInstallJobItem{JobID: payload.JobID, ItemType: preflightItemSkill, ItemID: "semantic-skills", Name: "语义 Agent Skill", Required: true, Status: preflightStatusPending, UpdatedAt: now})
		}
	}
	items = append(items, model.RuntimeInstallJobItem{JobID: payload.JobID, ItemType: preflightItemApply, ItemID: "switch-agent", Name: "切换启动", Required: true, Status: preflightStatusPending, UpdatedAt: now})
	return items
}

func runtimeItemByID(items []model.RuntimeCatalogItem, id string) (model.RuntimeCatalogItem, bool) {
	id = strings.TrimSpace(id)
	for _, item := range items {
		if strings.TrimSpace(item.ID) == id {
			return item, true
		}
	}
	return model.RuntimeCatalogItem{}, false
}

type semanticPreflightMCPItem struct {
	id   string
	name string
}

func semanticPreflightMCPItems(profile *model.SemanticAgentProfile) []semanticPreflightMCPItem {
	if profile == nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]semanticPreflightMCPItem, 0)
	add := func(id string, name string) {
		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		if id == "" {
			id = name
		}
		if name == "" {
			name = id
		}
		idKey := strings.ToLower(id)
		nameKey := strings.ToLower(name)
		if id == "" || seen[idKey] || seen[nameKey] {
			return
		}
		seen[idKey] = true
		seen[nameKey] = true
		out = append(out, semanticPreflightMCPItem{id: id, name: name})
	}
	for index, id := range profile.MCPIDs {
		name := id
		if index < len(profile.RecommendedMCPServers) && strings.TrimSpace(profile.RecommendedMCPServers[index]) != "" {
			name = profile.RecommendedMCPServers[index]
		}
		add(id, name)
	}
	for _, server := range profile.RecommendedMCPConfigs {
		add(semanticPreflightMCPItemID(server), server.Name)
	}
	for _, name := range profile.RecommendedMCPServers {
		add(name, name)
	}
	for _, dependency := range profile.MCPDependencies {
		add(dependency.ID, firstNonEmpty(dependency.Name, dependency.Title, dependency.ID))
	}
	return out
}

func semanticPreflightMCPDependencies(profile *model.SemanticAgentProfile) map[string]model.SemanticAgentMCPDependency {
	out := map[string]model.SemanticAgentMCPDependency{}
	if profile == nil {
		return out
	}
	for _, item := range profile.MCPDependencies {
		item.ID = strings.TrimSpace(item.ID)
		item.Name = strings.TrimSpace(item.Name)
		if item.ID == "" {
			item.ID = item.Name
		}
		if item.Name == "" {
			item.Name = item.ID
		}
		if item.ID == "" {
			continue
		}
		out[strings.ToLower(item.ID)] = item
		if item.Name != "" {
			out[strings.ToLower(item.Name)] = item
		}
	}
	return out
}

func semanticPreflightMCPItemID(server model.DeviceMCPServerInfo) string {
	name := strings.TrimSpace(server.Name)
	if name != "" {
		return name
	}
	return strings.TrimSpace(server.URL)
}

func semanticPreflightMCPConfigItemID(profile *model.SemanticAgentProfile, server model.DeviceMCPServerInfo) string {
	serverName := strings.TrimSpace(server.Name)
	if profile != nil && serverName != "" {
		for _, item := range semanticPreflightMCPItems(profile) {
			if strings.EqualFold(strings.TrimSpace(item.name), serverName) || strings.EqualFold(strings.TrimSpace(item.id), serverName) {
				return item.id
			}
		}
	}
	return semanticPreflightMCPItemID(server)
}

func clonePreflightItems(input []model.RuntimeInstallJobItem) []model.RuntimeInstallJobItem {
	out := make([]model.RuntimeInstallJobItem, len(input))
	copy(out, input)
	return out
}

func uniqueStrings(input []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(input))
	for _, item := range input {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func sameStringSet(left []string, right []string) bool {
	left = uniqueStrings(left)
	right = uniqueStrings(right)
	if len(left) != len(right) {
		return false
	}
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func mergeMCPServers(existing []model.DeviceMCPServerInfo, recommended []model.DeviceMCPServerInfo, defaultEnabled bool) []model.DeviceMCPServerInfo {
	byName := map[string]model.DeviceMCPServerInfo{}
	order := []string{}
	for _, server := range existing {
		server.Name = strings.TrimSpace(server.Name)
		if server.Name == "" {
			continue
		}
		if _, ok := byName[server.Name]; !ok {
			order = append(order, server.Name)
		}
		byName[server.Name] = server
	}
	for _, server := range recommended {
		server.Name = strings.TrimSpace(server.Name)
		if server.Name == "" {
			continue
		}
		if _, ok := byName[server.Name]; !ok {
			order = append(order, server.Name)
			server.Enabled = defaultEnabled
		} else {
			server.Enabled = byName[server.Name].Enabled
		}
		byName[server.Name] = server
	}
	out := make([]model.DeviceMCPServerInfo, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

func selectRuntimeVersion(item model.RuntimeCatalogItem, constraint string) (model.RuntimeVersion, error) {
	enabled := make([]model.RuntimeVersion, 0, len(item.Versions))
	for _, version := range item.Versions {
		if version.Enabled {
			enabled = append(enabled, version)
		}
	}
	sort.SliceStable(enabled, func(i, j int) bool {
		if enabled[i].DefaultSelected != enabled[j].DefaultSelected {
			return enabled[i].DefaultSelected
		}
		if enabled[i].VersionOrder != enabled[j].VersionOrder {
			return enabled[i].VersionOrder < enabled[j].VersionOrder
		}
		return enabled[i].SortOrder < enabled[j].SortOrder
	})
	constraint = strings.TrimSpace(constraint)
	for _, version := range enabled {
		if runtimeVersionMatches(version.Version, constraint) {
			return version, nil
		}
	}
	if constraint == "" {
		for _, version := range enabled {
			if version.DefaultSelected {
				return version, nil
			}
		}
		if len(enabled) > 0 {
			return enabled[0], nil
		}
	}
	return model.RuntimeVersion{}, fmt.Errorf("运行时 %s 没有匹配版本约束 %q 的启用版本", item.ID, constraint)
}

func runtimeVersionMatches(version string, constraint string) bool {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" || strings.EqualFold(constraint, "latest") {
		return true
	}
	for _, part := range strings.Fields(constraint) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch {
		case strings.HasSuffix(part, ".x"):
			prefix := strings.TrimSuffix(part, ".x")
			if !strings.HasPrefix(version, strings.TrimPrefix(prefix, "v")+".") && version != strings.TrimPrefix(prefix, "v") {
				return false
			}
		case strings.HasPrefix(part, ">=") || strings.HasPrefix(part, "<=") || strings.HasPrefix(part, ">") || strings.HasPrefix(part, "<"):
			if !compareSimpleVersion(version, part) {
				return false
			}
		default:
			want := strings.TrimPrefix(part, "v")
			if version != want && !strings.HasPrefix(version, want+".") {
				return false
			}
		}
	}
	return true
}

func compareSimpleVersion(version string, expr string) bool {
	op := ""
	want := ""
	for _, candidate := range []string{">=", "<=", ">", "<"} {
		if strings.HasPrefix(expr, candidate) {
			op = candidate
			want = strings.TrimPrefix(expr, candidate)
			break
		}
	}
	if op == "" {
		return true
	}
	left := versionParts(version)
	right := versionParts(want)
	cmp := compareVersionParts(left, right)
	switch op {
	case ">=":
		return cmp >= 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case "<":
		return cmp < 0
	default:
		return true
	}
}

func versionParts(value string) []int {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	pieces := strings.Split(value, ".")
	out := make([]int, 0, len(pieces))
	for _, piece := range pieces {
		var n int
		for _, r := range piece {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

func compareVersionParts(left []int, right []int) int {
	n := max(len(left), len(right))
	for i := 0; i < n; i++ {
		var a, b int
		if i < len(left) {
			a = left[i]
		}
		if i < len(right) {
			b = right[i]
		}
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
	}
	return 0
}

func matchesRuntimeVersion(runtimeID string, versionID string, wantRuntimeID string, wantVersionID string) bool {
	if strings.TrimSpace(runtimeID) != "" && runtimeID != wantRuntimeID {
		return false
	}
	if strings.TrimSpace(versionID) != "" && versionID != wantVersionID {
		return false
	}
	return true
}

func matchesPlatformArch(platform string, arch string) bool {
	platform = strings.ToLower(strings.TrimSpace(platform))
	arch = strings.ToLower(strings.TrimSpace(arch))
	currentPlatform, currentArch := runtimePlatformForPreflight()
	if platform != "" && platform != "all" && platform != "any" && platform != currentPlatform {
		return false
	}
	if arch != "" && arch != "all" && arch != "any" && arch != currentArch {
		return false
	}
	return true
}

func currentRuntimePlatform() string {
	platform, _ := runtimePlatformForPreflight()
	return platform
}

func currentRuntimeArch() string {
	_, arch := runtimePlatformForPreflight()
	return arch
}

func renderRuntimeURL(mirror model.RuntimeMirror, artifact model.RuntimeArtifact, version model.RuntimeVersion) string {
	base := strings.TrimRight(strings.TrimSpace(mirror.BaseURL), "/")
	template := strings.TrimSpace(mirror.URLTemplate)
	if template == "" {
		template = "{base}/{filename}"
	}
	platform := currentRuntimePlatform()
	arch := currentRuntimeArch()
	replacer := strings.NewReplacer(
		"{base}", base,
		"{version}", version.Version,
		"{version_id}", version.ID,
		"{filename}", artifact.Filename,
		"{platform}", platform,
		"{arch}", arch,
		"{adoptium_os}", adoptiumOS(platform),
		"{adoptium_arch}", adoptiumArch(arch),
	)
	return replacer.Replace(template)
}

func adoptiumOS(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "darwin":
		return "mac"
	default:
		return strings.ToLower(strings.TrimSpace(platform))
	}
}

func adoptiumArch(arch string) string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "amd64":
		return "x64"
	case "arm64":
		return "aarch64"
	default:
		return strings.ToLower(strings.TrimSpace(arch))
	}
}

func runtimeCandidateFilename(rawURL string) string {
	parsed, err := urlpkg.Parse(rawURL)
	if err == nil {
		if base := filepath.Base(parsed.Path); base != "." && base != "/" {
			return base
		}
	}
	return "runtime-archive"
}

func normalizePackageKind(kind string, archivePath string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	kind = strings.TrimPrefix(kind, ".")
	if kind == "archive" || kind == "" {
		lower := strings.ToLower(archivePath)
		switch {
		case strings.HasSuffix(lower, ".zip"):
			return "zip"
		case strings.HasSuffix(lower, ".tar.gz"):
			return "tar.gz"
		case strings.HasSuffix(lower, ".tgz"):
			return "tgz"
		}
	}
	return kind
}

func cleanedMCPInstallAssets(input []model.MCPInstallAsset) []model.MCPInstallAsset {
	out := make([]model.MCPInstallAsset, 0, len(input))
	for _, item := range input {
		item.Name = strings.TrimSpace(item.Name)
		item.URL = strings.TrimSpace(item.URL)
		item.Filename = strings.TrimSpace(item.Filename)
		item.SHA256 = strings.TrimSpace(item.SHA256)
		item.PackageKind = strings.TrimSpace(item.PackageKind)
		item.TargetDir = strings.TrimSpace(item.TargetDir)
		item.Headers = cleanedStringMap(item.Headers)
		if item.URL == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func cleanedMCPInstallFiles(input []model.MCPInstallFile) []model.MCPInstallFile {
	out := make([]model.MCPInstallFile, 0, len(input))
	for _, item := range input {
		item.Path = strings.TrimSpace(item.Path)
		if item.Path == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func runtimeManifestPath(runtimeDir string, runtimeID string, version string) string {
	name := safeRuntimePathPart(runtimeID) + "-" + safeRuntimePathPart(version) + ".json"
	return filepath.Join(runtimeDir, "runtime-manifests", name)
}

func writeRuntimeManifest(runtimeDir string, manifest installedRuntimeManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(runtimeManifestPath(runtimeDir, manifest.RuntimeID, manifest.Version), append(data, '\n'), 0o644)
}

func runtimeManifestBinDirs(manifest installedRuntimeManifest) []string {
	var dirs []string
	for _, rel := range manifest.BinPaths {
		rel = filepath.Clean(strings.TrimSpace(rel))
		if rel == "." || rel == "" {
			dirs = append(dirs, manifest.InstallDir)
			continue
		}
		dir := filepath.Join(manifest.InstallDir, rel)
		if pathExists(dir) {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		for _, rel := range []string{"bin", filepath.Join("Scripts"), "."} {
			dir := filepath.Join(manifest.InstallDir, rel)
			if pathExists(dir) {
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs
}

func inferRuntimeBinPaths(installDir string) []string {
	for _, rel := range []string{"bin", "Scripts"} {
		if pathExists(filepath.Join(installDir, rel)) {
			return []string{rel}
		}
	}
	return []string{"."}
}

func lookPathInDirs(name string, pathValue string) (string, error) {
	for _, dir := range filepath.SplitList(pathValue) {
		candidate := filepath.Join(dir, name)
		if runtime.GOOS == "windows" && filepath.Ext(candidate) == "" {
			for _, ext := range []string{".exe", ".cmd", ".bat"} {
				if isExecutable(candidate + ext) {
					return candidate + ext, nil
				}
			}
		}
		if isExecutable(candidate) {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

func verifySHA256(path string, expected string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return errors.New("缺少 sha256")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("sha256 校验失败 expected=%s actual=%s", expected, actual)
	}
	return nil
}

func unzipRuntimeArchive(path string, target string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		targetPath, ok := safeArchiveTarget(target, file.Name)
		if !ok {
			continue
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, file.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := errors.Join(src.Close(), dst.Close())
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
		if mode := file.Mode(); mode != 0 {
			_ = os.Chmod(targetPath, mode)
		}
	}
	return nil
}

func untarRuntimeArchive(path string, target string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		targetPath, ok := safeArchiveTarget(target, header.Name)
		if !ok {
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(dst, reader)
			closeErr := dst.Close()
			if copyErr != nil || closeErr != nil {
				return errors.Join(copyErr, closeErr)
			}
		case tar.TypeSymlink:
			if err := createRuntimeArchiveSymlink(target, targetPath, header.Linkname); err != nil {
				return err
			}
		case tar.TypeLink:
			if err := createRuntimeArchiveHardLink(target, targetPath, header.Linkname); err != nil {
				return err
			}
		}
	}
}

func createRuntimeArchiveSymlink(root string, targetPath string, linkName string) error {
	linkName = strings.TrimSpace(linkName)
	if linkName == "" || filepath.IsAbs(linkName) {
		return nil
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(targetPath), linkName))
	if !isPathUnder(root, resolved) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	_ = os.RemoveAll(targetPath)
	return os.Symlink(linkName, targetPath)
}

func createRuntimeArchiveHardLink(root string, targetPath string, linkName string) error {
	sourcePath, ok := safeArchiveTarget(root, linkName)
	if !ok || !isPathUnder(root, sourcePath) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	_ = os.RemoveAll(targetPath)
	if err := os.Link(sourcePath, targetPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func safeArchiveTarget(root string, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
		return "", false
	}
	target := filepath.Join(root, clean)
	if !isPathUnder(root, target) {
		return "", false
	}
	return target, true
}

func safeMCPInstallPath(installRoot string, value string) (string, error) {
	installRoot = filepath.Clean(strings.TrimSpace(installRoot))
	if installRoot == "" {
		return "", errors.New("MCP 安装目录不能为空")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return installRoot, nil
	}
	if containsUnresolvedMCPPlaceholder(value) {
		return "", fmt.Errorf("MCP 安装路径仍包含示例占位：%s", value)
	}
	value = strings.ReplaceAll(value, mcpHomePlaceholder, installRoot)
	if strings.Contains(value, "${MCP_HOME") {
		return "", fmt.Errorf("MCP 安装路径模板格式不正确：%s", value)
	}
	resolved := value
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(installRoot, resolved)
	}
	resolved = filepath.Clean(resolved)
	if !isPathUnder(installRoot, resolved) {
		return "", fmt.Errorf("MCP 安装路径越界：%s", value)
	}
	return resolved, nil
}

func copyFile(source string, target string, mode os.FileMode) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()
	if mode == 0 {
		mode = 0o644
	}
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	return os.Chmod(target, mode)
}

func isPathUnder(root string, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

func singleChildDir(root string) string {
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		return ""
	}
	return filepath.Join(root, entries[0].Name())
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isHTTPURL(raw string) bool {
	parsed, err := urlpkg.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func safeRuntimePathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return replacer.Replace(value)
}

func pathKey() string {
	if runtime.GOOS == "windows" {
		return "Path"
	}
	return "PATH"
}
