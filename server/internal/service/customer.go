package service

import (
	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func ListCustomers(page, pageSize int, name string, age *int) ([]model.Customer, int64, error) {
	var list []model.Customer
	var total int64
	q := global.DB.Model(&model.Customer{})
	if name != "" {
		q = q.Where("name LIKE ?", "%"+name+"%")
	}
	if age != nil {
		q = q.Where("age = ?", *age)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("id asc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func CreateCustomer(m *model.Customer) error {
	return global.DB.Create(m).Error
}

func UpdateCustomer(id uint, m *model.Customer) error {
	var old model.Customer
	if err := global.DB.First(&old, id).Error; err != nil {
		return err
	}
	m.ID = id
	m.CreatedAt = old.CreatedAt
	return global.DB.Save(m).Error
}

func DeleteCustomer(id uint) error {
	return global.DB.Delete(&model.Customer{}, id).Error
}
