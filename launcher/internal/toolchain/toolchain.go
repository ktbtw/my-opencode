package toolchain

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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"launcher/internal/backgroundcmd"
	"launcher/internal/defaults"
	"launcher/internal/resumabledownload"
)

const (
	defaultNodeVersion   = "v22.16.0"
	defaultPythonVersion = "3.12"
	defaultJavaVersion   = "21"
	defaultJavaTemurin   = "21.0.11_10"
	defaultUVVersion     = "LatestRelease"
	defaultNodeMirror    = "https://cdn.npmmirror.com/binaries/node"
	defaultNPMRegistry   = "https://registry.npmmirror.com"
	defaultPythonIndex   = "https://pypi.tuna.tsinghua.edu.cn/simple"
	defaultUVMirror      = ""
	defaultPythonMirror  = "https://registry.npmmirror.com/-/binary/python-build-standalone"
	downloadUserAgent    = "chat-codex-launcher/0.1"
)

type Options struct {
	RuntimeDir  string
	Progress    func(string)
	Downloader  Downloader
	InstallJava bool
}

type Downloader interface {
	Download(url string, target string) error
}

type EnvPatch struct {
	Env map[string]string
}

type Diagnostics struct {
	Node   ToolStatus
	NPM    ToolStatus
	NPX    ToolStatus
	Python ToolStatus
	PIP    ToolStatus
	UV     ToolStatus
	Java   ToolStatus
	Javac  ToolStatus
}

type ToolStatus struct {
	Name    string
	Path    string
	Version string
	Managed bool
	Error   string
}

type httpDownloader struct{}

type archiveAsset struct {
	Filename string
	Root     string
	Kind     string
}

func Ensure(options Options) (EnvPatch, Diagnostics, error) {
	if strings.TrimSpace(options.RuntimeDir) == "" {
		return EnvPatch{}, Diagnostics{}, errors.New("runtimeDir 不能为空")
	}
	if options.Downloader == nil {
		options.Downloader = httpDownloader{}
	}
	env := EnvPatch{Env: runtimeEnv(options.RuntimeDir)}
	var diagnostics Diagnostics
	var pathDirs []string

	nodeDirs, nodeDiag, nodeErr := ensureNode(options)
	diagnostics.Node = nodeDiag.Node
	diagnostics.NPM = nodeDiag.NPM
	diagnostics.NPX = nodeDiag.NPX
	pathDirs = append(pathDirs, nodeDirs...)

	pythonDirs, pythonDiag, pythonErr := ensurePython(options)
	diagnostics.Python = pythonDiag.Python
	diagnostics.PIP = pythonDiag.PIP
	diagnostics.UV = pythonDiag.UV
	pathDirs = append(pathDirs, pythonDirs...)

	javaDirs, javaEnv, javaDiag, javaErr := ensureJava(options)
	diagnostics.Java = javaDiag.Java
	diagnostics.Javac = javaDiag.Javac
	for key, value := range javaEnv {
		env.Env[key] = value
	}
	pathDirs = append(pathDirs, javaDirs...)

	pathDirs = uniqueExistingDirs(pathDirs)
	if len(pathDirs) > 0 {
		env.Env[pathKey()] = joinPathWithCurrent(pathDirs)
	}
	if nodeErr != nil || pythonErr != nil || javaErr != nil {
		return env, diagnostics, errors.Join(nodeErr, pythonErr, javaErr)
	}
	return env, diagnostics, nil
}

func ensureNode(options Options) ([]string, Diagnostics, error) {
	var diagnostics Diagnostics
	nodeDir := findNodeDir(options.RuntimeDir)
	managed := isManagedPath(options.RuntimeDir, nodeDir)
	var installErr error
	if nodeDir == "" && shouldAutoInstall() {
		nodeDir, installErr = installNode(options)
		if installErr == nil {
			managed = true
		} else {
			report(options, "托管 Node 准备失败，尝试系统 Node: "+installErr.Error())
		}
	}
	if nodeDir == "" {
		nodeDir = findSystemNodeDir()
		managed = false
	}
	if nodeDir == "" {
		err := errors.New("未检测到 node/npm/npx")
		if installErr != nil {
			err = fmt.Errorf("托管 Node 安装失败且未检测到系统 node/npm/npx: %w", installErr)
		} else if !shouldAutoInstall() {
			err = errors.New("未检测到 node/npm/npx，且当前平台未启用自动安装")
		}
		diagnostics.Node = ToolStatus{Name: "node", Error: err.Error()}
		diagnostics.NPM = ToolStatus{Name: "npm", Error: err.Error()}
		diagnostics.NPX = ToolStatus{Name: "npx", Error: err.Error()}
		return nil, diagnostics, err
	}
	pathDirs := []string{nodeDir}
	if runtime.GOOS == "windows" {
		pathDirs = append(pathDirs, windowsNPMGlobalDirs()...)
	}
	envPath := joinPathWithCurrent(pathDirs)
	diagnostics.Node = inspectTool("node", envPath, managed)
	diagnostics.NPM = inspectTool("npm", envPath, managed)
	diagnostics.NPX = inspectTool("npx", envPath, managed)
	if diagnostics.Node.Error != "" || diagnostics.NPX.Error != "" {
		err := fmt.Errorf("node 工具链不可用: node=%s npx=%s", diagnostics.Node.Error, diagnostics.NPX.Error)
		return pathDirs, diagnostics, err
	}
	return pathDirs, diagnostics, nil
}

