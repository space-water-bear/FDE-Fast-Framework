package model

import "time"

// RefreshToken 刷新令牌(库中只存 SHA256 哈希)
type RefreshToken struct {
	Base
	UserID    uint      `json:"userId" gorm:"index"`
	TokenHash string    `json:"-" gorm:"size:64;uniqueIndex"`
	ExpiresAt time.Time `json:"expiresAt"`
	Revoked   bool      `json:"revoked" gorm:"default:false"`
}

func (RefreshToken) TableName() string { return "sys_refresh_tokens" }
