package main

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	defaultAddr     = ":8090"
	defaultUpstream = "http://127.0.0.1:8080"
)

// Config 是网关进程配置。环境变量优先于可选的 config.yaml。
type Config struct {
	Addr      string
	Upstream  string
	APIKey    string
	AuthToken string
}

// LoadConfig 读取 config.yaml(可选)与环境变量, 并校验 API Key 必填。
func LoadConfig() (Config, error) {
	return loadConfig("config.yaml", os.ReadFile, os.Getenv)
}

func loadConfig(path string, readFile func(string) ([]byte, error), getenv func(string) string) (Config, error) {
	cfg := Config{Addr: defaultAddr, Upstream: defaultUpstream}
	if b, err := readFile(path); err == nil {
		var raw map[string]any
		if err := yaml.Unmarshal(b, &raw); err != nil {
			return Config{}, fmt.Errorf("解析 %s 失败: %w", path, err)
		}
		if v, ok := raw["addr"].(string); ok && v != "" {
			cfg.Addr = v
		}
		if v, ok := raw["upstream"].(string); ok && v != "" {
			cfg.Upstream = v
		}
		if v, ok := raw["api_key"].(string); ok && v != "" {
			cfg.APIKey = v
		}
		if v, ok := raw["auth_token"].(string); ok && v != "" {
			cfg.AuthToken = v
		}
	}
	if v := getenv("GW_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := getenv("GW_UPSTREAM"); v != "" {
		cfg.Upstream = v
	}
	if v := getenv("GW_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := getenv("GW_AUTH_TOKEN"); v != "" {
		cfg.AuthToken = v
	}
	if cfg.APIKey == "" {
		return Config{}, errors.New("GW_API_KEY 不能为空")
	}
	if cfg.AuthToken == "" {
		return Config{}, errors.New("GW_AUTH_TOKEN 不能为空")
	}
	return cfg, nil
}