func ensurePython(options Options) ([]string, Diagnostics, error) {
	var diagnostics Diagnostics
	uvDir := findUVDir(options.RuntimeDir)
	managed := isManagedPath(options.RuntimeDir, uvDir)
	if uvDir == "" {
		if dir := findCommandDir("uv"); dir != "" {
			uvDir = dir
		}
	}
	if uvDir == "" && shouldAutoInstall() {
		var err error
		uvDir, err = installUV(options)
		managed = true
		if err != nil {
			diagnostics.UV = ToolStatus{Name: "uv", Error: err.Error()}
			return nil, diagnostics, err
		}
	}
	pathDirs := []string{}
	if uvDir != "" {
		pathDirs = append(pathDirs, uvDir)
	}
	pathDirs = append(pathDirs, findPythonDirs(options.RuntimeDir)...)
	envPath := joinPathWithCurrent(pathDirs)
	diagnostics.UV = inspectTool("uv", envPath, managed)
	diagnostics.Python = inspectFirstTool([]string{"python", "python3", "py"}, envPath)
	diagnostics.PIP = inspectFirstTool([]string{"pip", "pip3"}, envPath)
	if diagnostics.UV.Error == "" && diagnostics.Python.Error != "" && shouldInstallPythonWithUV() {
		if err := installPythonWithUV(options, envPath); err != nil {
			diagnostics.Python.Error = err.Error()
			return pathDirs, diagnostics, err
		}
		pathDirs = append(pathDirs, findPythonDirs(options.RuntimeDir)...)
		envPath = joinPathWithCurrent(pathDirs)
		diagnostics.Python = inspectFirstTool([]string{"python", "python3", "py"}, envPath)
		diagnostics.PIP = inspectFirstTool([]string{"pip", "pip3"}, envPath)
	}
	if diagnostics.UV.Error != "" {
		return pathDirs, diagnostics, fmt.Errorf("uv 不可用: %s", diagnostics.UV.Error)
	}
	return pathDirs, diagnostics, nil
}

func ensureJava(options Options) ([]string, map[string]string, Diagnostics, error) {
	var diagnostics Diagnostics
	javaHome := findJavaHome(options.RuntimeDir)
	managed := isManagedPath(options.RuntimeDir, javaHome)
	var installErr error
	if javaHome == "" && shouldInstallJava(options) && shouldAutoInstall() {
		javaHome, installErr = installJava(options)
		if installErr == nil {
			managed = true
		} else {
			report(options, "托管 Java 准备失败，尝试系统 Java: "+installErr.Error())
		}
	}
	if javaHome == "" {
		javaHome = findSystemJavaHome()
		managed = false
	}
	if javaHome == "" {
		err := errors.New("未检测到 java")
		if installErr != nil {
			err = fmt.Errorf("托管 Java 安装失败且未检测到系统 java: %w", installErr)
		} else if shouldInstallJava(options) && !shouldAutoInstall() {
			err = errors.New("未检测到 java，且当前平台未启用自动安装")
		}
		diagnostics.Java = ToolStatus{Name: "java", Error: err.Error()}
		diagnostics.Javac = ToolStatus{Name: "javac", Error: err.Error()}
		if shouldRequireJava() {
			return nil, nil, diagnostics, err
		}
		return nil, nil, diagnostics, nil
	}
	binDir := javaBinDir(javaHome)
	pathDirs := []string{binDir}
	envPath := joinPathWithCurrent(pathDirs)
	diagnostics.Java = inspectTool("java", envPath, managed)
	diagnostics.Javac = inspectTool("javac", envPath, managed)
	env := map[string]string{"JAVA_HOME": javaHome}
	if diagnostics.Java.Error != "" && shouldRequireJava() {
		return pathDirs, env, diagnostics, fmt.Errorf("java 运行时不可用: %s", diagnostics.Java.Error)
	}
	return pathDirs, env, diagnostics, nil
}

