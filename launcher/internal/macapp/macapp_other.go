//go:build !darwin

package macapp

type InstallResult struct {
	Installed        bool
	AlreadyInstalled bool
	RequiresRelaunch bool
	SourceAppPath    string
	AppPath          string
	ExecutablePath   string
}

func InstalledAppPath() string {
	return ""
}

func InstalledExecutablePath() string {
	return ""
}

func CurrentAppBundlePath() (string, bool) {
	return "", false
}

func PreferredExecutablePath() (string, bool) {
	return "", false
}

func EnsureInstalled() (InstallResult, error) {
	return InstallResult{}, nil
}

func RepairInstalledMetadata(version string) error {
	return nil
}

func OpenInstalledApp() error {
	return nil
}

func RegisterInstalledApp() error {
	return nil
}

func RevealPermissionTarget() error {
	return nil
}

func PermissionTargetName() string {
	return "码控"
}
