package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func TestNativeApplyRequiresIndependentApproval(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)
	dry := mustDryRun(t)
	got, err := ApplyFiles(context.Background(), fmt.Sprintf(`{"batchId":%q,"approve":true}`, dry.BatchID))
	if err != nil {
		t.Fatal(err)
	}
	res := got.(*ApplyFilesResult)
	if res.OK || res.Error == nil || res.Error.ErrCode != "APPROVAL_REQUIRED" {
		t.Fatalf("model-provided approval must not write host source: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(global.Config.Codegen.ServerDir, "internal/model/customer.go")); !os.IsNotExist(err) {
		t.Fatalf("native apply changed source: %v", err)
	}
}

func TestGenerateBusinessCodeDoesNotAutoApprove(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)
	got, err := GenerateBusinessCode(codegenTestContext(), `{"objectKey":"biz_customer","dryRun":false}`)
	if err != nil {
		t.Fatal(err)
	}
	res := got.(*BusinessCodeResult)
	if res.OK || res.Error == nil || res.Error.ErrCode != "APPROVAL_REQUIRED" {
		t.Fatalf("dryRun=false must not self-approve host writes: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(global.Config.Codegen.ServerDir, "internal/model/customer.go")); !os.IsNotExist(err) {
		t.Fatalf("dryRun=false wrote source: %v", err)
	}
}

func TestGenerateBusinessCodeRequiresTrustedIdentity(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)
	got, err := GenerateBusinessCode(context.Background(), `{"objectKey":"biz_customer"}`)
	if err == nil {
		res := got.(*BusinessCodeResult)
		if res.OK || res.Error == nil || res.Error.ErrCode != "IDENTITY_REQUIRED" {
			t.Fatalf("missing trusted identity was accepted: ok=%v err=%+v", res.OK, res.Error)
		}
	}
	var count int64
	if err := global.DB.Model(&model.GenAudit{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("anonymous generation created %d audits", count)
	}
}

func applyReviewedBatch(t *testing.T, batch string, actor, project uint) *ApplyFilesResult {
	t.Helper()
	approve := true
	res, err := ApplyFormgenFilesInProject(ApplyFilesInput{BatchID: batch, Approve: &approve}, actor, project)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func assertNoGeneratedModel(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(global.Config.Codegen.ServerDir, "internal/model/customer.go")); !os.IsNotExist(err) {
		t.Fatalf("rejected batch changed source: %v", err)
	}
}

func TestReviewedBatchIsBoundToActorAndProject(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)
	dry := mustDryRun(t)
	for _, scope := range []EngineeringScope{{ActorID: 2, ProjectID: 1}, {ActorID: 1, ProjectID: 2}, {ActorID: 0, ProjectID: 1}} {
		res := applyReviewedBatch(t, dry.BatchID, scope.ActorID, scope.ProjectID)
		if res.OK {
			t.Fatalf("foreign scope applied batch: %+v", scope)
		}
		assertNoGeneratedModel(t)
	}
	if res := applyReviewedBatch(t, dry.BatchID, 1, 1); !res.OK {
		t.Fatalf("owner apply failed: %+v", res.Error)
	}
}

func TestReviewedBatchRejectsCreatedFileAndUnresolvedConflict(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)
	dry := mustDryRun(t)
	path := filepath.Join(global.Config.Codegen.WebDir, "src/api/customer.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("manually written"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := applyReviewedBatch(t, dry.BatchID, 1, 1); res.OK || res.Error.ErrCode != "CODE_CONFLICT" {
		t.Fatalf("file created after preview was overwritten: %+v", res)
	}
	assertNoGeneratedModel(t)
	second := mustDryRun(t)
	if len(second.Diff.Conflicts) == 0 {
		t.Fatal("expected unresolved preview conflict")
	}
	if res := applyReviewedBatch(t, second.BatchID, 1, 1); res.OK || res.Error.ErrCode != "CODE_CONFLICT" {
		t.Fatalf("unresolved conflict accepted: %+v", res)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "manually written" {
		t.Fatalf("manual content changed: %q %v", got, err)
	}
}

func TestReviewedBatchRejectsModifiedOrDeletedExistingFile(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprint(remove), func(t *testing.T) {
			setupCodegenBusinessDB(t)
			seedBizCustomer(t)
			first := mustDryRun(t)
			f := first.Files[0]
			path := filepath.Join(global.Config.Codegen.ServerDir, f.Path)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(f.Content), 0o644); err != nil {
				t.Fatal(err)
			}
			dry := mustDryRun(t)
			if remove {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("changed after preview"), 0o644); err != nil {
				t.Fatal(err)
			}
			if res := applyReviewedBatch(t, dry.BatchID, 1, 1); res.OK || res.Error.ErrCode != "CODE_CONFLICT" {
				t.Fatalf("stale existing file accepted: %+v", res)
			}
		})
	}
}

