//go:build darwin

package macapp

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"launcher/internal/binarymeta"
)

const appBundleName = "码控.app"
const applicationsDir = "/Applications"

type InstallResult struct {
	Installed        bool
	AlreadyInstalled bool
	RequiresRelaunch bool
	SourceAppPath    string
	AppPath          string
	ExecutablePath   string
}

func InstalledAppPath() string {
	return filepath.Join(applicationsDir, appBundleName)
}

func InstalledExecutablePath() string {
	return filepath.Join(InstalledAppPath(), "Contents", "MacOS", binarymeta.GUILauncherBinaryName())
}

func CurrentAppBundlePath() (string, bool) {
	executable, err := os.Executable()
	if err != nil {
		return "", false
	}
	return appBundlePathForExecutable(executable)
}

func PreferredExecutablePath() (string, bool) {
	if skipInstallIntegration() {
		return "", false
	}
	if executable, err := os.Executable(); err == nil && isInstalledExecutable(executable) {
		return executable, true
	}
	installed := InstalledExecutablePath()
	if info, err := os.Stat(installed); err == nil && !info.IsDir() {
		return installed, true
	}
	return "", false
}

func EnsureInstalled() (InstallResult, error) {
	if skipInstallIntegration() {
		return InstallResult{}, nil
	}
	current, err := os.Executable()
	if err != nil {
		return InstallResult{}, err
	}
	if isInstalledExecutable(current) {
		_ = RepairInstalledMetadata("")
		return InstallResult{
			AlreadyInstalled: true,
			AppPath:          InstalledAppPath(),
			ExecutablePath:   current,
		}, nil
	}
	sourceApp, ok := appBundlePathForExecutable(current)
	if !ok {
		return InstallResult{ExecutablePath: current}, nil
	}
	if sameCleanPath(sourceApp, InstalledAppPath()) {
		_ = RepairInstalledMetadata("")
		return InstallResult{
			AlreadyInstalled: true,
			AppPath:          InstalledAppPath(),
			ExecutablePath:   current,
		}, nil
	}
	if err := copyAppBundle(sourceApp, InstalledAppPath()); err != nil {
		return InstallResult{}, err
	}
	return InstallResult{
		Installed:        true,
		RequiresRelaunch: true,
		SourceAppPath:    sourceApp,
		AppPath:          InstalledAppPath(),
		ExecutablePath:   InstalledExecutablePath(),
	}, nil
}

func RepairInstalledMetadata(version string) error {
	plist := filepath.Join(InstalledAppPath(), "Contents", "Info.plist")
	if _, err := os.Stat(plist); err != nil {
		return err
	}
	values := map[string]string{
		"CFBundleIdentifier":    "com.chatcodex.launcher",
		"CFBundleName":          "码控",
		"CFBundleExecutable":    binarymeta.GUILauncherBinaryName(),
		"CFBundleGetInfoString": "码控 launcher",
	}
	version = strings.TrimSpace(version)
	if version != "" {
		values["CFBundleShortVersionString"] = version
		values["CFBundleVersion"] = version
	}
	for key, value := range values {
		if err := setPlistValue(plist, key, value); err != nil {
			return err
		}
	}
	return nil
}

func skipInstallIntegration() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("LAUNCHER_SKIP_MAC_APP_INSTALL")), "1")
}

func OpenInstalledApp() error {
	if _, err := os.Stat(InstalledAppPath()); err != nil {
		return err
	}
	return exec.Command("open", InstalledAppPath()).Start()
}

func RegisterInstalledApp() error {
	if _, err := os.Stat(InstalledAppPath()); err != nil {
		return err
	}
	lsregister := "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
	if _, err := os.Stat(lsregister); err != nil {
		return exec.Command("open", InstalledAppPath()).Start()
	}
	return exec.Command(lsregister, "-f", InstalledAppPath()).Run()
}

func setPlistValue(plist string, key string, value string) error {
	if err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Set :"+key+" "+value, plist).Run(); err == nil {
		return nil
	}
	return exec.Command("/usr/libexec/PlistBuddy", "-c", "Add :"+key+" string "+value, plist).Run()
}

func RevealPermissionTarget() error {
	_ = RegisterInstalledApp()
	target := InstalledAppPath()
	if _, err := os.Stat(target); err != nil {
		if bundle, ok := CurrentAppBundlePath(); ok {
			target = bundle
		} else if executable, execErr := os.Executable(); execErr == nil && strings.TrimSpace(executable) != "" {
			target = executable
		} else {
			return err
		}
	}
	return exec.Command("open", "-R", target).Start()
}

func PermissionTargetName() string {
	if _, err := os.Stat(InstalledAppPath()); err == nil {
		return InstalledAppPath()
	}
	if bundle, ok := CurrentAppBundlePath(); ok {
		return bundle
	}
	return "码控.app"
}

func appBundlePathForExecutable(executable string) (string, bool) {
	executable = filepath.Clean(strings.TrimSpace(executable))
	marker := ".app" + string(filepath.Separator) + "Contents" + string(filepath.Separator) + "MacOS" + string(filepath.Separator)
	index := strings.Index(executable, marker)
	if index < 0 {
		return "", false
	}
	return executable[:index+len(".app")], true
}

func isInstalledExecutable(path string) bool {
	return sameCleanPath(path, InstalledExecutablePath())
}

func sameCleanPath(left string, right string) bool {
	return filepath.Clean(strings.TrimSpace(left)) == filepath.Clean(strings.TrimSpace(right))
}

func copyAppBundle(src string, dst string) error {
	src = filepath.Clean(strings.TrimSpace(src))
	dst = filepath.Clean(strings.TrimSpace(dst))
	if src == "" || dst == "" || src == string(filepath.Separator) || dst == string(filepath.Separator) {
		return errors.New("app 路径不能为空")
	}
	if _, err := os.Stat(filepath.Join(src, "Contents", "MacOS", binarymeta.GUILauncherBinaryName())); err != nil {
		return fmt.Errorf("源 app 不完整: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+".installing."+fmt.Sprint(os.Getpid()))
	_ = os.RemoveAll(tmp)
	defer os.RemoveAll(tmp)
	out, err := exec.Command("ditto", src, tmp).CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("复制码控到应用程序失败: %s", text)
	}
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", tmp).Run()
	_ = os.RemoveAll(dst)
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("安装码控到应用程序失败: %w", err)
	}
	return nil
}
