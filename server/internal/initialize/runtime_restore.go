package initialize

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"fde-admin/internal/config"
	"fde-admin/internal/model"
	"fde-admin/internal/runner"
	"gorm.io/gorm"
)

func sha256Sum(raw []byte) [32]byte { return sha256.Sum256(raw) }

func captureRuntimePermissions(db *gorm.DB) (runtimePermissions, error) {
	var p runtimePermissions
	if err := db.Order("id").Find(&p.Menus).Error; err != nil {
		return p, err
	}
	if err := db.Table("role_menus").Order("role_id,menu_id").Find(&p.RoleMenus).Error; err != nil {
		return p, err
	}
	if err := db.Table("user_roles").Order("user_id,role_id").Find(&p.UserRoles).Error; err != nil {
		return p, err
	}
	users, err := runtimeTableHash(db, "sys_users", true)
	if err != nil {
		return p, err
	}
	roles, err := runtimeTableHash(db, "sys_roles", true)
	if err != nil {
		return p, err
	}
	p.PrincipalsHash = byteHashRuntime([]byte(users + roles))
	return p, nil
}

func runtimeTableHash(db *gorm.DB, table string, skipTimestamps bool) (string, error) {
	rows, err := db.Raw("SELECT * FROM " + quoteRuntimeID(table)).Rows()
	if err != nil {
		return "", err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	var records []string
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return "", err
		}
		record := map[string]any{}
		for i, name := range columns {
			if skipTimestamps && (name == "created_at" || name == "updated_at") {
				continue
			}
			value := values[i]
			if b, ok := value.([]byte); ok {
				value = string(b)
			}
			record[name] = value
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return "", err
		}
		records = append(records, string(raw))
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	sort.Strings(records)
	raw, _ := json.Marshal(records)
	return byteHashRuntime(raw), nil
}

func runtimeBusinessHashes(db *gorm.DB) (map[string]string, error) {
	ignored := map[string]bool{}
	for _, table := range []string{"sys_users", "sys_roles", "sys_menus", "role_menus", "user_roles", "casbin_rule", "sys_runtime_installations", "sys_runtime_upgrades", "sys_biz_objects", "sys_biz_fields", "sys_form_schemas", "sys_refresh_tokens", "sys_active_tokens", "sys_jwt_blacklists", "sys_operation_logs"} {
		ignored[table] = true
	}
	var tables []string
	if err := db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name").Scan(&tables).Error; err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, table := range tables {
		if ignored[table] {
			continue
		}
		hash, err := runtimeTableHash(db, table, false)
		if err != nil {
			return nil, err
		}
		result[table] = hash
	}
	return result, nil
}

func compareRuntimeDatabaseBackup(current *gorm.DB, cfg *config.Config, backupPath string) error {
	info, err := os.Lstat(backupPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("备份必须是普通数据库文件")
	}
	backup, err := openUpgradeDB(cfg, backupPath)
	if err != nil {
		return err
	}
	defer closeUpgradeDB(backup)
	snapshot := func(db *gorm.DB) (map[string]string, error) {
		var tables []string
		if err := db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name").Scan(&tables).Error; err != nil {
			return nil, err
		}
		hashes := map[string]string{}
		for _, table := range tables {
			hash, err := runtimeTableHash(db, table, false)
			if err != nil {
				return nil, err
			}
			hashes[table] = hash
		}
		return hashes, nil
	}
	want, err := snapshot(backup)
	if err != nil {
		return err
	}
	got, err := snapshot(current)
	if err != nil {
		return err
	}
	if !sameRuntimeHashes(want, got) {
		return errors.New("数据或权限与升级前备份不同")
	}
	return nil
}

