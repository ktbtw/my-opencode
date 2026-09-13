//go:build windows

package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"launcher/internal/backgroundcmd"
)

func prepareIDARuntimePath(idaDir string) (string, error) {
	idaDir = filepath.Clean(strings.TrimSpace(idaDir))
	if idaDir == "." || idaDir == "" {
		return "", errors.New("IDA 运行目录为空")
	}
	if isASCIIPath(idaDir) {
		return idaDir, nil
	}

	leaf := windowsIDAAliasLeaf(idaDir)
	var failures []string
	for _, root := range windowsIDAPathRoots() {
		alias := filepath.Join(root, "ChatCodex", "idalib", leaf)
		if !isASCIIPath(alias) {
			failures = append(failures, alias+": 路径仍包含非 ASCII 字符")
			continue
		}
		if err := ensureWindowsDirectoryJunction(alias, idaDir); err != nil {
			failures = append(failures, alias+": "+err.Error())
			continue
		}
		return alias, nil
	}
	if len(failures) == 0 {
		return "", errors.New("未找到可用的 Windows ASCII 公共目录")
	}
	return "", errors.New(strings.Join(failures, "；"))
}

func windowsIDAAliasLeaf(idaDir string) string {
	hash := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(idaDir))))
	return "ida-" + hex.EncodeToString(hash[:8])
}

func validIDACompatibilityInstallDir(runtimeDir string, manifest installedRuntimeManifest) bool {
	if manifest.RuntimeID != "ida" || strings.TrimSpace(manifest.Version) == "" || !isASCIIPath(manifest.InstallDir) {
		return false
	}
	managedRoot := filepath.Join(runtimeDir, "runtimes", "ida", manifest.Version)
	managedIDARoot, err := findIDABinaryDir(managedRoot)
	if err != nil {
		return false
	}
	matchedAlias := false
	leaf := windowsIDAAliasLeaf(managedIDARoot)
	for _, root := range windowsIDAPathRoots() {
		expected := filepath.Join(root, "ChatCodex", "idalib", leaf)
		if sameWindowsPath(manifest.InstallDir, expected) {
			matchedAlias = true
			break
		}
	}
	if !matchedAlias {
		return false
	}
	same, err := sameWindowsFile(manifest.InstallDir, managedIDARoot)
	return err == nil && same
}

func windowsIDAPathRoots() []string {
	var roots []string
	for _, value := range []string{os.Getenv("PROGRAMDATA"), os.Getenv("PUBLIC")} {
		value = filepath.Clean(strings.TrimSpace(value))
		if value == "." || value == "" || !filepath.IsAbs(value) {
			continue
		}
		duplicate := false
		for _, current := range roots {
			if strings.EqualFold(current, value) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			roots = append(roots, value)
		}
	}
	return roots
}

func ensureWindowsDirectoryJunction(alias, target string) error {
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if _, statErr := os.Lstat(alias); statErr == nil {
		if same, sameErr := sameWindowsFile(alias, targetAbs); sameErr == nil && same {
			return nil
		}
		if removeErr := os.Remove(alias); removeErr != nil {
			return fmt.Errorf("移除旧目录联接失败：%w", removeErr)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		return err
	}

	script := fmt.Sprintf(
		"$ErrorActionPreference = 'Stop'; New-Item -ItemType Junction -Path '%s' -Target '%s' | Out-Null",
		powerShellSingleQuoted(alias), powerShellSingleQuoted(targetAbs),
	)
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShellUTF16LE(script))
	backgroundcmd.Configure(cmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("创建目录联接失败：%w；输出：%s", err, summarizeCommandOutput(string(output)))
	}
	same, err := sameWindowsFile(alias, targetAbs)
	if err != nil {
		return fmt.Errorf("验证目录联接失败：%w", err)
	}
	if !same {
		return fmt.Errorf("目录联接目标不匹配：alias=%s target=%s", alias, targetAbs)
	}
	return nil
}

func sameWindowsFile(left, right string) (bool, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false, err
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		return false, err
	}
	return os.SameFile(leftInfo, rightInfo), nil
}

func isASCIIPath(value string) bool {
	for _, char := range value {
		if char > 0x7f {
			return false
		}
	}
	return true
}

func sameWindowsPath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}
