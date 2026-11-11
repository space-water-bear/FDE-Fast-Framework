package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/runner"
	"fde-admin/internal/workspace"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func developmentGitHead(ctx context.Context, path string) (string, error) {
	raw, err := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--verify", "HEAD^{commit}").Output()
	if err != nil {
		return "", errors.New("项目源码基线不可用")
	}
	return strings.TrimSpace(string(raw)), nil
}

func ensureDevelopmentProjectBaseline(ctx context.Context, tx *gorm.DB, project *model.DevProject) error {
	if project.PromotionRunKey != "" {
		return errors.New("项目基线正在提升，请稍后重试")
	}
	head, err := developmentGitHead(ctx, project.WorkspacePath)
	if err != nil {
		return err
	}
	dirty, err := exec.CommandContext(ctx, "git", "-C", project.WorkspacePath, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return err
	}
	if len(dirty) != 0 {
		return errors.New("项目基线含未提交源码，不能静默遗漏人工修改")
	}
	hash, err := runner.SnapshotHash(project.WorkspacePath)
	if err != nil {
		return err
	}
	if project.CurrentCommit == "" {
		project.CurrentCommit, project.BaselineHash = head, hash
		return tx.Model(project).Updates(map[string]any{"current_commit": head, "baseline_hash": hash}).Error
	}
	if project.CurrentCommit != head || project.BaselineHash != hash {
		return errors.New("项目源码已在批准基线之外变化，请先核对并接管")
	}
	return nil
}

func verifyDevelopmentPromotion(ctx context.Context, run model.DevRun) error {
	if run.State != "succeeded" || run.CancelRequested || run.SourceHash == "" || run.WorkspacePath == "" || run.WorkspaceBaseCommit == "" {
		return errors.New("只有未取消且完成检查的源码候选可以提升")
	}
	hash, err := runner.SnapshotHash(run.WorkspacePath)
	if err != nil || hash != run.SourceHash {
		return errors.New("候选源码在检查后变化，请重新检查")
	}
	var report runner.Report
	if json.Unmarshal([]byte(run.CheckResult), &report) != nil || !report.Success || report.Profile != runner.Profile || report.SnapshotHash != hash || report.ReportPath == "" {
		return errors.New("候选缺少匹配的完整检查报告")
	}
	raw, err := os.ReadFile(report.ReportPath)
	if err != nil {
		return err
	}
	var saved runner.Report
	if json.Unmarshal(raw, &saved) != nil {
		return errors.New("磁盘检查报告无效")
	}
	expected, _ := json.Marshal(report)
	actual, _ := json.Marshal(saved)
	if string(expected) != string(actual) {
		return errors.New("检查报告与候选记录不一致")
	}
	names := []string{"go-version", "node-version", "pnpm-version", "go-dependencies", "web-dependencies", "go-format", "go-test", "go-build", "cli-build", "web-test", "typescript", "vite-build"}
	if len(report.Steps) != len(names) || report.GOOS != "linux" || (report.GOARCH != "arm64" && report.GOARCH != "amd64") || !strings.HasPrefix(report.ImageID, "sha256:") || report.FinishedAt.IsZero() {
		return errors.New("检查配置或执行步骤不完整")
	}
	for i, name := range names {
		if report.Steps[i].Name != name || report.Steps[i].ExitCode != 0 {
			return fmt.Errorf("检查步骤未通过: %s", name)
		}
	}
	if report.Toolchain["go"] != "1.26.0" || report.Toolchain["node"] != "22.22.3" || report.Toolchain["pnpm"] != "11.23.0" {
		return errors.New("检查工具链与固定配置不匹配")
	}
	artifacts, err := runner.HashTree(report.ArtifactsDir)
	if err != nil || len(artifacts) != len(report.Artifacts) {
		return errors.New("检查制品已变化")
	}
	for path, digest := range artifacts {
		if report.Artifacts[path] != digest {
			return errors.New("检查制品哈希不匹配")
		}
	}
	for _, path := range []string{"server", "fde", "web/index.html"} {
		if artifacts[path] == "" {
			return fmt.Errorf("缺少检查制品: %s", path)
		}
	}
	return ctx.Err()
}

