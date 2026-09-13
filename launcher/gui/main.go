package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	launcherApp "launcher/internal/app"
	"launcher/internal/autostart"
	"launcher/internal/bootstrap"
	"launcher/internal/config"
	"launcher/internal/launcherlog"
	"launcher/internal/selfupdate"
	launcherVersion "launcher/internal/version"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--background":
			if handedOff, handoffErr := handoffBackgroundToStable(); handoffErr != nil {
				fmt.Fprintln(os.Stderr, "launcher 后台服务稳定路径启动失败:", handoffErr)
				os.Exit(1)
			} else if handedOff {
				return
			}
			if err := runBackgroundLauncher(); err != nil {
				fmt.Fprintln(os.Stderr, "launcher 后台服务启动失败:", err)
				os.Exit(1)
			}
			return
		case "self-update-apply":
			if err := selfupdate.RunApplyCommand(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "launcher 自更新应用失败:", err)
				os.Exit(1)
			}
			return
		case "--print-version":
			fmt.Println(launcherVersion.Value)
			return
		}
	}
	if handedOff, err := handoffToInstalledGUI(); err != nil {
		fmt.Fprintln(os.Stderr, "launcher GUI 稳定路径启动失败:", err)
		os.Exit(1)
	} else if handedOff {
		return
	}

	gui := NewGUIApp()
	err := wails.Run(&options.App{
		Title:            "码控",
		Width:            520,
		Height:           560,
		MinWidth:         420,
		MinHeight:        520,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 239, G: 244, B: 251, A: 1},
		OnStartup:        gui.startup,
		OnShutdown:       gui.shutdown,
		Bind:             []interface{}{gui},
	})
	if err != nil {
		fmt.Println("launcher gui 启动失败:", err)
	}
}

func runBackgroundLauncher() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := migrateLegacyRuntimeScript(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "launcher 旧启动脚本迁移失败:", err)
	}
	if autostart.Status().Enabled {
		// A previously generated macOS compatibility script may still point at
		// an older installed .app. Rewrite it from the stable runtime binary
		// before the service enters its supervisor loop.
		if _, repairErr := NewGUIApp().enableAutostartForConfig(cfg); repairErr != nil {
			fmt.Fprintln(os.Stderr, "launcher 自启动入口迁移失败:", repairErr)
		}
	}
	if executable, executableErr := os.Executable(); executableErr == nil && sameExecutable(executable, installedGUIExecutable(cfg)) {
		go func() {
			if syncErr := syncLegacyGUIEntrypoints(executable); syncErr != nil {
				fmt.Fprintln(os.Stderr, "迁移 Windows Launcher 入口失败:", syncErr)
			}
		}()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		if launcherControlHealthy(cfg) {
			if version := launcherControlVersion(cfg); strings.TrimSpace(version) != "" && strings.TrimSpace(version) != launcherVersion.Value {
				fmt.Printf("launcher background monitor exiting because active service version is %s, local version is %s\n", version, launcherVersion.Value)
				return nil
			}
			fmt.Printf("launcher background monitor attached to http://%s\n", cfg.ListenAddr)
			if err := waitExistingLauncherExit(ctx, cfg); err != nil {
				return err
			}
			if selfupdate.ConsumeRestartPending(cfg.RuntimeDir) {
				fmt.Println("launcher self-update pending, attached background monitor exiting")
				return nil
			}
			continue
		}
		if err := runBackgroundServiceOnce(ctx, cfg); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintln(os.Stderr, "launcher 后台服务退出:", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
			continue
		}
		if selfupdate.ConsumeRestartPending(cfg.RuntimeDir) {
			fmt.Println("launcher self-update pending, background supervisor exiting")
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

func runBackgroundServiceOnce(ctx context.Context, cfg config.Config) error {
	if err := launcherlog.Configure(cfg.RuntimeDir); err != nil {
		return fmt.Errorf("初始化 launcher 日志失败: %w", err)
	}
	if err := bootstrap.PrepareWithOptions(&cfg, bootstrap.Console{
		In:          os.Stdin,
		Out:         os.Stdout,
		Interactive: bootstrap.IsInteractive(os.Stdin),
	}, bootstrap.PrepareOptions{
		SkipCLIUpdate:  true,
		SkipToolchains: true,
	}); err != nil {
		return err
	}
	svc, err := launcherApp.NewService(cfg)
	if err != nil {
		return err
	}
	return launcherApp.RunServiceContext(ctx, svc)
}

func waitExistingLauncherExit(ctx context.Context, cfg config.Config) error {
	misses := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
		if launcherControlHealthy(cfg) {
			misses = 0
			continue
		}
		misses++
		if misses >= 3 {
			return nil
		}
	}
}

func launcherControlHealthy(cfg config.Config) bool {
	addr := strings.TrimSpace(cfg.ListenAddr)
	if addr == "" {
		return false
	}
	client := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get("http://" + addr + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func launcherControlVersion(cfg config.Config) string {
	addr := strings.TrimSpace(cfg.ListenAddr)
	if addr == "" {
		return ""
	}
	client := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get("http://" + addr + "/state")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var payload struct {
		LauncherVersion string `json:"launcher_version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.LauncherVersion)
}
