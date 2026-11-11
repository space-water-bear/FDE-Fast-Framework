package initialize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fde-admin/internal/config"
	"fde-admin/internal/delivery"
	"fde-admin/internal/model"
	"fde-admin/internal/runner"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
	"gorm.io/gorm"
)

type RuntimeUpgradeResult struct {
	State         string `json:"state"`
	FromReleaseID string `json:"fromReleaseId"`
	ReleaseID     string `json:"releaseId"`
	JournalPath   string `json:"journalPath"`
	BackupID      string `json:"backupId"`
	PlanHash      string `json:"planHash"`
}

type runtimeColumnAddition struct {
	Table      string `json:"table"`
	Field      string `json:"field"`
	Definition string `json:"definition"`
}

type runtimePermissions struct {
	Menus          []model.Menu      `json:"menus"`
	RoleMenus      []runtimeGrant    `json:"roleMenus"`
	UserRoles      []runtimeUserRole `json:"userRoles"`
	PrincipalsHash string            `json:"principalsHash"`
}
type runtimeGrant struct {
	RoleID uint `json:"roleId"`
	MenuID uint `json:"menuId"`
}
type runtimeUserRole struct {
	UserID uint `json:"userId"`
	RoleID uint `json:"roleId"`
}
type runtimeUpgradeJournal struct {
	RuntimeUpgradeResult
	OldManifestHash    string             `json:"oldManifestHash"`
	NewManifestHash    string             `json:"newManifestHash"`
	DatabasePath       string             `json:"databasePath"`
	DatabaseBackup     string             `json:"databaseBackup"`
	DatabaseHash       string             `json:"databaseHash"`
	SourceDatabaseHash string             `json:"sourceDatabaseHash"`
	UploadsPath        string             `json:"uploadsPath"`
	UploadsBackup      string             `json:"uploadsBackup"`
	UploadsHashes      map[string]string  `json:"uploadsHashes"`
	BusinessHashes     map[string]string  `json:"businessHashes"`
	Permissions        runtimePermissions `json:"permissions"`
	OwnedMenus         []uint             `json:"ownedMenus"`
	Failure            string             `json:"failure,omitempty"`
	UpdatedAt          time.Time          `json:"updatedAt"`
}
type runtimeUpgradeAudit struct {
	Key       string `gorm:"primaryKey"`
	PlanHash  string
	State     string
	Record    string `gorm:"type:text"`
	UpdatedAt time.Time
}

var runtimeUpgradeStepHook func(string) error

func (runtimeUpgradeAudit) TableName() string { return "sys_runtime_upgrades" }

