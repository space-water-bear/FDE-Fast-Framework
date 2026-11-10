package initialize

import (
	"testing"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func TestEnsureChartDemoMenuIdempotentPreservesCustomization(t *testing.T) {
	setupSeedDB(t)
	system := model.Menu{Path: "system", Name: "System", Title: "系统管理", Type: 0}
	admin := model.Role{Name: "admin", Code: "admin", Enabled: true}
	if err := global.DB.Create(&system).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureChartDemoMenu(); err != nil {
		t.Fatal(err)
	}
	var menu model.Menu
	if err := global.DB.Where("name = ?", "SystemChartDemo").First(&menu).Error; err != nil {
		t.Fatal(err)
	}
	if menu.ParentID != system.ID || menu.Component != "demo/charts/index" {
		t.Fatalf("incorrect menu registration: %+v", menu)
	}
	if err := global.DB.Model(&menu).Update("title", "我的统计示例").Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureChartDemoMenu(); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := global.DB.Model(&model.Menu{}).Where("name = ?", "SystemChartDemo").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("menu duplicated: count=%d err=%v", count, err)
	}
	if err := global.DB.First(&menu, menu.ID).Error; err != nil || menu.Title != "我的统计示例" {
		t.Fatalf("custom menu was overwritten: %+v err=%v", menu, err)
	}
	var buttons []model.Menu
	if err := global.DB.Where("parent_id = ? AND type = ?", menu.ID, 2).Find(&buttons).Error; err != nil {
		t.Fatal(err)
	}
	if len(buttons) != 1 || buttons[0].ApiPath != "/api/demo/charts/sales" || buttons[0].ApiMethod != "GET" {
		t.Fatalf("incorrect API permission button: %+v", buttons)
	}
	var assigned []model.Menu
	if err := global.DB.Model(&admin).Association("Menus").Find(&assigned); err != nil {
		t.Fatal(err)
	}
	if len(assigned) != 2 {
		t.Fatalf("expected menu and button assigned once: %+v", assigned)
	}
}

func TestEnsureChartBuilderSkillIdempotentPreservesCustomization(t *testing.T) {
	setupSeedDB(t)
	if err := global.DB.AutoMigrate(&model.AiSkill{}); err != nil {
		t.Fatal(err)
	}
	if err := ensureChartBuilderSkill(); err != nil {
		t.Fatal(err)
	}
	var skill model.AiSkill
	if err := global.DB.Where("name = ?", "fde-chart-builder").First(&skill).Error; err != nil {
		t.Fatal(err)
	}
	if skill.Prompt == "" || skill.Description == "" {
		t.Fatal("installed skill is incomplete")
	}
	if err := global.DB.Model(&skill).Updates(map[string]any{"prompt": "客户自定义提示词", "description": "客户说明", "remark": "客户备注"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureChartBuilderSkill(); err != nil {
		t.Fatal(err)
	}
	var skills []model.AiSkill
	if err := global.DB.Where("name = ?", "fde-chart-builder").Find(&skills).Error; err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].ID != skill.ID || skills[0].Prompt != "客户自定义提示词" || skills[0].Description != "客户说明" || skills[0].Remark != "客户备注" {
		t.Fatalf("custom skill overwritten or duplicated: %+v", skills)
	}
}