func TestReviewedBatchCannotDisableSnapshotOrReplacePlan(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)
	dry := mustDryRun(t)
	yes, no := true, false
	res, err := ApplyFormgenFiles(ApplyFilesInput{BatchID: dry.BatchID, Approve: &yes, Snapshot: &no}, 1)
	if err != nil || res.OK || res.Error.ErrCode != "SNAPSHOT_REQUIRED" {
		t.Fatalf("snapshot disabled: %+v %v", res, err)
	}
	res, err = ApplyFormgenFiles(ApplyFilesInput{BatchID: dry.BatchID, Approve: &yes, Files: dry.Files}, 1)
	if err != nil || res.OK {
		t.Fatalf("mutable apply plan accepted: %+v %v", res, err)
	}
	assertNoGeneratedModel(t)
	if res := applyReviewedBatch(t, dry.BatchID, 1, 1); !res.OK || res.Rollback["snapshotId"] == "" {
		t.Fatalf("mandatory snapshot missing: %+v", res)
	}
}

func TestReviewedBatchRejectsExpiredSpecificationAndRoot(t *testing.T) {
	for _, change := range []string{"expired", "spec", "root"} {
		t.Run(change, func(t *testing.T) {
			setupCodegenBusinessDB(t)
			seedBizCustomer(t)
			dry := mustDryRun(t)
			switch change {
			case "expired":
				if err := global.DB.Model(&model.GenAudit{}).Where("batch_id = ?", dry.BatchID).Update("created_at", time.Now().Add(-31*time.Minute)).Error; err != nil {
					t.Fatal(err)
				}
			case "spec":
				if err := global.DB.Model(&model.BizObject{}).Where("key = ?", "biz_customer").Update("version", 2).Error; err != nil {
					t.Fatal(err)
				}
			case "root":
				global.Config.Codegen.WebDir = t.TempDir()
			}
			if res := applyReviewedBatch(t, dry.BatchID, 1, 1); res.OK || res.Error.ErrCode != "BATCH_STALE" {
				t.Fatalf("stale %s accepted: %+v", change, res)
			}
			assertNoGeneratedModel(t)
		})
	}
}

func TestReviewedBatchCanOnlyBeClaimedOnce(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)
	dry := mustDryRun(t)
	var wg sync.WaitGroup
	results := make(chan *ApplyFilesResult, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			yes := true
			res, err := ApplyFormgenFiles(ApplyFilesInput{BatchID: dry.BatchID, Approve: &yes}, 1)
			results <- res
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	success := 0
	for res := range results {
		if res.OK {
			success++
		} else if res.Error == nil || res.Error.ErrCode != "BATCH_STALE" {
			t.Fatalf("unexpected duplicate outcome: %+v", res)
		}
	}
	if success != 1 {
		t.Fatalf("batch applied %d times", success)
	}
	var count int64
	global.DB.Model(&model.Menu{}).Where("path = ?", "customer").Count(&count)
	if count != 1 {
		t.Fatalf("duplicate menu side effects: %d", count)
	}
}

