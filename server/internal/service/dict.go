package service

import (
	"errors"

	"gorm.io/gorm"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func ListDictTypes() ([]model.DictType, error) {
	var list []model.DictType
	if err := global.DB.Order("id asc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func CreateDictType(name, code, remark string) error {
	var exists int64
	if err := global.DB.Model(&model.DictType{}).Where("code = ?", code).Count(&exists).Error; err != nil {
		return err
	}
	if exists > 0 {
		return errors.New("字典编码已存在")
	}
	return global.DB.Create(&model.DictType{Name: name, Code: code, Remark: remark}).Error
}

func UpdateDictType(id uint, name, code, remark string) error {
	var t model.DictType
	if err := global.DB.First(&t, id).Error; err != nil {
		return err
	}
	if t.Code != code {
		var exists int64
		if err := global.DB.Model(&model.DictType{}).Where("code = ? AND id <> ?", code, id).Count(&exists).Error; err != nil {
			return err
		}
		if exists > 0 {
			return errors.New("字典编码已存在")
		}
	}
	t.Name = name
	t.Code = code
	t.Remark = remark
	return global.DB.Save(&t).Error
}

// DeleteDictType 删除类型并级联删除其数据
func DeleteDictType(id uint) error {
	return global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("type_id = ?", id).Delete(&model.DictData{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.DictType{}, id).Error
	})
}

func ListDictData(typeID uint) ([]model.DictData, error) {
	var list []model.DictData
	if err := global.DB.Where("type_id = ?", typeID).Order("sort asc, id asc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func CreateDictData(typeID uint, label, value string, sort int, enabled bool, remark string) error {
	var t model.DictType
	if err := global.DB.First(&t, typeID).Error; err != nil {
		return errors.New("字典类型不存在")
	}
	return global.DB.Create(&model.DictData{TypeID: typeID, Label: label, Value: value, Sort: sort, Enabled: enabled, Remark: remark}).Error
}

func UpdateDictData(id uint, label, value string, sort int, enabled bool, remark string) error {
	var d model.DictData
	if err := global.DB.First(&d, id).Error; err != nil {
		return err
	}
	d.Label = label
	d.Value = value
	d.Sort = sort
	d.Enabled = enabled
	d.Remark = remark
	return global.DB.Save(&d).Error
}

func DeleteDictData(id uint) error {
	return global.DB.Delete(&model.DictData{}, id).Error
}
