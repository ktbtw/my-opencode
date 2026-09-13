//go:build !windows

package app

func prepareIDARuntimePath(idaDir string) (string, error) {
	return idaDir, nil
}

func validIDACompatibilityInstallDir(string, installedRuntimeManifest) bool {
	return false
}