// UpgradeRuntime requires the target application to be stopped by its caller.
// Both packages are inspected, a backup copy is migrated first, then the live DB transaction.
func UpgradeRuntime(ctx context.Context, cfg *config.Config, configPath, oldManifest, newManifest string) (result RuntimeUpgradeResult, retErr error) {
	configPath, err := canonicalRuntimeUpgradeConfig(configPath)
	if err != nil {
		return result, err
	}
	old, oldHash, next, nextHash, before, after, columns, planHash, err := runtimeUpgradeInputs(oldManifest, newManifest)
	if err != nil {
		return result, err
	}
	if err := validateRuntimeBusinessAssets(after); err != nil {
		return result, err
	}
	dbPath, uploadPath, unlock, err := lockRuntimeUpgrade(cfg)
	if err != nil {
		return result, err
	}
	defer unlock()
	journalPath := configPath + ".upgrade-" + next.ID + ".json"
	j := runtimeUpgradeJournal{RuntimeUpgradeResult: RuntimeUpgradeResult{State: "planned", FromReleaseID: old.ID, ReleaseID: next.ID, JournalPath: journalPath, BackupID: "upgrade-" + old.ID + "-" + next.ID, PlanHash: planHash}, OldManifestHash: oldHash, NewManifestHash: nextHash, DatabasePath: dbPath, UploadsPath: uploadPath}
	backupDir := filepath.Join(filepath.Dir(configPath), "backups", j.BackupID)
	j.DatabaseBackup = filepath.Join(backupDir, "database.db")
	j.UploadsBackup = filepath.Join(backupDir, "uploads")
	db, err := openUpgradeDB(cfg, dbPath)
	if err != nil {
		return j.RuntimeUpgradeResult, err
	}
	defer closeUpgradeDB(db)
	if raw, readErr := readRuntimeUpgradeJournal(journalPath); readErr == nil {
		var saved runtimeUpgradeJournal
		if json.Unmarshal(raw, &saved) != nil || !sameUpgradeIdentity(j, saved) {
			return j.RuntimeUpgradeResult, errors.New("升级日志与候选或数据库不匹配")
		}
		j = saved
	} else if !os.IsNotExist(readErr) {
		return j.RuntimeUpgradeResult, readErr
	}
	if db.Migrator().HasTable(&runtimeUpgradeAudit{}) {
		var audit runtimeUpgradeAudit
		if db.First(&audit, "key = ?", next.ID).Error == nil && audit.State == "applied" {
			var recovered runtimeUpgradeJournal
			if json.Unmarshal([]byte(audit.Record), &recovered) != nil || !sameUpgradeIdentity(j, recovered) {
				return j.RuntimeUpgradeResult, errors.New("数据库迁移审计与日志不一致")
			}
			j = recovered
		}
	}
	defer func() {
		result = j.RuntimeUpgradeResult
		if retErr != nil && j.DatabaseHash != "" {
			j.Failure = retErr.Error()
			_ = saveRuntimeUpgradeJournal(j)
		}
	}()
	if j.State == "applied" {
		if err := verifyUpgradeDatabase(db, after, next.ID, nextHash); err != nil {
			return result, err
		}
		return j.RuntimeUpgradeResult, saveRuntimeUpgradeJournal(j)
	}
	if j.State == "rolled_back" {
		return result, errors.New("此候选已回退，请创建新的升级候选")
	}
	if err := verifyUpgradeDatabase(db, before, old.ID, oldHash); err != nil {
		return result, err
	}
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		return result, err
	}
	if j.DatabaseHash == "" {
		if err := checkRuntimeBackupDirectory(filepath.Dir(configPath), j.BackupID); err != nil {
			return result, err
		}
		if err := os.MkdirAll(backupDir, 0o700); err != nil {
			return result, err
		}
		j.DatabaseBackup = filepath.Join(backupDir, "database.db")
		j.UploadsBackup = filepath.Join(backupDir, "uploads")
		if _, statErr := os.Lstat(j.DatabaseBackup); statErr == nil {
			if err := compareRuntimeDatabaseBackup(db, cfg, j.DatabaseBackup); err != nil {
				return result, fmt.Errorf("孤立备份与当前数据库不一致: %w", err)
			}
			if err := os.RemoveAll(j.UploadsBackup); err != nil {
				return result, err
			}
		} else if os.IsNotExist(statErr) {
			if err := db.Exec("VACUUM INTO ?", j.DatabaseBackup).Error; err != nil {
				return result, err
			}
		} else {
			return result, statErr
		}
		j.DatabaseHash, err = runner.FileHash(j.DatabaseBackup)
		if err != nil {
			return result, err
		}
		j.SourceDatabaseHash, err = runner.FileHash(dbPath)
		if err != nil {
			return result, err
		}
		j.UploadsHashes, err = copyUpgradeTree(uploadPath, j.UploadsBackup)
		if err != nil {
			return result, err
		}
		j.State = "backed_up"
		if err := saveRuntimeUpgradeJournal(j); err != nil {
			return result, err
		}
	} else {
		if err := verifyUpgradeBackup(j); err != nil {
			return result, err
		}
		if err := compareRuntimeDatabaseBackup(db, cfg, j.DatabaseBackup); err != nil {
			return result, errors.New("失败备份之后数据库语义已变化，拒绝沿用旧备份升级")
		}
	}
	previewPath := filepath.Join(filepath.Dir(j.DatabaseBackup), "preview.db")
	if err := copyUpgradeFile(j.DatabaseBackup, previewPath); err != nil {
		return result, err
	}
	preview, err := openUpgradeDB(cfg, previewPath)
	if err != nil {
		return result, err
	}
	previewJournal := j
	err = applyRuntimeUpgrade(ctx, preview, before, after, columns, next, nextHash, &previewJournal)
	if err == nil {
		err = verifyUpgradeDatabase(preview, after, next.ID, nextHash)
	}
	closeUpgradeDB(preview)
	if err != nil {
		return result, fmt.Errorf("升级副本预演失败: %w", err)
	}
	if runtimeUpgradeStepHook != nil {
		if err := runtimeUpgradeStepHook("preview_verified"); err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := applyRuntimeUpgrade(ctx, db, before, after, columns, next, nextHash, &j); err != nil {
		return result, err
	}
	return j.RuntimeUpgradeResult, saveRuntimeUpgradeJournal(j)
}

