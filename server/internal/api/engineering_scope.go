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

func ownedAiSessionContext(c *gin.Context, id uint) (context.Context, bool) {
	scope, ok := requestEngineeringIdentity(c)
	if !ok {
		return nil, false
	}
	var session model.AiSession
	if err := global.DB.Where("id = ? AND user_id = ?", id, scope.ActorID).First(&session).Error; err != nil {
		utils.Forbidden(c, "会话不存在或不属于当前用户")
		return nil, false
	}
	boundProject := session.ProjectID
	if boundProject == 0 {
		boundProject = model.DefaultProjectID
	}
	if (c.GetHeader("X-FDE-Project-ID") != "" || c.Query("projectId") != "") && scope.ProjectID != boundProject {
		utils.Forbidden(c, "会话已绑定其他项目，请创建该项目的独立会话")
		return nil, false
	}
	scope.ProjectID = boundProject
	if boundProject != model.DefaultProjectID || c.GetHeader("X-FDE-Project-ID") != "" || c.Query("projectId") != "" {
		if _, err := service.EngineeringProject(scope); err != nil {
			utils.Forbidden(c, err.Error())
			return nil, false
		}
	}
	return service.WithEngineeringScope(c.Request.Context(), scope), true
}
