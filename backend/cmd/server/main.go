package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"relay-server/internal/app"
	"relay-server/internal/store"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "cleanup-legacy-plans" {
		if err := runCleanupLegacyPlans(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "cleanup-plan-history" {
		if err := runCleanupPlanHistory(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	addr := env("ADDR", ":8080")
	application, err := app.New()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := application.Close(); err != nil {
			log.Printf("close application store: %v", err)
		}
	}()
	srv := &http.Server{
		Addr:              addr,
		Handler:           application.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	log.Printf("relay server listening on %s", addr)
	serverErr := make(chan error, 1)
	go func() { serverErr <- srv.ListenAndServe() }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	case sig := <-signals:
		log.Printf("shutdown signal=%s", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
			_ = srv.Close()
		}
		if err := <-serverErr; err != nil && err != http.ErrServerClosed {
			log.Printf("server stopped: %v", err)
		}
	}
}

func runCleanupLegacyPlans(args []string) error {
	fs := flag.NewFlagSet("cleanup-legacy-plans", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "apply cleanup changes")
	backup := fs.String("backup", "", "backup file path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	path := *backup
	if *apply && path == "" {
		name := "legacy-plan-cleanup-" + time.Now().UTC().Format("20060102_150405") + ".json"
		path = filepath.Join(os.TempDir(), name)
	}

	archive, err := store.NewTaskArchiveFromEnv()
	if err != nil {
		return err
	}
	defer archive.Close()

	mysql, ok := archive.(*store.MySQLArchive)
	if !ok {
		return errUnsupportedArchive()
	}

	stats, err := mysql.CleanupLegacyPlans(context.Background(), store.LegacyPlanCleanupOptions{
		Apply:      *apply,
		BackupPath: path,
	})
	if err != nil {
		return err
	}

	buf, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}
	log.Print(string(buf))
	return nil
}

func runCleanupPlanHistory(args []string) error {
	fs := flag.NewFlagSet("cleanup-plan-history", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "apply cleanup changes")
	backup := fs.String("backup", "", "backup file path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	path := *backup
	if *apply && path == "" {
		name := "plan-history-cleanup-" + time.Now().UTC().Format("20060102_150405") + ".json"
		path = filepath.Join(os.TempDir(), name)
	}

	archive, err := store.NewTaskArchiveFromEnv()
	if err != nil {
		return err
	}
	defer archive.Close()

	mysql, ok := archive.(*store.MySQLArchive)
	if !ok {
		return errUnsupportedArchive()
	}

	stats, err := mysql.CleanupPlanHistory(context.Background(), store.PlanHistoryCleanupOptions{
		Apply:      *apply,
		BackupPath: path,
	})
	if err != nil {
		return err
	}

	buf, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}
	log.Print(string(buf))
	return nil
}

func errUnsupportedArchive() error {
	return &unsupportedArchiveError{}
}

type unsupportedArchiveError struct{}

func (*unsupportedArchiveError) Error() string {
	return "cleanup-legacy-plans requires MySQL archive"
}

func env(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}
