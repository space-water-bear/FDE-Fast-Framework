package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfigYAML(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return path
}

func TestLoadMCPAPIKeyEmptyByDefault(t *testing.T) {
	t.Setenv("FDE_MCP_API_KEY", "")
	cfg, err := Load(writeConfigYAML(t, `mcp:
  api-key: ""
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MCP.APIKey != "" {
		t.Fatalf("MCP.APIKey = %q, want empty", cfg.MCP.APIKey)
	}
}

func TestLoadMCPAPIKeyEnvOverride(t *testing.T) {
	t.Setenv("FDE_MCP_API_KEY", "secret-key")
	cfg, err := Load(writeConfigYAML(t, `mcp:
  api-key: ""
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MCP.APIKey != "secret-key" {
		t.Fatalf("MCP.APIKey = %q, want %q", cfg.MCP.APIKey, "secret-key")
	}
}

func TestLoadMCPAPIKeyYAMLValue(t *testing.T) {
	t.Setenv("FDE_MCP_API_KEY", "")
	cfg, err := Load(writeConfigYAML(t, `mcp:
  api-key: "yaml-key"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MCP.APIKey != "yaml-key" {
		t.Fatalf("MCP.APIKey = %q, want %q", cfg.MCP.APIKey, "yaml-key")
	}
}
