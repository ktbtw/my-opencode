package fridaruntime

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestABIForProperty(t *testing.T) {
	for input, want := range map[string]string{
		"arm64-v8a\n": "arm64-v8a",
		"armeabi":     "armeabi-v7a",
		"x86":         "x86",
		"x86_64":      "x86_64",
	} {
		got, err := ABIForProperty(input)
		if err != nil || got != want {
			t.Fatalf("ABIForProperty(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := ABIForProperty("riscv64"); err == nil {
		t.Fatal("expected unsupported ABI error")
	}
}

func TestProfileForAPI(t *testing.T) {
	cases := map[int]string{26: "compat", 28: "compat", 29: "standard", 34: "standard", 35: "modern", 37: "modern"}
	for api, want := range cases {
		got, err := ProfileForAPI(api)
		if err != nil || got != want {
			t.Fatalf("ProfileForAPI(%d) = %q, %v; want %q", api, got, err, want)
		}
	}
}

func TestInstallCommand(t *testing.T) {
	path := "/data/local/tmp/frida.zip"
	cases := map[ManagerEngine][]string{
		ManagerMagisk:   {"magisk", "--install-module", path},
		ManagerKernelSU: {"/data/adb/ksud", "module", "install", path},
		ManagerAPatch:   {"/data/adb/apd", "module", "install", path},
	}
	for engine, want := range cases {
		got, err := InstallCommand(engine, path)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("InstallCommand(%q) = %#v, %v; want %#v", engine, got, err, want)
		}
	}
}

func TestSUCommandEscapesQuotes(t *testing.T) {
	if got, want := suCommand("echo 'ok'"), "su -c 'echo '\"'\"'ok'\"'\"''"; got != want {
		t.Fatalf("suCommand() = %q; want %q", got, want)
	}
}

type fixtureRunner struct{}

func (fixtureRunner) Run(_ context.Context, args ...string) (string, error) {
	last := args[len(args)-1]
	switch last {
	case "ro.build.version.sdk":
		return "35\n", nil
	case "ro.product.cpu.abi":
		return "arm64-v8a\n", nil
	case "su -c 'id'":
		return "uid=0(root) gid=0(root)", nil
	default:
		if len(args) >= 1 && args[len(args)-1] == "su -c 'for p in magisk /data/adb/ksud /data/adb/apd; do [ -x \"$p\" ] || command -v \"$p\" 2>/dev/null; done'" {
			return "/data/adb/ksud\n", nil
		}
		return "", fmt.Errorf("unexpected command: %#v", args)
	}
}

func TestProbe(t *testing.T) {
	device, err := Probe(context.Background(), fixtureRunner{}, "DEVICE")
	if err != nil {
		t.Fatal(err)
	}
	if device.ABI != "arm64-v8a" || device.API != 35 || !device.Rooted || device.Manager.Engine != ManagerKernelSU {
		t.Fatalf("unexpected device: %#v", device)
	}
}