func installNode(options Options) (string, error) {
	version := getenv("LAUNCHER_NODE_VERSION", defaultNodeVersion)
	version = normalizeVersion(version)
	asset, err := nodeAsset(version)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(options.RuntimeDir, "runtimes", "node", version)
	if binDir := nodeBinDir(dir); binDir != "" {
		return binDir, nil
	}
	if pathExists(dir) {
		report(options, "托管 Node 目录校验失败，正在重新安装: "+dir)
		_ = os.RemoveAll(dir)
	}
	target := filepath.Join(options.RuntimeDir, "downloads", asset.Filename)
	report(options, "开始下载 Node: "+version)
	if err := downloadFirst(options, nodeDownloadURLs(version, asset), target, "Node"); err != nil {
		return "", err
	}
	extractDir := filepath.Join(options.RuntimeDir, "runtimes", "node")
	_ = os.RemoveAll(filepath.Join(extractDir, asset.Root))
	if err := unpackArchive(target, extractDir, asset.Kind); err != nil {
		return "", err
	}
	extracted := filepath.Join(extractDir, asset.Root)
	if extracted != dir {
		_ = os.RemoveAll(dir)
		if err := os.Rename(extracted, dir); err != nil {
			return "", err
		}
	}
	if binDir := nodeBinDir(dir); binDir != "" {
		return binDir, nil
	}
	return "", fmt.Errorf("Node 解压后未找到 node/npm/npx: %s", dir)
}

func installJava(options Options) (string, error) {
	version := normalizeJavaVersion(getenv("LAUNCHER_JAVA_VERSION", defaultJavaVersion))
	asset, err := javaAsset(version)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(options.RuntimeDir, "runtimes", "java", version)
	if home := javaHomeDir(dir); home != "" {
		return home, nil
	}
	target := filepath.Join(options.RuntimeDir, "downloads", asset.Filename)
	report(options, "开始下载 Java: "+version)
	if err := downloadFirst(options, javaDownloadURLs(version, asset), target, "Java"); err != nil {
		return "", err
	}
	extractDir := filepath.Join(options.RuntimeDir, "runtimes", "java", ".extract-"+version)
	_ = os.RemoveAll(extractDir)
	if err := unpackArchive(target, extractDir, asset.Kind); err != nil {
		_ = os.RemoveAll(extractDir)
		return "", err
	}
	home := findJavaHomeUnder(extractDir)
	if home == "" {
		_ = os.RemoveAll(extractDir)
		return "", fmt.Errorf("Java 解压后未找到 bin/java: %s", extractDir)
	}
	_ = os.RemoveAll(dir)
	if err := os.Rename(home, dir); err != nil {
		_ = os.RemoveAll(extractDir)
		return "", err
	}
	_ = os.RemoveAll(extractDir)
	if finalHome := javaHomeDir(dir); finalHome != "" {
		return finalHome, nil
	}
	return "", fmt.Errorf("Java 安装后未找到 bin/java: %s", dir)
}

func installUV(options Options) (string, error) {
	version := normalizeVersion(getenv("LAUNCHER_UV_VERSION", defaultUVVersion))
	asset, err := uvAsset()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(options.RuntimeDir, "runtimes", "uv", version)
	if binDir := uvBinDir(dir); binDir != "" {
		return binDir, nil
	}
	target := filepath.Join(options.RuntimeDir, "downloads", "uv-"+version+"-"+asset.Filename)
	report(options, "开始下载 uv: "+version)
	if err := downloadFirst(options, uvDownloadURLs(asset), target, "uv"); err != nil {
		return "", err
	}
	if err := unpackArchive(target, dir, asset.Kind); err != nil {
		return "", err
	}
	if uvBinDir(filepath.Join(dir, asset.Root)) != "" {
		nested := filepath.Join(dir, asset.Root)
		if err := flattenDir(nested, dir); err != nil {
			return "", err
		}
	}
	if binDir := uvBinDir(dir); binDir != "" {
		return binDir, nil
	}
	return "", fmt.Errorf("uv 解压后未找到可执行文件: %s", dir)
}

