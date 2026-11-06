package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"fde-admin/internal/codegen"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

// ---- 设计文档 6.2 / 6.3 入参与返回结构 ----

// BusinessCodeField generate_business_code 的字段入参
type BusinessCodeField struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Type     string `json:"type"`
	Search   bool   `json:"search"`
	InList   bool   `json:"inList"`
	InForm   bool   `json:"inForm"`
	Required bool   `json:"required"`
}

// BusinessCodeInput generate_business_code 工具入参
type BusinessCodeInput struct {
	ObjectKey    string              `json:"objectKey"`
	Business     string              `json:"business"`
	TableName    string              `json:"tableName"`
	StructName   string              `json:"structName"`
	ParentMenuID uint                `json:"parentMenuId"`
	Fields       []BusinessCodeField `json:"fields"`
	DryRun       *bool               `json:"dryRun"`
}

// CodegenDiff dry-run 差异统计
type CodegenDiff struct {
	Added     int      `json:"added"`
	Modified  int      `json:"modified"`
	Conflicts []string `json:"conflicts"`
}

// CodegenMenuPlanItem 菜单计划中的菜单项
type CodegenMenuPlanItem struct {
	Path      string `json:"path"`
	Title     string `json:"title"`
	Name      string `json:"name"`
	Component string `json:"component"`
}

// CodegenMenuPlan 菜单计划
type CodegenMenuPlan struct {
	Menu         CodegenMenuPlanItem `json:"menu"`
	Buttons      int                 `json:"buttons"`
	ParentMenuID uint                `json:"parentMenuId"`
	TableName    string              `json:"tableName"`
}

// BusinessCodeResult generate_business_code 返回结构(设计文档 6.2)
type BusinessCodeResult struct {
	OK              bool                 `json:"ok"`
	BatchID         string               `json:"batchId,omitempty"`
	Files           []codegen.FileResult `json:"files,omitempty"`
	Diff            *CodegenDiff         `json:"diff,omitempty"`
	MenuPlan        *CodegenMenuPlan     `json:"menuPlan,omitempty"`
	RestartRequired bool                 `json:"restartRequired"`
	Warnings        []string             `json:"warnings,omitempty"`
	Error           *FormgenError        `json:"error,omitempty"`
}

// ApplyFilesInput apply_files 工具入参(设计文档 6.3)
type ApplyFilesInput struct {
	BatchID      string               `json:"batchId"`
	ObjectKey    string               `json:"objectKey"`
	Files        []codegen.FileResult `json:"files"`
	ApplyMenu    *bool                `json:"applyMenu"`
	ParentMenuID *uint                `json:"parentMenuId"`
	Snapshot     *bool                `json:"snapshot"`
	Approve      *bool                `json:"approve"`
}

// ApplyFilesResult apply_files 返回结构(设计文档 6.3)
type ApplyFilesResult struct {
	OK            bool             `json:"ok"`
	Applied       []map[string]any `json:"applied,omitempty"`
	MenuID        uint             `json:"menuId"`
	CasbinRebuilt bool             `json:"casbinRebuilt"`
	Rollback      map[string]any   `json:"rollback,omitempty"`
	Error         *FormgenError    `json:"error,omitempty"`
}

// ObjectToGenRequest 把 sys_biz_fields 投影为 codegen.GenField:
// Type 取 string/text/int/float/bool/datetime 子集, enum/relation 跳过并记 warning
func ObjectToGenRequest(objectKey string) ([]codegen.GenField, []string, error) {
	_, fields, err := GetBizObjectByKey(objectKey)
	if err != nil {
		return nil, nil, fmt.Errorf("业务对象不存在: %s", objectKey)
	}
	gfs := make([]codegen.GenField, 0, len(fields))
	warnings := make([]string, 0)
	for _, f := range fields {
		if f.Type == "enum" || f.Type == "relation" {
			warnings = append(warnings, fmt.Sprintf("%s 字段不参与代码生成: %s", f.Type, f.Name))
			continue
		}
		gfs = append(gfs, codegen.GenField{
			Name:     f.Name,
			Title:    f.Title,
			Type:     f.Type,
			Search:   f.Searchable,
			InList:   f.InList,
			InForm:   f.InForm,
			Required: f.Required,
		})
	}
	return gfs, warnings, nil
}

