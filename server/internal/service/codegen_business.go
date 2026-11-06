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
	"sync"
	"time"

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

// CodegenFileBaseline 固定预览时的文件状态；不存在也是必须验证的状态。
type CodegenFileBaseline struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	SHA256 string `json:"sha256,omitempty"`
}

// BusinessCodeResult generate_business_code 返回结构(设计文档 6.2)
type BusinessCodeResult struct {
	OK              bool                  `json:"ok"`
	BatchID         string                `json:"batchId,omitempty"`
	Files           []codegen.FileResult  `json:"files,omitempty"`
	Diff            *CodegenDiff          `json:"diff,omitempty"`
	MenuPlan        *CodegenMenuPlan      `json:"menuPlan,omitempty"`
	RestartRequired bool                  `json:"restartRequired"`
	Warnings        []string              `json:"warnings,omitempty"`
	Error           *FormgenError         `json:"error,omitempty"`
	Baselines       []CodegenFileBaseline `json:"baselines,omitempty"`
	ObjectVersion   int                   `json:"objectVersion,omitempty"`
	RootHash        string                `json:"rootHash,omitempty"`
	ExpiresAt       time.Time             `json:"expiresAt"`
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
	return ObjectToGenRequestInProject(model.DefaultProjectID, objectKey)
}

func ObjectToGenRequestInProject(projectID uint, objectKey string) ([]codegen.GenField, []string, error) {
	_, fields, err := GetBizObjectByKeyInProject(projectID, objectKey)
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
	scope, err := RequireEngineeringScope(ctx)
	if err != nil {
		return businessCodeFail("IDENTITY_REQUIRED", err.Error()), nil
	}
	return generateBusinessCode(args, scope)
}

// CodegenBusinessPreview HTTP /api/codegen/preview 带 objectKey 时复用工具实现
func CodegenBusinessPreview(req codegen.GenRequest, userID uint) (*BusinessCodeResult, error) {
	return CodegenBusinessPreviewInProject(req, userID, model.DefaultProjectID)
}

func CodegenBusinessPreviewInProject(req codegen.GenRequest, userID, projectID uint) (*BusinessCodeResult, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return generateBusinessCode(string(b), EngineeringScope{ActorID: userID, ProjectID: projectID})
}

// ApplyFiles apply_files 工具执行函数(AiToolFunc 签名)
func ApplyFiles(ctx context.Context, args string) (any, error) {
	var in ApplyFilesInput
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return applyFilesFail("FORM_INVALID_SCHEMA", "参数不是合法 JSON: "+err.Error()), nil
	}
	return applyFilesFail("APPROVAL_REQUIRED", "请在代码预览页面明确确认该批次；智能体参数不能批准写入宿主源码"), nil
}

// ApplyFormgenFiles HTTP /api/formgen/apply 与工具共用同一实现
func ApplyFormgenFiles(in ApplyFilesInput, userID uint) (*ApplyFilesResult, error) {
	return ApplyFormgenFilesInProject(in, userID, model.DefaultProjectID)
}

