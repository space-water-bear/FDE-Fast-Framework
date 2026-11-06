package service

import (
	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func CreateOperationLog(log *model.OperationLog) {
	_ = global.DB.Create(log).Error
}

func ListOperationLogs(page, pageSize int, keyword string) ([]model.OperationLog, int64, error) {
	var list []model.OperationLog
	var total int64
	q := global.DB.Model(&model.OperationLog{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("username LIKE ? OR path LIKE ?", like, like)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func DeleteOperationLog(id uint) error {
	return global.DB.Delete(&model.OperationLog{}, id).Error
}

func ClearOperationLogs() error {
	return global.DB.Where("1 = 1").Delete(&model.OperationLog{}).Error
}
