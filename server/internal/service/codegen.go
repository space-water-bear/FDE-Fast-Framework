package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"fde-admin/internal/codegen"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

// CodegenOptions 生成器选项: 字段类型 + 可选的父菜单(目录树)
func CodegenOptions() ([]map[string]string, []model.Menu, error) {
	types := []map[string]string{
		{"value": "string", "label": "字符串(string)"},
		{"value": "text", "label": "长文本(text)"},
		{"value": "int", "label": "整数(int)"},
		{"value": "float", "label": "浮点数(float)"},
		{"value": "bool", "label": "布尔(bool)"},
		{"value": "datetime", "label": "时间(datetime)"},
	}
	menus, err := ListMenuTree()
	if err != nil {
		return nil, nil, err
	}
	return types, filterDirTree(menus), nil
}

func filterDirTree(menus []model.Menu) []model.Menu {
	out := []model.Menu{}
	for _, m := range menus {
		if m.Type == 2 {
			continue
		}
		m.Children = filterDirTree(m.Children)
		if m.Type == 0 || len(m.Children) > 0 {
			out = append(out, m)
		}
	}
	return out
}

// CodegenPreview 渲染模板但不写盘
func CodegenPreview(req codegen.GenRequest) ([]codegen.FileResult, error) {
	d, err := codegen.Prepare(req)
	if err != nil {
		return nil, err
	}
	return codegen.Render(d)
}

// CodegenGenerate 写盘 + 创建菜单/按钮 + 分配角色 + 重建 casbin
func CodegenGenerate(req codegen.GenRequest) ([]codegen.FileResult, uint, error) {
	d, err := codegen.Prepare(req)
	if err != nil {
		return nil, 0, err
	}
	if global.DB.Migrator().HasTable(req.TableName) {
		return nil, 0, errors.New("数据表 " + req.TableName + " 已存在，请更换表名")
	}
	var menuCount int64
	if err := global.DB.Model(&model.Menu{}).Where("path = ? AND name = ?", d.Snake, d.StructName).Count(&menuCount).Error; err != nil {
		return nil, 0, err
	}
	if menuCount > 0 {
		return nil, 0, errors.New("同名菜单已存在: " + d.Snake)
	}

	files, err := codegen.Render(d)
	if err != nil {
		return nil, 0, err
	}
	if err := writeGenFiles(files); err != nil {
		return nil, 0, err
	}
	menuID, err := createGeneratedMenu(d)
	if err != nil {
		return nil, 0, err
	}
	if err := RebuildCasbin(); err != nil {
		return nil, 0, err
	}
	return files, menuID, nil
}

func writeGenFiles(files []codegen.FileResult) error {
	cfg := global.Config.Codegen
	for _, f := range files {
		var full string
		if strings.HasPrefix(f.Path, "web/") {
			full = filepath.Join(cfg.WebDir, strings.TrimPrefix(f.Path, "web/"))
		} else {
			full = filepath.Join(cfg.ServerDir, f.Path)
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(f.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func createGeneratedMenu(d *codegen.Data) (uint, error) {
	var menuID uint
	err := global.DB.Transaction(func(tx *gorm.DB) error {
		menu := &model.Menu{
			ParentID:  d.ParentMenuID,
			Path:      d.Snake,
			Name:      d.StructName,
			Component: "generated/" + d.Snake + "/index",
			Title:     d.Business,
			Icon:      "Grid",
			Type:      1,
			Sort:      99,
		}
		if err := tx.Create(menu).Error; err != nil {
			return err
		}
		menuID = menu.ID
		btns := []*model.Menu{
			{ParentID: menu.ID, Title: d.Business + "-查询", Type: 2, Hidden: true, ApiPath: "/api/" + d.Snake + "/list", ApiMethod: "GET"},
			{ParentID: menu.ID, Title: d.Business + "-新增", Type: 2, Hidden: true, ApiPath: "/api/" + d.Snake, ApiMethod: "POST"},
			{ParentID: menu.ID, Title: d.Business + "-编辑", Type: 2, Hidden: true, ApiPath: "/api/" + d.Snake + "/:id", ApiMethod: "PUT"},
			{ParentID: menu.ID, Title: d.Business + "-删除", Type: 2, Hidden: true, ApiPath: "/api/" + d.Snake + "/:id", ApiMethod: "DELETE"},
		}
		for _, b := range btns {
			if err := tx.Create(b).Error; err != nil {
				return err
			}
		}

		// 分配角色: admin + 所有拥有父菜单的角色
		var roleIDs []uint
		if d.ParentMenuID > 0 {
			if err := tx.Table("role_menus").Where("menu_id = ?", d.ParentMenuID).Pluck("role_id", &roleIDs).Error; err != nil {
				return err
			}
		}
		var admin model.Role
		if err := tx.Where("code = ?", "admin").First(&admin).Error; err != nil {
			return err
		}
		roleIDs = append(roleIDs, admin.ID)
		uniq := map[uint]bool{}
		var targets []uint
		for _, id := range roleIDs {
			if !uniq[id] {
				uniq[id] = true
				targets = append(targets, id)
			}
		}
		menus := append([]*model.Menu{menu}, btns...)
		for _, rid := range targets {
			var role model.Role
			if err := tx.First(&role, rid).Error; err != nil {
				return err
			}
			if err := tx.Model(&role).Association("Menus").Append(menus); err != nil {
				return err
			}
		}
		return nil
	})
	return menuID, err
}
