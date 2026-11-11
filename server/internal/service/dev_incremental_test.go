package service

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fde-admin/internal/config"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/runner"
	"fde-admin/internal/utils"
	"fde-admin/internal/workspace"
	"github.com/google/uuid"
)

const incrementalCreateJSON = `{"tableName":"biz_incremental_ticket","business":"维修登记","fields":[{"name":"ticket_no","title":"登记号","type":"string","required":true,"inForm":true,"inList":true}]}`
const incrementalUpdateJSON = `{"objectKey":"biz_incremental_ticket","mode":"update","baseSpecVersion":1,"fields":[{"name":"priority","title":"优先级","type":"enum","required":true,"inForm":true,"inList":true,"rules":{"enum":["normal","urgent"],"default":"normal"}}]}`

func candidateRun(t *testing.T, scope EngineeringScope, agent model.AiAgent) model.DevRun {
	t.Helper()
	queued, err := CreateDevRun(context.Background(), scope, "new business", agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	var project model.DevProject
	if err := global.DB.First(&project, scope.ProjectID).Error; err != nil {
		t.Fatal(err)
	}
	exported, err := workspace.Export(context.Background(), project.WorkspacePath, queued.BaseCommit, filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	queued.WorkspacePath, queued.WorkspaceBaseCommit = exported.Path, exported.BaseCommit
	queued.State = "running"
	if err := global.DB.Model(queued).Updates(map[string]any{"workspace_path": queued.WorkspacePath, "workspace_base_commit": queued.WorkspaceBaseCommit, "state": "running"}).Error; err != nil {
		t.Fatal(err)
	}
	return *queued
}

func certifyCandidate(t *testing.T, run *model.DevRun) {
	t.Helper()
	_, hash, err := DevelopmentRunSnapshot(context.Background(), *run)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := t.TempDir()
	for _, path := range []string{"server", "fde", "web/index.html"} {
		full := filepath.Join(artifacts, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("fixed test artifact "+path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files, err := runner.HashTree(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	report := runner.Report{ID: uuid.NewString(), Profile: runner.Profile, Success: true, SnapshotHash: hash, GOOS: "linux", GOARCH: "arm64", ImageID: "sha256:fixed-test-image", ArtifactsDir: artifacts, Artifacts: files, Toolchain: map[string]string{"go": "1.26.0", "node": "22.22.3", "pnpm": "11.23.0"}, ReportPath: filepath.Join(t.TempDir(), "report.json"), FinishedAt: time.Now().UTC()}
	for _, name := range []string{"go-version", "node-version", "pnpm-version", "go-dependencies", "web-dependencies", "go-format", "go-test", "go-build", "cli-build", "web-test", "typescript", "vite-build"} {
		report.Steps = append(report.Steps, runner.Step{Name: name})
	}
	raw, _ := json.Marshal(report)
	if err := os.WriteFile(report.ReportPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	run.SourceHash, run.CheckResult, run.State = hash, string(raw), "succeeded"
	if err := global.DB.Model(run).Updates(map[string]any{"source_hash": hash, "check_result": string(raw), "state": "succeeded"}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestPromotionMakesOwnedModuleAvailableToNextTask(t *testing.T) {
	scope, agent, original := setupDevelopmentRunTest(t)
	global.Config = &config.Config{Development: config.Development{WorkspaceDir: t.TempDir()}}
	run := candidateRun(t, scope, agent)
	if _, err := generateDevelopmentBusiness(context.Background(), run, incrementalCreateJSON); err != nil {
		t.Fatal(err)
	}
	servicePath := "server/internal/service/incremental_ticket.go"
	content, err := readRootedFile(run.WorkspacePath, servicePath)
	if err != nil {
		t.Fatal(err)
	}
	custom := string(content) + "\n// Approved custom BLOCKED rule must survive later schema generation.\nfunc TicketCustomRule(value string) bool { return value != \"BLOCKED\" }\n"
	if _, err := applyDevelopmentChanges(run, []developmentChange{{Path: servicePath, ExpectedHash: sha256Hex(string(content)), Content: custom}}); err != nil {
		t.Fatal(err)
	}
	customBytes, _ := readRootedFile(run.WorkspacePath, servicePath)
	certifyCandidate(t, &run)
	project, err := PromoteDevRun(context.Background(), scope, run.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	if project.BaselineRevision != 1 || project.CurrentCommit == "" || project.BaselineHash != run.SourceHash || project.LastPromotedRunID != run.ID || project.WorkspacePath == original {
		t.Fatalf("promotion not recorded: %+v", project)
	}
	if again, err := PromoteDevRun(context.Background(), scope, run.Key, true); err != nil || again.CurrentCommit != project.CurrentCommit {
		t.Fatalf("promotion retry: %+v %v", again, err)
	}
	second := candidateRun(t, scope, agent)
	if second.BaseCommit != project.CurrentCommit || second.BaseProjectRevision != 1 {
		t.Fatalf("second task lost promoted baseline: %+v", second)
	}
	if _, err := generateDevelopmentBusiness(context.Background(), second, incrementalUpdateJSON); err != nil {
		t.Fatal(err)
	}
	preserved, err := readRootedFile(second.WorkspacePath, servicePath)
	if err != nil || string(preserved) != string(customBytes) {
		t.Fatalf("custom business service overwritten: %v", err)
	}
	modelSource, err := readRootedFile(second.WorkspacePath, "server/internal/model/incremental_ticket.go")
	if err != nil || !strings.Contains(string(modelSource), "Priority") || !strings.Contains(string(modelSource), "default:normal") {
		t.Fatalf("new model field missing: %s %v", modelSource, err)
	}
	raw, err := readRootedFile(second.WorkspacePath, "fde/assets/business.json")
	if err != nil {
		t.Fatal(err)
	}
	var assets BusinessInstallationAssets
	if err := json.Unmarshal(raw, &assets); err != nil {
		t.Fatal(err)
	}
	schema := assets.Objects[0].Schema
	if version, _ := developmentSchemaVersion(schema["version"]); version != 2 {
		t.Fatalf("spec not advanced: %v", schema)
	}
	priority := schema["fields"].(map[string]any)["priority"].(map[string]any)
	if priority["default"] != "normal" || priority["type"] != "string" || priority["x-ui"].(map[string]any)["control"] != "select" {
		t.Fatalf("enum/default projection invalid: %v", priority)
	}
	encoded, _ := json.Marshal(schema)
	if err := utils.ValidateBusinessValues(string(encoded), map[string]any{"ticketNo": "valid", "priority": "urgent"}); err != nil {
		t.Fatal(err)
	}
	if err := utils.ValidateBusinessValues(string(encoded), map[string]any{"ticketNo": "valid", "priority": "unsupported"}); err == nil {
		t.Fatal("backend enum accepted unknown option")
	}
	if _, err := generateDevelopmentBusiness(context.Background(), second, incrementalUpdateJSON); err == nil {
		t.Fatal("stale spec version accepted")
	}
	if _, err := applyDevelopmentChanges(second, []developmentChange{{Path: servicePath, ExpectedHash: sha256Hex(string(preserved)), Content: string(preserved) + "\nfunc AnotherRule() bool { return true }\n"}}); err != nil {
		t.Fatalf("owned custom code not editable across tasks: %v", err)
	}
	originalReadme, err := os.ReadFile(filepath.Join(original, "README.md"))
	if err != nil || string(originalReadme) != "baseline\n" {
		t.Fatal("promotion rewrote original source")
	}
}

func TestPromotionRejectsConcurrentQueueAndStaleCandidate(t *testing.T) {
	scope, agent, _ := setupDevelopmentRunTest(t)
	global.Config = &config.Config{Development: config.Development{WorkspaceDir: t.TempDir()}}
	run := candidateRun(t, scope, agent)
	if _, err := generateDevelopmentBusiness(context.Background(), run, incrementalCreateJSON); err != nil {
		t.Fatal(err)
	}
	certifyCandidate(t, &run)
	queued, err := CreateDevRun(context.Background(), scope, "waiting task", agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PromoteDevRun(context.Background(), scope, run.Key, true); err == nil {
		t.Fatal("promotion raced a queued task")
	}
	if err := CancelDevRun(scope, queued.Key); err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Model(&model.DevProject{}).Where("id = ?", scope.ProjectID).Update("baseline_revision", 2).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := PromoteDevRun(context.Background(), scope, run.Key, true); err == nil {
		t.Fatal("stale candidate replaced current baseline")
	}
}

func TestOwnershipRejectsForgedFrameworkTakeover(t *testing.T) {
	_, _, repo := setupDevelopmentRunTest(t)
	path := "server/internal/service/framework.go"
	if err := writeRootedFile(repo, path, []byte("package service\n")); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "--all"}, {"-c", "user.name=Test", "-c", "user.email=test@localhost", "commit", "--quiet", "-m", "framework"}} {
		if err := exec.Command("git", append([]string{"-C", repo}, args...)...).Run(); err != nil {
			t.Fatal(err)
		}
	}
	head, err := developmentGitHead(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	manifest := emptyDevelopmentOwnership()
	manifest.Files[path] = developmentFileOwnership{Kind: "custom", CurrentHash: sha256Hex("package service\n")}
	if err := saveDevelopmentOwnership(repo, manifest); err != nil {
		t.Fatal(err)
	}
	run := model.DevRun{WorkspacePath: repo, WorkspaceBaseCommit: head}
	if _, err := applyDevelopmentChanges(run, []developmentChange{{Path: path, ExpectedHash: sha256Hex("package service\n"), Content: "package service\nvar Stolen = true\n"}}); err == nil {
		t.Fatal("forged mutable manifest took over a framework file")
	}
}
