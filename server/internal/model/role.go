package model

// Role 角色
type Role struct {
	Base
	Name        string `json:"name" gorm:"size:64"`
	Code        string `json:"code" gorm:"size:64;uniqueIndex"`
	Description string `json:"description" gorm:"size:255"`
	Enabled     bool   `json:"enabled" gorm:"default:true"`
	Menus       []Menu `json:"menus" gorm:"many2many:role_menus;"`
}

func (Role) TableName() string { return "sys_roles" }
