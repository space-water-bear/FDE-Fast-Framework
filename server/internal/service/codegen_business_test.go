package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fde-admin/internal/codegen"
	"fde-admin/internal/config"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func setupCodegenBusinessDB(t *testing.T) {
	t.Helper()
	setupTestDB(t)
	if err := global.DB.AutoMigrate(
		&model.BizObject{}, &model.BizField{}, &model.FormSchema{}, &model.GenAudit{},
		&model.Menu{}, &model.Role{}, &model.User{},
	); err != nil {
		t.Fatalf("migrate codegen business: %v", err)
	}
	tmp := t.TempDir()
	serverDir := filepath.Join(tmp, "server")
	webDir := filepath.Join(tmp, "web")
	if err := os.MkdirAll(filepath.Join(serverDir, "internal"), 0o755); err != nil {
		t.Fatalf("mkdir server: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(webDir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir web: %v", err)
	}
	global.Config = &config.Config{Codegen: config.Codegen{ServerDir: serverDir, WebDir: webDir}}
	if err := global.DB.Create(&model.Role{Name: "超级管理员", Code: "admin", Enabled: true}).Error; err != nil {
		t.Fatalf("seed admin role: %v", err)
	}
	t.Cleanup(func() {
		testWriteFailpoint = nil
		testCasbinRebuildFailpoint = nil
	})
}

func seedBizCustomer(t *testing.T) {
	t.Helper()
	obj := model.BizObject{Key: "biz_customer", Name: "客户", Table: "biz_customer", Schema: "{}", Version: 1}
	if err := global.DB.Create(&obj).Error; err != nil {
		t.Fatalf("create biz object: %v", err)
	}
	fields := []model.BizField{
		{ObjectID: obj.ID, Name: "name", Title: "客户名称", Type: "string", Required: true, Searchable: true, InList: true, InForm: true, Sort: 1},
		{ObjectID: obj.ID, Name: "level", Title: "等级", Type: "enum", InForm: true, Sort: 2},
		{ObjectID: obj.ID, Name: "owner_id", Title: "负责人", Type: "relation", InForm: true, Sort: 3},
		{ObjectID: obj.ID, Name: "balance", Title: "余额", Type: "float", Searchable: true, InList: true, InForm: true, Sort: 4},
	}
	for i := range fields {
		if err := global.DB.Create(&fields[i]).Error; err != nil {
			t.Fatalf("create biz field %s: %v", fields[i].Name, err)
		}
	}
}

func codegenTestContext() context.Context {
	return WithEngineeringScope(context.Background(), EngineeringScope{ActorID: 1, ProjectID: model.DefaultProjectID})
}

func applyCodegenAsUser(args string) (any, error) {
	var in ApplyFilesInput
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return nil, err
	}
	return ApplyFormgenFiles(in, 1)
}

func mustDryRun(t *testing.T) *BusinessCodeResult {
	t.Helper()
	got, err := GenerateBusinessCode(codegenTestContext(), `{"objectKey":"biz_customer","dryRun":true}`)
	if err != nil {
		t.Fatalf("GenerateBusinessCode: %v", err)
	}
	res, ok := got.(*BusinessCodeResult)
	if !ok || !res.OK {
		t.Fatalf("dry-run failed: type=%T ok=%v", got, res != nil && res.OK)
	}
	return res
}

func TestObjectToGenRequestProjection(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)

	gfs, warnings, err := ObjectToGenRequest("biz_customer")
	if err != nil {
		t.Fatalf("ObjectToGenRequest: %v", err)
	}
	if len(gfs) != 2 {
		t.Fatalf("projected fields = %d, want 2 (enum/relation skipped)", len(gfs))
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %d, want 2: %v", len(warnings), warnings)
	}
	byName := map[string]codegen.GenField{}
	for _, f := range gfs {
		byName[f.Name] = f
	}
	nameF, ok := byName["name"]
	if !ok {
		t.Fatalf("name field missing: %v", byName)
	}
	if nameF.Title != "客户名称" || nameF.Type != "string" || !nameF.Search || !nameF.InList || !nameF.InForm || !nameF.Required {
		t.Fatalf("name projection mismatch: %+v", nameF)
	}
	balanceF, ok := byName["balance"]
	if !ok {
		t.Fatalf("balance field missing: %v", byName)
	}
	if balanceF.Type != "float" || !balanceF.Search || !balanceF.InList || !balanceF.InForm || balanceF.Required {
		t.Fatalf("balance projection mismatch: %+v", balanceF)
	}
	if _, _, err := ObjectToGenRequest("nope"); err == nil {
		t.Fatalf("expected missing object error")
	}
}

