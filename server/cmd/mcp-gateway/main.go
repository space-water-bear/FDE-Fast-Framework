package main

import (
	"log"
	"net/http"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	gw := NewGateway(cfg)
	log.Printf("mcp-gateway 启动: addr=%s upstream=%s", cfg.Addr, cfg.Upstream)
	if err := http.ListenAndServe(cfg.Addr, gw.Handler()); err != nil {
		log.Fatalf("mcp-gateway 启动失败: %v", err)
	}
}