// GenerateBusinessCode generate_business_code 工具执行函数(AiToolFunc 签名)
func GenerateBusinessCode(ctx context.Context, args string) (any, error) {
	return generateBusinessCode(args, 0)
}

// CodegenBusinessPreview HTTP /api/codegen/preview 带 objectKey 时复用工具实现
func CodegenBusinessPreview(req codegen.GenRequest, userID uint) (*BusinessCodeResult, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return generateBusinessCode(string(b), userID)
}

// ApplyFiles apply_files 工具执行函数(AiToolFunc 签名)
func ApplyFiles(ctx context.Context, args string) (any, error) {
	var in ApplyFilesInput
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return applyFilesFail("FORM_INVALID_SCHEMA", "参数不是合法 JSON: "+err.Error()), nil
	}
	return applyFilesCore(in, 0)
}

// ApplyFormgenFiles HTTP /api/formgen/apply 与工具共用同一实现
func ApplyFormgenFiles(in ApplyFilesInput, userID uint) (*ApplyFilesResult, error) {
	return applyFilesCore(in, userID)
}

func businessCodeFail(errCode, msg string) *BusinessCodeResult {
	return &BusinessCodeResult{OK: false, Error: &FormgenError{Code: 40001, ErrCode: errCode, Msg: msg}}
}

func applyFilesFail(errCode, msg string) *ApplyFilesResult {
	return &ApplyFilesResult{OK: false, Error: &FormgenError{Code: 40001, ErrCode: errCode, Msg: msg}}
}

func applyFilesFailWithRollback(errCode, msg string, rollback map[string]any) *ApplyFilesResult {
	return &ApplyFilesResult{OK: false, Error: &FormgenError{Code: 40001, ErrCode: errCode, Msg: msg}, Rollback: rollback}
}

// generateBusinessCode 解析工具入参并执行 dry-run(缺省)/apply(dryRun=false)
func generateBusinessCode(args string, userID uint) (*BusinessCodeResult, error) {
	var in BusinessCodeInput
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return businessCodeFail("FORM_INVALID_SCHEMA", "参数不是合法 JSON: "+err.Error()), nil
	}
	dryRun := true
	if in.DryRun != nil {
		dryRun = *in.DryRun
	}

	var req codegen.GenRequest
	warnings := make([]string, 0)
	if strings.TrimSpace(in.ObjectKey) != "" {
		obj, _, err := GetBizObjectByKey(in.ObjectKey)
		if err != nil {
			return businessCodeFail("FORM_INVALID_SCHEMA", "业务对象不存在: "+in.ObjectKey), nil
		}
		gfs, warns, err := ObjectToGenRequest(in.ObjectKey)
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, warns...)
		tableName := strings.TrimSpace(obj.Table)
		if tableName == "" {
			tableName = obj.Key
		}
		req = codegen.GenRequest{
			TableName:    tableName,
			Business:     obj.Name,
			StructName:   in.StructName,
			ParentMenuID: in.ParentMenuID,
			Fields:       gfs,
		}
	} else {
		gfs := make([]codegen.GenField, 0, len(in.Fields))
		for _, f := range in.Fields {
			gfs = append(gfs, codegen.GenField{
				Name: f.Name, Title: f.Title, Type: f.Type,
				Search: f.Search, InList: f.InList, InForm: f.InForm, Required: f.Required,
			})
		}
		req = codegen.GenRequest{
			TableName: in.TableName, Business: in.Business, StructName: in.StructName,
			ParentMenuID: in.ParentMenuID, Fields: gfs,
		}
	}

	d, err := codegen.Prepare(req)
	if err != nil {
		return businessCodeFail("FORM_INVALID_SCHEMA", err.Error()), nil
	}
	if global.DB.Migrator().HasTable(req.TableName) {
		return businessCodeFail("CODE_CONFLICT", "数据表已存在: "+req.TableName), nil
	}
	var menuCount int64
	if err := global.DB.Model(&model.Menu{}).Where("path = ? AND name = ?", d.Snake, d.StructName).Count(&menuCount).Error; err != nil {
		return nil, err
	}
	if menuCount > 0 {
		return businessCodeFail("CODE_CONFLICT", "同名菜单已存在: "+d.Snake), nil
	}

	files, err := codegen.Render(d)
	if err != nil {
		return businessCodeFail("FORM_INVALID_SCHEMA", err.Error()), nil
	}

	diff := computeGenDiff(files)
	menuPlan := &CodegenMenuPlan{
		Menu:         CodegenMenuPlanItem{Path: d.Snake, Title: d.Business, Name: d.StructName, Component: "generated/" + d.Snake + "/index"},
		Buttons:      4,
		ParentMenuID: d.ParentMenuID,
		TableName:    req.TableName,
	}
	batchID := uuid.NewString()
	res := &BusinessCodeResult{
		OK: true, BatchID: batchID, Files: files, Diff: diff, MenuPlan: menuPlan,
		RestartRequired: true, Warnings: warnings,
	}

	if err := persistDryRunAudit(batchID, in.ObjectKey, args, res, userID); err != nil {
		return nil, err
	}

	if !dryRun {
		approve := true
		applyRes, err := applyFilesCore(ApplyFilesInput{BatchID: batchID, ObjectKey: in.ObjectKey, Approve: &approve}, userID)
		if err != nil {
			return nil, err
		}
		if !applyRes.OK {
			res = &BusinessCodeResult{
				OK: false, BatchID: batchID, Files: files, Diff: diff, MenuPlan: menuPlan,
				RestartRequired: true, Warnings: warnings, Error: applyRes.Error,
			}
		}
	}
	return res, nil
}

