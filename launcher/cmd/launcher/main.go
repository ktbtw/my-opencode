package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"launcher/internal/app"
	"launcher/internal/autostart"
	"launcher/internal/bootstrap"
	"launcher/internal/config"
	"launcher/internal/launcherlog"
	"launcher/internal/selfupdate"
	launcherVersion "launcher/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "launcher 启动失败: %v\n", err)
		if runtime.GOOS == "windows" && bootstrap.IsInteractive(os.Stdin) {
			fmt.Fprint(os.Stderr, "按回车键退出...")
			_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		}
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "autostart":
			return runAutostart(os.Args[2:])
		case "self-update":
			return runSelfUpdate(os.Args[2:])
		case "self-update-apply":
			return runSelfUpdateApply(os.Args[2:])
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
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
	svc, err := app.NewService(cfg)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.RunServiceContext(ctx, svc); err != nil {
		return err
	}
	return nil
}

func runAutostart(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: launcher autostart status|enable|disable")
	}
	switch args[0] {
	case "status":
		printJSON(autostart.Refresh())
		return nil
	case "enable":
		flags := flag.NewFlagSet("autostart enable", flag.ContinueOnError)
		dryRun := flags.Bool("dry-run", false, "只生成结果，不写入系统自启动")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		info, err := autostart.Enable(autostart.Options{
			Executable: executable,
			Env: map[string]string{
				"LAUNCHER_RUNTIME_DIR": cfg.RuntimeDir,
				"LAUNCHER_LISTEN_ADDR": cfg.ListenAddr,
			},
			DryRun:    *dryRun,
			KeepAlive: true,
		})
		if err != nil {
			return err
		}
		printJSON(info)
		return nil
	case "disable":
		info, err := autostart.Disable()
		if err != nil {
			return err
		}
		printJSON(info)
		return nil
	default:
		return fmt.Errorf("未知 autostart 命令: %s", args[0])
	}
}

func runSelfUpdate(args []string) error {
	flags := flag.NewFlagSet("self-update", flag.ContinueOnError)
	target := flags.String("target", "", "目标 launcher 版本")
	baseURL := flags.String("base-url", "", "版本源地址")
	apply := flags.Bool("apply", false, "下载后调度替换当前 launcher")
	restart := flags.Bool("restart", false, "替换后重启 launcher")
	dryRun := flags.Bool("dry-run", false, "只检查版本，不下载或替换")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	timeout := cfg.SelfUpdate.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := selfupdate.Update(ctx, selfupdate.Options{
		CurrentVersion: launcherVersion.Value,
		RuntimeDir:     cfg.RuntimeDir,
		BaseURL:        *baseURL,
		TargetVersion:  *target,
		HealthURL:      app.LauncherHealthURL(cfg.ListenAddr),
		Apply:          *apply,
		Restart:        *restart,
		DryRun:         *dryRun,
		Progress: func(received int64, total int64) {
			if total > 0 {
				fmt.Fprintf(os.Stderr, "\r下载 launcher: %d/%d", received, total)
			}
		},
	})
	if err != nil {
		return err
	}
	if result.Downloaded {
		fmt.Fprintln(os.Stderr)
	}
	printJSON(result)
	if result.Applied {
		return nil
	}
	return nil
}

func runSelfUpdateApply(args []string) error {
	return selfupdate.RunApplyCommand(args)
}

func printJSON(value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Println("{}")
		return
	}
	fmt.Println(string(data))
}
