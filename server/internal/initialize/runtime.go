package initialize

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fde-admin/internal/config"
	"fde-admin/internal/delivery"
	"fde-admin/internal/global"
	zaplogger "fde-admin/internal/logger"
	"fde-admin/internal/model"
	"fde-admin/internal/runner"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"gorm.io/gorm"
)

type bootstrapJournal struct {
	ReleaseID    string `json:"releaseId"`
	ManifestHash string `json:"manifestHash"`
	State        string `json:"state"`
}

func runtimeGlobals(cfg *config.Config) {
	global.Config = cfg
	global.Logger = zaplogger.Init(cfg.Zap.Level, cfg.Zap.Director, cfg.Zap.LogInConsole)
	utils.InitCaptchaStore(cfg.Captcha.ExpireMinutes)
}

func loadRuntimePackage(manifestPath string) (delivery.Package, string, error) {
	if manifestPath == "" {
		return delivery.Package{}, "", errors.New("需要指定已验证的manifest.json")
	}
	pkg, err := delivery.Inspect(filepath.Dir(manifestPath))
	if err != nil {
		return delivery.Package{}, "", err
	}
	hash, err := runner.FileHash(manifestPath)
	return pkg, hash, err
}

// BootstrapRuntime only owns a new database, or resumes its own incomplete installation.
func BootstrapRuntime(cfg *config.Config, configPath, assetsPath, manifestPath string) error {
	pkg, manifestHash, err := loadRuntimePackage(manifestPath)
	if err != nil {
		return err
	}
	if cfg.DB.Driver != "sqlite" {
		return errors.New("P1首次安装仅支持SQLite")
	}
	if len(cfg.JWT.Secret) < 32 || cfg.JWT.Secret == "fde-admin-secret-change-me" || cfg.Seed.AdminUsername == "" || len(cfg.Seed.AdminPassword) < 16 {
		return errors.New("首次安装需要明确管理员与强随机初始化凭据")
	}
	assetBytes, err := os.ReadFile(filepath.Join(assetsPath, "business.json"))
	if err != nil {
		return err
	}
	var assets service.BusinessInstallationAssets
	if json.Unmarshal(assetBytes, &assets) != nil || len(assets.Objects) == 0 {
		return errors.New("发布包没有业务初始化资产")
	}
	if actual, err := runner.FileHash(filepath.Join(assetsPath, "business.json")); err != nil || actual != pkg.Manifest.AssetsHash {
		return errors.New("业务初始化资产与发布清单不一致")
	}
	journalPath := configPath + ".bootstrap.json"
	journal := bootstrapJournal{ReleaseID: pkg.ID, ManifestHash: manifestHash, State: "started"}
	if old, err := os.ReadFile(journalPath); err == nil {
		var previous bootstrapJournal
		if json.Unmarshal(old, &previous) != nil || previous.ReleaseID != pkg.ID || previous.ManifestHash != manifestHash {
			return errors.New("其他安装任务已占用此数据库")
		}
		if previous.State == "completed" {
			return VerifyRuntime(cfg, manifestPath)
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		db, err := openDatabase(cfg)
		if err != nil {
			return err
		}
		var count int64
		err = db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count).Error
		raw, _ := db.DB()
		_ = raw.Close()
		if err != nil {
			return err
		}
		if count != 0 {
			return errors.New("首次安装拒绝接管非空数据库")
		}
		if err := saveBootstrapJournal(journalPath, journal); err != nil {
			return err
		}
	}
	runtimeGlobals(cfg)
	if err := initDB(cfg); err != nil {
		return err
	}
	if err := global.DB.AutoMigrate(&model.RuntimeInstallation{}); err != nil {
		return err
	}
	if err := initCasbin(); err != nil {
		return err
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		adminRole := model.Role{Code: "admin", Name: "管理员", Enabled: true}
		if err := tx.Where("code = ?", "admin").FirstOrCreate(&adminRole).Error; err != nil {
			return err
		}
		operator := model.Role{Code: "operator", Name: "业务操作员", Enabled: true}
		if err := tx.Where("code = ?", "operator").FirstOrCreate(&operator).Error; err != nil {
			return err
		}
		admin := model.User{Username: cfg.Seed.AdminUsername, Nickname: "管理员", Enabled: true}
		if err := tx.Where("username = ?", admin.Username).FirstOrCreate(&admin).Error; err != nil {
			return err
		}
		if admin.Password == "" {
			password, err := utils.HashPassword(cfg.Seed.AdminPassword)
			if err != nil {
				return err
			}
			if err := tx.Model(&admin).Update("password", password).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&admin).Association("Roles").Append(&adminRole); err != nil {
			return err
		}
		project := model.DevProject{Base: model.Base{ID: model.DefaultProjectID}, Key: "default", Name: "客户运行项目"}
		if err := tx.Where("id = ?", model.DefaultProjectID).FirstOrCreate(&project).Error; err != nil {
			return err
		}
		baseMenus := []model.Menu{{Path: "dashboard", Name: "Dashboard", Component: "dashboard/index", Title: "首页", Icon: "Odometer", Type: 1, Affix: true}, {Path: "users", Name: "RuntimeUsers", Component: "system/user/index", Title: "用户管理", Icon: "User", Type: 1}, {Path: "roles", Name: "RuntimeRoles", Component: "system/role/index", Title: "角色管理", Icon: "Lock", Type: 1}}
		for i := range baseMenus {
			menu := &baseMenus[i]
			if err := tx.Where("name = ?", menu.Name).FirstOrCreate(menu).Error; err != nil {
				return err
			}
			if err := tx.Model(&adminRole).Association("Menus").Append(menu); err != nil {
				return err
			}
			if menu.Name == "Dashboard" {
				if err := tx.Model(&operator).Association("Menus").Append(menu); err != nil {
					return err
				}
			}
		}
		for _, asset := range assets.Objects {
			if !formgenRuntimeKey(asset.ObjectKey) || asset.Menu.Path == "" || asset.Menu.Name == "" || len(asset.Schema) == 0 {
				return errors.New("业务初始化定义无效")
			}
			object := model.BizObject{ProjectID: model.DefaultProjectID, Key: asset.ObjectKey, Name: asset.Name, Table: asset.TableName, Version: 1, ActiveVersion: 1}
			schema, _ := json.Marshal(asset.Schema)
			object.Schema = string(schema)
			if err := tx.Where("project_id = ? AND key = ?", model.DefaultProjectID, asset.ObjectKey).FirstOrCreate(&object).Error; err != nil {
				return err
			}
			form := model.FormSchema{ObjectID: object.ID, Version: 1, Schema: string(schema), Published: true, CreatedBy: admin.ID, ActivatedBy: admin.ID}
			if err := tx.Where("object_id = ? AND version = ?", object.ID, 1).FirstOrCreate(&form).Error; err != nil {
				return err
			}
			menu := model.Menu{Path: asset.Menu.Path, Name: asset.Menu.Name, Title: asset.Menu.Title, Component: asset.Menu.Component, Type: 1, Icon: "Document"}
			if err := tx.Where("name = ?", menu.Name).FirstOrCreate(&menu).Error; err != nil {
				return err
			}
			menus := []model.Menu{menu}
			for i, endpoint := range []struct{ path, method string }{{"/api/" + asset.Menu.Path + "/list", "GET"}, {"/api/" + asset.Menu.Path, "POST"}, {"/api/" + asset.Menu.Path + "/:id", "PUT"}, {"/api/" + asset.Menu.Path + "/:id", "DELETE"}, {"/api/biz/schema/" + asset.ObjectKey, "GET"}} {
				button := model.Menu{ParentID: menu.ID, Path: fmt.Sprintf("%s_action_%d", asset.Menu.Path, i), Name: fmt.Sprintf("%sAction%d", asset.Menu.Name, i), Title: "业务操作", Type: 2, Hidden: true, ApiPath: endpoint.path, ApiMethod: endpoint.method}
				if err := tx.Where("name = ?", button.Name).FirstOrCreate(&button).Error; err != nil {
					return err
				}
				menus = append(menus, button)
			}
			if err := tx.Model(&adminRole).Association("Menus").Append(menus); err != nil {
				return err
			}
			if err := tx.Model(&operator).Association("Menus").Append(menus); err != nil {
				return err
			}
		}
		installation := model.RuntimeInstallation{ReleaseID: pkg.ID, SourceHash: pkg.Manifest.SourceHash, ManifestHash: manifestHash, Complete: true}
		return tx.Where("release_id = ?", pkg.ID).FirstOrCreate(&installation).Error
	}); err != nil {
		return err
	}
	if err := service.RebuildCasbin(); err != nil {
		return err
	}
	journal.State = "completed"
	return saveBootstrapJournal(journalPath, journal)
}

