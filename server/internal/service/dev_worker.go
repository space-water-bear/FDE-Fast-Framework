package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/microsoft/agent-framework-go/tool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/workspace"
)

const maxDevelopmentRepairs = 3

var developmentExecutionMu sync.Mutex
var developmentWorkerControl = struct {
	sync.Mutex
	started bool
	active  map[uint]context.CancelFunc
}{active: map[uint]context.CancelFunc{}}

type developmentRunServices struct {
	tools    func(model.DevRun) []tool.Tool
	checks   func(context.Context, model.DevRun) (any, error)
	snapshot func(context.Context, model.DevRun) (string, string, error)
}

func defaultDevelopmentRunServices() developmentRunServices {
	return developmentRunServices{tools: DevelopmentAgentTools, checks: RunDevelopmentChecks, snapshot: DevelopmentRunSnapshot}
}

// StartDevelopmentWorker starts one process-local consumer. HTTP request contexts
// never own this lifecycle. Restarted servers retain incomplete work as evidence.
func StartDevelopmentWorker(ctx context.Context) error {
	if global.DB == nil {
		return errors.New("开发任务数据库尚未初始化")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	developmentWorkerControl.Lock()
	defer developmentWorkerControl.Unlock()
	if developmentWorkerControl.started {
		return nil
	}
	if err := RecoverInterruptedDevelopmentRuns(); err != nil {
		return err
	}
	developmentWorkerControl.started = true
	go func() {
		defer func() {
			developmentWorkerControl.Lock()
			developmentWorkerControl.started = false
			developmentWorkerControl.Unlock()
		}()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			claimed, err := ProcessNextDevelopmentRun(ctx)
			if err != nil && ctx.Err() == nil && global.Logger != nil {
				global.Logger.Error("开发任务worker失败: " + err.Error())
			}
			if claimed {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}

func RecoverInterruptedDevelopmentRuns() error {
	return global.DB.Transaction(func(tx *gorm.DB) error {
		var running []model.DevRun
		if err := tx.Where("state = ?", "running").Find(&running).Error; err != nil {
			return err
		}
		for _, run := range running {
			result := tx.Model(&model.DevRun{}).Where("id = ? AND state = ?", run.ID, "running").Updates(map[string]any{"state": "interrupted", "error": "服务重启中断了任务；保留工作区与已有结果，不自动重放工具操作", "finished_at": time.Now().UTC()})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				continue
			}
			if err := tx.Create(&model.DevRunEvent{RunID: run.ID, Kind: "interrupted", Message: "服务重启：保留工作区和事件，未自动重试任务"}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func cancelActiveDevelopmentRun(id uint) {
	developmentWorkerControl.Lock()
	cancel := developmentWorkerControl.active[id]
	developmentWorkerControl.Unlock()
	if cancel != nil {
		cancel()
	}
}

func ProcessNextDevelopmentRun(ctx context.Context) (bool, error) {
	return processNextDevelopmentRun(ctx, defaultDevelopmentRunServices())
}
func processNextDevelopmentRun(ctx context.Context, services developmentRunServices) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if !developmentExecutionMu.TryLock() {
		return false, nil
	}
	defer developmentExecutionMu.Unlock()
	run, err := claimNextDevelopmentRun()
	if err != nil || run == nil {
		return false, err
	}
	return true, executeDevelopmentRun(ctx, *run, services)
}

func claimNextDevelopmentRun() (*model.DevRun, error) {
	var claimed *model.DevRun
	err := global.DB.Transaction(func(tx *gorm.DB) error {
		var run model.DevRun
		resultQuery := tx.Where("state = ? AND NOT EXISTS (SELECT 1 FROM sys_dev_runs AS busy WHERE busy.project_id = sys_dev_runs.project_id AND busy.state = ?)", "queued", "running").Order("id asc").Limit(1).Find(&run)
		if resultQuery.Error != nil {
			return resultQuery.Error
		}
		if resultQuery.RowsAffected == 0 {
			return nil
		}
		var project model.DevProject
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&project, run.ProjectID).Error; err != nil {
			return err
		}
		var busy int64
		if err := tx.Model(&model.DevRun{}).Where("project_id = ? AND state = ?", run.ProjectID, "running").Count(&busy).Error; err != nil {
			return err
		}
		if busy > 0 {
			return nil
		}
		now := time.Now().UTC()
		result := tx.Model(&model.DevRun{}).Where("id = ? AND state = ? AND cancel_requested = ?", run.ID, "queued", false).Updates(map[string]any{"state": "running", "phase": "plan", "started_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		run.State, run.Phase, run.StartedAt = "running", "plan", &now
		if err := tx.Create(&model.DevRunEvent{RunID: run.ID, Kind: "running", Message: "后台worker已领取任务；浏览器断开不会取消执行"}).Error; err != nil {
			return err
		}
		claimed = &run
		return nil
	})
	return claimed, err
}

func executeDevelopmentRun(parent context.Context, run model.DevRun, services developmentRunServices) (terminalErr error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	developmentWorkerControl.Lock()
	developmentWorkerControl.active[run.ID] = cancel
	developmentWorkerControl.Unlock()
	defer func() {
		developmentWorkerControl.Lock()
		delete(developmentWorkerControl.active, run.ID)
		developmentWorkerControl.Unlock()
	}()
	watchDone := make(chan struct{})
	defer close(watchDone)
	db := global.DB
	watchContext := ctx
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-watchContext.Done():
				return
			case <-watchDone:
				return
			case <-ticker.C:
				var current model.DevRun
				if db.Select("state", "cancel_requested").First(&current, run.ID).Error == nil && (current.CancelRequested || current.State != "running") {
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		if recovered := recover(); recovered != nil {
			terminalErr = fmt.Errorf("开发执行器异常: %v", recovered)
		}
		if terminalErr == nil {
			return
		}
		var current model.DevRun
		_ = db.First(&current, run.ID).Error
		state := "failed"
		if current.CancelRequested {
			state = "canceled"
		} else if parent.Err() != nil {
			state = "interrupted"
		}
		if run.WorkspacePath != "" && services.snapshot != nil {
			snapshotCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			if diff, hash, err := services.snapshot(snapshotCtx, run); err == nil {
				run.Diff, run.SourceHash = diff, hash
			}
			stop()
		}
		message := developmentTokenPattern.ReplaceAllString(limitDevelopmentText(terminalErr.Error(), 8000), "<redacted>")
		updates := map[string]any{"state": state, "error": message, "diff": run.Diff, "source_hash": run.SourceHash, "finished_at": time.Now().UTC()}
		if err := db.Model(&model.DevRun{}).Where("id = ? AND state = ?", run.ID, "running").Updates(updates).Error; err != nil {
			terminalErr = err
			return
		}
		if err := appendDevelopmentEvent(run.ID, state, message); err != nil {
			terminalErr = err
			return
		}
		terminalErr = nil
	}()
	scope := EngineeringScope{ActorID: run.ActorID, ProjectID: run.ProjectID}
	project, err := EngineeringProject(scope)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var current model.DevRun
	if err := db.First(&current, run.ID).Error; err != nil {
		return err
	}
	if current.CancelRequested {
		return context.Canceled
	}
	if global.Config == nil {
		return errors.New("开发工作区未配置")
	}
	root := global.Config.Development.WorkspaceDir
	if root == "" {
		root = "./data/workspaces"
	}
	destination, err := developmentWorkspaceDestination(root, project.WorkspacePath, run.Key)
	if err != nil {
		return err
	}
	exported, err := workspace.Export(ctx, project.WorkspacePath, run.BaseCommit, destination)
	if err != nil {
		return err
	}
	run.WorkspacePath, run.WorkspaceBaseCommit = exported.Path, exported.BaseCommit
	if err := db.Model(&model.DevRun{}).Where("id = ? AND state = ?", run.ID, "running").Updates(map[string]any{"workspace_path": run.WorkspacePath, "workspace_base_commit": run.WorkspaceBaseCommit, "phase": "edit"}).Error; err != nil {
		return err
	}
	if err := appendDevelopmentEvent(run.ID, "workspace_ready", developmentJSON(map[string]any{"baseCommit": run.BaseCommit, "workspaceBaseCommit": run.WorkspaceBaseCommit})); err != nil {
		return err
	}
	ctx = WithEngineeringScope(ctx, scope)
	ctx = WithDevelopmentRunScope(ctx, DevelopmentRunScope{RunID: run.ID, RunKey: run.Key, ActorID: run.ActorID, ProjectID: run.ProjectID, WorkspacePath: run.WorkspacePath})
	feedback, final := "", ""
	for attempt := 1; attempt <= maxDevelopmentRepairs+1; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		run.Attempts = attempt
		if err := db.Model(&model.DevRun{}).Where("id = ?", run.ID).Updates(map[string]any{"attempts": attempt, "phase": "edit"}).Error; err != nil {
			return err
		}
		final, err = runDevelopmentAgent(ctx, run, feedback, services.tools(run))
		if err != nil {
			return err
		}
		diff, hash, err := services.snapshot(ctx, run)
		if err != nil {
			return err
		}
		run.Diff, run.SourceHash = diff, hash
		if err := db.Model(&model.DevRun{}).Where("id = ?", run.ID).Updates(map[string]any{"phase": "verify", "diff": diff, "source_hash": hash}).Error; err != nil {
			return err
		}
		report, checkErr := services.checks(ctx, run)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		encoded := developmentJSON(report)
		if checkErr == nil {
			var verdict struct {
				Success      bool   `json:"success"`
				SnapshotHash string `json:"snapshotHash"`
			}
			if json.Unmarshal([]byte(encoded), &verdict) != nil || !verdict.Success {
				checkErr = errors.New("宿主检查没有返回 success=true，不能交付")
			} else if hash == "" || verdict.SnapshotHash != hash {
				checkErr = errors.New("检查报告缺少匹配的源码哈希，不能交付")
			}
		}
		if strings.TrimSpace(diff) == "" && checkErr == nil {
			checkErr = errors.New("智能体未产生源码变更，不能把原项目标记为需求已交付")
		}
		run.CheckResult = encoded
		if err := db.Model(&model.DevRun{}).Where("id = ?", run.ID).Update("check_result", encoded).Error; err != nil {
			return err
		}
		event := map[string]any{"attempt": attempt, "report": report, "success": checkErr == nil}
		if checkErr != nil {
			event["error"] = checkErr.Error()
		}
		if err := appendDevelopmentEvent(run.ID, "checks", developmentSummary(event)); err != nil {
			return err
		}
		if checkErr != nil {
			if attempt > maxDevelopmentRepairs {
				return fmt.Errorf("首次实现及最多 %d 轮修复仍未通过: %w", maxDevelopmentRepairs, checkErr)
			}
			feedback = developmentSummary(event)
			if err := appendDevelopmentEvent(run.ID, "repair_requested", fmt.Sprintf("宿主检查失败，进入第 %d/%d 轮修复", attempt, maxDevelopmentRepairs)); err != nil {
				return err
			}
			continue
		}
		afterDiff, afterHash, err := services.snapshot(ctx, run)
		if err != nil {
			return err
		}
		if hash != afterHash {
			return errors.New("检查期间源码发生变化，结果已过期")
		}
		run.Diff, run.SourceHash = afterDiff, afterHash
		run.Result = developmentJSON(map[string]any{"summary": limitDevelopmentText(final, 8000), "attempts": attempt, "baseCommit": run.BaseCommit, "workspaceBaseCommit": run.WorkspaceBaseCommit, "sourceHash": afterHash, "checks": report})
		if err := ctx.Err(); err != nil {
			return err
		}
		return db.Transaction(func(tx *gorm.DB) error {
			result := tx.Model(&model.DevRun{}).Where("id = ? AND state = ? AND cancel_requested = ?", run.ID, "running", false).Updates(map[string]any{"state": "succeeded", "phase": "review", "error": "", "diff": afterDiff, "source_hash": afterHash, "result": run.Result, "finished_at": time.Now().UTC()})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return context.Canceled
			}
			return tx.Create(&model.DevRunEvent{RunID: run.ID, Kind: "succeeded", Message: "真实检查已通过；隔离工作区候选已保存，等待人工审阅或发布"}).Error
		})
	}
	return errors.New("开发任务未完成")
}

// Resolve actual parents before export so a configured or symlinked runs folder
// can never place an execution workspace inside the customer's baseline repo.
func developmentWorkspaceDestination(root, projectPath, key string) (string, error) {
	if _, err := uuid.Parse(key); err != nil {
		return "", errors.New("任务标识无效")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	project, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		return "", err
	}
	inside := func(path string) bool {
		relative, err := filepath.Rel(project, path)
		return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))))
	}
	if inside(absolute) {
		return "", errors.New("任务工作区必须位于项目源码目录之外")
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	if inside(resolved) {
		return "", errors.New("任务工作区必须位于项目源码目录之外")
	}
	parent := filepath.Join(resolved, "runs")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
	}
	if inside(parent) {
		return "", errors.New("任务工作区必须位于项目源码目录之外")
	}
	return filepath.Join(parent, key), nil
}
