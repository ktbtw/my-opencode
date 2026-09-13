package fridaruntime

import (
	"context"
	"fmt"
	"strings"
)

type ManagerEngine string

const (
	ManagerUnknown  ManagerEngine = "unknown"
	ManagerMagisk   ManagerEngine = "magisk"
	ManagerKernelSU ManagerEngine = "kernelsu"
	ManagerAPatch   ManagerEngine = "apatch"
)

type Manager struct {
	Name   string
	Engine ManagerEngine
}

type DeviceInfo struct {
	Serial  string
	API     int
	ABI     string
	Rooted  bool
	Manager Manager
}

type CommandRunner interface {
	Run(context.Context, ...string) (string, error)
}

func ABIForProperty(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "arm64-v8a":
		return "arm64-v8a", nil
	case "armeabi-v7a", "armeabi":
		return "armeabi-v7a", nil
	case "x86":
		return "x86", nil
	case "x86_64":
		return "x86_64", nil
	default:
		return "", fmt.Errorf("unsupported Android ABI: %s", value)
	}
}

func ProfileForAPI(api int) (string, error) {
	switch {
	case api >= 26 && api <= 28:
		return "compat", nil
	case api >= 29 && api <= 34:
		return "standard", nil
	case api >= 35 && api <= 37:
		return "modern", nil
	default:
		return "", fmt.Errorf("unsupported Android API: %d", api)
	}
}

func DetectManager(hasMagisk, hasKernelSU, hasAPatch bool) Manager {
	switch {
	case hasAPatch:
		return Manager{Name: "APatch", Engine: ManagerAPatch}
	case hasKernelSU:
		return Manager{Name: "KernelSU", Engine: ManagerKernelSU}
	case hasMagisk:
		return Manager{Name: "Magisk-compatible", Engine: ManagerMagisk}
	default:
		return Manager{Name: "Unknown", Engine: ManagerUnknown}
	}
}

func InstallCommand(engine ManagerEngine, remoteZIP string) ([]string, error) {
	if strings.TrimSpace(remoteZIP) == "" || !strings.HasPrefix(remoteZIP, "/") {
		return nil, fmt.Errorf("remote ZIP path must be absolute")
	}
	switch engine {
	case ManagerMagisk:
		return []string{"magisk", "--install-module", remoteZIP}, nil
	case ManagerKernelSU:
		return []string{"/data/adb/ksud", "module", "install", remoteZIP}, nil
	case ManagerAPatch:
		return []string{"/data/adb/apd", "module", "install", remoteZIP}, nil
	default:
		return nil, fmt.Errorf("unsupported root manager engine: %s", engine)
	}
}

func suCommand(command string) string {
	return "su -c '" + strings.ReplaceAll(command, "'", "'\"'\"'") + "'"
}

func Probe(ctx context.Context, runner CommandRunner, serial string) (DeviceInfo, error) {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return DeviceInfo{}, fmt.Errorf("device serial is required")
	}
	run := func(args ...string) (string, error) {
		return runner.Run(ctx, append([]string{"adb", "-s", serial}, args...)...)
	}
	apiRaw, err := run("shell", "getprop", "ro.build.version.sdk")
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("read Android API: %w", err)
	}
	var api int
	if _, err := fmt.Sscanf(strings.TrimSpace(apiRaw), "%d", &api); err != nil {
		return DeviceInfo{}, fmt.Errorf("parse Android API %q: %w", strings.TrimSpace(apiRaw), err)
	}
	abiRaw, err := run("shell", "getprop", "ro.product.cpu.abi")
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("read Android ABI: %w", err)
	}
	abi, err := ABIForProperty(abiRaw)
	if err != nil {
		return DeviceInfo{}, err
	}
	rootRaw, rootErr := run("shell", suCommand("id"))
	rooted := rootErr == nil && strings.Contains(rootRaw, "uid=0")
	probeRaw, err := run("shell", suCommand("for p in magisk /data/adb/ksud /data/adb/apd; do [ -x \"$p\" ] || command -v \"$p\" 2>/dev/null; done"))
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("probe root manager: %w", err)
	}
	return DeviceInfo{
		Serial: serial,
		API:    api,
		ABI:    abi,
		Rooted: rooted,
		Manager: DetectManager(
			strings.Contains(probeRaw, "magisk"),
			strings.Contains(probeRaw, "ksud"),
			strings.Contains(probeRaw, "apd"),
		),
	}, nil
}