func persistDryRunAudit(batchID, objectKey, args string, res *BusinessCodeResult, userID uint) error {
	resultJSON, err := json.Marshal(res)
	if err != nil {
		return err
	}
	filesJSON, err := json.Marshal(res.Files)
	if err != nil {
		return err
	}
	audit := model.GenAudit{
		UserID:    userID,
		Tool:      "generate_business_code",
		Action:    "dry_run",
		ObjectKey: objectKey,
		BatchID:   batchID,
		Status:    "pending",
		ArgsHash:  sha256Hex(args),
		Result:    string(resultJSON),
		Files:     string(filesJSON),
	}
	return global.DB.Create(&audit).Error
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// computeGenDiff 对比磁盘既有文件, 统计新增/修改/冲突
func computeGenDiff(files []codegen.FileResult) *CodegenDiff {
	diff := &CodegenDiff{Conflicts: []string{}}
	for _, f := range files {
		full, err := resolveGenFilePath(f.Path)
		if err != nil {
			diff.Conflicts = append(diff.Conflicts, f.Path)
			continue
		}
		_, statErr := os.Stat(full)
		if errors.Is(statErr, os.ErrNotExist) {
			diff.Added++
			continue
		}
		if statErr != nil {
			diff.Conflicts = append(diff.Conflicts, f.Path)
			continue
		}
		modified, err := codegen.Diff(full, f.Content)
		if err != nil {
			diff.Conflicts = append(diff.Conflicts, f.Path)
			continue
		}
		if modified {
			diff.Modified++
			diff.Conflicts = append(diff.Conflicts, f.Path)
		}
	}
	return diff
}

// applyFilesCore 两段式事务: 校验批次 -> 快照 -> 写盘 -> 菜单授权 -> casbin 重建 -> 审计
func applyFilesCore(in ApplyFilesInput, userID uint) (*ApplyFilesResult, error) {
	if strings.TrimSpace(in.BatchID) == "" {
		return applyFilesFail("BATCH_STALE", "缺少 batchId"), nil
	}
	if in.Approve == nil || !*in.Approve {
		return applyFilesFail("BATCH_STALE", "approve 必须显式为 true 才执行写盘"), nil
	}
	var audit model.GenAudit
	if err := global.DB.Where("batch_id = ?", in.BatchID).First(&audit).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return applyFilesFail("BATCH_STALE", "批次不存在: "+in.BatchID), nil
		}
		return nil, err
	}
	if audit.Status != "pending" {
		return applyFilesFail("BATCH_STALE", "批次已处理: "+audit.Status), nil
	}

	var dry BusinessCodeResult
	if err := json.Unmarshal([]byte(audit.Result), &dry); err != nil {
		return applyFilesFail("BATCH_STALE", "批次结果解析失败: "+err.Error()), nil
	}
	files := dry.Files
	if len(files) == 0 && strings.TrimSpace(audit.Files) != "" {
		if err := json.Unmarshal([]byte(audit.Files), &files); err != nil {
			return applyFilesFail("BATCH_STALE", "批次文件清单解析失败"), nil
		}
	}
	if len(files) == 0 {
		return applyFilesFail("BATCH_STALE", "批次缺少文件清单"), nil
	}
	for _, f := range files {
		if err := validateGenFilePath(f.Path); err != nil {
			return applyFilesFail("CODE_CONFLICT", err.Error()), nil
		}
	}

	// apply 阶段再查一次表/菜单冲突, 防止 dry-run 后并发变化
	if dry.MenuPlan != nil {
		if dry.MenuPlan.TableName != "" && global.DB.Migrator().HasTable(dry.MenuPlan.TableName) {
			return applyFilesFail("CODE_CONFLICT", "数据表已存在: "+dry.MenuPlan.TableName), nil
		}
		var menuCount int64
		if err := global.DB.Model(&model.Menu{}).Where("path = ? AND name = ?", dry.MenuPlan.Menu.Path, dry.MenuPlan.Menu.Name).Count(&menuCount).Error; err != nil {
			return nil, err
		}
		if menuCount > 0 {
			return applyFilesFail("CODE_CONFLICT", "同名菜单已存在: "+dry.MenuPlan.Menu.Path), nil
		}
	}

	applyMenu := true
	if in.ApplyMenu != nil {
		applyMenu = *in.ApplyMenu
	}
	doSnapshot := true
	if in.Snapshot != nil {
		doSnapshot = *in.Snapshot
	}
	parentMenuID := uint(0)
	if dry.MenuPlan != nil {
		parentMenuID = dry.MenuPlan.ParentMenuID
	}
	if in.ParentMenuID != nil {
		parentMenuID = *in.ParentMenuID
	}

	var snap *genSnapshot
	snapshotID := ""
	if doSnapshot {
		s, err := takeGenSnapshot(in.BatchID, files)
		if err != nil {
			_ = markAuditRolledBack(&audit, "SNAPSHOT_FAIL", err.Error(), err.Error())
			return applyFilesFailWithRollback("SNAPSHOT_FAIL", "快照失败: "+err.Error(), map[string]any{"snapshotId": "", "details": []string{err.Error()}}), nil
		}
		snap = s
		snapshotID = s.backupDir
	}

	applied := make([]map[string]any, 0, len(files))
	for _, f := range files {
		if testWriteFailpoint != nil {
			if err := testWriteFailpoint(f.Path); err != nil {
				return rollbackApply(&audit, snap, 0, err, applied), nil
			}
		}
		if err := writeGenFile(f); err != nil {
			return rollbackApply(&audit, snap, 0, err, applied), nil
		}
		applied = append(applied, map[string]any{"path": f.Path, "status": "written"})
	}

	var menuID uint
	if applyMenu && dry.MenuPlan != nil && dry.MenuPlan.Menu.Path != "" {
		d := &codegen.Data{
			Snake:        dry.MenuPlan.Menu.Path,
			StructName:   dry.MenuPlan.Menu.Name,
			Business:     dry.MenuPlan.Menu.Title,
			ParentMenuID: parentMenuID,
		}
		mid, err := createGeneratedMenu(d)
		if err != nil {
			return rollbackApply(&audit, snap, 0, err, applied), nil
		}
		menuID = mid
	}

	if err := rebuildCasbinForApply(); err != nil {
		return rollbackApply(&audit, snap, menuID, err, applied), nil
	}

	filesJSON, _ := json.Marshal(files)
	resultJSON, _ := json.Marshal(ApplyFilesResult{OK: true, Applied: applied, MenuID: menuID, CasbinRebuilt: true})
	if err := global.DB.Model(&audit).Updates(map[string]any{
		"status": "applied", "files": string(filesJSON), "snapshot_id": snapshotID, "result": string(resultJSON),
	}).Error; err != nil {
		return rollbackApply(&audit, snap, menuID, err, applied), nil
	}
	rollback := map[string]any{
		"snapshotId": snapshotID,
		"command":    fmt.Sprintf("从 .fde-gen-backup/%s 恢复备份文件; 删除菜单 %d 及其按钮/授权后调用 RebuildCasbin", in.BatchID, menuID),
	}
	return &ApplyFilesResult{OK: true, Applied: applied, MenuID: menuID, CasbinRebuilt: true, Rollback: rollback}, nil
}