// ApplyFormgenFilesInProject 只供已验证身份、项目权限及明确批次确认的 HTTP/人工入口调用。
func ApplyFormgenFilesInProject(in ApplyFilesInput, userID, projectID uint) (*ApplyFilesResult, error) {
	return applyFilesCore(in, EngineeringScope{ActorID: userID, ProjectID: projectID})
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

// generateBusinessCode 只生成预览；写盘必须经独立的人工确认入口。
func generateBusinessCode(args string, scope EngineeringScope) (*BusinessCodeResult, error) {
	if scope.ActorID == 0 || scope.ProjectID == 0 {
		return businessCodeFail("IDENTITY_REQUIRED", "代码预览需要已验证的用户与项目身份"), nil
	}
	var in BusinessCodeInput
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return businessCodeFail("FORM_INVALID_SCHEMA", "参数不是合法 JSON: "+err.Error()), nil
	}
	if in.DryRun != nil && !*in.DryRun {
		return businessCodeFail("APPROVAL_REQUIRED", "dryRun=false 不能批准写盘，请先预览再确认批次"), nil
	}

	var req codegen.GenRequest
	warnings := make([]string, 0)
	objectVersion := 0
	if strings.TrimSpace(in.ObjectKey) != "" {
		obj, _, err := GetBizObjectByKeyInProject(scope.ProjectID, in.ObjectKey)
		if err != nil {
			return businessCodeFail("FORM_INVALID_SCHEMA", "业务对象不存在: "+in.ObjectKey), nil
		}
		objectVersion = obj.Version
		gfs, warns, err := ObjectToGenRequestInProject(scope.ProjectID, in.ObjectKey)
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
	if scope.ProjectID == model.DefaultProjectID {
		if err := checkLegacyCodegenConflict(&CodegenMenuPlan{TableName: req.TableName, Menu: CodegenMenuPlanItem{Path: d.Snake, Name: d.StructName}}); err != nil {
			return businessCodeFail("CODE_CONFLICT", err.Error()), nil
		}
	}

	files, err := codegen.Render(d)
	if err != nil {
		return businessCodeFail("FORM_INVALID_SCHEMA", err.Error()), nil
	}

	roots, err := loadCodegenRoots(scope.ProjectID)
	if err != nil {
		return businessCodeFail("WORKSPACE_REQUIRED", err.Error()), nil
	}
	diff, baselines := computeGenPlan(files, roots)
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
		Baselines: baselines, ObjectVersion: objectVersion, RootHash: roots.hash(),
		ExpiresAt: time.Now().UTC().Add(codegenBatchTTL),
	}

	if err := persistDryRunAudit(batchID, in.ObjectKey, args, res, scope); err != nil {
		return nil, err
	}

	return res, nil
}

