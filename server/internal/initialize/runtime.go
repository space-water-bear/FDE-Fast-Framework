package initialize

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
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
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type bootstrapJournal struct {
	ReleaseID    string `json:"releaseId"`
	ManifestHash string `json:"manifestHash"`
	State        string `json:"state"`
	DatabasePath string `json:"databasePath"`
	SourceHash   string `json:"sourceHash"`
}

func runtimeGlobals(cfg *config.Config) {
	if cfg.App.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	global.Config = cfg
	global.Logger = zaplogger.Init(cfg.Zap.Level, cfg.Zap.Director, cfg.Zap.LogInConsole)
	utils.InitCaptchaStore(cfg.Captcha.ExpireMinutes)
}

func loadRuntimePackage(manifestPath string) (delivery.Package, string, error) {
	canonical, err := canonicalRuntimeManifestPath(manifestPath)
	if err != nil {
		return delivery.Package{}, "", err
	}
	manifestPath = canonical
	before, err := runner.FileHash(manifestPath)
	if err != nil {
		return delivery.Package{}, "", err
	}
	pkg, err := delivery.Inspect(filepath.Dir(manifestPath))
	if err != nil {
		return delivery.Package{}, "", err
	}
	hash, err := runner.FileHash(manifestPath)
	if err != nil {
		return delivery.Package{}, "", err
	}
	if before != hash {
		return delivery.Package{}, "", errors.New("校验期间manifest.json发生变化")
	}
	return pkg, hash, nil
}

// BootstrapRuntime only owns a new database, or resumes its own incomplete installation.
func BootstrapRuntime(cfg *config.Config, configPath, assetsPath, manifestPath string) error {
	manifestPath, err := canonicalRuntimeManifestPath(manifestPath)
	if err != nil {
		return err
	}
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
	if err := validateRuntimeBusinessAssets(assets); err != nil {
		return err
	}
	databasePath, err := canonicalRuntimeSQLitePath(cfg.DB.DSN)
	if err != nil {
		return err
	}
	cfg.DB.DSN = databasePath
	maintenanceUnlock, err := lockRuntimeBootstrap(databasePath + ".maintenance")
	if err != nil {
		return err
	}
	defer maintenanceUnlock()
	cfg.App.Mode = "release"
	cfg.Runtime.ManifestPath, cfg.Runtime.ReleaseID, cfg.Runtime.SourceHash = manifestPath, pkg.ID, pkg.Manifest.SourceHash
	journalPath := configPath + ".bootstrap.json"
	unlock, err := lockRuntimeBootstrap(journalPath)
	if err != nil {
		return err
	}
	defer unlock()
	journal := bootstrapJournal{ReleaseID: pkg.ID, ManifestHash: manifestHash, SourceHash: pkg.Manifest.SourceHash, DatabasePath: databasePath, State: "started"}
	complete, err := prepareRuntimeBootstrapDatabase(cfg, journalPath, journal)
	if err != nil {
		return err
	}
	if complete {
		return VerifyRuntime(cfg, manifestPath)
	}
	runtimeGlobals(cfg)
	if err := initDB(cfg); err != nil {
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
		return nil
	}); err != nil {
		return err
	}
	if err := service.RebuildCasbin(); err != nil {
		return err
	}
	result := global.DB.Model(&model.RuntimeInstallation{}).Where("release_id = ? AND source_hash = ? AND manifest_hash = ? AND complete = ?", pkg.ID, pkg.Manifest.SourceHash, manifestHash, false).Update("complete", true)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("首次安装marker已变化，拒绝完成安装")
	}
	journal.State = "completed"
	return saveBootstrapJournal(journalPath, journal)
}

var runtimeBusinessKey = regexp.MustCompile(`^biz_[a-z][a-z0-9_]{0,59}$`)
var runtimeBusinessPath = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var runtimeReservedPaths = map[string]bool{"user": true, "users": true, "role": true, "roles": true, "menu": true, "auth": true, "codegen": true, "formgen": true, "dev": true, "ai": true, "dashboard": true, "system": true, "dict": true, "file": true, "oplog": true, "biz": true, "health": true, "ready": true, "readyz": true}

