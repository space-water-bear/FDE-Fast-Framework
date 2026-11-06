package initialize

import (
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
