package initialize

import (
	"testing"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func TestAiBotMenuCreatesParentAndIsIdempotent(t *testing.T) {
	setupSeedDB(t)
	admin := model.Role{Name: "admin", Code: "admin", Enabled: true}
	if err := global.DB.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := ensureAiBotMenu(); err != nil {
			t.Fatal(err)
		}
	}
	var menus []model.Menu
	if err := global.DB.Where("name = ?", "AiBot").Find(&menus).Error; err != nil {
		t.Fatal(err)
	}
	if len(menus) != 1 || menus[0].ParentID == 0 {
		t.Fatalf("invalid robot menu: %+v", menus)
	}
	var buttons int64
	if err := global.DB.Model(&model.Menu{}).Where("parent_id = ?", menus[0].ID).Count(&buttons).Error; err != nil {
		t.Fatal(err)
	}
	if buttons != 4 {
		t.Fatalf("button count = %d", buttons)
	}
	var assigned []model.Menu
	if err := global.DB.Model(&admin).Association("Menus").Find(&assigned); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, menu := range assigned {
		if menu.ID == menus[0].ID {
			found = true
		}
	}
	if !found {
		t.Fatal("robot menu not assigned to admin")
	}
}
