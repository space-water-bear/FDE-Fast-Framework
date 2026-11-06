package service

import (
	"errors"

	"gorm.io/gorm"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/utils"
)

func GetUser(id uint) (*model.User, error) {
	var user model.User
	if err := global.DB.Preload("Roles").First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func ListUsers(page, pageSize int, keyword string) ([]model.User, int64, error) {
	var users []model.User
	var total int64
	q := global.DB.Model(&model.User{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("username LIKE ? OR nickname LIKE ?", like, like)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Preload("Roles").Order("id asc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&users).Error; err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

func CreateUser(username, password, nickname string, enabled bool, roleIDs []uint) error {
	var exists int64
	if err := global.DB.Model(&model.User{}).Where("username = ?", username).Count(&exists).Error; err != nil {
		return err
	}
	if exists > 0 {
		return errors.New("用户名已存在")
	}
	hash, err := utils.HashPassword(password)
	if err != nil {
		return err
	}
	user := &model.User{Username: username, Password: hash, Nickname: nickname, Enabled: enabled}
	roles, err := findRoles(roleIDs)
	if err != nil {
		return err
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		return tx.Model(user).Association("Roles").Replace(roles)
	}); err != nil {
		return err
	}
	return RebuildCasbin()
}

func UpdateUser(id uint, nickname, password string, enabled *bool, roleIDs []uint) error {
	var user model.User
	if err := global.DB.First(&user, id).Error; err != nil {
		return err
	}
	user.Nickname = nickname
	if enabled != nil {
		user.Enabled = *enabled
	}
	if password != "" {
		hash, err := utils.HashPassword(password)
		if err != nil {
			return err
		}
		user.Password = hash
	}
	roles, err := findRoles(roleIDs)
	if err != nil {
		return err
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&user).Error; err != nil {
			return err
		}
		return tx.Model(&user).Association("Roles").Replace(roles)
	}); err != nil {
		return err
	}
	return RebuildCasbin()
}

func DeleteUser(id uint) error {
	var user model.User
	if err := global.DB.First(&user, id).Error; err != nil {
		return err
	}
	if user.Username == "admin" {
		return errors.New("系统内置管理员不可删除")
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&user).Association("Roles").Clear(); err != nil {
			return err
		}
		return tx.Delete(&user).Error
	}); err != nil {
		return err
	}
	return RebuildCasbin()
}

func findRoles(ids []uint) ([]model.Role, error) {
	roles := []model.Role{}
	if len(ids) == 0 {
		return roles, nil
	}
	if err := global.DB.Find(&roles, ids).Error; err != nil {
		return nil, err
	}
	return roles, nil
}