func TestCodegenRejectsSymlinkEscape(t *testing.T) {
	for _, mode := range []string{"parent", "file", "backup"} {
		t.Run(mode, func(t *testing.T) {
			setupCodegenBusinessDB(t)
			seedBizCustomer(t)
			outside := t.TempDir()
			dry := mustDryRun(t)
			link := filepath.Join(global.Config.Codegen.ServerDir, "internal/model")
			target := outside
			if mode == "file" {
				if err := os.MkdirAll(link, 0o755); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(link, "customer.go")
				target = filepath.Join(outside, "sentinel")
				if err := os.WriteFile(target, []byte("untouched"), 0o644); err != nil {
					t.Fatal(err)
				}
			} else if mode == "backup" {
				link = filepath.Join(global.Config.Codegen.ServerDir, ".fde-gen-backup")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			res := applyReviewedBatch(t, dry.BatchID, 1, 1)
			if res.OK {
				t.Fatalf("symlink %s accepted", mode)
			}
			entries, err := os.ReadDir(outside)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "file" {
				data, err := os.ReadFile(target)
				if err != nil || string(data) != "untouched" {
					t.Fatalf("outside sentinel changed: %q %v", data, err)
				}
			} else if len(entries) != 0 {
				t.Fatalf("wrote outside project: %v", entries)
			}
		})
	}
}

func TestCustomerProjectGenerationUsesOnlyItsWorkspace(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)
	if err := global.DB.AutoMigrate(&model.DevProject{}); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	for _, dir := range []string{"server/internal", "web/src"} {
		if err := os.MkdirAll(filepath.Join(workspace, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	project := model.DevProject{Base: model.Base{ID: 2}, Key: "customer-two", Name: "客户二", OwnerID: 1, WorkspacePath: workspace}
	if err := global.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	obj := model.BizObject{ProjectID: 2, Key: "biz_customer", Name: "独立客户", Table: "biz_customer", Schema: "{}", Version: 1}
	if err := global.DB.Create(&obj).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Create(&model.BizField{ObjectID: obj.ID, Name: "name", Title: "独立客户名", Type: "string", InForm: true, InList: true}).Error; err != nil {
		t.Fatal(err)
	}
	// An unrelated workbench menu must not block or be reused by the customer project.
	if err := global.DB.Create(&model.Menu{Path: "customer", Name: "Customer", Title: "工作台菜单", Type: 1}).Error; err != nil {
		t.Fatal(err)
	}
	ctx := WithEngineeringScope(context.Background(), EngineeringScope{ActorID: 1, ProjectID: 2})
	got, err := GenerateBusinessCode(ctx, `{"objectKey":"biz_customer"}`)
	if err != nil {
		t.Fatal(err)
	}
	dry := got.(*BusinessCodeResult)
	if !dry.OK {
		t.Fatalf("project preview: %+v", dry.Error)
	}
	if res := applyReviewedBatch(t, dry.BatchID, 1, 2); !res.OK || res.MenuID != 0 || res.CasbinRebuilt {
		t.Fatalf("customer apply mutated host permissions: %+v", res)
	}
	assertNoGeneratedModel(t)
	data, err := os.ReadFile(filepath.Join(workspace, "server/internal/model/customer.go"))
	if err != nil || !strings.Contains(string(data), "独立客户名") {
		t.Fatalf("project source missing: %v", err)
	}
	var count int64
	global.DB.Model(&model.Menu{}).Count(&count)
	if count != 1 {
		t.Fatalf("customer menu leaked into workbench: %d", count)
	}
}

func TestCustomerProjectWithoutWorkspaceCannotUseHostSource(t *testing.T) {
	setupCodegenBusinessDB(t)
	if err := global.DB.AutoMigrate(&model.DevProject{}); err != nil {
		t.Fatal(err)
	}
	ctx := WithEngineeringScope(context.Background(), EngineeringScope{ActorID: 1, ProjectID: 2})
	got, err := GenerateBusinessCode(ctx, `{"tableName":"biz_customer","business":"客户","fields":[{"name":"name","title":"名称","type":"string","inForm":true}]}`)
	if err != nil {
		t.Fatal(err)
	}
	res := got.(*BusinessCodeResult)
	if res.OK || res.Error == nil || res.Error.ErrCode != "WORKSPACE_REQUIRED" {
		t.Fatalf("unconfigured project used host roots: %+v", res)
	}
	var count int64
	global.DB.Model(&model.GenAudit{}).Count(&count)
	if count != 0 {
		t.Fatalf("unconfigured workspace created audit: %d", count)
	}
	assertNoGeneratedModel(t)
}

func TestRollbackReportsUnrestoredManualChange(t *testing.T) {
	setupCodegenBusinessDB(t)
	seedBizCustomer(t)
	dry := mustDryRun(t)
	modelPath := filepath.Join(global.Config.Codegen.ServerDir, "internal/model/customer.go")
	testWriteFailpoint = func(p string) error {
		if p == "internal/service/customer.go" {
			if err := os.WriteFile(modelPath, []byte("edited during failed apply"), 0o644); err != nil {
				return err
			}
			return errors.New("injected failure after concurrent edit")
		}
		return nil
	}
	res := applyReviewedBatch(t, dry.BatchID, 1, 1)
	if res.OK || res.Error == nil || res.Error.ErrCode != "ROLLBACK_FAILED" {
		t.Fatalf("incomplete restoration reported as complete: %+v", res)
	}
	var audit model.GenAudit
	if err := global.DB.Where("batch_id = ?", dry.BatchID).First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.Status != "rollback_failed" {
		t.Fatalf("incomplete rollback status: %s", audit.Status)
	}
	data, err := os.ReadFile(modelPath)
	if err != nil || string(data) != "edited during failed apply" {
		t.Fatalf("manual change was overwritten: %q %v", data, err)
	}
}
