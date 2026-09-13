package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"launcher/internal/model"
)

func (s *service) applyExternalToolEnvironment(env map[string]string) {
	if env == nil {
		return
	}
	if !validIDADir(env["IDADIR"]) {
		if path := s.managedIDADir(); path != "" {
			env["IDADIR"] = path
		} else if path := discoverIDADir(s.cfg.RuntimeDir); path != "" {
			env["IDADIR"] = path
		}
	}
	if !validGhidraHome(env["GHIDRA_HOME"]) {
		if path := discoverGhidraHome(s.cfg.RuntimeDir); path != "" {
			env["GHIDRA_HOME"] = path
		}
	}
}

func (s *service) managedIDADir() string {
	manifest, ok := s.bestRuntimeManifest("ida", "")
	if !ok {
		return ""
	}
	if validIDADir(manifest.InstallDir) {
		return filepath.Clean(manifest.InstallDir)
	}
	if path := strings.TrimSpace(manifest.EnvPatch["IDADIR"]); validIDADir(path) {
		return filepath.Clean(path)
	}
	return ""
}

func (s *service) repairMCPServerExternalTools(server model.DeviceMCPServerInfo) model.DeviceMCPServerInfo {
	env := cloneEnv(server.Environment)
	s.applyExternalToolEnvironment(env)
	if _, declared := server.Environment["GHIDRA_HOME"]; !declared {
		delete(env, "GHIDRA_HOME")
	}
	if _, declared := server.Environment["IDADIR"]; !declared {
		delete(env, "IDADIR")
	}
	if len(env) == 0 {
		server.Environment = nil
	} else {
		server.Environment = env
	}
	return server
}

func discoverIDADir(runtimeDir string) string {
	candidates := []string{
		os.Getenv("IDADIR"),
		os.Getenv("IDA_HOME"),
		os.Getenv("IDA_INSTALL_DIR"),
		filepath.Join(runtimeDir, "runtimes", "ida"),
	}
	managedWindows, _ := filepath.Glob(filepath.Join(runtimeDir, "runtimes", "ida", "*"))
	managedMac, _ := filepath.Glob(filepath.Join(runtimeDir, "runtimes", "ida", "*", "IDA*.app", "Contents", "MacOS"))
	candidates = append(candidates, managedWindows...)
	candidates = append(candidates, managedMac...)
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramW6432"), os.Getenv("ProgramFiles(x86)")} {
		if strings.TrimSpace(base) == "" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(base, "IDA*"))
		candidates = append(candidates, matches...)
	}
	for _, base := range []string{"/Applications", filepath.Join(os.Getenv("HOME"), "Applications")} {
		matches, _ := filepath.Glob(filepath.Join(base, "IDA*.app", "Contents", "MacOS"))
		candidates = append(candidates, matches...)
	}
	return newestValidToolRoot(candidates, validIDADir)
}

func discoverGhidraHome(runtimeDir string) string {
	candidates := []string{
		os.Getenv("GHIDRA_HOME"),
		filepath.Join(runtimeDir, "runtimes", "ghidra"),
		filepath.Join(runtimeDir, "tools", "ghidra"),
		mcpInstallRoot(runtimeDir, "ghidra"),
		"/opt/homebrew/opt/ghidra/libexec",
		"/usr/local/opt/ghidra/libexec",
		"/opt/ghidra",
	}
	for _, base := range []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramW6432"),
		os.Getenv("LOCALAPPDATA"),
		"/Applications",
	} {
		if strings.TrimSpace(base) == "" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(base, "[Gg]hidra*"))
		candidates = append(candidates, matches...)
	}
	return newestValidToolRoot(candidates, validGhidraHome)
}

func newestValidToolRoot(candidates []string, valid func(string) bool) string {
	candidates = uniqueStrings(candidates)
	sort.Sort(sort.Reverse(sort.StringSlice(candidates)))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if valid(candidate) {
			return filepath.Clean(candidate)
		}
		children, _ := os.ReadDir(candidate)
		for _, child := range children {
			if !child.IsDir() {
				continue
			}
			nested := filepath.Join(candidate, child.Name())
			if valid(nested) {
				return filepath.Clean(nested)
			}
		}
	}
	return ""
}

func validIDADir(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	for _, name := range []string{"idalib.dll", "libidalib.dylib", "libidalib.so"} {
		if pathExists(filepath.Join(path, name)) {
			return true
		}
	}
	return false
}

func validGhidraHome(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	return pathExists(filepath.Join(path, "support", "analyzeHeadless")) ||
		pathExists(filepath.Join(path, "support", "analyzeHeadless.bat"))
}
