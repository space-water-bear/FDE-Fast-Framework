package api

import (
	"strings"

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
	var req codegen.GenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if strings.TrimSpace(req.ObjectKey) != "" {
		var userID uint
		if claims := utils.GetClaims(c); claims != nil {
			userID = claims.UserID
		}
		res, err := service.CodegenBusinessPreview(req, userID)
		if err != nil {
			utils.Fail(c, err.Error())
			return
		}
		if !res.OK {
			utils.FailData(c, res.Error.Msg, gin.H{"errCode": res.Error.ErrCode})
			return
		}
		utils.OK(c, res)
		return
	}
	files, err := service.CodegenPreview(req)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"files": files})
}

func CodegenGenerate(c *gin.Context) {
	var req codegen.GenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	files, menuID, err := service.CodegenGenerate(req)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"files": files, "menuId": menuID, "restartRequired": true})
}
