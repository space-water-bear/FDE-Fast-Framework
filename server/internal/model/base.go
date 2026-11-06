package model

import "time"

// Base 统一基础字段: 主键与时间戳(硬删除, 不使用软删除)
type Base struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
