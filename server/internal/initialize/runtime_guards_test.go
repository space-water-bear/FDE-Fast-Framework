package initialize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"fde-admin/internal/config"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/runner"
	"fde-admin/internal/service"
)

func runtimeGuardAsset(t *testing.T) service.BusinessInstallationAssets {
	t.Helper()
	raw := `{"objectKey":"biz_runtime_guard_fixture","name":"Guard fixture","version":1,"fields":{"name":{"type":"string","required":true}}}`
	if _, exists := global.GeneratedBusiness("biz_runtime_guard_fixture"); !exists {
		global.RegisterGeneratedBusiness(global.BusinessRegistration{ObjectKey: "biz_runtime_guard_fixture", TableName: "biz_runtime_guard_fixture", Path: "runtime_guard_fixture", Name: "RuntimeGuardFixture", Component: "generated/runtime_guard_fixture/index", SchemaJSON: raw})
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatal(err)
	}
	return service.BusinessInstallationAssets{Objects: []service.BusinessInstallationAsset{{ObjectKey: "biz_runtime_guard_fixture", Name: "Guard fixture", TableName: "biz_runtime_guard_fixture", Schema: schema, Menu: service.CodegenMenuPlanItem{Path: "runtime_guard_fixture", Name: "RuntimeGuardFixture", Title: "Guard fixture", Component: "generated/runtime_guard_fixture/index"}}}}
}
func TestRuntimeBusinessAssetsMustMatchCompiledRegistration(t *testing.T) {
	if err := validateRuntimeBusinessAssets(runtimeGuardAsset(t)); err != nil {
		t.Fatalf("valid compiled asset: %v", err)
	}
	for _, name := range []string{"reserved", "wildcard", "traversal", "table", "unregistered", "component", "name", "schema", "title", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			assets := runtimeGuardAsset(t)
			asset := &assets.Objects[0]
			switch name {
			case "reserved":
				asset.Menu.Path = "user"
			case "wildcard":
				asset.Menu.Path = "*"
			case "traversal":
				asset.Menu.Path = "../role"
			case "table":
				asset.TableName = "sys_users"
			case "unregistered":
				asset.ObjectKey = "biz_unregistered"
			case "component":
				asset.Menu.Component = "system/user/index"
			case "name":
				asset.Menu.Name = "RuntimeUsers"
			case "schema":
				asset.Schema["fields"].(map[string]any)["name"].(map[string]any)["required"] = false
			case "title":
				asset.Name = "changed"
				asset.Menu.Title = "changed"
			case "duplicate":
				assets.Objects = append(assets.Objects, *asset)
			}
			if err := validateRuntimeBusinessAssets(assets); err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
}
func TestRuntimeManifestRequiresExactRegularManifest(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(manifest, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := canonicalRuntimeManifestPath(manifest)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := filepath.EvalSymlinks(manifest)
	if err != nil || got != expected {
		t.Fatalf("canonical path: %s %s %v", got, expected, err)
	}
	license := filepath.Join(root, "LICENSE")
	if err := os.WriteFile(license, []byte("MIT"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalRuntimeManifestPath(license); err == nil {
		t.Fatal("bound runtime identity to LICENSE")
	}
	linked := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.Symlink(manifest, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalRuntimeManifestPath(linked); err == nil {
		t.Fatal("accepted manifest symlink")
	}
}
func guardRuntimeConfig(t *testing.T, path string) *config.Config {
	t.Helper()
	canonical, err := canonicalRuntimeSQLitePath(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.DB.Driver = "sqlite"
	cfg.DB.DSN = canonical
	return cfg
}
func guardRuntimeJournal(cfg *config.Config) bootstrapJournal {
	return bootstrapJournal{ReleaseID: "guard-release", ManifestHash: strings.Repeat("a", 64), SourceHash: strings.Repeat("b", 64), DatabasePath: cfg.DB.DSN, State: "started"}
}
func openGuardRuntimeDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func closeGuardRuntimeDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	raw, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestRuntimeBootstrapJournalBindsDatabaseAndPreservesForeignData(t *testing.T) {
	root := t.TempDir()
	first := guardRuntimeConfig(t, filepath.Join(root, "first.db"))
	journalPath := filepath.Join(root, "config.bootstrap.json")
	if complete, err := prepareRuntimeBootstrapDatabase(first, journalPath, guardRuntimeJournal(first)); err != nil || complete {
		t.Fatalf("fresh marker: %v %v", complete, err)
	}
	db := openGuardRuntimeDB(t, first.DB.DSN)
	var marker model.RuntimeInstallation
	if err := db.First(&marker).Error; err != nil || marker.Complete {
		t.Fatalf("incomplete marker missing: %+v %v", marker, err)
	}
	if err := db.Exec("CREATE TABLE own_partial_data (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	closeGuardRuntimeDB(t, db)
	if complete, err := prepareRuntimeBootstrapDatabase(first, journalPath, guardRuntimeJournal(first)); err != nil || complete {
		t.Fatalf("resume own installation: %v %v", complete, err)
	}
	other := guardRuntimeConfig(t, filepath.Join(root, "other.db"))
	db = openGuardRuntimeDB(t, other.DB.DSN)
	if err := db.Exec("CREATE TABLE customer_records (id INTEGER PRIMARY KEY,value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO customer_records VALUES(1,'customer-data')").Error; err != nil {
		t.Fatal(err)
	}
	closeGuardRuntimeDB(t, db)
	before, err := runner.FileHash(other.DB.DSN)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareRuntimeBootstrapDatabase(other, journalPath, guardRuntimeJournal(other)); err == nil {
		t.Fatal("reused started journal after DSN changed")
	}
	after, err := runner.FileHash(other.DB.DSN)
	if err != nil || before != after {
		t.Fatal("foreign database file was modified")
	}
	db = openGuardRuntimeDB(t, other.DB.DSN)
	defer closeGuardRuntimeDB(t, db)
	if db.Migrator().HasTable(&model.RuntimeInstallation{}) {
		t.Fatal("foreign database received marker table")
	}
	var value string
	if err := db.Raw("SELECT value FROM customer_records WHERE id=1").Scan(&value).Error; err != nil || value != "customer-data" {
		t.Fatalf("foreign data changed: %s %v", value, err)
	}
}
func TestRuntimeBootstrapResumeRequiresOwnDatabaseMarker(t *testing.T) {
	root := t.TempDir()
	cfg := guardRuntimeConfig(t, filepath.Join(root, "existing.db"))
	journalPath := filepath.Join(root, "config.bootstrap.json")
	db := openGuardRuntimeDB(t, cfg.DB.DSN)
	if err := db.Exec("CREATE TABLE customer_records(id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	closeGuardRuntimeDB(t, db)
	if err := saveBootstrapJournal(journalPath, guardRuntimeJournal(cfg)); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareRuntimeBootstrapDatabase(cfg, journalPath, guardRuntimeJournal(cfg)); err == nil {
		t.Fatal("started journal adopted a foreign nonempty database")
	}
	db = openGuardRuntimeDB(t, cfg.DB.DSN)
	defer closeGuardRuntimeDB(t, db)
	if db.Migrator().HasTable(&model.RuntimeInstallation{}) {
		t.Fatal("foreign database modified")
	}
}
func TestRuntimeSQLiteRejectsSymlinkAndURI(t *testing.T) {
	for _, dsn := range []string{"", ":memory:", "file:runtime.db", "runtime.db?mode=ro"} {
		if _, err := canonicalRuntimeSQLitePath(dsn); err == nil {
			t.Fatalf("accepted non-file DSN %q", dsn)
		}
	}
	root := t.TempDir()
	path := filepath.Join(root, "real.db")
	if err := os.WriteFile(path, []byte("do not open"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias.db")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalRuntimeSQLitePath(alias); err == nil {
		t.Fatal("accepted database symlink")
	}
}