func runtimeUpgradeInputs(oldManifest, newManifest string) (delivery.Package, string, delivery.Package, string, service.BusinessInstallationAssets, service.BusinessInstallationAssets, []runtimeColumnAddition, string, error) {
	old, oh, err := loadRuntimePackage(oldManifest)
	if err != nil {
		return old, oh, delivery.Package{}, "", service.BusinessInstallationAssets{}, service.BusinessInstallationAssets{}, nil, "", err
	}
	next, nh, err := loadRuntimePackage(newManifest)
	var before, after service.BusinessInstallationAssets
	if err == nil && (old.ID == next.ID || old.Manifest.GOARCH != next.Manifest.GOARCH) {
		err = errors.New("升级需要相同架构的不同发布版本")
	}
	if err == nil {
		err = readUpgradeAssets(old, &before)
	}
	if err == nil {
		err = readUpgradeAssets(next, &after)
	}
	var columns []runtimeColumnAddition
	if err == nil {
		columns, err = planRuntimeColumns(before, after)
	}
	encoded, _ := json.Marshal(struct {
		Old, Next string
		Columns   []runtimeColumnAddition
	}{oh, nh, columns})
	return old, oh, next, nh, before, after, columns, byteHashRuntime(encoded), err
}
func readUpgradeAssets(pkg delivery.Package, dst *service.BusinessInstallationAssets) error {
	raw, err := os.ReadFile(filepath.Join(pkg.Path, "assets", "business.json"))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
func byteHashRuntime(raw []byte) string { return fmt.Sprintf("%x", sha256Sum(raw)) }

func planRuntimeColumns(before, after service.BusinessInstallationAssets) ([]runtimeColumnAddition, error) {
	if len(before.Objects) == 0 || len(before.Objects) != len(after.Objects) {
		return nil, errors.New("P2增量升级需保持原有对象集合")
	}
	old := map[string]service.BusinessInstallationAsset{}
	for _, asset := range before.Objects {
		old[asset.ObjectKey] = asset
	}
	var columns []runtimeColumnAddition
	for _, asset := range after.Objects {
		prior, ok := old[asset.ObjectKey]
		if !ok || !runtimeBusinessKey.MatchString(asset.TableName) || prior.TableName != asset.TableName || prior.Menu.Path != asset.Menu.Path || prior.Menu.Name != asset.Menu.Name || prior.Menu.Component != asset.Menu.Component {
			return nil, errors.New("不能删除、重命名或接管业务对象及路由")
		}
		oldFields, ok := prior.Schema["fields"].(map[string]any)
		if !ok {
			return nil, errors.New("旧字段规格无效")
		}
		newFields, ok := asset.Schema["fields"].(map[string]any)
		if !ok {
			return nil, errors.New("新字段规格无效")
		}
		for name, field := range oldFields {
			next, exists := newFields[name]
			if !exists || string(runtimeFieldContract(name, field)) != string(runtimeFieldContract(name, next)) {
				return nil, fmt.Errorf("拒绝删除、重命名、类型或约束变化: %s.%s", asset.TableName, name)
			}
		}
		for name, value := range newFields {
			if _, exists := oldFields[name]; exists {
				continue
			}
			field, ok := value.(map[string]any)
			if !ok {
				return nil, errors.New("新增字段名称或规格无效")
			}
			column, err := runtimeFieldColumn(name, field)
			if err != nil || column == "id" || column == "created_at" || column == "updated_at" || column == "deleted_at" {
				return nil, errors.New("新增数据库字段无效或占用保留列")
			}
			definition, err := runtimeColumnDefinition(name, column, field)
			if err != nil {
				return nil, err
			}
			columns = append(columns, runtimeColumnAddition{Table: asset.TableName, Field: column, Definition: definition})
		}
	}
	sort.Slice(columns, func(i, j int) bool {
		return columns[i].Table+"."+columns[i].Field < columns[j].Table+"."+columns[j].Field
	})
	return columns, nil
}
func runtimeFieldContract(name string, value any) []byte {
	raw, _ := json.Marshal(value)
	var field map[string]any
	_ = json.Unmarshal(raw, &field)
	for _, key := range []string{"title", "x-ui", "searchable", "inList", "inForm", "sort"} {
		delete(field, key)
	}
	typeName, _ := field["type"].(string)
	if meta, ok := field["x-fde"].(map[string]any); ok {
		if declared, ok := meta["type"].(string); ok && declared != "" {
			typeName = declared
		}
	}
	businessType := map[string]string{"integer": "int", "number": "float", "boolean": "bool"}[typeName]
	if businessType == "" {
		businessType = typeName
	}
	if businessType == "string" && field["format"] == "date-time" {
		businessType = "datetime"
	}
	field["businessType"] = businessType
	field["required"] = field["required"] == true
	delete(field, "x-fde")
	var original map[string]any
	_ = json.Unmarshal(raw, &original)
	column, err := runtimeFieldColumn(name, original)
	if err != nil {
		return []byte("invalid-column:" + name)
	}
	field["column"] = column
	result, _ := json.Marshal(field)
	return result
}
func runtimeColumnDefinition(name, column string, field map[string]any) (string, error) {
	storage, _ := field["type"].(string)
	typeSQL := map[string]string{"string": "TEXT", "text": "TEXT", "integer": "INTEGER", "int": "INTEGER", "number": "REAL", "float": "REAL", "boolean": "INTEGER", "bool": "INTEGER", "datetime": "DATETIME", "enum": "TEXT"}[storage]
	if typeSQL == "" {
		return "", errors.New("新增字段仅支持基础标量或枚举")
	}
	required, _ := field["required"].(bool)
	value, hasDefault := field["default"]
	if required && (!hasDefault || value == nil) {
		return "", fmt.Errorf("必填新增字段需要明确默认值回填: %s", name)
	}
	if hasDefault {
		mini, _ := json.Marshal(map[string]any{"fields": map[string]any{name: field}})
		if err := utils.ValidateBusinessValues(string(mini), map[string]any{name: value}); err != nil {
			return "", fmt.Errorf("新增字段默认值无效: %w", err)
		}
	}
	definition := typeSQL
	if required {
		definition += " NOT NULL"
	}
	if hasDefault {
		literal, err := runtimeSQLValue(value)
		if err != nil {
			return "", err
		}
		definition += " DEFAULT " + literal
	}
	if values, ok := field["enum"].([]any); ok {
		if len(values) == 0 {
			return "", errors.New("枚举值不能为空")
		}
		literals := []string{}
		for _, value := range values {
			literal, err := runtimeSQLValue(value)
			if err != nil || value == nil {
				return "", errors.New("枚举必须为非空标量")
			}
			literals = append(literals, literal)
		}
		definition += " CHECK (" + quoteRuntimeID(column) + " IS NULL OR " + quoteRuntimeID(column) + " IN (" + strings.Join(literals, ",") + "))"
	}
	return definition, nil
}
func runtimeSQLValue(value any) (string, error) {
	switch value := value.(type) {
	case nil:
		return "NULL", nil
	case string:
		return "'" + strings.ReplaceAll(value, "'", "''") + "'", nil
	case bool:
		if value {
			return "1", nil
		}
		return "0", nil
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64), nil
	case int:
		return strconv.Itoa(value), nil
	default:
		return "", errors.New("默认值必须是标量")
	}
}
func quoteRuntimeID(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }

