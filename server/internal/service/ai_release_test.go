package service

import (
	"errors"
	"testing"

	"gorm.io/gorm"

	"fde-admin/internal/config"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func TestReleaseDeletesAiAgentWithoutRobotTables(t *testing.T) {
	setupTestDB(t)
	previous := global.Config
	global.Config = &config.Config{App: config.App{Mode: "release"}}
	t.Cleanup(func() { global.Config = previous })
	if err := global.DB.Migrator().DropTable(&model.AiBot{}); err != nil {
		t.Fatal(err)
	}
	agent := model.AiAgent{Name: "runtime fixture", Enabled: true}
	if err := global.DB.Create(&agent).Error; err != nil {
		t.Fatal(err)
	}
	if err := DeleteAiAgent(agent.ID); err != nil {
		t.Fatalf("delete agent in release schema: %v", err)
	}
	if err := global.DB.First(&model.AiAgent{}, agent.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("agent remained after delete: %v", err)
	}
}
