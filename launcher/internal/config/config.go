package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"launcher/internal/backgroundcmd"
	"launcher/internal/binarymeta"
	"launcher/internal/defaults"
	"launcher/internal/paths"
)

type Config struct {
	ListenAddr string
	RuntimeDir string
	Relay      RelayConfig
	Agent      AgentConfig
	Discovery  DiscoveryConfig
	Health     HealthConfig
	SelfUpdate SelfUpdateConfig
}

type RelayConfig struct {
	URL         string
	OperatorKey string
	MachineID   string
	Hostname    string
}

type AgentConfig struct {
	BinaryName string
	BaseArgs   []string
	Env        map[string]string
	PrintLogs  bool
}

type DiscoveryConfig struct {
	AllowedRoots []string
	IncludeCWD   bool
	MaxDepth     int
}

type HealthConfig struct {
	TimeoutSeconds int
	IntervalMillis int
	MaxRestarts    int
	BackoffMillis  int
}

type SelfUpdateConfig struct {
	Enabled      bool
	BaseURL      string
	InitialDelay time.Duration
	Interval     time.Duration
	Timeout      time.Duration
	Restart      bool
}

func Load() (Config, error) {
	runtimeDir, err := paths.LauncherRuntimeDir()
	if err != nil {
		return Config{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	return Config{
		ListenAddr: getenv("LAUNCHER_LISTEN_ADDR", "127.0.0.1:4071"),
		RuntimeDir: runtimeDir,
		Relay: RelayConfig{
			URL:         getenv("LAUNCHER_RELAY_URL", defaultRelayURL()),
			OperatorKey: os.Getenv("OPENCODE_RELAY_OPERATOR_KEY"),
			MachineID:   ensureMachineID(runtimeDir),
			Hostname:    hostName(),
		},
		Agent: AgentConfig{
			BinaryName: binarymeta.DefaultBinaryName(),
			BaseArgs:   []string{"serve", "--hostname", "0.0.0.0"},
			Env:        map[string]string{},
			PrintLogs:  getenv("LAUNCHER_PRINT_AGENT_LOGS", "1") != "0",
		},
		Discovery: DiscoveryConfig{
			AllowedRoots: []string{filepath.Join(home, "Downloads"), filepath.Join(home, "workspace")},
			IncludeCWD:   true,
			MaxDepth:     2,
		},
		Health: HealthConfig{
			TimeoutSeconds: 90,
			IntervalMillis: 500,
			MaxRestarts:    5,
			BackoffMillis:  2000,
		},
		SelfUpdate: SelfUpdateConfig{
			Enabled:      getenvBool("LAUNCHER_SELF_UPDATE_ENABLED", true),
			BaseURL:      strings.TrimRight(strings.TrimSpace(os.Getenv("LAUNCHER_UPDATE_BASE_URL")), "/"),
			InitialDelay: getenvDurationSeconds("LAUNCHER_SELF_UPDATE_INITIAL_DELAY_SECONDS", 20*time.Second),
			Interval:     getenvDurationSeconds("LAUNCHER_SELF_UPDATE_INTERVAL_SECONDS", 30*time.Minute),
			Timeout:      getenvDurationSeconds("LAUNCHER_SELF_UPDATE_TIMEOUT_SECONDS", 5*time.Minute),
			Restart:      getenvBool("LAUNCHER_SELF_UPDATE_RESTART", true),
		},
	}, nil
}

func getenv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func getenvDurationSeconds(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func hostName() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "launcher-machine"
	}
	return name
}

func defaultRelayURL() string {
	base := strings.TrimSpace(os.Getenv("LAUNCHER_PUBLIC_BASE"))
	if base == "" {
		base = defaults.PublicBase
	}
	base = strings.TrimRight(base, "/")
	if strings.HasPrefix(base, "https://") {
		return "wss://" + strings.TrimPrefix(base, "https://") + "/ws/device"
	}
	if strings.HasPrefix(base, "http://") {
		return "ws://" + strings.TrimPrefix(base, "http://") + "/ws/device"
	}
	return base + "/ws/device"
}

func ensureMachineID(runtimeDir string) string {
	if value := strings.TrimSpace(os.Getenv("LAUNCHER_MACHINE_ID")); value != "" {
		return value
	}
	path := filepath.Join(runtimeDir, "machine_id")
	if data, err := os.ReadFile(path); err == nil {
		if value := strings.TrimSpace(string(data)); value != "" {
			return value
		}
	}
	_ = os.MkdirAll(runtimeDir, 0o755)
	value := generateMachineID()
	_ = os.WriteFile(path, []byte(value+"\n"), 0o644)
	return value
}

func generateMachineID() string {
	if value := randomMachineID(); value != "" {
		return value
	}
	raw := stableHardwareID()
	if raw == "" {
		raw = hostName()
	}
	hash := sha256.Sum256([]byte("launcher-machine:" + raw))
	return "m_" + hex.EncodeToString(hash[:16])
}

func randomMachineID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return "m_" + hex.EncodeToString(buf)
}

func stableHardwareID() string {
	switch runtime.GOOS {
	case "darwin":
		return firstNonEmpty(command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice"), command("system_profiler", "SPHardwareDataType"))
	case "linux":
		for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if data, err := os.ReadFile(path); err == nil {
				if value := strings.TrimSpace(string(data)); value != "" {
					return value
				}
			}
		}
	case "windows":
		return firstNonEmpty(command("reg", "query", `HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid"), command("wmic", "csproduct", "get", "uuid"))
	}
	return ""
}

func command(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	backgroundcmd.Configure(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "IOPlatformUUID") {
			parts := strings.Split(line, "=")
			if len(parts) > 1 {
				return strings.Trim(strings.TrimSpace(parts[len(parts)-1]), `"`)
			}
		}
		if strings.Contains(line, "MachineGuid") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				return fields[len(fields)-1]
			}
		}
		if strings.EqualFold(line, "UUID") || strings.EqualFold(line, "Hardware UUID:") {
			continue
		}
		if strings.Contains(strings.ToLower(line), "serial") {
			continue
		}
		return strings.Trim(strings.TrimSpace(line), `"`)
	}
	return ""
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