func installPythonWithUV(options Options, envPath string) error {
	installDir := filepath.Join(options.RuntimeDir, "runtimes", "python")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return err
	}
	version := getenv("LAUNCHER_PYTHON_VERSION", defaultPythonVersion)
	report(options, "开始通过 uv 安装 Python: "+version)
	uvPath, err := lookPath("uv", envPath)
	if err != nil {
		return err
	}
	cmd := exec.Command(uvPath, "python", "install", version, "--install-dir", installDir)
	backgroundcmd.Configure(cmd)
	cmd.Env = append(os.Environ(), pathKey()+"="+envPath)
	for key, value := range runtimeEnv(options.RuntimeDir) {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("uv python install 失败: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (httpDownloader) Download(url string, target string) error {
	client := &http.Client{Timeout: downloadTimeout()}
	_, err := resumabledownload.Fetch(context.Background(), resumabledownload.Options{
		Client:  client,
		URL:     url,
		Target:  target,
		Headers: map[string]string{"User-Agent": downloadUserAgent},
	})
	return err
}

func downloadFirst(options Options, urls []string, target string, label string) error {
	var failures []string
	for _, url := range urls {
		report(options, fmt.Sprintf("%s 下载地址: %s", label, url))
		if err := options.Downloader.Download(url, target); err != nil {
			failures = append(failures, fmt.Sprintf("%s => %v", url, err))
			report(options, fmt.Sprintf("%s 下载失败，尝试备用源: %v", label, err))
			continue
		}
		return nil
	}
	return fmt.Errorf("下载失败: %s", strings.Join(failures, "; "))
}

func runtimeEnv(runtimeDir string) map[string]string {
	npmCacheDir := filepath.Join(runtimeDir, "caches", "npm")
	pipCacheDir := filepath.Join(runtimeDir, "caches", "pip")
	uvCacheDir := filepath.Join(runtimeDir, "caches", "uv")
	uvToolDir := filepath.Join(runtimeDir, "runtimes", "uv-tools")
	pythonInstallDir := filepath.Join(runtimeDir, "runtimes", "python")
	return map[string]string{
		"NPM_CONFIG_REGISTRY":           getenv("LAUNCHER_NPM_REGISTRY", defaultNPMRegistry),
		"npm_config_registry":           getenv("LAUNCHER_NPM_REGISTRY", defaultNPMRegistry),
		"NPM_CONFIG_CACHE":              npmCacheDir,
		"npm_config_cache":              npmCacheDir,
		"PIP_INDEX_URL":                 getenv("LAUNCHER_PYTHON_INDEX_URL", defaultPythonIndex),
		"PIP_CACHE_DIR":                 pipCacheDir,
		"UV_INDEX_URL":                  getenv("LAUNCHER_PYTHON_INDEX_URL", defaultPythonIndex),
		"UV_DEFAULT_INDEX":              getenv("LAUNCHER_PYTHON_INDEX_URL", defaultPythonIndex),
		"UV_PYTHON_INSTALL_MIRROR":      getenv("LAUNCHER_PYTHON_INSTALL_MIRROR", defaultPythonMirror),
		"UV_PYTHON_INSTALL_DIR":         pythonInstallDir,
		"UV_CACHE_DIR":                  uvCacheDir,
		"UV_TOOL_DIR":                   uvToolDir,
		"UV_PYTHON_DOWNLOADS":           "automatic",
		"PIP_DISABLE_PIP_VERSION_CHECK": "1",
	}
}

func findNodeDir(runtimeDir string) string {
	version := normalizeVersion(getenv("LAUNCHER_NODE_VERSION", defaultNodeVersion))
	for _, candidate := range []string{
		filepath.Join(runtimeDir, "runtimes", "node", version),
		filepath.Join(runtimeDir, "runtimes", "node"),
	} {
		if binDir := nodeBinDir(candidate); binDir != "" {
			return binDir
		}
	}
	if binDir := firstToolBinDir(filepath.Join(runtimeDir, "runtimes", "node"), nodeBinDir); binDir != "" {
		return binDir
	}
	return ""
}

func findSystemNodeDir() string {
	for _, dir := range systemNodeDirs() {
		if isNodeDir(dir) {
			return dir
		}
	}
	if hasConfiguredSystemNodeDirs() {
		return ""
	}
	if dir := findLoginShellNodeDir(); dir != "" && isNodeDir(dir) {
		return dir
	}
	return ""
}

func systemNodeDirs() []string {
	var dirs []string
	if dir := findCommandDir("node"); dir != "" {
		dirs = append(dirs, dir)
	}
	configured := splitEnvList(os.Getenv("LAUNCHER_SYSTEM_NODE_DIRS"))
	if len(configured) > 0 {
		return uniqueNonEmpty(append(dirs, configured...))
	}
	if runtime.GOOS == "windows" {
		return uniqueNonEmpty(append(dirs, windowsNodeDirs()...))
	}
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		if detected, err := os.UserHomeDir(); err == nil {
			home = detected
		}
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".asdf", "shims"),
		)
		dirs = appendGlobDirs(dirs, filepath.Join(home, ".nvm", "versions", "node", "*", "bin"))
		dirs = appendGlobDirs(dirs, filepath.Join(home, ".fnm", "node-versions", "*", "installation", "bin"))
	}
	return uniqueNonEmpty(dirs)
}

func hasConfiguredSystemNodeDirs() bool {
	return strings.TrimSpace(os.Getenv("LAUNCHER_SYSTEM_NODE_DIRS")) != ""
}

func appendGlobDirs(dirs []string, pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return dirs
	}
	return append(dirs, matches...)
}

func findLoginShellNodeDir() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-lc", "command -v node 2>/dev/null")
	backgroundcmd.Configure(cmd)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return ""
	}
	return filepath.Dir(path)
}

