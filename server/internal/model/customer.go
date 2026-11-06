package model

import "time"

// Customer 客户
type Customer struct {
	Base
	Name      string    `json:"name" gorm:"size:128;comment:客户名称"`
	Age       int       `json:"age" gorm:"default:0;comment:年龄"`
	Email     string    `json:"email" gorm:"size:128;comment:邮箱"`
	Enabled   bool      `json:"enabled" gorm:"default:false;comment:是否启用"`
	Remark    string    `json:"remark" gorm:"type:text;comment:备注"`
	OrderedAt time.Time `json:"orderedAt" gorm:"type:datetime;comment:下单时间"`
}

func (Customer) TableName() string { return "biz_customer" }

func init() {
	RegisterTable(&Customer{})
}
