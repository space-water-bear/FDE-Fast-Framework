package service

import (
	"errors"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func ListMenuTree() ([]model.Menu, error) {
	var menus []model.Menu
	if err := global.DB.Order("sort asc").Find(&menus).Error; err != nil {
		return nil, err
	}
	return BuildMenuTree(menus, 0), nil
}

// BuildMenuTree 将平铺菜单按 ParentID 组装为树
func BuildMenuTree(menus []model.Menu, parentID uint) []model.Menu {
	tree := []model.Menu{}
	for i := range menus {
		if menus[i].ParentID == parentID {
			menus[i].Children = BuildMenuTree(menus, menus[i].ID)
			tree = append(tree, menus[i])
		}
	}
	return tree
}

func CreateMenu(m *model.Menu) error {
	if err := global.DB.Create(m).Error; err != nil {
		return err
	}
	return RebuildCasbin()
}

func UpdateMenu(id uint, m *model.Menu) error {
	var old model.Menu
	if err := global.DB.First(&old, id).Error; err != nil {
		return err
	}
	m.ID = id
	m.CreatedAt = old.CreatedAt
	if err := global.DB.Save(m).Error; err != nil {
		return err
	}
	return RebuildCasbin()
}

func DeleteMenu(id uint) error {
	var childCount int64
	if err := global.DB.Model(&model.Menu{}).Where("parent_id = ?", id).Count(&childCount).Error; err != nil {
		return err
	}
	if childCount > 0 {
		return errors.New("存在子菜单，请先删除子菜单")
	}
	if err := global.DB.Exec("DELETE FROM role_menus WHERE menu_id = ?", id).Error; err != nil {
		return err
	}
	if err := global.DB.Delete(&model.Menu{}, id).Error; err != nil {
		return err
	}
	return RebuildCasbin()
}
