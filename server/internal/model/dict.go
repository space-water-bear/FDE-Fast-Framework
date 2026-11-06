package model

// DictType 字典类型
type DictType struct {
	Base
	Name   string `json:"name" gorm:"size:64"`
	Code   string `json:"code" gorm:"size:64;uniqueIndex"`
	Remark string `json:"remark" gorm:"size:255"`
}

func (DictType) TableName() string { return "sys_dict_types" }

// DictData 字典数据
type DictData struct {
	Base
	TypeID  uint   `json:"typeId" gorm:"index"`
	Label   string `json:"label" gorm:"size:128"`
	Value   string `json:"value" gorm:"size:128"`
	Sort    int    `json:"sort" gorm:"default:0"`
	Enabled bool   `json:"enabled" gorm:"default:true"`
	Remark  string `json:"remark" gorm:"size:255"`
}

func (DictData) TableName() string { return "sys_dict_data" }
