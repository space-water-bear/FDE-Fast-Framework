package initialize

import (
	"errors"
	"fmt"

	"fde-admin/internal/model"
	"gorm.io/gorm"
)

// migrateFormgenProjectNamespace runs after AutoMigrate. Build the replacement
// composite index before dropping the old single-column global-key index.
func migrateFormgenProjectNamespace(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, m := range []any{&model.BizObject{}, &model.GenAudit{}} {
			if err := tx.Model(m).Where("project_id IS NULL OR project_id = 0").Update("project_id", model.DefaultProjectID).Error; err != nil {
				return err
			}
		}
		if !tx.Migrator().HasIndex(&model.BizObject{}, "idx_biz_object_project_key") {
			if err := tx.Migrator().CreateIndex(&model.BizObject{}, "idx_biz_object_project_key"); err != nil {
				return err
			}
		}
		indexes, err := tx.Migrator().GetIndexes(&model.BizObject{})
		if err != nil {
			return fmt.Errorf("读取业务对象索引: %w", err)
		}
		for _, index := range indexes {
			unique, known := index.Unique()
			cols := index.Columns()
			if known && unique && len(cols) == 1 && cols[0] == "key" {
				if err := tx.Migrator().DropIndex(&model.BizObject{}, index.Name()); err != nil {
					return fmt.Errorf("迁移旧全局对象键索引 %s: %w", index.Name(), err)
				}
			}
		}
		// Existing installations published every save. Keep their active schema;
		// newly saved drafts have no published history and remain inactive.
		var objects []model.BizObject
		if err := tx.Where("active_version = 0").Find(&objects).Error; err != nil {
			return err
		}
		for _, obj := range objects {
			var published model.FormSchema
			err := tx.Where("object_id = ? AND published = ?", obj.ID, true).Order("version desc").First(&published).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if err := tx.Model(&obj).Update("active_version", published.Version).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.FormSchema{}).Where("object_id = ? AND version <> ?", obj.ID, published.Version).Update("published", false).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