func TestGenerateBusinessCodeDryRun(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)

	got, err := GenerateBusinessCode(codegenTestContext(), `{"objectKey":"biz_customer","dryRun":true}`)
	if err != nil {
		t.Fatalf("GenerateBusinessCode: %v", err)
	}
	res, ok := got.(*BusinessCodeResult)
	if !ok {
		t.Fatalf("unexpected result type: %T", got)
	}
	if !res.OK {
		t.Fatalf("expected ok, got: %+v", res.Error)
	}
	if len(res.Files) != 6 {
		t.Fatalf("files = %d, want 6", len(res.Files))
	}
	if res.Diff == nil || res.Diff.Added != 6 || res.Diff.Modified != 0 {
		t.Fatalf("diff = %+v", res.Diff)
	}
	if res.MenuPlan == nil || res.MenuPlan.Buttons != 4 || res.MenuPlan.Menu.Path != "customer" {
		t.Fatalf("menuPlan = %+v", res.MenuPlan)
	}
	if res.BatchID == "" {
		t.Fatalf("batchId is empty")
	}
	for _, f := range res.Files {
		full, err := resolveGenFilePath(f.Path)
		if err != nil {
			t.Fatalf("resolve %s: %v", f.Path, err)
		}
		if _, err := os.Stat(full); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("dry-run 不应写盘: %s", f.Path)
		}
	}
	var audit model.GenAudit
	if err := global.DB.Where("batch_id = ?", res.BatchID).First(&audit).Error; err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if audit.Action != "dry_run" || audit.Status != "pending" || audit.Tool != "generate_business_code" {
		t.Fatalf("audit = %+v", audit)
	}
	if audit.UserID != 1 || audit.ProjectID != model.DefaultProjectID {
		t.Fatalf("preview lost trusted actor/project: %+v", audit)
	}
	if len(res.Baselines) != len(res.Files) || res.RootHash == "" || res.ObjectVersion != 1 || res.ExpiresAt.IsZero() {
		t.Fatalf("preview does not fix its apply baseline: %+v", res)
	}
}

func TestApplyFilesSuccess(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)
	dry := mustDryRun(t)

	got, err := applyCodegenAsUser(fmt.Sprintf(`{"batchId":"%s","approve":true}`, dry.BatchID))
	if err != nil {
		t.Fatalf("ApplyFiles: %v", err)
	}
	ares, ok := got.(*ApplyFilesResult)
	if !ok {
		t.Fatalf("unexpected result type: %T", got)
	}
	if !ares.OK {
		t.Fatalf("apply failed: %+v", ares.Error)
	}
	if len(ares.Applied) != 6 {
		t.Fatalf("applied = %d, want 6", len(ares.Applied))
	}
	if ares.MenuID == 0 || !ares.CasbinRebuilt {
		t.Fatalf("menuId/casbin invalid: %+v", ares)
	}
	modelPath := filepath.Join(global.Config.Codegen.ServerDir, "internal", "model", "customer.go")
	if _, err := os.Stat(modelPath); err != nil {
		t.Fatalf("generated model missing: %v", err)
	}
	webPath := filepath.Join(global.Config.Codegen.WebDir, "src", "api", "customer.ts")
	if _, err := os.Stat(webPath); err != nil {
		t.Fatalf("generated web api missing: %v", err)
	}
	var menuCount int64
	if err := global.DB.Model(&model.Menu{}).Where("path = ? AND name = ?", "customer", "Customer").Count(&menuCount).Error; err != nil {
		t.Fatalf("menu count: %v", err)
	}
	if menuCount != 1 {
		t.Fatalf("menu count = %d, want 1", menuCount)
	}
	var btnCount int64
	if err := global.DB.Model(&model.Menu{}).Where("parent_id = ? AND type = ?", ares.MenuID, 2).Count(&btnCount).Error; err != nil {
		t.Fatalf("button count: %v", err)
	}
	if btnCount != 4 {
		t.Fatalf("button count = %d, want 4", btnCount)
	}
	var ids []uint
	if err := global.DB.Model(&model.Menu{}).Where("id = ? OR parent_id = ?", ares.MenuID, ares.MenuID).Pluck("id", &ids).Error; err != nil {
		t.Fatalf("menu ids: %v", err)
	}
	var rmCount int64
	if err := global.DB.Table("role_menus").Where("menu_id IN ?", ids).Count(&rmCount).Error; err != nil {
		t.Fatalf("role_menus count: %v", err)
	}
	if rmCount != 5 {
		t.Fatalf("role_menus count = %d, want 5 (1 menu + 4 buttons)", rmCount)
	}
	var audit model.GenAudit
	if err := global.DB.Where("batch_id = ?", dry.BatchID).First(&audit).Error; err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if audit.Status != "applied" || audit.SnapshotID == "" {
		t.Fatalf("audit not applied: status=%s snapshot=%s", audit.Status, audit.SnapshotID)
	}
	if _, err := os.Stat(filepath.Join(global.Config.Codegen.ServerDir, ".fde-gen-backup", dry.BatchID, "manifest.json")); err != nil {
		t.Fatalf("snapshot manifest missing: %v", err)
	}
}

