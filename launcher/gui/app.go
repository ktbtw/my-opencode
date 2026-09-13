package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"launcher/internal/autostart"
	"launcher/internal/bootstrap"
	"launcher/internal/config"
	"launcher/internal/defaults"
	"launcher/internal/macapp"
	"launcher/internal/model"
	proc "launcher/internal/process"
	"launcher/internal/selfupdate"
	launcherVersion "launcher/internal/version"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type GUIApp struct {
	ctx       context.Context
	mu        sync.Mutex
	cfg       config.Config
	logs      []string
	bootstrap BootstrapProgress
}

type LoginInput struct {
	ServerURL string `json:"server_url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

type LoginResult struct {
	Success         bool   `json:"success"`
	Message         string `json:"message,omitempty"`
	OperatorName    string `json:"operator_name,omitempty"`
	OperatorKey     string `json:"operator_key,omitempty"`
	OperatorKeyMask string `json:"operator_key_mask,omitempty"`
}

type SSHTunnelResult struct {
	TunnelID       string `json:"tunnel_id,omitempty"`
	Command        string `json:"command"`
	UnixCommand    string `json:"unix_command"`
	WindowsCommand string `json:"windows_command"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	Target         string `json:"target,omitempty"`
}

type guiSession struct {
	ServerURL   string `json:"server_url"`
	AccessToken string `json:"access_token"`
}

type SavedLogin struct {
	ServerURL string `json:"server_url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	AutoLogin bool   `json:"auto_login"`
	HasSaved  bool   `json:"has_saved"`
}

type GUISettings struct {
	TerminalCompat bool `json:"terminal_compat"`
}

type BootstrapResult struct {
	Ready     bool              `json:"ready"`
	Message   string            `json:"message,omitempty"`
	State     model.DeviceView  `json:"state,omitempty"`
	Logs      []string          `json:"logs,omitempty"`
	Bootstrap BootstrapProgress `json:"bootstrap,omitempty"`
}

type BootstrapProgress struct {
	Phase     string `json:"phase,omitempty"`
	Message   string `json:"message,omitempty"`
	Percent   int    `json:"percent,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

func NewGUIApp() *GUIApp {
	return &GUIApp{}
}

func (g *GUIApp) startup(ctx context.Context) {
	g.ctx = ctx
	if g.ensureInstalledMacAppAndRelaunch() {
		return
	}
	if err := macapp.RepairInstalledMetadata(launcherVersion.Value); err != nil {
		g.appendLog("修复码控应用元数据失败: " + err.Error())
	}
	g.migrateAutostartToBackground()
}

func (g *GUIApp) shutdown(ctx context.Context) {
	g.migrateAutostartToBackground()
	g.appendLog("GUI 已关闭，后台 launcher 保持运行")
}

func (g *GUIApp) Defaults() map[string]any {
	return map[string]any{
		"server_url":       defaults.PublicBase,
		"launcher_version": launcherVersion.Value,
	}
}

func (g *GUIApp) LoadSavedLogin() (SavedLogin, error) {
	cfg, err := config.Load()
	if err != nil {
		return SavedLogin{}, err
	}
	path := savedLoginPath(cfg.RuntimeDir)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return SavedLogin{ServerURL: defaults.PublicBase, AutoLogin: true, HasSaved: false}, nil
	}
	if err != nil {
		return SavedLogin{}, err
	}
	var saved SavedLogin
	if err := json.Unmarshal(data, &saved); err != nil {
		return SavedLogin{}, fmt.Errorf("读取自动登录配置失败: %w", err)
	}
	saved.ServerURL = normalizeServerURL(firstNonEmpty(saved.ServerURL, defaults.PublicBase))
	saved.HasSaved = strings.TrimSpace(saved.Username) != "" && strings.TrimSpace(saved.Password) != ""
	return saved, nil
}

func (g *GUIApp) SaveSavedLogin(input SavedLogin) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	username := strings.TrimSpace(input.Username)
	password := input.Password
	if username == "" || strings.TrimSpace(password) == "" {
		return errors.New("账号和密码不能为空")
	}
	saved := SavedLogin{
		ServerURL: normalizeServerURL(firstNonEmpty(input.ServerURL, defaults.PublicBase)),
		Username:  username,
		Password:  password,
		AutoLogin: input.AutoLogin,
		HasSaved:  true,
	}
	path := savedLoginPath(cfg.RuntimeDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return nil
}

