package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

type EngineeringScope struct {
	ActorID   uint
	ProjectID uint
}

type engineeringScopeKey struct{}

// WithEngineeringScope is called by the trusted host, never populated from tool JSON.
func WithEngineeringScope(ctx context.Context, scope EngineeringScope) context.Context {
	return context.WithValue(ctx, engineeringScopeKey{}, scope)
}

func RequireEngineeringScope(ctx context.Context) (EngineeringScope, error) {
	scope, ok := ctx.Value(engineeringScopeKey{}).(EngineeringScope)
	if !ok || scope.ActorID == 0 || scope.ProjectID == 0 {
		return EngineeringScope{}, errors.New("工程操作需要已登录用户和明确的项目上下文")
	}
	return scope, nil
}

// EngineeringProject authorizes the current actor before reading project state.
func EngineeringProject(scope EngineeringScope) (*model.DevProject, error) {
	if scope.ActorID == 0 || scope.ProjectID == 0 || global.DB == nil {
		return nil, errors.New("缺少工程执行身份")
	}
	var user model.User
	if err := global.DB.Preload("Roles").First(&user, scope.ActorID).Error; err != nil || !user.Enabled {
		return nil, errors.New("工程执行用户不存在或已停用")
	}
	var project model.DevProject
	if err := global.DB.First(&project, scope.ProjectID).Error; err != nil {
		return nil, errors.New("项目不存在")
	}
	if project.OwnerID == scope.ActorID {
		return &project, nil
	}
	for _, role := range user.Roles {
		if role.Enabled && role.Code == "admin" {
			return &project, nil
		}
	}
	return nil, errors.New("无权访问此客户项目")
}

func authorizeEngineeringTool(ctx context.Context, name, args string) error {
	paths := map[string]string{
		"generate_form_schema":   "/api/formgen/objects",
		"generate_business_code": "/api/codegen/preview",
		"apply_files":            "/api/formgen/apply",
	}
	path, engineering := paths[name]
	if !engineering {
		return nil
	}
	if global.Config != nil && global.Config.App.Mode == "release" {
		return errors.New("客户运行环境未启用工程工具")
	}
	scope, err := RequireEngineeringScope(ctx)
	if err != nil {
		return err
	}
	if _, err := EngineeringProject(scope); err != nil {
		return err
	}
	if global.Enforcer == nil {
		return errors.New("工程工具权限尚未初始化")
	}
	ok, err := global.Enforcer.Enforce(strconv.FormatUint(uint64(scope.ActorID), 10), path, "POST")
	if err != nil || !ok {
		return fmt.Errorf("无权执行工程工具 %s", name)
	}
	return nil
}

// EngineeringCodegenDirs never falls back to the host source for a customer project.
func EngineeringCodegenDirs(projectID uint) (string, string, error) {
	if projectID == model.DefaultProjectID {
		if global.Config == nil {
			return "", "", errors.New("未配置代码生成目录")
		}
		return global.Config.Codegen.ServerDir, global.Config.Codegen.WebDir, nil
	}
	var project model.DevProject
	if global.DB == nil || global.DB.First(&project, projectID).Error != nil || project.WorkspacePath == "" {
		return "", "", errors.New("项目尚未创建独立源码工作区")
	}
	return filepath.Join(project.WorkspacePath, "server"), filepath.Join(project.WorkspacePath, "web"), nil
}
