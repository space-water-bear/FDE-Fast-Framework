package config

import (
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App     App     `yaml:"app"`
	DB      DB      `yaml:"db"`
	JWT     JWT     `yaml:"jwt"`
	Seed    Seed    `yaml:"seed"`
	Codegen Codegen `yaml:"codegen"`
	Captcha Captcha `yaml:"captcha"`
	Upload  Upload  `yaml:"upload"`
	Zap     Zap     `yaml:"zap"`
	MCP     MCP     `yaml:"mcp"`
}

type App struct {
	Name string `yaml:"name"`
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"`
}

type DB struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

type JWT struct {
	Secret              string `yaml:"secret"`
	AccessExpiresHours  int    `yaml:"access-expires-hours"`
	RefreshExpiresHours int    `yaml:"refresh-expires-hours"`
	Issuer              string `yaml:"issuer"`
	UseMultipoint       bool   `yaml:"use-multipoint"` // true=允许多设备同时在线; false=单点登录(新登录踢掉旧设备)
}

type Seed struct {
	Enabled       bool   `yaml:"enabled"`
	AdminUsername string `yaml:"admin-username"`
	AdminPassword string `yaml:"admin-password"`
}

// Codegen 代码生成器输出目录(相对 server 运行目录)
type Codegen struct {
	ServerDir string `yaml:"server-dir"`
	WebDir    string `yaml:"web-dir"`
}

// Upload 文件上传
type Upload struct {
	Dir       string `yaml:"dir"`
	MaxSizeMB int    `yaml:"max-size-mb"`
}

// Zap zap 结构化日志
type Zap struct {
	Level        string `yaml:"level"`
	Director     string `yaml:"director"`
	LogInConsole bool   `yaml:"log-in-console"`
}

// MCP 服务间 API Key 配置(cmd/mcp-gateway 经 /api/mcp/* 调用主应用)
type MCP struct {
	APIKey string `yaml:"api-key"`
}

// Captcha 登录验证码
type Captcha struct {
	Enabled       bool `yaml:"enabled"`
	ExpireMinutes int  `yaml:"expire-minutes"`
}

// Load 读取 yaml 配置并应用环境变量覆盖
// 支持的环境变量: FDE_PORT / FDE_DB_DRIVER / FDE_DB_DSN / FDE_JWT_SECRET / FDE_CAPTCHA_ENABLED / FDE_MCP_API_KEY
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.applyEnv()
	if cfg.App.Port == 0 {
		cfg.App.Port = 8080
	}
	if cfg.JWT.AccessExpiresHours == 0 {
		cfg.JWT.AccessExpiresHours = 2
	}
	if cfg.JWT.RefreshExpiresHours == 0 {
		cfg.JWT.RefreshExpiresHours = 168
	}
	if cfg.JWT.Secret == "" {
		cfg.JWT.Secret = "fde-admin-secret-change-me"
	}
	if cfg.Captcha.ExpireMinutes == 0 {
		cfg.Captcha.ExpireMinutes = 5
	}
	if cfg.Upload.Dir == "" {
		cfg.Upload.Dir = "./uploads"
	}
	if cfg.Upload.MaxSizeMB == 0 {
		cfg.Upload.MaxSizeMB = 10
	}
	if cfg.Zap.Level == "" {
		cfg.Zap.Level = "info"
	}
	if cfg.Zap.Director == "" {
		cfg.Zap.Director = "./log"
	}
	if cfg.Codegen.ServerDir == "" {
		cfg.Codegen.ServerDir = "."
	}
	if cfg.Codegen.WebDir == "" {
		cfg.Codegen.WebDir = "../web"
	}
	return cfg, nil
}

func (c *Config) applyEnv() {
	if v := os.Getenv("FDE_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.App.Port = p
		}
	}
	if v := os.Getenv("FDE_DB_DRIVER"); v != "" {
		c.DB.Driver = v
	}
	if v := os.Getenv("FDE_DB_DSN"); v != "" {
		c.DB.DSN = v
	}
	if v := os.Getenv("FDE_JWT_SECRET"); v != "" {
		c.JWT.Secret = v
	}
	if v := os.Getenv("FDE_CAPTCHA_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Captcha.Enabled = b
		}
	}
	if v := os.Getenv("FDE_JWT_USE_MULTIPOINT"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.JWT.UseMultipoint = b
		}
	}
	if v := os.Getenv("FDE_MCP_API_KEY"); v != "" {
		c.MCP.APIKey = v
	}
}