func findUVDir(runtimeDir string) string {
	version := normalizeVersion(getenv("LAUNCHER_UV_VERSION", defaultUVVersion))
	for _, candidate := range []string{
		filepath.Join(runtimeDir, "runtimes", "uv", version),
		filepath.Join(runtimeDir, "runtimes", "uv"),
	} {
		if binDir := uvBinDir(candidate); binDir != "" {
			return binDir
		}
	}
	if binDir := firstToolBinDir(filepath.Join(runtimeDir, "runtimes", "uv"), uvBinDir); binDir != "" {
		return binDir
	}
	return ""
}

func findPythonDirs(runtimeDir string) []string {
	root := filepath.Join(runtimeDir, "runtimes", "python")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, dir := range []string{
			entryPath(root, entry.Name()),
			filepath.Join(root, entry.Name(), "bin"),
			filepath.Join(root, entry.Name(), "install"),
			filepath.Join(root, entry.Name(), "install", "bin"),
			filepath.Join(root, entry.Name(), "Scripts"),
		} {
			if hasExecutable(dir, "python") || hasExecutable(dir, "python3") {
				dirs = append(dirs, dir)
			}
			if hasExecutable(dir, "pip") || hasExecutable(dir, "pip3") {
				dirs = append(dirs, dir)
			}
		}
	}
	return uniqueExistingDirs(dirs)
}

func findJavaHome(runtimeDir string) string {
	version := normalizeJavaVersion(getenv("LAUNCHER_JAVA_VERSION", defaultJavaVersion))
	for _, candidate := range []string{
		filepath.Join(runtimeDir, "runtimes", "java", version),
		filepath.Join(runtimeDir, "runtimes", "java"),
	} {
		if home := javaHomeDir(candidate); home != "" {
			return home
		}
	}
	root := filepath.Join(runtimeDir, "runtimes", "java")
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if home := javaHomeDir(filepath.Join(root, entry.Name())); home != "" {
			return home
		}
	}
	return ""
}

func findSystemJavaHome() string {
	for _, dir := range systemJavaHomes() {
		if home := javaHomeDir(dir); home != "" {
			return home
		}
	}
	if path, err := exec.LookPath("java"); err == nil {
		home := filepath.Dir(filepath.Dir(path))
		if found := javaHomeDir(home); found != "" {
			return found
		}
	}
	return ""
}

func systemJavaHomes() []string {
	var dirs []string
	if javaHome := strings.TrimSpace(os.Getenv("JAVA_HOME")); javaHome != "" {
		dirs = append(dirs, javaHome)
	}
	configured := splitEnvList(os.Getenv("LAUNCHER_SYSTEM_JAVA_HOMES"))
	if len(configured) > 0 {
		return uniqueNonEmpty(append(dirs, configured...))
	}
	if runtime.GOOS == "darwin" {
		if home := macOSJavaHome(); home != "" {
			dirs = append(dirs, home)
		}
		dirs = append(dirs, "/Library/Java/JavaVirtualMachines")
	}
	if runtime.GOOS == "linux" {
		dirs = append(dirs, "/usr/lib/jvm", "/usr/java")
	}
	if runtime.GOOS == "windows" {
		if programFiles := strings.TrimSpace(os.Getenv("ProgramFiles")); programFiles != "" {
			dirs = append(dirs, filepath.Join(programFiles, "Java"))
			dirs = append(dirs, filepath.Join(programFiles, "Eclipse Adoptium"))
		}
		if programFilesX86 := strings.TrimSpace(os.Getenv("ProgramFiles(x86)")); programFilesX86 != "" {
			dirs = append(dirs, filepath.Join(programFilesX86, "Java"))
		}
	}
	return uniqueNonEmpty(dirs)
}

func macOSJavaHome() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/libexec/java_home").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func javaHomeDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	for _, candidate := range []string{
		dir,
		filepath.Join(dir, "Contents", "Home"),
	} {
		if isJavaHome(candidate) {
			return candidate
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, candidate := range []string{
			filepath.Join(dir, entry.Name()),
			filepath.Join(dir, entry.Name(), "Contents", "Home"),
		} {
			if isJavaHome(candidate) {
				return candidate
			}
		}
	}
	return ""
}

func findJavaHomeUnder(root string) string {
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" || !d.IsDir() {
			return nil
		}
		if isJavaHome(path) {
			found = path
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

func windowsNodeDirs() []string {
	var dirs []string
	if programFiles := strings.TrimSpace(os.Getenv("ProgramFiles")); programFiles != "" {
		dirs = append(dirs, filepath.Join(programFiles, "nodejs"))
	}
	if programFilesX86 := strings.TrimSpace(os.Getenv("ProgramFiles(x86)")); programFilesX86 != "" {
		dirs = append(dirs, filepath.Join(programFilesX86, "nodejs"))
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		dirs = append(dirs, filepath.Join(localAppData, "Programs", "nodejs"))
	}
	dirs = append(dirs, `C:\Program Files\nodejs`, `C:\Program Files (x86)\nodejs`)
	return dirs
}

func windowsNPMGlobalDirs() []string {
	var dirs []string
	if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
		dirs = append(dirs, filepath.Join(appData, "npm"))
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		dirs = append(dirs, filepath.Join(localAppData, "npm"))
	}
	return dirs
}

