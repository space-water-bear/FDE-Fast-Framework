package model

// User 用户
type User struct {
	Base
	Username string `json:"username" gorm:"size:64;uniqueIndex"`
	Password string `json:"-" gorm:"size:128"`
	Nickname string `json:"nickname" gorm:"size:64"`
	Avatar   string `json:"avatar" gorm:"size:255"`
	Enabled  bool   `json:"enabled" gorm:"default:true"`
	Roles    []Role `json:"roles" gorm:"many2many:user_roles;"`
}

func (User) TableName() string { return "sys_users" }
