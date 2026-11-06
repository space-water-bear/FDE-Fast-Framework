package model

import "time"

// ActiveToken 活跃访问令牌(多设备登录管理)
type ActiveToken struct {
	Base
	UserID    uint      `json:"userId" gorm:"index"`
	Username  string    `json:"username" gorm:"size:64"`
	TokenHash string    `json:"-" gorm:"size:64;uniqueIndex"`
	ExpiresAt time.Time `json:"expiresAt"`
	IP        string    `json:"ip" gorm:"size:64"`
	UserAgent string    `json:"userAgent" gorm:"size:255"`
}

func (ActiveToken) TableName() string { return "sys_active_tokens" }