func isNodeDir(dir string) bool {
	return hasExecutable(dir, "node") && hasExecutable(dir, "npm") && hasExecutable(dir, "npx")
}

func isUVDir(dir string) bool {
	return hasExecutable(dir, "uv")
}

func isJavaHome(dir string) bool {
	return hasExecutable(filepath.Join(dir, "bin"), "java")
}

func nodeBinDir(dir string) string {
	for _, candidate := range []string{dir, filepath.Join(dir, "bin")} {
		if isNodeDir(candidate) {
			return candidate
		}
	}
	return ""
}

func uvBinDir(dir string) string {
	for _, candidate := range []string{dir, filepath.Join(dir, "bin")} {
		if isUVDir(candidate) {
			return candidate
		}
	}
	return ""
}

func javaBinDir(home string) string {
	return filepath.Join(home, "bin")
}

func firstToolBinDir(root string, match func(string) string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if binDir := match(filepath.Join(root, entry.Name())); binDir != "" {
			return binDir
		}
	}
	return ""
}

func hasExecutable(dir string, name string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	for _, candidate := range executableCandidates(name) {
		if info, err := os.Stat(filepath.Join(dir, candidate)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func executableCandidates(name string) []string {
	if runtime.GOOS != "windows" {
		return []string{name}
	}
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".exe"), ".cmd")
	return []string{base + ".exe", base + ".cmd", base + ".bat", base}
}

func inspectTool(name string, envPath string, managed bool) ToolStatus {
	status := ToolStatus{Name: name, Managed: managed}
	path, err := lookPath(name, envPath)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Path = path
	status.Version = toolVersion(path)
	return status
}

func inspectFirstTool(names []string, envPath string) ToolStatus {
	for _, name := range names {
		status := inspectTool(name, envPath, false)
		if status.Error == "" {
			return status
		}
	}
	return ToolStatus{Name: names[0], Error: "未找到 " + strings.Join(names, "/")}
}

func lookPath(name string, envPath string) (string, error) {
	if strings.TrimSpace(envPath) == "" {
		return exec.LookPath(name)
	}
	original := os.Getenv(pathKey())
	_ = os.Setenv(pathKey(), envPath)
	defer func() {
		if original == "" {
			_ = os.Unsetenv(pathKey())
			return
		}
		_ = os.Setenv(pathKey(), original)
	}()
	return exec.LookPath(name)
}

func toolVersion(path string) string {
	if runtime.GOOS == "windows" {
		if version := nodePackageManagerVersion(path); version != "" {
			return version
		}
	}
	cmd := exec.Command(path, "--version")
	backgroundcmd.Configure(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func nodePackageManagerVersion(path string) string {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if name != "npm" && name != "npx" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "node_modules", "npm", "package.json"))
	if err != nil {
		return ""
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ""
	}
	return strings.TrimSpace(manifest.Version)
}

func pathKey() string {
	if runtime.GOOS == "windows" {
		return "Path"
	}
	return "PATH"
}

func joinPathWithCurrent(dirs []string) string {
	parts := append([]string{}, dirs...)
	if current := os.Getenv(pathKey()); current != "" {
		parts = append(parts, current)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func uniqueExistingDirs(dirs []string) []string {
	seen := map[string]struct{}{}
	var result []string
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		key := filepath.Clean(strings.ToLower(dir))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, dir)
	}
	return result
}

func findCommandDir(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return filepath.Dir(path)
}

func isManagedPath(runtimeDir string, path string) bool {
	rel, err := filepath.Rel(filepath.Join(runtimeDir, "runtimes"), path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

func shouldAutoInstall() bool {
	return strings.TrimSpace(os.Getenv("LAUNCHER_TOOLCHAIN_AUTO_INSTALL")) != "0"
}

func shouldInstallPythonWithUV() bool {
	return strings.TrimSpace(os.Getenv("LAUNCHER_TOOLCHAIN_INSTALL_PYTHON")) != "0"
}

func shouldInstallJava(options Options) bool {
	value := strings.TrimSpace(os.Getenv("LAUNCHER_TOOLCHAIN_INSTALL_JAVA"))
	if value != "" {
		return !isFalseEnvValue(value)
	}
	return options.InstallJava
}

func shouldRequireJava() bool {
	return isTrueEnvValue(os.Getenv("LAUNCHER_TOOLCHAIN_REQUIRE_JAVA"))
}

func isFalseEnvValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "n", "off":
		return true
	default:
		return false
	}
}

func isTrueEnvValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func getenv(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return version
	}
	if strings.EqualFold(version, "LatestRelease") {
		return "LatestRelease"
	}
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

func normalizeJavaVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return defaultJavaVersion
	}
	return strings.TrimPrefix(version, "jdk-")
}