// rollbackApply 失败补偿: 恢复快照 -> 删除本次菜单/授权 -> 重建 casbin -> 审计 rolled_back
func rollbackApply(audit *model.GenAudit, snap *genSnapshot, menuID uint, cause error, applied []map[string]any) *ApplyFilesResult {
	details := make([]string, 0, 4)
	details = append(details, fmt.Sprintf("本次已写盘 %d 个文件", len(applied)))
	snapshotID := ""
	if snap != nil {
		snapshotID = snap.backupDir
		if err := snap.restore(); err != nil {
			details = append(details, "恢复快照失败: "+err.Error())
		} else {
			details = append(details, fmt.Sprintf("已恢复 %d 个快照文件", len(snap.manifest.Entries)))
		}
	}
	if menuID > 0 {
		if err := deleteGeneratedMenuTree(menuID); err != nil {
			details = append(details, "删除菜单失败: "+err.Error())
		} else {
			details = append(details, fmt.Sprintf("已删除菜单 %d 及其按钮/授权", menuID))
		}
	}
	if err := RebuildCasbin(); err != nil {
		details = append(details, "重建casbin失败: "+err.Error())
	} else {
		details = append(details, "casbin 已重建")
	}
	detailText := strings.Join(details, "; ")
	_ = markAuditRolledBack(audit, "SNAPSHOT_FAIL", cause.Error(), detailText)
	return applyFilesFailWithRollback("SNAPSHOT_FAIL", "写盘/菜单/权限失败，已回滚: "+cause.Error(), map[string]any{
		"snapshotId": snapshotID,
		"details":    details,
		"command":    fmt.Sprintf("已恢复快照并删除菜单 %d，casbin 已重建", menuID),
	})
}

