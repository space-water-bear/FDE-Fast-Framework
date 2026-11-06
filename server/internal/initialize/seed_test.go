package initialize

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func setupSeedDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	global.DB = db
	if err := db.AutoMigrate(&model.AiTool{}, &model.Menu{}, &model.Role{}); err != nil {
		t.Fatalf("migrate seed: %v", err)
	}
}

func TestEnsureCodegenAiToolsIdempotent(t *testing.T) {
	setupSeedDB(t)
	for i := 0; i < 2; i++ {
		if err := ensureCodegenAiTools(); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
	}
	var tools []model.AiTool
	if err := global.DB.Where("name IN ?", []string{"generate_business_code", "apply_files"}).Order("name asc").Find(&tools).Error; err != nil {
		t.Fatalf("query tools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools count = %d, want 2", len(tools))
	}
	for _, tool := range tools {
		if !tool.Enabled || tool.Schema == "" {
			t.Fatalf("tool %s invalid: enabled=%v schemaLen=%d", tool.Name, tool.Enabled, len(tool.Schema))
		}
	}
}

func TestEnsureFormgenApplyButtonIdempotent(t *testing.T) {
	setupSeedDB(t)
	formgen := &model.Menu{Path: "formgen", Name: "Formgen", Component: "system/formgen/index", Title: "表单生成", Type: 1, Sort: 10}
	if err := global.DB.Create(formgen).Error; err != nil {
		t.Fatalf("create formgen menu: %v", err)
	}
	admin := &model.Role{Name: "超级管理员", Code: "admin", Enabled: true}
	if err := global.DB.Create(admin).Error; err != nil {
		t.Fatalf("create admin role: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := ensureFormgenApplyButton(); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
	}
	var n int64
	if err := global.DB.Model(&model.Menu{}).Where("parent_id = ? AND api_path = ?", formgen.ID, "/api/formgen/apply").Count(&n).Error; err != nil {
		t.Fatalf("count apply button: %v", err)
	}
	if n != 1 {
		t.Fatalf("apply button count = %d, want 1", n)
	}
}

func TestBuiltinToolProtocolUpgradePreservesUserSettings(t *testing.T) {
	setupSeedDB(t)
	for _, item := range []model.AiTool{
		{Name: "generate_form_schema", Description: "old form contract", Schema: `{"type":"object"}`, Remark: "表单生成器 P1"},
		{Name: "generate_business_code", Description: "old code contract", Schema: `{"type":"object"}`, Remark: "代码生成器 P3"},
		{Name: "apply_files", Description: "old apply contract", Schema: `{"type":"object"}`, Remark: "代码生成器 P3"},
		{Name: "custom_http", Description: "custom description", Schema: `{"custom":true}`, Remark: "user tool"},
	} {
		if err := global.DB.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := global.DB.Model(&model.AiTool{}).Where("1=1").Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := ensureFormgenAiTool(); err != nil {
			t.Fatal(err)
		}
		if err := ensureCodegenAiTools(); err != nil {
			t.Fatal(err)
		}
	}
	var items []model.AiTool
	if err := global.DB.Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("tool count changed: %d", len(items))
	}
	for _, item := range items {
		if item.Enabled {
			t.Fatalf("upgrade enabled disabled tool %s", item.Name)
		}
		if item.Name == "custom_http" {
			if item.Schema != `{"custom":true}` || item.Description != "custom description" || item.Remark != "user tool" {
				t.Fatal("custom tool modified")
			}
			continue
		}
		if item.Schema == `{"type":"object"}` {
			t.Fatalf("legacy %s contract not upgraded", item.Name)
		}
		var schema map[string]any
		if err := json.Unmarshal([]byte(item.Schema), &schema); err != nil {
			t.Fatal(err)
		}
		props := schema["properties"].(map[string]any)
		if item.Name == "generate_form_schema" && (props["schema"] == nil || props["expectedVersion"] == nil || props["linkages"] == nil) {
			t.Fatal("form contract missing incremental fields")
		}
		if item.Name == "generate_business_code" && props["dryRun"].(map[string]any)["const"] != true {
			t.Fatal("code tool advertises direct writes")
		}
		if item.Name == "apply_files" && !strings.Contains(item.Description, "APPROVAL_REQUIRED") {
			t.Fatal("apply tool still advertises model approval")
		}
	}
}

func TestBuiltinUpgradeDoesNotOverwriteCustomReservedName(t *testing.T) {
	setupSeedDB(t)
	custom := model.AiTool{Name: "generate_form_schema", Description: "my custom tool", Schema: `{"custom":true}`, Remark: "user-defined"}
	if err := global.DB.Create(&custom).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Model(&custom).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureFormgenAiTool(); err != nil {
		t.Fatal(err)
	}
	var tools []model.AiTool
	if err := global.DB.Where("name = ?", custom.Name).Find(&tools).Error; err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Schema != custom.Schema || tools[0].Description != custom.Description || tools[0].Enabled {
		t.Fatal("custom reserved-name tool was changed")
	}
}
