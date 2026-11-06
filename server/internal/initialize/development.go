package initialize

import (
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/service"
	"gorm.io/gorm/clause"
)

func ensureDevelopmentDefaults() error {
	project := model.DevProject{Base: model.Base{ID: model.DefaultProjectID}, Key: "default", Name: "默认项目（原生成器）"}
	return global.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&project).Error
}

func ensureDevelopmentMenus() error {
	var system model.Menu
	if err := global.DB.Where("path = ? AND type = ?", "system", 0).First(&system).Error; err != nil {
		return err
	}
	page := model.Menu{ParentID: system.ID, Path: "development", Name: "DevelopmentWorkbench", Title: "开发工作台", Component: "system/development/index", Type: 1, Icon: "MagicStick", Sort: 12}
	if err := global.DB.Where("name = ?", page.Name).FirstOrCreate(&page).Error; err != nil {
		return err
	}
	items := []model.Menu{
		{Path: "dev_projects_list", Name: "DevProjectsList", Title: "开发项目查询", Type: 2, Hidden: true, ApiPath: "/api/dev/projects", ApiMethod: "GET"},
		{Path: "dev_projects_create", Name: "DevProjectsCreate", Title: "创建客户项目", Type: 2, Hidden: true, ApiPath: "/api/dev/projects", ApiMethod: "POST"},
		{Path: "dev_runs_create", Name: "DevRunsCreate", Title: "保存开发任务", Type: 2, Hidden: true, ApiPath: "/api/dev/runs", ApiMethod: "POST"},
		{Path: "dev_runs_list", Name: "DevRunsList", Title: "开发任务列表", Type: 2, Hidden: true, ApiPath: "/api/dev/runs", ApiMethod: "GET"},
		{Path: "dev_release_build", Name: "DevReleaseBuild", Title: "构建候选发布包", Type: 2, Hidden: true, ApiPath: "/api/dev/runs/:key/release", ApiMethod: "POST"},
		{Path: "dev_release_get", Name: "DevReleaseGet", Title: "查看发布包", Type: 2, Hidden: true, ApiPath: "/api/dev/releases/:releaseId", ApiMethod: "GET"},
		{Path: "dev_release_download", Name: "DevReleaseDownload", Title: "下载发布包", Type: 2, Hidden: true, ApiPath: "/api/dev/releases/:releaseId/download", ApiMethod: "GET"},
		{Path: "dev_release_preview", Name: "DevReleasePreview", Title: "启动业务预览", Type: 2, Hidden: true, ApiPath: "/api/dev/releases/:releaseId/preview", ApiMethod: "POST"},
		{Path: "dev_release_deploy", Name: "DevReleaseDeploy", Title: "首次部署测试目标", Type: 2, Hidden: true, ApiPath: "/api/dev/releases/:releaseId/deploy", ApiMethod: "POST"},
		{Path: "dev_target_list", Name: "DevTargetList", Title: "部署目标列表", Type: 2, Hidden: true, ApiPath: "/api/dev/targets", ApiMethod: "GET"},
		{Path: "dev_runs_read", Name: "DevRunsRead", Title: "开发任务查询", Type: 2, Hidden: true, ApiPath: "/api/dev/runs/:key", ApiMethod: "GET"},
		{Path: "dev_runs_cancel", Name: "DevRunsCancel", Title: "取消开发任务", Type: 2, Hidden: true, ApiPath: "/api/dev/runs/:key/cancel", ApiMethod: "POST"},
		{Path: "formgen_activate", Name: "FormgenActivate", Title: "激活表单版本", Type: 2, Hidden: true, ApiPath: "/api/formgen/objects/:objectKey/activate", ApiMethod: "POST"},
	}
	for i := range items {
		items[i].ParentID = page.ID
		if err := global.DB.Where("path = ?", items[i].Path).FirstOrCreate(&items[i]).Error; err != nil {
			return err
		}
	}
	var admin model.Role
	if err := global.DB.Where("code = ?", "admin").First(&admin).Error; err != nil {
		return err
	}
	menus := append([]model.Menu{page}, items...)
	if err := global.DB.Model(&admin).Association("Menus").Append(menus); err != nil {
		return err
	}
	return service.RebuildCasbin()
}
