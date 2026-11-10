package main

import (
	"context"
	"errors"
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
		log.Printf("服务失败: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	initialize.Run(cfg)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	r := router.NewRouter()
	addr := fmt.Sprintf(":%d", cfg.App.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("启动监听失败: %w", err)
	}
	defer listener.Close()
	stopBots := service.StartWeComBots(ctx)
	defer stopBots()
	stopDingTalk := service.StartDingTalkBots(ctx)
	defer stopDingTalk()
	log.Printf("%s 启动成功: http://127.0.0.1%s (db=%s)", cfg.App.Name, addr, cfg.DB.Driver)
	server := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 10 * time.Second}
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()
	var runErr error
	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
		cancel()
	case <-ctx.Done():
	}
	shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		if runErr == nil {
			runErr = err
		}
	}
	return runErr
}
