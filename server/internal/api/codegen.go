package api

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/codegen"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

func CodegenOptions(c *gin.Context) {
	types, menus, err := service.CodegenOptions()
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"fieldTypes": types, "parentMenus": menus})
}

func CodegenPreview(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	var req codegen.GenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	res, err := service.CodegenBusinessPreviewInProject(req, scope.ActorID, scope.ProjectID)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	if !res.OK {
		utils.FailData(c, res.Error.Msg, gin.H{"errCode": res.Error.ErrCode})
		return
	}
	utils.OK(c, res)
}

func CodegenGenerate(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	var in service.ApplyFilesInput
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	res, err := service.ApplyFormgenFilesInProject(in, scope.ActorID, scope.ProjectID)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	if !res.OK {
		utils.FailData(c, res.Error.Msg, gin.H{"errCode": res.Error.ErrCode, "rollback": res.Rollback})
		return
	}
	utils.OK(c, res)
}