func persistDryRunAudit(batchID, objectKey, args string, res *BusinessCodeResult, scope EngineeringScope) error {
	resultJSON, err := json.Marshal(res)
	if err != nil {
		return err
	}
	filesJSON, err := json.Marshal(res.Files)
	if err != nil {
		return err
	}
	audit := model.GenAudit{
		UserID:    scope.ActorID,
		ProjectID: scope.ProjectID,
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

// computeGenDiff 保留默认项目的只读兼容入口。
func computeGenDiff(files []codegen.FileResult) *CodegenDiff {
	roots, err := loadCodegenRoots(model.DefaultProjectID)
	if err != nil {
		return &CodegenDiff{Conflicts: []string{err.Error()}}
	}
	diff, _ := computeGenPlan(files, roots)
	return diff
}

func computeGenPlan(files []codegen.FileResult, roots codegenRoots) (*CodegenDiff, []CodegenFileBaseline) {
	diff := &CodegenDiff{Conflicts: []string{}}
	baselines := make([]CodegenFileBaseline, 0, len(files))
	for _, f := range files {
		baseline, err := roots.baseline(f.Path)
		if err != nil {
			diff.Conflicts = append(diff.Conflicts, f.Path)
			continue
		}
		baselines = append(baselines, baseline)
		if !baseline.Exists {
			diff.Added++
			continue
		}
		if baseline.SHA256 != sha256Hex(f.Content) {
			diff.Modified++
			diff.Conflicts = append(diff.Conflicts, f.Path)
		}
	}
	return diff, baselines
}

const codegenBatchTTL = 30 * time.Minute

// A single P0 process serializes host/workspace writes. The persistent CAS below
// also prevents two requests/processes from claiming the same pending batch.
var codegenApplyMu sync.Mutex

func applyFilesCore(in ApplyFilesInput, scope EngineeringScope) (*ApplyFilesResult, error) {
	if scope.ActorID == 0 || scope.ProjectID == 0 {
		return applyFilesFail("IDENTITY_REQUIRED", "代码写入需要已验证的用户与项目身份"), nil
	}
	if strings.TrimSpace(in.BatchID) == "" {
		return applyFilesFail("BATCH_STALE", "请先预览并提供 batchId"), nil
	}
	if in.Approve == nil || !*in.Approve {
		return applyFilesFail("APPROVAL_REQUIRED", "必须明确确认已预览批次"), nil
	}
	if in.Snapshot != nil && !*in.Snapshot {
		return applyFilesFail("SNAPSHOT_REQUIRED", "写入前必须保留恢复快照"), nil
	}
	if len(in.Files) != 0 || in.ParentMenuID != nil || in.ApplyMenu != nil {
		return applyFilesFail("BATCH_STALE", "确认请求不能修改已预览的文件或菜单计划"), nil
	}
	codegenApplyMu.Lock()
	defer codegenApplyMu.Unlock()

	var audit model.GenAudit
	if err := global.DB.Where("batch_id = ? AND user_id = ? AND project_id = ?", in.BatchID, scope.ActorID, scope.ProjectID).First(&audit).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return applyFilesFail("BATCH_STALE", "批次不存在或不属于当前用户/项目"), nil
		}
		return nil, err
	}
	if audit.Status != "pending" {
		return applyFilesFail("BATCH_STALE", "批次已处理: "+audit.Status), nil
	}
	if audit.CreatedAt.IsZero() || time.Since(audit.CreatedAt) > codegenBatchTTL {
		return applyFilesFail("BATCH_STALE", "批次已过期，请重新预览"), nil
	}
	if in.ObjectKey != "" && in.ObjectKey != audit.ObjectKey {
		return applyFilesFail("BATCH_STALE", "对象与预览批次不一致"), nil
	}
	var dry BusinessCodeResult
	if err := json.Unmarshal([]byte(audit.Result), &dry); err != nil || !dry.OK || dry.BatchID != in.BatchID {
		return applyFilesFail("BATCH_STALE", "批次结果无效，请重新预览"), nil
	}
	if dry.Diff == nil || len(dry.Diff.Conflicts) != 0 {
		return applyFilesFail("CODE_CONFLICT", "预览包含未解决的文件冲突，请先解决冲突并重新预览"), nil
	}
	if len(dry.Files) == 0 || len(dry.Baselines) != len(dry.Files) {
		return applyFilesFail("BATCH_STALE", "批次缺少文件基线，请重新预览"), nil
	}
	roots, err := loadCodegenRoots(scope.ProjectID)
	if err != nil {
		return applyFilesFail("WORKSPACE_REQUIRED", err.Error()), nil
	}
	if dry.RootHash == "" || dry.RootHash != roots.hash() {
		return applyFilesFail("BATCH_STALE", "代码输出目录已改变，请重新预览"), nil
	}
	if audit.ObjectKey != "" {
		obj, _, err := GetBizObjectByKeyInProject(scope.ProjectID, audit.ObjectKey)
		if err != nil || obj.Version != dry.ObjectVersion {
			return applyFilesFail("BATCH_STALE", "业务规格已改变，请重新预览"), nil
		}
	}
	for i, f := range dry.Files {
		if dry.Baselines[i].Path != f.Path {
			return applyFilesFail("BATCH_STALE", "文件基线与预览不一致"), nil
		}
		if err := roots.checkBaseline(dry.Baselines[i]); err != nil {
			return applyFilesFail("CODE_CONFLICT", err.Error()), nil
		}
	}
	if scope.ProjectID == model.DefaultProjectID {
		if err := checkLegacyCodegenConflict(dry.MenuPlan); err != nil {
			return applyFilesFail("CODE_CONFLICT", err.Error()), nil
		}
	}
	claim := global.DB.Model(&model.GenAudit{}).Where("id = ? AND user_id = ? AND project_id = ? AND status = ?", audit.ID, scope.ActorID, scope.ProjectID, "pending").Update("status", "applying")
	if claim.Error != nil {
		return nil, claim.Error
	}
	if claim.RowsAffected != 1 {
		return applyFilesFail("BATCH_STALE", "批次已被其他请求处理"), nil
	}
	audit.Status = "applying"
	snap, err := takeGenSnapshotInRoots(in.BatchID, dry.Files, roots)
	if err != nil {
		_ = markAuditRolledBack(&audit, "SNAPSHOT_FAIL", err.Error(), err.Error())
		return applyFilesFailWithRollback("SNAPSHOT_FAIL", "快照失败: "+err.Error(), map[string]any{"details": []string{err.Error()}}), nil
	}
	applied := make([]map[string]any, 0, len(dry.Files))
	for i, f := range dry.Files {
		if testWriteFailpoint != nil {
			if err := testWriteFailpoint(f.Path); err != nil {
				return rollbackApply(&audit, snap, 0, err, applied), nil
			}
		}
		if err := roots.checkBaseline(dry.Baselines[i]); err != nil {
			return rollbackApply(&audit, snap, 0, err, applied), nil
		}
		if err := roots.writeFile(f.Path, []byte(f.Content)); err != nil {
			return rollbackApply(&audit, snap, 0, err, applied), nil
		}
		snap.written[f.Path] = sha256Hex(f.Content)
		applied = append(applied, map[string]any{"path": f.Path, "status": "written"})
	}
	var menuID uint
	casbinRebuilt := false
	// A customer workspace carries a plan; it must not install its menus in the workbench DB.
	if scope.ProjectID == model.DefaultProjectID {
		if dry.MenuPlan != nil && dry.MenuPlan.Menu.Path != "" {
			d := &codegen.Data{Snake: dry.MenuPlan.Menu.Path, StructName: dry.MenuPlan.Menu.Name, Business: dry.MenuPlan.Menu.Title, ParentMenuID: dry.MenuPlan.ParentMenuID}
			mid, err := createGeneratedMenu(d)
			if err != nil {
				return rollbackApply(&audit, snap, 0, err, applied), nil
			}
			menuID = mid
		}
		if err := rebuildCasbinForApply(); err != nil {
			return rollbackApply(&audit, snap, menuID, err, applied), nil
		}
		casbinRebuilt = true
	}
	result := &ApplyFilesResult{OK: true, Applied: applied, MenuID: menuID, CasbinRebuilt: casbinRebuilt, Rollback: map[string]any{"snapshotId": snap.backupDir}}
	resultJSON, _ := json.Marshal(result)
	update := global.DB.Model(&model.GenAudit{}).Where("id = ? AND status = ?", audit.ID, "applying").Updates(map[string]any{"status": "applied", "snapshot_id": snap.backupDir, "result": string(resultJSON)})
	if update.Error != nil {
		return rollbackApply(&audit, snap, menuID, update.Error, applied), nil
	}
	if update.RowsAffected != 1 {
		return rollbackApply(&audit, snap, menuID, errors.New("批次状态已改变"), applied), nil
	}
	return result, nil
}