// RollbackRuntime restores a verified DB and upload snapshot. New business writes
// block recovery. Unrelated grants between unchanged principals are reconciled.
func RollbackRuntime(ctx context.Context, cfg *config.Config, configPath, oldManifest, newManifest string) (RuntimeUpgradeResult, error) {
	configPath, err := canonicalRuntimeUpgradeConfig(configPath)
	if err != nil {
		return RuntimeUpgradeResult{}, err
	}
	old, oldHash, next, newHash, before, _, _, planHash, err := runtimeUpgradeInputs(oldManifest, newManifest)
	if err != nil {
		return RuntimeUpgradeResult{}, err
	}
	dbPath, uploadPath, unlock, err := lockRuntimeUpgrade(cfg)
	if err != nil {
		return RuntimeUpgradeResult{}, err
	}
	defer unlock()
	path := configPath + ".upgrade-" + next.ID + ".json"
	raw, err := readRuntimeUpgradeJournal(path)
	if err != nil {
		return RuntimeUpgradeResult{}, err
	}
	var j runtimeUpgradeJournal
	if json.Unmarshal(raw, &j) != nil || j.FromReleaseID != old.ID || j.ReleaseID != next.ID || j.PlanHash != planHash || j.OldManifestHash != oldHash || j.NewManifestHash != newHash || j.DatabasePath != dbPath || j.UploadsPath != uploadPath {
		return RuntimeUpgradeResult{}, errors.New("恢复日志与发布/目标不一致")
	}
	expectedDir := filepath.Join(filepath.Dir(configPath), "backups", "upgrade-"+old.ID+"-"+next.ID)
	if j.JournalPath != path || j.DatabaseBackup != filepath.Join(expectedDir, "database.db") || j.UploadsBackup != filepath.Join(expectedDir, "uploads") {
		return j.RuntimeUpgradeResult, errors.New("备份路径与本目标不一致")
	}
	db, err := openUpgradeDB(cfg, dbPath)
	if err != nil {
		return j.RuntimeUpgradeResult, err
	}
	defer closeUpgradeDB(db)
	if verifyUpgradeDatabase(db, before, old.ID, oldHash) == nil {
		j.State = "rolled_back"
		j.Failure = ""
		return j.RuntimeUpgradeResult, saveRuntimeUpgradeJournal(j)
	}
	if db.Migrator().HasTable(&runtimeUpgradeAudit{}) {
		var audit runtimeUpgradeAudit
		if db.First(&audit, "key = ?", next.ID).Error == nil && audit.State == "applied" {
			var recovered runtimeUpgradeJournal
			if json.Unmarshal([]byte(audit.Record), &recovered) != nil || !sameUpgradeIdentity(j, recovered) {
				return j.RuntimeUpgradeResult, errors.New("恢复审计不匹配")
			}
			j = recovered
		}
	}
	if j.State != "applied" {
		return j.RuntimeUpgradeResult, errors.New("没有已完成的升级可以恢复")
	}
	if err := verifyUpgradeBackup(j); err != nil {
		return j.RuntimeUpgradeResult, err
	}
	if err := verifyRollbackData(db, j); err != nil {
		return j.RuntimeUpgradeResult, err
	}
	current, err := captureRuntimePermissions(db)
	if err != nil {
		return j.RuntimeUpgradeResult, err
	}
	if err := checkRuntimePermissionConflicts(current, j.Permissions, j.OwnedMenus); err != nil {
		return j.RuntimeUpgradeResult, err
	}
	stage, err := os.CreateTemp(filepath.Dir(dbPath), ".runtime-restore-*.db")
	if err != nil {
		return j.RuntimeUpgradeResult, err
	}
	stagePath := stage.Name()
	stage.Close()
	defer os.Remove(stagePath)
	if err := copyUpgradeFile(j.DatabaseBackup, stagePath); err != nil {
		return j.RuntimeUpgradeResult, err
	}
	restored, err := openUpgradeDB(cfg, stagePath)
	if err != nil {
		return j.RuntimeUpgradeResult, err
	}
	err = restored.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return mergeRuntimePermissions(tx, current, j.OwnedMenus) })
	if err == nil {
		err = copyRuntimeSessions(restored, dbPath)
	}
	if err == nil {
		err = verifyUpgradeDatabase(restored, before, old.ID, oldHash)
	}
	if err == nil {
		j.State = "rolled_back"
		j.Failure = ""
		err = saveRuntimeUpgradeAudit(restored, j)
	}
	closeUpgradeDB(restored)
	if err != nil {
		return j.RuntimeUpgradeResult, err
	}
	uploadStage, err := os.MkdirTemp(filepath.Dir(uploadPath), ".runtime-uploads-restore-")
	if err != nil {
		return j.RuntimeUpgradeResult, err
	}
	defer os.RemoveAll(uploadStage)
	if _, err := copyUpgradeTree(j.UploadsBackup, uploadStage); err != nil {
		return j.RuntimeUpgradeResult, err
	}
	if err := verifyRollbackData(db, j); err != nil {
		return j.RuntimeUpgradeResult, err
	}
	latest, err := captureRuntimePermissions(db)
	if err != nil {
		return j.RuntimeUpgradeResult, err
	}
	if string(mustRuntimeJSON(latest)) != string(mustRuntimeJSON(current)) {
		return j.RuntimeUpgradeResult, errors.New("恢复准备期间权限已变化")
	}
	if err := ctx.Err(); err != nil {
		return j.RuntimeUpgradeResult, err
	}
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		return j.RuntimeUpgradeResult, err
	}
	closeUpgradeDB(db)
	if err := swapRuntimeRecovery(dbPath, stagePath, uploadPath, uploadStage); err != nil {
		return j.RuntimeUpgradeResult, err
	}
	return j.RuntimeUpgradeResult, saveRuntimeUpgradeJournal(j)
}

