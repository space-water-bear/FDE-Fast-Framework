package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"fde-admin/internal/config"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func setupEngineeringTest(t *testing.T) (EngineeringScope, EngineeringScope) {
	t.Helper()
	setupTestDB(t)
	if err := global.DB.AutoMigrate(&model.User{}, &model.Role{}, &model.DevProject{}, &model.DevRun{}, &model.DevRunEvent{}); err != nil {
		t.Fatal(err)
	}
	owner := model.User{Username: "project-owner", Enabled: true}
	other := model.User{Username: "other-user", Enabled: true}
	if err := global.DB.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	project := model.DevProject{Key: "customer-a", Name: "客户A", OwnerID: owner.ID}
	if err := global.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	return EngineeringScope{ActorID: owner.ID, ProjectID: project.ID}, EngineeringScope{ActorID: other.ID, ProjectID: project.ID}
}

func TestCustomerProjectExportAndDurableQueuedRun(t *testing.T) {
	owner, _ := setupEngineeringTest(t)
	repo := t.TempDir()
	for name, content := range map[string]string{"LICENSE": "MIT", "server/go.mod": "module fde-admin\n", "web/package.json": "{}"} {
		path := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "--all"}, {"-c", "user.name=Test", "-c", "user.email=test@localhost", "commit", "--quiet", "-m", "template"}} {
		if err := exec.Command("git", append([]string{"-C", repo}, args...)...).Run(); err != nil {
			t.Fatal(err)
		}
	}
	previous := global.Config
	global.Config = &config.Config{Development: config.Development{TemplateRepo: repo, TemplateRef: "HEAD", WorkspaceDir: t.TempDir()}}
	t.Cleanup(func() { global.Config = previous })
	project, err := CreateDevProject(context.Background(), owner.ActorID, "customer-b", "客户B")
	if err != nil {
		t.Fatal(err)
	}
	if project.OwnerID != owner.ActorID || project.WorkspacePath == "" || len(project.SourceCommit) != 40 {
		t.Fatalf("invalid project: %+v", project)
	}
	newScope := EngineeringScope{ActorID: owner.ActorID, ProjectID: project.ID}
	run, err := CreateDevRun(context.Background(), newScope, "新增维修登记")
	if err != nil {
		t.Fatal(err)
	}
	got, events, err := GetDevRun(newScope, run.Key)
	if err != nil || got.State != "queued" || len(events) != 1 || got.Requirement != "新增维修登记" || len(got.BaseCommit) != 40 {
		t.Fatalf("run/event not durable: %+v %+v %v", got, events, err)
	}
	if _, _, err := GetDevRun(owner, run.Key); err == nil {
		t.Fatal("run crossed project scope")
	}
	if err := CancelDevRun(newScope, run.Key); err != nil {
		t.Fatal(err)
	}
	got, events, err = GetDevRun(newScope, run.Key)
	if err != nil || got.State != "canceled" || len(events) != 2 {
		t.Fatalf("cancellation missing: %+v %+v %v", got, events, err)
	}
}

func TestEngineeringIdentityAndProjectOwnership(t *testing.T) {
	owner, other := setupEngineeringTest(t)
	if _, err := RequireEngineeringScope(context.Background()); err == nil {
		t.Fatal("accepted anonymous context")
	}
	ctx := WithEngineeringScope(context.Background(), owner)
	if got, err := RequireEngineeringScope(ctx); err != nil || got != owner {
		t.Fatalf("scope lost: %+v %v", got, err)
	}
	if _, err := EngineeringProject(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := EngineeringProject(other); err == nil {
		t.Fatal("other user accessed customer's metadata")
	}
	projects, err := ListDevProjects(other.ActorID)
	if err != nil || len(projects) != 0 {
		t.Fatalf("leaked projects: %+v %v", projects, err)
	}
	projects, err = ListDevProjects(owner.ActorID)
	if err != nil || len(projects) != 1 {
		t.Fatalf("owner missing project: %+v %v", projects, err)
	}
}

func TestEngineeringToolChecksCannotUseChatPermissionAsWritePermission(t *testing.T) {
	owner, _ := setupEngineeringTest(t)
	previous := global.Enforcer
	global.Enforcer = nil
	t.Cleanup(func() { global.Enforcer = previous })
	tool := &schemaFuncTool{name: "generate_business_code", call: func(context.Context, string) (any, error) {
		t.Fatal("executed without tool permission")
		return nil, nil
	}}
	if _, err := tool.Call(WithEngineeringScope(context.Background(), owner), `{"actorId":999,"projectId":999}`); err == nil {
		t.Fatal("missing authorization should fail closed")
	}
}

func TestRunAccessAndCancellationAreOwnerScoped(t *testing.T) {
	owner, other := setupEngineeringTest(t)
	run := model.DevRun{Key: "run-a", ActorID: owner.ActorID, ProjectID: owner.ProjectID, Requirement: "新增维修登记", State: "queued", Phase: "plan"}
	if err := global.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := GetDevRun(other, run.Key); err == nil {
		t.Fatal("other user accessed run")
	}
	if err := CancelDevRun(other, run.Key); err == nil {
		t.Fatal("other user canceled run")
	}
	if err := CancelDevRun(owner, run.Key); err != nil {
		t.Fatal(err)
	}
	if err := CancelDevRun(owner, run.Key); err == nil {
		t.Fatal("repeated cancellation should not duplicate event")
	}
	got, events, err := GetDevRun(owner, run.Key)
	if err != nil || got.State != "canceled" || len(events) != 1 {
		t.Fatalf("run/event inconsistent: %+v %+v %v", got, events, err)
	}
}
