package service

import (
	"context"
	"fde-admin/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func setupDevelopmentRunTest(t *testing.T) (EngineeringScope, model.AiAgent, string) {
	t.Helper()
	scope, _ := setupEngineeringTest(t)
	repo := t.TempDir()
	for name, content := range map[string]string{"LICENSE": "MIT", "server/go.mod": "module example.invalid/customer\n", "web/package.json": "{}", "README.md": "baseline\n"} {
		path := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "--all"}, {"-c", "user.name=Test", "-c", "user.email=test@localhost", "commit", "--quiet", "-m", "baseline"}} {
		if err := exec.Command("git", append([]string{"-C", repo}, args...)...).Run(); err != nil {
			t.Fatal(err)
		}
	}
	if err := global.DB.Model(&model.DevProject{}).Where("id = ?", scope.ProjectID).Update("workspace_path", repo).Error; err != nil {
		t.Fatal(err)
	}
	provider := model.AiProvider{Name: "test-provider", APIKey: "test-key", Model: "test-model", Enabled: true, APIMode: "chat"}
	if err := global.DB.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	agent := model.AiAgent{Name: "development-agent", ProviderID: provider.ID, Enabled: true}
	if err := global.DB.Create(&agent).Error; err != nil {
		t.Fatal(err)
	}
	return scope, agent, repo
}

func TestCreateDevelopmentRunRequiresAgentSelection(t *testing.T) {
	scope, _, _ := setupDevelopmentRunTest(t)
	if _, err := CreateDevRun(context.Background(), scope, "Add a customer module"); err == nil {
		t.Fatal("queued engineering task without an explicit agent")
	}
}

func TestDevelopmentQueueClaimSerializesProjectAndRecoversWithoutReplay(t *testing.T) {
	scope, selected, _ := setupDevelopmentRunTest(t)
	first, err := CreateDevRun(context.Background(), scope, "first", selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateDevRun(context.Background(), scope, "second", selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	canceled, err := CreateDevRun(context.Background(), scope, "cancel queued", selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := CancelDevRun(scope, canceled.Key); err != nil {
		t.Fatal(err)
	}
	claimed, err := claimNextDevelopmentRun()
	if err != nil || claimed == nil || claimed.ID != first.ID {
		t.Fatalf("first claim: %+v %v", claimed, err)
	}
	if got, err := claimNextDevelopmentRun(); err != nil || got != nil {
		t.Fatalf("claimed two running tasks for same project: %+v %v", got, err)
	}
	workspace := t.TempDir()
	if err := global.DB.Model(&model.DevRun{}).Where("id = ?", first.ID).Updates(map[string]any{"workspace_path": workspace, "diff": "partial change", "attempts": 1}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := RecoverInterruptedDevelopmentRuns(); err != nil {
			t.Fatal(err)
		}
	}
	interrupted, events, err := GetDevRun(scope, first.Key)
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.State != "interrupted" || interrupted.WorkspacePath != workspace || interrupted.Diff != "partial change" || interrupted.Attempts != 1 {
		t.Fatalf("restart discarded/replayed in-flight work: %+v", interrupted)
	}
	interruptedEvents := 0
	for _, event := range events {
		if event.Kind == "interrupted" {
			interruptedEvents++
		}
	}
	if interruptedEvents != 1 {
		t.Fatalf("recovery is not idempotent: %d interruption events", interruptedEvents)
	}
	next, err := claimNextDevelopmentRun()
	if err != nil || next == nil || next.ID != second.ID {
		t.Fatalf("replayed interrupted/canceled task: %+v %v", next, err)
	}
}

func TestDevelopmentRunWorkspaceCannotLiveInsideProjectSource(t *testing.T) {
	scope, selected, repo := setupDevelopmentRunTest(t)
	previous := global.Config
	global.Config = &config.Config{Development: config.Development{WorkspaceDir: repo}}
	t.Cleanup(func() { global.Config = previous })
	run, err := CreateDevRun(context.Background(), scope, "new module", selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = processNextDevelopmentRun(context.Background(), developmentTestServices(func(context.Context, model.DevRun) (any, error) { called = true; return nil, nil }))
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := GetDevRun(scope, run.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "failed" || !strings.Contains(got.Error, "源码目录之外") || called {
		t.Fatalf("execution workspace escaped isolation: %+v checked=%v", got, called)
	}
}