func markAuditRolledBack(audit *model.GenAudit, errCode, msg, detail string) error {
	resultJSON, _ := json.Marshal(map[string]any{"errCode": errCode, "msg": msg, "rollback": detail})
	return global.DB.Model(audit).Updates(map[string]any{"status": "rolled_back", "result": string(resultJSON)}).Error
}

func deleteGeneratedMenuTree(menuID uint) error {
	var ids []uint
	if err := global.DB.Model(&model.Menu{}).Where("id = ? OR parent_id = ?", menuID, menuID).Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if err := global.DB.Table("role_menus").Where("menu_id IN ?", ids).Delete(nil).Error; err != nil {
		return err
	}
	return global.DB.Where("id IN ?", ids).Delete(&model.Menu{}).Error
}

var (
	testWriteFailpoint         func(path string) error
	testCasbinRebuildFailpoint func() error
)

func rebuildCasbinForApply() error {
	if testCasbinRebuildFailpoint != nil {
		if err := testCasbinRebuildFailpoint(); err != nil {
			return err
		}
	}
	return RebuildCasbin()
}

// writeGenFile 校验路径白名单后写单个生成文件
func writeGenFile(f codegen.FileResult) error {
	full, err := resolveGenFilePath(f.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(f.Content), 0o644)
}

// validateGenFilePath 路径白名单: 仅允许 server/internal 与 web/src 前缀的相对路径
func validateGenFilePath(p string) error {
	if p == "" {
		return errors.New("文件路径为空")
	}
	if path.IsAbs(p) {
		return errors.New("不允许绝对路径: " + p)
	}
	clean := path.Clean(p)
	if clean != p {
		return errors.New("路径必须为规范相对路径: " + p)
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return errors.New("路径不允许 ..: " + p)
	}
	if strings.HasPrefix(clean, "internal/") || strings.HasPrefix(clean, "web/src/") {
		return nil
	}
	return errors.New("路径不在白名单(仅 server/internal 与 web/src): " + p)
}

