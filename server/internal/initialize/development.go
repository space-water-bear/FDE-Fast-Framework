package initialize

import (
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"gorm.io/gorm/clause"
)

func ensureDevelopmentDefaults() error {
	project := model.DevProject{Base: model.Base{ID: model.DefaultProjectID}, Key: "default", Name: "默认项目（原生成器）"}
	return global.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&project).Error
}

func ensureDevelopmentMenus() error {
	items := []model.Menu{
		{Path: "dev_projects_list", Name: "DevProjectsList", Title: "开发项目查询", Type: 2, Hidden: true, ApiPath: "/api/dev/projects", ApiMethod: "GET"},
		{Path: "dev_projects_create", Name: "DevProjectsCreate", Title: "创建客户项目", Type: 2, Hidden: true, ApiPath: "/api/dev/projects", ApiMethod: "POST"},
		{Path: "dev_runs_create", Name: "DevRunsCreate", Title: "保存开发任务", Type: 2, Hidden: true, ApiPath: "/api/dev/runs", ApiMethod: "POST"},
		{Path: "dev_runs_read", Name: "DevRunsRead", Title: "开发任务查询", Type: 2, Hidden: true, ApiPath: "/api/dev/runs/:key", ApiMethod: "GET"},
		{Path: "dev_runs_cancel", Name: "DevRunsCancel", Title: "取消开发任务", Type: 2, Hidden: true, ApiPath: "/api/dev/runs/:key/cancel", ApiMethod: "POST"},
		{Path: "formgen_activate", Name: "FormgenActivate", Title: "激活表单版本", Type: 2, Hidden: true, ApiPath: "/api/formgen/objects/:objectKey/activate", ApiMethod: "POST"},
	}
	for i := range items {
		if err := global.DB.Where("path = ?", items[i].Path).FirstOrCreate(&items[i]).Error; err != nil {
			return err
		}
	}
	return nil
}