func TestApplyFilesRollbackOnWriteFailure(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)
	dry := mustDryRun(t)

	webAPIPath := filepath.Join(global.Config.Codegen.WebDir, "src", "api", "customer.ts")
	testWriteFailpoint = func(p string) error {
		if strings.HasSuffix(p, "index.vue") {
			return errors.New("注入: 写盘失败")
		}
		return nil
	}

	got, err := applyCodegenAsUser(fmt.Sprintf(`{"batchId":"%s","approve":true}`, dry.BatchID))
	if err != nil {
		t.Fatalf("ApplyFiles: %v", err)
	}
	ares := got.(*ApplyFilesResult)
	if ares.OK || ares.Error == nil || ares.Error.Code != 40001 {
		t.Fatalf("expected 40001 failure, got ok=%v err=%+v", ares.OK, ares.Error)
	}
	if _, err := os.Stat(webAPIPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new web API should be removed after rollback: %v", err)
	}
	modelPath := filepath.Join(global.Config.Codegen.ServerDir, "internal", "model", "customer.go")
	if _, err := os.Stat(modelPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new file should be deleted: %s err=%v", modelPath, err)
	}
	var menuCount int64
	if err := global.DB.Model(&model.Menu{}).Where("path = ?", "customer").Count(&menuCount).Error; err != nil {
		t.Fatalf("menu count: %v", err)
	}
	if menuCount != 0 {
		t.Fatalf("menu should not exist, count = %d", menuCount)
	}
	var audit model.GenAudit
	if err := global.DB.Where("batch_id = ?", dry.BatchID).First(&audit).Error; err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if audit.Status != "rolled_back" {
		t.Fatalf("audit status = %s, want rolled_back", audit.Status)
	}
}

func TestApplyFilesRollbackDeletesMenu(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)
	dry := mustDryRun(t)

	webAPIPath := filepath.Join(global.Config.Codegen.WebDir, "src", "api", "customer.ts")
	testCasbinRebuildFailpoint = func() error { return errors.New("注入: casbin 重建失败") }

	got, err := applyCodegenAsUser(fmt.Sprintf(`{"batchId":"%s","approve":true}`, dry.BatchID))
	if err != nil {
		t.Fatalf("ApplyFiles: %v", err)
	}
	ares := got.(*ApplyFilesResult)
	if ares.OK || ares.Error == nil || ares.Error.Code != 40001 {
		t.Fatalf("expected 40001 failure, got ok=%v err=%+v", ares.OK, ares.Error)
	}
	if _, err := os.Stat(webAPIPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new web API should be removed after rollback: %v", err)
	}
	modelPath := filepath.Join(global.Config.Codegen.ServerDir, "internal", "model", "customer.go")
	if _, err := os.Stat(modelPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new file should be deleted: %s err=%v", modelPath, err)
	}
	var menuCount int64
	if err := global.DB.Model(&model.Menu{}).Where("path = ?", "customer").Count(&menuCount).Error; err != nil {
		t.Fatalf("menu count: %v", err)
	}
	if menuCount != 0 {
		t.Fatalf("menu should be deleted, count = %d", menuCount)
	}
	var rmCount int64
	if err := global.DB.Table("role_menus").Count(&rmCount).Error; err != nil {
		t.Fatalf("role_menus count: %v", err)
	}
	if rmCount != 0 {
		t.Fatalf("role_menus should be cleaned, count = %d", rmCount)
	}
	var audit model.GenAudit
	if err := global.DB.Where("batch_id = ?", dry.BatchID).First(&audit).Error; err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if audit.Status != "rolled_back" {
		t.Fatalf("audit status = %s, want rolled_back", audit.Status)
	}
}

