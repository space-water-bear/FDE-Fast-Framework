package main

import (
	"fmt"
	"log"

	"fde-admin/internal/config"
	"fde-admin/internal/initialize"
	"fde-admin/internal/router"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	initialize.Run(cfg)
	r := router.NewRouter()
	addr := fmt.Sprintf(":%d", cfg.App.Port)
	log.Printf("%s 启动成功: http://127.0.0.1%s (db=%s)", cfg.App.Name, addr, cfg.DB.Driver)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
