package initialize

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fde-admin/internal/config"
	"fde-admin/internal/delivery"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/runner"
	"fde-admin/internal/service"
	"github.com/google/uuid"
)

const upgradeFixtureKey = "biz_upgrade_fixture"

func upgradeFixtureAssets(t *testing.T, next bool) service.BusinessInstallationAssets {
	t.Helper()
	raw := `{"objectKey":"biz_upgrade_fixture","name":"旧工单","version":1,"fields":{"ticketNo":{"type":"string","required":true,"x-fde":{"type":"string","column":"ticket_no"}}}}`
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatal(err)
	}
	if next {
		schema["name"] = "升级后工单"
		schema["version"] = float64(2)
		fields := schema["fields"].(map[string]any)
		fields["priority"] = map[string]any{"type": "string", "enum": []any{"normal", "urgent"}, "default": "normal", "required": true, "x-fde": map[string]any{"type": "enum"}}
		fields["reportLevel"] = map[string]any{"type": "integer", "required": false, "x-fde": map[string]any{"column": "report_level", "type": "int"}}
		if _, ok := global.GeneratedBusiness(upgradeFixtureKey); !ok {
			encoded, _ := json.Marshal(schema)
			global.RegisterGeneratedBusiness(global.BusinessRegistration{ObjectKey: upgradeFixtureKey, TableName: upgradeFixtureKey, Path: "upgrade_fixture", Name: "UpgradeFixture", Component: "generated/upgrade_fixture/index", SchemaJSON: string(encoded)})
		}
	}
	return service.BusinessInstallationAssets{Objects: []service.BusinessInstallationAsset{{ObjectKey: upgradeFixtureKey, TableName: upgradeFixtureKey, Name: schema["name"].(string), Schema: schema, Menu: service.CodegenMenuPlanItem{Path: "upgrade_fixture", Name: "UpgradeFixture", Title: schema["name"].(string), Component: "generated/upgrade_fixture/index"}}}}
}

