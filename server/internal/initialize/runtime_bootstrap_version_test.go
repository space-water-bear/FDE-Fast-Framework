package initialize

import (
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/service"
)

func TestRuntimeBootstrapPreservesFrozenV2ActiveSchema(t *testing.T) {
	oldDB, oldConfig, oldEnforcer, oldLogger := global.DB, global.Config, global.Enforcer, global.Logger
	t.Cleanup(func() {
		if global.DB != nil && global.DB != oldDB {
			closeUpgradeDB(global.DB)
		}
		global.DB, global.Config, global.Enforcer, global.Logger = oldDB, oldConfig, oldEnforcer, oldLogger
	})
	assets := upgradeFixtureAssets(t, true)
	pkg := upgradeFixturePackage(t, assets)
	root := t.TempDir()
	cfg := guardRuntimeConfig(t, filepath.Join(root, "runtime.db"))
	cfg.App.Mode = "release"
	cfg.JWT.Secret = strings.Repeat("v", 48)
	cfg.Seed.AdminUsername = "bootstrap-version-qa"
	cfg.Seed.AdminPassword = strings.Repeat("q", 32)
	cfg.Zap.Director = filepath.Join(root, "logs")
	cfg.Captcha.ExpireMinutes = 5
	configPath := filepath.Join(root, "config.yaml")
	manifestPath := filepath.Join(pkg.Path, "manifest.json")
	for attempt := 0; attempt < 2; attempt++ {
		if global.DB != nil && global.DB != oldDB {
			closeUpgradeDB(global.DB)
		}
		if err := BootstrapRuntime(cfg, configPath, filepath.Join(pkg.Path, "assets"), manifestPath); err != nil {
			t.Fatal(err)
		}
		closeUpgradeDB(global.DB)
		if err := VerifyRuntime(cfg, manifestPath); err != nil {
			t.Fatal(err)
		}
		var object model.BizObject
		if err := global.DB.Where("key = ?", upgradeFixtureKey).First(&object).Error; err != nil {
			t.Fatal(err)
		}
		if object.Version != 2 || object.ActiveVersion != 2 {
			t.Fatalf("frozen v2 became draft=%d active=%d", object.Version, object.ActiveVersion)
		}
		var forms []model.FormSchema
		if err := global.DB.Where("object_id = ?", object.ID).Find(&forms).Error; err != nil {
			t.Fatal(err)
		}
		if len(forms) != 1 || forms[0].Version != 2 || !forms[0].Published {
			t.Fatalf("unexpected schema history: %+v", forms)
		}
		schema, err := service.GetFormSchemaByKeyInProject(model.DefaultProjectID, upgradeFixtureKey)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(schema)
		var response struct {
			Version       int `json:"version"`
			ActiveVersion int `json:"activeVersion"`
		}
		if err := json.Unmarshal(encoded, &response); err != nil || response.Version != 2 || response.ActiveVersion != 2 {
			t.Fatalf("active schema response: %s (%v)", encoded, err)
		}
	}
}

func TestRuntimeSchemaVersionDefaultsLegacyAndRejectsInvalidValues(t *testing.T) {
	if version, err := runtimeSchemaVersion(map[string]any{}); err != nil || version != 1 {
		t.Fatalf("legacy version: %d %v", version, err)
	}
	for _, value := range []any{float64(1), float64(2), 2, json.Number("3")} {
		if _, err := runtimeSchemaVersion(map[string]any{"version": value}); err != nil {
			t.Fatalf("valid version %v: %v", value, err)
		}
	}
	for _, value := range []any{nil, float64(0), float64(-1), 1.5, "2", true, math.Inf(1), math.NaN(), float64(math.MaxInt32) + 1} {
		if _, err := runtimeSchemaVersion(map[string]any{"version": value}); err == nil {
			t.Fatalf("invalid schema version accepted: %v", value)
		}
	}
}