func validateRuntimeBusinessAssets(assets service.BusinessInstallationAssets) error {
	if len(assets.Objects) == 0 {
		return errors.New("发布包没有业务初始化资产")
	}
	keys, tables, paths, names := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, asset := range assets.Objects {
		if !runtimeBusinessKey.MatchString(asset.ObjectKey) || !runtimeBusinessKey.MatchString(asset.TableName) || !runtimeBusinessPath.MatchString(asset.Menu.Path) || runtimeReservedPaths[asset.Menu.Path] {
			return errors.New("业务资产必须使用独立biz_命名空间及安全业务路由")
		}
		if keys[asset.ObjectKey] || tables[asset.TableName] || paths[asset.Menu.Path] || names[asset.Menu.Name] {
			return errors.New("业务资产包含重复对象、表或菜单")
		}
		keys[asset.ObjectKey], tables[asset.TableName], paths[asset.Menu.Path], names[asset.Menu.Name] = true, true, true, true
		registration, ok := global.GeneratedBusiness(asset.ObjectKey)
		if !ok {
			return fmt.Errorf("业务对象未编译注册: %s", asset.ObjectKey)
		}
		if registration.ObjectKey != asset.ObjectKey || registration.TableName != asset.TableName || registration.Path != asset.Menu.Path || registration.Name != asset.Menu.Name || registration.Component != asset.Menu.Component {
			return fmt.Errorf("业务菜单/表与已编译注册不一致: %s", asset.ObjectKey)
		}
		compiled, err := canonicalRuntimeJSON([]byte(registration.SchemaJSON))
		if err != nil {
			return fmt.Errorf("编译业务schema无效: %s", asset.ObjectKey)
		}
		schemaBytes, err := json.Marshal(asset.Schema)
		if err != nil {
			return err
		}
		actual, err := canonicalRuntimeJSON(schemaBytes)
		if err != nil || !bytes.Equal(actual, compiled) {
			return fmt.Errorf("业务schema与编译版本不一致: %s", asset.ObjectKey)
		}
		if name, ok := asset.Schema["name"].(string); !ok || name == "" || asset.Name != name || asset.Menu.Title != name {
			return fmt.Errorf("业务标题与编译schema不一致: %s", asset.ObjectKey)
		}
	}
	return nil
}
func canonicalRuntimeJSON(raw []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func canonicalRuntimeManifestPath(path string) (string, error) {
	if path == "" || filepath.Base(filepath.Clean(path)) != "manifest.json" {
		return "", errors.New("需要指定规范manifest.json文件")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("manifest.json必须为普通文件，不能是符号链接")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", err
	}
	canonical := filepath.Join(parent, "manifest.json")
	actual, err := os.Lstat(canonical)
	if err != nil || !actual.Mode().IsRegular() || actual.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, actual) {
		return "", errors.New("manifest.json路径已变化")
	}
	return canonical, nil
}

// Runtime SQLite deliberately accepts only plain filesystem paths, never URI,
// memory, query or symlink targets. Existing parents are resolved for stable identity.
func canonicalRuntimeSQLitePath(dsn string) (string, error) {
	if dsn == "" || strings.ContainsAny(dsn, "?\x00") || strings.HasPrefix(dsn, "file:") || strings.Contains(dsn, ":memory:") {
		return "", errors.New("运行SQLite必须使用普通数据库文件路径")
	}
	absolute, err := filepath.Abs(dsn)
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(absolute); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("运行数据库不能是符号链接或特殊文件")
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(absolute)
	missing := []string{}
	for {
		if _, err := os.Stat(parent); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		missing = append([]string{filepath.Base(parent)}, missing...)
		next := filepath.Dir(parent)
		if next == parent {
			return "", errors.New("数据库目录无效")
		}
		parent = next
	}
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
	}
	parts := append([]string{resolved}, missing...)
	parts = append(parts, filepath.Base(absolute))
	canonical := filepath.Join(parts...)
	if info, err := os.Lstat(canonical); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("运行数据库不能是符号链接或特殊文件")
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return canonical, nil
}

