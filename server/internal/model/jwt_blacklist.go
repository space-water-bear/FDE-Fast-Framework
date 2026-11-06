package model

import "time"

// JwtBlacklist 已吊销的访问令牌黑名单
type JwtBlacklist struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"createdAt"`
	UserID    uint      `json:"userId" gorm:"index"`
	Username  string    `json:"username" gorm:"size:64"`
	TokenHash string    `json:"-" gorm:"size:64;uniqueIndex"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (JwtBlacklist) TableName() string { return "sys_jwt_blacklists" }
