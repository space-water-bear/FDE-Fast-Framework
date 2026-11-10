package initialize

import (
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/service"

	"gorm.io/gorm"
)

// ensureAiBotMenu also ensures its parent because startup initializers run from
// a map, whose iteration order is unspecified.
func ensureAiBotMenu() error {
	if err := ensureAiMenus(); err != nil {
		return err
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		var parent model.Menu
		if err := tx.Where("path = ? AND name = ?", "ai", "Ai").First(&parent).Error; err != nil {
			return err
		}
		menu := model.Menu{ParentID: parent.ID, Path: "bot", Name: "AiBot", Component: "system/ai/bot/index", Title: "机器人", Icon: "ChatLineRound", Type: 1, Sort: 8}
		if err := tx.Where("name = ?", "AiBot").FirstOrCreate(&menu).Error; err != nil {
			return err
		}
		var admin model.Role
		if err := tx.Where("code = ?", "admin").First(&admin).Error; err != nil {
			return err
		}
		if err := tx.Model(&admin).Association("Menus").Append(&menu); err != nil {
			return err
		}
		for _, def := range []struct{ title, path, method string }{
			{"查询", "/api/ai/bot/list", "GET"}, {"新增", "/api/ai/bot", "POST"},
			{"编辑", "/api/ai/bot/:id", "PUT"}, {"删除", "/api/ai/bot/:id", "DELETE"},
		} {
			button := model.Menu{ParentID: menu.ID, Title: "机器人-" + def.title, Type: 2, Hidden: true, ApiPath: def.path, ApiMethod: def.method}
			if err := tx.Where("parent_id = ? AND api_path = ? AND api_method = ?", menu.ID, def.path, def.method).FirstOrCreate(&button).Error; err != nil {
				return err
			}
			if err := tx.Model(&admin).Association("Menus").Append(&button); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return service.RebuildCasbin()
}
