package api

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

func requestEngineeringIdentity(c *gin.Context) (service.EngineeringScope, bool) {
	claims := utils.GetClaims(c)
	if claims == nil || claims.UserID == 0 {
		utils.NoAuth(c, "工程写操作需要用户登录；服务 API Key 仅允许预览")
		return service.EngineeringScope{}, false
	}
	projectID := model.DefaultProjectID
	header, query := c.GetHeader("X-FDE-Project-ID"), c.Query("projectId")
	if header != "" && query != "" && header != query {
		utils.Fail(c, "项目参数不一致")
		return service.EngineeringScope{}, false
	}
	value := header
	if value == "" {
		value = query
	}
	if value != "" {
		id, err := strconv.ParseUint(value, 10, 64)
		if err != nil || id == 0 || uint64(uint(id)) != id {
			utils.Fail(c, "项目 ID 无效")
			return service.EngineeringScope{}, false
		}
		projectID = uint(id)
	}
	return service.EngineeringScope{ActorID: claims.UserID, ProjectID: projectID}, true
}

func engineeringRequestScope(c *gin.Context) (service.EngineeringScope, bool) {
	scope, ok := requestEngineeringIdentity(c)
	if !ok {
		return scope, false
	}
	if _, err := service.EngineeringProject(scope); err != nil {
		utils.Forbidden(c, err.Error())
		return service.EngineeringScope{}, false
	}
	return scope, true
}

// Ownerless sessions in the default project are the stored legacy bot format.
// Bot deletion drops the association, so neither the title nor a live binding
// identifies all history. Web session creation always requires a nonzero owner.
func isBotHistorySession(session model.AiSession) bool {
	return session.UserID == 0 && (session.ProjectID == 0 || session.ProjectID == model.DefaultProjectID)
}

func isAiSessionHistoryAdmin(userID uint) bool {
	if global.Config != nil && global.Config.App.Mode == "release" {
		return false
	}
	var user model.User
	if userID == 0 || global.DB.Preload("Roles").First(&user, userID).Error != nil || !user.Enabled {
		return false
	}
	for _, role := range user.Roles {
		if role.Enabled && role.Code == "admin" {
			return true
		}
	}
	return false
}

// Only this strict owner path creates a context that can execute chat tools.
func ownedAiSessionContext(c *gin.Context, id uint) (context.Context, bool) {
	_, scope, ok := authorizeAiSession(c, id, false)
	if !ok {
		return nil, false
	}
	return service.WithEngineeringScope(c.Request.Context(), scope), true
}

// History access grants no executable context. Admins may read default-project
// bot histories while ordinary conversations remain visible to their owners.
func readableAiSession(c *gin.Context, id uint) (model.AiSession, bool) {
	session, _, ok := authorizeAiSession(c, id, true)
	return session, ok
}

func authorizeAiSession(c *gin.Context, id uint, allowBotHistory bool) (model.AiSession, service.EngineeringScope, bool) {
	scope, ok := requestEngineeringIdentity(c)
	var session model.AiSession
	if !ok {
		return session, scope, false
	}
	query := global.DB.Where("id = ?", id)
	if allowBotHistory && isAiSessionHistoryAdmin(scope.ActorID) {
		query = query.Where("(user_id = ? OR (user_id = 0 AND COALESCE(project_id, 0) IN (0, ?)))", scope.ActorID, model.DefaultProjectID)
	} else {
		query = query.Where("user_id = ?", scope.ActorID)
	}
	if err := query.First(&session).Error; err != nil {
		utils.Forbidden(c, "会话不存在或不属于当前用户")
		return session, scope, false
	}
	boundProject := session.ProjectID
	if boundProject == 0 {
		boundProject = model.DefaultProjectID
	}
	explicitProject := c.GetHeader("X-FDE-Project-ID") != "" || c.Query("projectId") != ""
	if explicitProject && scope.ProjectID != boundProject {
		utils.Forbidden(c, "会话已绑定其他项目，请创建该项目的独立会话")
		return session, scope, false
	}
	scope.ProjectID = boundProject
	if boundProject != model.DefaultProjectID || explicitProject {
		if _, err := service.EngineeringProject(scope); err != nil {
			utils.Forbidden(c, err.Error())
			return session, scope, false
		}
	}
	return session, scope, true
}