func PromoteDevRun(ctx context.Context, scope EngineeringScope, key string, approve bool) (*model.DevProject, error) {
	if !approve {
		return nil, errors.New("提升基线需要明确人工批准")
	}
	run, _, err := GetDevRun(scope, key)
	if err != nil {
		return nil, err
	}
	project, err := EngineeringProject(scope)
	if err != nil {
		return nil, err
	}
	if run.PromotedAt != nil {
		if project.LastPromotedRunID == run.ID && project.CurrentCommit == run.PromotedCommit {
			return project, nil
		}
		return nil, errors.New("此候选已提升过，当前项目基线已变化")
	}
	if err := verifyDevelopmentPromotion(ctx, *run); err != nil {
		return nil, err
	}
	manifest, err := readDevelopmentOwnership(run.WorkspacePath)
	if err != nil {
		return nil, err
	}
	if len(manifest.Files) == 0 {
		return nil, errors.New("候选缺少业务文件所有权清单")
	}
	if err := validateDevelopmentOwnership(*run, manifest); err != nil {
		return nil, err
	}
	paths, err := developmentPromotionPaths(ctx, *run, manifest)
	if err != nil {
		return nil, err
	}
	err = global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(project, scope.ProjectID).Error; err != nil {
			return err
		}
		if err := ensureDevelopmentProjectBaseline(ctx, tx, project); err != nil {
			return err
		}
		if project.CurrentCommit != run.BaseCommit || project.BaselineRevision != run.BaseProjectRevision {
			return errors.New("候选基于旧项目基线，不能覆盖当前基线")
		}
		var busy int64
		if err := tx.Model(&model.DevRun{}).Where("project_id = ? AND state IN ?", scope.ProjectID, []string{"queued", "running"}).Count(&busy).Error; err != nil {
			return err
		}
		if busy != 0 {
			return errors.New("项目仍有排队或运行任务，请先结束这些任务")
		}
		reserved := tx.Model(&model.DevProject{}).Where("id = ? AND baseline_revision = ? AND current_commit = ? AND promotion_run_key = ?", project.ID, run.BaseProjectRevision, run.BaseCommit, "").Update("promotion_run_key", run.Key)
		if reserved.Error != nil {
			return reserved.Error
		}
		if reserved.RowsAffected != 1 {
			return errors.New("项目基线已变化或正被提升")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	reserved := true
	defer func() {
		if reserved {
			_ = global.DB.Model(&model.DevProject{}).Where("id = ? AND promotion_run_key = ?", project.ID, run.Key).Update("promotion_run_key", "").Error
		}
	}()
	if len(paths) > 0 {
		args := append([]string{"-C", run.WorkspacePath, "add", "--all", "--"}, paths...)
		if err := exec.CommandContext(ctx, "git", args...).Run(); err != nil {
			return nil, err
		}
	}
	changed, err := exec.CommandContext(ctx, "git", "-C", run.WorkspacePath, "diff", "--cached", "--name-only").Output()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(changed)) != "" {
		if output, err := exec.CommandContext(ctx, "git", "-C", run.WorkspacePath, "-c", "core.hooksPath=/dev/null", "-c", "user.name=FDE", "-c", "user.email=fde@localhost", "commit", "--quiet", "-m", "Approve business candidate "+run.Key).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("冻结候选失败: %s: %w", strings.TrimSpace(string(output)), err)
		}
	}
	acceptedCommit, err := developmentGitHead(ctx, run.WorkspacePath)
	if err != nil {
		return nil, err
	}
	if err := verifyDevelopmentPromotion(ctx, *run); err != nil {
		return nil, err
	}
	root := global.Config.Development.WorkspaceDir
	if root == "" {
		root = "./data/workspaces"
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	baselineRoot := filepath.Join(root, "baselines", fmt.Sprint(project.ID))
	baselineKey := uuid.NewString()
	destination, err := developmentWorkspaceDestination(baselineRoot, project.WorkspacePath, baselineKey)
	if err != nil {
		return nil, err
	}
	if _, err := developmentWorkspaceDestination(baselineRoot, run.WorkspacePath, baselineKey); err != nil {
		return nil, err
	}
	exported, err := workspace.Export(ctx, run.WorkspacePath, acceptedCommit, destination)
	if err != nil {
		return nil, err
	}
	installed := false
	defer func() {
		if !installed {
			_ = os.RemoveAll(exported.Path)
		}
	}()
	copiedHash, err := runner.SnapshotHash(exported.Path)
	if err != nil || copiedHash != run.SourceHash {
		return nil, errors.New("提升后的源码快照与检查候选不一致")
	}
	now := time.Now().UTC()
	err = global.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.DevProject{}).Where("id = ? AND baseline_revision = ? AND current_commit = ? AND promotion_run_key = ?", project.ID, run.BaseProjectRevision, run.BaseCommit, run.Key).Updates(map[string]any{"workspace_path": exported.Path, "current_commit": exported.BaseCommit, "baseline_hash": run.SourceHash, "baseline_revision": run.BaseProjectRevision + 1, "last_promoted_run_id": run.ID, "promotion_run_key": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("项目基线已变化，提升已停止")
		}
		saved := tx.Model(&model.DevRun{}).Where("id = ? AND state = ? AND cancel_requested = ? AND promoted_at IS NULL", run.ID, "succeeded", false).Updates(map[string]any{"promoted_at": now, "promoted_commit": exported.BaseCommit})
		if saved.Error != nil {
			return saved.Error
		}
		if saved.RowsAffected != 1 {
			return errors.New("候选状态已变化")
		}
		audit := map[string]any{"actorId": scope.ActorID, "projectId": scope.ProjectID, "previousCommit": run.BaseCommit, "acceptedCommit": acceptedCommit, "currentCommit": exported.BaseCommit, "sourceHash": run.SourceHash, "revision": run.BaseProjectRevision + 1, "ownershipHash": ownershipManifestHash(manifest)}
		return tx.Create(&model.DevRunEvent{RunID: run.ID, Kind: "baseline_promoted", Message: developmentJSON(audit)}).Error
	})
	if err != nil {
		return nil, err
	}
	installed, reserved = true, false
	if err := global.DB.First(project, project.ID).Error; err != nil {
		return nil, err
	}
	return project, nil
}