func applyRuntimeUpgrade(ctx context.Context, db *gorm.DB, before, after service.BusinessInstallationAssets, columns []runtimeColumnAddition, next delivery.Package, nextHash string, j *runtimeUpgradeJournal) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, column := range columns {
			if err := tx.Exec("ALTER TABLE " + quoteRuntimeID(column.Table) + " ADD COLUMN " + quoteRuntimeID(column.Field) + " " + column.Definition).Error; err != nil {
				return err
			}
		}
		if runtimeUpgradeStepHook != nil {
			if err := runtimeUpgradeStepHook("transaction_after_columns"); err != nil {
				return err
			}
		}
		owned, err := updateRuntimeMetadata(tx, before, after)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.RuntimeInstallation{}).Where("release_id = ? AND manifest_hash = ? AND complete = ?", j.FromReleaseID, j.OldManifestHash, true).Updates(map[string]any{"release_id": next.ID, "manifest_hash": nextHash, "source_hash": next.Manifest.SourceHash}).Error; err != nil {
			return err
		}
		if err := verifyUpgradeDatabase(tx, after, next.ID, nextHash); err != nil {
			return err
		}
		j.OwnedMenus = owned
		j.Permissions, err = captureRuntimePermissions(tx)
		if err != nil {
			return err
		}
		j.BusinessHashes, err = runtimeBusinessHashes(tx)
		if err != nil {
			return err
		}
		j.State = "applied"
		j.Failure = ""
		return saveRuntimeUpgradeAudit(tx, *j)
	})
}