func upgradeFixturePackage(t *testing.T, assets service.BusinessInstallationAssets) delivery.Package {
	t.Helper()
	workspace := t.TempDir()
	artifacts := t.TempDir()
	reportDir := t.TempDir()
	assetJSON, _ := json.Marshal(assets)
	files := map[string][]byte{"LICENSE": []byte("MIT"), "server/main.go": []byte("package main\n"), "web/package.json": []byte("{}"), "fde/assets/business.json": assetJSON}
	for path, data := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(workspace, path)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workspace, path), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	elf := make([]byte, 64)
	copy(elf, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(elf[16:], 2)
	binary.LittleEndian.PutUint16(elf[18:], 183)
	binary.LittleEndian.PutUint32(elf[20:], 1)
	binary.LittleEndian.PutUint16(elf[52:], 64)
	binary.LittleEndian.PutUint16(elf[54:], 56)
	binary.LittleEndian.PutUint16(elf[58:], 64)
	for _, name := range []string{"server", "fde"} {
		if err := os.WriteFile(filepath.Join(artifacts, name), elf, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(artifacts, "web"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "web/index.html"), []byte("checked"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceHash, err := runner.SnapshotHash(workspace)
	if err != nil {
		t.Fatal(err)
	}
	artifactHashes, err := runner.HashTree(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	report := runner.Report{ID: uuid.NewString(), Profile: runner.Profile, Success: true, SnapshotHash: sourceHash, ImageID: "sha256:test", GOOS: "linux", GOARCH: "arm64", ArtifactsDir: artifacts, Artifacts: artifactHashes, Toolchain: map[string]string{"go": "1.26.0", "node": "22.22.3", "pnpm": "11.23.0"}}
	for _, name := range []string{"go-version", "node-version", "pnpm-version", "go-dependencies", "web-dependencies", "go-format", "go-test", "go-build", "cli-build", "web-test", "typescript", "vite-build"} {
		report.Steps = append(report.Steps, runner.Step{Name: name})
	}
	raw, _ := json.Marshal(report)
	reportPath := filepath.Join(reportDir, "checks.json")
	if err := os.WriteFile(reportPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, err := delivery.Pack(context.Background(), delivery.PackRequest{Workspace: workspace, CheckReportPath: reportPath, OutputDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

type runtimeUpgradeFixture struct {
	cfg                                  *config.Config
	configPath, oldManifest, newManifest string
	ownedMenu, otherMenu, operator       uint
}

func setupRuntimeUpgrade(t *testing.T) runtimeUpgradeFixture {
	t.Helper()
	before := upgradeFixtureAssets(t, false)
	after := upgradeFixtureAssets(t, true)
	old := upgradeFixturePackage(t, before)
	next := upgradeFixturePackage(t, after)
	root := t.TempDir()
	cfg := guardRuntimeConfig(t, filepath.Join(root, "app.db"))
	cfg.Upload.Dir = filepath.Join(root, "uploads")
	if err := os.MkdirAll(cfg.Upload.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Upload.Dir, "attachment.txt"), []byte("original upload"), 0o600); err != nil {
		t.Fatal(err)
	}
	db := openGuardRuntimeDB(t, cfg.DB.DSN)
	if err := db.AutoMigrate(&model.User{}, &model.Role{}, &model.Menu{}, &model.RuntimeInstallation{}, &model.BizObject{}, &model.FormSchema{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE casbin_rule(id INTEGER PRIMARY KEY AUTOINCREMENT,ptype TEXT,v0 TEXT,v1 TEXT,v2 TEXT,v3 TEXT,v4 TEXT,v5 TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE biz_upgrade_fixture(id INTEGER PRIMARY KEY,ticket_no TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 20; id++ {
		if err := db.Exec("INSERT INTO biz_upgrade_fixture(id,ticket_no) VALUES(?,?)", id, "original").Error; err != nil {
			t.Fatal(err)
		}
	}
	oldHash, _ := runner.FileHash(filepath.Join(old.Path, "manifest.json"))
	if err := db.Create(&model.RuntimeInstallation{ReleaseID: old.ID, ManifestHash: oldHash, SourceHash: old.Manifest.SourceHash, Complete: true}).Error; err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(before.Objects[0].Schema)
	object := model.BizObject{Key: upgradeFixtureKey, ProjectID: model.DefaultProjectID, Name: before.Objects[0].Name, Table: upgradeFixtureKey, Schema: string(raw), Version: 1, ActiveVersion: 1}
	if err := db.Create(&object).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.FormSchema{ObjectID: object.ID, Version: 1, Schema: string(raw), Published: true}).Error; err != nil {
		t.Fatal(err)
	}
	owned := model.Menu{Name: "UpgradeFixture", Path: "upgrade_fixture", Component: "generated/upgrade_fixture/index", Title: before.Objects[0].Name, Type: 1}
	other := model.Menu{Name: "Unrelated", Path: "unrelated", ApiPath: "/api/unrelated", ApiMethod: "GET", Type: 2}
	if err := db.Create(&owned).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	role := model.Role{Name: "业务", Code: "operator", Enabled: true, Menus: []model.Menu{owned}}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.User{Username: "qa", Password: "hash", Enabled: true, Roles: []model.Role{role}}).Error; err != nil {
		t.Fatal(err)
	}
	closeGuardRuntimeDB(t, db)
	return runtimeUpgradeFixture{cfg: cfg, configPath: filepath.Join(root, "config.yaml"), oldManifest: filepath.Join(old.Path, "manifest.json"), newManifest: filepath.Join(next.Path, "manifest.json"), ownedMenu: owned.ID, otherMenu: other.ID, operator: role.ID}
}
func (f runtimeUpgradeFixture) upgrade(t *testing.T) RuntimeUpgradeResult {
	t.Helper()
	result, err := UpgradeRuntime(context.Background(), f.cfg, f.configPath, f.oldManifest, f.newManifest)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func (f runtimeUpgradeFixture) rollback() (RuntimeUpgradeResult, error) {
	return RollbackRuntime(context.Background(), f.cfg, f.configPath, f.oldManifest, f.newManifest)
}

func TestRuntimeUpgradeAdditiveBackfillAndRealSnapshotRollback(t *testing.T) {
	f := setupRuntimeUpgrade(t)
	result := f.upgrade(t)
	if result.State != "applied" || result.BackupID == "" {
		t.Fatalf("missing upgrade evidence: %+v", result)
	}
	f.upgrade(t)
	db := openGuardRuntimeDB(t, f.cfg.DB.DSN)
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM biz_upgrade_fixture WHERE priority='normal' AND report_level IS NULL").Scan(&count).Error; err != nil || count != 20 {
		t.Fatalf("backfill: %d %v", count, err)
	}
	if err := db.Exec("UPDATE biz_upgrade_fixture SET priority='invalid' WHERE id=1").Error; err == nil {
		t.Fatal("enum constraint missing")
	}
	if err := db.Exec("INSERT INTO role_menus(role_id,menu_id) VALUES(?,?)", f.operator, f.otherMenu).Error; err != nil {
		t.Fatal(err)
	}
	closeGuardRuntimeDB(t, db)
	rolled, err := f.rollback()
	if err != nil || rolled.State != "rolled_back" {
		t.Fatalf("rollback: %+v %v", rolled, err)
	}
	if _, err := f.rollback(); err != nil {
		t.Fatalf("repeat rollback: %v", err)
	}
	db = openGuardRuntimeDB(t, f.cfg.DB.DSN)
	defer closeGuardRuntimeDB(t, db)
	if db.Migrator().HasColumn(upgradeFixtureKey, "priority") {
		t.Fatal("rollback did not restore original database schema")
	}
	if err := db.Raw("SELECT COUNT(*) FROM biz_upgrade_fixture WHERE ticket_no='original'").Scan(&count).Error; err != nil || count != 20 {
		t.Fatalf("original data: %d %v", count, err)
	}
	if err := db.Table("role_menus").Where("role_id = ? AND menu_id = ?", f.operator, f.otherMenu).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("unrelated manual grant lost")
	}
	var menu model.Menu
	if err := db.First(&menu, f.ownedMenu).Error; err != nil || menu.Title != "旧工单" {
		t.Fatal("old menu metadata not restored")
	}
	upload, err := os.ReadFile(filepath.Join(f.cfg.Upload.Dir, "attachment.txt"))
	if err != nil || string(upload) != "original upload" {
		t.Fatal("upload backup not restored")
	}
}

func TestRuntimeRollbackRejectsNewDataUploadsAndManagedPermissionConflicts(t *testing.T) {
	for _, mutation := range []string{"new-business", "changed-business", "uploads", "owned-grant", "owned-menu", "backup"} {
		t.Run(mutation, func(t *testing.T) {
			f := setupRuntimeUpgrade(t)
			result := f.upgrade(t)
			db := openGuardRuntimeDB(t, f.cfg.DB.DSN)
			switch mutation {
			case "new-business":
				if err := db.Exec("INSERT INTO biz_upgrade_fixture(id,ticket_no,priority) VALUES(21,'new','urgent')").Error; err != nil {
					t.Fatal(err)
				}
			case "changed-business":
				if err := db.Exec("UPDATE biz_upgrade_fixture SET priority='urgent' WHERE id=1").Error; err != nil {
					t.Fatal(err)
				}
			case "uploads":
				if err := os.WriteFile(filepath.Join(f.cfg.Upload.Dir, "new.txt"), []byte("new"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "owned-grant":
				if err := db.Exec("DELETE FROM role_menus WHERE menu_id=?", f.ownedMenu).Error; err != nil {
					t.Fatal(err)
				}
			case "owned-menu":
				if err := db.Model(&model.Menu{}).Where("id=?", f.ownedMenu).Update("title", "manual").Error; err != nil {
					t.Fatal(err)
				}
			case "backup":
				var j runtimeUpgradeJournal
				raw, _ := os.ReadFile(result.JournalPath)
				_ = json.Unmarshal(raw, &j)
				if err := os.WriteFile(j.DatabaseBackup, []byte("tampered"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			closeGuardRuntimeDB(t, db)
			if _, err := f.rollback(); err == nil {
				t.Fatal("unsafe snapshot rollback accepted")
			}
			db = openGuardRuntimeDB(t, f.cfg.DB.DSN)
			defer closeGuardRuntimeDB(t, db)
			if !db.Migrator().HasColumn(upgradeFixtureKey, "priority") {
				t.Fatal("rejected restore modified live schema")
			}
		})
	}
}

func TestRuntimeUpgradeFailurePreservesBackupAndCanRetry(t *testing.T) {
	f := setupRuntimeUpgrade(t)
	runtimeUpgradeStepHook = func(phase string) error {
		if phase == "preview_verified" {
			return errors.New("injected after verified preview")
		}
		return nil
	}
	result, err := UpgradeRuntime(context.Background(), f.cfg, f.configPath, f.oldManifest, f.newManifest)
	runtimeUpgradeStepHook = nil
	if err == nil || result.State != "backed_up" {
		t.Fatalf("missing failure evidence: %+v %v", result, err)
	}
	db := openGuardRuntimeDB(t, f.cfg.DB.DSN)
	if db.Migrator().HasColumn(upgradeFixtureKey, "priority") {
		t.Fatal("failed preview modified live DB")
	}
	closeGuardRuntimeDB(t, db)
	f.upgrade(t)
}

func TestRuntimeUpgradeLiveTransactionFailureCanRetryAndRecoverOrphanBackup(t *testing.T) {
	f := setupRuntimeUpgrade(t)
	transactions := 0
	runtimeUpgradeStepHook = func(phase string) error {
		if phase == "transaction_after_columns" {
			transactions++
			if transactions == 2 {
				return errors.New("injected live transaction failure")
			}
		}
		return nil
	}
	result, err := UpgradeRuntime(context.Background(), f.cfg, f.configPath, f.oldManifest, f.newManifest)
	runtimeUpgradeStepHook = nil
	if err == nil || result.State != "backed_up" {
		t.Fatalf("live failure did not retain backup: %+v %v", result, err)
	}
	db := openGuardRuntimeDB(t, f.cfg.DB.DSN)
	if db.Migrator().HasColumn(upgradeFixtureKey, "priority") {
		t.Fatal("live DDL was not rolled back")
	}
	closeGuardRuntimeDB(t, db)
	// Simulate process loss after a durable backup but before the external journal write.
	if err := os.Remove(result.JournalPath); err != nil {
		t.Fatal(err)
	}
	f.upgrade(t)
}

func TestRuntimeMigrationRejectsDestructiveChangesAndMissingDefault(t *testing.T) {
	for _, change := range []string{"drop", "type", "required-default", "rename"} {
		t.Run(change, func(t *testing.T) {
			before := upgradeFixtureAssets(t, false)
			after := upgradeFixtureAssets(t, true)
			fields := after.Objects[0].Schema["fields"].(map[string]any)
			switch change {
			case "drop":
				delete(fields, "ticketNo")
			case "rename":
				fields["renamed"] = fields["ticketNo"]
				delete(fields, "ticketNo")
			case "type":
				fields["ticketNo"].(map[string]any)["type"] = "integer"
			case "required-default":
				delete(fields["priority"].(map[string]any), "default")
			}
			if _, err := planRuntimeColumns(before, after); err == nil {
				t.Fatal("unsupported destructive migration accepted")
			}
		})
	}
}

func TestRuntimeUpgradeUsesSameDatabaseMaintenanceLock(t *testing.T) {
	f := setupRuntimeUpgrade(t)
	unlock, err := lockRuntimeBootstrap(f.cfg.DB.DSN + ".maintenance")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := UpgradeRuntime(context.Background(), f.cfg, f.configPath, f.oldManifest, f.newManifest); err == nil || !strings.Contains(err.Error(), "运行") {
		t.Fatalf("concurrent migration accepted: %v", err)
	}
}

func TestRuntimeUpgradeRespectsServingLockAndLegacyColumnMapping(t *testing.T) {
	f := setupRuntimeUpgrade(t)
	unlock, err := LockRuntimeServing(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpgradeRuntime(context.Background(), f.cfg, f.configPath, f.oldManifest, f.newManifest); err == nil {
		unlock()
		t.Fatal("upgrade acquired exclusive lock while runtime was serving")
	}
	unlock()
	before, after := upgradeFixtureAssets(t, false), upgradeFixtureAssets(t, true)
	oldFields := before.Objects[0].Schema["fields"].(map[string]any)
	delete(oldFields["ticketNo"].(map[string]any), "x-fde")
	columns, err := planRuntimeColumns(before, after)
	if err != nil {
		t.Fatalf("legacy camel-case mapping: %v", err)
	}
	found := false
	for _, column := range columns {
		if column.Field == "report_level" {
			found = true
		}
	}
	if !found {
		t.Fatal("JSON reportLevel was not mapped to report_level")
	}
	for _, name := range []string{"ticket_no", "ticketNo"} {
		column, err := runtimeFieldColumn(name, map[string]any{})
		if err != nil || column != "ticket_no" {
			t.Fatalf("legacy field %s resolved to %s: %v", name, column, err)
		}
	}
	if _, err := runtimeFieldColumn("reportLevel", map[string]any{"x-fde": map[string]any{"column": "../escape"}}); err == nil {
		t.Fatal("unsafe explicit database column accepted")
	}
}