func verifyRollbackData(db *gorm.DB, j runtimeUpgradeJournal) error {
	hashes, err := runtimeBusinessHashes(db)
	if err != nil {
		return err
	}
	if !sameRuntimeHashes(hashes, j.BusinessHashes) {
		return errors.New("升级后已有业务数据新增或修改，拒绝快照恢复；需要专门数据恢复方案")
	}
	files, err := runner.HashTree(j.UploadsPath)
	if err != nil {
		return err
	}
	if !sameRuntimeHashes(files, j.UploadsHashes) {
		return errors.New("升级后上传文件发生变化，拒绝丢弃文件恢复")
	}
	return nil
}
func mustRuntimeJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }
func runtimeMenuContract(menu model.Menu) string {
	var value map[string]any
	_ = json.Unmarshal(mustRuntimeJSON(menu), &value)
	delete(value, "createdAt")
	delete(value, "updatedAt")
	delete(value, "children")
	return string(mustRuntimeJSON(value))
}
func checkRuntimePermissionConflicts(current, after runtimePermissions, owned []uint) error {
	if current.PrincipalsHash != after.PrincipalsHash {
		return errors.New("用户或角色主体已变化，恢复需人工对账")
	}
	ids := map[uint]bool{}
	for _, id := range owned {
		ids[id] = true
	}
	old := map[uint]string{}
	for _, menu := range after.Menus {
		if ids[menu.ID] {
			old[menu.ID] = runtimeMenuContract(menu)
		}
	}
	seen := map[uint]bool{}
	for _, menu := range current.Menus {
		if ids[menu.ID] {
			if old[menu.ID] != runtimeMenuContract(menu) {
				return errors.New("本次管理菜单存在人工修改，恢复冲突")
			}
			seen[menu.ID] = true
		}
		if ids[menu.ParentID] && !ids[menu.ID] {
			return errors.New("本次管理菜单新增了人工子权限，恢复冲突")
		}
	}
	if len(seen) != len(old) {
		return errors.New("本次管理菜单已被删除，恢复冲突")
	}
	filter := func(grants []runtimeGrant) string {
		var selected []runtimeGrant
		for _, grant := range grants {
			if ids[grant.MenuID] {
				selected = append(selected, grant)
			}
		}
		return string(mustRuntimeJSON(selected))
	}
	if filter(current.RoleMenus) != filter(after.RoleMenus) {
		return errors.New("本次管理权限已被人工调整，恢复冲突")
	}
	return nil
}

