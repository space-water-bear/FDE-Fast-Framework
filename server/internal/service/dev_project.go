package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/workspace"
)

var projectKeyRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

func ListDevProjects(actorID uint) ([]model.DevProject, error) {
	var user model.User
	if actorID == 0 || global.DB.Preload("Roles").First(&user, actorID).Error != nil || !user.Enabled {
		return nil, errors.New("用户无效")
	}
	q := global.DB.Model(&model.DevProject{})
	admin := false
	for _, role := range user.Roles {
		if role.Enabled && role.Code == "admin" {
			admin = true
		}
	}
	if !admin {
		q = q.Where("owner_id = ?", actorID)
	}
	var list []model.DevProject
	err := q.Order("id asc").Find(&list).Error
	return list, err
}

func CreateDevProject(ctx context.Context, actorID uint, key, name string) (*model.DevProject, error) {
	key, name = strings.TrimSpace(key), strings.TrimSpace(name)
	if actorID == 0 || !projectKeyRe.MatchString(key) || name == "" || len([]rune(name)) > 128 {
		return nil, errors.New("项目标识或名称无效")
	}
	var user model.User
	if global.DB.First(&user, actorID).Error != nil || !user.Enabled {
		return nil, errors.New("用户无效")
	}
	if global.Config == nil {
		return nil, errors.New("开发环境未配置")
	}
	cfg := global.Config.Development
	root := cfg.WorkspaceDir
	if root == "" {
		root = "./data/workspaces"
	}
	repo := cfg.TemplateRepo
	if repo == "" {
		server, err := filepath.Abs(global.Config.Codegen.ServerDir)
		if err != nil {
			return nil, err
		}
		repo = filepath.Dir(server)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	exportCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	exported, err := workspace.Export(exportCtx, repo, cfg.TemplateRef, filepath.Join(root, uuid.NewString()))
	if err != nil {
		return nil, err
	}
	project := &model.DevProject{Key: key, Name: name, OwnerID: actorID, SourceCommit: exported.SourceCommit, WorkspacePath: exported.Path}
	if err := global.DB.Create(project).Error; err != nil {
		_ = os.RemoveAll(exported.Path)
		return nil, err
	}
	return project, nil
}

func CreateDevRun(ctx context.Context, scope EngineeringScope, requirement string, agentIDs ...uint) (*model.DevRun, error) {
	project, err := EngineeringProject(scope)
	if err != nil {
		return nil, err
	}
	if len(agentIDs) != 1 || agentIDs[0] == 0 {
		return nil, errors.New("请选择执行开发任务的智能体")
	}
	var selectedAgent model.AiAgent
	if err := global.DB.First(&selectedAgent, agentIDs[0]).Error; err != nil || !selectedAgent.Enabled {
		return nil, errors.New("开发智能体不存在或已禁用")
	}
	if _, err := loadProvider(&selectedAgent); err != nil {
		return nil, err
	}
	requirement = strings.TrimSpace(requirement)
	if requirement == "" || len(requirement) > 32000 {
		return nil, errors.New("需求不能为空且不能超过 32000 字节")
	}
	if project.WorkspacePath == "" {
		return nil, errors.New("请先创建具有独立源码工作区的客户项目")
	}
	base, err := exec.CommandContext(ctx, "git", "-C", project.WorkspacePath, "rev-parse", "HEAD").Output()
	if err != nil {
		return nil, errors.New("项目源码基线不可用")
	}
	run := &model.DevRun{Key: uuid.NewString(), ProjectID: scope.ProjectID, ActorID: scope.ActorID, AgentID: selectedAgent.ID, Requirement: requirement, BaseCommit: strings.TrimSpace(string(base)), State: "queued", Phase: "plan"}
	err = global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(run).Error; err != nil {
			return err
		}
		return tx.Create(&model.DevRunEvent{RunID: run.ID, Kind: "queued", Message: "需求已保存；等待开发执行器处理"}).Error
	})
	return run, err
}

func GetDevRun(scope EngineeringScope, key string) (*model.DevRun, []model.DevRunEvent, error) {
	if _, err := EngineeringProject(scope); err != nil {
		return nil, nil, err
	}
	var run model.DevRun
	if err := global.DB.Where("key = ? AND project_id = ? AND actor_id = ?", key, scope.ProjectID, scope.ActorID).First(&run).Error; err != nil {
		return nil, nil, errors.New("任务不存在或无权访问")
	}
	var events []model.DevRunEvent
	err := global.DB.Where("run_id = ?", run.ID).Order("id asc").Find(&events).Error
	return &run, events, err
}

func ListDevRuns(scope EngineeringScope, page, pageSize int) ([]model.DevRun, int64, error) {
	if _, err := EngineeringProject(scope); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	query := global.DB.Model(&model.DevRun{}).Where("project_id = ? AND actor_id = ?", scope.ProjectID, scope.ActorID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.DevRun
	if err := query.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func CancelDevRun(scope EngineeringScope, key string) error {
	run, _, err := GetDevRun(scope, key)
	if err != nil {
		return err
	}
	if run.State == "canceled" {
		return errors.New("任务已取消，无需重复操作")
	}
	if run.State == "running" && run.CancelRequested {
		cancelActiveDevelopmentRun(run.ID)
		return nil
	}
	err = global.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		changes := map[string]any{"cancel_requested": true}
		kind, message := "cancel_requested", "用户请求停止开发任务"
		switch run.State {
		case "queued":
			changes["state"], changes["finished_at"] = "canceled", now
			kind, message = "canceled", "用户取消排队中的开发任务"
		case "running":
		default:
			return errors.New("此任务已结束，不能取消")
		}
		result := tx.Model(&model.DevRun{}).Where("id = ? AND state = ?", run.ID, run.State).Updates(changes)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("任务状态已变化，请刷新")
		}
		return tx.Create(&model.DevRunEvent{RunID: run.ID, Kind: kind, Message: message}).Error
	})
	if err == nil {
		cancelActiveDevelopmentRun(run.ID)
	}
	return err
}
