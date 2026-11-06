package model

import "time"

// Product 产品
type Product struct {
	Base
	Name       string    `json:"name" gorm:"size:128;comment:产品名称"`
	Price      float64   `json:"price" gorm:"default:0;comment:价格"`
	Stock      int       `json:"stock" gorm:"default:0;comment:库存"`
	OnSale     bool      `json:"onSale" gorm:"default:false;comment:是否上架"`
	ReleasedAt time.Time `json:"releasedAt" gorm:"type:datetime;comment:上架时间"`
	Remark     string    `json:"remark" gorm:"type:text;comment:备注"`
}

func (Product) TableName() string { return "biz_product" }

func init() {
	RegisterTable(&Product{})
}