func uvDownloadURLs(asset archiveAsset) []string {
	override := strings.TrimSpace(os.Getenv("LAUNCHER_UV_DOWNLOAD_URL"))
	if override != "" {
		return []string{override}
	}
	mirror := strings.TrimSpace(os.Getenv("LAUNCHER_UV_MIRROR"))
	if mirror == "" {
		mirror = defaultUVMirror
	}
	urls := []string{}
	if mirror != "" {
		urls = append(urls, uvMirrorURL(mirror, asset.Filename))
	}
	hosted := uvMirrorURL(defaultRuntimeUVMirror(), asset.Filename)
	if !containsString(urls, hosted) {
		urls = append(urls, hosted)
	}
	return uniqueNonEmpty(urls)
}

func nodeDownloadURLs(version string, asset archiveAsset) []string {
	override := strings.TrimSpace(os.Getenv("LAUNCHER_NODE_DOWNLOAD_URL"))
	if override != "" {
		return []string{override}
	}
	if urls := downloadURLsFromEnv("LAUNCHER_NODE_DOWNLOAD_URLS"); len(urls) > 0 {
		return urls
	}
	mirrors := downloadMirrorsFromEnv("LAUNCHER_NODE_MIRRORS")
	if len(mirrors) == 0 {
		mirrors = []string{
			strings.TrimRight(getenv("LAUNCHER_NODE_MIRROR", defaultNodeMirror), "/") + "/" + version,
			defaultRuntimeNodeMirror(),
		}
	}
	urls := make([]string, 0, len(mirrors))
	for _, mirror := range mirrors {
		urls = append(urls, uvMirrorURL(mirror, asset.Filename))
	}
	return uniqueNonEmpty(urls)
}

func javaDownloadURLs(version string, asset archiveAsset) []string {
	override := strings.TrimSpace(os.Getenv("LAUNCHER_JAVA_DOWNLOAD_URL"))
	if override != "" {
		return []string{override}
	}
	mirror := strings.TrimSpace(os.Getenv("LAUNCHER_JAVA_MIRROR"))
	if mirror != "" {
		return uniqueNonEmpty([]string{uvMirrorURL(mirror, asset.Filename)})
	}
	return uniqueNonEmpty([]string{uvMirrorURL(defaultRuntimeJavaMirror(), asset.Filename)})
}

func downloadURLsFromEnv(key string) []string {
	return splitEnvList(os.Getenv(key))
}

func downloadMirrorsFromEnv(key string) []string {
	return splitEnvList(os.Getenv(key))
}

func splitEnvList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	return uniqueNonEmpty(parts)
}

func defaultRuntimeUVMirror() string {
	return defaultRuntimeMirror("uv")
}

func defaultRuntimeNodeMirror() string {
	return defaultRuntimeMirror("node")
}

func defaultRuntimeJavaMirror() string {
	return defaultRuntimeMirror("java")
}

func defaultRuntimeMirror(name string) string {
	base := strings.TrimSpace(os.Getenv("LAUNCHER_RUNTIME_DOWNLOAD_BASE"))
	if base == "" {
		base = strings.TrimSpace(os.Getenv("LAUNCHER_PUBLIC_BASE"))
	}
	if base == "" {
		base = defaults.PublicBase
	}
	return strings.TrimRight(base, "/") + "/api/runtime/" + name + "/download?artifact="
}

func uvMirrorURL(mirror string, filename string) string {
	mirror = strings.TrimSpace(mirror)
	if strings.Contains(mirror, "{artifact}") {
		return strings.ReplaceAll(mirror, "{artifact}", filename)
	}
	if strings.HasSuffix(mirror, "=") || strings.HasSuffix(mirror, "/") {
		return mirror + filename
	}
	return strings.TrimRight(mirror, "/") + "/" + filename
}