func updateRuntimeMetadata(db *gorm.DB, before, after service.BusinessInstallationAssets) ([]uint, error) {
	var owned []uint
	old := map[string]service.BusinessInstallationAsset{}
	for _, asset := range before.Objects {
		old[asset.ObjectKey] = asset
	}
	for _, asset := range after.Objects {
		var object model.BizObject
		if err := db.Where("project_id = ? AND key = ?", model.DefaultProjectID, asset.ObjectKey).First(&object).Error; err != nil {
			return nil, err
		}
		prior, _ := json.Marshal(old[asset.ObjectKey].Schema)
		actual, err := canonicalRuntimeJSON([]byte(object.Schema))
		if err != nil || string(actual) != string(prior) {
			return nil, errors.New("运行对象规格已被修改，拒绝覆盖")
		}
		schema, _ := json.Marshal(asset.Schema)
		version := object.Version + 1
		if v, ok := asset.Schema["version"].(float64); ok && int(v) > object.Version {
			version = int(v)
		}
		if err := db.Model(&object).Updates(map[string]any{"name": asset.Name, "schema": string(schema), "version": version, "active_version": version}).Error; err != nil {
			return nil, err
		}
		if err := db.Model(&model.FormSchema{}).Where("object_id = ?", object.ID).Update("published", false).Error; err != nil {
			return nil, err
		}
		if err := db.Create(&model.FormSchema{ObjectID: object.ID, Schema: string(schema), Version: version, Published: true}).Error; err != nil {
			return nil, err
		}
		var menu model.Menu
		if err := db.Where("name = ? AND type = ?", asset.Menu.Name, 1).First(&menu).Error; err != nil {
			return nil, err
		}
		if menu.Path != old[asset.ObjectKey].Menu.Path || menu.Component != old[asset.ObjectKey].Menu.Component || menu.Title != old[asset.ObjectKey].Menu.Title {
			return nil, errors.New("管理菜单存在人工修改，拒绝覆盖")
		}
		if err := db.Model(&menu).Update("title", asset.Menu.Title).Error; err != nil {
			return nil, err
		}
		owned = append(owned, menu.ID)
		var children []model.Menu
		if err := db.Where("parent_id = ?", menu.ID).Find(&children).Error; err != nil {
			return nil, err
		}
		for _, child := range children {
			owned = append(owned, child.ID)
		}
	}
	return owned, nil
}

