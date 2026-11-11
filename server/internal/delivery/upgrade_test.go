package delivery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallUpgradeStagesVerifiedPackageAndActivatesExplicitly(t *testing.T) {
	old, next := packFixture(t), packFixture(t)
	target := t.TempDir()
	installed, err := Install(old.Path, target)
	if err != nil {
		t.Fatal(err)
	}
	dataPath := filepath.Join(installed.SharedDir, "data", "app.db")
	if err := os.WriteFile(dataPath, []byte("existing-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		result, err := InstallUpgrade(context.Background(), old.Path, next.Path, target)
		if err != nil || result.ReleaseID != next.ID || result.SharedDir != installed.SharedDir || !result.Installed {
			t.Fatalf("stage: %+v %v", result, err)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "current.json")); !os.IsNotExist(err) {
		t.Fatal("staging activated candidate")
	}
	if err := ActivateUpgrade(context.Background(), old.Path, next.Path, target); err != nil {
		t.Fatal(err)
	}
	if err := RollbackInstall(context.Background(), old.Path, next.Path, target); err != nil {
		t.Fatal(err)
	}
	var active installJournal
	raw, err := os.ReadFile(filepath.Join(target, "current.json"))
	if err != nil || json.Unmarshal(raw, &active) != nil || active.ReleaseID != old.ID {
		t.Fatalf("active pointer: %+v %v", active, err)
	}
	data, err := os.ReadFile(dataPath)
	if err != nil || string(data) != "existing-data" {
		t.Fatal("package staging touched business data")
	}
}

func TestInstallUpgradeRejectsTargetAndLockSymlinks(t *testing.T) {
	for _, kind := range []string{"target", "lock"} {
		t.Run(kind, func(t *testing.T) {
			old, next := packFixture(t), packFixture(t)
			target := t.TempDir()
			if _, err := Install(old.Path, target); err != nil {
				t.Fatal(err)
			}
			if kind == "target" {
				linked := filepath.Join(t.TempDir(), "target")
				if err := os.Symlink(target, linked); err != nil {
					t.Fatal(err)
				}
				target = linked
			} else {
				lock := filepath.Join(target, ".install.lock")
				if err := os.Remove(lock); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(t.TempDir(), "lock"), lock); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := InstallUpgrade(context.Background(), old.Path, next.Path, target); err == nil {
				t.Fatal("unsafe target accepted")
			}
		})
	}
}

func TestInstallUpgradeRetryRejectsSameIDManifestChangesAndInjectedFiles(t *testing.T) {
	for _, change := range []string{"manifest", "file"} {
		t.Run(change, func(t *testing.T) {
			old, next := packFixture(t), packFixture(t)
			target := t.TempDir()
			if _, err := Install(old.Path, target); err != nil {
				t.Fatal(err)
			}
			result, err := InstallUpgrade(context.Background(), old.Path, next.Path, target)
			if err != nil {
				t.Fatal(err)
			}
			if change == "manifest" {
				path := filepath.Join(result.ReleasePath, "manifest.json")
				raw, _ := os.ReadFile(path)
				var m Manifest
				_ = json.Unmarshal(raw, &m)
				m.SourceCommit = "changed-source-claim"
				raw, _ = json.Marshal(m)
				if err := os.WriteFile(path, raw, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(filepath.Join(result.ReleasePath, "extra.txt"), []byte("injected"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := InstallUpgrade(context.Background(), old.Path, next.Path, target); err == nil {
				t.Fatal("tampered staged package accepted")
			}
		})
	}
}