func checkLegacyCodegenConflict(plan *CodegenMenuPlan) error {
	if plan == nil {
		return nil
	}
	if plan.TableName != "" && global.DB.Migrator().HasTable(plan.TableName) {
		return fmt.Errorf("数据表已存在: %s", plan.TableName)
	}
	var count int64
	if err := global.DB.Model(&model.Menu{}).Where("path = ? OR name = ?", plan.Menu.Path, plan.Menu.Name).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("同名菜单已存在: %s", plan.Menu.Path)
	}
	return nil
}

// rollbackApply 失败补偿: 恢复快照 -> 删除本次菜单/授权 -> 重建 casbin -> 审计 rolled_back
func rollbackApply(audit *model.GenAudit, snap *genSnapshot, menuID uint, cause error, applied []map[string]any) *ApplyFilesResult {
	details := []string{fmt.Sprintf("本次已写盘 %d 个文件", len(applied))}
	snapshotID := ""
	complete := true
	if snap != nil {
		snapshotID = snap.backupDir
		if err := snap.restore(); err != nil {
			complete = false
			details = append(details, "恢复快照失败: "+err.Error())
		} else {
			details = append(details, "本次写入已恢复")
		}
	}
	if menuID > 0 {
		if err := deleteGeneratedMenuTree(menuID); err != nil {
			complete = false
			details = append(details, "删除菜单失败: "+err.Error())
		} else {
			details = append(details, fmt.Sprintf("已删除菜单 %d 及其按钮/授权", menuID))
		}
	}
	if audit.ProjectID == model.DefaultProjectID {
		if err := RebuildCasbin(); err != nil {
			complete = false
			details = append(details, "重建casbin失败: "+err.Error())
		} else {
			details = append(details, "casbin 已重建")
		}
	}
	status, code, msg := "rolled_back", "APPLY_FAILED", "写入失败，已恢复本次变更: "
	if !complete {
		status, code, msg = "rollback_failed", "ROLLBACK_FAILED", "写入失败且恢复未完成，请检查恢复详情: "
	}
	detailText := strings.Join(details, "; ")
	resultJSON, _ := json.Marshal(map[string]any{"errCode": code, "msg": cause.Error(), "rollback": detailText})
	if err := global.DB.Model(audit).Updates(map[string]any{"status": status, "snapshot_id": snapshotID, "result": string(resultJSON)}).Error; err != nil {
		complete = false
		code, msg = "ROLLBACK_FAILED", "恢复记录保存失败，请核对文件与数据库状态: "
		details = append(details, err.Error())
	}
	return applyFilesFailWithRollback(code, msg+cause.Error(), map[string]any{"snapshotId": snapshotID, "complete": complete, "details": details})
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

// writeGenFile 保留默认项目写入兼容函数；公开生成入口不得直接调用。
func writeGenFile(f codegen.FileResult) error {
	roots, err := loadCodegenRoots(model.DefaultProjectID)
	if err != nil {
		return err
	}
	return roots.writeFile(f.Path, []byte(f.Content))
}

func validateGenFilePath(p string) error {
	if p == "" || path.IsAbs(p) || filepath.VolumeName(p) != "" || strings.Contains(p, "\\") {
		return errors.New("不允许绝对路径或非规范文件路径")
	}
	clean := path.Clean(p)
	if clean != p || clean == ".." || strings.HasPrefix(clean, "../") {
		return errors.New("路径必须为规范相对路径，不能包含 ..")
	}
	if strings.HasPrefix(clean, "internal/") || strings.HasPrefix(clean, "web/src/") {
		return nil
	}
	return errors.New("路径不在白名单(仅 server/internal 与 web/src)")
}

func resolveGenFilePath(p string) (string, error) {
	return resolveGenFilePathInProject(model.DefaultProjectID, p)
}

func resolveGenFilePathInProject(projectID uint, p string) (string, error) {
	roots, err := loadCodegenRoots(projectID)
	if err != nil {
		return "", err
	}
	dir, rel, err := roots.location(p)
	if err != nil {
		return "", err
	}
	r, err := openCheckedRoot(dir, rel)
	if err != nil {
		return "", err
	}
	r.Close()
	return filepath.Join(dir, rel), nil
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
	roots     codegenRoots
	manifest  snapshotManifest
	written   map[string]string
}

func takeGenSnapshot(batchID string, files []codegen.FileResult) (*genSnapshot, error) {
	roots, err := loadCodegenRoots(model.DefaultProjectID)
	if err != nil {
		return nil, err
	}
	return takeGenSnapshotInRoots(batchID, files, roots)
}

func takeGenSnapshotInRoots(batchID string, files []codegen.FileResult, roots codegenRoots) (*genSnapshot, error) {
	if _, err := uuid.Parse(batchID); err != nil {
		return nil, errors.New("非法批次号")
	}
	backupRel := filepath.Join(".fde-gen-backup", batchID)
	snap := &genSnapshot{batchID: batchID, backupDir: filepath.Join(roots.server, backupRel), roots: roots, written: map[string]string{}}
	for i, f := range files {
		dir, rel, err := roots.location(f.Path)
		if err != nil {
			return nil, err
		}
		data, err := readRootedFile(dir, rel)
		if errors.Is(err, os.ErrNotExist) {
			snap.manifest.Entries = append(snap.manifest.Entries, snapshotEntry{Path: f.Path, Status: "will_create"})
			continue
		}
		if err != nil {
			return nil, err
		}
		name := fmt.Sprintf("%04d_%s", i, filepath.Base(f.Path))
		if err := writeRootedFile(roots.server, filepath.Join(backupRel, name), data); err != nil {
			return nil, err
		}
		snap.manifest.Entries = append(snap.manifest.Entries, snapshotEntry{Path: f.Path, Status: "backed_up", Backup: name})
	}
	snap.manifest.BatchID = batchID
	b, err := json.MarshalIndent(snap.manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeRootedFile(roots.server, filepath.Join(backupRel, "manifest.json"), b); err != nil {
		return nil, err
	}
	return snap, nil
}

func (s *genSnapshot) restore() error {
	if s == nil {
		return nil
	}
	for _, e := range s.manifest.Entries {
		writtenHash, changed := s.written[e.Path]
		if !changed {
			continue
		}
		current, err := s.roots.baseline(e.Path)
		if err != nil {
			return err
		}
		if !current.Exists || current.SHA256 != writtenHash {
			return fmt.Errorf("恢复前文件已被其他操作修改: %s", e.Path)
		}
		switch e.Status {
		case "backed_up":
			data, err := readRootedFile(s.roots.server, filepath.Join(".fde-gen-backup", s.batchID, e.Backup))
			if err != nil {
				return err
			}
			if err := s.roots.writeFile(e.Path, data); err != nil {
				return err
			}
		case "will_create":
			dir, rel, err := s.roots.location(e.Path)
			if err != nil {
				return err
			}
			r, err := openCheckedRoot(dir, rel)
			if err != nil {
				return err
			}
			err = r.Remove(rel)
			r.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