func verifyUpgradeDatabase(db *gorm.DB, assets service.BusinessInstallationAssets, releaseID, manifestHash string) error {
	var markers []model.RuntimeInstallation
	if err := db.Find(&markers).Error; err != nil {
		return err
	}
	if len(markers) != 1 || !markers[0].Complete || markers[0].ReleaseID != releaseID || markers[0].ManifestHash != manifestHash {
		return errors.New("数据库安装版本与升级基线不匹配")
	}
	for _, asset := range assets.Objects {
		if !db.Migrator().HasTable(asset.TableName) {
			return errors.New("缺少原业务表")
		}
		fields, ok := asset.Schema["fields"].(map[string]any)
		if !ok {
			return errors.New("业务字段规格无效")
		}
		for name, value := range fields {
			field, ok := value.(map[string]any)
			if !ok {
				return errors.New("业务字段规格无效")
			}
			column, err := runtimeFieldColumn(name, field)
			if err != nil {
				return err
			}
			if !db.Migrator().HasColumn(asset.TableName, column) {
				return fmt.Errorf("数据库缺少字段 %s.%s", asset.TableName, column)
			}
		}
	}
	return nil
}

// Existing packages may omit x-fde.column. Their JSON names use the generator's
// lower-camel convention, whose inverse is one underscore before each capital.
func runtimeFieldColumn(name string, field map[string]any) (string, error) {
	column := ""
	if metadata, ok := field["x-fde"].(map[string]any); ok {
		if value, exists := metadata["column"]; exists {
			var valid bool
			column, valid = value.(string)
			if !valid || column == "" {
				return "", errors.New("x-fde.column必须为明确列名")
			}
		}
	}
	if column == "" {
		var builder strings.Builder
		for index, character := range name {
			if character >= 'A' && character <= 'Z' {
				if index > 0 {
					builder.WriteByte('_')
				}
				builder.WriteRune(character - 'A' + 'a')
			} else {
				builder.WriteRune(character)
			}
		}
		column = builder.String()
	}
	if !runtimeBusinessPath.MatchString(column) {
		return "", errors.New("数据库列名不合法")
	}
	return column, nil
}