func (g *GUIApp) ClearSavedLogin() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	err = os.Remove(savedLoginPath(cfg.RuntimeDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (g *GUIApp) Login(input LoginInput) (LoginResult, error) {
	serverURL := normalizeServerURL(input.ServerURL)
	username := strings.TrimSpace(input.Username)
	password := input.Password
	if username == "" || strings.TrimSpace(password) == "" {
		return LoginResult{}, errors.New("请输入账号和密码")
	}
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, strings.TrimRight(serverURL, "/")+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		return LoginResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return LoginResult{}, fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return LoginResult{}, fmt.Errorf("登录失败: %s", strings.TrimSpace(string(data)))
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
		Operator    struct {
			Name        string `json:"name"`
			Username    string `json:"username"`
			OperatorKey string `json:"operator_key"`
		} `json:"operator"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return LoginResult{}, fmt.Errorf("登录响应解析失败: %w", err)
	}
	operatorKey := strings.TrimSpace(parsed.Operator.OperatorKey)
	accessToken := strings.TrimSpace(parsed.AccessToken)
	if operatorKey == "" {
		return LoginResult{}, errors.New("登录响应缺少 operator_key")
	}
	if accessToken == "" {
		return LoginResult{}, errors.New("登录响应缺少 access_token")
	}
	cfg, err := config.Load()
	if err != nil {
		return LoginResult{}, err
	}
	if err := bootstrap.StoreOperatorKey(cfg.RuntimeDir, operatorKey); err != nil {
		return LoginResult{}, err
	}
	if err := storeGUISession(cfg.RuntimeDir, guiSession{ServerURL: serverURL, AccessToken: accessToken}); err != nil {
		return LoginResult{}, err
	}
	g.appendLog("账号登录成功，operator key 已写入本地 launcher 配置")
	return LoginResult{
		Success:         true,
		Message:         "登录成功",
		OperatorName:    firstNonEmpty(parsed.Operator.Name, parsed.Operator.Username, username),
		OperatorKey:     operatorKey,
		OperatorKeyMask: maskKey(operatorKey),
	}, nil
}

func savedLoginPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "gui", "login.json")
}

func guiSettingsPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "gui", "settings.json")
}

func guiSessionPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "gui", "session.json")
}

func storeGUISession(runtimeDir string, session guiSession) error {
	if strings.TrimSpace(session.AccessToken) == "" {
		return errors.New("access token 不能为空")
	}
	dir := filepath.Dir(guiSessionPath(runtimeDir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil && runtime.GOOS != "windows" {
		return err
	}
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	if err := os.WriteFile(guiSessionPath(runtimeDir), data, 0o600); err != nil {
		return err
	}
	return nil
}

func loadGUISession(runtimeDir string) (guiSession, error) {
	data, err := os.ReadFile(guiSessionPath(runtimeDir))
	if err != nil {
		return guiSession{}, err
	}
	var session guiSession
	if err := json.Unmarshal(data, &session); err != nil {
		return guiSession{}, fmt.Errorf("读取 launcher 会话失败: %w", err)
	}
	if strings.TrimSpace(session.AccessToken) == "" {
		return guiSession{}, errors.New("launcher 会话缺少 access token")
	}
	session.ServerURL = normalizeServerURL(session.ServerURL)
	return session, nil
}

func (g *GUIApp) SSHStatus() (model.SSHStatus, error) {
	cfg, err := g.requireConfig()
	if err != nil {
		return model.SSHStatus{}, err
	}
	var result model.SSHStatus
	if err := g.controlJSON(cfg, http.MethodGet, "/ssh/status", nil, &result); err != nil {
		return model.SSHStatus{}, err
	}
	return result, nil
}

func (g *GUIApp) SetupSSH() (model.SSHSetupResult, error) {
	cfg, err := g.requireConfig()
	if err != nil {
		return model.SSHSetupResult{}, err
	}
	var result model.SSHSetupResult
	if err := g.controlJSONWithTimeout(cfg, http.MethodPost, "/ssh/setup", map[string]any{}, &result, 15*time.Minute); err != nil {
		return model.SSHSetupResult{}, err
	}
	return result, nil
}

func (g *GUIApp) InstallSSHAuthorizedKey(publicKey string) (model.SSHAuthorizedKeyResult, error) {
	cfg, err := g.requireConfig()
	if err != nil {
		return model.SSHAuthorizedKeyResult{}, err
	}
	var result model.SSHAuthorizedKeyResult
	if err := g.controlJSON(cfg, http.MethodPost, "/ssh/authorized-key", model.SSHAuthorizedKeyInput{PublicKey: publicKey}, &result); err != nil {
		return model.SSHAuthorizedKeyResult{}, err
	}
	return result, nil
}

func (g *GUIApp) CreateSSHTunnel() (SSHTunnelResult, error) {
	cfg, err := g.currentConfig()
	if err != nil {
		return SSHTunnelResult{}, err
	}
	username := currentSSHUsername()
	var response struct {
		TunnelID             string    `json:"tunnel_id"`
		BootstrapUnixPath    string    `json:"bootstrap_unix_path"`
		BootstrapWindowsPath string    `json:"bootstrap_windows_path"`
		Target               string    `json:"target"`
		ExpiresAt            time.Time `json:"expires_at"`
	}
	if err := g.relayJSON(cfg, http.MethodPost, "/api/devices/"+urlPath(cfg.Relay.MachineID)+"/tunnels", map[string]any{
		"machine_id": cfg.Relay.MachineID, "username": username, "bootstrap_version": 1,
	}, &response); err != nil {
		return SSHTunnelResult{}, err
	}
	serverURL, err := g.sessionServerURL(cfg)
	if err != nil {
		return SSHTunnelResult{}, err
	}
	if strings.TrimSpace(response.BootstrapUnixPath) == "" || strings.TrimSpace(response.BootstrapWindowsPath) == "" {
		return SSHTunnelResult{}, errors.New("服务器未返回临时 SSH 脚本地址")
	}
	unixURL, err := tunnelBootstrapURL(serverURL, response.BootstrapUnixPath, "unix")
	if err != nil {
		return SSHTunnelResult{}, err
	}
	windowsURL, err := tunnelBootstrapURL(serverURL, response.BootstrapWindowsPath, "windows")
	if err != nil {
		return SSHTunnelResult{}, err
	}
	unixCommand := formatUnixTunnelBootstrapCommand(unixURL)
	windowsCommand := formatWindowsTunnelBootstrapCommand(windowsURL)
	return SSHTunnelResult{
		TunnelID: response.TunnelID, Command: unixCommand, UnixCommand: unixCommand, WindowsCommand: windowsCommand,
		ExpiresAt: response.ExpiresAt.Local().Format("2006-01-02 15:04:05"), Target: response.Target,
	}, nil
}

func tunnelBootstrapURL(serverURL, bootstrapPath, platform string) (string, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(serverURL), "/"))
	if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" || base.RawQuery != "" || base.Fragment != "" {
		return "", errors.New("服务器地址格式错误")
	}
	bootstrapPath = strings.TrimSpace(bootstrapPath)
	prefix := "/b/"
	suffix := "/" + platform
	if !strings.HasPrefix(bootstrapPath, prefix) || !strings.HasSuffix(bootstrapPath, suffix) {
		return "", errors.New("服务器返回的临时脚本地址格式错误")
	}
	token := strings.TrimSuffix(strings.TrimPrefix(bootstrapPath, prefix), suffix)
	if len(token) != 64 {
		return "", errors.New("服务器返回的临时脚本凭据格式错误")
	}
	if _, err := hex.DecodeString(token); err != nil {
		return "", errors.New("服务器返回的临时脚本凭据格式错误")
	}
	return strings.TrimRight(base.String(), "/") + bootstrapPath, nil
}

func formatUnixTunnelBootstrapCommand(bootstrapURL string) string {
	return "bash -o pipefail -c " + shellQuote("curl -fsSL "+bootstrapURL+" | bash")
}

func formatWindowsTunnelBootstrapCommand(bootstrapURL string) string {
	return "$ErrorActionPreference='Stop'; irm " + powerShellQuote(bootstrapURL) + " | iex"
}

func (g *GUIApp) sessionServerURL(cfg config.Config) (string, error) {
	session, err := loadGUISession(cfg.RuntimeDir)
	if err != nil {
		return "", err
	}
	return session.ServerURL, nil
}

func (g *GUIApp) relayJSON(cfg config.Config, method, path string, input, output any) error {
	session, err := loadGUISession(cfg.RuntimeDir)
	if err != nil {
		return errors.New("登录会话已失效，请重新登录")
	}
	body := io.Reader(nil)
	if input != nil {
		data, marshalErr := json.Marshal(input)
		if marshalErr != nil {
			return marshalErr
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, strings.TrimRight(session.ServerURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("服务器请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var parsed struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &parsed)
		if parsed.Error != "" {
			return errors.New(parsed.Error)
		}
		return fmt.Errorf("服务器请求失败: %s", resp.Status)
	}
	if output != nil {
		if err := json.Unmarshal(data, output); err != nil {
			return fmt.Errorf("服务器响应解析失败: %w", err)
		}
	}
	return nil
}

func currentSSHUsername() string {
	if runtime.GOOS == "windows" {
		if value := strings.TrimSpace(os.Getenv("USERNAME")); value != "" {
			return value
		}
	}
	if current, err := user.Current(); err == nil && strings.TrimSpace(current.Username) != "" {
		return current.Username
	}
	if value := strings.TrimSpace(os.Getenv("USERNAME")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("USER")); value != "" {
		return value
	}
	return "USER"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func powerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func (g *GUIApp) TerminalCompatSettings() (GUISettings, error) {
	cfg, err := config.Load()
	if err != nil {
		return GUISettings{}, err
	}
	return loadGUISettings(cfg.RuntimeDir)
}

func (g *GUIApp) SetTerminalCompatEnabled(enabled bool) (GUISettings, error) {
	cfg, err := g.currentConfig()
	if err != nil {
		return GUISettings{}, err
	}
	settings := GUISettings{TerminalCompat: enabled}
	if err := saveGUISettings(cfg.RuntimeDir, settings); err != nil {
		return GUISettings{}, err
	}
	g.appendLog(fmt.Sprintf("终端兼容模式已%s", map[bool]string{true: "开启", false: "关闭"}[enabled]))
	if autostart.Status().Enabled {
		if _, err := g.enableAutostartForConfig(cfg); err != nil {
			g.appendLog("更新终端兼容自启动配置失败: " + err.Error())
		}
	}
	if _, err := g.RestartBackground(); err != nil {
		g.appendLog("切换终端兼容模式后重启后台失败: " + err.Error())
	}
	return settings, nil
}

func loadGUISettings(runtimeDir string) (GUISettings, error) {
	settings := GUISettings{TerminalCompat: true}
	data, err := os.ReadFile(guiSettingsPath(runtimeDir))
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return GUISettings{}, err
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return GUISettings{}, fmt.Errorf("读取 GUI 设置失败: %w", err)
	}
	return settings, nil
}

func saveGUISettings(runtimeDir string, settings GUISettings) error {
	path := guiSettingsPath(runtimeDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (g *GUIApp) StartLauncher() (BootstrapResult, error) {
	g.setBootstrapProgress("loading_config", "正在读取本机 launcher 配置", 5, "")
	g.mu.Lock()
	logs := append([]string{}, g.logs...)
	g.mu.Unlock()

	cfg, err := config.Load()
	if err != nil {
		g.setBootstrapProgress("failed", "读取本机 launcher 配置失败", 100, err.Error())
		return BootstrapResult{}, err
	}
	g.mu.Lock()
	g.cfg = cfg
	g.mu.Unlock()

	g.setBootstrapProgress("checking_existing", "正在检查已有后台 launcher", 12, "")
	if state, err := g.controlState(cfg); err == nil {
		if g.terminalCompatEnabled() && !g.autostartUsesTerminalCompat() {
			g.setBootstrapProgress("configuring_autostart", "正在切换终端兼容启动模式", 20, "")
			if info, migrateErr := g.enableAutostartForConfig(cfg); migrateErr == nil {
				g.appendLog("已切换到终端兼容启动模式: " + info.Command)
				g.stopControlProcess(cfg)
				g.waitControlDown(cfg, 8*time.Second)
				if startErr := g.startBackgroundProcess(cfg); startErr != nil {
					g.appendLog("终端兼容模式重启后台失败: " + startErr.Error())
				}
				g.setBootstrapProgress("waiting_control", "正在等待终端兼容后台服务连接", 65, "")
				if restarted, waitErr := g.waitControlState(cfg, 45*time.Second); waitErr == nil {
					g.setBootstrapProgress("ready", "launcher 已通过终端兼容模式接管", 100, "")
					return g.bootstrapResult(true, "launcher 已通过终端兼容模式接管", restarted), nil
				}
			} else {
				g.appendLog("切换终端兼容启动模式失败: " + migrateErr.Error())
			}
		}
		g.setBootstrapProgress("checking_update", "正在确认后台 launcher 版本", 30, "")
		if refreshed, updated, updateErr := g.ensureBackgroundLauncherVersion(cfg, state); updateErr == nil && updated {
			g.appendLog("已将 launcher 后台服务同步到 GUI 当前版本")
			g.setBootstrapProgress("ready", "launcher 后台服务已同步并接管", 100, "")
			return g.bootstrapResult(true, "launcher 后台服务已同步并接管", refreshed), nil
		} else if updateErr != nil {
			g.appendLog("同步 launcher 后台版本失败: " + updateErr.Error())
		}
		g.appendLog("已接管正在运行的 launcher 后台服务")
		g.setBootstrapProgress("ready", "launcher 已运行，GUI 已接管", 100, "")
		return g.bootstrapResult(true, "launcher 已运行，GUI 已接管", state), nil
	}

	if autostart.Status().Enabled {
		g.setBootstrapProgress("starting_autostart", "正在通过自启动服务拉起后台 launcher", 25, "")
		g.migrateAutostartToBackground()
		if state, err := g.waitControlState(cfg, 25*time.Second); err == nil {
			g.appendLog("已通过自启动服务接管 launcher 后台")
			g.setBootstrapProgress("ready", "launcher 后台服务已启动", 100, "")
			return g.bootstrapResult(true, "launcher 后台服务已启动", state), nil
		}
	}

	if stopped, stopErr := g.stopControlProcessAndWait(cfg); stopErr != nil {
		err = fmt.Errorf("清理无响应的 launcher 后台服务失败: %w", stopErr)
		g.setBootstrapProgress("failed", "launcher 后台服务清理失败", 100, err.Error())
		return g.bootstrapResultWithLogs(false, "launcher 后台服务清理失败", model.DeviceView{}, logs), err
	} else if stopped {
		g.appendLog("已停止无响应的 launcher 后台服务，准备重新安装并启动")
	}
	if err := g.startBackgroundProcess(cfg); err != nil {
		g.setBootstrapProgress("failed", "launcher 后台服务启动失败", 100, err.Error())
		return g.bootstrapResultWithLogs(false, "launcher 后台服务启动失败", model.DeviceView{}, logs), err
	}
	g.setBootstrapProgress("waiting_control", "后台 launcher 已启动，正在等待本地控制口连接", 70, "")
	state, lastErr := g.waitControlState(cfg, 3*time.Minute)
	if lastErr == nil {
		g.appendLog("launcher 后台服务已启动，GUI 已接管")
		g.setBootstrapProgress("ready", "launcher 后台服务已启动，GUI 已接管", 100, "")
		return g.bootstrapResult(true, "launcher 后台服务已启动", state), nil
	}
	err = fmt.Errorf("launcher 后台服务启动超时: %w", lastErr)
	g.setBootstrapProgress("failed", "launcher 后台服务启动超时", 100, err.Error())
	return g.bootstrapResult(false, "launcher 后台服务启动超时", model.DeviceView{}), err
}

func (g *GUIApp) State() (BootstrapResult, error) {
	cfg, err := g.currentConfig()
	if err != nil {
		logs := g.snapshotLogs()
		return g.bootstrapResultWithLogs(false, "launcher 尚未启动", model.DeviceView{}, logs), nil
	}
	state, err := g.controlState(cfg)
	if err != nil {
		logs := g.snapshotLogs()
		return g.bootstrapResultWithLogs(false, "launcher 尚未启动", model.DeviceView{}, logs), nil
	}
	return g.bootstrapResult(true, "launcher 运行中", state), nil
}

func (g *GUIApp) Directories(req model.DirectoryRequest) (model.DirectoryListResult, error) {
	cfg, err := g.requireConfig()
	if err != nil {
		return model.DirectoryListResult{}, err
	}
	var result model.DirectoryListResult
	err = g.controlJSON(cfg, http.MethodGet, "/directories?path="+urlQuery(req.Path)+"&allow_all="+boolQuery(req.AllowAll), nil, &result)
	return result, err
}

func (g *GUIApp) CreateAgent(input model.CreateAgentInput) (model.Agent, error) {
	cfg, err := g.requireConfig()
	if err != nil {
		return model.Agent{}, err
	}
	var agent model.Agent
	err = g.controlJSON(cfg, http.MethodPost, "/agents", input, &agent)
	if err == nil {
		g.appendLog("已创建 agent: " + agent.Name)
	}
	return agent, err
}

func (g *GUIApp) RestartAgent(agentID string) (model.Agent, error) {
	cfg, err := g.requireConfig()
	if err != nil {
		return model.Agent{}, err
	}
	var agent model.Agent
	err = g.controlJSON(cfg, http.MethodPost, "/agents/"+urlPath(agentID)+"/restart", map[string]any{}, &agent)
	if err == nil {
		g.appendLog("已重启 agent: " + agent.Name)
	}
	return agent, err
}

func (g *GUIApp) SetAgentEnabled(input model.SetAgentEnabledInput) (model.Agent, error) {
	cfg, err := g.requireConfig()
	if err != nil {
		return model.Agent{}, err
	}
	var agent model.Agent
	err = g.controlJSON(cfg, http.MethodPost, "/agents/"+urlPath(input.AgentID)+"/enabled", map[string]bool{"enabled": input.Enabled}, &agent)
	if err == nil {
		if input.Enabled {
			g.appendLog("已启动 agent: " + agent.Name)
		} else {
			g.appendLog("已停止 agent: " + agent.Name)
		}
	}
	return agent, err
}

func (g *GUIApp) RemoveAgent(agentID string) error {
	cfg, err := g.requireConfig()
	if err != nil {
		return err
	}
	if err := g.controlJSON(cfg, http.MethodDelete, "/agents/"+urlPath(agentID), nil, nil); err != nil {
		return err
	}
	g.appendLog("已删除 agent: " + agentID)
	return nil
}

func (g *GUIApp) DirectoryPermission(input model.DirectoryPermissionInput) (model.DirectoryPermissionResult, error) {
	cfg, err := g.requireConfig()
	if err != nil {
		return model.DirectoryPermissionResult{}, err
	}
	var result model.DirectoryPermissionResult
	err = g.controlJSON(cfg, http.MethodPost, "/directory-permission", input, &result)
	if err == nil {
		g.appendLog("已检查目录权限: " + result.Path)
	}
	return result, err
}

func (g *GUIApp) RestartBackground() (BootstrapResult, error) {
	cfg, err := g.currentConfig()
	if err != nil {
		return BootstrapResult{}, err
	}
	wasAutostartEnabled := autostart.Status().Enabled
	if wasAutostartEnabled {
		_, _ = autostart.Disable()
	}
	g.stopControlProcess(cfg)
	g.waitControlDown(cfg, 8*time.Second)
	if err := g.startBackgroundProcess(cfg); err != nil {
		return BootstrapResult{Ready: false, Message: "launcher 后台服务重启失败", Logs: g.snapshotLogs()}, err
	}
	state, waitErr := g.waitControlState(cfg, 45*time.Second)
	if waitErr != nil {
		return BootstrapResult{Ready: false, Message: "launcher 后台服务重启后未响应", Logs: g.snapshotLogs()}, waitErr
	}
	if wasAutostartEnabled {
		if _, err := g.enableAutostartForConfig(cfg); err != nil {
			g.appendLog("重启后恢复自启动失败: " + err.Error())
		}
	}
	g.appendLog("launcher 后台服务已通过终端兼容模式重启")
	return BootstrapResult{Ready: true, Message: "launcher 后台服务已重启", State: state, Logs: g.snapshotLogs()}, nil
}

func (g *GUIApp) RuntimeEnvironment() (model.RuntimeEnvironmentInfo, error) {
	cfg, err := g.requireConfig()
	if err != nil {
		return model.RuntimeEnvironmentInfo{}, err
	}
	var result model.RuntimeEnvironmentInfo
	err = g.controlJSON(cfg, http.MethodGet, "/runtime-env", nil, &result)
	return result, err
}

func (g *GUIApp) MCPStatus(agentID string) (model.MCPStatusInfo, error) {
	cfg, err := g.requireConfig()
	if err != nil {
		return model.MCPStatusInfo{}, err
	}
	var result model.MCPStatusInfo
	err = g.controlJSON(cfg, http.MethodGet, "/mcp-status?agent_id="+urlQuery(agentID), nil, &result)
	return result, err
}

func (g *GUIApp) PrepareRuntimeEnvironment() (model.RuntimeEnvironmentInfo, error) {
	cfg, err := g.requireConfig()
	if err != nil {
		return model.RuntimeEnvironmentInfo{}, err
	}
	var result model.RuntimeEnvironmentInfo
	err = g.controlJSON(cfg, http.MethodPost, "/runtime-env/prepare", map[string]any{}, &result)
	if err == nil {
		g.appendLog("已执行托管运行时检测和准备")
	}
	return result, err
}

func (g *GUIApp) AutostartStatus() (model.LauncherAutostartInfo, error) {
	return convertAutostartInfo(autostart.Status()), nil
}

func (g *GUIApp) SetAutostart(enabled bool) (model.LauncherAutostartInfo, error) {
	cfg, err := g.currentConfig()
	if err != nil {
		return model.LauncherAutostartInfo{}, err
	}
	if enabled {
		info, err := g.enableAutostartForConfig(cfg)
		if err == nil {
			g.appendLog("已开启 launcher 后台服务自启动")
		}
		return convertAutostartInfo(info), err
	}
	info, err := autostart.Disable()
	if err == nil {
		g.appendLog("已关闭 launcher 自启动")
	}
	return convertAutostartInfo(info), err
}

func (g *GUIApp) CheckSelfUpdate() (model.LauncherSelfUpdateResult, error) {
	cfg, err := g.currentConfig()
	if err != nil {
		return model.LauncherSelfUpdateResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), guiSelfUpdateTimeout(cfg))
	defer cancel()
	result, err := selfupdate.Update(ctx, selfupdate.Options{
		CurrentVersion: launcherVersion.Value,
		RuntimeDir:     cfg.RuntimeDir,
		BaseURL:        cfg.SelfUpdate.BaseURL,
		DryRun:         true,
	})
	return convertGUISelfUpdateResult(result), err
}

func (g *GUIApp) ApplySelfUpdate() (model.LauncherSelfUpdateResult, error) {
	cfg, err := g.currentConfig()
	if err != nil {
		return model.LauncherSelfUpdateResult{}, err
	}
	var result model.LauncherSelfUpdateResult
	err = g.controlJSONWithTimeout(cfg, http.MethodPost, "/self-update", model.LauncherSelfUpdateInput{
		BaseURL: cfg.SelfUpdate.BaseURL,
		Apply:   true,
		Restart: !g.terminalCompatEnabled(),
	}, &result, guiSelfUpdateTimeout(cfg))
	if err == nil {
		g.appendLog("launcher 自更新已调度: " + result.Message)
		guiOutdated := strings.TrimSpace(result.CurrentVersion) != "" &&
			strings.TrimSpace(result.CurrentVersion) != strings.TrimSpace(launcherVersion.Value)
		if result.Applied || guiOutdated {
			executable := installedGUIExecutable(cfg)
			if _, statErr := os.Stat(executable); statErr != nil {
				g.appendLog("GUI 自动重启准备失败: " + statErr.Error())
			} else if restartErr := selfupdate.ScheduleGUIRestart(
				cfg.RuntimeDir,
				executable,
				firstNonEmpty(result.TargetVersion, result.CurrentVersion, result.LatestVersion),
			); restartErr != nil {
				g.appendLog("GUI 自动重启调度失败: " + restartErr.Error())
			} else {
				g.appendLog("GUI 将在 launcher 更新完成后自动重新打开")
				g.quitAfter(700 * time.Millisecond)
			}
		}
	}
	return result, err
}

func guiSelfUpdateTimeout(cfg config.Config) time.Duration {
	if cfg.SelfUpdate.Timeout > 0 {
		return cfg.SelfUpdate.Timeout
	}
	return 5 * time.Minute
}

func (g *GUIApp) quitAfter(delay time.Duration) {
	go func() {
		if delay > 0 {
			time.Sleep(delay)
		}
		if g.ctx != nil {
			wailsRuntime.Quit(g.ctx)
			return
		}
		os.Exit(0)
	}()
}

func convertGUISelfUpdateResult(result selfupdate.Result) model.LauncherSelfUpdateResult {
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

func (g *GUIApp) requireConfig() (config.Config, error) {
	cfg, err := g.currentConfig()
	if err != nil {
		return config.Config{}, errors.New("launcher 尚未启动，请先登录并启动")
	}
	if _, err := g.controlState(cfg); err != nil {
		return config.Config{}, errors.New("launcher 后台服务未连接，请重新登录启动")
	}
	return cfg, nil
}

func (g *GUIApp) ensureBackgroundLauncherVersion(cfg config.Config, state model.DeviceView) (model.DeviceView, bool, error) {
	current := strings.TrimSpace(state.LauncherVersion)
	target := strings.TrimSpace(launcherVersion.Value)
	if current == "" || target == "" || current == target {
		return state, false, nil
	}
	g.setBootstrapProgress("self_update_checking", fmt.Sprintf("检测到后台版本 %s，正在同步到 %s", current, target), 34, "")
	var result model.LauncherSelfUpdateResult
	err := g.controlJSONWithTimeout(cfg, http.MethodPost, "/self-update", model.LauncherSelfUpdateInput{
		TargetVersion: target,
		BaseURL:       cfg.SelfUpdate.BaseURL,
		Apply:         true,
		Restart:       true,
	}, &result, guiSelfUpdateTimeout(cfg))
	if err != nil {
		return state, false, err
	}
	g.setBootstrapProgress("self_update_applying", "后台 launcher 更新已调度，正在等待重启", 72, "")
	deadline := time.Now().Add(90 * time.Second)
	var last model.DeviceView
	for time.Now().Before(deadline) {
		time.Sleep(1200 * time.Millisecond)
		next, stateErr := g.controlState(cfg)
		if stateErr != nil {
			g.setBootstrapProgress("self_update_restarting", "后台 launcher 正在重启，等待本地控制口恢复", 82, "")
			continue
		}
		last = next
		g.syncBootstrapFromDeviceUpgrade(next)
		if strings.TrimSpace(next.LauncherVersion) == target {
			return next, true, nil
		}
	}
	if strings.TrimSpace(last.LauncherVersion) != "" {
		return last, true, fmt.Errorf("launcher 后台已调度更新，但当前版本仍为 %s", last.LauncherVersion)
	}
	return state, true, errors.New("launcher 后台已调度更新，但重连超时")
}

func (g *GUIApp) currentConfig() (config.Config, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if strings.TrimSpace(g.cfg.ListenAddr) != "" {
		return g.cfg, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, err
	}
	g.cfg = cfg
	return cfg, nil
}

func (g *GUIApp) startBackgroundProcess(cfg config.Config) error {
	g.setBootstrapProgress("installing_launcher", "正在安装本机后台 launcher", 38, "")
	executable, err := g.backgroundExecutable(cfg)
	if err != nil {
		return err
	}
	if g.terminalCompatEnabled() {
		g.setBootstrapProgress("starting_background", "正在通过终端兼容模式启动后台 launcher", 52, "")
		if err := terminalCompatStartBackground(cfg, executable); err == nil {
			g.appendLog("正在通过终端兼容模式启动 launcher 后台服务")
			return nil
		} else {
			g.appendLog("终端兼容模式启动失败，尝试直接启动: " + err.Error())
		}
	}
	g.setBootstrapProgress("starting_background", "正在启动后台 launcher 进程", 55, "")
	cmd := exec.Command(executable, "--background")
	configureBackgroundCommand(cmd)
	cmd.Env = append(os.Environ(),
		"LAUNCHER_RUNTIME_DIR="+cfg.RuntimeDir,
		"LAUNCHER_LISTEN_ADDR="+cfg.ListenAddr,
	)
	if err := cmd.Start(); err != nil {
		return err
	}
	g.appendLog("正在启动 launcher 后台服务")
	return cmd.Process.Release()
}

func (g *GUIApp) waitControlState(cfg config.Config, timeout time.Duration) (model.DeviceView, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if state, err := g.controlState(cfg); err == nil {
			return state, nil
		} else {
			lastErr = err
		}
		time.Sleep(300 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("launcher 后台服务未响应")
	}
	return model.DeviceView{}, lastErr
}

func (g *GUIApp) waitControlDown(cfg config.Config, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := g.controlState(cfg); err != nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (g *GUIApp) stopControlProcess(cfg config.Config) {
	if _, err := g.stopControlProcessAndWait(cfg); err != nil {
		g.appendLog("停止旧 launcher 后台服务失败: " + err.Error())
	}
}

func (g *GUIApp) stopControlProcessAndWait(cfg config.Config) (bool, error) {
	port := listenPort(cfg.ListenAddr)
	if port == "" {
		return false, nil
	}
	portNumber := parsePort(port)
	pid, err := proc.FindListeningPID(portNumber)
	if errors.Is(err, proc.ErrListeningPIDNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("查找控制端口 %s 的进程失败: %w", port, err)
	}
	if pid <= 0 || pid == os.Getpid() {
		return false, nil
	}

	if err := proc.Stop(pid); err != nil {
		return true, fmt.Errorf("停止进程 %d 失败: %w", pid, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := proc.WaitForPortClosed(ctx, portNumber, 200*time.Millisecond); err != nil {
		return true, fmt.Errorf("等待控制端口 %s 释放失败: %w", port, err)
	}
	return true, nil
}

func (g *GUIApp) migrateAutostartToBackground() {
	info := autostart.Status()
	if !info.Enabled {
		return
	}
	cfg, err := g.currentConfig()
	if err != nil {
		return
	}
	if _, err := g.enableAutostartForConfig(cfg); err == nil {
		g.appendLog("已迁移自启动入口为后台服务")
	}
}

func (g *GUIApp) enableAutostartForConfig(cfg config.Config) (autostart.Info, error) {
	executable, err := g.autostartBackgroundExecutable(cfg)
	if err != nil {
		return autostart.Info{}, err
	}
	if autostart.IsUpdaterExecutablePath(executable) {
		return autostart.Info{}, errors.New("不能使用 launcher 临时更新进程作为后台服务")
	}
	if g.terminalCompatEnabled() {
		terminalExecutable, args, terminalErr := terminalCompatAutostartOptions(cfg, executable)
		if terminalErr == nil {
			return autostart.Enable(autostart.Options{
				Executable: terminalExecutable,
				Args:       args,
				Env:        guiAutostartEnv(cfg),
				KeepAlive:  false,
			})
		}
		g.appendLog("终端兼容自启动配置失败，尝试直接自启动: " + terminalErr.Error())
	}
	return autostart.Enable(autostart.Options{
		Executable: executable,
		Args:       []string{"--background"},
		Env:        guiAutostartEnv(cfg),
		KeepAlive:  true,
	})
}

func (g *GUIApp) terminalCompatEnabled() bool {
	if !terminalCompatSupported() {
		return false
	}
	value := strings.TrimSpace(os.Getenv("LAUNCHER_TERMINAL_COMPAT"))
	if value != "" {
		return !strings.EqualFold(value, "0")
	}
	cfg, err := g.currentConfig()
	if err != nil {
		return true
	}
	settings, err := loadGUISettings(cfg.RuntimeDir)
	if err != nil {
		return true
	}
	return settings.TerminalCompat
}

func (g *GUIApp) autostartUsesTerminalCompat() bool {
	return autostart.CommandUsesTerminalCompat(autostart.Status().Command)
}

func (g *GUIApp) backgroundExecutable(cfg config.Config) (string, error) {
	executable, source, install, err := resolveBackgroundExecutable(cfg)
	if err != nil {
		return "", err
	}
	if !install {
		return executable, nil
	}
	if err := g.copyBackgroundExecutableWithRecovery(cfg, source, executable); err != nil {
		return "", err
	}
	return executable, nil
}

func (g *GUIApp) autostartBackgroundExecutable(cfg config.Config) (string, error) {
	executable, source, install, err := resolveBackgroundExecutable(cfg)
	if err != nil {
		return "", err
	}
	if !install {
		return executable, nil
	}
	if info, statErr := os.Stat(executable); statErr == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("launcher 后台路径不是普通文件: %s", executable)
		}
		return executable, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	if err := copyExecutableIfChanged(source, executable); err != nil {
		return "", err
	}
	return executable, nil
}

func resolveBackgroundExecutable(cfg config.Config) (target string, source string, install bool, err error) {
	current, err := os.Executable()
	if err != nil {
		return "", "", false, err
	}
	if autostart.IsUpdaterExecutablePath(current) {
		return "", "", false, errors.New("不能使用 launcher 临时更新进程作为后台服务")
	}
	target = installedGUIExecutable(cfg)
	if sameExecutable(current, target) {
		return target, current, false, nil
	}
	return target, current, true, nil
}

func installedGUIExecutable(cfg config.Config) string {
	return stableGUIPath(cfg.RuntimeDir)
}

func (g *GUIApp) copyBackgroundExecutableWithRecovery(cfg config.Config, source string, target string) error {
	firstErr := copyExecutableIfChanged(source, target)
	if firstErr == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	stopped, stopErr := g.stopControlProcessAndWait(cfg)
	if stopErr != nil {
		return fmt.Errorf("替换 launcher 后台文件失败: %v；清理旧进程失败: %w", firstErr, stopErr)
	}
	if stopped {
		g.appendLog("检测到旧 launcher 正在占用可执行文件，已停止旧进程并重试安装")
	}
	if extra, processErr := proc.StopProcessesByExecutable(ctx, target, os.Getpid()); processErr != nil {
		return fmt.Errorf("清理占用 launcher 文件的进程失败: %w", processErr)
	} else if extra > 0 {
		g.appendLog(fmt.Sprintf("已清理 %d 个占用 launcher 文件的残留进程", extra))
	}

	var lastErr error = firstErr
	for attempt := 0; attempt < 24; attempt++ {
		if err := copyExecutableIfChanged(source, target); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("替换 launcher 后台文件超时: %w", lastErr)
		case <-time.After(time.Duration(100+attempt*100) * time.Millisecond):
		}
	}
	return fmt.Errorf("替换 launcher 后台文件失败: %w", lastErr)
}

func (g *GUIApp) ensureInstalledMacAppAndRelaunch() bool {
	result, err := macapp.EnsureInstalled()
	if err != nil {
		g.appendLog("码控安装到应用程序失败: " + err.Error())
		return false
	}
	if !result.RequiresRelaunch {
		return false
	}
	g.appendLog("已安装码控到应用程序，正在重启到稳定应用路径")
	if err := macapp.OpenInstalledApp(); err != nil {
		g.appendLog("重启到应用程序失败: " + err.Error())
		return false
	}
	g.quitAfter(200 * time.Millisecond)
	return true
}

func guiAutostartEnv(cfg config.Config) map[string]string {
	env := map[string]string{
		"LAUNCHER_RUNTIME_DIR": cfg.RuntimeDir,
		"LAUNCHER_LISTEN_ADDR": cfg.ListenAddr,
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

func sameExecutable(left string, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return os.SameFile(leftInfo, rightInfo)
}

func listenPort(addr string) string {
	addr = strings.TrimSpace(addr)
	index := strings.LastIndex(addr, ":")
	if index < 0 || index == len(addr)-1 {
		return "4071"
	}
	return addr[index+1:]
}

func parsePort(port string) int {
	value, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func copyExecutableIfChanged(src string, dst string) error {
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

func (g *GUIApp) controlState(cfg config.Config) (model.DeviceView, error) {
	var state model.DeviceView
	err := g.controlJSON(cfg, http.MethodGet, "/state", nil, &state)
	return state, err
}

func (g *GUIApp) controlJSON(cfg config.Config, method string, path string, input any, output any) error {
	return g.controlJSONWithTimeout(cfg, method, path, input, output, 10*time.Second)
}

func (g *GUIApp) controlJSONWithTimeout(cfg config.Config, method string, path string, input any, output any, timeout time.Duration) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, controlURL(cfg, path), body)
	if err != nil {
		return err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var parsed struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &parsed)
		if strings.TrimSpace(parsed.Error) != "" {
			return errors.New(parsed.Error)
		}
		return fmt.Errorf("launcher 本地接口失败: %s", strings.TrimSpace(string(data)))
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("launcher 本地接口响应解析失败: %w", err)
	}
	return nil
}

func (g *GUIApp) appendLog(message string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.appendLogLocked(message)
}

func (g *GUIApp) appendLogLocked(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		g.logs = append(g.logs, time.Now().Format("15:04:05")+"  "+line)
	}
	if len(g.logs) > 80 {
		g.logs = g.logs[len(g.logs)-80:]
	}
}

func (g *GUIApp) snapshotLogs() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string{}, g.logs...)
}

func (g *GUIApp) setBootstrapProgress(phase string, message string, percent int, errorMessage string) {
	now := time.Now().Format(time.RFC3339)
	progress := BootstrapProgress{
		Phase:   strings.TrimSpace(phase),
		Message: strings.TrimSpace(message),
		Percent: percent,
		Error:   strings.TrimSpace(errorMessage),
	}
	if progress.Percent < 0 {
		progress.Percent = 0
	}
	if progress.Percent > 100 {
		progress.Percent = 100
	}
	g.mu.Lock()
	if strings.TrimSpace(g.bootstrap.StartedAt) == "" || progress.Phase == "loading_config" {
		g.bootstrap.StartedAt = now
	}
	progress.StartedAt = g.bootstrap.StartedAt
	progress.UpdatedAt = now
	g.bootstrap = progress
	g.mu.Unlock()
	if g.ctx != nil {
		wailsRuntime.EventsEmit(g.ctx, "launcher:bootstrap-progress", progress)
	}
}

func (g *GUIApp) snapshotBootstrapProgress() BootstrapProgress {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.bootstrap
}

func (g *GUIApp) bootstrapResult(ready bool, message string, state model.DeviceView) BootstrapResult {
	return g.bootstrapResultWithLogs(ready, message, state, g.snapshotLogs())
}

func (g *GUIApp) bootstrapResultWithLogs(ready bool, message string, state model.DeviceView, logs []string) BootstrapResult {
	return BootstrapResult{
		Ready:     ready,
		Message:   message,
		State:     state,
		Logs:      append([]string{}, logs...),
		Bootstrap: g.snapshotBootstrapProgress(),
	}
}

func (g *GUIApp) syncBootstrapFromDeviceUpgrade(state model.DeviceView) {
	stage := strings.TrimSpace(state.UpgradeStage)
	message := strings.TrimSpace(state.UpgradeMessage)
	percent := state.UpgradeProgress
	if stage == "" && message == "" && percent <= 0 {
		return
	}
	if message == "" {
		message = "正在同步后台 launcher 更新状态"
	}
	if percent <= 0 {
		percent = 65
	}
	phase := "self_update_applying"
	switch stage {
	case "checking_version":
		phase = "self_update_checking"
	case "downloading":
		phase = "self_update_downloading"
	case "switching_version":
		phase = "self_update_applying"
	case "starting_agents":
		phase = "self_update_restarting"
	case "completed", "already_latest":
		phase = "waiting_control"
	case "failed":
		phase = "failed"
	}
	mappedPercent := 35 + int(float64(percent)*0.5)
	if mappedPercent < 35 {
		mappedPercent = 35
	}
	if mappedPercent > 92 {
		mappedPercent = 92
	}
	g.setBootstrapProgress(phase, message, mappedPercent, "")
}

type logBuffer struct {
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	return l.buf.Write(p)
}

func (l *logBuffer) String() string {
	return l.buf.String()
}

func normalizeServerURL(input string) string {
	value := strings.TrimSpace(input)
	if value == "" {
		value = defaults.PublicBase
	}
	value = strings.TrimRight(value, "/")
	if strings.HasSuffix(value, "/ws/device") {
		value = strings.TrimSuffix(value, "/ws/device")
	}
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		value = "https://" + value
	}
	return value
}

func maskKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return key
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func controlURL(cfg config.Config, path string) string {
	return "http://" + strings.TrimRight(cfg.ListenAddr, "/") + "/" + strings.TrimLeft(path, "/")
}

func urlQuery(value string) string {
	return url.QueryEscape(value)
}

func urlPath(value string) string {
	return url.PathEscape(strings.TrimSpace(value))
}

func boolQuery(value bool) string {
	if value {
		return "true"
	}
	return "false"
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