func formgenRuntimeKey(key string) bool {
	if key == "" || len(key) > 64 {
		return false
	}
	for i, r := range key {
		if (r < 'a' || r > 'z') && (i == 0 || r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return !strings.HasPrefix(key, "_")
}
func saveBootstrapJournal(path string, journal bootstrapJournal) error {
	raw, _ := json.MarshalIndent(journal, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// VerifyRuntime performs reads only: no AutoMigrate, seed, ensure or policy rebuild.
func VerifyRuntime(cfg *config.Config, manifestPath string) error {
	pkg, hash, err := loadRuntimePackage(manifestPath)
	if err != nil {
		return err
	}
	if len(cfg.JWT.Secret) < 32 || cfg.JWT.Secret == "fde-admin-secret-change-me" {
		return errors.New("运行配置需要明确的强随机JWT密钥")
	}
	if cfg.DB.Driver == "sqlite" {
		if _, err := os.Stat(cfg.DB.DSN); err != nil {
			return errors.New("运行数据库尚未初始化")
		}
	}
	runtimeGlobals(cfg)
	db, err := openDatabase(cfg)
	if err != nil {
		return err
	}
	global.DB = db
	if err := runtimeDatabaseReady(pkg.ID, hash); err != nil {
		return err
	}
	gormadapter.TurnOffAutoMigrate(global.DB)
	if err := initCasbin(); err != nil {
		return err
	}
	return global.Enforcer.LoadPolicy()
}

func runtimeDatabaseReady(releaseID, manifestHash string) error {
	if global.DB == nil {
		return errors.New("数据库未连接")
	}
	raw, err := global.DB.DB()
	if err != nil {
		return err
	}
	if err := raw.Ping(); err != nil {
		return err
	}
	if !global.DB.Migrator().HasTable(&model.RuntimeInstallation{}) {
		return errors.New("数据库尚未完成首次安装")
	}
	var installed model.RuntimeInstallation
	if err := global.DB.Where("release_id = ? AND manifest_hash = ? AND complete = ?", releaseID, manifestHash, true).First(&installed).Error; err != nil {
		return errors.New("数据库与运行版本不匹配")
	}
	for _, registered := range model.RegistryTables() {
		if !global.DB.Migrator().HasTable(registered) {
			return errors.New("业务数据表尚未初始化")
		}
		statement := &gorm.Statement{DB: global.DB}
		if err := statement.Parse(registered); err != nil {
			return err
		}
		for _, field := range statement.Schema.Fields {
			if field.DBName != "" && !global.DB.Migrator().HasColumn(registered, field.DBName) {
				return errors.New("业务数据结构与运行版本不一致")
			}
		}
	}
	return nil
}

func RuntimeReadiness(manifestPath string) error {
	if manifestPath == "" {
		if global.DB == nil {
			return errors.New("数据库未连接")
		}
		raw, err := global.DB.DB()
		if err != nil {
			return err
		}
		return raw.Ping()
	}
	pkg, hash, err := loadRuntimePackage(manifestPath)
	if err != nil {
		return err
	}
	return runtimeDatabaseReady(pkg.ID, hash)
}

// Time reference is included in runtime diagnostics without exposing configuration.
func RuntimeStartedAt() string { return time.Now().UTC().Format(time.RFC3339) }
