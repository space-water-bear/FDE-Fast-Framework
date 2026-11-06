package service

import (
	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func ListProducts(page, pageSize int, name string, stock *int) ([]model.Product, int64, error) {
	var list []model.Product
	var total int64
	q := global.DB.Model(&model.Product{})
	if name != "" {
		q = q.Where("name LIKE ?", "%"+name+"%")
	}
	if stock != nil {
		q = q.Where("stock = ?", *stock)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("id asc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func CreateProduct(m *model.Product) error {
	return global.DB.Create(m).Error
}

func UpdateProduct(id uint, m *model.Product) error {
	var old model.Product
	if err := global.DB.First(&old, id).Error; err != nil {
		return err
	}
	m.ID = id
	m.CreatedAt = old.CreatedAt
	return global.DB.Save(m).Error
}

func DeleteProduct(id uint) error {
	return global.DB.Delete(&model.Product{}, id).Error
}