// resolveGenFilePath 把生成器相对路径解析为绝对路径(web/ 前缀落到 WebDir, 其余落到 ServerDir)
func resolveGenFilePath(p string) (string, error) {
	if err := validateGenFilePath(p); err != nil {
		return "", err
	}
	if global.Config == nil {
		return "", errors.New("代码生成目录配置未初始化")
	}
	cfg := global.Config.Codegen
	if strings.HasPrefix(p, "web/") {
		return filepath.Join(cfg.WebDir, strings.TrimPrefix(p, "web/")), nil
	}
	return filepath.Join(cfg.ServerDir, p), nil
}

type snapshotManifest struct {
	BatchID string          `json:"batchId"`
	Entries []snapshotEntry `json:"entries"`
}

type snapshotEntry struct {
	Path   string `json:"path"`
	Status string `json:"status"` // backed_up | will_create
	Backup string `json:"backup,omitempty"`
}

type genSnapshot struct {
	batchID   string
	backupDir string
	manifest  snapshotManifest
}

// takeGenSnapshot 把将被覆盖的既有文件备份到 .fde-gen-backup/<batchId>/ 并写 manifest.json
func takeGenSnapshot(batchID string, files []codegen.FileResult) (*genSnapshot, error) {
	if global.Config == nil {
		return nil, errors.New("代码生成目录配置未初始化")
	}
	if batchID == "" || filepath.Base(batchID) != batchID {
		return nil, errors.New("非法批次号: " + batchID)
	}
	backupDir := filepath.Join(global.Config.Codegen.ServerDir, ".fde-gen-backup", batchID)
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return nil, err
	}
	snap := &genSnapshot{batchID: batchID, backupDir: backupDir}
	cleanup := func() { _ = os.RemoveAll(backupDir) }
	for i, f := range files {
		full, err := resolveGenFilePath(f.Path)
		if err != nil {
			cleanup()
			return nil, err
		}
		st, statErr := os.Stat(full)
		if statErr == nil && st.IsDir() {
			cleanup()
			return nil, fmt.Errorf("目标路径是目录: %s", f.Path)
		}
		if statErr == nil {
			backupName := fmt.Sprintf("%04d_%s", i, filepath.Base(f.Path))
			if err := copyFile(full, filepath.Join(backupDir, backupName)); err != nil {
				cleanup()
				return nil, err
			}
			snap.manifest.Entries = append(snap.manifest.Entries, snapshotEntry{Path: f.Path, Status: "backed_up", Backup: backupName})
		} else if errors.Is(statErr, os.ErrNotExist) {
			snap.manifest.Entries = append(snap.manifest.Entries, snapshotEntry{Path: f.Path, Status: "will_create"})
		} else {
			cleanup()
			return nil, statErr
		}
	}
	manifest := snapshotManifest{BatchID: batchID, Entries: snap.manifest.Entries}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), b, 0o644); err != nil {
		cleanup()
		return nil, err
	}
	return snap, nil
}

// restore 恢复快照: backed_up 文件覆盖回原位, will_create 文件删除
func (s *genSnapshot) restore() error {
	if s == nil {
		return nil
	}
	for _, e := range s.manifest.Entries {
		full, err := resolveGenFilePath(e.Path)
		if err != nil {
			continue
		}
		switch e.Status {
		case "backed_up":
			if err := copyFile(filepath.Join(s.backupDir, e.Backup), full); err != nil {
				return err
			}
		case "will_create":
			_ = os.Remove(full)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