func prepareRuntimeBootstrapDatabase(cfg *config.Config, journalPath string, journal bootstrapJournal) (bool, error) {
	if cfg.DB.Driver != "sqlite" {
		return false, errors.New("P1首次安装仅支持SQLite")
	}
	canonical, err := canonicalRuntimeSQLitePath(cfg.DB.DSN)
	if err != nil {
		return false, err
	}
	if journal.DatabasePath != canonical {
		return false, errors.New("安装日志数据库目标不一致")
	}
	cfg.DB.DSN = canonical
	hadJournal := false
	completedJournal := false
	if info, err := os.Lstat(journalPath); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("安装日志不能是符号链接或特殊文件")
		}
		raw, err := os.ReadFile(journalPath)
		if err != nil {
			return false, err
		}
		var previous bootstrapJournal
		if json.Unmarshal(raw, &previous) != nil || previous.ReleaseID != journal.ReleaseID || previous.ManifestHash != journal.ManifestHash || previous.SourceHash != journal.SourceHash || previous.DatabasePath != canonical || (previous.State != "started" && previous.State != "completed") {
			return false, errors.New("安装日志与数据库目标或发布版本不匹配")
		}
		hadJournal = true
		completedJournal = previous.State == "completed"
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if hadJournal {
		if _, err := os.Stat(canonical); err != nil {
			return false, errors.New("安装日志对应的数据库已丢失，拒绝重建")
		}
	}
	db, err := openDatabase(cfg)
	if err != nil {
		return false, err
	}
	raw, err := db.DB()
	if err != nil {
		return false, err
	}
	defer raw.Close()
	complete := false
	err = db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			if hadJournal {
				return errors.New("已开始安装的数据库缺少自身marker，拒绝重放安装")
			}
			if err := tx.Migrator().CreateTable(&model.RuntimeInstallation{}); err != nil {
				return err
			}
			return tx.Create(&model.RuntimeInstallation{ReleaseID: journal.ReleaseID, SourceHash: journal.SourceHash, ManifestHash: journal.ManifestHash, Complete: false}).Error
		}
		if !tx.Migrator().HasTable(&model.RuntimeInstallation{}) {
			return errors.New("首次安装拒绝接管无自身marker的非空数据库")
		}
		var markers []model.RuntimeInstallation
		if err := tx.Find(&markers).Error; err != nil {
			return err
		}
		if len(markers) != 1 || markers[0].ReleaseID != journal.ReleaseID || markers[0].SourceHash != journal.SourceHash || markers[0].ManifestHash != journal.ManifestHash {
			return errors.New("数据库自身安装marker与发布版本不匹配")
		}
		complete = markers[0].Complete
		if completedJournal && !complete {
			return errors.New("已完成安装日志与数据库未完成marker不一致")
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if complete {
		journal.State = "completed"
	}
	if err := saveBootstrapJournal(journalPath, journal); err != nil {
		return false, err
	}
	return complete, nil
}

func lockRuntimeBootstrap(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	lockPath := path + ".lock"
	if info, err := os.Lstat(lockPath); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("安装锁文件不能是链接")
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("此配置已有首次安装任务运行")
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}

func saveBootstrapJournal(path string, journal bootstrapJournal) error {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("安装日志不能是符号链接或特殊文件")
	}
	raw, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".bootstrap-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// VerifyRuntime performs reads only: no AutoMigrate, seed, ensure or policy rebuild.
func VerifyRuntime(cfg *config.Config, manifestPath string) error {
	manifestPath, err := canonicalRuntimeManifestPath(manifestPath)
	if err != nil {
		return err
	}
	pkg, hash, err := loadRuntimePackage(manifestPath)
	if err != nil {
		return err
	}
	if len(cfg.JWT.Secret) < 32 || cfg.JWT.Secret == "fde-admin-secret-change-me" {
		return errors.New("运行配置需要明确的强随机JWT密钥")
	}
	cfg.App.Mode = "release"
	cfg.Runtime.ManifestPath, cfg.Runtime.ReleaseID, cfg.Runtime.SourceHash = manifestPath, pkg.ID, pkg.Manifest.SourceHash
	if cfg.DB.Driver == "sqlite" {
		canonical, err := canonicalRuntimeSQLitePath(cfg.DB.DSN)
		if err != nil {
			return err
		}
		cfg.DB.DSN = canonical
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