func lockRuntimeUpgrade(cfg *config.Config) (string, string, func(), error) {
	if cfg == nil || cfg.DB.Driver != "sqlite" {
		return "", "", nil, errors.New("增量交付仅支持SQLite")
	}
	dbPath, err := canonicalRuntimeSQLitePath(cfg.DB.DSN)
	if err != nil {
		return "", "", nil, err
	}
	if _, err := os.Stat(dbPath); err != nil {
		return "", "", nil, errors.New("升级数据库不存在")
	}
	upload, err := filepath.Abs(cfg.Upload.Dir)
	if err != nil || cfg.Upload.Dir == "" {
		return "", "", nil, errors.New("升级需要明确uploads目录")
	}
	if err := os.MkdirAll(upload, 0o700); err != nil {
		return "", "", nil, err
	}
	if info, err := os.Lstat(upload); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", nil, errors.New("uploads必须为真实目录")
	}
	unlock, err := lockRuntimeBootstrap(dbPath + ".maintenance")
	return dbPath, upload, unlock, err
}
func openUpgradeDB(cfg *config.Config, path string) (*gorm.DB, error) {
	copy := *cfg
	copy.DB.DSN = path
	db, err := openDatabase(&copy)
	if err == nil {
		if raw, rawErr := db.DB(); rawErr == nil {
			raw.SetMaxOpenConns(1)
		}
	}
	return db, err
}
func closeUpgradeDB(db *gorm.DB) {
	if raw, err := db.DB(); err == nil {
		_ = raw.Close()
	}
}
func sameUpgradeIdentity(a, b runtimeUpgradeJournal) bool {
	return a.FromReleaseID == b.FromReleaseID && a.ReleaseID == b.ReleaseID && a.OldManifestHash == b.OldManifestHash && a.NewManifestHash == b.NewManifestHash && a.PlanHash == b.PlanHash && a.DatabasePath == b.DatabasePath && a.UploadsPath == b.UploadsPath && a.DatabaseBackup == b.DatabaseBackup && a.UploadsBackup == b.UploadsBackup && a.BackupID == b.BackupID && a.JournalPath == b.JournalPath
}
func saveRuntimeUpgradeJournal(j runtimeUpgradeJournal) error {
	j.UpdatedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return atomicRuntimeFile(j.JournalPath, raw)
}
func atomicRuntimeFile(path string, raw []byte) error {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("日志目标必须为普通文件")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".runtime-upgrade-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
func saveRuntimeUpgradeAudit(db *gorm.DB, j runtimeUpgradeJournal) error {
	if err := db.AutoMigrate(&runtimeUpgradeAudit{}); err != nil {
		return err
	}
	raw, _ := json.Marshal(j)
	return db.Save(&runtimeUpgradeAudit{Key: j.ReleaseID, PlanHash: j.PlanHash, State: j.State, Record: string(raw), UpdatedAt: time.Now().UTC()}).Error
}
func copyUpgradeFile(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("备份源不能是符号链接或特殊文件")
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if destinationInfo, statErr := os.Lstat(destination); statErr == nil && (!destinationInfo.Mode().IsRegular() || destinationInfo.Mode()&os.ModeSymlink != 0) {
		return errors.New("备份目标不能是符号链接或特殊文件")
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	return errors.Join(copyErr, syncErr, closeErr)
}
func copyUpgradeTree(source, destination string) (map[string]string, error) {
	files, err := runner.HashTree(source)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return nil, err
	}
	for path := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(destination, path)), 0o700); err != nil {
			return nil, err
		}
		if err := copyUpgradeFile(filepath.Join(source, path), filepath.Join(destination, path)); err != nil {
			return nil, err
		}
	}
	after, err := runner.HashTree(source)
	if err != nil || !sameRuntimeHashes(files, after) {
		return nil, errors.New("备份期间上传目录发生变化")
	}
	return files, nil
}
func sameRuntimeHashes(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}
func verifyUpgradeBackup(j runtimeUpgradeJournal) error {
	info, err := os.Lstat(j.DatabaseBackup)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("数据库备份不是普通文件")
	}
	uploadInfo, err := os.Lstat(j.UploadsBackup)
	if err != nil || !uploadInfo.IsDir() || uploadInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("上传备份不是真实目录")
	}
	hash, err := runner.FileHash(j.DatabaseBackup)
	if err != nil || hash != j.DatabaseHash {
		return errors.New("数据库备份校验失败")
	}
	files, err := runner.HashTree(j.UploadsBackup)
	if err != nil || !sameRuntimeHashes(files, j.UploadsHashes) {
		return errors.New("上传备份校验失败")
	}
	return nil
}

func canonicalRuntimeUpgradeConfig(path string) (string, error) {
	if path == "" {
		return "", errors.New("升级需要明确运行配置路径")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(absolute); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return "", errors.New("运行配置不能是符号链接或特殊文件")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(absolute)), nil
}
func readRuntimeUpgradeJournal(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("升级日志必须是普通文件")
	}
	return os.ReadFile(path)
}
func checkRuntimeBackupDirectory(base, id string) error {
	for _, path := range []string{filepath.Join(base, "backups"), filepath.Join(base, "backups", id)} {
		if info, err := os.Lstat(path); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return errors.New("备份目录不能是符号链接或特殊文件")
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
