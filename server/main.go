package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fde-admin/internal/config"
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
	command := "workbench"
	if len(args) > 0 && (args[0] == "bootstrap" || args[0] == "verify" || args[0] == "serve" || args[0] == "workbench") {
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
	if flags.NArg() != 0 {
		return fmt.Errorf("unknown server command or positional argument: %s", flags.Arg(0))
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	cfg.Runtime.ManifestPath = *manifest
	cfg.Runtime.WebDir = *web
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
	if command == "serve" {
		cfg.App.Mode = "release"
		if err := initialize.VerifyRuntime(cfg, *manifest); err != nil {
			return err
		}
	} else {
		if cfg.App.Mode == "release" || *manifest != "" {
			return fmt.Errorf("workbench仅用于开发；运行交付产物请显式使用serve --manifest")
		}
		initialize.Run(cfg)
	}

	server := &http.Server{Addr: fmt.Sprintf(":%d", cfg.App.Port), Handler: router.NewRouter(), ReadHeaderTimeout: 10 * time.Second}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("启动监听失败: %w", err)
	}
	defer listener.Close()
	if command == "workbench" {
		if err := service.StartDevelopmentWorker(ctx); err != nil {
			return err
		}
		stopWeCom := service.StartWeComBots(ctx)
		defer stopWeCom()
		stopDingTalk := service.StartDingTalkBots(ctx)
		defer stopDingTalk()
	}

	log.Printf("%s listening on %s", cfg.App.Name, server.Addr)
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()
	var runErr error
	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
		stop()
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return errors.Join(runErr, server.Shutdown(shutdown))
}
