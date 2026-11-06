package initialize

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/casbin/casbin/v2"
	casbinmodel "github.com/casbin/casbin/v2/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"fde-admin/internal/config"
	"fde-admin/internal/global"
	zaplogger "fde-admin/internal/logger"
	"fde-admin/internal/model"
	"fde-admin/internal/utils"
)

// CasbinModelText RBAC 模型: 用户经 g(用户,角色) 获得角色, 角色经 p(角色,路径,方法) 获得接口权限
// keyMatch2 支持 :id 形式的路由通配
const CasbinModelText = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && keyMatch2(r.obj, p.obj) && (r.act == p.act || p.act == "*")
`

// Run 初始化: 运行模式 -> 数据库 -> casbin -> 种子数据 -> 菜单补齐
func Run(cfg *config.Config) {
	global.Config = cfg
	global.Logger = zaplogger.Init(cfg.Zap.Level, cfg.Zap.Director, cfg.Zap.LogInConsole)
	utils.InitCaptchaStore(cfg.Captcha.ExpireMinutes)
	if cfg.App.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	if err := initDB(cfg); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	if err := initCasbin(); err != nil {
		log.Fatalf("初始化casbin失败: %v", err)
	}
	if err := seedData(); err != nil {
		log.Fatalf("初始化种子数据失败: %v", err)
	}
	for name, fn := range map[string]func() error{
		"代码生成器菜单":     ensureCodegenMenu,
		"字典管理菜单":      ensureDictMenu,
		"操作日志菜单":      ensureOplogMenu,
		"登录设备菜单":      ensureDeviceMenu,
		"文件管理菜单":      ensureFileMenu,
		"表格示例菜单":      ensureTableDemoMenu,
		"AI助手菜单":      ensureAiMenus,
		"AI流式聊天按钮":    ensureAiSessionStreamButton,
		"AI用量统计菜单":    ensureAiUsageMenu,
		"表单生成工具":      ensureFormgenAiTool,
		"表单生成菜单":      ensureFormgenMenu,
		"表单生成apply按钮": ensureFormgenApplyButton,
		"表单生成更新按钮":    ensureFormgenUpdateButton,
		"代码生成工具":      ensureCodegenAiTools,
	} {
		if err := fn(); err != nil {
			log.Fatalf("初始化%s失败: %v", name, err)
		}
	}
}

func initDB(cfg *config.Config) error {
	var dialector gorm.Dialector
	switch cfg.DB.Driver {
	case "sqlite":
		if dir := filepath.Dir(cfg.DB.DSN); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		dialector = sqlite.Open(cfg.DB.DSN)
	case "postgres":
		dialector = postgres.Open(cfg.DB.DSN)
	default:
		return fmt.Errorf("不支持的数据库驱动: %s (仅支持 sqlite / postgres)", cfg.DB.Driver)
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return err
	}
	models := append([]any{
		&model.User{}, &model.Role{}, &model.Menu{},
		&model.RefreshToken{}, &model.DictType{}, &model.DictData{}, &model.OperationLog{},
		&model.ActiveToken{}, &model.JwtBlacklist{}, &model.File{},
		&model.AiProvider{}, &model.AiAgent{}, &model.AiSkill{}, &model.AiTool{},
		&model.AiMcpServer{}, &model.AiSession{}, &model.AiMessage{}, &model.AiUsage{},
		&model.BizObject{}, &model.BizField{}, &model.FormSchema{}, &model.GenAudit{},
	}, model.RegistryTables()...)
	if err := db.AutoMigrate(models...); err != nil {
		return err
	}
	global.DB = db
	return nil
}

func initCasbin() error {
	adapter, err := gormadapter.NewAdapterByDB(global.DB)
	if err != nil {
		return err
	}
	m, err := casbinmodel.NewModelFromString(CasbinModelText)
	if err != nil {
		return err
	}
	enforcer, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return err
	}
	global.Enforcer = enforcer
	return nil
}
