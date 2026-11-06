package initialize

import (
	"encoding/json"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/service"
	"fde-admin/internal/toolcontract"
	"fde-admin/internal/utils"
)

// seedData 首次启动写入内置数据: 菜单/角色/用户, 并重建 casbin 策略
func seedData() error {
	if !global.Config.Seed.Enabled {
		return nil
	}
	var count int64
	if err := global.DB.Model(&model.User{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	db := global.DB
	create := func(v any) error { return db.Create(v).Error }

	// ---- 菜单 ----
	dashboard := &model.Menu{Path: "dashboard", Name: "Dashboard", Component: "dashboard/index", Title: "首页", Icon: "Odometer", Type: 1, Sort: 1, Affix: true}
	system := &model.Menu{Path: "system", Name: "System", Title: "系统管理", Icon: "Setting", Type: 0, Sort: 2}
	mUser := &model.Menu{Path: "user", Name: "SystemUser", Component: "system/user/index", Title: "用户管理", Icon: "User", Type: 1, Sort: 1}
	mRole := &model.Menu{Path: "role", Name: "SystemRole", Component: "system/role/index", Title: "角色管理", Icon: "UserFilled", Type: 1, Sort: 2}
	mMenu := &model.Menu{Path: "menu", Name: "SystemMenu", Component: "system/menu/index", Title: "菜单管理", Icon: "Menu", Type: 1, Sort: 3}
	mCodegen := &model.Menu{Path: "codegen", Name: "SystemCodegen", Component: "system/codegen/index", Title: "代码生成器", Icon: "MagicStick", Type: 1, Sort: 4}
	for _, m := range []*model.Menu{dashboard, system, mUser, mRole, mMenu, mCodegen} {
		if err := create(m); err != nil {
			return err
		}
	}
	mUser.ParentID = system.ID
	mRole.ParentID = system.ID
	mMenu.ParentID = system.ID
	mCodegen.ParentID = system.ID
	for _, m := range []*model.Menu{mUser, mRole, mMenu, mCodegen} {
		if err := db.Save(m).Error; err != nil {
			return err
		}
	}

	allMenus := []model.Menu{*dashboard, *system, *mUser, *mRole, *mMenu, *mCodegen}
	buttons := []struct {
		parent *model.Menu
		title  string
		path   string
		method string
	}{
		{mUser, "用户-查询", "/api/user/list", "GET"},
		{mUser, "用户-新增", "/api/user", "POST"},
		{mUser, "用户-编辑", "/api/user/:id", "PUT"},
		{mUser, "用户-删除", "/api/user/:id", "DELETE"},
		{mRole, "角色-查询", "/api/role/list", "GET"},
		{mRole, "角色-详情", "/api/role/:id", "GET"},
		{mRole, "角色-新增", "/api/role", "POST"},
		{mRole, "角色-编辑", "/api/role/:id", "PUT"},
		{mRole, "角色-删除", "/api/role/:id", "DELETE"},
		{mMenu, "菜单-查询", "/api/menu/tree", "GET"},
		{mMenu, "菜单-新增", "/api/menu", "POST"},
		{mMenu, "菜单-编辑", "/api/menu/:id", "PUT"},
		{mMenu, "菜单-删除", "/api/menu/:id", "DELETE"},
		{mCodegen, "生成器-选项", "/api/codegen/options", "GET"},
		{mCodegen, "生成器-预览", "/api/codegen/preview", "POST"},
		{mCodegen, "生成器-执行", "/api/codegen/generate", "POST"},
	}
	for _, b := range buttons {
		m := &model.Menu{ParentID: b.parent.ID, Title: b.title, Type: 2, Hidden: true, ApiPath: b.path, ApiMethod: b.method}
		if err := create(m); err != nil {
			return err
		}
		allMenus = append(allMenus, *m)
	}

	// ---- 角色 ----
	adminRole := &model.Role{Name: "超级管理员", Code: "admin", Description: "拥有全部权限", Enabled: true}
	userRole := &model.Role{Name: "普通用户", Code: "user", Description: "仅可访问首页", Enabled: true}
	if err := create(adminRole); err != nil {
		return err
	}
	if err := create(userRole); err != nil {
		return err
	}
	if err := db.Model(adminRole).Association("Menus").Replace(allMenus); err != nil {
		return err
	}
	if err := db.Model(userRole).Association("Menus").Replace([]model.Menu{*dashboard}); err != nil {
		return err
	}

	// ---- 用户 ----
	adminPass, err := utils.HashPassword(global.Config.Seed.AdminPassword)
	if err != nil {
		return err
	}
	demoPass, err := utils.HashPassword("123456")
	if err != nil {
		return err
	}
	adminUser := &model.User{Username: global.Config.Seed.AdminUsername, Password: adminPass, Nickname: "系统管理员", Enabled: true}
	demoUser := &model.User{Username: "user", Password: demoPass, Nickname: "演示用户", Enabled: true}
	if err := create(adminUser); err != nil {
		return err
	}
	if err := create(demoUser); err != nil {
		return err
	}
	if err := db.Model(adminUser).Association("Roles").Replace([]model.Role{*adminRole}); err != nil {
		return err
	}
	if err := db.Model(demoUser).Association("Roles").Replace([]model.Role{*userRole}); err != nil {
		return err
	}

	return service.RebuildCasbin()
}

// ensureCodegenMenu 为已初始化的老库补齐代码生成器菜单(幂等)
func ensureCodegenMenu() error {
	var count int64
	if err := global.DB.Model(&model.Menu{}).Where("path = ? AND name = ?", "codegen", "SystemCodegen").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	var system model.Menu
	if err := global.DB.Where("path = ? AND name = ?", "system", "System").First(&system).Error; err != nil {
		return err
	}
	menu := &model.Menu{ParentID: system.ID, Path: "codegen", Name: "SystemCodegen", Component: "system/codegen/index", Title: "代码生成器", Icon: "MagicStick", Type: 1, Sort: 4}
	if err := global.DB.Create(menu).Error; err != nil {
		return err
	}
	btns := []*model.Menu{
		{ParentID: menu.ID, Title: "生成器-选项", Type: 2, Hidden: true, ApiPath: "/api/codegen/options", ApiMethod: "GET"},
		{ParentID: menu.ID, Title: "生成器-预览", Type: 2, Hidden: true, ApiPath: "/api/codegen/preview", ApiMethod: "POST"},
		{ParentID: menu.ID, Title: "生成器-执行", Type: 2, Hidden: true, ApiPath: "/api/codegen/generate", ApiMethod: "POST"},
	}
	for _, b := range btns {
		if err := global.DB.Create(b).Error; err != nil {
			return err
		}
	}
	var admin model.Role
	if err := global.DB.Where("code = ?", "admin").First(&admin).Error; err != nil {
		return err
	}
	menus := append([]*model.Menu{menu}, btns...)
	if err := global.DB.Model(&admin).Association("Menus").Append(menus); err != nil {
		return err
	}
	return service.RebuildCasbin()
}

// ensureSystemMenu 为老库补齐系统管理下的子菜单与按钮(幂等)
func ensureSystemMenu(path, name, component, title, icon string, sort int, btns []struct {
	title  string
	api    string
	method string
}) error {
	var count int64
	if err := global.DB.Model(&model.Menu{}).Where("path = ? AND name = ?", path, name).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	var system model.Menu
	if err := global.DB.Where("path = ? AND name = ?", "system", "System").First(&system).Error; err != nil {
		return err
	}
	menu := &model.Menu{ParentID: system.ID, Path: path, Name: name, Component: component, Title: title, Icon: icon, Type: 1, Sort: sort}
	if err := global.DB.Create(menu).Error; err != nil {
		return err
	}
	all := []*model.Menu{menu}
	for _, b := range btns {
		m := &model.Menu{ParentID: menu.ID, Title: b.title, Type: 2, Hidden: true, ApiPath: b.api, ApiMethod: b.method}
		if err := global.DB.Create(m).Error; err != nil {
			return err
		}
		all = append(all, m)
	}
	var admin model.Role
	if err := global.DB.Where("code = ?", "admin").First(&admin).Error; err != nil {
		return err
	}
	if err := global.DB.Model(&admin).Association("Menus").Append(all); err != nil {
		return err
	}
	return service.RebuildCasbin()
}

// ensureDictMenu 字典管理菜单(幂等)
func ensureDictMenu() error {
	return ensureSystemMenu("dict", "SystemDict", "system/dict/index", "字典管理", "Collection", 5, []struct {
		title  string
		api    string
		method string
	}{
		{"字典类型-查询", "/api/dict/types", "GET"},
		{"字典类型-新增", "/api/dict/type", "POST"},
		{"字典类型-编辑", "/api/dict/type/:id", "PUT"},
		{"字典类型-删除", "/api/dict/type/:id", "DELETE"},
		{"字典数据-查询", "/api/dict/data/list", "GET"},
		{"字典数据-新增", "/api/dict/data", "POST"},
		{"字典数据-编辑", "/api/dict/data/:id", "PUT"},
		{"字典数据-删除", "/api/dict/data/:id", "DELETE"},
	})
}

// ensureOplogMenu 操作日志菜单(幂等)
func ensureOplogMenu() error {
	return ensureSystemMenu("oplog", "SystemOplog", "system/oplog/index", "操作日志", "Document", 6, []struct {
		title  string
		api    string
		method string
	}{
		{"日志-查询", "/api/oplog/list", "GET"},
		{"日志-删除", "/api/oplog/:id", "DELETE"},
		{"日志-清空", "/api/oplog/clear", "DELETE"},
	})
}

// ensureDeviceMenu 登录设备菜单(幂等)
func ensureDeviceMenu() error {
	return ensureSystemMenu("device", "SystemDevice", "system/device/index", "登录设备", "Monitor", 7, []struct {
		title  string
		api    string
		method string
	}{
		{"设备-查询", "/api/auth/devices", "GET"},
		{"设备-踢下线", "/api/auth/devices/:id", "DELETE"},
		{"设备-踢出其他", "/api/auth/devices", "DELETE"},
	})
}

// ensureFileMenu 文件管理菜单(幂等)
func ensureFileMenu() error {
	return ensureSystemMenu("file", "SystemFile", "system/file/index", "文件管理", "Folder", 8, []struct {
		title  string
		api    string
		method string
	}{
		{"文件-上传", "/api/file/upload", "POST"},
		{"文件-查询", "/api/file/list", "GET"},
		{"文件-删除", "/api/file/:id", "DELETE"},
	})
}

// ensureTableDemoMenu 表格组件示例菜单(幂等)
func ensureTableDemoMenu() error {
	return ensureSystemMenu("table-demo", "SystemTableDemo", "demo/table-page/index", "表格组件示例", "Grid", 9, nil)
}

// ensureAiUsageMenu 用量统计菜单(幂等, 老库补建)
func ensureAiUsageMenu() error {
	var aiDir model.Menu
	if err := global.DB.Where("path = ? AND name = ?", "ai", "Ai").First(&aiDir).Error; err != nil {
		return nil
	}
	var n int64
	if err := global.DB.Model(&model.Menu{}).Where("parent_id = ? AND name = ?", aiDir.ID, "AiUsage").Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	m := &model.Menu{ParentID: aiDir.ID, Path: "usage", Name: "AiUsage", Component: "system/ai/usage/index", Title: "用量统计", Icon: "DataAnalysis", Type: 1, Sort: 7}
	if err := global.DB.Create(m).Error; err != nil {
		return err
	}
	btns := []*model.Menu{
		{ParentID: m.ID, Title: "用量统计-查询", Type: 2, Hidden: true, ApiPath: "/api/ai/usage/list", ApiMethod: "GET"},
		{ParentID: m.ID, Title: "用量统计-汇总", Type: 2, Hidden: true, ApiPath: "/api/ai/usage/summary", ApiMethod: "GET"},
	}
	var admin model.Role
	if err := global.DB.Where("code = ?", "admin").First(&admin).Error; err != nil {
		return err
	}
	for _, b := range btns {
		if err := global.DB.Create(b).Error; err != nil {
			return err
		}
		if err := global.DB.Model(&admin).Association("Menus").Append(b); err != nil {
			return err
		}
	}
	if err := global.DB.Model(&admin).Association("Menus").Append(m); err != nil {
		return err
	}
	return service.RebuildCasbin()
}

// ensureAiSessionStreamButton 为老库补建会话流式聊天按钮(幂等)
func ensureAiSessionStreamButton() error {
	var sessionMenu model.Menu
	if err := global.DB.Where("path = ? AND name = ?", "session", "AiSession").First(&sessionMenu).Error; err != nil {
		return nil
	}
	var n int64
	if err := global.DB.Model(&model.Menu{}).Where("parent_id = ? AND api_path = ?", sessionMenu.ID, "/api/ai/session/:id/chat/stream").Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	btn := &model.Menu{ParentID: sessionMenu.ID, Title: "会话-流式聊天", Type: 2, Hidden: true, ApiPath: "/api/ai/session/:id/chat/stream", ApiMethod: "POST"}
	if err := global.DB.Create(btn).Error; err != nil {
		return err
	}
	var admin model.Role
	if err := global.DB.Where("code = ?", "admin").First(&admin).Error; err != nil {
		return err
	}
	if err := global.DB.Model(&admin).Association("Menus").Append(btn); err != nil {
		return err
	}
	return service.RebuildCasbin()
}

// ensureAiMenus AI 助手目录与子菜单(幂等)

// ensureFormgenAiTool upgrades only recognizable built-in protocol metadata.
func ensureFormgenAiTool() error { return ensureEngineeringToolDefinitions("generate_form_schema") }

func ensureEngineeringToolDefinitions(names ...string) error {
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	for _, definition := range toolcontract.Definitions(false) {
		if !wanted[definition.Name] {
			continue
		}
		var existing []model.AiTool
		if err := global.DB.Where("name = ?", definition.Name).Find(&existing).Error; err != nil {
			return err
		}
		raw, err := json.Marshal(definition.Schema)
		if err != nil {
			return err
		}
		if len(existing) == 0 {
			marker := "代码生成器 P3"
			if definition.Name == "generate_form_schema" {
				marker = "表单生成器 P1"
			}
			if err := global.DB.Create(&model.AiTool{Name: definition.Name, Description: definition.Description, Schema: string(raw), Enabled: true, Remark: marker}).Error; err != nil {
				return err
			}
			continue
		}
		for _, item := range existing {
			// Names alone do not make a user-created tool ours to overwrite.
			expectedMarker := "代码生成器 P3"
			if definition.Name == "generate_form_schema" {
				expectedMarker = "表单生成器 P1"
			}
			if item.Remark != expectedMarker {
				continue
			}
			if err := global.DB.Model(&item).Updates(map[string]any{"schema": string(raw), "description": definition.Description}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// ensureFormgenMenu 表单生成菜单与预览按钮(幂等)
func ensureFormgenMenu() error {
	return ensureSystemMenu("formgen", "Formgen", "system/formgen/index", "表单生成", "Document", 10, []struct {
		title  string
		api    string
		method string
	}{
		{"表单生成-查询", "/api/formgen/objects", "GET"},
		{"表单生成-新增", "/api/formgen/objects", "POST"},
		{"表单生成-更新", "/api/formgen/objects/:objectKey", "PUT"},
		{"表单生成-schema查询", "/api/formgen/schema/:objectKey", "GET"},
		{"表单 schema 预览", "/api/formgen/schema/preview", "POST"},
		{"表单生成-apply", "/api/formgen/apply", "POST"},
	})
}

// ensureFormgenApplyButton 为老库补齐表单生成 apply 按钮(幂等)
func ensureFormgenApplyButton() error {
	var formgen model.Menu
	if err := global.DB.Where("path = ? AND name = ?", "formgen", "Formgen").First(&formgen).Error; err != nil {
		return nil
	}
	var n int64
	if err := global.DB.Model(&model.Menu{}).Where("parent_id = ? AND api_path = ?", formgen.ID, "/api/formgen/apply").Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	btn := &model.Menu{ParentID: formgen.ID, Title: "表单生成-apply", Type: 2, Hidden: true, ApiPath: "/api/formgen/apply", ApiMethod: "POST"}
	if err := global.DB.Create(btn).Error; err != nil {
		return err
	}
	var admin model.Role
	if err := global.DB.Where("code = ?", "admin").First(&admin).Error; err != nil {
		return nil
	}
	if err := global.DB.Model(&admin).Association("Menus").Append(btn); err != nil {
		return err
	}
	return service.RebuildCasbin()
}

// ensureFormgenUpdateButton 为老库补齐表单生成更新按钮(幂等)
func ensureFormgenUpdateButton() error {
	var formgen model.Menu
	if err := global.DB.Where("path = ? AND name = ?", "formgen", "Formgen").First(&formgen).Error; err != nil {
		return nil
	}
	var n int64
	if err := global.DB.Model(&model.Menu{}).Where("parent_id = ? AND api_path = ?", formgen.ID, "/api/formgen/objects/:objectKey").Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	btn := &model.Menu{ParentID: formgen.ID, Title: "表单生成-更新", Type: 2, Hidden: true, ApiPath: "/api/formgen/objects/:objectKey", ApiMethod: "PUT"}
	if err := global.DB.Create(btn).Error; err != nil {
		return err
	}
	var admin model.Role
	if err := global.DB.Where("code = ?", "admin").First(&admin).Error; err != nil {
		return nil
	}
	if err := global.DB.Model(&admin).Association("Menus").Append(btn); err != nil {
		return err
	}
	return service.RebuildCasbin()
}

// ensureCodegenAiTools preserves enablement and user-defined tools while upgrading built-ins.
func ensureCodegenAiTools() error {
	return ensureEngineeringToolDefinitions("generate_business_code", "apply_files")
}

func ensureAiMenus() error {
	var count int64
	if err := global.DB.Model(&model.Menu{}).Where("path = ? AND name = ?", "ai", "Ai").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	dir := &model.Menu{Path: "ai", Name: "Ai", Title: "AI 助手", Icon: "Cpu", Type: 0, Sort: 3}
	if err := global.DB.Create(dir).Error; err != nil {
		return err
	}

	type menuDef struct {
		path, name, component, title, icon string
		sort                               int
	}
	defs := []menuDef{
		{"provider", "AiProvider", "system/ai/provider/index", "供应商", "OfficeBuilding", 1},
		{"agent", "AiAgent", "system/ai/agent/index", "智能体", "Avatar", 2},
		{"skill", "AiSkill", "system/ai/skill/index", "技能", "MagicStick", 3},
		{"tool", "AiTool", "system/ai/tool/index", "工具", "SetUp", 4},
		{"mcp", "AiMcp", "system/ai/mcp/index", "MCP服务", "Connection", 5},
		{"session", "AiSession", "system/ai/session/index", "会话", "ChatDotRound", 6},
	}

	all := []model.Menu{*dir}
	for _, d := range defs {
		m := &model.Menu{ParentID: dir.ID, Path: d.path, Name: d.name, Component: d.component, Title: d.title, Icon: d.icon, Type: 1, Sort: d.sort}
		if err := global.DB.Create(m).Error; err != nil {
			return err
		}
		all = append(all, *m)
		prefix := "/api/ai/" + d.path
		btns := []*model.Menu{
			{ParentID: m.ID, Title: d.title + "-查询", Type: 2, Hidden: true, ApiPath: prefix + "/list", ApiMethod: "GET"},
			{ParentID: m.ID, Title: d.title + "-新增", Type: 2, Hidden: true, ApiPath: prefix, ApiMethod: "POST"},
			{ParentID: m.ID, Title: d.title + "-编辑", Type: 2, Hidden: true, ApiPath: prefix + "/:id", ApiMethod: "PUT"},
			{ParentID: m.ID, Title: d.title + "-删除", Type: 2, Hidden: true, ApiPath: prefix + "/:id", ApiMethod: "DELETE"},
		}
		for _, b := range btns {
			if err := global.DB.Create(b).Error; err != nil {
				return err
			}
			all = append(all, *b)
		}
		if d.path == "session" {
			extras := []*model.Menu{
				{ParentID: m.ID, Title: "会话-消息", Type: 2, Hidden: true, ApiPath: "/api/ai/session/:id/messages", ApiMethod: "GET"},
				{ParentID: m.ID, Title: "会话-聊天", Type: 2, Hidden: true, ApiPath: "/api/ai/session/:id/chat", ApiMethod: "POST"},
			}
			for _, b := range extras {
				if err := global.DB.Create(b).Error; err != nil {
					return err
				}
				all = append(all, *b)
			}
		}
	}

	var admin model.Role
	if err := global.DB.Where("code = ?", "admin").First(&admin).Error; err != nil {
		return err
	}
	if err := global.DB.Model(&admin).Association("Menus").Append(all); err != nil {
		return err
	}
	return service.RebuildCasbin()
}