func mergeRuntimePermissions(db *gorm.DB, current runtimePermissions, owned []uint) error {
	ids := map[uint]bool{}
	for _, id := range owned {
		ids[id] = true
	}
	if err := db.Where("id NOT IN ?", owned).Delete(&model.Menu{}).Error; err != nil {
		return err
	}
	for _, menu := range current.Menus {
		if !ids[menu.ID] {
			if err := db.Create(&menu).Error; err != nil {
				return err
			}
		}
	}
	if err := db.Exec("DELETE FROM role_menus WHERE menu_id NOT IN ?", owned).Error; err != nil {
		return err
	}
	for _, grant := range current.RoleMenus {
		if !ids[grant.MenuID] {
			if err := db.Exec("INSERT INTO role_menus(role_id,menu_id) VALUES(?,?)", grant.RoleID, grant.MenuID).Error; err != nil {
				return err
			}
		}
	}
	if err := db.Exec("DELETE FROM user_roles").Error; err != nil {
		return err
	}
	for _, grant := range current.UserRoles {
		if err := db.Exec("INSERT INTO user_roles(user_id,role_id) VALUES(?,?)", grant.UserID, grant.RoleID).Error; err != nil {
			return err
		}
	}
	return rebuildRuntimePolicies(db)
}

func rebuildRuntimePolicies(db *gorm.DB) error {
	if !db.Migrator().HasTable("casbin_rule") {
		return errors.New("缺少权限策略表")
	}
	if err := db.Exec("DELETE FROM casbin_rule").Error; err != nil {
		return err
	}
	var roles []model.Role
	if err := db.Preload("Menus").Find(&roles).Error; err != nil {
		return err
	}
	seen := map[string]bool{}
	insert := func(kind, v0, v1, v2 string) error {
		key := strings.Join([]string{kind, v0, v1, v2}, "\x00")
		if seen[key] {
			return nil
		}
		seen[key] = true
		return db.Exec("INSERT INTO casbin_rule(ptype,v0,v1,v2,v3,v4,v5) VALUES(?,?,?,?,?,?,?)", kind, v0, v1, v2, "", "", "").Error
	}
	for _, role := range roles {
		roleID := strconv.Itoa(int(role.ID))
		if role.Code == "admin" {
			if err := insert("p", roleID, "/*", "*"); err != nil {
				return err
			}
			continue
		}
		for _, menu := range role.Menus {
			if menu.ApiPath == "" {
				continue
			}
			method := menu.ApiMethod
			if method == "" {
				method = "*"
			}
			if err := insert("p", roleID, menu.ApiPath, method); err != nil {
				return err
			}
		}
	}
	var users []model.User
	if err := db.Preload("Roles").Find(&users).Error; err != nil {
		return err
	}
	for _, user := range users {
		for _, role := range user.Roles {
			if err := insert("g", strconv.Itoa(int(user.ID)), strconv.Itoa(int(role.ID)), ""); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyRuntimeSessions(db *gorm.DB, currentPath string) error {
	if err := db.Exec("ATTACH DATABASE ? AS runtime_current", currentPath).Error; err != nil {
		return err
	}
	defer db.Exec("DETACH DATABASE runtime_current")
	return db.Transaction(func(tx *gorm.DB) error {
		for _, table := range []string{"sys_refresh_tokens", "sys_active_tokens", "sys_jwt_blacklists", "sys_operation_logs"} {
			if !tx.Migrator().HasTable(table) {
				continue
			}
			if err := tx.Exec("DELETE FROM main." + quoteRuntimeID(table)).Error; err != nil {
				return err
			}
			if err := tx.Exec("INSERT INTO main." + quoteRuntimeID(table) + " SELECT * FROM runtime_current." + quoteRuntimeID(table)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func swapRuntimeRecovery(database, stageDB, uploads, stageUploads string) error {
	oldDB := database + ".restore-hold"
	oldUploads := uploads + ".restore-hold"
	for _, path := range []string{oldDB, oldUploads} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return errors.New("存在未完成恢复保留文件，需人工对账")
		}
	}
	if err := os.Rename(database, oldDB); err != nil {
		return err
	}
	if err := os.Rename(stageDB, database); err != nil {
		return errors.Join(err, os.Rename(oldDB, database))
	}
	if err := os.Rename(uploads, oldUploads); err != nil {
		return errors.Join(err, os.Rename(database, stageDB), os.Rename(oldDB, database))
	}
	if err := os.Rename(stageUploads, uploads); err != nil {
		return errors.Join(err, os.Rename(oldUploads, uploads), os.Rename(database, stageDB), os.Rename(oldDB, database))
	}
	return errors.Join(os.Remove(oldDB), os.RemoveAll(oldUploads))
}