func downloadTimeout() time.Duration {
	seconds := 20
	if value := strings.TrimSpace(os.Getenv("LAUNCHER_DOWNLOAD_TIMEOUT_SECONDS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			seconds = parsed
		}
	}
	return time.Duration(seconds) * time.Second
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func nodeAsset(version string) (archiveAsset, error) {
	platform := ""
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH == "amd64" {
			platform = "win-x64"
		} else if runtime.GOARCH == "arm64" {
			platform = "win-arm64"
		}
	case "darwin":
		if runtime.GOARCH == "amd64" {
			platform = "darwin-x64"
		} else if runtime.GOARCH == "arm64" {
			platform = "darwin-arm64"
		}
	case "linux":
		if runtime.GOARCH == "amd64" {
			platform = "linux-x64"
		} else if runtime.GOARCH == "arm64" {
			platform = "linux-arm64"
		}
	}
	if platform == "" {
		return archiveAsset{}, fmt.Errorf("当前平台暂不支持自动安装 Node: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	root := "node-" + version + "-" + platform
	if runtime.GOOS == "windows" {
		return archiveAsset{Filename: root + ".zip", Root: root, Kind: "zip"}, nil
	}
	return archiveAsset{Filename: root + ".tar.gz", Root: root, Kind: "tar.gz"}, nil
}

func javaAsset(version string) (archiveAsset, error) {
	platform := ""
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH == "amd64" {
			platform = "windows-x64"
		} else if runtime.GOARCH == "arm64" {
			platform = "windows-aarch64"
		}
	case "darwin":
		if runtime.GOARCH == "amd64" {
			platform = "darwin-x64"
		} else if runtime.GOARCH == "arm64" {
			platform = "darwin-aarch64"
		}
	case "linux":
		if runtime.GOARCH == "amd64" {
			platform = "linux-x64"
		} else if runtime.GOARCH == "arm64" {
			platform = "linux-aarch64"
		}
	}
	if platform == "" {
		return archiveAsset{}, fmt.Errorf("当前平台暂不支持自动安装 Java: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	root := "jdk-" + version + "-" + platform
	if runtime.GOOS == "windows" {
		return archiveAsset{Filename: root + ".zip", Root: root, Kind: "zip"}, nil
	}
	return archiveAsset{Filename: root + ".tar.gz", Root: root, Kind: "tar.gz"}, nil
}

func uvAsset() (archiveAsset, error) {
	platform := ""
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH == "amd64" {
			platform = "x86_64-pc-windows-msvc"
		} else if runtime.GOARCH == "arm64" {
			platform = "aarch64-pc-windows-msvc"
		}
	case "darwin":
		if runtime.GOARCH == "amd64" {
			platform = "x86_64-apple-darwin"
		} else if runtime.GOARCH == "arm64" {
			platform = "aarch64-apple-darwin"
		}
	case "linux":
		if runtime.GOARCH == "amd64" {
			platform = "x86_64-unknown-linux-gnu"
		} else if runtime.GOARCH == "arm64" {
			platform = "aarch64-unknown-linux-gnu"
		}
	}
	if platform == "" {
		return archiveAsset{}, fmt.Errorf("当前平台暂不支持自动安装 uv: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	root := "uv-" + platform
	if runtime.GOOS == "windows" {
		return archiveAsset{Filename: root + ".zip", Root: root, Kind: "zip"}, nil
	}
	return archiveAsset{Filename: root + ".tar.gz", Root: root, Kind: "tar.gz"}, nil
}

func report(options Options, message string) {
	if options.Progress != nil {
		options.Progress(message)
	}
}

func unpackArchive(archivePath string, targetDir string, kind string) error {
	switch kind {
	case "zip":
		return unzipSingleRoot(archivePath, targetDir)
	case "tar.gz":
		return untarGz(archivePath, targetDir)
	default:
		return fmt.Errorf("不支持的压缩包格式: %s", kind)
	}
}

func unzipSingleRoot(archivePath string, targetDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	for _, file := range reader.File {
		target, err := safeArchiveTarget(targetDir, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if err != nil {
			_ = src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := errors.Join(src.Close(), dst.Close())
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
	}
	return nil
}

func untarGz(archivePath string, targetDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeArchiveTarget(targetDir, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(dst, reader)
			closeErr := dst.Close()
			if copyErr != nil || closeErr != nil {
				return errors.Join(copyErr, closeErr)
			}
		case tar.TypeSymlink:
			if err := createArchiveSymlink(targetDir, target, header.Linkname); err != nil {
				return err
			}
		case tar.TypeLink:
			if err := createArchiveHardLink(targetDir, target, header.Linkname); err != nil {
				return err
			}
		}
	}
}

func createArchiveSymlink(root string, target string, linkName string) error {
	linkName = strings.TrimSpace(linkName)
	if linkName == "" || filepath.IsAbs(linkName) {
		return nil
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(target), linkName))
	if !isPathUnder(root, resolved) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	_ = os.RemoveAll(target)
	return os.Symlink(linkName, target)
}

func createArchiveHardLink(root string, target string, linkName string) error {
	source, err := safeArchiveTarget(root, linkName)
	if err != nil || !isPathUnder(root, source) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	_ = os.RemoveAll(target)
	if err := os.Link(source, target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func safeArchiveTarget(targetDir string, name string) (string, error) {
	name = filepath.Clean(name)
	if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("压缩包路径不安全: %s", name)
	}
	target := filepath.Join(targetDir, name)
	rel, err := filepath.Rel(targetDir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("压缩包路径不安全: %s", name)
	}
	return target, nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func flattenDir(src string, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		from := filepath.Join(src, entry.Name())
		to := filepath.Join(dst, entry.Name())
		_ = os.RemoveAll(to)
		if err := os.Rename(from, to); err != nil {
			return err
		}
	}
	return os.RemoveAll(src)
}

func entryPath(root string, name string) string {
	return filepath.Join(root, name)
}
