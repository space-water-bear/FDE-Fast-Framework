package model

import "time"

// OperationLog 操作日志
type OperationLog struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"createdAt"`
	UserID    uint      `json:"userId" gorm:"index"`
	Username  string    `json:"username" gorm:"size:64;index"`
	Method    string    `json:"method" gorm:"size:16"`
	Path      string    `json:"path" gorm:"size:255;index"`
	Query     string    `json:"query" gorm:"type:text"`
	Body      string    `json:"body" gorm:"type:text"`
	IP        string    `json:"ip" gorm:"size:64"`
	UserAgent string    `json:"userAgent" gorm:"size:255"`
	Status    int       `json:"status"`
	Code      int       `json:"code"`
	Msg       string    `json:"msg" gorm:"size:255"`
	LatencyMs int64     `json:"latencyMs"`
}

func (OperationLog) TableName() string { return "sys_operation_logs" }
