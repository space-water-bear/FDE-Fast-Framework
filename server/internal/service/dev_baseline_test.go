package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fde-admin/internal/config"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func TestPromotionRequiresIndependentApproval(t *testing.T) {
	scope, _, _ := setupDevelopmentRunTest(t)
	if _, err := PromoteDevRun(context.Background(), scope, "candidate", false); err == nil || !strings.Contains(err.Error(), "批准") {
		t.Fatalf("missing explicit approval gate: %v", err)
	}
}

func interruptedPromotionFixture(t *testing.T) (EngineeringScope, model.DevRun, model.DevProject) {
	t.Helper()
	scope, agent, _ := setupDevelopmentRunTest(t)
	global.Config = &config.Config{Development: config.Development{WorkspaceDir: t.TempDir()}}
	run := candidateRun(t, scope, agent)
	if _, err := generateDevelopmentBusiness(context.Background(), run, incrementalCreateJSON); err != nil {
		t.Fatal(err)
	}
	certifyCandidate(t, &run)
	var project model.DevProject
	if err := global.DB.First(&project, scope.ProjectID).Error; err != nil {
		t.Fatal(err)
	}
	return scope, run, project
}

func TestStartupRecoversInterruptedPromotionWithoutPromotingCandidate(t *testing.T) {
	scope, run, before := interruptedPromotionFixture(t)
	if err := global.DB.Model(&model.DevProject{}).Where("id = ?", scope.ProjectID).Update("promotion_run_key", run.Key).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := RecoverInterruptedDevelopmentRuns(); err != nil {
			t.Fatal(err)
		}
	}
	var project model.DevProject
	if err := global.DB.First(&project, scope.ProjectID).Error; err != nil {
		t.Fatal(err)
	}
	if project.PromotionRunKey != "" || project.CurrentCommit != before.CurrentCommit || project.BaselineHash != before.BaselineHash || project.BaselineRevision != before.BaselineRevision || project.WorkspacePath != before.WorkspacePath {
		t.Fatalf("recovery changed approved baseline: %+v", project)
	}
	if err := global.DB.First(&run, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.PromotedAt != nil || run.PromotedCommit != "" {
		t.Fatal("startup automatically promoted an uncertain candidate")
	}
	var count int64
	if err := global.DB.Model(&model.DevRunEvent{}).Where("run_id = ? AND kind = ?", run.ID, "baseline_promotion_interrupted").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recovery audit repeated: %d", count)
	}
}

func TestStartupKeepsCompletedPromotionWhenReconcilingReservation(t *testing.T) {
	scope, run, _ := interruptedPromotionFixture(t)
	promoted, err := PromoteDevRun(context.Background(), scope, run.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Model(&model.DevProject{}).Where("id = ?", scope.ProjectID).Update("promotion_run_key", run.Key).Error; err != nil {
		t.Fatal(err)
	}
	if err := RecoverInterruptedDevelopmentRuns(); err != nil {
		t.Fatal(err)
	}
	var current model.DevProject
	if err := global.DB.First(&current, scope.ProjectID).Error; err != nil {
		t.Fatal(err)
	}
	if current.PromotionRunKey != "" || current.CurrentCommit != promoted.CurrentCommit || current.WorkspacePath != promoted.WorkspacePath || current.BaselineHash != promoted.BaselineHash || current.LastPromotedRunID != run.ID {
		t.Fatalf("committed promotion was changed: %+v", current)
	}
	if err := global.DB.First(&run, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.PromotedAt == nil || run.PromotedCommit != current.CurrentCommit {
		t.Fatal("completed promotion marker was lost")
	}
}

func TestStartupLeavesAmbiguousPromotionReservationForInspection(t *testing.T) {
	scope, run, project := interruptedPromotionFixture(t)
	if err := global.DB.Model(&model.DevProject{}).Where("id = ?", scope.ProjectID).Update("promotion_run_key", run.Key).Error; err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project.WorkspacePath, "README.md"), []byte("unapproved source change"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RecoverInterruptedDevelopmentRuns(); err == nil {
		t.Fatal("uncertain baseline was silently recovered")
	}
	var current model.DevProject
	if err := global.DB.First(&current, scope.ProjectID).Error; err != nil {
		t.Fatal(err)
	}
	if current.PromotionRunKey != run.Key || current.CurrentCommit != project.CurrentCommit || current.BaselineRevision != project.BaselineRevision {
		t.Fatal("ambiguous promotion changed the approved pointer or reservation")
	}
}