func TestApplyFilesRollbackOnPathConflict(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)
	dry := mustDryRun(t)

	// 制造同名冲突: 目标文件位置预先创建为目录, 快照阶段即失败
	conflictPath := filepath.Join(global.Config.Codegen.WebDir, "src", "views", "generated", "customer", "index.vue")
	if err := os.MkdirAll(conflictPath, 0o755); err != nil {
		t.Fatalf("mkdir conflict: %v", err)
	}

	got, err := applyCodegenAsUser(fmt.Sprintf(`{"batchId":"%s","approve":true}`, dry.BatchID))
	if err != nil {
		t.Fatalf("ApplyFiles: %v", err)
	}
	if ares, ok := got.(*ApplyFilesResult); !ok {
		t.Fatalf("unexpected result type: %T", got)
	} else if ares.OK || ares.Error == nil || ares.Error.Code != 40001 {
		t.Fatalf("expected 40001 failure, got ok=%v err=%+v", ares.OK, ares.Error)
	}
	var menuCount int64
	if err := global.DB.Model(&model.Menu{}).Where("path = ?", "customer").Count(&menuCount).Error; err != nil {
		t.Fatalf("menu count: %v", err)
	}
	if menuCount != 0 {
		t.Fatalf("menu should not exist, count = %d", menuCount)
	}
	var audit model.GenAudit
	if err := global.DB.Where("batch_id = ?", dry.BatchID).First(&audit).Error; err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if audit.Status != "pending" {
		t.Fatalf("audit status = %s, want pending", audit.Status)
	}
}

func TestCodegenPreviewGenerateRegression(t *testing.T) {
	setupCodegenBusinessDB(t)
	req := codegen.GenRequest{
		TableName:  "biz_order",
		Business:   "订单",
		StructName: "Order",
		Fields: []codegen.GenField{
			{Name: "order_no", Title: "订单号", Type: "string", Search: true, InList: true, InForm: true, Required: true},
			{Name: "amount", Title: "金额", Type: "float", Search: true, InList: true, InForm: true},
			{Name: "paid", Title: "已支付", Type: "bool", InList: true, InForm: true},
		},
	}
	files, err := CodegenPreview(req)
	if err != nil {
		t.Fatalf("CodegenPreview: %v", err)
	}
	if len(files) != 6 {
		t.Fatalf("CodegenPreview files = %d, want 6", len(files))
	}
	if _, _, err := CodegenGenerate(req); err == nil || !strings.Contains(err.Error(), "APPROVAL_REQUIRED") {
		t.Fatalf("legacy generation must require a reviewed batch: %v", err)
	}
	dry, err := CodegenBusinessPreview(req, 1)
	if err != nil || !dry.OK {
		t.Fatalf("preview: %+v %v", dry, err)
	}
	approve := true
	applied, err := ApplyFormgenFiles(ApplyFilesInput{BatchID: dry.BatchID, Approve: &approve}, 1)
	if err != nil || !applied.OK {
		t.Fatalf("explicit apply: %+v %v", applied, err)
	}
	modelPath := filepath.Join(global.Config.Codegen.ServerDir, "internal", "model", "order.go")
	if _, err := os.Stat(modelPath); err != nil {
		t.Fatalf("generated model missing: %v", err)
	}
	var menuCount int64
	if err := global.DB.Model(&model.Menu{}).Where("path = ? AND name = ?", "order", "Order").Count(&menuCount).Error; err != nil {
		t.Fatalf("menu count: %v", err)
	}
	if menuCount != 1 {
		t.Fatalf("menu count = %d, want 1", menuCount)
	}
}
