package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestExplicitServeNeverBootstrapsDebugConfiguration(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "must-not-be-created.db")
	configuration := filepath.Join(root, "config.yaml")
	raw := "app:\n  name: Runtime guard test\n  mode: debug\ndb:\n  driver: sqlite\n  dsn: " + strconv.Quote(database) + "\njwt:\n  secret: " + strconv.Quote(strings.Repeat("s", 40)) + "\nseed:\n  enabled: true\n"
	if err := os.WriteFile(configuration, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := os.Args
	t.Cleanup(func() { os.Args = previous })
	os.Args = []string{"fde-server", "serve", "--config", configuration}
	if err := run(); err == nil {
		t.Fatal("serve without a verified manifest started development initialization")
	}
	if _, err := os.Stat(database); !os.IsNotExist(err) {
		t.Fatalf("explicit serve wrote a database using debug config: %v", err)
	}
	if _, err := os.Stat(configuration + ".bootstrap.json"); !os.IsNotExist(err) {
		t.Fatal("serve wrote a bootstrap journal")
	}
}
