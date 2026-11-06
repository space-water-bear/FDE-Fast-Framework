package service

import (
	"errors"

	"gorm.io/gorm"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func ListRoles() ([]model.Role, error) {
	var roles []model.Role
	if err := global.DB.Preload("Menus").Order("id asc").Find(&roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

func GetRole(id uint) (*model.Role, []uint, error) {
	var role model.Role
	if err := global.DB.Preload("Menus").First(&role, id).Error; err != nil {
		return nil, nil, err
	}
	ids := make([]uint, 0, len(role.Menus))
	for _, m := range role.Menus {
		ids = append(ids, m.ID)
	}
	return &role, ids, nil
}

func CreateRole(name, code, description string, enabled bool, menuIDs []uint) error {
	var exists int64
	if err := global.DB.Model(&model.Role{}).Where("code = ?", code).Count(&exists).Error; err != nil {
		return err
	}
	if exists > 0 {
		return errors.New("角色编码已存在")
	}
	role := &model.Role{Name: name, Code: code, Description: description, Enabled: enabled}
	menus, err := findMenus(menuIDs)
	if err != nil {
		return err
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(role).Error; err != nil {
			return err
		}
		return tx.Model(role).Association("Menus").Replace(menus)
	}); err != nil {
		return err
	}
	return RebuildCasbin()
}

func UpdateRole(id uint, name, description string, enabled *bool, menuIDs []uint) error {
	var role model.Role
	if err := global.DB.First(&role, id).Error; err != nil {
		return err
	}
	role.Name = name
	role.Description = description
	if enabled != nil {
		role.Enabled = *enabled
	}
	menus, err := findMenus(menuIDs)
	if err != nil {
		return err
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&role).Error; err != nil {
			return err
		}
		return tx.Model(&role).Association("Menus").Replace(menus)
	}); err != nil {
		return err
	}
	return RebuildCasbin()
}

func DeleteRole(id uint) error {
	var role model.Role
	if err := global.DB.First(&role, id).Error; err != nil {
		return err
	}
	if role.Code == "admin" {
		return errors.New("系统内置超级管理员角色不可删除")
	}
	var userCount int64
	if err := global.DB.Table("user_roles").Where("role_id = ?", id).Count(&userCount).Error; err != nil {
		return err
	}
	if userCount > 0 {
		return errors.New("该角色仍被用户使用，请先解除绑定")
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&role).Association("Menus").Clear(); err != nil {
			return err
		}
		return tx.Delete(&role).Error
	}); err != nil {
		return err
	}
	return RebuildCasbin()
}

func findMenus(ids []uint) ([]model.Menu, error) {
	menus := []model.Menu{}
	if len(ids) == 0 {
		return menus, nil
	}
	if err := global.DB.Find(&menus, ids).Error; err != nil {
		return nil, err
	}
	return menus, nil
}