func ownershipManifestHash(manifest developmentOwnershipManifest) string {
	raw, _ := json.Marshal(manifest)
	return sha256Hex(string(raw))
}

// Promotion changes the project pointer and run marker in one final transaction.
// At startup a verified old pointer can release its abandoned reservation. An
// already committed promotion keeps its pointer and promotion history intact.
func recoverDevelopmentBaselineReservations(tx *gorm.DB) error {
	var projects []model.DevProject
	if err := tx.Where("promotion_run_key <> ?", "").Find(&projects).Error; err != nil {
		return err
	}
	for _, project := range projects {
		var run model.DevRun
		if err := tx.Where("key = ? AND project_id = ?", project.PromotionRunKey, project.ID).First(&run).Error; err != nil {
			return fmt.Errorf("项目 %d 的基线提升归属无法核对: %w", project.ID, err)
		}
		if run.State != "succeeded" || run.CancelRequested {
			return fmt.Errorf("项目 %d 的基线提升状态不确定，保留预留以便核对", project.ID)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := verifyFrozenDevelopmentBaseline(ctx, project)
		cancel()
		if err != nil {
			return fmt.Errorf("项目 %d 的原基线无法核对，保留提升预留: %w", project.ID, err)
		}
		kind := "baseline_promotion_interrupted"
		if run.PromotedAt != nil {
			if project.LastPromotedRunID != run.ID || project.CurrentCommit != run.PromotedCommit || project.BaselineRevision != run.BaseProjectRevision+1 || project.BaselineHash != run.SourceHash {
				return fmt.Errorf("项目 %d 的已提升标记与当前基线不一致", project.ID)
			}
			kind = "baseline_promotion_reconciled"
		} else if project.CurrentCommit != run.BaseCommit || project.BaselineRevision != run.BaseProjectRevision || project.LastPromotedRunID == run.ID {
			return fmt.Errorf("项目 %d 的提升尚未确认，保留当前指针与预留", project.ID)
		}
		result := tx.Model(&model.DevProject{}).Where("id = ? AND current_commit = ? AND baseline_revision = ? AND promotion_run_key = ?", project.ID, project.CurrentCommit, project.BaselineRevision, project.PromotionRunKey).Update("promotion_run_key", "")
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("项目基线在恢复期间变化")
		}
		audit := map[string]any{"actorId": run.ActorID, "projectId": project.ID, "reservation": project.PromotionRunKey, "currentCommit": project.CurrentCommit, "baselineHash": project.BaselineHash, "promoted": run.PromotedAt != nil}
		if err := tx.Create(&model.DevRunEvent{RunID: run.ID, Kind: kind, Message: developmentJSON(audit)}).Error; err != nil {
			return err
		}
	}
	return nil
}

func verifyFrozenDevelopmentBaseline(ctx context.Context, project model.DevProject) error {
	if project.CurrentCommit == "" || project.BaselineHash == "" || project.WorkspacePath == "" {
		return errors.New("缺少已批准基线记录")
	}
	head, err := developmentGitHead(ctx, project.WorkspacePath)
	if err != nil {
		return err
	}
	if head != project.CurrentCommit {
		return errors.New("项目Git基线已变化")
	}
	dirty, err := exec.CommandContext(ctx, "git", "-C", project.WorkspacePath, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return err
	}
	if len(dirty) != 0 {
		return errors.New("项目含未提交源码")
	}
	hash, err := runner.SnapshotHash(project.WorkspacePath)
	if err != nil {
		return err
	}
	if hash != project.BaselineHash {
		return errors.New("项目源码哈希与批准基线不一致")
	}
	return nil
}
