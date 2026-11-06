package service

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func TestDevelopmentToolsGenerateRuntimeAssetsWithoutTouchingBaseline(t *testing.T) {
	scope, _, repo := setupDevelopmentRunTest(t)
	run := model.DevRun{ProjectID: scope.ProjectID, ActorID: scope.ActorID, WorkspacePath: repo}
	// No host project/global roots are used by the run's scoped generator.
	args := `{"tableName":"biz_tools_ticket","business":"维修登记","fields":[{"name":"ticket_no","title":"登记号","type":"string","required":true,"inList":true,"inForm":true,"rules":{"maxLength":40}}]}`
	result, err := generateDevelopmentBusiness(context.Background(), run, args)
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["written"] != true {
		t.Fatal("generation did not apply to run workspace")
	}
	view, err := os.ReadFile(filepath.Join(repo, "web/src/views/generated/tools_ticket/index.vue"))
	if err != nil || !strings.Contains(string(view), "FormRender") || strings.Contains(string(view), "<el-form ref=") {
		t.Fatalf("managed page is not using shared schema form: %v", err)
	}
	api, _ := os.ReadFile(filepath.Join(repo, "server/internal/api/tools_ticket.go"))
	if !strings.Contains(string(api), "GeneratedBusinessSchemaJSON") || !strings.Contains(string(api), "BindBusinessJSON") {
		t.Fatal("backend validation is not bound to compiled schema")
	}
	router, _ := os.ReadFile(filepath.Join(repo, "server/internal/router/tools_ticket.go"))
	if !strings.Contains(string(router), "RegisterGeneratedBusiness") {
		t.Fatal("compiled asset registration missing")
	}
	raw, _ := os.ReadFile(filepath.Join(repo, "fde/assets/business.json"))
	var assets BusinessInstallationAssets
	if err := json.Unmarshal(raw, &assets); err != nil || len(assets.Objects) != 1 || assets.Objects[0].ObjectKey != "biz_tools_ticket" {
		t.Fatalf("missing target metadata: %s %v", raw, err)
	}
	if _, err := generateDevelopmentBusiness(context.Background(), run, args); err == nil {
		t.Fatal("generator overwrote existing custom module")
	}
	if _, err := generateDevelopmentBusiness(context.Background(), run, `{"tableName":"sys_users","business":"覆盖用户","fields":[{"name":"name","title":"名称","type":"string","inForm":true}]}`); err == nil {
		t.Fatal("managed generator targeted system data")
	}
}

func TestDevelopmentPatchProtectsControlCodeAndSourceBaselines(t *testing.T) {
	_, _, repo := setupDevelopmentRunTest(t)
	path := "server/internal/service/protected.go"
	full := filepath.Join(repo, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("package service\n")
	if err := os.WriteFile(full, original, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "--all"}, {"-c", "user.name=Test", "-c", "user.email=test@localhost", "commit", "--quiet", "-m", "protected control"}} {
		if err := exec.Command("git", append([]string{"-C", repo}, args...)...).Run(); err != nil {
			t.Fatal(err)
		}
	}
	base, _ := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	run := model.DevRun{WorkspacePath: repo, WorkspaceBaseCommit: strings.TrimSpace(string(base))}
	if _, err := applyDevelopmentChanges(run, []developmentChange{{Path: path, ExpectedHash: sha256Hex(string(original)), Content: "package service\nvar altered = true\n"}}); err == nil {
		t.Fatal("allowed overwrite of baseline control file")
	}
	for _, path := range []string{"server/internal/initialize/runtime.go", "server/internal/middleware/casbin.go", "server/internal/runner/check.go", "server/config.yaml", "../outside.go"} {
		if allowedDevelopmentPath(path, true) == nil {
			t.Fatalf("allowed control write: %s", path)
		}
	}
	newPath := "server/internal/service/customer_rule.go"
	if _, err := applyDevelopmentChanges(run, []developmentChange{{Path: newPath, ExpectedHash: "missing", Content: "package service\nfunc CustomerRule( )string{return \"ok\"}\n"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(repo, newPath))
	if !strings.Contains(string(before), "func CustomerRule() string") {
		t.Fatal("Go patch was not formatted")
	}
	if _, err := applyDevelopmentChanges(run, []developmentChange{{Path: newPath, ExpectedHash: "stale", Content: "package service\n"}}); err == nil {
		t.Fatal("stale file hash was accepted")
	}
	after, _ := os.ReadFile(filepath.Join(repo, newPath))
	if string(after) != string(before) {
		t.Fatal("failed patch mutated file")
	}
}

func TestDevelopmentSnapshotDoesNotCountFrozenRequirementAsAgentCode(t *testing.T) {
	_, _, repo := setupDevelopmentRunTest(t)
	base, _ := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	run := model.DevRun{WorkspacePath: repo, WorkspaceBaseCommit: strings.TrimSpace(string(base))}
	if err := writeRootedFile(repo, "fde/requirements/task.json", []byte(`{"requirement":"host frozen"}`)); err != nil {
		t.Fatal(err)
	}
	diff, before, err := DevelopmentRunSnapshot(context.Background(), run)
	if err != nil || diff != "" || before == "" {
		t.Fatalf("host requirement was treated as completed business code: %q %v", diff, err)
	}
	if err := writeRootedFile(repo, "server/internal/service/business.go", []byte("package service\nvar BusinessChanged = true\n")); err != nil {
		t.Fatal(err)
	}
	diff, after, err := DevelopmentRunSnapshot(context.Background(), run)
	if err != nil || !strings.Contains(diff, "BusinessChanged") || strings.Contains(diff, "host frozen") || after == before {
		t.Fatalf("business source change was not captured: %q %v", diff, err)
	}
}

func TestDevelopmentGeneratorRejectsUnsupportedLinkedDraftWithoutLosingRules(t *testing.T) {
	scope, _, repo := setupDevelopmentRunTest(t)
	if err := global.DB.AutoMigrate(&model.BizObject{}, &model.BizField{}, &model.FormSchema{}); err != nil {
		t.Fatal(err)
	}
	input := FormgenObjectInput{ObjectKey: "biz_linked", Name: "联动对象", Fields: []FormgenField{{Name: "active", Title: "启用", Type: "bool", InForm: true}, {Name: "name", Title: "名称", Type: "string", InForm: true}}, Linkages: []FormgenLinkage{{When: map[string]any{"field": "active", "eq": true}, Then: map[string]any{"field": "name", "required": true}}}}
	saved, err := CreateBizObjectInProject(scope.ProjectID, input, scope.ActorID)
	if err != nil || !saved.OK {
		t.Fatalf("draft fixture invalid: %+v %v", saved, err)
	}
	run := model.DevRun{ProjectID: scope.ProjectID, ActorID: scope.ActorID, WorkspacePath: repo}
	if _, err := generateDevelopmentBusiness(context.Background(), run, `{"objectKey":"biz_linked"}`); err == nil || !strings.Contains(err.Error(), "联动") {
		t.Fatal("generator silently discarded linkage rules")
	}
	if _, err := os.Stat(filepath.Join(repo, "server/internal/model/linked.go")); !os.IsNotExist(err) {
		t.Fatal("unsupported draft wrote files before rejection")
	}
}
