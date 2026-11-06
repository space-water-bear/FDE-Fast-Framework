package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"fde-admin/internal/config"
	"fde-admin/internal/delivery"
	"fde-admin/internal/initialize"
	"fde-admin/internal/router"
	"fde-admin/internal/service"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	args := os.Args[1:]
	command := "serve"
	if len(args) > 0 && (args[0] == "bootstrap" || args[0] == "verify" || args[0] == "serve") {
		command = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	configPath := flags.String("config", "config.yaml", "configuration file")
	assets := flags.String("assets", "", "frozen initialization assets")
	manifest := flags.String("manifest", "", "verified release manifest")
	web := flags.String("web", "", "built frontend directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	cfg.Runtime.ManifestPath = *manifest
	cfg.Runtime.WebDir = *web
	if *manifest != "" {
		pkg, err := delivery.Inspect(filepath.Dir(*manifest))
		if err != nil {
			return err
		}
		cfg.App.Mode = "release"
		cfg.Runtime.ReleaseID = pkg.ID
		cfg.Runtime.SourceHash = pkg.Manifest.SourceHash
	}
	switch command {
	case "bootstrap":
		if *assets == "" {
			return fmt.Errorf("bootstrap requires --assets")
		}
		if err := initialize.BootstrapRuntime(cfg, *configPath, *assets, *manifest); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"bootstrapped": true, "releaseId": cfg.Runtime.ReleaseID})
	case "verify":
		if err := initialize.VerifyRuntime(cfg, *manifest); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"verified": true, "releaseId": cfg.Runtime.ReleaseID})
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.App.Mode == "release" {
		if err := initialize.VerifyRuntime(cfg, *manifest); err != nil {
			return err
		}
	} else {
		initialize.Run(cfg)
		if err := service.StartDevelopmentWorker(ctx); err != nil {
			return err
		}
	}
	server := &http.Server{Addr: fmt.Sprintf(":%d", cfg.App.Port), Handler: router.NewRouter(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("%s listening on %s", cfg.App.Name, server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
